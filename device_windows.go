//go:build windows

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

func runDeviceSpeedTest(devicePath, sizeMBStr string, noDelete bool) error {
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

	fmt.Printf("Device Speed Test\n")
	fmt.Printf("Target: %s\n", normalizedPath)
	fmt.Printf("Test file size: %d MB\n\n", sizeMB)

	// Step 1: Check if device is accessible and writable
	fmt.Printf("Step 1: Checking device accessibility...\n")

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

	fmt.Printf("✓ Device is accessible and writable\n\n")

	// Step 2: Create test file in current directory
	fmt.Printf("Step 2: Creating test file (%d MB)...\n", sizeMB)
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	localFileName := fmt.Sprintf("speedtest_%d_%d.txt", sizeMB, time.Now().Unix())
	localFilePath := filepath.Join(currentDir, localFileName)

	startCreate := time.Now()
	err = createRandomFile(localFilePath, sizeMB)
	if err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}
	createDuration := time.Since(startCreate)
	fmt.Printf("✓ Test file created in %v\n\n", createDuration)

	// Step 3: Upload Speed Test - Copy file to device
	deviceFileName := filepath.Join(normalizedPath, localFileName)
	fmt.Printf("Step 3: Upload Speed Test - Copying file to device...\n")
	fmt.Printf("Source: %s\n", localFilePath)
	fmt.Printf("Target: %s\n", deviceFileName)

	startUpload := time.Now()
	bytesUploaded, err := copyFileWithProgress(localFilePath, deviceFileName)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFilePath)
		return fmt.Errorf("failed to copy file to device: %w", err)
	}
	uploadDuration := time.Since(startUpload)

	// Calculate upload speed
	uploadSpeedMBps := float64(bytesUploaded) / (1024 * 1024) / uploadDuration.Seconds()
	uploadSpeedMbps := uploadSpeedMBps * 8 // Convert to megabits per second

	fmt.Printf("\n✓ File uploaded successfully\n")
	fmt.Printf("Upload completed in %v\n", uploadDuration)
	fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)

	// Step 4: Download Speed Test - Copy file back from device
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
	downloadFilePath := filepath.Join(currentDir, downloadFileName)
	fmt.Printf("Step 4: Download Speed Test - Copying file from device...\n")
	fmt.Printf("Source: %s\n", deviceFileName)
	fmt.Printf("Target: %s\n", downloadFilePath)

	startDownload := time.Now()
	bytesDownloaded, err := copyFileWithProgress(deviceFileName, downloadFilePath)
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

	fmt.Printf("\n✓ File downloaded successfully\n")
	fmt.Printf("Download completed in %v\n", downloadDuration)
	fmt.Printf("Download Speed: %.2f MB/s (%.2f Mbps)\n\n", downloadSpeedMBps, downloadSpeedMbps)

	// Step 5: Clean up files
	fmt.Printf("Step 5: Cleaning up test files...\n")

	// Remove original local file
	if err := os.Remove(localFilePath); err != nil {
		fmt.Printf("⚠ Warning: Could not remove original local file: %v\n", err)
	} else {
		fmt.Printf("✓ Original local test file removed\n")
	}

	// Remove downloaded file
	if err := os.Remove(downloadFilePath); err != nil {
		fmt.Printf("⚠ Warning: Could not remove downloaded file: %v\n", err)
	} else {
		fmt.Printf("✓ Downloaded test file removed\n")
	}

	// Remove device file (unless noDelete flag is set)
	if noDelete {
		fmt.Printf("✓ Device test file kept: %s\n", deviceFileName)
	} else {
		if err := os.Remove(deviceFileName); err != nil {
			fmt.Printf("⚠ Warning: Could not remove device file: %v\n", err)
		} else {
			fmt.Printf("✓ Device test file removed\n")
		}
	}

	fmt.Printf("\nSpeed Test Summary:\n")
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Upload time: %v, Speed: %.2f MB/s (%.2f Mbps)\n", uploadDuration, uploadSpeedMBps, uploadSpeedMbps)
	fmt.Printf("Download time: %v, Speed: %.2f MB/s (%.2f Mbps)\n", downloadDuration, downloadSpeedMBps, downloadSpeedMbps)

	return nil
}
