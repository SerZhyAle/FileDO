package main

// launcher_windows_test.go is identical in cmd/filedo-check, cmd/filedo-fill
// and cmd/filedo-test.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

var procGenerateConsoleCtrlEvent = syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")

// TestOutlivesCtrlBreakAndReturnsTheChildsCode is the "do not die first"
// proof. The launcher is started in a process group of its own, filedo.exe
// (the stub) joins it, and Ctrl+Break - which Go delivers as os.Interrupt,
// exactly like Ctrl+C - is sent to that group. The stub keeps working for a
// moment after the interrupt and then exits 7; the launcher has to be still
// there to return that 7, rather than dying on the same keypress with
// STATUS_CONTROL_C_EXIT and leaving filedo.exe behind.
func TestOutlivesCtrlBreakAndReturnsTheChildsCode(t *testing.T) {
	_, launcher := install(t, true)
	scratch := t.TempDir()
	ready := filepath.Join(scratch, "ready")
	recordPath := filepath.Join(scratch, "record.json")

	cmd := exec.Command(launcher, sampleArgs...)
	cmd.Env = append(os.Environ(),
		"FILEDO_STUB_RECORD="+recordPath,
		"FILEDO_STUB_WAIT_INTERRUPT="+ready,
		"FILEDO_STUB_EXIT=7")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stop := func() {
		_ = cmd.Process.Kill()
		<-done
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("the launcher ended before filedo.exe was ready: %v\n%s%s", err, stdout.String(), stderr.String())
		default:
		}
		if time.Now().After(deadline) {
			stop()
			t.Fatal("filedo.exe never became ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if r, _, callErr := procGenerateConsoleCtrlEvent.Call(uintptr(syscall.CTRL_BREAK_EVENT), uintptr(cmd.Process.Pid)); r == 0 {
		stop()
		t.Skipf("no console to deliver Ctrl+Break through: %v", callErr)
	}

	var err error
	select {
	case err = <-done:
	case <-time.After(30 * time.Second):
		stop()
		t.Fatal("the launcher did not end after filedo.exe did")
	}
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatal(err)
		}
		code = exitErr.ExitCode()
	}
	if code != 7 {
		t.Errorf("exit %d (0x%X), want filedo.exe's 7: the launcher did not wait for it\n%s", code, uint32(code), stderr.String())
	}
	data, readErr := os.ReadFile(recordPath)
	if readErr != nil {
		t.Fatalf("filedo.exe left no record: %v", readErr)
	}
	var rec stubRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatal(err)
	}
	if !rec.Interrupted {
		t.Errorf("filedo.exe did not see the interrupt itself")
	}
}
