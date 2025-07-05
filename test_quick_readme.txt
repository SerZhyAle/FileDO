# Quick Test Suite for Core README.md Features
# Tests essential functionality declared in README.md
# Run with: filedo.exe from test_quick_readme.txt

# Create test directory
folder C:\temp\filedo_quick_test info

# Use small partition here
# Test Core Features
folder F:\temp\filedo_quick_test speed 50
folder F:\temp\filedo_quick_test test del
folder F:\temp\filedo_quick_test fill 50 del
folder F:\temp\filedo_quick_test clean

# Test Command Options
folder C:\temp\filedo_quick_test short
folder C:\temp\filedo_quick_test speed 50 short
folder C:\temp\filedo_quick_test speed 50 nodel
folder C:\temp\filedo_quick_test clean

# Test History
hist
