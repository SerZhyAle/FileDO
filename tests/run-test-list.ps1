#Requires -Version 7.0
<#
.SYNOPSIS
  Runs a tests\*.lst scenario list against a disposable target - never a real data volume.

.DESCRIPTION
  The scenario lists fill, clean, wipe and scan whatever they name: `device X: fill 10`
  writes until the volume is nearly full, and `device X: cd short` reads all of it. So the
  lists name no real path. They carry placeholders, and this runner fills them in from
  FILEDO_TEST_TARGET after refusing anything that is not disposable (SP-0030 REL-11):

    * a drive letter (V:) of a mounted virtual disk - VHD or VHDX, bus type
      "File Backed Virtual" - and never the system drive; or
    * a folder under %TEMP% (created when missing). With a folder the `device` lines are
      dropped: a device test on the volume that holds %TEMP% is one on the system drive.

  Placeholders in the lists:
    <TARGET_DRIVE>  the drive letter, e.g. V:   (lines using it are dropped for a folder)
    <TARGET_DIR>    the working folder: V:\FileDO_TestRun, or the %TEMP% folder itself

  `<` and `>` cannot appear in a Windows path, so a list run directly with `filedo from`
  can open, create or delete nothing through them (measured: stat, glob and create all fail
  with "syntax is incorrect"); only a `device ... info` line still reports the current
  directory's volume, read-only. The rendered copy is written into the
  working folder and run from there, so relative paths in a list (.\test_dups) land there too.

  A VHD for this: Disk Management > Action > Create VHD, or from an elevated console
    New-VHD -Path $env:TEMP\filedo-test.vhdx -SizeBytes 2GB -Dynamic | Mount-VHD -Passthru |
      Initialize-Disk -Passthru | New-Partition -AssignDriveLetter -UseMaximumSize | Format-Volume
  (New-VHD needs the Hyper-V module; Disk Management works everywhere.)

  Exit code: filedo's own for the run; 2 when the target is missing or refused, in which
  case nothing ran.

.EXAMPLE
  $env:FILEDO_TEST_TARGET = 'V:'; pwsh -File tests\run-test-list.ps1 -List tests\test_list.lst
.EXAMPLE
  $env:FILEDO_TEST_TARGET = "$env:TEMP\filedo-lists"; pwsh -File tests\run-test-list.ps1 -PrepareOnly
#>
[CmdletBinding()]
param(
    [string]$List = (Join-Path $PSScriptRoot 'test_list.lst'),
    [string]$Exe = (Join-Path (Split-Path $PSScriptRoot -Parent) 'exe_to_download\filedo.exe'),
    # Create the fixture folders and files, and run nothing.
    [switch]$PrepareOnly
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

function Refuse([string]$why) {
    Write-Host "run-test-list: REFUSED - $why" -ForegroundColor Yellow
    Write-Host "  Point FILEDO_TEST_TARGET at a mounted VHD (e.g. V:) or at a folder under $([IO.Path]::GetTempPath())." -ForegroundColor Yellow
    exit 2
}

$target = $env:FILEDO_TEST_TARGET
if (-not $target) { Refuse "FILEDO_TEST_TARGET is not set; the lists fill and wipe what they name." }

$drive = $null
if ($target -match '^([A-Za-z]):\\?$') {
    $letter = $Matches[1].ToUpperInvariant()
    if ("${letter}:" -ieq $env:SystemDrive) { Refuse "${letter}: is the system drive." }
    try {
        $disk = Get-Partition -DriveLetter $letter -ErrorAction Stop | Get-Disk -ErrorAction Stop
    } catch {
        Refuse "cannot tell which disk ${letter}: is on ($($_.Exception.Message)); an elevated console may be needed."
    }
    if ($disk.IsSystem -or $disk.IsBoot) { Refuse "${letter}: is on the system or boot disk." }
    if ($disk.BusType -ne 'File Backed Virtual') {
        Refuse "${letter}: is on '$($disk.FriendlyName)' ($($disk.BusType)), not on a mounted virtual disk."
    }
    $drive = "${letter}:"
    $workDir = "${letter}:\FileDO_TestRun"
} else {
    $tempRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    try { $full = [IO.Path]::GetFullPath($target).TrimEnd('\') } catch { Refuse "'$target' is not a path." }
    if (-not ($full + '\').StartsWith($tempRoot, [StringComparison]::OrdinalIgnoreCase) -or ($full + '\') -ieq $tempRoot) {
        Refuse "'$full' is neither a virtual-disk drive letter nor a folder below $tempRoot."
    }
    if ((Test-Path $full) -and ((Get-Item $full -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)) {
        Refuse "'$full' is a link; it may lead anywhere."
    }
    $workDir = $full
}

# The fixtures prepare_test_env.cmd used to write onto D:.
New-Item -ItemType Directory -Force -Path (Join-Path $workDir 'TestFolder'), (Join-Path $workDir 'TestDuplicates') | Out-Null
$fixtures = [ordered]@{
    'TestFolder\testfile.txt'              = 'This is a test file for FileDO.'
    'TestDuplicates\file1.txt'             = 'This is duplicate content 1'
    'TestDuplicates\file1_copy.txt'        = 'This is duplicate content 1'
    'TestDuplicates\file2.txt'             = 'This is duplicate content 2'
    'TestDuplicates\file2_copy.txt'        = 'This is duplicate content 2'
    'TestFolder\folder_file1.txt'          = 'Folder duplicate content 1'
    'TestFolder\folder_file1_copy.txt'     = 'Folder duplicate content 1'
    'TestFolder\folder_file2.txt'          = 'Folder duplicate content 2'
    'TestFolder\folder_file2_copy.txt'     = 'Folder duplicate content 2'
}
foreach ($f in $fixtures.Keys) { Set-Content -Path (Join-Path $workDir $f) -Value $fixtures[$f] -Encoding ascii }
Write-Host "Test environment ready in $workDir$(if ($drive) { " (virtual disk $drive)" } else { ' (folder target: device lines are dropped)' })."
if ($PrepareOnly) { exit 0 }

if (-not (Test-Path $List)) { Write-Host "run-test-list: NOT VERIFIED (no list at $List)" -ForegroundColor Yellow; exit 2 }
if (-not (Test-Path $Exe)) { Write-Host "run-test-list: NOT VERIFIED (no filedo at $Exe - run .\build.ps1 first)" -ForegroundColor Yellow; exit 2 }

$rendered = [System.Collections.Generic.List[string]]::new()
$dropped = 0
foreach ($line in (Get-Content $List)) {
    if ($line.Contains('<TARGET_DRIVE>')) {
        if (-not $drive) { $dropped++; continue }
        $line = $line.Replace('<TARGET_DRIVE>', $drive)
    }
    $rendered.Add($line.Replace('<TARGET_DIR>', $workDir))
}
$renderedPath = Join-Path $workDir ("rendered-" + (Split-Path $List -Leaf))
Set-Content -Path $renderedPath -Value $rendered -Encoding utf8NoBOM
if ($dropped) { Write-Host "Dropped $dropped device line(s): a folder target has no device of its own." }

Write-Host "Running $(Split-Path $List -Leaf) against $workDir ..."
Push-Location $workDir
try {
    & $Exe from $renderedPath
    $code = $LASTEXITCODE
} finally {
    Pop-Location
}
Write-Host "run-test-list: filedo exited $code"
exit $code
