package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"filedo/vdisk"
)

// `filedo vd status json` (SP-0063 section 8.1): one machine-readable snapshot
// of every disk FileDO knows about, for the window that shows them. The text
// of `vd list` and `vd status` stays for people; this is the one output a
// program reads, so a reworded console line can never empty a column.
//
// The document is an in-repo wire shape (SP-0063 D2): the GUI and the CLI ship
// in one release, so it lives here and nowhere else until a second consumer
// appears. It carries its schema and version from its first build, and its
// reader ignores a field it does not know:
//
//	{
//	  "schema": "filedo.vd-status", "version": 1,
//	  "at": "<RFC 3339, local>",
//	  "packaged": false,
//	  "transport": {"ready": true, "initiator_service": "running|stopped|disabled|missing|unknown|", "reason": "|packaged|initiator_missing|initiator_disabled|service_manager"},
//	  "disks": [
//	    {"kind": "container", "name": "<registry name or empty>", "path": "..", "container_id": "..",
//	     "registered": true, "file": "ok|missing|different|unreadable", "file_error": "<only when unreadable>",
//	     "profile": "plain|fast|ram|sealed|vault", "protection": "obfuscated|encrypted|",
//	     "logical_size": 0, "clean": true|null, "last_good_save": "<time>"|null, "auto": false,
//	     "mount": null | {"letter": "X:", "read_only": false, "mounted_at": "<time>", "server_alive": true,
//	                      "ram": null | {"dirty_bytes": 0, "saving": false, "last_good_save": "<time>"|null,
//	                                     "save_error": "<only while saving to the file fails>"}}},
//	    {"kind": "image", "path": "..", "letter": "X:", "mounted_at": "<time>"}
//	  ],
//	  "guard": {"installed": false, "running": false, "last_run": null, "ended": "",
//	            "containers": [{"name": "", "path": "..", "action": "save|unmount",
//	                            "outcome": "saved|unmounted|skipped|unfinished",
//	                            "reason": "..", "bytes_saved": 0}]}
//	}
//
// Rows: every registered container, then every mounted container the registry
// does not name, then every foreign image FileDO mounted. A mount and its
// registration are joined by container id, never by path.
//
// `guard` is the shutdown guard (SP-0080): additive inside this schema
// version, whose reader ignores unknown fields, so an old GUI and a new CLI -
// and the reverse - both keep working. `last_run` is when the guard last ran,
// null before its first session end; `containers` is that run's rows.
//
// `mount.ram.save_error` (AUD-35-F5, 2026-10-01) is additive the same way: the
// last save's error while saving a ram buffer to its file fails, absent while
// saves succeed. The version stays 1 - it is the major the reader refuses
// above (DiskSnapshot.KnownVersion), and an optional field is not a major.
//
// What it never carries: a credential, a port, a process id, an IQN, a
// session id or a serial. Those stay in vdisk-state.json and in the text of
// `vd status`; a window needs "alive or not", not how. It reads container
// headers only (vdisk.Inspect) and asks for no credential, so it is safe and
// fast for every container.

const (
	vdSnapshotSchema  = "filedo.vd-status"
	vdSnapshotVersion = 1
)

// vdStamp is a time in the snapshot: RFC 3339 in local time to the second, or
// null when the time is zero (never saved, not known).
type vdStamp time.Time

func (s vdStamp) MarshalJSON() ([]byte, error) {
	t := time.Time(s)
	if t.IsZero() {
		return []byte("null"), nil
	}
	return json.Marshal(t.Local().Truncate(time.Second).Format(time.RFC3339))
}

// UnmarshalJSON reads a stamp back - the heartbeat, the report and the
// snapshot's guard field are written and read on this machine. null is the
// zero time, as MarshalJSON writes it.
func (s *vdStamp) UnmarshalJSON(b []byte) error {
	var t time.Time
	if err := json.Unmarshal(b, &t); err != nil {
		return err
	}
	*s = vdStamp(t)
	return nil
}

type vdSnapshot struct {
	Schema    string          `json:"schema"`
	Version   int             `json:"version"`
	At        vdStamp         `json:"at"`
	Packaged  bool            `json:"packaged"`
	Transport vdSnapTransport `json:"transport"`
	Disks     []interface{}   `json:"disks"`
	Guard     vdSnapGuard     `json:"guard"`
}

// vdSnapGuard is the shutdown guard's state at the snapshot's moment
// (SP-0080 4-5): whether the task is installed, whether a watcher holds the
// heartbeat, and what the last session's end did.
type vdSnapGuard struct {
	Installed  bool             `json:"installed"`
	Running    bool             `json:"running"`
	LastRun    vdStamp          `json:"last_run"`
	Ended      string           `json:"ended,omitempty"`
	Containers []vdSnapGuardRow `json:"containers,omitempty"`
}

type vdSnapGuardRow struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Action     string `json:"action"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
	BytesSaved int64  `json:"bytes_saved,omitempty"`
}

type vdSnapTransport struct {
	Ready            bool   `json:"ready"`
	InitiatorService string `json:"initiator_service"`
	Reason           string `json:"reason"`
}

type vdSnapContainer struct {
	Kind        string       `json:"kind"`
	Name        string       `json:"name"`
	Path        string       `json:"path"`
	ContainerID string       `json:"container_id"`
	Registered  bool         `json:"registered"`
	File        string       `json:"file"`
	FileError   string       `json:"file_error,omitempty"`
	Profile     string       `json:"profile"`
	Protection  string       `json:"protection"`
	LogicalSize int64        `json:"logical_size"`
	Clean       *bool        `json:"clean"`
	LastGood    vdStamp      `json:"last_good_save"`
	Auto        bool         `json:"auto"`
	Mount       *vdSnapMount `json:"mount"`
}

type vdSnapMount struct {
	Letter      string     `json:"letter"`
	ReadOnly    bool       `json:"read_only"`
	MountedAt   vdStamp    `json:"mounted_at"`
	ServerAlive bool       `json:"server_alive"`
	RAM         *vdSnapRAM `json:"ram"`
}

type vdSnapRAM struct {
	DirtyBytes int64   `json:"dirty_bytes"`
	Saving     bool    `json:"saving"`
	LastGood   vdStamp `json:"last_good_save"`
	SaveError  string  `json:"save_error,omitempty"` // AUD-35-F5: saving to the file is failing
}

type vdSnapImage struct {
	Kind      string  `json:"kind"`
	Path      string  `json:"path"`
	Letter    string  `json:"letter"`
	MountedAt vdStamp `json:"mounted_at"`
}

// The seams of the snapshot's tests: the scheduled tasks, the initiator
// service, a block server's liveness and the clock are the machine's, and a
// golden document must not depend on them.
var (
	vdSnapshotTasks     = vdAutoTasks
	vdSnapshotTransport = vdTransportState
	vdSnapshotAlive     = vdServerAlive
	vdSnapshotGuard     = vdGuardState
	vdSnapshotNow       = time.Now
)

// vdMachineOutput reports a command line whose stdout is a document for a
// program - `vd status json` - so main prints neither its banner nor its
// finish line around it, and `filedo vd status json | ConvertFrom-Json`
// reads the document and nothing else. The global flags are skipped the way
// extractGlobalFlags reads them.
func vdMachineOutput(argv []string) bool {
	var words []string
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case (a == "--events" || a == "--stop-file") && i+1 < len(argv):
			i++
		case strings.HasPrefix(a, "--events="), strings.HasPrefix(a, "--stop-file="),
			a == "--no-ui", a == "--pause", a == "-pause", a == "/pause", a == "--no-history":
		default:
			words = append(words, a)
		}
	}
	return len(words) == 3 && contains(list_of_flags_for_vd, strings.ToLower(words[0])) &&
		strings.EqualFold(words[1], "status") && strings.EqualFold(words[2], "json")
}

// vdStatusJSON prints the snapshot as one line of JSON on stdout. A path is
// written as it is: & < > stay themselves rather than & escapes, which
// are valid JSON but not what a person reading the line expects.
func vdStatusJSON() error {
	s, err := vdBuildSnapshot()
	if err != nil {
		return err
	}
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return err
	}
	fmt.Print(b.String())
	return nil
}

// vdBuildSnapshot reads the registry and the mount state - FileDO's own files,
// which the window never reads itself - and every registered container's
// header. A registry or a state file that cannot be read fails the whole
// snapshot: a partial list would look like "nothing is mounted".
func vdBuildSnapshot() (vdSnapshot, error) {
	s := vdSnapshot{Schema: vdSnapshotSchema, Version: vdSnapshotVersion, At: vdStamp(vdSnapshotNow()), Disks: []interface{}{}}
	s.Packaged = vdPackaged()
	if s.Packaged {
		s.Transport = vdSnapTransport{Ready: false, Reason: "packaged"}
	} else {
		s.Transport = vdSnapshotTransport()
	}
	s.Guard = vdSnapshotGuard()
	reg, err := vdLoadRegistry()
	if err != nil {
		return s, err
	}
	state, err := vdLoadState()
	if err != nil {
		return s, err
	}
	var tasks map[string]bool
	if len(reg.Containers) > 0 {
		// schtasks is the slow part; with nothing registered nothing can have a task.
		tasks = vdSnapshotTasks()
	}
	claimed := map[string]bool{}
	var rows []*vdSnapContainer
	var wantIDs []string
	for _, e := range reg.Containers {
		row := &vdSnapContainer{Kind: "container", Name: e.Name, Path: e.Path, ContainerID: e.ContainerID,
			Registered: true, Profile: e.Profile, LogicalSize: e.LogicalSize, Auto: tasks[strings.ToLower(e.Name)]}
		for _, m := range state.Mounts {
			if strings.EqualFold(m.ContainerID, e.ContainerID) {
				row.Mount = vdSnapMountOf(m)
				claimed[strings.ToLower(m.ContainerID)] = true
				break
			}
		}
		rows, wantIDs = append(rows, row), append(wantIDs, e.ContainerID)
	}
	mounts := append([]vdMountRow(nil), state.Mounts...)
	sort.SliceStable(mounts, func(a, b int) bool { return mounts[a].Letter < mounts[b].Letter })
	for _, m := range mounts {
		if claimed[strings.ToLower(m.ContainerID)] {
			continue
		}
		row := &vdSnapContainer{Kind: "container", Path: m.Path, ContainerID: m.ContainerID, Profile: m.Profile}
		row.Mount = vdSnapMountOf(m)
		rows, wantIDs = append(rows, row), append(wantIDs, m.ContainerID)
	}
	if err := vdSnapFiles(rows, wantIDs); err != nil {
		return s, err
	}
	for _, row := range rows {
		s.Disks = append(s.Disks, *row)
	}
	images := vdLetteredImages(state.Images) // AUD-34-F2: only rows with a letter
	sort.SliceStable(images, func(a, b int) bool { return images[a].Letter < images[b].Letter })
	for _, im := range images {
		s.Disks = append(s.Disks, vdSnapImage{Kind: "image", Path: im.Path, Letter: im.Letter, MountedAt: vdStamp(im.MountedAt)})
	}
	return s, nil
}

// vdSnapFileTimeout bounds how long one container file may take to answer. A
// file on an unreachable network path takes about 20 s to fail, and the window
// stops waiting for the whole snapshot after 8: without a bound of its own one
// dead entry hid every disk (AUD-34-F6, AUD-30-F1). A variable so a test can
// shorten it.
var vdSnapFileTimeout = 3 * time.Second

// vdSnapFileFn is the per-file inspection; a seam for the test.
var vdSnapFileFn = vdSnapFile

// vdSnapFileWorkers bounds how many files are inspected at once (AUD-34-F6): a
// registry of many dead network entries must not open that many hung requests
// together. A variable so a test can lower it.
var vdSnapFileWorkers = 8

// vdSnapStopped and vdSnapStopPoll are the seams for the run's stop: the build
// looks at the stop this often while it waits.
var (
	vdSnapStopped  = runStopRequested
	vdSnapStopPoll = 50 * time.Millisecond
)

// vdSnapFiles inspects every row's file in parallel - at most vdSnapFileWorkers
// at a time - each with its own bound. A file that has not answered in time
// reads as unreadable, with the reason: it is known to exist in the registry,
// so "missing" would be a claim nobody checked. A stop ends the wait at once
// and the build returns errRunStopped: a half-inspected list would pass for
// the real one.
func vdSnapFiles(rows []*vdSnapContainer, wantIDs []string) error {
	stop := make(chan struct{})
	quit := make(chan struct{})
	defer close(quit)
	go func() {
		t := time.NewTicker(vdSnapStopPoll)
		defer t.Stop()
		for {
			if vdSnapStopped() {
				close(stop)
				return
			}
			select {
			case <-t.C:
			case <-quit:
				return
			}
		}
	}()
	slots := make(chan struct{}, vdSnapFileWorkers)
	var wg sync.WaitGroup
	for i := range rows {
		wg.Add(1)
		go func(row *vdSnapContainer, wantID string) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-stop:
				return
			}
			done := make(chan vdSnapContainer, 1)
			go func() {
				tmp := *row // the inspection works on a copy: a late answer never touches the row
				vdSnapFileFn(&tmp, wantID)
				done <- tmp
			}()
			timer := time.NewTimer(vdSnapFileTimeout)
			defer timer.Stop()
			select {
			case tmp := <-done:
				*row = tmp
			case <-timer.C:
				row.File = "unreadable"
				row.FileError = fmt.Sprintf("did not answer within %s (a network path or a device that is not responding)", vdSnapFileTimeout)
			case <-stop:
			}
		}(rows[i], wantIDs[i])
	}
	wg.Wait()
	select {
	case <-stop:
		return errRunStopped
	default:
		return nil
	}
}

// vdNetworkGone reports an error that says the path's host or share could not
// be reached. Go files these under "does not exist", which is not what they
// mean: the file may be there, and the network is not.
func vdNetworkGone(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case 53, 64, 67, 1222, 1231: // BAD_NETPATH, NETNAME_DELETED, BAD_NET_NAME, NO_NETWORK, NETWORK_UNREACHABLE
		return true
	}
	return false
}

// vdSnapFile fills what the container file says: whether it is there, whether
// it is still the container the row expects (by id), and what its header
// holds. A header that does not read leaves the registry's profile and size
// and claims no protection.
func vdSnapFile(row *vdSnapContainer, wantID string) {
	if _, err := os.Stat(row.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) && !vdNetworkGone(err) {
			row.File = "missing"
			return
		}
		row.File, row.FileError = "unreadable", vdExplain(err)
		return
	}
	info, err := vdisk.Inspect(row.Path)
	if err != nil {
		row.File, row.FileError = "unreadable", vdExplain(err)
		return
	}
	if wantID != "" && !strings.EqualFold(info.ContainerID, wantID) {
		// Another container is at this path now: nothing it says is this row's.
		row.File = "different"
		return
	}
	row.File = "ok"
	row.Profile = info.Profile.String()
	row.Protection = info.Protection()
	row.LogicalSize = info.LogicalSize
	clean := info.Clean
	row.Clean = &clean
	row.LastGood = vdStamp(info.LastGoodSave)
	if row.ContainerID == "" {
		row.ContainerID = info.ContainerID
	}
}

func vdSnapMountOf(m vdMountRow) *vdSnapMount {
	out := &vdSnapMount{Letter: m.Letter, ReadOnly: m.ReadOnly, MountedAt: vdStamp(m.MountedAt), ServerAlive: vdSnapshotAlive(m)}
	if m.Profile == vdisk.ProfileRAM.String() && out.ServerAlive {
		if r, ok := vdReadRAMStatus(m.ContainerID); ok {
			out.RAM = &vdSnapRAM{DirtyBytes: r.DirtyBytes, Saving: r.Saving, LastGood: vdStamp(r.LastGoodSave), SaveError: r.SaveError}
		}
	}
	return out
}
