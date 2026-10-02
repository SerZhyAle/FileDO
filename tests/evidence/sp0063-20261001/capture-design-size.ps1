<#
.SYNOPSIS
  Screenshot a FileDO window on a named display, for the DPI proofs of SP-0016 T4.

.DESCRIPTION
  Starts filedo_win.exe (the shell, or the Disk manager with -Window disks), puts it on the named
  display either by launching it straight there (-Path launch: the saved placement is on that
  display, as after a close on it) or by opening it on the primary display and dragging it across
  (-Path drag), and grabs the window from the real screen. It reports the window's DPI and the
  height of the rail's first row for the shell, so a run is also a number and not only a picture.

  Everything it changes is put back: HKCU\Software\FileDO is snapshotted first and restored in a
  finally block (a live run writes placement, language and theme values), the process is killed
  (a graceful close would save the placement). Close any open FileDO window first: a second start
  of the shell hands over to the first.

  Runs under Windows PowerShell 5.1 (the GUI is a .NET Framework exe).

.EXAMPLE
  .\tests\capture-dpi.ps1 -Exe filedo_win_vb\bin\Release\filedo_win.exe -Monitor \\.\DISPLAY5 -Path launch -OutDir $env:TEMP\dpi
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Exe,
    [Parameter(Mandatory)][string]$Monitor,
    [ValidateSet('launch', 'drag')][string]$Path = 'launch',
    [ValidateSet('shell', 'disks')][string]$Window = 'shell',
    [ValidateSet('light', 'dark')][string]$Theme = 'light',
    [string]$Language = 'en',
    [int]$ClientWidth = 1400,
    [int]$ClientHeight = 900,
    # A key to press in the window once it is up (a SendKeys key name, e.g. F1); the shot is then of the
    # other top-level window the process opened (a dialog or the help), not of the main one.
    [string]$Key = "",
    [Parameter(Mandatory)][string]$OutDir
)
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSEdition -ne 'Desktop') {
    $fwd = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $PSCommandPath)
    foreach ($k in $PSBoundParameters.Keys) { $fwd += "-$k"; $fwd += [string]$PSBoundParameters[$k] }
    & powershell.exe @fwd
    exit $LASTEXITCODE
}
$Exe = (Resolve-Path $Exe).Path
Add-Type -AssemblyName System.Drawing, System.Windows.Forms
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class CdWin {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
    [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
    [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr a, int x, int y, int cx, int cy, uint f);
    [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    public delegate bool EnumProc(IntPtr h, IntPtr l);
    [DllImport("user32.dll")] static extern bool EnumWindows(EnumProc p, IntPtr l);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
    [DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
    public static IntPtr OtherWindow(uint pid, IntPtr main) {
        IntPtr found = IntPtr.Zero;
        EnumWindows(delegate (IntPtr h, IntPtr l) { uint p; GetWindowThreadProcessId(h, out p); if (p == pid && h != main && IsWindowVisible(h)) { RECT r; GetWindowRect(h, out r); if (r.Right - r.Left > 100) { found = h; return false; } } return true; }, IntPtr.Zero);
        return found;
    }
    [DllImport("user32.dll")] public static extern IntPtr MonitorFromPoint(POINT p, uint f);
    [DllImport("shcore.dll")] public static extern int GetDpiForMonitor(IntPtr m, int t, out uint dx, out uint dy);
    public static int DpiAtPoint(int x, int y) { POINT p; p.X = x; p.Y = y; uint dx, dy; return GetDpiForMonitor(MonitorFromPoint(p, 2), 0, out dx, out dy) == 0 ? (int)dx : 96; }
}
'@
[void][CdWin]::SetProcessDpiAwarenessContext([IntPtr](-4))

$keyPath = 'Software\FileDO'
function Get-Snapshot {
    $snap = @{}
    $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($keyPath)
    if ($k) { foreach ($n in $k.GetValueNames()) { $snap[$n] = @{ Value = $k.GetValue($n, $null, 'DoNotExpandEnvironmentNames'); Kind = $k.GetValueKind($n) } }; $k.Close() }
    return $snap
}
function Restore-Snapshot($snap) {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath)
    foreach ($n in @($k.GetValueNames())) { if (-not $snap.ContainsKey($n)) { $k.DeleteValue($n) } }
    foreach ($n in $snap.Keys) { $k.SetValue($n, $snap[$n].Value, $snap[$n].Kind) }
    $k.Close()
}
function Set-Gui([string]$n, $v, [string]$kind = 'String') {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath)
    $k.SetValue($n, $v, [Microsoft.Win32.RegistryValueKind]$kind); $k.Close()
}

if (Get-Process filedo_win -ErrorAction SilentlyContinue) { throw 'a filedo_win window is already open; close it first' }
$target = [System.Windows.Forms.Screen]::AllScreens | Where-Object { $_.DeviceName -eq $Monitor } | Select-Object -First 1
if (-not $target) { throw "no display named '$Monitor'" }
$primary = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea
$wa = $target.WorkingArea
$prefix = if ($Window -eq 'disks') { 'DiskManager' } else { 'Shell' }
$startArea = if ($Path -eq 'launch') { $wa } else { $primary }
$startDpi = if ($Path -eq 'launch') { [CdWin]::DpiAtPoint($wa.Left + 50, $wa.Top + 50) } else { 0 }

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$snapshot = Get-Snapshot
$proc = $null
try {
    Set-Gui 'GuiLang' $Language
    Set-Gui 'ShellTheme' $Theme
    Set-Gui 'ShellRailCollapsed' 'rail_group_tidy;rail_group_erase;rail_group_protect;rail_group_disks;rail_group_program'
    Set-Gui 'DiskManagerWelcomed' 1 'DWord'
    foreach ($p in 'Shell', 'DiskManager') {
        Set-Gui "${p}PlacementV" 3 'DWord'
        Set-Gui "${p}X" ($startArea.Left + 40) 'DWord'; Set-Gui "${p}Y" ($startArea.Top + 40) 'DWord'
        Set-Gui "${p}W" $ClientWidth 'DWord'; Set-Gui "${p}H" $ClientHeight 'DWord'; Set-Gui "${p}Max" 0 'DWord'
        if ($startDpi) { Set-Gui "${p}Dpi" $startDpi 'DWord' } else { $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath); if ($k.GetValueNames() -contains "${p}Dpi") { $k.DeleteValue("${p}Dpi") }; $k.Close() }
    }
    $argv = if ($Window -eq 'disks') { @('--disks') } else { @() }
    $scratch = Join-Path $env:TEMP 'filedo-capture-dpi-state'
    New-Item -ItemType Directory -Force -Path $scratch | Out-Null
    $env:FILEDO_STATE_DIR = $scratch
    $proc = if ($argv.Count) { Start-Process $Exe -ArgumentList $argv -PassThru -WorkingDirectory $env:TEMP } else { Start-Process $Exe -PassThru -WorkingDirectory $env:TEMP }
    $h = [IntPtr]::Zero
    for ($i = 0; $i -lt 60 -and $h -eq [IntPtr]::Zero; $i++) {
        Start-Sleep -Milliseconds 500
        $proc.Refresh()
        if ($proc.HasExited) { throw 'the window exited at start' }
        $h = $proc.MainWindowHandle
    }
    if ($h -eq [IntPtr]::Zero) { throw 'the window did not appear' }
    Start-Sleep -Milliseconds 1500
    if ($Path -eq 'drag') {
        [void][CdWin]::SetWindowPos($h, [IntPtr]::Zero, $wa.Left + 40, $wa.Top + 40, 0, 0, 0x0001 -bor 0x0004)
        Start-Sleep -Milliseconds 2000
    }
    [void][CdWin]::SetWindowPos($h, [IntPtr](-1), 0, 0, 0, 0, 0x0001 -bor 0x0002 -bor 0x0040)   # topmost, no move, no size
    $actualDpi = [CdWin]::GetDpiForWindow($h)
    [void][CdWin]::SetWindowPos($h, [IntPtr]::Zero, 0, 0, [int]($ClientWidth * $actualDpi / 96), [int]($ClientHeight * $actualDpi / 96), 0x0002 -bor 0x0004)
    [void][CdWin]::SetForegroundWindow($h)
    [void][CdWin]::SetCursorPos($wa.Right - 5, $wa.Bottom - 5)
    Start-Sleep -Milliseconds 1200
    if ($Key) {
        [System.Windows.Forms.SendKeys]::SendWait("{$Key}")
        Start-Sleep -Milliseconds 2500
        $h2 = [CdWin]::OtherWindow([uint32]$proc.Id, $h)
        if ($h2 -eq [IntPtr]::Zero) { throw "no second window appeared after the keys" }
        $h = $h2
        [void][CdWin]::SetWindowPos($h, [IntPtr](-1), 0, 0, 0, 0, 0x0001 -bor 0x0002 -bor 0x0040)
        Start-Sleep -Milliseconds 800
    }
    $dpi = [CdWin]::GetDpiForWindow($h)
    $cr = New-Object CdWin+RECT; $wr = New-Object CdWin+RECT
    [void][CdWin]::GetClientRect($h, [ref]$cr); [void][CdWin]::GetWindowRect($h, [ref]$wr)
    $pt = New-Object CdWin+POINT; [void][CdWin]::ClientToScreen($h, [ref]$pt)
    $w = $cr.Right - $cr.Left; $hh = $cr.Bottom - $cr.Top
    $bmp = New-Object System.Drawing.Bitmap($w, $hh)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($pt.X, $pt.Y, 0, 0, (New-Object System.Drawing.Size($w, $hh)))
    $g.Dispose()
    $name = "$Window$(if ($Key) { "-dialog" })-$Path-$([int]($dpi * 100 / 96))pct-$Theme.png"
    $bmp.Save((Join-Path $OutDir $name), [System.Drawing.Imaging.ImageFormat]::Png)
    $bmp.Dispose()
    $onTarget = ($wr.Left -ge $target.Bounds.Left -and $wr.Right -le $target.Bounds.Right -and $wr.Top -ge $target.Bounds.Top)
    "{0}: window dpi {1} ({2} %), client {3}x{4}, on the named display: {5}" -f $name, $dpi, [int]($dpi * 100 / 96), $w, $hh, $onTarget
}
finally {
    if ($proc -and -not $proc.HasExited) { $proc.Kill(); $proc.WaitForExit(5000) | Out-Null }
    Restore-Snapshot $snapshot
    Remove-Item Env:FILEDO_STATE_DIR -ErrorAction SilentlyContinue
}

