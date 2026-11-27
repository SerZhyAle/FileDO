#Requires -Version 7.0
<#
.SYNOPSIS
  Writes the command reference pages of the site from the READMEs, never by hand (SP-0166 F12).

.DESCRIPTION
  docs/guides/cli-reference.html and its de and fr pages show, between the markers <!-- cli-reference:begin and
  <!-- cli-reference:end -->, three parts of each language's README, rendered from Markdown to HTML:

    reference   the section "## Command Reference": target types, operations, short forms, modifiers
    read-check  the "### " subsections of the section "## Quick Start" whose heading names the read check (the word
                "check", any case), with what they hold; their headings move up one level
    advanced    the section "## Advanced Features": batch files, exit codes, history, interruption, network paths

  The sections are found in README.md by their English headings and paired with each translation by index, the way
  packaging/check-external-docs.ps1 pairs them for the translation record (its locales step holds the translations
  to the same section count). The English page shows README.md, README.ru.md and README.ua.md, one block per
  language (data-l en, ru and ua - the site's keys - the en block first, so a reader without script meets English);
  the de and fr pages show README.de.md and README.fr.md. The rest of each page - head, hero, footer - is written by
  hand, so an edit of a README part reaches the page only through this script.

  The renderer reads the Markdown these parts use: headings, fenced code, pipe tables, bullet and numbered lists
  (nested by a tab or by two spaces), paragraphs, inline code, bold and italics. A link becomes an <a> only when its
  target is an https:// address; any other link is written as its text, because a path relative to the README does
  not resolve on the site. A page keeps its own byte order mark (or its lack of one) and is written with LF.

  Run it after an edit of any README part it renders; packaging/check-internal-docs.ps1 runs -Check.

.PARAMETER Check
  Render and compare; write nothing.

  Exit codes (CHECK-VERDICT): 0 written or matching, 1 a page differs (-Check) or lacks its markers, 2 a README or a
  page is missing, or a README is not the shape this script reads (a section or the read check subsection is not
  found, or a translation has another section count).
#>
[CmdletBinding(PositionalBinding = $false)]
param([switch]$Check)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent

function Stop-Unverified([string]$why) {
    Write-Host "cli-reference: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

function Read-Input([string]$rel) {
    $full = Join-Path $root ($rel -replace '/', '\')
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { Stop-Unverified "$rel is missing" }
    return ([IO.File]::ReadAllText($full)) -replace "`r`n", "`n"
}

# ---- The README parts ----------------------------------------------------------------------------
# README sections the way packaging/check-external-docs.ps1 cuts them: what precedes the first "## " heading, then
# one per "## " heading outside a fenced block.
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

$readmes = [ordered]@{ en = 'README.md'; ru = 'README.ru.md'; ua = 'README.ua.md'; de = 'README.de.md'; fr = 'README.fr.md' }
$sections = @{}
foreach ($l in $readmes.Keys) { $sections[$l] = Get-ReadmeSections (Read-Input $readmes[$l]) }
foreach ($l in $readmes.Keys) {
    if ($sections[$l].Count -ne $sections['en'].Count) {
        Stop-Unverified "$($readmes[$l]) has $($sections[$l].Count - 1) '## ' sections and README.md has $($sections['en'].Count - 1); the translations are paired with it by index"
    }
}
function Find-Section([string]$heading) {
    $en = $sections['en']
    for ($i = 0; $i -lt $en.Count; $i++) { if (($en[$i] -split "`n")[0].TrimEnd() -ceq $heading) { return $i } }
    Stop-Unverified "README.md has no section '$heading'"
}
$iQuick = Find-Section '## Quick Start'
$iRef = Find-Section '## Command Reference'
$iAdv = Find-Section '## Advanced Features'

# The read check part of a Quick Start section: each "### " heading that names the check opens a subsection, which
# runs to the next "### " or "## " heading (a "#### " heading stays inside it).
function Get-CheckPart([string]$section) {
    $sb = [System.Text.StringBuilder]::new()
    $in = $false
    $fence = $null
    foreach ($line in ($section -split "`n")) {
        if (-not $fence -and $line -match '^\s{0,3}(```+|~~~+)') { $fence = $Matches[1] }
        elseif ($fence -and $line -match "^\s{0,3}$([regex]::Escape($fence))") { $fence = $null }
        elseif (-not $fence -and $line -match '^#{2,3} ') { $in = ($line -match '^### ' -and $line -match '(?i)\bcheck\b') }
        if ($in) { [void]$sb.Append($line).Append("`n") }
    }
    return $sb.ToString()
}

$parts = @(
    @{ Id = 'reference'; Shift = 0; Read = { param($l) $sections[$l][$iRef] } }
    @{ Id = 'read-check'; Shift = 1; Read = { param($l) Get-CheckPart $sections[$l][$iQuick] } }
    @{ Id = 'advanced'; Shift = 0; Read = { param($l) $sections[$l][$iAdv] } }
)

# ---- Markdown to HTML ----------------------------------------------------------------------------
function ConvertTo-HtmlText([string]$s) { return $s.Replace('&', '&amp;').Replace('<', '&lt;').Replace('>', '&gt;') }

# Inline Markdown: code spans are set aside first (their text is literal), then links, bold and italics are read
# in the rest, then the code spans go back. Plain replacement strings only - a script block per match is slow, and
# this step runs on every local build gate.
$codeSpanRe = [regex]::new('(?<!`)(`+)(?!`)(.+?)(?<!`)\1(?!`)')
$httpsLinkRe = [regex]::new('\[([^\]]+)\]\((https://[^)\s"]+)\)')
$otherLinkRe = [regex]::new('\[([^\]]+)\]\([^)\s]+\)')
$boldRe = [regex]::new('\*\*(?!\s)(.+?)(?<!\s)\*\*')
$italicRe = [regex]::new('(?<![\w*])\*(?![\s*])(.+?)(?<![\s*])\*(?![\w*])')
function ConvertTo-InlineHtml([string]$s) {
    if ($s.IndexOfAny([char[]]'`*[') -lt 0) { return ConvertTo-HtmlText $s }
    $codes = [System.Collections.Generic.List[string]]::new()
    $sb = [System.Text.StringBuilder]::new()
    $pos = 0
    foreach ($m in $codeSpanRe.Matches($s)) {
        [void]$sb.Append($s, $pos, $m.Index - $pos).Append([char]0).Append($codes.Count).Append([char]0)
        $codes.Add('<code>' + (ConvertTo-HtmlText $m.Groups[2].Value) + '</code>')
        $pos = $m.Index + $m.Length
    }
    [void]$sb.Append($s, $pos, $s.Length - $pos)
    $t = ConvertTo-HtmlText $sb.ToString()
    $t = $httpsLinkRe.Replace($t, '<a href="$2" target="_blank" rel="noopener">$1</a>')
    $t = $otherLinkRe.Replace($t, '$1')
    $t = $boldRe.Replace($t, '<strong>$1</strong>')
    $t = $italicRe.Replace($t, '<em>$1</em>')
    for ($k = 0; $k -lt $codes.Count; $k++) { $t = $t.Replace("$([char]0)$k$([char]0)", $codes[$k]) }
    return $t
}

# The cells of a pipe table row; a pipe inside a code span or escaped as \| does not end a cell.
function Split-Row([string]$row) {
    $r = $row.Trim()
    if ($r.StartsWith('|')) { $r = $r.Substring(1) }
    if ($r.EndsWith('|') -and -not $r.EndsWith('\|')) { $r = $r.Substring(0, $r.Length - 1) }
    $cells = [System.Collections.Generic.List[string]]::new()
    $sb = [System.Text.StringBuilder]::new()
    $inCode = $false
    for ($k = 0; $k -lt $r.Length; $k++) {
        $c = $r[$k]
        if ($c -eq '\' -and $k + 1 -lt $r.Length -and $r[$k + 1] -eq '|') { [void]$sb.Append('|'); $k++; continue }
        if ($c -eq '`') { $inCode = -not $inCode }
        if ($c -eq '|' -and -not $inCode) { $cells.Add($sb.ToString().Trim()); [void]$sb.Clear(); continue }
        [void]$sb.Append($c)
    }
    $cells.Add($sb.ToString().Trim())
    return , $cells
}

function Format-Table([string[]]$rows, [string]$indent) {
    $head = Split-Row $rows[0]
    $out = [System.Collections.Generic.List[string]]::new()
    $out.Add("$indent<div class=`"tablewrap`">")
    $out.Add("$indent  <table class=`"keytable`">")
    $out.Add("$indent    <thead>")
    $out.Add("$indent      <tr>" + (($head | ForEach-Object { '<th>' + (ConvertTo-InlineHtml $_) + '</th>' }) -join '') + '</tr>')
    $out.Add("$indent    </thead>")
    $out.Add("$indent    <tbody>")
    foreach ($row in @($rows | Select-Object -Skip 2)) {
        $cells = Split-Row $row
        $line = for ($c = 0; $c -lt $head.Count; $c++) { '<td>' + $(if ($c -lt $cells.Count) { ConvertTo-InlineHtml $cells[$c] } else { '' }) + '</td>' }
        $out.Add("$indent      <tr>" + ($line -join '') + '</tr>')
    }
    $out.Add("$indent    </tbody>")
    $out.Add("$indent  </table>")
    $out.Add("$indent</div>")
    return , $out
}

# Nested lists from items of a level: a list at depth d is indented by four spaces per level, its items by two more.
function Format-List([object[]]$items, [string]$indent) {
    $out = [System.Collections.Generic.List[string]]::new()
    $stack = [System.Collections.Generic.List[object]]::new()
    $closeItem = {
        $top = $stack[$stack.Count - 1]
        if ($top.ItemLine -eq $out.Count - 1) { $out[$out.Count - 1] += '</li>' }
        else { $out.Add($indent + ('    ' * ($stack.Count - 1)) + '  </li>') }
    }
    foreach ($it in $items) {
        while ($stack.Count -and $stack[$stack.Count - 1].Level -gt $it.Level) {
            & $closeItem
            $out.Add($indent + ('    ' * ($stack.Count - 1)) + "</$($stack[$stack.Count - 1].Tag)>")
            $stack.RemoveAt($stack.Count - 1)
        }
        if ($stack.Count -and $stack[$stack.Count - 1].Level -eq $it.Level) { & $closeItem }
        else {
            $tag = if ($it.Ordered) { 'ol' } else { 'ul' }
            $out.Add($indent + ('    ' * $stack.Count) + "<$tag>")
            $stack.Add(@{ Level = $it.Level; Tag = $tag; ItemLine = -1 })
        }
        $out.Add($indent + ('    ' * ($stack.Count - 1)) + '  <li>' + (ConvertTo-InlineHtml $it.Text))
        $stack[$stack.Count - 1].ItemLine = $out.Count - 1
    }
    while ($stack.Count) {
        & $closeItem
        $out.Add($indent + ('    ' * ($stack.Count - 1)) + "</$($stack[$stack.Count - 1].Tag)>")
        $stack.RemoveAt($stack.Count - 1)
    }
    return , $out
}

$itemRe = '^(?<lead>[ \t]*)(?<mark>[-*+]|\d+[.)])[ \t]+(?<text>.*)$'
function Get-ItemLevel([string]$lead) { return ([regex]::Matches($lead, "`t")).Count + [Math]::Floor(($lead -replace "`t", '').Length / 2) }

function ConvertTo-BlockHtml([string]$md, [int]$shift, [string]$indent) {
    $lines = @(($md.TrimEnd("`n")) -split "`n")
    $out = [System.Collections.Generic.List[string]]::new()
    $para = [System.Collections.Generic.List[string]]::new()
    $flush = {
        if ($para.Count) { $out.Add("$indent<p>" + (ConvertTo-InlineHtml ($para -join ' ')) + '</p>'); $para.Clear() }
    }
    $i = 0
    while ($i -lt $lines.Count) {
        $line = $lines[$i]
        if ($line -match '^\s{0,3}(```+|~~~+)') {
            & $flush
            $fence = $Matches[1]
            $code = [System.Collections.Generic.List[string]]::new()
            $i++
            while ($i -lt $lines.Count -and $lines[$i] -notmatch "^\s{0,3}$([regex]::Escape($fence))") { $code.Add($lines[$i].TrimEnd()); $i++ }
            $i++
            while ($code.Count -and -not $code[$code.Count - 1]) { $code.RemoveAt($code.Count - 1) }
            $out.Add("$indent<pre><code>" + (ConvertTo-HtmlText ($code -join "`n")) + '</code></pre>')
            continue
        }
        if (-not $line.Trim()) { & $flush; $i++; continue }
        if ($line -match '^(#{2,6})[ \t]+(.+?)[ \t]*#*[ \t]*$') {
            & $flush
            $level = [Math]::Min(6, [Math]::Max(2, $Matches[1].Length - $shift))
            $out.Add("$indent<h$level>" + (ConvertTo-InlineHtml $Matches[2]) + "</h$level>")
            $i++
            continue
        }
        if ($line -match '^\s*\|') {
            & $flush
            $rows = [System.Collections.Generic.List[string]]::new()
            while ($i -lt $lines.Count -and $lines[$i] -match '^\s*\|') { $rows.Add($lines[$i]); $i++ }
            if ($rows.Count -lt 2 -or $rows[1] -notmatch '^\s*\|?\s*:?-{3,}') { throw "a table without a separator row at '$($rows[0])'" }
            foreach ($l in (Format-Table $rows.ToArray() $indent)) { $out.Add($l) }
            continue
        }
        if ($line -match '^\s{0,3}(?:-{3,}|\*{3,}|_{3,})\s*$') { & $flush; $i++; continue }
        if ($line -match $itemRe) {
            & $flush
            $items = [System.Collections.Generic.List[object]]::new()
            while ($i -lt $lines.Count) {
                $l = $lines[$i]
                if ($l -match $itemRe) {
                    $items.Add(@{ Level = (Get-ItemLevel $Matches['lead']); Ordered = ($Matches['mark'] -match '\d'); Text = $Matches['text'].Trim() })
                    $i++
                } elseif ($l.Trim() -and $l -match '^[ \t]+\S' -and $items.Count) {
                    $items[$items.Count - 1].Text += ' ' + $l.Trim()
                    $i++
                } elseif (-not $l.Trim()) {
                    $j = $i + 1
                    while ($j -lt $lines.Count -and -not $lines[$j].Trim()) { $j++ }
                    if ($j -lt $lines.Count -and $lines[$j] -match $itemRe) { $i = $j } else { break }
                } else { break }
            }
            foreach ($l in (Format-List $items.ToArray() $indent)) { $out.Add($l) }
            continue
        }
        $para.Add($line.Trim())
        $i++
    }
    & $flush
    return , $out
}

# ---- Pages ---------------------------------------------------------------------------------------
$pages = [ordered]@{
    'docs/guides/cli-reference.html'    = @('en', 'ru', 'ua')
    'docs/de/guides/cli-reference.html' = @('de')
    'docs/fr/guides/cli-reference.html' = @('fr')
}
function Format-Region([string[]]$langs, [bool]$runtime) {
    $sb = [System.Text.StringBuilder]::new()
    [void]$sb.Append("<!-- cli-reference:begin - written by packaging/write-cli-reference.ps1 from the Command Reference, the read check of the Quick Start and the Advanced Features of README.md and its translations; never edit by hand -->`n")
    foreach ($part in $parts) {
        [void]$sb.Append("      <section class=`"section`" id=`"$($part.Id)`">`n")
        foreach ($l in $langs) {
            $md = & $part.Read $l
            if (-not $md.Trim()) { Stop-Unverified "$($readmes[$l]) gives no text for the part '$($part.Id)' (for read-check: no '### ' heading of its Quick Start section names the check)" }
            $langAttr = if ($runtime) { " lang=`"$(if ($l -eq 'ua') { 'uk' } else { $l })`"" } else { '' }
            [void]$sb.Append("        <div class=`"readme-render`" data-l=`"$l`"$langAttr>`n")
            try { $html = ConvertTo-BlockHtml $md $part.Shift '          ' }
            catch { Stop-Unverified "$($readmes[$l]), part '$($part.Id)': $($_.Exception.Message)" }
            foreach ($h in $html) { [void]$sb.Append($h).Append("`n") }
            [void]$sb.Append("        </div>`n")
        }
        [void]$sb.Append("      </section>`n")
    }
    [void]$sb.Append('      <!-- cli-reference:end -->')
    return $sb.ToString()
}

$problems = [System.Collections.Generic.List[string]]::new()
$written = 0
foreach ($rel in $pages.Keys) {
    $full = Join-Path $root ($rel -replace '/', '\')
    if (-not (Test-Path -LiteralPath $full -PathType Leaf)) { Stop-Unverified "$rel is missing" }
    $bytes = [IO.File]::ReadAllBytes($full)
    $bom = $bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF
    $orig = [IO.File]::ReadAllText($full)
    $block = [regex]::Matches($orig, '(?s)<!-- cli-reference:begin.*?<!-- cli-reference:end -->')
    if ($block.Count -ne 1) { $problems.Add("${rel}: expected one cli-reference block between its markers, found $($block.Count)"); continue }
    $region = Format-Region $pages[$rel] ($pages[$rel].Count -gt 1)
    $next = $orig.Substring(0, $block[0].Index) + $region + $orig.Substring($block[0].Index + $block[0].Length)
    $next = $next -replace "`r`n", "`n"
    if ($next -cne $orig) {
        if ($Check) { $problems.Add("${rel}: the command reference differs from what the READMEs give - run packaging/write-cli-reference.ps1") }
        else { [IO.File]::WriteAllText($full, $next, [Text.UTF8Encoding]::new($bom)); $written++ }
    }
}
if ($problems.Count) {
    foreach ($p in $problems) { Write-Host "  FAIL  $p" -ForegroundColor Red }
    Write-Host "cli-reference: FAIL ($($problems.Count) problem(s))" -ForegroundColor Red
    exit 1
}
$summary = "README sections $iRef, $iAdv and the read check of section $iQuick"
if ($Check) { Write-Host "cli-reference: PASS ($($pages.Count) pages match; $summary)" -ForegroundColor Green }
else { Write-Host "cli-reference: PASS ($written of $($pages.Count) pages written; $summary)" -ForegroundColor Green }
exit 0
