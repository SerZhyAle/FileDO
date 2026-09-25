//go:build windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filedo/statedir"

	"golang.org/x/sys/windows"
)

// Lifetime mechanism 1, against a real exclusive hold rather than a mock. The
// first clause of S4's exit criterion is "a video opens in its registered
// player and the copy is gone afterwards"; the player is what this substitutes
// for, by doing the one thing about a player that matters here - taking the
// file and later letting go of it.

// holdExclusively opens path the way a media player does and returns a
// function that lets go.
//
// It retries, because the test watches the filesystem for the copy to appear
// while the reveal is still finishing it - writing the untrusted-origin
// stream and setting it read-only - and those briefly hold the file
// themselves. A real handler never sees this: the reveal launches it only
// after all of that is done. The retry makes the test wait for the same
// moment rather than assert against a window that does not exist in use.
func holdExclusively(t *testing.T, path string) func() {
	t.Helper()
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		h, oerr := windows.CreateFile(p, windows.GENERIC_READ, 0, nil,
			windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
		if oerr == nil {
			return func() { windows.CloseHandle(h) }
		}
		if time.Now().After(deadline) {
			t.Fatalf("cannot take an exclusive hold on %s: %v", path, oerr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// awaitCopy waits for a reveal to produce its sandbox copy and returns its
// path. It is the test's stand-in for "the player has the file open now".
func awaitCopy(t *testing.T, wd, name string, within time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(revealRoot(wd))
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() || !strings.HasPrefix(e.Name(), "rv-") {
					continue
				}
				candidate := filepath.Join(revealRoot(wd), e.Name(), name)
				if _, serr := os.Stat(candidate); serr == nil {
					return candidate
				}
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no sandbox copy of %s appeared within %s", name, within)
	return ""
}

// startReveal launches a reveal that will block on its lifetime mechanisms,
// and returns the process and a way to read what it printed.
func startReveal(t *testing.T, wd string, args ...string) (*exec.Cmd, *bytes.Buffer, *sync.Mutex) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(wd),
		statedir.EnvOverride+"="+wd,
	)
	cmd.Stdin = strings.NewReader("")
	var mu sync.Mutex
	buf := &bytes.Buffer{}
	cmd.Stdout = &lockedWriter{mu: &mu, buf: buf}
	cmd.Stderr = cmd.Stdout.(*lockedWriter)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the reveal: %v", err)
	}
	return cmd, buf, &mu
}

type lockedWriter struct {
	mu  *sync.Mutex
	buf *bytes.Buffer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

func TestReveal_Mechanism1_ClosesWhenTheHandlerLetsGo(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	cmd, buf, mu := startReveal(t, dir, "plain.fd-sec", "reveal", "p:"+secret)
	copyPath := awaitCopy(t, dir, "plain.txt", 20*time.Second)

	// The player takes it, and keeps it past the minimum a hold must last to
	// count - a shorter one is a reader, not a player, and
	// TestReveal_ABriefReadIsNotAHold holds that half.
	release := holdExclusively(t, copyPath)
	time.Sleep(fdsecRevealHoldMin + time.Second)

	// A hold dropped and taken again inside the settle window is not the
	// handler finishing - S0's probe P3 measured a release edge meaning only
	// "the app moved on". This gap proves the settle window is real: without
	// it the reveal would end here.
	release()
	time.Sleep(fdsecRevealSettle / 3)
	release = holdExclusively(t, copyPath)
	time.Sleep(2 * time.Second)

	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("the copy was removed during a momentary release: %v", err)
	}
	if cmd.ProcessState != nil {
		t.Fatalf("the reveal ended during a momentary release")
	}

	// Now the player closes for good.
	release()

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the reveal exited with an error: %v", err)
		}
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the reveal did not end after the hold was released")
	}

	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if !strings.Contains(out, "the handler let go of it") {
		t.Errorf("the reveal did not close for the reason mechanism 1 gives\n%s", out)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Errorf("the copy survived the handler letting go (err=%v)\n%s", err, out)
	}
	if n := countRevealSandboxes(t, dir); n != 0 {
		t.Errorf("%d sandbox(es) survived\n%s", n, out)
	}
}

// A FileDO killed mid-reveal leaves a copy that the next start removes - the
// second clause of the exit criterion, with a real kill rather than a
// simulated leftover.
func TestReveal_KilledMidRevealIsReclaimedByTheNextStart(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	cmd, _, _ := startReveal(t, dir, "plain.fd-sec", "reveal", "p:"+secret)
	copyPath := awaitCopy(t, dir, "plain.txt", 20*time.Second)
	hold := holdExclusively(t, copyPath) // keep it alive so the kill matters

	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("cannot kill the reveal: %v", err)
	}
	_ = cmd.Wait()
	hold()

	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("the killed reveal left nothing behind, so there is nothing to sweep: %v", err)
	}

	out, code := run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+revealSecret)
	if code != 0 {
		t.Fatalf("the next start exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "Removed a leftover reveal") {
		t.Errorf("the next start did not reclaim the killed reveal's copy\n%s", out)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Errorf("the copy survived the next start (err=%v)\n%s", err, out)
	}
}

// The lock a live reveal holds is what lets the two coexist: a FileDO started
// while another is revealing sweeps the leftovers and steps around the live
// sandbox. Without it, mechanism 3 would delete a copy that is on screen.
func TestReveal_ASecondFileDODoesNotSweepALiveSandbox(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	cmd, _, _ := startReveal(t, dir, "plain.fd-sec", "reveal", "p:"+secret)
	copyPath := awaitCopy(t, dir, "plain.txt", 20*time.Second)
	release := holdExclusively(t, copyPath)

	// A second FileDO starts, doing something unrelated. Its sweep runs.
	out, code := run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+revealSecret)
	if code != 0 {
		t.Fatalf("the second FileDO exited %d\n%s", code, out)
	}
	if strings.Contains(out, "Removed a leftover reveal") {
		t.Errorf("the second FileDO swept a sandbox that was in use\n%s", out)
	}
	if _, err := os.Stat(copyPath); err != nil {
		t.Errorf("the live copy was deleted by another FileDO's startup sweep: %v\n%s", err, out)
	}

	release()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("the reveal did not end after the hold was released")
	}
}
