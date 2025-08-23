// sza250707
// sza250712
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

// the version collected from the current datetime in format YYMMDDHHMM
const version = "2508230100"

var start_time time.Time
var globalInterruptHandler *InterruptHandler

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

type HistoryLogger struct {
	enabled      bool
	startTime    time.Time
	entry        HistoryEntry
	originalArgs []string
	historyFile  string
	canWriteHist bool
}

func NewHistoryLogger(args []string) *HistoryLogger {
	// Check for nohist/no_history flags to disable history logging
	enabled := true
	for _, arg := range args {
		if arg == "nohist" || arg == "no_history" {
			enabled = false
			break
		}
	}

	historyFile := "history.json"
	canWriteHist := true

	// Check if we can write to the history file
	if enabled {
		// Try to open the file for writing to check permissions
		if file, err := os.OpenFile(historyFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644); err != nil {
			canWriteHist = false
			// Don't disable entirely, just note we can't write
		} else {
			file.Close()
		}
	}

	// Create full command string from args
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

	// Generate result summary if not already set
	if hl.entry.ResultSummary == "" && hl.entry.Success {
		hl.entry.ResultSummary = hl.generateResultSummary()
	}

	saveToHistory(hl.entry)
}

// generateResultSummary creates a short summary of the operation results
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
	if totalCmds, ok := hl.entry.Results["totalCommands"].(float64); ok {
		if successCmds, ok2 := hl.entry.Results["successfulCommands"].(float64); ok2 {
			details = append(details, fmt.Sprintf("Batch: %.0f/%.0f", successCmds, totalCmds))
		}
	}
	if duplicates, ok := hl.entry.Results["duplicatesFound"].(float64); ok {
		details = append(details, fmt.Sprintf("Duplicates: %.0f", duplicates))
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

var usage = fmt.Sprintf(`
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

File Analysis:
  filedo.exe readme.txt            → Show detailed file information
  filedo.exe file data.zip info    → Show detailed file information
  filedo.exe file document.pdf short → Show brief file summary

═══════════════════════════════════════════════════════════════════════════════
NETWORK OPERATIONS (SMB shares, network drives)

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
COPY & WIPE OPERATIONS

Smart Copy (Automatic Strategy Selection):
  filedo.exe copy D:\Source E:\Target     → Smart copy with automatic strategy selection (15s analysis)
  filedo.exe folder D:\Source copy E:\Target → Copy folder with auto-optimized strategy
  filedo.exe network \\server\share copy C:\Local → Copy network share with optimal method
  filedo.exe file document.txt copy backup.txt → Copy individual file with best approach

Manual Copy Strategies:
  filedo.exe device C: copy D:\Backup     → Copy device contents to folder

High-Speed Copy (Optimized for large datasets):
  filedo.exe fastcopy D:\LargeFolder E:\Backup → Optimized parallel copy
  filedo.exe fcopy D:\SlowHDD F:\FastSSD    → Fast copy with adaptive buffers
  filedo.exe fc \\server\data C:\Local      → Multi-threaded network copy

Synchronized Copy (Debug mode for I/O analysis):
  filedo.exe synccopy D:\Source E:\Target   → Single-threaded sync copy (reduced caching)  
  filedo.exe scopy D:\HDD1 D:\HDD2         → For analyzing real read/write speeds
  filedo.exe sc \\server\data C:\Local     → Bypass cache effects in copy operations

Balanced Copy (Optimized for HDD-to-HDD):
  filedo.exe balanced D:\Source E:\Target   → 4 threads, 64MB buffers, optimized for HDD
  filedo.exe bcopy D:\HDD1 D:\HDD2         → Balanced performance for mechanical drives
  filedo.exe bc \\server\data C:\Local     → Optimal for network-to-HDD operations

Maximum Performance Copy (Aggressive CPU utilization):
  filedo.exe maxcopy D:\Source E:\Target    → 16 threads, 128MB buffers, maximum speed
  filedo.exe mcopy D:\LargeData E:\Fast     → Turbo mode for maximum system utilization
  filedo.exe turbo \\server\data C:\Local  → Maximum parallelism for fastest possible copy

Fast Content Wiping:
  filedo.exe folder D:\Temp wipe          → Fast wipe folder contents (delete & recreate)
  filedo.exe device D: wipe               → Wipe device contents (standard method for system folders)
  filedo.exe network \\server\temp wipe   → Wipe network folder contents
  filedo.exe folder C:\Cache w            → Short form of wipe command

═══════════════════════════════════════════════════════════════════════════════
BATCH OPERATIONS & HISTORY

Batch Processing:
  filedo.exe from commands.txt     → Execute commands from file
  filedo.exe batch script.lst      → Same as 'from' command
  
History & Monitoring:
  filedo.exe hist                  → Show last 10 operations
  filedo.exe history               → Show command history

═══════════════════════════════════════════════════════════════════════════════
COMMAND OPTIONS & MODIFIERS

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

Fast Backup & Cleanup:
  filedo.exe folder C:\ImportantData copy D:\Backup → Copy folder with progress tracking
  filedo.exe device D: copy \\server\archive → Copy entire device to network storage
  filedo.exe fastcopy D:\SlowHDD E:\FastSSD → Optimized parallel copy for large datasets
  filedo.exe fc \\NAS\Photos C:\LocalBackup → High-speed copy from slow network/extFAT drives
  filedo.exe synccopy D:\HDD1 D:\HDD2       → Synchronized copy for diagnosing I/O speeds
  filedo.exe folder D:\TempFiles wipe      → Fast wipe temporary folder (delete & recreate)
  filedo.exe network \\server\temp w       → Quick wipe of network temp folder

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

• System Drive Protection: Write operations on C: are automatically redirected
  to safe temporary locations (%TEMP%\FileDO_Operations) with user confirmation.
  Environment variables:
  - FILEDO_DISABLE_REDIRECT=1 → Disable redirection (advanced users only)
  - FILEDO_AUTO_CONFIRM=1 → Auto-confirm redirections (for scripts/testing)

Help & Support:
  filedo.exe ?                     → Show this help
  filedo.exe help                  → Show this help
`, version)

var list_of_flags_for_device = []string{"device", "dev", "disk", "d"}
var list_of_flags_for_folder = []string{"folder", "fold", "dir", "fld"}
var list_of_flags_for_file = []string{"file", "fl", "f"}
var list_of_flags_for_network = []string{"network", "net", "n"}
var list_of_flags_for_from = []string{"from", "batch", "script"}
var list_of_flags_for_hist = []string{"hist", "history"}
var list_of_flags_for_duplicates = []string{"check-duplicates", "cd", "duplicate"}
var list_of_flags_for_copy = []string{"copy", "cp"}
var list_of_flags_for_fastcopy = []string{"fastcopy", "fcopy", "fc"}
var list_of_flags_for_synccopy = []string{"synccopy", "scopy", "sc"}
var list_of_flags_for_balanced = []string{"balanced", "bcopy", "bc"}
var list_of_flags_for_maxcopy = []string{"maxcopy", "mcopy", "max", "turbo"}
var list_of_flags_for_wipe = []string{"wipe", "w"}
var list_fo_flags_for_help = []string{"?", "help", "h", "?"}
var list_of_flags_for_all = append(append(append(append(append(append(append(append(append(append(append(append(list_of_flags_for_device, list_of_flags_for_folder...), list_of_flags_for_file...), list_of_flags_for_network...), list_of_flags_for_from...), list_of_flags_for_hist...), list_of_flags_for_duplicates...), list_of_flags_for_copy...), list_of_flags_for_fastcopy...), list_of_flags_for_synccopy...), list_of_flags_for_balanced...), list_of_flags_for_maxcopy...), list_of_flags_for_wipe...)

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
	var add_args []string

	// Convert only commands to lowercase for comparison, preserve paths
	lowerArgs := make([]string, len(args))
	copy(lowerArgs, args)
	
	// Convert first argument (command) to lowercase for comparison
	if len(lowerArgs) >= 1 {
		arg := lowerArgs[0]
		if !strings.Contains(arg, ":") && !strings.Contains(arg, "\\") && !strings.Contains(arg, "/") && !strings.Contains(arg, ".") {
			lowerArgs[0] = strings.ToLower(lowerArgs[0])
		}
	}
	
	// Convert potential operation arguments to lowercase (but preserve paths)
	for i := 1; i < len(lowerArgs); i++ {
		arg := lowerArgs[i]
		// Only convert to lowercase if it doesn't look like a path
		if !strings.Contains(arg, ":") && !strings.Contains(arg, "\\") && !strings.Contains(arg, "/") && 
		   !strings.Contains(arg, ".") && len(arg) < 20 { // Short non-path arguments
			lowerArgs[i] = strings.ToLower(lowerArgs[i])
		}
	}

	firstArg := args[0] // Use original arg to preserve case in paths

	if contains(list_of_flags_for_all, lowerArgs[0]) {
		command = lowerArgs[0]
		add_args = args[1:] // Use original args to preserve paths
	} else {
		// Auto-detect based on path
		firstArgLower := strings.ToLower(firstArg) // Only for comparison
		if len(firstArgLower) > 0 && ((len(firstArgLower) == 1) || (len(firstArgLower) > 1 && len(firstArgLower) < 4 && string([]rune(firstArgLower)[1]) == ":")) {
			if len(firstArg) == 1 {
				args[0] += ":" // Modify original
			}
			command = "device"
			add_args = args // Use original args
		} else if len(firstArg) > 2 && (firstArg[0:2] == "\\" || firstArg[0:2] == "//") {
			command = "network"
			add_args = args // Use original args
		} else {
			// Check if the path exists
			if info, err := os.Stat(args[0]); err == nil {
				if info.IsDir() {
					command = "folder"
					add_args = args // Use original args
				} else {
					command = "file"
					add_args = args // Use original args
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
	case contains(list_of_flags_for_duplicates, command):
		// Handle check-duplicates command
		if len(args) > 1 && strings.ToLower(args[1]) == "from" {
			internalLogger.SetCommand(command, "from", "check-duplicates")
			err := handleCheckDuplicatesCommand(args)
			if err != nil {
				internalLogger.SetError(err)
				return err
			}
			internalLogger.SetSuccess()
		} else {
			return fmt.Errorf("invalid format for duplicate command: %s", strings.Join(args, " "))
		}
	case contains(list_of_flags_for_hist, command):
		// Handle history command
		internalLogger.SetCommand(command, "", "history")
		handleHistoryCommand(args)
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_copy, command):
		// Handle intelligent copy command with automatic strategy selection
		if len(args) < 3 {
			return fmt.Errorf("copy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "smart-copy")
		err := handleSmartCopyCommand(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_fastcopy, command):
		// Handle fast copy command
		if len(args) < 3 {
			return fmt.Errorf("fastcopy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "fastcopy")
		err := handleFastCopyCommand(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_synccopy, command):
		// Handle synchronized copy command  
		if len(args) < 3 {
			return fmt.Errorf("synccopy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "synccopy")
		err := handleSyncCopyCommand(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_balanced, command):
		// Handle balanced copy command optimized for HDD-to-HDD
		if len(args) < 3 {
			return fmt.Errorf("balanced command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "balanced")
		err := handleBalancedCopyCommand(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_maxcopy, command):
		// Handle maximum performance copy command  
		if len(args) < 3 {
			return fmt.Errorf("maxcopy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "maxcopy")
		err := handleMaxCopyCommand(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_from, command):
		// Handle from file command (nested call)
		if len(args) < 2 {
			return fmt.Errorf("missing file path for 'from' command")
		}
		internalLogger.SetCommand(command, args[1], "batch")
		err := executeFromFile(args[1], internalLogger)
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	default:
		return fmt.Errorf("unknown command: %s", command)
	}

	return nil
}

// handleFastCopyCommand handles the fastcopy command with optimized performance
func handleFastCopyCommand(sourcePath, targetPath string) error {
	return FastCopy(sourcePath, targetPath)
}

// handleSyncCopyCommand handles the synccopy command with synchronized I/O
func handleSyncCopyCommand(sourcePath, targetPath string) error {
	return FastCopySync(sourcePath, targetPath)
}

// handleBalancedCopyCommand handles the balanced copy command optimized for HDD-to-HDD
func handleBalancedCopyCommand(sourcePath, targetPath string) error {
	return FastCopyBalanced(sourcePath, targetPath)
}

// handleMaxCopyCommand handles the maxcopy command with maximum CPU utilization
func handleMaxCopyCommand(sourcePath, targetPath string) error {
	return FastCopyMax(sourcePath, targetPath)
}

// handleSmartCopyCommand analyzes source/target and selects optimal copy strategy  
func handleSmartCopyCommand(sourcePath, targetPath string) error {
	// Perform strategy analysis (max 15 seconds)
	analysis, err := AnalyzeCopyStrategy(sourcePath, targetPath)
	if err != nil {
		return fmt.Errorf("failed to analyze copy strategy: %v", err)
	}
	
	// Execute selected strategy
	return ExecuteSelectedStrategy(analysis, sourcePath, targetPath)
}

func isValidPath(path string) bool {
	// Check if it's a drive letter
	if len(path) > 0 && ((len(path) == 1) || (len(path) > 1 && len(path) < 4 && string([]rune(path)[1]) == ":")) {
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

		// Use full command if available, otherwise reconstruct from parts
		var cmdDisplay string
		if entry.FullCommand != "" {
			cmdDisplay = entry.FullCommand
			// Remove "filedo" prefix if present
			if strings.HasPrefix(cmdDisplay, "filedo ") {
				cmdDisplay = cmdDisplay[7:]
			}
			if strings.HasPrefix(cmdDisplay, "filedo.exe ") {
				cmdDisplay = cmdDisplay[11:]
			}
			if strings.HasPrefix(cmdDisplay, "./filedo.exe ") {
				cmdDisplay = cmdDisplay[13:]
			}
		} else {
			// Fallback to old reconstruction method
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
			cmdDisplay = cmd
		}

		fmt.Printf("[%d] %s %s %s (%s)\n", num, status, timeStr, cmdDisplay, entry.Duration)

		if !entry.Success && entry.ErrorMsg != "" {
			fmt.Printf("    Error: %s\n", entry.ErrorMsg)
		}

		if entry.Success {
			// Use result summary if available, otherwise show individual results
			if entry.ResultSummary != "" {
				fmt.Printf("    %s\n", entry.ResultSummary)
			} else if len(entry.Results) > 0 {
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
				if duplicates, ok := entry.Results["duplicatesFound"].(float64); ok {
					details = append(details, fmt.Sprintf("Duplicates: %.0f", duplicates))
				}
				if freed, ok := entry.Results["spaceFreed"].(string); ok {
					details = append(details, "Freed: "+freed)
				}

				if len(details) > 0 {
					fmt.Printf("    %s\n", strings.Join(details, ", "))
				}
			}
		}

		if i < len(history[start:])-1 {
			fmt.Println()
		}
	}
}

func main() {
	start_time = time.Now()

	// Initialize global interrupt handler first
	globalInterruptHandler = NewInterruptHandler()

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
	historyLogger := NewHistoryLogger(os.Args)
	defer historyLogger.Finish()

	// Convert only the first few arguments (commands/flags) to lowercase, preserve paths
	lowerArgs := make([]string, len(args))
	copy(lowerArgs, args)
	
	// Convert first argument (command) to lowercase for comparison
	if len(lowerArgs) >= 2 {
		lowerArgs[1] = strings.ToLower(lowerArgs[1])
	}
	
	// Convert potential second command/flag to lowercase
	if len(lowerArgs) >= 3 {
		// Check if it looks like a command/flag, not a path
		arg := lowerArgs[2]
		if !strings.Contains(arg, ":") && !strings.Contains(arg, "\\") && !strings.Contains(arg, "/") && !strings.Contains(arg, ".") {
			lowerArgs[2] = strings.ToLower(lowerArgs[2])
		}
	}
	
	// Convert operation arguments to lowercase (but preserve paths)
	for i := 3; i < len(lowerArgs); i++ {
		arg := lowerArgs[i]
		// Only convert to lowercase if it doesn't look like a path
		if !strings.Contains(arg, ":") && !strings.Contains(arg, "\\") && !strings.Contains(arg, "/") && 
		   !strings.Contains(arg, ".") && len(arg) < 20 { // Short non-path arguments
			lowerArgs[i] = strings.ToLower(lowerArgs[i])
		}
	}

	if len(args) < 2 || contains(list_fo_flags_for_help, lowerArgs[1]) {
		fmt.Println(usage)
		return
	}

	// Check for direct cd from command (without device/folder/network context)
	if contains(list_of_flags_for_duplicates, lowerArgs[1]) && len(args) > 2 && lowerArgs[2] == "from" {
		historyLogger.SetCommand(lowerArgs[1], "from", "check-duplicates")
		// Pass original command to handler (preserve case in paths)
		err := handleCheckDuplicatesCommand(args[1:])
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

	if contains(list_of_flags_for_all, lowerArgs[1]) {
		command = lowerArgs[1]
		add_args = args[2:] // Use original args to preserve case in paths
		
		// Special handling for short copy command 'c'
		// If 'c' is used with 3+ arguments, treat it as copy
		if lowerArgs[1] == "c" && len(args) >= 4 {
			command = "copy"
			add_args = args[2:] // Use original args
		}
	} else {
		firstArg := args[1] // Use original arg to preserve case in paths

		// For drive C can be used as "C:" or "C:\"
		if len(firstArg) > 0 && ((len(firstArg) == 1) || (len(firstArg) > 1 && len(firstArg) < 4 && string([]rune(firstArg)[1]) == ":")) {
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
					if strings.HasPrefix(lowerArgs[1], "folder") || strings.HasPrefix(lowerArgs[1], "dir") {
						command = lowerArgs[1]
						add_args = args[2:]
					} else if strings.HasSuffix(args[1], "/") || strings.HasSuffix(args[1], "\\") {
						fmt.Printf("Info: The folder \"%s\" does not exist.\n", args[1])
						return
					} else if strings.Contains(args[1], ".") {
						fmt.Printf("Info: The file \"%s\" does not exist.\n", args[1])
						return
					} else {
						// Could be a command or non-existent path
						command = lowerArgs[1] // Use lowercase for command comparison
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
		handleHistoryCommand(os.Args[1:])
		return
	case contains(list_of_flags_for_copy, command):
		if len(add_args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Copy command requires source and target paths\n")
			os.Exit(1)
		}
		historyLogger.SetCommand(command, add_args[0], "smart-copy")
		if err := handleSmartCopyCommand(add_args[0], add_args[1]); err != nil {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_fastcopy, command):
		if len(add_args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Fast copy command requires source and target paths\n")
			os.Exit(1)
		}
		historyLogger.SetCommand(command, add_args[0], "fastcopy")
		if err := handleFastCopyCommand(add_args[0], add_args[1]); err != nil {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_synccopy, command):
		if len(add_args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Sync copy command requires source and target paths\n")
			os.Exit(1)
		}
		historyLogger.SetCommand(command, add_args[0], "synccopy")
		if err := handleSyncCopyCommand(add_args[0], add_args[1]); err != nil {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_balanced, command):
		if len(add_args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Balanced copy command requires source and target paths\n")
			os.Exit(1)
		}
		historyLogger.SetCommand(command, add_args[0], "balanced")
		if err := handleBalancedCopyCommand(add_args[0], add_args[1]); err != nil {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_maxcopy, command):
		if len(add_args) < 2 {
			fmt.Fprintf(os.Stderr, "Error: Max copy command requires source and target paths\n")
			os.Exit(1)
		}
		historyLogger.SetCommand(command, add_args[0], "maxcopy")
		if err := handleMaxCopyCommand(add_args[0], add_args[1]); err != nil {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		historyLogger.SetSuccess()
		return
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}
}
