//go:build windows

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
		// fmt.Printf("WMI Error (LogicalDiskToPartition): %v\n", err) // For debugging
		return
	}

	if len(logicalDiskToPartitions) == 0 {
		return
	}

	// The Antecedent contains the path to Win32_DiskPartition, e.g., "Win32_DiskPartition.DeviceID='Disk #0, Partition #0'"
	partitionPath := logicalDiskToPartitions[0].Antecedent

	// 2. Find the physical disk associated with the partition
	var diskDriveToPartitions []Win32_DiskDriveToDiskPartition
	query = fmt.Sprintf("SELECT Antecedent, Dependent FROM Win32_DiskDriveToDiskPartition WHERE Dependent = \"%s\"", partitionPath)
	err = wmi.Query(query, &diskDriveToPartitions)
	if err != nil {
		// fmt.Printf("WMI Error (DiskDriveToDiskPartition): %v\n", err) // For debugging
		return
	}

	if len(diskDriveToPartitions) == 0 {
		return
	}

	// The Antecedent contains the path to Win32_DiskDrive, e.g., "Win32_DiskDrive.DeviceID='\\\\.\\PHYSICALDRIVE0'"
	diskDrivePath := diskDriveToPartitions[0].Antecedent

	// Extract DeviceID from the path, e.g., 'Win32_DiskDrive.DeviceID="\\\\.\\PHYSICALDRIVE0"'
	// This is a bit fragile, but common for WMI association queries.
	// A more robust way would be to parse the string or query Win32_DiskDrive directly with LIKE.
	start := strings.Index(diskDrivePath, "DeviceID=\"")
	if start == -1 {
		return
	}
	start += len("DeviceID=\"")
	end := strings.LastIndex(diskDrivePath, "\"")
	if end == -1 || end <= start {
		return
	}
	physicalDiskDeviceID := diskDrivePath[start:end]

	// 3. Get details of the physical disk
	var diskDrives []Win32_DiskDrive
	query = fmt.Sprintf("SELECT Model, SerialNumber, InterfaceType FROM Win32_DiskDrive WHERE DeviceID = \"%s\"", physicalDiskDeviceID)
	err = wmi.Query(query, &diskDrives)
	if err != nil {
		// fmt.Printf("WMI Error (DiskDrive): %v\n", err) // For debugging
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
					return nil // Подавить ошибку прав доступа и продолжить
				}
				return err // Для других ошибок остановить обход
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

	return DeviceInfo{
		Path: path, VolumeName: windows.UTF16ToString(volName[:]), SerialNumber: serialNumber, FileSystem: windows.UTF16ToString(fsName[:]),
		TotalBytes: totalBytes, FreeBytes: totalFreeBytes, AvailableBytes: freeBytesAvailable,
		FileCount: fileCount, FolderCount: folderCount, FullScan: fullScan, AccessErrors: accessErrors,
		DiskModel: diskModel, DiskSerialNumber: diskSerialNumber, DiskInterface: diskInterface,
	}, nil
}
