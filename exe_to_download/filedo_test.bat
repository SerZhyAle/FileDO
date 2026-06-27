@echo off
REM ===========================================================================
REM  FileDO - TEST (capacity) shortcut
REM  Verifies real storage capacity to catch fake / counterfeit drives. Writes
REM  files spread across the device and reads them back to confirm the space.
REM
REM  Usage:    filedo_test.bat <target> [file_count] [del]
REM  Examples:
REM    filedo_test.bat E:        -> capacity test on drive E: (100 files)
REM    filedo_test.bat E: 1000   -> more thorough test (1000 files)
REM    filedo_test.bat E: del    -> test, then auto-delete the test files
REM
REM  Wrapper around:  filedo.exe <target> test [count] [del]
REM  Project:         https://github.com/SerZhyAle/FileDO
REM ===========================================================================
filedo.exe %1 test %2 %3 %4 %5 %6 %7 %8 %9
