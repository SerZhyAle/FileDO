# FileDO FILL - Specialized Fill Operation Tool

This is a standalone subproject that contains all the FILL functionality from the main FileDO project in a compact, specialized executable.

## Project Structure

```
FILL/
├── main.go              # Main entry point and command parsing
├── device_fill.go       # Device (drive) fill operations
├── folder_fill.go       # Folder fill operations  
├── network_fill.go      # Network share fill operations
├── progress.go          # Progress tracking and reporting
├── interrupt.go         # Ctrl+C interrupt handling
├── utils.go             # Utility functions and file operations
├── go.mod               # Go module definition
├── BUILD_INSTRUCTIONS.cmd  # Build guide
└── README.md            # This file
```

## Building

1. Install Go 1.21+ from https://golang.org/dl/
2. Open command prompt in the FILL folder
3. Run: `go mod tidy`
4. Run: `go build -o filedo_fill.exe .`

## Usage

### Fill Operations

```bash
# Fill drive with default 100MB files
filedo_fill.exe C:

# Fill drive with specific size files
filedo_fill.exe D: 500

# Fill drive with auto-delete (for testing)
filedo_fill.exe E: 1000 del

# Fill folder
filedo_fill.exe C:\temp 200

# Fill network share
filedo_fill.exe \\server\share 100
```

### Clean Operations

```bash
# Clean test files from drive
filedo_fill.exe C: clean

# Clean test files from folder
filedo_fill.exe C:\temp clean

# Clean test files from network share
filedo_fill.exe \\server\share clean
```

### Help

```bash
filedo_fill.exe help
filedo_fill.exe ?
```

## Features

- **Complete FILL functionality** - All FILL operations from main FileDO
- **Multi-target support** - Devices (C:, D:), folders, network shares
- **Auto-delete option** - For testing without leaving files behind
- **Progress tracking** - Real-time progress with speed reporting
- **Interrupt handling** - Graceful Ctrl+C handling with cleanup
- **Parallel operations** - Fast file creation using worker pools
- **Compatible file formats** - Same file naming as main FileDO
- **History logging** - Operations logged to history.json
- **Error recovery** - Automatic handling of disk full and other errors

## File Formats

Creates files with the same naming convention as main FileDO:
- `FILL_00001_ddHHmmss.tmp`
- `FILL_00002_ddHHmmss.tmp`
- etc.

Where `ddHHmmss` is the timestamp when the operation started.

## Compatibility

- Fully compatible with main FileDO cleanup operations
- Uses same file naming and formats
- History logging compatible with main FileDO
- Clean operations work on files created by either tool

## Size Limits

- Minimum file size: 1MB
- Maximum file size: 10240MB (10GB)
- Default file size: 100MB

## Options

- `del`, `delete`, `d` - Auto-delete files after creation
- `clean`, `c` - Clean existing test files
- `help`, `?` - Show help information

## Examples

```bash
# Test drive capacity with auto-cleanup
filedo_fill.exe F: 100 del

# Fill temporary folder for testing
filedo_fill.exe C:\temp 50 del

# Fill until disk full, then clean up
filedo_fill.exe D: 1000
filedo_fill.exe D: clean

# Clean all test files from multiple locations
filedo_fill.exe C: clean
filedo_fill.exe D: clean
filedo_fill.exe C:\temp clean
```

## Technical Details

- Written in Go 1.21+
- Uses Windows APIs for disk space information
- Parallel file operations for performance
- Optimized buffer sizes for different storage types
- Comprehensive error handling and recovery
- Memory-efficient for large operations
- Signal-safe interrupt handling

## History Integration

All operations are logged to `history.json` in the same format as main FileDO, allowing for:
- Operation tracking and audit trails
- Performance analysis and benchmarking
- Integration with main FileDO history system
- Troubleshooting and error analysis