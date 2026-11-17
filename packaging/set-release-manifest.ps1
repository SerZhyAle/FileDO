#Requires -Version 7.0
<#
.SYNOPSIS
  Writes packaging/release-manifest.json from a published GitHub release (PAGE-STYLE 3.10).

.DESCRIPTION
  Reads the published GitHub release via `gh release view` (and downloads its `.sha256` sidecars),
  refusing drafts, pre-releases, invalid stamps, or any release missing any of the six expected
  assets (setup EXE, portable ZIP, MSI, and their `.sha256` sidecars).

  Exit codes (CHECK-VERDICT):
    0  manifest written
    1  defect found (draft/prerelease, bad tag/date, missing asset, checksum mismatch)
    2  could not verify (gh missing, network/API failure, unreadable output)
#>
[CmdletBinding(PositionalBinding = $false)]
param(
    [string]$Tag,
    [string]$OutPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0

$root = Split-Path $PSScriptRoot -Parent
$repo = 'SerZhyAle/FileDO'
if (-not $OutPath) { $OutPath = Join-Path $root 'packaging\release-manifest.json' }

function Stop-Fail([string]$why) {
    Write-Host "set-release-manifest: FAIL ($why)" -ForegroundColor Red
    exit 1
}

function Stop-Unverified([string]$why) {
    Write-Host "set-release-manifest: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    Stop-Unverified "'gh' is not on PATH"
}

$viewArgs = @('release', 'view')
if ($Tag) { $viewArgs += $Tag }
$viewArgs += @('--repo', $repo, '--json', 'tagName,isDraft,isPrerelease,publishedAt,url,assets')

$raw = (& gh @viewArgs 2>&1 | Out-String)
if ($LASTEXITCODE -ne 0) {
    Stop-Unverified "gh release view failed (exit $LASTEXITCODE): $($raw.Trim())"
}

try {
    $rel = $raw | ConvertFrom-Json
} catch {
    Stop-Unverified "gh release view returned invalid JSON: $($_.Exception.Message)"
}

if ($null -eq $rel -or -not $rel.tagName) {
    Stop-Unverified 'gh release view returned no release'
}
if ($rel.isDraft) {
    Stop-Fail "release $($rel.tagName) is a draft"
}
if ($rel.isPrerelease) {
    Stop-Fail "release $($rel.tagName) is a pre-release"
}

$tag = [string]$rel.tagName
if ($tag -cnotmatch '^v(\d{10})$') {
    Stop-Fail "tag '$tag' does not match v<yyMMddHHmm>"
}
$version = $Matches[1]
try {
    $stampDate = [datetime]::ParseExact($version, 'yyMMddHHmm', [Globalization.CultureInfo]::InvariantCulture)
} catch {
    Stop-Fail "tag '$tag' does not carry a valid yyMMddHHmm date"
}
$date = $stampDate.ToString('yyyy-MM-dd', [Globalization.CultureInfo]::InvariantCulture)

$byName = @{}
foreach ($a in @($rel.assets)) {
    if ($a -and $a.name) {
        $byName[[string]$a.name] = $a
    }
}

$expected = [ordered]@{
    setup = "FileDO-$version-setup.exe"
    zip   = "FileDO-$version-windows-x64.zip"
    msi   = "FileDO-$version-windows-x64.msi"
}

foreach ($k in $expected.Keys) {
    $binName = $expected[$k]
    $shaName = "$binName.sha256"
    foreach ($req in $binName, $shaName) {
        if (-not $byName.ContainsKey($req)) {
            Stop-Fail "release $tag is missing required asset '$req'"
        }
        $asset = $byName[$req]
        if ([long]$asset.size -le 0) {
            Stop-Fail "release $tag asset '$req' has non-positive size ($($asset.size))"
        }
        if ($asset.PSObject.Properties['state'] -and $asset.state -and $asset.state -ne 'uploaded') {
            Stop-Fail "release $tag asset '$req' state is '$($asset.state)', not 'uploaded'"
        }
    }
}

$tmp = Join-Path ([IO.Path]::GetTempPath()) "filedo-manifest-$version-$PID"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    $dlOut = (& gh release download $tag --repo $repo --pattern '*.sha256' --dir $tmp --clobber 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0) {
        Stop-Unverified "gh release download *.sha256 failed (exit $LASTEXITCODE): $($dlOut.Trim())"
    }

    $assetsOut = [ordered]@{}
    $baseDl = "https://github.com/$repo/releases/download/$tag"
    foreach ($k in $expected.Keys) {
        $binName = $expected[$k]
        $shaName = "$binName.sha256"
        $binAsset = $byName[$binName]
        $shaPath = Join-Path $tmp $shaName
        if (-not (Test-Path -LiteralPath $shaPath -PathType Leaf)) {
            Stop-Unverified "downloaded sidecar '$shaName' is missing"
        }
        $shaText = ([IO.File]::ReadAllText($shaPath)).Trim()
        $shaToken = ($shaText -split '\s+')[0].ToLowerInvariant()
        if ($shaToken -notmatch '^[0-9a-f]{64}$') {
            Stop-Fail "sidecar '$shaName' does not start with a 64-hex SHA-256 digest: '$shaText'"
        }
        if ($binAsset.PSObject.Properties['digest'] -and [string]$binAsset.digest -match '^sha256:([0-9a-fA-F]{64})$') {
            $ghDigest = $Matches[1].ToLowerInvariant()
            if ($ghDigest -ne $shaToken) {
                Stop-Fail "sidecar '$shaName' ($shaToken) disagrees with GitHub asset digest ($ghDigest)"
            }
        }
        $size = [long]$binAsset.size
        $sizeMb = ($size / 1048576.0).ToString('F1', [Globalization.CultureInfo]::InvariantCulture)
        $assetsOut[$k] = [ordered]@{
            name       = $binName
            size       = $size
            sizeMb     = $sizeMb
            url        = "$baseDl/$binName"
            sha256Name = $shaName
            sha256Url  = "$baseDl/$shaName"
            sha256     = $shaToken
        }
    }

    $publishedAt = ''
    if ($rel.PSObject.Properties['publishedAt'] -and $rel.publishedAt) {
        if ($rel.publishedAt -is [datetime]) {
            $publishedAt = $rel.publishedAt.ToUniversalTime().ToString('yyyy-MM-ddTHH:mm:ssZ', [Globalization.CultureInfo]::InvariantCulture)
        } else {
            $publishedAt = [string]$rel.publishedAt
        }
    }

    $manifest = [ordered]@{
        '_about'     = 'Published GitHub release manifest for FileDO site landing pages (PAGE-STYLE 3.10). Generated by packaging/set-release-manifest.ps1 from the published release; never edited by hand.'
        repo         = $repo
        tag          = $tag
        version      = $version
        date         = $date
        publishedAt  = $publishedAt
        releaseUrl   = "https://github.com/$repo/releases/tag/$tag"
        assets       = $assetsOut
    }

    $parentDir = Split-Path $OutPath -Parent
    if ($parentDir -and -not (Test-Path -LiteralPath $parentDir)) {
        New-Item -ItemType Directory -Force -Path $parentDir | Out-Null
    }
    $json = (($manifest | ConvertTo-Json -Depth 6) -replace "`r`n", "`n") + "`n"
    [IO.File]::WriteAllText($OutPath, $json, [Text.UTF8Encoding]::new($false))
    Write-Host "set-release-manifest: PASS ($tag -> $OutPath)" -ForegroundColor Green
    exit 0
} finally {
    Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
