package main

import (
	"reflect"
	"strings"
	"testing"

	"filedo/statedir"
)

// The window verbs of APP-COMMANDS 0.1 (SP-0217): disk-manager show, settings show
// and tray start. With no first instance each one launches the window program with
// the spelling of its own start and then asks the copy it started; with a copy
// running, the copy answers and nothing is started; a busy copy answers 11. The
// harness and the fake first instance are the ones of appcmd_test.go.

// windowVerbs are the three verbs, as the manifest names them.
var windowVerbs = []string{"disk-manager show", "settings show", "tray start"}

// TestWindowVerbsLaunchTheRightWindow holds the spelling each cold start launches:
// the Disk Manager alone, the hidden tray start, and a bare start for the two that
// bring the main window (show keeps the bare start it always made).
func TestWindowVerbsLaunchTheRightWindow(t *testing.T) {
	cases := []struct {
		verb string
		want []string
	}{
		{"disk-manager show", []string{"--disks"}},
		{"tray start", []string{"--startup", "--tray"}},
		{"settings show", nil},
		{"show", nil},
	}
	for _, c := range cases {
		if got := appCmdStartArgs(c.verb); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: cold start launches %q, want %q", c.verb, got, c.want)
		}
	}
}

// TestWindowVerbs_ColdStartLaunchesThenAsks: with no first instance, each verb
// launches its own spelling once, asks the copy it started, and exits with the
// answer's code. Nothing is answered locally.
func TestWindowVerbs_ColdStartLaunchesThenAsks(t *testing.T) {
	for _, verb := range windowVerbs {
		t.Run(verb, func(t *testing.T) {
			pipe := testPipePath(t)
			h := newAppCmdHarness(t, pipe)
			var f *fakeFirstInstance
			appCmdLaunch = func(args ...string) error {
				h.launched++
				h.launchArgs = args
				f = startFakeFirstInstance(t, pipe, doneAnswer(nil))
				return nil
			}
			if code := runAppCmd(t, strings.Fields(verb)...); code != 0 {
				t.Fatalf("exit %d, want 0; err %q", code, h.err.String())
			}
			if h.launched != 1 {
				t.Fatalf("launched %d times, want 1", h.launched)
			}
			if !reflect.DeepEqual(h.launchArgs, appCmdStartArgs(verb)) {
				t.Fatalf("launched with %q, want %q", h.launchArgs, appCmdStartArgs(verb))
			}
			if f == nil {
				t.Fatal("the started copy never listened")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.requests) != 1 || f.requests[0].Command != verb {
				t.Fatalf("the started copy was asked %+v, want one request for %q", f.requests, verb)
			}
		})
	}
}

// TestWindowVerbs_ForwardedWhenRunningLaunchesNothing: with a copy running, each
// verb is carried to it and answered; the window program is not started again, so
// a tray start over a running copy changes nothing (A3).
func TestWindowVerbs_ForwardedWhenRunningLaunchesNothing(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	f := startFakeFirstInstance(t, pipe, doneAnswer(nil))
	for _, verb := range windowVerbs {
		if code := runAppCmd(t, strings.Fields(verb)...); code != 0 {
			t.Fatalf("%s: exit %d, want 0; err %q", verb, code, h.err.String())
		}
	}
	if h.launched != 0 {
		t.Fatalf("launched %d times with a copy running, want 0", h.launched)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != len(windowVerbs) {
		t.Fatalf("the copy was asked %d times, want %d", len(f.requests), len(windowVerbs))
	}
	for i, verb := range windowVerbs {
		if f.requests[i].Command != verb {
			t.Errorf("request %d carried %q, want %q", i, f.requests[i].Command, verb)
		}
	}
}

// TestWindowVerbs_BusyIsExit11: a copy that answers busy (a modal window is open,
// the program is closing) makes each verb exit 11 with reason busy, and nothing is
// started, whatever the verb.
func TestWindowVerbs_BusyIsExit11(t *testing.T) {
	pipe := testPipePath(t)
	h := newAppCmdHarness(t, pipe)
	startFakeFirstInstance(t, pipe, func(req appCmdRequest) *appCmdResponse {
		return &appCmdResponse{SchemaVersion: 1, RequestID: req.RequestID, Status: "busy", ExitCode: 11, Reason: "busy",
			Message: "FileDO is busy with another command; try again in a moment."}
	})
	for _, verb := range windowVerbs {
		h.out.Reset()
		h.err.Reset()
		if code := runAppCmd(t, strings.Fields(verb)...); code != 11 {
			t.Fatalf("%s: exit %d, want 11", verb, code)
		}
		if !strings.Contains(h.err.String(), "busy") {
			t.Errorf("%s: stderr %q does not say the program is busy", verb, h.err.String())
		}
	}
	if h.launched != 0 {
		t.Fatalf("launched %d times while busy, want 0", h.launched)
	}
}

// TestWindowVerbs_FrozenSpellingsStillStart: `dm` keeps the Disk Manager start it
// always made (one launch with --disks, no answer awaited), and the spellings of
// the new verbs' aliases are not taken by the front as verbs.
func TestWindowVerbs_FrozenSpellingsStillStart(t *testing.T) {
	t.Setenv(statedir.EnvOverride, t.TempDir())
	var gotArgs []string
	launches := 0
	saved := uiLauncher
	uiLauncher = func(args ...string) error { launches++; gotArgs = args; return nil }
	err := dispatchLine([]string{"dm"}, NewHistoryLogger([]string{"filedo", "dm"}), false)
	uiLauncher = saved
	if err != nil || launches != 1 || !reflect.DeepEqual(gotArgs, []string{"--disks"}) {
		t.Fatalf("dm: launched %d time(s) with %q (err %v), want one launch with --disks", launches, gotArgs, err)
	}
	for _, alias := range []string{"dm", "-dm", "--disks", "--startup", "--tray"} {
		if e, _ := appCommandOf([]string{alias}); e != nil {
			t.Errorf("%s was taken by the front; it is a frozen spelling that keeps its start", alias)
		}
	}
	for _, words := range [][]string{{"--startup", "--disks"}, {"--startup", "--tray"}} {
		if e, _ := appCommandOf(words); e != nil {
			t.Errorf("%q was taken by the front; it is a frozen spelling that keeps its start", words)
		}
	}
}
