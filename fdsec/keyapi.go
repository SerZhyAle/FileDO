package fdsec

import (
	"errors"
	"runtime"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
)

// The key API the two container families share (SP-0004 P5 section 3,
// change 1). The derivation of suite 2 (FDSEC-FORMAT.md section 18.2) is the
// derivation of the disk container's credential key (FDD-FORMAT.md section
// 8.2) with another pepper, deliberately: one implementation serves both
// formats, and suite 2's vectors are what prove it did not move.

// KDFParams is one Argon2id work factor. Whoever names it on disk - a
// try-list position in suite 2, a kdf_params_id row in FDD - owns the table;
// this package only runs it. A work factor in use is frozen for the life of
// every file written under it.
type KDFParams struct {
	MemoryKiB uint32
	Time      uint32
	Lanes     uint8
}

// DeriveKeyLen is the length of the key DeriveKey returns.
const DeriveKeyLen = fileKeySize

// DeriveKey = Argon2id(BLAKE2b-512(key = pepper, msg = credential), salt, T,
// M, P, 32). There is no length threshold: every credential, empty included,
// takes the one derivation. The pepper is a published constant of the calling
// format, never a secret; it only keeps one format's dictionary from running
// against another's.
func DeriveKey(c Credential, pepper, salt []byte, p KDFParams) ([]byte, error) {
	if p.MemoryKiB == 0 || p.Time == 0 || p.Lanes == 0 {
		return nil, errors.New("fdsec: a work factor with a zero parameter")
	}
	h, err := blake2b.New512(pepper)
	if err != nil {
		return nil, err
	}
	h.Write(c)
	seed := h.Sum(nil)
	defer clear(seed)
	// A 256 MiB derivation is the largest allocation FileDO makes, and it
	// often follows another one (suite 1's 64 MiB under the dispatch).
	// Collecting first lets the heap reuse that block instead of reserving
	// both, which matters in the 32-bit build (SP-0019 D3 measurement).
	runtime.GC()
	return argon2.IDKey(seed, salt, p.Time, p.MemoryKiB, p.Lanes, fileKeySize), nil
}
