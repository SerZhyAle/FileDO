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
    [switch]$Install
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

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

# Version = current build date/time in yyMMddHHmm format
$version = Get-Date -Format "yyMMddHHmm"

Write-Host "Version: $version"
Write-Host ""

$out = "$root\exe_to_download"

# Environment before work, not after: a missing toolchain means this run can
# prove nothing, which is exit 2 ("could not verify") and not exit 1 ("found a
# defect"). Checking it here rather than inside the gate is the point - by the
# time the gate runs, a missing Go would already have surfaced as build errors.
if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Cannot verify: 'go' is not on PATH - nothing was built or checked." -ForegroundColor Yellow
    exit 2
}

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
            # The GUI's app.config travels with the exe or the DPI declaration in it does nothing:
            # the manifest inside the exe makes the process per-monitor aware, and this file is
            # what makes WinForms rescale its controls. Shipping one without the other looks worse
            # than shipping neither (SP-0006 M0).
            $guiConfig = "$root\filedo_win_vb\bin\Release\filedo_win.exe.config"
            if (Test-Path $guiConfig) { Copy-Item $guiConfig "$out\filedo_win.exe.config" -Force }
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

    # 0) The artifact the gate is about to smoke-run must exist. If a build
    #    reported OK and the exe is still absent, the gate has proven nothing -
    #    exit 2 ("could not verify"), never exit 1 ("found a defect").
    if (-not (Test-Path "$out\filedo.exe")) {
        Write-Host "Cannot verify: $out\filedo.exe is missing." -ForegroundColor Yellow
        exit 2
    }

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
    #    cmd\filedo-test is its own module: the check must run from inside it.
    #    `go test ./cmd/filedo-test` from the root fails with "main module
    #    (filedo) does not contain package", so do not print that form here -
    #    a console line naming a command that cannot work gets copy-pasted.
    Write-Host "go test ./... (in cmd\filedo-test) ..." -NoNewline
    Push-Location "$root\cmd\filedo-test"
    try {
        $testOut = go test ./... 2>&1 | Out-String
        if ($LASTEXITCODE -ne 0) {
            Write-Host " FAILED"
            Write-Host $testOut
            exit 1
        }
        Write-Host " OK"
    } finally {
        Pop-Location
    }

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
        Write-Host " FAILED"
        Write-Host $fdsecOut
        exit 1
    }
    Write-Host " OK"

    Write-Host "go test ./cmd/filedo/ (fdsec command surface) ..." -NoNewline
    $cliOut = go test ./cmd/filedo/ -count=1 -vet=off 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0) {
        Write-Host " FAILED"
        Write-Host $cliOut
        exit 1
    }
    Write-Host " OK"

    # The GUI's own gate (filedo_win_vb\SelfTest.vb): it builds every job page, checks the
    # command each one would run and the localization keys behind it, then exits 0 or 1. A page
    # that throws on the way up, or a job whose label is missing in a locale, fails here rather
    # than on the first click after a release.
    #
    # It is skipped, not failed, when the GUI was not built (-SkipGui): a gate cannot prove
    # anything about an executable that is not there, and saying so is the honest outcome.
    $guiExe = "$out\filedo_win.exe"
    if (Test-Path $guiExe) {
        Write-Host "filedo_win.exe --selftest ..." -NoNewline
        $selfTest = Start-Process $guiExe -ArgumentList "--selftest" -PassThru -Wait
        if ($selfTest.ExitCode -ne 0) {
            Write-Host " FAILED"
            $log = "$out\filedo_win_selftest.log"
            if (Test-Path $log) { Get-Content $log | Where-Object { $_ -like "FAIL*" } | Write-Host }
            exit 1
        }
        Write-Host " OK"
    } else {
        Write-Host "filedo_win.exe --selftest ... SKIPPED (no GUI in this build)"
    }

    Write-Host "Test gate passed."
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
# LICENSE, README.md, and assets\icon.ico under the name the document type
# points at (FileDO.ico).
$wixPresent = [bool](Get-Command wix -ErrorAction SilentlyContinue)
if ($buildInstaller -and -not $wixPresent) {
    Write-Host ""
    Write-Host "== Installer =="
    Write-Host "Skipped: the WiX tool is not on PATH, so dist\ has no installer." -ForegroundColor Yellow
    Write-Host "  dotnet tool install --global wix --version 5.*"
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
        if ($extList -match [regex]::Escape($ext)) { continue }
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

    # yyMMddHHmm -> Major.Minor.Patch.Build, the same mapping release.yml uses.
    # MSI version fields are numeric and bounded, so the stamp cannot be the
    # product version verbatim; the stamp stays the truth everywhere else.
    if ($version -match '^(\d{2})(\d{2})(\d{2})(\d{4})$') {
        $productVersion = "$([int]$Matches[1]).$([int]$Matches[2]).$([int]$Matches[3]).$([int]$Matches[4])"
    } else {
        Write-Host "Cannot verify: version '$version' is not the yyMMddHHmm stamp." -ForegroundColor Yellow
        exit 2
    }

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
    Copy-Item "$root\README.md"         $stage -Force
    Copy-Item "$root\assets\icon.ico"   "$stage\FileDO.ico" -Force

    # Every file the wxs names must be in the stage, or wix reports it one at
    # a time. Saying so here names them all at once.
    $required = @("filedo.exe", "filedo_win.exe", "filedo_win.exe.config", "filedo_check.exe",
                  "filedo_fill.exe", "filedo_test.exe", "FileDO.ico", "LICENSE", "README.md",
                  "filedo_cd.bat", "filedo_clean.bat", "filedo_fill.bat", "filedo_speed.bat", "filedo_test.bat")
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

# ---- What this run produced -------------------------------------------------
# The last thing on the screen, because that is the only line a person is
# guaranteed to see: everything above it scrolls. It names full paths - a build
# whose output you have to go looking for is a build that answers "so where is
# the installer?" with silence.
Write-Host ""
Write-Host "== Built =="
Write-Host "  Executables : $out"
$setupBuilt = "$root\dist\FileDO-$version-setup.exe"
$msiBuilt   = "$root\dist\FileDO-$version-windows-x64.msi"
if (Test-Path $setupBuilt) {
    Write-Host "  INSTALLER   : $setupBuilt" -ForegroundColor Green
    Write-Host "  MSI inside  : $msiBuilt"
    if (-not $Install) {
        Write-Host "  Run it with a double-click, or: .\build.ps1 -Install"
    }
} elseif ($SkipInstaller) {
    Write-Host "  Installer   : not built (-SkipInstaller)." -ForegroundColor Yellow
} else {
    Write-Host "  Installer   : NOT built - see the Installer section above." -ForegroundColor Yellow
}
