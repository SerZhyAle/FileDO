@echo off
echo ===========================================
echo DEMO: Stuck File Detection and Auto-Rescue
echo ===========================================
echo.

echo 1. Testing with potentially damaged file:
echo    f:\mov\_UnSORT\1988 My Neighbor Totoro\DISC\BDMV\STREAM\00009.m2ts
echo.

echo Expected behavior:
echo - Fast copy will detect stuck file after 10 seconds
echo - System will automatically switch to SAFE RESCUE mode
echo - File will be logged and skipped after 10 second timeout
echo.

echo Press any key to start test...
pause > nul

echo Starting fast copy (should detect stuck file and auto-switch)...
filedo copy "f:\mov\_UnSORT\1988 My Neighbor Totoro\DISC\BDMV\STREAM\00009.m2ts" c:\temp\test_auto.m2ts

echo.
echo Test completed. Checking results...
echo.

if exist c:\temp\test_auto.m2ts (
    echo SUCCESS: File was copied successfully
) else (
    echo INFO: File was not copied (likely skipped due to damage)
)

echo.
echo Checking log files:
if exist damaged_files.log (
    echo DAMAGED FILES LOG:
    type damaged_files.log | findstr "2025-08-24 02:" | tail -5
) else (
    echo No damaged files log found
)

echo.
if exist skip_files.list (
    echo SKIP LIST:
    type skip_files.list | findstr "m2ts"
) else (
    echo No skip list found
)

echo.
echo ===========================================
echo Demo completed
echo ===========================================
pause
