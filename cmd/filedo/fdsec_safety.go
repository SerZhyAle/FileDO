package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"

	"filedo/fdsec"
)

// Guards the container verbs share (SP-0025). Each one closes a way the old
// code could dispose of the only copy of something.

// fdsecOptionWords is the parser's option vocabulary - the words
// parseFdsecArgs takes as options rather than as a password. The redactor
// reads the same table, so a word the parser would take as the password is
// never one the redactor keeps (FDSEC-18). `to` is not here: it takes a path,
// and both handle it by name.
var fdsecOptionWords = map[string]bool{
	"del": true, "delete": true,
	"wipe":   true,
	"rename": true, "ren": true,
	"here":   true,
	"suite2": true,
	"start":  true,
	"-rw":    true, "rw": true,
	"-keep": true, "keep": true,
	"-y": true, "y": true, "--force": true, "force": true,
}

// errFdsecStopped is what a container verb returns when a stop request ended
// it: nothing further was packed, restored, deleted, wiped or launched.
var errFdsecStopped = fmt.Errorf("%w; nothing further was done", errRunStopped)

// fdsecStopped reports whether err means "stopped by request".
func fdsecStopped(err error) bool {
	return errors.Is(err, fdsec.ErrStopped) || errors.Is(err, errRunStopped)
}

// fdsecStreamStop is the stop request a pack or unpack observes at every chunk
// boundary (FDSEC-02).
func fdsecStreamStop() fdsec.StreamOption {
	return fdsec.WithStop(runStopRequested)
}

// fdsecSourceSnapshot is what the original looked like when it was packed.
// The disposition compares against it immediately before deleting, so an edit
// made during the read-back or while the y/N question waited is never deleted
// along with the old content the container holds (FDSEC-12).
type fdsecSourceSnapshot struct {
	size    int64
	modTime time.Time
	id      PathIdentity
	haveID  bool
}

func takeFdsecSnapshot(path string, fi os.FileInfo) fdsecSourceSnapshot {
	s := fdsecSourceSnapshot{size: fi.Size(), modTime: fi.ModTime()}
	if id, err := pathIdentityOf(path); err == nil {
		s.id, s.haveID = id, true
	}
	return s
}

// changedSince reports why the original no longer matches the snapshot, or ""
// when it still does.
func (s fdsecSourceSnapshot) changedSince(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("it can no longer be examined (%v)", err)
	}
	if fi.Size() != s.size || !fi.ModTime().Equal(s.modTime) {
		return "it changed after it was packed - the container holds the earlier content"
	}
	if s.haveID {
		if id, err := pathIdentityOf(path); err != nil || !id.SameObject(s.id) {
			return "it was replaced by another file after it was packed"
		}
	}
	return ""
}

// fdsecBeforeDisposition is a test seam: called with the original's path
// after the pack verified and before its disposition. nil in every shipped
// path, the same kind of seam as fdsec's beforeReadBack.
var fdsecBeforeDisposition func(path string)

// fdsecMaxNameUnits is the longest name component NTFS, exFAT and SMB accept,
// in UTF-16 code units.
const fdsecMaxNameUnits = 255

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// fdsecWindowsRefusedNameChars are U+0022, U+003C, U+003E and U+007C: valid in
// an authenticated sealed name (FDSEC-FORMAT 1.4 section 20 keeps them out of
// the format's damage checks) and impossible in a Windows file name. Reading a
// container and restoring it to this destination are separate questions; the
// first is fdsec.ValidateName, this is the second.
const fdsecWindowsRefusedNameChars = "\"<>|"

// fdsecDestinationNameMarker is the words every destination-name refusal below
// carries and the window looks for (filedo_win_vb\JobCatalogue.vb,
// FdsecResults.DestinationNameRefusal; a Go test reads that file for it), so
// the result card can say it in the user's language.
const fdsecDestinationNameMarker = "cannot be created on this PC"

// fdsecReasonDestinationName is the closed token of a destination-name refusal
// (FDSEC-BEHAVIOUR 1.7 section 19, CLI-EVENT-STREAM rule 17). It carries no
// name, path or credential and is written in two places: one stderr line,
// "reason=destination-name", after the human-readable error line, and
// details.reason on the error finding that ends the run, so a wrapper or the
// window can say why without parsing a sentence. Exit code 5 is unchanged.
const fdsecReasonDestinationName = "destination-name"

// fdsecReasonLine is the stderr line a wrapper may match exactly.
const fdsecReasonLine = "reason=" + fdsecReasonDestinationName

// fdsecDestinationNameError is a valid sealed name that this destination
// cannot create (FDSEC-BEHAVIOUR 1.6 section 18). It is the outcome class of an
// I/O or destination failure - exit 5 - and deliberately wraps no container
// class: a damaged-head or credential error would call readable user data
// corrupt. Its text is one sentence that names the rule, never the name, so it
// is safe on the console, in history.json and in the event stream alike.
type fdsecDestinationNameError struct{ msg string }

func (e *fdsecDestinationNameError) Error() string { return e.msg }

// fdsecNameUse says which restore is asking, because the way out differs: a
// file or a folder's root has `to <path>` and `rename`; a reveal has no
// alternate name of its own and points at `unsecure`; a name inside a folder
// container has no alternate at all.
type fdsecNameUse int

const (
	fdsecUseFile      fdsecNameUse = iota // unsecure of a file, unsecure start, a folder container's root
	fdsecUseReveal                        // reveal's sandbox copy
	fdsecUseTreeEntry                     // a file or folder name inside a folder container
)

// fdsecNameFitsDestination reports whether a sealed name can be created as one
// file name on a Windows file system: none of the four characters, and at most
// 255 UTF-16 units (NTFS, exFAT and SMB). The reserved device names, trailing
// dots and spaces, separators and controls are the format's own checks
// (fdsec.ValidateName) and stay damage, not a destination matter.
func fdsecNameFitsDestination(name string) bool {
	return !strings.ContainsAny(name, fdsecWindowsRefusedNameChars) && utf16Len(name) <= fdsecMaxNameUnits
}

// fdsecDestinationNameCheck is the one predicate behind every place a sealed
// name becomes a Windows path: nil when the name can be created, otherwise the
// destination-name error for the restore that asked. The error never holds the
// name.
func fdsecDestinationNameCheck(name string, use fdsecNameUse) error {
	if fdsecNameFitsDestination(name) {
		return nil
	}
	const rule = "(Windows refuses \" < > | in a name, and any name over 255 characters)"
	switch use {
	case fdsecUseReveal:
		return &fdsecDestinationNameError{"the name sealed in this container " + fdsecDestinationNameMarker + " " + rule +
			", so nothing was revealed and the container is untouched; restore it under a name you choose with unsecure to <path>, or under a random name with unsecure rename"}
	case fdsecUseTreeEntry:
		return &fdsecDestinationNameError{"a name sealed inside this folder container " + fdsecDestinationNameMarker + " " + rule +
			", so nothing was restored and the container is untouched; fdsec verify still proves it, and the folder can be restored on a system that can create those names"}
	}
	return &fdsecDestinationNameError{"the name sealed in this container " + fdsecDestinationNameMarker + " " + rule +
		", so nothing was restored and the container is untouched; choose the name yourself with to <path>, or restore under a random name with rename"}
}

// fdsecSealedNameIsUsed reports whether the restore described by o turns the
// container's sealed name into a file name. `rename` writes a random name and
// an exact `to <path>` (a destination that is not an existing folder) is the
// name the user chose - those are the ways out of a destination-name refusal,
// and they bypass the predicate. sandboxed is `unsecure start` without a
// destination of its own.
func fdsecSealedNameIsUsed(o *fdsecOpts, sandboxed bool) bool {
	if o.rename {
		return false
	}
	return sandboxed || !(o.haveTo && !fdsecIsDir(o.to))
}

// fdsecNameGate is the fdsec.WithNameCheck of a restore: it runs after the
// container authenticated and before the first byte of output exists, and it
// ends the restore with the destination-name error instead of a container class.
// used says whether this restore turns the sealed name into a file name at all.
func fdsecNameGate(use fdsecNameUse, used bool) fdsec.StreamOption {
	return fdsec.WithNameCheck(func(name string) error {
		if !used {
			return nil
		}
		return fdsecDestinationNameCheck(name, use)
	})
}

// fdsecSuffixedName returns the first free "stem-N.ext" beside path, keeping
// the component within the 255-unit limit by shortening the stem, and giving
// up after a bounded number of attempts or on any error other than "does not
// exist" - the old loop spun for ever on a 254-character name, because the
// suffixed name was invalid and Stat never said "not exist" (FDSEC-09).
func fdsecSuffixedName(path string, isDir bool) (string, error) {
	dir, base := filepath.Split(path)
	ext := ""
	if !isDir {
		ext = filepath.Ext(base)
	}
	stem := strings.TrimSuffix(base, ext)
	const maxAttempts = 10000
	for i := 1; i <= maxAttempts; i++ {
		suffix := fmt.Sprintf("-%d", i)
		s := stem
		for utf16Len(s+suffix+ext) > fdsecMaxNameUnits && len(s) > 0 {
			r := []rune(s)
			s = string(r[:len(r)-1])
		}
		if s == "" && utf16Len(suffix+ext) > fdsecMaxNameUnits {
			return "", usagef("no free name can be made beside %s within the 255-character limit; name the destination with to <path>", dir)
		}
		candidate := filepath.Join(dir, s+suffix+ext)
		_, err := os.Lstat(candidate)
		if os.IsNotExist(err) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("cannot examine %s for a free name: %w", dir, err)
		}
	}
	return "", fmt.Errorf("no free suffixed name after %d attempts in %s; name the destination with to <path>", maxAttempts, dir)
}
