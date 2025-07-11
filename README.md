# FileDO - Advanced File & Storage Operations Tool

<div align="center">

[![Go Report Card](https://goreportcard.com/badge/github.com/sza/FileDO)](https://goreportcard.com/report/github.com/sza/FileDO)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Version](https://img.shields.io/badge/Version-v2507082120-blue.svg)](https://github.com/sza/FileDO)
[![Windows](https://img.shields.io/badge/Platform-Windows-lightgrey.svg)](https://github.com/sza/FileDO)

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
filedo_cd D: old del

# Show device info
filedo C: info
```

### 📥 Installation

1. **Download**: Get `filedo.exe` from releases
2. **GUI Option**: Also download `filedo_win.exe` for visual interface


5. **Duplicate Tool**: Download `filedo_cd.exe` for finding and managing duplicate files
6. **Run**: Place all files in same folder and execute








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
| `fill [size]` | Fill with test data (default 100MB) | `filedo D: fill`, `filedo D: f` |
| `clean` | Remove test files | `filedo C: clean` |
| `cd [mode] [action]` | Check duplicates | `filedo C: cd old del` |

#### Fill Command Shortcuts
| Command | Equivalent | Purpose |
|---------|------------|---------|
| `filedo D: fill` | `filedo D: fill 100` | Fill with 100MB default |
| `filedo D: f` | `filedo D: fill 100` | Short form |
| `filedo D: f del` | `filedo D: fill 100 del` | With auto-delete |
| `filedo D: f d` | `filedo D: fill 100 delete` | Short auto-delete |



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

**FileDO GUI** (`filedo_win.exe`) provides point-and-click interface:

- ✅ **Visual target selection** (checkboxes for Device/Folder/Network/File)
- ✅ **Operation dropdown** (Info, Speed, Fill, Test, Clean, Check Duplicates)
- ✅ **Duplicate file management** (selection modes: old/new/abc/xyz, actions: delete/move)
- ✅ **Real-time command preview**
- ✅ **Browse button** for path selection
- ✅ **One-click execution**

```bash
filedo_win.exe          # Normal mode
filedo_win.exe -debug   # Debug logging
```

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
filedo hist            # Show last 10 operations
filedo C: info hist    # Run command with history logging
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

> **🎯 Fake Capacity Detection**: Creates 100 files (1% capacity each) with **random position verification**. Each file checked at unique random internal positions every time, defeating sophisticated controllers that preserve predictable data locations.

> **🟡 High-Speed Secure Wiping**: `fill <size> del` overwrites free space at 4.7+ GB/s with parallel writing to prevent data recovery. Use before disposing drives or after deleting sensitive data.

> **🟢 Test Files**: Creates `FILL_*.tmp` and `speedtest_*.txt` files. Use `clean` command to remove them.

> **🔵 System Drive Protection**: When targeting the system drive (`C:`), operations are automatically redirected to `C:\TEMP` to prevent root folder access issues. The directory is created if it doesn't exist.

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
# Find duplicates in folder
filedo_cd C:\data

# Find and delete older duplicates
filedo C: cd old del
filedo_cd D: old del

# Find and move newer duplicates to backup
filedo E: cd new move E:\Backup
filedo_cd F: new move F:\Archive

# Save duplicates list for later processing
filedo_cd D: list duplicates.lst

# Process saved list with specific action
filedo_cd from list duplicates.lst xyz del
```
</details>

---

## 🏗️ Technical Details

### Architecture
- **Unified Interface**: `FakeCapacityTester` for all storage types
- **Memory Optimization**: Streaming operations for large files
- **Cross-Platform**: Primary Windows support, portable Go codebase

### File Structure
```
FileDO/
├── filedo.exe           # Main CLI application


├── filedo_cd.exe        # Dedicated duplicates checking tool
├── filedo_win.exe       # GUI application
├── cmd/                 # Command entry points
│   ├── filedo/          # Main CLI source


│   └── filedo_cd/       # Duplicates tool source
├── main.go              # Legacy entry point
├── types.go             # Core interfaces and logic
├── device_windows.go    # Device operations
├── folder.go            # Folder operations
├── duplicates.go        # Duplicates operations
├── network_windows.go   # Network operations
├── history.json         # Operation history
└── hash_cache.json      # Hash cache for duplicate detection
```

---

## 🔄 Version History

**v2507082120** (Current)
- Added duplicate file detection and management (filedo_cd)
- Multiple duplicate selection modes (old/new/abc/xyz)
- Duplicate management in GUI application
- Hash caching for faster duplicate scanning
- Support for saving/loading duplicate lists

**v2507062220**
- Enhanced verification system with multi-position checking
- Readable text patterns for corruption detection
- Improved progress display and Chinese controller protection
- Bug fixes and error handling improvements

---

<div align="center">

**FileDO v2507062220** - Advanced File & Storage Operations Tool

Created by **sza@ukr.net** | [MIT License](LICENSE) | [Contributing Guidelines](CONTRIBUTING.md)

</div>
