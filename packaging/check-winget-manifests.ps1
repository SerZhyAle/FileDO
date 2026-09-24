#Requires -Version 7.0
<#
.SYNOPSIS
  Pre-publication checks for the winget manifest set.

.DESCRIPTION
  Everything that can be wrong with winget\*.yaml and still look fine until the tag is out.
  A winget-pkgs PR is re-validated by Microsoft's own CI after the tag is pushed, which is one
  irreversible step too late: these checks run before the commit that release.ps1 makes in its
  winget step, so a bad manifest aborts the release instead of dying in someone else's pipeline.

  STATIC (always)
    * winget validate --manifest <dir> - the schema, as winget itself reads it.
    * PackageIdentifier is the frozen anchor SerZhyAle.FileDO in all three files.
    * The five PortableCommandAlias values are the frozen set; renaming one orphans installs.
    * Every RelativeFilePath is its alias plus .exe - the zip layout the aliases resolve against.
    * PackageVersion is one 10-digit stamp shared by all three files.
    * InstallerUrl, and the ReleaseNotesUrl of the locale file, carry that same version.

  NETWORK (unless -NoNetwork)
    * InstallerSha256 equals the hash the release published for the artifact the URL points at.
      Read from <InstallerUrl>.sha256, which the release workflow writes beside the asset and
      verifies against it there - so this compares the manifest against the published release,
      not against whatever the syncing script happened to hold in memory.
    * -Download hashes the artifact itself instead of trusting the sidecar.

  INSTALL (-Install only)
    * winget install --manifest, run the portable shim, winget uninstall, assert nothing is left.
      Per-user and admin-free, but it writes to the profile of whoever runs it - which is why it
      is a switch and not the default.

  Exit code: 0 = every check passed, 1 = a defect was found, 2 = could not verify.

  This script lives in packaging\ and not in winget\ on purpose: `winget validate --manifest`
  is pointed at the directory and parses EVERY file in it as YAML, so one .ps1 next to the
  manifests fails the very check it implements. winget\ holds the three manifests and nothing
  else - that is also the directory `wingetcreate submit` is handed.

.PARAMETER Path
  The manifest directory. Default: the repo's winget\ beside this script's parent.

.PARAMETER Version
  The 10-digit stamp being released (the tag without the v). Given, the version checks assert
  against it; omitted, they only assert that the three files agree with each other.

.EXAMPLE
  pwsh -NoProfile -File .\packaging\check-winget-manifests.ps1
.EXAMPLE
  pwsh -NoProfile -File .\packaging\check-winget-manifests.ps1 -Version 2607301014 -Install
#>
[CmdletBinding()]
param(
    [string]$Path,
    [ValidatePattern('^\d{10}$')]
    [string]$Version,
    [switch]$Download,
    [switch]$NoNetwork,
    [switch]$Install
)

$ErrorActionPreference = "Stop"
if (-not $Path) { $Path = Join-Path (Split-Path $PSScriptRoot -Parent) "winget" }
if (-not (Test-Path $Path)) {
    Write-Host "winget-manifests: NOT VERIFIED (manifest directory does not exist: $Path)" -ForegroundColor Yellow
    exit 2
}
$Path = (Resolve-Path $Path).Path

# --- frozen anchors ----------------------------------------------------------
# A shipped identifier or command alias is never changed - a rename orphans every install
# that carries the old one. They are spelled out here so changing one has to be a deliberate
# edit of this file, and never a slip in a manifest a tool rewrote.
$AnchorIdentifier = 'SerZhyAle.FileDO'
$AnchorAliases    = @('filedo', 'filedo_check', 'filedo_fill', 'filedo_test', 'filedo_win')

$Files = [ordered]@{
    version   = 'SerZhyAle.FileDO.yaml'
    locale    = 'SerZhyAle.FileDO.locale.en-US.yaml'
    installer = 'SerZhyAle.FileDO.installer.yaml'
}

$script:fail = 0; $script:pass = 0; $script:unverified = @()
function Check([string]$name, [bool]$ok, [string]$detail = "") {
    if ($ok) { $script:pass++; Write-Host "  PASS  $name" -ForegroundColor Green }
    else     { $script:fail++; Write-Host "  FAIL  $name  $detail" -ForegroundColor Red }
}
function CannotVerify([string]$reason) {
    $script:unverified += $reason
    Write-Host "  NOT VERIFIED  $reason" -ForegroundColor Yellow
}
function Get-Field([string]$text, [string]$key) {
    # The installer keys sit inside the Installers sequence, so the line may be indented and
    # may open a list item ("- Architecture: x64" is followed by an indented InstallerUrl).
    if ($text -match "(?m)^\s*(?:-\s+)?$key\s*:\s*(.+?)\s*$") { return $Matches[1].Trim('"') }
    return $null
}

Write-Host "winget manifest checks - $Path" -ForegroundColor Cyan
Write-Host ""

# --- the files are there and readable ----------------------------------------
$raw = [ordered]@{}
$missing = @()
foreach ($k in $Files.Keys) {
    $p = Join-Path $Path $Files[$k]
    if (Test-Path $p) { $raw[$k] = Get-Content $p -Raw } else { $missing += $Files[$k] }
}
Check "the three manifests exist" ($missing.Count -eq 0) ($missing -join ', ')
if ($missing.Count) { Write-Host ""; Write-Host "winget-manifests: FAIL (missing manifests)" -ForegroundColor Red; exit 1 }

# --- the schema, as winget reads it ------------------------------------------
Write-Host "SCHEMA" -ForegroundColor Cyan
if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
    CannotVerify "winget is not on PATH (install App Installer)"
} else {
    $vOut  = & winget validate --manifest $Path 2>&1 | Out-String
    $vCode = $LASTEXITCODE
    # 0 = valid. -1978335192 = valid with warnings, which winget-pkgs CI accepts; anything
    # else is an error. winget's own output is localized, so only the code is read.
    $ok = ($vCode -eq 0 -or $vCode -eq -1978335192)
    Check "winget validate --manifest (exit $vCode)" $ok ($vOut.Trim())
    if ($ok -and $vCode -ne 0) {
        Write-Host "        validation passed with warnings:" -ForegroundColor Yellow
        Write-Host ($vOut.Trim()) -ForegroundColor Yellow
    }
}

# --- frozen anchors ----------------------------------------------------------
Write-Host "ANCHORS" -ForegroundColor Cyan
$ids = @($raw.Keys | ForEach-Object { Get-Field $raw[$_] 'PackageIdentifier' })
Check "PackageIdentifier is '$AnchorIdentifier' in all three files (frozen anchor)" `
      ((@($ids | Select-Object -Unique)).Count -eq 1 -and $ids[0] -ceq $AnchorIdentifier) ($ids -join ', ')

# NestedInstallerFiles: each entry is a RelativeFilePath and the alias it is exposed as.
$nested  = [regex]::Matches($raw['installer'], '(?m)^\s*-\s*RelativeFilePath:\s*(\S+)\s*\r?\n\s*PortableCommandAlias:\s*(\S+)\s*$')
$paths   = @($nested | ForEach-Object { $_.Groups[1].Value })
$aliases = @($nested | ForEach-Object { $_.Groups[2].Value })
Check "the five PortableCommandAlias values are the frozen set" (($aliases -join ',') -ceq ($AnchorAliases -join ',')) ("found: " + ($aliases -join ','))
$badPath = @()
for ($i = 0; $i -lt $aliases.Count; $i++) {
    if ($paths[$i] -cne "$($aliases[$i]).exe") { $badPath += "$($paths[$i]) is not $($aliases[$i]).exe" }
}
Check "every RelativeFilePath is its alias plus .exe, at the zip root" ($badPath.Count -eq 0) ($badPath -join '; ')

# --- version consistency -----------------------------------------------------
Write-Host "VERSION" -ForegroundColor Cyan
$vers   = @($raw.Keys | ForEach-Object { Get-Field $raw[$_] 'PackageVersion' })
$oneVer = (@($vers | Select-Object -Unique)).Count -eq 1
Check "PackageVersion is one 10-digit stamp shared by all three files" ($oneVer -and $vers[0] -match '^\d{10}$') ($vers -join ', ')
$stamp = if ($oneVer) { $vers[0] } else { $null }
if ($Version) {
    $said = if ($stamp) { "the manifests say '$stamp'" } else { "the manifests do not agree: $($vers -join ', ')" }
    Check "PackageVersion is the version being released ($Version)" ($stamp -ceq $Version) $said
    $stamp = $Version
}

$url   = Get-Field $raw['installer'] 'InstallerUrl'
$sha   = Get-Field $raw['installer'] 'InstallerSha256'
$notes = Get-Field $raw['locale'] 'ReleaseNotesUrl'
if ($stamp) {
    $wantUrl = "https://github.com/SerZhyAle/FileDO/releases/download/v$stamp/FileDO-$stamp-windows-x64.zip"
    Check "InstallerUrl points at the v$stamp asset" ($url -ceq $wantUrl) "url: $url"
    Check "ReleaseNotesUrl points at the v$stamp tag" ($notes -ceq "https://github.com/SerZhyAle/FileDO/releases/tag/v$stamp") "notes: $notes"
} else {
    Check "InstallerUrl and ReleaseNotesUrl carry the released version" $false "the three files do not agree on a version to check them against"
}
Check "InstallerSha256 is 64 uppercase hex digits" ($sha -cmatch '^[0-9A-F]{64}$') "sha: $sha"

# --- the hash is the published artifact's ------------------------------------
Write-Host "HASH" -ForegroundColor Cyan
if ($NoNetwork) {
    Write-Host "  SKIP  InstallerSha256 against the published artifact (-NoNetwork)" -ForegroundColor Yellow
} elseif (-not $url) {
    Check "InstallerSha256 against the published artifact" $false "no InstallerUrl to fetch"
} elseif ($Download) {
    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("fd-winget-" + [guid]::NewGuid().ToString('N').Substring(0, 8) + ".zip")
    try {
        Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
        $real = (Get-FileHash $tmp -Algorithm SHA256).Hash.ToUpper()
        Check "InstallerSha256 is the hash of the downloaded artifact" ($sha -ceq $real) "the asset hashes to $real"
    } catch {
        $status = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { $null }
        if ($status -eq 404) { Check "the artifact at InstallerUrl can be downloaded" $false "status 404 - $($_.Exception.Message)" }
        else { CannotVerify "the artifact at InstallerUrl could not be reached: $($_.Exception.Message)" }
    } finally { Remove-Item $tmp -ErrorAction SilentlyContinue }
} else {
    try {
        # GitHub serves release assets as application/octet-stream, so .Content comes back as
        # bytes, not as a string - splitting that on whitespace yields the first byte's value.
        $side = (Invoke-WebRequest -Uri "$url.sha256" -UseBasicParsing).Content
        if ($side -is [byte[]]) { $side = [System.Text.Encoding]::ASCII.GetString($side) }
        $published = ((([string]$side).Trim() -split '\s+')[0]).ToUpper()
        Check "InstallerSha256 equals the hash published beside the asset" ($sha -ceq $published) "published: $published"
    } catch {
        $status = if ($_.Exception.Response) { [int]$_.Exception.Response.StatusCode } else { $null }
        if ($status -eq 404) { Check "the .sha256 sidecar of InstallerUrl can be read" $false "status 404 - $($_.Exception.Message)" }
        else { CannotVerify "the .sha256 sidecar of InstallerUrl could not be reached: $($_.Exception.Message)" }
    }
}

# --- install from the manifest, run a shim, uninstall ------------------------
if ($Install) {
    Write-Host "INSTALL" -ForegroundColor Cyan
    $pkgRoot = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Packages"
    $links   = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Links"
    $already = @(Get-ChildItem $pkgRoot -Directory -Filter "$AnchorIdentifier*" -ErrorAction SilentlyContinue)
    if ($already.Count) {
        Check "no earlier $AnchorIdentifier install is in the way" $false "remove it first: winget uninstall $AnchorIdentifier"
    } else {
        $iOut = & winget install --manifest $Path --accept-package-agreements --accept-source-agreements --disable-interactivity 2>&1 | Out-String
        Check "winget install --manifest succeeds (exit $LASTEXITCODE)" ($LASTEXITCODE -eq 0) ($iOut.Trim())
        $dir = @(Get-ChildItem $pkgRoot -Directory -Filter "$AnchorIdentifier*" -ErrorAction SilentlyContinue)
        Check "the package landed in $pkgRoot" ($dir.Count -eq 1) "found $($dir.Count) directories"
        # Every alias must be a real shim, and the one that matters must run: an exe named in
        # the manifest but absent from the zip produces a link that resolves to nothing.
        $noShim = @($AnchorAliases | Where-Object { -not (Test-Path (Join-Path $links "$_.exe")) })
        Check "all five shims were created in $links" ($noShim.Count -eq 0) ($noShim -join ', ')
        $shim = Join-Path $links "filedo.exe"
        if (Test-Path $shim) {
            $sOut = & $shim "-?" 2>&1 | Out-String
            Check "the filedo shim runs" ($LASTEXITCODE -eq 0 -and $sOut -match 'FileDO') (($sOut.Trim() -split "`n" | Select-Object -First 2) -join ' / ')
        }
        $uOut = & winget uninstall $AnchorIdentifier --disable-interactivity 2>&1 | Out-String
        Check "winget uninstall succeeds (exit $LASTEXITCODE)" ($LASTEXITCODE -eq 0) ($uOut.Trim())
        $left = @(Get-ChildItem $pkgRoot -Directory -Filter "$AnchorIdentifier*" -ErrorAction SilentlyContinue)
        Check "uninstall leaves no package directory behind" ($left.Count -eq 0) ($left.Name -join ', ')
    }
}

Write-Host ""
if ($script:fail -gt 0) { Write-Host "winget-manifests: FAIL ($script:fail checks)" -ForegroundColor Red; exit 1 }
if ($script:unverified.Count -gt 0) { Write-Host "winget-manifests: NOT VERIFIED ($($script:unverified -join '; '))" -ForegroundColor Yellow; exit 2 }
Write-Host "winget-manifests: PASS ($script:pass checks)" -ForegroundColor Green
exit 0
