# FROM Command for FileDO

## Overview
The `from` command allows executing a series of FileDO commands from a text file, providing automation for batch operations.

## Syntax
```bash
filedo from <filepath>
```

## Command File Format
- One command per line
- Empty lines are ignored
- Lines starting with `#` are comments
- Commands are executed without `filedo` prefix

## Example Command File
```text
# Device checks
device c: info hist
device d: info hist

# Network testing
network \\server\share speed 100 hist
network \\server\share test del hist

# Cleanup
network \\server\share clean hist
```

## Supported Commands
All FileDO commands:
- **device** - disk operations
- **folder** - folder operations  
- **file** - file operations
- **network** - network path operations

## Logging
- The `from` command itself records history when `hist` flag is present
- Each executed command creates its own history entry
- Overall statistics are recorded: total commands and successful executions

## Usage Example
```bash
# Create command file
echo "device c: info hist" > commands.txt
echo "folder . speed 10 hist" >> commands.txt

# Execute commands from file
filedo from commands.txt hist
```

## Execution Results
```
[1] Executing: device c: info hist
Device Information
...
[2] Executing: folder . speed 10 hist  
Folder Speed Test
...
Batch execution complete: 2/2 commands succeeded
```

## Error Handling
- When a command fails, execution continues
- Final statistics show successful/failed commands
- Each error is displayed with description
