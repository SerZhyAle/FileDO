#Requires -Version 7.0
<#
.SYNOPSIS
  Writes the control-glyph sprite of every site page from assets/glyphs/, or checks that it matches.

.DESCRIPTION
  SITE-EXPERIENCE rule 6 (bound at the guide tier by 0.5 item C) and ICON-SET section 2 rule 8: where a
  guide names a control of the shell, it shows the glyph the shell draws for it beside the name. A
  mention is written in the page as

      <svg class="cg" aria-hidden="true" focusable="false"><use href="#g-<id>"/></svg>

  and the drawing itself lives once per page, in a hidden sprite between two markers, last in the
  body (the skip link stays the first thing after <body>):

      <!-- control-glyphs -->
      <svg class="cg-sprite" aria-hidden="true" focusable="false"><symbol id="g-<id>" viewBox="0 0 24 24">..</symbol>..</svg>
      <!-- /control-glyphs -->

  The sprite is a render target: each symbol is the inner drawing of assets/glyphs/<id>.svg - the copy
  the shell embeds, vendored from the catalog by assets/sync-icon-glyphs.ps1 - and is never edited by
  hand. Re-run this script after adding or removing a mention. The block takes the line endings of its
  page (a CRLF page keeps CRLF). A rewrite keeps the page's UTF-8 byte order mark (BOM): the BOM is read
  from the page's bytes and written back with it.

  Without -Check the script rewrites each page's sprite (adding it, or removing it when the page has no
  mention left). With -Check it writes nothing and fails when a sprite differs from what it would write,
  when a mention names an id that has no file in assets/glyphs/, when an svg of class cg is not in
  the exact form above, or when a page's byte order mark differs from its committed copy (git HEAD).
  A page that HEAD does not hold has no committed copy and is not compared on its BOM.

  Pages: every .html under docs/ except the folders ru, ua, kit, assets, help and contracts (the
  landing, the privacy page, the guides, and their de and fr pages).

.NOTES
  Exit codes (CHECK-VERDICT):
    0 - -Check: every sprite matches; write: every page is written.
    1 - -Check: drift - a sprite differs, an id has no glyph file, or a mention is malformed;
        write: refused for an unknown id or a malformed mention (nothing written).
    2 - could not verify: the script failed, or -Check found no git HEAD to compare the byte order
        marks with.
#>
[CmdletBinding()]
param(
    [switch]$Check
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

trap {
    Write-Host "  $($_.Exception.Message)"
    Write-Host 'sync-guide-glyphs: COULD NOT VERIFY (the script failed)'
    exit 2
}

$root = Split-Path -Parent $PSScriptRoot
$glyphDir = Join-Path $root 'assets\glyphs'
# The site pages, as the surfaces test walks them: every .html under docs/ outside the folders
# that are not site pages.
$docs = Join-Path $root 'docs'
$skip = 'ua', 'kit', 'assets', 'help', 'contracts'
$pages = Get-ChildItem -LiteralPath $docs -Filter '*.html' -File -Recurse |
    Where-Object { $skip -notcontains ([IO.Path]::GetRelativePath($docs, $_.FullName) -split '[\\/]')[0] } |
    Sort-Object FullName

$open = '<!-- control-glyphs -->'
$close = '<!-- /control-glyphs -->'
$blockRx = [regex]('(?s)\r?\n[ \t]*' + [regex]::Escape($open) + '.*?' + [regex]::Escape($close))
$mentionRx = [regex]'<svg class="cg" aria-hidden="true" focusable="false"><use href="#g-([a-z0-9.-]+)"/></svg>'
$anyCgRx = [regex]'<svg\b[^>]*\bclass="cg"[^>]*>'
$symbols = @{}

function Get-Symbol([string]$id) {
    if (-not $symbols.ContainsKey($id)) {
        $file = Join-Path $glyphDir "$id.svg"
        if (-not (Test-Path -LiteralPath $file)) { return $null }
        $svg = [IO.File]::ReadAllText($file)
        $m = [regex]::Match($svg, '(?s)<svg\b[^>]*\bviewBox="([^"]+)"[^>]*>(.*)</svg>')
        if (-not $m.Success) { throw "assets/glyphs/$id.svg has no <svg viewBox=..> element" }
        $inner = [regex]::Replace($m.Groups[2].Value.Trim(), '>\s+<', '><')
        $symbols[$id] = "<symbol id=`"g-$id`" viewBox=`"$($m.Groups[1].Value)`">$inner</symbol>"
    }
    return $symbols[$id]
}

function Test-Utf8Bom([byte[]]$bytes) {
    return ($bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF)
}

# The committed copy of a page (HEAD) as bytes; $null when HEAD does not hold the page.
function Get-CommittedPageBytes([string]$rel) {
    $psi = [Diagnostics.ProcessStartInfo]::new('git')
    foreach ($a in @('-C', $root, 'show', "HEAD:$rel")) { [void]$psi.ArgumentList.Add($a) }
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $proc = [Diagnostics.Process]::Start($psi)
    $ms = [IO.MemoryStream]::new()
    $proc.StandardOutput.BaseStream.CopyTo($ms)
    [void]$proc.StandardError.ReadToEnd()
    $proc.WaitForExit()
    if ($proc.ExitCode -ne 0) { return $null }
    return ,$ms.ToArray()
}

# -Check compares each page's byte order mark with its committed copy, so it needs git and a HEAD.
if ($Check) {
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw 'git is not on PATH, so the byte order marks cannot be compared with the committed pages' }
    $null = & git -C $root rev-parse -q --verify HEAD 2>$null
    if ($LASTEXITCODE -ne 0) { throw 'no HEAD commit, so the byte order marks cannot be compared with the committed pages' }
}

$failures = [System.Collections.Generic.List[string]]::new()
$writes = [System.Collections.Generic.List[object]]::new()
$mentionCount = 0
foreach ($page in $pages) {
    $rel = [IO.Path]::GetRelativePath($root, $page.FullName).Replace('\', '/')
    $pageBytes = [IO.File]::ReadAllBytes($page.FullName)
    $bom = Test-Utf8Bom $pageBytes
    $text = [IO.File]::ReadAllText($page.FullName)
    if ($Check) {
        $committed = Get-CommittedPageBytes $rel
        if ($null -ne $committed -and (Test-Utf8Bom $committed) -ne $bom) { $failures.Add("${rel}: the UTF-8 byte order mark differs from the committed page (HEAD)") }
    }
    $body = $blockRx.Replace($text, '', 1)

    $wellFormed = $mentionRx.Matches($body).Count
    $all = $anyCgRx.Matches($body).Count
    if ($all -ne $wellFormed) { $failures.Add("${rel}: $($all - $wellFormed) svg of class cg not in the form <svg class=`"cg`" aria-hidden=`"true`" focusable=`"false`"><use href=`"#g-ID`"/></svg>") }

    $ids = [System.Collections.Generic.SortedSet[string]]::new([StringComparer]::Ordinal)
    foreach ($m in $mentionRx.Matches($body)) { [void]$ids.Add($m.Groups[1].Value); $mentionCount++ }

    $parts = foreach ($id in $ids) {
        $s = Get-Symbol $id
        if (-not $s) { $failures.Add("${rel}: no assets/glyphs/$id.svg for the mention g-$id") }
        $s
    }

    $want = $body
    if ($ids.Count) {
        # Last in the body, so the skip link stays the first thing after <body>.
        $b = [regex]::Matches($body, '(?m)^([ \t]*)</body>')
        if ($b.Count -ne 1 -or $b[0].Index -lt 1) { $failures.Add("${rel}: no single </body> tag at the start of a line"); continue }
        # The block takes the line endings of its page: CRLF when the line before </body> ends in CRLF.
        # Writing LF into a CRLF page is what made the sprite differ on every run (FILEDO-0252).
        $nl = if ($b[0].Index -ge 2 -and $body[$b[0].Index - 2] -eq [char]13) { "`r`n" } else { "`n" }
        $ind = $b[0].Groups[1].Value + '  '
        $block = "$ind$open$nl$ind<svg class=`"cg-sprite`" aria-hidden=`"true`" focusable=`"false`">$(-join $parts)</svg>$nl$ind$close$nl"
        $want = $body.Insert($b[0].Index, $block)
    }
    if ($want -cne $text) {
        if ($Check) { $failures.Add("${rel}: the control-glyph sprite differs from assets/glyphs (run packaging/sync-guide-glyphs.ps1)") }
        else { $writes.Add(@{ Path = $page.FullName; Rel = $rel; Text = $want; Bom = $bom }) }
    }
}

if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  $_" }
    $verb = if ($Check) { 'DRIFT' } else { 'REFUSED (nothing written)' }
    Write-Host "sync-guide-glyphs: $verb - $($failures.Count) problem(s)"
    exit 1
}
foreach ($w in $writes) {
    [IO.File]::WriteAllText($w.Path, $w.Text, [Text.UTF8Encoding]::new($w.Bom))
    Write-Host "  wrote $($w.Rel)"
}
$what = if ($Check) { 'every sprite matches' } else { "$($writes.Count) page(s) written" }
Write-Host "sync-guide-glyphs: PASS - $(@($pages).Count) pages, $mentionCount mentions, $($symbols.Count) glyphs; $what"
exit 0
