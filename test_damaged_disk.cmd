@echo off
echo Testing FileDO damaged disk functionality
echo.

rem Create test directory structure
mkdir test_source_damaged 2>nul
mkdir test_target_rescue 2>nul

rem Create some test files
echo Normal file content > test_source_damaged\normal_file.txt
echo Small file > test_source_damaged\small.txt
echo Large content that should be copied successfully > test_source_damaged\large_file.txt

rem Create a zero-byte file (simulating potential issue)
type nul > test_source_damaged\zero_file.txt

echo Test files created in test_source_damaged\
echo.

echo Testing normal copy (should handle damaged files gracefully):
echo.
filedo.exe copy test_source_damaged test_target_rescue
echo.

echo Testing safe rescue mode (maximum protection):
echo.
filedo.exe safecopy test_source_damaged test_target_rescue_safe
echo.

echo Testing damaged disk mode explicitly:
echo.
filedo.exe damaged test_source_damaged test_target_rescue_damaged
echo.

echo Check for log files:
if exist damaged_files.log (
    echo damaged_files.log created:
    type damaged_files.log
) else (
    echo No damaged_files.log (good - no damaged files detected)
)

if exist skip_files.list (
    echo skip_files.list created:
    type skip_files.list  
) else (
    echo No skip_files.list (good - no damaged files to skip)
)

echo.
echo Test completed! Check the target directories for results.
pause
