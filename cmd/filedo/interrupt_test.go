package main

import (
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
