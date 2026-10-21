//go:build windows

package main

// Black-box tests for `vd register` / `vd unregister` (SP-0004 P6 T6.16), in
// the style of the sibling's (fdsec_register_windows_test.go): the real
// command, the real registry, never the user's own shell - FILEDO_FDSEC_REG_ROOT
// moves the whole registration to a scratch key under HKCU, and nothing else.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filedo/statedir"

	"golang.org/x/sys/windows/registry"
)

// vdTestInstallDir lays out what the setup, winget and the zip install: filedo.exe with
// filedo_win.exe beside it, and (withIcons) the icons folder. Only the CLI is real; the
// GUI and the icons are placeholders, because the registration only names them.
func vdTestInstallDir(t *testing.T, withIcons bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "FileDO")
	if err := os.MkdirAll(filepath.Join(dir, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filedoExe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "filedo.exe"), body, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"filedo_win.exe": "gui", "FileDO.ico": "icon"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withIcons {
		if err := os.WriteFile(filepath.Join(dir, "icons", vdContainerIcon+".ico"), []byte("icon"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runRegExe is runReg for an exe other than the suite's own build: the install layout
// above, whose folder is what the registration names. The state root is wd, so the
// change log (vdisk.log) lands there.
func runRegExe(t *testing.T, exe, wd, seam string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(exe, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(wd),
		statedir.EnvOverride+"="+wd,
		fdsecRegRootEnv+"="+seam,
	)
	if os.Getenv(fdsecPackagedMenuEnv) == "" {
		cmd.Env = append(cmd.Env, fdsecPackagedMenuEnv+"=0")
	}
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %s %v: %v", exe, args, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// vdRegisteredDir is the folder the registering exe resolved itself to, read from the mark.
func vdRegisteredDir(t *testing.T, classes string) string {
	t.Helper()
	return filepath.Dir(mustRegString(t, classes+`\`+vdProgID, fdsecOwnerValue))
}

// P6 section 4.1, key by key.
func TestVdRegister_WritesTheDiskContainerTable(t *testing.T) {
	inst := vdTestInstallDir(t, true)
	wd := t.TempDir()
	seam, user, _ := regSeam(t)
	out, code := runRegExe(t, filepath.Join(inst, "filedo.exe"), wd, seam, "vd", "register")
	if code != 0 {
		t.Fatalf("exit %d, want 0\n%s", code, out)
	}
	dir := vdRegisteredDir(t, user)
	gui := filepath.Join(dir, "filedo_win.exe")
	for _, want := range []struct{ key, name, value string }{
		{`.fdd`, "", "FileDO.DiskContainer"},
		{`.fdd\OpenWithProgids`, "FileDO.DiskContainer", ""},
		{`FileDO.DiskContainer`, "", "FileDO disk container"},
		{`FileDO.DiskContainer`, "FriendlyTypeName", "FileDO disk container"},
		{`FileDO.DiskContainer\DefaultIcon`, "", filepath.Join(dir, "icons", "content.disk-container.ico")},
		{`FileDO.DiskContainer\shell`, "", "mount"},
		{`FileDO.DiskContainer\shell\mount`, "MUIVerb", "Mount with FileDO"},
		{`FileDO.DiskContainer\shell\mount\command`, "", `"` + gui + `" "%1"`},
		{`FileDO.DiskContainer\shell\mountro`, "MUIVerb", "Mount read-only"},
		{`FileDO.DiskContainer\shell\mountro\command`, "", `"` + gui + `" --mount-ro "%1"`},
		{`FileDO.DiskContainer\shell\unmount`, "MUIVerb", "Unmount"},
		{`FileDO.DiskContainer\shell\unmount\command`, "", `"` + gui + `" --unmount "%1"`},
	} {
		if got := mustRegString(t, user+`\`+want.key, want.name); got != want.value {
			t.Errorf("%s [%s] = %q, want %q", want.key, want.name, got, want.value)
		}
	}
	// The double-click never opens a console, and never the sibling's secure page.
	for _, v := range vdShellVerbs {
		cmd := mustRegString(t, user+`\`+vdProgID+`\shell\`+v.key+`\command`, "")
		if strings.Contains(strings.ToLower(cmd), `\filedo.exe"`) {
			t.Errorf("%s runs the console: %q", v.key, cmd)
		}
	}
	// Nothing of the sibling's is written by this command.
	if _, ok := regString(t, user+`\.fd-sec`, ""); ok {
		t.Error("vd register wrote the .fd-sec type")
	}
	if _, ok := regString(t, user+`\*\shell\FileDO`, "MUIVerb"); ok {
		t.Error("vd register wrote the File DO.. group")
	}
	for _, want := range []string{".fdd", vdProgID, "Mount with FileDO", "Mount read-only", "Unmount", "vd unregister"} {
		if !strings.Contains(out, want) {
			t.Errorf("the report never mentions %q\n%s", want, out)
		}
	}
	// Observability: the change is logged like every other state change.
	logb, err := os.ReadFile(filepath.Join(wd, "vdisk.log"))
	if err != nil || !strings.Contains(string(logb), "register ") || !strings.Contains(string(logb), vdProgID) {
		t.Errorf("the registration is not in vdisk.log (%v): %q", err, logb)
	}
}

// Without the icons folder the type still has an icon - the product's own, not none.
func TestVdRegister_FallsBackToTheProductIcon(t *testing.T) {
	inst := vdTestInstallDir(t, false)
	seam, user, _ := regSeam(t)
	if out, code := runRegExe(t, filepath.Join(inst, "filedo.exe"), t.TempDir(), seam, "vd", "register"); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	want := filepath.Join(vdRegisteredDir(t, user), "FileDO.ico")
	if got := mustRegString(t, user+`\`+vdProgID+`\DefaultIcon`, ""); !strings.EqualFold(got, want) {
		t.Errorf("the type shows %q, want the product icon %q", got, want)
	}
}

// Unregister removes exactly what register wrote and is safe to run twice; the
// log says so.
func TestVdUnregister_RemovesWhatItWrote(t *testing.T) {
	inst := vdTestInstallDir(t, true)
	exe := filepath.Join(inst, "filedo.exe")
	wd := t.TempDir()
	seam, user, _ := regSeam(t)
	if out, code := runRegExe(t, exe, wd, seam, "vd", "register"); code != 0 {
		t.Fatalf("register exit %d\n%s", code, out)
	}
	out, code := runRegExe(t, exe, wd, seam, "vdisk", "unregister")
	if code != 0 {
		t.Fatalf("unregister exit %d, want 0\n%s", code, out)
	}
	for _, key := range []string{`.fdd`, `.fdd\OpenWithProgids`, vdProgID} {
		k, err := registry.OpenKey(registry.CURRENT_USER, user+`\`+key, registry.QUERY_VALUE)
		if err == nil {
			k.Close()
			t.Errorf("%s survived the unregister", key)
		}
	}
	logb, _ := os.ReadFile(filepath.Join(wd, "vdisk.log"))
	if !strings.Contains(string(logb), "unregister ") {
		t.Errorf("the removal is not in vdisk.log: %q", logb)
	}
	out, code = runRegExe(t, exe, wd, seam, "vd", "unregister")
	if code != 0 || !strings.Contains(out, "Nothing to remove") {
		t.Errorf("a second unregister: exit %d\n%s", code, out)
	}
}

// Whoever owned .fdd before gets it back, and another program's "Open with" entry
// under it stays.
func TestVdUnregister_HandsTheExtensionBack(t *testing.T) {
	inst := vdTestInstallDir(t, true)
	exe := filepath.Join(inst, "filedo.exe")
	seam, user, _ := regSeam(t)
	for _, v := range []struct{ key, name string }{{`.fdd`, ""}, {`.fdd\OpenWithProgids`, "Other.App"}} {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, user+`\`+v.key, registry.SET_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		val := "Some.Other.Type"
		if v.name != "" {
			val = ""
		}
		if err := k.SetStringValue(v.name, val); err != nil {
			t.Fatal(err)
		}
		k.Close()
	}
	if out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "register"); code != 0 {
		t.Fatalf("register exit %d\n%s", code, out)
	}
	if got := mustRegString(t, user+`\.fdd`, ""); got != vdProgID {
		t.Fatalf(".fdd points at %q after register", got)
	}
	out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "unregister")
	if code != 0 {
		t.Fatalf("unregister exit %d\n%s", code, out)
	}
	if got := mustRegString(t, user+`\.fdd`, ""); got != "Some.Other.Type" {
		t.Errorf(".fdd came back as %q, want the previous owner", got)
	}
	if !strings.Contains(out, "handed back to Some.Other.Type") {
		t.Errorf("the hand-back is not reported\n%s", out)
	}
	if _, ok := regString(t, user+`\.fdd\OpenWithProgids`, "Other.App"); !ok {
		t.Error("another program's Open with entry was removed")
	}
	if _, ok := regString(t, user+`\.fdd\OpenWithProgids`, vdProgID); ok {
		t.Error("FileDO's Open with entry survived the unregister")
	}
	if _, ok := regString(t, user+`\.fdd`, fdsecPreviousProgIDValue); ok {
		t.Error("the previous-owner note survived the unregister")
	}
}

// A type without FileDO's mark is somebody else's: refused, named, left alone.
func TestVdUnregister_RefusesAForeignOwner(t *testing.T) {
	seam, user, _ := regSeam(t)
	k, _, err := registry.CreateKey(registry.CURRENT_USER, user+`\`+vdProgIDMountCommand, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if err := k.SetStringValue("", `"C:\Windows\notepad.exe" "%1"`); err != nil {
		t.Fatal(err)
	}
	k.Close()
	out, code := runReg(t, t.TempDir(), seam, "vd", "unregister")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (usage): a foreign registration\n%s", code, out)
	}
	if !strings.Contains(strings.ToLower(out), "notepad") || !strings.Contains(out, "no FileDO registration mark") {
		t.Errorf("the refusal does not name what it found\n%s", out)
	}
	if _, ok := regString(t, user+`\`+vdProgIDMountCommand, ""); !ok {
		t.Error("the foreign command was removed anyway")
	}
}

// HKLM wins: a per-user register stands down when the machine-wide copy is there, and
// a machine-wide register reports a per-user copy it cannot see past.
func TestVdRegister_PerUserStandsDownForTheMachineCopy(t *testing.T) {
	inst := vdTestInstallDir(t, true)
	exe := filepath.Join(inst, "filedo.exe")
	seam, user, machine := regSeam(t)
	if out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "register", "-all-users"); code != 0 {
		t.Fatalf("machine register exit %d\n%s", code, out)
	}
	if _, ok := regString(t, machine+`\`+vdProgIDMountCommand, ""); !ok {
		t.Fatal("the machine-wide registration was not written")
	}
	out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "register")
	if code != 0 || !strings.Contains(out, "Already registered for all users") {
		t.Errorf("per-user register over a machine copy: exit %d\n%s", code, out)
	}
	if _, ok := regString(t, user+`\`+vdProgIDMountCommand, ""); ok {
		t.Error("a per-user copy was written on top of the machine-wide one")
	}
	if out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "unregister", "-all-users"); code != 0 {
		t.Fatalf("machine unregister exit %d\n%s", code, out)
	}
	if out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "register"); code != 0 {
		t.Fatalf("per-user register exit %d\n%s", code, out)
	}
	out, code = runRegExe(t, exe, t.TempDir(), seam, "vd", "register", "-all-users")
	if code != 0 || !strings.Contains(out, "per-user .fdd registration") {
		t.Errorf("register -all-users does not report the per-user copy that wins: exit %d\n%s", code, out)
	}
}

// A FileDO package installed for this user already holds .fdd: the classic per-user
// copy yields; a machine-wide one is written and says this account sees both.
func TestVdRegister_StandsDownForThePackage(t *testing.T) {
	inst := vdTestInstallDir(t, true)
	exe := filepath.Join(inst, "filedo.exe")
	seam, user, machine := regSeam(t)
	t.Setenv(fdsecPackagedMenuEnv, "1")
	out, code := runRegExe(t, exe, t.TempDir(), seam, "vd", "register")
	if code != 0 || !strings.Contains(out, "FileDO package is installed") || !strings.Contains(out, "nothing written") {
		t.Errorf("per-user register beside the package: exit %d\n%s", code, out)
	}
	if _, ok := regString(t, user+`\.fdd`, ""); ok {
		t.Error("a per-user .fdd was written beside the package")
	}
	out, code = runRegExe(t, exe, t.TempDir(), seam, "vd", "register", "-all-users")
	if code != 0 || !strings.Contains(out, "from both") {
		t.Errorf("machine register beside the package: exit %d\n%s", code, out)
	}
	if _, ok := regString(t, machine+`\`+vdProgIDMountCommand, ""); !ok {
		t.Error("the machine-wide registration was not written")
	}
}

// Inside the package: class 6, one sentence, nothing written - register and unregister.
func TestVdRegister_RefusesInThePackagedBuild(t *testing.T) {
	inst := vdTestInstallDir(t, true)
	seam, user, _ := regSeam(t)
	t.Setenv(fdsecAsPackagedEnv, "1")
	for _, sub := range []string{"register", "unregister"} {
		out, code := runRegExe(t, filepath.Join(inst, "filedo.exe"), t.TempDir(), seam, "vd", sub)
		if code != 6 {
			t.Errorf("packaged vd %s exit %d, want 6 (unsupported)\n%s", sub, code, out)
		}
		if !strings.Contains(out, "Microsoft Store build") || !strings.Contains(out, "setup, winget and zip") {
			t.Errorf("packaged vd %s does not say why, or where .fdd comes from\n%s", sub, out)
		}
	}
	if _, ok := regString(t, user+`\.fdd`, ""); ok {
		t.Error("the packaged build wrote a registration")
	}
}

// Every verb opens the window: without filedo_win.exe beside the exe the registration
// would be a double-click that fails, so it is refused (class 6) and nothing is written.
func TestVdRegister_RefusesWithoutTheWindow(t *testing.T) {
	if _, err := os.Stat(filepath.Join(filepath.Dir(filedoExe), "filedo_win.exe")); err == nil {
		t.Skip("the suite's build folder has a filedo_win.exe")
	}
	seam, user, _ := regSeam(t)
	out, code := runReg(t, t.TempDir(), seam, "vd", "register")
	if code != 6 || !strings.Contains(out, "filedo_win.exe is not beside") {
		t.Errorf("exit %d, want 6 with the reason\n%s", code, out)
	}
	if _, ok := regString(t, user+`\.fdd`, ""); ok {
		t.Error("a registration without the window was written")
	}
}

// A stray word is refused as usage and never quoted back by the refusal: it may be a
// password, and the error text reaches history.json. (The line's own record is the
// redaction's business: fdsec_redact.go keeps the word after a vd verb as its subject.)
func TestVdRegister_NeverQuotesAStrayWord(t *testing.T) {
	seam, _, _ := regSeam(t)
	wd := t.TempDir()
	for _, word := range []string{"Sw0rdfish77", "p:Sw0rdfish77"} {
		out, code := runReg(t, wd, seam, "vd", "register", word)
		if code != 2 {
			t.Errorf("vd register %s: exit %d, want 2\n%s", "<word>", code, out)
		}
		if strings.Contains(out, "Sw0rdfish77") {
			t.Errorf("the stray word was quoted back\n%s", out)
		}
	}
	b, err := os.ReadFile(filepath.Join(wd, "history.json"))
	if err != nil || !strings.Contains(string(b), "vd register takes no word but -all-users") {
		t.Fatalf("history.json does not carry the refusal (%v)\n%s", err, b)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, `"error"`) && strings.Contains(line, "Sw0rdfish77") {
			t.Errorf("the refusal quoted the stray word into history.json: %s", line)
		}
	}
}

// The type's icon is drawn and kept in the repository like the sibling's (SP-0016 T8),
// and the GUI writes it: MenuIcons.Ids names the meaning, so --write-menu-icons makes
// it and the --selftest menu-icon: rows check it.
func TestMenuIcons_TheDiskContainerIconIsInTheRepository(t *testing.T) {
	root := repoRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "assets", "menu-icons", vdContainerIcon+".ico"))
	if err != nil {
		t.Fatalf("assets\\menu-icons\\%s.ico: %v - run filedo_win.exe --write-menu-icons assets\\menu-icons", vdContainerIcon, err)
	}
	if len(body) < 6 || body[0] != 0 || body[1] != 0 || body[2] != 1 || body[3] != 0 {
		t.Errorf("assets\\menu-icons\\%s.ico is not an icon file", vdContainerIcon)
	}
	if src := readSurface(t, root, filepath.Join("filedo_win_vb", "MenuIcons.vb")); !strings.Contains(src, `"`+vdContainerIcon+`"`) {
		t.Errorf("MenuIcons.Ids does not name %s, so --write-menu-icons and --selftest skip it", vdContainerIcon)
	}
	if wxs := readSurface(t, root, filepath.Join("packaging", "wix", "FileDO.wxs")); !strings.Contains(wxs, `$(var.StageDir)\icons\`+vdContainerIcon+`.ico`) {
		t.Errorf("the MSI does not install icons\\%s.ico, which its DefaultIcon names", vdContainerIcon)
	}
}
