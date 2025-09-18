package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// runFolderCheck performs CHECK operation on a folder
func runFolderCheck(folderPath string) error {
	fmt.Printf("Starting CHECK operation on folder: %s\n", folderPath)
	
	// Validate folder path
	info, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder %s does not exist: %v", folderPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", folderPath)
	}
	
	// Get absolute path for consistency
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		absPath = folderPath
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(absPath)
}

// runFolderCheckQuick performs quick CHECK operation on a folder
func runFolderCheckQuick(folderPath string) error {
	fmt.Printf("Starting quick CHECK operation on folder: %s\n", folderPath)
	
	// Set quick mode environment variable
	os.Setenv("FILEDO_CHECK_MODE", "quick")
	
	// Validate folder path
	info, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder %s does not exist: %v", folderPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", folderPath)
	}
	
	// Get absolute path for consistency
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		absPath = folderPath
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(absPath)
}

// runFolderCheckDeep performs deep CHECK operation on a folder
func runFolderCheckDeep(folderPath string) error {
	fmt.Printf("Starting deep CHECK operation on folder: %s\n", folderPath)
	
	// Set deep mode environment variable
	os.Setenv("FILEDO_CHECK_MODE", "deep")
	
	// Validate folder path
	info, err := os.Stat(folderPath)
	if err != nil {
		return fmt.Errorf("folder %s does not exist: %v", folderPath, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", folderPath)
	}
	
	// Get absolute path for consistency
	absPath, err := filepath.Abs(folderPath)
	if err != nil {
		absPath = folderPath
	}
	
	// Call the main CHECK function from parent project
	return CheckFolder(absPath)
}