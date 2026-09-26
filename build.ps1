#Requires -Version 7.0
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
#  Requirements: Go toolchain on PATH - exactly the version go.mod's `toolchain`
#  line pins - plus goversioninfo v1.4.1 (Go variants), and MSBuild / VS Build
#  Tools (the Windows GUI). The host must be able to run amd64 executables.
#
#  The Go executables are built exactly as .github/workflows/release.yml builds
#  them (SP-0030 REL-04, CI-03): windows/amd64, CGO off, the pinned toolchain,
#  goversioninfo -64 (PE version + app.manifest), -trimpath. The gate therefore
#  tests the binary that ships, not a 386 build of the same source, and the go
#  test runs of the gate are amd64 as well.
#
#  A plain `.\build.ps1` produces EVERYTHING that is distributable - the five
#  executables AND both installer artifacts. No switch is needed for the normal
#  case; the switches below only subtract work or add the test gate.
#
#  Usage (from the repo root):
#    .\build.ps1                       # exes + installer (the normal case)
#    .\build.ps1 -Test                 # ..and run the smoke-test + go test gate
#    .\build.ps1 -Test -Commit "msg"   # ..and commit, only if both pass
#    .\build.ps1 -SkipGui              # Go variants only (no MSBuild needed)
#    .\build.ps1 -SkipInstaller        # no dist\ artifacts (fast inner loop)
#    .\build.ps1 -Install              # build, then RUN the installer here
#    .\build.ps1 -DeployTo C:\Tools    # ..and copy the exes there once all passed
#
#  Deploying is opt-in: -DeployTo <folder>, or the FILEDO_DEPLOY_DIR environment
#  variable. It runs last - after the gate and the installer - and copies only
#  the *.exe, *.bat and *.config files, so a build that failed never overwrites
#  working tools and no log or history file travels along (REL-07).
#
#  The installer is built from the same wxs files the release workflow uses and
#  from the same staged files, so an installer defect is found here rather than
#  in a tagged run. Two artifacts land in dist\, which .gitignore keeps out of
#  the repository:
#
#    FileDO-<version>-windows-x64.msi  - the package itself
#    FileDO-<version>-setup.exe        - the setup EXE that carries that MSI
#                                        inside it; this is what a person
#                                        downloads and double-clicks
#
#  Without the WiX tool installed, the installer step says so and is skipped -
#  a missing packaging tool must not fail a build of the code. `-Msi` demands
#  it instead: with that switch a missing WiX is exit 2 ("nothing was proven"),
#  which is what release.ps1 wants before it tags anything.
#
#  -Install runs that setup EXE on THIS machine (Windows will ask for
#  elevation) and waits for it. It is the only switch here that changes the
#  machine; everything else is still local, publishes nothing and tags nothing.
#
#  Exit codes:
#    0  build (and, with -Test, the gate) succeeded
#    1  a defect was found - a build error, or a gate that failed
#    2  the gate could not verify: a prerequisite is missing, so nothing was
#       proven. Not a pass and not a defect report.
# ============================================================================
[CmdletBinding()]
param(
    # Run the local test gate after a successful build (smoke-run + go test).
    [switch]$Test,
    # Use this exact yyMMddHHmm stamp instead of the current time. release.ps1
    # passes the tag's stamp so the gate, tracked binaries and tag agree.
    [ValidatePattern('^\d{10}$')]
    [string]$Version,
    # Commit message. When set, stages everything and commits ONLY if the build
    # (and the test gate, which is forced on) succeeded. Never tags, never pushes.
    [string]$Commit,
    # Skip the VB.NET GUI build (handy when MSBuild/VS Build Tools are absent).
    [switch]$SkipGui,
    # Skip the installer entirely - no dist\ artifacts. For a fast inner loop.
    [switch]$SkipInstaller,
    # Require the installer: a missing WiX becomes exit 2 instead of a skip.
    # The installer itself is built either way; this only says how hard a
    # missing packaging tool is. release.ps1 passes it before it tags.
    [switch]$Msi,
    # Build the installer and RUN it - the setup EXE starts, Windows asks for
    # elevation, and this script waits for it and reports how it ended. This
    # is the only switch that changes the machine it runs on.
    [switch]$Install,
    # Copy the built executables into this folder after everything else passed.
    # Default: the FILEDO_DEPLOY_DIR environment variable; with neither, nothing
    # is copied anywhere.
    [string]$DeployTo = $env:FILEDO_DEPLOY_DIR
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
# StrictMode refuses to read an automatic variable no native command has set yet.
# Global, never script-scoped: a script-scoped copy would shadow every later exit code.
$global:LASTEXITCODE = 0
$root = $PSScriptRoot

# REL-06: every relative path below (`go test ./fdsec/`, `go vet ./cmd/filedo/`) means
# the repository, whatever directory the caller stands in. The try below has no body
# indentation of its own; it exists so that every `exit` - there are many - leaves
# through the finally at the end, which puts the caller's location and Go environment back.
Push-Location $root
$savedGoEnv = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
try {

# A commit implies the test gate: we never commit an untested build.
if ($Commit) { $Test = $true }
# The installer is part of a normal build. Two switches bend that: -SkipInstaller
# drops it, and -Install (or -Msi) demands it - with either of those, a missing
# WiX is a verdict of "could not verify" rather than a line of prose.
if ($Install) { $Msi = $true }
$buildInstaller  = -not $SkipInstaller
$installerNeeded = $Msi -and -not $SkipInstaller
if ($Msi -and $SkipInstaller) {
    Write-Host "-Msi and -SkipInstaller contradict each other: one demands the installer, the other drops it." -ForegroundColor Yellow
    exit 2
}

# Version = current build date/time in yyMMddHHmm format unless a release pins it.
if (-not $Version) { $Version = Get-Date -Format "yyMMddHHmm" }
$version = $Version
# REL-05: ten digits are not yet a stamp - month 13 or day 32 must not reach a binary.
try {
    $stampDate = [datetime]::ParseExact($version, 'yyMMddHHmm', [Globalization.CultureInfo]::InvariantCulture)
} catch {
    Write-Host "Cannot verify: version '$version' is not a real yyMMddHHmm date." -ForegroundColor Yellow
    exit 2
}

# The installer's version (PKG-01). Windows Installer compares only the first three fields
# of ProductVersion, so the old yy.MM.dd.HHmm mapping made two releases of one day the same
# version and the older MSI stayed installed beside the newer one. The minute of the month
# goes into field 3 instead: yy . M . ((d-1)*1440 + H*60 + m) - at most 99, 12 and 44 639,
# inside MSI's 255 / 255 / 65 535. The setup EXE carries the same version (Burn compares
# four fields and reads a missing fourth as 0). release.yml derives the identical triple,
# and release.ps1 asserts it is greater than the last published MSI's before it tags.
# Only this derived mapping changed; the stamp and the PE version below did not.
function Get-InstallerVersion([datetime]$when) {
    "{0}.{1}.{2}" -f ($when.Year % 100), $when.Month, (($when.Day - 1) * 1440 + $when.Hour * 60 + $when.Minute)
}
$installerVersion = Get-InstallerVersion $stampDate
# The PE VS_VERSIONINFO keeps yy.M.d.HHmm - the mapping release.yml, the GUI build and
# msix\build-msix.ps1 all use, and what About and the smoke test read back.
$vMaj = $stampDate.Year % 100; $vMin = $stampDate.Month; $vPat = $stampDate.Day
$vBld = $stampDate.Hour * 100 + $stampDate.Minute
$peVersion = "$vMaj.$vMin.$vPat.$vBld"

Write-Host "Version: $version   (PE $peVersion, installer $installerVersion)"
Write-Host ""

$out = "$root\exe_to_download"

function Write-BuildSummary {
    Write-Host ""
    Write-Host "== Built =="
    Write-Host "  Executables : $out"
    $setupBuilt = "$root\dist\FileDO-$version-setup.exe"
    $msiBuilt   = "$root\dist\FileDO-$version-windows-x64.msi"
    if (Test-Path $setupBuilt) {
        Write-Host "  INSTALLER   : $setupBuilt" -ForegroundColor Green
        Write-Host "  MSI inside  : $msiBuilt"
        if (-not $Install) { Write-Host "  Run it with a double-click, or: .\build.ps1 -Install" }
    } elseif ($SkipInstaller) {
        Write-Host "  Installer   : not built (-SkipInstaller)." -ForegroundColor Yellow
    } else {
        Write-Host "  Installer   : NOT built - see the Installer section above." -ForegroundColor Yellow
    }
}

# REL-16: a native command whose failure means "stop" goes through here, so no exit
# code is left unread. The calls that judge their own exit code - the gate's go test
# and go vet runs, wix extension list, git symbolic-ref - read it right after the call.
function Invoke-Checked([string]$Label, [scriptblock]$Block, [int]$FailCode = 1) {
    $nativeOut = & $Block
    if ($LASTEXITCODE -ne 0) {
        Write-Host "$Label failed (exit $LASTEXITCODE)." -ForegroundColor Red
        if ($nativeOut) { Write-Host ($nativeOut | Out-String) }
        exit $FailCode
    }
    # No output is no output: emitting $null would make @(...) a one-element array.
    if ($null -ne $nativeOut) { $nativeOut }
}

function Write-GateVerdict([int]$code, [string[]]$failures, [string[]]$unverified, [int]$ran, [int]$skipped) {
    if ($code -eq 0) {
        Write-Host "build-gate ${version}: PASS (ran $ran, skipped $skipped)" -ForegroundColor Green
    } elseif ($code -eq 1) {
        Write-Host "build-gate ${version}: FAIL ($($failures.Count) step(s): $($failures -join ', '))" -ForegroundColor Red
    } else {
        Write-Host "build-gate ${version}: NOT VERIFIED ($($unverified -join '; '))" -ForegroundColor Yellow
    }
}

# Environment before work, not after: a missing toolchain means this run can
# prove nothing, which is exit 2 ("could not verify") and not exit 1 ("found a
# defect"). Checking it here rather than inside the gate is the point - by the
# time the gate runs, a missing Go would already have surfaced as build errors.
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Cannot verify: 'go' is not on PATH - nothing was built or checked." -ForegroundColor Yellow
    exit 2
}

# REL-04: the release ships windows/amd64, so that is what is built and tested here. A host
# that cannot run an amd64 executable could only test some other build - which proves nothing
# about the one that ships, so it is "could not verify", never a quiet fall-back to 386.
$osArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture
$runsAmd64 = ($osArch -eq 'X64') -or ($osArch -eq 'Arm64' -and [Environment]::OSVersion.Version.Build -ge 22000)
if (-not $runsAmd64) {
    Write-Host "Cannot verify: this $osArch Windows cannot run the amd64 executables the release ships." -ForegroundColor Yellow
    exit 2
}

# CI-03: one toolchain for every channel. go.mod's `toolchain` line is the pin (the `go` line
# stays the language floor); release.yml installs the same version and the gate checks that
# it does. A different local Go would compile a different binary from the one the tag ships.
$goModText = Get-Content "$root\go.mod" -Raw
if ($goModText -notmatch '(?m)^toolchain\s+(go\d+\.\d+(?:\.\d+)?)\s*$') {
    Write-Host "go.mod has no 'toolchain goX.Y.Z' line - the release toolchain is not pinned." -ForegroundColor Red
    exit 1
}
$pinnedToolchain = $Matches[1]
$localToolchain = (& go env GOVERSION | Out-String).Trim()
if ($LASTEXITCODE -ne 0) {
    Write-Host "Cannot verify: 'go env GOVERSION' failed ($LASTEXITCODE): $localToolchain" -ForegroundColor Yellow
    exit 2
}
if ($localToolchain -ne $pinnedToolchain) {
    Write-Host "Cannot verify: the local Go is $localToolchain, the release builds with $pinnedToolchain (go.mod toolchain)." -ForegroundColor Yellow
    Write-Host "  Install $pinnedToolchain, or set GOTOOLCHAIN=$pinnedToolchain so go fetches it, or move the pin" -ForegroundColor Yellow
    Write-Host "  (go.mod 'toolchain' and release.yml 'go-version') to the new version together." -ForegroundColor Yellow
    exit 2
}

# goversioninfo writes the PE version and links app.manifest (long paths, UTF-8 code page).
# Pinned to the version release.yml and msix\build-msix.ps1 install.
$goversioninfoPin = 'v1.4.1'
$gvi = Get-Command goversioninfo -ErrorAction SilentlyContinue
$gviVersion = $null
if ($gvi) {
    foreach ($line in ((& go version -m $gvi.Source | Out-String) -split "`r?`n")) {
        if ($line -match '^\s*mod\s+github\.com/josephspurrier/goversioninfo\s+(\S+)') { $gviVersion = $Matches[1]; break }
    }
}
if ($gviVersion -ne $goversioninfoPin) {
    $found = if ($gvi) { "goversioninfo $gviVersion at $($gvi.Source)" } else { "no goversioninfo on PATH" }
    Write-Host "Cannot verify: $found; the release embeds its resources with goversioninfo $goversioninfoPin." -ForegroundColor Yellow
    Write-Host "  go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@$goversioninfoPin" -ForegroundColor Yellow
    exit 2
}

# From here on every go command in this process - builds, go test, go vet - targets what
# ships. The finally at the end of the script restores the caller's values.
$env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'

# goversioninfo -64 into resource.syso, which `go build` links from the package directory:
# the PE version fields and app.manifest, exactly the release.yml invocation. The file is
# removed again right after each use - a stale amd64 .syso would break any later 386 build.
function New-VersionResource([string]$dir) {
    Push-Location $dir
    try {
        $gviOut = & goversioninfo -64 -ver-major $vMaj -ver-minor $vMin -ver-patch $vPat -ver-build $vBld `
            -product-ver-major $vMaj -product-ver-minor $vMin -product-ver-patch $vPat -product-ver-build $vBld `
            -file-version $peVersion -product-version $peVersion -o resource.syso versioninfo.json 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0) { throw "goversioninfo exit $LASTEXITCODE in ${dir}: $gviOut" }
    } finally {
        Pop-Location
    }
}
function Remove-VersionResource([string]$dir) {
    Remove-Item (Join-Path $dir 'resource.syso') -Force -ErrorAction SilentlyContinue
}

$builds = @(
    @{ Name = "filedo";       Dir = "$root\cmd\filedo";       Out = "$out\filedo.exe" },
    @{ Name = "filedo_fill";  Dir = "$root\cmd\filedo-fill";  Out = "$out\filedo_fill.exe" },
    @{ Name = "filedo_check"; Dir = "$root\cmd\filedo-check"; Out = "$out\filedo_check.exe" },
    @{ Name = "filedo_test";  Dir = "$root\cmd\filedo-test";  Out = "$out\filedo_test.exe" }
)

$failed = @()

# ---- Go variants -----------------------------------------------------------
foreach ($b in $builds) {
    $name = $b.Name
    Write-Host "Building $name..." -NoNewline

    Push-Location $b.Dir
    try {
        # The release.yml build line, flag for flag: the version resource, -trimpath, the
        # stamp. No -s -w - stripped Go binaries draw more antivirus false positives.
        New-VersionResource $b.Dir
        $goOut = go build -trimpath -ldflags "-X main.version=$version" -o $b.Out . 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0) { throw "exit code $LASTEXITCODE`n$goOut" }
        Write-Host " OK"
    } catch {
        Write-Host " FAILED: $_"
        $failed += $name
    } finally {
        Remove-VersionResource $b.Dir
        Pop-Location
    }
}

# MSBuild missing is a prerequisite, not a defect (REL-06): the GUI could not be built, so
# nothing about it was proven - exit 2 after the Go results, unless -SkipGui asked for that.
$guiUnverified = $null

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
        $guiVersion = $peVersion
        & $msbuild "$root\filedo_win_vb\FileDOGUI.vbproj" /t:Rebuild /p:Configuration=Release /p:Platform=AnyCPU /p:BuildStamp=$version /p:AssemblyVersion=$guiVersion /v:quiet /nologo | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host " FAILED: MSBuild exit code $LASTEXITCODE"
            $failed += "filedo_win"
        } else {
            Copy-Item "$root\filedo_win_vb\bin\Release\filedo_win.exe" "$out\filedo_win.exe" -Force
            # The GUI's app.config travels with the exe or the DPI declaration in it does nothing:
            # the manifest inside the exe makes the process per-monitor aware, and this file is
            # what makes WinForms rescale its controls. Shipping one without the other looks worse
            # than shipping neither (SP-0006 M0).
            $guiConfig = "$root\filedo_win_vb\bin\Release\filedo_win.exe.config"
            if (Test-Path $guiConfig) { Copy-Item $guiConfig "$out\filedo_win.exe.config" -Force }
            Write-Host " OK"
        }
    } else {
        Write-Host " NOT BUILT: MSBuild not found (install VS Build Tools, or pass -SkipGui)" -ForegroundColor Yellow
        $guiUnverified = "MSBuild not found - filedo_win.exe was not built"
    }
}

Write-Host ""
if ($failed.Count -ne 0) {
    # A build error outranks a missing tool: exit 1 even when MSBuild was absent too.
    Write-Host "Failed: $($failed -join ', ')"
    exit 1
}
if ($guiUnverified) {
    Write-Host "Cannot verify: $guiUnverified." -ForegroundColor Yellow
    exit 2
}
Write-Host "All builds successful."

# ---- Local test gate (-Test / implied by -Commit) ---------------------------
# Cheap, deterministic checks that the freshly built artifacts actually run.
# This is the "test locally" step of the BUILD flow - it does NOT touch CI.
if ($Test) {
    Write-Host ""
    Write-Host "== Test gate =="

    $gateFailures = [System.Collections.Generic.List[string]]::new()
    $gateUnverified = [System.Collections.Generic.List[string]]::new()
    $gateRan = 0
    $gateSkipped = 0
    function Record-GateStep([string]$name, [int]$code, [string]$detail = "") {
        if ($code -eq 0) {
            $script:gateRan++
            Write-Host " OK"
        } elseif ($code -eq 2) {
            $script:gateRan++
            $script:gateUnverified.Add($name)
            Write-Host " NOT VERIFIED: $detail" -ForegroundColor Yellow
        } else {
            $script:gateRan++
            $script:gateFailures.Add($name)
            Write-Host " FAILED" -ForegroundColor Red
            if ($detail) { Write-Host $detail }
        }
    }

    # 0) The artifact the gate is about to smoke-run must exist. If a build
    #    reported OK and the exe is still absent, the gate has proven nothing -
    #    exit 2 ("could not verify"), never exit 1 ("found a defect").
    if (-not (Test-Path "$out\filedo.exe")) {
        Write-Host "Cannot verify: $out\filedo.exe is missing." -ForegroundColor Yellow
        Write-BuildSummary
        Write-GateVerdict 2 @() @('filedo.exe is missing') 0 0
        exit 2
    }

    # 1) Each Go executable says the stamp it was built with, and is the shape
    # the release ships (REL-04): amd64, -trimpath, the pinned toolchain, and the
    # goversioninfo resource - the PE version and app.manifest. The GUI is a
    # WinExe, so its stamped PE FileVersion is the smoke surface instead.
    Write-Host "Smoke: all shipped executables carry $version and the release build shape ..." -NoNewline
    $smokeProblems = [System.Collections.Generic.List[string]]::new()
    foreach ($exe in @('filedo.exe', 'filedo_fill.exe', 'filedo_check.exe', 'filedo_test.exe')) {
        $path = Join-Path $out $exe
        if (-not (Test-Path $path)) { $smokeProblems.Add("$exe is missing"); continue }
        $smoke = & $path '-?' 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0) { $smokeProblems.Add("$exe -? exited $LASTEXITCODE") }
        if ($smoke -notmatch [regex]::Escape($version)) { $smokeProblems.Add("$exe does not print $version") }
        $buildInfo = & go version -m $path 2>&1 | Out-String
        if ($buildInfo -notmatch "(?m):\s+$([regex]::Escape($pinnedToolchain))\s*$") { $smokeProblems.Add("$exe was not built with $pinnedToolchain") }
        if ($buildInfo -notmatch '(?m)^\s*build\s+GOARCH=amd64\s*$') { $smokeProblems.Add("$exe is not an amd64 build") }
        if ($buildInfo -notmatch '(?m)^\s*build\s+-trimpath=true\s*$') { $smokeProblems.Add("$exe was built without -trimpath") }
        $fileVersion = (Get-Item $path).VersionInfo.FileVersion
        if ($fileVersion -ne $peVersion) { $smokeProblems.Add("$exe PE version is '$fileVersion', want '$peVersion'") }
        # The manifest is stored as plain XML in the PE's resources.
        if (-not [System.Text.Encoding]::ASCII.GetString([System.IO.File]::ReadAllBytes($path)).Contains('longPathAware')) {
            $smokeProblems.Add("$exe carries no app.manifest")
        }
    }
    if (-not $SkipGui) {
        $guiPath = Join-Path $out 'filedo_win.exe'
        if (-not (Test-Path $guiPath)) {
            $smokeProblems.Add('filedo_win.exe is missing')
        } else {
            $guiStamp = (Get-Item $guiPath).VersionInfo.FileVersion
            if ($guiStamp -ne $peVersion) { $smokeProblems.Add("filedo_win.exe PE version is '$guiStamp', want '$peVersion'") }
        }
    }
    if ($smokeProblems.Count) {
        Record-GateStep 'smoke' 1 ($smokeProblems -join '; ')
    } else {
        Record-GateStep 'smoke' 0
    }

    # 2) Compile-check the test module (root `go test ./...` is known-broken per
    #    AGENTS.md, so we only run the module that is expected to pass).
    #    cmd\filedo-test is its own module: the check must run from inside it.
    #    `go test ./cmd/filedo-test` from the root fails with "main module
    #    (filedo) does not contain package", so do not print that form here -
    #    a console line naming a command that cannot work gets copy-pasted.
    Write-Host "go test ./... (in cmd\filedo-test) ..." -NoNewline
    $compileCode = 0; $compileDetail = ''
    Push-Location "$root\cmd\filedo-test"
    try {
        $testOut = go test ./... 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0) {
            $compileCode = 1; $compileDetail = $testOut
        }
    } finally {
        Pop-Location
    }
    if ($compileCode -eq 0) {
        $placementOut = & "$root\packaging\check-placement.ps1" 2>&1 | Out-String
        $placementCode = $LASTEXITCODE
        if ($placementCode -ne 0) {
            $compileCode = $placementCode
            $compileDetail = $placementOut
        }
    }
    Record-GateStep 'compile-test-module' $compileCode $compileDetail

    # 3) The real tests. Two packages carry `func Test*` today and both must be
    #    green: fdsec (the container format and its vectors) and cmd\filedo
    #    (the black-box fdsec command surface, which builds its own exe and
    #    asserts exit codes, history redaction and the destructive paths).
    #    -short drops the >4 GiB round trip, which belongs to a full run, not
    #    to a per-build gate. -vet=off is required for cmd\filedo only because
    #    package main carries pre-existing vet debt (AGENTS.md "Testing");
    #    fdsec is vet-clean and is checked without it.
    Write-Host "go test ./fdsec/ ..." -NoNewline
    $fdsecOut = go test ./fdsec/ -count=1 -short 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        Record-GateStep 'fdsec' 1 $fdsecOut
    } else {
        Record-GateStep 'fdsec' 0
    }

    # The suite builds its own filedo.exe from this package directory. The version resource
    # is put back for the length of the run, so that exe carries the same app.manifest (UTF-8
    # code page, long paths) as the one that ships - a manifest-dependent defect fails here.
    Write-Host "go test ./cmd/filedo/ and vet baseline ..." -NoNewline
    $oldRootRequirement = $env:FILEDO_FDSEC_REQUIRE_REPO_ROOT
    $env:FILEDO_FDSEC_REQUIRE_REPO_ROOT = '1'
    try {
        New-VersionResource "$root\cmd\filedo"
        $cliOut = go test ./cmd/filedo/ -count=1 -vet=off 2>&1 | Out-String
        $cliNativeCode = $LASTEXITCODE
    } catch {
        $cliOut = "could not prepare the version resource: $_"
        $cliNativeCode = 1
    } finally {
        Remove-VersionResource "$root\cmd\filedo"
        if ($null -eq $oldRootRequirement) { Remove-Item Env:FILEDO_FDSEC_REQUIRE_REPO_ROOT -ErrorAction SilentlyContinue }
        else { $env:FILEDO_FDSEC_REQUIRE_REPO_ROOT = $oldRootRequirement }
    }
    $cliCode = if ($cliNativeCode -eq 0) { 0 } elseif ($cliOut -match 'FDSEC_SURFACES_REPO_ROOT_MISSING') { 2 } else { 1 }
    $cliDetails = [System.Collections.Generic.List[string]]::new()
    if ($cliCode -ne 0) { $cliDetails.Add($cliOut) }

    # CHECK-BASELINE: vet debt is a set, never a suppression. A missing file is
    # "could not verify" (rule 4), never "accept everything". HashSets rather
    # than Compare-Object, which refuses an empty side - and an empty baseline
    # is exactly where this ratchet is meant to end up.
    $vetCode = 0
    $baseline = "$root\cmd\filedo\vet-baseline.txt"
    if (-not (Test-Path $baseline)) {
        $vetCode = 2; $cliDetails.Add("could not verify: missing $baseline")
    } else {
        $vetOut = go vet ./cmd/filedo/ 2>&1 | Out-String
        $vetNativeCode = $LASTEXITCODE
        $actual = [System.Collections.Generic.HashSet[string]]::new()
        $seen = @{}
        foreach ($line in ($vetOut -split "`r?`n")) {
            if ($line -match '^(.+?):\d+:\d+:\s+(.+)$') {
                $rel = $Matches[1].Replace('\', '/')
                $key = "${rel}: $($Matches[2])"
                $seen[$key] = 1 + [int]($seen[$key])
                [void]$actual.Add("$key #$($seen[$key])")
            }
        }
        $expected = [System.Collections.Generic.HashSet[string]]::new(
            [string[]]@(Get-Content $baseline | Where-Object { $_ -and -not $_.StartsWith('#') }))
        if ($vetNativeCode -ne 0 -and $actual.Count -eq 0) {
            $vetCode = 2; $cliDetails.Add("could not verify: go vet exited $vetNativeCode with no findings`n$vetOut")
        } else {
            $new = @($actual | Where-Object { -not $expected.Contains($_) } | Sort-Object)
            $gone = @($expected | Where-Object { -not $actual.Contains($_) } | Sort-Object)
            if ($new.Count -or $gone.Count) {
                $vetCode = 1
                $drift = @($new | ForEach-Object { "new vet finding: $_" }) + @($gone | ForEach-Object { "fixed, remove from the baseline: $_" })
                $cliDetails.Add("vet baseline drift - fix a new finding; delete a fixed one from cmd\filedo\vet-baseline.txt:`n  " + ($drift -join "`n  "))
            }
        }
    }
    # A defect in either half outranks "could not verify" in the other.
    $stepCode = if ($cliCode -eq 1 -or $vetCode -eq 1) { 1 } elseif ($cliCode -eq 2 -or $vetCode -eq 2) { 2 } else { 0 }
    Record-GateStep 'filedo-cli-and-vet' $stepCode ($cliDetails -join "`n")

    # The GUI's own gate (filedo_win_vb\SelfTest.vb): it builds every job page, checks the
    # command each one would run and the localization keys behind it, then exits 0 or 1. A page
    # that throws on the way up, or a job whose label is missing in a locale, fails here rather
    # than on the first click after a release.
    #
    # It is skipped, not failed, when the GUI was not built (-SkipGui): a gate cannot prove
    # anything about an executable that is not there, and saying so is the honest outcome.
    $guiExe = "$out\filedo_win.exe"
    if ($SkipGui) {
        $gateSkipped++
        Write-Host "filedo_win.exe --selftest ... SKIPPED (-SkipGui)" -ForegroundColor Yellow
    } elseif (Test-Path $guiExe) {
        Write-Host "filedo_win.exe --selftest ..." -NoNewline
        # SP-0029 SHELL-15: the log from an earlier run must not stand in for this one. A
        # selftest that dies before writing leaves no log, which is "could not verify" - never
        # the previous run's lines printed as this run's details.
        $log = "$out\filedo_win_selftest.log"
        $staleLog = $null
        if (Test-Path $log) {
            try { Remove-Item $log -Force -ErrorAction Stop } catch { $staleLog = $_.Exception.Message }
        }
        if ($staleLog) {
            Record-GateStep 'gui-selftest' 2 "could not verify: the previous selftest log could not be removed ($staleLog)"
        } else {
            $selfTest = Start-Process $guiExe -ArgumentList "--selftest" -PassThru -Wait
            if (-not (Test-Path $log)) {
                Record-GateStep 'gui-selftest' 2 "could not verify: no selftest log (exit $($selfTest.ExitCode))"
            } elseif ($selfTest.ExitCode -ne 0) {
                $logLines = Get-Content $log
                Record-GateStep 'gui-selftest' $selfTest.ExitCode (($logLines | Where-Object { $_ -like 'FAIL *' -or $_ -like 'selftest:*' }) -join "`n")
            } else {
                Record-GateStep 'gui-selftest' 0
            }
        }
    } else {
        $gateSkipped++
        Write-Host "filedo_win.exe --selftest ... SKIPPED (no GUI in this build)" -ForegroundColor Yellow
    }

    # 6) THIRD-PARTY-NOTICES.txt against the Go modules each shipped executable links
    #    (REL-18). The file is edited by hand; a dependency added or bumped without its
    #    license section would otherwise ship without one (canon invariant 12).
    Write-Host "third-party notices vs linked modules ..." -NoNewline
    $noticesOut = & "$root\packaging\check-third-party-notices.ps1" *>&1 | Out-String
    $noticesCode = $LASTEXITCODE
    Record-GateStep 'third-party-notices' $noticesCode $(if ($noticesCode -eq 2) { ($noticesOut.Trim() -split "`r?`n")[-1] } else { $noticesOut })

    # 7) The pins that make this gate's binary the release's binary (CI-03): the Go the
    #    workflow installs is go.mod's toolchain line, which the local Go was checked
    #    against before the build. Drift between the two files would only surface inside the
    #    tagged run - one irreversible step too late.
    Write-Host "release.yml pins the same Go ($pinnedToolchain) ..." -NoNewline
    $workflow = "$root\.github\workflows\release.yml"
    if (-not (Test-Path $workflow)) {
        Record-GateStep 'release-pins' 2 "could not verify: $workflow is missing"
    } else {
        $workflowGo = @([regex]::Matches((Get-Content $workflow -Raw), "(?m)^\s*go-version:\s*'?`"?([0-9.]+)'?`"?\s*$") | ForEach-Object { "go$($_.Groups[1].Value)" })
        $pinProblems = @()
        if ($workflowGo.Count -eq 0) { $pinProblems += "release.yml sets no go-version" }
        foreach ($w in $workflowGo) { if ($w -ne $pinnedToolchain) { $pinProblems += "release.yml installs $w, go.mod pins $pinnedToolchain" } }
        Record-GateStep 'release-pins' ([int]($pinProblems.Count -gt 0)) ($pinProblems -join '; ')
    }

    # 8) The documentation corpus against DOC-INTERNAL-QUALITY (SP-0061): every document
    #    declared in docs/DOCUMENT_REGISTRY.jsonl and every declared one on disk, every
    #    relative link and anchor resolving, and the internal corpus in house style with no
    #    remote embeds. The flag tables are held to the parser by go test (step 4).
    Write-Host "documentation registry, links and style ..." -NoNewline
    $docsOut = & "$root\packaging\check-internal-docs.ps1" *>&1 | Out-String
    $docsCode = $LASTEXITCODE
    Record-GateStep 'internal-docs' $docsCode $(if ($docsCode -eq 2) { ($docsOut.Trim() -split "`r?`n")[-1] } else { $docsOut })

    # 9) The published site and the READMEs against DOC-EXTERNAL-QUALITY (SP-0062): glossary
    #    and subject index, the sitemap against the page set, the SEO block, every locale
    #    present and its translation not older than the English, the termbase, screenshots,
    #    and prose hygiene. An EN edit fails here until check-external-docs.ps1 -Record.
    Write-Host "published site and READMEs ..." -NoNewline
    $siteOut = & "$root\packaging\check-external-docs.ps1" *>&1 | Out-String
    $siteCode = $LASTEXITCODE
    Record-GateStep 'external-docs' $siteCode $(if ($siteCode -eq 2) { ($siteOut.Trim() -split "`r?`n")[-1] } else { $siteOut })

    # A failed step outranks a step that could not verify. All nine steps run
    # before this decision, so one run names every defect it found.
    $gateExit = if ($gateFailures.Count) { 1 } elseif ($gateUnverified.Count) { 2 } else { 0 }
    if ($gateExit -ne 0) {
        Write-BuildSummary
        Write-GateVerdict $gateExit $gateFailures.ToArray() $gateUnverified.ToArray() $gateRan $gateSkipped
        exit $gateExit
    }
}

# ---- Optional installer (-Msi) ---------------------------------------------
# The MSI is what turns four executables into a Windows program: it puts
# filedo on PATH, creates the shortcuts, and - as a feature the user can
# deselect - registers the .fd-sec document type and the two Explorer verbs
# (packaging\wix\FileDO.wxs). Building it here means an installer defect
# surfaces during a build rather than inside a tagged release run.
#
# Everything the wxs reads comes from a staged folder, exactly as the release
# workflow stages it: the built exes, the GUI's config, the .bat helpers,
# LICENSE, THIRD-PARTY-NOTICES.txt, README.md, and assets\icon.ico under the name the document type
# points at (FileDO.ico).
$wixPresent = [bool](Get-Command wix -ErrorAction SilentlyContinue)
if ($buildInstaller -and -not $wixPresent) {
    Write-Host ""
    Write-Host "== Installer =="
    Write-Host "Skipped: the WiX tool is not on PATH, so dist\ has no installer." -ForegroundColor Yellow
    Write-Host "  dotnet tool install --global wix --version 5.0.2   (the version release.yml pins)"
    # A missing packaging tool is not a defect in the code that was just built
    # - unless the caller asked for the installer by name.
    if ($installerNeeded) { exit 2 }
}

# A function, not an inline block, for one reason: the steps below can decide
# midway that there will be no installer this run (a packaging tool that will
# not install, a stage missing the GUI). Without -Msi that is a skip, and a
# skip has to be able to leave early without taking the whole build with it.
function Build-FileDOInstaller {
    Write-Host ""
    Write-Host "== Installer =="

    # Two WiX extensions are needed, and both must be pinned to the tool's own
    # version: unpinned, `wix extension add` resolves to the newest release
    # (7.x today), which a WiX 5 tool refuses with "Could not find expected
    # package root folder wixext5".
    #   UI                      - WixUI_FeatureTree, the feature checkboxes
    #   BootstrapperApplications - Burn, which turns the MSI into a setup EXE
    $wixVersion = ((& wix --version) | Select-Object -First 1) -replace '\+.*', ''
    $extList = & wix extension list --global 2>&1 | Out-String
    foreach ($ext in @("WixToolset.UI.wixext", "WixToolset.BootstrapperApplications.wixext")) {
        # REL-08: the name AND the tool's version. A 7.x copy of the extension in the global
        # cache matches the name alone, skips the pinned add, and `wix build` then fails on
        # "wixext5".
        if ($extList -match "(?m)^\s*$([regex]::Escape($ext))\s+$([regex]::Escape($wixVersion))(\s|$)") { continue }
        Write-Host "Adding $ext/$wixVersion ..." -NoNewline
        & wix extension add --global "$ext/$wixVersion" 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Host " FAILED"
            Write-Host "No installer this run: $ext/$wixVersion could not be added." -ForegroundColor Yellow
            if ($installerNeeded) { exit 2 }
            return
        }
        Write-Host " OK"
    }

    # The installer version, not the PE one: see Get-InstallerVersion at the top (PKG-01).
    # MSI version fields are numeric and bounded, so the stamp cannot be the
    # product version verbatim; the stamp stays the truth everywhere else.
    $productVersion = $installerVersion

    $dist  = "$root\dist"
    $stage = "$dist\FileDO"
    Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
    New-Item -ItemType Directory -Force -Path $stage | Out-Null

    # Every build stamps a new version, so without this dist\ would collect one
    # 20 MB pair per run and the newest would be just another row in a list.
    # What stays here is this build's installer - and anything Windows will not
    # let go of, which is never a surprise worth hiding: a locked artifact means
    # an installer from an earlier build is open right now, and the person who
    # opened it should hear that rather than wonder why dist\ has three files.
    foreach ($stale in @(Get-ChildItem $dist -File -Filter "FileDO-*" -ErrorAction SilentlyContinue |
                         Where-Object { $_.Name -notlike "FileDO-$version*" })) {
        try {
            Remove-Item $stale.FullName -Force -ErrorAction Stop
        } catch {
            Write-Host "  kept $($stale.Name) - in use (an installer from that build is open)." -ForegroundColor Yellow
        }
    }

    Copy-Item "$out\filedo*.exe"        $stage -Force
    Copy-Item "$out\filedo_win.exe.config" $stage -Force -ErrorAction SilentlyContinue
    Copy-Item "$out\*.bat"              $stage -Force
    Copy-Item "$root\LICENSE"           $stage -Force
    Copy-Item "$root\THIRD-PARTY-NOTICES.txt" $stage -Force
    Copy-Item "$root\README.md"         $stage -Force
    Copy-Item "$root\assets\icon.ico"   "$stage\FileDO.ico" -Force
    # The Explorer entries' and the document type's meaning icons (SP-0016 T8).
    New-Item -ItemType Directory -Force -Path "$stage\icons" | Out-Null
    Copy-Item "$root\assets\menu-icons\*.ico" "$stage\icons" -Force

    # Every file the wxs names must be in the stage, or wix reports it one at
    # a time. Saying so here names them all at once.
    $required = @("filedo.exe", "filedo_win.exe", "filedo_win.exe.config", "filedo_check.exe",
                  "filedo_fill.exe", "filedo_test.exe", "FileDO.ico", "LICENSE", "THIRD-PARTY-NOTICES.txt", "README.md",
                  "filedo_cd.bat", "filedo_clean.bat", "filedo_fill.bat", "filedo_speed.bat", "filedo_test.bat",
                  "icons\action.secure.ico", "icons\action.unsecure.ico", "icons\action.wipe.ico",
                  "icons\action.verify.ico", "icons\app.info.ico", "icons\content.secret-file.ico")
    $missing = $required | Where-Object { -not (Test-Path "$stage\$_") }
    if ($missing) {
        Write-Host "No installer this run: the stage is missing $($missing -join ', ')." -ForegroundColor Yellow
        Write-Host "(-SkipGui does not rebuild filedo_win.exe; the stage then carries"
        Write-Host " whatever exe_to_download\ already holds, and here it holds nothing.)"
        if ($installerNeeded) { exit 2 }
        return
    }

    $msiPath = "$dist\FileDO-$version-windows-x64.msi"
    Write-Host "wix build -> $msiPath ..." -NoNewline
    $wixOut = & wix build "$root\packaging\wix\FileDO.wxs" `
        -arch x64 `
        -ext WixToolset.UI.wixext `
        -d "ProductVersion=$productVersion" `
        -d "StageDir=$stage" `
        -d "IconFile=$root\assets\icon.ico" `
        -d "LicenseRtf=$root\packaging\wix\License.rtf" `
        -d "BannerBmp=$root\packaging\wix\banner.bmp" `
        -d "DialogBmp=$root\packaging\wix\dialog.bmp" `
        -loc "$root\packaging\wix\FileDO.en-us.wxl" `
        -pdbtype none `
        -o $msiPath 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        Write-Host " FAILED"
        Write-Host $wixOut
        exit 1
    }
    Write-Host " OK"
    Write-Host "  SHA256: $((Get-FileHash -Algorithm SHA256 $msiPath).Hash)"

    # ---- The setup EXE -----------------------------------------------------
    # A Burn bundle carrying that MSI. This is the artifact a person downloads:
    # one file, double-clickable, and the one an unsigned .msi download often
    # cannot be. Its UI is the MSI's own feature tree, so there is no second
    # wizard to keep in step with the first.
    $setupPath = "$dist\FileDO-$version-setup.exe"
    Write-Host "wix build -> $setupPath ..." -NoNewline
    $burnOut = & wix build "$root\packaging\wix\FileDO-bundle.wxs" `
        -arch x64 `
        -ext WixToolset.BootstrapperApplications.wixext `
        -d "ProductVersion=$productVersion" `
        -d "MsiFile=$msiPath" `
        -d "IconFile=$root\assets\icon.ico" `
        -d "LicenseRtf=$root\packaging\wix\License.rtf" `
        -pdbtype none `
        -o $setupPath 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        Write-Host " FAILED"
        Write-Host $burnOut
        exit 1
    }
    Write-Host " OK"
    Write-Host "  SHA256: $((Get-FileHash -Algorithm SHA256 $setupPath).Hash)"

    # The staged payload is 40 MB of copies of files that are already in
    # exe_to_download\, and it has done its job the moment the EXE is built.
    # Leaving it next to the two artifacts is what makes a person ask which of
    # the three things in dist\ is the installer.
    Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue

    Write-Host ""
    Write-Host "Install it:"
    Write-Host "  `"$setupPath`"                      # the wizard, feature checkboxes and all"
    Write-Host "  `"$setupPath`" /quiet               # no questions, every feature"
    Write-Host "  msiexec /i `"$msiPath`" /qn ADDLOCAL=Main   # the package alone, no integration"
    Write-Host "Remove it:"
    Write-Host "  `"$setupPath`" /uninstall"
}

if ($buildInstaller -and $wixPresent) { Build-FileDOInstaller }

# ---- Optional install (-Install) -------------------------------------------
# Runs the setup EXE just built. This is the one thing in this script that
# changes the machine it runs on, which is why it needs its own switch: a
# build that silently installed itself would be a build nobody could run
# twice in a row without thinking about it.
#
# It starts the EXE with its window, so Windows asks for elevation the normal
# way and the feature checkboxes are there to be seen. Burn's exit codes are
# MSI's: 0 is done, 3010 is done-and-wants-a-restart, 1602 is the user
# cancelling - which is an answer, not a defect, so it is not exit 1 here.
if ($Install) {
    Write-Host ""
    Write-Host "== Install =="
    $setupPath = "$root\dist\FileDO-$version-setup.exe"
    if (-not (Test-Path $setupPath)) {
        Write-Host "Cannot verify: $setupPath was not built." -ForegroundColor Yellow
        exit 2
    }
    Write-Host "Starting $setupPath ..."
    Write-Host "(Windows will ask for elevation; the installer's own window follows.)"
    $proc = Start-Process -FilePath $setupPath -PassThru -Wait
    switch ($proc.ExitCode) {
        0     { Write-Host "Installed. `"filedo`" works in a NEW console - PATH is read at start." }
        3010  { Write-Host "Installed. Windows wants a restart to finish (exit 3010)." }
        1602  { Write-Host "Cancelled by the user (exit 1602). Nothing was installed." -ForegroundColor Yellow }
        1603  { Write-Host "The installer failed (exit 1603). Run it by hand to see the message:" -ForegroundColor Yellow
                Write-Host "  `"$setupPath`" /log `"$root\dist\install.log`""
                exit 1 }
        default { Write-Host "The installer ended with exit code $($proc.ExitCode)." -ForegroundColor Yellow
                  exit 1 }
    }
}

# ---- Optional commit (-Commit "msg") ----------------------------------------
# Reached only if every build succeeded AND the test gate passed (the early
# `exit 1`s above guarantee that). Stages and commits; never tags, never pushes.
if ($Commit) {
    Write-Host ""
    Write-Host "== Commit =="
    Push-Location $root
    try {
        # `git symbolic-ref -q` answers a detached HEAD with exit 1 and no output; the
        # output is what is read here.
        if ($null -eq (git symbolic-ref -q HEAD)) {
            Write-Host "Refusing to commit: detached HEAD."; exit 1
        }
        Invoke-Checked 'git add -A' { git add -A } | Out-Null
        $staged = Invoke-Checked 'git diff --cached' { git diff --cached --name-only }
        if (-not $staged) {
            Write-Host "Nothing to commit - working tree clean."
        } else {
            Invoke-Checked 'git commit' { git commit -m $Commit }
            Write-Host "Committed: $Commit"
            Write-Host "(Not pushed, not tagged. Use release.ps1 to publish.)"
        }
    } finally {
        Pop-Location
    }
}

# ---- Optional local deploy (-DeployTo / FILEDO_DEPLOY_DIR) -----------------
# Last, because only now has everything that was asked for passed: the build, the
# gate (-Test), the installer, the install (-Install). It was the first thing
# after the build once, and a build that then failed its gate had already
# overwritten the developer's working tools (REL-07). Only the programs travel -
# the executables, the .bat helpers and the GUI's .config; never a log, a
# history.json or anything else a run leaves in exe_to_download\.
if ($DeployTo) {
    Write-Host ""
    if (-not (Test-Path $DeployTo -PathType Container)) {
        Write-Host "Deploy skipped: $DeployTo is not a folder (from -DeployTo or FILEDO_DEPLOY_DIR)." -ForegroundColor Yellow
    } else {
        $deployFiles = @(Get-ChildItem $out -File | Where-Object { $_.Extension -in '.exe', '.bat', '.config' })
        Write-Host "Deploying $($deployFiles.Count) files to $DeployTo ..." -NoNewline
        $deployFiles | Copy-Item -Destination $DeployTo -Force
        Write-Host " OK"
    }
}

# ---- What this run produced -------------------------------------------------
Write-BuildSummary
if ($Test) {
    # CHECK-VERDICT rule 5: the gate's machine-readable conclusion is last.
    Write-GateVerdict 0 @() @() $gateRan $gateSkipped
}
# Every failure path above exits on its own. Without this line the script's exit
# code is whatever the last native command left in $LASTEXITCODE - `go vet`,
# which exits 1 on the accepted baseline debt - and a passing gate reads as 1.
exit 0
} catch {
    # A terminating error nobody expected proves nothing - exit 2 with the reason, never a
    # stale $LASTEXITCODE that reads as a pass to the caller (release.ps1 runs this in-process).
    Write-Host "Cannot verify: unexpected error at line $($_.InvocationInfo.ScriptLineNumber): $($_.Exception.Message)" -ForegroundColor Yellow
    if ($Test) { Write-Host "build-gate ${version}: NOT VERIFIED (unexpected error: $($_.Exception.Message))" -ForegroundColor Yellow }
    exit 2
} finally {
    # Every exit above leaves through here: the caller gets back its own directory and
    # Go environment (this script may run inside release.ps1's process).
    foreach ($k in @($savedGoEnv.Keys)) { [Environment]::SetEnvironmentVariable($k, $savedGoEnv[$k], 'Process') }
    Pop-Location
}
