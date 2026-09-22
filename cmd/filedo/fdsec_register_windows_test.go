//go:build windows

package main

// Black-box tests for `fdsec register` / `fdsec unregister`.
//
// They drive the real command and read the real registry, but never the
// user's own shell: FILEDO_FDSEC_REG_ROOT points the whole registration at a
// scratch key under HKCU, the way FILEDO_FDSEC_REVEAL_ROOT moves the reveal
// sandbox. What is exercised is therefore the shipped code path, with only
// the location changed - the key names, the command lines, the ownership
// checks and the stand-down rule are the ones a real registration uses.

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

// regSeam creates a scratch registry root for one test and removes it
// afterwards, returning the root path and the two scope prefixes under it.
func regSeam(t *testing.T) (root, userClasses, machineClasses string) {
	t.Helper()
	root = fmt.Sprintf(`Software\FileDO\test-shell\%d-%d`, os.Getpid(), time.Now().UnixNano())
	t.Cleanup(func() { deleteSeamTree(root) })
	return root, root + `\user\Classes`, root + `\machine\Classes`
}

func deleteSeamTree(path string) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return
	}
	children, _ := k.ReadSubKeyNames(-1)
	k.Close()
	for _, c := range children {
		deleteSeamTree(path + `\` + c)
	}
	_ = registry.DeleteKey(registry.CURRENT_USER, path)
}

// runReg runs filedo with the registry seam set. It is `run` with one more
// environment entry; keeping it separate leaves the shared helper untouched
// for the suite that must NOT be able to write to the registry at all.
func runReg(t *testing.T, wd, seam string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(wd),
		"FILEDO_FDSEC_REG_ROOT="+seam,
	)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running filedo %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

func regString(t *testing.T, key, name string) (string, bool) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, key, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", false
	}
	return v, true
}

func mustRegString(t *testing.T, key, name string) string {
	t.Helper()
	v, ok := regString(t, key, name)
	if !ok {
		t.Fatalf("%s\\%s (value %q) is missing", "HKCU", key, name)
	}
	return v
}

// The two things SP-0005 9.1 registers, and nothing else: the cascading
// "File DO.." group on all files, and the .fd-sec document type.
func TestFdsecRegisterWritesTheGroupAndTheDocumentType(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	out, code := runReg(t, dir, seam, "fdsec", "register")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}

	if got := mustRegString(t, userClasses+`\.fd-sec`, ""); got != "FileDO.SecureContainer" {
		t.Errorf(".fd-sec points at %q, want the FileDO document type", got)
	}
	// The open command, asserted byte-for-byte against fdsecOpenCommand in
	// fdsec_register_open_test.go; here it is enough to see it land in the
	// registry unchanged.
	open := mustRegString(t, userClasses+`\FileDO.SecureContainer\shell\open\command`, "")
	if !strings.Contains(strings.ToLower(open), `"%1" unsecure start`) {
		t.Errorf("the open command does not unsecure and start the clicked container: %q", open)
	}
	if !strings.Contains(strings.ToLower(open), ".exe") {
		t.Errorf("the open command does not name an executable: %q", open)
	}
	if strings.Contains(strings.ToLower(open), "filedo_win.exe") {
		t.Errorf("the open command still names the GUI: %q", open)
	}
	// The document type carries no verb of its own any more: everything a
	// container needs is in the group, which is on every file.
	if _, ok := regString(t, userClasses+`\FileDO.SecureContainer\shell\unsecure`, ""); ok {
		t.Error("the retired \"Unsecure here\" verb was written again")
	}
	if _, ok := regString(t, userClasses+`\*\shell\FileDO.Secure`, ""); ok {
		t.Error("the retired single all-files verb was written again")
	}
	// An icon, or the document type shows the generic unknown-file page.
	if _, ok := regString(t, userClasses+`\FileDO.SecureContainer\DefaultIcon`, ""); !ok {
		t.Error("the document type has no icon")
	}

	// The group. An EMPTY SubCommands value is what makes the shell read the
	// `shell` subkey below as a cascade; a missing one makes the key an
	// ordinary verb with no command, which draws a dead entry.
	group := userClasses + `\*\shell\FileDO`
	if label := mustRegString(t, group, "MUIVerb"); label != "File DO.." {
		t.Errorf("the group is labelled %q", label)
	}
	if v, ok := regString(t, group, "SubCommands"); !ok || v != "" {
		t.Errorf("SubCommands is %q (present=%v), want an empty string", v, ok)
	}
	// A (Default) value on the group makes the shell build the sub-menu empty:
	// the arrow is drawn and nothing opens. Found on a real Windows 11 machine
	// by asking the shell for the menu; the label lives in MUIVerb alone.
	if v, ok := regString(t, group, ""); ok {
		t.Errorf("the group has a (Default) value %q - the sub-menu would open empty", v)
	}

	// Every entry the owner asked for, each one running the verb it says.
	for _, want := range []struct{ key, label, cmd string }{
		{"10Secure", "Secure", `"%1" secure`},
		{"20SecureDel", "Secure and delete original", `"%1" secure del`},
		{"30SecureWipe", "Secure and wipe original", `"%1" secure wipe`},
		{"40SecureRename", "Secure with a random name", `"%1" secure rename`},
		{"50Unsecure", "Unsecure", `"%1" unsecure`},
		{"60UnsecureDel", "Unsecure and delete container", `"%1" unsecure del -y`},
		{"65UnsecureStart", "Unsecure and start", `"%1" unsecure start`},
		{"70Wipe", "Wipe this file", `file "%1" wipe`},
		{"80Check", "Check this file", `check "%1"`},
		{"90Info", "Info", `file "%1" info`},
	} {
		item := group + `\shell\` + want.key
		if label := mustRegString(t, item, "MUIVerb"); label != want.label {
			t.Errorf("%s is labelled %q, want %q", want.key, label, want.label)
		}
		cmd := mustRegString(t, item+`\command`, "")
		if !strings.Contains(cmd, want.cmd) {
			t.Errorf("%s runs %q, want it to contain %q", want.key, cmd, want.cmd)
		}
		// Explorer closes the window when the process ends, so every entry
		// has to hold it open or its output is unreadable.
		if !strings.Contains(cmd, "--pause") {
			t.Errorf("%s does not hold the console open: %q", want.key, cmd)
		}
	}

	// The report has to name what it wrote: a registration nobody can read
	// back is one nobody can undo by hand.
	for _, want := range []string{".fd-sec", "FileDO.SecureContainer", "File DO..", "unregister"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report never mentions %q\n%s", want, out)
		}
	}
}

// An upgrade takes the first shape's entries away rather than leaving them
// beside the group, where they would say the same thing twice.
func TestFdsecRegisterRemovesTheRetiredEntries(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	// The old shape, written by hand with FileDO's own mark on it - which is
	// what an earlier version left behind.
	for _, v := range []struct{ key, name, value string }{
		{userClasses + `\*\shell\FileDO.Secure`, "", "Secure with FileDO"},
		{userClasses + `\*\shell\FileDO.Secure`, "FileDO.RegisteredBy", `C:\Program Files\FileDO\filedo.exe`},
		{userClasses + `\*\shell\FileDO.Secure\command`, "", `"C:\Program Files\FileDO\filedo.exe" "%1" secure`},
		{userClasses + `\FileDO.SecureContainer\shell\unsecure`, "", "Unsecure here"},
		{userClasses + `\FileDO.SecureContainer\shell\unsecure\command`, "", `"C:\Program Files\FileDO\filedo.exe" "%1" unsecure here`},
	} {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, v.key, registry.SET_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		if err := k.SetStringValue(v.name, v.value); err != nil {
			t.Fatal(err)
		}
		k.Close()
	}

	out, code := runReg(t, dir, seam, "fdsec", "register")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	if _, ok := regString(t, userClasses+`\*\shell\FileDO.Secure\command`, ""); ok {
		t.Error("the retired single verb survived a re-register")
	}
	if _, ok := regString(t, userClasses+`\FileDO.SecureContainer\shell\unsecure\command`, ""); ok {
		t.Error("the retired \"Unsecure here\" verb survived a re-register")
	}
	if !strings.Contains(out, "retired") {
		t.Errorf("the report does not say what it took away\n%s", out)
	}
}

// An earlier build wrote a (Default) on the group, and that value is what made
// the sub-menu open empty. Writing the right shape over it does not remove it,
// so a re-register has to.
func TestFdsecRegisterHealsAGroupDefaultValue(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	group := userClasses + `\*\shell\FileDO`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, group, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("", "File DO.."); err != nil {
		t.Fatal(err)
	}
	k.Close()

	if out, code := runReg(t, dir, seam, "fdsec", "register"); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	if v, ok := regString(t, group, ""); ok {
		t.Errorf("the group still has a (Default) value %q after a re-register", v)
	}
	if label := mustRegString(t, group, "MUIVerb"); label != "File DO.." {
		t.Errorf("the group lost its label: %q", label)
	}
}

// A foreign all-files verb that merely happens to use the old key name is
// somebody else's, and a re-register leaves it where it is.
func TestFdsecRegisterLeavesAnUnmarkedLegacyKeyAlone(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	k, _, err := registry.CreateKey(registry.CURRENT_USER,
		userClasses+`\*\shell\FileDO.Secure\command`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("", `"C:\Windows\notepad.exe" "%1"`); err != nil {
		t.Fatal(err)
	}
	k.Close()

	if out, code := runReg(t, dir, seam, "fdsec", "register"); code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	got := mustRegString(t, userClasses+`\*\shell\FileDO.Secure\command`, "")
	if !strings.Contains(strings.ToLower(got), "notepad") {
		t.Errorf("an unmarked key was taken away: %q", got)
	}
}

// Unregister removes exactly what register wrote, and is safe to run twice.
func TestFdsecUnregisterRemovesWhatItWrote(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	if out, code := runReg(t, dir, seam, "fdsec", "register"); code != 0 {
		t.Fatalf("register exit %d\n%s", code, out)
	}
	out, code := runReg(t, dir, seam, "fdsec", "unregister")
	if code != 0 {
		t.Fatalf("unregister exit %d, want 0\n%s", code, out)
	}
	for _, key := range []string{
		userClasses + `\.fd-sec`,
		userClasses + `\FileDO.SecureContainer`,
		userClasses + `\FileDO.SecureContainer\shell\open\command`,
		userClasses + `\*\shell\FileDO`,
		userClasses + `\*\shell\FileDO\shell\10Secure\command`,
	} {
		if _, ok := regString(t, key, ""); ok {
			t.Errorf("%s survived the unregister", key)
		}
	}

	out, code = runReg(t, dir, seam, "fdsec", "unregister")
	if code != 0 {
		t.Fatalf("a second unregister exit %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "Nothing to remove") {
		t.Errorf("a second unregister does not say there was nothing left\n%s", out)
	}
}

// The machine-wide registration wins. A per-user copy on top of it would put
// every entry in the menu twice, with no way to tell which one an uninstall
// takes away - so the per-user register stands down and says so.
func TestFdsecPerUserRegisterStandsDownForTheMachineCopy(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, machineClasses := regSeam(t)

	if out, code := runReg(t, dir, seam, "fdsec", "register", "-all-users"); code != 0 {
		t.Fatalf("machine register exit %d\n%s", code, out)
	}
	if _, ok := regString(t, machineClasses+`\FileDO.SecureContainer\shell\open\command`, ""); !ok {
		t.Fatal("the machine-wide registration was not written")
	}

	out, code := runReg(t, dir, seam, "fdsec", "register")
	if code != 0 {
		t.Fatalf("exit %d, want 0 (standing down is not a failure)\n%s", code, out)
	}
	if !strings.Contains(out, "Already registered for all users") {
		t.Errorf("the stand-down does not say why nothing was written\n%s", out)
	}
	if _, ok := regString(t, userClasses+`\FileDO.SecureContainer\shell\open\command`, ""); ok {
		t.Error("a per-user copy was written on top of the machine-wide one")
	}
}

// A document type that now belongs to another program is that program's.
// Removing it would break an association this command never made.
func TestFdsecUnregisterRefusesAForeignOwner(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	k, _, err := registry.CreateKey(registry.CURRENT_USER,
		userClasses+`\FileDO.SecureContainer\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("", `"C:\Windows\notepad.exe" "%1"`); err != nil {
		t.Fatal(err)
	}
	k.Close()

	out, code := runReg(t, dir, seam, "fdsec", "unregister")
	if code == 0 {
		t.Fatalf("exit 0: a foreign registration was removed\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "notepad") {
		t.Errorf("the refusal does not name what it found\n%s", out)
	}
	if _, ok := regString(t, userClasses+`\FileDO.SecureContainer\shell\open\command`, ""); !ok {
		t.Error("the foreign command was removed anyway")
	}
}

// Whoever owned .fd-sec before FileDO did gets it back.
func TestFdsecUnregisterHandsTheExtensionBack(t *testing.T) {
	dir := t.TempDir()
	seam, userClasses, _ := regSeam(t)

	k, _, err := registry.CreateKey(registry.CURRENT_USER, userClasses+`\.fd-sec`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("", "Some.Other.Type"); err != nil {
		t.Fatal(err)
	}
	k.Close()

	if out, code := runReg(t, dir, seam, "fdsec", "register"); code != 0 {
		t.Fatalf("register exit %d\n%s", code, out)
	}
	if out, code := runReg(t, dir, seam, "fdsec", "unregister"); code != 0 {
		t.Fatalf("unregister exit %d\n%s", code, out)
	}
	if got := mustRegString(t, userClasses+`\.fd-sec`, ""); got != "Some.Other.Type" {
		t.Errorf(".fd-sec came back as %q, want the previous owner", got)
	}
}
