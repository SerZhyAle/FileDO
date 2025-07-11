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
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		fmt.Printf("\n\n⚠ Interrupt signal received (Ctrl+C). Cleaning up..\n")
		handler.Interrupt()
	}()

	return handler
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
