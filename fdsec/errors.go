package fdsec

import "errors"

// The three outcome classes of FDSEC-FORMAT.md section 12. They are produced by
// separate code paths and must never be reported as each other:
//
//	ErrCredentialOrTamper (class A) - the AEAD tag failed on a structurally
//	  intact file. A wrong credential and a flipped ciphertext byte are
//	  cryptographically indistinguishable, so the message says "wrong
//	  credential, or tampered" and never one of them alone.
//	ErrDamaged (class B) - header digest mismatch, structural rule broken,
//	  truncation, final digest mismatch.
//	ErrUnsupported - unknown version, suite, flags or key-slot type: refused
//	  by name, never called damage.
//
// I/O errors (class C) are returned unwrapped, as the underlying *os.PathError
// or its equivalent.
var (
	ErrDamaged            = errors.New("fdsec: damaged or truncated container")
	ErrCredentialOrTamper = errors.New("fdsec: wrong credential, or the container was tampered with")
	ErrUnsupported        = errors.New("fdsec: unsupported container")
)
