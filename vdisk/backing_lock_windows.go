package vdisk

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// lockOffset is a byte far past any container's end. Locking it, rather than
// the file, leaves every byte a reader or writer uses unlocked, while two
// processes that both want the container still meet on the same byte.
const lockOffset = 1 << 62

// lockFile takes the container lock: exclusive for a writer, shared for a
// reader, never waiting. A lock another process holds is ErrBusy.
func lockFile(f *os.File, exclusive bool) (func(), error) {
	h := windows.Handle(f.Fd())
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if exclusive {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	ol := &windows.Overlapped{Offset: uint32(lockOffset & 0xFFFFFFFF), OffsetHigh: uint32(lockOffset >> 32)}
	if err := windows.LockFileEx(h, flags, 0, 1, 0, ol); err != nil {
		if errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return nil, ErrBusy
		}
		return nil, ioErr(err)
	}
	return func() {
		ol := &windows.Overlapped{Offset: uint32(lockOffset & 0xFFFFFFFF), OffsetHigh: uint32(lockOffset >> 32)}
		windows.UnlockFileEx(h, 0, 1, 0, ol)
	}, nil
}
