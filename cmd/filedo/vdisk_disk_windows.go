//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// The disk a mounted container presents, as Windows sees it: found by the
// serial number the block server derives from the container id, brought
// online or taken offline, partitioned and formatted the first time, lettered,
// and locked and dismounted before it is taken away. Every function here runs
// in the elevated step.

const (
	ioctlStorageQueryProperty  = 0x002D1400
	ioctlDiskGetDriveLayoutEx  = 0x00070050
	ioctlDiskSetDiskAttributes = 0x0007C0F4
	ioctlDiskUpdateProperties  = 0x00070140
	ioctlVolumeGetDiskExtents  = 0x00560000
	fsctlDismountVolume        = 0x00090020

	diskAttributeOffline  = 0x1
	diskAttributeReadOnly = 0x2
)

func openDisk(n int, access uint32) (windows.Handle, error) {
	p, _ := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\PhysicalDrive%d`, n))
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

func ioctl(h windows.Handle, code uint32, in []byte, out []byte) (uint32, error) {
	var n uint32
	var inp, outp *byte
	if len(in) > 0 {
		inp = &in[0]
	}
	if len(out) > 0 {
		outp = &out[0]
	}
	err := windows.DeviceIoControl(h, code, inp, uint32(len(in)), outp, uint32(len(out)), &n, nil)
	return n, err
}

// diskSerial reads the serial number Windows reports for PhysicalDrive n.
func diskSerial(n int) (string, error) {
	h, err := openDisk(n, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	q := make([]byte, 12) // STORAGE_PROPERTY_QUERY: StorageDeviceProperty, PropertyStandardQuery
	out := make([]byte, 4096)
	if _, err := ioctl(h, ioctlStorageQueryProperty, q, out); err != nil {
		return "", err
	}
	off := binary.LittleEndian.Uint32(out[24:]) // SerialNumberOffset
	if off == 0 || int(off) >= len(out) {
		return "", nil
	}
	end := bytes.IndexByte(out[off:], 0)
	if end < 0 {
		end = len(out) - int(off)
	}
	return strings.TrimSpace(string(out[off : int(off)+end])), nil
}

// storageBusTypeiSCSI is BusTypeiScsi of STORAGE_BUS_TYPE (winioctl.h).
const storageBusTypeiSCSI = 9

// diskBusAndVendor reads the bus and the SCSI vendor id Windows reports for
// PhysicalDrive n: the BusType of its STORAGE_DEVICE_DESCRIPTOR, at offset 28,
// and the string at VendorIdOffset (offset 12; 0 when there is none).
func diskBusAndVendor(n int) (uint32, string, error) {
	h, err := openDisk(n, 0)
	if err != nil {
		return 0, "", err
	}
	defer windows.CloseHandle(h)
	q := make([]byte, 12) // STORAGE_PROPERTY_QUERY: StorageDeviceProperty, PropertyStandardQuery
	out := make([]byte, 4096)
	got, err := ioctl(h, ioctlStorageQueryProperty, q, out)
	if err != nil {
		return 0, "", err
	}
	if got < 32 {
		return 0, "", fmt.Errorf("the storage descriptor is %d bytes", got)
	}
	vendor := ""
	if off := binary.LittleEndian.Uint32(out[12:]); off != 0 && off < got {
		v := out[off:got]
		if end := bytes.IndexByte(v, 0); end >= 0 {
			v = v[:end]
		}
		vendor = strings.TrimSpace(string(v))
	}
	return binary.LittleEndian.Uint32(out[28:]), vendor, nil
}

// vdSerialSpelling is the only serial a container's disk has: FDD and 20
// upper-case hex digits (vdisk.DiskSerial). Anything else names no container
// disk (AUD-32-F8).
var vdSerialSpelling = regexp.MustCompile(`^FDD[0-9A-F]{20}$`)

// findDiskBySerial waits up to timeout for the disk whose serial is serial.
// The S0 measurement saw the disk arrive in 59-94 ms, and after refused
// logouts in about 60 s, so the wait is long and the poll is short.
func findDiskBySerial(serial string, timeout time.Duration) (int, error) {
	if serial == "" {
		return -1, fmt.Errorf("the container's disk serial is empty; no disk was selected")
	}
	if !vdSerialSpelling.MatchString(serial) {
		return -1, fmt.Errorf("%q is not a container disk's serial (FDD and 20 hex digits); no disk was selected", serial)
	}
	deadline := time.Now().Add(timeout)
	for {
		for n := 0; n < 128; n++ {
			if s, err := diskSerial(n); err == nil && s == serial {
				return n, nil
			}
		}
		if time.Now().After(deadline) {
			return -1, fmt.Errorf("Windows did not present the container's disk (serial %s) within %s", serial, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// diskIsBlank reports whether the disk carries no partition table: a container
// that has never been formatted.
func diskIsBlank(n int) (bool, error) {
	h, err := openDisk(n, windows.GENERIC_READ)
	if err != nil {
		return false, err
	}
	defer windows.CloseHandle(h)
	out := make([]byte, 64<<10)
	if _, err := ioctl(h, ioctlDiskGetDriveLayoutEx, nil, out); err != nil {
		return false, err
	}
	style := binary.LittleEndian.Uint32(out[0:])
	count := binary.LittleEndian.Uint32(out[4:])
	const styleRaw = 2
	return style == styleRaw || count == 0, nil
}

// setDiskAttributes sets or clears the offline and read-only attributes, for
// this boot only (the attributes are not persisted: a container's disk is
// brought online by every mount anyway).
func setDiskAttributes(n int, attrs, mask uint64) error {
	h, err := openDisk(n, windows.GENERIC_READ|windows.GENERIC_WRITE)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	in := make([]byte, 40) // SET_DISK_ATTRIBUTES
	binary.LittleEndian.PutUint32(in[0:], 40)
	binary.LittleEndian.PutUint64(in[8:], attrs)
	binary.LittleEndian.PutUint64(in[16:], mask)
	if _, err := ioctl(h, ioctlDiskSetDiskAttributes, in, nil); err != nil {
		return err
	}
	_, _ = ioctl(h, ioctlDiskUpdateProperties, nil, nil)
	return nil
}

func setDiskOnline(n int, readOnly bool) error {
	var attrs uint64
	if readOnly {
		attrs = diskAttributeReadOnly
	}
	return setDiskAttributes(n, attrs, diskAttributeOffline|diskAttributeReadOnly)
}

func setDiskOffline(n int) error {
	return setDiskAttributes(n, diskAttributeOffline, diskAttributeOffline)
}

// volumesOnDisk lists the volume GUID paths (\\?\Volume{..}\) with an extent on
// disk n.
func volumesOnDisk(n int) ([]string, error) {
	buf := make([]uint16, windows.MAX_PATH+1)
	fh, err := windows.FindFirstVolume(&buf[0], uint32(len(buf)))
	if err != nil {
		return nil, err
	}
	defer windows.FindVolumeClose(fh)
	var out []string
	for {
		name := windows.UTF16ToString(buf)
		if volumeOnDisk(name, n) {
			out = append(out, name)
		}
		if err := windows.FindNextVolume(fh, &buf[0], uint32(len(buf))); err != nil {
			break
		}
	}
	return out, nil
}

func volumeOnDisk(guidPath string, n int) bool {
	p, _ := windows.UTF16PtrFromString(strings.TrimSuffix(guidPath, `\`))
	h, err := windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	out := make([]byte, 8+24*16) // VOLUME_DISK_EXTENTS with up to 16 extents
	if _, err := ioctl(h, ioctlVolumeGetDiskExtents, nil, out); err != nil {
		return false
	}
	count := binary.LittleEndian.Uint32(out[0:])
	for i := uint32(0); i < count && i < 16; i++ {
		if binary.LittleEndian.Uint32(out[8+24*i:]) == uint32(n) {
			return true
		}
	}
	return false
}

// waitVolumeOnDisk waits for the file system volume on disk n to appear.
func waitVolumeOnDisk(n int, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		vols, _ := volumesOnDisk(n)
		if len(vols) > 0 {
			return vols[0], nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no volume appeared on disk %d within %s", n, timeout)
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// vdConfirmFileSystem reads the file system of a volume (its GUID path) with
// GetVolumeInformation and fails unless it is fs: a format is claimed only
// when the volume reads back as what was asked for (AUD-31-F2).
func vdConfirmFileSystem(vol, fs string) error {
	p, err := windows.UTF16PtrFromString(vol)
	if err != nil {
		return err
	}
	name := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		return fmt.Errorf("the new volume's file system could not be read (%v); it is not taken as formatted", err)
	}
	if got := windows.UTF16ToString(name); !strings.EqualFold(got, fs) {
		return fmt.Errorf("the new volume reads as %q, not the %s it was formatted as; it is not taken as formatted", got, fs)
	}
	return nil
}

// volumeLetter returns the drive letter ("X:") a volume is mounted at, or "".
func volumeLetter(guidPath string) string {
	p, _ := windows.UTF16PtrFromString(guidPath)
	buf := make([]uint16, 1024)
	var n uint32
	if err := windows.GetVolumePathNamesForVolumeName(p, &buf[0], uint32(len(buf)), &n); err != nil {
		return ""
	}
	for _, s := range multiSZ(buf[:n]) {
		if len(s) == 3 && s[1] == ':' {
			return strings.ToUpper(s[:2])
		}
	}
	return ""
}

func multiSZ(b []uint16) []string {
	var out []string
	start := 0
	for i, c := range b {
		if c == 0 {
			if i == start {
				break
			}
			out = append(out, windows.UTF16ToString(b[start:i]))
			start = i + 1
		}
	}
	return out
}

// letterFree reports whether X: is unused.
func letterFree(letter string) bool {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return false
	}
	return mask&(1<<uint(strings.ToUpper(letter)[0]-'A')) == 0
}

// assignLetter mounts the volume at want (or at the first free letter from D:
// when want is ""), unless it already has a letter and no particular one was
// asked for. It returns the letter.
func assignLetter(guidPath, want string) (string, error) {
	have := volumeLetter(guidPath)
	if want == "" && have != "" {
		return have, nil
	}
	if want != "" && strings.EqualFold(have, want) {
		return have, nil
	}
	if want == "" {
		for c := 'D'; c <= 'Z'; c++ {
			if letterFree(string(c)) {
				want = string(c) + ":"
				break
			}
		}
		if want == "" {
			return "", errTransport("no drive letter is free")
		}
	} else if !letterFree(want) {
		return "", errTransport(fmt.Sprintf("the drive letter %s is in use", want))
	}
	if have != "" {
		hp, _ := windows.UTF16PtrFromString(have + `\`)
		_ = windows.DeleteVolumeMountPoint(hp)
	}
	mp, _ := windows.UTF16PtrFromString(want + `\`)
	vp, _ := windows.UTF16PtrFromString(guidPath)
	if err := windows.SetVolumeMountPoint(mp, vp); err != nil {
		return "", fmt.Errorf("could not assign %s to the volume: %w", want, err)
	}
	return want, nil
}

// lockAndDismount flushes, locks and dismounts the volume at guidPath. A lock
// that cannot be taken means a file on the volume is open: without force that
// is busy, class 8, and nothing is changed; with force the volume is
// dismounted anyway, which invalidates every open handle. locked reports
// whether the file system let go by itself.
func lockAndDismount(guidPath string, force bool) (locked bool, err error) {
	p, _ := windows.UTF16PtrFromString(strings.TrimSuffix(guidPath, `\`))
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false, fmt.Errorf("could not open the volume: %w", err)
	}
	defer windows.CloseHandle(h)
	_ = windows.FlushFileBuffers(h)
	// Explorer and the indexer touch a new volume for a moment; a few tries
	// tell that apart from a file somebody holds open.
	for i := 0; i < 10; i++ {
		if _, err = ioctl(h, fsctlLockVolume, nil, nil); err == nil {
			locked = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !locked && !force {
		return false, errBusy("a file on the volume is open (unmount force detaches it anyway and marks the container not closed cleanly)")
	}
	if _, err := ioctl(h, fsctlDismountVolume, nil, nil); err != nil {
		// A volume whose server is gone answers "the device is not ready":
		// it is already gone for the file system, and a forced detach goes on
		// to take the disk away (S3 kit case C6).
		if !force {
			return locked, fmt.Errorf("could not dismount the volume: %w", err)
		}
		vdLogf("dismount under force failed, continuing: %v", err)
	}
	return locked, nil
}

// setNotIndexed marks the root of a freshly formatted volume "not content
// indexed", the same bit Explorer's "Allow files on this drive to have
// contents indexed" clears. NTFS gives it to every file and folder created
// below, so the indexer leaves the whole volume alone (owner decision,
// 2026-09-27). It is set once, at format: the owner may turn it back on.
func setNotIndexed(guidPath string) error {
	root := strings.TrimSuffix(guidPath, `\`) + `\`
	p, _ := windows.UTF16PtrFromString(root)
	a, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(p, a|windows.FILE_ATTRIBUTE_NOT_CONTENT_INDEXED)
}

// defenderExclude adds (add true) or removes the backing file as a Microsoft
// Defender exclusion path, through the in-box Defender cmdlets. The file holds
// only ciphertext or obfuscated sectors, so scanning it finds nothing and costs
// a read of the whole container; the volume's own files are still scanned. It
// needs administrator rights and runs in the elevated step. present reports an
// exclusion that was there before, which the unmount must then leave alone.
func defenderExclude(path string, add bool) (present bool, err error) {
	script := `$ErrorActionPreference = 'Stop'
$containerPath = ` + psDataExpr(path) + `
$pref = Get-MpPreference
$have = @($pref.ExclusionPath) -contains $containerPath
`
	if add {
		script += `if ($have) { 'present' } else { Add-MpPreference -ExclusionPath $containerPath; 'added' }
`
	} else {
		script += `if ($have) { Remove-MpPreference -ExclusionPath $containerPath; 'removed' } else { 'absent' }
`
	}
	out, err := vdPowerShell(script)
	text := strings.TrimSpace(out)
	if err != nil {
		return false, fmt.Errorf("Microsoft Defender did not take the change (%v): %s", err, text)
	}
	return text == "present", nil
}

var labelUnsafe = regexp.MustCompile(`[^A-Za-z0-9 _-]`)

// vdFileSystem is the file system a format writes: NTFS unless exFAT was
// asked for (spec 5.2, Q9). Anything else never reaches here - the console
// parser refuses it.
func vdFileSystem(fs string) string {
	if strings.EqualFold(fs, "exfat") {
		return "exFAT"
	}
	return "NTFS"
}

// formatBlankDisk partitions a blank disk (GPT, one partition) and formats it
// with the in-box Storage cmdlets, the documented tools (plan S3, Q18).
// diskpart was the first choice and took 53 s on the S3 kit's first run -
// the Virtual Disk Service it starts - where the S0 measurement saw these
// cmdlets take about 4 s. Their output goes to the mount log. The first mount
// formats a blank disk NTFS; `format` (clear set) empties a disk that holds a
// volume first, which is what destroys its contents, and may ask for exFAT.
func formatBlankDisk(n int, serial, label, fs string, clearFirst bool, logf func(string, ...interface{})) error {
	// A disk number may change after attach. Prove its serial and bus before
	// either the explicit clear or the first format; the script checks again
	// immediately before Clear-Disk (AUD-34-F1).
	if err := vdProveContainerDisk(n, serial); err != nil {
		return err
	}
	script, err := formatScript(n, serial, label, fs, clearFirst)
	if err != nil {
		return err
	}
	out, err := vdPowerShell(script)
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			logf("format: %s", line)
		}
	}
	if err != nil {
		return fmt.Errorf("the new volume could not be formatted (%v); the output is in the mount log", err)
	}
	return nil
}

// vdProveContainerDisk fails unless PhysicalDrive n reports the container's
// serial number and the iSCSI bus: the two facts that make it the disk the
// mount just attached.
func vdProveContainerDisk(n int, serial string) error {
	if err := vdCheckContainerDisk(n, serial); err != nil {
		return fmt.Errorf("%v; nothing was cleared", err)
	}
	return nil
}

// vdCheckContainerDisk is the proof behind every step that changes a disk:
// PhysicalDrive n reports the container's serial (FDD and 20 hex digits), the
// iSCSI bus and FileDO's SCSI vendor id (AUD-34-F1, AUD-32-F8).
func vdCheckContainerDisk(n int, serial string) error {
	if serial == "" {
		return fmt.Errorf("the container's disk serial is empty")
	}
	if !vdSerialSpelling.MatchString(serial) {
		return fmt.Errorf("%q is not a container disk's serial", serial)
	}
	got, err := diskSerial(n)
	if err != nil {
		return fmt.Errorf("disk %d could not be identified (%v)", n, err)
	}
	if got != serial {
		return fmt.Errorf("disk %d is not the container's disk (its serial is not %s)", n, serial)
	}
	bus, vendor, err := diskBusAndVendor(n)
	if err != nil {
		return fmt.Errorf("disk %d could not be identified (%v)", n, err)
	}
	if bus != storageBusTypeiSCSI {
		return fmt.Errorf("disk %d is not on the iSCSI bus", n)
	}
	if !strings.EqualFold(vendor, "FileDO") {
		return fmt.Errorf("disk %d is not a FileDO disk (its vendor is %q)", n, vendor)
	}
	return nil
}

// formatScript is the Storage-cmdlet script that partitions and formats disk n.
// It runs as one unit (vdPowerShell), so a failing step ends it: the first error
// is the last thing that runs, never hidden by a line after it.
func formatScript(n int, serial, label, fs string, clearFirst bool) (string, error) {
	serial = labelUnsafe.ReplaceAllString(serial, "")
	if serial == "" {
		return "", fmt.Errorf("the container's disk serial is empty; nothing was formatted")
	}
	label = strings.TrimSpace(labelUnsafe.ReplaceAllString(label, ""))
	if len(label) > 32 {
		label = label[:32]
	}
	if label == "" {
		label = "FileDO"
	}
	fs = vdFileSystem(fs)
	clearStep := ""
	if clearFirst {
		clearStep = fmt.Sprintf(`Clear-Disk -Number %d -RemoveData -RemoveOEM -Confirm:$false
"cleared in $($sw.ElapsedMilliseconds) ms"
`, n)
	}
	return fmt.Sprintf(`$ErrorActionPreference = 'Stop'
$sw = [Diagnostics.Stopwatch]::StartNew()
Update-HostStorageCache
$d = Get-Disk -Number %d
if (-not $d -or [string]$d.SerialNumber -ne '%s' -or $d.BusType -ne 'iSCSI') { throw "disk %d is not the container's disk; nothing was formatted" }
%sInitialize-Disk -Number %d -PartitionStyle GPT
"initialized in $($sw.ElapsedMilliseconds) ms"
$p = New-Partition -DiskNumber %d -UseMaximumSize
"partition $($p.PartitionNumber) in $($sw.ElapsedMilliseconds) ms"
$v = $p | Format-Volume -FileSystem %s -NewFileSystemLabel '%s' -Confirm:$false
if (-not $v -or $v.FileSystem -ne '%s') { throw "Format-Volume did not return the requested file system" }
"formatted $($v.FileSystem) '$($v.FileSystemLabel)' in $($sw.ElapsedMilliseconds) ms"
`, n, serial, n, clearStep, n, n, fs, label, fs), nil
}

// pnpVetoHolder names the process Windows says stopped the removal of a
// device of ours in the last two minutes (Kernel-PnP event 225, S0 section
// 4.6), or "" when none did.
func pnpVetoHolder() string {
	sys, _ := windows.GetSystemDirectory()
	q := "*[System[Provider[@Name='Microsoft-Windows-Kernel-PnP'] and (EventID=225) and TimeCreated[timediff(@SystemTime) <= 120000]]]"
	out, err := exec.Command(filepath.Join(sys, "wevtutil.exe"), "qe", "System", "/q:"+q, "/c:5", "/rd:true", "/f:text").Output()
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`application (\S+) with process id (\d+) stopped the removal or ejection for the device (\S+)`)
	for _, m := range re.FindAllStringSubmatch(string(out), -1) {
		if strings.Contains(strings.ToUpper(m[3]), "VEN_FILEDO") {
			return fmt.Sprintf("%s (process %s)", filepath.Base(m[1]), m[2])
		}
	}
	return ""
}
