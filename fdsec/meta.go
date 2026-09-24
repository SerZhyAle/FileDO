package fdsec

import (
	"encoding/binary"
	"fmt"
	"math"
	"path/filepath"
	"strings"
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

var reservedStems = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true,
	"com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true,
	"lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

// ValidateName screens the sealed true name against path separators, reserved
// device names, control characters and trailing dots or spaces (FDSEC-FORMAT.md
// section 7, FDSEC-BEHAVIOUR.md section 4.3). The error does not echo the name.
func ValidateName(name string) error {
	if len(name) < 1 || len(name) > MaxNameBytes {
		return fmt.Errorf("name must be 1..%d UTF-8 bytes", MaxNameBytes)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("name is a relative directory reference")
	}
	if strings.ContainsAny(name, `/\:*?"<>|`) {
		return fmt.Errorf("name contains a path separator or illegal character")
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("name contains a control character")
		}
	}
	if strings.TrimRight(name, ". ") != name {
		return fmt.Errorf("name ends in a dot or a space")
	}
	if filepath.IsAbs(name) || filepath.Base(name) != name {
		return fmt.Errorf("name is a path, not a file name")
	}
	stem := strings.ToLower(name)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if reservedStems[stem] {
		return fmt.Errorf("name is a reserved device name")
	}
	return nil
}

// buildMetadataBlock lays out the fixed 4096-byte plaintext block:
//
//	u16le name_len || name || u64le size || 4x u64le FILETIME ||
//	32-byte digest || random pad to 4096
func buildMetadataBlock(m Metadata, digest [digestSize]byte, pad func(int) ([]byte, error)) ([]byte, error) {
	name := norm.NFC.String(m.Name)
	if err := ValidateName(name); err != nil {
		return nil, fmt.Errorf("fdsec: %w", err)
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
	name := string(b[2 : 2+nameLen])
	if err := ValidateName(name); err != nil {
		return m, digest, fmt.Errorf("%w: sealed name: %v", ErrDamaged, err)
	}
	m.Name = name

	sizeRaw := binary.LittleEndian.Uint64(b[o:])
	if sizeRaw > math.MaxInt64 {
		return m, digest, fmt.Errorf("%w: sealed size %d exceeds max int64", ErrDamaged, sizeRaw)
	}
	m.Size = int64(sizeRaw)
	m.EncodedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+8:]))
	m.CreatedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+16:]))
	m.AccessedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+24:]))
	m.ModifiedAt = fromFILETIME(binary.LittleEndian.Uint64(b[o+32:]))
	copy(digest[:], b[o+40:])
	return m, digest, nil
}
