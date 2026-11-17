package backupzip

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// verifyFixture packs a small tree with a stored (level 0) file, so a flipped
// byte inside it is a byte of the file and not of a deflate stream, and returns
// the zip with what Pack reported.
func verifyFixture(t *testing.T) (string, PackResult) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Docs")
	writeTree(t, src, map[string][]byte{
		"a.txt":     bytes.Repeat([]byte("alpha "), 2000),
		"sub/b.bin": noise(40 << 10),
		"empty.dat": {},
	})
	if err := os.MkdirAll(filepath.Join(src, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := Walk(src, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "Docs_001.zip")
	res, err := Pack(dst, w, PackOptions{Level: 0, Name: "Docs"})
	if err != nil {
		t.Fatal(err)
	}
	return dst, res
}

// TestVerifyAcceptsWhatPackWrote: a zip read back in full matches the counts the
// pack reported, folders and an empty file included (SP-0208).
func TestVerifyAcceptsWhatPackWrote(t *testing.T) {
	path, res := verifyFixture(t)
	if err := Verify(path, res.Files, res.Bytes, nil); err != nil {
		t.Fatalf("a zip Pack just wrote does not verify: %v", err)
	}
}

// TestVerifyRefusesAWrongCount: a zip that holds other numbers than the run
// packed is not the generation the run built.
func TestVerifyRefusesAWrongCount(t *testing.T) {
	path, res := verifyFixture(t)
	for name, c := range map[string][2]int64{
		"one file too many":  {res.Files + 1, res.Bytes},
		"one file too few":   {res.Files - 1, res.Bytes},
		"one byte too many":  {res.Files, res.Bytes + 1},
		"nothing was packed": {0, 0},
	} {
		if err := Verify(path, c[0], c[1], nil); !errors.Is(err, ErrVerify) {
			t.Errorf("%s: Verify = %v, want ErrVerify", name, err)
		}
	}
}

// TestVerifyRefusesFlippedAndTruncated: a flipped byte inside a stored file fails
// its checksum, and a truncated zip has no central directory; both are ErrVerify,
// never a pass and never a bare read error.
func TestVerifyRefusesFlippedAndTruncated(t *testing.T) {
	path, res := verifyFixture(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The stored text of a.txt is the first long run of "alpha " in the file.
	at := bytes.Index(b, []byte("alpha alpha alpha"))
	if at < 0 {
		t.Fatal("the stored text is not in the zip")
	}
	flipped := append([]byte(nil), b...)
	flipped[at+3] ^= 0xFF
	flippedPath := filepath.Join(t.TempDir(), "flipped.zip")
	if err := os.WriteFile(flippedPath, flipped, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(flippedPath, res.Files, res.Bytes, nil); !errors.Is(err, ErrVerify) {
		t.Errorf("a flipped byte: Verify = %v, want ErrVerify", err)
	}
	truncPath := filepath.Join(t.TempDir(), "truncated.zip")
	if err := os.WriteFile(truncPath, b[:len(b)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Verify(truncPath, res.Files, res.Bytes, nil); !errors.Is(err, ErrVerify) {
		t.Errorf("a truncated zip: Verify = %v, want ErrVerify", err)
	}
}

// TestVerifyStopsAndMissingIsNotAVerdict: a stop ends with ErrStopped, and a file
// that cannot be opened at all is the read failing, not a zip that failed.
func TestVerifyStopsAndMissingIsNotAVerdict(t *testing.T) {
	path, res := verifyFixture(t)
	if err := Verify(path, res.Files, res.Bytes, func() bool { return true }); !errors.Is(err, ErrStopped) {
		t.Errorf("a stop: Verify = %v, want ErrStopped", err)
	}
	err := Verify(filepath.Join(t.TempDir(), "absent.zip"), 1, 1, nil)
	if err == nil || errors.Is(err, ErrVerify) {
		t.Errorf("a missing file: Verify = %v, want the file system's own error", err)
	}
}
