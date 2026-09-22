package main

import (
	"strings"
	"testing"
)

// The sealed name is attacker-controlled the moment somebody else built the
// container, so it is the sandbox's real attack surface: it decides where the
// copy is written and, through its extension, whether the copy is handed to
// the shell at all. These are the security review's findings at the end of
// stage S4, each kept as the test that would have caught it.

func TestSandboxName_RefusesWhatWouldEscapeOrMislead(t *testing.T) {
	for _, tc := range []struct{ name, why string }{
		// Escaping the sandbox.
		{`..\..\Windows\System32\evil.dll`, "a path with parent references"},
		{`C:\Windows\evil.dll`, "an absolute path"},
		{`sub/dir/evil.txt`, "a forward-slash path"},
		{`.`, "a directory reference"},
		{`..`, "a parent reference"},
		{``, "an empty name"},

		// The one that actually defeats invariant 7: Windows drops a trailing
		// space or dot when it creates the file, so "invoice.exe " lands on
		// disk as "invoice.exe" while a naive extension check sees ".exe "
		// and finds it in no list of things never to launch.
		{`invoice.exe `, "a trailing space that Windows drops"},
		{`invoice.exe.`, "a trailing dot that Windows drops"},
		{`invoice.exe  `, "two trailing spaces"},
		{`invoice.exe. `, "a trailing dot and space"},

		// A name that is a request to write an alternate data stream of some
		// other file rather than a file of its own.
		{`notes.txt:payload.exe`, "an alternate data stream"},
		{`notes:payload.txt`, "an alternate data stream whose visible extension looks harmless"},

		// Names Windows resolves before it looks at a directory at all.
		{`CON`, "a reserved device name"},
		{`nul.txt`, "a reserved device name with an extension"},
		{`COM1.mp4`, "a reserved device name that looks like a video"},

		// Characters that are not legal in a name, and control characters
		// that would make the printed name lie about itself.
		{"bad\x00name.txt", "a NUL byte"},
		{"bad\rname.txt", "a carriage return that would overwrite the printed line"},
		{`what?.txt`, "a wildcard character"},
		{`a*b.txt`, "a wildcard character"},
	} {
		t.Run(tc.why, func(t *testing.T) {
			got, err := fdsecSandboxName(tc.name)
			if err == nil {
				t.Errorf("fdsecSandboxName(%q) returned %q; want a refusal - %s", tc.name, got, tc.why)
			}
		})
	}
}

// The refusal must not echo the sealed name back, because the error reaches
// history.json, which outlives the reveal and is exactly the place the true
// name must never land.
func TestSandboxName_RefusalDoesNotEchoTheSealedName(t *testing.T) {
	const hostile = `..\..\Windows\System32\quite-distinctive-name.dll`
	_, err := fdsecSandboxName(hostile)
	if err == nil {
		t.Fatalf("the hostile name was accepted")
	}
	if strings.Contains(err.Error(), "quite-distinctive-name") {
		t.Errorf("the refusal echoes the sealed name into a message that reaches history.json:\n%s", err)
	}
}

// And it accepts what a real file is actually called, including the awkward
// and the foreign - a refusal that also refuses ordinary names is a feature
// nobody can use.
func TestSandboxName_AcceptsOrdinaryNames(t *testing.T) {
	for _, name := range []string{
		"holiday.mp4",
		"Отчёт за квартал.docx",
		"budget (final) v2.xlsx",
		"archive.tar.gz",
		"file with  inner  spaces.txt",
		".gitignore",
		"CONTRACT.pdf",       // starts with CON but is not CON
		"nullify.txt",        // starts with nul but is not nul
		"com10.log",          // COM10 is not a reserved device
		"a-file-named-lpt.md", // lpt without a digit is not reserved
	} {
		if _, err := fdsecSandboxName(name); err != nil {
			t.Errorf("fdsecSandboxName(%q) refused an ordinary name: %v", name, err)
		}
	}
}

// Every type in the never-launch list is classified from the name that
// survived the checks above, so the two must agree: what the list refuses is
// what the name rules cannot disguise.
func TestNeverLaunch_CoversTheTypesThatRunThemselves(t *testing.T) {
	for _, ext := range []string{".exe", ".com", ".bat", ".cmd", ".ps1", ".vbs", ".js", ".msi", ".lnk", ".scr", ".hta", ".reg"} {
		if !fdsecNeverLaunch[ext] {
			t.Errorf("%s is not in the never-launch list", ext)
		}
	}
	for _, ext := range []string{".mp4", ".mkv", ".pdf", ".docx", ".txt", ".zip", ".png"} {
		if fdsecNeverLaunch[ext] {
			t.Errorf("%s is refused a launch; it is the case the feature exists for", ext)
		}
	}
}
