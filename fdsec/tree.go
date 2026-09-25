package fdsec

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"time"

	"golang.org/x/text/unicode/norm"
)

// Suite 3, the directory container (FDSEC-FORMAT.md section 17): one directory
// tree in one container. The head, the key derivation, the AEAD and the chunk
// discipline are suite 1's; what differs is what is sealed after the head - a
// fixed directory block (17.3) describing a variable-length manifest (17.4),
// and one chunk stream per file entry (17.5).

// SuiteID3 is the published directory suite (FDSEC-FORMAT.md section 13.3).
const SuiteID3 = 3

const (
	// MaxTreePath is the longest relative path a manifest record may carry
	// (FDSEC-FORMAT.md section 17.4 rule 1).
	MaxTreePath = 32767
	// recordFixed is a manifest record without its path: kind, path length,
	// size, three FILETIMEs and the digest (FDSEC-FORMAT.md section 17.4).
	recordFixed = 1 + 2 + 8 + 3*8 + digestSize
	// dirFixed is the directory block without its root name: name length,
	// E, M, T, four FILETIMEs and the manifest digest (section 17.3).
	dirFixed = 2 + 3*8 + 4*8 + digestSize
)

// EntryKind is a manifest record's kind (FDSEC-FORMAT.md section 17.4).
type EntryKind uint8

const (
	KindFile EntryKind = 1
	KindDir  EntryKind = 2
)

// TreeEntry is one manifest record: a file or a directory under the packed
// root, named by its relative, '/'-separated path.
type TreeEntry struct {
	Path       string
	Kind       EntryKind
	Size       int64 // 0 for a directory
	CreatedAt  time.Time
	AccessedAt time.Time
	ModifiedAt time.Time

	digest [digestSize]byte
}

// IsDir reports whether the entry is a directory.
func (e TreeEntry) IsDir() bool { return e.Kind == KindDir }

// TreeMetadata is the sealed directory block's content (FDSEC-FORMAT.md
// section 17.3): the root's true name and timestamps and the manifest's
// totals. Like suite 1's metadata, none of it leaks from the container.
type TreeMetadata struct {
	Name       string
	Entries    int64 // E: every manifest record, files and directories
	TotalSize  int64 // T: the sum of every file entry's size
	EncodedAt  time.Time
	CreatedAt  time.Time
	AccessedAt time.Time
	ModifiedAt time.Time
}

// ValidateTreePath screens a manifest path against FDSEC-FORMAT.md section
// 17.4 rule 1: relative, '/'-separated, every component a valid sealed name.
// It is applied when a path is sealed and again when one is read, and the
// error never echoes the path - a hostile container chooses it.
func ValidateTreePath(p string) error {
	if len(p) < 1 || len(p) > MaxTreePath {
		return fmt.Errorf("path must be 1..%d UTF-8 bytes", MaxTreePath)
	}
	for _, c := range strings.Split(p, "/") {
		if err := ValidateName(c); err != nil {
			return fmt.Errorf("path component: %w", err)
		}
	}
	return nil
}

// treeParent is the path of an entry's parent, or "" for a top-level entry.
func treeParent(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return ""
}

// checkManifestOrder applies section 17.4 rules 2 to 4 to entries already in
// manifest order: strictly ascending by bytes, every parent an earlier
// directory, consistent kinds. It returns the sum of the file sizes.
func checkManifestOrder(entries []TreeEntry) (int64, error) {
	dirs := make(map[string]bool)
	var total int64
	for i, e := range entries {
		if i > 0 && entries[i-1].Path >= e.Path {
			return 0, fmt.Errorf("manifest record %d is out of order or repeated", i)
		}
		if par := treeParent(e.Path); par != "" && !dirs[par] {
			return 0, fmt.Errorf("manifest record %d has no directory record for its parent", i)
		}
		switch e.Kind {
		case KindDir:
			if e.Size != 0 || e.digest != ([digestSize]byte{}) {
				return 0, fmt.Errorf("manifest record %d is a directory with a size or a digest", i)
			}
			dirs[e.Path] = true
		case KindFile:
			if e.Size < 0 || e.Size > math.MaxInt64-total {
				return 0, fmt.Errorf("manifest record %d has an impossible size", i)
			}
			total += e.Size
		default:
			return 0, fmt.Errorf("manifest record %d has kind %d", i, e.Kind)
		}
	}
	return total, nil
}

// encodeManifest lays out the records back to back (FDSEC-FORMAT.md section
// 17.4). The caller has already validated and sorted them.
func encodeManifest(entries []TreeEntry) []byte {
	var b bytes.Buffer
	for _, e := range entries {
		rec := make([]byte, 0, recordFixed+len(e.Path))
		rec = append(rec, byte(e.Kind))
		rec = binary.LittleEndian.AppendUint16(rec, uint16(len(e.Path)))
		rec = append(rec, e.Path...)
		rec = binary.LittleEndian.AppendUint64(rec, uint64(e.Size))
		rec = binary.LittleEndian.AppendUint64(rec, toFILETIME(e.CreatedAt))
		rec = binary.LittleEndian.AppendUint64(rec, toFILETIME(e.AccessedAt))
		rec = binary.LittleEndian.AppendUint64(rec, toFILETIME(e.ModifiedAt))
		rec = append(rec, e.digest[:]...)
		b.Write(rec)
	}
	return b.Bytes()
}

// parseManifest reads exactly count records from b and applies every rule of
// FDSEC-FORMAT.md section 17.4. It runs on authenticated bytes, so a broken
// rule is damage and a reserved kind is a refusal by name - never a
// credential problem. Nothing is written anywhere until it has returned.
func parseManifest(b []byte, count, total int64) ([]TreeEntry, error) {
	if count < 0 || count > int64(len(b))/(recordFixed+1) {
		return nil, fmt.Errorf("%w: manifest of %d bytes cannot hold %d records", ErrDamaged, len(b), count)
	}
	entries := make([]TreeEntry, 0, count)
	o := 0
	for i := int64(0); i < count; i++ {
		if len(b)-o < recordFixed+1 {
			return nil, fmt.Errorf("%w: manifest ends inside record %d", ErrDamaged, i)
		}
		kind := EntryKind(b[o])
		if kind != KindFile && kind != KindDir {
			return nil, fmt.Errorf("%w: manifest entry kind %d", ErrUnsupported, kind)
		}
		plen := int(binary.LittleEndian.Uint16(b[o+1:]))
		if plen < 1 || plen > MaxTreePath || len(b)-o-3 < plen+recordFixed-3 {
			return nil, fmt.Errorf("%w: manifest record %d has an impossible path length", ErrDamaged, i)
		}
		p := string(b[o+3 : o+3+plen])
		if !norm.NFC.IsNormalString(p) {
			return nil, fmt.Errorf("%w: manifest record %d path is not NFC", ErrDamaged, i)
		}
		if err := ValidateTreePath(p); err != nil {
			return nil, fmt.Errorf("%w: manifest record %d: %v", ErrDamaged, i, err)
		}
		f := o + 3 + plen
		size := binary.LittleEndian.Uint64(b[f:])
		if size > math.MaxInt64 {
			return nil, fmt.Errorf("%w: manifest record %d size exceeds max int64", ErrDamaged, i)
		}
		e := TreeEntry{
			Path:       p,
			Kind:       kind,
			Size:       int64(size),
			CreatedAt:  fromFILETIME(binary.LittleEndian.Uint64(b[f+8:])),
			AccessedAt: fromFILETIME(binary.LittleEndian.Uint64(b[f+16:])),
			ModifiedAt: fromFILETIME(binary.LittleEndian.Uint64(b[f+24:])),
		}
		copy(e.digest[:], b[f+32:f+32+digestSize])
		entries = append(entries, e)
		o = f + 32 + digestSize
	}
	if o != len(b) {
		return nil, fmt.Errorf("%w: manifest carries %d bytes after its last record", ErrDamaged, len(b)-o)
	}
	sum, err := checkManifestOrder(entries)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDamaged, err)
	}
	if sum != total {
		return nil, fmt.Errorf("%w: file sizes sum to %d, the directory block says %d", ErrDamaged, sum, total)
	}
	return entries, nil
}

// buildDirBlock lays out the fixed 4096-byte directory block (FDSEC-FORMAT.md
// section 17.3):
//
//	u16le name_len || name || u64le E || u64le M || u64le T ||
//	4x u64le FILETIME || 32-byte manifest digest || random pad to 4096
func buildDirBlock(m TreeMetadata, manifestLen int64, manifestDigest [digestSize]byte, pad func(int) ([]byte, error)) ([]byte, error) {
	name := norm.NFC.String(m.Name)
	if err := ValidateName(name); err != nil {
		return nil, fmt.Errorf("fdsec: folder name: %w", err)
	}
	b := make([]byte, metaPlain)
	binary.LittleEndian.PutUint16(b[0:2], uint16(len(name)))
	copy(b[2:], name)
	o := 2 + len(name)
	binary.LittleEndian.PutUint64(b[o:], uint64(m.Entries))
	binary.LittleEndian.PutUint64(b[o+8:], uint64(manifestLen))
	binary.LittleEndian.PutUint64(b[o+16:], uint64(m.TotalSize))
	binary.LittleEndian.PutUint64(b[o+24:], toFILETIME(m.EncodedAt))
	binary.LittleEndian.PutUint64(b[o+32:], toFILETIME(m.CreatedAt))
	binary.LittleEndian.PutUint64(b[o+40:], toFILETIME(m.AccessedAt))
	binary.LittleEndian.PutUint64(b[o+48:], toFILETIME(m.ModifiedAt))
	copy(b[o+56:], manifestDigest[:])
	off := o + 56 + digestSize
	p, err := pad(metaPlain - off)
	if err != nil {
		return nil, err
	}
	copy(b[off:], p)
	return b, nil
}

// parseDirBlock is buildDirBlock's reader half; it runs on authenticated bytes.
func parseDirBlock(b []byte) (TreeMetadata, int64, [digestSize]byte, error) {
	var m TreeMetadata
	var digest [digestSize]byte
	if len(b) != metaPlain {
		return m, 0, digest, fmt.Errorf("%w: directory block is %d bytes, want %d", ErrDamaged, len(b), metaPlain)
	}
	nameLen := int(binary.LittleEndian.Uint16(b[0:2]))
	if nameLen < 1 || nameLen > MaxNameBytes || 2+nameLen+dirFixed-2 > metaPlain {
		return m, 0, digest, fmt.Errorf("%w: directory block name length %d", ErrDamaged, nameLen)
	}
	name := string(b[2 : 2+nameLen])
	if err := ValidateName(name); err != nil {
		return m, 0, digest, fmt.Errorf("%w: sealed folder name: %v", ErrDamaged, err)
	}
	m.Name = name
	o := 2 + nameLen
	e := binary.LittleEndian.Uint64(b[o:])
	ml := binary.LittleEndian.Uint64(b[o+8:])
	t := binary.LittleEndian.Uint64(b[o+16:])
	if e > math.MaxInt64 || ml > math.MaxInt64 || t > math.MaxInt64 {
		return m, 0, digest, fmt.Errorf("%w: directory block count or size exceeds max int64", ErrDamaged)
	}
	if e > 0 && ml/e < recordFixed+1 {
		return m, 0, digest, fmt.Errorf("%w: a manifest of %d bytes cannot hold %d records", ErrDamaged, ml, e)
	}
	m.Entries, m.TotalSize = int64(e), int64(t)
	m.EncodedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+24:]))
	m.CreatedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+32:]))
	m.AccessedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+40:]))
	m.ModifiedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+48:]))
	copy(digest[:], b[o+56:])
	return m, int64(ml), digest, nil
}

// streamLen is the on-disk length of a chunk stream of n plaintext bytes:
// (k-1)*S + n_last + 16 (FDSEC-FORMAT.md sections 8, 17.2).
func (h *header) streamLen(n int64) int64 {
	return (h.chunkCount(n)-1)*int64(h.ChunkSize) + h.lastLen(n) + tagSize
}

// alignUp rounds v up to the container's cluster alignment.
func (h *header) alignUp(v int64) int64 {
	a := int64(h.ClusterAlignment)
	return (v + a - 1) / a * a
}

// streamEnd is off + streamLen(n), rounded up to the next stream's start,
// with every step checked for signed 64-bit overflow (section 17.6). ok is
// false when the arithmetic does not fit - which is damage.
func (h *header) streamEnd(off, n int64) (int64, bool) {
	k := h.chunkCount(n)
	s := int64(h.ChunkSize)
	if k-1 > (math.MaxInt64-tagSize-int64(h.ClusterAlignment))/s {
		return 0, false
	}
	l := (k-1)*s + h.lastLen(n) + tagSize
	if off > math.MaxInt64-l-int64(h.ClusterAlignment) {
		return 0, false
	}
	return h.alignUp(off + l), true
}

// streamID names a chunk stream: the manifest, or the file entry at manifest
// position e. It owns the stream's nonce context and associated data
// (FDSEC-FORMAT.md section 17.5).
type streamID struct {
	manifest bool
	entry    int64
}

func (s streamID) nonceCtx(i int64) string {
	var b []byte
	if s.manifest {
		b = append(b, "manifest:"...)
	} else {
		b = append(b, "entry:"...)
		b = binary.LittleEndian.AppendUint64(b, uint64(s.entry))
	}
	return string(binary.LittleEndian.AppendUint64(b, uint64(i)))
}

func (s streamID) ad(digest [digestSize]byte, i, k int64, last bool, n int64) []byte {
	var b []byte
	if s.manifest {
		b = append(b, adPrefixManifest...)
		b = append(b, digest[:]...)
	} else {
		b = append(b, adPrefixEntry...)
		b = append(b, digest[:]...)
		b = binary.LittleEndian.AppendUint64(b, uint64(s.entry))
	}
	b = binary.LittleEndian.AppendUint64(b, uint64(i))
	b = binary.LittleEndian.AppendUint64(b, uint64(k))
	if last {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	return binary.LittleEndian.AppendUint64(b, uint64(n))
}

func dirCtx() string { return "dir" }

func adDir(digest [digestSize]byte) []byte {
	return append([]byte(adPrefixDir), digest[:]...)
}
