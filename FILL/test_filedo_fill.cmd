@echo off
echo.
echo ======================================
echo FileDO FILL SubProject Test
echo ======================================
echo.

echo Creating test folder...
mkdir test_fill 2>nul

echo.
echo Testing FILL functionality:
echo.

rem Test 1: Show help
echo [TEST 1] Showing help:
FILL\filedo_fill.exe help
echo.

rem Test 2: Try to fill test folder with small files
echo [TEST 2] Fill test folder with 1MB files:
FILL\filedo_fill.exe test_fill 1
echo.

rem Test 3: Clean test files
echo [TEST 3] Clean test files:
FILL\filedo_fill.exe test_fill clean
echo.

echo.
echo ======================================
echo Test completed. Check results above.
echo ======================================
echo.
pause