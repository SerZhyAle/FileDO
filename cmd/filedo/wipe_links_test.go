package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SP-0034 AUD-03-F1: an overwrite in place writes into the data stream, and
// every hard link to the file shares that stream. The other names would keep
// existing and hold random bytes, so a file with more than one name is never
// overwritten - by `secure wipe`, by the folder form of it, or by `file wipe`.

func linkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Link(oldname, newname); err != nil {
		t.Skipf("hard links are unavailable here: %v", err)
	}
}

func TestSecureWipeKeepsAnOriginalThatHasAnotherHardLink(t *testing.T) {
	dir, payload := workdir(t)
	linkOrSkip(t, filepath.Join(dir, "plain.txt"), filepath.Join(dir, "twin.txt"))

	out, code := run(t, dir, "plain.txt", "secure", "wipe", "-y", "p:pw-links")
	if code != 0 {
		t.Fatalf("secure wipe -y exited %d, want 0 (the original is kept, the container is complete)\n%s", code, out)
	}
	if !strings.Contains(out, "Original kept") || !strings.Contains(out, "hard link") {
		t.Errorf("the run does not say the original was kept because of its hard links\n%s", out)
	}
	if got := mustRead(t, filepath.Join(dir, "twin.txt")); !bytes.Equal(got, payload) {
		t.Error("the other hard link no longer holds the original bytes - the wipe reached it")
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Error("the named original was overwritten although it has another hard link")
	}
	if !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Error("the container is missing")
	}
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "to", "restored.txt", "p:pw-links"); code != 0 {
		t.Fatalf("unsecure of the container exited %d\n%s", code, out)
	}
}

func TestSecureWipeOfAFolderKeepsItWhenAFileHasAnotherHardLink(t *testing.T) {
	dir, items := folderWorkdir(t)
	outside := filepath.Join(dir, "outside.txt")
	linkOrSkip(t, filepath.Join(dir, "Album", "a.txt"), outside)

	out, code := run(t, dir, "Album", "secure", "wipe", "-y", "p:pw-links")
	if code != 0 {
		t.Fatalf("secure wipe -y of the folder exited %d, want 0 (the folder is kept)\n%s", code, out)
	}
	if !strings.Contains(out, "Original folder kept") || !strings.Contains(out, "hard link") {
		t.Errorf("the run does not say the folder was kept because of a hard link\n%s", out)
	}
	if got := mustRead(t, outside); string(got) != "alpha" {
		t.Errorf("the hard link outside the folder holds %q, want %q - the wipe reached it", got, "alpha")
	}
	assertFolder(t, filepath.Join(dir, "Album"), items)
}

func TestFileWipeRefusesAFileThatHasAnotherHardLink(t *testing.T) {
	dir, payload := workdir(t)
	linkOrSkip(t, filepath.Join(dir, "plain.txt"), filepath.Join(dir, "twin.txt"))

	out, code := run(t, dir, "file", "plain.txt", "wipe", "--force")
	if code == 0 {
		t.Fatalf("file wipe --force of a file with another hard link exited 0\n%s", out)
	}
	if !strings.Contains(out, "hard link") {
		t.Errorf("the refusal does not name the hard links\n%s", out)
	}
	for _, name := range []string{"plain.txt", "twin.txt"} {
		if got := mustRead(t, filepath.Join(dir, name)); !bytes.Equal(got, payload) {
			t.Errorf("%s was overwritten by a refused wipe", name)
		}
	}
}

func TestWipeFileInPlaceGuardsTheHandleItOverwrites(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	writeFile(t, a, []byte("original bytes"))
	b := filepath.Join(dir, "b.bin")
	linkOrSkip(t, a, b)

	err := wipeFileInPlace(a)
	var hle *hardLinkedError
	if !errors.As(err, &hle) || hle.Links != 2 {
		t.Fatalf("wipeFileInPlace of a file with two names returned %v, want a hardLinkedError with 2 links", err)
	}
	if got := mustRead(t, b); string(got) != "original bytes" {
		t.Errorf("the other name holds %q after the refusal", got)
	}
	// One name only: the ordinary wipe still works.
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if err := wipeFileInPlace(a); err != nil {
		t.Fatalf("wipeFileInPlace of a file with one name: %v", err)
	}
	if exists(a) {
		t.Error("the wiped file is still there")
	}
}
