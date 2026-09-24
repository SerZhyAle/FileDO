package main

// Black-box tests for the two halves of the supervisor contract: the exit-code
// vocabulary of CLI-EVENT-STREAM rule 11, and the `run`-first/`result`-last
// shape of the channel itself (rules 3, 9 and 10).
//
// They drive the freshly built exe that TestMain in fdsec_cli_test.go produces,
// because an exit code is only observable from outside the process - which is
// the same reason the fdsec acceptance suite is black-box.
//
// Run them with:  go test ./cmd/filedo/ -count=1 -vet=off

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fillFile writes one FILL test file whose embedded header names `embeds`.
// A file that names itself is what an honest drive reads back; a file that
// names a later one is what a fake controller leaves behind when it wraps its
// address space, and it is the one defect this repository can stage on a real
// disk without a fake disk (device_windows.go, runDeviceFillVerify).
func fillFile(t *testing.T, dir, name, embeds string) {
	t.Helper()
	body := make([]byte, 4096)
	header := fmt.Sprintf("FILEDO_TEST_%s_20260324_012341\n", embeds)
	copy(body, header)
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func genuineFillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range []string{"FILL_00001_021504.tmp", "FILL_00002_021505.tmp"} {
		fillFile(t, dir, n, n)
	}
	return dir
}

func fakeFillDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// The first file reads back as the second one: the wrap a fake controller
	// leaves behind.
	fillFile(t, dir, "FILL_00001_021504.tmp", "FILL_00002_021505.tmp")
	fillFile(t, dir, "FILL_00002_021505.tmp", "FILL_00002_021505.tmp")
	return dir
}

// TestExitCodeVocabulary is the table T2 asks for: one verb of each kind, with
// the code it must exit and the verdict its `result` event must carry. Before
// the outcome layer existed every one of these rows exited 0, so a failed check
// and a passed one were the same thing to any caller.
func TestExitCodeVocabulary(t *testing.T) {
	genuine := genuineFillDir(t)
	fake := fakeFillDir(t)
	plain := t.TempDir()
	missing := filepath.Join(t.TempDir(), "no-such-folder")

	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantVerd string
	}{
		{
			name:     "a judging verb that found nothing wrong",
			args:     []string{genuine, "fill", "verify"},
			wantCode: 0,
			wantVerd: "Passed",
		},
		{
			name:     "a judging verb that found a defect",
			args:     []string{fake, "fill", "verify"},
			wantCode: 1,
			wantVerd: "Failed",
		},
		{
			name:     "a verb that could not judge - the target is not there",
			args:     []string{"folder", missing, "info"},
			wantCode: 2,
			wantVerd: "Not proven",
		},
		{
			name:     "a bare missing folder still closes the stream",
			args:     []string{missing + "\\"},
			wantCode: 2,
			wantVerd: "Not proven",
		},
		{
			name:     "an unreadable batch list still closes the stream",
			args:     []string{"from", filepath.Join(t.TempDir(), "missing.lst")},
			wantCode: 2,
			wantVerd: "Not proven",
		},
		{
			name:     "an acting verb that did what it was asked",
			args:     []string{plain, "info"},
			wantCode: 0,
			wantVerd: "Done",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wd := t.TempDir()
			events := filepath.Join(wd, "events.jsonl")
			args := append([]string{"--events", events}, c.args...)
			out, code := run(t, wd, args...)
			if code != c.wantCode {
				t.Errorf("exit code %d, want %d\n%s", code, c.wantCode, out)
			}
			result := lastResult(t, events)
			if got := result["verdict"]; got != c.wantVerd {
				t.Errorf("verdict %q, want %q\n%s", got, c.wantVerd, out)
			}
		})
	}
}

// TestContainerVerbKeepsItsOwnClasses is rule 12: a container verb's digits are
// FDSEC-BEHAVIOUR section 7.1 and mean something else, so they are left alone -
// and the `result` event is what carries the verdict across that seam.
func TestContainerVerbKeepsItsOwnClasses(t *testing.T) {
	wd, _ := workdir(t)
	events := filepath.Join(wd, "events.jsonl")
	src := filepath.Join(wd, "plain.txt")

	if out, code := run(t, wd, src, "secure", "p:correct-horse-battery", "to", "box.fd-sec"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	container := filepath.Join(wd, "box.fd-sec")
	if !exists(container) {
		t.Fatalf("no container at %s", container)
	}

	out, code := run(t, wd, "--events", events, "fdsec", "verify", container, "wrong-password")
	if code != fdsecExitCredentialOrTamper {
		t.Errorf("exit code %d, want the FDSEC-BEHAVIOUR class %d\n%s", code, fdsecExitCredentialOrTamper, out)
	}
	if got := lastResult(t, events)["verdict"]; got != "Failed" {
		t.Errorf("verdict %q, want %q - the digit is FDSEC-BEHAVIOUR's, the verdict is this contract's\n%s", got, "Failed", out)
	}
}

// TestEventStreamShape is the producer half of T1: a supervised run of each of
// the four target verbs writes a stream whose first line is `run` and whose
// last line is `result`, every line a complete JSON object carrying the schema
// version (rules 3, 6, 9 and 10).
func TestEventStreamShape(t *testing.T) {
	plain := t.TempDir()
	genuine := genuineFillDir(t)
	wdFdsec, _ := workdir(t)

	if out, code := run(t, wdFdsec, filepath.Join(wdFdsec, "plain.txt"), "secure", "p:correct-horse-battery", "to", "box.fd-sec"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}

	cases := []struct {
		name string
		wd   string
		args []string
	}{
		{"folder info", plain, []string{plain, "info"}},
		{"fill verify", genuine, []string{genuine, "fill", "verify"}},
		{"file info", wdFdsec, []string{"file", filepath.Join(wdFdsec, "box.fd-sec"), "info"}},
		{"container verify", wdFdsec, []string{"fdsec", "verify",
			filepath.Join(wdFdsec, "box.fd-sec"), "correct-horse-battery"}},
		{"fdsec missing sub-verb", wdFdsec, []string{"fdsec"}},
		{"missing container unsecure", wdFdsec, []string{filepath.Join(wdFdsec, "missing.fd-sec"), "unsecure", "p:any"}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wd := t.TempDir()
			events := filepath.Join(wd, "events.jsonl")
			args := append([]string{"--events", events}, c.args...)
			out, _ := run(t, wd, args...)

			evs := readEvents(t, events)
			if len(evs) < 2 {
				t.Fatalf("expected at least a run and a result, got %d events\n%s", len(evs), out)
			}
			if evs[0]["kind"] != "run" {
				t.Errorf("first event is %v, want run", evs[0]["kind"])
			}
			if evs[len(evs)-1]["kind"] != "result" {
				t.Errorf("last event is %v, want result", evs[len(evs)-1]["kind"])
			}
			for i, ev := range evs {
				if ev["schemaVersion"] != float64(SchemaVersion) {
					t.Errorf("event %d carries schemaVersion %v, want %d", i, ev["schemaVersion"], SchemaVersion)
				}
				ts, ok := ev["timestamp"].(string)
				if !ok || ts == "" {
					t.Errorf("event %d carries no timestamp", i)
					continue
				}
				if _, err := time.Parse(time.RFC3339Nano, ts); err != nil || (!strings.HasSuffix(ts, "Z") && !strings.Contains(ts[10:], "+") && !strings.Contains(ts[10:], "-")) {
					t.Errorf("event %d timestamp %q is not RFC 3339 with an offset: %v", i, ts, err)
				}
			}
		})
	}
}

func TestRecoveredPanicEndsNotProven(t *testing.T) {
	dir := t.TempDir()
	events := filepath.Join(dir, "events.jsonl")
	savedRun, savedExit, savedEvents, savedInterrupt := currentRun, globalExitCode, globalEventManager, globalInterruptHandler
	defer func() {
		currentRun, globalExitCode, globalEventManager, globalInterruptHandler = savedRun, savedExit, savedEvents, savedInterrupt
	}()
	currentRun = &runOutcome{numbers: make(map[string]interface{})}
	globalExitCode = 0
	globalInterruptHandler = nil
	em, err := InitEventManager(events)
	if err != nil {
		t.Fatal(err)
	}
	defer em.Close()

	func() {
		defer finishRun()
		defer recoverRunPanic()
		beginRun(runActs, "panic-test", "", nil)
		panic("test panic")
	}()
	if globalExitCode != 2 {
		t.Fatalf("panic set exit %d, want 2", globalExitCode)
	}
	if got := lastResult(t, events)["verdict"]; got != "Not proven" {
		t.Fatalf("panic result verdict %q, want Not proven", got)
	}
}

func TestForcedExitEndsNotProven(t *testing.T) {
	dir := t.TempDir()
	events := filepath.Join(dir, "events.jsonl")
	savedRun, savedExit, savedEvents := currentRun, globalExitCode, globalEventManager
	defer func() { currentRun, globalExitCode, globalEventManager = savedRun, savedExit, savedEvents }()
	currentRun = &runOutcome{numbers: make(map[string]interface{})}
	globalExitCode = 0
	em, err := InitEventManager(events)
	if err != nil {
		t.Fatal(err)
	}
	defer em.Close()
	beginRun(runActs, "force-test", "", nil)
	if got := finishForcedRun(); got != 2 {
		t.Fatalf("forced exit code %d, want 2", got)
	}
	if got := lastResult(t, events)["verdict"]; got != "Not proven" {
		t.Fatalf("forced exit verdict %q, want Not proven", got)
	}
}

func TestFdsecSealedNameIsNotInEvents(t *testing.T) {
	dir, payload := workdir(t)
	const sealedName = "sealed-event-token.txt"
	if err := os.WriteFile(filepath.Join(dir, sealedName), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, sealedName, "secure", "p:correct-horse-battery", "to", "box.fd-sec"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	if err := os.WriteFile(filepath.Join(dir, sealedName), []byte("collision"), 0o644); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(dir, "events.jsonl")
	out, code := run(t, dir, "--events", events, "box.fd-sec", "unsecure", "p:correct-horse-battery")
	if code != fdsecExitUsage {
		t.Fatalf("unsecure collision exited %d, want %d\n%s", code, fdsecExitUsage, out)
	}
	raw, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), sealedName) {
		t.Fatalf("sealed name reached the events file:\n%s", raw)
	}

	revealEvents := filepath.Join(dir, "reveal-events.jsonl")
	if out, code := run(t, dir, "--events", revealEvents, "box.fd-sec", "reveal", "p:correct-horse-battery"); code != 0 {
		t.Fatalf("reveal exited %d\n%s", code, out)
	}
	revealRaw, err := os.ReadFile(revealEvents)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(revealRaw), sealedName) {
		t.Fatalf("sealed name reached reveal events:\n%s", revealRaw)
	}
}

// TestRunEventRedactsCredentials is rule 13 on this channel specifically: the
// events file outlives the run and a supervisor may log it, so it is a log.
func TestRunEventRedactsCredentials(t *testing.T) {
	wd, _ := workdir(t)
	events := filepath.Join(wd, "events.jsonl")
	const secret = "events-pa55phrase-not-a-real-one"

	out, code := run(t, wd, "--events", events, filepath.Join(wd, "plain.txt"), "secure", "p:"+secret)
	if code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	raw, err := os.ReadFile(events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatalf("the credential reached the events file:\n%s", raw)
	}
}

// readEvents parses the channel: one JSON object per line, and a line that does
// not parse is a defect rather than something to skip past (rule 3).
func readEvents(t *testing.T, path string) []map[string]interface{} {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the events file: %v", err)
	}
	var out []map[string]interface{}
	for i, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d is not a JSON object: %v\n%s", i+1, err, line)
		}
		out = append(out, ev)
	}
	return out
}

// lastResult returns the data of the last `result` event, which is the only
// place a verdict may be read from (rule 10).
func lastResult(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	evs := readEvents(t, path)
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i]["kind"] != "result" {
			continue
		}
		data, ok := evs[i]["data"].(map[string]interface{})
		if !ok {
			t.Fatalf("the result event carries no data: %v", evs[i])
		}
		return data
	}
	t.Fatalf("no result event in %s - the run said nothing", path)
	return nil
}
