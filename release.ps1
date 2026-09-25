#Requires -Version 7.0
# ============================================================================
#  FileDO - "RELIZ" (RELEASE): the public, CI-driven flow.
#
#  This is the RELEASE half of the project's two-flow model (see RELEASE.md):
#
#    BUILD  (build.ps1)   -> compile, test locally, commit. Never touches CI.
#    RELEASE (this script) -> the ONLY thing that tags v* and triggers GitHub.
#
#  Pushing a `v*` tag is the single action that starts the public GitHub
#  release (.github/workflows/release.yml). Everything that publishes lives
#  here so a plain `build.ps1` can never accidentally ship anything.
#
#  Full pipeline (all automatic, per chosen policy):
#    1. preflight  - tools present, gh authed, on main, tree clean (nothing outside
#                    exe_to_download\), the stamp a real date newer than every v* tag,
#                    the MSI version newer than the last published one, winget anchors intact
#    2. gate       - build.ps1 -Test, the full fdsec run, govulncheck (fail fast BEFORE tagging)
#    3. commit     - commit exe_to_download\ - and nothing else - if the build changed it
#    4. push       - push main, then create + push tag v<version>  <-- triggers CI
#    5. wait       - poll until the GitHub Release + all six assets exist
#    6. winget     - sync winget/*.yaml (version, URL, SHA256, date), CHECK them, commit, push
#    7. submit     - wingetcreate submit -> PR to microsoft/winget-pkgs
#    8. store      - build the MSIX for Microsoft Store (upload stays manual)
#    9. checklist  - print what is done, what failed and why, and the manual step left
#
#  Usage (from the repo root):
#    .\release.ps1                 # full release with version = now (yyMMddHHmm)
#    .\release.ps1 -Version 2606271600
#    .\release.ps1 -DryRun         # do everything EXCEPT push tag / submit / Store
#    .\release.ps1 -SkipStore      # skip the MSIX build (CLI + winget only)
#    .\release.ps1 -SkipWinget     # skip winget sync + submit
#    .\release.ps1 -WingetInstallTest   # also install the synced manifest here, then uninstall
#    .\release.ps1 -Resume -Version 2606271600 [-SkipStore]
#                                  # the tag is already on origin: skip steps 1-4 and carry
#                                  # on from the wait (after a failed winget or Store step, a
#                                  # CI re-dispatch, or a wait that timed out)
#
#  The winget manifests are checked twice by packaging\check-winget-manifests.ps1: in the
#  preflight (frozen anchors, offline - still no tag) and in step 6 against the synced files
#  (schema, version, published hash - before the commit and the winget-pkgs PR).
#
#  A channel that fails after the tag (winget sync, submit, the MSIX) does not stop the
#  others; the checklist marks it [ ] with the reason, and the script exits 1 (REL-03).
#
#  Store identity: read from msix\identity.json (SZA.FileDO, reserved in Partner Center) or
#  passed here. There is no placeholder default: without a reserved identity the MSIX step
#  refuses and says so. The package is pinned to this release's stamp, so the exe inside
#  prints the tag's version.
#    .\release.ps1 -StoreIdentityName "..." [-StorePublisher "CN=..."] [-StorePublisherDisplayName "..."]
# ============================================================================
[CmdletBinding()]
param(
    [string]$Version,
    [switch]$DryRun,
    # The tag v<Version> is already on origin: skip preflight-to-push and continue from the
    # wait for the GitHub Release (REL-02). Needs -Version.
    [switch]$Resume,
    [switch]$SkipStore,
    [switch]$SkipWinget,
    [string]$StoreIdentityName,
    [string]$StorePublisher,
    [string]$StorePublisherDisplayName,
    # Also install the synced manifest on this machine, run a portable shim and uninstall it
    # (packaging\check-winget-manifests.ps1 -Install). Off by default: it proves the zip layout
    # is real, but it writes into the operator's own profile.
    [switch]$WingetInstallTest,
    # How long to wait for the GitHub Release to appear after pushing the tag.
    [int]$WaitMinutes = 25
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
# StrictMode refuses to read an automatic variable no native command has set yet.
# Global, never script-scoped: a script-scoped copy would shadow every later exit code.
$global:LASTEXITCODE = 0
$root = $PSScriptRoot
$repo = "SerZhyAle/FileDO"
$script:inPreflight = $true
$storeFailure = $null
$wingetFailure = $null
if ($Resume -and -not $Version) {
    Write-Host "release-preflight: NOT VERIFIED (-Resume needs -Version <stamp> - the tag it resumes)" -ForegroundColor Yellow
    exit 2
}
if (-not $Version) { $Version = Get-Date -Format "yyMMddHHmm" }

# The six assets release.yml publishes. The site and winget read them by these names.
$assetNames = @(
    "FileDO-$Version-setup.exe", "FileDO-$Version-setup.exe.sha256",
    "FileDO-$Version-windows-x64.zip", "FileDO-$Version-windows-x64.zip.sha256",
    "FileDO-$Version-windows-x64.msi", "FileDO-$Version-windows-x64.msi.sha256")

function Fail([string]$msg, [int]$code = 1) {
    $subject = if ($script:inPreflight) { "release-preflight $Version" } else { "release $Version" }
    if ($code -eq 2) {
        Write-Host "${subject}: NOT VERIFIED ($msg)" -ForegroundColor Yellow
    } else {
        Write-Host "${subject}: FAIL ($msg)" -ForegroundColor Red
    }
    exit $code
}
function Step($n)   { Write-Host ""; Write-Host "== $n ==" -ForegroundColor Cyan }

# REL-16: every native command goes through one of these two, so no exit code is left
# unread. Invoke-Checked stops the release on a non-zero exit; Invoke-Native hands the code
# back to a caller that records the failure and carries on (the post-tag channels).
function Invoke-Checked([string]$Label, [scriptblock]$Block, [int]$FailCode = 1) {
    $nativeOut = & $Block
    if ($LASTEXITCODE -ne 0) {
        if ($nativeOut) { Write-Host ($nativeOut | Out-String) }
        Fail "$Label failed (exit $LASTEXITCODE)." $FailCode
    }
    # No output is no output: emitting $null would make @(...) a one-element array.
    if ($null -ne $nativeOut) { $nativeOut }
}
function Invoke-Native([scriptblock]$Block) {
    & $Block | Out-Host
    return $LASTEXITCODE
}

# REL-05: a stamp is a real date, not ten digits - month 13 must not become a tag.
function Get-StampDate([string]$stamp) {
    if ($stamp -notmatch '^\d{10}$') { return $null }
    try { return [datetime]::ParseExact($stamp, 'yyMMddHHmm', [Globalization.CultureInfo]::InvariantCulture) }
    catch { return $null }
}

# PKG-01: the MSI ProductVersion derived from a stamp - yy . M . ((d-1)*1440 + H*60 + m), the
# same arithmetic as build.ps1's Get-InstallerVersion and release.yml's "Resolve version".
function Get-InstallerVersion([datetime]$when) {
    [version]("{0}.{1}.{2}" -f ($when.Year % 100), $when.Month, (($when.Day - 1) * 1440 + $when.Hour * 60 + $when.Minute))
}

# The ProductVersion a published MSI really carries, read from its Property table. Read, not
# derived: the tags before PKG-01 used another mapping, and the arithmetic is exactly what
# this comparison must not have to trust.
function Get-MsiProductVersion([string]$msiPath) {
    $installer = New-Object -ComObject WindowsInstaller.Installer
    $db = $installer.OpenDatabase($msiPath, 0)   # 0 = read-only
    $view = $db.OpenView("SELECT ``Value`` FROM ``Property`` WHERE ``Property``='ProductVersion'")
    try {
        # [void]: a COM method without a result still writes a $null into the output.
        [void]$view.Execute()
        $record = $view.Fetch()
        if ($null -eq $record) { throw "$msiPath has no ProductVersion" }
        return [version]$record.StringData(1)
    } finally {
        [void]$view.Close()
        foreach ($com in @($view, $db, $installer)) { [void][Runtime.InteropServices.Marshal]::ReleaseComObject($com) }
    }
}

# Git status lines for anything outside the given top-level folders. Untracked files count:
# an unignored *.pfx at the root is exactly what must never reach a public commit (REL-01).
function Get-DirtyOutside([string[]]$allowed) {
    $lines = @(Invoke-Checked 'git status' { git status --porcelain --untracked-files=all } 2)
    @($lines | Where-Object {
        $path = $_.Substring(3).Trim('"')
        if ($path -match ' -> (.+)$') { $path = $Matches[1].Trim('"') }
        $inside = $false
        foreach ($a in $allowed) { if ($path.StartsWith("$a/", [StringComparison]::OrdinalIgnoreCase)) { $inside = $true } }
        -not $inside
    })
}

# ---------------------------------------------------------------------------
# 1. Preflight
# ---------------------------------------------------------------------------
Step "1/9 Preflight$(if ($Resume) { ' (-Resume)' })"

foreach ($t in @('git','gh','go')) {
    if (-not (Get-Command $t -ErrorAction SilentlyContinue)) { Fail "$t not found on PATH." 2 }
}
if (-not $SkipWinget -and -not (Get-Command wingetcreate -ErrorAction SilentlyContinue)) {
    Fail "wingetcreate not found. Install: winget install Microsoft.WingetCreate (or use -SkipWinget)." 2
}
if (-not $Resume -and -not (Get-Command govulncheck -ErrorAction SilentlyContinue)) {
    Fail "govulncheck not found. Install: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0" 2
}

gh auth status 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail "GitHub CLI is not authenticated. Run: gh auth login" 2 }

Push-Location $root
try {
    # Read by its output: a detached HEAD exits 1 and prints nothing, which is "not main" too.
    $branch = (git symbolic-ref --short -q HEAD)
    if ($branch -ne 'main') { Fail "Releases must be cut from 'main' (you are on '$branch')." }

    # Version + tag. REL-05: ten digits AND a real yyMMddHHmm date.
    $stampDate = Get-StampDate $Version
    if (-not $stampDate) { Fail "Version must be a real yyMMddHHmm date (10 digits); got '$Version'." }
    $tag = "v$Version"

    # REL-01: the tree is clean. The only thing a release commits is exe_to_download\ (the
    # binaries the gate rebuilds), so an uncommitted source edit or an unignored file - a
    # signing key at the root, a runtime list - stops the release here instead of riding into
    # the public "Build v<version>" commit. -Resume commits only winget\, which a failed
    # step 6 may have left rewritten.
    $allowedDirty = if ($Resume) { @('winget') } else { @('exe_to_download') }
    $dirty = @(Get-DirtyOutside $allowedDirty)
    if ($dirty.Count) {
        $dirty | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
        Fail "the working tree has $($dirty.Count) change(s) outside $($allowedDirty -join ', ')\ - commit, stash or ignore them first. Nothing tagged."
    }

    Write-Host "  repo    : $repo"
    Write-Host "  branch  : $branch"
    Write-Host "  version : $Version   (MSI $(Get-InstallerVersion $stampDate))"
    Write-Host "  tag     : $tag"
    if (-not $SkipStore -and -not $StoreIdentityName -and -not (Test-Path (Join-Path $root 'msix\identity.json'))) {
        Write-Host "  Store   : no reserved identity (msix\identity.json is missing, no -StoreIdentityName) - step 8 will not build the MSIX." -ForegroundColor Yellow
        Write-Host "            Restore msix\identity.json (SZA.FileDO), or pass -SkipStore." -ForegroundColor Yellow
    }
    if ($DryRun) { Write-Host "  MODE    : DRY RUN (no tag push, no submit, no Store upload)" -ForegroundColor Yellow }

    if ($Resume) {
        # REL-02: resuming is only honest when the tag really is public - otherwise the
        # release never started and there is nothing to resume.
        $remoteTag = Invoke-Checked 'git ls-remote' { git ls-remote --tags origin "refs/tags/$tag" } 2
        if (-not $remoteTag) { Fail "$tag is not on origin, so there is no release to resume. Run without -Resume." }
        Write-Host "  resume  : $tag is on origin - steps 2-4 are skipped." -ForegroundColor Yellow
    } else {
        Invoke-Checked 'git fetch --tags' { git fetch --tags --quiet } 2 | Out-Null
        $existing = Invoke-Checked 'git tag --list' { git tag --list $tag } 2
        if ($existing) { Fail "Tag $tag already exists. Pick a new version, or resume it with -Resume -Version $Version." }

        # REL-05: newer than every stamp tag. A typo, a wrong clock or a DST fall-back that
        # goes backwards would otherwise tag a release that downgrades winget, the MSIX and
        # the setup EXE.
        $stampTags = @(Invoke-Checked 'git tag --list' { git tag --list 'v*' } 2 | Where-Object { $_ -match '^v\d{10}$' } | Sort-Object)
        if ($stampTags.Count) {
            $highest = $stampTags[-1]
            if ([long]$Version -le [long]$highest.Substring(1)) {
                Fail "$tag is not newer than the highest release tag $highest - a release must never go backwards."
            }
            Write-Host "  newest  : $highest (this one is newer)"
        }

        # PKG-01: the MSI must upgrade over the last one published. Windows Installer compares
        # three fields, so the new triple has to be greater than the ProductVersion the latest
        # release's MSI actually carries - read from that MSI, not recomputed.
        $latestTag = (& gh release view --repo $repo --json tagName --jq .tagName 2>&1 | Out-String).Trim()
        $latestCode = $LASTEXITCODE
        if ($latestCode -ne 0 -and $latestTag -match 'release not found|HTTP 404') {
            Write-Host "  MSI     : no published release yet - nothing to upgrade over."
        } elseif ($latestCode -ne 0) {
            Fail "could not read the latest GitHub release to compare MSI versions: $latestTag" 2
        } else {
            $msiDir = Join-Path ([IO.Path]::GetTempPath()) "filedo-rel-msi-$Version"
            New-Item -ItemType Directory -Force -Path $msiDir | Out-Null
            try {
                Invoke-Checked "gh release download $latestTag" { gh release download $latestTag --repo $repo --pattern '*-windows-x64.msi' --dir $msiDir --clobber } 2 | Out-Null
                $lastMsi = @(Get-ChildItem $msiDir -Filter '*-windows-x64.msi')
                if ($lastMsi.Count -ne 1) { Fail "the $latestTag release carries $($lastMsi.Count) '*-windows-x64.msi' assets, expected 1." 2 }
                try { $lastVersion = Get-MsiProductVersion $lastMsi[0].FullName } catch { Fail "could not read the ProductVersion of $($lastMsi[0].Name): $($_.Exception.Message)" 2 }
                $lastTriple = [version]("{0}.{1}.{2}" -f $lastVersion.Major, $lastVersion.Minor, [Math]::Max(0, $lastVersion.Build))
                $newTriple = Get-InstallerVersion $stampDate
                if ($newTriple -le $lastTriple) {
                    Fail "the MSI version $newTriple is not greater than $lastTriple, which $latestTag's MSI carries ($lastVersion) - Windows Installer would not upgrade it."
                }
                Write-Host "  MSI     : $newTriple > $lastTriple ($latestTag, $lastVersion) - upgrades"
            } finally {
                Remove-Item $msiDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }

    # The frozen anchors of the winget manifests, read before anything irreversible happens.
    # Step 6 re-runs these against the SYNCED manifests, but a broken PackageIdentifier or a
    # renamed PortableCommandAlias is already broken here, and here there is still no tag.
    # Offline and version-free on purpose: winget\ still carries the previous release.
    if (-not $SkipWinget -and -not $Resume) {
        & "$root\packaging\check-winget-manifests.ps1" -NoNetwork
        $manifestCode = $LASTEXITCODE
        if ($manifestCode -ne 0) { Fail "winget manifest checks did not pass on the current winget\ - nothing tagged." $manifestCode }
    }

    if ($Resume) {
        Write-Host "release-preflight ${Version}: PASS (resume)" -ForegroundColor Green
        $script:inPreflight = $false
        Step "2-4/9 Gate, commit, push"
        Write-Host "  skipped (-Resume: $tag is already on origin)."
    } else {
    # -----------------------------------------------------------------------
    # 2. Build + test gate (fail BEFORE we tag anything)
    # -----------------------------------------------------------------------
    Step "2/9 Build + test gate"
    # build.ps1 builds the installer on any plain run; -Msi is what makes a
    # missing WiX exit 2 instead of a printed skip. For a release that
    # distinction matters: the installer is what most users actually get, and a
    # wxs that does not compile must surface here and not inside the tagged CI
    # run, after the one irreversible step. On a machine with no WiX the gate
    # still runs - the workflow builds the real artifacts on the runner.
    if (Get-Command wix -ErrorAction SilentlyContinue) {
        & "$root\build.ps1" -Test -Msi -Version $Version
        $gateCode = $LASTEXITCODE
        if ($gateCode -ne 0) { Fail "build.ps1 -Test -Msi did not pass. Nothing tagged." $gateCode }
    } else {
        & "$root\build.ps1" -Test -Version $Version
        $gateCode = $LASTEXITCODE
        if ($gateCode -ne 0) { Fail "build.ps1 -Test did not pass. Nothing tagged." $gateCode }
        Write-Host "  note: WiX is not installed here, so the MSI was not built locally." -ForegroundColor Yellow
        Write-Host "        dotnet tool install --global wix --version 5.0.2" -ForegroundColor Yellow
    }
    # The full round trip, as the architecture that ships (REL-04): build.ps1 puts its own
    # Go environment back when it ends, so this run sets windows/amd64 again.
    Write-Host "go test ./fdsec/ (full release run, windows/amd64) ..." -NoNewline
    $savedArch = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
    $env:GOOS = 'windows'; $env:GOARCH = 'amd64'; $env:CGO_ENABLED = '0'
    try {
        $fullFdsec = go test ./fdsec/ -count=1 2>&1 | Out-String
        $fullCode = $LASTEXITCODE
    } finally {
        foreach ($k in @($savedArch.Keys)) { [Environment]::SetEnvironmentVariable($k, $savedArch[$k], 'Process') }
    }
    if ($fullCode -ne 0) {
        Write-Host " FAILED" -ForegroundColor Red
        Write-Host $fullFdsec
        Fail "the full fdsec release run found a defect. Nothing tagged."
    }
    Write-Host " OK"

    # CI-03: release.yml refuses to publish a build in which govulncheck finds reachable
    # vulnerable code - but that runs after the tag. The same scan runs here first, on the
    # executables the gate just built the way the workflow builds them.
    $gvc = Get-Command govulncheck -ErrorAction SilentlyContinue
    if (-not $gvc) {
        Fail "govulncheck is not on PATH (go install golang.org/x/vuln/cmd/govulncheck@v1.8.0); the workflow runs it after the tag, so it must pass here first." 2
    }
    foreach ($exe in 'filedo.exe', 'filedo_fill.exe', 'filedo_check.exe', 'filedo_test.exe') {
        Write-Host "govulncheck -mode=binary $exe ..." -NoNewline
        $vulnOut = & $gvc.Source -mode=binary (Join-Path $root "exe_to_download\$exe") 2>&1 | Out-String
        $vulnCode = $LASTEXITCODE
        if ($vulnCode -eq 3) {
            Write-Host " VULNERABLE" -ForegroundColor Red
            Write-Host $vulnOut
            Fail "govulncheck: $exe links reachable vulnerable code - the workflow would refuse to publish it. Bump the module (or the toolchain pin) first. Nothing tagged."
        } elseif ($vulnCode -ne 0) {
            Write-Host " NOT VERIFIED" -ForegroundColor Yellow
            Fail "govulncheck could not scan $exe (exit $vulnCode): $($vulnOut.Trim())" 2
        }
        Write-Host " OK"
    }

    # The gate built from the tree the preflight saw. Anything it changed outside
    # exe_to_download\ would make the tagged commit differ from what was tested.
    $dirtyAfter = @(Get-DirtyOutside @('exe_to_download'))
    if ($dirtyAfter.Count) {
        $dirtyAfter | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
        Fail "the gate changed $($dirtyAfter.Count) file(s) outside exe_to_download\ - the tested tree is not the one that would be tagged. Nothing tagged."
    }
    Write-Host "release-preflight ${Version}: PASS" -ForegroundColor Green
    $script:inPreflight = $false

    # -----------------------------------------------------------------------
    # 3. Commit refreshed artifacts (binaries are tracked in exe_to_download\)
    # -----------------------------------------------------------------------
    Step "3/9 Commit"
    # REL-01: exe_to_download\ and nothing else. A dry run does not touch the index at all.
    if ($DryRun) {
        $wouldCommit = @(Invoke-Checked 'git status' { git status --porcelain --untracked-files=all -- exe_to_download } 2)
        if ($wouldCommit.Count) {
            Write-Host "  [dry-run] would commit (exe_to_download\ only):" -ForegroundColor Yellow
            $wouldCommit | ForEach-Object { Write-Host "      $_" }
        } else {
            Write-Host "  nothing to commit (tree already matches build output)."
        }
    } else {
        Invoke-Checked 'git add exe_to_download' { git add -- exe_to_download } | Out-Null
        $staged = Invoke-Checked 'git diff --cached' { git diff --cached --name-only }
        if ($staged) {
            Invoke-Checked "git commit" { git commit -m "Build $tag" }
            Write-Host "  committed refreshed artifacts."
        } else {
            Write-Host "  nothing to commit (tree already matches build output)."
        }
    }

    # -----------------------------------------------------------------------
    # 4. Push main + tag  (the tag push is what triggers release.yml)
    # -----------------------------------------------------------------------
    Step "4/9 Push main + tag"
    if ($DryRun) {
        Write-Host "  [dry-run] would: git push origin main" -ForegroundColor Yellow
        Write-Host "  [dry-run] would: git tag $tag; git push origin $tag" -ForegroundColor Yellow
        Write-Host ""
        Write-Host "Dry run complete. To release for real, re-run without -DryRun." -ForegroundColor Yellow
        exit 0
    }
    Invoke-Checked 'git push origin main' { git push origin main }
    Invoke-Checked "git tag $tag" { git tag $tag }
    Invoke-Checked "git push origin $tag" { git push origin $tag }
    Write-Host "  pushed $tag -> GitHub Actions release is now building."
    }

    # -----------------------------------------------------------------------
    # 5. Wait for the Release + assets to appear
    # -----------------------------------------------------------------------
    Step "5/9 Wait for GitHub Release"
    $zipName = "FileDO-$Version-windows-x64.zip"
    $deadline = (Get-Date).AddMinutes($WaitMinutes)
    $ready = $false
    $missingAssets = $assetNames
    while ((Get-Date) -lt $deadline) {
        # The poll reads the exit code itself: "no release yet" is the expected answer
        # for the first minutes, not a failure.
        $published = @(gh release view $tag --repo $repo --json assets --jq '.assets[].name' 2>$null)
        if ($LASTEXITCODE -eq 0) {
            $missingAssets = @($assetNames | Where-Object { $published -notcontains $_ })
            if ($missingAssets.Count -eq 0) { $ready = $true; break }
        }
        Write-Host "  ...waiting for $tag assets (CI ~10 min)"
        Start-Sleep -Seconds 20
    }
    if (-not $ready) {
        Fail "Release $tag is not complete after $WaitMinutes min (missing: $($missingAssets -join ', ')). Check: gh run list --repo $repo - then re-run with -Resume -Version $Version."
    }
    Write-Host "  Release $tag is live with all $($assetNames.Count) assets."
    if ($DryRun) {
        # Only a resumed dry run gets here; the release exists, and nothing below is read-only.
        Write-Host "  [dry-run] would: sync + check winget\, commit, push, wingetcreate submit, build the MSIX" -ForegroundColor Yellow
        Write-Host "Dry run complete." -ForegroundColor Yellow
        exit 0
    }

    # -----------------------------------------------------------------------
    # 6. Sync winget/*.yaml
    # -----------------------------------------------------------------------
    # From here on the tag is public. A failing channel is recorded and reported, never
    # silently passed over, and never allowed to stop the channels after it (REL-03).
    if (-not $SkipWinget) {
        Step "6/9 Sync winget manifests"
        $tmp = Join-Path $env:TEMP "filedo-rel-$Version"
        New-Item -ItemType Directory -Force -Path $tmp | Out-Null
        $wd = Join-Path $root "winget"
        $dlCode = Invoke-Native { gh release download $tag --repo $repo --pattern "$zipName.sha256" --dir $tmp --clobber }
        if ($dlCode -ne 0) {
            $wingetFailure = "could not download $zipName.sha256 (gh exit $dlCode)"
        } else {
            $sha = ((Get-Content (Join-Path $tmp "$zipName.sha256") -Raw).Trim() -split '\s+')[0].ToUpper()
            Write-Host "  SHA256: $sha"

            $url  = "https://github.com/$repo/releases/download/$tag/$zipName"
            $date = Get-Date -Format "yyyy-MM-dd"

            # version bump in all three manifests
            foreach ($f in 'SerZhyAle.FileDO.yaml','SerZhyAle.FileDO.locale.en-US.yaml','SerZhyAle.FileDO.installer.yaml') {
                $p = Join-Path $wd $f
                $c = Get-Content $p -Raw
                $c = $c -replace 'PackageVersion:\s*"\d{10}"', "PackageVersion: `"$Version`""
                Set-Content $p $c -NoNewline -Encoding UTF8
            }
            # installer: url + sha + release date
            $pi = Join-Path $wd 'SerZhyAle.FileDO.installer.yaml'
            $ci = Get-Content $pi -Raw
            $ci = $ci -replace 'InstallerUrl:\s*\S+',    "InstallerUrl: $url"
            $ci = $ci -replace 'InstallerSha256:\s*\S+', "InstallerSha256: $sha"
            $ci = $ci -replace 'ReleaseDate:\s*\S+',     "ReleaseDate: $date"
            Set-Content $pi $ci -NoNewline -Encoding UTF8
            # locale: release notes url -> this tag
            $pl = Join-Path $wd 'SerZhyAle.FileDO.locale.en-US.yaml'
            $cl = Get-Content $pl -Raw
            $cl = $cl -replace 'ReleaseNotesUrl:\s*\S+', "ReleaseNotesUrl: https://github.com/$repo/releases/tag/$tag"
            Set-Content $pl $cl -NoNewline -Encoding UTF8

            # Gate the synced manifests BEFORE committing them. microsoft/winget-pkgs re-validates
            # every PR in its own CI, which is after the tag is already pushed - the one place a
            # schema slip or a broken frozen anchor must not first surface. The install test is
            # opt-in because it writes to this machine's profile.
            $checkArgs = @{ Version = $Version }
            if ($WingetInstallTest) { $checkArgs['Install'] = $true }
            & "$root\packaging\check-winget-manifests.ps1" @checkArgs
            $manifestCode = $LASTEXITCODE
            if ($manifestCode -ne 0) {
                $wingetFailure = "the synced manifests did not pass their checks (exit $manifestCode) - winget\ is rewritten but not committed"
            }
        }

        if (-not $wingetFailure) {
            $addCode = Invoke-Native { git add -- winget }
            if ($addCode -ne 0) { $wingetFailure = "git add winget failed (exit $addCode)" }
        }
        if (-not $wingetFailure) {
            $stagedWinget = @(git diff --cached --name-only -- winget)
            if ($LASTEXITCODE -ne 0) {
                $wingetFailure = "git diff --cached failed (exit $LASTEXITCODE)"
            } elseif ($stagedWinget.Count) {
                $commitCode = Invoke-Native { git commit -m "Sync winget/ to $tag" }
                if ($commitCode -ne 0) {
                    $wingetFailure = "git commit of winget\ failed (exit $commitCode)"
                } else {
                    $pushCode = Invoke-Native { git push origin main }
                    if ($pushCode -ne 0) {
                        # Most often main moved on origin. The commit is local; pull and resume.
                        $wingetFailure = "git push origin main failed (exit $pushCode) - run: git pull --rebase origin main, then -Resume"
                    } else {
                        Write-Host "  winget/ synced and pushed."
                    }
                }
            } else {
                Write-Host "  winget/ already carries $tag."
            }
        }

        # -------------------------------------------------------------------
        # 7. Submit PR to microsoft/winget-pkgs
        # -------------------------------------------------------------------
        Step "7/9 winget submit"
        if ($wingetFailure) {
            Write-Host "  not submitted: $wingetFailure" -ForegroundColor Yellow
        } else {
            # REL-09: the token reaches wingetcreate through its environment variable, for this
            # one call - never on a command line, where the process list and audit logs see it.
            $token = (& gh auth token 2>$null | Out-String).Trim()
            if ($LASTEXITCODE -ne 0 -or -not $token) {
                $wingetFailure = "gh auth token returned no token - wingetcreate was not run"
            } else {
                $env:WINGET_CREATE_GITHUB_TOKEN = $token
                try {
                    $submitCode = Invoke-Native { wingetcreate submit $wd }
                } finally {
                    Remove-Item Env:WINGET_CREATE_GITHUB_TOKEN -ErrorAction SilentlyContinue
                    $token = $null
                }
                if ($submitCode -ne 0) {
                    $wingetFailure = "wingetcreate submit failed (exit $submitCode)"
                } else {
                    Write-Host "  winget-pkgs PR submitted."
                }
            }
        }
        if ($wingetFailure) { Write-Host "  winget FAILED: $wingetFailure" -ForegroundColor Red }
    } else {
        Step "6-7/9 winget"; Write-Host "  skipped (-SkipWinget)."
    }

    # -----------------------------------------------------------------------
    # 8. Microsoft Store (MSIX build; upload is manual - no Partner Center CLI)
    # -----------------------------------------------------------------------
    if (-not $SkipStore) {
        Step "8/9 Store MSIX"
        # Pinned to this release's stamp (the tag without the v), so the exe inside the package
        # prints the version the release carries. build-msix.ps1 refuses without a reserved
        # identity; by now the tag is out, so a refusal is recorded and reported, not fatal here.
        $msixArgs = @{ Stamp = $Version }
        if ($StoreIdentityName)        { $msixArgs['IdentityName'] = $StoreIdentityName }
        if ($StorePublisher)           { $msixArgs['Publisher'] = $StorePublisher }
        if ($StorePublisherDisplayName){ $msixArgs['PublisherDisplayName'] = $StorePublisherDisplayName }
        try {
            & "$root\msix\build-msix.ps1" @msixArgs
            if ($LASTEXITCODE -ne 0) { throw "build-msix.ps1 exited $LASTEXITCODE" }
        } catch {
            $storeFailure = "$($_.Exception.Message)".Trim()
            Write-Host "  MSIX build did NOT produce a Store package:" -ForegroundColor Yellow
            Write-Host "    $storeFailure" -ForegroundColor Yellow
        }
    } else {
        Step "8/9 Store MSIX"; Write-Host "  skipped (-SkipStore)."
    }

    # -----------------------------------------------------------------------
    # 9. Final checklist
    # -----------------------------------------------------------------------
    Step "9/9 Done"
    $wTxt = if ($SkipWinget) { "skipped" } elseif ($wingetFailure) { "FAILED - $wingetFailure" } else { "synced + PR submitted" }
    $sTxt = if ($SkipStore) { "skipped" } elseif ($storeFailure) { "NOT BUILT - $storeFailure" } else { "built into msix\out\ (unsigned, stamp $Version)" }
    Write-Host "  [x] GitHub Release : https://github.com/$repo/releases/tag/$tag"
    Write-Host "  [$(if ($wingetFailure -or $SkipWinget) { ' ' } else { 'x' })] winget/ : $wTxt"
    Write-Host "  [$(if ($storeFailure -or $SkipStore) { ' ' } else { 'x' })] Store MSIX : $sTxt"
    Write-Host ""
    if ($wingetFailure) {
        Write-Host "  RECOVER winget (the tag and the GitHub Release stand; do not cut a new version):" -ForegroundColor Yellow
        Write-Host "    .\release.ps1 -Resume -Version $Version -SkipStore" -ForegroundColor Yellow
        Write-Host "  or by hand - RELEASE.md, 'If something fails mid-release'." -ForegroundColor Yellow
    }
    if ($storeFailure) {
        Write-Host "  RECOVER Store: fix the cause, then .\msix\build-msix.ps1 -Stamp $Version" -ForegroundColor Yellow
    }
    if (-not $SkipStore -and -not $storeFailure) {
        Write-Host "  MANUAL (no CLI exists), in this order - msix\README.md sections 4-6:" -ForegroundColor Yellow
        Write-Host "    1. Partner Center > Create new submission > Packages: upload msix\out\FileDO_*.msix" -ForegroundColor Yellow
        Write-Host "    2. Store listings: export, .\msix\build-store-listing-csv.ps1 -Refresh ReleaseNotes, import msix\out\store-import" -ForegroundColor Yellow
        Write-Host "       (ReleaseNotes come from msix\listing\<code>.txt: write this release's notes there first)" -ForegroundColor Yellow
        Write-Host "    3. Submit, then update over a real prior install and check the version and the notes." -ForegroundColor Yellow
    }
    Write-Host "  Then verify: winget show SerZhyAle.FileDO   (after the winget-pkgs PR merges)"
    $failedChannels = @()
    if ($wingetFailure) { $failedChannels += 'winget' }
    if ($storeFailure)  { $failedChannels += 'Store MSIX' }
    if ($failedChannels.Count) {
        Write-Host "release ${Version}: FAIL (published to GitHub; not completed: $($failedChannels -join ', '))" -ForegroundColor Red
        exit 1
    }
    Write-Host "release ${Version}: PASS" -ForegroundColor Green
    exit 0
}
catch {
    # A terminating error nobody expected still ends in a verdict and a non-zero exit code,
    # never in a stale $LASTEXITCODE that reads as success to whoever called this script.
    Fail "unexpected error at line $($_.InvocationInfo.ScriptLineNumber): $($_.Exception.Message)" 2
}
finally {
    Pop-Location
}
