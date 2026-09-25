// Package fileduplicates provides functionality for finding and managing duplicate files
package fileduplicates

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"time"
)

// Constants for duplicate file processing
const (
	MIN_DUPLICATE_FILE_SIZE = 16                // Minimum file size to consider (in bytes)
	QUICK_HASH_SIZE         = 4096              // Size for quick hash sample (4KB)
	MAX_WORKERS             = 24                // Maximum concurrent hash workers
	HASH_CACHE_FILE         = "hash_cache.json" // Filename for hash cache
)

// hashAlgo names the digest every cache entry was computed with. An entry
// carrying another name (the MD5 of older builds) is never trusted.
const hashAlgo = "sha256"

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
	CreatedTime time.Time // When the file was created (the creation time on Windows)
	ModTime     time.Time // When the file was last modified
	IsOriginal  bool      // Whether this file is considered the original

	// The object behind the name (DUP-05, DUP-06). Two paths with the same
	// volume serial and file index are one file - a hard link or a second
	// spelling - and never a pair of duplicates. ChangeTime moves on every
	// write, including one that restores the size and the modification time.
	VolSerial  uint32
	FileIndex  uint64
	ChangeTime time.Time
	identified bool
}

// HashCache stores file hashes for reuse between runs
type HashCache struct {
	Entries  map[string]CacheEntry
	mutex    sync.RWMutex
	loadedAt time.Time
}

// CacheEntry represents a single cached hash entry.
//
// The key is the absolute path. A cached hash is only valid while the file on
// disk still has the same size and modification time (the permanent
// invariant, and only its floor) and the same volume serial, file index and
// change time, and while the entry was computed with the current digest.
// Nothing is ever deleted on the strength of a cached hash alone: the files
// are compared byte for byte first.
type CacheEntry struct {
	Path       string
	Size       int64
	ModTime    time.Time
	QuickHash  string
	FullHash   string
	LastSeen   time.Time
	VolSerial  uint32
	FileIndex  uint64
	ChangeTime time.Time
	Algo       string
}

// Worker pool for parallel hash calculation
type HashWorker struct {
	jobs        chan hashJob
	results     chan hashResult
	workerCount int
	wg          sync.WaitGroup
	stop        func() bool
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
	SelectionModeSet    bool                   // Whether a rule word (old/new/abc/xyz) was given
	Action              DuplicateAction        // What to do with duplicates
	TargetDir           string                 // Where to move duplicates
	IsDevice            bool                   // Whether root path is a device
	BatchMode           bool                   // -y / --yes: no per-file prompt

	// Interactive says a person can answer a prompt on Input. The caller
	// decides it (a console, and no machine stop channel); the default is
	// false, so a deleting run with no -y and nobody to ask is refused.
	Interactive bool
	// Input is where answers are read; nil means os.Stdin.
	Input io.Reader
	// Context and Stop end the run early: both are checked in the walk, in
	// the hashing loops and before every delete or move.
	Context context.Context
	Stop    func() bool

	// Words ParseArguments could not accept, kept for Validate.
	problems []string
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

// ErrStopped reports a run that was asked to stop before it finished. What it
// already deleted or moved is in the message and in the result.
var ErrStopped = errors.New("stopped before the work was finished")

// UsageError is a run refused before it touched anything: the words were
// wrong, or the run would delete with nobody to ask and no -y.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// IsUsageError reports whether err is (or wraps) a UsageError.
func IsUsageError(err error) bool {
	var ue *UsageError
	return errors.As(err, &ue)
}

// GetOptimalWorkerCount returns optimal number of workers based on CPU cores
func GetOptimalWorkerCount() int {
	cores := runtime.NumCPU()
	workers := cores - 1
	if workers < 2 {
		workers = 2
	}
	if workers > MAX_WORKERS {
		workers = MAX_WORKERS
	}
	return workers
}
