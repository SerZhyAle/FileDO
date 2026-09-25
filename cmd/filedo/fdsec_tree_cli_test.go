package main

// Acceptance tests for folder containers (SP-0009 D2). The first one is the
// exit criterion: a real folder packed and restored from a .lst batch line,
// because a verb that works interactively can silently not work from a batch
// file (AGENTS.md "Testing").

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// folderWorkdir builds a scratch directory holding one folder "Album" with an
// empty subfolder, an empty file, a multi-chunk file and depth.
func folderWorkdir(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	dir := t.TempDir()
	big := make([]byte, 3<<20+123) // crosses several 1 MiB chunks
	for i := range big {
		big[i] = byte(i*13) ^ 0x3c
	}
	items := map[string][]byte{
		"a.txt":            []byte("alpha"),
		"empty.txt":        {},
		"sub/b.txt":        []byte("beta"),
		"sub/deep/big.bin": big,
	}
	for p, c := range items {
		full := filepath.Join(dir, "Album", filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, c, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, "Album", "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, items
}

// assertFolder checks a restored folder against the items, including the
// empty subfolder.
func assertFolder(t *testing.T, root string, items map[string][]byte) {
	t.Helper()
	for p, want := range items {
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
		if err != nil {
			t.Fatalf("restored folder lacks %s: %v", p, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s is not byte-exact: %d bytes back, want %d", p, len(got), len(want))
		}
	}
	if fi, err := os.Stat(filepath.Join(root, "empty-dir")); err != nil || !fi.IsDir() {
		t.Fatalf("the empty subfolder did not round-trip: %v", err)
	}
	// The root, four files, and sub, sub/deep and empty-dir: nothing more.
	n := 0
	filepath.Walk(root, func(string, os.FileInfo, error) error { n++; return nil })
	if n != 8 {
		t.Fatalf("restored folder has %d entries including its root, want 8", n)
	}
}

func TestFdsecFolderRoundTripFromBatchFile(t *testing.T) {
	dir, items := folderWorkdir(t)
	const secret = "folder-from-the-batch-file"
	lst := strings.Join([]string{
		"Album secure p:" + secret,
		"fdsec info Album.fd-sec p:" + secret,
		"fdsec verify Album.fd-sec p:" + secret,
		"Album.fd-sec unsecure p:" + secret + " to Restored",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "work.lst"), []byte(lst), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, dir, "from", "work.lst")
	if code != 0 {
		t.Fatalf("batch run exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "4/4 commands succeeded") {
		t.Errorf("not every batch command succeeded\n%s", out)
	}
	if !strings.Contains(out, "holds:           a folder - 7 entries") {
		t.Errorf("fdsec info does not report the folder and its entry count\n%s", out)
	}
	if !strings.Contains(out, "entries: 7 (4 files, 3 folders)") {
		t.Errorf("fdsec verify does not report the folder's entries\n%s", out)
	}
	assertFolder(t, filepath.Join(dir, "Restored"), items)
	// The original is untouched - no del or wipe was asked for.
	assertFolder(t, filepath.Join(dir, "Album"), items)
	// One file, and nothing beside it: no sidecar, no leftovers (R3).
	des, _ := os.ReadDir(dir)
	for _, d := range des {
		switch d.Name() {
		case "Album", "Album.fd-sec", "Restored", "work.lst", "history.json", ".reveal-root":
		default:
			t.Errorf("unexpected %s beside the container", d.Name())
		}
	}
	if strings.Contains(out, secret) {
		t.Errorf("the credential was echoed\n%s", out)
	}
	assertNoSecretOnDisk(t, dir, secret, "work.lst")
	assertNoPartials(t, dir)
}

func TestFdsecFolderSameVerbsInteractively(t *testing.T) {
	dir, items := folderWorkdir(t)
	const secret = "folder-typed-by-hand"
	if out, code := run(t, dir, "Album", "secure", "p:"+secret); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	// Default destination: beside the container, under the folder's own
	// name - which exists, so -y restores under a suffix, never into it.
	out, code := run(t, dir, "Album.fd-sec", "unsecure", "p:"+secret)
	if code != 2 || !strings.Contains(out, "already exists") {
		t.Fatalf("an existing destination folder did not stop the restore off a terminal: exit %d\n%s", code, out)
	}
	if out, code := run(t, dir, "Album.fd-sec", "unsecure", "-y", "p:"+secret); code != 0 {
		t.Fatalf("unsecure -y exited %d\n%s", code, out)
	}
	assertFolder(t, filepath.Join(dir, "Album-1"), items)
	assertFolder(t, filepath.Join(dir, "Album"), items)

	// The documented type prefix reaches the same dispatch.
	if out, code := run(t, dir, "folder", "Album", "secure", "p:"+secret, "to", "prefixed.fd-sec"); code != 0 {
		t.Fatalf("folder <target> secure exited %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "prefixed.fd-sec")) {
		t.Error("the folder-prefixed form did not produce a container")
	}
	assertNoPartials(t, dir)
}

func TestFdsecFolderHistoryNeverNamesTheFolder(t *testing.T) {
	dir, _ := folderWorkdir(t)
	const secret = "pw-history"
	if out, code := run(t, dir, "Album", "secure", "p:"+secret, "rename"); code != 0 {
		t.Fatalf("secure rename exited %d\n%s", code, out)
	}
	var blob string
	des, _ := os.ReadDir(dir)
	for _, d := range des {
		if !d.IsDir() && !strings.Contains(d.Name(), ".") {
			blob = d.Name()
		}
	}
	if blob == "" {
		t.Fatal("rename did not produce a nameless container")
	}
	if err := os.Rename(filepath.Join(dir, "Album"), filepath.Join(dir, "moved-away")); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "history.json"))
	if out, code := run(t, dir, blob, "unsecure", "p:"+secret); code != 0 {
		t.Fatalf("unsecure of a nameless container exited %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "Album", "a.txt")) {
		t.Fatal("the sealed folder name was not restored")
	}
	if h, err := os.ReadFile(filepath.Join(dir, "history.json")); err == nil && bytes.Contains(h, []byte("Album")) {
		t.Errorf("history.json names the sealed folder\n%s", h)
	}
}

func TestFdsecFolderDelOnlyAfterTheReadBack(t *testing.T) {
	t.Run("del removes the verified tree", func(t *testing.T) {
		dir, items := folderWorkdir(t)
		if out, code := run(t, dir, "Album", "secure", "del", "-y", "p:pw"); code != 0 {
			t.Fatalf("secure del exited %d\n%s", code, out)
		}
		if exists(filepath.Join(dir, "Album")) {
			t.Fatal("the original folder survived del -y")
		}
		if out, code := run(t, dir, "Album.fd-sec", "unsecure", "p:pw"); code != 0 {
			t.Fatalf("unsecure exited %d\n%s", code, out)
		}
		assertFolder(t, filepath.Join(dir, "Album"), items)
	})
	t.Run("wipe says the caveat and removes the tree", func(t *testing.T) {
		dir, _ := folderWorkdir(t)
		out, code := run(t, dir, "Album", "secure", "wipe", "-y", "p:pw")
		if code != 0 {
			t.Fatalf("secure wipe exited %d\n%s", code, out)
		}
		if !strings.Contains(out, "does not guarantee erasure") {
			t.Errorf("the honest caveat is missing\n%s", out)
		}
		if exists(filepath.Join(dir, "Album")) {
			t.Fatal("the original folder survived wipe -y")
		}
	})
	t.Run("without -y off a terminal the tree is kept", func(t *testing.T) {
		dir, items := folderWorkdir(t)
		out, code := run(t, dir, "Album", "secure", "del", "p:pw")
		if code != 0 {
			t.Fatalf("secure del exited %d\n%s", code, out)
		}
		if !strings.Contains(out, "Original folder kept") {
			t.Errorf("the kept original was not reported\n%s", out)
		}
		assertFolder(t, filepath.Join(dir, "Album"), items)
	})
	t.Run("a failed pack keeps the tree even under -y", func(t *testing.T) {
		dir, items := folderWorkdir(t)
		out, code := run(t, dir, "Album", "secure", "del", "-y", "p:pw", "to", filepath.Join("no-such-parent", "x.fd-sec"))
		if code == 0 {
			t.Fatalf("a pack into a missing parent succeeded\n%s", out)
		}
		assertFolder(t, filepath.Join(dir, "Album"), items)
	})
	t.Run("the container is never written inside the folder it packs", func(t *testing.T) {
		dir, items := folderWorkdir(t)
		out, code := run(t, dir, "Album", "secure", "del", "-y", "p:pw", "to", filepath.Join("Album", "inside.fd-sec"))
		if code != 2 {
			t.Fatalf("exit %d, want 2 (usage)\n%s", code, out)
		}
		assertFolder(t, filepath.Join(dir, "Album"), items)
	})
}

func TestFdsecFolderRefusals(t *testing.T) {
	t.Run("reveal refuses a folder container and names unsecure", func(t *testing.T) {
		dir, _ := folderWorkdir(t)
		if out, code := run(t, dir, "Album", "secure", "p:pw"); code != 0 {
			t.Fatalf("secure exited %d\n%s", code, out)
		}
		out, code := run(t, dir, "Album.fd-sec", "reveal", "p:pw")
		if code != 2 || !strings.Contains(out, "unsecure") {
			t.Fatalf("reveal of a folder container: exit %d\n%s", code, out)
		}
		// Nothing was left in the sandbox root.
		if des, _ := os.ReadDir(revealRoot(dir)); len(des) != 0 {
			t.Errorf("reveal left %d entries in the sandbox root", len(des))
		}
	})
	t.Run("unsecure start refuses a folder container", func(t *testing.T) {
		dir, _ := folderWorkdir(t)
		run(t, dir, "Album", "secure", "p:pw")
		out, code := run(t, dir, "Album.fd-sec", "unsecure", "start", "p:pw")
		if code != 2 || !strings.Contains(out, "holds a folder") {
			t.Fatalf("unsecure start of a folder container: exit %d\n%s", code, out)
		}
	})
	t.Run("a wrong password is 3 and writes nothing", func(t *testing.T) {
		dir, _ := folderWorkdir(t)
		run(t, dir, "Album", "secure", "p:pw")
		out, code := run(t, dir, "Album.fd-sec", "unsecure", "p:not-it", "to", "Out")
		if code != 3 {
			t.Fatalf("exit %d, want 3\n%s", code, out)
		}
		if exists(filepath.Join(dir, "Out")) {
			t.Fatal("a wrong password left a restored folder")
		}
		assertNoPartials(t, dir)
	})
	t.Run("a tampered folder container is 3 and leaves no partial tree", func(t *testing.T) {
		dir, _ := folderWorkdir(t)
		run(t, dir, "Album", "secure", "p:pw")
		p := filepath.Join(dir, "Album.fd-sec")
		b := mustRead(t, p)
		b[len(b)-5000] ^= 0xff // inside the last file stream
		os.WriteFile(p, b, 0o644)
		out, code := run(t, dir, "Album.fd-sec", "unsecure", "p:pw", "to", "Out")
		if code != 3 {
			t.Fatalf("exit %d, want 3\n%s", code, out)
		}
		if exists(filepath.Join(dir, "Out")) {
			t.Fatal("a tampered container left a restored folder")
		}
		assertNoPartials(t, dir)
	})
	t.Run("a junction inside the folder refuses the whole folder", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("junctions are a Windows construct")
		}
		dir, _ := folderWorkdir(t)
		outside := filepath.Join(dir, "outside")
		os.Mkdir(outside, 0o755)
		j := filepath.Join(dir, "Album", "jn")
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, outside).CombinedOutput(); err != nil {
			t.Skipf("cannot create a junction here: %v %s", err, out)
		}
		out, code := run(t, dir, "Album", "secure", "del", "-y", "p:pw")
		if code == 0 || !strings.Contains(out, "reparse point") || !strings.Contains(out, "jn") {
			t.Fatalf("a folder holding a junction was packed: exit %d\n%s", code, out)
		}
		if exists(filepath.Join(dir, "Album.fd-sec")) || !exists(filepath.Join(dir, "Album", "a.txt")) {
			t.Fatal("a refused folder produced a container or lost its content")
		}
	})
	t.Run("a mask packs one container per matched folder", func(t *testing.T) {
		dir, _ := folderWorkdir(t)
		os.MkdirAll(filepath.Join(dir, "Album2", "x"), 0o755)
		out, code := run(t, dir, "Albu*", "secure", "p:pw")
		if code != 0 {
			t.Fatalf("mask secure exited %d\n%s", code, out)
		}
		if !exists(filepath.Join(dir, "Album.fd-sec")) || !exists(filepath.Join(dir, "Album2.fd-sec")) {
			t.Fatalf("a mask over two folders did not give two containers\n%s", out)
		}
	})
}
