package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Глобальный кеш оптимальных буферов
var optimalBuffers = make(map[string]int)

// Tester interface для тестирования fake capacity
type Tester interface {
	GetTestInfo() (testType, targetPath string)
	GetAvailableSpace() (int64, error)
	CreateTestFile(fileName string, fileSize int64) (filePath string, err error)
	CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error)
	VerifyTestFile(filePath string) error
	CleanupTestFile(filePath string) error
	GetCleanupCommand() string
}

// TestResult содержит результаты тестирования fake capacity
type TestResult struct {
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

// RunGenericTest выполняет тест fake capacity используя предоставленный tester
func RunGenericTest(tester Tester, autoDelete bool, logger *HistoryLogger, interruptHandler *InterruptHandler, progressTracker func(maxItems int, maxBytes int64, interval time.Duration) *ProgressTracker) (*TestResult, error) {
	testType, targetPath := tester.GetTestInfo()

	// Setup history logging if provided
	if logger != nil {
		logger.SetCommand(strings.ToLower(testType), targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
	}

	result := &TestResult{
		CreatedFiles: make([]string, 0, 100),
	}

	// Получение доступного места
	freeSpace, err := tester.GetAvailableSpace()
	if err != nil {
		if logger != nil {
			logger.SetError(err)
		}
		return result, err
	}

	// Проверка минимального объема места (100MB)
	minSpaceBytes := int64(100 * 1024 * 1024) // 100MB
	if freeSpace < minSpaceBytes {
		err = fmt.Errorf("insufficient free space. At least 100MB required, but only %d MB available", freeSpace/(1024*1024))
		if logger != nil {
			logger.SetError(err)
		}
		return result, err
	}

	// Расчет размера файла для использования 95% доступного места для 100 файлов
	const maxFiles = 100
	totalDataTarget := int64(float64(freeSpace) * 0.95) // Используем 95% доступного места
	fileSize := totalDataTarget / maxFiles
	fileSizeMB := fileSize / (1024 * 1024)

	// Обеспечиваем минимальный размер файла 1MB
	if fileSize < 1024*1024 {
		fileSize = 1024 * 1024 // 1MB минимум
		fileSizeMB = 1
	}

	fmt.Printf("%s Fake Capacity Test\n", testType)
	fmt.Printf("Target: %s\n", GetEnhancedTargetInfo(tester))
	fmt.Printf("Available space: %.2f GB\n", float64(freeSpace)/(1024*1024*1024))
	fmt.Printf("Test file size: %d MB (%.1f%% of available space for %d files)\n",
		fileSizeMB, float64(totalDataTarget)/float64(freeSpace)*100, maxFiles)
	fmt.Printf("Will create %d test files...\n\n", maxFiles)

	// Пред-калибровка оптимального буфера для этой цели
	dir := targetPath
	if !filepath.IsAbs(dir) {
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
	}
	if _, exists := optimalBuffers[dir]; !exists {
		// Проверка прерывания во время калибровки
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

	// Создание progress tracker
	var progress *ProgressTracker
	if progressTracker != nil {
		progress = progressTracker(maxFiles, maxFiles*fileSize, 2*time.Second)
	}

	// Фаза записи
	fmt.Printf("Starting capacity test - writing %d files...\n", maxFiles)

	for i := 1; i <= maxFiles; i++ {
		// Проверка прерывания с расширенной проверкой контекста
		if interruptHandler != nil {
			if err := interruptHandler.CheckContext(); err != nil {
				fmt.Printf("\n\n⚠ Operation interrupted by user. Cleaning up created files...\n")

				// Очистка созданных файлов
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
			// НЕ очищаем при ошибке создания - сохраняем файлы для анализа
			result.FailureReason = fmt.Sprintf("Failed to create file %d: %v", i, err)

			// Расчет предполагаемой реальной емкости
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

		// Верификация ВСЕХ ранее созданных файлов (включая новый) с контекстом
		if interruptHandler != nil {
			if err := VerifyAllTestFilesContext(interruptHandler.Context(), result.CreatedFiles); err != nil {
				// НЕ очищаем при ошибке верификации - сохраняем файлы для анализа
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Verification failed after creating file %d: %v", i, err)

				// Расчет предполагаемой реальной емкости
				realCapacity := fileSize * int64(i-1) // Считаем файлы до неудавшегося

				fmt.Printf("\n❌ TEST FAILED: %s\n", result.FailureReason)
				fmt.Printf("This indicates delayed data corruption or fake capacity.\n")
				fmt.Printf("Error details: %v\n", err)

				// Попробуем найти, какой именно файл не прошел проверку
				for j, fp := range result.CreatedFiles {
					if verifyErr := VerifyTestFileCompleteContext(interruptHandler.Context(), fp); verifyErr != nil {
						fmt.Printf("Failed file: %s (file %d/%d)\n", fp, j+1, len(result.CreatedFiles))

						// Дополнительный анализ файла
						if fileInfo, statErr := os.Stat(fp); statErr == nil {
							fmt.Printf("File size: %d bytes (expected: %d bytes)\n", fileInfo.Size(), fileSize)
							if fileInfo.Size() != fileSize {
								fmt.Printf("❌ FILE SIZE MISMATCH - This confirms fake capacity!\n")
							}
						}

						// Попробуем прочитать первые несколько байт для диагностики
						if diagFile, diagErr := os.Open(fp); diagErr == nil {
							diagBuf := make([]byte, 128)
							if n, readErr := diagFile.Read(diagBuf); readErr == nil && n > 0 {
								fmt.Printf("File content preview (first %d bytes): %q\n", n, string(diagBuf[:n]))

								// Проверим, содержит ли файл нули (обычно в fake capacity)
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
			// Используем обычную верификацию без контекста
			if err := VerifyAllTestFiles(result.CreatedFiles); err != nil {
				// НЕ очищаем при ошибке верификации - сохраняем файлы для анализа
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Verification failed after creating file %d: %v", i, err)

				// Расчет предполагаемой реальной емкости
				realCapacity := fileSize * int64(i-1) // Считаем файлы до неудавшегося

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

		// Расчет скорости записи
		speed := float64(fileSize) / duration.Seconds() / (1024 * 1024) // MB/s
		speeds = append(speeds, speed)

		// Обновление прогресса
		if progress != nil {
			progress.Update(int64(result.FilesCreated), result.TotalDataBytes)
			progress.PrintProgress("Test")
		}

		// Установка базовой скорости из первых 3 файлов
		if i <= baselineFileCount {
			if i == baselineFileCount {
				// Расчет среднего из первых 3 файлов как базовая скорость
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
			// Проверка аномальной скорости после установки базовой скорости
			if speed < baselineSpeed*0.1 { // Менее 10% от базовой скорости
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Speed dropped to %.2f MB/s (less than 10%% of baseline %.2f MB/s) at file %d", speed, baselineSpeed, i)

				// Расчет предполагаемой реальной емкости
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
			if speed > baselineSpeed*10 { // Более 10x базовой скорости
				result.TestPassed = false
				result.FailureReason = fmt.Sprintf("Speed jumped to %.2f MB/s (more than 1000%% of baseline %.2f MB/s) at file %d", speed, baselineSpeed, i)

				// Расчет предполагаемой реальной емкости
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

	// Расчет статистики
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

	// Финальная статистика
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

	// Авто-очистка при необходимости
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
		result.CreatedFiles = result.CreatedFiles[:0] // Очистка списка
	} else {
		fmt.Printf("\n⚠️  Test files preserved on device (%d files, %.2f GB)\n", len(result.CreatedFiles), float64(result.TotalDataBytes)/(1024*1024*1024))
		fmt.Printf("Use '%s' to clean them up manually.\n", tester.GetCleanupCommand())
	}

	return result, nil
}

// GetEnhancedTargetInfo возвращает расширенную информацию о цели
func GetEnhancedTargetInfo(tester Tester) string {
	testType, targetPath := tester.GetTestInfo()

	// Пытаемся получить дополнительную информацию в зависимости от типа tester
	switch testType {
	case "Device":
		return targetPath
	case "Folder":
		return targetPath
	case "Network":
		return targetPath
	}

	// Fallback к простому пути
	return targetPath
}

// getOptimalBufferSize возвращает оптимальный размер буфера для пути
func getOptimalBufferSize(path string) int {
	if buffer, exists := optimalBuffers[path]; exists {
		return buffer
	}
	// Значение по умолчанию
	return 16 * 1024 * 1024 // 16MB
}

// Helper function for min
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}