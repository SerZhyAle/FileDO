#Requires -Version 7.0
<#
.SYNOPSIS
  Validates FileDO's CHECK-PLACEMENT registry in both directions.

.DESCRIPTION
  The JSON Lines registry records each check's runner class, owner ticket and
  reason. This check proves every record still names a real runner that invokes
  it, and every check script under packaging\ and msix\test-* has a record.

  Exit code: 0 = pass, 1 = placement defect, 2 = could not verify.
#>
[CmdletBinding()]
param(
    [string]$Registry = (Join-Path $PSScriptRoot 'check-placement.jsonl')
)

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if (-not (Test-Path $Registry)) {
    Write-Host "check-placement: NOT VERIFIED (registry is missing: $Registry)" -ForegroundColor Yellow
    exit 2
}

$records = @()
try {
    $records = @(Get-Content $Registry | Where-Object { $_.Trim() } | ForEach-Object { $_ | ConvertFrom-Json })
} catch {
    Write-Host "check-placement: NOT VERIFIED (registry is unreadable: $($_.Exception.Message))" -ForegroundColor Yellow
    exit 2
}

$failures = [System.Collections.Generic.List[string]]::new()
$requiredFields = 'check', 'class', 'date', 'ticket', 'state', 'reason', 'runnerFile', 'runnerToken'
$seen = @{}
foreach ($record in $records) {
    $names = @($record.PSObject.Properties.Name)
    $missing = @($requiredFields | Where-Object { $_ -notin $names -or -not [string]$record.$_ })
    if ($missing.Count) { $failures.Add("record is missing $($missing -join ', ')"); continue }
    if ($seen[$record.check]) { $failures.Add("duplicate record for $($record.check)") }
    $seen[$record.check] = $true
    if ($record.state -ne 'judged') { $failures.Add("$($record.check) is $($record.state), not judged") }
    $runnerPath = Join-Path $root $record.runnerFile
    if (-not (Test-Path $runnerPath)) { $failures.Add("$($record.check) runner is missing: $($record.runnerFile)"); continue }
    $runnerText = Get-Content $runnerPath -Raw
    if (-not $runnerText.Contains([string]$record.runnerToken)) {
        $failures.Add("$($record.check) is not invoked by $($record.runnerFile) (token '$($record.runnerToken)')")
    }
}

$declaredScripts = @(
    Get-ChildItem $PSScriptRoot -Filter '*.ps1' -File | ForEach-Object { "packaging/$($_.Name)" }
    Get-ChildItem (Join-Path $root 'msix') -Filter 'test-*.ps1' -File | ForEach-Object { "msix/$($_.Name)" }
)
foreach ($scriptPath in $declaredScripts) {
    if (-not $seen[$scriptPath]) { $failures.Add("unrecorded check script: $scriptPath") }
}

if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "check-placement: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "check-placement: PASS ($($records.Count) records)" -ForegroundColor Green
exit 0
