#Requires -Version 7.0
<#
.SYNOPSIS
  Writes one dated metrics checkpoint of the free promotion campaign: <OutDir>\<yyyy-MM-dd>.md.

.DESCRIPTION
  Every checkpoint is produced by this script, from public or owner-held counters only, with read-only
  calls (GET requests and GraphQL queries - nothing is written to GitHub or to any channel). A number it
  cannot read is written "unknown" with the reason, never estimated, and a channel page that answers 403 or
  fails is "unknown", never "absent". Only a page that answers 404 or 410 is written as not found.

  What it reads
    Repository      gh api repos/SerZhyAle/FileDO (stars, forks, subscribers, open issues, discussions,
                    license, description, homepage, topics) and the GraphQL field usesCustomOpenGraphImage
    Traffic         views, clones and referrers of the last fourteen days. GitHub shows them only to a
                    token with push access (the owner's) and forgets them after fourteen days, so a day
                    that is not in a checkpoint is lost; while outreach is active checkpoints are at
                    most fourteen days apart
    Releases        asset download_count of every release, the .sha256 files excluded, and the total
    winget          the versions in microsoft/winget-pkgs manifests/s/SerZhyAle/FileDO, and the state
                    (open, merged, closed) of the pull request(s) for the newest tag
    Channels        the HTTP status of the Microsoft Store page, Chocolatey, Scoop Main and Extras,
                    SourceForge and AlternativeTo. A 200 shows that a page exists, not that it is ours
    Site            the Pages config, the served status of the landing page, sitemap.xml and the
                    project robots.txt, and of the host-root robots.txt (crawlers read only that one)

  Counters no script can read (Store acquisitions and ratings, winget installs, Search Console and Bing
  data) are written as "unknown" with the owner-held source named.

  The delta against the previous file in -OutDir (the newest <date>*.md dated on or before -Date, other
  than the file being written) is printed and appended to the new file as its last section. Traffic
  counters are rolling fourteen-day windows: a difference between two windows is not a count of new events.
  Writing a second checkpoint for the same date replaces that day's file.

  Exit code: 0 = file written, 1 = defect (the file could not be written), 2 = could not read (gh missing
  or not authenticated, the GitHub API unreachable) - nothing is written then, never a fake zero.

.PARAMETER OutDir
  The folder that receives <yyyy-MM-dd>.md and holds the earlier checkpoints. Mandatory; the script names
  no default path.

.PARAMETER Date
  The checkpoint date, yyyy-MM-dd. Defaults to today. The file also records the real UTC time of the read.

.EXAMPLE
  pwsh -NoProfile -File .\packaging\write-promo-checkpoint.ps1 -OutDir <folder>
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)]
    [string]$OutDir,

    [ValidatePattern('^\d{4}-\d{2}-\d{2}$')]
    [string]$Date = (Get-Date -Format 'yyyy-MM-dd')
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$owner     = 'SerZhyAle'
$repoName  = 'FileDO'
$slug      = "$owner/$repoName"
$package   = 'SerZhyAle.FileDO'
$wingetDir = 'manifests/s/SerZhyAle/FileDO'
$siteRoot  = 'https://serzhyale.github.io'
$siteBase  = "$siteRoot/$repoName/"
$userAgent = "FileDO-promo-checkpoint/1 (+https://github.com/$slug)"

$inv = [cultureinfo]::InvariantCulture
$dateValue = [datetime]::MinValue
if (-not [datetime]::TryParseExact($Date, 'yyyy-MM-dd', $inv, [Globalization.DateTimeStyles]::None, [ref]$dateValue)) {
    Write-Host "write-promo-checkpoint: FAIL (-Date '$Date' is not a calendar date)" -ForegroundColor Red
    exit 1
}

# --- helpers ----------------------------------------------------------------------------------

function Get-Prop {
    # Walks a dotted path through parsed JSON; a missing step is $null, never an error under strict mode.
    param($Obj, [string]$Path)
    foreach ($seg in $Path.Split('.')) {
        if ($null -eq $Obj) { return $null }
        $p = $Obj.PSObject.Properties[$seg]
        if ($null -eq $p) { return $null }
        $Obj = $p.Value
    }
    return $Obj
}

function Test-Prop {
    param($Obj, [string]$Name)
    return ($null -ne $Obj) -and ($null -ne $Obj.PSObject.Properties[$Name])
}

function Invoke-Gh {
    # Runs gh and keeps stdout and stderr apart; the HTTP status is read from gh's error line.
    param([string[]]$GhArgs)
    $ErrorActionPreference = 'Continue'
    $all = @(& gh @GhArgs 2>&1)
    $code = $LASTEXITCODE
    $err = @($all | Where-Object { $_ -is [System.Management.Automation.ErrorRecord] } | ForEach-Object { "$_" })
    $out = @($all | Where-Object { $_ -isnot [System.Management.Automation.ErrorRecord] } | ForEach-Object { "$_" })
    $http = $null
    $m = [regex]::Match(($err -join "`n"), '\(HTTP (\d{3})\)')
    if ($m.Success) { $http = [int]$m.Groups[1].Value }
    return [pscustomobject]@{ Ok = ($code -eq 0); Code = $code; Out = ($out -join "`n"); Err = ($err -join "`n").Trim(); Http = $http }
}

function Get-FailReason {
    param($Result)
    $msg = $null
    if ($Result.Out) {
        try { $msg = Get-Prop ($Result.Out | ConvertFrom-Json -ErrorAction Stop) 'message' } catch { $msg = $null }
    }
    if (-not $msg -and $Result.Err) {
        $msg = ($Result.Err -split "`n")[0] -replace '^gh:\s*', '' -replace '\s*\(HTTP \d{3}\)\s*$', ''
    }
    if (-not $msg) { $msg = "gh exit $($Result.Code)" }
    if ($Result.Http) { return "HTTP $($Result.Http) - $msg" }
    return [string]$msg
}

function Get-GhJson {
    param([string[]]$GhArgs)
    $r = Invoke-Gh $GhArgs
    if (-not $r.Ok) { return [pscustomobject]@{ Ok = $false; Data = $null; Http = $r.Http; Reason = (Get-FailReason $r) } }
    try { $d = $r.Out | ConvertFrom-Json -NoEnumerate -ErrorAction Stop }
    catch { return [pscustomobject]@{ Ok = $false; Data = $null; Http = $null; Reason = "unreadable answer: $($_.Exception.Message)" } }
    return [pscustomobject]@{ Ok = $true; Data = $d; Http = $null; Reason = $null }
}

function Get-HttpState {
    # One GET with a plain User-Agent and no evasion. 429 and 5xx are tried once more; the last answer stands.
    param([string]$Url)
    $code = $null; $body = $null; $err = $null
    for ($try = 1; $try -le 2; $try++) {
        try {
            $r = Invoke-WebRequest -Uri $Url -Method Get -Headers @{ 'User-Agent' = $userAgent } `
                -MaximumRedirection 5 -SkipHttpErrorCheck -TimeoutSec 30
            $code = [int]$r.StatusCode
            $err = $null
            $body = if ($r.Content -is [string]) { $r.Content } else { $null }
            if ($code -ne 429 -and $code -lt 500) { break }
        } catch {
            $code = $null
            $err = "request failed: $($_.Exception.Message)"
        }
        if ($try -lt 2) { Start-Sleep -Seconds 2 }
    }
    return [pscustomobject]@{ Code = $code; Body = $body; Error = $err; Url = $Url }
}

function Get-PageText {
    # 2xx -> $Present, 404/410 -> $Absent (both formatted with the code), anything else is unknown.
    param($State, [string]$Present, [string]$Absent)
    if ($null -eq $State.Code) { return "unknown ($($State.Error))" }
    if ($State.Code -ge 200 -and $State.Code -lt 300) { return ($Present -f $State.Code) }
    if ($State.Code -in 404, 410) { return ($Absent -f $State.Code) }
    return "unknown (HTTP $($State.Code) to a scripted request)"
}

function ConvertTo-UtcDay {
    param($Value)
    if ($null -eq $Value) { return $null }
    if ($Value -is [datetime]) { return $Value.ToUniversalTime().ToString('yyyy-MM-dd', $inv) }
    $t = [datetimeoffset]::MinValue
    if ([datetimeoffset]::TryParse([string]$Value, $inv, [Globalization.DateTimeStyles]::AssumeUniversal, [ref]$t)) {
        return $t.UtcDateTime.ToString('yyyy-MM-dd', $inv)
    }
    return [string]$Value
}

function ConvertTo-Cell {
    param($Value)
    return (([string]$Value) -replace '\|', '\|') -replace '\r?\n', ' '
}

function Format-Value {
    param($Value)
    if ($null -eq $Value) { return 'unknown (not in the GitHub answer)' }
    if ($Value -is [bool]) { return $Value.ToString().ToLowerInvariant() }
    return [string]$Value
}

function ConvertTo-NullableInt {
    param($Text)
    $n = 0
    if ([int]::TryParse([string]$Text, [ref]$n)) { return $n }
    return $null
}

$unknowns = [System.Collections.Generic.List[string]]::new()
function Add-Unknown {
    param([string]$What, [string]$Reason)
    $unknowns.Add("${What}: $Reason")
}

# --- previous checkpoints: parse a file written by this script, or the hand-written baseline -------

function Read-Checkpoint {
    param([string]$Path)
    $s = @{
        stars = $null; forks = $null; watchers = $null; openIssues = $null
        description = $null; homepage = $null; topics = $null; ogImage = $null
        views = $null; viewsUnique = $null; clones = $null; clonesUnique = $null
        total = $null; releases = @{}; versions = $null; prs = @{}
        channels = @{}; sitemapLocs = $null; pages = $null
    }
    foreach ($line in [IO.File]::ReadAllLines($Path)) {
        if ($line -match '^## Change since') { break }
        if ($line -notmatch '^\|') { continue }
        $cells = @([regex]::Split($line.Trim().Trim('|'), '(?<!\\)\|') | ForEach-Object {
            ($_ -replace '\*\*', '' -replace '`', '' -replace '\\\|', '|').Trim()
        })
        $label = [string]$cells[0]
        $v1 = if ($cells.Count -gt 1) { [string]$cells[1] } else { '' }
        $v2 = if ($cells.Count -gt 2) { [string]$cells[2] } else { '' }
        switch -Regex ($label) {
            '^stargazers_count$'          { $s.stars = ConvertTo-NullableInt $v1; continue }
            '^forks_count$'               { $s.forks = ConvertTo-NullableInt $v1; continue }
            '^subscribers_count$'         { $s.watchers = ConvertTo-NullableInt $v1; continue }
            '^open_issues_count$'         { $s.openIssues = ConvertTo-NullableInt $v1; continue }
            '^description$'               { if ($v1 -notmatch '^unknown') { $s.description = $v1.Trim('"') }; continue }
            '^homepage$'                  { if ($v1 -notmatch '^unknown') { $s.homepage = ($v1 -split '\s+')[0] }; continue }
            '^topics'                     {
                if ($v1 -notmatch '^unknown') {
                    $s.topics = (@($v1 -split ',\s*' | Where-Object { $_ -and $_ -ne '(none)' } | Sort-Object) -join ', ')
                }
                continue
            }
            '^usesCustomOpenGraphImage$'  { if ($v1 -in 'true', 'false') { $s.ogImage = $v1 }; continue }
            '^views$'                     { $s.views = ConvertTo-NullableInt $v1; $s.viewsUnique = ConvertTo-NullableInt $v2; continue }
            '^clones$'                    { $s.clones = ConvertTo-NullableInt $v1; $s.clonesUnique = ConvertTo-NullableInt $v2; continue }
            '^Total'                      { $s.total = ConvertTo-NullableInt $v2; continue }
            '^v\d{10}$'                   {
                if ($v1 -match '^(\d{4}-\d{2}-\d{2}|draft)$') {
                    $n = ConvertTo-NullableInt $v2
                    if ($null -ne $n) { $s.releases[$label] = $n }
                }
                continue
            }
            '^Pages config$'              { $s.pages = $v1; continue }
            '^winget .*SerZhyAle\.FileDO' {
                if ($v1 -match 'catalog:\s*([^;]*)') {
                    $s.versions = @([regex]::Matches($Matches[1], '\b\d{10}\b') | ForEach-Object { $_.Value } | Sort-Object)
                }
                foreach ($m in [regex]::Matches($v1, 'PR #(\d+) for (\d{10}) (\w+)')) {
                    $s.prs[$m.Groups[1].Value] = @{ version = $m.Groups[2].Value; state = $m.Groups[3].Value }
                }
                continue
            }
            '^Microsoft Store'            { $s.channels['store'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^Chocolatey'                 { $s.channels['choco'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^Scoop Main / Extras'        {
                $c = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value
                $s.channels['scoopMain'] = $c; $s.channels['scoopExtras'] = $c; continue
            }
            '^Scoop Main'                 { $s.channels['scoopMain'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^Scoop Extras'               { $s.channels['scoopExtras'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^SourceForge'                { $s.channels['sourceforge'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^AlternativeTo'              { $s.channels['alternativeto'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^Landing$'                   { $s.channels['landing'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            '^Host-root'                  { $s.channels['robotsHost'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
            'sitemap\.xml'                {
                $s.channels['sitemap'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value
                if ($v1 -match '(\d+)\s+<loc>') { $s.sitemapLocs = [int]$Matches[1] }
                continue
            }
            'robots\.txt'                 { $s.channels['robotsProject'] = ([regex]::Match($v1, '(?<![\d.])[1-5]\d\d(?![\d.])')).Value; continue }
        }
    }
    return $s
}

# --- preconditions ----------------------------------------------------------------------------

if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    Write-Host 'write-promo-checkpoint: COULD NOT READ (the gh CLI is not installed or not on PATH)' -ForegroundColor Yellow
    exit 2
}

$repoR = Get-GhJson @('api', "repos/$slug")
if (-not $repoR.Ok) {
    $hint = if ($repoR.Http -eq 401) { ' - run gh auth login, or give a valid GH_TOKEN' } else { '' }
    Write-Host "write-promo-checkpoint: COULD NOT READ (repos/${slug}: $($repoR.Reason)$hint)" -ForegroundColor Yellow
    exit 2
}

try {

$readAt = [DateTime]::UtcNow.ToString('yyyy-MM-dd HH:mm', $inv) + ' UTC'
$repo = $repoR.Data

# --- repository -------------------------------------------------------------------------------

$topics = if (Test-Prop $repo 'topics') { @(Get-Prop $repo 'topics') } else { $null }
$description = Get-Prop $repo 'description'
$homepage = Get-Prop $repo 'homepage'
$licenseId = Get-Prop $repo 'license.spdx_id'
if (-not $licenseId -and (Test-Prop $repo 'license')) { $licenseId = if ($null -eq (Get-Prop $repo 'license')) { 'none' } else { Get-Prop $repo 'license.name' } }
$repoUrl = Get-Prop $repo 'html_url'

$ogR = Get-GhJson @('api', 'graphql', '-f', 'query=query($o:String!,$n:String!){repository(owner:$o,name:$n){usesCustomOpenGraphImage}}', '-F', "o=$owner", '-F', "n=$repoName")
$ogImage = if ($ogR.Ok) { Get-Prop $ogR.Data 'data.repository.usesCustomOpenGraphImage' } else { $null }
$ogReason = if ($ogR.Ok -and $null -eq $ogImage) { 'not in the GraphQL answer' } elseif (-not $ogR.Ok) { $ogR.Reason } else { $null }
if ($null -eq $ogImage) { Add-Unknown 'usesCustomOpenGraphImage' $ogReason }

foreach ($f in 'stargazers_count', 'forks_count', 'subscribers_count', 'open_issues_count') {
    if ($null -eq (Get-Prop $repo $f)) { Add-Unknown $f 'not in the GitHub answer' }
}

# --- traffic ----------------------------------------------------------------------------------

function Read-Traffic {
    param([string]$Kind)
    $r = Get-GhJson @('api', "repos/$slug/traffic/$Kind")
    if (-not $r.Ok) { return [pscustomobject]@{ Ok = $false; Count = $null; Uniques = $null; Data = $null; Reason = $r.Reason } }
    $c = ConvertTo-NullableInt (Get-Prop $r.Data 'count')
    $u = ConvertTo-NullableInt (Get-Prop $r.Data 'uniques')
    if ($null -eq $c -or $null -eq $u) { return [pscustomobject]@{ Ok = $false; Count = $null; Uniques = $null; Data = $null; Reason = 'count or uniques missing from the answer' } }
    return [pscustomobject]@{ Ok = $true; Count = $c; Uniques = $u; Data = $r.Data; Reason = $null }
}
$views = Read-Traffic 'views'
$clones = Read-Traffic 'clones'
if (-not $views.Ok) { Add-Unknown 'traffic views' $views.Reason }
if (-not $clones.Ok) { Add-Unknown 'traffic clones' $clones.Reason }

$refR = Get-GhJson @('api', "repos/$slug/traffic/popular/referrers")
$referrers = $null
if ($refR.Ok) {
    $referrers = @(@($refR.Data) | ForEach-Object {
        $n = [string](Get-Prop $_ 'referrer'); $c = Get-Prop $_ 'count'; $u = Get-Prop $_ 'uniques'
        "$n $c $(if ($c -eq 1) { 'view' } else { 'views' }) / $u unique"
    })
} else { Add-Unknown 'traffic referrers' $refR.Reason }

# --- releases ---------------------------------------------------------------------------------

$releases = [System.Collections.Generic.List[object]]::new()
$relReason = $null
for ($page = 1; $page -le 20; $page++) {
    $rr = Get-GhJson @('api', "repos/$slug/releases?per_page=100&page=$page")
    if (-not $rr.Ok) { $relReason = "releases page ${page}: $($rr.Reason)"; break }
    $chunk = @($rr.Data)
    foreach ($x in $chunk) { $releases.Add($x) }
    if ($chunk.Count -lt 100) { break }
}
$releaseRows = @()
$totalDownloads = $null
$newestVersion = $null
if ($relReason) {
    Add-Unknown 'release downloads' $relReason
} else {
    $sum = 0
    $rows = foreach ($x in $releases) {
        $assets = @(Get-Prop $x 'assets' | Where-Object { $_ -and (Get-Prop $_ 'name') -notmatch '\.sha256$' })
        $n = 0
        foreach ($a in $assets) { $n += [int](Get-Prop $a 'download_count') }
        $sum += $n
        $draft = [bool](Get-Prop $x 'draft')
        [pscustomobject]@{
            Tag = [string](Get-Prop $x 'tag_name'); Draft = $draft
            Published = if ($draft) { 'draft' } else { ConvertTo-UtcDay (Get-Prop $x 'published_at') }
            Downloads = $n
        }
    }
    $releaseRows = @($rows | Sort-Object -Property @{ Expression = { $_.Tag }; Descending = $true })
    $totalDownloads = $sum
    $newest = @($releaseRows | Where-Object { -not $_.Draft -and $_.Tag -match '^v\d{10}$' }) | Select-Object -First 1
    if ($newest) { $newestVersion = $newest.Tag.Substring(1) }
}

# --- winget -----------------------------------------------------------------------------------

$wingetVersions = $null
$wingetReason = $null
$wr = Get-GhJson @('api', "repos/microsoft/winget-pkgs/contents/$wingetDir")
if ($wr.Ok) {
    $wingetVersions = @(@($wr.Data) | Where-Object { (Get-Prop $_ 'type') -eq 'dir' } | ForEach-Object { [string](Get-Prop $_ 'name') } | Sort-Object -CaseSensitive)
} elseif ($wr.Http -eq 404) {
    $wingetVersions = @()
} else {
    $wingetReason = $wr.Reason
    Add-Unknown 'winget catalog versions' $wingetReason
}

$prs = $null
$prReason = $null
if ($newestVersion) {
    $sr = Get-GhJson @('api', '--method', 'GET', 'search/issues', '-f', "q=repo:microsoft/winget-pkgs is:pr in:title $package $newestVersion", '-f', 'per_page=30')
    if ($sr.Ok) {
        $prs = @(@(Get-Prop $sr.Data 'items') | Where-Object {
            $_ -and ([string](Get-Prop $_ 'title')) -match [regex]::Escape($newestVersion) -and ([string](Get-Prop $_ 'title')) -match [regex]::Escape($package)
        } | ForEach-Object {
            $state = if (Get-Prop $_ 'pull_request.merged_at') { 'merged' } elseif ((Get-Prop $_ 'state') -eq 'open') { 'open' } else { 'closed' }
            [pscustomobject]@{ Number = [int](Get-Prop $_ 'number'); State = $state }
        } | Sort-Object Number)
    } else {
        $prReason = $sr.Reason
        Add-Unknown "winget pull request for $newestVersion" $prReason
    }
} elseif (-not $relReason) {
    $prReason = 'no release tag of the form v + 10 digits was found'
    Add-Unknown 'winget pull request' $prReason
} else {
    $prReason = 'the newest tag is unknown (releases could not be read)'
    Add-Unknown 'winget pull request' $prReason
}

# --- channels and site ------------------------------------------------------------------------

$channelDefs = @(
    @{ Key = 'store';         Label = 'Microsoft Store 9PH1LPCMRG83'; Url = 'https://apps.microsoft.com/detail/9PH1LPCMRG83'
       Present = 'product page answers {0}'; Absent = 'product page answers {0} (no such product)' }
    @{ Key = 'choco';         Label = 'Chocolatey `filedo`'; Url = 'https://community.chocolatey.org/packages/filedo'
       Present = 'page answers {0}'; Absent = 'page answers {0} (not listed)' }
    @{ Key = 'scoopMain';     Label = 'Scoop Main `filedo.json`'; Url = 'https://raw.githubusercontent.com/ScoopInstaller/Main/master/bucket/filedo.json'
       Present = 'manifest answers {0}'; Absent = '{0} (no such manifest in the bucket)' }
    @{ Key = 'scoopExtras';   Label = 'Scoop Extras `filedo.json`'; Url = 'https://raw.githubusercontent.com/ScoopInstaller/Extras/master/bucket/filedo.json'
       Present = 'manifest answers {0}'; Absent = '{0} (no such manifest in the bucket)' }
    @{ Key = 'sourceforge';   Label = 'SourceForge `filedo`'; Url = 'https://sourceforge.net/projects/filedo/'
       Present = 'page answers {0}'; Absent = 'page answers {0} (not listed)' }
    @{ Key = 'alternativeto'; Label = 'AlternativeTo `filedo`'; Url = 'https://alternativeto.net/software/filedo/'
       Present = 'page answers {0}'; Absent = 'page answers {0} (not listed)' }
)
$channelRows = foreach ($c in $channelDefs) {
    $st = Get-HttpState $c.Url
    $text = Get-PageText $st $c.Present $c.Absent
    if ($text -like 'unknown*') { Add-Unknown $c.Label $text }
    [pscustomobject]@{ Key = $c.Key; Label = $c.Label; Text = $text; Source = '`GET ' + $c.Url + '`'; Code = $st.Code }
}

$pagesR = Get-GhJson @('api', "repos/$slug/pages")
$pagesText = $null
if ($pagesR.Ok) {
    $pagesText = '{0} build, branch `{1}`, path `{2}`, `{3}`' -f (Get-Prop $pagesR.Data 'build_type'), (Get-Prop $pagesR.Data 'source.branch'), (Get-Prop $pagesR.Data 'source.path'), (Get-Prop $pagesR.Data 'html_url')
} elseif ($pagesR.Http -eq 404) {
    $pagesText = 'no Pages site (HTTP 404)'
} else {
    $pagesText = "unknown ($($pagesR.Reason))"
    Add-Unknown 'Pages config' $pagesR.Reason
}

$landing = Get-HttpState $siteBase
$sitemap = Get-HttpState ($siteBase + 'sitemap.xml')
$robotsProject = Get-HttpState ($siteBase + 'robots.txt')
$robotsHost = Get-HttpState ($siteRoot + '/robots.txt')

$landingText = Get-PageText $landing 'answers {0}' 'answers {0} (not found)'
$sitemapText = Get-PageText $sitemap 'served {0}' 'answers {0} (not served)'
$sitemapLocs = $null
if ($sitemap.Code -ge 200 -and $sitemap.Code -lt 300) {
    if ($sitemap.Body) {
        $locs = @([regex]::Matches($sitemap.Body, '<loc>\s*([^<\s]+)\s*</loc>'))
        $mods = @([regex]::Matches($sitemap.Body, '<lastmod>\s*([^<\s]+)\s*</lastmod>') | ForEach-Object { $_.Groups[1].Value.Substring(0, [Math]::Min(10, $_.Groups[1].Value.Length)) } | Sort-Object)
        $sitemapLocs = $locs.Count
        $sitemapText += ", $($locs.Count) ``<loc>``"
        if ($mods.Count) { $sitemapText += "; ``lastmod`` $($mods[0])..$($mods[-1])" } else { $sitemapText += '; no `lastmod`' }
    } else {
        $sitemapText += ' (body not read)'
    }
}
$robotsProjectText = Get-PageText $robotsProject 'served {0} at `/FileDO/robots.txt` - a project path, which crawlers do not read' 'answers {0} (not served)'
$robotsHostText = Get-PageText $robotsHost 'answers {0}' 'answers {0} (no host-root robots.txt)'
if ($robotsHost.Code -ge 200 -and $robotsHost.Code -lt 300 -and $robotsHost.Body) {
    $hasLine = $robotsHost.Body -match '(?im)^\s*Sitemap:\s*https://serzhyale\.github\.io/FileDO/sitemap\.xml\s*$'
    $robotsHostText += if ($hasLine) { '; carries a `Sitemap:` line for the FileDO sitemap' } else { '; no `Sitemap:` line for the FileDO sitemap' }
}
foreach ($pair in @(@('landing', $landingText), @('sitemap.xml', $sitemapText), @('project robots.txt', $robotsProjectText), @('host-root robots.txt', $robotsHostText))) {
    if ($pair[1] -like 'unknown*') { Add-Unknown $pair[0] $pair[1] }
}

# --- the live snapshot, in the shape Read-Checkpoint returns ----------------------------------

$now = @{
    stars = ConvertTo-NullableInt (Get-Prop $repo 'stargazers_count'); forks = ConvertTo-NullableInt (Get-Prop $repo 'forks_count')
    watchers = ConvertTo-NullableInt (Get-Prop $repo 'subscribers_count'); openIssues = ConvertTo-NullableInt (Get-Prop $repo 'open_issues_count')
    description = if (Test-Prop $repo 'description') { [string]$description } else { $null }
    homepage = if (Test-Prop $repo 'homepage') { [string]$homepage } else { $null }
    topics = if ($null -ne $topics) { (@($topics | Sort-Object) -join ', ') } else { $null }
    ogImage = if ($null -ne $ogImage) { ([bool]$ogImage).ToString().ToLowerInvariant() } else { $null }
    views = $views.Count; viewsUnique = $views.Uniques; clones = $clones.Count; clonesUnique = $clones.Uniques
    total = $totalDownloads; releases = @{}; versions = $wingetVersions; prs = @{}
    channels = @{}; sitemapLocs = $sitemapLocs; pages = $pagesText
}
foreach ($x in $releaseRows) { $now.releases[$x.Tag] = $x.Downloads }
if ($prs) { foreach ($p in $prs) { $now.prs["$($p.Number)"] = @{ version = $newestVersion; state = $p.State } } }
foreach ($c in $channelRows) { if ($null -ne $c.Code) { $now.channels[$c.Key] = [string]$c.Code } }
foreach ($pair in @(@('landing', $landing), @('sitemap', $sitemap), @('robotsProject', $robotsProject), @('robotsHost', $robotsHost))) {
    if ($null -ne $pair[1].Code) { $now.channels[$pair[0]] = [string]$pair[1].Code }
}

# --- delta against the previous file ----------------------------------------------------------

$outFile = Join-Path $OutDir "$Date.md"
$previous = $null
if (Test-Path -LiteralPath $OutDir -PathType Container) {
    $previous = Get-ChildItem -LiteralPath $OutDir -Filter '*.md' -File |
        Where-Object { $_.Name -match '^(\d{4}-\d{2}-\d{2})' -and $Matches[1] -le $Date -and $_.Name -ne "$Date.md" } |
        Sort-Object -Property @{ Expression = { $_.Name.Substring(0, 10) }; Descending = $true }, @{ Expression = { $_.LastWriteTimeUtc }; Descending = $true } |
        Select-Object -First 1
}

$changes = [System.Collections.Generic.List[object]]::new()
function Add-Change {
    param([string]$Item, [string]$Before, [string]$After)
    $changes.Add([pscustomobject]@{ Item = $Item; Before = $Before; After = $After })
}
$deltaFile = $null
if ($previous) {
    $deltaFile = $previous.Name
    $then = Read-Checkpoint $previous.FullName
    foreach ($pair in @(@('stars', 'stars'), @('forks', 'forks'), @('watchers', 'watchers'), @('openIssues', 'open issues'),
                        @('views', 'views, 14-day window'), @('viewsUnique', 'unique viewers, 14-day window'),
                        @('clones', 'clones, 14-day window'), @('clonesUnique', 'unique cloners, 14-day window'),
                        @('total', 'release downloads, total'))) {
        $a = $then[$pair[0]]; $b = $now[$pair[0]]
        if ($null -ne $a -and $null -ne $b -and $a -ne $b) { Add-Change $pair[1] "$a" ("$b ({0:+0;-0})" -f ($b - $a)) }
    }
    foreach ($pair in @(@('description', 'description'), @('homepage', 'homepage'), @('topics', 'topics'), @('ogImage', 'usesCustomOpenGraphImage'), @('pages', 'Pages config'))) {
        $a = $then[$pair[0]]; $b = $now[$pair[0]]
        if ($null -ne $a -and $null -ne $b -and ([string]$a -replace '[`]', '') -ne ([string]$b -replace '[`]', '')) { Add-Change $pair[1] "$a" "$b" }
    }
    foreach ($tag in ($now.releases.Keys | Sort-Object -Descending)) {
        if (-not $then.releases.ContainsKey($tag)) { Add-Change "release $tag" '(not listed)' "$($now.releases[$tag]) downloads (new release)" }
        elseif ($then.releases[$tag] -ne $now.releases[$tag]) { Add-Change "release $tag downloads" "$($then.releases[$tag])" ("$($now.releases[$tag]) ({0:+0;-0})" -f ($now.releases[$tag] - $then.releases[$tag])) }
    }
    if ($null -ne $then.versions -and $null -ne $now.versions) {
        foreach ($v in $now.versions) { if ($v -notin $then.versions) { Add-Change "winget catalog $v" '(absent)' 'in the catalog' } }
        foreach ($v in $then.versions) { if ($v -notin $now.versions) { Add-Change "winget catalog $v" 'in the catalog' '(absent)' } }
    }
    foreach ($n in ($now.prs.Keys | Sort-Object)) {
        if (-not $then.prs.ContainsKey($n)) { Add-Change "winget PR #$n" '(not seen)' $now.prs[$n].state }
        elseif ($then.prs[$n].state -ne $now.prs[$n].state) { Add-Change "winget PR #$n" $then.prs[$n].state $now.prs[$n].state }
    }
    $pageNames = @{
        store = 'Microsoft Store page'; choco = 'Chocolatey page'; scoopMain = 'Scoop Main manifest'; scoopExtras = 'Scoop Extras manifest'
        sourceforge = 'SourceForge page'; alternativeto = 'AlternativeTo page'; landing = 'landing page'; sitemap = 'sitemap.xml'
        robotsProject = 'project robots.txt'; robotsHost = 'host-root robots.txt'
    }
    foreach ($k in ($now.channels.Keys | Sort-Object)) {
        if ($then.channels.ContainsKey($k) -and $then.channels[$k] -and $then.channels[$k] -ne $now.channels[$k]) {
            Add-Change "HTTP status of the $($pageNames[$k])" $then.channels[$k] $now.channels[$k]
        }
    }
    if ($null -ne $then.sitemapLocs -and $null -ne $now.sitemapLocs -and $then.sitemapLocs -ne $now.sitemapLocs) {
        Add-Change 'sitemap.xml entries' "$($then.sitemapLocs)" "$($now.sitemapLocs)"
    }
}

# --- the file ---------------------------------------------------------------------------------

$L = [System.Collections.Generic.List[string]]::new()
function Add-Row {
    param([object[]]$Cells)
    $L.Add('| ' + (($Cells | ForEach-Object { ConvertTo-Cell $_ }) -join ' | ') + ' |')
}

$L.Add("# SP-0166 metrics - checkpoint $Date")
$L.Add('')
$L.Add("Written by ``packaging/write-promo-checkpoint.ps1`` at $readAt, read-only (GET requests and one GraphQL query), from")
$L.Add('public or owner-held counters. Every number below was read from the source named beside it; a number that could')
$L.Add('not be read is written "unknown" with the reason. Nothing here is an estimate. A channel row records the HTTP')
$L.Add('status of its page: a 200 shows that a page exists, not that it is FileDO''s, and a 403, a timeout or any other')
$L.Add('failure is "unknown", never "absent". GitHub keeps only fourteen days of traffic, so while outreach is active the')
$L.Add('next checkpoint is due within fourteen days of this one.')
$L.Add('')
$L.Add('## Repository (`gh api repos/SerZhyAle/FileDO`, GraphQL)')
$L.Add('')
$L.Add('| Field | Value |')
$L.Add('| --- | --- |')
Add-Row 'stargazers_count', (Format-Value (Get-Prop $repo 'stargazers_count'))
Add-Row 'forks_count', (Format-Value (Get-Prop $repo 'forks_count'))
Add-Row 'subscribers_count', (Format-Value (Get-Prop $repo 'subscribers_count'))
Add-Row 'open_issues_count', (Format-Value (Get-Prop $repo 'open_issues_count'))
Add-Row 'has_discussions', (Format-Value (Get-Prop $repo 'has_discussions'))
Add-Row 'license', (Format-Value $licenseId)
Add-Row 'description', $(if ($description) { '"' + $description + '"' } else { '(empty)' })
$homeNote = if (-not $homepage) { '' }
            elseif ($repoUrl -and $homepage.TrimEnd('/') -eq ([string]$repoUrl).TrimEnd('/')) { ' (the repository itself, not the site)' }
            elseif ($homepage.TrimEnd('/') -eq $siteBase.TrimEnd('/')) { ' (the site)' }
            else { '' }
Add-Row 'homepage', $(if ($homepage) { '`' + $homepage + '`' + $homeNote } else { '(empty)' })
Add-Row "topics ($(if ($null -ne $topics) { $topics.Count } else { 'unknown' }))", $(if ($null -eq $topics) { 'unknown (not in the GitHub answer)' } elseif ($topics.Count) { (@($topics | Sort-Object) -join ', ') } else { '(none)' })
Add-Row 'usesCustomOpenGraphImage', $(if ($null -ne $ogImage) { Format-Value ([bool]$ogImage) } else { "unknown ($ogReason)" })
$L.Add('')
$L.Add('## Traffic (`gh api repos/SerZhyAle/FileDO/traffic/*`, the last fourteen days)')
$L.Add('')
$L.Add('GitHub shows traffic only to a token with push access (the owner''s) and forgets it after fourteen days: a day that is')
$L.Add('not in a checkpoint is lost. The two windows below slide, so a difference between two checkpoints is not a count of')
$L.Add('new events.')
$L.Add('')
$L.Add('| Counter | count | uniques |')
$L.Add('| --- | --- | --- |')
Add-Row 'views', $(if ($views.Ok) { $views.Count } else { 'unknown' }), $(if ($views.Ok) { $views.Uniques } else { 'unknown' })
Add-Row 'clones', $(if ($clones.Ok) { $clones.Count } else { 'unknown' }), $(if ($clones.Ok) { $clones.Uniques } else { 'unknown' })
$L.Add('')
if (-not $views.Ok) { $L.Add("Views: unknown ($($views.Reason)).") ; $L.Add('') }
if (-not $clones.Ok) { $L.Add("Clones: unknown ($($clones.Reason)).") ; $L.Add('') }
if ($null -eq $referrers) { $L.Add("Referrers: unknown ($($refR.Reason)).") }
elseif ($referrers.Count -eq 0) { $L.Add('Referrers: none in the window.') }
else { $L.Add('Referrers: ' + ($referrers -join '; ') + '.') }
$L.Add('')
$L.Add('## Release downloads (`gh api repos/SerZhyAle/FileDO/releases`, `.sha256` files excluded)')
$L.Add('')
if ($relReason) {
    $L.Add("Unknown ($relReason).")
} else {
    $L.Add('| Release | Published | Downloads |')
    $L.Add('| --- | --- | --- |')
    foreach ($x in $releaseRows) { Add-Row $x.Tag, $x.Published, $x.Downloads }
    Add-Row "**Total, $($releaseRows.Count) releases**", '', "**$totalDownloads**"
}
$L.Add('')
$L.Add('## Channels')
$L.Add('')
$L.Add('| Channel | State read | Source |')
$L.Add('| --- | --- | --- |')
$wingetParts = @()
if ($null -ne $wingetVersions) {
    $wingetParts += if ($wingetVersions.Count) { 'versions in the catalog: ' + ($wingetVersions -join ', ') } else { 'no versions in the catalog' }
} else {
    $wingetParts += "catalog unknown ($wingetReason)"
}
if ($null -ne $prs) {
    $wingetParts += if ($prs.Count) { ((@($prs) | ForEach-Object { "PR #$($_.Number) for $newestVersion **$($_.State)**" }) -join ', ') }
                    else { "no pull request found for $newestVersion (title search)" }
} else {
    $wingetParts += "PR state unknown ($prReason)"
}
if ($newestVersion -and $null -ne $wingetVersions) {
    $wingetParts += if ($newestVersion -in $wingetVersions) { "newest tag $newestVersion is in the catalog" } else { "newest tag $newestVersion is not in the catalog yet" }
}
$wingetCell = $wingetParts -join '; '
Add-Row 'winget `SerZhyAle.FileDO`', $wingetCell, '`gh api repos/microsoft/winget-pkgs/contents/manifests/s/SerZhyAle/FileDO`, `gh api search/issues`'
Add-Row 'winget installs', 'unknown (winget publishes no install count)', '-'
foreach ($c in $channelRows) {
    Add-Row $c.Label, $c.Text, $c.Source
    if ($c.Key -eq 'store') { Add-Row 'Store acquisitions, ratings', 'unknown (Partner Center, owner-held)', '-' }
}
$L.Add('')
$L.Add('## Site and indexing')
$L.Add('')
$L.Add('| Item | State | Source |')
$L.Add('| --- | --- | --- |')
Add-Row 'Pages config', $pagesText, '`gh api repos/SerZhyAle/FileDO/pages`'
Add-Row 'Landing', $landingText, ('`GET ' + $siteBase + '`')
Add-Row '`sitemap.xml` as served', $sitemapText, ('`GET ' + $siteBase + 'sitemap.xml`')
Add-Row '`robots.txt` at `/FileDO/robots.txt`', $robotsProjectText, ('`GET ' + $siteBase + 'robots.txt`')
Add-Row "Host-root ``$siteRoot/robots.txt``", $robotsHostText, ('`GET ' + $siteRoot + '/robots.txt`')
Add-Row 'Search Console / Bing data', 'unknown (owner-held consoles; a script cannot read them)', '-'
$L.Add('')
$L.Add('Tree facts (page heads, structured data, tracker grep, screenshots) are not counters; `packaging/check-external-docs.ps1`')
$L.Add('holds them.')
if ($unknowns.Count) {
    $L.Add('')
    $L.Add("## Not readable in this run ($($unknowns.Count))")
    $L.Add('')
    foreach ($u in $unknowns) { $L.Add('- ' + (ConvertTo-Cell $u)) }
}
if ($deltaFile) {
    $L.Add('')
    $L.Add("## Change since $deltaFile")
    $L.Add('')
    if ($changes.Count) {
        $L.Add('| Item | Before | Now |')
        $L.Add('| --- | --- | --- |')
        foreach ($ch in $changes) { Add-Row $ch.Item, $ch.Before, $ch.After }
    } else {
        $L.Add('No change in any item that was read both times.')
    }
    $L.Add('')
    $L.Add('Only items read in both files are compared; an item that is unknown in either one is left out.')
} else {
    $L.Add('')
    $L.Add('## Change since the previous checkpoint')
    $L.Add('')
    $L.Add('No earlier checkpoint on or before this date in the output folder.')
}

try {
    New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
    [IO.File]::WriteAllText($outFile, (($L -join "`n") + "`n"), [Text.UTF8Encoding]::new($false))
} catch {
    Write-Host "write-promo-checkpoint: FAIL (cannot write $outFile : $($_.Exception.Message))" -ForegroundColor Red
    exit 1
}

# --- console report ---------------------------------------------------------------------------

Write-Host "write-promo-checkpoint: wrote $outFile" -ForegroundColor Green
if ($unknowns.Count) {
    Write-Host "  not readable in this run ($($unknowns.Count)):" -ForegroundColor Yellow
    foreach ($u in $unknowns) { Write-Host "    $u" -ForegroundColor Yellow }
}
if ($deltaFile) {
    Write-Host "  change since ${deltaFile}:"
    if ($changes.Count) { foreach ($ch in $changes) { Write-Host ("    {0}: {1} -> {2}" -f $ch.Item, $ch.Before, $ch.After) } }
    else { Write-Host '    no change in any item that was read both times' }
} else {
    Write-Host '  no earlier checkpoint to compare with'
}
exit 0

} catch {
    Write-Host "write-promo-checkpoint: FAIL (unexpected: $($_.Exception.Message) at line $($_.InvocationInfo.ScriptLineNumber))" -ForegroundColor Red
    exit 1
}
