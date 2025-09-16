package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Network-specific fill operations (adapted for network paths)

func runNetworkFill(networkPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	// Setup interrupt handler
	handler := NewInterruptHandler()
	templateFilePath := ""

	// Add cleanup for template file
	handler.AddCleanup(func() {
		if templateFilePath != "" {
			os.Remove(templateFilePath)
			if logger != nil {
				logger.LogAction("Cleanup", "Template file removed: "+templateFilePath)
			}
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

	if logger != nil {
		logger.LogAction("NetworkFill", fmt.Sprintf("Started: %s, Size: %dMB, AutoDelete: %v", networkPath, sizeMB, autoDelete))
	}

	fmt.Printf("Network Fill Operation\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Check if network path exists and is accessible
	stat, err := os.Stat(networkPath)
	if err != nil {
		errorMsg := fmt.Sprintf("Network path error: %v", err)
		if logger != nil {
			logger.LogAction("NetworkFill", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}
	if !stat.IsDir() {
		errorMsg := fmt.Sprintf("Path is not a directory: %s", networkPath)
		if logger != nil {
			logger.LogAction("NetworkFill", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}

	// Test write access
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(networkPath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		errorMsg := fmt.Sprintf("Network path is not writable: %v", err)
		if logger != nil {
			logger.LogAction("NetworkFill", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	// For network paths, create fewer files to avoid overwhelming the network
	maxFiles := int64(100) // Create up to 100 files for network
	fileSizeBytes := int64(sizeMB) * 1024 * 1024

	// Get timestamp for file naming (ddHHmmss format)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Create a template file first (optimization from main project)
	templateFileName := fmt.Sprintf("__template_%d.tmp", time.Now().UnixNano())
	templateFilePath = filepath.Join(networkPath, templateFileName)
	
	err = writeTestFileWithBuffer(templateFilePath, fileSizeBytes, 16*1024*1024) // Smaller buffer for network
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to create template file: %v", err)
		if logger != nil {
			logger.LogAction("NetworkFill", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}

	if logger != nil {
		logger.LogAction("NetworkFill", fmt.Sprintf("Template created: %s (%d bytes)", templateFilePath, fileSizeBytes))
	}

	fmt.Printf("Starting fill operation...\n")
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSizeBytes, 3*time.Second) // Slower updates for network
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	for i := int64(1); i <= maxFiles; i++ {
		// Check for interruption
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			if logger != nil {
				logger.LogAction("NetworkFill", "Operation cancelled by user")
			}
			break
		}

		// Generate file name: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(networkPath, fileName)

		// Copy template file to target (optimized for network)
		bytesCopied, err := copyFileOptimized(templateFilePath, targetFilePath)
		if err != nil {
			fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", i, err)
			if logger != nil {
				logger.LogAction("NetworkFill", fmt.Sprintf("WARNING: Failed to create file %d: %v", i, err))
			}
			break
		}

		filesCreated++
		totalBytesWritten += bytesCopied
		progress.Update(filesCreated, totalBytesWritten)
		progress.PrintProgress("Fill")

		// Log every 10 files for network operations
		if logger != nil && i%10 == 0 {
			logger.LogAction("NetworkFill", fmt.Sprintf("Progress: %d files created, %.2f MB written", filesCreated, float64(totalBytesWritten)/(1024*1024)))
		}
	}

	// Clean up template file
	os.Remove(templateFilePath)

	// Final summary
	progress.Finish("Network Fill Operation")

	if logger != nil {
		logger.LogAction("NetworkFill", fmt.Sprintf("Completed: %d files created, %.2f MB total", filesCreated, float64(totalBytesWritten)/(1024*1024)))
	}

	// Auto-delete if requested
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files...\n")
		if logger != nil {
			logger.LogAction("NetworkFillClean", "Auto-delete started")
		}

		// Find all FILL_*.tmp files in the network path
		pattern := filepath.Join(networkPath, "FILL_*.tmp")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Printf("⚠ Warning: Failed to search for files to delete: %v\n", err)
			if logger != nil {
				logger.LogAction("NetworkFillClean", fmt.Sprintf("WARNING: Failed to search for files: %v", err))
			}
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
						if logger != nil {
							logger.LogAction("NetworkFillClean", fmt.Sprintf("WARNING: Failed to delete file: %v", err))
						}
					} else {
						deletedCount++
						deletedSize += fileSize
					}
				}

				// Show progress every 5 files for network
				if (i+1)%5 == 0 || i == len(matches)-1 {
					fmt.Printf("Deleted %d/%d files\r", deletedCount, len(matches))
				}
			}

			fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", 
				deletedCount, float64(deletedSize)/(1024*1024*1024))
			
			if logger != nil {
				logger.LogAction("NetworkFillClean", fmt.Sprintf("Auto-delete completed: %d files deleted, %.2f GB freed", deletedCount, float64(deletedSize)/(1024*1024*1024)))
			}
		}
	}

	return nil
}

func runNetworkFillClean(networkPath string, logger *HistoryLogger) error {
	if logger != nil {
		logger.LogAction("NetworkFillClean", "Started: "+networkPath)
	}

	fmt.Printf("Network Clean Operation\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)...\n\n")

	// Check if network path exists and is accessible
	if stat, err := os.Stat(networkPath); err != nil {
		errorMsg := fmt.Sprintf("Network path error: %v", err)
		if logger != nil {
			logger.LogAction("NetworkFillClean", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	} else if !stat.IsDir() {
		errorMsg := fmt.Sprintf("Path is not a directory: %s", networkPath)
		if logger != nil {
			logger.LogAction("NetworkFillClean", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}

	// Find all FILL_*.tmp files
	fillPattern := filepath.Join(networkPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to search for FILL files: %v", err)
		if logger != nil {
			logger.LogAction("NetworkFillClean", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}

	// Find all speedtest_*.txt files
	speedtestPattern := filepath.Join(networkPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		errorMsg := fmt.Sprintf("Failed to search for speedtest files: %v", err)
		if logger != nil {
			logger.LogAction("NetworkFillClean", "ERROR: "+errorMsg)
		}
		return fmt.Errorf(errorMsg)
	}

	// Combine all matches
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", networkPath)
		fmt.Printf("Searched for: FILL_*.tmp and speedtest_*.txt\n")
		if logger != nil {
			logger.LogAction("NetworkFillClean", "No test files found")
		}
		return nil
	}

	fmt.Printf("Found %d test files:\n", len(allMatches))
	fmt.Printf("  FILL files: %d\n", len(fillMatches))
	fmt.Printf("  Speedtest files: %d\n", len(speedtestMatches))

	if logger != nil {
		logger.LogAction("NetworkFillClean", fmt.Sprintf("Found %d test files (%d FILL, %d speedtest)", len(allMatches), len(fillMatches), len(speedtestMatches)))
	}

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
				if logger != nil {
					logger.LogAction("NetworkFillClean", fmt.Sprintf("WARNING: Failed to delete %s: %v", filepath.Base(filePath), err))
				}
			} else {
				deletedCount++
				deletedSize += fileSize
			}
		}

		// Show progress every 5 files for network
		if (i+1)%5 == 0 || i == len(allMatches)-1 {
			fmt.Printf("Processed %d/%d files\r", i+1, len(allMatches))
		}
	}

	fmt.Printf("\n\nNetwork Clean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deletedCount, len(allMatches))
	fmt.Printf("Space freed: %.2f GB\n", float64(deletedSize)/(1024*1024*1024))

	if logger != nil {
		logger.LogAction("NetworkFillClean", fmt.Sprintf("Completed: %d files deleted, %.2f GB freed", deletedCount, float64(deletedSize)/(1024*1024*1024)))
	}

	if deletedCount < len(allMatches) {
		fmt.Printf("Warning: %d files could not be deleted\n", len(allMatches)-deletedCount)
	}

	return nil
}

// copyFileOptimized performs optimized file copying for network operations
func copyFileOptimized(srcPath, dstPath string) (int64, error) {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	// Use smaller buffer for network operations
	buffer := make([]byte, 16*1024*1024) // 16MB buffer
	var totalBytes int64

	for {
		n, err := srcFile.Read(buffer)
		if n > 0 {
			_, writeErr := dstFile.Write(buffer[:n])
			if writeErr != nil {
				return totalBytes, writeErr
			}
			totalBytes += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return totalBytes, err
		}
	}

	return totalBytes, nil
}
