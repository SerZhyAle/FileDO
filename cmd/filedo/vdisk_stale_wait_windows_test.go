//go:build windows

package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// A program that holds the disk device open - a monitor such as Task Manager - makes Windows refuse the
// session's logout after a clean dismount, and keeps refusing it until Windows gives the dead session up
// by itself, about a minute after its connection ends. FileDO must never fail or stall for it: the
// unmount stops retrying after a few seconds, and a mount that meets the leftover session waits for it
// to go instead of refusing.

var errVetoed = &iscsiError{call: "LogoutIScsiTarget", status: 0xEFFF0040}

// The logout after a clean dismount is retried inside a budget, not five times: with Task Manager open
// one call alone takes seconds, and the retries used to turn every unmount into a 37-second wait.
func TestVDLogoutWithin_StopsAtTheBudget(t *testing.T) {
	var calls int32
	slow := func(iscsiSessionID) error { // one refused call, taking as long as the PnP veto does
		atomic.AddInt32(&calls, 1)
		time.Sleep(60 * time.Millisecond)
		return errVetoed
	}
	start := time.Now()
	err := vdLogoutWithinFn(slow, iscsiSessionID{}, 80*time.Millisecond, 40*time.Millisecond)
	if err == nil {
		t.Fatal("a session that keeps refusing was reported logged out")
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("%d logout calls; one slow refusal already used the budget, so no second call is made", n)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("took %v for a budget of 80ms", d)
	}
}

func TestVDLogoutWithin_RetriesWhatSettlesQuickly(t *testing.T) {
	var calls int32
	settles := func(iscsiSessionID) error { // a write that has not settled: refused twice, then fine
		if atomic.AddInt32(&calls, 1) < 3 {
			return errVetoed
		}
		return nil
	}
	if err := vdLogoutWithinFn(settles, iscsiSessionID{}, time.Second, 10*time.Millisecond); err != nil {
		t.Fatalf("a logout that settles after two refusals failed: %v", err)
	}
	if n := atomic.LoadInt32(&calls); n != 3 {
		t.Errorf("%d calls, want 3", n)
	}
}

func TestVDLogoutWithin_AnAlreadyGoneSessionIsLoggedOut(t *testing.T) {
	gone := func(iscsiSessionID) error {
		return &iscsiError{call: "LogoutIScsiTarget", status: isdscInvalidSessionID}
	}
	if err := vdLogoutWithinFn(gone, iscsiSessionID{}, time.Second, time.Millisecond); err != nil {
		t.Fatalf("a session Windows no longer knows is logged out, got %v", err)
	}
}

// The wait for a leftover session ends as soon as the session is gone, however it went.
func TestVDPollStale_EndsWhenTheSessionGoes(t *testing.T) {
	var polls int32
	gone := func() bool { return atomic.AddInt32(&polls, 1) >= 4 }
	never := func() bool { t.Error("the logout was tried before its turn"); return false }
	ok, err := vdPollStale(2*time.Second, time.Millisecond, time.Hour, gone, never, func() error { return nil })
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v, want the wait to end with the session gone", ok, err)
	}
}

func TestVDPollStale_EndsWhenTheLogoutSucceeds(t *testing.T) {
	var tries int32
	logout := func() bool { return atomic.AddInt32(&tries, 1) >= 2 } // the holder let go
	ok, err := vdPollStale(2*time.Second, time.Millisecond, 5*time.Millisecond, func() bool { return false }, logout, func() error { return nil })
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v, want the wait to end with the logout done", ok, err)
	}
	if n := atomic.LoadInt32(&tries); n != 2 {
		t.Errorf("%d logout tries, want 2", n)
	}
}

// A holder that never lets go ends the wait at the limit - the only case in which a mount reports busy.
func TestVDPollStale_GivesUpAtTheLimit(t *testing.T) {
	start := time.Now()
	ok, err := vdPollStale(60*time.Millisecond, time.Millisecond, 10*time.Millisecond, func() bool { return false }, func() bool { return false }, func() error { return nil })
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want a wait that ran out", ok, err)
	}
	if d := time.Since(start); d < 50*time.Millisecond || d > time.Second {
		t.Errorf("waited %v for a limit of 60ms", d)
	}
}

// A cancelled mount leaves the wait at once, with the cancellation.
func TestVDPollStale_ACancelledMountLeaves(t *testing.T) {
	stop := errors.New("the mount was interrupted")
	_, err := vdPollStale(time.Minute, time.Millisecond, time.Hour, func() bool { return false }, func() bool { return false }, func() error { return stop })
	if !errors.Is(err, stop) {
		t.Fatalf("err = %v, want the cancellation", err)
	}
}
