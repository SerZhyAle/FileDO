@echo off
REM ===========================================================================
REM  FileDO - CD (Check Duplicates) shortcut
REM  Scans a target for duplicate files (by content hash) and, optionally,
REM  moves or deletes the redundant copies.
REM
REM  Usage:    filedo_cd.bat <target> [options]
REM  Examples:
REM    filedo_cd.bat C:\Photos                 -> just list duplicates
REM    filedo_cd.bat C:\Photos del old         -> delete the older copy of each
REM    filedo_cd.bat D:\Data move E:\Dups new  -> move the newer copy to E:\Dups
REM
REM  Wrapper around:  filedo.exe <target> cd [options]
REM  Project:         https://github.com/SerZhyAle/FileDO
REM ===========================================================================
filedo.exe %1 cd %2 %3 %4 %5 %6 %7 %8 %9
