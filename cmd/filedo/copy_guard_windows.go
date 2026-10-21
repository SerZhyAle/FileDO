package main

import (
	"path/filepath"

	"golang.org/x/sys/windows"
)

// reparseTagNameSurrogate is the bit (winnt.h IsReparseTagNameSurrogate) a
// reparse tag carries when the point stands for another name - a junction, a
// volume mount point, a symbolic link. Cloud, dedup and WOF tags do not carry
// it: those hold the file's own data and are copied as what they are.
const reparseTagNameSurrogate = 0x20000000

// copySourceRootLink reports whether a copy source, as named, is itself a link
// to another folder (AUD-07-F1): a junction, a volume mount point or a
// directory symbolic link. kind names it for the refusal message.
//
// A trailing separator is dropped first, so `link\` is judged as `link` - the
// name the user gave, not the folder behind it. A volume root is never a link.
func copySourceRootLink(source string) (kind string, isLink bool) {
	p := longPathForAPI(filepath.Clean(source))
	name, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	attrs, err := windows.GetFileAttributes(name)
	if err != nil || attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return "", false
	}
	var fd windows.Win32finddata
	h, err := windows.FindFirstFile(name, &fd)
	if err != nil {
		return "", false
	}
	windows.FindClose(h)
	tag := fd.Reserved0
	switch {
	case tag == reparseTagMountPoint:
		return "a junction or volume mount point", true
	case tag == reparseTagSymlink:
		return "a directory symbolic link", true
	case tag&reparseTagNameSurrogate != 0:
		return "a link", true
	}
	return "", false
}
