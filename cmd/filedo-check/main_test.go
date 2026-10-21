package main

import (
	"reflect"
	"testing"
)

// The sample the shared launcher tests run end to end: a folder with a space
// and a trailing backslash (the quoting a command line most often breaks), a
// mode, a check option with its value, and global options with values
// containing spaces.
var (
	sampleArgs = []string{`D:\My Test Dir\`, "DEEP", "--threshold", "5", "--events", `C:\ev dir\run.jsonl`, "--pause"}
	sampleWant = []string{"check", `D:\My Test Dir\`, "--mode", "deep", "--threshold", "5", "--events", `C:\ev dir\run.jsonl`, "--pause"}
)

// badArgs are command lines filedo_check refuses with exit 2, before
// filedo.exe is started.
var badArgs = [][]string{
	{"D:", "fast"},
	{"D:", "quick", "deep"},
	{"D:", "--resume"},
	{"D:", "--dry-run"},
	{"D:", "--mode", "deep"},
	{"D:", "--bogus"},
	{"D:", "-x"},
	{"D:", "-y"},
	{"D:", "--force"},
	{"D:", "--threshold"},
	{"D:", "--report", "xml"},
	{"D:", "--report=pdf"},
	{"D:", "--verbose=true"},
	{"D:", "--events"},
	{"D:", "--PAUSE"},
	{""},
	{"   "},
	{"--verbose"},
}

// TestMapArgs pins every form filedo_check documents to the exact filedo.exe
// command line it runs.
func TestMapArgs(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		// Targets; a drive is checked from its root, as filedo_check always did.
		{[]string{"C:"}, []string{"check", `C:\`, "--mode", "balanced"}},
		{[]string{"d:"}, []string{"check", `d:\`, "--mode", "balanced"}},
		{[]string{`C:\`}, []string{"check", `C:\`, "--mode", "balanced"}},
		{[]string{"C"}, []string{"check", `C:\`, "--mode", "balanced"}},
		{[]string{"d"}, []string{"check", `d:\`, "--mode", "balanced"}},
		{[]string{`C:\folder`}, []string{"check", `C:\folder`, "--mode", "balanced"}},
		{[]string{`\\server\share`}, []string{"check", `\\server\share`, "--mode", "balanced"}},
		{[]string{`D:\Video\one.mkv`}, []string{"check", `D:\Video\one.mkv`, "--mode", "balanced"}},
		{[]string{"Photos"}, []string{"check", `.\Photos`, "--mode", "balanced"}},
		{[]string{"secure"}, []string{"check", `.\secure`, "--mode", "balanced"}},
		{[]string{"  D:  "}, []string{"check", `D:\`, "--mode", "balanced"}},
		// Modes.
		{[]string{"C:", "quick"}, []string{"check", `C:\`, "--mode", "quick"}},
		{[]string{"C:", "q"}, []string{"check", `C:\`, "--mode", "quick"}},
		{[]string{"D:", "balanced"}, []string{"check", `D:\`, "--mode", "balanced"}},
		{[]string{"D:", "b"}, []string{"check", `D:\`, "--mode", "balanced"}},
		{[]string{"C:", "deep"}, []string{"check", `C:\`, "--mode", "deep"}},
		{[]string{"C:", "d"}, []string{"check", `C:\`, "--mode", "deep"}},
		{[]string{"C:", "Deep"}, []string{"check", `C:\`, "--mode", "deep"}},
		{[]string{`C:\folder`, "quick"}, []string{"check", `C:\folder`, "--mode", "quick"}},
		{[]string{`\\server\share`, "balanced"}, []string{"check", `\\server\share`, "--mode", "balanced"}},
		{[]string{"C:", "quick", "q"}, []string{"check", `C:\`, "--mode", "quick"}},
		// Options, each passed under its own name, value forms kept.
		{[]string{"C:", "--threshold", "5"}, []string{"check", `C:\`, "--mode", "balanced", "--threshold", "5"}},
		{[]string{"C:", "--threshold=1.5"}, []string{"check", `C:\`, "--mode", "balanced", "--threshold=1.5"}},
		{[]string{"C:", "--verbose"}, []string{"check", `C:\`, "--mode", "balanced", "--verbose"}},
		{[]string{"C:", "--quiet"}, []string{"check", `C:\`, "--mode", "balanced", "--quiet"}},
		{[]string{"C:", "--workers", "4"}, []string{"check", `C:\`, "--mode", "balanced", "--workers", "4"}},
		{[]string{"C:", "--report", "csv"}, []string{"check", `C:\`, "--mode", "balanced", "--report", "csv"}},
		{[]string{"C:", "--report=JSON"}, []string{"check", `C:\`, "--mode", "balanced", "--report=JSON"}},
		{[]string{"C:", "--max-files", "1000"}, []string{"check", `C:\`, "--mode", "balanced", "--max-files", "1000"}},
		{[]string{"C:", "--min-mb", "100"}, []string{"check", `C:\`, "--mode", "balanced", "--min-mb", "100"}},
		{[]string{"C:", "--max-mb", "4096"}, []string{"check", `C:\`, "--mode", "balanced", "--max-mb", "4096"}},
		{[]string{"C:", "--include-ext", "jpg,png,mp4"}, []string{"check", `C:\`, "--mode", "balanced", "--include-ext", "jpg,png,mp4"}},
		{[]string{"C:", "--exclude-ext", "tmp,log"}, []string{"check", `C:\`, "--mode", "balanced", "--exclude-ext", "tmp,log"}},
		{[]string{"C:", "--precount"}, []string{"check", `C:\`, "--mode", "balanced", "--precount"}},
		{[]string{"C:", "--threshold", "-1"}, []string{"check", `C:\`, "--mode", "balanced", "--threshold", "-1"}},
		// Mode and options in any order, as the old parser allowed.
		{[]string{"--verbose", "C:", "--threshold", "5", "quick"}, []string{"check", `C:\`, "--mode", "quick", "--verbose", "--threshold", "5"}},
		// Global options, handed over unchanged and in order, wherever given.
		{[]string{"C:", "quick", "--events", "x.jsonl", "--stop-file", "s.stop", "--pause", "--no-history"},
			[]string{"check", `C:\`, "--mode", "quick", "--events", "x.jsonl", "--stop-file", "s.stop", "--pause", "--no-history"}},
		{[]string{"--events=x.jsonl", "C:", "--stop-file=s.stop"}, []string{"check", `C:\`, "--mode", "balanced", "--events=x.jsonl", "--stop-file=s.stop"}},
		{[]string{"C:", "-pause", "/pause", "--no-ui", "nohist", "no_history", "--verbose"},
			[]string{"check", `C:\`, "--mode", "balanced", "--verbose", "-pause", "/pause", "--no-ui", "nohist", "no_history"}},
	}
	for _, c := range cases {
		got, err := mapArgs(c.args)
		if err != nil {
			t.Errorf("mapArgs(%q): %v", c.args, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("mapArgs(%q) = %q, want %q", c.args, got, c.want)
		}
	}
	if got, err := mapArgs(sampleArgs); err != nil || !reflect.DeepEqual(got, sampleWant) {
		t.Errorf("mapArgs(sampleArgs) = %q, %v; want %q", got, err, sampleWant)
	}
}

func TestMapArgsRefuses(t *testing.T) {
	for _, args := range append(badArgs, []string{}) {
		got, err := mapArgs(args)
		if !isUsageError(err) {
			t.Errorf("mapArgs(%q) = %q, %v; want a usage error", args, got, err)
		}
	}
}

// TestEveryDocumentedOptionIsMapped holds the usage text and the option table
// together: an option in one and not the other is a promise the code does not
// keep, or a feature nobody can find.
func TestEveryDocumentedOptionIsMapped(t *testing.T) {
	usage := usageText()
	for name := range checkOptions {
		if !containsWord(usage, name) {
			t.Errorf("%s is accepted but the usage text does not list it", name)
		}
	}
	for name := range withdrawnOptions {
		if containsWord(usage, name) {
			t.Errorf("%s is refused but the usage text still lists it", name)
		}
	}
}

func containsWord(text, word string) bool {
	for i := 0; i+len(word) <= len(text); i++ {
		if text[i:i+len(word)] != word {
			continue
		}
		end := i + len(word)
		if end == len(text) || !isOptionChar(text[end]) {
			return true
		}
	}
	return false
}

func isOptionChar(c byte) bool {
	return c == '-' || ('a' <= c && c <= 'z') || ('0' <= c && c <= '9')
}
