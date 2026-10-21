package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"filedo/vdisk"
)

// Keep-alive and the stale-row cleanup: the watch loop's decisions behind its
// seams, the mount word, the logon task that carries it, and the mount that no
// longer refuses the row of a server that is gone.

// keepSim is a scripted environment: the container's row and its server, a
// mount that succeeds or fails on demand, and a clock that only the waits move.
type keepSim struct {
	row      *vdMountRow
	rowErr   error // row() fails with it while set: the state cannot be read
	alive    bool
	mounts   int
	mountErr []error // consumed one per mount call; nil entries and an empty list succeed
	waits    []time.Duration
	log      []string
	// onWait runs at each wait and may change the world; it returns false to
	// stop the watch.
	onWait func(s *keepSim, n int) bool
	stop   bool
	clock  time.Duration // moved by the waits only
}

var keepEpoch = time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)

func (s *keepSim) env() vdKeepEnv {
	return vdKeepEnv{
		now: func() time.Time { return keepEpoch.Add(s.clock) },
		row: func() (vdMountRow, bool, error) {
			if s.rowErr != nil {
				return vdMountRow{}, false, s.rowErr
			}
			if s.row == nil {
				return vdMountRow{}, false, nil
			}
			return *s.row, true, nil
		},
		alive: func(vdMountRow) bool { return s.alive },
		mount: func() error {
			s.mounts++
			var err error
			if len(s.mountErr) > 0 {
				err, s.mountErr = s.mountErr[0], s.mountErr[1:]
			}
			if err != nil {
				return err
			}
			s.row = &vdMountRow{Path: `C:\a.fdd`, Letter: "D:", ServerPID: 100 + s.mounts}
			s.alive = true
			return nil
		},
		stopped: func() bool { return s.stop },
		wait: func(d time.Duration) bool {
			s.waits = append(s.waits, d)
			s.clock += d
			if s.onWait != nil && !s.onWait(s, len(s.waits)) {
				return false
			}
			return true
		},
		logf: func(f string, a ...interface{}) { s.log = append(s.log, fmt.Sprintf(f, a...)) },
	}
}

// The first start mounts, then the watch idles while the server runs.
func TestVD_KeepLoop_MountsThenWatches(t *testing.T) {
	s := &keepSim{}
	s.onWait = func(s *keepSim, n int) bool { return n < 3 }
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 1 {
		t.Errorf("mounts = %d, want 1", s.mounts)
	}
}

// A dead server with its row left behind is a crash: the loop mounts again.
func TestVD_KeepLoop_RemountsAfterACrash(t *testing.T) {
	s := &keepSim{row: &vdMountRow{Path: `C:\a.fdd`, ServerPID: 1}, alive: true}
	s.onWait = func(s *keepSim, n int) bool {
		if n == 1 {
			s.alive = false // the server dies; the row stays
		}
		return n < 12
	}
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 1 {
		t.Errorf("mounts = %d, want 1 after one crash", s.mounts)
	}
	if !s.alive || s.row == nil || s.row.ServerPID != 101 {
		t.Errorf("the disk was not brought back: %+v alive=%v", s.row, s.alive)
	}
}

// A server that exits during an unmount loses its row a moment later: that is
// the owner's unmount, and the watch ends without mounting anything.
func TestVD_KeepLoop_AnUnmountEndsTheWatch(t *testing.T) {
	s := &keepSim{row: &vdMountRow{Path: `C:\a.fdd`, ServerPID: 1}, alive: true}
	s.onWait = func(s *keepSim, n int) bool {
		switch n {
		case 1:
			s.alive = false // unmount stops the server first..
		case 3:
			s.row = nil // ..and removes the row a little later
		}
		return n < 20
	}
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 0 {
		t.Errorf("the watch mounted a disk the owner unmounted (%d mounts)", s.mounts)
	}
}

// An unmount of a live disk removes the row with the server still seen as
// alive for a poll or two: the missing row after a healthy watch ends it.
func TestVD_KeepLoop_ARowThatVanishesEndsTheWatch(t *testing.T) {
	s := &keepSim{row: &vdMountRow{Path: `C:\a.fdd`}, alive: true}
	s.onWait = func(s *keepSim, n int) bool {
		if n == 2 {
			s.row, s.alive = nil, false
		}
		return n < 20
	}
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 0 {
		t.Errorf("the watch mounted again after an unmount (%d mounts)", s.mounts)
	}
}

// A failed remount after a crash leaves no row; that must not read as an
// unmount. The loop retries with a growing pause that stops at the cap.
func TestVD_KeepLoop_RetriesWithBackoff(t *testing.T) {
	busy := errBusy("the file is open elsewhere")
	s := &keepSim{
		row: &vdMountRow{Path: `C:\a.fdd`, ServerPID: 1}, alive: false,
		mountErr: []error{busy, busy, busy, busy, busy, busy, busy, busy, busy, busy, nil},
	}
	// The first mount heals the row away, like vdClearStaleMount does.
	healed := false
	env := s.env()
	inner := env.mount
	env.mount = func() error {
		if !healed {
			healed, s.row = true, nil
		}
		return inner()
	}
	s.onWait = func(s *keepSim, n int) bool { return n < 40 }
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 11 || !s.alive {
		t.Errorf("mounts = %d alive = %v, want the 11th to succeed", s.mounts, s.alive)
	}
	var backoffs []time.Duration
	for _, w := range s.waits { // the one-second waits are the settle check; the first ten others are the pauses
		if w != time.Second && len(backoffs) < 10 {
			backoffs = append(backoffs, w)
		}
	}
	if len(backoffs) < 10 || backoffs[0] != vdKeepBackoffMin || backoffs[1] != 2*vdKeepBackoffMin {
		t.Errorf("the pauses do not grow from %s: %v", vdKeepBackoffMin, backoffs)
	}
	for _, b := range backoffs {
		if b > vdKeepBackoffMax {
			t.Errorf("a pause of %s passes the cap %s", b, vdKeepBackoffMax)
		}
	}
}

// A damaged file or a credential nobody can give is no use retrying.
func TestVD_KeepLoop_APermanentFailureEndsIt(t *testing.T) {
	for _, bad := range []error{vdisk.ErrDamaged, vdisk.ErrCredential, vdisk.ErrUnsupported} {
		s := &keepSim{mountErr: []error{fmt.Errorf("%w: x", bad)}}
		err := vdKeepLoop(s.env())
		if !errors.Is(err, bad) {
			t.Errorf("%v: loop returned %v", bad, err)
		}
		if s.mounts != 1 {
			t.Errorf("%v: %d mounts, want 1", bad, s.mounts)
		}
	}
	if vdKeepPermanent(errBusy("x")) || vdKeepPermanent(errTransport("x")) {
		t.Error("a busy file or an initiator that is down is waited out, not fatal")
	}
}

// The stop file (auto off) and an interrupt end the watch at once.
func TestVD_KeepLoop_StopEndsIt(t *testing.T) {
	s := &keepSim{stop: true}
	if err := vdKeepLoop(s.env()); err != nil || s.mounts != 0 {
		t.Errorf("err = %v mounts = %d, want a quiet end", err, s.mounts)
	}
}

func TestVD_KeepWord(t *testing.T) {
	o, err := vdParseMountOpts([]string{"a.fdd", "keep"})
	if err != nil || !o.Keep || o.Cred.given() {
		t.Fatalf("keep alone: %+v, %v", o, err)
	}
	if o, err = vdParseMountOpts([]string{"a.fdd", "ro", "keep", "as", "X:"}); err != nil || !o.Keep || !o.ReadOnly || o.Letter != "X:" {
		t.Fatalf("keep beside other words: %+v, %v", o, err)
	}
	if !vdMountOptionWord("keep") || !vdMountWordSet("KEEP") {
		t.Error("keep is not an option word of the bare-token and redaction rules")
	}
}

// The logon task carries the keep word and has no time limit; a copy made
// before keep existed is replaced by switching auto off and on.
func TestVD_AutoTaskKeeps(t *testing.T) {
	x := vdTaskXML("work", `C:\a\work.fdd`, `C:\Program Files\FileDO\filedo.exe`, "S-1-5-21-1-2-3-1001")
	for _, want := range []string{"--no-history vd mount work keep", "<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>"} {
		if !strings.Contains(x, want) {
			t.Errorf("the mount task XML lacks %q", want)
		}
	}
	if strings.Contains(x, "PT10M") {
		t.Error("the resident task still has a ten minute limit")
	}
}

// A row whose server runs is a real mount; one whose server is gone is cleared
// like an unmount would, and the mount goes on.
func TestVD_ClearStaleMount(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const id = "9b28e437-f594-4b27-9f10-7bb50a7bad81"
	started, ok := vdProcessStart(os.Getpid())
	if !ok {
		t.Fatal("cannot read this process's start time")
	}
	put := func(row vdMountRow) {
		t.Helper()
		if err := vdUpdateState(func(s *vdState) error { s.Mounts = []vdMountRow{row}; return nil }); err != nil {
			t.Fatal(err)
		}
	}
	var cleared []vdMountRow
	old := vdUnmountOneFn
	defer func() { vdUnmountOneFn = old }()
	vdUnmountOneFn = func(row vdMountRow, force, nosave, batch bool) (bool, string, vdResult, error) {
		cleared = append(cleared, row)
		err := vdUpdateState(func(s *vdState) error { s.Mounts = nil; return nil })
		return false, "unclean", vdResult{}, err
	}

	if err := vdClearStaleMount(id, `C:\a.fdd`, true); err != nil {
		t.Fatalf("no row: %v", err)
	}

	put(vdMountRow{ContainerID: id, Path: `C:\a.fdd`, Letter: "D:", ServerPID: os.Getpid(), ServerStarted: started})
	err := vdClearStaleMount(id, `C:\a.fdd`, true)
	if !errors.Is(err, vdisk.ErrBusy) || !strings.Contains(err.Error(), "already mounted") || len(cleared) != 0 {
		t.Fatalf("a live server: err = %v, cleared = %d; want busy and nothing cleared", err, len(cleared))
	}

	put(vdMountRow{ContainerID: id, Path: `C:\a.fdd`, Letter: "D:", ServerPID: os.Getpid(), ServerStarted: started + 1})
	if err := vdClearStaleMount(id, `C:\a.fdd`, true); err != nil {
		t.Fatalf("a gone server: %v", err)
	}
	if len(cleared) != 1 {
		t.Fatalf("the stale row was not cleared (%d)", len(cleared))
	}
	if _, still := vdFindMount(id); still {
		t.Error("the row is still in the state after the cleanup")
	}

	// A cleanup that fails keeps the old advice and the busy class.
	put(vdMountRow{ContainerID: id, Path: `C:\a.fdd`, Letter: "D:", ServerPID: os.Getpid(), ServerStarted: started + 1})
	vdUnmountOneFn = func(vdMountRow, bool, bool, bool) (bool, string, vdResult, error) {
		return false, "", vdResult{}, errors.New("consent declined")
	}
	err = vdClearStaleMount(id, `C:\a.fdd`, true)
	if !errors.Is(err, vdisk.ErrBusy) || !strings.Contains(err.Error(), "unmount") {
		t.Errorf("failed cleanup: %v", err)
	}
}

// auto off writes the stop file of the container's watcher, and the watcher
// removes a stale one at its start.
func TestVD_KeepRequestStop(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const id = "9b28e437-f594-4b27-9f10-7bb50a7bad81"
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		r.Containers = append(r.Containers, vdRegEntry{Name: "work", Path: `C:\a.fdd`, ContainerID: id})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	vdKeepRequestStop("work")
	p, _ := vdKeepStopPath(id)
	if _, err := os.Stat(p); err != nil {
		t.Errorf("no stop file after a stop request: %v", err)
	}
	vdKeepRequestStop("nobody") // an unknown name is a no-op
}
