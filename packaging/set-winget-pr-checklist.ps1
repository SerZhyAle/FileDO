#Requires -Version 7.0
<#
.SYNOPSIS
  Ticks the checklist in the body of a winget-pkgs PR - item by item, only where there is evidence.

.DESCRIPTION
  `wingetcreate submit` opens the PR with the repository's template and every box empty. An empty box
  reads as "the author did not check", and the Description section stays blank. This fills both, and
  it does not tick on faith: each item is decided by something this script looked at itself.

    Signed the CLA                  the PR's license/cla status is SUCCESS
    Linked to an issue              ticked as not applicable (a release has no issue to close)
    No other open PR                no other open PR whose title names SerZhyAle.FileDO
    Only one (1) manifest           every changed file sits in one manifests\...\<version>\ folder
    winget validate                 `winget validate --manifest winget\` exits 0 right now
    winget install                  only with -InstallTested (the caller ran check-winget-manifests -Install)
    Schema 1.x                      every winget\*.yaml carries ManifestVersion 1.x.*, x as named in the item

  An item without evidence stays empty and is named in the output; the exit code is then 1. Running it
  again is safe: a ticked item stays ticked, and the Description is only added when it is blank.

.PARAMETER Pr
  The PR number in microsoft/winget-pkgs. Omitted, the open PR of the current gh user titled
  "SerZhyAle.FileDO version <Version>" is looked up (a few tries - a fresh PR can take a moment).

.PARAMETER Version
  The 10-digit stamp (the tag without the v). Needed without -Pr, and for the Description text.

.PARAMETER InstallTested
  The `winget install --manifest` run has just passed on this machine; ticks the install item.

.PARAMETER BodyFile
  Test aid: read the body from this file instead of the PR and print the result; nothing is sent.

.PARAMETER DryRun
  Print what would be ticked; do not edit the PR.

.EXAMPLE
  pwsh -NoProfile -File .\packaging\set-winget-pr-checklist.ps1 -Version 2610070412 -InstallTested
.EXAMPLE
  pwsh -NoProfile -File .\packaging\set-winget-pr-checklist.ps1 -Pr 448000 -Version 2610070412 -DryRun
#>
[CmdletBinding()]
param(
    [int]$Pr,
    [ValidatePattern('^\d{10}$')]
    [string]$Version,
    [switch]$InstallTested,
    [string]$BodyFile,
    [switch]$DryRun
)

$ErrorActionPreference = "Stop"
$upstream = "microsoft/winget-pkgs"
$package  = "SerZhyAle.FileDO"
$wingetDir = Join-Path (Split-Path $PSScriptRoot -Parent) "winget"

if (-not $Pr -and -not $BodyFile -and -not $Version) {
    Write-Host "winget-pr-checklist: COULD NOT VERIFY (give -Pr, or -Version to find the PR)" -ForegroundColor Yellow
    exit 2
}

# --- the PR and its body -------------------------------------------------------
if ($BodyFile) {
    $body = Get-Content -LiteralPath $BodyFile -Raw
} else {
    if (-not $Pr) {
        for ($try = 1; $try -le 6 -and -not $Pr; $try++) {
            $mine = gh pr list --repo $upstream --author "@me" --state open --limit 20 --json number,title | ConvertFrom-Json
            if ($LASTEXITCODE -ne 0) { Write-Host "winget-pr-checklist: COULD NOT VERIFY (gh pr list exit $LASTEXITCODE)" -ForegroundColor Yellow; exit 2 }
            $hit = @($mine | Where-Object { $_.title -match [regex]::Escape("$package version $Version") })
            if ($hit.Count) { $Pr = $hit[0].number } elseif ($try -lt 6) { Start-Sleep -Seconds 10 }
        }
        if (-not $Pr) { Write-Host "winget-pr-checklist: COULD NOT VERIFY (no open PR titled '$package version $Version')" -ForegroundColor Yellow; exit 2 }
    }
    $info = gh pr view $Pr --repo $upstream --json body,files,statusCheckRollup | ConvertFrom-Json
    if ($LASTEXITCODE -ne 0) { Write-Host "winget-pr-checklist: COULD NOT VERIFY (gh pr view $Pr exit $LASTEXITCODE)" -ForegroundColor Yellow; exit 2 }
    $body = $info.body
}

# --- evidence, one value per item: $null = proven, otherwise why not ----------
function Get-Evidence([string]$item) {
    switch -Regex ($item) {
        'Contributor License Agreement' {
            if ($BodyFile) { return "no PR to ask" }
            $cla = @($info.statusCheckRollup | Where-Object { ($_.name ?? $_.context) -eq 'license/cla' })
            if ($cla.Count -and ($cla[0].conclusion ?? $cla[0].state) -eq 'SUCCESS') { return $null }
            return "license/cla is not SUCCESS yet - run this again in a minute"
        }
        'Linked to an issue' { return $null }
        'other open' {
            if ($BodyFile) { return "no PR to ask" }
            $open = gh pr list --repo $upstream --state open --search "$package in:title" --json number | ConvertFrom-Json
            if ($LASTEXITCODE -ne 0) { return "gh pr list exit $LASTEXITCODE" }
            $others = @($open | Where-Object { $_.number -ne $Pr })
            if ($others.Count -eq 0) { return $null }
            return "other open PRs: " + (($others | ForEach-Object { "#$($_.number)" }) -join ', ')
        }
        'only modifies one' {
            if ($BodyFile) { return "no PR to ask" }
            $dirs = @($info.files | ForEach-Object { Split-Path $_.path -Parent } | Sort-Object -Unique)
            if ($dirs.Count -eq 1) { return $null }
            return "the PR touches $($dirs.Count) folders"
        }
        'winget validate' {
            $out = & winget validate --manifest $wingetDir 2>&1 | Out-String
            if ($LASTEXITCODE -eq 0) { return $null }
            return "winget validate exit ${LASTEXITCODE}: $($out.Trim())"
        }
        'winget install' {
            if ($InstallTested) { return $null }
            return "not run here - pass -InstallTested after check-winget-manifests.ps1 -Install, or tick it after your own install test"
        }
        'conforms to the \[(\d+\.\d+) schema' {
            $want = $Matches[1]
            $bad = @(Get-ChildItem $wingetDir -Filter *.yaml | Where-Object {
                (Get-Content $_.FullName -Raw) -notmatch "(?m)^ManifestVersion:\s*$([regex]::Escape($want))\.\d+\s*$" })
            if ($bad.Count -eq 0) { return $null }
            return "ManifestVersion is not $want.x in " + (($bad | ForEach-Object Name) -join ', ')
        }
        default { return "no rule for this item" }
    }
}

$unticked = @()
$lines = $body -split "(?<=\n)"
for ($i = 0; $i -lt $lines.Count; $i++) {
    if ($lines[$i] -notmatch '^(?<pre>- )\[(?<mark> |x|X)\](?<rest> .*)$') { continue }
    $item = $Matches['rest'].Trim()
    $why = Get-Evidence $item
    $short = ($item -replace '\[([^\]]+)\]\([^)]*\)', '$1' -replace '`', '').Trim()
    if ($why) {
        if ($Matches['mark'] -eq ' ') { $unticked += "$short ($why)" }
        Write-Host "  [$($Matches['mark'])] $short - $why" -ForegroundColor Yellow
        continue
    }
    $lines[$i] = $lines[$i] -replace '^- \[ \]', '- [x]'
    Write-Host "  [x] $short"
}
$new = -join $lines

# --- the Description: add one line only where the section is still blank -------
if ($Version) {
    $desc = "$package version $Version - portable zip with the five FileDO tools (filedo, filedo_check, filedo_fill, filedo_test, filedo_win). Manifest generated by ``wingetcreate``; the zip hash matches the GitHub release asset."
    $withDesc = $new -replace '(?s)(## [^\r\n]*Description[^\r\n]*\r?\n<!--[^\r\n]*-->\r?\n)(\s*\r?\n)(?=## )', ('$1' + "`n" + $desc + "`n`n")
    if ($withDesc -ne $new) { $new = $withDesc; Write-Host "  [x] Description filled" }
}

if ($new -ceq $body) {
    Write-Host "winget-pr-checklist: nothing to change" -ForegroundColor Green
} elseif ($BodyFile) {
    Write-Host ""; Write-Host $new
} elseif ($DryRun) {
    Write-Host "winget-pr-checklist: dry run - PR #$Pr not edited" -ForegroundColor Cyan
} else {
    $tmp = [IO.Path]::GetTempFileName()
    try {
        [IO.File]::WriteAllText($tmp, $new, [Text.UTF8Encoding]::new($false))
        gh pr edit $Pr --repo $upstream --body-file $tmp | Out-Host
        if ($LASTEXITCODE -ne 0) { Write-Host "winget-pr-checklist: FAIL (gh pr edit exit $LASTEXITCODE)" -ForegroundColor Red; exit 1 }
    } finally { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
    Write-Host "winget-pr-checklist: PR #$Pr updated" -ForegroundColor Green
}

if ($unticked.Count) {
    Write-Host "winget-pr-checklist: $($unticked.Count) item(s) left empty - $($unticked -join '; ')" -ForegroundColor Yellow
    exit 1
}
exit 0
