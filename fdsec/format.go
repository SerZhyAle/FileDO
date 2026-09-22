// Package fdsec implements the FileDO secret-file container, format version 1,
// suite 1. The on-disk contract is FDSEC-FORMAT.md, which lives outside this
// repository in the shared contracts catalog - see AGENTS.md "External
// contracts" for where that is, and docs/contracts/FDSEC-FORMAT.md for what
// this package owes it. Every offset, width and rule below cites the section of
// that document it implements. A container built under a non-empty credential
// is encryption; the empty credential is accepted and is obfuscation with no
// secrecy - never call it protection.
//
// A container never announces itself. There is no magic, no version byte in the
// clear, no field a scanner can read: the only unmasked bytes of the head are
// the 16-byte salt, and everything after it - parameters and key slots alike -
// is masked with a keystream that only the credential produces
// (FDSEC-FORMAT.md sections 3, 4, 13.4). Consequently nothing in this package
// can tell "not a container" from "wrong credential": the two are one outcome
// by construction.
package fdsec

import (
	"encoding/binary"
	"fmt"
	"hash"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20"
)

// Head layout constants (FDSEC-FORMAT.md sections 3-5).
const (
	FormatVersion = 1
	SuiteID1      = 1

	saltSize   = 16
	headerSize = 28       // salt + the masked parameter block
	maskOff    = saltSize // the mask starts right after the salt
	slotCount  = 8
	slotSize   = 80
	slotsSize  = slotCount * slotSize   // 640
	preMeta    = headerSize + slotsSize // 668: the whole head
	maskLen    = preMeta - maskOff      // 652 masked bytes
	metaPlain  = 4096
	metaSize   = metaPlain + tagSize // sealed metadata, 4112 on disk

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

// Writer defaults (FDSEC-FORMAT.md section 6). They are recorded in the masked
// header of every container; a reader obeys the file, never its own defaults.
// The KDF profile is not among them: it is fixed by the format version
// (section 6), because the mask must be derived before any byte on disk can be
// read.
const (
	DefaultClusterAlignment uint32 = 4096
	DefaultChunkSize        uint32 = 1 << 20 // on-disk chunk slot; plaintext capacity is S-16
)

// header is the container head (FDSEC-FORMAT.md section 4): the clear salt
// followed by the parameter block, which is masked on disk. All integers are
// little-endian.
type header struct {
	Salt             [saltSize]byte
	Version          uint8
	Suite            uint8
	Flags            uint16
	ChunkSize        uint32
	ClusterAlignment uint32
	HeaderDigest     [digestSize]byte // derived, never stored
}

func (h *header) marshal() []byte {
	b := make([]byte, headerSize)
	copy(b[0:16], h.Salt[:])
	b[16] = h.Version
	b[17] = h.Suite
	binary.LittleEndian.PutUint16(b[18:20], h.Flags)
	binary.LittleEndian.PutUint32(b[20:24], h.ChunkSize)
	binary.LittleEndian.PutUint32(b[24:28], h.ClusterAlignment)
	return b
}

func (h *header) unmarshal(b []byte) {
	copy(h.Salt[:], b[0:16])
	h.Version = b[16]
	h.Suite = b[17]
	h.Flags = binary.LittleEndian.Uint16(b[18:20])
	h.ChunkSize = binary.LittleEndian.Uint32(b[20:24])
	h.ClusterAlignment = binary.LittleEndian.Uint32(b[24:28])
}

// setDigest computes the header digest over the 28 plaintext header bytes
// (FDSEC-FORMAT.md section 4). The digest is the associated data of every AEAD
// call in the container and is never written to disk: a stored verifier would
// hand an attacker a cheap way to confirm a credential guess, and a container
// that stores nothing verifiable cannot be recognised at all.
func (h *header) setDigest() {
	h.HeaderDigest = blake2b.Sum256(h.marshal())
}

// mask XORs the keystream of the credential-derived mask key over b, the head
// from the end of the salt onwards (FDSEC-FORMAT.md section 4, "Masking").
// ChaCha20 with a 32-byte key, a twelve-byte zero nonce and counter 0 - the key
// is single-use per container because the salt is.
func mask(maskKey, b []byte) error {
	c, err := chacha20.NewUnauthenticatedCipher(maskKey, make([]byte, chacha20.NonceSize))
	if err != nil {
		return err
	}
	c.XORKeyStream(b, b)
	return nil
}

// validateHeader applies the rules of FDSEC-FORMAT.md sections 4 and 9. It runs
// only after slot 0 has authenticated, so by the time it can fail the file is
// known to be a container opened with the right credential: an unknown version,
// suite or flag bit is a refusal by name, a broken structural rule is damage,
// and neither can be a credential problem any more (section 12).
func validateHeader(h *header) error {
	switch {
	case h.Version != FormatVersion:
		return fmt.Errorf("%w: format version %d", ErrUnsupported, h.Version)
	case h.Suite != SuiteID1:
		return fmt.Errorf("%w: suite id %d", ErrUnsupported, h.Suite)
	case h.Flags != flagV1Conform:
		return fmt.Errorf("%w: flags 0x%04x", ErrUnsupported, h.Flags)
	case !isPow2(h.ClusterAlignment) || h.ClusterAlignment < 512 || h.ClusterAlignment > 1<<21:
		return fmt.Errorf("%w: cluster alignment %d", ErrDamaged, h.ClusterAlignment)
	case h.ChunkSize < h.ClusterAlignment || h.ChunkSize%h.ClusterAlignment != 0:
		return fmt.Errorf("%w: chunk size %d breaks the alignment invariant", ErrDamaged, h.ChunkSize)
	}
	return nil
}

func isPow2(v uint32) bool { return v > 0 && v&(v-1) == 0 }

// preLen is the payload start offset: head + sealed metadata, padded with
// random bytes to a cluster boundary (FDSEC-FORMAT.md section 3).
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
	// Slots 1..7 stay type 0 in version 1; the mask makes their zero bytes
	// look like every other byte of the file.
	return b
}

func parseSlots(b []byte) []slotRecord {
	slots := make([]slotRecord, slotCount)
	for i := range slots {
		slots[i].Type = b[i*slotSize]
		copy(slots[i].Payload[:], b[i*slotSize+1:(i+1)*slotSize])
	}
	return slots
}

// validateSlots runs after slot 0 authenticated, for the same reason as
// validateHeader: before that point every byte here is indistinguishable from
// noise (FDSEC-FORMAT.md sections 5, 12).
func validateSlots(slots []slotRecord) error {
	if slots[0].Type != typeCredWrap {
		return fmt.Errorf("%w: key-slot type %d in slot 0", ErrUnsupported, slots[0].Type)
	}
	for _, s := range slots[1:] {
		if s.Type != typeEmpty {
			return fmt.Errorf("%w: key-slot type %d in a reserved slot", ErrUnsupported, s.Type)
		}
	}
	if nz(slots[0].Payload[nonceSize+wrappedSize:]) {
		return fmt.Errorf("%w: nonzero reserved bytes in key slot 0", ErrDamaged)
	}
	for _, s := range slots[1:] {
		if nz(s.Payload[:]) {
			return fmt.Errorf("%w: nonzero payload in an empty key slot", ErrDamaged)
		}
	}
	return nil
}

func nz(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

// newDigest is a BLAKE2b-256 hash; keyed mode is used for nonce derivation.
func newDigest(key []byte) (hash.Hash, error) {
	return blake2b.New256(key)
}
