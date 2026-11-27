#Requires -Version 7.0
<#
.SYNOPSIS
  Holds the documentation corpus to DOC-INTERNAL-QUALITY: registry, links, style, offline safety.

.DESCRIPTION
  Four checks over the documents git sees (tracked, or new and not ignored), in one run so that
  one run names every defect it found (SP-0061):

    registry  docs/DOCUMENT_REGISTRY.jsonl declares every maintained document - path, topic,
              product area, update triggers, role (source|render) and corpus (internal|external).
              Both directions are checked: a declared file that is not on disk fails, and a
              .md/.html file (or a winget/*.yaml manifest) that is neither declared nor under an
              ignore record fails (contract rules 1-2).
    links     Every relative link and image in every declared source document resolves to a
              file or folder that a clone of the repository has - never to a git-ignored path -
              and every #anchor names a heading or id in its target (rule 3). Scheme links, site-
              root links (/..) and links built at runtime are not files in this tree and are left
              alone.
    style     The internal corpus only (the external corpus is DOC-EXTERNAL-QUALITY's): no three-
              dot ellipsis or U+2026, no en or em dash, no http:// link, no <script>, <iframe>,
              <link>, <object> or <embed>, no remote image (rules 5-7). Fenced code blocks and
              inline code spans are exempt - a command is quoted, not written in house style.

    generated docs/CLI.md is a render target of docs/cli-commands.json (APP-COMMANDS rule 20): packaging/
              write-cli-docs.ps1 -Check fails when it is stale, when the manifest and the copy filedo.exe
              embeds differ, or when a commands guide page (en, de, fr) leaves a command out (SP-0220).
              The component and channel table of the install guide (en, de, fr) is a render target of
              packaging/wix/FileDO.wxs, msix/AppxManifest.xml and the winget installer manifest:
              packaging/write-install-matrix.ps1 -Check fails when it is stale (SP-0166 F18). The command reference
              pages (en, de, fr) are a render target of the READMEs' Command Reference, read check and Advanced
              Features: packaging/write-cli-reference.ps1 -Check fails when they are stale (SP-0166 F12).
              The product's pictures - the master's SVG forms, every rendition and assets/identity/identity-
              manifest.json - are a render target of the drawing in assets/make-product-icons.ps1
              (PRODUCT-IDENTITY): its -Check fails on a file that differs or a retired second drawing (FILEDO-0236).

  Exit code (CHECK-VERDICT): 0 = pass, 1 = a defect was found, 2 = could not verify (git cannot
  list the tree, the registry cannot be read, or the manifest does not parse). The last line is the verdict.
#>
[CmdletBinding()]
param(
    [string]$Registry
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent
if (-not $Registry) { $Registry = Join-Path $root 'docs\DOCUMENT_REGISTRY.jsonl' }
$registryRel = [IO.Path]::GetRelativePath($root, $Registry) -replace '\\', '/'

function Stop-Unverified([string]$why) {
    Write-Host "internal-docs: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

# ---- The tree as a clone has it ----------------------------------------------
if (-not (Get-Command git -ErrorAction SilentlyContinue)) { Stop-Unverified "'git' is not on PATH" }
$listing = & git -C $root ls-files --cached --others --exclude-standard 2>&1
if ($LASTEXITCODE -ne 0) { Stop-Unverified "git cannot list the tree: $(($listing | Out-String).Trim())" }
# ls-files --cached still names a tracked file deleted in the working tree.
$files = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
$dirs = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::OrdinalIgnoreCase)
foreach ($f in $listing) {
    $f = [string]$f
    if (-not $f -or -not (Test-Path -LiteralPath (Join-Path $root $f) -PathType Leaf)) { continue }
    [void]$files.Add($f)
    $d = $f
    while (($i = $d.LastIndexOf('/')) -gt 0) { $d = $d.Substring(0, $i); if (-not $dirs.Add($d)) { break } }
}

# ---- Registry ------------------------------------------------------------------
if (-not (Test-Path -LiteralPath $Registry)) { Stop-Unverified "registry is missing: $registryRel" }
$records = @()
$n = 0
try {
    foreach ($line in Get-Content -LiteralPath $Registry) {
        $n++
        if (-not $line.Trim()) { continue }
        $records += , @{ Line = $n; Data = ($line | ConvertFrom-Json) }
    }
} catch {
    Stop-Unverified "registry is unreadable at line ${n}: $($_.Exception.Message)"
}

$failures = [System.Collections.Generic.List[string]]::new()
$ignores = [System.Collections.Generic.List[string]]::new()
$docs = [ordered]@{}
foreach ($r in $records) {
    $d = $r.Data
    $names = @($d.PSObject.Properties.Name)
    $where = "${registryRel}:$($r.Line)"
    if ('ignore' -in $names) {
        if (-not [string]$d.ignore -or -not [string]$d.reason) { $failures.Add("$where ignore record needs 'ignore' and 'reason'") }
        else { $ignores.Add((([string]$d.ignore) -replace '\\', '/')) }
        continue
    }
    $missing = @('path', 'topic', 'area', 'triggers', 'role', 'corpus' | Where-Object { $_ -notin $names -or -not $d.$_ })
    if ($missing.Count) { $failures.Add("$where record is missing $($missing -join ', ')"); continue }
    $path = ([string]$d.path) -replace '\\', '/'
    if ($d.triggers -isnot [array] -or @($d.triggers | Where-Object { -not [string]$_ }).Count) { $failures.Add("$where $path triggers must be a list of non-empty strings") }
    if ($d.role -notin 'source', 'render') { $failures.Add("$where $path role is '$($d.role)', not source or render") }
    if ($d.corpus -notin 'internal', 'external') { $failures.Add("$where $path corpus is '$($d.corpus)', not internal or external") }
    if ($docs.Contains($path)) { $failures.Add("$where $path is declared twice"); continue }
    $docs[$path] = $d
    if (-not $files.Contains($path)) {
        $why = if (Test-Path -LiteralPath (Join-Path $root $path)) { 'is git-ignored' } else { 'is not on disk' }
        $failures.Add("$where declared document $path $why")
    }
}

function Test-Ignored([string]$path) {
    foreach ($ig in $ignores) {
        if ($ig.EndsWith('/')) { if ($path.StartsWith($ig, [StringComparison]::OrdinalIgnoreCase)) { return $true } }
        elseif ($path -ieq $ig) { return $true }
    }
    return $false
}

foreach ($f in ($files | Sort-Object)) {
    if ($f -notmatch '\.(md|html?)$' -and $f -notmatch '^winget/[^/]+\.ya?ml$') { continue }
    if ($docs.Contains($f) -or (Test-Ignored $f)) { continue }
    $failures.Add("$f is not declared in $registryRel (add a record, or an ignore record with its reason)")
}
$registryFailures = $failures.Count

# ---- Parsing helpers ------------------------------------------------------------
# Markdown with fenced blocks blanked (line numbers kept) and inline code spans removed.
function Get-ProseLines([string]$text) {
    $out = [System.Collections.Generic.List[string]]::new()
    $fence = $null
    foreach ($line in ($text -split "`r?`n")) {
        if ($fence) {
            if ($line -match "^\s{0,3}$([regex]::Escape($fence))[``~]*\s*$") { $fence = $null }
            $out.Add('')
            continue
        }
        if ($line -match '^\s{0,3}(```+|~~~+)') { $fence = $Matches[1]; $out.Add(''); continue }
        $out.Add(($line -replace '(`+)(?:(?!\1).)+?\1', ''))
    }
    return , $out
}

function Get-Slug([string]$heading) {
    $h = $heading -replace '!?\[([^\]]*)\]\([^)]*\)', '$1' -replace '<[^>]+>', '' -replace '`', ''
    $h = $h.Trim().ToLowerInvariant()
    $h = [regex]::Replace($h, '[^\p{L}\p{Nd}\p{Mn} _-]', '')
    return ($h -replace ' ', '-')
}

$anchorCache = @{}
function Get-Anchors([string]$rel) {
    if ($anchorCache.ContainsKey($rel)) { return $anchorCache[$rel] }
    $set = [System.Collections.Generic.HashSet[string]]::new([StringComparer]::Ordinal)
    $text = Get-Content -LiteralPath (Join-Path $root $rel) -Raw
    if ($null -eq $text) { $text = '' }
    foreach ($m in [regex]::Matches($text, '\s(?:id|name)\s*=\s*["'']([^"'']+)["'']')) { [void]$set.Add($m.Groups[1].Value) }
    if ($rel -match '\.md$') {
        $seen = @{}
        $fence = $null
        foreach ($line in ($text -split "`r?`n")) {
            if ($fence) { if ($line -match "^\s{0,3}$([regex]::Escape($fence))") { $fence = $null }; continue }
            if ($line -match '^\s{0,3}(```+|~~~+)') { $fence = $Matches[1]; continue }
            if ($line -notmatch '^\s{0,3}#{1,6}\s+(.+?)\s*#*\s*$') { continue }
            $slug = Get-Slug $Matches[1]
            $k = if ($seen.ContainsKey($slug)) { "$slug-$($seen[$slug])" } else { $slug }
            $seen[$slug] = 1 + [int]$seen[$slug]
            [void]$set.Add($k)
        }
    }
    $anchorCache[$rel] = $set
    return $set
}

# Links as @{ Line; Target; Image } - Markdown inline and reference links, and HTML href/src.
function Get-Links([string]$rel, [System.Collections.Generic.List[string]]$lines, [bool]$isHtml) {
    $found = [System.Collections.Generic.List[object]]::new()
    $inScript = $false
    for ($i = 0; $i -lt $lines.Count; $i++) {
        $line = $lines[$i]
        if ($isHtml) {
            # Script bodies build URLs at runtime; what they concatenate is not a file here.
            if ($inScript) { if ($line -match '</script') { $inScript = $false }; continue }
            if ($line -match '<script(?![^>]*\ssrc=)[^>]*>(?!.*</script)') { $inScript = $true }
        } else {
            foreach ($m in [regex]::Matches($line, '(!?)\[(?:[^\[\]]|\[[^\]]*\])*\]\(\s*<?([^)\s>]+)>?(?:\s+"[^"]*")?\s*\)')) {
                $found.Add(@{ Line = $i + 1; Target = $m.Groups[2].Value; Image = [bool]$m.Groups[1].Value })
            }
            if ($line -match '^\s{0,3}\[[^\]]+\]:\s*<?(\S+?)>?(\s|$)') { $found.Add(@{ Line = $i + 1; Target = $Matches[1]; Image = $false }) }
        }
        foreach ($m in [regex]::Matches($line, '<(\w+)[^>]*?\s(href|src)=["'']([^"'']*)["'']')) {
            $found.Add(@{ Line = $i + 1; Target = $m.Groups[3].Value; Image = ($m.Groups[1].Value -ieq 'img') })
        }
    }
    return , $found
}

# ---- Links and anchors (rule 3) and asset bookmarks (rule 6) ------------------------
$linkCount = 0
foreach ($path in $docs.Keys) {
    $d = $docs[$path]
    if ($d.role -ne 'source' -or $path -notmatch '\.(md|html?)$' -or -not $files.Contains($path)) { continue }
    $isHtml = $path -match '\.html?$'
    $raw = Get-Content -LiteralPath (Join-Path $root $path) -Raw
    if ($null -eq $raw) { $raw = '' }
    $lines = if ($isHtml) { [System.Collections.Generic.List[string]]::new([string[]]($raw -split "`r?`n")) } else { Get-ProseLines $raw }
    $base = [IO.Path]::GetDirectoryName($path) -replace '\\', '/'
    foreach ($l in (Get-Links $path $lines $isHtml)) {
        $t = $l.Target.Trim()
        $kind = if ($l.Image) { 'image' } else { 'link' }
        if (-not $t -or $t -match '^[a-zA-Z][a-zA-Z0-9+.-]*:' -or $t.StartsWith('//') -or $t.StartsWith('/') -or $t -match '\{\{|\$\{') { continue }
        $linkCount++
        $hash = $t.IndexOf('#')
        $anchor = if ($hash -ge 0) { $t.Substring($hash + 1) } else { '' }
        $p = if ($hash -ge 0) { $t.Substring(0, $hash) } else { $t }
        $q = $p.IndexOf('?'); if ($q -ge 0) { $p = $p.Substring(0, $q) }
        $p = [Uri]::UnescapeDataString($p)
        $where = "${path}:$($l.Line)"
        if ($p) {
            $combined = if ($base) { "$base/$p" } else { $p }
            $full = [IO.Path]::GetFullPath((Join-Path $root $combined))
            $target = [IO.Path]::GetRelativePath($root, $full) -replace '\\', '/'
            if ($target.StartsWith('..')) { $failures.Add("$where $kind '$t' leaves the repository"); continue }
            $target = $target.TrimEnd('/')
            if ($files.Contains($target)) {
                # found
            } elseif ($dirs.Contains($target) -and -not $l.Image) {
                if ($anchor) { $target = "$target/index.html"; if (-not $files.Contains($target)) { $failures.Add("$where $kind '$t' has an anchor into a folder with no index.html"); continue } }
            } else {
                $why = if (Test-Path -LiteralPath $full) { 'is git-ignored, so a clone does not have it' } else { 'does not exist' }
                $failures.Add("$where $kind '$t' -> $target $why")
                continue
            }
        } else {
            $target = $path
        }
        if ($anchor -and $target -match '\.(md|html?)$') {
            $a = [Uri]::UnescapeDataString($anchor)
            if (-not (Get-Anchors $target).Contains($a)) { $failures.Add("$where $kind '$t' -> no heading or id '#$a' in $target") }
        }
    }
}
$linkFailures = $failures.Count - $registryFailures

# ---- House style and offline safety, internal corpus (rules 5-7) -----------------------
$styleRules = @(
    @{ Name = 'ellipsis (write ..)';             Pattern = '\.\.\.|…' },
    @{ Name = 'en or em dash (write -)';          Pattern = '[–—]' },
    @{ Name = 'http:// link (use https://)';      Pattern = '(?i)http://' },
    @{ Name = 'remote embed';                     Pattern = '(?i)<(script|iframe|link|object|embed)\b' },
    @{ Name = 'remote image (vendor it locally)'; Pattern = '(?i)!\[[^\]]*\]\(\s*<?(https?:)?//|<img\b[^>]*\ssrc=["''](https?:)?//' }
)
$styleScanned = 0
foreach ($path in $docs.Keys) {
    $d = $docs[$path]
    if ($d.corpus -ne 'internal' -or $d.role -ne 'source' -or -not $files.Contains($path)) { continue }
    $styleScanned++
    $raw = Get-Content -LiteralPath (Join-Path $root $path) -Raw
    if ($null -eq $raw) { continue }
    $lines = Get-ProseLines $raw
    for ($i = 0; $i -lt $lines.Count; $i++) {
        foreach ($rule in $styleRules) {
            if ($lines[$i] -match $rule.Pattern) { $failures.Add("${path}:$($i + 1) $($rule.Name): '$($Matches[0])'") }
        }
    }
}
$styleFailures = $failures.Count - $registryFailures - $linkFailures

# ---- Generated documents: docs/CLI.md renders from the command manifest (SP-0220) ------
# packaging/write-cli-docs.ps1 -Check holds the manifest to the copy filedo.exe embeds, docs/CLI.md to what the
# manifest renders to, and the commands guide pages (en, de, fr) to naming every command; its verdict is this check's.
$generatedOut = & "$PSScriptRoot\write-cli-docs.ps1" -Check *>&1 | Out-String
$generatedCode = $LASTEXITCODE
if ($generatedCode -eq 2) { Stop-Unverified "write-cli-docs.ps1 could not verify: $((($generatedOut.Trim() -split "`r?`n")[-1]))" }
$beforeGenerated = $failures.Count
foreach ($gl in ($generatedOut -split "`r?`n")) {
    if ($gl -match '^\s*FAIL\s+(.+)$') { $failures.Add($Matches[1]) }
}
if ($generatedCode -ne 0 -and $failures.Count -eq $beforeGenerated) { $failures.Add("write-cli-docs.ps1 -Check exited $generatedCode with no FAIL line") }
# The install guide's component and channel table renders from the packaging files (SP-0166 F18); a change of
# packaging/wix/FileDO.wxs, msix/AppxManifest.xml or the winget installer manifest fails here until it is re-rendered.
$matrixOut = & "$PSScriptRoot\write-install-matrix.ps1" -Check *>&1 | Out-String
$matrixCode = $LASTEXITCODE
if ($matrixCode -eq 2) { Stop-Unverified "write-install-matrix.ps1 could not verify: $((($matrixOut.Trim() -split "`r?`n")[-1]))" }
$beforeMatrix = $failures.Count
foreach ($gl in ($matrixOut -split "`r?`n")) {
    if ($gl -match '^\s*FAIL\s+(.+)$') { $failures.Add($Matches[1]) }
}
if ($matrixCode -ne 0 -and $failures.Count -eq $beforeMatrix) { $failures.Add("write-install-matrix.ps1 -Check exited $matrixCode with no FAIL line") }
# The command reference pages (en, de, fr) render the READMEs' Command Reference, read check and Advanced Features
# (SP-0166 F12); an edit of one of those README parts fails here until the pages are re-rendered.
$referenceOut = & "$PSScriptRoot\write-cli-reference.ps1" -Check *>&1 | Out-String
$referenceCode = $LASTEXITCODE
if ($referenceCode -eq 2) { Stop-Unverified "write-cli-reference.ps1 could not verify: $((($referenceOut.Trim() -split "`r?`n")[-1]))" }
$beforeReference = $failures.Count
foreach ($gl in ($referenceOut -split "`r?`n")) {
    if ($gl -match '^\s*FAIL\s+(.+)$') { $failures.Add($Matches[1]) }
}
if ($referenceCode -ne 0 -and $failures.Count -eq $beforeReference) { $failures.Add("write-cli-reference.ps1 -Check exited $referenceCode with no FAIL line") }
# The product's pictures render from one drawing (PRODUCT-IDENTITY, FILEDO-0236): assets/make-product-icons.ps1 -Check
# redraws every master form, rendition and the identity manifest into a scratch folder and fails on a file that
# differs from what the drawing gives, or on a retired second drawing put back.
$identityOut = & (Join-Path $root 'assets\make-product-icons.ps1') -Check *>&1 | Out-String
$identityCode = $LASTEXITCODE
if ($identityCode -eq 2) { Stop-Unverified "make-product-icons.ps1 could not verify: $((($identityOut.Trim() -split "`r?`n")[-1]))" }
$beforeIdentity = $failures.Count
foreach ($gl in ($identityOut -split "`r?`n")) {
    if ($gl -match '^\s*FAIL\s+(.+)$') { $failures.Add($Matches[1]) }
}
if ($identityCode -ne 0 -and $failures.Count -eq $beforeIdentity) { $failures.Add("make-product-icons.ps1 -Check exited $identityCode with no FAIL line") }
$generatedFailures = $failures.Count - $beforeGenerated

Write-Host "  registry  $($docs.Count) documents, $($ignores.Count) ignore records - $registryFailures problems"
Write-Host "  links     $linkCount relative links and anchors - $linkFailures problems"
Write-Host "  style     $styleScanned internal documents - $styleFailures problems"
Write-Host "  generated docs/CLI.md and the commands guide against the manifest, the install matrix against the packaging files, the command reference against the READMEs, the product's pictures against their drawing - $generatedFailures problems"
if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "internal-docs: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "internal-docs: PASS ($($docs.Count) documents, $linkCount links, $styleScanned internal documents styled)" -ForegroundColor Green
exit 0
