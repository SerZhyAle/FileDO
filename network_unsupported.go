//go:build !windows

package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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

func runNetworkSpeedTest(networkPath, sizeMBStr string, noDelete bool) error {
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
	fmt.Printf("Step 1: Checking network accessibility...\n")
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
	err = createRandomFile(localFileName, sizeMB)
	if err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}
	createDuration := time.Since(startCreate)
	fmt.Printf("✓ Test file created in %v\n\n", createDuration)

	// Step 3: Copy file to network location
	networkFileName := filepath.Join(networkPath, localFileName)
	fmt.Printf("Step 3: Copying file to network location...\n")
	fmt.Printf("Source: %s\n", localFileName)
	fmt.Printf("Target: %s\n", networkFileName)

	startCopy := time.Now()
	bytesCopied, err := copyFileWithProgress(localFileName, networkFileName)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFileName)
		return fmt.Errorf("failed to copy file to network: %w", err)
	}
	copyDuration := time.Since(startCopy)

	// Calculate speed
	speedMBps := float64(bytesCopied) / (1024 * 1024) / copyDuration.Seconds()
	speedMbps := speedMBps * 8 // Convert to megabits per second

	fmt.Printf("\n✓ File copied successfully\n")
	fmt.Printf("Transfer completed in %v\n", copyDuration)
	fmt.Printf("Speed: %.2f MB/s (%.2f Mbps)\n\n", speedMBps, speedMbps)

	// Step 4: Clean up files
	fmt.Printf("Step 4: Cleaning up test files...\n")

	// Remove local file
	if err := os.Remove(localFileName); err != nil {
		fmt.Printf("⚠ Warning: Could not remove local file: %v\n", err)
	} else {
		fmt.Printf("✓ Local test file removed\n")
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
	fmt.Printf("Transfer time: %v\n", copyDuration)
	fmt.Printf("Average speed: %.2f MB/s (%.2f Mbps)\n", speedMBps, speedMbps)

	return nil
}

func parseSize(sizeStr string) (int, error) {
	var size int
	var err error

	sizeStr = strings.TrimSpace(strings.ToLower(sizeStr))

	// Handle suffixes
	if strings.HasSuffix(sizeStr, "mb") || strings.HasSuffix(sizeStr, "m") {
		sizeStr = strings.TrimSuffix(sizeStr, "mb")
		sizeStr = strings.TrimSuffix(sizeStr, "m")
	}

	size, err = strconv.Atoi(sizeStr)
	if err != nil {
		return 0, err
	}

	return size, nil
}

func createRandomFile(fileName string, sizeMB int) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	sizeBytes := int64(sizeMB) * 1024 * 1024

	// Create a 1MB pattern block once
	const blockSizeMB = 1
	const blockSizeBytes = blockSizeMB * 1024 * 1024

	// Generate the base pattern for 1MB block (without the number prefix)
	basePattern := generateBasePattern(blockSizeBytes - 50) // Reserve 50 bytes for block number prefix

	written := int64(0)
	blockNumber := 1

	for written < sizeBytes {
		remaining := sizeBytes - written
		blockSize := int64(blockSizeBytes)
		if remaining < blockSize {
			blockSize = remaining
		}

		// Create block with number prefix
		blockData := createNumberedBlock(blockNumber, basePattern, int(blockSize))

		n, err := file.Write(blockData)
		if err != nil {
			return err
		}
		written += int64(n)
		blockNumber++

		// Show progress for large files
		if sizeMB >= 10 && written%(1024*1024*10) == 0 { // Every 10MB
			progress := float64(written) / float64(sizeBytes) * 100
			fmt.Printf("  Creating file: %.1f%%\r", progress)
		}
	}

	if sizeMB >= 10 {
		fmt.Printf("  Creating file: 100.0%%\n")
	}

	return nil
}

func generateBasePattern(size int) []byte {
	// Create readable text pattern that will be reused
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .,!?\n"
	const lineLength = 80 // Create lines of 80 characters

	pattern := make([]byte, size)
	charIndex := 0

	for i := 0; i < size; i++ {
		if i > 0 && i%lineLength == 0 {
			pattern[i] = '\n'
		} else {
			pattern[i] = charset[charIndex%len(charset)]
			charIndex++
		}
	}

	return pattern
}

func createNumberedBlock(blockNum int, basePattern []byte, targetSize int) []byte {
	// Create block header with block number
	header := fmt.Sprintf("=== BLOCK %06d === START ===\n", blockNum)
	footer := fmt.Sprintf("\n=== BLOCK %06d === END ===\n", blockNum)

	headerBytes := []byte(header)
	footerBytes := []byte(footer)

	// Calculate how much space we need for the pattern
	patternSize := targetSize - len(headerBytes) - len(footerBytes)
	if patternSize <= 0 {
		// If block is too small, just return the header truncated to fit
		if targetSize <= len(headerBytes) {
			return headerBytes[:targetSize]
		}
		return append(headerBytes, footerBytes[:targetSize-len(headerBytes)]...)
	}

	// Create the block
	block := make([]byte, 0, targetSize)
	block = append(block, headerBytes...)

	// Fill with pattern, repeating as necessary
	patternPos := 0
	for len(block) < targetSize-len(footerBytes) {
		if patternPos >= len(basePattern) {
			patternPos = 0
		}
		block = append(block, basePattern[patternPos])
		patternPos++
	}

	// Add footer
	block = append(block, footerBytes...)

	// Ensure exact size
	if len(block) > targetSize {
		block = block[:targetSize]
	}

	return block
}

func copyFileWithProgress(src, dst string) (int64, error) {
	sourceFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return 0, err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destFile.Close()

	totalSize := sourceInfo.Size()
	buffer := make([]byte, 64*1024) // 64KB buffer
	var totalCopied int64

	fmt.Printf("  Progress: 0.0%%")

	for {
		n, err := sourceFile.Read(buffer)
		if n > 0 {
			written, writeErr := destFile.Write(buffer[:n])
			if writeErr != nil {
				return totalCopied, writeErr
			}
			totalCopied += int64(written)

			// Show progress
			progress := float64(totalCopied) / float64(totalSize) * 100
			fmt.Printf("\r  Progress: %.1f%%", progress)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return totalCopied, err
		}
	}

	fmt.Printf("\r  Progress: 100.0%%")
	return totalCopied, nil
}
