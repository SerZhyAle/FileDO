package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"filedo/statedir"
	"filedo/vdisk"
)

// SP-0063 M1: `filedo vd status json`, the snapshot the Disk Manager window reads. The golden
// document holds every file state (ok, missing, a different container at the path, unreadable),
// mounted and not, a ram buffer, a block server that is gone, a mount the registry does not name
// and a foreign image. FILEDO_WRITE_VD_STATUS_GOLDEN=1 rewrites testdata/vdstatus/golden.json from
// what the code produces now; the diff is then read, never trusted.

const vdGoldenPath = "testdata/vdstatus/golden.json"

// vdSnapFixture builds the state behind the golden document and returns the state root and the
// folder the containers are in. The machine's parts - scheduled tasks, the initiator service, a
// block server's liveness and the clock - are fixed through the snapshot's seams.
func vdSnapFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := vdTestEnv(t)
	stateDir := os.Getenv("FILEDO_STATE_DIR")

	tasks, transport, alive, now := vdSnapshotTasks, vdSnapshotTransport, vdSnapshotAlive, vdSnapshotNow
	guard := vdSnapshotGuard
	t.Cleanup(func() {
		vdSnapshotTasks, vdSnapshotTransport, vdSnapshotAlive, vdSnapshotNow = tasks, transport, alive, now
		vdSnapshotGuard = guard
	})
	vdSnapshotTasks = func() map[string]bool { return map[string]bool{"archive": true} }
	vdSnapshotTransport = func() vdSnapTransport { return vdSnapTransport{Ready: true, InitiatorService: "running"} }
	// A row with a server process id is alive; the "gone" row has none.
	vdSnapshotAlive = func(m vdMountRow) bool { return m.ServerPID != 0 }
	vdSnapshotNow = func() time.Time { return time.Date(2026, 9, 27, 12, 2, 11, 0, time.UTC) }
	// The shutdown guard (SP-0080): installed and running, with a last run
	// that saved one ram disk and left nothing behind. The other half of the
	// unknown-field rule - a snapshot without the guard key - is an older
	// CLI's document, and the reader takes its defaults (TestVD_GuardSnapshot).
	vdSnapshotGuard = func() vdSnapGuard {
		return vdSnapGuard{Installed: true, Running: true,
			LastRun: vdStamp(time.Date(2026, 9, 27, 5, 12, 33, 0, time.UTC)), Ended: "session",
			Containers: []vdSnapGuardRow{
				{Name: "scratch", Path: `C:\vd\scratch.fdd`, Action: "save", Outcome: "saved", BytesSaved: 180 << 20},
				{Name: "work", Path: `C:\vd\work.fdd`, Action: "unmount", Outcome: "unmounted"},
			}}
	}

	mk := func(name string, profile vdisk.Profile, cred string) string {
		p := filepath.Join(dir, name+".fdd")
		vdTestContainer(t, p, 1<<20, profile, cred)
		return p
	}
	add := func(p, name string) {
		if _, err := vdTestRun(t, false, "add", p, "as", name); err != nil {
			t.Fatalf("vd add %s: %v", name, err)
		}
	}
	id := func(p string) string {
		i, err := vdisk.Inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		return i.ContainerID
	}

	work := mk("work", vdisk.ProfilePlain, "")
	add(work, "work")
	secrets := mk("secrets", vdisk.ProfileVault, "selftest-vd-secret")
	add(secrets, "secrets")
	scratch := mk("scratch", vdisk.ProfileRAM, "")
	add(scratch, "scratch")
	old := mk("old", vdisk.ProfilePlain, "")
	add(old, "old")
	archive := mk("archive", vdisk.ProfilePlain, "")
	add(archive, "archive")

	// Registered, then the file went away.
	backup := mk("backup", vdisk.ProfilePlain, "")
	add(backup, "backup")
	if err := os.Remove(backup); err != nil {
		t.Fatal(err)
	}
	// Registered, then another container took its path.
	moved := mk("moved", vdisk.ProfilePlain, "")
	add(moved, "moved")
	if err := os.Remove(moved); err != nil {
		t.Fatal(err)
	}
	vdTestContainer(t, moved, 2<<20, vdisk.ProfileFast, "")
	// Registered by hand (vd add would refuse it), and its header does not read.
	junk := filepath.Join(dir, "junk.fdd")
	if err := os.WriteFile(junk, []byte("not a container at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		r.Containers = append(r.Containers, vdRegEntry{Name: "junk", Path: junk, ContainerID: "00000000-0000-4000-8000-000000000001",
			Profile: "plain", LogicalSize: 5 << 20, Added: vdSnapshotNow()})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Mounted by path, never registered.
	loose := mk("loose", vdisk.ProfilePlain, "")

	at := time.Date(2026, 9, 27, 9, 12, 40, 0, time.UTC)
	mount := func(p, letter, profile string, ro bool, pid int) vdMountRow {
		return vdMountRow{ContainerID: id(p), Path: p, Letter: letter, ReadOnly: ro, MountedAt: at, ServerPID: pid,
			ServerStarted: 133700, Port: 42424, IQN: vdIQN(id(p)), Serial: "SERIAL-SNAP-1", Session: "SESSION-SNAP-1", Profile: profile}
	}
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = append(s.Mounts,
			mount(work, "W:", "plain", false, 424241),
			mount(secrets, "X:", "vault", true, 424242),
			mount(scratch, "R:", "ram", false, 424243),
			mount(old, "S:", "plain", false, 0),
			mount(loose, "U:", "plain", false, 424244))
		s.Images = append(s.Images, vdImageRow{Letter: "I:", Path: filepath.Join(dir, "disc.iso"), MountedAt: at})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ram, err := vdSidePath(id(scratch), ".status.json")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(vdRAMStatus{At: at, DirtyBytes: 180 << 20, LastGoodSave: at})
	if err := os.WriteFile(ram, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return stateDir, dir
}

// vdSnapNormalize turns a snapshot line into the golden form: the folder of the fixture is <DIR>,
// every container id is <ID n> in order of appearance, and every time - after it is checked to be
// RFC 3339 - is <TIME>, because the fixture's times are written in the machine's zone.
func vdSnapNormalize(t *testing.T, line, dir string) string {
	t.Helper()
	var doc interface{}
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatalf("the snapshot is not JSON: %v\n%s", err, line)
	}
	ids := map[string]string{}
	stamp := regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(Z|[+-]\d\d:\d\d)$`)
	var walk func(key string, v interface{}) interface{}
	walk = func(key string, v interface{}) interface{} {
		switch x := v.(type) {
		case map[string]interface{}:
			for k, inner := range x {
				x[k] = walk(k, inner)
			}
			return x
		case []interface{}:
			for i, inner := range x {
				x[i] = walk(key, inner)
			}
			return x
		case string:
			switch key {
			case "at", "mounted_at", "last_good_save", "last_run":
				if !stamp.MatchString(x) {
					t.Errorf("%s is not an RFC 3339 time to the second: %q", key, x)
				}
				return "<TIME>"
			case "container_id":
				if _, ok := ids[x]; !ok {
					ids[x] = fmt.Sprintf("<ID %d>", len(ids)+1)
				}
				return ids[x]
			}
			return strings.ReplaceAll(x, dir, "<DIR>")
		}
		return v
	}
	doc = walk("", doc)
	var out strings.Builder
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(out.String(), "\r\n", "\n")
}

func vdStatusLine(t *testing.T) string {
	t.Helper()
	out, err := vdTestRun(t, false, "status", "json")
	if err != nil {
		t.Fatalf("vd status json: %v", err)
	}
	lines := strings.Split(strings.TrimRight(out, "\r\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("vd status json printed %d lines, want one line of JSON:\n%s", len(lines), out)
	}
	return lines[0]
}

func TestVD_StatusJSON_Golden(t *testing.T) {
	_, dir := vdSnapFixture(t)
	got := vdSnapNormalize(t, vdStatusLine(t), dir)
	if os.Getenv("FILEDO_WRITE_VD_STATUS_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(vdGoldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(vdGoldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", vdGoldenPath)
		return
	}
	want, err := os.ReadFile(vdGoldenPath)
	if err != nil {
		t.Fatalf("%v (FILEDO_WRITE_VD_STATUS_GOLDEN=1 writes it)", err)
	}
	if got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("vd status json differs from %s.\ngot:\n%s", vdGoldenPath, got)
	}
}

// The rows' states, asserted by name beside the golden file, so a golden rewritten carelessly
// still has to say these things.
func TestVD_StatusJSON_States(t *testing.T) {
	_, _ = vdSnapFixture(t)
	var doc struct {
		Schema    string          `json:"schema"`
		Version   int             `json:"version"`
		At        string          `json:"at"`
		Transport vdSnapTransport `json:"transport"`
		Disks     []struct {
			Kind       string `json:"kind"`
			Name       string `json:"name"`
			Path       string `json:"path"`
			Registered bool   `json:"registered"`
			File       string `json:"file"`
			FileError  string `json:"file_error"`
			Profile    string `json:"profile"`
			Protection string `json:"protection"`
			Clean      *bool  `json:"clean"`
			Auto       bool   `json:"auto"`
			Letter     string `json:"letter"`
			Mount      *struct {
				Letter      string `json:"letter"`
				ReadOnly    bool   `json:"read_only"`
				MountedAt   string `json:"mounted_at"`
				ServerAlive bool   `json:"server_alive"`
				RAM         *struct {
					DirtyBytes int64 `json:"dirty_bytes"`
				} `json:"ram"`
			} `json:"mount"`
		} `json:"disks"`
	}
	if err := json.Unmarshal([]byte(vdStatusLine(t)), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Schema != "filedo.vd-status" || doc.Version != 1 || !doc.Transport.Ready {
		t.Fatalf("head: %s %d %+v", doc.Schema, doc.Version, doc.Transport)
	}
	if want := time.Date(2026, 9, 27, 12, 2, 11, 0, time.UTC).Local().Format(time.RFC3339); doc.At != want {
		t.Errorf("at %q, want %q", doc.At, want)
	}
	byName := map[string]int{}
	for i, d := range doc.Disks {
		key := d.Name
		if d.Kind == "image" {
			key = "(image)"
		} else if !d.Registered {
			key = "(loose)"
		}
		byName[key] = i
	}
	get := func(name string) int {
		i, ok := byName[name]
		if !ok {
			t.Fatalf("no row %s in %+v", name, byName)
		}
		return i
	}
	for _, c := range []struct {
		name, file, profile, protection string
	}{
		{"work", "ok", "plain", "obfuscated"},
		{"secrets", "ok", "vault", "encrypted"},
		{"scratch", "ok", "ram", "obfuscated"},
		{"archive", "ok", "plain", "obfuscated"},
		{"backup", "missing", "plain", ""},
		{"moved", "different", "plain", ""},
		{"junk", "unreadable", "plain", ""},
		{"(loose)", "ok", "plain", "obfuscated"},
	} {
		d := doc.Disks[get(c.name)]
		if d.File != c.file || d.Profile != c.profile || d.Protection != c.protection {
			t.Errorf("%s: file %s profile %s protection %q, want %s %s %q", c.name, d.File, d.Profile, d.Protection, c.file, c.profile, c.protection)
		}
	}
	if d := doc.Disks[get("junk")]; d.FileError == "" || d.Clean != nil {
		t.Errorf("junk: an unreadable header says why and claims nothing about clean: %+v", d)
	}
	if d := doc.Disks[get("archive")]; d.Mount != nil || d.Clean == nil || !*d.Clean || !d.Auto {
		t.Errorf("archive: not mounted, closed cleanly, auto: %+v", d)
	}
	if d := doc.Disks[get("secrets")]; d.Mount == nil || !d.Mount.ReadOnly || d.Mount.Letter != "X:" || !d.Mount.ServerAlive {
		t.Errorf("secrets: mounted read-only at X:, alive: %+v", d.Mount)
	}
	if d := doc.Disks[get("scratch")]; d.Mount == nil || d.Mount.RAM == nil || d.Mount.RAM.DirtyBytes != 180<<20 {
		t.Errorf("scratch: 180 MiB not saved: %+v", d.Mount)
	}
	if d := doc.Disks[get("old")]; d.Mount == nil || d.Mount.ServerAlive || d.Mount.RAM != nil {
		t.Errorf("old: mounted with its server gone: %+v", d.Mount)
	}
	if d := doc.Disks[get("(loose)")]; d.Registered || d.Mount == nil || d.Mount.Letter != "U:" {
		t.Errorf("loose: an unregistered mount is a row of its own: %+v", d)
	}
	if d := doc.Disks[get("(image)")]; d.Letter != "I:" || !strings.HasSuffix(d.Path, "disc.iso") {
		t.Errorf("image: %+v", d)
	}
	if len(doc.Disks) != 10 {
		t.Errorf("%d rows, want 10 (8 registered, 1 loose, 1 image)", len(doc.Disks))
	}
}

// The "never carries" list of SP-0063 8.1: no credential, port, process id, IQN, session id or
// serial - not as a key, and not as a value the state file holds.
func TestVD_StatusJSON_NeverCarriesTheMachinery(t *testing.T) {
	stateDir, _ := vdSnapFixture(t)
	line := vdStatusLine(t)
	var doc interface{}
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		t.Fatal(err)
	}
	// Matched on the key's words, so "transport" is not a port and "server_pid" is a pid.
	forbidden := regexp.MustCompile(`(?i)(^|_)(port|pid|iqn|session|serial|password|secret|keyfile|token|cred\w*)(_|$)`)
	for _, k := range []string{"server_pid", "port", "iqn", "session", "serial", "credential", "cred"} {
		if !forbidden.MatchString(k) {
			t.Fatalf("the key rule misses %q, a key vdisk-state.json has or a credential would", k)
		}
	}
	var keys []string
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch x := v.(type) {
		case map[string]interface{}:
			for k, inner := range x {
				keys = append(keys, k)
				walk(inner)
			}
		case []interface{}:
			for _, inner := range x {
				walk(inner)
			}
		}
	}
	walk(doc)
	sort.Strings(keys)
	for _, k := range keys {
		if forbidden.MatchString(k) {
			t.Errorf("the snapshot carries the key %q", k)
		}
	}
	for _, v := range []string{"42424", "424241", "424242", "133700", "iqn.2026-09", "SERIAL-SNAP-1", "SESSION-SNAP-1", "selftest-vd-secret"} {
		if strings.Contains(line, v) {
			t.Errorf("the snapshot carries %q", v)
		}
	}
	// And the state file really does hold them, so the test above is not vacuous.
	b, err := os.ReadFile(filepath.Join(stateDir, vdStateFile))
	if err != nil || !strings.Contains(string(b), "SESSION-SNAP-1") || !strings.Contains(string(b), "42424") {
		t.Fatalf("the fixture's state file does not hold the machinery: %v", err)
	}
}

// `json` is a bare word after status and nowhere else; anything else after status is refused and
// never quoted, since it may be a password.
func TestVD_StatusJSON_Grammar(t *testing.T) {
	vdTestEnv(t)
	for _, args := range [][]string{{"status", "json"}, {"status", "JSON"}} {
		out, err := vdTestRun(t, false, args...)
		if err != nil || !strings.HasPrefix(out, `{"schema":"filedo.vd-status","version":1,`) {
			t.Errorf("vd %v: %v %q", args, err, out)
		}
	}
	for _, args := range [][]string{{"status", "xml"}, {"status", "json", "hunter2"}, {"status", "hunter2", "json"}, {"list", "json"}} {
		_, err := vdTestRun(t, false, args...)
		if vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("vd %v: class %d %v", args, vdExitClass(err), err)
			continue
		}
		for _, w := range args[1:] {
			if w != "json" && w != "JSON" && strings.Contains(err.Error(), w) {
				t.Errorf("vd %v quoted %q back: %v", args, w, err)
			}
		}
	}
	// Nothing registered and nothing mounted is an empty list, never a missing one.
	out, _ := vdTestRun(t, false, "status", "json")
	if !strings.Contains(out, `"disks":[]`) {
		t.Errorf("an empty state has no empty disks list: %s", out)
	}
}

// In the Store build the mount path does not exist, and the snapshot says so without asking the
// service manager.
func TestVD_StatusJSON_Packaged(t *testing.T) {
	vdTestEnv(t)
	was, transport := vdPackaged, vdSnapshotTransport
	defer func() { vdPackaged, vdSnapshotTransport = was, transport }()
	vdPackaged = func() bool { return true }
	vdSnapshotTransport = func() vdSnapTransport {
		t.Error("the packaged snapshot asked for the initiator service")
		return vdSnapTransport{}
	}
	out, err := vdTestRun(t, false, "status", "json")
	if err != nil {
		t.Fatal(err)
	}
	var doc vdSnapshotProbe
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if !doc.Packaged || doc.Transport.Ready || doc.Transport.Reason != "packaged" {
		t.Errorf("packaged: %+v", doc)
	}
}

type vdSnapshotProbe struct {
	Packaged  bool            `json:"packaged"`
	Transport vdSnapTransport `json:"transport"`
}

// The document is the whole of stdout: no banner and no finish line, so a script's
// `filedo vd status json | ConvertFrom-Json` reads it - and the run still ends with its result
// event and exit code 0, as every run does.
func TestVD_StatusJSON_IsTheWholeOfStdout(t *testing.T) {
	wd := t.TempDir()
	events := filepath.Join(wd, "events.jsonl")
	cmd := exec.Command(filedoExe, "--events", events, "--no-history", "vd", "status", "json")
	cmd.Dir = wd
	cmd.Env = append(os.Environ(), statedir.EnvOverride+"="+wd)
	cmd.Stdin = strings.NewReader("")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("filedo vd status json: %v\n%s%s", err, stdout.String(), stderr.String())
	}
	out := strings.TrimRight(stdout.String(), "\r\n")
	if strings.Contains(out, "\n") || !strings.HasPrefix(out, `{"schema":"filedo.vd-status",`) {
		t.Errorf("stdout is not one line of JSON:\n%s", stdout.String())
	}
	if got := lastResult(t, events)["verdict"]; got != "Passed" {
		t.Errorf("verdict %v, want Passed", got)
	}
}

func TestVD_StatusJSON_MachineOutputLine(t *testing.T) {
	for _, c := range []struct {
		argv []string
		want bool
	}{
		{[]string{"filedo.exe", "vd", "status", "json"}, true},
		{[]string{"filedo.exe", "--no-history", "VDISK", "Status", "JSON"}, true},
		{[]string{"filedo.exe", "--events", "e.jsonl", "--stop-file", "s.stop", "vd", "status", "json"}, true},
		{[]string{"filedo.exe", "--events=e.jsonl", "vd", "status", "json", "--pause"}, true},
		{[]string{"filedo.exe", "vd", "status"}, false},
		{[]string{"filedo.exe", "vd", "list", "json"}, false},
		{[]string{"filedo.exe", "vd", "status", "json", "x"}, false},
		{[]string{"filedo.exe", "status", "json"}, false},
		{[]string{"filedo.exe"}, false},
	} {
		if got := vdMachineOutput(c.argv); got != c.want {
			t.Errorf("vdMachineOutput(%v) = %v, want %v", c.argv, got, c.want)
		}
	}
}

// `vd auto` names a scheduled task after the registry name, and the Disks pages name a container
// by its file: a registered container's path means its name (the auto page and the list's "turn
// off auto-mount" pass the path).
func TestVD_RegisteredNameOfAPath(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "work.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	if _, err := vdTestRun(t, false, "add", p, "as", "work"); err != nil {
		t.Fatal(err)
	}
	for arg, want := range map[string]string{
		"work": "work", "WORK": "work", p: "work", strings.ToUpper(p): "work",
		filepath.Join(dir, "other.fdd"): "", "nobody": "",
	} {
		if got := vdRegisteredName(arg); got != want {
			t.Errorf("vdRegisteredName(%q) = %q, want %q", arg, got, want)
		}
	}
}
