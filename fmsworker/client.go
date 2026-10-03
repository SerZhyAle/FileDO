//go:build windows

// Package fmsworker implements the WORKER-IPC disk-control client.
package fmsworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	winio "github.com/Microsoft/go-winio"
)

var (
	ErrWorkerUnavailable = errors.New("FMS worker is not available")
	ErrNotCapable        = errors.New("FMS worker does not support disk sharing")
	ErrDiskNotShared     = errors.New("disk is not shared")
	ErrDiskNotFound      = errors.New("disk not found")
	ErrAlreadyShared     = errors.New("disk is already shared")
	ErrRootNameClash     = errors.New("root name already exists")
	ErrCommunication     = errors.New("communication error with FMS worker")
)

type WorkerMode int

const (
	WorkerModeUnknown WorkerMode = iota
	WorkerModeUser
	WorkerModeService
)

type DiskState int

const (
	DiskStateUnknown DiskState = iota
	DiskStateClosed
	DiskStateOpening
	DiskStateOpen
	DiskStateClosing
	DiskStateFailed
	DiskStateLocked
)

// DiskStateUnshared is the legacy spelling for an absent share.
const DiskStateUnshared = DiskStateUnknown

func (s DiskState) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }
func (s DiskState) String() string {
	names := []string{"unknown", "closed", "opening", "open", "closing", "failed", "locked"}
	if int(s) < 0 || int(s) >= len(names) {
		return "unknown"
	}
	return names[s]
}
func (s *DiskState) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return err
	}
	for i := DiskStateUnknown; i <= DiskStateLocked; i++ {
		if i.String() == name {
			*s = i
			return nil
		}
	}
	return fmt.Errorf("unknown disk state")
}

type SharedDiskInfo struct {
	ContainerID   string    `json:"containerId"`
	ContainerPath string    `json:"containerPath"`
	RootName      string    `json:"rootName"`
	State         DiskState `json:"state"`
	Holder        string    `json:"holder"`
	ReadOnly      bool      `json:"readOnly"`
	Autostart     bool      `json:"autostart"`
	HasStoredKey  bool      `json:"hasStoredKey"`
	Encrypted     bool      `json:"encrypted"`
	MountPath     string    `json:"mountPath,omitempty"`
	OpenHandles   int       `json:"openHandles"`
	ErrorMessage  string    `json:"errorMessage,omitempty"`
	SharedAt      time.Time `json:"sharedAt"`
	LastOpened    time.Time `json:"lastOpened,omitempty"`
	LastClosed    time.Time `json:"lastClosed,omitempty"`
}
type Root struct {
	Name     string `json:"name"`
	HostPath string `json:"hostPath"`
	ReadOnly bool   `json:"readOnly"`
	Owner    string `json:"owner,omitempty"`
}
type WorkerStatus struct {
	PID          int             `json:"pid,omitempty"`
	Version      int             `json:"version"`
	Mode         string          `json:"mode"`
	Executable   string          `json:"executable"`
	Capabilities map[string]bool `json:"capabilities"`
	Roots        []Root          `json:"roots"`
}

// WorkerError is an ok=false answer from the worker. OutcomeClass is the
// worker's classification of the failure (0 when it sent none).
type WorkerError struct {
	Message      string
	OutcomeClass int
	cause        error
}

func (e *WorkerError) Error() string { return e.Message }
func (e *WorkerError) Unwrap() error { return e.cause }

type wireResponse struct {
	SchemaVersion int              `json:"schemaVersion"`
	OK            bool             `json:"ok"`
	OutcomeClass  int              `json:"outcomeClass,omitempty"`
	Error         string           `json:"error,omitempty"`
	Status        *WorkerStatus    `json:"status,omitempty"`
	Disk          *SharedDiskInfo  `json:"disk,omitempty"`
	Disks         []SharedDiskInfo `json:"disks,omitempty"`
}

// Dial is replaceable per client for isolated exchange tests, never globally.
type Dial func(context.Context, string) (net.Conn, error)
type FMSWorkerClient struct {
	mu         sync.Mutex
	pipePath   string
	mode       WorkerMode
	capable    bool
	capability map[string]bool
	dial       Dial
	retryDelay time.Duration
	// dialTimeout bounds one connect attempt: a busy pipe makes DialPipeContext
	// wait for as long as its context lives.
	dialTimeout time.Duration
	// attempts is how many times one request dials a pipe that is not there (0: the default of 3).
	attempts int
	// verify checks the pipe's server before the first byte of a request is written; nil only for
	// the clients the tests build around a pipe of their own.
	verify ServerVerifier
}

const (
	defaultPipe        = `\\.\pipe\fms-companion`
	defaultDialTimeout = 5 * time.Second
)

// NewFMSWorkerClient is the production client: the worker's own pipe, and every connection is tied to
// the FMS worker before anything is written to it (DISK-SHARE 18).
func NewFMSWorkerClient() *FMSWorkerClient {
	c := NewClient(defaultPipe, winio.DialPipeContext)
	c.verify = verifyPipeServer
	return c
}

// NewProbeClient is NewFMSWorkerClient for a surface that polls (the status snapshot, the mount
// guard): one dial attempt per request, so an absent worker answers at once instead of after the
// retry pauses.
func NewProbeClient() *FMSWorkerClient {
	c := NewFMSWorkerClient()
	c.attempts = 1
	return c
}

// SetServerVerifier replaces the pipe-server check; a test seam, never called by the product.
func (c *FMSWorkerClient) SetServerVerifier(v ServerVerifier) { c.verify = v }
func NewClient(pipe string, dial Dial) *FMSWorkerClient {
	return &FMSWorkerClient{pipePath: pipe, dial: dial, retryDelay: 500 * time.Millisecond, dialTimeout: defaultDialTimeout}
}

// dialOnce makes one bounded connect attempt.
func (c *FMSWorkerClient) dialOnce(ctx context.Context) (net.Conn, error) {
	limit := c.dialTimeout
	if limit <= 0 {
		limit = defaultDialTimeout
	}
	dctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	return c.dial(dctx, c.pipePath)
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.Is(err, os.ErrDeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout())
}

// exchangeError reports a failed read or write, keeping a cancelled or expired
// caller context distinguishable from a malformed worker answer.
func exchangeError(ctx context.Context, err error, what string) error {
	if cerr := ctx.Err(); cerr != nil {
		return fmt.Errorf("%s: %w", what, cerr)
	}
	if isTimeout(err) {
		if d, ok := ctx.Deadline(); ok && !time.Now().Before(d) {
			return fmt.Errorf("%s: %w", what, context.DeadlineExceeded)
		}
		return fmt.Errorf("%w: %s timed out: %w", ErrCommunication, what, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrCommunication, what, err)
}
func (c *FMSWorkerClient) exchange(ctx context.Context, req any, version int) (*wireResponse, error) {
	var conn net.Conn
	var err error
	attempts := c.attempts
	if attempts <= 0 {
		attempts = 3
	}
	for attempt := 0; attempt < attempts; attempt++ {
		conn, err = c.dialOnce(ctx)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Access refusals are not transient startup failures.
		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("%w: pipe access denied", ErrCommunication)
		}
		if attempt < attempts-1 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retryDelay):
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrWorkerUnavailable, err)
	}
	defer conn.Close()
	if c.verify != nil {
		if err = c.verify(conn, carriesCredential(req)); err != nil {
			return nil, err
		}
	}
	limit := 30 * time.Second
	if version == 2 {
		limit = 3 * time.Minute
	}
	deadline := time.Now().Add(limit)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("%w: set deadline: %w", ErrCommunication, err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if err = json.NewEncoder(conn).Encode(req); err != nil {
		return nil, exchangeError(ctx, err, "write request")
	}
	var resp wireResponse
	if err = json.NewDecoder(io.LimitReader(conn, maxResponseBytes)).Decode(&resp); err != nil {
		if ctx.Err() != nil || isTimeout(err) {
			return nil, exchangeError(ctx, err, "read response")
		}
		return nil, fmt.Errorf("%w: invalid worker response", ErrCommunication)
	}
	if resp.SchemaVersion != version {
		return nil, fmt.Errorf("%w: schema mismatch; update FileDO and FMS for Windows", ErrCommunication)
	}
	if len(resp.Disks) > maxRecords || (resp.Status != nil && len(resp.Status.Roots) > maxRecords) {
		return nil, fmt.Errorf("%w: the worker answered with more than %d records", ErrCommunication, maxRecords)
	}
	if !resp.OK {
		if resp.Error == "" {
			return nil, &WorkerError{Message: ErrCommunication.Error() + ": worker refused request", OutcomeClass: resp.OutcomeClass, cause: ErrCommunication}
		}
		return nil, &WorkerError{Message: boundMessage(redactCredential(resp.Error, req)), OutcomeClass: resp.OutcomeClass}
	}
	return &resp, nil
}

// A real answer is a few KB (a hundred disks fit in 40 KB). The limits bound what a buggy or hostile
// peer can make FileDO allocate and print: bytes read, records decoded, characters of a message
// (AUD-86-F4).
const (
	maxResponseBytes = 256 << 10
	maxRecords       = 1024
	maxMessageBytes  = 1024
)

// boundMessage cuts a worker's message to maxMessageBytes and replaces control characters, so it can be
// printed and recorded as it is.
func boundMessage(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= maxMessageBytes {
			b.WriteString("..")
			break
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			r = ' '
		}
		b.WriteRune(r)
	}
	return b.String()
}

// carriesCredential reports whether a request has a disk password in it.
func carriesCredential(req any) bool {
	switch r := req.(type) {
	case DiskOpenRequest:
		return r.Password != ""
	case DiskAutostartRequest:
		return r.Password != ""
	}
	return false
}

// redactCredential removes the request's password, raw and JSON-escaped, from a
// worker message.
func redactCredential(message string, req any) string {
	var credential string
	switch r := req.(type) {
	case DiskOpenRequest:
		credential = r.Password
	case DiskAutostartRequest:
		credential = r.Password
	}
	if credential == "" {
		return message
	}
	message = strings.ReplaceAll(message, credential, "***")
	if b, e := json.Marshal(credential); e == nil && len(b) > 2 {
		if escaped := string(b[1 : len(b)-1]); escaped != credential {
			message = strings.ReplaceAll(message, escaped, "***")
		}
	}
	return message
}
func (c *FMSWorkerClient) GetStatus(ctx context.Context) (*WorkerStatus, error) {
	r, e := c.exchange(ctx, struct {
		SchemaVersion int    `json:"schemaVersion"`
		Type          string `json:"type"`
	}{1, "GetStatus"}, 1)
	if e != nil {
		return nil, e
	}
	if r.Status == nil {
		return nil, fmt.Errorf("%w: missing status", ErrCommunication)
	}
	return r.Status, nil
}
func (c *FMSWorkerClient) Connect(ctx context.Context) error {
	status, err := c.GetStatus(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capable = false
	c.mode = WorkerModeUnknown
	c.capability = nil
	if err != nil {
		return err
	}
	c.mode = parseWorkerMode(status)
	c.capability = status.Capabilities
	c.capable = status.Capabilities["disk-sharing"]
	if !c.capable {
		return ErrNotCapable
	}
	return nil
}
func (c *FMSWorkerClient) CheckCapability(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.capability[name] {
		return ErrNotCapable
	}
	return nil
}
func (c *FMSWorkerClient) WorkerMode() WorkerMode { c.mu.Lock(); defer c.mu.Unlock(); return c.mode }
func (c *FMSWorkerClient) IsAvailable() bool      { c.mu.Lock(); defer c.mu.Unlock(); return c.capable }
func parseWorkerMode(s *WorkerStatus) WorkerMode {
	if s != nil {
		switch s.Mode {
		case "service":
			return WorkerModeService
		case "session":
			return WorkerModeUser
		}
	}
	return WorkerModeUnknown
}
func (c *FMSWorkerClient) request(req any) (*wireResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return c.requestCtx(ctx, req)
}
func (c *FMSWorkerClient) requestCtx(ctx context.Context, req any) (*wireResponse, error) {
	c.mu.Lock()
	capable := c.capable
	c.mu.Unlock()
	if !capable {
		if e := c.Connect(ctx); e != nil {
			return nil, e
		}
	}
	return c.exchange(ctx, req, 2)
}
func base(typ, action string) BaseRequest {
	now := time.Now().UTC()
	return BaseRequest{RequestID: fmt.Sprintf("filedo-%d-%d", os.Getpid(), now.UnixNano()), SchemaVersion: 2, Type: typ, Action: action, Timestamp: now}
}
func (c *FMSWorkerClient) ShareDisk(path, name string, ro bool) error {
	_, e := c.request(DiskShareRequest{BaseRequest: base("disk-share", "share"), ContainerPath: path, RootName: name, ReadOnly: ro, Owner: "filedo", Persist: true})
	return e
}
func (c *FMSWorkerClient) UnshareDisk(path string) error {
	_, e := c.request(DiskShareRequest{BaseRequest: base("disk-share", "unshare"), ContainerPath: path, Owner: "filedo"})
	return e
}
func (c *FMSWorkerClient) OpenDisk(path, password string) error {
	_, e := c.request(DiskOpenRequest{BaseRequest: base("disk-open", "open"), ContainerPath: path, Password: password, Owner: "filedo", MountOptions: DiskMountOptions{NoLetter: true}})
	return e
}
func (c *FMSWorkerClient) CloseDisk(path string, force bool) error {
	return c.CloseDiskBound(path, force, 30)
}

// CloseDiskBound closes a disk waiting up to bound seconds for handles to drain:
// 0 selects the worker default, otherwise 1..90 (the worker refuses anything else).
func (c *FMSWorkerClient) CloseDiskBound(path string, force bool, bound int) error {
	if bound < 0 || bound > 90 {
		return fmt.Errorf("drain bound must be 0 (default) or 1..90 seconds, got %d", bound)
	}
	_, e := c.request(DiskCloseRequest{BaseRequest: base("disk-close", "close"), ContainerPath: path, Force: force, DrainTimeout: bound, Owner: "filedo"})
	return e
}

// SetAutostart is called only after the CLI has collected explicit storage consent.
func (c *FMSWorkerClient) SetAutostart(path string, enable bool, password []byte) error {
	_, e := c.request(DiskAutostartRequest{BaseRequest: base("disk-autostart", "set-autostart"), ContainerPath: path, Enable: enable, Consent: enable, Password: string(password), Owner: "filedo"})
	return e
}
func (c *FMSWorkerClient) GetDiskStatus(path string) (*SharedDiskInfo, error) {
	// Owner is required even for a read, avoiding ambiguity with FMS folder controls.
	r, e := c.request(DiskQueryRequest{BaseRequest: base("disk-status", "status"), ContainerPath: path, Owner: "filedo"})
	if e != nil {
		return nil, e
	}
	if r.Disk == nil {
		return nil, fmt.Errorf("%w: missing disk record", ErrCommunication)
	}
	return r.Disk, nil
}
func (c *FMSWorkerClient) GetOpenHandles(path string) (int, error) {
	s, e := c.GetDiskStatus(path)
	if e != nil {
		return 0, e
	}
	return s.OpenHandles, nil
}

// Probe asks the worker for its mode and its full disk list inside ctx: the one question the status
// snapshot and the mount guard put. ErrNotCapable when the worker cannot share disks.
func (c *FMSWorkerClient) Probe(ctx context.Context) (WorkerMode, []SharedDiskInfo, error) {
	if e := c.Connect(ctx); e != nil {
		return c.WorkerMode(), nil, e
	}
	r, e := c.requestCtx(ctx, DiskQueryRequest{BaseRequest: base("disk-list", "list"), Owner: "filedo"})
	if e != nil {
		return c.WorkerMode(), nil, e
	}
	return c.WorkerMode(), r.Disks, nil
}
func (c *FMSWorkerClient) ListSharedDisks() ([]SharedDiskInfo, error) {
	r, e := c.request(DiskQueryRequest{BaseRequest: base("disk-list", "list"), Owner: "filedo"})
	if e != nil {
		return nil, e
	}
	return r.Disks, nil
}
func (c *FMSWorkerClient) ListRoots() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, e := c.GetStatus(ctx)
	if e != nil {
		return nil, e
	}
	names := make([]string, 0, len(s.Roots))
	for _, r := range s.Roots {
		names = append(names, r.Name)
	}
	return names, nil
}
func GetWorkerMode() WorkerMode {
	c := NewFMSWorkerClient()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s, e := c.GetStatus(ctx)
	if e != nil {
		return WorkerModeUnknown
	}
	return parseWorkerMode(s)
}
func InstallPath() (string, error) {
	// A 386 process sees ProgramFiles as the x86 folder; ProgramW6432 is the native one.
	return installPathIn(os.Getenv("ProgramW6432"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"))
}
func installPathIn(roots ...string) (string, error) {
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, name := range []string{"Fast Media Sorter Server", "Fast Media Sorter", "FastMediaSorter", "FastMediaSorter_Server", "FastMediaSorter_LITE"} {
			p := filepath.Join(root, name, "filedo", "filedo.exe")
			if s, e := os.Stat(p); e == nil && s.Mode().IsRegular() {
				return p, nil
			}
		}
	}
	return "", ErrWorkerUnavailable
}
func VerifyBundledFileDo(path, expected string) error {
	expected = strings.TrimSpace(expected)
	if fields := strings.Fields(expected); len(fields) > 0 {
		expected = fields[0]
	}
	want, e := hex.DecodeString(expected)
	if e != nil || len(want) != sha256.Size {
		return errors.New("invalid expected FileDO SHA-256")
	}
	f, e := os.Open(path)
	if e != nil {
		return errors.New("bundled FileDO is missing")
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return e
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), expected) {
		return errors.New("bundled FileDO SHA-256 mismatch")
	}
	return nil
}
