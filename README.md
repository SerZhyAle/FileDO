<p align="center"><img src="assets/icon.png" alt="FileDO" width="128" height="128" /></p>

# FileDO - Advanced File & Storage Operations Tool

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2609241700-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**Storage Testing • Performance Analysis • Security Wiping • Fake Capacity Detection • Duplicate Management**

*A command-line tool for Windows storage: capacity and speed tests, fake-capacity detection, secure wipe, fill and duplicate management.*

</div>

---

## Quick Start

### Most Common Tasks

```bash
# Check if USB/SD card is fake
filedo E: test del

# Test drive performance
filedo C: speed 100

# Secure wipe free space
filedo D: fill 1000 del

# Find and manage duplicate files
filedo C: check-duplicates
filedo D: cd old del

# Copy files with progress tracking
filedo folder C:\Source copy D:\Backup
filedo device E: copy F:\Archive
# Count the tree first for an exact time left (otherwise copying starts at the first file)
filedo folder C:\Source copy D:\Backup --precount

# Fast wipe folder contents
filedo folder C:\Temp wipe
filedo folder D:\Cache w

# Show device info
filedo C: info
```

### Compare & Cleanup

```bash
# Compare two folders and show summary + save report
filedo compare D:\Data E:\Backup

# Compare and delete (permanent, no confirmation)
filedo cmp D:\Data E:\Backup del source  # delete in Source if also exists in Target
filedo cmp D:\Data E:\Backup del target  # delete in Target if also exists in Source
filedo cmp D:\Data E:\Backup del old     # delete older side (by mtime), equal time: skip
filedo cmp D:\Data E:\Backup del new     # delete newer side (by mtime), equal time: skip
filedo cmp D:\Data E:\Backup del small   # delete smaller side, equal size: skip
filedo cmp D:\Data E:\Backup del big     # delete bigger side, equal size: skip

# Optional side qualifier (only apply when that side matches)
filedo cmp D:\Data E:\Backup del small source  # only if smaller is on Source
filedo cmp D:\Data E:\Backup del big target    # only if bigger is on Target
filedo cmp D:\Data E:\Backup del old target    # only if older is on Target
filedo cmp D:\Data E:\Backup del new source    # only if newer is on Source
```

Notes: matching by relative path, size-only equality; optional side qualifier for old/new/small/big; mtime used for old/new; Windows compare is case-insensitive; logs: compare_report_*.log, delete_report_<mode>_*.log.

### Health CHECK (fast read check)

```bash
# Check folder by reading files; mark as damaged if initial read delay > 2.0s
filedo check F:\Mov
```

### CHECK: CLI flags (flags override env)

Flags mirror FILEDO_CHECK_* environment variables and have precedence. Use them after `check <path>`.

- General
	- `--threshold <sec>` (FILEDO_CHECK_THRESHOLD_SECONDS)
	- `--warmup <sec>` (FILEDO_CHECK_WARMUP_SECONDS)
	- `--warmup-idle <sec>` (FILEDO_CHECK_WARMUP_IDLE_RESET_SECONDS)
	- `--workers <int>` (FILEDO_CHECK_WORKERS)
	- `--buf-kb <int>` (FILEDO_CHECK_BUF_KB)
	- `--mode quick|balanced|deep` (FILEDO_CHECK_MODE)
	- `--balanced-min-mb <int>` (FILEDO_CHECK_BALANCED_MIN_MB)
	- `--min-mb <float>` / `--max-mb <float>` (FILEDO_CHECK_MIN_MB/MAX_MB)
	- `--include-ext ".jpg,.png"` / `--exclude-ext ".bak,.tmp"`
- Limits
	- `--max-files <int>` (FILEDO_CHECK_MAX_FILES)
	- `--max-seconds <float>` (FILEDO_CHECK_MAX_DURATION_SEC)
	- `--precount` / `--no-precount` (FILEDO_CHECK_PRECOUNT)
- Behavior & output
	- `--dry-run` (FILEDO_CHECK_DRYRUN)
	- `--verbose` (FILEDO_CHECK_VERBOSE)
	- `--quiet` (FILEDO_CHECK_QUIET)
	- `--resume` (FILEDO_CHECK_RESUME)
- Reporting
	- `--report csv|json` (FILEDO_CHECK_REPORT)
	- `--report-file <path>` (FILEDO_CHECK_REPORT_FILE)
- Good files cache
	- `--good-list <path>` (FILEDO_CHECK_GOODLIST)
- HDD‑friendly I/O (single-reader + adaptive throttling)
	- `--single-reader auto|on|off` (FILEDO_CHECK_SINGLE_READER)
	- `--ewma-alpha <float>` (FILEDO_CHECK_EWMA_ALPHA)
	- `--ewma-high-frac <float>` (FILEDO_CHECK_EWMA_HIGH_FRAC)
	- `--ewma-low-frac <float>` (FILEDO_CHECK_EWMA_LOW_FRAC)
	- `--max-sleep-ms <int>` (FILEDO_CHECK_MAX_SLEEP_MS)
	- `--sleep-step-ms <int>` (FILEDO_CHECK_SLEEP_STEP_MS)

Examples:

```bash
# Balanced mode, worker override, ETA precount
filedo check D:\Data --mode balanced --workers 6 --threshold 1.8 --precount

# Force single-reader for HDD/USB with adaptive throttling
filedo check F:\Photos --single-reader on --ewma-alpha 0.2 --max-sleep-ms 250

# Filter by extensions, cap files, save CSV report
filedo check D:\Media --include-ext .jpg,.png --max-files 1000 --report csv --report-file D:\rep.csv

# Use custom good files cache list and quiet output
filedo check D:\Data --good-list D:\check_files.list --quiet
```

- One-time warm-up allowance up to 10.0s before the first read (spin-up)
- Uses skip_files.list for immediate, persistent recording (no damaged_files.log)
- Skips paths already in skip_files.list; parallel workers; Ctrl+C supported

### Installation

#### Option 1 - winget (recommended)

```powershell
winget install SerZhyAle.FileDO
# short form (works once Microsoft indexes the moniker):
winget install filedo
```

This installs the CLI tools (`filedo`, `filedo_check`, `filedo_fill`, `filedo_test`) plus the `filedo_win` GUI (a window with a page per job), and adds them all to your `PATH`. To upgrade later:

```powershell
winget upgrade SerZhyAle.FileDO
```

To uninstall:

```powershell
winget uninstall SerZhyAle.FileDO
```

Also available in the [Microsoft Store](https://apps.microsoft.com/detail/9PH1LPCMRG83) - the same tools, installed and updated by the Store.

#### Option 2 - Installer (setup EXE)

Download `FileDO-<version>-setup.exe` from [Releases](https://github.com/SerZhyAle/FileDO/releases/latest) and run it. This is the option that makes FileDO an ordinary Windows program rather than a folder of executables:

- installs the binaries into `C:\Program Files\FileDO` and puts `filedo` on the system `PATH`;
- creates a **Start menu entry** and a **desktop icon** for the FileDO window (`filedo_win.exe`);
- registers the **Explorer integration**: a `File DO..` group in the right-click menu of every file - Secure (keeping, deleting or wiping the original, or under a random name), Unsecure (optionally deleting the container or starting the restored file), Wipe this file, Check this file, Info - plus the `.fd-sec` document type with its own icon, whose double-click is exactly the Unsecure-and-start entry: a console asks for the password there, restores the original under its true name into `%LOCALAPPDATA%\FileDO\reveal` - readable by this account and the system only - hands it to whatever program owns its real extension, and takes that copy away again when the console window closes. On Windows 11 the group lives under *Show more options*.

The installer is not code-signed, so on first run Windows may show *Windows protected your PC* and then ask for administrator permission. Compare the SHA256 (the `.sha256` file published next to the download; `certutil -hashfile FileDO-<version>-setup.exe SHA256`), then choose *More info* and *Run anyway*. Why the warning appears, what the administrator prompt is used for, and what FileDO never does: [Windows warned you about FileDO](https://serzhyale.github.io/FileDO/guides/install-trust.html).

The last two are features you can deselect on the installer's Customize page, and turn on or off later with **Change** in Apps & features. For an unattended rollout:

```powershell
# everything, no questions asked
FileDO-<version>-setup.exe /quiet
# ..with a log, if it has to be explained afterwards
FileDO-<version>-setup.exe /quiet /log install.log
# remove it again
FileDO-<version>-setup.exe /uninstall
```

The same release also publishes the bare `FileDO-<version>-windows-x64.msi` - the setup EXE carries exactly that file inside it - for deployment tools that want the package and the feature names directly:

```powershell
msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main,ExplorerIntegration,DesktopShortcut
msiexec /i FileDO-<version>-windows-x64.msi /qn ADDLOCAL=Main   # no Explorer entries, no desktop icon
```

Uninstalling removes everything the installer wrote, the registry entries included.

#### Option 3 - Microsoft Store (MSIX)

One package, two entries: a clickable **FileDO** tile - a GUI with a page per job, whose Command page builds and runs any command - and the `filedo` command exposed on `PATH` for any terminal. Best when you want the graphical window and the CLI together. The Store build does **not** carry the Explorer entries: a packaged build can only get them through a signed shell handler, which is separate work.

#### Option 4 - Manual download

1. **Download**: Grab the latest `FileDO-<version>-windows-x64.zip` from [Releases](https://github.com/SerZhyAle/FileDO/releases/latest)
2. **Extract** anywhere (e.g. `C:\Tools\FileDO`)
3. **Optional**: add the folder to your `PATH` so you can call `filedo` from any directory
4. **GUI**: the zip includes `filedo_win.exe` - run it next to `filedo.exe` for the graphical window (a page per job, plus a Command page)
5. **Run**: execute from command line or GUI

#### Explorer integration without an installer

winget, the portable zip and `go install` never run an installer, so they register nothing at all. Ask for the same group and document type yourself, and take them back the same way:

```powershell
filedo fdsec register              # for this user
filedo fdsec register -all-users   # machine-wide (needs an elevated console)
filedo fdsec unregister            # removes exactly what was written
```

FileDO removes only what it marked as its own: a document type that meanwhile belongs to another program is left alone, and whoever owned `.fd-sec` before FileDO did gets it back. A machine that already has the installer's registration is left alone too - the per-user copy would show every entry twice.

#### Option 5 - Build from source

```powershell
git clone https://github.com/SerZhyAle/FileDO.git
cd FileDO
.\build.ps1   # or build.bat
```

Requires Go 1.24+. Builds all four executables into `exe_to_download/`.

Build only the main CLI:

```powershell
go build -o filedo.exe .\cmd\filedo
```








---

## Core Operations

<table>
<tr>
<td width="50%">

### Device Testing
```bash
# Information
filedo C: info
filedo D: short

# Fake capacity detection
filedo E: test
filedo F: test del

# Performance testing  
filedo C: speed 100
filedo D: speed max
```

</td>
<td width="50%">

### File & Folder Operations
```bash
# Folder analysis
filedo C:\temp info
filedo . short

# Performance testing
filedo C:\data speed 100
filedo folder . speed max

# Network operations
filedo \\server\share test
filedo network \\nas\backup speed 100

# Batch operations
filedo from commands.txt
filedo batch script.lst

# Cleanup
filedo C:\temp clean
```

</td>
</tr>
</table>

---

## Secret Files (`.fd-sec`)

One file goes into one container, behind a password, and comes back out - from the command line, from the
Explorer menu, or from the **Protect** pages in the window. The original's true name, its real size and
its timestamps are sealed inside; the container itself reveals only its own size, its visible name, and
its timestamps (`rename` writes a nameless blob).

```bash
# Pack it (asks for the password twice, with no echo)
filedo report.docx secure

# Pack it and get rid of the original - the recoverable way, and the harder one
filedo report.docx secure del p:hunter2
filedo report.docx secure wipe p:hunter2

# Get it back under its sealed name, or into this folder
filedo report.fd-sec unsecure
filedo report.fd-sec unsecure here

# Open it in the program it belongs to, without unpacking it
filedo report.fd-sec reveal
```

Five things said plainly, because a security feature that oversells itself is worse than none:

- **An empty password is obfuscation only - no secrecy.** It is accepted, and every surface that takes a
  password says what it is worth while it is being typed.
- **`wipe` lowers the odds of recovery and promises nothing.** On SSDs and on copy-on-write or journaled
  file systems, overwriting a file in place does not guarantee the old blocks are gone.
- **A revealed copy lives in `%LOCALAPPDATA%\FileDO\reveal`**, read-only, readable by this account and the
  system only. It goes when you say so, or when the program that opened it lets go of it.
- **`unsecure start` - the double-click - puts its copy in that same protected folder**, not beside
  the container, and removes it when the console window closes. That copy is your own file rather
  than a read-only view, and if the program still has it open when the window closes, the next
  FileDO start sweeps it. Plain `unsecure` is the one that brings the file back for good.
- **A power loss leaves that copy on disk until the next FileDO start**, which sweeps it. Nothing else
  will.

There is no recovery key: a forgotten password is a lost file. The original is kept unless you ask for it
to go, and nothing is removed before the container has been written, read back and verified. Executables
and scripts are never launched out of a container - they are extracted and their location shown.

In the window, the **Protect** group carries the same three operations as pages: the password is masked,
typed twice when packing, and handed to `filedo.exe` out of sight - it reaches no command line, no run
report and no history file. A double-click on a `.fd-sec` does not open that window; it runs
**Unsecure and start** on the console, exactly as the Explorer menu entry of the same name does.

---

## Key Features

### **Fake Capacity Detection**
- **100-file verification** with 1% capacity each
- **Random position verification** - each file checked at unique random positions every time
- **Anti-sophisticated fake protection** - defeats controllers that preserve predictable data positions  
- **Readable patterns** - uses `ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` for easy corruption detection
- **Fast raw probe** (`probe`) - writes 32 markers via direct LBA access, completes in ~1 min (requires Admin)

### **Performance Testing**
- Real-world read/write speed measurement
- Memory-optimized streaming for large files
- Progress tracking with ETA calculations
- Configurable file sizes (1MB to 10GB)

### **Duplicate File Management**
- Multiple selection modes (oldest/newest/alphabetical)
- Flexible actions (delete/move duplicates)
- MD5 hash-based reliable identification
- Hash caching for faster repeated scans - the cache (`hash_cache.json`) lives next to the executable, and
  a cached hash is reused only while the file's size **and** modification time still match, so an edited
  file never counts as a duplicate
- Support for saving/loading duplicate lists

### **Copy Operations**
- **Starts at the first file** - a plain copy walks the tree once and copies as it goes; add
  `--precount` to count the tree first for exact totals and an ETA from the start
- **Progress tracking** with detailed ETA calculations
- **Timeout protection** for corrupted/slow filesystems (3-second timeout)
- **Preserves metadata** - file permissions and timestamps
- **Robust error handling** - continues copying even if individual files fail
- **Universal support** - works with devices, folders, network shares, and individual files

### **Fast Wipe Operations**
- **Ultra-fast method** - delete entire folder and recreate (milliseconds)
- **Standard fallback** - file-by-file deletion with progress for restricted folders
- **Metadata preservation** - maintains original folder permissions and timestamps
- **Smart error handling** - works with system folders and access restrictions
- **Universal compatibility** - supports devices, folders, and network shares
- **Confirmation guardrails** - `wipe` always asks `Type WIPE to continue` before
  deleting. Add `--force` (or `-y`) to skip the prompt in automation. Dangerous
  targets - drive/share roots, reparse points (junctions/symlinks) and the system
  TEMP folder - always require interactive confirmation and are never bypassed by
  `--force`.

### **Security Features**
- High-speed secure data wiping to prevent recovery (4.7+ GB/s)
- Fill operations with parallel writing and automatic cleanup
- Secret files: one file per `.fd-sec` container behind a password, with a bounded plaintext window when
  one is opened - see [Secret Files](#secret-files-fd-sec) for what each choice does and does not promise
- Batch processing for multiple targets
- Comprehensive operation history, with credentials redacted before anything is written to it
- **Context-aware interruption** - a graceful cancellation that still cleans up after itself

---

## Command Reference

### Target Types (Auto-detected)
| Pattern | Type | Example |
|---------|------|---------|
| `C:`, `D:` | Device | `filedo C: test` |
| `C:\folder` | Folder | `filedo C:\temp speed 100` |
| `\\server\share` | Network | `filedo \\nas\backup test` |
| `file.txt` | File | `filedo document.pdf info` |

### Operations
| Command | Purpose | Example |
|---------|---------|---------|
| `info` | Show detailed information | `filedo C: info` |
| `short` | Brief summary | `filedo D: short` |
| `test` | Fake capacity detection | `filedo E: test del` |
| `test N` | Test with N files (default 100) | `filedo D: test 1000` |
| `probe` | Fast raw-I/O probe (~1 min, needs Admin) | `filedo D: probe` |
| `speed <size>` | Performance testing | `filedo C: speed 500` |
| `fill [size]` | Fill with test data | `filedo D: fill 1000` |
| `clean` | Remove test files | `filedo C: clean` |
| `check-duplicates` | Find duplicate files | `filedo C: check-duplicates` |
| `cd [mode] [action]` | Check duplicates (short) | `filedo C: cd old del` |
| `copy <target>` | Copy files with progress | `filedo C: copy D:\backup` |
| `wipe` | Fast wipe folder contents | `filedo folder C:\temp wipe` |
| `from <file>` | Execute batch commands | `filedo from script.txt` |
| `hist` | Show operation history | `filedo hist` |

#### Fill Command Shortcuts
| Command | Equivalent | Purpose |
|---------|------------|---------|
| `filedo D: fill` | `filedo D: fill 100` | Fill with 100MB default |
| `filedo D: f` | `filedo D: fill 100` | Short form |
| `filedo D: f del` | `filedo D: fill 100 del` | With auto-delete |
| `filedo D: f d` | `filedo D: fill 100 delete` | Short auto-delete |

#### Copy & Wipe Shortcuts
| Command | Equivalent | Purpose |
|---------|------------|---------|
| `filedo copy A B` | `filedo folder A copy B` | Generic copy command |
| `filedo A cp B` | `filedo A copy B` | Short copy form |
| `filedo A w` | `filedo A wipe` | Short wipe form |



### Modifiers
| Flag | Purpose | Example |
|------|---------|---------|
| `del` | Auto-delete after operation | `filedo E: test del` |
| `nodel` | Keep test files | `filedo C: speed 100 nodel` |
| `short` | Brief output only | `filedo D: speed 100 short` |
| `max` | Maximum size (10GB) | `filedo C: speed max` |
| `old` | Keep newest as original (for cd) | `filedo D: cd old del` |
| `new` | Keep oldest as original (for cd) | `filedo E: cd new move F:` |
| `abc` | Keep alphabetically last (for cd) | `filedo C: cd abc` |
| `xyz` | Keep alphabetically first (for cd) | `filedo C: cd xyz list dups.lst` |

---

## GUI Application

**FileDO GUI** (`filedo_win.exe`) - a VB.NET Windows Forms shell: a rail of jobs on the left and one numbered page per job - what to work on, which parameters, then check and run - plus a **Command** page, the expert builder that can assemble **any** FileDO command and run it. What the window holds:

- **5-language UI** (EN / RU / UA / DE / FR, remembered) with an **inline help panel**: what the selected operation and flags do, the expected result, and an example for the target
- **Target selection** (device / folder / network / file); for **device** the path becomes a **drive-letter picker**, and choosing the system drive explains the `%TEMP%\FileDO_Operations` redirect
- **Every operation** on the Command page, in one dropdown: info, speed, test, fill, clean, cd (duplicates), the full copy family (copy / fastcopy / synccopy / balanced / maxcopy / smartcopy / safecopy), wipe, compare, check, probe, recover, from, hist
- **Source + destination** fields for copy and compare
- **Context-aware flags** (max, del, nodel, short, hist, `--force` for wipe) and duplicate/compare delete rules
- **A page per job** in the default window - capacity, speed, info, damaged-files check, raw probe, recover, duplicates, compare, clean, copy, fill, wipe and the three secret-file jobs - each carrying every option the CLI takes for it, including all twenty-nine `check` flags
- **Live command preview** in an editable box on the Command page - tweak by hand for anything the builder doesn't cover
- **Browse** buttons for paths, **Copy command** to clipboard, and **RUN** in a console window
- Finds `filedo.exe` next to itself or on `PATH` (works under the Store package and portable zip alike)
- **About page**: build stamp, author, and links to the site, the source, the issue tracker, the privacy page and the author's other tools
- **Send logs to the author** - the one button on that page packs the FileDO logs found on this machine into a single zip, opens the folder with it selected, puts its path on the clipboard, and opens your default mail program already addressed and titled. You see the zip first, and nothing is sent until you press Send yourself

```powershell
filedo_win              # if installed via winget / Store (on PATH)
filedo_win.exe          # next to filedo.exe in the portable zip
```

The **Protect** pages handle `.fd-sec` secret files - make a file secret, get the original back, or open it without unpacking. The password is masked and never enters a command line. Handing the window a `.fd-sec` path directly opens it on that container's page (`filedo_win.exe C:\a\x.fd-sec`); a double-click in Explorer does not go through the window at all - it runs **Unsecure and start** on the console, as described above. History, Settings and About have pages of their own; the older command-builder window is retired, and a hand-written command line goes on the **Command** page.

**Ships everywhere:** the GUI is included in the winget package, the portable zip, the MSI, and is the clickable tile of the Microsoft Store (MSIX) package.

---

## Advanced Features

### Batch Processing
Create `commands.txt`:
```text
# Multiple device check
device C: info
device D: test del
device E: speed 100
folder C:\temp clean
```

Run: `filedo from commands.txt`

Exit codes (all non-container verbs): **0 passed or done, 1 ran and found a defect, 2 could not be verified.**

### History Tracking
```bash
filedo hist              # Show last 10 operations
filedo history           # Show command history
# History automatically logged for all operations
```

### Interruption Support
```bash
# All long-running operations support Ctrl+C interruption
# Graceful cancellation with cleanup
# Context-aware interruption at optimal points
```

### Network Operations
```bash
# SMB shares and network drives
filedo \\server\backup speed 100
filedo \\nas\storage test del
filedo network \\pc\share info
```

---

## Important Notes

> **Fake Capacity Detection**: Creates 100 files (1% capacity each) with **context-aware interruption support**. Uses modern random verification patterns and optimized buffer management for reliable detection.

> **Enhanced Interruption**: All long-running operations support **Ctrl+C graceful cancellation** with automatic cleanup. Context-aware interruption checks at optimal points for immediate responsiveness.

> **Secure Wiping**: `fill <size> del` overwrites free space with optimized buffer management and context-aware writing for secure data deletion.

> **Test Files**: Creates `FILL_*.tmp` and `speedtest_*.txt` files. Use `clean` command to remove them automatically.

> **Modular Architecture**: Refactored with separate `capacitytest` and `fileduplicates` packages for better maintainability and extensibility.

> **CHECK Command**: Marks a file as damaged if initial read delay exceeds 2.0s. Allows a one-time warm-up up to 10.0s. Writes immediately to `skip_files.list` and respects existing entries. No `damaged_files.log`.

---

## Examples by Use Case

<details>
<summary><b>Verify USB/SD Card Authenticity</b></summary>

```bash
# Quick test with cleanup
filedo E: test del

# Detailed test, keep files for analysis  
filedo F: test

# Check drive info first
filedo E: info
```
</details>

<details>
<summary><b>Performance Benchmarking</b></summary>

```bash
# Quick 100MB test
filedo C: speed 100 short

# Maximum performance test (10GB)
filedo D: speed max

# Network speed test
filedo \\server\backup speed 500
```
</details>

<details>
<summary><b>Secure Data Wiping</b></summary>

```bash
# Fill 5GB then secure delete
filedo C: fill 5000 del

# Clean existing test files
filedo D: clean

# Before disposing drive
filedo E: fill max del
```
</details>

<details>
<summary><b>Find & Manage Duplicates</b></summary>

```bash
# Find duplicates in current directory
filedo . check-duplicates

# Find and delete older duplicates
filedo C: cd old del

# Find and move newer duplicates to backup
filedo E: cd new move E:\Backup

# Save duplicates list for later processing
filedo D: cd list duplicates.lst

# Process saved list with specific action
filedo cd from list duplicates.lst xyz del
```
</details>

---

## Technical Details

### Architecture
- **Modular Design**: Separated into specialized packages for better maintainability
- **Context-Aware Operations**: All long-running operations support graceful cancellation
- **Unified Interface**: Common `Tester` interface for all storage types
- **Memory Optimization**: Streaming operations with optimized buffer management
- **Cross-Platform**: Primary Windows support with portable Go codebase

### Package Structure
```
FileDO/
├── main.go                    # Application entry point
├── capacitytest/             # Capacity testing module
│   ├── types.go              # Core interfaces and types
│   ├── test.go               # Main testing logic
│   └── utils.go              # Utility functions and verification
├── fileduplicates/           # Duplicate file management
│   ├── types.go              # Duplicate detection interfaces
│   ├── duplicates.go         # Core duplicate logic
│   ├── duplicates_impl.go    # Implementation details
│   └── worker.go             # Background processing
├── filedo_win_vb/           # VB.NET GUI application
│   ├── FileDOGUI.sln        # Visual Studio solution
│   ├── Program.vb           # Entry point; the window is ShellForm.vb
│   └── bin/                 # Compiled GUI executable
├── cmd/
│   ├── filedo/              # Main CLI sources
│   ├── filedo-check/        # Standalone health check utility
│   ├── filedo-fill/         # Standalone fill utility
│   └── filedo-test/         # Standalone test utility
├── capacitytest/             # Capacity-testing shared logic
├── fileduplicates/           # Duplicate detection logic
└── helpers/                  # Shared helpers
```

### Key Features
- **Enhanced InterruptHandler**: Thread-safe interruption with context support
- **Optimized Buffer Management**: Dynamic buffer sizing for optimal performance
- **Comprehensive Testing**: Fake capacity detection with random verification
- **Duplicate Detection**: MD5-based file comparison with caching
- **Batch Processing**: Script execution with error handling
- **History Logging**: JSON-based operation tracking

---

## Version History

**v2609241700** (Current)
- **Secret files (`.fd-sec`)**: pack one file into a password-protected container and get it back - `secure`, `unsecure`, `reveal` - from the command line, the Explorer menu or the window's Protect pages; the original's name, size and timestamps are sealed inside
- **Explorer integration**: the setup EXE adds a `File DO..` right-click group (Secure, Unsecure, Wipe this file, Check this file, Info) and the `.fd-sec` document type; `filedo fdsec register` / `unregister` does the same without an installer
- **GUI**: a new window - a rail of jobs with one page per job, each carrying every CLI option, plus Command, History, Settings and About pages; the old command-builder window is retired
- **Copy**: copying starts at the first file; `--precount` counts the tree first for exact totals and an ETA
- **CLI**: one set of exit codes - 0 passed or done, 1 found a defect, 2 could not be verified; credentials are redacted before anything reaches the history
- **Distribution**: FileDO is on the Microsoft Store; `THIRD-PARTY-NOTICES.txt` ships in the zip, the MSI and the Store package
- **Docs**: new guides - secret files and "Windows warned you about FileDO"

**v2607301014** (Previous)
- **GUI**: 5-language interface (English, Russian, Ukrainian, German, French) with runtime language switching and an app icon
- **GUI**: About window with a send-logs-to-the-author action
- **Privacy**: hosted privacy page, linked from the site and the Store listing
- **CLI**: new "pleasantly paranoid" tagline and clearer usage text
- **Docs**: redesigned site with new step-by-step guides (storage check, files and copies, GUI command builder)
- **Project**: sources reorganized under `cmd/` (filedo, filedo-check, filedo-fill, filedo-test); separate build and release flows

**v2606120121**
- **Wipe**: hardened wipe safety checks
- **Duplicates**: fixed duplicate-cache correctness and parallelism
- **Copy**: trimmed copy directory walks

**v2605152056**
- **Installer**: setup EXE (a WiX bundle carrying the MSI) plus the bare MSI; application icon embedded in all EXEs and the installed-programs entry
- **Store**: Microsoft Store submission spec and social preview image

**v2604272228**
- **Distribution**: first winget release (SerZhyAle.FileDO) and GitHub Actions release workflow
- **Binaries**: PE version info and Windows application manifest embedded in all binaries
- **Docs**: GitHub Pages redesigned and simplified

**v2507112115**
- **Major Refactoring**: Extracted capacity testing logic into dedicated `capacitytest` package
- **Enhanced Interruption**: Added context-aware cancellation with thread-safe `InterruptHandler`
- **Improved Performance**: Optimized buffer management and verification algorithms
- **Better Architecture**: Modular design with clear separation of concerns
- **VB.NET GUI**: Updated Windows Forms GUI application with better integration

**v2507082120**
- Added duplicate file detection and management
- Multiple duplicate selection modes (old/new/abc/xyz)
- Hash caching for faster duplicate scanning
- Support for saving/loading duplicate lists
- GUI application with duplicate management features

**v2507062220** (Earlier)
- Enhanced verification system with multi-position checking
- Readable text patterns for corruption detection
- Improved progress display and protection mechanisms
- Bug fixes and error handling improvements

---

<div align="center">

**FileDO v2609241700** - Advanced File & Storage Operations Tool

Created by **sza@ukr.net** | [MIT License](LICENSE) | [GitHub Repository](https://github.com/SerZhyAle/FileDO) | [Universal Agent Kit](https://serzhyale.github.io/universal-agent-kit/)

---

### Recent Improvements

- **Modular Architecture**: Refactored into specialized packages (`capacitytest`, `fileduplicates`)
- **Enhanced Interruption**: Context-aware cancellation with graceful cleanup
- **Thread-Safe Operations**: Improved `InterruptHandler` with mutex protection
- **Better Performance**: Optimized buffer management and verification algorithms
- **Updated GUI**: VB.NET Windows Forms application with improved integration

</div>




