//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"filedo/vdisk"
)

// Keep-alive (`filedo <file.fdd> mount keep`): the word the automatic-mount
// task carries. It mounts the container like a plain mount and then stays
// resident for the rest of the session, so the disk is there from logon on and
// comes back when its block server dies - a crash, a kill, a stale record left
// by a session that ended without an unmount.
//
// What it never does: it never remounts a disk the owner unmounted (the mount
// row going away while the server is gone is an unmount, and ends the watch; a
// row that cannot be read is neither), it never raises a consent prompt or asks
// a question (it needs the elevation the logon task already has, refuses to
// start without it, and mounts as a batch does), and it never touches an
// encrypted container (the auto verb refuses those; a container that needs a
// credential - one that gained a password after the task was made included - or
// a question nobody can answer ends the watch as a permanent refusal).
//
// One watcher per container: a named mutex keeps a second one out. `vd auto
// off` asks a running watcher to end through a stop file, so it does not
// outlive its task to the end of the session. The stop file is a request only
// when it is newer than the watcher; the watcher knows it before the container
// can be read (the registry entry of its path), and every wait of the watch
// sleeps in slices that end at once when a stop is seen.
//
// Every wait is a growing pause (5 s doubling to 5 min), and a repeated failure
// is logged when it first appears, when its text changes and when its pause
// grows - not once per pass, so a container that is absent for the whole
// session does not fill vdisk.log.

const (
	vdKeepPoll       = 5 * time.Second
	vdKeepSettle     = 6 * time.Second // how long a gone server may take to lose its row before it counts as a crash
	vdKeepBackoffMin = 5 * time.Second
	vdKeepBackoffMax = 5 * time.Minute
	// vdKeepStable is how long a disk this watch mounted must run before the
	// pause is forgotten: a server that dies seconds after every mount is
	// remounted ever more slowly, not at the same pace for ever.
	vdKeepStable = 2 * time.Minute
	// vdKeepSlice bounds one sleep of a wait, so a stop is seen within it.
	vdKeepSlice = time.Second
)

// vdKeepEnv is everything the watch loop touches, so the loop's decisions are
// tested without a disk, a clock or an initiator.
type vdKeepEnv struct {
	// resolve reads the container until its identity is known - a drive that is
	// not up yet fails here, and is waited out. It runs until it returns nil, and
	// a permanent error ends the watch. nil: the container is known already.
	resolve func() error
	// ready runs once, when the container is known and before the first look at
	// its mount row: it takes the per-container lock. false means another watcher
	// holds it, and this one ends quietly. nil: nothing to take.
	ready func() bool
	// now is the clock the stable-life rule reads; nil means time.Now.
	now func() time.Time
	// row is the container's mount row, and whether it has one. A state that
	// cannot be read is an error, never "no row": it says nothing about whether
	// the owner unmounted.
	row func() (vdMountRow, bool, error)
	// alive: the row's block server runs.
	alive func(vdMountRow) bool
	// mount brings the disk up: it clears a stale row, mounts, and returns nil
	// when the disk is mounted with a live server - an already healthy mount
	// included.
	mount func() error
	// stopped: the owner asked the watch to end (stop file, interrupt).
	stopped func() bool
	// wait sleeps; false means the watch was asked to end meanwhile.
	wait func(time.Duration) bool
	logf func(format string, args ...interface{})
}

// vdKeepPermanent is a mount failure no retry fixes: the file is damaged, not
// a container, or needs a credential this task cannot give, or a command line
// or a question nobody can answer is refused (the usage class: a container that
// gained a password and reaches the no-terminal refusal, a ram container whose
// interrupted save asks "mount it anyway?"). Anything else - a file on a drive
// that is not there yet, a busy file, an initiator that is not up - is waited
// out with a growing pause.
func vdKeepPermanent(err error) bool {
	return errors.Is(err, vdisk.ErrDamaged) || errors.Is(err, vdisk.ErrCredential) || errors.Is(err, vdisk.ErrUnsupported) ||
		vdExitClass(err) == vdisk.ExitUsage
}

// vdKeepUnattendedOK refuses a container the watcher cannot mount without a
// person: one that needs a credential. An obfuscated container is the only kind
// keep carries; the refusal is a usage error, so it is permanent.
func vdKeepUnattendedOK(info vdisk.Info, path string) error {
	if !info.Obfuscated {
		return vdUsagef("keep keeps obfuscated containers only: %s needs a credential, and a watch that remounts without asking has nobody to give it", path)
	}
	return nil
}

// vdKeepNote decides when a repeated condition is worth another log line: the
// first time, when its text changes, and when the pause that follows it grows.
type vdKeepNote struct {
	set  bool
	text string
	step time.Duration
}

func (n *vdKeepNote) changed(text string, step time.Duration) bool {
	if n.set && n.text == text && n.step == step {
		return false
	}
	n.set, n.text, n.step = true, text, step
	return true
}

func (n *vdKeepNote) clear() { *n = vdKeepNote{} }

// vdKeepGrow is the next pause: doubled, to the cap.
func vdKeepGrow(d time.Duration) time.Duration {
	if d *= 2; d > vdKeepBackoffMax {
		d = vdKeepBackoffMax
	}
	return d
}

// vdKeepResolve is the first stage of the watch: the container is read until
// its identity is known, with the growing pause and the stop checks of every
// other stage. done means the watch is over, with err as its result.
func vdKeepResolve(env vdKeepEnv) (done bool, err error) {
	pause := vdKeepBackoffMin
	var note vdKeepNote
	for {
		if env.stopped() {
			return true, nil
		}
		rerr := env.resolve()
		if rerr == nil {
			break
		}
		if vdKeepPermanent(rerr) {
			env.logf("keep: the container cannot be watched, the watch ends: %v", rerr)
			return true, rerr
		}
		if note.changed(rerr.Error(), pause) {
			env.logf("keep: the container cannot be read yet (%v); trying again in %s", rerr, pause)
		}
		if !env.wait(pause) {
			return true, nil
		}
		pause = vdKeepGrow(pause)
	}
	if env.ready != nil && !env.ready() {
		return true, nil
	}
	return false, nil
}

// vdKeepReadRow reads the mount row. While the state cannot be read it waits a
// poll and asks again: an unreadable state is unknown, never "no row", so it
// ends nothing and mounts nothing. stop is true when the watch was asked to end
// meanwhile.
func vdKeepReadRow(env vdKeepEnv, note *vdKeepNote) (r vdMountRow, ok, stop bool) {
	for {
		row, found, err := env.row()
		if err == nil {
			note.clear()
			return row, found, false
		}
		if note.changed(err.Error(), vdKeepPoll) {
			env.logf("keep: the mount state cannot be read (%v); asking again in %s", err, vdKeepPoll)
		}
		if !env.wait(vdKeepPoll) || env.stopped() {
			return vdMountRow{}, false, true
		}
	}
}

// vdKeepLoop runs until the owner stops it or unmounts the disk, and returns
// nil for those, or the permanent error that ended it.
func vdKeepLoop(env vdKeepEnv) error {
	now := env.now
	if now == nil {
		now = time.Now
	}
	if env.resolve != nil {
		if done, err := vdKeepResolve(env); done {
			return err
		}
	}
	backoff := vdKeepBackoffMin
	everUp := false   // the disk has been seen healthy by this watch
	retrying := false // a remount after a crash has failed and is being retried
	// fresh: this watch mounted the disk, and it has not yet run for
	// vdKeepStable. A crash while the disk is fresh is a short life, and its
	// remount waits the grown pause first.
	fresh := false
	var mountedAt time.Time
	var failNote, stateNote vdKeepNote
	for {
		if env.stopped() {
			return nil
		}
		r, ok, stop := vdKeepReadRow(env, &stateNote)
		if stop {
			return nil
		}
		switch {
		case ok && env.alive(r):
			everUp, retrying = true, false
			if fresh && now().Sub(mountedAt) >= vdKeepStable {
				fresh, backoff = false, vdKeepBackoffMin
			}
			if !env.wait(vdKeepPoll) {
				return nil
			}
			continue
		case ok:
			// The server is gone. An unmount stops the server first and removes
			// the row a moment later, so give the row that moment before this
			// counts as a crash.
			switch vdKeepSettled(env, &stateNote) {
			case vdKeepUnmounted:
				env.logf("keep: the row of %s went away after its server exited; an unmount, so the watch ends", r.Path)
				return nil
			case vdKeepStopped:
				return nil
			}
			env.logf("keep: the block server of %s (pid %d) is gone; remounting", r.Path, r.ServerPID)
			retrying = true
			if fresh {
				// A short life: the disk died soon after this watch mounted it.
				// Pause before the next try, and let the owner unmount meanwhile.
				env.logf("keep: the disk of %s ran for %s only; waiting %s before it is mounted again", r.Path, now().Sub(mountedAt).Round(time.Second), backoff)
				if !env.wait(backoff) {
					return nil
				}
				backoff = vdKeepGrow(backoff)
				_, still, stop := vdKeepReadRow(env, &stateNote)
				if stop {
					return nil
				}
				if !still {
					env.logf("keep: the mount row went away during the pause; the disk was unmounted, so the watch ends")
					return nil
				}
			}
		case everUp && !retrying:
			env.logf("keep: the mount row is gone; the disk was unmounted, so the watch ends")
			return nil
		}
		// Not mounted yet (first start), or crashed: mount, healing a stale row.
		if err := env.mount(); err != nil {
			if vdKeepPermanent(err) {
				env.logf("keep: the mount failed for good, the watch ends: %v", err)
				return err
			}
			if failNote.changed(err.Error(), backoff) {
				env.logf("keep: the mount failed (%v); trying again in %s", err, backoff)
			}
			retrying = true
			if !env.wait(backoff) {
				return nil
			}
			backoff = vdKeepGrow(backoff)
			continue
		}
		failNote.clear()
		// The pause is not reset here: only a disk that runs for vdKeepStable
		// earns that (the alive branch above).
		everUp, retrying = true, false
		fresh, mountedAt = true, now()
		env.logf("keep: mounted")
	}
}

// vdKeepVerdict is what the settle time made of a gone server.
type vdKeepVerdict int

const (
	vdKeepCrashed   vdKeepVerdict = iota // the row stayed: the server died
	vdKeepUnmounted                      // the row went away: the owner unmounted
	vdKeepStopped                        // the watch was asked to end meanwhile
)

// vdKeepSettled reports whether the mount row of a gone server disappears
// within the settle time - which is what an unmount in progress looks like. A
// state that cannot be read does not count as the row going away.
func vdKeepSettled(env vdKeepEnv, note *vdKeepNote) vdKeepVerdict {
	for waited := time.Duration(0); waited < vdKeepSettle; waited += time.Second {
		_, ok, stop := vdKeepReadRow(env, note)
		switch {
		case stop:
			return vdKeepStopped
		case !ok:
			return vdKeepUnmounted
		}
		if !env.wait(time.Second) {
			return vdKeepStopped
		}
	}
	_, ok, stop := vdKeepReadRow(env, note)
	switch {
	case stop:
		return vdKeepStopped
	case !ok:
		return vdKeepUnmounted
	}
	return vdKeepCrashed
}

// vdKeepRow is the watcher's read of the container's mount row. Unlike
// vdFindMount it does not turn a state file that cannot be read (a sharing
// violation against the atomic replace, a half-written file) into "no row".
func vdKeepRow(id string) (vdMountRow, bool, error) {
	s, err := vdLoadState()
	if err != nil {
		return vdMountRow{}, false, err
	}
	for _, m := range s.Mounts {
		if strings.EqualFold(m.ContainerID, id) {
			return m, true, nil
		}
	}
	return vdMountRow{}, false, nil
}

// vdKeepSleep waits d in slices of at most vdKeepSlice and returns false as soon
// as stopped reports a stop, so a request is not slept through, not even inside
// a five minute pause. nap is the sleep.
func vdKeepSleep(d time.Duration, stopped func() bool, nap func(time.Duration)) bool {
	for d > 0 {
		if stopped() {
			return false
		}
		s := d
		if s > vdKeepSlice {
			s = vdKeepSlice
		}
		nap(s)
		d -= s
	}
	return !stopped()
}

// vdKeepNap sleeps d, or less when the run is interrupted.
func vdKeepNap(d time.Duration) {
	if h := globalInterruptHandler; h != nil {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-h.Context().Done():
		case <-t.C:
		}
		return
	}
	time.Sleep(d)
}

// vdKeepStopPath is the stop file of a container's watcher.
func vdKeepStopPath(containerID string) (string, error) {
	return vdSidePath(containerID, ".keep.stop")
}

// vdKeepStopPaths are the stop files a watcher honours: one per distinct
// container id it knows (the id of the file once it is read, and the id of the
// registry entry of its path, which is all it has while the drive is out).
func vdKeepStopPaths(ids ...string) []string {
	var paths []string
	seen := map[string]bool{}
	for _, id := range ids {
		k := strings.ToLower(id)
		if id == "" || seen[k] {
			continue
		}
		seen[k] = true
		if p, err := vdKeepStopPath(id); err == nil {
			paths = append(paths, p)
		}
	}
	return paths
}

// vdKeepStopRequested reports whether the stop file at path is a request to the
// watcher that started at since. A file older than the watcher is a leftover of
// an `auto off` from before this run - it must not end it - and is removed; a
// newer one ends the watch, even when it was written before the container could
// be read.
func vdKeepStopRequested(path string, since time.Time) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.ModTime().Before(since) {
		os.Remove(path)
		return false
	}
	return true
}

// vdKeepRegisteredID is the container id of the registry entry whose path is
// path, or "" - the watcher's stop file while the container cannot be read.
func vdKeepRegisteredID(path string) string {
	r, err := vdLoadRegistry()
	if err != nil {
		return ""
	}
	p := filepath.Clean(path)
	for _, e := range r.Containers {
		if strings.EqualFold(filepath.Clean(e.Path), p) {
			return e.ContainerID
		}
	}
	return ""
}

// vdKeepRequestStop asks the watcher of the container a name or path means to
// end. Best effort: with no watcher the file is older than the next one, which
// removes it as stale.
func vdKeepRequestStop(name string) {
	r, err := vdLoadRegistry()
	if err != nil {
		return
	}
	i := r.find(name)
	if i < 0 {
		return
	}
	if p, err := vdKeepStopPath(r.Containers[i].ContainerID); err == nil {
		os.WriteFile(p, []byte("stop"), 0o600)
	}
}

// vdKeep is the verb's body: path is the container, plain the mount line
// without the keep word. batch is not passed on: the watcher is unattended by
// nature, so its mounts always run as a batch does - no consent prompt, and a
// question (an interrupted ram save) is answered "no" instead of waited for.
func vdKeep(path string, plain []string, batch bool) error {
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errTransport("keep needs administrator rights, because it remounts without asking; it is meant for the logon task (filedo vd auto), which has them")
	}
	if abs, err := absPath(path); err == nil && !isPartLocator(path) {
		path = abs
	}
	// A stop file is a request only when it is newer than this process.
	since := time.Now()
	if ns, ok := vdProcessStart(os.Getpid()); ok {
		since = time.Unix(0, ns)
	}
	// The container id comes from the file; until the file can be read (a drive
	// that is not up yet) the watch waits like for any other failed mount, and
	// its stop file is the one of the registry entry of its path.
	var id string
	regID := vdKeepRegisteredID(path)
	stopped := func() bool {
		if interrupted() {
			return true
		}
		requested := false
		for _, p := range vdKeepStopPaths(id, regID) {
			if vdKeepStopRequested(p, since) {
				requested = true
			}
		}
		return requested
	}
	var release func()
	duplicate := false
	defer func() {
		if release != nil {
			release()
		}
	}()

	env := vdKeepEnv{
		resolve: func() error {
			if id != "" {
				return nil
			}
			regID = vdKeepRegisteredID(path)
			info, err := vdInspectAny(path)
			if err != nil {
				return err
			}
			if err := vdKeepUnattendedOK(info, path); err != nil {
				return err
			}
			id = info.ContainerID
			return nil
		},
		ready: func() bool {
			var held bool
			if release, held = vdKeepLock(id); !held {
				duplicate = true
				vdLogf("keep: a watcher of %s runs already; this one exits", path)
				fmt.Printf("%s is watched already.\n", path)
				return false
			}
			if vdOwnsConsole() {
				vdGuardHideConsole()
			}
			vdLogf("keep: watching %s (pid %d)", path, os.Getpid())
			return true
		},
		row:   func() (vdMountRow, bool, error) { return vdKeepRow(id) },
		alive: vdServerAlive,
		mount: func() error {
			if m, ok := vdFindMount(id); ok && vdServerAlive(m) {
				return nil
			}
			// A container that gained a password since the watch began must not
			// reach a prompt: it is refused here, as a usage error, for good.
			if info, err := vdInspectAny(path); err == nil {
				if err := vdKeepUnattendedOK(info, path); err != nil {
					return err
				}
			}
			return vdMount(plain, true)
		},
		stopped: stopped,
		wait: func(d time.Duration) bool {
			return vdKeepSleep(d, stopped, vdKeepNap)
		},
		logf: vdLogf,
	}
	err := vdKeepLoop(env)
	if duplicate {
		return err // the stop file belongs to the watcher that holds the lock
	}
	for _, p := range vdKeepStopPaths(id, regID) {
		os.Remove(p)
	}
	vdLogf("keep: the watch of %s ended", path)
	return err
}

func interrupted() bool {
	return globalInterruptHandler != nil && globalInterruptHandler.IsInterrupted()
}

// vdKeepLock takes the per-container mutex. False means another watcher holds
// it: an elevated watcher's mutex answers a plain process with access denied,
// which is the same answer.
func vdKeepLock(containerID string) (release func(), held bool) {
	name, err := windows.UTF16PtrFromString(`Local\FileDO-vd-keep-` + strings.ToLower(strings.ReplaceAll(containerID, "-", "")))
	if err != nil {
		return func() {}, true
	}
	h, err := windows.CreateMutex(nil, true, name)
	if h == 0 || err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_ACCESS_DENIED {
		if h != 0 {
			windows.CloseHandle(h)
		}
		return func() {}, false
	}
	return func() { windows.ReleaseMutex(h); windows.CloseHandle(h) }, true
}

// vdOwnsConsole is true when this process is the only one on its console: the
// console a logon task gives a console program, which nobody reads. A terminal
// the owner typed into holds other processes, and is never hidden.
func vdOwnsConsole() bool {
	var pids [2]uint32
	r, _, _ := vdGuardKernel32.NewProc("GetConsoleProcessList").Call(uintptr(unsafe.Pointer(&pids[0])), uintptr(len(pids)))
	return r == 1
}
