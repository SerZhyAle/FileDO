package vdisk

import (
	"encoding/binary"
	"io"
	"strings"
)

// The iSCSI wire format (RFC 7143 sections 11 and 13) the block server needs:
// the 48-byte basic header segment, the padded data segment, and the
// key=value text of login and text requests. Nothing here knows SCSI.

const bhsLen = 48

// Initiator opcodes (RFC 7143 section 11.1.1).
const (
	opNopOut    = 0x00
	opSCSICmd   = 0x01
	opTaskMgmt  = 0x02
	opLoginReq  = 0x03
	opTextReq   = 0x04
	opDataOut   = 0x05
	opLogoutReq = 0x06
)

// Target opcodes.
const (
	opNopIn      = 0x20
	opSCSIResp   = 0x21
	opTaskResp   = 0x22
	opLoginResp  = 0x23
	opTextResp   = 0x24
	opDataIn     = 0x25
	opLogoutResp = 0x26
	opR2T        = 0x31
	opReject     = 0x3f
)

func be16(b []byte) uint16     { return binary.BigEndian.Uint16(b) }
func be32(b []byte) uint32     { return binary.BigEndian.Uint32(b) }
func be64(b []byte) uint64     { return binary.BigEndian.Uint64(b) }
func put16(b []byte, v uint16) { binary.BigEndian.PutUint16(b, v) }
func put32(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }
func put64(b []byte, v uint64) { binary.BigEndian.PutUint64(b, v) }

func dataSegLen(bhs []byte) int {
	return int(bhs[5])<<16 | int(bhs[6])<<8 | int(bhs[7])
}

func setDataSegLen(bhs []byte, n int) {
	bhs[5] = byte(n >> 16)
	bhs[6] = byte(n >> 8)
	bhs[7] = byte(n)
}

func pad4(n int) int { return (4 - n%4) % 4 }

// writePDU writes a header and an optional data segment with its padding.
func writePDU(w io.Writer, bhs []byte, data []byte) error {
	setDataSegLen(bhs, len(data))
	if _, err := w.Write(bhs[:bhsLen]); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if p := pad4(len(data)); p > 0 {
		var z [4]byte
		if _, err := w.Write(z[:p]); err != nil {
			return err
		}
	}
	return nil
}

// readFullPadded reads exactly len(dst) bytes and the padding after them.
func readFullPadded(r io.Reader, dst []byte) error {
	if _, err := io.ReadFull(r, dst); err != nil {
		return err
	}
	if p := pad4(len(dst)); p > 0 {
		var z [4]byte
		if _, err := io.ReadFull(r, z[:p]); err != nil {
			return err
		}
	}
	return nil
}

// kv is one text key and its value.
type kv struct{ K, V string }

func parseKeys(b []byte) []kv {
	var out []kv
	for _, part := range strings.Split(string(b), "\x00") {
		if part == "" {
			continue
		}
		if i := strings.IndexByte(part, '='); i >= 0 {
			out = append(out, kv{part[:i], part[i+1:]})
		} else {
			out = append(out, kv{part, ""})
		}
	}
	return out
}

func encodeKeys(keys []kv) []byte {
	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k.K)
		sb.WriteByte('=')
		sb.WriteString(k.V)
		sb.WriteByte(0)
	}
	return []byte(sb.String())
}

// keysText prints keys for the log, with every CHAP value cut: the challenge
// and the response are not secrets, but nothing about the handshake belongs in
// a log a user may attach to a report.
func keysText(keys []kv) string {
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := k.V
		if strings.HasPrefix(k.K, "CHAP_") && k.K != "CHAP_A" {
			v = "<cut>"
		}
		parts = append(parts, k.K+"="+v)
	}
	return strings.Join(parts, " ")
}

func listHas(list, v string) bool {
	for _, x := range strings.Split(list, ",") {
		if x == v {
			return true
		}
	}
	return false
}
