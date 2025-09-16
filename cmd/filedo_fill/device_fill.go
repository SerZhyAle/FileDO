//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// Device-specific fill operations (Windows implementation)

func runDeviceFill(devicePath, sizeMBStr string, autoDelete bool) error {
	// Setup interrupt handler
	handler := NewInterruptHandler()

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
	fmt.Printf("Target: %s\n", getEnhancedDeviceInfo(normalizedPath))
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

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

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Start filling
	fmt.Printf("Starting fill operation...\n")
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	for i := int64(1); i <= maxFiles; i++ {
		// Check for interruption
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}

		// Generate file name: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(normalizedPath, fileName)

		// Create file directly
		err := writeTestFileWithBuffer(targetFilePath, fileSizeBytes, 64*1024*1024)
		if err != nil {
			if isCriticalError(err) {
				fmt.Printf("\n❌ Critical error on file %d: %v\n", i, err)
				fmt.Printf("Stopping operation to prevent further issues\n")
				break
			} else {
				fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", i, err)
				break
			}
		}

		filesCreated++
		totalBytesWritten += fileSizeBytes

		// Update progress
		if filesCreated%4 == 0 || filesCreated == 1 || progress.ShouldUpdate() {
			progress.Update(filesCreated, totalBytesWritten)
			progress.PrintProgress("Fill")
		}
	}

	// Final summary
	progress.Update(filesCreated, totalBytesWritten)
	progress.Finish("Fill Operation")

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files...\n")

		deletedCount := int64(0)
		deletedSize := int64(0)

		for i := int64(1); i <= filesCreated; i++ {
			fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
			filePath := filepath.Join(normalizedPath, fileName)
			
			if info, err := os.Stat(filePath); err == nil {
				fileSize := info.Size()
				if err := os.Remove(filePath); err == nil {
					deletedCount++
					deletedSize += fileSize
				}
			}
		}

		fmt.Printf("Auto-delete complete: %d files deleted, %.2f GB freed\n", 
			deletedCount, float64(deletedSize)/(1024*1024*1024))
	}

	return nil
}

func runDeviceFillClean(devicePath string) error {
	// Normalize device path
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Clean Operation\n")
	fmt.Printf("Target: %s\n", getEnhancedDeviceInfo(normalizedPath))
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)...\n\n")

	// Check if device is accessible
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Find all FILL_*.tmp files
	fillPattern := filepath.Join(normalizedPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	// Find all speedtest_*.txt files
	speedtestPattern := filepath.Join(normalizedPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		return fmt.Errorf("failed to search for speedtest files: %w", err)
	}

	// Combine all matches
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", normalizedPath)
		fmt.Printf("Searched for: FILL_*.tmp and speedtest_*.txt\n")
		return nil
	}

	fmt.Printf("Found %d test files:\n", len(allMatches))
	fmt.Printf("  FILL files: %d\n", len(fillMatches))
	fmt.Printf("  Speedtest files: %d\n", len(speedtestMatches))

	// Calculate total size before deletion
	var totalSize int64
	for _, filePath := range allMatches {
		if info, err := os.Stat(filePath); err == nil {
			totalSize += info.Size()
		}
	}

	fmt.Printf("Total size to delete: %.2f GB\n", float64(totalSize)/(1024*1024*1024))
	fmt.Printf("Deleting files...\n\n")

	// Delete files using worker pool for parallel deletion
	var deletedCount int64
	var deletedSize int64
	deletionWorkers := 24
	deletionJobs := make(chan string, len(allMatches))
	var wg sync.WaitGroup

	// Start deletion workers
	for w := 0; w < deletionWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range deletionJobs {
				info, err := os.Stat(filePath)
				if err == nil {
					fileSize := info.Size()
					err = os.Remove(filePath)
					if err != nil {
						fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
					} else {
						atomic.AddInt64(&deletedCount, 1)
						atomic.AddInt64(&deletedSize, fileSize)
					}
				}
			}
		}()
	}

	// Queue all files for deletion
	for _, filePath := range allMatches {
		deletionJobs <- filePath
	}
	close(deletionJobs)

	// Wait for completion
	wg.Wait()

	fmt.Printf("\nClean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deletedCount, len(allMatches))
	fmt.Printf("Space freed: %.2f GB\n", float64(deletedSize)/(1024*1024*1024))

	if deletedCount < int64(len(allMatches)) {
		fmt.Printf("Warning: %d files could not be deleted\n", int64(len(allMatches))-deletedCount)
	}

	return nil
}

// Helper function to get enhanced device info
func getEnhancedDeviceInfo(devicePath string) string {
	// Simple implementation - just return the device path
	// In full implementation, this would query device information
	return devicePath
}
