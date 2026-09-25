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
//   - path is the path to the file; a relative path is relative to the list
//   - size is the file size in bytes (optional)
//   - modtime is the modification time in format "2006-01-02 15:04:05" (optional)
//
// Or the format created by FileDO's duplicate check command (a `.lst` file):
//
//	# Group 1 (2 files, 0.01 MB each)
//	* path/to/file (the kept copy when the list was written)
//	  path/to/duplicate
//
// The list is a claim, not a proof: fileduplicates re-reads every entry,
// counts one file under two names once, and compares each duplicate byte for
// byte with the kept copy right before it is removed (DUP-01). The rule and
// the prompting are exactly the scan's - one default rule, and no question per
// file only with -y (DUP-02, DUP-03).
//
// configure runs on the parsed options before anything is read; the CLI uses
// it to say whether a person can answer prompts and how a stop is requested.
func CheckDuplicatesFromFile(args []string, configure ...func(*fileduplicates.DuplicateOptions)) error {
	// Check if we have the right format
	if len(args) < 3 || !strings.EqualFold(args[0], "from") || !strings.EqualFold(args[1], "list") {
		return &fileduplicates.UsageError{Msg: "invalid format. Use: cd from list <file_path> [options]"}
	}

	// Process duplicates from a file list
	filePath := args[2]

	// Skip the first three arguments (from, list, file_path)
	options := fileduplicates.ParseArguments(args[3:])
	for _, c := range configure {
		if c != nil {
			c(&options)
		}
	}
	if err := options.Validate(); err != nil {
		return err
	}
	// The same refusal the scan makes, before the list is even opened.
	if err := options.CheckConsent(); err != nil {
		return err
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("cannot open the duplicate list %s: %w", filePath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("the duplicate list %s is a folder, not a file", filePath)
	}

	// Determine file format based on extension
	var duplicateGroups map[string][]fileduplicates.DuplicateFileInfo
	if strings.HasSuffix(strings.ToLower(filePath), ".lst") {
		// A FileDO duplicate list
		duplicateGroups, err = readDuplicateListFormat(filePath)
		if err != nil {
			return fmt.Errorf("error reading duplicate list: %w", err)
		}
	} else {
		duplicateGroups, err = readHashListFormat(filePath)
		if err != nil {
			return err
		}
	}

	// Process the duplicate groups with the provided options
	return fileduplicates.ProcessDuplicateGroupsFromList(duplicateGroups, options)
}

// resolveListedPath makes a listed path absolute. A relative path is relative
// to the folder holding the list - never to the current directory, which is
// wherever the command happens to be run from (DUP-01). A path that is
// neither absolute nor plainly relative (`C:name`, `\name`) is refused.
func resolveListedPath(listFile, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("empty path")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if filepath.VolumeName(p) != "" || strings.HasPrefix(p, `\`) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("%q is neither absolute nor relative to the list", p)
	}
	listAbs, err := filepath.Abs(listFile)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(listAbs), p), nil
}

// readHashListFormat reads the `<hash>|<path>|<size>|<modtime>` format and
// groups the entries by hash. Size and modtime are informational: the files
// are looked up again before anything is done with them.
func readHashListFormat(filePath string) (map[string][]fileduplicates.DuplicateFileInfo, error) {
	files, err := fileduplicates.LoadFileList(filePath)
	if err != nil {
		return nil, fmt.Errorf("error loading file list: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no files found in list %s", filePath)
	}

	fmt.Printf("Processing %d files from list: %s\n", len(files), filePath)

	// Group files by hash
	duplicateGroups := make(map[string][]fileduplicates.DuplicateFileInfo)

	// Expected format of each line: <hash>|<path>|<size>|<modtime>
	for _, line := range files {
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			continue // Skip invalid entries
		}

		hash := strings.TrimSpace(parts[0])
		path, err := resolveListedPath(filePath, parts[1])
		if err != nil {
			fmt.Printf("Warning: %s: %v, skipping\n", filePath, err)
			continue
		}

		// Create file info
		fileInfo := fileduplicates.DuplicateFileInfo{
			Path:     path,
			FullHash: hash,
		}

		// Try to parse size if available
		if len(parts) > 2 {
			if size, err := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64); err == nil {
				fileInfo.Size = size
			} else {
				fmt.Printf("Warning: Invalid size format in %s: %v\n", filePath, err)
			}
		}

		// Try to parse modtime if available
		if len(parts) > 3 {
			// Try to parse modtime in format like "2023-05-15 14:30:45"
			modTime, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(parts[3]))
			if err == nil {
				fileInfo.ModTime = modTime
			} else {
				fmt.Printf("Warning: Invalid modtime format in %s: %v\n", filePath, err)
			}
		}

		// Add to groups
		duplicateGroups[hash] = append(duplicateGroups[hash], fileInfo)
	}

	// Filter out non-duplicates (groups with only one entry)
	for hash, group := range duplicateGroups {
		if len(group) <= 1 {
			delete(duplicateGroups, hash)
		}
	}
	if len(duplicateGroups) == 0 {
		return nil, fmt.Errorf("no valid duplicate groups found in %s", filePath)
	}
	return duplicateGroups, nil
}

// isGroupHeader recognises the `# Group N (..)` line that opens a group.
func isGroupHeader(line string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "# group ")
}

// readDuplicateListFormat reads a FileDO duplicate list file
// Format:
// # Group 1 (2 files, 0.01 MB each)
// * path/to/file (original file)
//
//	path/to/duplicate (duplicate file)
//
// An entry before the first `# Group` header means the list is not in this
// format (or lost a header while being edited), and the whole list is
// refused: guessing where one group ends would merge two.
func readDuplicateListFormat(filePath string) (map[string][]fileduplicates.DuplicateFileInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open duplicate list: %w", err)
	}
	defer file.Close()

	duplicateGroups := make(map[string][]fileduplicates.DuplicateFileInfo)
	var currentGroup []fileduplicates.DuplicateFileInfo
	currentKey := ""
	groupIndex := 0
	lineNo := 0

	flush := func() {
		if currentKey != "" && len(currentGroup) > 1 {
			duplicateGroups[currentKey] = currentGroup
		}
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()

		if isGroupHeader(line) {
			flush()
			groupIndex++
			currentKey = fmt.Sprintf("group_%06d", groupIndex)
			currentGroup = nil
			continue
		}
		// Skip empty lines and comment lines
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		if currentKey == "" {
			return nil, fmt.Errorf("line %d: an entry before the first '# Group' header - the list is not in FileDO's format", lineNo)
		}

		// Parse file entry lines
		trimmed := strings.TrimSpace(line)
		isOriginal := strings.HasPrefix(trimmed, "*")
		entry := strings.TrimSpace(strings.TrimPrefix(trimmed, "*"))

		// If there's a modification time in parentheses, drop it
		if i := strings.LastIndex(entry, "(modified:"); i > 0 {
			entry = strings.TrimSpace(entry[:i])
		}

		path, err := resolveListedPath(filePath, entry)
		if err != nil {
			fmt.Printf("Warning: line %d: %v, skipping\n", lineNo, err)
			continue
		}

		// Check if the file exists
		if _, err := os.Stat(path); err != nil {
			fmt.Printf("Warning: Cannot access file %s: %v\n", path, err)
			continue
		}

		currentGroup = append(currentGroup, fileduplicates.DuplicateFileInfo{
			Path:       path,
			IsOriginal: isOriginal,
			FullHash:   currentKey, // The list carries no hash; the group is the claim
		})
	}
	flush()

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading duplicate list: %w", err)
	}

	if len(duplicateGroups) == 0 {
		return nil, fmt.Errorf("no duplicate groups found in file")
	}

	return duplicateGroups, nil
}
