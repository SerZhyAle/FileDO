#Requires -Version 7.0
<#
.SYNOPSIS
  The speed gate of tests\prove-operations.ps1, tested on its own: no filedo run, no disk, a second.

.DESCRIPTION
  The functions are lifted out of the operations script by AST and fed synthetic rows and baselines, so the
  thresholds that decide "slower" are pinned: a change to them is a change to this file, in the same edit.
  Exit code: 0 all held, 1 one did not.
#>
$ErrorActionPreference = 'Stop'
$path = Join-Path $PSScriptRoot 'prove-operations.ps1'
$ast = [System.Management.Automation.Language.Parser]::ParseFile($path, [ref]$null, [ref]$null)
$want = 'Get-Median', 'Get-SpeedFindings'
foreach ($f in $ast.FindAll({ param($n) $n -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $n.Name -in $want }, $true)) { Invoke-Expression $f.Extent.Text }

function Base([hashtable]$m) {
    $s = @{}
    foreach ($k in $m.Keys) { $l = [System.Collections.Generic.List[double]]::new(); foreach ($v in $m[$k]) { $l.Add([double]$v) }; $s[$k] = $l }
    [pscustomobject]@{ Samples = $s }
}
function Row([string]$name, [string]$tier, [int]$ms) { [pscustomobject]@{ Name = $name; Tier = $tier; Ms = $ms } }
$script:fail = 0
function Expect([string]$what, $got, $wantValue) {
    if ($got -ne $wantValue) { "FAIL $what : got $got, want $wantValue"; $script:fail++ } else { "ok   $what" }
}
function Counts($r) { '{0}/{1}' -f $r.Hard.Count, $r.Warn.Count }

$st = 'startup: filedo -? (best of 7)'
$b = Base @{ $st = 10, 11, 10, 11; 'load: short' = 400, 410, 390, 405; 'load: long' = 3000, 3100, 2900, 3050; 'secure: file' = 100, 110, 90; 'device: fill' = 1000, 1100, 900 }

# the start-up figure: twice the median and 20 ms more
Expect 'startup 45 vs 10 is hard' (Counts (Get-SpeedFindings @(Row $st 'load' 45) $b)) '1/0'
Expect 'startup 25 vs 10 is not hard (+15 only)' (Get-SpeedFindings @(Row $st 'load' 25) $b).Hard.Count 0
Expect 'startup 31 vs 10.5 is hard (2.9x, +20)' (Get-SpeedFindings @(Row $st 'load' 31) $b).Hard.Count 1
# a load step that normally runs under 1.5 s: twice and 300 ms
Expect 'short load 900 vs 402 is hard' (Counts (Get-SpeedFindings @(Row 'load: short' 'load' 900) $b)) '1/0'
Expect 'short load 790 vs 402 warns only (1.97x)' (Counts (Get-SpeedFindings @(Row 'load: short' 'load' 790) $b)) '0/1'
Expect 'short load 600 vs 402 is quiet (+198)' (Counts (Get-SpeedFindings @(Row 'load: short' 'load' 600) $b)) '0/0'
# a load step that normally runs 1.5 s or more: 1.5x and 500 ms
Expect 'long load 4800 vs 3012 is hard (1.6x)' (Counts (Get-SpeedFindings @(Row 'load: long' 'load' 4800) $b)) '1/0'
Expect 'long load 4400 vs 3012 warns only (1.46x)' (Counts (Get-SpeedFindings @(Row 'load: long' 'load' 4400) $b)) '0/1'
Expect 'long load 3600 vs 3012 is quiet (1.2x)' (Counts (Get-SpeedFindings @(Row 'load: long' 'load' 3600) $b)) '0/0'
Expect 'a faster load step is never slow' (Counts (Get-SpeedFindings @(Row 'load: long' 'load' 1200) $b)) '0/0'
# everything outside the load section only warns
Expect 'tier 1 at 4x is advisory only' (Counts (Get-SpeedFindings @(Row 'secure: file' '1' 400) $b)) '0/1'
Expect 'tier 1 at 2.2x but +120 ms is quiet' (Counts (Get-SpeedFindings @(Row 'secure: file' '1' 220) $b)) '0/0'
Expect 'tier 2 at 4x is advisory only' (Counts (Get-SpeedFindings @(Row 'device: fill' '2' 4000) $b)) '0/1'
Expect 'tier 2 at 1.8x is quiet' (Counts (Get-SpeedFindings @(Row 'device: fill' '2' 1800) $b)) '0/0'
# no baseline, no verdict
Expect 'a step with no baseline is never slow' (Counts (Get-SpeedFindings @(Row 'new: no baseline' 'load' 9000) $b)) '0/0'
$thin = Base @{ 'load: short' = 400, 410 }
Expect 'two samples are not a baseline yet' (Get-SpeedFindings @(Row 'load: short' 'load' 9000) $thin).Hard.Count 0
# the median step ratio over tier 1 is a warning
$many = @{}; foreach ($i in 1..25) { $many["t1 step $i"] = 100, 100, 100 }
$rows = foreach ($i in 1..25) { Row "t1 step $i" '1' 160 }
Expect 'median ratio 1.6 over 25 steps warns only' (Counts (Get-SpeedFindings @($rows) (Base $many))) '0/1'
$rows = foreach ($i in 1..25) { Row "t1 step $i" '1' 120 }
Expect 'median ratio 1.2 is quiet' (Counts (Get-SpeedFindings @($rows) (Base $many))) '0/0'
# the long/short line moves with the argument
Expect 'a 2000 ms step is long at the default line' (Counts (Get-SpeedFindings @(Row 'load: edge' 'load' 3200) (Base @{ 'load: edge' = 2000, 2000, 2000 }))) '1/0'
Expect 'the same step is short when the line is 2500' (Counts (Get-SpeedFindings @(Row 'load: edge' 'load' 3200) (Base @{ 'load: edge' = 2000, 2000, 2000 }) 2500)) '0/1'
"failures: $script:fail"
exit ([int]($script:fail -gt 0))
