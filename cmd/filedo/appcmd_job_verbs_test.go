package main

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// The job verbs of APP-COMMANDS 0.1 (SP-0218): job list, job stop and the
// --stop-jobs option of quit. `job list` only means something with the window
// program running (coldStart refuse); `job stop` and `quit` with none running
// are done with nothing to do (oneshot). The harness and the fake first instance
// are the ones of appcmd_test.go; what the window program does with a request -
// the snapshot, the stop, the wait - is the self-test group appcmd.

// jobsAnswer is what a first instance with a Capacity run in the main window and
// one operation queued in the Disk Manager answers to `job list`.
func jobsAnswer() func(req appCmdRequest) *appCmdResponse {
	return doneAnswer(map[string]interface{}{"jobs": []interface{}{
		map[string]interface{}{"where": "shell", "verb": "test", "state": "running", "step": "write", "percent": 45},
		map[string]interface{}{"where": "manager", "verb": "unmount", "state": "queued", "step": "", "percent": nil},
	}})
}

// TestJobVerbs_ListNotRunningIsExit11: with no first instance `job list` is
// refused 11 with reason not-running, and nothing is started.
func TestJobVerbs_ListNotRunningIsExit11(t *testing.T) {
	h := newAppCmdHarness(t, testPipePath(t))
	if code := runAppCmd(t, "job", "list", "--json"); code != 11 {
		t.Fatalf("exit %d, want 11", code)
	}
	var r appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &r); err != nil || r.Reason != "not-running" || r.Status != "refused" {
		t.Fatalf("job list --json: %q", h.out.String())
	}
	if h.launched != 0 {
		t.Fatal("job list started the window program")
	}
}

// TestJobVerbs_ListPrintsEachRun: the request is the canonical verb with the raw
// arguments; the answer's runs are printed one to a line, and with --json the
// response line itself carries both of them.
func TestJobVerbs_ListPrintsEachRun(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	f := startFakeFirstInstance(t, pipe, jobsAnswer())
	if code := runAppCmd(t, "job", "list"); code != 0 {
		t.Fatalf("exit %d, want 0; err %q", code, h.err.String())
	}
	lines := strings.Split(strings.TrimSpace(h.out.String()), "\n")
	var shell, manager string
	for _, l := range lines {
		switch {
		case strings.Contains(l, "shell"):
			shell = l
		case strings.Contains(l, "manager"):
			manager = l
		}
	}
	for _, want := range []string{"test", "running", "write", "45%"} {
		if !strings.Contains(shell, want) {
			t.Errorf("the run of the main window printed %q, want it to say %q", shell, want)
		}
	}
	if !strings.Contains(manager, "unmount") || !strings.Contains(manager, "queued") || strings.Contains(manager, "%") {
		t.Errorf("the queued operation printed %q, want its verb and state and no percent", manager)
	}

	h.out.Reset()
	if code := runAppCmd(t, "job", "list", "--json"); code != 0 {
		t.Fatalf("--json: exit %d, want 0", code)
	}
	var r appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &r); err != nil {
		t.Fatalf("job list --json: %q", h.out.String())
	}
	if jobs, _ := r.Data["jobs"].([]interface{}); len(jobs) != 2 {
		t.Errorf("data.jobs holds %v, want both runs", r.Data["jobs"])
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 2 || f.requests[0].Command != "job list" || len(f.requests[0].Args) != 0 ||
		!reflect.DeepEqual(f.requests[1].Args, []string{"--json"}) {
		t.Errorf("requests %+v, want job list with no arguments, then with the raw --json", f.requests)
	}
	if h.launched != 0 {
		t.Error("a running first instance was started again")
	}
}

// TestJobVerbs_ListEmptyPrintsNoRunLine: an empty array prints the sentence the
// program sent and no run line, and exits 0.
func TestJobVerbs_ListEmptyPrintsNoRunLine(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, doneAnswer(map[string]interface{}{"jobs": []interface{}{}}))
	if code := runAppCmd(t, "job", "list"); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if strings.TrimSpace(h.out.String()) != "ok" {
		t.Errorf("printed %q, want only the program's sentence", h.out.String())
	}
}

// TestJobVerbs_StopWithNoProgramSaysZero: with no first instance `job stop` is
// done, exit 0, with stopped and cleared both 0 - and again the second time.
func TestJobVerbs_StopWithNoProgramSaysZero(t *testing.T) {
	h := newAppCmdHarness(t, testPipePath(t))
	for i := 0; i < 2; i++ {
		h.out.Reset()
		if code := runAppCmd(t, "job", "stop", "--json"); code != 0 {
			t.Fatalf("run %d: exit %d, want 0; err %q", i+1, code, h.err.String())
		}
		var r appCmdResponse
		if err := json.Unmarshal(h.out.Bytes(), &r); err != nil || r.Status != "done" || r.Data["stopped"] != float64(0) || r.Data["cleared"] != float64(0) {
			t.Fatalf("run %d: job stop --json: %q", i+1, h.out.String())
		}
	}
	h.out.Reset()
	if code := runAppCmd(t, "job", "stop"); code != 0 || !strings.Contains(h.out.String(), "nothing to stop") {
		t.Fatalf("plain job stop: exit %d, printed %q, want 0 and the sentence that there is nothing to stop", code, h.out.String())
	}
	if h.launched != 0 {
		t.Fatal("job stop started the window program")
	}
}

// TestJobVerbs_StopForwardsTheScope: the scope reaches the program as typed; the
// grammar is the program's, so the front does not read it when it forwards.
func TestJobVerbs_StopForwardsTheScope(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	f := startFakeFirstInstance(t, pipe, doneAnswer(map[string]interface{}{"stopped": 1, "cleared": 2}))
	if code := runAppCmd(t, "job", "stop", "--where", "manager"); code != 0 {
		t.Fatalf("exit %d, want 0; err %q", code, h.err.String())
	}
	f.mu.Lock()
	requests := append([]appCmdRequest(nil), f.requests...)
	f.mu.Unlock()
	if len(requests) != 1 || requests[0].Command != "job stop" || !reflect.DeepEqual(requests[0].Args, []string{"--where", "manager"}) {
		t.Fatalf("requests %+v, want one job stop with the raw --where manager", requests)
	}
}

// TestJobVerbs_StopScopeOutsideTheManifestIsExit10: with no program to ask, the
// front reads the scope itself, with the manifest's reading, and refuses a value
// the manifest does not list - nothing is printed as an answer of done.
func TestJobVerbs_StopScopeOutsideTheManifestIsExit10(t *testing.T) {
	h := newAppCmdHarness(t, testPipePath(t))
	if code := runAppCmd(t, "job", "stop", "--where=bogus"); code != 10 {
		t.Fatalf("exit %d, want 10", code)
	}
	if h.out.Len() != 0 || !strings.Contains(h.err.String(), "all, shell, manager") {
		t.Errorf("stdout %q, stderr %q, want the list of scopes on standard error only", h.out.String(), h.err.String())
	}
}

// TestJobVerbs_StopPastTheBoundIsExit2: the program's own answer of a run that
// did not end - failed, timeout, 2 - passes through, with the counts.
func TestJobVerbs_StopPastTheBoundIsExit2(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(req appCmdRequest) *appCmdResponse {
		return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "failed", ExitCode: 2, Reason: "timeout",
			Message: "A run did not end in time.", Data: map[string]interface{}{"stopped": 1, "cleared": 0}}
	})
	if code := runAppCmd(t, "job", "stop", "--json"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	var r appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &r); err != nil || r.Reason != "timeout" || r.Data["stopped"] != float64(1) {
		t.Fatalf("job stop --json: %q", h.out.String())
	}
}

// TestJobVerbs_QuitStopJobs: the option is part of the manifest, so it is read
// here and forwarded as typed; a run in progress without it is the program's busy
// answer, 11, and nothing is started; with no program running both are done.
func TestJobVerbs_QuitStopJobs(t *testing.T) {
	e, _ := appCommandOf([]string{"quit"})
	if e == nil {
		t.Fatal("quit is not in the manifest")
	}
	found := false
	for _, a := range e.Args {
		if a.Name == "stop-jobs" && a.Kind == "bool" && !a.Required {
			found = true
		}
	}
	if !found {
		t.Fatal("quit does not declare the optional bool --stop-jobs")
	}

	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	f := startFakeFirstInstance(t, pipe, func(req appCmdRequest) *appCmdResponse {
		if len(req.Args) == 0 {
			return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "busy", ExitCode: 11, Reason: "busy",
				Message: "A run is in progress in FileDO."}
		}
		return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "done", ExitCode: 0, Message: "FileDO has closed."}
	})
	if code := runAppCmd(t, "quit"); code != 11 {
		t.Fatalf("quit with a run in progress: exit %d, want 11", code)
	}
	if code := runAppCmd(t, "quit", "--stop-jobs"); code != 0 {
		t.Fatalf("quit --stop-jobs: exit %d, want 0; err %q", code, h.err.String())
	}
	f.mu.Lock()
	requests := append([]appCmdRequest(nil), f.requests...)
	f.mu.Unlock()
	if len(requests) != 2 || !reflect.DeepEqual(requests[1].Args, []string{"--stop-jobs"}) || requests[1].Command != "quit" {
		t.Fatalf("requests %+v, want quit twice, the second with the raw --stop-jobs", requests)
	}
	if h.launched != 0 {
		t.Fatal("quit started the window program")
	}

	// No program: both forms are done, nothing started.
	h2 := newAppCmdHarness(t, testPipePath(t))
	for _, args := range [][]string{{"quit"}, {"quit", "--stop-jobs"}} {
		if code := runAppCmd(t, args...); code != 0 {
			t.Fatalf("%q with no program: exit %d, want 0", args, code)
		}
	}
	if h2.launched != 0 {
		t.Fatal("quit with no program started the window program")
	}
}

// TestJobVerbs_ManifestRows: the two verbs and the option have the fields the
// rest of the code relies on - safe, the cold-start modes of the ticket, a bound
// that leaves the program room to wait.
func TestJobVerbs_ManifestRows(t *testing.T) {
	m, err := loadAppCmdManifest()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]appCmdEntry{}
	for _, c := range m.Commands {
		byName[c.Name] = c
	}
	list, stop := byName["job list"], byName["job stop"]
	if list.Risk != "safe" || list.ColdStart != "refuse" {
		t.Errorf("job list is %s/%s, want safe/refuse", list.Risk, list.ColdStart)
	}
	if stop.Risk != "safe" || stop.ColdStart != "oneshot" || stop.TimeoutMs < 10000 {
		t.Errorf("job stop is %s/%s/%d ms, want safe/oneshot and a bound of at least 10 s", stop.Risk, stop.ColdStart, stop.TimeoutMs)
	}
	var where *appCmdArg
	for i := range stop.Args {
		if stop.Args[i].Name == "where" {
			where = &stop.Args[i]
		}
	}
	if where == nil || where.Kind != "enum" || !reflect.DeepEqual(where.Values, []string{"all", "shell", "manager"}) {
		t.Errorf("job stop --where is %+v, want the enum all, shell, manager", where)
	}
}
