# PowerShell Script to Test All README.md Functionality
# Run with: .\Test-ReadmeFunctionality.ps1

param(
    [switch]$Quick = $false,
    [switch]$Full = $false,
    [string]$TestType = "all"
)

Write-Host "=== FileDO README.md Functionality Test Suite ===" -ForegroundColor Green
Write-Host "Testing all features declared in README.md" -ForegroundColor Yellow
Write-Host ""

# Ensure filedo.exe exists
if (-not (Test-Path ".\filedo.exe")) {
    Write-Host "ERROR: filedo.exe not found in current directory" -ForegroundColor Red
    Write-Host "Please ensure you're running this script from the FileDO directory" -ForegroundColor Red
    exit 1
}

# Create test directories
Write-Host "Creating test directories..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path "C:\temp\filedo_test" | Out-Null
New-Item -ItemType Directory -Force -Path "C:\temp\filedo_quick_test" | Out-Null
New-Item -ItemType Directory -Force -Path "C:\temp\filedo_verification_test" | Out-Null
New-Item -ItemType Directory -Force -Path "C:\temp\filedo_progress_test" | Out-Null
New-Item -ItemType Directory -Force -Path "C:\temp\filedo_memory_test" | Out-Null

function Run-Test {
    param([string]$TestFile, [string]$Description)
    
    Write-Host "`n=== $Description ===" -ForegroundColor Yellow
    Write-Host "Running test file: $TestFile" -ForegroundColor Cyan
    
    if (Test-Path $TestFile) {
        $startTime = Get-Date
        .\filedo.exe from $TestFile
        $endTime = Get-Date
        $duration = $endTime - $startTime
        Write-Host "Test completed in: $($duration.TotalSeconds) seconds" -ForegroundColor Green
    } else {
        Write-Host "ERROR: Test file $TestFile not found" -ForegroundColor Red
    }
}

# Run tests based on parameters
switch ($TestType.ToLower()) {
    "quick" {
        Run-Test "test_quick_readme.txt" "Quick README Functionality Test"
        Run-Test "test_device_features.txt" "Device Features Test"
    }
    "verification" {
        Run-Test "test_verification_system.txt" "Verification System Test"
        Run-Test "test_progress_format.txt" "Progress Format Test"
    }
    "memory" {
        Run-Test "test_memory_optimization.txt" "Memory Optimization Test"
    }
    "all" {
        if ($Quick) {
            Run-Test "test_quick_readme.txt" "Quick README Functionality Test"
        } elseif ($Full) {
            Run-Test "test_readme_functionality.txt" "Full README Functionality Test"
        } else {
            # Default: Run all individual tests
            Run-Test "test_quick_readme.txt" "Quick README Functionality Test"
            Run-Test "test_device_features.txt" "Device Features Test"
            Run-Test "test_verification_system.txt" "Verification System Test"
            Run-Test "test_progress_format.txt" "Progress Format Test"
            Run-Test "test_memory_optimization.txt" "Memory Optimization Test"
        }
    }
    default {
        Write-Host "Invalid test type. Use: quick, verification, memory, or all" -ForegroundColor Red
        exit 1
    }
}

# Final verification
Write-Host "`n=== Final Verification ===" -ForegroundColor Yellow
Write-Host "Checking history functionality..." -ForegroundColor Cyan
.\filedo.exe hist

Write-Host "`n=== Test Suite Complete ===" -ForegroundColor Green
Write-Host "All README.md functionality tests have been executed." -ForegroundColor Yellow
Write-Host ""
Write-Host "Test files created:" -ForegroundColor Cyan
Write-Host "  - test_readme_functionality.txt (Full test suite)" -ForegroundColor White
Write-Host "  - test_quick_readme.txt (Quick test)" -ForegroundColor White
Write-Host "  - test_verification_system.txt (Verification features)" -ForegroundColor White
Write-Host "  - test_progress_format.txt (Progress display)" -ForegroundColor White
Write-Host "  - test_device_features.txt (Device operations)" -ForegroundColor White
Write-Host "  - test_memory_optimization.txt (Memory optimization)" -ForegroundColor White
Write-Host ""
Write-Host "Usage examples:" -ForegroundColor Cyan
Write-Host "  .\Test-ReadmeFunctionality.ps1 -Quick" -ForegroundColor White
Write-Host "  .\Test-ReadmeFunctionality.ps1 -Full" -ForegroundColor White
Write-Host "  .\Test-ReadmeFunctionality.ps1 -TestType verification" -ForegroundColor White
