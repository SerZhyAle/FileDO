// sza250407
// sza2504072115
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const version = "2507050300"

type HistoryEntry struct {
	Timestamp  time.Time              `json:"timestamp"`
	Command    string                 `json:"command"`
	Target     string                 `json:"target"`
	Operation  string                 `json:"operation"`
	Parameters map[string]interface{} `json:"parameters"`
	Results    map[string]interface{} `json:"results"`
	Duration   string                 `json:"duration"`
	Success    bool                   `json:"success"`
	ErrorMsg   string                 `json:"error,omitempty"`
}

type HistoryLogger struct {
	enabled   bool
	startTime time.Time
	entry     HistoryEntry
}

func NewHistoryLogger(args []string) *HistoryLogger {
	enabled := false
	for _, arg := range args {
		if arg == "history" || arg == "hist" {
			enabled = true
			break
		}
	}

	return &HistoryLogger{
		enabled:   enabled,
		startTime: time.Now(),
		entry: HistoryEntry{
			Timestamp:  time.Now(),
			Parameters: make(map[string]interface{}),
			Results:    make(map[string]interface{}),
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

func (hl *HistoryLogger) Finish() {
	if !hl.enabled {
		return
	}

	hl.entry.Duration = formatDuration(time.Since(hl.startTime))

	saveToHistory(hl.entry)
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

var usage = fmt.Sprintf(`FileDO %s sza@ukr.net
Processes files on devices/folders/networks.
Usage:
  filedo.exe <command> [arguments]

Commands:
  device <path> [info|i|short|s] Show information about a disk volume. Use 'short' for concise output.
  device <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test device write speed. Use 'max' for 10GB test.
  device <path> fill <size_mb> [del] Fill device with test files of specified size until full. Use with 'del' for secure wiping of free space to prevent data recovery.
  device <path> <cln|clean|c> Delete all test files (FILL_*.tmp and speedtest_*.txt) from device.
  device <path> test [del|delete|d] Test device for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  folder <path> [info|i|short|s] Show information about a folder and its size. Use 'short' for concise output.
  folder <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test folder write speed. Use 'max' for 10GB test.
  folder <path> fill <size_mb> [del] Fill folder with test files of specified size until full. Use with 'del' for secure wiping of free space to prevent data recovery.
  folder <path> <cln|clean|c> Delete all test files (FILL_*.tmp and speedtest_*.txt) from folder.
  folder <path> test [del|delete|d] Test folder for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  file <path> [info|i|short|s] Show information about a file. Use 'short' for concise output.
  
  network <path> [info|i] Show information about a network path.
  network <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test network speed. Use 'max' for 10GB test.
  network <path> fill <size_mb> [del] Fill network path with test files of specified size until full. Use with 'del' for secure wiping of free space to prevent data recovery.
  network <path> <cln|clean|c> Delete all test files (FILL_*.tmp and speedtest_*.txt) from network path.
  network <path> test [del|delete|d] Test network path for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  from <filepath> Execute commands from file (one command per line). Empty lines and lines starting with # are ignored.
  hist Show last 10 history entries in user-friendly format.

Note: Use no|nodel|nodelete to keep the test file on the destination.
Note: Use short|s with speed tests to show only final upload/download results.
Note: Fill creates files named FILL_#####_ddHHmmss.tmp until available space is used.
Note: Use cln|clean|c to delete all test files (FILL_*.tmp and speedtest_*.txt) from the specified location.
Note: Use del with fill to automatically delete all created files after successful completion.
Security: Use 'fill <size> del' to securely overwrite free space and prevent recovery of deleted files.
Example: filedo.exe device C: fill 1000 del

Flags:
  ?    Show this help message.`, version)

var list_of_flags_for_device = []string{"device", "dev", "disk", "d"}
var list_of_flags_for_folder = []string{"folder", "fold", "dir", "fld"}
var list_of_flags_for_file = []string{"file", "fl", "f"}
var list_of_flags_for_network = []string{"network", "net", "n"}
var list_of_flags_for_from = []string{"from", "batch", "script"}
var list_of_flags_for_hist = []string{"hist", "history"}
var list_fo_flags_for_help = []string{"?", "help", "h", "?"}
var list_of_flags_for_all = append(append(append(append(append(list_of_flags_for_device, list_of_flags_for_folder...), list_of_flags_for_file...), list_of_flags_for_network...), list_of_flags_for_from...), list_of_flags_for_hist...)

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func executeFromFile(filePath string, historyLogger *HistoryLogger) {
	historyLogger.SetCommand("from", filePath, "batch")

	file, err := os.Open(filePath)
	if err != nil {
		historyLogger.SetError(fmt.Errorf("failed to open file: %w", err))
		fmt.Fprintf(os.Stderr, "Error: Cannot open file '%s': %v\n", filePath, err)
		os.Exit(1)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	commandCount := 0
	successCount := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		commandCount++
		fmt.Printf("\n[%d] Executing: %s\n", commandCount, line)

		// Split command into arguments
		args := strings.Fields(line)
		if len(args) == 0 {
			continue
		}
		var err error

		// Check if it's a filedo command (starts with filedo or is a known internal command)
		if args[0] == "filedo" || args[0] == "./filedo.exe" || args[0] == "filedo.exe" {
			// Execute as internal command (remove "filedo" prefix)
			err = executeInternalCommand(args[1:])
		} else if contains(list_of_flags_for_all, strings.ToLower(args[0])) || isValidPath(args[0]) {
			// Execute as internal command (all args)
			err = executeInternalCommand(args)
		} else {
			// Execute as external command
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err = cmd.Run()
		}

		if err != nil {
			fmt.Printf("Command failed with error: %v\n", err)
		} else {
			successCount++
		}
	}

	if err := scanner.Err(); err != nil {
		historyLogger.SetError(fmt.Errorf("error reading file: %w", err))
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\nBatch execution complete: %d/%d commands succeeded\n", successCount, commandCount)

	historyLogger.SetResult("totalCommands", commandCount)
	historyLogger.SetResult("successfulCommands", successCount)
	historyLogger.SetSuccess()
}

func executeInternalCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("empty command")
	}

	// Create a new history logger for internal command
	internalLogger := NewHistoryLogger(append([]string{"filedo"}, args...))
	defer internalLogger.Finish()

	// Parse command similar to main function logic
	command := ""
	add_args := []string{}

	// Convert to lowercase for comparison
	lowerArgs := make([]string, len(args))
	for i, arg := range args {
		lowerArgs[i] = strings.ToLower(arg)
	}

	firstArg := lowerArgs[0]

	if contains(list_of_flags_for_all, firstArg) {
		command = firstArg
		add_args = lowerArgs[1:]
	} else {
		// Auto-detect based on path
		if (len(firstArg) == 1) || (len(firstArg) > 1 && len(firstArg) < 4 && string([]rune(firstArg)[1]) == ":") {
			if len(firstArg) == 1 {
				lowerArgs[0] += ":"
			}
			command = "device"
			add_args = lowerArgs
		} else if len(firstArg) > 2 && (firstArg[0:2] == "\\" || firstArg[0:2] == "//") {
			command = "network"
			add_args = lowerArgs
		} else {
			if info, err := os.Stat(args[0]); err == nil {
				if info.IsDir() {
					command = "folder"
					add_args = lowerArgs
				} else {
					command = "file"
					add_args = lowerArgs
				}
			} else {
				return fmt.Errorf("unknown command or path: %s", args[0])
			}
		}
	}

	// Create flag sets that don't exit on error
	switch {
	case contains(list_of_flags_for_device, command):
		deviceCmd := flag.NewFlagSet("device", flag.ContinueOnError)
		deviceCmd.SetOutput(os.Stdout) // Suppress error output
		runGenericCommand(deviceCmd, CommandDevice, add_args, internalLogger)
	case contains(list_of_flags_for_folder, command):
		folderCmd := flag.NewFlagSet("folder", flag.ContinueOnError)
		folderCmd.SetOutput(os.Stdout)
		runGenericCommand(folderCmd, CommandFolder, add_args, internalLogger)
	case contains(list_of_flags_for_file, command):
		fileCmd := flag.NewFlagSet("file", flag.ContinueOnError)
		fileCmd.SetOutput(os.Stdout)
		runGenericCommand(fileCmd, CommandFile, add_args, internalLogger)
	case contains(list_of_flags_for_network, command):
		networkCmd := flag.NewFlagSet("network", flag.ContinueOnError)
		networkCmd.SetOutput(os.Stdout)
		runGenericCommand(networkCmd, CommandNetwork, add_args, internalLogger)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}

	return nil
}

func isValidPath(path string) bool {
	// Check if it's a drive letter
	if (len(path) == 1) || (len(path) > 1 && len(path) < 4 && string([]rune(path)[1]) == ":") {
		return true
	}
	// Check if it's a network path
	if len(path) > 2 && (path[0:2] == "\\" || path[0:2] == "//") {
		return true
	}
	// Check if it's a file or folder that exists
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func ShowLastHistory(count int) {
	historyFile := "history.json"

	if _, err := os.Stat(historyFile); os.IsNotExist(err) {
		fmt.Println("No history found")
		return
	}

	data, err := os.ReadFile(historyFile)
	if err != nil {
		fmt.Printf("Error reading history: %v\n", err)
		return
	}

	var history []HistoryEntry
	if err := json.Unmarshal(data, &history); err != nil {
		fmt.Printf("Error parsing history: %v\n", err)
		return
	}

	if len(history) == 0 {
		fmt.Println("No history entries found")
		return
	}

	start := len(history) - count
	if start < 0 {
		start = 0
	}

	fmt.Printf("Last %d history entries:\n\n", len(history)-start)

	for i, entry := range history[start:] {
		num := start + i + 1
		status := "✓"
		if !entry.Success {
			status = "✗"
		}

		timeStr := entry.Timestamp.Format("15:04:05")
		cmd := entry.Command
		if entry.Target != "" {
			cmd += " " + entry.Target
		}
		if entry.Operation != "" && entry.Operation != "info" {
			cmd += " " + entry.Operation
		}

		if params, ok := entry.Parameters["args"].([]interface{}); ok && len(params) > 2 {
			for _, p := range params[2:] {
				if str, ok := p.(string); ok && str != "hist" && str != "history" {
					cmd += " " + str
				}
			}
		}

		// Fallback for empty commands - try to reconstruct from args
		if cmd == "" && entry.Parameters != nil {
			if params, ok := entry.Parameters["args"].([]interface{}); ok && len(params) > 0 {
				var parts []string
				for _, p := range params {
					if str, ok := p.(string); ok && str != "hist" && str != "history" {
						parts = append(parts, str)
					}
				}
				cmd = strings.Join(parts, " ")
			}
		}

		fmt.Printf("[%d] %s %s %s (%s)\n", num, status, timeStr, cmd, entry.Duration)

		if !entry.Success && entry.ErrorMsg != "" {
			fmt.Printf("    Error: %s\n", entry.ErrorMsg)
		}

		if entry.Success && len(entry.Results) > 0 {
			var details []string
			if size, ok := entry.Results["totalSize"].(string); ok {
				details = append(details, "Size: "+size)
			}
			if files, ok := entry.Results["fileCount"].(float64); ok {
				details = append(details, fmt.Sprintf("Files: %.0f", files))
			}
			if speed, ok := entry.Results["uploadSpeed"].(string); ok {
				details = append(details, "Speed: "+speed)
			}
			if totalCmds, ok := entry.Results["totalCommands"].(float64); ok {
				if successCmds, ok2 := entry.Results["successfulCommands"].(float64); ok2 {
					details = append(details, fmt.Sprintf("Batch: %.0f/%.0f", successCmds, totalCmds))
				}
			}

			if len(details) > 0 {
				fmt.Printf("    %s\n", strings.Join(details, ", "))
			}
		}

		if i < len(history[start:])-1 {
			fmt.Println()
		}
	}
}

func main() {
	args := os.Args

	// Initialize history logger
	historyLogger := NewHistoryLogger(args)
	defer historyLogger.Finish()

	for i := range args {
		args[i] = strings.ToLower(args[i])
	}

	if len(args) < 2 || contains(list_fo_flags_for_help, args[1]) {
		fmt.Println(usage)
		return
	}

	var command string
	var add_args []string

	if contains(list_of_flags_for_all, args[1]) {
		command = args[1]
		add_args = args[2:]
	} else {
		firstArg := args[1]

		// For drive C can be used as "C:" or "C:\"
		if (len(firstArg) == 1) || (len(firstArg) > 1 && len(firstArg) < 4 && string([]rune(firstArg)[1]) == ":") {
			if len(firstArg) == 1 {
				args[1] += ":"
			}

			command = "device"
			add_args = args[1:]
		} else {
			if len(firstArg) > 2 && (firstArg[0:2] == "\\" || firstArg[0:2] == "//") {
				command = "network"
				add_args = args[1:]
			} else {
				// Check if args[1] is an existing file or folder
				if info, err := os.Stat(args[1]); err == nil {
					if info.IsDir() {
						command = "folder"
						add_args = args[1:]
					} else {
						command = "file"
						add_args = args[1:]
					}
				} else {
					command = os.Args[1]
					add_args = args[2:]
				}
			}
		}
	}

	runNetworkCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandNetwork, add_args, historyLogger)
	}

	runDeviceCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandDevice, add_args, historyLogger)
	}

	runFolderCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandFolder, add_args, historyLogger)
	}

	runFileCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandFile, add_args, historyLogger)
	}

	switch {
	case contains(list_of_flags_for_device, command):
		deviceCmd := flag.NewFlagSet("device", flag.ExitOnError)
		runDeviceCommand(deviceCmd)
	case contains(list_of_flags_for_folder, command):
		folderCmd := flag.NewFlagSet("folder", flag.ExitOnError)
		runFolderCommand(folderCmd)
	case contains(list_of_flags_for_file, command):
		fileCmd := flag.NewFlagSet("file", flag.ExitOnError)
		runFileCommand(fileCmd)
	case contains(list_of_flags_for_network, command):
		networkCmd := flag.NewFlagSet("network", flag.ExitOnError)
		runNetworkCommand(networkCmd)
	case contains(list_of_flags_for_from, command):
		if len(add_args) < 1 {
			fmt.Fprintf(os.Stderr, "Error: Missing file path for 'from' command\n")
			os.Exit(1)
		}
		executeFromFile(add_args[0], historyLogger)
	case contains(list_of_flags_for_hist, command):
		ShowLastHistory(10)
		return
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}

	bue_message := "\n" + time.Now().Format("2006-01-02 15:04:05") + " sza@ukr.net " + version
	fmt.Print(bue_message)
}
