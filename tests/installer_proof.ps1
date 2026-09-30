<#
.SYNOPSIS
  The machine-changing proofs for SP-0030 PKG-01 (MSI versioning and upgrades) and PKG-02 (the setup
  EXE's Modify path and its upgrade of an earlier install), run for real on this machine.

.DESCRIPTION
  ELEVATED, and it installs and removes FileDO on the machine it runs on - that is the point, and
  why nothing else starts it. It asks nothing; start it deliberately:

      Start-Process powershell -Verb RunAs -Wait -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File',
        'tests\installer_proof.ps1','-Msi',<A.msi>,'-Setup',<A-setup.exe>,'-MsiB',<B.msi>,'-SetupB',<B-setup.exe>,
        '-PrevMsi',<published.msi>,'-PrevSetup',<published-setup.exe>,'-Log',<log path>

  A is the build under test, B is a later build of the same day (a higher minute, so a higher
  installer version), Prev is the last published release (the old four-field version mapping).
  Scenarios, each logging PASS/FAIL lines:
    1 A over what is installed (a real prior install), then B over A (same-day upgrade), then A over B
      (refused, 1638), then removal leaves nothing.
    2 A fresh with every feature; A with ADDLOCAL=Main only; published MSI with two features, then A over it
      (a deselected feature stays deselected).
    3 published setup EXE, a feature removed by maintenance, A's setup EXE over it, B's setup EXE over that:
      one visible Apps-and-features entry, Modify offered, the feature choice kept; removal is complete.
  At the end the machine gets back what it had when the script started: A's MSI with the same features
  the first installed product had (a script that starts on a machine with no FileDO leaves none).
  The Modify wizard itself (the feature tree page) is a window: installer_modify_ui.ps1 drives and
  photographs it.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Msi,
    [Parameter(Mandatory)][string]$Setup,
    [Parameter(Mandatory)][string]$MsiB,
    [Parameter(Mandatory)][string]$SetupB,
    [Parameter(Mandatory)][string]$PrevMsi,
    [Parameter(Mandatory)][string]$PrevSetup,
    [Parameter(Mandatory)][string]$Log
)
$ErrorActionPreference = 'Stop'
$id = [Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $id.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'run this elevated' }
New-Item -ItemType Directory -Force -Path (Split-Path $Log -Parent) | Out-Null
$msiLogs = Join-Path (Split-Path $Log -Parent) 'msi-logs'
New-Item -ItemType Directory -Force -Path $msiLogs | Out-Null
"" | Set-Content $Log
$script:pass = 0; $script:fail = 0
function Say([string]$t) { $t | Add-Content $Log; Write-Host $t }
function Check([string]$name, [bool]$ok, [string]$detail = '') {
    if ($ok) { $script:pass++; Say "PASS $name $detail" } else { $script:fail++; Say "FAIL $name $detail" }
}

$features = 'Main', 'ExplorerIntegration', 'DiskContainerIntegration', 'DesktopShortcut'
$inst = New-Object -ComObject WindowsInstaller.Installer

function Get-Arp {
    $rows = @()
    foreach ($h in 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall', 'HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall') {
        if (-not (Test-Path $h)) { continue }
        foreach ($k in Get-ChildItem $h) {
            $p = Get-ItemProperty $k.PSPath
            if ($p.DisplayName -eq 'FileDO') {
                $rows += [pscustomobject]@{
                    Key = $k.PSChildName; Version = $p.DisplayVersion; Hidden = ($p.SystemComponent -eq 1)
                    Msi = ($p.WindowsInstaller -eq 1); NoModify = $p.NoModify; ModifyPath = $p.ModifyPath
                    Uninstall = $p.UninstallString
                }
            }
        }
    }
    return $rows
}
function Get-Visible { @(Get-Arp | Where-Object { -not $_.Hidden }) }
function Get-MsiProduct { @(Get-Arp | Where-Object { $_.Msi }) | Select-Object -First 1 }
function Get-FeatureStates([string]$productCode) {
    $s = @{}
    foreach ($f in $features) {
        try { $s[$f] = [int]$inst.GetType().InvokeMember('FeatureState', 'GetProperty', $null, $inst, @($productCode, $f)) } catch { $s[$f] = -9 }
    }
    return $s
}
function Format-States($s) { ($features | ForEach-Object { "$_=$($s[$_])" }) -join ' ' }
function LocalFeatures($s) { @($features | Where-Object { $s[$_] -eq 3 }) }

function Has-Key([string]$sub) { $k = [Microsoft.Win32.Registry]::LocalMachine.OpenSubKey($sub); if ($k) { $k.Close(); return $true }; return $false }
function Get-Shell {
    [pscustomobject]@{
        Verbs = Has-Key 'SOFTWARE\Classes\*\shell\FileDO'
        FdSec = Has-Key 'SOFTWARE\Classes\FileDO.SecureContainer'
        Fdd = Has-Key 'SOFTWARE\Classes\FileDO.DiskContainer'
        Desktop = [bool](Get-ChildItem 'C:\Users\Public\Desktop' -Filter 'FileDO*.lnk' -ErrorAction SilentlyContinue)
        Start = @(Get-ChildItem "$env:ProgramData\Microsoft\Windows\Start Menu\Programs" -Recurse -Filter 'FileDO*.lnk' -ErrorAction SilentlyContinue).Count
        Path = @([Environment]::GetEnvironmentVariable('Path', 'Machine') -split ';' | ForEach-Object { $_.TrimEnd('\') }) -contains 'C:\Program Files\FileDO'
        Files = Test-Path 'C:\Program Files\FileDO\filedo.exe'
    }
}
function Format-Shell($s) { "verbs=$($s.Verbs) fdsec=$($s.FdSec) fdd=$($s.Fdd) desktop=$($s.Desktop) startlinks=$($s.Start) path=$($s.Path) files=$($s.Files)" }

$n = 0
function Run-Msi([string]$what, [string[]]$msiArgs) {
    $script:n++
    $l = Join-Path $msiLogs ("{0:D2}-{1}.log" -f $script:n, $what)
    $p = Start-Process msiexec.exe -ArgumentList (@($msiArgs) + @('/qn', '/norestart', '/l*v', "`"$l`"")) -Wait -PassThru
    return $p.ExitCode
}
function Run-Exe([string]$what, [string]$exe, [string[]]$exeArgs) {
    $script:n++
    $l = Join-Path $msiLogs ("{0:D2}-{1}.log" -f $script:n, $what)
    $p = Start-Process $exe -ArgumentList (@($exeArgs) + @('/quiet', '/norestart', '/log', "`"$l`"")) -Wait -PassThru
    return $p.ExitCode
}
# The cached setup EXE a bundle's Apps-and-features entry runs (its UninstallString is that exe with /modify).
function Get-BundleExe($a) { if ($a.Uninstall -match '^"?([^"]+?.exe)"?') { return $Matches[1] } return $null }
function Remove-All {
    # Whatever is there, by the way a person would: bundles by their own exe, then the MSI by product code.
    foreach ($a in (Get-Arp | Where-Object { -not $_.Msi })) {
        $exe = Get-BundleExe $a
        if ($exe) { Start-Process $exe -ArgumentList '/uninstall','/quiet','/norestart' -Wait | Out-Null }
    }
    foreach ($a in (Get-Arp | Where-Object { $_.Msi })) { Start-Process msiexec.exe -ArgumentList "/x $($a.Key) /qn /norestart" -Wait | Out-Null }
}
function Assert-Clean([string]$name) {
    $arp = @(Get-Arp); $sh = Get-Shell
    Check "$name-removed-clean" (($arp.Count -eq 0) -and -not $sh.Verbs -and -not $sh.FdSec -and -not $sh.Fdd -and -not $sh.Desktop -and ($sh.Start -eq 0) -and -not $sh.Path -and -not $sh.Files) "arp=$($arp.Count) $(Format-Shell $sh)"
}
function Version-Of([string]$msi) {
    $d = $inst.GetType().InvokeMember('OpenDatabase', 'InvokeMethod', $null, $inst, @($msi, 0))
    $v = $d.GetType().InvokeMember('OpenView', 'InvokeMethod', $null, $d, @("SELECT Value FROM Property WHERE Property='ProductVersion'"))
    $v.GetType().InvokeMember('Execute', 'InvokeMethod', $null, $v, $null) | Out-Null
    $r = $v.GetType().InvokeMember('Fetch', 'InvokeMethod', $null, $v, $null)
    return $r.GetType().InvokeMember('StringData', 'GetProperty', $null, $r, @(1))
}

$vA = Version-Of $Msi; $vB = Version-Of $MsiB; $vPrev = Version-Of $PrevMsi
Say "FileDO installer proof $(Get-Date -Format s) on $env:COMPUTERNAME"
Say "A=$vA (build under test) B=$vB (later, same day) Prev=$vPrev (last published)"
Check 'versions-ordered' (([version]$vPrev -lt [version]$vA) -and ([version]$vA -lt [version]$vB)) "$vPrev < $vA < $vB"

if (Get-Process filedo_win, filedo -ErrorAction SilentlyContinue) { throw 'a FileDO process is running; close it first' }

# ---- baseline -------------------------------------------------------------------------------------
$base = @(Get-Arp)
$baseProd = Get-MsiProduct
$baseLocal = @()
if ($baseProd) {
    $bs = Get-FeatureStates $baseProd.Key
    $baseLocal = LocalFeatures $bs
    Say "baseline: $($baseProd.Version) product $($baseProd.Key); $(Format-States $bs)"
} else { Say 'baseline: no FileDO installed' }
$baseShell = Get-Shell; Say "baseline shell: $(Format-Shell $baseShell)"

try {
    # ---- 1. PKG-01 -------------------------------------------------------------------------------
    Say ''; Say '== PKG-01: upgrades on Windows Installer versions =='
    if ($baseProd) {
        $rc = Run-Msi 'A-over-installed' @('/i', "`"$Msi`"")
        $v = @(Get-Visible); $p = Get-MsiProduct
        Check 'A-over-installed-exit' ($rc -eq 0) "exit $rc"
        Check 'A-over-installed-one-entry' (($v.Count -eq 1) -and ($v[0].Version -eq $vA)) "entries=$($v.Count) version=$($v.Version -join ',')"
        $st = Get-FeatureStates $p.Key
        Check 'A-over-installed-features-kept' ((($baseLocal | Sort-Object) -join ',') -eq ((LocalFeatures $st | Sort-Object) -join ',')) "before=$($baseLocal -join ',') after=$((LocalFeatures $st) -join ',')"
    }
    $rc = Run-Msi 'B-over-A' @('/i', "`"$MsiB`"")
    $v = @(Get-Visible)
    Check 'B-over-A-exit' ($rc -eq 0) "exit $rc"
    Check 'B-over-A-one-entry-B' (($v.Count -eq 1) -and ($v[0].Version -eq $vB)) "entries=$($v.Count) version=$($v.Version -join ',') (two entries = the old PKG-01 defect)"
    $rc = Run-Msi 'A-over-B' @('/i', "`"$Msi`"")
    $v = @(Get-Visible)
    $lg = Get-ChildItem $msiLogs -Filter '*A-over-B.log' | Select-Object -Last 1
    $said = [bool](Select-String -Path $lg.FullName -Pattern 'A newer version of FileDO is already installed' -Quiet)
    Check 'A-over-B-refused' (($rc -eq 1603) -and $said) "exit $rc (WiX's downgrade block: 1603 with its message: $said)"
    Check 'A-over-B-still-B' (($v.Count -eq 1) -and ($v[0].Version -eq $vB)) "version=$($v.Version -join ',')"
    Remove-All; Assert-Clean 'B'

    $rc = Run-Msi 'A-fresh' @('/i', "`"$Msi`"")
    $sh = Get-Shell; $p = Get-MsiProduct
    Check 'A-fresh-exit' ($rc -eq 0) "exit $rc"
    Check 'A-fresh-everything' ($sh.Verbs -and $sh.FdSec -and $sh.Fdd -and $sh.Desktop -and ($sh.Start -ge 2) -and $sh.Path -and $sh.Files) (Format-Shell $sh)
    Check 'A-fresh-one-entry' (@(Get-Visible).Count -eq 1) "version=$(@(Get-Visible).Version)"
    $cli = cmd.exe /c '"C:\Program Files\FileDO\filedo.exe" --version 2>&1' | Out-String
    Check 'A-fresh-cli-runs' ($cli -match '\d{10}') ($cli.Trim())
    Remove-All; Assert-Clean 'A'

    $rc = Run-Msi 'A-main-only' @('/i', "`"$Msi`"", 'ADDLOCAL=Main')
    $sh = Get-Shell
    Check 'A-main-only' (($rc -eq 0) -and -not $sh.Verbs -and -not $sh.FdSec -and -not $sh.Fdd -and -not $sh.Desktop -and ($sh.Start -ge 2) -and $sh.Path) "exit $rc $(Format-Shell $sh)"
    Remove-All; Assert-Clean 'A-main'

    $rc = Run-Msi 'prev-two-features' @('/i', "`"$PrevMsi`"", 'ADDLOCAL=Main,ExplorerIntegration')
    $p = Get-MsiProduct
    $pst = Get-FeatureStates $p.Key
    Check 'prev-installed' (($rc -eq 0) -and ($p.Version -eq $vPrev)) "exit $rc version=$($p.Version) $(Format-States $pst)"
    $rc = Run-Msi 'A-over-prev' @('/i', "`"$Msi`"")
    $v = @(Get-Visible); $p = Get-MsiProduct; $st = Get-FeatureStates $p.Key
    Check 'A-over-prev-exit' ($rc -eq 0) "exit $rc"
    Check 'A-over-prev-one-entry' (($v.Count -eq 1) -and ($v[0].Version -eq $vA)) "entries=$($v.Count) version=$($v.Version -join ',')"
    Check 'A-over-prev-choice-kept' (($st['DesktopShortcut'] -ne 3) -and ($st['ExplorerIntegration'] -eq 3) -and ($st['Main'] -eq 3)) (Format-States $st)
    Remove-All; Assert-Clean 'prev-A'

    # ---- 2. PKG-02 -------------------------------------------------------------------------------
    Say ''; Say '== PKG-02: the setup EXE =='
    $rc = Run-Exe 'prev-setup' $PrevSetup @()
    $v = @(Get-Visible)
    Check 'prev-setup-installed' (($rc -eq 0) -and ($v.Count -eq 1)) "exit $rc entries=$($v.Count) version=$($v.Version -join ',')"
    $rc = 0
    $p = Get-MsiProduct
    $m = Start-Process msiexec.exe -ArgumentList "/i $($p.Key) REMOVE=ExplorerIntegration /qn /norestart" -Wait -PassThru
    $st = Get-FeatureStates $p.Key
    Check 'prev-maintenance-removed-a-feature' (($m.ExitCode -eq 0) -and ($st['ExplorerIntegration'] -ne 3)) "exit $($m.ExitCode) $(Format-States $st)"

    $rc = Run-Exe 'A-setup-over-prev-setup' $Setup @()
    $v = @(Get-Visible); $all = @(Get-Arp); $p = Get-MsiProduct
    Check 'A-setup-exit' ($rc -eq 0) "exit $rc"
    Check 'A-setup-one-visible-entry' (($v.Count -eq 1) -and ($v[0].Version -eq $vA)) "visible=$($v.Count) version=$($v.Version -join ',') all=$($all.Count)"
    Check 'A-setup-change-button' (($v.Count -eq 1) -and ($v[0].Uninstall -match '/modify') -and ($v[0].NoModify -ne 1)) "UninstallString=$($v[0].Uninstall) NoModify=$($v[0].NoModify) (DisableModify=button: one Uninstall/Change button that runs the bundle with /modify)"
    Check 'A-setup-hides-the-msi-entry' (@($all | Where-Object { $_.Msi -and -not $_.Hidden }).Count -eq 0) "msi entries hidden=$(@($all | Where-Object { $_.Msi -and $_.Hidden }).Count)"
    $st = Get-FeatureStates $p.Key
    Check 'A-setup-choice-kept' (($st['ExplorerIntegration'] -ne 3) -and ($st['Main'] -eq 3)) (Format-States $st)

    $rc = Run-Exe 'B-setup-over-A-setup' $SetupB @()
    $v = @(Get-Visible)
    Check 'B-setup-one-visible-entry' (($rc -eq 0) -and ($v.Count -eq 1) -and ($v[0].Version -eq $vB)) "exit $rc visible=$($v.Count) version=$($v.Version -join ',')"

    $rc = Run-Exe 'A-setup-over-B-setup' $Setup @()
    $v = @(Get-Visible)
    Check 'A-setup-over-B-not-downgraded' (($v.Count -eq 1) -and ($v[0].Version -eq $vB)) "exit $rc version=$($v.Version -join ',')"

    # removal, the way Apps and features does it
    $exe = Get-BundleExe (@(Get-Visible)[0])
    Say "bundle exe: $exe"
    $m = Start-Process $exe -ArgumentList '/uninstall','/quiet','/norestart' -Wait -PassThru
    Assert-Clean 'setup'
}
finally {
    # ---- put the machine back ---------------------------------------------------------------------
    Say ''; Say '== restore =='
    try {
        Remove-All
        if ($baseProd) {
            $add = if ($baseLocal.Count) { "ADDLOCAL=$($baseLocal -join ',')" } else { 'ADDLOCAL=Main' }
            $rc = Run-Msi 'restore-A' @('/i', "`"$Msi`"", $add)
            $p = Get-MsiProduct
            Say "restored A $($p.Version) with $($baseLocal -join ',') exit $rc; $(Format-Shell (Get-Shell))"
            Check 'restore-matches-baseline-shell' ((Get-Shell).Verbs -eq $baseShell.Verbs -and (Get-Shell).Desktop -eq $baseShell.Desktop) ''
        } else { Say 'nothing to restore (no FileDO at the start)' }
    } catch { Say "RESTORE FAILED: $_" }
    Say ''
    Say "RESULT pass=$($script:pass) fail=$($script:fail)"
}
