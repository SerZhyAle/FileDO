package main

// AUD-08-F1 (SP-0039): check never opens an online-only file. A OneDrive
// placeholder reports its logical size, and reading it makes the provider
// download it; a slow download was judged damage and persisted. The owner's
// decision: such a file - FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS,
// FILE_ATTRIBUTE_RECALL_ON_OPEN or FILE_ATTRIBUTE_OFFLINE, read from the
// listing and never by opening - is "not read (online-only)", makes the run
// could-not-verify (exit 2 when it is the only reason; a defect still wins),
// and is never recorded as damage. The recall bits cannot be set without a
// Cloud Files provider; FILE_ATTRIBUTE_OFFLINE can, and takes the same path.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// markOffline sets FILE_ATTRIBUTE_OFFLINE on p, or skips.
func markOffline(t *testing.T, p string) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetFileAttributes(name, attrs|windows.FILE_ATTRIBUTE_OFFLINE); err != nil {
		t.Skipf("cannot set FILE_ATTRIBUTE_OFFLINE here: %v", err)
	}
	// Judged here from the raw attributes, not by isOnlineOnly: a broken
	// isOnlineOnly must fail the tests below, not skip them.
	if got, err := windows.GetFileAttributes(name); err != nil || got&windows.FILE_ATTRIBUTE_OFFLINE == 0 {
		t.Skipf("the file system did not keep FILE_ATTRIBUTE_OFFLINE (attrs %#x, %v)", got, err)
	}
}

// holdExclusive opens p with no sharing for the rest of the test: any open
// by check would fail with a sharing violation and show as "not-read=1"
// (locked), so "not-read=0" proves check never opened the file.
func holdExclusive(t *testing.T, p string) {
	t.Helper()
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatalf("cannot hold %s: %v", p, err)
	}
	t.Cleanup(func() { windows.CloseHandle(h) })
}

// damagedListNames reports whether the damaged list in wd mentions base.
func damagedListNames(t *testing.T, wd, base string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(wd, checkDamagedName))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(b)), strings.ToLower(base))
}

func TestCheckOnlineOnlyFileIsNotRead(t *testing.T) {
	root := checkTree(t, 3)
	cloud := filepath.Join(root, "d00", "online-only.jpg")
	writeFile(t, cloud, patternBytes(4096, 9))
	markOffline(t, cloud)
	holdExclusive(t, cloud)

	for _, extra := range [][]string{{"--no-precount"}, {"--precount"}} {
		wd := t.TempDir()
		args := append([]string{"check", root}, extra...)
		out, code := run(t, wd, args...)
		if code != 2 {
			t.Errorf("check %v over an online-only file exited %d, want 2 (not proven)\n%s", extra, code, out)
		}
		if !strings.Contains(out, "found=4, checked=3, damaged(new)=0, damaged(known, not re-read)=0, not-read=0, not-read(online-only)=1") {
			t.Errorf("check %v: the online-only file was opened or miscounted\n%s", extra, out)
		}
		if !strings.Contains(out, "online-only") || strings.Contains(out, "Passed") {
			t.Errorf("check %v: the verdict does not say why nothing was proven\n%s", extra, out)
		}
		if damagedListNames(t, wd, "online-only.jpg") {
			t.Errorf("check %v recorded the online-only file as damaged", extra)
		}
	}
}

func TestCheckSingleOnlineOnlyFileIsNotRead(t *testing.T) {
	dir := t.TempDir()
	cloud := filepath.Join(dir, "movie.mkv")
	writeFile(t, cloud, patternBytes(4096, 3))
	markOffline(t, cloud)
	holdExclusive(t, cloud)

	wd := t.TempDir()
	out, code := run(t, wd, "check", cloud)
	if code != 2 {
		t.Errorf("check <one online-only file> exited %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "is online-only") || !strings.Contains(out, "checked=0") || !strings.Contains(out, "not-read=0,") {
		t.Errorf("the single-file verdict does not say the file was left unread\n%s", out)
	}
	if damagedListNames(t, wd, "movie.mkv") {
		t.Error("an explicit check of an online-only file recorded it as damaged")
	}
}

// A found defect still wins over an online-only file: the run is Failed,
// exit 1, not Not proven.
func TestCheckOnlineOnlyDoesNotHideDefect(t *testing.T) {
	root := checkTree(t, 2)
	cloud := filepath.Join(root, "d00", "online-only.jpg")
	writeFile(t, cloud, patternBytes(4096, 9))
	markOffline(t, cloud)

	wd := t.TempDir()
	bad := filepath.Join(root, "d01", "f0001.bin")
	l, err := loadStateList(filepath.Join(wd, checkDamagedName))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.AddInfo(bad, info); err != nil {
		t.Fatal(err)
	}
	l.Close()

	out, code := run(t, wd, "check", root, "--no-precount")
	if code != 1 {
		t.Errorf("a known damaged file next to an online-only one exited %d, want 1\n%s", code, out)
	}
	if !strings.Contains(out, "damaged(known, not re-read)=1") || !strings.Contains(out, "not-read(online-only)=1") {
		t.Errorf("the counts are wrong\n%s", out)
	}
}
