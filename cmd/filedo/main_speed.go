package main

import (
	"fmt"
	"os"
)

func mainSpeed() {
	os.Exit(runMainSpeed())
}

func runMainSpeed() int {
	if len(os.Args) < 2 {
		fmt.Println("Usage: filedo_speed <args>")
		return 1
	}
	return 0
}
