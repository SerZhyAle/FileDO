//go:build windows

package main

import (
	"context"
	"errors"
	"filedo/fmsworker"
	"filedo/fsx"
	"filedo/statedir"
	"filedo/vdisk"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type vdWorker interface {
	ShareDisk(string, string, bool) error
	UnshareDisk(string) error
	SetAutostart(string, bool, []byte) error
	GetDiskStatus(string) (*fmsworker.SharedDiskInfo, error)
	ListSharedDisks() ([]fmsworker.SharedDiskInfo, error)
	ListRoots() ([]string, error)
	WorkerMode() fmsworker.WorkerMode
	OpenDisk(path, password string) error
	CloseDiskBound(path string, force bool, bound int) error
}

var vdWorkerFactory = func() (vdWorker, error) {
	c := fmsworker.NewFMSWorkerClient()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if e := c.Connect(ctx); e != nil {
		return nil, fmt.Errorf("%w: start or update FMS for Windows: %v", vdisk.ErrUnsupported, e)
	}
	return c, nil
}
var vdShareInspect = vdisk.Inspect
var vdAutostartConsent = vdConfirm
var vdAutostartCredential = vdResolveCredential

func getFMSWorkerClient() (vdWorker, error) { return vdWorkerFactory() }
func vdSharePath(arg string, mustExist bool) (string, error) {
	p, e := filepath.Abs(vdResolve(arg))
	if e != nil {
		return "", e
	}
	if mustExist {
		s, e := os.Stat(p)
		if e != nil {
			return "", e
		}
		if !s.Mode().IsRegular() || !strings.EqualFold(filepath.Ext(p), ".fdd") {
			return "", vdUsagef("share needs an existing .fdd container")
		}
	}
	return fsx.Resolve(p)
}

// vdClassed carries an FDD-BEHAVIOUR outcome class (as its vdisk sentinel) next
// to a worker-side error, so vdExitClass sees the class and errors.Is still
// reaches the worker's own sentinel.
type vdClassed struct{ class, err error }

func (e *vdClassed) Error() string        { return e.err.Error() }
func (e *vdClassed) Unwrap() error        { return e.err }
func (e *vdClassed) Is(target error) bool { return target == e.class }

// vdClassSentinel is the sentinel of a worker-reported outcome class; ok is
// false for a number that is not an FDD-BEHAVIOUR section 4 class.
func vdClassSentinel(class int) (error, bool) {
	switch class {
	case vdisk.ExitUsage:
		return vdisk.ErrUsage, true
	case vdisk.ExitCredential:
		return vdisk.ErrCredential, true
	case vdisk.ExitDamaged:
		return vdisk.ErrDamaged, true
	case vdisk.ExitIO:
		return vdisk.ErrIO, true
	case vdisk.ExitUnsupported:
		return vdisk.ErrUnsupported, true
	case vdisk.ExitBusy:
		return vdisk.ErrBusy, true
	}
	return nil, false
}

// vdNotShared reports the worker's "this disk is not shared" answer.
func vdNotShared(err error) bool {
	if errors.Is(err, fmsworker.ErrDiskNotShared) || errors.Is(err, vdisk.ErrDiskNotShared) {
		return true
	}
	var we *fmsworker.WorkerError
	return errors.As(err, &we) && strings.Contains(strings.ToLower(we.Message), strings.ToLower(fmsworker.ErrDiskNotShared.Error()))
}

// vdWorkerError gives a worker-side failure its FDD-BEHAVIOUR class: the
// worker's own class when it sent a valid one, otherwise by what the failure
// is. An error that already carries a class passes through; an unclassified
// one stays I/O (class 5) in vdExitClass.
func vdWorkerError(err error) error {
	if err == nil {
		return nil
	}
	for _, s := range []error{vdisk.ErrDamaged, vdisk.ErrCredential, vdisk.ErrUnsupported, vdisk.ErrBusy, vdisk.ErrUsage, vdisk.ErrStopped} {
		if errors.Is(err, s) {
			return err
		}
	}
	var we *fmsworker.WorkerError
	if errors.As(err, &we) {
		if s, ok := vdClassSentinel(we.OutcomeClass); ok {
			return &vdClassed{s, err}
		}
	}
	switch {
	case errors.Is(err, fmsworker.ErrAlreadyShared), errors.Is(err, vdisk.ErrAlreadyShared):
		// The FMS holder owns the container, as another process holding its lock does.
		return &vdClassed{vdisk.ErrBusy, err}
	case errors.Is(err, fmsworker.ErrRootNameClash), errors.Is(err, fmsworker.ErrDiskNotFound), vdNotShared(err):
		return &vdClassed{vdisk.ErrUsage, err}
	case errors.Is(err, fmsworker.ErrWorkerUnavailable), errors.Is(err, fmsworker.ErrNotCapable):
		return &vdClassed{vdisk.ErrUnsupported, err}
	}
	return err
}

// vdSnapshotWarn reports a snapshot failure after the worker already did the
// work: the snapshot is a cache, so this never changes the outcome.
func vdSnapshotWarn(what string, err error) {
	msg := fmt.Sprintf("%s; the local shared-disk snapshot could not be updated and will be refreshed by the next share or autostart command: %v", what, err)
	fmt.Fprintln(os.Stderr, "warning: "+msg)
	vdLogf("share snapshot: %s", msg)
}

func vdShare(args []string, batch bool) error { return vdWorkerError(vdShareRun(args, batch)) }
func vdShareRun(args []string, batch bool) error {
	if len(args) < 2 {
		return vdUsagef("vd share needs <container> on|off [ro] [as <name>]")
	}
	action := strings.ToLower(args[1])
	if action != "on" && action != "off" {
		return vdUsagef("vd share needs on or off")
	}
	name := ""
	ro := false
	for i := 2; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "ro":
			if action == "off" || ro {
				return vdUsagef("ro is an on option")
			}
			ro = true
		case "as":
			if action == "off" || name != "" || i+1 >= len(args) {
				return vdUsagef("as needs one root name when sharing on")
			}
			i++
			name = args[i]
			if e := validateRootName(name); e != nil {
				return vdUsagef("%v", e)
			}
		default:
			return vdUsagef("unknown share option at word %d", i+1)
		}
	}
	path, e := vdSharePath(args[0], action == "on")
	if e != nil {
		return e
	}
	client, e := getFMSWorkerClient()
	if e != nil {
		return e
	}
	if action == "off" {
		if st, se := client.GetDiskStatus(path); se == nil && st.OpenHandles > 0 {
			fmt.Printf("%d open handle(s) on %s; the holder closes it cleanly or refuses.\n", st.OpenHandles, path)
		}
		// The worker owns close/drain/unshare as one operation. Never ignore a failed
		// close; a disk it does not know is already unshared, which is what off asks.
		already := false
		if e = client.UnshareDisk(path); e != nil {
			if !vdNotShared(e) {
				return e
			}
			already = true
		}
		if e = vdRefreshShareSnapshot(client); e != nil {
			vdSnapshotWarn("unshared", e)
		}
		vdLogf("share: off %s", path)
		if already {
			fmt.Printf("%s is not shared with FMS for Windows; its local record was cleared.\n", path)
		} else {
			fmt.Printf("Unshared %s from FMS for Windows.\n", path)
		}
		return nil
	}
	info, e := vdShareInspect(path)
	if e != nil {
		return e
	}
	// Fail closed: a mount state that cannot be read cannot prove FileDO is not the holder.
	st, e := vdLoadState()
	if e != nil {
		return fmt.Errorf("the mount state could not be read, so it is not known whether FileDO holds this disk; nothing was shared: %w", e)
	}
	for _, m := range st.Mounts {
		if strings.EqualFold(m.ContainerID, info.ContainerID) {
			return fmt.Errorf("%w: already mounted by FileDO at %s; unmount it before sharing", vdisk.ErrBusy, m.Letter)
		}
	}
	disks, e := client.ListSharedDisks()
	if e != nil {
		return e
	}
	for _, d := range disks {
		same, se := fsx.SameFile(path, d.ContainerPath)
		if se == nil && same || d.ContainerID == info.ContainerID {
			return fmt.Errorf("%w: %s", fmsworker.ErrAlreadyShared, path)
		}
	}
	if name == "" {
		name = generateRootNameFromContainer(info, path)
	}
	if e = validateRootName(name); e != nil {
		return vdUsagef("give a usable root name with as <name>")
	}
	if e = checkRootNameClash(client, name); e != nil {
		return e
	}
	ro = ro || info.Profile == vdisk.ProfileSealed
	if e = client.ShareDisk(path, name, ro); e != nil {
		return e
	}
	if e = vdRefreshShareSnapshot(client); e != nil {
		vdSnapshotWarn("shared", e)
	}
	vdLogf("share: on %s as %s ro=%t", path, name, ro)
	fmt.Printf("Shared %s as FMS root '%s' (read-only: %t).\n", path, name, ro)
	vdSayHolderMode(client.WorkerMode())
	fmt.Println("Open FMS Share Manager to obtain the QR code for paired devices.")
	return nil
}
func validateRootName(name string) error {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, `/\`) {
		return errors.New("root name must be nonempty and contain no path separators")
	}
	if name == "." || name == ".." {
		return errors.New("root name cannot be a relative path")
	}
	return nil
}
func cleanRootName(name string) string {
	s := strings.TrimSpace(strings.NewReplacer("/", "-", `\`, "-").Replace(name))
	if strings.Trim(s, "- ") == "" {
		return ""
	}
	return s
}
func generateRootNameFromContainer(info vdisk.Info, path string) string {
	if name := cleanRootName(info.FriendlyName); name != "" {
		return name
	}
	b := filepath.Base(path)
	return cleanRootName(b[:len(b)-len(filepath.Ext(b))])
}
func checkRootNameClash(client vdWorker, name string) error {
	roots, e := client.ListRoots()
	if e != nil {
		return e
	}
	for _, r := range roots {
		if strings.EqualFold(r, name) {
			return fmt.Errorf("%w: choose another name with as <name>", fmsworker.ErrRootNameClash)
		}
	}
	return nil
}
func vdShareSnapshot() (*vdisk.SharedDiskManager, func(), error) { return vdShareSnapshotOpen(false) }

// vdShareSnapshotOpen locks and loads the snapshot, waiting up to 5 s for the lock. With rebuild an
// unreadable file is not an error: the caller replaces the whole cache from the worker.
func vdShareSnapshotOpen(rebuild bool) (*vdisk.SharedDiskManager, func(), error) {
	return vdShareSnapshotOpenWait(rebuild, 5*time.Second)
}

// vdShareSnapshotOpenWait is vdShareSnapshotOpen with its own bound on the lock wait: a reader that
// polls (the status snapshot) must not sit behind a stuck writer for seconds (AUD-82-F3).
func vdShareSnapshotOpenWait(rebuild bool, wait time.Duration) (*vdisk.SharedDiskManager, func(), error) {
	if !vdShareOn {
		// Nothing is shared in a build without the surface (vdShareShipped): the file is not read, and
		// the status never waits on its lock.
		return nil, func() {}, nil
	}
	p, e := statedir.Path("vdisk-shared.json")
	if e != nil {
		return nil, nil, e
	}
	unlock, e := statedir.Lock(p, wait)
	if e != nil {
		return nil, nil, e
	}
	m := vdisk.NewSharedDiskManager(p, nil)
	if e = m.Load(); e != nil {
		if !rebuild {
			unlock()
			return nil, nil, e
		}
		vdLogf("share snapshot: unreadable, rebuilt from the worker: %v", e)
		m = vdisk.NewSharedDiskManager(p, nil)
	}
	return m, unlock, nil
}

func vdShareStates(disks []fmsworker.SharedDiskInfo) []vdisk.SharedDiskState {
	out := make([]vdisk.SharedDiskState, 0, len(disks))
	for _, d := range disks {
		out = append(out, vdisk.SharedDiskState{ContainerID: d.ContainerID, ContainerPath: d.ContainerPath, RootName: d.RootName, Shared: true, Holder: vdHolderOf(d), State: vdSharedStateOf(d.State), ReadOnly: d.ReadOnly, Encrypted: d.Encrypted, Autostart: d.Autostart, HasStoredKey: d.HasStoredKey, MountPath: d.MountPath, OpenHandles: d.OpenHandles, SharedAt: d.SharedAt, LastOpened: d.LastOpened, LastClosed: d.LastClosed})
	}
	return out
}

// vdRefreshShareSnapshot makes the local cache equal to the worker's full list:
// a disk the worker no longer holds (unshared, or never known) leaves it too.
func vdRefreshShareSnapshot(client vdWorker) error {
	disks, e := client.ListSharedDisks()
	if e != nil {
		return e
	}
	m, unlock, e := vdShareSnapshotOpen(true)
	if e != nil {
		return e
	}
	defer unlock()
	if m == nil { // the surface is off in this build (vdShareShipped): there is no cache to refresh
		return vdShareOffError("the shared-disk snapshot")
	}
	return m.ReconcileAll(vdShareStates(disks))
}
