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
    locales    rule 3. Every run of sibling data-l spans carries ru, en and ua exactly once, and
               every README translation has the section count of README.md. Freshness: the EN text
               of every span group and every README.md section is fingerprinted beside its
               translations in docs/translation-fingerprints.json. EN text whose fingerprint is not
               recorded fails - naming the locales left untouched as stale - until the translations
               are checked and the fingerprints re-recorded with -Record.
    terms      rule 4. docs/termbase.json: every term's per-locale form is used in that locale, and
               no forbidden synonym appears in that locale's prose (code excluded).
    screens    rule 6. A page whose <main> carries data-ui-steps shows at least one screenshot;
               every <img> on a public page has alt text, width and height, and a file under
               docs/assets/. The pictures are produced by packaging/capture-guide-screens.ps1.
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
$readmeLangs = [ordered]@{ 'README.ru.md' = 'ru'; 'README.ua.md' = 'ua'; 'README.de.md' = 'de'; 'README.fr.md' = 'fr' }
$registryPath = Join-Path $root 'docs\DOCUMENT_REGISTRY.jsonl'
$sitemapPath = Join-Path $root 'docs\sitemap.xml'
$termbasePath = Join-Path $root 'docs\termbase.json'
$printsPath = Join-Path $root 'docs\translation-fingerprints.json'

function Stop-Unverified([string]$why) {
    Write-Host "external-docs: NOT VERIFIED ($why)" -ForegroundColor Yellow
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
$prints = $null
if (-not $Record) {
    if (-not (Test-Path -LiteralPath $printsPath)) { Stop-Unverified 'docs/translation-fingerprints.json is missing (run with -Record once)' }
    try { $prints = [IO.File]::ReadAllText($printsPath) | ConvertFrom-Json -AsHashtable } catch { Stop-Unverified "docs/translation-fingerprints.json cannot be read: $($_.Exception.Message)" }
}

$external = @($registry | Where-Object { $_.PSObject.Properties.Name -contains 'path' -and $_.corpus -eq 'external' -and $_.role -eq 'source' })
$pages = [ordered]@{}   # public site pages: rel path -> raw text
$stubs = [System.Collections.Generic.List[string]]::new()
foreach ($r in $external) {
    $rel = ([string]$r.path) -replace '\\', '/'
    if ($rel -notmatch '^docs/.+\.html$') { continue }
    $text = Read-Text $rel
    if ($text -match '(?i)http-equiv\s*=\s*["'']refresh') { $stubs.Add($rel) } else { $pages[$rel] = $text }
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
        $units = @(foreach ($g in $groupsByPage[$rel]) {
            $u = [ordered]@{}
            foreach ($l in $siteLangs) { if ($g.Langs.Contains($l)) { $u[$l] = Get-Fingerprint $g.Langs[$l] } }
            $u
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
    $hub = 'docs/guides/index.html'
    $glossary = 'docs/guides/glossary.html'
    $topics = 'docs/guides/topics.html'
    foreach ($need in $hub, $glossary, $topics) { if (-not $pages.Contains($need)) { $failures.Add("$need is not a declared public page") } }
    if ($pages.Contains($hub)) {
        foreach ($href in 'glossary.html', 'topics.html') { if ($pages[$hub] -notmatch "href=[""']$([regex]::Escape($href))[""'#]") { $failures.Add("$hub does not link $href") } }
    }
    if ($pages.Contains($topics)) {
        foreach ($rel in $pages.Keys) {
            if ($rel -notmatch '^docs/guides/([^/]+\.html)$' -or $rel -in $hub, $topics) { continue }
            $name = $Matches[1]
            if ($pages[$topics] -notmatch "href=[""']$([regex]::Escape($name))[""'#]") { $failures.Add("$topics does not index $name") }
        }
    }
    if ($pages.Contains($glossary)) {
        foreach ($t in $terms) { if ($pages[$glossary] -notmatch "id=[""']term-$([regex]::Escape($t.id))[""']") { $failures.Add("$glossary has no entry id=""term-$($t.id)"" for termbase term '$($t.id)'") } }
    }
}

# ---- sitemap (rule 2) ------------------------------------------------------------------------
Add-Section 'sitemap' {
    $want = @{}
    foreach ($rel in $pages.Keys) { $want[(Get-UrlFor $rel)] = $rel }
    $seen = @{}
    $ns = [System.Xml.XmlNamespaceManager]::new($sitemap.NameTable)
    $ns.AddNamespace('s', 'http://www.sitemaps.org/schemas/sitemap/0.9')
    foreach ($u in $sitemap.SelectNodes('/s:urlset/s:url', $ns)) {
        $loc = [string]$u.SelectSingleNode('s:loc', $ns).InnerText
        $mod = $u.SelectSingleNode('s:lastmod', $ns)
        if ($seen.ContainsKey($loc)) { $failures.Add("docs/sitemap.xml lists $loc twice"); continue }
        $seen[$loc] = $true
        if (-not $want.ContainsKey($loc)) { $failures.Add("docs/sitemap.xml lists $loc, which is not a public page (a stub, docs/contracts/, or not on disk)") }
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
        $canon = Get-LinkHref $head '(?i)rel\s*=\s*"canonical"'
        if ($canon -ne $url) { $failures.Add("$rel canonical is '$canon', want '$url'") }
        foreach ($p in 'og:type', 'og:url', 'og:title', 'og:description', 'og:image') {
            $v = Get-MetaContent $head 'property' $p
            if (-not $v) { $failures.Add("$rel has no $p") }
            elseif ($p -eq 'og:url' -and $v -ne $url) { $failures.Add("$rel og:url is '$v', want '$url'") }
            elseif ($p -eq 'og:image' -and $v -notmatch '^https://') { $failures.Add("$rel og:image is not an absolute https URL") }
        }
        if (-not (Get-MetaContent $head 'name' 'twitter:card')) { $failures.Add("$rel has no twitter:card") }
        $xd = Get-LinkHref $head '(?i)hreflang\s*=\s*"x-default"'
        if ($xd -ne $url) { $failures.Add("$rel hreflang x-default is '$xd', want '$url'") }
        $ld = [regex]::Matches($head, '(?is)<script\b[^>]*type\s*=\s*"application/ld\+json"[^>]*>(.*?)</script>')
        if (-not $ld.Count) { $failures.Add("$rel has no JSON-LD") }
        foreach ($m in $ld) {
            try { $j = $m.Groups[1].Value | ConvertFrom-Json; if ([string]$j.'@context' -ne 'https://schema.org') { $failures.Add("$rel JSON-LD @context is not https://schema.org") } }
            catch { $failures.Add("$rel JSON-LD does not parse: $($_.Exception.Message)") }
        }
        # The per-locale text the page swaps in at runtime (guide.js guideMeta, the landing's meta).
        $rt = [regex]::Match($text, '(?s)(?:window\.guideMeta|var meta)\s*=\s*\{(.*?)\n\s*\};')
        if (-not $rt.Success) { $failures.Add("$rel has no runtime per-locale title and description"); continue }
        foreach ($l in $siteLangs) {
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
        $byEn = @{}
        $byLang = @{}
        foreach ($l in $siteLangs) { $byLang[$l] = @{} }
        foreach ($u in $recorded) {
            if ($u.ContainsKey('en')) { $byEn[$u.en] = $true }
            foreach ($l in $siteLangs) { if ($u.ContainsKey($l)) { $byLang[$l][$u[$l]] = $true } }
        }
        foreach ($g in $groupsByPage[$rel]) {
            $script:groupTotal++
            $missing = @($siteLangs | Where-Object { -not $g.Langs.Contains($_) })
            $extra = @($g.Langs.Keys | Where-Object { $_ -notin $siteLangs })
            if ($missing.Count -or $extra.Count) {
                $failures.Add("${rel}:$($g.Line) span group has $((@($g.Langs.Keys)) -join '/'), needs ru/en/ua exactly once")
                continue
            }
            $en = Get-Fingerprint $g.Langs['en']
            if ($byEn.ContainsKey($en)) { continue }
            $stale = @($siteLangs | Where-Object { $_ -ne 'en' -and $byLang[$_].ContainsKey((Get-Fingerprint $g.Langs[$_])) })
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
Write-Host "  sitemap   $($pages.Count) public pages, $($stubs.Count) stubs - $($counts['sitemap']) problems"
Write-Host "  seo       $($pages.Count) pages - $($counts['seo']) problems"
Write-Host "  locales   $groupTotal span groups, $($sectionsByReadme['README.md'].Count) README sections - $($counts['locales']) problems"
Write-Host "  terms     $($terms.Count) terms - $($counts['terms']) problems"
Write-Host "  screens   $imgTotal images - $($counts['screens']) problems"
Write-Host "  hygiene   $($pages.Count + $readmes.Count) documents - $($counts['hygiene']) problems"
if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "external-docs: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "external-docs: PASS ($($pages.Count) pages, $groupTotal span groups, $($terms.Count) terms, $imgTotal images)" -ForegroundColor Green
exit 0
