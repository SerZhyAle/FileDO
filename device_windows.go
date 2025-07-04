//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

	volumePathName := make([]uint16, windows.MAX_PATH)
	err := windows.GetVolumePathName(windows.StringToUTF16Ptr(pathWithSlash), &volumePathName[0], windows.MAX_PATH)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("failed to get volume path name for '%s': %w", path, err)
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
				if os.IsPermission(err) {
					accessErrors = true
					return nil
				}
				return err
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
		if walkErr != nil {
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
		fmt.Printf("Target: %s\n", normalizedPath)
		fmt.Printf("Test file size: %d MB\n\n", sizeMB)

		// Step 1: Check if device is accessible and writable
		fmt.Printf("Step 1: Checking device accessibility...\n")
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
		fmt.Printf("Step 2: Creating test file (%d MB)...\n", sizeMB)
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
		fmt.Printf("✓ Test file created in %v\n\n", createDuration)

		// Step 3: Upload Speed Test - Copy file to device
		deviceFileName := filepath.Join(normalizedPath, localFileName)
		fmt.Printf("Step 3: Upload Speed Test - Copying file to device...\n")
		fmt.Printf("Source: %s\n", localFilePath)
		fmt.Printf("Target: %s\n", deviceFileName)
	}

	// Step 3: Upload Speed Test - Copy file to device
	deviceFileName := filepath.Join(normalizedPath, localFileName)

	startUpload := time.Now()
	bytesUploaded, err := copyFileWithProgress(localFilePath, deviceFileName, !shortFormat)
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
		fmt.Printf("Upload completed in %v\n", uploadDuration)
		fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)

		// Step 4: Download Speed Test - Copy file back from device
		downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
		downloadFilePath := filepath.Join(currentDir, downloadFileName)
		fmt.Printf("Step 4: Download Speed Test - Copying file from device...\n")
		fmt.Printf("Source: %s\n", deviceFileName)
		fmt.Printf("Target: %s\n", downloadFilePath)
	}

	// Step 4: Download Speed Test - Copy file back from device
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
	downloadFilePath := filepath.Join(currentDir, downloadFileName)

	startDownload := time.Now()
	bytesDownloaded, err := copyFileWithProgress(deviceFileName, downloadFilePath, !shortFormat)
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
		fmt.Printf("Download completed in %v\n", downloadDuration)
		fmt.Printf("Download Speed: %.2f MB/s (%.2f Mbps)\n\n", downloadSpeedMBps, downloadSpeedMbps)

		// Step 5: Clean up files
		fmt.Printf("Step 5: Cleaning up test files...\n")
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
		fmt.Printf("Upload time: %v, Speed: %.2f MB/s (%.2f Mbps)\n", uploadDuration, uploadSpeedMBps, uploadSpeedMbps)
		fmt.Printf("Download time: %v, Speed: %.2f MB/s (%.2f Mbps)\n", downloadDuration, downloadSpeedMBps, downloadSpeedMbps)
	}

	return nil
}

func runDeviceFill(devicePath, sizeMBStr string, autoDelete bool) error {
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
	fmt.Printf("Target: %s\n", normalizedPath)
	fmt.Printf("File size: %d MB\n\n", sizeMB)

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

	// Create template file first
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	templateFileName := fmt.Sprintf("fill_template_%d_%d.txt", sizeMB, time.Now().Unix())
	templateFilePath := filepath.Join(currentDir, templateFileName)

	fmt.Printf("Creating template file (%d MB)...\n", sizeMB)
	startTemplate := time.Now()
	err = createRandomFile(templateFilePath, sizeMB, false) // No progress for template
	if err != nil {
		return fmt.Errorf("failed to create template file: %w", err)
	}
	templateDuration := time.Since(startTemplate)
	fmt.Printf("✓ Template file created in %v\n\n", templateDuration)

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Start filling
	fmt.Printf("Starting fill operation...\n")
	startFill := time.Now()
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	for i := int64(1); i <= maxFiles; i++ {
		// Generate file name: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(normalizedPath, fileName)

		// Copy template file to target
		startCopy := time.Now()
		bytesCopied, err := copyFileWithProgress(templateFilePath, targetFilePath, false) // No progress for individual files
		if err != nil {
			fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", i, err)
			break
		}
		copyDuration := time.Since(startCopy)

		filesCreated++
		totalBytesWritten += bytesCopied

		// Show progress every 10 files or every second
		if i%10 == 0 || copyDuration > time.Second {
			copySpeedMBps := float64(bytesCopied) / (1024 * 1024) / copyDuration.Seconds()
			percentComplete := float64(filesCreated) / float64(maxFiles) * 100
			gbWritten := float64(totalBytesWritten) / (1024 * 1024 * 1024)
			fmt.Printf("Fill %s: %3.0f%% %d/%d files (%6.1f MB/s) - %6.2f GB\r",
				normalizedPath, percentComplete, filesCreated, maxFiles, copySpeedMBps, gbWritten)
		}
	}

	fillDuration := time.Since(startFill)

	// Clean up template file
	os.Remove(templateFilePath)

	// Final summary
	fmt.Printf("\n\nFill Operation Complete!\n")
	fmt.Printf("Files created: %d\n", filesCreated)
	fmt.Printf("Total data written: %.2f GB\n", float64(totalBytesWritten)/(1024*1024*1024))
	fmt.Printf("Total time: %v\n", fillDuration)

	if fillDuration.Seconds() > 0 {
		avgSpeedMBps := float64(totalBytesWritten) / (1024 * 1024) / fillDuration.Seconds()
		fmt.Printf("Average write speed: %.2f MB/s\n", avgSpeedMBps)
	}

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files...\n")

		// Find all FILL_*.tmp files in the device
		pattern := filepath.Join(normalizedPath, "FILL_*.tmp")
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

func runDeviceFillClean(devicePath string) error {
	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Fill Clean Operation\n")
	fmt.Printf("Target: %s\n", normalizedPath)
	fmt.Printf("Searching for FILL_*.tmp files...\n\n")

	// Check if device is accessible
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Find all FILL_*.tmp files
	pattern := filepath.Join(normalizedPath, "FILL_*.tmp")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	if len(matches) == 0 {
		fmt.Printf("No FILL_*.tmp files found in %s\n", normalizedPath)
		return nil
	}

	fmt.Printf("Found %d FILL_*.tmp files\n", len(matches))

	// Calculate total size before deletion
	var totalSize int64
	for _, filePath := range matches {
		if info, err := os.Stat(filePath); err == nil {
			totalSize += info.Size()
		}
	}

	fmt.Printf("Total size to delete: %.2f GB\n", float64(totalSize)/(1024*1024*1024))
	fmt.Printf("Deleting files...\n\n")

	// Delete files
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

	fmt.Printf("\n\nClean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deletedCount, len(matches))
	fmt.Printf("Space freed: %.2f GB\n", float64(deletedSize)/(1024*1024*1024))

	if deletedCount < len(matches) {
		fmt.Printf("Warning: %d files could not be deleted\n", len(matches)-deletedCount)
	}

	return nil
}

func runDeviceTest(devicePath string, autoDelete bool) error {
	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Fake Capacity Test\n")
	fmt.Printf("Target: %s\n", normalizedPath)
	fmt.Printf("Testing for fake capacity by writing 100 files...\n\n")

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

	// Check minimum space requirement (100MB)
	minSpaceBytes := int64(100 * 1024 * 1024) // 100MB
	if int64(freeBytesAvailable) < minSpaceBytes {
		return fmt.Errorf("insufficient space: need at least 100MB free, but only %.2f MB available",
			float64(freeBytesAvailable)/(1024*1024))
	}

	// Calculate file size (1% of free space)
	fileSizeBytes := int64(freeBytesAvailable) / 100
	fileSizeMB := fileSizeBytes / (1024 * 1024)

	fmt.Printf("Available space: %.2f GB\n", float64(freeBytesAvailable)/(1024*1024*1024))
	fmt.Printf("Test file size: %d MB (1%% of free space)\n", fileSizeMB)
	fmt.Printf("Will create 100 files for capacity test\n\n")

	// Create template file first
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	templateFileName := fmt.Sprintf("test_template_%d_%d.txt", fileSizeMB, time.Now().Unix())
	templateFilePath := filepath.Join(currentDir, templateFileName)

	fmt.Printf("Creating template file (%d MB)...\n", fileSizeMB)
	startTemplate := time.Now()
	err = createRandomFile(templateFilePath, int(fileSizeMB), false)
	if err != nil {
		return fmt.Errorf("failed to create template file: %w", err)
	}
	templateDuration := time.Since(startTemplate)
	fmt.Printf("✓ Template file created in %v\n\n", templateDuration)

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Start capacity test
	fmt.Printf("Starting capacity test - writing 100 files...\n")
	startTest := time.Now()
	filesCreated := 0
	totalBytesWritten := int64(0)
	writeSpeeds := make([]float64, 0, 100)
	var normalSpeed float64
	testFailed := false
	failureReason := ""

	for i := 1; i <= 100; i++ {
		// Generate file name: FILL_001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%03d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(normalizedPath, fileName)

		// Copy template file to target
		startCopy := time.Now()
		bytesCopied, err := copyFileWithProgress(templateFilePath, targetFilePath, false)
		if err != nil {
			testFailed = true
			failureReason = fmt.Sprintf("Failed to create file %d: %v", i, err)
			break
		}
		copyDuration := time.Since(startCopy)

		filesCreated++
		totalBytesWritten += bytesCopied

		// Calculate write speed for this file
		copySpeedMBps := float64(bytesCopied) / (1024 * 1024) / copyDuration.Seconds()
		writeSpeeds = append(writeSpeeds, copySpeedMBps)

		// Establish normal speed from first 3 files
		if i <= 3 {
			// For first 3 files, just collect speeds
			fmt.Printf("File %3d: %.1f MB/s - establishing baseline\n", i, copySpeedMBps)
		} else if i == 4 {
			// Calculate normal speed as average of first 3 files
			normalSpeed = (writeSpeeds[0] + writeSpeeds[1] + writeSpeeds[2]) / 3
			fmt.Printf("Normal speed established: %.1f MB/s\n", normalSpeed)
			fmt.Printf("File %3d: %.1f MB/s\n", i, copySpeedMBps)
		} else {
			// Check speed against normal speed
			speedRatio := copySpeedMBps / normalSpeed

			// Check for abnormal speeds
			if copySpeedMBps < normalSpeed*0.1 { // Less than 10% of normal speed
				testFailed = true
				failureReason = fmt.Sprintf("Write speed dropped to %.1f MB/s (%.1f%% of normal %.1f MB/s) at file %d - possible fake capacity detected",
					copySpeedMBps, speedRatio*100, normalSpeed, i)
				break
			} else if copySpeedMBps > normalSpeed*10 { // More than 10x normal speed
				testFailed = true
				failureReason = fmt.Sprintf("Write speed jumped to %.1f MB/s (%.1fx normal %.1f MB/s) at file %d - unrealistic speed, possible fake writing",
					copySpeedMBps, speedRatio, normalSpeed, i)
				break
			}

			// Show progress
			fmt.Printf("File %3d: %.1f MB/s (%3.0f%% of normal)\n", i, copySpeedMBps, speedRatio*100)
		}
	}

	testDuration := time.Since(startTest)

	// Clean up template file
	os.Remove(templateFilePath)

	fmt.Printf("\nCapacity Test Phase Complete!\n")
	fmt.Printf("Files created: %d out of 100\n", filesCreated)
	fmt.Printf("Total data written: %.2f GB\n", float64(totalBytesWritten)/(1024*1024*1024))
	fmt.Printf("Test duration: %v\n", testDuration)

	if testFailed {
		fmt.Printf("❌ TEST FAILED: %s\n\n", failureReason)
	} else {
		fmt.Printf("✅ Capacity test passed - no fake capacity detected\n\n")
	}

	// Verification phase - check file integrity
	fmt.Printf("Starting verification phase - checking file integrity...\n")

	verificationFailed := false
	verificationFailureFile := ""
	verifiedCount := 0

	// Find all FILL_*.tmp files we created
	pattern := filepath.Join(normalizedPath, fmt.Sprintf("FILL_*_%s.tmp", timestamp))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		fmt.Printf("⚠ Warning: Failed to search for test files: %v\n", err)
	} else {
		// Sort matches to check in order
		sort.Strings(matches)

		for i, filePath := range matches {
			// Read first line of file
			file, err := os.Open(filePath)
			if err != nil {
				verificationFailed = true
				verificationFailureFile = filepath.Base(filePath)
				break
			}

			scanner := bufio.NewScanner(file)
			if scanner.Scan() {
				firstLine := scanner.Text()
				expectedLine := "=== BLOCK 000001 === START ==="

				if firstLine != expectedLine {
					verificationFailed = true
					verificationFailureFile = filepath.Base(filePath)
					file.Close()
					break
				}
			} else {
				verificationFailed = true
				verificationFailureFile = filepath.Base(filePath)
				file.Close()
				break
			}
			file.Close()

			verifiedCount++

			// Show progress every 10 files
			if (i+1)%10 == 0 || i == len(matches)-1 {
				fmt.Printf("Verified %d/%d files\n", verifiedCount, len(matches))
			}
		}
	}

	fmt.Printf("\nVerification Phase Complete!\n")
	if verificationFailed {
		fmt.Printf("❌ VERIFICATION FAILED: File corruption detected at %s\n", verificationFailureFile)
		fmt.Printf("Files verified: %d out of %d\n", verifiedCount, len(matches))
		testFailed = true
	} else {
		fmt.Printf("✅ All files verified successfully\n")
		fmt.Printf("Files verified: %d\n", verifiedCount)
	}

	// Final summary
	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("FAKE CAPACITY TEST SUMMARY\n")
	fmt.Printf(strings.Repeat("=", 60) + "\n")
	fmt.Printf("Device: %s\n", normalizedPath)
	fmt.Printf("Reported capacity: %.2f GB\n", float64(totalBytes)/(1024*1024*1024))
	fmt.Printf("Available space: %.2f GB\n", float64(freeBytesAvailable)/(1024*1024*1024))
	fmt.Printf("Test file size: %d MB each\n", fileSizeMB)
	fmt.Printf("Files created: %d out of 100\n", filesCreated)
	fmt.Printf("Data written: %.2f GB\n", float64(totalBytesWritten)/(1024*1024*1024))

	if len(writeSpeeds) >= 3 {
		fmt.Printf("Normal write speed: %.1f MB/s\n", normalSpeed)
	}

	if testFailed {
		fmt.Printf("\n❌ OVERALL RESULT: FAKE CAPACITY DETECTED\n")
		if failureReason != "" {
			fmt.Printf("Reason: %s\n", failureReason)
		}
		if verificationFailed {
			fmt.Printf("Additional issue: File corruption at %s\n", verificationFailureFile)
		}
		fmt.Printf("\n⚠ WARNING: This device appears to have fake capacity!\n")
		fmt.Printf("The actual capacity is likely much smaller than reported.\n")
		fmt.Printf("Test files have been preserved for analysis.\n")
	} else {
		fmt.Printf("\n✅ OVERALL RESULT: DEVICE APPEARS GENUINE\n")
		fmt.Printf("No fake capacity detected. Device seems to have legitimate storage.\n")

		// Auto-delete if requested and test passed
		if autoDelete {
			fmt.Printf("\nAuto-delete enabled - Deleting all test files...\n")
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

						// Show progress every 10 files
						if (i+1)%10 == 0 || i == len(matches)-1 {
							fmt.Printf("Deleted %d/%d files - %.2f GB freed\n", deletedCount, len(matches), float64(deletedSize)/(1024*1024*1024))
						}
					}
				}
			}

			fmt.Printf("Auto-delete complete: %d files deleted\n", deletedCount)
		}
	}

	return nil
}
