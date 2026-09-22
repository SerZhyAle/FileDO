package fdsec

import "errors"

// The three outcome classes of FDSEC-FORMAT.md section 12. They are produced by
// separate code paths and must never be reported as each other:
//
//	ErrCredentialOrTamper (class A) - the head did not open, or an AEAD tag
//	  failed later on. Because the head carries no marker and no stored
//	  verifier, a wrong credential, a file that was never a container and a
//	  damaged head are one outcome by construction, and the message says all
//	  three rather than choosing one.
//	ErrDamaged (class B) - a structural rule broken after the container
//	  authenticated, truncation, a file too short to hold a head, or a final
//	  digest mismatch.
//	ErrUnsupported - unknown version, suite, flags or key-slot type in a
//	  container that did authenticate: refused by name, never called damage.
//
// I/O errors (class C) are returned unwrapped, as the underlying *os.PathError
// or its equivalent.
var (
	ErrDamaged            = errors.New("fdsec: damaged or truncated container")
	ErrCredentialOrTamper = errors.New("fdsec: wrong credential, not a container, or it was tampered with")
	ErrUnsupported        = errors.New("fdsec: unsupported container")
)
