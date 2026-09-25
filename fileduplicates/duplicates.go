package fileduplicates

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
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
	RootPath        string                         // The folder that was scanned, absolute
	Actions         ActionSummary                  // What the delete/move phase did
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

// checkRoot is the first thing a scan does (DUP-16): a root that does not
// exist, is not a folder, or cannot be listed is an error, never "no
// duplicate files found".
func checkRoot(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return fmt.Errorf("cannot scan %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cannot scan %s: not a folder", root)
	}
	f, err := os.Open(root)
	if err != nil {
		return fmt.Errorf("cannot scan %s: %w", root, err)
	}
	defer f.Close()
	if _, err := f.Readdirnames(1); err != nil && err != io.EOF {
		return fmt.Errorf("cannot scan %s: %w", root, err)
	}
	return nil
}

// FindDuplicates finds duplicate files in a directory tree, and deletes or
// moves them when the options say so.
//
// Nothing is touched until every check that can refuse the run has passed:
// the words (Validate), consent (-y or somebody to ask), the root itself, and
// the protected-location guard. The report named by `list` is written for
// every scan that gets that far, even one that finds nothing, so an old list
// never survives a clean rescan (DUP-08).
func FindDuplicates(rootPath string, options DuplicateOptions) (*DuplicateResult, error) {
	startTime := time.Now()

	// Create a result structure
	result := &DuplicateResult{
		Groups: make(map[string][]DuplicateFileInfo),
	}
	rs := newRunState(&options)

	if err := options.Validate(); err != nil {
		return result, err
	}
	if err := options.CheckConsent(); err != nil {
		return result, err
	}
	root, err := filepath.Abs(rootPath)
	if err != nil {
		return result, fmt.Errorf("cannot scan %s: %w", rootPath, err)
	}
	result.RootPath = root
	if err := checkRoot(root); err != nil {
		return result, err
	}
	if options.Action != NoAction {
		if protected, reason := classifyProtected(root); protected {
			if err := rs.confirmProtected(root, reason); err != nil {
				return result, err
			}
		}
		if err := options.prepareTarget(); err != nil {
			return result, err
		}
	}

	// Load hash cache
	cache, err := LoadHashCache()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: %v (starting with an empty cache)\n", err)
	}

	// Create worker pool for hash calculation
	workerCount := GetOptimalWorkerCount()

	if options.Verbose {
		fmt.Printf("Using %d workers for hash calculation\n", workerCount)
	}

	excluded := scanExclusions(root)

	// Maps to track duplicates
	filesBySize := make(map[int64][]DuplicateFileInfo)

	// Scan files
	filesScanned := 0
	unreadable := 0
	fmt.Println("Scanning for duplicates...")

	// Progress tracking variables
	lastProgressUpdate := time.Now()
	progressUpdateInterval := 500 * time.Millisecond

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if rs.stopped() {
			return filepath.SkipAll
		}
		if err != nil {
			if path == root {
				return err
			}
			unreadable++
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil // Skip files with errors
		}

		if d.IsDir() {
			for _, ex := range excluded {
				if strings.EqualFold(path, ex) {
					fmt.Printf("Skipping %s (a system folder; start the scan inside it to include it)\n", path)
					return filepath.SkipDir
				}
			}
			return nil
		}
		// Regular files only: links and reparse points are not followed.
		if !d.Type().IsRegular() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			unreadable++
			return nil
		}
		if info.Size() < MIN_DUPLICATE_FILE_SIZE {
			return nil
		}
		filesScanned++

		// Update progress
		now := time.Now()
		if options.Verbose && now.Sub(lastProgressUpdate) > progressUpdateInterval {
			lastProgressUpdate = now
			fmt.Printf("\rScanning: %s [%d files]", path, filesScanned)
		}

		fileInfo := fileInfoFrom(path, info)
		filesBySize[fileInfo.Size] = append(filesBySize[fileInfo.Size], fileInfo)
		return nil
	})

	if options.Verbose {
		fmt.Println() // End the progress line
	}
	if err != nil {
		return result, fmt.Errorf("error walking %s: %w", root, err)
	}
	if rs.stopped() {
		return result, fmt.Errorf("%w: during the scan; nothing was deleted or moved", ErrStopped)
	}
	result.TotalFiles = filesScanned

	// Candidates: files that share their size with another. Each gets its
	// object identity, and one file under two names (a hard link, or the
	// same file reached twice) is kept once - it is not a duplicate of
	// itself (DUP-06).
	var potentialDuplicates []DuplicateFileInfo
	for _, files := range filesBySize {
		if rs.stopped() {
			return result, fmt.Errorf("%w: during the scan; nothing was deleted or moved", ErrStopped)
		}
		if len(files) < 2 {
			continue
		}
		distinct := collapseSameObjects(identifyAll(files, &unreadable))
		if len(distinct) > 1 {
			potentialDuplicates = append(potentialDuplicates, distinct...)
		}
	}
	if unreadable > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d files or folders could not be read and were left out of the scan.\n", unreadable)
	}

	if len(potentialDuplicates) == 0 {
		fmt.Println("No duplicate files found.")
		result.ProcessingTime = time.Since(startTime)
		return result, finishReport(result, options, nil)
	}

	filesByQuickHash, err := hashStage(rs, cache, workerCount, potentialDuplicates, QuickHash, options.Verbose)
	if err != nil {
		return result, err
	}

	var fullCandidates []DuplicateFileInfo
	for _, files := range filesByQuickHash {
		if len(files) > 1 {
			fullCandidates = append(fullCandidates, files...)
		}
	}
	filesByFullHash, err := hashStage(rs, cache, workerCount, fullCandidates, FullHash, options.Verbose)
	if err != nil {
		return result, err
	}

	// Save cache
	if err := cache.Save(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to save hash cache: %v\n", err)
	}

	// Find true duplicates by full hash
	var duplicateGroups [][]DuplicateFileInfo
	for key, files := range filesByFullHash {
		if len(files) > 1 {
			duplicateGroups = append(duplicateGroups, files)
			result.Groups[key] = files
		}
	}

	// No duplicates found
	if len(duplicateGroups) == 0 {
		fmt.Println("No duplicate files found.")
		result.ProcessingTime = time.Since(startTime)
		return result, finishReport(result, options, nil)
	}

	// Decide the keeper of every group before anything else, so the report
	// says what the action phase is about to do.
	planGroups(duplicateGroups, options.SelectionMode)

	// Calculate statistics
	result.DuplicateGroups = len(duplicateGroups)
	for _, group := range duplicateGroups {
		// Count all but one file in each group as duplicates
		result.DuplicateFiles += len(group) - 1
		result.DuplicateSize += group[0].Size * int64(len(group)-1)
	}
	result.ProcessingTime = time.Since(startTime)

	reportErr := finishReport(result, options, duplicateGroups)

	// Process duplicate groups - apply the action to all but the keeper
	summary, actErr := rs.process(duplicateGroups)
	result.Actions = summary
	result.ProcessingTime = time.Since(startTime)

	if actErr != nil {
		return result, actErr
	}
	return result, reportErr
}

// identifyAll reads the object identity of every file; a file that cannot be
// opened for it is left out (it could not be hashed or compared either).
func identifyAll(files []DuplicateFileInfo, unreadable *int) []DuplicateFileInfo {
	out := files[:0:0]
	for _, f := range files {
		if err := identify(&f); err != nil {
			*unreadable++
			continue
		}
		out = append(out, f)
	}
	return out
}

// collapseSameObjects keeps one entry per object: two names with the same
// volume serial and file index are one file. The alphabetically first name
// represents it, so the choice does not depend on walk order.
func collapseSameObjects(files []DuplicateFileInfo) []DuplicateFileInfo {
	sort.SliceStable(files, func(i, j int) bool { return pathLess(files[i].Path, files[j].Path) })
	type objectKey struct {
		vol uint32
		idx uint64
	}
	seen := make(map[objectKey]bool)
	out := files[:0:0]
	for _, f := range files {
		if f.identified {
			k := objectKey{f.VolSerial, f.FileIndex}
			if seen[k] {
				continue
			}
			seen[k] = true
		}
		out = append(out, f)
	}
	return out
}

// hashStage computes (or takes from the cache) one kind of hash for every
// candidate and returns them grouped by size and hash. Flow: cache lookup
// (read-only) -> misses go to the worker pool -> results are stored in the
// cache and aggregated by a consumer goroutine.
func hashStage(rs *runState, cache *HashCache, workerCount int, candidates []DuplicateFileInfo,
	mode FileHashType, verbose bool) (map[string][]DuplicateFileInfo, error) {

	grouped := make(map[string][]DuplicateFileInfo)
	if len(candidates) == 0 {
		return grouped, nil
	}
	label := "Quick hash"
	every := int64(100)
	if mode == FullHash {
		label = "Full hash"
		every = 50
	}
	if verbose {
		fmt.Printf("%s: %d candidate files...\n", label, len(candidates))
	}

	keyOf := func(f DuplicateFileInfo) string {
		h := f.QuickHash
		if mode == FullHash {
			h = f.FullHash
		}
		return fmt.Sprintf("%d:%s", f.Size, h)
	}

	var mu sync.Mutex
	processed := int64(0)
	total := int64(len(candidates))
	stageStart := time.Now()
	report := func() {
		n := atomic.AddInt64(&processed, 1)
		if verbose && n%every == 0 {
			elapsed := time.Since(stageStart)
			percent := float64(n) / float64(total) * 100
			timePerFile := elapsed.Seconds() / float64(n)
			eta := formatETA(time.Duration(timePerFile*float64(total-n)) * time.Second)
			fmt.Printf("%s progress: %d/%d (%.1f%%, ETA: %s)\r", label, n, total, percent, eta)
		}
	}

	worker := NewHashWorker(workerCount)
	worker.stop = rs.stopped

	// Consumer goroutine: started BEFORE submitting so workers never block on a
	// full results channel (which would otherwise deadlock the submit loop).
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for result := range worker.results {
			if result.err == nil {
				cache.StoreHash(result.file, mode)
				mu.Lock()
				k := keyOf(result.file)
				grouped[k] = append(grouped[k], result.file)
				mu.Unlock()
			}
			report()
		}
	}()

	for _, file := range candidates {
		if rs.stopped() {
			break
		}
		fileCopy := file
		// Read-only cache lookup; never compute on the main goroutine.
		if h, found := cache.LookupHash(fileCopy, mode); found {
			if mode == QuickHash {
				fileCopy.QuickHash = h
			} else {
				fileCopy.FullHash = h
			}
			mu.Lock()
			k := keyOf(fileCopy)
			grouped[k] = append(grouped[k], fileCopy)
			mu.Unlock()
			report()
		} else {
			// Cache miss: let a worker compute the hash.
			worker.AddJob(fileCopy, mode)
		}
	}

	// Wait for all queued jobs, then for the consumer to drain all results.
	worker.Wait()
	<-consumerDone

	if rs.stopped() {
		return nil, fmt.Errorf("%w: while hashing; nothing was deleted or moved", ErrStopped)
	}
	if verbose {
		fmt.Printf("%s progress: %d/%d (100.0%%) - Complete\n", label, total, total)
	}
	return grouped, nil
}

// finishReport prints the console summary (unless quiet) and writes the
// report file whenever one was asked for - quiet or not, duplicates or not.
func finishReport(result *DuplicateResult, options DuplicateOptions, groups [][]DuplicateFileInfo) error {
	if options.Verbose {
		printSummary(result)
	}
	if !options.OutputFileSpecified {
		return nil
	}
	if err := writeReport(result, options, groups); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing the duplicate list %s: %v\n", options.OutputPath, err)
		return fmt.Errorf("cannot write the duplicate list %s: %w", options.OutputPath, err)
	}
	fmt.Printf("Duplicate list saved to: %s\n", options.OutputPath)
	return nil
}

func printSummary(result *DuplicateResult) {
	fmt.Printf("\nDuplicate files summary:\n")
	fmt.Printf("Total files scanned: %d\n", result.TotalFiles)
	fmt.Printf("Duplicate groups: %d\n", result.DuplicateGroups)
	fmt.Printf("Duplicate files: %d\n", result.DuplicateFiles)
	fmt.Printf("Wasted space: %.2f MB\n", float64(result.DuplicateSize)/(1024*1024))
	fmt.Printf("Processing time: %v\n\n", result.ProcessingTime)
}

// writeReport writes the duplicate list: the header, then one block per
// group with the kept copy marked `*`. An empty scan writes the header with
// zero groups, which replaces any older list at that path.
func writeReport(result *DuplicateResult, options DuplicateOptions, groups [][]DuplicateFileInfo) error {
	file, err := os.Create(options.OutputPath)
	if err != nil {
		return err
	}
	writer := bufio.NewWriter(file)

	// Write header
	fmt.Fprintf(writer, "# Duplicate files report\n")
	fmt.Fprintf(writer, "# Date: %s\n", time.Now().Format(time.RFC1123))
	fmt.Fprintf(writer, "# Root path: %s\n", result.RootPath)
	fmt.Fprintf(writer, "# Total files: %d\n", result.TotalFiles)
	fmt.Fprintf(writer, "# Duplicate groups: %d\n", len(groups))
	fmt.Fprintf(writer, "# Duplicate files: %d\n", result.DuplicateFiles)
	fmt.Fprintf(writer, "# Wasted space: %.2f MB\n\n", float64(result.DuplicateSize)/(1024*1024))

	// Write each group
	for i, group := range groups {
		fmt.Fprintf(writer, "# Group %d (%d files, %.2f MB each)\n",
			i+1, len(group), float64(group[0].Size)/(1024*1024))

		for _, f := range group {
			originalMark := " "
			if f.IsOriginal {
				originalMark = "*"
			}
			fmt.Fprintf(writer, "%s %s\n", originalMark, f.Path)
		}
		fmt.Fprintf(writer, "\n")
	}

	if err := writer.Flush(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// OutputResults writes duplicate information to console and file
func OutputResults(result *DuplicateResult, options DuplicateOptions, duplicateGroups [][]DuplicateFileInfo) {
	_ = finishReport(result, options, duplicateGroups)
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

// ProcessDuplicateGroupsFromList processes duplicate groups loaded from a
// file. A list is a claim, possibly stale and possibly hand-edited, so
// nothing in it is trusted (DUP-01): every entry is looked up again, the same
// file named twice (the same path, a case variant, a hard link) counts once,
// groups that share a file are merged, and each removal is preceded by the
// byte comparison with the kept copy that ProcessDuplicateGroups makes.
func ProcessDuplicateGroupsFromList(duplicateGroups map[string][]DuplicateFileInfo, options DuplicateOptions) error {
	rs := newRunState(&options)
	if err := options.Validate(); err != nil {
		return err
	}
	if err := options.CheckConsent(); err != nil {
		return err
	}

	keys := make([]string, 0, len(duplicateGroups))
	for k := range duplicateGroups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	skippedFiles := 0
	totalFiles := 0
	var groupsSlice [][]DuplicateFileInfo
	for _, k := range keys {
		totalFiles += len(duplicateGroups[k])
		var validFiles []DuplicateFileInfo
		for _, entry := range duplicateGroups[k] {
			info, err := os.Stat(entry.Path)
			if err != nil || !info.Mode().IsRegular() {
				if err == nil {
					err = errors.New("not a regular file")
				}
				fmt.Printf("Warning: %s: %v, skipping\n", entry.Path, err)
				skippedFiles++
				continue
			}
			f := fileInfoFrom(entry.Path, info)
			f.FullHash = entry.FullHash
			if err := identify(&f); err != nil {
				fmt.Printf("Warning: cannot identify %s: %v, skipping\n", entry.Path, err)
				skippedFiles++
				continue
			}
			validFiles = append(validFiles, f)
		}
		groupsSlice = append(groupsSlice, validFiles)
	}

	groupsSlice = mergeGroupsSharingFiles(groupsSlice)
	var usable [][]DuplicateFileInfo
	for _, g := range groupsSlice {
		g = collapseSameObjects(g)
		if len(g) >= 2 {
			usable = append(usable, g)
		}
	}

	if len(usable) == 0 {
		if skippedFiles > 0 {
			return fmt.Errorf("no valid duplicate groups found (%d files were skipped due to errors)", skippedFiles)
		}
		return fmt.Errorf("no valid duplicate groups found")
	}

	fmt.Printf("Found %d duplicate groups from list (%d of %d files are valid)\n",
		len(usable), totalFiles-skippedFiles, totalFiles)

	// Process the groups: a listed file inside the Windows folder or Program
	// Files needs a person to confirm first (DUP-06).
	_, err := rs.guardAndProcess(usable)
	return err
}

// mergeGroupsSharingFiles joins groups that name the same file (by object
// identity): a file listed in two groups makes them one claim, and handling
// them apart could let each group delete the other's keeper.
func mergeGroupsSharingFiles(groups [][]DuplicateFileInfo) [][]DuplicateFileInfo {
	parent := make([]int, len(groups))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	type objectKey struct {
		vol uint32
		idx uint64
	}
	owner := make(map[objectKey]int)
	for gi, g := range groups {
		for _, f := range g {
			if !f.identified {
				continue
			}
			k := objectKey{f.VolSerial, f.FileIndex}
			if other, ok := owner[k]; ok {
				a, b := find(gi), find(other)
				if a != b {
					parent[a] = b
				}
			} else {
				owner[k] = gi
			}
		}
	}
	merged := make(map[int][]DuplicateFileInfo)
	var order []int
	for gi, g := range groups {
		r := find(gi)
		if _, ok := merged[r]; !ok {
			order = append(order, r)
		}
		merged[r] = append(merged[r], g...)
	}
	out := make([][]DuplicateFileInfo, 0, len(order))
	for _, r := range order {
		out = append(out, merged[r])
	}
	return out
}
