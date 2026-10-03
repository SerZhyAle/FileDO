<#
.SYNOPSIS
  Live proof of the stale-row cleanup and the keep-alive watcher (`vd mount <file> keep`).

.DESCRIPTION
  Run from an ELEVATED PowerShell. It builds filedo.exe (amd64) from this tree into the run folder
  and uses that copy - the installed one is not touched.

  Part A (only if the registered container -RealName has a mount row whose block server is gone):
    `vd mount <name>` must clear the stale row and mount the disk (exit 0, the letter exists).
    The disk is left mounted.
  Part B (a scratch 64 MB container in the run folder, never a real disk):
    1. `mount keep` starts a resident watcher and the disk comes up
    2. the block server is killed; the watcher must bring the disk back with a new server
    3. the watcher is still running afterwards
    4. a second watcher for the same container exits at once
    5. `unmount` ends the watcher, and nothing remounts the disk

  Evidence (log, watcher output, built exe) goes to temp\evidence\<run-id>\ (git-ignored, private).
  Exit code: 0 all steps passed, 1 a step failed, 2 could not run (not elevated, build failed).
#>
param(
  [string]$RealName = 'fddtest',
  [switch]$SkipReal
)

$ErrorActionPreference = 'Continue'
$root = Split-Path $PSScriptRoot -Parent
$run = 'keep-' + (Get-Date -Format 'yyyyMMdd-HHmmss')
$out = Join-Path $root "temp\evidence\$run"
New-Item -ItemType Directory -Force $out | Out-Null
$log = Join-Path $out 'proof.log'
$exe = Join-Path $out 'filedo.exe'
$scratch = Join-Path $out 'scratch.fdd'
$stateDir = Join-Path $env:LOCALAPPDATA 'FileDO\state'
$results = [System.Collections.Generic.List[object]]::new()

function Say([string]$m) { $l = '{0:HH:mm:ss} {1}' -f (Get-Date), $m; Write-Host $l; Add-Content $log $l }
function Step([string]$name, [bool]$ok, [string]$detail = '') {
  $results.Add([pscustomobject]@{ Step = $name; Pass = $ok; Detail = $detail })
  Say ('{0}: {1} {2}' -f ($(if ($ok) { 'PASS' } else { 'FAIL' })), $name, $detail)
}
function Filedo([string[]]$a) {
  Say ("filedo " + ($a -join ' '))
  $o = & $exe --no-history @a 2>&1 | Out-String
  $code = $LASTEXITCODE
  Add-Content $log $o
  [pscustomobject]@{ Code = $code; Text = $o }
}
function Rows { try { (Get-Content (Join-Path $stateDir 'vdisk-state.json') -Raw | ConvertFrom-Json).mounts } catch { @() } }
function RowOf([string]$path) { Rows | Where-Object { $_.path -ieq $path } | Select-Object -First 1 }
function ServerUp($row) { if (-not $row) { return $false }; $p = Get-Process -Id $row.server_pid -ErrorAction SilentlyContinue; [bool]$p }
function WaitFor([scriptblock]$cond, [int]$sec) {
  $end = (Get-Date).AddSeconds($sec)
  while ((Get-Date) -lt $end) { if (& $cond) { return $true }; Start-Sleep -Milliseconds 500 }
  return [bool](& $cond)
}

$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) { Write-Host 'Run this from an elevated PowerShell.'; exit 2 }

Say "run $run, evidence in $out"
Push-Location $root
try {
  $oldArch = $env:GOARCH
  $env:GOARCH = 'amd64'
  & go build -o $exe ./cmd/filedo 2>&1 | Tee-Object -Append $log
  $env:GOARCH = $oldArch
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path $exe)) { Say 'build failed'; exit 2 }
} finally { Pop-Location }
Say "built $exe"

$watcher = $null
try {
  # ---------------------------------------------------------------- Part A
  if (-not $SkipReal) {
    $reg = try { (Get-Content (Join-Path $stateDir 'vd-registry.json') -Raw | ConvertFrom-Json).containers | Where-Object { $_.name -ieq $RealName } } catch { $null }
    $row = if ($reg) { RowOf $reg.path } else { $null }
    if ($reg -and $row -and -not (ServerUp $row)) {
      Say "A: $RealName has a stale row (server pid $($row.server_pid) is gone), letter $($row.letter)"
      $r = Filedo @('vd', 'mount', $RealName)
      $now = RowOf $reg.path
      Step 'A1 mount clears the stale row and mounts' ($r.Code -eq 0 -and (ServerUp $now) -and (Test-Path ($now.letter + '\'))) "exit $($r.Code), letter $($now.letter)"
    } else {
      Say "A: skipped - $RealName has no stale mount row now"
    }
  }

  # ---------------------------------------------------------------- Part B
  $r = Filedo @('vd', 'new', $scratch, '64M')
  if ($r.Code -ne 0) { Step 'B0 create the scratch container' $false $r.Text; throw 'no scratch' }

  $watcher = Start-Process -FilePath $exe -ArgumentList @('--no-history', 'vd', 'mount', $scratch, 'keep') -PassThru -WindowStyle Hidden `
    -RedirectStandardOutput (Join-Path $out 'watcher.out.txt') -RedirectStandardError (Join-Path $out 'watcher.err.txt')
  $up = WaitFor { $x = RowOf $scratch; (ServerUp $x) -and (Test-Path ($x.letter + '\')) } 120
  $first = RowOf $scratch
  Step 'B1 keep mounts the disk' $up "server pid $($first.server_pid), letter $($first.letter)"
  if (-not $up) { throw 'not mounted' }

  Stop-Process -Id $first.server_pid -Force
  Say "killed the block server pid $($first.server_pid)"
  $back = WaitFor { $x = RowOf $scratch; $x -and $x.server_pid -ne $first.server_pid -and (ServerUp $x) -and (Test-Path ($x.letter + '\')) } 180
  $second = RowOf $scratch
  Step 'B2 the disk comes back with a new server' $back "new pid $($second.server_pid), letter $($second.letter)"

  $watcher.Refresh()
  Step 'B3 the watcher is still running' (-not $watcher.HasExited)

  $w2 = Start-Process -FilePath $exe -ArgumentList @('--no-history', 'vd', 'mount', $scratch, 'keep') -PassThru -WindowStyle Hidden
  $w2Gone = WaitFor { $w2.Refresh(); $w2.HasExited } 30
  Step 'B4 a second watcher exits at once' $w2Gone
  if (-not $w2Gone) { Stop-Process -Id $w2.Id -Force -ErrorAction SilentlyContinue }

  $r = Filedo @('vd', 'unmount', $scratch)
  $ended = WaitFor { $watcher.Refresh(); $watcher.HasExited } 60
  Start-Sleep -Seconds 15
  $stayed = -not (RowOf $scratch)
  Step 'B5 unmount ends the watcher and nothing remounts' ($r.Code -eq 0 -and $ended -and $stayed) "unmount exit $($r.Code), watcher ended=$ended, no row=$stayed"
}
catch { Say "stopped: $_" }
finally {
  if ($watcher -and -not $watcher.HasExited) { Stop-Process -Id $watcher.Id -Force -ErrorAction SilentlyContinue }
  if (RowOf $scratch) { Filedo @('vd', 'unmount', $scratch, 'force') | Out-Null }
  Remove-Item $scratch -Force -ErrorAction SilentlyContinue
}

$failed = @($results | Where-Object { -not $_.Pass }).Count
Say ('keep-proof {0}: {1} steps, {2} failed' -f $(if ($failed -eq 0 -and $results.Count -gt 0) { 'PASS' } else { 'FAIL' }), $results.Count, $failed)
Say "evidence: $out"
if ($failed -eq 0 -and $results.Count -gt 0) { exit 0 } else { exit 1 }
