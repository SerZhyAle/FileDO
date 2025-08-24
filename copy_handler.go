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

// CopyProgress tracks the progress of copy operations
type CopyProgress struct {
	TotalFiles      int64        // Use atomic operations
	ProcessedFiles  int64        // Use atomic operations  
	TotalSize       int64        // Use atomic operations
	CopiedSize      int64        // Use atomic operations
	StartTime       time.Time    // Read-only after initialization
	CurrentFile     string       // Protected by CurrentFileMutex
	CurrentFileMutex sync.RWMutex // Mutex for CurrentFile access
	SkippedFiles    int64        // Use atomic operations
	DamagedFiles    int64        // Use atomic operations - files that couldn't be read
	Errors          []string     // Protected by ErrorsMutex
	ErrorsMutex     sync.Mutex   // Mutex for thread-safe error logging
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

// copyDirectory copies entire directory structure
func copyDirectory(sourcePath, targetPath string) error {
	// First, scan directory to calculate total files and size
	progress := &CopyProgress{
		StartTime: time.Now(),
	}

	fmt.Println("Scanning directory structure...")
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			progress.logError(path, fmt.Errorf("error accessing during scan: %v", err))
			return nil // Skip files with errors
		}
		
		// For broken sources, use timeout for stat operations during scanning
		if info == nil {
			// Re-stat with timeout if info is nil
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				progress.logError(path, fmt.Errorf("stat timeout during scan: %v", statErr))
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

	if err != nil {
		return fmt.Errorf("error scanning directory: %v", err)
	}

	fmt.Printf("Found %d files, total size: %.2f MB\n", 
		progress.GetTotalFiles(), float64(progress.GetTotalSize())/(1024*1024))

	// Now perform the actual copy
	copyErr := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			progress.logError(path, fmt.Errorf("error accessing during copy: %v", err))
			return nil // Continue with other files
		}

		// For broken sources, handle cases where info might be corrupted
		if info == nil {
			// Re-stat with timeout if info is nil
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				progress.logError(path, fmt.Errorf("stat timeout during copy: %v", statErr))
				return nil
			}
			info = timeoutInfo
		}

		// Calculate relative path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			progress.logError(path, fmt.Errorf("error calculating relative path: %v", err))
			return nil
		}

		targetFilePath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			// Create directory
			err := os.MkdirAll(targetFilePath, info.Mode())
			if err != nil {
				progress.logError(targetFilePath, fmt.Errorf("error creating directory: %v", err))
			}
			return nil
		}

		// Copy file with progress and timeout handling
		progress.SetCurrentFile(path)
		return copyFileWithCopyProgress(path, targetFilePath, info, progress)
	})
	
	// Print error summary at the end
	progress.printErrorSummary()
	
	return copyErr
}

// copyFile copies a single file
func copyFile(sourcePath, targetPath string) error {
	sourceInfo, err := statWithTimeout(sourcePath, FileOperationTimeout)
	if err != nil {
		return fmt.Errorf("cannot stat source file: %v", err)
	}

	progress := &CopyProgress{
		StartTime:  time.Now(),
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
		
		// Calculate ETA
		elapsed := time.Since(progress.StartTime)
		eta := "unknown"
		if currentSize > 0 {
			remainingSize := progress.TotalSize - currentSize
			if remainingSize > 0 {
				speed := float64(currentSize) / elapsed.Seconds() // bytes per second
				etaSeconds := int64(float64(remainingSize) / speed)
				eta = formatETA(time.Duration(etaSeconds) * time.Second)
			} else {
				eta = "0s"
			}
		}

		fmt.Printf("Skipped: (%d/%d) %s (%.2f / %.2f MB) ETA: %s - already exists\n", 
			currentFile,
			progress.TotalFiles,
			sourcePath,
			float64(currentSize)/(1024*1024),
			float64(progress.TotalSize)/(1024*1024),
			eta)
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
	totalSize := progress.GetTotalSize()
	totalFiles := progress.GetTotalFiles()
	damagedFiles := atomic.LoadInt64(&progress.DamagedFiles)
	
	elapsed := time.Since(progress.StartTime)
	
	// Calculate ETA
	eta := "unknown"
	if copiedSize > 0 {
		remainingSize := totalSize - copiedSize
		if remainingSize > 0 {
			speed := float64(copiedSize) / elapsed.Seconds() // bytes per second
			etaSeconds := int64(float64(remainingSize) / speed)
			eta = formatETA(time.Duration(etaSeconds) * time.Second)
		} else {
			eta = "0s"
		}
	}

	// Get short filename for display
	currentFile := progress.GetCurrentFile()
	if len(currentFile) > 50 {
		parts := strings.Split(currentFile, string(os.PathSeparator))
		if len(parts) > 2 {
			currentFile = "..." + string(os.PathSeparator) + parts[len(parts)-2] + string(os.PathSeparator) + parts[len(parts)-1]
		}
	}

	copiedMB := float64(copiedSize) / (1024 * 1024)
	totalMB := float64(totalSize) / (1024 * 1024)
	
	// Show damaged files count if any
	damagedInfo := ""
	if damagedFiles > 0 {
		damagedInfo = fmt.Sprintf(", %d damaged", damagedFiles)
	}
	
	fmt.Printf("\rCopying: %s [%d/%d files, %.1f/%.1f MB, ETA: %s%s]",
		currentFile,
		processedFiles,
		totalFiles,
		copiedMB,
		totalMB,
		eta,
		damagedInfo)
}

// copyDirectoryWithDamageHandling copies entire directory structure with damage handling
func copyDirectoryWithDamageHandling(sourcePath, targetPath string, handler *DamagedDiskHandler) error {
	// First, scan directory to calculate total files and size
	progress := &CopyProgress{
		StartTime: time.Now(),
	}

	fmt.Println("🔍 Scanning directory structure...")
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			progress.logError(path, fmt.Errorf("error accessing during scan: %v", err))
			return nil // Skip files with errors
		}
		
		// For broken sources, use timeout for stat operations during scanning
		if info == nil {
			// Re-stat with timeout if info is nil
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				progress.logError(path, fmt.Errorf("stat timeout during scan: %v", statErr))
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

	if err != nil {
		return fmt.Errorf("error scanning directory: %v", err)
	}

	fmt.Printf("📁 Found %d files, total size: %.2f MB\n", 
		progress.GetTotalFiles(), float64(progress.GetTotalSize())/(1024*1024))

	// Now perform the actual copy with damage handling
	copyErr := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			progress.logError(path, fmt.Errorf("error accessing during copy: %v", err))
			return nil // Continue with other files
		}

		// For broken sources, handle cases where info might be corrupted
		if info == nil {
			// Re-stat with timeout if info is nil
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				progress.logError(path, fmt.Errorf("stat timeout during copy: %v", statErr))
				return nil
			}
			info = timeoutInfo
		}

		// Calculate relative path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			progress.logError(path, fmt.Errorf("error calculating relative path: %v", err))
			return nil
		}

		targetFilePath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			// Create directory
			err := os.MkdirAll(targetFilePath, info.Mode())
			if err != nil {
				progress.logError(targetFilePath, fmt.Errorf("error creating directory: %v", err))
			}
			return nil
		}

		// Copy file with damage handling and progress tracking
		progress.SetCurrentFile(path)
		return copyFileWithDamageHandlingAndProgress(path, targetFilePath, info, progress, handler)
	})
	
	// Print error summary at the end
	progress.printErrorSummary()
	
	return copyErr
}

// copyFileWithDamageHandling copies a single file with damage handling
func copyFileWithDamageHandling(sourcePath, targetPath string, sourceInfo os.FileInfo, handler *DamagedDiskHandler) error {
	progress := &CopyProgress{
		StartTime:  time.Now(),
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
		
		// Calculate ETA
		elapsed := time.Since(progress.StartTime)
		eta := "unknown"
		if currentSize > 0 {
			remainingSize := progress.TotalSize - currentSize
			if remainingSize > 0 {
				speed := float64(currentSize) / elapsed.Seconds() // bytes per second
				etaSeconds := int64(float64(remainingSize) / speed)
				eta = formatETA(time.Duration(etaSeconds) * time.Second)
			} else {
				eta = "0s"
			}
		}

		fmt.Printf("⏭️ Skipped: (%d/%d) %s (%.2f / %.2f MB) ETA: %s - already exists\n", 
			currentFile,
			progress.TotalFiles,
			sourcePath,
			float64(currentSize)/(1024*1024),
			float64(progress.TotalSize)/(1024*1024),
			eta)
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
