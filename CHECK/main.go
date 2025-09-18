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

const version = "250916_check"

var start_time time.Time
var globalInterruptHandler *InterruptHandler

// HistoryEntry structure for logg	err := handleCheckOperation(targetPath, checkMode, checkOptions, historyLogger)

	if err != nil {
		historyLogger.SetError(err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		// Don't use os.Exit(1) to allow defer cleanup message
		return
	}pe HistoryEntry struct {
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

// HistoryLogger for compatibility with main project
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

// generateResultSummary creates brief description of operation result
func (hl *HistoryLogger) generateResultSummary() string {
	var details []string

	if found, ok := hl.entry.Results["filesFound"].(int64); ok {
		details = append(details, fmt.Sprintf("Found: %d", found))
	}
	if checked, ok := hl.entry.Results["filesChecked"].(int64); ok {
		details = append(details, fmt.Sprintf("Checked: %d", checked))
	}
	if damaged, ok := hl.entry.Results["filesDamaged"].(int64); ok {
		details = append(details, fmt.Sprintf("Damaged: %d", damaged))
	}
	if speed, ok := hl.entry.Results["readSpeed"].(string); ok {
		details = append(details, "Speed: "+speed)
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

	// GC optimization for better performance
	debug.SetGCPercent(50)
	runtime.GOMAXPROCS(0)

	// Initialize global interrupt handler
	globalInterruptHandler = NewInterruptHandler()

	hi_message := "\n" + start_time.Format("2006-01-02 15:04:05") + " FileDO CHECK v" + version + " sza@ukr.net\n"
	fmt.Print(hi_message)

	// Ensure always printing completion message
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\nPanic: %v\n", r)
		}
		bue_message := "\n Finish:" + time.Now().Format("2006-01-02 15:04:05") + ", Duration: " + fmt.Sprintf("%.0fs", time.Since(start_time).Seconds()) + "\n"
		fmt.Print(bue_message)
	}()

	args := os.Args

	// Initialize history logger
	historyLogger := NewHistoryLogger(os.Args)
	defer historyLogger.Finish()

	if len(args) < 2 {
		showUsage()
		return
	}

	// Check for help
	if isHelpFlag(args[1]) {
		showUsage()
		return
	}

	// Parse arguments for CHECK command
	// Expected format: filedo_check.exe C: [mode] [options]
	// Should work as: filedo.exe C: check [mode] [options]
	
	targetPath := args[1]
	
	// Default values
	checkMode := "balanced" // default mode
	var checkOptions []string
	
	// Parse additional arguments
	for i := 2; i < len(args); i++ {
		arg := strings.ToLower(strings.TrimSpace(args[i]))
		
		// Проверка режимов проверки
		if arg == "quick" || arg == "q" {
			checkMode = "quick"
			continue
		}
		if arg == "balanced" || arg == "b" {
			checkMode = "balanced"
			continue
		}
		if arg == "deep" || arg == "d" {
			checkMode = "deep"
			continue
		}
		
		// Все остальные аргументы передаем как опции
		checkOptions = append(checkOptions, args[i])
	}

	var err error

	err = handleCheckOperation(targetPath, checkMode, checkOptions, historyLogger)

	if err != nil {
		historyLogger.SetError(err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	
	historyLogger.SetSuccess()
}

func showUsage() {
	usage := fmt.Sprintf(`
FileDO CHECK v%s - Specialized CHECK Operation Tool
Created by sza@ukr.net

USAGE:
  filedo_check.exe <target> [mode] [options]

EXAMPLES:
  filedo_check.exe C:                    → Check C: with balanced mode (default)
  filedo_check.exe C: quick              → Quick check C: (fast scan)
  filedo_check.exe C: deep               → Deep check C: (thorough scan)
  filedo_check.exe D: balanced           → Balanced check D:
  filedo_check.exe C:\folder             → Check specific folder
  filedo_check.exe C:\folder quick       → Quick check folder
  filedo_check.exe \\server\share        → Check network share
  filedo_check.exe C: --threshold 5      → Check with 5 second threshold
  filedo_check.exe C: --verbose          → Check with verbose output

TARGETS:
  C:, D:, etc.        → Device/drive operations
  C:\folder           → Folder operations  
  \\server\share      → Network operations

MODES:
  quick, q            → Quick scan (read only first part of files)
  balanced, b         → Balanced scan (read first + middle of files) [DEFAULT]
  deep, d             → Deep scan (read first + middle + end of files)

COMMON OPTIONS:
  --threshold N       → Set delay threshold in seconds (default: 2.0)
  --verbose           → Verbose output with detailed information
  --quiet             → Quiet output with minimal information
  --workers N         → Set number of worker threads
  --report csv|json   → Generate report in specified format
  --max-files N       → Limit number of files to check
  --resume            → Resume from last saved position

ADVANCED OPTIONS:
  --min-mb N          → Only check files larger than N MB
  --max-mb N          → Only check files smaller than N MB
  --include-ext exts  → Only check files with these extensions (comma-separated)
  --exclude-ext exts  → Skip files with these extensions (comma-separated)
  --dry-run           → Simulate operation without changes

NOTES:
• Scans files for read delays that indicate potential damage
• Creates skip_files.list with damaged files automatically
• Creates check_files.list with verified good files
• Files taking > threshold seconds to read are marked as damaged
• Compatible with main FileDO damage detection system
• Use Ctrl+C to cancel operation safely
• All operations are logged in history.json

For advanced options and environment variables, see README.md

`, version)
	fmt.Print(usage)
}

func handleCheckOperation(targetPath, mode string, options []string, logger *HistoryLogger) error {
	// Установка режима через environment variable
	os.Setenv("FILEDO_CHECK_MODE", mode)

	// Определение типа пути (аналогично логике main filedo)
	targetPath = strings.TrimSpace(targetPath)
	
	// Проверка, является ли это буквой диска
	if len(targetPath) > 0 && ((len(targetPath) == 1) || (len(targetPath) > 1 && len(targetPath) < 4 && string([]rune(targetPath)[1]) == ":")) {
		if len(targetPath) == 1 {
			targetPath += ":"
		}
		// Операция с устройством
		logger.SetCommand("device", targetPath, "check")
		logger.SetParameter("mode", mode)
		logger.SetParameter("options", options)
		
		switch mode {
		case "quick":
			return runDeviceCheckQuick(targetPath)
		case "deep":
			return runDeviceCheckDeep(targetPath)
		default: // balanced
			return runDeviceCheck(targetPath)
		}
	}
	
	// Проверка, является ли это сетевым путем
	if len(targetPath) > 2 && (targetPath[0:2] == "\\" || targetPath[0:2] == "//") {
		// Сетевая операция
		logger.SetCommand("network", targetPath, "check")
		logger.SetParameter("mode", mode)
		logger.SetParameter("options", options)
		
		switch mode {
		case "quick":
			return runNetworkCheckQuick(targetPath, logger)
		case "deep":
			return runNetworkCheckDeep(targetPath, logger)
		default: // balanced
			return runNetworkCheck(targetPath, logger)
		}
	}
	
	// Проверка, является ли это существующей папкой
	if info, err := os.Stat(targetPath); err == nil && info.IsDir() {
		// Операция с папкой
		logger.SetCommand("folder", targetPath, "check")
		logger.SetParameter("mode", mode)
		logger.SetParameter("options", options)
		
		switch mode {
		case "quick":
			return runFolderCheckQuick(targetPath)
		case "deep":
			return runFolderCheckDeep(targetPath)
		default: // balanced
			return runFolderCheck(targetPath)
		}
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