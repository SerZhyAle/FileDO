package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// Buffer pools for different sizes to reduce GC pressure
var (
	smallBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 1024*1024) // 1MB
			return &buf
		},
	}
	mediumBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 16*1024*1024) // 16MB
			return &buf
		},
	}
	largeBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 64*1024*1024) // 64MB
			return &buf
		},
	}
	tinyBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 256*1024) // 256KB for small files
			return &buf
		},
	}
)

// FastCopyConfig contains configuration for optimized copying
type FastCopyConfig struct {
	MaxConcurrentFiles int     // Max files to copy in parallel
	MinBufferSize      int     // Minimum buffer size (1MB)
	MaxBufferSize      int     // Maximum buffer size (64MB)
	LargeFileThreshold int64   // Files larger than this use large buffers (100MB)
	PreallocateSpace   bool    // Whether to preallocate space for large files
	UseMemoryMapping   bool    // Use memory mapping for large files (>LargeFileThreshold)
	MemoryMapThreshold int64   // Files larger than this use memory mapping (500MB)
	SmallFileThreshold int64   // Files smaller than this are batched together (1MB)
	SmallFileBatchSize int     // Number of small files to process in one batch (10)
}

// NewFastCopyConfig creates optimized configuration for different scenarios
func NewFastCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: runtime.NumCPU(),
		MinBufferSize:      1024 * 1024,      // 1MB
		MaxBufferSize:      64 * 1024 * 1024, // 64MB
		LargeFileThreshold: 100 * 1024 * 1024, // 100MB
		PreallocateSpace:   true,
		UseMemoryMapping:   true,                // Enable memory mapping for very large files
		MemoryMapThreshold: 500 * 1024 * 1024, // 500MB - use mmap for files larger than this
		SmallFileThreshold: 1024 * 1024,       // 1MB - files smaller than this are batched
		SmallFileBatchSize: 10,                 // Process 10 small files per goroutine
	}
}

// FastCopyProgress extends Progress with additional metrics
type FastCopyProgress struct {
	TotalFiles         int64
	ProcessedFiles     int64
	TotalSize          int64
	CopiedSize         int64
	StartTime          time.Time
	ActiveFiles        int64
	BytesPerSecond     float64
	LastSpeedUpdate    time.Time
	BufferPoolHits     int64 // Number of buffer pool reuses
	CurrentFile        string // Currently processing file name
	CurrentFileMux     sync.RWMutex // Mutex for CurrentFile access
	MemoryMappedFiles  int64 // Number of files copied using memory mapping
	MemoryMappedBytes  int64 // Total bytes copied using memory mapping
	SmallFileBatches   int64 // Number of small file batches processed
	BatchedFiles       int64 // Total number of files processed in batches
}

// calculateBufferSize determines optimal buffer size based on file size
func calculateBufferSize(fileSize int64, config FastCopyConfig) int {
	if fileSize >= config.LargeFileThreshold {
		return config.MaxBufferSize
	}
	
	// Scale buffer size based on file size
	ratio := float64(fileSize) / float64(config.LargeFileThreshold)
	bufferSize := int(float64(config.MinBufferSize) + ratio*float64(config.MaxBufferSize-config.MinBufferSize))
	
	if bufferSize < config.MinBufferSize {
		return config.MinBufferSize
	}
	if bufferSize > config.MaxBufferSize {
		return config.MaxBufferSize
	}
	
	return bufferSize
}

// getBufferFromPool retrieves a buffer from the appropriate pool and updates statistics
func getBufferFromPool(requiredSize int, progress *FastCopyProgress) ([]byte, *sync.Pool) {
	atomic.AddInt64(&progress.BufferPoolHits, 1)
	
	if requiredSize <= 256*1024 { // 256KB
		buf := tinyBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &tinyBufferPool
	} else if requiredSize <= 1024*1024 { // 1MB
		buf := smallBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &smallBufferPool
	} else if requiredSize <= 16*1024*1024 { // 16MB
		buf := mediumBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &mediumBufferPool
	} else { // Large files
		buf := largeBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &largeBufferPool
	}
}

// Windows-specific memory mapping functions
var (
	kernel32            = syscall.NewLazyDLL("kernel32.dll")
	procCreateFileMapping = kernel32.NewProc("CreateFileMappingW")
	procMapViewOfFile    = kernel32.NewProc("MapViewOfFile")
	procUnmapViewOfFile  = kernel32.NewProc("UnmapViewOfFile")
	procCloseHandle      = kernel32.NewProc("CloseHandle")
)

// copyFileWithMemoryMapping copies a file using Windows memory mapping
func copyFileWithMemoryMapping(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress) error {
	if runtime.GOOS != "windows" {
		// Fallback to regular copying on non-Windows systems
		return copyFileRegular(sourcePath, targetPath, sourceInfo, progress)
	}
	
	// Open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()
	
	// Create target file
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %v", err)
	}
	defer targetFile.Close()
	
	fileSize := sourceInfo.Size()
	
	// Preallocate space for target file
	if err := preallocateFile(targetFile, fileSize); err != nil {
		fmt.Printf("Warning: Failed to preallocate space: %v\n", err)
	}
	
	// Get Windows handles
	sourceHandle := syscall.Handle(sourceFile.Fd())
	targetHandle := syscall.Handle(targetFile.Fd())
	
	const chunkSize = 1024 * 1024 * 1024 // 1GB chunks for very large files
	
	for offset := int64(0); offset < fileSize; offset += chunkSize {
		remainingSize := fileSize - offset
		mapSize := chunkSize
		if remainingSize < chunkSize {
			mapSize = int(remainingSize)
		}
		
		// Create file mapping for source
		sourceMappingHandle, _, err := procCreateFileMapping.Call(
			uintptr(sourceHandle),
			0, // default security
			syscall.PAGE_READONLY,
			uintptr((offset+int64(mapSize))>>32), // high-order DWORD of size
			uintptr(offset+int64(mapSize)),       // low-order DWORD of size
			0, // unnamed mapping
		)
		
		if sourceMappingHandle == 0 {
			return fmt.Errorf("failed to create source file mapping: %v", err)
		}
		
		// Map source view
		sourceView, _, err := procMapViewOfFile.Call(
			sourceMappingHandle,
			syscall.FILE_MAP_READ,
			uintptr(offset>>32), // high-order DWORD of offset
			uintptr(offset),     // low-order DWORD of offset
			uintptr(mapSize),
		)
		
		if sourceView == 0 {
			procCloseHandle.Call(sourceMappingHandle)
			return fmt.Errorf("failed to map source view: %v", err)
		}
		
		// Create file mapping for target
		targetMappingHandle, _, err := procCreateFileMapping.Call(
			uintptr(targetHandle),
			0, // default security
			syscall.PAGE_READWRITE,
			uintptr((offset+int64(mapSize))>>32),
			uintptr(offset+int64(mapSize)),
			0, // unnamed mapping
		)
		
		if targetMappingHandle == 0 {
			procUnmapViewOfFile.Call(sourceView)
			procCloseHandle.Call(sourceMappingHandle)
			return fmt.Errorf("failed to create target file mapping: %v", err)
		}
		
		// Map target view
		targetView, _, err := procMapViewOfFile.Call(
			targetMappingHandle,
			syscall.FILE_MAP_WRITE,
			uintptr(offset>>32),
			uintptr(offset),
			uintptr(mapSize),
		)
		
		if targetView == 0 {
			procCloseHandle.Call(targetMappingHandle)
			procUnmapViewOfFile.Call(sourceView)
			procCloseHandle.Call(sourceMappingHandle)
			return fmt.Errorf("failed to map target view: %v", err)
		}
		
		// Perform memory copy
		sourceSlice := (*[1 << 30]byte)(unsafe.Pointer(sourceView))[:mapSize:mapSize]
		targetSlice := (*[1 << 30]byte)(unsafe.Pointer(targetView))[:mapSize:mapSize]
		
		copy(targetSlice, sourceSlice)
		
		// Update progress
		atomic.AddInt64(&progress.CopiedSize, int64(mapSize))
		
		// Cleanup for this chunk
		procUnmapViewOfFile.Call(targetView)
		procCloseHandle.Call(targetMappingHandle)
		procUnmapViewOfFile.Call(sourceView)
		procCloseHandle.Call(sourceMappingHandle)
	}
	
	return nil
}

// copyFileRegular copies a file using regular I/O (fallback)
func copyFileRegular(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress) error {
	// This is the existing copyFileSingle logic without memory mapping
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()
	
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %v", err)
	}
	defer targetFile.Close()
	
	if err := preallocateFile(targetFile, sourceInfo.Size()); err != nil {
		fmt.Printf("Warning: Failed to preallocate space: %v\n", err)
	}
	
	// Use buffer pool for 64MB buffer
	const bufferSize = 64 * 1024 * 1024 // 64MB
	buffer, pool := getBufferFromPool(bufferSize, progress)
	defer pool.Put(&buffer)
	
	for {
		bytesRead, readErr := sourceFile.Read(buffer)
		if bytesRead > 0 {
			_, writeErr := targetFile.Write(buffer[:bytesRead])
			if writeErr != nil {
				return fmt.Errorf("failed to write to target: %v", writeErr)
			}
			atomic.AddInt64(&progress.CopiedSize, int64(bytesRead))
		}
		
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("failed to read from source: %v", readErr)
		}
	}
	
	return nil
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

// FileJob represents a file copy job
type FileJob struct {
	SourcePath string
	TargetPath string
	Info       os.FileInfo
}

// SmallFileBatch represents a batch of small files to be processed together
type SmallFileBatch struct {
	Jobs []FileJob
}

// copyFileSingle copies a single file with optimizations (used by workers)
func copyFileSingle(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, config FastCopyConfig) error {
	atomic.AddInt64(&progress.ActiveFiles, 1)
	defer atomic.AddInt64(&progress.ActiveFiles, -1)
	
	// Set current file in progress
	progress.CurrentFileMux.Lock()
	progress.CurrentFile = sourcePath
	progress.CurrentFileMux.Unlock()
	
	// Check if target file already exists
	if _, err := statWithTimeout(targetPath, FileOperationTimeout); err == nil {
		fmt.Printf("Skipping existing file: %s\n", targetPath)
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		return nil
	}
	
	fileSize := sourceInfo.Size()
	
	// Use memory mapping for very large files
	if config.UseMemoryMapping && fileSize >= config.MemoryMapThreshold {
		atomic.AddInt64(&progress.MemoryMappedFiles, 1)
		atomic.AddInt64(&progress.MemoryMappedBytes, fileSize)
		
		err := copyFileWithMemoryMapping(sourcePath, targetPath, sourceInfo, progress)
		if err != nil {
			// Rollback statistics if memory mapping failed
			atomic.AddInt64(&progress.MemoryMappedFiles, -1)
			atomic.AddInt64(&progress.MemoryMappedBytes, -fileSize)
			
			// Fallback to regular copying if memory mapping fails
			fmt.Printf("Memory mapping failed for %s, falling back to regular copy: %v\n", sourcePath, err)
			err = copyFileRegular(sourcePath, targetPath, sourceInfo, progress)
		}
		
		if err == nil {
			// Set file permissions and timestamps
			if err := os.Chmod(targetPath, sourceInfo.Mode()); err != nil {
				fmt.Printf("Warning: Failed to set permissions for %s: %v\n", targetPath, err)
			}
			
			if err := os.Chtimes(targetPath, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
				fmt.Printf("Warning: Failed to set timestamps for %s: %v\n", targetPath, err)
			}
			
			atomic.AddInt64(&progress.ProcessedFiles, 1)
		}
		
		return err
	}
	
	// Regular buffered copying for smaller files
	return copyFileWithBuffers(sourcePath, targetPath, sourceInfo, progress, config)
}

// copyFileWithBuffers uses the existing buffer pool method for smaller files
func copyFileWithBuffers(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, config FastCopyConfig) error {
	// Open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()
	
	// Create target file
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %v", err)
	}
	defer func() {
		if closeErr := targetFile.Close(); closeErr != nil {
			fmt.Printf("Warning: Failed to close target file %s: %v\n", targetPath, closeErr)
		}
	}()
	
	// Preallocate space to reduce fragmentation
	if err := preallocateFile(targetFile, sourceInfo.Size()); err != nil {
		fmt.Printf("Warning: Failed to preallocate space for %s: %v\n", targetPath, err)
	}
	
	// Determine optimal buffer size and get buffer from pool
	bufferSize := calculateBufferSize(sourceInfo.Size(), config)
	buffer, bufferPool := getBufferFromPool(bufferSize, progress)
	defer bufferPool.Put(&buffer)
	
	// Ensure we use only the required buffer size
	buffer = buffer[:bufferSize]
	
	// Copy file with progress tracking
	for {
		bytesRead, readErr := sourceFile.Read(buffer)
		if bytesRead > 0 {
			_, writeErr := targetFile.Write(buffer[:bytesRead])
			if writeErr != nil {
				return fmt.Errorf("failed to write to target: %v", writeErr)
			}
			
			atomic.AddInt64(&progress.CopiedSize, int64(bytesRead))
		}
		
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("failed to read from source: %v", readErr)
		}
	}
	
	// Set file permissions and timestamps
	if err := os.Chmod(targetPath, sourceInfo.Mode()); err != nil {
		fmt.Printf("Warning: Failed to set permissions for %s: %v\n", targetPath, err)
	}
	
	if err := os.Chtimes(targetPath, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
		fmt.Printf("Warning: Failed to set timestamps for %s: %v\n", targetPath, err)
	}
	
	atomic.AddInt64(&progress.ProcessedFiles, 1)
	return nil
}

// copyDirectoryOptimized copies a directory with fast pre-scan and immediate copying
func copyDirectoryOptimized(sourcePath, targetPath string, progress *FastCopyProgress, config FastCopyConfig) error {
	fmt.Printf("Fast scanning for totals...\n")
	
	// Phase 1: Fast scan for totals only (no file collection)
	scanStart := time.Now()
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		if info.IsDir() {
			return nil
		}
		
		// Only count files and size
		progress.TotalFiles++
		progress.TotalSize += info.Size()
		return nil
	})
	
	if err != nil {
		return fmt.Errorf("directory scan failed: %v", err)
	}
	
	scanDuration := time.Since(scanStart)
	fmt.Printf("Scan completed in %v: %d files, %.2f GB\n", 
		scanDuration, progress.TotalFiles, float64(progress.TotalSize)/(1024*1024*1024))
	
	// Phase 2: Immediate copying with streaming processing
	fmt.Printf("Starting optimized copy with streaming pipeline...\n")
	
	// Channels for different job types
	smallBatchChannel := make(chan FileJob, config.SmallFileBatchSize*10) // Buffer for small files
	largeFileChannel := make(chan FileJob, config.MaxConcurrentFiles*2)   // Buffer for large files
	done := make(chan bool)
	copyComplete := make(chan bool)
	
	// Progress monitoring goroutine
	progressTicker := time.NewTicker(1 * time.Second)
	defer progressTicker.Stop()
	
	go func() {
		for {
			select {
			case <-progressTicker.C:
				showFastProgress(progress)
			case <-done:
				return
			}
		}
	}()
	
	// Small file batch processor
	var currentBatch []FileJob
	var batchWg sync.WaitGroup
	
	processBatch := func() {
		if len(currentBatch) > 0 {
			batch := SmallFileBatch{Jobs: make([]FileJob, len(currentBatch))}
			copy(batch.Jobs, currentBatch)
			
			batchWg.Add(1)
			go func() {
				defer batchWg.Done()
				if err := copySmallFileBatch(batch, progress, config); err != nil {
					fmt.Printf("Warning: Failed to copy batch: %v\n", err)
				}
			}()
			
			currentBatch = currentBatch[:0] // Clear batch
		}
	}
	
	// Start worker goroutines for large files
	var wg sync.WaitGroup
	for i := 0; i < config.MaxConcurrentFiles; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range largeFileChannel {
				if err := copyFileSingle(job.SourcePath, job.TargetPath, job.Info, progress, config); err != nil {
					fmt.Printf("Warning: Failed to copy %s: %v\n", job.SourcePath, err)
				}
			}
		}()
	}
	
	// Second-pass scanner and dispatcher (now with known totals)
	go func() {
		defer close(smallBatchChannel)
		defer close(largeFileChannel)
		defer close(copyComplete)
		
		scanErr := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
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
				// Create target directory immediately
				return os.MkdirAll(targetFilePath, info.Mode())
			}
			
			job := FileJob{
				SourcePath: path,
				TargetPath: targetFilePath,
				Info:       info,
			}
			
			// Route to appropriate processor
			if info.Size() < config.SmallFileThreshold {
				// Batch small files
				currentBatch = append(currentBatch, job)
				if len(currentBatch) >= config.SmallFileBatchSize {
					processBatch()
				}
			} else {
				// Send large files directly to workers
				largeFileChannel <- job
			}
			
			return nil
		})
		
		// Process remaining small files in the last batch
		processBatch()
		
		if scanErr != nil {
			fmt.Printf("Warning: Directory scan encountered errors: %v\n", scanErr)
		}
		
		// Wait for all small file batches to complete
		batchWg.Wait()
	}()
	
	// Wait for copy to complete
	<-copyComplete
	
	// Close large file channel and wait for workers
	wg.Wait()
	done <- true
	
	duration := time.Since(progress.StartTime)
	copyDuration := duration - scanDuration
	avgSpeed := float64(atomic.LoadInt64(&progress.CopiedSize)) / duration.Seconds() / (1024 * 1024) // MB/s
	
	fmt.Printf("\nOptimized copy completed in %v (scan: %v + copy: %v)\n", 
		duration, scanDuration, copyDuration)
	fmt.Printf("Average speed: %.2f MB/s\n", avgSpeed)
	fmt.Printf("Total files processed: %d\n", progress.TotalFiles)
	fmt.Printf("Buffer pool reuses: %d (%.1f%% memory savings)\n", 
		atomic.LoadInt64(&progress.BufferPoolHits), 
		float64(atomic.LoadInt64(&progress.BufferPoolHits))/float64(progress.TotalFiles)*100)
	
	memoryMappedFiles := atomic.LoadInt64(&progress.MemoryMappedFiles)
	memoryMappedBytes := atomic.LoadInt64(&progress.MemoryMappedBytes)
	if memoryMappedFiles > 0 {
		fmt.Printf("Memory-mapped files: %d (%.2f GB) - %.1f%% of total size\n",
			memoryMappedFiles,
			float64(memoryMappedBytes)/(1024*1024*1024),
			float64(memoryMappedBytes)/float64(progress.TotalSize)*100)
	}
	
	smallFileBatchCount := atomic.LoadInt64(&progress.SmallFileBatches)
	batchedFiles := atomic.LoadInt64(&progress.BatchedFiles)
	if smallFileBatchCount > 0 {
		fmt.Printf("Small file batches: %d (avg %.1f files/batch) - %d files optimized\n",
			smallFileBatchCount,
			float64(batchedFiles)/float64(smallFileBatchCount),
			batchedFiles)
	}
	
	return nil
}

// copySmallFileBatch copies a batch of small files sequentially in one goroutine
func copySmallFileBatch(batch SmallFileBatch, progress *FastCopyProgress, config FastCopyConfig) error {
	atomic.AddInt64(&progress.ActiveFiles, 1)
	defer atomic.AddInt64(&progress.ActiveFiles, -1)
	
	atomic.AddInt64(&progress.SmallFileBatches, 1)
	atomic.AddInt64(&progress.BatchedFiles, int64(len(batch.Jobs)))
	
	// Use buffer pool for 256KB buffer
	const bufferSize = 256 * 1024 // 256KB buffer for small files
	buffer, pool := getBufferFromPool(bufferSize, progress)
	defer pool.Put(&buffer)
	
	for _, job := range batch.Jobs {
		// Update current file
		progress.CurrentFileMux.Lock()
		progress.CurrentFile = job.SourcePath
		progress.CurrentFileMux.Unlock()
		
		// Check if target file already exists
		if _, err := statWithTimeout(job.TargetPath, FileOperationTimeout); err == nil {
			fmt.Printf("Skipping existing file: %s\n", job.TargetPath)
			atomic.AddInt64(&progress.ProcessedFiles, 1)
			continue
		}
		
		// Copy single small file
		if err := copySmallFileDirect(job.SourcePath, job.TargetPath, job.Info, progress, buffer); err != nil {
			fmt.Printf("Warning: Failed to copy small file %s: %v\n", job.SourcePath, err)
		}
	}
	
	return nil
}

// copySmallFileDirect copies a small file directly without goroutine overhead
func copySmallFileDirect(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, buffer []byte) error {
	// Open source file
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()
	
	// Create target file
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %v", err)
	}
	defer targetFile.Close()
	
	// Copy file content using provided buffer
	for {
		bytesRead, readErr := sourceFile.Read(buffer)
		if bytesRead > 0 {
			_, writeErr := targetFile.Write(buffer[:bytesRead])
			if writeErr != nil {
				return fmt.Errorf("failed to write to target: %v", writeErr)
			}
			
			atomic.AddInt64(&progress.CopiedSize, int64(bytesRead))
		}
		
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("failed to read from source: %v", readErr)
		}
	}
	
	// Set file permissions and timestamps
	if err := os.Chmod(targetPath, sourceInfo.Mode()); err != nil {
		fmt.Printf("Warning: Failed to set permissions for %s: %v\n", targetPath, err)
	}
	
	if err := os.Chtimes(targetPath, sourceInfo.ModTime(), sourceInfo.ModTime()); err != nil {
		fmt.Printf("Warning: Failed to set timestamps for %s: %v\n", targetPath, err)
	}
	
	atomic.AddInt64(&progress.ProcessedFiles, 1)
	return nil
}

// showFastProgress displays enhanced progress information with current file
func showFastProgress(progress *FastCopyProgress) {
	processedFiles := atomic.LoadInt64(&progress.ProcessedFiles)
	copiedSize := atomic.LoadInt64(&progress.CopiedSize)
	totalFiles := atomic.LoadInt64(&progress.TotalFiles)
	totalSize := atomic.LoadInt64(&progress.TotalSize)
	activeFiles := atomic.LoadInt64(&progress.ActiveFiles)
	
	// Get current file safely
	progress.CurrentFileMux.RLock()
	currentFile := progress.CurrentFile
	progress.CurrentFileMux.RUnlock()
	
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
	if copiedSize > 0 && totalSize > 0 {
		remainingSize := totalSize - copiedSize
		if remainingSize > 0 && progress.BytesPerSecond > 0 {
			etaSeconds := float64(remainingSize) / (progress.BytesPerSecond * 1024 * 1024)
			eta = formatETA(time.Duration(etaSeconds * float64(time.Second)))
		}
	}
	
	// Calculate percentages
	filePercent := float64(0)
	sizePercent := float64(0)
	
	if totalFiles > 0 {
		filePercent = float64(processedFiles) / float64(totalFiles) * 100
	}
	
	if totalSize > 0 {
		sizePercent = float64(copiedSize) / float64(totalSize) * 100
	}
	
	// Don't truncate filename - show full path
	displayFile := currentFile
	
	fmt.Printf("\r%d/%d %s (%.1f%%) | %.2f/%.2f GB (%.1f%%) | %.2f MB/s | %d | ETA: %s",
		processedFiles, totalFiles, displayFile, filePercent,
		float64(copiedSize)/(1024*1024*1024), float64(totalSize)/(1024*1024*1024), sizePercent,
		progress.BytesPerSecond, activeFiles, eta)
}

// FastCopy performs optimized copying with single-pass directory scanning
func FastCopy(sourcePath, targetPath string) error {
	config := NewFastCopyConfig()
	progress := &FastCopyProgress{
		StartTime:       time.Now(),
		LastSpeedUpdate: time.Now(),
	}
	
	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path error: %v", err)
	}
	
	if sourceInfo.IsDir() {
		// Directory copy with optimization
		return copyDirectoryOptimized(sourcePath, targetPath, progress, config)
	} else {
		// Single file copy
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		return copyFileSingle(sourcePath, targetPath, sourceInfo, progress, config)
	}
}
