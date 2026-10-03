// The authoritative drain runs in the worker (fms_companion, diskshare
// Controller.closeLocked): it gates new opens, counts live handles and detaches
// atomically. DrainManager in this package is the client-side helper and
// contract model: it validates the drain bound and defines the result
// semantics (a clean drain, a timeout, a forced close) against a DrainBackend
// the holder supplies. It is not a handle counter and the global instance has no
// backend, so nothing here observes real handles on its own.
package fmsworker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Drain bound limits in seconds; they match the worker (0 there selects its default).
const (
	MinDrainBoundSeconds     = 1
	MaxDrainBoundSeconds     = 90
	DefaultDrainBoundSeconds = 30
)

func validDrainBound(n int) error {
	if n < MinDrainBoundSeconds || n > MaxDrainBoundSeconds {
		return fmt.Errorf("drain bound must be %d..%d seconds, got %d", MinDrainBoundSeconds, MaxDrainBoundSeconds, n)
	}
	return nil
}

// DrainBackend is supplied by the holder. Query failures never mean zero handles.
type DrainBackend interface {
	OpenHandles(context.Context, string) (int, error)
	ForceClose(context.Context, string) error
}
type DrainStatus struct {
	ContainerPath  string
	TargetHandles  int
	CurrentHandles int
	StartedAt      time.Time
	BoundSeconds   int
	Active         bool
	Cancelled      bool
	// Completed is true only for a clean drain: the handles reached zero by
	// themselves. A forced close never sets it.
	Completed      bool
	ForceRequested bool
	// Forced is true when the handles were closed by force; ForcedHandles is how
	// many were open at that moment. CurrentHandles stays the last count seen
	// before the force.
	Forced        bool
	ForcedHandles int
	TimedOut      bool
	Error         string
}
type drainRun struct {
	status DrainStatus
	cancel context.CancelFunc
	done   chan struct{}
}
type DrainManager struct {
	mu            sync.Mutex
	drains        map[string]*drainRun
	boundSeconds  int
	checkInterval time.Duration
	// boundUnit is the length of one bound second; tests shrink it.
	boundUnit time.Duration
	backend   DrainBackend
}

func NewDrainManager(backend DrainBackend) *DrainManager {
	return &DrainManager{drains: map[string]*drainRun{}, boundSeconds: DefaultDrainBoundSeconds, checkInterval: time.Second, boundUnit: time.Second, backend: backend}
}

var globalDrainManager = NewDrainManager(nil)

func GetDrainManager() *DrainManager { return globalDrainManager }

// resolveBoundLocked turns a caller bound into seconds: 0 is "unset" and takes
// the manager default; anything outside 1..90 is an error, never clamped.
func (dm *DrainManager) resolveBoundLocked(bound int) (int, error) {
	if bound == 0 {
		return dm.boundSeconds, nil
	}
	if e := validDrainBound(bound); e != nil {
		return 0, e
	}
	return bound, nil
}
func (dm *DrainManager) StartDrain(path string, initial, bound int) (*DrainStatus, error) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	if dm.backend == nil {
		return nil, errors.New("drain requires a holder backend")
	}
	if initial < 0 {
		return nil, errors.New("invalid open-handle count")
	}
	bound, e := dm.resolveBoundLocked(bound)
	if e != nil {
		return nil, e
	}
	if old := dm.drains[path]; old != nil && old.status.Active {
		return nil, errors.New("already draining")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(bound)*dm.boundUnit)
	run := &drainRun{status: DrainStatus{ContainerPath: path, TargetHandles: initial, CurrentHandles: initial, StartedAt: time.Now(), BoundSeconds: bound, Active: true}, cancel: cancel, done: make(chan struct{})}
	dm.drains[path] = run
	interval := dm.checkInterval
	go dm.monitor(ctx, run, interval)
	copy := run.status
	return &copy, nil
}
func (dm *DrainManager) monitor(ctx context.Context, run *drainRun, interval time.Duration) {
	defer close(run.done)
	defer run.cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		dm.mu.Lock()
		active := run.status.Active
		dm.mu.Unlock()
		if !active {
			return
		}
		n, e := dm.backend.OpenHandles(ctx, run.status.ContainerPath)
		dm.mu.Lock()
		if !run.status.Active {
			dm.mu.Unlock()
			return
		}
		if e != nil || n < 0 {
			run.status.Active = false
			if ctx.Err() != nil {
				// The bound ran out while the query was in flight: that is a
				// timeout, not a failed query, so a requested force still applies.
				run.status.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
			} else {
				run.status.Error = "could not query open handles"
			}
			dm.mu.Unlock()
			return
		}
		run.status.CurrentHandles = n
		if n == 0 {
			run.status.Completed = true
			run.status.Active = false
			dm.mu.Unlock()
			return
		}
		dm.mu.Unlock()
		select {
		case <-ticker.C:
		case <-ctx.Done():
			dm.mu.Lock()
			if run.status.Active {
				run.status.Active = false
				run.status.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
			}
			dm.mu.Unlock()
			return
		}
	}
}
func (dm *DrainManager) CancelDrain(path string) error {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	r := dm.drains[path]
	if r == nil {
		return errors.New("no drain in progress")
	}
	r.status.Cancelled = true
	r.status.Active = false
	r.cancel()
	return nil
}

// ForceDrain closes the remaining handles through the backend. The run is
// finished as Forced, never as Completed, so it cannot pass for a clean drain.
func (dm *DrainManager) ForceDrain(path string) error {
	dm.mu.Lock()
	r := dm.drains[path]
	backend := dm.backend
	seen := 0
	if r != nil {
		seen = r.status.CurrentHandles
		if r.status.Completed || r.status.Cancelled {
			dm.mu.Unlock()
			return errors.New("drain already finished; nothing to force")
		}
	}
	dm.mu.Unlock()
	if r == nil || backend == nil {
		return errors.New("no drain backend")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if n, e := backend.OpenHandles(ctx, path); e == nil && n >= 0 {
		seen = n
	}
	if e := backend.ForceClose(ctx, path); e != nil {
		return e
	}
	dm.mu.Lock()
	r.status.ForceRequested = true
	r.status.Forced = true
	r.status.ForcedHandles = seen
	r.status.Active = false
	r.cancel()
	dm.mu.Unlock()
	return nil
}
func (dm *DrainManager) GetDrainStatus(path string) (*DrainStatus, bool) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	r := dm.drains[path]
	if r == nil {
		return nil, false
	}
	copy := r.status
	return &copy, true
}
func (dm *DrainManager) CleanupDrain(path string) {
	dm.mu.Lock()
	r := dm.drains[path]
	if r != nil {
		r.cancel()
		r.status.Active = false
		delete(dm.drains, path)
	}
	dm.mu.Unlock()
}

// SetBoundSeconds sets the default bound, 1..90; anything else is rejected.
func (dm *DrainManager) SetBoundSeconds(n int) error {
	if e := validDrainBound(n); e != nil {
		return e
	}
	dm.mu.Lock()
	dm.boundSeconds = n
	dm.mu.Unlock()
	return nil
}
func (dm *DrainManager) SetCheckInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	dm.mu.Lock()
	dm.checkInterval = d
	dm.mu.Unlock()
}

// WaitForDrain reports whether the drain ended clean. A timeout, cancel or
// forced close is false.
func (dm *DrainManager) WaitForDrain(path string, timeout time.Duration) bool {
	dm.mu.Lock()
	r := dm.drains[path]
	dm.mu.Unlock()
	if r == nil {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-r.done:
		s, _ := dm.GetDrainStatus(path)
		return s != nil && s.Completed
	case <-timer.C:
		return false
	}
}

// DrainResult: Success is true only for a clean drain, so a forced close
// (Forced, ForcedHandles) is Success=false and must not set a clean marker.
type DrainResult struct {
	Success bool
	// DrainedHandles were closed by their clients; ForcedHandles by force.
	DrainedHandles int
	ForcedHandles  int
	// Forced is the explicit forced-close flag; ForceUsed is the same value.
	Forced    bool
	ForceUsed bool
	TimedOut  bool
	Duration  time.Duration
	Message   string
}

func (dm *DrainManager) ExecuteDrain(path string, force bool, bound int) DrainResult {
	start := time.Now()
	result := DrainResult{}
	dm.mu.Lock()
	backend := dm.backend
	bound, e := dm.resolveBoundLocked(bound)
	unit := dm.boundUnit
	dm.mu.Unlock()
	if backend == nil {
		result.Message = "drain requires a holder backend"
		return result
	}
	if e != nil {
		result.Message = e.Error()
		return result
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	n, e := backend.OpenHandles(ctx, path)
	cancel()
	if e != nil || n < 0 {
		result.Message = "could not query open handles"
		return result
	}
	if _, e = dm.StartDrain(path, n, bound); e != nil {
		result.Message = e.Error()
		return result
	}
	// The waiter outlives the bound by a real margin: with a bound second shrunk for a test, one unit
	// is below the timer tick, and a loaded machine let the waiter give up before the monitor had set
	// TimedOut (AUD-92-F1). With the production 1 s unit the margin is the same one second.
	margin := unit
	if margin < 250*time.Millisecond {
		margin = 250 * time.Millisecond
	}
	dm.WaitForDrain(path, time.Duration(bound)*unit+margin)
	status, _ := dm.GetDrainStatus(path)
	if status != nil && !status.Completed && status.TimedOut && force {
		e = dm.ForceDrain(path)
		if e != nil {
			result.Message = e.Error()
		}
		status, _ = dm.GetDrainStatus(path)
	}
	remaining := 0
	if status != nil {
		result.Success = status.Completed
		result.Forced = status.Forced
		result.ForceUsed = status.Forced
		result.ForcedHandles = status.ForcedHandles
		result.TimedOut = status.TimedOut
		remaining = status.CurrentHandles
		result.DrainedHandles = n - remaining
		if result.DrainedHandles < 0 {
			result.DrainedHandles = 0
		}
		if result.Message == "" {
			result.Message = status.Error
		}
	}
	dm.CleanupDrain(path)
	result.Duration = time.Since(start)
	if !result.Success && result.Message == "" {
		switch {
		case status == nil:
			result.Message = "drain state was cleaned up concurrently"
		case result.Forced:
			result.Message = fmt.Sprintf("forced close of %d open handles; not a clean drain", result.ForcedHandles)
		default:
			result.Message = fmt.Sprintf("drain incomplete; %d handles remain", remaining)
		}
	}
	return result
}

type DrainBehavior struct {
	RefuseNewOpens   bool
	ErrorForNewOpens string
	AllowExisting    bool
	StatusMessage    string
}

func GetDrainBehavior() DrainBehavior {
	return DrainBehavior{true, "No such file or directory", true, "Closing"}
}

// The holder gates new SFTP opens and performs drain and detach atomically.
// A client-side polling loop would leave a race for new opens.
func HandleDrainForClose(client *FMSWorkerClient, path string, force bool, bound int) error {
	return client.CloseDiskBound(path, force, bound)
}
