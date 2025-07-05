//go:build !windows

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func getNetworkInfo(path string, fullScan bool) (NetworkInfo, error) {
	// Normalize the path for Unix-style network paths
	normalizedPath := path
	if !strings.HasPrefix(normalizedPath, "//") && !strings.HasPrefix(normalizedPath, "/") {
		normalizedPath = "//" + normalizedPath
	}

	// Test if the network path exists and is accessible
	canRead := testNetworkRead(normalizedPath)
	canWrite := testNetworkWrite(normalizedPath)

	var size uint64
	var fileCount, folderCount int64
	var accessErrors bool

	if canRead {
		if fullScan {
			size, fileCount, folderCount, accessErrors = scanNetworkPath(normalizedPath)
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

func scanNetworkPath(path string) (uint64, int64, int64, bool) {
	var totalSize uint64
	var fileCount, folderCount int64
	var accessErrors bool

	walkErr := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
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

	if walkErr != nil {
		accessErrors = true
	}

	return totalSize, fileCount, folderCount, accessErrors
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
		"no such host",
		"connection refused",
	}

	for _, netErr := range networkErrors {
		if strings.Contains(strings.ToLower(errStr), netErr) {
			return true
		}
	}

	return false
}

func runNetworkSpeedTest(networkPath, sizeMBStr string, noDelete, shortFormat bool, logger *HistoryLogger) error {
	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		return fmt.Errorf("invalid size '%s': %w", sizeMBStr, err)
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB
		return fmt.Errorf("size must be between 1 and 10240 MB")
	}

	fmt.Printf("Network Speed Test\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("Test file size: %d MB\n\n", sizeMB)

	// Step 1: Check if network address is reachable and writable
	fmt.Printf("Step 1: Checking network accessibility\n")
	canRead := testNetworkRead(networkPath)
	canWrite := testNetworkWrite(networkPath)

	if !canRead {
		return fmt.Errorf("network path is not readable")
	}
	if !canWrite {
		return fmt.Errorf("network path is not writable")
	}
	fmt.Printf("✓ Network path is readable and writable\n\n")

	// Step 2: Create random file
	fmt.Printf("Step 2: Creating test file (%d MB)...\n", sizeMB)
	localFileName := fmt.Sprintf("speedtest_%d_%d.txt", sizeMB, time.Now().Unix())

	startCreate := time.Now()
	err = createRandomFile(localFileName, sizeMB, true)
	if err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}
	createDuration := time.Since(startCreate)
	fmt.Printf("✓ Test file created in %s\n\n", formatDuration(createDuration))

	// Step 3: Upload Speed Test - Copy file to network location
	networkFileName := filepath.Join(networkPath, localFileName)
	fmt.Printf("Step 3: Upload Speed Test - Copying file to network location...\n")
	fmt.Printf("Source: %s\n", localFileName)
	fmt.Printf("Target: %s\n", networkFileName)

	startUpload := time.Now()
	bytesUploaded, err := copyFileWithProgress(localFileName, networkFileName, true)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFileName)
		return fmt.Errorf("failed to copy file to network: %w", err)
	}
	uploadDuration := time.Since(startUpload)

	// Calculate upload speed
	uploadSpeedMBps := float64(bytesUploaded) / (1024 * 1024) / uploadDuration.Seconds()
	uploadSpeedMbps := uploadSpeedMBps * 8 // Convert to megabits per second

	fmt.Printf("\n✓ File uploaded successfully\n")
	fmt.Printf("Upload completed in %s\n", formatDuration(uploadDuration))
	fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)

	// Step 4: Download Speed Test - Copy file back from network location
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
	fmt.Printf("Step 4: Download Speed Test - Copying file from network location...\n")
	fmt.Printf("Source: %s\n", networkFileName)
	fmt.Printf("Target: %s\n", downloadFileName)

	startDownload := time.Now()
	bytesDownloaded, err := copyFileWithProgress(networkFileName, downloadFileName, true)
	if err != nil {
		// Clean up files before returning error
		os.Remove(localFileName)
		os.Remove(networkFileName)
		return fmt.Errorf("failed to copy file from network: %w", err)
	}
	downloadDuration := time.Since(startDownload)

	// Calculate download speed
	downloadSpeedMBps := float64(bytesDownloaded) / (1024 * 1024) / downloadDuration.Seconds()
	downloadSpeedMbps := downloadSpeedMBps * 8 // Convert to megabits per second

	fmt.Printf("\n✓ File downloaded successfully\n")
	fmt.Printf("Download completed in %s\n", formatDuration(downloadDuration))
	fmt.Printf("Download Speed: %.2f MB/s (%.2f Mbps)\n\n", downloadSpeedMBps, downloadSpeedMbps)

	// Step 5: Clean up files
	fmt.Printf("Step 5: Cleaning up test files...\n")

	// Remove original local file
	if err := os.Remove(localFileName); err != nil {
		fmt.Printf("⚠ Warning: Could not remove original local file: %v\n", err)
	} else {
		fmt.Printf("✓ Original local test file removed\n")
	}

	// Remove downloaded file
	if err := os.Remove(downloadFileName); err != nil {
		fmt.Printf("⚠ Warning: Could not remove downloaded file: %v\n", err)
	} else {
		fmt.Printf("✓ Downloaded test file removed\n")
	}

	// Remove network file (unless noDelete flag is set)
	if noDelete {
		fmt.Printf("✓ Network test file kept: %s\n", networkFileName)
	} else {
		if err := os.Remove(networkFileName); err != nil {
			fmt.Printf("⚠ Warning: Could not remove network file: %v\n", err)
		} else {
			fmt.Printf("✓ Network test file removed\n")
		}
	}

	fmt.Printf("\nSpeed Test Summary:\n")
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Upload time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(uploadDuration), uploadSpeedMBps, uploadSpeedMbps)
	fmt.Printf("Download time: %s, Speed: %.2f MB/s (%.2f Mbps)\n", formatDuration(downloadDuration), downloadSpeedMBps, downloadSpeedMbps)

	return nil
}

func runNetworkFill(networkPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	return fmt.Errorf("network fill operation is not supported on this operating system")
}

func runNetworkFillClean(networkPath string, logger *HistoryLogger) error {
	return fmt.Errorf("network fill clean operation is not supported on this operating system")
}

func runNetworkTest(networkPath string, autoDelete bool, logger *HistoryLogger) error {
	return fmt.Errorf("network test operation is not supported on this operating system")
}
