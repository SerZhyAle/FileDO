package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// The sandbox `unsecure start` restores into (SP-0008).
//
// `unsecure` restores beside the container, permanently, and that is right:
// the user asked for their file back. `unsecure start` - the "Unsecure and
// start" menu entry and the double-click on a .fd-sec - only ever wanted the
// file handed to its program once, and inherited the location and the
// permanence without a second thought. On a shared drive or in a Downloads
// folder another account can browse, that wrote the decrypted original in
// full view of exactly the audience the container was keeping it from.
//
// So `start` writes into the protected root `reveal` already uses and whose
// behaviour SP-0005 stage S4 already measured - %LOCALAPPDATA%\FileDO\reveal,
// not %TEMP%, because Storage Sense sweeps %TEMP% on a schedule nobody
// controls - in a per-run directory only this user and SYSTEM can read.
// Nothing here is a second implementation: the root, the access list, the
// free-space check, the sealed-name screen and the lock are reveal's, reused.
//
// Two things are deliberately NOT reveal's:
//
//   - the copy is not read-only and not marked untrusted-origin. A reveal's
//     copy is explicitly not fully trusted; this one is the file the user
//     just chose to unpack, launched with the same trust as an ordinary
//     double-click (SP-0008 4.2), executables included.
//   - the lifetime is one best-effort removal when the console window closes,
//     not reveal's three mechanisms (SP-0008 4.3, the owner's D2). A handler
//     still holding the file refuses that removal, and then the copy is left
//     for the startup sweep - which is why the sweep knows this prefix too.
const fdsecStartDirPrefix = "us-"

// fdsecStartSandbox is one run's directory, its lock and the copy inside it.
// The zero value is not usable; fdsecStartSandboxOpen makes one.
type fdsecStartSandbox struct {
	dir      string
	lockPath string
	lock     *os.File
	copyPath string

	scheduled bool // the close was handed to the console-hold queue
	once      sync.Once
	// removeCleanup unregisters the force-exit cleanup once close has run.
	removeCleanup func()
}

// fdsecStartRoot prepares the sandbox root and answers the free-space
// question before the credential is asked, so a run that cannot fit the
// original refuses without first making the user type a password.
//
// The original's real size is sealed and cannot be known this early, so the
// check uses the container's size, which is a strict upper bound on it -
// padding and tags only ever add.
func fdsecStartRoot(containerSize int64) (string, error) {
	root, err := fdsecRevealRoot()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	if free, ferr := fdsecFreeSpaceFor(root); ferr == nil && free < containerSize {
		return "", fmt.Errorf("not enough free space for the restored copy: %s free at %s, the container is %s and the original cannot be larger",
			formatBytes(uint64(free)), root, formatBytes(uint64(containerSize)))
	}
	return root, nil
}

// fdsecStartSandboxOpen takes the lock and creates the per-run directory with
// its access list already in place.
//
// The lock comes first, before the directory exists, and is a sibling of it
// rather than a file inside it - the same ordering reveal uses and for the
// same reason: a second FileDO starting in the gap would otherwise find a
// sandbox with no lock, call it a leftover, and delete a copy that is about
// to be used.
func fdsecStartSandboxOpen(root string) (*fdsecStartSandbox, error) {
	s := &fdsecStartSandbox{}
	s.dir = filepath.Join(root, fdsecStartDirPrefix+fdsecRandomName())
	s.lockPath = s.dir + fdsecRevealLockSuffix
	if lock, lerr := fdsecHoldLock(s.lockPath); lerr == nil {
		s.lock = lock
	}
	if err := fdsecSandboxCreate(s.dir); err != nil {
		s.releaseLock()
		return nil, fmt.Errorf("cannot create the sandbox for the restored copy %s: %w", s.dir, err)
	}
	// Force exit only. A graceful stop is observed by the restore itself, and
	// its deferred closeUnlessScheduled removes the sandbox once the
	// temporary file inside it is closed. Run on every stop, this cleanup
	// spent the sync.Once while the file was still open - the removal failed
	// and the later close then did nothing (FDSEC-02).
	s.removeCleanup = globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			s.close()
		}
	})
	return s, nil
}

// name resolves what the copy is called inside the sandbox. The sealed true
// name is attacker-controlled the moment somebody else built the container,
// so it goes through the same screen reveal puts it through - a path, an
// alternate data stream, a reserved device name or a trailing dot or space is
// refused before anything is written, not sanitised into something else.
func (s *fdsecStartSandbox) path(trueName string, random bool) (string, error) {
	if random {
		return filepath.Join(s.dir, fdsecRandomName()), nil
	}
	name, err := fdsecSandboxName(trueName)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.dir, name), nil
}

// scheduleClose decides when the one removal attempt is made (SP-0008 4.3).
//
// With a console to hold - the Explorer menu entry and the double-click both
// carry --pause - that is the instant the "Press Enter to close this window"
// wait returns, which is as close to "when the window closes" as a process
// can get. Without one - a .lst batch line, a run with no --pause, the GUI's
// child process - there is no window and nobody to wait for, so the attempt
// is made here, as soon as the launch call has returned.
func (s *fdsecStartSandbox) scheduleClose() {
	if s == nil {
		return
	}
	if willHoldConsole() {
		s.scheduled = true
		addAfterConsoleHold(s.close)
		fmt.Printf("The copy is removed when this window closes.\n")
		return
	}
	s.close()
}

// closeUnlessScheduled is the caller's defer: it removes a sandbox whose run
// ended before the copy was ever handed to anything - a failed unpack, a
// refused sealed name - and stands down once the removal has been scheduled
// for the end of the run instead.
func (s *fdsecStartSandbox) closeUnlessScheduled() {
	if s == nil || s.scheduled {
		return
	}
	s.close()
}

// close is the single best-effort attempt, and "best-effort" is exact rather
// than a hedge: the launched handler is very often still holding the copy
// open at this instant - SP-0005's probe P3 measured a media player taking an
// exclusive hold in 0.01 s - and Windows refuses to unlink a file another
// process holds without FILE_SHARE_DELETE. The attempt then fails, says so on
// stderr and nowhere else (the console is a heartbeat from closing and there
// is nobody left to answer a prompt), and the copy is left for the next
// FileDO start to sweep.
//
// The lock is released either way. A leftover this attempt could not remove
// must be reclaimable by that sweep, and the sweep steps around anything
// still locked.
func (s *fdsecStartSandbox) close() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		if s.copyPath == "" {
			fdsecRemoveSandbox(s.dir)
		} else if err := os.Remove(s.copyPath); err != nil && !os.IsNotExist(err) {
			// The directory, never the name inside it: the true name is
			// sealed metadata, and a note about a copy that outlives this
			// run is exactly the place not to spell it out.
			fmt.Fprintf(os.Stderr, "Note: the restored copy is still open elsewhere and could not be removed now. The next FileDO start removes it: %s\n", s.dir)
		} else {
			// Only when the copy is gone, and only if nothing else was
			// written beside it - os.Remove refuses a directory that is not
			// empty, which is the check rather than a separate one.
			os.Remove(s.dir)
		}
		s.releaseLock()
		if s.removeCleanup != nil {
			s.removeCleanup()
		}
	})
}

func (s *fdsecStartSandbox) releaseLock() {
	if s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
	if s.lockPath != "" {
		os.Remove(s.lockPath)
	}
}
