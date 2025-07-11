# FileDO Test Suite

This folder contains a comprehensive test suite for FileDO application.

## Files

- `test_list.lst` - Main test file containing commands to test all major functionalities
- `prepare_test_env.cmd` - Script to create necessary test environment (folders and files)
- `cleanup_test_env.cmd` - Script to clean up test files after testing

## Usage

1. **Prepare Test Environment**:
   ```
   prepare_test_env.cmd
   ```
   This will create necessary test folders and files on drive D:

2. **Run Tests**:
   ```
   filedo.exe from .\test\test_list.lst
   ```
   This will execute the full test suite, testing all major functionalities of FileDO.

3. **Clean Up**:
   ```
   cleanup_test_env.cmd
   ```
   This will remove all test folders and files created during testing.

## Test Coverage

The test suite covers:
- Device operations (info, speed, test, fill, clean, duplicate detection)
- Folder operations (info, speed, test, fill, clean, duplicate detection)
- File operations (info)
- Network operations (if uncommented and configured)
- Duplicate file management with various selection modes
- History functionality
- Cleanup operations
- Error handling for non-existent resources (files, folders, devices, networks)

## Note

- The test suite assumes drive D: is available for testing
- Network operations are commented out by default
- Some tests create temporary files that may use significant disk space
- Use cleanup script after testing to remove all test artifacts
