#Requires -Version 7.0
<#
.SYNOPSIS
  Fails when a Go package that carries tests is run by no release gate and is not on a reasoned exemption list.

.DESCRIPTION
  build.ps1 and the release workflow name their test packages one by one, so a package added later is silently
  ungated: fmsworker was linked into filedo.exe and had forty tests that ran nowhere (AUD-92-F1). This check lists
  the root module's packages that have _test.go files (go list) and requires each one to be either

    - gated: build.ps1 AND .github\workflows\release.yml both run `go test ./<package>/`, or
    - exempt: named below with the reason it is not a per-build gate.

  A package that is neither fails the gate, and so does a gated package one of the two runners does not call.

  Exit code: 0 = pass, 1 = an ungated package, 2 = could not verify.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent

# Every package build.ps1 and the release workflow run.
$gated = @('cmd/filedo', 'fdsec', 'vdisk', 'fmsworker')

# Not per-build gates, each with its reason. A package leaves this list by being gated, never by being
# deleted from it.
$exempt = [ordered]@{
    'fileduplicates' = 'duplicate-finder library; its tests are run by hand (research/01 records it as ungated)'
    'helpers'        = 'shared helper library; its tests are run by hand (research/01 records it as ungated)'
    'fsx'            = 'path-identity library; its tests are run by hand (research/01 records it as ungated)'
    'statedir'       = 'runtime-state library; exercised through cmd/filedo, whose gated tests use it on every run'
    'tests'          = 'the SP-0121 joint regression: it skips without an installed FMS for Windows worker and an elevated mounting session, so a per-build run would pass vacuously; it is the release-time proof of SP-0121 S4'
}

Push-Location $root
try {
    $env:GOARCH = 'amd64'
    $listed = go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "check-gated-packages: COULD NOT VERIFY (go list failed: $($listed -join ' '))" -ForegroundColor Yellow
        exit 2
    }
} finally {
    Pop-Location
}

# Packages under the git-ignored temp\ folder are working material, not part of the product.
$packages = @($listed | Where-Object { $_ -and $_ -notmatch '^filedo/temp/' } | ForEach-Object { $_ -replace '^filedo/', '' })
if (-not $packages.Count) {
    Write-Host 'check-gated-packages: COULD NOT VERIFY (go list found no package with tests)' -ForegroundColor Yellow
    exit 2
}

$build = Get-Content (Join-Path $root 'build.ps1') -Raw
$workflowPath = Join-Path $root '.github\workflows\release.yml'
if (-not (Test-Path $workflowPath)) {
    Write-Host 'check-gated-packages: COULD NOT VERIFY (the release workflow is missing)' -ForegroundColor Yellow
    exit 2
}
$workflow = Get-Content $workflowPath -Raw

$failures = [System.Collections.Generic.List[string]]::new()
foreach ($p in $packages) {
    if ($p -in $gated) { continue }
    if ($exempt.Contains($p)) { continue }
    $failures.Add("package $p has tests that no gate runs: gate it in build.ps1 and release.yml, or exempt it here with a reason")
}
foreach ($p in $gated) {
    $needle = "go test ./$p/"
    if (-not $build.Contains($needle)) { $failures.Add("$p is gated here but build.ps1 does not run '$needle'") }
    if (-not $workflow.Contains($needle)) { $failures.Add("$p is gated here but release.yml does not run '$needle'") }
}
foreach ($p in $exempt.Keys) {
    if ($p -in $gated) { $failures.Add("$p is both gated and exempt") }
    if ($p -notin $packages) { $failures.Add("exempt package $p has no tests any more: remove it from the exemption list") }
}

if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "check-gated-packages: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "check-gated-packages: PASS ($($gated.Count) gated, $($exempt.Count) exempt, $($packages.Count) with tests)" -ForegroundColor Green
exit 0
