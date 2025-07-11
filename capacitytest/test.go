package capacitytest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RunGenericTest performs a fake capacity test using the provided tester
func RunGenericTest(tester Tester, autoDelete bool, logger HistoryLogger, interruptHandler InterruptHandler, progressTracker func(maxItems int, maxBytes int64, interval time.Duration) ProgressTracker) (*TestResult, error) {
	testType, targetPath := tester.GetTestInfo()

	// Setup history logging if provided
	if logger != nil {
		logger.SetCommand(strings.ToLower(testType), targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
	}

	result := &TestResult{
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
	fmt.Printf("Target: %s\n", GetEnhancedTargetInfo(tester))
	fmt.Printf("Available space: %.2f GB\n", float64(freeSpace)/(1024*1024*1024))
	fmt.Printf("Test file size: %d MB (%.1f%% of available space for %d files)\n",
		fileSizeMB, float64(totalDataTarget)/float64(freeSpace)*100, maxFiles)
	fmt.Printf("Will create %d test files...\n\n", maxFiles)

	// Pre-calibrate optimal buffer for this target
	dir := targetPath
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	if _, exists := optimalBuffers[dir]; !exists {
		// Check for interrupt during calibration
		if interruptHandler != nil && interruptHandler.IsCancelled() {
			fmt.Printf("\n\n⚠ Operation interrupted by user during optimization.\n")
			err := fmt.Errorf("operation interrupted by user")
			if logger != nil {
				logger.SetError(err)
				logger.SetResult("interrupted", true)
			}
			return result, err
		}

		optimalBuffer := CalibrateOptimalBufferSize(dir)
		optimalBuffers[dir] = optimalBuffer
		fmt.Printf("Buffer optimized: %dMB\n", optimalBuffer/(1024*1024))
	}

	const baselineFileCount = 3
	var speeds []float64
	var baselineSpeed float64
	baselineSet := false

	// Create progress tracker
	var progress ProgressTracker
	if progressTracker != nil {
		progress = progressTracker(maxFiles, maxFiles*fileSize, 2*time.Second)
	}

	// Write phase
	fmt.Printf("Starting capacity test - writing %d files...\n", maxFiles)

	for i := 1; i <= maxFiles; i++ {
		// Check for interrupt using enhanced context checking
		if interruptHandler != nil {
			if err := interruptHandler.CheckContext(); err != nil {
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
		}

		fileName := fmt.Sprintf("FILL_%03d_%s.tmp", i, time.Now().Format("02150405"))

		start := time.Now()
		var filePath string
		if interruptHandler != nil {
			filePath, err = tester.CreateTestFileContext(interruptHandler.Context(), fileName, fileSize)
		} else {
			filePath, err = tester.CreateTestFile(fileName, fileSize)
		}
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

		// Verify ALL previously created files (including the new one) with context
		if interruptHandler != nil {
			if err := VerifyAllTestFilesContext(interruptHandler.Context(), result.CreatedFiles); err != nil {
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
					if verifyErr := VerifyTestFileCompleteContext(interruptHandler.Context(), fp); verifyErr != nil {
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
		} else {
			// Use regular verification without context
			if err := VerifyAllTestFiles(result.CreatedFiles); err != nil {
				// DON'T clean up on verification error - keep files for analysis
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Verification failed after creating file %d: %v", i, err)

				// Calculate estimated real capacity
				realCapacity := fileSize * int64(i-1) // Count files before the failed one

				fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
				fmt.Printf("This indicates delayed data corruption or fake capacity.\n")
				fmt.Printf("Error details: %v\n", err)

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
		}

		// Calculate write speed
		speed := float64(fileSize) / duration.Seconds() / (1024 * 1024) // MB/s
		speeds = append(speeds, speed)

		// Update progress
		if progress != nil {
			progress.Update(int64(result.FilesCreated), result.TotalDataBytes)
			progress.PrintProgress("Test")
		}

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

	// Final statistics
	fmt.Printf("\n📊 TEST STATISTICS:\n")
	fmt.Printf("  Total files created: %d\n", result.FilesCreated)
	fmt.Printf("  Total data written: %.2f GB\n", float64(result.TotalDataBytes)/(1024*1024*1024))
	fmt.Printf("  Baseline speed: %.2f MB/s\n", result.BaselineSpeedMBps)
	fmt.Printf("  Average speed: %.2f MB/s\n", result.AverageSpeedMBps)
	fmt.Printf("  Min speed: %.2f MB/s\n", result.MinSpeedMBps)
	fmt.Printf("  Max speed: %.2f MB/s\n", result.MaxSpeedMBps)
	fmt.Printf("  Speed variation: %.1f%%\n", (result.MaxSpeedMBps-result.MinSpeedMBps)/result.AverageSpeedMBps*100)

	if logger != nil {
		logger.SetResult("testPassed", true)
		logger.SetResult("averageSpeedMBps", result.AverageSpeedMBps)
		logger.SetResult("minSpeedMBps", result.MinSpeedMBps)
		logger.SetResult("maxSpeedMBps", result.MaxSpeedMBps)
		logger.SetResult("baselineSpeedMBps", result.BaselineSpeedMBps)
		logger.SetResult("totalDataMB", (result.TotalDataBytes)/(1024*1024))
		logger.SetResult("filesDeleted", autoDelete)
	}

	// Auto-cleanup if requested
	if autoDelete {
		fmt.Printf("\n🗑️  Auto-cleanup enabled - removing test files...\n")
		deletedCount := 0
		totalDeletedSize := int64(0)

		for _, filePath := range result.CreatedFiles {
			if fileInfo, err := os.Stat(filePath); err == nil {
				fileSize := fileInfo.Size()
				if err := tester.CleanupTestFile(filePath); err == nil {
					deletedCount++
					totalDeletedSize += fileSize
				}
			}
		}

		fmt.Printf("✅ Cleaned up %d files (%.2f GB)\n", deletedCount, float64(totalDeletedSize)/(1024*1024*1024))
		result.CreatedFiles = result.CreatedFiles[:0] // Clear the list
	} else {
		fmt.Printf("\n⚠️  Test files preserved on device (%d files, %.2f GB)\n", len(result.CreatedFiles), float64(result.TotalDataBytes)/(1024*1024*1024))
		fmt.Printf("Use '%s' to clean them up manually.\n", tester.GetCleanupCommand())
	}

	return result, nil
}
