package vdisk

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// SP-0064 T3-F3 / AUD-35-F2 remainder: a Discovery session cannot hold a slot
// for good by pinging under the idle deadline, and Discovery sessions cannot
// fill the connection cap the real initiator needs.

// sendTargets sends one SendTargets text request and reads the answer; it
// reports false when the connection is gone.
func (i *ini) sendTargets() bool {
	b := make([]byte, bhsLen)
	b[0] = opTextReq
	b[1] = 0x80
	i.itt++
	put32(b[16:], i.itt)
	put32(b[20:], 0xffffffff)
	put32(b[24:], i.cmdSN)
	i.cmdSN++
	put32(b[28:], i.expStat)
	if writePDU(i.w, b, encodeKeys([]kv{{"SendTargets", "All"}})) != nil || i.w.Flush() != nil {
		return false
	}
	i.nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	r, err := i.readPDU()
	if err != nil {
		return false
	}
	i.expStat = be32(r.bhs[24:]) + 1
	return true
}

func TestVD_SCSI_ADiscoverySessionHasAnAbsoluteLifetime(t *testing.T) {
	oldIdle, oldLife := discoveryIdle, discoveryLifetime
	discoveryIdle, discoveryLifetime = 400*time.Millisecond, time.Second
	defer func() { discoveryIdle, discoveryLifetime = oldIdle, oldLife }()
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)

	i := dial(t, h.addr)
	i.loginDiscovery()
	start := time.Now()
	for time.Since(start) < 4*time.Second {
		// NOP pings, not SendTargets: the answer to that ends the session at once.
		if !i.nop() {
			if took := time.Since(start); took > 2500*time.Millisecond {
				t.Errorf("the Discovery session lasted %v, want about %v", took, discoveryLifetime)
			}
			return
		}
		time.Sleep(150 * time.Millisecond) // well under the idle deadline
	}
	t.Fatalf("a Discovery session kept alive by requests was still open after 4 s (lifetime %v)", discoveryLifetime)
}

func TestVD_SCSI_DiscoverySessionsAreCappedApart(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	for n := 0; n < maxDiscoverySessions; n++ {
		i := dial(t, h.addr)
		i.loginDiscovery() // held open: no SendTargets, which would end it
		if !i.nop() {
			t.Fatalf("Discovery session %d was refused", n+1)
		}
	}
	extra := dial(t, h.addr)
	r := extra.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:ini"}, {"SessionType", "Discovery"}, {"AuthMethod", "None"}})
	if status(r)>>8 == 0 {
		t.Fatalf("Discovery session %d was accepted (status %04x); want at most %d", maxDiscoverySessions+1, status(r), maxDiscoverySessions)
	}
	// The held Discovery sessions do not keep the real initiator out.
	i := dial(t, h.addr)
	i.loginNormal(testIQN, testSecret)
	if !h.srv.SessionActive() {
		t.Fatal("a normal CHAP login failed while Discovery sessions were held")
	}
}

// nop sends one NOP-Out ping and reads the NOP-In; it reports false when the
// connection is gone or the answer is something else.
func (i *ini) nop() bool {
	b := make([]byte, bhsLen)
	b[0] = 0x40 | opNopOut
	b[1] = 0x80
	i.itt++
	put32(b[16:], i.itt)
	put32(b[20:], 0xffffffff)
	put32(b[24:], i.cmdSN)
	put32(b[28:], i.expStat)
	if writePDU(i.w, b, nil) != nil || i.w.Flush() != nil {
		return false
	}
	i.nc.SetReadDeadline(time.Now().Add(2 * time.Second))
	r, err := i.readPDU()
	if err != nil || r.bhs[0]&0x3f != opNopIn {
		return false
	}
	i.expStat = be32(r.bhs[24:]) + 1
	return true
}

// closedWithin reports whether the server closes nc within d (a read ends in
// an error that is not the test's own timeout), and how long that took.
func closedWithin(nc net.Conn, d time.Duration) (bool, time.Duration) {
	start := time.Now()
	nc.SetReadDeadline(start.Add(d))
	_, err := nc.Read(make([]byte, 1))
	return err != nil && !isTimeout(err), time.Since(start)
}

// waitLog waits up to 2 s for the server log to contain sub (the server logs a
// close after the peer may already have seen it).
func waitLog(h *harness, sub string) bool {
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(h.log.text(), sub) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
	return true
}

func (s *Server) preLoginCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preLogin
}

// T3-F3, owner decision 2026-10-01: after the SendTargets answer a Discovery
// session takes only a Logout. The Windows initiator's own flow (Discovery
// login, SendTargets, Logout - SP-0004 M0 transcripts) keeps working.
func TestVD_SCSI_DiscoverySendTargetsThenLogoutIsAnswered(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	i := dial(t, h.addr)
	i.loginDiscovery()
	got := i.text([]kv{{"SendTargets", "All"}})
	if len(got) != 2 || got[0].V != testIQN || got[1].V != h.addr+",1" {
		t.Fatalf("SendTargets = %v", got)
	}
	i.logout() // fails the test unless a Logout response comes back
	if !waitLog(h, "closed after logout") {
		t.Fatalf("the Discovery session did not end as a logout:\n%s", h.log.text())
	}
	// And the real login still follows on a fresh connection.
	n := dial(t, h.addr)
	n.loginNormal(testIQN, testSecret)
	if !h.srv.SessionActive() {
		t.Fatal("no normal session after the Discovery flow")
	}
}

func TestVD_SCSI_DiscoveryTakesNothingButLogoutAfterSendTargets(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	i := dial(t, h.addr)
	i.loginDiscovery()
	if !i.nop() {
		t.Fatal("a NOP before SendTargets was not answered")
	}
	if !i.sendTargets() {
		t.Fatal("SendTargets was not answered")
	}
	if i.nop() {
		t.Fatal("a NOP after the SendTargets answer was answered; want the session closed")
	}
	if closed, _ := closedWithin(i.nc, time.Second); !closed {
		t.Fatal("the Discovery session stayed open after a NOP that followed the SendTargets answer")
	}
	if !waitLog(h, "after the SendTargets answer") {
		t.Errorf("the close was not logged with its reason:\n%s", h.log.text())
	}
	// A second SendTargets is no exception.
	j := dial(t, h.addr)
	j.loginDiscovery()
	j.sendTargets()
	if j.sendTargets() {
		t.Fatal("a second SendTargets was answered")
	}
}

func TestVD_SCSI_DiscoveryIsClosedAfterTheGrace(t *testing.T) {
	old := discoveryGrace
	discoveryGrace = 300 * time.Millisecond
	defer func() { discoveryGrace = old }()
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	i := dial(t, h.addr)
	i.loginDiscovery()
	if !i.sendTargets() {
		t.Fatal("SendTargets was not answered")
	}
	closed, took := closedWithin(i.nc, 3*time.Second)
	if !closed {
		t.Fatal("a Discovery session silent after the SendTargets answer was still open after 3 s")
	}
	if took > 2*time.Second {
		t.Errorf("the session lasted %v after the answer, want about %v", took, discoveryGrace)
	}
}

// T3-F3, owner decision 2026-10-01: a process that keeps every pre-login slot
// filled no longer locks the real initiator out - a newcomer takes the oldest
// pre-login slot - and the total stays bounded.
func TestVD_SCSI_PreLoginSquattersDoNotLockOutTheInitiator(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	squat := make([]net.Conn, 0, maxUnauthConnections)
	for n := 0; n < maxUnauthConnections; n++ {
		nc, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { nc.Close() })
		squat = append(squat, nc)
		// Accept order is what "oldest" means: let each one land first.
		deadline := time.Now().Add(2 * time.Second)
		for h.srv.preLoginCount() < n+1 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if got := h.srv.preLoginCount(); got != maxUnauthConnections {
		t.Fatalf("%d squatters hold %d pre-login slots", maxUnauthConnections, got)
	}

	i := dial(t, h.addr)
	i.loginNormal(testIQN, testSecret)
	if !h.srv.SessionActive() {
		t.Fatal("a CHAP login failed while every pre-login slot was squatted")
	}
	if closed, _ := closedWithin(squat[0], time.Second); !closed {
		t.Error("the oldest squatter was not the one closed")
	}
	for n, nc := range squat[1:] {
		if closed, _ := closedWithin(nc, 100*time.Millisecond); closed {
			t.Errorf("squatter %d was closed; only the oldest should be", n+2)
		}
	}
	if !waitLog(h, "closed 1 oldest before login") {
		t.Errorf("the displacement was not logged:\n%s", h.log.text())
	}

	// A flood still leaves the server at its caps.
	for n := 0; n < 100; n++ {
		nc, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { nc.Close() })
	}
	time.Sleep(300 * time.Millisecond)
	if got := h.srv.preLoginCount(); got > maxUnauthConnections {
		t.Errorf("%d connections before login, want at most %d", got, maxUnauthConnections)
	}
	if got := h.srv.connCount(); got > maxConnections {
		t.Errorf("the server holds %d connections, want at most %d", got, maxConnections)
	}
}

func TestVD_SCSI_ANormalSessionIsNeverEvicted(t *testing.T) {
	h, i := loggedIn(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	for n := 0; n < 4*maxConnections; n++ {
		nc, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { nc.Close() })
		time.Sleep(2 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if !h.srv.SessionActive() {
		t.Fatal("the normal session was dropped by a flood of new connections")
	}
	if r := i.cmd(cdb10(0x28, 0, 1), nil, 512); r.status != statGood {
		t.Fatalf("READ(10) on the normal session after the flood: status %d", r.status)
	}
	i.logout()
	waitNoSession(t, h)
}

// failingSaveBacking fails every write into [from, to) - the data region -
// while failing is set. A failed header write is a fail-stop on purpose
// (AUD-35-F5), so a retry is shown with failing data writes only.
type failingSaveBacking struct {
	*memBacking
	from, to int64
	failing  atomic.Bool
}

func newFailingSaveBacking(c *Container, mb *memBacking) *failingSaveBacking {
	return &failingSaveBacking{memBacking: mb, from: int64(c.hdr.DataOffset), to: c.fileLen - headerSize}
}

func (b *failingSaveBacking) WriteAt(p []byte, off int64) (int, error) {
	if b.failing.Load() && off >= b.from && off < b.to {
		return 0, errors.New("backing write failure")
	}
	return b.memBacking.WriteAt(p, off)
}

// AUD-35-F5 remainder: a save that keeps failing is retried every few seconds
// but logged once a minute, not at every retry.
func TestVD_SavePolicyLogsAFailingSaveOnceAMinute(t *testing.T) {
	oldTick, oldRetry := savePolicyTick, savePolicyRetry
	savePolicyTick, savePolicyRetry = 2*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { savePolicyTick, savePolicyRetry = oldTick, oldRetry })

	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
	b := newFailingSaveBacking(c, mb)
	c.b = b
	if _, err := c.WriteAt(pattern(13, 4096), 0); err != nil {
		t.Fatal(err)
	}
	b.failing.Store(true)
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	failed := 0
	done := make(chan struct{})
	go func() {
		RunSavePolicy(ctx, c, SavePolicy{Every: time.Hour, DirtyLimit: 1 << 12}, func(format string, _ ...interface{}) {
			if strings.HasPrefix(format, "ram: save failed") {
				mu.Lock()
				failed++
				mu.Unlock()
			}
		})
		close(done)
	}()
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done
	if failed != 1 {
		t.Fatalf("a save failing for 300 ms was logged %d times, want once a minute", failed)
	}
}

// AUD-35-F5 remainder: RAMState says that saving is failing, and forgets it
// once a save succeeds.
func TestVD_RAMStateCarriesTheSaveError(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
	b := newFailingSaveBacking(c, mb)
	c.b = b
	if _, err := c.WriteAt(pattern(13, 4096), 0); err != nil {
		t.Fatal(err)
	}
	b.failing.Store(true)
	if err := c.Save(); err == nil {
		t.Fatal("a save onto a failing backing succeeded")
	}
	if s, _ := c.RAMState(); !strings.Contains(s.SaveError, "backing write failure") {
		t.Fatalf("RAMState after a failed save: %+v", s)
	}
	b.failing.Store(false)
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if s, _ := c.RAMState(); s.SaveError != "" || s.DirtyBytes != 0 {
		t.Fatalf("RAMState after a good save: %+v", s)
	}
}
