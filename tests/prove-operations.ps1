#Requires -Version 7.0
<#
.SYNOPSIS
  Pre-release run of every kind of filedo operation on small data: each one timed, each result checked,
  the whole run compared with the last passing runs so a loss of function or of speed cannot slip by.

.DESCRIPTION
  Runs the shipped exe_to_download\filedo.exe (the binary the release builds) against scratch data and
  judges every operation by its own result event (`--events`: verdict and numbers), its exit code and,
  where it moved or sealed data, by the bytes themselves - a SHA-256 manifest of the source compared with
  the result, never filedo's own word for it.

  Tier 1 - a scratch folder under %TEMP%, no elevation, a private state folder (FILEDO_STATE_DIR):
    help, usage errors, info, speed, clean, the seven copy modes, --precount and --stop-file, a repeated
    copy and a copy that must keep a different file, compare, cmp .. del, cd (report, del / move, the
    name rules), check (modes, resume, report) and its companion, history (a password never stored),
    secure / unsecure / fdsec info / verify in suites 1, 2 and 3 with the refusals (wrong password, a
    tampered and a truncated container), reveal -rw, wipe (refused without the typed word, then --force),
    a batch run, the network target (the machine's own admin share, when it is reachable) and the
    virtual-disk verbs that need no elevation (new, info, verify, export, add / list / forget, a vault's
    wrong password, tests\vd_batch.lst).
    Then the load section: the same operations on data sized so each runs for seconds (2400 and 9000 small
    files, 768 MiB in big files, a 384 MiB suite 2 file, a 1.5 GB speed test, 1 GiB virtual-disk containers),
    because on a few kilobytes every step is process start-up and no loss of throughput could show.
    The plan and why each number is what it is: the `$Load` block below.

  Tier 2 - small mounted virtual disks, because `test` and `fill` are sized by the free space of their
  volume and a folder on the system drive would hand them all of it. It needs administrator rights,
  which the mount does (one UAC prompt for the whole tier when this console is not elevated). A 320 MB
  disk: the size it reports, speed, the capacity test, fill / fill verify / clean, a copy and its
  manifest, check, cd, compare, probe and recover (the data must survive both), folder wipe, the root
  wipe that must be refused, unmount, the container verified, a read-only remount that refuses a write,
  `vd format` (clean) and the size again, destroy. Then an encrypted vault (mount, a wrong password
  refused, the data back under the right one) and a ram disk (save, unmount, remount).

  The mounted drive letter is read from `vd status json` for the scratch container and refused unless it
  is new and not the system drive - nothing here ever points at a drive it did not just mount itself.
  Not covered, on purpose: ui / dm (the shell's own --selftest), vd auto / vd stop (they change the
  machine or drop every mounted disk), fdsec register (the Explorer registration, proven by its own
  tests), reveal without -rw (launches an app), the filedo_fill / filedo_test companions (they fill).

  Coverage is enforced: every verb the exe's own help lists must have an entry in the run's inventory
  (a step, a batch line, or an exemption with a reason), so a verb added later cannot go unexercised.

  Comparison with the last passing runs (temp\evidence\ops-*\timings.json, up to 8, this machine only):
    - function: a step that used to run and no longer does, or whose exit code, verdict or counts changed,
      fails the run - an intentional change is accepted with -Rebaseline;
    - speed: the median of the baseline is the reference. In the load section a step that normally runs 1.5 s
      or more trips the speed gate at 1.5x its median (and 500 ms more), a shorter one at twice (and 300 ms),
      and so does the process start-up figure (the best of seven `filedo -?`, twice its median and 20 ms
      more); a tripped gate repeats the load section once on fresh data and fails only if the best of the
      two still trips. Tier 1 steps, tier 2 and smaller slowdowns of a load step only warn. A load step that
      runs shorter than 1.5 s is reported as too noisy to compare. Gates need 3 passing runs.
  A run that does not pass is never added to the baseline.

  Evidence (per-step output, event streams, timings.json) goes to temp\evidence\ops-<run-id>\ (git-ignored,
  private); the scratch data is removed after a pass and kept after a failure.
  Exit code: 0 passed, 1 a step failed or the comparison did, 2 could not verify (no exe, no mount
  transport, elevation declined).
#>
[CmdletBinding()]
param(
    [string]$Exe = (Join-Path (Split-Path $PSScriptRoot -Parent) 'exe_to_download\filedo.exe'),
    [string]$Version,
    # Leave tier 2 out - reported as skipped, never as passed.
    [switch]$SkipMount,
    # Accept what this run shows as the new baseline: a changed behaviour or a slower step is reported but not failed,
    # and the older runs stop counting.
    [switch]$Rebaseline,
    # A step that outlives this many seconds is killed and fails (a hang on small data is a defect).
    [int]$BudgetSeconds = 90,
    [string]$OutDir,
    [string]$RunId,
    # Where the evidence of every run lives, and so where the baseline is read from. Only the tests of this
    # script's own comparison change it.
    [string]$EvidenceRoot,
    # Internal: the elevated child that runs tier 2 and hands its rows back through -ResultFile.
    [switch]$MountTierOnly,
    [string]$ResultFile
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$repo = Split-Path $PSScriptRoot -Parent
if (-not $RunId) { $RunId = Get-Date -Format 'yyyyMMdd-HHmmss' }
if (-not $EvidenceRoot) { $EvidenceRoot = Join-Path $repo 'temp\evidence' }
if (-not $OutDir) { $OutDir = Join-Path $EvidenceRoot "ops-$RunId" }
$script:Started = Get-Date
$Work = Join-Path ([IO.Path]::GetTempPath()) ("filedo-ops-$RunId" + $(if ($MountTierOnly) { '-mount' } else { '' }))
$MiB = 1MB
$script:Rows = [System.Collections.Generic.List[object]]::new()
$script:Skipped = [System.Collections.Generic.List[string]]::new()
$script:SkippedPrefix = [System.Collections.Generic.List[string]]::new()
$script:Unverified = [System.Collections.Generic.List[string]]::new()
$script:NameCount = @{}
$script:Seq = 0
$script:Note = ''
$script:Tier = '1'
$script:Exe = $Exe
$script:Work = $Work
$script:OutDir = $OutDir
$script:Sha = [Security.Cryptography.SHA256]::Create()

function Done([int]$code, [string]$verdict, [string]$why = '') {
    $color = switch ($code) { 0 { 'Green' } 1 { 'Red' } default { 'Yellow' } }
    $stamp = if ($Version) { $Version } else { 'unknown' }
    Write-Host ("operations-run ${stamp}: $verdict" + $(if ($why) { " ($why)" } else { '' })) -ForegroundColor $color
    exit $code
}

# ---- plumbing ---------------------------------------------------------------------------------

function ConvertTo-ArgString([string[]]$items) {
    ($items | ForEach-Object { if ($_ -eq '' -or $_ -match '[\s"]') { '"' + $_.Replace('"', '\"') + '"' } else { $_ } }) -join ' '
}
function Read-Shared([string]$path) {
    if (-not (Test-Path -LiteralPath $path)) { return '' }
    $fs = [IO.File]::Open($path, 'Open', 'Read', 'ReadWrite')
    try { (New-Object IO.StreamReader($fs, [Text.Encoding]::UTF8)).ReadToEnd() } finally { $fs.Dispose() }
}
function Note([string]$text) { $script:Note = $text }
function N($r, [string]$name) {
    $p = $r.Numbers.PSObject.Properties[$name]
    if ($p) { $p.Value } else { $null }
}
function Test-Elevated {
    ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
}
function Get-FileSha256([string]$path) {
    $fs = [IO.File]::Open($path, 'Open', 'Read', 'ReadWrite')
    try { [BitConverter]::ToString($script:Sha.ComputeHash($fs)).Replace('-', '') } finally { $fs.Dispose() }
}

# The behaviour of one run, as far as it is the same on every run: exit code, verdict and the counts of
# the result event. Speeds and times are left out - they are what the speed comparison is for.
function Get-Sig($r) {
    $parts = foreach ($p in $r.Numbers.PSObject.Properties) {
        if (($p.Name -cmatch 'Ms$') -or ($p.Name -imatch 'mbps|speed|second|elapsed|duration|^eta')) { continue }
        $v = if ($p.Value -is [string] -or $p.Value -is [ValueType]) { [string]::Format([Globalization.CultureInfo]::InvariantCulture, '{0}', $p.Value) } else { ConvertTo-Json $p.Value -Compress -Depth 4 }
        '{0}={1}' -f $p.Name, $v
    }
    '{0}|{1}|{2}' -f $r.Code, $r.Verdict, ((@($parts) | Sort-Object) -join ';')
}

# One filedo run. stdin is an empty file, so a prompt sees "nobody can answer"; stdout and stderr go to
# files (a pipe would be held open by the block server a mount leaves behind). A run that outlives its
# budget is killed with its children.
function Invoke-Filedo {
    param([string]$Name, [string[]]$Arguments, [int]$Budget, [bool]$Events = $true, [bool]$RealState = $false, [string]$Exe = $script:Exe, [bool]$Bare = $false, [bool]$History = $false)
    $n = ++$script:Seq
    $stem = Join-Path $script:OutDir ('{0:000}-{1}' -f $n, ($Name -replace '[^A-Za-z0-9.-]+', '_'))
    $ev = "$stem.events.jsonl"
    # -Bare: a companion launcher's command line is pinned by its tests, so it gets exactly the arguments given.
    $full = if ($Bare) { $Arguments } else { @($(if (-not $History) { '--no-history' })) + $(if ($Events) { @('--events', $ev) } else { @() }) + $Arguments }
    $savedState = $env:FILEDO_STATE_DIR
    if ($RealState) { Remove-Item Env:FILEDO_STATE_DIR -ErrorAction SilentlyContinue }
    try {
        $sw = [Diagnostics.Stopwatch]::StartNew()
        $p = Start-Process -FilePath $Exe -ArgumentList (ConvertTo-ArgString $full) -WorkingDirectory $script:Work -NoNewWindow -PassThru `
            -RedirectStandardInput $script:EmptyFile -RedirectStandardOutput "$stem.out.txt" -RedirectStandardError "$stem.err.txt"
        $null = $p.Handle
        $finished = $p.WaitForExit($Budget * 1000)
        if (-not $finished) {
            & taskkill.exe /PID $p.Id /T /F 2>&1 | Out-Null
            [void]$p.WaitForExit(5000)
        }
        $sw.Stop()
    } finally {
        if ($null -ne $savedState) { $env:FILEDO_STATE_DIR = $savedState } else { Remove-Item Env:FILEDO_STATE_DIR -ErrorAction SilentlyContinue }
    }
    $verdict = $null; $numbers = [pscustomobject]@{}; $hasResult = $false
    if ($Events) {
        foreach ($line in ((Read-Shared $ev) -split "`r?`n")) {
            if ($line -like '*"kind":"result"*') {
                try {
                    $o = $line | ConvertFrom-Json
                    $verdict = [string]$o.data.verdict; $hasResult = $true
                    if ($o.data.PSObject.Properties['numbers'] -and $o.data.numbers) { $numbers = $o.data.numbers }
                } catch { }
            }
        }
    }
    [pscustomobject]@{
        Code = if ($finished) { $p.ExitCode } else { -1 }; Ms = [int]$sw.ElapsedMilliseconds; TimedOut = (-not $finished)
        Verdict = $verdict; Numbers = $numbers; HasResult = $hasResult; Events = $ev
        Text = ((Read-Shared "$stem.out.txt") + (Read-Shared "$stem.err.txt"))
    }
}

# One recorded operation: run it, judge the exit code and the result event, then whatever -Verify finds
# (it emits one string per problem). Prints one line; the row feeds the table, the baseline comparison
# and timings.json. A password given as p:<word> must never reach the event stream or the output.
function Op {
    param([string]$Name, [string[]]$Arguments, [int[]]$Code = @(0), [string[]]$Verdict = @('Done', 'Passed'),
          [long]$Bytes = 0, [scriptblock]$Verify, [int]$Budget = $BudgetSeconds, [switch]$NoEvents, [switch]$RealState, [switch]$Bare,
          [switch]$History, [switch]$NoSig, [long]$Files = 0, [string]$Exe = $script:Exe)
    $script:Note = ''
    $judgeEvent = -not ($NoEvents -or $Bare)
    $r = Invoke-Filedo -Name $Name -Arguments $Arguments -Budget $Budget -Events $judgeEvent -RealState $RealState.IsPresent -Exe $Exe -Bare $Bare.IsPresent -History $History.IsPresent
    $problems = [System.Collections.Generic.List[string]]::new()
    if ($r.TimedOut) {
        $problems.Add("did not finish within $Budget s (killed)")
    } else {
        if ($Code -notcontains $r.Code) { $problems.Add("exit $($r.Code), want $($Code -join '/')") }
        if ($judgeEvent) {
            if (-not $r.HasResult) { $problems.Add('no result event') }
            elseif ($Verdict -notcontains $r.Verdict) { $problems.Add("verdict '$($r.Verdict)', want $($Verdict -join '/')") }
        }
        foreach ($a in $Arguments) {
            if ($a -match '^p:(.{4,})$') {
                $secret = [regex]::Escape($Matches[1])
                if ($judgeEvent -and (Read-Shared $r.Events) -match $secret) { $problems.Add('the password reached the event stream') }
                if ($r.Text -match $secret) { $problems.Add('the password was printed') }
            }
        }
        if ($Verify) { foreach ($p in @(& $Verify $r)) { if ($p) { $problems.Add([string]$p) } } }
    }
    $mbps = if ($Bytes -gt 0 -and $r.Ms -gt 0) { [math]::Round($Bytes / $MiB / ($r.Ms / 1000.0), 1) } else { $null }
    if ($Files -gt 0 -and $r.Ms -gt 0) { $script:Note = ((@($script:Note, ('{0:N2} ms/file' -f ($r.Ms / $Files))) | Where-Object { $_ }) -join '  ') }
    $sig = if ($NoSig -or $r.TimedOut -or $Bare) { '' } else { Get-Sig $r }
    Add-Row -Name $Name -Ms $r.Ms -MBps $mbps -Note $script:Note -Problems @($problems) -Code $r.Code -Verdict $r.Verdict -Sig $sig
    if ($problems.Count) {
        $tail = (($r.Text -split "`r?`n" | Where-Object { $_.Trim() } | Select-Object -Last 4) -join ' | ')
        if ($tail) { Write-Host "         last output: $tail" -ForegroundColor DarkGray }
    }
    $r
}
function Add-Row {
    param([string]$Name, [int]$Ms, $MBps, [string]$Note, [string[]]$Problems, [int]$Code = 0, [string]$Verdict = '', [string]$Sig = '')
    # A name is the row's identity in the baseline, so a repeated one is numbered rather than shared.
    $script:NameCount[$Name] = 1 + $(if ($script:NameCount.ContainsKey($Name)) { $script:NameCount[$Name] } else { 0 })
    if ($script:NameCount[$Name] -gt 1) { $Name = "$Name #$($script:NameCount[$Name])" }
    $row = [pscustomobject]@{ Name = $Name; Tier = $script:Tier; Ms = $Ms; MBps = $MBps; Note = $Note; Pass = (@($Problems).Count -eq 0); Problems = @($Problems); Code = $Code; Verdict = $Verdict; Sig = $Sig }
    $script:Rows.Add($row)
    Write-RowLine $row
}
function Write-RowLine($row) {
    $speed = if ($null -ne $row.MBps) { '{0,8:N1} MB/s' -f $row.MBps } else { ' ' * 13 }
    $line = '{0} {1,-44} {2,7} ms {3}  {4}' -f $(if ($row.Pass) { '[ OK ]' } else { '[FAIL]' }), $row.Name, $row.Ms, $speed, $row.Note
    Write-Host $line -ForegroundColor $(if ($row.Pass) { 'Gray' } else { 'Red' })
    foreach ($p in $row.Problems) { Write-Host "         - $p" -ForegroundColor Red }
}
# A check that is not a filedo run (a file tree, a size) - recorded as a row with no timing.
function Check([string]$name, [string[]]$problems) { Add-Row -Name $name -Ms 0 -MBps $null -Note '' -Problems @($problems | Where-Object { $_ }) }

# ---- fixtures and the byte-level judges --------------------------------------------------------

function Get-Manifest([string]$dir) {
    $m = @{}
    $base = (Get-Item -LiteralPath $dir).FullName.TrimEnd('\')
    foreach ($f in Get-ChildItem -LiteralPath $dir -Recurse -File -Force) {
        $m[$f.FullName.Substring($base.Length + 1)] = [pscustomobject]@{ Size = $f.Length; Hash = (Get-FileSha256 $f.FullName); Time = $f.LastWriteTimeUtc }
    }
    $m
}
# The problems between the source manifest and a result: a missing or extra file, another size or
# hash, a modification time off by more than two seconds (copy and the containers keep it).
function Compare-Manifest($want, $got, [switch]$IgnoreTime) {
    foreach ($k in $want.Keys) {
        if (-not $got.ContainsKey($k)) { "missing: $k"; continue }
        if ($got[$k].Size -ne $want[$k].Size) { "size differs: $k ($($got[$k].Size), want $($want[$k].Size))"; continue }
        if ($got[$k].Hash -ne $want[$k].Hash) { "content differs: $k" }
        elseif (-not $IgnoreTime -and [math]::Abs(($got[$k].Time - $want[$k].Time).TotalSeconds) -gt 2) { "modification time differs: $k" }
    }
    foreach ($k in $got.Keys) { if (-not $want.ContainsKey($k)) { "extra: $k" } }
}
function New-SourceTree([string]$dir) {
    $rnd = [Random]::new(20261002)
    $i = 0
    $put = {
        param([string]$rel, [byte[]]$bytes)
        $path = Join-Path $dir $rel
        [void][IO.Directory]::CreateDirectory((Split-Path $path))
        [IO.File]::WriteAllBytes($path, $bytes)
        (Get-Item -LiteralPath $path).LastWriteTime = [datetime]'2025-03-01T10:00:00'
        (Get-Item -LiteralPath $path).LastWriteTime = (Get-Item -LiteralPath $path).LastWriteTime.AddMinutes((++$script:fixtureStep) * 17)
    }
    $script:fixtureStep = 0
    $rand = { param([int]$size) $b = New-Object byte[] $size; $rnd.NextBytes($b); , $b }
    foreach ($i in 1..24) { & $put ('a\f{0:00}.bin' -f $i) (& $rand ($i * 29KB)) }
    & $put 'big.bin' (& $rand (4 * $MiB))
    & $put 'a\b\empty.bin' ([byte[]]@())
    $uni = [string]::new([char[]](0x444, 0x430, 0x439, 0x43B, 0x2D, 0x451))   # a Cyrillic name, on purpose
    & $put "with space\$uni.txt" ([Text.Encoding]::UTF8.GetBytes('text under a spaced and non-Latin path'))
    $dup = & $rand (64KB)
    foreach ($d in 'd1', 'd2', 'd3') { & $put "dups\$d.bin" $dup }
    Get-Manifest $dir
}
# ---- the load plan -----------------------------------------------------------------------------------
# Every size below is chosen so the operation it feeds runs for seconds: long enough that process start-up and
# scheduling noise is a few percent of it, and a real loss of speed shows. The figures come from measuring
# this build on 2026-10-02 (a NVMe system drive, Defender on): per-file work dominates copy (about 1 ms a
# file; safecopy and synccopy 2.5), check (0.27 ms), cd, wipe (0.09 ms) and cmp .. del (2.3 ms and worse
# above 1000 files); bytes dominate secure / unsecure / fdsec verify (250-700 MB/s), speed and the
# virtual-disk files. compare reads metadata only (0.01 ms a file) and a big-file copy runs at 2 GB/s: neither
# can be made to last seconds with a sane amount of data, so they are timed and cannot gate. A load step
# that runs shorter than MinStepMs is reported, so a plan that has gone stale (an operation got faster) says so.
$Load = [pscustomobject]@{
    CopyFiles = 2400        # the copy modes
    ManyFiles = 4500        # in each of two trees: check, deep check (first runs cost about 1 ms a file); cd and wipe take both
    CmpFiles = 1000         # cmp .. del
    BigFiles = 6; BigMiB = 128   # secure / verify / unsecure: 768 MiB
    Suite2MiB = 640         # secure, suite 2 (one file; about 250 MB/s secure, 400 unsecure)
    SpeedMB = 1536          # folder speed
    VdMiB = 1536            # the virtual-disk container files (clone is the slow one: about 200 MB/s)
    MinFreeGiB = 8          # the plan peaks at about 4.5 GiB of the temp drive (the 1.5 GiB containers and their raw export); a gate against a full disk
    MinStepMs = 1500
    ShortByNature = @('load: compare', 'load: copy again, all there', 'load: copy, big files', 'load: fdsec verify', 'load: vd verify', 'load: wipe the many-file tree', 'load: cd', 'load: unsecure the big tree', 'load: unsecure, suite 2', 'load: vd new, fast (all allocated)')
}
# Small random files in numbered folders, each file its own length; a shared buffer keeps the fixture quick.
function New-SmallFiles([string]$dir, [int]$count, [int]$minSize, [int]$spread, [int]$perDir, [int]$seed) {
    $rnd = [Random]::new($seed)
    $buf = New-Object byte[] ($minSize + $spread)
    $sub = $dir
    for ($i = 0; $i -lt $count; $i++) {
        if ($i % $perDir -eq 0) { $sub = Join-Path $dir ('d{0:000}' -f [int]($i / $perDir)); [void][IO.Directory]::CreateDirectory($sub) }
        $rnd.NextBytes($buf)
        $fs = [IO.File]::Create((Join-Path $sub ('f{0:00000}.dat' -f $i)))
        $fs.Write($buf, 0, $minSize + $rnd.Next($spread + 1)); $fs.Dispose()
    }
}
# Big random files, the random bytes made once per file so no two files are alike.
function New-BigFile([string]$path, [int]$sizeMiB, [int]$seed) {   # not "$mib": variable names are case-insensitive and $MiB is the unit
    $rnd = [Random]::new($seed)
    $buf = New-Object byte[] (1 * $MiB)
    $fs = [IO.File]::Create($path)
    try { for ($i = 0; $i -lt $sizeMiB; $i++) { $rnd.NextBytes($buf); $fs.Write($buf, 0, $buf.Length) } } finally { $fs.Dispose() }
}
function New-DupFolder([string]$dir) {
    [void][IO.Directory]::CreateDirectory($dir)
    $b = New-Object byte[] (48KB); ([Random]::new(7)).NextBytes($b)
    $year = 2020
    foreach ($d in 'older', 'middle', 'newer') {
        $p = Join-Path $dir "$d.bin"
        [IO.File]::WriteAllBytes($p, $b)
        (Get-Item -LiteralPath $p).CreationTime = [datetime]"$year-06-01T12:00:00"
        $year += 2
    }
    $u = New-Object byte[] (30KB); ([Random]::new(8)).NextBytes($u)
    [IO.File]::WriteAllBytes((Join-Path $dir 'unique.bin'), $u)
}
# Three files with the same bytes and names that sort apart: what the abc / xyz rules choose between.
function New-NameDupFolder([string]$dir) {
    [void][IO.Directory]::CreateDirectory($dir)
    $b = New-Object byte[] (4KB); ([Random]::new(5)).NextBytes($b)
    foreach ($d in 'alpha', 'mike', 'zulu') { [IO.File]::WriteAllBytes((Join-Path $dir "$d.bin"), $b) }
}
function Remove-Scratch([string]$path) {
    # Only ever something this run made: under the temp folder, named for this run or its parts.
    $temp = [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') + '\'
    $full = [IO.Path]::GetFullPath($path)
    if (-not $full.StartsWith($temp, [StringComparison]::OrdinalIgnoreCase) -or -not $full.StartsWith($script:Work, [StringComparison]::OrdinalIgnoreCase)) { return }
    if (Test-Path -LiteralPath $full -PathType Container) { [IO.Directory]::Delete($full, $true) }
    elseif (Test-Path -LiteralPath $full) { [IO.File]::Delete($full) }
}
function Count-Files([string]$dir) { if (Test-Path -LiteralPath $dir) { @(Get-ChildItem -LiteralPath $dir -Recurse -File -Force).Count } else { 0 } }

function Test-CopyNumbers($r, [int]$files, [long]$bytes) {
    if ((N $r 'copiedFiles') -ne $files) { "copiedFiles = $(N $r 'copiedFiles'), want $files" }
    if ((N $r 'copiedBytes') -ne $bytes) { "copiedBytes = $(N $r 'copiedBytes'), want $bytes" }
    foreach ($z in 'failedFiles', 'keptFiles', 'nameCollisions', 'damagedSkippedFiles', 'uncheckedFiles', 'notRegularFiles') {
        if ((N $r $z) -ne 0) { "$z = $(N $r $z), want 0" }
    }
}
# ---- tier 1: a scratch folder, no elevation ------------------------------------------------------

function Invoke-Tier1 {
    $script:Tier = '1'
    $env:FILEDO_STATE_DIR = Join-Path $Work 'state'
    $src = Join-Path $Work 'src'
    Write-Host "`n== Tier 1: operations on scratch data ($Work) ==" -ForegroundColor Cyan
    $want = New-SourceTree $src
    $nFiles = $want.Count
    $nBytes = ($want.Values | Measure-Object Size -Sum).Sum
    $nReadable = @($want.Values | Where-Object { $_.Size -gt 0 }).Count
    Write-Host ("  source tree: {0} files, {1:N1} MiB" -f $nFiles, ($nBytes / $MiB))

    # -- help and the ways a command line is wrong: they must say so and exit 2
    [void](Op 'help: help is the detailed screen' @('help') -NoEvents -Verify {
        param($r) if ($r.Text -notmatch 'DEVICE OPERATIONS' -or $r.Text -notmatch 'VIRTUAL DISKS') { 'the detailed help lacks its device or virtual-disk sections' }
    })
    [void](Op 'help: -? lists the main operations' @('-?') -NoEvents -Verify {
        param($r) if ($r.Text -notmatch 'MAIN OPERATIONS') { 'the short help does not list the main operations' }
    })
    [void](Op 'usage: an unknown command' @('bogusverb') -Code @(2) -Verdict @('Not proven') -Verify {
        param($r) if ($r.Text -notmatch 'Unknown command') { 'no "Unknown command" message' }
    })
    [void](Op 'usage: copy without a target' @('copy', $src) -Code @(2) -Verdict @('Not proven') -Verify {
        param($r) if ($r.Text -notmatch 'requires source and target') { 'no "requires source and target" message' }
    })
    [void](Op 'usage: compare with one path' @('compare', $src) -Code @(2) -Verdict @('Not proven'))
    [void](Op 'usage: info of a path that is not there' @((Join-Path $Work 'no-such-folder'), 'info') -Code @(2) -Verdict @('Not proven') -Verify {
        param($r) if ($r.Text -notmatch 'does not exist') { 'no "does not exist" message' }
    })

    # -- information
    [void](Op 'info: folder' @($src, 'info'))
    [void](Op 'info: folder, short' @($src, 'info', 'short') -Verify {
        param($r) if ($r.Text -notmatch "Full Contains:\s+$nFiles files") { "the short info does not count $nFiles files" }
    })
    [void](Op 'info: file' @((Join-Path $src 'big.bin'), 'info'))
    # `device C: info` walks the whole drive (measured: over 60 s on a system drive), so a device's info
    # is only run on the small mounted disk of tier 2.

    # -- speed, and the clean that follows a kept test file
    [void](Op 'speed: 8 MB, deleted after' @('folder', $Work, 'speed', '8') -Bytes (16 * $MiB) -Verify {
        param($r)
        if ((N $r 'fileSizeMB') -ne 8) { "fileSizeMB = $(N $r 'fileSizeMB'), want 8" }
        if (-not ((N $r 'uploadMBps') -gt 0 -and (N $r 'downloadMBps') -gt 0)) { 'a speed is not positive' }
        Note ('up {0:N0} / down {1:N0} MB/s' -f (N $r 'uploadMBps'), (N $r 'downloadMBps'))
        $left = @(Get-ChildItem -LiteralPath $Work -Filter 'speedtest*').Count
        if ($left) { "$left speedtest file(s) left behind" }
    })
    [void](Op 'speed: 2 MB, nodel' @('folder', $Work, 'speed', '2', 'nodel') -Verify {
        param($r)
        $left = @(Get-ChildItem -LiteralPath $Work -Filter 'speedtest*').Count
        if ($left -ne 1) { "$left speedtest files kept, want 1" }
    })
    [void](Op 'clean: the kept test file' @('folder', $Work, 'clean', '--yes') -Verify {
        param($r)
        if ((N $r 'filesDeleted') -ne 1) { "filesDeleted = $(N $r 'filesDeleted'), want 1" }
        $left = @(Get-ChildItem -LiteralPath $Work -Filter 'speedtest*').Count
        if ($left) { "$left speedtest file(s) still there" }
    })

    # -- the seven copy modes: the bytes, the names and the times must arrive
    foreach ($mode in 'copy', 'fastcopy', 'balanced', 'maxcopy', 'smartcopy', 'safecopy', 'synccopy') {
        $dst = Join-Path $Work "dst-$mode"
        [void](Op "copy: $mode" @($mode, $src, $dst) -Bytes $nBytes -Verify {
            param($r)
            Test-CopyNumbers $r $nFiles $nBytes
            Compare-Manifest $want (Get-Manifest $dst)
        })
        if ($mode -notin 'copy', 'balanced', 'maxcopy') { Remove-Scratch $dst }
    }
    $dstPre = Join-Path $Work 'dst-precount'
    [void](Op 'copy: --precount' @('copy', $src, $dstPre, '--precount') -Bytes $nBytes -Verify {
        param($r)
        Test-CopyNumbers $r $nFiles $nBytes
        Compare-Manifest $want (Get-Manifest $dstPre)
    })
    Remove-Scratch $dstPre
    # A stop that was asked for before the start ends the run Stopped (exit 0), and what is under a final
    # name is whole: copy writes through a temporary name, so nothing half-written may be left.
    $stopFlag = Join-Path $Work 'stop.flag'; Set-Content -LiteralPath $stopFlag -Value 'stop'
    $dstStop = Join-Path $Work 'dst-stop'
    [void](Op 'copy: --stop-file asked before the start' @('copy', $src, $dstStop, '--stop-file', $stopFlag) -Verdict @('Stopped') -Verify {
        param($r)
        if (Test-Path -LiteralPath $dstStop) {
            $got = Get-Manifest $dstStop
            foreach ($k in $got.Keys) { if (-not $want.ContainsKey($k) -or $got[$k].Hash -ne $want[$k].Hash) { "not a whole file under its final name: $k" } }
        }
    })
    Remove-Scratch $dstStop; Remove-Scratch $stopFlag
    $dstCopy = Join-Path $Work 'dst-copy'
    [void](Op 'copy: again, nothing to do' @('copy', $src, $dstCopy) -Verify {
        param($r)
        if ((N $r 'alreadyThereFiles') -ne $nFiles) { "alreadyThereFiles = $(N $r 'alreadyThereFiles'), want $nFiles" }
        if ((N $r 'copiedFiles') -ne 0) { "copiedFiles = $(N $r 'copiedFiles'), want 0" }
    })
    [void](Op 'copy: one file' @('file', (Join-Path $src 'big.bin'), 'copy', (Join-Path $Work 'big-copy.bin')) -Bytes (4 * $MiB) -Verify {
        param($r)
        if ((Get-FileSha256 (Join-Path $Work 'big-copy.bin')) -ne $want['big.bin'].Hash) { 'the copied file differs' }
    })

    # -- compare: the counts are in the result event now (SP-0130 rule 16); the
    #    byte-level manifest stays the independent judge of tree equality
    [void](Op 'compare: identical trees' @('compare', $src, $dstCopy) -Verify {
        param($r)
        foreach ($pair in @(@('onlyInSource', 0), @('onlyInTarget', 0), @('differentFiles', 0), @('sameFiles', $nFiles), @('totalSource', $nFiles), @('totalTarget', $nFiles))) {
            if ((N $r $pair[0]) -ne $pair[1]) { "$($pair[0]) = $(N $r $pair[0]), want $($pair[1])" }
        }
    })
    Remove-Item -LiteralPath (Join-Path $dstCopy 'a\f01.bin')
    Set-Content -LiteralPath (Join-Path $dstCopy 'a\f02.bin') -Value 'cut short'
    (Get-Item -LiteralPath (Join-Path $dstCopy 'a\f02.bin')).LastWriteTime = $want['a\f02.bin'].Time.ToLocalTime()
    [void](Op 'compare: one missing, one cut short' @('compare', $src, $dstCopy, '--strict') -Code @(1) -Verdict @('Failed') -Verify {
        param($r)
        if ((N $r 'onlyInSource') -ne 1) { "onlyInSource = $(N $r 'onlyInSource'), want 1" }
        if ((N $r 'differentFiles') -ne 1) { "differentFiles = $(N $r 'differentFiles'), want 1" }
    })
    [void](Op 'copy: refuses to overwrite a different file' @('copy', $src, $dstCopy) -Code @(2) -Verdict @('Not proven') -Verify {
        param($r)
        if ((N $r 'copiedFiles') -ne 1) { "copiedFiles = $(N $r 'copiedFiles'), want 1 (the missing file)" }
        if ((N $r 'keptFiles') -ne 1) { "keptFiles = $(N $r 'keptFiles'), want 1" }
        if ((Get-Item -LiteralPath (Join-Path $dstCopy 'a\f02.bin')).Length -ge 100) { 'the different file was overwritten' }
        if ((Get-FileSha256 (Join-Path $dstCopy 'a\f01.bin')) -ne $want['a\f01.bin'].Hash) { 'the missing file came back wrong' }
    })
    Remove-Scratch $dstCopy

    $dstA = Join-Path $Work 'dst-balanced'; $dstB = Join-Path $Work 'dst-maxcopy'
    [void](Op 'cmp: del source (identical pairs)' @('cmp', $dstA, $dstB, 'del', 'source', '-y') -Verify {
        param($r)
        $left = Count-Files $dstA
        if ($left) { "$left file(s) still in the source of the pair" }
        Compare-Manifest $want (Get-Manifest $dstB)
    })
    Remove-Scratch $dstA; Remove-Scratch $dstB

    # -- duplicates: the report, the age rules (old / new) and the name rules (abc / xyz)
    [void](Op 'cd: report' @($src, 'cd', 'short') -Verify {
        param($r)
        if ((N $r 'duplicateGroups') -ne 1) { "duplicateGroups = $(N $r 'duplicateGroups'), want 1" }
        if ((N $r 'duplicateFiles') -ne 2) { "duplicateFiles = $(N $r 'duplicateFiles'), want 2" }
        if ((N $r 'filesScanned') -ne $nReadable) { "filesScanned = $(N $r 'filesScanned'), want $nReadable" }
    })
    $dupDel = Join-Path $Work 'dups-del'; New-DupFolder $dupDel
    [void](Op 'cd: del old, keep the newest' @($dupDel, 'cd', 'del', 'old', '-y') -Verify {
        param($r)
        if ((N $r 'deleted') -ne 2) { "deleted = $(N $r 'deleted'), want 2" }
        $left = @(Get-ChildItem -LiteralPath $dupDel -File | ForEach-Object Name | Sort-Object)
        if (($left -join ',') -ne 'newer.bin,unique.bin') { "left: $($left -join ', '); want newer.bin, unique.bin" }
    })
    $dupMove = Join-Path $Work 'dups-move'; New-DupFolder $dupMove
    $moved = Join-Path $Work 'dups-moved'; [void][IO.Directory]::CreateDirectory($moved)
    [void](Op 'cd: move new, keep the oldest' @($dupMove, 'cd', 'move', $moved, 'new', '-y') -Verify {
        param($r)
        if ((N $r 'moved') -ne 2) { "moved = $(N $r 'moved'), want 2" }
        if ((Count-Files $moved) -ne 2) { "$(Count-Files $moved) file(s) in the move target, want 2" }
        if (-not (Test-Path -LiteralPath (Join-Path $dupMove 'older.bin'))) { 'the oldest copy is gone' }
    })
    foreach ($rule in @(@('abc', 'zulu.bin'), @('xyz', 'alpha.bin'))) {
        $dupName = Join-Path $Work "dups-$($rule[0])"; New-NameDupFolder $dupName
        $keeps = $rule[1]
        [void](Op "cd: del $($rule[0]), keep $keeps" @($dupName, 'cd', 'del', $rule[0], '-y') -Verify {
            param($r)
            $left = @(Get-ChildItem -LiteralPath $dupName -File | ForEach-Object Name)
            if (($left -join ',') -ne $keeps) { "left: $($left -join ', '); want only $keeps" }
        })
    }

    # -- check: the default, its modes, the good list, the report; and the companion launcher over the same verb
    [void](Op 'check: tree' @('check', $src) -Verify {
        param($r)
        if ((N $r 'checkedFiles') -ne $nReadable) { "checkedFiles = $(N $r 'checkedFiles'), want $nReadable (empty files are not read)" }
        if ((N $r 'damagedFiles') -ne 0) { "damagedFiles = $(N $r 'damagedFiles'), want 0" }
    })
    [void](Op 'check: deep mode' @('check', $src, '--mode', 'deep') -Verify {
        param($r) if ((N $r 'checkedFiles') -ne $nReadable) { "checkedFiles = $(N $r 'checkedFiles'), want $nReadable" }
    })
    [void](Op 'check: --include-ext' @('check', $src, '--include-ext', 'txt') -Verify {
        param($r) if ((N $r 'checkedFiles') -ne 1) { "checkedFiles = $(N $r 'checkedFiles'), want 1 (the one .txt)" }
    })
    # --max-files is a hard limit: a read is claimed before the file is opened,
    # so not one file past the limit is read (SP-0130 R5). The sweep stops the moment the
    # limit is hit, so how many files the walk had reached by then (totalFiles, and the
    # checked/unverified split) races with the workers: the limit itself is verified every
    # run, but the counts cannot be compared against the baseline (run ops-20261003-222014
    # failed on nothing else but this count).
    [void](Op 'check: --max-files stops at the limit' @('check', $src, '--max-files', '2', '--single-reader', 'off', '--workers', '20') -NoSig -Verify {
        param($r)
        $read = (N $r 'checkedFiles') + (N $r 'unverifiedFiles')
        if ($read -ne 2) { "checkedFiles + unverifiedFiles = $read, want 2" }
    })
    $report = Join-Path $Work 'check-report.json'
    [void](Op 'check: --report json' @('check', $src, '--report', 'json', '--report-file', $report) -Verify {
        param($r)
        if (-not (Test-Path -LiteralPath $report)) { return 'no report file written' }
        $rows = @(Get-Content -LiteralPath $report -Raw | ConvertFrom-Json)
        $ok = @($rows | Where-Object { $_.status -eq 'ok' }).Count
        if ($ok -ne $nReadable) { "$ok ok rows in the report, want $nReadable" }
    })
    # Every file is on the good list by now: a resumed run reads none of them
    # and ends Passed - the good list is check's own proof (SP-0130 D2).
    [void](Op 'check: --resume skips what was read' @('check', $src, '--resume') -Verify {
        param($r)
        if ((N $r 'skippedGoodFiles') -ne $nReadable) { "skippedGoodFiles = $(N $r 'skippedGoodFiles'), want $nReadable" }
        if ((N $r 'checkedFiles') -ne 0) { "checkedFiles = $(N $r 'checkedFiles'), want 0" }
    })
    $companion = Join-Path (Split-Path $script:Exe -Parent) 'filedo_check.exe'
    if (Test-Path $companion) {
        [void](Op 'check: companion launcher' @($src, '--no-history') -Exe $companion -Bare)
    } else {
        $script:Skipped.Add('check: companion launcher (filedo_check.exe is not beside filedo.exe)')
        $script:SkippedPrefix.Add('check: companion')
    }

    # -- history: a run leaves an entry, its password never
    $pw = 'p:Pa55word'
    $histFile = Join-Path $Work 'hist-secret.bin'; Copy-Item (Join-Path $src 'a\f03.bin') $histFile
    $histSecret = 'Hist-SECRET-7731'
    [void](Op 'history: a password on the line is not stored' @($histFile, 'secure', "p:$histSecret") -History -Verify {
        param($r)
        $files = @(Get-ChildItem -LiteralPath (Join-Path $Work 'state') -Recurse -File -Filter 'history.json' -ErrorAction SilentlyContinue)
        if (-not $files.Count) { 'no history.json was written' }
        foreach ($f in $files) {
            $raw = Read-Shared $f.FullName
            if ($raw -match [regex]::Escape($histSecret)) { "the password is in $($f.Name)" }
            $entries = ConvertFrom-Json $raw -ErrorAction SilentlyContinue
            if ($null -ne $entries -and $entries.Count -ne 1) { "history.json holds $($entries.Count) entries, want exactly 1 (legacy files leaked into the override state folder)" }
        }
    })
    [void](Op 'history: hist lists the run' @('hist') -NoEvents -Verify {
        param($r)
        if ($r.Text -notmatch 'hist-secret\.bin') { 'hist does not list the secure run' }
        elseif ($r.Text -notmatch 'Last 1 history entries:') { 'hist does not list exactly 1 entry' }
    })

    # -- containers: .fd-sec in the three suites, and every way it must refuse
    $one = Join-Path $Work 'one.bin'; Copy-Item (Join-Path $src 'a\f20.bin') $one
    $oneHash = Get-FileSha256 $one
    $oneC = Join-Path $Work 'one.fd-sec'
    [void](Op 'secure: file, suite 1' @($one, 'secure', $pw) -Bytes (Get-Item $one).Length -Verify {
        param($r)
        if (-not (Test-Path $oneC)) { 'no container written' } elseif ((Get-Item $oneC).Length -le (Get-Item $one).Length) { 'the container is not larger than its content' }
    })
    [void](Op 'fdsec: info' @('fdsec', 'info', $oneC, $pw) -Verdict @('Passed'))
    [void](Op 'fdsec: verify' @('fdsec', 'verify', $oneC, $pw) -Verdict @('Passed'))
    $oneOut = Join-Path $Work 'one-out.bin'
    [void](Op 'unsecure: file, suite 1' @($oneC, 'unsecure', 'to', $oneOut, $pw) -Bytes (Get-Item $one).Length -Verify {
        param($r)
        if (-not (Test-Path $oneOut)) { 'nothing restored' } elseif ((Get-FileSha256 $oneOut) -ne $oneHash) { 'the restored file differs' }
    })
    $badOut = Join-Path $Work 'bad-out.bin'
    [void](Op 'unsecure: wrong password refused' @($oneC, 'unsecure', 'to', $badOut, 'p:not-the-password') -Code @(3) -Verdict @('Failed') -Verify {
        param($r) if (Test-Path $badOut) { 'a file was written for a wrong password' }
    })
    $big = Join-Path $Work 'tamper.bin'; Copy-Item (Join-Path $src 'big.bin') $big
    [void](Op 'secure: 4 MB, for the damage tests' @($big, 'secure', $pw) -Bytes (4 * $MiB))
    $bigC = Join-Path $Work 'tamper.fd-sec'
    $bytes = [IO.File]::ReadAllBytes($bigC)
    $flip = $bytes.Clone(); $flip[[int]($flip.Length / 2)] = $flip[[int]($flip.Length / 2)] -bxor 0xFF
    [IO.File]::WriteAllBytes((Join-Path $Work 'flipped.fd-sec'), $flip)
    [IO.File]::WriteAllBytes((Join-Path $Work 'cut.fd-sec'), $bytes[0..([int]($bytes.Length / 2))])
    [void](Op 'fdsec: verify, a flipped byte refused' @('fdsec', 'verify', (Join-Path $Work 'flipped.fd-sec'), $pw) -Code @(3, 4) -Verdict @('Failed'))
    [void](Op 'fdsec: verify, a cut container refused' @('fdsec', 'verify', (Join-Path $Work 'cut.fd-sec'), $pw) -Code @(4) -Verdict @('Failed'))
    $s2 = Join-Path $Work 's2.bin'; Copy-Item (Join-Path $src 'a\f12.bin') $s2
    $s2Hash = Get-FileSha256 $s2
    [void](Op 'secure: file, suite 2' @($s2, 'secure', $pw, 'suite2') -Bytes (Get-Item $s2).Length)
    $s2Out = Join-Path $Work 's2-out.bin'
    [void](Op 'unsecure: file, suite 2' @((Join-Path $Work 's2.fd-sec'), 'unsecure', 'to', $s2Out, $pw) -Verify {
        param($r) if (-not (Test-Path $s2Out) -or (Get-FileSha256 $s2Out) -ne $s2Hash) { 'the restored file differs' }
    })
    $folderC = Join-Path $Work 'src.fd-sec'
    [void](Op 'secure: folder, suite 3' @($src, 'secure', $pw) -Bytes $nBytes -Verify { param($r) if (-not (Test-Path $folderC)) { 'no container written' } })
    $restored = Join-Path $Work 'restored'
    [void](Op 'unsecure: folder tree' @($folderC, 'unsecure', 'to', $restored, $pw) -Bytes $nBytes -Verify {
        param($r) Compare-Manifest $want (Get-Manifest $restored)
    })
    Remove-Scratch $restored
    $gone = Join-Path $Work 'seal-del.bin'; Copy-Item (Join-Path $src 'a\f07.bin') $gone
    $goneHash = Get-FileSha256 $gone
    [void](Op 'secure: del removes the original' @($gone, 'secure', $pw, 'del', '-y') -Verify {
        param($r) if (Test-Path $gone) { 'the original is still there' }; if (-not (Test-Path (Join-Path $Work 'seal-del.fd-sec'))) { 'no container' }
    })
    $goneOut = Join-Path $Work 'seal-del-out.bin'
    [void](Op 'unsecure: the sealed-then-deleted file' @((Join-Path $Work 'seal-del.fd-sec'), 'unsecure', 'to', $goneOut, $pw) -Verify {
        param($r) if (-not (Test-Path $goneOut) -or (Get-FileSha256 $goneOut) -ne $goneHash) { 'the restored file differs' }
    })
    # SP-0126: a del nobody can answer is the usage class - the original and the verified container both stay.
    $kept = Join-Path $Work 'kept.bin'; Copy-Item (Join-Path $src 'a\f09.bin') $kept
    $keptHash = Get-FileSha256 $kept
    [void](Op 'secure: del, nobody to ask' @($kept, 'secure', $pw, 'del') -Code @(2) -Verdict @('Not proven') -Verify {
        param($r)
        if (-not (Test-Path $kept)) { 'the original is gone although nobody confirmed the delete' }
        elseif ((Get-FileSha256 $kept) -ne $keptHash) { 'the original changed although nobody confirmed the delete' }
        if (-not (Test-Path (Join-Path $Work 'kept.fd-sec'))) { 'no container written' }
        if ($r.Text -notmatch '-y') { 'the refusal does not name -y' }
    })
    $keptOut = Join-Path $Work 'kept-out.bin'
    [void](Op 'unsecure: the container behind an unanswered del' @((Join-Path $Work 'kept.fd-sec'), 'unsecure', 'to', $keptOut, $pw) -Verify {
        param($r) if (-not (Test-Path $keptOut) -or (Get-FileSha256 $keptOut) -ne $keptHash) { 'the container behind an unanswered del does not restore' }
    })
    $revOut = Join-Path $Work 'reveal-out.bin'
    [void](Op 'reveal: -rw to a file' @($oneC, 'reveal', '-rw', 'to', $revOut, $pw) -Verify {
        param($r) if (-not (Test-Path $revOut) -or (Get-FileSha256 $revOut) -ne $oneHash) { 'the revealed file differs' }
    })

    # -- wipe: the typed word is the safety, --force skips only the prompt
    $wipeDir = Join-Path $Work 'wipe-me'
    foreach ($i in 1..3) { New-Item -ItemType Directory -Force (Join-Path $wipeDir "sub$i") | Out-Null; Set-Content (Join-Path $wipeDir "sub$i\f.txt") "x$i" }
    Set-Content (Join-Path $wipeDir 'top.txt') 'top'
    [void](Op 'wipe: refused without the typed word' @('folder', $wipeDir, 'wipe') -Code @(2) -Verdict @('Not proven', 'Failed') -Verify {
        param($r) $left = Count-Files $wipeDir; if ($left -ne 4) { "$left of 4 files left - a wipe that was not confirmed deleted data" }
    })
    [void](Op 'wipe: --force on a plain folder' @('folder', $wipeDir, 'wipe', '--force') -Verify {
        param($r)
        if (Count-Files $wipeDir) { "$(Count-Files $wipeDir) file(s) left" }
        if (-not (Test-Path -LiteralPath $wipeDir)) { 'the folder itself is gone (it must stay)' }
    })

    # -- a batch run: the same dispatch with its own tokenizer and stop rule
    $dstBatch = Join-Path $Work 'dst-batch'
    $list = Join-Path $Work 'ops.lst'
    Set-Content -LiteralPath $list -Encoding utf8NoBOM -Value @(
        '# the batch path: comments, quotes, several verbs',
        "folder `"$src`" info", "check `"$src`"", "copy `"$src`" `"$dstBatch`"", "compare `"$src`" `"$dstBatch`"")
    [void](Op 'batch: from a list' @('from', $list) -Bytes $nBytes -Verify {
        param($r) Compare-Manifest $want (Get-Manifest $dstBatch)
    })
    Remove-Scratch $dstBatch

    # -- the network target: this machine's own admin share, when this account may reach it
    $drive = Split-Path $Work -Qualifier
    $unc = '\\localhost\' + $drive.Substring(0, 1) + '$' + $Work.Substring(2)
    $reach = $false
    try { $reach = (Test-Path -LiteralPath (Join-Path $unc 'src')) } catch { }
    if ($reach) {
        $uncSrc = Join-Path $unc 'src'
        [void](Op 'network: info' @($uncSrc, 'info') -Verify {
            param($r)
            if ($r.Text -notmatch 'network path') { 'info does not call it a network path' }
            if ($r.Text -notmatch "Full Contains:\s+$nFiles files") { "info does not count $nFiles files" }
        })
        [void](Op 'network: speed 2 MB' @($uncSrc, 'speed', '2') -Bytes (4 * $MiB) -Verify {
            param($r)
            if (-not ((N $r 'uploadMBps') -gt 0 -and (N $r 'downloadMBps') -gt 0)) { 'a speed is not positive' }
            if (@(Get-ChildItem -LiteralPath (Join-Path $Work 'src') -Filter 'speedtest*').Count) { 'a speedtest file was left on the share' }
        })
        $dstNet = Join-Path $Work 'dst-net'
        [void](Op 'network: copy onto a share' @('copy', $src, (Join-Path $unc 'dst-net')) -Bytes $nBytes -Verify {
            param($r) Test-CopyNumbers $r $nFiles $nBytes; Compare-Manifest $want (Get-Manifest $dstNet)
        })
    [void](Op 'network: compare against the share' @('compare', $src, (Join-Path $unc 'dst-net')) -Verify {
        param($r) if ((N $r 'totalTarget') -ne $nFiles) { "totalTarget = $(N $r 'totalTarget'), want $nFiles" }
        })
        Remove-Scratch $dstNet
    } else {
        $script:Skipped.Add("network: the admin share $unc is not reachable by this account - the network target was not exercised")
        $script:SkippedPrefix.Add('network:')
    }

    # -- virtual disks that need no elevation
    $fdd = Join-Path $Work 'v1.fdd'
    [void](Op 'vd: new 64M' @('vd', 'new', $fdd, '64M') -Verify { param($r) if (-not (Test-Path $fdd)) { 'no container file' } })
    [void](Op 'vd: info' @($fdd, 'info') -Verdict @('Passed') -Verify { param($r) if ($r.Text -notmatch '64 MiB') { 'info does not name the 64 MiB volume' } })
    [void](Op 'vd: verify' @($fdd, 'verify') -Verdict @('Passed') -Verify {
        param($r) if ((N $r 'problems') -ne 0) { "problems = $(N $r 'problems'), want 0" }
    })
    $img = Join-Path $Work 'v1.img'
    [void](Op 'vd: export raw' @($fdd, 'export', $img, 'raw') -Bytes (64 * $MiB) -Verify {
        param($r) if (-not (Test-Path $img) -or (Get-Item $img).Length -ne 64 * $MiB) { 'the raw image is not exactly 64 MiB' }
    })
    Remove-Scratch $img
    [void](Op 'vd: add a name' @('vd', 'add', $fdd, 'as', 'opsv') -Verify { param($r) if ($r.Text -notmatch 'Registered') { 'no "Registered" message' } })
    [void](Op 'vd: list shows it' @('vd', 'list') -Verdict @('Passed') -Verify { param($r) if ($r.Text -notmatch 'opsv') { 'the name is not listed' } })
    [void](Op 'vd: forget the name' @('vd', 'forget', 'opsv') -Verify {
        param($r) if (-not (Test-Path $fdd)) { 'forget removed the container file (it must not)' }
    })
    [void](Op 'vd: list without it' @('vd', 'list') -Verdict @('Passed') -Verify { param($r) if ($r.Text -match 'opsv') { 'the forgotten name is still listed' } })
    [void](Op 'vd: status' @('vd', 'status') -Verdict @('Passed', 'Done'))
    # SP-0148: the disk list reads every disk with access 0 and changes nothing.
    [void](Op 'vd: disks' @('vd', 'disks') -Verdict @('Passed', 'Done') -Verify { param($r) if ($r.Text -notmatch '(?m)^Disk \d+ ') { 'no disk was listed' } })
    $vault = Join-Path $Work 'vault1.fdd'
    [void](Op 'vd: new, a vault' @('vd', 'new', $vault, '32M', 'vault', 'p:Right-Pass1') -Verify { param($r) if ($r.Text -notmatch 'encrypted') { 'the vault is not said to be encrypted' } })
    [void](Op 'vd: verify, a wrong password refused' @($vault, 'verify', 'p:wrong-pass') -Code @(3) -Verdict @('Failed'))
    [void](Op 'vd: verify, the right password' @($vault, 'verify', 'p:Right-Pass1') -Verdict @('Passed') -Verify {
        param($r) if ((N $r 'problems') -ne 0) { "problems = $(N $r 'problems'), want 0" }
    })
    $vb = Join-Path $Work 'vdbatch'; [void][IO.Directory]::CreateDirectory($vb)
    Set-Content -LiteralPath (Join-Path $vb 'vdb_new.txt') -Value 'batch-new-pass' -NoNewline
    $env:FILEDO_VDB_OLD = 'batch-old-pass'
    try {
        $saved = $script:Work; $script:Work = $vb
        [void](Op 'vd: every verb of tests\vd_batch.lst' @('from', (Join-Path $PSScriptRoot 'vd_batch.lst')) -Budget 240 -Verdict @('Passed', 'Done'))
    } finally { $script:Work = $saved; Remove-Item Env:FILEDO_VDB_OLD -ErrorAction SilentlyContinue }
}

# ---- the load section: sizes at which a loss of throughput shows ---------------------------------------

function Invoke-Load {
    $script:Tier = 'load'
    $env:FILEDO_STATE_DIR = Join-Path $Work 'state-load'
    $free = ([IO.DriveInfo]::new((Split-Path $Work -Qualifier))).AvailableFreeSpace
    if ($free -lt $Load.MinFreeGiB * 1GB) {
        $script:Unverified.Add(("load section: {0:N1} GiB free on the temp drive, {1} GiB needed for the load plan" -f ($free / 1GB), $Load.MinFreeGiB))
        return
    }
    Write-Host "`n== Load: operations sized to run for seconds ==" -ForegroundColor Cyan
    # Process start-up is the one thing every step pays, and on a 40 ms step it is most of the time. The best
    # of seven runs is the figure noise cannot inflate (it only ever adds), so it is what gates a start-up
    # regression; the small steps of tier 1 are too noisy to.
    $startup = @(foreach ($i in 1..7) { Invoke-Filedo -Name 'startup' -Arguments @('-?') -Budget 30 -Events $false })
    $bad = @($startup | Where-Object { $_.Code -ne 0 -or $_.TimedOut })
    Add-Row -Name 'startup: filedo -? (best of 7)' -Ms ([int](($startup | Measure-Object Ms -Minimum).Minimum)) -MBps $null -Note ('the others: ' + ((@($startup | ForEach-Object Ms | Sort-Object) | Select-Object -Skip 1) -join ' ')) `
        -Problems @($(if ($bad.Count) { "$($bad.Count) of 7 runs did not exit 0 in time" })) -Code 0 -Verdict '' -Sig ''

    $root = Join-Path $Work 'load'
    $sw = [Diagnostics.Stopwatch]::StartNew()
    $filesDir = Join-Path $root 'files'; New-SmallFiles $filesDir $Load.CopyFiles 1024 3072 100 101
    # Two trees of tiny files: check keeps a good list, and its first run over a tree costs four times what a
    # repeat does, so each of the two modes gets a tree of its own to be a first run on.
    $manyDir = Join-Path $root 'many'; $manyA = Join-Path $manyDir 'a'; $manyB = Join-Path $manyDir 'b'
    New-SmallFiles $manyA $Load.ManyFiles 256 512 100 102; New-SmallFiles $manyB $Load.ManyFiles 256 512 100 104
    # Real duplicates, so `cd` has to read and hash: one small file six times, one 64 KiB file twice.
    foreach ($i in 1..5) { [IO.File]::Copy((Join-Path $manyA 'd000\f00000.dat'), (Join-Path $manyA "d000\dup$i.dat")) }
    $pb = New-Object byte[] (64KB); ([Random]::new(9)).NextBytes($pb)
    [IO.File]::WriteAllBytes((Join-Path $manyA 'pair1.bin'), $pb); [IO.File]::WriteAllBytes((Join-Path $manyA 'pair2.bin'), $pb)
    $bigDir = Join-Path $root 'big'; [void][IO.Directory]::CreateDirectory($bigDir)
    foreach ($i in 1..$Load.BigFiles) { New-BigFile (Join-Path $bigDir "b$i.bin") $Load.BigMiB (200 + $i) }
    $wantFiles = Get-Manifest $filesDir; $wantMany = Get-Manifest $manyDir; $wantBig = Get-Manifest $bigDir   # reading them also lets Defender scan once, before anything is timed
    $filesBytes = ($wantFiles.Values | Measure-Object Size -Sum).Sum; $bigBytes = ($wantBig.Values | Measure-Object Size -Sum).Sum
    Write-Host ("  fixtures in {0:N1} s: {1} copy files ({2:N1} MiB), {3} many-files ({4:N1} MiB), {5} big files ({6:N0} MiB)" -f $sw.Elapsed.TotalSeconds,
        $wantFiles.Count, ($filesBytes / $MiB), $wantMany.Count, (($wantMany.Values | Measure-Object Size -Sum).Sum / $MiB), $wantBig.Count, ($bigBytes / $MiB))
    $nFiles = $wantFiles.Count; $nMany = $wantMany.Count; $nB = $Load.ManyFiles; $nA = $nMany - $nB

    # -- the copy modes on a tree of many small files: per-file cost is what they differ in
    foreach ($mode in 'copy', 'fastcopy', 'balanced', 'maxcopy', 'smartcopy', 'safecopy', 'synccopy') {
        $dst = Join-Path $root "dst-$mode"
        [void](Op "load: $mode" @($mode, $filesDir, $dst) -Files $nFiles -Verify {
            param($r) Test-CopyNumbers $r $nFiles $filesBytes; Compare-Manifest $wantFiles (Get-Manifest $dst)
        })
        if ($mode -ne 'copy') { Remove-Scratch $dst }
    }
    $dstCopy = Join-Path $root 'dst-copy'
    [void](Op 'load: copy again, all there' @('copy', $filesDir, $dstCopy) -Files $nFiles -Verify {
        param($r)
        if ((N $r 'alreadyThereFiles') -ne $nFiles) { "alreadyThereFiles = $(N $r 'alreadyThereFiles'), want $nFiles" }
        if ((N $r 'copiedFiles') -ne 0) { "copiedFiles = $(N $r 'copiedFiles'), want 0" }
    })
    [void](Op 'load: compare' @('compare', $filesDir, $dstCopy) -Files $nFiles -Verify {
        param($r)
        if ((N $r 'totalSource') -ne $nFiles -or (N $r 'totalTarget') -ne $nFiles) { "the compare totals are not $nFiles" }
        if ((N $r 'onlyInSource') -ne 0) { "onlyInSource = $(N $r 'onlyInSource'), want 0" }
    })
    Remove-Scratch $dstCopy

    # -- cmp .. del over identical pairs: the safety checks run per file, so this is where per-file cost shows worst
    $cmpA = Join-Path $root 'cmp-a'; $cmpB = Join-Path $root 'cmp-b'
    foreach ($d in $cmpA, $cmpB) { [void][IO.Directory]::CreateDirectory($d) }
    $made = 0
    foreach ($f in (Get-ChildItem -LiteralPath $filesDir -Recurse -File | Select-Object -First $Load.CmpFiles)) {
        [IO.File]::Copy($f.FullName, (Join-Path $cmpA $f.Name)); [IO.File]::Copy($f.FullName, (Join-Path $cmpB $f.Name)); $made++
    }
    $wantCmp = Get-Manifest $cmpB; [void](Get-Manifest $cmpA)
    [void](Op 'load: cmp .. del source' @('cmp', $cmpA, $cmpB, 'del', 'source', '-y') -Files $made -Verify {
        param($r)
        if (Count-Files $cmpA) { "$(Count-Files $cmpA) file(s) still in the source of the pair" }
        Compare-Manifest $wantCmp (Get-Manifest $cmpB)
    })
    Remove-Scratch $cmpA; Remove-Scratch $cmpB

    # -- check, cd and wipe over many tiny files
    [void](Op 'load: check' @('check', $manyA) -Files $nA -Verify {
        param($r)
        if ((N $r 'checkedFiles') -ne $nA) { "checkedFiles = $(N $r 'checkedFiles'), want $nA" }
        if ((N $r 'damagedFiles') -ne 0) { "damagedFiles = $(N $r 'damagedFiles'), want 0" }
    })
    [void](Op 'load: check, deep mode' @('check', $manyB, '--mode', 'deep') -Files $nB -Verify {
        param($r) if ((N $r 'checkedFiles') -ne $nB) { "checkedFiles = $(N $r 'checkedFiles'), want $nB" }
    })
    [void](Op 'load: cd' @($manyDir, 'cd', 'short') -Files $nMany -Verify {
        param($r)
        if ((N $r 'filesScanned') -ne $nMany) { "filesScanned = $(N $r 'filesScanned'), want $nMany" }
        if ((N $r 'duplicateGroups') -ne 2) { "duplicateGroups = $(N $r 'duplicateGroups'), want 2 (the six small files and the 64 KiB pair)" }
        if ((N $r 'duplicateFiles') -ne 6) { "duplicateFiles = $(N $r 'duplicateFiles'), want 6" }
    })
    [void](Op 'load: wipe the many-file tree' @('folder', $manyDir, 'wipe', '--force') -Files $nMany -Verify {
        param($r)
        if (Count-Files $manyDir) { "$(Count-Files $manyDir) file(s) left" }
        if (-not (Test-Path -LiteralPath $manyDir)) { 'the folder itself is gone (it must stay)' }
    })
    Remove-Scratch $manyDir

    # -- bytes: the containers and the copy of big files
    $dstBig = Join-Path $root 'dst-big'
    [void](Op 'load: copy, big files' @('copy', $bigDir, $dstBig) -Bytes $bigBytes -Verify { param($r) Test-CopyNumbers $r $Load.BigFiles $bigBytes })
    Remove-Scratch $dstBig
    $pw = 'p:Pa55word'
    $sealed = Join-Path $root 'big.fd-sec'
    [void](Op 'load: secure the big tree' @($bigDir, 'secure', $pw) -Bytes $bigBytes -Verify { param($r) if (-not (Test-Path $sealed)) { 'no container written' } })
    [void](Op 'load: fdsec verify' @('fdsec', 'verify', $sealed, $pw) -Verdict @('Passed') -Bytes $bigBytes)
    $back = Join-Path $root 'big-back'
    [void](Op 'load: unsecure the big tree' @($sealed, 'unsecure', 'to', $back, $pw) -Bytes $bigBytes -Verify {
        param($r) Compare-Manifest $wantBig (Get-Manifest $back)
    })
    Remove-Scratch $back; Remove-Scratch $sealed; Remove-Scratch $bigDir
    # suite 2 is one file, with a memory-hard key step: its fixed cost and its per-byte cost both show
    $one = Join-Path $root 'one.bin'
    $fs = [IO.File]::Create($one); try { $b1 = New-Object byte[] (1 * $MiB); $r1 = [Random]::new(77); for ($i = 0; $i -lt $Load.Suite2MiB; $i++) { $r1.NextBytes($b1); $fs.Write($b1, 0, $b1.Length) } } finally { $fs.Dispose() }
    $oneHash = Get-FileSha256 $one
    [void](Op 'load: secure, suite 2' @($one, 'secure', $pw, 'suite2') -Bytes ($Load.Suite2MiB * $MiB))
    $oneOut = Join-Path $root 'one-out.bin'
    [void](Op 'load: unsecure, suite 2' @((Join-Path $root 'one.fd-sec'), 'unsecure', 'to', $oneOut, $pw) -Bytes ($Load.Suite2MiB * $MiB) -Verify {
        param($r) if (-not (Test-Path $oneOut) -or (Get-FileSha256 $oneOut) -ne $oneHash) { 'the restored file differs' }
    })
    Remove-Scratch $one; Remove-Scratch $oneOut; Remove-Scratch (Join-Path $root 'one.fd-sec')
    [void](Op "load: speed $($Load.SpeedMB) MB" @('folder', $Work, 'speed', "$($Load.SpeedMB)") -Bytes (2L * $Load.SpeedMB * $MiB) -Verify {
        param($r)
        if (-not ((N $r 'uploadMBps') -gt 0 -and (N $r 'downloadMBps') -gt 0)) { 'a speed is not positive' }
        Note ('up {0:N0} / down {1:N0} MB/s' -f (N $r 'uploadMBps'), (N $r 'downloadMBps'))
        if (@(Get-ChildItem -LiteralPath $Work -Filter 'speedtest*').Count) { 'a speedtest file was left behind' }
    })

    # -- a virtual-disk container of a gigabyte, not mounted: allocation, a clone, a raw export
    $vd = Join-Path $root 'v.fdd'; $vd2 = Join-Path $root 'v2.fdd'; $img = Join-Path $root 'v.img'
    $vdBytes = [long]$Load.VdMiB * $MiB
    [void](Op 'load: vd new, fast (all allocated)' @('vd', 'new', $vd, "$($Load.VdMiB)M", 'fast') -Bytes $vdBytes -Verify { param($r) if (-not (Test-Path $vd)) { 'no container file' } })
    [void](Op 'load: vd verify' @($vd, 'verify') -Verdict @('Passed') -Verify { param($r) if ((N $r 'problems') -ne 0) { "problems = $(N $r 'problems'), want 0" } })
    [void](Op 'load: vd clone' @($vd, 'clone', $vd2) -Bytes $vdBytes -Verify { param($r) if (-not (Test-Path $vd2)) { 'no clone written' } })
    [void](Op 'load: vd export raw' @($vd, 'export', $img, 'raw') -Bytes $vdBytes -Verify {
        param($r) if (-not (Test-Path $img) -or (Get-Item $img).Length -ne $vdBytes) { "the raw image is not exactly $($Load.VdMiB) MiB" }
    })
    Remove-Scratch $img; Remove-Scratch $vd; Remove-Scratch $vd2
    Remove-Scratch $root
    $script:Tier = '1'
}

# ---- tier 2: small mounted virtual disks (elevated) ---------------------------------------------

function Get-MountLetter([string]$fdd) {
    $r = Invoke-Filedo -Name 'vd-status' -Arguments @('vd', 'status', 'json') -Budget 30 -Events $false -RealState $true
    $a = $r.Text.IndexOf('{'); $b = $r.Text.LastIndexOf('}')
    if ($a -lt 0 -or $b -le $a) { return $null }
    $doc = $r.Text.Substring($a, $b - $a + 1) | ConvertFrom-Json
    $want = [IO.Path]::GetFullPath($fdd)
    foreach ($d in @($doc.disks)) {
        if ($d.PSObject.Properties['path'] -and [string]::Equals([IO.Path]::GetFullPath($d.path), $want, 'OrdinalIgnoreCase') -and $d.PSObject.Properties['mount'] -and $d.mount) { return [string]$d.mount.letter }
    }
    $null
}
function Wait-Volume([string]$letter, [int]$seconds = 30) {
    $end = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $end) { if (([IO.DriveInfo]::new($letter)).IsReady) { return $true }; Start-Sleep -Milliseconds 400 }
    $false
}
function Wait-NoLetter([string]$fdd, [int]$seconds = 20) {
    $end = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $end) { if (-not (Get-MountLetter $fdd)) { return $true }; Start-Sleep -Milliseconds 500 }
    $false
}
# Mount a scratch container and return its letter - or $null, with a failed row, when the letter is not
# provably this run's own: it must be new since before the first mount, and never the system drive.
function Mount-Own([string]$fdd, [string]$label, [string[]]$extra = @()) {
    [void](Op "mount: $label" (@($fdd, 'mount') + $extra) -Budget 180 -RealState)
    $l = Get-MountLetter $fdd
    if (-not $l) { Check "mount: $label has a drive letter" @('vd status json lists no mount for the scratch container'); return $null }
    $l = $l.ToUpperInvariant()
    $unsafe = @()
    if ($l -ieq $env:SystemDrive) { $unsafe += "$l is the system drive" }
    if ($script:DrivesBefore -contains $l) { $unsafe += "$l existed before the first mount - not this run's disk" }
    if ($unsafe) { Check "mount: $label - the letter is the new disk" $unsafe; return $null }
    if (-not (Wait-Volume $l)) { Check "mount: $label - the volume is ready" @("$l never became ready"); return $null }
    $l
}
function Dismount-Own([string]$letter, [string]$fdd, [string]$label) {
    [void](Op "unmount: $label" @($letter, 'unmount') -Budget 120 -RealState)
    if (-not (Wait-NoLetter $fdd)) { Check "unmount: $label - the disk is gone" @("$letter is still mounted") }
}

function Invoke-Tier2 {
    $script:Tier = '2'
    Write-Host "`n== Tier 2: mounted virtual disks ($Work) ==" -ForegroundColor Cyan
    $src = Join-Path $Work 'src'
    [void](New-SourceTree $src)
    # More for the disk to hold: 2000 small files and one 96 MiB file, so the copy, the verify after use and the
    # read-only remount have bytes to move (the disk is 320 MB; this is a third of it).
    New-SmallFiles (Join-Path $src 'many') 2000 1024 3072 100 103
    New-BigFile (Join-Path $src 'big96.bin') 96 301
    $want = Get-Manifest $src
    $nBytes = ($want.Values | Measure-Object Size -Sum).Sum
    $nReadable = @($want.Values | Where-Object { $_.Size -gt 0 }).Count
    $fdd = Join-Path $Work 'mnt.fdd'
    $fddVault = Join-Path $Work 'mnt-vault.fdd'
    $fddRam = Join-Path $Work 'mnt-ram.fdd'
    $volume = 320 * $MiB
    $env:FILEDO_STATE_DIR = Join-Path $Work 'state'   # check and the rest keep their lists out of yours; vd verbs say -RealState

    $status = Invoke-Filedo -Name 'vd-status-pre' -Arguments @('vd', 'status', 'json') -Budget 30 -Events $false -RealState $true
    if ($status.Text -notmatch '"ready"\s*:\s*true') {
        $script:Unverified.Add('mount tier: the block-server transport is not ready (see vd status)')
        Write-Host '  the mount transport is not ready - tier 2 cannot run' -ForegroundColor Yellow
        return
    }
    $script:DrivesBefore = @([IO.DriveInfo]::GetDrives() | ForEach-Object { $_.Name.Substring(0, 2).ToUpperInvariant() })
    try {
        # ---- the 320 MB plain disk (the capacity test needs a budget of 100 MB or more; at this size it, and fill, run for seconds)
        [void](Op 'vd: new, the plain disk' @('vd', 'new', $fdd, '320M') -RealState)
        $letter = Mount-Own $fdd 'first mount formats NTFS'
        if (-not $letter) { return }
        $drive = [IO.DriveInfo]::new($letter)
        Check "mount: $letter is a new drive of the right size" @(
            $(if ($drive.TotalSize -gt $volume -or $drive.TotalSize -lt 0.8 * $volume) { "TotalSize $($drive.TotalSize) is not within 80..100 % of $volume" })
            $(if ($drive.DriveFormat -ne 'NTFS') { "formatted $($drive.DriveFormat), want NTFS" }))
        $freeStart = $drive.AvailableFreeSpace
        Write-Host ("  {0} mounted: {1:N0} MiB total, {2:N0} MiB free" -f $letter, ($drive.TotalSize / $MiB), ($freeStart / $MiB))

        [void](Op 'device: info' @('device', $letter, 'info') -Verify { param($r) if ($r.Text -notmatch 'NTFS') { 'info does not name NTFS' } })
        [void](Op 'device: speed 16 MB' @('device', $letter, 'speed', '16') -Bytes (32 * $MiB) -Verify {
            param($r)
            if (-not ((N $r 'uploadMBps') -gt 0 -and (N $r 'downloadMBps') -gt 0)) { 'a speed is not positive' }
            Note ('up {0:N0} / down {1:N0} MB/s' -f (N $r 'uploadMBps'), (N $r 'downloadMBps'))
            if (@(Get-ChildItem -LiteralPath "$letter\" -Filter 'speedtest*' -Force).Count) { 'a speedtest file was left behind' }
        })
        [void](Op 'device: capacity test (del)' @('device', $letter, 'test', 'del') -Budget 300 -Verdict @('Passed') -Verify {
            param($r)
            $written = N $r 'bytesWritten'
            if (-not ($written -ge 100 * $MiB)) { "bytesWritten = $written, want at least 100 MiB (the test's own minimum)" }
            if ($written -gt $freeStart) { "bytesWritten $written is more than the $freeStart bytes that were free" }
            Note ('{0:N0} MiB over {1} files, avg {2:N0} MB/s' -f ($written / $MiB), (N $r 'filesWritten'), (N $r 'averageSpeedMBps'))
            $freeNow = ([IO.DriveInfo]::new($letter)).AvailableFreeSpace
            if ([math]::Abs($freeNow - $freeStart) -gt 2 * $MiB) { "free space is $freeNow after the test, was $freeStart - test files were left" }
        })
        [void](Op 'device: fill, 1 MB files' @('device', $letter, 'fill', '1') -Budget 300 -Verify {
            param($r)
            $created = N $r 'filesCreated'; $script:filled = $created
            if (-not ($created -ge 100)) { "filesCreated = $created, want 100 or more on a 320 MiB disk" }
            if ((N $r 'bytesWritten') -ne $created * $MiB) { "bytesWritten = $(N $r 'bytesWritten') for $created files of 1 MiB" }
            $freeNow = ([IO.DriveInfo]::new($letter)).AvailableFreeSpace
            if ($freeNow -gt 0.1 * $volume) { "$([int]($freeNow / $MiB)) MiB still free after a fill" }
            Note ("$created files, {0:N0} MiB" -f ((N $r 'bytesWritten') / $MiB))
        })
        [void](Op 'device: fill verify' @('device', $letter, 'fill', 'verify') -Budget 300 -Verify {
            param($r)
            if ((N $r 'filesChecked') -ne $script:filled) { "filesChecked = $(N $r 'filesChecked'), want $($script:filled)" }
            if ((N $r 'headersWrong') -ne 0) { "headersWrong = $(N $r 'headersWrong')" }
            if ((N $r 'unreadable') -ne 0) { "unreadable = $(N $r 'unreadable')" }
        })
        [void](Op 'device: clean the fill files' @('device', $letter, 'clean', '--yes') -Budget 120 -Verify {
            param($r)
            if ((N $r 'filesDeleted') -ne $script:filled) { "filesDeleted = $(N $r 'filesDeleted'), want $($script:filled)" }
            $freeNow = ([IO.DriveInfo]::new($letter)).AvailableFreeSpace
            if ([math]::Abs($freeNow - $freeStart) -gt 2 * $MiB) { "free space is $freeNow after clean, was $freeStart" }
        })

        $data = "$letter\data"
        [void](Op 'fastcopy: tree onto the disk' @('fastcopy', $src, $data) -Bytes $nBytes -Verify {
            param($r) Test-CopyNumbers $r $want.Count $nBytes; Compare-Manifest $want (Get-Manifest $data)
        })
        [void](Op 'check: the copied tree' @('check', $data) -Verify {
            param($r) if ((N $r 'checkedFiles') -ne $nReadable) { "checkedFiles = $(N $r 'checkedFiles'), want $nReadable" }; if ((N $r 'damagedFiles') -ne 0) { 'damaged files reported' }
        })
        [void](Op 'cd: duplicates on the disk' @($data, 'cd', 'short') -Verify {
            param($r) if ((N $r 'duplicateGroups') -ne 1 -or (N $r 'duplicateFiles') -ne 2) { "groups/files = $(N $r 'duplicateGroups')/$(N $r 'duplicateFiles'), want 1/2" }
        })
        [void](Op 'compare: source against the disk' @('compare', $src, $data) -Verify {
            param($r)
            if ((N $r 'totalTarget') -ne $want.Count) { "totalTarget = $(N $r 'totalTarget'), want $($want.Count)" }
            if ((N $r 'onlyInSource') -ne 0) { "onlyInSource = $(N $r 'onlyInSource'), want 0" }
        })

        # probe writes raw sectors and restores them; recover runs chkdsk. Neither may touch the data.
        [void](Op 'probe: refused without the typed word' @('device', $letter, 'probe') -Code @(2) -Verdict @('Not proven') -Verify {
            param($r) Compare-Manifest $want (Get-Manifest $data)
        })
        [void](Op 'probe: yes, the sectors are restored' @('device', $letter, 'probe', 'yes') -Budget 300 -Verdict @('Passed') -Verify {
            param($r)
            if ($r.Text -notmatch 'GENUINE') { 'the probe did not call the disk genuine' }
            if ($r.Text -notmatch 'written sectors restored') { 'no "sectors restored" line' }
            Compare-Manifest $want (Get-Manifest $data)
        })
        [void](Op 'recover: chkdsk leaves the data alone' @('device', $letter, 'recover', 'yes') -Budget 300 -Verify {
            param($r)
            if ($r.Text -notmatch 'accessible after chkdsk') { 'the drive is not reported accessible after chkdsk' }
            if (-not ([IO.DriveInfo]::new($letter)).IsReady) { "$letter is not ready after chkdsk" } else { Compare-Manifest $want (Get-Manifest $data) }
        })

        $scrap = "$letter\scrap"
        [void](Op 'fastcopy: a small scrap tree' @('fastcopy', (Join-Path $src 'dups'), $scrap))
        [void](Op 'wipe: folder on the disk, --force' @('folder', $scrap, 'wipe', '--force') -Verify {
            param($r) if (Count-Files $scrap) { 'files left in the wiped folder' }; if (-not (Test-Path -LiteralPath $scrap)) { 'the folder itself is gone' }
        })
        Set-Content -LiteralPath "$letter\canary.txt" -Value 'must survive an unconfirmed wipe'
        [void](Op 'wipe: a drive root is refused, --force or not' @('device', $letter, 'wipe', '--force') -Code @(1, 2) -Verdict @('Not proven', 'Failed') -Verify {
            param($r) if (-not (Test-Path -LiteralPath "$letter\canary.txt")) { 'the root wipe ran without the typed confirmation' }
        })
        Remove-Item -LiteralPath "$letter\canary.txt" -Force

        # unmount, prove the container, come back read-only
        Dismount-Own $letter $fdd 'plain disk'
        $letter = $null
        [void](Op 'vd: verify after use' @($fdd, 'verify') -Verdict @('Passed') -Budget 120 -RealState -Verify {
            param($r)
            if ((N $r 'problems') -ne 0) { "problems = $(N $r 'problems'), want 0" }
            if (-not ((N $r 'clusters_allocated') -gt 0)) { 'no cluster was allocated by the data that was written' }
        })
        [void](Op 'vd: info says closed clean' @($fdd, 'info') -Verdict @('Passed') -RealState -Verify {
            param($r) if ($r.Text -notmatch 'Closed clean:\s+yes') { 'the container is not marked closed clean after the unmount' }
        })
        $letter = Mount-Own $fdd 'read-only' @('ro')
        if (-not $letter) { return }
        Check 'the data survived the unmount (byte for byte)' @(Compare-Manifest $want (Get-Manifest "$letter\data"))
        $denied = "$letter\denied"
        [void](Op 'write to a read-only disk is refused' @('copy', (Join-Path $src 'dups'), $denied) -Code @(1, 2, 5) -Verdict @('Not proven', 'Failed') -Verify {
            param($r) if (Test-Path -LiteralPath $denied) { 'a folder was created on a read-only mount' }
        })
        Dismount-Own $letter $fdd 'read-only'
        $letter = $null

        # chkdsk on the volume of a disk at rest: the scan writes nothing and says so, the repair of a
        # clean volume finds nothing to repair, and the data is the same after both
        [void](Op 'vd: chkdsk scan' @($fdd, 'chkdsk') -Budget 300 -RealState -Verify {
            param($r) if ($r.Text -notmatch 'found no problems') { 'the scan of a clean volume did not say "found no problems"' }
        })
        [void](Op 'vd: chkdsk fix' @($fdd, 'chkdsk', 'fix', 'force') -Budget 300 -RealState -Verify {
            param($r) if ($r.Text -notmatch 'found no problems|repaired them') { 'the repair did not say what chkdsk found' }
        })
        $letter = Mount-Own $fdd 'after chkdsk'
        if (-not $letter) { return }
        Check 'the data survived chkdsk (byte for byte)' @(Compare-Manifest $want (Get-Manifest "$letter\data"))
        Dismount-Own $letter $fdd 'after chkdsk'
        $letter = $null

        # clean the whole volume and check its size again
        [void](Op 'vd: format (empty NTFS again)' @($fdd, 'format', 'force') -Budget 180 -RealState)
        $letter = Mount-Own $fdd 'after format'
        if (-not $letter) { return }
        $drive = [IO.DriveInfo]::new($letter)
        $left = @(Get-ChildItem -LiteralPath "$letter\" -Force | Where-Object { $_.Name -notin 'System Volume Information', '$RECYCLE.BIN' })
        Check 'format: the volume is empty and still the same size' @(
            $(if ($left.Count) { "$($left.Count) item(s) survived the format: $(($left | ForEach-Object Name) -join ', ')" })
            $(if ($drive.TotalSize -gt $volume -or $drive.TotalSize -lt 0.8 * $volume) { "TotalSize $($drive.TotalSize) is not within 80..100 % of $volume" })
            $(if ([math]::Abs($drive.AvailableFreeSpace - $freeStart) -gt 4 * $MiB) { "free $($drive.AvailableFreeSpace) after the format, was $freeStart on the first mount" }))
        [void](Op 'device: capacity test on the empty disk' @('device', $letter, 'test', 'del') -Budget 300 -Verdict @('Passed'))
        Dismount-Own $letter $fdd 'after format'
        $letter = $null
        [void](Op 'vd: destroy' @($fdd, 'destroy', 'force') -RealState -Verify { param($r) if (Test-Path $fdd) { 'the container file is still there' } })

        # ---- an encrypted vault: the data comes back under the right password and under no other
        $vw = Get-Manifest (Join-Path $src 'a')
        [void](Op 'vault: new 64M' @('vd', 'new', $fddVault, '64M', 'vault', 'p:Right-Pass1') -RealState)
        $letter = Mount-Own $fddVault 'vault' @('p:Right-Pass1')
        if (-not $letter) { return }
        $vdata = "$letter\a"
        [void](Op 'vault: copy a tree onto it' @('fastcopy', (Join-Path $src 'a'), $vdata) -Verify {
            param($r) Test-CopyNumbers $r $vw.Count (($vw.Values | Measure-Object Size -Sum).Sum); Compare-Manifest $vw (Get-Manifest $vdata)
        })
        Dismount-Own $letter $fddVault 'vault'
        $letter = $null
        [void](Op 'vault: mount refused under a wrong password' @($fddVault, 'mount', 'p:wrong-pass') -Code @(3) -Verdict @('Failed') -RealState -Verify {
            param($r) if (Get-MountLetter $fddVault) { 'the vault got a drive letter under a wrong password' }
        })
        $letter = Mount-Own $fddVault 'vault again' @('p:Right-Pass1')
        if (-not $letter) { return }
        Check 'vault: the data is back under the right password' @(Compare-Manifest $vw (Get-Manifest "$letter\a"))
        Dismount-Own $letter $fddVault 'vault again'
        $letter = $null
        [void](Op 'vault: verify after use' @($fddVault, 'verify', 'p:Right-Pass1') -Verdict @('Passed') -RealState -Verify {
            param($r) if ((N $r 'problems') -ne 0) { "problems = $(N $r 'problems'), want 0" }
        })
        [void](Op 'vault: destroy' @($fddVault, 'destroy', 'force') -RealState -Verify { param($r) if (Test-Path $fddVault) { 'the container file is still there' } })

        # ---- a ram disk: the volume lives in memory until it is saved
        $rw = Get-Manifest (Join-Path $src 'dups')
        [void](Op 'ram: new 96M' @('vd', 'new', $fddRam, '96M', 'ram') -RealState)
        $letter = Mount-Own $fddRam 'ram'
        if (-not $letter) { return }
        $rdata = "$letter\dups"
        [void](Op 'ram: copy a tree onto it' @('fastcopy', (Join-Path $src 'dups'), $rdata) -Verify {
            param($r) Compare-Manifest $rw (Get-Manifest $rdata)
        })
        [void](Op 'ram: save to the file' @($letter, 'save') -RealState -Verify { param($r) if ($r.Text -notmatch 'Saved') { 'no "Saved" message' } })
        Dismount-Own $letter $fddRam 'ram'
        $letter = $null
        [void](Op 'ram: info says closed clean' @($fddRam, 'info') -Verdict @('Passed') -RealState -Verify {
            param($r) if ($r.Text -notmatch 'Closed clean:\s+yes') { 'the ram container is not closed clean after the unmount' }
        })
        $letter = Mount-Own $fddRam 'ram again'
        if (-not $letter) { return }
        Check 'ram: the saved data is back after a remount' @(Compare-Manifest $rw (Get-Manifest "$letter\dups"))
        Dismount-Own $letter $fddRam 'ram again'
        $letter = $null
        [void](Op 'ram: destroy' @($fddRam, 'destroy', 'force') -RealState -Verify { param($r) if (Test-Path $fddRam) { 'the container file is still there' } })
    } finally {
        # Whatever happened, no scratch disk is left mounted. Only a letter this run saw for its own containers.
        foreach ($c in @($fdd, $fddVault, $fddRam)) {
            $left = Get-MountLetter $c
            if ($left) {
                Write-Host "  cleanup: $left is still mounted - detaching it" -ForegroundColor Yellow
                [void](Invoke-Filedo -Name 'cleanup-unmount' -Arguments @($left.ToUpperInvariant(), 'unmount', 'force') -Budget 120 -RealState $true)
            }
        }
    }
}

# ---- the inventory: every verb the help lists is a step, a batch line or an exemption with a reason ----

function E([string]$key, [string]$step = '', [string]$tier = '1', [string]$batch = '', [string]$exempt = '', [switch]$optional) {
    [pscustomobject]@{ Key = $key; Step = $step; Tier = $tier; Batch = $batch; Exempt = $exempt; Optional = $optional.IsPresent }
}
$Inventory = @(
    E 'info' '^info: '; E 'speed' '^speed: '; E 'clean' '^clean: '
    E 'test' '^device: capacity test' -tier 2; E 'fill' '^device: fill, ' -tier 2
    E 'cd' '^cd: '; E 'check' '^check: tree$'; E 'compare' '^compare: '; E 'cmp' '^cmp: '
    E 'copy' '^copy: copy$'; E 'fastcopy' '^copy: fastcopy$'; E 'balanced' '^copy: balanced$'; E 'maxcopy' '^copy: maxcopy$'
    E 'smartcopy' '^copy: smartcopy$'; E 'safecopy' '^copy: safecopy$'; E 'synccopy' '^copy: synccopy$'
    E 'wipe' '^wipe: '; E 'secure' '^secure: '; E 'unsecure' '^unsecure: '; E 'reveal' '^reveal: '
    E 'from' '^batch: '; E 'hist' '^history: hist'; E 'help' '^help: '
    E 'probe' '^probe: yes' -tier 2; E 'recover' '^recover: ' -tier 2
    E 'network' '^network: ' -optional
    E 'fdsec:info' '^fdsec: info$'; E 'fdsec:verify' '^fdsec: verify$'
    E 'fdsec:register' -exempt 'writes the Explorer registration for this user; proven by its own tests (fdsec_register_*_test.go)'
    E 'fdsec:unregister' -exempt 'the same registration, removed; proven by the same tests'
    E 'ui' -exempt 'the shell window; filedo_win.exe --selftest proves it'
    E 'dm' -exempt 'the Disk Manager window; filedo_win.exe --selftest proves it'
    E 'vd:new' '^vd: new 64M$'; E 'vd:info' '^vd: info$'; E 'vd:verify' '^vd: verify$'; E 'vd:export' '^vd: export raw$'
    E 'vd:list' '^vd: list shows it$'; E 'vd:status' '^vd: status$'; E 'vd:add' '^vd: add a name$'; E 'vd:forget' '^vd: forget the name$'
    E 'vd:destroy' '^vd: destroy$' -tier 2
    E 'vd:mount' '^mount: ' -tier 2; E 'vd:unmount' '^unmount: ' -tier 2; E 'vd:save' '^ram: save to the file$' -tier 2
    E 'vd:format' '^vd: format ' -tier 2
    E 'vd:chkdsk' '^vd: chkdsk scan$' -tier 2
    E 'vd:compact' -batch 'compact'; E 'vd:grow' -batch 'grow'; E 'vd:seal' -batch 'seal'; E 'vd:clone' -batch 'clone'; E 'vd:pass' -batch 'pass'
    E 'vd:auto' -exempt 'installs the logon task: it changes the machine'
    E 'vd:stop' -exempt 'stops the block server that every mounted disk of this machine depends on'
    # The FMS share verbs (SP-0121) talk to the Fast Media Sorter for Windows worker, which a test machine
    # does not have installed; they are proven by the client and CLI tests (fmsworker, cmd\filedo) and by the
    # joint manual kit of SP-0121 stage S4, not by this run.
    E 'vd:share' -exempt 'offers the disk to the Fast Media Sorter for Windows worker, which must be installed and running; proven by the client and CLI tests and the SP-0121 manual kit'
    E 'vd:open' -exempt 'asks that worker to mount the disk (an elevated session of its own); proven by the same'
    E 'vd:close' -exempt 'asks that worker to close the disk and drain its transfers; proven by the same'
    E 'vd:autostart' -exempt 'stores a credential in that worker''s protected store: it changes the machine'
    # Partition disks (SP-0148). `vd disks` reads every disk with access 0 and changes nothing, so it runs
    # in tier 1. Creating, imaging and adopting a partition change a disk's partition table or read a raw
    # partition: they run only on a disposable VHDX in the elevated tier (tests\prove-partition-disks.ps1)
    # and never on a disk of this machine.
    E 'vd:disks' '^vd: disks$'
    E 'vd:image' -exempt 'copies a partition disk to a file: needs a partition, made only on a disposable VHDX by tests\prove-partition-disks.ps1 (elevated)'
    E 'vd:adopt' -exempt 'registers a FileDO partition found on a disk: proven on a disposable VHDX by tests\prove-partition-disks.ps1 (elevated)'
)

function Get-HelpVerbs([string]$helpText) {
    $keys = [System.Collections.Generic.List[string]]::new()
    $block = { param($title) if ($helpText -match "(?s)$title\r?\n(.*?)\r?\n\s*\r?\n") { $Matches[1] -split "`r?`n" } else { @() } }
    foreach ($line in (& $block 'MAIN OPERATIONS:')) { if ($line -match '^\s{2}([a-z][a-z-]*)\s{2,}\S') { if ($Matches[1] -ne 'vd') { $keys.Add($Matches[1]) } } }
    foreach ($line in (& $block 'COPY MODES:')) { if ($line -match '^\s{2}([a-z]+)\s{2,}\S') { $keys.Add($Matches[1]) } }
    if ($helpText -match '(?s)Virtual disks:.*?\((new,.*?)\)') { foreach ($v in ($Matches[1] -split '[,\s]+' | Where-Object { $_ })) { $keys.Add("vd:$v") } }
    $keys | Select-Object -Unique
}
function Test-Coverage([string]$helpText, [bool]$mountRan) {
    $problems = [System.Collections.Generic.List[string]]::new()
    $names = @($script:Rows | ForEach-Object Name)
    $batchTokens = @()
    $listFile = Join-Path $PSScriptRoot 'vd_batch.lst'
    if (Test-Path $listFile) {
        $batchTokens = @(Get-Content $listFile | Where-Object { $_.Trim() -and $_.TrimStart()[0] -ne '#' } | ForEach-Object { $_ -split '\s+' } | Select-Object -Unique)
    }
    $batchRan = [bool](@($names | Where-Object { $_ -match '^vd: every verb' }).Count)
    $done = 0; $exempt = [System.Collections.Generic.List[string]]::new(); $notRun = [System.Collections.Generic.List[string]]::new()
    foreach ($e in $Inventory) {
        if ($e.Exempt) { $exempt.Add($e.Key); continue }
        if ($e.Tier -eq '2' -and -not $mountRan) { $notRun.Add($e.Key); continue }
        $hit = if ($e.Batch) { $batchRan -and ($batchTokens -contains $e.Batch) } else { [bool](@($names | Where-Object { $_ -match $e.Step }).Count) }
        if ($hit) { $done++ } elseif ($e.Optional) { $notRun.Add($e.Key) } else { $problems.Add("'$($e.Key)' was not exercised - no step matches $(if ($e.Batch) { "a '$($e.Batch)' line of vd_batch.lst" } else { $e.Step })") }
    }
    $keys = @($Inventory | ForEach-Object Key)
    foreach ($v in (Get-HelpVerbs $helpText)) {
        if ($keys -notcontains $v) { $problems.Add("the help lists '$v' but the operations run has no entry for it - add a step, or an exemption with its reason, to the inventory in tests\prove-operations.ps1") }
    }
    [pscustomobject]@{ Problems = @($problems); Done = $done; Exempt = @($exempt); NotRun = @($notRun); Total = $Inventory.Count }
}

# ---- the baseline: the last passing runs of this machine --------------------------------------------

function Get-Median([double[]]$v) {
    if (-not @($v).Count) { return 0.0 }
    $s = @($v | Sort-Object); $m = [int][math]::Floor($s.Count / 2)
    if ($s.Count % 2) { $s[$m] } else { ($s[$m - 1] + $s[$m]) / 2 }
}
function Get-Baseline {
    $root = $EvidenceRoot
    $docs = [System.Collections.Generic.List[object]]::new()
    if (Test-Path $root) {
        foreach ($d in Get-ChildItem $root -Directory -Filter 'ops-*') {
            if ($d.FullName -eq $script:OutDir) { continue }
            $t = Join-Path $d.FullName 'timings.json'
            if (-not (Test-Path $t)) { continue }
            try { $j = Get-Content $t -Raw | ConvertFrom-Json } catch { continue }
            if ($j.PSObject.Properties['schema'] -and $j.schema -ge 2 -and $j.pass -eq $true -and $j.machine -eq $env:COMPUTERNAME) { $docs.Add($j) }
        }
    }
    $use = @()
    foreach ($j in @($docs | Sort-Object { [datetime]$_.at } -Descending)) {
        $use += $j
        if ($j.PSObject.Properties['rebaseline'] -and $j.rebaseline) { break }
        if ($use.Count -ge 8) { break }
    }
    $samples = @{}; $lastSig = @{}; $lastAt = @{}; $latestByTier = @{}
    foreach ($j in $use) {   # newest first
        foreach ($s in @($j.steps)) {
            $n = [string]$s.name
            if ($s.ms -gt 0) { if (-not $samples.ContainsKey($n)) { $samples[$n] = [System.Collections.Generic.List[double]]::new() }; $samples[$n].Add([double]$s.ms) }
            if (-not $lastSig.ContainsKey($n)) { $lastSig[$n] = [string]$s.sig; $lastAt[$n] = [datetime]$j.at }
            if (-not $latestByTier.ContainsKey([string]$s.tier)) { $latestByTier[[string]$s.tier] = $j }
        }
    }
    [pscustomobject]@{ Runs = @($use); Samples = $samples; LastSig = $lastSig; LastAt = $lastAt; LatestByTier = $latestByTier }
}

# Speed against the baseline medians. Hard, in the load section only: a step that normally runs 1.5 s or
# more at 1.5x its median and 500 ms more, a shorter one at twice and 300 ms (a short step is noisier),
# and the process start-up figure at twice and 20 ms. Warn: a load step at 1.25x and 250 ms, any other
# step at twice its median and 150 ms more (500 ms in tier 2, whose disks vary more), and a median step
# ratio of 1.5 over tier 1.
function Get-SpeedFindings($rows, $base, [int]$longMs = 1500) {
    $hard = [System.Collections.Generic.List[string]]::new(); $warn = [System.Collections.Generic.List[string]]::new()
    $ratios = [System.Collections.Generic.List[double]]::new()
    foreach ($row in $rows) {
        if ($row.Ms -le 0 -or -not $base.Samples.ContainsKey($row.Name) -or $base.Samples[$row.Name].Count -lt 3) { continue }
        $med = Get-Median $base.Samples[$row.Name].ToArray()
        if ($med -le 0) { continue }
        $ratio = $row.Ms / $med
        if ($row.Tier -eq '1') { $ratios.Add($ratio) }
        $line = '{0}: {1} ms, the median of the last {2} runs is {3:N0} ms ({4:N1}x)' -f $row.Name, $row.Ms, $base.Samples[$row.Name].Count, $med, $ratio
        if ($row.Name -like 'startup:*') { if ($ratio -ge 2 -and ($row.Ms - $med) -ge 20) { $hard.Add($line) } }
        elseif ($row.Tier -eq 'load') {
            $long = $med -ge $longMs
            if ($ratio -ge $(if ($long) { 1.5 } else { 2.0 }) -and ($row.Ms - $med) -ge $(if ($long) { 500 } else { 300 })) { $hard.Add($line) }
            elseif ($ratio -ge 1.25 -and ($row.Ms - $med) -ge 250) { $warn.Add($line) }
        }
        elseif ($ratio -ge 2 -and ($row.Ms - $med) -ge $(if ($row.Tier -eq '2') { 500 } else { 150 })) { $warn.Add($line) }
    }
    $mr = if ($ratios.Count -ge 20) { Get-Median $ratios.ToArray() } else { $null }
    if ($null -ne $mr -and $mr -ge 1.5) { $warn.Add(('tier 1 as a whole: the median step is {0:N1}x its baseline over {1} steps (machine noise, or everything got slower - see the start-up figure)' -f $mr, $ratios.Count)) }
    [pscustomobject]@{ Hard = @($hard); Warn = @($warn); MedianRatio = $mr; Steps = $ratios.Count }
}

# What changed against the baseline in what the run does: steps that vanished, steps that are new, and
# steps whose exit code, verdict or counts are no longer what they were.
function Get-FunctionFindings($rows, $base, [string[]]$tiersRan) {
    $vanished = [System.Collections.Generic.List[string]]::new(); $added = [System.Collections.Generic.List[string]]::new(); $changed = [System.Collections.Generic.List[string]]::new()
    $have = @{}; foreach ($row in $rows) { $have[$row.Name] = $row }
    foreach ($t in $tiersRan) {
        if (-not $base.LatestByTier.ContainsKey($t)) { continue }
        foreach ($s in @($base.LatestByTier[$t].steps)) {
            if ([string]$s.tier -ne $t -or $have.ContainsKey([string]$s.name)) { continue }
            $skipped = $false; foreach ($p in $script:SkippedPrefix) { if ([string]$s.name -like "$p*") { $skipped = $true } }
            if (-not $skipped) { $vanished.Add("$($s.name) (in the passing run of $([datetime]$base.LatestByTier[$t].at))") }
        }
    }
    foreach ($row in $rows) {
        if (-not $base.LastSig.ContainsKey($row.Name)) { if ($base.Runs.Count) { $added.Add($row.Name) }; continue }
        $was = $base.LastSig[$row.Name]
        if ($row.Sig -and $was -and $row.Sig -ne $was) { $changed.Add("$($row.Name): was [$was], now [$($row.Sig)] (baseline of $($base.LastAt[$row.Name]))") }
    }
    [pscustomobject]@{ Vanished = @($vanished); Added = @($added); Changed = @($changed) }
}

# ---- the run ------------------------------------------------------------------------------------

$helpText = ''
$base = $null
$tier1Repeated = $false
try {
    New-Item -ItemType Directory -Force -Path $OutDir, $Work | Out-Null
    $script:EmptyFile = Join-Path $OutDir 'empty-stdin.txt'
    Set-Content -LiteralPath $script:EmptyFile -Value '' -NoNewline
    if (-not (Test-Path -LiteralPath $Exe)) { Done 2 'COULD NOT VERIFY' "no filedo at $Exe - run .\build.ps1 first" }
    $helpText = (& $Exe '-?' 2>&1 | Out-String)
    if (-not $Version) { if ($helpText -match 'FileDO v(\d{10})') { $Version = $Matches[1] } }

    if ($MountTierOnly) {
        if (-not (Test-Elevated)) { Done 2 'COULD NOT VERIFY' 'the mount tier needs an elevated console' }
        Invoke-Tier2
    } else {
        $base = Get-Baseline
        Write-Host "operations-run: $Exe (version $Version), evidence in $OutDir"
        Write-Host ("  baseline: {0} passing run(s) of this machine{1}" -f $base.Runs.Count, $(if ($base.Runs.Count -lt 3) { ' - the speed gates start at 3' } else { '' }))
        Invoke-Tier1
        Invoke-Load

        # A tripped speed gate is measured once more, the load section on fresh data: noise is added time, so
        # the best of two is the fair figure, and only a step that is slow both times counts as slow.
        $gated = @($script:Rows | Where-Object { $_.Tier -eq 'load' })
        if ($base.Runs.Count -ge 3 -and -not @($script:Rows | Where-Object { -not $_.Pass }).Count -and $gated.Count -and (Get-SpeedFindings $gated $base $Load.MinStepMs).Hard.Count) {
            Write-Host "`n== The speed gate tripped - the load section again on fresh data, the best of the two counts ==" -ForegroundColor Yellow
            $first = @($gated)
            $rest = @($script:Rows | Where-Object { $_.Tier -ne 'load' })
            $mainWork = $script:Work
            Remove-Scratch (Join-Path $mainWork 'load')
            $script:Rows.Clear(); $script:NameCount = @{}
            $script:Work = "$mainWork-r2"; New-Item -ItemType Directory -Force -Path $script:Work | Out-Null
            Invoke-Load
            $second = @{}; foreach ($row in $script:Rows) { $second[$row.Name] = $row }
            $merged = foreach ($row in $first) {
                $again = $second[$row.Name]
                if ($again) {
                    $row.Note = ((@($row.Note, ('best of 2: {0} / {1} ms' -f $row.Ms, $again.Ms)) | Where-Object { $_ }) -join '  ')
                    $row.Ms = [math]::Min($row.Ms, $again.Ms)
                    if (-not $again.Pass) { $row.Pass = $false; $row.Problems = @($row.Problems) + @($again.Problems | ForEach-Object { "second pass: $_" }) }
                    if ($again.Sig -ne $row.Sig -and $again.Sig) { $row.Pass = $false; $row.Problems = @($row.Problems) + "the two passes disagree: [$($row.Sig)] against [$($again.Sig)]" }
                }
                $row
            }
            $script:Rows.Clear(); foreach ($row in (@($rest) + @($merged))) { $script:Rows.Add($row) }
            Remove-Scratch $script:Work; $script:Work = $mainWork
            $tier1Repeated = $true
        }

        if ($SkipMount) {
            $script:Skipped.Add('mount tier (-SkipMount): test, fill, clean, probe, recover, vault, ram and the size checks on a mounted disk were not run')
        } elseif (Test-Elevated) {
            Invoke-Tier2
        } else {
            Write-Host "`n== Tier 2: needs administrator rights - asking once (UAC) ==" -ForegroundColor Cyan
            $env:FILEDO_STATE_DIR = $null
            $result = Join-Path $OutDir 'mount-tier.json'
            $pwsh = (Get-Process -Id $PID).Path
            $childArgs = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $PSCommandPath, '-MountTierOnly', '-Exe', $Exe, '-Version', $Version,
                           '-OutDir', (Join-Path $OutDir 'mount'), '-RunId', $RunId, '-BudgetSeconds', $BudgetSeconds, '-ResultFile', $result)
            try {
                $child = Start-Process -FilePath $pwsh -ArgumentList (ConvertTo-ArgString $childArgs) -Verb RunAs -PassThru -WindowStyle Minimized
                if (-not $child.WaitForExit(20 * 60 * 1000)) { $child.Kill($true); $script:Unverified.Add('mount tier: the elevated run did not finish in 20 minutes') }
            } catch {
                $script:Unverified.Add("mount tier: the elevation was not granted ($($_.Exception.Message.Trim()))")
            }
            if (Test-Path $result) {
                $doc = Get-Content $result -Raw | ConvertFrom-Json
                foreach ($row in @($doc.rows)) {
                    $row.Problems = @($row.Problems)
                    $script:Rows.Add($row); Write-RowLine $row
                }
                foreach ($u in @($doc.unverified)) { $script:Unverified.Add([string]$u) }
            } elseif (-not $script:Unverified.Count) {
                $script:Unverified.Add('mount tier: the elevated run left no result file')
            }
        }
    }
} catch {
    $script:Unverified.Add("unexpected error: $($_.Exception.Message) at line $($_.InvocationInfo.ScriptLineNumber)")
} finally {
    $env:FILEDO_STATE_DIR = $null
}

# ---- verdict, timings, the comparison with the baseline --------------------------------------------

$failed = @($script:Rows | Where-Object { -not $_.Pass })
if ($MountTierOnly) {
    $doc = [pscustomobject]@{ rows = @($script:Rows); unverified = @($script:Unverified) }
    $doc | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $ResultFile -Encoding utf8NoBOM
    if ($failed.Count -eq 0 -and -not $script:Unverified.Count) { Remove-Scratch $Work }
    exit ([int]($failed.Count -gt 0))
}

function Get-MsSum($rows) { $x = @($rows); if ($x.Count) { [int]($x | Measure-Object Ms -Sum).Sum } else { 0 } }
$timed = @($script:Rows | Where-Object { $_.Ms -gt 0 })
$total = Get-MsSum $timed
$tiersRan = @($script:Rows | ForEach-Object Tier | Select-Object -Unique)
$mountRan = $tiersRan -contains '2'
$compareProblems = [System.Collections.Generic.List[string]]::new()
$coverageProblems = [System.Collections.Generic.List[string]]::new()
$coverage = $null
if ($base) {
    $fn = Get-FunctionFindings @($script:Rows) $base $tiersRan
    $sp = Get-SpeedFindings $timed $base $Load.MinStepMs
    $coverage = Test-Coverage $helpText $mountRan
    Write-Host ''
    Write-Host ("operations-run: {0} steps, {1} ms of operation time, {2}" -f $script:Rows.Count, $total,
        $(if ($base.Runs.Count) { "baseline of $($base.Runs.Count) passing run(s), the newest from $(([datetime]$base.Runs[0].at).ToString('yyyy-MM-dd HH:mm'))" } else { 'no passing run to compare with yet - this one becomes the baseline' }))
    $loadRows = @($timed | Where-Object { $_.Tier -eq 'load' })
    if ($base.Runs.Count -and $loadRows.Count) {
        $runTotals = @($base.Runs | ForEach-Object { [double](@($_.steps | Where-Object { $_.tier -eq 'load' } | ForEach-Object { $_.ms }) | Measure-Object -Sum).Sum } | Where-Object { $_ -gt 0 })
        Write-Host ("  load section: {0} ms now{1}{2}" -f (Get-MsSum $loadRows),
            $(if ($runTotals.Count) { ', median {0:N0} ms over the baseline' -f (Get-Median $runTotals) } else { '' }),
            $(if ($null -ne $sp.MedianRatio) { ('; tier 1 median step {0:N2}x' -f $sp.MedianRatio) } else { '' }))
        # How comparable the figures are: the spread of each load step across the baseline runs.
        $cvs = @(foreach ($row in ($loadRows | Where-Object { $_.Name -notlike 'startup:*' -and $Load.ShortByNature -notcontains $_.Name })) {
            if ($base.Samples.ContainsKey($row.Name) -and $base.Samples[$row.Name].Count -ge 3) {
                $v = $base.Samples[$row.Name].ToArray(); $avg = ($v | Measure-Object -Average).Average
                if ($avg -gt 0) { [pscustomobject]@{ Name = $row.Name; Cv = [math]::Sqrt(@($v | ForEach-Object { ($_ - $avg) * ($_ - $avg) } | Measure-Object -Sum).Sum / ($v.Count - 1)) / $avg } }
            }
        })
        if ($cvs.Count) {
            $worst = $cvs | Sort-Object Cv -Descending | Select-Object -First 1
            Write-Host ('  spread of the load steps over the baseline: median {0:P0}, worst {1:P0} ({2})' -f (Get-Median @($cvs | ForEach-Object Cv)), $worst.Cv, $worst.Name)
        }
        $short = @($loadRows | Where-Object { $_.Ms -lt $Load.MinStepMs -and $_.Name -notlike 'startup:*' -and $Load.ShortByNature -notcontains $_.Name } | ForEach-Object { '{0} ({1} ms)' -f $_.Name, $_.Ms })
        if ($short.Count) { Write-Host ("  shorter than {0} ms, so too noisy to compare well - raise its size in the load plan: {1}" -f $Load.MinStepMs, ($short -join '; ')) -ForegroundColor Yellow }
    }
    foreach ($s in $fn.Vanished) { $compareProblems.Add("a step that used to run is gone: $s") }
    foreach ($s in $fn.Changed) { $compareProblems.Add("behaviour changed: $s") }
    if ($base.Runs.Count -ge 3) { foreach ($s in $sp.Hard) { $compareProblems.Add("slower: $s") } }
    foreach ($s in $coverage.Problems) { $coverageProblems.Add($s) }
    if ($fn.Added.Count) {
        $shown = @($fn.Added | Select-Object -First 4) -join '; '
        Write-Host ("  {0} new step(s), no baseline for them yet: {1}{2}" -f $fn.Added.Count, $shown, $(if ($fn.Added.Count -gt 4) { '; ..' } else { '' })) -ForegroundColor DarkGray
    }
    foreach ($s in $sp.Warn) { Write-Host "  slower than usual (advisory): $s" -ForegroundColor Yellow }
    Write-Host ("  coverage: {0} verbs in the inventory, {1} exercised, {2} exempt{3}" -f $coverage.Total, $coverage.Done, $coverage.Exempt.Count,
        $(if ($coverage.NotRun.Count) { ", $($coverage.NotRun.Count) not run here ($($coverage.NotRun -join ', '))" } else { '' }))
    if ($tier1Repeated) { Write-Host '  the speed gate tripped on the first pass; the figures above are the best of two passes' -ForegroundColor Yellow }
}
foreach ($s in $script:Skipped) { Write-Host "  skipped: $s" -ForegroundColor Yellow }
foreach ($u in $script:Unverified) { Write-Host "  could not verify: $u" -ForegroundColor Yellow }
$compareFailed = $compareProblems.Count -gt 0 -and -not $Rebaseline
$coverageFailed = $coverageProblems.Count -gt 0   # no -Rebaseline waives an unexercised verb
foreach ($s in $coverageProblems) { Write-Host "  FAIL: coverage: $s" -ForegroundColor Red }
foreach ($s in $compareProblems) { Write-Host "  $(if ($Rebaseline) { 'accepted by -Rebaseline' } else { 'FAIL' }): $s" -ForegroundColor $(if ($Rebaseline) { 'Yellow' } else { 'Red' }) }

$passed = ($failed.Count -eq 0 -and -not $script:Unverified.Count -and -not $compareFailed -and -not $coverageFailed)
$tierMs = { param($t) Get-MsSum @($timed | Where-Object { $_.Tier -eq $t }) }
[pscustomobject]@{
    schema = 2; version = $Version; at = (Get-Date).ToUniversalTime().ToString('o'); machine = $env:COMPUTERNAME
    pass = $passed; rebaseline = $Rebaseline.IsPresent; tiers = $tiersRan
    totals = [pscustomobject]@{ tier1Ms = (& $tierMs '1'); loadMs = (& $tierMs 'load'); tier2Ms = (& $tierMs '2'); totalMs = [int]$total }
    steps = @($script:Rows | ForEach-Object { [pscustomobject]@{ name = $_.Name; tier = $_.Tier; ms = $_.Ms; mbps = $_.MBps; code = $_.Code; verdict = $_.Verdict; sig = $_.Sig } })
} | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath (Join-Path $OutDir 'timings.json') -Encoding utf8NoBOM

if ($failed.Count -eq 0 -and -not $script:Unverified.Count) { Remove-Scratch $Work }   # a finding of the comparison leaves nothing to inspect there
if ($passed) {
    # Private evidence is kept for the last dozen runs only.
    $old = @(Get-ChildItem $EvidenceRoot -Directory -Filter 'ops-*' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending | Select-Object -Skip 12)
    foreach ($d in $old) { if ($d.FullName -ne $OutDir) { try { [IO.Directory]::Delete($d.FullName, $true) } catch { } } }
} elseif (Test-Path -LiteralPath $Work) {
    Write-Host "  scratch data kept: $Work"
}
if ($failed.Count) { Done 1 'FAIL' "$($failed.Count) of $($script:Rows.Count) step(s): $((@($failed | ForEach-Object Name) | Select-Object -First 6) -join '; ')" }
if ($coverageFailed) { Done 1 'FAIL' "coverage: $($coverageProblems.Count) verb(s) without a step - a new verb needs one (or a reasoned exemption) in the inventory of tests\prove-operations.ps1" }
if ($compareFailed) { Done 1 'FAIL' "against the baseline: $($compareProblems.Count) finding(s) - fix them, or accept an intended change with -Rebaseline" }
if ($script:Unverified.Count) { Done 2 'COULD NOT VERIFY' ($script:Unverified -join '; ') }
Done 0 'PASS' ("$($script:Rows.Count) steps in $([int]((Get-Date) - $script:Started).TotalSeconds) s" + $(if ($script:Skipped.Count) { ", $($script:Skipped.Count) skipped" } else { '' }))
