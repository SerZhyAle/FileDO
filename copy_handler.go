package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// CopyProgress tracks the progress of copy operations
type CopyProgress struct {
	TotalFiles      int64
	ProcessedFiles  int64
	TotalSize       int64
	CopiedSize      int64
	StartTime       time.Time
	CurrentFile     string
}

// FileOperationTimeout defines timeout for file operations with broken sources
const FileOperationTimeout = 3 * time.Second

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

// handleCopyCommand processes the copy command
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

	fmt.Printf("Copying from %s to %s\n", sourcePath, targetPath)

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
			fmt.Fprintf(os.Stderr, "Warning: Error accessing %s: %v\n", path, err)
			return nil // Skip files with errors
		}
		
		// For broken sources, use timeout for stat operations during scanning
		if info == nil {
			// Re-stat with timeout if info is nil
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: Stat timeout for %s: %v - skipping\n", path, statErr)
				return nil
			}
			info = timeoutInfo
		}
		
		if !info.IsDir() {
			progress.TotalFiles++
			progress.TotalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("error scanning directory: %v", err)
	}

	fmt.Printf("Found %d files, total size: %.2f MB\n", 
		progress.TotalFiles, float64(progress.TotalSize)/(1024*1024))

	// Now perform the actual copy
	return filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Error accessing %s: %v - skipping\n", path, err)
			return nil // Continue with other files
		}

		// For broken sources, handle cases where info might be corrupted
		if info == nil {
			// Re-stat with timeout if info is nil
			timeoutInfo, statErr := statWithTimeout(path, FileOperationTimeout)
			if statErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: Stat timeout for %s: %v - skipping\n", path, statErr)
				return nil
			}
			info = timeoutInfo
		}

		// Calculate relative path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error calculating relative path for %s: %v - skipping\n", path, err)
			return nil
		}

		targetFilePath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			// Create directory
			err := os.MkdirAll(targetFilePath, info.Mode())
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory %s: %v\n", targetFilePath, err)
			}
			return nil
		}

		// Copy file with progress and timeout handling
		progress.CurrentFile = path
		return copyFileWithCopyProgress(path, targetFilePath, info, progress)
	})
}

// copyFile copies a single file
func copyFile(sourcePath, targetPath string) error {
	sourceInfo, err := statWithTimeout(sourcePath, FileOperationTimeout)
	if err != nil {
		return fmt.Errorf("cannot stat source file: %v", err)
	}

	progress := &CopyProgress{
		TotalFiles: 1,
		TotalSize:  sourceInfo.Size(),
		StartTime:  time.Now(),
		CurrentFile: sourcePath,
	}

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
				eta = formatDuration(time.Duration(etaSeconds) * time.Second)
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
		currentFile := atomic.AddInt64(&progress.ProcessedFiles, 1)
		currentSize := atomic.LoadInt64(&progress.CopiedSize)
		
		fmt.Fprintf(os.Stderr, "Error: (%d/%d) %s (%.2f / %.2f MB) - %v\n", 
			currentFile,
			progress.TotalFiles,
			sourcePath,
			float64(currentSize)/(1024*1024),
			float64(progress.TotalSize)/(1024*1024),
			err)
		return nil // Continue with other files
	}
	defer sourceFile.Close()

	// Create target file
	targetFile, err := os.Create(targetPath)
	if err != nil {
		// Update counters even on error for progress consistency
		currentFile := atomic.AddInt64(&progress.ProcessedFiles, 1)
		currentSize := atomic.LoadInt64(&progress.CopiedSize)
		
		fmt.Fprintf(os.Stderr, "Error: (%d/%d) %s (%.2f / %.2f MB) - cannot create target: %v\n", 
			currentFile,
			progress.TotalFiles,
			sourcePath,
			float64(currentSize)/(1024*1024),
			float64(progress.TotalSize)/(1024*1024),
			err)
		return nil // Continue with other files
	}
	defer targetFile.Close()

	// Copy file content with timeout and progress reporting
	copiedBytes, err := copyWithTimeout(targetFile, sourceFile, FileOperationTimeout)
	if err != nil {
		// Update counters even on timeout/error
		currentFile := atomic.AddInt64(&progress.ProcessedFiles, 1)
		currentSize := atomic.LoadInt64(&progress.CopiedSize)
		
		// Calculate ETA
		elapsed := time.Since(progress.StartTime)
		eta := "unknown"
		if currentSize > 0 {
			remainingSize := progress.TotalSize - currentSize
			if remainingSize > 0 {
				speed := float64(currentSize) / elapsed.Seconds()
				etaSeconds := int64(float64(remainingSize) / speed)
				eta = formatDuration(time.Duration(etaSeconds) * time.Second)
			} else {
				eta = "0s"
			}
		}
		
		fmt.Fprintf(os.Stderr, "Error: (%d/%d) %s (%.2f / %.2f MB) ETA: %s - copy timeout: %v\n", 
			currentFile,
			progress.TotalFiles,
			sourcePath,
			float64(currentSize)/(1024*1024),
			float64(progress.TotalSize)/(1024*1024),
			eta,
			err)
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
	processedFiles := atomic.LoadInt64(&progress.ProcessedFiles)
	copiedSize := atomic.LoadInt64(&progress.CopiedSize)
	
	elapsed := time.Since(progress.StartTime)
	
	// Calculate ETA
	eta := "unknown"
	if copiedSize > 0 {
		remainingSize := progress.TotalSize - copiedSize
		if remainingSize > 0 {
			speed := float64(copiedSize) / elapsed.Seconds() // bytes per second
			etaSeconds := int64(float64(remainingSize) / speed)
			eta = formatDuration(time.Duration(etaSeconds) * time.Second)
		} else {
			eta = "0s"
		}
	}

	// Get short filename for display
	currentFile := progress.CurrentFile
	if len(currentFile) > 50 {
		parts := strings.Split(currentFile, string(os.PathSeparator))
		if len(parts) > 2 {
			currentFile = "..." + string(os.PathSeparator) + parts[len(parts)-2] + string(os.PathSeparator) + parts[len(parts)-1]
		}
	}

	copiedMB := float64(copiedSize) / (1024 * 1024)
	totalMB := float64(progress.TotalSize) / (1024 * 1024)
	
	fmt.Printf("\rCopying: %s [%d/%d files, %.1f/%.1f MB, ETA: %s]",
		currentFile,
		processedFiles,
		progress.TotalFiles,
		copiedMB,
		totalMB,
		eta)
}
