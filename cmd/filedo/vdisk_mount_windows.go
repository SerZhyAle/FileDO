//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"filedo/fdsec"
	"filedo/statedir"
	"filedo/vdisk"
)

// The life of a mount, in three processes (plan S3 sections 5 and 5.1):
//
//  1. The command the user typed (not elevated, usually). It checks the
//     container, starts the block server, asks Windows for consent once, and
//     records the mount in vdisk-state.json.
//  2. The block server: `filedo vd _serve`, started detached (no console, its
//     own process group, out of the caller's job) so that closing the console
//     or pressing Ctrl+C does not pull the volume. It is never elevated - a
//     socket owned by an elevated process would turn every protocol bug into
//     an elevation bug - and it refuses to serve if it finds itself elevated.
//     It exits when told to by its stop file, or by itself when the initiator
//     never arrives or has gone for good.
//  3. The elevated step: `filedo vd _attach`, `_detach`, `_format` or `_task`, started through
//     the consent prompt with a hidden window. It configures the initiator and
//     the disk and nothing else: it starts no server and listens on nothing.
//
// Unmount order (S0 requirement 2): lock and dismount the volume, take the
// disk offline, log the session out with a bounded retry, remove the portal,
// and only then tell the server to close the container - which is when the
// clean marker is set.

// ---------------------------------------------------------------- internal entry

// vdInternalMain runs `vd _serve|_attach|_detach` before main() opens a run:
// the internal steps write no history and no events of their own; the command
// that started them does.
func vdInternalMain(args []string) int {
	var err error
	switch args[0] {
	case "_serve":
		err = vdServeMain(args[1:])
	case "_attach", "_detach", "_task", "_image", "_flush", "_format":
		err = vdElevatedMain(args[0], args[1:])
	default:
		err = vdUsagef("unknown internal verb %q", args[0])
	}
	if err != nil {
		vdLogf("%s failed: %v", args[0], err)
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return vdExitClass(err)
	}
	return 0
}

func isVdInternal(args []string) bool {
	return len(args) >= 3 && contains(list_of_flags_for_vd, strings.ToLower(args[1])) && strings.HasPrefix(args[2], "_")
}

// ---------------------------------------------------------------- the block server process

// vdHandoff is what the server tells the mounting command once it listens.
// It carries the CHAP secret; it lives in the per-user state root and is
// removed as soon as the elevated step has read it.
type vdHandoff struct {
	PID         int    `json:"pid"`
	Port        int    `json:"port"`
	IQN         string `json:"iqn"`
	Secret      string `json:"secret"`
	Serial      string `json:"serial"`
	Label       string `json:"label"`
	ContainerID string `json:"container_id"`
	Error       string `json:"error,omitempty"`
	Class       int    `json:"class,omitempty"`
}

const (
	vdAttachGrace = 5 * time.Minute  // the initiator must log in within this, or the server exits
	vdGoneGrace   = 60 * time.Second // a session gone this long means the disk was disconnected outside FileDO
)

func vdServeMain(args []string) error {
	if len(args) < 2 {
		return vdUsagef("_serve <container> <handoff> [ro] [cred]")
	}
	path, handoffPath := args[0], args[1]
	ro, withCred := false, false
	for _, w := range args[2:] {
		switch w {
		case "ro":
			ro = true
		case vdServeCredWord:
			withCred = true
		}
	}
	stopPath := strings.TrimSuffix(handoffPath, ".handoff.json") + ".stop"
	fail := func(err error) error {
		b, _ := json.Marshal(vdHandoff{PID: os.Getpid(), Error: err.Error(), Class: vdExitClass(err)})
		statedir.WriteFileAtomic(handoffPath, b, 0o600)
		return err
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		return fail(errTransport("the block server refuses to run elevated"))
	}
	vdWriterStamp()
	mode := vdisk.OpenMount
	if ro {
		mode = vdisk.OpenRead
	}
	var cred fdsec.Credential
	if withCred {
		var err error
		if cred, err = vdReadServeCredential(); err != nil {
			return fail(err)
		}
	}
	c, err := vdisk.Open(context.Background(), path, cred, mode)
	clear(cred)
	if err != nil {
		return fail(err)
	}
	info := c.Info()
	secret, err := vdisk.NewChapSecret()
	if err != nil {
		c.Close()
		return fail(err)
	}
	iqn := vdIQN(info.ContainerID)
	logf := func(f string, a ...interface{}) { vdLogf("server %s: %s", info.ContainerID, fmt.Sprintf(f, a...)) }
	ram := info.Profile == vdisk.ProfileRAM && !ro
	srv, err := vdisk.NewServer(vdisk.TargetConfig{
		IQN: iqn, Device: c, Size: info.LogicalSize, PhysicalBlock: int(info.SectorSize),
		ReadOnly: ro, Identity: info.ContainerID, ChapSecret: secret, WriteBack: ram, Logf: logf,
	})
	if err != nil {
		c.Close()
		return fail(err)
	}
	port := srv.Addr().Port
	vdLogf("bind 127.0.0.1:%d container %s profile %s read-only=%v elevated=%v path %s", port, info.ContainerID, info.Profile, ro, windows.GetCurrentProcessToken().IsElevated(), path)
	label := info.FriendlyName
	b, _ := json.Marshal(vdHandoff{PID: os.Getpid(), Port: port, IQN: iqn, Secret: secret,
		Serial: vdisk.DiskSerial(info.ContainerID), Label: label, ContainerID: info.ContainerID})
	os.Remove(stopPath)
	if err := statedir.WriteFileAtomic(handoffPath, b, 0o600); err != nil {
		srv.Close()
		c.Close()
		return err
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()

	// A ram container saves by its policy while it is served; the loop below
	// also answers `save` requests and writes the status `vd status` shows.
	policyCtx, stopPolicy := context.WithCancel(context.Background())
	policyDone := make(chan struct{})
	savePath, _ := vdSidePath(info.ContainerID, ".save")
	savedPath, _ := vdSidePath(info.ContainerID, ".saved.json")
	statusPath, _ := vdSidePath(info.ContainerID, ".status.json")
	if ram {
		go func() {
			vdisk.RunSavePolicy(policyCtx, c, vdisk.DefaultSavePolicy, logf)
			close(policyDone)
		}()
	} else {
		close(policyDone)
	}
	var lastStatus time.Time
	writeStatus := func() {
		s, _ := c.RAMState()
		b, _ := json.Marshal(vdRAMStatus{At: time.Now(), DirtyBytes: s.DirtyBytes, Saving: s.Saving, LastGoodSave: s.LastGoodSave})
		statedir.WriteFileAtomic(statusPath, b, 0o600)
		lastStatus = time.Now()
	}

	started := time.Now()
	var seen bool
	var lastActive time.Time
	how := "clean"
	reason := ""
loop:
	for {
		select {
		case err := <-done:
			reason = fmt.Sprintf("listener failed: %v", err)
			how = "unclean"
			break loop
		case <-time.After(250 * time.Millisecond):
		}
		if b, err := os.ReadFile(stopPath); err == nil {
			switch strings.TrimSpace(string(b)) {
			case "unclean":
				how = "unclean"
			case "nosave":
				how = "nosave"
			}
			reason = "stop requested"
			break
		}
		if ram {
			if req, err := os.ReadFile(savePath); err == nil {
				os.Remove(savePath)
				began := time.Now()
				s, _ := c.RAMState()
				ans := vdSaved{Request: strings.TrimSpace(string(req)), At: time.Now()}
				if err := c.Save(); err != nil {
					ans.Error, ans.Class = err.Error(), vdExitClass(err)
					logf("ram: save on request failed: %v", err)
				} else {
					logf("ram: saved %d MB on request in %d ms", s.DirtyBytes>>20, time.Since(began).Milliseconds())
				}
				ans.At = time.Now()
				b, _ := json.Marshal(ans)
				statedir.WriteFileAtomic(savedPath, b, 0o600)
				writeStatus()
			}
			if time.Since(lastStatus) >= 2*time.Second {
				writeStatus()
			}
		}
		if srv.SessionActive() {
			seen, lastActive = true, time.Now()
		} else if !seen && time.Since(started) > vdAttachGrace {
			reason = "the initiator never logged in"
			break
		} else if seen && time.Since(lastActive) > vdGoneGrace {
			reason = "the initiator session ended outside FileDO"
			break
		}
	}
	srv.Close()
	stopPolicy()
	<-policyDone
	var cerr error
	// Only a volume the initiator actually had can have been pulled away
	// from its file system; a server nobody logged in to closes cleanly.
	switch {
	case how == "nosave":
		s, _ := c.RAMState()
		logf("ram: unmount nosave drops %d MB written since the save of %s", s.DirtyBytes>>20, vdTime(s.LastGoodSave))
		cerr = c.Discard()
	case how == "unclean" && seen:
		cerr = c.Abandon()
	default:
		cerr = c.Close()
	}
	for _, p := range []string{handoffPath, stopPath, savePath, savedPath, statusPath} {
		os.Remove(p)
	}
	vdLogf("server exit 127.0.0.1:%d container %s: %s, closed %s, close error %v", port, info.ContainerID, reason, how, cerr)
	return cerr
}

// ---------------------------------------------------------------- the elevated step

// vdRequest is what the mounting or unmounting command hands the elevated
// step; vdResult is its answer.
type vdRequest struct {
	Port     int    `json:"port"`
	IQN      string `json:"iqn"`
	Secret   string `json:"secret,omitempty"`
	Serial   string `json:"serial"`
	Label    string `json:"label"`
	Letter   string `json:"letter,omitempty"`
	ReadOnly bool   `json:"read_only"`
	Force    bool   `json:"force"`
	// NeverHeld is the caller's fact about the container: no data cluster was
	// ever allocated, so a disk with no partition table is a container that has
	// never been formatted. Only then may the mount format it (AUD-32-F3).
	NeverHeld bool   `json:"never_held,omitempty"`
	Session   string `json:"session,omitempty"`
	// NoScan names the backing file to exclude from Microsoft Defender for
	// this mount (the opt-in word noscan), or to un-exclude at unmount.
	NoScan string `json:"no_scan,omitempty"`
	// TaskName and TaskSID (or TaskDelete) name the automatic-mount task the
	// _task step registers or removes (vdisk_auto_windows.go). They are data:
	// the step builds the task itself from the registered container of that
	// name, its own executable and TaskSID, never from XML in this file
	// (AUD-31-F4).
	TaskName   string `json:"task_name,omitempty"`
	TaskSID    string `json:"task_sid,omitempty"`
	TaskDelete bool   `json:"task_delete,omitempty"`
	// ImagePath is the .vhd or .vhdx the _image step mounts, or dismounts
	// when ImageDetach is set.
	ImagePath   string `json:"image_path,omitempty"`
	ImageDetach bool   `json:"image_detach,omitempty"`
	// FormatOnly is the _format step: attach, clear the disk, format it with
	// FS ("NTFS" or "exFAT") and Label, and detach again - no drive letter.
	FormatOnly bool   `json:"format_only,omitempty"`
	FS         string `json:"fs,omitempty"`
	// StateDir is the caller's state root. A process started through the
	// consent prompt gets a fresh environment, so an overridden root would
	// otherwise not reach it and its log lines would land elsewhere.
	StateDir string `json:"state_dir,omitempty"`
}

type vdResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Class     int    `json:"class,omitempty"`
	Letter    string `json:"letter,omitempty"`
	Session   string `json:"session,omitempty"`
	Formatted bool   `json:"formatted,omitempty"`
	Unclean   bool   `json:"unclean,omitempty"`
	Holder    string `json:"holder,omitempty"`
	// Excluded: this mount added the Defender exclusion and its unmount
	// removes it. Warning: a step that did not work but does not fail the mount.
	Excluded bool   `json:"excluded,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

func vdElevatedMain(verb string, args []string) error {
	if len(args) < 2 {
		return vdUsagef("%s <request> <result>", verb)
	}
	reqPath, resPath := args[0], args[1]
	b, err := os.ReadFile(reqPath)
	os.Remove(reqPath) // it may carry the CHAP secret
	if err != nil {
		return err
	}
	var req vdRequest
	if err := json.Unmarshal(b, &req); err != nil {
		return err
	}
	if req.StateDir != "" {
		os.Setenv(statedir.EnvOverride, req.StateDir)
	}
	var res vdResult
	switch verb {
	case "_attach":
		res, err = vdAttach(req, resPath+".cancel")
	case "_task":
		res, err = vdTaskStep(req)
	case "_image":
		res, err = vdImageStep(req)
	case "_flush":
		res, err = vdFlushStep(req)
	case "_format":
		res, err = vdFormatStep(req, resPath+".cancel")
	default:
		res, err = vdDetach(req)
	}
	if err != nil {
		res.OK, res.Error, res.Class = false, err.Error(), vdExitClass(err)
	} else {
		res.OK = true
	}
	out, _ := json.Marshal(res)
	// CREATE_NEW: the caller never creates the result file, so one that is
	// already there is somebody else's, and a reparse point planted at that
	// name must not be written through by an elevated process (AUD-31-F4).
	f, ferr := os.OpenFile(resPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if ferr != nil {
		return ferr
	}
	if _, werr := f.Write(out); werr != nil {
		f.Close()
		return werr
	}
	return f.Close()
}

// vdEnsureInitiator starts MSiSCSI when it is stopped. It never changes the
// service's start type: that stays whatever the administrator set.
func vdEnsureInitiator() error {
	m, err := mgr.Connect()
	if err != nil {
		return errTransport("cannot reach the service manager: " + err.Error())
	}
	defer m.Disconnect()
	s, err := m.OpenService("MSiSCSI")
	if err != nil {
		return errTransport("the Microsoft iSCSI Initiator service is not installed: " + err.Error())
	}
	defer s.Close()
	cfg, err := s.Config()
	if err == nil && cfg.StartType == mgr.StartDisabled {
		return errTransport("the Microsoft iSCSI Initiator service (MSiSCSI) is disabled")
	}
	st, err := s.Query()
	if err != nil {
		return errTransport("cannot query MSiSCSI: " + err.Error())
	}
	if st.State == svc.Running {
		return nil
	}
	vdLogf("starting MSiSCSI (it was not running)")
	if err := s.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return errTransport("could not start MSiSCSI: " + err.Error())
	}
	for deadline := time.Now().Add(60 * time.Second); time.Now().Before(deadline); time.Sleep(250 * time.Millisecond) {
		if st, err := s.Query(); err == nil && st.State == svc.Running {
			return nil
		}
	}
	return errTransport("MSiSCSI did not start within 60 s")
}

// vdTransportState is whether a mount could reach the Windows iSCSI
// initiator now, for the snapshot of `vd status json` (SP-0063 8.1). It is
// read without administrator rights: the service manager is opened for
// connect only and the service for its status and configuration, never for
// control (mgr.Connect asks for every right, which a user does not have). A
// stopped service is ready - a mount starts it, as vdEnsureInitiator does; a
// disabled or missing one is not.
func vdTransportState() vdSnapTransport {
	h, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return vdSnapTransport{InitiatorService: "unknown", Reason: "service_manager"}
	}
	defer windows.CloseServiceHandle(h)
	name, _ := windows.UTF16PtrFromString("MSiSCSI")
	sh, err := windows.OpenService(h, name, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
			return vdSnapTransport{InitiatorService: "missing", Reason: "initiator_missing"}
		}
		return vdSnapTransport{InitiatorService: "unknown", Reason: "service_manager"}
	}
	s := &mgr.Service{Name: "MSiSCSI", Handle: sh}
	defer s.Close()
	if cfg, err := s.Config(); err == nil && cfg.StartType == mgr.StartDisabled {
		return vdSnapTransport{InitiatorService: "disabled", Reason: "initiator_disabled"}
	}
	st, err := s.Query()
	switch {
	case err != nil:
		return vdSnapTransport{Ready: true, InitiatorService: "unknown"}
	case st.State == svc.Running:
		return vdSnapTransport{Ready: true, InitiatorService: "running"}
	}
	return vdSnapTransport{Ready: true, InitiatorService: "stopped"}
}

func vdAttach(req vdRequest, cancelPath string) (res vdResult, err error) {
	step := func(format string, a ...interface{}) error {
		vdLogf("attach %s: %s", req.IQN, fmt.Sprintf(format, a...))
		if _, e := os.Stat(cancelPath); e == nil {
			return fmt.Errorf("%w: the mount was interrupted", vdisk.ErrStopped)
		}
		return nil
	}
	disk := -1
	var sid iscsiSessionID
	loggedIn, portal := false, false
	defer func() {
		if err == nil {
			return
		}
		// Roll back everything this step did, in the unmount order.
		vdLogf("attach %s failed, rolling back: %v", req.IQN, err)
		if disk >= 0 {
			setDiskOffline(disk)
		}
		if loggedIn {
			vdLogoutRetry(sid, 5)
		}
		if portal {
			iscsiRemovePortal(req.Port)
		}
		if res.Excluded {
			if _, e := defenderExclude(req.NoScan, false); e != nil {
				vdLogf("attach %s: removing the Defender exclusion: %v", req.IQN, e)
			}
		}
	}()
	if err = step("checking the initiator service"); err != nil {
		return res, err
	}
	if err = vdEnsureInitiator(); err != nil {
		return res, err
	}
	if err = iscsiLoad(); err != nil {
		return res, errTransport(err.Error())
	}
	if req.NoScan != "" {
		if err = step("excluding %s from Microsoft Defender", req.NoScan); err != nil {
			return res, err
		}
		present, xerr := defenderExclude(req.NoScan, true)
		switch {
		case xerr != nil:
			// Another antivirus, or Defender managed by policy: the mount
			// still works, only slower, so this is a warning and not a failure.
			res.Warning = xerr.Error()
			vdLogf("attach %s: %v", req.IQN, xerr)
		case present:
			vdLogf("attach %s: the Defender exclusion was already there; it stays after unmount", req.IQN)
		default:
			res.Excluded = true
		}
	}
	if err = step("adding portal 127.0.0.1:%d", req.Port); err != nil {
		return res, err
	}
	if err = iscsiAddPortal(req.Port); err != nil {
		return res, vdIscsiErr(err)
	}
	portal = true
	if err = step("logging in"); err != nil {
		return res, err
	}
	if sid, err = iscsiLogin(req.IQN, req.Port, req.Secret); err != nil {
		return res, vdIscsiErr(err)
	}
	loggedIn = true
	res.Session = sid.String()
	if err = step("session %s; waiting for the disk %s", sid, req.Serial); err != nil {
		return res, err
	}
	if disk, err = findDiskBySerial(req.Serial, 90*time.Second); err != nil {
		return res, err
	}
	if err = step("disk %d; bringing it online", disk); err != nil {
		return res, err
	}
	if err = setDiskOnline(disk, req.ReadOnly); err != nil {
		return res, fmt.Errorf("could not bring disk %d online: %w", disk, err)
	}
	blank, err := diskIsBlank(disk)
	if err != nil {
		return res, fmt.Errorf("could not read the partition table of disk %d: %w", disk, err)
	}
	if err := vdMayFormatOnMount(blank, req.FormatOnly, req.NeverHeld, disk); err != nil {
		return res, err
	}
	if blank || req.FormatOnly {
		if req.ReadOnly {
			return res, vdUsagef("the container has never been formatted; mount it once without ro")
		}
		fs := vdFileSystem(req.FS)
		if err = step("formatting the volume %s (blank disk: %v)", fs, blank); err != nil {
			return res, err
		}
		if err = formatBlankDisk(disk, req.Serial, req.Label, fs, !blank, vdLogf); err != nil {
			return res, err
		}
		res.Formatted = true
	}
	if err = step("waiting for the volume"); err != nil {
		return res, err
	}
	vol, err := waitVolumeOnDisk(disk, 60*time.Second)
	if err != nil {
		return res, err
	}
	if res.Formatted {
		if ierr := setNotIndexed(vol); ierr != nil {
			vdLogf("attach %s: could not turn indexing off on the new volume: %v", req.IQN, ierr)
		}
	}
	if req.FormatOnly {
		// format gives no drive letter: the step that follows detaches.
		vdLogf("attach %s: formatted %s for format", req.IQN, vol)
		return res, nil
	}
	if res.Letter, err = assignLetter(vol, req.Letter); err != nil {
		return res, err
	}
	// A stop that arrived during the last step still wins: the mounting
	// command may already be gone, and a volume nobody recorded is the one
	// outcome an interrupted mount must never leave (plan S3 T3.10).
	if err = step("mounted at %s", res.Letter); err != nil {
		return res, err
	}
	vdLogf("attach %s: mounted %s at %s", req.IQN, vol, res.Letter)
	return res, nil
}

// vdIscsiErr classifies a discovery API failure: the service not running is
// the transport, class 7; everything else is an I/O failure with the API's text.
func vdIscsiErr(err error) error {
	switch iscsiStatusOf(err) {
	case isdscServiceNotRunning:
		return errTransport(err.Error())
	}
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || iscsiStatusOf(err) == 5 {
		return errTransport("the initiator refused a standard token; mounting needs administrator rights: " + err.Error())
	}
	return err
}

// vdLogoutRetry logs a session out, retrying a refusal: a volume that was
// just written vetoes the removal for a few seconds (S0 section 4.6, R4).
func vdLogoutRetry(sid iscsiSessionID, attempts int) error {
	var err error
	for i := 0; i < attempts; i++ {
		err = iscsiLogout(sid)
		if err == nil || iscsiStatusOf(err) == isdscInvalidSessionID {
			return nil
		}
		vdLogf("logout %s attempt %d: %v", sid, i+1, err)
		time.Sleep(2 * time.Second)
	}
	return err
}

func vdDetach(req vdRequest) (res vdResult, err error) {
	if err = iscsiLoad(); err != nil {
		return res, errTransport(err.Error())
	}
	if err = vdEnsureInitiator(); err != nil {
		return res, err
	}
	disk, derr := findDiskBySerial(req.Serial, 3*time.Second)
	if derr == nil {
		vols, _ := volumesOnDisk(disk)
		for _, v := range vols {
			locked, lerr := lockAndDismount(v, req.Force)
			if lerr != nil {
				return res, lerr
			}
			if !locked {
				res.Unclean = true
			}
		}
		vdLogf("detach %s: disk %d dismounted; taking it offline", req.IQN, disk)
		if err := setDiskOffline(disk); err != nil {
			vdLogf("detach %s: offline failed: %v", req.IQN, err)
		}
	} else {
		vdLogf("detach %s: no disk (%v); cleaning the session only", req.IQN, derr)
	}
	sessions, serr := iscsiSessionsFor(req.IQN)
	if serr != nil {
		return res, vdIscsiErr(serr)
	}
	if id, ok := parseSessionID(req.Session); ok {
		have := false
		for _, s := range sessions {
			have = have || s == id
		}
		if !have {
			sessions = append(sessions, id)
		}
	}
	for _, s := range sessions {
		if lerr := vdLogoutRetry(s, 5); lerr != nil {
			res.Holder = pnpVetoHolder()
			holder := "Windows refused to remove the disk"
			if res.Holder != "" {
				holder = res.Holder + " holds the disk open"
			}
			if !req.Force {
				// Nothing detached: bring the volume back as it was.
				if derr == nil {
					setDiskOnline(disk, req.ReadOnly)
				}
				return res, errBusy(holder + " (close it and unmount again, or use unmount force)")
			}
			vdLogf("detach %s: logout refused under force (%s); the server will be stopped under the disk", req.IQN, holder)
			res.Unclean = true
		}
	}
	if perr := iscsiRemovePortal(req.Port); perr != nil {
		vdLogf("detach %s: portal 127.0.0.1:%d: %v", req.IQN, req.Port, perr)
	}
	if req.NoScan != "" {
		if _, xerr := defenderExclude(req.NoScan, false); xerr != nil {
			res.Warning = xerr.Error()
			vdLogf("detach %s: %v", req.IQN, xerr)
		}
	}
	vdLogf("detach %s: done, unclean=%v", req.IQN, res.Unclean)
	return res, nil
}

// ---------------------------------------------------------------- running the elevated step

var (
	shell32            = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteEx = shell32.NewProc("ShellExecuteExW")
)

type shellExecuteInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

const (
	seeMaskNoCloseProcess = 0x00000040
	seeMaskNoAsync        = 0x00000100
	seeMaskFlagNoUI       = 0x00000400
)

// vdRunElevated runs `filedo vd <verb> <request> <result>` with an elevated
// token and returns its result. An elevated caller runs it in-process; a
// standard one asks Windows for consent - never silently, and never from a
// batch, where nobody is watching for the prompt (spec 5.6).
func vdRunElevated(verb string, req vdRequest, batch bool, onStart func(cancel string)) (vdResult, error) {
	d, err := statedir.Dir()
	if err != nil {
		return vdResult{}, err
	}
	tag := fmt.Sprintf("vd-%d-%d", os.Getpid(), time.Now().UnixNano())
	reqPath := d + `\` + tag + ".request.json"
	resPath := d + `\` + tag + ".result.json"
	defer os.Remove(reqPath)
	defer os.Remove(resPath)
	defer os.Remove(resPath + ".cancel")
	if onStart != nil {
		onStart(resPath + ".cancel")
	}
	req.StateDir = d
	b, _ := json.Marshal(req)
	if err := os.WriteFile(reqPath, b, 0o600); err != nil {
		return vdResult{}, err
	}
	if windows.GetCurrentProcessToken().IsElevated() {
		if err := vdElevatedMain(verb, []string{reqPath, resPath}); err != nil {
			return vdResult{}, err
		}
	} else {
		if batch {
			return vdResult{}, errTransport("this step needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
		}
		exe, err := os.Executable()
		if err != nil {
			return vdResult{}, err
		}
		params := strings.Join([]string{"vd", verb, syscall.EscapeArg(reqPath), syscall.EscapeArg(resPath)}, " ")
		info := &shellExecuteInfo{
			fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
			lpVerb:       windows.StringToUTF16Ptr("runas"),
			lpFile:       windows.StringToUTF16Ptr(exe),
			lpParameters: windows.StringToUTF16Ptr(params),
			nShow:        windows.SW_HIDE,
		}
		info.cbSize = uint32(unsafe.Sizeof(*info))
		if r, _, e := procShellExecuteEx.Call(uintptr(unsafe.Pointer(info))); r == 0 {
			if errors.Is(e, windows.ERROR_CANCELLED) {
				return vdResult{}, errTransport("administrator consent was not given; nothing was changed")
			}
			return vdResult{}, errTransport("could not start the elevated step: " + e.Error())
		}
		defer windows.CloseHandle(info.hProcess)
		windows.WaitForSingleObject(info.hProcess, windows.INFINITE)
	}
	out, err := os.ReadFile(resPath)
	if err != nil {
		return vdResult{}, fmt.Errorf("the elevated step left no result (see vdisk.log in the state folder): %w", err)
	}
	var res vdResult
	if err := json.Unmarshal(out, &res); err != nil {
		return vdResult{}, err
	}
	if !res.OK {
		return res, vdClassError(res.Class, res.Error)
	}
	return res, nil
}

// vdClassError rebuilds an error of the given class from another process.
func vdClassError(class int, msg string) error {
	var s error
	switch class {
	case vdExitTransport:
		s = errVdTransport
	case vdisk.ExitBusy:
		s = vdisk.ErrBusy
	case vdisk.ExitUsage:
		s = vdisk.ErrUsage
	case vdisk.ExitDamaged:
		s = vdisk.ErrDamaged
	case vdisk.ExitUnsupported:
		s = vdisk.ErrUnsupported
	case vdisk.ExitCredential:
		s = vdisk.ErrCredential
	case vdisk.ExitStopped:
		s = vdisk.ErrStopped
	default:
		s = vdisk.ErrIO
	}
	return &vdRemoteError{msg: msg, class: s}
}

type vdRemoteError struct {
	msg   string
	class error
}

func (e *vdRemoteError) Error() string        { return e.msg }
func (e *vdRemoteError) Is(target error) bool { return target == e.class }

// ---------------------------------------------------------------- starting the server

const (
	createNewProcessGroup  = 0x00000200
	detachedProcess        = 0x00000008
	createBreakawayFromJob = 0x01000000
	createUnicodeEnv       = 0x00000400
)

var (
	advapi32                  = windows.NewLazySystemDLL("advapi32.dll")
	procCreateRestrictedToken = advapi32.NewProc("CreateRestrictedToken")
)

// vdStartServer starts the block server detached and unelevated, and returns
// its pid. From an elevated caller the server gets a restricted copy of the
// caller's token with the administrator group disabled and medium integrity.
//
// For an encrypted container the credential goes down an anonymous pipe that
// is the server's stdin - never its command line, which any process of this
// user can read and process-creation auditing records, and never its
// environment, which every child would inherit. The pipe's read end is the
// one handle the server inherits; the write end is written, then closed.
func vdStartServer(path, handoff string, ro, encrypted bool, cred []byte) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	argv := []string{exe, "vd", "_serve", path, handoff}
	if ro {
		argv = append(argv, "ro")
	}
	var credRead, credWrite windows.Handle
	if encrypted {
		argv = append(argv, vdServeCredWord)
		if len(cred) > vdMaxCredential {
			return 0, vdUsagef("the credential is longer than %d bytes", vdMaxCredential)
		}
		// Both ends are created non-inheritable, and only the read end is
		// made inheritable: the write end is never inheritable, even for an
		// instant another thread's CreateProcess could catch.
		if err := windows.CreatePipe(&credRead, &credWrite, nil, vdMaxCredential); err != nil {
			return 0, fmt.Errorf("could not open the credential pipe: %w", err)
		}
		defer windows.CloseHandle(credRead)
		if err := windows.SetHandleInformation(credRead, windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT); err != nil {
			windows.CloseHandle(credWrite)
			return 0, err
		}
		var n uint32
		werr := windows.WriteFile(credWrite, cred, &n, nil)
		windows.CloseHandle(credWrite)
		if werr != nil || int(n) != len(cred) {
			return 0, fmt.Errorf("could not hand the credential to the block server: %v", werr)
		}
	}
	quoted := make([]string, len(argv))
	for i, a := range argv {
		quoted[i] = syscall.EscapeArg(a)
	}
	cmdLine, _ := windows.UTF16PtrFromString(strings.Join(quoted, " "))
	exeW, _ := windows.UTF16PtrFromString(exe)
	six := &windows.StartupInfoEx{}
	si := &six.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(*si))
	inherit := false
	var extFlags uint32
	if encrypted {
		// Inheritance limited to the pipe's read end: without the handle
		// list the server would also inherit whatever inheritable handles
		// this process holds - the GUI's output pipes among them - and keep
		// them open for the life of the mount.
		al, err := windows.NewProcThreadAttributeList(1)
		if err != nil {
			return 0, err
		}
		defer al.Delete()
		if err := al.Update(windows.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&credRead), unsafe.Sizeof(credRead)); err != nil {
			return 0, err
		}
		six.ProcThreadAttributeList = al.List()
		si.Cb = uint32(unsafe.Sizeof(*six))
		si.Flags |= windows.STARTF_USESTDHANDLES
		si.StdInput = credRead
		inherit = true
		extFlags = windows.EXTENDED_STARTUPINFO_PRESENT
	}
	var pi windows.ProcessInformation

	var token windows.Token
	if windows.GetCurrentProcessToken().IsElevated() {
		token, err = vdUnelevatedToken()
		if err != nil {
			return 0, errTransport("could not derive an unelevated token for the block server: " + err.Error())
		}
		defer token.Close()
	}
	start := func(flags uint32) error {
		if token != 0 {
			return windows.CreateProcessAsUser(token, exeW, cmdLine, nil, nil, inherit, flags, nil, nil, si, &pi)
		}
		return windows.CreateProcess(exeW, cmdLine, nil, nil, inherit, flags, nil, nil, si, &pi)
	}
	flags := uint32(detachedProcess|createNewProcessGroup) | extFlags
	// Out of the caller's job, so closing the GUI's window does not end the
	// server (plan S3 section 5); a job that forbids it keeps the server in.
	if err = start(flags | createBreakawayFromJob); err != nil {
		err = start(flags)
	}
	if err != nil {
		return 0, fmt.Errorf("could not start the block server: %w", err)
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return int(pi.ProcessId), nil
}

func vdUnelevatedToken() (windows.Token, error) {
	var self windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(),
		windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_ADJUST_DEFAULT, &self); err != nil {
		return 0, err
	}
	defer self.Close()
	const disableMaxPrivilege, luaToken = 0x1, 0x4
	var restricted windows.Token
	r, _, e := procCreateRestrictedToken.Call(uintptr(self), disableMaxPrivilege|luaToken, 0, 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&restricted)))
	if r == 0 {
		return 0, e
	}
	medium, err := windows.CreateWellKnownSid(windows.WinMediumLabelSid)
	if err != nil {
		restricted.Close()
		return 0, err
	}
	tml := windows.Tokenmandatorylabel{Label: windows.SIDAndAttributes{Sid: medium, Attributes: windows.SE_GROUP_INTEGRITY}}
	if err := windows.SetTokenInformation(restricted, windows.TokenIntegrityLevel,
		(*byte)(unsafe.Pointer(&tml)), uint32(unsafe.Sizeof(tml))+windows.GetLengthSid(medium)); err != nil {
		restricted.Close()
		return 0, err
	}
	return restricted, nil
}

// vdProcessStart returns a process's creation time (so a recycled pid is not
// taken for the server) and whether it still runs.
func vdProcessStart(pid int) (int64, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return 0, false
	}
	defer windows.CloseHandle(h)
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return 0, false
	}
	ev, _ := windows.WaitForSingleObject(h, 0)
	return c.Nanoseconds(), ev == uint32(windows.WAIT_TIMEOUT)
}

func vdServerAlive(m vdMountRow) bool {
	started, alive := vdProcessStart(m.ServerPID)
	return alive && started == m.ServerStarted
}

func vdWaitExit(pid int, timeout time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, uint32(timeout/time.Millisecond))
	return ev == windows.WAIT_OBJECT_0
}

// ---------------------------------------------------------------- mount

// vdMountOpts is a parsed mount line. NoScan is the opt-in word noscan: the
// backing file is excluded from Microsoft Defender while it is mounted.
type vdMountOpts struct {
	Path     string
	Letter   string
	ReadOnly bool
	NoScan   bool
	Cred     credArg // p:, pf:, pe:, k:, or the bare sole trailing token
}

func vdParseMountOpts(args []string) (o vdMountOpts, err error) {
	if len(args) < 1 {
		return o, vdUsagef("mount needs a container: filedo <file.fdd> mount [ro] [noscan] [as <X:>] [p:<password>]")
	}
	o.Path = args[0]
	// The bare trailing token of the shared grammar: a password is taken
	// without p: only when it is the one word after the container, so a
	// password that equals an option word is never eaten as that option.
	if len(args) == 2 {
		if _, ok := credentialToken(args[1]); !ok && !vdMountOptionWord(args[1]) {
			o.Cred = credArg{src: "bare", val: args[1]}
			return o, nil
		}
	}
	for i := 1; i < len(args); i++ {
		if a, ok := credentialToken(args[i]); ok {
			if o.Cred.given() {
				return vdMountOpts{}, vdUsagef("one credential per mount: give p:, pf:, pe: or k: once")
			}
			o.Cred = a
			continue
		}
		switch w := strings.ToLower(args[i]); w {
		case "ro", "readonly":
			o.ReadOnly = true
		case "noscan":
			o.NoScan = true
		case "as":
			if i+1 >= len(args) {
				return vdMountOpts{}, vdUsagef("as needs a drive letter: as X:")
			}
			l := strings.ToUpper(strings.TrimRight(args[i+1], `:\/`))
			if len(l) != 1 || l[0] < 'D' || l[0] > 'Z' {
				return vdMountOpts{}, vdUsagef("the word after as (word %d) is not a drive letter from D: to Z:", i+2)
			}
			o.Letter = l + ":"
			i++
		default:
			// Never quoted back: the word may be a password typed without
			// p: beside an option, and this message reaches history.json.
			return vdMountOpts{}, vdUsagef("unknown mount word %d (want ro, noscan, as <X:>; with any option a password is given as p:<password>)", i+1)
		}
	}
	return o, nil
}

// vdMountOptionWord reports a word the mount parser takes as an option. A
// drive letter is one too: it only means something after as, and a bare
// "X:" alone is far likelier a mistyped option than a password.
func vdMountOptionWord(w string) bool {
	switch strings.ToLower(w) {
	case "ro", "readonly", "noscan", "as":
		return true
	}
	return driveRootSpelling.MatchString(w)
}

func vdMount(args []string, batch bool) error {
	if len(args) > 0 && vdImageKind(args[0]) != "" {
		return vdImageMount(args, batch)
	}
	o, err := vdParseMountOpts(args)
	if err != nil {
		return err
	}
	path, letter, ro := o.Path, o.Letter, o.ReadOnly
	if abs, aerr := absPath(path); aerr == nil {
		path = abs
	}
	info, err := vdisk.Inspect(path)
	if err != nil {
		return err
	}
	switch info.Profile {
	case vdisk.ProfilePlain, vdisk.ProfileFast, vdisk.ProfileRAM, vdisk.ProfileVault:
	case vdisk.ProfileSealed:
		ro = true // below the file system as well: the server refuses every write
	default:
		return fmt.Errorf("%w: mounting a %s container is not carried by this build", vdisk.ErrUnsupported, info.Profile)
	}
	if batch && !windows.GetCurrentProcessToken().IsElevated() {
		return errTransport("mounting needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
	}
	if m, ok := vdFindMount(info.ContainerID); ok {
		if vdServerAlive(m) {
			return errBusy(fmt.Sprintf("%s is already mounted at %s", path, m.Letter))
		}
		return errBusy(fmt.Sprintf("an earlier mount of %s at %s did not end (its block server is gone); run: filedo %s unmount", path, m.Letter, m.Letter))
	}
	fmt.Printf("Container %s: %s, %s, %s.\n", path, info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info))
	switch {
	case info.SaveInProgress:
		// FDD-FORMAT 10.4: a save cut in its second step. Nothing repairs it;
		// the owner decides whether a volume of mixed state is worth mounting.
		fmt.Printf("WARNING: a save of this ram container was interrupted. It began %s; the last complete save was %s.\n", vdTime(info.SaveStarted), vdTime(info.LastGoodSave))
		fmt.Println("The volume may hold a mixture of the two states. Copy the file first if its contents matter.")
		if !ro && !vdConfirm("Mount it anyway?", batch) {
			return vdUsagef("not mounted: an interrupted save needs an answer (mount it from a console, or mount it ro to look first)")
		}
	case !info.Clean:
		fmt.Printf("Note: it was not closed cleanly the last time; last good save %s. Windows checks the volume as it would after a power loss.\n", vdTime(info.LastGoodSave))
	}
	if info.Profile == vdisk.ProfileRAM && !ro {
		p := vdisk.DefaultSavePolicy
		fmt.Printf("ram: writes are kept in memory and saved to the file every %d s or at %d MB; a crash loses what was written since the last save.\n", int(p.Every.Seconds()), p.DirtyLimit>>20)
		if !info.Obfuscated {
			fmt.Println("ram with a credential: Windows may page the volume's memory out, so plaintext can reach the page file while it is mounted.")
		}
	}

	// The credential is resolved here, once, after every check that could
	// refuse the mount: a prompt belongs to the person at this console, and
	// a line with no credential and no terminal fails as usage rather than
	// hanging (FDD-BEHAVIOUR 7 rule 9). It reaches the block server through
	// a pipe, never its command line.
	var cred []byte
	if info.Obfuscated {
		if o.Cred.src == "bare" {
			// Nothing here takes a password, so a lone unknown word is a
			// mistyped option (`mount rw`), refused as it always was.
			return vdUsagef("unknown mount word 2 (want ro, noscan, as <X:>)")
		}
		if o.Cred.given() {
			fmt.Println(vdCredentialNotUsed)
		}
		if o.Cred.src == "pe" {
			// Taken out of the environment all the same, so the detached
			// block server does not inherit it for the life of the mount.
			fdsecLookupCredentialEnv(o.Cred.val)
		}
	} else {
		c, err := vdResolveCredential(o.Cred, false)
		if err != nil {
			return err
		}
		cred = c
		defer clear(cred)
	}

	var cancelPath string
	attached := false
	removeCleanup := func() {}
	if globalInterruptHandler != nil {
		removeCleanup = globalInterruptHandler.AddCleanup(func() {
			if attached {
				return
			}
			if cancelPath != "" {
				os.WriteFile(cancelPath, []byte("cancel"), 0o600)
			}
		})
	}
	defer removeCleanup()

	srv, err := vdLaunchServer(path, info, ro, cred, o.Cred)
	if err != nil {
		return err
	}
	pid, started, h, handoff, stopServer := srv.pid, srv.started, srv.h, srv.handoff, srv.stop
	vdLogf("mount %s: server pid %d on 127.0.0.1:%d", path, pid, h.Port)
	if globalInterruptHandler != nil && globalInterruptHandler.IsInterrupted() {
		stopServer("clean")
		return fmt.Errorf("%w: the mount was interrupted before the disk was connected", vdisk.ErrStopped)
	}
	if !windows.GetCurrentProcessToken().IsElevated() && !batch {
		fmt.Println("Connecting the disk needs administrator rights for the Windows iSCSI initiator; Windows will ask for consent now.")
	}
	req := vdRequest{Port: h.Port, IQN: h.IQN, Secret: h.Secret, Serial: h.Serial,
		Label: h.Label, Letter: letter, ReadOnly: ro, NeverHeld: vdNeverHeldData(info)}
	if o.NoScan {
		req.NoScan = path
	}
	res, err := vdRunElevated("_attach", req, batch, func(c string) { cancelPath = c })
	os.Remove(handoff) // the secret is not needed any more
	if err != nil {
		stopServer("clean")
		return err
	}
	attached = true
	row := vdMountRow{ContainerID: info.ContainerID, Path: path, Letter: res.Letter, ReadOnly: ro,
		MountedAt: time.Now(), ServerPID: pid, ServerStarted: started, Port: h.Port, IQN: h.IQN,
		Serial: h.Serial, Session: res.Session, ScanExcluded: res.Excluded, Profile: info.Profile.String()}
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = append(s.Mounts, row)
		return nil
	}); err != nil {
		return fmt.Errorf("mounted at %s, but the mount state could not be recorded (unmount by letter still works): %w", res.Letter, err)
	}
	if res.Formatted {
		fmt.Println("The new volume was formatted NTFS, with indexing turned off (the drive's Properties can turn it back on).")
	}
	switch {
	case res.Warning != "":
		fmt.Printf("Warning: the container file was not excluded from antivirus scanning: %s\n", res.Warning)
	case res.Excluded:
		fmt.Println("The container file is excluded from Microsoft Defender until unmount; files on the volume are still scanned.")
	}
	roText := ""
	if ro {
		roText = " read-only"
	}
	fmt.Printf("Mounted%s at %s. Unmount with: filedo %s unmount\n", roText, res.Letter, res.Letter)
	vdLogf("mount %s at %s", path, res.Letter)
	vdTouchRegistry(info.ContainerID)
	return nil
}

// vdServer is a block server this command started and that listens now.
type vdServer struct {
	pid      int
	started  int64
	handoff  string
	stopPath string
	h        vdHandoff
}

// stop tells the server to close the container - how is "clean", "unclean"
// or "nosave" - and waits for it to exit.
func (s *vdServer) stop(how string) {
	os.WriteFile(s.stopPath, []byte(how), 0o600)
	vdWaitExit(s.pid, 60*time.Second)
	os.Remove(s.handoff)
}

// vdLaunchServer starts the block server for a container and waits until it
// listens: the one start of the server that mount and format share. A server
// that refused (a wrong credential, a busy file) has exited, and its class is
// the error.
func vdLaunchServer(path string, info vdisk.Info, ro bool, cred []byte, a credArg) (*vdServer, error) {
	handoff, stopPath, err := vdFiles(info.ContainerID)
	if err != nil {
		return nil, err
	}
	os.Remove(handoff)
	pid, err := vdStartServer(path, handoff, ro, !info.Obfuscated, cred)
	if err != nil {
		return nil, err
	}
	started, _ := vdProcessStart(pid)
	s := &vdServer{pid: pid, started: started, handoff: handoff, stopPath: stopPath}
	h, err := vdWaitHandoff(handoff, pid, 60*time.Second)
	if err != nil {
		s.stop("clean")
		return nil, vdCredentialErr(err, a)
	}
	s.h = h
	return s, nil
}

func vdWaitHandoff(path string, pid int, timeout time.Duration) (vdHandoff, error) {
	deadline := time.Now().Add(timeout)
	for {
		if b, err := os.ReadFile(path); err == nil {
			var h vdHandoff
			if json.Unmarshal(b, &h) == nil && h.PID == pid {
				if h.Error != "" {
					if h.Class == vdisk.ExitCredential {
						// The door's own sentence, and nothing about where
						// it was decided.
						return h, vdClassError(h.Class, h.Error)
					}
					return h, vdClassError(h.Class, "the block server could not start: "+h.Error)
				}
				return h, nil
			}
		}
		if _, alive := vdProcessStart(pid); !alive {
			return vdHandoff{}, fmt.Errorf("the block server exited before it listened (see vdisk.log in the state folder)")
		}
		if time.Now().After(deadline) {
			return vdHandoff{}, fmt.Errorf("the block server did not start listening within %s", timeout)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// ---------------------------------------------------------------- unmount

func vdUnmount(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("unmount needs a container or a drive letter: filedo <X:|file.fdd> unmount [force]")
	}
	force, nosave := false, false
	for _, a := range args[1:] {
		switch strings.ToLower(a) {
		case "force", "-y":
			force = true
		case "nosave":
			nosave = true
		default:
			return vdUsagef("unknown unmount option %q (want force, nosave)", a)
		}
	}
	row, err := vdMountedRow(args[0])
	if err != nil {
		if vdImageKind(args[0]) != "" || driveRootSpelling.MatchString(args[0]) {
			if handled, ierr := vdImageUnmount(args[0], batch); handled {
				return ierr
			}
		}
		return err
	}
	if nosave {
		if row.Profile != vdisk.ProfileRAM.String() {
			return vdUsagef("nosave is for a ram container; %s is %s and keeps nothing in memory", row.Path, row.Profile)
		}
		// The consequence is printed always; force skips the question, never
		// the printing (hard invariant 9).
		lost := "everything written since the last save"
		if r, ok := vdReadRAMStatus(row.ContainerID); ok {
			lost = fmt.Sprintf("%s written since the save of %s", vdSize(r.DirtyBytes), vdTime(r.LastGoodSave))
		}
		fmt.Printf("unmount nosave drops %s. The container stays as that save left it, marked not closed cleanly.\n", lost)
		if !force && !vdConfirm("Drop it?", batch) {
			return vdUsagef("not unmounted (a batch never answers the question; add force to drop without asking)")
		}
	}
	if !windows.GetCurrentProcessToken().IsElevated() && !batch {
		fmt.Println("Disconnecting the disk needs administrator rights for the Windows iSCSI initiator; Windows will ask for consent now.")
	}
	alive, how, res, err := vdUnmountOne(row, force, nosave, batch)
	if err != nil {
		return err
	}
	if res.Warning != "" {
		fmt.Printf("Warning: the Defender exclusion of %s could not be removed: %s\n", row.Path, res.Warning)
	}
	switch {
	case !alive:
		fmt.Printf("Cleaned up %s: its block server had already gone, so %s was not closed cleanly; the next mount reports it.\n", row.Letter, row.Path)
	case how == "nosave":
		fmt.Printf("Unmounted %s without the final save. %s holds its last save and is marked as not closed cleanly.\n", row.Letter, row.Path)
	case how == "unclean":
		fmt.Printf("Unmounted %s by force. %s is marked as not closed cleanly; the next mount reports it.\n", row.Letter, row.Path)
	default:
		fmt.Printf("Unmounted %s. %s is closed cleanly.\n", row.Letter, row.Path)
	}
	return nil
}

// vdUnmountOne is the machine work of unmounting one mounted container: the
// detach step, the stop to its server, the wait, the state row. The unmount
// verb adds its printing and the shutdown guard its recording (SP-0080 3.2);
// neither assembles the steps again. batch governs the consent question
// exactly as the verb's does - a batch never raises one, and neither does the
// guard, whose caller has nobody to answer it.
func vdUnmountOne(row vdMountRow, force, nosave, batch bool) (alive bool, how string, res vdResult, err error) {
	// A volume whose server is gone is dead already: it can be neither locked
	// nor flushed, so its detach is a forced one whatever was asked.
	alive = vdServerAlive(row)
	req := vdRequest{Port: row.Port, IQN: row.IQN, Serial: row.Serial,
		Session: row.Session, Force: force || !alive, ReadOnly: row.ReadOnly}
	if row.ScanExcluded {
		req.NoScan = row.Path
	}
	res, err = vdRunElevated("_detach", req, batch, nil)
	if err != nil {
		return alive, "", res, err
	}
	how = "clean"
	switch {
	case !alive:
		how = "unclean"
	case nosave: // unclean as well, and without the save an Abandon would make
		how = "nosave"
	case res.Unclean:
		how = "unclean"
	}
	if alive {
		_, stopPath, _ := vdFiles(row.ContainerID)
		if err = os.WriteFile(stopPath, []byte(how), 0o600); err != nil {
			return alive, "", res, err
		}
		if !vdWaitExit(row.ServerPID, 120*time.Second) {
			return alive, how, res, fmt.Errorf("the disk is detached, but the block server (process %d) did not exit within 120 s; the container may not be marked clean", row.ServerPID)
		}
	}
	if err = vdUpdateState(func(s *vdState) error {
		kept := s.Mounts[:0]
		for _, m := range s.Mounts {
			if !strings.EqualFold(m.ContainerID, row.ContainerID) {
				kept = append(kept, m)
			}
		}
		s.Mounts = kept
		return nil
	}); err != nil {
		return alive, how, res, err
	}
	vdLogf("unmount %s from %s: %s", row.Path, row.Letter, how)
	return alive, how, res, nil
}

// vdMountedRow finds the mount a target names: a drive letter, or a container
// by its path or, when the path differs, by its id.
func vdMountedRow(target string) (vdMountRow, error) {
	s, err := vdLoadState()
	if err != nil {
		return vdMountRow{}, err
	}
	if driveRootSpelling.MatchString(target) {
		l := strings.ToUpper(target[:1]) + ":"
		for _, m := range s.Mounts {
			if strings.EqualFold(m.Letter, l) {
				return m, nil
			}
		}
	} else {
		abs, _ := absPath(target)
		for _, m := range s.Mounts {
			if strings.EqualFold(m.Path, abs) {
				return m, nil
			}
		}
		if i, err := vdisk.Inspect(target); err == nil {
			if m, ok := vdFindMount(i.ContainerID); ok {
				return m, nil
			}
		}
	}
	return vdMountRow{}, vdUsagef("%s is not mounted", target)
}

// ---------------------------------------------------------------- save

// vdSave asks a mounted ram container's block server to save now and waits
// for its answer. It needs no elevation: the request is a file in the state
// root, which the server watches.
func vdSave(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("save needs a mounted ram container or its drive letter: filedo <X:|file.fdd> save")
	}
	row, err := vdMountedRow(args[0])
	if err != nil {
		return err
	}
	if row.Profile != vdisk.ProfileRAM.String() {
		return vdUsagef("%s is a %s container: every write already reaches its file, there is nothing to save", row.Letter, row.Profile)
	}
	if row.ReadOnly {
		return vdUsagef("%s is mounted read-only: nothing to save", row.Letter)
	}
	if !vdServerAlive(row) {
		return errTransport("the block server of " + row.Letter + " is gone; what it held in memory is lost. Run: filedo " + row.Letter + " unmount")
	}
	savePath, err := vdSidePath(row.ContainerID, ".save")
	if err != nil {
		return err
	}
	// What Windows still holds in its own cache has not reached the disk,
	// so a save that begins now would not contain it (S4 kit case R2).
	// Flushing a whole volume needs administrator rights: the one step
	// _flush, through the consent prompt; a batch cannot ask, and says so.
	flushed := true
	switch {
	case windows.GetCurrentProcessToken().IsElevated() || !batch:
		if !windows.GetCurrentProcessToken().IsElevated() {
			fmt.Println("Flushing the Windows cache of the volume first needs administrator rights; Windows will ask for consent now.")
		}
		if _, err := vdRunElevated("_flush", vdRequest{Letter: row.Letter}, batch, nil); err != nil {
			return err
		}
	default:
		flushed = false
	}
	savedPath, _ := vdSidePath(row.ContainerID, ".saved.json")
	token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	os.Remove(savedPath)
	if err := statedir.WriteFileAtomic(savePath, []byte(token), 0o600); err != nil {
		return err
	}
	for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		b, err := os.ReadFile(savedPath)
		if err != nil {
			if !vdServerAlive(row) {
				return errTransport("the block server exited before it answered")
			}
			continue
		}
		var ans vdSaved
		if json.Unmarshal(b, &ans) != nil || ans.Request != token {
			continue
		}
		if ans.Error != "" {
			return vdClassError(ans.Class, "the save failed: "+ans.Error)
		}
		if flushed {
			fmt.Printf("Saved %s: its file now holds everything written to it before this command.\n", row.Letter)
		} else {
			fmt.Printf("Saved %s. A batch cannot ask for consent, so the Windows cache of the volume was not flushed first: files written in the last seconds may not be in this save.\n", row.Letter)
		}
		vdLogf("save %s at %s on request", row.Path, row.Letter)
		return nil
	}
	return errBusy("the block server did not answer the save within 10 minutes")
}

// vdFlushStep is the elevated half of save: it flushes the file system cache
// of the volume at req.Letter, so everything written to it reaches the block
// server before the save begins. Nothing else.
func vdFlushStep(req vdRequest) (vdResult, error) {
	var res vdResult
	if !driveRootSpelling.MatchString(req.Letter) {
		return res, vdUsagef("not a drive letter: %q", req.Letter)
	}
	p, _ := windows.UTF16PtrFromString(`\\.\` + strings.ToUpper(req.Letter[:1]) + ":")
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return res, fmt.Errorf("could not open the volume %s: %w", req.Letter, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.FlushFileBuffers(h); err != nil {
		return res, fmt.Errorf("Windows could not flush %s: %w", req.Letter, err)
	}
	vdLogf("flush %s before a save", req.Letter)
	return res, nil
}

func absPath(p string) (string, error) {
	return filepath.Abs(p)
}

// vdServeCredWord tells `vd _serve` that its credential waits on stdin; it is
// bounded by vdMaxCredential, since the pipe is written in full before the server
// starts, so the credential has to fit its buffer.
const (
	vdServeCredWord = "cred"
)

// vdReadServeCredential reads the credential the mount wrote down the pipe.
// An EOF with nothing read is the empty credential, tried like any other.
func vdReadServeCredential() (fdsec.Credential, error) {
	b, err := io.ReadAll(io.LimitReader(os.Stdin, vdMaxCredential+1))
	if err != nil {
		clear(b)
		return nil, fmt.Errorf("could not read the credential from the mount: %w", err)
	}
	if len(b) > vdMaxCredential {
		clear(b)
		return nil, vdUsagef("the credential is longer than %d bytes", vdMaxCredential)
	}
	return fdsec.Credential(b), nil
}

// vdNeverHeldData reports whether a container has never held data: no data
// cluster was ever allocated in it, or it was never mounted (the fast profile
// allocates every cluster when it is created, so its count alone says nothing),
// and its header is its own (a header read back from the backup may be older
// than the data). Only such a container may be formatted by a mount that finds
// no partition table (AUD-32-F3).
func vdNeverHeldData(info vdisk.Info) bool {
	return !info.FromBackup && (info.AllocatedClusters == 0 || info.MountCount == 0)
}

// vdMayFormatOnMount is the decision a mount takes when the disk it attached
// shows no partition table. An explicit `vd format` (formatOnly) formats. A
// mount formats only a container that has never held data: on any other a
// damaged table (a failed save, a stray write, a bad sector) looks exactly like
// a blank disk, and a quick format over it would empty the volume behind a
// one-line "formatted". The refusal touches nothing and names the ways out.
func vdMayFormatOnMount(blank, formatOnly, neverHeld bool, disk int) error {
	if blank && !formatOnly && !neverHeld {
		return vdUsagef("disk %d shows no partition table, but this container has held data; nothing was formatted. If its volume is damaged, copy the container file first, then run `filedo vd format` on it (that empties it, and asks first)", disk)
	}
	return nil
}
