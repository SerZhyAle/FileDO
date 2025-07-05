# FileDO GUI Application (filedo_win.exe)

Windows GUI frontend for FileDO command-line utility.

## Overview

**filedo_win.exe** is a VB.NET Framework 4.8 Windows Forms application that provides an intuitive graphical interface for FileDO operations. It builds command lines dynamically based on user selections and executes FileDO in a terminal window.

## Features

### Interface Elements

- **Target Selection** (Checkboxes - mutually exclusive):
  - `device` - Physical drives and partitions
  - `folder` - Directory operations
  - `network` - Network path operations  
  - `file` - Individual file operations

- **Operation Selection** (Checkboxes - mutually exclusive):
  - `none` - No operation (default)
  - `info` - Display information
  - `speed` - Performance testing
  - `fill` - Fill with test data
  - `test` - Fake capacity detection
  - `clean` - Remove test files

- **Path Configuration**:
  - Text input field for target path
  - Browse button for folder selection
  - Auto-defaults: `C:\` for device/folder, empty for file/network

- **Size Configuration**:
  - Text input for file size (default: 100MB)
  - Used with speed/fill operations
  - Always included in command when speed/fill is selected

- **Flags** (Optional checkboxes):
  - `max` - Maximum capacity testing
  - `help` - Show help information
  - `hist` - Display command history
  - `short` - Short output format

- **Command Preview**:
  - Real-time display of generated command
  - Updates automatically when selections change

- **Execution**:
  - RUN button executes command in CMD window
  - Includes pause for result viewing

### Smart Logic

1. **Operation Filtering**: 
   - `fill` disabled for file targets
   - `clean` disabled for file and network targets
   - `test` disabled for network targets

2. **Auto Path Defaults**:
   - Device/Folder targets: Default to `C:\`
   - File/Network targets: Default to empty

3. **Mutual Exclusivity**:
   - Only one target can be selected
   - Only one operation can be selected
   - Multiple flags can be selected

4. **Command Building**:
   - Format: `filedo.exe [target] [path] [operation] [size] [flags]`
   - Size only included for speed/fill operations
   - Empty components automatically omitted

## Usage

### Normal Mode
```cmd
filedo_win.exe
```

### Debug Mode
```cmd
filedo_win.exe -debug
```

Debug mode creates `filedo_win_debug.log` with detailed operation logging including:
- Form initialization
- User interactions (checkbox changes, text input)
- Command building steps
- Execution attempts
- Error conditions

## Requirements

- **Windows OS** with .NET Framework 4.8 (pre-installed on Windows 10/11)
- **filedo.exe** in the same directory
- **GUI mode**: Interactive desktop session required

## Technical Details

### Implementation
- **Language**: Visual Basic .NET
- **Framework**: .NET Framework 4.8
- **UI**: Windows Forms
- **Build**: MSBuild (Visual Studio 2022)

### Architecture
- Single form application (`MainForm`)
- Event-driven checkbox logic
- Real-time command string building
- Process execution via `ProcessStartInfo`
- Optional file-based debug logging

### Error Handling
- Path validation before execution
- filedo.exe presence verification
- Process execution error capture
- User-friendly error messages

## Examples

### Basic Usage Flow
1. Select target (e.g., `device`)
2. Enter or browse path (e.g., `C:`)
3. Select operation (e.g., `info`)
4. Click RUN
5. Command executed: `filedo.exe device C: info`

### Speed Testing Flow
1. Select target: `folder`
2. Browse to path: `C:\temp`
3. Select operation: `speed`
4. Set size: `500`
5. Command: `filedo.exe folder C:\temp speed 500`

### Advanced Usage with Flags
1. Target: `device`, Path: `E:`, Operation: `test`
2. Select flags: `max`, `short`
3. Command: `filedo.exe device E: test max short`

## Troubleshooting

### Common Issues

**Application won't start**:
- Verify .NET Framework 4.8 is installed
- Check Windows event log for startup errors

**"filedo.exe not found" error**:
- Ensure filedo.exe is in same directory as filedo_win.exe
- Check file permissions

**No command execution**:
- Verify path is entered
- Check that target and operation are selected
- Review debug log if using -debug mode

**Browse button not working**:
- Some network paths may not be browsable
- Enter path manually for network/UNC paths

### Debug Mode
Use `-debug` parameter for detailed logging:
- All user interactions logged
- Command building steps traced  
- Execution attempts recorded
- Error conditions captured

Log file: `filedo_win_debug.log` (created in application directory)

## Development

### Build Instructions
```cmd
cd filedo_win_vb
msbuild FileDOGUI.vbproj /p:Configuration=Release
```

Output: `bin\Release\filedo_win.exe`

### Project Structure
```
filedo_win_vb/
├── FileDOGUI.vbproj     # Project file
├── MainForm.vb          # Application logic
├── MainForm.Designer.vb # UI layout (generated)
├── MainForm.resx        # Resources (generated)
└── README.md           # Project documentation
```

### Customization
The application can be extended by:
- Adding new target types in checkbox arrays
- Extending operation logic in `UpdateOperationsAvailability()`
- Adding new flags to the flags section
- Modifying command building logic in `UpdateCommand()`

## License

Same as FileDO main project.
