package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
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
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

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
			os.Exit(1)
		} else {
			// Ctrl+C after grace period - treat as new first Ctrl+C
			ih.firstCtrlC = now
			fmt.Printf("\n⚠ Interrupt signal received. Press Ctrl+C again within 3 seconds to force exit.\n")
		}
	} else if sig == syscall.SIGTERM {
		// SIGTERM - graceful shutdown
		ih.interrupted = true
		for i := len(ih.cleanupFns) - 1; i >= 0; i-- {
			ih.cleanupFns[i]()
		}
		ih.cancel()
	}
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

// WithTimeoutContext creates a context with timeout based on the interrupt handler's context
func (ih *InterruptHandler) WithTimeoutContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ih.ctx, timeout)
}
