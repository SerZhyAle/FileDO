//go:build !windows

package main

import "os"

// hasReparsePoint reports whether the path is a symlink on non-Windows systems.
func hasReparsePoint(path string) bool {
	li, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return li.Mode()&os.ModeSymlink != 0
}
