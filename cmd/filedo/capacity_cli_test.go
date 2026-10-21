package main

// Black-box tests of the capacity verbs (SP-0026), through the exe TestMain
// in fdsec_cli_test.go builds. Every target is a scratch folder, and none of
// these runs writes test data: each ends at a usage error, a refused write,
// an empty folder or a verification of files the test wrote itself.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CAP-05 / CLI-03: `fill clean` - the cleanup hint FileDO itself used to
// print - is a usage error that writes nothing.
func TestFillRejectsUnknownSizeToken(t *testing.T) {
	for _, args := range [][]string{{"fill", "clean"}, {"fill", "20000"}, {"fill", "1GBx"}, {"speed", "lots"}, {"test", "many"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			dir := t.TempDir()
			events := filepath.Join(t.TempDir(), "events.jsonl")
			out, code := run(t, t.TempDir(), append([]string{"--events", events, dir}, args...)...)
			if code != 2 {
				t.Fatalf("exit %d, want 2\n%s", code, out)
			}
			if got := lastResult(t, events)["verdict"]; got != "Not proven" {
				t.Errorf("verdict %q, want Not proven", got)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Fatalf("a usage error left %d entries in the target", len(entries))
			}
		})
	}
}

// CAP-05: every tester's cleanup hint, run as printed, is the clean verb.
func TestCleanupHintIsTheCleanVerb(t *testing.T) {
	dir := t.TempDir()
	for _, tester := range []FakeCapacityTester{NewDeviceTester(dir), NewFolderTester(dir), &NetworkTester{networkPath: dir}} {
		args := splitHint(tester.GetCleanupCommand())
		out, code := run(t, t.TempDir(), args[1:]...)
		if code != 0 || !strings.Contains(out, "Clean Operation") || !strings.Contains(out, "No FileDO test files found") {
			t.Fatalf("%v: exit %d\n%s", args, code, out)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Fatalf("the hint %v wrote into the target", args)
		}
	}
}

// CAP-08: fill verify with nothing to verify is not proven (exit 2).
func TestFillVerifyWithNoFilesIsNotProven(t *testing.T) {
	events := filepath.Join(t.TempDir(), "events.jsonl")
	out, code := run(t, t.TempDir(), "--events", events, t.TempDir(), "fill", "verify")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
	if got := lastResult(t, events)["verdict"]; got != "Not proven" {
		t.Errorf("verdict %q, want Not proven", got)
	}
}

// CAP-14: clean off a terminal - the GUI's shape - removes nothing without
// --yes, and removes FileDO's files only with it.
func TestCleanNeedsConfirmationOffATerminal(t *testing.T) {
	dir := t.TempDir()
	ours := filepath.Join(dir, capacityFileName(1, 5, "01000000", "0badf00d"))
	if _, err := writeFillFile(context.Background(), ours, 64<<10, nil); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(dir, "speedtest_x.txt")
	if err := os.WriteFile(foreign, []byte("the user's"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, t.TempDir(), dir, "clean")
	if code != 2 || !exists(ours) {
		t.Fatalf("clean without --yes: exit %d, file kept %v\n%s", code, exists(ours), out)
	}
	out, code = run(t, t.TempDir(), dir, "clean", "--yes")
	if code != 0 || exists(ours) || !exists(foreign) {
		t.Fatalf("clean --yes: exit %d, ours kept %v, foreign kept %v\n%s", code, exists(ours), exists(foreign), out)
	}
}

// readOnlyDir creates a folder this user can list but not write: the shape of
// a folder on someone else's share, or one an administrator locked.
func readOnlyDir(t *testing.T) string {
	t.Helper()
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	sid := tu.User.Sid.String()
	dir := filepath.Join(t.TempDir(), "locked")
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;0x1200a9;;;" + sid + ")")
	if err != nil {
		t.Fatal(err)
	}
	sa := windows.SecurityAttributes{SecurityDescriptor: sd}
	sa.Length = uint32(unsafe.Sizeof(sa))
	p, _ := windows.UTF16PtrFromString(dir)
	if err := windows.CreateDirectory(p, &sa); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		full, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;" + sid + ")")
		if err != nil {
			return
		}
		if dacl, _, err := full.DACL(); err == nil {
			windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
				windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
		}
	})
	if f, err := os.Create(filepath.Join(dir, "probe")); err == nil {
		f.Close()
		t.Skip("the access list did not take effect (running with backup privileges?)")
	}
	return dir
}

// CAP-04: a folder this user cannot write is "could not verify" (exit 2),
// never "fake capacity".
func TestReadOnlyFolderTestIsNotProven(t *testing.T) {
	dir := readOnlyDir(t)
	// A safety net under the refused write: were the lock not to hold, the
	// test could never plan more than a few hundred megabytes.
	t.Setenv(testFreeBytesEnv, "209715200")
	events := filepath.Join(t.TempDir(), "events.jsonl")
	out, code := run(t, t.TempDir(), "--events", events, dir, "test", "3")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
	if got := lastResult(t, events)["verdict"]; got != "Not proven" {
		t.Errorf("verdict %q, want Not proven", got)
	}
	if !strings.Contains(out, "not writable") {
		t.Errorf("the refusal is not the write probe's:\n%s", out)
	}
	if strings.Contains(out, "TEST FAILED") {
		t.Errorf("a locked folder was reported as a failed device:\n%s", out)
	}
}
