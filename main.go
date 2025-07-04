package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const version = "2507041230"

var usage = fmt.Sprintf(`FileDO %s sza@ukr.net

Processes files.

Usage:
  filedo.exe <command> [arguments]

Commands:
  device <path> [full] Show information about a disk volume.
  folder <path> [full] Show information about a folder and its size.
  file <path>          Show information about a file.

Flags:
  /?    Show this help message.`, version)

func main() {
	if len(os.Args) < 2 || os.Args[1] == "/?" || os.Args[1] == "?" || strings.ToLower(os.Args[1]) == "help" {
		fmt.Println(usage)
		return
	}

	command := strings.ToLower(os.Args[1])
	args := os.Args[2:]

	runCommand := func(cmd *flag.FlagSet, getInfoFunc func(string, bool) (fmt.Stringer, error)) {
		cmd.Parse(args)
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
	case "folder", "fold", "dir", "fld":
		folderCmd := flag.NewFlagSet("folder", flag.ExitOnError)
		runCommand(folderCmd, func(p string, f bool) (fmt.Stringer, error) { return getFolderInfo(p, f) })
	case "file", "f":

	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}

	bue_message := "\n" + time.Now().Format("2006-01-02 15:04:05") + " sza@ukr.net " + version
	fmt.Print(bue_message)
}
