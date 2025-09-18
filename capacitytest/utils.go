package capacitytest

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// VerifyTestFileStartEnd verifies file has correct header at start and end
func VerifyTestFileStartEnd(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

// VerifyTestFileComplete performs comprehensive verification of test file
func VerifyTestFileComplete(filePath string) error {
	return VerifyTestFileCompleteContext(context.Background(), filePath)
}

// VerifyTestFileCompleteContext performs comprehensive verification with context
func VerifyTestFileCompleteContext(ctx context.Context, filePath string) error {
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

	// Check context before starting verification
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
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

	// Verify header format
	if !strings.HasPrefix(firstLine, "FILEDO_TEST_") {
		return fmt.Errorf("invalid header format: %s", firstLine)
	}

	// Check context after header verification
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
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

	// Check context after reading footer
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Extract last line
	lastLine := string(lastLineBuffer[:n])
	if newlineIndex := strings.Index(lastLine, "\n"); newlineIndex > 0 {
		lastLine = lastLine[:newlineIndex]
	}

	// Verify footer matches header
	if firstLine != lastLine {
		return fmt.Errorf("header/footer mismatch: '%s' vs '%s'", firstLine, lastLine)
	}

	// Continue with pattern verification if file is large enough
	const minSizeForPatternCheck = 1024
	if fileSize < minSizeForPatternCheck {
		return nil
	}

	// Check context before pattern verification
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Pattern verification with context checking
	return verifyPatternWithContext(ctx, file, fileSize, firstLine)
}

// VerifyTestFileQuick performs quick verification (header + footer + one random middle position)
func VerifyTestFileQuick(filePath string) error {
	return VerifyTestFileQuickContext(context.Background(), filePath)
}

// VerifyTestFileQuickContext performs quick verification with context
func VerifyTestFileQuickContext(ctx context.Context, filePath string) error {
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

	// Check context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
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

	// Check context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
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

	// Check context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
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
		if err := verifyPatternQuickContext(ctx, file, fileSize, firstLine); err != nil {
			return err
		}
	}

	return nil
}

// verifyPatternQuickContext performs quick pattern verification (one random middle position)
func verifyPatternQuickContext(ctx context.Context, file *os.File, fileSize int64, firstLine string) error {
	dataPattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(dataPattern)

	// Calculate data area boundaries
	headerSize := int64(len(firstLine) + 1)
	footerSize := headerSize
	dataStart := headerSize
	dataEnd := fileSize - footerSize

	if dataEnd-dataStart < int64(len(patternBytes)*4) {
		return nil
	}

	// Check context
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Generate one random position in middle of file
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	minPos := dataStart + (dataEnd-dataStart)/4 // Start at 1/4 of file
	maxPos := dataEnd - (dataEnd-dataStart)/4   // End at 3/4 of file

	if maxPos <= minPos {
		// File too small, check middle
		minPos = dataStart + int64(len(patternBytes))
		maxPos = dataEnd - int64(len(patternBytes)*2)
	}

	if maxPos <= minPos {
		return nil // File too small for checking
	}

	randomPos := minPos + rng.Int63n(maxPos-minPos)

	// Seek to random position
	_, err := file.Seek(randomPos, 0)
	if err != nil {
		return fmt.Errorf("could not seek to position %d: %v", randomPos, err)
	}

	readBuffer := make([]byte, len(patternBytes)*4)
	n, err := file.Read(readBuffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("could not read at position %d: %v", randomPos, err)
	}

	if n < len(patternBytes) {
		return nil
	}

	// Check context after reading
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Search for pattern in read chunk
	found := false
	for j := 0; j <= n-len(patternBytes); j++ {
		if string(readBuffer[j:j+len(patternBytes)]) == dataPattern {
			found = true
			break
		}
	}

	if !found {
		// Check if we have valid pattern characters
		validChars := 0
		totalChars := min(n, len(patternBytes)*2)

		for j := 0; j < totalChars; j++ {
			ch := readBuffer[j]
			if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == ' ' {
				validChars++
			}
		}

		validRatio := float64(validChars) / float64(totalChars)
		if validRatio < 0.8 {
			return fmt.Errorf("data corruption detected at position %d - found invalid data pattern (%.1f%% valid chars)", randomPos, validRatio*100)
		}
	}

	return nil
}

// VerifySmartTestFiles performs smart verification according to new strategy:
// - Full verification every 5th file
// - After every 5th file (5,10,15,20..) check 1st file
// - After every 10th file (10,20,30..) check 5th file
// - After every 20th file (20,40,60..) check 10th file
func VerifySmartTestFiles(filePaths []string, currentIndex int) error {
	return VerifySmartTestFilesContext(context.Background(), filePaths, currentIndex)
}

// VerifySmartTestFilesContext performs smart verification with context
func VerifySmartTestFilesContext(ctx context.Context, filePaths []string, currentIndex int) error {
	if len(filePaths) == 0 {
		return nil
	}

	// Determine which files to verify
	filesToVerify := make(map[int]bool) // map[index]fullVerification

	// Always verify current file
	if currentIndex%5 == 0 {
		// Every 5th file - full verification
		filesToVerify[currentIndex-1] = true // currentIndex starts at 1, array at 0
	} else {
		// Other files - quick verification
		filesToVerify[currentIndex-1] = false
	}

	// Additional control checks
	if currentIndex%5 == 0 {
		// After every 5th file check 1st file (quick)
		if len(filePaths) >= 1 {
			filesToVerify[0] = false
		}
	}

	if currentIndex%10 == 0 {
		// After every 10th file check 5th file (quick)
		if len(filePaths) >= 5 {
			filesToVerify[4] = false // 5th file has index 4
		}
	}

	if currentIndex%20 == 0 {
		// After every 20th file check 10th file (quick)
		if len(filePaths) >= 10 {
			filesToVerify[9] = false // 10th file has index 9
		}
	}

	// Perform verification of selected files
	for fileIndex, fullVerification := range filesToVerify {
		if fileIndex >= len(filePaths) {
			continue
		}

		// Check context before each file
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		filePath := filePaths[fileIndex]
		var err error

		if fullVerification {
			// Full verification
			err = VerifyTestFileCompleteContext(ctx, filePath)
		} else {
			// Quick verification
			err = VerifyTestFileQuickContext(ctx, filePath)
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

// VerifyAllTestFiles verifies all files in the list with progress indication
func VerifyAllTestFiles(filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	for i, filePath := range filePaths {
		if err := VerifyTestFileStartEnd(filePath); err != nil {
			fmt.Printf("❌ FAILED at file %d/%d\n", i+1, len(filePaths))
			return fmt.Errorf("file %d/%d (%s) verification failed: %v", i+1, len(filePaths), filePath, err)
		}
	}

	return nil
}

// VerifyAllTestFilesContext verifies all files in the list with context support
func VerifyAllTestFilesContext(ctx context.Context, filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	for i, filePath := range filePaths {
		// Check context before each file verification
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := VerifyTestFileCompleteContext(ctx, filePath); err != nil {
			fmt.Printf("❌ FAILED at file %d/%d\n", i+1, len(filePaths))
			return fmt.Errorf("file %d/%d (%s) verification failed: %v", i+1, len(filePaths), filePath, err)
		}
	}

	return nil
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CalibrateOptimalBufferSize calibrates the optimal buffer size for writing
func CalibrateOptimalBufferSize(testPath string) int {
	// Test different buffer sizes (4MB to 128MB)
	bufferSizes := []int{
		4 * 1024 * 1024,   // 4MB
		8 * 1024 * 1024,   // 8MB
		16 * 1024 * 1024,  // 16MB
		32 * 1024 * 1024,  // 32MB
		64 * 1024 * 1024,  // 64MB
		128 * 1024 * 1024, // 128MB
	}

	testFileSize := 50 * 1024 * 1024 // 50MB test file
	bestBuffer := bufferSizes[2]     // Default to 16MB
	bestSpeed := 0.0

	for _, bufferSize := range bufferSizes {
		// Create test file
		testFileName := fmt.Sprintf("__buffer_test_%d.tmp", time.Now().UnixNano())
		testFilePath := filepath.Join(testPath, testFileName)

		start := time.Now()
		err := WriteTestFileWithBuffer(testFilePath, int64(testFileSize), bufferSize)
		duration := time.Since(start)

		if err != nil {
			os.Remove(testFilePath)
			continue
		}

		speed := float64(testFileSize) / (1024 * 1024) / duration.Seconds()

		if speed > bestSpeed {
			bestSpeed = speed
			bestBuffer = bufferSize
		}

		os.Remove(testFilePath)
	}

	return bestBuffer
}

// WriteTestFileWithBuffer writes test file with specified buffer size
func WriteTestFileWithBuffer(filePath string, fileSize int64, bufferSize int) error {
	return WriteTestFileWithBufferContext(context.Background(), filePath, fileSize, bufferSize)
}

// WriteTestFileWithBufferContext writes test file with context for cancellation
func WriteTestFileWithBufferContext(ctx context.Context, filePath string, fileSize int64, bufferSize int) error {
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

	// Fill with readable pattern
	pattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(pattern)
	block := make([]byte, bufferSize)

	// Fill buffer with pattern - optimize by pre-filling once
	for i := 0; i < bufferSize; {
		copyLen := min(len(patternBytes), bufferSize-i)
		copy(block[i:i+copyLen], patternBytes[:copyLen])
		i += copyLen
	}

	// Write data blocks in larger chunks with frequent context checks
	blockCount := 0
	const checkInterval = 100 // Check context every 100 blocks

	for remaining > int64(len(headerLine)) {
		// Check context cancellation more frequently for large files
		if blockCount%checkInterval == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
		blockCount++

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

	// Final context check before footer
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Write footer (same as header)
	_, err = file.WriteString(headerLine)
	if err != nil {
		return err
	}

	// Explicitly sync only once at the end for better performance
	return file.Sync()
}

// GetEnhancedTargetInfo returns enhanced target info string
func GetEnhancedTargetInfo(tester interface{}) string {
	if t, ok := tester.(interface{ GetTestInfo() (string, string) }); ok {
		testType, targetPath := t.GetTestInfo()

		// Try to get additional info based on tester type
		switch testType {
		case "Device":
			return targetPath
		case "Folder":
			return targetPath
		case "Network":
			return targetPath
		}

		// Fallback to simple path
		return targetPath
	}

	// Fallback for unknown types
	return "Unknown"
}

func verifyPatternWithContext(ctx context.Context, file *os.File, fileSize int64, firstLine string) error {
	dataPattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(dataPattern)

	// Calculate data area bounds
	headerSize := int64(len(firstLine) + 1)
	footerSize := headerSize
	dataStart := headerSize
	dataEnd := fileSize - footerSize

	if dataEnd-dataStart < int64(len(patternBytes)*4) {
		return nil
	}

	// Generate check positions
	var checkPositions []int64
	checkPositions = append(checkPositions, dataStart)
	checkPositions = append(checkPositions, dataEnd-int64(len(patternBytes)*2))

	// Add random positions with modern Go random
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < 3; i++ {
		minPos := dataStart + int64(len(patternBytes))
		maxPos := dataEnd - int64(len(patternBytes)*2)
		if maxPos > minPos {
			randomPos := minPos + rng.Int63n(maxPos-minPos)
			checkPositions = append(checkPositions, randomPos)
		}
	}

	readBuffer := make([]byte, len(patternBytes)*4)

	for i, pos := range checkPositions {
		// Check context before each position verification
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if pos < dataStart || pos >= dataEnd-int64(len(patternBytes)) {
			continue
		}

		_, err := file.Seek(pos, 0)
		if err != nil {
			return fmt.Errorf("could not seek to position %d: %v", pos, err)
		}

		n, err := file.Read(readBuffer)
		if err != nil && err != io.EOF {
			return fmt.Errorf("could not read at position %d: %v", pos, err)
		}

		if n < len(patternBytes) {
			continue
		}

		// Look for pattern in the read chunk
		found := false
		for j := 0; j <= n-len(patternBytes); j++ {
			if string(readBuffer[j:j+len(patternBytes)]) == dataPattern {
				found = true
				break
			}
		}

		if !found {
			// Check if we have valid pattern characters
			validChars := 0
			totalChars := min(n, len(patternBytes)*2)

			for j := 0; j < totalChars; j++ {
				ch := readBuffer[j]
				if (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == ' ' {
					validChars++
				}
			}

			validRatio := float64(validChars) / float64(totalChars)
			if validRatio < 0.8 {
				return fmt.Errorf("data corruption detected at position %d - found invalid data pattern (%.1f%% valid chars)", pos, validRatio*100)
			}
		}

		// Progress indication for large files
		if len(checkPositions) > 2 && i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
	}

	return nil
}
