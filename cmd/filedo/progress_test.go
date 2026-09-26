package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// captureProgress runs fn with the event stream pointed at a scratch file and
// the console line sent to the null device, and returns the progress events
// fn emitted.
func captureProgress(t *testing.T, fn func()) []map[string]interface{} {
	t.Helper()
	events := filepath.Join(t.TempDir(), "ev.jsonl")
	em, err := InitEventManager(events)
	if err != nil {
		t.Fatal(err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = null
	func() {
		defer func() {
			os.Stdout = stdout
			null.Close()
			em.Close()
			globalEventManager = nil
		}()
		fn()
	}()
	var progress []map[string]interface{}
	for _, ev := range readEvents(t, events) {
		if ev["kind"] == "progress" {
			progress = append(progress, ev)
		}
	}
	return progress
}

// TestProgressThrottleHoldsPastTheItemEstimate is AUD-29-F4: a tracker whose
// item count runs past its total (an estimate) still prints at most once per
// interval, plus one line for reaching the total.
func TestProgressThrottleHoldsPastTheItemEstimate(t *testing.T) {
	got := captureProgress(t, func() {
		pt := NewProgressTrackerWithInterval(20, 20*10240, time.Hour)
		for i := int64(1); i <= 2000; i++ {
			pt.Update(i, i*10240)
			pt.PrintProgress("Securing")
		}
	})
	if len(got) > 1 {
		t.Fatalf("%d progress events within one interval, want at most 1 (the completion line)", len(got))
	}
}

// TestFdsecProgressCountStaysWithinItsTotal is AUD-29-F4 on the container
// tracker: a folder of many small entries calls back once per entry, far more
// often than the chunk estimate, and neither floods the stream nor reports
// more items done than there are.
func TestFdsecProgressCountStaysWithinItsTotal(t *testing.T) {
	const entries, size = 2000, 10 * 1024
	got := captureProgress(t, func() {
		cb := fdsecProgressFunc("Securing", entries*size)
		for i := int64(1); i <= entries; i++ {
			cb(i*size, entries*size)
		}
	})
	if len(got) > 2 {
		t.Fatalf("%d progress events for a run far shorter than the 500 ms interval, want at most 2", len(got))
	}
	for _, ev := range got {
		data, _ := ev["data"].(map[string]interface{})
		done, _ := data["doneItems"].(float64)
		total, _ := data["totalItems"].(float64)
		if done > total {
			t.Fatalf("progress event reports %v of %v items", done, total)
		}
	}
}
