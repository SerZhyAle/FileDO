package fdsec

import (
	"errors"
	"fmt"
)

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

// The two suites answer different questions, and a caller that asked one of a
// container holding the other gets a sentinel naming what the container is.
// Neither is an outcome class of section 12: the container authenticated and
// is perfectly readable - by the other entry point.
var (
	ErrTreeContainer = errors.New("fdsec: the container holds a folder, not a file")
	ErrFileContainer = errors.New("fdsec: the container holds one file, not a folder")
)

// Two more sentinels that are not outcome classes either. ErrExists: the
// destination name is taken - it existed before the pack, or it appeared
// while the pack ran - and nothing was written over it (FDSEC-01); the caller
// decides again, with its collision rule, before anything is disposed of.
// ErrStopped: the caller's stop function asked the stream to end at a chunk
// boundary (FDSEC-02); nothing complete was produced.
var (
	ErrExists  = errors.New("fdsec: destination already exists")
	ErrStopped = errors.New("fdsec: stopped by request")
)

// chunkBuffer sizes the buffer a reader opens chunks into: one sealed chunk,
// and for a one-chunk body only that chunk's real length - never the chunk
// size a header merely claims (FDSEC-10). A chunk too large to address in this
// build's int is refused as unsupported rather than panicking in make.
func chunkBuffer(k, capacity, last int64) ([]byte, error) {
	n := capacity
	if k <= 1 {
		n = last
	}
	n += tagSize
	if n < 0 || int64(int(n)) != n {
		return nil, fmt.Errorf("%w: a %d-byte chunk cannot be opened by this build", ErrUnsupported, n)
	}
	return make([]byte, n), nil
}
