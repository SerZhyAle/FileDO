@echo off
REM ===========================================================================
REM  FileDO - SPEED shortcut
REM  Measures read/write speed of a target by writing a temporary test file.
REM
REM  Usage:    filedo_speed.bat <target> [size_MB | max] [short] [nodel]
REM  Examples:
REM    filedo_speed.bat D:            -> default speed test on drive D:
REM    filedo_speed.bat D: 500        -> test using a 500MB file
REM    filedo_speed.bat D: max short  -> 10GB test, results only
REM    filedo_speed.bat D: 1000 nodel -> keep the test file after measuring
REM
REM  Wrapper around:  filedo.exe <target> speed [size] [options]
REM  Project:         https://github.com/SerZhyAle/FileDO
REM ===========================================================================
filedo.exe %1 speed %2 %3 %4 %5 %6 %7 %8 %9
