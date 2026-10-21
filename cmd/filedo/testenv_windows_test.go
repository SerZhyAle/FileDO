//go:build windows

package main

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// The Windows runners of the release workflow hand out the temporary folder in its 8.3 short spelling
// (C:\Users\RUNNER~1\...), while FileDO reports every path in its canonical long spelling. A test that
// builds a path from t.TempDir() and compares it with what the program prints passes on a developer
// machine, whose user name is short enough to have no such spelling, and fails on the runner. The test
// binary therefore runs with the temporary folder spelled the canonical way, whatever the environment
// says. init runs before TestMain and before any t.TempDir().
func init() {
	t := os.TempDir()
	long, ok := longPathName(t)
	if ok && len(long) > 1 && long[1] == ':' {
		long = strings.ToUpper(long[:1]) + long[1:] // GetLongPathName keeps the drive letter as it was given
	}
	if ok && long != t {
		os.Setenv("TMP", long)
		os.Setenv("TEMP", long)
	}
}

// longPathName is the canonical long spelling of an existing path.
func longPathName(p string) (string, bool) {
	in, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", false
	}
	buf := make([]uint16, 4*windows.MAX_PATH)
	n, err := windows.GetLongPathName(in, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 || int(n) > len(buf) {
		return "", false
	}
	return windows.UTF16ToString(buf[:n]), true
}

// An SDDL string spells some well-known accounts as aliases: the built-in Administrator account (RID 500,
// what the runner's user is) is "LA", not its S-1-5-21-..-500. sddlTrustee returns the spelling Windows
// itself gives a SID in an ACE, so a test can look for the right text on every kind of account.
func sddlTrustee(t *testing.T, sid *windows.SID) string {
	t.Helper()
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;" + sid.String() + ")")
	if err != nil {
		t.Fatalf("cannot spell %s the SDDL way: %v", sid, err)
	}
	s := sd.String()
	i, j := strings.LastIndex(s, ";"), strings.LastIndex(s, ")")
	if i < 0 || j <= i {
		t.Fatalf("unexpected SDDL %q", s)
	}
	return s[i+1 : j]
}

func TestSddlTrustee_SpellsAccountsTheWayWindowsDoes(t *testing.T) {
	everyone, err := windows.StringToSid("S-1-1-0")
	if err != nil {
		t.Fatal(err)
	}
	if got := sddlTrustee(t, everyone); got != "WD" {
		t.Errorf("Everyone is spelled %q, want WD", got)
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	// Whatever this account is called in SDDL, an ACE for it is found by that name.
	name := sddlTrustee(t, tu.User.Sid)
	sd, err := windows.SecurityDescriptorFromString("D:(A;;FA;;;" + tu.User.Sid.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sd.String(), ";"+name+")") {
		t.Errorf("the ACE for %s is not found under %q in %s", tu.User.Sid, name, sd.String())
	}
}

// The canonical TEMP is canonical: a second pass changes nothing.
func TestTestEnv_TempIsCanonical(t *testing.T) {
	dir := t.TempDir()
	long, ok := longPathName(dir)
	if !ok {
		t.Skip("the temporary folder cannot be resolved")
	}
	if len(dir) < 2 || dir[1] != ':' || dir[:1] != strings.ToUpper(dir[:1]) {
		t.Errorf("t.TempDir() %q does not start with an upper-case drive letter", dir)
	}
	if !strings.EqualFold(long, dir) || strings.TrimPrefix(long, long[:1]) != strings.TrimPrefix(dir, dir[:1]) {
		t.Errorf("t.TempDir() %q is not the canonical spelling %q", dir, long)
	}
}
