package vdisk

import (
	"bufio"
	"crypto/md5"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The block server: a minimal iSCSI target (RFC 7143) that serves one
// BlockDevice as LUN 0 of one target, to Windows' own initiator, on loopback.
//
// It implements the subset the SP-0004 S0 measurement recorded the Windows
// initiator using: login with a fixed negotiation, SendTargets discovery,
// immediate data plus one outstanding R2T, Data-In with the status collapsed
// into the last PDU, NOP, task management answered "complete", logout, and the
// SCSI commands of scsi.go. Everything else is refused.
//
// Commands of one connection execute one at a time, in the order their data
// completes (S0 requirement 1: the concurrent variant halved reads and produced
// an unexplained "device is not ready").
//
// Access. A loopback socket is reachable by every local process, of every
// user. A normal session therefore needs CHAP with a secret drawn for this
// mount and handed only to the elevated step that logs the initiator in, and a
// target accepts one normal session at a time. Discovery needs no
// authentication and reveals the target name and port, nothing else.

// loopbackAddr is the only address the block server binds: loopback, with a
// port the operating system chooses. It is a constant, and nothing in this
// package accepts an address from a caller (TestVD_BindAddress).
const loopbackAddr = "127.0.0.1:0"

// Negotiated limits (S0 section 4.2: the Windows initiator accepts all of them).
const (
	ourMaxRecvSegment = 262144 // largest data segment we accept
	ourMaxBurst       = 262144
	ourFirstBurst     = 65536
	cmdWindow         = 32 // MaxCmdSN - ExpCmdSN + 1
	maxPendingWrites  = 32 // writes waiting for Data-Out on one connection
	loginTimeout      = 30 * time.Second
)

// ChapSecretLen is the length of a mount's CHAP secret. The Windows initiator
// takes 12 to 16 bytes.
const ChapSecretLen = 16

// NewChapSecret draws a CHAP secret: ChapSecretLen characters from a 62-letter
// alphabet, about 95 bits.
func NewChapSecret() (string, error) {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, ChapSecretLen)
	for i := range b {
		var r [1]byte
		for {
			if _, err := io.ReadFull(rand.Reader, r[:]); err != nil {
				return "", ioErr(err)
			}
			if int(r[0]) < 248 { // 248 = 4*62: no modulo bias
				break
			}
		}
		b[i] = alphabet[int(r[0])%len(alphabet)]
	}
	return string(b), nil
}

// TargetConfig describes the one target a server serves.
type TargetConfig struct {
	IQN           string
	Device        BlockDevice
	Size          int64  // bytes, a multiple of 512
	PhysicalBlock int    // the crypto sector size
	ReadOnly      bool   // write-protected above the file system too
	Identity      string // the container id: the disk's serial and NAA derive from it
	ChapSecret    string // required for a normal session
	// WriteBack answers SYNCHRONIZE CACHE and forced-unit-access writes
	// without flushing the device: a ram container, whose file follows at its
	// save policy's pace and whose loss on a crash is what was written since
	// the last completed save (spec 9). Never set for any other profile.
	WriteBack bool
	// Logf receives one line per connection event. It must not block.
	Logf func(format string, args ...interface{})
}

// Server is a listening block server.
type Server struct {
	cfg TargetConfig
	lun *lun
	ln  net.Listener

	mu      sync.Mutex
	conns   map[*conn]bool
	session *conn // the logged-in normal session, if any
	closed  bool
	tsih    uint16
	wg      sync.WaitGroup
}

// NewServer validates cfg and binds loopbackAddr.
func NewServer(cfg TargetConfig) (*Server, error) {
	if cfg.IQN == "" || cfg.Device == nil {
		return nil, usagef("a target needs a name and a device")
	}
	if n := len(cfg.ChapSecret); n < 12 || n > 16 {
		return nil, usagef("a CHAP secret is 12 to 16 bytes, not %d", n)
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...interface{}) {}
	}
	l, err := newLUN(cfg.Device, cfg.Size, cfg.PhysicalBlock, cfg.ReadOnly, cfg.Identity)
	if err != nil {
		return nil, err
	}
	l.logf = cfg.Logf
	l.writeBack = cfg.WriteBack
	ln, err := net.Listen("tcp", loopbackAddr)
	if err != nil {
		return nil, ioErr(err)
	}
	return &Server{cfg: cfg, lun: l, ln: ln, conns: map[*conn]bool{}}, nil
}

// Addr is the bound address, 127.0.0.1 and the chosen port.
func (s *Server) Addr() *net.TCPAddr { return s.ln.Addr().(*net.TCPAddr) }

// SessionActive reports whether a normal session is logged in now.
func (s *Server) SessionActive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.session != nil
}

// Serve accepts connections until Close. It returns nil after Close.
func (s *Server) Serve() error {
	for {
		nc, err := s.ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return nil
			}
			return ioErr(err)
		}
		if tc, ok := nc.(*net.TCPConn); ok {
			_ = tc.SetNoDelay(true)
		}
		c := &conn{s: s, nc: nc, r: bufio.NewReaderSize(nc, 1<<20), w: bufio.NewWriterSize(nc, 1<<20),
			id: nc.RemoteAddr().String(), pending: map[uint32]*writeTask{},
			initMRDSL: 8192, maxBurst: ourMaxBurst, firstBurst: ourFirstBurst, immediate: true}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			nc.Close()
			return nil
		}
		s.conns[c] = true
		s.wg.Add(1)
		s.mu.Unlock()
		go func() {
			defer s.wg.Done()
			c.serve()
		}()
	}
}

// Close stops listening, drops every connection and waits for their
// goroutines. Commands already executing finish first; nothing is flushed
// here - the owner of the device flushes and closes it after Close returns.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	err := s.ln.Close()
	for c := range s.conns {
		c.nc.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
	return err
}

func (s *Server) logf(format string, args ...interface{}) { s.cfg.Logf(format, args...) }

// claimSession makes c the normal session, or reports that one exists.
func (s *Server) claimSession(c *conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil && s.session != c {
		return false
	}
	s.session = c
	return true
}

func (s *Server) nextTSIH() uint16 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tsih++
	if s.tsih == 0 {
		s.tsih = 1
	}
	return s.tsih
}

func (s *Server) drop(c *conn) {
	s.mu.Lock()
	delete(s.conns, c)
	if s.session == c {
		s.session = nil
	}
	s.mu.Unlock()
}

// ---------------------------------------------------------------- connection

type writeTask struct {
	itt      uint32
	cdb      [16]byte
	buf      []byte
	received int
	burstEnd int
	ttt      uint32
	r2tsn    uint32
}

type conn struct {
	s  *Server
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer
	id string

	// Login state.
	loginBuf   []byte
	started    bool
	discovery  bool
	normal     bool
	authMethod string // "", "None" or "CHAP", once chosen
	authed     bool
	chapID     byte
	chapChal   []byte
	ffp        bool
	sentTPGT   bool
	declMRDSL  bool
	isid       [6]byte

	// Sequence numbers.
	statSN   uint32
	expCmdSN uint32

	// Negotiated.
	initMRDSL  int
	maxBurst   int
	firstBurst int
	immediate  bool

	pending map[uint32]*writeTask
	nextTTT uint32
}

var errLogout = errors.New("logout")

func (c *conn) logf(format string, args ...interface{}) {
	c.s.logf("%s %s", c.id, fmt.Sprintf(format, args...))
}

func (c *conn) serve() {
	c.logf("connect")
	_ = c.nc.SetReadDeadline(time.Now().Add(loginTimeout))
	err := c.loop()
	c.w.Flush()
	c.nc.Close()
	c.s.drop(c)
	switch {
	case err == nil:
		c.logf("closed after logout")
	case errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed):
		c.logf("closed")
	default:
		c.logf("closed: %v", err)
	}
}

func (c *conn) loop() error {
	var bhs [bhsLen]byte
	for {
		if c.r.Buffered() == 0 {
			if err := c.w.Flush(); err != nil {
				return err
			}
		}
		if _, err := io.ReadFull(c.r, bhs[:]); err != nil {
			return err
		}
		dlen := dataSegLen(bhs[:])
		if dlen > ourMaxRecvSegment {
			return fmt.Errorf("a %d-byte data segment exceeds the %d bytes negotiated", dlen, ourMaxRecvSegment)
		}
		op := bhs[0] & 0x3f
		if !c.ffp && op != opLoginReq {
			return fmt.Errorf("opcode 0x%02x before the login completed", op)
		}
		if ahs := int(bhs[4]) * 4; ahs > 0 {
			if _, err := io.CopyN(io.Discard, c.r, int64(ahs)); err != nil {
				return err
			}
		}
		var err error
		switch op {
		case opSCSICmd:
			err = c.scsiCmd(&bhs, dlen)
		case opDataOut:
			err = c.dataOut(&bhs, dlen)
		default:
			data := make([]byte, dlen)
			if err = readFullPadded(c.r, data); err != nil {
				return err
			}
			switch op {
			case opLoginReq:
				err = c.login(&bhs, data)
			case opTextReq:
				err = c.text(&bhs, data)
			case opNopOut:
				err = c.nopOut(&bhs, data)
			case opLogoutReq:
				err = c.logout(&bhs)
			case opTaskMgmt:
				err = c.taskMgmt(&bhs)
			default:
				err = c.reject(&bhs, 0x04) // command not supported
			}
		}
		if err != nil {
			if errors.Is(err, errLogout) {
				return nil
			}
			return err
		}
	}
}

func (c *conn) send(bhs []byte, data []byte) error {
	return writePDU(c.w, bhs, data)
}

// fillSN sets StatSN, ExpCmdSN and MaxCmdSN; a PDU that carries status
// consumes a StatSN.
func (c *conn) fillSN(b []byte, withStatus bool) {
	put32(b[24:], c.statSN)
	if withStatus {
		c.statSN++
	}
	put32(b[28:], c.expCmdSN)
	put32(b[32:], c.expCmdSN+cmdWindow-1)
}

func (c *conn) advanceCmdSN(bhs *[bhsLen]byte) {
	if bhs[0]&0x40 != 0 { // immediate delivery
		return
	}
	if sn := be32(bhs[24:]); int32(sn-c.expCmdSN) >= 0 {
		c.expCmdSN = sn + 1
	}
}

// ---------------------------------------------------------------- login

func (c *conn) login(bhs *[bhsLen]byte, data []byte) error {
	if c.ffp {
		return errors.New("a login request after the login completed")
	}
	flags := bhs[1]
	transit := flags&0x80 != 0
	cont := flags&0x40 != 0
	csg := (flags >> 2) & 3
	nsg := flags & 3
	if !c.started {
		c.started = true
		c.expCmdSN = be32(bhs[24:])
		c.statSN = be32(bhs[28:])
		copy(c.isid[:], bhs[8:14])
	}
	if len(c.loginBuf)+len(data) > 64<<10 {
		return errors.New("login text over 64 KiB")
	}
	c.loginBuf = append(c.loginBuf, data...)
	resp := make([]byte, bhsLen)
	resp[0] = opLoginResp
	copy(resp[8:14], bhs[8:14])
	copy(resp[16:20], bhs[16:20])
	if cont {
		resp[1] = csg << 2
		c.fillSN(resp, true)
		return c.send(resp, nil)
	}
	keys := parseKeys(c.loginBuf)
	c.loginBuf = nil
	c.logf("login csg=%d nsg=%d T=%v keys: %s", csg, nsg, transit, keysText(keys))

	out, class, detail := c.loginStage(csg, keys)
	// The target moves on only once the stage's own work is done: security
	// needs a chosen method and, for CHAP, a verified response.
	ready := class == 0 && transit && (csg != 0 || c.authed)
	var rflags byte = csg << 2
	if ready {
		rflags |= 0x80 | nsg
	}
	resp[1] = rflags
	if ready && nsg == 3 {
		tsih := be16(bhs[14:])
		if tsih == 0 {
			tsih = c.s.nextTSIH()
		}
		put16(resp[14:], tsih)
		if c.normal && !c.s.claimSession(c) {
			class, detail = 0x02, 0x02 // authorization failure: one session per target
			out = nil
		} else {
			c.ffp = true
		}
	}
	resp[36], resp[37] = class, detail
	c.fillSN(resp, true)
	if class != 0 {
		c.logf("login refused %d/%d", class, detail)
		c.send(resp, nil)
		c.w.Flush()
		return fmt.Errorf("login refused %d/%d", class, detail)
	}
	if err := c.send(resp, encodeKeys(out)); err != nil {
		return err
	}
	if c.ffp {
		_ = c.nc.SetReadDeadline(time.Time{})
		c.logf("logged in: discovery=%v MRDSL=%d MaxBurst=%d FirstBurst=%d ImmediateData=%v",
			c.discovery, c.initMRDSL, c.maxBurst, c.firstBurst, c.immediate)
	}
	return nil
}

// loginStage answers one stage's keys. It returns the keys to send back and a
// status class and detail (0, 0 = go on).
func (c *conn) loginStage(csg byte, keys []kv) ([]kv, byte, byte) {
	get := func(name string) (string, bool) {
		for _, k := range keys {
			if k.K == name {
				return k.V, true
			}
		}
		return "", false
	}
	if st, ok := get("SessionType"); ok {
		c.discovery = st == "Discovery"
	}
	if tn, ok := get("TargetName"); ok {
		if tn != c.s.cfg.IQN {
			return nil, 0x02, 0x03 // not found
		}
		c.normal = true
	}
	if c.discovery {
		c.normal = false
	}
	if !c.discovery && !c.normal && csg == 0 && c.authMethod == "" {
		return nil, 0x02, 0x07 // missing parameter: a normal session names its target
	}
	var out []kv
	if csg == 0 {
		o, class, detail := c.security(keys)
		if class != 0 {
			return nil, class, detail
		}
		out = append(out, o...)
	} else {
		if !c.authed {
			if !c.discovery || c.authMethod != "" {
				return nil, 0x02, 0x01 // a normal session must pass the security stage
			}
			c.authed = true // discovery may skip it (RFC 7143 section 6.3)
		}
		for _, k := range keys {
			if r, ok := c.negotiate(k); ok {
				out = append(out, r)
			}
		}
		if !c.declMRDSL {
			out = append(out, kv{"MaxRecvDataSegmentLength", strconv.Itoa(ourMaxRecvSegment)})
			c.declMRDSL = true
		}
	}
	if !c.discovery && !c.sentTPGT {
		out = append(out, kv{"TargetPortalGroupTag", "1"})
		c.sentTPGT = true
	}
	return out, 0, 0
}

// security runs the security stage: AuthMethod, then CHAP (RFC 7143 section
// 12.1.3, MD5 only). Discovery takes None; a normal session must use CHAP.
func (c *conn) security(keys []kv) ([]kv, byte, byte) {
	var out []kv
	for _, k := range keys {
		switch k.K {
		case "AuthMethod":
			if c.authMethod != "" {
				continue
			}
			switch {
			case c.normal && listHas(k.V, "CHAP"):
				c.authMethod = "CHAP"
			case c.discovery && listHas(k.V, "None"):
				c.authMethod = "None"
				c.authed = true
			default:
				return nil, 0x02, 0x01 // authentication failure
			}
			out = append(out, kv{"AuthMethod", c.authMethod})
		case "CHAP_A":
			if c.authMethod != "CHAP" || c.chapChal != nil || !listHas(k.V, "5") {
				return nil, 0x02, 0x01
			}
			var id [1]byte
			chal := make([]byte, 16)
			if _, err := io.ReadFull(rand.Reader, id[:]); err != nil {
				return nil, 0x03, 0x00 // target error
			}
			if _, err := io.ReadFull(rand.Reader, chal); err != nil {
				return nil, 0x03, 0x00
			}
			c.chapID, c.chapChal = id[0], chal
			out = append(out, kv{"CHAP_A", "5"}, kv{"CHAP_I", strconv.Itoa(int(id[0]))},
				kv{"CHAP_C", "0x" + hex.EncodeToString(chal)})
		case "CHAP_R":
			if c.chapChal == nil || c.authed {
				return nil, 0x02, 0x01
			}
			got, ok := decodeChapBinary(k.V)
			h := md5.New()
			h.Write([]byte{c.chapID})
			h.Write([]byte(c.s.cfg.ChapSecret))
			h.Write(c.chapChal)
			if !ok || subtle.ConstantTimeCompare(got, h.Sum(nil)) != 1 {
				return nil, 0x02, 0x01
			}
			c.authed = true
		}
	}
	if c.normal && c.authMethod == "" {
		return nil, 0x02, 0x01 // a normal session that offered no CHAP
	}
	return out, 0, 0
}

// decodeChapBinary reads a CHAP binary value: 0x hex or 0b base64.
func decodeChapBinary(v string) ([]byte, bool) {
	switch {
	case strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X"):
		b, err := hex.DecodeString(v[2:])
		return b, err == nil
	case strings.HasPrefix(v, "0b") || strings.HasPrefix(v, "0B"):
		b, err := base64.StdEncoding.DecodeString(v[2:])
		return b, err == nil
	}
	return nil, false
}

func minKey(v string, ours int) (int, string) {
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 || ours < n {
		n = ours
	}
	return n, strconv.Itoa(n)
}

func (c *conn) negotiate(k kv) (kv, bool) {
	switch k.K {
	case "InitiatorName", "InitiatorAlias", "SessionType", "TargetName":
		return kv{}, false
	case "HeaderDigest", "DataDigest":
		return kv{k.K, "None"}, true
	case "MaxConnections":
		return kv{k.K, "1"}, true
	case "ErrorRecoveryLevel":
		return kv{k.K, "0"}, true
	case "InitialR2T":
		return kv{k.K, "Yes"}, true // OR: ours wins
	case "ImmediateData":
		c.immediate = k.V == "Yes" // AND
		return kv{k.K, k.V}, true
	case "MaxRecvDataSegmentLength":
		if n, err := strconv.Atoi(k.V); err == nil && n >= 512 && n <= 1<<24-1 {
			c.initMRDSL = n
		}
		c.declMRDSL = true
		return kv{k.K, strconv.Itoa(ourMaxRecvSegment)}, true
	case "MaxBurstLength":
		var s string
		c.maxBurst, s = minKey(k.V, ourMaxBurst)
		return kv{k.K, s}, true
	case "FirstBurstLength":
		var s string
		c.firstBurst, s = minKey(k.V, ourFirstBurst)
		return kv{k.K, s}, true
	case "MaxOutstandingR2T":
		return kv{k.K, "1"}, true
	case "DefaultTime2Wait":
		return kv{k.K, k.V}, true
	case "DefaultTime2Retain":
		return kv{k.K, "0"}, true
	case "DataPDUInOrder", "DataSequenceInOrder":
		return kv{k.K, "Yes"}, true
	case "IFMarker", "OFMarker":
		return kv{k.K, "No"}, true
	case "IFMarkInt", "OFMarkInt":
		return kv{k.K, "Irrelevant"}, true
	}
	return kv{k.K, "NotUnderstood"}, true
}

// ---------------------------------------------------------------- text, nop, logout, task management, reject

func (c *conn) text(bhs *[bhsLen]byte, data []byte) error {
	c.advanceCmdSN(bhs)
	var out []kv
	for _, k := range parseKeys(data) {
		if k.K == "SendTargets" && c.discovery {
			out = append(out, kv{"TargetName", c.s.cfg.IQN}, kv{"TargetAddress", c.nc.LocalAddr().String() + ",1"})
		}
	}
	resp := make([]byte, bhsLen)
	resp[0] = opTextResp
	resp[1] = 0x80
	copy(resp[8:16], bhs[8:16])
	copy(resp[16:20], bhs[16:20])
	put32(resp[20:], 0xffffffff)
	c.fillSN(resp, true)
	return c.send(resp, encodeKeys(out))
}

func (c *conn) nopOut(bhs *[bhsLen]byte, data []byte) error {
	c.advanceCmdSN(bhs)
	if be32(bhs[16:]) == 0xffffffff { // a ping answering ours: no reply
		return nil
	}
	resp := make([]byte, bhsLen)
	resp[0] = opNopIn
	resp[1] = 0x80
	copy(resp[8:16], bhs[8:16])
	copy(resp[16:20], bhs[16:20])
	put32(resp[20:], 0xffffffff)
	c.fillSN(resp, true)
	return c.send(resp, data)
}

func (c *conn) logout(bhs *[bhsLen]byte) error {
	c.advanceCmdSN(bhs)
	c.pending = map[uint32]*writeTask{}
	resp := make([]byte, bhsLen)
	resp[0] = opLogoutResp
	resp[1] = 0x80
	copy(resp[16:20], bhs[16:20])
	c.fillSN(resp, true)
	c.logf("logout, reason %d", bhs[1]&0x7f)
	if err := c.send(resp, nil); err != nil {
		return err
	}
	if err := c.w.Flush(); err != nil {
		return err
	}
	return errLogout
}

func (c *conn) taskMgmt(bhs *[bhsLen]byte) error {
	c.advanceCmdSN(bhs)
	fn := bhs[1] & 0x7f
	c.logf("task management function %d", fn)
	// Commands execute to completion as they arrive; only writes still waiting
	// for Data-Out can be aborted.
	if fn == 1 { // ABORT TASK
		delete(c.pending, be32(bhs[20:]))
	} else {
		c.pending = map[uint32]*writeTask{}
	}
	resp := make([]byte, bhsLen)
	resp[0] = opTaskResp
	resp[1] = 0x80
	copy(resp[16:20], bhs[16:20])
	c.fillSN(resp, true)
	return c.send(resp, nil)
}

func (c *conn) reject(bhs *[bhsLen]byte, reason byte) error {
	c.logf("reject opcode 0x%02x, reason 0x%02x", bhs[0]&0x3f, reason)
	resp := make([]byte, bhsLen)
	resp[0] = opReject
	resp[1] = 0x80
	resp[2] = reason
	put32(resp[16:], 0xffffffff)
	c.fillSN(resp, true)
	return c.send(resp, append([]byte(nil), bhs[:]...))
}

// ---------------------------------------------------------------- SCSI

func (c *conn) scsiCmd(bhs *[bhsLen]byte, dlen int) error {
	if c.discovery {
		return errors.New("a SCSI command on a discovery session")
	}
	c.advanceCmdSN(bhs)
	itt := be32(bhs[16:])
	edtl := int(be32(bhs[20:]))
	var cdb [16]byte
	copy(cdb[:], bhs[32:48])
	write := bhs[1]&0x20 != 0

	imm := make([]byte, dlen)
	if err := readFullPadded(c.r, imm); err != nil {
		return err
	}
	if n := transferLen(cdb[:]); edtl > maxTransfer || n > maxTransfer {
		return c.respond(itt, edtl, write, checkCond(keyIllegal, ascInvField, 0))
	}
	if !write || edtl == 0 {
		return c.execute(itt, cdb, nil, edtl, false)
	}
	if dlen > edtl || (dlen > 0 && !c.immediate) || dlen > c.firstBurst {
		return errors.New("immediate data beyond what was negotiated")
	}
	if transferLen(cdb[:]) >= 0 && c.s.lun.readOnly {
		// A write to a write-protected disk fails without asking for its data.
		return c.respond(itt, edtl, true, checkCond(keyDataProtect, ascWriteProtect, 0))
	}
	buf := make([]byte, edtl)
	copy(buf, imm)
	if dlen >= edtl {
		return c.execute(itt, cdb, buf, edtl, true)
	}
	if len(c.pending) >= maxPendingWrites {
		return errors.New("too many writes waiting for data")
	}
	wt := &writeTask{itt: itt, cdb: cdb, buf: buf, received: dlen}
	c.pending[itt] = wt
	return c.sendR2T(wt)
}

func (c *conn) sendR2T(wt *writeTask) error {
	n := min(len(wt.buf)-wt.received, c.maxBurst)
	c.nextTTT++
	if c.nextTTT == 0xffffffff {
		c.nextTTT = 1
	}
	wt.ttt = c.nextTTT
	wt.burstEnd = wt.received + n
	resp := make([]byte, bhsLen)
	resp[0] = opR2T
	resp[1] = 0x80
	put32(resp[16:], wt.itt)
	put32(resp[20:], wt.ttt)
	c.fillSN(resp, false)
	put32(resp[36:], wt.r2tsn)
	put32(resp[40:], uint32(wt.received))
	put32(resp[44:], uint32(n))
	wt.r2tsn++
	return c.send(resp, nil)
}

func (c *conn) dataOut(bhs *[bhsLen]byte, dlen int) error {
	itt := be32(bhs[16:])
	off := int(be32(bhs[40:]))
	wt := c.pending[itt]
	if wt == nil || be32(bhs[20:]) != wt.ttt || off != wt.received || off+dlen > wt.burstEnd {
		// Data for a write that was aborted, or out of order (DataPDUInOrder
		// was negotiated Yes): discard it.
		_, err := io.CopyN(io.Discard, c.r, int64(dlen+pad4(dlen)))
		return err
	}
	if err := readFullPadded(c.r, wt.buf[off:off+dlen]); err != nil {
		return err
	}
	if off+dlen > wt.received {
		wt.received = off + dlen
	}
	if bhs[1]&0x80 == 0 { // not the burst's last PDU
		return nil
	}
	if wt.received < len(wt.buf) {
		return c.sendR2T(wt)
	}
	delete(c.pending, itt)
	return c.execute(itt, wt.cdb, wt.buf, len(wt.buf), true)
}

func (c *conn) execute(itt uint32, cdb [16]byte, dataOut []byte, edtl int, write bool) error {
	return c.respond(itt, edtl, write, c.s.lun.exec(cdb[:], dataOut))
}

// respond sends Data-In (with the status in its last PDU) or a SCSI Response.
func (c *conn) respond(itt uint32, edtl int, write bool, res result) error {
	data := res.data
	var resFlag byte
	var residual int
	if !write {
		if len(data) > edtl {
			resFlag, residual = 0x04, len(data)-edtl // overflow
			data = data[:edtl]
		} else if len(data) < edtl {
			resFlag, residual = 0x02, edtl-len(data) // underflow
		}
	}
	if res.status == statGood && len(data) > 0 {
		return c.dataIn(itt, data, resFlag, residual)
	}
	resp := make([]byte, bhsLen)
	resp[0] = opSCSIResp
	resp[1] = 0x80 | resFlag
	resp[3] = res.status
	put32(resp[16:], itt)
	c.fillSN(resp, true)
	put32(resp[44:], uint32(residual))
	var sense []byte
	if len(res.sense) > 0 {
		sense = make([]byte, 2+len(res.sense))
		put16(sense, uint16(len(res.sense)))
		copy(sense[2:], res.sense)
	}
	return c.send(resp, sense)
}

func (c *conn) dataIn(itt uint32, data []byte, resFlag byte, residual int) error {
	ch := c.initMRDSL
	var sn uint32
	for off := 0; off < len(data); off += ch {
		end := min(off+ch, len(data))
		last := end == len(data)
		bhs := make([]byte, bhsLen)
		bhs[0] = opDataIn
		var f byte
		if last || end%c.maxBurst == 0 {
			f |= 0x80
		}
		if last {
			f |= 0x01 | resFlag // status travels with the last PDU
		}
		bhs[1] = f
		put32(bhs[16:], itt)
		put32(bhs[20:], 0xffffffff)
		c.fillSN(bhs, last)
		put32(bhs[36:], sn)
		put32(bhs[40:], uint32(off))
		if last {
			put32(bhs[44:], uint32(residual))
		}
		sn++
		if err := c.send(bhs, data[off:end]); err != nil {
			return err
		}
	}
	return nil
}
