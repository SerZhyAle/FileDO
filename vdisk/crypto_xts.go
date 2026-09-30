package vdisk

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/subtle"
	"encoding/binary"
)

// The data layer (FDD-FORMAT section 9): AES-256-XTS (IEEE 1619), one crypto
// sector of 1 << sector_shift bytes at a time, keyed by the slot's 64-byte
// data key - bytes 0-31 the data key, 32-63 the tweak key.
//
// The tweak is the LOGICAL sector index - the sector's offset in the volume
// divided by the sector size - never its position in the file, encoded as a
// 128-bit little-endian number. Because the tweak is logical, a cluster moved
// in the file keeps its ciphertext: compaction is a byte copy with no key.
//
// The implementation is the standard construction with the tweak stream for a
// whole sector computed up front in 64-bit words and applied with
// crypto/subtle.XORBytes, which the SP-0004 S0 measurement found three to four
// times faster than golang.org/x/crypto/xts's byte-wise loops. Its output is
// byte-identical to x/crypto/xts, which the tests keep as the independent
// oracle (TestVD_XTS_MatchesXCrypto). Sector sizes are multiples of 16, so no
// ciphertext stealing is ever needed.
//
// XTS detects nothing. A changed ciphertext byte decrypts to a garbled sector,
// silently; this layer is obfuscation unless the data key sits behind a
// credential, and it is never described as integrity.

type sectorCipher struct {
	k1, k2 cipher.Block
	shift  uint
}

func newSectorCipher(dataKey []byte, sectorShift uint16) (*sectorCipher, error) {
	if len(dataKey) != dataKeySize {
		return nil, damagedf("the data key is %d bytes, not %d", len(dataKey), dataKeySize)
	}
	k1, err := aes.NewCipher(dataKey[:32])
	if err != nil {
		return nil, err
	}
	k2, err := aes.NewCipher(dataKey[32:])
	if err != nil {
		return nil, err
	}
	return &sectorCipher{k1: k1, k2: k2, shift: uint(sectorShift)}, nil
}

func (s *sectorCipher) size() int { return 1 << s.shift }

// tweaks fills tb with the tweak of every 16-byte block of one sector: the
// sector index encrypted under the tweak key, then multiplied by x in GF(2^128)
// once per block (IEEE 1619 section 5.2).
func (s *sectorCipher) tweaks(tb []byte, sector uint64) {
	var t [16]byte
	binary.LittleEndian.PutUint64(t[:8], sector)
	s.k2.Encrypt(t[:], t[:])
	lo := binary.LittleEndian.Uint64(t[:8])
	hi := binary.LittleEndian.Uint64(t[8:])
	for i := 0; i < len(tb); i += 16 {
		binary.LittleEndian.PutUint64(tb[i:], lo)
		binary.LittleEndian.PutUint64(tb[i+8:], hi)
		carry := hi >> 63
		hi = hi<<1 | lo>>63
		lo = lo<<1 ^ carry*0x87
	}
}

// encrypt encrypts whole sectors of src into dst (which may be src);
// firstSector is the logical index of the first one.
func (s *sectorCipher) encrypt(dst, src []byte, firstSector uint64) {
	s.run(dst, src, firstSector, s.k1.Encrypt)
}

// decrypt is encrypt's inverse.
func (s *sectorCipher) decrypt(dst, src []byte, firstSector uint64) {
	s.run(dst, src, firstSector, s.k1.Decrypt)
}

func (s *sectorCipher) run(dst, src []byte, firstSector uint64, block func(dst, src []byte)) {
	n := s.size()
	tb := make([]byte, n)
	for i := 0; i+n <= len(src); i += n {
		d, p := dst[i:i+n], src[i:i+n]
		s.tweaks(tb, firstSector+uint64(i/n))
		subtle.XORBytes(d, p, tb)
		for j := 0; j < n; j += 16 {
			block(d[j:j+16], d[j:j+16])
		}
		subtle.XORBytes(d, d, tb)
	}
}
