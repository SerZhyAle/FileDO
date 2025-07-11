//go:build !windows

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

func getCreationTime(info fs.FileInfo) time.Time {
	return time.Time{}
}

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
				accessErrors = true
				return nil // Continue walking even if some directories are inaccessible
			}
			if d.IsDir() {
				if p != path {
					folderCount++
				}
			} else {
				fileCount++
				info, err := d.Info()
				if err != nil {
					accessErrors = true
					return nil // Continue walking
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
				if info, err := entry.Info(); err == nil {
					size += uint64(info.Size())
				} else {
					accessErrors = true
				}
			}
		}
	}

	if err != nil && !accessErrors {
		return FolderInfo{}, fmt.Errorf("failed to walk directory '%s': %w", path, err)
	}

	creationTime := getCreationTime(stat)

	canRead := false
	if _, err := os.ReadDir(path); err == nil {
		canRead = true
	}

	canWrite := false
	testFileName := fmt.Sprintf("__filedo_access_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(path, testFileName)
	if testFile, err := os.Create(testFilePath); err == nil {
		testFile.Close()
		os.Remove(testFilePath)
		canWrite = true
	}

	return FolderInfo{
		Path:         path,
		Size:         size,
		FileCount:    fileCount,
		FolderCount:  folderCount,
		ModTime:      stat.ModTime(),
		CreationTime: creationTime,
		Mode:         stat.Mode(),
		FullScan:     fullScan,
		AccessErrors: accessErrors,
		CanRead:      canRead,
		CanWrite:     canWrite,
	}, nil
}
