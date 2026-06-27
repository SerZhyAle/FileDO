# ============================================================================
#  FileDO - "SBORKA" (BUILD): the local, zero-CI flow.
#
#  Build every distributable executable into exe_to_download\, optionally test
#  locally, optionally commit. This script is the BUILD half of the project's
#  two-flow model (see RELEASE.md):
#
#    BUILD  (this script) -> compile, test locally, commit. Never touches CI.
#    RELEASE (release.ps1) -> the ONLY thing that tags v* and triggers GitHub.
#
#  HARD RULE: build.ps1 must never create or push a `v*` tag. Tagging is what
#  starts the public GitHub release, so it lives exclusively in release.ps1.
#  That separation is what lets you "just build" without spending a CI run or
#  publishing anything.
#
#  Variants produced (all of them, every run):
#    1. filedo.exe        - main all-in-one CLI (version stamped from build time)
#    2. filedo_fill.exe   - standalone "fill" tool   (cmd\filedo-fill)
#    3. filedo_check.exe  - standalone "check" tool  (cmd\filedo-check)
#    4. filedo_test.exe   - standalone "test" tool   (cmd\filedo-test)
#    5. filedo_win.exe    - Windows GUI front-end    (filedo_win_vb, VB.NET)
#
#  Requirements: Go toolchain on PATH (Go variants) and MSBuild / VS Build
#  Tools (the Windows GUI).
#
#  Usage (from the repo root):
#    .\build.ps1                       # build everything (default, unchanged)
#    .\build.ps1 -Test                 # build, then smoke-test + go test
#    .\build.ps1 -Test -Commit "msg"   # build + test, commit only if both pass
#    .\build.ps1 -SkipGui             # Go variants only (no MSBuild needed)
# ============================================================================
[CmdletBinding()]
param(
    # Run the local test gate after a successful build (smoke-run + go test).
    [switch]$Test,
    # Commit message. When set, stages everything and commits ONLY if the build
    # (and the test gate, which is forced on) succeeded. Never tags, never pushes.
    [string]$Commit,
    # Skip the VB.NET GUI build (handy when MSBuild/VS Build Tools are absent).
    [switch]$SkipGui
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

# A commit implies the test gate: we never commit an untested build.
if ($Commit) { $Test = $true }

# Version = current build date/time in yyMMddHHmm format
$version = Get-Date -Format "yyMMddHHmm"

Write-Host "Version: $version"
Write-Host ""

$out = "$root\exe_to_download"

$builds = @(
    @{ Name = "filedo";       Dir = "$root\cmd\filedo";       Out = "$out\filedo.exe";       Flags = "-ldflags=-X 'main.version=$version'" },
    @{ Name = "filedo_fill";  Dir = "$root\cmd\filedo-fill";  Out = "$out\filedo_fill.exe";  Flags = "" },
    @{ Name = "filedo_check"; Dir = "$root\cmd\filedo-check"; Out = "$out\filedo_check.exe"; Flags = "" },
    @{ Name = "filedo_test";  Dir = "$root\cmd\filedo-test";  Out = "$out\filedo_test.exe";  Flags = "" }
)

$failed = @()

# ---- Go variants -----------------------------------------------------------
foreach ($b in $builds) {
    $name = $b.Name
    Write-Host "Building $name..." -NoNewline

    Push-Location $b.Dir
    try {
        if ($b.Flags) {
            go build $b.Flags -o $b.Out . 2>&1 | Out-Null
        } else {
            go build -o $b.Out . 2>&1 | Out-Null
        }
        if ($LASTEXITCODE -ne 0) { throw "exit code $LASTEXITCODE" }
        Write-Host " OK"
    } catch {
        Write-Host " FAILED: $_"
        $failed += $name
    } finally {
        Pop-Location
    }
}

# ---- Windows GUI variant (filedo_win.exe) ----------------------------------
# Built every run as a first-class variant. Requires MSBuild (VS Build Tools).
if ($SkipGui) {
    Write-Host "Building filedo_win (GUI)... SKIPPED (-SkipGui)"
} else {
    Write-Host "Building filedo_win (GUI)..." -NoNewline
    $vsw = "${env:ProgramFiles(x86)}\Microsoft Visual Studio\Installer\vswhere.exe"
    $msbuild = $null
    if (Test-Path $vsw) {
        $msbuild = & $vsw -latest -products * -requires Microsoft.Component.MSBuild -find "MSBuild\**\Bin\MSBuild.exe" | Select-Object -First 1
    }
    if ($msbuild -and (Test-Path $msbuild)) {
        & $msbuild "$root\filedo_win_vb\FileDOGUI.vbproj" /t:Rebuild /p:Configuration=Release /p:Platform=AnyCPU /v:quiet /nologo | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host " FAILED: MSBuild exit code $LASTEXITCODE"
            $failed += "filedo_win"
        } else {
            Copy-Item "$root\filedo_win_vb\bin\Release\filedo_win.exe" "$out\filedo_win.exe" -Force
            Write-Host " OK"
        }
    } else {
        Write-Host " FAILED: MSBuild not found (install VS Build Tools to build the GUI)"
        $failed += "filedo_win"
    }
}

Write-Host ""
if ($failed.Count -eq 0) {
    Write-Host "All builds successful."
} else {
    Write-Host "Failed: $($failed -join ', ')"
    exit 1
}

# ---- Optional local deploy --------------------------------------------------
$deploy = "C:\GD\tc\SZA\_APP"
if (Test-Path $deploy) {
    Write-Host ""
    Write-Host "Copying to $deploy ..." -NoNewline
    Copy-Item "$out\*" $deploy -Force
    Write-Host " OK"
} else {
    Write-Host ""
    Write-Host "Deploy folder $deploy not found - skipping copy."
}

# ---- Local test gate (-Test / implied by -Commit) ---------------------------
# Cheap, deterministic checks that the freshly built artifacts actually run.
# This is the "test locally" step of the BUILD flow - it does NOT touch CI.
if ($Test) {
    Write-Host ""
    Write-Host "== Test gate =="

    # 1) Smoke-run filedo.exe and assert it prints the version we just stamped.
    Write-Host "Smoke: filedo.exe -? ..." -NoNewline
    $smoke = & "$out\filedo.exe" "-?" 2>&1 | Out-String
    if ($smoke -match [regex]::Escape($version)) {
        Write-Host " OK (version $version)"
    } else {
        Write-Host " FAILED: version $version not found in output"
        exit 1
    }

    # 2) Compile-check the test module (root `go test ./...` is known-broken per
    #    AGENTS.md, so we only run the module that is expected to pass).
    Write-Host "go test ./cmd/filedo-test ..." -NoNewline
    Push-Location "$root\cmd\filedo-test"
    try {
        go test ./... 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { Write-Host " FAILED"; exit 1 }
        Write-Host " OK"
    } finally {
        Pop-Location
    }

    Write-Host "Test gate passed."
}

# ---- Optional commit (-Commit "msg") ----------------------------------------
# Reached only if every build succeeded AND the test gate passed (the early
# `exit 1`s above guarantee that). Stages and commits; never tags, never pushes.
if ($Commit) {
    Write-Host ""
    Write-Host "== Commit =="
    Push-Location $root
    try {
        if ((git symbolic-ref -q HEAD) -eq $null) {
            Write-Host "Refusing to commit: detached HEAD."; exit 1
        }
        git add -A
        $staged = git diff --cached --name-only
        if (-not $staged) {
            Write-Host "Nothing to commit - working tree clean."
        } else {
            git commit -m $Commit
            if ($LASTEXITCODE -ne 0) { Write-Host "Commit FAILED."; exit 1 }
            Write-Host "Committed: $Commit"
            Write-Host "(Not pushed, not tagged. Use release.ps1 to publish.)"
        }
    } finally {
        Pop-Location
    }
}
