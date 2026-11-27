<#
.SYNOPSIS
  Record the FileDO demo: one silent run through the window - a speed test of a drive, then a duplicate search
  in a photo folder - as docs\assets\demo.mp4, docs\assets\demo.gif and the poster docs\assets\demo-poster.jpg.

.DESCRIPTION
  The window is driven the way make-screenshots.ps1 drives it, without the mouse, the keyboard focus or the
  foreground, so the run neither needs an idle machine nor captures anything but the FileDO window:
    - a rail row, a group header or a button is pressed through its accessible default action (MSAA, APP-BEHAVIOUR
      rule 9), found by its label (read from filedo_win_vb\Localization.vb for the rail);
    - a path is typed into its box as WM_CHAR messages to the box's edit control, so the page sees real typing and
      updates its target and its command line;
    - the page is scrolled with WM_MOUSEWHEEL messages to its scroll panel.
  Frames are the window's own client area (PrintWindow, PW_RENDERFULLCONTENT) taken on a second thread while the
  scenario runs; a mouse pointer and a click ring are drawn onto them afterwards from the log of what was pressed
  where and when, and ffmpeg turns them into the MP4 (H.264, no audio track) and the GIF; the poster is the frame just
  before the speed test starts, as a JPEG.

  Nothing of the machine's own state is shown or kept:
    - HKCU\Software\FileDO is snapshotted first and restored in a finally block; for the run the language is
      English, the theme dark and the run reports off (with reports on, the result names the reports folder,
      which is inside the user's profile);
    - FILEDO_STATE_DIR points at a scratch folder;
    - the demo drive is a subst letter (default T:) over a scratch folder holding generated "photos" - random
      bytes under .jpg names, a few of them copied under other folders and names to be found as duplicates;
    - the session log and the event files the window writes under %LOCALAPPDATA%\FileDO during the run are
      deleted afterwards;
    - the window is killed, never closed, because a graceful close writes the window placement.

  Runs under Windows PowerShell 5.1 (UI Automation is a .NET Framework assembly); started from PowerShell 7 it
  re-launches itself. Needs ffmpeg on PATH. A run takes about a minute; the encode a few seconds more.

  Exit codes: 0 = the three files are written; 1 = the run failed (the reason is printed; nothing in OutDir is
  replaced, since the files are copied there only at the end); 2 = could not start (no exe, no ffmpeg, the window already open, the drive letter taken).

.PARAMETER Exe
  filedo_win.exe to record, with filedo.exe and filedo_win.exe.config beside it. Default: the installed program,
  %ProgramFiles%\FileDO\filedo_win.exe. A release zip extracted to a folder works the same.

.PARAMETER Monitor
  Display to open the window on, by device name (\\.\DISPLAY2). Default: the primary display. The window opens
  at its design size times that display's scaling. Keep the primary display unless it is busy: on a display whose
  scaling differs from the system's, the window's boxes are drawn with a smaller font.

.PARAMETER DesignWidth
  Client size in design pixels (96 DPI). At 1024 by 640 the text stays readable in an 800-pixel GIF; on a display
  at the system scaling every page lays out in full there.

.PARAMETER Drive
  The subst letter the demo files are shown under. It must be free.

.PARAMETER OutDir
  Where demo.mp4, demo.gif and demo-poster.jpg go. Default: docs\assets.

.EXAMPLE
  .\msix\make-demo.ps1 -Monitor \\.\DISPLAY2
#>
[CmdletBinding()]
param(
    [string]$Exe = (Join-Path $env:ProgramFiles 'FileDO\filedo_win.exe'),
    [string]$Monitor,
    [int]$DesignWidth = 1024,
    [int]$DesignHeight = 640,
    [string]$Drive = 'T:',
    [string]$OutDir,
    [string]$WorkDir,
    [int]$Fps = 12,
    [int]$VideoWidth = 1280,
    [int]$GifWidth = 800,
    [switch]$KeepWork
)
$ErrorActionPreference = 'Stop'

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

$repo = Split-Path $PSScriptRoot -Parent
if (-not $OutDir) { $OutDir = Join-Path $repo 'docs\assets' }
if (-not $WorkDir) { $WorkDir = Join-Path $repo 'temp\make-demo' }
function Stop-Demo([int]$code, [string]$msg) {
    if ($code -eq 2) { Write-Host "make-demo: COULD NOT START ($msg)" -ForegroundColor Yellow } else { Write-Host "make-demo: FAIL ($msg)" -ForegroundColor Red }
    exit $code
}
if (-not (Test-Path -LiteralPath $Exe)) { Stop-Demo 2 "$Exe not found (pass -Exe)" }
foreach ($need in 'filedo.exe', 'filedo_win.exe.config') {
    if (-not (Test-Path (Join-Path (Split-Path $Exe -Parent) $need))) { Stop-Demo 2 "$need is not beside $Exe (the window needs both)" }
}
if (Get-Process filedo_win -ErrorAction SilentlyContinue) { Stop-Demo 2 'a filedo_win window is already open; a second launch hands over to it (single instance)' }
$ffmpeg = (Get-Command ffmpeg -ErrorAction SilentlyContinue).Source
if (-not $ffmpeg) { Stop-Demo 2 'ffmpeg is not on PATH' }
if ($Drive -notmatch '^[A-Za-z]:$') { Stop-Demo 2 "-Drive '$Drive' is not a drive letter like T:" }
if (Test-Path "$Drive\") { Stop-Demo 2 "$Drive is in use; pass a free letter with -Drive" }
$version = (& (Join-Path (Split-Path $Exe -Parent) 'filedo.exe') version 2>&1 | Select-Object -First 1)
Write-Host "make-demo: recording $Exe ($version)"

Add-Type -AssemblyName System.Drawing, System.Windows.Forms, UIAutomationClient, UIAutomationTypes, WindowsBase
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class FdDemo {
    [StructLayout(LayoutKind.Sequential)] public struct RECT { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential)] public struct POINT { public int X, Y; }
    [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr value);
    [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
    [DllImport("user32.dll")] public static extern bool ClientToScreen(IntPtr h, ref POINT p);
    [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint flags);
    [DllImport("user32.dll")] public static extern uint GetDpiForWindow(IntPtr h);
    [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h, int msg, IntPtr w, IntPtr l);
    [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr h, int msg, IntPtr w, IntPtr l);
    [DllImport("user32.dll", CharSet = CharSet.Unicode)] public static extern IntPtr FindWindowEx(IntPtr p, IntPtr after, string cls, string title);
    [DllImport("user32.dll")] public static extern IntPtr MonitorFromPoint(POINT p, uint flags);
    [DllImport("shcore.dll")] public static extern int GetDpiForMonitor(IntPtr mon, int type, out uint dx, out uint dy);
    public static int DpiAtPoint(int x, int y) { POINT p; p.X = x; p.Y = y; uint dx, dy; IntPtr m = MonitorFromPoint(p, 2); return GetDpiForMonitor(m, 0, out dx, out dy) == 0 ? (int)dx : 96; }
}
'@
Add-Type -ReferencedAssemblies Accessibility -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class FdDemoMsaa {
    [DllImport("oleacc.dll")]
    static extern int AccessibleObjectFromWindow(IntPtr hwnd, int id, ref Guid iid, [MarshalAs(UnmanagedType.Interface)] out object o);
    static Accessibility.IAccessible Of(IntPtr h) {
        Guid g = new Guid("618736E0-3C3D-11CF-810C-00AA00389B71");
        object o;
        int hr = AccessibleObjectFromWindow(h, unchecked((int)0xFFFFFFFC), ref g, out o);
        if (hr != 0) throw new Exception("AccessibleObjectFromWindow 0x" + hr.ToString("X8"));
        return (Accessibility.IAccessible)o;
    }
    public static void DoDefault(IntPtr h) { Of(h).accDoDefaultAction(0); }
}
'@
[void][FdDemo]::SetProcessDpiAwarenessContext([IntPtr](-4))

# ---- settings snapshot (restored in finally) -------------------------------------------------------------
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
function Set-GuiSetting([string]$name, $value, [string]$kind = 'String') {
    $k = [Microsoft.Win32.Registry]::CurrentUser.CreateSubKey($keyPath); $k.SetValue($name, $value, [Microsoft.Win32.RegistryValueKind]$kind); $k.Close()
}

# ---- UI Automation helpers --------------------------------------------------------------------------------
$AE = [System.Windows.Automation.AutomationElement]
$CT = [System.Windows.Automation.ControlType]
function Find-ShellWindow([int]$procId) {
    $cond = New-Object System.Windows.Automation.PropertyCondition($AE::ProcessIdProperty, $procId)
    for ($i = 0; $i -lt 80; $i++) {
        foreach ($w in $AE::RootElement.FindAll('Children', $cond)) { if ($w.Current.Name -eq 'FileDO' -and $w.Current.NativeWindowHandle -ne 0) { return $w } }
        Start-Sleep -Milliseconds 250
    }
    return $null
}
function Get-Label([string]$key) {
    $src = [System.IO.File]::ReadAllText((Join-Path $repo 'filedo_win_vb\Localization.vb'))
    $start = $src.IndexOf('Private Function EnLines()')
    if ($start -lt 0) { throw 'Localization.vb has no EnLines table' }
    $m = [regex]::Match($src.Substring($start), '"' + [regex]::Escape($key) + '\|([^"]*)"')
    if (-not $m.Success) { throw "Localization.vb EnLines has no $key" }
    return $m.Groups[1].Value
}
function Find-Element([string]$name, $type, [switch]$IgnoreCase) {
    $win = Find-ShellWindow $script:proc.Id
    for ($i = 0; $i -lt 40; $i++) {
        $win = Find-ShellWindow $script:proc.Id
        foreach ($e in $win.FindAll('Descendants', (New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, $type)))) {
            $n = $e.Current.Name
            if ($n -ceq $name -or ($IgnoreCase -and $n -ieq $name)) { return $e }
        }
        Start-Sleep -Milliseconds 150
    }
    $seen = @($win.FindAll('Descendants', (New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, $type))) | ForEach-Object { $_.Current.Name } | Where-Object { $_ } | Select-Object -First 12)
    throw "the $($type.ProgrammaticName) '$name' was not found (the window shows $($seen.Count) named: $($seen -join ' | '))"
}

# ---- frame coordinates and the pointer log ------------------------------------------------------------------
$script:actions = New-Object System.Collections.ArrayList   # @{ T = ms; X; Y; Click }
$moveMs = 650
function Get-FramePoint($el, [double]$fx = 0.5, [double]$fy = 0.5) {
    $r = $el.Current.BoundingRectangle
    $o = New-Object FdDemo+POINT; [void][FdDemo]::ClientToScreen($script:hwnd, [ref]$o)
    return @{ X = ($r.X + $r.Width * $fx - $o.X) * $script:frameScale; Y = ($r.Y + $r.Height * $fy - $o.Y) * $script:frameScale }
}
# The pointer arrives at the element, then the element acts: the log entry is written for the arrival time.
function Move-To($el, [switch]$Click, [double]$fx = 0.5, [double]$fy = 0.5) {
    $p = Get-FramePoint $el $fx $fy
    [void]$script:actions.Add(@{ T = $script:sync.Clock.ElapsedMilliseconds + $moveMs; X = $p.X; Y = $p.Y; Click = [bool]$Click })
    Start-Sleep -Milliseconds ($moveMs + 60)
}
function Press($el, [double]$fx = 0.5) {
    Move-To $el -Click -fx $fx
    [FdDemoMsaa]::DoDefault([IntPtr]$el.Current.NativeWindowHandle)
}
function Type-Path([string]$label, [string]$text) {
    $box = Find-Element $label $CT::ComboBox
    Move-To $box -Click -fx 0.08
    $edit = [FdDemo]::FindWindowEx([IntPtr]$box.Current.NativeWindowHandle, [IntPtr]::Zero, 'Edit', $null)
    if ($edit -eq [IntPtr]::Zero) { throw "the box '$label' has no edit control" }
    [void][FdDemo]::SendMessage($edit, 0x00B1, [IntPtr]0, [IntPtr](-1))   # EM_SETSEL: everything
    [void][FdDemo]::PostMessage($edit, 0x0102, [IntPtr]8, [IntPtr]1)       # WM_CHAR backspace: clear it
    Start-Sleep -Milliseconds 250
    foreach ($ch in $text.ToCharArray()) { [void][FdDemo]::PostMessage($edit, 0x0102, [IntPtr][int]$ch, [IntPtr]1); Start-Sleep -Milliseconds 110 }
}
function Find-Scroller {
    $win = Find-ShellWindow $script:proc.Id
    $walker = [System.Windows.Automation.TreeWalker]::ControlViewWalker
    foreach ($p in $win.FindAll('Descendants', (New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, $CT::Pane)))) {
        $c = $walker.GetFirstChild($p)
        if ($c -and -not $walker.GetNextSibling($c) -and $c.Current.BoundingRectangle.Height -gt ($p.Current.BoundingRectangle.Height + 50) -and $p.Current.BoundingRectangle.Width -gt 400) { return $p }
    }
    return $null   # the page fits: nothing to scroll
}
# Scroll the page until the named button sits in its lower part, one wheel notch at a time (a page that fits
# the window has no scroll panel and is left as it is).
function Scroll-To([string]$buttonName) {
    $sc = Find-Scroller
    if (-not $sc) { return }
    Move-To $sc -fx 0.55 -fy 0.55
    $r = $sc.Current.BoundingRectangle
    $lp = [IntPtr]((([int]($r.Y + $r.Height * 0.55)) -shl 16) -bor ([int]($r.X + $r.Width * 0.55) -band 0xFFFF))
    for ($i = 0; $i -lt 25; $i++) {
        $b = (Find-Element $buttonName $CT::Pane).Current.BoundingRectangle
        if ($b.Bottom -lt ($r.Bottom - $r.Height * 0.12)) { break }
        [void][FdDemo]::SendMessage([IntPtr]$sc.Current.NativeWindowHandle, 0x020A, [IntPtr]((-120) -shl 16), $lp)
        Start-Sleep -Milliseconds 140
    }
    Start-Sleep -Milliseconds 400
}
function Wait-Result([int]$timeoutSec = 60) {
    $label = Get-Label 'shell_btn_run_again'
    $deadline = (Get-Date).AddSeconds($timeoutSec)
    while ((Get-Date) -lt $deadline) {
        $win = Find-ShellWindow $script:proc.Id
        $c = New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, $label)
        if ($win.FindFirst('Descendants', $c)) { return }
        Start-Sleep -Milliseconds 200
    }
    throw "no result after $timeoutSec s"
}

# The target box opens on a drive row, "C:\ (<label>, NTFS, ..)", and the label and sizes are the recording
# machine's own. Its text is rewritten to the bare root through the Value pattern before the first frame; the page
# still reads "C:\" as the same drive (make-screenshots.ps1 does the same, AUD-61-F5).
function Hide-VolumeLabel {
    $win = Find-ShellWindow $script:proc.Id
    foreach ($c in $win.FindAll('Descendants', (New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, $CT::ComboBox)))) {
        $vp = $null
        if (-not $c.TryGetCurrentPattern([System.Windows.Automation.ValuePattern]::Pattern, [ref]$vp)) { continue }
        $m = [regex]::Match([string]$vp.Current.Value, '^([A-Za-z]:\\) \(')
        if ($m.Success) { $vp.SetValue($m.Groups[1].Value) }
    }
}
# ---- the frame grabber (second thread) ----------------------------------------------------------------------
$grabber = {
    param($sync)
    $h = $sync.Hwnd; $n = 0
    $interval = 1000.0 / $sync.Fps
    while (-not $sync.Stop) {
        $t = $sync.Clock.ElapsedMilliseconds
        $cr = New-Object FdDemo+RECT; [void][FdDemo]::GetClientRect($h, [ref]$cr)
        $full = New-Object System.Drawing.Bitmap($cr.Right, $cr.Bottom)
        $g = [System.Drawing.Graphics]::FromImage($full); $hdc = $g.GetHdc()
        $ok = [FdDemo]::PrintWindow($h, $hdc, 3)
        $g.ReleaseHdc($hdc); $g.Dispose()
        if ($ok) {
            $small = New-Object System.Drawing.Bitmap($sync.W, $sync.H)
            $gs = [System.Drawing.Graphics]::FromImage($small)
            $gs.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBilinear
            $gs.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::Half
            $gs.DrawImage($full, 0, 0, $sync.W, $sync.H); $gs.Dispose()
            $small.Save((Join-Path $sync.Frames ('f{0:D5}.jpg' -f $n)), $sync.Jpeg, $sync.JpegParams); $small.Dispose()
            [void]$sync.Times.Add($t); $n++
        }
        $full.Dispose()
        $wait = [int]($interval - ($sync.Clock.ElapsedMilliseconds - $t))
        if ($wait -gt 0) { Start-Sleep -Milliseconds $wait }
    }
}

# ---- demo data ----------------------------------------------------------------------------------------------
if (Test-Path -LiteralPath $WorkDir) { Remove-Item -LiteralPath $WorkDir -Recurse -Force }
$driveDir = Join-Path $WorkDir 'drive'; $frames = Join-Path $WorkDir 'frames'; $stateDir = Join-Path $WorkDir 'state'
foreach ($d in (Join-Path $driveDir 'Photos\2025'), (Join-Path $driveDir 'Photos\Phone backup'), (Join-Path $driveDir 'Photos\Holiday'), $frames, $stateDir) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
$rnd = New-Object System.Random 2611
$photos = @(foreach ($i in 0..23) {
    $p = Join-Path $driveDir ('Photos\2025\IMG_{0}.jpg' -f (2041 + $i * 3))
    $bytes = New-Object byte[] ($rnd.Next(1800000, 4600000)); $rnd.NextBytes($bytes); [IO.File]::WriteAllBytes($p, $bytes); $p
})
foreach ($i in 0..5) { $bytes = New-Object byte[] ($rnd.Next(1800000, 4600000)); $rnd.NextBytes($bytes); [IO.File]::WriteAllBytes((Join-Path $driveDir ('Photos\Phone backup\IMG_{0}.jpg' -f (3105 + $i))), $bytes) }
foreach ($i in 2, 5, 9, 13, 17) { Copy-Item $photos[$i] (Join-Path $driveDir 'Photos\Phone backup') }
$names = @{ 4 = 'beach.jpg'; 9 = 'harbour.jpg'; 20 = 'sunset.jpg' }
foreach ($i in $names.Keys) { Copy-Item $photos[$i] (Join-Path $driveDir ('Photos\Holiday\' + $names[$i])) }
foreach ($n in 'family.jpg', 'old town.jpg') { $bytes = New-Object byte[] ($rnd.Next(1800000, 4600000)); $rnd.NextBytes($bytes); [IO.File]::WriteAllBytes((Join-Path $driveDir "Photos\Holiday\$n"), $bytes) }
Write-Host "  demo files: $((Get-ChildItem $driveDir -Recurse -File).Count) under $Drive\Photos (7 groups of copies, 8 duplicates)"

# ---- the run ------------------------------------------------------------------------------------------------
$snapshot = Get-Snapshot
$t0 = Get-Date
$failure = $null
$script:proc = $null
$sync = [hashtable]::Synchronized(@{ Stop = $false; Times = (New-Object System.Collections.ArrayList); Clock = [Diagnostics.Stopwatch]::StartNew() })
$ps = $null; $handle = $null
subst $Drive $driveDir
if (-not (Test-Path "$Drive\")) { Restore-Snapshot $snapshot; Stop-Demo 2 "subst $Drive failed" }
try {
    $screen = [System.Windows.Forms.Screen]::PrimaryScreen
    if ($Monitor) { $screen = [System.Windows.Forms.Screen]::AllScreens | Where-Object { $_.DeviceName -eq $Monitor } | Select-Object -First 1; if (-not $screen) { throw "no display named '$Monitor'" } }
    $area = $screen.WorkingArea
    $dpi = [FdDemo]::DpiAtPoint($area.Left + 50, $area.Top + 50)
    $scale = $dpi / 96.0
    $rails = @('rail_group_check', 'rail_group_tidy', 'rail_group_erase', 'rail_group_protect', 'rail_group_disks')
    Set-GuiSetting 'GuiLang' 'en'; Set-GuiSetting 'ShellTheme' 'dark'; Set-GuiSetting 'ShellHistory' 0 'DWord'
    Set-GuiSetting 'ShellRailCollapsed' ((@($rails | Where-Object { $_ -ne 'rail_group_check' })) -join ';')
    Set-GuiSetting 'ShellPage' 'rail_job_speed'
    Set-GuiSetting 'ShellPlacementV' 3 'DWord'; Set-GuiSetting 'ShellMax' 0 'DWord'; Set-GuiSetting 'ShellDpi' $dpi 'DWord'
    Set-GuiSetting 'ShellX' ($area.Left + 40) 'DWord'; Set-GuiSetting 'ShellY' ($area.Top + 40) 'DWord'
    # The frame around the client area, measured at 225 % (28 x 79 px) and scaled.
    Set-GuiSetting 'ShellW' ([int][Math]::Round($DesignWidth * $scale + 12.4 * $scale)) 'DWord'
    Set-GuiSetting 'ShellH' ([int][Math]::Round($DesignHeight * $scale + 35.1 * $scale)) 'DWord'

    $env:FILEDO_STATE_DIR = $stateDir
    $script:proc = Start-Process $Exe -PassThru -WorkingDirectory $WorkDir
    $win = Find-ShellWindow $script:proc.Id
    if (-not $win) { throw 'the FileDO window did not appear' }
    $script:hwnd = [IntPtr]$win.Current.NativeWindowHandle
    Start-Sleep -Milliseconds 2500
    $cr = New-Object FdDemo+RECT; [void][FdDemo]::GetClientRect($script:hwnd, [ref]$cr)
    $script:frameScale = $VideoWidth / [double]$cr.Right
    $sync.Hwnd = $script:hwnd; $sync.Fps = $Fps; $sync.Frames = $frames; $sync.W = $VideoWidth
    # JPEG at quality 95 keeps the grabber near its frame rate; the frames are re-encoded once anyway
    $sync.Jpeg = [System.Drawing.Imaging.ImageCodecInfo]::GetImageEncoders() | Where-Object { $_.MimeType -eq 'image/jpeg' }
    $sync.JpegParams = New-Object System.Drawing.Imaging.EncoderParameters(1)
    $sync.JpegParams.Param[0] = New-Object System.Drawing.Imaging.EncoderParameter([System.Drawing.Imaging.Encoder]::Quality, [long]95)
    $sync.H = [int]([Math]::Round($cr.Bottom * $script:frameScale / 2) * 2)
    Write-Host ("  window client {0}x{1} at {2} DPI -> frames {3}x{4}" -f $cr.Right, $cr.Bottom, $dpi, $sync.W, $sync.H)

    Hide-VolumeLabel
    Start-Sleep -Milliseconds 400
    $rs = [runspacefactory]::CreateRunspace(); $rs.Open()
    $ps = [powershell]::Create(); $ps.Runspace = $rs
    [void]$ps.AddScript($grabber).AddArgument($sync)
    $sync.Clock.Restart()
    $handle = $ps.BeginInvoke()
    Start-Sleep -Milliseconds 1800

    # 1. Speed test of the drive (the window opens on its page)
    Type-Path (Get-Label 'shell_lbl_target_drive_folder') "$Drive\"
    Start-Sleep -Milliseconds 900
    $runSpeed = (Get-Label 'shell_btn_run_action') -f (Get-Label 'rail_job_speed')
    Scroll-To $runSpeed
    Start-Sleep -Milliseconds 1200
    $script:posterAt = $sync.Clock.ElapsedMilliseconds
    Press (Find-Element $runSpeed $CT::Pane)
    Wait-Result
    Start-Sleep -Milliseconds 3200

    # 2. Duplicate files in the photo folder
    Press (Find-Element (Get-Label 'rail_group_tidy') $CT::Pane -IgnoreCase) 0.3
    Start-Sleep -Milliseconds 700
    Press (Find-Element (Get-Label 'rail_job_duplicates') $CT::Pane) 0.3
    Start-Sleep -Milliseconds 900
    Type-Path (Get-Label 'ui_folder') "$Drive\Photos"
    Start-Sleep -Milliseconds 900
    $runDup = (Get-Label 'shell_btn_run_action') -f (Get-Label 'rail_job_duplicates')
    Scroll-To $runDup
    Start-Sleep -Milliseconds 1000
    Press (Find-Element $runDup $CT::Pane)
    Wait-Result
    Start-Sleep -Milliseconds 4000
} catch {
    $failure = "$($_.Exception.Message) (line $($_.InvocationInfo.ScriptLineNumber))"
} finally {
    $sync.Stop = $true
    if ($ps) { try { [void]$ps.EndInvoke($handle) } catch { if (-not $failure) { $failure = "frame grabber: $($_.Exception.Message)" } }; $ps.Dispose() }
    if ($script:proc -and -not $script:proc.HasExited) { Stop-Process -Id $script:proc.Id -Force -ErrorAction SilentlyContinue }
    Start-Sleep -Milliseconds 600
    Restore-Snapshot $snapshot
    subst $Drive /d
    $la = Join-Path $env:LOCALAPPDATA 'FileDO'
    Get-ChildItem $la -File -Filter 'filedo_win.session-*.log' -ErrorAction SilentlyContinue | Where-Object { $_.CreationTime -ge $t0 } | ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force }
    foreach ($d in 'runs', 'reports') { Get-ChildItem (Join-Path $la $d) -File -ErrorAction SilentlyContinue | Where-Object { $_.CreationTime -ge $t0 } | ForEach-Object { Remove-Item -LiteralPath $_.FullName -Force } }
    Write-Host '  settings restored, demo drive removed, the run''s session log and event files deleted'
}
if ($failure) { Stop-Demo 1 $failure }
$times = @($sync.Times)
if ($times.Count -lt 50) { Stop-Demo 1 "only $($times.Count) frames were captured" }
Write-Host ("  {0} frames over {1:N1} s ({2:N1} fps)" -f $times.Count, ($times[-1] / 1000.0), ($times.Count / ($times[-1] / 1000.0)))

# ---- the pointer, drawn onto the frames ---------------------------------------------------------------------
$acts = @($script:actions | Sort-Object { $_.T })
$rest = @{ X = $VideoWidth * 0.62; Y = $sync.H * 0.58 }
$arrow = @(@(0, 0), @(0, 21), @(5, 16), @(9, 25), @(13, 23), @(9, 15), @(15, 15))
$k = [Math]::Max(1.0, $VideoWidth / 1280.0) * 1.15
$penOut = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb(255, 20, 20, 20), [float](1.6 * $k))
$penOut.LineJoin = [System.Drawing.Drawing2D.LineJoin]::Round
function Get-Pointer([double]$t) {
    $prev = $rest
    foreach ($a in $acts) {
        if ($t -lt $a.T - $moveMs) { return $prev }
        if ($t -lt $a.T) {
            $u = ($t - ($a.T - $moveMs)) / $moveMs; $u = if ($u -lt 0.5) { 4 * $u * $u * $u } else { 1 - [Math]::Pow(-2 * $u + 2, 3) / 2 }
            return @{ X = $prev.X + ($a.X - $prev.X) * $u; Y = $prev.Y + ($a.Y - $prev.Y) * $u }
        }
        $prev = @{ X = $a.X; Y = $a.Y }
    }
    return $prev
}
for ($i = 0; $i -lt $times.Count; $i++) {
    $f = Join-Path $frames ('f{0:D5}.png' -f $i)
    $jpg = Join-Path $frames ('f{0:D5}.jpg' -f $i)
    $src = [System.Drawing.Image]::FromFile($jpg); $bmp = New-Object System.Drawing.Bitmap($src); $src.Dispose()
    $g = [System.Drawing.Graphics]::FromImage($bmp); $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $t = [double]$times[$i]
    foreach ($a in $acts) {
        $age = $t - $a.T
        if ($a.Click -and $age -ge 0 -and $age -lt 420) {
            $u = $age / 420.0; $r = (7 + 17 * $u) * $k
            $pen = New-Object System.Drawing.Pen([System.Drawing.Color]::FromArgb([int](220 * (1 - $u)), 61, 186, 78), [float](3 * $k))
            $g.DrawEllipse($pen, [float]($a.X - $r), [float]($a.Y - $r), [float](2 * $r), [float](2 * $r)); $pen.Dispose()
        }
    }
    $p = Get-Pointer $t
    $pts = [System.Drawing.PointF[]]@($arrow | ForEach-Object { New-Object System.Drawing.PointF([float]($p.X + $_[0] * $k), [float]($p.Y + $_[1] * $k)) })
    $g.FillPolygon([System.Drawing.Brushes]::White, $pts); $g.DrawPolygon($penOut, $pts)
    $g.Dispose()
    $bmp.Save($f, [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose()
    Remove-Item -LiteralPath $jpg
}

# ---- encode -------------------------------------------------------------------------------------------------
$list = Join-Path $WorkDir 'frames.txt'
$sb = New-Object System.Text.StringBuilder
for ($i = 0; $i -lt $times.Count; $i++) {
    $dur = if ($i + 1 -lt $times.Count) { ($times[$i + 1] - $times[$i]) / 1000.0 } else { 1.0 / $Fps }
    [void]$sb.AppendLine(("file 'frames/f{0:D5}.png'" -f $i)); [void]$sb.AppendLine(('duration {0}' -f $dur.ToString([Globalization.CultureInfo]::InvariantCulture)))
}
[void]$sb.AppendLine(("file 'frames/f{0:D5}.png'" -f ($times.Count - 1)))
[IO.File]::WriteAllText($list, $sb.ToString(), (New-Object System.Text.UTF8Encoding($false)))
$mp4 = Join-Path $WorkDir 'demo.mp4'; $gif = Join-Path $WorkDir 'demo.gif'; $poster = Join-Path $WorkDir 'demo-poster.jpg'
Push-Location $WorkDir
try {
    & $ffmpeg -hide_banner -loglevel error -y -f concat -safe 0 -i frames.txt -fps_mode cfr -r 25 -c:v libx264 -preset slow -crf 26 -pix_fmt yuv420p -movflags +faststart -an $mp4
    if ($LASTEXITCODE -ne 0) { Stop-Demo 1 "ffmpeg (mp4) exited $LASTEXITCODE" }
    & $ffmpeg -hide_banner -loglevel error -y -f concat -safe 0 -i frames.txt -vf "fps=10,scale=${GifWidth}:-2:flags=lanczos,split[a][b];[a]palettegen=max_colors=96:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=4:diff_mode=rectangle" -loop 0 $gif
    if ($LASTEXITCODE -ne 0) { Stop-Demo 1 "ffmpeg (gif) exited $LASTEXITCODE" }
} finally { Pop-Location }
$posterFrame = 0
for ($i = 0; $i -lt $times.Count; $i++) { if ($times[$i] -le $script:posterAt) { $posterFrame = $i } }
$src = [System.Drawing.Image]::FromFile((Join-Path $frames ('f{0:D5}.png' -f $posterFrame)))
$jq = New-Object System.Drawing.Imaging.EncoderParameters(1)
$jq.Param[0] = New-Object System.Drawing.Imaging.EncoderParameter([System.Drawing.Imaging.Encoder]::Quality, [long]90)
$src.Save($poster, ($sync.Jpeg), $jq); $src.Dispose()

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null
foreach ($f in $mp4, $gif, $poster) { Copy-Item -LiteralPath $f -Destination $OutDir -Force }
foreach ($f in 'demo.mp4', 'demo.gif', 'demo-poster.jpg') { $fi = Get-Item (Join-Path $OutDir $f); Write-Host ("  {0,-16} {1,10:N0} bytes" -f $fi.Name, $fi.Length) }
if (-not $KeepWork) { Remove-Item -LiteralPath $WorkDir -Recurse -Force }
Write-Host ("make-demo: PASS ({0:N1} s recorded with {1}; written to {2})" -f ($times[-1] / 1000.0), $version, $OutDir) -ForegroundColor Green
exit 0
