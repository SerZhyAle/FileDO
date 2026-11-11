<#
.SYNOPSIS
  Fill a Partner Center listing export with the per-language copy from msix\listing\*.txt.

.DESCRIPTION
  Partner Center round-trips a listing as a CSV: one row per field, one column per locale. The
  flow is export, then merge, then import - never author the CSV by hand, because the ID column
  holds account-specific values and a hand-made file is refused.

  The rule this script exists to enforce is "patch, never regenerate": it fills only EMPTY cells
  and leaves everything else - above all the asset URLs Partner Center puts in the screenshot and
  logo rows - exactly as exported. Rewriting one detaches an image already uploaded.

  Column order is never changed and no column is invented. A locale that is not already a column
  of the export is dropped by the import WITHOUT an error, so adding one here would look like it
  worked and do nothing: add the language in Partner Center (Store listings > Manage additional
  languages), export again, then run this.

  What it REFUSES (exit 1, nothing written - no file, no folder; WINDOWS-STORE rules 7 and 9 and the
  0.2 amendment): a language the product ships (en ru uk de fr) with no column in the export or no
  msix\listing\<code>.txt; a cell over its cap (counted after CRLF normalisation); more than seven
  search terms, or a duplicate among them; more than 21 words across a language's seven search
  terms (a word is a non-whitespace run); and, in the export's own dialect (WINDOWS-STORE 0.3 item B),
  a record break that is not the export's, a mixed in-cell break, or an open last quoted cell. One cell over its cap loses the whole language at import,
  so the run stops here instead of producing a file that would. Each problem is named by language,
  field, length and deck file. Missing screenshots, empty required fields and the like stay warnings.

  The file's own style is preserved, not imposed: the BOM (or its absence), the record newline,
  whether a final newline exists, and the quoting style are all read from the export and written
  back the same way. A real export measured on 2026-09-21 is UTF-8 WITH BOM, CRLF between records,
  LF inside cells, minimal quoting and no final newline; writing it back any other way is what
  makes an import "succeed" with ruined text. -FillNothing proves the round trip is byte-identical.

.PARAMETER Csv
  The fresh export. Default: msix\out\listingData.export.csv - save the export there.

.PARAMETER Out
  Where the patched CSV goes. Default: msix\out\store-import\listingData.csv. The export itself
  is never modified.

.PARAMETER Screenshots
  Fill EMPTY DesktopScreenshotN cells with the per-locale images from msix\screenshots (order: the
  -Pages list, the same as make-screenshots.ps1) and copy those images beside the CSV.

  The cell holds a path RELATIVE TO THE IMPORT ROOT AND INCLUDING THE ROOT FOLDER'S NAME -
  "store-import/capacity-ru.png", never "capacity-ru.png". Measured on 2026-09-22: a bare name is
  answered with "The value you provided is not valid (capacity-ru.png)" and, because the import is
  all-or-nothing per language, it drops every language in the file, text included. The Type column
  of the export says so - "Relative path (or URL to file in Partner Center)". The zip therefore
  contains the FOLDER, not loose files, and Partner Center rewrites these paths into its own asset
  URLs, which the next export carries and -Refresh must never overwrite.

.PARAMETER Refresh
  Fields to overwrite even when the cell has a value. ReleaseNotes is the one that changes every
  release. Screenshot and logo fields are refused: their cells hold Partner Center's asset URLs.

.PARAMETER Only
  Patch ONLY these fields (wildcards allowed, e.g. -Only 'SearchTerm*'). Re-sending a field you did
  not need to change is one more chance for a language to be rejected, and the import is
  all-or-nothing per language.

.PARAMETER SkipFields
  Fields to leave alone even when empty.

.PARAMETER FillNothing
  Parse and rewrite without filling anything. A byte-identical result proves the reader and writer
  round-trip this export. Run it before any real patching. It writes no file and no folder, and it
  refuses an export that lacks the column of a shipped language, exactly as a patching run does.

.EXAMPLE
  .\msix\build-store-listing-csv.ps1 -FillNothing                 # self-test on the export
.EXAMPLE
  .\msix\build-store-listing-csv.ps1 -Screenshots                 # first submission
.EXAMPLE
  .\msix\build-store-listing-csv.ps1 -Refresh ReleaseNotes        # every later release
#>
[CmdletBinding()]
param(
    [string]$Csv,
    [string]$Out,
    [switch]$Screenshots,
    [string]$Pages = "capacity,speed,duplicates,secure,wipe",
    [string[]]$Refresh = @(),
    [string[]]$Only = @(),
    [string[]]$SkipFields = @(),
    [switch]$FillNothing
)

$ErrorActionPreference = "Stop"
$msix       = $PSScriptRoot
$ListingDir = Join-Path $msix "listing"
$ShotDir    = Join-Path $msix "screenshots"
if (-not $Csv) { $Csv = Join-Path $msix "out\listingData.export.csv" }
if (-not (Test-Path $Csv)) {
    throw "build-store-listing-csv: no export at $Csv. In Partner Center: Store listings > Export listing, save the file there (msix\README.md, section 4)."
}
# .NET file APIs resolve relative paths against the process directory, which PowerShell does not
# keep in step with the session location - so make every path absolute before anything writes.
$Csv = (Resolve-Path $Csv).Path
if (-not $Out) { $Out = Join-Path $msix "out\store-import\listingData.csv" }
$Out = [System.IO.Path]::GetFullPath($(if ([System.IO.Path]::IsPathRooted($Out)) { $Out } else { Join-Path (Get-Location) $Out }))
if ($Out -eq $Csv) { throw "build-store-listing-csv: -Out is the export itself; the export is never overwritten (re-take it from Partner Center every time instead)." }

# language code -> Partner Center column header. One entry per language the GUI ships
# (Localization.vb) and per Resource in AppxManifest.xml; keep the three in step.
$Locale = [ordered]@{ en = "en-us"; ru = "ru"; uk = "uk"; de = "de"; fr = "fr" }

# The values known when this was written (WINDOWS-STORE rule 9; the word cap is the 0.2 amendment of
# 2026-10-17, conservative: 22 was never measured). A cell over one is REFUSED before anything is
# written. Partner Center's console is the authority and its caps drift: when the console and the
# contract move, change the number here in the same edit (comply or amend, never work around).
$KnownCaps = @{ ShortDescription = 1000; Description = 10000; ReleaseNotes = 1500 }
$FeatureCap = 200; $SearchTermCap = 30; $SearchTermMax = 7; $SearchTermWordCap = 21

# --- reader / writer ---------------------------------------------------------------------------
function Read-Csv([string]$text) {
    $rows = New-Object System.Collections.ArrayList
    $row  = New-Object System.Collections.ArrayList
    $sb   = New-Object System.Text.StringBuilder
    $inQuotes = $false
    for ($i = 0; $i -lt $text.Length; $i++) {
        $ch = $text[$i]
        if ($inQuotes) {
            if ($ch -eq '"') {
                if ($i + 1 -lt $text.Length -and $text[$i + 1] -eq '"') { [void]$sb.Append('"'); $i++ }
                else { $inQuotes = $false }
            } else { [void]$sb.Append($ch) }
            continue
        }
        switch ($ch) {
            '"'  { $inQuotes = $true }
            ','  { [void]$row.Add($sb.ToString()); [void]$sb.Clear() }
            "`r" { }
            "`n" { [void]$row.Add($sb.ToString()); [void]$sb.Clear(); [void]$rows.Add($row.ToArray()); $row.Clear() }
            default { [void]$sb.Append($ch) }
        }
    }
    if ($sb.Length -gt 0 -or $row.Count -gt 0) { [void]$row.Add($sb.ToString()); [void]$rows.Add($row.ToArray()) }
    return , $rows
}
function Format-Cell([string]$v, [bool]$alwaysQuote) {
    if ($alwaysQuote -or $v -match '[",\r\n]') { return '"' + $v.Replace('"', '""') + '"' }
    return $v
}
function Format-Csv($rows, [string]$newline, [bool]$trailing, [bool]$alwaysQuote) {
    $sb = New-Object System.Text.StringBuilder
    for ($i = 0; $i -lt $rows.Count; $i++) {
        [void]$sb.Append((($rows[$i] | ForEach-Object { Format-Cell $_ $alwaysQuote }) -join ','))
        if ($i -lt $rows.Count - 1 -or $trailing) { [void]$sb.Append($newline) }
    }
    return $sb.ToString()
}
# WINDOWS-STORE rule 8 as amended 2026-10-29 (0.3, item B): a writer of the export's own dialect refuses a
# record break that is not the export's, a mixed in-cell break, and an open last quoted cell. Run on the export
# (a foreign break there would be rewritten silently) and on the text about to be written.
function Get-DialectProblems([string]$text, [string]$newline, [string]$label) {
    $problems = New-Object System.Collections.ArrayList
    $cellBreaks = New-Object System.Collections.Generic.HashSet[string]
    $inQuotes = $false; $record = 1
    for ($i = 0; $i -lt $text.Length; $i++) {
        $ch = $text[$i]
        if ($ch -eq '"') { $inQuotes = -not $inQuotes; continue }
        if ($ch -ne "`n") { continue }
        $br = if ($i -gt 0 -and $text[$i - 1] -eq "`r") { "`r`n" } else { "`n" }
        if ($inQuotes) { [void]$cellBreaks.Add($br); continue }
        if ($br -ne $newline) {
            $name = if ($br -eq "`n") { "LF" } else { "CRLF" }; $want = if ($newline -eq "`n") { "LF" } else { "CRLF" }
            [void]$problems.Add("${label}: record $record ends with $name, the export's record break is $want")
        }
        $record++
    }
    if ($inQuotes) { [void]$problems.Add("${label}: the last quoted cell is left open") }
    if ($cellBreaks.Count -gt 1) { [void]$problems.Add("${label}: in-cell breaks mix LF and CRLF") }
    return , $problems
}
function Stop-OnDialect($problems) {
    if ($problems.Count) {
        foreach ($p in $problems) { Write-Host "  refused: $p" -ForegroundColor Red }
        Write-Host "build-store-listing-csv: $($problems.Count) dialect problem(s); nothing was written." -ForegroundColor Red
        exit 1
    }
}

# --- the export, style and all -----------------------------------------------------------------
$bytes  = [System.IO.File]::ReadAllBytes($Csv)
$hasBom = $bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF
$raw    = (New-Object System.Text.UTF8Encoding($false)).GetString($bytes, $(if ($hasBom) { 3 } else { 0 }), $bytes.Length - $(if ($hasBom) { 3 } else { 0 }))
$newline        = if ($raw -match "`r`n") { "`r`n" } else { "`n" }
$trailing       = $raw.EndsWith("`n")
$alwaysQuote    = $raw.StartsWith('"')
$rows = Read-Csv $raw
if ($rows.Count -lt 2) { throw "build-store-listing-csv: the export has no data rows: $Csv" }
Stop-OnDialect (Get-DialectProblems $raw $newline "the export")
$header   = $rows[0]
$fieldCol = [array]::IndexOf($header, "Field")
if ($fieldCol -lt 0) { throw "build-store-listing-csv: no 'Field' column in $Csv" }
$styleNote = "{0}, {1}, {2}, {3}" -f $(if ($hasBom) { "BOM" } else { "no BOM" }), $(if ($newline -eq "`r`n") { "CRLF" } else { "LF" }), $(if ($alwaysQuote) { "all fields quoted" } else { "minimal quoting" }), $(if ($trailing) { "final newline" } else { "no final newline" })

# --- the per-language sources ------------------------------------------------------------------
function Read-ListingSource([string]$path) {
    $fields = [ordered]@{}
    $name = $null
    $buf = New-Object System.Collections.ArrayList
    foreach ($line in [System.IO.File]::ReadAllLines($path)) {
        if ($line -match '^@@(\w+)\s*$') {
            if ($name) { $fields[$name] = ($buf -join "`n").Trim() }
            $name = $Matches[1]; $buf.Clear()
        } elseif ($name -and $line -notmatch '^#') {
            [void]$buf.Add($line)
        }
    }
    if ($name) { $fields[$name] = ($buf -join "`n").Trim() }
    return $fields
}
$source = @{}
$shared = if (Test-Path (Join-Path $ListingDir "shared.txt")) { Read-ListingSource (Join-Path $ListingDir "shared.txt") } else { @{} }
foreach ($code in $Locale.Keys) {
    $p = Join-Path $ListingDir "$code.txt"
    if (Test-Path $p) {
        $merged = [ordered]@{}
        foreach ($k in $shared.Keys) { $merged[$k] = $shared[$k] }
        $mine = Read-ListingSource $p
        foreach ($k in $mine.Keys) { $merged[$k] = $mine[$k] }
        $source[$Locale[$code]] = $merged
    }
}

$present = @(); $missing = @()
foreach ($tag in $Locale.Values) { if ($header -contains $tag) { $present += $tag } else { $missing += $tag } }

# Every language the product ships needs a column in the export AND a source file (WINDOWS-STORE rule 7
# and section 3). An import cannot create a column and drops the copy of a language that has none; a
# column with no deck stays empty. Continuing would look like a success and do nothing for that
# language, so this is a failure that names the language, in the round-trip self-test as well.
$languageProblems = New-Object System.Collections.ArrayList
foreach ($code in $Locale.Keys) {
    $tag = $Locale[$code]
    if ($missing -contains $tag) { [void]$languageProblems.Add("${tag}: the export has no '$tag' column, so the import would drop this language's copy without an error. Add the language in Partner Center (Store listings > Manage additional languages), take a fresh export, then run this again") }
    if (-not $source.ContainsKey($tag)) { [void]$languageProblems.Add("${tag}: no source file msix\listing\$code.txt, so this language's column would stay empty") }
}
if ($languageProblems.Count) {
    foreach ($p in $languageProblems) { Write-Host "  refused: $p" -ForegroundColor Red }
    Write-Host "build-store-listing-csv: $($languageProblems.Count) shipped-language problem(s); nothing was written." -ForegroundColor Red
    exit 1
}

# Asset rows must never be refreshed: their cells hold URLs of images already uploaded.
$assetFields = @($rows | ForEach-Object { $_[$fieldCol] } | Where-Object { $_ -match 'Screenshot|Logo' })
foreach ($f in $Refresh) {
    if ($assetFields -contains $f) { throw "build-store-listing-csv: -Refresh $f would overwrite a listing-asset URL; refused." }
}
function Test-Selected([string]$field) {
    if ($SkipFields -contains $field) { return $false }
    if ($Only.Count -eq 0) { return $true }
    foreach ($o in $Only) { if ($field -like $o) { return $true } }
    return $false
}

# --- patch ---------------------------------------------------------------------------------------
$filled = 0; $refreshed = 0
if (-not $FillNothing) {
    for ($r = 1; $r -lt $rows.Count; $r++) {
        $row = $rows[$r]
        $field = $row[$fieldCol]
        if (-not $field -or -not (Test-Selected $field)) { continue }
        foreach ($tag in $present) {
            $c = [array]::IndexOf($header, $tag)
            if ($c -lt 0 -or $c -ge $row.Count -or -not $source.ContainsKey($tag)) { continue }
            $isRefresh = $Refresh -contains $field
            if ($row[$c] -ne "" -and -not $isRefresh) { continue }   # asset URLs live in these cells
            $value = $source[$tag][$field]
            if (-not $value) { continue }
            # -ceq, not -eq: PowerShell compares strings case-insensitively, so a correction that
            # differs only in case would be silently treated as already up to date.
            if ($row[$c] -ceq $value) { continue }
            if ($row[$c] -ne "") { $refreshed++ } else { $filled++ }
            $row[$c] = $value
        }
        $rows[$r] = $row
    }
}

# Screenshots: attached by relative path, and only where the locale has none.
# The path MUST start with the name of the import root folder ("store-import/capacity-ru.png").
# A bare file name is refused with "The value you provided is not valid (<name>)", and because the
# import is all-or-nothing per language that one value drops every language in the file - text and all.
$shotRoot  = Split-Path -Leaf (Split-Path -Parent $Out)
$shotKinds = @($Pages -split '[,; ]+' | Where-Object { $_ })
$shots = 0; $shotFiles = New-Object System.Collections.Generic.HashSet[string]
if ($Screenshots -and -not $FillNothing) {
    for ($r = 1; $r -lt $rows.Count; $r++) {
        $row = $rows[$r]
        if ($row[$fieldCol] -notmatch '^DesktopScreenshot(\d+)$') { continue }
        $n = [int]$Matches[1]
        if ($n -lt 1 -or $n -gt $shotKinds.Count) { continue }
        foreach ($tag in $present) {
            $c = [array]::IndexOf($header, $tag)
            if ($c -lt 0 -or $c -ge $row.Count -or $row[$c] -ne "") { continue }
            $name = "{0}-{1}.png" -f $shotKinds[$n - 1], $tag
            if (-not (Test-Path (Join-Path $ShotDir $name))) { Write-Host "  missing screenshot $name (run msix\make-screenshots.ps1)" -ForegroundColor Yellow; continue }
            $row[$c] = "$shotRoot/$name"
            [void]$shotFiles.Add($name)
            $shots++
        }
        $rows[$r] = $row
    }
}

# --- the caps, BEFORE anything is written (WINDOWS-STORE rule 9, 0.2 amendment) ----------------------
# What is checked is the file about to be written: the cells as they stand after the patch, including
# the ones this script does not own. One cell over its cap loses the whole language at import, so a
# problem refuses the run: exit 1, no file, no folder. The round-trip self-test writes nothing and
# checks no copy, so it skips this.
$rowByField = @{}
for ($r = 1; $r -lt $rows.Count; $r++) { $rowByField[$rows[$r][$fieldCol]] = $rows[$r] }
$wordTotals = @()
if (-not $FillNothing) {
    $capProblems = New-Object System.Collections.ArrayList
    foreach ($code in $Locale.Keys) {
        $tag = $Locale[$code]
        $c = [array]::IndexOf($header, $tag)
        $deck = "msix\listing\$code.txt"
        $terms = New-Object System.Collections.ArrayList
        $words = 0
        foreach ($f in @($rowByField.Keys | Sort-Object)) {
            $fieldRow = $rowByField[$f]
            if ($c -lt 0 -or $c -ge $fieldRow.Count) { continue }
            $v = [string]$fieldRow[$c]
            if (-not $v) { continue }
            # Partner Center counts the characters after the line breaks are CRLF, so a multi-line cell
            # is longer than its LF-joined source: normalise first, then count.
            $len = (($v -replace "`r`n", "`n") -replace "`n", "`r`n").Length
            $cap = if ($KnownCaps.ContainsKey($f)) { $KnownCaps[$f] } elseif ($f -match '^Feature\d+$') { $FeatureCap } elseif ($f -match '^SearchTerm\d+$') { $SearchTermCap } else { 0 }
            if ($cap -and $len -gt $cap) { [void]$capProblems.Add("${tag} ${f}: $len characters, over the cap of $cap (deck $deck)") }
            if ($f -match '^SearchTerm\d+$') {
                [void]$terms.Add($v.ToLowerInvariant())
                $words += ([regex]::Matches($v, '\S+')).Count
            }
        }
        if ($terms.Count -gt $SearchTermMax) { [void]$capProblems.Add("${tag}: $($terms.Count) search terms, at most $SearchTermMax (deck $deck)") }
        if (@($terms | Select-Object -Unique).Count -ne $terms.Count) { [void]$capProblems.Add("${tag}: duplicate search terms (deck $deck)") }
        if ($words -gt $SearchTermWordCap) { [void]$capProblems.Add("${tag}: $words words across the $($terms.Count) search terms, over the cap of $SearchTermWordCap (WINDOWS-STORE 0.2; deck $deck)") }
        $wordTotals += "$tag $words"
    }
    if ($capProblems.Count) {
        foreach ($p in $capProblems) { Write-Host "  refused: $p" -ForegroundColor Red }
        Write-Host "build-store-listing-csv: $($capProblems.Count) cap problem(s); one cell over its cap loses the whole language at import, so nothing was written." -ForegroundColor Red
        exit 1
    }
}

# --- write, preserving the export's style ----------------------------------------------------------
# The folder is made only for a real write: the self-test leaves nothing behind, not even an empty one.
$text = Format-Csv $rows $newline $trailing $alwaysQuote
Stop-OnDialect (Get-DialectProblems $text $newline "the file to write")
if (-not $FillNothing) { New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Out) | Out-Null }
# The self-test never leaves a file in the import folder: an unpatched CSV sitting there is one
# careless upload away from a listing that was never filled.
$target = if ($FillNothing) { [System.IO.Path]::GetTempFileName() } else { $Out }
[System.IO.File]::WriteAllText($target, $text, (New-Object System.Text.UTF8Encoding($hasBom)))
Write-Host "export style : $styleNote" -ForegroundColor DarkGray

if ($FillNothing) {
    $same = ([System.IO.File]::ReadAllBytes($Csv) -join ',') -eq ([System.IO.File]::ReadAllBytes($target) -join ',')
    [System.IO.File]::Delete($target)
    Write-Host ("round trip   : {0}" -f $(if ($same) { "byte-identical" } else { "DIFFERS - do not trust a patching run" })) -ForegroundColor $(if ($same) { "Green" } else { "Red" })
    if (-not $same) {
        Write-Host "  The reader/writer does not reproduce this export byte for byte (an unusual quoting or newline style?). Nothing was patched; fix the tool before using it on this file." -ForegroundColor Red
        exit 1
    }
    exit 0
}
Write-Host "filled $filled empty cells, refreshed $refreshed, attached $shots screenshots, across $($present.Count) locales -> $Out" -ForegroundColor Green
Write-Host ("search-term words (cap {0}): {1}" -f $SearchTermWordCap, ($wordTotals -join ', ')) -ForegroundColor DarkGray

# --- completeness: warnings only (the caps above are refusals) ------------------------------------------
$warn = New-Object System.Collections.ArrayList
foreach ($tag in $present) {
    $c = [array]::IndexOf($header, $tag)
    foreach ($need in 'Title', 'ShortDescription', 'Description') {
        if ($rowByField.ContainsKey($need) -and -not $rowByField[$need][$c]) { [void]$warn.Add("${tag}: $need is empty - the listing stays Incomplete") }
    }
    if ($rowByField.ContainsKey('DesktopScreenshot1') -and -not $rowByField['DesktopScreenshot1'][$c]) {
        [void]$warn.Add("${tag}: no DesktopScreenshot1 - a listing with no screenshot is Incomplete and Partner Center reports nothing")
    }
    if ($rowByField.ContainsKey('OverrideLogosForWin10') -and $rowByField['OverrideLogosForWin10'][$c] -eq 'True' -and -not $rowByField['StoreLogo300x300'][$c]) {
        [void]$warn.Add("${tag}: OverrideLogosForWin10 is True with no StoreLogo of its own - that holds the listing Incomplete")
    }
}
foreach ($w in $warn) { Write-Host "  warning: $w" -ForegroundColor Yellow }

# --- the import folder: CSV + the images it names, and a zip of both ----------------------------------
$importDir = Split-Path -Parent $Out
if ($Screenshots) {
    foreach ($n in $shotFiles) { Copy-Item (Join-Path $ShotDir $n) (Join-Path $importDir $n) -Force }
    Write-Host "import folder: $importDir  (CSV + $($shotFiles.Count) images)" -ForegroundColor Cyan
    $zip = "$importDir.zip"
    if (Test-Path $zip) { [System.IO.File]::Delete($zip) }
    # The folder itself is archived, not its contents: the cells name "<folder>/<file>", so the
    # folder has to exist inside the zip for those paths to resolve.
    Compress-Archive -Path $importDir -DestinationPath $zip
    Write-Host "import zip   : $zip" -ForegroundColor Cyan
} else {
    Write-Host "patched CSV  : $Out  (upload it on its own only if no screenshot cell was touched)" -ForegroundColor Cyan
}
