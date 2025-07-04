# Fake Capacity Detection Algorithm

This document explains how FileDO's fake capacity detection algorithm works and why it's effective at identifying counterfeit storage devices.

## Overview

Fake USB drives and SD cards are unfortunately common in the market. These devices appear to have large storage capacities (e.g., 128GB, 1TB) but actually contain much smaller storage chips (e.g., 8GB, 32GB). They use firmware hacks to report false capacity information to the operating system.

## The Problem

Fake storage devices typically exhibit these characteristics:

1. **False Capacity Reporting**: The device reports much more storage than it actually has
2. **Write Loop-back**: After the real capacity is exceeded, writes start overwriting earlier data
3. **Speed Degradation**: Writing speed often drops dramatically when the real capacity is reached
4. **Data Corruption**: Files written beyond the real capacity become corrupted or inaccessible

## FileDO's Detection Method

### Phase 1: Baseline Establishment

```
Files 1-3: Establish baseline write speed
┌─────────┬─────────┬─────────┐
│ File 1  │ File 2  │ File 3  │
│ 45 MB/s │ 44 MB/s │ 46 MB/s │
└─────────┴─────────┴─────────┘
Baseline: 45 MB/s (average)
```

The algorithm writes the first 3 files and calculates their average write speed as the baseline. This represents the device's normal performance.

### Phase 2: Continuous Monitoring

```
Files 4-100: Monitor for anomalies
┌─────────┬─────────┬─────────┬─────────┬─────────┐
│ File 4  │ File 5  │ File 6  │  ...    │File 100 │
│ 44 MB/s │ 45 MB/s │ 46 MB/s │         │ 45 MB/s │
│ ✓ 98%   │ ✓ 100%  │ ✓ 102%  │         │ ✓ 100%  │
└─────────┴─────────┴─────────┴─────────┴─────────┘
All within normal range (10% - 1000% of baseline)
```

For each subsequent file, the algorithm checks if the write speed falls within acceptable bounds:
- **Too Slow**: Less than 10% of baseline (indicates device may be full)
- **Too Fast**: More than 1000% of baseline (indicates fake writing/caching)

### Phase 3: Data Verification

```
Verification: Read back all files and check integrity
┌─────────┬─────────┬─────────┬─────────┐
│ File 1  │ File 2  │ File 3  │  ...    │
│ Header: │ Header: │ Header: │         │
│ ✓ Valid │ ✓ Valid │ ✓ Valid │         │
└─────────┴─────────┴─────────┴─────────┘
```

Every file contains a known header pattern. The algorithm reads back each file and verifies:
- File can be opened and read
- Header pattern is intact and correct
- No data corruption has occurred

## File Structure

Each test file has this structure:
```
FILL_TEST_HEADER_LINE\n
[1671 MB of test data...]
```

The header line serves as a corruption detector. If this line is missing or altered, it indicates data corruption.

## Test File Naming

Files are named: `FILL_001_DDHHMMSS.tmp` to `FILL_100_DDHHMMSS.tmp`
- Sequential numbering ensures proper order
- Timestamp prevents conflicts
- .tmp extension clearly identifies test files

## Failure Scenarios

### Scenario 1: Speed Drop (Fake Capacity Reached)
```
Normal operation:        Speed drop detected:
File 30: 45 MB/s ✓      File 31: 45 MB/s ✓
File 31: 44 MB/s ✓      File 32: 2.1 MB/s ❌
File 32: 46 MB/s ✓      
                        Result: FAKE CAPACITY DETECTED
                        Real capacity: ~30% of reported
```

### Scenario 2: Speed Jump (Fake Writing)
```
Normal operation:        Speed jump detected:
File 15: 45 MB/s ✓      File 16: 45 MB/s ✓
File 16: 44 MB/s ✓      File 17: 450 MB/s ❌
File 17: 46 MB/s ✓      
                        Result: FAKE WRITING DETECTED
                        Device not actually writing data
```

### Scenario 3: Data Corruption
```
Write phase: ✓ All files written successfully
Read phase:  
File 1: ✓ Header intact
File 2: ✓ Header intact
File 3: ❌ Header corrupted
                        
Result: DATA CORRUPTION DETECTED
```

## Algorithm Parameters

| Parameter | Value | Reason |
|-----------|--------|---------|
| File Count | 100 | Provides thorough coverage of device |
| File Size | 1% of free space | Balances thoroughness with speed |
| Baseline Files | 3 | Sufficient for stable average |
| Min Speed Threshold | 10% of baseline | Accounts for normal variance |
| Max Speed Threshold | 1000% of baseline | Detects impossible speeds |
| Min Free Space | 100MB | Ensures meaningful test |

## Effectiveness

This algorithm is effective because:

1. **Multi-faceted Detection**: Tests both speed and data integrity
2. **Adaptive Thresholds**: Uses device's own performance as baseline
3. **Comprehensive Coverage**: Tests across the entire claimed capacity
4. **Real-world Conditions**: Uses actual file I/O operations
5. **Early Detection**: Stops at first sign of problems

## Limitations

- **Time**: Testing 100 files takes significant time on slower devices
- **Wear**: Repeated writes may contribute to device wear (minimal impact)
- **Network Latency**: Network paths may show variable speeds due to latency
- **System Load**: Other system activity may affect results

## Best Practices

1. **Run on Dedicated System**: Minimize other I/O activity during testing
2. **Use Administrator Rights**: Ensures full device access
3. **Test New Devices**: Always test before trusting with important data
4. **Keep Test Files**: Don't use auto-delete if device fails (useful for analysis)
5. **Multiple Tests**: Run test multiple times for suspicious devices

## Technical Implementation

The algorithm is implemented in Go with these key components:

- **Windows API Integration**: Uses `golang.org/x/sys/windows` for accurate disk space information
- **Buffered I/O**: Efficient file operations with proper error handling  
- **Progress Reporting**: Real-time feedback during long operations
- **Memory Management**: Efficient handling of large files
- **Error Recovery**: Graceful handling of I/O errors and cleanup

## Conclusion

FileDO's fake capacity detection algorithm provides a robust, reliable method for identifying counterfeit storage devices. By combining speed monitoring with data integrity verification, it can detect the various techniques used by fake devices to deceive users and operating systems.
