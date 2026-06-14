package main

import "fmt"

func testMain() {
	fmt.Println("Simple test")
}

func testFunctionVisibility() {
	// These should be visible if files compile correctly
	logger := NewHistoryLogger([]string{"test"})
	defer logger.Finish()
	
	err := runDeviceCapacityTest("C:", false, logger)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}