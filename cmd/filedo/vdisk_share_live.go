//go:build windows

package main

import (
	"context"
	"errors"
	"filedo/fmsworker"
	"filedo/vdisk"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// What FileDO knows about the disks it shares comes from one place: the worker. The local file
// (vdisk-shared.json) remembers which disks are shared and nothing about who holds them - holder, state,
// mount path and handle count do not survive a restart by design (vdisk.SharedDiskState.persisted) - so a
// surface that reads only the file reports every held disk as free (AUD-82-F2, AUD-86-F1, AUD-87-F6). The
// readers below ask the worker, bounded; when it does not answer they say the holder is unknown and never
// "none" (DISK-SHARE 18).

const (
	// vdShareSnapshotProbe bounds the status snapshot's question: the window gives the whole snapshot 8 s.
	vdShareSnapshotProbe = 1 * time.Second
	// vdShareSnapshotLock bounds the wait for the local file when the worker did not answer.
	vdShareSnapshotLock = 1 * time.Second
	// vdShareMountProbe bounds the mount guard's question (DISK-SHARE 19).
	vdShareMountProbe = 2 * time.Second
)

// vdShareLiveResult is the worker's answer about its disks. Err nil means Disks is the live, complete list.
type vdShareLiveResult struct {
	Disks []fmsworker.SharedDiskInfo
	Mode  fmsworker.WorkerMode
	Err   error
}

// vdShareProbe asks the worker for its disk list within limit. A seam: the tests answer for the worker.
var vdShareProbe = func(limit time.Duration) vdShareLiveResult {
	c := fmsworker.NewProbeClient()
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	mode, disks, err := c.Probe(ctx)
	return vdShareLiveResult{Disks: disks, Mode: mode, Err: err}
}

// vdSharedView is the shared disks as one surface shows them.
type vdSharedView struct {
	Disks []vdisk.SharedDiskState
	// Live: the worker answered, so holder, state and handle count are its own. Not live: the disks are
	// the local file's and those three fields are not known.
	Live bool
	// Known is false only when neither the worker nor the local file could be read: nothing can be said
	// about which disks are shared.
	Known        bool
	Mode         fmsworker.WorkerMode
	Reason       string // why the worker did not answer, for the console
	Availability string // stable, private GUI reason; never infers installation absence from a pipe
}

// vdSharedViewNow builds the view. In a build without the share surface nothing is shared and nothing is
// read (vdShareShipped).
func vdSharedViewNow() vdSharedView {
	if !vdShareOn {
		return vdSharedView{Known: true, Live: true}
	}
	r := vdShareProbe(vdShareSnapshotProbe)
	if r.Err == nil {
		return vdSharedView{Disks: vdShareStates(r.Disks), Live: true, Known: true, Mode: r.Mode, Availability: "ready"}
	}
	v := vdSharedView{Reason: vdProbeReason(r.Err), Availability: vdShareAvailability(r.Err)}
	sm, unlock, err := vdShareSnapshotOpenWait(false, vdShareSnapshotLock)
	if err != nil || sm == nil {
		if err != nil {
			vdLogf("share snapshot: neither the worker (%v) nor the local file (%v) could be read", r.Err, err)
		}
		return v
	}
	defer unlock()
	v.Disks, v.Known = sm.ListSharedDisks(), true
	return v
}

func vdShareAvailability(err error) string {
	switch {
	case err == nil:
		return "ready"
	case errors.Is(err, fmsworker.ErrUntrustedServer):
		return "untrusted"
	case errors.Is(err, fmsworker.ErrNotCapable):
		return "not-capable"
	case errors.Is(err, fmsworker.ErrWorkerUnavailable):
		return "unavailable"
	default:
		return "communication"
	}
}

// vdProbeReason is one short sentence for a worker that did not answer.
func vdProbeReason(err error) string {
	switch {
	case errors.Is(err, fmsworker.ErrUntrustedServer):
		return "the FMS worker pipe is served by a process FileDO cannot verify"
	case errors.Is(err, fmsworker.ErrNotCapable):
		return "the installed FMS for Windows cannot share disks"
	case errors.Is(err, fmsworker.ErrWorkerUnavailable):
		return "FMS for Windows is not running"
	case errors.Is(err, context.DeadlineExceeded):
		return "FMS for Windows did not answer in time"
	}
	return "FMS for Windows did not answer"
}

// vdHolderOf reads the worker's holder word. A word this build does not know is unknown, never free; an
// open or opening or closing disk with no holder named is not free either.
func vdHolderOf(d fmsworker.SharedDiskInfo) vdisk.DiskHolder {
	switch strings.ToLower(d.Holder) {
	case "service":
		return vdisk.HolderFMSService
	case "session":
		return vdisk.HolderFMSSession
	case "none", "":
		switch d.State {
		case fmsworker.DiskStateOpen, fmsworker.DiskStateOpening, fmsworker.DiskStateClosing:
			return vdisk.HolderUnknown
		}
		return vdisk.HolderNone
	}
	return vdisk.HolderUnknown
}

// vdSharedStateOf maps the worker's state; a state the worker did not send is unknown.
func vdSharedStateOf(s fmsworker.DiskState) vdisk.DiskState {
	if s == fmsworker.DiskStateUnknown {
		return vdisk.StateUnknown
	}
	return vdisk.DiskState(s)
}

// vdHeldByWorker reports a disk the FMS worker holds right now (or is opening or closing), by holder or
// by state. FileDO must not mount it (DISK-SHARE 17).
func vdHeldByWorker(d fmsworker.SharedDiskInfo) bool {
	switch vdHolderOf(d) {
	case vdisk.HolderFMSService, vdisk.HolderFMSSession:
		return true
	}
	switch d.State {
	case fmsworker.DiskStateOpen, fmsworker.DiskStateOpening, fmsworker.DiskStateClosing:
		return true
	}
	return false
}

// vdSameSharedDisk matches a worker record to a container by id, else by file identity.
func vdSameSharedDisk(d fmsworker.SharedDiskInfo, containerID, path string) bool {
	if containerID != "" && strings.EqualFold(d.ContainerID, containerID) {
		return true
	}
	if d.ContainerPath != "" && path != "" {
		if strings.EqualFold(filepath.Clean(d.ContainerPath), filepath.Clean(path)) {
			return true
		}
	}
	return false
}

// vdHolderWords is the console's sentence for who holds a disk and in which edition's worker.
func vdHolderWords(h vdisk.DiskHolder) string {
	switch h {
	case vdisk.HolderFMSService:
		return "the FMS for Windows service"
	case vdisk.HolderFMSSession:
		return "FMS for Windows (running in a signed-in session)"
	case vdisk.HolderFileDO:
		return "FileDO"
	case vdisk.HolderNone:
		return "nobody"
	}
	return "someone FileDO could not ask"
}

// vdMountHolderGuard is DISK-SHARE 17 and 19 at the mount: a disk the worker holds is refused, naming the
// holder and the root; a disk that is shared but closed is mounted with a warning that FMS cannot open it
// meanwhile. A worker that cannot be asked is not "free": the mount goes on the container lock alone.
func vdMountHolderGuard(info vdisk.Info, path string) error {
	if !vdShareOn {
		return nil
	}
	r := vdShareProbe(vdShareMountProbe)
	if r.Err != nil {
		return nil
	}
	for _, d := range r.Disks {
		if !vdSameSharedDisk(d, info.ContainerID, path) {
			continue
		}
		if vdHeldByWorker(d) {
			return errBusy(fmt.Sprintf("%s is held by %s and shared as '%s'; close it there, or run: filedo %s share off", path, vdHolderWords(vdHolderOf(d)), d.RootName, vdQuoteArg(path)))
		}
		fmt.Printf("Note: %s is shared with FMS for Windows as '%s'. While FileDO has it mounted, FMS cannot open it, and paired devices see it as an unavailable folder.\n", path, d.RootName)
		return nil
	}
	return nil
}

// vdQuoteArg quotes a path for a command line the user may copy: only when it has a space.
func vdQuoteArg(p string) string {
	if strings.ContainsAny(p, " \t") {
		return `"` + p + `"`
	}
	return p
}
