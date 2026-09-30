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

// RunSavePolicy saves c by the policy until ctx ends; it returns at once for
// any profile but ram. Writes are never throttled: under sustained pressure a
// new save starts as soon as the last one ends, and when the waiting amount
// stays above twice the limit, logf says so once a minute. A failed save is
// logged and ends the loop - the container writes nothing more after one.
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
			logf("ram: save failed, nothing more is written to the file: %v", err)
			return
		}
		last = began
		logf("ram: saved %d MB in %d ms", s.DirtyBytes>>20, time.Since(began).Milliseconds())
	}
}
