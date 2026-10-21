//go:build windows

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// fill, fill verify and clean for every target kind (SP-0026 CAP-02, CAP-08,
// CAP-14, CAP-17, CAP-18). Device, folder and network used to carry three
// copies of each; the folder and network fills wrote a template file that
// `fill verify` could not read, so every healthy folder "failed". There is now
// one fill, writing with the one test-file writer, one verify and one clean.

// ---------------------------------------------------------------------------
// The fill core

// fillBufferSize is each fill writer's buffer: twelve device writers stay well
// inside a 32-bit process's address space.
const fillBufferSize = 4 * 1024 * 1024

// writeFillFile writes one fill file - with the same writer, and so in the
// same format, as the capacity test's files (CAP-02).
func writeFillFile(ctx context.Context, path string, size int64, progress *atomic.Int64) (bool, error) {
	return writeCapacityFile(ctx, path, size, fillBufferSize, progress)
}

// fillWriteFile is the writer a fill uses. A variable, so a test can stand a
// slow or failing writer in (CAP-08).
var fillWriteFile = writeFillFile

// fillStallTimeout is how long a fill may go without writing a byte before it
// is judged stalled. There is no wall-clock limit on a file: a slow stick
// writing a 1 GB file for minutes is making progress, and progress is what is
// watched (CAP-08). The writers flush every fillSyncEvery bytes, so the
// longest silent stretch is twelve such flushes on the slowest device.
var fillStallTimeout = 5 * time.Minute

// fillSyncEvery is how much a fill writer hands the cache between flushes.
const fillSyncEvery = 32 * 1024 * 1024

// fillJoinBudget bounds the wait for writers after a stop; a write stuck in
// the kernel on a dead device must not turn "stop" into "never".
var fillJoinBudget = 30 * time.Second

// errFillStalled is the watchdog's verdict.
var errFillStalled = errors.New("no data was written for too long - the device stopped responding")

// fillJob is one fill's parameters.
type fillJob struct {
	FileSize    int64
	MaxFiles    int64
	Parallelism int
	// Path is the i-th file's path (1-based), made when it is needed rather
	// than for every file up front (CAP-18).
	Path func(i int64) string
}

// fillResult is what a fill did.
type fillResult struct {
	done       []uint64 // bit i-1 set: file i was written completely
	Completed  int64
	Bytes      int64
	Cause      error // nil when every planned file was written
	Stragglers bool  // writers still running when the join budget ran out
}

func (r *fillResult) markDone(i int64) {
	r.done[(i-1)/64] |= 1 << uint((i-1)%64)
}

// eachCompleted calls fn with the index of every completed file.
func (r *fillResult) eachCompleted(fn func(i int64)) {
	for w, bits := range r.done {
		for bits != 0 {
			b := int64(0)
			for bits&(1<<uint(b)) == 0 {
				b++
			}
			bits &^= 1 << uint(b)
			fn(int64(w)*64 + b + 1)
		}
	}
}

// runFillFiles writes up to job.MaxFiles files with job.Parallelism writers.
// Writers take the context (CAP-17); names are made on demand and the queues
// hold about two items per writer (CAP-18); a watchdog on bytes written - not
// a timer per file - decides a stall (CAP-08). Every writer is joined before
// it returns, and every partial file is removed, so what is on disk
// afterwards is exactly result's completed files.
func runFillFiles(parent context.Context, job fillJob, onFile func(completed, bytes int64)) fillResult {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)

	par := job.Parallelism
	if par < 1 {
		par = 1
	}
	res := fillResult{done: make([]uint64, (job.MaxFiles+63)/64)}

	type outcome struct {
		index   int64
		path    string
		created bool
		err     error
	}
	jobs := make(chan int64, 2*par)
	results := make(chan outcome, 2*par)
	var progress atomic.Int64
	var wg sync.WaitGroup

	for w := 0; w < par; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue // drain: the producer stops on the same signal
				}
				p := job.Path(i)
				created, err := fillWriteFile(ctx, p, job.FileSize, &progress)
				results <- outcome{index: i, path: p, created: created, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := int64(1); i <= job.MaxFiles; i++ {
			select {
			case jobs <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	joined := make(chan struct{})
	go func() {
		wg.Wait()
		close(results)
		close(joined)
	}()
	go func() {
		tick := fillStallTimeout / 8
		if tick < 10*time.Millisecond {
			tick = 10 * time.Millisecond
		}
		if tick > 5*time.Second {
			tick = 5 * time.Second
		}
		t := time.NewTicker(tick)
		defer t.Stop()
		last, lastAt := progress.Load(), time.Now()
		for {
			select {
			case <-joined:
				return
			case <-ctx.Done():
				return
			case now := <-t.C:
				if v := progress.Load(); v != last {
					last, lastAt = v, now
					continue
				}
				if now.Sub(lastAt) >= fillStallTimeout {
					cancel(errFillStalled)
					return
				}
			}
		}
	}()

	var partials []string
	stopped := ctx.Done()
	var budget <-chan time.Time
collect:
	for {
		select {
		case o, ok := <-results:
			if !ok {
				break collect
			}
			if o.err == nil {
				res.markDone(o.index)
				res.Completed++
				res.Bytes += job.FileSize
				if onFile != nil {
					onFile(res.Completed, res.Bytes)
				}
				continue
			}
			if o.created {
				partials = append(partials, o.path)
			}
			if res.Cause == nil && ctx.Err() == nil {
				res.Cause = o.err
				cancel(o.err)
			}
		case <-stopped:
			stopped = nil
			budget = time.After(fillJoinBudget)
		case <-budget:
			res.Stragglers = true
			go func() {
				for range results {
				}
			}()
			break collect
		}
	}

	for _, p := range partials {
		os.Remove(p)
	}
	if res.Cause == nil && ctx.Err() != nil {
		res.Cause = context.Cause(ctx)
	}
	return res
}

// ---------------------------------------------------------------------------
// fill

// fillParallelism is the writer count per target kind: the device fill has
// always written twelve files at once; folders and shares one.
func fillParallelism(kind string) int {
	if kind == "Device" {
		return 12
	}
	return 1
}

// runCapacityFill fills a target with test files down to its reserve. kind is
// "Device", "Folder" or "Network".
func runCapacityFill(kind, targetPath, sizeStr string, autoDelete bool, logger *HistoryLogger) error {
	if logger != nil {
		logger.SetCommand(strings.ToLower(kind), targetPath, "fill")
		logger.SetParameter("size", sizeStr)
		logger.SetParameter("autoDelete", autoDelete)
	}
	fail := func(err error) error {
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	sizeMB, err := parseSizeMB(sizeStr, 1, 10240)
	if err != nil {
		return fail(err)
	}

	fmt.Printf("%s Fill Operation\n", kind)
	fmt.Printf("Target: %s\n", describeCapacityTarget(kind, targetPath))
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	if st, err := os.Stat(targetPath); err != nil {
		return fail(fmt.Errorf("%s path is not accessible: %w", strings.ToLower(kind), err))
	} else if !st.IsDir() {
		return fail(fmt.Errorf("path is not a directory: %s", targetPath))
	}
	if err := probeWritable(targetPath); err != nil {
		return fail(err)
	}
	free, _, err := capacityFreeSpace(targetPath)
	if err != nil {
		return fail(fmt.Errorf("failed to get disk space information: %w", err))
	}
	plan, err := planFill(free, sizeMB, capacityVolumeFacts(targetPath))
	if err != nil {
		return fail(err)
	}

	fmt.Printf("Available space: %.2f GB\n", gbOf(free))
	for _, note := range plan.Notes {
		fmt.Printf("Note: %s\n", note)
	}
	fmt.Printf("File size: %d MB\n", plan.FileSize/capMiB)
	fmt.Printf("Maximum files to create: %d\n", plan.MaxFiles)
	fmt.Printf("Total space to fill: %.2f GB\n\n", gbOf(plan.MaxFiles*plan.FileSize))

	dir := targetPath
	if plan.SubDir != "" {
		dir = filepath.Join(targetPath, plan.SubDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fail(fmt.Errorf("could not create %s: %w", dir, err))
		}
	}
	runID := capacityRunID(newCapacityNonce())
	stamp := time.Now().Format(capacityStampLayout)
	width := len(fmt.Sprint(plan.MaxFiles))
	if width < 5 {
		width = 5
	}

	fmt.Printf("Starting fill operation..\n")
	progress := NewProgressTrackerWithInterval(plan.MaxFiles, plan.MaxFiles*plan.FileSize, 2*time.Second)
	ctx := capacityContext()
	out := runFillFiles(ctx, fillJob{
		FileSize:    plan.FileSize,
		MaxFiles:    plan.MaxFiles,
		Parallelism: fillParallelism(kind),
		Path: func(i int64) string {
			return filepath.Join(dir, capacityFileName(i, width, stamp, runID))
		},
	}, func(completed, bytes int64) {
		progress.Update(completed, bytes)
		progress.PrintProgress("Fill")
	})
	progress.Update(out.Completed, out.Bytes)
	progress.Finish("Fill Operation")

	runNumber("filesCreated", out.Completed)
	runNumber("bytesWritten", out.Bytes)
	if out.Stragglers {
		fmt.Printf("\n⚠ Some writers had not stopped within %s; the files they were writing may remain.\n", fillJoinBudget)
	}

	if autoDelete && out.Completed > 0 {
		fmt.Printf("\nAuto-delete enabled - deleting the %d files this fill created..\n", out.Completed)
		var paths []string
		out.eachCompleted(func(i int64) {
			paths = append(paths, filepath.Join(dir, capacityFileName(i, width, stamp, runID)))
		})
		deleted, freed, failed := deleteFiles(paths)
		fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", deleted, gbOf(freed))
		if failed > 0 {
			fmt.Printf("Warning: %d files could not be deleted\n", failed)
		}
		if plan.SubDir != "" {
			os.Remove(dir)
		}
	} else if out.Completed > 0 {
		fmt.Printf("\nCheck the written data with: filedo %s fill verify\n", quoteIfNeeded(targetPath))
		fmt.Printf("Remove the files with:       %s\n", cleanupHint(targetPath))
	}

	endErr := fillEndError(ctx, out, plan)
	if logger != nil {
		logger.SetResult("filesCreated", out.Completed)
		logger.SetResult("totalGBWritten", gbOf(out.Bytes))
		logger.SetResult("autoDeleteUsed", autoDelete)
		if endErr == nil {
			logger.SetSuccess()
		} else {
			logger.SetError(endErr)
		}
	}
	return endErr
}

// fillEndError is the one reading of how a fill ended (CAP-08): only a fill
// that wrote every planned file - or ran into the genuinely full file system,
// which is what a fill is for - is done. Every other early end is an error
// that says how far it got.
func fillEndError(ctx context.Context, out fillResult, plan fillPlan) error {
	written := fmt.Sprintf("%d of %d files (%.2f GB)", out.Completed, plan.MaxFiles, gbOf(out.Bytes))
	switch {
	case out.Cause == nil:
		return nil
	case isDiskFullError(out.Cause):
		fmt.Printf("\nThe file system is full after %s - fill complete.\n", written)
		return nil
	case ctx.Err() != nil || errors.Is(out.Cause, errRunStopped):
		return fmt.Errorf("fill stopped after %s: %w", written, errRunStopped)
	case errors.Is(out.Cause, errFillStalled):
		return fmt.Errorf("fill could not finish after %s: %w - nothing is proven about the rest of the device", written, errFillStalled)
	}
	return ioJudgement(out.Cause, "fill stopped after %s", written)
}

// deleteFiles removes paths with a small worker pool and reports the counts.
func deleteFiles(paths []string) (deleted int64, freed int64, failed int64) {
	jobs := make(chan string, 64)
	var wg sync.WaitGroup
	workers := 24
	if len(paths) < workers {
		workers = len(paths)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				var size int64
				if info, err := os.Stat(p); err == nil {
					size = info.Size()
				}
				if err := os.Remove(p); err != nil {
					if !os.IsNotExist(err) {
						fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(p), err)
						atomic.AddInt64(&failed, 1)
					}
					continue
				}
				atomic.AddInt64(&deleted, 1)
				atomic.AddInt64(&freed, size)
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		for _, p := range paths {
			jobs <- p
		}
		close(jobs)
		wg.Wait()
		close(done)
	}()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	total := len(paths)
	for {
		select {
		case <-tick.C:
			fmt.Printf("Deleted %d/%d files - %.2f GB freed\r", atomic.LoadInt64(&deleted), total, gbOf(atomic.LoadInt64(&freed)))
		case <-done:
			fmt.Printf("Deleted %d/%d files - %.2f GB freed\r", deleted, total, gbOf(freed))
			return deleted, freed, failed
		}
	}
}

func quoteIfNeeded(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}

// describeCapacityTarget is the target line of a fill or verify header.
func describeCapacityTarget(kind, path string) string {
	if kind == "Device" {
		return getEnhancedDeviceInfo(path)
	}
	return path
}

// capacityDirs are the folders a target's test files can be in: the target,
// and the subfolder a FAT12/16 run writes into.
func capacityDirs(targetPath string) []string {
	dirs := []string{targetPath}
	sub := filepath.Join(targetPath, fatSubdirName)
	if st, err := os.Stat(sub); err == nil && st.IsDir() {
		dirs = append(dirs, sub)
	}
	return dirs
}

// ---------------------------------------------------------------------------
// fill verify

type fillFileEntry struct {
	path  string
	name  string
	seq   int64
	runID string
	size  int64
}

// findFillFiles lists the files named exactly like FileDO test files, in
// sequence order.
func findFillFiles(targetPath string) ([]fillFileEntry, error) {
	var out []fillFileEntry
	for _, dir := range capacityDirs(targetPath) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			seq, runID, ok := fillNameInfo(e.Name())
			if !ok {
				continue
			}
			f := fillFileEntry{path: filepath.Join(dir, e.Name()), name: e.Name(), seq: seq, runID: runID}
			if info, err := e.Info(); err == nil {
				f.size = info.Size()
			}
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].seq != out[j].seq {
			return out[i].seq < out[j].seq
		}
		return out[i].name < out[j].name
	})
	return out, nil
}

type fillCheckState int

const (
	fillCheckOK fillCheckState = iota
	fillCheckOKLegacy
	fillCheckBad
	fillCheckNoHeader
	fillCheckUnreadable
)

type fillCheck struct {
	file   fillFileEntry
	state  fillCheckState
	detail string
	head   []byte // the first bytes, for a file with no header
	// incomplete marks a file an interrupted fill left: cut short or empty,
	// with everything that was written intact. It proves nothing either way.
	incomplete bool
}

// fillVerifySamples is how many body blocks fill verify reads per file, beside
// the header and the footer.
const fillVerifySamples = 2

// checkFillFile reads one FILL file back past the cache.
func checkFillFile(f fillFileEntry) fillCheck {
	c := fillCheck{file: f}
	judgeErr := func(err error) fillCheck {
		if isEvidence(err) {
			c.state = fillCheckBad
		} else {
			c.state = fillCheckUnreadable
		}
		c.detail = err.Error()
		return c
	}
	r, err := openForVerify(f.path)
	if err != nil {
		return judgeErr(err)
	}
	head := make([]byte, tfBlockSize)
	n, rerr := r.ReadAt(head, 0)
	size := r.Size()
	r.Close()
	if n == 0 && rerr != nil && !errors.Is(rerr, io.EOF) {
		return judgeErr(rerr)
	}
	head = head[:n]
	if size == 0 {
		// CREATE_NEW, then an exit before the first write: a file size is
		// file-system metadata a fake controller does not change (AUD-05-F1).
		c.state = fillCheckUnreadable
		c.incomplete = true
		c.detail = "empty - left by an interrupted fill"
		return c
	}
	line, ok := headerLine(head)
	switch {
	case ok && strings.HasPrefix(line, tfHeaderPrefix):
		if meta, err := parseTestFileHeader(line); err == nil && meta.Name == f.name && size < meta.Size {
			// The writer is sequential: an interrupted fill leaves the
			// header and a prefix of the body. What was written must read
			// back; what was never written is not evidence (AUD-05-F1).
			if err := verifyFillPrefix(f.path, meta, fillVerifySamples); err != nil {
				return judgeErr(err)
			}
			c.state = fillCheckUnreadable
			c.incomplete = true
			c.detail = fmt.Sprintf("incomplete - left by an interrupted fill (%d of %d bytes written; what was written reads back intact)", size, meta.Size)
			return c
		}
		if err := verifyTestFileSampled(f.path, fillVerifySamples); err != nil {
			return judgeErr(err)
		}
		c.state = fillCheckOK
		return c
	case ok && strings.HasPrefix(line, tfLegacyPrefix):
		return checkLegacyFillFile(c, line, size)
	}
	c.state = fillCheckNoHeader
	c.head = head
	return c
}

// verifyFillPrefix reads back, past the cache, the part of a cut-short
// current-format file that was written: its first block, its last written
// block and k sampled blocks between them. Any byte that is not the one
// written is a tfMismatchError, as in verifyTestFileWith.
func verifyFillPrefix(path string, meta *testFileMeta, k int) error {
	r, err := openForVerify(path)
	if err != nil {
		return fmt.Errorf("could not open test file: %w", err)
	}
	defer r.Close()
	present := r.Size()
	if present > meta.Size {
		present = meta.Size
	}
	got := make([]byte, tfBlockSize)
	want := make([]byte, tfBlockSize)
	check := func(b int64) error {
		off := b * tfBlockSize
		n := int64(tfBlockSize)
		if off+n > present {
			n = present - off
		}
		if n <= 0 {
			return nil
		}
		nr, err := r.ReadAt(got[:n], off)
		if int64(nr) < n {
			if err == nil || errors.Is(err, io.EOF) {
				return &tfMismatchError{Path: path, Offset: off + int64(nr), What: "is missing - the file ends early"}
			}
			return fmt.Errorf("could not read offset %d: %w", off, err)
		}
		return meta.check(path, got[:n], off, want)
	}
	blocks := (present + tfBlockSize - 1) / tfBlockSize
	picks := []int64{0}
	if blocks > 1 {
		picks = append(picks, blocks-1)
	}
	if blocks > 2 {
		picks = append(picks, stratifiedBlocks(1, blocks-2, k)...)
	}
	for _, b := range picks {
		if err := check(b); err != nil {
			return err
		}
	}
	return nil
}

// checkLegacyFillFile is the old format's check, kept for one release: the
// header must name the file, and the footer must repeat the header.
func checkLegacyFillFile(c fillCheck, line string, size int64) fillCheck {
	embedded, ok := legacyHeaderName(line)
	if !ok || embedded != c.file.name {
		c.state = fillCheckBad
		c.detail = fmt.Sprintf("contains: %s", embedded)
		if embedded == "" {
			c.detail = "contains: (no valid header)"
		}
		return c
	}
	r, err := openForVerify(c.file.path)
	if err != nil {
		c.state = fillCheckUnreadable
		c.detail = err.Error()
		return c
	}
	defer r.Close()
	footer := make([]byte, len(line))
	if size < int64(2*len(line)) {
		c.state = fillCheckBad
		c.detail = "file is too short to hold its footer"
		return c
	}
	n, err := r.ReadAt(footer, size-int64(len(line)))
	if n < len(footer) {
		if err != nil && !errors.Is(err, io.EOF) && !isEvidence(err) {
			c.state = fillCheckUnreadable
			c.detail = err.Error()
			return c
		}
		c.state = fillCheckBad
		c.detail = "the footer could not be read"
		return c
	}
	if string(footer) != line {
		c.state = fillCheckBad
		c.detail = "the footer does not repeat the header"
		return c
	}
	c.state = fillCheckOKLegacy
	return c
}

// runCapacityFillVerify reads back every FILL file on a target (CAP-02,
// CAP-03, CAP-08). A file that holds another file's data, or loses its data,
// is a defect; a file that is not FileDO's or cannot be read proves nothing;
// and a volume the FILL files do not cover is not proven, because the space
// nobody wrote is exactly where a counterfeit hides.
func runCapacityFillVerify(kind, targetPath string) error {
	fmt.Printf("Fill Verify Operation (Fake Capacity Detection)\n")
	fmt.Printf("Target: %s\n", describeCapacityTarget(kind, targetPath))
	fmt.Printf("How it works: every FILL file is read back past the Windows cache - its header,\n")
	fmt.Printf("  its footer and sampled blocks, each of which names its file, its offset and\n")
	fmt.Printf("  its run. A fake controller that wraps or drops writes is caught by what it returns.\n")
	fmt.Printf("For the strongest result, unplug and re-plug the device before verifying, so\n")
	fmt.Printf("  nothing it holds can be answered from a cache.\n\n")

	files, err := findFillFiles(targetPath)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}
	if len(files) == 0 {
		fmt.Printf("No FILL test files found in %s\n", targetPath)
		return fmt.Errorf("no FILL test files to verify in %s - run 'filedo %s fill' first; nothing was verified",
			targetPath, quoteIfNeeded(targetPath))
	}
	var totalBytes, maxSize int64
	for _, f := range files {
		totalBytes += f.size
		if f.size > maxSize {
			maxSize = f.size
		}
	}
	fmt.Printf("Found %d FILL files (%.2f GB of test data)\n", len(files), gbOf(totalBytes))
	fmt.Printf("Verifying..\n\n")
	runStep("verify", fmt.Sprintf("Reading back %d FILL files", len(files)))

	checks := make([]fillCheck, len(files))
	jobs := make(chan int, 64)
	var wg sync.WaitGroup
	var checked atomic.Int64
	workers := 8
	if len(files) < workers {
		workers = len(files)
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				checks[i] = checkFillFile(files[i])
				checked.Add(1)
			}
		}()
	}
	progress := NewProgressTrackerWithInterval(int64(len(files)), totalBytes, 2*time.Second)
	go func() {
		for i := range files {
			if runStopRequested() {
				break
			}
			jobs <- i
		}
		close(jobs)
	}()
	waitDone := make(chan struct{})
	go func() { wg.Wait(); close(waitDone) }()
	tick := time.NewTicker(500 * time.Millisecond)
wait:
	for {
		select {
		case <-waitDone:
			break wait
		case <-tick.C:
			progress.Update(checked.Load(), 0)
			progress.PrintProgress("Verify")
		}
	}
	tick.Stop()
	if runStopRequested() {
		return fmt.Errorf("fill verify stopped after %d of %d files: %w", checked.Load(), len(files), errRunStopped)
	}

	// A file whose header is gone is FileDO's when another file of the same
	// run - same run id in the name - read back intact: its name was written
	// by this program, and its data is what went missing.
	intactRuns := map[string]bool{}
	for _, c := range checks {
		if c.state == fillCheckOK && c.file.runID != "" {
			intactRuns[c.file.runID] = true
		}
	}
	var good, goodLegacy, bad, notOurs, unreadable, incomplete int
	var goodBytes int64
	var badExamples, otherExamples []fillCheck
	for i := range checks {
		c := &checks[i]
		if c.state == fillCheckNoHeader && c.file.runID != "" && intactRuns[c.file.runID] {
			c.state = fillCheckBad
			var none *testFileMeta
			c.detail = "lost its FileDO header: it " + none.describeForeign(c.head)
		}
		switch c.state {
		case fillCheckOK:
			good++
			goodBytes += c.file.size
		case fillCheckOKLegacy:
			goodLegacy++
			goodBytes += c.file.size
		case fillCheckBad:
			bad++
			if len(badExamples) < 5 {
				badExamples = append(badExamples, *c)
			}
		case fillCheckNoHeader:
			notOurs++
			if len(otherExamples) < 5 {
				otherExamples = append(otherExamples, *c)
			}
		case fillCheckUnreadable:
			unreadable++
			if c.incomplete {
				incomplete++
			}
			if len(otherExamples) < 5 {
				otherExamples = append(otherExamples, *c)
			}
		}
	}

	fmt.Printf("\nResults:\n")
	fmt.Printf("  Total files:      %d\n", len(files))
	fmt.Printf("  Read back intact: %d\n", good+goodLegacy)
	fmt.Printf("  Data WRONG:       %d\n", bad)
	if notOurs > 0 {
		fmt.Printf("  Not FileDO's:     %d (no FileDO header)\n", notOurs)
	}
	if unreadable > 0 {
		fmt.Printf("  Unreadable:       %d\n", unreadable)
	}
	fmt.Printf("\n")

	runNumber("filesChecked", len(files))
	runNumber("headersOK", good+goodLegacy)
	runNumber("headersWrong", bad)
	runNumber("unreadable", unreadable)
	if notOurs > 0 {
		runNumber("notFileDO", notOurs)
	}
	if goodLegacy > 0 {
		runNumber("oldFormatFiles", goodLegacy)
	}

	if bad > 0 {
		realGB := gbOf(goodBytes)
		claimedGB := gbOf(totalBytes)
		// The finding is what makes it a `Failed` verdict and exit 1
		// (CLI-EVENT-STREAM rule 11).
		runDefect("fake-capacity", fmt.Sprintf("%d of %d FILL files do not hold the data written to them - real capacity is about %.1f GB, not %.1f GB",
			bad, len(files), realGB, claimedGB),
			map[string]interface{}{
				"claimedCapacityGB": claimedGB,
				"realCapacityGB":    realGB,
				"overwrittenFiles":  bad,
			})
		fmt.Printf("⚠ FAKE CAPACITY DETECTED!\n")
		fmt.Printf("  Claimed capacity: ~%.1f GB (%d files)\n", claimedGB, len(files))
		fmt.Printf("  Real capacity:    ~%.1f GB (%d files with intact data)\n", realGB, good+goodLegacy)
		fmt.Printf("  Data WRONG:        %d files (%.1f GB of data is LOST)\n\n", bad, claimedGB-realGB)
		fmt.Printf("Examples of corrupted files:\n")
		for _, c := range badExamples {
			fmt.Printf("  %-36s → %s\n", c.file.name, c.detail)
		}
		return nil
	}

	if notOurs > 0 || unreadable > 0 {
		fmt.Printf("Files that could not be verified:\n")
		for _, c := range otherExamples {
			detail := c.detail
			if c.state == fillCheckNoHeader {
				detail = "no FileDO header - written by something else, or by an older FileDO fill that wrote none"
			}
			fmt.Printf("  %-36s → %s\n", c.file.name, detail)
		}
		if incomplete > 0 {
			fmt.Printf("%d files were left by an interrupted fill. Remove them with: %s\n", incomplete, cleanupHint(targetPath))
		}
		return fmt.Errorf("could not verify %d of %d FILL files (%d without a FileDO header, %d unreadable) - re-run fill and verify again",
			notOurs+unreadable, len(files), notOurs, unreadable)
	}

	// Coverage: the space nobody wrote is exactly where a counterfeit hides.
	free, _, err := capacityFreeSpace(targetPath)
	if err != nil {
		return fmt.Errorf("could not measure the free space, so the coverage is unknown: %w", err)
	}
	facts := capacityVolumeFacts(targetPath)
	if slack := fillCoverageSlack(facts, maxSize); free > slack {
		runNumber("uncoveredGB", gbOf(free))
		return fmt.Errorf("not proven: the FILL files cover %.2f GB, but %.2f GB of the volume is still free and was never written - run 'filedo %s fill' to cover it, then verify again",
			gbOf(totalBytes), gbOf(free), quoteIfNeeded(targetPath))
	}

	if goodLegacy > 0 {
		note := fmt.Sprintf("%d files are in the old format - re-run fill for the strong check (their headers and footers were checked, not their data)", goodLegacy)
		fmt.Printf("Note: %s.\n", note)
		EmitNoteEvent(note)
	}
	if facts.SystemVolume {
		fmt.Printf("Note: the %.1f GB kept free on the system volume was not tested.\n", gbOf(fillReserve(facts)))
	}
	fmt.Printf("✓ GENUINE: all %d files read back intact and cover the volume - storage capacity appears real.\n", len(files))
	return nil
}

// ---------------------------------------------------------------------------
// clean

// cleanCandidate is one file clean may remove.
type cleanCandidate struct {
	path string
	size int64
}

// fileStartsWith reports whether the file's first bytes are one of prefixes.
func fileStartsWith(path string, prefixes ...string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 64)
	n, _ := io.ReadFull(f, buf)
	for _, p := range prefixes {
		if strings.HasPrefix(string(buf[:n]), p) {
			return true
		}
	}
	return false
}

// speedtestBlockHeader is the first line createRandomFile writes - the speed
// test's files, and the old folder and network fills' template.
const speedtestBlockHeader = "=== BLOCK 000001 === START ==="

// findCleanCandidates lists FileDO's own files in a target: an exact FileDO
// name *and* FileDO content (CAP-14). A FILL-named file whose header is gone
// is FileDO's only when a sibling of the same run still carries one.
func findCleanCandidates(targetPath string) (found []cleanCandidate, skipped []string, err error) {
	type entry struct {
		path, name, runID string
		size              int64
	}
	var fills []entry
	for _, dir := range capacityDirs(targetPath) {
		entries, rerr := os.ReadDir(dir)
		if rerr != nil {
			return nil, nil, rerr
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			name := e.Name()
			p := filepath.Join(dir, name)
			var size int64
			if info, ierr := e.Info(); ierr == nil {
				size = info.Size()
			}
			switch {
			case fillNameCurrent.MatchString(name) || fillNameLegacy.MatchString(name):
				_, runID, _ := fillNameInfo(name)
				fills = append(fills, entry{p, name, runID, size})
			case speedtestName.MatchString(name):
				if fileStartsWith(p, speedtestBlockHeader) {
					found = append(found, cleanCandidate{p, size})
				} else {
					skipped = append(skipped, p)
				}
			case probeFileName.MatchString(name):
				if size == 0 || (size == 4 && fileStartsWith(p, "test")) {
					found = append(found, cleanCandidate{p, size})
				} else {
					skipped = append(skipped, p)
				}
			}
		}
	}
	ours := map[string]bool{}
	var headerless []entry
	for _, f := range fills {
		if fileStartsWith(f.path, tfHeaderPrefix, tfLegacyPrefix, speedtestBlockHeader) {
			found = append(found, cleanCandidate{f.path, f.size})
			if f.runID != "" {
				ours[f.runID] = true
			}
			continue
		}
		headerless = append(headerless, f)
	}
	for _, f := range headerless {
		if f.runID != "" && ours[f.runID] {
			found = append(found, cleanCandidate{f.path, f.size})
		} else {
			skipped = append(skipped, f.path)
		}
	}
	return found, skipped, nil
}

// cleanConfirm asks before clean removes anything. It is a variable so a test
// can answer; the real one reads a line from stdin, and a closed stdin - the
// GUI's, or a script's - is no answer, so nothing is removed (CAP-14).
var cleanConfirm = func(prompt string) (answered, yes bool) {
	fmt.Print(prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		fmt.Println()
		return false, false
	}
	a := strings.ToLower(strings.TrimSpace(line))
	return true, a == "y" || a == "yes"
}

// runCapacityClean removes FileDO's test files from a target after listing
// them and asking (skipped by --yes).
func runCapacityClean(targetPath string, assumeYes bool, logger *HistoryLogger) error {
	fmt.Printf("Clean Operation\n")
	fmt.Printf("Target: %s\n", targetPath)
	fmt.Printf("Looking for FileDO's test files (FILL_*.tmp, speedtest_*.txt) - by exact name and content..\n\n")

	if st, err := os.Stat(targetPath); err != nil {
		return fmt.Errorf("path is not accessible: %w", err)
	} else if !st.IsDir() {
		return fmt.Errorf("path is not a directory: %s", targetPath)
	}
	found, skipped, err := findCleanCandidates(targetPath)
	if err != nil {
		return fmt.Errorf("failed to search for test files: %w", err)
	}
	if len(skipped) > 0 {
		fmt.Printf("Left alone: %d files named like FileDO's whose content is not FileDO's.\n", len(skipped))
		for i, p := range skipped {
			if i == 5 {
				fmt.Printf("  .. and %d more\n", len(skipped)-5)
				break
			}
			fmt.Printf("  %s\n", filepath.Base(p))
		}
		fmt.Println()
	}
	if len(found) == 0 {
		fmt.Printf("No FileDO test files found in %s\n", targetPath)
		runNumber("filesDeleted", 0)
		return nil
	}

	var total int64
	for _, c := range found {
		total += c.size
	}
	fmt.Printf("Found %d FileDO test files (%.2f GB):\n", len(found), gbOf(total))
	for i, c := range found {
		if i == 10 {
			fmt.Printf("  .. and %d more\n", len(found)-10)
			break
		}
		fmt.Printf("  %s\n", c.path)
	}
	if assumeYes {
		fmt.Printf("Confirmation skipped (--yes).\n")
	} else {
		answered, yes := cleanConfirm(fmt.Sprintf("\nRemove these %d files? (y/N): ", len(found)))
		if !answered {
			return fmt.Errorf("clean needs a confirmation: %d files listed, nothing removed - pass --yes to remove them without a prompt", len(found))
		}
		if !yes {
			fmt.Printf("Nothing removed.\n")
			runNumber("filesDeleted", 0)
			return nil
		}
	}

	paths := make([]string, len(found))
	for i, c := range found {
		paths[i] = c.path
	}
	fmt.Printf("Deleting files..\n\n")
	deleted, freed, failed := deleteFiles(paths)
	os.Remove(filepath.Join(targetPath, fatSubdirName)) // only when empty
	fmt.Printf("\n\nClean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deleted, len(found))
	fmt.Printf("Space freed: %.2f GB\n", gbOf(freed))
	runNumber("filesDeleted", deleted)
	if logger != nil {
		logger.SetResult("filesDeleted", deleted)
		logger.SetResult("totalGBFreed", gbOf(freed))
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d test files could not be deleted", failed, len(found))
	}
	return nil
}
