//go:build windows

package main

import (
	"errors"
	"filedo/fdsec"
	"filedo/fmsworker"
	"filedo/vdisk"
	"fmt"
	"strconv"
	"strings"
)

// `vd open` and `vd close` act on a disk the FMS worker holds (SP-0121 section 6, A18): the PC is where an
// encrypted shared disk is unlocked and where it is closed, never the phone. FileDO sends the request over
// the worker's pipe; the worker mounts through its own bundled filedo.exe and runs the drain. FileDO never
// mounts a shared disk itself (guarantee 10).

var vdOpenCredential = vdResolveCredential

// vdOpen is `vd open <container> [credential]`: the worker opens the shared disk, with the password for an
// encrypted one. The password goes over the local pipe and is never printed or logged.
func vdOpen(args []string, batch bool) error { return vdWorkerError(vdOpenRun(args, batch)) }
func vdOpenRun(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("vd open needs <container> [credential]")
	}
	var cred credArg
	for _, w := range args[1:] {
		if a, ok := credentialToken(w); ok && !cred.given() {
			cred = a
			continue
		}
		if strings.EqualFold(w, "stdin") && !cred.given() {
			cred = credArg{src: "stdin"}
			continue
		}
		return vdUsagef("unknown open option (want a credential: p:<password>, pf:<file>, pe:<VAR>, k:<keyfile> or stdin)")
	}
	path, e := vdSharePath(args[0], true)
	if e != nil {
		return e
	}
	client, e := getFMSWorkerClient()
	if e != nil {
		return e
	}
	st, e := client.GetDiskStatus(path)
	if e != nil {
		if vdNotShared(e) {
			return fmt.Errorf("share the container first (filedo vd share <container> on): %w", e)
		}
		return e
	}
	if st.State == fmsworker.DiskStateOpen {
		fmt.Printf("%s is already open as FMS root '%s'.\n", path, st.RootName)
		return nil
	}
	info, e := vdShareInspect(path)
	if e != nil {
		return e
	}
	if m, ok := vdFindMount(info.ContainerID); ok {
		return errBusy(fmt.Sprintf("%s is mounted by FileDO at %s; unmount it first, then open it for FMS", path, vdRowPlace(m)))
	}
	var password []byte
	if !info.Obfuscated {
		c, e := vdOpenCredential(cred, false)
		if e != nil {
			return e
		}
		password = []byte(c)
		defer clear(password)
		defer clear(c)
	} else if cred.given() {
		fmt.Println(vdCredentialNotUsed)
	}
	if e = client.OpenDisk(path, string(password)); e != nil {
		return e
	}
	vdLogf("open: %s", path)
	after, e := client.GetDiskStatus(path)
	if e != nil || after.State != fmsworker.DiskStateOpen {
		return fmt.Errorf("the holder accepted the open request, but its record does not show the disk open yet; check: filedo vd status")
	}
	fmt.Printf("Opened %s for FMS root '%s'.\n", path, after.RootName)
	vdSayHolderMode(client.WorkerMode())
	if e = vdRefreshShareSnapshot(client); e != nil {
		vdSnapshotWarn("opened", e)
	}
	return nil
}

// vdCloseRun parses `vd close <container> [wait <seconds>] [force]`.
func vdClose(args []string, batch bool) error { return vdWorkerError(vdCloseRun(args, batch)) }

// vdCloseConfirm is the one question of a close that phones are still using; a seam for the tests.
var vdCloseConfirm = vdConfirm

func vdCloseRun(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("vd close needs <container> [wait <seconds>] [force]")
	}
	force := false
	bound := fmsworker.DefaultDrainBoundSeconds
	for i := 1; i < len(args); i++ {
		switch strings.ToLower(args[i]) {
		case "force", "-y":
			force = true
		case "wait":
			if i+1 >= len(args) {
				return vdUsagef("wait needs a number of seconds (%d..%d)", fmsworker.MinDrainBoundSeconds, fmsworker.MaxDrainBoundSeconds)
			}
			i++
			n, e := strconv.Atoi(args[i])
			if e != nil || n < fmsworker.MinDrainBoundSeconds || n > fmsworker.MaxDrainBoundSeconds {
				return vdUsagef("wait needs a number of seconds (%d..%d)", fmsworker.MinDrainBoundSeconds, fmsworker.MaxDrainBoundSeconds)
			}
			bound = n
		default:
			return vdUsagef("unknown close option at word %d (want wait <seconds>, force)", i+1)
		}
	}
	path, e := vdSharePath(args[0], true)
	if e != nil {
		return vdWorkerError(e)
	}
	client, e := getFMSWorkerClient()
	if e != nil {
		return vdWorkerError(e)
	}
	st, e := client.GetDiskStatus(path)
	if e != nil {
		if vdNotShared(e) {
			return vdWorkerError(fmt.Errorf("%s is not shared with FMS for Windows, so there is nothing to close: %w", path, e))
		}
		return vdWorkerError(e)
	}
	if st.State == fmsworker.DiskStateClosed {
		fmt.Printf("%s is already closed.\n", path)
		return nil
	}
	if st.OpenHandles > 0 {
		fmt.Printf("%d open handle(s) on %s: a paired device is working with it. Waiting up to %d s for them to finish.\n", st.OpenHandles, path, bound)
	}
	e = client.CloseDiskBound(path, force, bound)
	if e != nil && !force && vdStillBusy(e) {
		// The drain ran out of time. Only a person may say that work in flight is expendable.
		fmt.Printf("The disk still has open files after %d s; %s\n", bound, vdWorkerMessage(e))
		if batch || !vdCloseConfirm("Close it anyway? Whatever those files were still writing is lost", batch) {
			return vdWorkerError(fmt.Errorf("%w: not closed; wait longer with wait <seconds>, or add force", vdisk.ErrBusy))
		}
		// The wait is spent already; one second is the shortest bound the worker takes.
		e = client.CloseDiskBound(path, true, fmsworker.MinDrainBoundSeconds)
		force = true
	}
	if e != nil {
		return e
	}
	vdLogf("close: %s force=%t", path, force)
	if force {
		fmt.Printf("Closed %s by force. It is marked as not closed cleanly; the next open reports it.\n", path)
	} else {
		fmt.Printf("Closed %s.\n", path)
	}
	if e = vdRefreshShareSnapshot(client); e != nil {
		vdSnapshotWarn("closed", e)
	}
	return nil
}

// vdStillBusy reports the worker's "open handles remain" refusal of a close.
func vdStillBusy(err error) bool {
	var we *fmsworker.WorkerError
	if errors.As(err, &we) {
		if we.OutcomeClass == vdisk.ExitBusy {
			return true
		}
		return strings.Contains(strings.ToLower(we.Message), "open handle")
	}
	return errors.Is(err, vdisk.ErrBusy)
}

func vdWorkerMessage(err error) string {
	var we *fmsworker.WorkerError
	if errors.As(err, &we) {
		return we.Message
	}
	return err.Error()
}

// vdSayHolderMode says which edition's worker holds the shared disks and what that means for availability
// (SP-0121 4.3 rule 1, A11): "always available" is the Server edition's, and everyone else must be told.
func vdSayHolderMode(m fmsworker.WorkerMode) {
	switch m {
	case fmsworker.WorkerModeService:
		fmt.Println("FMS for Windows holds shared disks as a service: they stay available with nobody signed in, until someone closes them or the PC stops.")
	case fmsworker.WorkerModeUser:
		fmt.Println("FMS for Windows holds shared disks in this sign-in: they are available only while you are signed in. Always-on sharing needs the FMS for Windows Server edition.")
	}
}

// vdAutostartWhen is "when" an autostarted disk opens, for the consent text: a session worker opens it at
// sign-in, not at startup (AUD-84-F10).
func vdAutostartWhen(m fmsworker.WorkerMode) string {
	switch m {
	case fmsworker.WorkerModeService:
		return "after the PC starts"
	case fmsworker.WorkerModeUser:
		return "after you sign in"
	}
	return "after startup, or after sign-in when FMS for Windows runs in a session"
}

// vdProveCredential proves a credential against a closed container with a read-only open, which writes
// nothing and answers a wrong credential as class 3 (AUD-84-F8); a seam for the tests.
var vdProveCredential = vdProveCredentialFile

// vdProveCredentialFile is the real proof.
func vdProveCredentialFile(path string, cred fdsec.Credential) error {
	c, err := vdisk.Open(vdContext(), path, cred, vdisk.OpenRead)
	if err != nil {
		return err
	}
	return c.Close()
}
