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

const version = "250916_fill"

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

	if size, ok := hl.entry.Results["totalSize"].(string); ok {
		details = append(details, "Size: "+size)
	}
	if files, ok := hl.entry.Results["fileCount"].(float64); ok {
		details = append(details, fmt.Sprintf("Files: %.0f", files))
	}
	if speed, ok := hl.entry.Results["uploadSpeed"].(string); ok {
		details = append(details, "Speed: "+speed)
	}
	if freed, ok := hl.entry.Results["spaceFreed"].(string); ok {
		details = append(details, "Freed: "+freed)
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

	hi_message := "\n" + start_time.Format("2006-01-02 15:04:05") + " FileDO FILL v" + version + " sza@ukr.net\n"
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

	// Парсинг аргументов для команды FILL
	// Ожидаемый формат: filedo_fill.exe C: 1000 del
	// Должно работать как: filedo.exe C: fill 1000 del
	
	targetPath := args[1]
	
	// Значения по умолчанию
	sizeMBStr := "100"
	autoDelete := false
	cleanMode := false
	
	// Парсинг дополнительных аргументов
	for i := 2; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		
		// Проверка операции очистки
		if arg == "clean" || arg == "c" {
			cleanMode = true
			continue
		}
		
		// Проверка флагов автоудаления
		if arg == "del" || arg == "delete" || arg == "d" {
			autoDelete = true
			continue
		}
		
		// Если не флаг, то это размер
		if !isFlag(arg) {
			sizeMBStr = arg
		}
	}

	var err error

	if cleanMode {
		err = handleCleanOperation(targetPath, historyLogger)
	} else {
		err = handleFillOperation(targetPath, sizeMBStr, autoDelete, historyLogger)
	}

	if err != nil {
		historyLogger.SetError(err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	
	historyLogger.SetSuccess()
}

func showUsage() {
	usage := fmt.Sprintf(`
FileDO FILL v%s - Specialized Fill Operation Tool
Created by sza@ukr.net

USAGE:
  filedo_fill.exe <target> [size] [options]

EXAMPLES:
  filedo_fill.exe C:               → Fill C: with 100MB files (default size)
  filedo_fill.exe D: 500           → Fill D: with 500MB files
  filedo_fill.exe E: 1000 del      → Fill E: with 1000MB files and auto-delete
  filedo_fill.exe C:\temp          → Fill folder with 100MB files
  filedo_fill.exe C:\temp 200 del  → Fill folder with 200MB files and auto-delete
  filedo_fill.exe \\server\share   → Fill network share with 100MB files
  filedo_fill.exe C: clean         → Clean test files from C:
  filedo_fill.exe C:\temp clean    → Clean test files from folder

TARGETS:
  C:, D:, etc.        → Device/drive operations
  C:\folder           → Folder operations  
  \\server\share      → Network operations

SIZE:
  <number>            → File size in megabytes (1-10240)
  Default: 100MB if not specified

OPTIONS:
  del, delete, d      → Auto-delete files after creation (for testing)
  clean, c            → Clean existing test files (FILL_*.tmp, speedtest_*.txt)

NOTES:
• Creates files named FILL_00001_ddHHmmss.tmp, FILL_00002_ddHHmmss.tmp, etc.
• Fills until disk is full or error occurs
• Use Ctrl+C to cancel operation safely
• All operations are logged in history.json
• Compatible with main FileDO - uses same file formats and cleanup commands

`, version)
	fmt.Print(usage)
}

func handleFillOperation(targetPath, sizeMBStr string, autoDelete bool, logger *HistoryLogger) error {
	// Определение типа пути (аналогично логике main filedo)
	targetPath = strings.TrimSpace(targetPath)
	
	// Проверка, является ли это буквой диска
	if len(targetPath) > 0 && ((len(targetPath) == 1) || (len(targetPath) > 1 && len(targetPath) < 4 && string([]rune(targetPath)[1]) == ":")) {
		if len(targetPath) == 1 {
			targetPath += ":"
		}
		// Операция с устройством
		logger.SetCommand("device", targetPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
		
		return runDeviceFill(targetPath, sizeMBStr, autoDelete)
	}
	
	// Проверка, является ли это сетевым путем
	if len(targetPath) > 2 && (targetPath[0:2] == "\\" || targetPath[0:2] == "//") {
		// Сетевая операция
		logger.SetCommand("network", targetPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
		
		return runNetworkFill(targetPath, sizeMBStr, autoDelete, logger)
	}
	
	// Проверка, является ли это существующей папкой
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		// Операция с папкой
		logger.SetCommand("folder", targetPath, "fill")
		logger.SetParameter("size", sizeMBStr)
		logger.SetParameter("autoDelete", autoDelete)
		
		return runFolderFill(targetPath, sizeMBStr, autoDelete)
	}
	
	// Путь не существует или является файлом
	if strings.HasSuffix(targetPath, "/") || strings.HasSuffix(targetPath, "\\") {
		return fmt.Errorf("folder \"%s\" does not exist", targetPath)
	} else {
		return fmt.Errorf("path \"%s\" does not exist or is not a valid target", targetPath)
	}
}

func handleCleanOperation(targetPath string, logger *HistoryLogger) error {
	// Определение типа пути и вызов соответствующей функции очистки
	targetPath = strings.TrimSpace(targetPath)
	
	// Проверка, является ли это буквой диска
	if len(targetPath) > 0 && ((len(targetPath) == 1) || (len(targetPath) > 1 && len(targetPath) < 4 && string([]rune(targetPath)[1]) == ":")) {
		if len(targetPath) == 1 {
			targetPath += ":"
		}
		// Операция очистки устройства
		logger.SetCommand("device", targetPath, "clean")
		return runDeviceFillClean(targetPath)
	}
	
	// Проверка, является ли это сетевым путем
	if len(targetPath) > 2 && (targetPath[0:2] == "\\" || targetPath[0:2] == "//") {
		// Операция очистки сети
		logger.SetCommand("network", targetPath, "clean")
		return runNetworkFillClean(targetPath, logger)
	}
	
	// Проверка, является ли это существующей папкой
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		// Операция очистки папки
		logger.SetCommand("folder", targetPath, "clean")
		return runFolderFillClean(targetPath)
	}
	
	return fmt.Errorf("path \"%s\" does not exist or is not a valid target", targetPath)
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

func isFlag(arg string) bool {
	flags := []string{"del", "delete", "d", "clean", "c", "nodel", "nodelete"}
	for _, flag := range flags {
		if arg == flag {
			return true
		}
	}
	return false
}