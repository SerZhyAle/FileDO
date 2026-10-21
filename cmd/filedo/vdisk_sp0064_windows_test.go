package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filedo/vdisk"

	"golang.org/x/sys/windows"
)

// SP-0064 final pre-release audit: the virtual-disk fixes that can be proved
// without elevation or a real mount.

// vdFakeServer starts a stand-in for the block server that exits with code
// after about a second, and returns the mount row that names it.
func vdFakeServer(t *testing.T, code string) vdMountRow {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/c", "ping -n 2 127.0.0.1 >nul & exit "+code)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the stand-in server: %v", err)
	}
	t.Cleanup(func() { cmd.Wait() })
	started, alive := vdProcessStart(cmd.Process.Pid)
	if !alive {
		t.Fatal("the stand-in server is not running")
	}
	return vdMountRow{ContainerID: "0123456789abcdef0123456789abcdef", Path: `C:\scratch\ram.fdd`, Letter: "R:",
		ServerPID: cmd.Process.Pid, ServerStarted: started, Profile: vdisk.ProfileRAM.String()}
}

// T3-F1: a block server whose final save or commit failed exits non-zero; the
// unmount must learn that and fail with class 5, never report a clean close.
func TestVD_UnmountLearnsAFailedServerClose(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	err := vdStopServer(vdFakeServer(t, "5"), "clean")
	if err == nil {
		t.Fatal("a server that exited 5 (its final save failed) was taken for a clean close")
	}
	if c := vdExitClass(err); c != vdisk.ExitIO {
		t.Errorf("class %d, want %d: %v", c, vdisk.ExitIO, err)
	}
	if !strings.Contains(err.Error(), "not saved") || !strings.Contains(err.Error(), `C:\scratch\ram.fdd`) {
		t.Errorf("the error does not name what was not saved: %v", err)
	}
	if err := vdStopServer(vdFakeServer(t, "0"), "clean"); err != nil {
		t.Errorf("a server that closed cleanly is reported as failed: %v", err)
	}
}

// vdScratchContainer creates a small plain container; unclean leaves it the
// way a killed mount does (the marker clear).
func vdScratchContainer(t *testing.T, unclean bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "c.fdd")
	c, err := vdisk.Create(context.Background(), vdisk.CreateOptions{Path: path, LogicalSize: 2 << 20, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteAt(bytes.Repeat([]byte{7}, 4096), 0); err != nil {
		t.Fatal(err)
	}
	if unclean {
		err = c.Abandon()
	} else {
		err = c.Close()
	}
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// AUD-36-F1 (2): an offline writer verb names a container that was not closed
// cleanly before it writes, and asks; a batch never answers, so it refuses.
func TestVD_GrowAndCompactNameAnUncleanContainer(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	for _, verb := range []string{"grow", "compact"} {
		path := vdScratchContainer(t, true)
		var err error
		out := captureStdout(t, func() {
			if verb == "grow" {
				err = vdGrow([]string{path, "4M"}, true)
			} else {
				err = vdCompact([]string{path}, true)
			}
		})
		if vdExitClass(err) != vdisk.ExitUsage {
			t.Fatalf("%s on a container not closed cleanly: class %d, %v\n%s", verb, vdExitClass(err), err, out)
		}
		if !strings.Contains(out+err.Error(), "not closed cleanly") {
			t.Errorf("%s did not name the unclean container: %v\n%s", verb, err, out)
		}
		if i, ierr := vdisk.Inspect(path); ierr != nil || i.LogicalSize != 2<<20 {
			t.Errorf("%s wrote anyway: %+v, %v", verb, i, ierr)
		}
	}
	// A clean container grows in a batch as before.
	path := vdScratchContainer(t, false)
	captureStdout(t, func() {
		if err := vdGrow([]string{path, "4M"}, true); err != nil {
			t.Errorf("grow of a clean container: %v", err)
		}
	})
}

// AUD-36-F1 (2): an interrupted ram save is named and asked about, as mount
// does; a batch refuses.
func TestVD_OfflineWriterRefusesAnInterruptedSaveInABatch(t *testing.T) {
	info := vdisk.Info{Profile: vdisk.ProfileRAM, SaveInProgress: true, SaveStarted: time.Now(), LastGoodSave: time.Now().Add(-time.Hour)}
	var err error
	out := captureStdout(t, func() { err = vdOfflineWriterGate(`C:\scratch\r.fdd`, info, "grow", true) })
	if vdExitClass(err) != vdisk.ExitUsage || !strings.Contains(out, "a save of this ram container was interrupted") {
		t.Fatalf("an interrupted save: class %d, %v\n%s", vdExitClass(err), err, out)
	}
}

// AUD-36-F1 (2), the window's side: a Disks page that runs grow or compact in a
// batch gets class 2, and tells that refusal from any other class 2 by these
// words. The CLI's sentence carries them for both cases - an unclean container
// and an interrupted save - and the window's copy is the same text.
func TestVD_OfflineWriterRefusalCarriesTheWindowsMarker(t *testing.T) {
	for name, info := range map[string]vdisk.Info{
		"unclean":     {Profile: vdisk.ProfilePlain, Clean: false, LastGoodSave: time.Now()},
		"interrupted": {Profile: vdisk.ProfileRAM, SaveInProgress: true, SaveStarted: time.Now(), LastGoodSave: time.Now()},
	} {
		var err error
		captureStdout(t, func() { err = vdOfflineWriterGate(`C:\scratch\r.fdd`, info, "compact", true) })
		if err == nil || !strings.Contains(err.Error(), vdOfflineWriterRefusal) {
			t.Errorf("%s: the refusal does not carry %q: %v", name, vdOfflineWriterRefusal, err)
		}
	}
	src, err := os.ReadFile(`..\..\filedo_win_vb\DiskJobs.vb`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `"`+vdOfflineWriterRefusal+`"`) {
		t.Errorf("filedo_win_vb\\DiskJobs.vb does not look for %q: the window would show the generic class 2 sentence", vdOfflineWriterRefusal)
	}
}

// AUD-36-F1 (3): info and verify say that a save was interrupted, with when
// it began and the last complete save.
func TestVD_InfoAndVerifySayASaveWasInterrupted(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	path := vdScratchContainer(t, false)
	info, err := vdisk.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	info.Clean, info.SaveInProgress, info.SaveStarted = false, true, time.Now()
	out := captureStdout(t, func() { vdInfoPrint(path, info) })
	if !strings.Contains(out, "a save was interrupted") || !strings.Contains(out, vdTime(info.SaveStarted)) {
		t.Errorf("info does not say a save was interrupted:\n%s", out)
	}
	out = captureStdout(t, func() { vdVerifyOne(path, info, nil, credArg{}, false) })
	if !strings.Contains(out, "a save was interrupted") || !strings.Contains(out, vdTime(info.SaveStarted)) {
		t.Errorf("verify does not say a save was interrupted:\n%s", out)
	}
}

// AUD-32-F8: a serial that is not FDD + 20 hex digits (what vdisk.DiskSerial
// makes) selects no disk, and at once - it is not waited for.
func TestFindDiskBySerial_RefusesAMalformedSerial(t *testing.T) {
	for _, s := range []string{"WD-WX12345678", "FDD0123", "FDD" + strings.Repeat("G", 20), "FDD" + strings.Repeat("A", 21), " FDD" + strings.Repeat("A", 20)} {
		start := time.Now()
		disk, err := findDiskBySerial(s, 3*time.Second)
		if err == nil || disk != -1 {
			t.Errorf("serial %q selected disk %d: %v", s, disk, err)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("serial %q was waited for (%s) instead of refused", s, d)
		}
	}
	good := vdisk.DiskSerial("0123456789abcdef0123456789abcdef")
	if !vdSerialSpelling.MatchString(good) {
		t.Error("a serial vdisk.DiskSerial made is refused")
	}
	// A well-formed serial still proves nothing about PhysicalDrive0.
	if err := vdCheckContainerDisk(0, good); err == nil {
		t.Error("PhysicalDrive0 passed as a FileDO iSCSI disk")
	}
}

// AUD-32-F8: attach and detach refuse an empty or malformed serial as usage,
// before any initiator call or disk access. (Not run against the pre-fix code:
// there the same request reaches the initiator and the portal list.)
func TestVdAttachDetach_RefuseAMalformedSerialFirst(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	for _, s := range []string{"", "WD-WX12345678"} {
		if _, err := vdDetach(vdRequest{Serial: s, IQN: "iqn.test"}); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("detach with serial %q: class %d, %v", s, vdExitClass(err), err)
		}
		if _, err := vdAttach(vdRequest{Serial: s, IQN: "iqn.test"}, filepath.Join(t.TempDir(), "cancel")); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("attach with serial %q: class %d, %v", s, vdExitClass(err), err)
		}
	}
}

// AUD-31-F2: "formatted" is claimed only when the volume reads back as the
// file system asked for (GetVolumeInformation on its GUID path).
func TestVdConfirmFileSystem_ReadsTheVolume(t *testing.T) {
	root := os.Getenv("SystemDrive") + `\`
	buf := make([]uint16, 64)
	if err := windows.GetVolumeNameForVolumeMountPoint(windows.StringToUTF16Ptr(root), &buf[0], uint32(len(buf))); err != nil {
		t.Skipf("no GUID path for %s: %v", root, err)
	}
	vol := windows.UTF16ToString(buf)
	name := make([]uint16, 64)
	if err := windows.GetVolumeInformation(windows.StringToUTF16Ptr(vol), nil, 0, nil, nil, nil, &name[0], uint32(len(name))); err != nil {
		t.Skipf("GetVolumeInformation(%s): %v", vol, err)
	}
	have := windows.UTF16ToString(name)
	other := "exFAT"
	if strings.EqualFold(have, other) {
		other = "NTFS"
	}
	if err := vdConfirmFileSystem(vol, other); err == nil {
		t.Errorf("a %s volume was confirmed as %s", have, other)
	}
	if err := vdConfirmFileSystem(vol, have); err != nil {
		t.Errorf("a %s volume was not confirmed as %s: %v", have, have, err)
	}
	if err := vdConfirmFileSystem(`\\?\Volume{00000000-0000-0000-0000-000000000000}\`, have); err == nil {
		t.Error("a volume that cannot be read was confirmed")
	}
}

// AUD-34-F2: a stopped PowerShell step is vdisk.ErrStopped (the stop class),
// not an I/O failure.
func TestVdPowerShellCtx_AStopIsTheStopClass(t *testing.T) {
	psAvailable(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	_, err := vdPowerShellCtx(ctx, "Start-Sleep -Seconds 30", time.Minute)
	if !errors.Is(err, vdisk.ErrStopped) || vdExitClass(err) != vdisk.ExitStopped {
		t.Fatalf("a stopped run: class %d, %v; want vdisk.ErrStopped", vdExitClass(err), err)
	}
	if !errors.Is(err, errRunStopped) {
		t.Errorf("a stopped run no longer carries errRunStopped: %v", err)
	}
}

// AUD-34-F2: an image row whose letter is not X: (an old build recorded a
// failed mount's error text as the letter) is shown by neither vd status nor
// the snapshot.
func TestVD_StatusIgnoresAnImageRowWithoutALetter(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const bad = "Mount-DiskImage : The file or directory is corrupted"
	if err := vdUpdateState(func(s *vdState) error {
		s.Images = []vdImageRow{{Letter: bad, Path: `C:\scratch\bad.iso`}, {Letter: "E:", Path: `C:\scratch\good.iso`}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() {
		if err := vdStatus(); err != nil {
			t.Error(err)
		}
	})
	if strings.Contains(out, "bad.iso") || !strings.Contains(out, "good.iso") {
		t.Errorf("vd status shows a row without a letter, or hides the good one:\n%s", out)
	}
	s, err := vdBuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(s)
	if strings.Contains(string(b), "bad.iso") || !strings.Contains(string(b), "good.iso") {
		t.Errorf("the snapshot shows a row without a letter, or hides the good one: %s", b)
	}
}

// AUD-35-F5 remainder: vd status says that a mounted ram disk's saving is
// failing, from the server's .status.json.
func TestVD_StatusSaysSavingIsFailing(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	started, _ := vdProcessStart(os.Getpid())
	row := vdMountRow{ContainerID: "0123456789abcdef0123456789abcdef", Path: `C:\scratch\ram.fdd`, Letter: "R:",
		ServerPID: os.Getpid(), ServerStarted: started, Profile: vdisk.ProfileRAM.String()}
	if err := vdUpdateState(func(s *vdState) error { s.Mounts = []vdMountRow{row}; return nil }); err != nil {
		t.Fatal(err)
	}
	p, _ := vdSidePath(row.ContainerID, ".status.json")
	if err := os.WriteFile(p, []byte(`{"at":"2026-10-01T00:00:00Z","dirty_bytes":65536,"saving":false,"last_good_save":"2026-10-01T00:00:00Z","save_error":"vdisk: I/O error: the device is not ready"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { vdStatus() })
	if !strings.Contains(out, "the device is not ready") {
		t.Errorf("vd status does not say that saving is failing:\n%s", out)
	}
}

// SP-0064 R-F2: a server that exited - here with its final save failed - holds
// the container no more, so the unmount drops its mount row and still returns
// the error; only a server that may still run keeps the row.
func TestVD_AFailedServerCloseDropsTheMountRow(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	err := vdStopServer(vdFakeServer(t, "5"), "clean")
	if err == nil {
		t.Fatal("a failed close was not reported")
	}
	if vdStopKeepsMountRow(err) {
		t.Errorf("a server that exited keeps its mount row: %v", err)
	}
	if !vdStopKeepsMountRow(vdServerRunningError{errors.New("did not exit")}) {
		t.Error("a server that may still run lost its mount row")
	}
}
