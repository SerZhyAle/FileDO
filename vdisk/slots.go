package vdisk

import (
	"io"

	"golang.org/x/crypto/chacha20poly1305"

	"filedo/fdsec"
)

// The key-slot region (FDD-FORMAT section 7): eight 512-byte slots, each
// slot_salt(16) || slot_nonce(24) || Seal(slot_key, nonce, "FDD/v1/slot",
// plaintext 80)(96) || padding(376). An unused slot is 512 bytes of CSPRNG
// output. Kind 1 is the pepper-wrapped data key of an obfuscated container;
// kinds 2 and 3 wrap it under a credential key, and only they make a container
// encrypted (SP-0004 S5).

const (
	slotSaltSize   = 16
	slotNonceSize  = 24
	slotPlainSize  = 80
	slotSealedSize = slotPlainSize + 16
	slotPadSize    = slotSize - slotSaltSize - slotNonceSize - slotSealedSize // 376
	dataKeySize    = 64                                                       // AES-256-XTS key pair
)

const (
	slotKindPepper     = 1
	slotKindPassphrase = 2
	slotKindKeyfile    = 3
)

var slotMagic = [4]byte{'S', 'L', 'O', 'T'}

// slotPlaintext is magic || kind || slot_flags(0) || reserved(2) || data_key
// || reserved(8) (section 7.2).
func slotPlaintext(kind byte, dataKey []byte) []byte {
	pt := make([]byte, slotPlainSize)
	copy(pt[0:4], slotMagic[:])
	pt[4] = kind
	copy(pt[8:72], dataKey)
	return pt
}

// sealSlot builds one used slot from its drawn salt, nonce and padding.
func sealSlot(k []byte, salt, nonce, pad []byte, kind byte, dataKey []byte) []byte {
	sk := slotKey(k, salt)
	aead, err := chacha20poly1305.NewX(sk[:])
	clear(sk[:])
	if err != nil {
		panic(err)
	}
	pt := slotPlaintext(kind, dataKey)
	b := make([]byte, 0, slotSize)
	b = append(b, salt...)
	b = append(b, nonce...)
	b = aead.Seal(b, nonce, pt, []byte(adSlot))
	clear(pt)
	b = append(b, pad...)
	return b
}

// drawSlots draws the slot region of an obfuscated container: slot 0 is the
// used kind-1 slot under obf_key.
func drawSlots(r io.Reader, k []byte, dataKey []byte) ([]byte, error) {
	return drawSlotsKind(r, k, slotKindPepper, dataKey)
}

// drawSlotsKind draws the slot region in the order of section 15.1: slot 0 is
// the one used slot, of the given kind under K (salt, nonce, padding), and
// slots 1-7 are 512 random bytes each.
func drawSlotsKind(r io.Reader, k []byte, kind byte, dataKey []byte) ([]byte, error) {
	region := make([]byte, 0, slotRegionSize)
	used, err := drawSlot(r, k, kind, dataKey)
	if err != nil {
		return nil, err
	}
	region = append(region, used...)
	rest := make([]byte, slotRegionSize-slotSize)
	if _, err := io.ReadFull(r, rest); err != nil {
		return nil, err
	}
	return append(region, rest...), nil
}

// drawSlot draws one used slot: salt, nonce and padding, in that order.
func drawSlot(r io.Reader, k []byte, kind byte, dataKey []byte) ([]byte, error) {
	salt := make([]byte, slotSaltSize)
	nonce := make([]byte, slotNonceSize)
	pad := make([]byte, slotPadSize)
	for _, b := range [][]byte{salt, nonce, pad} {
		if _, err := io.ReadFull(r, b); err != nil {
			return nil, err
		}
	}
	return sealSlot(k, salt, nonce, pad, kind, dataKey), nil
}

// openSlots tries all eight slots under K and returns the data key (section
// 8.3). obfuscated says which key K is, and so which outcome a slot region
// with no opening slot is.
func openSlots(region []byte, k []byte, obfuscated bool) ([]byte, error) {
	key, _, err := openSlotsAt(region, k, obfuscated)
	return key, err
}

// openSlotsAt is openSlots that also reports which slots opened, for the
// operations that rewrite slots. All eight are always tried, whatever the
// first one did, so the time taken says nothing about which slot, or how
// many, opened (FDD-FORMAT 8.3; P5 section 5 rule 1).
func openSlotsAt(region []byte, k []byte, obfuscated bool) ([]byte, []int, error) {
	if len(region) != slotRegionSize {
		return nil, nil, damagedf("the slot region is %d bytes", len(region))
	}
	var found []byte
	var opened []int
	var fail error
	for i := 0; i < slotCount; i++ {
		s := region[i*slotSize : (i+1)*slotSize]
		salt := s[:slotSaltSize]
		nonce := s[slotSaltSize : slotSaltSize+slotNonceSize]
		sk := slotKey(k, salt)
		aead, err := chacha20poly1305.NewX(sk[:])
		clear(sk[:])
		if err != nil {
			return nil, nil, err
		}
		pt, err := aead.Open(nil, nonce, s[slotSaltSize+slotNonceSize:slotSaltSize+slotNonceSize+slotSealedSize], []byte(adSlot))
		if err != nil {
			continue
		}
		// The first failure decides, but the loop still runs all eight.
		if fail != nil {
			clear(pt)
			continue
		}
		switch {
		case [4]byte(pt[0:4]) != slotMagic:
			fail = damagedf("a key slot opened without its SLOT marker")
		case pt[4] == 0 || pt[4] > slotKindKeyfile:
			fail = unsupportedf("key slot kind %d", pt[4])
		case pt[5] != 0:
			fail = unsupportedf("key slot flags 0x%02x", pt[5])
		case obfuscated != (pt[4] == slotKindPepper):
			fail = damagedf("a key slot of kind %d opened under the wrong key", pt[4])
		case found != nil && !sameBytes(found, pt[8:72]):
			fail = damagedf("two key slots hold different data keys")
		default:
			if found == nil {
				found = append([]byte(nil), pt[8:72]...)
			}
			opened = append(opened, i)
		}
		clear(pt)
	}
	if fail != nil {
		clear(found)
		return nil, nil, fail
	}
	if found == nil {
		if obfuscated {
			return nil, nil, damagedf("no key slot opens (an obfuscated container cannot have a wrong credential)")
		}
		return nil, nil, ErrCredential
	}
	return found, opened, nil
}

// credKey is the credential key of an encrypted container (section 8.2):
// Argon2id over BLAKE2b-512(key = PEPPER, msg = credential) - the shared
// fdsec.DeriveKey under this format's pepper - with the row kdf_params_id
// names. One derivation serves all eight slot attempts.
func credKey(cred fdsec.Credential, kdfSalt []byte, paramsID uint32) ([]byte, error) {
	p, ok := kdfParams[paramsID]
	if !ok {
		return nil, unsupportedf("kdf_params_id %d", paramsID)
	}
	return fdsec.DeriveKey(cred, pepper[:], kdfSalt, p)
}

// slotKindFor is the kind a credential's slot records: 3 for a keyfile's
// digest, 2 for a passphrase. The kind is a label for reports; the key does
// not depend on it.
func slotKindFor(keyfile bool) byte {
	if keyfile {
		return slotKindKeyfile
	}
	return slotKindPassphrase
}

// unlockSlots reads the slot region and opens it under the key the header
// names: obf_key for an obfuscated container, where cred is not used
// (FDD-BEHAVIOUR 7 rule 4), and cred_key for an encrypted one, where an empty
// credential is tried like any other and fails like any other wrong one.
// Nothing is written, whatever the outcome.
func unlockSlots(b backing, h *header, cred fdsec.Credential) (dataKey []byte, opened []int, err error) {
	region := make([]byte, slotRegionSize)
	if err := readFull(b, region, int64(h.SlotsOffset)); err != nil {
		return nil, nil, err
	}
	if h.obfuscated() {
		k := obfKey(h.KDFSalt[:])
		defer clear(k[:])
		return openSlotsAt(region, k[:], true)
	}
	k, err := credKey(cred, h.KDFSalt[:], h.KDFParamsID)
	if err != nil {
		return nil, nil, err
	}
	defer clear(k)
	return openSlotsAt(region, k, false)
}
