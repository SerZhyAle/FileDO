@echo off
REM FileDO TEST - Storage Capacity Testing Tool
REM Test script for testing FileDO TEST functionality

echo.
echo =====================================
echo FileDO TEST - Test Script
echo =====================================
echo.

REM Build the project
echo Building FileDO TEST...
cd /d "%~dp0"
go build -o filedo_test.exe
if %errorlevel% neq 0 (
    echo ERROR: Failed to build filedo_test.exe
    pause
    exit /b 1
)

echo Build successful!
echo.

REM Show usage
echo Testing help display...
filedo_test.exe /?
echo.

REM Test with a small folder (create test folder if needed)
set TEST_FOLDER=%TEMP%\filedo_test_demo
if not exist "%TEST_FOLDER%" mkdir "%TEST_FOLDER%"

echo Testing folder capacity test (with auto-delete)...
echo Target: %TEST_FOLDER%
echo.
echo WARNING: This will create large test files in %TEST_FOLDER%
echo Press Ctrl+C to cancel or any key to continue...
pause >nul

REM Run the test
filedo_test.exe "%TEST_FOLDER%" del

echo.
echo Test completed!
echo.

REM Cleanup test folder
if exist "%TEST_FOLDER%" rmdir /s /q "%TEST_FOLDER%"

echo Cleanup completed.
echo.
echo =====================================
echo Test script finished.
echo Press any key to exit...
pause >nul