package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	"runtime"
)

type InterruptHandler struct {
	ctx         context.Context
	cancel      context.CancelFunc
	cleanupFns  []func()
	interrupted bool
	forceExit   bool
	firstCtrlC  time.Time
	mu          sync.Mutex
}

func NewInterruptHandler() *InterruptHandler {
	ctx, cancel := context.WithCancel(context.Background())

	handler := &InterruptHandler{
		ctx:        ctx,
		cancel:     cancel,
		cleanupFns: make([]func(), 0),
	}

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

func (ih *InterruptHandler) handleSignal(sig os.Signal) {
	ih.mu.Lock()
	defer ih.mu.Unlock()

	if sig == os.Interrupt {
		now := time.Now()
		
		if !ih.interrupted {
			// First Ctrl+C - graceful shutdown
			ih.interrupted = true
			ih.firstCtrlC = now
			fmt.Printf("\n\n⚠ Interrupt signal received (Ctrl+C). Cleaning up gracefully...\n")
			fmt.Printf("Press Ctrl+C again within 3 seconds to force immediate exit.\n")
			
			// Run cleanup functions in reverse order
			for i := len(ih.cleanupFns) - 1; i >= 0; i-- {
				ih.cleanupFns[i]()
			}
			ih.cancel()
			
			// Start timer to reset force exit window
			go func() {
				time.Sleep(3 * time.Second)
				ih.mu.Lock()
				if ih.interrupted && !ih.forceExit {
					fmt.Printf("Grace period expired. Use Ctrl+C again if needed.\n")
				}
				ih.mu.Unlock()
			}()
			
		} else if !ih.forceExit && now.Sub(ih.firstCtrlC) <= 3*time.Second {
			// Second Ctrl+C within 3 seconds - immediate exit
			ih.forceExit = true
			fmt.Printf("\n🔥 Force exit requested! Terminating all processes immediately...\n")
			// Execute cleanup functions to allow partial file cleanup
			for i := len(ih.cleanupFns) - 1; i >= 0; i-- {
				ih.cleanupFns[i]()
			}
			os.Exit(1)
		} else {
			// Ctrl+C after grace period - treat as new first Ctrl+C
			ih.firstCtrlC = now
			fmt.Printf("\n⚠ Interrupt signal received. Press Ctrl+C again within 3 seconds to force exit.\n")
		}
	} else if sig == syscall.SIGTERM || (func() bool { sb := sigBreakSignal(); return sb != nil && sig == sb })() {
		// SIGTERM - graceful shutdown
		ih.interrupted = true
		for i := len(ih.cleanupFns) - 1; i >= 0; i-- {
			ih.cleanupFns[i]()
		}
		ih.cancel()
	} else {
		// Other signals ignored
	}
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

func (ih *InterruptHandler) AddCleanup(fn func()) {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	ih.cleanupFns = append(ih.cleanupFns, fn)
}

func (ih *InterruptHandler) Interrupt() {
	ih.mu.Lock()
	defer ih.mu.Unlock()

	if ih.interrupted {
		return // Already interrupted
	}

	ih.interrupted = true
	ih.firstCtrlC = time.Now()

	// Run cleanup functions in reverse order
	for i := len(ih.cleanupFns) - 1; i >= 0; i-- {
		ih.cleanupFns[i]()
	}
	ih.cancel()
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
	ih.mu.Lock()
	defer ih.mu.Unlock()
	return ih.interrupted
}

func (ih *InterruptHandler) IsForceExit() bool {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	return ih.forceExit
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
