#Requires -Version 7.0
<#
.SYNOPSIS
  Re-takes the guide screenshots under docs/assets/guides/ from the current build (SP-0062 T6).

.DESCRIPTION
  DOC-EXTERNAL-QUALITY rule 6: a multi-step guide shows the interface as it is today, in the
  reader's language or language-neutral. The pictures are therefore produced, never drawn by hand:

    gui-<page>-<en|ru|ua>.png  filedo_win.exe --capture-screens renders its own pages in each
                               language the site authors. It writes no setting (Capture.vb).
    msi-customize.png          The MSI's feature page, reached by driving the stock WiX dialogs
                               to "Choose what to install" and captured there; the dialog is then
                               cancelled, so nothing is installed. The WiX UI is English in every
                               locale, which makes this capture language-neutral.

  Run it after a GUI page, a rail label or the installer's feature tree changes, then look at the
  pictures before committing them. It opens windows on the desktop for a few seconds.

  Exit: 0 = every picture written, 1 = one failed, 2 = a prerequisite is missing.
#>
[CmdletBinding()]
param(
    [string]$Gui,
    [string]$Msi,
    [switch]$SkipMsi
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$out = Join-Path $root 'docs\assets\guides'
New-Item -ItemType Directory -Force -Path $out | Out-Null

if (-not $Gui) { $Gui = Join-Path $root 'filedo_win_vb\bin\Release\filedo_win.exe' }
if (-not (Test-Path -LiteralPath $Gui)) { Write-Host "capture: NOT VERIFIED (no GUI build at $Gui - run build.ps1 first)" -ForegroundColor Yellow; exit 2 }

$failed = $false
$p = Start-Process -FilePath $Gui -ArgumentList '--capture-screens', "`"$out`"" -Wait -PassThru
if ($p.ExitCode -ne 0) { Write-Host "  FAIL  filedo_win.exe --capture-screens exit $($p.ExitCode)" -ForegroundColor Red; $failed = $true }
else { Write-Host "  gui   pages written to $out" }

if (-not $SkipMsi) {
    if (-not $Msi) { $Msi = Get-ChildItem (Join-Path $root 'dist') -Filter '*-windows-x64.msi' -ErrorAction SilentlyContinue | Sort-Object LastWriteTime -Descending | Select-Object -First 1 -ExpandProperty FullName }
    if (-not $Msi) { Write-Host "capture: NOT VERIFIED (no MSI in dist\ - build.ps1 without -SkipInstaller, or pass -Msi)" -ForegroundColor Yellow; exit 2 }

    Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Drawing
    if (-not ('CaptureNative' -as [type])) {
        Add-Type @'
using System; using System.Runtime.InteropServices;
public static class CaptureNative {
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
  [DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h, uint m, IntPtr w, IntPtr l);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
}
'@
    }
    $A = [System.Windows.Automation.AutomationElement]
    $scope = [System.Windows.Automation.TreeScope]
    # MSI dialogs are replaced, not updated, on every page: the window is found again each step.
    function Find-SetupWindow([int]$procId) {
        for ($i = 0; $i -lt 100; $i++) {
            $c = [System.Windows.Automation.PropertyCondition]::new($A::ProcessIdProperty, $procId)
            foreach ($w in $A::RootElement.FindAll($scope::Children, $c)) { if ($w.Current.Name -like '*Setup*') { return $w } }
            Start-Sleep -Milliseconds 200
        }
        throw 'the setup window did not open'
    }
    function Find-Control($window, [string]$name) {
        for ($i = 0; $i -lt 50; $i++) {
            $e = $window.FindFirst($scope::Descendants, [System.Windows.Automation.PropertyCondition]::new($A::NameProperty, $name))
            if ($e) { return $e }
            Start-Sleep -Milliseconds 200
        }
        throw "the setup dialog has no control '$name'"
    }
    # The MSI's controls expose no Invoke pattern; a BM_CLICK is what a mouse click sends.
    function Click([int]$procId, [string]$name) {
        $e = Find-Control (Find-SetupWindow $procId) $name
        [void][CaptureNative]::SendMessage([IntPtr]$e.Current.NativeWindowHandle, 0x00F5, [IntPtr]::Zero, [IntPtr]::Zero)
        Start-Sleep -Milliseconds 700
    }

    $file = Join-Path $out 'msi-customize.png'
    $m = Start-Process msiexec.exe -ArgumentList '/i', "`"$Msi`"" -PassThru
    try {
        Click $m.Id 'Next'
        Click $m.Id 'I accept the terms in the License Agreement'
        Click $m.Id 'Next'
        $w = Find-SetupWindow $m.Id
        [void](Find-Control $w 'Choose what to install')
        Start-Sleep -Milliseconds 600
        $h = [IntPtr]$w.Current.NativeWindowHandle
        $r = [CaptureNative+RECT]::new()
        [void][CaptureNative]::GetWindowRect($h, [ref]$r)
        $bmp = [System.Drawing.Bitmap]::new($r.R - $r.L, $r.B - $r.T)
        $g = [System.Drawing.Graphics]::FromImage($bmp)
        $hdc = $g.GetHdc()
        [void][CaptureNative]::PrintWindow($h, $hdc, 2)
        $g.ReleaseHdc($hdc); $g.Dispose()
        $bmp.Save($file, [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose()
        Write-Host "  msi   $file"
    } catch {
        Write-Host "  FAIL  MSI capture: $($_.Exception.Message)" -ForegroundColor Red
        $failed = $true
    } finally {
        try { Click $m.Id 'Cancel'; Click $m.Id 'Yes' } catch { }
        Start-Sleep -Seconds 2
        if (-not $m.HasExited) { Stop-Process -Id $m.Id -Force }
    }
}

if ($failed) { Write-Host 'capture: FAIL' -ForegroundColor Red; exit 1 }
Write-Host 'capture: PASS - review the pictures before committing them' -ForegroundColor Green
exit 0
