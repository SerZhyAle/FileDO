# FileDO - Advanced File & Storage Operations Tool

<div align="center">

[![Go Report Card](https://goreportcard.### 🔍 **Duplicate Fil### 🔍 **Duplicate File Management**
- Multiple selection modes (oldest/newest/alphabetical)
- Flexible actions (delete/move duplicates)
- MD5 hash-based reliable identification
- Hash caching for faster repeated scans
- Support for saving/loading duplicate lists

### 📋 **Copy Operations**
- **Progress tracking** with detailed ETA calculations
- **Timeout protection** for corrupted/slow filesystems (3-second timeout)
- **Preserves metadata** - file permissions and timestamps
- **Robust error handling** - continues copying even if individual files fail
- **Universal support** - works with devices, folders, network shares, and individual files

### 🧹 **Fast Wipe Operations**
- **Ultra-fast method** - delete entire folder and recreate (milliseconds)
- **Standard fallback** - file-by-file deletion with progress for restricted folders
- **Metadata preservation** - maintains original folder permissions and timestamps
- **Smart error handling** - works with system folders and access restrictions
- **Universal compatibility** - supports devices, folders, and network shares

### 🛡️ **Security Features**
- High-speed secure data wiping to prevent recovery (4.7+ GB/s)
- Fill operations with parallel writing and automatic cleanup
- Batch processing for multiple targets
- Comprehensive operation history**
- **Built-in duplicate detection** - integrated into main application
- **Multiple selection modes** (oldest/newest/alphabetical)
- **Flexible actions** (delete/move duplicates)
- **MD5 hash-based reliable identification**
- **Hash caching** for faster repeated scans
- **Save/load duplicate lists** for batch processing
- **Modular architecture** with dedicated fileduplicates package

### 🛡️ **Security Features**
- **High-speed secure data wiping** to prevent recovery
- **Fill operations** with optimized buffer management
- **Batch processing** for multiple targets
- **Comprehensive operation history** with JSON logging
- **Context-aware interruption** - graceful cancellation supportthub.com/SerZhyAle/FileDO)](https://goreportcard.com/report/github.com/SerZhyAle/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2507112115-blue.svg)](https://github.com/SerZhyAle/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/SerZhyAle/FileDO)

**🔍 Storage Testing • 🚀 Performance Analysis • 🛡️ Security Wiping • 🎯 Fake Capacity Detection • 📁 Duplicate Management**

</div>

---

## 🎯 Quick Start

### ⚡ Most Common Tasks

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

# Fast wipe folder contents
filedo folder C:\Temp wipe
filedo folder D:\Cache w

# Show device info
filedo C: info
```

### � Compare & Cleanup

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

### 🧪 Health CHECK (fast read check)

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

### 📥 Installation

#### Option 1 — winget (recommended)

```powershell
winget install SerZhyAle.FileDO
# short form (works once Microsoft indexes the moniker):
winget install filedo
```

This installs all four CLI tools (`filedo`, `filedo_check`, `filedo_fill`, `filedo_test`) and adds them to your `PATH`. To upgrade later:

```powershell
winget upgrade SerZhyAle.FileDO
```

To uninstall:

```powershell
winget uninstall SerZhyAle.FileDO
```

#### Option 2 — Manual download

1. **Download**: Grab the latest `FileDO-<version>-windows-x64.zip` from [Releases](https://github.com/SerZhyAle/FileDO/releases/latest)
2. **Extract** anywhere (e.g. `C:\Tools\FileDO`)
3. **Optional**: add the folder to your `PATH` so you can call `filedo` from any directory
4. **GUI Option**: also download `filedo_win.exe` from `exe_to_download/` for the VB.NET visual interface
5. **Run**: execute from command line or GUI

#### Option 3 — Build from source

```powershell
git clone https://github.com/SerZhyAle/FileDO.git
cd FileDO
.\build.ps1   # or build.bat
```

Requires Go 1.24+. Builds all four executables into `exe_to_download/`.








---

## 🔧 Core Operations

<table>
<tr>
<td width="50%">

### 💾 Device Testing
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

### 📁 File & Folder Operations
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

## 🌟 Key Features

### 🎯 **Fake Capacity Detection**
- **100-file verification** with 1% capacity each
- **Random position verification** - each file checked at unique random positions every time
- **Anti-sophisticated fake protection** - defeats controllers that preserve predictable data positions  
- **Readable patterns** - uses `ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789` for easy corruption detection
- **Fast raw probe** (`probe`) - writes 32 markers via direct LBA access, completes in ~1 min (requires Admin)

### ⚡ **Performance Testing**
- Real-world read/write speed measurement
- Memory-optimized streaming for large files
- Progress tracking with ETA calculations
- Configurable file sizes (1MB to 10GB)

### � **Duplicate File Management**
- Multiple selection modes (oldest/newest/alphabetical)
- Flexible actions (delete/move duplicates)
- MD5 hash-based reliable identification
- Hash caching for faster repeated scans
- Support for saving/loading duplicate lists

### �🛡️ **Security Features**
- High-speed secure data wiping to prevent recovery (4.7+ GB/s)
- Fill operations with parallel writing and automatic cleanup
- Batch processing for multiple targets
- Comprehensive operation history

---

## 💻 Command Reference

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

## 🖥️ GUI Application

**FileDO GUI** (`filedo_win.exe`) - VB.NET Windows Forms application provides user-friendly interface:

- ✅ **Visual target selection** with radio buttons (Device/Folder/Network/File)
- ✅ **Operation dropdown** (Info, Speed, Fill, Test, Clean, Check Duplicates)
- ✅ **Parameter input** with validation
- ✅ **Real-time command preview** showing equivalent CLI command
- ✅ **Browse button** for easy path selection
- ✅ **Progress tracking** with real-time output
- ✅ **One-click execution** with output display

```bash
# Run from filedo_win_vb folder
filedo_win.exe          # Windows GUI interface
```

**Features:**
- Built with VB.NET Windows Forms for native Windows experience
- Automatic command validation and parameter checking
- Real-time output display with color coding
- Integration with main CLI application

---

## 🔍 Advanced Features

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

## ⚠️ Important Notes

> **🎯 Fake Capacity Detection**: Creates 100 files (1% capacity each) with **context-aware interruption support**. Uses modern random verification patterns and optimized buffer management for reliable detection.

> **🔥 Enhanced Interruption**: All long-running operations support **Ctrl+C graceful cancellation** with automatic cleanup. Context-aware interruption checks at optimal points for immediate responsiveness.

> **�️ Secure Wiping**: `fill <size> del` overwrites free space with optimized buffer management and context-aware writing for secure data deletion.

> **🟢 Test Files**: Creates `FILL_*.tmp` and `speedtest_*.txt` files. Use `clean` command to remove them automatically.

> **🔵 Modular Architecture**: Refactored with separate `capacitytest` and `fileduplicates` packages for better maintainability and extensibility.

> **🧪 CHECK Command**: Marks a file as damaged if initial read delay exceeds 2.0s. Allows a one-time warm-up up to 10.0s. Writes immediately to `skip_files.list` and respects existing entries. No `damaged_files.log`.

---

## 📖 Examples by Use Case

<details>
<summary><b>🔍 Verify USB/SD Card Authenticity</b></summary>

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
<summary><b>⚡ Performance Benchmarking</b></summary>

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
<summary><b>🛡️ Secure Data Wiping</b></summary>

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
<summary><b>🔍 Find & Manage Duplicates</b></summary>

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

## 🏗️ Technical Details

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
│   ├── MainForm.vb          # Main form logic
│   └── bin/                 # Compiled GUI executable
├── command_handlers.go       # Command processing
├── device_windows.go         # Device-specific operations
├── folder.go                 # Folder operations
├── network_windows.go        # Network storage operations
├── interrupt.go              # Interruption handling
├── progress.go               # Progress tracking
├── main_types.go             # Legacy type definitions
├── history.json              # Operation history
└── hash_cache.json           # Hash cache for duplicates
```

### Key Features
- **Enhanced InterruptHandler**: Thread-safe interruption with context support
- **Optimized Buffer Management**: Dynamic buffer sizing for optimal performance
- **Comprehensive Testing**: Fake capacity detection with random verification
- **Duplicate Detection**: MD5-based file comparison with caching
- **Batch Processing**: Script execution with error handling
- **History Logging**: JSON-based operation tracking

---

## 🔄 Version History

**v2507112115** (Current)
- **Major Refactoring**: Extracted capacity testing logic into dedicated `capacitytest` package
- **Enhanced Interruption**: Added context-aware cancellation with thread-safe `InterruptHandler`
- **Improved Performance**: Optimized buffer management and verification algorithms
- **Better Architecture**: Modular design with clear separation of concerns
- **VB.NET GUI**: Updated Windows Forms GUI application with better integration

**v2507082120** (Previous)
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

**FileDO v2507112115** - Advanced File & Storage Operations Tool

Created by **sza@ukr.net** | [MIT License](LICENSE) | [GitHub Repository](https://github.com/SerZhyAle/FileDO)

---

### 🚀 Recent Improvements

- **🔧 Modular Architecture**: Refactored into specialized packages (`capacitytest`, `fileduplicates`)
- **⚡ Enhanced Interruption**: Context-aware cancellation with graceful cleanup
- **🛡️ Thread-Safe Operations**: Improved `InterruptHandler` with mutex protection
- **📊 Better Performance**: Optimized buffer management and verification algorithms
- **🖥️ Updated GUI**: VB.NET Windows Forms application with improved integration

</div>




