//go:build !windows

package vdisk

import "os"

// lockFile is a no-op off Windows: the product runs there, and the package is
// only built elsewhere for its tests.
func lockFile(f *os.File, exclusive bool) (func(), error) {
	return func() {}, nil
}
