package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Microsoft/go-winio"

	"filedo/statedir"
)

// The app-command front (APP-COMMANDS 0.1, appcmd.go). Each test names the
// conformance case of APP-COMMANDS section 4 it carries. The pipe of a fake
// first instance is a fresh name per test, never the user's real command pipe,
// and the launcher seam never starts a real window.

// appCmdHarness swaps the seams for one test and captures what is printed.
type appCmdHarness struct {
	out, err   bytes.Buffer
	launched   int
	launchArgs []string
}

func newAppCmdHarness(t *testing.T, pipe string) *appCmdHarness {
	t.Helper()
	h := &appCmdHarness{}
	savedPath, savedLaunch, savedOut, savedErr := appCmdPipePath, appCmdLaunch, appCmdOut, appCmdErr
	appCmdPipePath = func() string { return pipe }
	appCmdLaunch = func(args ...string) error { h.launched++; h.launchArgs = args; return nil }
	appCmdOut, appCmdErr = &h.out, &h.err
	t.Cleanup(func() {
		appCmdPipePath, appCmdLaunch, appCmdOut, appCmdErr = savedPath, savedLaunch, savedOut, savedErr
	})
	return h
}

func testPipePath(t *testing.T) string {
	return fmt.Sprintf(`\\.\pipe\FileDO_Commands_test_%d_%s`, os.Getpid(), strings.ReplaceAll(newRequestID(), "-", ""))
}

// runAppCmd runs one command line the way main() does.
func runAppCmd(t *testing.T, args ...string) int {
	t.Helper()
	e, rest := appCommandOf(args)
	if e == nil {
		t.Fatalf("%q is not an app command", args)
	}
	return appCommandMain(e, rest)
}

// fakeFirstInstance answers each request with what answer returns; a nil
// answer holds the connection open and never replies.
type fakeFirstInstance struct {
	l        net.Listener
	mu       sync.Mutex
	requests []appCmdRequest
}

func startFakeFirstInstance(t *testing.T, pipe string, answer func(req appCmdRequest) *appCmdResponse) *fakeFirstInstance {
	t.Helper()
	l, err := winio.ListenPipe(pipe, nil)
	if err != nil {
		t.Fatalf("listen %s: %v", pipe, err)
	}
	f := &fakeFirstInstance{l: l}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				line, err := bufio.NewReader(c).ReadBytes('\n')
				if err != nil {
					c.Close()
					return
				}
				var req appCmdRequest
				_ = json.Unmarshal(line, &req)
				f.mu.Lock()
				f.requests = append(f.requests, req)
				f.mu.Unlock()
				resp := answer(req)
				if resp == nil {
					time.Sleep(30 * time.Second)
					c.Close()
					return
				}
				out, _ := json.Marshal(resp)
				_, _ = c.Write(append(out, '\n'))
				c.Close()
			}(c)
		}
	}()
	return f
}

func doneAnswer(data map[string]interface{}) func(req appCmdRequest) *appCmdResponse {
	return func(req appCmdRequest) *appCmdResponse {
		return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "done", ExitCode: 0, Message: "ok", Data: data}
	}
}

// C1: the embedded manifest is the shipped file, byte for byte, and every
// command carries the members of rule 18 with a cold-start mode.
func TestAppCommands_C1_EmbeddedManifestEqualsDocsFile(t *testing.T) {
	docs, err := os.ReadFile(filepath.Join("..", "..", "docs", "cli-commands.json"))
	if err != nil {
		t.Fatalf("read docs/cli-commands.json: %v", err)
	}
	norm := func(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }
	if !bytes.Equal(norm(docs), norm(appCmdManifestBytes)) {
		t.Fatal("cmd/filedo/cli-commands.json differs from docs/cli-commands.json; copy the docs file over it")
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(appCmdManifestBytes, &raw); err != nil {
		t.Fatalf("the manifest does not parse: %v", err)
	}
	for _, k := range []string{"schemaVersion", "product", "exe", "cliVersion", "commands"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("the manifest has no %s", k)
		}
	}
	m, _ := loadAppCmdManifest()
	names := map[string]bool{}
	for _, c := range raw["commands"].([]interface{}) {
		entry := c.(map[string]interface{})
		for _, k := range []string{"name", "aliases", "summary", "synopsis", "args", "returns", "risk", "coldStart", "wait", "timeoutMs", "interactive", "idempotent", "exitCodes", "examples", "since"} {
			if _, ok := entry[k]; !ok {
				t.Errorf("command %v has no %s", entry["name"], k)
			}
		}
		names[entry["name"].(string)] = true
	}
	for _, base := range []string{"commands", "version", "status", "show", "quit"} {
		if !names[base] {
			t.Errorf("base command %s is missing (rule 17)", base)
		}
	}
	for _, c := range m.Commands {
		switch c.ColdStart {
		case "start", "oneshot", "refuse":
		default:
			t.Errorf("%s: coldStart %q is not start, oneshot or refuse", c.Name, c.ColdStart)
		}
		switch c.Risk {
		case "safe", "reads-user-data", "writes-user-data", "leaves-machine":
		default:
			t.Errorf("%s: risk %q is not a class of rule 13", c.Name, c.Risk)
		}
		if c.isDeveloper() && !c.isForwardNever() {
			t.Errorf("%s: developer verb must declare forward: never", c.Name)
		}
	}
	for _, never := range []string{"commands", "version", "open", "selftest run", "help-pages build", "screens capture", "menu-icons write", "ui-drive run"} {
		e, _ := appCommandOf(strings.Fields(never))
		for i := range m.Commands {
			if m.Commands[i].Name == never {
				e = &m.Commands[i]
			}
		}
		if e == nil || !e.isForwardNever() {
			t.Errorf("%s: expected forward: never in manifest", never)
		}
	}
	if len(m.Families) < 3 {
		t.Fatalf("families: got %d, want at least 3 (target-first, fdsec, vd)", len(m.Families))
	}
	for _, f := range m.Families {
		if f.Name == "" || f.Summary == "" || f.Exe == "" || f.Help == "" || f.ExitContract == "" || f.EventContract == "" {
			t.Errorf("incomplete family entry %+v", f)
		}
	}
}

// C1: `commands --json` with no first instance prints the shipped manifest,
// exits 0 and starts nothing; `commands` hides developer verbs unless --developer
// is given and prints families[] underneath.
func TestAppCommands_C1_CommandsJSONStartsNoWindow(t *testing.T) {
	h := newAppCmdHarness(t, testPipePath(t))
	if code := runAppCmd(t, "commands", "--json"); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if h.launched != 0 {
		t.Fatal("commands --json started the window program")
	}
	if !bytes.Equal(bytes.TrimSpace(h.out.Bytes()), bytes.TrimSpace(appCmdManifestBytes)) {
		t.Fatal("commands --json did not print the embedded manifest")
	}
	h.out.Reset()
	if code := runAppCmd(t, "COMMANDS"); code != 0 || !strings.Contains(h.out.String(), "filedo status") {
		t.Fatalf("commands: exit %d, output %q", code, h.out.String())
	}
	if !strings.Contains(h.out.String(), "filedo_win.exe open") || !strings.Contains(h.out.String(), "default for filedo_win.exe") {
		t.Fatalf("plain commands omitted open defaultFor mark: %q", h.out.String())
	}
	if strings.Contains(h.out.String(), "selftest run") {
		t.Fatalf("plain commands leaked developer verb: %q", h.out.String())
	}
	for _, fam := range []string{"target-first", "fdsec", "vd"} {
		if !strings.Contains(h.out.String(), fam) {
			t.Errorf("plain commands omitted family %s", fam)
		}
	}
	h.out.Reset()
	if code := runAppCmd(t, "commands", "--developer"); code != 0 || !strings.Contains(h.out.String(), "selftest run") {
		t.Fatalf("commands --developer: exit %d, output %q", code, h.out.String())
	}
}

// C2: `version` exits 0; `status` with none running exits 11, not-running.
func TestAppCommands_C2_VersionAndStatusNotRunning(t *testing.T) {
	h := newAppCmdHarness(t, testPipePath(t))
	if code := runAppCmd(t, "version", "--json"); code != 0 {
		t.Fatalf("version: exit %d", code)
	}
	var v appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &v); err != nil || v.Data["product"] != "FileDO" || v.Data["cliVersion"] == "" {
		t.Fatalf("version --json: %q", h.out.String())
	}
	h.out.Reset()
	if code := runAppCmd(t, "status", "--json"); code != 11 {
		t.Fatalf("status: exit %d, want 11", code)
	}
	var s appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &s); err != nil || s.Reason != "not-running" || s.Status != "refused" {
		t.Fatalf("status --json: %q", h.out.String())
	}
	if h.launched != 0 {
		t.Fatal("status started the window program")
	}
}

// C3 (the second process's half): the command is forwarded once, with the
// canonical verb and the raw arguments, and the process exits with the
// answer's code only after the answer arrived.
func TestAppCommands_C3_ForwardsCommandAndRawArgs(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	f := startFakeFirstInstance(t, pipe, doneAnswer(map[string]interface{}{"pid": 4242}))
	if code := runAppCmd(t, "Status", "--json"); code != 0 {
		t.Fatalf("exit %d, want 0; out %q err %q", code, h.out.String(), h.err.String())
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 {
		t.Fatalf("%d requests, want 1", len(f.requests))
	}
	r := f.requests[0]
	if r.SchemaVersion != 1 || r.Command != "status" || r.Origin != "cli" || r.Wait != "done" || r.RequestID == "" || r.WorkingDir == "" {
		t.Errorf("request %+v", r)
	}
	if !reflect.DeepEqual(r.Args, []string{"--json"}) {
		t.Errorf("args %q, want the raw [--json]", r.Args)
	}
	var resp appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &resp); err != nil || resp.Data["pid"] != float64(4242) {
		t.Errorf("printed %q", h.out.String())
	}
	if h.launched != 0 {
		t.Error("a running first instance was started again")
	}
}

// C4: an unknown verb, an unknown option and an oversized request exit 10 and
// execute nothing; a following status still exits 0.
func TestAppCommands_C4_UsageRefusalsLeaveTheInstanceUsable(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	refuseUnknown := func(req appCmdRequest) *appCmdResponse {
		for _, a := range req.Args {
			if strings.HasPrefix(a, "--") && !strings.EqualFold(a, "--json") {
				return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "refused", ExitCode: 10, Reason: "bad-argument", Message: "unknown option"}
			}
		}
		return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "done", ExitCode: 0}
	}
	f := startFakeFirstInstance(t, pipe, refuseUnknown)

	if e, _ := appCommandOf([]string{"frobnicate"}); e != nil {
		t.Fatal("an unknown word was taken for a verb")
	}
	if code := runAppCmd(t, "status", "--bogus"); code != 10 {
		t.Errorf("unknown option: exit %d, want 10", code)
	}
	big := strings.Repeat("x", 65*1024)
	if code := runAppCmd(t, "status", "--json="+big); code != 10 {
		t.Errorf("65 KiB request: exit %d, want 10", code)
	}
	parts := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		parts = append(parts, strings.Repeat("y", 30*1024))
	}
	if code := runAppCmd(t, append([]string{"status"}, parts...)...); code != 10 {
		t.Errorf("a request of 90 KiB in three arguments: exit %d, want 10", code)
	}
	f.mu.Lock()
	sent := len(f.requests)
	f.mu.Unlock()
	if sent != 1 {
		t.Errorf("%d requests reached the first instance, want 1 (only the unknown option, which it refuses)", sent)
	}
	if code := runAppCmd(t, "status"); code != 0 {
		t.Errorf("status after the refusals: exit %d, want 0; err %q", code, h.err.String())
	}

	// With no first instance the front reads the options itself, from the same manifest.
	h2 := newAppCmdHarness(t, testPipePath(t))
	if code := runAppCmd(t, "quit", "--bogus"); code != 10 {
		t.Errorf("cold quit --bogus: exit %d, want 10", code)
	}
	if code := runAppCmd(t, "commands", "extra"); code != 10 {
		t.Errorf("commands extra: exit %d, want 10", code)
	}
	if h2.launched != 0 {
		t.Error("a refused command started the window program")
	}
}

// C5: a first instance that holds the pipe and never answers, and one whose
// only connection is taken: the second process returns inside its bounds with
// exit 2 or 11, and never hangs.
func TestAppCommands_C5_HungFirstInstanceNeverHangsTheCaller(t *testing.T) {
	pipe := testPipePath(t)
	newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(appCmdRequest) *appCmdResponse { return nil })

	e, _ := appCommandOf([]string{"status"})
	start := time.Now()
	code := runAppCmd(t, "status")
	took := time.Since(start)
	if code != 2 && code != 11 {
		t.Fatalf("exit %d, want 2 or 11", code)
	}
	if bound := time.Duration(appCmdTimeout(e))*time.Millisecond + appCmdDeliveryBound + time.Second; took > bound {
		t.Fatalf("returned after %v, bound %v", took, bound)
	}
}

// C5: a pipe whose one instance is taken by somebody else is busy within the
// delivery bound.
func TestAppCommands_C5_TakenPipeIsBusyWithinTheDeliveryBound(t *testing.T) {
	pipe := testPipePath(t)
	newAppCmdHarness(t, pipe)
	// One instance, held by a client of our own that never sends.
	l, err := winio.ListenPipe(pipe, &winio.PipeConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err == nil {
			time.Sleep(10 * time.Second)
			c.Close()
		}
	}()
	timeout := 500 * time.Millisecond
	holder, err := winio.DialPipe(pipe, &timeout)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	time.Sleep(50 * time.Millisecond)

	start := time.Now()
	code := runAppCmd(t, "status")
	took := time.Since(start)
	if code != 11 && code != 2 {
		t.Fatalf("exit %d, want 11 (busy) or 2", code)
	}
	if took > 3*time.Second {
		t.Fatalf("returned after %v", took)
	}
}

// C7: an answer that does not come inside the verb's timeoutMs is exit 2,
// reason timeout - never done.
func TestAppCommands_C7_NoAnswerInTimeIsTimeout(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(appCmdRequest) *appCmdResponse { return nil })
	if code := runAppCmd(t, "status", "--json"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	var r appCmdResponse
	if err := json.Unmarshal(h.out.Bytes(), &r); err != nil || r.Reason != "timeout" || r.Status == "done" {
		t.Fatalf("printed %q", h.out.String())
	}
}

// C7: an answer to somebody else's request is not this command's answer.
func TestAppCommands_C7_AnswerToAnotherRequestIsNotDone(t *testing.T) {
	pipe := testPipePath(t)
	newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(req appCmdRequest) *appCmdResponse {
		return &appCmdResponse{SchemaVersion: 1, RequestID: "00000000-0000-4000-8000-000000000000", Status: "done"}
	})
	if code := runAppCmd(t, "status"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// C8 (the second process's half): a refusal of the first instance - switch-off
// and the rest - comes out with its own exit code and message.
func TestAppCommands_C8_RefusalExitCodePassesThrough(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(req appCmdRequest) *appCmdResponse {
		return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "refused", ExitCode: 12, Reason: "switch-off", Message: "Turn the setting on."}
	})
	if code := runAppCmd(t, "show"); code != 12 {
		t.Fatalf("exit %d, want 12", code)
	}
	if !strings.Contains(h.err.String(), "Turn the setting on.") {
		t.Fatalf("stderr %q", h.err.String())
	}
}

// C10: one test per declared coldStart mode with no first instance.
func TestAppCommands_C10_ColdStartModes(t *testing.T) {
	t.Run("refuse", func(t *testing.T) {
		h := newAppCmdHarness(t, testPipePath(t))
		if code := runAppCmd(t, "status"); code != 11 || h.launched != 0 {
			t.Fatalf("exit %d launched %d, want 11 and nothing started", code, h.launched)
		}
	})
	t.Run("oneshot", func(t *testing.T) {
		h := newAppCmdHarness(t, testPipePath(t))
		if code := runAppCmd(t, "quit"); code != 0 || h.launched != 0 {
			t.Fatalf("exit %d launched %d, want 0 and nothing started", code, h.launched)
		}
		if code := runAppCmd(t, "version"); code != 0 || h.launched != 0 {
			t.Fatalf("version: exit %d launched %d", code, h.launched)
		}
	})
	t.Run("start", func(t *testing.T) {
		pipe := testPipePath(t)
		h := newAppCmdHarness(t, pipe)
		var f *fakeFirstInstance
		appCmdLaunch = func(args ...string) error {
			h.launched++
			f = startFakeFirstInstance(t, pipe, doneAnswer(nil))
			return nil
		}
		if code := runAppCmd(t, "show"); code != 0 {
			t.Fatalf("exit %d, want 0; err %q", code, h.err.String())
		}
		if h.launched != 1 || f == nil || len(f.requests) != 1 || f.requests[0].Command != "show" {
			t.Fatalf("launched %d, the started instance was not asked to show", h.launched)
		}
	})
	t.Run("start without the window program", func(t *testing.T) {
		h := newAppCmdHarness(t, testPipePath(t))
		appCmdLaunch = func(args ...string) error { return fmt.Errorf("filedo_win.exe not found") }
		if code := runAppCmd(t, "show"); code != 11 {
			t.Fatalf("exit %d, want 11", code)
		}
		if !strings.Contains(h.err.String(), "filedo_win.exe") {
			t.Fatalf("stderr %q", h.err.String())
		}
	})
}

// C13 and the frozen aliases: `ui`, `gui`, `-ui`, `dm`, `-dm` and the empty
// argument list are not taken by the manifest front, so they keep the start
// they always made (TestWindowVerbsLaunchTheRightWindow holds what that start is).
func TestAppCommands_C13_FrozenAliasesKeepTheirStart(t *testing.T) {
	m, _ := loadAppCmdManifest()
	var show *appCmdEntry
	for i := range m.Commands {
		if m.Commands[i].Name == "show" {
			show = &m.Commands[i]
		}
	}
	if show == nil {
		t.Fatal("no show command")
	}
	for _, spec := range []struct{ spelling, exe string }{
		{"ui", "filedo.exe"},
		{"gui", "filedo.exe"},
		{"-ui", "filedo.exe"},
		{"", "filedo.exe"},
		{"", "filedo_win.exe"},
		{"--startup", "filedo_win.exe"},
		{"--legacy-builder", "filedo_win.exe"},
	} {
		found := false
		for _, a := range show.Aliases {
			if a.Spelling == spec.spelling && a.Exe == spec.exe && a.Legacy && !a.AnswersCaller() {
				found = true
			}
		}
		if !found {
			t.Errorf("alias %+v of show is missing or lacks exe/legacy/answers:false in the manifest", spec)
		}
	}
	if e, _ := appCommandOf(nil); e != nil {
		t.Error("the empty argument list was taken by the front")
	}
	for _, word := range []string{"ui", "gui", "-ui", "dm", "-dm", "UI", "GUI"} {
		if e, _ := appCommandOf([]string{word}); e != nil {
			t.Errorf("%s was taken by the front; it is a frozen alias that keeps its start", word)
		}
	}
	for _, word := range []string{"gui"} {
		t.Setenv(statedir.EnvOverride, t.TempDir())
		var gotArgs []string
		saved := uiLauncher
		uiLauncher = func(args ...string) error { gotArgs = args; return nil }
		err := dispatchLine([]string{word}, NewHistoryLogger([]string{"filedo", word}), false)
		uiLauncher = saved
		if err != nil || gotArgs == nil || len(gotArgs) != 0 {
			t.Errorf("%s: launched with %q (err %v), want a plain start", word, gotArgs, err)
		}
	}
}

// The reserved first words: a folder named after a verb does not shadow it,
// and the target-first grammar of every other word is unchanged.
func TestAppCommands_ReservedWordsWinOverAPathProbe(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"status", "show", "quit", "version", "commands"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	for _, name := range []string{"status", "show", "quit", "version", "commands", "STATUS"} {
		if e, _ := appCommandOf([]string{name}); e == nil || !strings.EqualFold(e.Name, name) {
			t.Errorf("%s beside a folder of that name was not taken for the verb", name)
		}
	}
	for _, word := range []string{"C:", "info", "copy", "vd", "backup", "fdsec", "check"} {
		if e, _ := appCommandOf([]string{word}); e != nil {
			t.Errorf("%s was taken for an app command", word)
		}
	}
	// `vd status` keeps its own meaning: only the first word is read.
	if e, _ := appCommandOf([]string{"vd", "status"}); e != nil {
		t.Error("vd status was taken for an app command")
	}
	if err := dispatchLine([]string{"status"}, NewHistoryLogger([]string{"filedo", "status"}), true); err == nil {
		t.Error("a batch line `status` was read as a folder target")
	}
	globalExitCode = 0
}

// The manifest-driven option reader: names without regard to case, values as
// typed, bool switches with or without a value.
func TestAppCommands_OptionReader(t *testing.T) {
	e, _ := appCommandOf([]string{"status"})
	for _, ok := range [][]string{nil, {"--json"}, {"--JSON"}, {"--json=true"}, {"--json=false"}, {"--json", "--"}} {
		if _, problem := parseAppCmdArgs(e, ok); problem != "" {
			t.Errorf("%q refused: %s", ok, problem)
		}
	}
	for _, bad := range [][]string{{"--json=yes"}, {"--json", "--json"}, {"json"}, {"--", "x"}, {"-j"}, {"--color"}} {
		if _, problem := parseAppCmdArgs(e, bad); problem == "" {
			t.Errorf("%q accepted", bad)
		}
	}
	if !wantsJSON([]string{"--Json"}) || wantsJSON([]string{"--", "--json"}) {
		t.Error("wantsJSON reads the wrong switch")
	}
}

// The pipe name is the third shape of APP-ACTIVATION rule 3: user SID and
// session id, so another user's or session's copy is never reached (C9's
// automatic half; the elevated first instance is a manual step).
func TestAppCommands_PipeNameCarriesUserAndSession(t *testing.T) {
	name := appCmdPipeName()
	parts := strings.Split(name, "_")
	if len(parts) != 4 || parts[0] != "FileDO" || parts[1] != "Commands" || !strings.HasPrefix(parts[2], "S-1-") {
		t.Fatalf("pipe name %q is not FileDO_Commands_<sid>_<session>", name)
	}
	if !strings.HasPrefix(appCmdDefaultPipePath(), `\\.\pipe\FileDO_Commands_S-1-`) {
		t.Fatalf("pipe path %q", appCmdDefaultPipePath())
	}
}

// C13 for the developer tooling of the window program (SP-0219): each frozen
// spelling of the five release-tooling switches is an alias of its verb in the
// manifest, the verb belongs to filedo_win.exe, and the window program's one table
// (Program.vb) holds the same verb and spelling pair. The static half reads the
// table, so a change on one side alone fails here.
func TestAppCommands_C13_ReleaseToolingSpellingsAreListed(t *testing.T) {
	m, err := loadAppCmdManifest()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(filepath.Join("..", "..", "filedo_win_vb", "Program.vb"))
	if err != nil {
		t.Fatalf("read the window program's table: %v", err)
	}
	pairs := []struct {
		verb, legacy, risk string
		takesOut           bool
	}{
		{"selftest run", "--selftest", "safe", false},
		{"help-pages build", "--build-help", "writes-user-data", true},
		{"screens capture", "--capture-screens", "writes-user-data", true},
		{"menu-icons write", "--write-menu-icons", "writes-user-data", true},
		{"ui-drive run", "--ui-drive", "writes-user-data", true},
	}
	for _, p := range pairs {
		var e *appCmdEntry
		for i := range m.Commands {
			if m.Commands[i].Name == p.verb {
				e = &m.Commands[i]
			}
		}
		if e == nil {
			t.Errorf("%s is not in the manifest", p.verb)
			continue
		}
		if e.Exe != "filedo_win.exe" || !e.isDeveloper() || !e.isForwardNever() {
			t.Errorf("%s: exe %q audience %q forward %q, want filedo_win.exe / developer / never", p.verb, e.Exe, e.Audience, e.Forward)
		}
		if e.ColdStart != "oneshot" || e.Risk != p.risk {
			t.Errorf("%s: coldStart %q and risk %q, want oneshot and %s", p.verb, e.ColdStart, e.Risk, p.risk)
		}
		aliased := false
		for _, a := range e.Aliases {
			wantSpelling := p.legacy
			if p.takesOut {
				wantSpelling = p.legacy + " {out}"
			}
			if a.Spelling == wantSpelling && a.Exe == "filedo_win.exe" && a.Legacy && a.AnswersCaller() {
				if !p.takesOut || a.Maps["out"] == "out" {
					aliased = true
				}
			}
		}
		if !aliased {
			t.Errorf("%s: its frozen spelling %s is not listed as a legacy alias object", p.verb, p.legacy)
		}
		row := `"` + p.verb + `", "` + p.legacy + `"`
		if !bytes.Contains(program, []byte(row)) {
			t.Errorf("Program.vb holds no table row with %s", row)
		}
	}
}

// The developer tooling of the window program is not a verb of filedo.exe (SP-0219):
// no front takes it, a cold start never runs for it, and its first words are not
// reserved, so the target-first grammar of the console program is unchanged - a
// folder named screens is still a target.
func TestAppCommands_ReleaseToolingIsNotFilediosVerbs(t *testing.T) {
	for _, line := range [][]string{
		{"selftest", "run"},
		{"help-pages", "build", "--out", "x"},
		{"screens", "capture", "--out", "x"},
		{"menu-icons", "write", "--out", "x"},
		{"ui-drive", "run", "--out", "x"},
		{"selftest"},
		{"screens"},
	} {
		if e, _ := appCommandOf(line); e != nil {
			t.Errorf("%v was taken by filedo.exe as %q", line, e.Name)
		}
	}
	for _, word := range []string{"selftest", "help-pages", "screens", "menu-icons", "ui-drive"} {
		if isAppCommandWord(word) {
			t.Errorf("%s is a reserved first word of filedo.exe", word)
		}
	}
}

// C13 for the container-file start of filedo_win.exe (SP-0228): `open` carries
// forward: never, defaultFor: filedo_win.exe, coldStart: oneshot, and the four
// legacy aliases (bare path, --mount, --mount-ro, --unmount) with answers: false
// and legacy: true; Program.vb wires OpenCallOf and filedo.exe does not reserve `open`.
func TestAppCommands_C13_ContainerOpenSpellingsAreListed(t *testing.T) {
	m, err := loadAppCmdManifest()
	if err != nil {
		t.Fatal(err)
	}
	var e *appCmdEntry
	for i := range m.Commands {
		if m.Commands[i].Name == "open" {
			e = &m.Commands[i]
			break
		}
	}
	if e == nil {
		t.Fatal("open is not in the manifest")
	}
	if e.Exe != "filedo_win.exe" || e.DefaultFor != "filedo_win.exe" || e.isDeveloper() || !e.isForwardNever() || e.ColdStart != "oneshot" || e.Risk != "safe" {
		t.Fatalf("open metadata mismatch: %+v", *e)
	}
	wantAliases := []struct {
		spelling string
		implies  []string
	}{
		{"{path}", nil},
		{"--mount {path}", []string{"--mode=mount"}},
		{"--mount-ro {path}", []string{"--mode=mount-ro"}},
		{"--unmount {path}", []string{"--mode=unmount"}},
	}
	for _, want := range wantAliases {
		found := false
		for _, a := range e.Aliases {
			if a.Spelling == want.spelling && a.Exe == "filedo_win.exe" && a.Legacy && !a.AnswersCaller() && a.Maps["path"] == "path" && reflect.DeepEqual(a.Implies, want.implies) {
				found = true
			}
		}
		if !found {
			t.Errorf("open: alias %q with implies=%v, legacy:true, answers:false, exe:filedo_win.exe missing", want.spelling, want.implies)
		}
	}
	for _, line := range [][]string{{"open"}, {"open", "--path", "x.fdd"}} {
		if got, _ := appCommandOf(line); got != nil {
			t.Errorf("%v was taken by filedo.exe as %q", line, got.Name)
		}
	}
	if isAppCommandWord("open") {
		t.Error("open is a reserved first word of filedo.exe")
	}
	program, err := os.ReadFile(filepath.Join("..", "..", "filedo_win_vb", "Program.vb"))
	if err != nil {
		t.Fatalf("read Program.vb: %v", err)
	}
	if !bytes.Contains(program, []byte("OpenCallOf")) {
		t.Error("Program.vb does not define OpenCallOf")
	}
	diskJobs, err := os.ReadFile(filepath.Join("..", "..", "filedo_win_vb", "DiskJobs.vb"))
	if err != nil {
		t.Fatalf("read DiskJobs.vb: %v", err)
	}
	for _, sw := range []string{`"--mount"`, `"--mount-ro"`, `"--unmount"`} {
		if !bytes.Contains(diskJobs, []byte(sw)) {
			t.Errorf("DiskJobs.vb does not list %s", sw)
		}
	}
}

// C15 (item K): connection closed with no response line, non-JSON line, or JSON
// missing schemaVersion exits 2 with the older-copy sentence (and sends the
// legacy launch once for show/disk-manager show, never after silence).
func TestAppCommands_C15_OlderFirstInstance(t *testing.T) {
	serveRaw := func(t *testing.T, pipe string, reply []byte) {
		t.Helper()
		l, err := winio.ListenPipe(pipe, nil)
		if err != nil {
			t.Fatalf("listen %s: %v", pipe, err)
		}
		t.Cleanup(func() { l.Close() })
		go func() {
			for {
				c, err := l.Accept()
				if err != nil {
					return
				}
				go func(c net.Conn) {
					_, _ = bufio.NewReader(c).ReadBytes('\n')
					if len(reply) > 0 {
						_, _ = c.Write(reply)
					}
					c.Close()
				}(c)
			}
		}()
	}

	for _, tc := range []struct {
		name  string
		reply []byte
	}{
		{"closed-without-line", nil},
		{"plain-ok", []byte("ok\n")},
		{"json-no-schema", []byte(`{"status":"done","exitCode":0}` + "\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pipe := testPipePath(t)
			h := newAppCmdHarness(t, pipe)
			serveRaw(t, pipe, tc.reply)
			if code := runAppCmd(t, "status"); code != 2 {
				t.Fatalf("status: exit %d, want 2", code)
			}
			if !strings.Contains(h.err.String(), "older copy") || h.launched != 0 {
				t.Fatalf("err %q launched %d", h.err.String(), h.launched)
			}
			h.err.Reset()
			if code := runAppCmd(t, "show"); code != 2 || h.launched != 1 {
				t.Fatalf("show: exit %d launched %d, want 2 and 1 legacy launch", code, h.launched)
			}
		})
	}
}

// C16 (item C): a forward: never verb never connects to the pipe or launches the
// window program even when a pipe listener is running.
func TestAppCommands_C16_ForwardNeverDoesNotTouchPipe(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	f := startFakeFirstInstance(t, pipe, doneAnswer(nil))
	for _, line := range [][]string{{"commands"}, {"commands", "--developer"}, {"commands", "--json"}, {"version"}, {"version", "--json"}} {
		if code := runAppCmd(t, line...); code != 0 {
			t.Fatalf("%v: exit %d, want 0", line, code)
		}
	}
	f.mu.Lock()
	got := len(f.requests)
	f.mu.Unlock()
	if got != 0 || h.launched != 0 {
		t.Fatalf("forward: never touched pipe (%d requests) or launched (%d)", got, h.launched)
	}
}

// C19 (item I.3): an answer with an empty requestId exits 2 (not proven).
func TestAppCommands_C19_UnreadableReplyExits2(t *testing.T) {
	pipe := testPipePath(t)
	newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(req appCmdRequest) *appCmdResponse {
		return &appCmdResponse{SchemaVersion: 1, RequestID: "", Status: "refused", ExitCode: 10, Reason: "bad-argument", Message: "bad"}
	})
	if code := runAppCmd(t, "status"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}
