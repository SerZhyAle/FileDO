//go:build windows

package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unsafe"

	"golang.org/x/sys/windows"
)

// runDeviceProbe performs a fast fake-capacity probe by writing unique markers
// directly to raw LBA positions via \\.\D: and reading them back.
// Requires administrator privileges.
//
// Algorithm:
//  1. Open \\.\D: for read+write (requires admin)
//  2. Distribute N probe points evenly across 95% of claimed capacity
//  3. Write a unique 512-byte marker at each point
//  4. Read all markers back and check for mismatches
//
// On a fake device the controller wraps addresses, so later writes overwrite
// earlier ones — caught when we verify.
func runDeviceProbe(devicePath string) error {
	// Normalise: accept "D", "D:", "D:\"
	driveLetter := rune(0)
	for _, r := range devicePath {
		if unicode.IsLetter(r) {
			driveLetter = unicode.ToUpper(r)
			break
		}
	}
	if driveLetter == 0 {
		return fmt.Errorf("probe requires a drive letter (e.g. D:), got: %s", devicePath)
	}

	rawPath := fmt.Sprintf(`\\.\%c:`, driveLetter)

	fmt.Printf("Device Probe (fast fake-capacity detection)\n")
	fmt.Printf("Target : %s  (raw: %s)\n", getEnhancedDeviceInfo(fmt.Sprintf(`%c:`, driveLetter)), rawPath)
	fmt.Printf("Mode   : Administrator raw I/O — no files written to filesystem\n\n")

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
			return fmt.Errorf("access denied — run as Administrator to use probe\n"+
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

	// ── Get disk geometry to find total sectors ──────────────────────────────
	totalBytes, sectorSize, err := probeGetDiskSize(handle)
	if err != nil {
		return fmt.Errorf("cannot read disk geometry: %w", err)
	}
	if sectorSize < 512 {
		sectorSize = 512
	}

	fmt.Printf("Disk size    : %.2f GB\n", float64(totalBytes)/(1<<30))
	fmt.Printf("Sector size  : %d bytes\n\n", sectorSize)

	// ── Distribute probe points ──────────────────────────────────────────────
	const numProbes = 32
	// Never touch the first sectors (partition boot/metadata area).
	// Probe 5%..95% range to reduce risk of filesystem damage.
	const probeStartPercent = 0.05
	const probeEndPercent = 0.95
	startOffset := int64(float64(totalBytes) * probeStartPercent)
	endOffset := int64(float64(totalBytes) * probeEndPercent)

	// Align each offset to sector boundary.
	align := int64(sectorSize)
	if startOffset < align*2048 {
		startOffset = align * 2048 // 1MB minimum from start
	}
	if endOffset <= startOffset {
		endOffset = startOffset + align*int64(numProbes+1)
	}
	step := float64(endOffset-startOffset) / float64(numProbes-1)

	offsets := make([]int64, numProbes)
	for i := 0; i < numProbes; i++ {
		raw := startOffset + int64(float64(i)*step)
		offsets[i] = (raw / align) * align
	}

	// ── Generate unique markers ──────────────────────────────────────────────
	// Each marker fits in one sector. Format (first 64 bytes):
	//   "FILEDO_PROBE <index> <hex-token>\n"
	// followed by zeros to sector size.
	tokens := make([]string, numProbes)
	for i := range tokens {
		buf := make([]byte, 8)
		rand.Read(buf)
		tokens[i] = hex.EncodeToString(buf)
	}

	bufSize := int(sectorSize)
	// Sector-aligned buffers (required for FILE_FLAG_NO_BUFFERING)
	writeBuf := probeMakeAlignedBuf(bufSize)
	readBuf := probeMakeAlignedBuf(bufSize)

	// ── Save original sector content ─────────────────────────────────────────
	// We restore everything after the test so the filesystem is not damaged.
	fmt.Printf("Saving original content of %d sectors...\n", numProbes)
	originals := make([][]byte, numProbes)
	for i, offset := range offsets {
		orig := probeMakeAlignedBuf(bufSize)
		if err := probeReadSector(handle, offset, orig); err != nil {
			// Non-fatal: sector might be unreadable on a damaged device
			fmt.Printf("  ⚠ Cannot read sector at %.2f MB: %v (will skip restore)\n",
				float64(offset)/(1<<20), err)
		}
		originals[i] = orig
	}
	fmt.Printf("✓ Originals saved\n\n")

	// Ensure we restore sectors even if we return early due to error
	restored := false
	defer func() {
		if !restored {
			probeRestoreOriginals(handle, offsets, originals)
		}
	}()

	fmt.Printf("Writing %d probe markers across %.2f..%.2f GB...\n",
		numProbes, float64(startOffset)/(1<<30), float64(endOffset)/(1<<30))
	start := time.Now()

	for i, offset := range offsets {
		// Build sector-aligned marker
		for j := range writeBuf {
			writeBuf[j] = 0
		}
		marker := fmt.Sprintf("FILEDO_PROBE %02d %s\n", i, tokens[i])
		copy(writeBuf, marker)

		if err := probeWriteSector(handle, offset, writeBuf); err != nil {
			probeRestoreOriginals(handle, offsets, originals)
			restored = true
			return fmt.Errorf("write failed at offset %.2f MB (probe %d): %w",
				float64(offset)/(1<<20), i, err)
		}
		fmt.Printf("  Written probe %2d/%d at offset %10.2f MB\r",
			i+1, numProbes, float64(offset)/(1<<20))
	}
	fmt.Printf("\n✓ All %d probes written in %s\n\n", numProbes, time.Since(start).Round(time.Millisecond))

	// ── Read back and verify ─────────────────────────────────────────────────
	fmt.Printf("Reading back %d probe markers...\n", numProbes)
	mismatch := 0
	firstBad := -1

	for i, offset := range offsets {
		if err := probeReadSector(handle, offset, readBuf); err != nil {
			fmt.Printf("  ❌ Probe %2d: read error at offset %.2f MB: %v\n",
				i, float64(offset)/(1<<20), err)
			mismatch++
			if firstBad < 0 {
				firstBad = i
			}
			continue
		}

		expected := fmt.Sprintf("FILEDO_PROBE %02d %s\n", i, tokens[i])
		got := string(readBuf[:len(expected)])
		if got != expected {
			mismatch++
			if firstBad < 0 {
				firstBad = i
			}
			// Try to decode what we actually found
			line := ""
			for j, b := range readBuf {
				if b == '\n' || j >= 60 {
					break
				}
				line += string(rune(b))
			}
			fmt.Printf("  ❌ Probe %2d at %.2f MB: expected token %s, found: %q\n",
				i, float64(offset)/(1<<20), tokens[i], line)
		}
	}

	// ── Restore original sectors ─────────────────────────────────────────────
	fmt.Printf("\nRestoring original sector content...\n")
	probeRestoreOriginals(handle, offsets, originals)
	restored = true
	fmt.Printf("✓ All sectors restored\n\n")

	fmt.Println()
	if mismatch == 0 {
		fmt.Printf("✅ GENUINE: All %d probes verified — no fake capacity detected.\n", numProbes)
		fmt.Printf("   Checked range: %.2f → %.2f GB\n", float64(startOffset)/(1<<30), float64(endOffset)/(1<<30))
	} else {
		// Estimate real capacity: last good probe before first bad one
		realBytes := int64(0)
		if firstBad > 0 {
			realBytes = offsets[firstBad-1]
		}
		fmt.Printf("⚠️  FAKE CAPACITY DETECTED\n")
		fmt.Printf("   Probes failed  : %d / %d\n", mismatch, numProbes)
		if realBytes > 0 {
			fmt.Printf("   Estimated real capacity: %.2f GB\n", float64(realBytes)/(1<<30))
		} else {
			fmt.Printf("   Fake starts at the very first sector — actual capacity near zero.\n")
		}
		fmt.Printf("   Claimed size   : %.2f GB\n", float64(totalBytes)/(1<<30))
	}

	return nil
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

func probeRestoreOriginals(h windows.Handle, offsets []int64, originals [][]byte) {
	for i, offset := range offsets {
		if originals[i] != nil {
			probeWriteSector(h, offset, originals[i])
		}
	}
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

func probeExtractDriveLetter(devicePath string) (rune, error) {
	for _, r := range devicePath {
		if unicode.IsLetter(r) {
			return unicode.ToUpper(r), nil
		}
	}
	return 0, fmt.Errorf("probe requires a drive letter (e.g. D:), got: %s", devicePath)
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
