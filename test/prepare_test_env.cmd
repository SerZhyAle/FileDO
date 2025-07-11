@echo off
echo Creating test environment for FileDO testing...

REM Create test folders
mkdir D:\TestFolder 2>nul
mkdir D:\TestDuplicates 2>nul

REM Create test file
echo This is a test file for FileDO. > D:\TestFolder\testfile.txt

REM Create duplicate files for testing
echo This is duplicate content 1 > D:\TestDuplicates\file1.txt
echo This is duplicate content 1 > D:\TestDuplicates\file1_copy.txt
echo This is duplicate content 2 > D:\TestDuplicates\file2.txt
echo This is duplicate content 2 > D:\TestDuplicates\file2_copy.txt

REM Create duplicate files in test folder
echo Folder duplicate content 1 > D:\TestFolder\folder_file1.txt
echo Folder duplicate content 1 > D:\TestFolder\folder_file1_copy.txt
echo Folder duplicate content 2 > D:\TestFolder\folder_file2.txt
echo Folder duplicate content 2 > D:\TestFolder\folder_file2_copy.txt

echo Test environment created successfully.
echo Run filedo.exe from .\test\test_list.lst to start testing.
