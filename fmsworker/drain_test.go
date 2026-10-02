package fmsworker

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type drainFixture struct {
	count    atomic.Int32
	fail     bool
	forced   atomic.Bool
	forceErr error
	onForce  func()
}

func (b *drainFixture) OpenHandles(context.Context, string) (int, error) {
	if b.fail {
		return 0, errors.New("query failed")
	}
	return int(b.count.Load()), nil
}
func (b *drainFixture) ForceClose(context.Context, string) error {
	if b.forceErr != nil {
		return b.forceErr
	}
	b.forced.Store(true)
	b.count.Store(0)
	if b.onForce != nil {
		b.onForce()
	}
	return nil
}

// fastManager shrinks one bound second to 10 ms, so a bound of 1..5 elapses
// without real waiting; the public 1..90 limits are unchanged.
func fastManager(b DrainBackend) *DrainManager {
	m := NewDrainManager(b)
	m.boundUnit = 10 * time.Millisecond
	m.SetCheckInterval(time.Millisecond)
	return m
}
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// scriptBackend answers each query with the next value the test sends and honours ctx.
type scriptBackend struct {
	steps  chan int
	forced atomic.Bool
}

func (b *scriptBackend) OpenHandles(ctx context.Context, _ string) (int, error) {
	select {
	case v := <-b.steps:
		return v, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
func (b *scriptBackend) ForceClose(context.Context, string) error { b.forced.Store(true); return nil }

func TestDrainStartInitialState(t *testing.T) {
	b := &scriptBackend{steps: make(chan int)}
	m := NewDrainManager(b)
	s, e := m.StartDrain("one", 3, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer m.CleanupDrain("one")
	if s.ContainerPath != "one" || s.TargetHandles != 3 || s.CurrentHandles != 3 || s.BoundSeconds != 30 || !s.Active || s.Completed || s.Cancelled || s.TimedOut || s.Forced || s.Error != "" || s.StartedAt.IsZero() {
		t.Fatalf("initial state %+v", s)
	}
}
func TestDrainCompletesOnlyWhenHandlesClose(t *testing.T) {
	b := &drainFixture{}
	b.count.Store(2)
	m := NewDrainManager(b)
	m.SetCheckInterval(time.Millisecond)
	if _, e := m.StartDrain("one", 2, 1); e != nil {
		t.Fatal(e)
	}
	if _, e := m.StartDrain("one", 2, 1); e == nil {
		t.Fatal("duplicate drain accepted")
	}
	if m.WaitForDrain("one", 10*time.Millisecond) {
		t.Fatal("open handles were ignored")
	}
	b.count.Store(0)
	if !m.WaitForDrain("one", time.Second) {
		t.Fatal("closed handles did not finish drain")
	}
	s, _ := m.GetDrainStatus("one")
	if !s.Completed || s.Forced || s.TimedOut || s.Active {
		t.Fatalf("clean drain status %+v", s)
	}
	s.CurrentHandles = 99
	again, _ := m.GetDrainStatus("one")
	if again.CurrentHandles != 0 {
		t.Fatal("snapshot mutated drain")
	}
	m.CleanupDrain("one")
}

// 04-05-03/04/05/08: the handle count is reported as it moves, may rise, ends
// the drain at zero, and a negative count is a failed query, never a drain.
func TestDrainProgressSequence(t *testing.T) {
	b := &scriptBackend{steps: make(chan int)}
	m := NewDrainManager(b)
	m.SetCheckInterval(time.Millisecond)
	if _, e := m.StartDrain("d", 1, 30); e != nil {
		t.Fatal(e)
	}
	defer m.CleanupDrain("d")
	for _, n := range []int{3, 5, 2, 1} {
		b.steps <- n
		waitFor(t, "progress", func() bool { s, _ := m.GetDrainStatus("d"); return s.CurrentHandles == n })
		if s, _ := m.GetDrainStatus("d"); !s.Active || s.Completed {
			t.Fatalf("drain ended at %d handles: %+v", n, s)
		}
	}
	b.steps <- 0
	if !m.WaitForDrain("d", 5*time.Second) {
		t.Fatal("zero handles did not complete the drain")
	}
	if s, _ := m.GetDrainStatus("d"); s.CurrentHandles != 0 || !s.Completed || s.Active {
		t.Fatalf("final status %+v", s)
	}
}
func TestDrainNegativeCountIsAFailedQuery(t *testing.T) {
	b := &scriptBackend{steps: make(chan int)}
	m := NewDrainManager(b)
	if _, e := m.StartDrain("d", 1, 30); e != nil {
		t.Fatal(e)
	}
	defer m.CleanupDrain("d")
	b.steps <- -1
	if m.WaitForDrain("d", 5*time.Second) {
		t.Fatal("negative count completed the drain")
	}
	if s, _ := m.GetDrainStatus("d"); s.Completed || s.Active || s.Error == "" || s.TimedOut {
		t.Fatalf("status %+v", s)
	}
}
func TestDrainTimeoutIsNotConsentToForce(t *testing.T) {
	for _, force := range []bool{false, true} {
		b := &drainFixture{}
		b.count.Store(1)
		m := fastManager(b)
		r := m.ExecuteDrain("disk", force, 1)
		if !r.TimedOut || r.Forced != force || r.ForceUsed != force || b.forced.Load() != force {
			t.Fatalf("force=%t result=%+v", force, r)
		}
		// A forced close is never a clean drain.
		if r.Success {
			t.Fatalf("force=%t reported Success: %+v", force, r)
		}
		if force && (r.ForcedHandles != 1 || !strings.Contains(r.Message, "not a clean drain")) {
			t.Fatalf("forced result %+v", r)
		}
		if !force && (r.ForcedHandles != 0 || r.DrainedHandles != 0 || !strings.Contains(r.Message, "1 handles remain")) {
			t.Fatalf("unforced result %+v", r)
		}
	}
}
func TestForceDrainStatusIsNotCompleted(t *testing.T) {
	b := &drainFixture{}
	b.count.Store(4)
	m := fastManager(b)
	if _, e := m.StartDrain("d", 4, 1); e != nil {
		t.Fatal(e)
	}
	waitFor(t, "timeout", func() bool { s, _ := m.GetDrainStatus("d"); return s.TimedOut })
	if e := m.ForceDrain("d"); e != nil {
		t.Fatal(e)
	}
	s, _ := m.GetDrainStatus("d")
	if !s.Forced || !s.ForceRequested || s.ForcedHandles != 4 || s.Completed || s.Active {
		t.Fatalf("forced status %+v", s)
	}
	if m.WaitForDrain("d", time.Second) {
		t.Fatal("forced drain reported clean")
	}
	// A failing backend leaves the status unforced.
	b2 := &drainFixture{forceErr: errors.New("busy")}
	b2.count.Store(1)
	m2 := fastManager(b2)
	m2.StartDrain("d", 1, 1)
	waitFor(t, "timeout", func() bool { s, _ := m2.GetDrainStatus("d"); return s.TimedOut })
	if e := m2.ForceDrain("d"); e == nil {
		t.Fatal("force failure hidden")
	}
	if s, _ := m2.GetDrainStatus("d"); s.Forced || s.ForceRequested {
		t.Fatalf("failed force recorded: %+v", s)
	}
}
func TestForceDrainRefusesFinishedRuns(t *testing.T) {
	b := &drainFixture{}
	m := fastManager(b)
	m.StartDrain("clean", 0, 1)
	if !m.WaitForDrain("clean", time.Second) {
		t.Fatal("empty disk did not drain")
	}
	if e := m.ForceDrain("clean"); e == nil || b.forced.Load() {
		t.Fatalf("force after a clean drain: %v", e)
	}
	b.count.Store(1)
	m.StartDrain("cancelled", 1, 5)
	m.CancelDrain("cancelled")
	if e := m.ForceDrain("cancelled"); e == nil || b.forced.Load() {
		t.Fatalf("force overrode a cancel: %v", e)
	}
	if e := m.ForceDrain("unknown"); e == nil {
		t.Fatal("force without a drain")
	}
}
func TestDrainQueryFailureDoesNotBecomeZero(t *testing.T) {
	b := &drainFixture{fail: true}
	m := fastManager(b)
	r := m.ExecuteDrain("disk", true, 1)
	if r.Success || r.Message == "" || r.Forced || b.forced.Load() {
		t.Fatalf("failed query reported drained or forced: %+v", r)
	}
	// The initial query works but the monitor's query fails: still no force.
	b2 := &queryFailsLater{}
	m2 := fastManager(b2)
	r = m2.ExecuteDrain("disk", true, 1)
	if r.Success || r.Forced || r.TimedOut || r.Message != "could not query open handles" || b2.forced.Load() {
		t.Fatalf("monitor query failure: %+v", r)
	}
}

type queryFailsLater struct {
	calls  atomic.Int32
	forced atomic.Bool
}

func (b *queryFailsLater) OpenHandles(context.Context, string) (int, error) {
	if b.calls.Add(1) == 1 {
		return 2, nil
	}
	return 0, errors.New("query failed")
}
func (b *queryFailsLater) ForceClose(context.Context, string) error { b.forced.Store(true); return nil }

// ctxBackend answers its first query, blocks the second until ctx ends (as a
// real holder query would), and answers the rest at once.
type ctxBackend struct {
	calls  atomic.Int32
	forced atomic.Bool
}

func (b *ctxBackend) OpenHandles(ctx context.Context, _ string) (int, error) {
	if b.calls.Add(1) == 2 {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return 1, nil
}
func (b *ctxBackend) ForceClose(context.Context, string) error { b.forced.Store(true); return nil }

// The bound expiring while a query is in flight is a timeout, not a query
// failure, so a requested force is not skipped.
func TestDrainBoundExpiringDuringQueryIsATimeout(t *testing.T) {
	b := &ctxBackend{}
	r := fastManager(b).ExecuteDrain("disk", true, 1)
	if !r.TimedOut || !r.Forced || r.ForcedHandles != 1 || r.Success || !b.forced.Load() {
		t.Fatalf("force skipped after an in-flight timeout: %+v", r)
	}
	if strings.Contains(r.Message, "could not query") {
		t.Fatalf("timeout reported as query failure: %+v", r)
	}
	b = &ctxBackend{}
	r = fastManager(b).ExecuteDrain("disk", false, 1)
	if !r.TimedOut || r.Forced || r.Success || b.forced.Load() || strings.Contains(r.Message, "could not query") {
		t.Fatalf("unforced in-flight timeout: %+v", r)
	}
}
func TestDrainBoundValidation(t *testing.T) {
	b := &drainFixture{}
	b.count.Store(1)
	m := fastManager(b)
	for _, bad := range []int{-1, -30, 91, 1000} {
		if _, e := m.StartDrain("d", 1, bad); e == nil {
			t.Fatalf("bound %d accepted", bad)
		}
		if r := m.ExecuteDrain("d", true, bad); r.Success || r.Forced || r.Message == "" || b.forced.Load() {
			t.Fatalf("bound %d executed: %+v", bad, r)
		}
		if _, ok := m.GetDrainStatus("d"); ok {
			t.Fatalf("bound %d left a drain behind", bad)
		}
	}
	for _, good := range []int{1, 30, 90} {
		s, e := m.StartDrain("d", 1, good)
		if e != nil || s.BoundSeconds != good {
			t.Fatalf("bound %d: %v %+v", good, e, s)
		}
		m.CleanupDrain("d")
	}
	s, e := m.StartDrain("d", 1, 0)
	if e != nil || s.BoundSeconds != 30 {
		t.Fatalf("unset bound: %v %+v", e, s)
	}
	m.CleanupDrain("d")

	for _, bad := range []int{0, -1, 91} {
		if e := m.SetBoundSeconds(bad); e == nil {
			t.Fatalf("default bound %d accepted", bad)
		}
	}
	if s, _ = m.StartDrain("d", 1, 0); s.BoundSeconds != 30 {
		t.Fatalf("rejected default changed the bound: %+v", s)
	}
	m.CleanupDrain("d")
	for _, good := range []int{1, 90, 7} {
		if e := m.SetBoundSeconds(good); e != nil {
			t.Fatalf("default bound %d: %v", good, e)
		}
	}
	if s, _ = m.StartDrain("d", 1, 0); s.BoundSeconds != 7 {
		t.Fatalf("default not applied: %+v", s)
	}
	m.CleanupDrain("d")
}
func TestDrainCancellationAndConcurrentContainers(t *testing.T) {
	b := &drainFixture{}
	b.count.Store(1)
	m := NewDrainManager(b)
	m.SetCheckInterval(time.Millisecond)
	for _, p := range []string{"one", "two"} {
		if _, e := m.StartDrain(p, 1, 1); e != nil {
			t.Fatal(e)
		}
	}
	if e := m.CancelDrain("one"); e != nil {
		t.Fatal(e)
	}
	if m.WaitForDrain("one", time.Second) {
		t.Fatal("cancel was success")
	}
	if s, _ := m.GetDrainStatus("one"); !s.Cancelled || s.Completed || s.Active {
		t.Fatalf("cancelled status %+v", s)
	}
	b.count.Store(0)
	if !m.WaitForDrain("two", time.Second) {
		t.Fatal("one cancellation affected another disk")
	}
	m.CleanupDrain("one")
	m.CleanupDrain("two")
	if e := m.CancelDrain("one"); e == nil {
		t.Fatal("cancel of a cleaned-up drain succeeded")
	}
}

// 04-05-07: a drain that timed out without force can be started again with a
// longer bound and then completes.
func TestDrainTimeoutThenExtendedDrain(t *testing.T) {
	b := &drainFixture{}
	b.count.Store(2)
	m := fastManager(b)
	if _, e := m.StartDrain("d", 2, 1); e != nil {
		t.Fatal(e)
	}
	if m.WaitForDrain("d", time.Second) {
		t.Fatal("open handles drained")
	}
	if s, _ := m.GetDrainStatus("d"); !s.TimedOut || s.Active || s.Completed || s.Forced {
		t.Fatalf("timed-out status %+v", s)
	}
	if b.forced.Load() {
		t.Fatal("timeout forced the close")
	}
	s, e := m.StartDrain("d", 2, 5)
	if e != nil || s.BoundSeconds != 5 || s.TimedOut {
		t.Fatalf("extended drain: %v %+v", e, s)
	}
	b.count.Store(0)
	if !m.WaitForDrain("d", time.Second) {
		t.Fatal("extended drain did not complete")
	}
	if s, _ = m.GetDrainStatus("d"); !s.Completed || s.TimedOut || s.Forced {
		t.Fatalf("extended status %+v", s)
	}
}

// A concurrent CleanupDrain removes the state ExecuteDrain is about to read.
func TestExecuteDrainSurvivesConcurrentCleanup(t *testing.T) {
	for _, force := range []bool{false, true} {
		b := &drainFixture{}
		b.count.Store(1)
		m := fastManager(b)
		b.onForce = func() { m.CleanupDrain("disk") }
		done := make(chan DrainResult, 1)
		go func() { done <- m.ExecuteDrain("disk", force, 1) }()
		if !force {
			waitFor(t, "drain start", func() bool { _, ok := m.GetDrainStatus("disk"); return ok })
			m.CleanupDrain("disk")
		}
		select {
		case r := <-done:
			if r.Success || r.Message == "" {
				t.Fatalf("force=%t result %+v", force, r)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("force=%t ExecuteDrain hung", force)
		}
	}
}
func TestDrainNeedsBackendAndRejectsNegativeCounts(t *testing.T) {
	if _, e := NewDrainManager(nil).StartDrain("a", 0, 1); e == nil {
		t.Fatal("fake backend accepted")
	}
	if _, e := NewDrainManager(&drainFixture{}).StartDrain("a", -1, 1); e == nil {
		t.Fatal("negative handles accepted")
	}
	// The package-level manager has no backend: it cannot invent a drain.
	if _, e := GetDrainManager().StartDrain("a", 0, 1); e == nil {
		t.Fatal("global manager drained without a backend")
	}
	if r := GetDrainManager().ExecuteDrain("a", true, 1); r.Success || r.Message == "" {
		t.Fatalf("global ExecuteDrain: %+v", r)
	}
}
func TestDrainBehaviorContract(t *testing.T) {
	b := GetDrainBehavior()
	if !b.RefuseNewOpens || !b.AllowExisting || b.ErrorForNewOpens != "No such file or directory" || b.StatusMessage != "Closing" {
		t.Fatalf("behavior %+v", b)
	}
}
