package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// SP-0008's acceptance criteria, against the shipped exe.
//
// The spec's complaint is a location and a lifetime: `unsecure start` used to
// write the decrypted original beside the container - on a share, in a
// Downloads folder another account can browse - and leave it there for good.
// Every test here plants that defect and shows the guard firing.
//
// The hand-off to the machine's real handler is suppressed by
// FILEDO_FDSEC_NO_LAUNCH, which is also what stands in for "a handler that
// does not lock the file": nothing holds the copy when the removal attempt
// runs, so the attempt succeeds and AC4 is a real assertion rather than a
// hopeful one. The locking case is the Windows half of this suite.

// startSandboxes lists the us- directories under a test's sandbox root.
func startSandboxes(t *testing.T, wd string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(revealRoot(wd))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), fdsecStartDirPrefix) {
			out = append(out, filepath.Join(revealRoot(wd), e.Name()))
		}
	}
	return out
}

// AC1 and AC4: the copy is written into the protected root, never beside the
// container, and it is gone once the run's cleanup attempt has made its one
// try.
func TestUnsecureStart_RestoresIntoTheSandboxAndTakesItAwayAgain(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")
	// The original goes out of the way, so anything named plain.txt found
	// beside the container afterwards was written by the run under test.
	if err := os.Remove(filepath.Join(dir, "plain.txt")); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "plain.fd-sec", "unsecure", "start", "p:"+secret)
	if code != 0 {
		t.Fatalf("unsecure start exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "suppressed by FILEDO_FDSEC_NO_LAUNCH") {
		t.Errorf("the hand-off seam did not engage - this test may have opened a real window\n%s", out)
	}

	// AC1, the location, read from what the run itself reported.
	wantPrefix := revealRoot(dir) + string(os.PathSeparator) + fdsecStartDirPrefix
	if !strings.Contains(out, wantPrefix) {
		t.Errorf("the restored copy was not written under %s*\n%s", wantPrefix, out)
	}
	if exists(filepath.Join(dir, "plain.txt")) {
		t.Errorf("the restored original was written beside the container - the defect SP-0008 exists to close\n%s", out)
	}

	// AC4, the lifetime: nothing holds the copy, so the one attempt works.
	if left := startSandboxes(t, dir); len(left) != 0 {
		t.Errorf("%d sandbox(es) survived the run that created them: %v\n%s", len(left), left, out)
	}
	// And the container is untouched: a start reads it, nothing more.
	if !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Error("the container did not survive its own unsecure start")
	}
}

// The same command from a .lst batch file. The two dispatches are separate
// code paths in this program, so a feature proved only interactively is a
// feature that can be silently missing from every automated run.
func TestUnsecureStart_FromABatchFile(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "s3cr3t-batch-start"
	lst := strings.Join([]string{
		"plain.txt secure p:" + secret,
		"plain.fd-sec unsecure start p:" + secret,
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "work.lst"), []byte(lst), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "from", "work.lst")
	if code != 0 {
		t.Fatalf("batch run exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "2/2 commands succeeded") {
		t.Errorf("not every batch command succeeded\n%s", out)
	}
	if left := startSandboxes(t, dir); len(left) != 0 {
		t.Errorf("a batch line left %d sandbox(es) behind: %v\n%s", len(left), left, out)
	}
	assertNoSecretOnDisk(t, dir, secret, "work.lst")
	assertNoPartials(t, dir)
}

// AC7, the regression guard: plain `unsecure` keeps its own contract - the
// true name, beside the container, permanently, and no sandbox anywhere.
func TestUnsecureStart_PlainUnsecureIsUnchanged(t *testing.T) {
	dir, payload := workdir(t)
	secret := secureOne(t, dir, "plain.txt")
	if err := os.Remove(filepath.Join(dir, "plain.txt")); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret)
	if code != 0 {
		t.Fatalf("unsecure exited %d, want 0\n%s", code, out)
	}
	restored := filepath.Join(dir, "plain.txt")
	if !exists(restored) {
		t.Fatalf("plain unsecure no longer restores beside the container\n%s", out)
	}
	if got := mustRead(t, restored); !bytes.Equal(got, payload) {
		t.Errorf("the restore is not byte-exact: %d bytes back, want %d", len(got), len(payload))
	}
	if left := startSandboxes(t, dir); len(left) != 0 {
		t.Errorf("plain unsecure opened a sandbox it has no business opening: %v\n%s", left, out)
	}
}

// A destination the user named is the user asking for a permanent file in a
// place they chose, so `to` and `here` keep the ordinary restore and only the
// default location moves into the sandbox.
func TestUnsecureStart_ANamedDestinationKeepsTheOrdinaryRestore(t *testing.T) {
	dir, payload := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	out, code := run(t, dir, "plain.fd-sec", "unsecure", "to", "kept.bin", "start", "p:"+secret)
	if code != 0 {
		t.Fatalf("unsecure to .. start exited %d, want 0\n%s", code, out)
	}
	kept := filepath.Join(dir, "kept.bin")
	if !exists(kept) {
		t.Fatalf("the named destination was not written\n%s", out)
	}
	if got := mustRead(t, kept); !bytes.Equal(got, payload) {
		t.Errorf("the named destination is not byte-exact: %d bytes back, want %d", len(got), len(payload))
	}
	if left := startSandboxes(t, dir); len(left) != 0 {
		t.Errorf("a named destination still opened a sandbox: %v\n%s", left, out)
	}
	if !strings.Contains(out, "suppressed by FILEDO_FDSEC_NO_LAUNCH") {
		t.Errorf("the file was not handed to a handler at all\n%s", out)
	}
}

// del removes the container. With the copy now living in a sandbox that the
// end of the run takes away, the two together would leave the user with
// neither - so the combination is refused as the usage error it is, and the
// refusal names the form that does what they meant.
func TestUnsecureStart_RefusesToDeleteTheContainerItOnlyBorrowed(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	out, code := run(t, dir, "plain.fd-sec", "unsecure", "start", "del", "-y", "p:"+secret)
	if code != 2 {
		t.Fatalf("unsecure start del exited %d, want 2 (usage)\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Error("the refused command removed the container anyway")
	}
	if !strings.Contains(out, "to <dest> start del") {
		t.Errorf("the refusal does not name the form that keeps the file\n%s", out)
	}
}

// AC6: history.json is a permanent file on the user's disk and the true name
// is the one thing the container exists to hide, so a start logs the sandbox
// directory and never what is inside it.
func TestUnsecureStart_HistoryCarriesTheDirectoryNeverTheName(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")
	// The container is renamed, so the only thing that could put the true
	// name in the log is the log line itself - not the command line.
	if err := os.Rename(filepath.Join(dir, "plain.fd-sec"), filepath.Join(dir, "box.bin")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "plain.txt")); err != nil {
		t.Fatal(err)
	}
	// Everything logged so far is setup; the assertion is about this run.
	os.Remove(filepath.Join(dir, "history.json"))

	out, code := run(t, dir, "box.bin", "unsecure", "start", "p:"+secret)
	if code != 0 {
		t.Fatalf("unsecure start exited %d, want 0\n%s", code, out)
	}
	history := string(mustRead(t, filepath.Join(dir, "history.json")))
	if strings.Contains(history, "plain.txt") {
		t.Errorf("history.json carries the sealed true name:\n%s", history)
	}
	if !strings.Contains(history, fdsecStartDirPrefix) {
		t.Errorf("history.json does not record the sandbox the copy lived in:\n%s", history)
	}
}

// AC3: the sealed name decides what is written and, through its extension,
// what the shell is then handed - and it is attacker-controlled the moment
// somebody else built the container. The screen is reveal's, exercised here
// through its second caller.
func TestUnsecureStart_RefusesASealedNameThatWouldEscapeOrMislead(t *testing.T) {
	sb := &fdsecStartSandbox{dir: t.TempDir()}
	for _, tc := range []struct{ name, why string }{
		{`..\..\Windows\System32\evil.dll`, "a path with parent references"},
		{`C:\Windows\evil.dll`, "an absolute path"},
		{`sub/dir/evil.txt`, "a forward-slash path"},
		{`notes.txt:payload.exe`, "an alternate data stream"},
		{`invoice.exe `, "a trailing space Windows drops"},
		{`invoice.exe.`, "a trailing dot Windows drops"},
		{`CON`, "a reserved device name"},
		{"bad\rname.txt", "a control character"},
		{``, "an empty name"},
	} {
		if got, err := sb.path(tc.name, false); err == nil {
			t.Errorf("a start accepted %q as %s; want a refusal - %s", tc.name, got, tc.why)
		}
	}
	// And an ordinary name lands inside the sandbox and nowhere else.
	got, err := sb.path("holiday.mp4", false)
	if err != nil {
		t.Fatalf("an ordinary sealed name was refused: %v", err)
	}
	if want := filepath.Join(sb.dir, "holiday.mp4"); got != want {
		t.Errorf("the copy would be written to %s, want %s", got, want)
	}
}

// 4.4's backstop, from the outside: a sandbox whose run could not remove it -
// because the handler still had the file, or because the power went out - is
// reclaimed by the next FileDO start, whatever that start was asked to do.
func TestUnsecureStart_ALeftoverIsReclaimedByTheNextStart(t *testing.T) {
	dir, _ := workdir(t)
	leftover := filepath.Join(revealRoot(dir), fdsecStartDirPrefix+"leftover")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "plain.txt"), []byte("plaintext"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Something that is not ours, under the same root. The sweep removes
	// what carries one of the two prefixes and nothing else.
	bystander := filepath.Join(revealRoot(dir), "not-ours")
	if err := os.MkdirAll(bystander, 0o700); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "file", "plain.txt", "info")
	if code != 0 {
		t.Fatalf("the next start exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "Removed a leftover unsecure-start copy") {
		t.Errorf("the next start did not reclaim the leftover copy\n%s", out)
	}
	if exists(leftover) {
		t.Errorf("the leftover sandbox survived the next start\n%s", out)
	}
	if !exists(bystander) {
		t.Errorf("the sweep removed a directory that is not FileDO's\n%s", out)
	}
}
