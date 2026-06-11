package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// wipeCountCap bounds the pre-wipe object count so the confirmation prompt
// stays fast even on very large trees.
const wipeCountCap = 100000

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

	// Parse optional automation flags; the first non-flag argument is the target.
	// --yes / -y / --force / --force-wipe skip the interactive prompt for normal
	// targets, but never bypass the dangerous-target guardrails below.
	force := false
	targetPath := ""
	for _, a := range args {
		switch strings.ToLower(a) {
		case "--yes", "-y", "--force", "--force-wipe", "/y":
			force = true
		default:
			if targetPath == "" {
				targetPath = a
			}
		}
	}
	if targetPath == "" {
		return fmt.Errorf("no target specified for wipe operation")
	}
	if os.Getenv("FILEDO_AUTO_CONFIRM") == "1" {
		force = true
	}

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

	// Safety guardrails: require explicit confirmation before any destruction.
	if err := confirmWipe(targetPath, force); err != nil {
		return err
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

// confirmWipe shows the target, a best-effort object count and requires explicit
// confirmation before a wipe proceeds. Dangerous targets (drive/share roots,
// reparse points, system TEMP) always require strong, interactive confirmation
// and are never bypassed by the --force flag.
func confirmWipe(targetPath string, force bool) error {
	dangerous, reason := classifyWipeTarget(targetPath)

	fmt.Printf("\nWIPE will permanently delete the contents of:\n  %s\n", targetPath)

	// Best-effort, bounded count so huge trees do not stall the prompt.
	if count, capped := quickCountWipeItems(targetPath); count >= 0 {
		suffix := ""
		if capped {
			suffix = "+"
		}
		fmt.Printf("Found %d%s item(s) to delete.\n", count, suffix)
	}

	if dangerous {
		fmt.Printf("\n!!! DANGEROUS TARGET: %s\n", reason)
		fmt.Printf("This is a high-risk location. Extra confirmation is required.\n")
		// --force intentionally does NOT bypass the dangerous-target guardrail.
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Type WIPE to continue: ")
		line, _ := reader.ReadString('\n')
		if strings.TrimSpace(line) != "WIPE" {
			return fmt.Errorf("wipe cancelled by user")
		}
		fmt.Printf("Type the exact target path to confirm: ")
		line, _ = reader.ReadString('\n')
		if strings.TrimSpace(line) != strings.TrimSpace(targetPath) {
			return fmt.Errorf("wipe cancelled: target path confirmation did not match")
		}
		return nil
	}

	if force {
		fmt.Printf("Confirmation skipped (--yes/--force).\n")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Type WIPE to continue: ")
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != "WIPE" {
		return fmt.Errorf("wipe cancelled by user")
	}
	return nil
}

// classifyWipeTarget reports whether a wipe target is a high-risk location and
// why. It flags drive roots (C:\), network share roots (\\server\share),
// reparse points (junctions/symlinks/mount points) and the system TEMP dir.
func classifyWipeTarget(targetPath string) (dangerous bool, reason string) {
	abs, err := filepath.Abs(targetPath)
	if err != nil {
		abs = targetPath
	}
	clean := filepath.Clean(abs)

	// Reparse point / junction / symlink / mount point.
	if hasReparsePoint(clean) {
		return true, "target is a reparse point (junction/symlink/mount point)"
	}

	// Drive root (C:\, D:\) or network share root (\\server\share).
	vol := filepath.VolumeName(clean)
	if vol != "" && (clean == vol || clean == vol+string(os.PathSeparator)) {
		if strings.HasPrefix(vol, `\\`) {
			return true, "target is the root of a network share"
		}
		return true, "target is the root of a drive"
	}

	// System TEMP directory (wiping it can break the OS and FileDO itself).
	if tmp := os.TempDir(); tmp != "" && pathsEqual(clean, filepath.Clean(tmp)) {
		return true, "target is the system TEMP directory"
	}

	return false, ""
}

// pathsEqual compares two paths, case-insensitively on Windows.
func pathsEqual(a, b string) bool {
	if os.PathSeparator == '\\' {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// quickCountWipeItems returns a bounded count of items under targetPath. The
// count is capped at wipeCountCap so the confirmation prompt stays responsive on
// very large trees; capped is true when the cap was reached.
func quickCountWipeItems(targetPath string) (count int, capped bool) {
	_ = filepath.Walk(targetPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // ignore errors while counting
		}
		if path == targetPath {
			return nil // don't count the root itself
		}
		count++
		if count >= wipeCountCap {
			capped = true
			return filepath.SkipAll
		}
		return nil
	})
	return count, capped
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
