@echo off
echo Advanced testing of FileDO damaged disk functionality
echo.

rem Clean previous tests
rmdir /s /q test_source_damaged 2>nul
rmdir /s /q test_target_rescue 2>nul  
rmdir /s /q test_target_rescue_safe 2>nul
rmdir /s /q test_target_rescue_damaged 2>nul
del damaged_files.log 2>nul
del skip_files.list 2>nul

rem Create test directory structure
mkdir test_source_damaged 2>nul

rem Create normal test files
echo This is a normal file that should copy fine > test_source_damaged\normal1.txt
echo Another normal file > test_source_damaged\normal2.txt
echo Small content > test_source_damaged\small.txt

rem Create a larger file for more realistic test
fsutil file createnew test_source_damaged\large_file.dat 1048576 >nul 2>&1

rem Create directory structure
mkdir test_source_damaged\subfolder 2>nul
echo Subfolder file content > test_source_damaged\subfolder\file_in_subfolder.txt

echo Test files created in test_source_damaged\
dir test_source_damaged /s
echo.

echo Testing RESCUE mode for damaged disk simulation:
echo.
filedo.exe rescue test_source_damaged test_target_rescue_damaged
echo.

echo Testing second run (should use skip list if any files were marked as damaged):
echo.  
filedo.exe rescue test_source_damaged test_target_rescue_damaged_2nd
echo.

echo Checking results:
echo.

if exist test_target_rescue_damaged (
    echo Target directory created successfully:
    dir test_target_rescue_damaged /s
) else (
    echo Warning: Target directory not created
)

echo.
echo Log files status:

if exist damaged_files.log (
    echo === DAMAGED FILES LOG ===
    type damaged_files.log
    echo.
) else (
    echo No damaged_files.log (good - no damaged files detected)
)

if exist skip_files.list (
    echo === SKIP FILES LIST ===
    type skip_files.list
    echo.  
) else (
    echo No skip_files.list (good - no damaged files to skip)
)

echo.
echo Advanced test completed!
echo - All normal files should be copied successfully
echo - Large files should be handled appropriately
echo - Directory structure should be preserved
echo - No timeouts should occur with normal files
echo.
pause
