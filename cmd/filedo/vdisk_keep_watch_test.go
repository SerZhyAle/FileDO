package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filedo/vdisk"
)

// The keep watcher's failure paths (SP-0064 re-audit: AUD-87-F1..F4, AUD-83-F4):
// the resolve stage, the stop request, the backoff, an unreadable state and the
// refusals a retry cannot fix. keepSim is in vdisk_keep_windows_test.go.

// keepNotThere is the refusal of a container on a drive that is not up yet.
func keepNotThere() error {
	return fmt.Errorf("%w: the system cannot find the path specified", vdisk.ErrIO)
}

// AUD-87-F1: the watcher reads the container before it can watch it, and that
// stage is part of the loop - it obeys a stop like every other stage.
func TestVD_KeepLoop_ResolvePhaseHonoursStop(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	resolves := 0
	env.resolve = func() error { resolves++; return keepNotThere() }
	s.onWait = func(s *keepSim, n int) bool {
		if n == 2 {
			s.stop = true // the owner's auto off, written while the drive is out
		}
		return n < 30
	}
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 0 {
		t.Errorf("DEFECT: %d mount(s) although the container was never read", s.mounts)
	}
	if resolves == 0 || len(s.waits) > 3 {
		t.Errorf("DEFECT: the stop was not honoured in the resolve stage: %d resolves, %d waits", resolves, len(s.waits))
	}
}

// Once the container is readable the watch goes on with the mount, and the
// stage that waited for it ran its ready step exactly once.
func TestVD_KeepLoop_ResolvePhaseThenWatches(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	resolves, readies := 0, 0
	env.resolve = func() error {
		if resolves++; resolves < 3 {
			return keepNotThere()
		}
		return nil
	}
	env.ready = func() bool { readies++; return true }
	s.onWait = func(s *keepSim, n int) bool { return n < 6 }
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	if resolves != 3 || readies != 1 || s.mounts != 1 {
		t.Errorf("resolves = %d readies = %d mounts = %d, want 3, 1, 1", resolves, readies, s.mounts)
	}
	if len(s.waits) < 2 || s.waits[0] != vdKeepBackoffMin || s.waits[1] != 2*vdKeepBackoffMin {
		t.Errorf("the waits for the drive do not grow from %s: %v", vdKeepBackoffMin, s.waits)
	}
}

// A second watcher of the same container (the lock is taken in the ready step)
// ends quietly and mounts nothing.
func TestVD_KeepLoop_AWatcherThatRunsAlreadyEnds(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	env.resolve = func() error { return nil }
	env.ready = func() bool { return false }
	s.onWait = func(s *keepSim, n int) bool { return n < 6 }
	if err := vdKeepLoop(env); err != nil || s.mounts != 0 {
		t.Errorf("err = %v mounts = %d, want a quiet end with no mount", err, s.mounts)
	}
}

// AUD-87-F2: a container that stays unreadable is retried with the growing
// pause of every other failure, capped, and noted when something changes - not
// every few seconds for the whole session.
func TestVD_KeepLoop_ResolvePhaseBacksOff(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	resolves := 0
	env.resolve = func() error { resolves++; return keepNotThere() }
	s.onWait = func(s *keepSim, n int) bool { return n < 14 }
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	if resolves != 14 {
		t.Fatalf("DEFECT: the container was read %d times in 14 passes (the resolve stage is not in the loop)", resolves)
	}
	want := vdKeepBackoffMin
	for i, w := range s.waits {
		if w != want {
			t.Errorf("pause %d is %s, want %s (5 s doubling to the %s cap)", i+1, w, want, vdKeepBackoffMax)
		}
		if want *= 2; want > vdKeepBackoffMax {
			want = vdKeepBackoffMax
		}
	}
	if len(s.log) == 0 || len(s.log) > 7 {
		t.Errorf("DEFECT: %d log lines for 14 passes of one unchanging failure, want one per pause step (at most 7): %q", len(s.log), s.log)
	}
}

// A different reason is news: it is logged again.
func TestVD_KeepLoop_ResolvePhaseLogsAChangeOfError(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	resolves := 0
	env.resolve = func() error {
		if resolves++; resolves < 12 {
			return keepNotThere()
		}
		return fmt.Errorf("%w: the file is open elsewhere", vdisk.ErrIO)
	}
	s.onWait = func(s *keepSim, n int) bool { return n < 14 }
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	changed := 0
	for _, l := range s.log {
		if strings.Contains(l, "open elsewhere") {
			changed++
		}
	}
	if resolves != 14 || changed != 1 {
		t.Errorf("resolves = %d, lines for the new reason = %d, want 14 and exactly 1: %q", resolves, changed, s.log)
	}
}

// AUD-83-F4 / AUD-87-F5: a refusal that is a usage error - a container that
// gained a password and the no-terminal refusal, a ram container whose save was
// interrupted and the question nobody can answer - is no use retrying: the
// watch ends with it, in the mount stage and in the resolve stage, and never
// waits.
func TestVD_KeepPermanentOnUsageRefusal(t *testing.T) {
	refusals := []error{
		usagef("no password given and stdin is not a terminal; use p:<password>, pf:<file>, pe:<VAR> or k:<keyfile>"),
		vdUsagef("not mounted: an interrupted save needs an answer (mount it from a console, or mount it ro to look first)"),
	}
	for _, refusal := range refusals {
		s := &keepSim{mountErr: []error{refusal}}
		s.onWait = func(s *keepSim, n int) bool { return n < 10 }
		err := vdKeepLoop(s.env())
		if !errors.Is(err, refusal) || s.mounts != 1 || len(s.waits) != 0 {
			t.Errorf("DEFECT: mount refusal %q: loop returned %v after %d mount(s) and %d wait(s), want the refusal at once", refusal, err, s.mounts, len(s.waits))
		}

		s = &keepSim{}
		env := s.env()
		env.resolve = func() error { return refusal }
		s.onWait = func(s *keepSim, n int) bool { return n < 10 }
		if err := vdKeepLoop(env); !errors.Is(err, refusal) || s.mounts != 0 || len(s.waits) != 0 {
			t.Errorf("DEFECT: resolve refusal %q: loop returned %v, %d mount(s), %d wait(s)", refusal, err, s.mounts, len(s.waits))
		}
	}
	if vdKeepPermanent(keepNotThere()) || vdKeepPermanent(fmt.Errorf("%w: x", vdisk.ErrStopped)) {
		t.Error("a drive that is not there yet is waited out, not fatal")
	}
}

// The unattended mount refuses what needs a credential, by the container's own
// flag: an obfuscated container is the only kind keep carries.
func TestVD_KeepUnattendedOK(t *testing.T) {
	if err := vdKeepUnattendedOK(vdisk.Info{Obfuscated: true}, `C:\a.fdd`); err != nil {
		t.Errorf("an obfuscated container is refused: %v", err)
	}
	err := vdKeepUnattendedOK(vdisk.Info{}, `C:\a.fdd`)
	if !errors.Is(err, vdisk.ErrUsage) || !vdKeepPermanent(err) || !strings.Contains(err.Error(), "obfuscated containers only") {
		t.Errorf("DEFECT: a container that needs a credential is not refused for good: %v", err)
	}
	dir := t.TempDir()
	for _, c := range []struct {
		name    string
		profile vdisk.Profile
		cred    string
		want    bool // the unattended mount accepts it
	}{
		{"plain.fdd", vdisk.ProfilePlain, "", true},
		{"ram.fdd", vdisk.ProfileRAM, "", true},
		{"plainpw.fdd", vdisk.ProfilePlain, "pw", false},
		{"vault.fdd", vdisk.ProfileVault, "pw", false},
		{"rampw.fdd", vdisk.ProfileRAM, "pw", false},
	} {
		p := filepath.Join(dir, c.name)
		vdTestContainer(t, p, 1<<20, c.profile, c.cred)
		info, err := vdisk.Inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := vdKeepUnattendedOK(info, p) == nil; got != c.want {
			t.Errorf("DEFECT: %s (%s, credential %q): accepted = %v, want %v", c.name, c.profile, c.cred, got, c.want)
		}
	}
}

// AUD-87-F3: the pause before a remount is forgotten only after the disk has
// run for a stable period - a server that dies seconds after every mount is
// remounted ever more slowly, to the cap.
func TestVD_KeepLoop_ACrashLoopBacksOff(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	var mountAt []time.Duration
	inner := env.mount
	env.mount = func() error { mountAt = append(mountAt, s.clock); return inner() }
	s.onWait = func(s *keepSim, n int) bool {
		if s.alive && s.waits[len(s.waits)-1] == vdKeepPoll {
			s.alive = false // the fresh server dies during its first poll
		}
		return len(mountAt) < 40
	}
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	var gaps []time.Duration
	for i := 1; i < len(mountAt); i++ {
		gaps = append(gaps, mountAt[i]-mountAt[i-1])
	}
	if len(gaps) < 39 {
		t.Fatalf("only %d remounts", len(mountAt))
	}
	t.Logf("first gaps %v, last gaps %v", gaps[:8], gaps[len(gaps)-3:])
	for i := 1; i < len(gaps); i++ {
		if gaps[i] < gaps[i-1] {
			t.Errorf("DEFECT: the gap before remount %d (%s) is shorter than the one before (%s)", i+2, gaps[i], gaps[i-1])
		}
	}
	if last := gaps[len(gaps)-1]; last < vdKeepBackoffMax {
		t.Errorf("DEFECT: the 40th remount of a disk that dies within seconds each time comes %s after the 39th, want a pause that grew to %s", last, vdKeepBackoffMax)
	}
}

// A disk that ran for the stable period before it died is remounted at once,
// with the pause forgotten; the next short life pauses from the minimum again.
func TestVD_KeepLoop_AStableLifeForgetsTheBackoff(t *testing.T) {
	s := &keepSim{}
	env := s.env()
	var mountAt, diedAt []time.Duration
	inner := env.mount
	env.mount = func() error { mountAt = append(mountAt, s.clock); return inner() }
	s.onWait = func(s *keepSim, n int) bool {
		if s.alive && s.waits[len(s.waits)-1] == vdKeepPoll {
			k := len(mountAt)
			if k != 4 || s.clock-mountAt[k-1] >= 3*time.Minute { // the fourth server runs for three minutes
				s.alive = false
				diedAt = append(diedAt, s.clock)
			}
		}
		return len(mountAt) < 6
	}
	if err := vdKeepLoop(env); err != nil {
		t.Fatal(err)
	}
	if len(mountAt) < 6 || len(diedAt) < 5 {
		t.Fatalf("mounts = %d deaths = %d", len(mountAt), len(diedAt))
	}
	if got := mountAt[4] - diedAt[3]; got != vdKeepSettle {
		t.Errorf("after a three minute life the remount comes %s after the death, want the settle time %s alone", got, vdKeepSettle)
	}
	if got := mountAt[5] - diedAt[4]; got != vdKeepSettle+vdKeepBackoffMin {
		t.Errorf("DEFECT: the next short life is remounted %s after its death, want the settle time plus the minimum pause %s", got, vdKeepSettle+vdKeepBackoffMin)
	}
}

// The pause before a remount is a window in which the owner may unmount: the
// disk is not mounted behind that unmount's back.
func TestVD_KeepLoop_AnUnmountDuringTheRemountPauseIsRespected(t *testing.T) {
	s := &keepSim{}
	s.onWait = func(s *keepSim, n int) bool {
		switch n {
		case 1:
			s.alive = false // the server dies at its first poll; the row stays
		case 8:
			s.row = nil // the owner unmounts during the pause that follows the settle
		}
		return n < 30
	}
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 1 {
		t.Errorf("DEFECT: %d mounts, want the first only - the disk was unmounted during the pause", s.mounts)
	}
}

// AUD-87-F4: a state that cannot be read is "unknown", never "the owner
// unmounted": the watch asks again and neither ends nor mounts.
func TestVD_KeepLoop_AnUnreadableStateIsNotAnUnmount(t *testing.T) {
	s := &keepSim{row: &vdMountRow{Path: `C:\a.fdd`, ServerPID: 1}, alive: true}
	s.onWait = func(s *keepSim, n int) bool {
		switch n {
		case 1:
			s.rowErr = errors.New("the mount state is unreadable: sharing violation")
		case 3:
			s.rowErr = nil
		}
		return n < 6
	}
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if len(s.waits) < 6 {
		t.Errorf("DEFECT: the watch ended after %d wait(s) because one read of the state failed; log=%q", len(s.waits), s.log)
	}
	if s.mounts != 0 {
		t.Errorf("DEFECT: %d mount(s) while the state could not be read", s.mounts)
	}
	unread := 0
	for _, l := range s.log {
		if strings.Contains(l, "cannot be read") {
			unread++
		}
	}
	if unread != 1 {
		t.Errorf("the unreadable state was logged %d times, want once: %q", unread, s.log)
	}
}

// The settle of a gone server reads the row too: an unreadable state during it
// is no verdict, and the crash is still repaired once the state reads again.
func TestVD_KeepLoop_AnUnreadableStateDoesNotSettleAsAnUnmount(t *testing.T) {
	s := &keepSim{row: &vdMountRow{Path: `C:\a.fdd`, ServerPID: 1}, alive: false}
	s.onWait = func(s *keepSim, n int) bool {
		switch n {
		case 1:
			s.rowErr = errors.New("sharing violation")
		case 3:
			s.rowErr = nil
		}
		return n < 20
	}
	if err := vdKeepLoop(s.env()); err != nil {
		t.Fatal(err)
	}
	if s.mounts != 1 {
		t.Errorf("DEFECT: %d mounts, want 1: a crashed server whose row could not be read for a moment was taken for an unmount; log=%q", s.mounts, s.log)
	}
}

// The watcher's own read of the state reports a failure instead of "no row".
func TestVD_KeepRowReportsAnUnreadableState(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const id = "9b28e437-f594-4b27-9f10-7bb50a7bad81"
	if _, ok, err := vdKeepRow(id); ok || err != nil {
		t.Fatalf("no state yet: ok = %v err = %v, want neither", ok, err)
	}
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = []vdMountRow{{ContainerID: id, Path: `C:\a.fdd`, Letter: "D:", ServerPID: 7}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if r, ok, err := vdKeepRow(id); !ok || err != nil || r.ServerPID != 7 {
		t.Fatalf("a row: %+v ok = %v err = %v", r, ok, err)
	}
	p, _ := vdStatePath()
	if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := vdKeepRow(id); ok || err == nil {
		t.Errorf("DEFECT: an unreadable state reads as ok = %v err = %v, want an error", ok, err)
	}
}

// AUD-87-F1: a stop file is a request only when it is newer than the watcher;
// an older one is a leftover of an `auto off` from before, and is removed.
func TestVD_KeepStopRequested(t *testing.T) {
	p := filepath.Join(t.TempDir(), "vd-x.keep.stop")
	since := time.Now()
	if vdKeepStopRequested(p, since) {
		t.Fatal("no file, yet a stop was requested")
	}
	put := func(age time.Duration) {
		t.Helper()
		if err := os.WriteFile(p, []byte("stop"), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := since.Add(age)
		if err := os.Chtimes(p, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	put(-time.Hour)
	if vdKeepStopRequested(p, since) {
		t.Error("DEFECT: a stop file from before the watcher started ended it")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("the stale stop file was not removed")
	}
	put(time.Second)
	if !vdKeepStopRequested(p, since) {
		t.Error("DEFECT: a stop file written after the watcher started is not a request")
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("a live request was removed by the check: %v", err)
	}
}

// The watcher knows its stop file before the container can be read: the id of
// the registry entry of its path, beside the id of the file once it is read.
func TestVD_KeepStopPathsAndRegisteredID(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const a, b = "9b28e437-f594-4b27-9f10-7bb50a7bad81", "0d1c2b3a-1111-4222-8333-444455556666"
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		r.Containers = append(r.Containers, vdRegEntry{Name: "work", Path: `E:\vault\work.fdd`, ContainerID: a})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := vdKeepRegisteredID(`e:\VAULT\Work.fdd`); got != a {
		t.Errorf("registered id of the path = %q, want %q", got, a)
	}
	if got := vdKeepRegisteredID(`E:\other.fdd`); got != "" {
		t.Errorf("an unregistered path has id %q", got)
	}
	want, _ := vdKeepStopPath(a)
	if got := vdKeepStopPaths("", a, strings.ToUpper(a)); len(got) != 1 || got[0] != want {
		t.Errorf("stop paths of one id = %v, want [%s]", got, want)
	}
	if got := vdKeepStopPaths(a, b); len(got) != 2 {
		t.Errorf("stop paths of two ids = %v, want two", got)
	}
	if got := vdKeepStopPaths(""); len(got) != 0 {
		t.Errorf("stop paths of no id = %v", got)
	}
}

// AUD-87-F1: a wait sleeps in slices and returns the moment a stop is seen: a
// request is not slept through, not even inside a five minute pause.
func TestVD_KeepSleepEndsAtAStop(t *testing.T) {
	var slept time.Duration
	nap := func(d time.Duration) {
		if d > vdKeepSlice {
			t.Errorf("DEFECT: one sleep of %s, longer than a slice (%s)", d, vdKeepSlice)
		}
		slept += d
	}
	if ok := vdKeepSleep(vdKeepBackoffMax, func() bool { return slept >= 2*time.Second }, nap); ok || slept > 3*time.Second {
		t.Errorf("DEFECT: a stop two seconds into a %s wait: ok = %v after sleeping %s", vdKeepBackoffMax, ok, slept)
	}
	slept = 0
	if ok := vdKeepSleep(3500*time.Millisecond, func() bool { return false }, nap); !ok || slept != 3500*time.Millisecond {
		t.Errorf("an undisturbed wait: ok = %v slept %s, want true and 3.5s", ok, slept)
	}
	if ok := vdKeepSleep(time.Second, func() bool { return true }, nap); ok {
		t.Error("a wait that starts after the stop returns true")
	}
}

// AUD-87-F1: auto off asks the watcher to end whether or not the task is still
// there - a task deleted by hand leaves a running watcher nobody could end.
func TestVD_AutoOffStopsTheWatcherWithoutATask(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const id = "9b28e437-f594-4b27-9f10-7bb50a7bad81"
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		r.Containers = append(r.Containers, vdRegEntry{Name: "work", Path: `C:\a.fdd`, ContainerID: id})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	stop, _ := vdKeepStopPath(id)
	stopWritten := func() bool { _, err := os.Stat(stop); return err == nil }

	err := vdAutoOffRun("work", false, func() error { t.Error("a task that is not there was deleted"); return nil })
	if err != nil || !stopWritten() {
		t.Errorf("DEFECT: auto off of a registered name whose task is gone: err = %v, stop file written = %v", err, stopWritten())
	}
	os.Remove(stop)

	if err := vdAutoOffRun("nobody", false, func() error { return nil }); !errors.Is(err, vdisk.ErrUsage) {
		t.Errorf("neither a task nor a registered name: %v, want the usage refusal", err)
	}

	if err := vdAutoOffRun("work", true, func() error { return errors.New("consent declined") }); err == nil || stopWritten() {
		t.Errorf("a task that could not be removed: err = %v, stop file written = %v, want an error and no stop request", err, stopWritten())
	}

	removed := false
	if err := vdAutoOffRun("work", true, func() error { removed = true; return nil }); err != nil || !removed || !stopWritten() {
		t.Errorf("with the task: err = %v removed = %v stop file written = %v", err, removed, stopWritten())
	}
}
