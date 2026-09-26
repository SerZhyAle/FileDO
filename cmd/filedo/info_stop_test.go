package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
