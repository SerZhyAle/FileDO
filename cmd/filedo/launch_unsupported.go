//go:build !windows

package main

import "os/exec"

func setDetachedProcess(cmd *exec.Cmd) {}
