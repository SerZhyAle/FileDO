#Requires -Version 7.0
<#
.SYNOPSIS
  Renders release facts from packaging/release-manifest.json into the five landing pages (PAGE-STYLE 3.10).

.DESCRIPTION
  Reads packaging/release-manifest.json and updates (or checks with -Check) the static markup of:
    docs/index.html
    docs/de/index.html
    docs/fr/index.html
    docs/ru/index.html
    docs/uk/index.html

  Updates #heroSetupLink, #setupLink, #setupFile (unhidden), #releaseTagLink, #releaseVersion,
  #releaseDate, #setupName, #setupMb, #setupHash, #zipLink, and #msiLink while keeping #releaseLink
  on the durable /releases/latest URL. Never touches data-l translation spans.

  Exit codes (CHECK-VERDICT):
    0  rendered (or -Check matched all 5 landing pages)
    1  defect found (-Check mismatch or missing required element)
    2  could not verify (manifest or page missing/unreadable)
#>
[CmdletBinding(PositionalBinding = $false)]
param(
    [switch]$Check,
    [string]$ManifestPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0

$root = Split-Path $PSScriptRoot -Parent
if (-not $ManifestPath) { $ManifestPath = Join-Path $root 'packaging\release-manifest.json' }

function Stop-Fail([string]$why) {
    Write-Host "render-site-release: FAIL ($why)" -ForegroundColor Red
    exit 1
}

function Stop-Unverified([string]$why) {
    Write-Host "render-site-release: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

if (-not (Test-Path -LiteralPath $ManifestPath -PathType Leaf)) {
    Stop-Unverified "manifest is missing: $ManifestPath"
}

try {
    $manifest = [IO.File]::ReadAllText($ManifestPath) | ConvertFrom-Json
} catch {
    Stop-Unverified "manifest cannot be parsed: $($_.Exception.Message)"
}

if (-not $manifest -or -not $manifest.tag -or -not $manifest.version -or -not $manifest.date -or -not $manifest.releaseUrl -or -not $manifest.assets) {
    Stop-Unverified 'manifest is missing required top-level fields'
}
if ($manifest.tag -cnotmatch '^v\d{10}$' -or $manifest.version -cne $manifest.tag.Substring(1)) {
    Stop-Fail "manifest tag '$($manifest.tag)' and version '$($manifest.version)' disagree"
}
if ($manifest.date -cnotmatch '^\d{4}-\d{2}-\d{2}$') {
    Stop-Fail "manifest date '$($manifest.date)' is not yyyy-MM-dd"
}
foreach ($k in 'setup', 'zip', 'msi') {
    $a = $manifest.assets.$k
    if (-not $a -or -not $a.name -or -not $a.url -or -not $a.sha256Url -or -not $a.sizeMb) {
        Stop-Unverified "manifest assets.$k is incomplete"
    }
}

$durableUrl = "https://github.com/$($manifest.repo)/releases/latest"
$landingPages = @(
    'docs/index.html',
    'docs/de/index.html',
    'docs/fr/index.html',
    'docs/ru/index.html',
    'docs/uk/index.html'
)

$linkTargets = [ordered]@{
    heroSetupLink  = [string]$manifest.assets.setup.url
    setupLink      = [string]$manifest.assets.setup.url
    setupHash      = [string]$manifest.assets.setup.sha256Url
    zipLink        = [string]$manifest.assets.zip.url
    msiLink        = [string]$manifest.assets.msi.url
    releaseLink    = $durableUrl
    releaseTagLink = [string]$manifest.releaseUrl
}

function Set-ElementHref([string]$html, [string]$rel, [string]$id, [string]$target, [System.Collections.Generic.List[string]]$problems) {
    $pattern = "(?s)(<a\b[^>]*?\bid=`"$([regex]::Escape($id))`"[^>]*?\bhref=`")([^`"]*)(`")"
    $matches = [regex]::Matches($html, $pattern)
    if ($matches.Count -ne 1) {
        $problems.Add("${rel}: expected 1 <a id=""$id"" href=..>, found $($matches.Count)")
        return $html
    }
    return [regex]::Replace($html, $pattern, { param($m) $m.Groups[1].Value + $target + $m.Groups[3].Value }, 1)
}

function Set-ElementSingle([string]$html, [string]$rel, [string]$label, [string]$pattern, [scriptblock]$replacer, [System.Collections.Generic.List[string]]$problems) {
    $matches = [regex]::Matches($html, $pattern)
    if ($matches.Count -ne 1) {
        $problems.Add("${rel}: expected 1 $label, found $($matches.Count)")
        return $html
    }
    return [regex]::Replace($html, $pattern, $replacer, 1)
}

$problems = [System.Collections.Generic.List[string]]::new()
$updatedCount = 0

foreach ($rel in $landingPages) {
    $full = Join-Path $root ($rel -replace '/', '\')
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) {
        Stop-Unverified "$rel is missing"
    }
    $orig = [IO.File]::ReadAllText($full)
    $next = $orig

    foreach ($id in $linkTargets.Keys) {
        $next = Set-ElementHref $next $rel $id $linkTargets[$id] $problems
    }
    $next = Set-ElementSingle $next $rel '<span id="setupFile">' '(?s)<span\s+id="setupFile"(?:\s+hidden)?\s*>' { param($m) '<span id="setupFile">' } $problems
    $next = Set-ElementSingle $next $rel '<code id="releaseVersion">' '(?s)(<code\s+id="releaseVersion">)[^<]*(</code>)' { param($m) $m.Groups[1].Value + [string]$manifest.tag + $m.Groups[2].Value } $problems
    $next = Set-ElementSingle $next $rel '<time id="releaseDate">' '(?s)(<time\s+id="releaseDate"\s+datetime=")[^"]*(">)[^<]*(</time>)' { param($m) $m.Groups[1].Value + [string]$manifest.date + $m.Groups[2].Value + [string]$manifest.date + $m.Groups[3].Value } $problems
    $next = Set-ElementSingle $next $rel '<code id="setupName">' '(?s)(<code\s+id="setupName">)[^<]*(</code>)' { param($m) $m.Groups[1].Value + [string]$manifest.assets.setup.name + $m.Groups[2].Value } $problems
    $next = Set-ElementSingle $next $rel '<span id="setupMb">' '(?s)(<span\s+id="setupMb">)[^<]*(</span>)' { param($m) $m.Groups[1].Value + [string]$manifest.assets.setup.sizeMb + $m.Groups[2].Value } $problems

    if ($next -cne $orig) {
        if ($Check) {
            $problems.Add("${rel}: landing markup differs from $($manifest.tag) manifest")
        } else {
            [IO.File]::WriteAllText($full, ($next -replace "`r`n", "`n"), [Text.UTF8Encoding]::new($false))
            $updatedCount++
        }
    }
}

if ($problems.Count -gt 0) {
    foreach ($p in $problems) { Write-Host "  FAIL  $p" -ForegroundColor Red }
    Stop-Fail "$($problems.Count) problem(s)"
}

if ($Check) {
    Write-Host "render-site-release: PASS ($($landingPages.Count) landing pages match $($manifest.tag))" -ForegroundColor Green
} else {
    Write-Host "render-site-release: PASS ($($manifest.tag) -> $updatedCount of $($landingPages.Count) landing page(s) updated)" -ForegroundColor Green
}
exit 0
