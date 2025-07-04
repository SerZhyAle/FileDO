# FileDO - File and Device Operations Tool

**FileDO** is a powerful command-line utility for Windows that provides comprehensive file, folder, device, and network operations. It's designed for system administrators, developers, and power users who need reliable tools for storage testing, capacity verification, and performance analysis.

## 🚀 Features

- **Device Analysis**: Get detailed information about disk drives, including physical disk properties
- **Folder Operations**: Comprehensive folder analysis with size calculations and file counting
- **Network Testing**: Test network storage performance and capacity
- **Fake Capacity Detection**: Advanced testing to detect counterfeit USB drives and SD cards
- **Speed Testing**: Measure real-world read/write performance
- **Fill Operations**: Fill devices/folders with test data for capacity testing
- **Cross-Platform Ready**: Windows-focused with extensible architecture

## 📦 Installation

### Download Release
1. Download the latest `filedo.exe` from the [Releases](https://github.com/yourusername/FileDO/releases) page
2. Place it in a directory in your PATH or run directly

### Build from Source
```bash
git clone https://github.com/yourusername/FileDO.git
cd FileDO
go build -o filedo.exe
```

## 🎯 Quick Start

```bash
# Get device information
filedo.exe device C: info

# Test USB drive for fake capacity
filedo.exe device E: test

# Test network share performance  
filedo.exe network \\server\share speed 100

# Clean up test files
filedo.exe folder C:\temp clean
```

## 📚 Usage Examples

### 1. Test Internal Network Speed and Quality

Test your internal network storage performance:

```bash
# Basic network speed test (100MB)
filedo.exe network \\fileserver\shared speed 100

# Comprehensive network test with detailed output
filedo.exe network \\nas\backup speed max

# Test network path for fake capacity issues
filedo.exe network \\server\storage test

# Get network path information
filedo.exe network \\fileserver\data info
```

**Real-world example:**
```bash
C:\> filedo.exe network \\corp-nas\engineering speed 500

Network Speed Test
Target: \\corp-nas\engineering
Testing with 500 MB file...

Upload Test:
Writing test file (500 MB)...  ████████████████████ 100%
Upload completed in 12.3s
Upload speed: 40.7 MB/s

Download Test: 
Reading test file (500 MB)...  ████████████████████ 100%
Download completed in 8.9s
Download speed: 56.2 MB/s

✅ Network performance test completed successfully
Average speed: 48.4 MB/s
Test file deleted automatically
```

### 2. Test Hard Drive Speed and Quality

Comprehensive hard drive testing:

```bash
# Basic drive information
filedo.exe device C: info

# Speed test with 1GB file
filedo.exe device D: speed 1024

# Maximum speed test (10GB)
filedo.exe device D: speed max

# Advanced fake capacity test
filedo.exe device D: test

# Fill drive to test capacity
filedo.exe device D: fill 100 del
```

**Real-world example:**
```bash
C:\> filedo.exe device D: info

Information for device: D:\
  Access:        Readable, Writable
  Volume Name:   Data Drive
  Serial Number: 1234567890
  File System:   NTFS
  --- Physical Disk Info ---
  Model:         Samsung SSD 980 PRO 1TB
  Serial Number: S6B2NS0R123456
  Interface:     NVMe
  --------------------------
  Total Size:    931.51 GiB (1000204886016 bytes)
  Free Space:    456.78 GiB (490234568704 bytes)
  Full Contains: 15,432 files, 892 folders
  Usage:         51.02%

C:\> filedo.exe device D: speed 1024

Device Write Speed Test
Target: D:\
File size: 1024 MB
Creating test file... ████████████████████ 100%
Write completed in 3.2s - Speed: 320.0 MB/s

Read Speed Test
Reading test file... ████████████████████ 100%  
Read completed in 2.1s - Speed: 487.6 MB/s

✅ Speed test completed
Average speed: 403.8 MB/s
Test file deleted
```

### 3. Test USB Drive/SD Card for Fake Capacity

Detect counterfeit storage devices:

```bash
# Quick fake capacity test
filedo.exe device E: test

# Test with auto-cleanup on success
filedo.exe device E: test del

# Get detailed device info first
filedo.exe device E: info

# Manual capacity fill test
filedo.exe device E: fill 50
```

**Real-world example - Genuine Device:**
```bash
C:\> filedo.exe device E: test

Device Fake Capacity Test
Target: E:\
Testing for fake capacity by writing 100 files...
Available space: 29.54 GB
Test file size: 295 MB (1% of free space)
Will create 100 files for capacity test

Creating template file (295 MB)...
✓ Template file created in 2.1s

Starting capacity test - writing 100 files...
File   1: 45.2 MB/s - establishing baseline  
File   2: 44.8 MB/s - establishing baseline
File   3: 45.1 MB/s - establishing baseline
Normal speed established: 45.0 MB/s
File   4: 44.9 MB/s ( 99% of normal)
File   5: 45.3 MB/s (100% of normal)
...
File  98: 44.7 MB/s ( 99% of normal)
File  99: 45.1 MB/s (100% of normal)  
File 100: 44.8 MB/s ( 99% of normal)

Capacity Test Phase Complete!
Files created: 100 out of 100
Total data written: 28.80 GB
✅ Capacity test passed - no fake capacity detected

Starting verification phase...
Verified 100/100 files
✅ All files verified successfully

============================================================
FAKE CAPACITY TEST SUMMARY
============================================================
Device: E:\
Reported capacity: 29.54 GB
Available space: 29.54 GB
Test file size: 295 MB each
Files created: 100 out of 100
Data written: 28.80 GB
Normal write speed: 45.0 MB/s
✅ OVERALL RESULT: DEVICE APPEARS GENUINE
No fake capacity detected. Device seems to have legitimate storage.
```

**Real-world example - Fake Device Detected:**
```bash
C:\> filedo.exe device F: test

Device Fake Capacity Test
Target: F:\
Testing for fake capacity by writing 100 files...
Available space: 128.00 GB
Test file size: 1.28 GB (1% of free space)
Will create 100 files for capacity test

Starting capacity test - writing 100 files...
File   1: 42.1 MB/s - establishing baseline
File   2: 41.8 MB/s - establishing baseline  
File   3: 42.3 MB/s - establishing baseline
Normal speed established: 42.1 MB/s
File   4: 41.9 MB/s ( 99% of normal)
...
File  32: 41.7 MB/s ( 99% of normal)
File  33: 2.1 MB/s ( 5% of normal)

❌ TEST FAILED: Speed dropped to 2.1 MB/s (less than 10% of baseline 42.1 MB/s)
This indicates potential fake capacity - device may be full or failing.
Keeping 33 test files for analysis.

============================================================
FAKE CAPACITY TEST SUMMARY  
============================================================
Device: F:\
Reported capacity: 128.00 GB
Actual capacity: ~32 GB (test failed at file 33)
❌ OVERALL RESULT: FAKE CAPACITY DETECTED
This device appears to be counterfeit with false capacity reporting.
Real capacity is approximately 32 GB, not the reported 128 GB.
```

## 🔧 Command Reference

### Device Commands
```bash
filedo.exe device <path> [info|i|short|s]           # Device information
filedo.exe device <path> speed <size_mb|max>        # Speed test
filedo.exe device <path> fill <size_mb> [del]       # Fill with test data  
filedo.exe device <path> <cln|clean|c>              # Clean test files
filedo.exe device <path> test [del|delete|d]        # Fake capacity test
```

### Folder Commands  
```bash
filedo.exe folder <path> [info|i|short|s]           # Folder information
filedo.exe folder <path> speed <size_mb|max>        # Speed test
filedo.exe folder <path> fill <size_mb> [del]       # Fill with test data
filedo.exe folder <path> <cln|clean|c>              # Clean test files  
filedo.exe folder <path> test [del|delete|d]        # Fake capacity test
```

### Network Commands
```bash
filedo.exe network <path> [info|i]                  # Network path info
filedo.exe network <path> speed <size_mb|max>       # Network speed test
filedo.exe network <path> fill <size_mb> [del]      # Fill with test data
filedo.exe network <path> <cln|clean|c>             # Clean test files
filedo.exe network <path> test [del|delete|d]       # Fake capacity test
```

### File Commands
```bash
filedo.exe file <path> [info|i|short|s]             # File information
```

## 🛡️ Safety Features

- **Non-destructive testing**: All tests use temporary files that can be automatically cleaned
- **Automatic cleanup**: Use `del` flag to auto-delete test files after successful completion
- **Progress monitoring**: Real-time progress indicators for all operations
- **Error handling**: Graceful handling of access permissions and disk errors
- **Verification**: File integrity checking during capacity tests

## 🎛️ Advanced Options

### Speed Test Options
- `max` or `10240`: Use 10GB test file for maximum accuracy
- `no|nodel|nodelete`: Keep test files after completion
- `short|s`: Show only final results without progress

### Fill Options  
- `del`: Automatically delete created files after successful completion
- `<cln|clean|c>`: Clean up existing FILL_*.tmp files

### Test Options
- `del|delete|d`: Auto-delete test files if test passes
- Automatic baseline calculation from first 3 files
- Speed deviation detection (fails if <10% or >1000% of baseline)
- File integrity verification with custom headers

## 🔍 How Fake Capacity Detection Works

FileDO's fake capacity detection uses a sophisticated multi-phase approach:

1. **Baseline Establishment**: First 3 files establish normal write speed
2. **Continuous Monitoring**: Each subsequent file is monitored for speed anomalies  
3. **Anomaly Detection**: Test fails if speed drops below 10% or exceeds 1000% of baseline
4. **Data Verification**: All files are verified for data integrity
5. **Comprehensive Reporting**: Detailed analysis of results with clear recommendations

This method can detect:
- **Fake capacity**: When device reports more storage than actually available
- **Write caching fraud**: When device falsely reports successful writes
- **Data corruption**: When stored data becomes corrupted or inaccessible

## 📊 Output Formats

### Standard Output
Detailed information with full context and explanations

### Short Output (`short|s`)
Concise format perfect for scripts and automation

### Progress Indicators
Real-time progress bars and percentage completion for long operations

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 👨‍💻 Author

**sza** - [sza@ukr.net](mailto:sza@ukr.net)

## 🐛 Bug Reports & Feature Requests

Please use the [GitHub Issues](https://github.com/yourusername/FileDO/issues) page to report bugs or request features.

## ⭐ Star History

If you find FileDO useful, please consider giving it a star on GitHub!

---

**Version**: 2507042100  
**Platform**: Windows  
**Language**: Go
