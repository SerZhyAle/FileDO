# What's New in FileDO

## Version 2507050100 (2025-07-05)

### 🎯 Enhanced Clean Command
- **NEW**: Clean command now removes both `FILL_*.tmp` AND `speedtest_*.txt` files
- **IMPROVEMENT**: All clean operations (device, folder, network) now handle both file types
- **BETTER UX**: Clear reporting of file types found and deleted

### 🔧 Technical Improvements
- Unified clean functionality across all modules (device, folder, network)
- Enhanced progress reporting with separate counts for FILL and speedtest files
- Improved error handling during cleanup operations

### 📋 Clean Command Usage
```bash
# Clean both FILL_*.tmp and speedtest_*.txt files
filedo.exe folder C:\temp clean
filedo.exe device D: clean  
filedo.exe network \\server\share clean
```

### 🆕 What Files Are Cleaned
- `FILL_*.tmp` - Files created by fill operations
- `speedtest_*.txt` - Files created by speed tests
- Both file types are now cleaned together in one operation

---

## Version 2507042100 (2025-07-04)

### 🚀 Major Refactoring & Internationalization
- **MAJOR**: Eliminated ~300 lines of duplicated code using CommandHandler interface
- **TRANSLATION**: Complete Russian to English translation throughout project
- **CONSISTENCY**: All duration outputs now show 3 decimal places (e.g., "1.037s")
- **DOCUMENTATION**: Updated README with accurate installation instructions

### 🛠️ Code Quality Improvements
- Reduced main.go complexity by 66% (400 → 135 lines)
- Implemented SOLID principles for better maintainability
- Enhanced cross-platform compatibility
- Added comprehensive error handling

### 📚 Documentation Updates
- Fixed README installation instructions (removed non-existent releases reference)
- Added pre-built executable availability notice
- Created comprehensive change documentation
- Improved code comments and structure

### ⚡ Performance & Reliability
- Optimized command handling architecture
- Consistent duration formatting across all operations
- Enhanced progress reporting
- Better error messages and user feedback

---

## Previous Versions

### Version History
For complete version history and detailed technical changes, see [CHANGELOG.md](CHANGELOG.md).

### Key Features (All Versions)
- **Device Analysis**: Complete physical disk information and testing
- **Speed Testing**: Upload/download performance measurement
- **Capacity Testing**: Fake capacity detection for USB drives and SD cards
- **Fill Operations**: Space filling for security and testing purposes
- **Network Support**: Remote path testing and analysis
- **Cross-Platform**: Windows-focused with cross-platform architecture

### Security Features
- Non-destructive testing with temporary files only
- Secure space overwriting with fill operations
- Safe cleanup of test files
- Permission-aware operations

---

## Quick Start

```bash
# Show device information
filedo.exe C: info

# Test device speed
filedo.exe C: speed 100

# Test for fake capacity
filedo.exe D: test

# Clean test files
filedo.exe C: clean
```

## Getting FileDO

- **Pre-built**: Use `filedo.exe` included in repository
- **From Source**: `git clone` → `go build -o filedo.exe`
- **Requirements**: Windows (primary), Go 1.19+ for building

## Support

For issues, feature requests, or contributions, please visit the project repository.

---

*Last updated: 2025-07-05*
