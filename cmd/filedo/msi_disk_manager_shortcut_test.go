package main

// The Disk Manager's own Start menu entry (SP-0063), read from packaging/wix/FileDO.wxs as text.
//
// The MSI gives filedo_win.exe two Start menu shortcuts: "FileDO" (the shell) and
// "FileDO Disk Manager" (the same exe with --disks). The second one is easy to break without
// a compiler noticing, and each way is silent:
//   - it lands in DiskContainerIntegration, so a person who unticks the .fdd file type loses
//     the window's entry too (the window is part of the program, not of that file type);
//   - it reuses or renames an existing component GUID, so an upgrade leaves the old copy
//     installed or drops the shortcut of a machine that already has FileDO;
//   - it stops pointing at filedo_win.exe with --disks, and opens the shell instead.
// The check is a function over the wxs text, so the second test can prove it fails on each of
// those mistakes instead of trusting that it would.

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Component GUIDs that shipped. A component keeps its GUID for the product's life (AGENTS.md,
// frozen anchors), so these two are pinned here by value.
const (
	msiStartMenuComponentGUID = "7e1c9a44-2f8b-4d63-9a1e-5c6b8d2f4a30"
	msiDesktopComponentGUID   = "3f5d8c21-6b04-4e97-8a52-0d7c9e1b4f63"
)

type msiShortcut struct {
	ID        string `xml:"Id,attr"`
	Name      string `xml:"Name,attr"`
	Target    string `xml:"Target,attr"`
	Arguments string `xml:"Arguments,attr"`
}

type msiComponent struct {
	ID        string        `xml:"Id,attr"`
	Directory string        `xml:"Directory,attr"`
	GUID      string        `xml:"Guid,attr"`
	Shortcuts []msiShortcut `xml:"Shortcut"`
}

type msiComponentRef struct {
	ID string `xml:"Id,attr"`
}

type msiFeature struct {
	ID       string            `xml:"Id,attr"`
	Refs     []msiComponentRef `xml:"ComponentRef"`
	Children []msiFeature      `xml:"Feature"`
}

type msiWxsDoc struct {
	Features   []msiFeature   `xml:"Package>Feature"`
	Components []msiComponent `xml:"Package>Component"`
	Fragments  []struct {
		Components []msiComponent `xml:"Component"`
		Groups     []struct {
			Components []msiComponent `xml:"Component"`
		} `xml:"ComponentGroup"`
	} `xml:"Fragment"`
}

func (d msiWxsDoc) allComponents() []msiComponent {
	out := append([]msiComponent(nil), d.Components...)
	for _, f := range d.Fragments {
		out = append(out, f.Components...)
		for _, g := range f.Groups {
			out = append(out, g.Components...)
		}
	}
	return out
}

// hasRef reports whether the feature or anything nested under it references the component.
func (f msiFeature) hasRef(id string) bool {
	for _, r := range f.Refs {
		if r.ID == id {
			return true
		}
	}
	for _, c := range f.Children {
		if c.hasRef(id) {
			return true
		}
	}
	return false
}

func findMSIFeature(fs []msiFeature, id string) (msiFeature, bool) {
	for _, f := range fs {
		if f.ID == id {
			return f, true
		}
		if got, ok := findMSIFeature(f.Children, id); ok {
			return got, true
		}
	}
	return msiFeature{}, false
}

var msiGUIDShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// msiDiskManagerProblems returns every way the wxs text fails the Disk Manager shortcut's
// contract; none means it holds.
func msiDiskManagerProblems(raw string) []string {
	var doc msiWxsDoc
	if err := xml.Unmarshal([]byte(raw), &doc); err != nil {
		return []string{"FileDO.wxs does not parse: " + err.Error()}
	}
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	comps := doc.allComponents()

	// The shortcut: exactly one, on filedo_win.exe with --disks, named for what it opens.
	var owner msiComponent
	var sc msiShortcut
	found := 0
	for _, c := range comps {
		for _, s := range c.Shortcuts {
			if s.Arguments == "--disks" {
				found++
				owner, sc = c, s
			}
		}
	}
	if found != 1 {
		add(`want exactly one Shortcut with Arguments="--disks", found %d`, found)
		return problems
	}
	if !strings.HasSuffix(sc.Target, "filedo_win.exe") {
		add("shortcut %s targets %q, want a path ending filedo_win.exe", sc.ID, sc.Target)
	}
	if sc.Name != "FileDO Disk Manager" {
		add("shortcut %s is named %q, want %q", sc.ID, sc.Name, "FileDO Disk Manager")
	}
	if owner.Directory != "AppProgramMenuDir" {
		add("component %s holding the shortcut is in Directory %q, want AppProgramMenuDir (the Start menu folder)", owner.ID, owner.Directory)
	}

	// Feature membership: a ComponentRef of Main itself, and of no feature nested under it -
	// DiskContainerIntegration above all, which a person can deselect.
	mainFeature, ok := findMSIFeature(doc.Features, "Main")
	if !ok {
		add("feature Main is gone")
	} else {
		direct := false
		for _, r := range mainFeature.Refs {
			if r.ID == owner.ID {
				direct = true
			}
		}
		if !direct {
			add("feature Main has no ComponentRef to %s - the entry would not be installed with the program", owner.ID)
		}
		for _, child := range mainFeature.Children {
			if child.hasRef(owner.ID) {
				add("component %s is also referenced under the optional feature %s - deselecting it would drop the entry", owner.ID, child.ID)
			}
		}
	}

	// GUIDs: no two components share one ("*" is WiX generating it, so those are exempt), the
	// new component's is a real fixed GUID, and the two shipped shortcut GUIDs are untouched.
	seen := map[string]string{}
	for _, c := range comps {
		g := strings.ToLower(strings.TrimSpace(c.GUID))
		if g == "" || g == "*" {
			continue
		}
		if prev, dup := seen[g]; dup {
			add("components %s and %s share the Guid %s", prev, c.ID, g)
		}
		seen[g] = c.ID
	}
	if g := strings.ToLower(strings.TrimSpace(owner.GUID)); !msiGUIDShape.MatchString(g) {
		add("component %s needs a fixed GUID of its own, not %q", owner.ID, owner.GUID)
	}
	for _, want := range []string{msiStartMenuComponentGUID, msiDesktopComponentGUID} {
		if _, ok := seen[want]; !ok {
			add("shipped component GUID %s is gone from FileDO.wxs - a component keeps its GUID for the product's life", want)
		}
	}
	return problems
}

func TestMSIDiskManagerShortcut_HoldsItsContract(t *testing.T) {
	raw := readSurface(t, repoRoot(t), filepath.Join("packaging", "wix", "FileDO.wxs"))
	for _, p := range msiDiskManagerProblems(raw) {
		t.Error(p)
	}
}

// The checks above are only worth having if they can fail. Each mutation is one plausible
// mistake made to the real wxs text; every one must be reported.
func TestMSIDiskManagerShortcut_TheChecksCanFail(t *testing.T) {
	raw := readSurface(t, repoRoot(t), filepath.Join("packaging", "wix", "FileDO.wxs"))
	regex := func(pattern, repl string) func(string) string {
		re := regexp.MustCompile(pattern)
		return func(s string) string { return re.ReplaceAllString(s, repl) }
	}
	literal := func(from, to string) func(string) string {
		return func(s string) string { return strings.Replace(s, from, to, 1) }
	}
	const ref = `<ComponentRef Id="DiskManagerShortcut" />`
	for _, m := range []struct {
		name   string
		mutate func(string) string
	}{
		{"wrong arguments", literal(`Arguments="--disks"`, `Arguments="--disk"`)},
		{"wrong target", regex(`Target="\[INSTALLFOLDER\]filedo_win\.exe"(\s+Arguments="--disks")`, `Target="[INSTALLFOLDER]filedo.exe"$1`)},
		{"wrong name", literal(`Name="FileDO Disk Manager"`, `Name="Disks"`)},
		{"wrong directory", regex(`(<Component Id="DiskManagerShortcut" Directory=")AppProgramMenuDir(")`, `${1}DesktopFolder${2}`)},
		{"not referenced by Main", literal(ref, ``)},
		{"moved under DiskContainerIntegration", func(s string) string {
			s = strings.Replace(s, ref, ``, 1)
			return strings.Replace(s, `<ComponentGroupRef Id="DiskContainerShell" />`, `<ComponentGroupRef Id="DiskContainerShell" />`+ref, 1)
		}},
		{"GUID reused from the FileDO shortcut", regex(`(<Component Id="DiskManagerShortcut"[^>]*Guid=")[^"]+(")`, `${1}`+msiStartMenuComponentGUID+`${2}`)},
		{"GUID left to WiX", regex(`(<Component Id="DiskManagerShortcut"[^>]*Guid=")[^"]+(")`, `${1}*${2}`)},
		{"shipped GUID changed", literal(msiStartMenuComponentGUID, `00000000-0000-4000-8000-000000000001`)},
		{"desktop GUID changed", literal(msiDesktopComponentGUID, `00000000-0000-4000-8000-000000000002`)},
	} {
		t.Run(m.name, func(t *testing.T) {
			mutated := m.mutate(raw)
			if mutated == raw {
				t.Fatalf("the mutation changed nothing - FileDO.wxs no longer has the text this test edits")
			}
			if len(msiDiskManagerProblems(mutated)) == 0 {
				t.Errorf("%s: no problem reported", m.name)
			}
		})
	}
}
