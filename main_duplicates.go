package main

import (
	"fmt"
	"time"

	"filedo/fileduplicates"
)

// findDuplicatesUsingPackage is a common function that utilizes the fileduplicates package
func findDuplicatesUsingPackage(rootPath string, args []string) error {
	startTime := time.Now()

	// Parse options from command line arguments
	options := fileduplicates.ParseArguments(args)

	// Run the duplicate finder
	result, err := fileduplicates.FindDuplicates(rootPath, options)
	if err != nil {
		return err
	}

	// Print results
	if options.Verbose {
		fmt.Printf("\nScan completed in %s\n", formatDuration(time.Since(startTime)))
		fmt.Printf("Scanned %d files\n", result.TotalFiles)
		fmt.Printf("Found %d duplicate groups with %d files (%.2f GB)\n",
			result.DuplicateGroups,
			result.DuplicateFiles,
			float64(result.DuplicateSize)/(1024*1024*1024))
	}

	// Process duplicates according to options
	if options.Action != fileduplicates.NoAction && result.DuplicateFiles > 0 {
		// Convert map to array of arrays for processing
		var duplicateGroups [][]fileduplicates.DuplicateFileInfo

		for _, group := range result.Groups {
			if len(group) > 1 {
				duplicateGroups = append(duplicateGroups, group)
			}
		}

		// Process the duplicate groups
		fileduplicates.ProcessDuplicateGroups(duplicateGroups, options)
	}

	return nil
}

// runDeviceCheckDuplicates performs duplicate file check on a device
func runDeviceCheckDuplicates(devicePath string, args []string) error {
	fmt.Printf("Checking for duplicate files on device: %s\n", devicePath)

	// Convert device path to directory format
	deviceDir := devicePath
	if len(devicePath) == 2 && devicePath[1] == ':' {
		deviceDir = devicePath + "\\"
	}

	return findDuplicatesUsingPackage(deviceDir, args)
}

// runFolderCheckDuplicates performs duplicate file check in a folder
func runFolderCheckDuplicates(folderPath string, args []string) error {
	fmt.Printf("Checking for duplicate files in folder: %s\n", folderPath)
	return findDuplicatesUsingPackage(folderPath, args)
}

// runNetworkCheckDuplicates performs duplicate file check on a network path
func runNetworkCheckDuplicates(networkPath string, args []string, logger *HistoryLogger) error {
	fmt.Printf("Checking for duplicate files on network path: %s\n", networkPath)
	return findDuplicatesUsingPackage(networkPath, args)
}
