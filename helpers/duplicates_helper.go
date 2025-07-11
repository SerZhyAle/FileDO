package helpers

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"filedo/fileduplicates"
)

// CheckDuplicatesFromFile handles duplicate checking from a file list
// Format of the file list should be:
// <hash>|<path>|<size>|<modtime>
// Where:
//   - hash is the full hash of the file
//   - path is the absolute path to the file
//   - size is the file size in bytes (optional)
//   - modtime is the modification time in format "2006-01-02 15:04:05" (optional)
//
// Or the format created by FileDO's duplicate check command:
//   - path/to/file (original file)
//     path/to/duplicate (duplicate file)
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

	// Determine file format based on extension or content
	isStandardFormat := false
	if strings.HasSuffix(strings.ToLower(filePath), ".lst") {
		// Assume it's a FileDO duplicate list format
		duplicateGroups, err := readDuplicateListFormat(filePath)
		if err != nil {
			return fmt.Errorf("error reading duplicate list: %v", err)
		}

		// Process the duplicate groups with the provided options
		return fileduplicates.ProcessDuplicateGroupsFromList(duplicateGroups, options)
	} else {
		isStandardFormat = true
	}

	// Standard format processing (hash|path|size|modtime)
	if isStandardFormat {
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
				if size, err := strconv.ParseInt(parts[2], 10, 64); err == nil {
					fileInfo.Size = size
				} else {
					fmt.Printf("Warning: Invalid size format in %s: %v\n", filePath, err)
				}
			}

			// Try to parse modtime if available
			if len(parts) > 3 {
				// Try to parse modtime in format like "2023-05-15 14:30:45"
				modTime, err := time.Parse("2006-01-02 15:04:05", parts[3])
				if err == nil {
					fileInfo.ModTime = modTime
				} else {
					fmt.Printf("Warning: Invalid modtime format in %s: %v\n", filePath, err)
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

	return nil
}

// readDuplicateListFormat reads a FileDO duplicate list file
// Format:
// # Group 1 (2 files, 0.01 MB each)
//   - path/to/file (original file)
//     path/to/duplicate (duplicate file)
func readDuplicateListFormat(filePath string) (map[string][]fileduplicates.DuplicateFileInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open duplicate list: %w", err)
	}
	defer file.Close()

	duplicateGroups := make(map[string][]fileduplicates.DuplicateFileInfo)
	var currentGroup []fileduplicates.DuplicateFileInfo
	var currentHash string
	groupIndex := 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and header lines
		if line == "" || strings.HasPrefix(line, "# ") {
			// Check if this is a new group header
			if strings.Contains(line, "Group") && strings.Contains(line, "files") {
				// New group, save the previous one if it exists
				if len(currentGroup) > 1 {
					groupIndex++
					duplicateGroups[fmt.Sprintf("group_%d", groupIndex)] = currentGroup
				}
				currentGroup = []fileduplicates.DuplicateFileInfo{}
				currentHash = fmt.Sprintf("group_%d", groupIndex+1)
			}
			continue
		}

		// Parse file entry lines
		isOriginal := strings.HasPrefix(line, "*")
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		// Extract path and creation time if available
		path := line

		// If there's a modification time in parentheses, extract it
		if i := strings.LastIndex(line, "(modified:"); i > 0 {
			path = strings.TrimSpace(line[:i])
		}

		// Get absolute path if the path is relative
		if !filepath.IsAbs(path) {
			// Check if file exists as is
			if _, err := os.Stat(path); err != nil {
				// Try prepending the directory of the list file
				dir := filepath.Dir(filePath)
				altPath := filepath.Join(dir, path)
				if _, err := os.Stat(altPath); err == nil {
					path = altPath
				}
			}
		}

		// Check if the file exists and get its info
		info, err := os.Stat(path)
		if err != nil {
			fmt.Printf("Warning: Cannot access file %s: %v\n", path, err)
			continue
		}

		// Create duplicate file info
		fileInfo := fileduplicates.DuplicateFileInfo{
			Path:       path,
			Size:       info.Size(),
			ModTime:    info.ModTime(),
			IsOriginal: isOriginal,
			FullHash:   currentHash, // Use group index as hash since we don't have actual hash
		}

		currentGroup = append(currentGroup, fileInfo)
	}

	// Add the last group if it exists
	if len(currentGroup) > 1 {
		duplicateGroups[currentHash] = currentGroup
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading duplicate list: %w", err)
	}

	if len(duplicateGroups) == 0 {
		return nil, fmt.Errorf("no duplicate groups found in file")
	}

	return duplicateGroups, nil
}
