package fileduplicates

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Result of duplicate file search
type DuplicateResult struct {
	TotalFiles      int                            // Total files processed
	DuplicateFiles  int                            // Number of duplicate files
	DuplicateGroups int                            // Number of duplicate groups
	DuplicateSize   int64                          // Total size of duplicate files
	Groups          map[string][]DuplicateFileInfo // Map of groups by hash
	ProcessingTime  time.Duration                  // Total processing time
}

// Progress information for ongoing search
type ProgressInfo struct {
	CurrentFile  string        // Current file being processed
	FilesScanned int           // Number of files scanned so far
	TotalFiles   int           // Estimated total files (if known)
	StartTime    time.Time     // When processing started
	ElapsedTime  time.Duration // Time elapsed so far
	PercentDone  float64       // Percent complete (0-100)
	EstimatedETA string        // Estimated time remaining
}

// FindDuplicates finds duplicate files in a directory tree
func FindDuplicates(rootPath string, options DuplicateOptions) (*DuplicateResult, error) {
	startTime := time.Now()

	// Create a result structure
	result := &DuplicateResult{
		Groups: make(map[string][]DuplicateFileInfo),
	}

	// Load hash cache
	cache, err := LoadHashCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to load hash cache: %v\n", err)
		cache = &HashCache{
			Entries: make(map[string]CacheEntry),
		}
	}

	// Create worker pool for hash calculation
	workerCount := GetOptimalWorkerCount()
	worker := NewHashWorker(workerCount)

	if options.Verbose {
		fmt.Printf("Using %d workers for hash calculation\n", workerCount)
	}

	// First scan to estimate total file count (for progress reporting)
	totalFiles := 0
	if options.Verbose {
		fmt.Println("Scanning directory for files...")
		err := filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil // Skip files with errors
			}
			if !info.IsDir() && info.Size() >= MIN_DUPLICATE_FILE_SIZE {
				totalFiles++
			}
			return nil
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Error during file scan: %v\n", err)
		}
		fmt.Printf("Found %d files to check.\n", totalFiles)
	}

	// Maps to track duplicates
	filesBySize := make(map[int64][]DuplicateFileInfo)
	filesByQuickHash := make(map[string][]DuplicateFileInfo)

	// Scan files and calculate quick hashes
	filesScanned := 0
	fmt.Println("Scanning for duplicates...")

	// Progress tracking variables
	lastProgressUpdate := time.Now()
	progressUpdateInterval := 500 * time.Millisecond

	err = filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip files with errors
		}

		if !info.IsDir() && info.Size() >= MIN_DUPLICATE_FILE_SIZE {
			filesScanned++

			// Update progress
			now := time.Now()
			if options.Verbose && now.Sub(lastProgressUpdate) > progressUpdateInterval {
				lastProgressUpdate = now
				elapsed := now.Sub(startTime)

				// Calculate progress percentage and ETA
				percentDone := 0.0
				eta := "unknown"
				if totalFiles > 0 {
					percentDone = float64(filesScanned) * 100 / float64(totalFiles)
					if filesScanned > 0 && percentDone > 0 {
						timePerFile := elapsed.Seconds() / float64(filesScanned)
						remainingFiles := totalFiles - filesScanned
						rs := timePerFile * float64(remainingFiles)
						eta = formatETA(time.Duration(rs) * time.Second)
					}
				}

				// Update progress display
				fmt.Printf("\rScanning: %s [%d/%d files, %.1f%%, ETA: %s]",
					path, filesScanned, totalFiles, percentDone, eta)
			}

			// Get file info
			fileInfo, err := GetFileInfo(path)
			if err != nil {
				return nil // Skip problematic files
			}

			// Group by file size first
			filesBySize[fileInfo.Size] = append(filesBySize[fileInfo.Size], fileInfo)
		}
		return nil
	})

	if options.Verbose {
		fmt.Println() // End the progress line
	}

	if err != nil {
		return nil, fmt.Errorf("error walking directory: %v", err)
	}

	// Process potential duplicates by size
	var potentialDuplicates []DuplicateFileInfo
	for _, files := range filesBySize {
		if len(files) > 1 {
			potentialDuplicates = append(potentialDuplicates, files...)
		}
	}

	if len(potentialDuplicates) == 0 {
		fmt.Println("No duplicate files found.")
		return result, nil
	}

	// Calculate quick hashes for potential duplicates
	if options.Verbose {
		fmt.Printf("Found %d potential duplicates by size. Calculating quick hashes...\n",
			len(potentialDuplicates))
	}

	// Submit jobs to calculate quick hashes
	resultsMutex := sync.Mutex{}
	processedCount := int64(0)
	totalCount := int64(len(potentialDuplicates))
	quickHashStartTime := time.Now()

	for _, file := range potentialDuplicates {
		// Use a copy of file to avoid race condition in the closure
		fileCopy := file

		// Try to get hash from cache first
		quickHash, err := cache.GetHash(fileCopy, QuickHash)
		if err == nil && quickHash != "" {
			fileCopy.QuickHash = quickHash
			resultsMutex.Lock()
			filesByQuickHash[quickHash] = append(filesByQuickHash[quickHash], fileCopy)
			resultsMutex.Unlock()

			// Update progress for cached files
			processed := atomic.AddInt64(&processedCount, 1)
			if options.Verbose && processed%100 == 0 {
				elapsed := time.Since(quickHashStartTime)
				percent := float64(processed) / float64(totalCount) * 100
				eta := "unknown"
				if processed > 0 {
					timePerFile := elapsed.Seconds() / float64(processed)
					remainingFiles := totalCount - processed
					rs := timePerFile * float64(remainingFiles)
					eta = formatETA(time.Duration(rs) * time.Second)
				}
				fmt.Printf("Quick hash progress: %d/%d (%.1f%%, ETA: %s)\r",
					processed, totalCount, percent, eta)
			}
		} else {
			// Submit for calculation
			worker.AddJob(fileCopy, QuickHash)
		}
	}

	// Process results as they come in
	go func() {
		for result := range worker.results {
			if result.err == nil {
				resultsMutex.Lock()
				filesByQuickHash[result.file.QuickHash] = append(
					filesByQuickHash[result.file.QuickHash], result.file)
				resultsMutex.Unlock()
			}

			// Update progress for calculated files
			processed := atomic.AddInt64(&processedCount, 1)
			if options.Verbose && processed%100 == 0 {
				elapsed := time.Since(quickHashStartTime)
				percent := float64(processed) / float64(totalCount) * 100
				eta := "unknown"
				if processed > 0 {
					timePerFile := elapsed.Seconds() / float64(processed)
					remainingFiles := totalCount - processed
					rs := timePerFile * float64(remainingFiles)
					eta = formatETA(time.Duration(rs) * time.Second)
				}
				fmt.Printf("Quick hash progress: %d/%d (%.1f%%, ETA: %s)\r",
					processed, totalCount, percent, eta)
			}
		}
	}()

	// Wait for all quick hashes to complete
	worker.Wait()

	// Final progress update
	if options.Verbose {
		fmt.Printf("Quick hash progress: %d/%d (100.0%%) - Complete\n",
			totalCount, totalCount)
	}

	// Process files with matching quick hashes for full hash comparison
	var duplicateGroups [][]DuplicateFileInfo

	// New worker for full hashes
	worker = NewHashWorker(workerCount)
	filesByFullHash := make(map[string][]DuplicateFileInfo)

	// Count files that need full hash calculation
	fullHashCount := int64(0)
	for _, files := range filesByQuickHash {
		if len(files) > 1 {
			fullHashCount += int64(len(files))
		}
	}

	fullHashProcessed := int64(0)
	fullHashStartTime := time.Now()
	if options.Verbose && fullHashCount > 0 {
		fmt.Printf("Found %d files requiring full hash calculation...\n", fullHashCount)
	}

	// Find groups with matching quick hashes
	for _, files := range filesByQuickHash {
		if len(files) > 1 {
			// Submit for full hash calculation
			for _, file := range files {
				fileCopy := file

				// Try cache first
				fullHash, err := cache.GetHash(fileCopy, FullHash)
				if err == nil && fullHash != "" {
					fileCopy.FullHash = fullHash
					resultsMutex.Lock()
					filesByFullHash[fullHash] = append(filesByFullHash[fullHash], fileCopy)
					resultsMutex.Unlock()

					// Update progress for cached files
					processed := atomic.AddInt64(&fullHashProcessed, 1)
					if options.Verbose && processed%50 == 0 {
						elapsed := time.Since(fullHashStartTime)
						percent := float64(processed) / float64(fullHashCount) * 100
						eta := "unknown"
						if processed > 0 {
							timePerFile := elapsed.Seconds() / float64(processed)
							remainingFiles := fullHashCount - processed
							rs := timePerFile * float64(remainingFiles)
							eta = formatETA(time.Duration(rs) * time.Second)
						}
						fmt.Printf("Full hash progress: %d/%d (%.1f%%, ETA: %s)\r",
							processed, fullHashCount, percent, eta)
					}
				} else {
					// Submit for calculation
					worker.AddJob(fileCopy, FullHash)
				}
			}
		}
	}

	// Process full hash results
	go func() {
		for result := range worker.results {
			if result.err == nil {
				resultsMutex.Lock()
				filesByFullHash[result.file.FullHash] = append(
					filesByFullHash[result.file.FullHash], result.file)
				resultsMutex.Unlock()
			}

			// Update progress for calculated files
			processed := atomic.AddInt64(&fullHashProcessed, 1)
			if options.Verbose && processed%50 == 0 {
				elapsed := time.Since(fullHashStartTime)
				percent := float64(processed) / float64(fullHashCount) * 100
				eta := "unknown"
				if processed > 0 {
					timePerFile := elapsed.Seconds() / float64(processed)
					remainingFiles := fullHashCount - processed
					rs := timePerFile * float64(remainingFiles)
					eta = formatETA(time.Duration(rs) * time.Second)
				}
				fmt.Printf("Full hash progress: %d/%d (%.1f%%, ETA: %s)\r",
					processed, fullHashCount, percent, eta)
			}
		}
	}()

	// Wait for all full hashes to complete
	worker.Wait()

	// Final progress update
	if options.Verbose && fullHashCount > 0 {
		fmt.Printf("Full hash progress: %d/%d (100.0%%) - Complete\n",
			fullHashCount, fullHashCount)
	}

	// Find true duplicates by full hash
	for fullHash, files := range filesByFullHash {
		if len(files) > 1 {
			duplicateGroups = append(duplicateGroups, files)
			result.Groups[fullHash] = files
		}
	}

	// Save cache
	if err := cache.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save hash cache: %v\n", err)
	}

	// No duplicates found
	if len(duplicateGroups) == 0 {
		fmt.Println("No duplicate files found.")
		return result, nil
	}

	// Process duplicate groups - mark original files and apply actions
	options.BatchMode = ProcessDuplicateGroups(duplicateGroups, options)

	// Calculate statistics
	result.TotalFiles = filesScanned
	result.DuplicateGroups = len(duplicateGroups)
	result.ProcessingTime = time.Since(startTime)

	// Count duplicates and size
	for _, group := range duplicateGroups {
		// Count all but one file in each group as duplicates
		result.DuplicateFiles += len(group) - 1

		// Calculate wasted space
		if len(group) > 0 {
			// Multiply by number of duplicates (all files minus the original)
			result.DuplicateSize += group[0].Size * int64(len(group)-1)
		}
	}

	// Output results
	if options.Verbose {
		OutputResults(result, options, duplicateGroups)
	}

	return result, nil
}

// ProcessDuplicateGroups marks original files and processes duplicates according to options
// It returns a boolean indicating if the batch mode was enabled during processing.
func ProcessDuplicateGroups(duplicateGroups [][]DuplicateFileInfo, options DuplicateOptions) bool {
	// Process each group
	for i := range duplicateGroups {
		group := duplicateGroups[i]

		// Sort the group according to selection mode
		sortDuplicateGroup(&group, options.SelectionMode)

		// Mark the first file as original
		if len(group) > 0 {
			group[0].IsOriginal = true
		}

		// Apply action to duplicate files (all but first)
		if options.Action != NoAction {
			for j := 1; j < len(group); j++ {
				file := group[j]

				// Check if file still exists before processing
				if _, err := os.Stat(file.Path); os.IsNotExist(err) {
					// File doesn't exist anymore (already processed), skip
					continue
				}

				switch options.Action {
				case DeleteAction:
					// Check if we're in interactive mode
					if !options.BatchMode {
						// Ask for confirmation if deleting
						fmt.Printf("Delete duplicate file: %s? (y/n/a, a=all): ", file.Path)
						var response string
						fmt.Scanln(&response)
						responseLower := strings.ToLower(response)

						if responseLower == "a" {
							// Set batch mode to true so we don't ask for future files
							options.BatchMode = true
							// Fall through to delete code
						} else if responseLower != "y" {
							// Skip this file if response is not "y" or "a"
							fmt.Println("Skipped")
							continue
						}
					}

					// Delete the file
					if err := os.Remove(file.Path); err != nil {
						fmt.Fprintf(os.Stderr, "Error deleting file %s: %v\n", file.Path, err)
					} else {
						fmt.Printf("Deleted: %s\n", file.Path)
					}

				case MoveAction:
					if options.TargetDir != "" {
						// Create target directory if it doesn't exist
						if err := os.MkdirAll(options.TargetDir, 0755); err != nil {
							fmt.Fprintf(os.Stderr, "Error creating target directory: %v\n", err)
							continue
						}

						// Get base filename
						fileName := filepath.Base(file.Path)
						targetPath := filepath.Join(options.TargetDir, fileName)

						// Handle filename collision
						counter := 1
						for {
							if _, err := os.Stat(targetPath); os.IsNotExist(err) {
								break // File doesn't exist, so we can use this name
							}

							ext := filepath.Ext(fileName)
							name := fileName[:len(fileName)-len(ext)]
							targetPath = filepath.Join(options.TargetDir,
								fmt.Sprintf("%s_(%d)%s", name, counter, ext))
							counter++
						}

						// Move the file
						if err := os.Rename(file.Path, targetPath); err != nil {
							fmt.Fprintf(os.Stderr, "Error moving file %s: %v\n", file.Path, err)
						} else {
							fmt.Printf("Moved: %s -> %s\n", file.Path, targetPath)
						}
					}
				}
			}
		}

		// Update the group in case files were moved/deleted
		duplicateGroups[i] = group
	}
	return options.BatchMode
}

// Sort a group of duplicate files according to selection mode
func sortDuplicateGroup(group *[]DuplicateFileInfo, mode DuplicateSelectionMode) {
	switch mode {
	case OldestAsOriginal:
		// Sort by creation time ascending (oldest first)
		sort.Slice(*group, func(i, j int) bool {
			return (*group)[i].CreatedTime.Before((*group)[j].CreatedTime)
		})

	case NewestAsOriginal:
		// Sort by creation time descending (newest first)
		sort.Slice(*group, func(i, j int) bool {
			return (*group)[i].CreatedTime.After((*group)[j].CreatedTime)
		})

	case FirstAlphaAsOriginal:
		// Sort alphabetically ascending
		sort.Slice(*group, func(i, j int) bool {
			return (*group)[i].Path < (*group)[j].Path
		})

	case LastAlphaAsOriginal:
		// Sort alphabetically descending
		sort.Slice(*group, func(i, j int) bool {
			return (*group)[i].Path > (*group)[j].Path
		})
	}
}

// OutputResults writes duplicate information to console and file
func OutputResults(result *DuplicateResult, options DuplicateOptions, duplicateGroups [][]DuplicateFileInfo) {
	// Print summary to console
	fmt.Printf("\nDuplicate files summary:\n")
	fmt.Printf("Total files scanned: %d\n", result.TotalFiles)
	fmt.Printf("Duplicate groups: %d\n", result.DuplicateGroups)
	fmt.Printf("Duplicate files: %d\n", result.DuplicateFiles)
	fmt.Printf("Wasted space: %.2f MB\n", float64(result.DuplicateSize)/(1024*1024))
	fmt.Printf("Processing time: %v\n\n", result.ProcessingTime)

	// Output to file if specified
	if options.OutputFileSpecified {
		file, err := os.Create(options.OutputPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
			return
		}
		defer file.Close()

		writer := bufio.NewWriter(file)
		defer writer.Flush()

		// Write header
		fmt.Fprintf(writer, "# Duplicate files report\n")
		fmt.Fprintf(writer, "# Date: %s\n", time.Now().Format(time.RFC1123))
		fmt.Fprintf(writer, "# Root path: %s\n", options.OutputPath)
		fmt.Fprintf(writer, "# Total files: %d\n", result.TotalFiles)
		fmt.Fprintf(writer, "# Duplicate groups: %d\n", result.DuplicateGroups)
		fmt.Fprintf(writer, "# Duplicate files: %d\n", result.DuplicateFiles)
		fmt.Fprintf(writer, "# Wasted space: %.2f MB\n\n", float64(result.DuplicateSize)/(1024*1024))

		// Write each group
		for i, group := range duplicateGroups {
			fmt.Fprintf(writer, "# Group %d (%d files, %.2f MB each)\n",
				i+1, len(group), float64(group[0].Size)/(1024*1024))

			for _, file := range group {
				originalMark := " "
				if file.IsOriginal {
					originalMark = "*"
				}
				fmt.Fprintf(writer, "%s %s\n", originalMark, file.Path)
			}
			fmt.Fprintf(writer, "\n")
		}

		fmt.Printf("Duplicate list saved to: %s\n", options.OutputPath)
	}
}

// ParseArguments parses command line arguments for duplicate processing
func ParseArguments(args []string) DuplicateOptions {
	options := DefaultOptions()

	// Process arguments in any order
	for i := 0; i < len(args); i++ {
		arg := strings.ToLower(args[i])

		// Check for output file specification
		if arg == "list" && i+1 < len(args) {
			options.OutputPath = args[i+1]
			options.OutputFileSpecified = true
			i++ // Skip the next argument as it's the filename
			continue
		}

		// Check for verbosity options
		if arg == "quiet" || arg == "q" || arg == "short" || arg == "s" {
			options.Verbose = false
			continue
		}

		// Check for selection mode options
		switch arg {
		case "old":
			options.SelectionMode = NewestAsOriginal // Keep newest as original, move/delete older files
		case "new":
			options.SelectionMode = OldestAsOriginal // Keep oldest as original, move/delete newer files
		case "abc":
			options.SelectionMode = LastAlphaAsOriginal // Keep last alphabetically as original
		case "xyz":
			options.SelectionMode = FirstAlphaAsOriginal // Keep first alphabetically as original
		case "move":
			if i+1 < len(args) {
				options.Action = MoveAction
				options.TargetDir = args[i+1]
				i++ // Skip the next argument as it's the target directory
			} else {
				fmt.Fprintf(os.Stderr, "Warning: 'move' option specified without a target directory\n")
			}
		case "delete", "del":
			options.Action = DeleteAction
			// If we have a selection mode specified, enable batch mode
			if options.SelectionMode != NewestAsOriginal {
				options.BatchMode = true
			}
		}
	}

	return options
}

// LoadFileList loads a list of files to check from a file
func LoadFileList(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file list: %w", err)
	}
	defer file.Close()

	var files []string
	lineCount := 0
	validLines := 0
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		lineCount++
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			files = append(files, line)
			validLines++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file list: %w", err)
	}

	if validLines == 0 {
		return nil, fmt.Errorf("no valid entries found in file list (total lines: %d)", lineCount)
	}

	return files, nil
}

// ProcessDuplicateGroupsFromList processes duplicate groups loaded from a file
func ProcessDuplicateGroupsFromList(duplicateGroups map[string][]DuplicateFileInfo, options DuplicateOptions) error {
	// Convert map of groups to slice for processing
	var groupsSlice [][]DuplicateFileInfo
	skippedFiles := 0
	totalFiles := 0

	for _, group := range duplicateGroups {
		totalFiles += len(group)
		// Verify files actually exist before processing
		var validFiles []DuplicateFileInfo
		for _, file := range group {
			if stat, err := os.Stat(file.Path); err == nil {
				// Update size and modtime from actual file
				file.Size = stat.Size()
				file.ModTime = stat.ModTime()
				validFiles = append(validFiles, file)
			} else {
				fmt.Printf("Warning: File not found: %s, skipping\n", file.Path)
				skippedFiles++
			}
		}

		// Only include groups with at least 2 files
		if len(validFiles) >= 2 {
			groupsSlice = append(groupsSlice, validFiles)
		}
	}

	if len(groupsSlice) == 0 {
		if skippedFiles > 0 {
			return fmt.Errorf("no valid duplicate groups found (%d files were skipped due to errors)", skippedFiles)
		}
		return fmt.Errorf("no valid duplicate groups found")
	}

	fmt.Printf("Found %d duplicate groups from list (%d of %d files are valid)\n",
		len(groupsSlice), totalFiles-skippedFiles, totalFiles)

	// Process the groups
	ProcessDuplicateGroups(groupsSlice, options)

	return nil
}
