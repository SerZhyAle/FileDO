package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CopyProgress tracks the progress of copy operations.
//
// Every int64 touched through sync/atomic comes first: only the start of an
// allocated struct is guaranteed 64-bit aligned on 32-bit platforms, and
// SkippedFiles and DamagedFiles once sat behind a time.Time, where a 386
// build panicked in atomic.LoadInt64 on the first progress line.
type CopyProgress struct {
	TotalFiles       int64        // Use atomic operations
	ProcessedFiles   int64        // Use atomic operations
	TotalSize        int64        // Use atomic operations
	CopiedSize       int64        // Use atomic operations
	SkippedFiles     int64        // Use atomic operations
	DamagedFiles     int64        // Use atomic operations - files that couldn't be read
	StartTime        time.Time    // Read-only after initialization
	CurrentFile      string       // Protected by CurrentFileMutex
	CurrentFileMutex sync.RWMutex // Mutex for CurrentFile access
	TotalsKnown      bool         // The totals were counted before copying (--precount); read-only after
}

// countsText is the "how far along" part of a progress line. With counted
// totals it is done/total; without them the totals are only what the walk has
// found so far, and the line says so instead of passing them off as the end.
func (p *CopyProgress) countsText(doneFiles, doneBytes int64) string {
	const mb = 1024 * 1024
	text := fmt.Sprintf("%d/%d files, %.1f/%.1f MB", doneFiles, p.GetTotalFiles(),
		float64(doneBytes)/mb, float64(p.GetTotalSize())/mb)
	if !p.TotalsKnown {
		text += " found so far"
	}
	return text
}

// etaText is the time left. Without counted totals there is no honest
// estimate - the walk has not seen the rest of the tree - so it says so.
func (p *CopyProgress) etaText(doneBytes int64) string {
	if !p.TotalsKnown {
		return "unknown"
	}
	if doneBytes <= 0 {
		return "unknown"
	}
	remaining := p.GetTotalSize() - doneBytes
	if remaining <= 0 {
		return formatETA(0)
	}
	speed := float64(doneBytes) / time.Since(p.StartTime).Seconds() // bytes per second
	return formatETA(time.Duration(float64(remaining)/speed) * time.Second)
}

// FileOperationTimeout bounds one look at a path (a stat) on a share or a
// failing disk that does not answer. It is never a limit on how long a file
// may take to copy: that is the no-progress watchdog (COPY-11).
const FileOperationTimeout = 10 * time.Second

// Atomic helper methods for CopyProgress
func (p *CopyProgress) AddTotalFiles(delta int64) {
	atomic.AddInt64(&p.TotalFiles, delta)
}

func (p *CopyProgress) AddTotalSize(delta int64) {
	atomic.AddInt64(&p.TotalSize, delta)
}

func (p *CopyProgress) GetTotalFiles() int64 {
	return atomic.LoadInt64(&p.TotalFiles)
}

func (p *CopyProgress) GetTotalSize() int64 {
	return atomic.LoadInt64(&p.TotalSize)
}

func (p *CopyProgress) GetProcessedFiles() int64 {
	return atomic.LoadInt64(&p.ProcessedFiles)
}

func (p *CopyProgress) GetCopiedSize() int64 {
	return atomic.LoadInt64(&p.CopiedSize)
}

func (p *CopyProgress) GetSkippedFiles() int64 {
	return atomic.LoadInt64(&p.SkippedFiles)
}

func (p *CopyProgress) SetCurrentFile(filename string) {
	p.CurrentFileMutex.Lock()
	defer p.CurrentFileMutex.Unlock()
	p.CurrentFile = filename
}

func (p *CopyProgress) GetCurrentFile() string {
	p.CurrentFileMutex.RLock()
	defer p.CurrentFileMutex.RUnlock()
	return p.CurrentFile
}

// statWithTimeout performs os.Stat with timeout
func statWithTimeout(path string, timeout time.Duration) (os.FileInfo, error) {
	type statResult struct {
		info os.FileInfo
		err  error
	}

	ch := make(chan statResult, 1)
	go func() {
		info, err := os.Stat(path)
		ch <- statResult{info, err}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	select {
	case result := <-ch:
		return result.info, result.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w after %v", path, errStatTimeout, timeout)
	}
}

// handleCopyCommand - regular copy with damaged disk protection (the target
// verbs' `copy`: folder, device, network and file).
func handleCopyCommand(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("copy command requires source and target paths")
	}

	sourcePath := args[1]
	targetPath := args[2]
	if err := refuseCopyPaths("copy", sourcePath, targetPath); err != nil {
		return err
	}

	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path does not exist: %s", sourcePath)
	}

	fmt.Printf("Starting copy with damaged disk protection from %s to %s\n", sourcePath, targetPath)

	damagedHandler, err := NewDamagedDiskHandler()
	if err != nil {
		fmt.Printf("Warning: %v - damaged files are skipped for this run only.\n", err)
	}
	defer func() {
		damagedHandler.PrintSummary()
		damagedHandler.Close()
	}()

	if sourceInfo.IsDir() {
		return copyTree("copy", sourcePath, targetPath, damagedHandler)
	}
	return copyFile("copy", sourcePath, targetPath, damagedHandler)
}

// handleCopyCommandNoDamage - regular copy without damaged disk protection (for normal operation)
func handleCopyCommandNoDamage(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("copy command requires source and target paths")
	}

	sourcePath := args[1]
	targetPath := args[2]
	if err := refuseCopyPaths("copy", sourcePath, targetPath); err != nil {
		return err
	}

	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path does not exist: %s", sourcePath)
	}

	fmt.Printf("Starting regular copy from %s to %s\n", sourcePath, targetPath)

	if sourceInfo.IsDir() {
		return copyDirectory(sourcePath, targetPath)
	}
	return copyFile("copy", sourcePath, targetPath, nil)
}

// copyPrecountFlag asks a copy to count the whole tree before the first file
// is copied. The same word `check` takes for the same trade, which is why it
// is read by the copy dispatch alone and never stripped as a global flag.
const copyPrecountFlag = "--precount"

// copyPrecount is --precount for the copy command being run. It is assigned,
// not accumulated, at every copy dispatch, so one line of a .lst batch file
// cannot hand it on to the next.
var copyPrecount bool

// wantsCopyPrecount reports whether the words after a copy command's source
// and target ask for --precount.
func wantsCopyPrecount(rest []string) bool {
	for _, arg := range rest {
		if strings.EqualFold(arg, copyPrecountFlag) {
			return true
		}
	}
	return false
}

// copyWalk is the tree walk every plain copy makes. It is a variable so a test
// can count the walks, which is the claim copyTree exists to keep.
var copyWalk = filepath.Walk

// plainCopyRun is one plain copy: its progress line, its counts, its stop
// and, when damage handling is on, the skip list.
type plainCopyRun struct {
	progress *CopyProgress
	stats    *copyRunStats
	ctx      context.Context
	handler  *InterruptHandler
	damaged  *DamagedDiskHandler
}

func newPlainCopyRun(verb string, damaged *DamagedDiskHandler) *plainCopyRun {
	r := &plainCopyRun{
		progress: &CopyProgress{StartTime: time.Now()},
		stats:    newCopyRunStats(verb),
		ctx:      context.Background(),
		damaged:  damaged,
	}
	if globalInterruptHandler != nil {
		r.handler = globalInterruptHandler
		r.ctx = globalInterruptHandler.Context()
	}
	return r
}

// copyOne copies one source file of a plain copy and counts the outcome.
func (r *plainCopyRun) copyOne(path, target string, info os.FileInfo) {
	progress := r.progress
	if !info.Mode().IsRegular() {
		if info.Mode()&os.ModeSymlink == 0 {
			r.stats.recordSpecial(path)
			return
		}
		ti, err := os.Stat(path)
		if err != nil {
			atomic.AddInt64(&progress.ProcessedFiles, 1)
			r.stats.recordFailure(path, fmt.Errorf("cannot follow the link: %w", err))
			return
		}
		if !ti.Mode().IsRegular() {
			r.stats.recordSpecial(path)
			return
		}
		info = ti
	}
	if isPartialName(info.Name()) {
		r.stats.recordPartialSource(path)
		return
	}
	if r.damaged != nil && r.damaged.ShouldSkip(path, info) {
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		atomic.AddInt64(&progress.SkippedFiles, 1)
		r.stats.recordDamagedSkip(path, r.damaged.SkipListPath())
		return
	}

	v, verr := skipDecision(info, target)
	if !r.stats.admit(v, path, target, verr) {
		currentFile := atomic.AddInt64(&progress.ProcessedFiles, 1)
		atomic.AddInt64(&progress.SkippedFiles, 1)
		currentSize := atomic.AddInt64(&progress.CopiedSize, info.Size())
		if v == skipAlreadyCopied {
			fmt.Printf("Skipped: %s [%s, ETA: %s] - already at the target (same size and time)\n",
				path, progress.countsText(currentFile, currentSize), progress.etaText(currentSize))
		}
		return
	}

	var err error
	if r.damaged != nil {
		err = r.damaged.CopyFileWithDamageHandling(r.ctx, path, target, info, v == copyReplaceEmpty, nil, r.handler)
	} else {
		buf, put := takeCopyBuffer(1 << 20)
		err = copyOneFile(r.ctx, path, info, target, fileCopyOptions{
			Buffer: buf,
			// A no-progress watchdog, never a limit on the total time: a
			// 200 MB file to a 10 MB/s stick takes twenty seconds and is
			// fine; a read that returns nothing for ten is not (COPY-11).
			NoProgress: NewDamagedDiskConfig().FileTimeout,
			Replace:    v == copyReplaceEmpty,
			Handler:    r.handler,
			Release:    put,
		})
	}
	switch {
	case err == nil:
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		atomic.AddInt64(&progress.CopiedSize, info.Size())
		r.stats.recordCopied(info.Size())
		showProgress(progress)
	case isStopError(err):
		// A stop is not this file's failure.
	default:
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		var damaged *damagedSourceError
		if errors.As(err, &damaged) {
			atomic.AddInt64(&progress.DamagedFiles, 1)
		}
		r.stats.recordFailure(path, err)
	}
}

// copyTree copies sourcePath into targetPath in one walk of the tree: each
// directory is created and each file copied as the walk reaches it, so the
// first file moves as soon as it is found instead of after a full counting
// pass (SP-0002 item 5). The totals therefore grow while the copy runs, and
// the progress line says so rather than showing an ETA against half a tree.
// --precount buys the exact totals back with one counting walk first.
//
// The walk obeys a stop: it ends at the next entry, and no target is created
// after it (COPY-04).
func copyTree(verb, sourcePath, targetPath string, damaged *DamagedDiskHandler) error {
	run := newPlainCopyRun(verb, damaged)
	progress := run.progress

	if copyPrecount {
		fmt.Println("Counting files first (--precount)..")
		if err := countTree(sourcePath, progress); err != nil {
			return fmt.Errorf("error scanning directory: %v", err)
		}
		progress.TotalsKnown = true
		fmt.Printf("Found %d files, total size: %.2f MB\n",
			progress.GetTotalFiles(), float64(progress.GetTotalSize())/(1024*1024))
	} else {
		fmt.Println("Copying while the tree is walked - totals grow as files are found (--precount counts them first).")
	}

	var collisions caseCollisionIndex
	copyErr := copyWalk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if run.ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			run.stats.recordFailure(path, fmt.Errorf("cannot read: %w", err))
			return nil
		}

		// For broken sources, handle cases where info might be corrupted
		if info == nil {
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				run.stats.recordFailure(path, statErr)
				return nil
			}
			info = timeoutInfo
		}

		if winner, lost := collisions.winner(path); lost {
			run.stats.recordCollision(path, winner)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			run.stats.recordFailure(path, err)
			return nil
		}
		targetFilePath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			if err := os.MkdirAll(targetFilePath, 0o755); err != nil {
				run.stats.recordFailure(targetFilePath, fmt.Errorf("cannot create the folder: %w", err))
				return filepath.SkipDir
			}
			collisions.scanDir(path)
			return nil
		}

		if !progress.TotalsKnown {
			progress.AddTotalFiles(1)
			progress.AddTotalSize(info.Size())
		}
		progress.SetCurrentFile(path)
		run.copyOne(path, targetFilePath, info)
		return nil
	})
	if copyErr != nil && !isStopError(copyErr) {
		run.stats.recordFailure(sourcePath, copyErr)
	}

	fmt.Printf("\nCopy finished: %d files, %.2f MB in %v\n",
		progress.GetProcessedFiles(), float64(progress.GetCopiedSize())/(1024*1024),
		time.Since(progress.StartTime).Round(time.Millisecond))
	return run.stats.finish()
}

// countTree is the counting walk --precount asks for. It records nothing but
// the totals: a path it cannot read is reported once, by the copying walk
// that follows, not twice.
func countTree(sourcePath string, progress *CopyProgress) error {
	return copyWalk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if runStopRequested() {
			return filepath.SkipAll
		}
		if info == nil {
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				return nil
			}
			info = timeoutInfo
		}
		if !info.IsDir() {
			progress.AddTotalFiles(1)
			progress.AddTotalSize(info.Size())
		}
		return nil
	})
}

// copyDirectory copies entire directory structure
func copyDirectory(sourcePath, targetPath string) error {
	return copyTree("copy", sourcePath, targetPath, nil)
}

// copyFile copies a single file. A target that is a folder (existing, or
// spelled with a trailing separator) receives the file under its own name.
func copyFile(verb, sourcePath, targetPath string, damaged *DamagedDiskHandler) error {
	sourceInfo, err := statWithTimeout(sourcePath, FileOperationTimeout)
	if err != nil {
		return fmt.Errorf("cannot stat source file: %v", err)
	}
	if targetPath, err = singleFileTarget(verb, sourcePath, targetPath); err != nil {
		return err
	}

	run := newPlainCopyRun(verb, damaged)
	run.progress.TotalsKnown = true // one file: the totals are its own size
	run.progress.AddTotalFiles(1)
	run.progress.AddTotalSize(sourceInfo.Size())
	run.progress.SetCurrentFile(sourcePath)

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("error creating target directory: %v", err)
	}
	run.copyOne(sourcePath, targetPath, sourceInfo)
	fmt.Println()
	return run.stats.finish()
}

// showProgress displays current copy progress
func showProgress(progress *CopyProgress) {
	processedFiles := progress.GetProcessedFiles()
	copiedSize := progress.GetCopiedSize()
	damagedFiles := atomic.LoadInt64(&progress.DamagedFiles)

	// Get short filename for display
	currentFile := progress.GetCurrentFile()
	if len(currentFile) > 50 {
		parts := strings.Split(currentFile, string(os.PathSeparator))
		if len(parts) > 2 {
			currentFile = "..." + string(os.PathSeparator) + parts[len(parts)-2] + string(os.PathSeparator) + parts[len(parts)-1]
		}
	}

	// Show damaged files count if any
	damagedInfo := ""
	if damagedFiles > 0 {
		damagedInfo = fmt.Sprintf(", %d damaged", damagedFiles)
	}

	fmt.Printf("\rCopying: %s [%s, ETA: %s%s]",
		currentFile,
		progress.countsText(processedFiles, copiedSize),
		progress.etaText(copiedSize),
		damagedInfo)
}
