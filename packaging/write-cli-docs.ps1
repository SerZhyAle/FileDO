#Requires -Version 7.0
<#
.SYNOPSIS
  Writes docs/CLI.md from docs/cli-commands.json, or checks that the committed copy is current (SP-0220).

.DESCRIPTION
  docs/CLI.md is a render target (APP-COMMANDS rule 20): the command table, each command's synopsis, risk,
  exit codes and its cmd and PowerShell examples, all taken from the manifest. It is never edited by hand;
  change docs/cli-commands.json and run this script. The text around the tables - how a command is answered,
  the exit-code and risk vocabulary - is fixed here, and the counts and the lists in it are computed.

    (no switch)  writes docs/CLI.md (UTF-8 without BOM, LF line ends) and prints what it wrote.
    -Check       writes nothing. Prints one "  FAIL  <reason>" line per defect and a last verdict line:
                   manifest   docs/cli-commands.json parses, carries the members of APP-COMMANDS rule 18, and
                              equals cmd/filedo/cli-commands.json byte for byte (the copy filedo.exe embeds)
                   render     docs/CLI.md equals what this script would write now (line ends compared as LF)
                   guides     docs/guides/commands.html and its de and fr pages name every command of the
                              manifest (the text "filedo <command>", or "filedo_win.exe <command>" for the
                              developer tooling), so a verb cannot be added to the manifest and left off the site

  Exit code (CHECK-VERDICT): 0 = written, or current under -Check; 1 = -Check found a defect; 2 = could not
  verify (a file is missing or does not parse). The last line is the verdict.

.PARAMETER Check
  Compare instead of write (see above).
#>
[CmdletBinding()]
param([switch]$Check)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$global:LASTEXITCODE = 0
$root = Split-Path $PSScriptRoot -Parent
$manifestPath = Join-Path $root 'docs\cli-commands.json'
$embeddedPath = Join-Path $root 'cmd\filedo\cli-commands.json'
$outPath = Join-Path $root 'docs\CLI.md'
$guidePages = @('docs\guides\commands.html', 'docs\de\guides\commands.html', 'docs\fr\guides\commands.html')

function Stop-Unverified([string]$why) {
    Write-Host "cli-docs: COULD NOT VERIFY ($why)" -ForegroundColor Yellow
    exit 2
}

foreach ($p in @($manifestPath, $embeddedPath)) {
    if (-not (Test-Path -LiteralPath $p -PathType Leaf)) { Stop-Unverified "$([IO.Path]::GetRelativePath($root, $p)) is missing" }
}
try {
    $manifest = [IO.File]::ReadAllText($manifestPath, [Text.UTF8Encoding]::new($false)) | ConvertFrom-Json
} catch {
    Stop-Unverified "docs/cli-commands.json does not parse: $($_.Exception.Message)"
}

$failures = [System.Collections.Generic.List[string]]::new()

# ---- manifest ---------------------------------------------------------------------
foreach ($m in 'schemaVersion', 'product', 'exe', 'cliVersion', 'commands') {
    if (-not $manifest.PSObject.Properties[$m]) { $failures.Add("manifest has no '$m' member (APP-COMMANDS rule 18)") }
}
if ($failures.Count) { $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }; Write-Host "cli-docs: FAIL ($($failures.Count) problems)" -ForegroundColor Red; exit 1 }

$entryMembers = 'name', 'aliases', 'summary', 'synopsis', 'args', 'returns', 'risk', 'coldStart', 'wait', 'timeoutMs',
    'interactive', 'idempotent', 'exitCodes', 'examples', 'since'
$commands = @($manifest.commands)
foreach ($c in $commands) {
    foreach ($m in $entryMembers) {
        if (-not $c.PSObject.Properties[$m]) { $failures.Add("command '$($c.name)' has no '$m' member (APP-COMMANDS rule 18)") }
    }
}
$manifestHash = (Get-FileHash -LiteralPath $manifestPath -Algorithm SHA256).Hash
$embeddedHash = (Get-FileHash -LiteralPath $embeddedPath -Algorithm SHA256).Hash
if ($manifestHash -ne $embeddedHash) { $failures.Add('docs/cli-commands.json and cmd/filedo/cli-commands.json differ (filedo.exe embeds the second; copy the first over it)') }
if ($failures.Count) { $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }; Write-Host "cli-docs: FAIL ($($failures.Count) problems)" -ForegroundColor Red; exit 1 }

# ---- render -----------------------------------------------------------------------
function Cell([object]$text) { return ([string]$text) -replace '\|', '\|' -replace "`r?`n", ' ' }
function Seconds([int]$ms) { if ($ms -le 0) { return 'no bound' }; if ($ms % 1000 -eq 0) { return "$($ms / 1000) s" } else { return "$ms ms" } }
function Own([object]$c) { return ($c.PSObject.Properties['exe'] -and [string]$c.exe -ne [string]$manifest.exe) }
function IsDeveloper([object]$c) { return ($c.PSObject.Properties['audience'] -and [string]$c.audience -eq 'developer') }
function Spelling([object]$c) {
    if (Own $c) { return "$([IO.Path]::GetFileNameWithoutExtension([string]$c.exe)).exe $($c.name)" }
    return "filedo $($c.name)"
}
function FormatAlias([object]$a, [string]$defaultExe) {
    if ($a -is [string]) {
        if ($a -eq '') { return '(no arguments)' }
        return "``$a``"
    }
    $sp = [string]$a.spelling
    $label = if ($sp -eq '') { '(no arguments)' } else { "``$sp``" }
    $notes = [System.Collections.Generic.List[string]]::new()
    if ($a.PSObject.Properties['exe'] -and [string]$a.exe -and [string]$a.exe -ne $defaultExe) {
        $notes.Add([string]$a.exe)
    }
    if ($a.PSObject.Properties['implies'] -and @($a.implies).Count) {
        $notes.Add("implies ``$(@($a.implies) -join ' ')``")
    }
    if ($a.PSObject.Properties['answers'] -and $a.answers -eq $false) {
        $notes.Add('no answer')
    }
    if ($a.PSObject.Properties['legacy'] -and $a.legacy -eq $true) {
        $notes.Add('legacy')
    }
    if ($notes.Count) { return "$label ($($notes -join ', '))" }
    return $label
}

$coldText = @{
    start   = 'starts FileDO, then asks it'
    oneshot = 'does the work on the spot'
    refuse  = 'refuses: exit 11, reason `not-running`'
}
$riskText = [ordered]@{
    'safe'             = 'changes only FileDO''s own windows and runs and reads nothing of yours'
    'reads-user-data'  = 'puts something of yours - screen content, the clipboard, file contents or file names - into its answer or into a file the caller names'
    'writes-user-data' = 'creates, changes or deletes your files or settings outside FileDO''s own data folder, or acts inside another program'
    'leaves-machine'   = 'sends anything off this computer'
}
# String keys on purpose: an ordered dictionary indexed by an integer reads a position, not a key.
$exitText = [ordered]@{
    '0'  = 'done'
    '1'  = 'the command ran and the operation failed'
    '2'  = 'not proven: the time bound passed with no answer, or the answer was not understood'
    '10' = 'usage: an unknown command, option or value, a missing required option, or a request that is too large'
    '11' = 'not ready: no window program is running, it is busy (a dialog is open, it is closing, or another command is being answered), or the named target or folder does not exist'
    '12' = 'permission: the command''s risk class is switched off in Settings, or the running FileDO is elevated and this caller is not'
}

$userCommands = @($commands | Where-Object { -not (IsDeveloper $_) })
$devCommands = @($commands | Where-Object { IsDeveloper $_ })
$families = if ($manifest.PSObject.Properties['families']) { @($manifest.families) } else { @() }

$sb = [System.Text.StringBuilder]::new()
function Line([string]$s = '') { [void]$sb.Append($s).Append("`n") }

function RenderCommandSection([object]$c) {
    Line "### ``$(Spelling $c)``"
    Line
    Line (Cell $c.summary)
    Line
    Line "- **Synopsis:** ``$($c.synopsis)``"
    Line "- **Risk:** $($c.risk) - $($riskText[[string]$c.risk])"
    $cold = $coldText[[string]$c.coldStart]
    if (-not $cold) { $cold = [string]$c.coldStart }
    Line "- **With no window program running:** $cold"
    if ([int]$c.timeoutMs -le 0) { Line "- **Waits for:** $($c.wait), no bound of its own" }
    else { Line "- **Waits for:** $($c.wait), at most $(Seconds ([int]$c.timeoutMs))" }
    if ($c.idempotent) { Line '- **Repeat after exit 2:** safe, a second run leaves the same state as one' }
    else { Line '- **Repeat after exit 2:** not safe, a second run may repeat the work' }
    if ($c.interactive) { Line '- **Interactive:** yes, it waits for the person' }
    if ($c.PSObject.Properties['defaultFor'] -and [string]$c.defaultFor) { Line "- **Default verb for:** ``$($c.defaultFor)`` (or just a path)" }
    if ($c.PSObject.Properties['forward'] -and [string]$c.forward -eq 'never') { Line '- **Process:** runs in the receiving process (`forward: never`), never through the running window' }
    if (IsDeveloper $c) { Line '- **Audience:** developer (listed by `filedo commands --developer` and `filedo commands --json`)' }
    if (Own $c) {
        if (IsDeveloper $c) { Line "- **Run by:** ``$($c.exe)`` itself, in its own process and never through the running window; developer tooling" }
        else { Line "- **Run by:** ``$($c.exe)`` itself, in its own process (one window per target)" }
    }
    $aliases = @($c.aliases)
    if ($aliases.Count) {
        $cmdExe = if ($c.PSObject.Properties['exe'] -and [string]$c.exe) { [string]$c.exe } else { [string]$manifest.exe }
        $shown = @($aliases | ForEach-Object { FormatAlias $_ $cmdExe })
        Line "- **Kept spellings:** $($shown -join ', ') - older forms that still start the same thing"
    }
    if ($c.PSObject.Properties['deprecated'] -and $c.deprecated) { Line "- **Deprecated:** $($c.deprecated)" }
    Line "- **Since:** command set $($c.since)"
    Line

    $cargs = @($c.args)
    if ($cargs.Count) {
        Line '| Option | Kind | Required | Default | Meaning |'
        Line '| --- | --- | --- | --- | --- |'
        foreach ($a in $cargs) {
            $kind = [string]$a.kind
            if ($a.PSObject.Properties['values'] -and @($a.values).Count) { $kind = "${kind}: $(@($a.values) -join ', ')" }
            $def = ''
            if ($a.PSObject.Properties['default']) { $def = ([string]$a.default).ToLowerInvariant() }
            $req = if ($a.required) { 'yes' } else { 'no' }
            Line "| ``--$($a.name)`` | $(Cell $kind) | $req | $(Cell $def) | $(Cell $a.description) |"
        }
        Line
    }
    $rets = @($c.returns)
    if ($rets.Count) {
        Line 'Answer (`data`):'
        Line
        Line '| Key | Kind | Meaning |'
        Line '| --- | --- | --- |'
        foreach ($r in $rets) { Line "| ``$($r.name)`` | $($r.kind) | $(Cell $r.description) |" }
        Line
    }
    Line '| Exit code | Meaning |'
    Line '| --- | --- |'
    foreach ($e in @($c.exitCodes)) { Line "| $($e.code) | $(Cell $e.meaning) |" }
    Line

    foreach ($x in @($c.examples)) {
        Line "Example ($($x.shell)):"
        Line
        Line ('```' + $(if ($x.shell -eq 'powershell') { 'powershell' } else { 'bat' }))
        Line ([string]$x.line)
        Line '```'
        Line
        Line "Expect: $($x.expect)."
        Line
    }
}

Line '# FileDO commands'
Line
Line '> This file is generated from [cli-commands.json](cli-commands.json) by `packaging/write-cli-docs.ps1`. Do not edit it by hand: change the manifest and run the script.'
Line
$userFiledo = @($userCommands | Where-Object { -not (Own $_) }).Count
$userWin = $userCommands.Count - $userFiledo
$winPart = if ($userWin -gt 0) { " ($userFiledo on ``filedo.exe``, $userWin on ``filedo_win.exe``)" } else { ' that ``filedo.exe`` answers, most of them by asking the running window program' }
Line "FileDO answers commands from scripts, scheduled tasks, shortcuts and development tools. Type ``filedo <command>`` in cmd or PowerShell. Command set version **$($manifest.cliVersion)**: $($userCommands.Count) user commands$winPart, $($devCommands.Count) developer tools that ``filedo_win.exe`` runs in its own process, and $($families.Count) non-forwarded console families."
Line
Line 'To see what a FileDO on a machine can be asked, run `filedo commands --json`. It prints the manifest as shipped, starts no window, and is the same list as this file.'
Line
Line '## How a command is answered'
Line
Line '- `filedo.exe` carries the command to the FileDO window program (`filedo_win.exe`: the main window, the Disk Manager and the tray icon) when it is running. The carrying is a local pipe that only your own Windows account, in your own session, can reach - never the network. The running copy does the work and answers; `filedo` prints the answer and exits with its exit code.'
Line '- With no window program running, each command''s own rule decides, as the table says: start it and ask it, do the work on the spot, or refuse with exit 11.'
Line '- Add `--json` to a command that offers it to get the answer as one JSON line with `status`, `exitCode`, `reason`, `message` and, when the command returns something, `data`.'
Line '- A command waits for the answer for at most its time bound. If the bound passes, `filedo` exits 2 and the running copy may still be working; it never reports a command done that it did not see end.'
Line '- One command is answered at a time. A second caller meets exit 11 (busy) when the first has not finished.'
Line '- FileDO''s console verbs - `filedo C: info`, `filedo copy`, `filedo vd mount` and the rest of `filedo -?` - are listed under Command families below with their own exit and event contracts.'
Line
Line '## Commands'
Line
Line '| Command | What it does | Risk | With no window program running | Time bound |'
Line '| --- | --- | --- | --- | --- |'
foreach ($c in $userCommands) {
    $cold = $coldText[[string]$c.coldStart]
    if (-not $cold) { $cold = [string]$c.coldStart }
    Line "| ``$(Cell (Spelling $c))`` | $(Cell $c.summary) | $($c.risk) | $cold | $(Seconds ([int]$c.timeoutMs)) |"
}
Line

foreach ($c in $userCommands) {
    RenderCommandSection $c
}

if ($devCommands.Count) {
    Line '## Developer tools'
    Line
    Line 'Hidden from plain `filedo commands`; pass `filedo commands --developer` or `filedo commands --json` to list them.'
    Line
    Line '| Command | What it does | Risk | With no window program running | Time bound |'
    Line '| --- | --- | --- | --- | --- |'
    foreach ($c in $devCommands) {
        $cold = $coldText[[string]$c.coldStart]
        if (-not $cold) { $cold = [string]$c.coldStart }
        Line "| ``$(Cell (Spelling $c))`` | $(Cell $c.summary) | $($c.risk) | $cold | $(Seconds ([int]$c.timeoutMs)) |"
    }
    Line

    foreach ($c in $devCommands) {
        RenderCommandSection $c
    }
}

if ($families.Count) {
    Line '## Command families'
    Line
    Line 'These grammars are not forwarded through the running window program; each runs in `filedo.exe` and keeps its own usage and exit contract.'
    Line
    foreach ($f in $families) {
        Line "### ``$($f.name)``"
        Line
        Line (Cell $f.summary)
        Line
        Line "- **Executable:** ``$($f.exe)``"
        Line "- **Usage:** ``$($f.help)``"
        Line "- **Exit contract:** $($f.exitContract)"
        Line "- **Event contract:** $($f.eventContract)"
        Line
    }
}



Line '## Exit codes'
Line
Line 'The codes are the portfolio vocabulary; each command lists the subset it can return.'
Line
Line '| Code | Meaning here |'
Line '| --- | --- |'
$used = @($commands | ForEach-Object { @($_.exitCodes) | ForEach-Object { [int]$_.code } } | Sort-Object -Unique)
foreach ($k in $exitText.Keys) { if ([int]$k -in $used) { Line "| $k | $($exitText[[string]$k]) |" } }
Line
Line '## Risk classes and the one setting'
Line
Line 'Every command declares one risk class, and the class decides who may ask.'
Line
Line '| Risk | What a command of this class does | Commands in this set |'
Line '| --- | --- | --- |'
foreach ($k in $riskText.Keys) {
    $names = @($commands | Where-Object { [string]$_.risk -eq $k } | ForEach-Object { "``$(Spelling $_)``" })
    $list = if ($names.Count) { $names -join ', ' } else { 'none' }
    Line "| $k | $($riskText[$k]) | $(Cell $list) |"
}
Line
Line 'A `safe` command is always accepted. A command of any other class that goes through the running window program is refused with exit 12 until the person turns on one setting: **Let other programs ask FileDO to work with my files, screen, clipboard or the network**, in Settings under "Windows and window positions". The setting is off until the person turns it on, and no command can change it. `filedo status` reports whether it is on.'
Line

$text = $sb.ToString()

# ---- compare or write ---------------------------------------------------------------
if (-not $Check) {
    [IO.File]::WriteAllText($outPath, $text, [Text.UTF8Encoding]::new($false))
    Write-Host "cli-docs: wrote docs/CLI.md ($($commands.Count) commands, $($text.Split("`n").Count - 1) lines)" -ForegroundColor Green
    exit 0
}

if (-not (Test-Path -LiteralPath $outPath -PathType Leaf)) {
    $failures.Add('docs/CLI.md does not exist (run packaging/write-cli-docs.ps1)')
} else {
    $have = ([IO.File]::ReadAllText($outPath, [Text.UTF8Encoding]::new($false))) -replace "`r`n", "`n"
    if ($have -ne $text) { $failures.Add('docs/CLI.md is stale: it differs from what docs/cli-commands.json renders to (run packaging/write-cli-docs.ps1)') }
}

foreach ($rel in $guidePages) {
    $page = Join-Path $root $rel
    if (-not (Test-Path -LiteralPath $page -PathType Leaf)) { $failures.Add("$($rel -replace '\\', '/') does not exist (the commands guide has a page in en, de and fr)"); continue }
    $html = [IO.File]::ReadAllText($page, [Text.UTF8Encoding]::new($false))
    foreach ($c in $commands) {
        $want = Spelling $c
        if (-not $html.Contains($want)) { $failures.Add("$($rel -replace '\\', '/') does not name the command '$want'") }
    }
}

if ($failures.Count) {
    $failures | ForEach-Object { Write-Host "  FAIL  $_" -ForegroundColor Red }
    Write-Host "cli-docs: FAIL ($($failures.Count) problems)" -ForegroundColor Red
    exit 1
}
Write-Host "cli-docs: PASS ($($commands.Count) commands, docs/CLI.md current, $($guidePages.Count) guide pages name them all)" -ForegroundColor Green
exit 0
