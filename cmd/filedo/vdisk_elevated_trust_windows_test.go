//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filedo/vdisk"
)

// SP-0064 release-queue row 5 (AUD-31-F4): the elevated half acts only on the
// request bytes whose SHA-256 the consent-covered command line carries. A file
// rewritten after the parent wrote it - any field: NeverHeld, the serial, the
// state folder - is refused before it is parsed, and is removed either way.
func TestVD_ElevatedRequestIsBoundToItsDigest(t *testing.T) {
	dir := t.TempDir()
	write := func(r vdRequest) (string, []byte) {
		t.Helper()
		b, _ := json.Marshal(r)
		p := filepath.Join(dir, "vd.request.json")
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p, b
	}
	orig := vdRequest{Port: 3260, IQN: "iqn.2026-09.ua.od.sza:filedo-x", Serial: "FDD0123456789abcdef0123", StateDir: dir}

	p, b := write(orig)
	got, err := vdReadBoundRequest(p, vdRequestDigest(b))
	if err != nil || got.IQN != orig.IQN || got.Serial != orig.Serial {
		t.Fatalf("the parent's own request was not accepted: %+v, %v", got, err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("the request file was left behind")
	}

	for name, forged := range map[string]vdRequest{
		"never_held flipped": func() vdRequest { r := orig; r.NeverHeld = true; return r }(),
		"serial changed":     func() vdRequest { r := orig; r.Serial = "FDD000000000000000000000"; return r }(),
		"state dir moved":    func() vdRequest { r := orig; r.StateDir = `C:\Windows\System32`; return r }(),
		"task sid changed":   func() vdRequest { r := orig; r.TaskSID = "S-1-5-18"; return r }(),
	} {
		_, want := write(orig)
		p, _ := write(forged) // the swap between the consent and the read
		got, err := vdReadBoundRequest(p, vdRequestDigest(want))
		if err == nil || !strings.Contains(err.Error(), "nothing was done") || vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("%s: the forged request was accepted: %+v, %v", name, got, err)
		}
		if got != (vdRequest{}) {
			t.Errorf("%s: the forged request was parsed: %+v", name, got)
		}
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s: the forged request file was left behind", name)
		}
	}
	p, _ = write(orig)
	if _, err := vdReadBoundRequest(p, ""); err == nil {
		t.Error("a request with no digest on the command line was accepted")
	}

	// The internal entry point refuses a command line without the digest.
	if err := vdElevatedMain("_detach", []string{p, filepath.Join(dir, "r.json")}); err == nil {
		t.Error("the elevated step ran without a request digest")
	}
	// A mismatch writes a refusal as the result and does nothing else.
	p, _ = write(orig)
	res := filepath.Join(dir, "vd.result.json")
	if err := vdElevatedMain("_detach", []string{p, res, strings.Repeat("0", 64)}); err != nil {
		t.Fatal(err)
	}
	out, err := os.ReadFile(res)
	if err != nil {
		t.Fatal(err)
	}
	var r vdResult
	if json.Unmarshal(out, &r) != nil || r.OK || !strings.Contains(r.Error, "SHA-256") {
		t.Errorf("the result of a forged request: %s", out)
	}
}

// AUD-31-F4 (b): the task's owner is the request's SID, which the digest binds
// to the consent. It is not compared with the elevated token (over-the-shoulder
// elevation must keep working), but it must resolve to a user account.
func TestVD_TaskOwnerMustBeAUserAccount(t *testing.T) {
	own, err := vdCurrentUserSID()
	if err != nil {
		t.Fatal(err)
	}
	if got, err := vdTaskOwnerSID(own); err != nil || got != own {
		t.Errorf("this user's own SID: %q, %v", got, err)
	}
	for name, sid := range map[string]string{
		"empty":                 "",
		"not a SID":             "not-a-sid",
		"no such account":       "S-1-5-21-1-2-3-1001",
		"SYSTEM is not a user":  "S-1-5-18",
		"a group is not a user": "S-1-5-32-544",
	} {
		if got, err := vdTaskOwnerSID(sid); err == nil || !strings.Contains(err.Error(), "no task was created") {
			t.Errorf("%s (%q): got %q, %v", name, sid, got, err)
		}
	}
	// The real step refuses before any XML is built or schtasks runs.
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		r.Containers = append(r.Containers, vdRegEntry{Name: "work", Path: `C:\data\work.fdd`})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := vdTaskStep(vdRequest{TaskName: vdTaskName("work"), TaskSID: "S-1-5-18"}); err == nil || !strings.Contains(err.Error(), "no task was created") {
		t.Errorf("a task for a non-user SID: %v", err)
	}
}

// T3-F6: the elevated step uses the state folder only when it is an existing
// plain folder - not a reparse point. Its owner is not checked: under
// over-the-shoulder elevation it is the standard user's, not the token's.
func TestVD_ElevatedStateDirMustBeAPlainFolder(t *testing.T) {
	dir := t.TempDir()
	if err := vdCheckStateDir(dir); err != nil {
		t.Fatalf("the user's own folder was refused: %v", err)
	}
	if err := vdCheckStateDir("relative\\state"); err == nil {
		t.Error("a relative state folder was accepted")
	}
	if err := vdCheckStateDir(filepath.Join(dir, "missing")); err == nil {
		t.Error("a missing state folder was accepted")
	}
	f := filepath.Join(dir, "file")
	os.WriteFile(f, nil, 0o600)
	if err := vdCheckStateDir(f); err == nil {
		t.Error("a file was accepted as the state folder")
	}
	// A folder another account owns (TrustedInstaller owns %SystemRoot%) is
	// accepted: ownership is not the test.
	if sr := os.Getenv("SystemRoot"); sr != "" {
		if err := vdCheckStateDir(sr); err != nil {
			t.Errorf("a plain folder of another owner was refused: %v", err)
		}
	}
	// A junction is refused, wherever it points.
	target := filepath.Join(dir, "target")
	os.Mkdir(target, 0o700)
	link := filepath.Join(dir, "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v %s", err, out)
	}
	if err := vdCheckStateDir(link); err == nil || !strings.Contains(err.Error(), "link") {
		t.Errorf("a junction as the state folder: %v", err)
	}
}

// AUD-31-F4 (a): the task XML stays readable but cannot be written, replaced,
// renamed or deleted while schtasks reads it, and is gone afterwards.
func TestVD_TaskXMLIsHeldUntilReleased(t *testing.T) {
	dir := t.TempDir()
	data := []byte{0xFF, 0xFE, 'x', 0}
	p, release, err := vdWriteLockedTemp(dir, "filedo-task-*.xml", data)
	if err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p); err != nil || string(b) != string(data) {
		t.Errorf("a reader cannot read the held file: %q, %v", b, err)
	}
	if err := os.WriteFile(p, []byte("<Task>calc</Task>"), 0o600); err == nil {
		t.Error("the held file was rewritten")
	}
	if err := os.Remove(p); err == nil {
		t.Error("the held file was deleted")
	}
	if err := os.Rename(p, p+".old"); err == nil {
		t.Error("the held file was renamed")
	}
	if _, _, err := vdWriteLockedTemp(dir, filepath.Base(p), data); err == nil {
		t.Error("a second file was created at the same name")
	}
	release()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Error("the file outlived its release")
	}
}
