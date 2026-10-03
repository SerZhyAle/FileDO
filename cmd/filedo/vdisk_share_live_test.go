//go:build windows

package main

import (
	"errors"
	"filedo/fmsworker"
	"filedo/statedir"
	"filedo/vdisk"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// liveFixture is a registered container, shared in the local file, with the worker's answer under the
// test's control.
func liveFixture(t *testing.T) (path string, id string) {
	t.Helper()
	dir := vdTestEnv(t)
	path = filepath.Join(dir, "shared_test.fdd")
	vdTestContainer(t, path, 1<<20, vdisk.ProfilePlain, "")
	info, err := vdisk.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	sharedPath, err := statedir.Path("vdisk-shared.json")
	if err != nil {
		t.Fatal(err)
	}
	sm := vdisk.NewSharedDiskManager(sharedPath, nil)
	if err := sm.RegisterSharedDisk(info.ContainerID, path, "MySharedRoot", false); err != nil {
		t.Fatal(err)
	}
	old := vdShareProbe
	t.Cleanup(func() { vdShareProbe = old })
	return path, info.ContainerID
}

func sharedRowOf(t *testing.T, snap vdSnapshot, id string) vdSnapContainer {
	t.Helper()
	for _, item := range snap.Disks {
		if row, ok := item.(vdSnapContainer); ok && row.ContainerID == id {
			return row
		}
	}
	t.Fatalf("container %s is not in the snapshot", id)
	return vdSnapContainer{}
}

// AUD-82-F2 / AUD-86-F1 / AUD-87-F6: a disk the worker holds open is reported as held, open, with its
// handles - not as the constants the local file resets to.
func TestVD_StatusJSON_ReportsTheWorkersLiveHolder(t *testing.T) {
	path, id := liveFixture(t)
	vdShareProbe = func(time.Duration) vdShareLiveResult {
		return vdShareLiveResult{Mode: fmsworker.WorkerModeService, Disks: []fmsworker.SharedDiskInfo{
			{ContainerID: id, ContainerPath: path, RootName: "MySharedRoot", State: fmsworker.DiskStateOpen, Holder: "service", OpenHandles: 3, Autostart: true, HasStoredKey: true},
		}}
	}
	snap, err := vdBuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	row := sharedRowOf(t, snap, id)
	if !row.Shared || row.RootName != "MySharedRoot" || row.Holder != "fms-service" || row.SharedState != "open" || row.OpenHandles != 3 || !row.Autostart || !row.HasStoredKey {
		t.Fatalf("row = %+v", row)
	}
	if snap.Sharing != nil {
		t.Fatalf("a complete list carries no sharing block: %+v", snap.Sharing)
	}
}

// A worker that cannot be asked leaves the holder unknown: "none" would offer a Mount the lock refuses.
func TestVD_StatusJSON_UnreachableWorkerIsUnknownNotFree(t *testing.T) {
	_, id := liveFixture(t)
	vdShareProbe = func(time.Duration) vdShareLiveResult {
		return vdShareLiveResult{Err: errors.Join(fmsworker.ErrWorkerUnavailable)}
	}
	snap, err := vdBuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	row := sharedRowOf(t, snap, id)
	if !row.Shared || row.Holder != "unknown" || row.SharedState != "unknown" || row.OpenHandles != 0 {
		t.Fatalf("row = %+v", row)
	}
}

// A shared disk that FileDO itself has mounted is held by FileDO, not by nobody.
func TestVD_StatusJSON_FileDOHoldsAMountedSharedDisk(t *testing.T) {
	path, id := liveFixture(t)
	vdShareProbe = func(time.Duration) vdShareLiveResult {
		return vdShareLiveResult{Disks: []fmsworker.SharedDiskInfo{{ContainerID: id, ContainerPath: path, RootName: "MySharedRoot", State: fmsworker.DiskStateClosed, Holder: "none"}}}
	}
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = append(s.Mounts, vdMountRow{ContainerID: id, Path: path, Letter: "Q:", Profile: "plain", MountedAt: time.Now()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	old := vdSnapshotAlive
	vdSnapshotAlive = func(vdMountRow) bool { return true }
	t.Cleanup(func() { vdSnapshotAlive = old })
	snap, err := vdBuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if row := sharedRowOf(t, snap, id); row.Holder != "file-do" {
		t.Fatalf("holder = %q, want file-do", row.Holder)
	}
}

// An unrecognised or missing holder word, and an open disk with no holder named, are unknown - never free.
func TestVDHolderOf_NeverReadsUnknownAsFree(t *testing.T) {
	for _, tc := range []struct {
		holder string
		state  fmsworker.DiskState
		want   vdisk.DiskHolder
	}{
		{"service", fmsworker.DiskStateOpen, vdisk.HolderFMSService},
		{"session", fmsworker.DiskStateOpen, vdisk.HolderFMSSession},
		{"none", fmsworker.DiskStateClosed, vdisk.HolderNone},
		{"", fmsworker.DiskStateClosed, vdisk.HolderNone},
		{"none", fmsworker.DiskStateOpen, vdisk.HolderUnknown},
		{"none", fmsworker.DiskStateClosing, vdisk.HolderUnknown},
		{"somebody-new", fmsworker.DiskStateClosed, vdisk.HolderUnknown},
	} {
		if got := vdHolderOf(fmsworker.SharedDiskInfo{Holder: tc.holder, State: tc.state}); got != tc.want {
			t.Errorf("holder %q state %v -> %v, want %v", tc.holder, tc.state, got, tc.want)
		}
	}
	for h, want := range map[vdisk.DiskHolder]string{vdisk.HolderNone: "none", vdisk.HolderFileDO: "file-do", vdisk.HolderFMSService: "fms-service", vdisk.HolderFMSSession: "fms-session", vdisk.HolderUnknown: "unknown", vdisk.DiskHolder(99): "unknown"} {
		if got := h.Token(); got != want {
			t.Errorf("Token(%d) = %q, want %q", h, got, want)
		}
	}
}

// AUD-82-F3: a held lock on the local file must not make the snapshot wait seconds per call. The worker is
// down here, so the file is the only source, and its lock is bounded by vdShareSnapshotLock.
func TestVD_StatusJSON_HeldSharedLockDoesNotStallTheSnapshot(t *testing.T) {
	_, id := liveFixture(t)
	vdShareProbe = func(time.Duration) vdShareLiveResult { return vdShareLiveResult{Err: fmsworker.ErrWorkerUnavailable} }
	p, err := statedir.Path("vdisk-shared.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".lock", []byte("1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	snap, err := vdBuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("the snapshot took %s behind a held lock; the window gives up at 8 s", d)
	}
	if snap.Sharing == nil || snap.Sharing.Known {
		t.Fatalf("neither source could be read, so the snapshot must say the shared list is not known: %+v", snap.Sharing)
	}
	for _, item := range snap.Disks {
		if row, ok := item.(vdSnapContainer); ok && row.ContainerID == id && row.Shared {
			t.Fatalf("a row claims shared state it could not have read: %+v", row)
		}
	}
}

// DISK-SHARE 17 and 19: `vd mount` of a disk the worker holds is refused, naming the holder and the root.
func TestVD_MountRefusedWhileFMSHolds(t *testing.T) {
	path, id := liveFixture(t)
	info, err := vdisk.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, holder := range []string{"service", "session"} {
		vdShareProbe = func(time.Duration) vdShareLiveResult {
			return vdShareLiveResult{Disks: []fmsworker.SharedDiskInfo{{ContainerID: id, ContainerPath: path, RootName: "MySharedRoot", State: fmsworker.DiskStateOpen, Holder: holder}}}
		}
		e := vdMountHolderGuard(info, path)
		if e == nil || vdExitClass(e) != vdisk.ExitBusy {
			t.Fatalf("%s: err = %v, want a busy refusal", holder, e)
		}
		if !strings.Contains(e.Error(), "MySharedRoot") || !strings.Contains(e.Error(), "FMS for Windows") || !strings.Contains(e.Error(), "share off") {
			t.Fatalf("%s: the refusal does not name the holder, the root and the way out: %v", holder, e)
		}
	}
}

// A shared disk that is closed mounts, with a warning; an unreachable worker is not "free" but does not
// block either: the container lock alone decides.
func TestVD_MountGuard_ClosedSharedDiskWarnsAndUnreachableWorkerProceeds(t *testing.T) {
	path, id := liveFixture(t)
	info, err := vdisk.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	vdShareProbe = func(time.Duration) vdShareLiveResult {
		return vdShareLiveResult{Disks: []fmsworker.SharedDiskInfo{{ContainerID: id, ContainerPath: path, RootName: "MySharedRoot", State: fmsworker.DiskStateClosed, Holder: "none"}}}
	}
	out, _ := captureBoth(t, func() {
		if e := vdMountHolderGuard(info, path); e != nil {
			t.Errorf("a closed shared disk was refused: %v", e)
		}
	})
	if !strings.Contains(out, "FMS cannot open it") {
		t.Errorf("no warning that FMS cannot open the disk while FileDO has it: %q", out)
	}
	vdShareProbe = func(time.Duration) vdShareLiveResult { return vdShareLiveResult{Err: fmsworker.ErrWorkerUnavailable} }
	if e := vdMountHolderGuard(info, path); e != nil {
		t.Errorf("an unreachable worker blocked the mount: %v", e)
	}
	// In a build without the share surface the guard asks nobody.
	vdShareOn = false
	defer func() { vdShareOn = true }()
	called := false
	vdShareProbe = func(time.Duration) vdShareLiveResult { called = true; return vdShareLiveResult{} }
	if e := vdMountHolderGuard(info, path); e != nil || called {
		t.Errorf("the guard asked the worker with the surface off (called=%v, err=%v)", called, e)
	}
}
