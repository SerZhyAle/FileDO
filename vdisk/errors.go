package vdisk

import (
	"errors"
	"fmt"
)

// The error taxonomy. Every failure this package returns satisfies exactly one
// of these sentinels under errors.Is, and each maps to one outcome class of
// FDD-BEHAVIOUR section 4 (ExitClass). The classes never merge: a wrong
// credential is never reported as damage, and damage never as a wrong
// credential (FDD-FORMAT section 12).
var (
	// ErrDamaged: neither header opens (including a file that is not a
	// container, or one of another build family), a file shorter than any
	// container, or a structural check failing after the header opened.
	ErrDamaged = errors.New("vdisk: damaged container, or not a container")
	// ErrCredential: an encrypted container whose header opened and on which
	// no slot opens under the credential given. Never produced for an
	// obfuscated container.
	ErrCredential = errors.New("vdisk: the credential did not open this container")
	// ErrIO: a missing file, no space, permission denied, a failed flush.
	ErrIO = errors.New("vdisk: I/O error")
	// ErrUnsupported: a version, profile, flag, KDF parameter set or slot kind
	// this build does not know, a write this build may not make, or an
	// operation this build does not carry.
	ErrUnsupported = errors.New("vdisk: unsupported")
	// ErrBusy: another process holds the container.
	ErrBusy = errors.New("vdisk: the container is in use by another process")
	// ErrUsage: a request the caller got wrong - a bad size, an offset past
	// the end of the volume, a destination that exists.
	ErrUsage = errors.New("vdisk: invalid request")
	// ErrStopped: the caller's context ended a long operation. The container is
	// left in the named state its winning header describes.
	ErrStopped = errors.New("vdisk: stopped by request")
)

// Exit classes of FDD-BEHAVIOUR section 4 (and FD-SEC-CONTRACT section 7.1 for
// 2-6, with the same digits). ExitStopped is not a container class: a stopped
// run ends with the supervisor's Stopped verdict.
const (
	ExitSuccess     = 0
	ExitUsage       = 2
	ExitCredential  = 3
	ExitDamaged     = 4
	ExitIO          = 5
	ExitUnsupported = 6
	ExitBusy        = 8
	ExitStopped     = -1
)

// ExitClass maps an error of this package to its outcome class. An error that
// carries no sentinel of this package is an I/O error: everything the package
// itself decides is classified where it is decided.
func ExitClass(err error) int {
	switch {
	case err == nil:
		return ExitSuccess
	case errors.Is(err, ErrStopped):
		return ExitStopped
	case errors.Is(err, ErrCredential):
		return ExitCredential
	case errors.Is(err, ErrDamaged):
		return ExitDamaged
	case errors.Is(err, ErrUnsupported):
		return ExitUnsupported
	case errors.Is(err, ErrBusy):
		return ExitBusy
	case errors.Is(err, ErrUsage):
		return ExitUsage
	default:
		return ExitIO
	}
}

// ioError keeps the underlying error (an *os.PathError, say) reachable through
// errors.As while classifying it as ErrIO.
type ioError struct{ err error }

func (e *ioError) Error() string        { return "vdisk: I/O error: " + e.err.Error() }
func (e *ioError) Unwrap() error        { return e.err }
func (e *ioError) Is(target error) bool { return target == ErrIO }

// ioErr classifies an error from the backing store. An error that already
// carries a sentinel of this package (a stop, a failure decided below) passes
// through unchanged.
func ioErr(err error) error {
	if err == nil {
		return nil
	}
	for _, s := range []error{ErrDamaged, ErrCredential, ErrIO, ErrUnsupported, ErrBusy, ErrUsage, ErrStopped} {
		if errors.Is(err, s) {
			return err
		}
	}
	return &ioError{err: err}
}

func damagedf(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrDamaged, fmt.Sprintf(format, args...))
}

func unsupportedf(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrUnsupported, fmt.Sprintf(format, args...))
}

func usagef(format string, args ...interface{}) error {
	return fmt.Errorf("%w: %s", ErrUsage, fmt.Sprintf(format, args...))
}

// The failures a person meets at the door, each with the one sentence a
// surface prints for it (SP-0004 P5 section 5). The sentences are the
// feature: they never merge a wrong credential with damage, and never hint at
// how close an attempt was.
const (
	msgCredential = "The credential did not open this container."
	msgHeaders    = "This container is damaged: the header did not verify. Nothing was changed.\n" +
		"A file that is not a container, or one written by another build family, reads the same way."
	msgTruncated = "This container is incomplete: the file is shorter than its header describes (%d bytes missing)."
	msgVault     = "A vault container is encrypted: its data key lives only in credential-wrapped slots, " +
		"so losing every credential loses the data. It needs a credential; without one a container is only obfuscated."
)

// namedError is a failure whose message is the sentence itself, classified
// by the sentinel it carries.
type namedError struct {
	class error
	msg   string
	cause error
}

func (e *namedError) Error() string        { return e.msg }
func (e *namedError) Is(target error) bool { return target == e.class }
func (e *namedError) Unwrap() error        { return e.cause }

// ErrVaultNeedsCredential refuses `new .. vault` without a credential
// (FDD-BEHAVIOUR 7 rule 3): refused with the reason, never silently made
// obfuscated instead. A usage error.
var ErrVaultNeedsCredential error = &namedError{class: ErrUsage, msg: msgVault}

var errHeadersDamaged error = &namedError{class: ErrDamaged, msg: msgHeaders}

// truncatedf is a file shorter than its header describes, where the header
// makes the shortfall computable; detail is kept for the log.
func truncatedf(missing uint64, detail error) error {
	return &namedError{class: ErrDamaged, msg: fmt.Sprintf(msgTruncated, missing), cause: detail}
}

// Explain returns the sentence a surface prints for err: the named sentence
// of a door failure, and the error's own text for anything else.
func Explain(err error) string {
	var n *namedError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &n):
		return n.msg
	case errors.Is(err, ErrCredential):
		return msgCredential
	}
	return err.Error()
}
