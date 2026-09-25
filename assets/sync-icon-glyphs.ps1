#Requires -Version 7.0
<#
.SYNOPSIS
    Imports the catalog glyphs the shell draws, or checks that the vendored copies still match.

.DESCRIPTION
    The iconography catalog (contracts ICON-SET / ICON-RENDER / ICON-EXTERNAL) is the source of truth.
    FileDO vendors only the glyph files it draws, plus the shared palette its state colours are held
    to, into assets/glyphs/ with the SHA-256 of every file in assets/glyphs/PROVENANCE.txt. The shell
    embeds that folder and refuses to draw a byte whose hash does not match (filedo_win_vb/Glyphs.vb).
    Never hand-edit the folder - re-run this script.

    Without -Check the script re-imports the declared glyphs and palette.json and rewrites
    PROVENANCE.txt. It refuses when the catalog's ICON-SET MAJOR differs from the one recorded: a
    MAJOR is a changed shape or a changed name (the contract's section 5), which a person re-maps
    before it ships - never a copy.

    With -Check it writes nothing and compares the vendored files against the CATALOG, not against
    the local provenance record, so a glyph redrawn in the catalog is drift even while every local
    hash still matches. It also reports a meaning the shell is still waiting for (GlyphRef.Waiting
    in filedo_win_vb) once the catalog has a glyph for it, because that is the day the rail row can
    move to the vocabulary.

    The catalog root: -CatalogRoot, then SZA_CONTRACTS_ROOT in the process, then the same variable
    at user scope. AGENTS.md names the location; this script never carries a literal path to it.

.NOTES
    Exit codes (CHECK-VERDICT):
      0 - -Check: every vendored file matches the catalog; import: the import completed.
      1 - -Check: drift - a vendored file differs from the catalog, is missing, is not declared, a
          provenance record disagrees, or a waiting meaning has landed; import: refused (MAJOR).
      2 - could not verify / could not import: no catalog root, or the catalog is unreachable or
          lacks a declared glyph.
#>
[CmdletBinding()]
param(
    [string]$CatalogRoot = '',
    [switch]$Check
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# An unexpected terminating error must not surface as exit 1, which reads as drift.
trap {
    Write-Host "  $($_.Exception.Message)"
    Write-Host 'sync-icon-glyphs: NOT VERIFIED (the script failed)'
    exit 2
}

# The declared subset: every vocabulary id the shell draws (GlyphRef.Vocabulary in filedo_win_vb).
# The catalog remains the source of truth; mapping a new meaning means adding it here as well, and
# the shell's self-test fails on an id it draws that is not vendored.
$glyphIds = @(
    'action.compare', 'action.copy', 'action.delete', 'action.fill-space', 'action.find-duplicates',
    'action.recover-drive', 'action.secure', 'action.unsecure', 'action.verify', 'action.wipe',
    'app.command-line', 'app.info', 'app.settings', 'content.history', 'content.secret-file',
    'feature.capacity-test', 'feature.raw-probe', 'feature.speed-test',
    'nav.collapse', 'nav.expand', 'nav.open-external',
    'status.error', 'status.not-proven', 'status.ok', 'status.stopped'
)
$dataFiles = @('palette.json')

$subject = 'sync-icon-glyphs'

function Stop-Verdict([int]$Code, [string]$Word, [string[]]$Lines = @()) {
    foreach ($l in $Lines) { Write-Host "  $l" }
    Write-Host "${subject}: $Word"
    exit $Code
}

function Get-Sha256([string]$Path) {
    (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

if (-not $CatalogRoot) { $CatalogRoot = $env:SZA_CONTRACTS_ROOT }
if (-not $CatalogRoot) { $CatalogRoot = [Environment]::GetEnvironmentVariable('SZA_CONTRACTS_ROOT', 'User') }
if (-not $CatalogRoot) {
    Stop-Verdict 2 'NOT VERIFIED' @('no catalog root: pass -CatalogRoot or set SZA_CONTRACTS_ROOT (AGENTS.md names the location)')
}

$iconography = Join-Path $CatalogRoot 'iconography'
$sourceGlyphs = Join-Path $iconography 'glyphs'
$sourceVocabulary = Join-Path $iconography 'vocabulary.jsonl'
$sourceReadme = Join-Path $iconography 'README.md'
foreach ($required in @($sourceGlyphs, $sourceVocabulary, $sourceReadme)) {
    if (-not (Test-Path -LiteralPath $required)) {
        Stop-Verdict 2 'NOT VERIFIED' @("the iconography catalog is unreachable: '$required' was not found")
    }
}
$missingInCatalog = @(
    $glyphIds | Where-Object { -not (Test-Path -LiteralPath (Join-Path $sourceGlyphs ($_ + '.svg'))) }
    $dataFiles | Where-Object { -not (Test-Path -LiteralPath (Join-Path $iconography $_)) }
)
if ($missingInCatalog.Count -gt 0) {
    Stop-Verdict 2 'NOT VERIFIED' @($missingInCatalog | ForEach-Object { "declared file '$_' is missing from the catalog" })
}

# "ICON-SET 0.13; ICON-RENDER 0.11; ICON-EXTERNAL 0.9", read from the contract blocks.
$contractVersions = [System.Collections.Generic.List[string]]::new()
$pendingId = $null
foreach ($line in Get-Content -LiteralPath $sourceReadme) {
    if ($line -match '^id:\s*(\S+)') { $pendingId = $Matches[1] }
    elseif ($line -match '^version:\s*(\S+)' -and $pendingId) { $contractVersions.Add("$pendingId $($Matches[1])"); $pendingId = $null }
}
$versionsText = $contractVersions -join '; '
$iconSetVersion = ($contractVersions | Where-Object { $_ -like 'ICON-SET *' } | Select-Object -First 1) -replace '^ICON-SET ', ''
if (-not $iconSetVersion) { Stop-Verdict 2 'NOT VERIFIED' @('the catalog README carries no ICON-SET version block') }
$catalogMajor = ($iconSetVersion -split '\.')[0]
$vocabularyHash = Get-Sha256 $sourceVocabulary

$repoRoot = Split-Path -Parent $PSScriptRoot
$destination = Join-Path $PSScriptRoot 'glyphs'
$provenancePath = Join-Path $destination 'PROVENANCE.txt'

# The MAJOR the vendored set was mapped against, from the provenance on disk (none on a first import).
$recordedMajor = $null
$recorded = @{}
if (Test-Path -LiteralPath $provenancePath) {
    foreach ($line in Get-Content -LiteralPath $provenancePath) {
        if ($line -match '^Catalog versions: ICON-SET (\d+)\.') { $recordedMajor = $Matches[1] }
        $parts = @($line -split '\s+' | Where-Object { $_ })
        if ($parts.Count -eq 2 -and ($parts[0].EndsWith('.svg') -or $parts[0] -in $dataFiles)) { $recorded[$parts[0]] = $parts[1] }
    }
}

# The meanings the shell draws a stand-in for while the vocabulary has no record (GlyphRef.Waiting).
$waiting = [System.Collections.Generic.SortedSet[string]]::new()
foreach ($source in Get-ChildItem -LiteralPath (Join-Path $repoRoot 'filedo_win_vb') -Filter '*.vb') {
    foreach ($m in [regex]::Matches((Get-Content -LiteralPath $source.FullName -Raw), 'GlyphRef\.Waiting\("([a-z0-9.-]+)"')) {
        [void]$waiting.Add($m.Groups[1].Value)
    }
}

if ($Check) {
    $drift = [System.Collections.Generic.List[string]]::new()

    if (-not (Test-Path -LiteralPath $provenancePath)) {
        $drift.Add('assets/glyphs/PROVENANCE.txt is missing')
    } else {
        if (-not (Select-String -LiteralPath $provenancePath -SimpleMatch "Catalog versions: $versionsText" -Quiet)) {
            $drift.Add("PROVENANCE.txt does not record the catalog versions '$versionsText'")
        }
        if ($recordedMajor -ne $null -and $recordedMajor -ne $catalogMajor) {
            $drift.Add("ICON-SET MAJOR is $catalogMajor in the catalog and $recordedMajor in PROVENANCE.txt - re-map by a person, then import")
        }
        if (-not (Select-String -LiteralPath $provenancePath -SimpleMatch "Vocabulary SHA-256: $vocabularyHash" -Quiet)) {
            $drift.Add('the vocabulary changed since the import (PROVENANCE.txt records a different SHA-256) - re-read the mapped meanings')
        }
    }

    $declared = @($glyphIds | ForEach-Object { $_ + '.svg' }) + $dataFiles
    foreach ($name in $declared) {
        $target = Join-Path $destination $name
        $source = if ($name.EndsWith('.svg')) { Join-Path $sourceGlyphs $name } else { Join-Path $iconography $name }
        if (-not (Test-Path -LiteralPath $target)) { $drift.Add("$name is declared but not vendored"); continue }
        $vendored = Get-Sha256 $target
        if ($vendored -ne (Get-Sha256 $source)) { $drift.Add("$name differs from the catalog") }
        if (-not $recorded.ContainsKey($name)) { $drift.Add("$name has no line in PROVENANCE.txt") }
        elseif ($recorded[$name] -ne $vendored) { $drift.Add("$name does not match its PROVENANCE.txt line") }
    }
    if (Test-Path -LiteralPath $destination) {
        foreach ($file in Get-ChildItem -LiteralPath $destination -File) {
            if ($file.Name -eq 'PROVENANCE.txt') { continue }
            if ($declared -notcontains $file.Name) { $drift.Add("$($file.Name) is vendored but not declared in this script") }
        }
    }

    foreach ($id in $waiting) {
        if (Test-Path -LiteralPath (Join-Path $sourceGlyphs ($id + '.svg'))) {
            $drift.Add("the shell still waits for '$id', and the catalog now has its glyph - map it (SP-0016 T1) and import")
        }
    }

    if ($drift.Count -gt 0) {
        Stop-Verdict 1 "FAIL ($($drift.Count) drift, catalog $versionsText)" $drift
    }
    Stop-Verdict 0 "PASS ($($glyphIds.Count) glyphs and $($dataFiles.Count) data file match the catalog, $versionsText; $($waiting.Count) meanings still waiting)"
}

if ($recordedMajor -ne $null -and $recordedMajor -ne $catalogMajor) {
    Stop-Verdict 1 "REFUSED (ICON-SET MAJOR $recordedMajor -> $catalogMajor)" @(
        'a MAJOR changes a shape or a name: re-read every mapped meaning, then delete PROVENANCE.txt and import')
}

New-Item -ItemType Directory -Force -Path $destination | Out-Null

# A vendored file the lists no longer declare would otherwise linger with no provenance line.
$declaredNames = @($glyphIds | ForEach-Object { $_ + '.svg' }) + $dataFiles + @('PROVENANCE.txt')
foreach ($file in Get-ChildItem -LiteralPath $destination -File) {
    if ($declaredNames -notcontains $file.Name) { Remove-Item -LiteralPath $file.FullName }
}

$provenance = [System.Collections.Generic.List[string]]::new()
$provenance.Add('ICONOGRAPHY VENDORED ARTIFACT')
$provenance.Add('Do not hand-edit the files in this folder. Regenerate with assets/sync-icon-glyphs.ps1.')
$provenance.Add("Catalog versions: $versionsText")
$provenance.Add("Vocabulary SHA-256: $vocabularyHash")
$provenance.Add('Source: iconography/glyphs/<id>.svg and iconography/palette.json in the shared contracts catalog')
$provenance.Add("Taken: $(Get-Date -Format 'yyyy-MM-dd')")
$provenance.Add('Licence: the drawings are Material Icons (Apache-2.0) - see THIRD-PARTY-NOTICES.txt')
$provenance.Add('')

foreach ($id in $glyphIds) {
    $target = Join-Path $destination ($id + '.svg')
    Copy-Item -LiteralPath (Join-Path $sourceGlyphs ($id + '.svg')) -Destination $target -Force
    $provenance.Add("$id.svg  $(Get-Sha256 $target)")
}
foreach ($name in $dataFiles) {
    $target = Join-Path $destination $name
    Copy-Item -LiteralPath (Join-Path $iconography $name) -Destination $target -Force
    $provenance.Add("$name  $(Get-Sha256 $target)")
}

$utf8 = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllLines($provenancePath, $provenance, $utf8)

Stop-Verdict 0 "SYNCED ($($glyphIds.Count) glyphs and $($dataFiles.Count) data file, $versionsText)"
