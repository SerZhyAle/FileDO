package main

// In-process tests of the fake-capacity engine (SP-0026). Every target is a
// t.TempDir(); the volume queries are stubbed to a scratch volume that is not
// the system volume, so no test plans around, or writes to, the real disk
// beyond its own temporary folder.

import (
	"bytes"
	"compress/flate"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// withCapacityStubs stands a scratch volume with the given free space and
// size in for the real one, and gives the test a fresh run outcome.
func withCapacityStubs(t *testing.T, free, total int64) {
	t.Helper()
	savedQuery, savedFacts, savedRun := diskSpaceQuery, capacityVolumeFacts, currentRun
	diskSpaceQuery = func(string) (uint64, uint64, error) { return uint64(free), uint64(total), nil }
	capacityVolumeFacts = func(string) volumeFacts { return volumeFacts{Total: total, FileSystem: "NTFS"} }
	currentRun = &runOutcome{numbers: make(map[string]interface{})}
	t.Cleanup(func() { diskSpaceQuery, capacityVolumeFacts, currentRun = savedQuery, savedFacts, savedRun })
}

// fixedSpaceTester is a folder tester on a scratch folder that reports a
// fixed free space, with an optional hook around file creation.
type fixedSpaceTester struct {
	*FolderTester
	free   int64
	create func(ctx context.Context, name string, size int64) (string, error)
}

func (f *fixedSpaceTester) GetAvailableSpace() (int64, error) { return f.free, nil }

func (f *fixedSpaceTester) CreateTestFileContext(ctx context.Context, name string, size int64) (string, error) {
	if f.create != nil {
		return f.create(ctx, name, size)
	}
	return f.FolderTester.CreateTestFileContext(ctx, name, size)
}

func fillNamesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "FILL_") {
			out = append(out, e.Name())
		}
	}
	return out
}

// CAP-01: every tester writes with the one writer the verifier was written for.
func TestTesterWritersPassLiveVerifier(t *testing.T) {
	testers := []struct {
		name string
		make func(dir string) FakeCapacityTester
	}{
		{"device", func(dir string) FakeCapacityTester { return NewDeviceTester(dir) }},
		{"folder", func(dir string) FakeCapacityTester { return NewFolderTester(dir) }},
		{"network", func(dir string) FakeCapacityTester { return &NetworkTester{networkPath: dir} }},
	}
	for _, tc := range testers {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			p, err := tc.make(dir).CreateTestFileContext(context.Background(), "FILL_001_01000000.tmp", 8<<20)
			if err != nil {
				t.Fatal(err)
			}
			if err := verifySmartTestFiles([]string{p}, 1); err != nil {
				t.Errorf("verifySmartTestFiles: %v", err)
			}
			if err := verifyTestFileComplete(p); err != nil {
				t.Errorf("verifyTestFileComplete: %v", err)
			}
			if fi, err := os.Stat(p); err != nil || fi.Size() != 8<<20 {
				t.Errorf("size %v (%v), want %d", fi.Size(), err, 8<<20)
			}
		})
	}
}

// CAP-16: the network tester writes exactly one file per call - no
// calibration files, no per-file buffer probing.
func TestNetworkTesterDoesNotCalibratePerFile(t *testing.T) {
	dir := t.TempDir()
	nt := &NetworkTester{networkPath: dir}
	for i := 1; i <= 3; i++ {
		if _, err := nt.CreateTestFileContext(context.Background(), fmt.Sprintf("FILL_%03d_01000000.tmp", i), 1<<20); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != i {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			t.Fatalf("after %d calls the folder holds %d entries: %v", i, len(entries), names)
		}
	}
}

// CAP-19: a free-space query that fails is an error, not 1 GB.
func TestNetworkFreeSpaceQueryFailureIsAnError(t *testing.T) {
	saved := diskSpaceQuery
	defer func() { diskSpaceQuery = saved }()
	diskSpaceQuery = func(string) (uint64, uint64, error) { return 0, 0, windows.ERROR_BAD_NETPATH }
	free, err := (&NetworkTester{networkPath: t.TempDir()}).GetAvailableSpace()
	if err == nil {
		t.Fatalf("GetAvailableSpace = %d, nil; want an error", free)
	}
}

// CAP-02: folder fill writes files fill verify understands.
func TestFolderFillFileVerifies(t *testing.T) {
	withCapacityStubs(t, 0, 1<<30) // a fill that ran to the end: nothing free
	dir := t.TempDir()
	for i := int64(1); i <= 2; i++ {
		p := filepath.Join(dir, capacityFileName(i, 5, "01000000", "0badf00d"))
		if _, err := writeFillFile(context.Background(), p, 1<<20, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := runCapacityFillVerify("Folder", dir); err != nil {
		t.Fatalf("fill verify: %v", err)
	}
	if currentRun.defects != 0 || currentRun.notProven {
		t.Fatalf("defects %d, notProven %v - a healthy folder fill must pass", currentRun.defects, currentRun.notProven)
	}
}

// CAP-02: a file with no FileDO header is not ours - never "overwritten".
func TestFillVerifyHeaderlessFileIsNotADefect(t *testing.T) {
	withCapacityStubs(t, 0, 1<<30)
	dir := t.TempDir()
	// The old folder fill's template: a FILL name over createRandomFile's blocks.
	if err := createRandomFile(filepath.Join(dir, "FILL_00001_021504.tmp"), 1, false); err != nil {
		t.Fatal(err)
	}
	err := runCapacityFillVerify("Folder", dir)
	if err == nil || errors.Is(err, errDefect) || currentRun.defects != 0 {
		t.Fatalf("err %v, defects %d - want could not verify and no defect", err, currentRun.defects)
	}
}

// Section 5: the old header shape is still read, for one release, and named.
func TestFillVerifyReadsOldFormat(t *testing.T) {
	withCapacityStubs(t, 0, 1<<30)
	dir := t.TempDir()
	name := "FILL_00001_021504.tmp"
	header := "FILEDO_TEST_" + name + "_20260324_012341\n"
	body := bytes.Repeat([]byte("F00001_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "), 30000)
	data := append([]byte(header), body...)
	data = append(data, header...)
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCapacityFillVerify("Folder", dir); err != nil {
		t.Fatalf("fill verify: %v", err)
	}
	if currentRun.numbers["oldFormatFiles"] != 1 {
		t.Errorf("oldFormatFiles = %v, want 1", currentRun.numbers["oldFormatFiles"])
	}
}

// CAP-08: nothing to verify is not a pass.
func TestFillVerifyWithNoFilesIsNotProvenInProcess(t *testing.T) {
	withCapacityStubs(t, 0, 1<<30)
	if err := runCapacityFillVerify("Folder", t.TempDir()); err == nil || errors.Is(err, errDefect) {
		t.Fatalf("err %v - want could not verify", err)
	}
}

// CAP-08: FILL files that leave most of the volume unwritten prove nothing
// about the space nobody wrote.
func TestFillVerifyPartialCoverageIsNotProven(t *testing.T) {
	withCapacityStubs(t, 50<<30, 100<<30)
	dir := t.TempDir()
	for i := int64(1); i <= 2; i++ {
		p := filepath.Join(dir, capacityFileName(i, 5, "01000000", "0badf00d"))
		if _, err := writeFillFile(context.Background(), p, 1<<20, nil); err != nil {
			t.Fatal(err)
		}
	}
	err := runCapacityFillVerify("Folder", dir)
	if err == nil || errors.Is(err, errDefect) || !strings.Contains(err.Error(), "not proven") {
		t.Fatalf("err %v - want not proven", err)
	}
	if currentRun.defects != 0 {
		t.Fatalf("partial coverage recorded %d defects", currentRun.defects)
	}
}

// writeTruncatedFillRun writes three fill files of one run into dir and
// truncates the second to half its size and the third to 0 bytes - what an
// interrupted fill leaves behind. It returns the paths.
func writeTruncatedFillRun(t *testing.T, dir string) []string {
	t.Helper()
	const size = 1 << 20
	var paths []string
	for i := int64(1); i <= 3; i++ {
		p := filepath.Join(dir, capacityFileName(i, 5, "01000000", "0badf00d"))
		if _, err := writeFillFile(context.Background(), p, size, nil); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	if err := os.Truncate(paths[1], size/2); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(paths[2], 0); err != nil {
		t.Fatal(err)
	}
	return paths
}

// AUD-05-F1: the files an interrupted fill leaves - one cut short, one empty -
// are incomplete, not evidence: could not verify, never FAKE CAPACITY.
func TestFillVerifyInterruptedLeftoverIsNotADefect(t *testing.T) {
	withCapacityStubs(t, 0, 1<<30)
	dir := t.TempDir()
	writeTruncatedFillRun(t, dir)
	err := runCapacityFillVerify("Folder", dir)
	if currentRun.defects != 0 {
		t.Fatalf("an interrupted fill's leftovers recorded %d defects (err %v) - they are incomplete, not fake capacity", currentRun.defects, err)
	}
	if err == nil || errors.Is(err, errDefect) {
		t.Fatalf("err %v - want could not verify", err)
	}
}

// AUD-05-F1: a byte that does not read back inside the part of a cut-short
// file that was written is still a defect.
func TestFillVerifyCatchesMismatchInPartialFile(t *testing.T) {
	withCapacityStubs(t, 0, 1<<30)
	dir := t.TempDir()
	paths := writeTruncatedFillRun(t, dir)
	f, err := os.OpenFile(paths[1], os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := f.Stat()
	if _, err := f.WriteAt([]byte{'#'}, st.Size()-100); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if err := runCapacityFillVerify("Folder", dir); err != nil && !errors.Is(err, errDefect) {
		t.Logf("fill verify: %v", err)
	}
	if currentRun.defects == 0 {
		t.Fatal("a flipped byte inside a cut-short file's written part recorded no defect")
	}
}

// CAP-03: a non-anchor file damaged after its own check is caught by the
// final pass, and the damage reads back from the media, not the cache.
func TestFakeCapacityCatchesDelayedCorruptionOfNonAnchorFile(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	dir := t.TempDir()
	paths := map[string]string{}
	ft := &fixedSpaceTester{FolderTester: NewFolderTester(dir), free: 128 << 20}
	ft.create = func(ctx context.Context, name string, size int64) (string, error) {
		p, err := ft.FolderTester.CreateTestFileContext(ctx, name, size)
		if err != nil {
			return p, err
		}
		seq := testFileSeq(filepath.Base(name))
		paths[seq] = p
		if seq == "008" {
			// The counterfeit: writing file 8 lands its body on file 7's middle.
			src, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			f, err := os.OpenFile(paths["007"], os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt(src[size/4:3*size/4], size/4); err != nil {
				t.Fatal(err)
			}
			f.Sync()
			f.Close()
		}
		return p, nil
	}
	_, err := runGenericFakeCapacityTest(ft, false, 10, nil)
	if !errors.Is(err, errDefect) {
		t.Fatalf("err = %v, want a defect", err)
	}
	if !strings.Contains(err.Error(), "file 7 (") {
		t.Errorf("the defect does not name file 7: %v", err)
	}
	if len(currentRun.filesLeft) != 10 {
		t.Errorf("filesLeft names %d files, want the 10 kept as evidence", len(currentRun.filesLeft))
	}
}

// CAP-03: every verification read goes through openForVerify, which opens
// with FILE_FLAG_NO_BUFFERING.
func TestVerifyOpensUnbuffered(t *testing.T) {
	if verifyOpenFlags&windows.FILE_FLAG_NO_BUFFERING == 0 {
		t.Fatalf("verifyOpenFlags %#x lack FILE_FLAG_NO_BUFFERING", verifyOpenFlags)
	}
	dir := t.TempDir()
	name := capacityFileName(1, 5, "01000000", "0badf00d")
	p := filepath.Join(dir, name)
	if _, err := writeFillFile(context.Background(), p, 1<<20, nil); err != nil {
		t.Fatal(err)
	}
	r, err := openUnbufferedForVerify(p)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Unbuffered() {
		t.Errorf("a file on the local temp volume was opened buffered")
	}
	r.Close()

	saved := openForVerify
	defer func() { openForVerify = saved }()
	var calls atomic.Int64
	openForVerify = func(path string) (verifyReader, error) {
		calls.Add(1)
		return openUnbufferedForVerify(path)
	}
	checks := []struct {
		name string
		run  func() error
	}{
		{"quick", func() error { return verifyTestFileQuick(p) }},
		{"complete", func() error { return verifyTestFileComplete(p) }},
		{"sampled", func() error { return verifyTestFileSampled(p, 3) }},
		{"fill verify", func() error {
			seq, run, _ := fillNameInfo(name)
			if c := checkFillFile(fillFileEntry{path: p, name: name, seq: seq, runID: run, size: 1 << 20}); c.state != fillCheckOK {
				return fmt.Errorf("state %d: %s", c.state, c.detail)
			}
			return nil
		}},
	}
	for _, c := range checks {
		before := calls.Load()
		if err := c.run(); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
		if calls.Load() == before {
			t.Errorf("%s read the file without openForVerify", c.name)
		}
	}
}

// CAP-04: only a device-level I/O error is a defect.
func TestCreateErrorClassification(t *testing.T) {
	cases := []struct {
		name   string
		errno  syscall.Errno
		defect bool
	}{
		{"access denied", errAccessDenied, false},
		{"write protect", errWriteProtect, false},
		{"not ready", errNotReady, false},
		{"device not connected", errDeviceNotConnected, false},
		{"network name deleted", errNetnameDeleted, false},
		{"bad network name", errBadNetName, false},
		{"disk full", errDiskFull, false},
		{"file too large", errFileTooLarge, false},
		{"io device", errIODevice, true},
		{"crc", errCRC, true},
		{"write fault", errWriteFault, true},
		{"general failure", errGenFailure, true},
		{"device hardware error", errDeviceHardwareError, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			withCapacityStubs(t, 1<<40, 1<<41)
			dir := t.TempDir()
			ft := &fixedSpaceTester{FolderTester: NewFolderTester(dir), free: 110 << 20}
			ft.create = func(ctx context.Context, name string, size int64) (string, error) {
				if testFileSeq(filepath.Base(name)) == "002" {
					return "", fmt.Errorf("failed to create test file %s: %w", name,
						&os.PathError{Op: "write", Path: filepath.Join(dir, name), Err: c.errno})
				}
				return ft.FolderTester.CreateTestFileContext(ctx, name, size)
			}
			_, err := runGenericFakeCapacityTest(ft, false, 50, nil)
			if err == nil {
				t.Fatal("the test passed through a failed write")
			}
			if got := errors.Is(err, errDefect); got != c.defect {
				t.Fatalf("defect = %v, want %v: %v", got, c.defect, err)
			}
			if !c.defect {
				if _, ok := currentRun.numbers["estimatedRealCapacityGB"]; ok {
					t.Errorf("an environmental error estimated a real capacity")
				}
			}
		})
	}
}

// CAP-07: a speed drop with intact data is a warning, never a verdict.
func TestSpeedDropWithIntactDataIsNotADefect(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	dir := t.TempDir()
	var baseline []time.Duration
	ft := &fixedSpaceTester{FolderTester: NewFolderTester(dir), free: 110 << 20}
	ft.create = func(ctx context.Context, name string, size int64) (string, error) {
		start := time.Now()
		p, err := ft.FolderTester.CreateTestFileContext(ctx, name, size)
		seq := testFileSeq(filepath.Base(name))
		switch {
		case seq <= "004":
			time.Sleep(30 * time.Millisecond)
			if seq <= "003" {
				baseline = append(baseline, time.Since(start))
			}
		default:
			// From file 5 the device is 15x slower, as an SLC cache runs out.
			var sum time.Duration
			for _, d := range baseline {
				sum += d
			}
			time.Sleep(15 * sum / time.Duration(len(baseline)))
		}
		return p, err
	}
	res, err := runGenericFakeCapacityTest(ft, false, 6, nil)
	if err != nil {
		t.Fatalf("a slow but honest device failed the test: %v", err)
	}
	if !res.TestPassed {
		t.Fatalf("TestPassed = false")
	}
	if n, _ := currentRun.numbers["speedAnomalies"].(int); n < 1 {
		t.Errorf("speedAnomalies = %v, want the drop recorded as a warning", currentRun.numbers["speedAnomalies"])
	}
}

// CAP-13: a stop in the middle of a file is a stop: no defect, no estimate,
// and no file left behind, partial or complete.
func TestStopMidFileIsNotADefect(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	saved := globalInterruptHandler
	globalInterruptHandler = newInterruptHandlerNoSignals()
	defer func() { globalInterruptHandler = saved }()

	dir := t.TempDir()
	ft := &fixedSpaceTester{FolderTester: NewFolderTester(dir), free: 110 << 20}
	ft.create = func(ctx context.Context, name string, size int64) (string, error) {
		if testFileSeq(filepath.Base(name)) == "003" {
			// The stop arrives while file 3 is being written: the real
			// writer has created it and sees the cancelled context.
			globalInterruptHandler.Interrupt()
		}
		return ft.FolderTester.CreateTestFileContext(ctx, name, size)
	}
	_, err := runGenericFakeCapacityTest(ft, false, 10, nil)
	if err == nil || errors.Is(err, errDefect) || !errors.Is(err, errRunStopped) {
		t.Fatalf("err = %v, want a stop and no defect", err)
	}
	if currentRun.defects != 0 {
		t.Errorf("the stop recorded %d defects", currentRun.defects)
	}
	if _, ok := currentRun.numbers["estimatedRealCapacityGB"]; ok {
		t.Errorf("the stop estimated a real capacity")
	}
	if left := fillNamesIn(t, dir); len(left) != 0 {
		t.Errorf("files left after the stop: %v", left)
	}
}

// CAP-10: the writer never reuses a name, so an old file's tail can never
// survive under a new header.
func TestWriterNeverReusesAName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "FILL_001_01000000_0badf00d.tmp")
	old := bytes.Repeat([]byte("old run"), 1000)
	if err := os.WriteFile(p, old, 0o644); err != nil {
		t.Fatal(err)
	}
	created, err := writeCapacityFile(context.Background(), p, 1<<20, 1<<20, nil)
	if created || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("created %v, err %v - want the existing name refused", created, err)
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, old) {
		t.Fatalf("the existing file was touched")
	}

	// The driver takes a fresh name when one is taken.
	withCapacityStubs(t, 1<<40, 1<<41)
	var names []string
	ft := &fixedSpaceTester{FolderTester: NewFolderTester(t.TempDir()), free: 110 << 20}
	ft.create = func(ctx context.Context, name string, size int64) (string, error) {
		names = append(names, name)
		if len(names) == 1 {
			return "", fmt.Errorf("failed to create test file %s: %w", name, &os.PathError{Op: "open", Path: name, Err: fs.ErrExist})
		}
		return ft.FolderTester.CreateTestFileContext(ctx, name, size)
	}
	if _, err := runGenericFakeCapacityTest(ft, true, 1, nil); err != nil {
		t.Fatalf("test after a taken name: %v", err)
	}
	if len(names) < 2 || names[0] == names[1] {
		t.Fatalf("names tried: %v - want a second, different name", names)
	}
	for _, n := range names {
		if !fillNameCurrent.MatchString(n) {
			t.Errorf("name %q lacks the seconds and the run id", n)
		}
	}
}

// CAP-11: the plan fits the free space, the file system's per-file limit and
// FAT12/16's root directory.
func TestPlanFitsFreeSpace(t *testing.T) {
	cases := []struct {
		name     string
		free     int64
		files    int
		fs       string
		wantErr  bool
		wantSub  bool
		minFiles int
	}{
		{"thorough preset on 1 GB free", 1000 * capMiB, 1000, "NTFS", false, false, 1},
		{"default on a small stick", 7 * capGiB, 100, "exFAT", false, false, 100},
		{"test 10 on a 64 GB FAT32 card", 60 * capGiB, 10, "FAT32", false, false, 10},
		{"default on 500 GB FAT32", 500 * capGiB, 100, "FAT32", false, false, 100},
		{"FAT16 root directory", 1900 * capMiB, 1000, "FAT", false, true, 1},
		{"exFAT has no 4 GB limit", 1000 * capGiB, 10, "exFAT", false, false, 10},
		{"too little space", 50 * capMiB, 100, "NTFS", true, false, 0},
		{"no files", 10 * capGiB, 0, "NTFS", true, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := planCapacityTest(c.free, c.files, volumeFacts{Total: 2 * c.free, FileSystem: c.fs})
			if c.wantErr {
				if err == nil {
					t.Fatalf("plan %+v, want an error", p)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Files < c.minFiles || p.FileSize < capMiB || p.FileSize%capMiB != 0 {
				t.Errorf("plan %d files of %d bytes", p.Files, p.FileSize)
			}
			if p.Target() > p.Budget || p.Budget > c.free {
				t.Errorf("target %d over budget %d (free %d)", p.Target(), p.Budget, c.free)
			}
			if limit := fsMaxFileBytes(c.fs); limit > 0 && p.FileSize > limit {
				t.Errorf("file size %d over the %s limit %d", p.FileSize, c.fs, limit)
			}
			if (p.SubDir != "") != c.wantSub {
				t.Errorf("SubDir %q, want one: %v", p.SubDir, c.wantSub)
			}
		})
	}
}

// CAP-12: the system volume keeps max(10 GB, 10%) free, and the plan says so.
func TestPlanCapsSystemVolume(t *testing.T) {
	cases := []struct {
		name        string
		free, total int64
		reserve     int64
		wantPlanErr bool
		wantFillErr bool
	}{
		{"large volume keeps 10%", 60 * capGiB, 500 * capGiB, 50 * capGiB, false, false},
		{"small volume keeps 10 GB", 20 * capGiB, 50 * capGiB, 10 * capGiB, false, false},
		{"nothing beyond the reserve", 10*capGiB + 50*capMiB, 50 * capGiB, 10 * capGiB, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := volumeFacts{Total: c.total, FileSystem: "NTFS", SystemVolume: true}
			p, err := planCapacityTest(c.free, 100, v)
			if c.wantPlanErr != (err != nil) {
				t.Fatalf("plan err %v, want error: %v", err, c.wantPlanErr)
			}
			if err == nil {
				if p.Reserve != c.reserve {
					t.Errorf("reserve %d, want %d", p.Reserve, c.reserve)
				}
				if c.free-p.Target() < c.reserve {
					t.Errorf("the test leaves %d free, less than the reserve %d", c.free-p.Target(), c.reserve)
				}
				if len(p.Notes) == 0 || !strings.Contains(p.Notes[0], "System volume") {
					t.Errorf("the plan does not say what it keeps free: %v", p.Notes)
				}
			}
			f, err := planFill(c.free, 100, v)
			if c.wantFillErr != (err != nil) {
				t.Fatalf("fill plan err %v, want error: %v", err, c.wantFillErr)
			}
			if err == nil && c.free-f.MaxFiles*f.FileSize < c.reserve {
				t.Errorf("the fill leaves %d free, less than the reserve %d", c.free-f.MaxFiles*f.FileSize, c.reserve)
			}
		})
	}
}

// CAP-14: clean removes FileDO's files only - by exact name and content.
func TestCleanSparesForeignFiles(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	dir := t.TempDir()
	ctx := context.Background()
	ours := []string{
		filepath.Join(dir, capacityFileName(1, 5, "01000000", "0badf00d")),
		filepath.Join(dir, "speedtest_1_1700000000.txt"),
	}
	if _, err := writeFillFile(ctx, ours[0], 64<<10, nil); err != nil {
		t.Fatal(err)
	}
	if err := createRandomFile(ours[1], 1, false); err != nil {
		t.Fatal(err)
	}
	// A file of the same run whose header the device lost is FileDO's too.
	lost := filepath.Join(dir, capacityFileName(2, 5, "01000000", "0badf00d"))
	if err := os.WriteFile(lost, make([]byte, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	ours = append(ours, lost)
	foreign := map[string]string{
		"speedtest_x.txt":                  "the user's notes",
		"FILL_1.tmp":                       "no header at all",
		"FILL_00003_01000000_0badf00e.tmp": "a FileDO-shaped name of another run, the user's data",
		"FILL_00004_021504.tmp":            "an old-shaped name, the user's data",
	}
	for n, content := range foreign {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := runCapacityClean(dir, true, nil); err != nil {
		t.Fatal(err)
	}
	for _, p := range ours {
		if exists(p) {
			t.Errorf("FileDO's %s survived clean", filepath.Base(p))
		}
	}
	for n := range foreign {
		if !exists(filepath.Join(dir, n)) {
			t.Errorf("clean removed the foreign %s", n)
		}
	}
}

// CAP-14: without --yes clean asks, and no answer removes nothing.
func TestCleanAsksAndNoAnswerRemovesNothing(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	dir := t.TempDir()
	p := filepath.Join(dir, capacityFileName(1, 5, "01000000", "0badf00d"))
	if _, err := writeFillFile(context.Background(), p, 64<<10, nil); err != nil {
		t.Fatal(err)
	}
	saved := cleanConfirm
	defer func() { cleanConfirm = saved }()

	cleanConfirm = func(string) (bool, bool) { return false, false } // stdin closed
	if err := runCapacityClean(dir, false, nil); err == nil {
		t.Errorf("clean without an answer reported success")
	}
	cleanConfirm = func(string) (bool, bool) { return true, false } // "n"
	if err := runCapacityClean(dir, false, nil); err != nil {
		t.Errorf("an explicit no is not an error: %v", err)
	}
	if !exists(p) {
		t.Fatalf("clean removed a file without a yes")
	}
	cleanConfirm = func(string) (bool, bool) { return true, true }
	if err := runCapacityClean(dir, false, nil); err != nil || exists(p) {
		t.Fatalf("clean with a yes: err %v, file still there: %v", err, exists(p))
	}
}

// CAP-15: the body is keyed by the run and the offset.
func TestBodyDependsOnRunAndOffset(t *testing.T) {
	const name = "FILL_001_01000000_0badf00d.tmp"
	a, _ := newTestFileMeta(name, 1, 1<<20, "20260925_120000")
	b, _ := newTestFileMeta(name, 2, 1<<20, "20260925_120000")
	c, _ := newTestFileMeta("FILL_002_01000000_0badf00d.tmp", 1, 1<<20, "20260925_120000")
	blk := func(m *testFileMeta, off int64) []byte {
		buf := make([]byte, tfBlockSize)
		m.fill(buf, off)
		return buf
	}
	if bytes.Equal(blk(a, 8192), blk(b, 8192)) {
		t.Error("two runs wrote the same bytes at one offset")
	}
	if bytes.Equal(blk(a, 8192), blk(a, 12288)) {
		t.Error("one file holds the same bytes at two offsets")
	}
	if bytes.Equal(blk(a, 8192), blk(c, 8192)) {
		t.Error("two files of one run hold the same bytes at one offset")
	}

	// The verifier names what it found instead.
	want := make([]byte, tfBlockSize)
	err := a.check("x", blk(b, 8192), 8192, want)
	if !errors.Is(err, errDataMismatch) || !strings.Contains(err.Error(), "earlier run") {
		t.Errorf("another run's block: %v", err)
	}
	err = a.check("x", blk(a, 12288), 8192, want)
	if !errors.Is(err, errDataMismatch) || !strings.Contains(err.Error(), "of this run") {
		t.Errorf("another offset's block: %v", err)
	}
	if err := a.check("x", blk(a, 8192), 8192, want); err != nil {
		t.Errorf("the right block: %v", err)
	}

	// The header round-trips and a damaged one is refused.
	if m, err := parseTestFileHeader(string(a.Header)); err != nil || m.key != a.key {
		t.Errorf("header round trip: %v", err)
	}
	for _, damaged := range []string{
		strings.Replace(string(a.Header), " 1048576 ", " 01048576 ", 1),
		strings.Replace(string(a.Header), "0000000000000001", "000000000000000G", 1),
		strings.Replace(string(a.Header), " ", "  ", 1),
		strings.TrimSuffix(string(a.Header), "\n"),
	} {
		if _, err := parseTestFileHeader(damaged); err == nil {
			t.Errorf("a damaged header was accepted: %q", damaged)
		}
	}
}

// CAP-15: an NTFS-compressed folder or a deduplicating share cannot store the
// body in less space than it takes.
func TestBodyIncompressible(t *testing.T) {
	m, _ := newTestFileMeta("FILL_001_01000000_0badf00d.tmp", 0x1234, 4<<20, "20260925_120000")
	body := make([]byte, 1<<20)
	m.fill(body, 1<<20)
	var out bytes.Buffer
	w, _ := flate.NewWriter(&out, flate.BestCompression)
	w.Write(body)
	w.Close()
	if float64(out.Len()) < 0.99*(1<<20) {
		t.Fatalf("1 MiB of body compresses to %d bytes", out.Len())
	}
}

// CAP-17: a stopped fill joins its writers and leaves exactly its complete
// files; del then removes exactly those.
func TestDeviceFillCancelCleansExactlyWhatItWrote(t *testing.T) {
	saved := fillWriteFile
	defer func() { fillWriteFile = saved }()
	dir := t.TempDir()
	before := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n atomic.Int64
	fillWriteFile = func(c context.Context, path string, size int64, progress *atomic.Int64) (bool, error) {
		if n.Add(1) == 20 {
			cancel()
		}
		return writeFillFile(c, path, size, progress)
	}
	name := func(i int64) string { return capacityFileName(i, 5, "01000000", "0badf00d") }
	res := runFillFiles(ctx, fillJob{FileSize: 256 << 10, MaxFiles: 1000, Parallelism: 12,
		Path: func(i int64) string { return filepath.Join(dir, name(i)) }}, nil)
	if res.Cause == nil {
		t.Fatalf("a cancelled fill reported no cause")
	}

	var want []string
	res.eachCompleted(func(i int64) { want = append(want, name(i)) })
	got := fillNamesIn(t, dir)
	if strings.Join(got, ",") != strings.Join(want, ",") || int64(len(want)) != res.Completed {
		t.Fatalf("on disk %v, completed %v (%d)", got, want, res.Completed)
	}
	var paths []string
	for _, w := range want {
		paths = append(paths, filepath.Join(dir, w))
	}
	deleteFiles(paths)
	if left := fillNamesIn(t, dir); len(left) != 0 {
		t.Fatalf("after del: %v", left)
	}

	deadline := time.Now().Add(3 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if g := runtime.NumGoroutine(); g > before {
		t.Fatalf("%d goroutines after the fill, %d before - a writer is still running", g, before)
	}
}

// CAP-18: the fill does not allocate per planned file.
func TestFillAllocationDoesNotScaleWithPlannedFiles(t *testing.T) {
	saved := fillWriteFile
	defer func() { fillWriteFile = saved }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var n atomic.Int64
	fillWriteFile = func(context.Context, string, int64, *atomic.Int64) (bool, error) {
		if n.Add(1) >= 64 {
			cancel()
		}
		return false, nil
	}
	runtime.GC()
	var m1, m2 runtime.MemStats
	runtime.ReadMemStats(&m1)
	runFillFiles(ctx, fillJob{FileSize: 1, MaxFiles: 1_000_000, Parallelism: 12,
		Path: func(i int64) string { return capacityFileName(i, 7, "01000000", "0badf00d") }}, nil)
	runtime.ReadMemStats(&m2)
	if alloc := m2.TotalAlloc - m1.TotalAlloc; alloc > 1<<20 {
		t.Fatalf("a fill planned for 1e6 files allocated %d bytes before writing 64", alloc)
	}
}

// CAP-08: a fill that stops making progress is not Done.
func TestDeviceFillTimeoutIsNotDone(t *testing.T) {
	withCapacityStubs(t, 200<<20, 1<<30)
	savedW, savedStall := fillWriteFile, fillStallTimeout
	defer func() { fillWriteFile, fillStallTimeout = savedW, savedStall }()
	fillStallTimeout = 200 * time.Millisecond
	fillWriteFile = func(ctx context.Context, path string, size int64, progress *atomic.Int64) (bool, error) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
		if err != nil {
			return false, err
		}
		f.Close()
		<-ctx.Done() // a device that takes the create and then nothing
		return true, ctx.Err()
	}
	dir := t.TempDir()
	err := runCapacityFill("Device", dir, "1", false, nil)
	if err == nil || errors.Is(err, errDefect) || !errors.Is(err, errFillStalled) {
		t.Fatalf("err = %v, want a stall that is not a defect", err)
	}
	if !strings.Contains(err.Error(), "after 0 of") {
		t.Errorf("the error does not say how far the fill got: %v", err)
	}
	if left := fillNamesIn(t, dir); len(left) != 0 {
		t.Errorf("partial files left: %v", left)
	}
}

// CAP-05: the cleanup hint is the clean verb on the folder holding the files.
func TestCleanupHintIsTheCleanVerbInProcess(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "with space")
	for _, tester := range []FakeCapacityTester{NewDeviceTester(dir), NewFolderTester(dir), &NetworkTester{networkPath: dir}, NewFolderTester(spaced)} {
		hint := tester.GetCleanupCommand()
		args := splitHint(hint)
		if len(args) < 3 || args[0] != "filedo" {
			t.Fatalf("hint %q", hint)
		}
		_, target := tester.GetTestInfo()
		if args[1] != target {
			t.Errorf("hint %q names %q, want %q", hint, args[1], target)
		}
		cmd := flag.NewFlagSet("folder", flag.ContinueOnError)
		cmd.Parse(args[1:])
		if op, _ := genericOperation(cmd); op != "clean" {
			t.Errorf("hint %q dispatches to %q, want clean", hint, op)
		}
	}
}

// splitHint splits a printed command line, honouring double quotes.
func splitHint(s string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for _, r := range s {
		switch {
		case r == '"':
			quoted = !quoted
		case r == ' ' && !quoted:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// CAP-09: every speed test measures through the unbuffered copies.
func TestDeviceSpeedUsesUnbufferedCopy(t *testing.T) {
	savedUp, savedDown := speedUploadCopy, speedDownloadCopy
	defer func() { speedUploadCopy, speedDownloadCopy = savedUp, savedDown }()
	var ups, downs atomic.Int64
	speedUploadCopy = func(src, dst string) (int64, error) { ups.Add(1); return copySpeedTestUpload(src, dst) }
	speedDownloadCopy = func(src, dst string) (int64, error) { downs.Add(1); return copySpeedTestDownload(src, dst) }

	withCapacityStubs(t, 1<<40, 1<<41)
	t.Chdir(t.TempDir())
	target := t.TempDir()
	runs := []struct {
		name string
		run  func() error
	}{
		{"device", func() error { return runDeviceSpeedTest(target, "1", false, true) }},
		{"folder", func() error { return runFolderSpeedTest(target, "1", false, true) }},
		{"network", func() error { return runNetworkSpeedTest(target, "1", false, true, nil) }},
	}
	for _, r := range runs {
		u, d := ups.Load(), downs.Load()
		if err := r.run(); err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if ups.Load() != u+1 || downs.Load() != d+1 {
			t.Errorf("%s did not go through both unbuffered copies", r.name)
		}
	}
	if entries, _ := os.ReadDir(target); len(entries) != 0 {
		t.Errorf("speed tests left files in the target: %d", len(entries))
	}
}

// CLI-25: sizes take k/m/g/t, and nothing unreadable falls back silently.
func TestParseSize(t *testing.T) {
	good := map[string]int{
		"100": 100, "100m": 100, "100MB": 100, " 7 mb ": 7, "1g": 1024, "1GB": 1024,
		"1.5g": 1536, "2048k": 2, "2048KB": 2, "1t": 1 << 20,
	}
	for in, want := range good {
		if got, err := parseSize(in); err != nil || got != want {
			t.Errorf("parseSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "abc", "0", "-5", "512k", "1.5", "clean", "max", "10x", "nan", "inf"} {
		if got, err := parseSize(in); err == nil {
			t.Errorf("parseSize(%q) = %d, want an error", in, got)
		}
	}
	if _, err := parseSizeMB("20000", 1, 10240); err == nil {
		t.Errorf("20000 MB accepted for a 10 GB limit")
	}
	if got, err := parseSizeMB("10g", 1, 10240); err != nil || got != 10240 {
		t.Errorf("10g = %d, %v", got, err)
	}
}

// CLI-29: a short unbuffered read or a refused unbuffered call reopens the
// side buffered and the copy completes; a short write is an error.
func TestCopyShortReadFallsBackBuffered(t *testing.T) {
	data := make([]byte, 20<<20+123)
	for i := range data {
		data[i] = byte(i*7 + i>>9)
	}
	buf := capAlignedBuffer(unbufferedChunkSize, 64<<10)

	t.Run("short read", func(t *testing.T) {
		src := &fakeUnbufferedReader{data: data, shortFirst: 1000}
		var out bytes.Buffer
		n, err := doCopy(&copyEnds{src: src, dst: &out, srcNoBuffer: true, align: 4096,
			reopenSrcBuffered: func(off int64) (io.Reader, error) { return bytes.NewReader(data[off:]), nil },
		}, buf, int64(len(data)))
		if err != nil || n != int64(len(data)) || !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("n %d, err %v, equal %v", n, err, bytes.Equal(out.Bytes(), data))
		}
	})
	t.Run("invalid parameter mid-copy", func(t *testing.T) {
		src := &fakeUnbufferedReader{data: data, refuseAfter: 1}
		var out bytes.Buffer
		reopened := false
		n, err := doCopy(&copyEnds{src: src, dst: &out, srcNoBuffer: true, align: 4096,
			reopenSrcBuffered: func(off int64) (io.Reader, error) {
				reopened = true
				return bytes.NewReader(data[off:]), nil
			},
		}, buf, int64(len(data)))
		if err != nil || !reopened || n != int64(len(data)) || !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("n %d, err %v, reopened %v", n, err, reopened)
		}
	})
	t.Run("unaligned chunk to an unbuffered destination", func(t *testing.T) {
		src := &fakeUnbufferedReader{data: data, shortFirst: 1000, buffered: true}
		var out bytes.Buffer
		dst := &alignedOnlyWriter{w: &out, align: 4096}
		n, err := doCopy(&copyEnds{src: src, dst: dst, dstNoBuffer: true, align: 4096,
			reopenDstBuffered: func(off int64) (io.Writer, error) { return &out, nil },
		}, buf, int64(len(data)))
		if err != nil || n != int64(len(data)) || !bytes.Equal(out.Bytes(), data) {
			t.Fatalf("n %d, err %v, out %d bytes", n, err, out.Len())
		}
	})
	t.Run("short write", func(t *testing.T) {
		src := bytes.NewReader(data)
		_, err := doCopy(&copyEnds{src: src, dst: shortWriter{}, align: 4096}, buf, int64(len(data)))
		if !errors.Is(err, io.ErrShortWrite) {
			t.Fatalf("err = %v, want io.ErrShortWrite", err)
		}
	})
}

// fakeUnbufferedReader serves data the way an unbuffered handle does: reads
// must start on a 4096 boundary. It can return one short read, or refuse with
// ERROR_INVALID_PARAMETER after a number of reads.
type fakeUnbufferedReader struct {
	data        []byte
	off         int
	reads       int
	shortFirst  int
	refuseAfter int
	buffered    bool
}

func (r *fakeUnbufferedReader) Read(p []byte) (int, error) {
	r.reads++
	if !r.buffered && r.off%4096 != 0 {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	if r.refuseAfter > 0 && r.reads > r.refuseAfter {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := len(p)
	if r.reads == 1 && r.shortFirst > 0 {
		n = r.shortFirst
	}
	n = copy(p[:n], r.data[r.off:])
	r.off += n
	return n, nil
}

type alignedOnlyWriter struct {
	w     io.Writer
	align int
}

func (a *alignedOnlyWriter) Write(p []byte) (int, error) {
	if len(p)%a.align != 0 {
		return 0, windows.ERROR_INVALID_PARAMETER
	}
	return a.w.Write(p)
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

// CAP-20: Usage % under a per-user quota.
func TestUsagePercentUnderQuota(t *testing.T) {
	quota := DeviceInfo{TotalBytes: 10 << 30, FreeBytes: 500 << 30, AvailableBytes: 4 << 30}
	if s := quota.String(); !strings.Contains(s, "Usage:         60.0%") {
		t.Errorf("quota-limited String:\n%s", s)
	}
	if s := quota.StringShort(); !strings.Contains(s, "(Usage: 60.0%)") {
		t.Errorf("quota-limited StringShort: %s", s)
	}
	if s := (DeviceInfo{}).String(); !strings.Contains(s, "Usage:         0.0%") {
		t.Errorf("zero total:\n%s", s)
	}
	if got := usagePercent(100, 25); got != 75 {
		t.Errorf("usagePercent(100, 25) = %v", got)
	}
}

// CAP-21: a 4 MB cluster does not wrap the buffer size to zero.
func TestCalculateOptimalBufferSizeLargeClusters(t *testing.T) {
	d := &DriveInfo{ClusterSize: 4 << 20, DriveType: DriveTypeSSD, FileSystem: "exFAT"}
	if got := calculateOptimalBufferSize(d, d); got != 128<<20 {
		t.Fatalf("buffer %d, want %d", got, 128<<20)
	}
}

// GUI-10: the result event names at most filesLeftListCap files and carries
// the full count as a number.
func TestFilesLeftIsCapped(t *testing.T) {
	withCapacityStubs(t, 0, 0)
	paths := make([]string, 2500)
	for i := range paths {
		paths[i] = fmt.Sprintf(`E:\FILL_%05d.tmp`, i)
	}
	recordFilesLeft(paths)
	if len(currentRun.filesLeft) != filesLeftListCap || currentRun.numbers["filesLeftTotal"] != 2500 {
		t.Fatalf("filesLeft %d, total %v", len(currentRun.filesLeft), currentRun.numbers["filesLeftTotal"])
	}
}

// AUD-24-F1: a passed test without del keeps every test file, and the result
// event names them, as it does on a defect or a could-not-verify ending.
func TestPassWithoutDelNamesFilesLeft(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	ft := &fixedSpaceTester{FolderTester: NewFolderTester(t.TempDir()), free: 110 << 20}
	logger := NewHistoryLogger([]string{"test"})
	res, err := runGenericFakeCapacityTest(ft, false, 4, logger)
	if err != nil || !res.TestPassed {
		t.Fatalf("passed %v, err %v", res.TestPassed, err)
	}
	for _, p := range res.CreatedFiles {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("test file %s is not on disk: %v", p, err)
		}
	}
	if len(res.CreatedFiles) != 4 {
		t.Fatalf("created %d files, want 4", len(res.CreatedFiles))
	}
	if len(currentRun.filesLeft) != 4 || currentRun.numbers["filesLeftTotal"] != 4 {
		t.Fatalf("filesLeft %d, filesLeftTotal %v - want the 4 kept files named", len(currentRun.filesLeft), currentRun.numbers["filesLeftTotal"])
	}
	if got := logger.entry.Results["filesDeleted"]; got != false {
		t.Errorf("history filesDeleted = %v, want false", got)
	}
}

// failCleanupTester refuses to delete one test file, as a file held open by
// another process is refused.
type failCleanupTester struct {
	*fixedSpaceTester
	failSeq string
}

func (f *failCleanupTester) CleanupTestFile(p string) error {
	if testFileSeq(filepath.Base(p)) == f.failSeq {
		return &os.PathError{Op: "remove", Path: p, Err: windows.ERROR_SHARING_VIOLATION}
	}
	return f.fixedSpaceTester.CleanupTestFile(p)
}

// AUD-24-F1: with del, a file whose deletion failed is named, and history
// does not claim the files were deleted.
func TestPassWithDelNamesUndeletedFiles(t *testing.T) {
	withCapacityStubs(t, 1<<40, 1<<41)
	ft := &failCleanupTester{
		fixedSpaceTester: &fixedSpaceTester{FolderTester: NewFolderTester(t.TempDir()), free: 110 << 20},
		failSeq:          "002",
	}
	logger := NewHistoryLogger([]string{"test"})
	res, err := runGenericFakeCapacityTest(ft, true, 4, logger)
	if err != nil || !res.TestPassed {
		t.Fatalf("passed %v, err %v", res.TestPassed, err)
	}
	var kept []string
	for _, p := range res.CreatedFiles {
		if _, err := os.Stat(p); err == nil {
			kept = append(kept, p)
		}
	}
	if len(kept) != 1 || testFileSeq(filepath.Base(kept[0])) != "002" {
		t.Fatalf("files on disk after del: %v, want only file 002", kept)
	}
	if len(currentRun.filesLeft) != 1 || currentRun.filesLeft[0] != kept[0] || currentRun.numbers["filesLeftTotal"] != 1 {
		t.Fatalf("filesLeft %v, filesLeftTotal %v - want %s named", currentRun.filesLeft, currentRun.numbers["filesLeftTotal"], kept[0])
	}
	if got := logger.entry.Results["filesDeleted"]; got != false {
		t.Errorf("history filesDeleted = %v, want false when a file stayed", got)
	}
}
