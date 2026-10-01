//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"
)

// SP-0070 AUD-34-F6 / SP-0066 AUD-30-F1: one registered container on a network
// path that never answers must not hide every other disk. The window kills
// `vd status json` after 8 s; a file on a dead path takes about 20 s to fail.

func TestVdBuildSnapshot_AHungFileIsBoundedAndTheRestStillAnswer(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	for _, n := range []string{"dead1", "dead2", "live"} {
		name := n
		if err := vdUpdateRegistry(func(r *vdRegistry) error {
			r.Containers = append(r.Containers, vdRegEntry{Name: name, Path: `C:\fdd\` + name + `.fdd`, ContainerID: "id-" + name})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	oldFn, oldTO := vdSnapFileFn, vdSnapFileTimeout
	defer func() { vdSnapFileFn, vdSnapFileTimeout = oldFn, oldTO }()
	vdSnapFileTimeout = 300 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	var mu sync.Mutex
	started := 0
	vdSnapFileFn = func(row *vdSnapContainer, wantID string) {
		if row.Name == "live" {
			row.File = "ok"
			return
		}
		mu.Lock()
		started++
		mu.Unlock()
		<-release // a path that never answers
	}

	start := time.Now()
	s, err := vdBuildSnapshot()
	took := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if took > 5*time.Second {
		t.Fatalf("the snapshot took %v with two dead files; each is bounded to %v and they run in parallel", took, vdSnapFileTimeout)
	}
	files := map[string]string{}
	reasons := map[string]string{}
	for _, d := range s.Disks {
		if c, ok := d.(vdSnapContainer); ok {
			files[c.Name], reasons[c.Name] = c.File, c.FileError
		}
	}
	if files["live"] != "ok" {
		t.Errorf("the reachable container reads %q, want ok", files["live"])
	}
	for _, n := range []string{"dead1", "dead2"} {
		if files[n] != "unreadable" || reasons[n] == "" {
			t.Errorf("%s reads %q (%q), want unreadable with a reason - never missing", n, files[n], reasons[n])
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if started != 2 {
		t.Errorf("%d dead files were inspected in parallel, want 2", started)
	}
}

func vdBoundTestRegistry(t *testing.T, n int) {
	t.Helper()
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("c%02d", i)
		if err := vdUpdateRegistry(func(r *vdRegistry) error {
			r.Containers = append(r.Containers, vdRegEntry{Name: name, Path: `C:\fdd\` + name + `.fdd`, ContainerID: "id-" + name})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// AUD-34-F6: a registry of many entries never has more files in flight than
// the bound, and every row still gets its answer.
func TestVdSnapFiles_NeverMoreInFlightThanTheBound(t *testing.T) {
	vdBoundTestRegistry(t, 12)
	oldFn, oldW := vdSnapFileFn, vdSnapFileWorkers
	defer func() { vdSnapFileFn, vdSnapFileWorkers = oldFn, oldW }()
	vdSnapFileWorkers = 3
	var mu sync.Mutex
	inFlight, peak := 0, 0
	vdSnapFileFn = func(row *vdSnapContainer, wantID string) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		row.File = "ok"
	}
	s, err := vdBuildSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, d := range s.Disks {
		if c, ok := d.(vdSnapContainer); ok {
			rows++
			if c.File != "ok" {
				t.Errorf("%s reads %q, want ok", c.Name, c.File)
			}
		}
	}
	if rows != 12 {
		t.Errorf("%d rows, want 12", rows)
	}
	mu.Lock()
	defer mu.Unlock()
	if peak > 3 || peak < 2 {
		t.Errorf("peak %d files in flight, want at most 3 (and more than one: they run in parallel)", peak)
	}
}

// AUD-34-F6: a stop ends the build at once, even while every file hangs, and
// the run is a stop - not a half-filled list.
func TestVdBuildSnapshot_AStopEndsTheWaitAtOnce(t *testing.T) {
	vdBoundTestRegistry(t, 6)
	oldFn, oldTO, oldStop, oldPoll := vdSnapFileFn, vdSnapFileTimeout, vdSnapStopped, vdSnapStopPoll
	defer func() {
		vdSnapFileFn, vdSnapFileTimeout, vdSnapStopped, vdSnapStopPoll = oldFn, oldTO, oldStop, oldPoll
	}()
	vdSnapFileTimeout = time.Minute
	vdSnapStopPoll = 10 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	vdSnapFileFn = func(row *vdSnapContainer, wantID string) { <-release }
	asked := time.Now().Add(150 * time.Millisecond)
	vdSnapStopped = func() bool { return time.Now().After(asked) }

	start := time.Now()
	_, err := vdBuildSnapshot()
	if !errors.Is(err, errRunStopped) {
		t.Fatalf("a stopped build returned %v, want errRunStopped", err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("the build took %v after the stop; it must not wait on a hung file", took)
	}
}

func TestVdNetworkGone_IsNotMissing(t *testing.T) {
	for _, code := range []syscall.Errno{53, 64, 67, 1222, 1231} {
		err := &os.PathError{Op: "CreateFile", Path: `\nas\share\a.fdd`, Err: code}
		if !vdNetworkGone(err) {
			t.Errorf("errno %d is not read as a network failure", code)
		}
	}
	for _, err := range []error{
		&os.PathError{Op: "CreateFile", Path: `C:\a.fdd`, Err: syscall.Errno(2)},
		&os.PathError{Op: "CreateFile", Path: `C:\a.fdd`, Err: syscall.Errno(3)},
		errors.New("something else"),
	} {
		if vdNetworkGone(err) {
			t.Errorf("%v was read as a network failure", err)
		}
	}
}
