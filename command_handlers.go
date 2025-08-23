package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// isPathAccessible checks if a path exists and is accessible
func isPathAccessible(path string) bool {
	// Check for network paths specially
	if len(path) > 2 && (path[0:2] == "\\" || path[0:2] == "//") {
		// For network paths, we need to check if they are accessible
		// If it's a UNC path, just check if we can stat it
		_, err := os.Stat(path)
		return err == nil
	}

	// For local paths
	_, err := os.Stat(path)
	return err == nil
}

// handleErrorWithUserMessage handles errors with user-friendly messages
// and returns true if the error was handled
func handleErrorWithUserMessage(err error, path string, historyLogger *HistoryLogger) bool {
	if err == nil {
		return false
	}

	historyLogger.SetError(err)
	errMsg := err.Error()

	// Handle common error patterns with user-friendly messages
	if strings.Contains(errMsg, "device") && strings.Contains(errMsg, "does not exist") {
		fmt.Printf("Info: Device \"%s\" does not exist.\n", path)
		return true
	} else if strings.Contains(errMsg, "file") && strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "system cannot find the file") {
		fmt.Printf("Info: File \"%s\" does not exist or is not accessible.\n", path)
		return true
	} else if strings.Contains(errMsg, "folder") && strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "directory") && strings.Contains(errMsg, "not found") ||
		strings.Contains(errMsg, "system cannot find the path") {
		fmt.Printf("Info: Folder \"%s\" does not exist or is not accessible.\n", path)
		return true
	} else if strings.Contains(errMsg, "network") && strings.Contains(errMsg, "not accessible") {
		fmt.Printf("Info: Network path \"%s\" is not accessible.\n", path)
		return true
	}

	// Default error handling
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
	return true
}

// redirectSystemDrive redirects C: to user's temp directory for write operations with user confirmation
func redirectSystemDrive(path string) string {
	if strings.ToLower(path) == "c:" {
		// Check if redirection is disabled by environment variable
		if os.Getenv("FILEDO_DISABLE_REDIRECT") == "1" {
			fmt.Printf("⚠️  System drive redirection disabled by FILEDO_DISABLE_REDIRECT=1\n")
			fmt.Printf("   WARNING: Writing directly to C: - use with caution!\n")
			return path
		}
		
		// Get user's temp directory from environment
		tempDir := os.Getenv("TEMP")
		if tempDir == "" {
			tempDir = os.Getenv("TMP") // Fallback to TMP
		}
		if tempDir == "" {
			tempDir = "C:\\TEMP" // Final fallback
		}
		
		// Create subdirectory for FileDO operations
		fileDoTempDir := filepath.Join(tempDir, "FileDO_Operations")
		
		// Show warning and ask for confirmation
		fmt.Printf("⚠️  WARNING: Write operation requested on system drive C:\n")
		fmt.Printf("   For safety, redirecting to temporary directory:\n")
		fmt.Printf("   %s\n\n", fileDoTempDir)
		fmt.Printf("   This protects your system from potential issues during testing.\n")
		fmt.Printf("   Test files will be created in this safe location instead.\n\n")
		
		var response string
		// Check for auto-confirm environment variable for testing
		if os.Getenv("FILEDO_AUTO_CONFIRM") == "1" {
			fmt.Printf("Auto-confirming redirection (FILEDO_AUTO_CONFIRM=1)\n")
			response = "y"
		} else {
			fmt.Printf("Continue with redirection? (Y/n): ")
			fmt.Scanln(&response)
		}
		response = strings.TrimSpace(strings.ToLower(response))
		
		// Default to Yes if empty input or 'y'
		if response == "" || response == "y" || response == "yes" {
			// Create the directory if it doesn't exist
			if _, err := os.Stat(fileDoTempDir); os.IsNotExist(err) {
				if err := os.MkdirAll(fileDoTempDir, 0755); err != nil {
					fmt.Fprintf(os.Stderr, "Error: Could not create %s: %v\n", fileDoTempDir, err)
					fmt.Fprintf(os.Stderr, "Falling back to original path (use at your own risk).\n")
					return path
				}
				fmt.Printf("✓ Created safe directory: %s\n", fileDoTempDir)
			}
			fmt.Printf("✓ Using safe location: %s\n", fileDoTempDir)
			return fileDoTempDir
		} else {
			fmt.Printf("⚠️  User chose to proceed with system drive C: directly.\n")
			fmt.Printf("   WARNING: This may affect system stability or performance.\n")
			return path
		}
	}
	return path
}

// CommandType represents the command type
type CommandType int

const (
	CommandDevice CommandType = iota
	CommandFolder
	CommandNetwork
	CommandFile
)

// CommandHandler interface for command handlers
type CommandHandler interface {
	Info(path string, fullScan bool) (string, error)
	SpeedTest(path, size string, noDelete, shortFormat bool) error
	Fill(path, size string, autoDelete bool) error
	FillClean(path string) error
	Test(path string, autoDelete bool) error
	CheckDuplicates(path string, args []string) error
	Copy(sourcePath, targetPath string) error
	Wipe(path string) error
}

// DeviceHandler implements CommandHandler for devices
type DeviceHandler struct{}

func (h DeviceHandler) Info(path string, fullScan bool) (string, error) {
	info, err := getDeviceInfo(path, fullScan)
	if err != nil {
		return "", err
	}
	return info.String(), nil
}

func (h DeviceHandler) SpeedTest(path, size string, noDelete, shortFormat bool) error {
	return runDeviceSpeedTest(path, size, noDelete, shortFormat)
}

func (h DeviceHandler) Fill(path, size string, autoDelete bool) error {
	return runDeviceFill(path, size, autoDelete)
}

func (h DeviceHandler) FillClean(path string) error {
	return runDeviceFillClean(path)
}

func (h DeviceHandler) Test(path string, autoDelete bool) error {
	return runDeviceTest(path, autoDelete)
}

func (h DeviceHandler) CheckDuplicates(path string, args []string) error {
	return runDeviceCheckDuplicates(path, args)
}

func (h DeviceHandler) Copy(sourcePath, targetPath string) error {
	// For device operations, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h DeviceHandler) Wipe(path string) error {
	return handleWipeCommand([]string{path})
}

// FolderHandler implements CommandHandler for folders
type FolderHandler struct{}

func (h FolderHandler) Info(path string, fullScan bool) (string, error) {
	info, err := getFolderInfo(path, fullScan)
	if err != nil {
		return "", err
	}
	return info.String(), nil
}

func (h FolderHandler) SpeedTest(path, size string, noDelete, shortFormat bool) error {
	return runFolderSpeedTest(path, size, noDelete, shortFormat)
}

func (h FolderHandler) Fill(path, size string, autoDelete bool) error {
	return runFolderFill(path, size, autoDelete)
}

func (h FolderHandler) FillClean(path string) error {
	return runFolderFillClean(path)
}

func (h FolderHandler) Test(path string, autoDelete bool) error {
	return runFolderTest(path, autoDelete)
}

func (h FolderHandler) CheckDuplicates(path string, args []string) error {
	return runFolderCheckDuplicates(path, args)
}

func (h FolderHandler) Copy(sourcePath, targetPath string) error {
	// For folder operations, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h FolderHandler) Wipe(path string) error {
	return handleWipeCommand([]string{path})
}

// NetworkHandler implements CommandHandler for network
type NetworkHandler struct{}

func (h NetworkHandler) Info(path string, fullScan bool) (string, error) {
	info, err := getNetworkInfo(path, fullScan)
	if err != nil {
		return "", err
	}
	return info.String(), nil
}

func (h NetworkHandler) SpeedTest(path, size string, noDelete, shortFormat bool) error {
	return runNetworkSpeedTest(path, size, noDelete, shortFormat, nil)
}

func (h NetworkHandler) Fill(path, size string, autoDelete bool) error {
	return runNetworkFill(path, size, autoDelete, nil)
}

func (h NetworkHandler) FillClean(path string) error {
	return runNetworkFillClean(path, nil)
}

func (h NetworkHandler) Test(path string, autoDelete bool) error {
	return runNetworkTest(path, autoDelete, nil)
}

func (h NetworkHandler) CheckDuplicates(path string, args []string) error {
	return runNetworkCheckDuplicates(path, args, nil)
}

func (h NetworkHandler) Copy(sourcePath, targetPath string) error {
	// For network operations, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h NetworkHandler) Wipe(path string) error {
	return handleWipeCommand([]string{path})
}

// FileHandler implements CommandHandler for files
type FileHandler struct{}

func (h FileHandler) Info(path string, fullScan bool) (string, error) {
	info, err := getFileInfo(path, fullScan)
	if err != nil {
		return "", err
	}
	return info.String(), nil
}

func (h FileHandler) SpeedTest(path, size string, noDelete, shortFormat bool) error {
	return fmt.Errorf("speed test is not supported for files")
}

func (h FileHandler) Fill(path, size string, autoDelete bool) error {
	return fmt.Errorf("fill operation is not supported for files")
}

func (h FileHandler) FillClean(path string) error {
	return fmt.Errorf("fill clean operation is not supported for files")
}

func (h FileHandler) Test(path string, autoDelete bool) error {
	return fmt.Errorf("test operation is not supported for files")
}

func (h FileHandler) CheckDuplicates(path string, args []string) error {
	return fmt.Errorf("check-duplicates operation is not supported for individual files")
}

func (h FileHandler) Copy(sourcePath, targetPath string) error {
	// For individual files, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h FileHandler) Wipe(path string) error {
	// For files, wipe doesn't make sense - you can only delete the file
	return fmt.Errorf("wipe command is not applicable to individual files - use delete instead")
}

// getCommandHandler returns the appropriate command handler
func getCommandHandler(cmdType CommandType) CommandHandler {
	switch cmdType {
	case CommandDevice:
		return DeviceHandler{}
	case CommandFolder:
		return FolderHandler{}
	case CommandNetwork:
		return NetworkHandler{}
	case CommandFile:
		return FileHandler{}
	default:
		return nil
	}
}

// runGenericCommand generic function for executing commands
func runGenericCommand(cmd *flag.FlagSet, cmdType CommandType, args []string, historyLogger *HistoryLogger) {
	cmd.Parse(args)
	if cmd.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
		os.Exit(1)
	}

	path := cmd.Arg(0)

	// First check if the path exists (for folders, files and network paths)
	if cmdType != CommandDevice && !isPathAccessible(path) {
		resourceType := "Path"
		if cmdType == CommandFolder {
			resourceType = "Folder"
		} else if cmdType == CommandFile {
			resourceType = "File"
		} else if cmdType == CommandNetwork {
			resourceType = "Network path"
		}
		fmt.Printf("Info: %s \"%s\" does not exist or is not accessible.\n", resourceType, path)
		return
	}

	// Redirect system drive for write operations (speed, fill, test)
	if cmd.NArg() >= 2 {
		operation := strings.ToLower(cmd.Arg(1))
		if operation == "speed" || operation == "fill" || operation == "f" || operation == "test" {
			path = redirectSystemDrive(path)
		}
	}

	handler := getCommandHandler(cmdType)

	// Set basic command info for history
	cmdTypeName := map[CommandType]string{
		CommandDevice:  "device",
		CommandFolder:  "folder",
		CommandNetwork: "network",
		CommandFile:    "file",
	}[cmdType]

	historyLogger.SetCommand(cmdTypeName, path, "")
	historyLogger.SetParameter("args", args)

	// Check if this is a clean command
	if cmd.NArg() >= 2 {
		cleanParam := strings.ToLower(cmd.Arg(1))
		if cleanParam == "cln" || cleanParam == "clean" || cleanParam == "c" {
			historyLogger.SetCommand(cmdTypeName, path, "clean")
			// Special handling for network clean to pass logger
			if cmdTypeName == "network" {
				err := runNetworkFillClean(path, historyLogger)
				if err != nil {
					if !handleErrorWithUserMessage(err, path, historyLogger) {
						historyLogger.SetError(err)
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						os.Exit(1)
					}
				}
			} else {
				err := handler.FillClean(path)
				if err != nil {
					if !handleErrorWithUserMessage(err, path, historyLogger) {
						historyLogger.SetError(err)
						fmt.Fprintf(os.Stderr, "Error: %v\n", err)
						os.Exit(1)
					}
				}
			}
			historyLogger.SetSuccess()
			return
		}

		// Check if this is a check-duplicates command
		duplicatesParam := strings.ToLower(cmd.Arg(1))
		if duplicatesParam == "check-duplicates" || duplicatesParam == "cd" || duplicatesParam == "duplicate" {
			historyLogger.SetCommand(cmdTypeName, path, "check-duplicates")

			// Collect additional arguments if any
			var dupArgs []string
			if cmd.NArg() > 2 {
				dupArgs = cmd.Args()[2:]
			}

			err := handler.CheckDuplicates(path, dupArgs)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
			historyLogger.SetSuccess()
			return
		}
	}

	// Check if this is a speed test
	if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
		historyLogger.SetCommand(cmdTypeName, path, "speed")
		sizeParam := cmd.Arg(2)
		historyLogger.SetParameter("size", sizeParam)

		// Check for no-delete option and short format
		noDelete := false
		shortFormat := false
		for i := 3; i < cmd.NArg(); i++ {
			arg := strings.ToLower(cmd.Arg(i))
			if arg == "no" || arg == "nodel" || arg == "nodelete" {
				noDelete = true
				historyLogger.SetParameter("noDelete", true)
			} else if arg == "short" || arg == "s" {
				shortFormat = true
				historyLogger.SetParameter("shortFormat", true)
			}
		}

		// Handle "max" as size parameter
		if strings.ToLower(sizeParam) == "max" {
			sizeParam = "10240" // 10GB
			historyLogger.SetParameter("actualSize", "10240MB")
		}

		// Special handling for network speed test to pass logger
		if cmdTypeName == "network" {
			err := runNetworkSpeedTest(path, sizeParam, noDelete, shortFormat, historyLogger)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		} else {
			err := handler.SpeedTest(path, sizeParam, noDelete, shortFormat)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a fill command
	if cmd.NArg() >= 2 && (strings.ToLower(cmd.Arg(1)) == "fill" || strings.ToLower(cmd.Arg(1)) == "f") {
		historyLogger.SetCommand(cmdTypeName, path, "fill")

		sizeParam := "100"
		autoDelete := false

		if cmd.NArg() >= 3 {
			thirdArg := strings.ToLower(cmd.Arg(2))
			if thirdArg == "del" || thirdArg == "delete" || thirdArg == "d" {
				autoDelete = true
			} else {
				sizeParam = cmd.Arg(2)
			}
		}

		if cmd.NArg() >= 4 && !autoDelete {
			fourthArg := strings.ToLower(cmd.Arg(3))
			if fourthArg == "del" || fourthArg == "delete" || fourthArg == "d" {
				autoDelete = true
			}
		}

		historyLogger.SetParameter("size", sizeParam)
		if autoDelete {
			historyLogger.SetParameter("autoDelete", true)
		}

		// Special handling for network fill to pass logger
		if cmdTypeName == "network" {
			err := runNetworkFill(path, sizeParam, autoDelete, historyLogger)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		} else {
			err := handler.Fill(path, sizeParam, autoDelete)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a test command
	if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "test" {
		historyLogger.SetCommand(cmdTypeName, path, "test")

		// Check for "del" option
		autoDelete := false
		if cmd.NArg() >= 3 {
			delParam := strings.ToLower(cmd.Arg(2))
			autoDelete = delParam == "del" || delParam == "delete" || delParam == "d"
			if autoDelete {
				historyLogger.SetParameter("autoDelete", true)
			}
		}

		// Special handling for network test to pass logger
		if cmdTypeName == "network" {
			err := runNetworkTest(path, autoDelete, historyLogger)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		} else {
			err := handler.Test(path, autoDelete)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a copy command
	if cmd.NArg() >= 3 {
		copyParam := strings.ToLower(cmd.Arg(1))
		if copyParam == "copy" || copyParam == "cp" || copyParam == "c" {
			historyLogger.SetCommand(cmdTypeName, path, "copy")
			targetPath := cmd.Arg(2)
			historyLogger.SetParameter("targetPath", targetPath)

			err := handler.Copy(path, targetPath)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
			historyLogger.SetSuccess()
			return
		}
	}

	// Check if this is a wipe command
	if cmd.NArg() >= 2 {
		wipeParam := strings.ToLower(cmd.Arg(1))
		if wipeParam == "wipe" || wipeParam == "w" {
			historyLogger.SetCommand(cmdTypeName, path, "wipe")

			err := handler.Wipe(path)
			if err != nil {
				if !handleErrorWithUserMessage(err, path, historyLogger) {
					historyLogger.SetError(err)
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
			historyLogger.SetSuccess()
			return
		}
	}

	// Regular info command
	historyLogger.SetCommand(cmdTypeName, path, "info")
	fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
	shortFormat := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "short" || strings.ToLower(cmd.Arg(1)) == "s")

	if fullScan {
		historyLogger.SetParameter("fullScan", true)
	}
	if shortFormat {
		historyLogger.SetParameter("shortFormat", true)
	}

	// Special handling for folder short format
	if cmdType == CommandFolder && shortFormat {
		fullScan = true
	}

	result, err := handler.Info(path, fullScan)
	if err != nil {
		if !handleErrorWithUserMessage(err, path, historyLogger) {
			historyLogger.SetError(err)
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Special handling for folder and device short format
	if shortFormat && (cmdType == CommandFolder || cmdType == CommandDevice) {
		switch cmdType {
		case CommandFolder:
			info, _ := getFolderInfo(path, fullScan)
			fmt.Print(info.StringShort())
		case CommandDevice:
			info, _ := getDeviceInfo(path, fullScan)
			fmt.Print(info.StringShort())
		}
	} else {
		fmt.Print(result)
	}

	historyLogger.SetSuccess()
}
