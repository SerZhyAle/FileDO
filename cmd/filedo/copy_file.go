package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// One file, copied the one way every copy engine copies it (SP-0027 B2,
// COPY-02, COPY-11, COPY-12, COPY-13, COPY-15).
//
// The bytes go into `<target>.filedo-partial`; only a complete, flushed file
// is given the source's modification time and renamed to the final name, with
// no replace. A failure, a stall or a stop therefore leaves nothing under the
// final name - never the truncated file that the next run used to skip as
// "already copied" and that `compare .. del source` then trusted.
//
// What is preserved: the bytes, the modification time and the read-only
// attribute. What is not: other attributes, alternate data streams, the
// access list and the creation time - the target gets the defaults of the
// folder it lands in.

// minCopyBuffer is the smallest buffer a copy reads with. A zero-length Read
// returns (0, nil) forever on Windows, which is how a single empty file once
// spun a core for ten seconds and then landed on the skip list (COPY-12).
const minCopyBuffer = 64 * 1024

// copyStopGrace is how long a stopped copy waits for its loop to notice the
// stop between two buffers before it closes the handles under it.
const copyStopGrace = 500 * time.Millisecond

// copyStallError is a copy that moved no byte for the watchdog period.
type copyStallError struct{ after time.Duration }

func (e *copyStallError) Error() string {
	return fmt.Sprintf("no data moved for %v (the read or write did not return)", e.after)
}

func isCopyStall(err error) bool {
	var s *copyStallError
	return errors.As(err, &s)
}

// errTooManyStalledReads ends a run whose source has left so many reads
// blocked that starting one more would only add another stuck thread
// (COPY-15). Closing a handle does not unblock a pending synchronous read on
// Windows, so every abandoned attempt keeps a thread until the device answers.
var errTooManyStalledReads = errors.New("too many reads on the source are still blocked - stopping instead of starting more")

// maxAbandonedCopies bounds the copies whose loop was given up on after a
// stall and has not returned yet. A variable so a test can lower it.
var maxAbandonedCopies int32 = 16

// abandonedCopies is how many of those there are right now.
var abandonedCopies atomic.Int32

// copyAttemptAllowed reports whether another copy may start: not while the
// source already holds maxAbandonedCopies reads blocked.
func copyAttemptAllowed() bool {
	return abandonedCopies.Load() < maxAbandonedCopies
}

// fileCopyOptions tunes copyOneFile.
type fileCopyOptions struct {
	// Buffer is the read buffer; one shorter than minCopyBuffer is replaced.
	Buffer []byte
	// NoProgress is the watchdog: the copy fails when no byte moved for this
	// long. Zero means no watchdog. It is never a limit on the total time -
	// a large file on a slow stick takes as long as it takes (COPY-11).
	NoProgress time.Duration
	// Replace lets the final rename replace an existing target. Only an empty
	// leftover target is ever replaced (skipDecision).
	Replace bool
	// OnBytes, when set, is told how many bytes each write added.
	OnBytes func(n int64)
	// Handler, when set, gets a cleanup for this one file that closes its
	// handles and removes its partial - so a forced exit leaves nothing behind.
	// The registration ends with the file (CLI-20).
	Handler *InterruptHandler
	// Release, when set, gives Buffer back to its pool. It is called exactly
	// once, by whichever side uses the buffer last: a copy loop given up on
	// after a stall may still read into it, and a pooled buffer handed to
	// another worker meanwhile would mix two files.
	Release func()
}

// copyDiscardWait is how long a failed or stopped copy waits for its handles to
// close and its partial to go before it reports; what is left is finished in
// the background. On a healthy device both take microseconds.
const copyDiscardWait = 500 * time.Millisecond

// copyHandles owns the handles of one copy: the source and the partial.
//
// Closing an os.File with a synchronous ReadFile or WriteFile pending waits
// for that call to return (Go's poll.FD.Close waits on the descriptor's last
// reference), so on a device that never answers Close never returns either.
// Nothing that has to end - the stall watchdog, a stop, a forced exit - may
// therefore wait on a close (AUD-01-F1): release starts it on a goroutine
// nobody waits on and returns at once, and the partial is removed by that
// goroutine after the handles are really closed, because a Remove while the
// partial is open fails with a sharing violation and leaves it behind.
type copyHandles struct {
	closers []io.Closer
	partial string
	once    sync.Once
	closed  chan struct{}
}

func newCopyHandles(partial string, closers ...io.Closer) *copyHandles {
	return &copyHandles{closers: closers, partial: partial, closed: make(chan struct{})}
}

// release starts closing every handle and returns at once. It is safe to call
// from any number of goroutines and any number of times.
func (h *copyHandles) release() {
	h.once.Do(func() {
		go func() {
			for _, c := range h.closers {
				c.Close()
			}
			close(h.closed)
		}()
	})
}

// discard releases the handles and removes the partial once they are closed. It
// waits at most wait for that; a close that has not returned by then is left to
// its goroutine, which removes the partial whenever the device answers.
func (h *copyHandles) discard(wait time.Duration) {
	h.release()
	removed := make(chan struct{})
	go func() {
		<-h.closed
		if h.partial != "" {
			os.Remove(h.partial)
		}
		close(removed)
	}()
	select {
	case <-removed:
	case <-time.After(wait):
	}
}

// copyOneFile copies src to dst through a partial file. The caller has
// already decided that dst may be written (skipDecision) and that its folder
// exists.
func copyOneFile(ctx context.Context, src string, info os.FileInfo, dst string, opt fileCopyOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	release := opt.Release
	if release == nil {
		release = func() {}
	}
	loopStarted := false
	defer func() {
		if !loopStarted {
			release()
		}
	}()
	// Nothing is created after a stop (COPY-04).
	if ctx.Err() != nil {
		return errRunStopped
	}
	if !copyAttemptAllowed() {
		return errTooManyStalledReads
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("cannot open the source: %w", err)
	}
	out, err := createPartial(dst)
	if err != nil {
		in.Close()
		return fmt.Errorf("cannot create the target: %w", err)
	}
	partialName := dst + partialSuffix

	handles := newCopyHandles(partialName, in, out.File)
	unblock := handles.release
	if opt.Handler != nil {
		remove := opt.Handler.AddCleanup(func() {
			handles.discard(copyDiscardWait)
		})
		defer remove()
	}

	buf := opt.Buffer
	if len(buf) < minCopyBuffer {
		buf = make([]byte, minCopyBuffer)
	}
	loopStarted = true
	if _, err := copyStreamWatched(ctx, out, in, buf, opt.NoProgress, opt.OnBytes, unblock, release); err != nil {
		// Not out.Abort(): it closes the partial on this goroutine, which waits
		// for a write that is still in flight. discard removes the partial after
		// the handles really close, and waits for that only briefly.
		handles.discard(copyDiscardWait)
		return err
	}
	in.Close()
	if err := out.Commit(info.ModTime(), opt.Replace); err != nil {
		if ctx.Err() != nil {
			return errRunStopped
		}
		return fmt.Errorf("cannot finish the target: %w", err)
	}
	// The read-only attribute is the one mode bit Windows keeps; it is set
	// after the rename, because a read-only partial could not be removed.
	if info.Mode().Perm()&0o200 == 0 {
		_ = os.Chmod(dst, info.Mode().Perm())
	}
	return nil
}

// copy loop states, for the handshake between a copy loop and its watchdog.
const (
	copyLoopRunning int32 = iota
	copyLoopFinished
	copyLoopAbandoned
)

// copyStreamWatched copies r to w on its own goroutine and returns when the
// copy ends, when ctx ends, or when no byte has moved for noProgress. unblock
// (may be nil) closes whatever the loop is blocked on; release (may be nil)
// is called by the loop when it no longer touches buf.
//
// A loop given up on after a stall is counted in abandonedCopies until it
// finally returns, which is the bound COPY-15 asks for.
func copyStreamWatched(ctx context.Context, w io.Writer, r io.Reader, buf []byte, noProgress time.Duration, onBytes func(int64), unblock func(), release func()) (int64, error) {
	var moved atomic.Int64
	var state atomic.Int32
	done := make(chan error, 1)

	go func() {
		err := copyLoop(ctx, w, r, buf, &moved, onBytes)
		if release != nil {
			release()
		}
		if !state.CompareAndSwap(copyLoopRunning, copyLoopFinished) {
			abandonedCopies.Add(-1)
		}
		done <- err
	}()

	// abandon gives the loop up. It reports false when the loop has in fact
	// just finished, in which case its result is on done.
	abandon := func() bool {
		if state.CompareAndSwap(copyLoopRunning, copyLoopAbandoned) {
			abandonedCopies.Add(1)
			return true
		}
		return false
	}
	// The close is never waited on: with a synchronous read pending it does not
	// return until the read does, and a device that never answers would hold the
	// watchdog and the stop for ever (AUD-01-F1).
	doUnblock := func() {
		if unblock != nil {
			go unblock()
		}
	}

	var tick <-chan time.Time
	if noProgress > 0 {
		period := noProgress / 4
		if period > 250*time.Millisecond {
			period = 250 * time.Millisecond
		}
		if period < 10*time.Millisecond {
			period = 10 * time.Millisecond
		}
		t := time.NewTicker(period)
		defer t.Stop()
		tick = t.C
	}
	last := int64(-1)
	lastMove := time.Now()

	for {
		select {
		case err := <-done:
			return moved.Load(), err
		case <-ctx.Done():
			select {
			case <-done:
				return moved.Load(), errRunStopped
			case <-time.After(copyStopGrace):
			}
			doUnblock()
			select {
			case <-done:
			case <-time.After(3 * copyStopGrace):
				abandon()
			}
			return moved.Load(), errRunStopped
		case <-tick:
			if m := moved.Load(); m != last {
				last = m
				lastMove = time.Now()
				continue
			}
			if time.Since(lastMove) <= noProgress {
				continue
			}
			if !abandon() {
				return moved.Load(), <-done
			}
			doUnblock()
			return moved.Load(), &copyStallError{after: noProgress}
		}
	}
}

// copyLoop is the read/write loop itself; it notices a stop between buffers.
func copyLoop(ctx context.Context, w io.Writer, r io.Reader, buf []byte, moved *atomic.Int64, onBytes func(int64)) error {
	if len(buf) == 0 {
		buf = make([]byte, minCopyBuffer)
	}
	zeroReads := 0
	for {
		if ctx.Err() != nil {
			return errRunStopped
		}
		n, rerr := r.Read(buf)
		if n > 0 {
			zeroReads = 0
			if _, werr := w.Write(buf[:n]); werr != nil {
				if ctx.Err() != nil {
					return errRunStopped
				}
				return fmt.Errorf("cannot write the target: %w", werr)
			}
			moved.Add(int64(n))
			if onBytes != nil {
				onBytes(int64(n))
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return errRunStopped
			}
			return fmt.Errorf("cannot read the source: %w", rerr)
		}
		if n == 0 {
			zeroReads++
			if zeroReads > 1000 {
				return fmt.Errorf("cannot read the source: %w", io.ErrNoProgress)
			}
		}
	}
}

// The copy buffers (COPY-08, COPY-09). A worker takes a buffer sized for the
// file in hand, never larger than the pool it comes from - a 256 MB request
// once sliced a 128 MB pool buffer past its capacity and killed the process -
// and never larger than the build's address space allows for every worker at
// once: a 386 build has 2 GB for everything, and sixteen 128 MB buffers are
// all of it.
var copyBufferPools = []struct {
	size int
	pool *sync.Pool
}{
	{256 << 10, newBufferPool(256 << 10)},
	{1 << 20, newBufferPool(1 << 20)},
	{16 << 20, newBufferPool(16 << 20)},
	{64 << 20, newBufferPool(64 << 20)},
	{128 << 20, newBufferPool(128 << 20)},
}

func newBufferPool(size int) *sync.Pool {
	return &sync.Pool{New: func() interface{} {
		b := make([]byte, size)
		return &b
	}}
}

// copyBufferCap is the largest buffer one worker may hold, for this build.
func copyBufferCap() int {
	if strconv.IntSize == 32 {
		return 16 << 20
	}
	return 128 << 20
}

// copyBufferBudget is what all workers of one run may hold together.
func copyBufferBudget() int64 {
	if strconv.IntSize == 32 {
		return 256 << 20
	}
	return 2 << 30
}

// copyBufferLimit is the per-worker buffer ceiling for a run with this many
// workers and this configured maximum.
func copyBufferLimit(workers, configured int) int {
	if workers < 1 {
		workers = 1
	}
	limit := int64(configured)
	if perWorker := copyBufferBudget() / int64(workers); limit > perWorker {
		limit = perWorker
	}
	if limit > int64(copyBufferCap()) {
		limit = int64(copyBufferCap())
	}
	if limit < minCopyBuffer {
		limit = minCopyBuffer
	}
	return int(limit)
}

// takeCopyBuffer returns a buffer of about size bytes - clamped to
// [minCopyBuffer, the build's cap] and to the capacity of the pooled buffer
// it is cut from - and the function that returns it to its pool.
func takeCopyBuffer(size int) ([]byte, func()) {
	if size < minCopyBuffer {
		size = minCopyBuffer
	}
	if size > copyBufferCap() {
		size = copyBufferCap()
	}
	for _, p := range copyBufferPools {
		if size <= p.size {
			ptr := p.pool.Get().(*[]byte)
			b := *ptr
			if size > cap(b) {
				size = cap(b)
			}
			pool := p.pool
			return b[:size], func() { pool.Put(ptr) }
		}
	}
	b := make([]byte, size)
	return b, func() {}
}
