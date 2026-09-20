// Package fdsec implements the FileDO secret-file container, format version 1,
// suite 1. The on-disk contract is FDSEC-FORMAT.md at the repository root; every
// offset, width and rule in this package cites the section of that document it
// implements. A container built under a non-empty credential is encryption; the
// empty credential is accepted and is obfuscation with no secrecy - never call
// it protection.
package fdsec

import (
	"encoding/binary"
	"fmt"
	"hash"
	"io"

	"golang.org/x/crypto/blake2b"
)

// Clear-header layout constants (FDSEC-FORMAT.md sections 3-4).
const (
	Magic         = "FDSEC"
	FormatVersion = 1
	SuiteID1      = 1

	headerSize = 81 // clear header
	slotCount  = 8
	slotSize   = 80
	slotsSize  = slotCount * slotSize // 640
	metaPlain  = 4096
	metaSize   = metaPlain + tagSize // sealed metadata, 4112 on disk
	preMeta    = headerSize + slotsSize

	saltSize     = 16
	fileKeySize  = 32
	nonceSize    = 24
	tagSize      = 16
	digestSize   = 32
	wrappedSize  = fileKeySize + tagSize
	slotPayload  = slotSize - 1
	typeEmpty    = 0
	typeCredWrap = 1
	typeRecovery = 2 // reserved by name (FDSEC-FORMAT.md section 5), never written

	flagV1Conform uint16 = 0x0001

	MaxNameBytes = 3000
)

// Writer defaults (FDSEC-FORMAT.md section 6). They are recorded in every
// container's header; a reader obeys the file, never its own defaults.
const (
	DefaultClusterAlignment uint32 = 4096
	DefaultChunkSize        uint32 = 1 << 20 // on-disk chunk slot; plaintext capacity is S-16
	DefaultThreshold        uint32 = 64
	DefaultKDFMemoryKiB     uint32 = 65536
	DefaultKDFTime          uint32 = 3
	DefaultKDFLanes         uint8  = 4
)

// header is the clear header (FDSEC-FORMAT.md section 4). All integers are
// little-endian on disk.
type header struct {
	Version          uint8
	Suite            uint8
	Flags            uint16
	Salt             [saltSize]byte
	KDFMemoryKiB     uint32
	KDFTime          uint32
	KDFLanes         uint8
	KDFKeyLen        uint8
	KDFReserved      uint16
	Threshold        uint32
	ChunkSize        uint32
	ClusterAlignment uint32
	HeaderDigest     [digestSize]byte
}

func (h *header) marshal() []byte {
	b := make([]byte, headerSize)
	copy(b[0:5], Magic)
	b[5] = h.Version
	b[6] = h.Suite
	binary.LittleEndian.PutUint16(b[7:9], h.Flags)
	copy(b[9:25], h.Salt[:])
	binary.LittleEndian.PutUint32(b[25:29], h.KDFMemoryKiB)
	binary.LittleEndian.PutUint32(b[29:33], h.KDFTime)
	b[33] = h.KDFLanes
	b[34] = h.KDFKeyLen
	binary.LittleEndian.PutUint16(b[35:37], h.KDFReserved)
	binary.LittleEndian.PutUint32(b[37:41], h.Threshold)
	binary.LittleEndian.PutUint32(b[41:45], h.ChunkSize)
	binary.LittleEndian.PutUint32(b[45:49], h.ClusterAlignment)
	copy(b[49:81], h.HeaderDigest[:])
	return b
}

// setDigest computes the header digest over bytes [0, 49) and stores it
// (FDSEC-FORMAT.md section 4, last row).
func (h *header) setDigest() {
	saved := h.HeaderDigest
	h.HeaderDigest = [digestSize]byte{}
	b := h.marshal()
	h.HeaderDigest = saved
	h.HeaderDigest = blake2b.Sum256(b[:49])
}

// parseHeader reads and validates the clear header. The three failure classes
// never fall through into each other (FDSEC-FORMAT.md sections 4, 12): a bad
// digest is damage, an unknown version/suite/flag/slot type is a refusal by
// name, and neither is ever a credential problem.
func parseHeader(r io.Reader) (*header, error) {
	b := make([]byte, headerSize)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("%w: header unreadable (%v)", ErrDamaged, err)
	}
	if string(b[0:5]) != Magic {
		return nil, fmt.Errorf("%w: not a framed fdsec container (no magic)", ErrUnsupported)
	}
	var stored [digestSize]byte
	copy(stored[:], b[49:81])
	if blake2b.Sum256(b[:49]) != stored {
		return nil, fmt.Errorf("%w: header digest mismatch", ErrDamaged)
	}
	h := &header{
		Version:          b[5],
		Suite:            b[6],
		Flags:            binary.LittleEndian.Uint16(b[7:9]),
		KDFMemoryKiB:     binary.LittleEndian.Uint32(b[25:29]),
		KDFTime:          binary.LittleEndian.Uint32(b[29:33]),
		KDFLanes:         b[33],
		KDFKeyLen:        b[34],
		KDFReserved:      binary.LittleEndian.Uint16(b[35:37]),
		Threshold:        binary.LittleEndian.Uint32(b[37:41]),
		ChunkSize:        binary.LittleEndian.Uint32(b[41:45]),
		ClusterAlignment: binary.LittleEndian.Uint32(b[45:49]),
	}
	copy(h.Salt[:], b[9:25])
	copy(h.HeaderDigest[:], b[49:81])
	switch {
	case h.Version != FormatVersion:
		return nil, fmt.Errorf("%w: format version %d", ErrUnsupported, h.Version)
	case h.Suite != SuiteID1:
		return nil, fmt.Errorf("%w: suite id %d", ErrUnsupported, h.Suite)
	case h.Flags != flagV1Conform:
		return nil, fmt.Errorf("%w: flags 0x%04x", ErrUnsupported, h.Flags)
	case h.KDFKeyLen != fileKeySize:
		return nil, fmt.Errorf("%w: KDF key length %d", ErrDamaged, h.KDFKeyLen)
	case h.KDFReserved != 0:
		return nil, fmt.Errorf("%w: nonzero KDF reserved bytes", ErrDamaged)
	case h.KDFMemoryKiB < 1 || h.KDFTime < 1 || h.KDFLanes < 1:
		return nil, fmt.Errorf("%w: impossible KDF parameters", ErrDamaged)
	case h.Threshold < 1:
		return nil, fmt.Errorf("%w: zero KDF length threshold", ErrDamaged)
	case !isPow2(h.ClusterAlignment) || h.ClusterAlignment < 512 || h.ClusterAlignment > 1<<21:
		return nil, fmt.Errorf("%w: cluster alignment %d", ErrDamaged, h.ClusterAlignment)
	case h.ChunkSize < h.ClusterAlignment || h.ChunkSize%h.ClusterAlignment != 0:
		return nil, fmt.Errorf("%w: chunk size %d breaks the alignment invariant", ErrDamaged, h.ChunkSize)
	}
	return h, nil
}

func isPow2(v uint32) bool { return v > 0 && v&(v-1) == 0 }

// preLen is the payload start offset: header + slots + sealed metadata, padded
// with random bytes to a cluster boundary (FDSEC-FORMAT.md section 3).
func (h *header) preLen() int64 {
	raw := int64(preMeta + metaSize)
	a := int64(h.ClusterAlignment)
	return (raw + a - 1) / a * a
}

// chunkCapacity is the plaintext capacity of a full chunk slot: S - 16 tag
// bytes (FDSEC-FORMAT.md section 8).
func (h *header) chunkCapacity() int64 { return int64(h.ChunkSize) - tagSize }

// chunkCount derives the number of chunks from the real size N. An empty
// original has one zero-length chunk (FDSEC-FORMAT.md section 8).
func (h *header) chunkCount(n int64) int64 {
	k := (n + h.chunkCapacity() - 1) / h.chunkCapacity()
	if k < 1 {
		k = 1
	}
	return k
}

func (h *header) lastLen(n int64) int64 {
	return n - (h.chunkCount(n)-1)*h.chunkCapacity()
}

// slotRecord is one 80-byte key-slot record (FDSEC-FORMAT.md section 5).
type slotRecord struct {
	Type    uint8
	Payload [slotPayload]byte
}

// Type-1 payload: wrap_nonce(24) || wrapped(48) || 6 reserved zero bytes.
const wrapNonceOff = 0

func marshalSlots(slot0 *slotRecord) []byte {
	b := make([]byte, slotsSize)
	b[0] = slot0.Type
	copy(b[1:slotSize], slot0.Payload[:])
	// Slots 1..7 stay type 0 in version 1.
	return b
}

func parseSlots(r io.Reader) ([]slotRecord, error) {
	b := make([]byte, slotsSize)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, fmt.Errorf("%w: key slots unreadable (%v)", ErrDamaged, err)
	}
	slots := make([]slotRecord, slotCount)
	for i := range slots {
		slots[i].Type = b[i*slotSize]
		copy(slots[i].Payload[:], b[i*slotSize+1:(i+1)*slotSize])
	}
	if slots[0].Type == typeEmpty {
		return nil, fmt.Errorf("%w: key slot 0 is empty", ErrDamaged)
	}
	if slots[0].Type != typeCredWrap {
		return nil, fmt.Errorf("%w: key-slot type %d in slot 0", ErrUnsupported, slots[0].Type)
	}
	for _, s := range slots[1:] {
		if s.Type != typeEmpty {
			return nil, fmt.Errorf("%w: key-slot type %d in a reserved slot", ErrUnsupported, s.Type)
		}
	}
	if nz(slots[0].Payload[nonceSize+wrappedSize:]) {
		return nil, fmt.Errorf("%w: nonzero reserved bytes in key slot 0", ErrDamaged)
	}
	return slots, nil
}

func nz(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

// IsFramed reports whether b begins with the suite-1 frame magic. This is the
// framed-versus-quiet dispatch point (FDSEC-FORMAT.md section 13.4).
func IsFramed(b []byte) bool {
	return len(b) >= 5 && string(b[:5]) == Magic
}

// newDigest is a BLAKE2b-256 hash; keyed mode is used for nonce derivation.
func newDigest(key []byte) (hash.Hash, error) {
	return blake2b.New256(key)
}
