# FileDO - Advanced File & Storage Operations Tool

**FileDO v2507050402** is a comprehensive command-line utility for Windows that provides advanced file, folder, device, and network operations. It specializes in storage capacity verification, performance testing, secure data wiping, and counterfeit storage device detection.

## 📊 GUI Application

**FileDO GUI** (`filedo_win.exe`) provides a user-friendly Windows interface for FileDO operations:

- **VB.NET Framework 4.8** - Stable native Windows interface
- **Checkbox-based selection** - Intuitive target and operation selection
- **Real-time command preview** - See the exact command before execution
- **Smart operation filtering** - Only shows valid operations for selected target
- **Auto path defaults** - C:\ for device/folder, empty for file/network
- **Additional flags support** - max, help, hist, short options
- **Debug logging** - Detailed logging when launched with `-debug`

### GUI Usage

```cmd
filedo_win.exe              # Normal mode
filedo_win.exe -debug       # Debug mode with logging
```

## 🚀 Key Features

### Core Capabilities

- **Multi-Platform Storage Analysis**: Devices, folders, files, and network paths
- **Fake Capacity Detection**: Advanced 100-file testing to detect counterfeit USB drives and SD cards
- **Performance Testing**: Real-world read/write speed measurement with configurable file sizes
- **Secure Data Wiping**: Fill and delete operations for preventing data recovery
- **Batch Operations**: Execute multiple commands from script files
- **Command History**: Comprehensive logging with 1000-entry history
- **Auto-Detection**: Intelligent path type detection (device/folder/file/network)

### Advanced Features

- **Memory-Optimized**: Streaming file writing for large capacity tests
- **Unified Architecture**: Single interface for all storage types via `FakeCapacityTester`
- **Two-Stage Verification**: Immediate verification during file creation + final integrity check
- **Smart Error Handling**: Automatic test termination and cleanup on verification failures
- **Real-Time Progress**: Live progress display with format "Test: X/Y (speed) - data ETA: time"
- **Detailed Diagnostics**: Comprehensive error reporting with file paths and corruption details
- **User-Friendly Help**: Comprehensive 79-line help with practical examples

## 📦 Installation

### Option 1: Use Pre-built Binaries (Recommended)

Pre-built executables are available in the repository:

1. **Download from GitHub:**

   ```cmd
   git clone https://github.com/yourusername/FileDO.git
   cd FileDO
   # Use the included filedo.exe (latest build v2507050402)
   # Use the included filedo_win.exe (GUI application)
   ```

2. **Or download executables directly:**
   - Navigate to the repository files on GitHub
   - Download `filedo.exe` (command-line tool)
   - Download `filedo_win.exe` (GUI application)
   - Place them in the same directory and run

### GUI Application Setup

The GUI application (`filedo_win.exe`) requires:
- **.NET Framework 4.8** (pre-installed on Windows 10/11)
- `filedo.exe` in the same directory
- Optional: `-debug` parameter for detailed logging

### Option 2: Build from Source

#### Command-line tool:
```cmd
git clone https://github.com/yourusername/FileDO.git
cd FileDO
go build -o filedo.exe
```

#### GUI application:
```cmd
cd filedo_win_vb
msbuild FileDOGUI.vbproj /p:Configuration=Release
# Output: bin\Release\filedo_win.exe
```
```

### Requirements

- **Windows OS** (primary target platform)
- **For building from source**: Go 1.19+ installed
- **For pre-built binary**: No additional requirements

## 🎯 Quick Start

**Ready to use immediately!** Download the repository and run:

```cmd
# Clone repository
git clone https://github.com/yourusername/FileDO.git
cd FileDO

# Command-line usage (filedo.exe)
.\filedo.exe device C: info          # Get device information
.\filedo.exe device E: test          # Test USB drive for fake capacity  
.\filedo.exe folder C:\temp speed 100 # Test folder write speed
.\filedo.exe folder C:\temp clean    # Clean up test files

# GUI usage (filedo_win.exe)
.\filedo_win.exe                     # Launch GUI interface
.\filedo_win.exe -debug              # Launch with debug logging
```

**Or download executables only:**

1. Go to the repository on GitHub
2. Download `filedo.exe` (command-line tool)
3. Download `filedo_win.exe` (GUI application)  
4. Place both files in the same directory and run

### GUI Interface

The GUI application provides an intuitive interface with:

- **Target selection**: Device, Folder, Network, File (checkboxes)
- **Operation selection**: None, Info, Speed, Fill, Test, Clean
- **Path input**: Manual entry or Browse button
- **Size configuration**: For speed/fill operations (default: 100MB)
- **Flags**: max, help, hist, short options
- **Command preview**: Real-time command display
- **One-click execution**: RUN button launches command in terminal

## 📚 Usage Examples

### 1. Device Operations (Hard drives, USB drives, SD cards)

**Basic Information:**

```cmd
filedo.exe C:                    # Show detailed device information
filedo.exe device D: info        # Show detailed device information  
filedo.exe device E: short       # Show brief device summary
```

**Performance Testing:**

```cmd
filedo.exe C: speed 100          # Test write speed with 100MB file
filedo.exe device D: speed max   # Test write speed with 10GB file
filedo.exe device E: speed 500 short # Quick speed test (results only)
filedo.exe device F: speed 1000 nodel # Test but keep the test file
```

**Fake Capacity Detection:**

```cmd
filedo.exe C: test               # Test for fake capacity (100 files, 1% each)
filedo.exe device D: test del    # Test capacity and auto-delete files
```

**Space Management:**

```cmd
filedo.exe C: fill 500           # Fill device with 500MB files until full
filedo.exe device D: fill 1000 del # Fill and auto-delete (secure wipe)
filedo.exe device E: clean       # Delete all test files (FILL_*, speedtest_*)
```

### 2. Folder Operations (Local directories)

**Information & Analysis:**

```cmd
filedo.exe .                     # Show current folder information
filedo.exe C:\Temp info          # Show detailed folder information
filedo.exe folder D:\Data short  # Show brief folder summary
```

**Performance Testing:**

```cmd
filedo.exe C:\Temp speed 100     # Test folder write speed with 100MB
filedo.exe folder D:\Data speed max # Test with 10GB file
filedo.exe folder . speed 200 short # Quick test (results only)
```

**Capacity Testing:**

```cmd
filedo.exe C:\Temp test          # Test folder capacity (100 files)
filedo.exe folder D:\Data test del # Test and auto-delete files
```

### 3. Network Operations (SMB shares, network drives)

**Information & Analysis:**

```cmd
filedo.exe \\server\share        # Show network path information
filedo.exe network \\pc\folder info # Detailed network info
```

**Performance Testing:**

```cmd
filedo.exe \\server\share speed 100 # Test network speed with 100MB
filedo.exe network \\pc\data speed max # Test with 10GB transfer
filedo.exe network \\server\temp speed 500 short # Quick network test
```

**Capacity Testing:**

```cmd
filedo.exe \\server\share test   # Test network storage capacity
filedo.exe network \\pc\backup test del # Test and auto-cleanup
```

### 4. Batch Operations

**Creating Batch Scripts:**

Create a file `test_all.txt` with:

```text
# Test script for multiple devices
device C: info
device D: test del
folder C:\Temp speed 100
network \\server\share info
```

**Running Batch Scripts:**

```cmd
filedo.exe from test_all.txt     # Execute commands from file
filedo.exe batch script.lst      # Same as 'from' command
```

### 5. History & Monitoring

```cmd
filedo.exe hist                  # Show last 10 operations
filedo.exe history               # Show command history
```

## 🔧 Command Options & Modifiers

### Output Control

- `short`, `s` → Show brief/summary output only
- `info`, `i` → Show detailed information (default)

### File Management

- `del`, `delete`, `d` → Auto-delete test files after successful operation
- `nodel`, `nodelete` → Keep test files on target (don't delete)
- `clean`, `cln`, `c` → Delete all existing test files

### Size Specifications

- `<number>` → Size in megabytes (e.g., 100, 500, 1000)
- `max` → Use maximum size (10GB for speed tests)

## 🛡️ Security Features

### Secure Data Wiping

FileDO provides secure data wiping capabilities to prevent data recovery:

```cmd
# Fill device with data then securely delete
filedo.exe C: fill 5000 del      # Fill 5GB then secure delete

# Clean existing space
filedo.exe device D: clean       # Remove all test files
```

**Use Cases:**

- Before disposing of computers or hard drives
- After deleting sensitive files
- Preparing storage devices for resale
- Compliance with data protection regulations

## 🔍 Fake Capacity Detection

### How It Works

FileDO's fake capacity detection creates 100 test files, each representing around 1% of the free storage capacity. This method effectively detects counterfeit storage devices that report false sizes.

**Detection Process:**

1. **Analysis Phase**: Calculate file size based on available storage
2. **Write Phase**: Create 100 files with unique content and immediate verification
3. **Final Verification Phase**: Complete integrity check of all created files
4. **Report Phase**: Display results with detailed diagnostics if needed

**Advanced Verification System:**

- **Two-Stage Verification**: Each file is verified immediately after creation, then all files undergo final verification
- **Immediate Error Detection**: Test stops immediately if any file fails verification during creation
- **Auto-Cleanup on Errors**: All created files are automatically deleted if test fails
- **Progress Format**: Real-time progress display as "Test: X/Y (speed) - data ETA: time"
- **Final Output**: Single summary line "Verified N files - ✅ OK" on success, or detailed error information on failure

**Error Handling & Diagnostics:**

When verification fails, FileDO provides comprehensive diagnostics:

- Exact file path where error occurred
- Expected vs. actual file header content
- Detailed error description (corruption, device failure, fake capacity)
- Information about automatic cleanup performed

### Common Scenarios

```cmd
# Test USB drive authenticity
filedo.exe E: test del           # Check if USB is fake, auto-cleanup

# Test SD card capacity
filedo.exe F: test               # Check SD card, keep files for manual review

# Network storage verification
filedo.exe \\server\backup test  # Verify network storage capacity
```

## 📋 Practical Examples

### Quick Device Check

```cmd
filedo.exe D: short              # Fast overview of drive D:
```

### USB Drive Verification

```cmd
filedo.exe E: test del           # Check if USB is fake, auto-cleanup
```

### Network Speed Test

```cmd
filedo.exe \\server\backup speed max short # Max speed test, brief results
```

### Secure Space Wiping

```cmd
filedo.exe C: fill 5000 del      # Fill 5GB then secure delete (data recovery prevention)
```

### Batch Testing Multiple Locations

Create file 'test_all.txt' with:

```text
# Test script for multiple devices
device C: info
device D: test del
folder C:\Temp speed 100
network \\server\share info
```

Run:

```cmd
filedo.exe from test_all.txt
```

## 📖 Important Notes

### Fake Capacity Detection

The 'test' command creates 100 files, each 1% of total capacity, to detect counterfeit storage devices that report false sizes.

### Secure Wiping

Use 'fill <size> del' to overwrite free space and prevent recovery of previously deleted files.

### Test Files

Operations create files named FILL_#####_ddHHmmss.tmp and speedtest_*.txt. Use 'clean' to remove them.

### Batch Files

Commands in batch files support # comments and empty lines. Each line should contain one complete filedo command.

### Path Detection

FileDO automatically detects path types:

- C:, D:, etc. → Device operations
- \\server\share → Network operations  
- C:\folder, ./dir → Folder operations
- file.txt → File operations

### History

All operations are logged. Use 'hist' flag with any command to enable detailed history logging: `filedo.exe C: info hist`

## 🆘 Help & Support

```cmd
filedo.exe ?                     # Show comprehensive help
filedo.exe help                  # Show comprehensive help
```

## 🏗️ Architecture

### Core Components

- **Unified Interface**: `FakeCapacityTester` interface for all storage types
- **Memory Optimization**: Streaming file operations for large capacity tests
- **Error Handling**: Comprehensive error reporting and batch execution recovery
- **History System**: JSON-based operation logging with 1000-entry limit

### File Structure

```text
FileDO/
├── main.go                 # Main entry point and CLI parsing
├── types.go               # Core interfaces and unified test logic
├── device_windows.go      # Device-specific operations
├── folder.go             # Folder operations
├── network_windows.go    # Network operations  
├── command_handlers.go   # Command routing and execution
├── progress.go           # Progress reporting
├── interrupt.go          # Interrupt handling
├── speedtest_utils.go    # Speed testing utilities
├── history.json          # Operation history
└── docs/                 # Documentation
```

## 🔄 Version History

### v2507050402 (Current)

- **User-Friendly Help**: Complete rewrite of help system with 79-line comprehensive guide
- **Enhanced Documentation**: Updated all documentation to reflect current functionality
- **Code Cleanup**: Removed redundant files (error_test.lst, filedo_test.exe, sample_output.txt)

### v2507050401

- **Unified Architecture**: Implemented `FakeCapacityTester` interface
- **Memory Optimization**: Streaming file writing for large capacity tests
- **File Verification**: First-line content verification for all test files
- **Error Handling**: Enhanced `executeFromFile` with proper error returns
- **Code Refactoring**: Eliminated duplicate code in fake capacity tests

## 📄 License

This project is licensed under the MIT License - see the LICENSE file for details.

## 👨‍💻 Author

Created by **sza@ukr.net**

## 🤝 Contributing

Contributions are welcome! Please read CONTRIBUTING.md for guidelines.

---

**FileDO v2507050402** - Advanced File & Storage Operations Tool
