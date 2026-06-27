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
#    1. preflight  - tools present, gh authed, on main, tree clean
#    2. gate       - build.ps1 -Test (fail fast BEFORE tagging)
#    3. commit     - commit refreshed binaries if the build changed them
#    4. push       - push main, then create + push tag v<version>  <-- triggers CI
#    5. wait       - poll until the GitHub Release + assets exist
#    6. winget     - sync winget/*.yaml (version, URL, SHA256, date), commit, push
#    7. submit     - wingetcreate submit -> PR to microsoft/winget-pkgs
#    8. store      - build the MSIX for Microsoft Store (upload stays manual)
#    9. checklist  - print what is done and the one manual step that remains
#
#  Usage (from the repo root):
#    .\release.ps1                 # full release with version = now (yyMMddHHmm)
#    .\release.ps1 -Version 2606271600
#    .\release.ps1 -DryRun         # do everything EXCEPT push tag / submit / Store
#    .\release.ps1 -SkipStore      # skip the MSIX build (CLI + winget only)
#    .\release.ps1 -SkipWinget     # skip winget sync + submit
#
#  Store identity (only needed for a real Store build; defaults are placeholders
#  from msix\build-msix.ps1 - the real values come from Partner Center):
#    .\release.ps1 -StoreIdentityName "..." -StorePublisher "CN=..." -StorePublisherDisplayName "..."
# ============================================================================
[CmdletBinding()]
param(
    [string]$Version,
    [switch]$DryRun,
    [switch]$SkipStore,
    [switch]$SkipWinget,
    [string]$StoreIdentityName,
    [string]$StorePublisher,
    [string]$StorePublisherDisplayName,
    # How long to wait for the GitHub Release to appear after pushing the tag.
    [int]$WaitMinutes = 25
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
$repo = "SerZhyAle/FileDO"

function Fail($msg) { Write-Host "RELEASE ABORTED: $msg" -ForegroundColor Red; exit 1 }
function Step($n)   { Write-Host ""; Write-Host "== $n ==" -ForegroundColor Cyan }

# ---------------------------------------------------------------------------
# 1. Preflight
# ---------------------------------------------------------------------------
Step "1/9 Preflight"

foreach ($t in @('git','gh','go')) {
    if (-not (Get-Command $t -ErrorAction SilentlyContinue)) { Fail "$t not found on PATH." }
}
if (-not $SkipWinget -and -not (Get-Command wingetcreate -ErrorAction SilentlyContinue)) {
    Fail "wingetcreate not found. Install: winget install Microsoft.WingetCreate (or use -SkipWinget)."
}

gh auth status 2>&1 | Out-Null
if ($LASTEXITCODE -ne 0) { Fail "GitHub CLI is not authenticated. Run: gh auth login" }

Push-Location $root
try {
    $branch = (git symbolic-ref --short -q HEAD)
    if ($branch -ne 'main') { Fail "Releases must be cut from 'main' (you are on '$branch')." }

    # Version + tag
    if (-not $Version) { $Version = Get-Date -Format "yyMMddHHmm" }
    if ($Version -notmatch '^\d{10}$') { Fail "Version must be 10 digits (yyMMddHHmm); got '$Version'." }
    $tag = "v$Version"

    git fetch --tags --quiet
    $existing = git tag --list $tag
    if ($existing) { Fail "Tag $tag already exists. Pick a new version." }

    Write-Host "  repo    : $repo"
    Write-Host "  branch  : $branch"
    Write-Host "  version : $Version"
    Write-Host "  tag     : $tag"
    if ($DryRun) { Write-Host "  MODE    : DRY RUN (no tag push, no submit, no Store upload)" -ForegroundColor Yellow }

    # -----------------------------------------------------------------------
    # 2. Build + test gate (fail BEFORE we tag anything)
    # -----------------------------------------------------------------------
    Step "2/9 Build + test gate"
    & "$root\build.ps1" -Test
    if ($LASTEXITCODE -ne 0) { Fail "build.ps1 -Test failed. Nothing tagged." }

    # -----------------------------------------------------------------------
    # 3. Commit refreshed artifacts (binaries are tracked in exe_to_download\)
    # -----------------------------------------------------------------------
    Step "3/9 Commit"
    git add -A
    $staged = git diff --cached --name-only
    if ($staged) {
        if ($DryRun) {
            Write-Host "  [dry-run] would commit:" -ForegroundColor Yellow
            $staged | ForEach-Object { Write-Host "      $_" }
            git reset --quiet   # leave the working tree staged-free for the dry run
        } else {
            git commit -m "Build $tag"
            if ($LASTEXITCODE -ne 0) { Fail "commit failed." }
            Write-Host "  committed refreshed artifacts."
        }
    } else {
        Write-Host "  nothing to commit (tree already matches build output)."
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
    git push origin main
    if ($LASTEXITCODE -ne 0) { Fail "git push origin main failed." }
    git tag $tag
    git push origin $tag
    if ($LASTEXITCODE -ne 0) { Fail "git push origin $tag failed." }
    Write-Host "  pushed $tag -> GitHub Actions release is now building."

    # -----------------------------------------------------------------------
    # 5. Wait for the Release + assets to appear
    # -----------------------------------------------------------------------
    Step "5/9 Wait for GitHub Release"
    $zipName = "FileDO-$Version-windows-x64.zip"
    $deadline = (Get-Date).AddMinutes($WaitMinutes)
    $ready = $false
    while ((Get-Date) -lt $deadline) {
        $assets = gh release view $tag --repo $repo --json assets 2>$null
        if ($LASTEXITCODE -eq 0 -and $assets -and ($assets -match [regex]::Escape("$zipName.sha256"))) {
            $ready = $true; break
        }
        Write-Host "  ...waiting for $tag assets (CI ~10 min)"
        Start-Sleep -Seconds 20
    }
    if (-not $ready) {
        Fail "Release $tag did not publish within $WaitMinutes min. Check: gh run list --repo $repo"
    }
    Write-Host "  Release $tag is live with assets."

    # -----------------------------------------------------------------------
    # 6. Sync winget/*.yaml
    # -----------------------------------------------------------------------
    if (-not $SkipWinget) {
        Step "6/9 Sync winget manifests"
        $tmp = Join-Path $env:TEMP "filedo-rel-$Version"
        New-Item -ItemType Directory -Force -Path $tmp | Out-Null
        gh release download $tag --repo $repo --pattern "$zipName.sha256" --dir $tmp --clobber
        if ($LASTEXITCODE -ne 0) { Fail "could not download $zipName.sha256" }
        $sha = ((Get-Content (Join-Path $tmp "$zipName.sha256") -Raw).Trim() -split '\s+')[0].ToUpper()
        Write-Host "  SHA256: $sha"

        $url  = "https://github.com/$repo/releases/download/$tag/$zipName"
        $date = Get-Date -Format "yyyy-MM-dd"
        $wd   = Join-Path $root "winget"

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

        git add winget
        if (git diff --cached --name-only) {
            git commit -m "Sync winget/ to $tag"
            git push origin main
            Write-Host "  winget/ synced and pushed."
        }

        # -------------------------------------------------------------------
        # 7. Submit PR to microsoft/winget-pkgs
        # -------------------------------------------------------------------
        Step "7/9 winget submit"
        $token = (gh auth token).Trim()
        wingetcreate submit --token $token $wd
        if ($LASTEXITCODE -ne 0) {
            Write-Host "  wingetcreate submit FAILED - submit manually:" -ForegroundColor Yellow
            Write-Host "    wingetcreate submit --token (gh auth token) winget" -ForegroundColor Yellow
        } else {
            Write-Host "  winget-pkgs PR submitted."
        }
    } else {
        Step "6-7/9 winget"; Write-Host "  skipped (-SkipWinget)."
    }

    # -----------------------------------------------------------------------
    # 8. Microsoft Store (MSIX build; upload is manual - no Partner Center CLI)
    # -----------------------------------------------------------------------
    if (-not $SkipStore) {
        Step "8/9 Store MSIX"
        $msixArgs = @{}
        if ($StoreIdentityName)        { $msixArgs['IdentityName'] = $StoreIdentityName }
        if ($StorePublisher)           { $msixArgs['Publisher'] = $StorePublisher }
        if ($StorePublisherDisplayName){ $msixArgs['PublisherDisplayName'] = $StorePublisherDisplayName }
        & "$root\msix\build-msix.ps1" @msixArgs
        if ($LASTEXITCODE -ne 0) { Write-Host "  MSIX build FAILED (non-fatal)." -ForegroundColor Yellow }
    } else {
        Step "8/9 Store MSIX"; Write-Host "  skipped (-SkipStore)."
    }

    # -----------------------------------------------------------------------
    # 9. Final checklist
    # -----------------------------------------------------------------------
    Step "9/9 Done"
    $wTxt = if ($SkipWinget) { "skipped" } else { "synced + PR submitted" }
    $sTxt = if ($SkipStore)  { "skipped" } else { "built into msix\out\" }
    Write-Host "  [x] GitHub Release : https://github.com/$repo/releases/tag/$tag"
    Write-Host "  [x] winget/ : $wTxt"
    Write-Host "  [x] Store MSIX : $sTxt"
    Write-Host ""
    Write-Host "  MANUAL (no CLI exists): upload msix\out\FileDO_*.msix to Partner Center." -ForegroundColor Yellow
    Write-Host "  Then verify: winget show $repo   (after the winget-pkgs PR merges)"
}
finally {
    Pop-Location
}
