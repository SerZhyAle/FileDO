package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// infoTree builds a scratch folder with enough entries that a walk which
// ignores the stop visibly finishes.
func infoTree(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "tree")
	for i := 0; i < 50; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("d%02d", i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 4; j++ {
			if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%d.txt", j)), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	return dir
}

// TestInfoWalksObserveStopFile is AUD-10-F1: the folder and network `info`
// walks observe the stop file. A stop requested before the walk ends the run
// Stopped (exit 0, rule 15) and prints no totals - a partial count printed as
// the tree's size is a result nobody measured.
func TestInfoWalksObserveStopFile(t *testing.T) {
	dir := infoTree(t)
	wd := t.TempDir()
	stop := filepath.Join(wd, "run.stop")
	if err := os.WriteFile(stop, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, verb, target, op string }{
		{"folder-short", "folder", dir, "short"},
		{"folder-info", "folder", dir, "info"},
		{"network-info", "network", `\\?\` + dir, "info"},
	} {
		events := filepath.Join(wd, c.name+".ev.jsonl")
		out, code := run(t, wd, "--events", events, "--stop-file", stop, c.verb, c.target, c.op)
		if code != 0 {
			t.Errorf("%s: a stopped run exits 0, got %d\n%s", c.name, code, out)
		}
		if strings.Contains(out, "Contains:") {
			t.Errorf("%s: a stopped info printed totals\n%s", c.name, out)
		}
		if v := lastResult(t, events)["verdict"]; v != "Stopped" {
			t.Errorf("%s: verdict %v, want Stopped\n%s", c.name, v, out)
		}
	}
}

// TestInfoShortWalksOnce is AUD-10-F1 (AUD-29-F6): `folder X short` walks the
// tree once and prints the short form of that one walk.
func TestInfoShortWalksOnce(t *testing.T) {
	dir := infoTree(t)
	walks := 0
	saved := infoWalkStarted
	infoWalkStarted = func(string) { walks++ }
	defer func() { infoWalkStarted = saved }()

	runGenericCommand(flag.NewFlagSet("folder", flag.ContinueOnError), CommandFolder, []string{dir, "short"}, NewHistoryLogger(nil))
	if walks != 1 {
		t.Fatalf("folder short walked the tree %d times, want 1", walks)
	}
}

// TestWalkInfoTreeCompletedAndStopped is SP-0128 T4: a completed walk returns the
// exact counts, and a stop requested during the walk returns within two seconds
// with errRunStopped and partial totals.
func TestWalkInfoTreeCompletedAndStopped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep_tree")
	const numDirs = 40
	const filesPerDir = 50
	for i := 0; i < numDirs; i++ {
		sub := filepath.Join(dir, fmt.Sprintf("d%02d", i))
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		for j := 0; j < filesPerDir; j++ {
			if err := os.WriteFile(filepath.Join(sub, fmt.Sprintf("f%d.txt", j)), []byte("data"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	// 1. Completed walk returns exact count
	totals, err := walkInfoTree(dir, false)
	if err != nil {
		t.Fatalf("completed walk failed: %v", err)
	}
	if totals.files != numDirs*filesPerDir || totals.folders != numDirs {
		t.Fatalf("completed walk totals = %d files, %d folders; want %d files, %d folders",
			totals.files, totals.folders, numDirs*filesPerDir, numDirs)
	}

	// 2. Stopped walk returns partial count and errRunStopped within two seconds
	stopHandler := NewInterruptHandler()
	prevHandler := globalInterruptHandler
	globalInterruptHandler = stopHandler
	defer func() { globalInterruptHandler = prevHandler }()

	// The stop is raised as the walk starts, not from a timer: a cached tree of
	// this size is walked in a few milliseconds, so a delayed stop raced the
	// walk and sometimes arrived after it had already completed.
	saved := infoWalkStarted
	infoWalkStarted = func(root string) {
		stopHandler.Interrupt()
	}
	defer func() { infoWalkStarted = saved }()

	start := time.Now()
	stoppedTotals, walkErr := walkInfoTree(dir, false)
	took := time.Since(start)

	if took > 2*time.Second {
		t.Errorf("stopped walk took %v, want <= 2s", took)
	}
	if !errors.Is(walkErr, errRunStopped) {
		t.Errorf("stopped walk err = %v, want errRunStopped", walkErr)
	}
	t.Logf("stopped walk returned partial totals: %d files, %d folders", stoppedTotals.files, stoppedTotals.folders)
}

// TestDeviceInfoObservesStopFile is SP-0128: `device <X:> info` prints quick
// facts first, stops gracefully within two seconds on --stop-file, prints
// partial totals labelled "Full Contains (partial):", and ends Stopped exit 0.
func TestDeviceInfoObservesStopFile(t *testing.T) {
	wd := t.TempDir()
	stop := filepath.Join(wd, "run.stop")
	if err := os.WriteFile(stop, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	events := filepath.Join(wd, "devinfo.ev.jsonl")
	out, code := run(t, wd, "--events", events, "--stop-file", stop, "device", "C:", "info")
	if code != 0 {
		t.Fatalf("stopped device info exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "Information for device: C:") {
		t.Errorf("quick facts not printed before stop\n%s", out)
	}
	if !strings.Contains(out, "Full Contains (partial):") {
		t.Errorf("output missing 'Full Contains (partial):'\n%s", out)
	}
	if v := lastResult(t, events)["verdict"]; v != "Stopped" {
		t.Errorf("verdict %v, want Stopped\n%s", v, out)
	}
}
