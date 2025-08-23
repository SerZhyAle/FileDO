package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	superLargeBufferPool = sync.Pool{
		New: func() interface{} {
			buf := make([]byte, 128*1024*1024) // 128MB for maximum performance
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
	DirectIO           bool    // Use direct I/O to bypass OS cache (reduce speed differences)
	ForceFlush         bool    // Force immediate flush to disk after each file
	SyncReadWrite      bool    // Synchronize read and write operations (reduce cache effects)
}

// NewFastCopyConfig creates optimized configuration for different scenarios
func NewFastCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 8,                      // Limited to 8 threads for better balance with HDD
		MinBufferSize:      1024 * 1024,            // 1MB
		MaxBufferSize:      64 * 1024 * 1024,       // 64MB
		LargeFileThreshold: 100 * 1024 * 1024,      // 100MB
		PreallocateSpace:   true,
		UseMemoryMapping:   true,                   // Enable memory mapping for very large files
		MemoryMapThreshold: 500 * 1024 * 1024,     // 500MB - use mmap for files larger than this
		SmallFileThreshold: 2 * 1024 * 1024,       // 2MB - files smaller than this are batched (increased for images)
		SmallFileBatchSize: 25,                     // Process 25 small files per goroutine (increased for images)
		DirectIO:           false,                  // Disable by default (may reduce performance)
		ForceFlush:         false,                  // Disable by default (may reduce performance) 
		SyncReadWrite:      false,                  // Disable by default (may reduce performance)
	}
}

// NewSyncCopyConfig creates configuration for synchronized copying to match read/write speeds
func NewSyncCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 1,                   // Single-threaded for synchronized operation
		MinBufferSize:      4 * 1024 * 1024,   // 4MB - smaller buffers for more frequent progress updates
		MaxBufferSize:      16 * 1024 * 1024,  // 16MB - reduced buffer to avoid excessive caching
		LargeFileThreshold: 50 * 1024 * 1024,  // 50MB - lower threshold for immediate processing
		PreallocateSpace:   false,              // Disable preallocation to reduce cache effects
		UseMemoryMapping:   false,              // Disable memory mapping to force direct I/O
		MemoryMapThreshold: 0,                  // Never use memory mapping in sync mode
		SmallFileThreshold: 1024 * 1024,       // 1MB - smaller batching
		SmallFileBatchSize: 5,                  // Smaller batches for more regular progress
		DirectIO:           true,               // Enable direct I/O to bypass cache
		ForceFlush:         true,               // Force immediate flush to disk
		SyncReadWrite:      true,               // Synchronize read/write operations
	}
}

// NewBalancedCopyConfig creates configuration optimized for HDD-to-HDD copying
func NewBalancedCopyConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 4,                      // Very limited threads for HDD operations
		MinBufferSize:      16 * 1024 * 1024,       // 16MB - larger buffers for HDD efficiency
		MaxBufferSize:      64 * 1024 * 1024,       // 64MB - optimal for HDD sequential access
		LargeFileThreshold: 50 * 1024 * 1024,       // 50MB - lower threshold
		PreallocateSpace:   true,                   // Preallocation helps with HDD fragmentation
		UseMemoryMapping:   false,                  // Disable mmap for better control over I/O
		MemoryMapThreshold: 0,                      // Never use memory mapping
		SmallFileThreshold: 4 * 1024 * 1024,       // 4MB - larger threshold for less threading
		SmallFileBatchSize: 10,                     // Smaller batches for HDD
		DirectIO:           false,                  // Keep caching enabled for HDD
		ForceFlush:         false,                  // Let OS manage flushing
		SyncReadWrite:      false,                  // Async for better performance
	}
}

// NewMaxPerformanceConfig creates configuration for maximum CPU utilization
func NewMaxPerformanceConfig() FastCopyConfig {
	return FastCopyConfig{
		MaxConcurrentFiles: 16,                      // Limited to 16 threads for optimal balance
		MinBufferSize:      8 * 1024 * 1024,        // 8MB - larger buffers for efficiency
		MaxBufferSize:      128 * 1024 * 1024,      // 128MB - maximum buffer size with super large pool
		LargeFileThreshold: 50 * 1024 * 1024,       // 50MB - lower threshold for more parallel files
		PreallocateSpace:   true,                    // Enable preallocation for performance
		UseMemoryMapping:   true,                    // Enable memory mapping for large files
		MemoryMapThreshold: 100 * 1024 * 1024,      // 100MB - lower threshold for more mmap usage
		SmallFileThreshold: 512 * 1024,             // 512KB - smaller batching for more parallelism
		SmallFileBatchSize: 50,                     // Larger batches for efficiency
		DirectIO:           false,                   // Disable DirectIO for maximum speed
		ForceFlush:         false,                   // Disable ForceFlush for maximum speed
		SyncReadWrite:      false,                   // Disable sync for maximum speed
	}
}

// FastCopyProgress extends Progress with additional metrics
type FastCopyProgress struct {
	TotalFiles         int64
	ProcessedFiles     int64
	TotalSize          int64
	CopiedSize         int64
	ActualCopiedSize   int64 // Size of files actually copied (excluding skipped files)
	SkippedFiles       int64 // Number of skipped files (already exist)
	SkippedSize        int64 // Size of skipped files
	ActualFiles        int64 // Files that need to be copied (TotalFiles - SkippedFiles)
	ActualSize         int64 // Size that needs to be copied (TotalSize - SkippedSize)
	StartTime          time.Time
	ActiveFiles        int64
	BytesPerSecond     float64
	LastSpeedUpdate    time.Time
	BufferPoolHits     int64 // Number of buffer pool reuses
	CurrentFile        string // Currently processing file name
	CurrentFileMux     sync.RWMutex // Mutex for CurrentFile access
	ActiveFilesList    []ActiveFileInfo // List of currently processing files with sizes
	ActiveFilesMux     sync.RWMutex // Mutex for ActiveFilesList access
	LargeFilesList     []ActiveFileInfo // List of large files currently processing (priority display)
	LargeFilesMux      sync.RWMutex // Mutex for LargeFilesList access
	LastDisplayedFile  int    // Index for rotating displayed file
	MemoryMappedFiles  int64 // Number of files copied using memory mapping
	MemoryMappedBytes  int64 // Total bytes copied using memory mapping
	SmallFileBatches   int64 // Number of small file batches processed
	BatchedFiles       int64 // Total number of files processed in batches
}

// ActiveFileInfo stores information about currently processing file
type ActiveFileInfo struct {
	Path string
	Size int64
}

// addActiveFile adds a file to the active files list for progress display
func (progress *FastCopyProgress) addActiveFile(filepath string, filesize int64) {
	progress.ActiveFilesMux.Lock()
	defer progress.ActiveFilesMux.Unlock()
	
	// Remove duplicates and add new file
	filtered := make([]ActiveFileInfo, 0, len(progress.ActiveFilesList))
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
	
	// Remove duplicates and add new large file
	filtered := make([]ActiveFileInfo, 0, len(progress.LargeFilesList))
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
		// Show the most recently added large file
		largeFile := progress.LargeFilesList[len(progress.LargeFilesList)-1]
		progress.LargeFilesMux.RUnlock()
		return largeFile.Path, largeFile.Size
	}
	progress.LargeFilesMux.RUnlock()
	
	// Priority 2: Show regular active files
	progress.ActiveFilesMux.RLock()
	defer progress.ActiveFilesMux.RUnlock()
	
	if len(progress.ActiveFilesList) == 0 {
		// Fallback to old CurrentFile system
		progress.CurrentFileMux.RLock()
		currentFile := progress.CurrentFile
		progress.CurrentFileMux.RUnlock()
		return currentFile, 0 // Unknown size for fallback
	}
	
	// Show the most recently added file (currently being processed)
	activeFile := progress.ActiveFilesList[len(progress.ActiveFilesList)-1]
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
	
	// Ensure requiredSize doesn't exceed maximum buffer capacity
	const maxBufferCapacity = 128 * 1024 * 1024 // 128MB - our largest pool
	if requiredSize > maxBufferCapacity {
		requiredSize = maxBufferCapacity
	}
	
	if requiredSize <= 256*1024 { // 256KB
		buf := tinyBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &tinyBufferPool
	} else if requiredSize <= 1024*1024 { // 1MB
		buf := smallBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &smallBufferPool
	} else if requiredSize <= 16*1024*1024 { // 16MB
		buf := mediumBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &mediumBufferPool
	} else if requiredSize <= 64*1024*1024 { // 64MB
		buf := largeBufferPool.Get().(*[]byte)
		return (*buf)[:requiredSize], &largeBufferPool
	} else { // Super large files - 128MB
		buf := superLargeBufferPool.Get().(*[]byte)
		actualSize := requiredSize
		if actualSize > 128*1024*1024 {
			actualSize = 128*1024*1024
		}
		return (*buf)[:actualSize], &superLargeBufferPool
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
func copyFileWithMemoryMapping(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, handler *InterruptHandler) error {
	if runtime.GOOS != "windows" {
		// Fallback to regular copying on non-Windows systems
		return copyFileRegular(sourcePath, targetPath, sourceInfo, progress, handler)
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
	atomic.AddInt64(&progress.ActualCopiedSize, int64(mapSize))		// Cleanup for this chunk
		procUnmapViewOfFile.Call(targetView)
		procCloseHandle.Call(targetMappingHandle)
		procUnmapViewOfFile.Call(sourceView)
		procCloseHandle.Call(sourceMappingHandle)
	}
	
	return nil
}

// copyFileRegular copies a file using regular I/O (fallback)
func copyFileRegular(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, handler *InterruptHandler) error {
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
		// Check for forced exit (double Ctrl+C)
		if handler != nil && handler.IsForceExit() {
			return fmt.Errorf("operation terminated by user (force exit)")
		}
		
		// Check for graceful interruption
		if handler != nil && handler.IsInterrupted() {
			return fmt.Errorf("operation interrupted by user")
		}
		
		bytesRead, readErr := sourceFile.Read(buffer)
		if bytesRead > 0 {
			_, writeErr := targetFile.Write(buffer[:bytesRead])
			if writeErr != nil {
				return fmt.Errorf("failed to write to target: %v", writeErr)
			}
			atomic.AddInt64(&progress.CopiedSize, int64(bytesRead))
			atomic.AddInt64(&progress.ActualCopiedSize, int64(bytesRead))
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
func copyFileSingle(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, config FastCopyConfig, handler *InterruptHandler) error {
	// Check for interruption before starting
	if handler.IsCancelled() {
		return fmt.Errorf("operation cancelled by user")
	}
	
	atomic.AddInt64(&progress.ActiveFiles, 1)
	defer atomic.AddInt64(&progress.ActiveFiles, -1)
	
	fileSize := sourceInfo.Size()
	isLargeFile := fileSize >= config.LargeFileThreshold // 100MB+
	
	// Add to active files list for progress display
	progress.addActiveFile(sourcePath, fileSize)
	defer progress.removeActiveFile(sourcePath)
	
	// Add large files to priority display list
	if isLargeFile {
		progress.addLargeFile(sourcePath, fileSize)
		defer progress.removeLargeFile(sourcePath)
	}
	
	// Set current file in progress (backward compatibility)
	progress.CurrentFileMux.Lock()
	progress.CurrentFile = sourcePath
	progress.CurrentFileMux.Unlock()
	
	// Check if target file already exists
	if _, err := statWithTimeout(targetPath, FileOperationTimeout); err == nil {
		fmt.Printf("Skipping existing file: %s\n", targetPath)
		atomic.AddInt64(&progress.ProcessedFiles, 1)
		atomic.AddInt64(&progress.SkippedFiles, 1)
		atomic.AddInt64(&progress.SkippedSize, fileSize)
		atomic.AddInt64(&progress.CopiedSize, fileSize) // Add file size to copied total
		// Update actual values
		atomic.AddInt64(&progress.ActualFiles, -1)
		atomic.AddInt64(&progress.ActualSize, -fileSize)
		return nil
	}
	
	// Use memory mapping for very large files
	if config.UseMemoryMapping && fileSize >= config.MemoryMapThreshold {
		atomic.AddInt64(&progress.MemoryMappedFiles, 1)
		atomic.AddInt64(&progress.MemoryMappedBytes, fileSize)
		
		err := copyFileWithMemoryMapping(sourcePath, targetPath, sourceInfo, progress, handler)
		if err != nil {
			// Rollback statistics if memory mapping failed
			atomic.AddInt64(&progress.MemoryMappedFiles, -1)
			atomic.AddInt64(&progress.MemoryMappedBytes, -fileSize)
			
			// Fallback to regular copying if memory mapping fails
			fmt.Printf("Memory mapping failed for %s, falling back to regular copy: %v\n", sourcePath, err)
			err = copyFileRegular(sourcePath, targetPath, sourceInfo, progress, handler)
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
	return copyFileWithBuffers(sourcePath, targetPath, sourceInfo, progress, config, handler)
}

// copyFileWithBuffers uses the existing buffer pool method for smaller files
func copyFileWithBuffers(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, config FastCopyConfig, handler *InterruptHandler) error {
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
		// Check for forced exit (double Ctrl+C)
		if handler != nil && handler.IsForceExit() {
			return fmt.Errorf("operation terminated by user (force exit)")
		}
		
		// Check for graceful interruption
		if handler != nil && handler.IsInterrupted() {
			return fmt.Errorf("operation interrupted by user")
		}
		
		bytesRead, readErr := sourceFile.Read(buffer)
		if bytesRead > 0 {
			_, writeErr := targetFile.Write(buffer[:bytesRead])
			if writeErr != nil {
				return fmt.Errorf("failed to write to target: %v", writeErr)
			}
			
			// Force immediate flush in sync mode
			if config.ForceFlush {
				if err := targetFile.Sync(); err != nil {
					fmt.Printf("Warning: Failed to sync target file: %v\n", err)
				}
			}
			
			atomic.AddInt64(&progress.CopiedSize, int64(bytesRead))
			atomic.AddInt64(&progress.ActualCopiedSize, int64(bytesRead))
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
func copyDirectoryOptimized(sourcePath, targetPath string, progress *FastCopyProgress, config FastCopyConfig, handler *InterruptHandler) error {
	// Check for interruption before starting
	if handler.IsCancelled() {
		return fmt.Errorf("operation cancelled by user")
	}
	
	fmt.Printf("Fast scanning for totals...\n")
	
	// Phase 1: Fast scan for totals only (no file collection)
	scanStart := time.Now()
	err := filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		
		// Check for forced exit during scan
		if handler != nil && handler.IsForceExit() {
			return fmt.Errorf("scan terminated by user (force exit)")
		}
		
		// Check for graceful interruption during scan
		if handler != nil && handler.IsInterrupted() {
			return fmt.Errorf("scan interrupted by user")
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
	
	// Initialize actual values (will be updated as files are skipped)
	progress.ActualFiles = progress.TotalFiles
	progress.ActualSize = progress.TotalSize
	
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
				if err := copySmallFileBatch(batch, progress, config, handler); err != nil {
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
				// Check for interruption in worker
				if handler.IsCancelled() {
					return
				}
				if err := copyFileSingle(job.SourcePath, job.TargetPath, job.Info, progress, config, handler); err != nil {
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
			// Check for forced exit during scanning
			if handler != nil && handler.IsForceExit() {
				return fmt.Errorf("copy terminated by user (force exit)")
			}
			
			// Check for graceful interruption during scanning
			if handler != nil && handler.IsInterrupted() {
				return fmt.Errorf("copy interrupted by user")
			}
			
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
	if progress.SkippedFiles > 0 {
		fmt.Printf("Files skipped (already exist): %d (%.2f GB)\n", 
			progress.SkippedFiles, float64(progress.SkippedSize)/(1024*1024*1024))
		fmt.Printf("Files actually copied: %d (%.2f GB)\n", 
			progress.TotalFiles-progress.SkippedFiles, 
			float64(progress.TotalSize-progress.SkippedSize)/(1024*1024*1024))
	}
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
func copySmallFileBatch(batch SmallFileBatch, progress *FastCopyProgress, config FastCopyConfig, handler *InterruptHandler) error {
	// Check for interruption before processing batch
	if handler.IsCancelled() {
		return fmt.Errorf("operation cancelled by user")
	}
	
	atomic.AddInt64(&progress.ActiveFiles, 1)
	defer atomic.AddInt64(&progress.ActiveFiles, -1)
	
	atomic.AddInt64(&progress.SmallFileBatches, 1)
	atomic.AddInt64(&progress.BatchedFiles, int64(len(batch.Jobs)))
	
	// Use buffer pool for 256KB buffer
	const bufferSize = 256 * 1024 // 256KB buffer for small files
	buffer, pool := getBufferFromPool(bufferSize, progress)
	defer pool.Put(&buffer)
	
	for _, job := range batch.Jobs {
		// Check for interruption during batch processing
		if handler.IsCancelled() {
			return fmt.Errorf("operation cancelled by user")
		}
		
		// Add to active files list for progress display
		progress.addActiveFile(job.SourcePath, job.Info.Size())
		
		// Update current file (backward compatibility)
		progress.CurrentFileMux.Lock()
		progress.CurrentFile = job.SourcePath
		progress.CurrentFileMux.Unlock()
		
		// Check if target file already exists
		if _, err := statWithTimeout(job.TargetPath, FileOperationTimeout); err == nil {
			fmt.Printf("Skipping existing file: %s\n", job.TargetPath)
			atomic.AddInt64(&progress.ProcessedFiles, 1)
			atomic.AddInt64(&progress.SkippedFiles, 1)
			atomic.AddInt64(&progress.SkippedSize, job.Info.Size())
			atomic.AddInt64(&progress.CopiedSize, job.Info.Size()) // Add file size to copied total
			// Update actual values
			atomic.AddInt64(&progress.ActualFiles, -1)
			atomic.AddInt64(&progress.ActualSize, -job.Info.Size())
			continue
		}
		
		// Copy single small file
		if err := copySmallFileDirect(job.SourcePath, job.TargetPath, job.Info, progress, buffer, handler); err != nil {
			// Check if it's a device hardware error
			if strings.Contains(err.Error(), "device hardware error") {
				fmt.Printf("Hardware error on %s - skipping\n", job.SourcePath)
			} else {
				fmt.Printf("Warning: Failed to copy small file %s: %v\n", job.SourcePath, err)
			}
		}
		
		// Remove from active files list when processing is complete
		progress.removeActiveFile(job.SourcePath)
	}
	
	return nil
}

// copySmallFileDirect copies a small file directly without goroutine overhead
func copySmallFileDirect(sourcePath, targetPath string, sourceInfo os.FileInfo, progress *FastCopyProgress, buffer []byte, handler *InterruptHandler) error {
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
		// Check for forced exit (double Ctrl+C)
		if handler != nil && handler.IsForceExit() {
			return fmt.Errorf("operation terminated by user (force exit)")
		}
		
		// Check for graceful interruption
		if handler != nil && handler.IsInterrupted() {
			return fmt.Errorf("operation interrupted by user")
		}
		
		bytesRead, readErr := sourceFile.Read(buffer)
		if bytesRead > 0 {
			_, writeErr := targetFile.Write(buffer[:bytesRead])
			if writeErr != nil {
				return fmt.Errorf("failed to write to target: %v", writeErr)
			}
			
			atomic.AddInt64(&progress.CopiedSize, int64(bytesRead))
			atomic.AddInt64(&progress.ActualCopiedSize, int64(bytesRead))
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
	totalFiles := atomic.LoadInt64(&progress.TotalFiles)
	totalSize := atomic.LoadInt64(&progress.TotalSize)
	activeFiles := atomic.LoadInt64(&progress.ActiveFiles)
	
	// Get display file with size using priority system
	displayFilePath, displayFileSize := progress.getDisplayFile()
	
	elapsed := time.Since(progress.StartTime)
	
	// Calculate current speed based on actually copied data (excluding skipped)
	now := time.Now()
	if now.Sub(progress.LastSpeedUpdate) > time.Second {
		actualCopiedSize := atomic.LoadInt64(&progress.ActualCopiedSize)
		if actualCopiedSize > 0 {
			// Use average speed over total elapsed time, but with minimum realistic time
			minElapsed := 5.0 // Minimum 5 seconds to avoid unrealistic speeds
			elapsedForSpeed := math.Max(elapsed.Seconds(), minElapsed)
			currentSpeed := float64(actualCopiedSize) / elapsedForSpeed / (1024 * 1024) // MB/s
			
			// Cap maximum displayed speed to prevent unrealistic values
			maxReasonableSpeed := 2000.0 // 2 GB/s is very fast but realistic for NVMe
			if currentSpeed > maxReasonableSpeed {
				currentSpeed = maxReasonableSpeed
			}
			
			progress.BytesPerSecond = currentSpeed
		}
		progress.LastSpeedUpdate = now
	}
	
	// Calculate ETA based on actual files to copy (excluding skipped)
	eta := "unknown"
	actualSizeToCopy := progress.ActualSize
	if actualSizeToCopy == 0 { // Fallback if ActualSize not set yet
		actualSizeToCopy = totalSize - progress.SkippedSize
	}
	
	actualCopiedSize := atomic.LoadInt64(&progress.ActualCopiedSize)
	if actualCopiedSize > 0 && actualSizeToCopy > 0 {
		remainingSize := actualSizeToCopy - actualCopiedSize
		if remainingSize > 0 && progress.BytesPerSecond > 0 {
			// Use conservative speed estimate (80% of current speed) for ETA
			conservativeSpeed := progress.BytesPerSecond * 0.8
			etaSeconds := float64(remainingSize) / (conservativeSpeed * 1024 * 1024)
			
			// Add minimum ETA to prevent unrealistic short times
			minEtaSeconds := 10.0 // At least 10 seconds for any substantial copy
			if remainingSize > 1024*1024*1024 { // If more than 1GB remaining
				minEtaSeconds = 30.0 // At least 30 seconds
			}
			
			etaSeconds = math.Max(etaSeconds, minEtaSeconds)
			eta = formatETA(time.Duration(etaSeconds * float64(time.Second)))
		}
	}
	
	// Calculate percentages based on actual files to copy
	filePercent := float64(0)
	sizePercent := float64(0)
	
	actualFilesToCopy := progress.ActualFiles
	if actualFilesToCopy == 0 { // Fallback if ActualFiles not set yet
		actualFilesToCopy = totalFiles - progress.SkippedFiles
	}
	
	if actualFilesToCopy > 0 {
		filePercent = float64(processedFiles - progress.SkippedFiles) / float64(actualFilesToCopy) * 100
	}
	
	if actualSizeToCopy > 0 {
		actualCopiedSize := atomic.LoadInt64(&progress.ActualCopiedSize)
		sizePercent = float64(actualCopiedSize) / float64(actualSizeToCopy) * 100
	}
	
	// Show progress with rotating active file display
	// Clear line first to prevent overlapping text and truncate filename
	truncatedFile := truncateFilePath(displayFilePath, 70) // Limit filename to 70 chars
	fileSizeStr := ""
	if displayFileSize > 0 {
		fileSizeStr = fmt.Sprintf(" [%s]", formatFileSize(displayFileSize))
	}
	// Display actual copied vs actual total (excluding skipped files)
	actualCopiedSizeDisplay := atomic.LoadInt64(&progress.ActualCopiedSize)
	fmt.Printf("\r%s\r%d/%d %s%s (%.1f%%) | %.2f/%.2f GB (%.1f%%) | %.2f MB/s | %d | ETA: %s",
		strings.Repeat(" ", 160), // Clear previous line (increased for size info)
		processedFiles-progress.SkippedFiles, actualFilesToCopy, truncatedFile, fileSizeStr, filePercent,
		float64(actualCopiedSizeDisplay)/(1024*1024*1024), float64(actualSizeToCopy)/(1024*1024*1024), sizePercent,
		progress.BytesPerSecond, activeFiles, eta)
}

// FastCopy performs optimized copying with single-pass directory scanning
func FastCopy(sourcePath, targetPath string) error {
	// Use global interrupt handler
	handler := globalInterruptHandler
	if handler == nil {
		handler = NewInterruptHandler()
	}
	
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
		return copyDirectoryOptimized(sourcePath, targetPath, progress, config, handler)
	} else {
		// Single file copy
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		progress.ActualFiles = 1
		progress.ActualSize = sourceInfo.Size()
		return copyFileSingle(sourcePath, targetPath, sourceInfo, progress, config, handler)
	}
}

// FastCopySync performs synchronized copying to match read/write speeds and reduce cache effects
func FastCopySync(sourcePath, targetPath string) error {
	// Use global interrupt handler
	handler := globalInterruptHandler
	if handler == nil {
		handler = NewInterruptHandler()
	}
	
	config := NewSyncCopyConfig() // Use synchronized configuration
	progress := &FastCopyProgress{
		StartTime:       time.Now(),
		LastSpeedUpdate: time.Now(),
	}
	
	fmt.Printf("🔄 Starting synchronized copy mode (single-threaded, reduced caching)...\n")
	fmt.Printf("This mode is designed to match read and write speeds for diagnostic purposes.\n")
	
	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path error: %v", err)
	}
	
	if sourceInfo.IsDir() {
		// Directory copy with synchronization
		return copyDirectoryOptimized(sourcePath, targetPath, progress, config, handler)
	} else {
		// Single file copy with synchronization
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		progress.ActualFiles = 1
		progress.ActualSize = sourceInfo.Size()
		return copyFileSingle(sourcePath, targetPath, sourceInfo, progress, config, handler)
	}
}

// FastCopyMax performs maximum performance copying with aggressive CPU utilization
func FastCopyMax(sourcePath, targetPath string) error {
	// Use global interrupt handler
	handler := globalInterruptHandler
	if handler == nil {
		handler = NewInterruptHandler()
	}
	
	config := NewMaxPerformanceConfig() // Use maximum performance configuration
	progress := &FastCopyProgress{
		StartTime:       time.Now(),
		LastSpeedUpdate: time.Now(),
	}
	
	fmt.Printf("🚀 Starting MAXIMUM PERFORMANCE copy mode (%dx CPU threads, 128MB buffers)...\n", config.MaxConcurrentFiles)
	fmt.Printf("This mode uses aggressive parallelism and maximum system resources.\n")
	
	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path error: %v", err)
	}
	
	if sourceInfo.IsDir() {
		// Directory copy with maximum performance
		return copyDirectoryOptimized(sourcePath, targetPath, progress, config, handler)
	} else {
		// Single file copy with maximum performance
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		progress.ActualFiles = 1
		progress.ActualSize = sourceInfo.Size()
		return copyFileSingle(sourcePath, targetPath, sourceInfo, progress, config, handler)
	}
}

// FastCopyBalanced performs balanced copying optimized for HDD-to-HDD operations
func FastCopyBalanced(sourcePath, targetPath string) error {
	// Use global interrupt handler
	handler := globalInterruptHandler
	if handler == nil {
		handler = NewInterruptHandler()
	}
	
	config := NewBalancedCopyConfig() // Use balanced configuration for HDD
	progress := &FastCopyProgress{
		StartTime:       time.Now(),
		LastSpeedUpdate: time.Now(),
	}
	
	fmt.Printf("⚖️ Starting BALANCED copy mode (%d threads, %dMB buffers)...\n", 
		config.MaxConcurrentFiles, config.MaxBufferSize/(1024*1024))
	fmt.Printf("This mode is optimized for HDD-to-HDD operations with better I/O balance.\n")
	
	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path error: %v", err)
	}
	
	if sourceInfo.IsDir() {
		// Directory copy with balanced performance
		return copyDirectoryOptimized(sourcePath, targetPath, progress, config, handler)
	} else {
		// Single file copy with balanced performance
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		progress.ActualFiles = 1
		progress.ActualSize = sourceInfo.Size()
		return copyFileSingle(sourcePath, targetPath, sourceInfo, progress, config, handler)
	}
}
