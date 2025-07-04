//go:build windows

package main

import (
	"fmt"
	"unicode"
	"io/fs"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func getDeviceInfo(path string) (DeviceInfo, error) {
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
	filepath.WalkDir(rootPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
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

	return DeviceInfo{
		Path: path, VolumeName: windows.UTF16ToString(volName[:]), SerialNumber: serialNumber, FileSystem: windows.UTF16ToString(fsName[:]),
		TotalBytes: totalBytes, FreeBytes: totalFreeBytes, AvailableBytes: freeBytesAvailable,
		FileCount: fileCount, FolderCount: folderCount,
	}, nil
}
