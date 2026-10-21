package main

import (
	"regexp"
	"testing"
)

// CONTEXT-HELP rule 1 (SP-0168): F1 is the window's own key - handled where the window has the focus,
// never registered as a system-wide chord and never taken from another program. Nothing in the GUI
// shell may register a hotkey or a keyboard hook, so the rule holds by the absence of the API.
func TestHelp_F1IsNeverAGlobalHotkey(t *testing.T) {
	global := regexp.MustCompile(`RegisterHotKey|SetWindowsHookEx|WH_KEYBOARD|GlobalHotkey`)
	for name, body := range shellSources(t) {
		for n, ln := range codeLines(body) {
			if global.MatchString(ln) {
				t.Errorf("%s:%d registers a global hotkey or a keyboard hook (CONTEXT-HELP rule 1): %s", name, n+1, ln)
			}
		}
	}
}
