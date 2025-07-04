//go:build !windows

package main

import "fmt"

// getDeviceInfo is a placeholder function for unsupported operating systems.
func getDeviceInfo(path string) (DeviceInfo, error) {
	return DeviceInfo{}, fmt.Errorf("the 'device' command is not supported on this operating system")
}

func runDeviceSpeedTest(devicePath, sizeMBStr string, noDelete bool) error {
	return fmt.Errorf("device speed test is not supported on this operating system")
}
