package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DamagedDiskConfig содержит настройки для работы с повреждёнными дисками
type DamagedDiskConfig struct {
	FileTimeout       time.Duration // Таймаут для чтения файла (по умолчанию 10 секунд)
	DamagedLogFile    string        // Путь к лог-файлу повреждённых файлов
	SkipListFile      string        // Путь к файлу со списком файлов для пропуска
	RetryCount        int           // Количество попыток чтения файла
	UseSkipList       bool          // Использовать ли список пропуска при следующих запусках
	LogDetailedErrors bool          // Логировать ли детальные ошибки
	BufferSize        int           // Размер буфера для чтения (меньший для безопасности)
}

// DamagedFileInfo содержит информацию о повреждённом файле
type DamagedFileInfo struct {
	FilePath    string    `json:"filePath"`
	Reason      string    `json:"reason"`
	Timestamp   time.Time `json:"timestamp"`
	Size        int64     `json:"size"`
	AttemptNum  int       `json:"attemptNum"`
	ErrorDetail string    `json:"errorDetail,omitempty"`
}

// DamagedDiskHandler обрабатывает копирование с повреждённых дисков
type DamagedDiskHandler struct {
	config      DamagedDiskConfig
	damagedFiles []DamagedFileInfo
	skipSet     map[string]bool
	mutex       sync.RWMutex
	logFile     *os.File
	workingDir  string
}

// NewDamagedDiskConfig создаёт конфигурацию по умолчанию для повреждённых дисков
func NewDamagedDiskConfig() DamagedDiskConfig {
	return DamagedDiskConfig{
		FileTimeout:       10 * time.Second,
		DamagedLogFile:    "damaged_files.log",
		SkipListFile:      "skip_files.list",
		RetryCount:        1,
		UseSkipList:       true,
		LogDetailedErrors: true,
		BufferSize:        64 * 1024, // 64KB буфер для безопасности
	}
}

// NewDamagedDiskHandler создаёт новый обработчик для повреждённых дисков
func NewDamagedDiskHandler() (*DamagedDiskHandler, error) {
	config := NewDamagedDiskConfig()
	
	// Получаем рабочую директорию (где запущен filedo.exe)
	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "." // fallback to current directory
	}
	
	// Устанавливаем полные пути к лог-файлам
	config.DamagedLogFile = filepath.Join(workingDir, config.DamagedLogFile)
	config.SkipListFile = filepath.Join(workingDir, config.SkipListFile)
	
	handler := &DamagedDiskHandler{
		config:     config,
		skipSet:    make(map[string]bool),
		workingDir: workingDir,
	}
	
	// Загружаем список файлов для пропуска
	if err := handler.loadSkipList(); err != nil {
		fmt.Printf("Warning: Could not load skip list: %v\n", err)
	}
	
	// Открываем лог-файл для записи
	if err := handler.openLogFile(); err != nil {
		fmt.Printf("Warning: Could not open log file: %v\n", err)
	}
	
	return handler, nil
}

// Close закрывает обработчик и сохраняет данные
func (h *DamagedDiskHandler) Close() error {
	if h.logFile != nil {
		h.logFile.Close()
	}
	return h.saveSkipList()
}

// loadSkipList загружает список файлов для пропуска из файла
func (h *DamagedDiskHandler) loadSkipList() error {
	if !h.config.UseSkipList {
		return nil
	}
	
	file, err := os.Open(h.config.SkipListFile)
	if os.IsNotExist(err) {
		return nil // Файл не существует - это нормально для первого запуска
	}
	if err != nil {
		return err
	}
	defer file.Close()
	
	scanner := bufio.NewScanner(file)
	count := 0
	
	h.mutex.Lock()
	defer h.mutex.Unlock()
	
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			h.skipSet[line] = true
			count++
		}
	}
	
	if count > 0 {
		fmt.Printf("📋 Loaded %d previously damaged files from skip list\n", count)
	}
	
	return scanner.Err()
}

// saveSkipList сохраняет список файлов для пропуска в файл
func (h *DamagedDiskHandler) saveSkipList() error {
	if !h.config.UseSkipList {
		return nil
	}
	
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	if len(h.skipSet) == 0 {
		return nil
	}
	
	file, err := os.Create(h.config.SkipListFile)
	if err != nil {
		return err
	}
	defer file.Close()
	
	writer := bufio.NewWriter(file)
	
	// Заголовок файла
	fmt.Fprintf(writer, "# FileDO Damaged Files Skip List\n")
	fmt.Fprintf(writer, "# Generated: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(writer, "# Total files: %d\n\n", len(h.skipSet))
	
	// Записываем пути файлов
	for filePath := range h.skipSet {
		fmt.Fprintf(writer, "%s\n", filePath)
	}
	
	return writer.Flush()
}

// openLogFile открывает лог-файл для записи
func (h *DamagedDiskHandler) openLogFile() error {
	var err error
	h.logFile, err = os.OpenFile(h.config.DamagedLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	
	// Записываем заголовок сессии
	fmt.Fprintf(h.logFile, "\n=== FileDO Damaged Files Log Session ===\n")
	fmt.Fprintf(h.logFile, "Started: %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(h.logFile, "Timeout: %v\n\n", h.config.FileTimeout)
	
	return nil
}

// ShouldSkipFile проверяет, нужно ли пропустить файл
func (h *DamagedDiskHandler) ShouldSkipFile(filePath string) bool {
	if !h.config.UseSkipList {
		return false
	}
	
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	return h.skipSet[filePath]
}

// LogDamagedFile записывает информацию о повреждённом файле
func (h *DamagedDiskHandler) LogDamagedFile(filePath, reason string, size int64, attemptNum int, errorDetail string) {
	info := DamagedFileInfo{
		FilePath:    filePath,
		Reason:      reason,
		Timestamp:   time.Now(),
		Size:        size,
		AttemptNum:  attemptNum,
		ErrorDetail: errorDetail,
	}
	
	h.mutex.Lock()
	h.damagedFiles = append(h.damagedFiles, info)
	h.skipSet[filePath] = true // Добавляем в список пропуска
	h.mutex.Unlock()
	
	// Выводим в консоль
	fmt.Printf("⚠️ SKIPPED: %s (%s)\n", filePath, reason)
	
	// Записываем в лог-файл
	if h.logFile != nil {
		logEntry := fmt.Sprintf("[%s] SKIPPED: %s\n", 
			time.Now().Format("2006-01-02 15:04:05"), filePath)
		logEntry += fmt.Sprintf("  Reason: %s\n", reason)
		logEntry += fmt.Sprintf("  Size: %d bytes\n", size)
		logEntry += fmt.Sprintf("  Attempt: %d\n", attemptNum)
		
		if errorDetail != "" && h.config.LogDetailedErrors {
			logEntry += fmt.Sprintf("  Error: %s\n", errorDetail)
		}
		logEntry += "\n"
		
		h.logFile.WriteString(logEntry)
		h.logFile.Sync()
	}
}

// GetDamagedStats возвращает статистику повреждённых файлов
func (h *DamagedDiskHandler) GetDamagedStats() (int, int64) {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	count := len(h.damagedFiles)
	var totalSize int64
	
	for _, info := range h.damagedFiles {
		totalSize += info.Size
	}
	
	return count, totalSize
}

// GetSkippedStats возвращает статистику пропущенных файлов
func (h *DamagedDiskHandler) GetSkippedStats() int {
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	return len(h.skipSet)
}

// CopyFileWithDamageHandling копирует файл с обработкой повреждений и обновлением прогресса
func (h *DamagedDiskHandler) CopyFileWithDamageHandling(sourcePath, targetPath string, sourceInfo os.FileInfo, progress interface{}) error {
	// Проверяем, нужно ли пропустить файл
	if h.ShouldSkipFile(sourcePath) {
		fmt.Printf("📋 Skipping previously damaged file: %s\n", sourcePath)
		return nil
	}
	
	// Создаём директорию назначения если нужно
	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %v", err)
	}
	
	// Пытаемся скопировать файл с таймаутом
	for attempt := 1; attempt <= h.config.RetryCount; attempt++ {
		err := h.copyFileWithTimeoutAndProgress(sourcePath, targetPath, sourceInfo, attempt, progress)
		
		if err == nil {
			// Успешно скопировали
			return nil
		}
		
		// Анализируем ошибку
		errorStr := err.Error()
		var reason string
		
		if strings.Contains(errorStr, "timeout") || strings.Contains(errorStr, "context deadline exceeded") {
			reason = "timeout"
		} else if strings.Contains(errorStr, "I/O error") || strings.Contains(errorStr, "read error") {
			reason = "I/O error"
		} else if strings.Contains(errorStr, "device hardware error") {
			reason = "hardware error"
		} else if strings.Contains(errorStr, "bad sector") {
			reason = "bad sector"
		} else {
			reason = "read error"
		}
		
		// Если это последняя попытка, логируем как повреждённый
		if attempt >= h.config.RetryCount {
			h.LogDamagedFile(sourcePath, reason, sourceInfo.Size(), attempt, errorStr)
			return nil // Не возвращаем ошибку - продолжаем с другими файлами
		}
		
		fmt.Printf("🔄 Retry %d/%d for %s (reason: %s)\n", attempt, h.config.RetryCount, sourcePath, reason)
		time.Sleep(1 * time.Second) // Небольшая пауза перед повтором
	}
	
	return nil
}

// copyFileWithTimeoutAndProgress копирует файл с таймаутом на основе отсутствия прогресса и обновлением внешнего прогресса
func (h *DamagedDiskHandler) copyFileWithTimeoutAndProgress(sourcePath, targetPath string, sourceInfo os.FileInfo, attemptNum int, externalProgress interface{}) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Канал для результата операции
	done := make(chan error, 1)
	// Канал для отслеживания прогресса чтения
	progressChan := make(chan int64, 1)
	
	go func() {
		done <- h.copyFileInternalWithProgress(ctx, sourcePath, targetPath, sourceInfo, progressChan, externalProgress)
	}()
	
	// Отслеживание прогресса - таймаут только при отсутствии чтения данных
	lastProgressTime := time.Now()
	var lastBytesRead int64 = 0
	
	ticker := time.NewTicker(1 * time.Second) // Проверяем каждую секунду
	defer ticker.Stop()
	
	for {
		select {
		case err := <-done:
			return err
		case bytesRead := <-progressChan:
			// Получили прогресс - обновляем время последнего чтения
			if bytesRead > lastBytesRead {
				lastProgressTime = time.Now()
				lastBytesRead = bytesRead
			}
		case <-ticker.C:
			// Проверяем, не истёк ли таймаут без прогресса
			if time.Since(lastProgressTime) > h.config.FileTimeout {
				cancel() // Отменяем операцию
				return fmt.Errorf("file copy timeout after %v without progress (attempt %d)", h.config.FileTimeout, attemptNum)
			}
		}
	}
}

// copyFileWithTimeout копирует файл с таймаутом на основе отсутствия прогресса (старая версия без внешнего прогресса)
func (h *DamagedDiskHandler) copyFileWithTimeout(sourcePath, targetPath string, sourceInfo os.FileInfo, attemptNum int) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Канал для результата операции
	done := make(chan error, 1)
	// Канал для отслеживания прогресса чтения
	progressChan := make(chan int64, 1)
	
	go func() {
		done <- h.copyFileInternalWithProgress(ctx, sourcePath, targetPath, sourceInfo, progressChan, nil)
	}()
	
	// Отслеживание прогресса - таймаут только при отсутствии чтения данных
	lastProgressTime := time.Now()
	var lastBytesRead int64 = 0
	
	ticker := time.NewTicker(1 * time.Second) // Проверяем каждую секунду
	defer ticker.Stop()
	
	for {
		select {
		case err := <-done:
			return err
		case bytesRead := <-progressChan:
			// Получили прогресс - обновляем время последнего чтения
			if bytesRead > lastBytesRead {
				lastProgressTime = time.Now()
				lastBytesRead = bytesRead
			}
		case <-ticker.C:
			// Проверяем, не истёк ли таймаут без прогресса
			if time.Since(lastProgressTime) > h.config.FileTimeout {
				cancel() // Отменяем операцию
				return fmt.Errorf("file copy timeout after %v without progress (attempt %d)", h.config.FileTimeout, attemptNum)
			}
		}
	}
}

// copyFileInternalWithProgress внутренний метод копирования файла с отправкой прогресса
func (h *DamagedDiskHandler) copyFileInternalWithProgress(ctx context.Context, sourcePath, targetPath string, sourceInfo os.FileInfo, progressChan chan<- int64, externalProgress interface{}) error {
	// Открываем исходный файл
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source file: %v", err)
	}
	defer sourceFile.Close()
	
	// Создаём целевой файл
	targetFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target file: %v", err)
	}
	defer targetFile.Close()
	
	// Используем небольшой буфер для безопасности
	buffer := make([]byte, h.config.BufferSize)
	var totalBytesRead int64 = 0
	
	for {
		// Проверяем контекст перед чтением
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		
		n, readErr := sourceFile.Read(buffer)
		if n > 0 {
			totalBytesRead += int64(n)
			
			// Отправляем прогресс (неблокирующе)
			select {
			case progressChan <- totalBytesRead:
			default:
			}
			
			// Обновляем внешний прогресс если передан
			if externalProgress != nil {
				if fastProgress, ok := externalProgress.(*FastCopyProgress); ok {
					fastProgress.setCurrentFileProgress(sourcePath, sourceInfo.Size(), totalBytesRead)
				}
			}
			
			if _, writeErr := targetFile.Write(buffer[:n]); writeErr != nil {
				return fmt.Errorf("failed to write to target file: %v", writeErr)
			}
		}
		
		if readErr == io.EOF {
			break
		}
		
		if readErr != nil {
			return fmt.Errorf("failed to read from source file: %v", readErr)
		}
	}
	
	// Синхронизируем запись
	if err := targetFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync target file: %v", err)
	}
	
	// Устанавливаем правильные права доступа
	if err := os.Chmod(targetPath, sourceInfo.Mode()); err != nil {
		// Не критичная ошибка, логгируем но не прерываем
		fmt.Printf("Warning: failed to set file permissions: %v\n", err)
	}
	
	return nil
}

// copyFileInternal внутренний метод копирования файла (для обратной совместимости)
func (h *DamagedDiskHandler) copyFileInternal(sourcePath, targetPath string, sourceInfo os.FileInfo) error {
	// Простая заглушка для прогресса
	progressChan := make(chan int64, 1)
	return h.copyFileInternalWithProgress(context.Background(), sourcePath, targetPath, sourceInfo, progressChan, nil)
}

// PrintSummary выводит итоговую сводку
func (h *DamagedDiskHandler) PrintSummary() {
	damagedCount, damagedSize := h.GetDamagedStats()
	skippedCount := h.GetSkippedStats()
	
	if damagedCount == 0 && skippedCount == 0 {
		fmt.Printf("✅ All files processed successfully - no damaged files found\n")
		return
	}
	
	fmt.Printf("\n" + strings.Repeat("=", 60) + "\n")
	fmt.Printf("📊 DAMAGED DISK COPY SUMMARY\n")
	fmt.Printf(strings.Repeat("=", 60) + "\n")
	
	if skippedCount > damagedCount {
		fmt.Printf("📋 Previously damaged files (skipped): %d\n", skippedCount-damagedCount)
	}
	
	if damagedCount > 0 {
		fmt.Printf("⚠️ Newly damaged files found: %d\n", damagedCount)
		fmt.Printf("💽 Total size of damaged files: %s\n", formatDiskFileSize(damagedSize))
		fmt.Printf("📁 Damaged files log: %s\n", h.config.DamagedLogFile)
		fmt.Printf("📋 Skip list updated: %s\n", h.config.SkipListFile)
	}
	
	fmt.Printf("\n💡 RECOMMENDATIONS:\n")
	if damagedCount > 0 {
		fmt.Printf("• Check disk health with disk diagnostic tools\n")
		fmt.Printf("• Consider running next copy with longer timeout (currently %v)\n", h.config.FileTimeout)
		fmt.Printf("• Review damaged files list to determine if they're critical\n")
		fmt.Printf("• Next copy will automatically skip these damaged files\n")
	}
	if skippedCount > 0 {
		fmt.Printf("• To retry previously damaged files, delete: %s\n", h.config.SkipListFile)
		fmt.Printf("• Or manually edit the skip list to remove specific files\n")
	}
	fmt.Printf(strings.Repeat("=", 60) + "\n")
}

// formatDiskFileSize форматирует размер файла для поврежденного диска
func formatDiskFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
