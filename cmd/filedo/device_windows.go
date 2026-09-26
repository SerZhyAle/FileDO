//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
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
		totals, walkErr := walkInfoTree(rootPath, false)
		if errors.Is(walkErr, errRunStopped) {
			return DeviceInfo{}, walkErr
		}
		fileCount, folderCount, accessErrors = totals.files, totals.folders, totals.accessErrors
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

// normalizeDevicePath turns `E:` into `E:\`; anything else is unchanged.
func normalizeDevicePath(devicePath string) string {
	if len(devicePath) == 2 && devicePath[1] == ':' {
		return devicePath + `\`
	}
	return devicePath
}

func runDeviceSpeedTest(devicePath, sizeMBStr string, noDelete, shortFormat bool) error {
	return runSpeedTest("Device", normalizeDevicePath(devicePath), sizeMBStr, noDelete, shortFormat, nil)
}

func runDeviceFill(devicePath, sizeMBStr string, autoDelete bool) error {
	return runCapacityFill("Device", normalizeDevicePath(devicePath), sizeMBStr, autoDelete, nil)
}

func runDeviceFillClean(devicePath string, assumeYes bool) error {
	return runCapacityClean(normalizeDevicePath(devicePath), assumeYes, nil)
}

// runDeviceFillVerify verifies FILL test files to detect fake/counterfeit
// storage. Folder and network targets use the same verify (capacity_fill_windows.go).
func runDeviceFillVerify(devicePath string) error {
	return runCapacityFillVerify("Device", normalizeDevicePath(devicePath))
}

// DeviceTester implements FakeCapacityTester for device testing
type DeviceTester struct {
	devicePath string
}

// NewDeviceTester creates a new device tester
func NewDeviceTester(devicePath string) *DeviceTester {
	return &DeviceTester{devicePath: normalizeDevicePath(devicePath)}
}

func (dt *DeviceTester) GetTestInfo() (string, string) {
	return "Device", dt.devicePath
}

func (dt *DeviceTester) GetAvailableSpace() (int64, error) {
	if _, err := os.Stat(dt.devicePath); err != nil {
		return 0, fmt.Errorf("device path is not accessible: %w", err)
	}
	if err := probeWritable(dt.devicePath); err != nil {
		return 0, err
	}
	free, _, err := capacityFreeSpace(dt.devicePath)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk space information: %w", err)
	}
	return free, nil
}

func (dt *DeviceTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (string, error) {
	return createTesterFile(ctx, dt.devicePath, fileName, fileSize)
}

func (dt *DeviceTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (dt *DeviceTester) GetCleanupCommand() string {
	return cleanupHint(dt.devicePath)
}

// runDeviceTest now uses the generic test function
func runDeviceTest(devicePath string, autoDelete bool, maxFiles int) error {
	tester := NewDeviceTester(devicePath)
	_, err := runGenericFakeCapacityTest(tester, autoDelete, maxFiles, nil)
	return err
}

// getEnhancedDeviceInfo describes a target for a header line: the path, the
// volume's label and its size. It only reads - it used to go through
// getDeviceInfo, which lists the volume root and writes a probe file there.
func getEnhancedDeviceInfo(devicePath string) string {
	label, total, ok := volumeLabelAndSize(devicePath)
	if !ok {
		return devicePath
	}
	if label == "" {
		label = "No label"
	}
	return fmt.Sprintf("%s (%s) [%.1f GB]", devicePath, label, float64(total)/(1024*1024*1024))
}

// volumeLabelAndSize reads the label and the size of the volume holding path.
func volumeLabelAndSize(path string) (label string, total int64, ok bool) {
	root, err := capVolumeRoot(path)
	if err != nil {
		return "", 0, false
	}
	rp, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", 0, false
	}
	var volName [windows.MAX_PATH + 1]uint16
	if err := windows.GetVolumeInformation(rp, &volName[0], uint32(len(volName)), nil, nil, nil, nil, 0); err != nil {
		return "", 0, false
	}
	_, t, err := diskSpaceQuery(root)
	if err != nil {
		return "", 0, false
	}
	return windows.UTF16ToString(volName[:]), clampToInt64(t), true
}
