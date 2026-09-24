package main

import (
	"context"
	"fmt"
	"io"
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
	Errors           []string     // Protected by ErrorsMutex
	ErrorsMutex      sync.Mutex   // Mutex for thread-safe error logging
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

// FileOperationTimeout defines timeout for file operations with broken sources
const FileOperationTimeout = 10 * time.Second // Increased to 10 seconds for damaged disks

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

// logError safely logs an error to the progress structure
func (p *CopyProgress) logError(path string, err error) {
	p.ErrorsMutex.Lock()
	defer p.ErrorsMutex.Unlock()

	errorMsg := fmt.Sprintf("%s: %v", path, err)
	p.Errors = append(p.Errors, errorMsg)
	atomic.AddInt64(&p.SkippedFiles, 1)

	// Also log to stderr for immediate visibility
	fmt.Fprintf(os.Stderr, "Warning: %s\n", errorMsg)
}

// printErrorSummary prints a summary of all errors encountered
func (p *CopyProgress) printErrorSummary() {
	p.ErrorsMutex.Lock()
	defer p.ErrorsMutex.Unlock()

	if len(p.Errors) > 0 {
		fmt.Printf("\n⚠️  COPY OPERATION COMPLETED WITH ERRORS:\n")
		fmt.Printf("   %d files were skipped due to access errors:\n\n", len(p.Errors))

		// Show first 10 errors, then summarize if more
		maxShow := 10
		for i, errMsg := range p.Errors {
			if i < maxShow {
				fmt.Printf("   • %s\n", errMsg)
			} else {
				fmt.Printf("   ... and %d more errors\n", len(p.Errors)-maxShow)
				break
			}
		}

		fmt.Printf("\nRecommendations:\n")
		fmt.Printf("• Check file permissions for skipped files\n")
		fmt.Printf("• Run as administrator if accessing system files\n")
		fmt.Printf("• Some files may be in use by other applications\n")
		fmt.Printf("• Consider using 'fastcopy' for better error handling\n\n")
	}
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
		return nil, fmt.Errorf("stat operation timed out after %v", timeout)
	}
}

// copyWithTimeout performs file copy with timeout and built-in progress support
func copyWithTimeout(dst io.Writer, src io.Reader, timeout time.Duration) (int64, error) {
	type copyResult struct {
		written int64
		err     error
	}

	ch := make(chan copyResult, 1)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	go func() {
		defer close(ch)
		var totalWritten int64
		buffer := make([]byte, 1024*1024) // 1MB buffer

		for {
			select {
			case <-ctx.Done():
				ch <- copyResult{totalWritten, ctx.Err()}
				return
			default:
			}

			n, readErr := src.Read(buffer)
			if readErr != nil && readErr != io.EOF {
				ch <- copyResult{totalWritten, readErr}
				return
			}
			if n == 0 {
				break
			}

			written, writeErr := dst.Write(buffer[:n])
			if writeErr != nil {
				ch <- copyResult{totalWritten, writeErr}
				return
			}

			totalWritten += int64(written)

			if readErr == io.EOF {
				break
			}
		}

		ch <- copyResult{totalWritten, nil}
	}()

	select {
	case result := <-ch:
		return result.written, result.err
	case <-ctx.Done():
		return 0, fmt.Errorf("copy operation timed out after %v", timeout)
	}
}

// handleCopyCommand processes the copy command with damaged disk handling
// handleCopyCommand - regular copy with damaged disk protection (for safety commands)
func handleCopyCommand(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("copy command requires source and target paths")
	}

	sourcePath := args[1]
	targetPath := args[2]

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path does not exist: %s", sourcePath)
	}

	fmt.Printf("🔄 Starting copy with damaged disk protection from %s to %s\n", sourcePath, targetPath)

	// Initialize damaged disk handler
	damagedHandler, err := NewDamagedDiskHandler()
	if err != nil {
		fmt.Printf("Warning: Could not initialize damaged disk handler: %v\n", err)
		// Continue without damage handling
		if sourceInfo.IsDir() {
			return copyDirectory(sourcePath, targetPath)
		} else {
			return copyFile(sourcePath, targetPath)
		}
	}
	defer func() {
		damagedHandler.PrintSummary()
		damagedHandler.Close()
	}()

	if sourceInfo.IsDir() {
		return copyDirectoryWithDamageHandling(sourcePath, targetPath, damagedHandler)
	} else {
		return copyFileWithDamageHandling(sourcePath, targetPath, sourceInfo, damagedHandler)
	}
}

// handleCopyCommandNoDamage - regular copy without damaged disk protection (for normal operation)
func handleCopyCommandNoDamage(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("copy command requires source and target paths")
	}

	sourcePath := args[1]
	targetPath := args[2]

	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path does not exist: %s", sourcePath)
	}

	fmt.Printf("🔄 Starting regular copy from %s to %s\n", sourcePath, targetPath)

	if sourceInfo.IsDir() {
		return copyDirectory(sourcePath, targetPath)
	} else {
		return copyFile(sourcePath, targetPath)
	}
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

// copyTree copies sourcePath into targetPath in one walk of the tree: each
// directory is created and each file handed to copyOne as the walk reaches
// it, so the first file moves as soon as it is found instead of after a full
// counting pass (SP-0002 item 5). The totals therefore grow while the copy
// runs, and the progress line says so rather than showing an ETA against half
// a tree. --precount buys the exact totals back with one counting walk first.
func copyTree(sourcePath, targetPath string, copyOne func(path, target string, info os.FileInfo, progress *CopyProgress) error) error {
	progress := &CopyProgress{
		StartTime: time.Now(),
	}

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

	copyErr := copyWalk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			progress.logError(path, fmt.Errorf("error accessing during copy: %v", err))
			return nil // Continue with other files
		}

		// For broken sources, handle cases where info might be corrupted
		if info == nil {
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				progress.logError(path, fmt.Errorf("stat timeout during copy: %v", statErr))
				return nil
			}
			info = timeoutInfo
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			progress.logError(path, fmt.Errorf("error calculating relative path: %v", err))
			return nil
		}

		targetFilePath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			if err := os.MkdirAll(targetFilePath, info.Mode()); err != nil {
				progress.logError(targetFilePath, fmt.Errorf("error creating directory: %v", err))
			}
			return nil
		}

		if !progress.TotalsKnown {
			progress.AddTotalFiles(1)
			progress.AddTotalSize(info.Size())
		}
		progress.SetCurrentFile(path)
		return copyOne(path, targetFilePath, info, progress)
	})

	fmt.Printf("\nCopy finished: %d files, %.2f MB in %v\n",
		progress.GetProcessedFiles(), float64(progress.GetCopiedSize())/(1024*1024),
		time.Since(progress.StartTime).Round(time.Millisecond))
	progress.printErrorSummary()

	return copyErr
}

// countTree is the counting walk --precount asks for. It records nothing but
// the totals: a path it cannot read is reported once, by the copying walk
// that follows, not twice.
func countTree(sourcePath string, progress *CopyProgress) error {
	return copyWalk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
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
	return copyTree(sourcePath, targetPath, copyFileWithCopyProgress)
}

// copyFile copies a single file
func copyFile(sourcePath, targetPath string) error {
	sourceInfo, err := statWithTimeout(sourcePath, FileOperationTimeout)
	if err != nil {
		return fmt.Errorf("cannot stat source file: %v", err)
	}

	progress := &CopyProgress{
		StartTime:   time.Now(),
		TotalsKnown: true, // one file: the totals are its own size
	}

	// Initialize atomic fields
	progress.AddTotalFiles(1)
	progress.AddTotalSize(sourceInfo.Size())
	progress.SetCurrentFile(sourcePath)

	// Create target directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("error creating target directory: %v", err)
	}

	return copyFileWithCopyProgress(sourcePath, targetPath, sourceInfo, progress)
}

// copyFileWithProgress copies a single file and updates progress
func copyFileWithCopyProgress(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *CopyProgress) error {
	// Check if target file already exists (with timeout for broken filesystems)
	if _, err := statWithTimeout(targetPath, FileOperationTimeout); err == nil {
		// Update counters and show skip message
		currentFile := atomic.AddInt64(&progress.ProcessedFiles, 1)
		currentSize := atomic.AddInt64(&progress.CopiedSize, sourceInfo.Size())

		fmt.Printf("Skipped: %s [%s, ETA: %s] - already exists\n",
			sourcePath, progress.countsText(currentFile, currentSize), progress.etaText(currentSize))
		return nil
	}

	// Open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		// Update counters even on error for progress consistency
		atomic.AddInt64(&progress.ProcessedFiles, 1)

		progress.logError(sourcePath, fmt.Errorf("cannot open source file: %v", err))
		return nil // Continue with other files
	}
	defer sourceFile.Close()

	// Create target file
	targetFile, err := os.Create(targetPath)
	if err != nil {
		// Update counters even on error for progress consistency
		atomic.AddInt64(&progress.ProcessedFiles, 1)

		progress.logError(targetPath, fmt.Errorf("cannot create target file: %v", err))
		return nil // Continue with other files
	}
	defer targetFile.Close()

	// Copy file content with timeout and progress reporting
	copiedBytes, err := copyWithTimeout(targetFile, sourceFile, FileOperationTimeout)
	if err != nil {
		// Update counters even on timeout/error
		atomic.AddInt64(&progress.ProcessedFiles, 1)

		progress.logError(sourcePath, fmt.Errorf("copy timeout/error: %v", err))
		return nil // Continue with other files
	}

	atomic.AddInt64(&progress.CopiedSize, copiedBytes)
	atomic.AddInt64(&progress.ProcessedFiles, 1)

	// Show progress after successful copy
	showProgress(progress)

	// Set file permissions and timestamps
	err = os.Chmod(targetPath, sourceInfo.Mode())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not set permissions for %s: %v\n", targetPath, err)
	}

	err = os.Chtimes(targetPath, sourceInfo.ModTime(), sourceInfo.ModTime())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Could not set timestamps for %s: %v\n", targetPath, err)
	}

	return nil
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

// copyDirectoryWithDamageHandling copies entire directory structure with damage handling
func copyDirectoryWithDamageHandling(sourcePath, targetPath string, handler *DamagedDiskHandler) error {
	return copyTree(sourcePath, targetPath, func(path, target string, info os.FileInfo, progress *CopyProgress) error {
		return copyFileWithDamageHandlingAndProgress(path, target, info, progress, handler)
	})
}

// copyFileWithDamageHandling copies a single file with damage handling
func copyFileWithDamageHandling(sourcePath, targetPath string, sourceInfo os.FileInfo, handler *DamagedDiskHandler) error {
	progress := &CopyProgress{
		StartTime:   time.Now(),
		TotalsKnown: true, // one file: the totals are its own size
	}

	// Initialize atomic fields
	progress.AddTotalFiles(1)
	progress.AddTotalSize(sourceInfo.Size())
	progress.SetCurrentFile(sourcePath)

	// Create target directory if it doesn't exist
	targetDir := filepath.Dir(targetPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("error creating target directory: %v", err)
	}

	return copyFileWithDamageHandlingAndProgress(sourcePath, targetPath, sourceInfo, progress, handler)
}

// copyFileWithDamageHandlingAndProgress copies a single file with damage handling and updates progress
func copyFileWithDamageHandlingAndProgress(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *CopyProgress, handler *DamagedDiskHandler) error {
	// Check if target file already exists (with timeout for broken filesystems)
	if _, err := statWithTimeout(targetPath, FileOperationTimeout); err == nil {
		// Update counters and show skip message
		currentFile := atomic.AddInt64(&progress.ProcessedFiles, 1)
		currentSize := atomic.AddInt64(&progress.CopiedSize, sourceInfo.Size())

		fmt.Printf("⏭️ Skipped: %s [%s, ETA: %s] - already exists\n",
			sourcePath, progress.countsText(currentFile, currentSize), progress.etaText(currentSize))
		return nil
	}

	// Use damage handler to copy the file
	err := handler.CopyFileWithDamageHandling(sourcePath, targetPath, sourceInfo, nil)
	if err != nil {
		// This is a critical error, not a damage issue
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		progress.logError(sourcePath, fmt.Errorf("copy error: %v", err))
		return nil // Continue with other files
	}

	// Check if file was actually copied (not skipped due to damage)
	if _, statErr := os.Stat(targetPath); statErr == nil {
		// File was successfully copied
		atomic.AddInt64(&progress.CopiedSize, sourceInfo.Size())
		atomic.AddInt64(&progress.ProcessedFiles, 1)

		// Show progress after successful copy
		showProgress(progress)
	} else {
		// File was skipped due to damage
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		atomic.AddInt64(&progress.DamagedFiles, 1)
	}

	return nil
}
