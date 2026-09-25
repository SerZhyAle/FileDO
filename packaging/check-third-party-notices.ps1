#Requires -Version 7.0
<#
.SYNOPSIS
  Holds THIRD-PARTY-NOTICES.txt to the Go modules the shipped executables link.

.DESCRIPTION
  Every FileDO executable statically links the Go modules its main package imports, and
  every package - zip, MSI, setup EXE, MSIX - carries THIRD-PARTY-NOTICES.txt with the
  license of each one. That file is written by hand, so a dependency that is added or bumped
  without a matching section ships without its license, and nothing else would notice
  (SP-0030 REL-18).

  For each of the four Go modules that produce a shipped executable:
    * `go list -m all` must succeed. A module whose graph does not resolve cannot be
      audited, verified or scanned for vulnerabilities either (REL-13 was one).
    * `go list -deps` of the main package, as windows/amd64 with cgo off - the release build
      shape - names the modules that are compiled in. That is the part of `go list -m all`
      that ships: the graph also holds modules that only a dependency's tests or tools need
      (golang.org/x/tools, x/mod, ..), which are never linked and carry no notice.

  Then both directions are checked:
    * every linked module@version has a section header in the notices;
    * every Go module header in the notices is linked by at least one shipped executable, so
      a section never claims a dependency or a version that no longer ships.

  A section header names the module path, the version or versions it covers, and the
  license in parentheses:
      golang.org/x/sys v0.34.0, v0.15.0  (BSD-3-Clause)
  Headers that are not Go modules (the icon drawings) are left alone.

  Exit code: 0 = pass, 1 = the notices and the build disagree, 2 = could not verify.
#>
[CmdletBinding()]
param(
    [string]$Notices
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent
if (-not $Notices) { $Notices = Join-Path $root 'THIRD-PARTY-NOTICES.txt' }

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "third-party-notices: NOT VERIFIED ('go' is not on PATH)" -ForegroundColor Yellow
    exit 2
}
if (-not (Test-Path $Notices)) {
    Write-Host "third-party-notices: FAIL ($Notices is missing - every package must carry it)" -ForegroundColor Red
    exit 1
}

# The shipped executables and the module each one is built from.
$shipped = @(
    @{ Exe = 'filedo.exe';       Dir = $root;                                   Pkg = './cmd/filedo' },
    @{ Exe = 'filedo_check.exe'; Dir = (Join-Path $root 'cmd\filedo-check');    Pkg = '.' },
    @{ Exe = 'filedo_fill.exe';  Dir = (Join-Path $root 'cmd\filedo-fill');     Pkg = '.' },
    @{ Exe = 'filedo_test.exe';  Dir = (Join-Path $root 'cmd\filedo-test');     Pkg = '.' }
)

# Errors that are a defect of the repository rather than of this machine.
$repoDefect = 'missing go\.sum entry|updates to go\.mod needed|no required module provides|errors parsing go\.mod|inconsistent vendoring|malformed module path'

$failures = [System.Collections.Generic.List[string]]::new()
$unverified = [System.Collections.Generic.List[string]]::new()
# "path version" -> the executables that link it
$linked = @{}

$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED; GOFLAGS = $env:GOFLAGS }
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
# Read-only: a check never rewrites go.mod or go.sum to make itself pass.
$env:GOFLAGS = '-mod=readonly'
try {
    foreach ($s in $shipped) {
        Push-Location $s.Dir
        try {
            $graph = & go list -m all 2>&1 | Out-String
            if ($LASTEXITCODE -ne 0) {
                $msg = "$($s.Exe): go list -m all failed in $($s.Dir): $($graph.Trim())"
                if ($graph -match $repoDefect) { $failures.Add($msg) } else { $unverified.Add($msg) }
                continue
            }
            $fmt = '{{with .Module}}{{if not .Main}}{{with .Replace}}{{.Path}} {{.Version}}{{else}}{{.Path}} {{.Version}}{{end}}{{end}}{{end}}'
            $deps = & go list -deps -f $fmt $s.Pkg 2>&1 | Out-String
            if ($LASTEXITCODE -ne 0) {
                $msg = "$($s.Exe): go list -deps $($s.Pkg) failed in $($s.Dir): $($deps.Trim())"
                if ($deps -match $repoDefect) { $failures.Add($msg) } else { $unverified.Add($msg) }
                continue
            }
            foreach ($line in ($deps -split "`r?`n" | Where-Object { $_.Trim() } | Sort-Object -Unique)) {
                $key = $line.Trim()
                if (-not $linked.ContainsKey($key)) { $linked[$key] = [System.Collections.Generic.List[string]]::new() }
                $linked[$key].Add($s.Exe)
            }
        } finally {
            Pop-Location
        }
    }
} finally {
    foreach ($k in @($saved.Keys)) { [Environment]::SetEnvironmentVariable($k, $saved[$k], 'Process') }
}

# Section headers: the line between two rules of '=' signs.
$lines = @(Get-Content $Notices)
$noticed = @{}
for ($i = 1; $i -lt $lines.Count - 1; $i++) {
    if ($lines[$i - 1] -notmatch '^={20,}\s*$' -or $lines[$i + 1] -notmatch '^={20,}\s*$') { continue }
    $header = $lines[$i].Trim()
    # A Go module path starts with a host name (a dot before the first slash).
    if ($header -notmatch '^(?<path>[A-Za-z0-9\-]+(?:\.[A-Za-z0-9\-]+)+(?:/\S+)+)\s+(?<vers>v\S+(?:\s*,\s*v\S+)*)\s+\((?<lic>[^)]+)\)$') { continue }
    foreach ($v in ($Matches['vers'] -split '\s*,\s*')) {
        $noticed["$($Matches['path']) $v"] = $true
    }
}

foreach ($key in ($linked.Keys | Sort-Object)) {
    if (-not $noticed.ContainsKey($key)) {
        $failures.Add("$key is linked into $(($linked[$key] | Sort-Object -Unique) -join ', ') but has no section in THIRD-PARTY-NOTICES.txt")
    }
}
# A stale section is only provable when every module could be listed.
if ($unverified.Count -eq 0) {
    foreach ($key in ($noticed.Keys | Sort-Object)) {
        if (-not $linked.ContainsKey($key)) {
            $failures.Add("THIRD-PARTY-NOTICES.txt has a section for $key, which no shipped executable links")
        }
    }
}

foreach ($key in ($linked.Keys | Sort-Object)) {
    Write-Host "  linked  $key  ($(($linked[$key] | Sort-Object -Unique) -join ', '))"
}
if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "third-party-notices: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
if ($unverified.Count) {
    $unverified | ForEach-Object { Write-Host "  NOT VERIFIED  $_" -ForegroundColor Yellow }
    Write-Host "third-party-notices: NOT VERIFIED ($($unverified.Count) modules could not be listed)" -ForegroundColor Yellow
    exit 2
}
Write-Host "third-party-notices: PASS ($($linked.Count) linked modules, all noticed)" -ForegroundColor Green
exit 0
