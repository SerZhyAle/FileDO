package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// formatDuration formats a duration for short output with consistent spacing
func formatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%6.1fns", float64(d.Nanoseconds()))
	} else if d < time.Millisecond {
		return fmt.Sprintf("%6.1fμs", float64(d.Nanoseconds())/1000.0)
	} else if d < time.Second {
		return fmt.Sprintf("%6.1fms", float64(d.Nanoseconds())/1000000.0)
	} else {
		return fmt.Sprintf("%6.1fs", d.Seconds())
	}
}

// formatETA formats duration for ETA display in human-readable format
func formatETA(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}

	totalSeconds := int64(d.Seconds())
	
	if totalSeconds < 60 {
		return fmt.Sprintf("%ds", totalSeconds)
	} else if totalSeconds < 3600 {
		minutes := totalSeconds / 60
		seconds := totalSeconds % 60
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	} else {
		hours := totalSeconds / 3600
		minutes := (totalSeconds % 3600) / 60
		seconds := totalSeconds % 60
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	}
}

// parseSize parses a size string and returns the size in MB
func parseSize(sizeStr string) (int, error) {
	var size int
	var err error

	sizeStr = strings.TrimSpace(strings.ToLower(sizeStr))

	// Handle suffixes
	if strings.HasSuffix(sizeStr, "mb") || strings.HasSuffix(sizeStr, "m") {
		sizeStr = strings.TrimSuffix(sizeStr, "mb")
		sizeStr = strings.TrimSuffix(sizeStr, "m")
	}

	size, err = strconv.Atoi(sizeStr)
	if err != nil {
		return 0, err
	}

	return size, nil
}

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

// copyFileWithProgress copies a file from src to dst with progress reporting
func copyFileWithProgress(src, dst string, showProgress bool) (int64, error) {
	sourceFile, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return 0, err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer destFile.Close()

	totalSize := sourceInfo.Size()
	buffer := make([]byte, 64*1024) // 64KB buffer
	var totalCopied int64
	var lastProgressUpdate int64

	if showProgress {
		fmt.Printf("  Progress: 0.0%%")
	}

	for {
		n, err := sourceFile.Read(buffer)
		if n > 0 {
			written, writeErr := destFile.Write(buffer[:n])
			if writeErr != nil {
				return totalCopied, writeErr
			}
			totalCopied += int64(written)

			// Show progress less frequently - only every 5% or 10MB
			if showProgress {
				progressThreshold := int64(1024 * 1024 * 10) // 10MB
				if totalSize < progressThreshold {
					progressThreshold = totalSize / 20 // 5% for smaller files
				}

				if totalCopied-lastProgressUpdate >= progressThreshold {
					progress := float64(totalCopied) / float64(totalSize) * 100
					fmt.Printf("\r  Progress: %.1f%%", progress)
					lastProgressUpdate = totalCopied
				}
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			return totalCopied, err
		}
	}

	if showProgress {
		fmt.Printf("\r  Progress: 100.0%%")
	}
	return totalCopied, nil
}
