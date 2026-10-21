package main

import (
	"fmt"
	"os"
	"time"

	"filedo/fileduplicates"

	"golang.org/x/term"
)

// configureDuplicateRun tells the duplicate finder what only this process
// knows: whether a person can answer its prompts, and how a stop arrives.
//
// A person can answer when stdin is a console and the caller did not start
// us with a machine stop channel (the GUI's --stop-file): a run like that
// has nobody at the keyboard, so a deleting run needs -y or is refused before
// it touches anything (DUP-03). The stop is the one stop model every verb
// obeys - Ctrl+C and the stop file both end the scan and the delete/move
// phase (DUP-07).
func configureDuplicateRun(o *fileduplicates.DuplicateOptions) {
	o.Interactive = term.IsTerminal(int(os.Stdin.Fd())) && !machineStopChannel
	if globalInterruptHandler != nil {
		o.Context = globalInterruptHandler.Context()
	}
	o.Stop = runStopRequested
}

// recordDuplicateNumbers puts what the run found and did into result.numbers.
func recordDuplicateNumbers(result *fileduplicates.DuplicateResult) {
	if result == nil {
		return
	}
	runNumber("filesScanned", result.TotalFiles)
	runNumber("duplicateGroups", result.DuplicateGroups)
	runNumber("duplicateFiles", result.DuplicateFiles)
	runNumber("duplicateBytes", result.DuplicateSize)
	a := result.Actions
	if a.Deleted+a.Moved+a.Skipped+a.Refused+a.Failed > 0 {
		runNumber("deleted", a.Deleted)
		runNumber("moved", a.Moved)
		runNumber("skipped", a.Skipped)
		runNumber("refused", a.Refused)
		runNumber("failed", a.Failed)
	}
}

// findDuplicatesUsingPackage is a common function that utilizes the fileduplicates package
func findDuplicatesUsingPackage(rootPath string, args []string) error {
	startTime := time.Now()

	// Parse options from command line arguments
	options := fileduplicates.ParseArguments(args)
	configureDuplicateRun(&options)

	// Run the duplicate finder. It checks the root itself (DUP-16), so a
	// device target that skipped the generic path probe still ends in an
	// error rather than in "no duplicate files found".
	result, err := fileduplicates.FindDuplicates(rootPath, options)
	recordDuplicateNumbers(result)
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

	return nil
}

// runDeviceCheckDuplicates performs duplicate file check on a device
func runDeviceCheckDuplicates(devicePath string, args []string) error {
	fmt.Printf("Checking for duplicate files on device: %s\n", devicePath)

	// `D:` alone is D:'s current directory; a device verb means its root.
	return findDuplicatesUsingPackage(deviceRootPath(devicePath), args)
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
