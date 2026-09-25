// sza250707
// sza250712
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"filedo/statedir"

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

// findUIExecutable finds the GUI beside this executable - through a winget
// Links symlink to the real install folder when there is one - or on PATH.
// Never in the current directory: a filedo_win.exe planted in a folder the
// user runs FileDO from would otherwise be launched (CLI-26); exec.LookPath's
// ErrDot answer is refused for the same reason.
func findUIExecutable() string {
	if exePath, err := os.Executable(); err == nil {
		dirs := []string{filepath.Dir(exePath)}
		if real, rerr := filepath.EvalSymlinks(exePath); rerr == nil {
			dirs = append(dirs, filepath.Dir(real))
		}
		for _, dir := range dirs {
			target := filepath.Join(dir, "filedo_win.exe")
			if _, err := os.Stat(target); err == nil {
				return target
			}
		}
	}
	if path, err := exec.LookPath("filedo_win.exe"); err == nil && filepath.IsAbs(path) {
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
	finished     bool
	// touched records that the run did something worth a history line. A
	// run that only printed help, listed the history or opened the window
	// writes nothing at all - not even an empty file (CLI-31).
	touched bool
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

	// Create full command string from args. Credential material of the fdsec
	// family is redacted HERE, before anything is written: a password that
	// reaches history.json is on the user's disk permanently (spec 12).
	fullCommand := strings.Join(redactCredentialArgs(args), " ")

	// Nothing touches the disk here: the file is located, created and
	// written only by Finish, and only for a run that did something.
	return &HistoryLogger{
		enabled:      enabled,
		startTime:    time.Now(),
		originalArgs: args,
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
	hl.touched = true
	hl.entry.Command = command
	hl.entry.Target = target
	hl.entry.Operation = operation
}

// HideTarget replaces the target - in the target field and in the recorded
// command line - with the folder that holds it. `secure rename` exists so
// that nothing links the anonymous blob to the original's name, and a history
// line saying "salary-2026.xlsx -> QgTZ.." was exactly that link (CLI-21).
func (hl *HistoryLogger) HideTarget(path string) {
	if !hl.enabled {
		return
	}
	dir := filepath.Dir(path)
	if abs, err := filepath.Abs(path); err == nil {
		dir = filepath.Dir(abs)
	}
	hl.entry.Target = dir
	args := redactCredentialArgs(hl.originalArgs)
	for i, a := range args {
		if a == path {
			args[i] = dir
		}
	}
	hl.entry.FullCommand = strings.Join(args, " ")
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
	hl.touched = true
	hl.entry.Success = false
	// The same screened message the event stream gets: an error that carries
	// a container's sealed name keeps it on the console and out of this
	// permanent file (FDSEC-06).
	hl.entry.ErrorMsg = eventSafeErrorMessage(err)
}

func (hl *HistoryLogger) SetSuccess() {
	if !hl.enabled {
		return
	}
	hl.touched = true
	hl.entry.Success = true
}

func (hl *HistoryLogger) SetResultSummary(summary string) {
	if !hl.enabled {
		return
	}
	hl.entry.ResultSummary = summary
}

func (hl *HistoryLogger) Finish() {
	if !hl.enabled || hl.finished || !hl.touched {
		return
	}
	hl.finished = true

	hl.entry.Duration = formatDuration(time.Since(hl.startTime))

	// Generate result summary if not already set
	if hl.entry.ResultSummary == "" && hl.entry.Success {
		hl.entry.ResultSummary = hl.generateResultSummary()
	}

	if err := saveToHistory(hl.entry); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: history not saved: %v\n", err)
	}
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

// historyFilePath is where history.json lives: the state root, with a
// one-time import of the file earlier versions left in the current directory
// or beside the executable (SP-0024 section 4).
func historyFilePath() (string, error) {
	return statedir.Path("history.json", statedir.LegacyInCwd("history.json"), statedir.LegacyBesideExe("history.json"))
}

// saveToHistory appends one entry. The read-modify-write runs under a lock
// file, so two FileDO processes never lose each other's entries; the write
// goes through a temporary file and a rename, so a crash never leaves half a
// file; and a file that does not parse is set aside as
// history.json.corrupt-<time>, never overwritten - it is the user's record
// (CLI-16).
func saveToHistory(entry HistoryEntry) error {
	historyFile, err := historyFilePath()
	if err != nil {
		return err
	}
	unlock, err := statedir.Lock(historyFile, 5*time.Second)
	if err != nil {
		return err
	}
	defer unlock()

	var history []HistoryEntry
	if data, err := os.ReadFile(historyFile); err == nil && len(bytes.TrimSpace(data)) > 0 {
		if uerr := json.Unmarshal(data, &history); uerr != nil {
			kept := historyFile + ".corrupt-" + time.Now().Format("20060102-150405")
			if rerr := os.Rename(historyFile, kept); rerr != nil {
				return fmt.Errorf("history.json does not parse (%v) and could not be set aside: %w", uerr, rerr)
			}
			fmt.Fprintf(os.Stderr, "Warning: history.json did not parse (%v); kept it as %s and started a new one.\n", uerr, kept)
			history = nil
		}
	}

	history = append(history, entry)

	if len(history) > 1000 {
		history = history[len(history)-1000:]
	}

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}

	return statedir.WriteFileAtomic(historyFile, data, 0o644)
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
  filedo.exe device E: fill verify → Read back what fill wrote (fake capacity check)
  filedo.exe device E: clean       → Delete FileDO's test files (lists them, asks; --yes skips)

File Organization:
  filedo.exe C: check-duplicates   → Find duplicate files on the device
  filedo.exe device D: cd          → Short form of check-duplicates command
  filedo.exe D:\Photos cd old move E:\Dups → Move the older copies to E:\Dups, asking per file
  filedo.exe D:\Photos cd del new  → Delete the newer copies, asking before each one
  filedo.exe D:\Photos cd -y del old → Delete the older copies without asking
  filedo.exe cd from list dups.lst del new -y → Process duplicates from a saved list file
  Rules - each word names what is removed; word order does not matter:
    old → remove the older copies, keep the newest (the default)
    new → remove the newer copies, keep the oldest
    abc → keep the alphabetically last name, remove the others
    xyz → keep the alphabetically first name, remove the others
  Newest and oldest compare creation times; a tie keeps the first path.
  del and move ask before each file; -y (or --yes) answers yes for all of
  them, and is required where nobody can answer (no console, or a run given
  --stop-file). Each copy is compared byte for byte with the kept one right
  before it goes; a move never replaces a file. A drive or share root,
  Windows, Program Files and the system TEMP ask for a typed confirmation,
  -y or not, and a scan skips Windows and Program Files unless it starts
  inside them.

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
  filedo.exe folder E:\Data cd move F:\Backup abc → Keep the alphabetically last name, move the others
  filedo.exe folder E:\Data cd abc -y move F:\Backup → The same without asking (any word order)
  filedo.exe cd from list my_dups.lst del → Process duplicates from a saved list, asking per file
  (Rules, -y and the guarded places: DEVICE OPERATIONS, File Organization.)

═══════════════════════════════════════════════════════════════════════════════
FILE OPERATIONS (Individual files)

File Analysis:
  filedo.exe readme.txt            → Show detailed file information
  filedo.exe file data.zip info    → Show detailed file information
  filedo.exe file document.pdf short → Show brief file summary

═══════════════════════════════════════════════════════════════════════════════
SECRET FILES (.fd-sec containers)

  A container holds one file, or one folder with its whole tree. The true
  name, the real size and the timestamps are sealed inside it - for a folder,
  every entry's path, size and timestamps too; only the container's own size
  and its format parameters are visible without the password.

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
  filedo.exe "Tax 2025" secure           → Pack a whole folder into
                                           Tax 2025.fd-sec (junctions and
                                           symlinks inside are refused)
  filedo.exe "Tax 2025.fd-sec" unsecure to D:\Restored
                                         → Restore the tree; an existing
                                           folder is never merged into

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
  suite2         → secure one file with suite 2 instead of the default
                   suite 1: pepper-folded Argon2id at 256 MiB, and a file
                   with no alignment either. It keeps the true name, the
                   real size and the time of encryption, but NOT the
                   original's timestamps, and only FileDO reads it - other
                   programs that read .fd-sec do not. unsecure, reveal, info
                   and verify find the suite themselves
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
  filedo.exe network \\server\share\Photos cd xyz del → Keep the alphabetically first name, delete the others

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
  filedo.exe folder D:\Temp wipe          → Delete everything inside the folder (the folder, its access list and attributes stay)
  filedo.exe device D: wipe               → Delete everything on D:\ (a root: always the typed confirmation)
  filedo.exe network \\server\temp wipe   → Wipe network folder contents
  filedo.exe folder C:\Cache w            → Short form of wipe command
  filedo.exe folder D:\Temp wipe --force  → Skip the interactive prompt (automation)
  Note: wipe always asks "Type WIPE to continue" before deleting. --force (or -y)
        skips that prompt for normal targets only. Drive/share roots and the
        TEMP, profile, Windows and Program Files folders (and every folder
        above them) ALWAYS require interactive confirmation and are never
        bypassed by --force. A junction or symlink is refused - wipe the
        folder it points to by its own path. Anything left undeleted: exit 2.

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
	Notes: matching by relative path; del source|target deletes a pair only when size and time match
	       (--by-hash: equal content; --allow-mismatch: any pair); mtime used for old/new;
	       the two folders must not be one folder or nest; permanent delete; no confirmation

Folder Health Check:
	filedo.exe check D:\Data                 → Read-check all files; mark damaged on read delay > 2.0s
	filedo.exe check D:\Data\one.mkv         → Read-check that one file and say whether it reads cleanly
	filedo.exe check D:\Data --resume        → Carry on: skip files an earlier run already read cleanly
	Notes: one-time warm-up up to 10.0s before first read; parallel workers; Ctrl+C supported
	       damaged and good lists live in %%LOCALAPPDATA%%\FileDO\state (never beside your files);
	       a file already on the damaged list is reported again without being read; a changed file is read again
	       locked or unreadable files and folders are "could not verify" (exit 2), never "damaged"
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
  clean, cln, c   → Delete FileDO's test files (asks first; --yes skips the question)

Size Specifications:
  <number>        → Size in megabytes (e.g., 100, 500, 1000)
  <n>k/m/g/t      → Size with a unit (e.g., 500m, 2g, 1.5g); a size that does not
                    parse or is out of range is an error, never a default
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
  filedo.exe cd from list my_dups.lst del new -y → Delete the newer copies of every listed group, without asking

Secure Space Wiping:
  filedo.exe C: fill 5000 del      → Fill 5GB then secure delete (data recovery prevention)

Fast Backup & Cleanup:
  filedo.exe folder C:\ImportantData copy D:\Backup → Copy folder with progress tracking
  filedo.exe device D: copy \\server\archive → Copy entire device to network storage
  filedo.exe fastcopy D:\SlowHDD E:\FastSSD → Optimized parallel copy for large datasets
  filedo.exe fc \\NAS\Photos C:\LocalBackup → High-speed copy from slow network/extFAT drives
  filedo.exe synccopy D:\HDD1 D:\HDD2       → Synchronized copy for diagnosing I/O speeds
  filedo.exe folder D:\TempFiles wipe      → Empty a temporary folder (the folder itself stays)
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
	10 seconds are skipped and recorded at once in the skip list
	(%%LOCALAPPDATA%%\FileDO\state\skip_files.list) with their size and time;
	a file that changes is tried again. Timeout can be overridden via env var
	FILEDO_TIMEOUT_NOPROGRESS_SECONDS.

• Fake Capacity Detection: The 'test' command writes 100 files over 95%% of the
  free space (on the system drive it keeps the larger of 10 GB and 10%% free) to
  expose counterfeit storage devices that cheerfully claim sizes they do not
  actually have. Every block of every file names its file, its offset and its
  run; every read-back bypasses the Windows cache, file 1 is re-read after every
  write, and all files are re-read before a PASS. A speed change alone is never
  a verdict. Exit 1 means the data did not read back; exit 2 means the target
  could not be tested (write-protected, full, gone) - nothing was proven.

• Secure Wiping: Use 'fill <size> del' to overwrite free space and prevent
  recovery of previously deleted files.

• Automatic Hardware Protection: copy, fastcopy, balanced and maxcopy retry
  the files that stalled or hit device I/O errors in SAFE RESCUE mode.

• Copy Safety: every copy writes <name>.filedo-partial and renames it into
  place only when complete, with the source's modification time; an existing
  different file is never overwritten; a file already at the target (same
  size and time) is skipped. A copy onto itself or into its own subfolder is
  refused. Any file not at the target at the end: exit 2. The bytes, the
  modification time and the read-only flag are copied; other attributes,
  alternate data streams and access lists are not.

• Test Files: Operations create files named FILL_#####_ddHHmmss_<run>.tmp and
  speedtest_*.txt. Use 'clean' to remove them: it removes only files with a
  FileDO name and FileDO content, and asks first (--yes skips the question).

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
	if err := refuseCopyPaths("copy", sourcePath, targetPath); err != nil {
		return err
	}
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

func ShowLastHistory(count int) {
	historyFile, perr := historyFilePath()
	if perr != nil {
		fmt.Printf("Error locating history: %v\n", perr)
		return
	}

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

	if len(args) < 2 {
		fmt.Println(shortUsage)
		if !noUI && os.Getenv("FILEDO_NO_UI") != "1" {
			if err := launchUI(); err != nil {
				fmt.Printf("\nGUI is available as filedo_win.exe (download from https://github.com/SerZhyAle/FileDO/releases)\n")
			}
		}
		return
	}

	// The one dispatch, shared with the batch path (dispatch.go, CLI-30).
	dispatchLine(args[1:], historyLogger, false)
}
