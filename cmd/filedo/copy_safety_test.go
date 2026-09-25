package main

// SP-0027 copy tickets (COPY-01..COPY-19) and building block B3. The
// black-box rows drive the exe TestMain builds, in t.TempDir(), with stdin
// closed and the state root moved into the test's working directory; the
// unit rows call the engine in-process with a handler that no signal reaches.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"filedo/statedir"
)

// runWithEnv is run() with extra environment variables.
func runWithEnv(t *testing.T, wd string, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = wd
	cmd.Env = append(append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(wd),
		statedir.EnvOverride+"="+wd,
	), env...)
	cmd.Stdin = strings.NewReader("")
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

// startFiledo launches filedo without waiting for it.
func startFiledo(t *testing.T, wd string, args ...string) (*exec.Cmd, func() string) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(wd),
		statedir.EnvOverride+"="+wd,
	)
	cmd.Stdin = strings.NewReader("")
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	w := &lockedWriter{mu: &mu, buf: buf}
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start filedo: %v", err)
	}
	return cmd, func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
}

// waitExit waits for a started filedo and returns its exit code; a process
// that outlives the deadline is killed and fails the test.
func waitExit(t *testing.T, cmd *exec.Cmd, within time.Duration, output func() string) int {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err == nil {
			return 0
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		t.Fatalf("waiting for filedo: %v", err)
	case <-time.After(within):
		cmd.Process.Kill()
		<-done
		t.Fatalf("filedo did not exit within %v\n%s", within, output())
	}
	return -1
}

// patternBytes is n deterministic, non-zero bytes.
func patternBytes(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%251) ^ seed | 1
	}
	return b
}

func writeFile(t *testing.T, p string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertNoCopyPartials walks dir for .filedo-partial leftovers.
func assertNoCopyPartials(t *testing.T, dir string) {
	t.Helper()
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && isPartialName(p) {
			t.Errorf("a partial file survived: %s", p)
		}
		return nil
	})
}

// isolateState points the state root and the current directory of an
// in-process test at a scratch folder.
func isolateState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(statedir.EnvOverride, dir)
	t.Chdir(dir)
	return dir
}

// ---------------------------------------------------------------------------
// COPY-01: a copy onto itself is refused; an existing target is kept.
// ---------------------------------------------------------------------------

func TestSingleFileCopyOntoItselfRefused(t *testing.T) {
	wd := t.TempDir()
	p := filepath.Join(wd, "data.bin")
	body := patternBytes(100000, 0x31)
	writeFile(t, p, body)

	rows := [][]string{
		{"copy", p, strings.ToUpper(p)},
		{"fastcopy", p, p},
		{"safecopy", p, p},
		{"synccopy", p, wd + `\.\data.bin`},
		{"maxcopy", p, `\\?\` + p},
		{"file", p, "copy", strings.ToUpper(p)},
	}
	for _, args := range rows {
		out, code := run(t, wd, args...)
		if code != 2 {
			t.Errorf("%v exited %d, want 2 (usage refusal)\n%s", args, code, out)
		}
		if got := mustRead(t, p); !bytes.Equal(got, body) {
			t.Fatalf("%v changed the source: %d bytes, want %d\n%s", args, len(got), len(body), out)
		}
	}
	// From a batch file too: the same refusal on the other dispatch.
	lst := filepath.Join(wd, "self.lst")
	writeFile(t, lst, []byte("copy "+p+" "+strings.ToUpper(p)+"\r\n"))
	if out, code := run(t, wd, "from", lst); code != 2 {
		t.Errorf("batch self-copy exited %d, want 2\n%s", code, out)
	}
	if got := mustRead(t, p); !bytes.Equal(got, body) {
		t.Fatalf("the batch self-copy changed the source")
	}
}

func TestSingleFileCopyKeepsExistingTarget(t *testing.T) {
	for _, verb := range []string{"fastcopy", "copy", "safecopy"} {
		t.Run(verb, func(t *testing.T) {
			wd := t.TempDir()
			src := filepath.Join(wd, "new.docx")
			dst := filepath.Join(wd, "out", "old.docx")
			writeFile(t, src, patternBytes(5000, 0x11))
			older := patternBytes(3000, 0x22)
			writeFile(t, dst, older)

			out, code := run(t, wd, verb, src, dst)
			if code != 2 {
				t.Errorf("exit %d, want 2 (the target was kept, the copy is not done)\n%s", code, out)
			}
			if got := mustRead(t, dst); !bytes.Equal(got, older) {
				t.Fatalf("an existing, different target was overwritten\n%s", out)
			}
			if !strings.Contains(out, "KEPT") {
				t.Errorf("the kept target is not reported\n%s", out)
			}
			assertNoCopyPartials(t, wd)
		})
	}
}

// AUD-01-F2 (and its duplicate AUD-07-F4): a single-file target spelled with a
// trailing separator is a folder, even when it does not exist yet. It used to
// be created as a folder and then refused as a "different" target, exit 2.
func TestSingleFileCopyIntoNewFolderWithTrailingSeparator(t *testing.T) {
	rows := [][]string{
		{"fastcopy"},
		{"safecopy"},
		{"copy"},
		{"file", "copy"},
	}
	for _, verb := range rows {
		for _, sep := range []string{`\`, `/`} {
			t.Run(strings.Join(verb, "-")+sep, func(t *testing.T) {
				wd := t.TempDir()
				src := filepath.Join(wd, "a.txt")
				body := patternBytes(5000, 0x41)
				writeFile(t, src, body)
				dir := filepath.Join(wd, "newdir")

				var args []string
				if len(verb) == 2 {
					args = []string{verb[0], src, verb[1], dir + sep}
				} else {
					args = []string{verb[0], src, dir + sep}
				}
				out, code := run(t, wd, args...)
				if code != 0 {
					t.Errorf("%v exited %d, want 0\n%s", args, code, out)
				}
				if got, err := os.ReadFile(filepath.Join(dir, "a.txt")); err != nil || !bytes.Equal(got, body) {
					t.Fatalf("%v did not copy the file under the new folder (err %v)\n%s", args, err, out)
				}
				assertNoCopyPartials(t, wd)
			})
		}
	}
}

// ---------------------------------------------------------------------------
// COPY-02 / B2 / B3: a stop leaves nothing half-written; a re-run finishes.
// ---------------------------------------------------------------------------

func TestCopyStoppedLeavesNoPartial(t *testing.T) {
	for _, verb := range [][]string{{"fastcopy"}, {"folder-copy"}} {
		t.Run(verb[0], func(t *testing.T) {
			wd := t.TempDir()
			srcDir := filepath.Join(wd, "src")
			body := bytes.Repeat(patternBytes(1<<20, 0x5a), 64) // 64 MB
			writeFile(t, filepath.Join(srcDir, "big.bin"), body)
			dstDir := filepath.Join(wd, "dst")
			stop := filepath.Join(wd, "stop.now")

			args := []string{"--stop-file", stop, "fastcopy", srcDir, dstDir}
			if verb[0] == "folder-copy" {
				args = []string{"--stop-file", stop, "folder", srcDir, "copy", dstDir}
			}
			cmd, output := startFiledo(t, wd, args...)
			time.Sleep(150 * time.Millisecond)
			writeFile(t, stop, []byte("stop"))
			code := waitExit(t, cmd, 60*time.Second, output)
			if code != 0 {
				t.Logf("stopped run exited %d\n%s", code, output())
			}

			target := filepath.Join(dstDir, "big.bin")
			if got, err := os.ReadFile(target); err == nil && !bytes.Equal(got, body) {
				t.Fatalf("a stopped copy left %d bytes under the final name (want absent or all %d)", len(got), len(body))
			}
			assertNoCopyPartials(t, dstDir)

			os.Remove(stop)
			rerun := []string{"fastcopy", srcDir, dstDir}
			if verb[0] == "folder-copy" {
				rerun = []string{"folder", srcDir, "copy", dstDir}
			}
			out, code := run(t, wd, rerun...)
			if code != 0 {
				t.Fatalf("the re-run exited %d, want 0\n%s", code, out)
			}
			if got := mustRead(t, target); !bytes.Equal(got, body) {
				t.Fatalf("the re-run did not make the target byte-equal")
			}
			// A third run finds it already there: same size, same time.
			out, code = run(t, wd, rerun...)
			if code != 0 || !strings.Contains(out, "1 already at the target") {
				t.Errorf("a finished copy is not skipped as already there (exit %d)\n%s", code, out)
			}
		})
	}
}

// TestCopyOneFileStopMidwayLeavesNothing stops a copy between two buffers,
// deterministically: the progress hook cancels after the first write.
func TestCopyOneFileStopMidwayLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	writeFile(t, src, patternBytes(4<<20, 0x42))
	info, _ := os.Stat(src)
	dst := filepath.Join(dir, "dst.bin")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := copyOneFile(ctx, src, info, dst, fileCopyOptions{
		Buffer:  make([]byte, minCopyBuffer),
		OnBytes: func(int64) { cancel() },
	})
	if !isStopError(err) {
		t.Fatalf("a stopped copy returned %v, want the stop", err)
	}
	if exists(dst) || exists(dst+partialSuffix) {
		t.Fatalf("a stopped copy left a file behind: final=%v partial=%v", exists(dst), exists(dst+partialSuffix))
	}
}

// ---------------------------------------------------------------------------
// COPY-03 / COPY-07: a stop ends a large fast copy quickly, and per-file
// cleanups do not pile up.
// ---------------------------------------------------------------------------

func TestFastCopyStopEndsQuickly(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	for i := 0; i < 3000; i++ {
		writeFile(t, filepath.Join(src, fmt.Sprintf("d%02d", i%30), fmt.Sprintf("f%04d.bin", i)), patternBytes(16<<10, byte(i)))
	}
	dst := filepath.Join(wd, "dst")
	stop := filepath.Join(wd, "stop.now")
	events := filepath.Join(wd, "events.jsonl")

	cmd, output := startFiledo(t, wd, "--events", events, "--stop-file", stop, "fastcopy", src, dst)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if n := countFiles(dst); n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	writeFile(t, stop, []byte("stop"))
	stoppedAt := time.Now()
	code := waitExit(t, cmd, 30*time.Second, output)
	took := time.Since(stoppedAt)
	if took > 10*time.Second {
		t.Errorf("the stop took %v to end the copy", took)
	}
	if code != 0 {
		t.Errorf("a stopped copy exited %d, want 0\n%s", code, output())
	}
	if v := lastResult(t, events)["verdict"]; v != "Stopped" {
		t.Errorf("verdict %q, want Stopped\n%s", v, output())
	}
	assertNoCopyPartials(t, dst)
	t.Logf("stop to exit: %v, files copied before the stop: %d of 3000", took, countFiles(dst))
}

func countFiles(dir string) int {
	n := 0
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestCopyCleanupsDoNotAccumulate(t *testing.T) {
	isolateState(t)
	src := filepath.Join(t.TempDir(), "src")
	for i := 0; i < 200; i++ {
		writeFile(t, filepath.Join(src, fmt.Sprintf("f%03d.txt", i)), patternBytes(1000, byte(i)))
	}
	h := newInterruptHandlerNoSignals()
	cfg := NewFastCopyConfig()
	if _, err := runCopyEngine("fastcopy", src, filepath.Join(t.TempDir(), "dst"), cfg, nil, h); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if n := h.cleanupCount(); n != 0 {
		t.Errorf("%d per-file cleanups are still registered after the run (CLI-20/COPY-07)", n)
	}
}

// ---------------------------------------------------------------------------
// COPY-04: a stopped plain copy leaves no empty files.
// ---------------------------------------------------------------------------

func TestFolderCopyStopLeavesNoEmptyFiles(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	bodies := map[string][]byte{}
	for i := 0; i < 400; i++ {
		rel := filepath.Join(fmt.Sprintf("d%d", i%8), fmt.Sprintf("f%03d.bin", i))
		bodies[rel] = patternBytes(64<<10, byte(i))
		writeFile(t, filepath.Join(src, rel), bodies[rel])
	}
	dst := filepath.Join(wd, "dst")
	stop := filepath.Join(wd, "stop.now")
	events := filepath.Join(wd, "events.jsonl")

	cmd, output := startFiledo(t, wd, "--events", events, "--stop-file", stop, "folder", src, "copy", dst)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && countFiles(dst) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	writeFile(t, stop, []byte("stop"))
	code := waitExit(t, cmd, 60*time.Second, output)
	if code != 0 {
		t.Errorf("a stopped copy exited %d, want 0\n%s", code, output())
	}
	if v := lastResult(t, events)["verdict"]; v != "Stopped" {
		t.Errorf("verdict %q, want Stopped", v)
	}
	copied := 0
	filepath.Walk(dst, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		copied++
		if info.Size() == 0 {
			t.Errorf("an empty file was left for a stopped copy: %s", p)
			return nil
		}
		rel, _ := filepath.Rel(dst, p)
		if got := mustRead(t, p); !bytes.Equal(got, bodies[rel]) {
			t.Errorf("%s is not byte-equal to its source", rel)
		}
		return nil
	})
	assertNoCopyPartials(t, dst)
	t.Logf("%d of 400 files copied before the stop", copied)
}

// ---------------------------------------------------------------------------
// COPY-05: the progress line never writes the skip list.
// ---------------------------------------------------------------------------

func TestShowFastProgressWritesNoSkipList(t *testing.T) {
	dir := isolateState(t)
	p := &FastCopyProgress{StartTime: time.Now().Add(-30 * time.Second), MaxThreads: 1}
	p.addActiveFile(filepath.Join(dir, "large.mkv"), 5<<30)
	atomic.StoreInt64(&p.ActiveFiles, 1)
	atomic.StoreInt64(&p.ProcessedFiles, 3)

	// Constant file count, growing bytes, last progress 11 s ago.
	atomic.StoreInt64(&p.ActualCopiedSize, 100<<20)
	p.LastProgressBytes = 50 << 20
	p.LastProgressTime = time.Now().Add(-11 * time.Second)
	showFastProgress(p)
	if p.StallReported {
		t.Errorf("a copy whose bytes are moving was reported as stalled")
	}

	// Bytes standing still for 11 s: reported on the console, never recorded.
	p.LastProgressTime = time.Now().Add(-11 * time.Second)
	showFastProgress(p)
	if !p.StallReported {
		t.Errorf("11 s without a byte moving was not reported")
	}
	for _, name := range []string{copySkipListName, checkDamagedName} {
		if exists(filepath.Join(dir, name)) {
			t.Errorf("the progress line wrote %s", name)
		}
	}
}

// ---------------------------------------------------------------------------
// COPY-06: a file that cannot be copied is counted and the run is not done.
// ---------------------------------------------------------------------------

func TestFastCopyLockedFileNotProven(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "src")
	writeFile(t, filepath.Join(src, "a.txt"), []byte("alpha"))
	writeFile(t, filepath.Join(src, "b.txt"), []byte("bravo"))
	locked := filepath.Join(src, "locked.txt")
	writeFile(t, locked, []byte("locked"))
	release := holdExclusively(t, locked)
	defer release()
	dst := filepath.Join(wd, "dst")

	out, code := run(t, wd, "fastcopy", src, dst)
	if code != 2 {
		t.Errorf("a copy that could not read one file exited %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "FAILED") || !strings.Contains(out, "1 failed") {
		t.Errorf("the failed file is not counted and printed\n%s", out)
	}
	for _, n := range []string{"a.txt", "b.txt"} {
		if !exists(filepath.Join(dst, n)) {
			t.Errorf("%s was not copied", n)
		}
	}
	if exists(filepath.Join(dst, "locked.txt")) {
		t.Errorf("a file that could not be read exists at the target")
	}
	assertNoCopyPartials(t, dst)

	out, code = run(t, wd, "fastcopy", locked, filepath.Join(wd, "one.txt"))
	if code != 2 {
		t.Errorf("a single locked file exited %d, want 2\n%s", code, out)
	}
}

// ---------------------------------------------------------------------------
// COPY-08: a buffer above the largest pool buffer is clamped, never a panic.
// ---------------------------------------------------------------------------

func TestCopyBufferAbove128MBDoesNotPanic(t *testing.T) {
	isolateState(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "sparse.bin")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	const size = 130 << 20
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	for _, off := range []int64{0, size / 2, size - 4096} {
		if _, err := f.WriteAt(patternBytes(4096, byte(off)), off); err != nil {
			t.Fatal(err)
		}
	}
	f.Close()
	dst := filepath.Join(dir, "copy.bin")

	cfg := NewMaxPerformanceConfig()
	cfg.MaxBufferSize = 256 << 20
	cfg.MaxConcurrentFiles = 1
	if _, err := runCopyEngine("maxcopy", src, dst, cfg, nil, newInterruptHandlerNoSignals()); err != nil {
		t.Fatalf("copy: %v", err)
	}
	assertSameFileBytes(t, src, dst)

	opt := optimalFastCopyConfig(&OptimalCopyConfig{OptimalThreadCount: 16, MaxBufferSize: 256 << 20, OptimalBufferSize: 64 << 20, SmallFileThreshold: 1 << 20})
	if opt.MaxBufferSize > copyBufferCap() {
		t.Errorf("the optimal copy keeps a %d MB buffer, above the %d MB cap", opt.MaxBufferSize>>20, copyBufferCap()>>20)
	}
	buf, put := takeCopyBuffer(256 << 20)
	if len(buf) > cap(buf) || len(buf) > copyBufferCap() {
		t.Errorf("takeCopyBuffer(256 MB) returned %d bytes", len(buf))
	}
	put()
}

func assertSameFileBytes(t *testing.T, a, b string) {
	t.Helper()
	fa, err := os.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		t.Fatal(err)
	}
	defer fb.Close()
	ba, bb := make([]byte, 1<<20), make([]byte, 1<<20)
	for {
		na, ea := io.ReadFull(fa, ba)
		nb, eb := io.ReadFull(fb, bb)
		if na != nb || !bytes.Equal(ba[:na], bb[:nb]) {
			t.Fatalf("%s and %s differ", a, b)
		}
		if ea != nil || eb != nil {
			if (ea == nil) != (eb == nil) {
				t.Fatalf("%s and %s have different lengths", a, b)
			}
			return
		}
	}
}

// ---------------------------------------------------------------------------
// COPY-09: no more files in flight than workers.
// ---------------------------------------------------------------------------

func TestCopyWorkerPoolBoundsActiveFiles(t *testing.T) {
	isolateState(t)
	src := filepath.Join(t.TempDir(), "src")
	const files = 5000
	for i := 0; i < files; i++ {
		writeFile(t, filepath.Join(src, fmt.Sprintf("d%02d", i%50), fmt.Sprintf("f%04d", i)), []byte{byte(i) | 1})
	}
	dst := filepath.Join(t.TempDir(), "dst")
	cfg := NewFastCopyConfig()
	cfg.MaxConcurrentFiles = 2
	cfg.SmallFileBatchSize = 1

	e := newCopyEngine("fastcopy", cfg, nil, newInterruptHandlerNoSignals())
	defer e.close()
	start := time.Now()
	if err := e.copyTree(src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	peak := atomic.LoadInt64(&e.progress.PeakActiveFiles)
	if peak > 2 || peak < 1 {
		t.Errorf("peak files in flight %d, want 1..2 with two workers", peak)
	}
	if n := countFiles(dst); n != files {
		t.Errorf("%d files copied, want %d", n, files)
	}
	t.Logf("%d files in %v, peak in flight %d", files, time.Since(start).Round(time.Millisecond), peak)
}

// ---------------------------------------------------------------------------
// COPY-11: the watchdog is about progress, never the total time.
// ---------------------------------------------------------------------------

type throttledReader struct {
	chunk, left int
	every       time.Duration
}

func (r *throttledReader) Read(p []byte) (int, error) {
	if r.left <= 0 {
		return 0, io.EOF
	}
	time.Sleep(r.every)
	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	r.left--
	return n, nil
}

type blockingReader struct {
	first   bool
	release <-chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	if !r.first {
		r.first = true
		return 1, nil
	}
	<-r.release
	return 0, io.EOF
}

func TestCopyWatchdogIsNoProgressNotDuration(t *testing.T) {
	// Three times the watchdog period of data, always moving: it succeeds.
	const watchdog = time.Second
	start := time.Now()
	moved, err := copyStreamWatched(context.Background(), io.Discard,
		&throttledReader{chunk: 4096, left: 30, every: 100 * time.Millisecond},
		make([]byte, 64<<10), watchdog, nil, nil, nil)
	if err != nil || moved != 30*4096 {
		t.Fatalf("a slow but moving copy failed after %v: moved=%d err=%v", time.Since(start), moved, err)
	}
	if time.Since(start) < 2*watchdog {
		t.Fatalf("the throttled copy ran only %v - not longer than the watchdog", time.Since(start))
	}

	// A copy that stops moving is given up after the watchdog period.
	release := make(chan struct{})
	defer close(release)
	start = time.Now()
	_, err = copyStreamWatched(context.Background(), io.Discard, &blockingReader{release: release},
		make([]byte, 64<<10), watchdog, nil, nil, nil)
	if !isCopyStall(err) {
		t.Fatalf("a stalled copy returned %v, want a stall", err)
	}
	if took := time.Since(start); took > 3*watchdog {
		t.Errorf("the stall was noticed after %v", took)
	}
}

// ---------------------------------------------------------------------------
// COPY-12: one empty file copies at once.
// ---------------------------------------------------------------------------

func TestFastCopySingleEmptyFile(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "empty.txt")
	writeFile(t, src, nil)
	dst := filepath.Join(wd, "out", "empty.txt")
	start := time.Now()
	out, code := run(t, wd, "fastcopy", src, dst)
	took := time.Since(start)
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	if info, err := os.Stat(dst); err != nil || info.Size() != 0 {
		t.Fatalf("the empty target is missing: %v", err)
	}
	if took > 3*time.Second {
		t.Errorf("one empty file took %v", took)
	}
	if exists(filepath.Join(wd, copySkipListName)) {
		t.Errorf("an empty file landed on the skip list")
	}
}

// ---------------------------------------------------------------------------
// COPY-13: modification times survive every copy path.
// ---------------------------------------------------------------------------

func TestCopyPreservesModTime(t *testing.T) {
	stamp := time.Date(2001, 6, 15, 10, 30, 0, 0, time.UTC)
	for _, args := range [][]string{
		{"fastcopy"}, {"safecopy"}, {"copy"}, {"synccopy"}, {"folder", "copy"},
	} {
		t.Run(strings.Join(args, "-"), func(t *testing.T) {
			wd := t.TempDir()
			src := filepath.Join(wd, "src")
			f := filepath.Join(src, "old.bin")
			writeFile(t, f, patternBytes(5<<20, 0x77))
			if err := os.Chtimes(f, stamp, stamp); err != nil {
				t.Fatal(err)
			}
			dst := filepath.Join(wd, "dst")
			var cmd []string
			if len(args) == 2 {
				cmd = []string{"folder", src, "copy", dst}
			} else {
				cmd = []string{args[0], src, dst}
			}
			out, code := run(t, wd, cmd...)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, out)
			}
			info, err := os.Stat(filepath.Join(dst, "old.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if d := info.ModTime().Sub(stamp); d > time.Millisecond || d < -time.Millisecond {
				t.Errorf("modification time %v, want %v", info.ModTime().UTC(), stamp)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// COPY-14: names that differ only in case are a reported conflict.
// ---------------------------------------------------------------------------

// caseSensitiveDir makes a case-sensitive folder or skips the test.
func caseSensitiveDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "cs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("fsutil", "file", "setCaseSensitiveInfo", dir, "enable").CombinedOutput(); err != nil {
		t.Skipf("case-sensitive folders are unavailable here (%v: %s)", err, strings.TrimSpace(string(out)))
	}
	if err := os.WriteFile(filepath.Join(dir, "A.txt"), []byte("upper"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("lower"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "A.txt")); string(b) != "upper" {
		t.Skip("the folder did not become case-sensitive")
	}
	return dir
}

func TestCopyCaseCollisionReported(t *testing.T) {
	src := caseSensitiveDir(t)
	for _, verb := range []string{"fastcopy", "folder-copy"} {
		t.Run(verb, func(t *testing.T) {
			wd := t.TempDir()
			dst := filepath.Join(wd, "dst")
			args := []string{"fastcopy", src, dst}
			if verb == "folder-copy" {
				args = []string{"folder", src, "copy", dst}
			}
			out, code := run(t, wd, args...)
			if code != 2 {
				t.Errorf("exit %d, want 2\n%s", code, out)
			}
			if !strings.Contains(out, "NAME COLLISION") {
				t.Errorf("the collision is not reported\n%s", out)
			}
			if got, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(got) != "upper" {
				t.Errorf("the target holds %q, want the first name's bytes only", got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// COPY-15: copies given up on after a stall are bounded.
// ---------------------------------------------------------------------------

func TestStalledCopiesAreBounded(t *testing.T) {
	prev := maxAbandonedCopies
	maxAbandonedCopies = 4
	defer func() { maxAbandonedCopies = prev }()
	release := make(chan struct{})

	base := runtime.NumGoroutine()
	started, refused := 0, 0
	for i := 0; i < 50; i++ {
		if !copyAttemptAllowed() {
			refused++
			continue
		}
		started++
		_, err := copyStreamWatched(context.Background(), io.Discard, &blockingReader{release: release},
			make([]byte, 1024), time.Second, nil, nil, nil)
		if !isCopyStall(err) {
			t.Fatalf("attempt %d: %v, want a stall", i, err)
		}
	}
	if started > 4 || refused < 46 {
		t.Errorf("started %d, refused %d - the bound of 4 did not hold", started, refused)
	}
	if n := runtime.NumGoroutine() - base; n > 4+2 {
		t.Errorf("%d goroutines outlive the stalled copies, want at most the bound", n)
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for abandonedCopies.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := abandonedCopies.Load(); n != 0 {
		t.Errorf("%d abandoned copies are still counted after their reads returned", n)
	}
}

// ---------------------------------------------------------------------------
// COPY-16: the skip list is read once per run.
// ---------------------------------------------------------------------------

func writeBigSkipList(t *testing.T, dir string, entries int, real ...string) {
	t.Helper()
	l, err := loadStateList(filepath.Join(dir, copySkipListName))
	if err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < entries; i++ {
		if err := l.Add(fmt.Sprintf(`Z:\nowhere\file%06d.bin`, i), int64(i), stamp); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range real {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		l.AddInfo(p, info)
	}
	l.Close()
}

func TestCopySkipListLoadedOncePerRun(t *testing.T) {
	state := isolateState(t)
	src := filepath.Join(t.TempDir(), "src")
	for i := 0; i < 300; i++ {
		writeFile(t, filepath.Join(src, fmt.Sprintf("f%03d.bin", i)), patternBytes(1000+i, byte(i)))
	}
	damaged := filepath.Join(src, "f007.bin")
	writeBigSkipList(t, state, 50000, damaged)

	before := stateListLoads.Load()
	stats, err := runCopyEngine("fastcopy", src, filepath.Join(t.TempDir(), "dst"), NewFastCopyConfig(), nil, newInterruptHandlerNoSignals())
	if loads := stateListLoads.Load() - before; loads != 1 {
		t.Errorf("the skip list was loaded %d times for one run, want 1", loads)
	}
	if err == nil || stats.damagedSkipped.Load() != 1 || stats.copied.Load() != 299 {
		t.Errorf("want 299 copied and 1 skipped as damaged with an incomplete verdict; got copied=%d skipped=%d err=%v",
			stats.copied.Load(), stats.damagedSkipped.Load(), err)
	}
}

func BenchmarkCopySkipListLookup(b *testing.B) {
	dir := b.TempDir()
	l, _ := loadStateList(filepath.Join(dir, copySkipListName))
	stamp := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 50000; i++ {
		l.Add(fmt.Sprintf(`Z:\nowhere\file%06d.bin`, i), int64(i), stamp)
	}
	l.Close()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		list, _ := loadStateList(filepath.Join(dir, copySkipListName))
		for i := 0; i < 100000; i++ {
			list.Has(fmt.Sprintf(`Z:\other\file%06d.bin`, i), int64(i), stamp)
		}
	}
}

// ---------------------------------------------------------------------------
// COPY-17: one lock for the current file (the -race proof needs cgo; this
// exercises the readers and writers together).
// ---------------------------------------------------------------------------

func TestFastCopyProgressCurrentFileUnderOneLock(t *testing.T) {
	p := &FastCopyProgress{}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				p.setCurrentFileProgress(strings.Repeat(string(rune('a'+w)), 1+i%200), int64(i), int64(i))
				p.getDisplayFile()
			}
		}(w)
	}
	for i := 0; i < 20000; i++ {
		name, size, done := p.getCurrentFileProgress()
		if name != "" && strings.Trim(name, string(name[0])) != "" {
			t.Fatalf("a torn current-file name was read: %q", name)
		}
		_, _ = size, done
	}
	close(stop)
	wg.Wait()
}

// ---------------------------------------------------------------------------
// COPY-18: the size estimate neither races nor leaks on a timeout.
// ---------------------------------------------------------------------------

func TestQuickEstimateDoesNotLeakOnTimeout(t *testing.T) {
	dir := t.TempDir()
	release := make(chan struct{})
	prev := estimateWalk
	estimateWalk = func(root string, fn filepath.WalkFunc) error {
		info, _ := os.Stat(root)
		fn(root, info, nil)
		<-release
		return nil
	}
	defer func() { estimateWalk = prev }()

	base := runtime.NumGoroutine()
	start := time.Now()
	size, files := quickDirectorySizeEstimate(dir, 100*time.Millisecond)
	if took := time.Since(start); took > time.Second {
		t.Errorf("the estimate returned after %v, want about its 100 ms budget", took)
	}
	if size <= 0 || files <= 0 {
		t.Errorf("a timed-out estimate returned %d bytes, %d files", size, files)
	}
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine() - base; n > 0 {
		t.Errorf("%d goroutine(s) outlive a timed-out estimate", n)
	}
}

// ---------------------------------------------------------------------------
// B3 and COPY-19: what "already copied" means, and a stat that times out.
// ---------------------------------------------------------------------------

func TestSkipDecision(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	mk := func(name string, body []byte, mod time.Time) (string, os.FileInfo) {
		p := filepath.Join(dir, name)
		writeFile(t, p, body)
		os.Chtimes(p, mod, mod)
		info, _ := os.Stat(p)
		return p, info
	}
	_, src := mk("src.bin", []byte("0123456789"), stamp)
	_, emptySrc := mk("empty-src", nil, stamp)
	same, _ := mk("same.bin", []byte("abcdefghij"), stamp)
	near, _ := mk("near.bin", []byte("abcdefghij"), stamp.Add(time.Second))
	newer, _ := mk("newer.bin", []byte("abcdefghij"), stamp.Add(time.Hour))
	shorter, _ := mk("shorter.bin", []byte("abc"), stamp)
	empty, _ := mk("empty.bin", nil, stamp.Add(time.Hour))
	folder := filepath.Join(dir, "folder")
	os.MkdirAll(folder, 0o755)

	cases := []struct {
		name string
		src  os.FileInfo
		dst  string
		want skipVerdict
	}{
		{"absent", src, filepath.Join(dir, "absent.bin"), copyNeeded},
		{"same size and time", src, same, skipAlreadyCopied},
		{"time within the FAT step", src, near, skipAlreadyCopied},
		{"same size, other time", src, newer, skipTargetDiffers},
		{"other size", src, shorter, skipTargetDiffers},
		{"empty leftover", src, empty, copyReplaceEmpty},
		{"two empty files", emptySrc, empty, skipAlreadyCopied},
		{"a folder is there", src, folder, skipTargetDiffers},
		{"a partial is never a copy", src, same + partialSuffix, skipTargetDiffers},
	}
	for _, c := range cases {
		if got, _ := skipDecision(c.src, c.dst); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}

	// COPY-19: a target that does not answer is not absent.
	prev := targetStat
	targetStat = func(path string) (os.FileInfo, error) {
		return nil, fmt.Errorf("%s: %w after 10s", path, errStatTimeout)
	}
	defer func() { targetStat = prev }()
	got, err := skipDecision(src, filepath.Join(dir, "absent.bin"))
	if got != skipUnverified || err == nil {
		t.Fatalf("a timed-out stat gave %v (%v), want unverified", got, err)
	}
}

func TestCopyStatTimeoutSkipsAndCounts(t *testing.T) {
	isolateState(t)
	src := filepath.Join(t.TempDir(), "src")
	writeFile(t, filepath.Join(src, "one.bin"), []byte("payload"))
	dst := filepath.Join(t.TempDir(), "dst")
	// The target exists and must not be overwritten when it cannot be seen.
	writeFile(t, filepath.Join(dst, "one.bin"), []byte("keep me"))

	prev := targetStat
	targetStat = func(path string) (os.FileInfo, error) {
		return nil, fmt.Errorf("%s: %w after 10s", path, errStatTimeout)
	}
	defer func() { targetStat = prev }()

	stats, err := runCopyEngine("fastcopy", src, dst, NewFastCopyConfig(), nil, newInterruptHandlerNoSignals())
	if err == nil || stats.unverified.Load() != 1 {
		t.Fatalf("a stat timeout was not counted as unverified: unverified=%d err=%v", stats.unverified.Load(), err)
	}
	if got := mustRead(t, filepath.Join(dst, "one.bin")); string(got) != "keep me" {
		t.Fatalf("a target that could not be looked at was overwritten")
	}
}

// ---------------------------------------------------------------------------
// COPY-10: a copy into its own subfolder is refused.
// ---------------------------------------------------------------------------

func TestCopyIntoOwnSubfolderRefused(t *testing.T) {
	wd := t.TempDir()
	src := filepath.Join(wd, "Data")
	writeFile(t, filepath.Join(src, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(src, "sub", "b.txt"), []byte("b"))
	backup := filepath.Join(src, "backup")

	rows := [][]string{
		{"copy", src, backup},
		{"fastcopy", src, backup},
		{"safecopy", src, strings.ToLower(src) + `\backup`},
		{"folder", src, "copy", backup},
		{"smartcopy", src, backup},
		{"fastcopy", src, src},
	}
	for _, args := range rows {
		out, code := run(t, wd, args...)
		if code != 2 {
			t.Errorf("%v exited %d, want 2\n%s", args, code, out)
		}
		if exists(backup) {
			t.Fatalf("%v created %s", args, backup)
		}
	}
	lst := filepath.Join(wd, "nest.lst")
	writeFile(t, lst, []byte("folder "+src+" copy "+backup+"\r\nfastcopy "+src+" "+backup+"\r\n"))
	if out, code := run(t, wd, "from", lst); code != 2 || exists(backup) {
		t.Errorf("batch nested copy exited %d (want 2), backup exists=%v\n%s", code, exists(backup), out)
	}
}
