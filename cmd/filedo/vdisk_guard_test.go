package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"filedo/statedir"
	"filedo/vdisk"
)

// SP-0080, the shutdown guard: the task's XML, the `_task` step's two shapes,
// the watcher's decisions (the plan, the caps, the report) behind its seams,
// the heartbeat's staleness, and the snapshot's `guard` field.

func TestVD_GuardTaskXML(t *testing.T) {
	const sid = "S-1-5-21-1-2-3-1001"
	x := vdGuardTaskXML(`C:\Program Files\FileDO\filedo.exe`, sid)
	for _, want := range []string{
		"<UserId>" + sid + "</UserId>",
		"<RunLevel>HighestAvailable</RunLevel>",
		"<LogonTrigger>", "<Delay>PT10S</Delay>",
		"<LogonType>InteractiveToken</LogonType>", "<MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>",
		// The watcher is resident: its execution time limit is off.
		"<ExecutionTimeLimit>PT0S</ExecutionTimeLimit>",
		"--no-history vd guard run",
		`<Command>C:\Program Files\FileDO\filedo.exe</Command>`,
		`<WorkingDirectory>C:\Program Files\FileDO</WorkingDirectory>`,
		"filedo vd guard on", "filedo vd guard off",
	} {
		if !strings.Contains(x, want) {
			t.Errorf("the guard task XML lacks %q", want)
		}
	}
	if strings.Count(x, "<UserId>") != 2 {
		t.Error("the trigger or the principal does not name the user")
	}
	// The guard's name is not a mount task of a container: the two shapes of
	// the `_task` step stay apart.
	if _, err := vdTaskContainerName(vdGuardTask); err == nil {
		t.Error("vdTaskContainerName accepts the guard's task name as a mount task")
	}
}

// The elevated `_task` step accepts its two FileDO shapes - a container's
// logon mount and the shutdown guard - and still refuses everything else
// (SP-0080 4; AUD-31-F4 holds).
func TestVD_TaskStep_GuardShape(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	const sid = "S-1-5-21-1-2-3-1001"
	built, err := vdBuildGuardTaskXML(sid)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(built, "vd guard run") || !strings.Contains(built, sid) {
		t.Errorf("the guard task built by the step is not the guard:\n%s", built)
	}
	if _, err := vdBuildGuardTaskXML("not-a-sid"); err == nil {
		t.Error("an invalid owner SID was accepted for the guard")
	}
	// A request that carries XML of its own gets nothing from it: the guard
	// task is built from constants, the executable and the SID.
	hostile := `{"task_name":"\\FileDO\\FileDO Shutdown guard","task_xml":"<Task><Actions><Exec><Command>calc.exe</Command></Exec></Actions></Task>","task_sid":"` + sid + `"}`
	var req vdRequest
	if err := json.Unmarshal([]byte(hostile), &req); err != nil {
		t.Fatal(err)
	}
	built, err = vdBuildGuardTaskXML(req.TaskSID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(built, "calc.exe") {
		t.Fatalf("the guard task carries the request's XML:\n%s", built)
	}
	// Every other spelling is refused by the container-name check before any
	// schtasks call. The accepted shapes are read through their builders
	// alone: a vdTaskStep call with an accepted name would run schtasks and
	// create or delete a real task on this machine.
	for _, name := range []string{
		vdGuardTask + " x",      // the guard's name is exact
		`\FileDO\FileDO Guardx`, // no prefix of the guard's name is one
		`\FileDO\FileDO Mount work extra`,
		`FileDO Shutdown guard`,
		`\FileDO\..\FileDO Shutdown guard`,
		`\FileDO\FileDO Mount ..\..\Evil`,
	} {
		if _, err := vdTaskContainerName(name); err == nil {
			t.Errorf("the step would accept the task name %q", name)
		}
	}
}

// The plan: dirty ram saves first, largest first, then every mounted container
// by letter (SP-0080 3.2).
func TestVD_GuardPlan(t *testing.T) {
	mk := func(letter, profile string, pid int, ro bool, dirty int64) vdGuardTarget {
		tgt := vdGuardTarget{Row: vdMountRow{Letter: letter, Path: `C:\d\` + letter + `.fdd`, Profile: profile, ServerPID: pid, ReadOnly: ro}, ServerAlive: pid != 0, IsRAM: profile == "ram"}
		tgt.Name = letter
		tgt.DirtyBytes = dirty
		return tgt
	}
	targets := []vdGuardTarget{
		mk("C:", "plain", 1, false, 0),
		mk("E:", "ram", 2, false, 100<<20),
		mk("D:", "ram", 3, false, 900<<20), // the largest dirty goes first
		mk("F:", "ram", 4, true, 50<<20),   // read-only: nothing to save
		mk("G:", "ram", 0, false, 70<<20),  // server gone: nothing can be saved
		mk("H:", "ram", 5, false, 0),       // clean ram: the unmount saves it
		mk("I:", "fast", 6, false, 0),
	}
	saves, unmounts := vdGuardPlan(targets)
	var got []string
	for _, s := range saves {
		got = append(got, s.Row.Letter)
	}
	if want := []string{"D:", "E:"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("save order %v, want %v", got, want)
	}
	got = nil
	for _, u := range unmounts {
		got = append(got, u.Row.Letter)
	}
	if want := []string{"C:", "D:", "E:", "F:", "G:", "H:", "I:"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unmount order %v, want %v", got, want)
	}
	// Equal dirt is ordered by letter, so the report reads the same twice.
	same := []vdGuardTarget{mk("Z:", "ram", 1, false, 5), mk("Y:", "ram", 2, false, 5)}
	saves, _ = vdGuardPlan(same)
	if saves[0].Row.Letter != "Y:" {
		t.Errorf("a tie is not ordered by letter: %s then %s", saves[0].Row.Letter, saves[1].Row.Letter)
	}
	// Nothing mounted, nothing to do.
	if s, u := vdGuardPlan(nil); s != nil || u != nil {
		t.Errorf("an empty state plans %+v %+v", s, u)
	}
}

// The runner: the block reason only while a ram save runs, the caps handed to
// each step, the four outcomes, and the report built from a scripted run
// (SP-0080 3.2-3.3).
func TestVD_GuardExecute(t *testing.T) {
	ram := func(letter string, dirty int64) vdGuardTarget {
		return vdGuardTarget{Row: vdMountRow{Letter: letter, Path: `C:\d\` + letter + ".fdd"}, Name: strings.ToLower(letter), DirtyBytes: dirty, ServerAlive: true, IsRAM: dirty > 0}
	}
	var blocks []string
	steps := vdGuardSteps{
		Block: func(on bool) { blocks = append(blocks, map[bool]string{true: "on", false: "off"}[on]) },
		Save: func(tg vdGuardTarget, cap time.Duration) vdGuardRow {
			if cap != vdGuardSaveCap {
				t.Errorf("save of %s got cap %s, want %s", tg.Name, cap, vdGuardSaveCap)
			}
			return vdGuardRow{Name: tg.Name, Path: tg.Row.Path, Action: "save", Outcome: "saved", BytesSaved: tg.DirtyBytes, Seconds: 1}
		},
		Unmount: func(tg vdGuardTarget, cap time.Duration) vdGuardRow {
			if cap != vdGuardUnmountCap {
				t.Errorf("unmount of %s got cap %s, want %s", tg.Name, cap, vdGuardUnmountCap)
			}
			out := "unmounted"
			if tg.Row.Letter == "G:" {
				out = "skipped" // a detach the elevated step refused
			}
			return vdGuardRow{Name: tg.Name, Path: tg.Row.Path, Action: "unmount", Outcome: out}
		},
	}
	at := time.Date(2026, 9, 30, 5, 12, 33, 0, time.UTC)
	// The run is plan then execute, as the watcher does it: the targets go in
	// mounted order, the plan sorts them.
	saves, unmounts := vdGuardPlan([]vdGuardTarget{
		ram("C:", 0), ram("E:", 100<<20), ram("D:", 900<<20), ram("G:", 0)})
	if len(saves) != 2 || len(unmounts) != 4 {
		t.Fatalf("the plan is %d saves, %d unmounts, want 2 and 4", len(saves), len(unmounts))
	}
	rep := vdGuardExecute(saves, unmounts, steps, at)
	if len(blocks) != 2 || blocks[0] != "on" || blocks[1] != "off" {
		t.Errorf("block reasons %v, want on then off, once each", blocks)
	}
	if rep.Schema != vdGuardReportSchema || rep.Version != 1 || time.Time(rep.At) != at || rep.Ended != "session" {
		t.Errorf("the report head is %+v", rep)
	}
	if len(rep.Containers) != 6 {
		t.Fatalf("%d rows, want 6: %+v", len(rep.Containers), rep.Containers)
	}
	if rep.Containers[0].Action != "save" || rep.Containers[0].Name != "d:" || rep.Containers[1].Name != "e:" {
		t.Errorf("the saves are not largest dirty first: %+v", rep.Containers[:2])
	}
	if rep.Containers[0].BytesSaved != 900<<20 {
		t.Errorf("the bytes saved are %+v", rep.Containers[0])
	}
	if rep.Containers[5].Outcome != "skipped" || rep.Containers[5].Name != "g:" {
		t.Errorf("the refused unmount is %+v", rep.Containers[5])
	}
	if !strings.Contains(rep.Summary, "5 of 6") || !strings.Contains(rep.Summary, "1 skipped") {
		t.Errorf("the summary does not name the leftovers: %q", rep.Summary)
	}

	// D3: with no dirty ram disk there is no block reason at all - never for
	// an unmount.
	blocks = nil
	rep = vdGuardExecute(nil, []vdGuardTarget{ram("D:", 0)}, steps, at)
	if len(blocks) != 0 {
		t.Errorf("an unmount-only run touched the block reason: %v", blocks)
	}
	if rep.Summary != "Every mounted container was closed cleanly." {
		t.Errorf("summary %q", rep.Summary)
	}

	// The empty case: nothing mounted, one sentence, no rows.
	rep = vdGuardExecute(nil, nil, steps, at)
	if len(rep.Containers) != 0 || rep.Summary == "" {
		t.Errorf("an empty run is %+v", rep)
	}
	if rep.Summary != "Nothing was mounted; there was nothing to do." {
		t.Errorf("summary %q", rep.Summary)
	}
	// And a run that ran out of time says unfinished, not saved.
	steps2 := steps
	steps2.Save = func(tg vdGuardTarget, cap time.Duration) vdGuardRow {
		return vdGuardRow{Name: tg.Name, Path: tg.Row.Path, Action: "save", Outcome: "unfinished", Reason: "did not finish within 60 s"}
	}
	rep = vdGuardExecute([]vdGuardTarget{ram("E:", 5)}, nil, steps2, at)
	if rep.Summary == "" || !strings.Contains(rep.Summary, "1 unfinished") {
		t.Errorf("an unfinished run is not named: %q", rep.Summary)
	}
}

// The caps are the constants D4 fixed, in one place (SP-0080 9, D4).
func TestVD_GuardCaps(t *testing.T) {
	if vdGuardSaveCap != 60*time.Second {
		t.Errorf("the ram save cap is %s, want 60s", vdGuardSaveCap)
	}
	if vdGuardUnmountCap != 10*time.Second {
		t.Errorf("the unmount cap is %s, want 10s", vdGuardUnmountCap)
	}
	if vdGuardBeatEvery != 5*time.Second || vdGuardStaleAfter != 15*time.Second {
		t.Errorf("the heartbeat is %s touch, %s stale, want 5s and 15s (D8)", vdGuardBeatEvery, vdGuardStaleAfter)
	}
}

// The window procedure: WM_QUERYENDSESSION consents, WM_ENDSESSION with
// wParam runs the session-end work exactly once, and without it does nothing.
func TestVD_GuardWndProc(t *testing.T) {
	ends := 0
	was := vdGuardSessionEnd
	defer func() { vdGuardSessionEnd = was }()
	vdGuardSessionEnd = func() { ends++ }
	if r := vdGuardWndProc(0, vdGuardMsgQueryEndSession, 0, 0); r != 1 {
		t.Errorf("WM_QUERYENDSESSION returned %d, want 1", r)
	}
	vdGuardWndProc(0, vdGuardMsgEndSession, 0, 0) // the session goes on
	if ends != 0 {
		t.Errorf("a WM_ENDSESSION with wParam 0 ended the session %d times", ends)
	}
	vdGuardWndProc(0, vdGuardMsgEndSession, 1, 0)
	if ends != 1 {
		t.Errorf("a WM_ENDSESSION with wParam 1 ended the session %d times, want 1", ends)
	}
}

// The heartbeat (D8): touched within 15 s means running; older, corrupt or
// missing means not. The clock moves, so the test does not sleep.
func TestVD_GuardHeartbeat(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	was := vdGuardNow
	defer func() { vdGuardNow = was }()
	now := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	vdGuardNow = func() time.Time { return now }

	if vdGuardRunning() {
		t.Error("no heartbeat at all reads as running")
	}
	vdGuardTouchBeat()
	if !vdGuardRunning() {
		t.Error("a fresh heartbeat reads as not running")
	}
	now = now.Add(vdGuardStaleAfter)
	if !vdGuardRunning() {
		t.Error("a heartbeat exactly at the stale bound reads as not running")
	}
	now = now.Add(time.Second)
	if vdGuardRunning() {
		t.Error("a heartbeat past the stale bound reads as running")
	}
	// A document that does not parse is no heartbeat.
	if err := os.WriteFile(vdGuardStatePath(vdGuardBeatFile), []byte("not a heartbeat"), 0o600); err != nil {
		t.Fatal(err)
	}
	if vdGuardRunning() {
		t.Error("a corrupt heartbeat reads as running")
	}
}

// The report file: written and read back, refused when it does not parse, and
// carrying none of the machinery (the "never carries" rule of SP-0063 8.1).
func TestVD_GuardReportRoundTrip(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	if _, ok := vdGuardReadReport(); ok {
		t.Fatal("no report at all read as one")
	}
	rep := vdGuardReport{Schema: vdGuardReportSchema, Version: 1,
		At: vdStamp(time.Date(2026, 9, 30, 5, 12, 33, 0, time.UTC)), Ended: "session",
		Containers: []vdGuardRow{
			{Name: "scratch", Path: `C:\vd\scratch.fdd`, Action: "save", Outcome: "saved", BytesSaved: 180 << 20, Seconds: 1.5},
			{Name: "work", Path: `C:\vd\work.fdd`, Action: "unmount", Outcome: "unmounted"},
			{Name: "big", Path: `C:\vd\big.fdd`, Action: "unmount", Outcome: "unfinished", Reason: "did not finish within 10 s"},
		},
		Summary: "2 of 3 containers were closed cleanly, 1 unfinished - the rows above say which and why.",
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if err := statedir.WriteFileAtomic(vdGuardStatePath(vdGuardReportFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	got, ok := vdGuardReadReport()
	if !ok || len(got.Containers) != 3 || got.Containers[0].BytesSaved != 180<<20 || got.Containers[2].Outcome != "unfinished" {
		t.Fatalf("read back %+v", got)
	}
	line := string(b)
	// The "never carries" rule of SP-0063 8.1, on the report's keys: no
	// credential, no port, no process id, no IQN, no session id, no serial.
	// "ended":"session" is the word D7 chose, not a session id.
	forbidden := regexp.MustCompile(`(?i)"[a-z_]*(port|pid|iqn|session|serial|password|secret|cred\w*)[a-z_]*"\s*:`)
	for _, m := range forbidden.FindAllString(line, -1) {
		t.Errorf("the report carries the key %s", m)
	}
	// A file that does not parse is no report.
	if err := os.WriteFile(vdGuardStatePath(vdGuardReportFile), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := vdGuardReadReport(); ok {
		t.Error("a corrupt report read as one")
	}
}

// The snapshot's guard field: present with its state, and the unknown-field
// rule in the other direction - a document without the key reads as an
// uninstalled guard (SP-0080 4).
func TestVD_GuardSnapshot(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	was := vdSnapshotGuard
	defer func() { vdSnapshotGuard = was }()
	called := false
	vdSnapshotGuard = func() vdSnapGuard {
		called = true
		return vdSnapGuard{Installed: true, Running: true}
	}
	out, err := vdTestRun(t, false, "status", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Error("the snapshot did not ask for the guard's state")
	}
	var doc struct {
		Schema string `json:"schema"`
		Guard  *struct {
			Installed  bool             `json:"installed"`
			Running    bool             `json:"running"`
			LastRun    *string          `json:"last_run"`
			Containers []vdSnapGuardRow `json:"containers"`
		} `json:"guard"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Guard == nil || !doc.Guard.Installed || !doc.Guard.Running || doc.Guard.LastRun != nil {
		t.Fatalf("the guard field is %+v, want installed and running with last_run null", doc.Guard)
	}
	// Without the key - an older CLI's document - the reader takes its
	// defaults and claims nothing.
	var old struct {
		Guard vdSnapGuard `json:"guard"`
	}
	if err := json.Unmarshal([]byte(`{"schema":"filedo.vd-status","version":1}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Guard.Installed || old.Guard.Running || !time.Time(old.Guard.LastRun).IsZero() {
		t.Errorf("a snapshot without the key claimed a guard: %+v", old.Guard)
	}
}

// The summary's one sentence, per shape (SP-0080 3.3).
func TestVD_GuardSummary(t *testing.T) {
	row := func(action, outcome string) vdGuardRow {
		return vdGuardRow{Name: "x", Path: `C:\x.fdd`, Action: action, Outcome: outcome}
	}
	for _, c := range []struct {
		rows []vdGuardRow
		want string
	}{
		{nil, "Nothing was mounted; there was nothing to do."},
		{[]vdGuardRow{row("unmount", "unmounted")}, "Every mounted container was closed cleanly."},
		{[]vdGuardRow{row("save", "saved"), row("unmount", "unmounted")}, "Every mounted container was closed cleanly (1 ram disk(s) saved first)."},
	} {
		if got := vdGuardSummary(c.rows); got != c.want {
			t.Errorf("summary of %+v is %q, want %q", c.rows, got, c.want)
		}
	}
	got := vdGuardSummary([]vdGuardRow{row("unmount", "unmounted"), row("save", "skipped"), row("unmount", "unfinished")})
	if !strings.Contains(got, "1 of 3") || !strings.Contains(got, "1 skipped") || !strings.Contains(got, "1 unfinished") {
		t.Errorf("the leftovers sentence is %q", got)
	}
}

// vd guard refuses every spelling but its four words, and never quotes the
// word back (it may be a password). The task query is pointed at "no task", so
// nothing here reads this machine's scheduler or asks for consent.
func TestVD_GuardGrammar(t *testing.T) {
	vdTestEnv(t)
	was := vdGuardHasTask
	defer func() { vdGuardHasTask = was }()
	vdGuardHasTask = func() bool { return false }
	for _, args := range [][]string{
		{"guard"},
		{"guard", "on", "extra"},
		{"guard", "hunter2"},
		{"guard", "p:hunter2"},
	} {
		_, err := vdTestRun(t, false, args...)
		if vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("vd %v: class %d %v", args, vdExitClass(err), err)
			continue
		}
		for _, w := range args[1:] {
			if !contains([]string{"on", "off", "extra"}, strings.ToLower(w)) && strings.Contains(err.Error(), w) {
				t.Errorf("vd %v quoted %q back: %v", args, w, err)
			}
		}
	}
	// off without the task is refused as usage, with no elevation asked.
	_, err := vdTestRun(t, false, "guard", "off")
	if vdExitClass(err) != vdisk.ExitUsage || !strings.Contains(err.Error(), "not on") {
		t.Errorf("vd guard off with no task: %v", err)
	}
	// status with no task installed is three honest lines.
	out, err := vdTestRun(t, false, "guard", "status")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"not installed", "filedo vd guard on"} {
		if !strings.Contains(out, want) {
			t.Errorf("vd guard status says nothing about %q:\n%s", want, out)
		}
	}
}

// The watcher's target list joins the state rows with the registry's names and
// the servers' own word about dirt (SP-0080 3.2).
func TestVD_GuardTargets(t *testing.T) {
	dir := vdTestEnv(t)
	alive := vdGuardServerAlive
	defer func() { vdGuardServerAlive = alive }()
	vdGuardServerAlive = func(m vdMountRow) bool { return m.ServerPID != 0 }

	p := filepath.Join(dir, "scratch.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfileRAM, "")
	id := func() string {
		i, err := vdisk.Inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		return i.ContainerID
	}()
	if _, err := vdTestRun(t, false, "add", p, "as", "scratch"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 30, 5, 0, 0, 0, time.UTC)
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = append(s.Mounts, vdMountRow{ContainerID: id, Path: p, Letter: "R:", Profile: "ram",
			ServerPID: 424242, ServerStarted: 133700, Port: 42424, MountedAt: at})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(vdRAMStatus{At: at, DirtyBytes: 180 << 20, LastGoodSave: at})
	if err := os.WriteFile(mustSidePath(t, id, ".status.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	targets, err := vdGuardTargets()
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 {
		t.Fatalf("%d targets, want 1: %+v", len(targets), targets)
	}
	got := targets[0]
	if got.Name != "scratch" || !got.IsRAM || !got.ServerAlive || got.DirtyBytes != 180<<20 || got.Row.Letter != "R:" {
		t.Errorf("the target is %+v", got)
	}
}

func mustSidePath(t *testing.T, containerID, ext string) string {
	t.Helper()
	p, err := vdSidePath(containerID, ext)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
