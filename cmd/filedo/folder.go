//go:build windows

package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	return runSpeedTest("Folder", folderPath, sizeMBStr, noDelete, shortFormat, nil)
}

// runFolderFill fills the folder's volume through the folder, with the same
// writer the device fill uses - the files `fill verify` reads (SP-0026 CAP-02).
func runFolderFill(folderPath, sizeMBStr string, autoDelete bool) error {
	return runCapacityFill("Folder", folderPath, sizeMBStr, autoDelete, nil)
}

func runFolderFillClean(folderPath string, assumeYes bool) error {
	return runCapacityClean(folderPath, assumeYes, nil)
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

// GetAvailableSpace proves the folder takes a write, then returns the free
// space of its volume (SP-0026 CAP-04: a folder this user cannot write is
// "could not verify", not a fake).
func (ft *FolderTester) GetAvailableSpace() (int64, error) {
	if err := probeWritable(ft.folderPath); err != nil {
		return 0, err
	}
	free, _, err := capacityFreeSpace(ft.folderPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk space: %w", err)
	}
	return free, nil
}

func (ft *FolderTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (ft *FolderTester) GetCleanupCommand() string {
	return cleanupHint(ft.folderPath)
}

func (ft *FolderTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (string, error) {
	return createTesterFile(ctx, ft.folderPath, fileName, fileSize)
}

// runFolderTest now uses the generic test function
func runFolderTest(folderPath string, autoDelete bool, maxFiles int) error {
	tester := NewFolderTester(folderPath)
	_, err := runGenericFakeCapacityTest(tester, autoDelete, maxFiles, nil)
	return err
}
