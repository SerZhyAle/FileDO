package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	Quiet             bool          // Тихий режим (без лишних сообщений в консоль)
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
	persistedSkipSet map[string]bool
	mutex       sync.RWMutex
	workingDir  string

	// Session stats
	sessionSkippedCount int
	sessionLastSkipped  string
}

// NewDamagedDiskConfig создаёт конфигурацию по умолчанию для повреждённых дисков
func NewDamagedDiskConfig() DamagedDiskConfig {
	// Allow override via environment variable (seconds)
	timeout := 10 * time.Second
	if v := os.Getenv("FILEDO_TIMEOUT_NOPROGRESS_SECONDS"); v != "" {
		if n, err := time.ParseDuration(v + "s"); err == nil && n > 0 {
			timeout = n
		}
	}
	return DamagedDiskConfig{
		FileTimeout:       timeout,
		DamagedLogFile:    "damaged_files.log",
		SkipListFile:      "skip_files.list",
		RetryCount:        1,
		UseSkipList:       true,
		LogDetailedErrors: true,
		BufferSize:        64 * 1024, // 64KB буфер для безопасности
		Quiet:             false,
	}
}

// global flag to avoid repeated prints about loaded skip list
var skipListLoadedPrinted bool

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
	persistedSkipSet: make(map[string]bool),
		workingDir: workingDir,
	}
	
	// Загружаем список файлов для пропуска
	if err := handler.loadSkipList(); err != nil {
		fmt.Printf("Warning: Could not load skip list: %v\n", err)
	}
	
	return handler, nil
}

// NewDamagedDiskHandlerQuiet создаёт обработчик в тихом режиме (без информационных сообщений)
func NewDamagedDiskHandlerQuiet() (*DamagedDiskHandler, error) {
	config := NewDamagedDiskConfig()
	config.Quiet = true

	workingDir, err := os.Getwd()
	if err != nil {
		workingDir = "."
	}
	config.DamagedLogFile = filepath.Join(workingDir, config.DamagedLogFile)
	config.SkipListFile = filepath.Join(workingDir, config.SkipListFile)

	handler := &DamagedDiskHandler{
		config:     config,
		skipSet:    make(map[string]bool),
		persistedSkipSet: make(map[string]bool),
		workingDir: workingDir,
	}
	if err := handler.loadSkipList(); err != nil && !config.Quiet {
		fmt.Printf("Warning: Could not load skip list: %v\n", err)
	}
	return handler, nil
}

// Close закрывает обработчик и сохраняет данные
func (h *DamagedDiskHandler) Close() error {
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
			norm := h.normalizePath(line)
			h.skipSet[norm] = true
			h.persistedSkipSet[norm] = true
			count++
		}
	}
	
	if count > 0 && !h.config.Quiet && !skipListLoadedPrinted {
		fmt.Printf("📋 Loaded %d previously damaged files from skip list\n", count)
		skipListLoadedPrinted = true
	}
	
	return scanner.Err()
}

// saveSkipList сохраняет список файлов для пропуска в файл
func (h *DamagedDiskHandler) saveSkipList() error {
	if !h.config.UseSkipList {
		return nil
	}
	// Собираем список новых (за текущую сессию) путей и дописываем в файл без заголовков
	h.mutex.RLock()
	damagedSnapshot := make([]DamagedFileInfo, len(h.damagedFiles))
	copy(damagedSnapshot, h.damagedFiles)
	h.mutex.RUnlock()

	if len(damagedSnapshot) == 0 {
		return nil
	}

	// Откроем файл в режиме append
	file, err := os.OpenFile(h.config.SkipListFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	sessionWritten := make(map[string]bool)

	for _, info := range damagedSnapshot {
		norm := h.normalizePath(info.FilePath)
		// Пропускаем уже сохранённые ранее или уже записанные в этой сессии
		if h.persistedSkipSet[norm] || sessionWritten[norm] {
			continue
		}
		if _, err := fmt.Fprintf(writer, "%s\n", norm); err != nil {
			return err
		}
		sessionWritten[norm] = true
	}

	if err := writer.Flush(); err != nil {
		return err
	}

	// Обновляем persistedSkipSet новыми записями
	h.mutex.Lock()
	for norm := range sessionWritten {
		h.persistedSkipSet[norm] = true
		h.skipSet[norm] = true
	}
	h.mutex.Unlock()
	return nil
}

// openLogFile открывает лог-файл для записи
// openLogFile removed: damaged_files.log is no longer used

// ShouldSkipFile проверяет, нужно ли пропустить файл
func (h *DamagedDiskHandler) ShouldSkipFile(filePath string) bool {
	if !h.config.UseSkipList {
		return false
	}
	
	h.mutex.RLock()
	defer h.mutex.RUnlock()
	
	return h.skipSet[h.normalizePath(filePath)]
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
    
	// Немедленно фиксируем в памяти и (если включено) дописываем в skip_files.list без дубликатов
	h.mutex.Lock()
	h.damagedFiles = append(h.damagedFiles, info)
	norm := h.normalizePath(filePath)
	h.skipSet[norm] = true
	if h.config.UseSkipList && !h.persistedSkipSet[norm] {
		if f, err := os.OpenFile(h.config.SkipListFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			// Каждую запись с новой строки, без заголовков
			fmt.Fprintf(f, "%s\n", norm)
			f.Close()
			h.persistedSkipSet[norm] = true
		} else {
			fmt.Printf("Warning: failed to append to skip list: %v\n", err)
		}
	}
	h.mutex.Unlock()
	
	// Обновляем сессионные счетчики
	h.mutex.Lock()
	h.sessionSkippedCount++
	h.sessionLastSkipped = filePath
	sc := h.sessionSkippedCount
	ls := h.sessionLastSkipped
	h.mutex.Unlock()

	// Выводим компактно, если не тихий режим
	if !h.config.Quiet {
		fmt.Printf("⚠️ SKIPPED: %s (%s) | session: %d, last: %s\n", filePath, reason, sc, ls)
	}
	
	// damaged_files.log disabled; rely on skip_files.list and console output only
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
		
		// Если это была отмена пользователем — прерываем без логирования как повреждённый
		if strings.Contains(errorStr, "interrupted by user") {
			return fmt.Errorf("operation interrupted by user")
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
	// Derive from global interrupt context if available to support Ctrl+C
	parentCtx := context.Background()
	if globalInterruptHandler != nil {
		parentCtx = globalInterruptHandler.Context()
	}
	ctx, cancel := context.WithCancel(parentCtx)
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
			// If cancelled due to interrupt, return a specific error
			if err != nil && ctx.Err() != nil && time.Since(lastProgressTime) <= h.config.FileTimeout {
				return fmt.Errorf("operation interrupted by user")
			}
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

	// Unblock stuck Read/Write on cancellation by closing files when context is done
	cancelOnce := sync.Once{}
	go func() {
		<-ctx.Done()
		cancelOnce.Do(func() {
			sourceFile.Close()
			targetFile.Close()
		})
	}()
	
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

// normalizePath нормализует путь для Windows/Unix (кейс и разделители)
func (h *DamagedDiskHandler) normalizePath(p string) string {
	if p == "" {
		return p
	}
	abs := p
	if ap, err := filepath.Abs(p); err == nil {
		abs = ap
	}
	clean := filepath.Clean(abs)
	if runtime.GOOS == "windows" {
		return strings.ToLower(clean)
	}
	return clean
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
