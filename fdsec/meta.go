package fdsec

import (
	"encoding/binary"
	"fmt"
	"time"

	"golang.org/x/text/unicode/norm"
)

// Windows FILETIME: 100-nanosecond intervals since 1601-01-01 UTC
// (FDSEC-FORMAT.md section 7).
const filetimeEpoch = 116444736000000000

func toFILETIME(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	return uint64(t.UTC().UnixNano()/100 + filetimeEpoch)
}

func fromFILETIME(v uint64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.Unix(0, (int64(v)-filetimeEpoch)*100).UTC()
}

// Metadata is the sealed metadata block's content (FDSEC-FORMAT.md section 7):
// the facts that must not leak from the container - the true name, the real
// size, the original's timestamps - plus the digest the read-back proof uses.
type Metadata struct {
	Name       string
	Size       int64
	EncodedAt  time.Time // time of encryption
	CreatedAt  time.Time // original's creation time
	AccessedAt time.Time // original's last-access time
	ModifiedAt time.Time // original's last-write time
}

// buildMetadataBlock lays out the fixed 4096-byte plaintext block:
//
//	u16le name_len || name || u64le size || 4x u64le FILETIME ||
//	32-byte digest || random pad to 4096
func buildMetadataBlock(m Metadata, digest [digestSize]byte, pad func(int) ([]byte, error)) ([]byte, error) {
	name := norm.NFC.String(m.Name)
	if len(name) < 1 || len(name) > MaxNameBytes {
		return nil, fmt.Errorf("fdsec: original name must be 1..%d UTF-8 bytes, is %d", MaxNameBytes, len(name))
	}
	if m.Size < 0 {
		return nil, fmt.Errorf("fdsec: negative size %d", m.Size)
	}
	b := make([]byte, metaPlain)
	binary.LittleEndian.PutUint16(b[0:2], uint16(len(name)))
	copy(b[2:], name)
	o := 2 + len(name)
	binary.LittleEndian.PutUint64(b[o:], uint64(m.Size))
	binary.LittleEndian.PutUint64(b[o+8:], toFILETIME(m.EncodedAt))
	binary.LittleEndian.PutUint64(b[o+16:], toFILETIME(m.CreatedAt))
	binary.LittleEndian.PutUint64(b[o+24:], toFILETIME(m.AccessedAt))
	binary.LittleEndian.PutUint64(b[o+32:], toFILETIME(m.ModifiedAt))
	copy(b[o+40:], digest[:])
	off := o + 40 + digestSize
	if off > metaPlain {
		return nil, fmt.Errorf("fdsec: name of %d bytes overflows the metadata block", len(name))
	}
	p, err := pad(metaPlain - off)
	if err != nil {
		return nil, err
	}
	copy(b[off:], p)
	return b, nil
}

func parseMetadataBlock(b []byte) (Metadata, [digestSize]byte, error) {
	var m Metadata
	var digest [digestSize]byte
	if len(b) != metaPlain {
		return m, digest, fmt.Errorf("%w: metadata block is %d bytes, want %d", ErrDamaged, len(b), metaPlain)
	}
	nameLen := int(binary.LittleEndian.Uint16(b[0:2]))
	if nameLen < 1 || nameLen > MaxNameBytes {
		return m, digest, fmt.Errorf("%w: metadata name length %d", ErrDamaged, nameLen)
	}
	o := 2 + nameLen
	if o+40+digestSize > metaPlain {
		return m, digest, fmt.Errorf("%w: metadata name overflows the block", ErrDamaged)
	}
	m.Name = string(b[2 : 2+nameLen])
	m.Size = int64(binary.LittleEndian.Uint64(b[o:]))
	m.EncodedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+8:]))
	m.CreatedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+16:]))
	m.AccessedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+24:]))
	m.ModifiedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+32:]))
	copy(digest[:], b[o+40:])
	return m, digest, nil
}
