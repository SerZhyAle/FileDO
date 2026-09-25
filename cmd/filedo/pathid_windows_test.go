package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPathIdentitySameFileAcrossSpellings is B1: two spellings of one file are
// one object, and a different file is not.
func TestPathIdentitySameFileAcrossSpellings(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(a, []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	b := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(b, []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}

	spellings := []string{
		strings.ToUpper(a),
		filepath.Join(dir, ".", "a.bin"),
		filepath.Join(dir, "sub", "..", "a.bin"),
		`\\?\` + a,
	}
	for _, s := range spellings {
		same, err := sameFilePaths(a, s)
		if err != nil {
			t.Fatalf("sameFilePaths(%q, %q): %v", a, s, err)
		}
		if !same {
			t.Errorf("%q and %q must be the same file", a, s)
		}
	}
	if same, err := sameFilePaths(a, b); err != nil || same {
		t.Errorf("a.bin and b.bin: same=%v err=%v, want different", same, err)
	}
	if same, err := sameFilePaths(a, filepath.Join(dir, "missing")); err != nil || same {
		t.Errorf("a missing path is never the same file: same=%v err=%v", same, err)
	}

	link := filepath.Join(dir, "hard.bin")
	if err := os.Link(a, link); err == nil {
		if same, err := sameFilePaths(a, link); err != nil || !same {
			t.Errorf("a hard link names the same file: same=%v err=%v", same, err)
		}
	}
}

// TestPathsOverlapNestedAndJunction covers COPY-10/CHK-01's refusal shape:
// the same folder, a folder inside another (existing or not), and a junction
// that is a second name of the same folder.
func TestPathsOverlapNestedAndJunction(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "Data")
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "Other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		a, b string
		want bool
	}{
		{src, strings.ToLower(src) + `\`, true},
		{src, filepath.Join(src, "inner"), true},
		{src, filepath.Join(src, "backup", "not-yet"), true},
		{filepath.Join(src, "inner"), src, true},
		{src, other, false},
		{src, src + "2", false},
	}
	for _, c := range cases {
		got, err := pathsOverlap(c.a, c.b)
		if err != nil {
			t.Fatalf("pathsOverlap(%q, %q): %v", c.a, c.b, err)
		}
		if got != c.want {
			t.Errorf("pathsOverlap(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}

	junction := filepath.Join(dir, "Alias")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, src).CombinedOutput(); err == nil {
		got, err := pathsOverlap(junction, src)
		if err != nil || !got {
			t.Errorf("a junction to the folder overlaps it: got=%v err=%v", got, err)
		}
		got, err = pathsOverlap(filepath.Join(junction, "inner"), src)
		if err != nil || !got {
			t.Errorf("a folder below a junction to the source overlaps it: got=%v err=%v", got, err)
		}
	} else {
		t.Logf("mklink /J unavailable (%v: %s); junction rows skipped", err, out)
	}
}

// TestPathIdentityVolumeRoot: the root of a volume is recognised whatever the
// spelling, and a folder below it is not a root.
func TestPathIdentityVolumeRoot(t *testing.T) {
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	for _, s := range []string{sysDrive + `\`, `\\?\` + sysDrive + `\`, `\\.\` + sysDrive + `\`, strings.ToLower(sysDrive) + `/`} {
		id, err := pathIdentityOf(s)
		if err != nil {
			t.Fatalf("pathIdentityOf(%q): %v", s, err)
		}
		if !id.IsVolumeRoot() {
			t.Errorf("%q: isVolumeRoot = false (Final %q, Volume %q)", s, id.Final, id.Volume)
		}
	}
	id, err := pathIdentityOf(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if id.IsVolumeRoot() {
		t.Errorf("a temp folder is not a volume root (Final %q, Volume %q)", id.Final, id.Volume)
	}
}
