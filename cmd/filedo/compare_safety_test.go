package main

// SP-0027 compare tickets: CHK-01 (one folder under two spellings),
// CHK-05 (a pair that differs is never deleted), CHK-06 (unreadable entries
// and failed deletes are not "Done") and CHK-07 (case kept in delete paths).
// AUD-16-F1: the delete phase asks; --yes skips the question, a closed stdin
// cancels. The other delete runs here pass --yes so each proves its own
// safety check, not the prompt.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filedo/statedir"
)

// compareTree writes the same small tree into a folder.
func compareTree(t *testing.T, root string) {
	t.Helper()
	stamp := time.Date(2023, 5, 1, 8, 0, 0, 0, time.UTC)
	for _, rel := range []string{"a.txt", filepath.Join("sub", "b.txt"), filepath.Join("sub", "deep", "c.txt")} {
		p := filepath.Join(root, rel)
		writeFile(t, p, []byte("content of "+rel))
		os.Chtimes(p, stamp, stamp)
	}
}

func assertTreeIntact(t *testing.T, root string) {
	t.Helper()
	for _, rel := range []string{"a.txt", filepath.Join("sub", "b.txt"), filepath.Join("sub", "deep", "c.txt")} {
		if got, err := os.ReadFile(filepath.Join(root, rel)); err != nil || string(got) != "content of "+rel {
			t.Errorf("%s was deleted or changed: %v", rel, err)
		}
	}
}

func TestCompareDeleteSameFolderRefused(t *testing.T) {
	wd := t.TempDir()
	x := filepath.Join(wd, "Photos")
	compareTree(t, x)

	rows := [][]string{
		{"cmp", x, strings.ToLower(x) + `\`, "del", "source", "--yes"},
		{"compare", x, `\\?\` + x, "del", "target", "--yes"},
		{"cmp", x, filepath.Join(x, "sub"), "del", "source", "--yes"},
		{"cmp", filepath.Join(x, "sub"), x, "del", "target", "--yes"},
		{"cmp", x, strings.ToUpper(x)},
	}
	junction := filepath.Join(wd, "Alias")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", junction, x).CombinedOutput(); err == nil {
		rows = append(rows, []string{"cmp", x, junction, "del", "source", "--yes"})
	} else {
		t.Logf("mklink /J unavailable (%v: %s); junction row skipped", err, out)
	}
	for _, args := range rows {
		out, code := run(t, wd, args...)
		if code != 2 {
			t.Errorf("%v exited %d, want 2 (CHK-01)\n%s", args, code, out)
		}
		assertTreeIntact(t, x)
	}
	lst := filepath.Join(wd, "cmp.lst")
	writeFile(t, lst, []byte("cmp "+x+" "+strings.ToLower(x)+" del source --yes\r\n"))
	if out, code := run(t, wd, "from", lst); code != 2 {
		t.Errorf("batch self-compare exited %d, want 2\n%s", code, out)
	}
	assertTreeIntact(t, x)
}

func TestCompareDeleteNeedsEqualSizeAndTime(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	dst := filepath.Join(wd, "dst")
	stamp := time.Date(2023, 5, 1, 8, 0, 0, 0, time.UTC)
	put := func(p string, body string, mod time.Time) {
		writeFile(t, p, []byte(body))
		os.Chtimes(p, mod, mod)
	}
	// A truncated copy on the target: sizes 10 and 0.
	put(filepath.Join(src, "photo.jpg"), "0123456789", stamp)
	put(filepath.Join(dst, "photo.jpg"), "", stamp)
	// A true copy: same size and time.
	put(filepath.Join(src, "dup.txt"), "same bytes", stamp)
	put(filepath.Join(dst, "dup.txt"), "same bytes", stamp)
	// Same size, other time, same bytes: kept by default, deleted by hash.
	put(filepath.Join(src, "touched.txt"), "same again", stamp)
	put(filepath.Join(dst, "touched.txt"), "same again", stamp.Add(time.Hour))

	out, code := run(t, wd, "cmp", src, dst, "del", "source", "--yes")
	if code != 2 {
		t.Errorf("a delete that kept mismatched pairs exited %d, want 2\n%s", code, out)
	}
	if !exists(filepath.Join(src, "photo.jpg")) {
		t.Fatalf("the intact original was deleted next to a truncated copy (CHK-05)\n%s", out)
	}
	if exists(filepath.Join(src, "dup.txt")) {
		t.Errorf("a verified duplicate was not deleted\n%s", out)
	}
	if !exists(filepath.Join(src, "touched.txt")) {
		t.Errorf("a pair with different times was deleted without --by-hash")
	}

	out, code = run(t, wd, "cmp", src, dst, "del", "source", "--by-hash", "--yes")
	if exists(filepath.Join(src, "touched.txt")) {
		t.Errorf("--by-hash did not delete a pair with equal content\n%s", out)
	}
	if !exists(filepath.Join(src, "photo.jpg")) {
		t.Fatalf("--by-hash deleted a pair of different sizes")
	}

	out, code = run(t, wd, "cmp", src, dst, "del", "source", "--allow-mismatch", "--yes")
	if code != 0 || exists(filepath.Join(src, "photo.jpg")) {
		t.Errorf("--allow-mismatch did not delete the mismatched pair (exit %d)\n%s", code, out)
	}
}

func TestCompareWalkErrorNotProven(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	dst := filepath.Join(wd, "dst")
	compareTree(t, src)
	compareTree(t, dst)
	denyList(t, filepath.Join(src, "sub"))
	out, code := run(t, wd, "cmp", src, dst)
	if code != 2 {
		t.Errorf("a compare that could not read a folder exited %d, want 2 (CHK-06)\n%s", code, out)
	}
}

func TestCompareKeepsOriginalCase(t *testing.T) {
	cs := caseSensitiveDir(t) // holds A.txt ("upper") and a.txt ("lower")
	wd := t.TempDir()
	dst := filepath.Join(wd, "dst")
	writeFile(t, filepath.Join(dst, "a.txt"), []byte("lower"))

	out, code := run(t, wd, "cmp", cs, dst, "del", "source", "--allow-mismatch", "--yes")
	if code != 2 {
		t.Errorf("names that differ only in case exited %d, want 2 (CHK-07)\n%s", code, out)
	}
	for _, n := range []string{"A.txt", "a.txt"} {
		if !exists(filepath.Join(cs, n)) {
			t.Errorf("%s was deleted although its pair is ambiguous", n)
		}
	}

	// A pair found under different case on the two sides is deleted by the
	// name it really has.
	src := filepath.Join(wd, "src")
	writeFile(t, filepath.Join(src, "Report.TXT"), []byte("r"))
	writeFile(t, filepath.Join(dst, "report.txt"), []byte("r"))
	stamp := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(filepath.Join(src, "Report.TXT"), stamp, stamp)
	os.Chtimes(filepath.Join(dst, "report.txt"), stamp, stamp)
	os.Remove(filepath.Join(dst, "a.txt"))
	out, code = run(t, wd, "cmp", src, dst, "del", "source", "--yes")
	if code != 0 || exists(filepath.Join(src, "Report.TXT")) {
		t.Errorf("the pair was not deleted by its own name (exit %d)\n%s", code, out)
	}
}

// runAnswering runs filedo with the given text on stdin.
func runAnswering(t *testing.T, wd, stdin string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(), statedir.EnvOverride+"="+wd)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running filedo %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func TestCompareDeleteAsksFirst(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	dst := filepath.Join(wd, "dst")
	compareTree(t, src)
	compareTree(t, dst)

	// A closed stdin is no answer: nothing is deleted and the run is Not
	// proven, exit 2 - on the command line and from a batch file.
	out, code := run(t, wd, "compare", src, dst, "del", "source")
	if code != 2 {
		t.Errorf("an unanswered delete exited %d, want 2 (AUD-16-F1)\n%s", code, out)
	}
	if !strings.Contains(out, "3 files") || !strings.Contains(out, "SOURCE mode") {
		t.Errorf("the question does not name the count and the mode\n%s", out)
	}
	assertTreeIntact(t, src)
	assertTreeIntact(t, dst)

	lst := filepath.Join(wd, "cmp.lst")
	writeFile(t, lst, []byte("cmp "+src+" "+dst+" del old target\r\n"+"cmp "+src+" "+dst+" del source\r\n"))
	if out, code := run(t, wd, "from", lst); code != 2 {
		t.Errorf("an unanswered delete in a batch exited %d, want 2\n%s", code, out)
	}
	assertTreeIntact(t, src)
	assertTreeIntact(t, dst)

	// "n" is an answer: nothing deleted, the compare itself is done.
	out, code = runAnswering(t, wd, "n\r\n", "compare", src, dst, "del", "source")
	if code != 0 || !strings.Contains(out, "Nothing deleted") {
		t.Errorf("a declined delete exited %d, want 0 with \"Nothing deleted\"\n%s", code, out)
	}
	assertTreeIntact(t, src)

	// --yes skips the question and deletes; the other side is untouched.
	out, code = run(t, wd, "compare", src, dst, "del", "source", "--yes")
	if code != 0 {
		t.Errorf("compare .. del source --yes exited %d, want 0\n%s", code, out)
	}
	for _, rel := range []string{"a.txt", filepath.Join("sub", "b.txt"), filepath.Join("sub", "deep", "c.txt")} {
		if exists(filepath.Join(src, rel)) {
			t.Errorf("%s was not deleted under --yes\n%s", rel, out)
		}
	}
	assertTreeIntact(t, dst)
}

func TestCompareDeleteRechecksBothFilesAfterConfirmation(t *testing.T) {
	for _, changedSide := range []string{"source", "target"} {
		t.Run(changedSide, func(t *testing.T) {
			root := t.TempDir()
			src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
			name := "pair.txt"
			stamp := time.Date(2023, 5, 1, 8, 0, 0, 0, time.UTC)
			for _, dir := range []string{src, dst} {
				p := filepath.Join(dir, name)
				writeFile(t, p, []byte("original"))
				if err := os.Chtimes(p, stamp, stamp); err != nil {
					t.Fatal(err)
				}
			}
			oldConfirm := compareConfirm
			compareConfirm = func(string) (bool, bool) {
				p := filepath.Join(src, name)
				if changedSide == "target" {
					p = filepath.Join(dst, name)
				}
				if err := os.Remove(p); err != nil {
					t.Fatal(err)
				}
				writeFile(t, p, []byte("replaced")) // same size and time, different file
				if err := os.Chtimes(p, stamp, stamp); err != nil {
					t.Fatal(err)
				}
				return true, true
			}
			defer func() { compareConfirm = oldConfirm }()
			problems := performDelete(src, dst, "source", "", compareOptions{}, scanFiles(src), scanFiles(dst))
			if len(problems) == 0 || !exists(filepath.Join(src, name)) || !exists(filepath.Join(dst, name)) {
				t.Fatalf("changed %s file must keep both sides; problems: %v", changedSide, problems)
			}
		})
	}
}

func TestCompareDeleteStopBeforeNextHashAndRemove(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for _, dir := range []string{src, dst} {
		writeFile(t, filepath.Join(dir, "pair.txt"), []byte("same"))
	}
	savedHandler, savedHash := globalInterruptHandler, compareHashFile
	globalInterruptHandler = newInterruptHandlerNoSignals()
	defer func() {
		globalInterruptHandler = savedHandler
		compareHashFile = savedHash
	}()
	hashes := 0
	compareHashFile = func(path string) ([]byte, error) {
		hashes++
		globalInterruptHandler.Interrupt()
		return savedHash(path)
	}
	performDelete(src, dst, "source", "", compareOptions{byHash: true, assumeYes: true}, scanFiles(src), scanFiles(dst))
	if hashes != 1 || !exists(filepath.Join(src, "pair.txt")) {
		t.Fatalf("stop must prevent the second hash and deletion; hashes=%d", hashes)
	}
}

// AUD-54-F2: a stop with more tasks queued than the channel holds must not
// leave the producer blocked on a full channel with no worker reading it.
func TestCompareDeleteStopWithAFullQueueReturns(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for i := 0; i < 400; i++ {
		for _, dir := range []string{src, dst} {
			writeFile(t, filepath.Join(dir, fmt.Sprintf("f%03d.txt", i)), []byte("same"))
		}
	}
	savedHandler, savedHash := globalInterruptHandler, compareHashFile
	globalInterruptHandler = newInterruptHandlerNoSignals()
	defer func() {
		globalInterruptHandler = savedHandler
		compareHashFile = savedHash
	}()
	compareHashFile = func(path string) ([]byte, error) {
		globalInterruptHandler.Interrupt()
		return savedHash(path)
	}
	srcScan, dstScan := scanFiles(src), scanFiles(dst)
	done := make(chan struct{})
	go func() {
		performDelete(src, dst, "source", "", compareOptions{byHash: true, assumeYes: true}, srcScan, dstScan)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("performDelete did not return after a stop with a full task queue")
	}
	left, err := os.ReadDir(src)
	if err != nil || len(left) != 400 {
		t.Fatalf("a stopped run deleted files: %d of 400 left (%v)", len(left), err)
	}
}

// AUD-54-F3 / AUD-16-F3: the hash of a large file ends at the next buffer once
// a stop is requested, instead of reading the file to its end.
func TestCompareHashObservesStopBetweenBuffers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "big.bin")
	writeFile(t, p, make([]byte, 8<<20))
	savedHandler := globalInterruptHandler
	globalInterruptHandler = newInterruptHandlerNoSignals()
	defer func() { globalInterruptHandler = savedHandler }()
	globalInterruptHandler.Interrupt()
	if _, err := compareHashFile(p); err == nil {
		t.Fatal("a stopped hash returned a digest")
	}
}

// AUD-54-F1: on a volume that gives no file id to a directory listing (ReFS,
// FAT32, exFAT) an unchanged pair is still deleted. Set
// FILEDO_TEST_NON_NTFS_DIR to a folder on such a volume to run it.
func TestCompareDeleteOnAVolumeWithoutListingIds(t *testing.T) {
	base := os.Getenv("FILEDO_TEST_NON_NTFS_DIR")
	if base == "" {
		t.Skip("FILEDO_TEST_NON_NTFS_DIR not set")
	}
	root, err := os.MkdirTemp(base, "cmpdel")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	compareTree(t, src)
	compareTree(t, dst)
	problems := performDelete(src, dst, "source", "", compareOptions{assumeYes: true}, scanFiles(src), scanFiles(dst))
	if len(problems) != 0 {
		t.Fatalf("unchanged pairs were refused: %v", problems)
	}
	if exists(filepath.Join(src, "a.txt")) {
		t.Fatal("the source twin of an unchanged pair was kept")
	}
	assertTreeIntact(t, dst)
}
