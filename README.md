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

### 📥 Installation

1. **Download**: Get `filedo.exe` from releases
2. **GUI Option**: Also download `filedo_win.exe` for visual interface (VB.NET)
3. **Run**: Execute from command line or GUI








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




