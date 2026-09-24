#Requires -Version 7.0
<#
.SYNOPSIS
  Assert that every .sha256 sidecar in a directory matches the asset it names.

.DESCRIPTION
  Each release asset ships with a <asset>.sha256 beside it. That file is the download page's
  integrity contract - it is what a user is told to compare against - and it is what the winget
  manifest's InstallerSha256 is synced from, so a sidecar that drifted from its asset is wrong
  in two channels at once.

  The release workflow writes the sidecars as it builds each artifact and then calls this to
  re-read all of them and re-hash the files they name, before anything is published.

  Expected shape, as the workflow writes it: "<64 hex digits>  <file name>", the file sitting
  beside the sidecar, the sidecar named "<file name>.sha256".

  Exit code: 0 = every sidecar matches, 1 = one does not, 2 = could not verify.

.PARAMETER Path
  The directory holding the assets and their sidecars. Default: dist.

.PARAMETER Expect
  How many sidecars must be there. The release publishes three (setup exe, zip, msi); a
  missing one is a silently broken download button, so the count is checked, not assumed.

.EXAMPLE
  pwsh -NoProfile -File .\packaging\verify-sha256-sidecars.ps1 -Path dist -Expect 3
#>
[CmdletBinding()]
param(
    [string]$Path = "dist",
    [int]$Expect = 0
)

$ErrorActionPreference = "Stop"
if (-not (Test-Path $Path)) { Write-Host "sha256-sidecars: NOT VERIFIED ($Path does not exist)" -ForegroundColor Yellow; exit 2 }

$bad      = @()
$sidecars = @(Get-ChildItem (Join-Path $Path "*.sha256") -File -ErrorAction SilentlyContinue)
if ($Expect -gt 0 -and $sidecars.Count -ne $Expect) {
    $bad += "expected $Expect .sha256 sidecars in $Path, found $($sidecars.Count)"
}
if ($sidecars.Count -eq 0) {
    Write-Host "sha256-sidecars: NOT VERIFIED (no .sha256 sidecars in $Path)" -ForegroundColor Yellow
    exit 2
}

foreach ($s in $sidecars) {
    $asset = Join-Path $s.DirectoryName $s.BaseName
    $parts = (Get-Content $s.FullName -Raw).Trim() -split '\s+'
    $here  = @()
    if ($parts.Count -ne 2) { $bad += "$($s.Name): expected '<hash>  <name>', got '$($parts -join ' ')'"; continue }
    if ($parts[1] -cne $s.BaseName) { $here += "$($s.Name): names '$($parts[1])' but sits beside '$($s.BaseName)'" }
    if (-not (Test-Path $asset))    { $bad += $here + "$($s.Name): $($s.BaseName) is not there"; continue }
    $real = (Get-FileHash $asset -Algorithm SHA256).Hash.ToUpper()
    if ($parts[0].ToUpper() -cne $real) { $here += "$($s.Name): says $($parts[0]), the asset hashes to $real" }
    # One line per sidecar: a file with a problem is never also reported as OK.
    if ($here.Count) { $bad += $here } else { Write-Host "  OK  $($s.Name)  $real" -ForegroundColor Green }
}

if ($bad.Count) {
    $bad | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "sha256-sidecars: FAIL ($($bad.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "sha256-sidecars: PASS ($($sidecars.Count) checks)" -ForegroundColor Green
exit 0
