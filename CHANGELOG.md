# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

### Fixed
- Fixed function signature mismatches in *_unsupported.go files
- Added missing function stubs for cross-platform builds
- Resolved code duplication issues in command handling
- Corrected README installation instructions (removed non-existent releases page reference)

### Technical Improvements
- Reduced main.go from ~400 to 135 lines (-66%)
- Implemented SOLID principles (SRP, OCP, DIP)
- Enhanced code readability and structure
- Maintained 100% backward compatibility
- Added comprehensive testing and validation

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
