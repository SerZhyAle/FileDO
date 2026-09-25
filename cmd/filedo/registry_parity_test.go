//go:build windows

package main

// The two classic registry writers, held to each other value for value (SP-0030 REL-17).
//
// packaging/wix/FileDO.wxs (the MSI, HKLM) and `filedo fdsec register` (fdsec_register*.go,
// for the channels where no installer runs) write one contract: the .fd-sec document type
// and the ten-entry "File DO.." group. Until this test only the open verb was compared
// (TestSurfaces_BothRegistryWritersOpenTheSameProgram); the other keys stayed equal by
// convention, and a drift between them is silent - a machine simply shows a different menu
// depending on how FileDO arrived.
//
// So the test does not compare source text. It parses every RegistryValue of the MSI's
// ShellIntegration component group, runs the real `fdsec register -all-users` into a scratch
// key (FILEDO_FDSEC_REG_ROOT, the seam the registration tests use), reads back everything
// that landed, and demands the same set in both directions: every key, value name, type,
// label and command line, with [INSTALLFOLDER] standing for the folder the exe runs from.

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

type parityWxsValue struct {
	Key   string `xml:"Key,attr"`
	Name  string `xml:"Name,attr"`
	Type  string `xml:"Type,attr"`
	Value string `xml:"Value,attr"`
}

type parityWxsKey struct {
	Root   string           `xml:"Root,attr"`
	Key    string           `xml:"Key,attr"`
	Values []parityWxsValue `xml:"RegistryValue"`
}

type parityWxs struct {
	Fragments []struct {
		Groups []struct {
			ID         string `xml:"Id,attr"`
			Components []struct {
				ID   string         `xml:"Id,attr"`
				Keys []parityWxsKey `xml:"RegistryKey"`
			} `xml:"Component"`
		} `xml:"ComponentGroup"`
	} `xml:"Fragment"`
}

// parityEntry is one registry value: its type as the MSI names it ("string" for REG_SZ,
// "integer" for REG_DWORD) and its data.
type parityEntry struct{ typ, data string }

// parityID is the comparison key: the key path below Classes and the value name, both
// case-insensitive in the registry, so compared that way; the data is compared exactly.
func parityID(keyPath, name string) string {
	if name == "" {
		name = "(Default)"
	}
	return strings.ToLower(keyPath) + " | " + strings.ToLower(name)
}

// wxsShellRegistry returns what the MSI's ShellIntegration group writes, below
// HKLM\Software\Classes, with [INSTALLFOLDER] replaced by installDir.
func wxsShellRegistry(t *testing.T, installDir string) map[string]parityEntry {
	t.Helper()
	root := repoRoot(t)
	raw := readSurface(t, root, filepath.Join("packaging", "wix", "FileDO.wxs"))

	// The one preprocessor variable the keys use. Substituted before parsing, the way the
	// WiX preprocessor does it; any other $(var.*) left in a shell value fails below.
	define := regexp.MustCompile(`<\?define\s+ProgId\s*=\s*"([^"]+)"\s*\?>`).FindStringSubmatch(raw)
	if define == nil {
		t.Fatal("FileDO.wxs no longer defines ProgId - the parity test needs updating with it")
	}
	if define[1] != fdsecProgID {
		t.Errorf("the MSI's ProgId is %q, fdsec register's is %q", define[1], fdsecProgID)
	}
	raw = strings.ReplaceAll(raw, "$(var.ProgId)", define[1])

	var doc parityWxs
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("FileDO.wxs does not parse: %v", err)
	}
	const classes = `Software\Classes\`
	out := map[string]parityEntry{}
	groups := 0
	for _, f := range doc.Fragments {
		for _, g := range f.Groups {
			if g.ID != "ShellIntegration" {
				continue
			}
			groups++
			for _, c := range g.Components {
				for _, k := range c.Keys {
					if k.Root != "HKLM" || !strings.HasPrefix(strings.ToLower(k.Key), strings.ToLower(classes)) {
						t.Errorf("component %s writes %s\\%s - the shell integration belongs under HKLM\\%s", c.ID, k.Root, k.Key, classes)
						continue
					}
					base := k.Key[len(classes):]
					for _, v := range k.Values {
						keyPath := base
						if v.Key != "" {
							keyPath += `\` + v.Key
						}
						data := strings.ReplaceAll(v.Value, "[INSTALLFOLDER]", installDir+`\`)
						if strings.Contains(data, "$(") || strings.Contains(data, "[") {
							t.Errorf("%s value %q carries an unresolved variable: %q", keyPath, v.Name, v.Value)
						}
						id := parityID(keyPath, v.Name)
						if _, dup := out[id]; dup {
							t.Errorf("the MSI writes %s twice", id)
						}
						out[id] = parityEntry{typ: v.Type, data: data}
					}
				}
			}
		}
	}
	if groups != 1 || len(out) == 0 {
		t.Fatalf("found %d ShellIntegration component groups with %d values in FileDO.wxs", groups, len(out))
	}
	return out
}

// readRegistryTree returns every value below HKCU\<path>, keyed like wxsShellRegistry.
func readRegistryTree(t *testing.T, path, rel string, out map[string]parityEntry) {
	t.Helper()
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		t.Fatalf("open HKCU\\%s: %v", path, err)
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		t.Fatalf("list values of HKCU\\%s: %v", path, err)
	}
	for _, name := range names {
		entry := parityEntry{}
		_, typ, err := k.GetValue(name, nil)
		if err != nil {
			t.Fatalf("read the type of %s\\%s: %v", path, name, err)
		}
		switch typ {
		case registry.SZ:
			s, _, err := k.GetStringValue(name)
			if err != nil {
				t.Fatalf("read %s\\%s: %v", path, name, err)
			}
			entry = parityEntry{typ: "string", data: s}
		case registry.DWORD:
			n, _, err := k.GetIntegerValue(name)
			if err != nil {
				t.Fatalf("read %s\\%s: %v", path, name, err)
			}
			entry = parityEntry{typ: "integer", data: fmt.Sprint(n)}
		default:
			entry = parityEntry{typ: fmt.Sprintf("registry type %d", typ)}
		}
		out[parityID(rel, name)] = entry
	}
	subs, err := k.ReadSubKeyNames(-1)
	if err != nil {
		t.Fatalf("list subkeys of HKCU\\%s: %v", path, err)
	}
	for _, s := range subs {
		child := s
		if rel != "" {
			child = rel + `\` + s
		}
		readRegistryTree(t, path+`\`+s, child, out)
	}
}

func TestRegistryParity_TheMSIAndFdsecRegisterWriteTheSameShellIntegration(t *testing.T) {
	// The layout the MSI installs: filedo.exe with FileDO.ico beside it. The icon's bytes do
	// not matter here; its presence is what makes both writers point the group's Icon at it.
	installDir := filepath.Join(t.TempDir(), "FileDO")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(installDir, "filedo.exe")
	body, err := os.ReadFile(filedoExe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, body, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(installDir, "FileDO.ico"), []byte("icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	// And the meaning icons the MSI installs into icons\ (SP-0016 T8), whose presence is what
	// makes fdsec register write each entry's Icon and the document type's.
	if err := os.MkdirAll(filepath.Join(installDir, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"action.secure", "action.unsecure", "action.wipe", "action.verify", "app.info", fdsecSecretFileIcon} {
		if err := os.WriteFile(filepath.Join(installDir, "icons", id+".ico"), []byte("icon"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// -all-users: the machine-wide shape, which is the MSI's. The seam sends it to HKCU.
	seam, _, machineClasses := regSeam(t)
	cmd := exec.Command(exe, "fdsec", "register", "-all-users")
	cmd.Dir = installDir
	cmd.Env = append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(installDir),
		fdsecRegRootEnv+"="+seam,
		fdsecPackagedMenuEnv+"=0",
	)
	cmd.Stdin = strings.NewReader("")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fdsec register -all-users: %v\n%s", err, out)
	}

	got := map[string]parityEntry{}
	readRegistryTree(t, machineClasses, "", got)

	// [INSTALLFOLDER] is the folder the registering exe resolved itself to - read from the
	// mark it left, then checked to be the folder created above, whatever path spelling
	// (short name, case) Windows handed back.
	mark, ok := got[parityID(fdsecProgID, fdsecOwnerValue)]
	if !ok || !strings.EqualFold(filepath.Base(mark.data), "filedo.exe") {
		t.Fatalf("fdsec register left no usable %s mark on the document type: %+v", fdsecOwnerValue, mark)
	}
	resolvedDir := filepath.Dir(mark.data)
	a, errA := os.Stat(resolvedDir)
	b, errB := os.Stat(installDir)
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Fatalf("the registered exe is in %q, not in the install folder %q", resolvedDir, installDir)
	}

	want := wxsShellRegistry(t, resolvedDir)

	var problems []string
	for id, w := range want {
		g, ok := got[id]
		switch {
		case !ok:
			problems = append(problems, fmt.Sprintf("the MSI writes %s = %s %q; fdsec register does not write it", id, w.typ, w.data))
		case g.typ != w.typ:
			problems = append(problems, fmt.Sprintf("%s is a %s in the MSI and a %s from fdsec register", id, w.typ, g.typ))
		case g.data != w.data:
			problems = append(problems, fmt.Sprintf("%s differs:\n      MSI:            %q\n      fdsec register: %q", id, w.data, g.data))
		}
	}
	for id, g := range got {
		if _, ok := want[id]; !ok {
			problems = append(problems, fmt.Sprintf("fdsec register writes %s = %s %q; the MSI does not write it", id, g.typ, g.data))
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
	if len(problems) == 0 {
		t.Logf("%d registry values agree between FileDO.wxs and fdsec register", len(want))
	}
}
