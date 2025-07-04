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

const version = "2507042100"

var usage = fmt.Sprintf(`FileDO %s sza@ukr.net
Processes files on devices/folders/networks.
Usage:
  filedo.exe <command> [arguments]

Commands:
  device <path> [info|i|short|s] Show information about a disk volume. Use 'short' for concise output.
  device <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test device write speed. Use 'max' for 10GB test.
  device <path> fill <size_mb> [del] Fill device with test files of specified size until full.
  device <path> <cln|clean|c> Delete all FILL_*.tmp files from device.
  device <path> test [del|delete|d] Test device for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  folder <path> [info|i|short|s] Show information about a folder and its size. Use 'short' for concise output.
  folder <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test folder write speed. Use 'max' for 10GB test.
  folder <path> fill <size_mb> [del] Fill folder with test files of specified size until full.
  folder <path> <cln|clean|c> Delete all FILL_*.tmp files from folder.
  folder <path> test [del|delete|d] Test folder for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.
  
  file <path> [info|i|short|s] Show information about a file. Use 'short' for concise output.
  
  network <path> [info|i] Show information about a network path.
  network <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] Test network speed. Use 'max' for 10GB test.
  network <path> fill <size_mb> [del] Fill network path with test files of specified size until full.
  network <path> <cln|clean|c> Delete all FILL_*.tmp files from network path.
  network <path> test [del|delete|d] Test network path for fake capacity by writing 100 files (1%% each). Use 'del' to auto-delete files after successful test.

Note: Use no|nodel|nodelete to keep the test file on the destination.
Note: Use short|s with speed tests to show only final upload/download results.
Note: Fill creates files named FILL_#####_ddHHmmss.tmp until available space is used.
Note: Use cln|clean|c with fill to delete all FILL_*.tmp files from the specified location.
Note: Use del with fill to automatically delete all created files after successful completion.

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
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s network <path> [info|i] or %s network <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] or %s network <path> fill <size_mb>\n", os.Args[0], os.Args[0], os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)

		// Check if this is a clean command
		if cmd.NArg() >= 2 {
			cleanParam := strings.ToLower(cmd.Arg(1))
			if cleanParam == "cln" || cleanParam == "clean" || cleanParam == "c" {
				err := runNetworkFillClean(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				return
			}
		}

		// Check if this is a speed test
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
			sizeParam := cmd.Arg(2)
			// Check for no-delete option and short format
			noDelete := false
			shortFormat := false
			for i := 3; i < cmd.NArg(); i++ {
				arg := strings.ToLower(cmd.Arg(i))
				if arg == "no" || arg == "nodel" || arg == "nodelete" {
					noDelete = true
				} else if arg == "short" || arg == "s" {
					shortFormat = true
				}
			}

			// Handle "max" as size parameter
			if strings.ToLower(sizeParam) == "max" {
				sizeParam = "10240" // 10GB
			}

			err := runNetworkSpeedTest(path, sizeParam, noDelete, shortFormat)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if this is a fill command
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "fill" {
			sizeParam := cmd.Arg(2)
			// Check for "del" option
			autoDelete := cmd.NArg() >= 4 && strings.ToLower(cmd.Arg(3)) == "del"
			err := runNetworkFill(path, sizeParam, autoDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if this is a test command
		if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "test" {
			// Check for "del" option
			autoDelete := false
			if cmd.NArg() >= 3 {
				delParam := strings.ToLower(cmd.Arg(2))
				autoDelete = delParam == "del" || delParam == "delete" || delParam == "d"
			}
			err := runNetworkTest(path, autoDelete)
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
			fmt.Fprintf(os.Stderr, "Usage: %s device <path> [info|i|short|s] or %s device <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] or %s device <path> fill <size_mb>\n", os.Args[0], os.Args[0], os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)

		// Check if this is a clean command
		if cmd.NArg() >= 2 {
			cleanParam := strings.ToLower(cmd.Arg(1))
			if cleanParam == "cln" || cleanParam == "clean" || cleanParam == "c" {
				err := runDeviceFillClean(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				return
			}
		}

		// Check if this is a speed test
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
			sizeParam := cmd.Arg(2)
			// Check for no-delete option and short format
			noDelete := false
			shortFormat := false
			for i := 3; i < cmd.NArg(); i++ {
				arg := strings.ToLower(cmd.Arg(i))
				if arg == "no" || arg == "nodel" || arg == "nodelete" {
					noDelete = true
				} else if arg == "short" || arg == "s" {
					shortFormat = true
				}
			}

			// Handle "max" as size parameter
			if strings.ToLower(sizeParam) == "max" || strings.ToLower(sizeParam) == "m" {
				sizeParam = "10240" // 10GB
			}

			err := runDeviceSpeedTest(path, sizeParam, noDelete, shortFormat)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if this is a fill command
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "fill" {
			sizeParam := cmd.Arg(2)
			// Check for "del" option
			autoDelete := cmd.NArg() >= 4 && strings.ToLower(cmd.Arg(3)) == "del"
			err := runDeviceFill(path, sizeParam, autoDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if this is a test command
		if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "test" {
			// Check for "del" option
			autoDelete := false
			if cmd.NArg() >= 3 {
				delParam := strings.ToLower(cmd.Arg(2))
				autoDelete = delParam == "del" || delParam == "delete" || delParam == "d"
			}
			err := runDeviceTest(path, autoDelete)
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
			fmt.Fprintf(os.Stderr, "Usage: %s folder <path> [info|i|short|s] or %s folder <path> speed <size_mb|max> [no|nodel|nodelete] [short|s] or %s folder <path> fill <size_mb>\n", os.Args[0], os.Args[0], os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)

		// Check if this is a clean command
		if cmd.NArg() >= 2 {
			cleanParam := strings.ToLower(cmd.Arg(1))
			if cleanParam == "cln" || cleanParam == "clean" || cleanParam == "c" {
				err := runFolderFillClean(path)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
				return
			}
		}

		// Check if this is a speed test
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "speed" {
			sizeParam := cmd.Arg(2)
			// Check for no-delete option and short format
			noDelete := false
			shortFormat := false
			for i := 3; i < cmd.NArg(); i++ {
				arg := strings.ToLower(cmd.Arg(i))
				if arg == "no" || arg == "nodel" || arg == "nodelete" {
					noDelete = true
				} else if arg == "short" || arg == "s" {
					shortFormat = true
				}
			}

			// Handle "max" as size parameter
			if strings.ToLower(sizeParam) == "max" {
				sizeParam = "10240" // 10GB
			}

			err := runFolderSpeedTest(path, sizeParam, noDelete, shortFormat)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if this is a fill command
		if cmd.NArg() >= 3 && strings.ToLower(cmd.Arg(1)) == "fill" {
			sizeParam := cmd.Arg(2)
			// Check for "del" option
			autoDelete := cmd.NArg() >= 4 && strings.ToLower(cmd.Arg(3)) == "del"
			err := runFolderFill(path, sizeParam, autoDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if this is a test command
		if cmd.NArg() >= 2 && strings.ToLower(cmd.Arg(1)) == "test" {
			// Check for "del" option
			autoDelete := false
			if cmd.NArg() >= 3 {
				delParam := strings.ToLower(cmd.Arg(2))
				autoDelete = delParam == "del" || delParam == "delete" || delParam == "d"
			}
			err := runFolderTest(path, autoDelete)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Regular folder info
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
		shortFormat := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "short" || strings.ToLower(cmd.Arg(1)) == "s")

		// For short format, always perform full scan to get complete counts
		if shortFormat {
			fullScan = true
		}

		info, err := getFolderInfo(path, fullScan)
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

	runFileCommand := func(cmd *flag.FlagSet) {
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s file <path> [info|i|short|s]\n", os.Args[0])
			os.Exit(1)
		}

		path := cmd.Arg(0)
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
		shortFormat := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "short" || strings.ToLower(cmd.Arg(1)) == "s")

		info, err := getFileInfo(path, fullScan)
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
