package main

// SP-0027 check tickets (CHK-02, CHK-03, CHK-04, CHK-08, CHK-09, CHK-10) and
// the two honest-count findings: a precounted sweep reported every file
// twice, and files skipped from the good list were counted as "damaged
// before". Black-box, in t.TempDir(), with the state root in the working
// directory unless a row moves it.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filedo/statedir"
)

// checkTree writes n non-empty files under a fresh folder.
func checkTree(t *testing.T, n int) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	for i := 0; i < n; i++ {
		writeFile(t, filepath.Join(root, fmt.Sprintf("d%02d", i%20), fmt.Sprintf("f%04d.bin", i)), patternBytes(1024, byte(i)))
	}
	return root
}

// denyList takes "list folder" away from Everyone on dir, or skips.
func denyList(t *testing.T, dir string) {
	t.Helper()
	if out, err := exec.Command("icacls", dir, "/deny", "*S-1-1-0:(RD)").CombinedOutput(); err != nil {
		t.Skipf("icacls /deny is unavailable (%v: %s)", err, out)
	}
	t.Cleanup(func() { exec.Command("icacls", dir, "/remove:d", "*S-1-1-0").Run() })
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("the deny entry did not take effect (running with backup privilege?)")
	}
}

func TestCheckMaxFilesTerminates(t *testing.T) {
	root := checkTree(t, 3000)
	wd := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filedoExe, "check", root, "--max-files", "5")
	cmd.Dir = wd
	cmd.Env = append(os.Environ(), statedir.EnvOverride+"="+wd)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("check --max-files 5 on 3000 files did not end within 30 s (CHK-02)\n%s", out)
	}
	if err != nil {
		t.Fatalf("check --max-files 5 failed: %v\n%s", err, out)
	}
}

// TestCheckMaxFilesIsAHardLimit is SP-0130 R5: a read is claimed before the
// file is opened, so a folder of 50 files with --max-files 7 reads at most
// seven - the old limit was tested after each result and overshot by up to
// the worker count.
func TestCheckMaxFilesIsAHardLimit(t *testing.T) {
	root := checkTree(t, 50)
	for _, mode := range []string{"on", "off"} {
		t.Run(mode, func(t *testing.T) {
			wd := t.TempDir()
			events := filepath.Join(wd, "events.jsonl")
			out, code := run(t, wd, "--events", events, "check", root, "--max-files", "7", "--single-reader", mode, "--workers", "20")
			if code != 0 {
				t.Fatalf("a partial sweep exited %d, want 0\n%s", code, out)
			}
			nums, ok := lastResult(t, events)["numbers"].(map[string]interface{})
			if !ok {
				t.Fatalf("the result event carries no numbers\n%s", out)
			}
			read := nums["checkedFiles"].(float64) + nums["unverifiedFiles"].(float64)
			if read > 7 {
				t.Errorf("checkedFiles + unverifiedFiles = %g, want at most 7", read)
			}
			if read != 7 {
				t.Errorf("read %g files, want 7", read)
			}
			if got := lastResult(t, events)["verdict"]; got != "Passed" {
				t.Errorf("partial sweep verdict = %v, want Passed", got)
			}
		})
	}
}

func TestCheckKnownDamagedIsDefect(t *testing.T) {
	root := checkTree(t, 3)
	wd := t.TempDir()
	var bad string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && bad == "" {
			bad = p
		}
		return nil
	})
	l, err := loadStateList(filepath.Join(wd, checkDamagedName))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(bad)
	l.AddInfo(bad, info)
	l.Close()

	out, code := run(t, wd, "check", root)
	if code != 1 {
		t.Errorf("a disk with a file already known damaged exited %d, want 1 (CHK-03)\n%s", code, out)
	}
	if !strings.Contains(out, "damaged(known, not re-read)=1") {
		t.Errorf("the known damaged file is not re-reported\n%s", out)
	}

	// The same file, changed since it was recorded, is read again.
	writeFile(t, bad, []byte("rewritten since it was recorded"))
	out, code = run(t, wd, "check", root)
	if code != 0 || !strings.Contains(out, "checked=3") {
		t.Errorf("a changed file was not read again (exit %d)\n%s", code, out)
	}
}

func TestCheckUnreadableFolderNotProven(t *testing.T) {
	root := checkTree(t, 3)
	sub := filepath.Join(root, "locked-away")
	writeFile(t, filepath.Join(sub, "hidden.bin"), patternBytes(100, 7))
	denyList(t, sub)
	wd := t.TempDir()
	out, code := run(t, wd, "check", root)
	if code != 2 {
		t.Errorf("a sweep that could not list a folder exited %d, want 2 (CHK-03)\n%s", code, out)
	}
}

func TestCheckLockedFileNotProven(t *testing.T) {
	root := checkTree(t, 2)
	locked := filepath.Join(root, "in-use.dat")
	writeFile(t, locked, patternBytes(4096, 3))
	release := holdExclusively(t, locked)
	defer release()
	wd := t.TempDir()

	out, code := run(t, wd, "check", root)
	if code != 2 {
		t.Errorf("a sweep that met a locked file exited %d, want 2 (CHK-04)\n%s", code, out)
	}
	if b, err := os.ReadFile(filepath.Join(wd, checkDamagedName)); err == nil && strings.Contains(strings.ToLower(string(b)), "in-use.dat") {
		t.Errorf("a locked file was recorded as damaged\n%s", b)
	}
	if exists(filepath.Join(wd, copySkipListName)) {
		t.Errorf("check wrote copy's skip list")
	}
}

func TestCheckBufKBOutOfRange(t *testing.T) {
	root := checkTree(t, 2)
	wd := t.TempDir()
	for _, v := range []string{"0", "-5", "70000"} {
		out, code := run(t, wd, "check", root, "--buf-kb", v)
		if code != 2 || strings.Contains(out, "panic") {
			t.Errorf("--buf-kb %s exited %d, want 2 and no panic (CHK-09)\n%s", v, code, out)
		}
	}
	out, code := runWithEnv(t, wd, []string{"FILEDO_CHECK_BUF_KB=0"}, "check", root)
	if code != 2 {
		t.Errorf("FILEDO_CHECK_BUF_KB=0 exited %d, want 2\n%s", code, out)
	}
}

func TestCheckSingleFileWritesNothingBesideIt(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "movie.mkv")
	writeFile(t, f, patternBytes(8192, 9))
	state := t.TempDir()

	cmd := exec.Command(filedoExe, "check", f, "--threshold", "-1", "--warmup", "-1")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), statedir.EnvOverride+"="+state)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	if code != 1 {
		t.Fatalf("a file judged damaged exited %d, want 1\n%s", code, out)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "movie.mkv" {
			t.Errorf("a single-file check wrote %s beside the file (CHK-10)", e.Name())
		}
	}
	if b, err := os.ReadFile(filepath.Join(state, checkDamagedName)); err != nil || !strings.Contains(string(b), "movie.mkv") {
		t.Errorf("the damaged file is not in check's list in the state root: %v", err)
	}
}

func TestCheckResumeSkipsOnlyGoodFiles(t *testing.T) {
	root := checkTree(t, 3)
	wd := t.TempDir()
	// A stale resume marker from an older version is ignored: the sweep
	// used to skip everything until it met a path that never came (CHK-08).
	writeFile(t, filepath.Join(wd, "check_state.json"), []byte(`{"lastProcessedPath":"Z:\\gone\\nowhere.bin"}`))

	out, code := run(t, wd, "check", root, "--resume")
	if code != 0 || !strings.Contains(out, "checked=3") {
		t.Fatalf("the first --resume sweep did not read all 3 files (exit %d)\n%s", code, out)
	}
	out, code = run(t, wd, "check", root)
	if code != 0 || !strings.Contains(out, "checked=3") {
		t.Fatalf("a sweep without --resume did not read every file again (exit %d)\n%s", code, out)
	}
	events := filepath.Join(wd, "resume-events.jsonl")
	out, code = run(t, wd, "--events", events, "check", root, "--resume")
	// SP-0130 D2: the good list is check's own proof (size, time and file
	// id - a changed file would have been read), so the carry-on loop's
	// last, successful iteration is Passed, exit 0, not "Not proven".
	if code != 0 || !strings.Contains(out, "skipped(good, --resume)=3") {
		t.Errorf("a --resume sweep with nothing left to read exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "already on the good list") {
		t.Errorf("the resume-done sentence is not kept as information\n%s", out)
	}
	result := lastResult(t, events)
	if result["verdict"] != "Passed" {
		t.Errorf("resume verdict = %v, want Passed", result["verdict"])
	}
	nums := result["numbers"].(map[string]interface{})
	if nums["checkedFiles"] != float64(0) || nums["skippedGoodFiles"] != float64(3) {
		t.Errorf("resume numbers = %v, want checkedFiles=0 and skippedGoodFiles=3", nums)
	}
	var one string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && one == "" {
			one = p
		}
		return nil
	})
	writeFile(t, one, []byte("changed since the last sweep"))
	out, code = run(t, wd, "check", root, "--resume")
	if code != 0 || !strings.Contains(out, "checked=1") {
		t.Errorf("--resume did not read the one changed file (exit %d)\n%s", code, out)
	}
}

// The coordinator's finding: a precounted sweep reported found=6 for three
// files, and good-list skips were printed as "damaged before".
func TestCheckCountsAreHonest(t *testing.T) {
	root := checkTree(t, 3)
	wd := t.TempDir()
	for _, extra := range [][]string{{"--precount"}, {"--no-precount"}} {
		args := append([]string{"check", root}, extra...)
		out, code := run(t, wd, args...)
		if code != 0 || !strings.Contains(out, "found=3, checked=3") {
			t.Errorf("%v: want found=3, checked=3 (exit %d)\n%s", extra, code, out)
		}
	}
	out, _ := run(t, wd, "check", root, "--resume")
	if !strings.Contains(out, "damaged(known, not re-read)=0") || !strings.Contains(out, "skipped(good, --resume)=3") {
		t.Errorf("good-list skips are not reported as such\n%s", out)
	}
}

// TestCheckFlagsStayOnTheirBatchLine is AUD-49-F1: the flags of one `check`
// line in a `from` list end with that line. They were written to the
// process environment and applied to every later line, so the second line
// here read a few files and still passed.
func TestCheckFlagsStayOnTheirBatchLine(t *testing.T) {
	a, b := checkTree(t, 40), checkTree(t, 40)
	wd := t.TempDir()
	writeLst(t, wd, "two.lst", "check \""+a+"\" --no-precount --max-files 1\ncheck \""+b+"\" --no-precount\n")
	out, code := run(t, wd, "from", "two.lst")
	if code != 0 {
		t.Fatalf("from two.lst exited %d\n%s", code, out)
	}
	i := strings.LastIndex(out, "checked=")
	if i < 0 || !strings.HasPrefix(out[i:], "checked=40") {
		t.Fatalf("the second line did not read all 40 files: --max-files of the first line leaked\n%s", out)
	}
}
