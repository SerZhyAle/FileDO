#Requires -Version 7.0
<#
.SYNOPSIS
  Writes docs/sitemap.xml, the one render of the public page set (SP-0166 I2).

.DESCRIPTION
  docs/sitemap.xml is a render target: it is produced by this script and never edited by hand.

  The public page set is exactly what packaging/check-external-docs.ps1 calls "listed": the .html pages
  under docs/ that docs/DOCUMENT_REGISTRY.jsonl marks corpus "external" and role "source", minus the
  redirect stubs (http-equiv="refresh") and the pages that carry robots noindex. A page's URL comes from
  its path the way the gate maps it (docs/index.html is the site root, docs/<dir>/index.html is the
  folder, any other path is appended to the site base). The two scripts mirror each other's page-set
  logic on purpose, so that they cannot disagree about which pages are public.

  lastmod is the date of the page file's last commit (git log -1 --format=%cs), never the build time.
  A page that is untracked, or differs from HEAD, has no commit date that describes it yet: it falls
  back to the file's last-write date (UTC, yyyy-MM-dd), and the script prints which pages did. Run the
  script again after the commit and those dates become commit dates. priority and changefreq are
  ignored by Google and are left out.

  Order is deterministic: the landing page first, then ordinal by URL. The file is LF, UTF-8 without a
  byte order mark, and ends with a newline. Git is only read (rev-parse, ls-files, diff, log), with
  optional index locks off.

  -Check writes nothing. It compares the page set and the URLs (set and order) with the file and does
  NOT compare lastmod: a stale lastmod is the normal state between a regeneration and a commit. It also
  rejects what the generator never writes: a byte order mark, a CR, a missing final newline, and
  changefreq or priority elements.
  -SitemapPath points the write or the check at another file (the default is docs\sitemap.xml).

  Exit code: 0 = written or unchanged (-Check: the file matches), 1 = -Check found a difference,
  2 = could not verify (git, the registry or a listed page cannot be read). The last line says which.
#>
[CmdletBinding()]
param(
    [switch]$Check,
    [string]$SitemapPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent
$siteBase = 'https://serzhyale.github.io/FileDO/'
$registryPath = Join-Path $root 'docs\DOCUMENT_REGISTRY.jsonl'
if (-not $SitemapPath) { $SitemapPath = Join-Path $root 'docs\sitemap.xml' }
$env:GIT_OPTIONAL_LOCKS = '0'   # read-only means read-only: no opportunistic index refresh

function Stop-Unverified([string]$why) {
    Write-Host "write-sitemap: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

# Same mapping as Get-UrlFor in packaging/check-external-docs.ps1.
function Get-UrlFor([string]$rel) {
    $p = $rel.Substring('docs/'.Length)
    if ($p -eq 'index.html') { return $siteBase }
    if ($p.EndsWith('/index.html')) { return $siteBase + $p.Substring(0, $p.Length - 'index.html'.Length) }
    return $siteBase + $p
}

# Runs git against the repository and returns its stdout lines, or stops: nothing here works without git.
function Invoke-Git([string[]]$GitArguments) {
    $out = & git -C $root -c core.quotepath=false @GitArguments 2>&1
    if ($LASTEXITCODE -ne 0) { Stop-Unverified "git $($GitArguments -join ' ') failed: $((($out | Out-String)).Trim())" }
    return @($out | ForEach-Object { [string]$_ } | Where-Object { $_ })
}

# ---- Git must be readable (the generator needs it for lastmod; -Check holds the same line) ----
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Stop-Unverified "'git' is not on PATH" }
if (@(Invoke-Git @('rev-parse', '--is-inside-work-tree'))[0] -ne 'true') { Stop-Unverified "$root is not a git work tree" }

# ---- The public page set (mirrors packaging/check-external-docs.ps1, section "Inputs") ----
$registry = @()
try {
    foreach ($line in [IO.File]::ReadAllLines($registryPath)) { if ($line.Trim()) { $registry += , ($line | ConvertFrom-Json) } }
} catch { Stop-Unverified "the document registry cannot be read: $($_.Exception.Message)" }

$external = @($registry | Where-Object { $_.PSObject.Properties.Name -contains 'path' -and $_.corpus -eq 'external' -and $_.role -eq 'source' })
$listedByUrl = @{}   # url -> repo-relative path of the page that answers there
foreach ($r in $external) {
    $rel = ([string]$r.path) -replace '\\', '/'
    if ($rel -notmatch '^docs/.+\.html$') { continue }
    $full = Join-Path $root $rel
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { Stop-Unverified "$rel is missing" }
    $text = [IO.File]::ReadAllText($full)
    if ($null -eq $text) { $text = '' }
    if ($text -match '(?i)http-equiv\s*=\s*["'']refresh') { continue }   # a redirect stub is not a public page
    if ($text -match '(?is)<meta\b[^>]*\bname\s*=\s*["'']robots["''][^>]*\bcontent\s*=\s*["''][^"'']*noindex') { continue }   # served, kept out of the index
    $url = Get-UrlFor $rel
    if ($listedByUrl.ContainsKey($url)) { Stop-Unverified "$rel and $($listedByUrl[$url]) answer at the same URL $url" }
    $listedByUrl[$url] = $rel
}
if ($listedByUrl.Count -eq 0) { Stop-Unverified 'the registry names no public page' }

# Landing page first, then ordinal by URL.
$others = [System.Collections.Generic.List[string]]::new()
foreach ($u in $listedByUrl.Keys) { if ($u -ne $siteBase) { $others.Add($u) } }
$others.Sort([StringComparer]::Ordinal)
$orderedUrls = [System.Collections.Generic.List[string]]::new()
if ($listedByUrl.ContainsKey($siteBase)) { $orderedUrls.Add($siteBase) }
$orderedUrls.AddRange($others)

# ---- -Check: page set and URLs against the file, lastmod not compared ----
if ($Check) {
    $problems = [System.Collections.Generic.List[string]]::new()
    $fileUrls = [System.Collections.Generic.List[string]]::new()
    if (-not (Test-Path -LiteralPath $SitemapPath -PathType Leaf)) {
        $problems.Add("$SitemapPath does not exist")
    } else {
        # The bytes, once: a byte order mark, a CR or a missing final newline is a hand edit the XML parse would not see.
        $raw = [IO.File]::ReadAllBytes($SitemapPath)
        if ($raw.Length -ge 3 -and $raw[0] -eq 0xEF -and $raw[1] -eq 0xBB -and $raw[2] -eq 0xBF) { $problems.Add("$SitemapPath starts with a byte order mark (the generator writes none)") }
        if ($raw -contains 13) { $problems.Add("$SitemapPath contains CR characters (the generator writes LF only)") }
        if ($raw.Length -eq 0 -or $raw[$raw.Length - 1] -ne 10) { $problems.Add("$SitemapPath does not end with a newline (the generator ends the file with one)") }
        $doc = $null
        try { $doc = [xml][IO.File]::ReadAllText($SitemapPath) } catch { $problems.Add("$SitemapPath is not well-formed XML: $($_.Exception.Message)") }
        if ($doc) {
            $ns = [System.Xml.XmlNamespaceManager]::new($doc.NameTable)
            $ns.AddNamespace('s', 'http://www.sitemaps.org/schemas/sitemap/0.9')
            foreach ($node in $doc.SelectNodes('/s:urlset/s:url/s:loc', $ns)) { $fileUrls.Add([string]$node.InnerText) }
            if ($doc.SelectNodes('/s:urlset/s:url/s:changefreq | /s:urlset/s:url/s:priority', $ns).Count) { $problems.Add("$SitemapPath carries changefreq or priority, which the generator leaves out") }
        }
    }
    $seen = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    foreach ($u in $fileUrls) {
        if (-not $seen.Add($u)) { $problems.Add("lists $u twice") }
        elseif (-not $listedByUrl.ContainsKey($u)) { $problems.Add("lists $u, which is not a listed public page (a stub, a noindex page, or not in the registry)") }
    }
    foreach ($u in $orderedUrls) { if (-not $seen.Contains($u)) { $problems.Add("misses $u ($($listedByUrl[$u]))") } }
    if ($problems.Count -eq 0) {
        $same = $fileUrls.Count -eq $orderedUrls.Count
        for ($i = 0; $same -and $i -lt $orderedUrls.Count; $i++) { if ($fileUrls[$i] -cne $orderedUrls[$i]) { $same = $false } }
        if (-not $same) { $problems.Add("lists the right URLs in an order other than the generator's (landing page first, then ordinal by URL)") }
    }
    if ($problems.Count) {
        foreach ($p in $problems) { Write-Host "  DIFF  $p" -ForegroundColor Red }
        Write-Host "write-sitemap -Check: DIFFERS ($($problems.Count) problems) - run packaging/write-sitemap.ps1" -ForegroundColor Red
        exit 1
    }
    Write-Host "write-sitemap -Check: OK ($($orderedUrls.Count) URLs match the public page set; lastmod not compared)" -ForegroundColor Green
    exit 0
}

# ---- lastmod: the page file's last commit date; a file git has no commit date for falls back to its last-write date ----
$tracked = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
foreach ($f in (Invoke-Git @('ls-files', '--', 'docs'))) { [void]$tracked.Add($f) }
$changed = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
foreach ($f in (Invoke-Git @('diff', '--name-only', '--no-renames', 'HEAD', '--', 'docs'))) { [void]$changed.Add($f) }

$invariant = [Globalization.CultureInfo]::InvariantCulture
$fallback = [System.Collections.Generic.List[string]]::new()
$lastmod = @{}
foreach ($url in $orderedUrls) {
    $rel = $listedByUrl[$url]
    $date = $null
    if ($tracked.Contains($rel) -and -not $changed.Contains($rel)) {
        $date = (Invoke-Git @('log', '-1', '--format=%cs', '--', $rel) | Select-Object -First 1)
        if ($date -and $date -notmatch '^\d{4}-\d{2}-\d{2}$') { Stop-Unverified "git log gave '$date' for $rel, not a yyyy-MM-dd date" }
    }
    if (-not $date) {
        $date = [IO.File]::GetLastWriteTimeUtc((Join-Path $root $rel)).ToString('yyyy-MM-dd', $invariant)
        $fallback.Add("$rel  $date")
    }
    $lastmod[$url] = $date
}

# ---- Render ----
$sb = [System.Text.StringBuilder]::new()
[void]$sb.Append("<?xml version=`"1.0`" encoding=`"UTF-8`"?>`n")
[void]$sb.Append("<!--`n")
[void]$sb.Append("  Generated by packaging/write-sitemap.ps1 - do not edit by hand; change a page and run the script again.`n")
[void]$sb.Append("  It lists the canonical public pages only: the .html pages the document registry marks external,`n")
[void]$sb.Append("  minus redirect stubs (http-equiv refresh) and noindex pages. The /ru/ and /ua/ index files are`n")
[void]$sb.Append("  redirect stubs that canonicalise to the site root, so they are absent on purpose - listing a`n")
[void]$sb.Append("  non-canonical redirect is what makes a sitemap lie. Each lastmod is the date of the page file's`n")
[void]$sb.Append("  last commit; a page that is untracked or differs from HEAD carries its last-write date (UTC) until`n")
[void]$sb.Append("  it is committed and the script is run again.`n")
[void]$sb.Append("  packaging/check-external-docs.ps1 holds this list to the public page set both ways.`n")
[void]$sb.Append("-->`n")
[void]$sb.Append("<urlset xmlns=`"http://www.sitemaps.org/schemas/sitemap/0.9`">`n")
foreach ($url in $orderedUrls) {
    [void]$sb.Append("  <url>`n")
    [void]$sb.Append("    <loc>$([Security.SecurityElement]::Escape($url))</loc>`n")
    [void]$sb.Append("    <lastmod>$($lastmod[$url])</lastmod>`n")
    [void]$sb.Append("  </url>`n")
}
[void]$sb.Append("</urlset>`n")
$bytes = [System.Text.UTF8Encoding]::new($false).GetBytes($sb.ToString())

$existing = $null
if (Test-Path -LiteralPath $SitemapPath -PathType Leaf) { $existing = [IO.File]::ReadAllBytes($SitemapPath) }
$unchanged = $null -ne $existing -and [System.Linq.Enumerable]::SequenceEqual([byte[]]$existing, [byte[]]$bytes)
if (-not $unchanged) { [IO.File]::WriteAllBytes($SitemapPath, $bytes) }

if ($fallback.Count) {
    Write-Host "write-sitemap: $($fallback.Count) page(s) used the last-write date (UTC) because they are untracked or differ from HEAD:"
    foreach ($f in $fallback) { Write-Host "  $f" }
} else {
    Write-Host 'write-sitemap: every lastmod is a commit date'
}
$verb = if ($unchanged) { 'unchanged' } else { 'wrote' }
Write-Host "write-sitemap: $verb $SitemapPath - $($orderedUrls.Count) URLs" -ForegroundColor Green
exit 0
