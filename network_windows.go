//go:build windows

package main

import (
	"bufio"
	"context"
	"filedo/capacitytest"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// NetworkTester implements FakeCapacityTester for network paths
type NetworkTester struct {
	networkPath string
}

// NewNetworkTester creates a new NetworkTester
func NewNetworkTester(networkPath string) *NetworkTester {
	// Normalize the network path
	normalizedPath := strings.ReplaceAll(networkPath, "/", "\\")
	if !strings.HasPrefix(normalizedPath, "\\\\") {
		normalizedPath = "\\\\" + strings.TrimPrefix(normalizedPath, "\\")
	}
	return &NetworkTester{
		networkPath: normalizedPath,
	}
}

// GetTestInfo returns the test type name and target path for display
func (t *NetworkTester) GetTestInfo() (testType, targetPath string) {
	return "Network", t.networkPath
}

// GetAvailableSpace returns the available space in bytes for testing
func (t *NetworkTester) GetAvailableSpace() (int64, error) {
	// Try to get disk space info using Windows API
	var freeSpace int64 = 1024 * 1024 * 1024 // Default to 1GB if we can't detect

	// Convert to UTF16 for Windows API
	if pathUTF16, err := windows.UTF16PtrFromString(t.networkPath); err == nil {
		var freeBytesAvailableToCaller, totalNumberOfBytes, totalNumberOfFreeBytes uint64
		if err := windows.GetDiskFreeSpaceEx(pathUTF16, &freeBytesAvailableToCaller, &totalNumberOfBytes, &totalNumberOfFreeBytes); err == nil {
			freeSpace = int64(freeBytesAvailableToCaller)
		}
	}

	return freeSpace, nil
}

// CreateTestFile creates a test file with the given size and returns the file path
func (t *NetworkTester) CreateTestFile(fileName string, fileSize int64) (filePath string, err error) {
	filePath = filepath.Join(t.networkPath, fileName)

	// Create file with the specified size
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Write header for identification
	timestamp := time.Now().Format("20060102-150405")
	headerLine := fmt.Sprintf("FILEDO_TEST_%s_%s\n", fileName, timestamp)
	if _, err := file.WriteString(headerLine); err != nil {
		return "", err
	}

	// Calculate remaining size
	remainingSize := fileSize - int64(len(headerLine))

	// Reserve space for footer (same as header)
	footerSpace := int64(len(headerLine))
	dataSize := remainingSize - footerSpace

	// Write data in chunks to avoid excessive memory usage
	const chunkSize = 1024 * 1024 // 1MB chunks
	chunkSizeToUse := int(chunkSize)
	if dataSize < int64(chunkSize) {
		chunkSizeToUse = int(dataSize)
	}
	chunk := make([]byte, chunkSizeToUse)

	// Create pattern content for chunks
	dataPattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(dataPattern)

	// Fill the chunk with repeating pattern
	for i := 0; i < len(chunk); i++ {
		chunk[i] = patternBytes[i%len(patternBytes)]
	}

	// Write content in chunks
	var written int64 = 0
	for written < dataSize {
		writeSize := int64(len(chunk))
		if dataSize-written < writeSize {
			writeSize = dataSize - written
		}
		if _, err := file.Write(chunk[:writeSize]); err != nil {
			return filePath, err
		}
		written += writeSize
	}

	// Write footer (same as header) for verification
	if _, err := file.WriteString(headerLine); err != nil {
		return filePath, err
	}

	return filePath, nil
}

// CreateTestFileContext creates a test file with context for cancellation support
func (t *NetworkTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error) {
	// Check if context is cancelled
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
		// Continue with file creation
	}

	filePath = filepath.Join(t.networkPath, fileName)

	// Use optimal buffer size with context support
	optimalBuffer := capacitytest.CalibrateOptimalBufferSize(t.networkPath)

	// Create file with context-aware writing
	if err := capacitytest.WriteTestFileWithBufferContext(ctx, filePath, fileSize, optimalBuffer); err != nil {
		return "", fmt.Errorf("failed to create test file: %v", err)
	}

	return filePath, nil
}

// VerifyTestFile verifies that a test file contains the expected header
func (t *NetworkTester) VerifyTestFile(filePath string) error {
	return verifyTestFileStartEnd(filePath)
}

// CleanupTestFile removes a test file
func (t *NetworkTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

// GetCleanupCommand returns the command to clean test files manually
func (t *NetworkTester) GetCleanupCommand() string {
	return fmt.Sprintf("network %s clean", t.networkPath)
}

func getNetworkInfo(path string, fullScan bool) (NetworkInfo, error) {
	// Normalize the path
	normalizedPath := strings.ReplaceAll(path, "/", "\\")
	if !strings.HasPrefix(normalizedPath, "\\\\") {
		normalizedPath = "\\\\" + strings.TrimPrefix(normalizedPath, "\\")
	}

	// Test if the network path exists and is accessible
	canRead := testNetworkRead(normalizedPath)
	canWrite := testNetworkWrite(normalizedPath)

	var size uint64
	var fileCount, folderCount int64
	var accessErrors bool

	if canRead {
		if fullScan {
			size, fileCount, folderCount, accessErrors = scanNetworkPath(normalizedPath)
		} else {
			size, fileCount, folderCount, accessErrors = scanNetworkPathRoot(normalizedPath)
		}
	}

	return NetworkInfo{
		Path:         normalizedPath,
		CanRead:      canRead,
		CanWrite:     canWrite,
		Size:         size,
		FileCount:    fileCount,
		FolderCount:  folderCount,
		FullScan:     fullScan,
		AccessErrors: accessErrors,
	}, nil
}

func testNetworkRead(path string) bool {
	// Try to stat the path
	_, err := os.Stat(path)
	if err != nil {
		return false
	}

	// Try to open and read the directory
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	// If we can read at least the directory listing, consider it readable
	_ = entries
	return true
}

func testNetworkWrite(path string) bool {
	// Create a unique temporary file name
	tempFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	tempFilePath := filepath.Join(path, tempFileName)

	// Try to create a temporary file
	file, err := os.Create(tempFilePath)
	if err != nil {
		return false
	}

	// Write a small test content
	_, writeErr := file.WriteString("test")
	file.Close()

	// Clean up the test file
	os.Remove(tempFilePath)

	return writeErr == nil
}

func scanNetworkPathRoot(path string) (uint64, int64, int64, bool) {
	var totalSize uint64
	var fileCount, folderCount int64
	var accessErrors bool

	entries, err := os.ReadDir(path)
	if err != nil {
		return 0, 0, 0, true
	}

	for _, entry := range entries {
		if entry.IsDir() {
			folderCount++
		} else {
			fileCount++
			if info, err := entry.Info(); err == nil {
				totalSize += uint64(info.Size())
			}
		}
	}

	return totalSize, fileCount, folderCount, accessErrors
}

func scanNetworkPath(path string) (uint64, int64, int64, bool) {
	var totalSize uint64
	var fileCount, folderCount int64
	var accessErrors bool

	walkErr := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsPermission(err) || isNetworkError(err) {
				accessErrors = true
				return nil // Continue scanning
			}
			return err
		}

		if d.IsDir() {
			if p != path {
				folderCount++
			}
		} else {
			fileCount++
			if info, err := d.Info(); err == nil {
				totalSize += uint64(info.Size())
			}
		}
		return nil
	})

	if walkErr != nil {
		accessErrors = true
	}

	return totalSize, fileCount, folderCount, accessErrors
}

func isNetworkError(err error) bool {
	// Check for common network-related errors
	errStr := err.Error()
	networkErrors := []string{
		"network",
		"unreachable",
		"timeout",
		"connection",
		"remote",
		"share",
	}

	for _, netErr := range networkErrors {
		if strings.Contains(strings.ToLower(errStr), netErr) {
			return true
		}
	}

	return false
}

func runNetworkSpeedTest(networkPath, sizeMBStr string, noDelete, shortFormat bool, logger *HistoryLogger) error {
	// Setup history logging
	if logger != nil {
		logger.SetCommand("network", networkPath, "speed")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("noDelete", noDelete)
		logger.SetParameter("shortFormat", shortFormat)
	}

	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 1 // Default to 1 MB if parsing fails
		//return fmt.Errorf("invalid size '%s': %w", sizeMBStr, err)
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB
		sizeMB = 1 // Default to 1 MB if out of range
		//return fmt.Errorf("size must be between 1 and 10240 MB")
	}

	if !shortFormat {
		fmt.Printf("Network Speed Test\n")
		fmt.Printf("Target: %s\n", networkPath)
		fmt.Printf("Test file size: %d MB\n\n", sizeMB)

		// Step 1: Check if network address is reachable and writable
		fmt.Printf("Step 1: Checking network accessibility...\n")
	}

	canRead := testNetworkRead(networkPath)
	canWrite := testNetworkWrite(networkPath)

	if !canRead {
		err := fmt.Errorf("network path is not readable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	if !canWrite {
		err := fmt.Errorf("network path is not writable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	if !shortFormat {
		fmt.Printf("✓ Network path is readable and writable\n\n")

		// Step 2: Create random file
		fmt.Printf("Step 2: Creating test file (%d MB)...\n", sizeMB)
	}

	localFileName := fmt.Sprintf("speedtest_%d_%d.txt", sizeMB, time.Now().Unix())

	startCreate := time.Now()
	err = createRandomFile(localFileName, sizeMB, !shortFormat)
	if err != nil {
		err = fmt.Errorf("failed to create test file: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	createDuration := time.Since(startCreate)

	// Step 3: Upload Speed Test - Copy file to network location
	networkFileName := filepath.Join(networkPath, localFileName)

	if !shortFormat {
		fmt.Printf("✓ Test file created in %s\n\n", formatDuration(createDuration))

		// Step 3: Upload Speed Test - Copy file to network location
		fmt.Printf("Step 3: Upload Speed Test - Copying file to network location...\n")
		fmt.Printf("Source: %s\n", localFileName)
		fmt.Printf("Target: %s\n", networkFileName)
	}

	startUpload := time.Now()
	bytesUploaded, err := copyFileOptimized(localFileName, networkFileName)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFileName)
		err = fmt.Errorf("failed to copy file to network: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	uploadDuration := time.Since(startUpload)

	// Calculate upload speed
	uploadSpeedMBps := float64(bytesUploaded) / (1024 * 1024) / uploadDuration.Seconds()
	uploadSpeedMbps := uploadSpeedMBps * 8 // Convert to megabits per second

	if !shortFormat {
		fmt.Printf("\n✓ File uploaded successfully\n")
		fmt.Printf("Upload completed in %s\n", formatDuration(uploadDuration))
		fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)
	}

	// Step 4: Download Speed Test - Copy file back from network location
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())

	if !shortFormat {
		// Step 4: Download Speed Test - Copy file back from network location
		fmt.Printf("Step 4: Download Speed Test - Copying file from network location...\n")
		fmt.Printf("Source: %s\n", networkFileName)
		fmt.Printf("Target: %s\n", downloadFileName)
	}

	startDownload := time.Now()
	bytesDownloaded, err := copyFileOptimized(networkFileName, downloadFileName)
	if err != nil {
		// Clean up files before returning error
		os.Remove(localFileName)
		os.Remove(networkFileName)
		err = fmt.Errorf("failed to copy file from network: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	downloadDuration := time.Since(startDownload)

	// Calculate download speed
	downloadSpeedMBps := float64(bytesDownloaded) / (1024 * 1024) / downloadDuration.Seconds()
	downloadSpeedMbps := downloadSpeedMBps * 8 // Convert to megabits per second

	if shortFormat {
		// In short format, only show the final upload/download results
		fmt.Printf("Upload completed in   %s, Speed: %6.1f MB/s (%6.1f Mbps)\n",
			formatDuration(uploadDuration), uploadSpeedMBps, uploadSpeedMbps)
		fmt.Printf("Download completed in %s, Speed: %6.1f MB/s (%6.1f Mbps)\n",
			formatDuration(downloadDuration), downloadSpeedMBps, downloadSpeedMbps)
	} else {
		fmt.Printf("\n✓ File downloaded successfully\n")
		fmt.Printf("Download completed in %s\n", formatDuration(downloadDuration))
		fmt.Printf("Download Speed: %.2f MB/s (%.2f Mbps)\n\n", downloadSpeedMBps, downloadSpeedMbps)

		// Step 5: Clean up files
		fmt.Printf("Step 5: Cleaning up test files...\n")
	}

	// Clean up files (always done, but only show progress if not short format)
	// Remove original local file
	if err := os.Remove(localFileName); err != nil && !shortFormat {
		fmt.Printf("⚠ Warning: Could not remove original local file: %v\n", err)
	} else if !shortFormat {
		fmt.Printf("✓ Original local test file removed\n")
	}

	// Remove downloaded file
	if err := os.Remove(downloadFileName); err != nil && !shortFormat {
		fmt.Printf("⚠ Warning: Could not remove downloaded file: %v\n", err)
	} else if !shortFormat {
		fmt.Printf("✓ Downloaded test file removed\n")
	}

	// Remove network file (unless noDelete flag is set)
	if noDelete {
		if !shortFormat {
			fmt.Printf("✓ Network test file kept: %s\n", networkFileName)
		}
	} else {
		if err := os.Remove(networkFileName); err != nil && !shortFormat {
			fmt.Printf("⚠ Warning: Could not remove network file: %v\n", err)
		} else if !shortFormat {
			fmt.Printf("✓ Network test file removed\n")
		}
	}

	if !shortFormat {
		fmt.Printf("\nSpeed Test Summary:\n")
		fmt.Printf("File size: %d MB\n", sizeMB)
		fmt.Printf("Upload time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(uploadDuration), uploadSpeedMBps, uploadSpeedMbps)
		fmt.Printf("Download time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(downloadDuration), downloadSpeedMBps, downloadSpeedMbps)
	}

	// Log results
	if logger != nil {
		logger.SetResult("fileSizeMB", sizeMB)
		logger.SetResult("uploadSpeedMBps", uploadSpeedMBps)
		logger.SetResult("downloadSpeedMBps", downloadSpeedMBps)
		logger.SetResult("uploadTimeSec", uploadDuration.Seconds())
		logger.SetResult("downloadTimeSec", downloadDuration.Seconds())
		logger.SetSuccess()
	}

	return nil
}

func runNetworkFill(networkPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	// Setup history logging
	if logger != nil {
		logger.SetCommand("network", networkPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
	}

	// Setup interrupt handler
	handler := NewInterruptHandler()
	templateFilePath := ""

	// Add cleanup for template file
	handler.AddCleanup(func() {
		if templateFilePath != "" {
			os.Remove(templateFilePath)
			fmt.Printf("✓ Template file cleaned up\n")
		}
	})

	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 100 // Default to 100 MB if parsing fails
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB per file
		sizeMB = 100 // Default to 100 MB if out of range
	}

	fmt.Printf("Network Fill Operation\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Test if the network path exists and is accessible
	canRead := testNetworkRead(networkPath)
	canWrite := testNetworkWrite(networkPath)

	if !canRead {
		err := fmt.Errorf("network path is not readable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	if !canWrite {
		err := fmt.Errorf("network path is not writable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	fmt.Printf("✓ Network path is accessible and writable\n")

	// For network paths, we'll use a conservative approach and try to estimate available space
	// Since we can't reliably get disk space info for network paths, we'll use a different strategy
	// We'll keep creating files until we get an error (disk full)

	// Create template file first
	currentDir, err := os.Getwd()
	if err != nil {
		err = fmt.Errorf("failed to get current directory: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	templateFileName := fmt.Sprintf("fill_template_%d_%d.txt", sizeMB, time.Now().Unix())
	templateFilePath = filepath.Join(currentDir, templateFileName)

	fmt.Printf("Creating template file (%d MB)...\n", sizeMB)
	startTemplate := time.Now()
	err = createRandomFile(templateFilePath, sizeMB, false) // No progress for template
	if err != nil {
		err = fmt.Errorf("failed to create template file: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	templateDuration := time.Since(startTemplate)
	fmt.Printf("✓ Template file created in %s\n\n", formatDuration(templateDuration))

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Start filling
	fmt.Printf("Starting fill operation...\n")
	fmt.Printf("(Note: For network paths, will fill until disk full)\n\n")

	// For network, we estimate a high number of files since we don't know the target capacity
	fileSizeBytes := int64(sizeMB) * 1024 * 1024
	estimatedMaxFiles := int64(10000) // Conservative estimate
	progress := NewProgressTrackerWithInterval(estimatedMaxFiles, estimatedMaxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)
	for i := int64(1); i <= 99999; i++ { // Reasonable upper limit
		// Check for interruption
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}

		// Generate file name: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(networkPath, fileName)

		// Copy template file to target
		bytesCopied, err := copyFileOptimized(templateFilePath, targetFilePath)
		if err != nil {
			fmt.Printf("\n⚠ Stopping: Failed to create file %d: %v\n", i, err)
			break
		}

		filesCreated++
		totalBytesWritten += bytesCopied

		// Update progress - for network we don't show percentage since we don't know the total
		if i%10 == 0 {
			progress.Update(filesCreated, totalBytesWritten)
			speedMBps := progress.GetCurrentSpeed()
			gbWritten := float64(totalBytesWritten) / (1024 * 1024 * 1024)
			progress.PrintProgressCustom("Fill %s: %d files (%6.1f MB/s) - %6.2f GB\r",
				networkPath, filesCreated, speedMBps, gbWritten)
		}
	}

	// Clean up template file
	os.Remove(templateFilePath)

	// Final summary using progress tracker
	progress.currentItem = filesCreated
	progress.currentBytes = totalBytesWritten
	progress.Finish("Fill Operation")

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files...\n")

		// Find all FILL_*.tmp files in the network path
		pattern := filepath.Join(networkPath, "FILL_*.tmp")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Printf("⚠ Warning: Failed to search for files to delete: %v\n", err)
		} else if len(matches) > 0 {
			deletedCount := 0
			deletedSize := int64(0)

			for i, filePath := range matches {
				info, err := os.Stat(filePath)
				if err == nil {
					fileSize := info.Size()

					err = os.Remove(filePath)
					if err != nil {
						fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
					} else {
						deletedCount++
						deletedSize += fileSize

						// Show progress every 100 files
						if (i+1)%100 == 0 || i == len(matches)-1 {
							fmt.Printf("Deleted %d/%d files - %.2f GB freed\r", deletedCount, len(matches), float64(deletedSize)/(1024*1024*1024))
						}
					}
				}
			}

			fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", deletedCount, float64(deletedSize)/(1024*1024*1024))
		}
	}

	// Log results
	if logger != nil {
		logger.SetResult("filesCreated", filesCreated)
		logger.SetResult("totalGBWritten", float64(totalBytesWritten)/(1024*1024*1024))
		logger.SetResult("autoDeleteUsed", autoDelete)
		logger.SetSuccess()
	}

	return nil
}

func runNetworkFillClean(networkPath string, logger *HistoryLogger) error {
	// Setup history logging
	if logger != nil {
		logger.SetCommand("network", networkPath, "clean")
	}

	fmt.Printf("Network Clean Operation\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)...\n\n")

	// Test if the network path exists and is accessible
	canRead := testNetworkRead(networkPath)
	canWrite := testNetworkWrite(networkPath)

	if !canRead {
		err := fmt.Errorf("network path is not readable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	if !canWrite {
		err := fmt.Errorf("network path is not writable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Find all FILL_*.tmp files
	fillPattern := filepath.Join(networkPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		err = fmt.Errorf("failed to search for FILL files: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Find all speedtest_*.txt files
	speedtestPattern := filepath.Join(networkPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		err = fmt.Errorf("failed to search for speedtest files: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Combine all matches
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", networkPath)
		fmt.Printf("Searched for: FILL_*.tmp and speedtest_*.txt\n")
		return nil
	}

	fmt.Printf("Found %d test files:\n", len(allMatches))
	fmt.Printf("  FILL files: %d\n", len(fillMatches))
	fmt.Printf("  Speedtest files: %d\n", len(speedtestMatches))

	// Calculate total size before deletion
	var totalSize int64
	for _, filePath := range allMatches {
		if info, err := os.Stat(filePath); err == nil {
			totalSize += info.Size()
		}
	}

	fmt.Printf("Total size to delete: %.2f GB\n", float64(totalSize)/(1024*1024*1024))
	fmt.Printf("Deleting files...\n\n")

	// Delete files using worker pool for parallel deletion
	var deletedCount int64
	var deletedSize int64
	deletionWorkers := 24 // Use 24 workers for parallel deletion
	deletionJobs := make(chan string, len(allMatches))
	var wg sync.WaitGroup

	// Start deletion workers
	for w := 0; w < deletionWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range deletionJobs {
				info, err := os.Stat(filePath)
				if err == nil {
					fileSize := info.Size()
					err = os.Remove(filePath)
					if err != nil {
						fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
					} else {
						atomic.AddInt64(&deletedCount, 1)
						atomic.AddInt64(&deletedSize, fileSize)
					}
				}
			}
		}()
	}

	// Queue all files for deletion
	for _, filePath := range allMatches {
		deletionJobs <- filePath
	}
	close(deletionJobs)

	// Update progress periodically while waiting for completion
	updateTicker := time.NewTicker(100 * time.Millisecond)
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	// Show progress updates
	totalFiles := int64(len(allMatches))
	for {
		select {
		case <-updateTicker.C:
			currentCount := atomic.LoadInt64(&deletedCount)
			currentSize := atomic.LoadInt64(&deletedSize)
			fmt.Printf("Deleted %d/%d files - %.2f GB freed\r",
				currentCount, totalFiles, float64(currentSize)/(1024*1024*1024))
		case <-done:
			updateTicker.Stop()
			fmt.Printf("Deleted %d/%d files - %.2f GB freed\r",
				atomic.LoadInt64(&deletedCount), totalFiles,
				float64(atomic.LoadInt64(&deletedSize))/(1024*1024*1024))
			goto DeletionComplete
		}
	}

DeletionComplete:
	fmt.Printf("\n\nClean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deletedCount, len(allMatches))
	fmt.Printf("Space freed: %.2f GB\n", float64(deletedSize)/(1024*1024*1024))

	if deletedCount < int64(len(allMatches)) {
		fmt.Printf("Warning: %d files could not be deleted\n", int64(len(allMatches))-deletedCount)
	}

	// Log results
	if logger != nil {
		logger.SetResult("filesDeleted", deletedCount)
		logger.SetResult("totalGBFreed", float64(deletedSize)/(1024*1024*1024))
		logger.SetSuccess()
	}

	return nil
}

func runNetworkTest(networkPath string, autoDelete bool, logger *HistoryLogger) error {
	tester := NewNetworkTester(networkPath)
	_, err := runGenericFakeCapacityTest(tester, autoDelete, logger)
	return err
}

func runNetworkTestOld(networkPath string, autoDelete bool, logger *HistoryLogger) error {
	// Setup history logging
	if logger != nil {
		logger.SetCommand("network", networkPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
	}

	// Normalize the network path
	normalizedPath := strings.ReplaceAll(networkPath, "/", "\\")
	if !strings.HasPrefix(normalizedPath, "\\\\") {
		normalizedPath = "\\\\" + strings.TrimPrefix(normalizedPath, "\\")
	}

	// Check if the network path exists and is accessible
	if _, err := os.Stat(normalizedPath); err != nil {
		err = fmt.Errorf("network path not accessible: %v", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Try to get disk space info, but don't fail if it doesn't work
	var freeSpace int64 = 1024 * 1024 * 1024 // Default to 1GB if we can't detect

	// Convert to UTF16 for Windows API
	if pathUTF16, err := windows.UTF16PtrFromString(normalizedPath); err == nil {
		var freeBytesAvailableToCaller, totalNumberOfBytes, totalNumberOfFreeBytes uint64
		if err := windows.GetDiskFreeSpaceEx(pathUTF16, &freeBytesAvailableToCaller, &totalNumberOfBytes, &totalNumberOfFreeBytes); err == nil {
			freeSpace = int64(freeBytesAvailableToCaller)
		}
	}

	// Check if we have at least 100MB free space
	if freeSpace < 100*1024*1024 {
		err := fmt.Errorf("insufficient free space. At least 100MB required, but only %d MB available", freeSpace/(1024*1024))
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Calculate file size as 1% of free space
	fileSize := freeSpace / 100
	fmt.Printf("Starting fake capacity test for network path: %s\n", normalizedPath)
	fmt.Printf("Estimated free space: %d MB\n", freeSpace/(1024*1024))
	fmt.Printf("Test file size: %d MB (1%% of free space)\n", fileSize/(1024*1024))
	fmt.Printf("Will create 100 test files...\n\n")

	var createdFiles []string
	var speeds []float64
	baselineSpeed := 0.0
	baselineSet := false
	const maxFiles = 100
	const baselineFileCount = 3

	// Generate test content
	testContent := strings.Repeat("FILL_TEST_DATA_", int(fileSize)/15)
	if len(testContent) < int(fileSize) {
		testContent += strings.Repeat("X", int(fileSize)-len(testContent))
	}
	testContent = testContent[:fileSize]

	// Add header line to identify the file
	headerLine := "FILL_TEST_HEADER_LINE\n"
	testContent = headerLine + testContent[len(headerLine):]

	// Create files and monitor speed
	progress := NewProgressTrackerWithInterval(int64(maxFiles), int64(maxFiles)*fileSize, 2*time.Second)

	for i := 1; i <= maxFiles; i++ {
		fileName := fmt.Sprintf("FILL_%03d_%s.tmp", i, time.Now().Format("02150405"))
		filePath := filepath.Join(normalizedPath, fileName)

		start := time.Now()

		// Write file
		file, err := os.Create(filePath)
		if err != nil {
			// Clean up on error
			cleanupNetworkFiles(createdFiles)
			err = fmt.Errorf("failed to create file %s: %v", fileName, err)
			if logger != nil {
				logger.SetError(err)
			}
			return err
		}

		_, err = file.WriteString(testContent)
		file.Close()

		if err != nil {
			// Clean up on error
			cleanupNetworkFiles(createdFiles)
			err = fmt.Errorf("failed to write file %s: %v", fileName, err)
			if logger != nil {
				logger.SetError(err)
			}
			return err
		}

		// Verify file immediately after creation
		file, err = os.Open(filePath)
		if err != nil {
			// Clean up on verification error
			cleanupNetworkFiles(createdFiles)
			fmt.Printf("\n❌ TEST FAILED: Could not open file %s for immediate verification: %v\n", fileName, err)
			fmt.Printf("This indicates data corruption, network issues, or device failure.\n")
			fmt.Printf("File: %s\n", filePath)
			fmt.Printf("Error details: %v\n", err)
			fmt.Printf("All %d created files have been cleaned up.\n", len(createdFiles))
			err = fmt.Errorf("test failed during immediate file verification - could not open file")
			if logger != nil {
				logger.SetError(err)
			}
			return err
		}

		scanner := bufio.NewScanner(file)
		var firstLine string
		if scanner.Scan() {
			firstLine = scanner.Text()
		}
		file.Close()

		if firstLine != "FILL_TEST_HEADER_LINE" {
			// Clean up on verification error
			cleanupNetworkFiles(createdFiles)
			fmt.Printf("\n❌ TEST FAILED: File %s is corrupted immediately after creation\n", fileName)
			fmt.Printf("Expected header: 'FILL_TEST_HEADER_LINE'\n")
			fmt.Printf("Found header: '%s'\n", firstLine)
			fmt.Printf("This indicates data corruption, fake capacity, or network issues.\n")
			fmt.Printf("File: %s\n", filePath)
			fmt.Printf("All %d created files have been cleaned up.\n", len(createdFiles))
			err = fmt.Errorf("test failed during immediate file verification - data corruption detected")
			if logger != nil {
				logger.SetError(err)
			}
			return err
		}

		duration := time.Since(start)
		speed := float64(fileSize) / duration.Seconds() / (1024 * 1024) // MB/s
		speeds = append(speeds, speed)
		createdFiles = append(createdFiles, filePath)

		// Update progress
		progress.Update(int64(i), int64(i)*fileSize)
		progress.PrintProgress("Test")

		// Set baseline speed from first 3 files
		if i <= baselineFileCount {
			if i == baselineFileCount {
				// Calculate average of first 3 files as baseline
				sum := 0.0
				for _, s := range speeds[:baselineFileCount] {
					sum += s
				}
				baselineSpeed = sum / float64(baselineFileCount)
				baselineSet = true
				fmt.Printf("Baseline speed established: %.2f MB/s\n", baselineSpeed)
			}
		} else if baselineSet {
			// Check for abnormal speed after baseline is set
			if speed < baselineSpeed*0.1 { // Less than 10% of baseline
				fmt.Printf("\n❌ TEST FAILED: Speed dropped to %.2f MB/s (less than 10%% of baseline %.2f MB/s)\n", speed, baselineSpeed)
				fmt.Printf("This indicates potential network issues or fake capacity.\n")
				fmt.Printf("Keeping %d test files for analysis.\n", len(createdFiles))
				err := fmt.Errorf("test failed due to abnormally slow write speed")
				if logger != nil {
					logger.SetError(err)
				}
				return err
			}
			if speed > baselineSpeed*10 { // More than 10x baseline
				fmt.Printf("\n❌ TEST FAILED: Speed jumped to %.2f MB/s (more than 1000%% of baseline %.2f MB/s)\n", speed, baselineSpeed)
				fmt.Printf("This indicates potential fake writing or caching issues.\n")
				fmt.Printf("Keeping %d test files for analysis.\n", len(createdFiles))
				err := fmt.Errorf("test failed due to abnormally fast write speed")
				if logger != nil {
					logger.SetError(err)
				}
				return err
			}
		}
	}

	fmt.Printf("\n✅ Write phase completed successfully!\n")
	fmt.Printf("Now verifying file integrity...\n")

	// Verify files in creation order
	for i, filePath := range createdFiles {
		fileName := filepath.Base(filePath)

		// Read and verify file
		file, err := os.Open(filePath)
		if err != nil {
			fileNum := fmt.Sprintf("file %d/%d", i+1, len(createdFiles))
			fmt.Printf("Verifying %s - ❌ FAILED\n", fileNum)
			fmt.Printf("\n❌ TEST FAILED: Could not open file %s for verification: %v\n", fileName, err)
			fmt.Printf("This indicates data corruption or network issues.\n")
			fmt.Printf("Keeping %d test files for analysis.\n", len(createdFiles))
			err = fmt.Errorf("test failed during verification - file corruption detected")
			if logger != nil {
				logger.SetError(err)
			}
			return err
		}

		scanner := bufio.NewScanner(file)
		var firstLine string
		if scanner.Scan() {
			firstLine = scanner.Text()
		}
		file.Close()

		if firstLine != "FILL_TEST_HEADER_LINE" {
			fileNum := fmt.Sprintf("file %d/%d", i+1, len(createdFiles))
			fmt.Printf("Verifying %s - ❌ FAILED\n", fileNum)
			fmt.Printf("\n❌ TEST FAILED: File %s is corrupted (expected header not found)\n", fileName)
			fmt.Printf("Expected: 'FILL_TEST_HEADER_LINE'\n")
			fmt.Printf("Found: '%s'\n", firstLine)
			fmt.Printf("This indicates data corruption or fake capacity.\n")
			fmt.Printf("Keeping %d test files for analysis.\n", len(createdFiles))
			err := fmt.Errorf("test failed during verification - data corruption detected")
			if logger != nil {
				logger.SetError(err)
			}
			return err
		}
	}

	fmt.Printf("Verified %d files - ✅ OK\n", len(createdFiles))

	// Calculate statistics
	var minSpeed, maxSpeed, avgSpeed float64
	if len(speeds) > 0 {
		minSpeed = speeds[0]
		maxSpeed = speeds[0]
		sum := 0.0
		for _, speed := range speeds {
			if speed < minSpeed {
				minSpeed = speed
			}
			if speed > maxSpeed {
				maxSpeed = speed
			}
			sum += speed
		}
		avgSpeed = sum / float64(len(speeds))
	}

	fmt.Printf("\n✅ TEST PASSED SUCCESSFULLY!\n")
	fmt.Printf("All 100 files were written and verified successfully.\n")
	fmt.Printf("\n📊 Speed Statistics:\n")
	fmt.Printf("  Baseline speed (first 3 files): %.2f MB/s\n", baselineSpeed)
	fmt.Printf("  Average speed: %.2f MB/s\n", avgSpeed)
	fmt.Printf("  Minimum speed: %.2f MB/s\n", minSpeed)
	fmt.Printf("  Maximum speed: %.2f MB/s\n", maxSpeed)
	fmt.Printf("  Total data written: %d MB\n", (fileSize*maxFiles)/(1024*1024))

	// Delete files if requested and test passed
	if autoDelete {
		fmt.Printf("\n🗑️  Auto-delete enabled, cleaning up test files...\n")
		deletedCount := 0
		for _, filePath := range createdFiles {
			if err := os.Remove(filePath); err != nil {
				fmt.Printf("Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
			} else {
				deletedCount++
			}
		}
		fmt.Printf("Successfully deleted %d/%d test files.\n", deletedCount, len(createdFiles))
	} else {
		fmt.Printf("\n📁 Test files kept for manual inspection:\n")
		fmt.Printf("   Location: %s\n", normalizedPath)
		fmt.Printf("   Files: FILL_001_*.tmp to FILL_100_*.tmp\n")
		fmt.Printf("   Use 'filedo network %s fill clean' to remove them later.\n", normalizedPath)
	}

	// Log results
	if logger != nil {
		logger.SetResult("testPassed", true)
		logger.SetResult("averageSpeedMBps", avgSpeed)
		logger.SetResult("minSpeedMBps", minSpeed)
		logger.SetResult("maxSpeedMBps", maxSpeed)
		logger.SetResult("baselineSpeedMBps", baselineSpeed)
		logger.SetResult("totalDataMB", (fileSize*maxFiles)/(1024*1024))
		logger.SetResult("filesDeleted", autoDelete)
		logger.SetSuccess()
	}

	return nil
}

func cleanupNetworkFiles(files []string) {
	for _, filePath := range files {
		os.Remove(filePath) // Ignore errors during cleanup
	}
}
