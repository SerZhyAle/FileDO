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

	"golang.org/x/sys/windows"

	"filedo/fdsec"
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

// TestFdsecFolderRestoreFailureKeepsEntryNamesSealed is AUD-09-F1: an
// environmental failure while the tree is written (here: the new files are
// denied by an inherit-only ACE on the destination's parent, no elevation
// needed) must not put a sealed entry name into history.json or the event
// stream. The console may keep the full message.
func TestFdsecFolderRestoreFailureKeepsEntryNamesSealed(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the access list is a Windows construct")
	}
	dir := t.TempDir()
	const sealed = "TopSecretMerger"
	if err := os.MkdirAll(filepath.Join(dir, "Secret"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Secret", sealed+".txt"), []byte("merger terms"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "Secret", "secure", "p:pw"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	os.Remove(filepath.Join(dir, "history.json"))

	out := filepath.Join(dir, "Out")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := "*" + tu.User.Sid.String()
	if o, err := exec.Command("icacls", out, "/deny", sid+":(CI)(IO)(WD)").CombinedOutput(); err != nil {
		t.Skipf("cannot set the deny entry here: %v %s", err, o)
	}
	t.Cleanup(func() { exec.Command("icacls", out, "/remove:d", sid, "/T").Run() })

	events := filepath.Join(dir, "ev.jsonl")
	console, code := run(t, dir, "--events", events, "Secret.fd-sec", "unsecure", "p:pw", "to", filepath.Join("Out", "R"))
	if code == 0 {
		t.Fatalf("a restore whose files were denied succeeded\n%s", console)
	}
	for _, f := range []string{"ev.jsonl", "history.json"} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			t.Fatalf("%s was not written: %v", f, err)
		}
		if bytes.Contains(b, []byte(sealed)) {
			t.Errorf("%s carries the sealed entry name\n%s", f, b)
		}
	}
	if exists(filepath.Join(out, "R")) {
		t.Error("a failed restore left the destination folder")
	}
}

// TestFdsecFolderPackRefusesAContainerInsideItThroughAJunction is AUD-09-F6:
// "inside the folder it packs" is decided on the resolved location, so a
// junction spelling of the folder is refused like the direct one.
func TestFdsecFolderPackRefusesAContainerInsideItThroughAJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions are a Windows construct")
	}
	dir, items := folderWorkdir(t)
	j := filepath.Join(dir, "J")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, filepath.Join(dir, "Album")).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction here: %v %s", err, out)
	}
	out, code := run(t, dir, "Album", "secure", "-y", "p:pw", "to", filepath.Join("J", "Album.fd-sec"))
	if code != 2 {
		t.Fatalf("exit %d, want 2 (usage)\n%s", code, out)
	}
	if exists(filepath.Join(dir, "Album", "Album.fd-sec")) {
		t.Fatal("the container was written inside the folder it packs")
	}
	assertFolder(t, filepath.Join(dir, "Album"), items)
}

// TestFdsecFolderRestoreNeverReplacesANameThatAppeared is AUD-09-F5: a file
// created under the destination name after the last check is not replaced
// by the folder's move into place.
func TestFdsecFolderRestoreNeverReplacesANameThatAppeared(t *testing.T) {
	saved := globalInterruptHandler
	globalInterruptHandler = newInterruptHandlerNoSignals()
	defer func() { globalInterruptHandler = saved }()

	dir, _ := folderWorkdir(t)
	if out, code := run(t, dir, "Album", "secure", "p:pw"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	dest := filepath.Join(dir, "Restored")
	precious := []byte("created in the check-to-rename window")
	fdsecBeforeTreeRename = func(p string) { os.WriteFile(p, precious, 0o644) }
	defer func() { fdsecBeforeTreeRename = nil }()

	containerPath := filepath.Join(dir, "Album.fd-sec")
	src, err := os.Open(containerPath)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	fi, _ := src.Stat()
	c, err := fdsec.Open(src, fdsec.NewCredential("pw"))
	if err != nil {
		t.Fatal(err)
	}
	hl := NewHistoryLogger(nil)
	hl.enabled = false
	err = fdsecUnsecureTree(containerPath, c, src, fi.Size(), &fdsecOpts{haveTo: true, to: dest, assumeYes: true}, hl)
	if err == nil {
		t.Fatal("the restore reported success over a name that appeared")
	}
	if got, rerr := os.ReadFile(dest); rerr != nil || !bytes.Equal(got, precious) {
		t.Fatalf("the file that appeared was replaced or lost: %v", rerr)
	}
	assertNoPartials(t, dir)
}

// TestFdsecFolderWithCaseOnlyTwinsIsNotSecured is AUD-21-F1: a folder from a
// case-sensitive directory holding "Readme.txt" and "README.txt" used to pack,
// read back and have its originals deleted by "secure del -y", after which no
// "unsecure" could restore it. It is refused up front with both originals kept.
func TestFdsecFolderWithCaseOnlyTwinsIsNotSecured(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the per-directory case-sensitive flag is a Windows construct")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "Twins")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("fsutil", "file", "setCaseSensitiveInfo", src, "enable").CombinedOutput(); err != nil {
		t.Skipf("cannot make a case-sensitive folder here: %v %s", err, out)
	}
	lower, upper := filepath.Join(src, "Readme.txt"), filepath.Join(src, "README.txt")
	os.WriteFile(lower, []byte("lower"), 0o644)
	os.WriteFile(upper, []byte("upper"), 0o644)
	if des, _ := os.ReadDir(src); len(des) != 2 {
		t.Skip("the folder did not keep both case twins")
	}
	out, code := run(t, dir, "Twins", "secure", "del", "-y", "p:pw")
	if code == 0 {
		t.Fatalf("a folder with case-only twins was secured\n%s", out)
	}
	if exists(filepath.Join(dir, "Twins.fd-sec")) {
		t.Fatal("a refused folder left a container")
	}
	if a, b := mustRead(t, lower), mustRead(t, upper); string(a) != "lower" || string(b) != "upper" {
		t.Fatal("a refused folder lost an original")
	}
	assertNoPartials(t, dir)
}
