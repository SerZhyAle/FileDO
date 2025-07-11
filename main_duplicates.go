package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Constants for duplicate file processing
const (
	MIN_DUPLICATE_FILE_SIZE = 16                // Minimum file size to consider (in bytes)
	QUICK_HASH_SIZE         = 8192              // Size for quick hash sample (8KB)
	MAX_WORKERS             = 5                 // Maximum concurrent hash workers
	HASH_CACHE_FILE         = "hash_cache.json" // Filename for hash cache
)

// FileHashType indicates the type of hash
type FileHashType int

const (
	QuickHash FileHashType = iota
	FullHash
)

// DuplicateSelectionMode indicates how to select original files
type DuplicateSelectionMode int

const (
	OldestAsOriginal     DuplicateSelectionMode = iota // Keep oldest file as original
	NewestAsOriginal                                   // Keep newest file as original
	FirstAlphaAsOriginal                               // Keep first alphabetically as original
	LastAlphaAsOriginal                                // Keep last alphabetically as original
)

// DuplicateAction indicates what to do with duplicate files
type DuplicateAction int

const (
	NoAction     DuplicateAction = iota // Just report duplicates
	MoveAction                          // Move duplicates to target directory
	DeleteAction                        // Delete duplicates
)

// DuplicateFileInfo stores information about a file for duplicate detection
type DuplicateFileInfo struct {
	Path        string
	Size        int64
	QuickHash   string    // Hash of first few KB
	FullHash    string    // Complete file hash
	LastAccess  time.Time // When the file was last accessed
	CreatedTime time.Time // When the file was created
	ModTime     time.Time // When the file was last modified
	IsOriginal  bool      // Whether this file is considered the original
}

// HashCache stores file hashes for reuse between runs
type HashCache struct {
	Entries map[string]CacheEntry
	mutex   sync.RWMutex
}

// CacheEntry represents a single cached hash entry
type CacheEntry struct {
	Path      string
	Size      int64
	QuickHash string
	FullHash  string
	LastSeen  time.Time
}

// Worker pool for parallel hash calculation
type HashWorker struct {
	jobs        chan hashJob
	results     chan hashResult
	workerCount int
	wg          sync.WaitGroup
}

type hashJob struct {
	file DuplicateFileInfo
	mode FileHashType
}

type hashResult struct {
	file DuplicateFileInfo
	err  error
}

// runDeviceCheckDuplicates performs duplicate file check on a device
func runDeviceCheckDuplicates(devicePath string, args []string) error {
	fmt.Printf("Checking for duplicate files on device: %s\n", devicePath)

	// Convert device path to directory format
	deviceDir := devicePath
	if len(devicePath) == 2 && devicePath[1] == ':' {
		deviceDir = devicePath + "\\"
	}

	return findDuplicates(deviceDir, args, true)
}

// runFolderCheckDuplicates performs duplicate file check in a folder
func runFolderCheckDuplicates(folderPath string, args []string) error {
	fmt.Printf("Checking for duplicate files in folder: %s\n", folderPath)
	return findDuplicates(folderPath, args, false)
}

// runNetworkCheckDuplicates performs duplicate file check on a network path
func runNetworkCheckDuplicates(networkPath string, args []string, logger *HistoryLogger) error {
	fmt.Printf("Checking for duplicate files on network path: %s\n", networkPath)
	return findDuplicates(networkPath, args, false)
}

// Main function for finding duplicate files
func findDuplicates(rootPath string, args []string, isDevice bool) error {
	startTime := time.Now()

	// Determine output file and processing options
	outputPath := "duplicates.lst" // Default
	outputFileSpecified := false
	var verbose bool = true

	// Default settings for duplicate handling
	selectionMode := NewestAsOriginal
	action := NoAction
	targetDir := ""

	// First pass: identify all special arguments in any order
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(args[i])

		// Check for output file specification
		if arg == "list" && i+1 < len(args) {
			outputPath = args[i+1]
			outputFileSpecified = true
			i++ // Skip the next argument as it's the filename
			continue
		}

		// Check for verbosity options
		if arg == "quiet" || arg == "q" || arg == "short" || arg == "s" {
			verbose = false
			continue
		}

		// Check for selection mode options
		switch arg {
		case "old":
			selectionMode = NewestAsOriginal // Keep newest as original, move/delete older files
		case "new":
			selectionMode = OldestAsOriginal // Keep oldest as original, move/delete newer files
		case "abc":
			selectionMode = LastAlphaAsOriginal // Keep last alphabetically as original
		case "xyz":
			selectionMode = FirstAlphaAsOriginal // Keep first alphabetically as original
		case "move":
			if i+1 < len(args) {
				action = MoveAction
				targetDir = args[i+1]
				i++ // Skip the next argument as it's the target directory
			}
		case "delete", "del":
			action = DeleteAction
		}
	}

	// Initialize hash cache
	cache := NewHashCache()
	defer cache.Save() // Save cache when we're done

	// Determine number of worker threads based on available CPU cores
	workerCount := runtime.NumCPU() - 1
	if workerCount < 1 {
		workerCount = 1
	}
	if workerCount > MAX_WORKERS {
		workerCount = MAX_WORKERS
	}

	fmt.Printf("Scanning for files in %s (using %d workers)...\n", rootPath, workerCount)

	// Collect information about all files
	filesBySize := make(map[int64][]DuplicateFileInfo)
	var totalFiles int
	var totalSize int64

	// Show scanning progress
	var lastProgressUpdate time.Time
	var dirs int

	err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files we can't access
		}

		if info.IsDir() {
			dirs++

			// Show periodic progress
			if verbose && time.Since(lastProgressUpdate) > 2*time.Second {
				fmt.Printf("\r  Scanning: %d files, %d dirs...", totalFiles, dirs)
				lastProgressUpdate = time.Now()
			}

			return nil
		}

		if !info.IsDir() && info.Size() >= MIN_DUPLICATE_FILE_SIZE {
			// Get file information
			filesBySize[info.Size()] = append(filesBySize[info.Size()], DuplicateFileInfo{
				Path:       path,
				Size:       info.Size(),
				ModTime:    info.ModTime(),
				LastAccess: time.Now(),
			})
			totalFiles++
			totalSize += info.Size()
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("error scanning files: %w", err)
	}

	// Clear progress line
	if verbose {
		fmt.Print("\r                                                                \r")
	}

	fmt.Printf("Found %d files (%.2f GB), processing potential duplicates...\n",
		totalFiles, float64(totalSize)/(1024*1024*1024))

	// Find groups of files with the same size
	var duplicateGroups [][]DuplicateFileInfo
	var sizesWithDuplicates int
	var potentialDuplicateFiles int

	// Count potential duplicates for progress reporting
	for _, files := range filesBySize {
		if len(files) >= 2 {
			sizesWithDuplicates++
			potentialDuplicateFiles += len(files)
		}
	}

	fmt.Printf("Found %d file sizes with potential duplicates (%d files).\n",
		sizesWithDuplicates, potentialDuplicateFiles)

	// Track progress
	sizeIndex := 0

	for size, files := range filesBySize {
		if len(files) < 2 {
			continue // Skip unique sizes
		}

		sizeIndex++

		// STEP 1: Group by quick hash (first few KB)
		if verbose {
			// Calculate progress percentage and ETA
			percent := int((float64(sizeIndex) / float64(sizesWithDuplicates)) * 100)
			elapsed := time.Since(startTime)
			var eta time.Duration
			if sizeIndex > 0 {
				eta = time.Duration(float64(elapsed) * (float64(sizesWithDuplicates-sizeIndex) / float64(sizeIndex)))
			}

			// Use \r to overwrite the line instead of creating a new one
			etaStr := ""
			if eta > 0 {
				etaStr = formatETA(eta)
			}
			fmt.Printf("\r[%d/%d] %d%% Processing %d files of size %d bytes... ETA: %s",
				sizeIndex, sizesWithDuplicates, percent, len(files), size, etaStr)
		} else if sizeIndex%10 == 0 || sizeIndex == sizesWithDuplicates {
			percent := int((float64(sizeIndex) / float64(sizesWithDuplicates)) * 100)
			fmt.Printf("\rProcessing: %d/%d file sizes... %d%% complete", sizeIndex, sizesWithDuplicates, percent)
		}

		// Initialize worker pool for quick hashes
		quickHashWorker := NewHashWorker(workerCount)
		filesByQuickHash := make(map[string][]DuplicateFileInfo)

		// Queue jobs for quick hash calculation
		for _, file := range files {
			// Check if hash exists in cache
			if quickHash := cache.Get(file.Path, file.Size, QuickHash); quickHash != "" {
				file.QuickHash = quickHash
				filesByQuickHash[quickHash] = append(filesByQuickHash[quickHash], file)
			} else {
				quickHashWorker.AddJob(file, QuickHash)
			}
		}

		// Create channel for completion signal
		resultsDone := make(chan bool)

		// Start goroutine for processing results
		go func() {
			for result := range quickHashWorker.results {
				if result.err == nil {
					// Update cache
					cache.Store(result.file)
					// Group by quick hash
					filesByQuickHash[result.file.QuickHash] = append(
						filesByQuickHash[result.file.QuickHash], result.file)
				}
			}
			// Signal that processing is complete
			close(resultsDone)
		}()

		// Wait for all hash calculations to complete
		quickHashWorker.Wait()

		// Now close the results channel to allow the processing goroutine to finish
		close(quickHashWorker.results)

		// Wait for result processing to complete
		<-resultsDone

		// STEP 2: For files with matching quick hashes, calculate full hash
		for _, potentialDuplicates := range filesByQuickHash {
			if len(potentialDuplicates) < 2 {
				continue // Skip files with unique quick hash
			}

			// Initialize worker pool for full hashes
			fullHashWorker := NewHashWorker(workerCount)
			filesByFullHash := make(map[string][]DuplicateFileInfo)

			// Queue jobs for full hash calculation
			for _, file := range potentialDuplicates {
				// Check if hash exists in cache
				if fullHash := cache.Get(file.Path, file.Size, FullHash); fullHash != "" {
					file.FullHash = fullHash
					filesByFullHash[fullHash] = append(filesByFullHash[fullHash], file)
				} else {
					fullHashWorker.AddJob(file, FullHash)
				}
			}

			// Create channel for completion signal
			resultsDone := make(chan bool)

			// Start goroutine for processing results
			go func() {
				for result := range fullHashWorker.results {
					if result.err == nil {
						// Update cache
						cache.Store(result.file)
						// Group by full hash
						filesByFullHash[result.file.FullHash] = append(
							filesByFullHash[result.file.FullHash], result.file)
					}
				}
				// Signal that processing is complete
				close(resultsDone)
			}()

			// Wait for all hash calculations to complete
			fullHashWorker.Wait()

			// Now close the results channel to allow the processing goroutine to finish
			close(fullHashWorker.results)

			// Wait for result processing to complete
			<-resultsDone

			// Add groups with identical full hashes
			for _, duplicates := range filesByFullHash {
				if len(duplicates) > 1 {
					duplicateGroups = append(duplicateGroups, duplicates)
				}
			}
		}
	}

	elapsedTime := time.Since(startTime)

	// Make sure to clear any progress line
	fmt.Print("\r                                                                                    \r")

	// Output results
	if len(duplicateGroups) > 0 {
		// Process duplicate groups according to selection mode and action
		processedGroups, err := processDuplicateGroups(duplicateGroups, selectionMode, action, targetDir)
		if err != nil {
			return err
		}

		// Calculate statistics
		var totalDuplicateFiles int
		var totalDuplicateSize int64

		for _, group := range processedGroups {
			// One file is considered original, others are duplicates
			duplicateCount := len(group) - 1
			totalDuplicateFiles += duplicateCount
			totalDuplicateSize += group[0].Size * int64(duplicateCount)
		}

		// Generate action summary
		actionSummary := ""
		switch action {
		case MoveAction:
			actionSummary = fmt.Sprintf(" (%s files moved to %s)",
				getSelectionModeText(selectionMode), targetDir)
		case DeleteAction:
			actionSummary = fmt.Sprintf(" (%s files deleted)",
				getSelectionModeText(selectionMode))
		}

		if outputFileSpecified {
			fmt.Printf("\nAnalysis completed in %s.%s\n", elapsedTime.Round(time.Millisecond), actionSummary)
			fmt.Printf("Found %d groups with %d duplicate files, wasting %.2f MB.\n",
				len(processedGroups), totalDuplicateFiles, float64(totalDuplicateSize)/(1024*1024))
			return saveDuplicatesToFile(processedGroups, outputPath, rootPath)
		} else {
			// If no output file specified, just display on screen
			showDuplicatesOnScreen(processedGroups, rootPath)
			fmt.Printf("Analysis completed in %s.%s\n", elapsedTime.Round(time.Millisecond), actionSummary)
			return nil
		}
	} else {
		fmt.Printf("\nNo duplicate files found. Analysis completed in %s.\n",
			elapsedTime.Round(time.Millisecond))
		return nil
	}
}

// Save duplicate information to a file
func saveDuplicatesToFile(groups [][]DuplicateFileInfo, outputPath string, rootPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("error creating output file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	fmt.Fprintf(writer, "Duplicates in %s collected %s\n", rootPath, time.Now().Format("2006-01-02 15:04:05"))

	for _, group := range groups {
		if len(group) > 1 {
			// Use FullHash for output
			fmt.Fprintf(writer, "size: %d bytes, checksum %s\n", group[0].Size, group[0].FullHash)

			for _, f := range group {
				// Mark original files
				if f.IsOriginal {
					fmt.Fprintf(writer, "[ORIGINAL] %s (modified: %s)\n", f.Path, f.ModTime.Format("2006-01-02 15:04:05"))
				} else {
					fmt.Fprintf(writer, "%s (modified: %s)\n", f.Path, f.ModTime.Format("2006-01-02 15:04:05"))
				}
			}

			fmt.Fprintf(writer, "----\n")
		}
	}

	fmt.Printf("Duplicate file information saved to %s\n", outputPath)
	return nil
}

// Display duplicate information on screen
func showDuplicatesOnScreen(groups [][]DuplicateFileInfo, rootPath string) {
	fmt.Printf("\nDuplicates in %s:\n\n", rootPath)

	totalDuplicates := 0
	totalWastedSpace := int64(0)

	for i, group := range groups {
		if len(group) > 1 {
			duplicateCount := len(group) - 1 // One file considered original
			totalDuplicates += duplicateCount

			// Find original file for size calculation
			var originalFile DuplicateFileInfo
			for _, file := range group {
				if file.IsOriginal {
					originalFile = file
					break
				}
			}

			// If no file marked as original, use first file
			if originalFile.Path == "" {
				originalFile = group[0]
			}

			wastedSpace := originalFile.Size * int64(duplicateCount)
			totalWastedSpace += wastedSpace

			fmt.Printf("Group %d: %d files, size: %d bytes, checksum: %s\n",
				i+1, len(group), originalFile.Size, originalFile.FullHash)

			// Display files with original marked
			totalShown := 0

			// First show original
			for _, file := range group {
				if file.IsOriginal {
					fmt.Printf("  [ORIGINAL] %s (modified: %s)\n", file.Path, file.ModTime.Format("2006-01-02"))
					totalShown++
				}
			}

			// Then show duplicates (up to limit)
			shownDuplicates := 0
			for _, file := range group {
				if !file.IsOriginal {
					if shownDuplicates < 2 || totalShown < 3 || shownDuplicates == len(group)-1 {
						fmt.Printf("  %s (modified: %s)\n", file.Path, file.ModTime.Format("2006-01-02"))
						totalShown++
					} else if shownDuplicates == 2 && totalShown == 3 {
						fmt.Printf("  ... (%d more files)\n", len(group)-totalShown)
						break
					}
					shownDuplicates++
				}
			}

			fmt.Println()
		}
	}

	fmt.Printf("Summary: Found %d duplicate files, wasting %.2f MB of space\n",
		totalDuplicates, float64(totalWastedSpace)/(1024*1024))
}

// processDuplicateGroups handles duplicate files according to the specified action and selection mode
func processDuplicateGroups(groups [][]DuplicateFileInfo, selectionMode DuplicateSelectionMode,
	action DuplicateAction, targetDir string) ([][]DuplicateFileInfo, error) {

	// If there's no action to take, just return the groups
	if action == NoAction {
		return groups, nil
	}

	// Create target directory if it doesn't exist (for move action)
	if action == MoveAction && targetDir != "" {
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	processedGroups := make([][]DuplicateFileInfo, 0, len(groups))

	for _, group := range groups {
		if len(group) < 2 {
			continue // Skip non-duplicate files
		}

		// Determine which file should be kept as original based on selection mode
		var originalIndex int

		switch selectionMode {
		case OldestAsOriginal:
			// Find oldest file by modification time
			oldestTime := group[0].ModTime
			originalIndex = 0
			for i := 1; i < len(group); i++ {
				if group[i].ModTime.Before(oldestTime) {
					oldestTime = group[i].ModTime
					originalIndex = i
				}
			}

		case NewestAsOriginal:
			// Find newest file by modification time
			newestTime := group[0].ModTime
			originalIndex = 0
			for i := 1; i < len(group); i++ {
				if group[i].ModTime.After(newestTime) {
					newestTime = group[i].ModTime
					originalIndex = i
				}
			}

		case FirstAlphaAsOriginal:
			// Find first file alphabetically
			firstName := group[0].Path
			originalIndex = 0
			for i := 1; i < len(group); i++ {
				if strings.ToLower(filepath.Base(group[i].Path)) < strings.ToLower(filepath.Base(firstName)) {
					firstName = group[i].Path
					originalIndex = i
				}
			}

		case LastAlphaAsOriginal:
			// Find last file alphabetically
			lastName := group[0].Path
			originalIndex = 0
			for i := 1; i < len(group); i++ {
				if strings.ToLower(filepath.Base(group[i].Path)) > strings.ToLower(filepath.Base(lastName)) {
					lastName = group[i].Path
					originalIndex = i
				}
			}
		}

		// Mark the selected file as original
		for i := range group {
			group[i].IsOriginal = (i == originalIndex)
		}

		// Process according to action
		if action == NoAction {
			// Just add the group to results, no action needed
			processedGroups = append(processedGroups, group)
			continue
		}

		// Create a new slice with the original file first
		processedGroup := make([]DuplicateFileInfo, 0, len(group))
		processedGroup = append(processedGroup, group[originalIndex])

		// Add the rest of the files and apply actions to duplicates
		for i, file := range group {
			if i == originalIndex {
				continue // Skip the original, already added
			}

			switch action {
			case DeleteAction:
				// Delete duplicate file
				if err := os.Remove(file.Path); err != nil {
					fmt.Printf("Error deleting duplicate file %s: %v\n", file.Path, err)
				} else {
					fmt.Printf("Deleted duplicate: %s\n", file.Path)
				}

			case MoveAction:
				if targetDir != "" {
					// Create target directory structure
					err := os.MkdirAll(targetDir, 0755)
					if err != nil {
						return nil, fmt.Errorf("failed to create target directory: %w", err)
					}

					// Create a target path preserving the original folder structure
					targetPath := filepath.Join(targetDir, filepath.Base(file.Path))

					// If file exists, add a suffix
					if _, err := os.Stat(targetPath); err == nil {
						ext := filepath.Ext(targetPath)
						base := strings.TrimSuffix(filepath.Base(targetPath), ext)
						targetPath = filepath.Join(targetDir, fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
					}

					// Move file
					err = os.Rename(file.Path, targetPath)
					if err != nil {
						fmt.Printf("Error moving duplicate file %s: %v\n", file.Path, err)
					} else {
						fmt.Printf("Moved duplicate: %s -> %s\n", file.Path, targetPath)
						file.Path = targetPath // Update path for reporting
					}
				}
			}

			processedGroup = append(processedGroup, file)
		}

		processedGroups = append(processedGroups, processedGroup)
	}

	return processedGroups, nil
}

// getSelectionModeText returns a human-readable description of the selection mode
func getSelectionModeText(mode DuplicateSelectionMode) string {
	switch mode {
	case OldestAsOriginal:
		return "newer"
	case NewestAsOriginal:
		return "older"
	case FirstAlphaAsOriginal:
		return "alphabetically later"
	case LastAlphaAsOriginal:
		return "alphabetically earlier"
	default:
		return "duplicate"
	}
}

// loadDuplicatesFromFile loads duplicate groups from a file
func loadDuplicatesFromFile(filePath string) ([][]DuplicateFileInfo, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("error opening duplicates list file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	var duplicateGroups [][]DuplicateFileInfo
	var currentGroup []DuplicateFileInfo
	var currentSize int64
	var currentHash string
	inHeader := true

	for scanner.Scan() {
		line := scanner.Text()

		// Skip header line
		if inHeader {
			inHeader = false
			continue
		}

		// Check if we've reached the end of a group
		if line == "----" {
			if len(currentGroup) > 0 {
				duplicateGroups = append(duplicateGroups, currentGroup)
				currentGroup = nil
			}
			continue
		}

		// Check if line contains size and hash information
		if strings.HasPrefix(line, "size:") {
			parts := strings.Split(line, ",")
			if len(parts) >= 2 {
				// Parse size
				sizeStr := strings.TrimPrefix(parts[0], "size:")
				sizeStr = strings.TrimSpace(sizeStr)
				sizeStr = strings.TrimSuffix(sizeStr, " bytes")
				size, err := strconv.ParseInt(sizeStr, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("error parsing file size: %w", err)
				}
				currentSize = size

				// Parse hash
				hashStr := strings.TrimPrefix(parts[1], " checksum")
				currentHash = strings.TrimSpace(hashStr)

				// Start a new group
				currentGroup = []DuplicateFileInfo{}
			}
			continue
		}

		// Check if line contains file information
		isOriginal := false
		if strings.HasPrefix(line, "[ORIGINAL]") {
			isOriginal = true
			line = strings.TrimPrefix(line, "[ORIGINAL]")
			line = strings.TrimSpace(line)
		}

		// Extract file path and modification time
		pathParts := strings.Split(line, " (modified:")
		if len(pathParts) >= 1 {
			filePath := strings.TrimSpace(pathParts[0])

			// Parse modification time if available
			var modTime time.Time
			if len(pathParts) > 1 {
				timeStr := strings.TrimSuffix(pathParts[1], ")")
				timeStr = strings.TrimSpace(timeStr)
				modTime, _ = time.Parse("2006-01-02 15:04:05", timeStr)
			}

			// Create file info
			fileInfo := DuplicateFileInfo{
				Path:       filePath,
				Size:       currentSize,
				FullHash:   currentHash,
				ModTime:    modTime,
				IsOriginal: isOriginal,
			}

			currentGroup = append(currentGroup, fileInfo)
		}
	}

	// Add the last group if it exists
	if len(currentGroup) > 0 {
		duplicateGroups = append(duplicateGroups, currentGroup)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading duplicates list file: %w", err)
	}

	return duplicateGroups, nil
}

// processDuplicatesFromFile loads duplicates from a file and processes them according to selection mode and action
func processDuplicatesFromFile(filePath string, args []string) error {
	// Default settings for duplicate handling
	selectionMode := NewestAsOriginal
	action := NoAction
	targetDir := ""

	// Process arguments in any order
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(args[i])

		// Skip the "from" and list file arguments
		if (arg == "from" && i+1 < len(args) && strings.ToLower(args[i+1]) == "list") ||
			(arg == "list" && i+1 < len(args)) {
			i++ // Skip the next argument (list)
			i++ // Skip the file path
			continue
		}

		// Check for selection mode options
		switch arg {
		case "old":
			selectionMode = NewestAsOriginal // Keep newest as original, move/delete older files
		case "new":
			selectionMode = OldestAsOriginal // Keep oldest as original, move/delete newer files
		case "abc":
			selectionMode = LastAlphaAsOriginal // Keep last alphabetically as original
		case "xyz":
			selectionMode = FirstAlphaAsOriginal // Keep first alphabetically as original
		case "move":
			if i+1 < len(args) {
				action = MoveAction
				targetDir = args[i+1]
				i++ // Skip the next argument as it's the target directory
			}
		case "delete", "del":
			action = DeleteAction
		}
	}

	// If no action was specified, just display the duplicates
	if action == NoAction {
		fmt.Println("Loading duplicates from file, no action specified (use 'del' or 'move' to take action)")
	}

	// Load duplicate groups from file
	fmt.Printf("Loading duplicates from file: %s\n", filePath)
	startTime := time.Now()
	groups, err := loadDuplicatesFromFile(filePath)
	if err != nil {
		return err
	}

	// Check if we found any groups
	if len(groups) == 0 {
		fmt.Println("No duplicate groups found in the file.")
		return nil
	}

	fmt.Printf("Loaded %d duplicate groups from file.\n", len(groups))

	// Process the groups according to selection mode and action
	processedGroups, err := processDuplicateGroups(groups, selectionMode, action, targetDir)
	if err != nil {
		return err
	}

	// Calculate statistics
	var totalDuplicateFiles int
	var totalDuplicateSize int64

	for _, group := range processedGroups {
		// One file is considered original, others are duplicates
		duplicateCount := len(group) - 1
		totalDuplicateFiles += duplicateCount
		totalDuplicateSize += group[0].Size * int64(duplicateCount)
	}

	// Generate action summary
	actionSummary := ""
	switch action {
	case MoveAction:
		actionSummary = fmt.Sprintf(" (%s files moved to %s)",
			getSelectionModeText(selectionMode), targetDir)
	case DeleteAction:
		actionSummary = fmt.Sprintf(" (%s files deleted)",
			getSelectionModeText(selectionMode))
	}

	// Display on screen
	showDuplicatesOnScreen(processedGroups, "file "+filePath)
	fmt.Printf("Processing completed in %s.%s\n",
		time.Since(startTime).Round(time.Millisecond), actionSummary)

	return nil
}

// formatETA formats a duration for display in ETA
func formatETA(d time.Duration) string {
	d = d.Round(time.Second)

	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Hour {
		minutes := int(d.Minutes())
		seconds := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		hours := int(d.Hours())
		minutes := int(d.Minutes()) % 60
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
}
