package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DamagedDiskConfig holds the settings for copying from a damaged disk.
type DamagedDiskConfig struct {
	FileTimeout time.Duration // no-progress watchdog per file (default 10 s)
	RetryCount  int           // attempts per file
	UseSkipList bool          // honour and extend the skip list
	BufferSize  int           // read buffer (small, to stress the disk less)
	Quiet       bool          // no per-file console lines
}

// DamagedFileInfo describes one file this run recorded as damaged.
type DamagedFileInfo struct {
	FilePath    string    `json:"filePath"`
	Reason      string    `json:"reason"`
	Timestamp   time.Time `json:"timestamp"`
	Size        int64     `json:"size"`
	AttemptNum  int       `json:"attemptNum"`
	ErrorDetail string    `json:"errorDetail,omitempty"`
}

// DamagedDiskHandler is the damaged-disk side of the copy engines (safecopy,
// and plain copy's damage handling). It owns the copy skip list, which lives
// in the state root and names a source by path, size and modification time
// (SP-0027 COPY-05, CHK-04, CHK-10): a file that changed is tried again, and
// check's own findings no longer land here.
//
// A file is recorded only when the copy itself proved the source bad - it
// stalled with no byte moving for FileTimeout, or the device returned an I/O
// error. A locked file, a denied one or a full target is not the source's
// fault and is never recorded (SP-0023 theme T3).
type DamagedDiskHandler struct {
	config DamagedDiskConfig
	list   *fileStateList

	mutex        sync.Mutex
	damagedFiles []DamagedFileInfo
	listSkips    int
}

// NewDamagedDiskConfig is the default configuration. The watchdog can be
// changed with FILEDO_TIMEOUT_NOPROGRESS_SECONDS.
func NewDamagedDiskConfig() DamagedDiskConfig {
	timeout := 10 * time.Second
	if v := os.Getenv("FILEDO_TIMEOUT_NOPROGRESS_SECONDS"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 {
			timeout = n
		}
	}
	return DamagedDiskConfig{
		FileTimeout: timeout,
		RetryCount:  1,
		UseSkipList: true,
		BufferSize:  64 * 1024,
	}
}

// NewDamagedDiskHandler loads the skip list once for the run.
func NewDamagedDiskHandler() (*DamagedDiskHandler, error) {
	return newDamagedDiskHandler(false)
}

// NewDamagedDiskHandlerQuiet is NewDamagedDiskHandler without the per-file
// console lines.
func NewDamagedDiskHandlerQuiet() (*DamagedDiskHandler, error) {
	return newDamagedDiskHandler(true)
}

func newDamagedDiskHandler(quiet bool) (*DamagedDiskHandler, error) {
	config := NewDamagedDiskConfig()
	config.Quiet = quiet
	list, err := openStateList(copySkipListName, true)
	h := &DamagedDiskHandler{config: config, list: list}
	if err != nil {
		// The run goes on without a persistent list; it only forgets.
		return h, fmt.Errorf("cannot open the skip list: %w", err)
	}
	if !quiet {
		if n := list.Len(); n > 0 {
			fmt.Printf("Loaded %d previously damaged files from %s\n", n, list.Path())
		}
		if n := list.LegacyIgnored(); n > 0 {
			fmt.Printf("Note: %d older skip-list entries carry no size or time and are not trusted - those files will be tried again.\n", n)
		}
	}
	return h, nil
}

// Close releases the list.
func (h *DamagedDiskHandler) Close() error {
	if h == nil {
		return nil
	}
	return h.list.Close()
}

// SkipListPath is where the skip list lives.
func (h *DamagedDiskHandler) SkipListPath() string {
	if h == nil || h.list == nil {
		return ""
	}
	return h.list.Path()
}

// ShouldSkip reports whether the skip list names this exact source file.
func (h *DamagedDiskHandler) ShouldSkip(path string, info os.FileInfo) bool {
	if h == nil || !h.config.UseSkipList {
		return false
	}
	if h.list.HasInfo(path, info) {
		h.mutex.Lock()
		h.listSkips++
		h.mutex.Unlock()
		return true
	}
	return false
}

// LogDamagedFile records a source the copy proved unreadable and appends it
// to the skip list at once.
func (h *DamagedDiskHandler) LogDamagedFile(filePath, reason string, info os.FileInfo, attemptNum int, errorDetail string) {
	if h == nil {
		return
	}
	var size int64
	var mod time.Time
	if info != nil {
		size, mod = info.Size(), info.ModTime()
	}
	h.mutex.Lock()
	h.damagedFiles = append(h.damagedFiles, DamagedFileInfo{
		FilePath: filePath, Reason: reason, Timestamp: time.Now(),
		Size: size, AttemptNum: attemptNum, ErrorDetail: errorDetail,
	})
	count := len(h.damagedFiles)
	h.mutex.Unlock()

	if h.config.UseSkipList && info != nil {
		if err := h.list.Add(filePath, size, mod); err != nil {
			fmt.Printf("Warning: cannot append to the skip list %s: %v\n", h.list.Path(), err)
		}
	}
	if !h.config.Quiet {
		fmt.Printf("\nDAMAGED: %s (%s) - recorded in the skip list | this run: %d\n", filePath, reason, count)
	}
}

// GetDamagedStats returns the files recorded this run and their total size.
func (h *DamagedDiskHandler) GetDamagedStats() (int, int64) {
	h.mutex.Lock()
	defer h.mutex.Unlock()
	var total int64
	for _, d := range h.damagedFiles {
		total += d.Size
	}
	return len(h.damagedFiles), total
}

// damagedSourceError is a source the copy proved unreadable.
type damagedSourceError struct {
	reason string
	err    error
}

func (e *damagedSourceError) Error() string {
	return fmt.Sprintf("damaged source (%s): %v", e.reason, e.err)
}

func (e *damagedSourceError) Unwrap() error { return e.err }

// CopyFileWithDamageHandling copies one file with the damaged-disk watchdog.
// It returns nil when the file is at the target, a *damagedSourceError when
// the source proved unreadable (and was recorded), and any other error as it
// came - a locked source, a full target, a stop.
func (h *DamagedDiskHandler) CopyFileWithDamageHandling(ctx context.Context, sourcePath, targetPath string, sourceInfo os.FileInfo, replace bool, onBytes func(int64), handler *InterruptHandler) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return fmt.Errorf("cannot create the target folder: %w", err)
	}
	attempts := h.config.RetryCount
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		buf, put := takeCopyBuffer(h.config.BufferSize)
		err := copyOneFile(ctx, sourcePath, sourceInfo, targetPath, fileCopyOptions{
			Buffer:     buf,
			NoProgress: h.config.FileTimeout,
			Replace:    replace,
			OnBytes:    onBytes,
			Handler:    handler,
			Release:    put,
		})
		if err == nil {
			return nil
		}
		if isStopError(err) || errors.Is(err, errTooManyStalledReads) {
			return err
		}
		lastErr = err
		reason := ""
		switch {
		case isCopyStall(err):
			reason = "timeout"
		case isDeviceIOError(err):
			reason = "I/O error"
		default:
			// Not the source's fault: reported by the caller, never recorded.
			return err
		}
		if attempt >= attempts {
			h.LogDamagedFile(sourcePath, reason, sourceInfo, attempt, err.Error())
			return &damagedSourceError{reason: reason, err: err}
		}
		fmt.Printf("Retry %d/%d for %s (%s)\n", attempt, attempts, sourcePath, reason)
		select {
		case <-ctx.Done():
			return errRunStopped
		case <-time.After(time.Second):
		}
	}
	return lastErr
}

// PrintSummary prints what this run found and where the list is.
func (h *DamagedDiskHandler) PrintSummary() {
	if h == nil {
		return
	}
	damagedCount, damagedSize := h.GetDamagedStats()
	h.mutex.Lock()
	listSkips := h.listSkips
	h.mutex.Unlock()
	if damagedCount == 0 && listSkips == 0 {
		return
	}
	fmt.Print("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("DAMAGED DISK COPY SUMMARY\n")
	fmt.Print(strings.Repeat("=", 60) + "\n")
	if listSkips > 0 {
		fmt.Printf("Skipped because the skip list names them: %d\n", listSkips)
	}
	if damagedCount > 0 {
		fmt.Printf("Found damaged in this run: %d (%s)\n", damagedCount, formatDiskFileSize(damagedSize))
	}
	fmt.Printf("Skip list: %s\n", h.SkipListPath())
	fmt.Printf("A file that changes (size or time) is tried again; to retry every file, delete the list.\n")
	if damagedCount > 0 {
		fmt.Printf("The no-progress timeout is %v (FILEDO_TIMEOUT_NOPROGRESS_SECONDS changes it).\n", h.config.FileTimeout)
	}
	fmt.Print(strings.Repeat("=", 60) + "\n")
}

// formatDiskFileSize formats a byte count for the damaged-disk summary.
func formatDiskFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
