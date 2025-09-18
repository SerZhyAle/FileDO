package main

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

const version = "250916_test"

var start_time time.Time
var globalInterruptHandler *InterruptHandler

// HistoryEntry структура для логирования
type HistoryEntry struct {
	Timestamp     time.Time              `json:"timestamp"`
	Command       string                 `json:"command"`
	Target        string                 `json:"target"`
	Operation     string                 `json:"operation"`
	FullCommand   string                 `json:"fullCommand"`
	Parameters    map[string]interface{} `json:"parameters"`
	Results       map[string]interface{} `json:"results"`
	ResultSummary string                 `json:"resultSummary,omitempty"`
	Duration      string                 `json:"duration"`
	Success       bool                   `json:"success"`
	ErrorMsg      string                 `json:"error,omitempty"`
}

// HistoryLogger для совместимости с основным проектом
type HistoryLogger struct {
	enabled      bool
	startTime    time.Time
	entry        HistoryEntry
	originalArgs []string
	historyFile  string
	canWriteHist bool
}

func NewHistoryLogger(args []string) *HistoryLogger {
	// Проверка флагов отключения истории
	enabled := true
	for _, arg := range args {
		if arg == "nohist" || arg == "no_history" {
			enabled = false
			break
		}
	}

	historyFile := "history.json"
	canWriteHist := true

	// Проверка возможности записи в файл истории
	if enabled {
		if file, err := os.OpenFile(historyFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644); err != nil {
			canWriteHist = false
		} else {
			file.Close()
		}
	}

	// Создание строки полной команды
	fullCommand := strings.Join(args, " ")

	return &HistoryLogger{
		enabled:      enabled && canWriteHist,
		startTime:    time.Now(),
		originalArgs: args,
		historyFile:  historyFile,
		canWriteHist: canWriteHist,
		entry: HistoryEntry{
			Timestamp:   time.Now(),
			FullCommand: fullCommand,
			Parameters:  make(map[string]interface{}),
			Results:     make(map[string]interface{}),
		},
	}
}

func (hl *HistoryLogger) SetCommand(command, target, operation string) {
	if !hl.enabled {
		return
	}
	hl.entry.Command = command
	hl.entry.Target = target
	hl.entry.Operation = operation
}

func (hl *HistoryLogger) SetParameter(key string, value interface{}) {
	if !hl.enabled {
		return
	}
	hl.entry.Parameters[key] = value
}

func (hl *HistoryLogger) SetResult(key string, value interface{}) {
	if !hl.enabled {
		return
	}
	hl.entry.Results[key] = value
}

func (hl *HistoryLogger) SetError(err error) {
	if !hl.enabled {
		return
	}
	hl.entry.Success = false
	hl.entry.ErrorMsg = err.Error()
}

func (hl *HistoryLogger) SetSuccess() {
	if !hl.enabled {
		return
	}
	hl.entry.Success = true
}

func (hl *HistoryLogger) SetResultSummary(summary string) {
	if !hl.enabled {
		return
	}
	hl.entry.ResultSummary = summary
}

func (hl *HistoryLogger) Finish() {
	if !hl.enabled {
		return
	}

	hl.entry.Duration = formatDuration(time.Since(hl.startTime))

	// Генерация краткого резюме результата
	if hl.entry.ResultSummary == "" && hl.entry.Success {
		hl.entry.ResultSummary = hl.generateResultSummary()
	}

	saveToHistory(hl.entry)
}

// generateResultSummary создает краткое описание результата операции
func (hl *HistoryLogger) generateResultSummary() string {
	var details []string

	if passed, ok := hl.entry.Results["testPassed"].(bool); ok {
		if passed {
			details = append(details, "PASSED")
		} else {
			details = append(details, "FAILED")
		}
	}
	if files, ok := hl.entry.Results["filesCreated"].(float64); ok {
		details = append(details, fmt.Sprintf("Files: %.0f", files))
	}
	if capacity, ok := hl.entry.Results["estimatedRealCapacityGB"].(float64); ok {
		details = append(details, fmt.Sprintf("Real: %.1fGB", capacity))
	}
	if speed, ok := hl.entry.Results["averageSpeedMBps"].(float64); ok {
		details = append(details, fmt.Sprintf("Speed: %.1fMB/s", speed))
	}

	return strings.Join(details, ", ")
}

func saveToHistory(entry HistoryEntry) error {
	historyFile := "history.json"

	var history []HistoryEntry
	if data, err := os.ReadFile(historyFile); err == nil {
		json.Unmarshal(data, &history)
	}

	history = append(history, entry)

	if len(history) > 1000 {
		history = history[len(history)-1000:]
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(historyFile, data, 0644)
}

func main() {
	start_time = time.Now()

	// Оптимизация GC для лучшей производительности
	debug.SetGCPercent(50)
	runtime.GOMAXPROCS(0)

	// Инициализация глобального обработчика прерываний
	globalInterruptHandler = NewInterruptHandler()

	hi_message := "\n" + start_time.Format("2006-01-02 15:04:05") + " FileDO TEST v" + version + " sza@ukr.net\n"
	fmt.Print(hi_message)

	// Обеспечение всегда печати сообщения завершения
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\nPanic: %v\n", r)
		}
		bue_message := "\n Finish:" + time.Now().Format("2006-01-02 15:04:05") + ", Duration: " + fmt.Sprintf("%.0fs", time.Since(start_time).Seconds()) + "\n"
		fmt.Print(bue_message)
	}()

	args := os.Args

	// Инициализация логгера истории
	historyLogger := NewHistoryLogger(os.Args)
	defer historyLogger.Finish()

	if len(args) < 2 {
		showUsage()
		return
	}

	// Проверка справки
	if isHelpFlag(args[1]) {
		showUsage()
		return
	}

	// Парсинг аргументов для команды TEST
	// Ожидаемый формат: filedo_test.exe C:
	// Должно работать как: filedo.exe C: test
	
	targetPath := args[1]
	
	// Значения по умолчанию
	autoDelete := false
	
	// Парсинг дополнительных аргументов
	for i := 2; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		
		// Проверка флагов автоудаления
		if arg == "del" || arg == "delete" || arg == "d" {
			autoDelete = true
			continue
		}
	}

	err := handleTestOperation(targetPath, autoDelete, historyLogger)

	if err != nil {
		historyLogger.SetError(err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	
	historyLogger.SetSuccess()
}

func showUsage() {
	usage := fmt.Sprintf(`
FileDO TEST v%s - Storage Capacity Testing Tool (Fake Device Detection)
Created by sza@ukr.net

USAGE:
  filedo_test.exe <target> [options]

EXAMPLES:
  filedo_test.exe C:               → Test C: drive capacity (detect fake devices)
  filedo_test.exe D: del           → Test D: drive and auto-delete test files
  filedo_test.exe C:\temp          → Test folder capacity
  filedo_test.exe C:\temp del      → Test folder and auto-delete test files
  filedo_test.exe \\server\share   → Test network share capacity
  filedo_test.exe \\server\share del → Test network share and auto-delete

TARGETS:
  C:, D:, etc.        → Device/drive operations (primary use case)
  C:\folder           → Folder operations  
  \\server\share      → Network operations

OPTIONS:
  del, delete, d      → Auto-delete test files after completion

ABOUT FAKE CAPACITY TEST:
• Creates up to 100 large test files to fill 95%% of available space
• Verifies each file immediately after creation to detect corruption
• Monitors write speeds to detect abnormal behavior
• Detects fake capacity devices that report incorrect free space
• Identifies storage devices that corrupt data when full

TEST PROCESS:
• Calculates optimal file size based on available space
• Creates files named FILL_001_ddHHmmss.tmp, FILL_002_ddHHmmss.tmp, etc.
• Verifies data integrity with pattern matching
• Monitors write performance for anomalies
• Reports estimated real capacity if fake device detected

NOTES:
• Requires at least 100MB free space to run
• Test may take several minutes depending on device speed and capacity
• Use Ctrl+C to cancel operation safely
• All operations are logged in history.json
• Compatible with main FileDO - uses same file formats and cleanup commands

`, version)
	fmt.Print(usage)
}

func handleTestOperation(targetPath string, autoDelete bool, logger *HistoryLogger) error {
	// Определение типа пути (аналогично логике main filedo)
	targetPath = strings.TrimSpace(targetPath)
	
	// Проверка, является ли это буквой диска
	if len(targetPath) > 0 && ((len(targetPath) == 1) || (len(targetPath) > 1 && len(targetPath) < 4 && string([]rune(targetPath)[1]) == ":")) {
		if len(targetPath) == 1 {
			targetPath += ":"
		}
		// Операция с устройством
		logger.SetCommand("device", targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
		
		return runDeviceCapacityTest(targetPath, autoDelete, logger)
	}
	
	// Проверка, является ли это сетевым путем
	if len(targetPath) > 2 && (targetPath[0:2] == "\\" || targetPath[0:2] == "//") {
		// Сетевая операция
		logger.SetCommand("network", targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
		
		return runNetworkCapacityTest(targetPath, autoDelete, logger)
	}
	
	// Проверка, является ли это существующей папкой
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		// Операция с папкой
		logger.SetCommand("folder", targetPath, "test")
		logger.SetParameter("autoDelete", autoDelete)
		
		return runFolderCapacityTest(targetPath, autoDelete, logger)
	}
	
	// Путь не существует или является файлом
	if strings.HasSuffix(targetPath, "/") || strings.HasSuffix(targetPath, "\\") {
		return fmt.Errorf("folder \"%s\" does not exist", targetPath)
	} else {
		return fmt.Errorf("path \"%s\" does not exist or is not a valid target", targetPath)
	}
}

// Вспомогательные функции

func isHelpFlag(arg string) bool {
	helpFlags := []string{"?", "/?", "-?", "--help", "help", "h", "/help"}
	lowerArg := strings.ToLower(arg)
	for _, flag := range helpFlags {
		if lowerArg == flag {
			return true
		}
	}
	return false
}