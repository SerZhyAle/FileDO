@echo off
echo FileDO CHECK Test Suite
echo ======================

echo.
echo Testing basic functionality...

echo.
echo 1. Testing help display
filedo_check.exe /?

echo.
echo 2. Testing with non-existent path (should show error)
filedo_check.exe Z:\nonexistent 2>nul
if errorlevel 1 echo [OK] Error handling works correctly

echo.
echo 3. Testing current directory quick check
if exist "%CD%" (
    echo Testing quick check on current directory...
    filedo_check.exe "%CD%" quick --max-files 5 --verbose
) else (
    echo [SKIP] Current directory test
)

echo.
echo 4. Testing C: drive quick check (limited files for safety)
filedo_check.exe C: quick --max-files 10 --verbose --threshold 1.0

echo.
echo 5. Testing with various options
filedo_check.exe "%CD%" balanced --max-files 3 --report csv --verbose

echo.
echo 6. Testing environment variable override
set FILEDO_CHECK_MODE=quick
set FILEDO_CHECK_VERBOSE=1
set FILEDO_CHECK_MAX_FILES=5
filedo_check.exe "%CD%" --quiet

echo.
echo Test completed. Check generated files:
echo - history.json (operation log)
echo - check_files.list (good files)
echo - skip_files.list (problematic files)
echo - check_report_*.csv (if report was generated)

pause