@echo off
REM ===========================================================================
REM  FileDO - FILL shortcut
REM  Fills a target with test files until it is full. Handy for wiping free
REM  space (add "del") or for exposing fake-capacity USB / SD cards.
REM
REM  Usage:    filedo_fill.bat <target> [size_MB] [del]
REM  Examples:
REM    filedo_fill.bat D:           -> fill drive D: with 100MB files
REM    filedo_fill.bat D: 500       -> fill using 500MB files
REM    filedo_fill.bat D: 1000 del  -> fill, then securely delete (free-space wipe)
REM
REM  Wrapper around:  filedo.exe <target> fill [size] [del]
REM  Project:         https://github.com/SerZhyAle/FileDO
REM ===========================================================================
filedo.exe %1 fill %2 %3 %4 %5 %6 %7 %8 %9
