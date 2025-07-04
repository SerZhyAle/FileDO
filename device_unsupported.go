//go:build !windows

package main

import "fmt"

// getDeviceInfo is a placeholder function for unsupported operating systems.
func getDeviceInfo(path string, fullScan bool) (DeviceInfo, error) {
	return DeviceInfo{}, fmt.Errorf("the 'device' command is not supported on this operating system")
}

func runDeviceSpeedTest(devicePath, sizeMBStr string, noDelete, shortFormat bool) error {
	return fmt.Errorf("device speed test is not supported on this operating system")
}

func runDeviceFill(devicePath, sizeMBStr string, autoDelete bool) error {
	return fmt.Errorf("device fill operation is not supported on this operating system")
}

func runDeviceFillClean(devicePath string) error {
	return fmt.Errorf("device fill clean operation is not supported on this operating system")
}

func runDeviceTest(devicePath string, autoDelete bool) error {
	return fmt.Errorf("device test operation is not supported on this operating system")
}
