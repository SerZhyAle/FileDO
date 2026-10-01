package vdisk

import (
	"context"
	"time"
)

// SavePolicy says when a mounted ram container is saved (SP-0004 Q6): once
// the oldest unsaved write is Every old, or as soon as DirtyLimit bytes are
// waiting, whichever comes first.
type SavePolicy struct {
	Every      time.Duration
	DirtyLimit int64
}

// DefaultSavePolicy is the plan's: 60 seconds or 256 MB.
var DefaultSavePolicy = SavePolicy{Every: 60 * time.Second, DirtyLimit: 256 << 20}

// savePolicyTick is how often the policy looks; a variable so tests run fast.
var savePolicyTick = 250 * time.Millisecond

// A failed save may succeed when the backing store becomes writable again.
// Keep the dirty data in RAM and retry at this pace without a tight error loop.
var savePolicyRetry = 5 * time.Second

// RunSavePolicy saves c by the policy until ctx ends; it returns at once for
// any profile but ram. Writes are never throttled: under sustained pressure a
// new save starts as soon as the last one ends, and when the waiting amount
// stays above twice the limit, logf says so once a minute. A failed save is
// retried after a delay while the dirty data stays in RAM, and logged once a
// minute (AUD-35-F5).
// The final save is the session end's (Close), not this loop's.
func RunSavePolicy(ctx context.Context, c *Container, p SavePolicy, logf func(string, ...interface{})) {
	if _, ok := c.RAMState(); !ok {
		return
	}
	if p.Every <= 0 {
		p.Every = DefaultSavePolicy.Every
	}
	if p.DirtyLimit <= 0 {
		p.DirtyLimit = DefaultSavePolicy.DirtyLimit
	}
	if logf == nil {
		logf = func(string, ...interface{}) {}
	}
	last := time.Now()
	var warned time.Time
	var retryAt, failWarned time.Time
	t := time.NewTicker(savePolicyTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s, _ := c.RAMState()
		if s.DirtyBytes == 0 {
			last = time.Now()
			retryAt = time.Time{}
			continue
		}
		if time.Now().Before(retryAt) {
			continue
		}
		if s.DirtyBytes >= 2*p.DirtyLimit && time.Since(warned) >= time.Minute {
			warned = time.Now()
			logf("ram: %d MB waiting to be saved, more than twice the %d MB limit: the volume is written faster than its file", s.DirtyBytes>>20, p.DirtyLimit>>20)
		}
		if s.DirtyBytes < p.DirtyLimit && time.Since(last) < p.Every {
			continue
		}
		began := time.Now()
		if err := c.Save(); err != nil {
			// Retried every savePolicyRetry, logged once a minute (AUD-35-F5).
			if time.Since(failWarned) >= time.Minute {
				failWarned = time.Now()
				logf("ram: save failed; retrying in %s: %v", savePolicyRetry, err)
			}
			retryAt = time.Now().Add(savePolicyRetry)
			continue
		}
		retryAt, failWarned = time.Time{}, time.Time{}
		last = began
		logf("ram: saved %d MB in %d ms", s.DirtyBytes>>20, time.Since(began).Milliseconds())
	}
}
