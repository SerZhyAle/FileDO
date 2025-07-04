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
  device <path>  Show information about a disk volume.
  folder <path>  Show information about a folder and its size.
  file <path>    Show information about a file.

Flags:
  /?    Show this help message.`, version)

func main() {
	
	if len(os.Args) < 2 || os.Args[1] == "/?" || os.Args[1] == "?" || strings.ToLower(os.Args[1]) == "help" {
		fmt.Println(usage)
		return
	}

	deviceCmd := flag.NewFlagSet("device", flag.ExitOnError)
	folderCmd := flag.NewFlagSet("folder", flag.ExitOnError)
	//fileCmd := flag.NewFlagSet("file", flag.ExitOnError)

	command := strings.ToLower(os.Args[1])
	switch command {
	case "device", "dev", "disk", "d":
		deviceCmd.Parse(os.Args[2:])
		if deviceCmd.NArg() == 0 {
			fmt.Fprintln(os.Stderr, "Error: 'device' command requires a path argument.")
			fmt.Fprintf(os.Stderr, "Usage: %s device <path>\n", os.Args[0])
			os.Exit(1)
		}
		path := deviceCmd.Arg(0)

		info, err := getDeviceInfo(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(info)
	case "folder", "fold", "dir", "fld":
		folderCmd.Parse(os.Args[2:])
		if folderCmd.NArg() == 0 {
			fmt.Fprintln(os.Stderr, "Error: 'folder' command requires a path argument.")
			fmt.Fprintf(os.Stderr, "Usage: %s folder <path>\n", os.Args[0])
			os.Exit(1)
		}
		path := folderCmd.Arg(0)
		info, err := getFolderInfo(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Print(info)
	case "file", "f":
		
	default:
		fmt.Fprintf(os.Stderr, "Error: Unknown command '%s'\n\n", os.Args[1])
		fmt.Println(usage)
		os.Exit(1)
	}

	bue_message := "\n" + time.Now().Format("2006-01-02 15:04:05") + " sza@ukr.net "+version
	fmt.Print(bue_message)
}
