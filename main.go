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
  device <path> [full] Show information about a disk volume.
  folder <path> [full] Show information about a folder and its size.
  file <path>          Show information about a file.

Flags:
  ?    Show this help message.`, version)

func main() {
	args := os.Args

	if len(args) < 2 || args[1] == "/?" || args[1] == "?" || strings.ToLower(args[1]) == "help" {
		fmt.Println(usage)
		return
	}

	var command string
	var add_args []string

	firstArg := os.Args[1]

	// For drive C can be used as "C:" or "C:\"
	if len(firstArg) > 1  && len(firstArg) < 4 && string([]rune(firstArg)[1]) == ":" {
		command = "device"
		add_args = args[1:]
	} else {
		command = strings.ToLower(os.Args[1])
		add_args = args[2:]
	}

	runCommand := func(cmd *flag.FlagSet, getInfoFunc func(string, bool) (fmt.Stringer, error)) {
		cmd.Parse(add_args)
		if cmd.NArg() < 1 {
			fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
			fmt.Fprintf(os.Stderr, "Usage: %s %s <path> [full]\n", os.Args[0], cmd.Name())
			os.Exit(1)
		}
		path := cmd.Arg(0)
		fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "full" || strings.ToLower(cmd.Arg(1)) == "f")

		info, err := getInfoFunc(path, fullScan)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(info)
	}

	switch command {
	case "device", "dev", "disk", "d":
		deviceCmd := flag.NewFlagSet("device", flag.ExitOnError)
		runCommand(deviceCmd, func(p string, f bool) (fmt.Stringer, error) { return getDeviceInfo(p, f) })
	case "folder", "fold", "dir", "fld", "f":
		folderCmd := flag.NewFlagSet("folder", flag.ExitOnError)
		runCommand(folderCmd, func(p string, f bool) (fmt.Stringer, error) { return getFolderInfo(p, f) })
	//case "file" or the direct filename

	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}

	bue_message := "\n" + time.Now().Format("2006-01-02 15:04:05") + " sza@ukr.net " + version
	fmt.Print(bue_message)
}
