@echo off
echo Building FileDO CHECK...

rem Check if go is available
where go >nul 2>nul
if errorlevel 1 (
    echo Error: Go compiler not found in PATH
    echo Please install Go or add it to your PATH environment variable
    pause
    exit /b 1
)

echo Cleaning previous build...
if exist filedo_check.exe del filedo_check.exe

echo Updating dependencies...
go mod tidy

echo Building application...
go build -o filedo_check.exe

if exist filedo_check.exe (
    echo.
    echo Build successful! Created filedo_check.exe
    echo.
    echo Testing help output:
    filedo_check.exe /?
) else (
    echo.
    echo Build failed!
)

pause