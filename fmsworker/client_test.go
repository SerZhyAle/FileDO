//go:build windows

package fmsworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func exchangeClient(t *testing.T, reply func(map[string]any) any) *FMSWorkerClient {
	t.Helper()
	c := NewClient("test", func(ctx context.Context, _ string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer b.Close()
			var req map[string]any
			if e := json.NewDecoder(b).Decode(&req); e != nil {
				return
			}
			json.NewEncoder(b).Encode(reply(req))
		}()
		return a, nil
	})
	c.retryDelay = time.Millisecond
	return c
}
func TestWorkerDiscovery(t *testing.T) {
	for _, mode := range []string{"service", "session"} {
		t.Run(mode, func(t *testing.T) {
			c := exchangeClient(t, func(req map[string]any) any {
				if req["type"] != "GetStatus" || req["schemaVersion"] != float64(1) {
					t.Errorf("unexpected discovery: %v", req)
				}
				return map[string]any{"schemaVersion": 1, "ok": true, "status": map[string]any{"mode": mode, "capabilities": map[string]bool{"disk-sharing": true, "disabled": false}}}
			})
			if e := c.Connect(context.Background()); e != nil {
				t.Fatal(e)
			}
			if c.CheckCapability("disabled") == nil {
				t.Fatal("false capability became supported")
			}
			want := WorkerModeService
			if mode == "session" {
				want = WorkerModeUser
			}
			if c.WorkerMode() != want {
				t.Fatal("wrong hosting mode")
			}
		})
	}
}
func TestUnavailableAndRetry(t *testing.T) {
	for _, failures := range []int{0, 1, 2, 3} {
		t.Run(string(rune('0'+failures)), func(t *testing.T) {
			var calls atomic.Int32
			good := exchangeClient(t, func(_ map[string]any) any {
				return map[string]any{"schemaVersion": 1, "ok": true, "status": map[string]any{}}
			})
			dial := good.dial
			good.dial = func(ctx context.Context, p string) (net.Conn, error) {
				if int(calls.Add(1)) <= failures {
					return nil, os.ErrNotExist
				}
				return dial(ctx, p)
			}
			_, e := good.GetStatus(context.Background())
			if (e != nil) != (failures == 3) {
				t.Fatalf("failures=%d err=%v", failures, e)
			}
			if calls.Load() > 3 {
				t.Fatal("unbounded retries")
			}
		})
	}
}
func TestAccessDeniedDoesNotRetry(t *testing.T) {
	calls := 0
	c := NewClient("test", func(context.Context, string) (net.Conn, error) { calls++; return nil, os.ErrPermission })
	if _, e := c.GetStatus(context.Background()); e == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", e, calls)
	}
}
func TestWorkerRefusals(t *testing.T) {
	for _, reply := range []any{map[string]any{"schemaVersion": 2, "ok": true}, map[string]any{"schemaVersion": 1, "ok": false}, map[string]any{"schemaVersion": 1, "ok": true}, map[string]any{"schemaVersion": 1, "ok": true, "status": map[string]any{"capabilities": map[string]bool{"disk-sharing": false}}}} {
		c := exchangeClient(t, func(map[string]any) any { return reply })
		if e := c.Connect(context.Background()); e == nil {
			t.Fatalf("accepted invalid or incapable response: %v", reply)
		}
	}
}
func TestDiskRequestsUseFlatVersionTwo(t *testing.T) {
	c := exchangeClient(t, func(req map[string]any) any {
		if req["type"] == "GetStatus" {
			return map[string]any{"schemaVersion": 1, "ok": true, "status": map[string]any{"capabilities": map[string]bool{"disk-sharing": true}}}
		}
		if req["schemaVersion"] != float64(2) || req["type"] != "disk-share" || req["owner"] != "filedo" || req["readOnly"] != true || req["request"] != nil {
			t.Error("disk request has the wrong wire grammar")
		}
		return map[string]any{"schemaVersion": 2, "ok": true}
	})
	if e := c.ShareDisk(`C:\disk.fdd`, "Media", true); e != nil {
		t.Fatal(e)
	}
}
func TestRootsComeFromWorker(t *testing.T) {
	c := exchangeClient(t, func(map[string]any) any {
		return map[string]any{"schemaVersion": 1, "ok": true, "status": map[string]any{"roots": []map[string]any{{"name": "Photos"}, {"name": "Video"}}}}
	})
	roots, e := c.ListRoots()
	if e != nil || len(roots) != 2 || roots[0] != "Photos" {
		t.Fatalf("roots=%v err=%v", roots, e)
	}
}
func TestCancelledSilentPeerCloses(t *testing.T) {
	closed := make(chan struct{})
	c := NewClient("test", func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() {
			defer close(closed)
			defer b.Close()
			var req any
			json.NewDecoder(b).Decode(&req)
			var buf [1]byte
			b.Read(buf[:])
		}()
		return a, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, e := c.GetStatus(ctx); !errors.Is(e, context.DeadlineExceeded) || errors.Is(e, ErrCommunication) {
		t.Fatalf("silent peer: want the context deadline, got %v", e)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("cancelled call leaked its connection")
	}
}
func TestBundledHashIsVerified(t *testing.T) {
	p := filepath.Join(t.TempDir(), "filedo.exe")
	b := []byte("test executable")
	os.WriteFile(p, b, 0600)
	h := sha256.Sum256(b)
	hash := hex.EncodeToString(h[:])
	if e := VerifyBundledFileDo(p, hash+"  filedo.exe"); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("replaced"), 0600)
	if e := VerifyBundledFileDo(p, hash); e == nil {
		t.Fatal("modified exe accepted")
	}
	if e := VerifyBundledFileDo(p, "invalid"); e == nil {
		t.Fatal("invalid pin accepted")
	}
	if e := VerifyBundledFileDo(p+".missing", hash); !errors.Is(e, os.ErrNotExist) && e == nil {
		t.Fatal("missing binary accepted")
	}
}
func TestMalformedResponse(t *testing.T) {
	c := NewClient("test", func(context.Context, string) (net.Conn, error) {
		a, b := net.Pipe()
		go func() { defer b.Close(); var req any; json.NewDecoder(b).Decode(&req); b.Write([]byte("invalid JSON")) }()
		return a, nil
	})
	if _, e := c.GetStatus(context.Background()); e == nil {
		t.Fatal("malformed JSON accepted")
	}
}
