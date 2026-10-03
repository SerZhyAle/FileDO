//go:build windows

package fmsworker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	winio "github.com/Microsoft/go-winio"
)

const (
	testPID     = 4242
	otherPID    = 777
	testSelfSID = "S-1-5-21-1-2-3-1001"
	testService = `C:\Program Files\FastMediaSorter_Server\companion\fms-share-worker.exe`
)

// fakeIdentity builds identityOps over a table of processes.
type fakeProc struct {
	sid, image string
	noSID      bool
}

func fakeIdentity(servicePID uint32, procs map[uint32]fakeProc) identityOps {
	return identityOps{
		pipePID:    func(net.Conn) (uint32, error) { return testPID, nil },
		servicePID: func() (uint32, error) { return servicePID, nil },
		userSID: func(pid uint32) (string, error) {
			p, ok := procs[pid]
			if !ok || p.noSID {
				return "", errors.New("access denied")
			}
			return p.sid, nil
		},
		selfSID: func() (string, error) { return testSelfSID, nil },
		image: func(pid uint32) (string, error) {
			p, ok := procs[pid]
			if !ok {
				return "", errors.New("no such process")
			}
			return p.image, nil
		},
		programFiles: func() []string { return []string{`C:\Program Files`, `C:\Program Files (x86)`} },
	}
}

func TestCheckPipeServerTable(t *testing.T) {
	user := `C:\Users\me\AppData\Local\Programs\FastMediaSorter_LITE\companion\fms-share-worker.exe`
	cases := []struct {
		name       string
		servicePID uint32
		proc       fakeProc
		credential bool
		wantErr    string // "" = accepted
	}{
		{"service, no credential", testPID, fakeProc{noSID: true, image: testService}, false, ""},
		{"service, credential", testPID, fakeProc{noSID: true, image: testService}, true, ""},
		{"service image outside Program Files", testPID, fakeProc{noSID: true, image: `C:\Users\me\fms-share-worker.exe`}, true, "Program Files"},
		{"service image is not the worker", testPID, fakeProc{noSID: true, image: `C:\Program Files\Other\evil.exe`}, true, "not the FMS worker"},
		{"service pid belongs to another process", otherPID, fakeProc{sid: "S-1-5-18", image: testService}, true, "another account"},
		{"squatter of another account", 0, fakeProc{sid: "S-1-5-21-9-9-9-1002", image: user}, false, "another account"},
		{"squatter running as SYSTEM that is not the service", 0, fakeProc{noSID: true, image: testService}, true, "account of the pipe's server process could not be read"},
		{"session worker of the same account", 0, fakeProc{sid: testSelfSID, image: user}, true, ""},
		{"same account, image is not the worker", 0, fakeProc{sid: testSelfSID, image: `C:\Users\me\evil.exe`}, true, "not the FMS worker"},
		{"same account, no credential, any image", 0, fakeProc{sid: testSelfSID, image: `C:\Users\me\evil.exe`}, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ops := fakeIdentity(tc.servicePID, map[uint32]fakeProc{testPID: tc.proc, otherPID: tc.proc})
			err := checkPipeServer(nil, tc.credential, ops)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.wantErr)
			}
			if !errors.Is(err, ErrUntrustedServer) || !errors.Is(err, ErrWorkerUnavailable) {
				t.Fatalf("refusal %v is not ErrUntrustedServer and ErrWorkerUnavailable", err)
			}
		})
	}
}

func TestCheckPipeServerFailsClosedWhenTheServerCannotBeRead(t *testing.T) {
	ops := fakeIdentity(0, nil)
	ops.pipePID = func(net.Conn) (uint32, error) { return 0, errors.New("no handle") }
	if err := checkPipeServer(nil, false, ops); !errors.Is(err, ErrUntrustedServer) {
		t.Fatalf("err = %v", err)
	}
	ops = fakeIdentity(0, map[uint32]fakeProc{testPID: {sid: testSelfSID}})
	ops.selfSID = func() (string, error) { return "", errors.New("no token") }
	if err := checkPipeServer(nil, false, ops); !errors.Is(err, ErrUntrustedServer) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnderAnyIsADirectoryTest(t *testing.T) {
	roots := []string{`C:\Program Files`}
	for p, want := range map[string]bool{
		`C:\Program Files\X\w.exe`:      true,
		`c:\program files\x\w.exe`:      true,
		`C:\Program Files Evil\w.exe`:   false,
		`C:\Program Files`:              false,
		`D:\Program Files\X\w.exe`:      false,
		`C:\Users\me\Program Files\w.x`: false,
	} {
		if underAny(p, roots) != want {
			t.Errorf("underAny(%q) = %v", p, !want)
		}
	}
	if underAny(`C:\x\w.exe`, []string{""}) {
		t.Error("an empty root matched")
	}
}

// TestClientRefusesForeignPipeServer is AUD-86-F3 / AUD-84-F5: a listener that is not the worker
// answers GetStatus as a capable one; the password must never reach it.
func TestClientRefusesForeignPipeServer(t *testing.T) {
	name := uniquePipe()
	l, err := winio.ListenPipe(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var bytesRead atomic.Int64
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			// Answer GetStatus like a worker; count whatever else is written.
			var req map[string]any
			dec := json.NewDecoder(conn)
			if dec.Decode(&req) == nil && req["type"] == "GetStatus" {
				json.NewEncoder(conn).Encode(capableStatus)
				conn.Close()
				continue
			}
			n, _ := io.Copy(io.Discard, io.MultiReader(dec.Buffered(), conn))
			bytesRead.Add(n)
			conn.Close()
		}
	}()
	c := NewClient(name, winio.DialPipeContext)
	c.retryDelay = time.Millisecond
	var verified atomic.Int32
	c.SetServerVerifier(func(_ net.Conn, credential bool) error {
		verified.Add(1)
		if credential {
			return untrusted("the pipe's server is not the FMS worker")
		}
		return nil // discovery carries no secret
	})
	if e := c.Connect(context.Background()); e != nil {
		t.Fatal(e)
	}
	e := c.SetAutostart(`C:\d.fdd`, true, []byte("hunter2"))
	if !errors.Is(e, ErrUntrustedServer) || !errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("SetAutostart err = %v", e)
	}
	e = c.OpenDisk(`C:\d.fdd`, "hunter2")
	if !errors.Is(e, ErrUntrustedServer) {
		t.Fatalf("OpenDisk err = %v", e)
	}
	time.Sleep(50 * time.Millisecond)
	if n := bytesRead.Load(); n != 0 {
		t.Fatalf("the foreign server read %d bytes after the refusal", n)
	}
	if verified.Load() < 3 {
		t.Fatalf("verifier ran %d times, want one per connection", verified.Load())
	}
}

// TestRealPipeServerIsVerifiedByProcess drives the production check against a listener in this very
// process: it is the same account (so discovery passes), and the test binary is not the worker image
// (so a password is refused).
func TestRealPipeServerIsVerifiedByProcess(t *testing.T) {
	name := uniquePipe()
	l, err := winio.ListenPipe(name, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			conn, e := l.Accept()
			if e != nil {
				return
			}
			var req map[string]any
			if json.NewDecoder(conn).Decode(&req) == nil {
				json.NewEncoder(conn).Encode(capableStatus)
			}
			conn.Close()
		}
	}()
	c := NewClient(name, winio.DialPipeContext)
	c.retryDelay = time.Millisecond
	c.SetServerVerifier(verifyPipeServer)
	if e := c.Connect(context.Background()); e != nil {
		t.Fatalf("discovery against a same-account server: %v", e)
	}
	e := c.SetAutostart(`C:\d.fdd`, true, []byte("x"))
	if !errors.Is(e, ErrUntrustedServer) || !strings.Contains(e.Error(), "not the FMS worker") {
		t.Fatalf("a password to a server that is not the worker: %v", e)
	}
}

func TestProbeClientDoesNotRetryAnAbsentPipe(t *testing.T) {
	c := NewProbeClient()
	c.pipePath = uniquePipe() // nothing listens
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, e := c.Probe(ctx)
	if !errors.Is(e, ErrWorkerUnavailable) {
		t.Fatalf("err = %v", e)
	}
	if d := time.Since(start); d > 400*time.Millisecond {
		t.Fatalf("an absent pipe took %s; the retry pauses are still in the probe", d)
	}
}

func TestProbeReturnsModeAndList(t *testing.T) {
	c := exchangeClient(t, func(req map[string]any) any {
		if req["type"] == "GetStatus" {
			return capableStatus
		}
		return map[string]any{"schemaVersion": 2, "ok": true, "disks": []map[string]any{{"containerId": "id", "containerPath": `C:\d.fdd`, "rootName": "R", "state": "open", "holder": "service", "openHandles": 2}}}
	})
	mode, disks, e := c.Probe(context.Background())
	if e != nil || mode != WorkerModeService || len(disks) != 1 || disks[0].OpenHandles != 2 {
		t.Fatalf("mode %v disks %+v err %v", mode, disks, e)
	}
}
