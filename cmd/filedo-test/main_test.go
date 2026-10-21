package main

import (
	"reflect"
	"testing"
)

// The sample the shared launcher tests run end to end: a folder with a space
// and a trailing backslash (the quoting a command line most often breaks), an
// upper-case del, and global options with values containing spaces.
var (
	sampleArgs = []string{`D:\My Test Dir\`, "DEL", "--events", `C:\ev dir\run.jsonl`, "--pause", "-y"}
	sampleWant = []string{`D:\My Test Dir\`, "test", "del", "--events", `C:\ev dir\run.jsonl`, "--pause", "-y"}
)

// badArgs are command lines filedo_test refuses with exit 2, before filedo.exe
// is started.
var badArgs = [][]string{
	{"E:", "fast"},
	{"E:", "500"},
	{"E:", "clean", "del"},
	{"E:", "--bogus"},
	{"E:", "-x"},
	{"E:", "--events"},
	{"E:", "--stop-file"},
	{"E:", "--PAUSE"},
	{"E:", "speed"},
	{""},
	{"   "},
	{"--pause"},
}

// TestMapArgs pins every form filedo_test documents to the exact filedo.exe
// command line it runs.
func TestMapArgs(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"E:"}, []string{"E:", "test"}},
		{[]string{"E:", "del"}, []string{"E:", "test", "del"}},
		{[]string{"E:", "delete"}, []string{"E:", "test", "del"}},
		{[]string{"E:", "d"}, []string{"E:", "test", "del"}},
		{[]string{"E:", "Del"}, []string{"E:", "test", "del"}},
		{[]string{`C:\temp`}, []string{`C:\temp`, "test"}},
		{[]string{`C:\temp`, "del"}, []string{`C:\temp`, "test", "del"}},
		{[]string{`\\server\share`}, []string{`\\server\share`, "test"}},
		{[]string{`\\server\share`, "del"}, []string{`\\server\share`, "test", "del"}},
		// The system drive goes to filedo.exe exactly as typed, so its guard
		// sees what the user wrote.
		{[]string{"C:"}, []string{"C:", "test"}},
		{[]string{`C:\`}, []string{`C:\`, "test"}},
		// clean: what the old filedo_test told its users to run.
		{[]string{"E:", "clean"}, []string{"E:", "clean"}},
		{[]string{"E:", "c"}, []string{"E:", "clean"}},
		{[]string{`C:\temp`, "clean"}, []string{`C:\temp`, "clean"}},
		// Targets the companion always accepted that filedo.exe would misread.
		{[]string{"e"}, []string{"e:", "test"}},
		{[]string{"F", "del"}, []string{"F:", "test", "del"}},
		{[]string{"n"}, []string{"n:", "test"}},
		{[]string{"copy"}, []string{`.\copy`, "test"}},
		{[]string{"  E:  "}, []string{"E:", "test"}},
		// Global options, handed over unchanged and in order, wherever given.
		{[]string{"E:", "del", "--events", "x.jsonl", "--stop-file", "s.stop", "--pause", "--no-history"},
			[]string{"E:", "test", "del", "--events", "x.jsonl", "--stop-file", "s.stop", "--pause", "--no-history"}},
		{[]string{"--events=x.jsonl", "E:", "--stop-file=s.stop"}, []string{"E:", "test", "--events=x.jsonl", "--stop-file=s.stop"}},
		{[]string{"E:", "-pause", "/pause", "--no-ui", "nohist", "no_history"}, []string{"E:", "test", "-pause", "/pause", "--no-ui", "nohist", "no_history"}},
		{[]string{"E:", "-y", "--yes", "--force"}, []string{"E:", "test", "-y", "--yes", "--force"}},
		{[]string{"E:", "clean", "--pause"}, []string{"E:", "clean", "--pause"}},
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
