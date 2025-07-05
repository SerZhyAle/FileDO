# Command History and HIST Command for FileDO

## Overview
FileDO command history logging system records detailed execution information to `history.json` file for audit and performance analysis.

The `hist` command allows viewing the last 10 history entries in a user-friendly format.

## HIST Command

### Syntax
```bash
filedo hist
```

### Output Format
For each history entry shows:
- **Entry number** - sequential number in history
- **Status** - ✓ for successful commands, ✗ for failed ones
- **Time** - execution time in HH:MM:SS format
- **Command** - full command with parameters (without hist/history flags)
- **Duration** - command execution time

### Additional Information
For successful commands may display key results:
- **Size** - processed data size
- **Files** - number of files
- **Speed** - operation speed
- **Batch** - batch operation statistics (successful/total)

### Example hist command output
```
Last 10 history entries:

[15] ✓ 02:05:57 from example_list_of_connamds.lst batch (37.2ms)
    Batch: 3/3

[16] ✓ 02:06:50 folder . (7.9ms)
    Size: 28.9 MiB, Files: 207

[17] ✗ 02:14:18 device c: (2.1ms)
    Error: failed to walk directory

[18] ✓ 02:14:27 network \\server\share speed 100 (1.5s)
    Speed: 67.2 MB/s
```

## Logging Activation
Add `history` or `hist` flag to any command:
```bash
filedo network \\server\share speed 100 history
filedo network ./folder fill 50 del hist
```

## Record Structure
```json
{
  "timestamp": "2025-07-05T01:51:30.0413174+02:00",
  "command": "network",
  "target": "\\\\localhost\\c$\\git\\filedo\\test_share",
  "operation": "speed",
  "parameters": {
    "size": "1",
    "noDelete": false,
    "shortFormat": false
  },
  "results": {
    "downloadSpeedMBps": 105.64,
    "uploadSpeedMBps": 317.09,
    "fileSizeMB": 1
  },
  "duration": "30.6ms",
  "success": true
}
```

## Supported Operations

### Network Commands
- **speed**: Records upload/download speeds, file size, timing
- **fill**: Records number of created files, data volume, auto-delete usage
- **clean**: Records number of deleted files, freed space
- **test**: Records capacity test results, speed statistics

### Error Handling
All errors are recorded with full description:
```json
{
  "success": false,
  "error": "network path is not writable"
}
```

## File Management
- Maximum 1000 records (automatic rotation)
- Location: `./history.json` in working directory
- Format: JSON with indentation for readability

## Privacy
History is recorded only when explicitly specifying `history`/`hist` flag. Without the flag, no data is saved.

## Error Handling and Graceful Degradation
FileDO implements graceful error handling for file system access issues:

### Partial Results with Warnings
When encountering access restrictions or file locks, FileDO continues execution and shows partial results with warnings instead of stopping completely.

**Example with access errors:**
```
Information for device: c:
  Access:        Readable
  Volume Name:   SYS_2
  File System:   NTFS
  Total Size:    1.82 TiB
  Free Space:    160.13 GiB
  Full Contains: 834897 files, 171406 folders
  Usage:         91.40%

Warning: Some information could not be gathered due to access restrictions.
         Run as administrator for a complete scan.
```

### Common Access Issues
FileDO gracefully handles:
- Permission denied errors
- Files locked by other processes ("being used by another process")
- System files that cannot be accessed
- Temporary files in use

### Benefits
- **No data loss**: Always shows available information
- **Clear warnings**: Users understand result limitations
- **Continued operation**: Commands complete successfully with partial data
- **History logging**: Events are logged as successful with partial results
