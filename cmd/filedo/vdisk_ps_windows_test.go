//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SP-0070 AUD-34-F1/F2/F3/F4 and SP-0067 AUD-31-F2/F3/F5: the PowerShell that
// formats a disk, mounts an image or sets a Defender exclusion runs as ONE
// unit, so a failing step or a tripped guard ends the script; a path is data,
// never code; and what a script prints is only believed when it has the shape
// of an answer.

func psAvailable(t *testing.T) {
	t.Helper()
	if _, err := vdPowerShell("'ok'"); err != nil {
		t.Skipf("Windows PowerShell is not available: %v", err)
	}
}

func TestVdPowerShell_AThrowEndsTheScript(t *testing.T) {
	psAvailable(t)
	out, err := vdPowerShell("$d = 1\nif ($d -eq 1) { throw \"guard tripped\" }\nWrite-Output 'AFTER-GUARD-RAN'")
	if err == nil {
		t.Fatalf("a script whose guard threw exited 0\n%s", out)
	}
	if strings.Contains(out, "AFTER-GUARD-RAN") {
		t.Fatalf("a line after the tripped guard ran (the stdin -Command - shape):\n%s", out)
	}
	if !strings.Contains(out, "guard tripped") {
		t.Errorf("the guard's message is not in the output: %q", out)
	}
}

func TestVdPowerShell_AFailingCmdletEndsTheScript(t *testing.T) {
	psAvailable(t)
	out, err := vdPowerShell("Get-Item -LiteralPath 'Z:\\definitely\\not\\here\\" + t.Name() + "'\nWrite-Output 'FORMATTED-ANYWAY'")
	if err == nil || strings.Contains(out, "FORMATTED-ANYWAY") {
		t.Fatalf("a failing cmdlet was hidden by the line after it: err=%v out=%q", err, out)
	}
}

func TestPsQuote_APathIsDataNotCode(t *testing.T) {
	psAvailable(t)
	dir := t.TempDir()
	marker := filepath.Join(dir, "injected.txt")
	for name, path := range map[string]string{
		"ascii apostrophe":  `C:\x'; New-Item -ItemType File -Path ` + psQuote(marker) + ` | Out-Null; '`,
		"typographic quote": "C:\\x\u2019; New-Item -ItemType File -Path " + psQuote(marker) + " | Out-Null; \u2018",
		"low-9 quote":       "C:\\x\u201a; New-Item -ItemType File -Path " + psQuote(marker) + " | Out-Null; \u201b",
	} {
		out, err := vdPowerShell("Write-Output " + psQuote(path))
		if err != nil || strings.TrimSpace(out) != path {
			t.Errorf("%s: the path came back as %q (err %v), want it unchanged", name, out, err)
		}
		if _, statErr := os.Stat(marker); statErr == nil {
			t.Fatalf("%s: the path ran as code (the marker file exists)", name)
		}
	}
}

func TestVdPowerShell_ANonASCIIPathArrivesIntact(t *testing.T) {
	psAvailable(t)
	for _, p := range []string{`C:\Диск\Образ.iso`, `C:\Ordner\Über größe.iso`, "C:\\It\u2019s\\a.iso"} {
		out, err := vdPowerShell("Write-Output " + psQuote(p))
		if err != nil || strings.TrimSpace(out) != p {
			t.Errorf("path %q came back as %q (err %v)", p, out, err)
		}
	}
}

func TestPsDataExpr_AHostilePathStaysData(t *testing.T) {
	psAvailable(t)
	marker := filepath.Join(t.TempDir(), "ran.txt")
	path := "C:\\It's’; New-Item -ItemType File -Path " + psQuote(marker) + "; '.fdd"
	out, err := vdPowerShell("$containerPath = " + psDataExpr(path) + "\n[Console]::Write($containerPath)")
	if err != nil || strings.TrimSpace(out) != path {
		t.Fatalf("path changed in PowerShell: %q, %v", out, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a path ran as PowerShell code: %v", err)
	}
}

func TestVdImageScript_AHostileImagePathStaysData(t *testing.T) {
	psAvailable(t)
	marker := filepath.Join(t.TempDir(), "image-path-ran.txt")
	path := "C:\\It’s’; New-Item -ItemType File -Path " + psQuote(marker) + "; '.iso"
	fakes := `function Mount-DiskImage { param($ImagePath, $Access, [switch]$PassThru) if ($ImagePath -ne $expected) { throw 'wrong mount path' }; [pscustomobject]@{} }
function Get-DiskImage { param($ImagePath) if ($ImagePath -ne $expected) { throw 'wrong lookup path' }; [pscustomobject]@{ Attached = $true } }
function Get-Volume { }
function Dismount-DiskImage { param($ImagePath) if ($ImagePath -ne $expected) { throw 'wrong dismount path' } }
`
	for _, dismount := range []bool{false, true} {
		script := "$expected = " + psDataExpr(path) + "\n" + fakes + vdImageScript(path, dismount, true) + "\n[Console]::Write($imagePath)"
		out, err := vdPowerShell(script)
		if err != nil || strings.TrimSpace(out) != path {
			t.Errorf("dismount=%t: image path changed or became code: %q, %v", dismount, out, err)
		}
		if _, err := os.Stat(marker); !os.IsNotExist(err) {
			t.Fatalf("dismount=%t: image path ran as code: %v", dismount, err)
		}
	}
}

func TestVdPowerShellCtx_AStopAndADeadlineEndTheRun(t *testing.T) {
	psAvailable(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := vdPowerShellCtx(ctx, "Start-Sleep -Seconds 30", time.Minute)
	if !errors.Is(err, errRunStopped) {
		t.Errorf("a stopped run returned %v, want errRunStopped", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the stop took %v to end the script", took)
	}
	start = time.Now()
	_, err = vdPowerShellCtx(context.Background(), "Start-Sleep -Seconds 30", 400*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Errorf("a run past its deadline returned %v", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the deadline took %v to end the script", took)
	}
}

// fakeStorage defines the Storage cmdlets formatScript calls as script
// functions (a function outranks a cmdlet), so the real script can be run
// without a disk: the fakes record what they did.
func fakeStorage(serial, bus string, failAt string) string {
	fail := func(name string) string {
		if failAt == name {
			return `throw "` + name + ` failed"`
		}
		return ""
	}
	return `function Update-HostStorageCache { }
function Get-Disk { param($Number) [pscustomobject]@{ SerialNumber = '` + serial + `'; BusType = '` + bus + `' } }
function Clear-Disk { param($Number, [switch]$RemoveData, [switch]$RemoveOEM, $Confirm) ` + fail("Clear-Disk") + `; Write-Output 'CLEARED' }
function Initialize-Disk { param($Number, $PartitionStyle) ` + fail("Initialize-Disk") + `; Write-Output 'INITIALIZED' }
function New-Partition { param($DiskNumber, [switch]$UseMaximumSize) ` + fail("New-Partition") + `; [pscustomobject]@{ PartitionNumber = 1 } }
function Format-Volume { param([Parameter(ValueFromPipeline)]$In, $FileSystem, $NewFileSystemLabel, $Confirm) process { ` + fail("Format-Volume") + `; [pscustomobject]@{ FileSystem = $FileSystem; FileSystemLabel = $NewFileSystemLabel } } }
`
}

func TestFormatScript_TheGuardStopsTheClear(t *testing.T) {
	psAvailable(t)
	script, err := formatScript(7, "SERIAL123", "Data", "ntfs", true)
	if err != nil {
		t.Fatal(err)
	}
	// Another disk now carries the number: the serial does not match.
	out, err := vdPowerShell(fakeStorage("SOMEONE-ELSES", "iSCSI", "") + script)
	if err == nil {
		t.Fatalf("the format of a disk that is not the container's exited 0\n%s", out)
	}
	for _, word := range []string{"CLEARED", "INITIALIZED", "formatted NTFS"} {
		if strings.Contains(out, word) {
			t.Fatalf("%q ran after the guard tripped:\n%s", word, out)
		}
	}
	// Right serial, wrong bus.
	out, err = vdPowerShell(fakeStorage("SERIAL123", "SATA", "") + script)
	if err == nil || strings.Contains(out, "CLEARED") {
		t.Fatalf("a SATA disk was cleared: err=%v\n%s", err, out)
	}
	// The container's disk goes all the way through.
	out, err = vdPowerShell(fakeStorage("SERIAL123", "iSCSI", "") + script)
	if err != nil || !strings.Contains(out, "CLEARED") || !strings.Contains(out, "formatted NTFS 'Data'") {
		t.Fatalf("the container's own disk did not format: err=%v\n%s", err, out)
	}
}

func TestFormatScript_AFailingStepIsAnError(t *testing.T) {
	psAvailable(t)
	script, err := formatScript(7, "SERIAL123", "Data", "ntfs", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []string{"Clear-Disk", "Initialize-Disk", "New-Partition", "Format-Volume"} {
		out, err := vdPowerShell(fakeStorage("SERIAL123", "iSCSI", step) + script)
		if err == nil {
			t.Errorf("%s failed but the script exited 0\n%s", step, out)
		}
		if strings.Contains(out, "formatted NTFS") {
			t.Errorf("%s failed but the script still reported the volume formatted\n%s", step, out)
		}
	}
}

func TestFormatScript_ANullFormatResultIsAnError(t *testing.T) {
	psAvailable(t)
	script, err := formatScript(7, "SERIAL123", "Data", "ntfs", true)
	if err != nil {
		t.Fatal(err)
	}
	fakes := fakeStorage("SERIAL123", "iSCSI", "")
	fakes += "function Format-Volume { param([Parameter(ValueFromPipeline)]$In, $FileSystem, $NewFileSystemLabel, $Confirm) process { } }\n"
	out, err := vdPowerShell(fakes + script)
	if err == nil || strings.Contains(out, "formatted NTFS") {
		t.Fatalf("an empty Format-Volume result reported success: err=%v out=%q", err, out)
	}
}

func TestFormatScript_NoSerialNoFormat(t *testing.T) {
	if _, err := formatScript(7, "", "Data", "ntfs", true); err == nil {
		t.Fatal("a clear without a serial was scripted")
	}
	if _, err := formatScript(7, "", "Data", "ntfs", false); err == nil {
		t.Fatal("a first format without a serial was scripted")
	}
}

func TestFormatScript_TheGuardStopsFirstFormat(t *testing.T) {
	psAvailable(t)
	script, err := formatScript(7, "SERIAL123", "Data", "ntfs", false)
	if err != nil {
		t.Fatal(err)
	}
	out, err := vdPowerShell(fakeStorage("FOREIGN", "iSCSI", "") + script)
	if err == nil || strings.Contains(out, "INITIALIZED") || strings.Contains(out, "formatted NTFS") {
		t.Fatalf("a foreign disk was initialized: err=%v out=%q", err, out)
	}
}

func TestFormatScript_ReusesPartitionWhenSpanningDisk(t *testing.T) {
	psAvailable(t)
	script, err := formatScript(7, "SERIAL123", "Data", "ntfs", true)
	if err != nil {
		t.Fatal(err)
	}
	fakes := `function Update-HostStorageCache { }
function Get-Disk { param($Number) [pscustomobject]@{ SerialNumber = 'SERIAL123'; BusType = 'iSCSI'; PartitionStyle = 'GPT'; LargestFreeExtent = 0 } }
function Get-Partition { param($DiskNumber) @([pscustomobject]@{ PartitionNumber = 1; Type = 'Reserved' }, [pscustomobject]@{ PartitionNumber = 2; Type = 'Basic' }) }
function Clear-Disk { param($Number, [switch]$RemoveData, [switch]$RemoveOEM, $Confirm) Write-Output 'CLEARED' }
function Initialize-Disk { param($Number, $PartitionStyle) Write-Output 'INITIALIZED' }
function New-Partition { param($DiskNumber, [switch]$UseMaximumSize) [pscustomobject]@{ PartitionNumber = 1 } }
function Format-Volume { param([Parameter(ValueFromPipeline)]$In, $FileSystem, $NewFileSystemLabel, $Confirm) process { [pscustomobject]@{ FileSystem = $FileSystem; FileSystemLabel = $NewFileSystemLabel } } }
`
	out, err := vdPowerShell(fakes + script)
	if err != nil {
		t.Fatalf("reuse format failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "reusing partition 2") || !strings.Contains(out, "formatted NTFS 'Data'") {
		t.Fatalf("expected reused partition format output, got:\n%s", out)
	}
	if strings.Contains(out, "CLEARED") || strings.Contains(out, "INITIALIZED") {
		t.Fatalf("Clear-Disk or Initialize-Disk ran when partition spanned disk:\n%s", out)
	}
}

func TestFormatScript_ClearsWhenGrownOrNonGpt(t *testing.T) {
	psAvailable(t)
	script, err := formatScript(7, "SERIAL123", "Data", "ntfs", true)
	if err != nil {
		t.Fatal(err)
	}
	// Grown disk with free extent: should clear and repartition
	fakesGrown := `function Update-HostStorageCache { }
function Get-Disk { param($Number) [pscustomobject]@{ SerialNumber = 'SERIAL123'; BusType = 'iSCSI'; PartitionStyle = 'GPT'; LargestFreeExtent = 104857600 } }
function Get-Partition { param($DiskNumber) @([pscustomobject]@{ PartitionNumber = 2; Type = 'Basic' }) }
function Clear-Disk { param($Number, [switch]$RemoveData, [switch]$RemoveOEM, $Confirm) Write-Output 'CLEARED' }
function Initialize-Disk { param($Number, $PartitionStyle) Write-Output 'INITIALIZED' }
function New-Partition { param($DiskNumber, [switch]$UseMaximumSize) [pscustomobject]@{ PartitionNumber = 1 } }
function Format-Volume { param([Parameter(ValueFromPipeline)]$In, $FileSystem, $NewFileSystemLabel, $Confirm) process { [pscustomobject]@{ FileSystem = $FileSystem; FileSystemLabel = $NewFileSystemLabel } } }
`
	out, err := vdPowerShell(fakesGrown + script)
	if err != nil {
		t.Fatalf("grown clear format failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "CLEARED") || !strings.Contains(out, "INITIALIZED") || !strings.Contains(out, "formatted NTFS 'Data'") {
		t.Fatalf("expected clear and initialize for grown disk, got:\n%s", out)
	}
	if strings.Contains(out, "reusing partition") {
		t.Fatalf("reused partition when disk had unallocated extent:\n%s", out)
	}
}

// The Go-side proof runs before any script: a disk that is not the container's
// is refused without touching it.
func TestVdProveContainerDisk_RefusesAForeignDisk(t *testing.T) {
	err := vdProveContainerDisk(0, "NO-SUCH-SERIAL-"+strings.Repeat("Z", 8))
	if err == nil {
		t.Fatal("PhysicalDrive0 was accepted as a container's disk")
	}
	if !strings.Contains(err.Error(), "nothing was cleared") {
		t.Errorf("the refusal does not say nothing was cleared: %v", err)
	}
	if err := vdProveContainerDisk(0, ""); err == nil {
		t.Error("an empty serial was accepted")
	}
}

func TestFindDiskBySerial_EmptySerialSelectsNoDisk(t *testing.T) {
	start := time.Now()
	disk, err := findDiskBySerial("", time.Minute)
	if err == nil || disk != -1 {
		t.Fatalf("empty serial selected disk %d: %v", disk, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("empty serial waited for disk discovery")
	}
}

// A mount whose output is not a list of drive letters is not a mount.
func TestVdImageRun_AFailedMountIsAnError(t *testing.T) {
	psAvailable(t)
	iso := filepath.Join(t.TempDir(), "Image.iso")
	if err := os.WriteFile(iso, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	letters, err := vdImageRun(iso, false, true)
	if err == nil {
		// Do not leave anything mounted if a Windows build accepts an empty ISO.
		vdImageRun(iso, true, true)
		t.Fatalf("a 0-byte ISO mounted as %q", letters)
	}
	if strings.Contains(err.Error(), "CategoryInfo") || strings.Contains(err.Error(), "FullyQualifiedErrorId") {
		t.Errorf("the error carries the script echo: %v", err)
	}
	for _, ok := range []string{"", "E:", "E: F:"} {
		if !vdDriveLetters.MatchString(ok) {
			t.Errorf("%q is a valid answer and was refused", ok)
		}
	}
	for _, bad := range []string{"Mount-DiskImage : The file or directory is corrupted", "E:F:", "E: F", "E:\nF:"} {
		if vdDriveLetters.MatchString(bad) {
			t.Errorf("%q was accepted as drive letters", bad)
		}
	}
}
