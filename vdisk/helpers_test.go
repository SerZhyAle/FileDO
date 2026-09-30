package vdisk

import (
	"context"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20"
	"golang.org/x/crypto/chacha20poly1305"
)

// detRand is a deterministic random source: the ChaCha20 keystream under a key
// derived from a label. It records every draw, which is how the vectors list
// every random input in the order of FDD-FORMAT section 15.1.
type detRand struct {
	s     *chacha20.Cipher
	draws [][]byte
}

func newDetRand(label string) *detRand {
	key := blake2b.Sum256([]byte("FDD test random: " + label))
	s, err := chacha20.NewUnauthenticatedCipher(key[:], make([]byte, chacha20.NonceSize))
	if err != nil {
		panic(err)
	}
	return &detRand{s: s}
}

func (d *detRand) Read(p []byte) (int, error) {
	clear(p)
	d.s.XORKeyStream(p, p)
	d.draws = append(d.draws, append([]byte(nil), p...))
	return len(p), nil
}

// detClock starts at a fixed instant and moves one second per reading.
type detClock struct {
	t     time.Time
	reads []time.Time
}

func (c *detClock) now() time.Time {
	c.reads = append(c.reads, c.t)
	r := c.t
	c.t = c.t.Add(time.Second)
	return r
}

var vectorEpoch = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

// deterministic fixes randomness and time for the rest of the test.
func deterministic(t testing.TB, label string) (*detRand, *detClock) {
	oldR, oldN, oldW := randSource, nowFunc, WriterStamp
	r := newDetRand(label)
	c := &detClock{t: vectorEpoch}
	randSource, nowFunc, WriterStamp = r, c.now, 0
	t.Cleanup(func() { randSource, nowFunc, WriterStamp = oldR, oldN, oldW })
	return r, c
}

// newMemContainer creates a container in memory.
func newMemContainer(t testing.TB, o CreateOptions) (*Container, *memBacking) {
	t.Helper()
	if err := validateCreate(&o); err != nil {
		t.Fatal(err)
	}
	mb := &memBacking{}
	c, err := createOn(context.Background(), mb, o)
	if err != nil {
		t.Fatal(err)
	}
	return c, mb
}

// openMem opens a memory image.
func openMem(b backing, mode OpenMode) (*Container, error) {
	return openOn(context.Background(), b, nil, mode)
}

func mustOpenMem(t testing.TB, b backing, mode OpenMode) *Container {
	t.Helper()
	c, err := openMem(b, mode)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return c
}

// readAll reads the whole logical volume.
func readAll(t testing.TB, c *Container) []byte {
	t.Helper()
	b := make([]byte, c.Info().LogicalSize)
	if n, err := c.ReadAt(b, 0); err != nil || n != len(b) {
		t.Fatalf("read the volume: %d bytes, %v", n, err)
	}
	return b
}

func pattern(seed byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i*7) + byte(i>>9)
	}
	return b
}

// sealRaw seals an arbitrary slot plaintext, for slots the writer never makes.
func sealRaw(sk, salt, nonce, pad, pt []byte) []byte {
	aead, err := chacha20poly1305.NewX(sk)
	if err != nil {
		panic(err)
	}
	b := append(append([]byte(nil), salt...), nonce...)
	b = aead.Seal(b, nonce, pt, []byte(adSlot))
	return append(b, pad...)
}
