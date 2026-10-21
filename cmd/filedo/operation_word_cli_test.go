package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUnknownOperationWordIsUsageError is AUD-29-F3: a misspelt operation word
// after a target is a usage error (Not proven, exit 2), never the target's
// info and a Done that a script reads as success. The words that do mean
// info, and the history switch, still run it.
func TestUnknownOperationWordIsUsageError(t *testing.T) {
	dir, _ := workdir(t)
	sub := filepath.Join(dir, "dir")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{sub, "tset"},
		{sub, "fil", "100"},
		{filepath.Join(dir, "plain.txt"), "secrue"},
	} {
		events := filepath.Join(dir, "ev.jsonl")
		os.Remove(events)
		out, code := run(t, dir, append([]string{"--events", events}, args...)...)
		if code != 2 {
			t.Fatalf("filedo %q exited %d, want 2\n%s", args, code, out)
		}
		if v := lastResult(t, events)["verdict"]; v != "Not proven" {
			t.Fatalf("filedo %q ended %v, want Not proven\n%s", args, v, out)
		}
		if !strings.Contains(out, `Unknown command "`+args[1]+`"`) {
			t.Fatalf("filedo %q does not name the unknown word\n%s", args, out)
		}
	}
	for _, args := range [][]string{
		{sub},
		{sub, "info"},
		{sub, "i"},
		{sub, "short"},
		{sub, "s"},
		{sub, "nohist"},
		{sub, "no_history"},
		{sub, "INFO"},
	} {
		if out, code := run(t, dir, args...); code != 0 {
			t.Fatalf("filedo %q exited %d, want 0\n%s", args, code, out)
		}
	}
}
