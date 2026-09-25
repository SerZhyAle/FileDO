package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedirectSystemDriveForms is CLI-18: every spelling of the system
// volume's root is the system drive; a folder below it and another volume are
// not.
func TestRedirectSystemDriveForms(t *testing.T) {
	sys := os.Getenv("SystemDrive")
	if sys == "" {
		sys = "C:"
	}
	roots := []string{
		sys, sys + `\`, strings.ToLower(sys) + `\`, sys + `/`,
		`\\?\` + sys + `\`, `\\.\` + sys + `\`,
	}
	for _, r := range roots {
		if !isSystemVolumeRoot(r) {
			t.Errorf("%q must be recognised as the system drive", r)
		}
	}
	for _, notRoot := range []string{os.Getenv("SystemRoot"), t.TempDir()} {
		if notRoot == "" {
			continue
		}
		if isSystemVolumeRoot(notRoot) {
			t.Errorf("%q is a folder, not the system drive's root", notRoot)
		}
	}

	// A subst letter onto the root is the same volume root.
	if free := freeDriveLetter(); free != "" {
		if out, err := exec.Command("subst", free, sys+`\`).CombinedOutput(); err == nil {
			defer exec.Command("subst", free, "/d").Run()
			if !isSystemVolumeRoot(free) {
				t.Errorf("subst %s onto %s\\ must be the system drive", free, sys)
			}
		} else {
			t.Logf("subst unavailable (%v: %s); that row skipped", err, out)
		}
	}
}

// TestEffectiveTargetCleanOnSystemDrive is CLI-09: clean on the system drive
// looks in the redirect folder; clean elsewhere stays where it was pointed.
func TestEffectiveTargetCleanOnSystemDrive(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TEMP", tmp)
	t.Setenv("FILEDO_DISABLE_REDIRECT", "")
	sys := os.Getenv("SystemDrive")
	if sys == "" {
		sys = "C:"
	}
	want := filepath.Join(tmp, "FileDO_Operations")
	for _, op := range []string{"clean", "cln", "c"} {
		if got := effectiveTarget(op, sys); !strings.EqualFold(got, want) {
			t.Errorf("effectiveTarget(%q, %q) = %q, want %q", op, sys, got, want)
		}
	}
	folder := t.TempDir()
	if got := effectiveTarget("clean", folder); got != folder {
		t.Errorf("clean on a folder must stay there: got %q", got)
	}
	if got := effectiveTarget("info", sys); got != sys {
		t.Errorf("info is never redirected: got %q", got)
	}
}

// TestEffectiveTargetWriteOnSystemDriveRedirects: with the confirmation
// auto-answered, every spelling of the root is redirected for a write.
func TestEffectiveTargetWriteOnSystemDriveRedirects(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TEMP", tmp)
	t.Setenv("FILEDO_AUTO_CONFIRM", "1")
	t.Setenv("FILEDO_DISABLE_REDIRECT", "")
	sys := os.Getenv("SystemDrive")
	if sys == "" {
		sys = "C:"
	}
	want := filepath.Join(tmp, "FileDO_Operations")
	for _, spelling := range []string{sys, sys + `\`, strings.ToLower(sys) + `/`, `\\?\` + sys + `\`} {
		for _, op := range []string{"fill", "f", "test", "speed"} {
			if got := effectiveTarget(op, spelling); !strings.EqualFold(got, want) {
				t.Errorf("effectiveTarget(%q, %q) = %q, want the redirect %q", op, spelling, got, want)
			}
		}
	}
	folder := t.TempDir()
	if got := effectiveTarget("fill", folder); got != folder {
		t.Errorf("a folder the user chose is not redirected: got %q", got)
	}
}

func freeDriveLetter() string {
	for c := 'Z'; c >= 'M'; c-- {
		d := string(c) + ":"
		if _, err := os.Stat(d + `\`); err != nil {
			return d
		}
	}
	return ""
}
