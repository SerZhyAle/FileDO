package fsx

import (
	"errors"
	"fmt"
	"os"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procReplaceFileW = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReplaceFileW")

// ReplaceOriginal commits the partial over the existing file it was made from.
// ReplaceFileW keeps that file's creation time, security descriptor and named
// streams; the attributes are restored here, and modTime becomes the
// modification time. A volume without ReplaceFileW gets a plain replacing
// rename.
//
// The new bytes are never thrown away while the final name is missing: if a
// failed replace removed it, the partial takes the name, and if even that
// fails the partial stays on disk and the error names it.
func (p *PartialFile) ReplaceOriginal(modTime time.Time) error {
	if p.done {
		return errors.New("partial file already finished")
	}
	final, err := windows.UTF16PtrFromString(LongPath(p.final))
	if err != nil {
		p.Abort()
		return err
	}
	partial, err := windows.UTF16PtrFromString(LongPath(p.partial))
	if err != nil {
		p.Abort()
		return err
	}
	attrs, err := windows.GetFileAttributes(final)
	if err != nil {
		p.Abort()
		return &os.PathError{Op: "replace", Path: p.final, Err: err}
	}
	if !modTime.IsZero() {
		ft := windows.NsecToFiletime(modTime.UnixNano())
		_ = windows.SetFileTime(windows.Handle(p.File.Fd()), nil, &ft, &ft)
	}
	if err := p.File.Sync(); err != nil {
		p.Abort()
		return err
	}
	if err := p.File.Close(); err != nil {
		p.Abort()
		return err
	}

	ok, _, callErr := procReplaceFileW.Call(
		uintptr(unsafe.Pointer(final)), uintptr(unsafe.Pointer(partial)), 0, 0, 0, 0)
	if ok == 0 {
		err = &os.LinkError{Op: "replace", Old: p.partial, New: p.final, Err: callErr}
		if errors.Is(callErr, windows.ERROR_INVALID_FUNCTION) || errors.Is(callErr, windows.ERROR_NOT_SUPPORTED) {
			err = RenameReplace(p.partial, p.final)
		}
	}
	if err != nil {
		if _, serr := os.Lstat(p.final); errors.Is(serr, os.ErrNotExist) {
			if rerr := RenameNoReplace(p.partial, p.final); rerr != nil {
				p.done = true
				return fmt.Errorf("%w; the new content is kept in %q", err, p.partial)
			}
		} else {
			p.Abort()
			return err
		}
	}
	p.done = true
	// ReplaceFileW writes the named streams into the new file, which moves its
	// modification time, so the time is set again under the final name.
	if !modTime.IsZero() {
		_ = os.Chtimes(LongPath(p.final), modTime, modTime)
	}
	_ = windows.SetFileAttributes(final, attrs)
	return nil
}
