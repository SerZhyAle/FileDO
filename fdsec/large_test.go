package fdsec

import (
	"bytes"
	"crypto/sha256" //nolint:gosec // used only to derive a fixed test key, not for security
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/chacha20"
)

// seqReader is a seekable deterministic stream: the chacha20 keystream of a
// fixed key, so a >4 GiB "original" costs no disk and no entropy, and any
// offset can be re-derived exactly.
type seqReader struct {
	key   [32]byte
	nonce [12]byte
	size  int64
	pos   int64
	zeros []byte // reused zero source: content = 0 XOR keystream = keystream
}

func newSeqReader(size int64) *seqReader {
	s := &seqReader{size: size}
	k := sha256.Sum256([]byte("fdsec-test-stream"))
	copy(s.key[:], k[:])
	copy(s.nonce[:], "fdsec-seq-01")
	return s
}

func (s *seqReader) Read(p []byte) (int, error) {
	if s.pos >= s.size {
		return 0, io.EOF
	}
	if max := s.size - s.pos; int64(len(p)) > max {
		p = p[:max]
	}
	if len(p) == 0 {
		return 0, nil
	}
	c, err := chacha20.NewUnauthenticatedCipher(s.key[:], s.nonce[:])
	if err != nil {
		return 0, err
	}
	c.SetCounter(uint32(s.pos / 64))
	if rem := s.pos % 64; rem != 0 {
		skip := make([]byte, rem)
		c.XORKeyStream(skip, skip)
	}
	// The content IS the keystream, so it must be produced by XORing a zero
	// source - XORing the destination into itself (stale bytes from a previous
	// read) would chain old content into the new output.
	if len(s.zeros) < len(p) {
		s.zeros = make([]byte, len(p))
	}
	c.XORKeyStream(p, s.zeros[:len(p)])
	s.pos += int64(len(p))
	return len(p), nil
}

func (s *seqReader) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
		s.pos = off
	case io.SeekCurrent:
		s.pos += off
	case io.SeekEnd:
		s.pos = s.size + off
	default:
		return s.pos, os.ErrInvalid
	}
	if s.pos < 0 {
		s.pos = 0
		return 0, os.ErrInvalid
	}
	return s.pos, nil
}

var _ io.ReadSeeker = (*seqReader)(nil)

// The >4 GiB corpus member (exit criterion of stage S2): crosses the 32-bit
// boundary in every length and offset computation. The default chunk size and
// alignment are used; the KDF runs under the suite's test profile because the
// point here is the length arithmetic, not the stretch cost. Byte-exactness is
// proven by Unpack's internal digest check: the recovered stream must hash to
// the digest Pack computed over the same generator. Skipped under -short.
func TestRoundTrip_Over4GiB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the >4 GiB round trip in -short mode")
	}
	const size = int64(4)<<30 + 1
	p := DefaultParams() // default alignment and chunk size, so the 32-bit boundary is crossed at full slot size
	meta := Metadata{
		Name:      "large.bin",
		Size:      size,
		EncodedAt: time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	}
	cred := NewCredential("pw123")

	started := time.Now()
	f, err := os.Create(filepath.Join(t.TempDir(), "large.fd-sec"))
	if err != nil {
		t.Fatal(err)
	}
	info, err := Pack(f, newSeqReader(size), meta, cred, p)
	if err != nil {
		f.Close()
		t.Fatalf("Pack: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	packedAt := time.Now()

	if info.Size != size {
		t.Fatalf("size %d, want %d", info.Size, size)
	}
	capacity := int64(p.ChunkSize) - tagSize
	if want := (size + capacity - 1) / capacity; info.Chunks != want {
		t.Fatalf("chunks %d, want %d", info.Chunks, want)
	}
	if info.TotalLen%int64(DefaultClusterAlignment) != 0 {
		t.Fatalf("container length %d is not cluster-aligned", info.TotalLen)
	}

	rf, err := os.Open(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer rf.Close()
	m, err := Unpack(io.Discard, rf, cred)
	if err != nil {
		t.Fatalf("Unpack (this includes the final digest check): %v", err)
	}
	if m.Size != size || m.Name != "large.bin" {
		t.Fatalf("metadata: %+v", m)
	}
	t.Logf("packed %d bytes in %s, unpacked+verified in %s (total %s)",
		size, packedAt.Sub(started).Round(time.Millisecond), time.Since(packedAt).Round(time.Millisecond), time.Since(started).Round(time.Millisecond))
}

// Spot-check that the generator is truly seek-stable (the property the
// >4 GiB proof rests on): two instances at one offset agree, and different
// offsets differ.
func TestSeqReader_SeekStable(t *testing.T) {
	readAt := func(off int64) []byte {
		s := newSeqReader(1 << 20)
		if _, err := s.Seek(off, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 64)
		if _, err := io.ReadFull(s, b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.Equal(readAt(1<<16), readAt(1<<16)) {
		t.Fatal("generator is not seek-stable across instances")
	}
	if bytes.Equal(readAt(0), readAt(1<<16)) {
		t.Fatal("generator produced identical data at different offsets")
	}
}
