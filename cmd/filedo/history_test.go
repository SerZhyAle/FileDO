package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filedo/statedir"
)

// TestHistoryCorruptFileIsPreserved is CLI-16: a history.json that does not
// parse is set aside, never overwritten by the next run's single entry.
func TestHistoryCorruptFileIsPreserved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(statedir.EnvOverride, dir)
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	hist := filepath.Join(dir, "history.json")
	corrupt := []byte(`[{"timestamp":"2026-09-25T10:00:00Z","command":"device"`)
	if err := os.WriteFile(hist, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := saveToHistory(HistoryEntry{Timestamp: time.Now(), Command: "folder"}); err != nil {
		t.Fatalf("saveToHistory: %v", err)
	}

	matches, _ := filepath.Glob(filepath.Join(dir, "history.json.corrupt-*"))
	if len(matches) != 1 {
		t.Fatalf("want exactly one history.json.corrupt-* file, got %v", matches)
	}
	kept, _ := os.ReadFile(matches[0])
	if string(kept) != string(corrupt) {
		t.Fatalf("the corrupt file was altered:\n%s", kept)
	}
	var entries []HistoryEntry
	if err := json.Unmarshal(mustReadFile(t, hist), &entries); err != nil || len(entries) != 1 {
		t.Fatalf("the new history.json must hold the one new entry: n=%d err=%v", len(entries), err)
	}
}

// TestHistoryConcurrentWriters: two writers never lose each other's entries.
func TestHistoryConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(statedir.EnvOverride, dir)
	// An empty cwd, so no legacy history.json is imported into the count.
	wd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := saveToHistory(HistoryEntry{Timestamp: time.Now(), Command: "c", Target: strings.Repeat("x", i)}); err != nil {
				t.Errorf("saveToHistory: %v", err)
			}
		}(i)
	}
	wg.Wait()
	var entries []HistoryEntry
	if err := json.Unmarshal(mustReadFile(t, filepath.Join(dir, "history.json")), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != n {
		t.Fatalf("history holds %d entries after %d concurrent writes", len(entries), n)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "history.json.*")); len(left) != 0 {
		t.Fatalf("temporary or lock files left behind: %v", left)
	}
}

// TestHistoryImportsLegacyFileOnce: the first run after the move copies the
// old cwd history into the state root and leaves the old file alone.
func TestHistoryImportsLegacyFileOnce(t *testing.T) {
	stateDir := t.TempDir()
	cwd := t.TempDir()
	t.Setenv(statedir.EnvOverride, stateDir)
	legacy := `[{"timestamp":"2026-01-01T00:00:00Z","command":"old","target":"","operation":"","fullCommand":"","parameters":null,"results":null,"duration":"","success":true}]`
	if err := os.WriteFile(filepath.Join(cwd, "history.json"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)

	if err := saveToHistory(HistoryEntry{Timestamp: time.Now(), Command: "new"}); err != nil {
		t.Fatal(err)
	}
	var entries []HistoryEntry
	if err := json.Unmarshal(mustReadFile(t, filepath.Join(stateDir, "history.json")), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Command != "old" || entries[1].Command != "new" {
		t.Fatalf("want the imported entry then the new one, got %+v", entries)
	}
	if got := string(mustReadFile(t, filepath.Join(cwd, "history.json"))); got != legacy {
		t.Fatal("the legacy history.json must be left untouched")
	}
}

// TestHelpWritesNoHistory is CLI-31: help, the history listing and a bare
// start leave no history.json anywhere.
func TestHelpWritesNoHistory(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"?"}, {"hist"}} {
		wd := t.TempDir()
		out, _ := run(t, wd, args...)
		if exists(filepath.Join(wd, "history.json")) {
			t.Errorf("filedo %v wrote history.json:\n%s", args, out)
		}
	}
}

func mustReadFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
