@echo off
echo Cleaning up FileDO test environment...

REM Clean test files
if exist D:\TestFolder\*.* (
    echo Cleaning D:\TestFolder...
    del /F /Q D:\TestFolder\*.* 2>nul
    rmdir /S /Q D:\TestFolder 2>nul
)

if exist D:\TestDuplicates\*.* (
    echo Cleaning D:\TestDuplicates...
    del /F /Q D:\TestDuplicates\*.* 2>nul
    rmdir /S /Q D:\TestDuplicates 2>nul
)

if exist D:\FILL_*.* (
    echo Cleaning test files from drive D:...
    del /F /Q D:\FILL_*.* 2>nul
)

if exist D:\speedtest_*.* (
    echo Cleaning speed test files from drive D:...
    del /F /Q D:\speedtest_*.* 2>nul
)

echo Test environment cleanup complete.
