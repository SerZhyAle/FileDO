//go:build windows

package main

// SP-0016 T8: every entry of the "File DO.." group and the .fd-sec document
// type show their ICON-SET meaning's icon, never the product mark (ICON-SET
// rule 7); the group keeps the mark. The icons are icons\<id>.ico beside the
// exe, drawn by `filedo_win.exe --write-menu-icons` into assets\menu-icons.

import (
	"bytes"
	"encoding/binary"
	"image/png"
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

// The Disk Manager's own icon (owner decision 2026-10-02, ICON-RENDER section 3 rule 6): the
// content.disk-container glyph on a rounded accent plate, written by `filedo_win.exe
// --write-menu-icons` beside the mono ones and used by the window, its buttons and the MSI's
// Start menu shortcut. It must not be a copy of the generic mono icon the .fdd type keeps, and
// every size must be a plate: an opaque centre and a transparent corner.
func TestMenuIcons_TheDiskManagerIconIsAPlateNotTheGenericGlyph(t *testing.T) {
	root := repoRoot(t)
	const plated, generic = "app.disk-manager", "content.disk-container"
	body, err := os.ReadFile(filepath.Join(root, "assets", "menu-icons", plated+".ico"))
	if err != nil {
		t.Fatalf("assets\\menu-icons\\%s.ico: %v - run filedo_win.exe --write-menu-icons assets\\menu-icons", plated, err)
	}
	mono, err := os.ReadFile(filepath.Join(root, "assets", "menu-icons", generic+".ico"))
	if err != nil {
		t.Fatalf("assets\\menu-icons\\%s.ico: %v", generic, err)
	}
	if bytes.Equal(body, mono) {
		t.Fatalf("%s.ico is a byte copy of %s.ico: the product's picture would be the generic file-type glyph", plated, generic)
	}
	if len(body) < 6 || body[0] != 0 || body[1] != 0 || body[2] != 1 || body[3] != 0 {
		t.Fatalf("%s.ico is not an icon file", plated)
	}
	count := int(binary.LittleEndian.Uint16(body[4:6]))
	seen := map[int]bool{}
	for i := 0; i < count; i++ {
		e := body[6+16*i : 6+16*(i+1)]
		size := int(e[0])
		if size == 0 {
			size = 256
		}
		length := int(binary.LittleEndian.Uint32(e[8:12]))
		offset := int(binary.LittleEndian.Uint32(e[12:16]))
		if offset < 0 || length <= 0 || offset+length > len(body) {
			t.Errorf("%s.ico: the %d px entry points outside the file", plated, size)
			continue
		}
		seen[size] = true
		alpha := func(x, y int) (uint8, bool) { return 0, false }
		data := body[offset : offset+length]
		if len(data) > 8 && data[0] == 0x89 && data[1] == 'P' {
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Errorf("%s.ico: the %d px PNG does not decode: %v", plated, size, err)
				continue
			}
			alpha = func(x, y int) (uint8, bool) {
				_, _, _, a := img.At(img.Bounds().Min.X+x, img.Bounds().Min.Y+y).RGBA()
				return uint8(a >> 8), true
			}
		} else {
			// BITMAPINFOHEADER (40 bytes), then the colour rows bottom-up in BGRA.
			if len(data) < 40+size*size*4 || int(binary.LittleEndian.Uint32(data[0:4])) != 40 || binary.LittleEndian.Uint16(data[14:16]) != 32 {
				t.Errorf("%s.ico: the %d px entry is not a 32-bit DIB", plated, size)
				continue
			}
			alpha = func(x, y int) (uint8, bool) { return data[40+((size-1-y)*size+x)*4+3], true }
		}
		if a, _ := alpha(size/2, size/2); a != 255 {
			t.Errorf("%s.ico at %d px: the centre has alpha %d, want an opaque plate", plated, size, a)
		}
		if a, _ := alpha(0, 0); a != 0 {
			t.Errorf("%s.ico at %d px: the corner has alpha %d, want a transparent rounded corner", plated, size, a)
		}
	}
	for _, size := range []int{16, 24, 32, 48, 256} {
		if !seen[size] {
			t.Errorf("%s.ico has no %d px image", plated, size)
		}
	}
}
