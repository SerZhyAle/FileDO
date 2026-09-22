//go:build windows

package main

import (
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"filedo/fdsec"
)

// fdsecMetadataFromStat builds the pack metadata from a file's stat: the
// original's own timestamps travel inside the sealed metadata (FDSEC-FORMAT.md
// section 7), so they must be read, not invented.
func fdsecMetadataFromStat(fi os.FileInfo) fdsec.Metadata {
	meta := fdsec.Metadata{
		Name:       fi.Name(),
		Size:       fi.Size(),
		ModifiedAt: fi.ModTime(),
	}
	if d, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		meta.CreatedAt = time.Unix(0, d.CreationTime.Nanoseconds())
		meta.AccessedAt = time.Unix(0, d.LastAccessTime.Nanoseconds())
	}
	return meta
}

// fdsecRestoreTimes restores the original's timestamps to the recovered file,
// best-effort: modification and access via Chtimes, creation via
// SetFileTime. Failures are ignored - timestamps are fidelity, not safety.
func fdsecRestoreTimes(path string, m fdsec.Metadata) {
	_ = os.Chtimes(path, m.AccessedAt, m.ModifiedAt)
	if m.CreatedAt.IsZero() {
		return
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	h, err := windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	c := windows.NsecToFiletime(m.CreatedAt.UnixNano())
	a := windows.NsecToFiletime(m.AccessedAt.UnixNano())
	w := windows.NsecToFiletime(m.ModifiedAt.UnixNano())
	_ = windows.SetFileTime(h, &c, &a, &w)
}
