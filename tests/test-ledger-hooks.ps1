#requires -Version 7.0
<#
.SYNOPSIS
    Proves the release-ledger hooks of build.ps1, msix\build-msix.ps1 and release.ps1 on a scratch copy, in a few
    seconds, without a build and without touching docs\releases\ (RELEASE-LEDGER 0.1, SP-0229).

.DESCRIPTION
    The three hooks only call packaging\release-ledger.ps1 and must never fail the script that called them. This pulls
    the two hook functions (Write-BuildLedger, Write-Ledger) and the tag-drift check (Get-TagSourceDrift) out of the
    real scripts by their syntax tree, runs them against a scratch repository, and checks:

      A  the wrapper without a writer exits 2 and says how to point at one
      B  a build hook without a writer is one yellow line: no throw, the caller's exit code left alone
      C  with the writer: the installed sza plugin alone is enough; a test build and a release build are recorded
         (kind from FILEDO_BUILD_KIND; the installer is the artifact when one was built, else the GUI)
      D  a channel step is recorded; a bad channel key is one yellow line; a note with quotes survives as one value
      E  the tag-drift check leaves docs\releases\ out (a dirty ledger is not drift) and still catches a source change

    C and D need the writer (the sza plugin's releases\release-ledger.ps1, or SZA_HARNESS_ROOT). Without it they are
    skipped and the verdict is COULD NOT VERIFY. It does not run a real build or release: the first real build and
    release that write through the hooks are the proof of the wiring, this is the proof of the hooks.

    Exit codes: 0 passed, 1 a defect, 2 could not verify.
#>
$ErrorActionPreference = 'Stop'
$repo = Split-Path -Parent $PSScriptRoot
$scratch = Join-Path ([IO.Path]::GetTempPath()) ('filedo-ledger-hooks-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
$script:ran = 0; $script:skipped = 0; $script:fails = 0
$saved = @{ H = $env:SZA_HARNESS_ROOT; P = $env:CLAUDE_PLUGIN_ROOT; U = $env:USERPROFILE; K = $env:FILEDO_BUILD_KIND }

function Check([string]$name, [bool]$ok, [string]$detail = '') {
    $script:ran++
    if ($ok) { Write-Host "  PASS  $name" } else { $script:fails++; Write-Host "  FAIL  $name  $detail" -ForegroundColor Red }
}
function Skip([string]$name, [string]$why) { $script:skipped++; Write-Host "  SKIP  $name  ($why)" -ForegroundColor Yellow }
function Get-FunctionText([string]$file, [string]$name) {
    $tok = $null; $err = $null
    $ast = [System.Management.Automation.Language.Parser]::ParseFile($file, [ref]$tok, [ref]$err)
    $fn = $ast.Find({ param($n) $n -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -eq $name }, $true)
    if (-not $fn) { throw "function $name not found in $file" }
    $fn.Extent.Text
}
function Restore-Env {
    foreach ($pair in @(@('SZA_HARNESS_ROOT', 'H'), @('CLAUDE_PLUGIN_ROOT', 'P'), @('USERPROFILE', 'U'), @('FILEDO_BUILD_KIND', 'K'))) {
        if ($null -ne $saved[$pair[1]]) { Set-Item "Env:$($pair[0])" $saved[$pair[1]] } else { Remove-Item "Env:$($pair[0])" -ErrorAction SilentlyContinue }
    }
}

try {
    New-Item -ItemType Directory -Force "$scratch\packaging", "$scratch\docs\releases", "$scratch\dist", "$scratch\exe_to_download" | Out-Null
    Copy-Item "$repo\packaging\release-ledger.ps1" "$scratch\packaging\"
    Copy-Item "$repo\docs\releases\channels.json" "$scratch\docs\releases\"
    Set-Content "$scratch\exe_to_download\filedo_win.exe" 'x'
    . ([scriptblock]::Create((Get-FunctionText "$repo\build.ps1" 'Write-BuildLedger')))
    . ([scriptblock]::Create((Get-FunctionText "$repo\release.ps1" 'Write-Ledger')))
    . ([scriptblock]::Create((Get-FunctionText "$repo\release.ps1" 'Get-TagSourceDrift')))

    # A, B - a home with no plugin cache and no variable: no writer can be found
    Remove-Item Env:SZA_HARNESS_ROOT, Env:CLAUDE_PLUGIN_ROOT, Env:FILEDO_BUILD_KIND -ErrorAction SilentlyContinue
    $emptyHome = Join-Path $scratch 'empty_home'; New-Item -ItemType Directory -Force $emptyHome | Out-Null; $env:USERPROFILE = $emptyHome
    $out = @(& pwsh -NoProfile -File "$scratch\packaging\release-ledger.ps1" show 2>&1); $code = $LASTEXITCODE
    Check 'A1 the wrapper without a writer exits 2 and says why' ($code -eq 2 -and ($out -join ' ') -match 'not in the installed sza plugin') "code=$code out=$($out -join ' | ')"
    $root = $scratch; $version = '2611990001'
    $global:LASTEXITCODE = 7
    $o = Write-BuildLedger 6>&1 | Out-String
    Check 'B1 the build hook without a writer does not throw and reports it' ($o -match 'not recorded') $o
    Check 'B2 the build hook leaves the exit code it found' ($global:LASTEXITCODE -eq 7) "LASTEXITCODE=$global:LASTEXITCODE"

    # is there a writer at all (the plugin, or SZA_HARNESS_ROOT as the caller set it)?
    $env:USERPROFILE = $saved.U
    if ($null -ne $saved.H) { $env:SZA_HARNESS_ROOT = $saved.H }
    if ($null -ne $saved.P) { $env:CLAUDE_PLUGIN_ROOT = $saved.P }
    $probe = & pwsh -NoProfile -File "$scratch\packaging\release-ledger.ps1" show 2>&1 | Out-String
    $haveWriter = ($LASTEXITCODE -eq 0 -and $probe -match 'release ledger:')

    if ($haveWriter) {
        # C
        $global:LASTEXITCODE = 3
        $o = Write-BuildLedger 6>&1 | Out-String
        $b = Get-Content "$scratch\docs\releases\builds.json" -Raw | ConvertFrom-Json
        Check 'C1 a test build is recorded as package filedo' ($o -match 'test build 2611990001 recorded' -and @($b.builds).Count -eq 1 -and $b.builds[0].kind -eq 'test' -and $b.builds[0].package -eq 'filedo') $o
        Check 'C2 the artifact is the GUI exe when no installer was built' ($b.builds[0].artifact -eq 'exe_to_download/filedo_win.exe' -and [bool]$b.builds[0].sha256) ($b.builds[0] | ConvertTo-Json -Compress)
        Check 'C3 the exit code is left alone after a recorded build' ($global:LASTEXITCODE -eq 3) "LASTEXITCODE=$global:LASTEXITCODE"
        Set-Content "$scratch\dist\FileDO-2611990002-setup.exe" 'setup'
        $version = '2611990002'; $env:FILEDO_BUILD_KIND = 'release'
        $o = Write-BuildLedger 6>&1 | Out-String; Remove-Item Env:FILEDO_BUILD_KIND
        $b = Get-Content "$scratch\docs\releases\builds.json" -Raw | ConvertFrom-Json
        $rel = @($b.builds | Where-Object kind -eq 'release')
        Check 'C4 a release build sits beside the test build, the installer is its artifact' ($rel.Count -eq 1 -and $rel[0].artifact -eq 'dist/FileDO-2611990002-setup.exe' -and @($b.builds).Count -eq 2) ($b.builds | ConvertTo-Json -Compress)
        # D
        $global:LASTEXITCODE = 5
        $o = Write-Ledger @('publish', '-Key', 'github', '-Version', '2611990002', '-State', 'live', '-Ref', 'v2611990002', '-Url', 'https://example.invalid/r') 6>&1 | Out-String
        Check 'D1 a channel step is recorded' ($o -match 'github filedo 2611990002 live' -and @(Get-Content "$scratch\docs\releases\history.jsonl").Count -eq 1) $o
        Check 'D2 the exit code survives a recorded event' ($global:LASTEXITCODE -eq 5) "LASTEXITCODE=$global:LASTEXITCODE"
        $o = Write-Ledger @('publish', '-Key', 'no-such-key', '-Version', '1', '-State', 'live') 6>&1 | Out-String
        Check 'D3 a bad channel key is one yellow line, not a throw' ($o -match 'not recorded' -and $global:LASTEXITCODE -eq 5) "$o LASTEXITCODE=$global:LASTEXITCODE"
        $o = Write-Ledger @('publish', '-Key', 'winget', '-Version', '2611990002', '-State', 'pending', '-Note', "not submitted: it said 'no' and left") 6>&1 | Out-String
        Check 'D4 a note with quotes and spaces survives as one value' ($o -match 'winget filedo 2611990002 pending' -and ((Get-Content "$scratch\docs\releases\history.jsonl" | Select-Object -Last 1) -match "it said 'no' and left")) $o
    } else {
        foreach ($n in 'C1', 'C2', 'C3', 'C4', 'D1', 'D2', 'D3', 'D4') { Skip $n 'no writer: the sza plugin lacks releases\release-ledger.ps1 and SZA_HARNESS_ROOT is not set' }
    }

    # E - the tag-drift check on a throwaway git repository
    $gitRepo = Join-Path $scratch 'git'
    New-Item -ItemType Directory -Force "$gitRepo\docs\releases", "$gitRepo\winget" | Out-Null
    Push-Location $gitRepo
    try {
        git init -q . 2>&1 | Out-Null; git config user.email t@example.invalid; git config user.name t
        Set-Content src.txt 'a'; Set-Content docs\releases\history.jsonl '{}'; Set-Content winget\x.yaml 'v1'
        git add -A; git commit -q -m one; git tag v1
        Add-Content docs\releases\history.jsonl '{"more":1}'; Add-Content winget\x.yaml 'v2'; git add -A; git commit -q -m two
        Add-Content docs\releases\history.jsonl '{"dirty":1}'
        $drift = Get-TagSourceDrift 'v1'
        Check 'E1 ledger and winget changes after the tag, and a dirty ledger, are not drift' ($null -eq $drift) "$drift"
        Add-Content src.txt 'b'
        $drift = Get-TagSourceDrift 'v1'
        Check 'E2 a changed source is still drift' ($null -ne $drift) 'no drift reported for a changed source'
    } finally { Pop-Location }
}
catch {
    $script:fails++
    Write-Host "ledger-hooks: FAIL (unexpected error at line $($_.InvocationInfo.ScriptLineNumber): $($_.Exception.Message))" -ForegroundColor Red
}
finally {
    Restore-Env
    Set-Location $repo
    Remove-Item $scratch -Recurse -Force -ErrorAction SilentlyContinue
}

$counts = "run=$script:ran skipped=$script:skipped"
if ($script:fails -gt 0) { Write-Host "ledger-hooks: FAIL ($counts, $script:fails failed)" -ForegroundColor Red; exit 1 }
if ($script:skipped -gt 0) { Write-Host "ledger-hooks: COULD NOT VERIFY ($counts, the writer is not installed)" -ForegroundColor Yellow; exit 2 }
Write-Host "ledger-hooks: PASS ($counts)" -ForegroundColor Green
exit 0
