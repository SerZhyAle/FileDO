@echo off
echo Testing compilation...
cd /d "%~dp0"
go build -o filedo_test.exe 2>&1
if %errorlevel% neq 0 (
    echo Build failed!
) else (
    echo Build successful!
)
pause