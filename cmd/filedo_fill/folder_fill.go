package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Folder-specific fill operations

func runFolderFill(folderPath, sizeMBStr string, autoDelete bool) error {
	// Setup interrupt handler
	handler := NewInterruptHandler()
	templateFilePath := ""

	// Add cleanup for template file
	handler.AddCleanup(func() {
		if templateFilePath != "" {
			os.Remove(templateFilePath)
			fmt.Printf("✓ Template file cleaned up\n")
		}
	})

	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 100 // Default to 100 MB if parsing fails
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB per file
		sizeMB = 100 // Default to 100 MB if out of range
	}

	fmt.Printf("Folder Fill Operation\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Check if folder exists and is accessible
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path error: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Test write access
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(folderPath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("folder is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	// For folders, we'll create a reasonable number of files rather than fill to capacity
	// This is safer and more predictable
	maxFiles := int64(1000) // Create up to 1000 files
	fileSizeBytes := int64(sizeMB) * 1024 * 1024

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Create a template file first (optimization from main project)
	templateFileName := fmt.Sprintf("__template_%d.tmp", time.Now().UnixNano())
	templateFilePath = filepath.Join(folderPath, templateFileName)
	
	err = writeTestFileWithBuffer(templateFilePath, fileSizeBytes, 64*1024*1024)
	if err != nil {
		return fmt.Errorf("failed to create template file: %w", err)
	}

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
		targetFilePath := filepath.Join(folderPath, fileName)

		// Copy template file to target
		bytesCopied, err := copyFileOptimized(templateFilePath, targetFilePath)
		if err != nil {
			fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", i, err)
			break
		}

		filesCreated++
		totalBytesWritten += bytesCopied
		progress.Update(filesCreated, totalBytesWritten)
		progress.PrintProgress("Fill")
	}

	// Clean up template file
	os.Remove(templateFilePath)

	// Final summary
	progress.Finish("Fill Operation")

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files...\n")

		// Find all FILL_*.tmp files in the folder
		pattern := filepath.Join(folderPath, "FILL_*.tmp")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Printf("⚠ Warning: Failed to search for files to delete: %v\n", err)
		} else if len(matches) > 0 {
			deletedCount := 0
			deletedSize := int64(0)

			for i, filePath := range matches {
				info, err := os.Stat(filePath)
				if err == nil {
					fileSize := info.Size()
					err = os.Remove(filePath)
					if err != nil {
						fmt.Printf("⚠ Warning: Failed to delete file %d: %v\n", i+1, err)
					} else {
						deletedCount++
						deletedSize += fileSize
					}
				}

				// Show progress every 10 files
				if (i+1)%10 == 0 || i == len(matches)-1 {
					fmt.Printf("Deleted %d/%d files\r", deletedCount, len(matches))
				}
			}

			fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", 
				deletedCount, float64(deletedSize)/(1024*1024*1024))
		}
	}

	return nil
}

func runFolderFillClean(folderPath string) error {
	fmt.Printf("Folder Clean Operation\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)...\n\n")

	// Check if folder exists and is accessible
	if stat, err := os.Stat(folderPath); err != nil {
		return fmt.Errorf("folder path error: %w", err)
	} else if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Find all FILL_*.tmp files
	fillPattern := filepath.Join(folderPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	// Find all speedtest_*.txt files
	speedtestPattern := filepath.Join(folderPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		return fmt.Errorf("failed to search for speedtest files: %w", err)
	}

	// Combine all matches
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", folderPath)
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

	// Delete files one by one with progress
	deletedCount := 0
	deletedSize := int64(0)

	for i, filePath := range allMatches {
		info, err := os.Stat(filePath)
		if err == nil {
			fileSize := info.Size()
			err = os.Remove(filePath)
			if err != nil {
				fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
			} else {
				deletedCount++
				deletedSize += fileSize
			}
		}

		// Show progress every 10 files
		if (i+1)%10 == 0 || i == len(allMatches)-1 {
			fmt.Printf("Processed %d/%d files\r", i+1, len(allMatches))
		}
	}

	fmt.Printf("\n\nClean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deletedCount, len(allMatches))
	fmt.Printf("Space freed: %.2f GB\n", float64(deletedSize)/(1024*1024*1024))

	if deletedCount < len(allMatches) {
		fmt.Printf("Warning: %d files could not be deleted\n", len(allMatches)-deletedCount)
	}

	return nil
}
