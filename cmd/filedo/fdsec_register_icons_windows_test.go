//go:build windows

package main

// SP-0016 T8: every entry of the "File DO.." group and the .fd-sec document
// type show their ICON-SET meaning's icon, never the product mark (ICON-SET
// rule 7); the group keeps the mark. The icons are icons\<id>.ico beside the
// exe, drawn by `filedo_win.exe --write-menu-icons` into assets\menu-icons.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Every meaning the writers name has its drawn icon in the repository, so no
// channel can ship a path to a file that was never made.
func TestMenuIcons_EveryMeaningHasItsIconInTheRepository(t *testing.T) {
	root := repoRoot(t)
	ids := map[string]bool{fdsecSecretFileIcon: true}
	for _, it := range fdsecMenuItems {
		if it.icon == "" {
			t.Errorf("%s names no ICON-SET meaning", it.key)
			continue
		}
		ids[it.icon] = true
	}
	for id := range ids {
		body, err := os.ReadFile(filepath.Join(root, "assets", "menu-icons", id+".ico"))
		if err != nil {
			t.Errorf("assets\\menu-icons\\%s.ico: %v - run filedo_win.exe --write-menu-icons assets\\menu-icons", id, err)
			continue
		}
		// ICONDIR: reserved 0, type 1 (icon).
		if len(body) < 6 || body[0] != 0 || body[1] != 0 || body[2] != 1 || body[3] != 0 {
			t.Errorf("assets\\menu-icons\\%s.ico is not an icon file", id)
		}
	}
}

func TestFdsecRegister_EntriesShowTheirMeaningNotTheMark(t *testing.T) {
	installDir := filepath.Join(t.TempDir(), "FileDO")
	if err := os.MkdirAll(filepath.Join(installDir, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filedoExe)
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(installDir, "filedo.exe")
	if err := os.WriteFile(exe, body, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"action.secure", "action.unsecure", "action.wipe", "action.verify", "app.info", fdsecSecretFileIcon} {
		if err := os.WriteFile(filepath.Join(installDir, "icons", id+".ico"), []byte("icon"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	seam, userClasses, _ := regSeam(t)
	register := func(bin string) {
		t.Helper()
		cmd := exec.Command(bin, "fdsec", "register")
		cmd.Dir = filepath.Dir(bin)
		cmd.Env = append(os.Environ(),
			"FILEDO_FDSEC_NO_LAUNCH=1",
			"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(installDir),
			fdsecRegRootEnv+"="+seam,
			fdsecPackagedMenuEnv+"=0",
		)
		cmd.Stdin = strings.NewReader("")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fdsec register: %v\n%s", err, out)
		}
	}
	register(exe)

	// The folder the exe resolved itself to, read from the mark it left.
	mark := mustRegString(t, userClasses+`\`+fdsecProgID, fdsecOwnerValue)
	dir := filepath.Dir(mark)
	group := userClasses + `\*\shell\` + fdsecMenuGroupKey
	for _, it := range fdsecMenuItems {
		want := filepath.Join(dir, "icons", it.icon+".ico")
		if got := mustRegString(t, group+`\shell\`+it.key, "Icon"); !strings.EqualFold(got, want) {
			t.Errorf("%s shows %q, want its meaning %q", it.key, got, want)
		}
	}
	wantDoc := filepath.Join(dir, "icons", fdsecSecretFileIcon+".ico")
	if got := mustRegString(t, userClasses+`\`+fdsecProgID+`\DefaultIcon`, ""); !strings.EqualFold(got, wantDoc) {
		t.Errorf("the document type shows %q, want %q", got, wantDoc)
	}
	if got := mustRegString(t, group, "Icon"); strings.Contains(strings.ToLower(got), `\icons\`) {
		t.Errorf("the group shows a meaning icon %q; it carries the product mark", got)
	}

	// Registered again from an exe with no icons folder: no entry keeps an
	// Icon, rather than the old path or the mark standing in for the meaning.
	register(filedoExe)
	for _, it := range fdsecMenuItems {
		if v, ok := regString(t, group+`\shell\`+it.key, "Icon"); ok {
			t.Errorf("%s still shows %q after a registration without icons", it.key, v)
		}
	}
	if _, ok := regString(t, userClasses+`\`+fdsecProgID+`\DefaultIcon`, ""); !ok {
		t.Error("the document type has no icon at all without the icons folder")
	}
}
