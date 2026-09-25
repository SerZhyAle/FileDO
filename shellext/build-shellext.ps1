<#
.SYNOPSIS
  Build FileDOShell.dll, the packaged first-level Explorer command (SP-0020).

.DESCRIPTION
  One translation unit, x64, compiled with MSVC from the Visual Studio C++ workload
  (component Microsoft.VisualStudio.Component.VC.Tools.x86.x64) against the Windows SDK.
  The CRT is linked statically (/MT) so the DLL needs nothing Explorer or its COM
  surrogate does not already have; the import table is then checked to say so.

  Called by msix\build-msix.ps1, which stages the DLL into the package. Only the
  packaged build ships it (SP-0020 D2): the MSI, the zip and winget keep the classic
  registration and never carry this file.

.PARAMETER OutDir
  Where FileDOShell.dll is written. Default: shellext\out.

.PARAMETER FileVersion
  Four-part PE file version (a.b.c.d), the same remap build-msix.ps1 uses for the
  package. Default: 0.0.0.0.

.EXAMPLE
  .\shellext\build-shellext.ps1
#>
[CmdletBinding()]
param(
    [string]$OutDir,
    [ValidatePattern('^\d{1,5}\.\d{1,5}\.\d{1,5}\.\d{1,5}$')]
    [string]$FileVersion = "0.0.0.0"
)

$ErrorActionPreference = "Stop"
$src = $PSScriptRoot
if (-not $OutDir) { $OutDir = Join-Path $src "out" }
function Fail([string]$m) { throw "build-shellext: $m" }

$vsw = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
if (-not (Test-Path $vsw)) { Fail "vswhere not found. Install Visual Studio (or Build Tools) with the C++ x64 tools." }
$vs = & $vsw -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath | Select-Object -First 1
if (-not $vs) { Fail "no Visual Studio with the MSVC x64 tools. Add the component: Visual Studio Installer > Modify > 'MSVC ... x64/x86 build tools' (Microsoft.VisualStudio.Component.VC.Tools.x86.x64)." }
$vcvars = Join-Path $vs "VC\Auxiliary\Build\vcvars64.bat"
if (-not (Test-Path $vcvars)) { Fail "vcvars64.bat not found under $vs." }

New-Item -ItemType Directory -Path $OutDir -Force | Out-Null
$obj = Join-Path $OutDir "obj"
New-Item -ItemType Directory -Path $obj -Force | Out-Null
$dll = Join-Path $OutDir "FileDOShell.dll"

# The version resource, written fresh so the stamp is the build's and not a committed file's.
$v = $FileVersion.Split('.')
$rc = Join-Path $obj "FileDOShell.rc"
@"
#include <winver.h>
VS_VERSION_INFO VERSIONINFO
 FILEVERSION $($v -join ',')
 PRODUCTVERSION $($v -join ',')
 FILEOS VOS_NT_WINDOWS32
 FILETYPE VFT_DLL
BEGIN
  BLOCK "StringFileInfo"
  BEGIN
    BLOCK "040904b0"
    BEGIN
      VALUE "CompanyName", "SZA"
      VALUE "FileDescription", "FileDO Explorer command"
      VALUE "FileVersion", "$FileVersion"
      VALUE "InternalName", "FileDOShell"
      VALUE "OriginalFilename", "FileDOShell.dll"
      VALUE "ProductName", "FileDO"
      VALUE "ProductVersion", "$FileVersion"
    END
  END
  BLOCK "VarFileInfo"
  BEGIN
    VALUE "Translation", 0x409, 1200
  END
END
"@ | Set-Content -Path $rc -Encoding ASCII

$res = Join-Path $obj "FileDOShell.res"
$cpp = Join-Path $src "FileDOShell.cpp"
$def = Join-Path $src "FileDOShell.def"
$objFile = Join-Path $obj "FileDOShell.obj"

# Every tool runs inside one vcvars64 environment, through a script file: cmd.exe's
# quoting rules make an inline command line with quoted paths fragile.
function Invoke-Vc([string[]]$lines) {
    $bat = Join-Path $obj "step.cmd"
    (@('@echo off', "call `"$vcvars`" >nul || exit /b 1") + $lines) | Set-Content -Path $bat -Encoding ASCII
    $o = & cmd.exe /d /c $bat 2>&1 | Out-String
    return @{ Code = $LASTEXITCODE; Out = $o }
}

# /MT static CRT, /GS and /guard:cf on, /W4 /WX so a warning is a failed build.
$r = Invoke-Vc @(
    "rc /nologo /fo `"$res`" `"$rc`" || exit /b 1",
    "cl /nologo /c /O2 /MT /W4 /WX /EHsc /GS /guard:cf /permissive- /std:c++17 /DUNICODE /D_UNICODE /DWIN32_LEAN_AND_MEAN /Fo`"$objFile`" `"$cpp`" || exit /b 1",
    "link /nologo /DLL /DEF:`"$def`" /OUT:`"$dll`" /MACHINE:X64 /GUARD:CF /DYNAMICBASE /NXCOMPAT /OPT:REF /OPT:ICF `"$objFile`" `"$res`" kernel32.lib ole32.lib uuid.lib || exit /b 1")
if ($r.Code -ne 0) { Write-Host $r.Out; Fail "compile/link failed ($($r.Code))" }

# What the DLL imports is the handler's contract with Explorer: system DLLs only, and
# no C runtime of its own to load.
$dump = (Invoke-Vc @("dumpbin /nologo /dependents `"$dll`"")).Out
$deps = @([regex]::Matches($dump, '(?im)^\s+(\S+\.dll)\s*$') | ForEach-Object { $_.Groups[1].Value.ToLowerInvariant() })
$allowed = @('kernel32.dll', 'ole32.dll')
$extra = @($deps | Where-Object { $allowed -notcontains $_ -and $_ -notlike 'api-ms-win-*' })
if (-not $deps.Count) { Fail "dumpbin reported no dependents for $dll." }
if ($extra.Count) { Fail "FileDOShell.dll imports $($extra -join ', '); only $($allowed -join ', ') are allowed." }
$exports = (Invoke-Vc @("dumpbin /nologo /exports `"$dll`"")).Out
foreach ($e in 'DllGetClassObject', 'DllCanUnloadNow') {
    if ($exports -notmatch "\b$e\b") { Fail "FileDOShell.dll does not export $e." }
}
Write-Host "FileDOShell.dll built: $dll (imports: $($deps -join ', '))"
