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
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// This version string is automatically set by the build script
var version = "dev"

var start_time time.Time
var globalInterruptHandler *InterruptHandler

// machineStopChannel records that whoever started this process gave a
// --stop-file, so a graceful "stop now" can arrive without a console. A
// reveal reads it to know that the visible "remove it now" of spec 8.2 exists
// as a window somewhere even though stdin is not a terminal.
var machineStopChannel bool

// noHistoryFlag is --no-history: no history.json for this run.
var noHistoryFlag bool

func extractGlobalFlags(args []string) (filtered []string, eventsPath string, stopFilePath string, noUI bool, pause bool) {
	filtered = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--events" && i+1 < len(args) {
			eventsPath = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--events=") {
			eventsPath = strings.TrimPrefix(arg, "--events=")
		} else if arg == "--stop-file" && i+1 < len(args) {
			stopFilePath = args[i+1]
			i++
		} else if strings.HasPrefix(arg, "--stop-file=") {
			stopFilePath = strings.TrimPrefix(arg, "--stop-file=")
		} else if arg == "--no-ui" {
			noUI = true
		} else if arg == "--pause" || arg == "-pause" || arg == "/pause" {
			pause = true
		} else if arg == "--no-history" {
			noHistoryFlag = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	return filtered, eventsPath, stopFilePath, noUI, pause
}

// pauseOnExit is the --pause flag, and holdConsoleIfAsked is what it buys: a
// window that is still on screen when the work is over.
//
// It exists for the Explorer menu (SP-0005 9.1). Explorer gives a console
// verb its own window and closes it the instant the process ends, so
// "Info" and "Check" would print their answer into a window nobody can read.
// The flag is stripped in extractGlobalFlags before any command sees it, so
// no verb can mistake it for an operation word or a bare password.
//
// Once, because the exit paths overlap: the deferred history flush calls this
// before os.Exit (which would skip every remaining defer), and the normal
// path calls it again after the finish line. A second call is a no-op.
//
// A run whose stdin is not a console - a batch file, a redirected run, the
// GUI's child process - waits for nothing: there would be no one to press the
// key, and the wait would be a hang.
var (
	pauseOnExit bool
	pauseOnce   sync.Once

	afterHoldMu sync.Mutex
	afterHold   []func()
)

func holdConsoleIfAsked() {
	if pauseOnExit {
		pauseOnce.Do(func() {
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return
			}
			fmt.Print("\nPress Enter to close this window.. ")
			_, _ = readConsoleLine()
		})
	}
	runAfterConsoleHold()
}

// recoverRunPanic is deferred after finishRun, so it executes first while a
// panic unwinds. That ordering turns a panic into the rule-11 Not proven
// outcome before finishRun writes the channel's final result.
func recoverRunPanic() {
	if r := recover(); r != nil {
		runFailure(fmt.Errorf("panic: %v", r))
		fmt.Fprintf(os.Stderr, "\nPanic: %v\n", r)
	}
}

// willHoldConsole answers the one question a caller needs before it schedules
// work for "when the window closes": whether there will be a wait at all. The
// two conditions are exactly holdConsoleIfAsked's own - the flag, and a
// console to hold - so nothing can be queued behind a pause that never
// happens.
func willHoldConsole() bool {
	return pauseOnExit && term.IsTerminal(int(os.Stdin.Fd()))
}

// addAfterConsoleHold queues a function to run the moment that wait ends,
// immediately before the process exits. SP-0008 4.3 is the only caller: an
// `unsecure start` sandbox is removed when the console window Explorer opened
// for it closes, and on that path this is that instant.
func addAfterConsoleHold(fn func()) {
	afterHoldMu.Lock()
	defer afterHoldMu.Unlock()
	afterHold = append(afterHold, fn)
}

// runAfterConsoleHold drains the queue. It is called from every
// holdConsoleIfAsked - including the ones that did not wait for anything -
// and drains rather than iterates, so a second call is a no-op however the
// exit paths overlap.
func runAfterConsoleHold() {
	afterHoldMu.Lock()
	queued := afterHold
	afterHold = nil
	afterHoldMu.Unlock()
	for i := len(queued) - 1; i >= 0; i-- {
		queued[i]()
	}
}

func findUIExecutable() string {
	if exePath, err := os.Executable(); err == nil {
		dir := filepath.Dir(exePath)
		target := filepath.Join(dir, "filedo_win.exe")
		if _, err := os.Stat(target); err == nil {
			return target
		}
	}
	if _, err := os.Stat("filedo_win.exe"); err == nil {
		return "filedo_win.exe"
	}
	if path, err := exec.LookPath("filedo_win.exe"); err == nil {
		return path
	}
	return ""
}

func launchUI(args ...string) error {
	uiPath := findUIExecutable()
	if uiPath == "" {
		return fmt.Errorf("filedo_win.exe not found")
	}
	cmd := exec.Command(uiPath, args...)
	setDetachedProcess(cmd)
	return cmd.Start()
}

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
	finished     bool
}

func NewHistoryLogger(args []string) *HistoryLogger {
	// Check for nohist/no_history flags to disable history logging
	enabled := !noHistoryFlag
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

	// Create full command string from args. Credential material of the fdsec
	// family is redacted HERE, before anything is written: a password that
	// reaches history.json is on the user's disk permanently (spec 12).
	fullCommand := strings.Join(redactCredentialArgs(args), " ")

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
	if !hl.enabled || hl.finished {
		return
	}
	hl.finished = true

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

var shortUsage = fmt.Sprintf(`
                            FileDO v%s
                    Advanced File & Storage Operations Tool
═══════════════════════════════════════════════════════════════════════════════
BASIC USAGE:
  filedo.exe <target> [operation] [options]

MAIN OPERATIONS:
  ui       → Open the graphical UI shell
  info     → Show device/folder information
  speed    → Test read/write speed 
  test     → Test storage capacity (detect fake devices)
  fill     → Fill storage with test files
  clean    → Delete test files
  cd       → Check for duplicate files
  copy     → Copy files/folders with optimization
  wipe     → Fast wipe folder contents
  secure   → Pack a file into a .fd-sec container behind a password
  unsecure → Restore the original from a .fd-sec container
  reveal   → Open a container in its app, in a sandbox that is swept after
  compare  → Compare directory trees
  check    → Check files for corruption

TARGETS:
  C:, D:   → Device operations (drives)
  C:\path  → Folder operations
  \\pc\sh  → Network share operations
  file.txt → File operations

EXAMPLES:
  filedo.exe ui                   → Open the graphical UI shell
  filedo.exe D: info              → Show drive info
  filedo.exe E: speed 100         → Test speed with 100MB
  filedo.exe F: test del          → Test capacity, auto-cleanup
  filedo.exe C:\temp cd           → Find duplicates
  filedo.exe copy C:\src D:\dst   → Smart copy with optimization
  filedo.exe C:\temp wipe         → Fast wipe folder
  filedo.exe secret.txt secure    → Pack into secret.fd-sec

COPY MODES:
  copy      → Smart auto-detection (recommended)
  fastcopy  → High-speed parallel copy
  safecopy  → Safe mode for damaged drives
  maxcopy   → Maximum performance mode

OPTIONS:
  short     → Brief output only
  del       → Auto-delete test files
  max       → Maximum size (10GB)
  --no-ui   → Suppress GUI launch on empty args (or FILEDO_NO_UI=1)
  --pause   → Wait for Enter before the window closes (what the Explorer menu uses)
  --events <path>    → Write the opt-in JSON Lines event stream for a supervisor
  --stop-file <path> → Stop gracefully when the named file appears
  --precount → copy: count the tree first, for exact totals and ETA (otherwise copying starts at the first file)

EXIT CODES (all non-container verbs):
  0 passed or done, 1 ran and found a defect, 2 could not be verified

MORE INFO:
  filedo.exe help                 → Show detailed help
  filedo.exe /help                → Show detailed help

  GitHub: https://github.com/SerZhyAle/FileDO
`, version)

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
  filedo.exe C: test               → Test for fake capacity (100 files, 95%% of space)
  filedo.exe C: test 1000          → Test with 1000 files (95%% of space)
  filedo.exe device D: test del    → Test capacity and auto-delete files
  filedo.exe device D: test 500 del → Test with 500 files and auto-delete
  filedo.exe D: probe              → Fast probe (raw I/O, requires Administrator, ~1 min)
	filedo.exe D: recover            → Recover drive after probe (chkdsk + optional quick format)
  
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
SECRET FILES (.fd-sec containers)

  A container holds exactly one file. The true name, the real size and the
  timestamps are sealed inside it; only the container's own size and its
  format parameters are visible without the password.

Pack and restore:
  filedo.exe secret.txt secure           → Pack into secret.fd-sec (asks twice)
  filedo.exe secret.txt secure p:hunter2 → Pack with the password on the line
  filedo.exe secret.fd-sec unsecure      → Restore under the sealed true name
  filedo.exe secret.fd-sec unsecure here → Restore into the current folder
  filedo.exe c.fd-sec unsecure start     → Open it in its own program and keep
                                           nothing: the copy lives where only
                                           you can read it and goes when the
                                           window closes
  filedo.exe *.jpg secure p:hunter2      → One container per matched file

Open without unpacking (reveal):
  filedo.exe holiday.fd-sec reveal       → Open in the registered app, then
                                           remove the copy when it is done
  filedo.exe c.fd-sec reveal -keep       → Leave the copy; the next FileDO
                                           start of any kind removes it
  filedo.exe c.fd-sec reveal -rw to x.md → Not a sandbox: restore to a file
                                           you own (what unsecure does)

  The copy lives in %%LOCALAPPDATA%%\FileDO\reveal\<random>\, readable by you
  and the system only, read-only, and marked as coming from elsewhere. It is
  removed when the app lets go of it, or when you say so - the Enter prompt in
  a console, the "remove the copy now" button when the shell started it - or,
  if the machine lost power meanwhile, at the next FileDO start.
  Executables and scripts are NEVER launched: they are extracted, the
  location is shown, and you decide. Reveal takes one container, never a
  mask: a batch of plaintext copies is not something this offers.

In the window (filedo_win.exe, or filedo.exe ui): the Protect group carries
the same three operations as pages - the password is masked and typed twice on
secure, the disposition of the original is chosen before anything runs, and a
reveal's copy is removed by the button on the page rather than by a keypress in
a console. The password never reaches a command line from there.

Inspect:
  filedo.exe fdsec info c.fd-sec p:pwd   → Layout facts (the password is needed:
                                            nothing in the file says it is one)
  filedo.exe fdsec verify c.fd-sec p:pwd → Prove every chunk and the digest

Explorer integration (the MSI installer offers this as a feature; the
portable zip, winget and go install builds ask for it here):
  filedo.exe fdsec register              → A "File DO.." group on every file
                                           (secure, secure+del, secure+wipe,
                                           secure+rename, unsecure,
                                           unsecure+del, unsecure+start, wipe,
                                           check, info)
                                           and a .fd-sec document type whose
                                           double-click opens the shell window
                                           and reveals there - for this user
  filedo.exe fdsec register -all-users   → The same, machine-wide (needs an
                                           elevated console)
  filedo.exe fdsec unregister            → Remove exactly what was written

Credential sources. p: is REQUIRED whenever any option is present, so a
password equal to an option word is never eaten as that option:
  p:<password>   → on the command line (visible in the process list)
  pf:<file>      → from a password file (one trailing newline is dropped)
  pe:<VAR>       → from an environment variable
  k:<keyfile>    → any file; the digest of its bytes is the credential
  <password>     → bare trailing token, valid only when nothing else is given
  (nothing)      → prompt with no echo; twice on secure, once on the rest
  An EMPTY password is accepted and means obfuscation only - NO SECRECY.

Options (order does not matter):
  del / delete   → remove the source after the read-back verified it
  wipe           → overwrite the original, then remove it (secure only).
                   Honest caveat: on SSDs and on copy-on-write or journaled
                   volumes an overwrite-in-place lowers the odds of recovery
                   but does not guarantee erasure.
  rename / ren   → write a random name with no extension (a nameless blob)
  here           → restore into the current folder (unsecure only)
  start          → hand the restored file to its registered handler
                   (unsecure only, no executable/script refusal - like an
                   ordinary double-click). The copy goes into
                   %%LOCALAPPDATA%%\FileDO\reveal\<random>\, readable by you
                   and the system only, and removed when the window closes;
                   name a destination with to <dest> to keep the file
  to <dest>      → exact destination, or a folder; the parent must exist
  -y / --force   → skip the prompt, never the verification or the guards
  -rw            → reveal only: no writable sandbox exists, so this restores
                   to a file you own instead (combine with to <dest>)
  -keep          → reveal only: leave the copy for the next start to sweep

Exit codes: 0 ok, 2 usage, 3 wrong credential or tampered, 4 damaged,
            5 I/O, 6 unsupported container or stage not shipped yet.

Credentials are redacted from history.json and from the batch echo. The
argument form is still visible in the system process list while it runs.

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

COPY & WIPE OPERATIONS

Intelligent Copy (AI-Optimized for All Hardware):
  filedo.exe copy D:\Source E:\Target     → Auto-detects SSD/HDD/USB, optimizes threads & buffers automatically
                                           ⚡ Auto-switches to SAFE mode if hardware errors detected
  filedo.exe folder D:\Source copy E:\Target → Copy folder with hardware-aware optimization
  filedo.exe network \\server\share copy C:\Local → Copy network share with intelligent analysis
  filedo.exe file document.txt copy backup.txt → Copy individual file with automatic best strategy

Manual Copy Strategies:
  filedo.exe device C: copy D:\Backup     → Copy device contents to folder
  filedo.exe folder D:\Source copy E:\Target --precount → Count the tree first for exact totals and ETA
  Note: a plain copy walks the tree once and copies as it goes, so the first file
        moves at once and the totals grow while it runs. --precount adds one
        counting walk first. The parallel modes below always count first.

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

Smart Copy with Drive Analysis (AI-optimized performance):
  filedo.exe smartcopy C:\Source D:\Target  → Auto-detects SSD/HDD/USB, optimizes threads & buffers
  filedo.exe smart F:\Photos E:\Backup     → Analyzes file system, cluster sizes, drive types
  filedo.exe auto C:\Data E:\Mirror        → Intelligent strategy selection based on hardware

Safe Copy for Damaged/Problematic Drives:
  filedo.exe safecopy F:\Photos E:\Backup  → Ultra-safe mode for damaged drives (1 thread, small buffers)
  filedo.exe safe H:\Old_HDD C:\Rescue     → Minimizes drive stress, 10s timeout, skip damaged files
  filedo.exe rescue G:\Failing D:\Backup   → Conservative approach with damage logging and recovery
  filedo.exe damaged C:\Problem E:\Safe    → Automatic damaged file detection and skip list

Fast Content Wiping:
  filedo.exe folder D:\Temp wipe          → Fast wipe folder contents (delete & recreate)
  filedo.exe device D: wipe               → Wipe device contents (standard method for system folders)
  filedo.exe network \\server\temp wipe   → Wipe network folder contents
  filedo.exe folder C:\Cache w            → Short form of wipe command
  filedo.exe folder D:\Temp wipe --force  → Skip the interactive prompt (automation)
  Note: wipe always asks "Type WIPE to continue" before deleting. --force (or -y)
        skips that prompt for normal targets only. Drive/share roots, reparse
        points (junctions/symlinks) and the system TEMP folder ALWAYS require
        interactive confirmation and are never bypassed by --force.

Folder Compare:
	filedo.exe compare D:\Source E:\Target   → Compare directory trees and report differences
	filedo.exe cmp D:\Source E:\Target del source → Delete files in Source that also exist in Target
	filedo.exe cmp D:\Source E:\Target del target → Delete files in Target that also exist in Source
		filedo.exe cmp D:\Source E:\Target del old           → Delete older file of each pair (equal time: skip)
		filedo.exe cmp D:\Source E:\Target del new           → Delete newer file of each pair (equal time: skip)
		filedo.exe cmp D:\Source E:\Target del small         → Delete smaller file of each pair (equal size: skip)
		filedo.exe cmp D:\Source E:\Target del big           → Delete bigger file of each pair (equal size: skip)
		filedo.exe cmp D:\Source E:\Target del small source  → Delete only when smaller side is Source
		filedo.exe cmp D:\Source E:\Target del big target    → Delete only when bigger side is Target
		filedo.exe cmp D:\Source E:\Target del old target    → Delete only when older side is Target
		filedo.exe cmp D:\Source E:\Target del new source    → Delete only when newer side is Source
	Notes: matching by relative path; size-only comparison; mtime used for old/new; permanent delete; no confirmation

Folder Health Check:
	filedo.exe check D:\Data                 → Read-check all files; mark damaged on read delay > 2.0s
	filedo.exe check D:\Data\one.mkv         → Read-check that one file and say whether it reads cleanly
	Notes: one-time warm-up up to 10.0s before first read; uses 'skip_files.list' immediately; parallel workers; Ctrl+C supported
	       a single file is never skipped by the good list and never filtered out by size or extension

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
  --events <path> → Write the opt-in JSON Lines event stream for a supervisor
  --stop-file <path> → Stop gracefully when the named file appears

Exit codes (all non-container verbs):
  0 passed or done, 1 ran and found a defect, 2 could not be verified

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

• Damaged Disk Protection (safe/rescue): Files that show no read progress for
	10 seconds are skipped and appended immediately to 'skip_files.list'.
	Timeout can be overridden via env var FILEDO_TIMEOUT_NOPROGRESS_SECONDS.

• Fake Capacity Detection: The 'test' command creates 100 files, each 1%% of
  total capacity, to expose counterfeit storage devices that cheerfully claim
  sizes they do not actually have.
  Uses optimized smart verification - full verification for first 5 files and
  every 10th file, fast header-only checks for recent files between milestones.

• Secure Wiping: Use 'fill <size> del' to overwrite free space and prevent
  recovery of previously deleted files.

• Automatic Hardware Protection: All copy commands automatically detect
  hardware errors (buffer overflows, memory issues, I/O errors) and switch
  to SAFE RESCUE mode with minimal stress settings for damaged drives.

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
  to safe temporary locations (%%TEMP%%\FileDO_Operations) with user confirmation.
  Environment variables:
  - FILEDO_DISABLE_REDIRECT=1 → Disable redirection (advanced users only)
  - FILEDO_AUTO_CONFIRM=1 → Auto-confirm redirections (for scripts/testing)

Help & Support:
  filedo.exe ?                     → Show quick help summary
  filedo.exe help                  → Show detailed help (this screen)
  filedo.exe /help                 → Show detailed help (this screen)

  GitHub: https://github.com/SerZhyAle/FileDO
`, version)

var list_of_flags_for_device = []string{"device", "dev", "disk", "d"}
var list_of_flags_for_folder = []string{"folder", "fold", "dir", "fld"}
var list_of_flags_for_file = []string{"file", "fl", "f"}
var list_of_flags_for_network = []string{"network", "net", "n"}
var list_of_flags_for_from = []string{"from", "batch", "script"}
var list_of_flags_for_hist = []string{"hist", "history"}
var list_of_flags_for_duplicates = []string{"check-duplicates", "cd", "duplicate"}
var list_of_flags_for_compare = []string{"compare", "cmp"}
var list_of_flags_for_copy = []string{"copy", "cp"}
var list_of_flags_for_fastcopy = []string{"fastcopy", "fcopy", "fc"}
var list_of_flags_for_synccopy = []string{"synccopy", "scopy", "sc"}
var list_of_flags_for_balanced = []string{"balanced", "bcopy", "bc"}
var list_of_flags_for_maxcopy = []string{"maxcopy", "mcopy", "max", "turbo"}
var list_of_flags_for_smartcopy = []string{"smartcopy", "smart", "auto"}
var list_of_flags_for_safecopy = []string{"safecopy", "safe", "rescue", "damaged"}
var list_of_flags_for_check = []string{"check"}
var list_of_flags_for_wipe = []string{"wipe", "w"}
var list_of_flags_for_fdsec = []string{"fdsec", "fds"}
var list_of_flags_for_ui = []string{"ui", "gui"}
var list_fo_flags_for_help = []string{"?", "/?", "-?", "--help", "help", "h", "/help"}
var list_fo_flags_for_short_help = []string{"?", "/?", "-?", "--help"}
var list_fo_flags_for_full_help = []string{"help", "h", "/help"}
var list_of_flags_for_all = append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(append(list_of_flags_for_device, list_of_flags_for_folder...), list_of_flags_for_file...), list_of_flags_for_network...), list_of_flags_for_from...), list_of_flags_for_hist...), list_of_flags_for_duplicates...), list_of_flags_for_compare...), list_of_flags_for_copy...), list_of_flags_for_fastcopy...), list_of_flags_for_synccopy...), list_of_flags_for_balanced...), list_of_flags_for_maxcopy...), list_of_flags_for_smartcopy...), list_of_flags_for_safecopy...), list_of_flags_for_check...), list_of_flags_for_wipe...), list_of_flags_for_fdsec...), list_of_flags_for_ui...)

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
		// The echo repeats the command line, so the same redaction as history
		// applies (invariant 8: no credential on the console either).
		fmt.Printf("\n[%d] Executing: %s\n", commandCount, strings.Join(redactCredentialArgs(strings.Fields(line)), " "))

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

	// Each batch line asks for --precount on its own; none inherits it.
	copyPrecount = false

	// The target-first fdsec grammar is dispatched before the path probe:
	// a mask target (secure *.txt) never passes os.Stat, and the family owns
	// its own not-found message and exit code. This is the SAME call main()
	// makes, so a verb that works interactively cannot silently fail here.
	if handled, err := fdsecDispatchTarget(args, internalLogger); handled {
		if err != nil {
			internalLogger.SetError(err)
			fdsecSetExit(err)
			runFailure(err)
			return err
		}
		internalLogger.SetSuccess()
		return nil
	}

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
					return fmt.Errorf("the folder %q does not exist", args[0])
				} else if strings.Contains(args[0], ".") {
					return fmt.Errorf("the file %q does not exist", args[0])
				} else {
					return fmt.Errorf("the path %q does not exist", args[0])
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
	case contains(list_of_flags_for_fdsec, command):
		if err := handleFdsecCommand(add_args, internalLogger); err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
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
	case contains(list_of_flags_for_compare, command):
		if len(args) < 3 {
			return fmt.Errorf("compare command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "compare")
		if err := handleCompareCommand(args[1], args[2], args[3:]...); err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_copy, command):
		// Handle intelligent copy command with automatic strategy selection
		if len(args) < 3 {
			return fmt.Errorf("copy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "auto-copy")
		copyPrecount = wantsCopyPrecount(args[3:])
		err := handleAutoCopyCommand(args[1], args[2])
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
	case contains(list_of_flags_for_smartcopy, command):
		// Handle smart copy command with advanced drive analysis
		if len(args) < 3 {
			return fmt.Errorf("smartcopy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "smartcopy")
		err := handleSmartCopyCommand(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_safecopy, command):
		// Handle safe copy command for damaged drives
		if len(args) < 3 {
			return fmt.Errorf("safecopy command requires source and target paths")
		}
		internalLogger.SetCommand(command, args[1], "safecopy")
		err := SafeCopy(args[1], args[2])
		if err != nil {
			internalLogger.SetError(err)
			return err
		}
		internalLogger.SetSuccess()
	case contains(list_of_flags_for_check, command):
		// Handle check command with flags
		if len(args) < 2 {
			return fmt.Errorf("check command requires a folder or a file path")
		}
		internalLogger.SetCommand(command, args[1], "check")
		if err := HandleCheckArgs(args[1], args[2:]); err != nil {
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
	case contains(list_of_flags_for_ui, command):
		return fmt.Errorf("cannot launch UI shell from batch script")
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

// handleAutoCopyCommand provides simple copy with automatic optimization (user-friendly)
func handleAutoCopyCommand(sourcePath, targetPath string) error {
	// Perform silent advanced strategy analysis
	analysis, err := AnalyzeCopyStrategyQuiet(sourcePath, targetPath)
	if err != nil {
		// Fallback to basic smart copy if advanced analysis fails
		fmt.Printf("🔄 Starting intelligent copy...\n")
		return handleSmartCopyCommand(sourcePath, targetPath)
	}

	// Execute optimal strategy with minimal output
	fmt.Printf("🔄 Starting copy with auto-optimization (%s)...\n", analysis.StrategyName)
	return ExecuteSelectedStrategy(analysis, sourcePath, targetPath)
}

// handleSmartCopyCommand analyzes drives and selects optimal copy strategy
func handleSmartCopyCommand(sourcePath, targetPath string) error {
	// Perform advanced strategy analysis with drive detection
	analysis, err := AnalyzeCopyStrategyAdvanced(sourcePath, targetPath)
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

	// Optimize GC settings for better performance and less memory overhead
	debug.SetGCPercent(50) // More frequent GC to reduce memory usage
	runtime.GOMAXPROCS(0)  // Use all available CPUs

	// Initialize global interrupt handler first
	globalInterruptHandler = NewInterruptHandler()

	// Lifetime mechanism 3 (spec 8.2): every FileDO start reclaims any reveal
	// sandbox left behind by a previous one, before anything else runs. This
	// is the only remedy for a handler that never locks its input and for a
	// power loss while a copy exists - stage S0's probe P2 established that
	// the OS itself will not tidy %LOCALAPPDATA% up for us. A sandbox that is
	// in use right now, in another FileDO, holds a lock and is stepped around.
	fdsecSweepReveals()

	hi_message := "\n" + start_time.Format("2006-01-02 15:04:05") + " sza@ukr.net " + version + "\n"
	fmt.Print(hi_message)

	// Registered first, so it runs last: --pause holds the window open after
	// the finish line, not before it. The flag itself is read a few lines
	// down, which is soon enough - nothing above this point can end the run.
	defer holdConsoleIfAsked()

	// Ensure bue_message is always printed. Panic recovery is registered after
	// finishRun below so it records Not proven before the result is emitted.
	defer func() {
		bue_message := "\n Finish:" + time.Now().Format("2006-01-02 15:04:05") + ", Duration: " + formatDurationDetailed(time.Since(start_time)) + "\n"
		fmt.Print(bue_message)
	}()

	rawArgs := os.Args
	args, eventsPath, stopFilePath, noUI, pause := extractGlobalFlags(rawArgs)
	pauseOnExit = pause

	if eventsPath != "" {
		em, err := InitEventManager(eventsPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to initialize events channel: %v\n", err)
		} else {
			defer em.Close()
		}
	}

	if stopFilePath != "" {
		machineStopChannel = true
		globalInterruptHandler.WatchStopFile(stopFilePath)
	}

	// Initialize history logger. The deferred func flushes the history entry
	// and only then honours a nonzero exit code - os.Exit skips defers,
	// so the ordering is deliberate.
	historyLogger := NewHistoryLogger(args)
	defer func() {
		historyLogger.Finish()
		if globalEventManager != nil {
			globalEventManager.Close()
		}
		// os.Exit skips every remaining defer, so the window has to be held
		// here rather than in the deferred call registered above.
		if fdsecExitCode != 0 {
			holdConsoleIfAsked()
			os.Exit(fdsecExitCode)
		}
		if globalExitCode != 0 {
			holdConsoleIfAsked()
			os.Exit(globalExitCode)
		}
	}()

	// Registered after the history flush, so it runs before it: the `result`
	// event has to be on disk while the events file is still open, and the
	// exit code has to be decided before the deferred os.Exit reads it.
	// finishRun is the only writer of both (outcome.go).
	defer finishRun()
	defer recoverRunPanic()

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

	if len(args) < 2 {
		fmt.Println(shortUsage)
		if !noUI && os.Getenv("FILEDO_NO_UI") != "1" {
			if err := launchUI(); err != nil {
				fmt.Printf("\nGUI is available as filedo_win.exe (download from https://github.com/SerZhyAle/FileDO/releases)\n")
			}
		}
		return
	}

	if contains(list_fo_flags_for_help, lowerArgs[1]) {
		if contains(list_fo_flags_for_short_help, lowerArgs[1]) {
			fmt.Println(shortUsage)
		} else {
			fmt.Println(usage)
		}
		return
	}

	// Target-first fdsec ops (<file> secure|unsecure|reveal ..) are handled
	// before the generic path probe - see fdsecDispatchTarget. The batch
	// path calls exactly the same function, so a verb cannot work
	// interactively and silently fail from a .lst file.
	if handled, err := fdsecDispatchTarget(args[1:], historyLogger); handled {
		if err != nil {
			historyLogger.SetError(err)
			fdsecSetExit(err)
			runFailure(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		historyLogger.SetSuccess()
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
			return
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
						err := fmt.Errorf("the folder %q does not exist", args[1])
						reportRunError(err, historyLogger)
						return
					} else if strings.Contains(args[1], ".") {
						err := fmt.Errorf("the file %q does not exist", args[1])
						reportRunError(err, historyLogger)
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
	case contains(list_of_flags_for_fdsec, command):
		if err := handleFdsecCommand(add_args, historyLogger); err != nil {
			historyLogger.SetError(err)
			fdsecSetExit(err)
			runFailure(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		historyLogger.SetSuccess()
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
			usageFailure(command, args, "Missing file path for 'from' command")
			return
		}
		if err := executeFromFile(add_args[0], historyLogger); err != nil {
			reportRunError(err, historyLogger)
			return
		}
	case contains(list_of_flags_for_hist, command):
		handleHistoryCommand(os.Args[1:])
		return
	case contains(list_of_flags_for_compare, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Compare command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "compare")
		beginRun(runActs, "compare", add_args[0], args)
		if err := handleCompareCommand(add_args[0], add_args[1], add_args[2:]...); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_copy, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "auto-copy")
		beginRun(runActs, "auto-copy", add_args[0], args)
		copyPrecount = wantsCopyPrecount(add_args[2:])
		if err := handleAutoCopyCommand(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_fastcopy, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Fast copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "fastcopy")
		beginRun(runActs, "fastcopy", add_args[0], args)
		if err := handleFastCopyCommand(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_synccopy, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Sync copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "synccopy")
		beginRun(runActs, "synccopy", add_args[0], args)
		if err := handleSyncCopyCommand(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_balanced, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Balanced copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "balanced")
		beginRun(runActs, "balanced", add_args[0], args)
		if err := handleBalancedCopyCommand(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_maxcopy, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Max copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "maxcopy")
		beginRun(runActs, "maxcopy", add_args[0], args)
		if err := handleMaxCopyCommand(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_smartcopy, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Smart copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "smartcopy")
		beginRun(runActs, "smartcopy", add_args[0], args)
		if err := handleSmartCopyCommand(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_safecopy, command):
		if len(add_args) < 2 {
			usageFailure(command, args, "Safe copy command requires source and target paths")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "safecopy")
		beginRun(runActs, "safecopy", add_args[0], args)
		if err := SafeCopy(add_args[0], add_args[1]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_check, command):
		if len(add_args) < 1 {
			usageFailure(command, args, "CHECK command requires a folder or a file path")
			return
		}
		historyLogger.SetCommand(command, add_args[0], "check")
		beginRun(runJudges, "check", add_args[0], args)
		if err := HandleCheckArgs(add_args[0], add_args[1:]); err != nil {
			reportRunError(err, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	case contains(list_of_flags_for_ui, command):
		historyLogger.SetCommand("ui", "", "ui")
		beginRun(runActs, "ui", "", args)
		if err := launchUI(add_args...); err != nil {
			fmt.Fprintf(os.Stderr, "GUI is available as filedo_win.exe (download from https://github.com/SerZhyAle/FileDO/releases)\nError: %v\n", err)
			historyLogger.SetError(err)
			runFailure(err)
			return
		}
		historyLogger.SetSuccess()
		return
	default:
		usageFailure(command, args, "Unknown command %q", os.Args[1])
		fmt.Println(usage)
		return
	}
}
