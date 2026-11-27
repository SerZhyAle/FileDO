#Requires -Version 7.0
<#
.SYNOPSIS
  Bring FileDO's manifest in the own Scoop bucket (SerZhyAle/scoop-bucket, bucket/filedo.json) to a published
  release.

.DESCRIPTION
  release.ps1 calls it in step 7b, once the GitHub Release carries all its assets; by hand it is the recovery of
  that step:

      pwsh -NoProfile -File packaging\update-scoop-bucket.ps1 -Version 2611171800
      pwsh -NoProfile -File packaging\update-scoop-bucket.ps1 -Version 2611171800 -DryRun

  1. The SHA256 of the release's x64 zip is read twice - from the .sha256 file published beside it and from
     GitHub's own digest of the asset - and the two must agree.
  2. The bucket manifest is read through the GitHub contents API. A manifest that already carries the version
     is left alone; one at a newer version is never rolled back.
  3. Exactly three values are rewritten in place - "version", the architecture.64bit "url" and "hash" - so the
     order of the properties, the autoupdate block and the line endings stay as they are. The result must parse,
     read back the three values, and differ from the old text in exactly those three lines.
  4. It is written as one commit "filedo: Update to version <stamp>" (the message Scoop's own autoupdate uses)
     and read back.

  The bucket is the template's (ScoopInstaller/BucketTemplate). Its Excavator workflow is not what keeps it
  current: GitHub never registered that schedule (a template copy is not a push of the workflow file), and a
  schedule in a public repository is switched off after 60 days without activity. This script, run by every
  release, is. Needs the owner's gh login (repo scope) and the network, so no local gate runs it.

  Exit codes (CHECK-VERDICT): 0 = the bucket carries the version (written now, or already); 1 = refused or
  failed (hashes disagree, rollback, an unexpected manifest shape, the write or the read-back failed);
  2 = could not verify (gh missing, the release, its asset or the manifest could not be read).

.PARAMETER Version
  The release stamp: ten digits, the tag without the v.

.PARAMETER DryRun
  Read, rewrite and check the manifest and print the three changed lines; write nothing.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Version,
    [switch]$DryRun
)
$ErrorActionPreference = 'Stop'
# An error nobody expected still ends in a verdict line and exit 2, never in a bare exception.
trap {
    Write-Host "update-scoop-bucket: COULD NOT VERIFY (unexpected error at line $($_.InvocationInfo.ScriptLineNumber): $($_.Exception.Message))" -ForegroundColor Yellow
    exit 2
}

$productRepo = 'SerZhyAle/FileDO'
$bucketRepo = 'SerZhyAle/scoop-bucket'
$manifestPath = 'bucket/filedo.json'

function Stop-Run([int]$code, [string]$msg) {
    if ($code -eq 2) { Write-Host "update-scoop-bucket: COULD NOT VERIFY ($msg)" -ForegroundColor Yellow }
    else { Write-Host "update-scoop-bucket: FAIL ($msg)" -ForegroundColor Red }
    exit $code
}

# Reads a manifest through the contents API: its text, the blob sha the write needs, and the branch.
function Read-BucketManifest {
    $raw = & gh api "repos/$bucketRepo/contents/$manifestPath" 2>&1
    if ($LASTEXITCODE -ne 0) { Stop-Run 2 "gh api could not read $bucketRepo/$manifestPath (exit $LASTEXITCODE): $($raw | Select-Object -Last 1)" }
    $obj = ($raw -join "`n") | ConvertFrom-Json
    $b64 = ([string]$obj.content) -replace '\s', ''
    $text = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64))
    return @{ Text = $text; Sha = [string]$obj.sha }
}

function Get-ManifestFacts([string]$text) {
    $j = $text | ConvertFrom-Json
    $arch = $j.architecture.'64bit'
    return @{ Version = [string]$j.version; Url = [string]$arch.url; Hash = [string]$arch.hash }
}

if ($Version -notmatch '^\d{10}$') { Stop-Run 1 "-Version '$Version' is not a ten-digit release stamp" }
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) { Stop-Run 2 'gh (GitHub CLI) is not on PATH' }

$tag = "v$Version"
$zipName = "FileDO-$Version-windows-x64.zip"
$zipUrl = "https://github.com/$productRepo/releases/download/$tag/$zipName"

# 1. The zip's SHA256, from the published .sha256 file and from GitHub's digest of the asset.
$assetsRaw = & gh release view $tag --repo $productRepo --json assets 2>&1
if ($LASTEXITCODE -ne 0) { Stop-Run 2 "gh release view $tag failed (exit $LASTEXITCODE): $($assetsRaw | Select-Object -Last 1)" }
$assets = @((($assetsRaw -join "`n") | ConvertFrom-Json).assets)
$zipAsset = @($assets | Where-Object { $_.name -eq $zipName })
$shaAsset = @($assets | Where-Object { $_.name -eq "$zipName.sha256" })
if ($zipAsset.Count -ne 1 -or $shaAsset.Count -ne 1) { Stop-Run 2 "release $tag does not carry $zipName and $zipName.sha256" }

$tmp = Join-Path ([IO.Path]::GetTempPath()) "filedo-scoop-$Version"
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
$dl = & gh release download $tag --repo $productRepo --pattern "$zipName.sha256" --dir $tmp --clobber 2>&1
if ($LASTEXITCODE -ne 0) { Stop-Run 2 "could not download $zipName.sha256 (exit $LASTEXITCODE): $($dl | Select-Object -Last 1)" }
$shaLine = [IO.File]::ReadAllText((Join-Path $tmp "$zipName.sha256")).Trim()
if ($shaLine -notmatch '^([0-9a-fA-F]{64})\s+\*?(\S+)$') { Stop-Run 1 "$zipName.sha256 does not read '<hash>  <file name>': '$shaLine'" }
$fileHash = $Matches[1].ToLowerInvariant()
if ($Matches[2] -ne $zipName) { Stop-Run 1 "$zipName.sha256 names '$($Matches[2])', not $zipName" }
$digest = [string]$zipAsset[0].digest
if ($digest -match '^sha256:([0-9a-f]{64})$') {
    if ($Matches[1] -ne $fileHash) { Stop-Run 1 "the published .sha256 ($fileHash) and GitHub's digest of the asset ($($Matches[1])) disagree" }
    Write-Host "  SHA256 $fileHash (the .sha256 file and GitHub's asset digest agree)"
} else {
    Write-Host "  SHA256 $fileHash (from the .sha256 file; GitHub reports no digest for the asset)" -ForegroundColor Yellow
}

# 2. The bucket manifest as it stands.
$current = Read-BucketManifest
try { $before = Get-ManifestFacts $current.Text } catch { Stop-Run 1 "$bucketRepo/$manifestPath is not a readable manifest: $($_.Exception.Message)" }
if ($before.Version -notmatch '^\d{10}$') { Stop-Run 1 "$bucketRepo/$manifestPath carries version '$($before.Version)', not a ten-digit stamp" }
if ([long]$before.Version -gt [long]$Version) { Stop-Run 1 "$bucketRepo/$manifestPath already carries $($before.Version), newer than $Version - a release never rolls the bucket back" }
if ($before.Version -eq $Version) {
    if ($before.Url -ne $zipUrl -or $before.Hash.ToLowerInvariant() -ne $fileHash) {
        Stop-Run 1 "$bucketRepo/$manifestPath carries $Version with url '$($before.Url)' and hash '$($before.Hash)', not the published ones"
    }
    Write-Host "update-scoop-bucket: PASS ($bucketRepo/$manifestPath already carries $Version)" -ForegroundColor Green
    exit 0
}

# 3. Three values rewritten in place, each pattern matching exactly once.
$text = $current.Text
$edits = @(
    @{ Name = 'version'; Re = '("version"\s*:\s*")\d{10}(")'; New = "`${1}$Version`${2}" },
    @{ Name = 'architecture.64bit.url'; Re = '("url"\s*:\s*")https://github\.com/SerZhyAle/FileDO/releases/download/v\d{10}/FileDO-\d{10}-windows-x64\.zip(")'; New = "`${1}$zipUrl`${2}" },
    @{ Name = 'architecture.64bit.hash'; Re = '("hash"\s*:\s*")[0-9a-fA-F]{64}(")'; New = "`${1}$fileHash`${2}" }
)
foreach ($e in $edits) {
    $n = [regex]::Matches($text, $e.Re).Count
    if ($n -ne 1) { Stop-Run 1 "the $($e.Name) pattern matches $n times in $bucketRepo/$manifestPath, not once - the manifest shape changed; update it by hand" }
    $text = [regex]::Replace($text, $e.Re, $e.New)
}
try { $after = Get-ManifestFacts $text } catch { Stop-Run 1 "the rewritten manifest does not parse: $($_.Exception.Message)" }
if ($after.Version -ne $Version -or $after.Url -ne $zipUrl -or $after.Hash -ne $fileHash) {
    Stop-Run 1 "the rewritten manifest reads back version '$($after.Version)', url '$($after.Url)', hash '$($after.Hash)'"
}
$oldLines = $current.Text -split "`n"
$newLines = $text -split "`n"
if ($oldLines.Count -ne $newLines.Count) { Stop-Run 1 'the rewrite changed the number of lines' }
$changed = @(for ($i = 0; $i -lt $oldLines.Count; $i++) { if ($oldLines[$i] -ne $newLines[$i]) { $i } })
if ($changed.Count -ne 3) { Stop-Run 1 "the rewrite changed $($changed.Count) lines, not 3" }
foreach ($i in $changed) {
    Write-Host "  - $($oldLines[$i].Trim())"
    Write-Host "  + $($newLines[$i].Trim())"
}
if ($DryRun) {
    Write-Host "update-scoop-bucket: DRY RUN ($bucketRepo/$manifestPath would go from $($before.Version) to $Version; nothing written)" -ForegroundColor Yellow
    exit 0
}

# 4. One commit through the contents API, then read back.
$body = @{
    message = "filedo: Update to version $Version"
    content = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($text))
    sha     = $current.Sha
} | ConvertTo-Json -Compress
$bodyFile = Join-Path $tmp 'put-body.json'
[IO.File]::WriteAllText($bodyFile, $body, [Text.UTF8Encoding]::new($false))
$put = & gh api -X PUT "repos/$bucketRepo/contents/$manifestPath" --input $bodyFile 2>&1
$putCode = $LASTEXITCODE
Remove-Item -LiteralPath $bodyFile -ErrorAction SilentlyContinue
if ($putCode -ne 0) { Stop-Run 1 "the write to $bucketRepo/$manifestPath failed (exit $putCode): $($put | Select-Object -Last 1)" }
$commitUrl = [string]((($put -join "`n") | ConvertFrom-Json).commit.html_url)

$check = Get-ManifestFacts (Read-BucketManifest).Text
if ($check.Version -ne $Version -or $check.Hash.ToLowerInvariant() -ne $fileHash -or $check.Url -ne $zipUrl) {
    Stop-Run 1 "the read-back of $bucketRepo/$manifestPath shows version '$($check.Version)', hash '$($check.Hash)' - not what was written"
}
Write-Host "  commit $commitUrl"
Write-Host "update-scoop-bucket: PASS ($bucketRepo/$manifestPath $($before.Version) -> $Version, read back)" -ForegroundColor Green
exit 0
