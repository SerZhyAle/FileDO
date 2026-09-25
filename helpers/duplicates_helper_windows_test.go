package helpers

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filedo/fileduplicates"
	"filedo/statedir"

	"golang.org/x/sys/windows"
)

// Every test works in t.TempDir(): `cd from list .. del` deletes files, so
// every one of them proves that the right files survive.

func setup(t *testing.T) string {
	t.Helper()
	t.Setenv(statedir.EnvOverride, t.TempDir())
	return t.TempDir()
}

func put(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func bytesOf(seed byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%253) ^ seed
	}
	return b
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// lst writes a FileDO-format list: one `# Group` block per group, the first
// entry of each marked as kept.
func lst(t *testing.T, path string, groups ...[]string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("# Duplicate files report\n\n")
	for i, g := range groups {
		sb.WriteString("# Group " + string(rune('1'+i)) + " (2 files, 0.01 MB each)\n")
		for j, p := range g {
			mark := " "
			if j == 0 {
				mark = "*"
			}
			sb.WriteString(mark + " " + p + "\n")
		}
		sb.WriteString("\n")
	}
	return put(t, path, []byte(sb.String()))
}

func setTimes(t *testing.T, path string, created, modified time.Time) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	c := windows.NsecToFiletime(created.UnixNano())
	m := windows.NsecToFiletime(modified.UnixNano())
	if err := windows.SetFileTime(h, &c, nil, &m); err != nil {
		t.Fatal(err)
	}
}

func run(listPath string, words ...string) error {
	return CheckDuplicatesFromFile(append([]string{"from", "list", listPath}, words...))
}

// DUP-01: two files of different content in one group are never deleted.
func TestFromList_RefusesContentMismatch(t *testing.T) {
	dir := setup(t)
	a := put(t, filepath.Join(dir, "a.bin"), bytes.Repeat([]byte("A"), 4096))
	b := put(t, filepath.Join(dir, "b.bin"), bytes.Repeat([]byte("B"), 4096))
	list := lst(t, filepath.Join(dir, "dups.lst"), []string{a, b})
	if err := run(list, "del", "-y"); err == nil {
		t.Fatal("a refused group must end the run with an error")
	}
	hashList := put(t, filepath.Join(dir, "dups.txt"), []byte("h1|"+a+"\nh1|"+b+"\n"))
	if err := run(hashList, "del", "-y"); err == nil {
		t.Fatal("hash format: a refused group must end the run with an error")
	}
	for _, p := range []string{a, b} {
		if !exists(p) {
			t.Fatalf("%s was deleted although its content differs from the kept copy", p)
		}
	}
}

// DUP-01: the only copy, named twice, is not a pair.
func TestFromList_SameFileTwiceIsNotDeleted(t *testing.T) {
	cases := map[string]func(t *testing.T, dir, c string) string{
		"same path, .lst": func(t *testing.T, dir, c string) string {
			return lst(t, filepath.Join(dir, "dups.lst"), []string{c, c})
		},
		"same path, hash format": func(t *testing.T, dir, c string) string {
			return put(t, filepath.Join(dir, "dups.txt"), []byte("h|"+c+"\nh|"+c+"\n"))
		},
		"case variant": func(t *testing.T, dir, c string) string {
			return lst(t, filepath.Join(dir, "dups.lst"), []string{strings.ToUpper(c), c})
		},
		"relative and absolute": func(t *testing.T, dir, c string) string {
			return lst(t, filepath.Join(dir, "dups.lst"), []string{"c.bin", c})
		},
		"hard link": func(t *testing.T, dir, c string) string {
			link := filepath.Join(dir, "link.bin")
			if err := os.Link(c, link); err != nil {
				t.Skipf("hard links not supported here: %v", err)
			}
			return lst(t, filepath.Join(dir, "dups.lst"), []string{link, c})
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			dir := setup(t)
			c := put(t, filepath.Join(dir, "c.bin"), bytesOf(7, 8192))
			list := mk(t, dir, c)
			err := run(list, "del", "-y")
			if err == nil {
				t.Error("a list whose only group is one file must end with an error")
			}
			if !exists(c) {
				t.Fatalf("the only copy was deleted (%v)", err)
			}
			if !bytes.Equal(mustRead(t, c), bytesOf(7, 8192)) {
				t.Fatal("the only copy changed")
			}
		})
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// DUP-01: a group whose other entries are gone leaves the last one alone.
func TestFromList_KeeperMissingSkipsGroup(t *testing.T) {
	dir := setup(t)
	keep := filepath.Join(dir, "keep.bin") // never written: already gone
	victim := put(t, filepath.Join(dir, "victim.bin"), bytesOf(3, 5000))
	list := lst(t, filepath.Join(dir, "dups.lst"), []string{keep, victim})
	if err := run(list, "del", "-y"); err == nil {
		t.Error("a list with no usable group must end with an error")
	}
	if !exists(victim) {
		t.Fatal("the last remaining copy was deleted")
	}
}

// DUP-02: `old` removes the older copy and keeps the newest - on the list path
// exactly as on the scan path - and so does the default.
func TestFromListDelOldKeepsNewest(t *testing.T) {
	base := time.Date(2024, 5, 1, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		words []string
		gone  string
	}{
		{[]string{"del", "old", "-y"}, "old.bin"},
		{[]string{"old", "del", "-y"}, "old.bin"},
		{[]string{"del", "-y"}, "old.bin"},
		{[]string{"-y", "new", "del"}, "new.bin"},
	} {
		dir := setup(t)
		older := put(t, filepath.Join(dir, "old.bin"), bytesOf(9, 7000))
		newer := put(t, filepath.Join(dir, "new.bin"), bytesOf(9, 7000))
		setTimes(t, older, base, base)
		setTimes(t, newer, base.Add(time.Hour), base.Add(time.Hour))
		list := lst(t, filepath.Join(dir, "dups.lst"), []string{older, newer})
		if err := run(list, c.words...); err != nil {
			t.Fatalf("%v: %v", c.words, err)
		}
		for _, p := range []string{older, newer} {
			wantGone := filepath.Base(p) == c.gone
			if exists(p) == wantGone {
				t.Errorf("%v: %s exists=%v, want gone=%v", c.words, filepath.Base(p), exists(p), wantGone)
			}
		}
	}
}

// DUP-02 / DUP-03: the list path never forces batch mode - with somebody to
// ask it asks, and with nobody to ask and no -y it refuses.
func TestFromList_AsksUnlessYes(t *testing.T) {
	dir := setup(t)
	a := put(t, filepath.Join(dir, "a.bin"), bytesOf(5, 3000))
	b := put(t, filepath.Join(dir, "b.bin"), bytesOf(5, 3000))
	list := lst(t, filepath.Join(dir, "dups.lst"), []string{a, b})

	err := run(list, "del")
	if !fileduplicates.IsUsageError(err) {
		t.Fatalf("no console and no -y: err = %v, want a usage error", err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a refused run deleted a file")
	}

	answer := func(in string) func(*fileduplicates.DuplicateOptions) {
		return func(o *fileduplicates.DuplicateOptions) {
			o.Interactive = true
			o.Input = strings.NewReader(in)
		}
	}
	if err := CheckDuplicatesFromFile([]string{"from", "list", list, "del", "old"}, answer("n\n")); err != nil {
		t.Fatal(err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("'n' at the prompt deleted a file")
	}
	if err := CheckDuplicatesFromFile([]string{"from", "list", list, "del", "old"}, answer("")); err != nil {
		t.Fatal(err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("end of input was read as yes")
	}
	if err := CheckDuplicatesFromFile([]string{"from", "list", list, "del", "old"}, answer("y\n")); err != nil {
		t.Fatal(err)
	}
	if exists(a) == exists(b) {
		t.Fatal("'y' at the prompt should delete exactly one copy")
	}
}

// DUP-01: relative entries resolve against the list's folder, never the
// current directory.
func TestFromList_RelativePathsResolveAgainstListDir(t *testing.T) {
	listDir := setup(t)
	cwd := t.TempDir()
	for _, d := range []string{listDir, cwd} {
		put(t, filepath.Join(d, "a.bin"), bytesOf(4, 6000))
		put(t, filepath.Join(d, "b.bin"), bytesOf(4, 6000))
	}
	list := lst(t, filepath.Join(listDir, "dups.lst"), []string{"a.bin", "b.bin"})
	t.Chdir(cwd)
	if err := run(list, "del", "-y"); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(cwd, "a.bin")) || !exists(filepath.Join(cwd, "b.bin")) {
		t.Fatal("a file in the current directory was deleted")
	}
	if exists(filepath.Join(listDir, "a.bin")) == exists(filepath.Join(listDir, "b.bin")) {
		t.Fatal("exactly one of the listed copies should be gone")
	}

	rooted := lst(t, filepath.Join(listDir, "rooted.lst"), []string{`\a.bin`, `C:b.bin`})
	if err := run(rooted, "del", "-y"); err == nil {
		t.Error("a list of paths that are neither absolute nor relative must yield no group")
	}
}

// DUP-01: a hand-edited list that lost its first header is refused whole.
func TestFromList_EntryOutsideGroupRefused(t *testing.T) {
	dir := setup(t)
	a := put(t, filepath.Join(dir, "a.bin"), bytesOf(6, 3000))
	b := put(t, filepath.Join(dir, "b.bin"), bytesOf(6, 3000))
	list := put(t, filepath.Join(dir, "dups.lst"), []byte("* "+a+"\n  "+b+"\n"))
	err := run(list, "del", "-y")
	if err == nil || !strings.Contains(err.Error(), "Group") {
		t.Fatalf("err = %v, want the missing header named", err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a file was deleted from a list that is not in FileDO's format")
	}
}

// DUP-01: a file listed in two groups - each group's keeper is the other's
// victim - never loses both copies.
func TestFromList_KeeperInTwoGroups(t *testing.T) {
	dir := setup(t)
	a := put(t, filepath.Join(dir, "a.bin"), bytesOf(8, 4000))
	b := put(t, filepath.Join(dir, "b.bin"), bytesOf(8, 4000))
	list := lst(t, filepath.Join(dir, "dups.lst"), []string{a, b}, []string{b, a})
	if err := run(list, "del", "-y"); err != nil {
		t.Fatal(err)
	}
	if !exists(a) && !exists(b) {
		t.Fatal("both copies are gone")
	}
	if exists(a) && exists(b) {
		t.Fatal("the merged group should have lost exactly one copy")
	}
}

// DUP-06: a listed file inside Program Files (a scratch stand-in, set for
// this process) needs a typed confirmation that -y does not give.
func TestFromList_SystemFolderNeedsConfirmation(t *testing.T) {
	dir := setup(t)
	t.Setenv("ProgramFiles", dir)
	a := put(t, filepath.Join(dir, "App", "a.dll"), bytesOf(1, 3000))
	b := put(t, filepath.Join(dir, "App", "b.dll"), bytesOf(1, 3000))
	list := lst(t, filepath.Join(t.TempDir(), "dups.lst"), []string{a, b})
	err := run(list, "del", "-y")
	if !fileduplicates.IsUsageError(err) || !strings.Contains(err.Error(), "Program Files") {
		t.Fatalf("err = %v, want a usage error naming Program Files", err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a file inside Program Files was deleted without a confirmation")
	}
}

// DUP-11: a missing list is an error, and `FROM LIST` is `from list`.
func TestFromList_ErrorsAndCase(t *testing.T) {
	dir := setup(t)
	if err := run(filepath.Join(dir, "missing.lst")); err == nil {
		t.Fatal("a missing list must be an error")
	}
	if err := CheckDuplicatesFromFile([]string{"from", "list"}); !fileduplicates.IsUsageError(err) {
		t.Fatalf("no list name: %v, want a usage error", err)
	}
	a := put(t, filepath.Join(dir, "a.bin"), bytesOf(2, 3000))
	b := put(t, filepath.Join(dir, "b.bin"), bytesOf(2, 3000))
	list := lst(t, filepath.Join(dir, "dups.lst"), []string{a, b})
	if err := CheckDuplicatesFromFile([]string{"FROM", "List", list}); err != nil {
		t.Fatalf("FROM List: %v", err)
	}
	empty := put(t, filepath.Join(dir, "empty.lst"), []byte("# Duplicate files report\n# Duplicate groups: 0\n"))
	if err := run(empty, "del", "-y"); err == nil {
		t.Fatal("a list with no group must be an error")
	}
	if err := run(list, "del", "dell"); !fileduplicates.IsUsageError(err) {
		t.Fatalf("a typo must be a usage error, got %v", err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a file was deleted")
	}
}
