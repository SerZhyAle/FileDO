#Requires -Version 7.0
<#
.SYNOPSIS
  Fails when a build input is on disk but git-ignored, so a clean checkout would lack it.

.DESCRIPTION
  A build that passes on this machine proves nothing about the tagged CI run when one of its
  inputs exists here only because git ignores it: `git add -A` and the clean-tree check both
  skip an ignored file without a word, and the checkout the workflow builds from does not have
  it (SP-0049 AUD-18-F1: `*secret*` ignored two catalog glyphs that `wix build` and the GUI
  project need).

  The check lists the files that are on disk, untracked and ignored
  (`git ls-files --others --ignored --exclude-standard`) and reports every one that is a build
  input:
    * anything under assets\glyphs\ or assets\menu-icons\, and the files directly in assets\;
    * the files directly in packaging\wix\, shellext\ and msix\, and under msix\listing\,
      msix\screenshots\ and msix\testdata\;
    * a source or project file of any language (.go .vb .vbproj .wxs .wxl .cpp .h .rc .def
      .manifest .resx .ps1 .yml) outside the build-output folders (bin, obj, dist, out, stage)
      and outside PLAN\ and .claude\.
  A tracked file is never reported, even when a rule would ignore it: it is in the checkout.

  Exit code: 0 = no build input is ignored, 1 = at least one is, 2 = could not verify.
#>
[CmdletBinding()]
param(
    # The repository to check; the fixture seam of the tests.
    [string]$Root
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
if (-not $Root) { $Root = Split-Path $PSScriptRoot -Parent }

if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    Write-Host "build-inputs: NOT VERIFIED ('git' is not on PATH)" -ForegroundColor Yellow
    exit 2
}

$ignored = @(& git -C $Root -c core.quotepath=off ls-files --others --ignored --exclude-standard 2>&1)
if ($LASTEXITCODE -ne 0) {
    Write-Host "build-inputs: NOT VERIFIED (git ls-files failed in ${Root}: $(($ignored | Out-String).Trim()))" -ForegroundColor Yellow
    exit 2
}

$folderInput = '^(assets/(glyphs|menu-icons)/|assets/[^/]+$|packaging/wix/[^/]+$|shellext/[^/]+$|msix/[^/]+$|msix/(listing|screenshots|testdata)/)'
$sourceInput = '\.(go|vb|vbproj|wxs|wxl|cpp|h|rc|def|manifest|resx|ps1|yml)$'
$outputFolder = '(^|/)(bin|obj|dist|out|stage)/'
$notInput = '^(PLAN|\.claude)/'

$defects = [System.Collections.Generic.List[string]]::new()
foreach ($path in $ignored) {
    $p = ([string]$path).Trim() -replace '\\', '/'
    if (-not $p -or $p -match $notInput -or $p -match $outputFolder) { continue }
    if ($p -match $folderInput -or $p -match $sourceInput) { $defects.Add($p) }
}

if ($defects.Count) {
    foreach ($d in $defects) {
        $why = (& git -C $Root check-ignore -v --no-index -- $d 2>$null | Select-Object -First 1)
        Write-Host "  FAIL  $d is a build input on disk but git-ignored$(if ($why) { " ($why)" })" -ForegroundColor Red
    }
    Write-Host "build-inputs: FAIL ($($defects.Count) ignored input(s) - a clean checkout would not have them; re-include them in .gitignore and add them)" -ForegroundColor Red
    exit 1
}
Write-Host "build-inputs: PASS (no build input is git-ignored)" -ForegroundColor Green
exit 0
