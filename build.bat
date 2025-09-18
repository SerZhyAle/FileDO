@echo off
setlocal enabledelayedexpansion

echo Getting version from Git...
set "GIT_VERSION="
for /f "tokens=*" %%g in ('git log -1 --format^="%%cd" --date^=format:"%%y%%m%%d%%H%%M"') do (
    set "GIT_VERSION=%%g"
)

if "!GIT_VERSION!"=="" (
    echo Warning: Failed to get version from Git. Using current time.
    for /f "tokens=1-4 delims=/: " %%a in ("%TIME%") do (
        set "HH=%%a"
        set "MM=%%b"
    )
    for /f "tokens=1-3 delims=.-/ " %%a in ("%DATE%") do (
        set "YY=%%c"
        set "DD=%%a"
        set "MO=%%b"
    )
    set "YY=!YY:~-2!"
    set "GIT_VERSION=!YY!!MO!!DD!!HH!!MM!"
)

echo Building filedo.exe with version: !GIT_VERSION!

go build -ldflags="-X 'main.version=!GIT_VERSION!'" -o filedo.exe .

echo.
echo Build complete.
endlocal