//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

// hasReparsePoint reports whether the path itself is a reparse point
// (directory junction, symlink, or volume mount point). GetFileAttributes does
// not follow the reparse point, so the attribute reflects the path as named.
func hasReparsePoint(path string) bool {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(pathPtr)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0
}
