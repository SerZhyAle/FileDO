//go:build !windows

package fdsec

import (
	"errors"
	"os"
)

// moveIntoPlace renames tmp to dst. With replace false it never overwrites:
// a hard link fails atomically when dst exists, and only then is the
// temporary name removed (FDSEC-01).
func moveIntoPlace(tmp, dst string, replace bool) error {
	if replace {
		return os.Rename(tmp, dst)
	}
	if err := os.Link(tmp, dst); err != nil {
		if errors.Is(err, os.ErrExist) {
			return &os.LinkError{Op: "rename", Old: tmp, New: dst, Err: ErrExists}
		}
		return err
	}
	return os.Remove(tmp)
}
