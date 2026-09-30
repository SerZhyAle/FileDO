<#
.SYNOPSIS
  Read or change the scaling of one display, so the DPI proofs can run at a scale no display here has.

.DESCRIPTION
  Uses DisplayConfigGetDeviceInfo / DisplayConfigSetDeviceInfo with the undocumented DPI-scale
  packets (types -3 and -4) that Settings > Display itself uses: no sign-out, applies at once. The
  scale is given as a percentage (100 125 150 175 200 225 250 300 350 400 450 500); a value the
  display does not offer is refused. The change is to the user's desktop: whoever calls this puts
  the old value back (capture-dpi.ps1 callers do it in a finally block).

.EXAMPLE
  .\tests\set-display-scale.ps1 -Display \\.\DISPLAY5                 # prints current/recommended/max
  .\tests\set-display-scale.ps1 -Display \\.\DISPLAY5 -Percent 150
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory)][string]$Display,
    [int]$Percent = 0
)
$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSEdition -ne 'Desktop') {
    & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $PSCommandPath -Display $Display -Percent $Percent
    exit $LASTEXITCODE
}
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
using System.Text;
public static class DispScale {
    [StructLayout(LayoutKind.Sequential)] struct LUID { public uint Low; public int High; }
    [StructLayout(LayoutKind.Sequential)] struct Header { public int Type; public uint Size; public LUID Adapter; public uint Id; }
    [StructLayout(LayoutKind.Sequential, CharSet = CharSet.Unicode)] struct SourceName { public Header H; [MarshalAs(UnmanagedType.ByValTStr, SizeConst = 32)] public string Gdi; }
    [StructLayout(LayoutKind.Sequential)] struct DpiGet { public Header H; public int Min, Cur, Max; }
    [StructLayout(LayoutKind.Sequential)] struct DpiSet { public Header H; public int Rel; }
    [DllImport("user32.dll")] static extern int GetDisplayConfigBufferSizes(uint flags, out uint np, out uint nm);
    [DllImport("user32.dll")] static extern int QueryDisplayConfig(uint flags, ref uint np, byte[] paths, ref uint nm, byte[] modes, IntPtr topo);
    [DllImport("user32.dll")] static extern int DisplayConfigGetDeviceInfo(ref SourceName p);
    [DllImport("user32.dll")] static extern int DisplayConfigGetDeviceInfo(ref DpiGet p);
    [DllImport("user32.dll")] static extern int DisplayConfigSetDeviceInfo(ref DpiSet p);
    static readonly int[] Steps = { 100, 125, 150, 175, 200, 225, 250, 300, 350, 400, 450, 500 };

    static Header Find(string gdiName) {
        uint np, nm;
        if (GetDisplayConfigBufferSizes(2, out np, out nm) != 0) throw new Exception("GetDisplayConfigBufferSizes failed");
        byte[] paths = new byte[72 * np]; byte[] modes = new byte[64 * nm];
        if (QueryDisplayConfig(2, ref np, paths, ref nm, modes, IntPtr.Zero) != 0) throw new Exception("QueryDisplayConfig failed");
        for (int i = 0; i < np; i++) {
            LUID a; a.Low = BitConverter.ToUInt32(paths, i * 72); a.High = BitConverter.ToInt32(paths, i * 72 + 4);
            uint id = BitConverter.ToUInt32(paths, i * 72 + 8);
            SourceName s = new SourceName(); s.H.Type = 1; s.H.Size = (uint)Marshal.SizeOf(typeof(SourceName)); s.H.Adapter = a; s.H.Id = id;
            if (DisplayConfigGetDeviceInfo(ref s) != 0) continue;
            if (string.Equals(s.Gdi, gdiName, StringComparison.OrdinalIgnoreCase)) { Header h = new Header(); h.Adapter = a; h.Id = id; return h; }
        }
        throw new Exception("no active display named " + gdiName);
    }

    // returns {minRel, curRel, maxRel} as offsets from the recommended step
    public static int[] Get(string gdiName) {
        Header h = Find(gdiName);
        DpiGet g = new DpiGet(); g.H = h; g.H.Type = -3; g.H.Size = (uint)Marshal.SizeOf(typeof(DpiGet));
        if (DisplayConfigGetDeviceInfo(ref g) != 0) throw new Exception("DPI scale query failed");
        return new int[] { g.Min, g.Cur, g.Max };
    }

    public static int RecommendedIndex(int[] r) { return -r[0]; }          // index into Steps of the recommended scale
    public static int Percent(int[] r, int rel) { return Steps[RecommendedIndex(r) + rel]; }

    public static void SetPercent(string gdiName, int percent) {
        int[] r = Get(gdiName);
        int want = Array.IndexOf(Steps, percent);
        if (want < 0) throw new Exception("not a Windows scale step: " + percent);
        int rel = want - RecommendedIndex(r);
        if (rel < r[0] || rel > r[2]) throw new Exception(percent + " % is not offered by " + gdiName);
        DpiSet s = new DpiSet(); s.H = Find(gdiName); s.H.Type = -4; s.H.Size = (uint)Marshal.SizeOf(typeof(DpiSet)); s.Rel = rel;
        if (DisplayConfigSetDeviceInfo(ref s) != 0) throw new Exception("DPI scale change failed");
    }
}
'@
$r = [DispScale]::Get($Display)
$cur = [DispScale]::Percent($r, $r[1])
if ($Percent -gt 0) {
    [DispScale]::SetPercent($Display, $Percent)
    Start-Sleep -Milliseconds 1500
    $r = [DispScale]::Get($Display)
    "{0}: {1} % -> {2} %" -f $Display, $cur, [DispScale]::Percent($r, $r[1])
} else {
    "{0}: current {1} %, recommended {2} %, offers {3} .. {4} %" -f $Display, $cur, [DispScale]::Percent($r, 0), [DispScale]::Percent($r, $r[0]), [DispScale]::Percent($r, $r[2])
}
