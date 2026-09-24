//go:build !windows

package main

import (
	"fmt"

	"filedo/fdsec"
)

// The Explorer registration is Windows and nothing else: it is a set of keys
// under Software\Classes, read by one shell. Other desktops have their own
// mechanism (a .desktop entry and a MIME type), and calling that the same
// thing would be a claim this build has not earned - so the refusal names the
// platform instead of writing a weaker equivalent.

// Package identity is a Windows notion; no other build has one.
func fdsecHasPackageIdentity() bool { return false }

func fdsecShellRegister(allUsers bool) error {
	return fmt.Errorf("%w: registering a file type and an Explorer verb is a Windows operation", fdsec.ErrUnsupported)
}

func fdsecShellUnregister(allUsers bool) error {
	return fmt.Errorf("%w: registering a file type and an Explorer verb is a Windows operation", fdsec.ErrUnsupported)
}
