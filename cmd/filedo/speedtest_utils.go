package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// formatDuration formats a duration for short output with consistent spacing
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	} else {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
}

// formatDurationDetailed formats duration in days/hours:minutes:seconds format
func formatDurationDetailed(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	
	totalSeconds := int64(d.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd/%02d:%02d:%02d", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%d:%02d", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}

// formatETA formats duration for ETA display in human-readable format
func formatETA(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	secs := d.Seconds()
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	} else if secs < 3600 {
		m := int64(secs) / 60
		s := secs - float64(m*60)
		return fmt.Sprintf("%dm %.0fs", m, s)
	} else {
		h := int64(secs) / 3600
		rem := secs - float64(h*3600)
		m := int64(rem) / 60
		s := rem - float64(m*60)
		return fmt.Sprintf("%dh %dm %.0fs", h, m, s)
	}
}

// parseSize reads a size and returns it in whole megabytes (MiB). A bare
// number is megabytes; the suffixes k/kb, m/mb, g/gb and t/tb are binary
// units, and a decimal point is allowed (`1.5g` is 1536 MB). A size that does
// not parse, is not positive, or is not a whole number of megabytes is an
// error - never a silent fallback (SP-0024 CLI-25; `speed 1GB` used to run a
// 1 MB test).
func parseSize(sizeStr string) (int, error) {
	s := strings.TrimSpace(strings.ToLower(sizeStr))
	units := []struct {
		suffix string
		kib    float64 // the unit in KiB
	}{
		{"kb", 1}, {"k", 1},
		{"mb", 1024}, {"m", 1024},
		{"gb", 1024 * 1024}, {"g", 1024 * 1024},
		{"tb", 1024 * 1024 * 1024}, {"t", 1024 * 1024 * 1024},
	}
	unit := 1024.0 // megabytes by default
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			s, unit = strings.TrimSpace(strings.TrimSuffix(s, u.suffix)), u.kib
			break
		}
	}
	if s == "" {
		return 0, fmt.Errorf("no number in %q", sizeStr)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%q is not a size", sizeStr)
	}
	if v <= 0 {
		return 0, fmt.Errorf("size %q must be positive", sizeStr)
	}
	kib := v * unit
	mb := kib / 1024
	if mb != math.Trunc(mb) {
		return 0, fmt.Errorf("size %q is not a whole number of megabytes", sizeStr)
	}
	if mb > math.MaxInt32 {
		return 0, fmt.Errorf("size %q is too large", sizeStr)
	}
	return int(mb), nil
}

// parseSizeMB is parseSize with the range a verb accepts, and the wording of
// a usage error.
func parseSizeMB(sizeStr string, minMB, maxMB int) (int, error) {
	n, err := parseSize(sizeStr)
	if err != nil {
		return 0, fmt.Errorf("invalid size: %v (a number of megabytes, or a size with k, m, g or t, from %d MB to %d MB)", err, minMB, maxMB)
	}
	if n < minMB || n > maxMB {
		return 0, fmt.Errorf("size %q is out of range: %d MB, allowed %d MB to %d MB", sizeStr, n, minMB, maxMB)
	}
	return n, nil
}

// speedStopRequested is the stop the speed test's loops ask (AUD-02-F5); a
// variable so a test can trip it.
var speedStopRequested = runStopRequested

// createRandomFile creates a test file with the specified size in MB
func createRandomFile(fileName string, sizeMB int, showProgress bool) error {
	file, err := os.Create(fileName)
	if err != nil {
		return err
	}
	defer file.Close()

	sizeBytes := int64(sizeMB) * 1024 * 1024

	// Create a 1MB pattern block once
	const blockSizeMB = 1
	const blockSizeBytes = blockSizeMB * 1024 * 1024

	// Generate the base pattern for 1MB block (without the number prefix)
	basePattern := generateBasePattern(blockSizeBytes - 50) // Reserve 50 bytes for block number prefix

	written := int64(0)
	blockNumber := 1

	for written < sizeBytes {
		if speedStopRequested() {
			return errRunStopped
		}
		remaining := sizeBytes - written
		blockSize := int64(blockSizeBytes)
		if remaining < blockSize {
			blockSize = remaining
		}

		// Create block with number prefix
		blockData := createNumberedBlock(blockNumber, basePattern, int(blockSize))

		n, err := file.Write(blockData)
		if err != nil {
			return err
		}
		written += int64(n)
		blockNumber++

		// Show progress for large files - less frequent updates
		if showProgress && sizeMB >= 10 && written%(1024*1024*50) == 0 { // Every 50MB instead of 10MB
			progress := float64(written) / float64(sizeBytes) * 100
			fmt.Printf("  Creating file: %.1f%%\r", progress)
		}
	}

	if showProgress && sizeMB >= 10 {
		fmt.Printf("  Creating file: 100.0%%\n")
	}

	return nil
}

// generateBasePattern creates a readable text pattern of the specified size
func generateBasePattern(size int) []byte {
	// Create readable text pattern that will be reused
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .,!?\n"
	const lineLength = 80 // Create lines of 80 characters

	pattern := make([]byte, size)
	charIndex := 0

	for i := 0; i < size; i++ {
		if i > 0 && i%lineLength == 0 {
			pattern[i] = '\n'
		} else {
			pattern[i] = charset[charIndex%len(charset)]
			charIndex++
		}
	}

	return pattern
}

// createNumberedBlock creates a block with a number header and footer
func createNumberedBlock(blockNum int, basePattern []byte, targetSize int) []byte {
	// Create block header with block number
	header := fmt.Sprintf("=== BLOCK %06d === START ===\n", blockNum)
	footer := fmt.Sprintf("\n=== BLOCK %06d === END ===\n", blockNum)

	headerBytes := []byte(header)
	footerBytes := []byte(footer)

	// Calculate how much space we need for the pattern
	patternSize := targetSize - len(headerBytes) - len(footerBytes)
	if patternSize <= 0 {
		// If block is too small, just return the header truncated to fit
		if targetSize <= len(headerBytes) {
			return headerBytes[:targetSize]
		}
		return append(headerBytes, footerBytes[:targetSize-len(headerBytes)]...)
	}

	// Create the block
	block := make([]byte, 0, targetSize)
	block = append(block, headerBytes...)

	// Fill with pattern, repeating as necessary
	patternPos := 0
	for len(block) < targetSize-len(footerBytes) {
		if patternPos >= len(basePattern) {
			patternPos = 0
		}
		block = append(block, basePattern[patternPos])
		patternPos++
	}

	// Add footer
	block = append(block, footerBytes...)

	// Ensure exact size
	if len(block) > targetSize {
		block = block[:targetSize]
	}

	return block
}
