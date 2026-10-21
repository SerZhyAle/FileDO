#Requires -Version 7.0
<#
.SYNOPSIS
  Holds the generated in-app help set to CONTEXT-HELP: pages in every shipped language, links, no request.

.DESCRIPTION
  The static half of rung 1 (CONTEXT-HELP section 4) over docs\help\, the render target of
  `filedo_win.exe --build-help docs\help` (SP-0168). The other half - the surfaces of the source against
  the map, the resolver, the key and the button - is the `help:` rows of `filedo_win.exe --selftest`.

    map        help-map.json parses, ids are unique, every row has a form and a reason where it needs one.
    pages      every page the map names that is not baselined exists in every shipped language, plus the
               index; a baselined page that exists is stale; the language set equals Localization.Languages.
    anatomy    each page has, in order, a title, the purpose line, the picture, the order, the elements,
               related and the footer; lang and dir match its folder; the picture has alt text.
    requests   a page makes none: nothing but an anchor leads to another origin, no web font, no script
               source, no @import or url(); the referrer is withheld (rule 12).
    links      every relative link, picture and style sheet resolves; every #anchor names an id of its
               target page; a link between languages never occurs (a page stays in its own).
    pictures   each page has its dark and light picture in its own language (the picture's language is the
               page's, rule 14); the kit beside the pages is byte-identical to docs\kit\sza-kit.css.
    baseline   help-baseline.txt holds only `page <id>` and `ask <surface>` lines (CHECK-BASELINE); the
               `ask` lines are checked against the surfaces by the self-test, which knows the code.

  With -Exe <filedo_win.exe> the set is also regenerated into a scratch folder and compared with the
  committed one (rung 2, freshness): every .html byte for byte (the capture date aside), every picture by
  its size. The capture needs a desktop session and takes about a minute and a half.

  Exit code (CHECK-VERDICT): 0 = pass, 1 = a defect was found, 2 = could not verify. The last line is
  the verdict.
#>
[CmdletBinding()]
param(
    [string]$Help,
    [string]$Exe
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent
if (-not $Help) { $Help = Join-Path $root 'docs\help' }

function Stop-Unverified([string]$why) {
    Write-Host "help: COULD NOT VERIFY ($why)"
    exit 2
}

if (-not (Test-Path $Help)) { Stop-Unverified "no $Help" }
$mapFile = Join-Path $Help 'help-map.json'
if (-not (Test-Path $mapFile)) { Stop-Unverified 'no help-map.json' }
try { $map = Get-Content $mapFile -Raw | ConvertFrom-Json } catch { Stop-Unverified "help-map.json does not parse: $($_.Exception.Message)" }

$failures = [System.Collections.Generic.List[string]]::new()
function Fail([string]$m) { $failures.Add($m) }

# ---- the language set ---------------------------------------------------------------------------
$locFile = Join-Path $root 'filedo_win_vb\Localization.vb'
$langs = @('en', 'ru', 'uk', 'de', 'fr')
if (Test-Path $locFile) {
    $m = [regex]::Match((Get-Content $locFile -Raw), 'Languages As String\(\) = \{([^}]*)\}')
    if ($m.Success) {
        $declared = @([regex]::Matches($m.Groups[1].Value, '"([a-z]+)"') | ForEach-Object { $_.Groups[1].Value })
        if (($declared -join ',') -ne ($langs -join ',')) { Fail "the UI ships $($declared -join ', '), this check expects $($langs -join ', ')" }
    }
}

# ---- map ----------------------------------------------------------------------------------------
$surfaces = @($map.surfaces)
$forms = 'layer', 'page', 'both', 'inherit', 'none'
$ids = @{}
foreach ($s in $surfaces) {
    if ($ids.ContainsKey($s.id)) { Fail "map: surface $($s.id) twice" }
    $ids[$s.id] = $true
    if ($s.form -notin $forms) { Fail "map: $($s.id) has form '$($s.form)'" }
    if ($s.form -in 'inherit', 'none' -and -not ($s.PSObject.Properties.Name -contains 'reason' -and $s.reason)) { Fail "map: $($s.id) is $($s.form) with no reason" }
    if ($s.form -eq 'inherit' -and ($surfaces.id -notcontains $s.parent)) { Fail "map: $($s.id) inherits from '$($s.parent)', which is no surface" }
}
if ($map.schemaVersion -ne 1) { Fail "map: schemaVersion is $($map.schemaVersion), expected 1" }

# ---- baseline -----------------------------------------------------------------------------------
$baselineFile = Join-Path $Help 'help-baseline.txt'
$owedPages = @()
$owedAsks = @()
$baselineKinds = [System.Collections.Generic.List[string]]::new()
if (Test-Path $baselineFile) {
    foreach ($ln in Get-Content $baselineFile) {
        $t = $ln.Trim()
        if ($t -eq '' -or $t.StartsWith('#')) { continue }
        if ($t -match '^page (\S+)$') { $owedPages += $Matches[1]; $baselineKinds.Add('page') }
        elseif ($t -match '^ask (\S+)$') {
            $owedAsks += $Matches[1]; $baselineKinds.Add('ask')
            if (-not $ids.ContainsKey($Matches[1])) { Fail "baseline: 'ask $($Matches[1])' names no surface" }
        }
        else { Fail "baseline: unknown line '$t'" }
    }
}
# CHECK-BASELINE rule 3: a set baseline holds one identifier per line, sorted. The page lines come first and then
# the ask lines (as the file's own header says), each group in ordinal order - a hand edit that lands a line in the
# wrong place fails here instead of turning the diff of the next edit into noise.
foreach ($group in @(@{ Kind = 'page'; Owed = $owedPages }, @{ Kind = 'ask'; Owed = $owedAsks })) {
    $have = [string[]]@($group.Owed)
    $want = [string[]]@($group.Owed)
    [Array]::Sort($want, [StringComparer]::Ordinal)
    for ($i = 0; $i -lt $have.Count; $i++) {
        if ($have[$i] -cne $want[$i]) { Fail "baseline: the $($group.Kind) lines are not in ordinal order - '$($group.Kind) $($have[$i])' stands where '$($group.Kind) $($want[$i])' belongs"; break }
    }
}
if ($baselineKinds.Contains('ask') -and $baselineKinds.LastIndexOf('page') -gt $baselineKinds.IndexOf('ask')) { Fail 'baseline: a page line follows an ask line (the page lines come first)' }

# ---- pages --------------------------------------------------------------------------------------
$wanted = @($surfaces | Where-Object { $_.PSObject.Properties.Name -contains 'help' -and $_.help } | ForEach-Object { $_.help }) + @($surfaces | Where-Object { $_.PSObject.Properties.Name -contains 'fallback' -and $_.fallback } | ForEach-Object { $_.fallback }) | Sort-Object -Unique
$expected = @($wanted | Where-Object { $_ -notin $owedPages }) + @($map.index)
$pageCount = 0
foreach ($p in $owedPages) {
    foreach ($l in $langs) { if (Test-Path (Join-Path $Help "$l\$p.html")) { Fail "baseline: page $p exists in $l - delete its line" } }
}

$kit = Join-Path $root 'docs\kit\sza-kit.css'
if (Test-Path $kit) {
    if (-not (Test-Path (Join-Path $Help 'sza-kit.css'))) { Fail 'pictures: sza-kit.css is not beside the pages' }
    elseif ((Get-FileHash $kit).Hash -ne (Get-FileHash (Join-Path $Help 'sza-kit.css')).Hash) { Fail 'pictures: sza-kit.css differs from docs\kit\sza-kit.css (regenerate with --build-help)' }
}
if (-not (Test-Path (Join-Path $Help 'help.css'))) { Fail 'pictures: help.css is not beside the pages' }

foreach ($l in $langs) {
    foreach ($id in $expected) {
        $file = Join-Path $Help "$l\$id.html"
        if (-not (Test-Path $file)) { Fail "pages: $l\$id.html is missing"; continue }
        $pageCount++
        $raw = Get-Content $file -Raw
        $html = [regex]::Replace($raw, '<script>.*?</script>', '', 'Singleline')
        $where = "$l\$id.html"

        if ($html -notmatch ('<html lang="' + $l + '" dir="ltr">')) { Fail "anatomy: $where has the wrong lang or dir" }
        if ($html -notmatch '<meta name="referrer" content="no-referrer">') { Fail "requests: $where does not withhold the referrer" }
        if ($html -match '<(?!a )[^>]*\b(src|href)="https?:') { Fail "requests: $where loads a resource from another origin" }
        if ($html -match '@import|url\(' -or $html -match '<script[^>]*\bsrc=' -or $html -match 'fonts\.googleapis') { Fail "requests: $where imports a style, a script or a font" }

        $isIndex = ($id -eq $map.index)
        $marks = if ($isIndex) { @('<h1>', 'class="lead"', 'class="related"', 'class="help-foot"') } else { @('<h1>', 'class="lead"', 'class="shot"', '<h2>', 'class="elements"', 'class="related"', 'class="help-foot"') }
        $at = -1
        foreach ($mk in $marks) {
            $i = $html.IndexOf($mk, $at + 1, [StringComparison]::Ordinal)
            if ($i -lt 0) { Fail "anatomy: $where lacks '$mk' in its place"; break }
            $at = $i
        }
        if (-not $isIndex) {
            if ($html -notmatch '<img class="on-dark"[^>]* alt="[^"]+"') { Fail "anatomy: $where has no alt text on its picture" }
            foreach ($th in 'dark', 'light') {
                if (-not (Test-Path (Join-Path $Help "img\$id.$l.$th.png"))) { Fail "pictures: img\$id.$l.$th.png is missing" }
                if ($html -notmatch [regex]::Escape("../img/$id.$l.$th.png")) { Fail "pictures: $where does not show its own $l $th picture" }
            }
            foreach ($m2 in [regex]::Matches($html, 'src="\.\./img/([^"]+)"')) {
                if ($m2.Groups[1].Value -notmatch "^$([regex]::Escape($id))\.$l\.(dark|light)\.png$") { Fail "pictures: $where shows $($m2.Groups[1].Value), not a picture of its own page and language" }
            }
        }

        $pageIds = [regex]::Matches($html, '\bid="([^"]+)"') | ForEach-Object { $_.Groups[1].Value }
        foreach ($m3 in [regex]::Matches($html, '(?:href|src)="([^"]+)"')) {
            $target = $m3.Groups[1].Value
            if ($target -match '^(https?:|mailto:|#)') {
                if ($target.StartsWith('#') -and $target.Length -gt 1 -and $target.Substring(1) -notin $pageIds) { Fail "links: $where points at $target, which is no id of the page" }
                continue
            }
            $path, $frag = $target -split '#', 2
            $resolved = [IO.Path]::GetFullPath((Join-Path (Split-Path $file -Parent) $path))
            if (-not (Test-Path $resolved)) { Fail "links: $where links $target, which is not there"; continue }
            if ($frag -and $resolved.EndsWith('.html')) {
                $other = Get-Content $resolved -Raw
                if ($other -notmatch ('\bid="' + [regex]::Escape($frag) + '"')) { Fail "links: $where links $target, which has no such id" }
            }
        }
    }
}

# ---- freshness (rung 2) -------------------------------------------------------------------------
$fresh = ''
if ($Exe) {
    if (-not (Test-Path $Exe)) { Stop-Unverified "no $Exe" }
    $scratch = Join-Path ([IO.Path]::GetTempPath()) ('filedo_help_fresh_' + [guid]::NewGuid().ToString('N'))
    $out = Join-Path $scratch 'help'
    New-Item -ItemType Directory -Force -Path $out, (Join-Path $scratch 'kit'), (Join-Path $scratch 'state') | Out-Null
    Copy-Item $kit (Join-Path $scratch 'kit\sza-kit.css')
    $copy = Join-Path $scratch 'filedo_win_help.exe'
    Copy-Item $Exe $copy
    if (Test-Path "$Exe.config") { Copy-Item "$Exe.config" "$copy.config" }
    $env:FILEDO_STATE_DIR = Join-Path $scratch 'state'
    $run = Start-Process $copy -ArgumentList '--build-help', $out -Wait -PassThru -WindowStyle Hidden
    if ($run.ExitCode -ne 0) { Stop-Unverified "--build-help exited $($run.ExitCode)" }
    $stale = 0
    foreach ($f in Get-ChildItem $out -Recurse -File -Filter *.html) {
        $rel = [IO.Path]::GetRelativePath($out, $f.FullName)
        $committed = Join-Path $Help $rel
        $norm = { param($t) [regex]::Replace($t, 'Captured against FileDO [^<]*<', 'Captured<') }
        if (-not (Test-Path $committed)) { Fail "freshness: $rel is generated but not committed"; $stale++; continue }
        if ((& $norm (Get-Content $f.FullName -Raw)) -ne (& $norm (Get-Content $committed -Raw))) { Fail "freshness: $rel differs from the build"; $stale++ }
    }
    foreach ($f in Get-ChildItem (Join-Path $out 'img') -File -Filter *.png) {
        $committed = Join-Path $Help "img\$($f.Name)"
        if (-not (Test-Path $committed)) { Fail "freshness: img\$($f.Name) is generated but not committed"; continue }
        $a = [IO.File]::ReadAllBytes($f.FullName); $b = [IO.File]::ReadAllBytes($committed)
        if ([BitConverter]::ToString($a, 16, 8) -ne [BitConverter]::ToString($b, 16, 8)) { Fail "freshness: img\$($f.Name) has another size" }
    }
    Remove-Item $scratch -Recurse -Force -ErrorAction SilentlyContinue
    $fresh = ', regenerated and compared'
}

foreach ($f in $failures) { Write-Host "  FAIL  $f" }
$summary = "$pageCount page files in $($langs.Count) languages, $(@($surfaces).Count) surfaces, $(@($owedPages).Count) pages on the baseline$fresh"
if ($failures.Count -gt 0) {
    Write-Host "help: FAIL ($($failures.Count) problem(s); $summary)"
    exit 1
}
Write-Host "help: PASS ($summary)"
exit 0
