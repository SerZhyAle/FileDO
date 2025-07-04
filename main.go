// sza250407
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const version = "2507041500"

var usage = fmt.Sprintf(`FileDO %s sza@ukr.net

Processes files.

Usage:
  filedo.exe <command> [arguments]

Commands:
  device <path> [info|i|short|s] Show information about a disk volume. Use 'short' for concise output.
  device <path> speed <size_mb|max> [no|nodel|nodelete] Test device write speed. Use 'max' for 10GB test.
  folder <path> [info|i] Show information about a folder and its size.
  folder <path> speed <size_mb|max> [no|nodel|nodelete] Test folder write speed. Use 'max' for 10GB test.
  file <path>            Show information about a file.
  network <path> [info|i] Show information about a network path.
  network <path> speed <size_mb|max> [no|nodel|nodelete] Test network speed. Use 'max' for 10GB test.

Note: Use no|nodel|nodelete to keep the test file on the destination.

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

	runCommand := func(cmd *flag.FlagSet, getInfoFunc func(string, bool) (fmt.Stringer, error)) {
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s %s <path> [info|i]\n", os.Args[0], cmd.Name())
			os.Exit(1)
		}
		path := cmd.Arg(0)
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")

		info, err := getInfoFunc(path, fullScan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(info)
	}

	runNetworkCommand := func(cmd *flag.FlagSet) {
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s network <path> [info|i] or %s network <path> speed <size_mb|max> [no|nodel|nodelete]\n", os.Args[0], os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)

		// Check if this is a speed test
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
			sizeParam := cmd.Arg(2)
			// Check for no-delete option
			noDelete := false
			if cmd.NArg() >= 4 {
				deleteFlag := strings.ToLower(cmd.Arg(3))
				noDelete = deleteFlag == "no" || deleteFlag == "nodel" || deleteFlag == "nodelete"
			}

			// Handle "max" as size parameter
			if strings.ToLower(sizeParam) == "max" {
				sizeParam = "10240" // 10GB
			}

			err := runNetworkSpeedTest(path, sizeParam, noDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Regular network info
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
		info, err := getNetworkInfo(path, fullScan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(info)
	}

	runDeviceCommand := func(cmd *flag.FlagSet) {
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s device <path> [info|i|short|s] or %s device <path> speed <size_mb|max> [no|nodel|nodelete]\n", os.Args[0], os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)

		// Check if this is a speed test
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
			sizeParam := cmd.Arg(2)
			// Check for no-delete option
			noDelete := false
			if cmd.NArg() >= 4 {
				deleteFlag := strings.ToLower(cmd.Arg(3))
				noDelete = deleteFlag == "no" || deleteFlag == "nodel" || deleteFlag == "nodelete"
			}

			// Handle "max" as size parameter
			if strings.ToLower(sizeParam) == "max" || strings.ToLower(sizeParam) == "m" {
				sizeParam = "10240" // 10GB
			}

			err := runDeviceSpeedTest(path, sizeParam, noDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Regular device info
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
		shortFormat := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "short" || strings.ToLower(cmd.Arg(1)) == "s")

		info, err := getDeviceInfo(path, fullScan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if shortFormat {
			fmt.Print(info.StringShort())
		} else {
			fmt.Print(info)
		}
	}

	runFolderCommand := func(cmd *flag.FlagSet) {
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s folder <path> [info|i] or %s folder <path> speed <size_mb|max> [no|nodel|nodelete]\n", os.Args[0], os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)

		// Check if this is a speed test
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
			sizeParam := cmd.Arg(2)
			// Check for no-delete option
			noDelete := false
			if cmd.NArg() >= 4 {
				deleteFlag := strings.ToLower(cmd.Arg(3))
				noDelete = deleteFlag == "no" || deleteFlag == "nodel" || deleteFlag == "nodelete"
			}

			// Handle "max" as size parameter
			if strings.ToLower(sizeParam) == "max" {
				sizeParam = "10240" // 10GB
			}

			err := runFolderSpeedTest(path, sizeParam, noDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Regular folder info
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
		info, err := getFolderInfo(path, fullScan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(info)
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
		runCommand(fileCmd, func(p string, f bool) (fmt.Stringer, error) { return getFileInfo(p, f) })
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
