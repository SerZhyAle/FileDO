//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NetworkTester implements FakeCapacityTester for network paths. It writes
// with the same writer as the device and folder testers - the one the
// verifier was written for. It used to write through a separate package
// whose files lacked the body tag, so `test` failed every healthy share at
// file 1, and calibrated a buffer before every file inside the timed window
// (SP-0026 CAP-01, CAP-16).
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

// GetAvailableSpace proves the share takes a write and returns its free
// space. A read-only share, or a free-space query that fails, is an error -
// it used to be taken for 1 GB free and a PASS said so as if measured
// (SP-0026 CAP-04, CAP-19).
func (t *NetworkTester) GetAvailableSpace() (int64, error) {
	if err := probeWritable(t.networkPath); err != nil {
		return 0, err
	}
	free, _, err := capacityFreeSpace(t.networkPath)
	if err != nil {
		return 0, fmt.Errorf("could not read the free space of %s: %w", t.networkPath, err)
	}
	return free, nil
}

// CreateTestFileContext writes one test file - exactly one, with no
// calibration files beside it.
func (t *NetworkTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error) {
	return createTesterFile(ctx, t.networkPath, fileName, fileSize)
}

// CleanupTestFile removes a test file
func (t *NetworkTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

// GetCleanupCommand returns the command to clean test files manually
func (t *NetworkTester) GetCleanupCommand() string {
	return cleanupHint(t.networkPath)
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
			var err error
			size, fileCount, folderCount, accessErrors, err = scanNetworkPath(normalizedPath)
			if err != nil {
				return NetworkInfo{}, err
			}
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

// scanNetworkPath walks the share for `info`. Its error is only the stop
// (errRunStopped); every other walk error is folded into accessErrors.
func scanNetworkPath(path string) (uint64, int64, int64, bool, error) {
	var totalSize uint64
	var fileCount, folderCount int64
	var accessErrors bool

	walkErr := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if infoWalkStopped() {
			return errRunStopped
		}
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

	if errors.Is(walkErr, errRunStopped) {
		return 0, 0, 0, false, walkErr
	}
	if walkErr != nil {
		accessErrors = true
	}

	return totalSize, fileCount, folderCount, accessErrors, nil
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
	return runSpeedTest("Network", networkPath, sizeMBStr, noDelete, shortFormat, logger)
}

// runNetworkFill fills the share with the same writer as every other fill,
// sized from the share's free space (SP-0026 CAP-02, CAP-19).
func runNetworkFill(networkPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	return runCapacityFill("Network", networkPath, sizeMBStr, autoDelete, logger)
}

func runNetworkFillClean(networkPath string, assumeYes bool, logger *HistoryLogger) error {
	if logger != nil {
		logger.SetCommand("network", networkPath, "clean")
	}
	err := runCapacityClean(networkPath, assumeYes, logger)
	if err != nil && logger != nil {
		logger.SetError(err)
	}
	return err
}

func runNetworkTest(networkPath string, autoDelete bool, maxFiles int, logger *HistoryLogger) error {
	tester := NewNetworkTester(networkPath)
	_, err := runGenericFakeCapacityTest(tester, autoDelete, maxFiles, logger)
	return err
}
