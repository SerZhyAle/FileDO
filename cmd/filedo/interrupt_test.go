package main

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestInterruptCleanupMayQueryHandler is CLI-02: a cleanup that asks the
// handler about its own state used to re-lock the mutex the handler held while
// running it, and the process froze with no result. The stop must now return
// promptly whatever the cleanup asks.
func TestInterruptCleanupMayQueryHandler(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	sawInterrupted := make(chan bool, 1)
	ih.AddCleanup(func() {
		_ = ih.IsForceExit()
		_ = ih.IsCancelled()
		sawInterrupted <- ih.IsInterrupted()
		// Registering from inside a cleanup must not deadlock either.
		remove := ih.AddCleanup(func() {})
		remove()
	})

	done := make(chan struct{})
	go func() {
		ih.Interrupt()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Interrupt() did not return within 1 s: a cleanup querying the handler deadlocked it")
	}
	if !<-sawInterrupted {
		t.Fatal("a cleanup must see the handler as interrupted")
	}
	if !ih.IsCancelled() {
		t.Fatal("the context must be cancelled by a stop")
	}
}

// TestInterruptCancelsBeforeCleanups: a slow cleanup must not delay the
// cancellation every loop is watching.
func TestInterruptCancelsBeforeCleanups(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	cancelledFirst := make(chan bool, 1)
	ih.AddCleanup(func() {
		cancelledFirst <- ih.IsCancelled()
	})
	ih.Interrupt()
	if !<-cancelledFirst {
		t.Fatal("the context must already be cancelled when the cleanups run")
	}
}

// TestAddCleanupRemove is CLI-20: a registration is removable, a removed
// cleanup never runs, and the list is bounded by what is still registered.
func TestAddCleanupRemove(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	ran := map[string]bool{}
	removeA := ih.AddCleanup(func() { ran["a"] = true })
	ih.AddCleanup(func() { ran["b"] = true })
	removeC := ih.AddCleanup(func() { ran["c"] = true })

	removeA()
	removeC()
	removeC() // removing twice is harmless
	if got := ih.cleanupCount(); got != 1 {
		t.Fatalf("cleanupCount = %d after removing two of three, want 1", got)
	}

	ih.Interrupt()
	if ran["a"] || ran["c"] {
		t.Fatalf("a removed cleanup ran: %v", ran)
	}
	if !ran["b"] {
		t.Fatal("the remaining cleanup did not run")
	}
}

// TestAddCleanupBoundedByInFlight mimics the per-file registrations of the
// copy engine: after 20 000 completed items, nothing is left registered.
func TestAddCleanupBoundedByInFlight(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	const workers = 4
	sem := make(chan struct{}, workers)
	done := make(chan struct{})
	maxSeen := 0
	for i := 0; i < 20000; i++ {
		sem <- struct{}{}
		go func() {
			removeClose := ih.AddCleanup(func() {})
			removePartial := ih.AddCleanup(func() {})
			removePartial()
			removeClose()
			<-sem
			done <- struct{}{}
		}()
		if n := ih.cleanupCount(); n > maxSeen {
			maxSeen = n
		}
	}
	for i := 0; i < 20000; i++ {
		<-done
	}
	if got := ih.cleanupCount(); got != 0 {
		t.Fatalf("cleanupCount = %d after every item completed, want 0", got)
	}
	if maxSeen > 2*workers {
		t.Fatalf("saw %d registrations with %d items in flight, want <= %d", maxSeen, workers, 2*workers)
	}
}

// TestInterruptRunsCleanupsNewestFirst keeps the order the handler always had.
func TestInterruptRunsCleanupsNewestFirst(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	var order []int
	for i := 1; i <= 3; i++ {
		i := i
		ih.AddCleanup(func() { order = append(order, i) })
	}
	ih.Interrupt()
	if len(order) != 3 || order[0] != 3 || order[1] != 2 || order[2] != 1 {
		t.Fatalf("cleanup order = %v, want [3 2 1]", order)
	}
}

// TestVerdictDefectOutranksNotProven is CLI-06.
func TestVerdictDefectOutranksNotProven(t *testing.T) {
	saved := globalInterruptHandler
	globalInterruptHandler = nil
	defer func() { globalInterruptHandler = saved }()

	ro := &runOutcome{defects: 1, notProven: true, kind: runJudges}
	if v := ro.verdict(); v != VerdictFailed {
		t.Fatalf("verdict = %q, want %q", v, VerdictFailed)
	}
	if code := exitCodeFor(ro.verdict()); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	ro = &runOutcome{notProven: true, kind: runJudges}
	if v := ro.verdict(); v != VerdictNotProven {
		t.Fatalf("verdict = %q, want %q", v, VerdictNotProven)
	}
}

// TestBreakSignalLeavesSignalGoroutineFree is AUD-14-F2: Ctrl+Break and SIGTERM
// used to run every cleanup on the one goroutine that reads signals, so a
// cleanup blocked on a hung device kept each later Ctrl+C unread and the
// double-Ctrl+C force exit never came. handleSignal must return at once with
// the context cancelled, and the cleanups must still run.
func TestBreakSignalLeavesSignalGoroutineFree(t *testing.T) {
	sigs := []os.Signal{syscall.SIGTERM}
	if s := sigBreakSignal(); s != nil {
		sigs = append(sigs, s)
	}
	for _, sig := range sigs {
		ih := newInterruptHandlerNoSignals()
		release := make(chan struct{})
		started := make(chan struct{})
		ih.AddCleanup(func() {
			close(started)
			<-release
		})

		done := make(chan struct{})
		go func() {
			ih.handleSignal(sig)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("handleSignal(%v) did not return within 1 s: a blocked cleanup holds the signal goroutine", sig)
		}
		if !ih.IsCancelled() || !ih.IsInterrupted() {
			t.Fatalf("handleSignal(%v) must cancel the context and mark the run interrupted before returning", sig)
		}
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatalf("handleSignal(%v): the cleanups never ran", sig)
		}
		close(release)
	}
}

// AUD-02-F2 (CLI-07c): a forced exit gives ordinary cleanups the short budget
// and abandons the ones that outlast it, but it waits for a critical cleanup -
// the probe's sector restore - because cutting that one leaves the user's
// file system holding the probe's markers.
func TestForcedExitWaitsForACriticalCleanup(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	const work = 300 * time.Millisecond
	var ordinaryDone, criticalDone atomic.Bool
	ih.AddCleanup(func() {
		time.Sleep(work)
		ordinaryDone.Store(true)
	})
	ih.AddCriticalCleanup(func() {
		time.Sleep(work)
		criticalDone.Store(true)
	})

	var announced int
	started := time.Now()
	normalOK, criticalOK := ih.runForcedCleanups(20*time.Millisecond, 5*time.Second, func(n int) { announced = n })
	if normalOK {
		t.Error("an ordinary cleanup that outlasts its budget was reported as finished")
	}
	if !criticalOK || !criticalDone.Load() {
		t.Fatalf("the forced exit did not wait for the critical cleanup (finished=%v, ran to the end=%v)", criticalOK, criticalDone.Load())
	}
	if since := time.Since(started); since < work {
		t.Errorf("the forced cleanups returned after %v, before the critical cleanup's %v of work", since, work)
	}
	if announced != 1 {
		t.Errorf("the forced exit announced %d critical cleanup(s) still running, want 1", announced)
	}
}

// A critical cleanup is bounded too: a device that never answers cannot keep
// the process alive for ever, and the caller is told it was cut.
func TestForcedExitBoundsACriticalCleanup(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	release := make(chan struct{})
	defer close(release)
	ih.AddCriticalCleanup(func() { <-release })

	started := time.Now()
	_, criticalOK := ih.runForcedCleanups(10*time.Millisecond, 150*time.Millisecond, nil)
	if criticalOK {
		t.Error("a critical cleanup that never returned was reported as finished")
	}
	if since := time.Since(started); since > 2*time.Second {
		t.Errorf("the forced cleanups took %v; the critical budget is 150 ms", since)
	}
}

// With nothing registered as critical the forced exit costs the short budget
// at most, as before.
func TestForcedExitWithoutCriticalCleanupsIsShort(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	ih.AddCleanup(func() {})
	started := time.Now()
	normalOK, criticalOK := ih.runForcedCleanups(time.Second, time.Minute, func(int) { t.Error("announced a critical cleanup that does not exist") })
	if !normalOK || !criticalOK {
		t.Errorf("normal=%v critical=%v, want both finished", normalOK, criticalOK)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Errorf("an empty forced exit took %v", time.Since(started))
	}
}

// The probe's restore is registered as critical, and a graceful stop leaves it
// alone (the probe restores on its own way out).
func TestProbeRestoreIsACriticalForcedExitCleanup(t *testing.T) {
	ih := newInterruptHandlerNoSignals()
	restores := 0
	unregister := registerProbeRestore(ih)(func() { restores++ })
	defer unregister()

	if got := ih.criticalCount(); got != 1 {
		t.Fatalf("the probe registered %d critical cleanups, want 1", got)
	}
	ih.runCleanups() // a graceful stop: not a forced exit
	if restores != 0 {
		t.Errorf("a graceful stop ran the forced-exit restore %d time(s)", restores)
	}
	ih.forceExit.Store(true)
	ih.runCleanups()
	if restores != 1 {
		t.Errorf("a forced exit ran the restore %d time(s), want 1", restores)
	}
}
