package vdisk

import (
	"crypto/subtle"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
)

// The header seal (FDD-FORMAT sections 4 and 6). The pepper, the two
// associated-data strings, the positions of salt, nonce and pad, the sealed
// length and the positions of magic and the version fields are frozen across
// every major version, so that a reader refuses a later major by name rather
// than calling it damaged.

// pepper is FDD's own published constant (FDD-FORMAT section 4.1). It is not a
// secret; changing one byte makes every container unreadable.
var pepper = [32]byte{
	0xb1, 0xd3, 0xa9, 0x05, 0x4d, 0x75, 0xef, 0x6b, 0xf7, 0x21, 0xac, 0x38, 0x98, 0xcd, 0xf9, 0xb1,
	0x1c, 0x4f, 0x25, 0xa3, 0x61, 0xce, 0x40, 0xc5, 0x6d, 0x67, 0x0d, 0xec, 0xd6, 0x03, 0xff, 0xd1,
}

const (
	adHeader       = "FDD/v1/header"
	adHeaderBackup = "FDD/v1/header-backup"
	adSlot         = "FDD/v1/slot"
	msgObfuscation = "FDD/v1/obfuscation"
	msgSlotKey     = "FDD/v1/slot-key"
)

// keyedBlake256 is BLAKE2b-256(key = key, msg = parts...).
func keyedBlake256(key []byte, parts ...[]byte) [32]byte {
	h, err := blake2b.New256(key)
	if err != nil {
		panic(err) // a 32-byte key is always valid
	}
	for _, p := range parts {
		h.Write(p)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// headerKey = BLAKE2b-256(key = PEPPER, msg = salt) (section 4.2).
func headerKey(salt []byte) [32]byte { return keyedBlake256(pepper[:], salt) }

// obfKey = BLAKE2b-256(key = PEPPER, msg = "FDD/v1/obfuscation" || kdf_salt)
// (section 8.1). Derived from the header, so either header copy yields it.
func obfKey(kdfSalt []byte) [32]byte {
	return keyedBlake256(pepper[:], []byte(msgObfuscation), kdfSalt)
}

// slotKey = BLAKE2b-256(key = K, msg = "FDD/v1/slot-key" || slot_salt)
// (section 8.3).
func slotKey(k []byte, slotSalt []byte) [32]byte {
	return keyedBlake256(k, []byte(msgSlotKey), slotSalt)
}

// sealHeaderBlock builds one 4096-byte header block: salt || nonce || pad ||
// Seal(header_key, nonce, ad, plaintext).
func sealHeaderBlock(salt, nonce, pad, plaintext []byte, backup bool) []byte {
	ad := adHeader
	if backup {
		ad = adHeaderBackup
	}
	k := headerKey(salt)
	aead, err := chacha20poly1305.NewX(k[:])
	if err != nil {
		panic(err)
	}
	b := make([]byte, 0, headerSize)
	b = append(b, salt...)
	b = append(b, nonce...)
	b = append(b, pad...)
	b = aead.Seal(b, nonce, plaintext, []byte(ad))
	return b
}

// openHeaderBlock opens a 4096-byte header block under its own associated
// data. It returns the plaintext and the salt, or false when the tag does not
// verify - which is all "the header opens" means (section 6.1).
func openHeaderBlock(block []byte, backup bool) ([]byte, [32]byte, bool) {
	var salt [32]byte
	if len(block) != headerSize {
		return nil, salt, false
	}
	copy(salt[:], block[:headerSaltSize])
	ad := adHeader
	if backup {
		ad = adHeaderBackup
	}
	k := headerKey(salt[:])
	aead, err := chacha20poly1305.NewX(k[:])
	if err != nil {
		return nil, salt, false
	}
	nonce := block[headerSaltSize : headerSaltSize+headerNonceSize]
	pt, err := aead.Open(nil, nonce, block[sealedOffset:], []byte(ad))
	if err != nil {
		return nil, salt, false
	}
	return pt, salt, true
}

// sameBytes compares two plaintexts without an early exit; nothing secret
// rides on it, it is simply the idiom.
func sameBytes(a, b []byte) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare(a, b) == 1
}
