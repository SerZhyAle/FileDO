# FileDO CHECK - Specialized File Integrity Checker

FileDO CHECK v250916_check - A specialized tool for file integrity verification through read performance analysis.

## Overview

FileDO CHECK is a standalone version of the CHECK functionality from the main FileDO toolkit. It performs fast file integrity checks by monitoring file read delays, identifying potentially damaged files that may indicate disk problems.

## Features

- **Three Check Modes**: Quick, Balanced (default), and Deep scanning
- **Multi-target Support**: Devices, folders, and network shares
- **Intelligent Threading**: Auto-detects drive type for optimal performance
- **Progress Tracking**: Real-time progress with ETA calculations  
- **Damage Detection**: Identifies slow-reading files indicating potential damage
- **Resume Support**: Continue from last position for interrupted scans
- **Flexible Reporting**: CSV and JSON output formats
- **Integration Ready**: Compatible with main FileDO damage tracking system

## Usage

```cmd
filedo_check.exe <target> [mode] [options]
```

### Basic Examples

```cmd
# Check entire C: drive with balanced mode (default)
filedo_check.exe C:

# Quick check of D: drive
filedo_check.exe D: quick

# Deep check of specific folder
filedo_check.exe C:\Important deep

# Check network share
filedo_check.exe \\server\share balanced
```

### Modes

- **quick** (`q`) - Fast scan, reads only the beginning of files
- **balanced** (`b`) - Default mode, reads beginning and middle of files  
- **deep** (`d`) - Thorough scan, reads beginning, middle, and end of files

### Common Options

```cmd
# Set custom delay threshold (default: 2.0 seconds)
filedo_check.exe C: --threshold 5

# Verbose output with detailed information
filedo_check.exe C: --verbose

# Quiet mode with minimal output
filedo_check.exe C: --quiet

# Generate CSV report
filedo_check.exe C: --report csv

# Limit to specific file types
filedo_check.exe C: --include-ext "jpg,png,mp4"

# Skip certain file types
filedo_check.exe C: --exclude-ext "tmp,log"

# Check only large files (>100MB)
filedo_check.exe C: --min-mb 100

# Limit processing to 1000 files
filedo_check.exe C: --max-files 1000

# Resume from last position
filedo_check.exe C: --resume
```

## How It Works

1. **File Discovery**: Scans target location for eligible files
2. **Read Testing**: Attempts to read file beginnings (and middle/end for balanced/deep modes)
3. **Delay Analysis**: Measures read response times against threshold (default: 2 seconds)
4. **Damage Detection**: Files exceeding threshold are flagged as potentially damaged
5. **List Management**: Updates `skip_files.list` (damaged) and `check_files.list` (verified good)

## Output Files

- `skip_files.list` - Contains paths of files with read delays (potentially damaged)
- `check_files.list` - Contains paths of verified good files (skipped on subsequent runs)
- `history.json` - Operation history log with statistics
- `check_report_*.csv/json` - Optional detailed reports

## Environment Variables

Advanced configuration through environment variables:

```cmd
set FILEDO_CHECK_MODE=balanced
set FILEDO_CHECK_THRESHOLD_SECONDS=2.0
set FILEDO_CHECK_WORKERS=8
set FILEDO_CHECK_VERBOSE=1
set FILEDO_CHECK_REPORT=csv
set FILEDO_CHECK_MAX_FILES=10000
```

See main FileDO documentation for complete environment variable reference.

## Integration with FileDO

FileDO CHECK uses the same damage detection system as the main FileDO toolkit:

- Shares `skip_files.list` for consistent damage tracking
- Compatible with main FileDO rescue operations
- Uses same environmental configuration options
- Maintains unified operation history

## Performance Tips

- **SSDs**: Use higher worker counts (8+) for parallel processing
- **HDDs**: Use fewer workers (3-4) to avoid seek penalties  
- **Network**: Use moderate worker counts (5-6) depending on bandwidth
- **Large datasets**: Use `--precount` for accurate ETA estimation
- **Resume**: Use `--resume` for very large operations that may be interrupted

## Build Instructions

### Prerequisites
- Go 1.24.4 or later (download from https://golang.org/dl/)
- Windows (optimized for Windows file systems)

### Building
```cmd
cd CHECK
go mod tidy
go build -o filedo_check.exe
```

Or use the provided batch file:
```cmd
build.cmd
```

### Installation Notes
If you get "go is not recognized" error:
1. Download and install Go from https://golang.org/dl/
2. Restart command prompt/terminal
3. Verify installation: `go version`
4. Then run build commands above

## Requirements

- Go 1.24.4 or later
- Windows (optimized for Windows file systems)
- Access to parent FileDO module for core check functionality

## Author

Created by sza@ukr.net as part of the FileDO toolkit.

## License

See LICENSE file in the parent FileDO project.