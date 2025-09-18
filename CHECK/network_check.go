package main

import (
	"fmt"
	"os"
)

// runNetworkCheck performs CHECK operation on a network path
func runNetworkCheck(networkPath string, logger *HistoryLogger) error {
	fmt.Printf("Starting CHECK operation on network path: %s\n", networkPath)
	
	// Validate network path accessibility
	if _, err := os.Stat(networkPath); err != nil {
		return fmt.Errorf("network path %s is not accessible: %v", networkPath, err)
	}
	
	// Log network check parameters if logger is provided
	if logger != nil {
		logger.SetParameter("networkPath", networkPath)
		logger.SetParameter("mode", os.Getenv("FILEDO_CHECK_MODE"))
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(networkPath)
}

// runNetworkCheckQuick performs quick CHECK operation on a network path
func runNetworkCheckQuick(networkPath string, logger *HistoryLogger) error {
	fmt.Printf("Starting quick CHECK operation on network path: %s\n", networkPath)
	
	// Set quick mode environment variable
	os.Setenv("FILEDO_CHECK_MODE", "quick")
	
	// Validate network path accessibility
	if _, err := os.Stat(networkPath); err != nil {
		return fmt.Errorf("network path %s is not accessible: %v", networkPath, err)
	}
	
	// Log network check parameters if logger is provided
	if logger != nil {
		logger.SetParameter("networkPath", networkPath)
		logger.SetParameter("mode", "quick")
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(networkPath)
}

// runNetworkCheckDeep performs deep CHECK operation on a network path
func runNetworkCheckDeep(networkPath string, logger *HistoryLogger) error {
	fmt.Printf("Starting deep CHECK operation on network path: %s\n", networkPath)
	
	// Set deep mode environment variable
	os.Setenv("FILEDO_CHECK_MODE", "deep")
	
	// Validate network path accessibility
	if _, err := os.Stat(networkPath); err != nil {
		return fmt.Errorf("network path %s is not accessible: %v", networkPath, err)
	}
	
	// Log network check parameters if logger is provided
	if logger != nil {
		logger.SetParameter("networkPath", networkPath)
		logger.SetParameter("mode", "deep")
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(networkPath)
}