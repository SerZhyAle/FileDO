package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"
)

// FakeCapacityTester interface defines the operations needed for fake capacity testing
type FakeCapacityTester interface {
	// GetTestInfo returns the test type name and target path for display
	GetTestInfo() (testType, targetPath string)

	// GetAvailableSpace returns the available space in bytes for testing
	GetAvailableSpace() (int64, error)

	// CreateTestFile creates a test file with the given size and returns the file path
	CreateTestFile(fileName string, fileSize int64) (filePath string, err error)

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
	b.WriteString(fmt.Sprintf("  Usage:         %.2f%%\n", float64(di.TotalBytes-di.FreeBytes)*100/float64(di.TotalBytes)))
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
	fmt.Printf("Target: %s\n", targetPath)
	fmt.Printf("Available space: %.2f GB\n", float64(freeSpace)/(1024*1024*1024))
	fmt.Printf("Test file size: %d MB (%.1f%% of available space for %d files)\n",
		fileSizeMB, float64(totalDataTarget)/float64(freeSpace)*100, maxFiles)
	fmt.Printf("Will create %d test files...\n\n", maxFiles)

	const baselineFileCount = 3
	var speeds []float64
	var baselineSpeed float64
	baselineSet := false

	// Create progress tracker
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSize, 2*time.Second)

	// Write phase
	fmt.Printf("Starting capacity test - writing %d files...\n", maxFiles)

	for i := 1; i <= maxFiles; i++ {
		fileName := fmt.Sprintf("FILL_%03d_%s.tmp", i, time.Now().Format("02150405"))

		start := time.Now()
		filePath, err := tester.CreateTestFile(fileName, fileSize)
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

		// Optimized incremental verification strategy
		verifyCount := 0
		if i <= 5 {
			// Verify all files for first 5 files (critical phase)
			verifyCount = len(result.CreatedFiles)
		} else if i%10 == 0 || i == maxFiles {
			// Every 10th file or last file - verify all
			verifyCount = len(result.CreatedFiles)
		} else {
			// Normal operation - verify only last 3 files (recent files check)
			verifyCount = 3
			if verifyCount > len(result.CreatedFiles) {
				verifyCount = len(result.CreatedFiles)
			}
		}

		startIdx := len(result.CreatedFiles) - verifyCount
		for j := startIdx; j < len(result.CreatedFiles); j++ {
			prevFilePath := result.CreatedFiles[j]
			err = verifyFileHeaderFast(prevFilePath) // Use fast header-only verification
			if err != nil {
				// DON'T clean up on verification error - keep files for analysis
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Incremental verification failed at file %d/%d (%s): %v", j+1, len(result.CreatedFiles), prevFilePath, err)

				// Calculate estimated real capacity
				realCapacity := fileSize * int64(j) // Only count successfully verified files

				fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
				fmt.Printf("This indicates delayed data corruption or fake capacity.\n")
				fmt.Printf("File: %s\n", prevFilePath)
				fmt.Printf("Error details: %v\n", err)
				fmt.Printf("\n📊 ESTIMATED REAL CAPACITY ANALYSIS:\n")
				fmt.Printf("  Files successfully verified: %d out of %d\n", j, len(result.CreatedFiles))
				fmt.Printf("  Data verified before failure: %.2f GB\n", float64(fileSize*int64(j))/(1024*1024*1024))
				fmt.Printf("  ESTIMATED REAL FREE SPACE: %.2f GB\n", float64(realCapacity)/(1024*1024*1024))
				fmt.Printf("\n⚠️  Test files preserved for analysis (%d files).\n", len(result.CreatedFiles))

				err = fmt.Errorf("test failed during incremental verification - file corruption detected")
				if logger != nil {
					logger.SetError(err)
					logger.SetResult("estimatedRealCapacityGB", float64(realCapacity)/(1024*1024*1024))
					logger.SetResult("filesSuccessfullyVerified", j)
				}
				return result, err
			}
		}

		// Show verification success with strategy info
		if i%10 == 0 || i == maxFiles {
			if i <= 5 || i%10 == 0 || i == maxFiles {
				fmt.Printf("Verified %d files (full check) - ✅ OK", len(result.CreatedFiles))
			} else {
				fmt.Printf("Verified last %d files (fast check) - ✅ OK", verifyCount)
			}
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

	fmt.Printf("\n✅ Write and optimized incremental verification completed successfully!\n")
	fmt.Printf("All %d files verified with smart verification strategy.\n", len(result.CreatedFiles))

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

// copyFileOptimized uses large buffer for faster copying
func copyFileOptimized(src, dst string) (int64, error) {
	const bufferSize = 64 * 1024 * 1024 // 64MB buffer for optimal performance

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

	buf := make([]byte, bufferSize)
	return io.CopyBuffer(dstFile, srcFile, buf)
}

// writeTestFileContentOptimized writes directly to target with large blocks and delayed sync
func writeTestFileContentOptimized(filePath string, fileSize int64) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer func() {
		file.Sync() // Sync only once at the end
		file.Close()
	}()

	// Pre-allocate file space to avoid fragmentation
	err = file.Truncate(fileSize)
	if err != nil {
		return err
	}

	// Write header line first
	headerLine := "FILL_TEST_HEADER_LINE\n"
	written, err := file.WriteString(headerLine)
	if err != nil {
		return err
	}
	remaining := fileSize - int64(written)

	// Generate test pattern once
	pattern := "FILL_TEST_DATA_"
	patternBytes := []byte(pattern)
	patternLen := int64(len(patternBytes))

	// Use larger blocks for better performance
	const blockSize = 32 * 1024 * 1024 // 32MB blocks
	block := make([]byte, blockSize)

	// Pre-fill the entire block with pattern
	for i := 0; i < blockSize; {
		copyLen := int(patternLen)
		if i+copyLen > blockSize {
			copyLen = blockSize - i
		}
		copy(block[i:i+copyLen], patternBytes[:copyLen])
		i += copyLen
	}

	// Write in large blocks
	for remaining > 0 {
		writeSize := blockSize
		if remaining < blockSize {
			writeSize = int(remaining)
		}

		n, err := file.Write(block[:writeSize])
		if err != nil {
			return err
		}
		remaining -= int64(n)
	}

	return nil
}

// verifyFileHeaderFast reads only the first 64 bytes to check header efficiently
func verifyFileHeaderFast(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	header := make([]byte, 64)
	n, err := file.Read(header)
	if err != nil {
		return fmt.Errorf("could not read file header: %v", err)
	}

	headerStr := string(header[:n])
	expectedLine := "FILL_TEST_HEADER_LINE\n"

	if !strings.HasPrefix(headerStr, expectedLine) {
		actualLine := strings.Split(headerStr, "\n")[0]
		return fmt.Errorf("file corruption detected - expected '%s' but found '%s'",
			strings.TrimSuffix(expectedLine, "\n"), actualLine)
	}

	return nil
}

// writeTestFileContent writes test content to a file in chunks to avoid memory issues
