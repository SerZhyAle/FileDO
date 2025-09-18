package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NetworkTester реализует интерфейс Tester для сетевых путей
type NetworkTester struct {
	networkPath string
}

func NewNetworkTester(networkPath string) *NetworkTester {
	return &NetworkTester{networkPath: networkPath}
}

func (nt *NetworkTester) GetTestInfo() (testType, targetPath string) {
	return "Network", nt.networkPath
}

func (nt *NetworkTester) GetAvailableSpace() (int64, error) {
	// Для сетевых путей получаем информацию через GetDiskFreeSpaceEx
	// Нормализуем путь для Windows API
	networkPath := nt.networkPath
	if !strings.HasSuffix(networkPath, "\\") {
		networkPath += "\\"
	}

	// Попытка подключения к сетевому ресурсу
	if _, err := os.Stat(networkPath); err != nil {
		return 0, fmt.Errorf("cannot access network path %s: %v", nt.networkPath, err)
	}

	// Используем тот же подход, что и для обычных дисков
	deviceTester := &DeviceTester{drivePath: strings.TrimSuffix(nt.networkPath, "\\")}
	return deviceTester.GetAvailableSpace()
}

func (nt *NetworkTester) CreateTestFile(fileName string, fileSize int64) (filePath string, err error) {
	return nt.CreateTestFileContext(context.Background(), fileName, fileSize)
}

func (nt *NetworkTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error) {
	filePath = filepath.Join(nt.networkPath, fileName)
	
	// Для сетевых операций используем более консервативный буфер
	bufferSize := 4 * 1024 * 1024 // 4MB для сетевых операций
	
	return filePath, WriteTestFileWithBufferContext(ctx, filePath, fileSize, bufferSize)
}

func (nt *NetworkTester) VerifyTestFile(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

func (nt *NetworkTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (nt *NetworkTester) GetCleanupCommand() string {
	return fmt.Sprintf("filedo_test.exe \"%s\" clean", nt.networkPath)
}

func runNetworkCapacityTest(networkPath string, autoDelete bool, logger *HistoryLogger) error {
	// Нормализация сетевого пути
	networkPath = strings.TrimSuffix(networkPath, "/")
	networkPath = strings.TrimSuffix(networkPath, "\\")
	
	// Проверка доступности сетевого ресурса
	if _, err := os.Stat(networkPath); err != nil {
		return fmt.Errorf("network path \"%s\" is not accessible: %v", networkPath, err)
	}

	// Проверка прав записи
	testFile := filepath.Join(networkPath, "__write_test__.tmp")
	if file, err := os.Create(testFile); err != nil {
		return fmt.Errorf("cannot write to network path \"%s\": %v", networkPath, err)
	} else {
		file.Close()
		os.Remove(testFile)
	}

	fmt.Printf("Network Information:\n")
	fmt.Printf("  Path: %s\n", networkPath)
	
	// Пытаемся получить информацию о сетевом диске
	if freeSpace, err := (&DeviceTester{drivePath: networkPath}).GetAvailableSpace(); err == nil {
		fmt.Printf("  Available Space: %.2f GB\n", float64(freeSpace)/(1024*1024*1024))
	}
	fmt.Printf("  Type: Network Share\n")
	fmt.Printf("\n")

	tester := NewNetworkTester(networkPath)
	
	// Создание progress tracker
	progressTracker := func(maxItems int, maxBytes int64, interval time.Duration) *ProgressTracker {
		return NewProgressTracker(int64(maxItems), maxBytes, interval)
	}

	result, err := RunGenericTest(tester, autoDelete, logger, globalInterruptHandler, progressTracker)
	
	if err != nil {
		return err
	}

	// Логирование результатов
	if logger != nil {
		logger.SetResult("testPassed", result.TestPassed)
		logger.SetResult("filesCreated", result.FilesCreated)
		logger.SetResult("totalDataBytes", result.TotalDataBytes)
		logger.SetResult("averageSpeedMBps", result.AverageSpeedMBps)
		if result.BaselineSpeedMBps > 0 {
			logger.SetResult("baselineSpeedMBps", result.BaselineSpeedMBps)
		}
		if !result.TestPassed && len(result.FailureReason) > 0 {
			logger.SetParameter("failureReason", result.FailureReason)
		}
	}

	return nil
}