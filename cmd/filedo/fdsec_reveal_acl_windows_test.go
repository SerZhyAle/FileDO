//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// "No other user of the machine can read the copy while it exists" is the one
// clause of S4's exit criterion a test suite cannot prove end to end - that
// needs a second local account logging in and being refused. What it can
// prove is the thing that would make such a test pass or fail: the access list
// the sandbox is created with. Asserting it here turns "the ACL is assumed"
// into "the ACL is read back from the object the OS actually created".
func TestRevealSandbox_AccessListNamesThisUserAndSystemOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rv-acl-probe")
	if err := fdsecSandboxCreate(dir); err != nil {
		t.Fatalf("cannot create the sandbox: %v", err)
	}
	defer os.RemoveAll(dir)

	sd, err := windows.GetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("cannot read the sandbox's security descriptor: %v", err)
	}
	sddl := sd.String()

	// Inheritance off. Without this the sandbox would silently pick up
	// whatever a parent directory grants, which is the whole thing the
	// per-reveal directory exists to avoid.
	dacl := sddl[strings.Index(sddl, "D:"):]
	flags := dacl[2:strings.Index(dacl, "(")]
	if !strings.Contains(flags, "P") {
		t.Errorf("the sandbox DACL is not protected: inheritance is on (flags %q in %s)", flags, sddl)
	}
	if strings.Contains(flags, "AI") {
		t.Errorf("the sandbox DACL inherited entries from its parent (flags %q in %s)", flags, sddl)
	}

	// Exactly two entries, both allow, and nothing denied.
	if n := strings.Count(sddl, "(A;"); n != 2 {
		t.Errorf("the sandbox DACL has %d allow entries, want 2 (this user and SYSTEM)\n%s", n, sddl)
	}
	if strings.Contains(sddl, "(D;") {
		t.Errorf("the sandbox DACL carries a deny entry it was never given\n%s", sddl)
	}

	// And they are the two intended. Everyone, Users, Authenticated Users and
	// Administrators are each named here so that a future edit that lets one
	// of them in fails loudly rather than quietly widening the sandbox.
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
		{";IU)", "Interactive"},
	} {
		if strings.Contains(sddl, unwanted.sid) {
			t.Errorf("the sandbox DACL grants %s access it must not have\n%s", unwanted.who, sddl)
		}
	}
}

// The copy inside inherits that list rather than carrying a weaker one of its
// own - which is what OICI in the sandbox's SDDL is for, and what would break
// silently if someone dropped those flags.
func TestRevealSandbox_TheCopyInheritsTheList(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "rv-acl-inherit")
	if err := fdsecSandboxCreate(dir); err != nil {
		t.Fatalf("cannot create the sandbox: %v", err)
	}
	defer os.RemoveAll(dir)

	copyPath := filepath.Join(dir, "inside.bin")
	if err := os.WriteFile(copyPath, []byte("plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetNamedSecurityInfo(copyPath, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatalf("cannot read the copy's security descriptor: %v", err)
	}
	sddl := sd.String()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sddl, tu.User.Sid.String()) || !strings.Contains(sddl, ";SY)") {
		t.Errorf("the copy did not inherit the sandbox's access list\n%s", sddl)
	}
	for _, unwanted := range []string{";WD)", ";BU)", ";AU)", ";BA)"} {
		if strings.Contains(sddl, unwanted) {
			t.Errorf("the copy is readable by %s\n%s", unwanted, sddl)
		}
	}
}
