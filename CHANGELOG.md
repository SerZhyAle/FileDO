# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
