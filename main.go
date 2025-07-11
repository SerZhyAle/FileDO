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

	"filedo/helpers"
)

// the version collected from the current datetime in format YYMMDDHHMM
const version = "2507111111"

var start_time time.Time

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

var usage = fmt.Sprintf(`
═══════════════════════════════════════════════════════════════════════════════
                            FileDO v%s
                    Advanced File & Storage Operations Tool
                           Created by sza@ukr.net
═══════════════════════════════════════════════════════════════════════════════

OVERVIEW:
  FileDO is a comprehensive tool for testing, analyzing, and managing files on
  devices, folders, and network paths. It specializes in storage capacity
  verification, performance testing, and secure data wiping.

BASIC USAGE:
  filedo.exe <target> [operation] [options]
  filedo.exe <command> <target> [operation] [options]

═══════════════════════════════════════════════════════════════════════════════
DEVICE OPERATIONS (Hard drives, USB drives, SD cards)
═══════════════════════════════════════════════════════════════════════════════

Information & Analysis:
  filedo.exe C:                    → Show detailed device information
  filedo.exe device D: info        → Show detailed device information  
  filedo.exe device E: short       → Show brief device summary

Performance Testing:
  filedo.exe C: speed 100          → Test write speed with 100MB file
  filedo.exe device D: speed max   → Test write speed with 10GB file
  filedo.exe device E: speed 500 short → Quick speed test (results only)
  filedo.exe device F: speed 1000 nodel → Test but keep the test file

Capacity & Integrity Testing:
  filedo.exe C: test               → Test for fake capacity (100 files, 1%% each)
  filedo.exe device D: test del    → Test capacity and auto-delete files
  
Space Management:
  filedo.exe C: fill 500           → Fill device with 500MB files until full
  filedo.exe device D: fill 1000 del → Fill and auto-delete (secure wipe)
  filedo.exe device E: clean       → Delete all test files (FILL_*, speedtest_*)

File Organization:
  filedo.exe C: check-duplicates   → Find duplicate files on the device
  filedo.exe device D: cd          → Short form of check-duplicates command
  filedo.exe C: cd old move E:\Dups → Move older duplicate files to E:\Dups
  filedo.exe device D: cd del new  → Delete newer duplicate files (order doesn't matter)
  filedo.exe device D: cd del old  → Delete older duplicate files (flexible order)
  filedo.exe cd from list dups.lst del new → Process duplicates from saved list file

═══════════════════════════════════════════════════════════════════════════════
FOLDER OPERATIONS (Local directories)
═══════════════════════════════════════════════════════════════════════════════

Information & Analysis:
  filedo.exe .                     → Show current folder information
  filedo.exe C:\Temp info          → Show detailed folder information
  filedo.exe folder D:\Data short  → Show brief folder summary

Performance Testing:
  filedo.exe C:\Temp speed 100     → Test folder write speed with 100MB
  filedo.exe folder D:\Data speed max → Test with 10GB file
  filedo.exe folder . speed 200 short → Quick test (results only)

Capacity Testing:
  filedo.exe C:\Temp test          → Test folder capacity (100 files)
  filedo.exe folder D:\Data test del → Test and auto-delete files

Space Management:
  filedo.exe C:\Temp fill 1000     → Fill folder with test files
  filedo.exe folder D:\Data fill 500 del → Fill and secure delete
  filedo.exe folder C:\Temp clean  → Clean all test files

File Organization:
  filedo.exe C:\Temp check-duplicates → Find duplicate files in the folder
  filedo.exe folder D:\Data cd     → Short form of check-duplicates command
  filedo.exe folder E:\Data cd move F:\Backup abc → Move alphabetically last duplicates (parameters in any order)
  filedo.exe cd from list my_dups.lst del → Process duplicates from previously saved list

═══════════════════════════════════════════════════════════════════════════════
FILE OPERATIONS (Individual files)
═══════════════════════════════════════════════════════════════════════════════

File Analysis:
  filedo.exe readme.txt            → Show detailed file information
  filedo.exe file data.zip info    → Show detailed file information
  filedo.exe file document.pdf short → Show brief file summary

═══════════════════════════════════════════════════════════════════════════════
NETWORK OPERATIONS (SMB shares, network drives)
═══════════════════════════════════════════════════════════════════════════════

Information & Analysis:
  filedo.exe \\server\share        → Show network path information
  filedo.exe network \\pc\folder info → Detailed network info

Performance Testing:
  filedo.exe \\server\share speed 100 → Test network speed with 100MB
  filedo.exe network \\pc\data speed max → Test with 10GB transfer
  filedo.exe network \\server\temp speed 500 short → Quick network test

Capacity Testing:
  filedo.exe \\server\share test   → Test network storage capacity
  filedo.exe network \\pc\backup test del → Test and auto-cleanup

Space Management:
  filedo.exe \\server\share fill 1000 → Fill network storage
  filedo.exe network \\pc\temp clean → Clean test files from network

File Organization:
  filedo.exe \\server\share check-duplicates → Find duplicate files on network share
  filedo.exe network \\pc\temp cd → Short form of check-duplicates command
  filedo.exe network \\server\share cd xyz del → Delete alphabetically first duplicates (any param order)

═══════════════════════════════════════════════════════════════════════════════
BATCH OPERATIONS & HISTORY
═══════════════════════════════════════════════════════════════════════════════

Batch Processing:
  filedo.exe from commands.txt     → Execute commands from file
  filedo.exe batch script.lst      → Same as 'from' command
  
History & Monitoring:
  filedo.exe hist                  → Show last 10 operations
  filedo.exe history               → Show command history

═══════════════════════════════════════════════════════════════════════════════
COMMAND OPTIONS & MODIFIERS
═══════════════════════════════════════════════════════════════════════════════

Output Control:
  short, s        → Show brief/summary output only
  info, i         → Show detailed information (default)

File Management:
  del, delete, d  → Auto-delete test files after successful operation
  nodel, nodelete → Keep test files on target (don't delete)
  clean, cln, c   → Delete all existing test files

Size Specifications:
  <number>        → Size in megabytes (e.g., 100, 500, 1000)
  max             → Use maximum size (10GB for speed tests)

═══════════════════════════════════════════════════════════════════════════════
PRACTICAL EXAMPLES
═══════════════════════════════════════════════════════════════════════════════

Quick Device Check:
  filedo.exe D: short              → Fast overview of drive D:

USB Drive Verification:
  filedo.exe E: test del           → Check if USB is fake, auto-cleanup

Network Speed Test:
  filedo.exe \\server\backup speed max short → Max speed test, brief results

Process Saved Duplicate List:
  filedo.exe cd from list my_dups.lst del new → Delete newer duplicate files from list

Secure Space Wiping:
  filedo.exe C: fill 5000 del      → Fill 5GB then secure delete (data recovery prevention)

Batch Testing Multiple Locations:
  Create file 'test_all.txt' with:
    # Test script for multiple devices
    device C: info
    device D: test del
    folder C:\Temp speed 100
    network \\server\share info
  
  Run: filedo.exe from test_all.txt

═══════════════════════════════════════════════════════════════════════════════
IMPORTANT NOTES
═══════════════════════════════════════════════════════════════════════════════

• Fake Capacity Detection: The 'test' command creates 100 files, each 1%% of
  total capacity, to detect counterfeit storage devices that report false sizes.
  Uses optimized smart verification - full verification for first 5 files and
  every 10th file, fast header-only checks for recent files between milestones.

• Secure Wiping: Use 'fill <size> del' to overwrite free space and prevent
  recovery of previously deleted files.

• Test Files: Operations create files named FILL_#####_ddHHmmss.tmp and
  speedtest_*.txt. Use 'clean' to remove them.

• Batch Files: Commands in batch files support # comments and empty lines.
  Each line should contain one complete filedo command.

• Path Detection: FileDO automatically detects path types:
  - C:, D:, etc. → Device operations
  - \\server\share → Network operations  
  - C:\folder, ./dir → Folder operations
  - file.txt → File operations

• History: All operations are logged. Use 'hist' flag with any command to
  enable detailed history logging: filedo.exe C: info hist

Help & Support:
  filedo.exe ?                     → Show this help
  filedo.exe help                  → Show this help

═══════════════════════════════════════════════════════════════════════════════`, version)

var list_of_flags_for_device = []string{"device", "dev", "disk", "d"}
var list_of_flags_for_folder = []string{"folder", "fold", "dir", "fld"}
var list_of_flags_for_file = []string{"file", "fl", "f"}
var list_of_flags_for_network = []string{"network", "net", "n"}
var list_of_flags_for_from = []string{"from", "batch", "script"}
var list_of_flags_for_hist = []string{"hist", "history"}
var list_of_flags_for_duplicates = []string{"check-duplicates", "cd", "duplicate"}
var list_fo_flags_for_help = []string{"?", "help", "h", "?"}
var list_of_flags_for_all = append(append(append(append(append(append(list_of_flags_for_device, list_of_flags_for_folder...), list_of_flags_for_file...), list_of_flags_for_network...), list_of_flags_for_from...), list_of_flags_for_hist...), list_of_flags_for_duplicates...)

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func executeFromFile(filePath string, historyLogger *HistoryLogger) error {
	historyLogger.SetCommand("from", filePath, "batch")

	file, err := os.Open(filePath)
	if err != nil {
		historyLogger.SetError(fmt.Errorf("failed to open file: %w", err))
		return fmt.Errorf("cannot open file '%s': %w", filePath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	commandCount := 0
	successCount := 0
	var errors []string

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
			errorMsg := fmt.Sprintf("Command %d failed: %v", commandCount, err)
			fmt.Printf("%s\n", errorMsg)
			errors = append(errors, errorMsg)
		} else {
			successCount++
		}
	}

	if err := scanner.Err(); err != nil {
		readErr := fmt.Errorf("error reading file: %w", err)
		historyLogger.SetError(readErr)
		return readErr
	}

	fmt.Printf("\nBatch execution complete: %d/%d commands succeeded\n", successCount, commandCount)

	historyLogger.SetResult("totalCommands", commandCount)
	historyLogger.SetResult("successfulCommands", successCount)

	if len(errors) > 0 {
		batchErr := fmt.Errorf("batch execution failed: %d out of %d commands failed", len(errors), commandCount)
		historyLogger.SetError(batchErr)
		return batchErr
	}

	historyLogger.SetSuccess()
	return nil
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
			// Check if the path exists
			if info, err := os.Stat(args[0]); err == nil {
				if info.IsDir() {
					command = "folder"
					add_args = lowerArgs
				} else {
					command = "file"
					add_args = lowerArgs
				}
			} else {
				// Path doesn't exist - determine if it looks like a folder or file path
				// and provide a more helpful message
				if strings.HasSuffix(args[0], "/") || strings.HasSuffix(args[0], "\\") {
					fmt.Printf("Info: The folder \"%s\" does not exist.\n", args[0])
					return nil
				} else if strings.Contains(args[0], ".") {
					fmt.Printf("Info: The file \"%s\" does not exist.\n", args[0])
					return nil
				} else {
					fmt.Printf("Info: The path \"%s\" does not exist.\n", args[0])
					return nil
				}
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
	// For batch processing, we also consider paths that might not exist yet as valid syntax
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
	start_time = time.Now()

	hi_message := "\n" + start_time.Format("2006-01-02 15:04:05") + " sza@ukr.net " + version + "\n"
	fmt.Print(hi_message)

	// Ensure bue_message is always printed, even on errors or panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "\nPanic: %v\n", r)
		}
		bue_message := "\n Finish:" + time.Now().Format("2006-01-02 15:04:05") + ", Duration: " + time.Since(start_time).String() + "\n"
		fmt.Print(bue_message)
	}()

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

	// Check for direct cd from command (without device/folder/network context)
	if args[1] == "cd" && len(args) > 2 && args[2] == "from" {
		historyLogger.SetCommand("cd", "from", "check-duplicates")
		// Pass all arguments after "cd", i.e. "from list file.lst [options]"
		err := helpers.CheckDuplicatesFromFile(args[2:])
		if err != nil {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		historyLogger.SetSuccess()
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
					// Path doesn't exist - try to determine what it might be
					if strings.HasPrefix(args[1], "folder") || strings.HasPrefix(args[1], "dir") {
						command = args[1]
						add_args = args[2:]
					} else if strings.HasSuffix(args[1], "/") || strings.HasSuffix(args[1], "\\") {
						fmt.Printf("Info: The folder \"%s\" does not exist.\n", args[1])
						return
					} else if strings.Contains(args[1], ".") {
						fmt.Printf("Info: The file \"%s\" does not exist.\n", args[1])
						return
					} else {
						// Could be a command or non-existent path
						command = os.Args[1]
						add_args = args[2:]
					}
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
		if err := executeFromFile(add_args[0], historyLogger); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case contains(list_of_flags_for_hist, command):
		ShowLastHistory(10)
		return
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}
}
