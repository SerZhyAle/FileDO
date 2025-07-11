//go:build windows

package main

import (
	"context"
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

func getFolderInfo(path string, fullScan bool) (FolderInfo, error) {
	stat, err := os.Stat(path)
	if err != nil {
		return FolderInfo{}, err
	}
	if !stat.IsDir() {
		return FolderInfo{}, fmt.Errorf("path is not a directory: %s", path)
	}

	var size uint64
	var fileCount, folderCount int64
	var accessErrors bool

	if fullScan {
		err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsPermission(err) || strings.Contains(err.Error(), "being used by another process") || strings.Contains(err.Error(), "cannot access the file") {
					accessErrors = true
					return nil
				}
				accessErrors = true
				return nil
			}
			if d.IsDir() {
				if p != path {
					folderCount++
				}
			} else {
				fileCount++
				info, err := d.Info()
				if err != nil {
					if os.IsPermission(err) || strings.Contains(err.Error(), "being used by another process") || strings.Contains(err.Error(), "cannot access the file") {
						accessErrors = true
						return nil
					}
					accessErrors = true
					return nil
				}
				size += uint64(info.Size())
			}
			return nil
		})
	} else {
		entries, err := os.ReadDir(path)
		if err != nil {
			return FolderInfo{}, fmt.Errorf("failed to read directory '%s': %w", path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				folderCount++
			} else {
				fileCount++
				info, err := entry.Info()
				if err == nil {
					size += uint64(info.Size())
				}
			}
		}
	}

	if err != nil && !accessErrors {
		return FolderInfo{}, fmt.Errorf("failed to walk directory '%s': %w", path, err)
	}

	creationTime := getCreationTime(stat)

	// Test read access
	canRead := false
	_, readErr := os.ReadDir(path)
	if readErr == nil {
		canRead = true
	}

	// Test write access
	canWrite := false
	testFileName := fmt.Sprintf("__filedo_access_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(path, testFileName)
	if testFile, writeErr := os.Create(testFilePath); writeErr == nil {
		testFile.Close()
		os.Remove(testFilePath) // Clean up test file
		canWrite = true
	}

	return FolderInfo{
		Path: path, Size: size, FileCount: fileCount, FolderCount: folderCount, ModTime: stat.ModTime(),
		CreationTime: creationTime, Mode: stat.Mode(), FullScan: fullScan, AccessErrors: accessErrors,
		CanRead: canRead, CanWrite: canWrite,
	}, nil
}

func runFolderSpeedTest(folderPath, sizeMBStr string, noDelete, shortFormat bool) error {
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
		fmt.Printf("Folder Speed Test\n")
		fmt.Printf("Target: %s\n", folderPath)
		fmt.Printf("Test file size: %d MB\n\n", sizeMB)

		// Step 1: Check if folder is accessible and writable
		fmt.Printf("Step 1: Checking folder accessibility..\n")
	}

	// Check if folder exists and is accessible
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path is not accessible: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Test write access by creating a temporary file
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(folderPath, testFileName)

	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("folder path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	if !shortFormat {
		fmt.Printf("✓ Folder is accessible and writable\n\n")

		// Step 2: Create test file in current directory
		fmt.Printf("Step 2: Creating test file (%d MB)..\n", sizeMB)
	}

	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	localFileName := fmt.Sprintf("speedtest_%d_%d.txt", sizeMB, time.Now().Unix())
	localFilePath := filepath.Join(currentDir, localFileName)

	startCreate := time.Now()
	err = createRandomFile(localFilePath, sizeMB, !shortFormat)
	if err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}
	createDuration := time.Since(startCreate)

	if !shortFormat {
		fmt.Printf("✓ Test file created in %s\n\n", formatDuration(createDuration))

		// Step 3: Upload Speed Test - Copy file to folder
		folderFileName := filepath.Join(folderPath, localFileName)
		fmt.Printf("Step 3: Upload Speed Test - Copying file to folder..\n")
		fmt.Printf("Source: %s\n", localFilePath)
		fmt.Printf("Target: %s\n", folderFileName)
	}

	// Step 3: Upload Speed Test - Copy file to folder
	folderFileName := filepath.Join(folderPath, localFileName)

	startUpload := time.Now()
	bytesUploaded, err := copyFileOptimized(localFilePath, folderFileName)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFilePath)
		return fmt.Errorf("failed to copy file to folder: %w", err)
	}
	uploadDuration := time.Since(startUpload)

	// Calculate upload speed
	uploadSpeedMBps := float64(bytesUploaded) / (1024 * 1024) / uploadDuration.Seconds()
	uploadSpeedMbps := uploadSpeedMBps * 8 // Convert to megabits per second

	if !shortFormat {
		fmt.Printf("\n✓ File uploaded successfully\n")
		fmt.Printf("Upload completed in %s\n", formatDuration(uploadDuration))
		fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)

		// Step 4: Download Speed Test - Copy file back from folder
		downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
		downloadFilePath := filepath.Join(currentDir, downloadFileName)
		fmt.Printf("Step 4: Download Speed Test - Copying file from folder..\n")
		fmt.Printf("Source: %s\n", folderFileName)
		fmt.Printf("Target: %s\n", downloadFilePath)
	}

	// Step 4: Download Speed Test - Copy file back from folder
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
	downloadFilePath := filepath.Join(currentDir, downloadFileName)

	startDownload := time.Now()
	bytesDownloaded, err := copyFileOptimized(folderFileName, downloadFilePath)
	if err != nil {
		// Clean up files before returning error
		os.Remove(localFilePath)
		os.Remove(folderFileName)
		return fmt.Errorf("failed to copy file from folder: %w", err)
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
		fmt.Printf("Step 5: Cleaning up test files..\n")
	}

	// Clean up files (always done, but only show progress if not short format)
	// Remove original local file
	if err := os.Remove(localFilePath); err != nil && !shortFormat {
		fmt.Printf("⚠ Warning: Could not remove original local file: %v\n", err)
	} else if !shortFormat {
		fmt.Printf("✓ Original local test file removed\n")
	}

	// Remove downloaded file
	if err := os.Remove(downloadFilePath); err != nil && !shortFormat {
		fmt.Printf("⚠ Warning: Could not remove downloaded file: %v\n", err)
	} else if !shortFormat {
		fmt.Printf("✓ Downloaded test file removed\n")
	}

	// Remove folder file (unless noDelete flag is set)
	if noDelete {
		if !shortFormat {
			fmt.Printf("✓ Folder test file kept: %s\n", folderFileName)
		}
	} else {
		if err := os.Remove(folderFileName); err != nil && !shortFormat {
			fmt.Printf("⚠ Warning: Could not remove folder file: %v\n", err)
		} else if !shortFormat {
			fmt.Printf("✓ Folder test file removed\n")
		}
	}

	if !shortFormat {
		fmt.Printf("\nSpeed Test Summary:\n")
		fmt.Printf("File size: %d MB\n", sizeMB)
		fmt.Printf("Upload time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(uploadDuration), uploadSpeedMBps, uploadSpeedMbps)
		fmt.Printf("Download time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(downloadDuration), downloadSpeedMBps, downloadSpeedMbps)
	}

	return nil
}

func runFolderFill(folderPath, sizeMBStr string, autoDelete bool) error {
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

	fmt.Printf("Folder Fill Operation\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Check if folder exists and is accessible
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path is not accessible: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Test write access
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(folderPath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("folder path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	// Get available space on the filesystem containing this folder
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Use Windows API to get disk space for the drive containing the folder
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Get the root of the drive
	volumePathName := make([]uint16, windows.MAX_PATH)
	err = windows.GetVolumePathName(windows.StringToUTF16Ptr(absPath), &volumePathName[0], windows.MAX_PATH)
	if err != nil {
		return fmt.Errorf("failed to get volume path name: %w", err)
	}
	rootPath := windows.UTF16ToString(volumePathName)

	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(rootPath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return fmt.Errorf("failed to get disk space information: %w", err)
	}

	fileSizeBytes := int64(sizeMB) * 1024 * 1024
	maxFiles := int64(freeBytesAvailable) / fileSizeBytes

	// Reserve some space (100MB or 5% of total, whichever is smaller)
	reserveBytes := int64(100 * 1024 * 1024) // 100MB
	if fivePercent := int64(totalBytes) / 20; fivePercent < reserveBytes {
		reserveBytes = fivePercent
	}

	// Adjust max files to account for reserved space
	if reserveBytes > 0 {
		maxFiles = (int64(freeBytesAvailable) - reserveBytes) / fileSizeBytes
	}

	if maxFiles <= 0 {
		return fmt.Errorf("insufficient space to create even one file of %d MB", sizeMB)
	}

	fmt.Printf("Available space: %.2f GB\n", float64(freeBytesAvailable)/(1024*1024*1024))
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Maximum files to create: %d\n", maxFiles)
	fmt.Printf("Total space to fill: %.2f GB\n\n", float64(maxFiles*fileSizeBytes)/(1024*1024*1024))

	// Create template file first
	templateFileName := fmt.Sprintf("fill_template_%d_%d.txt", sizeMB, time.Now().Unix())
	templateFilePath = filepath.Join(currentDir, templateFileName)

	fmt.Printf("Creating template file (%d MB)..\n", sizeMB)
	startTemplate := time.Now()
	err = createRandomFile(templateFilePath, sizeMB, false) // No progress for template
	if err != nil {
		return fmt.Errorf("failed to create template file: %w", err)
	}
	templateDuration := time.Since(startTemplate)
	fmt.Printf("✓ Template file created in %s\n\n", formatDuration(templateDuration))

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Start filling
	fmt.Printf("Starting fill operation..\n")
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	for i := int64(1); i <= maxFiles; i++ {
		// Check for interruption
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}

		// Generate file name: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(folderPath, fileName)

		// Copy template file to target
		bytesCopied, err := copyFileOptimized(templateFilePath, targetFilePath)
		if err != nil {
			fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", i, err)
			break
		}

		filesCreated++
		totalBytesWritten += bytesCopied
		progress.Update(filesCreated, totalBytesWritten)
		progress.PrintProgress("Fill")
	}

	// Clean up template file
	os.Remove(templateFilePath)

	// Final summary
	progress.Finish("Fill Operation")

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files..\n")

		// Find all FILL_*.tmp files in the folder
		pattern := filepath.Join(folderPath, "FILL_*.tmp")
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

	return nil
}

func runFolderFillClean(folderPath string) error {
	fmt.Printf("Folder Clean Operation\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)..\n\n")

	// Check if folder exists and is accessible
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path is not accessible: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Find all FILL_*.tmp files
	fillPattern := filepath.Join(folderPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	// Find all speedtest_*.txt files
	speedtestPattern := filepath.Join(folderPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		return fmt.Errorf("failed to search for speedtest files: %w", err)
	}

	// Combine all matches
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", folderPath)
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
	fmt.Printf("Deleting files..\n\n")

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

	return nil
}

// FolderTester implements FakeCapacityTester for folder testing
type FolderTester struct {
	folderPath string
}

// NewFolderTester creates a new folder tester
func NewFolderTester(folderPath string) *FolderTester {
	return &FolderTester{folderPath: folderPath}
}

func (ft *FolderTester) GetTestInfo() (string, string) {
	return "Folder", ft.folderPath
}

func (ft *FolderTester) GetAvailableSpace() (int64, error) {
	// Get available space on the drive where the folder is located
	var freeBytesAvailableToCaller, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	folderPathUTF16, err := windows.UTF16PtrFromString(ft.folderPath)
	if err != nil {
		return 0, fmt.Errorf("failed to convert path to UTF16: %v", err)
	}

	err = windows.GetDiskFreeSpaceEx(folderPathUTF16, &freeBytesAvailableToCaller, &totalNumberOfBytes, &totalNumberOfFreeBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk space: %v", err)
	}

	return int64(freeBytesAvailableToCaller), nil
}

func (ft *FolderTester) CreateTestFile(fileName string, fileSize int64) (string, error) {
	filePath := filepath.Join(ft.folderPath, fileName)

	// Use optimized direct write instead of streaming
	err := writeTestFileContentOptimized(filePath, fileSize)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %v", fileName, err)
	}

	return filePath, nil
}

func (ft *FolderTester) VerifyTestFile(filePath string) error {
	return verifyTestFileStartEnd(filePath)
}

func (ft *FolderTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (ft *FolderTester) GetCleanupCommand() string {
	return fmt.Sprintf("filedo folder %s fill clean", ft.folderPath)
}

func (ft *FolderTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (string, error) {
	filePath := filepath.Join(ft.folderPath, fileName)

	// Use optimized direct write with context support
	err := writeTestFileContentOptimizedContext(ctx, filePath, fileSize)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %v", fileName, err)
	}

	return filePath, nil
}

// runFolderTest now uses the generic test function
func runFolderTest(folderPath string, autoDelete bool) error {
	tester := NewFolderTester(folderPath)
	_, err := runGenericFakeCapacityTest(tester, autoDelete, nil)
	return err
}
