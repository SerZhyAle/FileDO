package fdsec

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/text/unicode/norm"
)

// Tree is what PackTree seals: the root's metadata, its entries in any order,
// and a way to open each file entry by its manifest path. Open is called twice
// per file - once for the digest, once for the sealing - exactly as Pack reads
// a suite-1 original twice.
type Tree struct {
	Meta    TreeMetadata // Name and timestamps; Entries and TotalSize are computed
	Entries []TreeEntry
	Open    func(path string) (io.ReadCloser, error)
}

// ScanOptions tunes ScanTree.
type ScanOptions struct {
	// IsReparsePoint reports whether a path is a reparse point (junction,
	// symbolic link, mount point). The caller supplies the platform's own
	// classification - on Windows the attribute bit the wipe path already
	// checks - so there is one implementation of that test, not two.
	IsReparsePoint func(path string) bool
	// Times returns an entry's creation, last-access and last-write times.
	// Nil records the modification time only.
	Times func(fi os.FileInfo) (created, accessed, modified time.Time)
}

// ScanTree walks root once and records every file and directory under it
// (FDSEC-FORMAT.md section 17.7 step 1). Anything that is neither an ordinary
// file nor an ordinary directory - a junction, a symbolic link, a mount
// point, a device - is refused with its path named, never followed and never
// skipped: following one can walk in a circle or out of the tree the user
// pointed at, and skipping one would pack a tree that is not the one on disk.
// A name the format cannot carry is refused the same way rather than altered.
func ScanTree(root string, o ScanOptions) (*Tree, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	rfi, err := os.Lstat(abs)
	if err != nil {
		return nil, err
	}
	if err := scanRefuse(abs, rfi, o); err != nil {
		return nil, err
	}
	if !rfi.IsDir() {
		return nil, fmt.Errorf("%s is not a folder", abs)
	}
	name := norm.NFC.String(filepath.Base(abs))
	if err := ValidateName(name); err != nil {
		return nil, fmt.Errorf("%s: the folder's name cannot be stored in a container (%v)", abs, err)
	}
	t := &Tree{Meta: TreeMetadata{Name: name}}
	t.Meta.CreatedAt, t.Meta.AccessedAt, t.Meta.ModifiedAt = scanTimes(rfi, o)
	osPath := make(map[string]string)
	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		des, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, de := range des {
			full := filepath.Join(dir, de.Name())
			fi, err := os.Lstat(full)
			if err != nil {
				return err
			}
			if err := scanRefuse(full, fi, o); err != nil {
				return err
			}
			n := norm.NFC.String(de.Name())
			if err := ValidateName(n); err != nil {
				return fmt.Errorf("%s: this name cannot be stored in a container (%v)", full, err)
			}
			p := n
			if rel != "" {
				p = rel + "/" + n
			}
			if len(p) > MaxTreePath {
				return fmt.Errorf("%s: the path inside the folder is longer than %d bytes", full, MaxTreePath)
			}
			if _, dup := osPath[p]; dup {
				return fmt.Errorf("%s: two names in this folder become the same name once normalised to NFC", full)
			}
			osPath[p] = full
			e := TreeEntry{Path: p, Kind: KindFile}
			e.CreatedAt, e.AccessedAt, e.ModifiedAt = scanTimes(fi, o)
			if fi.IsDir() {
				e.Kind = KindDir
				t.Entries = append(t.Entries, e)
				if err := walk(full, p); err != nil {
					return err
				}
				continue
			}
			e.Size = fi.Size()
			t.Entries = append(t.Entries, e)
		}
		return nil
	}
	if err := walk(abs, ""); err != nil {
		return nil, err
	}
	t.Open = func(p string) (io.ReadCloser, error) {
		full, ok := osPath[p]
		if !ok {
			return nil, fmt.Errorf("fdsec: %s is not in the scanned tree", p)
		}
		return os.Open(full)
	}
	return t, nil
}

func scanRefuse(path string, fi os.FileInfo, o ScanOptions) error {
	if (o.IsReparsePoint != nil && o.IsReparsePoint(path)) || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return fmt.Errorf("%s is a reparse point (junction, symbolic link or mount point); a folder containing one is refused rather than followed", path)
	}
	if !fi.IsDir() && !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is neither an ordinary file nor a folder, and is refused", path)
	}
	return nil
}

func scanTimes(fi os.FileInfo, o ScanOptions) (time.Time, time.Time, time.Time) {
	if o.Times != nil {
		return o.Times(fi)
	}
	return time.Time{}, time.Time{}, fi.ModTime()
}

// sortedTree validates a tree against FDSEC-FORMAT.md section 17.4 and
// returns its entries in manifest order with the file-size total.
func sortedTree(t *Tree) ([]TreeEntry, int64, error) {
	entries := make([]TreeEntry, len(t.Entries))
	copy(entries, t.Entries)
	for i := range entries {
		entries[i].Path = norm.NFC.String(entries[i].Path)
		entries[i].digest = [digestSize]byte{}
		if err := ValidateTreePath(entries[i].Path); err != nil {
			return nil, 0, fmt.Errorf("fdsec: %w", err)
		}
		if entries[i].Kind == KindDir && entries[i].Size != 0 {
			return nil, 0, fmt.Errorf("fdsec: directory entry with a size")
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	total, err := checkManifestOrder(entries)
	if err != nil {
		return nil, 0, fmt.Errorf("fdsec: %w", err)
	}
	return entries, total, nil
}

// PackTree writes a complete suite-3 container for t to dst - the writer
// algorithm of FDSEC-FORMAT.md section 17.7, steps 2 to 4. Every file is read
// once for its digest and once for its sealing; a file whose size differs
// from what the scan recorded is refused, and one whose bytes change between
// the two reads is caught by the read-back PackTreeFile runs.
func PackTree(dst io.Writer, t *Tree, cred Credential, p Params, opts ...StreamOption) (Info, error) {
	entries, total, err := sortedTree(t)
	if err != nil {
		return Info{}, err
	}
	return packTree(dst, t, entries, total, cred, p, newStreamOpts(opts))
}

func packTree(dst io.Writer, t *Tree, entries []TreeEntry, total int64, cred Credential, p Params, so streamOpts) (Info, error) {
	var info Info
	p, err := p.withDefaults()
	if err != nil {
		return info, err
	}
	info.Params = p
	meta := t.Meta
	meta.Entries = int64(len(entries))
	meta.TotalSize = total
	if meta.EncodedAt.IsZero() {
		meta.EncodedAt = time.Now().UTC()
	}

	// Pass 1: every file's digest, and its size against the scan's claim.
	for i := range entries {
		e := &entries[i]
		if e.Kind != KindFile {
			continue
		}
		rc, err := t.Open(e.Path)
		if err != nil {
			return info, err
		}
		dh, _ := blake2b.New256(nil)
		n, err := io.Copy(dh, stopReader{r: rc, so: so})
		rc.Close()
		if errors.Is(err, ErrStopped) {
			return info, ErrStopped
		}
		if err != nil {
			return info, fmt.Errorf("fdsec: digest %s: %w", e.Path, err)
		}
		if n != e.Size {
			return info, fmt.Errorf("fdsec: %s changed while it was being packed (%d bytes, the scan saw %d)", e.Path, n, e.Size)
		}
		copy(e.digest[:], dh.Sum(nil))
	}
	info.Size = total

	// Randoms in the draw order of FDSEC-FORMAT.md section 17.9.
	salt, err := randBytes(saltSize)
	if err != nil {
		return info, fmt.Errorf("fdsec: salt: %w", err)
	}
	fileKey, err := randBytes(fileKeySize)
	if err != nil {
		return info, fmt.Errorf("fdsec: file key: %w", err)
	}
	defer clear(fileKey) // FDSEC-15
	wrapNonce, err := randBytes(nonceSize)
	if err != nil {
		return info, fmt.Errorf("fdsec: wrap nonce: %w", err)
	}

	h := header{
		Version:          FormatVersion,
		Suite:            SuiteID3,
		Flags:            flagV1Conform,
		ChunkSize:        p.ChunkSize,
		ClusterAlignment: p.ClusterAlignment,
	}
	copy(h.Salt[:], salt)
	h.setDigest()
	info.HeaderDigest = h.HeaderDigest

	// The head is suite 1's (FDSEC-FORMAT.md section 17): same mask, same
	// wrap, same slot-0 binding.
	maskKey, kek, err := deriveKeys(cred, salt)
	if err != nil {
		return info, err
	}
	defer clear(maskKey) // FDSEC-15
	defer clear(kek)
	kekAEAD, err := newXAEAD(kek)
	if err != nil {
		return info, err
	}
	var slot0 slotRecord
	slot0.Type = typeCredWrap
	copy(slot0.Payload[wrapNonceOff:], wrapNonce)
	copy(slot0.Payload[nonceSize:], kekAEAD.Seal(nil, wrapNonce, fileKey, adSlot0(h.HeaderDigest)))

	manifest := encodeManifest(entries)
	dirPlain, err := buildDirBlock(meta, int64(len(manifest)), blake2b.Sum256(manifest), randBytes)
	if err != nil {
		return info, err
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		return info, err
	}
	dirNonce, err := deriveNonce(fileKey, dirCtx())
	if err != nil {
		return info, err
	}
	sealedDir := fileAEAD.Seal(nil, dirNonce, dirPlain, adDir(h.HeaderDigest))

	head, err := buildHead(&h, &slot0, maskKey)
	if err != nil {
		return info, err
	}
	preLen := h.preLen()
	alignPad, err := randBytes(int(preLen) - preMeta - metaSize)
	if err != nil {
		return info, err
	}
	for _, part := range [][]byte{head, sealedDir, alignPad} {
		if _, err := dst.Write(part); err != nil {
			return info, fmt.Errorf("fdsec: write head: %w", err)
		}
	}

	w := &treeWriter{dst: dst, h: &h, aead: fileAEAD, key: fileKey, off: preLen, so: so, total: total}
	if err := w.stream(streamID{manifest: true}, bytes.NewReader(manifest), int64(len(manifest))); err != nil {
		return info, err
	}
	for i, e := range entries {
		if e.Kind != KindFile {
			continue
		}
		rc, err := t.Open(e.Path)
		if err != nil {
			return info, err
		}
		err = w.stream(streamID{entry: int64(i)}, rc, e.Size)
		rc.Close()
		if err != nil {
			return info, fmt.Errorf("fdsec: %s: %w", e.Path, err)
		}
	}
	info.Chunks = w.chunks
	info.TotalLen = w.off
	return info, nil
}

// treeWriter seals the chunk streams of a suite-3 container one after
// another, each starting on a cluster boundary and followed by its random gap
// pad (FDSEC-FORMAT.md sections 17.2, 17.5).
type treeWriter struct {
	dst    io.Writer
	h      *header
	aead   cipher.AEAD
	key    []byte
	off    int64
	chunks int64
	so     streamOpts
	done   int64
	total  int64
}

func (w *treeWriter) stream(id streamID, src io.Reader, n int64) error {
	h := w.h
	k := h.chunkCount(n)
	last := h.lastLen(n)
	capacity := h.chunkCapacity()
	pt := make([]byte, capacity)
	var written int64
	for i := int64(0); i < k; i++ {
		if w.so.stopped() {
			return ErrStopped
		}
		want := capacity
		if i == k-1 {
			want = last
		}
		if _, err := io.ReadFull(src, pt[:want]); err != nil {
			return fmt.Errorf("read chunk %d: %w", i, err)
		}
		nonce, err := deriveNonce(w.key, id.nonceCtx(i))
		if err != nil {
			return err
		}
		ct := w.aead.Seal(nil, nonce, pt[:want], id.ad(h.HeaderDigest, i, k, i == k-1, want))
		if _, err := w.dst.Write(ct); err != nil {
			return fmt.Errorf("write chunk %d: %w", i, err)
		}
		written += int64(len(ct))
		if !id.manifest && w.so.progress != nil {
			w.so.progress(w.done+int64(i)*capacity+want, w.total)
		}
	}
	if !id.manifest {
		w.done += n
	}
	w.chunks += k
	end := w.off + written
	gap, err := randBytes(int(h.alignUp(end) - end))
	if err != nil {
		return err
	}
	if _, err := w.dst.Write(gap); err != nil {
		return fmt.Errorf("write gap pad: %w", err)
	}
	w.off = h.alignUp(end)
	return nil
}

// PackTreeFile packs a tree into a container at dstPath with the guarantees
// PackFile gives a single file: a temporary sibling, a full read-back of
// every tag, the manifest digest and every entry's digest, and only then the
// rename into place - and never over an existing dstPath.
func PackTreeFile(dstPath string, t *Tree, cred Credential, p Params, opts ...StreamOption) (Info, error) {
	return packToFile(dstPath, newStreamOpts(opts).replace, func(f io.Writer) (Info, error) {
		return PackTree(f, t, cred, p, opts...)
	}, func(rb io.ReadSeeker) error {
		return verifyContainer(rb, cred)
	})
}
