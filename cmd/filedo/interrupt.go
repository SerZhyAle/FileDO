package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// InterruptHandler is the one stop model every long loop obeys (SP-0023 T1).
//
// Two rules keep it from deadlocking the program it is meant to stop. Its state
// is read through atomics, so a cleanup may ask IsInterrupted/IsForceExit while
// the handler is running it; and cleanups always run with no handler lock held,
// on a snapshot of the list, after the context is already cancelled - a slow
// cleanup can delay nothing but itself. A registration is removed by the
// function AddCleanup returns, so a copy of a million files holds as many
// cleanups as it has files in flight, not a million.
type InterruptHandler struct {
	ctx    context.Context
	cancel context.CancelFunc

	interrupted atomic.Bool
	forceExit   atomic.Bool

	mu         sync.Mutex // guards cleanups, nextID and firstCtrlC
	cleanups   map[uint64]func()
	nextID     uint64
	firstCtrlC time.Time
}

// forceExitCleanupBudget bounds how long a forced exit waits for cleanups. A
// cleanup stuck in a blocked system call must not turn "exit now" into "never".
const forceExitCleanupBudget = 2 * time.Second

func NewInterruptHandler() *InterruptHandler {
	handler := newInterruptHandlerNoSignals()

	// Setup signal handling
	sigChan := make(chan os.Signal, 2)
	// Always listen for Ctrl+C (os.Interrupt) and SIGTERM
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	// On Windows, also listen for Ctrl+Break if available
	if s := sigBreakSignal(); s != nil {
		signal.Notify(sigChan, s)
	}

	go func() {
		for sig := range sigChan {
			handler.handleSignal(sig)
		}
	}()

	return handler
}

// newInterruptHandlerNoSignals builds a handler that only a stop file or an
// explicit Interrupt can trigger - the shape the unit tests need.
func newInterruptHandlerNoSignals() *InterruptHandler {
	ctx, cancel := context.WithCancel(context.Background())
	return &InterruptHandler{
		ctx:      ctx,
		cancel:   cancel,
		cleanups: make(map[uint64]func()),
	}
}

func (ih *InterruptHandler) handleSignal(sig os.Signal) {
	if sig == os.Interrupt {
		now := time.Now()
		ih.mu.Lock()
		first := !ih.interrupted.Load()
		withinGrace := !first && now.Sub(ih.firstCtrlC) <= 3*time.Second
		if first || !withinGrace {
			ih.firstCtrlC = now
		}
		ih.mu.Unlock()

		switch {
		case first:
			// First Ctrl+C - graceful shutdown
			fmt.Printf("\n\n⚠ Interrupt signal received (Ctrl+C). Cleaning up gracefully...\n")
			fmt.Printf("Press Ctrl+C again within 3 seconds to force immediate exit.\n")
			ih.interrupted.Store(true)
			ih.cancel()
			// The cleanups run off the signal goroutine, so a second Ctrl+C is
			// still read while they work.
			go ih.runCleanups()

			// Start timer to reset force exit window
			go func() {
				time.Sleep(3 * time.Second)
				if ih.interrupted.Load() && !ih.forceExit.Load() {
					fmt.Printf("Grace period expired. Use Ctrl+C again if needed.\n")
				}
			}()

		case withinGrace && !ih.forceExit.Load():
			// Second Ctrl+C within 3 seconds - immediate exit
			ih.forceExit.Store(true)
			fmt.Printf("\n🔥 Force exit requested! Terminating all processes immediately...\n")
			// Execute cleanup functions to allow partial file cleanup, but never
			// wait on them longer than the budget.
			done := make(chan struct{})
			go func() {
				ih.runCleanups()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(forceExitCleanupBudget):
				fmt.Fprintf(os.Stderr, "Cleanup did not finish within %v; exiting anyway.\n", forceExitCleanupBudget)
			}
			// A forced exit bypasses main's deferred finishRun.  Close the
			// stream here instead, with the rule-11 "could not be verified"
			// vocabulary rather than the old misleading defect code 1.
			os.Exit(finishForcedRun())

		default:
			// Ctrl+C after grace period - treat as new first Ctrl+C
			fmt.Printf("\n⚠ Interrupt signal received. Press Ctrl+C again within 3 seconds to force exit.\n")
		}
		return
	}
	if sig == syscall.SIGTERM || (func() bool { sb := sigBreakSignal(); return sb != nil && sig == sb })() {
		// SIGTERM - graceful shutdown
		ih.Interrupt()
	}
	// Other signals ignored
}

// runCleanups calls every registered cleanup, newest first, on a snapshot
// taken under the lock and executed outside it.
func (ih *InterruptHandler) runCleanups() {
	ih.mu.Lock()
	ids := make([]uint64, 0, len(ih.cleanups))
	for id := range ih.cleanups {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
	fns := make([]func(), 0, len(ids))
	for _, id := range ids {
		fns = append(fns, ih.cleanups[id])
	}
	ih.mu.Unlock()

	for _, fn := range fns {
		runCleanupSafely(fn)
	}
}

// runCleanupSafely keeps one failing cleanup from taking the others (and the
// stop itself) down with it.
func runCleanupSafely(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Warning: cleanup panicked: %v\n", r)
		}
	}()
	fn()
}

// isWindows helper
func isWindows() bool {
	return runtime.GOOS == "windows"
}

// sigBreakSignal returns the OS signal for Ctrl+Break on Windows, or nil elsewhere
func sigBreakSignal() os.Signal {
	if isWindows() {
		return syscall.Signal(21) // syscall.SIGBREAK value on Windows
	}
	return nil
}

// AddCleanup registers fn to run when the run is stopped, and returns the
// function that unregisters it. A per-item registration must call the returned
// function when its item completes, or the list grows with every item.
func (ih *InterruptHandler) AddCleanup(fn func()) (remove func()) {
	ih.mu.Lock()
	ih.nextID++
	id := ih.nextID
	ih.cleanups[id] = fn
	ih.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			ih.mu.Lock()
			delete(ih.cleanups, id)
			ih.mu.Unlock()
		})
	}
}

// cleanupCount reports how many cleanups are registered - the bound CLI-20
// promises is "no more than the items in flight".
func (ih *InterruptHandler) cleanupCount() int {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	return len(ih.cleanups)
}

// Interrupt is the graceful stop: the stop file, SIGTERM and Ctrl+Break all end
// here. The context is cancelled before any cleanup runs.
func (ih *InterruptHandler) Interrupt() {
	if !ih.interrupted.CompareAndSwap(false, true) {
		return // Already interrupted
	}
	ih.mu.Lock()
	ih.firstCtrlC = time.Now()
	ih.mu.Unlock()

	ih.cancel()
	ih.runCleanups()
}

func (ih *InterruptHandler) Context() context.Context {
	return ih.ctx
}

func (ih *InterruptHandler) IsCancelled() bool {
	select {
	case <-ih.ctx.Done():
		return true
	default:
		return false
	}
}

func (ih *InterruptHandler) IsInterrupted() bool {
	return ih.interrupted.Load()
}

func (ih *InterruptHandler) IsForceExit() bool {
	return ih.forceExit.Load()
}

// CheckContext returns error if context is cancelled
func (ih *InterruptHandler) CheckContext() error {
	select {
	case <-ih.ctx.Done():
		return ih.ctx.Err()
	default:
		return nil
	}
}

// WatchStopFile starts a background goroutine that polls for the presence of stopFilePath.
// When the file is detected, it triggers graceful shutdown via ih.Interrupt().
func (ih *InterruptHandler) WatchStopFile(stopFilePath string) {
	if stopFilePath == "" {
		return
	}
	// A stop file that is already there is a stop requested before the run
	// began: honoured now, not at the first tick, so not even the first line
	// of a batch starts (CLI-04).
	// No note event here: nothing has been emitted yet, and the stream's
	// first event must be `run` (rule 9).
	if _, err := os.Stat(stopFilePath); err == nil {
		ih.Interrupt()
		return
	}
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ih.ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(stopFilePath); err == nil {
					EmitNoteEvent("Stop file detected; canceling operation gracefully.")
					ih.Interrupt()
					return
				}
			}
		}
	}()
}
