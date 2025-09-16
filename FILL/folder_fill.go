package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// runFolderFill заполняет папку тестовыми файлами
func runFolderFill(folderPath, sizeMBStr string, autoDelete bool) error {
	// Настройка обработчика прерываний
	handler := NewInterruptHandler()
	templateFilePath := ""

	// Добавление очистки для файла шаблона
	handler.AddCleanup(func() {
		if templateFilePath != "" {
			os.Remove(templateFilePath)
			fmt.Printf("✓ Template file cleaned up\n")
		}
	})

	// Парсинг размера
	sizeMB, err := parseSize(sizeMBStr)
	if err != nil {
		sizeMB = 100 // По умолчанию 100 MB если парсинг не удался
	}

	if sizeMB < 1 || sizeMB > 10240 { // Лимит 10GB на файл
		sizeMB = 100 // По умолчанию 100 MB если вне диапазона
	}

	fmt.Printf("Folder Fill Operation\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Проверка существования и доступности папки
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path is not accessible: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Тест доступа на запись
	testFileName := fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(folderPath, testFileName)
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return fmt.Errorf("folder path is not writable: %w", err)
	}
	testFile.WriteString("test")
	testFile.Close()
	os.Remove(testFilePath) // Очистка тестового файла

	// Получение доступного места на файловой системе, содержащей эту папку
	currentDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// Использование Windows API для получения места на диске для диска, содержащего папку
	var freeBytesAvailable, totalBytes, totalFreeBytes uint64
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Получение корня диска
	volumePathName := make([]uint16, windows.MAX_PATH)
	err = windows.GetVolumePathName(windows.StringToUTF16Ptr(absPath), &volumePathName[0], windows.MAX_PATH)
	if err != nil {
		return fmt.Errorf("failed to get volume path name: %w", err)
	}
	rootPath := windows.UTF16ToString(volumePathName)

	err = windows.GetDiskFreeSpaceEx(windows.StringToUTF16Ptr(rootPath), &freeBytesAvailable, &totalBytes, &totalFreeBytes)
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

	// Создание файла шаблона сначала
	templateFileName := fmt.Sprintf("fill_template_%d_%d.txt", sizeMB, time.Now().Unix())
	templateFilePath = filepath.Join(currentDir, templateFileName)

	fmt.Printf("Creating template file (%d MB)..\n", sizeMB)
	startTemplate := time.Now()
	err = createRandomFile(templateFilePath, sizeMB, false) // Без прогресса для шаблона
	if err != nil {
		return fmt.Errorf("failed to create template file: %w", err)
	}
	templateDuration := time.Since(startTemplate)
	fmt.Printf("✓ Template file created in %s\n\n", formatDuration(templateDuration))

	// Получение временной метки для именования файлов (формат ddHHmmss)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Начало заполнения
	fmt.Printf("Starting fill operation..\n")
	progress := NewProgressTrackerWithInterval(maxFiles, maxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)

	for i := int64(1); i <= maxFiles; i++ {
		// Проверка прерывания
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}

		// Генерация имени файла: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(folderPath, fileName)

		// Копирование файла шаблона в целевой
		bytesCopied, err := copyFileOptimized(templateFilePath, targetFilePath)
		if err != nil {
			fmt.Printf("\n⚠ Warning: Failed to create file %d: %v\n", i, err)
			break
		}

		filesCreated++
		totalBytesWritten += bytesCopied
		progress.Update(filesCreated, totalBytesWritten)
		progress.PrintProgress("Fill")
	}

	// Очистка файла шаблона
	os.Remove(templateFilePath)

	// Финальное резюме
	progress.Finish("Fill Operation")

	// Автоудаление при запросе
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files..\n")

		// Поиск всех файлов FILL_*.tmp в папке
		pattern := filepath.Join(folderPath, "FILL_*.tmp")
		matches, err := filepath.Glob(pattern)
		if err != nil {
			fmt.Printf("⚠ Warning: Failed to search for files to delete: %v\n", err)
		} else if len(matches) > 0 {
			deletedCount := 0
			deletedSize := int64(0)

			for i, filePath := range matches {
				info, err := os.Stat(filePath)
				if err == nil {
					fileSize := info.Size()

					err = os.Remove(filePath)
					if err != nil {
						fmt.Printf("⚠ Warning: Failed to delete %s: %v\n", filepath.Base(filePath), err)
					} else {
						deletedCount++
						deletedSize += fileSize

						// Показ прогресса каждые 100 файлов
						if (i+1)%100 == 0 || i == len(matches)-1 {
							fmt.Printf("Deleted %d/%d files - %.2f GB freed\r", deletedCount, len(matches), float64(deletedSize)/(1024*1024*1024))
						}
					}
				}
			}

			fmt.Printf("\nAuto-delete complete: %d files deleted, %.2f GB freed\n", deletedCount, float64(deletedSize)/(1024*1024*1024))
		}
	}

	return nil
}

func runFolderFillClean(folderPath string) error {
	fmt.Printf("Folder Clean Operation\n")
	fmt.Printf("Target: %s\n", folderPath)
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)..\n\n")

	// Проверка существования и доступности папки
	stat, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder path is not accessible: %w", err)
	}
	if !stat.IsDir() {
		return fmt.Errorf("path is not a directory: %s", folderPath)
	}

	// Поиск всех файлов FILL_*.tmp
	fillPattern := filepath.Join(folderPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		return fmt.Errorf("failed to search for FILL files: %w", err)
	}

	// Поиск всех файлов speedtest_*.txt
	speedtestPattern := filepath.Join(folderPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		return fmt.Errorf("failed to search for speedtest files: %w", err)
	}

	// Объединение всех совпадений
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", folderPath)
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