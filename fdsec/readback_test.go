package fdsec

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPackFileReadBackGuard proves the half of safety invariant 1 that lives
// in this package: PackFile does not hand back a container it could not read
// back. The CLI half - that a failed PackFile leaves the original untouched -
// is proven by the black-box test in cmd/filedo.
func TestPackFileReadBackGuard(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "original.bin")
	payload := make([]byte, 300000)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	if err := os.WriteFile(srcPath, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	dstPath := filepath.Join(dir, "original.fd-sec")

	// Corrupt one ciphertext byte after the container was written and closed,
	// so the read-back is the only thing that can catch it.
	corrupted := ""
	beforeReadBack = func(tmpPath string) {
		corrupted = tmpPath
		f, err := os.OpenFile(tmpPath, os.O_RDWR, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		// preLen is at least headerSize+slotsSize+metaSize rounded up to a
		// cluster; 1 KiB past it is inside the first sealed chunk. Flip the
		// byte rather than overwrite it: a fixed value equals the random
		// ciphertext byte about once in 256 runs and corrupts nothing.
		at := int64(preMeta+metaSize) + 4096 + 1024
		b := make([]byte, 1)
		if _, err := f.ReadAt(b, at); err != nil {
			t.Fatal(err)
		}
		b[0] ^= 0xff
		if _, err := f.WriteAt(b, at); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { beforeReadBack = nil }()

	meta := Metadata{Name: "original.bin", Size: int64(len(payload))}
	if _, err := PackFile(dstPath, srcPath, meta, NewCredential("pw"), Params{}); err == nil {
		t.Fatal("PackFile accepted a container whose read-back had to fail")
	}
	if corrupted == "" {
		t.Fatal("the read-back seam was never reached")
	}
	if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
		t.Fatalf("a container was left at %s after a failed read-back", dstPath)
	}
	if _, err := os.Stat(corrupted); !os.IsNotExist(err) {
		t.Fatalf("the partial temporary %s survived a failed read-back", corrupted)
	}
	// The original is the caller's; PackFile must never touch it.
	back, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(payload) {
		t.Fatalf("the original changed size: %d, want %d", len(back), len(payload))
	}
}
