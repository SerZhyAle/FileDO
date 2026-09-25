package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// isPathAccessible checks if a path exists and is accessible
func isPathAccessible(path string) bool {
	// Check for network paths specially
	if len(path) > 2 && (path[0:2] == `\\` || path[0:2] == "//") {
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
	return true
}

// reportOpError is the one place an operation's error becomes all three of the
// things it has always meant: the sentence the user reads, the history entry,
// and the verdict this run ends on (CLI-EVENT-STREAM rules 10 and 11).
//
// The third was missing. Every branch below used to read "if the message was
// handled, carry on", and carrying on meant falling through to SetSuccess and
// exiting 0 - so a check that failed and a check that passed were the same
// thing to the shell, to release.ps1 and to any script. That is the finding
// the contract alignment recorded as T2, and this function is its fix: an
// error ends the branch, and outcome.go turns it into a code.
func reportOpError(err error, path string, historyLogger *HistoryLogger) {
	if err == nil {
		return
	}
	handleErrorWithUserMessage(err, path, historyLogger)
	historyLogger.SetError(err)
	runFailure(err)
}

// genericOperation resolves the positional operation word - the same compare
// the branches of runGenericCommand make - into the name this run reports
// under and whether the verb judges its target or merely acts on it. The
// difference is a `Passed` against a `Done` and nothing else (rule 10).
//
// It mirrors the branch order below, including the two words that are an
// alias of two different operations: `c` is clean, never copy, and `copy`
// with nothing to copy to is not a copy at all.
func genericOperation(cmd *flag.FlagSet) (string, runKind) {
	if cmd.NArg() < 2 {
		return "info", runActs
	}
	switch strings.ToLower(cmd.Arg(1)) {
	case "cln", "clean", "c":
		return "clean", runActs
	case "check-duplicates", "cd", "duplicate":
		return "check-duplicates", runJudges
	case "speed":
		return "speed", runActs
	case "fill", "f":
		if cmd.NArg() >= 3 {
			third := strings.ToLower(cmd.Arg(2))
			if third == "verify" || third == "v" {
				return "fill-verify", runJudges
			}
		}
		return "fill", runActs
	case "test":
		return "test", runJudges
	case "probe":
		return "probe", runJudges
	case "recover", "repair":
		return "recover", runActs
	case "copy", "cp":
		if cmd.NArg() >= 3 {
			return "copy", runActs
		}
		return "info", runActs
	case "wipe", "w":
		return "wipe", runActs
	default:
		return "info", runActs
	}
}

// printRedirectCleanNote says where a system-drive clean is looking.
func printRedirectCleanNote(dir string) {
	fmt.Printf("System drive: FileDO writes its test files to %s, so clean looks there.\n", dir)
}

// redirectSystemDrive redirects the system drive to the user's temp directory
// for write operations, with confirmation. "The system drive" is decided by
// isSystemVolumeRoot, which recognises every spelling of the root of the
// volume holding Windows (CLI-18). With no answer to read - the GUI closes
// the child's stdin - the prompt takes its default, which is the redirect.
func redirectSystemDrive(path string) string {
	if isSystemVolumeRoot(path) {
		// Check if redirection is disabled by environment variable
		if os.Getenv("FILEDO_DISABLE_REDIRECT") == "1" {
			fmt.Printf("⚠️  System drive redirection disabled by FILEDO_DISABLE_REDIRECT=1\n")
			fmt.Printf("   WARNING: Writing directly to the system drive - use with caution!\n")
			return path
		}

		// Create subdirectory for FileDO operations
		fileDoTempDir := systemDriveRedirectDir()

		// Show warning and ask for confirmation
		fmt.Printf("⚠️  WARNING: Write operation requested on the system drive (%s)\n", path)
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
			fmt.Printf("⚠️  User chose to proceed with the system drive directly.\n")
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
	FillClean(path string, assumeYes bool) error
	FillVerify(path string) error
	Test(path string, autoDelete bool, maxFiles int) error
	Probe(path string, assumeYes bool, autoRepair bool) error
	CheckDuplicates(path string, args []string) error
	Copy(sourcePath, targetPath string) error
	Wipe(path string, args []string) error
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

func (h DeviceHandler) FillClean(path string, assumeYes bool) error {
	return runDeviceFillClean(path, assumeYes)
}

func (h DeviceHandler) FillVerify(path string) error {
	return runDeviceFillVerify(path)
}

func (h DeviceHandler) Test(path string, autoDelete bool, maxFiles int) error {
	return runDeviceTest(path, autoDelete, maxFiles)
}

func (h DeviceHandler) Probe(path string, assumeYes bool, autoRepair bool) error {
	return runDeviceProbeCheck(path, assumeYes, autoRepair)
}

func (h DeviceHandler) CheckDuplicates(path string, args []string) error {
	return runDeviceCheckDuplicates(path, args)
}

func (h DeviceHandler) Copy(sourcePath, targetPath string) error {
	// For device operations, delegate to handleCopyCommand. The device verb
	// means the whole drive: `D:` alone is D:'s current directory to Windows.
	return handleCopyCommand([]string{"copy", deviceRootPath(sourcePath), targetPath})
}

func (h DeviceHandler) Wipe(path string, args []string) error {
	// `device D: wipe` means D:\ - a root, and therefore always the strong
	// confirmation - never D:'s per-process current directory (WIPE-05).
	return handleWipeCommand(append([]string{deviceRootPath(path)}, args...))
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

func (h FolderHandler) FillClean(path string, assumeYes bool) error {
	return runFolderFillClean(path, assumeYes)
}

func (h FolderHandler) FillVerify(path string) error {
	return runCapacityFillVerify("Folder", path)
}

func (h FolderHandler) Test(path string, autoDelete bool, maxFiles int) error {
	return runFolderTest(path, autoDelete, maxFiles)
}

func (h FolderHandler) Probe(path string, assumeYes bool, autoRepair bool) error {
	return fmt.Errorf("probe requires a drive letter, not a folder path")
}

func (h FolderHandler) CheckDuplicates(path string, args []string) error {
	return runFolderCheckDuplicates(path, args)
}

func (h FolderHandler) Copy(sourcePath, targetPath string) error {
	// For folder operations, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h FolderHandler) Wipe(path string, args []string) error {
	return handleWipeCommand(append([]string{path}, args...))
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

func (h NetworkHandler) FillClean(path string, assumeYes bool) error {
	return runNetworkFillClean(path, assumeYes, nil)
}

func (h NetworkHandler) FillVerify(path string) error {
	return runCapacityFillVerify("Network", path)
}

func (h NetworkHandler) Test(path string, autoDelete bool, maxFiles int) error {
	return runNetworkTest(path, autoDelete, maxFiles, nil)
}

func (h NetworkHandler) Probe(path string, assumeYes bool, autoRepair bool) error {
	return fmt.Errorf("probe is only supported for local drive letters (e.g. D:)")
}

func (h NetworkHandler) CheckDuplicates(path string, args []string) error {
	return runNetworkCheckDuplicates(path, args, nil)
}

func (h NetworkHandler) Copy(sourcePath, targetPath string) error {
	// For network operations, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h NetworkHandler) Wipe(path string, args []string) error {
	return handleWipeCommand(append([]string{path}, args...))
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

func (h FileHandler) FillClean(path string, assumeYes bool) error {
	return fmt.Errorf("fill clean operation is not supported for files")
}

func (h FileHandler) FillVerify(path string) error {
	return fmt.Errorf("fill verify operation is not supported for files")
}

func (h FileHandler) Test(path string, autoDelete bool, maxFiles int) error {
	return fmt.Errorf("test operation is not supported for files")
}

func (h FileHandler) Probe(path string, assumeYes bool, autoRepair bool) error {
	return fmt.Errorf("probe is only supported for local drive letters (e.g. D:)")
}

func (h FileHandler) CheckDuplicates(path string, args []string) error {
	return fmt.Errorf("check-duplicates operation is not supported for individual files")
}

func (h FileHandler) Copy(sourcePath, targetPath string) error {
	// For individual files, delegate to handleCopyCommand
	return handleCopyCommand([]string{"copy", sourcePath, targetPath})
}

func (h FileHandler) Wipe(path string, args []string) error {
	// One file, overwritten in place and removed - the stronger meaning of
	// the word, and the one the Explorer group's "Wipe this file" invokes.
	// wipe_handler.go carries the confirmation and the caveat.
	return handleFileWipeCommand(path, args)
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
	// The set defines no flags; "--" makes every word positional, so a target
	// that starts with a dash (a folder named -old) is a target and never a
	// parse error that exits past the result and the history (CLI-23).
	cmd.Parse(append([]string{"--"}, args...))
	if cmd.NArg() < 1 {
		beginRun(runActs, cmd.Name(), "", args)
		runFailure(fmt.Errorf("'%s' command requires a path argument", cmd.Name()))
		fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
		return
	}

	path := cmd.Arg(0)

	// The run opens before the first thing that can end it, so that even a
	// target that does not exist produces a stream with a `result` on the end
	// of it (rule 10: a run that wrote no result has not said anything).
	operation, kind := genericOperation(cmd)
	beginRun(kind, operation, path, cmd.Args())

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
		// Nothing was measured, so nothing is claimed: an unreachable target
		// is *Not proven* and exit 2, never the zero it used to be.
		runFailure(fmt.Errorf("%s %q does not exist or is not accessible", strings.ToLower(resourceType), path))
		return
	}

	// The system drive: writes (speed, fill, test) are redirected to the
	// temp folder, and clean looks where they went (effectiveTarget).
	if cmd.NArg() >= 2 {
		path = effectiveTarget(cmd.Arg(1), path)
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
	// Defence in depth: the fdsec family never reaches this function, but a
	// parameter dump of raw args is exactly how a credential ends up on disk.
	historyLogger.SetParameter("args", redactCredentialArgs(args))

	// Check if this is a clean command
	if cmd.NArg() >= 2 {
		cleanParam := strings.ToLower(cmd.Arg(1))
		if cleanParam == "cln" || cleanParam == "clean" || cleanParam == "c" {
			historyLogger.SetCommand(cmdTypeName, path, "clean")
			runStep("clean", path)
			// clean lists what it will remove and asks; --yes skips the
			// question, never the name-and-content check (SP-0026 CAP-14).
			assumeYes := false
			for i := 2; i < cmd.NArg(); i++ {
				switch strings.ToLower(cmd.Arg(i)) {
				case "--yes", "-y", "yes", "y", "--force", "/y":
					assumeYes = true
				default:
					reportOpError(fmt.Errorf("unknown option %q for clean - the only option is --yes", cmd.Arg(i)), path, historyLogger)
					return
				}
			}
			if assumeYes {
				historyLogger.SetParameter("assumeYes", true)
			}
			// Special handling for network clean to pass logger
			var err error
			if cmdTypeName == "network" {
				err = runNetworkFillClean(path, assumeYes, historyLogger)
			} else {
				err = handler.FillClean(path, assumeYes)
			}
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
			historyLogger.SetSuccess()
			return
		}

		// Check if this is a check-duplicates command
		duplicatesParam := strings.ToLower(cmd.Arg(1))
		if duplicatesParam == "check-duplicates" || duplicatesParam == "cd" || duplicatesParam == "duplicate" {
			historyLogger.SetCommand(cmdTypeName, path, "check-duplicates")
			runStep("check-duplicates", path)

			// Collect additional arguments if any
			var dupArgs []string
			if cmd.NArg() > 2 {
				dupArgs = cmd.Args()[2:]
			}

			err := handler.CheckDuplicates(path, dupArgs)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
			historyLogger.SetSuccess()
			return
		}
	}

	// Check if this is a speed test
	if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "speed" {
		historyLogger.SetCommand(cmdTypeName, path, "speed")
		runStep("speed", path)
		// speed [size|max] [nodel] [short], in any order. A word that is none
		// of these is a usage error, never a silent 1 MB test (SP-0024 CLI-25).
		sizeParam := "100" // Default size
		sizeSet := false
		noDelete := false
		shortFormat := false
		for i := 2; i < cmd.NArg(); i++ {
			arg := strings.ToLower(cmd.Arg(i))
			switch {
			case arg == "no" || arg == "nodel" || arg == "nodelete":
				noDelete = true
				historyLogger.SetParameter("noDelete", true)
			case arg == "short" || arg == "s":
				shortFormat = true
				historyLogger.SetParameter("shortFormat", true)
			case !sizeSet && arg == "max":
				sizeParam, sizeSet = "10240", true // 10GB
				historyLogger.SetParameter("actualSize", "10240MB")
			case !sizeSet:
				sizeParam, sizeSet = cmd.Arg(i), true
			default:
				reportOpError(fmt.Errorf("unexpected argument %q - usage: speed [size|max] [nodel] [short]", cmd.Arg(i)), path, historyLogger)
				return
			}
		}
		historyLogger.SetParameter("size", sizeParam)
		if _, err := parseSizeMB(sizeParam, 1, 10240); err != nil {
			reportOpError(err, path, historyLogger)
			return
		}

		// Special handling for network speed test to pass logger
		if cmdTypeName == "network" {
			err := runNetworkSpeedTest(path, sizeParam, noDelete, shortFormat, historyLogger)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
		} else {
			err := handler.SpeedTest(path, sizeParam, noDelete, shortFormat)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a fill verify command: filedo D: fill verify
	if cmd.NArg() >= 3 &&
		(strings.ToLower(cmd.Arg(1)) == "fill" || strings.ToLower(cmd.Arg(1)) == "f") &&
		(strings.ToLower(cmd.Arg(2)) == "verify" || strings.ToLower(cmd.Arg(2)) == "v") {
		historyLogger.SetCommand(cmdTypeName, path, "fill-verify")
		runStep("fill-verify", path)
		if err := handler.FillVerify(path); err != nil {
			reportOpError(err, path, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a fill command
	if cmd.NArg() >= 2 && (strings.ToLower(cmd.Arg(1)) == "fill" || strings.ToLower(cmd.Arg(1)) == "f") {
		historyLogger.SetCommand(cmdTypeName, path, "fill")
		runStep("fill", path)

		// fill [size] [del]. An unreadable or out-of-range size is a usage
		// error and writes nothing: `fill clean`, the cleanup hint FileDO
		// itself used to print, filled the whole drive with 100 MB files
		// (SP-0026 CAP-05, SP-0024 CLI-03/CLI-25).
		sizeParam := "100"
		sizeSet := false
		autoDelete := false
		for i := 2; i < cmd.NArg(); i++ {
			arg := strings.ToLower(cmd.Arg(i))
			switch {
			case arg == "del" || arg == "delete" || arg == "d":
				autoDelete = true
			case !sizeSet:
				sizeParam, sizeSet = cmd.Arg(i), true
			default:
				reportOpError(fmt.Errorf("unexpected argument %q - usage: fill [size] [del], or fill verify", cmd.Arg(i)), path, historyLogger)
				return
			}
		}

		historyLogger.SetParameter("size", sizeParam)
		if autoDelete {
			historyLogger.SetParameter("autoDelete", true)
		}
		if _, err := parseSizeMB(sizeParam, 1, 10240); err != nil {
			if w := strings.ToLower(sizeParam); w == "clean" || w == "cln" || w == "c" {
				err = fmt.Errorf("%v - to remove FileDO's test files, run: %s", err, cleanupHint(path))
			}
			reportOpError(err, path, historyLogger)
			return
		}

		// Special handling for network fill to pass logger
		if cmdTypeName == "network" {
			err := runNetworkFill(path, sizeParam, autoDelete, historyLogger)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
		} else {
			err := handler.Fill(path, sizeParam, autoDelete)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a test command
	if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "test" {
		historyLogger.SetCommand(cmdTypeName, path, "test")
		runStep("test", path)

		// Parse additional arguments: optional N (number of files) and optional "del"
		// Supported forms: test | test N | test del | test N del
		// A word that is neither a count nor del is a usage error, never
		// silently ignored (SP-0024 CLI-25).
		const defaultMaxFiles = 100
		const maxTestFiles = 1000000
		maxFiles := defaultMaxFiles
		countSet := false
		autoDelete := false
		for i := 2; i < cmd.NArg(); i++ {
			arg := strings.ToLower(strings.TrimSpace(cmd.Arg(i)))
			if arg == "del" || arg == "delete" || arg == "d" {
				autoDelete = true
				continue
			}
			n, err := strconv.Atoi(arg)
			if countSet || err != nil || n < 1 || n > maxTestFiles {
				reportOpError(fmt.Errorf("unexpected argument %q - usage: test [number of files, 1..%d] [del]", cmd.Arg(i), maxTestFiles), path, historyLogger)
				return
			}
			maxFiles, countSet = n, true
		}
		if autoDelete {
			historyLogger.SetParameter("autoDelete", true)
		}
		if maxFiles != defaultMaxFiles {
			historyLogger.SetParameter("maxFiles", maxFiles)
		}

		// Special handling for network test to pass logger
		if cmdTypeName == "network" {
			err := runNetworkTest(path, autoDelete, maxFiles, historyLogger)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
		} else {
			err := handler.Test(path, autoDelete, maxFiles)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a probe command
	if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "probe" {
		historyLogger.SetCommand(cmdTypeName, path, "probe")
		runStep("probe", path)

		// Optional flags:
		//  yes|y|allright|force -> skip interactive confirmations
		//  fix|repair|format     -> offer/perform quick format if drive becomes unreadable after probe
		assumeYes := false
		autoRepair := false
		for i := 2; i < cmd.NArg(); i++ {
			arg := strings.ToLower(strings.TrimSpace(cmd.Arg(i)))
			switch arg {
			case "yes", "y", "allright", "force":
				assumeYes = true
			case "fix", "repair", "format":
				autoRepair = true
			}
		}
		if assumeYes {
			historyLogger.SetParameter("assumeYes", true)
		}
		if autoRepair {
			historyLogger.SetParameter("autoRepair", true)
		}

		err := handler.Probe(path, assumeYes, autoRepair)
		if err != nil {
			reportOpError(err, path, historyLogger)
			return
		}
		historyLogger.SetSuccess()
		return
	}

	// Check if this is a recover command (device only)
	if cmd.NArg() >= 2 {
		recoverParam := strings.ToLower(cmd.Arg(1))
		if recoverParam == "recover" || recoverParam == "repair" {
			historyLogger.SetCommand(cmdTypeName, path, "recover")
			runStep("recover", path)

			if cmdTypeName != "device" {
				reportOpError(fmt.Errorf("recover command is only supported for local drive letters (e.g. D:)"), path, historyLogger)
				return
			}

			// Optional flags:
			//  yes|y|allright|force -> skip interactive confirmations
			//  format|fmt|forceformat -> force quick format path
			assumeYes := false
			forceFormat := false
			for i := 2; i < cmd.NArg(); i++ {
				arg := strings.ToLower(strings.TrimSpace(cmd.Arg(i)))
				switch arg {
				case "yes", "y", "allright", "force":
					assumeYes = true
				case "format", "fmt", "forceformat":
					forceFormat = true
				}
			}
			if assumeYes {
				historyLogger.SetParameter("assumeYes", true)
			}
			if forceFormat {
				historyLogger.SetParameter("forceFormat", true)
			}

			err := runDeviceRecoverCheck(path, assumeYes, forceFormat)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
			historyLogger.SetSuccess()
			return
		}
	}

	// Check if this is a copy command
	if cmd.NArg() >= 3 {
		copyParam := strings.ToLower(cmd.Arg(1))
		if copyParam == "copy" || copyParam == "cp" || copyParam == "c" {
			historyLogger.SetCommand(cmdTypeName, path, "copy")
			runStep("copy", path)
			targetPath := cmd.Arg(2)
			historyLogger.SetParameter("targetPath", targetPath)
			copyPrecount = wantsCopyPrecount(cmd.Args()[3:])

			err := handler.Copy(path, targetPath)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
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
			runStep("wipe", path)

			// Collect trailing arguments (e.g. --yes / --force) if any
			var wipeArgs []string
			if cmd.NArg() > 2 {
				wipeArgs = cmd.Args()[2:]
			}

			err := handler.Wipe(path, wipeArgs)
			if err != nil {
				reportOpError(err, path, historyLogger)
				return
			}
			historyLogger.SetSuccess()
			return
		}
	}

	// Regular info command
	historyLogger.SetCommand(cmdTypeName, path, "info")
	runStep("info", path)
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
		reportOpError(err, path, historyLogger)
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
