// sza250407
// sza2504072115
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const version = "2507050100"

var usage = fmt.Sprintf(`FileDO %s sza@ukr.net
Processes files on devices/folders/networks.
Usage:
  filedo.exe <command> [arguments]

Commands:
  device <path> [info|i|short|s] Show information about a disk volume. Use 'short' for concise output.
  device <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test device write speed. Use 'max' for 10GB test.
  device <path> fill <size_mb> [del] Fill device with test files of specified size until full. Use with 'del' for secure wiping of free space to prevent data recovery.
  device <path> <cln|clean|c> Delete all test files (FILL_*.tmp and speedtest_*.txt) from device.
  device <path> test [del|delete|d] Test device for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  folder <path> [info|i|short|s] Show information about a folder and its size. Use 'short' for concise output.
  folder <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test folder write speed. Use 'max' for 10GB test.
  folder <path> fill <size_mb> [del] Fill folder with test files of specified size until full. Use with 'del' for secure wiping of free space to prevent data recovery.
  folder <path> <cln|clean|c> Delete all test files (FILL_*.tmp and speedtest_*.txt) from folder.
  folder <path> test [del|delete|d] Test folder for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  file <path> [info|i|short|s] Show information about a file. Use 'short' for concise output.
  
  network <path> [info|i] Show information about a network path.
  network <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test network speed. Use 'max' for 10GB test.
  network <path> fill <size_mb> [del] Fill network path with test files of specified size until full. Use with 'del' for secure wiping of free space to prevent data recovery.
  network <path> <cln|clean|c> Delete all test files (FILL_*.tmp and speedtest_*.txt) from network path.
  network <path> test [del|delete|d] Test network path for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.

Note: Use no|nodel|nodelete to keep the test file on the destination.
Note: Use short|s with speed tests to show only final upload/download results.
Note: Fill creates files named FILL_#####_ddHHmmss.tmp until available space is used.
Note: Use cln|clean|c to delete all test files (FILL_*.tmp and speedtest_*.txt) from the specified location.
Note: Use del with fill to automatically delete all created files after successful completion.
Security: Use 'fill <size> del' to securely overwrite free space and prevent recovery of deleted files.
Example: filedo.exe device C: fill 1000 del

Flags:
  ?    Show this help message.`, version)

var list_of_flags_for_device = []string{"device", "dev", "disk", "d"}
var list_of_flags_for_folder = []string{"folder", "fold", "dir", "fld"}
var list_of_flags_for_file = []string{"file", "fl", "f"}
var list_of_flags_for_network = []string{"network", "net", "n"}
var list_fo_flags_for_help = []string{"?", "help", "h", "?"}
var list_of_flags_for_all = append(append(append(list_of_flags_for_device, list_of_flags_for_folder...), list_of_flags_for_file...), list_of_flags_for_network...)

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func main() {
	args := os.Args
	for i := range args {
		args[i] = strings.ToLower(args[i])
	}

	if len(args) < 2 || contains(list_fo_flags_for_help, args[1]) {
		fmt.Println(usage)
		return
	}

	var command string
	var add_args []string

	if contains(list_of_flags_for_all, args[1]) {
		command = args[1]
		add_args = args[2:]
	} else {
		firstArg := args[1]

		// For drive C can be used as "C:" or "C:\"
		if (len(firstArg) == 1) || (len(firstArg) > 1 && len(firstArg) < 4 && string([]rune(firstArg)[1]) == ":") {
			if len(firstArg) == 1 {
				args[1] += ":"
			}

			command = "device"
			add_args = args[1:]
		} else {
			if len(firstArg) > 2 && (firstArg[0:2] == "\\" || firstArg[0:2] == "//") {
				command = "network"
				add_args = args[1:]
			} else {
				// Check if args[1] is an existing file or folder
				if info, err := os.Stat(args[1]); err == nil {
					if info.IsDir() {
						command = "folder"
						add_args = args[1:]
					} else {
						command = "file"
						add_args = args[1:]
					}
				} else {
					command = os.Args[1]
					add_args = args[2:]
				}
			}
		}
	}

	runNetworkCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandNetwork, add_args)
	}

	runDeviceCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandDevice, add_args)
	}

	runFolderCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandFolder, add_args)
	}

	runFileCommand := func(cmd *flag.FlagSet) {
		runGenericCommand(cmd, CommandFile, add_args)
	}

	switch {
	case contains(list_of_flags_for_device, command):
		deviceCmd := flag.NewFlagSet("device", flag.ExitOnError)
		runDeviceCommand(deviceCmd)
	case contains(list_of_flags_for_folder, command):
		folderCmd := flag.NewFlagSet("folder", flag.ExitOnError)
		runFolderCommand(folderCmd)
	case contains(list_of_flags_for_file, command):
		fileCmd := flag.NewFlagSet("file", flag.ExitOnError)
		runFileCommand(fileCmd)
	case contains(list_of_flags_for_network, command):
		networkCmd := flag.NewFlagSet("network", flag.ExitOnError)
		runNetworkCommand(networkCmd)

	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}

	bue_message := "\n" + time.Now().Format("2006-01-02 15:04:05") + " sza@ukr.net " + version
	fmt.Print(bue_message)
}
