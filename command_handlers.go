package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

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
	return runNetworkSpeedTest(path, size, noDelete, shortFormat)
}

func (h NetworkHandler) Fill(path, size string, autoDelete bool) error {
	return runNetworkFill(path, size, autoDelete)
}

func (h NetworkHandler) FillClean(path string) error {
	return runNetworkFillClean(path)
}

func (h NetworkHandler) Test(path string, autoDelete bool) error {
	return runNetworkTest(path, autoDelete)
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
func runGenericCommand(cmd *flag.FlagSet, cmdType CommandType, args []string) {
	cmd.Parse(args)
	if cmd.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Error: '%s' command requires a path argument.\n", cmd.Name())
		os.Exit(1)
	}

	path := cmd.Arg(0)
	handler := getCommandHandler(cmdType)

	// Check if this is a clean command
	if cmd.NArg() >= 2 {
		cleanParam := strings.ToLower(cmd.Arg(1))
		if cleanParam == "cln" || cleanParam == "clean" || cleanParam == "c" {
			err := handler.FillClean(path)
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

		err := handler.SpeedTest(path, sizeParam, noDelete, shortFormat)
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
		err := handler.Fill(path, sizeParam, autoDelete)
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
		err := handler.Test(path, autoDelete)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Regular info command
	fullScan := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "info" || strings.ToLower(cmd.Arg(1)) == "i")
	shortFormat := cmd.NArg() > 1 && (strings.ToLower(cmd.Arg(1)) == "short" || strings.ToLower(cmd.Arg(1)) == "s")

	// Special handling for folder short format
	if cmdType == CommandFolder && shortFormat {
		fullScan = true
	}

	result, err := handler.Info(path, fullScan)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
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
}
