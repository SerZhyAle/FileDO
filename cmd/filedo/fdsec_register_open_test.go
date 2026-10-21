package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Which command answers a double-click (SP-0005 9.4, amended 2026-09-22).
//
// It is the CLI, unconditionally, and it is exactly the "Unsecure and start"
// menu entry - built by the same helper, so the two cannot drift apart. A
// double-click on a container that has never seen the GUI installed (`go
// install`) or one sitting beside it makes no difference: there is nothing to
// detect any more, because nothing here reads the disk.

func TestFdsecOpenCommand_IsExactlyUnsecureStart(t *testing.T) {
	dir := t.TempDir()
	cli := filepath.Join(dir, "filedo.exe")

	cmd := fdsecOpenCommand(cli)
	want := fdsecMenuCommand(cli, fdsecMenuItem{args: `"%1" unsecure start`})
	if cmd != want {
		t.Errorf("fdsecOpenCommand(%q) = %q, want %q", cli, cmd, want)
	}
	if !strings.Contains(cmd, `"%1" unsecure start`) {
		t.Errorf("the open command does not unsecure and start the clicked file: %q", cmd)
	}
	if !strings.Contains(cmd, "--pause") || !strings.Contains(cmd, "--no-history") {
		t.Errorf("the open command drops a flag every other menu entry carries: %q", cmd)
	}
	if strings.Contains(cmd, "filedo_win.exe") {
		t.Errorf("the open command still names the GUI: %q", cmd)
	}
}
