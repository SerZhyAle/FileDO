//go:build !windows

package main

import (
	"io/fs"
	"time"
)

func getCreationTime(info fs.FileInfo) time.Time {
	return time.Time{}
}
