package fsx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SP-0037 AUD-06-F1: "inside" and "overlaps" are decided on the volume and the
// path below its root, not on how the two paths happen to be spelled. The
// package comment has always promised that `\\localhost\D$` is resolved; a
// final path of `\\localhost\C$\..` against `C:\..` broke the promise.

func TestLiesWithin_DecidesOnVolumeAndThePathBelowIt(t *testing.T) {
	local := func(final string) Identity {
		return Identity{Final: final, Volume: `C:\`, VolSerial: 0xC0FFEE}
	}
	admin := func(final string) Identity {
		return Identity{Final: final, Volume: `\\localhost\C$\`, VolSerial: 0xC0FFEE}
	}
	other := func(final string) Identity {
		return Identity{Final: final, Volume: `D:\`, VolSerial: 0xD00D}
	}
	cases := []struct {
		name          string
		child, parent Identity
		want          bool
	}{
		{"the same folder, two spellings", admin(`\\localhost\C$\Data`), local(`C:\Data`), true},
		{"below it, the child under an administrative share", admin(`\\localhost\C$\Data\backup\deeper`), local(`C:\Data`), true},
		{"below it, the parent under an administrative share", local(`C:\Data\backup`), admin(`\\localhost\C$\Data`), true},
		{"case does not matter", admin(`\\LOCALHOST\c$\DATA\Sub`), local(`C:\data`), true},
		{"a sibling that shares a prefix", admin(`\\localhost\C$\Data2`), local(`C:\Data`), false},
		{"the parent below the child", local(`C:\Data`), admin(`\\localhost\C$\Data\sub`), false},
		{"another volume with the same path", other(`D:\Data\sub`), local(`C:\Data`), false},
		{"below a volume root", admin(`\\localhost\C$\Anything\at\all`), local(`C:\`), true},
		{"an unknown volume is never matched", Identity{Final: `C:\Data\x`}, Identity{Final: `D:\Data`}, false},
	}
	for _, c := range cases {
		if got := c.child.LiesWithin(c.parent); got != c.want {
			t.Errorf("%s: %q LiesWithin %q = %v, want %v", c.name, c.child.Final, c.parent.Final, got, c.want)
		}
	}
}

func TestRelToVolume_IsTheSameUnderEverySpelling(t *testing.T) {
	a := Identity{Final: `C:\Users\me\x`, Volume: `C:\`}.RelToVolume()
	b := Identity{Final: `\\localhost\C$\Users\me\x`, Volume: `\\localhost\C$\`}.RelToVolume()
	if a != `\Users\me\x` || b != a {
		t.Errorf("RelToVolume = %q and %q, want both %q", a, b, `\Users\me\x`)
	}
	if root := (Identity{Final: `C:\`, Volume: `C:\`}).RelToVolume(); root != `\` {
		t.Errorf("the root's RelToVolume = %q, want a lone separator", root)
	}
}

// adminShareOf returns dir spelled through the local administrative share of
// its drive, or skips the test when that share is not reachable here (it needs
// an administrative token, or an administrator-made share).
func adminShareOf(t *testing.T, dir string) string {
	t.Helper()
	vol := filepath.VolumeName(dir)
	if len(vol) != 2 || vol[1] != ':' {
		t.Skipf("%s is not on a lettered drive", dir)
	}
	share := `\\localhost\` + strings.ToUpper(vol[:1]) + `$` + dir[len(vol):]
	if _, err := os.Stat(share); err != nil {
		t.Skipf("the administrative share %s is not reachable: %v", share, err)
	}
	return share
}

func TestWithinAndOverlap_SeeThroughAnAdministrativeShare(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "existing"), 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	share := adminShareOf(t, dir)

	nestedExisting := filepath.Join(share, "src", "existing")
	nestedNew := filepath.Join(share, "src", "new", "deeper")
	sibling := filepath.Join(share, "other")

	for _, target := range []string{nestedExisting, nestedNew} {
		if in, err := Within(target, src); err != nil || !in {
			t.Errorf("Within(%q, %q) = %v, %v; want true, nil", target, src, in, err)
		}
		if in, err := Within(src, target); err != nil || in {
			t.Errorf("Within(%q, %q) = %v, %v; want false, nil (the parent is not inside the child)", src, target, in, err)
		}
		if ov, err := Overlap(src, target); err != nil || !ov {
			t.Errorf("Overlap(%q, %q) = %v, %v; want true, nil", src, target, ov, err)
		}
		if ov, err := Overlap(target, src); err != nil || !ov {
			t.Errorf("Overlap(%q, %q) = %v, %v; want true, nil", target, src, ov, err)
		}
	}
	if ov, err := Overlap(src, sibling); err != nil || ov {
		t.Errorf("Overlap(%q, %q) = %v, %v; want false, nil (a sibling folder)", src, sibling, ov, err)
	}
	if in, err := Within(sibling, src); err != nil || in {
		t.Errorf("Within(%q, %q) = %v, %v; want false, nil", sibling, src, in, err)
	}
}
