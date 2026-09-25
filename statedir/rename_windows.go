package statedir

import (
	"os"

	"golang.org/x/sys/windows"
)

// renameNoReplace moves src to dst and fails if dst exists. Go's os.Rename on
// Windows is MoveFileEx with MOVEFILE_REPLACE_EXISTING, which is exactly the
// silent overwrite an import must never do.
func renameNoReplace(src, dst string) error {
	from, err := windows.UTF16PtrFromString(src)
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(dst)
	if err != nil {
		return err
	}
	if err := windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH); err != nil {
		if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_FILE_EXISTS {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: os.ErrExist}
		}
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}
