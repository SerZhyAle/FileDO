//go:build windows

package fdsec

import (
	"os"

	"golang.org/x/sys/windows"
)

// moveIntoPlace renames tmp to dst. With replace false it never overwrites:
// Go's os.Rename on Windows is MoveFileEx(REPLACE_EXISTING), which let two
// concurrent secures to one container name silently replace each other's
// container and then both delete their originals (FDSEC-01). An existing dst
// is reported as ErrExists and nothing moves.
func moveIntoPlace(tmp, dst string, replace bool) error {
	from, err := windows.UTF16PtrFromString(tmp)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(from, to, flags); err != nil {
		if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_FILE_EXISTS {
			return &os.LinkError{Op: "rename", Old: tmp, New: dst, Err: ErrExists}
		}
		return &os.LinkError{Op: "rename", Old: tmp, New: dst, Err: err}
	}
	return nil
}
