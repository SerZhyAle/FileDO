# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [2.5.11-20250705] - 2025-07-05

### Fixed

- **FAKE CAPACITY TEST**: Fixed test file cleanup on failure - files now preserved for analysis
- **REAL CAPACITY ESTIMATION**: Added calculation of estimated real storage capacity when test fails
- **DETAILED FAILURE ANALYSIS**: Enhanced error reporting with capacity analysis breakdown
- **TEST FILE PRESERVATION**: Failed tests no longer auto-delete files, keeping evidence for investigation

### Capacity Test Enhancements

- **FAILURE REPORTING**: Added detailed analysis showing:
  - Number of files successfully written/verified before failure
  - Amount of data written before corruption
  - Estimated real free space based on failure point
- **CAPACITY DETECTION**: Improved fake USB/SD card detection with real capacity estimation
- **ERROR CONTEXT**: Better error messages explaining what the failure indicates

### Test Implementation Fixes

- **Test Logic**: Removed automatic cleanup on verification failures across all test types
- **Capacity Calculation**: Added real-time capacity estimation based on test failure points
- **History Logging**: Enhanced logging with estimated capacity data for failed tests

## [2.5.10-20250705] - 2025-07-05

### Optimized

- **PROGRESS DISPLAY**: Optimized speed test progress updates for better UI performance
- **REDUCED UPDATE FREQUENCY**: Progress now updates every 2 seconds instead of every file operation
- **ENHANCED TRACKER**: New `PrintProgressCustom` method for specialized progress formats
- **FILE CREATION**: Reduced progress updates during large file creation (every 50MB vs 10MB)
- **COPY OPERATIONS**: Smarter progress updates during file copying (every 5% or 10MB chunks)
- **NETWORK OPERATIONS**: Simplified network fill progress updates (every 10 files)

### Progress Display Optimization

- **ProgressTracker API**: Added `NewProgressTrackerWithInterval`, `SetUpdateInterval`, `GetTimeSinceLastUpdate`, `PrintProgressCustom`, `ForceUpdate` methods
- **Encapsulation**: Fixed direct access to private ProgressTracker fields in network operations
- **Configurable Intervals**: Different update intervals for different operation types
- **Consistent UI**: Stable, readable progress display without flickering

### Performance Impact

- **Speed Tests**: Dramatically reduced console output frequency during large operations
- **Fill Operations**: 22,953 files processed with ~115 progress updates instead of 22,953
- **Clean Operations**: Grouped progress updates for large cleanup operations
- **Memory Efficiency**: Lower overhead from frequent console writes

## [2.5.9-20250705] - 2025-07-05

### Added

- **GUI APPLICATION**: New Windows GUI frontend `filedo_win.exe` built with VB.NET Framework 4.8
- **CHECKBOX INTERFACE**: Intuitive checkbox-based selection for targets and operations
- **REAL-TIME PREVIEW**: Live command preview that updates as selections change
- **SMART FILTERING**: Operation availability based on selected target type
- **AUTO PATH DEFAULTS**: C:\ for device/folder, empty for file/network targets
- **ADDITIONAL FLAGS**: Support for max, help, hist, short flags in GUI
- **DEBUG LOGGING**: Detailed logging when launched with `-debug` parameter
- **BROWSE FUNCTIONALITY**: Standard Windows folder browser integration

### GUI Features

- **Target Selection**: Device, Folder, Network, File (mutually exclusive checkboxes)
- **Operation Selection**: None, Info, Speed, Fill, Test, Clean (mutually exclusive)
- **Size Configuration**: Input field for speed/fill operations (default: 100MB)
- **Command Building**: Automatic command line construction with proper syntax
- **One-Click Execution**: RUN button launches filedo.exe in CMD with pause
- **Error Validation**: Path checking and filedo.exe presence verification

### Documentation

- **FILEDO_WIN.md**: Comprehensive GUI application documentation
- **Updated README.md**: Added GUI sections in installation and usage
- **Enhanced VB.NET README**: Complete project documentation in English

## [2.5.8-20250705] - 2025-07-05

### Enhanced

- **CLEAN COMMAND**: Enhanced clean command to remove both `FILL_*.tmp` AND `speedtest_*.txt` files
- **UNIFIED CLEANUP**: All clean operations (device, folder, network) now handle both file types in one command
- **BETTER REPORTING**: Improved progress reporting with separate counts for different file types

### Changed

- Updated clean functionality across all modules (device_windows.go, folder.go, network_windows.go)
- Enhanced user feedback showing counts of FILL files vs speedtest files found and deleted
- Improved command descriptions to reflect expanded cleanup capability

### Technical Improvements

- Unified file pattern matching logic across all clean functions
- Enhanced error handling during cleanup operations
- Better progress tracking and user feedback during deletion

## [2.5.7-20250705] - 2025-07-05

### Added
- CommandHandler interface for unified command processing
- Generic command handler architecture to eliminate code duplication
- New command_handlers.go file with centralized logic
- Comprehensive refactoring documentation
- Translation of all Russian text to English throughout project
- Pre-built filedo.exe (3.7MB) available in repository root

### Changed
- **MAJOR REFACTORING**: Eliminated ~300 lines of duplicated code in main.go
- Restructured command handlers using interface-based design
- Improved code maintainability and extensibility
- Enhanced cross-platform compatibility with complete stub implementations
- **DOCUMENTATION**: Translated all Russian documentation and comments to English
- **README**: Updated installation instructions to reflect available pre-built executable
- **DURATION FORMATTING**: Standardized all duration output to 3 decimal places using formatDuration()

### Fixed

- Fixed function signature mismatches in *_unsupported.go files
- Added missing function stubs for cross-platform builds
- Resolved code duplication issues in command handling
- Corrected README installation instructions (removed non-existent releases page reference)
- **DURATION OUTPUT**: Fixed all duration formatting in device_windows.go, network_windows.go, network_unsupported.go, and folder.go to use formatDuration() for consistent 3-decimal precision (e.g., "created in 1.037s")

### Technical Improvements
- Reduced main.go from ~400 to 135 lines (-66%)
- Implemented SOLID principles (SRP, OCP, DIP)
- Enhanced code readability and structure
- Maintained 100% backward compatibility
- Added comprehensive testing and validation
- **FORMATTING CONSISTENCY**: All duration/time outputs now consistently show 3 decimal places across all modules

## [Unreleased]

### Added
- Initial release of FileDO
- Device information and analysis
- Folder operations and size calculation
- Network path testing and analysis
- Fake capacity detection for USB drives and SD cards
- Speed testing for devices, folders, and network paths
- Fill operations for capacity testing
- Independent clean command for test file management
- Cross-platform architecture (Windows focused)

### Features
- **Device Commands**: Complete device analysis with physical disk information
- **Folder Commands**: Comprehensive folder analysis with recursive scanning
- **Network Commands**: Network storage testing and performance analysis
- **File Commands**: Detailed file information and attributes
- **Fake Capacity Detection**: Advanced algorithm to detect counterfeit storage devices
- **Speed Testing**: Real-world performance measurement
- **Fill Operations**: Controlled data writing for capacity verification
- **Clean Operations**: Efficient cleanup of test files from any command

### Security
- Non-destructive testing with temporary files only
- Automatic cleanup options to prevent disk clutter
- Safe error handling for access permissions
- Verification of file integrity during testing
- **Secure Free Space Wiping**: Use `fill <size> del` to overwrite free space and prevent data recovery
- Data recovery prevention for secure disposal and compliance
- Example: `filedo C: fill 1000 del` - securely wipes free space on C: drive

### Performance
- Optimized for large file operations
- Progress indicators for long-running operations
- Memory-efficient recursive folder scanning
- Fast baseline establishment for fake capacity detection

## [2507042100] - 2025-07-04

### Added
- Initial implementation
- Core command structure
- Device, folder, file, and network operations
- Test command for fake capacity detection
- Independent clean command syntax
- Comprehensive help system

### Changed
- Clean command syntax from `<command> <path> fill clean` to `<command> <path> clean`
- This allows cleaning test files created by both `fill` and `test` commands

### Technical Details
- Written in Go for performance and cross-platform compatibility
- Windows-specific implementations using golang.org/x/sys/windows
- Modular architecture for easy extension
- Comprehensive error handling and user feedback

### Testing
- Extensive testing on various storage devices
- Validated fake capacity detection algorithm
- Performance benchmarking on different storage types
- Network path compatibility testing
