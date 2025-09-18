# FileDO TEST v250916_test - Storage Capacity Testing Tool# FileDO Test Suite



**FileDO TEST** is a specialized sub-project within the FileDO ecosystem designed to detect fake storage devices by testing their actual capacity. This standalone application helps identify storage devices that report incorrect capacity or corrupt data when reaching their real limits.This folder contains a comprehensive test suite for FileDO application.



## Features## Files



- **Fake Capacity Detection**: Tests storage devices to detect fake capacity claims- `test_list.lst` - Main test file containing commands to test all major functionalities

- **Multi-Platform Support**: Supports devices, folders, and network shares  - `prepare_test_env.cmd` - Script to create necessary test environment (folders and files)

- **Real-time Verification**: Incrementally verifies data integrity during testing- `cleanup_test_env.cmd` - Script to clean up test files after testing

- **Speed Analysis**: Monitors write speeds to detect abnormal behavior

- **Comprehensive Reporting**: Provides detailed analysis of real vs claimed capacity## Usage

- **Safe Interruption**: Supports Ctrl+C for graceful cancellation

- **History Logging**: All operations logged in `history.json`1. **Prepare Test Environment**:

   ```

## Usage   prepare_test_env.cmd

   ```

```   This will create necessary test folders and files on drive D:

filedo_test.exe <target> [options]

```2. **Run Tests**:

   ```

### Examples   filedo.exe from .\test\test_list.lst

   ```

```bash   This will execute the full test suite, testing all major functionalities of FileDO.

# Test drive capacity (primary use case)

filedo_test.exe C:3. **Clean Up**:

filedo_test.exe D: del          # Test and auto-delete files   ```

   cleanup_test_env.cmd

# Test folder capacity     ```

filedo_test.exe C:\temp   This will remove all test folders and files created during testing.

filedo_test.exe C:\temp del     # Test folder and auto-delete

## Test Coverage

# Test network share capacity

filedo_test.exe \\server\shareThe test suite covers:

filedo_test.exe \\server\share del- Device operations (info, speed, test, fill, clean, duplicate detection)

```- Folder operations (info, speed, test, fill, clean, duplicate detection)

- File operations (info)

### Targets- Network operations (if uncommented and configured)

- Duplicate file management with various selection modes

- **C:, D:, etc.** - Device/drive operations (primary use case)- History functionality

- **C:\folder** - Folder operations- Cleanup operations

- **\\server\share** - Network operations- Error handling for non-existent resources (files, folders, devices, networks)



### Options## Note



- **del, delete, d** - Auto-delete test files after completion- The test suite assumes drive D: is available for testing

- Network operations are commented out by default

## How It Works- Some tests create temporary files that may use significant disk space

- Use cleanup script after testing to remove all test artifacts

### Test Process

1. **Space Analysis**: Calculates available space and determines optimal test file size
2. **Buffer Optimization**: Calibrates optimal write buffer size for target device
3. **File Creation**: Creates up to 100 large test files (using 95% of available space)
4. **Real-time Verification**: Verifies each file immediately after creation
5. **Speed Monitoring**: Analyzes write speeds to detect anomalies
6. **Integrity Checking**: Uses pattern-based data verification

### Detection Methods

- **Creation Failures**: Files fail to create when real capacity is reached
- **Data Corruption**: Files become corrupted when device limit exceeded
- **Speed Anomalies**: Write speeds drop dramatically or increase unrealistically
- **Pattern Verification**: Data patterns become corrupted or filled with zeros

### Test Files

- **Format**: `FILL_001_ddHHmmss.tmp`, `FILL_002_ddHHmmss.tmp`, etc.
- **Structure**: Header + Data Pattern + Footer
- **Pattern**: Readable text pattern for easy verification
- **Verification**: Header/footer matching + random data sampling

## Requirements

- **Minimum Space**: 100MB free space required
- **Operating System**: Windows (uses Windows-specific APIs for drive information)
- **Go Version**: 1.21 or later

## Building

```bash
cd TEST
go build -o filedo_test.exe
```

## Files Structure

```
TEST/
├── main.go           # Main application entry point
├── go.mod            # Go module definition
├── go.sum            # Go dependencies checksum
├── device_test.go    # Windows device testing
├── folder_test.go    # Folder testing
├── network_test.go   # Network share testing
├── test_core.go      # Core testing logic
├── utils.go          # Utility functions
├── interrupt.go      # Signal handling
├── progress.go       # Progress tracking
├── build.cmd         # Build script
├── test_filedo_test.cmd # Test script
└── README.md         # This file
```

## Integration

FileDO TEST integrates with the main FileDO ecosystem:

- **History Logging**: Compatible with main FileDO history format
- **File Formats**: Uses same test file patterns as FileDO FILL
- **Command Interface**: Consistent with main FileDO command structure

## Author

sza@ukr.net