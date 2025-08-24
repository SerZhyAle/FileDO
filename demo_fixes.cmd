@echo off
echo ================================================
echo DEMO: Fixed Damaged Disk System
echo ================================================
echo.

echo IMPROVEMENTS IMPLEMENTED:
echo 1. Stuck file detection: 30s → 10s timeout
echo 2. Auto-switch to rescue mode after 15s
echo 3. Proper logging to damaged_files.log
echo 4. Skip list creation for next runs
echo.

echo Current status check:
echo.

if exist damaged_files.log (
    echo ✅ damaged_files.log exists:
    for %%f in (damaged_files.log) do echo    Size: %%~zf bytes
    echo.
    echo Last entries:
    powershell "Get-Content damaged_files.log | Select-Object -Last 5"
) else (
    echo ❌ No damaged_files.log found
)

echo.
if exist skip_files.list (
    echo ✅ skip_files.list exists:
    for %%f in (skip_files.list) do echo    Size: %%~zf bytes  
    echo.
    echo Contents:
    type skip_files.list | findstr -v "^#"
) else (
    echo ❌ No skip_files.list found
)

echo.
echo ================================================
echo NEXT COPY ATTEMPT WILL:
echo 1. Detect stuck file in 10 seconds (not 30)
echo 2. Auto-switch to rescue mode after 15 seconds  
echo 3. Skip previously damaged files automatically
echo 4. Log any new damaged files
echo ================================================
echo.

echo Test with small file:
echo test > test_small.txt
filedo copy test_small.txt test_small_copy.txt
del test_small.txt test_small_copy.txt 2>nul

echo.
echo System is ready! Use:
echo   filedo copy [source] [target]     - Auto-optimization with damage protection
echo   filedo rescue [source] [target]   - Direct rescue mode for damaged disks
echo.
pause
