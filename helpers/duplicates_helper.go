package helpers

import (
	"fmt"
	"strings"
	"time"

	"filedo/pkg/fileduplicates"
)

// CheckDuplicatesFromFile handles duplicate checking from a file list
// Format of the file list should be:
// <hash>|<path>|<size>|<modtime>
// Where:
//   - hash is the full hash of the file
//   - path is the absolute path to the file
//   - size is the file size in bytes (optional)
//   - modtime is the modification time in format "2006-01-02 15:04:05" (optional)
func CheckDuplicatesFromFile(args []string) error {
	// Check if we have the right format
	if len(args) < 3 || args[0] != "from" || args[1] != "list" {
		return fmt.Errorf("invalid format. Use: cd from list <file_path> [options]")
	}

	// Process duplicates from a file list
	filePath := args[2]

	// Skip the first three arguments (from, list, file_path)
	options := fileduplicates.ParseArguments(args[3:])

	// If deletion is requested but no selection mode is specified, use "new" as default
	if options.Action == fileduplicates.DeleteAction &&
		options.SelectionMode == fileduplicates.NewestAsOriginal {
		// Set to OldestAsOriginal which is "new" in user interface terms
		// (keep oldest, delete newer files)
		options.SelectionMode = fileduplicates.OldestAsOriginal
		options.BatchMode = true
	}

	// Load file list
	files, err := fileduplicates.LoadFileList(filePath)
	if err != nil {
		return fmt.Errorf("error loading file list: %v", err)
	}

	if len(files) == 0 {
		return fmt.Errorf("no files found in list %s", filePath)
	}

	fmt.Printf("Processing %d files from list: %s\n", len(files), filePath)

	// Group files by hash
	duplicateGroups := make(map[string][]fileduplicates.DuplicateFileInfo)

	// Parse the loaded file list
	// Expected format of each line: <hash>|<path>|<size>|<modtime>
	for _, line := range files {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue // Skip invalid entries
		}

		hash := parts[0]
		path := parts[1]

		// Create file info
		fileInfo := fileduplicates.DuplicateFileInfo{
			Path:     path,
			FullHash: hash,
		}

		// Try to parse size if available
		if len(parts) > 2 {
			var size int64
			fmt.Sscanf(parts[2], "%d", &size)
			fileInfo.Size = size
		}

		// Try to parse modtime if available
		if len(parts) > 3 {
			// Try to parse modtime in format like "2023-05-15 14:30:45"
			modTime, err := time.Parse("2006-01-02 15:04:05", parts[3])
			if err == nil {
				fileInfo.ModTime = modTime
			}
		}

		// Add to groups
		duplicateGroups[hash] = append(duplicateGroups[hash], fileInfo)
	}

	// Filter out non-duplicates (groups with only one file)
	for hash, group := range duplicateGroups {
		if len(group) <= 1 {
			delete(duplicateGroups, hash)
		}
	}

	// Process the duplicate groups with the provided options
	return fileduplicates.ProcessDuplicateGroupsFromList(duplicateGroups, options)
}
