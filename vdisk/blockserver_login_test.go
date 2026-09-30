package vdisk

import (
	"bytes"
	"errors"
	"net"
	"runtime"
	"testing"
	"time"
)

// SP-0071 AUD-35-F1: the loopback port is reachable by every local process, so
// a normal session is CHAP or nothing. SessionType and TargetName belong to the
// first Login PDU of a connection: a Discovery security stage (AuthMethod None)
// must never be turned into a normal session by a later PDU, and a normal
// session reaches full feature phase only with a verified CHAP response.

func waitNoSession(t *testing.T, h *harness) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for h.srv.SessionActive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
}

func TestVD_SCSI_DiscoveryNeverBecomesANormalSession(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)

	// (1) A Discovery security stage with AuthMethod None, then an operational
	// PDU that switches to a normal session on the same connection.
	i := dial(t, h.addr)
	r := i.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:x"}, {"SessionType", "Discovery"}, {"AuthMethod", "None"}})
	if st := status(r); st != 0 {
		t.Fatalf("the Discovery security stage was refused: %04x", st)
	}
	r = i.loginPDU(1, 3, true, []kv{{"SessionType", "Normal"}, {"TargetName", testIQN}})
	if st := status(r); st>>8 != 0x02 {
		t.Fatalf("Discovery -> Normal without CHAP: status %04x, want an initiator error (02xx)", st)
	}
	if h.srv.SessionActive() {
		t.Fatal("a session is active after the CHAP bypass")
	}

	// (2) The csg=1 Discovery shortcut, then the same switch.
	i = dial(t, h.addr)
	r = i.loginPDU(1, 1, false, []kv{{"InitiatorName", "iqn.test:x"}, {"SessionType", "Discovery"}})
	if st := status(r); st != 0 {
		t.Fatalf("the Discovery shortcut was refused: %04x", st)
	}
	r = i.loginPDU(1, 3, true, []kv{{"SessionType", "Normal"}, {"TargetName", testIQN}})
	if st := status(r); st>>8 != 0x02 {
		t.Fatalf("Discovery shortcut -> Normal without CHAP: status %04x, want 02xx", st)
	}
	if h.srv.SessionActive() {
		t.Fatal("a session is active after the shortcut bypass")
	}

	// (3) A TargetName alone in a later PDU of a Discovery connection.
	i = dial(t, h.addr)
	r = i.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:x"}, {"SessionType", "Discovery"}, {"AuthMethod", "None"}})
	if st := status(r); st != 0 {
		t.Fatalf("the Discovery security stage was refused: %04x", st)
	}
	r = i.loginPDU(1, 3, true, []kv{{"TargetName", testIQN}})
	if st := status(r); st>>8 != 0x02 {
		t.Fatalf("a TargetName on a Discovery connection: status %04x, want 02xx", st)
	}
	if h.srv.SessionActive() {
		t.Fatal("a session is active after a TargetName on a Discovery connection")
	}

	// The real thing still works afterwards, and Discovery still answers.
	a := dial(t, h.addr)
	a.loginNormal(testIQN, testSecret)
	if !h.srv.SessionActive() {
		t.Fatal("no active session after a proper CHAP login")
	}
	a.logout()
	waitNoSession(t, h)
}

// A normal session may repeat its SessionType and TargetName in a later PDU (a
// value that does not change is harmless); a different one is refused.
func TestVD_SCSI_LoginRepeatsAreToleratedChangesAreNot(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)

	i := dial(t, h.addr)
	r := i.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:x"}, {"SessionType", "Normal"}, {"TargetName", testIQN}, {"AuthMethod", "CHAP,None"}})
	if st := status(r); st != 0 {
		t.Fatalf("security stage: %04x", st)
	}
	r = i.loginPDU(0, 1, true, []kv{{"SessionType", "Discovery"}, {"CHAP_A", "5"}})
	if st := status(r); st>>8 != 0x02 {
		t.Fatalf("changing the session type mid-login: status %04x, want 02xx", st)
	}
	if h.srv.SessionActive() {
		t.Fatal("a session is active after a mid-login type change")
	}
}

// SP-0071 AUD-35-F2: what the port costs before anyone has authenticated is
// bounded. Idle connections are capped, cost KiB and not MiB, and a Discovery
// session cannot park a connection for good.

func (s *Server) connCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.conns)
}

func TestVD_SCSI_UnauthenticatedConnectionsAreBounded(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	const attempts = 200
	conns := make([]net.Conn, 0, attempts)
	for n := 0; n < attempts; n++ {
		nc, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, nc)
	}
	t.Cleanup(func() {
		for _, nc := range conns {
			nc.Close()
		}
	})
	time.Sleep(300 * time.Millisecond) // let the accept loop work through the backlog
	held := h.srv.connCount()
	if held > maxUnauthConnections {
		t.Fatalf("the server holds %d connections that have not logged in, want at most %d", held, maxUnauthConnections)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > int64(held)*(64<<10)+(2<<20) {
		t.Errorf("%d idle connections grew the heap by %d KiB, want under 64 KiB each", held, grown>>10)
	}

	// The refused ones were closed by the server, not parked.
	closed := 0
	for _, nc := range conns {
		nc.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if _, err := nc.Read(make([]byte, 1)); err != nil && !isTimeout(err) {
			closed++
		}
	}
	if closed < attempts-maxUnauthConnections {
		t.Errorf("only %d of %d surplus connections were closed by the server", closed, attempts-maxUnauthConnections)
	}
}

func TestVD_SCSI_UnauthenticatedLoginHasAnAbsoluteDeadline(t *testing.T) {
	old := loginTimeout
	loginTimeout = 250 * time.Millisecond
	defer func() { loginTimeout = old }()
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	nc, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	start := time.Now()
	if _, err := nc.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("an unauthenticated idle peer was not closed: %v", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("login lasted %v despite a %v deadline", elapsed, loginTimeout)
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func TestVD_SCSI_ADiscoverySessionIsClosedWhenIdle(t *testing.T) {
	old := discoveryIdle
	discoveryIdle = 300 * time.Millisecond
	defer func() { discoveryIdle = old }()
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)

	i := dial(t, h.addr)
	r := i.loginPDU(0, 3, true, []kv{{"InitiatorName", "iqn.test:x"}, {"SessionType", "Discovery"}, {"AuthMethod", "None"}})
	if st := status(r); st != 0 {
		t.Fatalf("Discovery login: %04x", st)
	}
	i.nc.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	if _, err := i.nc.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("an idle Discovery session was not closed within 3 s (err %v)", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("the idle Discovery session lasted %v, want about %v", took, discoveryIdle)
	}
}

// Full feature phase keeps working on the grown buffers, including a write and
// a read larger than the login-time buffer.
func TestVD_SCSI_TheNormalSessionKeepsItsDataPath(t *testing.T) {
	const size = 8 << 20
	_, i := loggedIn(t, &memDev{b: make([]byte, size)}, size, false)
	want := bytes.Repeat([]byte{0x5a, 0xa5}, 128<<10) // 256 KiB
	w := make([]byte, 16)
	w[0], w[2], w[5], w[8] = 0x2a, 0, 0, 0
	w[7], w[8] = byte(len(want)/512>>8), byte(len(want)/512)
	if r := i.cmd(w, want, 0); r.status != statGood {
		t.Fatalf("WRITE(10) of 256 KiB: %d", r.status)
	}
	rd := make([]byte, 16)
	rd[0] = 0x28
	rd[7], rd[8] = byte(len(want)/512>>8), byte(len(want)/512)
	if r := i.cmd(rd, nil, len(want)); r.status != statGood || !bytes.Equal(r.data, want) {
		t.Fatalf("READ(10) of 256 KiB: status %d, equal %v", r.status, bytes.Equal(r.data, want))
	}
}
