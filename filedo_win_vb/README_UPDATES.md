# FileDO Windows GUI Updates

## Added Duplicates Management Functionality

The FileDO Windows GUI application has been updated to support the duplicate file detection and management capabilities from the FileDO command-line tool.

### New Features

1. **Check Duplicates Operation**
   - Added "Check duplicates" option in the operations section
   - Allows finding duplicate files in folders, devices, and network locations

2. **Duplicate Selection Modes**
   - **old** - Keep newest files as originals, handle older files as duplicates
   - **new** - Keep oldest files as originals, handle newer files as duplicates
   - **abc** - Keep alphabetically last files as originals
   - **xyz** - Keep alphabetically first files as originals

3. **Duplicate Actions**
   - **Delete** - Remove duplicate files (keeping originals)
   - **Move** - Relocate duplicate files to specified folder

### How to Use

1. Select the target type (device, folder, or network)
2. Enter the path to scan
3. Select "Check duplicates" operation
4. Choose selection mode (old/new/abc/xyz)
5. Choose action (delete or move)
6. If "Move" is selected, specify destination folder
7. Click "Run" to execute

### Command Line Equivalent

The GUI generates commands like:

```bash
filedo.exe device C:\ cd old del
filedo.exe folder D:\Data cd xyz move E:\Duplicates
```

### Notes

- The duplicate detection uses MD5 hashing for reliable file comparison
- A hash cache improves performance for repeated scans
- Be careful with the delete option - it permanently removes files
