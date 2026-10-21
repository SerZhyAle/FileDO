package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"filedo/vdisk"
)

// AUD-35-F5: a ram mount whose saving fails says so before the unmount - in the
// block server's .status.json (the writer) and in the `filedo.vd-status`
// snapshot the Disk Manager reads (`mount.ram.save_error`, an optional field
// of version 1). Absent while saves succeed, and an old server's status file
// without the field reads as healthy.

func TestVD_StatusFileCarriesTheSaveError(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	b, _ := json.Marshal(vdRAMStatus{At: at, DirtyBytes: 1 << 20, LastGoodSave: at, SaveError: "vdisk: I/O error: the device is not ready"})
	if !strings.Contains(string(b), `"save_error":"vdisk: I/O error: the device is not ready"`) {
		t.Errorf("a failing save is not in the status file: %s", b)
	}
	b, _ = json.Marshal(vdRAMStatus{At: at, DirtyBytes: 1 << 20, LastGoodSave: at})
	if strings.Contains(string(b), "save_error") {
		t.Errorf("a healthy status file carries save_error: %s", b)
	}
}

func TestVD_SnapshotCarriesTheSaveError(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	tasks, transport, alive, guard := vdSnapshotTasks, vdSnapshotTransport, vdSnapshotAlive, vdSnapshotGuard
	t.Cleanup(func() {
		vdSnapshotTasks, vdSnapshotTransport, vdSnapshotAlive, vdSnapshotGuard = tasks, transport, alive, guard
	})
	vdSnapshotTasks = func() map[string]bool { return nil }
	vdSnapshotTransport = func() vdSnapTransport { return vdSnapTransport{Ready: true, InitiatorService: "running"} }
	vdSnapshotAlive = func(vdMountRow) bool { return true }
	vdSnapshotGuard = func() vdSnapGuard { return vdSnapGuard{} }

	row := vdMountRow{ContainerID: "0123456789abcdef0123456789abcdef", Path: `C:\scratch\ram.fdd`, Letter: "R:",
		ServerPID: 4242, Profile: vdisk.ProfileRAM.String(), MountedAt: time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)}
	if err := vdUpdateState(func(s *vdState) error { s.Mounts = []vdMountRow{row}; return nil }); err != nil {
		t.Fatal(err)
	}
	status, err := vdSidePath(row.ContainerID, ".status.json")
	if err != nil {
		t.Fatal(err)
	}

	ramOf := func() map[string]interface{} {
		t.Helper()
		s, err := vdBuildSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		if s.Version != 1 {
			t.Fatalf("snapshot version %d, want 1: an optional field is not a new major", s.Version)
		}
		b, _ := json.Marshal(s)
		var doc struct {
			Disks []struct {
				Mount *struct {
					RAM map[string]interface{} `json:"ram"`
				} `json:"mount"`
			} `json:"disks"`
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Disks) != 1 || doc.Disks[0].Mount == nil || doc.Disks[0].Mount.RAM == nil {
			t.Fatalf("no ram mount in the snapshot: %s", b)
		}
		return doc.Disks[0].Mount.RAM
	}

	// A failing save: the snapshot carries the server's error.
	if err := os.WriteFile(status, []byte(`{"at":"2026-10-01T09:00:00Z","dirty_bytes":1048576,"saving":false,"last_good_save":"2026-10-01T08:00:00Z","save_error":"vdisk: I/O error: the device is not ready"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ramOf()["save_error"]; got != "vdisk: I/O error: the device is not ready" {
		t.Errorf("snapshot save_error = %v, want the server's error", got)
	}

	// An old server's status file (no field) and a healthy one: no save_error at all.
	if err := os.WriteFile(status, []byte(`{"at":"2026-10-01T09:00:00Z","dirty_bytes":1048576,"saving":false,"last_good_save":"2026-10-01T08:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ram := ramOf()
	if _, ok := ram["save_error"]; ok {
		t.Errorf("a healthy ram mount carries save_error: %v", ram)
	}
	if ram["dirty_bytes"] != float64(1<<20) {
		t.Errorf("an old status file was not read: %v", ram)
	}
}
