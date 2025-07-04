# FileDO API Documentation

This document describes the command-line interface and internal architecture of FileDO.

## Command Structure

All FileDO commands follow this general pattern:
```
filedo.exe <target_type> <path> <operation> [options]
```

## Target Types

### Device
Operates on disk drives and storage devices.
- **Paths**: `C:`, `D:`, `E:`, etc.
- **Auto-detection**: Single letters are automatically treated as devices

### Folder  
Operates on directories and folders.
- **Paths**: `C:\temp`, `D:\data`, etc.
- **Auto-detection**: Existing directory paths are automatically detected

### Network
Operates on network shares and UNC paths.
- **Paths**: `\\server\share`, `//nas/backup`, etc.
- **Auto-detection**: Paths starting with `\\` or `//` are treated as network

### File
Operates on individual files.
- **Paths**: `C:\file.txt`, `D:\document.pdf`, etc.
- **Auto-detection**: Existing file paths are automatically detected

## Operations

### Info Operations
Get detailed information about the target.

```bash
filedo.exe device C: info      # Detailed device information
filedo.exe folder C:\temp info # Detailed folder information  
filedo.exe file document.txt info # Detailed file information
```

#### Output Format
- **Standard**: Complete information with all available details
- **Short**: Concise format using `short` or `s` flag

### Speed Operations
Test read/write performance.

```bash
filedo.exe device D: speed 100           # Test with 100MB file
filedo.exe folder C:\temp speed max      # Test with 10GB file
filedo.exe network \\server speed 500    # Test network with 500MB
```

#### Parameters
- `<size_mb>`: File size in megabytes
- `max`: Use 10GB (10240MB) test file
- `no|nodel|nodelete`: Keep test file after completion
- `short|s`: Show only final results

### Fill Operations
Fill target with test data until full.

```bash
filedo.exe device E: fill 100     # Fill with 100MB files
filedo.exe folder C:\temp fill 50 # Fill with 50MB files  
filedo.exe device D: fill 100 del # Auto-delete after completion
```

#### Parameters
- `<size_mb>`: Size of each test file in megabytes
- `del`: Automatically delete files after successful completion

### Test Operations
Advanced fake capacity detection.

```bash
filedo.exe device E: test         # Test for fake capacity
filedo.exe device F: test del     # Test and auto-delete if successful
filedo.exe folder C:\test test d  # Test folder (short del flag)
```

#### Parameters
- `del|delete|d`: Auto-delete test files if test passes

### Clean Operations
Remove test files created by fill or test operations.

```bash
filedo.exe device D: clean        # Clean FILL_*.tmp files
filedo.exe folder C:\temp cln     # Clean using short form
filedo.exe network \\server c     # Clean using shortest form
```

#### Aliases
- `clean`: Full command
- `cln`: Short form  
- `c`: Shortest form

## Return Codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Invalid arguments |
| 3 | Access denied |
| 4 | File/path not found |
| 5 | Insufficient space |
| 6 | Test failed (fake capacity detected) |

## Output Format

### Progress Indicators
Long-running operations show progress:
```
Writing file 25/100: FILL_025_14230156.tmp - 45.2 MB/s
Verification: ████████████████████ 100% (50/50 files)
```

### Success Messages
```
✅ Operation completed successfully
✅ TEST PASSED - Device appears genuine
✅ All files verified successfully
```

### Error Messages
```
❌ TEST FAILED - Fake capacity detected
❌ Error: Insufficient free space
❌ Access denied - Run as administrator
```

### Summary Reports
```
============================================================
FAKE CAPACITY TEST SUMMARY
============================================================
Device: E:\
Reported capacity: 128.00 GB
Test file size: 1.28 GB each
Files created: 100 out of 100
Data written: 128.00 GB
Normal write speed: 45.0 MB/s
✅ OVERALL RESULT: DEVICE APPEARS GENUINE
```

## File Patterns

### Test Files
- **Pattern**: `FILL_###_DDHHMMSS.tmp`
- **Example**: `FILL_001_04143025.tmp`
- **Range**: 001 to 100
- **Cleanup**: Automatic with `del` flag or manual with `clean`

### Template Files
- **Pattern**: `fill_template_#_########.txt`  
- **Usage**: Internal template generation
- **Cleanup**: Automatic

## Architecture

### Core Modules

#### main.go
- Command parsing and dispatch
- Help system and usage information
- Error handling and exit codes

#### device_windows.go
- Windows-specific device operations
- Disk space and volume information
- Physical disk property detection

#### folder.go
- Cross-platform folder operations
- Recursive scanning and size calculation
- File counting and permission checking

#### network_windows.go
- Windows network share operations
- UNC path handling and validation
- Network performance testing

#### file_windows.go
- Individual file operations
- File attribute detection
- Metadata extraction

#### types.go
- Data structures for all information types
- String formatting methods
- Cross-platform compatibility types

### Key Functions

#### Information Gathering
```go
func getDeviceInfo(path string, fullScan bool) (DeviceInfo, error)
func getFolderInfo(path string, fullScan bool) (FolderInfo, error)
func getFileInfo(path string, fullScan bool) (FileInfo, error)
func getNetworkInfo(path string, fullScan bool) (NetworkInfo, error)
```

#### Performance Testing  
```go
func runDeviceSpeedTest(path, size string, noDelete, short bool) error
func runFolderSpeedTest(path, size string, noDelete, short bool) error
func runNetworkSpeedTest(path, size string, noDelete, short bool) error
```

#### Capacity Testing
```go
func runDeviceTest(path string, autoDelete bool) error
func runFolderTest(path string, autoDelete bool) error
func runNetworkTest(path string, autoDelete bool) error
```

#### Fill Operations
```go
func runDeviceFill(path, size string, autoDelete bool) error
func runFolderFill(path, size string, autoDelete bool) error
func runNetworkFill(path, size string, autoDelete bool) error
```

#### Cleanup Operations
```go
func runDeviceFillClean(path string) error
func runFolderFillClean(path string) error
func runNetworkFillClean(path string) error
```

## Error Handling

### Permission Errors
FileDO handles various permission scenarios:
- **Read-only access**: Limited operations available
- **No access**: Clear error messages with suggestions
- **Administrator required**: Automatic detection and recommendations

### Space Limitations
- **Insufficient space**: Graceful handling with clear messages
- **Minimum requirements**: 100MB free space for capacity tests
- **Dynamic sizing**: Test file sizes adapt to available space

### Network Issues
- **Connectivity**: Detection of network path availability
- **Timeouts**: Appropriate timeout handling for network operations
- **Authentication**: Support for network credential requirements

## Platform Support

### Current Support
- **Windows 10/11**: Full support with all features
- **Windows Server**: Tested on 2019/2022

### Future Platform Support
The architecture is designed for cross-platform expansion:
- Separate platform-specific implementations
- Common interfaces for all operations
- Extensible command structure

## Performance Considerations

### Memory Usage
- **Efficient I/O**: Streaming operations for large files
- **Buffer Management**: Optimal buffer sizes for different operations
- **Memory Cleanup**: Proper resource management and cleanup

### Disk Usage
- **Temporary Files**: All test files use .tmp extension
- **Auto-cleanup**: Built-in cleanup mechanisms
- **Space Monitoring**: Continuous monitoring of available space

### Network Efficiency
- **Bandwidth Awareness**: Appropriate test sizes for network testing
- **Latency Compensation**: Algorithm adjustments for network delays
- **Connection Reuse**: Efficient network resource utilization
