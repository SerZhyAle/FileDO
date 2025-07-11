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
	"unicode"

	"github.com/StackExchange/wmi"
	"golang.org/x/sys/windows"
)

// WMI structures for disk information
type Win32_LogicalDisk struct {
	DeviceID string
}

type Win32_LogicalDiskToPartition struct {
	Antecedent string // Reference to Win32_DiskPartition
	Dependent  string // Reference to Win32_LogicalDisk
}

type Win32_DiskDriveToDiskPartition struct {
	Antecedent string // Reference to Win32_DiskDrive
	Dependent  string // Reference to Win32_DiskPartition
}

type Win32_DiskDrive struct {
	DeviceID      string
	Model         string
	SerialNumber  string
	InterfaceType string
}

// getWMIPhysicalDiskInfo retrieves physical disk details using WMI.
func getWMIPhysicalDiskInfo(logicalDiskID string) (model, serial, iface string) {
	// WMI queries can fail for various reasons (permissions, WMI service issues, etc.)
	// We'll log errors but not return them, as this info is supplementary.
	// The logicalDiskID is typically like "C:"
	// WMI paths use backslashes, so ensure consistency.
	logicalDiskID = strings.TrimSuffix(logicalDiskID, `\`)
	logicalDiskID = strings.ReplaceAll(logicalDiskID, `\`, `\\`)

	// 1. Find the partition associated with the logical disk
	var logicalDiskToPartitions []Win32_LogicalDiskToPartition
	query := fmt.Sprintf("SELECT Antecedent, Dependent FROM Win32_LogicalDiskToPartition WHERE Dependent = \"Win32_LogicalDisk.DeviceID='%s'\"", logicalDiskID)
	err := wmi.Query(query, &logicalDiskToPartitions)
	if err != nil {
		// Silent failure - WMI info is supplementary
		return
	}

	if len(logicalDiskToPartitions) == 0 {
		return
	}

	// The Antecedent contains the path to Win32_DiskPartition, e.g., "Win32_DiskPartition.DeviceID='Disk #0, Partition #0'"
	partitionPath := logicalDiskToPartitions[0].Antecedent

	// Extract the DeviceID from the partition path
	// The path format is typically: Win32_DiskPartition.DeviceID="Disk #0, Partition #0"
	var partitionDeviceID string
	if idx := strings.Index(partitionPath, "DeviceID=\""); idx != -1 {
		start := idx + len("DeviceID=\"")
		if end := strings.Index(partitionPath[start:], "\""); end != -1 {
			partitionDeviceID = partitionPath[start : start+end]
		}
	}

	if partitionDeviceID == "" {
		return
	}

	// 2. Find the physical disk associated with the partition using DeviceID
	var diskDriveToPartitions []Win32_DiskDriveToDiskPartition
	query = fmt.Sprintf("SELECT Antecedent, Dependent FROM Win32_DiskDriveToDiskPartition WHERE Dependent = \"Win32_DiskPartition.DeviceID='%s'\"", partitionDeviceID)
	err = wmi.Query(query, &diskDriveToPartitions)
	if err != nil {
		// Silent failure - WMI info is supplementary
		return
	}

	if len(diskDriveToPartitions) == 0 {
		return
	}

	// The Antecedent contains the path to Win32_DiskDrive, e.g., "Win32_DiskDrive.DeviceID='\\\\.\\PHYSICALDRIVE0'"
	diskDrivePath := diskDriveToPartitions[0].Antecedent

	// Extract DeviceID from the disk drive path
	// The path format is typically: Win32_DiskDrive.DeviceID="\\\\.\\PHYSICALDRIVE0"
	var physicalDiskDeviceID string
	if idx := strings.Index(diskDrivePath, "DeviceID=\""); idx != -1 {
		start := idx + len("DeviceID=\"")
		if end := strings.Index(diskDrivePath[start:], "\""); end != -1 {
			physicalDiskDeviceID = diskDrivePath[start : start+end]
		}
	}

	if physicalDiskDeviceID == "" {
		return
	}

	// 3. Get details of the physical disk
	var diskDrives []Win32_DiskDrive
	query = fmt.Sprintf("SELECT Model, SerialNumber, InterfaceType FROM Win32_DiskDrive WHERE DeviceID = \"%s\"", physicalDiskDeviceID)
	err = wmi.Query(query, &diskDrives)
	if err != nil {
		// Silent failure - WMI info is supplementary
		return
	}

	if len(diskDrives) > 0 {
		return diskDrives[0].Model, diskDrives[0].SerialNumber, diskDrives[0].InterfaceType
	}
	return
}

func getDeviceInfo(path string, fullScan bool) (DeviceInfo, error) {
	pathWithSlash := path
	if len(pathWithSlash) == 1 && unicode.IsLetter(rune(pathWithSlash[0])) {
		pathWithSlash += ":"
	}

	if len(pathWithSlash) == 2 && pathWithSlash[1] == ':' {
		pathWithSlash += `\`
	}

	// First check if drive letter exists
	if len(pathWithSlash) >= 2 && pathWithSlash[1] == ':' {
		driveLetter := unicode.ToUpper(rune(pathWithSlash[0]))
		// Check if drive exists by attempting to get drive type
		driveType := windows.GetDriveType(windows.StringToUTF16Ptr(string(driveLetter) + `:\`))
		if driveType == windows.DRIVE_NO_ROOT_DIR {
			return DeviceInfo{}, fmt.Errorf("device '%s' does not exist", path)
		}
	}

	volumePathName := make([]uint16, windows.MAX_PATH)
	err := windows.GetVolumePathName(windows.StringToUTF16Ptr(pathWithSlash), &volumePathName[0], windows.MAX_PATH)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("device '%s' is not accessible: %w", path, err)
	}
	rootPath := windows.UTF16ToString(volumePathName)

	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(rootPath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("GetDiskFreeSpaceEx failed for '%s': %w", rootPath, err)
	}

	var volName, fsName [windows.MAX_PATH]uint16
	var serialNumber, maxComponentLen, fsFlags uint32
	err = windows.GetVolumeInformation(windows.StringToUTF16Ptr(rootPath), &volName[0], windows.MAX_PATH, &serialNumber, &maxComponentLen, &fsFlags, &fsName[0], windows.MAX_PATH)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("GetVolumeInformation failed for '%s': %w", rootPath, err)
	}

	var fileCount, folderCount int64
	var accessErrors bool

	if fullScan {
		walkErr := filepath.WalkDir(rootPath, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsPermission(err) || strings.Contains(err.Error(), "being used by another process") || strings.Contains(err.Error(), "cannot access the file") {
					accessErrors = true
					return nil
				}
				accessErrors = true
				return nil
			}
			if d.IsDir() {
				if p != rootPath {
					folderCount++
				}
			} else {
				fileCount++
			}
			return nil
		})
		if walkErr != nil && !accessErrors {
			return DeviceInfo{}, fmt.Errorf("failed to walk directory '%s': %w", rootPath, walkErr)
		}
	} else {
		entries, err := os.ReadDir(rootPath)
		if err != nil {
			return DeviceInfo{}, fmt.Errorf("failed to read root directory '%s': %w", rootPath, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				folderCount++
			} else {
				fileCount++
			}
		}
	}

	var diskModel, diskSerialNumber, diskInterface string
	if fullScan {
		// rootPath is like "C:\"
		diskModel, diskSerialNumber, diskInterface = getWMIPhysicalDiskInfo(rootPath)
	}

	// Test read access
	canRead := false
	_, readErr := os.ReadDir(rootPath)
	if readErr == nil {
		canRead = true
	}

	// Test write access
	canWrite := false
	testFileName := fmt.Sprintf("__filedo_access_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(rootPath, testFileName)
	if testFile, writeErr := os.Create(testFilePath); writeErr == nil {
		testFile.Close()
		os.Remove(testFilePath) // Clean up test file
		canWrite = true
	}

	return DeviceInfo{
		Path: path, VolumeName: windows.UTF16ToString(volName[:]), SerialNumber: serialNumber, FileSystem: windows.UTF16ToString(fsName[:]),
		TotalBytes: totalBytes, FreeBytes: totalFreeBytes, AvailableBytes: freeBytesAvailable,
		FileCount: fileCount, FolderCount: folderCount, FullScan: fullScan, AccessErrors: accessErrors,
		DiskModel: diskModel, DiskSerialNumber: diskSerialNumber, DiskInterface: diskInterface,
		CanRead: canRead, CanWrite: canWrite,
	}, nil
}

func runDeviceSpeedTest(devicePath, sizeMBStr string, noDelete, shortFormat bool) error {
	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 1
		//return fmt.Errorf("invalid size '%s': %w", sizeMBStr, err)
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB
		sizeMB = 1
		//return fmt.Errorf("size must be between 1 and 10240 MB")
	}

	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	if !shortFormat {
		fmt.Printf("Device Speed Test\n")
		fmt.Printf("Target: %s\n", getEnhancedDeviceInfo(normalizedPath))
		fmt.Printf("Test file size: %d MB\n\n", sizeMB)

		// Step 1: Check if device is accessible and writable
		fmt.Printf("Step 1: Checking device accessibility..\n")
	}

	// Check if we can stat the device path
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Test write access by creating a temporary file
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(normalizedPath, testFileName)

	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("device path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	if !shortFormat {
		fmt.Printf("✓ Device is accessible and writable\n\n")

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

		// Step 3: Upload Speed Test - Copy file to device
		deviceFileName := filepath.Join(normalizedPath, localFileName)
		fmt.Printf("Step 3: Upload Speed Test - Copying file to device..\n")
		fmt.Printf("Source: %s\n", localFilePath)
		fmt.Printf("Target: %s\n", deviceFileName)
	}

	// Step 3: Upload Speed Test - Copy file to device
	deviceFileName := filepath.Join(normalizedPath, localFileName)

	startUpload := time.Now()
	bytesUploaded, err := copyFileOptimized(localFilePath, deviceFileName)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFilePath)
		return fmt.Errorf("failed to copy file to device: %w", err)
	}
	uploadDuration := time.Since(startUpload)

	// Calculate upload speed
	uploadSpeedMBps := float64(bytesUploaded) / (1024 * 1024) / uploadDuration.Seconds()
	uploadSpeedMbps := uploadSpeedMBps * 8 // Convert to megabits per second

	if !shortFormat {
		fmt.Printf("\n✓ File uploaded successfully\n")
		fmt.Printf("Upload completed in %s\n", formatDuration(uploadDuration))
		fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)

		// Step 4: Download Speed Test - Copy file back from device
		downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
		downloadFilePath := filepath.Join(currentDir, downloadFileName)
		fmt.Printf("Step 4: Download Speed Test - Copying file from device..\n")
		fmt.Printf("Source: %s\n", deviceFileName)
		fmt.Printf("Target: %s\n", downloadFilePath)
	}

	// Step 4: Download Speed Test - Copy file back from device
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
	downloadFilePath := filepath.Join(currentDir, downloadFileName)

	startDownload := time.Now()
	bytesDownloaded, err := copyFileOptimized(deviceFileName, downloadFilePath)
	if err != nil {
		// Clean up files before returning error
		os.Remove(localFilePath)
		os.Remove(deviceFileName)
		return fmt.Errorf("failed to copy file from device: %w", err)
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

	// Remove device file (unless noDelete flag is set)
	if noDelete {
		if !shortFormat {
			fmt.Printf("✓ Device test file kept: %s\n", deviceFileName)
		}
	} else {
		if err := os.Remove(deviceFileName); err != nil && !shortFormat {
			fmt.Printf("⚠ Warning: Could not remove device file: %v\n", err)
		} else if !shortFormat {
			fmt.Printf("✓ Device test file removed\n")
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

func runDeviceFill(devicePath, sizeMBStr string, autoDelete bool) error {
	// Setup interrupt handler
	handler := NewInterruptHandler()

	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 100 // Default to 100 MB if parsing fails
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB per file
		sizeMB = 100 // Default to 100 MB if out of range
	}

	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Fill Operation\n")
	fmt.Printf("Target: %s\n", getEnhancedDeviceInfo(normalizedPath))
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Check if device is accessible and writable
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Test write access
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(normalizedPath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("device path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	// Get available space
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(normalizedPath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
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

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Pre-calibrate optimal buffer for faster operations
	dir := normalizedPath
	optimalBuffer := calibrateOptimalBufferSize(dir)
	optimalBuffers[dir] = optimalBuffer

	// Use larger bufferSize for fill operations (4x the calibrated buffer)
	optimalBuffer = min(optimalBuffer*4, 128*1024*1024) // Max 128MB buffer
	optimalBuffers[dir] = optimalBuffer

	// Start filling
	fmt.Printf("Starting fill operation..\n")
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	// Pre-create file paths to reduce string operations in loop
	filePathList := make([]string, maxFiles)
	for i := int64(1); i <= maxFiles; i++ {
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		filePathList[i-1] = filepath.Join(normalizedPath, fileName)
	}
	// Use worker pool for parallel file creation
	parallelism := 12 // Create 12 files concurrently

	// Create channel for work
	jobs := make(chan int64, maxFiles)
	results := make(chan struct {
		fileIndex int64
		err       error
	}, maxFiles)

	// Launch workers
	for w := 0; w < parallelism; w++ {
		go func() {
			for i := range jobs {
				if handler.IsCancelled() {
					results <- struct {
						fileIndex int64
						err       error
					}{i, fmt.Errorf("cancelled")}
					continue
				}

				// Use pre-computed file path
				targetFilePath := filePathList[i-1]

				// Create file directly with optimized function
				err := writeTestFileWithBuffer(targetFilePath, fileSizeBytes, optimalBuffer)
				results <- struct {
					fileIndex int64
					err       error
				}{i, err}
			}
		}()
	}

	// Send jobs
	for i := int64(1); i <= maxFiles; i++ {
		jobs <- i
	}
	close(jobs)

	// Collect results
	for i := int64(1); i <= maxFiles; i++ {
		// Check for interruption
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}

		result := <-results
		if result.err != nil {
			if result.err.Error() != "cancelled" {
				fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", result.fileIndex, result.err)
			}
			if result.fileIndex <= 1 {
				break
			}
		} else {
			filesCreated++
			totalBytesWritten += fileSizeBytes

			// Update progress less frequently (every 4 files or at least once every 2 seconds)
			if filesCreated%4 == 0 || filesCreated == 1 || progress.ShouldUpdate() {
				progress.Update(filesCreated, totalBytesWritten)
				progress.PrintProgress("Fill")
			}
		}
	}

	// Final summary
	progress.Finish("Fill Operation")

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files..\n")

		// Use pre-created file paths to avoid searching
		var deletedCount int64 = 0
		var deletedSize int64 = 0

		// Create a worker pool for deletion
		deletionWorkers := 24 // More workers for deletion
		deletionJobs := make(chan string, filesCreated)
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
						if err == nil {
							atomic.AddInt64((*int64)(&deletedCount), 1)
							atomic.AddInt64(&deletedSize, fileSize)
						}
					}
				}
			}()
		}

		// Queue all files for deletion
		for _, filePath := range filePathList[:filesCreated] {
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
		for {
			select {
			case <-updateTicker.C:
				fmt.Printf("Deleted %d/%d files - %.2f GB freed\r", deletedCount, filesCreated, float64(deletedSize)/(1024*1024*1024))
			case <-done:
				updateTicker.Stop()
				goto deletionComplete
			}
		}

	deletionComplete:

		fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", deletedCount, float64(deletedSize)/(1024*1024*1024))
	}

	return nil
}

func runDeviceFillClean(devicePath string) error {
	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Clean Operation\n")
	fmt.Printf("Target: %s\n", getEnhancedDeviceInfo(normalizedPath))
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)..\n\n")

	// Check if device is accessible
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Find all FILL_*.tmp files
	fillPattern := filepath.Join(normalizedPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	// Find all speedtest_*.txt files
	speedtestPattern := filepath.Join(normalizedPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		return fmt.Errorf("failed to search for speedtest files: %w", err)
	}

	// Combine all matches
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", normalizedPath)
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

// DeviceTester implements FakeCapacityTester for device testing
type DeviceTester struct {
	devicePath string
}

// NewDeviceTester creates a new device tester
func NewDeviceTester(devicePath string) *DeviceTester {
	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}
	return &DeviceTester{devicePath: normalizedPath}
}

func (dt *DeviceTester) GetTestInfo() (string, string) {
	return "Device", dt.devicePath
}

func (dt *DeviceTester) GetAvailableSpace() (int64, error) {
	// Check if device is accessible
	if _, err := os.Stat(dt.devicePath); err != nil {
		return 0, fmt.Errorf("device path is not accessible: %w", err)
	}

	// Test write access
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(dt.devicePath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return 0, fmt.Errorf("device path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	// Get available space using Windows API
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(dt.devicePath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk space information: %w", err)
	}

	return int64(freeBytesAvailable), nil
}

func (dt *DeviceTester) CreateTestFile(fileName string, fileSize int64) (string, error) {
	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Override filename with timestamp format: FILL_001_ddHHmmss.tmp
	parts := strings.Split(fileName, "_")
	if len(parts) >= 2 {
		fileName = fmt.Sprintf("FILL_%s_%s.tmp", parts[1], timestamp)
	}

	filePath := filepath.Join(dt.devicePath, fileName)

	// Use optimized direct write instead of template file approach
	err := writeTestFileContentOptimized(filePath, fileSize)
	if err != nil {
		return "", fmt.Errorf("failed to create test file: %w", err)
	}

	return filePath, nil
}

func (dt *DeviceTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (string, error) {
	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Override filename with timestamp format: FILL_001_ddHHmmss.tmp
	parts := strings.Split(fileName, "_")
	if len(parts) >= 2 {
		fileName = fmt.Sprintf("FILL_%s_%s.tmp", parts[1], timestamp)
	}

	filePath := filepath.Join(dt.devicePath, fileName)

	// Use optimized direct write with context support
	err := writeTestFileContentOptimizedContext(ctx, filePath, fileSize)
	if err != nil {
		return "", fmt.Errorf("failed to create test file: %w", err)
	}

	return filePath, nil
}

func (dt *DeviceTester) VerifyTestFile(filePath string) error {
	return verifyTestFileStartEnd(filePath)
}

func (dt *DeviceTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (dt *DeviceTester) GetCleanupCommand() string {
	return fmt.Sprintf("filedo device %s fill clean", dt.devicePath)
}

// runDeviceTest now uses the generic test function
func runDeviceTest(devicePath string, autoDelete bool) error {
	tester := NewDeviceTester(devicePath)
	_, err := runGenericFakeCapacityTest(tester, autoDelete, nil)
	return err
}

// Helper function to get enhanced device info for display
func getEnhancedDeviceInfo(devicePath string) string {
	if deviceInfo, err := getDeviceInfo(devicePath, false); err == nil {
		volumeName := deviceInfo.VolumeName
		if volumeName == "" {
			volumeName = "No label"
		}
		totalSizeGB := float64(deviceInfo.TotalBytes) / (1024 * 1024 * 1024)
		return fmt.Sprintf("%s (%s) [%.1f GB]", devicePath, volumeName, totalSizeGB)
	}
	return devicePath
}
