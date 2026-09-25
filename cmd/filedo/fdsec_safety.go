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
