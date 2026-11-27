package rehash

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sort"
)

// byteEdit overwrites len(b) source bytes at off with b (same length).
type byteEdit struct {
	off int64
	b   []byte
}

// byteInsert writes b before source offset at (at == source size appends).
type byteInsert struct {
	at int64
	b  []byte
}

// streamEdited streams src to dst, applying same-length edits and inserts. It
// hashes the original and the output on the fly, so files of any size are
// handled without loading them. Inserts at the same offset as an edit are
// written before it.
func streamEdited(ctx context.Context, src *os.File, srcSize int64, dst *os.File, edits []byteEdit, inserts ...byteInsert) (origSum [sha256.Size]byte, outSize int64, outSum [sha256.Size]byte, err error) {
	type op struct {
		off    int64
		insert bool
		b      []byte
	}
	ops := make([]op, 0, len(edits)+len(inserts))
	for _, e := range edits {
		if e.off < 0 || e.off+int64(len(e.b)) > srcSize {
			return origSum, 0, outSum, fmt.Errorf("edit at %d out of range", e.off)
		}
		ops = append(ops, op{e.off, false, e.b})
	}
	added := int64(0)
	for _, in := range inserts {
		if in.at < 0 || in.at > srcSize {
			return origSum, 0, outSum, fmt.Errorf("insert offset %d out of range", in.at)
		}
		ops = append(ops, op{in.at, true, in.b})
		added += int64(len(in.b))
	}
	sort.SliceStable(ops, func(i, j int) bool {
		if ops[i].off != ops[j].off {
			return ops[i].off < ops[j].off
		}
		return ops[i].insert && !ops[j].insert
	})
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return origSum, 0, outSum, err
	}
	origH := sha256.New()
	outH := sha256.New()
	r := io.TeeReader(src, origH)
	w := io.MultiWriter(dst, outH)

	cur := int64(0)
	for _, o := range ops {
		if o.off < cur {
			return origSum, 0, outSum, fmt.Errorf("overlapping edits at %d", o.off)
		}
		if _, err := copyChecked(ctx, w, r, o.off-cur); err != nil {
			return origSum, 0, outSum, err
		}
		cur = o.off
		if !o.insert {
			skip := make([]byte, len(o.b))
			if _, err := io.ReadFull(r, skip); err != nil {
				return origSum, 0, outSum, err
			}
			cur += int64(len(o.b))
		}
		if _, err := w.Write(o.b); err != nil {
			return origSum, 0, outSum, err
		}
	}
	if _, err := copyChecked(ctx, w, r, srcSize-cur); err != nil {
		return origSum, 0, outSum, err
	}
	copy(origSum[:], origH.Sum(nil))
	copy(outSum[:], outH.Sum(nil))
	return origSum, srcSize + added, outSum, nil
}

// appendAt is the insert that appends b at the end of a source of size n.
func appendAt(n int64, b []byte) byteInsert {
	return byteInsert{at: n, b: b}
}

// readAt reads exactly n bytes at off.
func readAt(r io.ReaderAt, off int64, n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := r.ReadAt(b, off); err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return b, nil
}

// tagPayload is the 30-byte ASCII marker every handler writes.
func tagPayload(nonce string) []byte {
	return []byte(tagPrefix + nonce)
}
