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
- **Operation**: none, info, speed, fill, test, clean  
- **Path**: path to file/folder/device
- **Size**: size for speed/fill operations (default 100)
- **Flags**: max, help, hist, short
- **Browse**: folder selection via standard dialog
- **RUN**: execute filedo.exe in CMD with pause

Command is automatically built when parameters change.
