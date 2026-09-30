//go:build windows

package main

import (
	"errors"
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
