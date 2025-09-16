package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WipeProgress tracks wipe operation progress
type WipeProgress struct {
	TotalItems    int64
	ProcessedItems int64
	StartTime     time.Time
	CurrentItem   string
}

// handleWipeCommand processes the wipe command
func handleWipeCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no target specified for wipe operation")
	}

	targetPath := args[0]
	
	// Check if target exists
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("target path does not exist: %s", targetPath)
		}
		return fmt.Errorf("error accessing target path: %v", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("target must be a directory: %s", targetPath)
	}

	fmt.Printf("Wiping contents of: %s\n", targetPath)
	startTime := time.Now()

	// Try fast method first: delete and recreate directory
	if err := wipeFast(targetPath, info); err != nil {
		fmt.Printf("Fast wipe failed, using standard method: %v\n", err)
		// Fallback to standard deletion
		return wipeStandard(targetPath)
	}

	duration := time.Since(startTime)
	fmt.Printf("\nWipe completed in %s\n", formatDuration(duration))
	return nil
}

// wipeFast tries to delete and recreate the directory (fastest method)
func wipeFast(targetPath string, originalInfo os.FileInfo) error {
	// Get parent directory
	parentDir := filepath.Dir(targetPath)
	
	// Check if we have write permission to parent directory
	if err := checkWritePermission(parentDir); err != nil {
		return fmt.Errorf("no write permission to parent directory: %v", err)
	}

	// Remove the entire directory
	err := os.RemoveAll(targetPath)
	if err != nil {
		return fmt.Errorf("failed to remove directory: %v", err)
	}

	// Recreate the directory with original permissions
	err = os.Mkdir(targetPath, originalInfo.Mode())
	if err != nil {
		return fmt.Errorf("failed to recreate directory: %v", err)
	}

	// Restore original timestamps
	err = os.Chtimes(targetPath, originalInfo.ModTime(), originalInfo.ModTime())
	if err != nil {
		fmt.Printf("Warning: Could not restore timestamps: %v\n", err)
	}

	fmt.Printf("Fast wipe completed - directory deleted and recreated\n")
	return nil
}

// wipeStandard performs standard file-by-file deletion with progress
func wipeStandard(targetPath string) error {
	progress := &WipeProgress{
		StartTime: time.Now(),
	}

	// First pass: count items
	fmt.Printf("Scanning directory contents...\n")
	err := filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors during counting
		}
		if path != targetPath { // Don't count the root directory
			progress.TotalItems++
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("error scanning directory: %v", err)
	}

	fmt.Printf("Found %d items to delete\n", progress.TotalItems)

	// Second pass: delete items with progress
	return filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			fmt.Printf("Warning: Error accessing %s: %v - skipping\n", path, err)
			return nil
		}

		// Skip the root directory itself
		if path == targetPath {
			return nil
		}

		progress.CurrentItem = path
		progress.ProcessedItems++

		// Show progress every 100 items or for large files
		if progress.ProcessedItems%100 == 0 || (info != nil && info.Size() > 1024*1024) {
			showWipeProgress(progress)
		}

		// Delete the item
		err = os.RemoveAll(path)
		if err != nil {
			fmt.Printf("Warning: Could not delete %s: %v\n", path, err)
			return nil // Continue with other items
		}

		// If we just deleted a directory, skip its contents
		if info != nil && info.IsDir() {
			return filepath.SkipDir
		}

		return nil
	})
}

// showWipeProgress displays current wipe progress
func showWipeProgress(progress *WipeProgress) {
	// Get short filename for display
	currentItem := progress.CurrentItem
	if len(currentItem) > 60 {
		parts := strings.Split(currentItem, string(os.PathSeparator))
		if len(parts) > 2 {
			currentItem = "..." + string(os.PathSeparator) + filepath.Base(currentItem)
		}
	}

	elapsed := time.Since(progress.StartTime)
	itemsPerSecond := float64(progress.ProcessedItems) / elapsed.Seconds()
	
	var eta string
	if itemsPerSecond > 0 {
		remainingItems := progress.TotalItems - progress.ProcessedItems
		etaSeconds := int64(float64(remainingItems) / itemsPerSecond)
		eta = formatETA(time.Duration(etaSeconds) * time.Second)
	} else {
		eta = "unknown"
	}

	fmt.Printf("\rWiping: %s [%d/%d items, %.0f items/sec, ETA: %s]",
		currentItem,
		progress.ProcessedItems,
		progress.TotalItems,
		itemsPerSecond,
		eta)
}

// checkWritePermission checks if we have write permission to a directory
func checkWritePermission(path string) error {
	// Try to create a temporary file
	testFile := filepath.Join(path, ".wipe_test_"+fmt.Sprintf("%d", time.Now().UnixNano()))
	file, err := os.Create(testFile)
	if err != nil {
		return err
	}
	file.Close()
	os.Remove(testFile)
	return nil
}
