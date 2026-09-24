<#
.SYNOPSIS
  Self-test for the Store listing tools: the listing sources and build-store-listing-csv.ps1.

.DESCRIPTION
  No Partner Center session and no build are needed. Three groups of checks:

  SOURCES   msix\listing\<code>.txt against the shape and the caps known when this was written,
            the house text style, and the locale set of AppxManifest.xml.
  MANIFEST  AppxManifest.xml against the Store policies that can be read off the template:
            the declared capabilities (10.6), no hidden application, and the language set
            behind the listing locales (10.7).
  BUILDER   build-store-listing-csv.ps1 against msix\testdata\listingData.fixture.csv, a real
            export's structure (453 rows, BOM, CRLF records, minimal quoting, no final newline)
            with every copy cell emptied. It carries no product data: the field IDs are the
            format's own. Replace it with FileDO's real, emptied export after the first submission.

  Exit code: 0 = every check passed, 1 = a defect was found, 2 = could not verify.

.EXAMPLE
  pwsh -NoProfile -File .\msix\test-store-tools.ps1
#>
[CmdletBinding()]
param()

$ErrorActionPreference = "Stop"
$msix    = $PSScriptRoot
$builder = Join-Path $msix "build-store-listing-csv.ps1"
$fixture = Join-Path $msix "testdata\listingData.fixture.csv"
$listing = Join-Path $msix "listing"
$shots   = Join-Path $msix "screenshots"
$hostExe = if ($PSVersionTable.PSEdition -eq 'Core') { Join-Path $PSHOME "pwsh.exe" } else { Join-Path $PSHOME "powershell.exe" }

if (-not (Test-Path $builder) -or -not (Test-Path $fixture) -or -not (Test-Path $listing)) {
    Write-Host "store-tools: NOT VERIFIED (required listing source, fixture, or builder is missing)" -ForegroundColor Yellow
    exit 2
}

$script:fail = 0; $script:pass = 0
function Check([string]$name, [bool]$ok, [string]$detail = "") {
    if ($ok) { $script:pass++; Write-Host "  PASS  $name" -ForegroundColor Green }
    else     { $script:fail++; Write-Host "  FAIL  $name  $detail" -ForegroundColor Red }
}
function Run-Builder([string[]]$argv) {
    $out = & $hostExe -NoProfile -ExecutionPolicy Bypass -File $builder @argv 2>&1 | Out-String
    return @{ Code = $LASTEXITCODE; Text = $out }
}
function Read-Fields([string]$path) {
    $f = [ordered]@{}; $name = $null; $buf = New-Object System.Collections.ArrayList
    foreach ($line in [System.IO.File]::ReadAllLines($path)) {
        if ($line -match '^@@(\w+)\s*$') { if ($name) { $f[$name] = ($buf -join "`n").Trim() }; $name = $Matches[1]; $buf.Clear() }
        elseif ($name -and $line -notmatch '^#') { [void]$buf.Add($line) }
    }
    if ($name) { $f[$name] = ($buf -join "`n").Trim() }
    return $f
}

$Locale = [ordered]@{ en = "en-us"; ru = "ru"; uk = "uk"; de = "de"; fr = "fr" }

# ---------------------------------------------------------------------------------------------
Write-Host "SOURCES" -ForegroundColor Cyan
$shared = Read-Fields (Join-Path $listing "shared.txt")
Check "shared.txt carries the Title" ($shared.Contains('Title') -and $shared['Title'] -ceq 'FileDO')
foreach ($code in $Locale.Keys) {
    $p = Join-Path $listing "$code.txt"
    if (-not (Test-Path $p)) { Check "$code.txt exists" $false; continue }
    $f = Read-Fields $p
    $missingF = @('ShortDescription', 'Description', 'ReleaseNotes') + (1..10 | ForEach-Object { "Feature$_" }) + (1..7 | ForEach-Object { "SearchTerm$_" }) | Where-Object { -not $f.Contains($_) -or -not $f[$_] }
    Check "$code.txt has every field filled" ($missingF.Count -eq 0) ($missingF -join ',')
    Check "$code.txt Description <= 10000, ShortDescription <= 1000, ReleaseNotes <= 1500" ($f['Description'].Length -le 10000 -and $f['ShortDescription'].Length -le 1000 -and $f['ReleaseNotes'].Length -le 1500) "lengths $($f['Description'].Length)/$($f['ShortDescription'].Length)/$($f['ReleaseNotes'].Length)"
    $longF = @(1..10 | Where-Object { $f["Feature$_"].Length -gt 200 })
    Check "$code.txt every feature <= 200 chars" ($longF.Count -eq 0) ("Feature" + ($longF -join ',Feature'))
    $terms = @(1..7 | ForEach-Object { $f["SearchTerm$_"] })
    Check "$code.txt search terms: 7, unique, <= 30 chars" (($terms | Select-Object -Unique).Count -eq 7 -and -not ($terms | Where-Object { $_.Length -gt 30 })) ($terms -join ' | ')
    $all = ($f.Values -join "`n")
    # house text style for prose: no em/en dash, ".." never "..."
    Check "$code.txt house text style (no em/en dash, no three dots)" (($all -notmatch [regex]::Escape([string][char]0x2014)) -and ($all -notmatch [regex]::Escape([string][char]0x2013)) -and ($all -notmatch '\.\.\.'))
    Check "$code.txt says the Store edition has no Explorer entries (plain statement, no implied parity)" ($f['Description'] -match 'MSI')
}
# ---------------------------------------------------------------------------------------------
# The manifest template against the Store policies it can be checked against statically
# (Store Policies 7.20). build-msix.ps1 asserts the PACKED manifest; these three assert the
# TEMPLATE, so a policy break is caught without a build and without the Windows SDK.
Write-Host "MANIFEST" -ForegroundColor Cyan
$manifest = [xml](Get-Content (Join-Path $msix "AppxManifest.xml") -Raw)
$mns = New-Object System.Xml.XmlNamespaceManager($manifest.NameTable)
$mns.AddNamespace('m',   'http://schemas.microsoft.com/appx/manifest/foundation/windows10')
$mns.AddNamespace('uap', 'http://schemas.microsoft.com/appx/manifest/uap/windows10')

# 10.7 - a locale the Store offers the listing in must have a package language behind it, and
# a listing source with no Resource entry is copy the Store will never show.
$resLangs = @($manifest.Package.Resources.Resource | ForEach-Object { $_.Language } | Sort-Object)
Check "manifest Resources == the listing locale set (policy 10.7)" (($resLangs -join ',') -ceq (($Locale.Values | Sort-Object) -join ',')) "manifest: $($resLangs -join ',')"

# 10.6 - capabilities are declared legitimately. runFullTrust is the standard desktop-bridge
# declaration and the only one this app can justify; anything added here has to be a deliberate
# edit of this allow-list, with a user-visible justification to go with it.
$caps = @($manifest.SelectNodes('/m:Package/m:Capabilities/*', $mns) | ForEach-Object { $_.GetAttribute('Name') } | Sort-Object)
Check "manifest declares exactly runFullTrust (policy 10.6)" (($caps -join ',') -ceq 'runFullTrust') "declared: $($caps -join ',')"

# Account-measured, not policy text: this account has no HeadlessAppBypass waiver, so a hidden
# application is rejected at package upload. The manifest's own header records the precedent.
$hidden = @($manifest.SelectNodes('/m:Package/m:Applications/m:Application', $mns) |
            Where-Object { $_.SelectSingleNode('uap:VisualElements', $mns).GetAttribute('AppListEntry') -eq 'none' } |
            ForEach-Object { $_.GetAttribute('Id') })
Check "no Application is hidden (AppListEntry='none' is rejected on this account)" ($hidden.Count -eq 0) "hidden: $($hidden -join ',')"

# ---------------------------------------------------------------------------------------------
Write-Host "BUILDER" -ForegroundColor Cyan
$tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("fd-listing-test-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
[void][System.IO.Directory]::CreateDirectory($tmp)
try {
    $fx = Join-Path $tmp "export.csv"; Copy-Item $fixture $fx

    $r = Run-Builder @('-Csv', $fx, '-FillNothing')
    Check "round trip on the fixture is byte-identical (exit 0)" ($r.Code -eq 0 -and $r.Text -match 'byte-identical') $r.Text

    # a full patch with screenshots
    $out1 = Join-Path $tmp "o1\listingData.csv"
    $r = Run-Builder @('-Csv', $fx, '-Out', $out1, '-Screenshots')
    Check "full patch exits 0" ($r.Code -eq 0) $r.Text
    $b = [System.IO.File]::ReadAllBytes($out1)
    $t = [System.Text.Encoding]::UTF8.GetString($b, 3, $b.Length - 3)
    Check "output keeps the export style: BOM, CRLF records, no final newline, minimal quoting" (($b[0] -eq 0xEF) -and ($t -match "`r`n") -and (-not $t.EndsWith("`n")) -and (-not $t.StartsWith('"')))
    $orig = Import-Csv -LiteralPath $fx -Encoding utf8
    $new  = Import-Csv -LiteralPath $out1 -Encoding utf8
    Check "row count and field order unchanged" ($orig.Count -eq $new.Count -and (($orig | ForEach-Object Field) -join '|') -ceq (($new | ForEach-Object Field) -join '|'))
    Check "no column invented, none dropped" ((($orig[0].psobject.Properties.Name) -join '|') -ceq (($new[0].psobject.Properties.Name) -join '|'))

    $bad = New-Object System.Collections.ArrayList
    foreach ($code in $Locale.Keys) {
        $tag = $Locale[$code]
        $f = Read-Fields (Join-Path $listing "$code.txt")
        foreach ($k in $shared.Keys) { if (-not $f.Contains($k)) { $f[$k] = $shared[$k] } }
        foreach ($k in $f.Keys) {
            $row = $new | Where-Object { $_.Field -ceq $k } | Select-Object -First 1
            if (-not $row) { [void]$bad.Add("$tag/$k has no row in the export"); continue }
            $cell = ([string]$row.$tag) -replace "`r`n", "`n"
            if ($cell -cne $f[$k]) { [void]$bad.Add("$tag/$k differs from its source") }
        }
    }
    Check "every source field landed in its language column, verbatim" ($bad.Count -eq 0) (($bad | Select-Object -First 4) -join '; ')

    $shotBad = New-Object System.Collections.ArrayList
    $kinds = 'capacity', 'speed', 'duplicates', 'secure', 'wipe'
    foreach ($code in $Locale.Keys) { for ($i = 1; $i -le 5; $i++) {
        $row = $new | Where-Object { $_.Field -ceq "DesktopScreenshot$i" } | Select-Object -First 1
        $want = "$($kinds[$i - 1])-$($Locale[$code]).png"
        # The cell carries the path Partner Center asks for: the import root folder's name, then the
        # file. A bare name is refused and drops the whole language, so the prefix is part of the check.
        $wantPath = "$(Split-Path -Leaf (Split-Path $out1))/$want"
        if ($row.($Locale[$code]) -cne $wantPath) { [void]$shotBad.Add("$($Locale[$code]) DesktopScreenshot$i = '$($row.($Locale[$code]))', want $wantPath") }
        if (-not (Test-Path (Join-Path $shots $want))) { [void]$shotBad.Add("$want is not in msix\screenshots") }
        if (-not (Test-Path (Join-Path (Split-Path $out1) $want))) { [void]$shotBad.Add("$want was not copied beside the CSV") }
    } }
    Check "DesktopScreenshot1..5 carry '<import folder>/<image>', present and copied beside the CSV" ($shotBad.Count -eq 0) (($shotBad | Select-Object -First 3) -join '; ')
    Check "import zip written" (Test-Path (Join-Path $tmp "o1.zip"))

    # nothing outside the copy fields and the five screenshot rows may change
    $allowed = @($Locale.Keys | ForEach-Object { (Read-Fields (Join-Path $listing "$_.txt")).Keys }) + @($shared.Keys) + @(1..5 | ForEach-Object { "DesktopScreenshot$_" }) | Select-Object -Unique
    $stray = New-Object System.Collections.ArrayList
    for ($i = 0; $i -lt $orig.Count; $i++) {
        foreach ($p in $orig[$i].psobject.Properties.Name) {
            if ($orig[$i].$p -cne $new[$i].$p -and ($allowed -notcontains $orig[$i].Field -or $p -notin $Locale.Values)) { [void]$stray.Add("$($orig[$i].Field)/$p") }
        }
    }
    Check "no cell outside the copy fields and language columns was touched" ($stray.Count -eq 0) (($stray | Select-Object -First 4) -join ', ')

    # idempotent: the patched file as a new export fills nothing
    $out2 = Join-Path $tmp "o2\listingData.csv"
    $r = Run-Builder @('-Csv', $out1, '-Out', $out2, '-Screenshots')
    Check "a second run over the patched file fills 0 cells" ($r.Text -match 'filled 0 empty cells, refreshed 0, attached 0') $r.Text

    # asset URLs are never rewritten: plant one in the export
    $planted = Join-Path $tmp "planted.csv"
    $text = [System.IO.File]::ReadAllText($fx)
    # columns: Field, ID, Type, default, en-us, ..: the URL goes into the EN cell (index 4)
    $text = ([regex]'(?m)^(DesktopScreenshot1,\d+,[^,\r\n]*,,),').Replace($text, '${1}https://example.invalid/asset-1.png,', 1)
    Check "the planted URL is really in the en-us cell of the test input" (((Import-Csv -LiteralPath $fx -Encoding utf8) | Select-Object -First 1) -ne $null -and $text -match 'asset-1\.png')
    [System.IO.File]::WriteAllText($planted, $text, (New-Object System.Text.UTF8Encoding($true)))
    $out3 = Join-Path $tmp "o3\listingData.csv"
    $r = Run-Builder @('-Csv', $planted, '-Out', $out3, '-Screenshots')
    $p3 = Import-Csv -LiteralPath $out3 -Encoding utf8 | Where-Object { $_.Field -ceq 'DesktopScreenshot1' } | Select-Object -First 1
    $pIn = Import-Csv -LiteralPath $planted -Encoding utf8 | Where-Object { $_.Field -ceq 'DesktopScreenshot1' } | Select-Object -First 1
    Check "the URL was planted in en-us and nowhere else" ($pIn.'en-us' -ceq 'https://example.invalid/asset-1.png' -and -not $pIn.ru -and -not $pIn.default)
    Check "a planted asset URL survives a -Screenshots run untouched, while the other languages still get their image" ($r.Code -eq 0 -and $p3.'en-us' -ceq 'https://example.invalid/asset-1.png' -and $p3.ru -ceq 'o3/capacity-ru.png') "en-us='$($p3.'en-us')' ru='$($p3.ru)'"

    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "o4\x.csv"), '-Refresh', 'DesktopScreenshot1')
    Check "-Refresh on a screenshot field is refused" ($r.Code -ne 0 -and $r.Text -match 'refused') $r.Text

    # -Refresh ReleaseNotes: only that field moves
    $stale = Join-Path $tmp "stale.csv"
    $s = [System.IO.File]::ReadAllText($out1)
    $s = ([regex]'(?m)^(ReleaseNotes,\d+,[^,\r\n]*,,)[^\r\n]*').Replace($s, '${1}old notes', 1)
    [System.IO.File]::WriteAllText($stale, $s, (New-Object System.Text.UTF8Encoding($true)))
    $o5 = Join-Path $tmp "o5\listingData.csv"
    [void](Run-Builder @('-Csv', $stale, '-Out', $o5))
    $o6 = Join-Path $tmp "o6\listingData.csv"
    [void](Run-Builder @('-Csv', $stale, '-Out', $o6, '-Refresh', 'ReleaseNotes'))
    $rn5 = (Import-Csv -LiteralPath $o5 -Encoding utf8 | Where-Object { $_.Field -ceq 'ReleaseNotes' }).'en-us'
    $rn6 = (Import-Csv -LiteralPath $o6 -Encoding utf8 | Where-Object { $_.Field -ceq 'ReleaseNotes' }).'en-us'
    Check "an already-filled cell is left alone without -Refresh, and rewritten with it" ($rn5 -ceq 'old notes' -and $rn6 -ceq (Read-Fields (Join-Path $listing 'en.txt'))['ReleaseNotes'])

    # -Only: nothing but the named fields
    $o7 = Join-Path $tmp "o7\listingData.csv"
    [void](Run-Builder @('-Csv', $fx, '-Out', $o7, '-Only', 'SearchTerm*'))
    $n7 = Import-Csv -LiteralPath $o7 -Encoding utf8
    $chg = @(); for ($i = 0; $i -lt $orig.Count; $i++) { foreach ($p in $Locale.Values) { if ($orig[$i].$p -cne $n7[$i].$p) { $chg += $orig[$i].Field } } }
    Check "-Only 'SearchTerm*' changes search-term rows and nothing else" ($chg.Count -eq 35 -and -not ($chg | Where-Object { $_ -notmatch '^SearchTerm\d$' })) "changed: $(($chg | Select-Object -Unique) -join ',')"

    # a locale that is not a column of the export is reported and never invented
    $nofr = Join-Path $tmp "nofr.csv"
    $lines = [System.IO.File]::ReadAllText($fx) -split "`r`n"
    $hdr = $lines[0] -split ','; $frIdx = [array]::IndexOf($hdr, 'fr')
    $cut = $lines | ForEach-Object { $c = $_ -split ',', $hdr.Count; ($c[0..($frIdx - 1)] + $(if ($frIdx + 1 -lt $c.Count) { $c[($frIdx + 1)..($c.Count - 1)] })) -join ',' }
    [System.IO.File]::WriteAllText($nofr, ($cut -join "`r`n"), (New-Object System.Text.UTF8Encoding($true)))
    $o8 = Join-Path $tmp "o8\listingData.csv"
    $r = Run-Builder @('-Csv', $nofr, '-Out', $o8)
    $h8 = (Get-Content $o8 -TotalCount 1 -Encoding utf8) -replace [char]0xFEFF, ''
    Check "a language missing from the export is warned about, never added as a column" ($r.Code -eq 0 -and $r.Text -match 'NOT in this export' -and $r.Text -match 'fr' -and ($h8 -split ',') -notcontains 'fr') $r.Text

    $r = Run-Builder @('-Csv', $fx, '-Out', $fx)
    Check "-Out equal to the export is refused" ($r.Code -ne 0 -and $r.Text -match 'never overwritten') $r.Text
}
finally {
    if (Test-Path $tmp) { [System.IO.Directory]::Delete($tmp, $true) }
}

Write-Host ""
if ($script:fail -eq 0) { Write-Host "store-tools: PASS ($script:pass checks)" -ForegroundColor Green; exit 0 }
Write-Host "store-tools: FAIL ($script:fail checks)" -ForegroundColor Red
exit 1
