package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// The system-drive guard (CLI-18, CLI-09; SP-0026 CAP-06/CAP-12 point here).
//
// It used to be `strings.ToLower(path) == "c:"`, so `C:\`, `c:/`, `\\?\C:\`,
// `\\localhost\C$`, a subst letter onto C:\ and a system drive that is not C:
// all wrote straight onto the system volume. The guard now asks the
// filesystem: a target is the system drive when it is the root of the volume
// that holds %SystemRoot%, however it was spelled. A folder below the root is
// the user's explicit choice and is left alone - that is what the GUI's
// "writes are redirected" notice mirrors (SP-0029 GUI-18).

var driveRootSpelling = regexp.MustCompile(`^[A-Za-z]:[\\/]?$`)

// deviceRootPath turns the device spellings `D:`, `d:\` and `D:/` into `D:\`.
// Anything else is returned unchanged. `D:` alone would otherwise mean D:'s
// per-process current directory, which is never what a device verb means.
func deviceRootPath(p string) string {
	if driveRootSpelling.MatchString(p) {
		return strings.ToUpper(p[:1]) + `:\`
	}
	return p
}

var (
	sysVolOnce sync.Once
	sysVolID   PathIdentity
	sysVolErr  error
)

// systemVolume is the identity of the root of the volume holding Windows.
func systemVolume() (PathIdentity, error) {
	sysVolOnce.Do(func() {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = os.Getenv("windir")
		}
		if root == "" {
			root = `C:\Windows`
		}
		id, err := pathIdentityOf(root)
		if err != nil {
			sysVolErr = err
			return
		}
		sysVolID, sysVolErr = pathIdentityOf(id.Volume)
	})
	return sysVolID, sysVolErr
}

// isSystemVolumeRoot reports whether p, in any spelling, is the root of the
// system volume. An unresolvable path is not.
func isSystemVolumeRoot(p string) bool {
	sys, err := systemVolume()
	if err != nil {
		// Without an identity to compare, fall back to the old literal rule
		// rather than to no guard at all.
		return strings.EqualFold(deviceRootPath(p), `C:\`)
	}
	id, err := pathIdentityOf(deviceRootPath(p))
	if err != nil {
		return false
	}
	return id.IsVolumeRoot() && id.VolSerial == sys.VolSerial
}

// onSystemVolume reports whether p lies anywhere on the system volume - the
// test the space budget uses (SP-0026 CAP-12), which applies to a folder on
// C: as much as to C:\ itself.
func onSystemVolume(p string) bool {
	sys, err := systemVolume()
	if err != nil {
		return false
	}
	resolved, err := resolvedPath(deviceRootPath(p))
	if err != nil {
		return false
	}
	// The nearest existing ancestor carries the volume serial.
	cur := resolved
	for {
		if id, err := pathIdentityOf(cur); err == nil {
			return id.VolSerial == sys.VolSerial
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return false
		}
		cur = parent
	}
}

// systemDriveRedirectDir is where writes aimed at the system drive go.
func systemDriveRedirectDir() string {
	tempDir := os.Getenv("TEMP")
	if tempDir == "" {
		tempDir = os.Getenv("TMP") // Fallback to TMP
	}
	if tempDir == "" {
		tempDir = `C:\TEMP` // Final fallback
	}
	return filepath.Join(tempDir, "FileDO_Operations")
}

// redirectedOperations are the target-verb words the guard applies to: the
// three that write test files, and clean, which must look where they wrote.
func redirectedOperation(op string) (writes, cleans bool) {
	switch strings.ToLower(op) {
	case "speed", "fill", "f", "test":
		return true, false
	case "cln", "clean", "c":
		return false, true
	}
	return false, false
}

// effectiveTarget is the one decision about where an operation really acts
// (CLI-09). A write operation on the system drive goes through the confirming
// redirect; clean on the system drive looks in the redirect folder, because
// that is where every earlier write went - no prompt, since clean removes only
// FileDO's own files. Everything else acts where it was pointed.
func effectiveTarget(op, path string) string {
	writes, cleans := redirectedOperation(op)
	switch {
	case writes:
		return redirectSystemDrive(path)
	case cleans && isSystemVolumeRoot(path) && os.Getenv("FILEDO_DISABLE_REDIRECT") != "1":
		dir := systemDriveRedirectDir()
		printRedirectCleanNote(dir)
		return dir
	}
	return path
}
