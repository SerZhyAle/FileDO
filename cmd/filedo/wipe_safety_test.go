package main

// SP-0027 wipe tickets. The classifier rows (WIPE-01, WIPE-02) call the
// classifier only - nothing real is ever wiped; the black-box rows wipe
// folders under t.TempDir() and nothing else.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestClassifyWipeTargetRootSpellings(t *testing.T) {
	sysDrive := os.Getenv("SystemDrive")
	if sysDrive == "" {
		sysDrive = "C:"
	}
	rows := []string{
		`\\?\UNC\filedo-no-such-srv\share\`,
		`\\?\` + sysDrive + `\`,
		`\\.\` + sysDrive + `\`,
		`\\filedo-no-such-srv\share`,
		`//filedo-no-such-srv/share`,
		`D:`, `D:/`, `D:\.`, `D:\ `, `D:\..`,
		sysDrive + `\`,
	}
	// The volume's GUID name and its GLOBALROOT device path, from the system.
	buf := make([]uint16, 64)
	root, _ := windows.UTF16PtrFromString(sysDrive + `\`)
	if err := windows.GetVolumeNameForVolumeMountPoint(root, &buf[0], uint32(len(buf))); err == nil {
		rows = append(rows, windows.UTF16ToString(buf))
	} else {
		t.Logf("no volume GUID name for %s: %v", sysDrive, err)
	}
	dev := make([]uint16, 512)
	name, _ := windows.UTF16PtrFromString(sysDrive)
	if n, err := windows.QueryDosDevice(name, &dev[0], uint32(len(dev))); err == nil && n > 0 {
		rows = append(rows, `\\?\GLOBALROOT`+windows.UTF16ToString(dev)+`\`)
	} else {
		t.Logf("QueryDosDevice(%s): %v", sysDrive, err)
	}
	rows = append(rows, `\\?\GLOBALROOT\Device\HarddiskVolume7\`)

	for _, p := range rows {
		if dangerous, reason := classifyWipeTarget(p); !dangerous {
			t.Errorf("%q is not classified dangerous (WIPE-01)", p)
		} else {
			t.Logf("%-50q %s", p, reason)
		}
	}

	plain := filepath.Join(t.TempDir(), "plain")
	os.MkdirAll(plain, 0o755)
	if dangerous, reason := classifyWipeTarget(plain); dangerous {
		t.Errorf("an ordinary folder is classified dangerous: %s", reason)
	}
}

func TestClassifyWipeTargetProtectedFolders(t *testing.T) {
	temp := os.Getenv("TEMP")
	profile := os.Getenv("USERPROFILE")
	sysRoot := os.Getenv("SystemRoot")
	if temp == "" || profile == "" || sysRoot == "" {
		t.Skip("TEMP, USERPROFILE or SystemRoot is not set")
	}
	exe, _ := os.Executable()
	rows := []string{
		temp,
		`\\?\` + temp,
		`\\.\` + temp,
		filepath.Dir(temp), // ...\AppData\Local, a parent of TEMP
		profile,
		filepath.Dir(profile), // the profiles root
		filepath.Join(sysRoot, "Temp"),
		sysRoot,
		filepath.Join(sysRoot, "System32"),
		os.Getenv("ProgramFiles"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "FileDO"),
		filepath.Dir(exe),
	}
	// The 8.3 spelling of TEMP, when the volume keeps short names.
	long, _ := windows.UTF16PtrFromString(temp)
	short := make([]uint16, 512)
	if n, err := windows.GetShortPathName(long, &short[0], uint32(len(short))); err == nil && n > 0 {
		if s := windows.UTF16ToString(short[:n]); !strings.EqualFold(s, temp) {
			rows = append(rows, s)
		} else {
			t.Logf("TEMP has no distinct 8.3 spelling here; row skipped")
		}
	}
	// The administrative share, when it is reachable.
	if len(temp) > 2 && temp[1] == ':' {
		admin := `\\localhost\` + temp[:1] + `$` + temp[2:]
		if _, err := os.Stat(admin); err == nil {
			rows = append(rows, admin)
		} else {
			t.Logf("%s is not reachable (%v); row skipped", admin, err)
		}
	}

	for _, p := range rows {
		if p == "" {
			continue
		}
		if dangerous, reason := classifyWipeTarget(p); !dangerous {
			t.Errorf("%q is not classified dangerous (WIPE-02)", p)
		} else {
			t.Logf("%-70q %s", p, reason)
		}
	}
}

func TestWipeDisplayPathAndDeviceRoot(t *testing.T) {
	for in, want := range map[string]string{`D:`: `D:\`, `d:/`: `D:\`, `D:\`: `D:\`, `D:\sub`: `D:\sub`} {
		if got := deviceRootPath(in); got != want {
			t.Errorf("deviceRootPath(%q) = %q, want %q", in, got, want)
		}
	}
	if dangerous, _ := classifyWipeTarget(deviceRootPath("D:")); !dangerous {
		t.Errorf("the device verb's D: is not a dangerous root")
	}

	dir := t.TempDir()
	sub := filepath.Join(dir, "Sub")
	os.MkdirAll(sub, 0o755)
	t.Chdir(dir)
	want, err := resolvedPath(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got := wipeDisplayPath("sub"); got != want || !filepath.IsAbs(got) {
		t.Errorf("wipeDisplayPath(\"sub\") = %q, want the canonical %q", got, want)
	}
	j := filepath.Join(dir, "Link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, sub).CombinedOutput(); err == nil {
		if got := wipeDisplayPath(j); got != want {
			t.Errorf("a junction is shown as %q, want the folder it names, %q", got, want)
		}
	} else {
		t.Logf("mklink /J unavailable (%v: %s)", err, out)
	}
}

func TestWipeRefusesReparsePoint(t *testing.T) {
	wd := t.TempDir()
	data := filepath.Join(wd, "data")
	writeFile(t, filepath.Join(data, "keep.txt"), []byte("keep"))
	link := filepath.Join(wd, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, data).CombinedOutput(); err != nil {
		t.Skipf("mklink /J unavailable (%v: %s)", err, out)
	}
	for _, target := range []string{link, link + `\`} {
		out, code := run(t, wd, "folder", target, "wipe", "--force")
		if code != 2 {
			t.Errorf("wiping a junction exited %d, want 2 (WIPE-03)\n%s", code, out)
		}
		if _, err := os.Lstat(link); err != nil || !hasReparsePoint(link) {
			t.Fatalf("the junction is gone or no longer a link: %v", err)
		}
		if !exists(filepath.Join(data, "keep.txt")) {
			t.Fatalf("the data behind the junction was deleted")
		}
	}
}

func icaclsOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("icacls", dir).CombinedOutput()
	if err != nil {
		t.Skipf("icacls unavailable: %v", err)
	}
	return string(out)
}

func TestWipeKeepsFolderACL(t *testing.T) {
	wd := t.TempDir()
	box := filepath.Join(wd, "box")
	writeFile(t, filepath.Join(box, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(box, "sub", "b.txt"), []byte("b"))
	if out, err := exec.Command("icacls", box, "/grant", "*S-1-1-0:(OI)(CI)(RX)").CombinedOutput(); err != nil {
		t.Skipf("icacls /grant unavailable (%v: %s)", err, out)
	}
	before := icaclsOf(t, box)

	out, code := run(t, wd, "folder", box, "wipe", "--force")
	if code != 0 {
		t.Fatalf("wipe exited %d\n%s", code, out)
	}
	entries, err := os.ReadDir(box)
	if err != nil {
		t.Fatalf("the folder itself is gone: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("%d entries survived the wipe", len(entries))
	}
	if after := icaclsOf(t, box); after != before {
		t.Errorf("the folder's access list changed (WIPE-04)\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestWipeLockedFileNotProven(t *testing.T) {
	wd := t.TempDir()
	box := filepath.Join(wd, "box")
	writeFile(t, filepath.Join(box, "a.txt"), []byte("a"))
	writeFile(t, filepath.Join(box, "sub", "b.txt"), []byte("b"))
	locked := filepath.Join(box, "locked.txt")
	writeFile(t, locked, []byte("in use"))
	release := holdExclusively(t, locked)
	defer release()

	out, code := run(t, wd, "folder", box, "wipe", "--force")
	if code != 2 {
		t.Errorf("a wipe that left a locked file exited %d, want 2 (WIPE-06)\n%s", code, out)
	}
	if exists(filepath.Join(box, "a.txt")) || exists(filepath.Join(box, "sub")) {
		t.Errorf("the entries that could be deleted were not")
	}
	if !exists(locked) {
		t.Errorf("the locked file is gone - the test did not hold it")
	}
}
