@echo off
rem Double-click to build and open FileDO for manual testing.
rem Optional: run.bat -Test also runs the existing build gate.
pwsh -NoProfile -File "%~dp0run.ps1" %*
set "run_exit=%ERRORLEVEL%"
if not "%run_exit%"=="0" pause
exit /b %run_exit%
