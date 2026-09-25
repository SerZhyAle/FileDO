package fdsec

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestPackFile_DestinationAppearsDuringPack is FDSEC-01: a name that appears
// while the pack runs - a second secure to the same container name - is never
// written over, nothing partial is left, and the caller learns ErrExists.
func TestPackFile_DestinationAppearsDuringPack(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "IMG_0001.JPG")
	content := randContent(t, 5000)
	if err := os.WriteFile(src, content, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "IMG_0001.fd-sec")
	theirs := []byte("the other process's container")
	beforeReadBack = func(string) {
		if err := os.WriteFile(dst, theirs, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	defer func() { beforeReadBack = nil }()

	meta := fixedTimes
	meta.Size = int64(len(content))
	_, err := PackFile(dst, src, meta, NewCredential("pw"), fastParams())
	if !errors.Is(err, ErrExists) {
		t.Fatalf("PackFile over a name that appeared mid-pack: err = %v, want ErrExists", err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, theirs) {
		t.Fatal("the container that appeared mid-pack was written over")
	}
	assertNoPartials(t, dir)
}

// TestPackFile_AllowReplaceKeepsOldUntilVerified is FDSEC-03: an overwrite the
// user chose replaces the old destination only after the new container read
// back in full; a failure before that leaves the old one intact.
func TestPackFile_AllowReplaceKeepsOldUntilVerified(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "report.docx")
	content := randContent(t, 3000)
	os.WriteFile(src, content, 0o644)
	dst := filepath.Join(dir, "report.fd-sec")
	old := []byte("an older container the user chose to overwrite")
	os.WriteFile(dst, old, 0o644)
	meta := fixedTimes
	meta.Size = int64(len(content))
	cred := NewCredential("pw")

	// Without AllowReplace the existing name is refused up front.
	if _, err := PackFile(dst, src, meta, cred, fastParams()); !errors.Is(err, ErrExists) {
		t.Fatalf("PackFile over an existing name without AllowReplace: err = %v, want ErrExists", err)
	}

	// A read-back failure after "overwrite" was chosen: the old file survives.
	beforeReadBack = func(tmp string) {
		b, _ := os.ReadFile(tmp)
		b[len(b)/2] ^= 0xff
		os.WriteFile(tmp, b, 0o644)
	}
	_, err := PackFile(dst, src, meta, cred, fastParams(), AllowReplace())
	beforeReadBack = nil
	if err == nil {
		t.Fatal("a corrupted read-back must fail the pack")
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, old) {
		t.Fatal("the old destination was lost before the new container was verified")
	}
	assertNoPartials(t, dir)

	// And a pack that verifies replaces it.
	if _, err := PackFile(dst, src, meta, cred, fastParams(), AllowReplace()); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(dst)
	defer f.Close()
	got := &bytes.Buffer{}
	if _, err := Unpack(got, f, cred); err != nil || !bytes.Equal(got.Bytes(), content) {
		t.Fatalf("the replacing container does not round-trip: %v", err)
	}
}

// TestPack_StopsAtChunkBoundary is FDSEC-02 at the format level: a stop
// request ends Pack, Unpack and the file-level pack with ErrStopped and leaves
// no container and no partial.
func TestPack_StopsAtChunkBoundary(t *testing.T) {
	content := randContent(t, 64*1024)
	meta := fixedTimes
	meta.Size = int64(len(content))
	cred := NewCredential("pw")

	// Stop after a few chunks of the sealing pass.
	chunks := 0
	stop := func() bool { chunks++; return chunks > 5 }
	out := &bytes.Buffer{}
	if _, err := Pack(out, bytes.NewReader(content), meta, cred, fastParams(), WithStop(stop)); !errors.Is(err, ErrStopped) {
		t.Fatalf("Pack with a stop: err = %v, want ErrStopped", err)
	}

	// A stop requested before anything runs ends the digest pass.
	if _, err := Pack(&bytes.Buffer{}, bytes.NewReader(content), meta, cred, fastParams(), WithStop(func() bool { return true })); !errors.Is(err, ErrStopped) {
		t.Fatalf("Pack stopped from the start: err = %v, want ErrStopped", err)
	}

	raw := packBytes(t, content, meta, cred, fastParams())
	calls := 0
	if _, err := Unpack(&bytes.Buffer{}, bytes.NewReader(raw), cred, WithStop(func() bool { calls++; return calls > 3 })); !errors.Is(err, ErrStopped) {
		t.Fatalf("Unpack with a stop: err = %v, want ErrStopped", err)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "big.bin")
	os.WriteFile(src, content, 0o644)
	dst := filepath.Join(dir, "big.fd-sec")
	n := 0
	if _, err := PackFile(dst, src, meta, cred, fastParams(), WithStop(func() bool { n++; return n > 10 })); !errors.Is(err, ErrStopped) {
		t.Fatalf("PackFile with a stop: err = %v, want ErrStopped", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("a stopped pack left a container under the final name")
	}
	assertNoPartials(t, dir)
}

// TestTamper_HugeChunkSize is FDSEC-10: a header that claims a 4 GiB chunk
// size over a one-byte payload - valid by the contract - is read without
// allocating anything near the claimed size, on 32-bit builds too.
func TestTamper_HugeChunkSize(t *testing.T) {
	cred := NewCredential("pw123")
	content := []byte{0x42}
	meta := fixedTimes
	meta.Size = 1
	raw := packBytes(t, content, meta, cred, Params{ClusterAlignment: 512, ChunkSize: 1024})
	forged := rewriteHeader(t, raw, cred, func(h *header) { h.ChunkSize = 0xFFFFF000 })

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got := &bytes.Buffer{}
	_, err := Unpack(got, bytes.NewReader(forged), cred)
	runtime.ReadMemStats(&after)

	if grew := after.TotalAlloc - before.TotalAlloc; grew > 8<<20 {
		t.Fatalf("reading a 1-byte payload allocated %d bytes", grew)
	}
	if err != nil {
		// Refusing it is acceptable only as one of the outcome classes.
		if !errors.Is(err, ErrDamaged) && !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unexpected error class: %v", err)
		}
		return
	}
	if !bytes.Equal(got.Bytes(), content) {
		t.Fatalf("recovered %x, want %x", got.Bytes(), content)
	}
}

func assertNoPartials(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, "*.fdsec-partial-*"))
	if len(matches) != 0 {
		t.Fatalf("partial files left behind: %v", matches)
	}
}
