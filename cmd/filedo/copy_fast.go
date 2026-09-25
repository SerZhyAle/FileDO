package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// FastCopyConfig contains configuration for optimized copying
type FastCopyConfig struct {
	MaxConcurrentFiles int   // Worker count: the most files copied at once
	MinBufferSize      int   // Minimum buffer size
	MaxBufferSize      int   // Maximum buffer size (clamped to the build's budget)
	LargeFileThreshold int64 // Files at or above this are shown as large files
	PreallocateSpace   bool  // Kept for the strategy report; the copy writes sequentially
	UseMemoryMapping   bool  // Kept for the strategy report; never used
	MemoryMapThreshold int64 // Kept for the strategy report; never used
	SmallFileThreshold int64 // Files smaller than this are handed to workers in batches
	SmallFileBatchSize int   // Files per batch
	DirectIO           bool  // Kept for the strategy report
	ForceFlush         bool  // Kept for the strategy report; every file is flushed before its rename
	SyncReadWrite      bool  // Kept for the strategy report
}

// NewFastCopyConfig creates optimized configuration for different scenarios
func NewFastCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 8,                 // Limited to 8 threads for better balance with HDD
		MinBufferSize:      1024 * 1024,       // 1MB
		MaxBufferSize:      64 * 1024 * 1024,  // 64MB
		LargeFileThreshold: 100 * 1024 * 1024, // 100MB
		PreallocateSpace:   true,
		UseMemoryMapping:   false,
		MemoryMapThreshold: 0,
		SmallFileThreshold: 2 * 1024 * 1024, // 2MB - files smaller than this are batched
		SmallFileBatchSize: 25,              // 25 small files per batch
		DirectIO:           false,
		ForceFlush:         false,
		SyncReadWrite:      false,
	}
}

// NewSyncCopyConfig creates configuration for synchronized copying to match read/write speeds
func NewSyncCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 1,                // Single-threaded - one worker, so truly one file at a time
		MinBufferSize:      4 * 1024 * 1024,  // 4MB
		MaxBufferSize:      16 * 1024 * 1024, // 16MB
		LargeFileThreshold: 50 * 1024 * 1024, // 50MB
		PreallocateSpace:   false,
		UseMemoryMapping:   false,
		MemoryMapThreshold: 0,
		SmallFileThreshold: 1024 * 1024, // 1MB
		SmallFileBatchSize: 5,
		DirectIO:           true,
		ForceFlush:         true,
		SyncReadWrite:      true,
	}
}

// NewBalancedCopyConfig creates configuration optimized for HDD-to-HDD copying
func NewBalancedCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 4,                // Very limited threads for HDD operations
		MinBufferSize:      16 * 1024 * 1024, // 16MB
		MaxBufferSize:      64 * 1024 * 1024, // 64MB
		LargeFileThreshold: 50 * 1024 * 1024, // 50MB
		PreallocateSpace:   true,
		UseMemoryMapping:   false,
		MemoryMapThreshold: 0,
		SmallFileThreshold: 4 * 1024 * 1024, // 4MB
		SmallFileBatchSize: 10,
		DirectIO:           false,
		ForceFlush:         false,
		SyncReadWrite:      false,
	}
}

// NewMaxPerformanceConfig creates configuration for maximum CPU utilization
func NewMaxPerformanceConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 16,                // 16 workers
		MinBufferSize:      8 * 1024 * 1024,   // 8MB
		MaxBufferSize:      128 * 1024 * 1024, // 128MB, clamped to the build's budget
		LargeFileThreshold: 50 * 1024 * 1024,  // 50MB
		PreallocateSpace:   true,
		UseMemoryMapping:   false,
		MemoryMapThreshold: 0,
		SmallFileThreshold: 512 * 1024, // 512KB
		SmallFileBatchSize: 50,
		DirectIO:           false,
		ForceFlush:         false,
		SyncReadWrite:      false,
	}
}

// NewSafeConfig creates configuration for problematic/damaged drives
func NewSafeConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 1,               // Single thread to minimize drive stress
		MinBufferSize:      64 * 1024,       // 64KB
		MaxBufferSize:      4 * 1024 * 1024, // 4MB
		LargeFileThreshold: 1 * 1024 * 1024, // 1MB
		PreallocateSpace:   false,
		UseMemoryMapping:   false,
		MemoryMapThreshold: 0,
		SmallFileThreshold: 64 * 1024, // 64KB
		SmallFileBatchSize: 1,         // One file at a time
		DirectIO:           false,
		ForceFlush:         true,
		SyncReadWrite:      true,
	}
}

// FastCopyProgress is the live state of one copy run, for the progress line
// and the summary.
//
// Every int64 comes first, ahead of the first time.Time: only the start of an
// allocated struct is guaranteed 64-bit aligned on 32-bit platforms, and with
// ActiveFiles behind StartTime a 386 build panicked in atomic.AddInt64 on the
// first small-file batch (the same class of fault CopyProgress had). Every
// counter is read and written through sync/atomic.
type FastCopyProgress struct {
	TotalFiles        int64 // Files found (so far, until ScanDone)
	ProcessedFiles    int64 // Files a worker finished, copied or failed
	TotalSize         int64
	CopiedSize        int64
	ActualCopiedSize  int64 // Bytes actually copied (excluding skipped files)
	SkippedFiles      int64 // Files not copied because of their target or the skip list
	SkippedSize       int64
	ActualFiles       int64 // Files handed to the workers
	ActualSize        int64
	ActiveFiles       int64 // Files being copied right now
	PeakActiveFiles   int64 // The most files ever copied at once (COPY-09)
	BufferPoolHits    int64
	SmallFileBatches  int64
	BatchedFiles      int64
	LastProgressBytes int64 // ActualCopiedSize at the last progress line that saw it move
	CurrentFileBytes  int64 // Guarded by CurrentFileMutex
	CurrentFileSize   int64 // Guarded by CurrentFileMutex
	StartTime         time.Time
	MaxThreads        int // Worker count
	BytesPerSecond    float64
	LastSpeedUpdate   time.Time
	CurrentFile       string           // Guarded by CurrentFileMutex
	ActiveFilesList   []ActiveFileInfo // List of currently processing files with sizes
	ActiveFilesMux    sync.RWMutex     // Mutex for ActiveFilesList and LastDisplayedFile
	LargeFilesList    []ActiveFileInfo // Large files currently processing (priority display)
	LargeFilesMux     sync.RWMutex     // Mutex for LargeFilesList access
	LastDisplayedFile int

	// The progress line's own state - touched only by the one goroutine that
	// prints it.
	LastProgressTime time.Time
	StallReported    bool

	// ScanDone is set when the walk has seen the whole tree.
	ScanDone atomic.Bool

	// CurrentFileMutex is the one lock for CurrentFile, CurrentFileSize and
	// CurrentFileBytes. There used to be two, and a string written under one
	// and read under the other could tear on a 386 build (COPY-17).
	CurrentFileMutex sync.RWMutex
}

// ActiveFileInfo stores information about currently processing file
type ActiveFileInfo struct {
	Path string
	Size int64
}

// setCurrentFileProgress updates the progress of the currently copying file
func (progress *FastCopyProgress) setCurrentFileProgress(filePath string, fileSize int64, copiedBytes int64) {
	progress.CurrentFileMutex.Lock()
	defer progress.CurrentFileMutex.Unlock()

	progress.CurrentFile = filePath
	progress.CurrentFileSize = fileSize
	progress.CurrentFileBytes = copiedBytes
}

// getCurrentFileProgress returns the progress of the currently copying file
func (progress *FastCopyProgress) getCurrentFileProgress() (string, int64, int64) {
	progress.CurrentFileMutex.RLock()
	defer progress.CurrentFileMutex.RUnlock()

	return progress.CurrentFile, progress.CurrentFileSize, progress.CurrentFileBytes
}

// enterActive counts a file a worker starts and keeps the peak.
func (progress *FastCopyProgress) enterActive() {
	n := atomic.AddInt64(&progress.ActiveFiles, 1)
	for {
		peak := atomic.LoadInt64(&progress.PeakActiveFiles)
		if n <= peak || atomic.CompareAndSwapInt64(&progress.PeakActiveFiles, peak, n) {
			return
		}
	}
}

// leaveActive counts a file a worker finished.
func (progress *FastCopyProgress) leaveActive() {
	atomic.AddInt64(&progress.ActiveFiles, -1)
}

// addActiveFile adds a file to the active files list for progress display
func (progress *FastCopyProgress) addActiveFile(filepath string, filesize int64) {
	progress.ActiveFilesMux.Lock()
	defer progress.ActiveFilesMux.Unlock()

	filtered := make([]ActiveFileInfo, 0, len(progress.ActiveFilesList)+1)
	for _, file := range progress.ActiveFilesList {
		if file.Path != filepath {
			filtered = append(filtered, file)
		}
	}
	progress.ActiveFilesList = append(filtered, ActiveFileInfo{Path: filepath, Size: filesize})

	// Keep only last 10 active files to prevent memory growth
	if len(progress.ActiveFilesList) > 10 {
		progress.ActiveFilesList = progress.ActiveFilesList[len(progress.ActiveFilesList)-10:]
	}
}

// addLargeFile adds a large file to priority display list
func (progress *FastCopyProgress) addLargeFile(filepath string, filesize int64) {
	progress.LargeFilesMux.Lock()
	defer progress.LargeFilesMux.Unlock()

	filtered := make([]ActiveFileInfo, 0, len(progress.LargeFilesList)+1)
	for _, file := range progress.LargeFilesList {
		if file.Path != filepath {
			filtered = append(filtered, file)
		}
	}
	progress.LargeFilesList = append(filtered, ActiveFileInfo{Path: filepath, Size: filesize})

	// Keep only last 5 large files for priority display
	if len(progress.LargeFilesList) > 5 {
		progress.LargeFilesList = progress.LargeFilesList[len(progress.LargeFilesList)-5:]
	}
}

// removeActiveFile removes a file from the active files list
func (progress *FastCopyProgress) removeActiveFile(filepath string) {
	progress.ActiveFilesMux.Lock()
	defer progress.ActiveFilesMux.Unlock()

	filtered := make([]ActiveFileInfo, 0, len(progress.ActiveFilesList))
	for _, file := range progress.ActiveFilesList {
		if file.Path != filepath {
			filtered = append(filtered, file)
		}
	}
	progress.ActiveFilesList = filtered
}

// removeLargeFile removes a large file from priority display list
func (progress *FastCopyProgress) removeLargeFile(filepath string) {
	progress.LargeFilesMux.Lock()
	defer progress.LargeFilesMux.Unlock()

	filtered := make([]ActiveFileInfo, 0, len(progress.LargeFilesList))
	for _, file := range progress.LargeFilesList {
		if file.Path != filepath {
			filtered = append(filtered, file)
		}
	}
	progress.LargeFilesList = filtered
}

// getDisplayFile returns the most relevant file to display in progress
func (progress *FastCopyProgress) getDisplayFile() (string, int64) {
	// Priority 1: Show large files currently being processed (they take longer)
	progress.LargeFilesMux.RLock()
	if len(progress.LargeFilesList) > 0 {
		largeFile := progress.LargeFilesList[len(progress.LargeFilesList)-1]
		progress.LargeFilesMux.RUnlock()
		return largeFile.Path, largeFile.Size
	}
	progress.LargeFilesMux.RUnlock()

	// Priority 2: Show regular active files
	progress.ActiveFilesMux.Lock()
	defer progress.ActiveFilesMux.Unlock()

	if len(progress.ActiveFilesList) == 0 {
		// Fall back to the last file a copy reported, under its one lock.
		currentFile, _, _ := progress.getCurrentFileProgress()
		return currentFile, 0
	}

	// Rotate displayed file to avoid showing the same path every tick
	progress.LastDisplayedFile = (progress.LastDisplayedFile + 1) % len(progress.ActiveFilesList)
	activeFile := progress.ActiveFilesList[progress.LastDisplayedFile]
	return activeFile.Path, activeFile.Size
}

// truncateFilePath truncates file path for display to prevent line wrapping
func truncateFilePath(path string, maxLength int) string {
	if len(path) <= maxLength {
		return path
	}

	// Try to show beginning and end of path
	if maxLength > 20 {
		prefixLen := maxLength/2 - 3
		suffixLen := maxLength - prefixLen - 3
		return path[:prefixLen] + "..." + path[len(path)-suffixLen:]
	}

	return path[:maxLength-3] + "..."
}

// formatFileSize formats file size in human readable format
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Drive analysis Windows API (drive_analysis.go)
var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	procGetDriveType         = kernel32.NewProc("GetDriveTypeW")
	procGetVolumeInformation = kernel32.NewProc("GetVolumeInformationW")
	procGetDiskFreeSpace     = kernel32.NewProc("GetDiskFreeSpaceW")
)

// FileJob represents a file copy job
type FileJob struct {
	SourcePath string
	TargetPath string
	Info       os.FileInfo
	// Replace: the target is an empty leftover and may be replaced.
	Replace bool
}

// SmallFileBatch represents a batch of small files to be processed together
type SmallFileBatch struct {
	Jobs []FileJob
}

// copyEngine is one run of the parallel copy (fastcopy, synccopy, balanced,
// maxcopy, the optimal copy and safecopy).
//
// The walk and the copy run together: the walk creates each folder and hands
// each file to a fixed pool of MaxConcurrentFiles workers - small files in
// batches, larger ones alone - so memory does not grow with the tree (COPY-16)
// and no more files are open than there are workers (COPY-09). Every file
// goes through skipDecision and copyOneFile, so nothing half-written is left
// under a final name (COPY-02) and nothing existing is overwritten (COPY-01).
type copyEngine struct {
	verb       string
	config     FastCopyConfig
	progress   *FastCopyProgress
	handler    *InterruptHandler
	ctx        context.Context
	cancel     context.CancelFunc
	stats      *copyRunStats
	damaged    *DamagedDiskHandler // safe mode: honours and extends the skip list
	skipList   *fileStateList      // the skip list, loaded once for the run
	ownList    bool
	collisions caseCollisionIndex
	noProgress time.Duration
	bufLimit   int

	displayStop chan struct{}
	displayDone chan struct{}
}

func newCopyEngine(verb string, config FastCopyConfig, damaged *DamagedDiskHandler, handler *InterruptHandler) *copyEngine {
	if handler == nil {
		handler = globalInterruptHandler
	}
	if handler == nil {
		handler = NewInterruptHandler()
	}
	if config.MaxConcurrentFiles < 1 {
		config.MaxConcurrentFiles = 1
	}
	ctx, cancel := context.WithCancel(handler.Context())
	e := &copyEngine{
		verb:       verb,
		config:     config,
		progress:   &FastCopyProgress{StartTime: time.Now(), LastSpeedUpdate: time.Now()},
		handler:    handler,
		ctx:        ctx,
		cancel:     cancel,
		stats:      newCopyRunStats(verb),
		damaged:    damaged,
		noProgress: NewDamagedDiskConfig().FileTimeout,
		bufLimit:   copyBufferLimit(config.MaxConcurrentFiles, config.MaxBufferSize),
	}
	e.progress.MaxThreads = config.MaxConcurrentFiles
	if damaged != nil {
		e.skipList = damaged.list
	} else {
		// Read once for the whole run; this engine never adds to it.
		list, err := openStateList(copySkipListName, true)
		if err != nil {
			fmt.Printf("Warning: cannot read the skip list: %v\n", err)
		}
		e.skipList = list
		e.ownList = true
	}
	return e
}

func (e *copyEngine) close() {
	e.cancel()
	if e.ownList {
		e.skipList.Close()
	}
}

// startDisplay prints the progress line once a second until stopDisplay.
func (e *copyEngine) startDisplay() {
	e.displayStop = make(chan struct{})
	e.displayDone = make(chan struct{})
	go func() {
		defer close(e.displayDone)
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				if e.damaged != nil {
					showFastProgressWithDamage(e.progress)
				} else {
					showFastProgress(e.progress)
				}
			case <-e.displayStop:
				return
			}
		}
	}()
}

func (e *copyEngine) stopDisplay() {
	if e.displayStop == nil {
		return
	}
	close(e.displayStop)
	<-e.displayDone
	e.displayStop = nil
}

// admit decides what happens to one source file: skipped (and counted) or a
// job for the workers.
func (e *copyEngine) admit(path string, info os.FileInfo, target string) (FileJob, bool) {
	if !info.Mode().IsRegular() {
		if info.Mode()&os.ModeSymlink == 0 {
			e.stats.recordSpecial(path)
			return FileJob{}, false
		}
		// A file link is copied as what it points at, as it always was.
		ti, err := os.Stat(path)
		if err != nil {
			e.stats.recordFailure(path, fmt.Errorf("cannot follow the link: %w", err))
			return FileJob{}, false
		}
		if !ti.Mode().IsRegular() {
			e.stats.recordSpecial(path)
			return FileJob{}, false
		}
		info = ti
	}
	if isPartialName(info.Name()) {
		e.stats.recordPartialSource(path)
		return FileJob{}, false
	}
	p := e.progress
	atomic.AddInt64(&p.TotalFiles, 1)
	atomic.AddInt64(&p.TotalSize, info.Size())

	skipListed := false
	if e.damaged != nil {
		skipListed = e.damaged.ShouldSkip(path, info)
	} else {
		skipListed = e.skipList.HasInfo(path, info)
	}
	if skipListed {
		e.stats.recordDamagedSkip(path, e.skipList.Path())
		atomic.AddInt64(&p.SkippedFiles, 1)
		atomic.AddInt64(&p.SkippedSize, info.Size())
		return FileJob{}, false
	}

	v, err := skipDecision(info, target)
	if !e.stats.admit(v, path, target, err) {
		atomic.AddInt64(&p.SkippedFiles, 1)
		atomic.AddInt64(&p.SkippedSize, info.Size())
		return FileJob{}, false
	}
	atomic.AddInt64(&p.ActualFiles, 1)
	atomic.AddInt64(&p.ActualSize, info.Size())
	return FileJob{SourcePath: path, TargetPath: target, Info: info, Replace: v == copyReplaceEmpty}, true
}

// bufferFor is the buffer size for a file of this size.
func (e *copyEngine) bufferFor(size int64) int {
	n := e.bufLimit
	if size < int64(n) {
		n = int(size)
	}
	return n
}

// copyJob copies one file on a worker and counts the outcome.
func (e *copyEngine) copyJob(job FileJob) {
	if e.ctx.Err() != nil {
		return
	}
	p := e.progress
	p.enterActive()
	defer p.leaveActive()

	size := job.Info.Size()
	p.addActiveFile(job.SourcePath, size)
	defer p.removeActiveFile(job.SourcePath)
	if size >= e.config.LargeFileThreshold {
		p.addLargeFile(job.SourcePath, size)
		defer p.removeLargeFile(job.SourcePath)
	}
	p.setCurrentFileProgress(job.SourcePath, size, 0)

	// fileBytes is touched only by the copy loop that calls onBytes.
	var fileBytes int64
	onBytes := func(n int64) {
		atomic.AddInt64(&p.CopiedSize, n)
		atomic.AddInt64(&p.ActualCopiedSize, n)
		fileBytes += n
		p.setCurrentFileProgress(job.SourcePath, size, fileBytes)
	}

	var err error
	func() {
		// A panic in one file is that file's failure, not the end of the
		// process (COPY-08).
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("the copy panicked: %v", r)
			}
		}()
		if e.damaged != nil {
			err = e.damaged.CopyFileWithDamageHandling(e.ctx, job.SourcePath, job.TargetPath, job.Info, job.Replace, onBytes, e.handler)
			return
		}
		atomic.AddInt64(&p.BufferPoolHits, 1)
		buf, put := takeCopyBuffer(e.bufferFor(size))
		err = copyOneFile(e.ctx, job.SourcePath, job.Info, job.TargetPath, fileCopyOptions{
			Buffer:     buf,
			NoProgress: e.noProgress,
			Replace:    job.Replace,
			OnBytes:    onBytes,
			Handler:    e.handler,
			Release:    put,
		})
	}()

	switch {
	case err == nil:
		e.stats.recordCopied(size)
		atomic.AddInt64(&p.ProcessedFiles, 1)
	case isStopError(err):
		// A stop is not this file's failure.
	case errors.Is(err, errTooManyStalledReads):
		e.stats.recordFailure(job.SourcePath, err)
		atomic.AddInt64(&p.ProcessedFiles, 1)
		e.stats.note(fmt.Sprintf("STOPPED EARLY: %v", err))
		e.cancel()
	default:
		e.stats.recordFailure(job.SourcePath, err)
		atomic.AddInt64(&p.ProcessedFiles, 1)
	}
}

// copyTree copies a folder tree: one walk, a fixed pool of workers.
func (e *copyEngine) copyTree(sourceRoot, targetRoot string) error {
	workers := e.config.MaxConcurrentFiles
	batchSize := e.config.SmallFileBatchSize
	if batchSize < 1 {
		batchSize = 1
	}
	fmt.Printf("Copying while the tree is walked: %d worker(s), buffers up to %s.\n", workers, formatFileSize(int64(e.bufLimit)))

	work := make(chan []FileJob, workers*2)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for batch := range work {
				if len(batch) > 1 {
					atomic.AddInt64(&e.progress.SmallFileBatches, 1)
					atomic.AddInt64(&e.progress.BatchedFiles, int64(len(batch)))
				}
				for _, job := range batch {
					if e.ctx.Err() != nil {
						break
					}
					e.copyJob(job)
				}
			}
		}()
	}

	e.startDisplay()
	send := func(items []FileJob) bool {
		select {
		case work <- items:
			return true
		case <-e.ctx.Done():
			return false
		}
	}
	var batch []FileJob
	flush := func() bool {
		if len(batch) == 0 {
			return true
		}
		b := batch
		batch = nil
		return send(b)
	}

	walkErr := filepath.Walk(sourceRoot, func(path string, info os.FileInfo, err error) error {
		// The walk itself obeys the stop (COPY-04).
		if e.ctx.Err() != nil {
			return filepath.SkipAll
		}
		if err != nil {
			e.stats.recordFailure(path, fmt.Errorf("cannot read: %w", err))
			return nil
		}
		if winner, lost := e.collisions.winner(path); lost {
			e.stats.recordCollision(path, winner)
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(sourceRoot, path)
		if rerr != nil {
			e.stats.recordFailure(path, rerr)
			return nil
		}
		target := filepath.Join(targetRoot, rel)
		if info.IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				e.stats.recordFailure(target, fmt.Errorf("cannot create the folder: %w", err))
				return filepath.SkipDir
			}
			e.collisions.scanDir(path)
			return nil
		}
		job, ok := e.admit(path, info, target)
		if !ok {
			return nil
		}
		if info.Size() < e.config.SmallFileThreshold && batchSize > 1 {
			batch = append(batch, job)
			if len(batch) >= batchSize && !flush() {
				return filepath.SkipAll
			}
			return nil
		}
		if !flush() || !send([]FileJob{job}) {
			return filepath.SkipAll
		}
		return nil
	})
	flush()
	close(work)
	wg.Wait()
	e.progress.ScanDone.Store(true)
	e.stopDisplay()

	if walkErr != nil && !isStopError(walkErr) {
		e.stats.recordFailure(sourceRoot, walkErr)
	}
	e.printTotals()
	return e.stats.finish()
}

// copySingle copies one file. A target that is a folder (existing, or spelled
// with a trailing separator) receives the file under its own name.
func (e *copyEngine) copySingle(sourcePath string, info os.FileInfo, targetPath string) error {
	targetPath, err := singleFileTarget(e.verb, sourcePath, targetPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("cannot create the target folder: %w", err)
	}
	job, ok := e.admit(sourcePath, info, targetPath)
	if ok {
		e.startDisplay()
		e.copyJob(job)
		e.progress.ScanDone.Store(true)
		e.stopDisplay()
	}
	e.printTotals()
	return e.stats.finish()
}

// printTotals is the timing part of the summary.
func (e *copyEngine) printTotals() {
	p := e.progress
	d := time.Since(p.StartTime)
	speed := 0.0
	if d.Seconds() > 0 {
		speed = float64(atomic.LoadInt64(&p.ActualCopiedSize)) / d.Seconds() / (1024 * 1024)
	}
	fmt.Printf("\n%s finished in %v: %d files found, %.2f MB/s", e.verb, d.Round(time.Millisecond),
		atomic.LoadInt64(&p.TotalFiles), speed)
	if b := atomic.LoadInt64(&p.SmallFileBatches); b > 0 {
		fmt.Printf(", %d small-file batches", b)
	}
	fmt.Printf(", at most %d files at once\n", atomic.LoadInt64(&p.PeakActiveFiles))
}

// runCopyEngine runs one engine over sourcePath and returns its counts and
// its answer.
func runCopyEngine(verb, sourcePath, targetPath string, config FastCopyConfig, damaged *DamagedDiskHandler, handler *InterruptHandler) (*copyRunStats, error) {
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("source path error: %v", err)
	}
	e := newCopyEngine(verb, config, damaged, handler)
	defer e.close()
	if info.IsDir() {
		err = e.copyTree(sourcePath, targetPath)
	} else {
		err = e.copySingle(sourcePath, info, targetPath)
	}
	runtime.GC()
	debug.FreeOSMemory()
	return e.stats, err
}

// copyWithSafeFallback runs an engine and, when files stalled or hit device
// errors, retries in the damaged-disk mode - which copies only what is not at
// the target yet, because everything that is passes skipDecision.
func copyWithSafeFallback(verb, sourcePath, targetPath string, config FastCopyConfig) error {
	stats, err := runCopyEngine(verb, sourcePath, targetPath, config, nil, nil)
	if stats != nil && stats.wantsSafeRetry() {
		fmt.Printf("\n%d file(s) stalled or hit device errors - retrying what is missing in SAFE RESCUE mode..\n", stats.retryable.Load())
		return SafeCopy(sourcePath, targetPath)
	}
	return err
}

// showFastProgress displays progress with the current file.
//
// A stall is judged by bytes - ActualCopiedSize not moving - and it only
// prints (COPY-05). It used to judge by the file count, so copying several
// large files that each take longer than ten seconds recorded a healthy file
// as "stuck" in the permanent skip list. Giving up on a file is the per-file
// watchdog's decision, and recording one is the damaged-disk mode's.
func showFastProgress(progress *FastCopyProgress) {
	processedFiles := atomic.LoadInt64(&progress.ProcessedFiles)
	activeFiles := atomic.LoadInt64(&progress.ActiveFiles)
	displayFilePath, displayFileSize := progress.getDisplayFile()

	now := time.Now()
	copied := atomic.LoadInt64(&progress.ActualCopiedSize)
	if progress.LastProgressTime.IsZero() || copied != progress.LastProgressBytes {
		progress.LastProgressTime = now
		progress.LastProgressBytes = copied
		progress.StallReported = false
	} else if activeFiles > 0 && !progress.StallReported && now.Sub(progress.LastProgressTime) > 10*time.Second {
		progress.StallReported = true
		fmt.Printf("\nNo data moved for 10+ seconds (now at %s) - waiting; a file that moves nothing for the watchdog period is given up and reported.\n",
			truncateFilePath(displayFilePath, 90))
	}

	elapsed := time.Since(progress.StartTime)
	if now.Sub(progress.LastSpeedUpdate) > time.Second {
		if copied > 0 {
			elapsedForSpeed := math.Max(elapsed.Seconds(), 5.0)
			progress.BytesPerSecond = math.Min(float64(copied)/elapsedForSpeed/(1024*1024), 2000.0)
		}
		progress.LastSpeedUpdate = now
	}

	scanDone := progress.ScanDone.Load()
	sizeToCopy := atomic.LoadInt64(&progress.ActualSize)
	filesToCopy := atomic.LoadInt64(&progress.ActualFiles)

	eta := "unknown"
	if scanDone && copied > 0 && sizeToCopy > copied && progress.BytesPerSecond > 0 {
		etaSeconds := float64(sizeToCopy-copied) / (progress.BytesPerSecond * 0.8 * 1024 * 1024)
		eta = formatETA(time.Duration(math.Max(etaSeconds, 1) * float64(time.Second)))
	}
	filePercent, sizePercent := 0.0, 0.0
	if filesToCopy > 0 {
		filePercent = float64(processedFiles) / float64(filesToCopy) * 100
	}
	if sizeToCopy > 0 {
		sizePercent = float64(copied) / float64(sizeToCopy) * 100
	}
	found := ""
	if !scanDone {
		found = " found so far"
	}
	fileSizeStr := ""
	if displayFileSize > 0 {
		fileSizeStr = fmt.Sprintf(" [%s]", formatFileSize(displayFileSize))
	}
	fmt.Printf("\r%s\r%d/%d%s %s%s (%.1f%%) | %.2f/%.2f GB (%.1f%%) | %.2f MB/s | %d | ETA: %s",
		strings.Repeat(" ", 160),
		processedFiles, filesToCopy, found, truncateFilePath(displayFilePath, 70), fileSizeStr, filePercent,
		float64(copied)/(1024*1024*1024), float64(sizeToCopy)/(1024*1024*1024), sizePercent,
		progress.BytesPerSecond, progress.MaxThreads, eta)
}

// FastCopy performs optimized copying; files that stall or hit device errors
// are retried in the damaged-disk mode.
func FastCopy(sourcePath, targetPath string) error {
	if err := refuseCopyPaths("fastcopy", sourcePath, targetPath); err != nil {
		return err
	}
	return copyWithSafeFallback("fastcopy", sourcePath, targetPath, NewFastCopyConfig())
}

// FastCopySync performs synchronized copying: one worker, one file at a time.
func FastCopySync(sourcePath, targetPath string) error {
	if err := refuseCopyPaths("synccopy", sourcePath, targetPath); err != nil {
		return err
	}
	fmt.Printf("Starting synchronized copy mode (one file at a time)..\n")
	_, err := runCopyEngine("synccopy", sourcePath, targetPath, NewSyncCopyConfig(), nil, nil)
	return err
}

// FastCopyMax performs maximum performance copying.
func FastCopyMax(sourcePath, targetPath string) error {
	if err := refuseCopyPaths("maxcopy", sourcePath, targetPath); err != nil {
		return err
	}
	config := NewMaxPerformanceConfig()
	fmt.Printf("Starting MAXIMUM PERFORMANCE copy mode (%d workers)..\n", config.MaxConcurrentFiles)
	return copyWithSafeFallback("maxcopy", sourcePath, targetPath, config)
}

// FastCopyBalanced performs balanced copying optimized for HDD-to-HDD operations.
func FastCopyBalanced(sourcePath, targetPath string) error {
	if err := refuseCopyPaths("balanced", sourcePath, targetPath); err != nil {
		return err
	}
	config := NewBalancedCopyConfig()
	fmt.Printf("Starting BALANCED copy mode (%d workers)..\n", config.MaxConcurrentFiles)
	return copyWithSafeFallback("balanced", sourcePath, targetPath, config)
}

// SafeCopy performs ultra-safe copying for problematic/damaged drives: one
// file at a time, small buffers, a no-progress watchdog per file, and a skip
// list that records sources the copy proved unreadable.
func SafeCopy(sourcePath, targetPath string) error {
	if err := refuseCopyPaths("safecopy", sourcePath, targetPath); err != nil {
		return err
	}
	damagedHandler, err := NewDamagedDiskHandler()
	if err != nil {
		fmt.Printf("Warning: %v - damaged files are skipped for this run only.\n", err)
	}
	defer func() {
		damagedHandler.PrintSummary()
		damagedHandler.Close()
	}()

	fmt.Printf("Starting SAFE RESCUE mode (1 worker, small buffers)..\n")
	fmt.Printf("Damaged disk protection: a file with no progress for %v is given up, recorded and skipped next time.\n",
		damagedHandler.config.FileTimeout)
	_, err = runCopyEngine("safecopy", sourcePath, targetPath, NewSafeConfig(), damagedHandler, nil)
	return err
}

// showFastProgressWithDamage displays progress in the damaged-disk mode.
func showFastProgressWithDamage(progress *FastCopyProgress) {
	processedFiles := atomic.LoadInt64(&progress.ProcessedFiles)
	displayFilePath, displayFileSize := progress.getDisplayFile()
	elapsed := time.Since(progress.StartTime)

	copied := atomic.LoadInt64(&progress.ActualCopiedSize)
	if elapsed.Seconds() > 5.0 && copied > 0 {
		progress.BytesPerSecond = float64(copied) / elapsed.Seconds() / (1024 * 1024)
	}
	scanDone := progress.ScanDone.Load()
	sizeToCopy := atomic.LoadInt64(&progress.ActualSize)
	filesToCopy := atomic.LoadInt64(&progress.ActualFiles)

	eta := "unknown"
	if scanDone && copied > 0 && sizeToCopy > copied && progress.BytesPerSecond > 0 {
		eta = formatETA(time.Duration(float64(sizeToCopy-copied) / (progress.BytesPerSecond * 1024 * 1024) * float64(time.Second)))
	}
	filePercent, sizePercent := 0.0, 0.0
	if filesToCopy > 0 {
		filePercent = float64(processedFiles) / float64(filesToCopy) * 100
	}
	if sizeToCopy > 0 {
		sizePercent = float64(copied) / float64(sizeToCopy) * 100
	}
	found := ""
	if !scanDone {
		found = " found so far"
	}
	fileSizeStr := ""
	if displayFileSize > 0 {
		fileSizeStr = fmt.Sprintf(" [%s]", formatFileSize(displayFileSize))
	}
	fmt.Printf("\r%s\r%d/%d%s %s%s (%.1f%%) | %.2f/%.2f GB (%.1f%%) | %.2f MB/s | ETA: %s",
		strings.Repeat(" ", 160),
		processedFiles, filesToCopy, found, truncateFilePath(displayFilePath, 60), fileSizeStr, filePercent,
		float64(copied)/(1024*1024*1024), float64(sizeToCopy)/(1024*1024*1024), sizePercent,
		progress.BytesPerSecond, eta)

	currentFile, currentFileSize, currentFileBytes := progress.getCurrentFileProgress()
	if currentFile != "" && currentFileSize > 100*1024*1024 {
		fmt.Printf(" [Current: %.1f%%]", float64(currentFileBytes)/float64(currentFileSize)*100)
	}
}
