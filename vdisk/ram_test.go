package vdisk

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type transientSaveBacking struct {
	*memBacking
	writes int
	failAt int
}

func (b *transientSaveBacking) WriteAt(p []byte, off int64) (int, error) {
	b.writes++
	if b.writes == b.failAt {
		return 0, errors.New("temporary backing write failure")
	}
	return b.memBacking.WriteAt(p, off)
}

// A data-region error keeps the snapshot in RAM. A later save must replay it,
// including a newer write to the same cluster, and clear the interrupted-save
// marker only after the replay completes. A header write error is sticky:
// see TestVD_RAMSaveHeaderWriteFailureIsSticky (AUD-35-F5).
func TestVD_RAMSaveRetriesAfterTemporaryWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{"data", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
			old := pattern(11, 4096)
			if _, err := c.WriteAt(old, 0); err != nil {
				t.Fatal(err)
			}
			b := &transientSaveBacking{memBacking: mb, failAt: tc.failAt}
			c.b = b
			if err := c.Save(); !errors.Is(err, ErrIO) {
				t.Fatalf("first save: %v, want I/O error", err)
			}
			if s, _ := c.RAMState(); s.DirtyBytes != 1<<16 || s.Saving {
				t.Fatalf("snapshot lost after failure: %+v", s)
			}
			newer := pattern(12, 4096)
			if _, err := c.WriteAt(newer, 0); err != nil {
				t.Fatalf("write after recoverable failure: %v", err)
			}
			if err := c.Save(); err != nil {
				t.Fatalf("retry: %v", err)
			}
			if info := c.Info(); info.SaveInProgress {
				t.Fatal("completed retry still carries the interrupted-save marker")
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			r := mustOpenMem(t, mb, OpenRead)
			got := make([]byte, len(newer))
			if _, err := r.ReadAt(got, 0); err != nil || !bytes.Equal(got, newer) {
				t.Fatalf("retry did not save the newest bytes: err=%v", err)
			}
		})
	}
}

// AUD-35-F5, owner decision 2026-10-01: a ram save whose header write fails is
// a fail-stop. Which header copy the file holds is uncertain then, so the
// container writes nothing more - a later Save, a WriteAt and Close all return
// the error, even with the backing healthy again - while a failed data write
// in the same position is retried (the test above).
func TestVD_RAMSaveHeaderWriteFailureIsSticky(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failAt int
	}{
		{"start primary header", 1},
		{"start backup header", 2},
		{"final primary header", 4},
		{"final backup header", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
			if _, err := c.WriteAt(pattern(11, 4096), 0); err != nil {
				t.Fatal(err)
			}
			b := &transientSaveBacking{memBacking: mb, failAt: tc.failAt}
			c.b = b
			if err := c.Save(); !errors.Is(err, ErrIO) {
				t.Fatalf("first save: %v, want I/O error", err)
			}
			if s, _ := c.RAMState(); s.DirtyBytes != 1<<16 || s.SaveError == "" {
				t.Fatalf("after a failed header write: %+v, want the snapshot kept and the error reported", s)
			}
			// The backing is healthy from here on (failAt has passed).
			if err := c.Save(); !errors.Is(err, ErrIO) {
				t.Fatalf("save after a failed header write: %v, want the sticky I/O error", err)
			}
			if _, err := c.WriteAt(pattern(12, 4096), 0); !errors.Is(err, ErrIO) {
				t.Fatalf("write after a failed header write: %v, want the sticky I/O error", err)
			}
			if s, _ := c.RAMState(); s.SaveError == "" {
				t.Fatalf("the sticky failure is not reported: %+v", s)
			}
			if err := c.Close(); err == nil {
				t.Fatal("Close after a failed header write reported success")
			}
			if r := mustOpenMem(t, mb, OpenRead); r.Info().Clean {
				t.Fatal("a container whose header write failed reopened as closed cleanly")
			}
		})
	}
}

func TestVD_SavePolicyRetriesAfterTemporaryWriteFailure(t *testing.T) {
	oldTick, oldRetry := savePolicyTick, savePolicyRetry
	savePolicyTick, savePolicyRetry = 5*time.Millisecond, 25*time.Millisecond
	t.Cleanup(func() { savePolicyTick, savePolicyRetry = oldTick, oldRetry })

	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
	// The session's first write clears the clean marker (two header writes)
	// before the failing backing goes in, so write 3 is the save's data write
	// and not a header write, which would be a fail-stop (AUD-35-F5).
	if _, err := c.WriteAt(pattern(13, 4096), 0); err != nil {
		t.Fatal(err)
	}
	b := &transientSaveBacking{memBacking: mb, failAt: 3}
	c.b = b
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var mu sync.Mutex
	var logs []string
	done := make(chan struct{})
	go func() {
		RunSavePolicy(ctx, c, SavePolicy{Every: time.Hour, DirtyLimit: 1 << 16}, func(format string, _ ...interface{}) {
			mu.Lock()
			logs = append(logs, format)
			mu.Unlock()
		})
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s, _ := c.RAMState(); s.DirtyBytes == 0 && !s.LastGoodSave.IsZero() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if s, _ := c.RAMState(); s.DirtyBytes != 0 || s.LastGoodSave.IsZero() {
		t.Fatalf("the policy did not retry the save: %+v", s)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) < 2 || logs[0] != "ram: save failed; retrying in %s: %v" || logs[1] != "ram: saved %d MB in %d ms" {
		t.Fatalf("the failed save and retry were not logged: %v", logs)
	}
}

// gateBacking holds the first data-region write until release is closed, so a
// test can act while a save is between its steps.
type gateBacking struct {
	*memBacking
	from    int64
	to      int64
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (g *gateBacking) WriteAt(p []byte, off int64) (int, error) {
	if off >= g.from && off < g.to {
		g.once.Do(func() {
			close(g.entered)
			<-g.release
		})
	}
	return g.memBacking.WriteAt(p, off)
}

// TestVD_RAMSaveDoesNotBlock: a save writes its snapshot while the volume is
// read and written; what was written during the save is in memory, not in
// the file, until the next save - and the file is the snapshot, exactly.
func TestVD_RAMSaveDoesNotBlock(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
	a := pattern(1, 3<<16)
	if _, err := c.WriteAt(a, 0); err != nil {
		t.Fatal(err)
	}
	g := &gateBacking{memBacking: mb, from: int64(c.hdr.DataOffset), to: int64(c.hdr.DataOffset) + 16<<16, entered: make(chan struct{}), release: make(chan struct{})}
	c.b = g
	done := make(chan error, 1)
	go func() { done <- c.Save() }()
	select {
	case <-g.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the save never reached the data region")
	}
	// The save is parked inside its second step. The volume must still serve.
	b := pattern(2, 1000)
	finished := make(chan error, 1)
	go func() {
		if _, err := c.WriteAt(b, 5); err != nil { // cluster 0, part of the snapshot
			finished <- err
			return
		}
		if _, err := c.WriteAt(b, 7<<16); err != nil { // cluster 7, not in it
			finished <- err
			return
		}
		got := make([]byte, 3<<16)
		if _, err := c.ReadAt(got, 0); err != nil {
			finished <- err
			return
		}
		want := append([]byte(nil), a...)
		copy(want[5:], b)
		if !bytes.Equal(got, want) {
			t.Error("a read during the save does not see the newest write")
		}
		s, _ := c.RAMState()
		if !s.Saving || s.DirtyBytes != 5<<16 { // 3 clusters saving + 2 new
			t.Errorf("state during the save: %+v", s)
		}
		finished <- nil
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("a write or a read waited for the save")
	}
	close(g.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// The file holds the snapshot: a, without b.
	snap := readAll(t, mustOpenMem(t, &memBacking{data: mb.snapshot()}, OpenRead))
	if !bytes.Equal(snap[:3<<16], a) || !allZero(snap[7<<16:7<<16+1000]) {
		t.Fatal("the file is not the snapshot the save began with")
	}
	if s, _ := c.RAMState(); s.Saving || s.DirtyBytes != 2<<16 {
		t.Fatalf("after the save: %+v", s)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	final := readAll(t, mustOpenMem(t, mb, OpenRead))
	want := append([]byte(nil), a...)
	copy(want[5:], b)
	if !bytes.Equal(final[:3<<16], want) || !bytes.Equal(final[7<<16:7<<16+1000], b) {
		t.Fatal("the close did not save what was written during the first save")
	}
}

// TestVD_SavePolicy: the dirty limit saves at once, the interval saves an old
// write, and nothing is saved while nothing is dirty.
func TestVD_SavePolicy(t *testing.T) {
	old := savePolicyTick
	savePolicyTick = 5 * time.Millisecond
	t.Cleanup(func() { savePolicyTick = old })

	c, _ := newMemContainer(t, CreateOptions{LogicalSize: 4 << 20, ClusterShift: 16, Profile: ProfileRAM})
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var lines []string
	stopped := make(chan struct{})
	go func() {
		RunSavePolicy(ctx, c, SavePolicy{Every: 300 * time.Millisecond, DirtyLimit: 4 << 16}, func(f string, a ...interface{}) {
			mu.Lock()
			lines = append(lines, f)
			mu.Unlock()
		})
		close(stopped)
	}()
	waitClean := func(within time.Duration) time.Duration {
		t.Helper()
		start := time.Now()
		for time.Since(start) < within {
			if s, _ := c.RAMState(); s.DirtyBytes == 0 && !s.LastGoodSave.IsZero() {
				return time.Since(start)
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("still dirty after %v", within)
		return 0
	}
	c.WriteAt(pattern(3, 5<<16), 0) // above the limit
	if d := waitClean(2 * time.Second); d > 200*time.Millisecond {
		t.Fatalf("the dirty limit saved after %v", d)
	}
	c.WriteAt(pattern(4, 1000), 1<<20) // below the limit
	time.Sleep(100 * time.Millisecond)
	if s, _ := c.RAMState(); s.DirtyBytes == 0 {
		t.Fatal("a small write was saved before its interval")
	}
	waitClean(2 * time.Second)
	cancel()
	<-stopped
	c.Close()
	if len(lines) < 2 {
		t.Fatalf("saves are not logged: %v", lines)
	}
}

// TestVD_RAMDiscard: unmount nosave keeps the last completed save and drops
// the rest, and the container says it was not closed cleanly.
func TestVD_RAMDiscard(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
	a := pattern(5, 5000)
	c.WriteAt(a, 0)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c.WriteAt(pattern(6, 5000), 0)
	if err := c.Discard(); err != nil {
		t.Fatal(err)
	}
	rc := mustOpenMem(t, mb, OpenRead)
	if rc.Info().Clean {
		t.Fatal("a discarded session is marked clean")
	}
	if !bytes.Equal(readAll(t, rc)[:5000], a) {
		t.Fatal("the discard did not leave the last completed save")
	}
}

// TestVD_WriteBack: a ram target answers SYNCHRONIZE CACHE and FUA writes
// without flushing; any other target flushes.
func TestVD_WriteBack(t *testing.T) {
	for _, wb := range []bool{false, true} {
		dev := &countDev{data: make([]byte, 1<<20)}
		l, err := newLUN(dev, 1<<20, 4096, false, "x")
		if err != nil {
			t.Fatal(err)
		}
		l.writeBack = wb
		if r := l.exec([]byte{0x35, 0, 0, 0, 0, 0, 0, 0, 0, 0}, nil); r.status != statGood {
			t.Fatal("sync cache failed")
		}
		cdb := []byte{0x2A, 0x08, 0, 0, 0, 0, 0, 0, 1, 0} // WRITE (10), FUA, 1 block
		if r := l.exec(cdb, make([]byte, 512)); r.status != statGood {
			t.Fatal("fua write failed")
		}
		if want := map[bool]int{false: 2, true: 0}[wb]; dev.flushes != want {
			t.Fatalf("writeBack %v: %d flushes, want %d", wb, dev.flushes, want)
		}
	}
}

type countDev struct {
	data    []byte
	flushes int
}

func (d *countDev) ReadAt(p []byte, off int64) (int, error)  { return copy(p, d.data[off:]), nil }
func (d *countDev) WriteAt(p []byte, off int64) (int, error) { return copy(d.data[off:], p), nil }
func (d *countDev) Flush() error                             { d.flushes++; return nil }
