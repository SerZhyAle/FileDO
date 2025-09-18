package main

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

// VerifyTestFileStartEnd checks file for correct header at start and end
func VerifyTestFileStartEnd(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

// VerifyTestFileComplete performs full verification of test file
func VerifyTestFileComplete(filePath string) error {
	return VerifyTestFileCompleteContext(context.Background(), filePath)
}

// VerifyTestFileQuick performs quick verification (header + footer + one random position in middle)
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

	// Get file information
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

	// Проверка формата заголовка
	if !strings.HasPrefix(firstLine, "FILEDO_TEST_") {
		return fmt.Errorf("invalid header format: %s", firstLine)
	}

	// Проверка контекста
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Расчет позиции последней строки
	lastLinePos := fileSize - int64(len(firstLine)+1)
	if lastLinePos < 0 {
		lastLinePos = 0
	}

	// Переход к последней строке
	_, err = file.Seek(lastLinePos, 0)
	if err != nil {
		return fmt.Errorf("could not seek to last line: %v", err)
	}

	// Чтение последней строки
	lastLineBuffer := make([]byte, 256)
	n, err = file.Read(lastLineBuffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("could not read file footer: %v", err)
	}

	// Проверка контекста
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Извлечение последней строки
	lastLine := string(lastLineBuffer[:n])
	if newlineIndex := strings.Index(lastLine, "\n"); newlineIndex > 0 {
		lastLine = lastLine[:newlineIndex]
	}

	// Проверка соответствия футера заголовку
	if firstLine != lastLine {
		return fmt.Errorf("header/footer mismatch: '%s' vs '%s'", firstLine, lastLine)
	}

	// Быстрая проверка паттерна в середине файла (только одна позиция)
	const minSizeForPatternCheck = 1024
	if fileSize >= minSizeForPatternCheck {
		if err := verifyPatternQuickContext(ctx, file, fileSize, firstLine); err != nil {
			return err
		}
	}

	return nil
}

// VerifyTestFileFull выполняет полную верификацию тестового файла (алиас для совместимости)
func VerifyTestFileFull(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

// VerifyTestFileFullContext выполняет полную верификацию с контекстом (алиас для совместимости) 
func VerifyTestFileFullContext(ctx context.Context, filePath string) error {
	return VerifyTestFileCompleteContext(ctx, filePath)
}

// VerifyTestFileCompleteContext выполняет полную верификацию с контекстом
func VerifyTestFileCompleteContext(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("could not open file: %v", err)
	}
	defer file.Close()

	// Получение информации о файле
	fileInfo, err := file.Stat()
	if err != nil {
		return fmt.Errorf("could not get file info: %v", err)
	}

	fileSize := fileInfo.Size()
	if fileSize < 100 {
		return fmt.Errorf("file too small: %d bytes", fileSize)
	}

	// Проверка контекста перед началом верификации
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Чтение первой строки (заголовка)
	firstLineBuffer := make([]byte, 256)
	n, err := file.Read(firstLineBuffer)
	if err != nil {
		return fmt.Errorf("could not read file header: %v", err)
	}

	if n == 0 {
		return fmt.Errorf("file is empty")
	}

	// Извлечение первой строки
	firstLine := string(firstLineBuffer[:n])
	if newlineIndex := strings.Index(firstLine, "\n"); newlineIndex > 0 {
		firstLine = firstLine[:newlineIndex]
	}

	// Проверка формата заголовка
	if !strings.HasPrefix(firstLine, "FILEDO_TEST_") {
		return fmt.Errorf("invalid header format: %s", firstLine)
	}

	// Проверка контекста после верификации заголовка
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Расчет позиции последней строки
	lastLinePos := fileSize - int64(len(firstLine)+1)
	if lastLinePos < 0 {
		lastLinePos = 0
	}

	// Переход к последней строке
	_, err = file.Seek(lastLinePos, 0)
	if err != nil {
		return fmt.Errorf("could not seek to last line: %v", err)
	}

	// Чтение последней строки
	lastLineBuffer := make([]byte, 256)
	n, err = file.Read(lastLineBuffer)
	if err != nil && err != io.EOF {
		return fmt.Errorf("could not read file footer: %v", err)
	}

	// Проверка контекста после чтения футера
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Извлечение последней строки
	lastLine := string(lastLineBuffer[:n])
	if newlineIndex := strings.Index(lastLine, "\n"); newlineIndex > 0 {
		lastLine = lastLine[:newlineIndex]
	}

	// Проверка соответствия футера заголовку
	if firstLine != lastLine {
		return fmt.Errorf("header/footer mismatch: '%s' vs '%s'", firstLine, lastLine)
	}

	// Продолжаем верификацией паттерна, если файл достаточно большой
	const minSizeForPatternCheck = 1024
	if fileSize < minSizeForPatternCheck {
		return nil
	}

	// Проверка контекста перед верификацией паттерна
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Верификация паттерна с проверкой контекста
	return verifyPatternWithContext(ctx, file, fileSize, firstLine)
}

// VerifySmartTestFiles выполняет умную верификацию согласно новой стратегии:
// - Полная проверка каждого 5-го файла
// - После каждого 5-го файла (5,10,15,20..) проверяем 1-й файл
// - После каждого 10-го файла (10,20,30..) проверяем 5-й файл  
// - После каждого 20-го файла (20,40,60..) проверяем 10-й файл
func VerifySmartTestFiles(filePaths []string, currentIndex int) error {
	return VerifySmartTestFilesContext(context.Background(), filePaths, currentIndex)
}

// VerifySmartTestFilesContext выполняет умную верификацию с контекстом
func VerifySmartTestFilesContext(ctx context.Context, filePaths []string, currentIndex int) error {
	if len(filePaths) == 0 {
		return nil
	}

	// Определяем какие файлы нужно проверить
	filesToVerify := make(map[int]bool) // map[index]fullVerification

	// Всегда проверяем текущий файл
	if currentIndex%5 == 0 {
		// Каждый 5-й файл - полная проверка
		filesToVerify[currentIndex-1] = true // currentIndex начинается с 1, массив с 0
	} else {
		// Остальные файлы - быстрая проверка
		filesToVerify[currentIndex-1] = false
	}

	// Дополнительные контрольные проверки
	if currentIndex%5 == 0 {
		// После каждого 5-го файла проверяем 1-й файл (быстро)
		if len(filePaths) >= 1 {
			filesToVerify[0] = false
		}
	}

	if currentIndex%10 == 0 {
		// После каждого 10-го файла проверяем 5-й файл (быстро)
		if len(filePaths) >= 5 {
			filesToVerify[4] = false // 5-й файл имеет индекс 4
		}
	}

	if currentIndex%20 == 0 {
		// После каждого 20-го файла проверяем 10-й файл (быстро)
		if len(filePaths) >= 10 {
			filesToVerify[9] = false // 10-й файл имеет индекс 9
		}
	}

	// Выполняем верификацию выбранных файлов
	for fileIndex, fullVerification := range filesToVerify {
		if fileIndex >= len(filePaths) {
			continue
		}

		// Проверка контекста перед каждым файлом
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		filePath := filePaths[fileIndex]
		var err error

		if fullVerification {
			// Полная верификация
			err = VerifyTestFileFullContext(ctx, filePath)
		} else {
			// Быстрая верификация
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

// VerifyAllTestFiles проверяет все файлы в списке с индикацией прогресса
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

// VerifyAllTestFilesContext проверяет все файлы в списке с поддержкой контекста
func VerifyAllTestFilesContext(ctx context.Context, filePaths []string) error {
	if len(filePaths) == 0 {
		return nil
	}

	for i, filePath := range filePaths {
		// Проверка контекста перед верификацией каждого файла
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

// CalibrateOptimalBufferSize calibrates optimal buffer size for writing
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
		// Создание тестового файла
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
	// Создание файла с оптимизированными флагами для более быстрой записи
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		return err
	}
	defer file.Close()

	// Генерация уникального заголовка с именем файла и временной меткой
	fileName := filepath.Base(filePath)
	timestamp := time.Now().Format("20060102_150405")
	headerLine := fmt.Sprintf("FILEDO_TEST_%s_%s\n", fileName, timestamp)

	// Запись заголовка
	written, err := file.WriteString(headerLine)
	if err != nil {
		return err
	}

	// Расчет оставшегося места для данных и футера
	remaining := fileSize - int64(written) - int64(len(headerLine)) // Резервируем место для футера (такого же как заголовок)

	// Заполнение читаемым паттерном
	pattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(pattern)
	block := make([]byte, bufferSize)

	// Fill buffer with pattern - optimize with pre-filling
	for i := 0; i < bufferSize; {
		copyLen := min(len(patternBytes), bufferSize-i)
		copy(block[i:i+copyLen], patternBytes[:copyLen])
		i += copyLen
	}

	// Запись блоков данных большими кусками с частой проверкой контекста
	blockCount := 0
	const checkInterval = 100 // Проверяем контекст каждые 100 блоков

	for remaining > int64(len(headerLine)) {
		// Проверка отмены контекста чаще для больших файлов
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

	// Финальная проверка контекста перед футером
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Запись футера (такого же как заголовок)
	_, err = file.WriteString(headerLine)
	if err != nil {
		return err
	}

	// Явно синхронизируем только один раз в конце для лучшей производительности
	return file.Sync()
}

func verifyPatternWithContext(ctx context.Context, file *os.File, fileSize int64, firstLine string) error {
	dataPattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(dataPattern)

	// Расчет границ области данных
	headerSize := int64(len(firstLine) + 1)
	footerSize := headerSize
	dataStart := headerSize
	dataEnd := fileSize - footerSize

	if dataEnd-dataStart < int64(len(patternBytes)*4) {
		return nil
	}

	// Генерация позиций проверки
	var checkPositions []int64
	checkPositions = append(checkPositions, dataStart)
	checkPositions = append(checkPositions, dataEnd-int64(len(patternBytes)*2))

	// Добавление случайных позиций с современным Go random
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
		// Проверка контекста перед верификацией каждой позиции
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

		// Поиск паттерна в прочитанном куске
		found := false
		for j := 0; j <= n-len(patternBytes); j++ {
			if string(readBuffer[j:j+len(patternBytes)]) == dataPattern {
				found = true
				break
			}
		}

		if !found {
			// Проверим, есть ли у нас валидные символы паттерна
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

		// Индикация прогресса для больших файлов
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

// verifyPatternQuickContext выполняет быструю проверку паттерна (одна случайная позиция в середине)
func verifyPatternQuickContext(ctx context.Context, file *os.File, fileSize int64, firstLine string) error {
	dataPattern := "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 "
	patternBytes := []byte(dataPattern)

	// Расчет границ области данных
	headerSize := int64(len(firstLine) + 1)
	footerSize := headerSize
	dataStart := headerSize
	dataEnd := fileSize - footerSize

	if dataEnd-dataStart < int64(len(patternBytes)*4) {
		return nil
	}

	// Проверка контекста
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Генерация одной случайной позиции в середине файла
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	minPos := dataStart + (dataEnd-dataStart)/4      // Начинаем с 1/4 файла
	maxPos := dataEnd - (dataEnd-dataStart)/4        // Заканчиваем на 3/4 файла
	
	if maxPos <= minPos {
		// Если файл слишком маленький, проверяем середину
		minPos = dataStart + int64(len(patternBytes))
		maxPos = dataEnd - int64(len(patternBytes)*2)
	}
	
	if maxPos <= minPos {
		return nil // Файл слишком маленький для проверки
	}

	randomPos := minPos + rng.Int63n(maxPos-minPos)
	
	// Переход к случайной позиции
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

	// Проверка контекста после чтения
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Поиск паттерна в прочитанном куске
	found := false
	for j := 0; j <= n-len(patternBytes); j++ {
		if string(readBuffer[j:j+len(patternBytes)]) == dataPattern {
			found = true
			break
		}
	}

	if !found {
		// Проверим, есть ли у нас валидные символы паттерна
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

// formatDuration форматирует продолжительность времени
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	
	totalSeconds := int64(d.Seconds())
	days := totalSeconds / 86400
	hours := (totalSeconds % 86400) / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	
	if days > 0 {
		return fmt.Sprintf("%dd/%02d:%02d:%02d", days, hours, minutes, seconds)
	} else if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	} else if minutes > 0 {
		return fmt.Sprintf("%d:%02d", minutes, seconds)
	} else {
		return fmt.Sprintf("%ds", seconds)
	}
}