//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filedo/vdisk"
)

// A listing of the scheduled tasks that fails says nothing about whether the task exists. auto off must
// then change nothing and claim nothing: no "removed" sentence, no exit 0, no stop request to a watcher.
func TestVD_AutoOffNeverClaimsARemovalWhenTheTaskListingFailed(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "work.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	if _, err := vdTestRun(t, false, "add", p, "as", "work"); err != nil {
		t.Fatal(err)
	}
	old := vdAutoTasksQuery
	t.Cleanup(func() { vdAutoTasksQuery = old })
	vdAutoTasksQuery = func() (map[string]bool, error) { return map[string]bool{}, errors.New("schtasks.exe: access denied") }

	var err error
	out := captureStdout(t, func() { err = vdAutoOff("work", true) })
	if err == nil {
		t.Fatalf("auto off succeeded although the task listing failed; output %q", out)
	}
	if vdExitClass(err) == vdisk.ExitUsage {
		t.Errorf("auto off called a failed listing a usage error (%v); the name is registered", err)
	}
	if strings.Contains(strings.ToLower(out), "removed") {
		t.Errorf("auto off said something was removed: %q", out)
	}
	id := vdKeepRegisteredID(p)
	if id == "" {
		t.Fatal("the container has no registered id")
	}
	for _, sp := range vdKeepStopPaths(id) {
		if _, serr := os.Stat(sp); serr == nil {
			t.Errorf("a stop request was written (%s) although nothing was known about the task", sp)
		}
	}
}

// The ordinary no-task answer still works when the listing succeeds and is empty.
func TestVD_AutoOffWithAnEmptyListingStillEndsAWatcher(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "work.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	if _, err := vdTestRun(t, false, "add", p, "as", "work"); err != nil {
		t.Fatal(err)
	}
	old := vdAutoTasksQuery
	t.Cleanup(func() { vdAutoTasksQuery = old })
	vdAutoTasksQuery = func() (map[string]bool, error) { return map[string]bool{}, nil }

	var err error
	captureStdout(t, func() { err = vdAutoOff("work", true) })
	if err != nil {
		t.Fatalf("auto off with an empty listing: %v", err)
	}
	for _, sp := range vdKeepStopPaths(vdKeepRegisteredID(p)) {
		if _, serr := os.Stat(sp); serr != nil {
			t.Errorf("no stop request was written to %s: %v", sp, serr)
		}
	}
}
