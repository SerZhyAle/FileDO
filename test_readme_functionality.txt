# Comprehensive Test Script for README.md Functionality
# Tests all features declared in README.md
# Run with: filedo.exe from test_readme_functionality.txt

# =====================================
# 1. DEVICE OPERATIONS TESTS
# =====================================

# Basic Information Tests
device C: info
device C: short
device C:

# Performance Testing
device C: speed 100
device C: speed 500 short
device C: speed 100 nodel

# Fake Capacity Detection (on smaller drive or folder to avoid issues)
# Note: Commented out disk tests to avoid filling system drives
# device F: test
# device F: test del

# Space Management Tests (commented for safety)
# device F: fill 100
# device F: fill 100 del
device F: clean

# =====================================
# 2. FOLDER OPERATIONS TESTS
# =====================================

# Create test folder first
folder F:\temp\filedo_test info
folder F:\temp\filedo_test short
folder F:\temp\filedo_test

# Performance Testing
folder F:\temp\filedo_test speed 50
folder F:\temp\filedo_test speed 100 short
folder C:\temp\filedo_test speed 50 nodel

# Capacity Testing
folder F:\temp\filedo_test test
folder F:\temp\filedo_test test del

# Space Management
folder F:\temp\filedo_test fill 100
folder F:\temp\filedo_test fill 50 del
folder F:\temp\filedo_test clean

# =====================================
# 3. NETWORK OPERATIONS TESTS
# =====================================

# Note: These will only work if network shares are available
# Uncomment and modify paths as needed
# network \\localhost\c$ info
# network \\localhost\c$ short
# network \\localhost\c$ speed 50
# network \\localhost\c$ test del
# network \\localhost\c$ clean

# =====================================
# 4. FILE OPERATIONS TESTS
# =====================================

# Create a test file first for file operations
# (This will be tested after creating some files)

# =====================================
# 5. COMMAND OPTIONS & MODIFIERS TESTS
# =====================================

# Output Control Tests
folder C:\temp\filedo_test info
folder C:\temp\filedo_test short

# File Management Tests
folder C:\temp\filedo_test speed 50 del
folder C:\temp\filedo_test speed 50 nodel
folder C:\temp\filedo_test speed 50 nodelete

# Size Specifications Tests
folder C:\temp\filedo_test speed 50
folder C:\temp\filedo_test speed 100
folder C:\temp\filedo_test speed 200
folder C:\temp\filedo_test speed max

# =====================================
# 6. BATCH OPERATIONS TESTS
# =====================================

# This file itself tests batch operations
# Additional nested batch test would go here

# =====================================
# 7. HISTORY & MONITORING TESTS
# =====================================

hist
history

# =====================================
# 8. VERIFICATION SYSTEM TESTS
# =====================================

# Two-Stage Verification Test
folder C:\temp\filedo_test test

# Error Handling Test (will create files then verify)
folder F:\temp\filedo_test fill 100
folder F:\temp\filedo_test clean

# =====================================
# 9. ADVANCED FEATURES TESTS
# =====================================

# Memory-Optimized Test (large file)
folder C:\temp\filedo_test speed max

# Real-Time Progress Test
folder F:\temp\filedo_test test

# Smart Error Handling Test
folder F:\temp\filedo_test test del

# =====================================
# 10. CLEANUP
# =====================================

# Clean up all test files
folder F:\temp\filedo_test clean
device F: clean
