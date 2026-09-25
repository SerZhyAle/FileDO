@echo off
rem build.bat is build.ps1 for a cmd.exe prompt: every argument is passed on
rem (build.bat -Test -SkipGui), and its exit code is build.ps1's - 0 built (and
rem passed), 1 a defect, 2 could not verify.
rem
rem It used to build filedo.exe alone with -race and CGO on - which the local
rem windows/386 toolchain cannot do without a C compiler - and then printed
rem "Build complete." whatever had happened (SP-0030 REL-10). There is one build,
rem the one the release ships, and it lives in build.ps1.
pwsh -NoProfile -File "%~dp0build.ps1" %*
exit /b %ERRORLEVEL%
