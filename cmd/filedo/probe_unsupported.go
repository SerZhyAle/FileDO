//go:build !windows

package main

import "fmt"

func runDeviceProbeCheck(devicePath string, assumeYes bool, autoRepair bool) error {
	return fmt.Errorf("probe operation is not supported on this operating system")
}

func runDeviceRecoverCheck(devicePath string, assumeYes bool, forceFormat bool) error {
	return fmt.Errorf("recover operation is not supported on this operating system")
}
