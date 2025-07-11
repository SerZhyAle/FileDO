# FileDO VB.NET GUI

Simple Windows GUI application for filedo.exe built with VB.NET Framework 4.8.

## Features

- Stable native Windows interface
- Checkboxes for target and operation selection
- Path input field with Browse button
- Size field for speed/fill operations
- Real-time command preview
- Debug logging when launched with -debug parameter
- Check for filedo.exe presence before execution

## Build

```cmd
cd c:\GIT\FileDo\filedo_win_vb
msbuild FileDOGUI.vbproj /p:Configuration=Release
```

## Usage

```cmd
bin\Release\filedo_win.exe
bin\Release\filedo_win.exe -debug
```

## Interface

- **Target**: device, folder, network, file
- **Operation**: none, info, speed, fill (f), test, clean  
- **Path**: path to file/folder/device
- **Size**: size for speed/fill operations (default 100MB for fill)
- **Flags**: max, help, hist, short, del/delete/d (auto-delete)
- **Browse**: folder selection via standard dialog
- **RUN**: execute filedo.exe in CMD with pause

Command is automatically built when parameters change.

## Recent Updates

### System Drive Protection

- When targeting system drive (`C:`), operations are automatically redirected to `C:\TEMP` 
- This prevents Windows access restrictions in the root folder
- Directory is automatically created if it doesn't exist
- Works with all operation types (speed, fill, test) in both regular and speed-specific tools

### Speed Command Enhancements

- `filedo_speed.exe` available for direct speed testing
- No need to specify "speed" keyword with filedo_speed.exe
- Supported formats:
  - `filedo_speed d:\temp 100` → direct speed test
  - `filedo_speed C: 1000 nodel` → with options (redirects to C:\TEMP)
  - `filedo_speed E: max short` → maximum size, brief output
  - `filedo_speed \\server\share 500` → network speed test

### Fill Command Enhancements

- `fill` command now works without size parameter (uses 100MB default)
- Added `f` shortcut for `fill` command
- Added `d` shortcut for `delete`/`del` option
- Supported formats:
  - `filedo d:\temp fill` → uses 100MB default
  - `filedo d:\temp f` → shortcut, uses 100MB default  
  - `filedo d:\temp fill 500` → custom size
  - `filedo d:\temp f del` → with auto-delete
  - `filedo d:\temp f d` → with auto-delete (short form)
