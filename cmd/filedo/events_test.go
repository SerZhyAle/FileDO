package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEventManager_Emission(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filedo_events_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	eventsFile := filepath.Join(tempDir, "events.jsonl")

	em, err := InitEventManager(eventsFile)
	if err != nil {
		t.Fatalf("failed to init event manager: %v", err)
	}

	EmitRunEvent("test-1.0", "speed", "D:", []string{"D:", "speed", "100"})
	EmitStepEvent("write", "Writing test payload")
	EmitProgressEvent(5, 10, 500, 1000, 50.5, "Halfway done")
	EmitFindingEvent("defect", "Mismatch at sector 42", map[string]interface{}{"sector": 42})
	EmitNoteEvent("Redirected C: to TEMP")
	EmitResultEvent("Passed", map[string]interface{}{"speed": "120 MB/s"}, []string{}, []string{"report.log"})

	em.Close()

	// Read and verify JSON lines
	f, err := os.Open(eventsFile)
	if err != nil {
		t.Fatalf("failed to open events file: %v", err)
	}
	defer f.Close()

	var events []Event
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("failed to unmarshal JSON line '%s': %v", line, err)
		}
		events = append(events, ev)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	if len(events) != 6 {
		t.Fatalf("expected 6 events, got %d", len(events))
	}

	expectedKinds := []EventKind{
		EventKindRun,
		EventKindStep,
		EventKindProgress,
		EventKindFinding,
		EventKindNote,
		EventKindResult,
	}

	for i, expectedKind := range expectedKinds {
		if events[i].Kind != expectedKind {
			t.Errorf("event %d: expected kind %s, got %s", i, expectedKind, events[i].Kind)
		}
		if events[i].SchemaVersion != SchemaVersion {
			t.Errorf("event %d: expected schemaVersion %d, got %d", i, SchemaVersion, events[i].SchemaVersion)
		}
	}
}

func TestInterruptHandler_StopFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "filedo_stop_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	stopFile := filepath.Join(tempDir, "test.stop")

	ih := NewInterruptHandler()
	ih.WatchStopFile(stopFile)

	cleaned := false
	ih.AddCleanup(func() {
		cleaned = true
	})

	if ih.IsCancelled() {
		t.Fatal("handler should not be cancelled before stop file is created")
	}

	// Create stop file
	if err := os.WriteFile(stopFile, []byte("stop"), 0644); err != nil {
		t.Fatalf("failed to write stop file: %v", err)
	}

	// Wait for polling detection
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ih.IsCancelled() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !ih.IsCancelled() {
		t.Fatal("handler failed to cancel within deadline after stop file was created")
	}

	if !cleaned {
		t.Fatal("cleanup function was not invoked on stop")
	}
}
