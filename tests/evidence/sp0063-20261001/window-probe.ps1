$ErrorActionPreference='Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System; using System.Text; using System.Collections.Generic; using System.Runtime.InteropServices;
public class DMProbe {
public delegate bool Callback(IntPtr h,IntPtr l);
[DllImport("user32.dll")] static extern bool EnumWindows(Callback c,IntPtr l);
[DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr h,out uint p);
[DllImport("user32.dll")] static extern bool IsWindowVisible(IntPtr h);
[DllImport("user32.dll",CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr h,StringBuilder b,int n);
[DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
[DllImport("user32.dll")] public static extern IntPtr SendMessage(IntPtr h,uint m,IntPtr w,IntPtr l);
public static IntPtr[] Handles(uint pid) {var a=new List<IntPtr>(); EnumWindows((h,l)=>{uint p;GetWindowThreadProcessId(h,out p);if(p==pid&&IsWindowVisible(h))a.Add(h);return true;},IntPtr.Zero);return a.ToArray();}
public static string Title(IntPtr h){var b=new StringBuilder(512);GetWindowText(h,b,512);return b.ToString();}
}
"@
$exe='P:\WINDOWS\FileDo\exe_to_download\filedo_win.exe'
$env:FILEDO_STATE_DIR=Join-Path $env:TEMP 'filedo-capture-dpi-state'
$reg='HKCU:\Software\FileDO'
$old=@{}; $rk=[Microsoft.Win32.Registry]::CurrentUser.OpenSubKey("Software\FileDO"); foreach($n in $rk.GetValueNames()){$old[$n]=@{Value=$rk.GetValue($n);Kind=$rk.GetValueKind($n)}}; $rk.Close()
$p=$null
try {
Set-ItemProperty $reg DiskManagerWelcomed 1
Set-ItemProperty $reg GuiLang en
$p=Start-Process $exe -ArgumentList '--disks' -PassThru
Start-Sleep -Seconds 2
$hs=@([DMProbe]::Handles($p.Id)); "manager-only: count=$($hs.Count); titles=$($hs | ForEach-Object {[DMProbe]::Title($_)})"
$second=Start-Process $exe -ArgumentList '--disks' -PassThru; $second.WaitForExit(10000) | Out-Null
"second --disks: exited=$($second.HasExited), code=$($second.ExitCode), windows=$(@([DMProbe]::Handles($p.Id)).Count)"
[DMProbe]::SetForegroundWindow($hs[0]) | Out-Null; Start-Sleep -Milliseconds 700
$open=Start-Process $exe -PassThru; $open.WaitForExit(10000)|Out-Null; Start-Sleep -Seconds 1
$hs=@([DMProbe]::Handles($p.Id)); "plain launch opens shell: count=$($hs.Count); titles=$($hs | ForEach-Object {[DMProbe]::Title($_)})"
$shell=$hs | Where-Object {[DMProbe]::Title($_) -notmatch 'Disk'} | Select-Object -First 1
[DMProbe]::SetForegroundWindow($shell) | Out-Null; Start-Sleep -Milliseconds 700
$bring=Start-Process $exe -ArgumentList "--disks" -PassThru; $bring.WaitForExit(10000)|Out-Null
"--disks brings manager: windows=$(@([DMProbe]::Handles($p.Id)).Count)"
[DMProbe]::SendMessage($shell,0x10,[IntPtr]::Zero,[IntPtr]::Zero) | Out-Null; Start-Sleep -Seconds 1
$p.Refresh(); "close shell: processAlive=$(-not $p.HasExited), windows=$(@([DMProbe]::Handles($p.Id)).Count)"
$plain=Start-Process $exe -PassThru; $plain.WaitForExit(10000)|Out-Null; Start-Sleep -Seconds 1
"plain second start: exited=$($plain.HasExited); windows=$(@([DMProbe]::Handles($p.Id)).Count)"
$hs=@([DMProbe]::Handles($p.Id)); $manager=$hs|Where-Object {[DMProbe]::Title($_) -match 'Disk'}|Select-Object -First 1
[DMProbe]::SendMessage($manager,0x10,[IntPtr]::Zero,[IntPtr]::Zero)|Out-Null;Start-Sleep -Seconds 1
$p.Refresh(); "close manager: processAlive=$(-not $p.HasExited), windows=$(@([DMProbe]::Handles($p.Id)).Count)"
foreach($h in @([DMProbe]::Handles($p.Id))){[DMProbe]::SendMessage($h,0x10,[IntPtr]::Zero,[IntPtr]::Zero)|Out-Null}
$p.WaitForExit(5000)|Out-Null; "close last: exited=$($p.HasExited), code=$($p.ExitCode)"
} finally {
if($p -and -not $p.HasExited){$p.Kill()}
$rk=[Microsoft.Win32.Registry]::CurrentUser.CreateSubKey("Software\FileDO"); foreach($n in @($rk.GetValueNames())){if(-not $old.ContainsKey($n)){$rk.DeleteValue($n)}}; foreach($n in $old.Keys){$rk.SetValue($n,$old[$n].Value,$old[$n].Kind)}; $rk.Close()
}




