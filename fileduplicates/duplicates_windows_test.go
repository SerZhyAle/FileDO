package fileduplicates

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filedo/fsx"
	"filedo/statedir"

	"golang.org/x/sys/windows"
)

// Every test here works in t.TempDir() and keeps the hash cache in a state
// root of its own, so nothing lands in the developer's profile.

func useStateDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv(statedir.EnvOverride, d)
	return d
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func payload(seed byte, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%251) ^ seed
	}
	return b
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// setFileTimes sets the creation and modification time of path.
func setFileTimes(t *testing.T, path string, created, modified time.Time) {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
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

// opts parses words the way the CLI does, quiet, with nobody to ask.
func opts(words ...string) DuplicateOptions {
	o := ParseArguments(append([]string{"quiet"}, words...))
	return o
}

var base = time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)

// threeCopies writes the same bytes three times with creation times one hour
// apart (a oldest, c newest) and one shared modification time - the shape an
// Explorer copy leaves behind.
func threeCopies(t *testing.T, dir string) (a, b, c string) {
	t.Helper()
	data := payload(0x11, 64*1024)
	a, b, c = filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin"), filepath.Join(dir, "c.bin")
	for i, p := range []string{a, b, c} {
		writeFile(t, p, data)
		setFileTimes(t, p, base.Add(time.Duration(i)*time.Hour), base)
	}
	return a, b, c
}

// DUP-16
func TestFindDuplicates_MissingRootErrors(t *testing.T) {
	useStateDir(t)
	missing := filepath.Join(t.TempDir(), "no-such-folder")
	_, err := FindDuplicates(missing, opts())
	if err == nil {
		t.Fatal("a missing root must be an error, not 'no duplicate files found'")
	}
	file := filepath.Join(t.TempDir(), "file.bin")
	writeFile(t, file, payload(1, 100))
	if _, err := FindDuplicates(file, opts()); err == nil {
		t.Fatal("a file as root must be an error")
	}
}

// DUP-03
func TestFindDuplicates_DeleteWithoutYesAndNoConsoleIsRefused(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a, b, c := threeCopies(t, dir)
	for _, words := range [][]string{{"del"}, {"new", "del"}, {"del", "xyz"}, {"move", filepath.Join(dir, "out")}} {
		_, err := FindDuplicates(dir, opts(words...))
		if !IsUsageError(err) {
			t.Errorf("%v: err = %v, want a usage error", words, err)
		}
	}
	for _, p := range []string{a, b, c} {
		if !exists(p) {
			t.Errorf("%s was deleted by a refused run", p)
		}
	}
	if exists(filepath.Join(dir, "out")) {
		t.Error("a refused move created its target folder")
	}
}

// DUP-09: the default keeps the newest copy by creation time, `new` the
// oldest; the modification times all tie.
func TestSelection_NewestByCreationTime(t *testing.T) {
	useStateDir(t)
	for _, c := range []struct {
		rule string
		keep string
	}{{"", "c.bin"}, {"old", "c.bin"}, {"new", "a.bin"}, {"xyz", "a.bin"}, {"abc", "c.bin"}} {
		dir := t.TempDir()
		threeCopies(t, dir)
		words := []string{"del", "-y"}
		if c.rule != "" {
			words = append(words, c.rule)
		}
		if _, err := FindDuplicates(dir, opts(words...)); err != nil {
			t.Fatalf("rule %q: %v", c.rule, err)
		}
		entries, _ := os.ReadDir(dir)
		var left []string
		for _, e := range entries {
			left = append(left, e.Name())
		}
		if len(left) != 1 || left[0] != c.keep {
			t.Errorf("rule %q: left %v, want only %s", c.rule, left, c.keep)
		}
	}

	// A tie on creation time breaks on the path, the same way every run.
	dir := t.TempDir()
	data := payload(0x22, 8192)
	for _, n := range []string{"z.bin", "m.bin", "b.bin"} {
		writeFile(t, filepath.Join(dir, n), data)
		setFileTimes(t, filepath.Join(dir, n), base, base)
	}
	if _, err := FindDuplicates(dir, opts("del", "-y")); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "b.bin")) || exists(filepath.Join(dir, "m.bin")) || exists(filepath.Join(dir, "z.bin")) {
		t.Error("a creation-time tie must keep the first path")
	}
}

// DUP-06: two names of one file are not duplicates of each other.
func TestFindDuplicates_HardlinksNotGrouped(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a := filepath.Join(dir, "a.bin")
	writeFile(t, a, payload(0x33, 1<<20))
	if err := os.Link(a, filepath.Join(dir, "a-link.bin")); err != nil {
		t.Skipf("hard links not supported here: %v", err)
	}
	res, err := FindDuplicates(dir, opts())
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicateGroups != 0 || res.DuplicateSize != 0 {
		t.Fatalf("a hard link formed %d group(s), %d bytes 'wasted'", res.DuplicateGroups, res.DuplicateSize)
	}

	// With a real copy beside them the group is two files, not three.
	writeFile(t, filepath.Join(dir, "copy.bin"), payload(0x33, 1<<20))
	res, err = FindDuplicates(dir, opts())
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicateGroups != 1 || res.DuplicateFiles != 1 {
		t.Fatalf("got %d groups / %d duplicates, want 1 / 1", res.DuplicateGroups, res.DuplicateFiles)
	}
}

// DUP-05: a file rewritten with the same length and its modification time
// put back must not be matched from the cache - and nothing is deleted.
func TestDelete_StaleCacheSameSizeSameMtime(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	writeFile(t, a, payload(0x44, 100000))
	writeFile(t, b, payload(0x44, 100000))
	res, err := FindDuplicates(dir, opts())
	if err != nil || res.DuplicateGroups != 1 {
		t.Fatalf("first scan: %v, %d groups", err, res.DuplicateGroups)
	}

	info, _ := os.Stat(b)
	mtime := info.ModTime()
	writeFile(t, b, payload(0x45, 100000)) // same length, other bytes
	if err := os.Chtimes(b, mtime, mtime); err != nil {
		t.Fatal(err)
	}

	res, err = FindDuplicates(dir, opts("del", "-y"))
	if err != nil {
		t.Fatalf("second scan: %v", err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("unique content was deleted on a stale cached hash")
	}
	if !bytes.Equal(mustRead(t, b), payload(0x45, 100000)) {
		t.Fatal("b changed")
	}
	if res.DuplicateGroups != 0 {
		t.Fatalf("stale cache still grouped the files (%d groups)", res.DuplicateGroups)
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

// DUP-05 / DUP-15: even when the cache is wrong in every field it checks - a
// forged entry, a filesystem without a change time, or two files whose hashes
// collide - the byte comparison before the delete refuses.
func TestDelete_ForgedCacheIsCaughtByByteCompare(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	writeFile(t, a, payload(0x55, 50000))
	writeFile(t, b, payload(0x55, 50000))
	if _, err := FindDuplicates(dir, opts()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, b, payload(0x56, 50000))

	cache, err := LoadHashCache()
	if err != nil {
		t.Fatal(err)
	}
	fa, fb := forgeIdentity(t, a), forgeIdentity(t, b)
	ea, ok := cache.Entries[fa.Path]
	if !ok {
		t.Fatalf("no cache entry for %s", fa.Path)
	}
	forged := ea
	forged.Path, forged.Size, forged.ModTime = fb.Path, fb.Size, fb.ModTime
	forged.VolSerial, forged.FileIndex, forged.ChangeTime = fb.VolSerial, fb.FileIndex, fb.ChangeTime
	forged.LastSeen = time.Now().Add(time.Minute)
	cache.Entries[fb.Path] = forged
	if err := cache.Save(); err != nil {
		t.Fatal(err)
	}

	res, err := FindDuplicates(dir, opts("del", "-y"))
	if res.DuplicateGroups != 1 {
		t.Fatalf("the forged cache should have grouped the files, got %d groups", res.DuplicateGroups)
	}
	if err == nil || IsUsageError(err) {
		t.Fatalf("a refused delete must end the run with an error, got %v", err)
	}
	if res.Actions.Refused != 1 || res.Actions.Deleted != 0 {
		t.Fatalf("actions %+v, want 1 refused, 0 deleted", res.Actions)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a file was deleted on a hash the bytes contradict")
	}
}

func forgeIdentity(t *testing.T, p string) DuplicateFileInfo {
	t.Helper()
	f, err := GetFileInfo(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := identify(&f); err != nil {
		t.Fatal(err)
	}
	return f
}

// DUP-15: two different files claiming one hash (a collision) are not
// deleted - the proof before a delete is the bytes, not the digest.
func TestDelete_HashCollisionNotDeleted(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	writeFile(t, a, payload(0x66, 4096))
	writeFile(t, b, payload(0x67, 4096))
	group := []DuplicateFileInfo{{Path: a, Size: 4096, FullHash: "same"}, {Path: b, Size: 4096, FullHash: "same"}}
	o := opts("del", "-y", "xyz")
	sum, err := ProcessDuplicateGroups([][]DuplicateFileInfo{group}, o)
	if err == nil || sum.Refused != 1 || sum.Deleted != 0 {
		t.Fatalf("sum %+v err %v, want one refusal", sum, err)
	}
	if !exists(a) || !exists(b) {
		t.Fatal("a colliding file was deleted")
	}
}

// DUP-01: the keeper is re-checked immediately before each deletion.
func TestProcessDuplicateGroups_KeeperMissingSkipsGroup(t *testing.T) {
	dir := t.TempDir()
	a, b, c := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin"), filepath.Join(dir, "c.bin")
	for _, p := range []string{b, c} {
		writeFile(t, p, payload(0x77, 4096))
	}
	// a (the keeper under xyz) is gone by the time the group is processed.
	group := []DuplicateFileInfo{{Path: a, Size: 4096}, {Path: b, Size: 4096}, {Path: c, Size: 4096}}
	sum, err := ProcessDuplicateGroups([][]DuplicateFileInfo{group}, opts("del", "-y", "xyz"))
	if err == nil || sum.Deleted != 0 || sum.Refused != 2 {
		t.Fatalf("sum %+v err %v, want the group skipped (2 refused)", sum, err)
	}
	if !exists(b) || !exists(c) {
		t.Fatal("a copy was deleted although its keeper is gone")
	}
}

// DUP-04: a move never replaces a file already in the target folder.
func TestMove_NeverOverwritesExisting(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	target := t.TempDir()
	a, b, c := threeCopies(t, dir)
	existing := payload(0x99, 1234)
	writeFile(t, filepath.Join(target, "a.bin"), existing)
	writeFile(t, filepath.Join(target, "b.bin"), existing)

	res, err := FindDuplicates(dir, opts("move", target, "-y"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions.Moved != 2 {
		t.Fatalf("moved %d, want 2", res.Actions.Moved)
	}
	if !exists(c) || exists(a) || exists(b) {
		t.Fatal("the newest copy should stay and the two older ones move")
	}
	for _, n := range []string{"a.bin", "b.bin"} {
		if !bytes.Equal(mustRead(t, filepath.Join(target, n)), existing) {
			t.Errorf("%s in the target was overwritten", n)
		}
	}
	for _, n := range []string{"a_(1).bin", "b_(1).bin"} {
		if !bytes.Equal(mustRead(t, filepath.Join(target, n)), payload(0x11, 64*1024)) {
			t.Errorf("%s does not hold the moved bytes", n)
		}
	}
}

// DUP-04: the cross-volume path (copy, verify, then delete) - forced on the
// one volume a test has.
func TestMove_CopyPathVerifiesAndRemoves(t *testing.T) {
	useStateDir(t)
	forceCopyMove = true
	defer func() { forceCopyMove = false }()
	dir := t.TempDir()
	target := t.TempDir()
	a, b, c := threeCopies(t, dir)
	res, err := FindDuplicates(dir, opts("move", target, "-y"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions.Moved != 2 || exists(a) || exists(b) || !exists(c) {
		t.Fatalf("actions %+v; a=%v b=%v c=%v", res.Actions, exists(a), exists(b), exists(c))
	}
	for _, n := range []string{"a.bin", "b.bin"} {
		if !bytes.Equal(mustRead(t, filepath.Join(target, n)), payload(0x11, 64*1024)) {
			t.Errorf("%s does not hold the moved bytes", n)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(target, "*"+fsx.PartialSuffix))
	if len(matches) != 0 {
		t.Errorf("partial files left behind: %v", matches)
	}
}

// DUP-04: a real second volume. Set FILEDO_TEST_SECOND_VOLUME_DIR to a
// scratch folder on another volume to run it; it creates and removes one
// folder of its own there.
func TestMove_CrossVolume(t *testing.T) {
	other := os.Getenv("FILEDO_TEST_SECOND_VOLUME_DIR")
	if other == "" {
		t.Skip("set FILEDO_TEST_SECOND_VOLUME_DIR to a scratch folder on another volume")
	}
	useStateDir(t)
	target, err := os.MkdirTemp(other, "filedo-dup-move-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(target)
	dir := t.TempDir()
	a, b, c := threeCopies(t, dir)
	res, err := FindDuplicates(dir, opts("move", target, "-y"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions.Moved != 2 || exists(a) || exists(b) || !exists(c) {
		t.Fatalf("actions %+v", res.Actions)
	}
}

// DUP-07: a stop after the first deletion leaves every other file in place.
func TestStop_AfterFirstDeletionLeavesTheRest(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	data := payload(0x12, 32*1024)
	var paths []string
	for i, n := range []string{"a.bin", "b.bin", "c.bin", "d.bin"} {
		p := filepath.Join(dir, n)
		writeFile(t, p, data)
		setFileTimes(t, p, base.Add(time.Duration(i)*time.Hour), base)
		paths = append(paths, p)
	}
	o := opts("del", "-y")
	o.Stop = func() bool {
		for _, p := range paths {
			if !exists(p) {
				return true
			}
		}
		return false
	}
	res, err := FindDuplicates(dir, o)
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("err = %v, want ErrStopped", err)
	}
	if res.Actions.Deleted != 1 {
		t.Fatalf("deleted %d, want exactly 1", res.Actions.Deleted)
	}
	left := 0
	for _, p := range paths {
		if exists(p) {
			left++
		}
	}
	if left != 3 {
		t.Fatalf("%d files left, want 3", left)
	}
	if !strings.Contains(err.Error(), "1 deleted") {
		t.Errorf("the stop does not say what was already done: %v", err)
	}
}

func TestStop_BeforeTheScanTouchesNothing(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a, b, c := threeCopies(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := opts("del", "-y")
	o.Context = ctx
	_, err := FindDuplicates(dir, o)
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("err = %v, want ErrStopped", err)
	}
	if !exists(a) || !exists(b) || !exists(c) {
		t.Fatal("a stopped run deleted a file")
	}
}

// DUP-08
func TestListWrittenWhenQuiet(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	threeCopies(t, dir)
	list := filepath.Join(t.TempDir(), "dups.lst")
	o := opts("list", list)
	if o.Verbose {
		t.Fatal("opts should be quiet")
	}
	if _, err := FindDuplicates(dir, o); err != nil {
		t.Fatal(err)
	}
	text := string(mustRead(t, list))
	root, _ := filepath.Abs(dir)
	if !strings.Contains(text, "# Root path: "+root+"\n") {
		t.Errorf("the report does not name the scanned root:\n%s", text)
	}
	if !strings.Contains(text, "# Group 1 (3 files") || !strings.Contains(text, "* "+filepath.Join(root, "c.bin")) {
		t.Errorf("the report does not list the group with its keeper:\n%s", text)
	}
}

func TestListTruncatedWhenNoDuplicates(t *testing.T) {
	useStateDir(t)
	for _, shape := range []string{"no-same-size", "same-size-different-bytes"} {
		dir := t.TempDir()
		writeFile(t, filepath.Join(dir, "a.bin"), payload(1, 5000))
		if shape == "no-same-size" {
			writeFile(t, filepath.Join(dir, "b.bin"), payload(1, 6000))
		} else {
			writeFile(t, filepath.Join(dir, "b.bin"), payload(2, 5000))
		}
		list := filepath.Join(t.TempDir(), "dups.lst")
		writeFile(t, list, []byte("# Group 1 (2 files, 0.01 MB each)\n* C:\\old\\a.bin\n  C:\\old\\b.bin\n"))
		res, err := FindDuplicates(dir, opts("list", list))
		if err != nil || res.DuplicateGroups != 0 {
			t.Fatalf("%s: %v, %d groups", shape, err, res.DuplicateGroups)
		}
		text := string(mustRead(t, list))
		if strings.Contains(text, "# Group 1") || !strings.Contains(text, "# Duplicate groups: 0") {
			t.Errorf("%s: the old list survived a clean rescan:\n%s", shape, text)
		}
	}
}

// DUP-13: two saves at once - the file is valid JSON and holds both.
func TestHashCache_ConcurrentSavesProduceValidJSON(t *testing.T) {
	useStateDir(t)
	mk := func(prefix string, n int) *HashCache {
		c := &HashCache{Entries: make(map[string]CacheEntry), loadedAt: time.Now()}
		for i := 0; i < n; i++ {
			key := filepath.Join(`C:\filedo-test-nonexistent`, prefix, strings.Repeat("x", i%7)+string(rune('a'+i%26)))
			key += "-" + time.Now().Format("150405.000000000") + "-" + string(rune('0'+i%10))
			c.Entries[key] = CacheEntry{Path: key, Size: int64(i), LastSeen: time.Now().Add(time.Minute), Algo: hashAlgo, FullHash: prefix}
		}
		return c
	}
	for round := 0; round < 5; round++ {
		c1, c2 := mk("one", 300), mk("two", 300)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, c := range []*HashCache{c1, c2} {
			wg.Add(1)
			go func(i int, c *HashCache) {
				defer wg.Done()
				errs[i] = c.Save()
			}(i, c)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("round %d: %v", round, err)
			}
		}
		path, _ := GetHashCachePath()
		var entries map[string]CacheEntry
		if err := json.Unmarshal(mustRead(t, path), &entries); err != nil {
			t.Fatalf("round %d: the cache is not valid JSON: %v", round, err)
		}
		for _, c := range []*HashCache{c1, c2} {
			for k := range c.Entries {
				if _, ok := entries[k]; !ok {
					t.Fatalf("round %d: entry %s lost", round, k)
				}
			}
		}
		leftovers, _ := filepath.Glob(path + ".tmp-*")
		if len(leftovers) != 0 {
			t.Fatalf("temporary files left: %v", leftovers)
		}
	}
}

// DUP-12: the cache lives under %LOCALAPPDATA%\FileDO\state, not beside the
// executable.
func TestGetHashCachePath_UnderLocalAppData(t *testing.T) {
	appData := t.TempDir()
	t.Setenv(statedir.EnvOverride, "")
	t.Setenv("LOCALAPPDATA", appData)
	p, err := GetHashCachePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(appData, "FileDO", "state", HASH_CACHE_FILE)
	if !strings.EqualFold(p, want) {
		t.Fatalf("GetHashCachePath() = %s, want %s", p, want)
	}
}

// DUP-06: the places a delete or move needs a person to confirm.
func TestClassifyProtected(t *testing.T) {
	scratch := t.TempDir()
	vol, err := fsx.VolumePathName(scratch)
	if err != nil {
		t.Fatal(err)
	}
	if ok, reason := classifyProtected(vol); !ok || reason != "the root of a drive" {
		t.Errorf("%s: %v %q, want the root of a drive", vol, ok, reason)
	}
	if sys := os.Getenv("SystemRoot"); sys != "" {
		if ok, reason := classifyProtected(filepath.Join(sys, "System32")); !ok || reason != "the Windows folder" {
			t.Errorf("System32: %v %q", ok, reason)
		}
	}
	if ok, reason := classifyProtected(os.TempDir()); !ok || reason != "the system TEMP folder" {
		t.Errorf("TEMP: %v %q", ok, reason)
	}
	if ok, reason := classifyProtected(scratch); ok {
		t.Errorf("an ordinary folder below TEMP is protected: %q", reason)
	}
	t.Setenv("ProgramFiles", scratch)
	sub := filepath.Join(scratch, "App")
	os.MkdirAll(sub, 0o755)
	if ok, reason := classifyProtected(sub); !ok || reason != "a Program Files folder" {
		t.Errorf("inside Program Files: %v %q", ok, reason)
	}
}

// DUP-06: -y does not cover a protected place; with nobody to ask the run is
// refused before it touches anything.
func TestFindDuplicates_ProtectedRootRefusedWithoutConsole(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	t.Setenv("ProgramFiles", dir)
	a, b, c := threeCopies(t, dir)
	_, err := FindDuplicates(dir, opts("del", "-y"))
	if !IsUsageError(err) || !strings.Contains(err.Error(), "Program Files") {
		t.Fatalf("err = %v, want a usage error naming the place", err)
	}
	if !exists(a) || !exists(b) || !exists(c) {
		t.Fatal("a file in a protected place was deleted without a confirmation")
	}
}

func TestFindDuplicates_ProtectedRootTypedConfirmation(t *testing.T) {
	useStateDir(t)
	for _, c := range []struct {
		input   string
		deleted bool
	}{{"DELETE\n", true}, {"delete\n", false}, {"", false}} {
		dir := t.TempDir()
		t.Setenv("ProgramFiles", dir)
		threeCopies(t, dir)
		o := opts("del", "-y")
		o.Interactive = true
		o.Input = strings.NewReader(c.input)
		res, err := FindDuplicates(dir, o)
		if c.deleted {
			if err != nil || res.Actions.Deleted != 2 {
				t.Errorf("%q: %v, %+v", c.input, err, res.Actions)
			}
		} else if err == nil || exists(filepath.Join(dir, "a.bin")) == false {
			t.Errorf("%q: err %v; a confirmation that is not the word must cancel", c.input, err)
		}
	}
}

// DUP-03: every prompt treats end of input as no.
func TestPrompt_EndOfInputIsNo(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a, b, c := threeCopies(t, dir)
	o := opts("del")
	o.Interactive = true
	o.Input = strings.NewReader("")
	res, err := FindDuplicates(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions.Deleted != 0 || res.Actions.Skipped != 2 {
		t.Fatalf("actions %+v, want 0 deleted, 2 skipped", res.Actions)
	}
	if !exists(a) || !exists(b) || !exists(c) {
		t.Fatal("end of input was read as yes")
	}
}

func TestPrompt_AnswersAreHonoured(t *testing.T) {
	useStateDir(t)
	dir := t.TempDir()
	a, b, c := threeCopies(t, dir)
	o := opts("del")
	o.Interactive = true
	o.Input = strings.NewReader("y\nn\n") // victims in order: b (newer), then a
	res, err := FindDuplicates(dir, o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Actions.Deleted != 1 || exists(b) || !exists(a) || !exists(c) {
		t.Fatalf("actions %+v; a=%v b=%v c=%v", res.Actions, exists(a), exists(b), exists(c))
	}
}

// DUP-06: %WINDIR% and Program Files are skipped by a scan that does not
// start inside them.
func TestScanExclusions_SkipSystemFolders(t *testing.T) {
	useStateDir(t)
	root := t.TempDir()
	pf := filepath.Join(root, "Programs")
	writeFile(t, filepath.Join(pf, "a.bin"), payload(0x21, 9000))
	writeFile(t, filepath.Join(root, "b.bin"), payload(0x21, 9000))
	t.Setenv("ProgramFiles", pf)

	res, err := FindDuplicates(root, opts())
	if err != nil {
		t.Fatal(err)
	}
	if res.DuplicateGroups != 0 {
		t.Fatalf("the Program Files stand-in was scanned (%d groups)", res.DuplicateGroups)
	}
	writeFile(t, filepath.Join(pf, "c.bin"), payload(0x21, 9000))
	res, err = FindDuplicates(pf, opts())
	if err != nil || res.DuplicateGroups != 1 {
		t.Fatalf("a scan started inside it must include it: %v, %d groups", err, res.DuplicateGroups)
	}
}
