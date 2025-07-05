package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

type InterruptHandler struct {
	ctx        context.Context
	cancel     context.CancelFunc
	cleanupFns []func()
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
	ih.cleanupFns = append(ih.cleanupFns, fn)
}

func (ih *InterruptHandler) Interrupt() {
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
