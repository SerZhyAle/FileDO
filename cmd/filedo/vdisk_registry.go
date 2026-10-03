package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"filedo/statedir"
	"filedo/vdisk"
)

// The container registry (SP-0004 Q27, P4 section 6): short names for
// containers, so `filedo vd mount work` finds work's file. It is convenience
// only - every verb also takes a container by path, and a path that exists
// always wins over a name. It lives in vd-registry.json in the state root and
// is changed under the state lock, written whole to a temporary file and
// renamed, never edited in place. add and forget touch the registry only,
// never the container.

const vdRegistryFile = "vd-registry.json"

type vdRegEntry struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	ContainerID string    `json:"container_id"`
	Profile     string    `json:"profile"`
	LogicalSize int64     `json:"logical_size"`
	Added       time.Time `json:"added"`
	LastMounted time.Time `json:"last_mounted,omitempty"`
	// Carrier and Part are written for a partition disk (SP-0148 9.2): Path
	// is then its locator, and Part its stored identity. An entry without
	// them is a file, so a registry an older build wrote reads unchanged.
	Carrier    string        `json:"carrier,omitempty"`
	Part       *vdPartRecord `json:"part,omitempty"`
	Protection string        `json:"protection,omitempty"`
}

// isPart reports whether the entry is a partition disk.
func (e vdRegEntry) isPart() bool { return e.Part != nil }

type vdRegistry struct {
	Version    int          `json:"version"`
	Containers []vdRegEntry `json:"containers"`
}

var vdNameSpelling = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$`)

func vdRegistryPath() (string, error) { return statedir.Path(vdRegistryFile) }

func vdLoadRegistry() (vdRegistry, error) {
	r := vdRegistry{Version: 1}
	p, err := vdRegistryPath()
	if err != nil {
		return r, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("the container registry %s is unreadable: %w", p, err)
	}
	return r, nil
}

func vdUpdateRegistry(change func(*vdRegistry) error) error {
	p, err := vdRegistryPath()
	if err != nil {
		return err
	}
	unlock, err := statedir.Lock(p, 10*time.Second)
	if err != nil {
		return errBusy("another FileDO command holds the container registry: " + err.Error())
	}
	defer unlock()
	r, err := vdLoadRegistry()
	if err != nil {
		return err
	}
	if err := change(&r); err != nil {
		return err
	}
	r.Version = 1
	sort.Slice(r.Containers, func(a, b int) bool {
		return strings.ToLower(r.Containers[a].Name) < strings.ToLower(r.Containers[b].Name)
	})
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return statedir.WriteFileAtomic(p, b, 0o600)
}

func (r *vdRegistry) find(name string) int {
	for i, e := range r.Containers {
		if strings.EqualFold(e.Name, name) {
			return i
		}
	}
	return -1
}

// vdResolve turns a verb's container argument into a path: an existing file
// or anything spelled as a path or a drive letter is itself; otherwise a
// registered name is its path. Anything else is returned unchanged, so the
// verb reports the missing file in its own words.
func vdResolve(arg string) string {
	if arg == "" || driveRootSpelling.MatchString(arg) || strings.ContainsAny(arg, `\/:`) || isFddPath(arg) {
		return arg
	}
	if _, err := os.Stat(arg); err == nil {
		return arg
	}
	r, err := vdLoadRegistry()
	if err != nil {
		return arg
	}
	if i := r.find(arg); i >= 0 {
		return r.Containers[i].Path
	}
	return arg
}

// vdRegisteredName is the registry name an argument means: the name itself,
// or the name of the entry whose path it is - the Disks pages name a
// container by its file (SP-0063). "" when it is neither.
func vdRegisteredName(arg string) string {
	r, err := vdLoadRegistry()
	if err != nil {
		return ""
	}
	if i := r.find(arg); i >= 0 {
		return r.Containers[i].Name
	}
	if abs, err := absPath(arg); err == nil {
		for _, e := range r.Containers {
			if strings.EqualFold(e.Path, abs) {
				return e.Name
			}
		}
	}
	return ""
}

// vdTouchRegistry records a mount's time on the entry of that container, if
// it has one. Best effort: the registry never stands in a mount's way.
func vdTouchRegistry(containerID string) {
	r, err := vdLoadRegistry()
	if err != nil {
		return
	}
	has := false
	for _, e := range r.Containers {
		has = has || strings.EqualFold(e.ContainerID, containerID)
	}
	if !has {
		return
	}
	vdUpdateRegistry(func(r *vdRegistry) error {
		for i := range r.Containers {
			if strings.EqualFold(r.Containers[i].ContainerID, containerID) {
				r.Containers[i].LastMounted = time.Now()
			}
		}
		return nil
	})
}

// vdAdd registers a container: filedo vd add <file.fdd> [as <name>].
func vdAdd(args []string) error {
	if len(args) < 1 {
		return vdUsagef("add needs a container: filedo vd add <file.fdd> [as <name>]")
	}
	path, name := args[0], ""
	switch {
	case len(args) == 3 && strings.EqualFold(args[1], "as"):
		name = args[2]
	case len(args) != 1:
		return vdUsagef("add takes a container and an optional name: filedo vd add <file.fdd> [as <name>]")
	}
	abs, err := absPath(path)
	if err != nil {
		return err
	}
	info, err := vdisk.Inspect(abs)
	if err != nil {
		return err
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	}
	if !vdNameSpelling.MatchString(name) || driveRootSpelling.MatchString(name) {
		return vdUsagef("%q is not a usable name: letters, digits, - and _, up to 40, starting with a letter or digit (and not a drive letter)", name)
	}
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		if i := r.find(name); i >= 0 {
			return vdUsagef("the name %s is taken by %s; forget it first or choose another with: as <name>", name, r.Containers[i].Path)
		}
		for _, e := range r.Containers {
			if strings.EqualFold(e.Path, abs) {
				return vdUsagef("%s is registered already as %s", abs, e.Name)
			}
		}
		r.Containers = append(r.Containers, vdRegEntry{Name: name, Path: abs, ContainerID: info.ContainerID,
			Profile: info.Profile.String(), LogicalSize: info.LogicalSize, Added: time.Now()})
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("Registered %s as %s. Mount it with: filedo vd mount %s\n", abs, name, name)
	if _, err := os.Stat(name); err == nil {
		fmt.Printf("Note: a file named %s exists in this folder; there, a command with %s means that file, not this container.\n", name, name)
	}
	return nil
}

// vdForget removes a name: filedo vd forget <name>. The container stays.
func vdForget(args []string) error {
	if len(args) != 1 {
		return vdUsagef("forget needs a registered name: filedo vd forget <name>")
	}
	var gone vdRegEntry
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		i := r.find(args[0])
		if i < 0 {
			return vdUsagef("%s is not a registered name (see: filedo vd list)", args[0])
		}
		gone = r.Containers[i]
		r.Containers = append(r.Containers[:i], r.Containers[i+1:]...)
		return nil
	}); err != nil {
		return err
	}
	if gone.isPart() {
		fmt.Printf("Forgot %s. The partition %s stays on its disk, unchanged; filedo vd adopt %s registers it again, and filedo vd destroy deletes it.\n", gone.Name, gone.Path, gone.Path)
		return nil
	}
	fmt.Printf("Forgot %s. The container %s is unchanged.\n", gone.Name, gone.Path)
	return nil
}

// vdList prints the registry with the resolved path beside every name, a
// file that is gone shown as missing, and what is mounted or has a task.
func vdList() error {
	r, err := vdLoadRegistry()
	if err != nil {
		return err
	}
	if len(r.Containers) == 0 {
		fmt.Println("No containers are registered. Register one with: filedo vd add <file.fdd> [as <name>]")
		return nil
	}
	tasks := vdAutoTasks()
	var disks []vdDisk
	for _, e := range r.Containers {
		state := ""
		if e.isPart() {
			// A partition disk's header needs elevation to read; list says
			// whether its partition is there, from discovery (no prompt).
			if disks == nil {
				disks, _ = vdDiscoverDisks()
			}
			state = vdPartListState(e, disks)
			if tasks[strings.ToLower(e.Name)] {
				state += "; mounts automatically"
			}
			fmt.Printf("%-16s %-6s %8s  %s  [partition disk, %s]\n", e.Name, e.Profile, vdSize(e.LogicalSize), e.Path, state)
			continue
		}
		_, serr := os.Stat(e.Path)
		switch info, err := vdisk.Inspect(e.Path); {
		case os.IsNotExist(serr):
			state = "MISSING - the file is gone; filedo vd forget " + e.Name + " removes the name"
		case err != nil:
			state = "unreadable: " + err.Error()
		case !strings.EqualFold(info.ContainerID, e.ContainerID):
			state = "A DIFFERENT CONTAINER is at this path now (its id changed)"
		default:
			if m, ok := vdFindMount(e.ContainerID); ok {
				state = "mounted at " + m.Letter
			}
		}
		if tasks[strings.ToLower(e.Name)] {
			if state != "" {
				state += "; "
			}
			state += "mounts automatically"
		}
		if state != "" {
			state = "  [" + state + "]"
		}
		fmt.Printf("%-16s %-6s %8s  %s%s\n", e.Name, e.Profile, vdSize(e.LogicalSize), e.Path, state)
	}
	return nil
}
