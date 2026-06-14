package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
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

// parseSize parses a size string and returns the size in MB
func parseSize(sizeStr string) (int, error) {
	var size int
	var err error

	sizeStr = strings.TrimSpace(strings.ToLower(sizeStr))
	
	// Handle "max" keyword
	if sizeStr == "max" {
		return 10240, nil // 10GB
	}

	// Handle suffixes
	if strings.HasSuffix(sizeStr, "mb") || strings.HasSuffix(sizeStr, "m") {
		sizeStr = strings.TrimSuffix(sizeStr, "mb")
		sizeStr = strings.TrimSuffix(sizeStr, "m")
	}

	size, err = strconv.Atoi(sizeStr)
	if err != nil {
		return 0, err
	}
	
	// Validate size range
	if size < 1 || size > 10240 {
		return 0, fmt.Errorf("size must be between 1 and 10240 MB")
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

// copyFileOptimized performs optimized file copy
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

	// Use 64MB buffer for optimal performance
	buf := make([]byte, 64*1024*1024)
	return io.CopyBuffer(dstFile, srcFile, buf)
}

// calibrateOptimalBufferSize calibrates optimal buffer size for path
func calibrateOptimalBufferSize(testPath string) int {
	// For simplicity, return a good default
	return 64 * 1024 * 1024 // 64MB
}

// isCriticalError determines if an error is critical enough to stop operations
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
	return fmt.Sprintf("%.2f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// generateRandomBytes generates random bytes for file content
func generateRandomBytes(size int) ([]byte, error) {
	data := make([]byte, size)
	_, err := rand.Read(data)
	return data, err
}