<#
.SYNOPSIS
  Live proof of partition disks (SP-0148) on disposable VHDX disks only.

.DESCRIPTION
  Run from an ELEVATED PowerShell (or let it ask for consent once). It builds filedo.exe (amd64) from
  this tree into the run folder, uses a private state folder there (FILEDO_STATE_DIR), and never selects a
  disk of this machine: every disk it touches is a VHDX it creates in the run folder and deletes at the end.

  For each sector size (512 and 4096 bytes, -Sectors to choose):
    1. a 1 GiB VHDX, GPT, two sentinel partitions with a free gap between them, each sentinel filled with
       known bytes and hashed;
    2. `vd disks json` lists the VHDX with the gap usable;
    3. `vd new part` makes a fast partition disk in the gap, then a vault one in what is left;
    4. the layout has exactly the two new FileDO entries and the sentinels are unchanged (entries and bytes);
    5. list, status json, info; mount, a file written and read back through the volume, a second mount and a
       verify while mounted refused as busy (class 8), unmount;
    6. verify, export raw, image to a file and the file verified, pass on the vault disk; forget the vault
       disk (the partition stays) and adopt it back by its locator;
    7. auto logon, the task started by hand mounts the disk (the server inherits the partition handle),
       unmount, auto off (the scheduled task is removed);
    8. destroy (one with wipe); a FileDO-type partition made by hand is refused by adopt while its header
       positions hold garbage, adopted as unfinished once they are zero, refused by mount and deleted by
       destroy; the layout is back to the two sentinels and their bytes are unchanged;
    9. an MBR VHDX is refused with the unsupported class (6).

  Evidence (log, outputs, the built exe) goes to temp\evidence\<run-id>\ (git-ignored, private).
  Exit code: 0 every step passed, 1 a step failed, 2 could not run (not elevated, no Hyper-V module, build).
#>
param(
  [string]$Sectors = '512,4096',
  [switch]$KeepEvidence
)
$sectorList = @($Sectors -split '[,\s]+' | Where-Object { $_ } | ForEach-Object { [int]$_ })

$ErrorActionPreference = 'Continue'
$root = Split-Path $PSScriptRoot -Parent
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) {
  # One consent prompt: the same script, elevated, in a window of its own; its exit code is this one's.
  $pwsh = (Get-Process -Id $PID).Path
  $a = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$PSCommandPath`"", '-Sectors', ($sectorList -join ','))
  if ($KeepEvidence) { $a += '-KeepEvidence' }
  $p = Start-Process -FilePath $pwsh -ArgumentList $a -Verb RunAs -PassThru -Wait
  exit $p.ExitCode
}
if (-not (Get-Command New-VHD -ErrorAction SilentlyContinue)) { Write-Host 'New-VHD (the Hyper-V module) is needed to make the disposable disks.'; exit 2 }

$run = 'partdisks-' + (Get-Date -Format 'yyyyMMdd-HHmmss')
$out = Join-Path $root "temp\evidence\$run"
New-Item -ItemType Directory -Force $out | Out-Null
$log = Join-Path $out 'proof.log'
$exe = Join-Path $out 'filedo.exe'
$env:FILEDO_STATE_DIR = Join-Path $out 'state'
New-Item -ItemType Directory -Force $env:FILEDO_STATE_DIR | Out-Null
$results = [System.Collections.Generic.List[object]]::new()
$FileDOType = '{6afb315b-8976-4841-84ec-d30afa89a33d}'

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

Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Runtime.InteropServices;
using System.Security.Cryptography;
using Microsoft.Win32.SafeHandles;
public static class RawDev {
  [DllImport("kernel32.dll", SetLastError = true, CharSet = CharSet.Unicode)]
  static extern SafeFileHandle CreateFile(string name, uint access, uint share, IntPtr sa, uint disp, uint flags, IntPtr tmpl);
  static FileStream Open(string path, bool write) {
    var h = CreateFile(path, write ? 0xC0000000u : 0x80000000u, 3u, IntPtr.Zero, 3u, 0u, IntPtr.Zero);
    if (h.IsInvalid) throw new IOException("open " + path + ": " + Marshal.GetLastWin32Error());
    return new FileStream(h, write ? FileAccess.ReadWrite : FileAccess.Read, 4096, false);
  }
  public static void Fill(string path, long off, int len, int seed) {
    using (var fs = Open(path, true)) {
      var b = new byte[len]; new Random(seed).NextBytes(b);
      fs.Seek(off, SeekOrigin.Begin); fs.Write(b, 0, len); fs.Flush(true);
    }
  }
  public static void Zero(string path, long off, int len) {
    using (var fs = Open(path, true)) {
      fs.Seek(off, SeekOrigin.Begin); fs.Write(new byte[len], 0, len); fs.Flush(true);
    }
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
    '{0}:{1}:{2}' -f $s.Guid, [RawDev]::Hash($p, 0, 4MB), [RawDev]::Hash($p, $s.Size - 4MB, 4MB)
  }
}
# The Windows Virtual Disk Service, woken by the Storage cmdlets this script runs, can hold a freshly
# mounted disk open for a few seconds; unmount refuses that as busy (class 8), as for any disk. Retried.
function UnmountRetry([string]$name) {
  for ($i = 0; $i -lt 6; $i++) {
    $r = Filedo @('vd', 'unmount', $name)
    if ($r.Code -ne 8 -or $r.Text -notmatch 'holds the disk open') { return $r }
    Start-Sleep -Seconds 5
  }
  $r
}
function UnmountAll {
  $rows = try { (Get-Content (Join-Path $env:FILEDO_STATE_DIR 'vdisk-state.json') -Raw | ConvertFrom-Json).mounts } catch { @() }
  foreach ($m in @($rows)) { if ($m.letter) { [void](Filedo @('vd', 'unmount', $m.letter, 'force')) } }
}
function StatusRow([string]$name) {
  $r = Filedo @('vd', 'status', 'json')
  $line = $r.Text -split "`r?`n" | Where-Object { $_.StartsWith('{') -and $_.Contains('"disks"') } | Select-Object -First 1
  $j = try { $line | ConvertFrom-Json } catch { $null }
  $j.disks | Where-Object { $_.name -eq $name } | Select-Object -First 1
}
function FreeLetter { foreach ($c in [char[]]'TUVWXYZRSQ') { if (-not (Test-Path "${c}:\")) { return "${c}:" } }; 'Z:' }

Say "run $run, evidence in $out"
Push-Location $root
try {
  $old = $env:GOARCH; $env:GOARCH = 'amd64'
  & go build -o $exe ./cmd/filedo 2>&1 | Tee-Object -Append $log
  $env:GOARCH = $old
  if ($LASTEXITCODE -ne 0 -or -not (Test-Path $exe)) { Say 'build failed'; exit 2 }
} finally { Pop-Location }
Say "built $exe"

$vhds = [System.Collections.Generic.List[string]]::new()
try {
  foreach ($sector in $sectorList) {
    $tag = "s$sector"
    $vhd = Join-Path $out "$tag.vhdx"
    $vhds.Add($vhd)
    Say "== ${tag}: $vhd"
    try {
      $disk = New-VHD -Path $vhd -SizeBytes 1GB -Dynamic -LogicalSectorSizeBytes $sector -PhysicalSectorSizeBytes 4096 -ErrorAction Stop | Mount-VHD -Passthru -ErrorAction Stop | Get-Disk -ErrorAction Stop
    } catch {
      Step "$tag make the disposable VHDX" $false $_.Exception.Message
      continue
    }
    $n = $disk.Number
    # A SAN policy may bring new disks up offline or read-only; this one is ours.
    Set-Disk -Number $n -IsOffline $false -ErrorAction SilentlyContinue
    Set-Disk -Number $n -IsReadOnly $false -ErrorAction SilentlyContinue
    Initialize-Disk -Number $n -PartitionStyle GPT
    $a = New-Partition -DiskNumber $n -Offset 32MB -Size 96MB
    $b = New-Partition -DiskNumber $n -Offset 800MB -Size 96MB
    foreach ($p in @($a, $b)) {
      Set-Partition -DiskNumber $n -PartitionNumber $p.PartitionNumber -NoDefaultDriveLetter $true -ErrorAction SilentlyContinue
      [RawDev]::Fill((PartPath $n $p.PartitionNumber), 0, 4MB, [int]$p.PartitionNumber)
      [RawDev]::Fill((PartPath $n $p.PartitionNumber), $p.Size - 4MB, 4MB, 100 + [int]$p.PartitionNumber)
    }
    $guid = (Get-Disk -Number $n).Guid.Trim('{}').ToUpper()
    $before = Sentinels $n | ConvertTo-Json -Compress
    $hashes = (SentinelHashes $n) -join '|'
    Say "disk $n {$guid}, sentinels $before"

    $r = Filedo @('vd', 'disks', 'json')
    $line = $r.Text -split "`r?`n" | Where-Object { $_.StartsWith('{"schema"') } | Select-Object -First 1
    $j = try { $line | ConvertFrom-Json } catch { $null }
    $row = $j.disks | Where-Object { $_.guid -eq $guid }
    $gap = $row.free | Where-Object { $_.offset -ge 128MB -and $_.offset -lt 800MB } | Select-Object -First 1
    Step "$tag disks json lists the VHDX and its gap usable" ([bool]($r.Code -eq 0 -and $row -and $row.bus -eq 'file-backed-virtual' -and $gap -and $gap.usable)) "gap $($gap.offset)+$($gap.length)"
    if (-not $gap) { continue }

    $r = Filedo @('vd', 'new', 'part', "disk:{$guid}", 'size', '400M', 'at', "$($gap.offset)", 'fast', 'as', "pd$tag", 'label', "PD$tag", 'force')
    Step "$tag new part fast" ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @('vd', 'new', 'part', "disk:{$guid}", 'size', 'max', 'vault', 'as', "pv$tag", 'p:secret-one', 'force')
    Step "$tag new part vault in the rest" ($r.Code -eq 0) "exit $($r.Code)"
    $fd = @(Get-Partition -DiskNumber $n | Where-Object { $_.GptType -eq $FileDOType })
    Step "$tag two FileDO entries, sentinels unchanged" ($fd.Count -eq 2 -and (Sentinels $n | ConvertTo-Json -Compress) -eq $before -and ((SentinelHashes $n) -join '|') -eq $hashes) "$($fd.Count) FileDO entries"
    $noVolume = -not (@($fd | Get-Volume -ErrorAction SilentlyContinue | Where-Object { $_ }).Count)
    Step "$tag the FileDO partitions get no volume" $noVolume

    $r = Filedo @('vd', 'list'); Step "$tag list" ($r.Code -eq 0 -and $r.Text -match "pd$tag" -and $r.Text -match 'partition disk')
    $r = Filedo @('vd', 'status', 'json'); Step "$tag status json carrier" ($r.Code -eq 0 -and $r.Text -match '"carrier":"partition"' -and $r.Text -match '"locator":"fdpart:')
    $r = Filedo @('vd', 'info', "pd$tag"); Step "$tag info" ($r.Code -eq 0 -and $r.Text -match 'Container id')

    $letter = FreeLetter
    $r = Filedo @('vd', 'mount', "pd$tag", 'as', $letter)
    $mounted = $r.Code -eq 0 -and (Test-Path "$letter\")
    Step "$tag mount at $letter (formats NTFS)" $mounted "exit $($r.Code)"
    if ($mounted) {
      $payload = [byte[]]::new(3MB); (New-Object Random 7).NextBytes($payload)
      [IO.File]::WriteAllBytes("$letter\payload.bin", $payload)
      $back = [IO.File]::ReadAllBytes("$letter\payload.bin")
      Step "$tag a file through the volume" ([Convert]::ToBase64String($back) -eq [Convert]::ToBase64String($payload))
      $r = Filedo @('vd', 'mount', "pd$tag"); Step "$tag second mount is busy" ($r.Code -eq 8) "exit $($r.Code)"
      $r = Filedo @('vd', 'verify', "pd$tag"); Step "$tag verify while mounted is busy" ($r.Code -eq 8) "exit $($r.Code)"
      $r = Filedo @('vd', 'compact', "pd$tag"); Step "$tag compact refused" ($r.Code -eq 6) "exit $($r.Code)"
      $r = UnmountRetry "pd$tag"; Step "$tag unmount" ($r.Code -eq 0 -and $r.Text -match 'closed cleanly') "exit $($r.Code)"
    }
    $r = Filedo @('vd', 'verify', "pd$tag"); Step "$tag verify" ($r.Code -eq 0) "exit $($r.Code)"
    $img = Join-Path $out "$tag-vol.img"
    $r = Filedo @('vd', 'export', "pd$tag", $img, 'raw'); Step "$tag export raw" ($r.Code -eq 0 -and (Test-Path $img)) "exit $($r.Code)"
    $file = Join-Path $out "$tag-image.fdd"
    $r = Filedo @('vd', 'image', "pd$tag", 'to', $file); Step "$tag image to a file" ($r.Code -eq 0 -and (Test-Path $file)) "exit $($r.Code)"
    $r = Filedo @($file, 'verify'); Step "$tag the image verifies as a file" ($r.Code -eq 0) "exit $($r.Code)"
    $clone = Join-Path $out "$tag-clone.fdd"
    $r = Filedo @('vd', 'clone', "pd$tag", $clone); Step "$tag clone to a file" ($r.Code -eq 0 -and (Test-Path $clone)) "exit $($r.Code)"
    $r = Filedo @('vd', 'pass', "pv$tag", 'p:secret-one', 'new', 'p:secret-two'); Step "$tag pass on the vault disk" ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @('vd', 'verify', "pv$tag", 'p:secret-two'); Step "$tag the new credential opens it" ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @('vd', 'verify', "pv$tag", 'p:secret-one'); Step "$tag the old credential does not" ($r.Code -eq 3) "exit $($r.Code)"

    $loc = (StatusRow "pv$tag").locator
    $r = Filedo @('vd', 'forget', "pv$tag")
    $kept = @(Get-Partition -DiskNumber $n | Where-Object { $_.GptType -eq $FileDOType }).Count -eq 2
    Step "$tag forget keeps the partition" ($r.Code -eq 0 -and $loc -and $kept -and -not (StatusRow "pv$tag")) "exit $($r.Code), $loc"
    $r = Filedo @('vd', 'adopt', "$loc", 'as', "pv$tag"); Step "$tag adopt registers it again" ($r.Code -eq 0 -and (StatusRow "pv$tag").locator -eq $loc) "exit $($r.Code)"
    $r = Filedo @('vd', 'verify', "pv$tag", 'p:secret-two'); Step "$tag the adopted disk verifies" ($r.Code -eq 0) "exit $($r.Code)"

    $r = Filedo @('vd', 'auto', "pd$tag", 'logon')
    $task = Get-ScheduledTask -TaskPath '\FileDO\' -TaskName "FileDO Mount pd$tag" -ErrorAction SilentlyContinue
    Step "$tag auto logon makes the task" ($r.Code -eq 0 -and $task) "exit $($r.Code)"
    if ($task) {
      # The task's own command line, run here: Task Scheduler would start it without this run's private
      # state folder (FILEDO_STATE_DIR), where the disk is not registered.
      Step "$tag the disk is not mounted before the task runs" (-not (StatusRow "pd$tag").mount)
      $act = $task.Actions | Select-Object -First 1
      $proc = Start-Process -FilePath $act.Execute -ArgumentList $act.Arguments -PassThru -WindowStyle Hidden
      $up = $false
      for ($i = 0; $i -lt 90 -and -not $up; $i++) { Start-Sleep -Seconds 1; $up = [bool](StatusRow "pd$tag").mount }
      Step "$tag the task's command mounts it (keep)" $up "after $i s: $($act.Execute) $($act.Arguments)"
      $r = UnmountRetry "pd$tag"; Step "$tag unmount after the task" ($r.Code -eq 0) "exit $($r.Code)"
      $gone = $proc.WaitForExit(30000)
      Step "$tag the keep watcher ends with the unmount" $gone
      if (-not $gone) { Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue }
    }
    $r = Filedo @('vd', 'auto', 'off', "pd$tag")
    $task = Get-ScheduledTask -TaskPath '\FileDO\' -TaskName "FileDO Mount pd$tag" -ErrorAction SilentlyContinue
    Step "$tag auto off removes it" ($r.Code -eq 0 -and -not $task) "exit $($r.Code)"

    $r = Filedo @('vd', 'destroy', "pv$tag", 'force'); Step "$tag destroy" ($r.Code -eq 0) "exit $($r.Code)"
    $r = Filedo @('vd', 'destroy', "pd$tag", 'wipe', 'force'); Step "$tag destroy wipe" ($r.Code -eq 0) "exit $($r.Code)"

    # An unfinished creation, made by hand: a FileDO-type partition in the gap. Garbage in its header
    # positions is refused by adopt; zeros there are adopted as unfinished, refused by mount, deleted by destroy.
    $np = try { New-Partition -DiskNumber $n -Offset $gap.offset -Size 64MB -GptType $FileDOType -ErrorAction Stop } catch { $null }
    if ($np) {
      $pp = PartPath $n $np.PartitionNumber
      $ploc = 'fdpart:' + $np.Guid
      [RawDev]::Fill($pp, 0, 4096, 7); [RawDev]::Fill($pp, 64MB - 4096, 4096, 8)
      $r = Filedo @('vd', 'adopt', $ploc, 'as', "pu$tag"); Step "$tag adopt refuses a partition holding no container" ($r.Code -eq 4) "exit $($r.Code)"
      [RawDev]::Zero($pp, 0, 4096); [RawDev]::Zero($pp, 64MB - 4096, 4096)
      $r = Filedo @('vd', 'adopt', $ploc, 'as', "pu$tag"); Step "$tag adopt registers an unfinished creation" ($r.Code -eq 0) "exit $($r.Code)"
      $r = Filedo @('vd', 'mount', "pu$tag"); Step "$tag an unfinished disk does not mount" ($r.Code -ne 0 -and -not (StatusRow "pu$tag").mount) "exit $($r.Code)"
      $r = Filedo @('vd', 'destroy', "pu$tag", 'force'); Step "$tag destroy deletes the unfinished partition" ($r.Code -eq 0) "exit $($r.Code)"
      Get-Partition -DiskNumber $n | Where-Object { $_.GptType -eq $FileDOType } | Remove-Partition -Confirm:$false -ErrorAction SilentlyContinue
    } else {
      Step "$tag make an unfinished partition by hand" $false 'New-Partition failed'
    }
    $fd = @(Get-Partition -DiskNumber $n | Where-Object { $_.GptType -eq $FileDOType })
    Step "$tag the layout is the sentinels again, bytes unchanged" ($fd.Count -eq 0 -and (Sentinels $n | ConvertTo-Json -Compress) -eq $before -and ((SentinelHashes $n) -join '|') -eq $hashes)
  }

  # An MBR disk is refused by name, with the unsupported class.
  $mbr = Join-Path $out 'mbr.vhdx'
  $vhds.Add($mbr)
  $md = New-VHD -Path $mbr -SizeBytes 256MB -Dynamic | Mount-VHD -Passthru | Get-Disk
  Set-Disk -Number $md.Number -IsOffline $false -ErrorAction SilentlyContinue
  Set-Disk -Number $md.Number -IsReadOnly $false -ErrorAction SilentlyContinue
  Initialize-Disk -Number $md.Number -PartitionStyle MBR
  $r = Filedo @('vd', 'new', 'part', "$($md.Number)", 'size', 'max', 'force')
  Step 'an MBR disk is refused (class 6)' ($r.Code -eq 6 -and $r.Text -match 'MBR') "exit $($r.Code)"
} finally {
  UnmountAll
  foreach ($v in $vhds) { Dismount-VHD -Path $v -ErrorAction SilentlyContinue; if (-not $KeepEvidence) { Remove-Item $v -Force -ErrorAction SilentlyContinue } }
}

$failed = @($results | Where-Object { -not $_.Pass })
if ($results.Count -eq 0) { Say 'COULD NOT VERIFY: no step ran'; exit 2 }
$results | Format-Table -AutoSize | Out-String | Tee-Object -Append $log | Write-Host
if ($failed.Count) { Say "FAIL: $($failed.Count) of $($results.Count) steps"; exit 1 }
Say "PASS: $($results.Count) steps"
exit 0
