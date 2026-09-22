//go:build !windows

package main

import (
	"fmt"
	"os"
	"syscall"

	"filedo/fdsec"
)

// The non-Windows stubs. Reveal is a Windows feature in this build: the
// sandbox's guarantee is an access control list, the hand-off is the shell's
// registered handler, and the untrusted-origin mark is an alternate data
// stream. Each has an equivalent elsewhere and none of them is this one, so
// the honest answer here is a refusal by name rather than a weaker sandbox
// wearing the same word.

func fdsecSandboxCreate(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil {
		return err
	}
	return nil
}

func fdsecMarkUntrusted(path string) error {
	return fmt.Errorf("%w: marking a copy as untrusted-origin needs an alternate data stream", fdsec.ErrUnsupported)
}

func fdsecFreeSpaceFor(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// fdsecIsHeld has no portable answer: an advisory lock is not the exclusive
// open that mechanism 1 needs, and reporting "not held" is the safe direction
// here - it never keeps a leftover alive forever.
func fdsecIsHeld(path string) bool {
	return false
}

func fdsecHoldLock(path string) (*os.File, error) {
	return nil, fmt.Errorf("%w: the reveal lock needs an exclusive open", fdsec.ErrUnsupported)
}

func fdsecLaunch(path string) error {
	return fmt.Errorf("%w: handing a file to the registered handler is the Windows shell's job in this build", fdsec.ErrUnsupported)
}
