package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FolderTester реализует интерфейс Tester для папок
type FolderTester struct {
	folderPath string
}

func NewFolderTester(folderPath string) *FolderTester {
	return &FolderTester{folderPath: folderPath}
}

func (ft *FolderTester) GetTestInfo() (testType, targetPath string) {
	return "Folder", ft.folderPath
}

func (ft *FolderTester) GetAvailableSpace() (int64, error) {
	// Получаем свободное место на диске, где находится папка
	absPath, err := filepath.Abs(ft.folderPath)
	if err != nil {
		return 0, fmt.Errorf("failed to get absolute path: %v", err)
	}

	// Получаем корень диска
	volume := filepath.VolumeName(absPath)
	if volume == "" {
		return 0, fmt.Errorf("could not determine volume for path %s", absPath)
	}

	// Используем DeviceTester для получения свободного места
	deviceTester := NewDeviceTester(volume)
	return deviceTester.GetAvailableSpace()
}

func (ft *FolderTester) CreateTestFile(fileName string, fileSize int64) (filePath string, err error) {
	return ft.CreateTestFileContext(context.Background(), fileName, fileSize)
}

func (ft *FolderTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error) {
	filePath = filepath.Join(ft.folderPath, fileName)
	
	// Определяем оптимальный размер буфера для этой папки
	bufferSize := getOptimalBufferSize(ft.folderPath)
	
	return filePath, WriteTestFileWithBufferContext(ctx, filePath, fileSize, bufferSize)
}

func (ft *FolderTester) VerifyTestFile(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

func (ft *FolderTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (ft *FolderTester) GetCleanupCommand() string {
	return fmt.Sprintf("filedo_test.exe \"%s\" clean", ft.folderPath)
}

func runFolderCapacityTest(folderPath string, autoDelete bool, logger *HistoryLogger) error {
	// Проверка существования папки
	if info, err := os.Stat(folderPath); os.IsNotExist(err) {
		return fmt.Errorf("folder \"%s\" does not exist", folderPath)
	} else if !info.IsDir() {
		return fmt.Errorf("\"%s\" is not a folder", folderPath)
	}

	// Проверка прав записи
	testFile := filepath.Join(folderPath, "__write_test__.tmp")
	if file, err := os.Create(testFile); err != nil {
		return fmt.Errorf("cannot write to folder \"%s\": %v", folderPath, err)
	} else {
		file.Close()
		os.Remove(testFile)
	}

	// Получение информации о папке и диске
	absPath, _ := filepath.Abs(folderPath)
	volume := filepath.VolumeName(absPath)
	
	fmt.Printf("Folder Information:\n")
	fmt.Printf("  Path: %s\n", absPath)
	fmt.Printf("  Volume: %s\n", volume)
	
	if driveInfo, err := GetDriveInfo(volume); err == nil {
		fmt.Printf("  Drive Type: %s\n", driveInfo.DriveType)
		fmt.Printf("  File System: %s\n", driveInfo.FileSystem)
	}
	fmt.Printf("\n")

	tester := NewFolderTester(folderPath)
	
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