package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// Platform-specific imports will be added based on build tags
// This is a Windows implementation focused on FILL functionality

const version = "250825_fill"

var start_time time.Time
var globalInterruptHandler *InterruptHandler

// History types and structures (copied from main project)
type HistoryEntry struct {
	Timestamp     time.Time              `json:"timestamp"`
	Command       string                 `json:"command"`
	Target        string                 `json:"target"`
	Operation     string                 `json:"operation"`
	FullCommand   string                 `json:"fullCommand"`
	Parameters    map[string]interface{} `json:"parameters"`
	Results       map[string]interface{} `json:"results"`
	ResultSummary string                 `json:"resultSummary,omitempty"`
	Duration      string                 `json:"duration"`
	Success       bool                   `json:"success"`
	ErrorMsg      string                 `json:"error,omitempty"`
}

type HistoryLogger struct {
	enabled      bool
	startTime    time.Time
	entry        HistoryEntry
	originalArgs []string
	historyFile  string
	canWriteHist bool
}

// Interrupt handler for Ctrl+C
type InterruptHandler struct {
	cancelled    bool
	forceExit    bool
	cleanupFuncs []func()
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
}

// Progress tracker
type ProgressTracker struct {
	startTime     time.Time
	lastUpdate    time.Time
	totalItems    int64
	totalBytes    int64
	currentItem   int64
	currentBytes  int64
	interval      time.Duration
	lastMBps      float64
}

// Device info types
type DeviceInfo struct {
	Path             string
	VolumeName       string
	SerialNumber     uint32
	FileSystem       string
	TotalBytes       uint64
	FreeBytes        uint64
	AvailableBytes   uint64
	FileCount        int64
	FolderCount      int64
	FullScan         bool
	DiskModel        string
	DiskSerialNumber string
	DiskInterface    string
	AccessErrors     bool
	CanRead          bool
	CanWrite         bool
}

func main() {
	start_time = time.Now()

	// Initialize history logger
	logger := NewHistoryLogger()
	defer logger.Close()

	// Parse command line arguments
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var target, size string
	var autoDelete, clean bool

	// Parse arguments
	args := os.Args[1:]
	
	// Check for clean command
	if len(args) > 0 && args[0] == "clean" {
		clean = true
		if len(args) < 2 {
			fmt.Printf("Error: clean command requires target path\n")
			printUsage()
			os.Exit(1)
		}
		target = args[1]
	} else {
		// Regular fill command
		if len(args) < 2 {
			printUsage()
			os.Exit(1)
		}
		target = args[0]
		size = args[1]
		
		// Check for 'del' flag
		if len(args) > 2 && args[2] == "del" {
			autoDelete = true
		}
	}

	// Log the command
	if clean {
		logger.LogAction("FillCommand", fmt.Sprintf("Clean: %s", target))
	} else {
		logger.LogAction("FillCommand", fmt.Sprintf("Fill: %s %s del:%v", target, size, autoDelete))
	}

	// Main execution logic based on target type
	var err error
	if isDevice(target) {
		if clean {
			err = runDeviceFillClean(target)
		} else {
			err = runDeviceFill(target, size, autoDelete)
		}
	} else if isNetworkPath(target) {
		if clean {
			err = runNetworkFillClean(target, logger)
		} else {
			err = runNetworkFill(target, size, autoDelete, logger)
		}
	} else {
		// Assume regular folder path
		if clean {
			err = runFolderFillClean(target)
		} else {
			err = runFolderFill(target, size, autoDelete)
		}
	}

	if err != nil {
		fmt.Printf("Operation failed: %v\n", err)
		logger.LogAction("FillCommand", fmt.Sprintf("ERROR: %v", err))
		os.Exit(1)
	}

	logger.LogAction("FillCommand", "Operation completed successfully")
	fmt.Printf("\nOperation completed successfully.\n")
}

func printUsage() {
	fmt.Printf("FileDO Fill - Specialized fill command\n")
	fmt.Printf("Usage:\n")
	fmt.Printf("  filedo_fill.exe <target> <size_mb> [del]  - Fill target with files\n")
	fmt.Printf("  filedo_fill.exe clean <target>           - Clean test files from target\n")
	fmt.Printf("\nExamples:\n")
	fmt.Printf("  filedo_fill.exe C: 1000       - Fill C: drive with 1000MB files\n")
	fmt.Printf("  filedo_fill.exe C: 500 del    - Fill C: drive with 500MB files and auto-delete\n")
	fmt.Printf("  filedo_fill.exe clean C:      - Clean all test files from C: drive\n")
	fmt.Printf("  filedo_fill.exe D:\\temp 100    - Fill folder with 100MB files\n")
}
	globalInterruptHandler = NewInterruptHandler()

	hi_message := "\n" + start_time.Format("2006-01-02 15:04:05") + " FileDO FILL v" + version + " sza@ukr.net\n"
	fmt.Print(hi_message)

	// Ensure bue_message is always printed, even on errors or panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\nPanic: %v\n", r)
		}
		bue_message := "\n Finish:" + time.Now().Format("2006-01-02 15:04:05") + ", Duration: " + fmt.Sprintf("%.0fs", time.Since(start_time).Seconds()) + "\n"
		fmt.Print(bue_message)
	}()

	args := os.Args
	if len(args) < 2 {
		showUsage()
		return
	}

	// Initialize history logger - use same format as main filedo
	historyLogger := NewHistoryLogger(os.Args)
	defer historyLogger.Finish()

	// Parse arguments for FILL command
	// Expected: filedo_fill.exe C: 1000 del
	// Should work like: filedo.exe C: fill 1000 del
	
	targetPath := args[1]
	
	// Default values
	sizeMBStr := "100"
	autoDelete := false
	
	// Parse additional arguments
	for i := 2; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		
		// Check if it's a size number
		if arg != "del" && arg != "delete" && arg != "d" && arg != "clean" && arg != "c" {
			sizeMBStr = arg
		}
		
		// Check for auto-delete flags
		if arg == "del" || arg == "delete" || arg == "d" {
			autoDelete = true
		}
		
		// Check for clean operation
		if arg == "clean" || arg == "c" {
			err := handleCleanOperation(targetPath, historyLogger)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}
	}

	// Determine path type and call appropriate fill function
	err := handleFillOperation(targetPath, sizeMBStr, autoDelete, historyLogger)
	if err != nil {
		historyLogger.SetError(err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	
	historyLogger.SetSuccess()
}

func showUsage() {
	usage := fmt.Sprintf(`
FileDO FILL v%s - Specialized Fill Operation Tool
Created by sza@ukr.net

USAGE:
  filedo_fill.exe <target> [size] [options]

EXAMPLES:
  filedo_fill.exe C:               → Fill C: with 100MB files (default size)
  filedo_fill.exe D: 500           → Fill D: with 500MB files
  filedo_fill.exe E: 1000 del      → Fill E: with 1000MB files and auto-delete
  filedo_fill.exe C:\temp          → Fill folder with 100MB files
  filedo_fill.exe C:\temp 200 del  → Fill folder with 200MB files and auto-delete
  filedo_fill.exe \\server\share   → Fill network share with 100MB files
  filedo_fill.exe C: clean         → Clean test files from C:
  filedo_fill.exe C:\temp clean    → Clean test files from folder

TARGETS:
  C:, D:, etc.        → Device/drive operations
  C:\folder           → Folder operations  
  \\server\share      → Network operations

SIZE:
  <number>            → File size in megabytes (1-10240)
  Default: 100MB if not specified

OPTIONS:
  del, delete, d      → Auto-delete files after creation (for testing)
  clean, c            → Clean existing test files (FILL_*.tmp, speedtest_*.txt)

NOTES:
• Creates files named FILL_00001_ddHHmmss.tmp, FILL_00002_ddHHmmss.tmp, etc.
• Fills until disk is full or error occurs
• Use Ctrl+C to cancel operation safely
• All operations are logged in history.json
• Compatible with main FileDO - uses same file formats and cleanup commands

`, version)
	fmt.Print(usage)
}

func handleFillOperation(targetPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	// Determine path type (similar to main filedo logic)
	targetPath = strings.TrimSpace(targetPath)
	
	// Check if it's a drive letter
	if len(targetPath) > 0 && ((len(targetPath) == 1) || (len(targetPath) > 1 && len(targetPath) < 4 && string([]rune(targetPath)[1]) == ":")) {
		if len(targetPath) == 1 {
			targetPath += ":"
		}
		// Device operation
		logger.SetCommand("device", targetPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
		
		return runDeviceFill(targetPath, sizeMBStr, autoDelete)
	}
	
	// Check if it's a network path
	if len(targetPath) > 2 && (targetPath[0:2] == "\\" || targetPath[0:2] == "//") {
		// Network operation
		logger.SetCommand("network", targetPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
		
		return runNetworkFill(targetPath, sizeMBStr, autoDelete, logger)
	}
	
	// Check if it's an existing folder
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		// Folder operation
		logger.SetCommand("folder", targetPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
		
		return runFolderFill(targetPath, sizeMBStr, autoDelete)
	}
	
	// Path doesn't exist or is a file
	if strings.HasSuffix(targetPath, "/") || strings.HasSuffix(targetPath, "\\") {
		return fmt.Errorf("folder \"%s\" does not exist", targetPath)
	} else {
		return fmt.Errorf("path \"%s\" does not exist or is not a valid target", targetPath)
	}
}

func handleCleanOperation(targetPath string, logger *HistoryLogger) error {
	// Determine path type and call appropriate clean function
	targetPath = strings.TrimSpace(targetPath)
	
	// Check if it's a drive letter
	if len(targetPath) > 0 && ((len(targetPath) == 1) || (len(targetPath) > 1 && len(targetPath) < 4 && string([]rune(targetPath)[1]) == ":")) {
		if len(targetPath) == 1 {
			targetPath += ":"
		}
		// Device clean operation
		logger.SetCommand("device", targetPath, "clean")
		return runDeviceFillClean(targetPath)
	}
	
	// Check if it's a network path
	if len(targetPath) > 2 && (targetPath[0:2] == "\\" || targetPath[0:2] == "//") {
		// Network clean operation
		logger.SetCommand("network", targetPath, "clean")
		return runNetworkFillClean(targetPath, logger)
	}
	
	// Check if it's an existing folder
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		// Folder clean operation
		logger.SetCommand("folder", targetPath, "clean")
		return runFolderFillClean(targetPath)
	}
	
	return fmt.Errorf("path \"%s\" does not exist or is not a valid target", targetPath)
}

// Note: The actual implementation functions (runDeviceFill, runNetworkFill, etc.)
// need to be imported or copied from the main filedo package.
// This will require restructuring the main package to make these functions exportable
// or copying the necessary code into this package.

// Utility functions (copied from main project)

// formatDuration formats a duration into a human-readable string
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	return fmt.Sprintf("%.0fh", d.Hours())
}

// parseSize parses size string like "100", "1000", "max"
func parseSize(sizeStr string) (int, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	if sizeStr == "" {
		return 100, nil // default
	}
	if sizeStr == "max" {
		return 10240, nil // 10GB
	}
	
	size, err := strconv.Atoi(sizeStr)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %s", sizeStr)
	}
	
	if size < 1 || size > 10240 {
		return 0, fmt.Errorf("size must be between 1 and 10240 MB")
	}
	
	return size, nil
}

// formatBytes formats bytes into human-readable format
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB (%d bytes)", float64(b)/float64(div), "KMGTPE"[exp], b)
}

// History logger functions
func NewHistoryLogger(args []string) *HistoryLogger {
	// Check for nohist/no_history flags to disable history logging
	enabled := true
	for _, arg := range args {
		if arg == "nohist" || arg == "no_history" {
			enabled = false
			break
		}
	}

	historyFile := "history.json"
	canWriteHist := true

	// Check if we can write to the history file
	if enabled {
		// Try to open the file for writing to check permissions
		if file, err := os.OpenFile(historyFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644); err != nil {
			canWriteHist = false
		} else {
			file.Close()
		}
	}

	// Create full command string from args
	fullCommand := strings.Join(args, " ")

	return &HistoryLogger{
		enabled:      enabled && canWriteHist,
		startTime:    time.Now(),
		originalArgs: args,
		historyFile:  historyFile,
		canWriteHist: canWriteHist,
		entry: HistoryEntry{
			Timestamp:   time.Now(),
			FullCommand: fullCommand,
			Parameters:  make(map[string]interface{}),
			Results:     make(map[string]interface{}),
		},
	}
}

func (hl *HistoryLogger) SetCommand(command, target, operation string) {
	if !hl.enabled {
		return
	}
	hl.entry.Command = command
	hl.entry.Target = target
	hl.entry.Operation = operation
}

func (hl *HistoryLogger) SetParameter(key string, value interface{}) {
	if !hl.enabled {
		return
	}
	hl.entry.Parameters[key] = value
}

func (hl *HistoryLogger) SetResult(key string, value interface{}) {
	if !hl.enabled {
		return
	}
	hl.entry.Results[key] = value
}

func (hl *HistoryLogger) SetError(err error) {
	if !hl.enabled {
		return
	}
	hl.entry.Success = false
	hl.entry.ErrorMsg = err.Error()
}

func (hl *HistoryLogger) SetSuccess() {
	if !hl.enabled {
		return
	}
	hl.entry.Success = true
}

func (hl *HistoryLogger) Finish() {
	if !hl.enabled {
		return
	}

	hl.entry.Duration = formatDuration(time.Since(hl.startTime))
	saveToHistory(hl.entry)
}

func saveToHistory(entry HistoryEntry) error {
	historyFile := "history.json"

	var history []HistoryEntry
	if data, err := os.ReadFile(historyFile); err == nil {
		json.Unmarshal(data, &history)
	}

	history = append(history, entry)

	if len(history) > 1000 {
		history = history[len(history)-1000:]
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyFile, data, 0644)
}

// Interrupt handler functions
func NewInterruptHandler() *InterruptHandler {
	ctx, cancel := context.WithCancel(context.Background())
	handler := &InterruptHandler{
		ctx:    ctx,
		cancel: cancel,
	}

	// Note: Signal handling would be implemented here for full functionality
	// For simplicity, this basic version just provides the interface
	
	return handler
}

func (h *InterruptHandler) IsCancelled() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cancelled
}

func (h *InterruptHandler) IsForceExit() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.forceExit
}

func (h *InterruptHandler) Context() context.Context {
	return h.ctx
}

func (h *InterruptHandler) AddCleanup(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cleanupFuncs = append(h.cleanupFuncs, fn)
}

// Progress tracker functions
func NewProgressTrackerWithInterval(totalItems, totalBytes int64, interval time.Duration) *ProgressTracker {
	return &ProgressTracker{
		startTime:   time.Now(),
		lastUpdate:  time.Now(),
		totalItems:  totalItems,
		totalBytes:  totalBytes,
		interval:    interval,
	}
}

func (p *ProgressTracker) Update(currentItem, currentBytes int64) {
	p.currentItem = currentItem
	p.currentBytes = currentBytes
	p.lastUpdate = time.Now()
}

func (p *ProgressTracker) ShouldUpdate() bool {
	return time.Since(p.lastUpdate) >= p.interval
}

func (p *ProgressTracker) GetCurrentSpeed() float64 {
	elapsed := time.Since(p.startTime).Seconds()
	if elapsed <= 0 {
		return 0
	}
	return float64(p.currentBytes) / (1024 * 1024) / elapsed
}

func (p *ProgressTracker) PrintProgress(operation string) {
	elapsed := time.Since(p.startTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}
	
	speedMBps := float64(p.currentBytes) / (1024 * 1024) / elapsed
	p.lastMBps = speedMBps
	
	percentComplete := float64(p.currentItem) / float64(p.totalItems) * 100
	if p.totalItems == 0 {
		percentComplete = 0
	}
	
	fmt.Printf("\r%s: %d/%d files (%.1f%%) - %.1f MB/s", 
		operation, p.currentItem, p.totalItems, percentComplete, speedMBps)
}

func (p *ProgressTracker) PrintProgressCustom(format string, args ...interface{}) {
	fmt.Printf(format, args...)
}

func (p *ProgressTracker) Finish(operation string) {
	elapsed := time.Since(p.startTime)
	avgSpeed := float64(p.currentBytes) / (1024 * 1024) / elapsed.Seconds()
	
	fmt.Printf("\n\n%s Complete!\n", operation)
	fmt.Printf("Files processed: %d\n", p.currentItem)
	fmt.Printf("Total data: %.2f MB\n", float64(p.currentBytes)/(1024*1024))
	fmt.Printf("Time elapsed: %s\n", formatDuration(elapsed))
	fmt.Printf("Average speed: %.2f MB/s\n", avgSpeed)
}

// File copy optimization
var optimalBuffers = make(map[string]int)

func copyFileOptimized(src, dst string) (int64, error) {
	// Use default buffer for simplicity
	srcFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer dstFile.Close()

	buf := make([]byte, 64*1024*1024) // 64MB buffer
	return io.CopyBuffer(dstFile, srcFile, buf)
}

// Error checking for critical errors
func isCriticalError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	criticalKeywords := []string{
		"no space left",
		"disk full",
		"insufficient disk space",
		"not enough space",
		"device not ready",
		"i/o error",
		"hardware error",
		"disk error",
	}
	
	for _, keyword := range criticalKeywords {
		if strings.Contains(errStr, keyword) {
			return true
		}
	}
	return false
}

// writeTestFileWithBuffer writes a test file with the given buffer size  
func writeTestFileWithBuffer(filePath string, fileSize int64, bufferSize int) error {
	return writeTestFileWithBufferContext(context.Background(), filePath, fileSize, bufferSize)
}

func writeTestFileWithBufferContext(ctx context.Context, filePath string, fileSize int64, bufferSize int) error {
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	// Generate unique header with filename and timestamp
	fileName := filepath.Base(filePath)
	timestamp := time.Now().Format("20060102_150405")
	headerLine := fmt.Sprintf("FILEDO_TEST_%s_%s\n", fileName, timestamp)

	// Write header
	written, err := file.WriteString(headerLine)
	if err != nil {
		return err
	}

	// Calculate remaining space for data and footer
	remaining := fileSize - int64(written) - int64(len(headerLine))

	// Fill with readable pattern
	pattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(pattern)
	block := make([]byte, bufferSize)

	// Fill buffer with pattern
	for i := 0; i < bufferSize; {
		copyLen := len(patternBytes)
		if i+copyLen > bufferSize {
			copyLen = bufferSize - i
		}
		copy(block[i:i+copyLen], patternBytes[:copyLen])
		i += copyLen
	}

	// Write data blocks
	for remaining > int64(len(headerLine)) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		writeSize := bufferSize
		if remaining-int64(len(headerLine)) < int64(bufferSize) {
			writeSize = int(remaining - int64(len(headerLine)))
		}

		n, err := file.Write(block[:writeSize])
		if err != nil {
			return err
		}
		remaining -= int64(n)
	}

	// Write footer (same as header)
	_, err = file.WriteString(headerLine)
	if err != nil {
		return err
	}

	return file.Sync()
}

// Buffer calibration for optimal performance
func calibrateOptimalBufferSize(testPath string) int {
	// Simple implementation - just return a reasonable default
	return 64 * 1024 * 1024 // 64MB
}

// Helper functions for path detection
func isDevice(path string) bool {
	// Check if it's a device path like "C:", "D:", etc.
	if len(path) == 2 && path[1] == ':' {
		drive := path[0]
		return (drive >= 'A' && drive <= 'Z') || (drive >= 'a' && drive <= 'z')
	}
	return false
}

func isNetworkPath(path string) bool {
	// Check for UNC paths (\\server\share) or network mappings
	return strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//")
}
