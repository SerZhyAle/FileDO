//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows half of the reveal sandbox: the access control list that makes
// "no other user of the machine can read the copy" true rather than asserted,
// the untrusted-origin mark, the exclusive-hold probe both lifetime mechanism
// 1 and the sweep use, and the hand-off to the registered handler.
//
// This is the repository's first security-descriptor code - nothing here had
// an existing helper to reuse. It adds no module: golang.org/x/sys was
// already a direct dependency.

// fdsecSandboxSDDL is the whole of the access decision, and it is short on
// purpose:
//
//	D:P   a DACL with inheritance switched off, so whatever the parent tree
//	      grants does not reach inside the sandbox;
//	(A;OICI;FA;;;SY)     SYSTEM, full - backup and the OS itself;
//	(A;OICI;FA;;;<user>) this user, full - and nobody else, which is the
//	      point. No Administrators entry: an administrator can take ownership
//	      anyway, and naming them here would only make the list look like it
//	      promises something it cannot.
//
// OICI propagates it to the file created inside, so the copy carries the same
// list without a second call.
func fdsecSandboxSDDL() (string, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", fmt.Errorf("cannot read this process's user: %w", err)
	}
	return "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;" + tu.User.Sid.String() + ")", nil
}

// fdsecSandboxCreate creates the per-reveal directory with its access control
// list already in place. The list is applied at creation rather than after,
// so there is never a moment - however short - when the directory exists
// under whatever the parent tree grants.
func fdsecSandboxCreate(dir string) error {
	sddl, err := fdsecSandboxSDDL()
	if err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("cannot build the sandbox security descriptor: %w", err)
	}
	sa := windows.SecurityAttributes{
		SecurityDescriptor: sd,
	}
	sa.Length = uint32(unsafe.Sizeof(sa))
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return err
	}
	return windows.CreateDirectory(p, &sa)
}

// fdsecMarkUntrusted writes the Zone.Identifier alternate data stream that
// marks the copy as having come from somewhere untrusted, so SmartScreen and
// the Office protections stay switched on for it. A reveal must not be a way
// to launder a file into a trusted one.
func fdsecMarkUntrusted(path string) error {
	return os.WriteFile(path+":Zone.Identifier", []byte("[ZoneTransfer]\r\nZoneId=3\r\n"), 0o644)
}

// fdsecFreeSpaceFor reports the free space on the volume holding dir, through
// the repository's existing helper rather than a seventh call to
// GetDiskFreeSpaceEx.
func fdsecFreeSpaceFor(dir string) (int64, error) {
	// A read-only query: the capacity tester's own free-space check now
	// writes a probe file, which has no business in the reveal root.
	free, _, err := diskSpaceQuery(dir)
	if err != nil {
		return 0, err
	}
	return clampToInt64(free), nil
}

// fdsecIsHeld reports whether something else has the file open in a way that
// refuses to share it - the exclusive-open probe stage S0's spike P3 measured
// at 0.01 s for a media player. A file that is not there is not held; an
// error that is not a sharing conflict is not a hold either, because treating
// "cannot tell" as "in use" would make leftovers immortal.
func fdsecIsHeld(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err == nil {
		windows.CloseHandle(h)
		return false
	}
	return err == windows.ERROR_SHARING_VIOLATION || err == windows.ERROR_LOCK_VIOLATION
}

// fdsecHoldLock creates the sandbox's lock file and keeps it open with no
// sharing, so fdsecIsHeld reports true for it in any other process. This is
// what lets a FileDO starting mid-reveal sweep the leftovers without deleting
// a sandbox that is in use.
func fdsecHoldLock(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, 0, nil,
		windows.CREATE_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// fdsecLaunch hands the copy to whatever the system has registered for its
// true extension. ShellExecute rather than a spawned command line: the point
// is the user's own default handler, not a guess at one. The working
// directory is the sandbox, so a handler that writes beside its input writes
// where the sweep will find it.
func fdsecLaunch(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	cwd, err := windows.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, verb, file, nil, cwd, windows.SW_SHOWNORMAL)
}
