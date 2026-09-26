package fdsec

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// TreeSink receives a directory container's entries in manifest order, parents
// before children (FDSEC-FORMAT.md section 17.4 rule 3). UnpackTree calls it
// only after the whole manifest has authenticated, hashed to its digest and
// passed every rule - so a sink never sees a path the format refuses.
type TreeSink interface {
	// Dir creates a directory entry.
	Dir(e TreeEntry) error
	// File returns where a file entry's bytes go. UnpackTree closes it.
	File(e TreeEntry) (io.WriteCloser, error)
}

// TreeMetadata reads the sealed directory block of a suite-3 container - the
// root's name and the totals - without reading the manifest or any entry.
func (c *Container) TreeMetadata() (TreeMetadata, error) {
	m, _, _, err := c.readDirBlock()
	return m, err
}

func (c *Container) readDirBlock() (TreeMetadata, int64, [digestSize]byte, error) {
	if !c.IsTree() {
		return TreeMetadata{}, 0, [digestSize]byte{}, ErrFileContainer
	}
	if _, err := c.src.Seek(preMeta, io.SeekStart); err != nil {
		return TreeMetadata{}, 0, [digestSize]byte{}, fmt.Errorf("fdsec: seek directory block: %w", err)
	}
	sealed := make([]byte, metaSize)
	if _, err := io.ReadFull(c.src, sealed); err != nil {
		return TreeMetadata{}, 0, [digestSize]byte{}, containerReadErr("directory block unreadable", "directory block", err)
	}
	nonce, err := deriveNonce(c.fileKey, dirCtx())
	if err != nil {
		return TreeMetadata{}, 0, [digestSize]byte{}, err
	}
	plain, fail := c.aead.Open(nil, nonce, sealed, adDir(c.h.HeaderDigest))
	if fail != nil {
		return TreeMetadata{}, 0, [digestSize]byte{}, fmt.Errorf("%w: directory block does not authenticate", ErrCredentialOrTamper)
	}
	return parseDirBlock(plain)
}

// openStream opens the chunk stream of n plaintext bytes at off, writing the
// plaintext to dst and returning its BLAKE2b-256 (FDSEC-FORMAT.md sections 8,
// 17.5). done is the plaintext already reported to the progress callback.
func (c *Container) openStream(dst io.Writer, id streamID, off, n int64, so streamOpts, done, total int64) ([digestSize]byte, error) {
	var sum [digestSize]byte
	h := c.h
	dh, err := blake2b.New256(nil)
	if err != nil {
		return sum, err
	}
	k := h.chunkCount(n)
	last := h.lastLen(n)
	capacity := h.chunkCapacity()
	// A stream the container cannot physically hold is damage, found before
	// any buffer is sized from it (FDSEC-10).
	containerLen, err := c.src.Seek(0, io.SeekEnd)
	if err != nil {
		return sum, fmt.Errorf("fdsec: size container: %w", err)
	}
	if k > 0 {
		chunkSize := int64(h.ChunkSize)
		if off < 0 || (k > 1 && (containerLen-off)/chunkSize < k-1) || off+(k-1)*chunkSize+last+tagSize > containerLen {
			return sum, fmt.Errorf("%w: a stream of %d bytes does not fit in the container", ErrDamaged, n)
		}
	}
	ct, err := chunkBuffer(k, capacity, last)
	if err != nil {
		return sum, err
	}
	for i := int64(0); i < k; i++ {
		if so.stopped() {
			return sum, ErrStopped
		}
		want := capacity + tagSize
		if i == k-1 {
			want = last + tagSize
		}
		if _, err := c.src.Seek(off+i*int64(h.ChunkSize), io.SeekStart); err != nil {
			return sum, fmt.Errorf("fdsec: seek chunk %d: %w", i, err)
		}
		if _, err := io.ReadFull(c.src, ct[:want]); err != nil {
			return sum, containerReadErr(fmt.Sprintf("chunk %d unreadable", i), fmt.Sprintf("chunk %d", i), err)
		}
		nonce, err := deriveNonce(c.fileKey, id.nonceCtx(i))
		if err != nil {
			return sum, err
		}
		pt, fail := c.aead.Open(nil, nonce, ct[:want], id.ad(h.HeaderDigest, i, k, i == k-1, want-tagSize))
		if fail != nil {
			return sum, fmt.Errorf("%w: chunk %d does not authenticate", ErrCredentialOrTamper, i)
		}
		if _, err := dst.Write(pt); err != nil {
			return sum, fmt.Errorf("fdsec: write output: %w", err)
		}
		dh.Write(pt)
		if so.progress != nil && !id.manifest {
			so.progress(done+int64(i)*capacity+int64(len(pt)), total)
		}
	}
	copy(sum[:], dh.Sum(nil))
	return sum, nil
}

// UnpackTree reads a suite-3 container - the reader algorithm of
// FDSEC-FORMAT.md section 17.8 - and hands every entry to sink. The directory
// block, the whole manifest and the file-length arithmetic are verified before
// the sink sees its first entry; each file's bytes are hashed to its sealed
// digest as they stream, and a mismatch is damage. When UnpackTree fails
// after the sink was called, what the sink wrote is the caller's to discard.
func (c *Container) UnpackTree(sink TreeSink, opts ...StreamOption) (TreeMetadata, []TreeEntry, error) {
	so := newStreamOpts(opts)
	tm, mlen, mdigest, err := c.readDirBlock()
	if err != nil {
		return tm, nil, err
	}
	h := c.h
	total, err := c.src.Seek(0, io.SeekEnd)
	if err != nil {
		return tm, nil, fmt.Errorf("fdsec: size container: %w", err)
	}

	// The manifest stream must fit in the file before a byte of it is read,
	// which also bounds the memory the manifest can claim.
	end, ok := h.streamEnd(h.preLen(), mlen)
	if !ok || end > total {
		return tm, nil, fmt.Errorf("%w: file length %d cannot hold a %d-byte manifest", ErrDamaged, total, mlen)
	}
	var mbuf bytes.Buffer
	got, err := c.openStream(&mbuf, streamID{manifest: true}, h.preLen(), mlen, so, 0, 0)
	if err != nil {
		return tm, nil, err
	}
	if got != mdigest {
		return tm, nil, fmt.Errorf("%w: the manifest does not hash to its sealed digest", ErrDamaged)
	}
	entries, err := parseManifest(mbuf.Bytes(), tm.Entries, tm.TotalSize)
	if err != nil {
		return tm, nil, err
	}

	// Stream offsets are derived, never stored; the file length must equal
	// the end of the last stream rounded to a cluster (section 17.6).
	offs := make([]int64, len(entries))
	for i, e := range entries {
		if e.Kind != KindFile {
			continue
		}
		offs[i] = end
		if end, ok = h.streamEnd(end, e.Size); !ok {
			return tm, nil, fmt.Errorf("%w: sealed sizes cause length overflow", ErrDamaged)
		}
	}
	if total != end {
		return tm, nil, fmt.Errorf("%w: file length %d is impossible for this tree (want %d)", ErrDamaged, total, end)
	}

	var done int64
	for i, e := range entries {
		if e.Kind == KindDir {
			if err := sink.Dir(e); err != nil {
				return tm, entries, err
			}
			continue
		}
		w, err := sink.File(e)
		if err != nil {
			return tm, entries, err
		}
		sum, err := c.openStream(w, streamID{entry: int64(i)}, offs[i], e.Size, so, done, tm.TotalSize)
		if cerr := w.Close(); err == nil && cerr != nil {
			err = cerr
		}
		if err != nil {
			return tm, entries, err
		}
		if sum != e.digest {
			return tm, entries, fmt.Errorf("%w: entry %d does not hash to its sealed digest", ErrDamaged, i)
		}
		done += e.Size
	}
	return tm, entries, nil
}

// discardSink verifies without writing: the read-back of PackTreeFile and the
// `fdsec verify` of a directory container.
type discardSink struct{}

func (discardSink) Dir(TreeEntry) error { return nil }
func (discardSink) File(TreeEntry) (io.WriteCloser, error) {
	return nopWriteCloser{io.Discard}, nil
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

// VerifyTree reads the whole directory container and writes nothing.
func (c *Container) VerifyTree(opts ...StreamOption) (TreeMetadata, []TreeEntry, error) {
	return c.UnpackTree(discardSink{}, opts...)
}

// UnpackTreeInto restores a suite-3 container into root, which must be an
// existing, empty directory the caller created for this restore
// (FDSEC-FORMAT.md section 17.8 step 9). Every path is joined to root
// component by component and checked to stay lexically inside it right before
// the write it guards; directories are created new and files exclusively, so
// nothing that already exists is ever opened, truncated or replaced. On an
// error the caller removes root: it holds nothing but this restore's output.
func (c *Container) UnpackTreeInto(root string, opts ...StreamOption) (TreeMetadata, []TreeEntry, error) {
	if !c.IsTree() {
		return TreeMetadata{}, nil, ErrFileContainer
	}
	des, err := os.ReadDir(root)
	if err != nil {
		return TreeMetadata{}, nil, err
	}
	if len(des) != 0 {
		return TreeMetadata{}, nil, fmt.Errorf("fdsec: restore root %s is not empty", root)
	}
	return c.UnpackTree(dirSink{root: root}, opts...)
}

type dirSink struct{ root string }

func (s dirSink) Dir(e TreeEntry) error {
	p, err := SafeJoin(s.root, e.Path)
	if err != nil {
		return err
	}
	return os.Mkdir(p, 0o755)
}

func (s dirSink) File(e TreeEntry) (io.WriteCloser, error) {
	p, err := SafeJoin(s.root, e.Path)
	if err != nil {
		return nil, err
	}
	return os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
}

// SafeJoin resolves a manifest path under root and refuses, as damage, any
// path that is not a valid tree path or does not stay lexically inside root
// (FDSEC-FORMAT.md sections 17.4 rule 1, 17.8 step 9). The manifest parser has
// already applied rule 1; this is the second, independent check, made next to
// the disk write it guards. The error does not echo the path.
func SafeJoin(root, rel string) (string, error) {
	if err := ValidateTreePath(rel); err != nil {
		return "", fmt.Errorf("%w: entry path: %v", ErrDamaged, err)
	}
	parts := append([]string{root}, strings.Split(rel, "/")...)
	out := filepath.Join(parts...)
	r, err := filepath.Rel(root, out)
	if err != nil || r == "." || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) || filepath.VolumeName(r) != "" {
		return "", fmt.Errorf("%w: an entry path resolves outside the restore root", ErrDamaged)
	}
	return out, nil
}
