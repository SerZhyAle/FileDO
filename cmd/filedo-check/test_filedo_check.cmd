@echo off
rem Manual walk-through of filedo_check.exe against a real filedo.exe.
rem filedo_check.exe runs the filedo.exe in its own folder, so run this from a
rem folder that holds both (exe_to_download\, or an installed FileDO). Every
rem step only reads files, and only in the current folder; nothing here touches
rem a whole drive. The automated tests are "go test ./..." in cmd\filedo-check.
echo FileDO CHECK walk-through
echo =========================

if not exist filedo_check.exe goto :missing
if not exist filedo.exe goto :missing

echo.
echo 1. Help (exit 0, shows the version)
filedo_check.exe /?
echo exit code %errorlevel%

echo.
echo 2. A usage error is refused before filedo.exe starts (exit 2)
filedo_check.exe "%CD%" --resume
if %errorlevel%==2 (echo [OK] exit 2) else (echo [FAIL] exit %errorlevel%, want 2)

echo.
echo 3. A path that does not exist cannot be verified (exit 2)
filedo_check.exe Z:\nonexistent
if %errorlevel%==2 (echo [OK] exit 2) else (echo [FAIL] exit %errorlevel%, want 2)

echo.
echo 4. Quick check of the current folder, at most 5 files
filedo_check.exe "%CD%" quick --max-files 5 --verbose
echo exit code %errorlevel% (0 passed, 1 damaged files found, 2 could not verify)

echo.
echo 5. Balanced check with a CSV report, at most 3 files
filedo_check.exe "%CD%" balanced --max-files 3 --report csv
echo exit code %errorlevel%

echo.
echo 6. An environment setting filedo.exe reads (FILEDO_CHECK_MAX_FILES)
set FILEDO_CHECK_MAX_FILES=5
filedo_check.exe "%CD%" --quiet
echo exit code %errorlevel%
set FILEDO_CHECK_MAX_FILES=

echo.
echo Done. filedo.exe wrote into the current folder:
echo - history.json (operation log)
echo - check_files.list (files that read cleanly)
echo - skip_files.list (files judged damaged, if any)
echo - check_report_*.csv (from step 5)
goto :end

:missing
echo filedo_check.exe and filedo.exe must both be in the current folder.
exit /b 2

:end
pause
