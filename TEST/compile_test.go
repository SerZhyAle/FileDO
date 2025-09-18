package main

import "fmt"

func testCompile() {
	fmt.Println("Basic test compile")
}

// Test if we can see functions from other files
func testFunction() {
	// Try to call functions that should be in other files
	logger := NewHistoryLogger([]string{"test"})
	defer logger.Finish()
	
	interrupt := NewInterruptHandler()
	fmt.Printf("Interrupt handler: %v\n", interrupt != nil)
}