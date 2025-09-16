package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// runNetworkFill заполняет сетевой путь тестовыми файлами
func runNetworkFill(networkPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	// Настройка логирования истории
	if logger != nil {
		logger.SetCommand("network", networkPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
	}

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

	fmt.Printf("Network Fill Operation\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("File size: %d MB\n", sizeMB)
	fmt.Printf("Press Ctrl+C to cancel operation\n\n")

	// Тест доступности и записи сетевого пути
	canRead := testNetworkRead(networkPath)
	canWrite := testNetworkWrite(networkPath)

	if !canRead {
		err := fmt.Errorf("network path is not readable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	if !canWrite {
		err := fmt.Errorf("network path is not writable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	fmt.Printf("✓ Network path is accessible and writable\n")

	// Для сетевых путей используем консервативный подход и пытаемся оценить доступное место
	// Поскольку мы не можем надежно получить информацию о дисковом пространстве для сетевых путей, используем другую стратегию
	// Будем создавать файлы до получения ошибки (диск заполнен)

	// Создание файла шаблона сначала
	currentDir, err := os.Getwd()
	if err != nil {
		err = fmt.Errorf("failed to get current directory: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	templateFileName := fmt.Sprintf("fill_template_%d_%d.txt", sizeMB, time.Now().Unix())
	templateFilePath = filepath.Join(currentDir, templateFileName)

	fmt.Printf("Creating template file (%d MB)...\n", sizeMB)
	startTemplate := time.Now()
	err = createRandomFile(templateFilePath, sizeMB, false) // Без прогресса для шаблона
	if err != nil {
		err = fmt.Errorf("failed to create template file: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	templateDuration := time.Since(startTemplate)
	fmt.Printf("✓ Template file created in %s\n\n", formatDuration(templateDuration))

	// Получение временной метки для именования файлов (формат ddHHmmss)
	now := time.Now()
	timestamp := now.Format("021504") // ddHHmmss

	// Начало заполнения
	fmt.Printf("Starting fill operation...\n")
	fmt.Printf("(Note: For network paths, will fill until disk full)\n\n")

	// Для сети оценим большое количество файлов, так как не знаем целевую емкость
	fileSizeBytes := int64(sizeMB) * 1024 * 1024
	estimatedMaxFiles := int64(10000) // Консервативная оценка
	progress := NewProgressTrackerWithInterval(estimatedMaxFiles, estimatedMaxFiles*fileSizeBytes, 2*time.Second)
	filesCreated := int64(0)
	totalBytesWritten := int64(0)
	
	for i := int64(1); i <= 99999; i++ { // Разумный верхний предел
		// Проверка прерывания
		if handler.IsCancelled() {
			fmt.Printf("\n⚠ Operation cancelled by user\n")
			break
		}

		// Генерация имени файла: FILL_00001_ddHHmmss.tmp
		fileName := fmt.Sprintf("FILL_%05d_%s.tmp", i, timestamp)
		targetFilePath := filepath.Join(networkPath, fileName)

		// Копирование файла шаблона в целевой
		bytesCopied, err := copyFileOptimized(templateFilePath, targetFilePath)
		if err != nil {
			fmt.Printf("\n⚠ Stopping: Failed to create file %d: %v\n", i, err)
			break
		}

		filesCreated++
		totalBytesWritten += bytesCopied

		// Обновление прогресса - для сети не показываем процент, так как не знаем общий объем
		if i%10 == 0 {
			progress.Update(filesCreated, totalBytesWritten)
			speedMBps := progress.GetCurrentSpeed()
			gbWritten := float64(totalBytesWritten) / (1024 * 1024 * 1024)
			progress.PrintProgressCustom("Fill %s: %d files (%6.1f MB/s) - %6.2f GB\r",
				networkPath, filesCreated, speedMBps, gbWritten)
		}
	}

	// Очистка файла шаблона
	os.Remove(templateFilePath)

	// Финальное резюме с использованием трекера прогресса
	progress.currentItem = filesCreated
	progress.currentBytes = totalBytesWritten
	progress.Finish("Fill Operation")

	// Автоудаление при запросе
	if autoDelete && filesCreated > 0 {
		fmt.Printf("\nAuto-delete enabled - Deleting all created files...\n")

		// Поиск всех файлов FILL_*.tmp в сетевом пути
		pattern := filepath.Join(networkPath, "FILL_*.tmp")
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

	// Логирование результатов
	if logger != nil {
		logger.SetResult("filesCreated", filesCreated)
		logger.SetResult("totalGBWritten", float64(totalBytesWritten)/(1024*1024*1024))
		logger.SetResult("autoDeleteUsed", autoDelete)
		logger.SetSuccess()
	}

	return nil
}

func runNetworkFillClean(networkPath string, logger *HistoryLogger) error {
	// Настройка логирования истории
	if logger != nil {
		logger.SetCommand("network", networkPath, "clean")
	}

	fmt.Printf("Network Clean Operation\n")
	fmt.Printf("Target: %s\n", networkPath)
	fmt.Printf("Searching for test files (FILL_*.tmp and speedtest_*.txt)...\n\n")

	// Тест доступности и записи сетевого пути
	canRead := testNetworkRead(networkPath)
	canWrite := testNetworkWrite(networkPath)

	if !canRead {
		err := fmt.Errorf("network path is not readable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}
	if !canWrite {
		err := fmt.Errorf("network path is not writable")
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	fmt.Printf("✓ Network path is accessible and writable\n")

	// Поиск всех файлов FILL_*.tmp
	fillPattern := filepath.Join(networkPath, "FILL_*.tmp")
	fillMatches, err := filepath.Glob(fillPattern)
	if err != nil {
		err = fmt.Errorf("failed to search for FILL files: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Поиск всех файлов speedtest_*.txt
	speedtestPattern := filepath.Join(networkPath, "speedtest_*.txt")
	speedtestMatches, err := filepath.Glob(speedtestPattern)
	if err != nil {
		err = fmt.Errorf("failed to search for speedtest files: %w", err)
		if logger != nil {
			logger.SetError(err)
		}
		return err
	}

	// Объединение всех совпадений
	var allMatches []string
	allMatches = append(allMatches, fillMatches...)
	allMatches = append(allMatches, speedtestMatches...)

	if len(allMatches) == 0 {
		fmt.Printf("No test files found in %s\n", networkPath)
		fmt.Printf("Searched for: FILL_*.tmp and speedtest_*.txt\n")
		if logger != nil {
			logger.SetSuccess()
		}
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
	deletionWorkers := 12 // Используем меньше воркеров для сетевых операций
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
	updateTicker := time.NewTicker(200 * time.Millisecond) // Реже для сетевых операций
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

	// Логирование результатов
	if logger != nil {
		logger.SetResult("filesDeleted", deletedCount)
		logger.SetResult("spaceFreedGB", float64(deletedSize)/(1024*1024*1024))
		logger.SetSuccess()
	}

	return nil
}

// testNetworkRead проверяет возможность чтения из сетевого пути
func testNetworkRead(networkPath string) bool {
	// Попытка получить информацию о пути
	_, err := os.Stat(networkPath)
	return err == nil
}

// testNetworkWrite проверяет возможность записи в сетевой путь
func testNetworkWrite(networkPath string) bool {
	// Создание тестового файла для проверки записи
	testFileName := fmt.Sprintf("__filedo_network_test_%d.tmp", time.Now().UnixNano())
	testFilePath := filepath.Join(networkPath, testFileName)
	
	testFile, err := os.Create(testFilePath)
	if err != nil {
		return false
	}
	
	_, err = testFile.WriteString("test")
	testFile.Close()
	
	if err != nil {
		return false
	}
	
	// Очистка тестового файла
	err = os.Remove(testFilePath)
	return err == nil
}