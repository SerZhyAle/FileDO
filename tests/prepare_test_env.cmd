@echo off
rem Creates the fixture folders and files the scenario lists expect - on the
rem disposable target named by FILEDO_TEST_TARGET (a mounted VHD drive letter or
rem a folder under %TEMP%), never on a real data volume. The checks and the files
rem live in run-test-list.ps1, which refuses any other target and exits 2.
if "%FILEDO_TEST_TARGET%"=="" (
    echo FILEDO_TEST_TARGET is not set. Point it at a mounted VHD ^(e.g. V:^) or a folder under %TEMP%.
    exit /b 2
)
pwsh -NoProfile -File "%~dp0run-test-list.ps1" -PrepareOnly
exit /b %ERRORLEVEL%
