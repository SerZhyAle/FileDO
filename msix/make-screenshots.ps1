<#
.SYNOPSIS
  Capture the FileDO window, one PNG per page and interface language, for the Store listing.

.DESCRIPTION
  filedo_win.exe is a WinForms window, so unlike a web UI it cannot be driven by a URL. The
  script starts the freshly staged exe once per language and page, sizes the window, chooses the
  page's row in the left rail by doing its accessible default action (APP-BEHAVIOUR rule 9 - the
  row is found by its label, read from filedo_win_vb\Localization.vb), and grabs the client area
  with PrintWindow.

  It takes the foreground for a few seconds per shot and parks the mouse pointer in a corner so
  no row is drawn hovered: run it when the machine is not in use.

  Everything it changes is put back: the developer's HKCU\Software\FileDO values (language,
  theme, placement, folded rail groups) are snapshotted first and restored in a finally block,
  and the demo file it creates for the "Make a file secret" page is deleted. The window is
  killed rather than closed, because a graceful close writes the window placement.

  Output: msix\screenshots\<page>-<store-locale>.png (Partner Center locale codes), PNG,
  never wider than 1920 px. The capture is sized in DESIGN pixels and scaled by the window's
  DPI, so the composition is the same on any display; the Store wants 1366x768 up to 3840x2160.

  Runs under Windows PowerShell 5.1 (UI Automation is a .NET Framework assembly); started from
  PowerShell 7 it re-launches itself.

.PARAMETER Exe
  filedo_win.exe to run. Default: msix\stage\filedo_win.exe (build-msix.ps1 stages it beside
  filedo.exe and filedo_win.exe.config, which the GUI needs).

.PARAMETER Language
  Interface languages (comma separated). Default: en,ru,uk,de,fr - the five the GUI ships.

.PARAMETER Page
  capacity, speed, duplicates, secure, wipe (comma separated). The ORDER is the order of
  DesktopScreenshot1..N in the listing and is shared with build-store-listing-csv.ps1.

.EXAMPLE
  .\msix\make-screenshots.ps1 -Language en -Page capacity     # one shot to look at
#>
[CmdletBinding()]
param(
    [string]$Exe,
    [string]$Language = "en,ru,uk,de,fr",
    [string]$Page = "capacity,speed,duplicates,secure,wipe",
    # dark or light. The Store set is dark; the light set is the APP-STYLE rung-3 pair. The
    # red-cross check below refuses to save a shot in which a control's paint threw.
    [ValidateSet('light', 'dark')]
    [string]$Theme = 'dark',
    # Client size in design pixels; the window's DPI scales it.
    [int]$ClientWidth = 1600,
    [int]$ClientHeight = 1000,
    [string]$OutDir,
    # Grab the real screen (CopyFromScreen) instead of asking the window to print itself. Slower
    # and needs the window unobstructed, but it is exactly what a user sees: use it to tell a
    # real defect from a PrintWindow artefact.
    [switch]$Live
)

$ErrorActionPreference = "Stop"

# UI Automation is a .NET Framework assembly: hand over to Windows PowerShell 5.1.
if ($PSVersionTable.PSEdition -ne 'Desktop') {
    $fwd = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $PSCommandPath)
    foreach ($k in $PSBoundParameters.Keys) {
        $v = $PSBoundParameters[$k]
        if ($v -is [System.Management.Automation.SwitchParameter]) { if ($v.IsPresent) { $fwd += "-$k" } }
        else { $fwd += "-$k"; $fwd += [string]$v }
    }
    & powershell.exe @fwd
    exit $LASTEXITCODE
}

$msix = $PSScriptRoot
if (-not $Exe)    { $Exe = Join-Path $msix "stage\filedo_win.exe" }
if (-not $OutDir) { $OutDir = Join-Path $msix "screenshots" }
$Languages = @($Language -split '[,; ]+' | Where-Object { $_ })
$Pages     = @($Page -split '[,; ]+' | Where-Object { $_ })

if (-not (Test-Path $Exe)) { throw "make-screenshots: $Exe not found. Run msix\build-msix.ps1 first (it stages the exes) or pass -Exe." }
foreach ($need in 'filedo.exe', 'filedo_win.exe.config') {
    if (-not (Test-Path (Join-Path (Split-Path $Exe -Parent) $need))) { throw "make-screenshots: $need is not beside $Exe (the GUI needs both)." }
}
if (Get-Process filedo_win -ErrorAction SilentlyContinue) {
    throw "make-screenshots: a filedo_win window is already open. A second launch would hand over to it (single-instance) and the capture would show the wrong window. Close it first."
}

# language code -> Partner Center locale, and page -> the rail row's localization key (Rail.vb)
# or a launch argument
$StoreLocale = @{ en = 'en-us'; ru = 'ru'; uk = 'uk'; de = 'de'; fr = 'fr' }
$PageRow     = @{ capacity = 'rail_job_capacity'; speed = 'rail_job_speed'; duplicates = 'rail_job_duplicates'; wipe = 'rail_job_wipe'; command = 'rail_job_command' }
foreach ($l in $Languages) { if (-not $StoreLocale.ContainsKey($l)) { throw "make-screenshots: unknown language '$l' (expected $($StoreLocale.Keys -join ' '))." } }
foreach ($p in $Pages)     { if (-not ($PageRow.ContainsKey($p) -or $p -eq 'secure')) { throw "make-screenshots: unknown page '$p' (expected capacity speed duplicates secure wipe command)." } }

Add-Type -AssemblyName System.Drawing, System.Windows.Forms, UIAutomationClient, UIAutomationTypes, WindowsBase
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class FdWin {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr value);
    [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool SetWindowPos(IntPtr h, IntPtr after, int x, int y, int cx, int cy, uint flags);
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
    [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
    [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int cmd);
    [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
    // BGRA pixels; counted in compiled code because a PowerShell loop over ~5M pixels takes seconds.
    public static int CountPureRed(byte[] b) { int n = 0; for (int i = 0; i + 3 < b.Length; i += 4) if (b[i + 2] == 255 && b[i + 1] == 0 && b[i] == 0) n++; return n; }
}
'@
Add-Type -ReferencedAssemblies Accessibility -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class FdMsaa {
    [DllImport("oleacc.dll")]
    static extern int AccessibleObjectFromWindow(IntPtr hwnd, int id, ref Guid iid, [MarshalAs(UnmanagedType.Interface)] out object o);
    static Accessibility.IAccessible Of(IntPtr h) {
        Guid g = new Guid("618736E0-3C3D-11CF-810C-00AA00389B71");   // IID_IAccessible
        object o;
        int hr = AccessibleObjectFromWindow(h, unchecked((int)0xFFFFFFFC), ref g, out o);   // OBJID_CLIENT
        if (hr != 0) throw new Exception("AccessibleObjectFromWindow 0x" + hr.ToString("X8"));
        return (Accessibility.IAccessible)o;
    }
    public static string DefaultAction(IntPtr h) { return Of(h).get_accDefaultAction(0); }
    public static void DoDefault(IntPtr h) { Of(h).accDoDefaultAction(0); }
}
'@
# Per-monitor v2, like the GUI itself: otherwise every rectangle below is virtualized.
[void][FdWin]::SetProcessDpiAwarenessContext([IntPtr](-4))

# --- snapshot the developer's GUI settings; restored in finally -----------------------------
$keyPath = 'Software\FileDO'
function Get-Snapshot {
    $snap = @{}
    $k = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey($keyPath)
    if ($k) {
        foreach ($n in $k.GetValueNames()) { $snap[$n] = @{ Value = $k.GetValue($n, $null, 'DoNotExpandEnvironmentNames'); Kind = $k.GetValueKind($n) } }
        $k.Close()
    }
    return $snap
}
function Restore-Snapshot($snap) {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath)
    foreach ($n in @($k.GetValueNames())) { if (-not $snap.ContainsKey($n)) { $k.DeleteValue($n) } }
    foreach ($n in $snap.Keys) { $k.SetValue($n, $snap[$n].Value, $snap[$n].Kind) }
    $k.Close()
}
function Set-GuiSetting([string]$name, $value, [string]$kind = 'String') {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath)
    $k.SetValue($name, $value, [Microsoft.Win32.RegistryValueKind]$kind)
    $k.Close()
}
function Clear-GuiSetting([string]$name) {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath)
    if ($k.GetValueNames() -contains $name) { $k.DeleteValue($name) }
    $k.Close()
}

$AE = [System.Windows.Automation.AutomationElement]
function Find-ShellWindow([int]$procId) {
    $cond = New-Object System.Windows.Automation.PropertyCondition($AE::ProcessIdProperty, $procId)
    for ($i = 0; $i -lt 80; $i++) {
        foreach ($w in $AE::RootElement.FindAll('Children', $cond)) {
            # The shell's window is titled exactly "FileDO"; a question it asks is a second window.
            if ($w.Current.Name -eq 'FileDO' -and -not $w.Current.IsOffscreen -and $w.Current.NativeWindowHandle -ne 0) { return $w }
        }
        Start-Sleep -Milliseconds 250
    }
    return $null
}

# WinForms replaces a control whose OnPaint threw with a red-bordered box crossed by two red
# lines (pure 255,0,0) and never paints it again. The app's own reds are Theme tokens, never
# pure red, so a few hundred pure-red pixels mean a broken control is in the shot.
function Get-PureRedCount($bmp) {
    $rect = New-Object System.Drawing.Rectangle(0, 0, $bmp.Width, $bmp.Height)
    $data = $bmp.LockBits($rect, [System.Drawing.Imaging.ImageLockMode]::ReadOnly, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
    try {
        $bytes = New-Object byte[] ($data.Stride * $data.Height)
        [System.Runtime.InteropServices.Marshal]::Copy($data.Scan0, $bytes, 0, $bytes.Length)
    } finally { $bmp.UnlockBits($data) }
    return [FdWin]::CountPureRed($bytes)
}

function Save-Png($bmp, [string]$path) {
    $maxW = 1920
    if ($bmp.Width -gt $maxW) {
        $h = [int][Math]::Round($bmp.Height * $maxW / $bmp.Width)
        $small = New-Object System.Drawing.Bitmap($maxW, $h)
        $g = [System.Drawing.Graphics]::FromImage($small)
        $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
        $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::HighQuality
        $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
        $g.DrawImage($bmp, 0, 0, $maxW, $h)
        $g.Dispose()
        $small.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
        $small.Dispose()
    } else {
        $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
    }
}

function Get-RailLabel([string]$lang, [string]$key) {
    # The row's label in the language the window was started in, read from the one table the
    # window reads it from, so the script never carries a second copy of the strings.
    $fn = @{ en = 'EnLines'; ru = 'RuLines'; uk = 'UkLines'; de = 'DeLines'; fr = 'FrLines' }[$lang]
    $src = [System.IO.File]::ReadAllText((Join-Path $msix "..\filedo_win_vb\Localization.vb"))
    $start = $src.IndexOf("Private Function $fn()")
    if ($start -lt 0) { throw "Localization.vb has no $fn table" }
    $m = [regex]::Match($src.Substring($start), '"' + [regex]::Escape($key) + '\|([^"]*)"')
    if (-not $m.Success) { throw "Localization.vb $fn has no $key" }
    return $m.Groups[1].Value
}

function Invoke-RailRow($win, [string]$lang, [string]$key) {
    # A rail row's default action is its click (Rail.vb, APP-BEHAVIOUR rule 9). The managed UI
    # Automation client does not bridge MSAA, so it sees the row only as a named pane: the row is
    # found by its label and its default action is done through MSAA - no pixel is looked for
    # and no mouse is moved.
    $label = Get-RailLabel $lang $key
    $cond = New-Object System.Windows.Automation.AndCondition(
        (New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, $label)),
        (New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, [System.Windows.Automation.ControlType]::Pane)))
    $row = $win.FindFirst('Descendants', $cond)
    if (-not $row) { throw "the rail row '$label' ($key) was not found" }
    $h = [IntPtr]$row.Current.NativeWindowHandle
    if (-not [FdMsaa]::DefaultAction($h)) { throw "the rail row '$label' ($key) has no default action (APP-BEHAVIOUR rule 9 - see Rail.vb)" }
    [FdMsaa]::DoDefault($h)
}

# A neutral path for the "Make a file secret" page: temp paths carry the user name.
$demoDir  = Join-Path $env:PUBLIC "Documents\FileDO-demo"
$demoFile = Join-Path $demoDir "quarterly-report.docx"

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
$snapshot = Get-Snapshot
$work = Split-Path $Exe -Parent
$made = New-Object System.Collections.ArrayList
try {
    if ($Pages -contains 'secure') {
        New-Item -ItemType Directory -Force -Path $demoDir | Out-Null
        [System.IO.File]::WriteAllText($demoFile, "FileDO screenshot demo file`r`n")
    }
    $screen = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea

    foreach ($lang in $Languages) {
        foreach ($pg in $Pages) {
            # settings first: the window reads them once, in its constructor
            Set-GuiSetting 'GuiLang' $lang
            Set-GuiSetting 'ShellTheme' $Theme
            Clear-GuiSetting 'ShellRailCollapsed'
            Set-GuiSetting 'ShellPlacementV' 3 'DWord'
            Clear-GuiSetting 'ShellDpi'                  # no saved DPI: the rectangle is taken as it stands
            Set-GuiSetting 'ShellX' ($screen.Left + 40) 'DWord'
            Set-GuiSetting 'ShellY' ($screen.Top + 40) 'DWord'
            Set-GuiSetting 'ShellW' 1400 'DWord'
            Set-GuiSetting 'ShellH' 900 'DWord'
            Set-GuiSetting 'ShellMax' 0 'DWord'

            $launchArgs = @()
            if ($pg -eq 'secure') { $launchArgs = @("`"$demoFile`"") }
            $proc = if ($launchArgs.Count) { Start-Process $Exe -ArgumentList $launchArgs -PassThru -WorkingDirectory $env:TEMP } else { Start-Process $Exe -PassThru -WorkingDirectory $env:TEMP }
            try {
                $win = Find-ShellWindow $proc.Id
                if (-not $win) { throw "the shell window did not appear for $lang/$pg" }
                $h = [IntPtr]$win.Current.NativeWindowHandle
                Start-Sleep -Milliseconds 800

                # size: client = design size * DPI/96, then move to the top-left of the work area
                $scale = [FdWin]::GetDpiForWindow($h) / 96.0
                $cw = [int][Math]::Round($ClientWidth * $scale); $ch = [int][Math]::Round($ClientHeight * $scale)
                $wr = New-Object FdWin+RECT; $cr = New-Object FdWin+RECT
                [void][FdWin]::GetWindowRect($h, [ref]$wr); [void][FdWin]::GetClientRect($h, [ref]$cr)
                $ncW = ($wr.Right - $wr.Left) - ($cr.Right - $cr.Left); $ncH = ($wr.Bottom - $wr.Top) - ($cr.Bottom - $cr.Top)
                [void][FdWin]::ShowWindow($h, 1)
                # HWND_TOPMOST (-1): the capture is of the window as a user sees it, so nothing may
                # cover it, and SetForegroundWindow alone is refused for a process not in the foreground.
                [void][FdWin]::SetWindowPos($h, [IntPtr](-1), $screen.Left, $screen.Top, $cw + $ncW, $ch + $ncH, 0x0040)  # SHOWWINDOW
                [void][FdWin]::SetForegroundWindow($h)
                Start-Sleep -Milliseconds 1500   # labels re-wrap after a resize

                [void][FdWin]::SetCursorPos($screen.Right - 5, $screen.Bottom - 5)   # out of the window: no hover state in the shot
                if ($PageRow.ContainsKey($pg)) {
                    $win = Find-ShellWindow $proc.Id
                    Invoke-RailRow $win $lang $PageRow[$pg]
                    Start-Sleep -Milliseconds 1200
                } else {
                    Start-Sleep -Milliseconds 800
                }

                [void][FdWin]::GetClientRect($h, [ref]$cr)
                $w = $cr.Right - $cr.Left; $hh = $cr.Bottom - $cr.Top
                $bmp = New-Object System.Drawing.Bitmap($w, $hh)
                $g = [System.Drawing.Graphics]::FromImage($bmp)
                if ($Live) {
                    $origin = New-Object FdWin+POINT
                    [void][FdWin]::ClientToScreen($h, [ref]$origin)
                    $g.CopyFromScreen($origin.X, $origin.Y, 0, 0, (New-Object System.Drawing.Size($w, $hh)))
                    $g.Dispose()
                } else {
                    $hdc = $g.GetHdc()
                    $ok = [FdWin]::PrintWindow($h, $hdc, 3)   # PW_CLIENTONLY | PW_RENDERFULLCONTENT
                    $g.ReleaseHdc($hdc); $g.Dispose()
                    if (-not $ok) { throw "PrintWindow failed for $lang/$pg" }
                }
                $red = Get-PureRedCount $bmp
                if ($red -gt 200) { $bmp.Dispose(); throw "a control in the $lang/$pg shot painted as a WinForms red-cross placeholder ($red pure-red pixels). That is an OnPaint exception in the app: not saved." }
                $out = Join-Path $OutDir ("{0}-{1}.png" -f $pg, $StoreLocale[$lang])
                Save-Png $bmp $out
                $bmp.Dispose()
                $img = [System.Drawing.Image]::FromFile($out)
                Write-Host ("  {0,-24} {1}x{2}  (window {3}x{4} @ {5:P0})" -f (Split-Path $out -Leaf), $img.Width, $img.Height, $w, $hh, $scale) -ForegroundColor Green
                $img.Dispose()
                [void]$made.Add($out)
            } finally {
                # killed, not closed: a graceful close writes the window placement
                if ($proc -and -not $proc.HasExited) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
                Start-Sleep -Milliseconds 500
            }
        }
    }
} finally {
    Restore-Snapshot $snapshot
    if (Test-Path $demoFile) { [System.IO.File]::Delete($demoFile) }
    if ((Test-Path $demoDir) -and -not (Get-ChildItem $demoDir -Force)) { [System.IO.Directory]::Delete($demoDir) }
}
Write-Host "$($made.Count) screenshot(s) written to $OutDir" -ForegroundColor Cyan
