@echo off
echo FileDO CHECK - Project Structure Verification
echo ============================================

echo.
echo Checking project files:

if exist main.go (
    echo [OK] main.go - Main application file
) else (
    echo [MISSING] main.go
)

if exist go.mod (
    echo [OK] go.mod - Module definition
) else (
    echo [MISSING] go.mod  
)

if exist check_core.go (
    echo [OK] check_core.go - Core CHECK functionality
) else (
    echo [MISSING] check_core.go
)

if exist device_check.go (
    echo [OK] device_check.go - Device operations
) else (
    echo [MISSING] device_check.go
)

if exist folder_check.go (
    echo [OK] folder_check.go - Folder operations  
) else (
    echo [MISSING] folder_check.go
)

if exist network_check.go (
    echo [OK] network_check.go - Network operations
) else (
    echo [MISSING] network_check.go
)

if exist interrupt.go (
    echo [OK] interrupt.go - Signal handling
) else (
    echo [MISSING] interrupt.go
)

if exist progress.go (
    echo [OK] progress.go - Progress tracking
) else (
    echo [MISSING] progress.go
)

if exist utils.go (
    echo [OK] utils.go - Utility functions
) else (
    echo [MISSING] utils.go
)

if exist README.md (
    echo [OK] README.md - Documentation
) else (
    echo [MISSING] README.md
)

if exist build.cmd (
    echo [OK] build.cmd - Build script
) else (
    echo [MISSING] build.cmd
)

echo.
echo Checking Go installation:
where go >nul 2>nul
if errorlevel 1 (
    echo [INFO] Go compiler not found in PATH
    echo       Download from: https://golang.org/dl/
    echo       After installing Go, run: build.cmd
) else (
    echo [OK] Go compiler found
    echo      To build: run build.cmd or go build -o filedo_check.exe
)

echo.
echo Project structure verification complete.
echo To build the project, ensure Go is installed and run: build.cmd

pause