//go:build windows

package fmsworker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	winio "github.com/Microsoft/go-winio"
)

var capableStatus = map[string]any{"schemaVersion": 1, "ok": true, "status": map[string]any{"mode": "service", "capabilities": map[string]bool{"disk-sharing": true}}}

// recordingClient answers GetStatus as a capable worker and passes every disk
// request to reply, remembering it.
func recordingClient(t *testing.T, reply func(map[string]any) any) (*FMSWorkerClient, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var seen []map[string]any
	c := exchangeClient(t, func(req map[string]any) any {
		if req["type"] == "GetStatus" {
			return capableStatus
		}
		mu.Lock()
		seen = append(seen, req)
		mu.Unlock()
		return reply(req)
	})
	return c, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), seen...)
	}
}
func okReply(map[string]any) any { return map[string]any{"schemaVersion": 2, "ok": true} }
func uniquePipe() string {
	return fmt.Sprintf(`\\.\pipe\filedo-test-%d-%d`, os.Getpid(), time.Now().UnixNano())
}
func TestNamedPipeConnectAndDiskStatus(t *testing.T) {
	name := uniquePipe()
	l, err := winio.ListenPipe(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	sharedAt := time.Date(2026, 10, 1, 12, 30, 45, 0, time.UTC)
	requests := make(chan map[string]any, 8)
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			var req map[string]any
			if json.NewDecoder(conn).Decode(&req) == nil {
				requests <- req
				var resp any
				if req["type"] == "GetStatus" {
					resp = capableStatus
				} else {
					resp = map[string]any{"schemaVersion": 2, "ok": true, "disk": map[string]any{
						"containerId": "id", "containerPath": req["containerPath"], "rootName": "Media",
						"state": "open", "holder": "service", "openHandles": 3, "sharedAt": sharedAt.Format(time.RFC3339),
					}}
				}
				json.NewEncoder(conn).Encode(resp)
			}
			conn.Close()
		}
	}()
	c := NewClient(name, winio.DialPipeContext)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := c.Connect(ctx); e != nil {
		t.Fatal(e)
	}
	if !c.IsAvailable() || c.WorkerMode() != WorkerModeService {
		t.Fatalf("available=%v mode=%v", c.IsAvailable(), c.WorkerMode())
	}
	d, e := c.GetDiskStatus(`C:\disk.fdd`)
	if e != nil {
		t.Fatal(e)
	}
	if d.State != DiskStateOpen || d.Holder != "service" || d.OpenHandles != 3 || d.RootName != "Media" || !d.SharedAt.Equal(sharedAt) {
		t.Fatalf("wrong parse: %+v", d)
	}
	first, second := <-requests, <-requests
	if first["type"] != "GetStatus" || second["type"] != "disk-status" || second["containerPath"] != `C:\disk.fdd` || second["owner"] != "filedo" {
		t.Fatalf("requests: %v %v", first, second)
	}
}
func TestNamedPipeUnavailable(t *testing.T) {
	c := NewClient(uniquePipe(), winio.DialPipeContext)
	c.retryDelay = time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if e := c.Connect(ctx); !errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("err=%v", e)
	}
	if c.IsAvailable() {
		t.Fatal("unavailable worker reported as capable")
	}
	if _, e := c.GetDiskStatus(`C:\disk.fdd`); !errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("disk call err=%v", e)
	}
}
func TestDialStageIsBounded(t *testing.T) {
	var calls atomic.Int32
	c := NewClient("busy", func(ctx context.Context, _ string) (net.Conn, error) {
		calls.Add(1)
		<-ctx.Done() // a busy pipe: DialPipeContext waits for as long as its context lives
		return nil, ctx.Err()
	})
	c.retryDelay = time.Millisecond
	c.dialTimeout = 20 * time.Millisecond
	done := make(chan error, 1)
	go func() { _, e := c.GetStatus(context.Background()); done <- e }()
	select {
	case e := <-done:
		if !errors.Is(e, ErrWorkerUnavailable) {
			t.Fatalf("err=%v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("dial without a caller deadline never gave up")
	}
	if calls.Load() != 3 {
		t.Fatalf("attempts=%d", calls.Load())
	}
}
func TestThreeAttemptsWithPause(t *testing.T) {
	var calls int
	c := NewClient("test", func(context.Context, string) (net.Conn, error) { calls++; return nil, os.ErrNotExist })
	c.retryDelay = 40 * time.Millisecond
	start := time.Now()
	_, e := c.GetStatus(context.Background())
	if !errors.Is(e, ErrWorkerUnavailable) || !errors.Is(e, os.ErrNotExist) {
		t.Fatalf("err=%v", e)
	}
	if calls != 3 {
		t.Fatalf("attempts=%d", calls)
	}
	if elapsed := time.Since(start); elapsed < 2*40*time.Millisecond {
		t.Fatalf("no pause between attempts: %v", elapsed)
	}
}
func TestAccessDeniedIsNotUnavailable(t *testing.T) {
	c := NewClient("test", func(context.Context, string) (net.Conn, error) { return nil, os.ErrPermission })
	_, e := c.GetStatus(context.Background())
	if !errors.Is(e, ErrCommunication) || errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("err=%v", e)
	}
}
func silentPeer() Dial {
	return func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			var req any
			json.NewDecoder(b).Decode(&req)
			var buf [1]byte
			b.Read(buf[:])
		}()
		return a, nil
	}
}
func TestContextCancellationIsReported(t *testing.T) {
	c := NewClient("test", silentPeer())
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, e := c.GetStatus(ctx)
	if !errors.Is(e, context.Canceled) || errors.Is(e, ErrCommunication) {
		t.Fatalf("err=%v", e)
	}
}
func TestContextDeadlineIsReported(t *testing.T) {
	c := NewClient("test", silentPeer())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, e := c.GetStatus(ctx)
	if !errors.Is(e, context.DeadlineExceeded) || errors.Is(e, ErrCommunication) {
		t.Fatalf("err=%v", e)
	}
}
func TestDiskRequestWireShapes(t *testing.T) {
	const path = `C:\disks\media.fdd`
	disk := map[string]any{"containerId": "id", "containerPath": path, "rootName": "Media", "state": "closed", "holder": "none", "sharedAt": "2026-10-01T10:00:00Z"}
	cases := []struct {
		name string
		call func(*FMSWorkerClient) error
		want map[string]any
		gone []string
	}{
		{"share", func(c *FMSWorkerClient) error { return c.ShareDisk(path, "Media", true) },
			map[string]any{"type": "disk-share", "action": "share", "containerPath": path, "rootName": "Media", "readOnly": true, "persist": true}, nil},
		{"unshare", func(c *FMSWorkerClient) error { return c.UnshareDisk(path) },
			map[string]any{"type": "disk-share", "action": "unshare", "containerPath": path}, []string{"rootName"}},
		{"open", func(c *FMSWorkerClient) error { return c.OpenDisk(path, "secret") },
			map[string]any{"type": "disk-open", "action": "open", "containerPath": path, "password": "secret"}, nil},
		{"close", func(c *FMSWorkerClient) error { return c.CloseDisk(path, true) },
			map[string]any{"type": "disk-close", "action": "close", "containerPath": path, "force": true, "drainTimeout": float64(30)}, nil},
		{"close bound", func(c *FMSWorkerClient) error { return c.CloseDiskBound(path, false, 90) },
			map[string]any{"type": "disk-close", "action": "close", "force": false, "drainTimeout": float64(90)}, nil},
		{"close default bound", func(c *FMSWorkerClient) error { return c.CloseDiskBound(path, false, 0) },
			map[string]any{"type": "disk-close", "drainTimeout": float64(0)}, nil},
		{"autostart on", func(c *FMSWorkerClient) error { return c.SetAutostart(path, true, []byte("secret")) },
			map[string]any{"type": "disk-autostart", "action": "set-autostart", "containerPath": path, "enable": true, "consent": true, "password": "secret"}, nil},
		{"autostart off", func(c *FMSWorkerClient) error { return c.SetAutostart(path, false, nil) },
			map[string]any{"type": "disk-autostart", "enable": false, "consent": false}, []string{"password"}},
		{"status", func(c *FMSWorkerClient) error { _, e := c.GetDiskStatus(path); return e },
			map[string]any{"type": "disk-status", "action": "status", "containerPath": path}, nil},
		{"list", func(c *FMSWorkerClient) error { _, e := c.ListSharedDisks(); return e },
			map[string]any{"type": "disk-list", "action": "list"}, []string{"containerPath"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, seen := recordingClient(t, func(map[string]any) any {
				return map[string]any{"schemaVersion": 2, "ok": true, "disk": disk, "disks": []any{disk}}
			})
			if e := tc.call(c); e != nil {
				t.Fatal(e)
			}
			reqs := seen()
			if len(reqs) != 1 {
				t.Fatalf("%d disk requests", len(reqs))
			}
			req := reqs[0]
			want := map[string]any{"schemaVersion": float64(2), "owner": "filedo"}
			for k, v := range tc.want {
				want[k] = v
			}
			for k, v := range want {
				if req[k] != v {
					t.Errorf("%s = %v, want %v (request %v)", k, req[k], v, req)
				}
			}
			for _, k := range tc.gone {
				if _, ok := req[k]; ok {
					t.Errorf("%s must be absent: %v", k, req)
				}
			}
			if id, _ := req["requestId"].(string); id == "" {
				t.Error("no requestId")
			}
			ts, _ := req["timestamp"].(string)
			parsed, e := time.Parse(time.RFC3339Nano, ts)
			if e != nil || !strings.HasSuffix(ts, "Z") || time.Since(parsed) > time.Minute || time.Until(parsed) > time.Minute {
				t.Errorf("timestamp %q is not a current UTC time (%v)", ts, e)
			}
		})
	}
}
func TestCloseDiskBoundRejectsBadBound(t *testing.T) {
	for _, bound := range []int{-1, 91, 1000} {
		c, seen := recordingClient(t, okReply)
		if e := c.CloseDiskBound(`C:\d.fdd`, false, bound); e == nil {
			t.Fatalf("bound %d accepted", bound)
		}
		if len(seen()) != 0 {
			t.Fatalf("bound %d reached the worker", bound)
		}
	}
	for _, bound := range []int{0, 1, 90} {
		c, _ := recordingClient(t, okReply)
		if e := c.CloseDiskBound(`C:\d.fdd`, false, bound); e != nil {
			t.Fatalf("bound %d refused: %v", bound, e)
		}
	}
}
func TestDiskStatusAndListParsing(t *testing.T) {
	a := map[string]any{"containerId": "a", "containerPath": `C:\a.fdd`, "rootName": "A", "state": "locked", "holder": "session", "readOnly": true,
		"autostart": true, "hasStoredKey": true, "encrypted": true, "openHandles": 2, "mountPath": `C:\mnt\a`, "errorMessage": "boom",
		"sharedAt": "2026-10-01T10:00:00Z", "lastOpened": "2026-10-02T10:00:00+02:00", "lastClosed": "2026-10-02T11:00:00Z"}
	b := map[string]any{"containerId": "b", "containerPath": `C:\b.fdd`, "rootName": "B", "state": "failed", "holder": "none", "sharedAt": "2026-10-01T10:00:00Z"}
	c, _ := recordingClient(t, func(req map[string]any) any {
		return map[string]any{"schemaVersion": 2, "ok": true, "disk": a, "disks": []any{a, b}}
	})
	d, e := c.GetDiskStatus(`C:\a.fdd`)
	if e != nil {
		t.Fatal(e)
	}
	if d.ContainerID != "a" || d.State != DiskStateLocked || d.Holder != "session" || !d.ReadOnly || !d.Autostart || !d.HasStoredKey || !d.Encrypted ||
		d.MountPath != `C:\mnt\a` || d.ErrorMessage != "boom" || !d.LastOpened.Equal(time.Date(2026, 10, 2, 8, 0, 0, 0, time.UTC)) || !d.LastClosed.Equal(time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)) {
		t.Fatalf("wrong parse: %+v", d)
	}
	if n, e := c.GetOpenHandles(`C:\a.fdd`); e != nil || n != 2 {
		t.Fatalf("handles=%d err=%v", n, e)
	}
	list, e := c.ListSharedDisks()
	if e != nil || len(list) != 2 || list[1].ContainerID != "b" || list[1].State != DiskStateFailed || !list[1].LastOpened.IsZero() {
		t.Fatalf("list=%+v err=%v", list, e)
	}
}
func TestDiskStatusWithoutRecordIsRefused(t *testing.T) {
	c, _ := recordingClient(t, okReply)
	if _, e := c.GetDiskStatus(`C:\a.fdd`); !errors.Is(e, ErrCommunication) {
		t.Fatalf("err=%v", e)
	}
	if _, e := c.GetOpenHandles(`C:\a.fdd`); e == nil {
		t.Fatal("a failed handle query became a zero count")
	}
}
// WORKER-IPC section 10 item D: a state this build does not know is read as unknown, per record - the
// answer is not refused, and a neighbour record keeps its own state. Unknown is never "closed".
func TestUnknownDiskStateIsReadAsUnknown(t *testing.T) {
	for _, word := range []any{"melting", "", nil, 7, true, map[string]any{"x": 1}} {
		c, _ := recordingClient(t, func(map[string]any) any {
			return map[string]any{"schemaVersion": 2, "ok": true,
				"disk":  map[string]any{"containerId": "a", "state": word, "holder": "session"},
				"disks": []any{map[string]any{"containerId": "a", "state": word}, map[string]any{"containerId": "b", "state": "open"}}}
		})
		d, e := c.GetDiskStatus(`C:\a.fdd`)
		if e != nil || d.State != DiskStateUnknown || d.State == DiskStateClosed || d.Holder != "session" {
			t.Fatalf("state %v: %+v err=%v", word, d, e)
		}
		list, e := c.ListSharedDisks()
		if e != nil || len(list) != 2 || list[0].State != DiskStateUnknown || list[1].State != DiskStateOpen {
			t.Fatalf("state %v: list=%+v err=%v", word, list, e)
		}
	}
}

// A higher schemaVersion carrying a state word of its own is told apart from a malformed answer: the
// user reads "update both halves", not "invalid worker response".
func TestHigherSchemaWithNewStateIsASchemaMismatch(t *testing.T) {
	c, _ := recordingClient(t, func(map[string]any) any {
		return map[string]any{"schemaVersion": 3, "ok": true, "disk": map[string]any{"state": "quiescing"}}
	})
	_, e := c.GetDiskStatus(`C:\a.fdd`)
	if !errors.Is(e, ErrCommunication) || !strings.Contains(e.Error(), "schema mismatch") {
		t.Fatalf("err=%v", e)
	}
}
func TestWriteFailureIsCommunicationError(t *testing.T) {
	c := NewClient("test", func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		b.Close()
		return a, nil
	})
	_, e := c.GetStatus(context.Background())
	if !errors.Is(e, ErrCommunication) || errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("err=%v", e)
	}
}
func TestWorkerErrorCarriesOutcomeClassAndHidesPassword(t *testing.T) {
	const password = `p"w\x`
	escaped, _ := json.Marshal(password)
	for _, tc := range []struct {
		name string
		call func(*FMSWorkerClient) error
	}{
		{"open", func(c *FMSWorkerClient) error { return c.OpenDisk(`C:\d.fdd`, password) }},
		{"autostart", func(c *FMSWorkerClient) error { return c.SetAutostart(`C:\d.fdd`, true, []byte(password)) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := recordingClient(t, func(map[string]any) any {
				return map[string]any{"schemaVersion": 2, "ok": false, "outcomeClass": 6,
					"error": "wrong password " + password + " / " + string(escaped[1:len(escaped)-1])}
			})
			e := tc.call(c)
			var we *WorkerError
			if !errors.As(e, &we) || we.OutcomeClass != 6 {
				t.Fatalf("err=%#v", e)
			}
			if strings.Contains(e.Error(), password) || strings.Contains(e.Error(), string(escaped[1:len(escaped)-1])) || !strings.Contains(e.Error(), "***") {
				t.Fatalf("credential leaked or message lost: %q", e.Error())
			}
		})
	}
}
func TestWorkerRefusalWithoutMessageKeepsClass(t *testing.T) {
	c, _ := recordingClient(t, func(map[string]any) any {
		return map[string]any{"schemaVersion": 2, "ok": false, "outcomeClass": 4}
	})
	e := c.UnshareDisk(`C:\d.fdd`)
	var we *WorkerError
	if !errors.As(e, &we) || we.OutcomeClass != 4 || we.Message == "" || !errors.Is(e, ErrCommunication) {
		t.Fatalf("err=%#v", e)
	}
}
func TestTransportFailuresAreNotWorkerErrors(t *testing.T) {
	c := NewClient("test", func(context.Context, string) (net.Conn, error) { return nil, os.ErrNotExist })
	c.retryDelay = time.Millisecond
	var we *WorkerError
	if e := c.OpenDisk(`C:\d.fdd`, "secret"); errors.As(e, &we) || strings.Contains(e.Error(), "secret") {
		t.Fatalf("err=%v", e)
	}
}
func TestOversizeResponseIsRejected(t *testing.T) {
	c := NewClient("test", func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			var req any
			json.NewDecoder(b).Decode(&req)
			b.Write([]byte(`{"schemaVersion":1,"ok":true,"status":{"mode":"`))
			chunk := []byte(strings.Repeat("x", 1<<16))
			for i := 0; i < (maxResponseBytes>>16)+2; i++ {
				if _, e := b.Write(chunk); e != nil {
					return
				}
			}
			b.Write([]byte(`"}}`))
		}()
		return a, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, e := c.GetStatus(ctx); !errors.Is(e, ErrCommunication) {
		t.Fatalf("err=%v", e)
	}
}
func TestInstallPathSearchesEveryProgramFilesRoot(t *testing.T) {
	native, x86 := t.TempDir(), t.TempDir()
	if _, e := installPathIn("", native, x86); !errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("err=%v", e)
	}
	want := filepath.Join(x86, "Fast Media Sorter", "filedo", "filedo.exe")
	os.MkdirAll(filepath.Dir(want), 0o700)
	os.WriteFile(want, []byte("x"), 0o600)
	if got, e := installPathIn("", native, x86); e != nil || got != want {
		t.Fatalf("got=%q err=%v", got, e)
	}
	nativeExe := filepath.Join(native, "Fast Media Sorter Server", "filedo", "filedo.exe")
	os.MkdirAll(filepath.Dir(nativeExe), 0o700)
	os.WriteFile(nativeExe, []byte("x"), 0o600)
	if got, _ := installPathIn("", native, x86); got != nativeExe {
		t.Fatalf("native install not preferred: %q", got)
	}
}

// A real list is a few KB; a peer that sends a million empty records must not make the client
// allocate for them (AUD-86-F4).
func TestOversizeRecordCountIsRefused(t *testing.T) {
	var body strings.Builder
	body.WriteString(`{"schemaVersion":2,"ok":true,"disks":[`)
	for i := 0; i <= maxRecords; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{}`)
	}
	body.WriteString(`]}`)
	if body.Len() > maxResponseBytes {
		t.Fatalf("fixture is %d bytes, over the byte limit: it would test the wrong cap", body.Len())
	}
	c := exchangeClient(t, func(req map[string]any) any {
		if req["type"] == "GetStatus" {
			return capableStatus
		}
		return json.RawMessage(body.String())
	})
	_, e := c.ListSharedDisks()
	if !errors.Is(e, ErrCommunication) || !strings.Contains(e.Error(), "more than") {
		t.Fatalf("err = %v", e)
	}
	// Exactly at the cap is accepted.
	body.Reset()
	body.WriteString(`{"schemaVersion":2,"ok":true,"disks":[`)
	for i := 0; i < maxRecords; i++ {
		if i > 0 {
			body.WriteByte(',')
		}
		body.WriteString(`{}`)
	}
	body.WriteString(`]}`)
	if d, e := c.ListSharedDisks(); e != nil || len(d) != maxRecords {
		t.Fatalf("at the cap: %d records, err %v", len(d), e)
	}
}

func TestWorkerErrorTextIsBounded(t *testing.T) {
	c, _ := recordingClient(t, func(map[string]any) any {
		return map[string]any{"schemaVersion": 2, "ok": false, "error": "bad\x1b[31mred\r\nline " + strings.Repeat("y", 5000)}
	})
	e := c.UnshareDisk(`C:\d.fdd`)
	var we *WorkerError
	if !errors.As(e, &we) {
		t.Fatalf("err = %#v", e)
	}
	if len(we.Message) > maxMessageBytes+4 {
		t.Fatalf("message is %d bytes", len(we.Message))
	}
	if strings.ContainsAny(we.Message, "\x1b\r\n") || !strings.HasPrefix(we.Message, "bad [31mred  line ") {
		t.Fatalf("control characters survived: %q", we.Message[:40])
	}
}
