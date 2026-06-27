@echo off
REM ===========================================================================
REM  FileDO - CLEAN shortcut
REM  Deletes leftover FileDO test files (FILL_*.tmp, speedtest_*.txt) that the
REM  fill / speed / test commands may leave behind on a target.
REM
REM  Usage:    filedo_clean.bat <target>
REM  Examples:
REM    filedo_clean.bat D:        -> remove all test files from drive D:
REM    filedo_clean.bat C:\Temp   -> clean test files inside a folder
REM
REM  Wrapper around:  filedo.exe <target> clean
REM  Project:         https://github.com/SerZhyAle/FileDO
REM ===========================================================================
filedo.exe %1 clean %2 %3 %4 %5 %6 %7 %8 %9
