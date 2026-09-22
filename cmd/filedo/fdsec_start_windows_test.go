//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// The two acceptance criteria that need the real OS: the access list the
// copy is written under (AC2) and the honest failure when the launched
// handler still has the file open (AC5).
//
// Both go through the shipped functions rather than through the exe, because
// what they assert happens inside one process: a handler that takes and keeps
// an exclusive hold cannot be staged from outside a run that lasts a few
// milliseconds.

// withInterruptHandler gives the sandbox the global handler it registers its
// cleanup with. main() builds one; a test binary has not run main().
func withInterruptHandler(t *testing.T) {
	t.Helper()
	if globalInterruptHandler == nil {
		globalInterruptHandler = NewInterruptHandler()
	}
}

// AC2: the copy lives where no other account can read it. The assertion is
// the same one the reveal sandbox is held to, made here against the directory
// an `unsecure start` actually creates - the two share fdsecSandboxCreate,
// and this is what would fail if a future edit gave the start path a sandbox
// of its own.
func TestStartSandbox_AccessListNamesThisUserAndSystemOnly(t *testing.T) {
	withInterruptHandler(t)
	root := t.TempDir()
	sb, err := fdsecStartSandboxOpen(root)
	if err != nil {
		t.Fatalf("cannot open the start sandbox: %v", err)
	}
	defer sb.close()

	if base := filepath.Base(sb.dir); !strings.HasPrefix(base, fdsecStartDirPrefix) {
		t.Errorf("the sandbox is named %s, which no sweep will recognise as an unsecure-start copy", base)
	}

	sd, err := windows.GetNamedSecurityInfo(sb.dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("cannot read the sandbox's security descriptor: %v", err)
	}
	sddl := sd.String()

	dacl := sddl[strings.Index(sddl, "D:"):]
	flags := dacl[2:strings.Index(dacl, "(")]
	if !strings.Contains(flags, "P") {
		t.Errorf("the sandbox DACL is not protected: inheritance is on (flags %q in %s)", flags, sddl)
	}
	if n := strings.Count(sddl, "(A;"); n != 2 {
		t.Errorf("the sandbox DACL has %d allow entries, want 2 (this user and SYSTEM)\n%s", n, sddl)
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("cannot read this process's user: %v", err)
	}
	if !strings.Contains(sddl, tu.User.Sid.String()) {
		t.Errorf("the sandbox DACL does not name this user %s\n%s", tu.User.Sid, sddl)
	}
	if !strings.Contains(sddl, ";SY)") {
		t.Errorf("the sandbox DACL does not name SYSTEM\n%s", sddl)
	}
	for _, unwanted := range []struct{ sid, who string }{
		{";WD)", "Everyone"},
		{";BU)", "Users"},
		{";AU)", "Authenticated Users"},
		{";BA)", "Administrators"},
	} {
		if strings.Contains(sddl, unwanted.sid) {
			t.Errorf("the sandbox DACL grants %s access it must not have\n%s", unwanted.who, sddl)
		}
	}
}

// AC5: one attempt, and it is allowed to fail.
//
// A player that still holds the copy is the common case, not the exotic one -
// SP-0005's probe P3 measured the hold taken in 0.01 s - and Windows refuses
// to unlink a file another process holds. The spec's trade is that the
// attempt then gives up rather than polling, says so, and leaves the copy for
// the startup sweep. Both halves are asserted here, because half of it is a
// leak and the other half is a copy nothing ever reclaims.
func TestStartSandbox_ALockedCopySurvivesTheAttemptAndTheSweepTakesItLater(t *testing.T) {
	withInterruptHandler(t)
	root := t.TempDir()
	t.Setenv(fdsecRevealRootEnv, root)

	sb, err := fdsecStartSandboxOpen(root)
	if err != nil {
		t.Fatalf("cannot open the start sandbox: %v", err)
	}
	copyPath := filepath.Join(sb.dir, "holiday.mp4")
	if err := os.WriteFile(copyPath, []byte("plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}
	sb.copyPath = copyPath

	// The handler takes it and does not let go.
	release := holdExclusively(t, copyPath)

	sb.close()
	if !exists(copyPath) {
		t.Fatal("the copy was removed while a handler held it open - which Windows should not have allowed at all")
	}
	// The lock is released even when the removal failed, or nothing would
	// ever be entitled to reclaim what was left behind.
	if fdsecIsHeld(sb.lockPath) {
		t.Error("the sandbox still holds its lock after the attempt, so no sweep will ever touch it")
	}

	// The sweep is entitled to try, and is still refused by the same hold -
	// it must not delete a copy that is open on screen.
	fdsecSweepReveals()
	if !exists(copyPath) {
		t.Fatal("the sweep removed a copy a handler still had open")
	}

	// The handler closes. The next FileDO start reclaims what the run itself
	// could not - section 4.4's backstop, which is the whole reason a leftover
	// is not forever.
	release()
	fdsecSweepReveals()
	if exists(sb.dir) {
		t.Errorf("the sandbox survived the sweep that followed the handler letting go: %s", sb.dir)
	}
	if exists(sb.lockPath) {
		t.Errorf("the sweep left the lock file behind: %s", sb.lockPath)
	}
}
