package main

import (
	"reflect"
	"testing"
)

// The sample the shared launcher tests run end to end: a folder with a space
// and a trailing backslash (the quoting a command line most often breaks), a
// size, an upper-case del, and global options with values containing spaces.
var (
	sampleArgs = []string{`D:\My Test Dir\`, "500", "DEL", "--events", `C:\ev dir\run.jsonl`, "--pause", "-y"}
	sampleWant = []string{`D:\My Test Dir\`, "fill", "500", "del", "--events", `C:\ev dir\run.jsonl`, "--pause", "-y"}
)

// badArgs are command lines filedo_fill refuses with exit 2, before filedo.exe
// is started.
var badArgs = [][]string{
	{"D:", "fast"},
	{"D:", "0"},
	{"D:", "10241"},
	{"D:", "99999999999999999999"},
	{"D:", "100", "200"},
	{"D:", "clean", "del"},
	{"D:", "clean", "100"},
	{"D:", "--bogus"},
	{"D:", "-x"},
	{"D:", "--events"},
	{"D:", "--stop-file"},
	{"D:", "--PAUSE"},
	{"D:", "nodel"},
	{"D:", "max"},
	{""},
	{"   "},
	{"--pause"},
}

// TestMapArgs pins every form filedo_fill documents to the exact filedo.exe
// command line it runs.
func TestMapArgs(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		// Fill, every documented spelling.
		{[]string{"D:"}, []string{"D:", "fill", "100"}},
		{[]string{"D:", "500"}, []string{"D:", "fill", "500"}},
		{[]string{"E:", "1000", "del"}, []string{"E:", "fill", "1000", "del"}},
		{[]string{"E:", "1000", "delete"}, []string{"E:", "fill", "1000", "del"}},
		{[]string{"E:", "1000", "d"}, []string{"E:", "fill", "1000", "del"}},
		{[]string{"E:", "1000", "DEL"}, []string{"E:", "fill", "1000", "del"}},
		{[]string{"E:", "del", "1000"}, []string{"E:", "fill", "1000", "del"}},
		{[]string{"D:", "del"}, []string{"D:", "fill", "100", "del"}},
		{[]string{"D:", "1"}, []string{"D:", "fill", "1"}},
		{[]string{"D:", "10240"}, []string{"D:", "fill", "10240"}},
		{[]string{"D:", "0500"}, []string{"D:", "fill", "500"}},
		{[]string{`C:\temp`}, []string{`C:\temp`, "fill", "100"}},
		{[]string{`C:\temp`, "200", "del"}, []string{`C:\temp`, "fill", "200", "del"}},
		{[]string{`\\server\share`}, []string{`\\server\share`, "fill", "100"}},
		{[]string{`\\server\share`, "100"}, []string{`\\server\share`, "fill", "100"}},
		// The system drive goes to filedo.exe exactly as typed, so its guard
		// sees what the user wrote.
		{[]string{"C:"}, []string{"C:", "fill", "100"}},
		{[]string{`C:\`}, []string{`C:\`, "fill", "100"}},
		// Clean.
		{[]string{"C:", "clean"}, []string{"C:", "clean"}},
		{[]string{"C:", "c"}, []string{"C:", "clean"}},
		{[]string{"C:", "CLEAN"}, []string{"C:", "clean"}},
		{[]string{`C:\temp`, "clean"}, []string{`C:\temp`, "clean"}},
		{[]string{`\\server\share`, "clean"}, []string{`\\server\share`, "clean"}},
		// Targets the companion always accepted that filedo.exe would misread.
		{[]string{"e"}, []string{"e:", "fill", "100"}},
		{[]string{"D"}, []string{"D:", "fill", "100"}},
		{[]string{"d", "del"}, []string{"d:", "fill", "100", "del"}},
		{[]string{"wipe"}, []string{`.\wipe`, "fill", "100"}},
		{[]string{"  D:  ", "300"}, []string{"D:", "fill", "300"}},
		// Global options, handed over unchanged and in order, wherever given.
		{[]string{"D:", "100", "--events", "x.jsonl", "--stop-file", "s.stop", "--pause", "--no-history"},
			[]string{"D:", "fill", "100", "--events", "x.jsonl", "--stop-file", "s.stop", "--pause", "--no-history"}},
		{[]string{"--events=x.jsonl", "D:", "--stop-file=s.stop"}, []string{"D:", "fill", "100", "--events=x.jsonl", "--stop-file=s.stop"}},
		{[]string{"D:", "-pause", "/pause", "--no-ui", "nohist", "no_history"}, []string{"D:", "fill", "100", "-pause", "/pause", "--no-ui", "nohist", "no_history"}},
		{[]string{"D:", "500", "del", "-y", "--yes", "--force"}, []string{"D:", "fill", "500", "del", "-y", "--yes", "--force"}},
		{[]string{"D:", "clean", "-y"}, []string{"D:", "clean", "-y"}},
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
