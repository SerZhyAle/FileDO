package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// FakeCapacityTester is what the fake-capacity engine needs from a target.
// Device, folder and network testers differ only in how they reach the target;
// every one of them writes its files with the same writer and is read back by
// the same verifier (SP-0026 CAP-01).
type FakeCapacityTester interface {
	// GetTestInfo returns the test type name and target path for display
	GetTestInfo() (testType, targetPath string)

	// GetAvailableSpace proves the target takes a write and returns the bytes
	// free for testing. A failed probe or query is an error, never a guess.
	GetAvailableSpace() (int64, error)

	// CreateTestFileContext writes the named test file (the name may carry a
	// subdirectory) and returns its path. On error the path is returned too
	// when a file - whole or partial - was left on disk by this call, and is
	// empty otherwise: a caller never removes a file the tester did not make.
	CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error)

	// CleanupTestFile removes a test file
	CleanupTestFile(filePath string) error

	// GetCleanupCommand returns the command that removes the test files: the
	// clean verb on the folder that holds them (CAP-05).
	GetCleanupCommand() string
}

// FakeCapacityTestResult holds the results of a fake capacity test
type FakeCapacityTestResult struct {
	TestPassed        bool
	FilesCreated      int
	TotalDataBytes    int64
	BaselineSpeedMBps float64
	AverageSpeedMBps  float64
	MinSpeedMBps      float64
	MaxSpeedMBps      float64
	FailureReason     string
	CreatedFiles      []string
}

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

func (di DeviceInfo) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Information for device: %s\n", di.Path))

	// Access status
	var accessStatus []string
	if di.CanRead {
		accessStatus = append(accessStatus, "Readable")
	}
	if di.CanWrite {
		accessStatus = append(accessStatus, "Writable")
	}
	if len(accessStatus) == 0 {
		accessStatus = append(accessStatus, "Not accessible")
	}
	b.WriteString(fmt.Sprintf("  Access:        %s\n", strings.Join(accessStatus, ", ")))

	b.WriteString(fmt.Sprintf("  Volume Name:   %s\n", di.VolumeName))
	b.WriteString(fmt.Sprintf("  Serial Number: %d\n", di.SerialNumber))
	b.WriteString(fmt.Sprintf("  File System:   %s\n", di.FileSystem))
	if di.FullScan && (di.DiskModel != "" || di.DiskSerialNumber != "" || di.DiskInterface != "") {
		b.WriteString("  --- Physical Disk Info ---\n")
		if di.DiskModel != "" {
			b.WriteString(fmt.Sprintf("  Model:         %s\n", di.DiskModel))
		}
		if di.DiskSerialNumber != "" {
			b.WriteString(fmt.Sprintf("  Serial Number: %s\n", di.DiskSerialNumber))
		}
		if di.DiskInterface != "" {
			b.WriteString(fmt.Sprintf("  Interface:     %s\n", di.DiskInterface))
		}
		b.WriteString("  --------------------------\n")
	}
	b.WriteString(fmt.Sprintf("  Total Size:    %s\n", formatBytes(di.TotalBytes)))
	b.WriteString(fmt.Sprintf("  Free Space:    %s\n", formatBytes(di.FreeBytes)))
	containsLabel := "Contains:"
	if di.FullScan {
		containsLabel = "Full Contains:"
	}
	b.WriteString(fmt.Sprintf("  %-14s %d files, %d folders\n", containsLabel, di.FileCount, di.FolderCount))
	b.WriteString(fmt.Sprintf("  Usage:         %.1f%%\n", di.usage()))
	if di.AccessErrors {
		b.WriteString("\nWarning: Some information could not be gathered due to access restrictions.\n")
		b.WriteString("         Run as administrator for a complete scan.\n")
	}
	return b.String()
}

func (di DeviceInfo) StringShort() string {
	var b strings.Builder

	// Format volume name and file system
	b.WriteString(fmt.Sprintf("Volume:   %s (%s)\n", di.VolumeName, di.FileSystem))

	// Format total size without full bytes, free space, and usage percentage
	totalFormatted := formatBytesShort(di.TotalBytes)
	freeFormatted := formatBytesShort(di.FreeBytes)
	usage := di.usage()

	b.WriteString(fmt.Sprintf("Total:  %s, Free:  %s (Usage: %.1f%%)", totalFormatted, freeFormatted, usage))

	return b.String()
}

// usagePercent is the used share of a volume. Under a per-user disk quota the
// total is the quota while the free space is the whole volume's, so free can
// exceed total; the unsigned subtraction then wrapped to nearly 2^64 and the
// usage printed as billions of percent (SP-0026 CAP-20). Free is clamped to
// total, and an unknown total is 0% rather than a division by zero.
func usagePercent(total, free uint64) float64 {
	if total == 0 {
		return 0
	}
	if free > total {
		free = total
	}
	return float64(total-free) * 100 / float64(total)
}

// usage prefers the bytes free to this user when the volume-wide free space
// does not fit inside a quota-limited total.
func (di DeviceInfo) usage() float64 {
	free := di.FreeBytes
	if free > di.TotalBytes && di.AvailableBytes <= di.TotalBytes {
		free = di.AvailableBytes
	}
	return usagePercent(di.TotalBytes, free)
}

type FolderInfo struct {
	Path         string
	Size         uint64
	FileCount    int64
	FolderCount  int64
	ModTime      time.Time
	CreationTime time.Time
	Mode         fs.FileMode
	FullScan     bool
	AccessErrors bool
	CanRead      bool
	CanWrite     bool
}

func (fi FolderInfo) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Information for folder: %s\n", fi.Path))

	// Access status
	var accessStatus []string
	if fi.CanRead {
		accessStatus = append(accessStatus, "Readable")
	}
	if fi.CanWrite {
		accessStatus = append(accessStatus, "Writable")
	}
	if len(accessStatus) == 0 {
		accessStatus = append(accessStatus, "Not accessible")
	}
	b.WriteString(fmt.Sprintf("  Access:     %s\n", strings.Join(accessStatus, ", ")))

	b.WriteString(fmt.Sprintf("  Mode:       %s\n", formatMode(fi.Mode)))
	if !fi.CreationTime.IsZero() {
		b.WriteString(fmt.Sprintf("  Created:    %s\n", fi.CreationTime.Format("2006-01-02 15:04:05")))
	}
	b.WriteString(fmt.Sprintf("  Modified:   %s\n", fi.ModTime.Format("2006-01-02 15:04:05")))
	sizeLabel := "Root Size:"
	if fi.FullScan {
		sizeLabel = "Total Size:"
	}
	b.WriteString(fmt.Sprintf("  %-14s %s\n", sizeLabel, formatBytes(fi.Size)))
	containsLabel := "Root Contains:"
	if fi.FullScan {
		containsLabel = "Full Contains:"
	}
	b.WriteString(fmt.Sprintf("  %-14s %d files, %d folders\n", containsLabel, fi.FileCount, fi.FolderCount))
	if fi.AccessErrors {
		b.WriteString("\nWarning: Some information could not be gathered due to access restrictions.\n")
		b.WriteString("         Run as administrator for a complete scan.\n")
	}
	return b.String()
}

func (fi FolderInfo) StringShort() string {
	var b strings.Builder

	// Access status
	var accessStatus []string
	if fi.CanRead {
		accessStatus = append(accessStatus, "Readable")
	}
	if fi.CanWrite {
		accessStatus = append(accessStatus, "Writable")
	}
	if len(accessStatus) == 0 {
		accessStatus = append(accessStatus, "Not accessible")
	}

	// Creation time
	createdStr := ""
	if !fi.CreationTime.IsZero() {
		createdStr = ", Created: " + fi.CreationTime.Format("2006-01-02 15:04:05")
	}

	b.WriteString(fmt.Sprintf("%s%s\n", strings.Join(accessStatus, ", "), createdStr))

	// Size and contains information
	sizeFormatted := formatBytesShort(fi.Size)
	// Always show "Full Contains" for short format
	containsLabel := "Full Contains:"

	b.WriteString(fmt.Sprintf("Total Size: %s  %s %d files, %d folders", sizeFormatted, containsLabel, fi.FileCount, fi.FolderCount))

	return b.String()
}

type FileInfo struct {
	Path         string
	Size         uint64
	ModTime      time.Time
	CreationTime time.Time
	Mode         fs.FileMode
	Extension    string
	MimeType     string
	IsExecutable bool
	IsHidden     bool
	IsReadOnly   bool
	IsSystem     bool
	IsArchive    bool
	IsTemporary  bool
	IsCompressed bool
	IsEncrypted  bool
}

func (fi FileInfo) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Information for file: %s\n", fi.Path))
	b.WriteString(fmt.Sprintf("  Size:       %s\n", formatBytes(fi.Size)))
	b.WriteString(fmt.Sprintf("  Mode:       %s\n", formatMode(fi.Mode)))
	if fi.Extension != "" {
		b.WriteString(fmt.Sprintf("  Extension:  %s\n", fi.Extension))
	}
	if fi.MimeType != "" {
		b.WriteString(fmt.Sprintf("  MIME Type:  %s\n", fi.MimeType))
	}

	// File attributes
	var attributes []string
	if fi.IsExecutable {
		attributes = append(attributes, "Executable")
	}
	if fi.IsHidden {
		attributes = append(attributes, "Hidden")
	}
	if fi.IsReadOnly {
		attributes = append(attributes, "Read-Only")
	}
	if fi.IsSystem {
		attributes = append(attributes, "System")
	}
	if fi.IsArchive {
		attributes = append(attributes, "Archive")
	}
	if fi.IsTemporary {
		attributes = append(attributes, "Temporary")
	}
	if fi.IsCompressed {
		attributes = append(attributes, "Compressed")
	}
	if fi.IsEncrypted {
		attributes = append(attributes, "Encrypted")
	}

	if len(attributes) > 0 {
		b.WriteString(fmt.Sprintf("  Attributes: %s\n", strings.Join(attributes, ", ")))
	}

	if !fi.CreationTime.IsZero() {
		b.WriteString(fmt.Sprintf("  Created:    %s\n", fi.CreationTime.Format("2006-01-02 15:04:05")))
	}
	b.WriteString(fmt.Sprintf("  Modified:   %s\n", fi.ModTime.Format("2006-01-02 15:04:05")))
	return b.String()
}

func (fi FileInfo) StringShort() string {
	var b strings.Builder

	// File attributes (only show if present)
	var attributes []string
	if fi.IsExecutable {
		attributes = append(attributes, "Executable")
	}
	if fi.IsHidden {
		attributes = append(attributes, "Hidden")
	}
	if fi.IsReadOnly {
		attributes = append(attributes, "Read-Only")
	}
	if fi.IsSystem {
		attributes = append(attributes, "System")
	}
	if fi.IsArchive {
		attributes = append(attributes, "Archive")
	}
	if fi.IsTemporary {
		attributes = append(attributes, "Temporary")
	}
	if fi.IsCompressed {
		attributes = append(attributes, "Compressed")
	}
	if fi.IsEncrypted {
		attributes = append(attributes, "Encrypted")
	}

	// Attributes and size on first line
	attributesStr := ""
	if len(attributes) > 0 {
		attributesStr = strings.Join(attributes, ", ") + ", "
	}

	sizeFormatted := formatBytesShort(fi.Size)
	b.WriteString(fmt.Sprintf("%sSize: %s\n", attributesStr, sizeFormatted))

	// Creation and modification times on second line
	createdStr := ""
	if !fi.CreationTime.IsZero() {
		createdStr = "Created: " + fi.CreationTime.Format("2006-01-02 15:04:05")
	}

	modifiedStr := "Modified: " + fi.ModTime.Format("2006-01-02 15:04:05")

	if createdStr != "" {
		b.WriteString(fmt.Sprintf("%s, %s", createdStr, modifiedStr))
	} else {
		b.WriteString(modifiedStr)
	}

	return b.String()
}

type NetworkInfo struct {
	Path         string
	CanRead      bool
	CanWrite     bool
	Size         uint64
	FileCount    int64
	FolderCount  int64
	FullScan     bool
	AccessErrors bool
}

func (ni NetworkInfo) String() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Information for network path: %s\n", ni.Path))

	// Access status
	var accessStatus []string
	if ni.CanRead {
		accessStatus = append(accessStatus, "Readable")
	}
	if ni.CanWrite {
		accessStatus = append(accessStatus, "Writable")
	}
	if len(accessStatus) == 0 {
		accessStatus = append(accessStatus, "Not accessible")
	}
	b.WriteString(fmt.Sprintf("  Access:     %s\n", strings.Join(accessStatus, ", ")))

	if ni.CanRead {
		sizeLabel := "Root Size:"
		if ni.FullScan {
			sizeLabel = "Total Size:"
		}
		b.WriteString(fmt.Sprintf("  %-12s %s\n", sizeLabel, formatBytes(ni.Size)))

		containsLabel := "Root Contains:"
		if ni.FullScan {
			containsLabel = "Full Contains:"
		}
		b.WriteString(fmt.Sprintf("  %-14s %d files, %d folders\n", containsLabel, ni.FileCount, ni.FolderCount))

		if ni.AccessErrors {
			b.WriteString("\nWarning: Some network locations could not be accessed.\n")
			b.WriteString("         This may be due to permissions or network connectivity issues.\n")
		}
	}

	return b.String()
}

func formatMode(m fs.FileMode) string {
	var desc []string
	var permissions []string

	// File type
	if m.IsDir() {
		desc = append(desc, "directory")
	} else if m&fs.ModeSymlink != 0 {
		desc = append(desc, "symbolic link")
	} else if m&fs.ModeDevice != 0 {
		desc = append(desc, "device")
	} else if m&fs.ModeNamedPipe != 0 {
		desc = append(desc, "named pipe")
	} else if m&fs.ModeSocket != 0 {
		desc = append(desc, "socket")
	} else if m&fs.ModeCharDevice != 0 {
		desc = append(desc, "character device")
	} else {
		desc = append(desc, "regular file")
	}

	// Group permissions - simplified format
	var groupPerms []string
	if m&0040 != 0 {
		groupPerms = append(groupPerms, "read")
	}
	if m&0020 != 0 {
		groupPerms = append(groupPerms, "write")
	}
	if m&0010 != 0 {
		groupPerms = append(groupPerms, "execute")
	}

	if len(groupPerms) > 0 {
		permissions = append(permissions, "group can "+strings.Join(groupPerms, ", "))
	}

	// Other permissions - simplified format
	var otherPerms []string
	if m&0004 != 0 {
		otherPerms = append(otherPerms, "read")
	}
	if m&0002 != 0 {
		otherPerms = append(otherPerms, "write")
	}
	if m&0001 != 0 {
		otherPerms = append(otherPerms, "execute")
	}

	if len(otherPerms) > 0 {
		permissions = append(permissions, "others can "+strings.Join(otherPerms, ", "))
	}

	// Special permissions
	if m&fs.ModeSetuid != 0 {
		permissions = append(permissions, "setuid")
	}
	if m&fs.ModeSetgid != 0 {
		permissions = append(permissions, "setgid")
	}
	if m&fs.ModeSticky != 0 {
		permissions = append(permissions, "sticky bit")
	}

	// Combine descriptions
	result := fmt.Sprintf("%s (%s", m.String(), strings.Join(desc, ", "))
	if len(permissions) > 0 {
		result += "; " + strings.Join(permissions, ", ")
	}
	result += ")"

	return result
}

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

// formatBytesShort formats bytes without showing the full byte count in parentheses
func formatBytesShort(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ---------------------------------------------------------------------------
// The fake-capacity test (SP-0026)
//
// runGenericFakeCapacityTest drives any FakeCapacityTester through one test.
// Three independent signals decide it, and all three stay: header == footer;
// a body that names its file, its run and its offset in every 4 KiB block
// (capacity_format.go); and the write speed. What each of them may conclude:
//
//   - every read-back bypasses the Windows cache (openForVerify), and before a
//     PASS a final pass re-reads every file - header, footer and sampled
//     blocks - so a file that went bad after its own check is still caught
//     (CAP-03);
//   - a speed anomaly alone is never a verdict: it triggers that re-read of
//     every file written so far, and only data that does not read back is a
//     defect (CAP-07);
//   - an error is judged by the shared classifier (errclass_windows.go): a
//     stop is a stop, an environmental error - access denied, write
//     protection, a device or share that went away, a full disk - proves
//     nothing (exit 2), and only a device-level I/O error or data that does
//     not read back is a defect (exit 1) (CAP-04, CAP-13).
//
// On a defect the test files are kept, never cleaned up: they are the evidence
// for the estimated-real-capacity report, and the result event names them. A
// stop removes them, whenever it comes.

const (
	baselineFileCount = 3
	finalPassSamples  = 3
	speedLowRatio     = 0.1
	speedHighRatio    = 10.0
)

// capacityRun is one test in progress.
type capacityRun struct {
	tester     FakeCapacityTester
	targetPath string
	plan       capacityPlan
	result     *FakeCapacityTestResult
	logger     *HistoryLogger
	ctx        context.Context
	evidence   []string // partial files kept beside the complete ones
	anomalies  int
}

// runGenericFakeCapacityTest performs a fake capacity test through tester.
func runGenericFakeCapacityTest(tester FakeCapacityTester, autoDelete bool, maxFiles int, logger *HistoryLogger) (*FakeCapacityTestResult, error) {
	testType, targetPath := tester.GetTestInfo()
	if logger != nil {
		logger.SetCommand(strings.ToLower(testType), targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
	}
	result := &FakeCapacityTestResult{CreatedFiles: make([]string, 0, 100)}
	r := &capacityRun{tester: tester, targetPath: targetPath, result: result, logger: logger, ctx: capacityContext()}

	freeSpace, err := tester.GetAvailableSpace()
	if err != nil {
		return result, r.logErr(err)
	}
	plan, err := planCapacityTest(freeSpace, maxFiles, capacityVolumeFacts(targetPath))
	if err != nil {
		return result, r.logErr(err)
	}
	r.plan = plan

	fmt.Printf("%s Fake Capacity Test\n", testType)
	fmt.Printf("Target: %s\n", getEnhancedTargetInfo(tester))
	fmt.Printf("Available space: %.2f GB\n", gbOf(freeSpace))
	for _, note := range plan.Notes {
		fmt.Printf("Note: %s\n", note)
	}
	fmt.Printf("Test file size: %d MB (%d files, %.2f GB - %.1f%% of available space)\n",
		plan.FileSize/capMiB, plan.Files, gbOf(plan.Target()), float64(plan.Target())/float64(freeSpace)*100)
	fmt.Printf("Every file is read back past the Windows cache, and all of them again before a PASS.\n\n")

	if plan.SubDir != "" {
		dir := filepath.Join(targetPath, plan.SubDir)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return result, r.logErr(fmt.Errorf("could not create %s: %w", dir, err))
		}
	}
	nonce := newCapacityNonce()
	runID := capacityRunID(nonce)
	stamp := time.Now().Format(capacityStampLayout)
	width := len(strconv.Itoa(plan.Files))
	if width < 3 {
		width = 3
	}
	cachedBefore := verifyCachedReads.Load()

	var speeds []float64
	var baseline float64
	progress := NewProgressTrackerWithInterval(int64(plan.Files), plan.Target(), 2*time.Second)

	runStep("write", fmt.Sprintf("Writing %d test files", plan.Files))
	fmt.Printf("Starting capacity test - writing %d files...\n", plan.Files)

	for i := 1; i <= plan.Files; i++ {
		if r.stopRequested() {
			return r.stopped("")
		}
		var path string
		var start time.Time
		for attempt := 0; ; attempt++ {
			name := capacityFileName(int64(i), width, stamp, runID)
			if plan.SubDir != "" {
				name = filepath.Join(plan.SubDir, name)
			}
			start = time.Now()
			path, err = tester.CreateTestFileContext(r.ctx, name, plan.FileSize)
			if err == nil || !errors.Is(err, fs.ErrExist) || attempt >= 2 {
				break
			}
			// CAP-10: a name is never reused. Something already has this one,
			// so the run takes a fresh nonce and with it a fresh run id.
			nonce = newCapacityNonce()
			runID = capacityRunID(nonce)
		}
		duration := time.Since(start)
		if err != nil {
			return r.createFailed(i, path, err)
		}
		result.FilesCreated++
		result.TotalDataBytes += plan.FileSize
		result.CreatedFiles = append(result.CreatedFiles, path)

		if err := verifySmartTestFiles(result.CreatedFiles, i); err != nil {
			return r.verifyFailed(err, i-1, fmt.Sprintf("Verification failed after creating file %d", i))
		}

		if duration <= 0 {
			duration = time.Nanosecond
		}
		speed := float64(plan.FileSize) / duration.Seconds() / float64(capMiB)
		speeds = append(speeds, speed)
		progress.Update(int64(result.FilesCreated), result.TotalDataBytes)
		progress.PrintProgress("Test")

		if i <= baselineFileCount {
			if i == baselineFileCount {
				sum := 0.0
				for _, s := range speeds[:baselineFileCount] {
					sum += s
				}
				baseline = sum / baselineFileCount
				result.BaselineSpeedMBps = baseline
				fmt.Printf("Baseline speed established: %.2f MB/s", baseline)
			}
		} else if baseline > 0 && (speed < baseline*speedLowRatio || speed > baseline*speedHighRatio) {
			if err := r.speedAnomaly(i, speed, baseline); err != nil {
				return result, err
			}
			// The anomaly was the device's cache or throttling: measure the
			// next files against what the device does now, so one sustained
			// change is reported once, not once per file.
			baseline = speed
		}
	}

	// The final pass: every file, past the cache, after the last write. A
	// counterfeit that drops or aliases later writes damages files the
	// in-loop checks had already passed (CAP-03).
	fmt.Printf("\nFinal pass: re-reading all %d files past the cache..\n", len(result.CreatedFiles))
	runStep("verify", fmt.Sprintf("Re-reading %d test files", len(result.CreatedFiles)))
	if good, bad := verifyTestFilesSampled(result.CreatedFiles, finalPassSamples); bad != nil {
		_, err := r.verifyFailed(bad, good, "Final verification failed")
		return result, err
	}

	fmt.Printf("\n✅ Write, incremental verification and the final re-read completed successfully!\n")

	if len(speeds) > 0 {
		result.MinSpeedMBps = speeds[0]
		result.MaxSpeedMBps = speeds[0]
		sum := 0.0
		for _, speed := range speeds {
			if speed < result.MinSpeedMBps {
				result.MinSpeedMBps = speed
			}
			if speed > result.MaxSpeedMBps {
				result.MaxSpeedMBps = speed
			}
			sum += speed
		}
		result.AverageSpeedMBps = sum / float64(len(speeds))
	}

	result.TestPassed = true

	runNumber("filesWritten", result.FilesCreated)
	runNumber("bytesWritten", result.TotalDataBytes)
	runNumber("averageSpeedMBps", result.AverageSpeedMBps)
	runNumber("baselineSpeedMBps", result.BaselineSpeedMBps)
	if r.anomalies > 0 {
		runNumber("speedAnomalies", r.anomalies)
	}

	fmt.Printf("\n✅ TEST PASSED SUCCESSFULLY!\n")
	fmt.Printf("All %d files were written and read back intact.\n", result.FilesCreated)
	if cached := verifyCachedReads.Load() - cachedBefore; cached > 0 {
		note := fmt.Sprintf("%d reads could not bypass the cache on this target (it refuses unbuffered I/O); they may have been answered from memory", cached)
		fmt.Printf("Note: %s.\n", note)
		EmitNoteEvent(note)
		runNumber("cachedReads", cached)
	}
	if r.anomalies > 0 {
		fmt.Printf("Speed changed %d time(s) during the test; every time, all files re-read intact.\n", r.anomalies)
	}
	fmt.Printf("\n📊 Speed Statistics:\n")
	fmt.Printf("  Baseline speed (first 3 files): %.2f MB/s\n", result.BaselineSpeedMBps)
	fmt.Printf("  Average speed: %.2f MB/s\n", result.AverageSpeedMBps)
	fmt.Printf("  Minimum speed: %.2f MB/s\n", result.MinSpeedMBps)
	fmt.Printf("  Maximum speed: %.2f MB/s\n", result.MaxSpeedMBps)
	fmt.Printf("  Total data written: %.2f MB\n", float64(result.TotalDataBytes)/float64(capMiB))

	// kept is every test file the run leaves on disk: all of them without
	// del, the ones that could not be deleted with it. They are named in the
	// result event, as on the defect and could-not-verify endings (AUD-24-F1).
	var kept []string
	if autoDelete {
		fmt.Printf("\n🗑️  Auto-delete enabled, cleaning up test files...\n")
		deletedCount := 0
		for _, filePath := range result.CreatedFiles {
			if err := tester.CleanupTestFile(filePath); err != nil {
				fmt.Printf("Warning: Failed to delete file: %v\n", err)
				kept = append(kept, filePath)
			} else {
				deletedCount++
			}
		}
		r.removeSubDir()
		fmt.Printf("Successfully deleted %d/%d test files.\n", deletedCount, len(result.CreatedFiles))
	} else {
		fmt.Printf("\n📁 Test files kept for manual inspection:\n")
		fmt.Printf("   Location: %s\n", filepath.Join(targetPath, plan.SubDir))
		fmt.Printf("   Files: %s .. %s\n", filepath.Base(result.CreatedFiles[0]), filepath.Base(result.CreatedFiles[len(result.CreatedFiles)-1]))
		fmt.Printf("   Use '%s' to remove them later.\n", tester.GetCleanupCommand())
		kept = append(kept, result.CreatedFiles...)
	}
	recordFilesLeft(kept)

	if logger != nil {
		logger.SetResult("testPassed", result.TestPassed)
		logger.SetResult("averageSpeedMBps", result.AverageSpeedMBps)
		logger.SetResult("minSpeedMBps", result.MinSpeedMBps)
		logger.SetResult("maxSpeedMBps", result.MaxSpeedMBps)
		logger.SetResult("baselineSpeedMBps", result.BaselineSpeedMBps)
		logger.SetResult("totalDataMB", float64(result.TotalDataBytes)/float64(capMiB))
		logger.SetResult("filesDeleted", autoDelete && len(kept) == 0)
		logger.SetSuccess()
	}

	return result, nil
}

func (r *capacityRun) logErr(err error) error {
	if r.logger != nil {
		r.logger.SetError(err)
	}
	return err
}

func (r *capacityRun) stopRequested() bool {
	return r.ctx.Err() != nil || runStopRequested()
}

// isEvidence reports whether a verification error is evidence against the
// media: data that did not read back, or the device failing the I/O itself.
func isEvidence(err error) bool {
	return errors.Is(err, errDataMismatch) || isDeviceIOError(err)
}

// filesLeft is every test file on disk: the complete ones and any partial one
// kept as evidence.
func (r *capacityRun) filesLeft() []string {
	left := append([]string(nil), r.result.CreatedFiles...)
	return append(left, r.evidence...)
}

func (r *capacityRun) removeSubDir() {
	if r.plan.SubDir != "" {
		os.Remove(filepath.Join(r.targetPath, r.plan.SubDir)) // only when empty
	}
}

// stopped is the one stop behaviour, whenever the stop comes (CAP-13): the
// partial file and every complete one are removed, nothing is judged, and no
// capacity is estimated. The verdict is Stopped (outcome.go).
func (r *capacityRun) stopped(partial string) (*FakeCapacityTestResult, error) {
	fmt.Printf("\n\n⚠ Operation stopped. Removing the test files..\n")
	if partial != "" {
		r.tester.CleanupTestFile(partial)
	}
	for _, p := range r.evidence {
		r.tester.CleanupTestFile(p)
	}
	deleted := 0
	for _, p := range r.result.CreatedFiles {
		if err := r.tester.CleanupTestFile(p); err == nil {
			deleted++
		}
	}
	r.removeSubDir()
	fmt.Printf("Removed %d/%d files.\n", deleted, len(r.result.CreatedFiles))
	err := fmt.Errorf("test stopped after %d of %d files: %w", len(r.result.CreatedFiles), r.plan.Files, errRunStopped)
	if r.logger != nil {
		r.logger.SetError(err)
		r.logger.SetResult("filesCreated", r.result.FilesCreated)
		r.logger.SetResult("interrupted", true)
	}
	return r.result, err
}

// createFailed judges an error from writing file i (CAP-04). partial is the
// file the failed write left behind, if any.
func (r *capacityRun) createFailed(i int, partial string, err error) (*FakeCapacityTestResult, error) {
	if isStopError(err) || r.stopRequested() {
		return r.stopped(partial)
	}
	written := len(r.result.CreatedFiles)
	if isDeviceIOError(err) {
		if partial != "" {
			r.evidence = append(r.evidence, partial)
		}
		verified := written
		if good, bad := verifyTestFilesSampled(r.result.CreatedFiles, finalPassSamples); bad != nil {
			verified = good
		}
		return r.defect(fmt.Sprintf("The device failed a write at file %d of %d", i, r.plan.Files),
			"The device itself reported an I/O error - a failing or counterfeit controller.", verified, err)
	}
	if partial != "" {
		r.tester.CleanupTestFile(partial)
	}
	if isDiskFullError(err) {
		// A controller that lies about its size cannot cause this - the file
		// system believes the size it claims - so the files already written
		// are what decides, and they are read back first.
		if good, bad := verifyTestFilesSampled(r.result.CreatedFiles, finalPassSamples); bad != nil {
			return r.verifyFailed(bad, good, "Verification failed after the disk filled up")
		}
		return r.unverified(fmt.Sprintf("the file system reported the disk full at file %d, after %.2f GB", i, gbOf(r.result.TotalDataBytes)),
			fmt.Sprintf("A counterfeit controller cannot cause this - the file system believes the size it claims - so it proves nothing about the media. All %d files written so far read back intact; something else used the space.", written),
			err)
	}
	return r.unverified(fmt.Sprintf("could not create test file %d", i),
		"The target refused the write for a reason that says nothing about the media.", err)
}

// verifyFailed judges a failed read-back. verified is how many files are known
// to hold their data, for the estimate.
func (r *capacityRun) verifyFailed(err error, verified int, headline string) (*FakeCapacityTestResult, error) {
	if isStopError(err) || r.stopRequested() {
		return r.stopped("")
	}
	if isEvidence(err) {
		return r.defect(headline, "The data read back is not the data written: delayed corruption or fake capacity.", verified, err)
	}
	return r.unverified(headline, "A test file could not be read back, which says nothing about the media.", err)
}

// speedAnomaly re-reads every file written so far when the speed leaves
// 0.1x..10x of the baseline (CAP-07). Only a failed read-back is a verdict.
func (r *capacityRun) speedAnomaly(i int, speed, baseline float64) error {
	word := "dropped to"
	if speed > baseline {
		word = "jumped to"
	}
	fmt.Printf("\n⚠ Speed %s %.2f MB/s at file %d (baseline %.2f MB/s) - re-reading all %d files past the cache..\n",
		word, speed, i, baseline, len(r.result.CreatedFiles))
	if good, bad := verifyTestFilesSampled(r.result.CreatedFiles, finalPassSamples); bad != nil {
		_, err := r.verifyFailed(bad, good, fmt.Sprintf("Verification failed after the speed %s %.2f MB/s at file %d", word, speed, i))
		return err
	}
	r.anomalies++
	msg := fmt.Sprintf("speed %s %.2f MB/s at file %d (baseline %.2f MB/s), and all %d files read back intact: the device's cache or throttling, not lost data",
		word, speed, i, baseline, len(r.result.CreatedFiles))
	fmt.Printf("✓ The %s. Continuing.\n", msg)
	EmitNoteEvent("The " + msg + ".")
	return nil
}

// defect is the judgement that the device lies or fails: the files are kept
// and named, the capacity that read back intact is the estimate, and the error
// is a defect (exit 1).
func (r *capacityRun) defect(headline, meaning string, verified int, cause error) (*FakeCapacityTestResult, error) {
	res := r.result
	res.TestPassed = false
	res.FailureReason = fmt.Sprintf("%s: %v", headline, cause)
	realCapacity := int64(verified) * r.plan.FileSize

	fmt.Printf("\n❌ TEST FAILED: %s\n", headline)
	fmt.Printf("%s\n", meaning)
	fmt.Printf("Details: %v\n", cause)
	fmt.Printf("\n📊 ESTIMATED REAL CAPACITY ANALYSIS:\n")
	fmt.Printf("  Files that read back intact: %d of %d written\n", verified, len(res.CreatedFiles))
	fmt.Printf("  Data verified: %.2f GB\n", gbOf(realCapacity))
	fmt.Printf("  ESTIMATED REAL FREE SPACE: %.2f GB\n", gbOf(realCapacity))
	left := r.filesLeft()
	fmt.Printf("\n⚠️  Test files preserved for analysis (%d files).\n", len(left))
	fmt.Printf("   Remove them with: %s\n", r.tester.GetCleanupCommand())

	recordFilesLeft(left)
	runNumber("estimatedRealCapacityGB", gbOf(realCapacity))
	err := defectf("%s: %v", headline, cause)
	if r.logger != nil {
		r.logger.SetError(err)
		r.logger.SetResult("estimatedRealCapacityGB", gbOf(realCapacity))
		r.logger.SetResult("filesVerified", verified)
	}
	return res, err
}

// unverified ends a test that could not judge: nothing is claimed about the
// media, the files written so far stay (named, with the command that removes
// them), and the error is "could not verify" (exit 2).
func (r *capacityRun) unverified(headline, meaning string, cause error) (*FakeCapacityTestResult, error) {
	res := r.result
	res.TestPassed = false
	res.FailureReason = fmt.Sprintf("%s: %v", headline, cause)

	fmt.Printf("\n⚠ COULD NOT VERIFY: %s\n", headline)
	fmt.Printf("%s\n", meaning)
	fmt.Printf("Details: %v\n", cause)
	if left := r.filesLeft(); len(left) > 0 {
		fmt.Printf("\nThe %d test files written so far were left in place.\n", len(left))
		fmt.Printf("   Remove them with: %s\n", r.tester.GetCleanupCommand())
		recordFilesLeft(left)
	}
	err := fmt.Errorf("%s: %w", headline, cause)
	if r.logger != nil {
		r.logger.SetError(err)
	}
	return res, err
}

// ---------------------------------------------------------------------------
// Read-back

const fixedBufferSize = 64 * 1024 * 1024 // 64 MB - good for all device types

// copyFileOptimized copies src→dst with a fixed 64 MB buffer (the speed
// test's fallback when unbuffered I/O cannot be used at all).
func copyFileOptimized(src, dst string) (int64, error) {
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

	buf := make([]byte, fixedBufferSize)
	return io.CopyBuffer(dstFile, srcFile, buf)
}

// writeTestFileContentOptimizedContext is the one test-file writer: every
// tester's files and every fill's files come from it, so the verifier reads
// exactly what it was written for (CAP-01, CAP-02).
func writeTestFileContentOptimizedContext(ctx context.Context, filePath string, fileSize int64) (created bool, err error) {
	return writeCapacityFile(ctx, filePath, fileSize, testFileBufferSize, nil)
}

// createTesterFile is the body of every tester's CreateTestFileContext.
func createTesterFile(ctx context.Context, root, fileName string, fileSize int64) (string, error) {
	path := filepath.Join(root, fileName)
	created, err := writeTestFileContentOptimizedContext(ctx, path, fileSize)
	if err != nil {
		if !created {
			path = ""
		}
		return path, fmt.Errorf("failed to create test file %s: %w", fileName, err)
	}
	return path, nil
}

// cleanupHint is the command that removes a target's test files: the clean
// verb, on the folder that actually holds them (CAP-05). It used to be
// `filedo device X fill clean`, which starts a fill.
func cleanupHint(path string) string {
	if strings.ContainsAny(path, " \t") {
		return fmt.Sprintf(`filedo "%s" clean`, path)
	}
	return fmt.Sprintf("filedo %s clean", path)
}

// verifyCachedReads counts verifications that had to read through the cache
// because the target refused unbuffered I/O.
var verifyCachedReads atomic.Int64

// fileCheckError names the test file a verification failed on.
type fileCheckError struct {
	Index int
	Path  string
	Err   error
}

func (e *fileCheckError) Error() string {
	return fmt.Sprintf("file %d (%s): %v", e.Index, e.Path, e.Err)
}

func (e *fileCheckError) Unwrap() error { return e.Err }

// verifyTestFileQuick checks the header block, the footer and one block from
// the middle half of the file.
func verifyTestFileQuick(filePath string) error {
	return verifyTestFileWith(filePath, func(first, last int64) []int64 {
		span := last - first + 1
		return stratifiedBlocks(first+span/4, last-span/4, 1)
	})
}

// verifyTestFileComplete checks the header block, the footer, the first and
// last body blocks and three random body blocks.
func verifyTestFileComplete(filePath string) error {
	return verifyTestFileWith(filePath, func(first, last int64) []int64 {
		return append([]int64{first, last}, stratifiedBlocks(first, last, 3)...)
	})
}

// verifyTestFileSampled checks the header block, the footer and k body blocks,
// one from each of k equal stretches of the file, so no stretch goes unread.
func verifyTestFileSampled(filePath string, k int) error {
	return verifyTestFileWith(filePath, func(first, last int64) []int64 {
		return stratifiedBlocks(first, last, k)
	})
}

// stratifiedBlocks picks one random block in each of k equal stretches of
// [first, last].
func stratifiedBlocks(first, last int64, k int) []int64 {
	span := last - first + 1
	if span <= 0 || k <= 0 {
		return nil
	}
	if int64(k) > span {
		k = int(span)
	}
	out := make([]int64, 0, k)
	for s := int64(0); s < int64(k); s++ {
		lo := first + span*s/int64(k)
		hi := first + span*(s+1)/int64(k)
		out = append(out, lo+mrand.Int64N(hi-lo))
	}
	return out
}

// verifyTestFileWith reads a current-format test file past the cache: the
// whole first block (header included), the footer, and the body blocks pick
// chooses among [first, last]. Any byte that is not the one written is a
// tfMismatchError (errDataMismatch); a failure to read is returned as is.
func verifyTestFileWith(filePath string, pick func(first, last int64) []int64) error {
	r, err := openForVerify(filePath)
	if err != nil {
		return fmt.Errorf("could not open test file: %w", err)
	}
	defer r.Close()
	if !r.Unbuffered() {
		verifyCachedReads.Add(1)
	}
	meta, err := readCurrentHeader(r, filePath)
	if err != nil {
		return err
	}
	size := meta.Size
	got := make([]byte, 2*tfBlockSize)
	want := make([]byte, 2*tfBlockSize)

	check := func(off int64, n int64) error {
		nr, err := r.ReadAt(got[:n], off)
		if int64(nr) < n {
			if err == nil || errors.Is(err, io.EOF) {
				return &tfMismatchError{Path: filePath, Offset: off + int64(nr), What: "is missing - the file ends early"}
			}
			return fmt.Errorf("could not read offset %d: %w", off, err)
		}
		return meta.check(filePath, got[:n], off, want)
	}

	head := int64(tfBlockSize)
	if head > size {
		head = size
	}
	if err := check(0, head); err != nil {
		return err
	}
	footerStart := (size - int64(len(meta.Header))) / tfBlockSize * tfBlockSize
	if footerStart < head {
		footerStart = head
	}
	if footerStart < size {
		if err := check(footerStart, size-footerStart); err != nil {
			return err
		}
	}
	blocks := (size + tfBlockSize - 1) / tfBlockSize
	if blocks > 2 {
		for _, b := range pick(1, blocks-2) {
			off := b * tfBlockSize
			n := int64(tfBlockSize)
			if off+n > size {
				n = size - off
			}
			if err := check(off, n); err != nil {
				return err
			}
		}
	}
	return nil
}

// readCurrentHeader reads and checks the header of a current-format test
// file: it must be one, it must name this file, and its size must be the
// file's.
func readCurrentHeader(r verifyReader, filePath string) (*testFileMeta, error) {
	size := r.Size()
	n := int64(tfBlockSize)
	if n > size {
		n = size
	}
	head := make([]byte, n)
	nr, err := r.ReadAt(head, 0)
	if nr == 0 && err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("could not read the header: %w", err)
	}
	head = head[:nr]
	var nilMeta *testFileMeta
	line, ok := headerLine(head)
	if !ok || !strings.HasPrefix(line, tfHeaderPrefix) {
		return nil, &tfMismatchError{Path: filePath, Offset: 0, What: "has no FileDO header: it " + nilMeta.describeForeign(head)}
	}
	meta, err := parseTestFileHeader(line)
	if err != nil {
		return nil, &tfMismatchError{Path: filePath, Offset: 0, What: "has a damaged FileDO header"}
	}
	if meta.Name != filepath.Base(filePath) {
		return nil, &tfMismatchError{Path: filePath, Offset: 0,
			What: fmt.Sprintf("holds the header of test file %s - another file's data", meta.Name)}
	}
	if meta.Size != size {
		return nil, &tfMismatchError{Path: filePath, Offset: 0,
			What: fmt.Sprintf("belongs to a file of %d bytes, but the file is %d bytes", meta.Size, size)}
	}
	return meta, nil
}

// verifyTestFilesSampled re-reads every file (header, footer, k sampled
// blocks). It returns how many read back intact and the first failure; a stop
// ends it early.
func verifyTestFilesSampled(filePaths []string, k int) (good int, firstBad error) {
	for i, p := range filePaths {
		if runStopRequested() {
			if firstBad == nil {
				firstBad = errRunStopped
			}
			return good, firstBad
		}
		if err := verifyTestFileSampled(p, k); err != nil {
			if firstBad == nil {
				firstBad = &fileCheckError{Index: i + 1, Path: p, Err: err}
			}
			continue
		}
		good++
	}
	return good, firstBad
}

// verifySmartTestFiles checks key files after each write to detect fake capacity early.
//
// Strategy:
//   - Current file: always quick-verified (catches write errors immediately)
//   - File 1: quick-verified after EVERY write - detects the moment a fake
//     controller overwrites the first file's middle blocks with later data
//   - File 5 and 10: quick-verified every 10th / 20th write (secondary anchors)
//   - Every 5th write: full verification of the current file
//
// Every read bypasses the cache; the final pass covers every other file.
func verifySmartTestFiles(filePaths []string, currentIndex int) error {
	if len(filePaths) == 0 {
		return nil
	}

	// map[arrayIndex] → true=full, false=quick
	filesToVerify := make(map[int]bool)

	// Current file: full every 5th, quick otherwise
	filesToVerify[currentIndex-1] = currentIndex%5 == 0

	// File 1: quick after EVERY write - earliest possible fake detection
	if len(filePaths) > 1 {
		filesToVerify[0] = false
	}

	// File 5: quick every 10th write
	if currentIndex%10 == 0 && len(filePaths) >= 5 {
		filesToVerify[4] = false
	}

	// File 10: quick every 20th write
	if currentIndex%20 == 0 && len(filePaths) >= 10 {
		filesToVerify[9] = false
	}

	for fileIndex, fullVerification := range filesToVerify {
		if fileIndex < 0 || fileIndex >= len(filePaths) {
			continue
		}
		filePath := filePaths[fileIndex]
		var err error
		if fullVerification {
			err = verifyTestFileComplete(filePath)
		} else {
			err = verifyTestFileQuick(filePath)
		}
		if err != nil {
			return &fileCheckError{Index: fileIndex + 1, Path: filePath, Err: err}
		}
	}

	return nil
}

// Helper function to get enhanced target info
func getEnhancedTargetInfo(tester FakeCapacityTester) string {
	testType, targetPath := tester.GetTestInfo()

	// Try to get additional info based on tester type. Read-only: a header
	// line never writes to the volume it describes.
	switch testType {
	case "Device", "Folder":
		return getEnhancedDeviceInfo(targetPath)
	case "Network":
		// For network paths, just show the path
		return targetPath
	}

	// Fallback to simple path
	return targetPath
}
