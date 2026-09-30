package vdisk

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
)

// BlockDevice is the transport seam of the SP-0004 specification, section 8:
// read at an offset, write at an offset, flush. *Container is one; nothing
// below this interface knows a socket exists, and nothing above it knows the
// container's layout.
type BlockDevice interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	// Flush returns only when every write it follows is durable. The block
	// server answers SYNCHRONIZE CACHE after Flush returns, never before.
	Flush() error
}

// The logical block the disk presents. 512 bytes with the crypto sector as the
// physical block ("512e") is the geometry every Windows tool and every image
// format accepts; a write smaller than a crypto sector is a read-modify-write
// inside the container, and NTFS, told the physical block is 4096, rarely
// issues one.
const logicalBlock = 512

// maxTransfer bounds one command's data. It is what VPD page B0 advertises and
// what the server refuses above, so a caller on the socket can never make it
// allocate more than this per command.
const maxTransfer = 1 << 20

// SCSI status, sense keys and additional sense codes used here (SPC-4).
const (
	statGood  = 0x00
	statCheck = 0x02

	keyNotReady    = 0x02
	keyMedium      = 0x03
	keyIllegal     = 0x05
	keyDataProtect = 0x07

	ascInvOpcode    = 0x20
	ascLBARange     = 0x21
	ascInvField     = 0x24
	ascWriteProtect = 0x27
	ascWriteError   = 0x0C
	ascReadError    = 0x11
)

// lun is the one logical unit (LUN 0) a target serves.
type lun struct {
	dev      BlockDevice
	blocks   uint64
	physExp  uint8 // log2(physical block / logical block)
	readOnly bool
	// writeBack: a flush is answered from memory (TargetConfig.WriteBack).
	writeBack bool
	serial    string
	naa       [8]byte
	logf      func(format string, args ...interface{})
}

// newLUN presents dev as a disk of size bytes. identity (the container id)
// gives the disk the same serial number and NAA identifier on every mount, so
// Windows recognises the disk it saw before and keeps its drive letter.
func newLUN(dev BlockDevice, size int64, physBlock int, readOnly bool, identity string) (*lun, error) {
	if size <= 0 || size%logicalBlock != 0 {
		return nil, usagef("a disk of %d bytes is not a whole number of %d-byte blocks", size, logicalBlock)
	}
	l := &lun{dev: dev, blocks: uint64(size / logicalBlock), readOnly: readOnly, logf: func(string, ...interface{}) {}}
	for b := physBlock; b > logicalBlock && l.physExp < 15; b >>= 1 {
		l.physExp++
	}
	sum := diskIdentity(identity)
	l.serial = DiskSerial(identity)
	copy(l.naa[:], sum[10:18])
	l.naa[0] = 0x30 | l.naa[0]&0x0f // NAA 3: locally assigned
	return l, nil
}

func diskIdentity(identity string) [32]byte {
	return sha256.Sum256([]byte("FileDO FDD disk identity\x00" + identity))
}

// DiskSerial is the serial number the disk of the container with this id
// reports (VPD page 0x80): the way the mounting side finds its disk among the
// machine's disks.
func DiskSerial(identity string) string {
	sum := diskIdentity(identity)
	return fmt.Sprintf("FDD%X", sum[:10])
}

type result struct {
	data   []byte
	status byte
	sense  []byte
}

func good(data []byte) result { return result{data: data} }

func checkCond(key, asc, ascq byte) result {
	s := make([]byte, 18) // fixed format, current error
	s[0] = 0x70
	s[2] = key
	s[7] = 10
	s[12] = asc
	s[13] = ascq
	return result{status: statCheck, sense: s}
}

func trunc(b []byte, alloc int) []byte {
	if len(b) > alloc {
		return b[:alloc]
	}
	return b
}

func padStr(s string, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	copy(b, s)
	return b
}

// rwDecode decodes READ/WRITE (10) and (16), the only transfer commands the
// Windows initiator issued in the S0 measurement.
func rwDecode(cdb []byte) (lba uint64, n uint32, fua, write, ok bool) {
	switch cdb[0] {
	case 0x28, 0x2A:
		lba, n, write = uint64(be32(cdb[2:])), uint32(be16(cdb[7:])), cdb[0] == 0x2A
	case 0x88, 0x8A:
		lba, n, write = be64(cdb[2:]), be32(cdb[10:]), cdb[0] == 0x8A
	default:
		return 0, 0, false, false, false
	}
	return lba, n, cdb[1]&0x08 != 0, write, true
}

// transferLen is the byte count a command moves, or -1 when the command is
// not a transfer. The connection uses it to refuse an oversized command before
// it allocates anything.
func transferLen(cdb []byte) int64 {
	if _, n, _, _, ok := rwDecode(cdb); ok {
		return int64(n) * logicalBlock
	}
	return -1
}

// exec runs one CDB. dataOut is the complete Data-Out payload of a write.
func (l *lun) exec(cdb []byte, dataOut []byte) result {
	if lba, n, fua, write, ok := rwDecode(cdb); ok {
		return l.readWrite(lba, n, fua, write, dataOut)
	}
	switch cdb[0] {
	case 0x00: // TEST UNIT READY
		return good(nil)
	case 0x03: // REQUEST SENSE: nothing is ever pending
		s := make([]byte, 18)
		s[0], s[7] = 0x70, 10
		return good(trunc(s, int(cdb[4])))
	case 0x12:
		return l.inquiry(cdb)
	case 0x25: // READ CAPACITY (10)
		b := make([]byte, 8)
		last := l.blocks - 1
		if last > 0xffffffff {
			last = 0xffffffff
		}
		put32(b, uint32(last))
		put32(b[4:], logicalBlock)
		return good(b)
	case 0x9E: // SERVICE ACTION IN (16)
		if cdb[1]&0x1f != 0x10 {
			return checkCond(keyIllegal, ascInvField, 0)
		}
		b := make([]byte, 32) // READ CAPACITY (16)
		put64(b, l.blocks-1)
		put32(b[8:], logicalBlock)
		b[13] = l.physExp & 0x0f
		return good(trunc(b, int(be32(cdb[10:]))))
	case 0x1A, 0x5A:
		return l.modeSense(cdb)
	case 0xA0: // REPORT LUNS: LUN 0 only
		b := make([]byte, 16)
		put32(b, 8)
		return good(trunc(b, int(be32(cdb[6:]))))
	case 0x35, 0x91: // SYNCHRONIZE CACHE (10), (16)
		if l.writeBack {
			return good(nil)
		}
		if err := l.dev.Flush(); err != nil {
			l.logf("flush failed: %v", err)
			return checkCond(keyMedium, ascWriteError, 0)
		}
		return good(nil)
	}
	return checkCond(keyIllegal, ascInvOpcode, 0)
}

func (l *lun) readWrite(lba uint64, n uint32, fua, write bool, dataOut []byte) result {
	if lba > l.blocks || uint64(n) > l.blocks-lba {
		return checkCond(keyIllegal, ascLBARange, 0)
	}
	nbytes := int(n) * logicalBlock
	off := int64(lba) * logicalBlock
	if nbytes > maxTransfer {
		return checkCond(keyIllegal, ascInvField, 0)
	}
	if write && l.readOnly {
		return checkCond(keyDataProtect, ascWriteProtect, 0)
	}
	if nbytes == 0 {
		return good(nil)
	}
	if !write {
		buf := make([]byte, nbytes)
		if _, err := l.dev.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
			l.logf("read of %d bytes at %d failed: %v", nbytes, off, err)
			return checkCond(keyMedium, ascReadError, 0)
		}
		return good(buf)
	}
	if len(dataOut) < nbytes {
		return checkCond(keyIllegal, ascInvField, 0)
	}
	if _, err := l.dev.WriteAt(dataOut[:nbytes], off); err != nil {
		l.logf("write of %d bytes at %d failed: %v", nbytes, off, err)
		return checkCond(keyMedium, ascWriteError, 0)
	}
	if fua && !l.writeBack {
		if err := l.dev.Flush(); err != nil {
			l.logf("flush after a forced-unit-access write failed: %v", err)
			return checkCond(keyMedium, ascWriteError, 0)
		}
	}
	return good(nil)
}

func (l *lun) inquiry(cdb []byte) result {
	alloc := int(be16(cdb[3:]))
	if cdb[1]&1 == 0 {
		if cdb[2] != 0 {
			return checkCond(keyIllegal, ascInvField, 0)
		}
		b := make([]byte, 36)
		b[2] = 0x06        // SPC-4
		b[3] = 0x02 | 0x10 // response format 2, HiSup
		b[4] = byte(len(b) - 5)
		b[7] = 0x02 // CmdQue
		copy(b[8:], padStr("FileDO", 8))
		copy(b[16:], padStr("FDD Container", 16))
		copy(b[32:], padStr("0001", 4))
		return good(trunc(b, alloc))
	}
	page := cdb[2]
	var body []byte
	switch page {
	case 0x00: // supported pages
		body = []byte{0x00, 0x80, 0x83, 0xB0, 0xB1}
	case 0x80: // unit serial number
		body = []byte(l.serial)
	case 0x83: // device identification: NAA, then T10 vendor id
		body = append(body, 0x01, 0x03, 0x00, 0x08)
		body = append(body, l.naa[:]...)
		t10 := append(padStr("FileDO", 8), l.serial...)
		body = append(body, 0x02, 0x01, 0x00, byte(len(t10)))
		body = append(body, t10...)
	case 0xB0: // block limits
		body = make([]byte, 0x3C)
		put16(body[2:], uint16(1)<<l.physExp)             // optimal transfer length granularity
		put32(body[4:], uint32(maxTransfer/logicalBlock)) // maximum transfer length
		put32(body[8:], uint32(maxTransfer/logicalBlock)) // optimal transfer length
	case 0xB1: // block device characteristics
		body = make([]byte, 0x3C)
		put16(body[0:], 1) // non-rotating
	default:
		return checkCond(keyIllegal, ascInvField, 0)
	}
	b := make([]byte, 4+len(body))
	b[1] = page
	put16(b[2:], uint16(len(body)))
	copy(b[4:], body)
	return good(trunc(b, alloc))
}

// modeSense answers pages 0x08 (caching), 0x0A (control), 0x1C (exceptions)
// and 0x3F (all). The device-specific byte carries the write-protect bit, which
// is how a read-only mount is read-only above the file system as well as below
// it, and DPOFUA, so Windows may send forced-unit-access writes.
func (l *lun) modeSense(cdb []byte) result {
	ten := cdb[0] == 0x5A
	dbd := cdb[1]&0x08 != 0
	pc := cdb[2] >> 6
	page := cdb[2] & 0x3f
	if sub := cdb[3]; sub != 0 && sub != 0xff {
		return checkCond(keyIllegal, ascInvField, 0)
	}
	alloc := int(cdb[4])
	if ten {
		alloc = int(be16(cdb[7:]))
	}
	caching := make([]byte, 20)
	caching[0], caching[1] = 0x08, 0x12
	if pc != 1 { // not the changeable-values mask
		caching[2] = 0x04 // WCE: writes reach the file system cache, and SYNCHRONIZE CACHE is honoured
	}
	control := make([]byte, 12)
	control[0], control[1] = 0x0A, 0x0A
	iec := make([]byte, 12)
	iec[0], iec[1] = 0x1C, 0x0A
	var pages []byte
	switch page {
	case 0x08:
		pages = caching
	case 0x0A:
		pages = control
	case 0x1C:
		pages = iec
	case 0x3F:
		pages = append(append(append(pages, caching...), control...), iec...)
	default:
		return checkCond(keyIllegal, ascInvField, 0)
	}
	var bd []byte
	if !dbd {
		bd = make([]byte, 8)
		nb := l.blocks
		if nb > 0xffffff {
			nb = 0xffffff
		}
		bd[1], bd[2], bd[3] = byte(nb>>16), byte(nb>>8), byte(nb)
		bd[6], bd[7] = byte(logicalBlock>>8), byte(logicalBlock&0xff)
	}
	dsp := byte(0x10) // DPOFUA
	if l.readOnly {
		dsp |= 0x80 // WP
	}
	var b []byte
	if ten {
		b = make([]byte, 8)
		b[3] = dsp
		put16(b[6:], uint16(len(bd)))
		b = append(append(b, bd...), pages...)
		put16(b[0:], uint16(len(b)-2))
	} else {
		b = make([]byte, 4)
		b[2] = dsp
		b[3] = byte(len(bd))
		b = append(append(b, bd...), pages...)
		b[0] = byte(len(b) - 1)
	}
	return good(trunc(b, alloc))
}
