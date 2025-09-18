package main

import (
	"fmt"
	"os"
)

// runDeviceCheck performs CHECK operation on a device/drive
func runDeviceCheck(devicePath string) error {
	fmt.Printf("Starting CHECK operation on device: %s\n", devicePath)
	
	// Ensure the drive exists and is accessible
	if _, err := os.Stat(devicePath + "\\"); err != nil {
		return fmt.Errorf("device %s is not accessible: %v", devicePath, err)
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(devicePath + "\\")
}

// runDeviceCheckQuick performs quick CHECK operation on a device/drive
func runDeviceCheckQuick(devicePath string) error {
	fmt.Printf("Starting quick CHECK operation on device: %s\n", devicePath)
	
	// Set quick mode environment variable
	os.Setenv("FILEDO_CHECK_MODE", "quick")
	
	// Ensure the drive exists and is accessible
	if _, err := os.Stat(devicePath + "\\"); err != nil {
		return fmt.Errorf("device %s is not accessible: %v", devicePath, err)
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(devicePath + "\\")
}

// runDeviceCheckDeep performs deep CHECK operation on a device/drive
func runDeviceCheckDeep(devicePath string) error {
	fmt.Printf("Starting deep CHECK operation on device: %s\n", devicePath)
	
	// Set deep mode environment variable
	os.Setenv("FILEDO_CHECK_MODE", "deep")
	
	// Ensure the drive exists and is accessible
	if _, err := os.Stat(devicePath + "\\"); err != nil {
		return fmt.Errorf("device %s is not accessible: %v", devicePath, err)
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(devicePath + "\\")
}