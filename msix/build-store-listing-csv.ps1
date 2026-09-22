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
  round-trip this export. Run it before any real patching.

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

# The values known when this was written, used ONLY to warn. Partner Center's console is the
# authority and its caps drift; an over-length cell is rejected there, not here.
$KnownCaps = @{ ShortDescription = 1000; Description = 10000; ReleaseNotes = 1500 }
$FeatureCap = 200; $SearchTermCap = 30; $SearchTermMax = 7

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

# --- the export, style and all -----------------------------------------------------------------
$bytes  = [System.IO.File]::ReadAllBytes($Csv)
$hasBom = $bytes.Length -ge 3 -and $bytes[0] -eq 0xEF -and $bytes[1] -eq 0xBB -and $bytes[2] -eq 0xBF
$raw    = (New-Object System.Text.UTF8Encoding($false)).GetString($bytes, $(if ($hasBom) { 3 } else { 0 }), $bytes.Length - $(if ($hasBom) { 3 } else { 0 }))
$newline        = if ($raw -match "`r`n") { "`r`n" } else { "`n" }
$trailing       = $raw.EndsWith("`n")
$alwaysQuote    = $raw.StartsWith('"')
$rows = Read-Csv $raw
if ($rows.Count -lt 2) { throw "build-store-listing-csv: the export has no data rows: $Csv" }
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

# --- write, preserving the export's style ----------------------------------------------------------
New-Item -ItemType Directory -Force -Path (Split-Path -Parent $Out) | Out-Null
$text = Format-Csv $rows $newline $trailing $alwaysQuote
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
if ($missing.Count) {
    Write-Host ("locales NOT in this export (add them in Partner Center > Store listings > Manage additional languages, then export again): {0}" -f ($missing -join ' ')) -ForegroundColor Yellow
}

# --- lint: warnings only ---------------------------------------------------------------------------
$warn = New-Object System.Collections.ArrayList
$rowByField = @{}
for ($r = 1; $r -lt $rows.Count; $r++) { $rowByField[$rows[$r][$fieldCol]] = $rows[$r] }
foreach ($tag in $present) {
    $c = [array]::IndexOf($header, $tag)
    if (-not $source.ContainsKey($tag)) { [void]$warn.Add("${tag}: no source file msix\listing\<code>.txt for this column; its cells stay as exported"); continue }
    $terms = @()
    foreach ($f in $rowByField.Keys) {
        $v = $rowByField[$f][$c]
        if (-not $v) { continue }
        $cap = if ($KnownCaps.ContainsKey($f)) { $KnownCaps[$f] } elseif ($f -match '^Feature\d+$') { $FeatureCap } elseif ($f -match '^SearchTerm\d+$') { $SearchTermCap } else { 0 }
        if ($cap -and $v.Length -gt $cap) { [void]$warn.Add("${tag} ${f}: $($v.Length) chars, over the $cap known when this was written (the console decides)") }
        if ($f -match '^SearchTerm\d+$') { $terms += $v.ToLowerInvariant() }
    }
    if ($terms.Count -gt $SearchTermMax) { [void]$warn.Add("${tag}: $($terms.Count) search terms, at most $SearchTermMax") }
    if (($terms | Select-Object -Unique).Count -ne $terms.Count) { [void]$warn.Add("${tag}: duplicate search terms") }
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
