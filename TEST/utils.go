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

// VerifyTestFileStartEnd проверяет файл на корректный заголовок в начале и конце
func VerifyTestFileStartEnd(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

// VerifyTestFileComplete выполняет полную верификацию тестового файла
func VerifyTestFileComplete(filePath string) error {
	return VerifyTestFileCompleteContext(context.Background(), filePath)
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

// CalibrateOptimalBufferSize калибрует оптимальный размер буфера для записи
func CalibrateOptimalBufferSize(testPath string) int {
	// Тестируем разные размеры буфера (4MB до 128MB)
	bufferSizes := []int{
		4 * 1024 * 1024,   // 4MB
		8 * 1024 * 1024,   // 8MB
		16 * 1024 * 1024,  // 16MB
		32 * 1024 * 1024,  // 32MB
		64 * 1024 * 1024,  // 64MB
		128 * 1024 * 1024, // 128MB
	}

	testFileSize := 50 * 1024 * 1024 // 50MB тестовый файл
	bestBuffer := bufferSizes[2]     // По умолчанию 16MB
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

// WriteTestFileWithBuffer записывает тестовый файл с указанным размером буфера
func WriteTestFileWithBuffer(filePath string, fileSize int64, bufferSize int) error {
	return WriteTestFileWithBufferContext(context.Background(), filePath, fileSize, bufferSize)
}

// WriteTestFileWithBufferContext записывает тестовый файл с контекстом для отмены
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

	// Заполнение буфера паттерном - оптимизируем предварительным заполнением
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

// formatDuration форматирует продолжительность времени
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm%.0fs", d.Minutes(), float64(d%time.Minute)/float64(time.Second))
	}
	return fmt.Sprintf("%.0fh%.0fm%.0fs", d.Hours(), float64(d%time.Hour)/float64(time.Minute), float64(d%time.Minute)/float64(time.Second))
}