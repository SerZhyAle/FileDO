//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filedo/fmsworker"
	"filedo/statedir"
	"filedo/vdisk"
)

// The suites of this package run the FMS share surface itself, so it is on for every test. The refusals of a
// build that holds the surface back are tested with the switch put off.
func init() {
	vdShareOn = true
	// No test talks to a real FMS worker unless it says so: on a machine where Fast Media Sorter is running,
	// the real pipe would put its shared disks into every snapshot a test builds.
	vdShareProbe = func(time.Duration) vdShareLiveResult {
		return vdShareLiveResult{Err: fmsworker.ErrWorkerUnavailable}
	}
}

func vdShareSwitchedOff(t *testing.T) {
	t.Helper()
	old := vdShareOn
	vdShareOn = false
	t.Cleanup(func() { vdShareOn = old })
}

// TestVDShare_ShippedOn pins the owner's decision to ship the FMS share surface (SP-0121): the tickets that
// held it back are closed. A release that must hold the surface back again flips vdShareShipped and this
// test together, and the refusals below keep their tests either way.
func TestVDShare_ShippedOn(t *testing.T) {
	if !vdShareShipped {
		t.Fatal("vdShareShipped is false: the FMS share surface is not part of this build; change this test together with the constant")
	}
}
func TestVDShare_OffRefusesEveryVerb(t *testing.T) {
	vdShareSwitchedOff(t)
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "off.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	// Without an FMS worker every share verb fails with class 6 anyway; what the switch adds is that the
	// worker is never asked, so nothing is sent to the pipe and no credential is read.
	oldFactory := vdWorkerFactory
	t.Cleanup(func() { vdWorkerFactory = oldFactory })
	asked := false
	vdWorkerFactory = func() (vdWorker, error) { asked = true; return nil, errors.New("the worker must not be asked") }
	for _, args := range [][]string{
		{"share", p, "on"}, {"share", p, "off"}, {"autostart", p, "on"}, {"autostart", p, "off", "p:x"},
		{"open", p, "p:x"}, {"close", p},
	} {
		_, err := vdTestRun(t, true, args...)
		wantClass(t, err, vdisk.ExitUnsupported)
		if err != nil && !strings.Contains(err.Error(), "not available in this build") {
			t.Errorf("%v: refusal %q does not say the build lacks it", args, err)
		}
	}
	if asked {
		t.Error("a share verb asked the FMS worker with the surface off")
	}
}

func TestVDShare_OffRefusesTheMountWords(t *testing.T) {
	vdShareSwitchedOff(t)
	for _, w := range []string{"noletter", "worker", "stdin", "NOLETTER"} {
		_, err := vdParseMountOpts([]string{"x.fdd", w})
		wantClass(t, err, vdisk.ExitUnsupported)
		if err != nil && strings.Contains(strings.ToLower(err.Error()), strings.ToLower(w)) {
			t.Errorf("the refusal quotes the word %q back: %v", w, err)
		}
	}
	// The other mount words and a password are untouched.
	if _, err := vdParseMountOpts([]string{"x.fdd", "ro", "noscan", "p:secret"}); err != nil {
		t.Errorf("an ordinary mount line is refused: %v", err)
	}
}

// vdRefreshShareSnapshot is behind the verbs, but it is the one caller that used the manager without
// asking whether there is one; with the surface off it is refused, not a nil dereference.
func TestVDShare_OffRefreshIsRefusedNotAPanic(t *testing.T) {
	vdShareSwitchedOff(t)
	vdTestEnv(t)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("vdRefreshShareSnapshot panicked with the surface off: %v", r)
		}
	}()
	wantClass(t, vdRefreshShareSnapshot(&shareWorkerStub{}), vdisk.ExitUnsupported)
}

func TestVDShare_OffReadsNoSharedState(t *testing.T) {
	vdShareSwitchedOff(t)
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "off-status.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	info, err := vdisk.Inspect(p)
	if err != nil {
		t.Fatal(err)
	}
	sharedPath, err := statedir.Path("vdisk-shared.json")
	if err != nil {
		t.Fatal(err)
	}
	sm := vdisk.NewSharedDiskManager(sharedPath, nil)
	if err := sm.RegisterSharedDisk(info.ContainerID, p, "OffRoot", false); err != nil {
		t.Fatal(err)
	}
	if m, unl, err := vdShareSnapshotOpen(false); m != nil || err != nil {
		t.Fatalf("vdShareSnapshotOpen with the surface off = %v, %v; want nil, nil", m, err)
	} else if unl != nil {
		unl()
	}
	snap, err := vdBuildSnapshot()
	if err != nil {
		t.Fatalf("vdBuildSnapshot: %v", err)
	}
	for _, item := range snap.Disks {
		if row, ok := item.(vdSnapContainer); ok && (row.Shared || row.RootName != "") {
			t.Errorf("the snapshot carries shared-disk fields with the surface off: %+v", row)
		}
	}
	if _, statErr := os.Stat(sharedPath); statErr != nil {
		t.Errorf("the shared-state file is gone: %v", statErr)
	}
}
