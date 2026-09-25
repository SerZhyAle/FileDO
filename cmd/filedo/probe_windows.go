//go:build windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

// runDeviceProbe performs a fast fake-capacity probe by writing unique markers
// directly to raw sectors via \\.\D: and reading them back. Requires
// administrator privileges. The plan and the verdict are probe_core.go's;
// this function opens, locks and sizes the volume, and reports.
func runDeviceProbe(devicePath string) error {
	driveLetter, err := probeExtractDriveLetter(devicePath)
	if err != nil {
		return err
	}
	rawPath := fmt.Sprintf(`\\.\%c:`, driveLetter)

	fmt.Printf("Device Probe (fast fake-capacity detection)\n")
	fmt.Printf("Target : %s  (raw: %s)\n", getEnhancedDeviceInfo(fmt.Sprintf(`%c:`, driveLetter)), rawPath)
	fmt.Printf("Mode   : Administrator raw I/O - no files written to filesystem\n\n")

	// ── Open raw device ─────────────────────────────────────────────────────
	// FILE_FLAG_NO_BUFFERING (0x20000000) bypasses cache manager for direct I/O.
	const fileFlagNoBuffering = 0x20000000
	pathPtr, err := windows.UTF16PtrFromString(rawPath)
	if err != nil {
		return err
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		fileFlagNoBuffering,
		0,
	)
	if err != nil {
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("access denied - run as Administrator to use probe\n"+
				"  Right-click cmd/PowerShell → \"Run as administrator\", then retry:\n"+
				"  filedo %s probe", devicePath)
		}
		return fmt.Errorf("cannot open raw device %s: %w", rawPath, err)
	}
	defer windows.CloseHandle(handle)

	// ── Lock the volume ──────────────────────────────────────────────────────
	// Required: Windows kernel blocks writes to sectors owned by the filesystem
	// driver unless the volume is exclusively locked.
	fmt.Printf("Locking volume %s...\n", rawPath)
	if err := probeLockVolume(handle); err != nil {
		return fmt.Errorf(
			"cannot lock volume %s: %v\n\n"+
				"Close Windows Explorer and all programs using drive %c: then retry.\n"+
				"Tip: in PowerShell run  Stop-Process -Name explorer  before probing.",
			rawPath, err, driveLetter)
	}
	defer func() {
		probeUnlockVolume(handle)
		fmt.Printf("Volume unlocked.\n")
	}()
	fmt.Printf("✓ Volume locked (exclusive write access granted)\n\n")

	// ── Size of the VOLUME, not of the disk ─────────────────────────────────
	// Offsets are applied to the volume handle, so the range must be the
	// volume's length; the drive geometry is the whole disk, and on a disk with
	// several partitions probes ran past the volume's end (CLI-28).
	totalBytes, err := probeVolumeLength(handle)
	if err != nil {
		return fmt.Errorf("cannot read the volume length: %w", err)
	}
	sectorSize := probeSectorSize(handle)

	fmt.Printf("Volume size  : %.2f GB\n", float64(totalBytes)/(1<<30))
	fmt.Printf("Sector size  : %d bytes\n\n", sectorSize)

	dev := &windowsSectorDevice{h: handle}
	fmt.Printf("Saving originals, writing markers, reading them back...\n")
	res, err := probeCore(dev, totalBytes, sectorSize, probeMakeAlignedBuf,
		func(restore func()) func() {
			// A forced exit skips every defer; the restore must still run
			// before the process ends (CLI-07c). It is idempotent.
			return globalInterruptHandler.AddCleanup(func() {
				if globalInterruptHandler.IsForceExit() {
					restore()
				}
			})
		},
		runStopRequested)
	if len(res.restoreErrs) > 0 {
		fmt.Printf("❌ %d sector(s) could NOT be restored:\n", len(res.restoreErrs))
		for _, e := range res.restoreErrs {
			fmt.Printf("   %v\n", e)
		}
		fmt.Printf("   Run chkdsk %c: /f before using the drive.\n", driveLetter)
	} else if res.written > 0 {
		fmt.Printf("✓ All %d written sectors restored\n", res.written)
	}
	if res.unreadable > 0 {
		fmt.Printf("⚠ %d position(s) could not be read and were left untouched.\n", res.unreadable)
	}
	if err != nil {
		return err
	}

	fmt.Println()
	verdict := probeVerdict(res)
	if errors.Is(verdict, errDefect) {
		fmt.Printf("⚠️  FAKE CAPACITY DETECTED\n")
		fmt.Printf("   Markers wrong  : %d / %d (%d answered with another marker's content)\n", res.mismatches+res.aliases, res.written, res.aliases)
		details := map[string]interface{}{
			"claimedCapacityGB": float64(totalBytes) / (1 << 30),
			"markersWrong":      res.mismatches + res.aliases,
			"markersAliased":    res.aliases,
			"markersWritten":    res.written,
		}
		if res.lastGoodOff > 0 {
			fmt.Printf("   Estimated real capacity: under %.2f GB\n", float64(res.firstBadOff)/(1<<30))
			details["estimatedRealCapacityGB"] = float64(res.firstBadOff) / (1 << 30)
		} else {
			fmt.Printf("   Fake starts at the first positions probed - the real capacity is very small.\n")
		}
		fmt.Printf("   Claimed size   : %.2f GB\n", float64(totalBytes)/(1<<30))
		return recordedDefect("fake-capacity", verdict.Error(), details)
	}
	if verdict != nil {
		return verdict
	}
	fmt.Printf("✅ GENUINE: all %d probe markers verified - no fake capacity detected.\n", res.written)
	return nil
}

// windowsSectorDevice is sectorDevice over an open volume handle.
type windowsSectorDevice struct{ h windows.Handle }

func (d *windowsSectorDevice) ReadAt(p []byte, off int64) error  { return probeReadSector(d.h, off, p) }
func (d *windowsSectorDevice) WriteAt(p []byte, off int64) error { return probeWriteSector(d.h, off, p) }

// IOCTL_DISK_GET_LENGTH_INFO returns the length of the volume the handle is
// open on.
const ioctlDiskGetLengthInfo = 0x0007405C

func probeVolumeLength(h windows.Handle) (int64, error) {
	var length int64
	var bytesReturned uint32
	err := windows.DeviceIoControl(h, ioctlDiskGetLengthInfo, nil, 0,
		(*byte)(unsafe.Pointer(&length)), uint32(unsafe.Sizeof(length)), &bytesReturned, nil)
	if err == nil && length > 0 {
		return length, nil
	}
	pos, seekErr := probeSeek(h, 0, 2 /*FILE_END*/)
	if seekErr != nil || pos <= 0 {
		return 0, fmt.Errorf("IOCTL_DISK_GET_LENGTH_INFO: %v; seek fallback: %v", err, seekErr)
	}
	return pos, nil
}

// probeSectorSize reads the sector size from the drive geometry; 512 when it
// cannot be read or is smaller.
func probeSectorSize(h windows.Handle) int {
	_, sector, err := probeGetDiskSize(h)
	if err != nil || sector < 512 {
		return 512
	}
	return sector
}

// ── Windows raw I/O helpers ──────────────────────────────────────────────────

// FSCTL codes for volume locking
const (
	fsctlLockVolume   = 0x00090018
	fsctlUnlockVolume = 0x0009001C
)

func probeLockVolume(h windows.Handle) error {
	var bytesReturned uint32
	return windows.DeviceIoControl(h, fsctlLockVolume, nil, 0, nil, 0, &bytesReturned, nil)
}

func probeUnlockVolume(h windows.Handle) {
	var bytesReturned uint32
	windows.DeviceIoControl(h, fsctlUnlockVolume, nil, 0, nil, 0, &bytesReturned, nil)
}

// probeMakeAlignedBuf allocates a buffer aligned to its own size (safe for
// FILE_FLAG_NO_BUFFERING which requires memory aligned to the sector size).
func probeMakeAlignedBuf(size int) []byte {
	// Over-allocate by size-1 so we can always find an aligned start.
	raw := make([]byte, size*2)
	off := uintptr(unsafe.Pointer(&raw[0])) % uintptr(size)
	if off == 0 {
		return raw[:size]
	}
	start := size - int(off)
	return raw[start : start+size]
}

// IOCTL_DISK_GET_DRIVE_GEOMETRY_EX retrieves geometry including total disk size.
const ioctlDiskGetDriveGeometryEx = 0x000700A0

type diskGeometryEx struct {
	Geometry struct {
		Cylinders         int64
		MediaType         uint32
		TracksPerCylinder uint32
		SectorsPerTrack   uint32
		BytesPerSector    uint32
	}
	DiskSize int64
	Data     [1]byte
}

func probeGetDiskSize(h windows.Handle) (totalBytes int64, sectorSize int, err error) {
	var geom diskGeometryEx
	var bytesReturned uint32
	err = windows.DeviceIoControl(
		h,
		ioctlDiskGetDriveGeometryEx,
		nil, 0,
		(*byte)(unsafe.Pointer(&geom)), uint32(unsafe.Sizeof(geom)),
		&bytesReturned, nil,
	)
	if err != nil {
		// Fallback: try to seek to end
		pos, seekErr := probeSeek(h, 0, 2 /*FILE_END*/)
		if seekErr != nil {
			return 0, 512, fmt.Errorf("DeviceIoControl: %w; seek fallback: %v", err, seekErr)
		}
		return pos, 512, nil
	}
	return geom.DiskSize, int(geom.Geometry.BytesPerSector), nil
}

func probeWriteSector(h windows.Handle, offset int64, data []byte) error {
	if _, err := probeSeek(h, offset, 0); err != nil {
		return err
	}
	var written uint32
	err := windows.WriteFile(h, data, &written, nil)
	if err != nil {
		return err
	}
	if int(written) != len(data) {
		return fmt.Errorf("short write: %d/%d bytes", written, len(data))
	}
	return nil
}

func probeReadSector(h windows.Handle, offset int64, buf []byte) error {
	if _, err := probeSeek(h, offset, 0); err != nil {
		return err
	}
	var read uint32
	err := windows.ReadFile(h, buf, &read, nil)
	if err != nil {
		return err
	}
	if int(read) != len(buf) {
		return fmt.Errorf("short read: %d/%d bytes", read, len(buf))
	}
	return nil
}

// probeSeek wraps SetFilePointerEx via syscall.
// whence: 0=FILE_BEGIN, 1=FILE_CURRENT, 2=FILE_END
func probeSeek(h windows.Handle, offset int64, whence uint32) (int64, error) {
	hi := int32(offset >> 32)
	lo := int32(offset)
	ret, _, err := syscall.Syscall6(
		procSetFilePointer.Addr(), 4,
		uintptr(h),
		uintptr(lo),
		uintptr(unsafe.Pointer(&hi)),
		uintptr(whence),
		0, 0,
	)
	if ret == 0xFFFFFFFF {
		return 0, err
	}
	newPos := int64(hi)<<32 | int64(uint32(ret))
	return newPos, nil
}

var procSetFilePointer = syscall.MustLoadDLL("kernel32.dll").MustFindProc("SetFilePointer")

// runDeviceProbeCheck is the entry-point used by command_handlers.go.
// It also checks admin elevation and gives a friendly message if not elevated.
func runDeviceProbeCheck(devicePath string, assumeYes bool, autoRepair bool) error {
	if !isRunningAsAdmin() {
		fmt.Fprintf(os.Stderr,
			"❌ Probe requires Administrator privileges.\n\n"+
				"Re-run this command from an elevated shell:\n"+
				"  Right-click PowerShell / cmd → \"Run as administrator\"\n"+
				"  filedo %s probe\n", devicePath)
		return fmt.Errorf("not running as administrator")
	}

	driveLetter, err := probeExtractDriveLetter(devicePath)
	if err != nil {
		return err
	}

	root := fmt.Sprintf("%c:\\", driveLetter)
	if _, err := os.Stat(root); err != nil {
		if !autoRepair {
			return fmt.Errorf("drive %c: is not accessible now. Use 'filedo %c: probe fix' for guided quick format", driveLetter, driveLetter)
		}
		fmt.Printf("⚠️  Drive %c: is already inaccessible.\n", driveLetter)
		fmt.Printf("Skipping probe and starting recovery format flow.\n")
		if !assumeYes {
			if err := probeAskConfirmFormat(driveLetter); err != nil {
				return err
			}
		}
		if err := probeQuickFormat(driveLetter); err != nil {
			probePrintDiskpartGuide(driveLetter)
			return fmt.Errorf("quick format failed for %c:: %w", driveLetter, err)
		}
		fmt.Printf("✅ Quick format completed. Drive %c: should be usable now.\n", driveLetter)
		return nil
	}

	if !assumeYes {
		if err := probeAskConfirmStart(driveLetter); err != nil {
			return err
		}
	}

	if err := runDeviceProbe(devicePath); err != nil {
		return err
	}

	if _, err := os.Stat(root); err == nil {
		return nil
	}

	fmt.Printf("\n⚠️  WARNING: drive %c: is not accessible after probe.\n", driveLetter)
	fmt.Printf("   You may need filesystem repair or quick format.\n")

	if !autoRepair {
		fmt.Printf("   Run manually: chkdsk %c: /f\n", driveLetter)
		fmt.Printf("   Or run: filedo %c: probe fix\n", driveLetter)
		return fmt.Errorf("drive %c: is not accessible after probe", driveLetter)
	}

	if !assumeYes {
		if err := probeAskConfirmFormat(driveLetter); err != nil {
			return err
		}
	}

	if err := probeQuickFormat(driveLetter); err != nil {
		probePrintDiskpartGuide(driveLetter)
		return fmt.Errorf("quick format failed for %c:: %w", driveLetter, err)
	}

	fmt.Printf("✅ Quick format completed. Drive %c: should be usable now.\n", driveLetter)
	return nil
}

// runDeviceRecoverCheck performs recovery flow without running probe.
// It tries chkdsk first, then quick format if needed (or forced).
func runDeviceRecoverCheck(devicePath string, assumeYes bool, forceFormat bool) error {
	if !isRunningAsAdmin() {
		fmt.Fprintf(os.Stderr,
			"❌ Recover requires Administrator privileges.\n\n"+
				"Re-run this command from an elevated shell:\n"+
				"  Right-click PowerShell / cmd → \"Run as administrator\"\n"+
				"  filedo %s recover\n", devicePath)
		return fmt.Errorf("not running as administrator")
	}

	driveLetter, err := probeExtractDriveLetter(devicePath)
	if err != nil {
		return err
	}

	if !assumeYes {
		reader := bufio.NewReader(os.Stdin)
		expected := fmt.Sprintf("%c:", driveLetter)
		fmt.Printf("\nRECOVERY MODE\n")
		fmt.Printf("This will attempt to repair drive %c: (chkdsk, optional quick format).\n", driveLetter)
		fmt.Printf("Type target drive exactly (%s) to continue: ", expected)
		line, _ := reader.ReadString('\n')
		if strings.ToUpper(strings.TrimSpace(line)) != expected {
			return fmt.Errorf("recover cancelled: wrong drive confirmation")
		}
	}

	checkTarget := fmt.Sprintf("%c:", driveLetter)
	if !forceFormat {
		fmt.Printf("Running chkdsk %c: /f /x ...\n", driveLetter)
		chkdskCmd := exec.Command("cmd", "/C", "chkdsk", checkTarget, "/f", "/x")
		out, err := chkdskCmd.CombinedOutput()
		if err != nil {
			fmt.Printf("⚠ chkdsk returned error: %v\n", err)
			if s := strings.TrimSpace(string(out)); s != "" {
				fmt.Printf("%s\n", s)
			}
		}
		if _, statErr := os.Stat(checkTarget + "\\"); statErr == nil {
			fmt.Printf("✅ Drive %c: is accessible after chkdsk.\n", driveLetter)
			return nil
		}
	}

	if !assumeYes {
		if err := probeAskConfirmFormat(driveLetter); err != nil {
			return err
		}
	}

	if err := probeQuickFormat(driveLetter); err != nil {
		probePrintDiskpartGuide(driveLetter)
		return fmt.Errorf("quick format failed for %c:: %w", driveLetter, err)
	}

	fmt.Printf("✅ Quick format completed. Drive %c: should be usable now.\n", driveLetter)
	return nil
}

// probeExtractDriveLetter accepts a drive letter and nothing else: `D`, `D:`,
// `D:\` or `D:/`. It used to take the first letter found anywhere in the
// string, so `\\.\PhysicalDrive1` meant P: and `\\?\Volume{..} recover y fmt`
// formatted V: - with `yes` and `fmt` switching off the confirmation that would
// have shown the wrong letter (CLI-27).
func probeExtractDriveLetter(devicePath string) (rune, error) {
	p := strings.TrimSpace(devicePath)
	if isASCIILetter(p) || driveRootSpelling.MatchString(p) {
		return unicode.ToUpper(rune(p[0])), nil
	}
	return 0, usagef("probe and recover take a drive letter such as D: - %q is not one", devicePath)
}

func probeAskConfirmStart(driveLetter rune) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\nDANGEROUS OPERATION WARNING\n")
	fmt.Printf("Probe uses raw sector writes on drive %c:.\n", driveLetter)
	fmt.Printf("If something goes wrong, filesystem may become unreadable.\n\n")

	fmt.Printf("Type YES to continue: ")
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != "YES" {
		return fmt.Errorf("probe cancelled by user")
	}

	expected := fmt.Sprintf("%c:", driveLetter)
	fmt.Printf("Type target drive exactly (%s) to confirm: ", expected)
	line, _ = reader.ReadString('\n')
	if strings.ToUpper(strings.TrimSpace(line)) != expected {
		return fmt.Errorf("probe cancelled: wrong drive confirmation")
	}

	return nil
}

func probeAskConfirmFormat(driveLetter rune) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\nDESTRUCTIVE RECOVERY WARNING\n")
	fmt.Printf("Quick format will ERASE filesystem metadata on drive %c:.\n", driveLetter)
	fmt.Printf("Data recovery may become impossible.\n\n")

	fmt.Printf("Type FORMAT to allow quick format: ")
	line, _ := reader.ReadString('\n')
	if strings.TrimSpace(line) != "FORMAT" {
		return fmt.Errorf("format cancelled by user")
	}

	expected := fmt.Sprintf("FORMAT %c:", driveLetter)
	fmt.Printf("Type '%s' to confirm target: ", expected)
	line, _ = reader.ReadString('\n')
	if strings.ToUpper(strings.TrimSpace(line)) != expected {
		return fmt.Errorf("format cancelled: wrong target confirmation")
	}

	return nil
}

func probeQuickFormat(driveLetter rune) error {
	formatLabel := "FD_RECOV" // <=11 chars for format.exe compatibility

	// First try to repair filesystem metadata without formatting.
	checkTarget := fmt.Sprintf("%c:", driveLetter)
	chkdskCmd := exec.Command("cmd", "/C", "chkdsk", checkTarget, "/f", "/x")
	_, _ = chkdskCmd.CombinedOutput()
	if _, statErr := os.Stat(checkTarget + "\\"); statErr == nil {
		fmt.Printf("✓ chkdsk repaired the volume. Format not required.\n")
		return nil
	}

	// Try PowerShell Format-Volume first (modern Windows).
	psCmd := fmt.Sprintf("Format-Volume -DriveLetter %c -FileSystem exFAT -NewFileSystemLabel %s -Confirm:$false -Force", driveLetter, formatLabel)
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", psCmd)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}

	// Fallback to classic format.exe quick mode.
	formatTarget := fmt.Sprintf("%c:", driveLetter)
	cmd = exec.Command("cmd", "/C", "format", formatTarget, "/FS:exFAT", "/Q", "/Y", "/V:"+formatLabel)
	out2, err2 := cmd.CombinedOutput()
	if err2 == nil {
		return nil
	}

	out1s := strings.TrimSpace(string(out))
	out2s := strings.TrimSpace(string(out2))
	if strings.Contains(strings.ToLower(out1s+" "+out2s), "0xc000009c") ||
		strings.Contains(strings.ToLower(out1s+" "+out2s), "track 0 bad") ||
		strings.Contains(strings.ToLower(out1s+" "+out2s), "invalid media") {
		return fmt.Errorf("media appears physically faulty (I/O at sector 0 failed). Replace the flash drive. Details: powershell=%v (%s); format=%v (%s)", err, out1s, err2, out2s)
	}

	return fmt.Errorf("powershell format error: %v (%s); format.exe error: %v (%s)", err, out1s, err2, out2s)
}

func probePrintDiskpartGuide(driveLetter rune) {
	fmt.Printf("\nDiskPart fallback (manual):\n")
	fmt.Printf("1) Open elevated terminal (Run as Administrator)\n")
	fmt.Printf("2) Run: diskpart\n")
	fmt.Printf("3) Run commands carefully:\n")
	fmt.Printf("   list volume\n")
	fmt.Printf("   select volume %c\n", driveLetter)
	fmt.Printf("   attributes volume clear readonly\n")
	fmt.Printf("   format fs=exfat quick label=FD_RECOV\n")
	fmt.Printf("   assign letter=%c\n", driveLetter)
	fmt.Printf("   exit\n")
	fmt.Printf("⚠ Verify the selected volume before formatting to avoid data loss on wrong disk.\n\n")
}

// isRunningAsAdmin checks if the current process has admin token.
func isRunningAsAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	token := windows.Token(0)
	member, err := token.IsMember(sid)
	return err == nil && member
}
