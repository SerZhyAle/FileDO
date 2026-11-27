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
#                    exe_to_download\ and docs\releases\), the stamp a real date newer than every v* tag,
#                    the MSI version newer than the last published one, winget anchors intact
#    2. gate       - build.ps1 -Test, the full fdsec run, govulncheck, the operations run
#                    (every verb on small data, timed and checked), and the in-app help
#                    freshness check (check-help.ps1 -Exe) - fail fast BEFORE tagging
#    3. commit     - commit exe_to_download\ and the ledger docs\releases\ - and nothing else - if the build changed them
#    4. push       - push main, then create + push tag v<version>  <-- triggers CI
#    5. wait       - poll until the GitHub Release + all six assets exist
#    6. winget     - sync winget/*.yaml (version, URL, SHA256, date), CHECK them, commit, push
#    7. submit     - wingetcreate submit -> PR to microsoft/winget-pkgs
#   7b. scoop      - bring bucket/filedo.json in the own Scoop bucket (SerZhyAle/scoop-bucket) to this
#                    release: SHA256 from the .sha256 file and GitHub's digest, one commit, read back
#                    (packaging\update-scoop-bucket.ps1)
#    8. store      - build the MSIX for Microsoft Store (upload stays manual), only from
#                    sources equal to the tag (HEAD = tag outside winget\, tree clean)
#    9. site       - write packaging\release-manifest.json from the published release,
#                    render the five landing pages, check external-docs, commit, push
#   10. checklist  - print what is done, what failed and why, and the manual step left
#
#  After step 9 the SP-0167 dynamic audit stand fixes the tagged build (-LockBuild) and runs its self-test and control run
#  (PLAN\SP-0167 dynamic-audit-five-properties\dynaudit.ps1 -SelfTest, -Control); the checklist
#  reports both verdicts for the owner to read. It never fails a release; -SkipDynamicAudit skips it.
#
#  The release ledger (RELEASE-LEDGER 0.1, docs\releases\): the gate's build records itself as a release
#  build, and each channel step records its state through packaging\release-ledger.ps1 (GitHub and the
#  installer live at step 5, winget submitted at step 7, the MSIX pending at step 8); what is written after
#  the tag is committed apart as "Ledger: v<stamp>" before the site sync. The ledger never fails a release.
#
#  Usage (from the repo root):
#    .\release.ps1                 # full release with version = now (yyMMddHHmm)
#    .\release.ps1 -Version 2606271600
#    .\release.ps1 -DryRun         # do everything EXCEPT push tag / submit / Store
#    .\release.ps1 -SkipStore      # skip the MSIX build (CLI + winget only)
#    .\release.ps1 -SkipWinget     # skip winget sync + submit
#    .\release.ps1 -SkipScoop      # skip the Scoop bucket update (step 7b)
#    .\release.ps1 -SkipOpsMount   # operations run without its mounted-disk tier (no UAC prompt)
#    .\release.ps1 -OpsRebaseline  # accept this operations run as the new baseline (an intended
#                                  # change of behaviour or speed that its comparison would fail)
#    .\release.ps1 -WingetInstallTest   # also install the synced manifest here, then uninstall
#    .\release.ps1 -SkipDynamicAudit    # leave out the SP-0167 audit stand's self-test and control run
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
#
#  Store preflight (before the tag; not with -SkipStore or -Resume): one read-only look at the live
#  submission through the sza plugin's shared Store-Preflight.ps1 (WINDOWS-STORE 0.4 section D). A
#  refusal stops the release before the tag; an unavailable channel prints one manual line and the
#  release goes on. -StoreCredentialExpiry yyyy-MM-dd (the owner's expiry list) adds the expiry check.
#  Not tested there: rule 22, rule 23 (c), (e) and (f). Evidence: temp\evidence\store-preflight.log.
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
    # Leave the own Scoop bucket (SerZhyAle/scoop-bucket) at the release it carries (step 7b).
    [switch]$SkipScoop,
    # Leave the mounted-virtual-disk tier out of the operations run (it needs administrator rights).
    [switch]$SkipOpsMount,
    # The operations run compares itself with the last passing runs (function and speed); this
    # accepts what it shows as the new baseline instead of failing on an intended change.
    [switch]$OpsRebaseline,
    [string]$StoreIdentityName,
    [string]$StorePublisher,
    [string]$StorePublisherDisplayName,
    # The credential's expiry date from the owner's list (yyyy-MM-dd). Without it the Store
    # preflight does not check the expiry.
    [string]$StoreCredentialExpiry,
    # Also install the synced manifest on this machine, run a portable shim and uninstall it
    # (packaging\check-winget-manifests.ps1 -Install). Off by default: it proves the zip layout
    # is real, but it writes into the operator's own profile.
    [switch]$WingetInstallTest,
    # Leave out the SP-0167 dynamic audit stand (its self-test and control run after the site step).
    [switch]$SkipDynamicAudit,
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
$scoopFailure = $null
$siteFailure = $null
if ($Resume -and -not $Version) {
    Write-Host "release-preflight: COULD NOT VERIFY (-Resume needs -Version <stamp> - the tag it resumes)" -ForegroundColor Yellow
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
        Write-Host "${subject}: COULD NOT VERIFY ($msg)" -ForegroundColor Yellow
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

# RELEASE-LEDGER 0.1 (RELEASE_AND_DISTRIBUTION section 10): each channel step is recorded in docs\releases\history.jsonl
# through packaging\release-ledger.ps1, at the moment it happens. The ledger never fails a release: a non-zero exit
# or a throw is one yellow line, and the exit code the caller read before is left as it was.
function Write-Ledger([string[]]$LedgerArgs) {
    $savedExit = if (Test-Path variable:global:LASTEXITCODE) { $global:LASTEXITCODE } else { 0 }
    try {
        $ledgerOut = @(& pwsh -NoProfile -File (Join-Path $root 'packaging\release-ledger.ps1') @LedgerArgs 2>&1)
        if ($LASTEXITCODE -eq 0) { Write-Host "  ledger  : $($ledgerOut | Select-Object -Last 1)" }
        else { Write-Host "  ledger  : not recorded (exit $LASTEXITCODE): $($ledgerOut | Select-Object -Last 1)" -ForegroundColor Yellow }
    } catch {
        Write-Host "  ledger  : not recorded ($($_.Exception.Message))" -ForegroundColor Yellow
    } finally {
        $global:LASTEXITCODE = $savedExit
    }
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

# Shared sza plugin tool lookup: prefers $env:CLAUDE_PLUGIN_ROOT, then the newest cached sza
# plugin copy under ~/.claude/plugins/cache. $null when none is installed.
function Get-SzaPluginTool([string]$relativePath) {
    if ($env:CLAUDE_PLUGIN_ROOT) {
        $inEnv = Join-Path $env:CLAUDE_PLUGIN_ROOT $relativePath
        if (Test-Path -LiteralPath $inEnv) { return $inEnv }
    }
    $cache = Join-Path $env:USERPROFILE '.claude\plugins\cache\sza-unified-rules\sza'
    $newest = Get-ChildItem -LiteralPath $cache -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^\d+(\.\d+)+$' } |
        Sort-Object { [version]$_.Name } | Select-Object -Last 1
    if (-not $newest) { return $null }
    $found = Join-Path $newest.FullName $relativePath
    if (Test-Path -LiteralPath $found) { return $found }
    return $null
}

# Store preflight: the sza plugin's shared read-only check (WINDOWS-STORE 0.4 section D). This finds
# the newest installed copy and does not duplicate the check. $null when none is installed.
function Get-StorePreflightScript {
    return (Get-SzaPluginTool 'tools\Store-Preflight.ps1')
}

# Contract catch-up preflight (CONTRACTS section 6 item 6): first step of every release (when not
# -Resume). Fails closed (exit 2) when the sza plugin contract tools or catalog are unreachable, and
# blocks (exit 1) when any row of the repository key `filedo` (REGISTRY section 2: the Product cell starts
# with the key, a surface in parentheses) is behind, pending, unparsed, ahead, partial without an active
# Section 3 dated reason/migration row, or flagged with a FileDO error by check-contracts.ps1.
function Invoke-ContractCatchupPreflight {
    $lagTool = Get-SzaPluginTool 'tools\contract-lag.ps1'
    $checkTool = Get-SzaPluginTool 'tools\check-contracts.ps1'
    if (-not $lagTool -or -not $checkTool) {
        Fail "contract catch-up preflight could not find tools\contract-lag.ps1 or tools\check-contracts.ps1 in the installed sza plugin - nothing tagged." 2
    }
    $lagArgs = @('-NoProfile', '-File', $lagTool, '-Product', 'filedo', '-Json')
    if ($env:SZA_CONTRACTS_ROOT) { $lagArgs += @('-CatalogRoot', $env:SZA_CONTRACTS_ROOT) }
    $lagRaw = (& pwsh @lagArgs 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0) {
        Fail "contract-lag preflight could not read FileDO rows (exit $LASTEXITCODE): $lagRaw" 2
    }
    try { $lag = $lagRaw | ConvertFrom-Json } catch {
        Fail "contract-lag preflight returned invalid JSON: $($_.Exception.Message)" 2
    }
    $catalogPath = [string]$lag.catalog
    if (-not $catalogPath -or -not (Test-Path -LiteralPath (Join-Path $catalogPath '_meta\REGISTRY.md'))) {
        Fail "contract catch-up preflight could not reach the catalog registry from '$catalogPath' - nothing tagged." 2
    }
    $regLines = Get-Content -LiteralPath (Join-Path $catalogPath '_meta\REGISTRY.md')
    $sec3Idx = ($regLines | Select-String -Pattern '^##\s+3\.\s+' | Select-Object -First 1).LineNumber
    if (-not $sec3Idx) { $sec3Idx = 900 }
    # A Section 3 row is ours when its Product cell starts with the key, matched case-sensitively as
    # contract-lag.ps1 matches it, so another product's row that only mentions FileDO in its text is not.
    $activeSec3 = @($regLines | Select-Object -Skip ($sec3Idx - 1) | Where-Object {
        $cells = $_ -split '\|'
        $_ -match '^\|' -and $cells.Count -gt 2 -and $cells[2].Trim() -cmatch '^filedo(\s|\(|$)' -and
            $_ -notmatch '\|\s*closed\s*\|\s*$' -and $_ -notmatch '\|\s*closed\s*$'
    })
    $badLag = @()
    foreach ($r in @($lag.rows)) {
        if ($r.state -in @('behind', 'pending', 'unparsed', 'ahead')) {
            $badLag += "$($r.contract) ($($r.product)): $($r.state) (synced $($r.synced) vs current $($r.current))"
        } elseif ([string]$r.verified -eq 'pending') {
            $badLag += "$($r.contract) ($($r.product)): verified=pending"
        } elseif ($r.state -eq 'partial') {
            $cPattern = "\b$([regex]::Escape([string]$r.contract))\b"
            if (-not ($activeSec3 | Where-Object { $_ -match $cPattern })) {
                $badLag += "$($r.contract) ($($r.product)): partial without an active dated reason in REGISTRY section 3"
            }
        }
    }
    $checkOut = @(& pwsh -NoProfile -File $checkTool -CatalogRoot $catalogPath 2>&1)
    $filedoErrors = @($checkOut | Where-Object { "$_" -match '^\s*error\b' -and "$_" -match '\bFileDO\b' } | ForEach-Object { "$_" })
    if ($badLag.Count -gt 0 -or $filedoErrors.Count -gt 0) {
        $reasons = @($badLag) + @($filedoErrors)
        $reasons | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
        Fail "contract catch-up preflight blocked the release ($($reasons.Count) FileDO row issue(s); sync against the catalog first) - nothing tagged." 1
    }
    Write-Host "  contracts: PASS ($($lag.rows.Count) FileDO rows; current $($lag.counts.current), partial $($lag.counts.partial) with dated reasons, 0 behind/pending/unparsed/errors)"

    # PRODUCT-IDENTITY 0.4 section 6 rung 2 / rule 10 (FILEDO-0243): -Release turns a rendition row whose
    # renderedFrom differs from master.sha256 (such as the 21 Store logo files before assets\make-product-icons.ps1
    # -StoreLogos runs) from a between-releases NOTE into a pre-flight finding, stopping the release before the tag.
    $identityChecker = Join-Path $catalogPath 'product-identity\reference\Test-ProductIdentity.ps1'
    if (-not (Test-Path -LiteralPath $identityChecker -PathType Leaf)) {
        Fail "product-identity preflight could not find '$identityChecker' in the catalog - nothing tagged." 2
    }
    $identityOut = @(& pwsh -NoProfile -File $identityChecker -RepoRoot $root -Release 2>&1)
    $identityCode = $LASTEXITCODE
    if ($identityCode -ne 0) {
        $identityOut | ForEach-Object { Write-Host "    $_" -ForegroundColor $(if ($identityCode -eq 2) { 'Yellow' } else { 'Red' }) }
        Fail "product-identity preflight (-Release) did not pass (run assets\make-product-icons.ps1 -StoreLogos if Store logos are rendered from an earlier master) - nothing tagged." $(if ($identityCode -eq 2) { 2 } else { 1 })
    }
    Write-Host "  identity : $(($identityOut | Select-Object -Last 1))"
}

# The four-part MSIX version build-msix.ps1 stamps for a release stamp (its storeVer): yy.MMdd.HHmm.0.
function Get-StoreVersion([datetime]$when) {
    "{0}.{1}.{2}.0" -f ($when.Year % 100), ($when.Month * 100 + $when.Day), ($when.Hour * 100 + $when.Minute)
}

# Get-MsiProductVersion: the ProductVersion a published MSI really carries, read from its Property
# table. Read, not derived: the tags before PKG-01 used another mapping, and the arithmetic is exactly
# what this comparison must not have to trust. It lives in the one helper build.ps1 also uses to read
# the version back from the MSI and the setup EXE it builds (PACKAGE-VERSIONING P5 / W4); the file
# holds functions only, so dot-sourcing it runs nothing.
. (Join-Path $PSScriptRoot 'packaging\wix\installer-version.ps1')

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

# T3-F2: $null when the sources here equal the tag's - HEAD's tree equals the tag's outside
# winget\ and docs\releases\ (the two folders a release commits after the tag: the manifests and the
# release ledger) and nothing is changed or untracked outside them - otherwise the reason. Never exits: the Store step records it and carries on
# (REL-03). msix\build-msix.ps1 holds the same rule for a hand run with -Stamp.
function Get-TagSourceDrift([string]$tagName) {
    $null = git rev-parse -q --verify "refs/tags/$tagName^{commit}" 2>$null
    if ($LASTEXITCODE -ne 0) { return "$tagName is not a local tag (git fetch origin refs/tags/${tagName}:refs/tags/${tagName})" }
    git diff --quiet $tagName HEAD -- . ':!winget' ':!docs/releases'
    $diffCode = $LASTEXITCODE
    if ($diffCode -eq 1) { return "HEAD's sources differ from $tagName outside winget\ and docs\releases\ (git diff --stat $tagName HEAD -- . ':!winget' ':!docs/releases') - the Store package must be built from the tagged sources" }
    if ($diffCode -ne 0) { return "could not compare HEAD with $tagName (git diff exit $diffCode)" }
    $status = @(git status --porcelain --untracked-files=all -- . ':!winget' ':!docs/releases')
    if ($LASTEXITCODE -ne 0) { return "git status failed (exit $LASTEXITCODE)" }
    if ($status.Count) { return "the working tree has $($status.Count) change(s) outside winget\ and docs\releases\ - the Store package must be built from the tagged sources" }
    return $null
}

# ---------------------------------------------------------------------------
# 1. Preflight
# ---------------------------------------------------------------------------
Step "1/10 Preflight$(if ($Resume) { ' (-Resume)' })"

if (-not $Resume) {
    Invoke-ContractCatchupPreflight
}

foreach ($t in @('git','gh','go')) {
    if (-not (Get-Command $t -ErrorAction SilentlyContinue)) { Fail "$t not found on PATH." 2 }
}
if (-not $SkipWinget -and -not (Get-Command wingetcreate -ErrorAction SilentlyContinue)) {
    Fail "wingetcreate not found. Install: winget install Microsoft.WingetCreate (or use -SkipWinget)." 2
}
if (-not $Resume -and -not (Get-Command govulncheck -ErrorAction SilentlyContinue)) {
    Fail "govulncheck not found. Install: go install golang.org/x/vuln/cmd/govulncheck@v1.8.0" 2
}

function Test-InteractiveDesktop {
    if (-not [Environment]::UserInteractive) { return $false }
    try { return ((Get-Process -Id $PID).SessionId -ne 0) }
    catch { return $false }
}

if (-not $Resume -and -not (Test-InteractiveDesktop)) {
    Fail "an interactive desktop session is required for the pre-release in-app help freshness check (packaging\check-help.ps1 -Exe). Nothing tagged." 2
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
    # step 6 may have left rewritten. docs\releases\ (the release ledger) is allowed dirty in both: every
    # build writes builds.json, so a plain build before the release is not a reason to stop, and the ledger
    # is committed with the build (step 3) or after the tag.
    $allowedDirty = if ($Resume) { @('winget', 'docs/releases') } else { @('exe_to_download', 'docs/releases') }
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
        # T3-F2: step 8 compares HEAD's sources with the tag, so the tag must be here, and be
        # origin's. Without --force a local tag that points elsewhere makes this fail.
        Invoke-Checked "git fetch $tag" { git fetch --quiet origin "refs/tags/${tag}:refs/tags/${tag}" } 2 | Out-Null
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

    # Store preflight (WINDOWS-STORE 0.4 section D): one read-only look at the live submission before the
    # tag. Refused (exit 1) stops the release here with nothing tagged. Channel unavailable (exit 2) prints
    # one manual line and the release goes on (rule 21). Not tested here: rule 22, rule 23 (c), (e), (f).
    if (-not $SkipStore -and -not $Resume) {
        $storeVer = Get-StoreVersion $stampDate
        $storeIdentityFile = Join-Path $root 'msix\identity.json'
        $storeScript = Get-StorePreflightScript
        $storeRecord = [ordered]@{ release = $Version; packageVersion = $storeVer; listingPath = 'csv' }
        if ($storeScript) {
            $storeArgs = @('-NoProfile', '-File', $storeScript, '-ProductName', 'FileDO', '-PackageVersion', $storeVer,
                '-IdentityFile', $storeIdentityFile, '-Json')
            if ($StoreCredentialExpiry) { $storeArgs += @('-CredentialExpiry', $StoreCredentialExpiry) }
            $storeOut = @(& pwsh @storeArgs)
            $storeCode = $LASTEXITCODE
            $storeJson = @($storeOut | Where-Object { $_ -match '^\{' }) | Select-Object -Last 1
            if (-not $storeJson) { Fail "the Store preflight printed no evidence (exit $storeCode) - nothing tagged." 1 }
            if (Test-Path -LiteralPath $storeIdentityFile) {
                $storeIdentity = Get-Content -LiteralPath $storeIdentityFile -Raw | ConvertFrom-Json
                if ($storeIdentity.PSObject.Properties['StoreId']) { $storeRecord['storeId'] = [string]$storeIdentity.StoreId }
            }
            foreach ($p in ($storeJson | ConvertFrom-Json).PSObject.Properties) { $storeRecord[$p.Name] = $p.Value }
        } else {
            $storeCode = 2
            $storeRecord['mode'] = 'manual'
            $storeRecord['reason'] = 'the shared Store preflight is not installed'
        }
        $storeLogDir = Join-Path $root 'temp\evidence'
        New-Item -ItemType Directory -Force -Path $storeLogDir | Out-Null
        Add-Content -LiteralPath (Join-Path $storeLogDir 'store-preflight.log') -Value (ConvertTo-Json $storeRecord -Compress) -Encoding UTF8
        if ($storeCode -eq 2) {
            Write-Host "  Store   : manual - the Store channel is unavailable; the Store step is yours (temp\evidence\store-preflight.log). The release goes on." -ForegroundColor Yellow
        } elseif ($storeCode -ne 0) {
            Fail "the Store preflight refused this run ($($storeRecord['refusal'])) - nothing tagged. Evidence: temp\evidence\store-preflight.log" 1
        } else {
            Write-Host "  Store   : preflight passed (submission $($storeRecord['submission']), pricing $($storeRecord['pricing']))."
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
        Step "2-4/10 Gate, commit, push"
        Write-Host "  skipped (-Resume: $tag is already on origin)."
    } else {
    # -----------------------------------------------------------------------
    # 2. Build + test gate (fail BEFORE we tag anything)
    # -----------------------------------------------------------------------
    Step "2/10 Build + test gate"
    # build.ps1 builds the installer on any plain run; -Msi is what makes a
    # missing WiX exit 2 instead of a printed skip. For a release that
    # distinction matters: the installer is what most users actually get, and a
    # wxs that does not compile must surface here and not inside the tagged CI
    # run, after the one irreversible step. On a machine with no WiX the gate
    # still runs - the workflow builds the real artifacts on the runner.
    # The ledger records this build as a release build (build.ps1 reads FILEDO_BUILD_KIND).
    $env:FILEDO_BUILD_KIND = 'release'
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
    Remove-Item Env:FILEDO_BUILD_KIND -ErrorAction SilentlyContinue
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
            Write-Host " COULD NOT VERIFY" -ForegroundColor Yellow
            Fail "govulncheck could not scan $exe (exit $vulnCode): $($vulnOut.Trim())" 2
        }
        Write-Host " OK"
    }

    # Every kind of operation, run on small data with the shipped filedo.exe: each one timed and its
    # result checked against the bytes (tests\prove-operations.ps1), and the whole run compared with
    # the last passing runs: a step that vanished, a changed result or a confirmed loss of speed fails
    # it. The mounted-disk tier needs administrator rights and asks once (UAC); -SkipOpsMount drops
    # that tier and the run says so. A step that outlives its budget, or any wrong result, stops the
    # release before the tag; -OpsRebaseline accepts an intended change of behaviour or speed.
    $opsArgs = @('-Version', $Version)
    if ($SkipOpsMount) { $opsArgs += '-SkipMount' }
    if ($OpsRebaseline) { $opsArgs += '-Rebaseline' }
    Write-Host "operations run (tests\prove-operations.ps1$(if ($SkipOpsMount) { ' -SkipMount' })$(if ($OpsRebaseline) { ' -Rebaseline' })) ..."
    & pwsh -NoProfile -File "$root\tests\prove-operations.ps1" @opsArgs
    $opsCode = $LASTEXITCODE
    if ($opsCode -ne 0) { Fail "the operations run did not pass (a wrong result, or its comparison with the last passing runs; an intended change is accepted with -OpsRebaseline). Nothing tagged." $opsCode }

    # CONTEXT-HELP 0.5 section 7 item D rung 2: regenerate the in-app help set from the freshly built
    # filedo_win.exe and compare it with docs\help\. Needs an interactive desktop session.
    if (-not (Test-InteractiveDesktop)) {
        Fail "in-app help freshness check (packaging\check-help.ps1 -Exe) requires an interactive desktop session. Nothing tagged." 2
    }
    $guiExe = Join-Path $root 'exe_to_download\filedo_win.exe'
    Write-Host "in-app help freshness (packaging\check-help.ps1 -Exe) ..."
    & pwsh -NoProfile -File "$root\packaging\check-help.ps1" -Exe $guiExe
    $helpFreshCode = $LASTEXITCODE
    if ($helpFreshCode -ne 0) {
        Fail "in-app help freshness check did not pass (regenerate docs\help with filedo_win.exe --build-help if the UI or strings changed). Nothing tagged." $helpFreshCode
    }

    # The gate built from the tree the preflight saw. Anything it changed outside
    # exe_to_download\ and the ledger docs\releases\ would make the tagged commit differ from what was tested.
    $dirtyAfter = @(Get-DirtyOutside @('exe_to_download', 'docs/releases'))
    if ($dirtyAfter.Count) {
        $dirtyAfter | ForEach-Object { Write-Host "    $_" -ForegroundColor Red }
        Fail "the gate changed $($dirtyAfter.Count) file(s) outside exe_to_download\ and docs\releases\ - the tested tree is not the one that would be tagged. Nothing tagged."
    }
    Write-Host "release-preflight ${Version}: PASS" -ForegroundColor Green
    $script:inPreflight = $false

    # -----------------------------------------------------------------------
    # 3. Commit refreshed artifacts (binaries are tracked in exe_to_download\)
    # -----------------------------------------------------------------------
    Step "3/10 Commit"
    # REL-01: exe_to_download\ and the ledger docs\releases\, nothing else. A dry run does not touch the index at all.
    if ($DryRun) {
        $wouldCommit = @(Invoke-Checked 'git status' { git status --porcelain --untracked-files=all -- exe_to_download docs/releases } 2)
        if ($wouldCommit.Count) {
            Write-Host "  [dry-run] would commit (exe_to_download\ and docs\releases\ only):" -ForegroundColor Yellow
            $wouldCommit | ForEach-Object { Write-Host "      $_" }
        } else {
            Write-Host "  nothing to commit (tree already matches build output)."
        }
    } else {
        Invoke-Checked 'git add exe_to_download docs/releases' { git add -- exe_to_download docs/releases } | Out-Null
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
    Step "4/10 Push main + tag"
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
    Step "5/10 Wait for GitHub Release"
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
        Write-Host "  [dry-run] would: sync + check winget\, commit, push, wingetcreate submit, build the MSIX, sync site release manifest" -ForegroundColor Yellow
        Write-Host "Dry run complete." -ForegroundColor Yellow
        exit 0
    }

    # RELEASE-LEDGER: the Release is live with all its assets - GitHub, and the setup installer that is one of them.
    Write-Ledger @('publish', '-Key', 'github', '-Version', $Version, '-State', 'live', '-Ref', $tag, '-Url', "https://github.com/$repo/releases/tag/$tag")
    Write-Ledger @('publish', '-Key', 'installer', '-Version', $Version, '-State', 'live', '-Ref', $tag, '-Url', "https://github.com/$repo/releases/tag/$tag", '-Note', 'the setup EXE and the MSI are assets of the Release')

    # -----------------------------------------------------------------------
    # 6. Sync winget/*.yaml
    # -----------------------------------------------------------------------
    # From here on the tag is public. A failing channel is recorded and reported, never
    # silently passed over, and never allowed to stop the channels after it (REL-03).
    if (-not $SkipWinget) {
        Step "6/10 Sync winget manifests"
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
        Step "7/10 winget submit"
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
                    # RELEASE-LEDGER: the pull request is submitted, not live (its merge makes it live, and a probe
                    # sees that). The number is looked up best-effort; a miss only drops the reference.
                    $ledgerPr = $null
                    try {
                        $ledgerPr = @(& gh pr list --repo microsoft/winget-pkgs --author '@me' --state open --limit 20 --json number,title,url 2>$null | ConvertFrom-Json |
                            Where-Object { $_.title -match [regex]::Escape("SerZhyAle.FileDO version $Version") }) | Select-Object -First 1
                    } catch { $ledgerPr = $null }
                    $ledgerWinget = @('publish', '-Key', 'winget', '-Version', $Version, '-State', 'submitted')
                    if ($ledgerPr) { $ledgerWinget += @('-Ref', "PR#$($ledgerPr.number)", '-Url', [string]$ledgerPr.url) }
                    Write-Ledger $ledgerWinget
                    # The PR opens with every template box empty and a blank Description. Fill them
                    # from evidence (CLA status, validate, schema, one manifest, no other PR; the
                    # install box only when -WingetInstallTest just passed). A box left empty is
                    # reported, not fatal: the PR exists and the owner can tick it by hand.
                    $prArgs = @{ Version = $Version }
                    if ($WingetInstallTest) { $prArgs['InstallTested'] = $true }
                    & "$root\packaging\set-winget-pr-checklist.ps1" @prArgs
                    if ($LASTEXITCODE -ne 0) {
                        Write-Host "  winget PR checklist: not every box was ticked (see above) - tick the rest on the PR." -ForegroundColor Yellow
                    }
                }
            }
        }
        if ($wingetFailure) {
            Write-Host "  winget FAILED: $wingetFailure" -ForegroundColor Red
            Write-Ledger @('publish', '-Key', 'winget', '-Version', $Version, '-State', 'pending', '-Note', "not submitted: $wingetFailure")
        }
    } else {
        Step "6-7/10 winget"; Write-Host "  skipped (-SkipWinget)."
    }

    # -----------------------------------------------------------------------
    # 7b. Own Scoop bucket: bucket/filedo.json in SerZhyAle/scoop-bucket to this release
    # -----------------------------------------------------------------------
    # The template's Excavator schedule does not keep the bucket current (GitHub never registered it), so the
    # release does: one commit through the contents API, read back. Idempotent, so -Resume repeats it safely.
    if (-not $SkipScoop) {
        Step "7b/10 Scoop bucket"
        $scoopCode = Invoke-Native { pwsh -NoProfile -File "$root\packaging\update-scoop-bucket.ps1" -Version $Version }
        if ($scoopCode -ne 0) {
            $scoopFailure = "update-scoop-bucket.ps1 exited $scoopCode - SerZhyAle/scoop-bucket still serves an older release"
            Write-Host "  Scoop FAILED: $scoopFailure" -ForegroundColor Red
        }
    } else {
        Step "7b/10 Scoop bucket"; Write-Host "  skipped (-SkipScoop)."
    }

    # -----------------------------------------------------------------------
    # 8. Microsoft Store (MSIX build; the upload and the submission stay manual - no write call through the Store API or msstore)
    # -----------------------------------------------------------------------
    if (-not $SkipStore) {
        Step "8/10 Store MSIX"
        # Pinned to this release's stamp (the tag without the v), so the exe inside the package
        # prints the version the release carries. build-msix.ps1 refuses without a reserved
        # identity; by now the tag is out, so a refusal is recorded and reported, not fatal here.
        $msixArgs = @{ Stamp = $Version }
        if ($StoreIdentityName)        { $msixArgs['IdentityName'] = $StoreIdentityName }
        if ($StorePublisher)           { $msixArgs['Publisher'] = $StorePublisher }
        if ($StorePublisherDisplayName){ $msixArgs['PublisherDisplayName'] = $StorePublisherDisplayName }
        # T3-F2 / AUD-68-F1 (owner decision 2026-10-01): the Store package stamped $Version is
        # compiled only from sources equal to $tag. In the normal flow HEAD is the tag plus the
        # winget commit; under -Resume main may have moved on since, and a package built from
        # that would carry code no gate judged and differ from GitHub's $Version. build-msix.ps1
        # repeats this check for a hand run with -Stamp.
        $storeFailure = Get-TagSourceDrift $tag
        if ($storeFailure) {
            Write-Host "  MSIX NOT built: $storeFailure" -ForegroundColor Yellow
        } else {
            try {
                & "$root\msix\build-msix.ps1" @msixArgs
                if ($LASTEXITCODE -ne 0) { throw "build-msix.ps1 exited $LASTEXITCODE" }
            } catch {
                $storeFailure = "$($_.Exception.Message)".Trim()
                Write-Host "  MSIX build did NOT produce a Store package:" -ForegroundColor Yellow
                Write-Host "    $storeFailure" -ForegroundColor Yellow
            }
            if (-not $storeFailure) {
                # RELEASE-LEDGER: the package is prepared, not handed to the Store (the upload and the
                # submission are manual), so the channel step is pending.
                Write-Ledger @('publish', '-Key', 'msstore', '-Version', (Get-StoreVersion $stampDate), '-State', 'pending', '-Note', 'MSIX built from the tag, unsigned; upload and submission are manual')
            }
        }
    } else {
        Step "8/10 Store MSIX"; Write-Host "  skipped (-SkipStore)."
    }

    # RELEASE-LEDGER: what the channel steps and the Store build wrote since the tag is committed apart
    # ("Ledger: v<stamp>") and pushed before the site sync, which stays tag-equal outside winget\ and
    # docs\releases\. A failure here is a note, never a stop.
    $ledgerAddCode = Invoke-Native { git add -- docs/releases }
    if ($ledgerAddCode -eq 0) {
        $ledgerStaged = @(git diff --cached --name-only -- docs/releases)
        if ($LASTEXITCODE -eq 0 -and $ledgerStaged.Count) {
            $ledgerCommitCode = Invoke-Native { git commit -m "Ledger: $tag" -- docs/releases }
            if ($ledgerCommitCode -eq 0) {
                $ledgerPushCode = Invoke-Native { git push origin main }
                if ($ledgerPushCode -eq 0) { Write-Host "  ledger  : docs\releases committed and pushed (Ledger: $tag)." }
                else { Write-Host "  ledger  : committed, not pushed (git push origin main failed, exit $ledgerPushCode)." -ForegroundColor Yellow }
            } else {
                Write-Host "  ledger  : git commit of docs\releases failed (exit $ledgerCommitCode)." -ForegroundColor Yellow
            }
        }
    } else {
        Write-Host "  ledger  : git add docs\releases failed (exit $ledgerAddCode)." -ForegroundColor Yellow
    }

    # -----------------------------------------------------------------------
    # 9. Sync site release manifest + landing pages (PAGE-STYLE 3.10)
    # -----------------------------------------------------------------------
    # Runs after Store MSIX so Get-TagSourceDrift keeps verifying exact tag equality
    # outside winget\. Updates packaging\release-manifest.json and the five landing
    # pages, verifies external-docs, commits, and pushes main (REL-03 on failure).
    Step "9/10 Sync site release manifest"
    $siteFiles = @(
        'packaging/release-manifest.json',
        'docs/index.html',
        'docs/de/index.html',
        'docs/fr/index.html',
        'docs/ru/index.html',
        'docs/uk/index.html'
    )
    $manifestCode = Invoke-Native { pwsh -NoProfile -File "$root\packaging\set-release-manifest.ps1" -Tag $tag }
    if ($manifestCode -ne 0) {
        $siteFailure = "set-release-manifest.ps1 failed (exit $manifestCode)"
    }
    if (-not $siteFailure) {
        $renderCode = Invoke-Native { pwsh -NoProfile -File "$root\packaging\render-site-release.ps1" }
        if ($renderCode -ne 0) {
            $siteFailure = "render-site-release.ps1 failed (exit $renderCode)"
        }
    }
    if (-not $siteFailure) {
        $extCode = Invoke-Native { pwsh -NoProfile -File "$root\packaging\check-external-docs.ps1" }
        if ($extCode -ne 0) {
            $siteFailure = "check-external-docs.ps1 failed after site render (exit $extCode)"
        }
    }
    if (-not $siteFailure) {
        $addSiteCode = Invoke-Native { git add -- @siteFiles }
        if ($addSiteCode -ne 0) { $siteFailure = "git add site release files failed (exit $addSiteCode)" }
    }
    if (-not $siteFailure) {
        $stagedSite = @(git diff --cached --name-only -- @siteFiles)
        if ($LASTEXITCODE -ne 0) {
            $siteFailure = "git diff --cached failed (exit $LASTEXITCODE)"
        } elseif ($stagedSite.Count) {
            $commitSiteCode = Invoke-Native { git commit -m "Sync site to $tag" }
            if ($commitSiteCode -ne 0) {
                $siteFailure = "git commit of site release files failed (exit $commitSiteCode)"
            } else {
                $pushSiteCode = Invoke-Native { git push origin main }
                if ($pushSiteCode -ne 0) {
                    $siteFailure = "git push origin main for site failed (exit $pushSiteCode) - run: git pull --rebase origin main, then push"
                } else {
                    Write-Host "  site manifest and landing pages synced and pushed."
                }
            }
        } else {
            Write-Host "  site manifest and landing pages already carry $tag."
        }
    }
    if ($siteFailure) { Write-Host "  site FAILED: $siteFailure" -ForegroundColor Red }

    # Dynamic audit stand (SP-0167, release tier by the owner's 2026-10-30 and 2026-11-18 decisions): its
    # build lock, self-test and control run go here, after every channel, so they never delay or shape what is
    # published. Neither starts the product. The verdicts are reported for the owner to read, and like the
    # ledger the stand never fails a release; -SkipDynamicAudit leaves it out.
    $dynAuditTxt = 'skipped (-SkipDynamicAudit)'
    if (-not $SkipDynamicAudit) {
        Write-Host ""
        Write-Host "dynamic audit stand (SP-0167: dynaudit.ps1 -LockBuild -Tag $tag, -SelfTest, -Control) ..."
        $dynAudit = Join-Path $root 'PLAN\SP-0167 dynamic-audit-five-properties\dynaudit.ps1'
        if (-not (Test-Path -LiteralPath $dynAudit)) {
            $dynAuditTxt = 'not run - the stand is missing (PLAN\SP-0167 dynamic-audit-five-properties\dynaudit.ps1)'
        } else {
            $dynVerdicts = @()
            foreach ($dynArgs in @(@('-LockBuild', '-Tag', $tag), @('-SelfTest'), @('-Control'))) {
                $dynVerb = $dynArgs[0]
                $dynOut = @(& pwsh -NoProfile -File $dynAudit @dynArgs 2>&1 | ForEach-Object { "$_" })
                $dynCode = $LASTEXITCODE
                $dynOut | ForEach-Object { Write-Host "    $_" }
                $dynLast = @($dynOut | Where-Object { $_ -match '^dynaudit' } | Select-Object -Last 1)
                $dynVerdicts += "$dynVerb exit $dynCode$(if ($dynLast.Count) { ' (' + $dynLast[0] + ')' })"
            }
            $global:LASTEXITCODE = 0
            $dynAuditTxt = ($dynVerdicts -join '; ') + ' - evidence in temp\evidence\dyn-*; the owner reads it (exit 2 = stand not complete yet)'
        }
    }

    # -----------------------------------------------------------------------
    # 10. Final checklist
    # -----------------------------------------------------------------------
    Step "10/10 Done"
    $wTxt = if ($SkipWinget) { "skipped" } elseif ($wingetFailure) { "FAILED - $wingetFailure" } else { "synced + PR submitted" }
    $scoopTxt = if ($SkipScoop) { "skipped" } elseif ($scoopFailure) { "FAILED - $scoopFailure" } else { "SerZhyAle/scoop-bucket serves $Version" }
    $sTxt = if ($SkipStore) { "skipped" } elseif ($storeFailure) { "NOT BUILT - $storeFailure" } else { "built into msix\out\ (unsigned, stamp $Version)" }
    $siteTxt = if ($siteFailure) { "FAILED - $siteFailure" } else { "manifest + 5 landing pages synced to $tag" }
    Write-Host "  [x] GitHub Release : https://github.com/$repo/releases/tag/$tag"
    Write-Host "  [$(if ($wingetFailure -or $SkipWinget) { ' ' } else { 'x' })] winget/ : $wTxt"
    Write-Host "  [$(if ($scoopFailure -or $SkipScoop) { ' ' } else { 'x' })] Scoop  : $scoopTxt"
    Write-Host "  [$(if ($storeFailure -or $SkipStore) { ' ' } else { 'x' })] Store MSIX : $sTxt"
    Write-Host "  [$(if ($siteFailure) { ' ' } else { 'x' })] site   : $siteTxt"
    Write-Host "  [i] audit  : $dynAuditTxt"
    Write-Host ""
    if ($wingetFailure) {
        Write-Host "  RECOVER winget (the tag and the GitHub Release stand; do not cut a new version):" -ForegroundColor Yellow
        Write-Host "    .\release.ps1 -Resume -Version $Version -SkipStore" -ForegroundColor Yellow
        Write-Host "  or by hand - RELEASE.md, 'If something fails mid-release'." -ForegroundColor Yellow
    }
    if ($scoopFailure) {
        Write-Host "  RECOVER Scoop: pwsh -NoProfile -File .\packaging\update-scoop-bucket.ps1 -Version $Version   (or .\release.ps1 -Resume -Version $Version -SkipWinget -SkipStore)" -ForegroundColor Yellow
    }
    if ($storeFailure) {
        Write-Host "  RECOVER Store: fix the cause, then .\msix\build-msix.ps1 -Stamp $Version from sources equal to $tag" -ForegroundColor Yellow
        Write-Host "    (it refuses unless: git diff --quiet $tag HEAD -- . ':!winget' ':!docs/releases' and no change outside winget\ and docs\releases\;" -ForegroundColor Yellow
        Write-Host "     if main has moved on: git worktree add ..\FileDO-$tag $tag, and run it there)" -ForegroundColor Yellow
    }
    if ($siteFailure) {
        Write-Host "  RECOVER site: .\packaging\set-release-manifest.ps1 -Tag $tag; .\packaging\render-site-release.ps1; .\packaging\check-external-docs.ps1" -ForegroundColor Yellow
    }
    if (-not $SkipStore -and -not $storeFailure) {
        Write-Host "  MANUAL (the upload and the submission are yours; nothing writes to the Store for you), in this order - msix\README.md sections 4-6:" -ForegroundColor Yellow
        Write-Host "    1. Partner Center > Create new submission > Packages: upload msix\out\FileDO_*.msix" -ForegroundColor Yellow
        Write-Host "    2. Store listings: export, .\msix\build-store-listing-csv.ps1 -Refresh ReleaseNotes, import msix\out\store-import" -ForegroundColor Yellow
        Write-Host "       (ReleaseNotes come from msix\listing\<code>.txt: write this release's notes there first)" -ForegroundColor Yellow
        Write-Host "    3. Submit, then update over a real prior install and check the version and the notes." -ForegroundColor Yellow
    }
    Write-Host "  Then verify: winget show SerZhyAle.FileDO   (after the winget-pkgs PR merges)"
    Write-Host "  Ledger: packaging\release-ledger.ps1 show   (run its probe verb after the winget PR merges or the Store submission is committed)"
    $failedChannels = @()
    if ($wingetFailure) { $failedChannels += 'winget' }
    if ($scoopFailure)  { $failedChannels += 'Scoop bucket' }
    if ($storeFailure)  { $failedChannels += 'Store MSIX' }
    if ($siteFailure)   { $failedChannels += 'site' }
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
