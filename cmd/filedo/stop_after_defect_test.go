package main

import (
	"path/filepath"
	"testing"
)

// AUD-66-F1 / SP-0064 T1-F5: a defect the run recorded before a stop outranks
// the stop (CLI-EVENT-STREAM 0.11 rule 15). These tests drive the recording
// API the verbs use - runDefect, then the stop channel's Interrupt, then the
// finishing path - and read the `result` event back from the stream.

// stopRunFixture gives a test a fresh run, a fresh exit code, an event file
// and an interrupt handler that is not wired to process signals, and restores
// all four afterwards.
func stopRunFixture(t *testing.T) string {
	t.Helper()
	events := filepath.Join(t.TempDir(), "events.jsonl")
	savedRun, savedExit, savedEvents, savedInterrupt := currentRun, globalExitCode, globalEventManager, globalInterruptHandler
	t.Cleanup(func() {
		currentRun, globalExitCode, globalEventManager, globalInterruptHandler = savedRun, savedExit, savedEvents, savedInterrupt
	})
	currentRun = &runOutcome{numbers: make(map[string]interface{})}
	globalExitCode = 0
	globalInterruptHandler = newInterruptHandlerNoSignals()
	em, err := InitEventManager(events)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { em.Close() })
	return events
}

func TestStopAfterDefectStaysFailed(t *testing.T) {
	events := stopRunFixture(t)
	beginRun(runJudges, "check", `D:\Data`, nil)
	runDefect("damaged-files", "2 damaged files", map[string]interface{}{"count": 2})
	globalInterruptHandler.Interrupt()
	finishRun()
	if got := lastResult(t, events)["verdict"]; got != string(VerdictFailed) {
		t.Fatalf("stop after a recorded defect: verdict %q, want Failed", got)
	}
	if globalExitCode != 1 {
		t.Fatalf("stop after a recorded defect: exit %d, want 1", globalExitCode)
	}
}

func TestStopWithoutDefectStaysStopped(t *testing.T) {
	events := stopRunFixture(t)
	beginRun(runJudges, "check", `D:\Data`, nil)
	globalInterruptHandler.Interrupt()
	finishRun()
	if got := lastResult(t, events)["verdict"]; got != string(VerdictStopped) {
		t.Fatalf("stop with no defect: verdict %q, want Stopped", got)
	}
	if globalExitCode != 0 {
		t.Fatalf("stop with no defect: exit %d, want 0", globalExitCode)
	}
}

func TestForcedExitAfterDefectStaysFailed(t *testing.T) {
	events := stopRunFixture(t)
	beginRun(runJudges, "check", `D:\Data`, nil)
	runDefect("damaged-files", "2 damaged files", map[string]interface{}{"count": 2})
	globalInterruptHandler.Interrupt()
	if got := finishForcedRun(); got != 1 {
		t.Fatalf("forced exit after a recorded defect: code %d, want 1", got)
	}
	if got := lastResult(t, events)["verdict"]; got != string(VerdictFailed) {
		t.Fatalf("forced exit after a recorded defect: verdict %q, want Failed", got)
	}
	if globalExitCode != 1 {
		t.Fatalf("forced exit after a recorded defect: globalExitCode %d, want 1", globalExitCode)
	}
}
