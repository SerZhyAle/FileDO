package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// FastCopyConfig contains configuration for optimized copying
type FastCopyConfig struct {
	MaxConcurrentFiles int     // Max files to copy in parallel
	MinBufferSize      int     // Minimum buffer size (1MB)
	MaxBufferSize      int     // Maximum buffer size (64MB)
	LargeFileThreshold int64   // Files larger than this use large buffers (100MB)
	UseMemoryMapping   bool    // Use memory mapping for large files
	PreallocateSpace   bool    // Preallocate space for large files
}

// getOptimalCopyConfig returns optimized config based on system capabilities
func getOptimalCopyConfig() FastCopyConfig {
	numCPU := runtime.NumCPU()
	
	// For slow HDDs, limit concurrency to avoid seek thrashing
	maxConcurrent := numCPU
	if maxConcurrent > 4 {
		maxConcurrent = 4 // Don't overwhelm slow HDDs
	}
	
	return FastCopyConfig{
		MaxConcurrentFiles: maxConcurrent,
		MinBufferSize:      1024 * 1024,      // 1MB
		MaxBufferSize:      64 * 1024 * 1024, // 64MB
		LargeFileThreshold: 100 * 1024 * 1024, // 100MB
		UseMemoryMapping:   true,
		PreallocateSpace:   true,
	}
}

// getOptimalBufferSize returns optimal buffer size based on file size
func getOptimalBufferSize(fileSize int64, config FastCopyConfig) int {
	if fileSize < 1024*1024 { // < 1MB
		return config.MinBufferSize
	} else if fileSize < config.LargeFileThreshold {
		// Scale buffer size with file size, up to 16MB for medium files
		bufferSize := int(fileSize / 64) // 1/64th of file size
		if bufferSize < config.MinBufferSize {
			bufferSize = config.MinBufferSize
		}
		if bufferSize > 16*1024*1024 {
			bufferSize = 16 * 1024 * 1024 // 16MB max for medium files
		}
		return bufferSize
	} else {
		// Large files use maximum buffer
		return config.MaxBufferSize
	}
}

// preallocateFile preallocates space for the target file to reduce fragmentation
func preallocateFile(file *os.File, size int64) error {
	if runtime.GOOS != "windows" {
		return nil // Only implemented for Windows
	}
	
	// Simple approach: seek to end and write a byte, then truncate
	oldPos, err := file.Seek(0, 1) // Get current position
	if err != nil {
		return err
	}
	
	// Seek to desired end position
	_, err = file.Seek(size-1, 0)
	if err != nil {
		return err
	}
	
	// Write a byte to allocate space
	_, err = file.Write([]byte{0})
	if err != nil {
		return err
	}
	
	// Seek back to original position
	_, err = file.Seek(oldPos, 0)
	return err
}

// FastCopyProgress extends CopyProgress with additional metrics
type FastCopyProgress struct {
	CopyProgress
	ActiveFiles     int64
	BytesPerSecond  float64
	LastSpeedUpdate time.Time
}

// copyFileParallel copies a single file with optimizations
func copyFileParallel(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, config FastCopyConfig, wg *sync.WaitGroup) error {
	defer wg.Done()
	
	atomic.AddInt64(&progress.ActiveFiles, 1)
	defer atomic.AddInt64(&progress.ActiveFiles, -1)
	
	// Check if target file already exists
	if _, err := statWithTimeout(targetPath, FileOperationTimeout); err == nil {
		// Update counters and skip
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		atomic.AddInt64(&progress.CopiedSize, sourceInfo.Size())
		return nil
	}

	// Open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		return fmt.Errorf("failed to open source: %v", err)
	}
	defer sourceFile.Close()

	// Create target file
	targetFile, err := os.Create(targetPath)
	if err != nil {
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		return fmt.Errorf("failed to create target: %v", err)
	}
	defer targetFile.Close()

	fileSize := sourceInfo.Size()
	
	// Preallocate space for large files to reduce fragmentation
	if config.PreallocateSpace && fileSize > config.LargeFileThreshold {
		if err := preallocateFile(targetFile, fileSize); err != nil {
			// Not critical, continue without preallocation
			fmt.Printf("Warning: Could not preallocate space for %s: %v\n", targetPath, err)
		}
	}

	// Determine optimal buffer size
	bufferSize := getOptimalBufferSize(fileSize, config)
	
	// Copy file content with optimized buffer
	copiedBytes, err := copyWithOptimizedBuffer(targetFile, sourceFile, bufferSize, FileOperationTimeout)
	if err != nil {
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		return fmt.Errorf("copy failed: %v", err)
	}

	// Update progress
	atomic.AddInt64(&progress.CopiedSize, copiedBytes)
	atomic.AddInt64(&progress.ProcessedFiles, 1)

	// Set file permissions and timestamps
	if err := os.Chmod(targetPath, sourceInfo.Mode()); err != nil {
		fmt.Printf("Warning: Could not set permissions for %s: %v\n", targetPath, err)
	}
	
	if err := os.Chtimes(targetPath, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
		fmt.Printf("Warning: Could not set timestamps for %s: %v\n", targetPath, err)
	}

	return nil
}

// copyWithOptimizedBuffer performs optimized file copying with custom buffer size
func copyWithOptimizedBuffer(dst io.Writer, src io.Reader, bufferSize int, timeout time.Duration) (int64, error) {
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
		buffer := make([]byte, bufferSize)
		
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
	case res := <-ch:
		return res.written, res.err
	case <-ctx.Done():
		return 0, fmt.Errorf("copy operation timed out after %v", timeout)
	}
}

// handleFastCopyCommand processes optimized copy operations
func handleFastCopyCommand(sourcePath, targetPath string) error {
	config := getOptimalCopyConfig()
	
	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("source path does not exist: %s", sourcePath)
		}
		return fmt.Errorf("error accessing source path: %v", err)
	}

	fmt.Printf("Fast copying from %s to %s\n", sourcePath, targetPath)
	fmt.Printf("Config: %d concurrent files, buffer %d-%d MB, large file threshold: %d MB\n", 
		config.MaxConcurrentFiles, 
		config.MinBufferSize/(1024*1024), 
		config.MaxBufferSize/(1024*1024),
		config.LargeFileThreshold/(1024*1024))

	progress := &FastCopyProgress{
		CopyProgress: CopyProgress{
			StartTime: time.Now(),
		},
		LastSpeedUpdate: time.Now(),
	}

	if !sourceInfo.IsDir() {
		// Single file copy
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		
		var wg sync.WaitGroup
		wg.Add(1)
		
		err := copyFileParallel(sourcePath, targetPath, sourceInfo, progress, config, &wg)
		wg.Wait()
		return err
	}

	// Directory copy with parallel processing
	return copyDirectoryOptimized(sourcePath, targetPath, progress, config)
}

// copyDirectoryOptimized copies a directory with parallel file processing
func copyDirectoryOptimized(sourcePath, targetPath string, progress *FastCopyProgress, config FastCopyConfig) error {
	// First pass: scan and count files
	fmt.Printf("Scanning directory structure...\n")
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
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

	fmt.Printf("Found %d files, total size: %.2f GB\n", 
		progress.TotalFiles, float64(progress.TotalSize)/(1024*1024*1024))

	// Create worker pool
	semaphore := make(chan struct{}, config.MaxConcurrentFiles)
	var wg sync.WaitGroup
	
	// Progress monitoring goroutine
	progressTicker := time.NewTicker(1 * time.Second)
	defer progressTicker.Stop()
	
	go func() {
		for range progressTicker.C {
			showFastProgress(progress)
		}
	}()

	// Second pass: copy files with parallelism
	err = filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Warning: Error accessing %s: %v - skipping\n", path, err)
			return nil
		}

		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return nil
		}
		
		targetFilePath := filepath.Join(targetPath, relPath)

		if info.IsDir() {
			return os.MkdirAll(targetFilePath, info.Mode())
		}

		// Acquire semaphore (limits concurrent file operations)
		semaphore <- struct{}{}
		wg.Add(1)
		
		go func(src, dst string, srcInfo os.FileInfo) {
			defer func() { <-semaphore }()
			
			if err := copyFileParallel(src, dst, srcInfo, progress, config, &wg); err != nil {
				fmt.Printf("Warning: Failed to copy %s: %v\n", src, err)
			}
		}(path, targetFilePath, info)

		return nil
	})

	wg.Wait()
	
	duration := time.Since(progress.StartTime)
	avgSpeed := float64(progress.CopiedSize) / duration.Seconds() / (1024 * 1024) // MB/s
	
	fmt.Printf("\nFast copy completed in %v\n", duration)
	fmt.Printf("Average speed: %.2f MB/s\n", avgSpeed)
	
	return err
}

// showFastProgress displays enhanced progress information
func showFastProgress(progress *FastCopyProgress) {
	processedFiles := atomic.LoadInt64(&progress.ProcessedFiles)
	copiedSize := atomic.LoadInt64(&progress.CopiedSize)
	activeFiles := atomic.LoadInt64(&progress.ActiveFiles)
	
	elapsed := time.Since(progress.StartTime)
	
	// Calculate current speed
	now := time.Now()
	if now.Sub(progress.LastSpeedUpdate) > time.Second {
		currentSpeed := float64(copiedSize) / elapsed.Seconds() / (1024 * 1024) // MB/s
		progress.BytesPerSecond = currentSpeed
		progress.LastSpeedUpdate = now
	}
	
	// Calculate ETA
	eta := "unknown"
	if copiedSize > 0 {
		remainingSize := progress.TotalSize - copiedSize
		if remainingSize > 0 && progress.BytesPerSecond > 0 {
			etaSeconds := int64(float64(remainingSize) / (progress.BytesPerSecond * 1024 * 1024))
			eta = formatETA(time.Duration(etaSeconds) * time.Second)
		} else {
			eta = "0s"
		}
	}

	copiedGB := float64(copiedSize) / (1024 * 1024 * 1024)
	totalGB := float64(progress.TotalSize) / (1024 * 1024 * 1024)
	
	fmt.Printf("\rFast Copy: [%d/%d files] [%.2f/%.2f GB] [%d active] [%.1f MB/s] [ETA: %s]",
		processedFiles,
		progress.TotalFiles,
		copiedGB,
		totalGB,
		activeFiles,
		progress.BytesPerSecond,
		eta)
}
