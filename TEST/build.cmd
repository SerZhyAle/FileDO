@echo off
REM Build FileDO TEST project

echo.
echo =====================================
echo Building FileDO TEST
echo =====================================
echo.

cd /d "%~dp0"

echo Building filedo_test.exe...
go build -o filedo_test.exe

if %errorlevel% neq 0 (
    echo.
    echo ERROR: Build failed!
    echo Check Go installation and dependencies.
    pause
    exit /b 1
)

echo.
echo SUCCESS: filedo_test.exe built successfully!
echo.
echo File location: %CD%\filedo_test.exe
echo.

REM Show file size
for %%A in (filedo_test.exe) do echo File size: %%~zA bytes

echo.
echo To test the application:
echo   filedo_test.exe C:           - Test C: drive
echo   filedo_test.exe C:\temp      - Test folder
echo   filedo_test.exe /?           - Show help
echo.
echo =====================================
pause