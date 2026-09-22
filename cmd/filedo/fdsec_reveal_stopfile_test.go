//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Mechanism 2 over a machine channel (spec 8.2, stage S6).
//
// A reveal started by the shell has no console: stdin is a pipe, so the Enter
// prompt cannot exist. What does exist is the --stop-file the shell always
// passes, and the window it belongs to - which is the same visible "remove it
// now" the console offers, wearing a button instead of a key. The contract is
// one statement in two halves: without the button nothing ends the reveal on a
// timer, and with it the copy goes at once.
//
// What history.json records is not re-proven here: the reveal writes the same
// entry whichever mechanism ends it, and
// TestReveal_DoesNotWriteTheSealedNameIntoHistory already holds that line.

// startRevealWithStopFile launches a console-less reveal that has a machine
// channel, and returns the stop file's path.
func startRevealWithStopFile(t *testing.T, wd string) (stopFile string, cmdArgs []string) {
	t.Helper()
	stopFile = filepath.Join(wd, "shell.stop")
	return stopFile, []string{"--stop-file", stopFile, "plain.fd-sec", "reveal", "p:" + revealSecret}
}

// A handler that never locks the file - S0's probe P3 measured the packaged
// Notepad and Explorer's zip view doing exactly that - must not cost the user
// the file they are looking at. On a console the Enter prompt holds the copy;
// under the shell the stop file does, and this asserts the wait is real rather
// than a ten-second timer.
func TestReveal_MachineChannelKeepsTheCopyUntilTheShellSaysSo(t *testing.T) {
	dir, _ := workdir(t)
	secureOne(t, dir, "plain.txt")

	stopFile, args := startRevealWithStopFile(t, dir)
	cmd, buf, mu := startReveal(t, dir, args...)
	copyPath := awaitCopy(t, dir, "plain.txt", 20*time.Second)

	// Nobody ever takes a hold. Past the acquire wait, mechanism 1 has given
	// its answer - "no handler ever held it" - and the reveal must still be
	// open, because there is a window that has not been asked yet.
	time.Sleep(fdsecRevealAcquireWait + 3*time.Second)

	if cmd.ProcessState != nil {
		mu.Lock()
		out := buf.String()
		mu.Unlock()
		t.Fatalf("the reveal ended on a timer although the shell never asked\n%s", out)
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("the copy was removed although the shell never asked: %v", err)
	}

	// The shell's "remove the copy now".
	if err := os.WriteFile(stopFile, []byte("stop"), 0o644); err != nil {
		t.Fatalf("cannot write the stop file: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the reveal exited with an error: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the reveal did not end after the shell asked")
	}

	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if !strings.Contains(out, "Reveal closed (you said so)") {
		t.Errorf("the reveal did not close for the reason mechanism 2 gives\n%s", out)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Errorf("the copy survived the shell's removal (err=%v)\n%s", err, out)
	}
	if n := countRevealSandboxes(t, dir); n != 0 {
		t.Errorf("%d sandbox(es) survived\n%s", n, out)
	}
}
