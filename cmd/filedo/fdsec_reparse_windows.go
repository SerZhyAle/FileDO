package main

import (
	"golang.org/x/sys/windows"
)

// Reparse tags (winnt.h) the container verbs distinguish.
const (
	reparseTagMountPoint  = 0xA0000003
	reparseTagSymlink     = 0xA000000C
	reparseTagAppExecLink = 0x8000001B
	reparseTagDedup       = 0x80000013
	reparseTagWOF         = 0x80000017
	reparseTagOneDrive    = 0x80000021
	reparseTagCloudMask   = 0xFFFF0FFF
	reparseTagCloud       = 0x9000001A
)

// fdsecRefusedReparse reports whether path is a reparse point the container
// verbs refuse as a source: a link of any kind (a symlink, a junction or mount
// point, an app execution alias), or a tag this code does not know.
//
// It classifies by the tag, not by the attribute. Every hydrated OneDrive file
// carries FILE_ATTRIBUTE_REPARSE_POINT with a cloud-files tag, and the old
// attribute test refused all of them as "a junction/symlink/mount point" -
// the wrong reason, for a file that is plainly data (FDSEC-11). Cloud files,
// deduplicated files and WOF-compressed files are data and are allowed. The
// wipe path keeps its own, stricter rule (SP-0027).
func fdsecRefusedReparse(path string) bool {
	name, err := windows.UTF16PtrFromString(longPathForAPI(path))
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return false
	}
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(name, &fd)
	if err != nil {
		// A reparse point whose tag cannot be read is refused: the safe
		// answer for a source that will be deleted after packing.
		return true
	}
	windows.FindClose(h)
	return !fdsecDataReparseTag(fd.Reserved0)
}

// fdsecDataReparseTag reports whether a reparse tag marks storage of the
// file's own data rather than a link to somewhere else.
func fdsecDataReparseTag(tag uint32) bool {
	switch {
	case tag&reparseTagCloudMask == reparseTagCloud:
		return true
	case tag == reparseTagDedup, tag == reparseTagWOF, tag == reparseTagOneDrive:
		return true
	}
	return false
}
