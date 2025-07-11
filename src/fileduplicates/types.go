// Package fileduplicates provides functionality for finding and managing duplicate files
package fileduplicates

import (
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

// Options for duplicate file processing
type DuplicateOptions struct {
	OutputPath          string                 // Path to output file
	OutputFileSpecified bool                   // Whether output file was specified
	Verbose             bool                   // Whether to print verbose output
	SelectionMode       DuplicateSelectionMode // How to select original files
	Action              DuplicateAction        // What to do with duplicates
	TargetDir           string                 // Where to move duplicates
	IsDevice            bool                   // Whether root path is a device
	BatchMode           bool                   // Whether to skip confirmation prompts
}

// Default options for duplicate processing
func DefaultOptions() DuplicateOptions {
	return DuplicateOptions{
		OutputPath:          "duplicates.lst",
		OutputFileSpecified: false,
		Verbose:             true,
		SelectionMode:       NewestAsOriginal,
		Action:              NoAction,
		TargetDir:           "",
		IsDevice:            false,
		BatchMode:           false,
	}
}
