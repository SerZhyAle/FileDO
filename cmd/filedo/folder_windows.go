//go:build windows

package main

import (
	"io/fs"
	"syscall"
	"time"
)

func getCreationTime(info fs.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, stat.CreationTime.Nanoseconds())
	}
	return time.Time{}
}
