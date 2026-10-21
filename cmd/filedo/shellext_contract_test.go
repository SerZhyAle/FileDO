package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

// The packaged first-level Explorer command (SP-0020) is a third codebase,
// shellext/FileDOShell.cpp, and it must run exactly the command lines the
// classic registration writes (SP-0020 section 3.1). These tests read its
// source, the way shell_contract_test.go reads the window program's, and hold
// it to fdsecMenuItems - the one table both mechanisms answer to.

var shellExtRow = regexp.MustCompile(`\{L"([^"]*)", L"([^"]*)", L"((?:[^"\\]|\\.)*)", (true|false), L"([^"]*)"\}`)

// cppUnescape undoes the only escapes the table uses: \" and \\.
func cppUnescape(s string) string {
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(s)
}

func TestShellExtMenuMatchesClassic(t *testing.T) {
	root := repoRoot(t)
	src := readSurface(t, root, filepath.Join("shellext", "FileDOShell.cpp"))
	begin := strings.Index(src, "// BEGIN MENU TABLE")
	end := strings.Index(src, "// END MENU TABLE")
	if begin < 0 || end < begin {
		t.Fatal("FileDOShell.cpp has no BEGIN/END MENU TABLE markers around its menu")
	}
	rows := shellExtRow.FindAllStringSubmatch(src[begin:end], -1)
	if len(rows) != len(fdsecMenuItems) {
		t.Fatalf("FileDOShell.cpp has %d menu rows, fdsecMenuItems has %d", len(rows), len(fdsecMenuItems))
	}
	for i, it := range fdsecMenuItems {
		r := rows[i]
		got := fdsecMenuItem{key: r[1], label: r[2], args: cppUnescape(r[3]), separator: r[4] == "true", icon: r[5]}
		if got != it {
			t.Errorf("row %d: FileDOShell.cpp has %+v, the classic registration has %+v", i, got, it)
		}
	}

	// The suffix every command line ends in, and the group's label.
	wantSuffix := " " + fdsecPauseFlag + " " + fdsecNoHistoryFlag
	if !strings.Contains(src, `kSuffix[] = L"`+wantSuffix+`";`) {
		t.Errorf("FileDOShell.cpp's kSuffix is not %q", wantSuffix)
	}
	if !strings.Contains(src, `kGroupLabel[] = L"`+fdsecMenuGroupLabel+`";`) {
		t.Errorf("FileDOShell.cpp's kGroupLabel is not %q", fdsecMenuGroupLabel)
	}
	// The handler runs the package's own CLI; the classic entry runs the
	// registering exe, which is filedo.exe.
	if !strings.Contains(src, `kExeName[] = L"filedo.exe";`) {
		t.Error("FileDOShell.cpp does not launch filedo.exe")
	}
}

// One CLSID, three places: the verb and the COM class in the manifest
// template, and the class the DLL answers for. build-msix.ps1 checks the
// packed manifest too; this catches the drift before a package is built.
func TestShellExtClsidAgrees(t *testing.T) {
	root := repoRoot(t)
	src := readSurface(t, root, filepath.Join("shellext", "FileDOShell.cpp"))
	manifest := readSurface(t, root, filepath.Join("msix", "AppxManifest.xml"))

	m := regexp.MustCompile(`//\s*\{([0-9A-Fa-f-]{36})\}\s*\r?\n\s*const CLSID CLSID_FileDOCommand = \{0x([0-9a-f]{8}), 0x([0-9a-f]{4}), 0x([0-9a-f]{4}), \{0x([0-9a-f]{2}), 0x([0-9a-f]{2}), 0x([0-9a-f]{2}), 0x([0-9a-f]{2}), 0x([0-9a-f]{2}), 0x([0-9a-f]{2}), 0x([0-9a-f]{2}), 0x([0-9a-f]{2})\}\};`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("FileDOShell.cpp: CLSID_FileDOCommand and its {GUID} comment are not in the expected shape")
	}
	comment := strings.ToUpper(m[1])
	fromBytes := strings.ToUpper(m[2] + "-" + m[3] + "-" + m[4] + "-" + m[5] + m[6] + "-" + m[7] + m[8] + m[9] + m[10] + m[11] + m[12])
	if comment != fromBytes {
		t.Fatalf("FileDOShell.cpp: the CLSID comment says %s but the initializer is %s", comment, fromBytes)
	}
	verb := regexp.MustCompile(`<desktop5:Verb Id="FileDO" Clsid="([0-9A-Fa-f-]{36})"`).FindStringSubmatch(manifest)
	class := regexp.MustCompile(`<com:Class Id="([0-9A-Fa-f-]{36})" Path="FileDOShell.dll"`).FindStringSubmatch(manifest)
	if verb == nil || class == nil {
		t.Fatal("msix/AppxManifest.xml does not declare the FileDO verb and its FileDOShell.dll class")
	}
	for what, got := range map[string]string{"verb": verb[1], "COM class": class[1]} {
		if strings.ToUpper(got) != comment {
			t.Errorf("manifest %s CLSID %s differs from FileDOShell.cpp's %s", what, got, comment)
		}
	}
}

// packagePublisherHash is Windows' publisher id: the first 8 bytes of the
// SHA-256 of the UTF-16LE publisher, read as 65 bits (a zero bit appended)
// and written as 13 base-32 digits.
func packagePublisherHash(publisher string) string {
	u := utf16.Encode([]rune(publisher))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	sum := sha256.Sum256(b)
	v := binary.BigEndian.Uint64(sum[:8])
	const alphabet = "0123456789abcdefghjkmnpqrstvwxyz"
	out := make([]byte, 13)
	for i := 0; i < 13; i++ {
		// Digit i is bits [60-5i, 65-5i) of v<<1, which is v shifted right
		// by 59-5i; only the last digit reaches the appended zero bit.
		shift := 59 - 5*i
		var d uint64
		if shift >= 0 {
			d = (v >> uint(shift)) & 31
		} else {
			d = (v << uint(-shift)) & 31
		}
		out[i] = alphabet[d]
	}
	return string(out)
}

// The families `fdsec register` checks for are derived, not remembered: the
// Store one from msix/identity.json, the test one from build-msix.ps1's fixed
// test identity. A family that drifted from its identity would make the
// stand-down look for a package that can never be installed.
func TestPackagedMenuFamiliesMatchTheIdentities(t *testing.T) {
	root := repoRoot(t)
	var id struct{ IdentityName, Publisher, PackageFamilyName string }
	if err := json.Unmarshal([]byte(readSurface(t, root, filepath.Join("msix", "identity.json"))), &id); err != nil {
		t.Fatalf("msix/identity.json: %v", err)
	}
	store := id.IdentityName + "_" + packagePublisherHash(id.Publisher)
	if store != id.PackageFamilyName {
		t.Fatalf("the publisher hash does not reproduce identity.json's own family (%s vs %s) - the hash is wrong, not the identity", store, id.PackageFamilyName)
	}

	script := readSurface(t, root, filepath.Join("msix", "build-msix.ps1"))
	name := regexp.MustCompile(`\$TestIdentity\s*=\s*"([^"]+)"`).FindStringSubmatch(script)
	pub := regexp.MustCompile(`\$TestPublisher\s*=\s*"([^"]+)"`).FindStringSubmatch(script)
	if name == nil || pub == nil {
		t.Fatal("build-msix.ps1 no longer declares $TestIdentity / $TestPublisher")
	}
	test := name[1] + "_" + packagePublisherHash(pub[1])

	want := []string{store, test}
	if strings.Join(fdsecPackagedMenuFamilies, ",") != strings.Join(want, ",") {
		t.Errorf("fdsecPackagedMenuFamilies is %v, the identities give %v", fdsecPackagedMenuFamilies, want)
	}
}
