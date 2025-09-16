//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// optimalBuffers кэш для оптимальных буферов
var optimalBuffers = make(map[string]int)

// runDeviceFill заполняет устройство тестовыми файлами
func runDeviceFill(devicePath, sizeMBStr string, autoDelete bool) error {
	// Настройка обработчика прерываний
	handler := NewInterruptHandler()

	// Парсинг размера
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 100 // По умолчанию 100 MB если парсинг не удался
	}

	if sizeMB < 1 || sizeMB > 10240 { // Лимит 10GB на файл
		sizeMB = 100 // По умолчанию 100 MB если вне диапазона
	}

	// Нормализация пути устройства
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Fill Operation\n")
	fmt.Printf("Target: %s\n", getSimpleDeviceInfo(normalizedPath))
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Проверка доступности и возможности записи устройства
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Тест доступа на запись
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(normalizedPath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("device path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Очистка тестового файла

	// Получение доступного места
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(normalizedPath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return fmt.Errorf("failed to get disk space information: %w", err)
	}

	fileSizeBytes := int64(sizeMB) * 1024 * 1024
	maxFiles := int64(freeBytesAvailable) / fileSizeBytes

	// Резерв места (100MB или 5% от общего, что меньше)
	reserveBytes := int64(100 * 1024 * 1024) // 100MB
	if fivePercent := int64(totalBytes) / 20; fivePercent < reserveBytes {
		reserveBytes = fivePercent
	}

	// Корректировка максимального количества файлов с учетом резерва
	if reserveBytes > 0 {
		maxFiles = (int64(freeBytesAvailable) - reserveBytes) / fileSizeBytes
	}

	if maxFiles <= 0 {
		return fmt.Errorf("insufficient space to create even one file of %d MB", sizeMB)
	}

	fmt.Printf("Available space: %.2f GB\n", float64(freeBytesAvailable)/(1024*1024*1024))
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Maximum files to create: %d\n", maxFiles)
	fmt.Printf("Total space to fill: %.2f GB\n\n", float64(maxFiles*fileSizeBytes)/(1024*1024*1024))

	// Получение временной метки для именования файлов (формат ddHHmmss)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Предварительная калибровка оптимального буфера для более быстрых операций
	dir := normalizedPath
	optimalBuffer := calibrateOptimalBufferSize(dir)
	optimalBuffers[dir] = optimalBuffer

	// Использование большего bufferSize для операций заполнения (4x калиброванный буфер)
	optimalBuffer = min(optimalBuffer*4, 128*1024*1024) // Максимум 128MB буфер
	optimalBuffers[dir] = optimalBuffer

	// Начало заполнения
	fmt.Printf("Starting fill operation..\n")
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	// Предварительное создание путей файлов для сокращения строковых операций в цикле
	filePathList := make([]string, maxFiles)
	for i := int64(1); i <= maxFiles; i++ {
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		filePathList[i-1] = filepath.Join(normalizedPath, fileName)
	}

	// Использование пула воркеров для параллельного создания файлов с контекстом для отмены
	parallelism := 12 // Создание 12 файлов одновременно
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Создание канала для работы
	jobs := make(chan int64, maxFiles)
	results := make(chan struct {
		fileIndex int64
		err       error
	}, maxFiles)

	// Отслеживание критических ошибок (заполнен диск, аппаратные сбои и т.д.)
	var criticalErrorCount int64

	// Запуск воркеров
	for w := 0; w < parallelism; w++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					// Контекст отменен - немедленный выход
					return
				case i, ok := <-jobs:
					if !ok {
						return
					}

					if handler.IsCancelled() {
						results <- struct {
							fileIndex int64
							err       error
						}{i, fmt.Errorf("cancelled")}
						continue
					}

					// Использование предварительно вычисленного пути файла
					targetFilePath := filePathList[i-1]

					// Создание файла напрямую с оптимизированной функцией
					err := writeTestFileWithBuffer(targetFilePath, fileSizeBytes, optimalBuffer)
					
					// Проверка критических ошибок, которые должны остановить все операции
					if err != nil && isCriticalError(err) {
						atomic.AddInt64(&criticalErrorCount, 1)
						// Отмена контекста для остановки всех других воркеров
						cancel()
						results <- struct {
							fileIndex int64
							err       error
						}{i, fmt.Errorf("critical error: %w", err)}
						return
					}
					
					results <- struct {
						fileIndex int64
						err       error
					}{i, err}
				}
			}
		}()
	}

	// Отправка заданий
	for i := int64(1); i <= maxFiles; i++ {
		jobs <- i
	}
	close(jobs)

	// Сбор результатов с улучшенной обработкой ошибок
	var consecutiveErrors int64
	for i := int64(1); i <= maxFiles; i++ {
		// Проверка прерывания или отмены контекста критической ошибки
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}
		
		select {
		case <-ctx.Done():
			fmt.Printf("\n⚠ Operation stopped due to critical error\n")
			goto fillComplete
		case result := <-results:
			if result.err != nil {
				if result.err.Error() != "cancelled" {
					if strings.Contains(result.err.Error(), "critical error") {
						fmt.Printf("\n❌ Critical error on file %d: %v\n", result.fileIndex, result.err)
						fmt.Printf("Stopping all operations to prevent further issues\n")
						goto fillComplete
					} else {
						fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", result.fileIndex, result.err)
						atomic.AddInt64(&consecutiveErrors, 1)
						
						// Остановка при слишком многих последовательных ошибках (вероятно диск заполнен или аппаратная проблема)
						if consecutiveErrors >= 3 {
							fmt.Printf("Too many consecutive errors - stopping operation\n")
							cancel() // Отмена оставшихся воркеров
							goto fillComplete
						}
					}
				}
				if result.fileIndex <= 1 {
					goto fillComplete
				}
			} else {
				// Успех - сброс счетчика последовательных ошибок
				atomic.StoreInt64(&consecutiveErrors, 0)
				filesCreated++
				totalBytesWritten += fileSizeBytes

				// Обновление прогресса реже (каждые 4 файла или как минимум раз в 2 секунды)
				if filesCreated%4 == 0 || filesCreated == 1 || progress.ShouldUpdate() {
					progress.Update(filesCreated, totalBytesWritten)
					progress.PrintProgress("Fill")
				}
			}
		case <-time.After(30 * time.Second):
			// Защита от таймаута - если нет результата в течение 30 секунд, что-то не так
			fmt.Printf("\n⚠ Warning: Operation timeout - stopping\n")
			cancel()
			goto fillComplete
		}
	}

fillComplete:

	// Финальное резюме
	progress.Finish("Fill Operation")

	// Автоудаление при запросе
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files..\n")

		// Использование предварительно созданных путей файлов для избежания поиска
		var deletedCount int64 = 0
		var deletedSize int64 = 0

		// Создание пула воркеров для удаления
		deletionWorkers := 24 // Больше воркеров для удаления
		deletionJobs := make(chan string, filesCreated)
		var wg sync.WaitGroup

		// Запуск воркеров удаления
		for w := 0; w < deletionWorkers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for filePath := range deletionJobs {
					info, err := os.Stat(filePath)
					if err == nil {
						fileSize := info.Size()
						err = os.Remove(filePath)
						if err == nil {
							atomic.AddInt64((*int64)(&deletedCount), 1)
							atomic.AddInt64(&deletedSize, fileSize)
						}
					}
				}
			}()
		}

		// Постановка всех файлов в очередь на удаление
		for _, filePath := range filePathList[:filesCreated] {
			deletionJobs <- filePath
		}
		close(deletionJobs)

		// Периодическое обновление прогресса во время ожидания завершения
		updateTicker := time.NewTicker(100 * time.Millisecond)
		done := make(chan struct{})

		go func() {
			wg.Wait()
			close(done)
		}()

		// Показ обновлений прогресса
		for {
			select {
			case <-updateTicker.C:
				fmt.Printf("Deleted %d/%d files - %.2f GB freed\r", deletedCount, filesCreated, float64(deletedSize)/(1024*1024*1024))
			case <-done:
				updateTicker.Stop()
				goto deletionComplete
			}
		}

	deletionComplete:

		fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", deletedCount, float64(deletedSize)/(1024*1024*1024))
	}

	return nil
}

func runDeviceFillClean(devicePath string) error {
	// Нормализация пути устройства
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	fmt.Printf("Device Clean Operation\n")
	fmt.Printf("Target: %s\n", getSimpleDeviceInfo(normalizedPath))
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)..\n\n")

	// Проверка доступности устройства
	if _, err := os.Stat(normalizedPath); err != nil {
		return fmt.Errorf("device path is not accessible: %w", err)
	}

	// Поиск всех файлов FILL_*.tmp
	fillPattern := filepath.Join(normalizedPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	// Поиск всех файлов speedtest_*.txt
	speedtestPattern := filepath.Join(normalizedPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		return fmt.Errorf("failed to search for speedtest files: %w", err)
	}

	// Объединение всех совпадений
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", normalizedPath)
		fmt.Printf("Searched for: FILL_*.tmp and speedtest_*.txt\n")
		return nil
	}

	fmt.Printf("Found %d test files:\n", len(allMatches))
	fmt.Printf("  FILL files: %d\n", len(fillMatches))
	fmt.Printf("  Speedtest files: %d\n", len(speedtestMatches))

	// Вычисление общего размера перед удалением
	var totalSize int64
	for _, filePath := range allMatches {
		if info, err := os.Stat(filePath); err == nil {
			totalSize += info.Size()
		}
	}

	fmt.Printf("Total size to delete: %.2f GB\n", float64(totalSize)/(1024*1024*1024))
	fmt.Printf("Deleting files..\n\n")

	// Удаление файлов с использованием пула воркеров для параллельного удаления
	var deletedCount int64
	var deletedSize int64
	deletionWorkers := 24 // Использование 24 воркеров для параллельного удаления
	deletionJobs := make(chan string, len(allMatches))
	var wg sync.WaitGroup

	// Запуск воркеров удаления
	for w := 0; w < deletionWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for filePath := range deletionJobs {
				info, err := os.Stat(filePath)
				if err == nil {
					fileSize := info.Size()
					err = os.Remove(filePath)
					if err != nil {
						fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
					} else {
						atomic.AddInt64(&deletedCount, 1)
						atomic.AddInt64(&deletedSize, fileSize)
					}
				}
			}
		}()
	}

	// Постановка всех файлов в очередь на удаление
	for _, filePath := range allMatches {
		deletionJobs <- filePath
	}
	close(deletionJobs)

	// Периодическое обновление прогресса во время ожидания завершения
	updateTicker := time.NewTicker(100 * time.Millisecond)
	done := make(chan struct{})

	go func() {
		wg.Wait()
		close(done)
	}()

	// Показ обновлений прогресса
	totalFiles := int64(len(allMatches))
	for {
		select {
		case <-updateTicker.C:
			currentCount := atomic.LoadInt64(&deletedCount)
			currentSize := atomic.LoadInt64(&deletedSize)
			fmt.Printf("Deleted %d/%d files - %.2f GB freed\r",
				currentCount, totalFiles, float64(currentSize)/(1024*1024*1024))
		case <-done:
			updateTicker.Stop()
			fmt.Printf("Deleted %d/%d files - %.2f GB freed\r",
				atomic.LoadInt64(&deletedCount), totalFiles,
				float64(atomic.LoadInt64(&deletedSize))/(1024*1024*1024))
			goto DeletionComplete
		}
	}

DeletionComplete:
	fmt.Printf("\n\nClean Operation Complete!\n")
	fmt.Printf("Files deleted: %d out of %d\n", deletedCount, len(allMatches))
	fmt.Printf("Space freed: %.2f GB\n", float64(deletedSize)/(1024*1024*1024))

	if deletedCount < int64(len(allMatches)) {
		fmt.Printf("Warning: %d files could not be deleted\n", int64(len(allMatches))-deletedCount)
	}

	return nil
}

// getSimpleDeviceInfo возвращает простую информацию об устройстве
func getSimpleDeviceInfo(devicePath string) string {
	// Нормализация пути
	normalizedPath := devicePath
	if len(normalizedPath) == 2 && normalizedPath[1] == ':' {
		normalizedPath += "\\"
	}

	// Получение информации о диске
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	err := windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(normalizedPath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
	if err != nil {
		return fmt.Sprintf("Device %s (info unavailable)", devicePath)
	}

	return fmt.Sprintf("Device %s (%.1f GB total, %.1f GB free)", 
		devicePath, 
		float64(totalBytes)/(1024*1024*1024),
		float64(freeBytesAvailable)/(1024*1024*1024))
}