package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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
				if os.IsPermission(err) {
					accessErrors = true
					return nil // Подавить ошибку прав доступа и продолжить
				}
				return err // Распространить другие ошибки
			}
			if d.IsDir() {
				if p != path {
					folderCount++
				}
			} else {
				fileCount++
				info, err := d.Info()
				if err != nil {
					if os.IsPermission(err) {
						accessErrors = true
						return nil // Подавить ошибку прав доступа и продолжить
					}
					return err // Распространить другие ошибки
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
			}
			info, err := entry.Info()
			if err == nil {
				size += uint64(info.Size())
			}
		}
	}

	if err != nil {
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

func runFolderSpeedTest(folderPath, sizeMBStr string, noDelete bool) error {
	// Parse size
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 1 // Default to 1 MB if parsing fails
		//return fmt.Errorf("invalid size '%s': %w", sizeMBStr, err)
	}

	if sizeMB < 1 || sizeMB > 10240 { // Limit to 10GB
		sizeMB = 1 // Default to 1 MB if out of range
		//return fmt.Errorf("size must be between 1 and 10240 MB")
	}

	fmt.Printf("Folder Speed Test\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("Test file size: %d MB\n\n", sizeMB)

	// Step 1: Check if folder is accessible and writable
	fmt.Printf("Step 1: Checking folder accessibility...\n")

	// Check if folder exists and is accessible
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path is not accessible: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Test write access by creating a temporary file
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(folderPath, testFileName)

	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("folder path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Clean up test file

	fmt.Printf("✓ Folder is accessible and writable\n\n")

	// Step 2: Create test file in current directory
	fmt.Printf("Step 2: Creating test file (%d MB)...\n", sizeMB)
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	localFileName := fmt.Sprintf("speedtest_%d_%d.txt", sizeMB, time.Now().Unix())
	localFilePath := filepath.Join(currentDir, localFileName)

	startCreate := time.Now()
	err = createRandomFile(localFilePath, sizeMB)
	if err != nil {
		return fmt.Errorf("failed to create test file: %w", err)
	}
	createDuration := time.Since(startCreate)
	fmt.Printf("✓ Test file created in %v\n\n", createDuration)

	// Step 3: Upload Speed Test - Copy file to folder
	folderFileName := filepath.Join(folderPath, localFileName)
	fmt.Printf("Step 3: Upload Speed Test - Copying file to folder...\n")
	fmt.Printf("Source: %s\n", localFilePath)
	fmt.Printf("Target: %s\n", folderFileName)

	startUpload := time.Now()
	bytesUploaded, err := copyFileWithProgress(localFilePath, folderFileName)
	if err != nil {
		// Clean up local file before returning error
		os.Remove(localFilePath)
		return fmt.Errorf("failed to copy file to folder: %w", err)
	}
	uploadDuration := time.Since(startUpload)

	// Calculate upload speed
	uploadSpeedMBps := float64(bytesUploaded) / (1024 * 1024) / uploadDuration.Seconds()
	uploadSpeedMbps := uploadSpeedMBps * 8 // Convert to megabits per second

	fmt.Printf("\n✓ File uploaded successfully\n")
	fmt.Printf("Upload completed in %v\n", uploadDuration)
	fmt.Printf("Upload Speed: %.2f MB/s (%.2f Mbps)\n\n", uploadSpeedMBps, uploadSpeedMbps)

	// Step 4: Download Speed Test - Copy file back from folder
	downloadFileName := fmt.Sprintf("speedtest_download_%d_%d.txt", sizeMB, time.Now().Unix())
	downloadFilePath := filepath.Join(currentDir, downloadFileName)
	fmt.Printf("Step 4: Download Speed Test - Copying file from folder...\n")
	fmt.Printf("Source: %s\n", folderFileName)
	fmt.Printf("Target: %s\n", downloadFilePath)

	startDownload := time.Now()
	bytesDownloaded, err := copyFileWithProgress(folderFileName, downloadFilePath)
	if err != nil {
		// Clean up files before returning error
		os.Remove(localFilePath)
		os.Remove(folderFileName)
		return fmt.Errorf("failed to copy file from folder: %w", err)
	}
	downloadDuration := time.Since(startDownload)

	// Calculate download speed
	downloadSpeedMBps := float64(bytesDownloaded) / (1024 * 1024) / downloadDuration.Seconds()
	downloadSpeedMbps := downloadSpeedMBps * 8 // Convert to megabits per second

	fmt.Printf("\n✓ File downloaded successfully\n")
	fmt.Printf("Download completed in %v\n", downloadDuration)
	fmt.Printf("Download Speed: %.2f MB/s (%.2f Mbps)\n\n", downloadSpeedMBps, downloadSpeedMbps)

	// Step 5: Clean up files
	fmt.Printf("Step 5: Cleaning up test files...\n")

	// Remove original local file
	if err := os.Remove(localFilePath); err != nil {
		fmt.Printf("⚠ Warning: Could not remove original local file: %v\n", err)
	} else {
		fmt.Printf("✓ Original local test file removed\n")
	}

	// Remove downloaded file
	if err := os.Remove(downloadFilePath); err != nil {
		fmt.Printf("⚠ Warning: Could not remove downloaded file: %v\n", err)
	} else {
		fmt.Printf("✓ Downloaded test file removed\n")
	}

	// Remove folder file (unless noDelete flag is set)
	if noDelete {
		fmt.Printf("✓ Folder test file kept: %s\n", folderFileName)
	} else {
		if err := os.Remove(folderFileName); err != nil {
			fmt.Printf("⚠ Warning: Could not remove folder file: %v\n", err)
		} else {
			fmt.Printf("✓ Folder test file removed\n")
		}
	}

	fmt.Printf("\nSpeed Test Summary:\n")
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Upload time: %v, Speed: %.2f MB/s (%.2f Mbps)\n", uploadDuration, uploadSpeedMBps, uploadSpeedMbps)
	fmt.Printf("Download time: %v, Speed: %.2f MB/s (%.2f Mbps)\n", downloadDuration, downloadSpeedMBps, downloadSpeedMbps)

	return nil
}
