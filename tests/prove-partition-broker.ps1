<#
.SYNOPSIS
  Live proof of the brokered (unelevated) path of partition disks (SP-0148 section 8), on a disposable VHDX.

.DESCRIPTION
  Run from an ordinary, NOT elevated PowerShell. prove-partition-disks.ps1 runs everything elevated, where
  FileDO opens the partition itself; this script drives the path a person takes: every filedo command runs
  unelevated and gets the partition handle from its one consent step (_part, _open, _attach with the
  handle handed to the mounting command and inherited by the block server, _part delete with the wipe done
  through a brokered handle). Each step asks Windows for consent; on a machine that prompts, answer each.

  The disposable VHDX is made and removed by two short elevated phases of this same script
  (-Phase setup / -Phase teardown), which also hash the sentinel partitions before and after. Nothing else
  on the machine is touched. Evidence goes to temp\evidence\<run-id>\ (git-ignored, private).
  Exit code: 0 every step passed, 1 a step failed, 2 could not run.
#>
param(
  [ValidateSet('run', 'setup', 'teardown')][string]$Phase = 'run',
  [string]$Out = ''
)

$ErrorActionPreference = 'Continue'
$root = Split-Path $PSScriptRoot -Parent
$FileDOType = '{6afb315b-8976-4841-84ec-d30afa89a33d}'
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)

Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using Microsoft.Win32.SafeHandles;
public static class RawDev2 {
  [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
  static extern SafeFileHandle CreateFile(string name, uint access, uint share, IntPtr sa, uint disp, uint flags, IntPtr tmpl);
  static FileStream Open(string path, bool write) {
    var h = CreateFile(path, write ? 0xC0000000u : 0x80000000u, 3u, IntPtr.Zero, 3u, 0u, IntPtr.Zero);
    if (h.IsInvalid) throw new IOException("open " + path + ": " + Marshal.GetLastWin32Error());
    return new FileStream(h, write ? FileAccess.ReadWrite : FileAccess.Read, 4096, false);
  }
  public static void Fill(string path, long off, int len, int seed) {
    using (var fs = Open(path, true)) { var b = new byte[len]; new Random(seed).NextBytes(b); fs.Seek(off, SeekOrigin.Begin); fs.Write(b, 0, len); fs.Flush(true); }
  }
  public static string Hash(string path, long off, int len) {
    using (var fs = Open(path, false)) {
      var b = new byte[len]; fs.Seek(off, SeekOrigin.Begin);
      int got = 0; while (got < len) { int n = fs.Read(b, got, len - got); if (n <= 0) break; got += n; }
      using (var sha = SHA256.Create()) return BitConverter.ToString(sha.ComputeHash(b, 0, got));
    }
  }
}
'@
function PartPath($disk, $part) { "\\?\GLOBALROOT\Device\Harddisk$disk\Partition$part" }
function Sentinels($n) { @(Get-Partition -DiskNumber $n | Where-Object { $_.GptType -ne $FileDOType } | Sort-Object Offset | Select-Object Guid, Offset, Size, GptType, PartitionNumber) }
function SentinelHashes($n) {
  foreach ($s in (Sentinels $n | Where-Object { $_.Size -ge 64MB })) {
    $p = PartPath $n $s.PartitionNumber
    '{0}:{1}:{2}' -f $s.Guid, [RawDev2]::Hash($p, 0, 4MB), [RawDev2]::Hash($p, $s.Size - 4MB, 4MB)
  }
}

# ---------------------------------------------------------------- the elevated phases
if ($Phase -eq 'setup') {
  $vhd = Join-Path $Out 'broker.vhdx'
  try {
    $disk = New-VHD -Path $vhd -SizeBytes 1GB -Dynamic -LogicalSectorSizeBytes 4096 -ErrorAction Stop | Mount-VHD -Passthru -ErrorAction Stop | Get-Disk -ErrorAction Stop
    $n = $disk.Number
    Set-Disk -Number $n -IsOffline $false -ErrorAction SilentlyContinue
    Set-Disk -Number $n -IsReadOnly $false -ErrorAction SilentlyContinue
    Initialize-Disk -Number $n -PartitionStyle GPT
    foreach ($o in @(32MB, 800MB)) {
      $p = New-Partition -DiskNumber $n -Offset $o -Size 96MB
      Set-Partition -DiskNumber $n -PartitionNumber $p.PartitionNumber -NoDefaultDriveLetter $true -ErrorAction SilentlyContinue
      [RawDev2]::Fill((PartPath $n $p.PartitionNumber), 0, 4MB, [int]$p.PartitionNumber)
    }
    @{ ok = $true; disk = $n; guid = (Get-Disk -Number $n).Guid.Trim('{}').ToUpper(); before = (Sentinels $n | ConvertTo-Json -Compress); hashes = ((SentinelHashes $n) -join '|') } |
      ConvertTo-Json | Set-Content (Join-Path $Out 'setup.json')
  } catch {
    @{ ok = $false; error = $_.Exception.Message } | ConvertTo-Json | Set-Content (Join-Path $Out 'setup.json')
  }
  exit 0
}
if ($Phase -eq 'teardown') {
  $s = Get-Content (Join-Path $Out 'setup.json') -Raw | ConvertFrom-Json
  $fd = @(Get-Partition -DiskNumber $s.disk -ErrorAction SilentlyContinue | Where-Object { $_.GptType -eq $FileDOType })
  $same = (Sentinels $s.disk | ConvertTo-Json -Compress) -eq $s.before -and ((SentinelHashes $s.disk) -join '|') -eq $s.hashes
  @{ fileDOLeft = $fd.Count; sentinelsUnchanged = $same } | ConvertTo-Json | Set-Content (Join-Path $Out 'teardown.json')
  Dismount-VHD -Path (Join-Path $Out 'broker.vhdx') -ErrorAction SilentlyContinue
  Remove-Item (Join-Path $Out 'broker.vhdx') -Force -ErrorAction SilentlyContinue
  exit 0
}

# ---------------------------------------------------------------- the unelevated run
if ($admin) { Write-Host 'Run this from an ordinary, NOT elevated PowerShell: it proves the unelevated path.'; exit 2 }
$run = 'partbroker-' + (Get-Date -Format 'yyyyMMdd-HHmmss')
$Out = Join-Path $root "temp\evidence\$run"
New-Item -ItemType Directory -Force $Out | Out-Null
$log = Join-Path $Out 'proof.log'
$exe = Join-Path $Out 'filedo.exe'
$env:FILEDO_STATE_DIR = Join-Path $Out 'state'
New-Item -ItemType Directory -Force $env:FILEDO_STATE_DIR | Out-Null
$results = [System.Collections.Generic.List[object]]::new()
function Say([string]$m) { $l = '{0:HH:mm:ss} {1}' -f (Get-Date), $m; Write-Host $l; Add-Content $log $l }
function Step([string]$name, [bool]$ok, [string]$detail = '') {
  $results.Add([pscustomobject]@{ Step = $name; Pass = $ok; Detail = $detail })
  Say ('{0}: {1} {2}' -f ($(if ($ok) { 'PASS' } else { 'FAIL' })), $name, $detail)
}
function Filedo([string[]]$a) {
  Say ("filedo " + ($a -join ' '))
  $o = & $exe --no-history @a 2>&1 | Out-String
  $code = $LASTEXITCODE
  Add-Content $log $o
  [pscustomobject]@{ Code = $code; Text = $o }
}
function Elevated([string]$phase) {
  $pwsh = (Get-Process -Id $PID).Path
  $p = Start-Process -FilePath $pwsh -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$PSCommandPath`"", '-Phase', $phase, '-Out', "`"$Out`"") -Verb RunAs -PassThru -Wait -WindowStyle Minimized
  $p.ExitCode
}

Say "run $run, evidence in $Out"
Push-Location $root
try {
  $old = $env:GOARCH; $env:GOARCH = 'amd64'
  & go build -o $exe ./cmd/filedo 2>&1 | Tee-Object -Append $log
  $env:GOARCH = $old
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path $exe)) { Say 'build failed'; exit 2 }
} finally { Pop-Location }

[void](Elevated 'setup')
$setup = try { Get-Content (Join-Path $Out 'setup.json') -Raw | ConvertFrom-Json } catch { $null }
if (-not $setup -or -not $setup.ok) { Say "setup failed: $($setup.error)"; exit 2 }
$guid = $setup.guid
Say "disposable disk $($setup.disk) {$guid}"
try {
  $r = Filedo @('vd', 'disks', 'json')
  $line = $r.Text -split "`r?`n" | Where-Object { $_.StartsWith('{"schema"') } | Select-Object -First 1
  $row = ($line | ConvertFrom-Json).disks | Where-Object { $_.guid -eq $guid }
  $gap = $row.free | Where-Object { $_.usable -and $_.offset -ge 128MB -and $_.offset -lt 800MB } | Select-Object -First 1
  Step 'disks json, unelevated' ([bool]$gap) "gap $($gap.offset)+$($gap.length)"
  if ($gap) {
    $r = Filedo @('vd', 'new', 'part', "disk:{$guid}", 'size', 'max', 'at', "$($gap.offset)", 'fast', 'as', 'pdb', 'force')
    Step 'new part through _part (one consent)' ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @('vd', 'info', 'pdb'); Step 'info through _open' ($r.Code -eq 0 -and $r.Text -match 'Container id') "exit $($r.Code)"
    $letter = foreach ($c in [char[]]'TUVWXYZRSQ') { if (-not (Test-Path "${c}:\")) { "${c}:"; break } }
    $r = Filedo @('vd', 'mount', 'pdb', 'as', $letter)
    $ok = $r.Code -eq 0 -and (Test-Path "$letter\")
    Step "mount through _attach with the brokered handle, at $letter" $ok "exit $($r.Code)"
    if ($ok) {
      $payload = [byte[]]::new(2MB); (New-Object Random 11).NextBytes($payload)
      [IO.File]::WriteAllBytes("$letter\payload.bin", $payload)
      Step 'a file through the volume' ([Convert]::ToBase64String([IO.File]::ReadAllBytes("$letter\payload.bin")) -eq [Convert]::ToBase64String($payload))
      $srv = (Get-Content (Join-Path $env:FILEDO_STATE_DIR 'vdisk-state.json') -Raw | ConvertFrom-Json).mounts | Select-Object -First 1
      $p = Get-CimInstance Win32_Process -Filter "ProcessId=$($srv.server_pid)" -ErrorAction SilentlyContinue
      Step 'the block server runs and holds the partition' ([bool]$p -and $p.CommandLine -match 'handle=') "pid $($srv.server_pid)"
      $r = Filedo @('vd', 'verify', 'pdb'); Step 'verify while mounted is busy' ($r.Code -eq 8) "exit $($r.Code)"
      $r = $null
      for ($i = 0; $i -lt 6; $i++) {
        $r = Filedo @('vd', 'unmount', 'pdb')
        if ($r.Code -ne 8 -or $r.Text -notmatch 'holds the disk open') { break }
        Start-Sleep -Seconds 5
      }
      Step 'unmount' ($r.Code -eq 0 -and $r.Text -match 'closed cleanly') "exit $($r.Code)"
    }
    $r = Filedo @('vd', 'verify', 'pdb'); Step 'verify through _open' ($r.Code -eq 0) "exit $($r.Code)"
    $file = Join-Path $Out 'pdb.fdd'
    $r = Filedo @('vd', 'image', 'pdb', 'to', $file); Step 'image to a file through _open' ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @($file, 'verify'); Step 'the image verifies with no elevation at all' ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @('vd', 'destroy', 'pdb', 'wipe', 'force'); Step 'destroy wipe through the _part broker' ($r.Code -eq 0) "exit $($r.Code)"
  }
} finally {
  $rows = try { (Get-Content (Join-Path $env:FILEDO_STATE_DIR 'vdisk-state.json') -Raw | ConvertFrom-Json).mounts } catch { @() }
  foreach ($m in @($rows)) { if ($m.letter) { [void](Filedo @('vd', 'unmount', $m.letter, 'force')) } }
  [void](Elevated 'teardown')
}
$td = try { Get-Content (Join-Path $Out 'teardown.json') -Raw | ConvertFrom-Json } catch { $null }
Step 'no FileDO partition left, sentinels unchanged' ([bool]($td -and $td.fileDOLeft -eq 0 -and $td.sentinelsUnchanged)) ($td | ConvertTo-Json -Compress)
$failed = @($results | Where-Object { -not $_.Pass })
$results | Format-Table -AutoSize | Out-String | Tee-Object -Append $log | Write-Host
if ($failed.Count) { Say "FAIL: $($failed.Count) of $($results.Count) steps"; exit 1 }
Say "PASS: $($results.Count) steps"
exit 0
