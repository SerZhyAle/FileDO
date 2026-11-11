<#
.SYNOPSIS
  Self-test for the Store listing tools: the listing sources and build-store-listing-csv.ps1.

.DESCRIPTION
  No Partner Center session and no build are needed. Three groups of checks:

  SOURCES   msix\listing\<code>.txt against the shape and the caps known when this was written,
            the house text style, and the locale set of AppxManifest.xml.
  MANIFEST  AppxManifest.xml against the Store policies that can be read off the template:
            the declared capabilities (10.6), no hidden application, and the language set
            behind the listing locales (10.7). Plus the opt-in Explorer command (AUD-17-F1):
            build-msix.ps1's own manifest generation and packed-manifest check (msix-manifest.ps1),
            run on the template in both shapes - default without it, -ExplorerCommand with it.
  BUILDER   build-store-listing-csv.ps1 against msix\testdata\listingData.fixture.csv, a real
            export's structure (453 rows, BOM, CRLF records, minimal quoting, no final newline)
            with every copy cell emptied. It carries no product data: the field IDs are the
            format's own. Replace it with FileDO's real, emptied export after the first submission.
            Also the WINDOWS-STORE 0.2 refusals, each with a negative case that must exit 1 and
            write nothing: more than 21 search-term words in a language (and exactly 21 passing),
            a cell over its cap in the unit Partner Center counts, a duplicate term, a shipped
            language with no column in the export or no source file; the WINDOWS-STORE 0.3 item B
            dialect refusals (a foreign record break, a mixed in-cell break, an open last quoted cell);
            and the fixture marked -text.

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
    Write-Host "store-tools: COULD NOT VERIFY (run=0 skipped=0, required listing source, fixture, or builder is missing)" -ForegroundColor Yellow
    exit 2
}

$script:fail = 0; $script:pass = 0
function Check([string]$name, [bool]$ok, [string]$detail = "") {
    if ($ok) { $script:pass++; Write-Host "  PASS  $name" -ForegroundColor Green }
    else     { $script:fail++; Write-Host "  FAIL  $name  $detail" -ForegroundColor Red }
}
function Run-Builder([string[]]$argv, [string]$script = $builder) {
    $out = & $hostExe -NoProfile -ExecutionPolicy Bypass -File $script @argv 2>&1 | Out-String
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
    # WINDOWS-STORE 0.2: at most 21 words across the seven cells (a word is a non-whitespace run).
    $termWords = ($terms | ForEach-Object { ([regex]::Matches([string]$_, '\S+')).Count } | Measure-Object -Sum).Sum
    Check "$code.txt search terms: at most 21 words in all (WINDOWS-STORE 0.2)" ($termWords -le 21) "$termWords words: $($terms -join ' | ')"
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

# SP-0005 9.2: this is an association, not an Explorer context-menu handler.
# The association is owned by the GUI, which already accepts a file argument.
$fdsecTypes = @($manifest.SelectNodes('/m:Package/m:Applications/m:Application[@Id="FileDOGui"]/m:Extensions/uap:Extension[@Category="windows.fileTypeAssociation"]/uap:FileTypeAssociation[@Name="filedo.securecontainer"]/uap:SupportedFileTypes/uap:FileType', $mns) | ForEach-Object { $_.InnerText })
Check "manifest associates .fd-sec with the GUI (SP-0005 9.2)" (($fdsecTypes -join ',') -ceq '.fd-sec') "types: $($fdsecTypes -join ',')"

# AUD-17-F1: the packaged Explorer command is opt-in (build-msix.ps1 -ExplorerCommand). The listing
# claim above ("no Explorer entries") holds only because a DEFAULT build drops it. These run the
# same manifest generation and packed-manifest assertion build-msix.ps1 runs, on the template,
# with synthetic package entry lists - no SDK, no build.
. (Join-Path $msix "msix-manifest.ps1")
$genArgs = @{ TemplatePath = (Join-Path $msix "AppxManifest.xml"); IdentityName = 'SZA.FileDO.LocalTest'
              Publisher = 'CN=SZA-LocalTest'; PublisherDisplayName = 'SZA (local test)'; Version = '26.926.1200.0' }
$cppClsid = Get-FileDOShellClsid (Join-Path (Split-Path $msix -Parent) "shellext\FileDOShell.cpp")
$baseEntries = @('AppxManifest.xml', 'filedo.exe', 'filedo_win.exe')
$withDll     = $baseEntries + 'FileDOShell.dll'
try {
    $defXml = New-FileDOManifest @genArgs
    $expXml = New-FileDOManifest @genArgs -ExplorerCommand
    foreach ($shape in @($defXml, $expXml)) {
        $startupProblems = @(Test-ApplicationStartup -Manifest $shape)
        Check 'startup task is disabled, unique and GUI-only' ($startupProblems.Count -eq 0) ($startupProblems -join '; ')
    }
    foreach ($change in @('missing', 'enabled', 'task-id', 'executable', 'multiple')) {
        $badStartup = $defXml.Clone()
        $startupExt = $badStartup.SelectSingleNode('//*[@Category="windows.startupTask"]')
        $startupTask = $startupExt.FirstChild
        # PreserveWhitespace means the first child can be whitespace.
        $startupTask = $startupExt.SelectSingleNode('*')
        switch ($change) {
            'missing' { [void]$startupExt.ParentNode.RemoveChild($startupExt) }
            'enabled' { $startupTask.SetAttribute('Enabled', 'true') }
            'task-id' { $startupTask.SetAttribute('TaskId', 'OtherTask') }
            'executable' { $startupExt.SetAttribute('Executable', 'filedo.exe') }
            'multiple' { [void]$startupExt.ParentNode.AppendChild($startupExt.Clone()) }
        }
        $startupProblems = @(Test-ApplicationStartup -Manifest $badStartup)
        Check "startup task refuses $change" ($startupProblems.Count -gt 0) ($startupProblems -join '; ')
    }
    $defCats = @($defXml.SelectNodes("//*[@Category]") | ForEach-Object { $_.GetAttribute('Category') })
    $expCats = @($expXml.SelectNodes("//*[@Category]") | ForEach-Object { $_.GetAttribute('Category') })
    Check "default manifest has no windows.fileExplorerContextMenus / windows.comServer extension" (-not ($defCats | Where-Object { $_ -in 'windows.fileExplorerContextMenus', 'windows.comServer' })) "categories: $($defCats -join ',')"
    Check "default manifest keeps the .fd-sec and .fdd associations and the CLI alias" (@($defCats | Where-Object { $_ -eq 'windows.fileTypeAssociation' }).Count -eq 2 -and 'windows.fileTypeAssociation' -in $defCats -and 'windows.appExecutionAlias' -in $defCats) "categories: $($defCats -join ',')"
    Check "-ExplorerCommand manifest carries both extensions" ('windows.fileExplorerContextMenus' -in $expCats -and 'windows.comServer' -in $expCats) "categories: $($expCats -join ',')"
    Check "default manifest + no FileDOShell.dll passes the package check" ((Test-ExplorerCommand -Manifest $defXml -Entries $baseEntries -ExplorerCommand $false).Count -eq 0)
    $p = Test-ExplorerCommand -Manifest $defXml -Entries $withDll -ExplorerCommand $false
    Check "default build that packs FileDOShell.dll is refused" ($p.Count -eq 1 -and $p[0] -match 'FileDOShell\.dll') ($p -join '; ')
    $p = Test-ExplorerCommand -Manifest $expXml -Entries $baseEntries -ExplorerCommand $false
    Check "default build whose manifest keeps the command is refused (both extensions named)" ($p.Count -eq 2) ($p -join '; ')
    $p = Test-ExplorerCommand -Manifest $expXml -Entries $withDll -ExplorerCommand $true -SourceClsid $cppClsid
    Check "-ExplorerCommand manifest + FileDOShell.dll passes (CLSID agrees with FileDOShell.cpp)" ($cppClsid -and $p.Count -eq 0) ($p -join '; ')
    $p = Test-ExplorerCommand -Manifest $expXml -Entries $baseEntries -ExplorerCommand $true -SourceClsid $cppClsid
    Check "-ExplorerCommand build without FileDOShell.dll is refused" ($p.Count -ge 1 -and ($p -join ' ') -match 'FileDOShell\.dll') ($p -join '; ')
    $p = Test-ExplorerCommand -Manifest $defXml -Entries $withDll -ExplorerCommand $true -SourceClsid $cppClsid
    Check "-ExplorerCommand build whose manifest lacks the command is refused" ($p.Count -ge 2) ($p -join '; ')

    # WINDOWS-STORE rule 2 and rung 3: the packed file offered for upload is unsigned.
    $p = Test-UnsignedStorePackage -Entries $baseEntries
    Check "a package with no AppxSignature.p7x entry passes the unsigned-upload check" ($p.Count -eq 0) ($p -join '; ')
    $p = Test-UnsignedStorePackage -Entries ($baseEntries + 'AppxSignature.p7x')
    Check "a package carrying AppxSignature.p7x is refused for a Store upload" ($p.Count -eq 1 -and $p[0] -match 'AppxSignature\.p7x') ($p -join '; ')
    $p = Test-UnsignedStorePackage -Entries ($baseEntries + 'appxsignature.P7X')
    Check "the AppxSignature.p7x check does not depend on case" ($p.Count -eq 1) ($p -join '; ')

    # SP-0004 T6.27: .fdd is declared for the GUI app only (its info and export page), once, with
    # no verb - the Store build cannot mount. The same check build-msix.ps1 runs on the packed
    # manifest, here on the template in both shapes and on four broken copies of it.
    $fddEntries = $baseEntries + 'Assets\DiskContainer.png'
    foreach ($shape in @(@{ Name = 'default'; Xml = $defXml }, @{ Name = '-ExplorerCommand'; Xml = $expXml })) {
        $p = Test-DiskContainerAssociation -Manifest $shape.Xml -Entries $fddEntries
        Check "$($shape.Name) manifest declares .fdd once, for the GUI, with no verb (T6.27)" ($p.Count -eq 0) ($p -join '; ')
    }
    $p = Test-DiskContainerAssociation -Manifest $defXml -Entries $baseEntries
    Check ".fdd association whose logo is not packed is refused" ($p.Count -eq 1 -and $p[0] -match 'logo') ($p -join '; ')
    $uapNs = 'http://schemas.microsoft.com/appx/manifest/uap/windows10'
    $fddAssoc = "//*[local-name()='FileTypeAssociation'][@Name='filedo.diskcontainer']"
    $bad = $defXml.Clone()
    $sv = $bad.CreateElement('uap', 'SupportedVerbs', $uapNs)
    $vb = $bad.CreateElement('uap', 'Verb', $uapNs); $vb.SetAttribute('Id', 'mount'); $vb.InnerText = 'Mount'
    [void]$sv.AppendChild($vb); [void]$bad.SelectSingleNode($fddAssoc).AppendChild($sv)
    $p = Test-DiskContainerAssociation -Manifest $bad -Entries $fddEntries
    Check "a mount verb on the .fdd association is refused" ($p.Count -ge 1 -and ($p -join ' ') -match 'verb') ($p -join '; ')
    $bad = $defXml.Clone()
    $ext = $bad.SelectSingleNode($fddAssoc).ParentNode; [void]$ext.ParentNode.RemoveChild($ext)
    $p = Test-DiskContainerAssociation -Manifest $bad -Entries $fddEntries
    Check "a manifest without the .fdd association is refused" ($p.Count -eq 1 -and $p[0] -match '0 filedo.diskcontainer') ($p -join '; ')
    $bad = $defXml.Clone()
    $ft = $bad.CreateElement('uap', 'FileType', $uapNs); $ft.InnerText = '.fdd'
    [void]$bad.SelectSingleNode("//*[local-name()='FileTypeAssociation'][@Name='filedo.securecontainer']/*[local-name()='SupportedFileTypes']").AppendChild($ft)
    $p = Test-DiskContainerAssociation -Manifest $bad -Entries $fddEntries
    Check ".fdd declared a second time (on the .fd-sec association) is refused" ($p.Count -eq 1 -and $p[0] -match '2 times') ($p -join '; ')
    $bad = $expXml.Clone()
    $bad.SelectSingleNode("//*[local-name()='ItemType'][@Type='*']").SetAttribute('Type', '.fdd')
    $p = Test-DiskContainerAssociation -Manifest $bad -Entries $fddEntries
    Check "an Explorer command on .fdd is refused" ($p.Count -eq 1 -and $p[0] -match 'Explorer command') ($p -join '; ')
} catch {
    Check "manifest generation (msix-manifest.ps1) runs" $false $_.Exception.Message
}

# ---------------------------------------------------------------------------------------------
Write-Host "BUILDER" -ForegroundColor Cyan
# WINDOWS-STORE rule 13: the committed export is marked -text, so git (core.autocrlf on a developer machine
# or a runner) never normalises the BOM, the CRLF record breaks or the LF inside cells it is compared by.
$attrs = [System.IO.File]::ReadAllText((Join-Path (Split-Path $msix -Parent) ".gitattributes"))
Check "the committed export fixture is marked -text in .gitattributes (rule 13)" ($attrs -match '(?m)^msix/testdata/listingData\.fixture\.csv[ \t]+-text[ \t\r]*$')
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

    # a shipped language that is not a column of the export is refused by name (WINDOWS-STORE rule 7, section 3)
    # and never invented: an import drops the copy of a column it does not have, without an error
    $nofr = Join-Path $tmp "nofr.csv"
    $lines = [System.IO.File]::ReadAllText($fx) -split "`r`n"
    $hdr = $lines[0] -split ','; $frIdx = [array]::IndexOf($hdr, 'fr')
    $cut = $lines | ForEach-Object { $c = $_ -split ',', $hdr.Count; ($c[0..($frIdx - 1)] + $(if ($frIdx + 1 -lt $c.Count) { $c[($frIdx + 1)..($c.Count - 1)] })) -join ',' }
    [System.IO.File]::WriteAllText($nofr, ($cut -join "`r`n"), (New-Object System.Text.UTF8Encoding($true)))
    $o8 = Join-Path $tmp "o8\listingData.csv"
    $r = Run-Builder @('-Csv', $nofr, '-Out', $o8)
    Check "a shipped language missing from the export is refused by name (exit 1) and nothing is written" ($r.Code -eq 1 -and $r.Text -match "refused: fr: the export has no 'fr' column" -and -not (Test-Path (Split-Path $o8))) $r.Text
    $r = Run-Builder @('-Csv', $nofr, '-FillNothing')
    Check "the round-trip self-test refuses that export too" ($r.Code -eq 1 -and $r.Text -match "refused: fr: the export has no 'fr' column" -and $r.Text -notmatch 'byte-identical') $r.Text

    $r = Run-Builder @('-Csv', $fx, '-Out', $fx)
    Check "-Out equal to the export is refused" ($r.Code -ne 0 -and $r.Text -match 'never overwritten') $r.Text

    # The self-test of the round trip leaves nothing behind, not even the empty default import folder.
    $dflt = Join-Path $msix "out\store-import"
    $hadDflt = Test-Path $dflt
    $r = Run-Builder @('-Csv', $fx, '-FillNothing')
    Check "-FillNothing creates no import folder" ($r.Code -eq 0 -and ($hadDflt -or -not (Test-Path $dflt))) $r.Text

    # --- WINDOWS-STORE 0.2 and rule 9: the caps refuse BEFORE anything is written ---------------------
    # The builder reads its decks from <its own folder>\listing, so a copy of the script beside a copy of
    # the decks lets a case break a deck without touching the real one.
    $alt = Join-Path $tmp "alt\msix"
    [void][System.IO.Directory]::CreateDirectory($alt)
    Copy-Item $builder $alt
    $altBuilder = Join-Path $alt "build-store-listing-csv.ps1"
    $altListing = Join-Path $alt "listing"
    function Reset-Decks { if (Test-Path $altListing) { [System.IO.Directory]::Delete($altListing, $true) }; Copy-Item $listing $altListing -Recurse }
    function Set-Deck([string]$code, [string]$field, [string]$value) {
        $path = Join-Path $altListing "$code.txt"
        $deck = [System.IO.File]::ReadAllText($path)
        $deck = ([regex]"(?ms)^@@$field[ \t]*\r?\n.*?(?=^@@|\z)").Replace($deck, "@@$field`n$value`n`n", 1)
        [System.IO.File]::WriteAllText($path, $deck, (New-Object System.Text.UTF8Encoding($false)))
    }
    function Built([string]$name) { Test-Path (Join-Path $tmp $name) }   # was the output folder created at all?

    Reset-Decks
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "cap0\listingData.csv")) $altBuilder
    Check "the unmodified decks pass the caps and the run reports each language's search-term words" ($r.Code -eq 0 -and $r.Text -match 'search-term words \(cap 21\): en-us \d+, ru \d+, uk \d+, de \d+, fr \d+' -and (Built "cap0\listingData.csv")) $r.Text

    # exactly 21 words passes, 22 is refused naming the language and the count, and nothing is created
    1..7 | ForEach-Object { Set-Deck 'en' "SearchTerm$_" "aa$_ bb$_ cc$_" }
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "cap21\listingData.csv")) $altBuilder
    Check "exactly 21 search-term words in a language pass" ($r.Code -eq 0 -and $r.Text -match 'en-us 21,' -and (Built "cap21\listingData.csv")) $r.Text
    Set-Deck 'en' 'SearchTerm7' "aa7 bb7 cc7 dd7"
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "cap22\listingData.csv")) $altBuilder
    Check "22 search-term words are refused: exit 1, the language and both numbers named" ($r.Code -eq 1 -and $r.Text -match 'refused: en-us: 22 words across the 7 search terms, over the cap of 21' -and $r.Text -match 'msix\\listing\\en\.txt') $r.Text
    Check "an over-cap run writes nothing: no file, no folder, no zip" (-not (Built "cap22") -and -not (Built "cap22.zip")) ((Get-ChildItem $tmp -Recurse -Filter 'listingData.csv' | ForEach-Object FullName) -join '; ')
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "cap22s\listingData.csv"), '-Screenshots') $altBuilder
    Check "an over-cap -Screenshots run writes no CSV, copies no image and builds no zip" ($r.Code -eq 1 -and -not (Built "cap22s") -and -not (Built "cap22s.zip")) $r.Text
    $r = Run-Builder @('-Csv', $fx, '-FillNothing') $altBuilder
    Check "the round-trip self-test does not read the copy, so an over-cap deck does not stop it" ($r.Code -eq 0 -and $r.Text -match 'byte-identical') $r.Text

    # ReleaseNotes is counted after CRLF normalisation: 600 lines are 1199 characters joined by LF, 1798 by CRLF
    Reset-Decks
    Set-Deck 'ru' 'ReleaseNotes' ((1..600 | ForEach-Object { 'x' }) -join "`n")
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "cap1798\listingData.csv")) $altBuilder
    Check "a 1199-character ReleaseNotes (1798 after CRLF normalisation) is refused, in the unit Partner Center counts" ($r.Code -eq 1 -and $r.Text -match 'refused: ru ReleaseNotes: 1798 characters, over the cap of 1500' -and -not (Built "cap1798")) $r.Text

    # a cell the builder does not own counts too: an over-cap value already in the export is in the file
    Reset-Decks
    $heavy = Join-Path $tmp "heavy.csv"
    $htext = [System.IO.File]::ReadAllText($fx)
    $htext = ([regex]'(?m)^(ShortDescription,\d+,[^,\r\n]*,,),').Replace($htext, ('${1}' + ('s' * 1001) + ','), 1)
    [System.IO.File]::WriteAllText($heavy, $htext, (New-Object System.Text.UTF8Encoding($true)))
    $r = Run-Builder @('-Csv', $heavy, '-Out', (Join-Path $tmp "capheavy\listingData.csv")) $altBuilder
    Check "an over-cap cell already in the export is refused too (1001 characters in ShortDescription)" ($r.Code -eq 1 -and $r.Text -match 'refused: en-us ShortDescription: 1001 characters, over the cap of 1000' -and -not (Built "capheavy")) $r.Text

    # a duplicate search term is refused (rule 15)
    Reset-Decks
    Set-Deck 'de' 'SearchTerm2' 'usb stick tester'
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "capdup\listingData.csv")) $altBuilder
    Check "a duplicate search term is refused" ($r.Code -eq 1 -and $r.Text -match 'refused: de: duplicate search terms' -and -not (Built "capdup")) $r.Text

    # a shipped language with a column but no deck is refused by name (rule 7, section 3)
    Reset-Decks
    [System.IO.File]::Delete((Join-Path $altListing "fr.txt"))
    $r = Run-Builder @('-Csv', $fx, '-Out', (Join-Path $tmp "nodeck\listingData.csv")) $altBuilder
    Check "a shipped language with no source file is refused by name (exit 1) and nothing is written" ($r.Code -eq 1 -and $r.Text -match 'refused: fr: no source file msix\\listing\\fr\.txt' -and -not (Built "nodeck")) $r.Text

    # WINDOWS-STORE 0.3 item B (rule 8): a writer of the export's own dialect refuses a foreign record break,
    # a mixed in-cell break and an open last quoted cell, before anything is written
    Reset-Decks
    $ftext = [System.IO.File]::ReadAllText($fx)
    $fi = $ftext.IndexOf("`r`n")
    $foreign = Join-Path $tmp "foreign.csv"
    [System.IO.File]::WriteAllText($foreign, ($ftext.Substring(0, $fi) + "`n" + $ftext.Substring($fi + 2)), (New-Object System.Text.UTF8Encoding($true)))
    $r = Run-Builder @('-Csv', $foreign, '-Out', (Join-Path $tmp "foreign\listingData.csv")) $altBuilder
    Check "a bare LF record break in a CRLF export is refused (exit 1, the record named, nothing written)" ($r.Code -eq 1 -and $r.Text -match 'refused: the export: record 1 ends with LF, the export''s record break is CRLF' -and -not (Built "foreign")) $r.Text

    # an en-us cell that holds a CRLF beside the LF of a patched Description is a mixed in-cell break
    Reset-Decks
    $mtext = [System.IO.File]::ReadAllText($fx)
    $mtext = ([regex]'(?m)^(ShortDescription,\d+,[^,\r\n]*,,),').Replace($mtext, ('${1}"a' + "`r`n" + 'b",'), 1)
    $mixed = Join-Path $tmp "mixed.csv"
    [System.IO.File]::WriteAllText($mixed, $mtext, (New-Object System.Text.UTF8Encoding($true)))
    $r = Run-Builder @('-Csv', $mixed, '-Out', (Join-Path $tmp "mixed\listingData.csv")) $altBuilder
    Check "in-cell breaks that mix CRLF and LF in the written file are refused (exit 1, nothing written)" ($r.Code -eq 1 -and $r.Text -match 'in-cell breaks mix LF and CRLF' -and -not (Built "mixed")) $r.Text

    Reset-Decks
    $otext = [System.IO.File]::ReadAllText($fx) + "`r`n" + 'Open,"unclosed'
    $open = Join-Path $tmp "open.csv"
    [System.IO.File]::WriteAllText($open, $otext, (New-Object System.Text.UTF8Encoding($true)))
    $r = Run-Builder @('-Csv', $open, '-Out', (Join-Path $tmp "open\listingData.csv")) $altBuilder
    Check "an open last quoted cell in the export is refused (exit 1, nothing written)" ($r.Code -eq 1 -and $r.Text -match 'the last quoted cell is left open' -and -not (Built "open")) $r.Text
}
finally {
    if (Test-Path $tmp) { [System.IO.Directory]::Delete($tmp, $true) }
}

. (Join-Path $PSScriptRoot 'wack-verdict.ps1')
Write-Host 'WACK verdict policy' -ForegroundColor Cyan
$requiredPass = '<TEST NAME="Required"><RESULT>PASS</RESULT></TEST>'
$optionalFailure = '<TEST NAME="Advisory" OPTIONAL="TRUE"><RESULT>FAIL</RESULT></TEST>'
$optionalWarning = '<TEST NAME="Advisory" OPTIONAL="TRUE"><RESULT>WARNING</RESULT></TEST>'
$requiredWarning = '<TEST NAME="Required"><RESULT>WARNING</RESULT></TEST>'
Check 'required PASS with optional FAIL preserves a passing gate' (Get-WackVerdict ([xml]("<REPORT OVERALL_RESULT='WARNING'>$requiredPass$optionalFailure</REPORT>"))).Passed
Check 'required PASS with optional WARNING preserves a passing gate' (Get-WackVerdict ([xml]("<REPORT OVERALL_RESULT='WARNING'>$requiredPass$optionalWarning</REPORT>"))).Passed
Check 'a required WARNING still blocks' (-not (Get-WackVerdict ([xml]("<REPORT OVERALL_RESULT='WARNING'>$requiredWarning</REPORT>"))).Passed)
Check 'an overall FAIL still blocks despite passing required rows' (-not (Get-WackVerdict ([xml]("<REPORT OVERALL_RESULT='FAIL'>$requiredPass</REPORT>"))).Passed)
Check 'an empty report cannot pass' (-not (Get-WackVerdict ([xml]"<REPORT OVERALL_RESULT='PASS'/>")).Passed)
Check 'optional-only reports cannot pass' (-not (Get-WackVerdict ([xml]("<REPORT OVERALL_RESULT='WARNING'>$optionalFailure</REPORT>"))).Passed)
Check 'a missing optional result cannot pass' (-not (Get-WackVerdict ([xml]("<REPORT OVERALL_RESULT='WARNING'>$requiredPass<TEST OPTIONAL='TRUE'/></REPORT>"))).Passed)
Write-Host ""
# CHECK-VERDICT 0.14 item A: a self-check of its own assertions ends in a test verdict line.
$ran = $script:pass + $script:fail
if ($script:fail -eq 0) { Write-Host "store-tools: PASS (run=$ran skipped=0)" -ForegroundColor Green; exit 0 }
Write-Host "store-tools: FAIL (run=$ran skipped=0 failed=$script:fail)" -ForegroundColor Red
exit 1
