package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
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
//	                      "ram": null | {"dirty_bytes": 0, "saving": false, "last_good_save": "<time>"|null}}},
//	    {"kind": "image", "path": "..", "letter": "X:", "mounted_at": "<time>"}
//	  ]
//	}
//
// Rows: every registered container, then every mounted container the registry
// does not name, then every foreign image FileDO mounted. A mount and its
// registration are joined by container id, never by path.
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

type vdSnapshot struct {
	Schema    string          `json:"schema"`
	Version   int             `json:"version"`
	At        vdStamp         `json:"at"`
	Packaged  bool            `json:"packaged"`
	Transport vdSnapTransport `json:"transport"`
	Disks     []interface{}   `json:"disks"`
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
	for _, e := range reg.Containers {
		row := vdSnapContainer{Kind: "container", Name: e.Name, Path: e.Path, ContainerID: e.ContainerID,
			Registered: true, Profile: e.Profile, LogicalSize: e.LogicalSize, Auto: tasks[strings.ToLower(e.Name)]}
		vdSnapFile(&row, e.ContainerID)
		for _, m := range state.Mounts {
			if strings.EqualFold(m.ContainerID, e.ContainerID) {
				row.Mount = vdSnapMountOf(m)
				claimed[strings.ToLower(m.ContainerID)] = true
				break
			}
		}
		s.Disks = append(s.Disks, row)
	}
	mounts := append([]vdMountRow(nil), state.Mounts...)
	sort.SliceStable(mounts, func(a, b int) bool { return mounts[a].Letter < mounts[b].Letter })
	for _, m := range mounts {
		if claimed[strings.ToLower(m.ContainerID)] {
			continue
		}
		row := vdSnapContainer{Kind: "container", Path: m.Path, ContainerID: m.ContainerID, Profile: m.Profile}
		vdSnapFile(&row, m.ContainerID)
		row.Mount = vdSnapMountOf(m)
		s.Disks = append(s.Disks, row)
	}
	images := append([]vdImageRow(nil), state.Images...)
	sort.SliceStable(images, func(a, b int) bool { return images[a].Letter < images[b].Letter })
	for _, im := range images {
		s.Disks = append(s.Disks, vdSnapImage{Kind: "image", Path: im.Path, Letter: im.Letter, MountedAt: vdStamp(im.MountedAt)})
	}
	return s, nil
}

// vdSnapFile fills what the container file says: whether it is there, whether
// it is still the container the row expects (by id), and what its header
// holds. A header that does not read leaves the registry's profile and size
// and claims no protection.
func vdSnapFile(row *vdSnapContainer, wantID string) {
	if _, err := os.Stat(row.Path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
			out.RAM = &vdSnapRAM{DirtyBytes: r.DirtyBytes, Saving: r.Saving, LastGood: vdStamp(r.LastGoodSave)}
		}
	}
	return out
}
