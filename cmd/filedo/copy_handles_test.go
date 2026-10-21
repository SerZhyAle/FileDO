package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// SP-0032 AUD-01-F1: closing a handle with a synchronous ReadFile pending
// waits for that read to return - in the pinned toolchain FD.Close blocks on
// the descriptor's last reference - so on a device that never answers the
// close never returns either. The stall watchdog and the stop must not wait on
// it: the stall is reported within the watchdog period plus a bounded margin,
// a stop ends the copy within a bounded time, and the partial is removed once
// the handle is really closed.

// hungClose stands for os.File.Close on a handle with a read in flight: it
// returns only when release is closed.
type hungClose struct {
	release chan struct{}
	entered atomic.Bool
}

func (h *hungClose) Close() error {
	h.entered.Store(true)
	<-h.release
	return nil
}

func TestCopyStreamWatched_TheWatchdogNeverWaitsOnAHungClose(t *testing.T) {
	const watchdog = 200 * time.Millisecond
	readRelease := make(chan struct{})
	defer close(readRelease)
	closer := &hungClose{release: make(chan struct{})}
	defer close(closer.release)

	start := time.Now()
	_, err := copyStreamWatched(context.Background(), io.Discard, &blockingReader{release: readRelease},
		make([]byte, 64<<10), watchdog, nil, func() { closer.Close() }, nil)
	if !isCopyStall(err) {
		t.Fatalf("a stalled copy returned %v, want a stall", err)
	}
	if took := time.Since(start); took > 3*watchdog+copyStopGrace {
		t.Errorf("the stall was reported after %v; the watchdog is %v and the close never returns", took, watchdog)
	}
	// The close runs on a goroutine nobody waits on: give it a moment to start.
	deadline := time.Now().Add(time.Second)
	for !closer.entered.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !closer.entered.Load() {
		t.Error("the stall never tried to close the handle it gave up on")
	}
}

func TestCopyStreamWatched_AStopNeverWaitsOnAHungClose(t *testing.T) {
	readRelease := make(chan struct{})
	defer close(readRelease)
	closer := &hungClose{release: make(chan struct{})}
	defer close(closer.release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := copyStreamWatched(ctx, io.Discard, &blockingReader{release: readRelease},
		make([]byte, 64<<10), 0, nil, func() { closer.Close() }, nil)
	if err != errRunStopped {
		t.Fatalf("a stopped copy returned %v, want errRunStopped", err)
	}
	if took := time.Since(start); took > 6*copyStopGrace {
		t.Errorf("the stop ended the copy after %v; the bound is 4x copyStopGrace and the close never returns", took)
	}
}

// discard hands the close to a goroutine nobody waits on, returns within its
// wait, and removes the partial once the handle is really closed.
func TestCopyHandles_DiscardDoesNotWaitOnAHungClose(t *testing.T) {
	partial := filepath.Join(t.TempDir(), "x.bin.filedo-partial")
	if err := os.WriteFile(partial, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	closer := &hungClose{release: make(chan struct{})}
	h := newCopyHandles(partial, closer)

	start := time.Now()
	h.discard(100 * time.Millisecond)
	if took := time.Since(start); took > time.Second {
		t.Fatalf("discard waited %v on a close that never returns", took)
	}
	if !exists(partial) {
		t.Fatal("the partial was removed while its handle was still open")
	}

	close(closer.release) // the device finally answers
	deadline := time.Now().Add(2 * time.Second)
	for exists(partial) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if exists(partial) {
		t.Error("the partial was not removed once the handle closed")
	}
}

func TestCopyHandles_ReleaseIsIdempotentAndClosesEveryHandle(t *testing.T) {
	var closes atomic.Int32
	a, b := &countingCloser{n: &closes}, &countingCloser{n: &closes}
	h := newCopyHandles("", a, b)
	h.release()
	h.release()
	h.discard(time.Second)
	if got := closes.Load(); got != 2 {
		t.Errorf("%d closes, want one per handle (2)", got)
	}
}

type countingCloser struct{ n *atomic.Int32 }

func (c *countingCloser) Close() error { c.n.Add(1); return nil }
