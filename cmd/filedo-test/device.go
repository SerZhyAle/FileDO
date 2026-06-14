package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows"
)

// DeviceTester реализует интерфейс Tester для устройств Windows
type DeviceTester struct {
	drivePath string
}

func NewDeviceTester(drivePath string) *DeviceTester {
	return &DeviceTester{drivePath: drivePath}
}

func (dt *DeviceTester) GetTestInfo() (testType, targetPath string) {
	return "Device", dt.drivePath
}

func (dt *DeviceTester) GetAvailableSpace() (int64, error) {
	var freeBytesAvailableToCaller, totalNumberOfBytes, totalNumberOfFreeBytes uint64

	drivePathPtr, err := windows.UTF16PtrFromString(dt.drivePath + "\\")
	if err != nil {
		return 0, fmt.Errorf("invalid drive path: %v", err)
	}

	err = windows.GetDiskFreeSpaceEx(
		drivePathPtr,
		&freeBytesAvailableToCaller,
		&totalNumberOfBytes,
		&totalNumberOfFreeBytes,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to get disk space for %s: %v", dt.drivePath, err)
	}

	return int64(freeBytesAvailableToCaller), nil
}

func (dt *DeviceTester) CreateTestFile(fileName string, fileSize int64) (filePath string, err error) {
	return dt.CreateTestFileContext(context.Background(), fileName, fileSize)
}

func (dt *DeviceTester) CreateTestFileContext(ctx context.Context, fileName string, fileSize int64) (filePath string, err error) {
	filePath = filepath.Join(dt.drivePath+"\\", fileName)
	
	// Определяем оптимальный размер буфера для этого диска
	bufferSize := getOptimalBufferSize(dt.drivePath)
	
	return filePath, WriteTestFileWithBufferContext(ctx, filePath, fileSize, bufferSize)
}

func (dt *DeviceTester) VerifyTestFile(filePath string) error {
	return VerifyTestFileComplete(filePath)
}

func (dt *DeviceTester) CleanupTestFile(filePath string) error {
	return os.Remove(filePath)
}

func (dt *DeviceTester) GetCleanupCommand() string {
	return fmt.Sprintf("filedo_test.exe %s clean", dt.drivePath)
}

func runDeviceCapacityTest(drivePath string, autoDelete bool, logger *HistoryLogger) error {
	// Проверка существования диска
	if _, err := os.Stat(drivePath + "\\"); os.IsNotExist(err) {
		return fmt.Errorf("drive %s does not exist or is not accessible", drivePath)
	}

	// Получение информации о диске
	driveInfo, err := GetDriveInfo(drivePath)
	if err != nil {
		fmt.Printf("Warning: Could not get extended drive information: %v\n", err)
	} else {
		fmt.Printf("Drive Information:\n")
		fmt.Printf("  Type: %s\n", driveInfo.DriveType)
		fmt.Printf("  File System: %s\n", driveInfo.FileSystem)
		fmt.Printf("  Label: %s\n", driveInfo.VolumeLabel)
		if driveInfo.TotalSize > 0 {
			fmt.Printf("  Total Size: %.2f GB\n", float64(driveInfo.TotalSize)/(1024*1024*1024))
		}
		fmt.Printf("\n")
	}

	tester := NewDeviceTester(drivePath)
	
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

// DriveInfo содержит информацию о диске
type DriveInfo struct {
	DrivePath    string
	DriveType    string
	FileSystem   string
	VolumeLabel  string
	TotalSize    int64
	FreeSpace    int64
}

// GetDriveInfo получает расширенную информацию о диске
func GetDriveInfo(drivePath string) (*DriveInfo, error) {
	info := &DriveInfo{
		DrivePath: drivePath,
	}

	// Получение типа диска
	drivePathPtr, err := windows.UTF16PtrFromString(drivePath + "\\")
	if err != nil {
		return nil, err
	}

	driveType := windows.GetDriveType(drivePathPtr)
	switch driveType {
	case windows.DRIVE_REMOVABLE:
		info.DriveType = "Removable"
	case windows.DRIVE_FIXED:
		info.DriveType = "Fixed"
	case windows.DRIVE_REMOTE:
		info.DriveType = "Network"
	case windows.DRIVE_CDROM:
		info.DriveType = "CD-ROM"
	case windows.DRIVE_RAMDISK:
		info.DriveType = "RAM Disk"
	default:
		info.DriveType = "Unknown"
	}

	// Получение информации о файловой системе и метке тома
	volumeName := make([]uint16, windows.MAX_PATH+1)
	fileSystemName := make([]uint16, windows.MAX_PATH+1)
	var serialNumber, maxComponentLength, fileSystemFlags uint32

	err = windows.GetVolumeInformation(
		drivePathPtr,
		&volumeName[0],
		uint32(len(volumeName)),
		&serialNumber,
		&maxComponentLength,
		&fileSystemFlags,
		&fileSystemName[0],
		uint32(len(fileSystemName)),
	)

	if err == nil {
		info.VolumeLabel = windows.UTF16ToString(volumeName)
		info.FileSystem = windows.UTF16ToString(fileSystemName)
	}

	// Получение размера диска
	var freeBytesAvailableToCaller, totalNumberOfBytes, totalNumberOfFreeBytes uint64
	err = windows.GetDiskFreeSpaceEx(
		drivePathPtr,
		&freeBytesAvailableToCaller,
		&totalNumberOfBytes,
		&totalNumberOfFreeBytes,
	)

	if err == nil {
		info.TotalSize = int64(totalNumberOfBytes)
		info.FreeSpace = int64(freeBytesAvailableToCaller)
	}

	return info, nil
}