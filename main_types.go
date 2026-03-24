package main

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Global variable to control verification mode
var DeepVerificationMode = false

// FakeCapacityTester interface defines the operations needed for fake capacity testing
type FakeCapacityTester interface {
	// GetTestInfo returns the test type name and target path for display
	GetTestInfo() (testType, targetPath string)

	// GetAvailableSpace returns the available space in bytes for testing
	GetAvailableSpace() (int64, error)

	// CreateTestFile creates a test file with the given size and returns the file path
	CreateTestFile(fileName string, fileSize int64) (filePath string, err error)

	// CreateTestFileContext creates a test file with context for cancellation support
	// If not implemented, should return the same as CreateTestFile
	CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error)

	// VerifyTestFile verifies that a test file contains the expected header
	VerifyTestFile(filePath string) error

	// CleanupTestFile removes a test file
	CleanupTestFile(filePath string) error

	// GetCleanupCommand returns the command to clean test files manually
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
	b.WriteString(fmt.Sprintf("  Usage:         %.1f%%\n", float64(di.TotalBytes-di.FreeBytes)*100/float64(di.TotalBytes)))
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
	usage := float64(di.TotalBytes-di.FreeBytes) * 100 / float64(di.TotalBytes)

	b.WriteString(fmt.Sprintf("Total:  %s, Free:  %s (Usage: %.1f%%)", totalFormatted, freeFormatted, usage))

	return b.String()
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

// Generic fake capacity testing functions

// runGenericFakeCapacityTest performs a generic fake capacity test using the provided tester interface
func runGenericFakeCapacityTest(tester FakeCapacityTester, autoDelete bool, logger *HistoryLogger) (*FakeCapacityTestResult, error) {
	testType, targetPath := tester.GetTestInfo()

	// Use global interrupt handler (avoid duplicate signal registration)
	interruptHandler := globalInterruptHandler

	// Setup history logging if provided
	if logger != nil {
		logger.SetCommand(strings.ToLower(testType), targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
	}

	result := &FakeCapacityTestResult{
		CreatedFiles: make([]string, 0, 100),
	}

	// Get available space
	freeSpace, err := tester.GetAvailableSpace()
	if err != nil {
		if logger != nil {
			logger.SetError(err)
		}
		return result, err
	}

	// Check minimum space requirement (100MB)
	minSpaceBytes := int64(100 * 1024 * 1024) // 100MB
	if freeSpace < minSpaceBytes {
		err = fmt.Errorf("insufficient free space. At least 100MB required, but only %d MB available", freeSpace/(1024*1024))
		if logger != nil {
			logger.SetError(err)
		}
		return result, err
	}

	// Calculate file size to use 95% of available space for 100 files
	const maxFiles = 100
	totalDataTarget := int64(float64(freeSpace) * 0.95) // Use 95% of available space
	fileSize := totalDataTarget / maxFiles
	fileSizeMB := fileSize / (1024 * 1024)

	// Ensure minimum file size of 1MB
	if fileSize < 1024*1024 {
		fileSize = 1024 * 1024 // 1MB minimum
		fileSizeMB = 1
	}

	fmt.Printf("%s Fake Capacity Test\n", testType)
	fmt.Printf("Target: %s\n", getEnhancedTargetInfo(tester))
	fmt.Printf("Available space: %.2f GB\n", float64(freeSpace)/(1024*1024*1024))
	fmt.Printf("Test file size: %d MB (%.1f%% of available space for %d files)\n",
		fileSizeMB, float64(totalDataTarget)/float64(freeSpace)*100, maxFiles)
	fmt.Printf("Will create %d test files...\n\n", maxFiles)

	// Write buffer is fixedBufferSize (64 MB) — set in writeTestFileContentOptimized.
	// No upfront calibration: eliminates 300 MB of wasted writes before the test.

	const baselineFileCount = 3
	var speeds []float64
	var baselineSpeed float64
	baselineSet := false

	// Create progress tracker
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSize, 2*time.Second)

	// Write phase
	fmt.Printf("Starting capacity test - writing %d files...\n", maxFiles)

	for i := 1; i <= maxFiles; i++ {
		// Check for interrupt
		if interruptHandler.IsCancelled() {
			fmt.Printf("\n\n⚠ Operation interrupted by user. Cleaning up created files...\n")

			// Cleanup created files
			deletedCount := 0
			for _, filePath := range result.CreatedFiles {
				if err := tester.CleanupTestFile(filePath); err == nil {
					deletedCount++
				}
			}

			fmt.Printf("Cleaned up %d/%d files.\n", deletedCount, len(result.CreatedFiles))
			err := fmt.Errorf("operation interrupted by user")
			if logger != nil {
				logger.SetError(err)
				logger.SetResult("filesCreated", result.FilesCreated)
				logger.SetResult("interrupted", true)
			}
			return result, err
		}

		fileName := fmt.Sprintf("FILL_%03d_%s.tmp", i, time.Now().Format("02150405"))

		start := time.Now()
		filePath, err := tester.CreateTestFileContext(interruptHandler.Context(), fileName, fileSize)
		if err != nil {
			// DON'T clean up on creation error - keep files for analysis
			result.FailureReason = fmt.Sprintf("Failed to create file %d: %v", i, err)

			// Calculate estimated real capacity
			realCapacity := fileSize * int64(i-1)

			fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
			fmt.Printf("This indicates storage device failure or fake capacity.\n")
			fmt.Printf("\n📊 ESTIMATED REAL CAPACITY ANALYSIS:\n")
			fmt.Printf("  Files successfully created: %d out of %d\n", i-1, maxFiles)
			fmt.Printf("  Data written before failure: %.2f GB\n", float64(fileSize*int64(i-1))/(1024*1024*1024))
			fmt.Printf("  ESTIMATED REAL FREE SPACE: %.2f GB\n", float64(realCapacity)/(1024*1024*1024))
			fmt.Printf("\n⚠️  Test files preserved for analysis (%d files).\n", len(result.CreatedFiles))

			err = fmt.Errorf("failed to create file %s: %v", fileName, err)
			if logger != nil {
				logger.SetError(err)
				logger.SetResult("estimatedRealCapacityGB", float64(realCapacity)/(1024*1024*1024))
				logger.SetResult("filesSuccessfullyCreated", i-1)
			}
			return result, err
		}
		duration := time.Since(start)

		result.FilesCreated++
		result.TotalDataBytes += fileSize
		result.CreatedFiles = append(result.CreatedFiles, filePath)

		// Smart verification with new strategy
		if err := verifySmartTestFiles(result.CreatedFiles, i); err != nil {
			// DON'T clean up on verification error - keep files for analysis
			result.TestPassed = false
			result.FailureReason = fmt.Sprintf("Verification failed after creating file %d: %v", i, err)

			// Calculate estimated real capacity
			realCapacity := fileSize * int64(i-1) // Count files before the failed one

			fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
			fmt.Printf("This indicates delayed data corruption or fake capacity.\n")
			fmt.Printf("Error details: %v\n", err)

			// Try to find which specific file failed
			for j, fp := range result.CreatedFiles {
				if verifyErr := verifyTestFileStartEnd(fp); verifyErr != nil {
					fmt.Printf("Failed file: %s (file %d/%d)\n", fp, j+1, len(result.CreatedFiles))

					// Additional file analysis
					if fileInfo, statErr := os.Stat(fp); statErr == nil {
						fmt.Printf("File size: %d bytes (expected: %d bytes)\n", fileInfo.Size(), fileSize)
						if fileInfo.Size() != fileSize {
							fmt.Printf("❌ FILE SIZE MISMATCH - This confirms fake capacity!\n")
						}
					}

					// Try to read first few bytes for diagnosis
					if diagFile, diagErr := os.Open(fp); diagErr == nil {
						diagBuf := make([]byte, 128)
						if n, readErr := diagFile.Read(diagBuf); readErr == nil && n > 0 {
							fmt.Printf("File content preview (first %d bytes): %q\n", n, string(diagBuf[:n]))

							// Check if file contains zeros (common in fake capacity)
							zeroCount := 0
							for _, b := range diagBuf[:n] {
								if b == 0 {
									zeroCount++
								}
							}
							if zeroCount > n/2 {
								fmt.Printf("❌ FILE CONTAINS MOSTLY ZEROS - Strong indicator of fake capacity!\n")
							}
						}
						diagFile.Close()
					}
					break
				}
			}

			fmt.Printf("\n📊 ESTIMATED REAL CAPACITY ANALYSIS:\n")
			fmt.Printf("  Files successfully verified: %d out of %d\n", i-1, len(result.CreatedFiles))
			fmt.Printf("  Data verified before failure: %.2f GB\n", float64(fileSize*int64(i-1))/(1024*1024*1024))
			fmt.Printf("  ESTIMATED REAL FREE SPACE: %.2f GB\n", float64(realCapacity)/(1024*1024*1024))
			fmt.Printf("\n⚠️  Test files preserved for analysis (%d files).\n", len(result.CreatedFiles))

			err = fmt.Errorf("test failed during verification - file corruption detected")
			if logger != nil {
				logger.SetError(err)
				logger.SetResult("estimatedRealCapacityGB", float64(realCapacity)/(1024*1024*1024))
				logger.SetResult("filesSuccessfullyVerified", i-1)
			}
			return result, err
		}

		// Calculate write speed
		speed := float64(fileSize) / duration.Seconds() / (1024 * 1024) // MB/s
		speeds = append(speeds, speed)

		// Update progress
		progress.Update(int64(result.FilesCreated), result.TotalDataBytes)
		progress.PrintProgress("Test")

		// Set baseline speed from first 3 files
		if i <= baselineFileCount {
			if i == baselineFileCount {
				// Calculate average of first 3 files as baseline
				sum := 0.0
				for _, s := range speeds[:baselineFileCount] {
					sum += s
				}
				baselineSpeed = sum / float64(baselineFileCount)
				result.BaselineSpeedMBps = baselineSpeed
				baselineSet = true
				fmt.Printf("Baseline speed established: %.2f MB/s", baselineSpeed)
			}
		} else if baselineSet {
			// Check for abnormal speed after baseline is set
			if speed < baselineSpeed*0.1 { // Less than 10% of baseline
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Speed dropped to %.2f MB/s (less than 10%% of baseline %.2f MB/s) at file %d", speed, baselineSpeed, i)

				// Calculate estimated real capacity
				realCapacity := fileSize * int64(i-1)

				fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
				fmt.Printf("This indicates potential fake capacity or device failure.\n")
				fmt.Printf("\n📊 ESTIMATED REAL CAPACITY ANALYSIS:\n")
				fmt.Printf("  Files successfully written: %d out of %d\n", i-1, maxFiles)
				fmt.Printf("  Data written before failure: %.2f GB\n", float64(fileSize*int64(i-1))/(1024*1024*1024))
				fmt.Printf("  ESTIMATED REAL FREE SPACE: %.2f GB\n", float64(realCapacity)/(1024*1024*1024))
				fmt.Printf("\n⚠️  Test files preserved for analysis (%d files).\n", len(result.CreatedFiles))

				err = fmt.Errorf("test failed due to abnormally slow write speed")
				if logger != nil {
					logger.SetError(err)
					logger.SetResult("estimatedRealCapacityGB", float64(realCapacity)/(1024*1024*1024))
					logger.SetResult("filesSuccessfullyWritten", i-1)
				}
				return result, err
			}
			if speed > baselineSpeed*10 { // More than 10x baseline
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Speed jumped to %.2f MB/s (more than 1000%% of baseline %.2f MB/s) at file %d", speed, baselineSpeed, i)

				// Calculate estimated real capacity
				realCapacity := fileSize * int64(i-1)

				fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
				fmt.Printf("This indicates potential fake writing or caching issues.\n")
				fmt.Printf("\n📊 ESTIMATED REAL CAPACITY ANALYSIS:\n")
				fmt.Printf("  Files successfully written: %d out of %d\n", i-1, maxFiles)
				fmt.Printf("  Data written before failure: %.2f GB\n", float64(fileSize*int64(i-1))/(1024*1024*1024))
				fmt.Printf("  ESTIMATED REAL FREE SPACE: %.2f GB\n", float64(realCapacity)/(1024*1024*1024))
				fmt.Printf("\n⚠️  Test files preserved for analysis (%d files).\n", len(result.CreatedFiles))

				err = fmt.Errorf("test failed due to abnormally fast write speed")
				if logger != nil {
					logger.SetError(err)
					logger.SetResult("estimatedRealCapacityGB", float64(realCapacity)/(1024*1024*1024))
					logger.SetResult("filesSuccessfullyWritten", i-1)
				}
				return result, err
			}
		}
	}

	fmt.Printf("\n✅ Write and smart incremental verification completed successfully!\n")
	fmt.Printf("All %d files verified with optimized smart verification strategy.\n", len(result.CreatedFiles))

	// Calculate statistics
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

	fmt.Printf("\n✅ TEST PASSED SUCCESSFULLY!\n")
	fmt.Printf("All %d files were written and verified successfully.\n", result.FilesCreated)
	fmt.Printf("\n📊 Speed Statistics:\n")
	fmt.Printf("  Baseline speed (first 3 files): %.2f MB/s\n", result.BaselineSpeedMBps)
	fmt.Printf("  Average speed: %.2f MB/s\n", result.AverageSpeedMBps)
	fmt.Printf("  Minimum speed: %.2f MB/s\n", result.MinSpeedMBps)
	fmt.Printf("  Maximum speed: %.2f MB/s\n", result.MaxSpeedMBps)
	fmt.Printf("  Total data written: %.2f MB\n", float64(result.TotalDataBytes)/(1024*1024))

	// Auto-delete if requested and test passed
	if autoDelete {
		fmt.Printf("\n🗑️  Auto-delete enabled, cleaning up test files...\n")
		deletedCount := 0
		for _, filePath := range result.CreatedFiles {
			if err := tester.CleanupTestFile(filePath); err != nil {
				fmt.Printf("Warning: Failed to delete file: %v\n", err)
			} else {
				deletedCount++
			}
		}
		fmt.Printf("Successfully deleted %d/%d test files.\n", deletedCount, len(result.CreatedFiles))
	} else {
		fmt.Printf("\n📁 Test files kept for manual inspection:\n")
		fmt.Printf("   Location: %s\n", targetPath)
		fmt.Printf("   Files: FILL_001_*.tmp to FILL_%03d_*.tmp\n", result.FilesCreated)
		fmt.Printf("   Use '%s' to remove them later.\n", tester.GetCleanupCommand())
	}

	// Log results if logger provided
	if logger != nil {
		logger.SetResult("testPassed", result.TestPassed)
		logger.SetResult("averageSpeedMBps", result.AverageSpeedMBps)
		logger.SetResult("minSpeedMBps", result.MinSpeedMBps)
		logger.SetResult("maxSpeedMBps", result.MaxSpeedMBps)
		logger.SetResult("baselineSpeedMBps", result.BaselineSpeedMBps)
		logger.SetResult("totalDataMB", float64(result.TotalDataBytes)/(1024*1024))
		logger.SetResult("filesDeleted", autoDelete)
		logger.SetSuccess()
	}

	return result, nil
}

// Optimized file operations without template files

const fixedBufferSize = 64 * 1024 * 1024 // 64 MB — good for all device types

// copyFileOptimized copies src→dst with a fixed 64 MB buffer.
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

// writeTestFileContentOptimized writes a test file with a fixed 64 MB buffer.
func writeTestFileContentOptimized(filePath string, fileSize int64) error {
	return writeTestFileContentOptimizedContext(context.Background(), filePath, fileSize)
}

func writeTestFileContentOptimizedContext(ctx context.Context, filePath string, fileSize int64) error {
	return writeTestFileWithBufferContext(ctx, filePath, fileSize, fixedBufferSize)
}

// verifyTestFileStartEnd verifies file has correct header at start and end
func verifyTestFileStartEnd(filePath string) error {
	return verifyTestFileComplete(filePath)
}

// verifyTestFileQuick performs quick verification (header + footer + one random middle position)
func verifyTestFileQuick(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("could not get file info: %v", err)
	}

	fileSize := fileInfo.Size()
	if fileSize < 100 {
		return fmt.Errorf("file too small: %d bytes", fileSize)
	}

	// Read first line (header)
	firstLineBuffer := make([]byte, 256)
	n, err := file.Read(firstLineBuffer)
	if err != nil {
		return fmt.Errorf("could not read file header: %v", err)
	}

	if n == 0 {
		return fmt.Errorf("file is empty")
	}

	// Extract first line
	firstLine := string(firstLineBuffer[:n])
	if newlineIndex := strings.Index(firstLine, "\n"); newlineIndex > 0 {
		firstLine = firstLine[:newlineIndex]
	}

	// Check header format
	if !strings.HasPrefix(firstLine, "FILEDO_TEST_") {
		return fmt.Errorf("invalid header format: %s", firstLine)
	}

	// Calculate last line position
	lastLinePos := fileSize - int64(len(firstLine)+1)
	if lastLinePos < 0 {
		lastLinePos = 0
	}

	// Seek to last line
	_, err = file.Seek(lastLinePos, 0)
	if err != nil {
		return fmt.Errorf("could not seek to last line: %v", err)
	}

	// Read last line
	lastLineBuffer := make([]byte, 256)
	n, err = file.Read(lastLineBuffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("could not read file footer: %v", err)
	}

	// Extract last line
	lastLine := string(lastLineBuffer[:n])
	if newlineIndex := strings.Index(lastLine, "\n"); newlineIndex > 0 {
		lastLine = lastLine[:newlineIndex]
	}

	// Check header/footer match
	if firstLine != lastLine {
		return fmt.Errorf("header/footer mismatch: '%s' vs '%s'", firstLine, lastLine)
	}

	// Quick pattern check in middle (only if file is large enough)
	const minSizeForPatternCheck = 1024
	if fileSize >= minSizeForPatternCheck {
		if err := verifyPatternQuick(file, fileSize, firstLine); err != nil {
			return err
		}
	}

	return nil
}

// verifyTestFileComplete performs comprehensive verification of test file
func verifyTestFileComplete(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Get file info
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("could not get file info: %v", err)
	}

	fileSize := fileInfo.Size()
	if fileSize < 100 {
		return fmt.Errorf("file too small: %d bytes", fileSize)
	}

	// Read first line (header)
	firstLineBuffer := make([]byte, 256)
	n, err := file.Read(firstLineBuffer)
	if err != nil {
		return fmt.Errorf("could not read file header: %v", err)
	}

	if n == 0 {
		return fmt.Errorf("file is empty")
	}

	// Extract first line
	firstLineStr := string(firstLineBuffer[:n])
	lines := strings.Split(firstLineStr, "\n")
	if len(lines) == 0 {
		return fmt.Errorf("no complete line found in file header")
	}

	expectedHeader := lines[0] + "\n"

	// Validate header format
	if !strings.HasPrefix(expectedHeader, "FILEDO_TEST_") {
		return fmt.Errorf("invalid header format - expected 'FILEDO_TEST_...' but found '%s'", lines[0])
	}

	headerLen := int64(len(expectedHeader))

	if fileSize < headerLen*2 {
		return fmt.Errorf("file too small - expected at least %d bytes but got %d", headerLen*2, fileSize)
	}

	// Check footer (last line should match header)
	footerStart := fileSize - headerLen
	_, err = file.Seek(footerStart, 0)
	if err != nil {
		return fmt.Errorf("could not seek to footer position: %v", err)
	}

	footerBuffer := make([]byte, headerLen)
	n, err = file.Read(footerBuffer)
	if err != nil {
		return fmt.Errorf("could not read file footer: %v", err)
	}

	actualFooter := string(footerBuffer[:n])

	if expectedHeader != actualFooter {
		return fmt.Errorf("header/footer mismatch - header: '%s' but footer: '%s'",
			strings.TrimSuffix(expectedHeader, "\n"), strings.TrimSuffix(actualFooter, "\n"))
	}

	// Extract the file-specific body tag from the header.
	// Header: "FILEDO_TEST_FILL_001_ddHHmmss.tmp_timestamp" → tag "F001_"
	// Body written as "F001_ABCDE... F001_ABCDE..." repeating.
	// A fake controller that overwrites file 001's middle with file 042's body
	// will have "F042_" instead of "F001_" → detected immediately.
	bodyTag := ""
	const hdrPrefix = "FILEDO_TEST_FILL_"
	hdr := strings.TrimSuffix(string(expectedHeader), "\n")
	if strings.HasPrefix(hdr, hdrPrefix) {
		rest := hdr[len(hdrPrefix):]
		if idx := strings.IndexByte(rest, '_'); idx > 0 {
			bodyTag = "F" + rest[:idx] + "_"
		}
	}

	// Define check positions: just after header, near footer, and 3 random middle spots
	dataStart := headerLen
	dataEnd := footerStart
	dataLength := dataEnd - dataStart

	if dataLength < 64 || bodyTag == "" {
		return nil
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	checkPositions := []int64{
		dataStart,
		dataEnd - int64(len(bodyTag)*4),
	}
	for i := 0; i < 3; i++ {
		minP := dataStart + int64(len(bodyTag))
		maxP := dataEnd - int64(len(bodyTag)*4)
		if maxP > minP {
			checkPositions = append(checkPositions, minP+rng.Int63n(maxP-minP))
		}
	}

	readBuffer := make([]byte, 256)

	for _, pos := range checkPositions {
		if pos < dataStart || pos+int64(len(bodyTag)) >= dataEnd {
			continue
		}
		if _, err := file.Seek(pos, 0); err != nil {
			return fmt.Errorf("could not seek to position %d: %v", pos, err)
		}
		n, err := file.Read(readBuffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("could not read at position %d: %v", pos, err)
		}
		if !strings.Contains(string(readBuffer[:n]), bodyTag) {
			return fmt.Errorf("body corruption at offset %d: expected tag %q not found (wrong file's data?)", pos, bodyTag)
		}
	}

	return nil
}

// verifyPatternQuick checks one random middle position for the file-specific body tag.
func verifyPatternQuick(file *os.File, fileSize int64, firstLine string) error {
	// Extract the body tag written into this file's body: "FILL_001_..." → "F001_"
	bodyTag := ""
	const hdrPrefix = "FILEDO_TEST_FILL_"
	if strings.HasPrefix(firstLine, hdrPrefix) {
		rest := firstLine[len(hdrPrefix):]
		if idx := strings.IndexByte(rest, '_'); idx > 0 {
			bodyTag = "F" + rest[:idx] + "_"
		}
	}
	if bodyTag == "" {
		return nil
	}

	headerSize := int64(len(firstLine) + 1)
	dataStart := headerSize
	dataEnd := fileSize - headerSize
	if dataEnd-dataStart < int64(len(bodyTag)*4) {
		return nil
	}

	// One random position in the middle quarter of the file
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	minPos := dataStart + (dataEnd-dataStart)/4
	maxPos := dataEnd - (dataEnd-dataStart)/4
	if maxPos <= minPos {
		return nil
	}
	pos := minPos + rng.Int63n(maxPos-minPos)

	if _, err := file.Seek(pos, 0); err != nil {
		return fmt.Errorf("could not seek to position %d: %v", pos, err)
	}
	buf := make([]byte, 256)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return fmt.Errorf("could not read at position %d: %v", pos, err)
	}
	if !strings.Contains(string(buf[:n]), bodyTag) {
		return fmt.Errorf("body corruption at offset %d: tag %q not found", pos, bodyTag)
	}
	return nil
}

// verifySmartTestFiles checks key files after each write to detect fake capacity early.
//
// Strategy:
//   - Current file: always quick-verified (catches write errors immediately)
//   - File 1: quick-verified after EVERY write — detects the moment a fake
//     controller overwrites the first file's middle blocks with later data
//   - File 5 and 10: quick-verified every 10th / 20th write (secondary anchors)
//   - Every 5th write: full (body-tag) verification of the current file
func verifySmartTestFiles(filePaths []string, currentIndex int) error {
	if len(filePaths) == 0 {
		return nil
	}

	// map[arrayIndex] → true=full, false=quick
	filesToVerify := make(map[int]bool)

	// Current file: full every 5th, quick otherwise
	if currentIndex%5 == 0 {
		filesToVerify[currentIndex-1] = true
	} else {
		filesToVerify[currentIndex-1] = false
	}

	// File 1: quick after EVERY write — earliest possible fake detection
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
		if fileIndex >= len(filePaths) {
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
			verifyType := "quick"
			if fullVerification {
				verifyType = "full"
			}
			return fmt.Errorf("file %d/%d (%s) %s verification failed: %v",
				fileIndex+1, len(filePaths), filePath, verifyType, err)
		}
	}

	return nil
}

// verifyAllTestFiles verifies all files in the list with progress indication
func verifyAllTestFiles(filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	//fmt.Printf("Verifying %d files... ", len(filePaths))

	for i, filePath := range filePaths {
		if err := verifyTestFileStartEnd(filePath); err != nil {
			fmt.Printf("❌ FAILED at file %d/%d\n", i+1, len(filePaths))
			return fmt.Errorf("file %d/%d (%s) verification failed: %v", i+1, len(filePaths), filePath, err)
		}
	}

	//fmt.Printf("✅ OK\n")
	return nil
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


func writeTestFileWithBuffer(filePath string, fileSize int64, bufferSize int) error {
	return writeTestFileWithBufferContext(context.Background(), filePath, fileSize, bufferSize)
}

func writeTestFileWithBufferContext(ctx context.Context, filePath string, fileSize int64, bufferSize int) error {
	// Create file with optimized flags for faster writing
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
	remaining := fileSize - int64(written) - int64(len(headerLine)) // Reserve space for footer (same as header)

	// Build body pattern: embed file sequence number so middle reads are file-specific.
	// A fake controller overwriting file 002's middle with file 042's body is caught
	// because "F042_" != "F002_" when verifying.
	// Extract sequence number from filename: FILL_00001_... → "00001"
	seqTag := ""
	if strings.HasPrefix(fileName, "FILL_") {
		parts := strings.SplitN(fileName, "_", 3)
		if len(parts) >= 2 {
			seqTag = "F" + parts[1] + "_"
		}
	}
	pattern := seqTag + "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(pattern)
	block := make([]byte, bufferSize)

	// Fill buffer with pattern - optimize by pre-filling once
	for i := 0; i < bufferSize; {
		copyLen := min(len(patternBytes), bufferSize-i)
		copy(block[i:i+copyLen], patternBytes[:copyLen])
		i += copyLen
	}

	// Write data blocks in larger chunks
	for remaining > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		writeSize := bufferSize
		if remaining < int64(bufferSize) {
			writeSize = int(remaining)
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

	// Explicitly sync only once at the end for better performance
	return file.Sync()
}

// Global variable to cache optimal buffer sizes per path

// Helper function to get enhanced target info
func getEnhancedTargetInfo(tester FakeCapacityTester) string {
	testType, targetPath := tester.GetTestInfo()

	// Try to get additional info based on tester type
	switch testType {
	case "Device":
		// Get device info for enhanced display
		if deviceInfo, err := getDeviceInfo(targetPath, false); err == nil {
			volumeName := deviceInfo.VolumeName
			if volumeName == "" {
				volumeName = "No label"
			}
			totalSizeGB := float64(deviceInfo.TotalBytes) / (1024 * 1024 * 1024)
			return fmt.Sprintf("%s (%s) [%.1f GB]", targetPath, volumeName, totalSizeGB)
		}
	case "Folder":
		// For folders, try to get the drive info
		if absPath, err := filepath.Abs(targetPath); err == nil {
			if len(absPath) >= 3 && absPath[1] == ':' {
				drivePath := absPath[:3] // "C:\"
				if deviceInfo, err := getDeviceInfo(drivePath, false); err == nil {
					volumeName := deviceInfo.VolumeName
					if volumeName == "" {
						volumeName = "No label"
					}
					totalSizeGB := float64(deviceInfo.TotalBytes) / (1024 * 1024 * 1024)
					return fmt.Sprintf("%s (%s) [%.1f GB]", targetPath, volumeName, totalSizeGB)
				}
			}
		}
	case "Network":
		// For network paths, just show the path
		return targetPath
	}

	// Fallback to simple path
	return targetPath
}

// writeTestFileContent writes test content to a file in chunks to avoid memory issues
