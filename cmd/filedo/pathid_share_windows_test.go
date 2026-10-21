package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SP-0037 AUD-06-F1 and SP-0047 AUD-16-F2: a folder spelled through the local
// administrative share (`\\localhost\C$\..`) is the same place as its
// drive-letter spelling. The copy and compare overlap guards and the wipe's
// "inside a protected folder" rule decide on that, not on the spelling. The
// tests skip when the share is not reachable here (it needs an administrative
// token, or a share made by an administrator).

// asAdminShare spells dir through its drive's administrative share, or skips.
func asAdminShare(t *testing.T, dir string) string {
	t.Helper()
	vol := filepath.VolumeName(dir)
	if len(vol) != 2 || vol[1] != ':' {
		t.Skipf("%s is not on a lettered drive", dir)
	}
	share := `\\localhost\` + strings.ToUpper(vol[:1]) + `$` + dir[len(vol):]
	if _, err := os.Stat(share); err != nil {
		t.Skipf("the administrative share %s is not reachable: %v", share, err)
	}
	return share
}

func TestCopyIntoOwnSubfolderThroughAShareRefused(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "Data")
	writeFile(t, filepath.Join(src, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(src, "sub", "b.txt"), []byte("b"))
	shareSrc := asAdminShare(t, src)

	for _, args := range [][]string{
		{"folder", src, "copy", filepath.Join(shareSrc, "backup")},
		{"copy", src, filepath.Join(shareSrc, "sub", "deeper", "still")},
		{"fastcopy", src, filepath.Join(shareSrc, "backup")},
		{"folder", shareSrc, "copy", filepath.Join(src, "backup")},
	} {
		out, code := run(t, wd, args...)
		if code != 2 {
			t.Errorf("%v exited %d, want 2 (a copy into itself under another spelling)\n%s", args, code, out)
		}
		if exists(filepath.Join(src, "backup")) || exists(filepath.Join(src, "sub", "deeper")) {
			t.Fatalf("%v created a folder inside the source", args)
		}
	}
}

func TestCompareDeleteNestedThroughAShareRefused(t *testing.T) {
	wd := t.TempDir()
	x := filepath.Join(wd, "Photos")
	compareTree(t, x)
	shareSub := filepath.Join(asAdminShare(t, x), "sub")

	for _, args := range [][]string{
		{"cmp", x, shareSub, "del", "source", "--yes"},
		{"cmp", shareSub, x, "del", "target", "--yes"},
	} {
		out, code := run(t, wd, args...)
		if code != 2 {
			t.Errorf("%v exited %d, want 2 (CHK-01 under another spelling)\n%s", args, code, out)
		}
		assertTreeIntact(t, x)
	}
}

func TestWipeInsideAProtectedFolderThroughAShareIsDangerous(t *testing.T) {
	wd := t.TempDir()
	lad := filepath.Join(wd, "lad")
	protected := filepath.Join(lad, "FileDO", "sub")
	writeFile(t, filepath.Join(protected, "keep.txt"), []byte("keep"))
	shareTarget := filepath.Join(asAdminShare(t, protected))
	env := []string{"LOCALAPPDATA=" + lad}

	// The drive-letter spelling is the reference: dangerous, refused, exit 2.
	if out, code := runWithEnv(t, wd, env, "folder", protected, "wipe", "--force"); code != 2 || !exists(filepath.Join(protected, "keep.txt")) {
		t.Fatalf("the local spelling exited %d (want 2), file kept=%v\n%s", code, exists(filepath.Join(protected, "keep.txt")), out)
	}
	out, code := runWithEnv(t, wd, env, "folder", shareTarget, "wipe", "--force")
	if code != 2 {
		t.Errorf("wiping a folder inside FileDO's data folder through the share exited %d, want 2\n%s", code, out)
	}
	if !exists(filepath.Join(protected, "keep.txt")) {
		t.Errorf("the file inside the protected folder was wiped through the share\n%s", out)
	}
	if !strings.Contains(out, "DANGEROUS") {
		t.Errorf("the run does not call the target dangerous\n%s", out)
	}
}
