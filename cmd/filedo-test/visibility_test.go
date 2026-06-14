package main

import "fmt"

// Minimal test to check if device_test functions are visible

func testVisibilityMain() {
	fmt.Println("Testing function visibility...")
	
	// This should work if runDeviceCapacityTest is defined properly
	// err := runDeviceCapacityTest("C:", false, nil)
	// if err != nil {
	// 	fmt.Printf("runDeviceCapacityTest error: %v\n", err)
	// }
}

func testVisibility() {
	fmt.Println("Compile test passed")
}