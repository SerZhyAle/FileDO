package main

import (
	"reflect"
	"strings"
	"testing"

	"filedo/statedir"
)

// TestWindowVerbsLaunchTheRightWindow covers the spellings of the two window
// verbs: `ui` and `-ui` hand the GUI no arguments, `dm` and `-dm` hand it
// --disks, and every spelling is case-insensitive like the rest of the
// vocabulary. The uiLauncher seam keeps a real window from being started.
func TestWindowVerbsLaunchTheRightWindow(t *testing.T) {
	cases := []struct {
		word string
		want []string
	}{
		{"ui", []string{}},
		{"-ui", []string{}},
		{"dm", []string{"--disks"}},
		{"-dm", []string{"--disks"}},
		{"DM", []string{"--disks"}},
	}
	for _, tc := range cases {
		t.Setenv(statedir.EnvOverride, t.TempDir())
		var gotArgs []string
		saved := uiLauncher
		uiLauncher = func(args ...string) error { gotArgs = args; return nil }
		err := dispatchLine([]string{tc.word}, NewHistoryLogger([]string{"filedo", tc.word}), false)
		uiLauncher = saved
		if err != nil {
			t.Fatalf("%s: dispatch failed: %v", tc.word, err)
		}
		if !reflect.DeepEqual(gotArgs, tc.want) {
			t.Errorf("%s: launched with %q, want %q", tc.word, gotArgs, tc.want)
		}
	}
}

// TestWindowVerbsRefuseBatch: a `.lst` line never starts a window.
func TestWindowVerbsRefuseBatch(t *testing.T) {
	for _, word := range []string{"ui", "-ui", "dm", "-dm"} {
		err := dispatchLine([]string{word}, NewHistoryLogger([]string{"filedo", word}), true)
		if err == nil {
			t.Errorf("%s: batch launch was not refused", word)
		}
	}
}

// TestWindowVerbReportsMissingGUI: with no filedo_win.exe beside the test
// binary and none on PATH, the verb fails with the not-found error instead of
// reporting a success.
func TestWindowVerbReportsMissingGUI(t *testing.T) {
	t.Setenv(statedir.EnvOverride, t.TempDir())
	t.Setenv("PATH", "")
	err := dispatchLine([]string{"dm"}, NewHistoryLogger([]string{"filedo", "dm"}), false)
	if err == nil || !strings.Contains(err.Error(), "filedo_win.exe not found") {
		t.Fatalf("dispatch error = %v, want the not-found error", err)
	}
}
