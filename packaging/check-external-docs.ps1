#Requires -Version 7.0
<#
.SYNOPSIS
  Holds the published site and the five READMEs to DOC-EXTERNAL-QUALITY (SP-0062).

.DESCRIPTION
  The external corpus is what docs/DOCUMENT_REGISTRY.jsonl marks corpus "external": the pages under
  docs/ (a page that only redirects - http-equiv="refresh" - is a stub, not a public page) and the
  root READMEs. Links, anchors and images across it are packaging/check-internal-docs.ps1's walk;
  this script holds the rest, in one run so that one run names every defect:

    structure  rule 1. The guide hub links the glossary and the subject index; the subject index
               links every guide; the glossary carries an entry (id="term-<id>") for every term
               in the termbase.
    sitemap    rule 2. docs/sitemap.xml lists every public page exactly once, at the URL its path
               gives, and nothing else - no stub, no docs/contracts/, no page that is not on disk.
               There is no generator to regenerate it from, so it is verified against the page set
               instead; there is no search index - see the catalog proposal named in the pointer.
    seo        rule 5. Every public page: a <title> with the product name under 60 characters, a
               description under 160, canonical = its sitemap URL, og:type/url/title/description/
               image, a Twitter card, parseable JSON-LD, and hreflang x-default = the canonical
               (the locales share one URL - see the proposal). The per-locale titles and
               descriptions the page swaps in at runtime are held to the same lengths.
    locales    rule 3. Every run of sibling data-l spans carries ru, en and ua exactly once (the shared
               guide script carries de and fr too). The standalone locale pages docs/de/ and docs/fr/
               (SP-0154) mirror the runtime page of the same path group for group, one language each,
               and are read against that page's English. Every README translation has the section
               count of README.md. Freshness: the EN text of every span group and every README.md
               section is fingerprinted beside its translations in docs/translation-fingerprints.json.
               EN text whose fingerprint is not recorded fails - naming the locales left untouched as
               stale - until the translations are checked and the fingerprints re-recorded with -Record.
    terms      rule 4. docs/termbase.json: every term's per-locale form is used in that locale, and
               no forbidden synonym appears in that locale's prose (code excluded).
    screens    rule 6. A page whose <main> carries data-ui-steps shows at least one screenshot;
               every <img> on a public page has alt text, width and height, and a file under
               docs/assets/. The pictures are produced by packaging/capture-guide-screens.ps1.
    addresses  SITE-STRUCTURE rule 8 (SP-0153). packaging/site-held-addresses.json lists every address
               of the site held outside it (the program, the READMEs, the Store listing source, the
               release workflow). Each entry resolves to a public page or a forwarder stub, its anchor
               exists on that page, and every holder still carries it; an address found in a listed
               surface and missing from the list fails.
    positioning SITE-REPRESENTATION rules 1 and 2 (SP-0157). packaging/positioning-source.json is the one
               positioning source: the ordered pillars, and per surface (landing and its locale pages,
               the guides hub, the five READMEs, the Store listing source, the winget manifest) the
               region that lists them. Each region must name the pillars the surface owes and the pillars
               must occur in the source's order - the last word of one pillar before the first word of
               the next.
    releasenotes SITE-STRUCTURE rule 2 and 14 (SP-0158). docs/guides/release-notes.html and its de and fr pages
               carry one section id="v<stamp>" for every shipped release of the Version History in
               README.md, newest first, and none that README.md does not list. The page is written by
               hand from that history when the release's "What's new" is; this step is what stops the two
               drifting apart. "Unreleased" is not a release and never appears on the page.
    hygiene    rule 7 and the house style: no http:// link, no en or em dash and no three-dot
               ellipsis in prose (code, scripts and styles are quoted, not prose).

  -Record rewrites docs/translation-fingerprints.json from the current text and then checks. Run it
  only after reading the translations against the English that changed.

  Exit code (CHECK-VERDICT): 0 = pass, 1 = a defect was found, 2 = could not verify (a required file
  is missing or unreadable). The last line is the verdict.
#>
[CmdletBinding()]
param(
    [switch]$Record
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent
$siteBase = 'https://serzhyale.github.io/FileDO/'
$siteLangs = @('ru', 'en', 'ua')
$allLangs = @('ru', 'en', 'ua', 'de', 'fr')
$readmeLangs = [ordered]@{ 'README.ru.md' = 'ru'; 'README.ua.md' = 'ua'; 'README.de.md' = 'de'; 'README.fr.md' = 'fr' }
$registryPath = Join-Path $root 'docs\DOCUMENT_REGISTRY.jsonl'
$sitemapPath = Join-Path $root 'docs\sitemap.xml'
$termbasePath = Join-Path $root 'docs\termbase.json'
$printsPath = Join-Path $root 'docs\translation-fingerprints.json'
$heldPath = Join-Path $root 'packaging\site-held-addresses.json'
$positioningPath = Join-Path $root 'packaging\positioning-source.json'

function Stop-Unverified([string]$why) {
    Write-Host "external-docs: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

function Read-Text([string]$rel) {
    $full = Join-Path $root $rel
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { Stop-Unverified "$rel is missing" }
    $t = [IO.File]::ReadAllText($full)
    if ($null -eq $t) { return '' }
    return $t
}

# ---- Inputs --------------------------------------------------------------------------
$registry = @()
try {
    foreach ($line in [IO.File]::ReadAllLines($registryPath)) { if ($line.Trim()) { $registry += , ($line | ConvertFrom-Json) } }
} catch { Stop-Unverified "the document registry cannot be read: $($_.Exception.Message)" }
try { $termbase = [IO.File]::ReadAllText($termbasePath) | ConvertFrom-Json } catch { Stop-Unverified "docs/termbase.json cannot be read: $($_.Exception.Message)" }
try { [xml]$sitemap = [IO.File]::ReadAllText($sitemapPath) } catch { Stop-Unverified "docs/sitemap.xml cannot be read: $($_.Exception.Message)" }
try { $held = [IO.File]::ReadAllText($heldPath) | ConvertFrom-Json } catch { Stop-Unverified "packaging/site-held-addresses.json cannot be read: $($_.Exception.Message)" }
try { $positioning = [IO.File]::ReadAllText($positioningPath) | ConvertFrom-Json } catch { Stop-Unverified "packaging/positioning-source.json cannot be read: $($_.Exception.Message)" }
$prints = $null
if (-not $Record) {
    if (-not (Test-Path -LiteralPath $printsPath)) { Stop-Unverified 'docs/translation-fingerprints.json is missing (run with -Record once)' }
    try { $prints = [IO.File]::ReadAllText($printsPath) | ConvertFrom-Json -AsHashtable } catch { Stop-Unverified "docs/translation-fingerprints.json cannot be read: $($_.Exception.Message)" }
}

$external = @($registry | Where-Object { $_.PSObject.Properties.Name -contains 'path' -and $_.corpus -eq 'external' -and $_.role -eq 'source' })
$pages = [ordered]@{}   # public site pages: rel path -> raw text
$stubs = [System.Collections.Generic.List[string]]::new()
$unlisted = [System.Collections.Generic.HashSet[string]]::new()   # served pages that carry robots noindex (the not-found page): kept out of the sitemap
foreach ($r in $external) {
    $rel = ([string]$r.path) -replace '\\', '/'
    if ($rel -notmatch '^docs/.+\.html$') { continue }
    $text = Read-Text $rel
    if ($text -match '(?i)http-equiv\s*=\s*["'']refresh') { $stubs.Add($rel) }
    else {
        $pages[$rel] = $text
        if ($text -match '(?is)<meta\b[^>]*\bname\s*=\s*["'']robots["''][^>]*\bcontent\s*=\s*["''][^"'']*noindex') { [void]$unlisted.Add($rel) }
    }
}
$readmes = [ordered]@{}
foreach ($rel in @('README.md') + @($readmeLangs.Keys)) { $readmes[$rel] = Read-Text $rel }

$failures = [System.Collections.Generic.List[string]]::new()
$counts = [ordered]@{}
function Add-Section([string]$name, [scriptblock]$body) {
    $before = $failures.Count
    & $body
    $counts[$name] = $failures.Count - $before
}

# ---- Helpers -------------------------------------------------------------------------
# Line numbers by binary search over each text's newline offsets, computed once per text.
$lineIndex = [System.Collections.Generic.Dictionary[object, int[]]]::new([System.Collections.Generic.ReferenceEqualityComparer]::Instance)
function Get-LineAt([string]$text, [int]$index) {
    $starts = $null
    if (-not $lineIndex.TryGetValue($text, [ref]$starts)) {
        $starts = [int[]]@(foreach ($m in [regex]::Matches($text, "`n")) { $m.Index })
        $lineIndex[$text] = $starts
    }
    $i = [Array]::BinarySearch($starts, $index)
    if ($i -lt 0) { $i = -bnot $i }
    return 1 + $i
}

function Get-UrlFor([string]$rel) {
    $p = $rel.Substring('docs/'.Length)
    if ($p -eq 'index.html') { return $siteBase }
    if ($p.EndsWith('/index.html')) { return $siteBase + $p.Substring(0, $p.Length - 'index.html'.Length) }
    return $siteBase + $p
}

# A standalone locale page (SP-0154): docs/de/... and docs/fr/... are written in one language and mirror the
# runtime-localised page of the same path (PAGE-STYLE 4.2: a further locale is its own page, never an sza-lang value).
function Get-PageLocale([string]$rel) {
    if ($rel -match '^docs/(de|fr)/') { return $Matches[1] }
    return $null
}
function Get-SourceRel([string]$rel) { return $rel -replace '^docs/(?:de|fr)/', 'docs/' }

# Replaces every match with blanks of the same length, so indexes and line numbers still hold.
function Clear-Regions([string]$text, [string]$pattern) {
    return [regex]::Replace($text, $pattern, { param($m) [regex]::Replace($m.Value, '[^\n]', ' ') })
}

# What a reader reads: markup, scripts, styles and code gone, entities decoded.
function Get-HtmlProse([string]$html) {
    $t = Clear-Regions $html '(?is)<(script|style|pre|code)\b.*?</\1\s*>'
    $t = Clear-Regions $t '(?s)<!--.*?-->'
    $t = Clear-Regions $t '(?s)<[^>]*>'
    return [Net.WebUtility]::HtmlDecode($t)
}

function Get-MarkdownProse([string]$md) {
    $out = [System.Text.StringBuilder]::new()
    $fence = $null
    foreach ($line in ($md -split "`n")) {
        if ($fence) {
            if ($line -match "^\s{0,3}$([regex]::Escape($fence))") { $fence = $null }
            [void]$out.Append("`n"); continue
        }
        if ($line -match '^\s{0,3}(```+|~~~+)') { $fence = $Matches[1]; [void]$out.Append("`n"); continue }
        $l = [regex]::Replace($line, '(`+)(?:(?!\1).)+?\1', '')
        $l = [regex]::Replace($l, '\]\([^)]*\)', ']')
        $l = [regex]::Replace($l, '<[^>]+>', '')
        [void]$out.Append($l).Append("`n")
    }
    return $out.ToString()
}

function Get-Fingerprint([string]$s) {
    $norm = ([regex]::Replace($s, '\s+', ' ')).Trim()
    $bytes = [Security.Cryptography.SHA256]::HashData([Text.Encoding]::UTF8.GetBytes($norm))
    return ([Convert]::ToHexString($bytes)).Substring(0, 16).ToLowerInvariant()
}

# The outermost data-l spans of a page, in order, as groups of siblings: two spans belong to one
# group when only whitespace separates them and the second one's language is not in it yet.
function Get-SpanGroups([string]$text) {
    $groups = [System.Collections.Generic.List[object]]::new()
    $depth = 0
    $open = $null
    $current = $null
    $lastEnd = -1
    foreach ($m in [regex]::Matches($text, '(?i)<span\b[^>]*>|</span\s*>')) {
        if ($m.Value.StartsWith('</')) {
            $depth--
            if ($open -and $depth -eq $open.Depth) {
                $inner = $text.Substring($open.InnerStart, $m.Index - $open.InnerStart)
                $gap = if ($lastEnd -ge 0 -and $open.Start -ge $lastEnd) { $text.Substring($lastEnd, $open.Start - $lastEnd) } else { 'x' }
                if (-not $current -or $gap.Trim() -or $current.Langs.Contains($open.Lang)) {
                    $current = @{ Line = (Get-LineAt $text $open.Start); Langs = [ordered]@{} }
                    $groups.Add($current)
                }
                $current.Langs[$open.Lang] = $inner
                $lastEnd = $m.Index + $m.Length
                $open = $null
            }
            continue
        }
        if (-not $open -and $m.Value -match 'data-l\s*=\s*["''](\w+)["'']') {
            $open = @{ Lang = $Matches[1]; Start = $m.Index; InnerStart = $m.Index + $m.Length; Depth = $depth }
        }
        $depth++
    }
    return , $groups
}

# README sections: what precedes the first "## " heading, then one per "## " heading.
function Get-ReadmeSections([string]$md) {
    $sections = [System.Collections.Generic.List[string]]::new()
    $sb = [System.Text.StringBuilder]::new()
    $fence = $null
    foreach ($line in ($md -split "`n")) {
        if (-not $fence -and $line -match '^\s{0,3}(```+|~~~+)') { $fence = $Matches[1] }
        elseif ($fence -and $line -match "^\s{0,3}$([regex]::Escape($fence))") { $fence = $null }
        elseif (-not $fence -and $line -match '^## ') { $sections.Add($sb.ToString()); [void]$sb.Clear() }
        [void]$sb.Append($line).Append("`n")
    }
    $sections.Add($sb.ToString())
    return , $sections
}

$groupsByPage = [ordered]@{}
foreach ($rel in $pages.Keys) { $groupsByPage[$rel] = Get-SpanGroups $pages[$rel] }
$guideJs = 'docs/guides/guide.js'
$groupsByPage[$guideJs] = Get-SpanGroups (Read-Text $guideJs)
# The languages one run of sibling spans must carry: a locale page is one language, the shared guide script
# carries every language (its labels are injected into runtime pages and into locale pages alike).
function Get-RequiredLangs([string]$rel) {
    $loc = Get-PageLocale $rel
    if ($loc) { return @($loc) }
    if ($rel -eq $guideJs) { return $allLangs }
    return $siteLangs
}
$sectionsByReadme = [ordered]@{}
foreach ($rel in $readmes.Keys) { $sectionsByReadme[$rel] = Get-ReadmeSections $readmes[$rel] }

# ---- -Record ---------------------------------------------------------------------------
if ($Record) {
    $out = [ordered]@{
        '_about' = 'Translation freshness record (DOC-EXTERNAL-QUALITY rule 3). Rewritten by packaging/check-external-docs.ps1 -Record after the translations were read against the English; never edited by hand.'
        pages    = [ordered]@{}
        readmes  = [ordered]@{}
    }
    foreach ($rel in $groupsByPage.Keys) {
        $loc = Get-PageLocale $rel
        $units = @(if ($loc) {
            # A locale page is read against the runtime page it mirrors: unit i holds the source's English and its own text.
            $srcGroups = @($groupsByPage[(Get-SourceRel $rel)])
            for ($i = 0; $i -lt @($groupsByPage[$rel]).Count; $i++) {
                $u = [ordered]@{}
                if ($i -lt $srcGroups.Count -and $srcGroups[$i].Langs.Contains('en')) { $u['en'] = Get-Fingerprint $srcGroups[$i].Langs['en'] }
                if (@($groupsByPage[$rel])[$i].Langs.Contains($loc)) { $u[$loc] = Get-Fingerprint @($groupsByPage[$rel])[$i].Langs[$loc] }
                $u
            }
        } else {
            foreach ($g in $groupsByPage[$rel]) {
                $u = [ordered]@{}
                foreach ($l in (Get-RequiredLangs $rel)) { if ($g.Langs.Contains($l)) { $u[$l] = Get-Fingerprint $g.Langs[$l] } }
                $u
            }
        })
        $out.pages[$rel] = $units
    }
    $en = $sectionsByReadme['README.md']
    $units = @(for ($i = 0; $i -lt $en.Count; $i++) {
        $u = [ordered]@{ en = Get-Fingerprint $en[$i] }
        foreach ($f in $readmeLangs.Keys) { $s = $sectionsByReadme[$f]; if ($i -lt $s.Count) { $u[$readmeLangs[$f]] = Get-Fingerprint $s[$i] } }
        $u
    })
    $out.readmes['README.md'] = $units
    [IO.File]::WriteAllText($printsPath, (($out | ConvertTo-Json -Depth 6) -replace "`r`n", "`n") + "`n", [Text.UTF8Encoding]::new($false))
    Write-Host "  recorded  $(@($groupsByPage.Values | ForEach-Object { $_.Count } | Measure-Object -Sum).Sum) span groups, $($en.Count) README sections -> docs/translation-fingerprints.json"
    $prints = [IO.File]::ReadAllText($printsPath) | ConvertFrom-Json -AsHashtable
}

# ---- structure (rule 1) ---------------------------------------------------------------------
$terms = @($termbase.terms)
Add-Section 'structure' {
    # The guide set is held once per published locale: the runtime pages and each standalone locale tree.
    foreach ($prefix in 'docs/', 'docs/de/', 'docs/fr/') {
        $hub = "${prefix}guides/index.html"
        $glossary = "${prefix}guides/glossary.html"
        $topics = "${prefix}guides/topics.html"
        foreach ($need in $hub, $glossary, $topics) { if (-not $pages.Contains($need)) { $failures.Add("$need is not a declared public page") } }
        if ($pages.Contains($hub)) {
            foreach ($href in 'glossary.html', 'topics.html') { if ($pages[$hub] -notmatch "href=[""']$([regex]::Escape($href))[""'#]") { $failures.Add("$hub does not link $href") } }
        }
        if ($pages.Contains($topics)) {
            foreach ($rel in $pages.Keys) {
                if (-not $rel.StartsWith("${prefix}guides/") -or $rel -notmatch '/guides/([^/]+\.html)$' -or $rel -in $hub, $topics) { continue }
                $name = $Matches[1]
                if ($pages[$topics] -notmatch "href=[""']$([regex]::Escape($name))[""'#]") { $failures.Add("$topics does not index $name") }
            }
        }
        if ($pages.Contains($glossary)) {
            foreach ($t in $terms) { if ($pages[$glossary] -notmatch "id=[""']term-$([regex]::Escape($t.id))[""']") { $failures.Add("$glossary has no entry id=""term-$($t.id)"" for termbase term '$($t.id)'") } }
        }
    }
    # A locale tree mirrors the runtime page set exactly: a page in one and not the other is a language control that leads nowhere.
    foreach ($rel in @($pages.Keys)) {
        $loc = Get-PageLocale $rel
        if ($loc -and -not $pages.Contains((Get-SourceRel $rel))) { $failures.Add("$rel mirrors no runtime page ($(Get-SourceRel $rel) is not a public page)") }
        if (-not $loc -and -not $unlisted.Contains($rel)) {
            foreach ($l in 'de', 'fr') { $m = $rel -replace '^docs/', "docs/$l/"; if (-not $pages.Contains($m)) { $failures.Add("$rel has no $l page at $m (the declared locale set is ru, en, ua, de, fr)") } }
        }
    }
}

# ---- sitemap (rule 2) ------------------------------------------------------------------------
Add-Section 'sitemap' {
    $want = @{}
    foreach ($rel in $pages.Keys) { if (-not $unlisted.Contains($rel)) { $want[(Get-UrlFor $rel)] = $rel } }
    $seen = @{}
    $ns = [System.Xml.XmlNamespaceManager]::new($sitemap.NameTable)
    $ns.AddNamespace('s', 'http://www.sitemaps.org/schemas/sitemap/0.9')
    foreach ($u in $sitemap.SelectNodes('/s:urlset/s:url', $ns)) {
        $loc = [string]$u.SelectSingleNode('s:loc', $ns).InnerText
        $mod = $u.SelectSingleNode('s:lastmod', $ns)
        if ($seen.ContainsKey($loc)) { $failures.Add("docs/sitemap.xml lists $loc twice"); continue }
        $seen[$loc] = $true
        if (-not $want.ContainsKey($loc)) { $failures.Add("docs/sitemap.xml lists $loc, which is not a listed public page (a stub, a noindex page, docs/contracts/, or not on disk)") }
        if (-not $mod -or $mod.InnerText -notmatch '^\d{4}-\d{2}-\d{2}$') { $failures.Add("docs/sitemap.xml entry $loc has no yyyy-mm-dd lastmod") }
    }
    foreach ($url in $want.Keys) { if (-not $seen.ContainsKey($url)) { $failures.Add("$($want[$url]) is a public page absent from docs/sitemap.xml (add <loc>$url</loc>)") } }
    foreach ($s in $stubs) {
        if ((Read-Text $s) -notmatch "rel=[""']canonical[""'][^>]*href=[""']$([regex]::Escape($siteBase))[""']") { $failures.Add("$s is a redirect stub without a canonical link to the site root") }
    }
}

# ---- seo (rule 5) ----------------------------------------------------------------------------
function Get-MetaContent([string]$head, [string]$attr, [string]$name) {
    foreach ($m in [regex]::Matches($head, '(?is)<meta\b[^>]*>')) {
        if ($m.Value -match "(?is)\b$attr\s*=\s*[""']$([regex]::Escape($name))[""']" -and $m.Value -match '(?is)\bcontent\s*=\s*"([^"]*)"') { return [Net.WebUtility]::HtmlDecode($Matches[1]) }
    }
    return $null
}
function Get-LinkHref([string]$head, [string]$pattern) {
    foreach ($m in [regex]::Matches($head, '(?is)<link\b[^>]*>')) {
        if ($m.Value -match $pattern -and $m.Value -match '(?is)\bhref\s*=\s*"([^"]*)"') { return $Matches[1] }
    }
    return $null
}
Add-Section 'seo' {
    foreach ($rel in $pages.Keys) {
        $text = $pages[$rel]
        $url = Get-UrlFor $rel
        $h = [regex]::Match($text, '(?is)<head\b.*?</head>')
        if (-not $h.Success) { $failures.Add("$rel has no <head>"); continue }
        $head = $h.Value
        $title = [regex]::Match($head, '(?is)<title>(.*?)</title>')
        if (-not $title.Success) { $failures.Add("$rel has no <title>") }
        else {
            $t = [Net.WebUtility]::HtmlDecode($title.Groups[1].Value.Trim())
            if ($t -notmatch 'FileDO') { $failures.Add("$rel <title> '$t' does not name FileDO") }
            if ($t.Length -ge 60) { $failures.Add("$rel <title> is $($t.Length) characters (under 60)") }
        }
        $desc = Get-MetaContent $head 'name' 'description'
        if (-not $desc) { $failures.Add("$rel has no meta description") }
        elseif ($desc.Length -ge 160) { $failures.Add("$rel meta description is $($desc.Length) characters (under 160)") }
        if ($unlisted.Contains($rel)) {
            # A noindex page (the not-found page) has no canonical, share card or structured data: it must not be indexed at all.
            if ($head -match '(?is)<link\b[^>]*rel\s*=\s*"canonical"') { $failures.Add("$rel is noindex but declares a canonical") }
        } else {
            $canon = Get-LinkHref $head '(?i)rel\s*=\s*"canonical"'
            if ($canon -ne $url) { $failures.Add("$rel canonical is '$canon', want '$url'") }
            foreach ($p in 'og:type', 'og:url', 'og:title', 'og:description', 'og:image') {
                $v = Get-MetaContent $head 'property' $p
                if (-not $v) { $failures.Add("$rel has no $p") }
                elseif ($p -eq 'og:url' -and $v -ne $url) { $failures.Add("$rel og:url is '$v', want '$url'") }
                elseif ($p -eq 'og:image' -and $v -notmatch '^https://') { $failures.Add("$rel og:image is not an absolute https URL") }
            }
            if (-not (Get-MetaContent $head 'name' 'twitter:card')) { $failures.Add("$rel has no twitter:card") }
            # A runtime page is its own x-default; a locale page names the runtime page it mirrors. Every page lists de and fr.
            $loc = Get-PageLocale $rel
            $xdWant = if ($loc) { Get-UrlFor (Get-SourceRel $rel) } else { $url }
            $xd = Get-LinkHref $head '(?i)hreflang\s*=\s*"x-default"'
            if ($xd -ne $xdWant) { $failures.Add("$rel hreflang x-default is '$xd', want '$xdWant'") }
            $srcForAlt = if ($loc) { Get-SourceRel $rel } else { $rel }
            foreach ($l in 'de', 'fr') {
                $wantAlt = (Get-UrlFor $srcForAlt) -replace [regex]::Escape($siteBase), "$siteBase$l/"
                $alt = Get-LinkHref $head "(?i)hreflang\s*=\s*""$l"""
                if ($alt -ne $wantAlt) { $failures.Add("$rel hreflang $l is '$alt', want '$wantAlt'") }
            }
            if ($loc) {
                $htmlTag = [regex]::Match($text, '(?is)<html\b[^>]*>').Value
                if ($htmlTag -notmatch "\blang\s*=\s*""$loc""") { $failures.Add("$rel is a $loc page but its <html> lang is not '$loc'") }
                if ($htmlTag -notmatch "\bdata-page-lang\s*=\s*""$loc""") { $failures.Add("$rel is a $loc page but its <html> has no data-page-lang=""$loc""") }
            }
            $ld = [regex]::Matches($head, '(?is)<script\b[^>]*type\s*=\s*"application/ld\+json"[^>]*>(.*?)</script>')
            if (-not $ld.Count) { $failures.Add("$rel has no JSON-LD") }
            foreach ($m in $ld) {
                try { $j = $m.Groups[1].Value | ConvertFrom-Json; if ([string]$j.'@context' -ne 'https://schema.org') { $failures.Add("$rel JSON-LD @context is not https://schema.org") } }
                catch { $failures.Add("$rel JSON-LD does not parse: $($_.Exception.Message)") }
            }
        }
        # The per-locale text the page swaps in at runtime (guide.js guideMeta, the landing's meta).
        # A locale page is one language: its static title and description are all it has to say, and its guide script
        # keeps only its own entry.
        $loc = Get-PageLocale $rel
        $rt = [regex]::Match($text, '(?s)(?:window\.guideMeta|var meta)\s*=\s*\{(.*?)\n\s*\};')
        if (-not $rt.Success) { if (-not $loc) { $failures.Add("$rel has no runtime per-locale title and description") }; continue }
        foreach ($l in $(if ($loc) { @($loc) } else { $siteLangs })) {
            $b = [regex]::Match($rt.Groups[1].Value, "(?s)\b$l\s*:\s*\{(.*?)\}")
            if (-not $b.Success) { $failures.Add("$rel runtime meta has no '$l' entry"); continue }
            $rtTitle = [regex]::Match($b.Groups[1].Value, '(?s)\b(?:t|title)\s*:\s*"([^"]*)"')
            $rtDesc = [regex]::Match($b.Groups[1].Value, '(?s)\b(?:d|description)\s*:\s*"([^"]*)"')
            if (-not $rtTitle.Success -or $rtTitle.Groups[1].Value -notmatch 'FileDO' -or $rtTitle.Groups[1].Value.Length -ge 60) { $failures.Add("$rel runtime $l title must name FileDO in under 60 characters: '$($rtTitle.Groups[1].Value)'") }
            if (-not $rtDesc.Success -or $rtDesc.Groups[1].Value.Length -lt 50 -or $rtDesc.Groups[1].Value.Length -ge 160) { $failures.Add("$rel runtime $l description must be a summary of 50-159 characters: $($rtDesc.Groups[1].Value.Length)") }
            if ($l -eq 'en' -and $rtDesc.Success -and $desc -and $rtDesc.Groups[1].Value -ne $desc) { $failures.Add("$rel runtime en description differs from the static meta description") }
        }
    }
}

# ---- locales (rule 3) ------------------------------------------------------------------------
$groupTotal = 0
Add-Section 'locales' {
    foreach ($rel in $groupsByPage.Keys) {
        $recorded = @()
        if ($prints -and $prints.pages.ContainsKey($rel)) { $recorded = @($prints.pages[$rel]) }
        $loc = Get-PageLocale $rel
        $need = Get-RequiredLangs $rel
        $byEn = @{}
        $byLang = @{}
        foreach ($l in $allLangs) { $byLang[$l] = @{} }
        foreach ($u in $recorded) {
            if ($u.ContainsKey('en')) { $byEn[$u.en] = $true }
            foreach ($l in $allLangs) { if ($u.ContainsKey($l)) { $byLang[$l][$u[$l]] = $true } }
        }
        $own = @($groupsByPage[$rel])
        $srcGroups = @()
        if ($loc) {
            # A locale page mirrors its runtime page span for span: the same number of groups, in the same order.
            $srcGroups = @($groupsByPage[(Get-SourceRel $rel)])
            if ($own.Count -ne $srcGroups.Count) { $failures.Add("$rel has $($own.Count) span groups, $(Get-SourceRel $rel) has $($srcGroups.Count) - the locale page must mirror it") }
        }
        for ($i = 0; $i -lt $own.Count; $i++) {
            $g = $own[$i]
            $script:groupTotal++
            $missing = @($need | Where-Object { -not $g.Langs.Contains($_) })
            $extra = @($g.Langs.Keys | Where-Object { $_ -notin $need })
            if ($missing.Count -or $extra.Count) {
                $failures.Add("${rel}:$($g.Line) span group has $((@($g.Langs.Keys)) -join '/'), needs $($need -join '/') exactly once")
                continue
            }
            if ($loc) {
                if ($i -ge $srcGroups.Count -or -not $srcGroups[$i].Langs.Contains('en')) { continue }
                $en = Get-Fingerprint $srcGroups[$i].Langs['en']
                if ($byEn.ContainsKey($en)) { continue }
                if ($byLang[$loc].ContainsKey((Get-Fingerprint $g.Langs[$loc]))) { $failures.Add("${rel}:$($g.Line) the English of $(Get-SourceRel $rel) changed and $loc did not - stale translation") }
                else { $failures.Add("${rel}:$($g.Line) new or changed text is not recorded - read the translation, then run with -Record") }
                continue
            }
            $en = Get-Fingerprint $g.Langs['en']
            if ($byEn.ContainsKey($en)) { continue }
            $stale = @($need | Where-Object { $_ -ne 'en' -and $byLang[$_].ContainsKey((Get-Fingerprint $g.Langs[$_])) })
            if ($stale.Count) { $failures.Add("${rel}:$($g.Line) the English changed and $($stale -join ', ') did not - stale translation") }
            else { $failures.Add("${rel}:$($g.Line) new or changed text is not recorded - read the translations, then run with -Record") }
        }
    }
    $en = $sectionsByReadme['README.md']
    $recorded = @()
    if ($prints -and $prints.readmes.ContainsKey('README.md')) { $recorded = @($prints.readmes['README.md']) }
    foreach ($f in $readmeLangs.Keys) {
        if ($sectionsByReadme[$f].Count -ne $en.Count) { $failures.Add("$f has $($sectionsByReadme[$f].Count - 1) '## ' sections, README.md has $($en.Count - 1)") }
    }
    for ($i = 0; $i -lt $en.Count; $i++) {
        $head = if ($i -eq 0) { '(top)' } else { ($en[$i] -split "`n")[0].Trim() }
        $cur = Get-Fingerprint $en[$i]
        if ($i -lt $recorded.Count -and $recorded[$i].en -eq $cur) { continue }
        $stale = @(foreach ($f in $readmeLangs.Keys) {
            $l = $readmeLangs[$f]
            if ($i -lt $recorded.Count -and $i -lt $sectionsByReadme[$f].Count -and $recorded[$i].ContainsKey($l) -and $recorded[$i][$l] -eq (Get-Fingerprint $sectionsByReadme[$f][$i])) { $f }
        })
        if ($stale.Count) { $failures.Add("README.md section $i $head changed and $($stale -join ', ') did not - stale translation") }
        else { $failures.Add("README.md section $i $head is not recorded - read the translations, then run with -Record") }
    }
}

# ---- terms (rule 4) --------------------------------------------------------------------------
# Prose per locale, with where it came from, so a hit names file and line.
$prose = @{}
foreach ($l in 'en', 'ru', 'ua', 'de', 'fr') { $prose[$l] = [System.Collections.Generic.List[object]]::new() }
foreach ($rel in $groupsByPage.Keys) {
    foreach ($g in $groupsByPage[$rel]) {
        foreach ($l in $g.Langs.Keys) { if ($prose.ContainsKey($l)) { $prose[$l].Add(@{ Where = "${rel}:$($g.Line)"; Text = (Get-HtmlProse $g.Langs[$l]) }) } }
    }
}
$prose['en'].Add(@{ Where = 'README.md'; Text = (Get-MarkdownProse $readmes['README.md']) })
foreach ($f in $readmeLangs.Keys) { $prose[$readmeLangs[$f]].Add(@{ Where = $f; Text = (Get-MarkdownProse $readmes[$f]) }) }

Add-Section 'terms' {
    foreach ($t in $terms) {
        foreach ($l in @($t.forms.PSObject.Properties.Name)) {
            if (-not $prose.ContainsKey($l)) { $failures.Add("docs/termbase.json term '$($t.id)' has a form for unknown locale '$l'"); continue }
            $re = "(?i)(?<![\p{L}\p{Nd}])$([regex]::Escape([string]$t.forms.$l))"
            if (-not @($prose[$l] | Where-Object { $_.Text -match $re }).Count) { $failures.Add("docs/termbase.json term '$($t.id)': the $l form '$($t.forms.$l)' is used nowhere in the $l corpus") }
        }
        if (-not $t.PSObject.Properties['forbidden']) { continue }
        foreach ($l in @($t.forbidden.PSObject.Properties.Name)) {
            foreach ($bad in @($t.forbidden.$l)) {
                $re = [regex]::new("(?<![\p{L}\p{Nd}])(?:$bad)", 'IgnoreCase')
                foreach ($p in $prose[$l]) {
                    foreach ($m in $re.Matches($p.Text)) {
                        $where = if ($p.Where -match ':\d+$') { $p.Where } else { "$($p.Where):$(Get-LineAt $p.Text $m.Index)" }
                        $failures.Add("$where '$($m.Value)' is a forbidden $l synonym (term '$($t.id)': $($t.concept))")
                    }
                }
            }
        }
    }
}

# ---- screens (rule 6) ------------------------------------------------------------------------
$imgTotal = 0
Add-Section 'screens' {
    foreach ($rel in $pages.Keys) {
        $text = $pages[$rel]
        $imgs = [regex]::Matches($text, '(?is)<img\b[^>]*>')
        $script:imgTotal += $imgs.Count
        if ($text -match '(?is)<main\b[^>]*\bdata-ui-steps\b' -and -not $imgs.Count) { $failures.Add("$rel is a multi-step UI guide (data-ui-steps) with no screenshot") }
        foreach ($m in $imgs) {
            $line = Get-LineAt $text $m.Index
            if ($m.Value -notmatch '(?is)\balt\s*=\s*"[^"]*\S[^"]*"') { $failures.Add("${rel}:$line <img> has no alt text") }
            if ($m.Value -notmatch '(?is)\bwidth\s*=\s*"\d+"' -or $m.Value -notmatch '(?is)\bheight\s*=\s*"\d+"') { $failures.Add("${rel}:$line <img> has no width and height") }
            if ($m.Value -notmatch '(?is)\bsrc\s*=\s*"([^"]+)"') { $failures.Add("${rel}:$line <img> has no src"); continue }
            $src = $Matches[1]
            if ($src -match '^[a-z]+:|^//') { $failures.Add("${rel}:$line <img> '$src' is remote - vendor it under docs/assets/"); continue }
            $target = [IO.Path]::GetRelativePath($root, [IO.Path]::GetFullPath((Join-Path $root (Join-Path (Split-Path $rel) $src)))) -replace '\\', '/'
            if ($target -notmatch '^docs/assets/') { $failures.Add("${rel}:$line <img> '$src' is not under docs/assets/") }
            elseif (-not (Test-Path -LiteralPath (Join-Path $root $target) -PathType Leaf)) { $failures.Add("${rel}:$line <img> '$src' -> $target does not exist") }
            elseif ($target -match '\.png$' -and $m.Value -match '(?is)\bwidth\s*=\s*"(\d+)"' -and ($w = [int]$Matches[1]) -and $m.Value -match '(?is)\bheight\s*=\s*"(\d+)"') {
                # A re-taken capture at another size would otherwise be stretched silently: IHDR holds the truth.
                $hgt = [int]$Matches[1]
                $png = [IO.File]::ReadAllBytes((Join-Path $root $target))
                $pw = [int]$png[16] * 16777216 + [int]$png[17] * 65536 + [int]$png[18] * 256 + [int]$png[19]
                $ph = [int]$png[20] * 16777216 + [int]$png[21] * 65536 + [int]$png[22] * 256 + [int]$png[23]
                if ($pw -ne $w -or $ph -ne $hgt) { $failures.Add("${rel}:$line <img> '$src' says ${w}x$hgt, the file is ${pw}x$ph") }
            }
        }
    }
}

# ---- addresses (SITE-STRUCTURE rule 8, SP-0153) --------------------------------------------------
$heldTotal = 0
Add-Section 'addresses' {
    if ([string]$held.base -ne $siteBase) { $failures.Add("packaging/site-held-addresses.json base is '$($held.base)', want '$siteBase'") }
    $byUrl = @{}
    foreach ($rel in $pages.Keys) { $byUrl[(Get-UrlFor $rel)] = $rel }
    foreach ($rel in $stubs) { $byUrl[(Get-UrlFor $rel)] = $rel }   # a forwarder answers too

    # What each listed surface holds: address (relative to the base) -> line of its first mention.
    $addrRe = [regex]::new('https://serzhyale\.github\.io/FileDO(?:/[^\s)"''<>\]`\\|]*)?')
    $surfaces = [ordered]@{}
    foreach ($g in @($held.surfaces)) {
        $files = @(Get-ChildItem -Path (Join-Path $root $g) -File -ErrorAction SilentlyContinue)
        if (-not $files.Count) { $failures.Add("packaging/site-held-addresses.json surface '$g' matches no file"); continue }
        foreach ($f in $files) {
            $rel = [IO.Path]::GetRelativePath($root, $f.FullName) -replace '\\', '/'
            $text = [IO.File]::ReadAllText($f.FullName)
            $held1 = [ordered]@{}
            foreach ($m in $addrRe.Matches($text)) {
                $v = $m.Value.TrimEnd('.', ',', ';', ':', '!', '?')
                $a = if ($v.Length -lt $siteBase.Length) { '' } else { $v.Substring($siteBase.Length) }
                if (-not $held1.Contains($a)) { $held1[$a] = Get-LineAt $text $m.Index }
            }
            $surfaces[$rel] = $held1
        }
    }

    $listed = @{}
    foreach ($e in @($held.addresses)) {
        $addr = [string]$e.address
        $label = "$siteBase$addr"
        if ($listed.ContainsKey($addr)) { $failures.Add("packaging/site-held-addresses.json lists $label twice"); continue }
        $listed[$addr] = $true
        $script:heldTotal++
        $page, $anchor = if ($addr.Contains('#')) { $addr.Substring(0, $addr.IndexOf('#')), $addr.Substring($addr.IndexOf('#') + 1) } else { $addr, '' }
        $rel = $byUrl[$siteBase + $page]
        if (-not $rel) { $failures.Add("$label answers no public page and no forwarder stub") }
        elseif ($anchor) {
            if (-not $pages.Contains($rel)) { $failures.Add("$label names an anchor on $rel, which is a forwarder stub") }
            elseif ($pages[$rel] -notmatch "(?i)\b(?:id|name)\s*=\s*[""']$([regex]::Escape($anchor))[""']") { $failures.Add("$label : $rel has no id=""$anchor""") }
        }
        if (-not @($e.heldBy).Count) { $failures.Add("packaging/site-held-addresses.json entry $label names no holder") }
        foreach ($h in @($e.heldBy)) {
            if (-not $surfaces.Contains($h)) { $failures.Add("$label is said to be held by $h, which is not a file of a listed surface") }
            elseif (-not $surfaces[$h].Contains($addr)) { $failures.Add("$h no longer holds $label - drop it from heldBy, or drop the entry") }
        }
    }
    foreach ($h in $surfaces.Keys) {
        foreach ($a in $surfaces[$h].Keys) {
            if (-not $listed.ContainsKey($a)) { $failures.Add("${h}:$($surfaces[$h][$a]) holds $siteBase$a, which packaging/site-held-addresses.json does not list") }
            elseif (-not (@(($held.addresses | Where-Object { [string]$_.address -eq $a }).heldBy) -contains $h)) { $failures.Add("${h}:$($surfaces[$h][$a]) holds $siteBase$a, but the list does not name $h as a holder") }
        }
    }
}

# ---- positioning (SITE-REPRESENTATION rules 1 and 2, SP-0157) -------------------------------------
$positioningRegions = 0
Add-Section 'positioning' {
    $src = 'packaging/positioning-source.json'
    $ids = @($positioning.pillars | ForEach-Object { [string]$_.id })
    if (-not $ids.Count) { $failures.Add("$src lists no pillar"); return }
    if (@($ids | Select-Object -Unique).Count -ne $ids.Count) { $failures.Add("$src repeats a pillar id") }
    foreach ($s in @($positioning.surfaces)) {
        $file = [string]$s.file
        $full = Join-Path $root $file
        if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { $failures.Add("$src surface $file is not a file"); continue }
        $text = [IO.File]::ReadAllText($full)
        $anchors = $s.anchors
        $allowed = if ($s.PSObject.Properties.Name -contains 'allowMissing') { @($s.allowMissing | ForEach-Object { [string]$_ }) } else { @() }
        foreach ($k in $anchors.PSObject.Properties.Name) { if ($ids -notcontains $k) { $failures.Add("$src surface $file anchors the unknown pillar '$k'") } }
        $owed = @($ids | Select-Object -First ([int]$s.names) | Where-Object { $allowed -notcontains $_ })
        foreach ($scope in @($s.scopes)) {
            $m = [regex]::Match($text, [string]$scope)
            if (-not $m.Success) { $failures.Add("${file}: the region /$scope/ of the positioning source is not found"); continue }
            $script:positioningRegions++
            $at = "${file}:$(Get-LineAt $text $m.Index)"
            $span = @{}   # pillar id -> first and last index of its words in the region
            foreach ($id in $ids) {
                if ($anchors.PSObject.Properties.Name -notcontains $id) { continue }
                $hits = [regex]::Matches($m.Value, [string]$anchors.$id)
                if ($hits.Count) { $span[$id] = @($hits[0].Index, $hits[$hits.Count - 1].Index) }
            }
            foreach ($id in $owed) {
                if (-not $span.ContainsKey($id)) { $failures.Add("$at does not name the pillar '$id', which the positioning source puts in its first $($s.names)") }
            }
            $prev = $null
            foreach ($id in $ids) {
                if (-not $span.ContainsKey($id)) { continue }
                if ($prev -and $span[$id][0] -lt $span[$prev][1]) { $failures.Add("$at names '$id' before it is done with '$prev' - the order is $($ids -join ', ')") }
                $prev = $id
            }
        }
    }
}

# ---- release notes (SITE-STRUCTURE rules 2 and 14, SP-0158) -------------------------------------------
$releaseNotesCount = 0
Add-Section 'releasenotes' {
    $vh = [regex]::Match($readmes['README.md'], '(?ms)^## Version History\s*$(.*?)(?=^---\s*$|^## )')
    if (-not $vh.Success) { $failures.Add('README.md has no "## Version History" section to hold the release notes page against'); return }
    $want = @([regex]::Matches($vh.Groups[1].Value, '(?m)^\*\*(v\d{10})\*\*') | ForEach-Object { $_.Groups[1].Value })
    if (-not $want.Count) { $failures.Add('README.md Version History lists no release (**v<stamp>**)'); return }
    $script:releaseNotesCount = $want.Count
    foreach ($rel in 'docs/guides/release-notes.html', 'docs/de/guides/release-notes.html', 'docs/fr/guides/release-notes.html') {
        if (-not $pages.Contains($rel)) { $failures.Add("$rel is not a declared public page (the release notes page, SITE-STRUCTURE rule 2)"); continue }
        $have = @([regex]::Matches($pages[$rel], '<section class="section" id="(v\d{10})"') | ForEach-Object { $_.Groups[1].Value })
        foreach ($v in $want) { if ($v -notin $have) { $failures.Add("$rel has no section id=""$v"" for the release README.md lists") } }
        foreach ($v in $have) { if ($v -notin $want) { $failures.Add("$rel lists $v, which the README.md Version History does not") } }
        $shared = @($have | Where-Object { $_ -in $want })
        if (($shared -join ',') -ne ((@($want | Where-Object { $_ -in $have })) -join ',')) { $failures.Add("$rel lists the releases in a different order than README.md (newest first)") }
    }
}
# ---- hygiene (rule 7, house style) -------------------------------------------------------------
$hygieneRules = @(
    @{ Name = 'en or em dash (write -)'; Pattern = '[–—]' },
    @{ Name = 'ellipsis (write ..)'; Pattern = '\.\.\.|…' }
)
Add-Section 'hygiene' {
    $docs = [ordered]@{}
    foreach ($rel in $pages.Keys) { $docs[$rel] = @{ Raw = $pages[$rel]; Prose = (Get-HtmlProse $pages[$rel]) } }
    foreach ($rel in $readmes.Keys) { $docs[$rel] = @{ Raw = $readmes[$rel]; Prose = (Get-MarkdownProse $readmes[$rel]) } }
    foreach ($rel in $docs.Keys) {
        $d = $docs[$rel]
        foreach ($m in [regex]::Matches($d.Raw, '(?i)(?:href|src)\s*=\s*["'']http://|\]\(\s*<?http://')) { $failures.Add("${rel}:$(Get-LineAt $d.Raw $m.Index) http:// link (use https://)") }
        foreach ($r in $hygieneRules) {
            foreach ($m in [regex]::Matches($d.Prose, $r.Pattern)) { $failures.Add("${rel}:$(Get-LineAt $d.Prose $m.Index) $($r.Name)") }
        }
    }
}

Write-Host "  structure $($counts['structure']) problems (hub, subject index, glossary of $($terms.Count) terms)"
Write-Host "  sitemap   $($pages.Count - $unlisted.Count) listed pages, $($unlisted.Count) noindex, $($stubs.Count) stubs - $($counts['sitemap']) problems"
Write-Host "  seo       $($pages.Count) pages - $($counts['seo']) problems"
Write-Host "  locales   $groupTotal span groups, $($sectionsByReadme['README.md'].Count) README sections - $($counts['locales']) problems"
Write-Host "  terms     $($terms.Count) terms - $($counts['terms']) problems"
Write-Host "  screens   $imgTotal images - $($counts['screens']) problems"
Write-Host "  addresses $heldTotal held addresses, $(@($held.surfaces).Count) surface patterns - $($counts['addresses']) problems"
Write-Host "  positioning $($positioning.pillars.Count) pillars, $positioningRegions regions of $(@($positioning.surfaces).Count) surface entries - $($counts['positioning']) problems"
Write-Host "  releasenotes $releaseNotesCount releases on 3 pages - $($counts['releasenotes']) problems"
Write-Host "  hygiene   $($pages.Count + $readmes.Count) documents - $($counts['hygiene']) problems"
if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "external-docs: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "external-docs: PASS ($($pages.Count) pages, $groupTotal span groups, $($terms.Count) terms, $imgTotal images)" -ForegroundColor Green
exit 0
