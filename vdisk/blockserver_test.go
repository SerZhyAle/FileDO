package vdisk

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---------------------------------------------------------------- a small test initiator

type ini struct {
	t        testing.TB
	nc       net.Conn
	r        *bufio.Reader
	w        *bufio.Writer
	cmdSN    uint32
	expStat  uint32
	itt      uint32
	mrdsl    int // what this initiator accepts
	tgtMRDSL int // what the target accepts
	firstB   int
	immed    bool
}

func dial(t testing.TB, addr string) *ini {
	t.Helper()
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { nc.Close() })
	return &ini{t: t, nc: nc, r: bufio.NewReaderSize(nc, 1<<20), w: bufio.NewWriterSize(nc, 1<<20),
		cmdSN: 1, mrdsl: 65536, tgtMRDSL: 8192, firstB: 65536}
}

type rpdu struct {
	bhs  [bhsLen]byte
	data []byte
}

func (i *ini) readPDU() (rpdu, error) {
	var p rpdu
	if _, err := io.ReadFull(i.r, p.bhs[:]); err != nil {
		return p, err
	}
	p.data = make([]byte, dataSegLen(p.bhs[:]))
	return p, readFullPadded(i.r, p.data)
}

func (i *ini) read() rpdu {
	i.t.Helper()
	p, err := i.readPDU()
	if err != nil {
		i.t.Fatalf("read: %v", err)
	}
	return p
}

func (i *ini) send(bhs []byte, data []byte) {
	i.t.Helper()
	if err := writePDU(i.w, bhs, data); err != nil {
		i.t.Fatal(err)
	}
	if err := i.w.Flush(); err != nil {
		i.t.Fatal(err)
	}
}

// loginPDU sends one login request and returns the response (any status).
func (i *ini) loginPDU(csg, nsg byte, transit bool, keys []kv) rpdu {
	i.t.Helper()
	b := make([]byte, bhsLen)
	b[0] = 0x40 | opLoginReq
	b[1] = csg<<2 | nsg
	if transit {
		b[1] |= 0x80
	}
	copy(b[8:14], []byte{0x40, 0, 0, 1, 0, 0})
	i.itt++
	put32(b[16:], i.itt)
	put32(b[24:], i.cmdSN)
	put32(b[28:], i.expStat)
	i.send(b, encodeKeys(keys))
	r := i.read()
	if r.bhs[0]&0x3f != opLoginResp {
		i.t.Fatalf("expected a login response, got 0x%02x", r.bhs[0])
	}
	i.expStat = be32(r.bhs[24:]) + 1
	return r
}

func status(r rpdu) int { return int(r.bhs[36])<<8 | int(r.bhs[37]) }

func keyMap(b []byte) map[string]string {
	m := map[string]string{}
	for _, k := range parseKeys(b) {
		m[k.K] = k.V
	}
	return m
}

func (i *ini) loginDiscovery() {
	i.t.Helper()
	r := i.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:ini"}, {"SessionType", "Discovery"}, {"AuthMethod", "None"}})
	if status(r) != 0 {
		i.t.Fatalf("discovery security stage: status %04x", status(r))
	}
	r = i.loginPDU(1, 3, true, []kv{{"HeaderDigest", "None"}, {"DataDigest", "None"}, {"MaxRecvDataSegmentLength", "65536"}})
	if status(r) != 0 || r.bhs[1]&0x83 != 0x83 {
		i.t.Fatalf("discovery operational stage: status %04x flags %02x", status(r), r.bhs[1])
	}
}

// chapLogin runs the security stage with CHAP under secret. It returns the
// final status (0 on success).
func (i *ini) chapLogin(iqn, secret string) int {
	i.t.Helper()
	r := i.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:ini"}, {"SessionType", "Normal"},
		{"TargetName", iqn}, {"AuthMethod", "CHAP,None"}})
	if st := status(r); st != 0 {
		return st
	}
	if m := keyMap(r.data); m["AuthMethod"] != "CHAP" || r.bhs[1]&0x80 != 0 {
		i.t.Fatalf("the target did not choose CHAP or transited early: %v flags %02x", m, r.bhs[1])
	}
	r = i.loginPDU(0, 1, true, []kv{{"CHAP_A", "7,5"}})
	if st := status(r); st != 0 {
		return st
	}
	m := keyMap(r.data)
	id, _ := strconv.Atoi(m["CHAP_I"])
	chal, err := hex.DecodeString(strings.TrimPrefix(m["CHAP_C"], "0x"))
	if m["CHAP_A"] != "5" || err != nil || len(chal) == 0 {
		i.t.Fatalf("bad CHAP challenge: %v", m)
	}
	h := md5.New()
	h.Write([]byte{byte(id)})
	h.Write([]byte(secret))
	h.Write(chal)
	r = i.loginPDU(0, 1, true, []kv{{"CHAP_N", "iqn.test:ini"}, {"CHAP_R", "0x" + hex.EncodeToString(h.Sum(nil))}})
	if st := status(r); st != 0 {
		return st
	}
	if r.bhs[1]&0x80 == 0 || r.bhs[1]&3 != 1 {
		i.t.Fatalf("no transit to the operational stage after CHAP: flags %02x", r.bhs[1])
	}
	return 0
}

func (i *ini) operational() int {
	i.t.Helper()
	keys := []kv{{"HeaderDigest", "None"}, {"DataDigest", "None"}, {"ErrorRecoveryLevel", "2"},
		{"InitialR2T", "No"}, {"ImmediateData", "Yes"}, {"MaxBurstLength", "262144"},
		{"FirstBurstLength", "65536"}, {"MaxOutstandingR2T", "16"}, {"MaxConnections", "32"},
		{"DataPDUInOrder", "Yes"}, {"DataSequenceInOrder", "Yes"},
		{"MaxRecvDataSegmentLength", strconv.Itoa(i.mrdsl)}, {"DefaultTime2Wait", "0"}, {"DefaultTime2Retain", "60"}}
	r := i.loginPDU(1, 3, true, keys)
	if st := status(r); st != 0 {
		return st
	}
	if r.bhs[1]&0x80 == 0 || r.bhs[1]&3 != 3 {
		i.t.Fatalf("no transit to full feature phase: flags %02x", r.bhs[1])
	}
	for k, v := range keyMap(r.data) {
		switch k {
		case "MaxRecvDataSegmentLength":
			i.tgtMRDSL, _ = strconv.Atoi(v)
		case "FirstBurstLength":
			i.firstB, _ = strconv.Atoi(v)
		case "ImmediateData":
			i.immed = v == "Yes"
		}
	}
	return 0
}

func (i *ini) loginNormal(iqn, secret string) {
	i.t.Helper()
	if st := i.chapLogin(iqn, secret); st != 0 {
		i.t.Fatalf("CHAP login: status %04x", st)
	}
	if st := i.operational(); st != 0 {
		i.t.Fatalf("operational stage: status %04x", st)
	}
}

func (i *ini) text(keys []kv) []kv {
	i.t.Helper()
	b := make([]byte, bhsLen)
	b[0] = opTextReq
	b[1] = 0x80
	i.itt++
	put32(b[16:], i.itt)
	put32(b[20:], 0xffffffff)
	put32(b[24:], i.cmdSN)
	i.cmdSN++
	put32(b[28:], i.expStat)
	i.send(b, encodeKeys(keys))
	r := i.read()
	if r.bhs[0]&0x3f != opTextResp {
		i.t.Fatalf("expected a text response, got 0x%02x", r.bhs[0])
	}
	i.expStat = be32(r.bhs[24:]) + 1
	return parseKeys(r.data)
}

func (i *ini) logout() {
	i.t.Helper()
	b := make([]byte, bhsLen)
	b[0] = 0x40 | opLogoutReq
	b[1] = 0x80
	i.itt++
	put32(b[16:], i.itt)
	put32(b[24:], i.cmdSN)
	put32(b[28:], i.expStat)
	i.send(b, nil)
	if r := i.read(); r.bhs[0]&0x3f != opLogoutResp {
		i.t.Fatalf("expected a logout response, got 0x%02x", r.bhs[0])
	}
	i.nc.Close()
}

type cmdResult struct {
	status   byte
	data     []byte
	sense    []byte
	resFlags byte
	residual uint32
	r2ts     int
}

// sendCmd issues one SCSI command; out != nil means the write direction.
func (i *ini) sendCmd(cdb []byte, out []byte, readLen int) uint32 {
	i.t.Helper()
	b := make([]byte, bhsLen)
	b[0] = opSCSICmd
	b[1] = 0x80
	i.itt++
	put32(b[16:], i.itt)
	var imm []byte
	if out != nil {
		b[1] |= 0x20
		put32(b[20:], uint32(len(out)))
		if i.immed {
			imm = out[:min(len(out), i.firstB, i.tgtMRDSL)]
		}
	} else if readLen > 0 {
		b[1] |= 0x40
		put32(b[20:], uint32(readLen))
	}
	put32(b[24:], i.cmdSN)
	i.cmdSN++
	put32(b[28:], i.expStat)
	copy(b[32:48], cdb)
	i.send(b, imm)
	return i.itt
}

func (i *ini) cmd(cdb []byte, out []byte, readLen int) cmdResult {
	i.t.Helper()
	itt := i.sendCmd(cdb, out, readLen)
	var res cmdResult
	buf := make([]byte, readLen)
	got := 0
	for {
		r := i.read()
		switch r.bhs[0] & 0x3f {
		case opR2T:
			res.r2ts++
			ttt, off, n := be32(r.bhs[20:]), int(be32(r.bhs[40:])), int(be32(r.bhs[44:]))
			var dsn uint32
			for sent := 0; sent < n; {
				k := min(n-sent, i.tgtMRDSL)
				d := make([]byte, bhsLen)
				d[0] = opDataOut
				if sent+k == n {
					d[1] = 0x80
				}
				put32(d[16:], itt)
				put32(d[20:], ttt)
				put32(d[28:], i.expStat)
				put32(d[36:], dsn)
				put32(d[40:], uint32(off+sent))
				dsn++
				if err := writePDU(i.w, d, out[off+sent:off+sent+k]); err != nil {
					i.t.Fatal(err)
				}
				sent += k
			}
			i.w.Flush()
		case opDataIn:
			off := int(be32(r.bhs[40:]))
			copy(buf[off:], r.data)
			got = max(got, off+len(r.data))
			if r.bhs[1]&0x01 != 0 {
				res.status, res.resFlags, res.residual = r.bhs[3], r.bhs[1]&0x06, be32(r.bhs[44:])
				res.data = buf[:got]
				i.expStat = be32(r.bhs[24:]) + 1
				return res
			}
		case opSCSIResp:
			res.status, res.resFlags, res.residual = r.bhs[3], r.bhs[1]&0x06, be32(r.bhs[44:])
			if len(r.data) >= 2 {
				res.sense = r.data[2 : 2+int(be16(r.data))]
			}
			res.data = buf[:got]
			i.expStat = be32(r.bhs[24:]) + 1
			return res
		default:
			i.t.Fatalf("unexpected PDU 0x%02x", r.bhs[0])
		}
	}
}

func cdb10(op byte, lba uint32, n uint16) []byte {
	c := make([]byte, 16)
	c[0] = op
	put32(c[2:], lba)
	put16(c[7:], n)
	return c
}

func cdb16(op byte, lba uint64, n uint32) []byte {
	c := make([]byte, 16)
	c[0] = op
	put64(c[2:], lba)
	put32(c[10:], n)
	return c
}

func senseKey(r cmdResult) (byte, byte) {
	if len(r.sense) < 14 {
		return 0xff, 0xff
	}
	return r.sense[2] & 0x0f, r.sense[12]
}

// ---------------------------------------------------------------- harness

const testIQN = "iqn.2026-09.ua.od.sza:filedo.vd.test"
const testSecret = "abcdefghijKLMNOP"

type harness struct {
	srv  *Server
	addr string
	log  *lockedLog
}

type lockedLog struct {
	mu    sync.Mutex
	lines []string
}

func (l *lockedLog) logf(format string, args ...interface{}) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *lockedLog) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func serve(t testing.TB, dev BlockDevice, size int64, readOnly bool) *harness {
	t.Helper()
	lg := &lockedLog{}
	srv, err := NewServer(TargetConfig{IQN: testIQN, Device: dev, Size: size, PhysicalBlock: 4096,
		ReadOnly: readOnly, Identity: "test-container", ChapSecret: testSecret, Logf: lg.logf})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() {
		srv.Close()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	return &harness{srv: srv, addr: srv.Addr().String(), log: lg}
}

// memDev is a BlockDevice in memory.
type memDev struct {
	mu      sync.Mutex
	b       []byte
	flushes atomic.Int32
}

func (m *memDev) ReadAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copy(p, m.b[off:]), nil
}

func (m *memDev) WriteAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return copy(m.b[off:], p), nil
}

func (m *memDev) Flush() error { m.flushes.Add(1); return nil }

func loggedIn(t *testing.T, dev BlockDevice, size int64, readOnly bool) (*harness, *ini) {
	h := serve(t, dev, size, readOnly)
	i := dial(t, h.addr)
	i.loginNormal(testIQN, testSecret)
	return h, i
}

// ---------------------------------------------------------------- tests

// TestVD_BindAddress: the listener address is the loopback literal, the bound
// socket is on loopback, and no file of the package listens on anything else.
func TestVD_BindAddress(t *testing.T) {
	if loopbackAddr != "127.0.0.1:0" {
		t.Fatalf("loopbackAddr = %q", loopbackAddr)
	}
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	a := h.srv.Addr()
	if !a.IP.Equal(net.IPv4(127, 0, 0, 1)) || a.Port == 0 {
		t.Fatalf("bound %v", a)
	}
	files, _ := filepath.Glob("*.go")
	listen := regexp.MustCompile(`net\.Listen\w*\(`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(src), "\n") {
			if listen.MatchString(line) && !strings.Contains(line, `net.Listen("tcp", loopbackAddr)`) {
				t.Errorf("%s listens on something other than loopbackAddr: %s", f, strings.TrimSpace(line))
			}
		}
	}
}

func TestVD_SCSI_Discovery(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	i := dial(t, h.addr)
	i.loginDiscovery()
	got := i.text([]kv{{"SendTargets", "All"}})
	if len(got) != 2 || got[0].V != testIQN || got[1].V != h.addr+",1" {
		t.Fatalf("SendTargets = %v", got)
	}
	i.logout()
}

func TestVD_SCSI_LoginNeedsCHAP(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)

	// No CHAP offered.
	i := dial(t, h.addr)
	r := i.loginPDU(0, 1, true, []kv{{"InitiatorName", "iqn.test:x"}, {"SessionType", "Normal"},
		{"TargetName", testIQN}, {"AuthMethod", "None"}})
	if status(r) != 0x0201 {
		t.Fatalf("login without CHAP: status %04x", status(r))
	}
	// Straight to the operational stage.
	i = dial(t, h.addr)
	r = i.loginPDU(1, 3, true, []kv{{"InitiatorName", "iqn.test:x"}, {"TargetName", testIQN}})
	if status(r) != 0x0201 {
		t.Fatalf("login skipping security: status %04x", status(r))
	}
	// A wrong secret.
	i = dial(t, h.addr)
	if st := i.chapLogin(testIQN, "wrong-secret-123"); st != 0x0201 {
		t.Fatalf("wrong CHAP secret: status %04x", st)
	}
	// A wrong target name.
	i = dial(t, h.addr)
	if st := i.chapLogin(testIQN+".other", testSecret); st != 0x0203 {
		t.Fatalf("wrong target: status %04x", st)
	}
	if strings.Contains(h.log.text(), testSecret) {
		t.Fatal("the log carries the CHAP secret")
	}

	// The right secret; then a second session is refused while the first lives.
	a := dial(t, h.addr)
	a.loginNormal(testIQN, testSecret)
	if !h.srv.SessionActive() {
		t.Fatal("no active session after login")
	}
	b := dial(t, h.addr)
	if st := b.chapLogin(testIQN, testSecret); st != 0 {
		t.Fatalf("second session security stage: %04x", st)
	}
	if st := b.operational(); st != 0x0202 {
		t.Fatalf("second session: status %04x, want 0202", st)
	}
	a.logout()
	deadline := time.Now().Add(2 * time.Second)
	for h.srv.SessionActive() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	c := dial(t, h.addr)
	c.loginNormal(testIQN, testSecret)
}

func TestVD_SCSI_Basics(t *testing.T) {
	const size = 64 << 20
	_, i := loggedIn(t, &memDev{b: make([]byte, size)}, size, false)

	if r := i.cmd(make([]byte, 16), nil, 0); r.status != statGood {
		t.Fatalf("TEST UNIT READY %d", r.status)
	}
	inq := make([]byte, 16)
	inq[0], inq[4] = 0x12, 96
	r := i.cmd(inq, nil, 96)
	if r.status != statGood || !bytes.HasPrefix(r.data[8:], []byte("FileDO  FDD Container")) {
		t.Fatalf("INQUIRY %d %q", r.status, r.data)
	}
	serial := func() string {
		c := make([]byte, 16)
		c[0], c[1], c[2], c[4] = 0x12, 1, 0x80, 64
		r := i.cmd(c, nil, 64)
		return string(r.data[4:])
	}
	if s := serial(); !strings.HasPrefix(s, "FDD") || len(s) != 23 {
		t.Fatalf("serial %q", s)
	}
	for _, page := range []byte{0x00, 0x83, 0xB0, 0xB1} {
		c := make([]byte, 16)
		c[0], c[1], c[2], c[4] = 0x12, 1, page, 255
		if r := i.cmd(c, nil, 255); r.status != statGood || r.data[1] != page {
			t.Fatalf("VPD %02x: %d", page, r.status)
		}
	}
	rc := make([]byte, 16)
	rc[0] = 0x25
	r = i.cmd(rc, nil, 8)
	if be32(r.data) != size/512-1 || be32(r.data[4:]) != 512 {
		t.Fatalf("READ CAPACITY(10) %x", r.data)
	}
	rc16 := make([]byte, 16)
	rc16[0], rc16[1] = 0x9E, 0x10
	put32(rc16[10:], 32)
	r = i.cmd(rc16, nil, 32)
	if be64(r.data) != size/512-1 || r.data[13] != 3 {
		t.Fatalf("READ CAPACITY(16) %x", r.data)
	}
	ms := make([]byte, 16)
	ms[0], ms[2], ms[4] = 0x1A, 0x3F, 255
	r = i.cmd(ms, nil, 255)
	if r.status != statGood || r.data[2]&0x80 != 0 || r.data[2]&0x10 == 0 {
		t.Fatalf("MODE SENSE(6) %x", r.data)
	}
	rl := make([]byte, 16)
	rl[0] = 0xA0
	put32(rl[6:], 16)
	if r = i.cmd(rl, nil, 16); r.status != statGood || be32(r.data) != 8 {
		t.Fatalf("REPORT LUNS %x", r.data)
	}
	// An opcode outside the subset: ILLEGAL REQUEST, INVALID COMMAND OPERATION CODE.
	un := make([]byte, 16)
	un[0] = 0x42 // UNMAP
	if k, asc := senseKey(i.cmd(un, nil, 0)); k != keyIllegal || asc != ascInvOpcode {
		t.Fatalf("UNMAP: sense %x/%x", k, asc)
	}
	// Past the end: a check condition, not a panic.
	if k, asc := senseKey(i.cmd(cdb10(0x28, size/512, 1), nil, 512)); k != keyIllegal || asc != ascLBARange {
		t.Fatalf("read past the end: %x/%x", k, asc)
	}
	if k, asc := senseKey(i.cmd(cdb16(0x88, 1<<62, 8), nil, 4096)); k != keyIllegal || asc != ascLBARange {
		t.Fatalf("read at a huge LBA: %x/%x", k, asc)
	}
	// Zero-length transfers.
	if r := i.cmd(cdb10(0x28, 0, 0), nil, 0); r.status != statGood {
		t.Fatalf("zero-length read %d", r.status)
	}
	// Above the advertised maximum transfer.
	if k, _ := senseKey(i.cmd(cdb16(0x88, 0, 4096), nil, 4096*512)); k != keyIllegal {
		t.Fatalf("oversized read: key %x", k)
	}
	i.logout()
}

// TestVD_SCSI_RoundTrip drives a real container through the server: the whole
// address space written and read back, then unaligned and sub-sector writes,
// then a reopen that must see the same bytes.
func TestVD_SCSI_RoundTrip(t *testing.T) {
	for _, p := range []Profile{ProfilePlain, ProfileFast} {
		t.Run(p.String(), func(t *testing.T) {
			const size = 8 << 20
			c, mb := newMemContainer(t, CreateOptions{LogicalSize: size, Profile: p, ClusterShift: 16})
			h, i := loggedIn(t, c, size, false)
			rng := rand.New(rand.NewSource(1))
			want := make([]byte, size)
			rng.Read(want)
			for off := 0; off < size; off += 256 << 10 {
				chunk := want[off : off+256<<10]
				if r := i.cmd(cdb10(0x2A, uint32(off/512), uint16(len(chunk)/512)), chunk, 0); r.status != statGood {
					t.Fatalf("write at %d: %d %x", off, r.status, r.sense)
				}
			}
			// Unaligned small writes, WRITE(16) and FUA.
			for k := 0; k < 200; k++ {
				n := 1 + rng.Intn(24)
				lba := rng.Intn(size/512 - n)
				buf := make([]byte, n*512)
				rng.Read(buf)
				copy(want[lba*512:], buf)
				c := cdb16(0x8A, uint64(lba), uint32(n))
				if k%7 == 0 {
					c[1] |= 0x08
				}
				if r := i.cmd(c, buf, 0); r.status != statGood {
					t.Fatalf("small write: %d", r.status)
				}
			}
			if r := i.cmd(cdb10(0x35, 0, 0), nil, 0); r.status != statGood {
				t.Fatalf("SYNCHRONIZE CACHE: %d", r.status)
			}
			got := make([]byte, 0, size)
			for off := 0; off < size; off += 128 << 10 {
				r := i.cmd(cdb10(0x28, uint32(off/512), 256), nil, 128<<10)
				if r.status != statGood || r.resFlags != 0 {
					t.Fatalf("read at %d: %d", off, r.status)
				}
				got = append(got, r.data...)
			}
			if !bytes.Equal(got, want) {
				t.Fatal("read back differs")
			}
			i.logout()
			h.srv.Close()
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			c2, err := openMem(mb, OpenRead)
			if err != nil {
				t.Fatal(err)
			}
			defer c2.Close()
			if !c2.Info().Clean {
				t.Fatal("not clean after close")
			}
			back := make([]byte, size)
			if _, err := c2.ReadAt(back, 0); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(back, want) {
				t.Fatal("the reopened container differs")
			}
		})
	}
}

func TestVD_SCSI_ReadOnly(t *testing.T) {
	const size = 1 << 20
	dev := &memDev{b: make([]byte, size)}
	_, i := loggedIn(t, dev, size, true)
	ms := make([]byte, 16)
	ms[0], ms[2], ms[4] = 0x1A, 0x08, 255
	if r := i.cmd(ms, nil, 255); r.data[2]&0x80 == 0 {
		t.Fatal("MODE SENSE does not report write protection")
	}
	big := bytes.Repeat([]byte{7}, 128<<10) // larger than the immediate burst
	r := i.cmd(cdb10(0x2A, 0, uint16(len(big)/512)), big, 0)
	if k, asc := senseKey(r); k != keyDataProtect || asc != ascWriteProtect || r.r2ts != 0 {
		t.Fatalf("write to a read-only disk: %x/%x, %d R2Ts", k, asc, r.r2ts)
	}
	if bytes.Contains(dev.b, []byte{7}) {
		t.Fatal("a read-only disk was written")
	}
	if r := i.cmd(cdb10(0x28, 0, 1), nil, 512); r.status != statGood {
		t.Fatal("read from a read-only disk failed")
	}
}

// blockingDev is a device whose Flush waits until released.
type blockingDev struct {
	memDev
	release chan struct{}
	synced  atomic.Int32
}

func (b *blockingDev) Flush() error {
	<-b.release
	b.synced.Add(1)
	return nil
}

// TestVD_Flush_Durability protects the one durability promise of spec 9:
// SYNCHRONIZE CACHE is answered only after the device's flush has completed.
func TestVD_Flush_Durability(t *testing.T) {
	const size = 1 << 20
	dev := &blockingDev{memDev: memDev{b: make([]byte, size)}, release: make(chan struct{})}
	_, i := loggedIn(t, dev, size, false)
	if r := i.cmd(cdb10(0x2A, 0, 8), bytes.Repeat([]byte{1}, 4096), 0); r.status != statGood {
		t.Fatal("write failed")
	}
	i.sendCmd(cdb10(0x35, 0, 0), nil, 0)
	i.nc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := i.readPDU(); err == nil {
		t.Fatal("SYNCHRONIZE CACHE was answered before the flush completed")
	}
	if dev.synced.Load() != 0 {
		t.Fatal("flush counted before release")
	}
	i.nc.SetReadDeadline(time.Time{})
	// The timed-out read may have consumed part of a header; nothing was sent,
	// so the reader is still aligned.
	close(dev.release)
	r := i.read()
	if r.bhs[0]&0x3f != opSCSIResp || r.bhs[3] != statGood {
		t.Fatalf("flush response: op %02x status %d", r.bhs[0], r.bhs[3])
	}
	if dev.synced.Load() != 1 {
		t.Fatalf("answered with %d completed flushes", dev.synced.Load())
	}
}

// TestVD_SCSI_Robust: malformed input closes the connection and never panics
// the server; the server still serves a proper session afterwards.
func TestVD_SCSI_Robust(t *testing.T) {
	h := serve(t, &memDev{b: make([]byte, 1<<20)}, 1<<20, false)
	cases := [][]byte{
		func() []byte { b := make([]byte, bhsLen); b[0] = opSCSICmd; return b }(),                           // before login
		func() []byte { b := make([]byte, bhsLen); b[0] = opLoginReq; setDataSegLen(b, 1<<23); return b }(), // oversized segment
		bytes.Repeat([]byte{0xff}, bhsLen),
	}
	for k, b := range cases {
		nc, err := net.Dial("tcp", h.addr)
		if err != nil {
			t.Fatal(err)
		}
		nc.Write(b)
		nc.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.ReadAll(nc); err != nil && !strings.Contains(err.Error(), "forcibly closed") {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatalf("case %d: the server kept a malformed connection open", k)
			}
		}
		nc.Close()
	}
	i := dial(t, h.addr)
	i.loginNormal(testIQN, testSecret)
	if r := i.cmd(cdb10(0x28, 0, 1), nil, 512); r.status != statGood {
		t.Fatal("no service after malformed input")
	}
}

func TestVD_ChapSecret(t *testing.T) {
	seen := map[string]bool{}
	for k := 0; k < 50; k++ {
		s, err := NewChapSecret()
		if err != nil {
			t.Fatal(err)
		}
		if len(s) != ChapSecretLen || seen[s] || !regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(s) {
			t.Fatalf("secret %q", s)
		}
		seen[s] = true
	}
	if _, err := NewServer(TargetConfig{IQN: "x", Device: &memDev{}, Size: 512, ChapSecret: "short"}); ExitClass(err) != ExitUsage {
		t.Fatalf("a short secret: %v", err)
	}
}

// TestVD_Abandon: a forced detach keeps every write but leaves the container
// marked not closed cleanly, even when the session wrote nothing.
func TestVD_Abandon(t *testing.T) {
	for _, write := range []bool{true, false} {
		c, mb := newMemContainer(t, CreateOptions{LogicalSize: 4 << 20, Profile: ProfilePlain, ClusterShift: 16})
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		c, err := openMem(mb, OpenMount)
		if err != nil {
			t.Fatal(err)
		}
		if write {
			if _, err := c.WriteAt(bytes.Repeat([]byte{9}, 70000), 12345); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Abandon(); err != nil {
			t.Fatal(err)
		}
		r, err := openMem(mb, OpenRead)
		if err != nil {
			t.Fatal(err)
		}
		if r.Info().Clean {
			t.Fatalf("write=%v: clean after Abandon", write)
		}
		got := make([]byte, 70000)
		r.ReadAt(got, 12345)
		if write && !bytes.Equal(got, bytes.Repeat([]byte{9}, 70000)) {
			t.Fatal("an abandoned container lost a write")
		}
		r.Close()
	}
}
