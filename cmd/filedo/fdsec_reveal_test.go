package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Stage S4's exit criterion, as far as a test suite can reach it:
//
//	"A video opens in its registered player and the copy is gone afterwards;
//	a FileDO killed mid-reveal leaves a copy that the next start removes; an
//	executable inside a container is extracted and not launched; no other
//	user of the machine can read the copy while it exists."
//
// Three of the four are here. The hand-off to a real player is suppressed by
// the FILEDO_FDSEC_NO_LAUNCH seam, because a suite that opens real windows on
// a developer's desktop is one nobody runs twice - what is proven here is that
// the copy appears, is read-only, is unreachable by anyone else, and is gone
// afterwards. The fourth, "no other user can read it", needs a second local
// account and is a manual check; what the suite can prove of it - that the
// directory's access list names this user and SYSTEM and nothing else, with
// inheritance off - it proves.

// countRevealSandboxes counts the per-reveal directories currently under the
// sandbox root of a test's working directory.
func countRevealSandboxes(t *testing.T, wd string) int {
	t.Helper()
	entries, err := os.ReadDir(revealRoot(wd))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "rv-") {
			n++
		}
	}
	return n
}

// revealSandboxes lists them.
func revealSandboxes(t *testing.T, wd string) []string {
	t.Helper()
	var out []string
	entries, err := os.ReadDir(revealRoot(wd))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "rv-") {
			out = append(out, filepath.Join(revealRoot(wd), e.Name()))
		}
	}
	return out
}

// revealSecret is the one password this suite packs its fixtures under; every
// later command that has to open them names it rather than copying the text.
const revealSecret = "pw-reveal"

// secureOne packs the working directory's plain.txt and returns the password.
func secureOne(t *testing.T, dir, name string) string {
	t.Helper()
	secret := revealSecret
	if out, code := run(t, dir, name, "secure", "p:"+secret); code != 0 {
		t.Fatalf("setup secure exited %d\n%s", code, out)
	}
	return secret
}

// ---------------------------------------------------------------------------
// The plaintext window opens and closes.
// ---------------------------------------------------------------------------

func TestReveal_WindowOpensAndCloses(t *testing.T) {
	dir, payload := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	out, code := run(t, dir, "plain.fd-sec", "reveal", "p:"+secret)
	if code != 0 {
		t.Fatalf("reveal exited %d, want 0\n%s", code, out)
	}
	// The copy carried the sealed true name, not the container's.
	if !strings.Contains(out, "plain.txt") {
		t.Errorf("the reveal did not report the copy under its true name\n%s", out)
	}
	if !strings.Contains(out, "suppressed by FILEDO_FDSEC_NO_LAUNCH") {
		t.Errorf("the hand-off seam did not engage - this test may have opened a real window\n%s", out)
	}
	// And the window is shut: no sandbox survives the run that made it.
	if n := countRevealSandboxes(t, dir); n != 0 {
		t.Errorf("%d sandbox(es) survived the reveal that created them\n%s", n, out)
	}
	// The container is untouched by a reveal - it only ever reads.
	if _, err := os.Stat(filepath.Join(dir, "plain.fd-sec")); err != nil {
		t.Errorf("the container did not survive its own reveal: %v", err)
	}
	// A reveal is not a restore: it wrote nothing into the working directory.
	// (plain.txt is still there because `secure` without `del` keeps it - so
	// the assertion is that nothing NEW appeared, not that nothing is there.)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		switch e.Name() {
		case "plain.txt", "plain.fd-sec", ".reveal-root", "history.json":
		default:
			t.Errorf("a reveal wrote %s into the working directory", e.Name())
		}
	}
	_ = payload
}

// ---------------------------------------------------------------------------
// -keep, the announced leftover, and mechanism 3 that reclaims it.
// ---------------------------------------------------------------------------

func TestReveal_KeepIsAnnouncedAndSweptAtTheNextStart(t *testing.T) {
	dir, payload := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	out, code := run(t, dir, "plain.fd-sec", "reveal", "p:"+secret, "-keep")
	if code != 0 {
		t.Fatalf("reveal -keep exited %d, want 0\n%s", code, out)
	}
	// Kept - and it said so. A silent leftover would pass a test that only
	// checked the file is still there, which is why this asserts the words.
	if !strings.Contains(out, "-keep:") || !strings.Contains(out, "next FileDO start removes it") {
		t.Errorf("-keep left the copy without announcing it\n%s", out)
	}
	boxes := revealSandboxes(t, dir)
	if len(boxes) != 1 {
		t.Fatalf("-keep left %d sandboxes, want 1\n%s", len(boxes), out)
	}

	// The copy is byte-exact and read-only: there is no writable sandbox.
	copyPath := filepath.Join(boxes[0], "plain.txt")
	got, err := os.ReadFile(copyPath)
	if err != nil {
		t.Fatalf("the kept copy is not readable: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("the revealed copy is not byte-exact: %d bytes, want %d", len(got), len(payload))
	}
	if fi, serr := os.Stat(copyPath); serr == nil && fi.Mode().Perm()&0o200 != 0 {
		t.Errorf("the sandbox copy is writable; spec 8.4 says there is no writable sandbox (mode %v)", fi.Mode())
	}
	if f, oerr := os.OpenFile(copyPath, os.O_WRONLY, 0); oerr == nil {
		f.Close()
		t.Errorf("the sandbox copy opened for writing")
	}

	// Mechanism 3: the next FileDO start of any kind - here a verb that has
	// nothing to do with the feature - reclaims it.
	out2, code2 := run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+revealSecret)
	if code2 != 0 {
		t.Fatalf("the next start exited %d\n%s", code2, out2)
	}
	if !strings.Contains(out2, "Removed a leftover reveal") {
		t.Errorf("the startup sweep did not report reclaiming the leftover\n%s", out2)
	}
	if n := countRevealSandboxes(t, dir); n != 0 {
		t.Errorf("%d leftover(s) survived the startup sweep\n%s", n, out2)
	}
}

// The power-loss case none of the live mechanisms covers: a sandbox that no
// process is watching, because the process that made it is gone. Only the
// sweep reclaims it, and this is the residual window written down as a test.
func TestReveal_PowerLossLeftoverIsOnlyReclaimedBySweep(t *testing.T) {
	dir, _ := workdir(t)
	secureOne(t, dir, "plain.txt")

	// Stand in for the killed FileDO: a sandbox with a copy in it, no lock
	// held, nobody waiting.
	leftover := filepath.Join(revealRoot(dir), "rv-powerloss")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(leftover, "plain.txt"), []byte("plaintext left behind"), 0o444); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+revealSecret)
	if code != 0 {
		t.Fatalf("the next start exited %d\n%s", code, out)
	}
	if _, err := os.Stat(leftover); !os.IsNotExist(err) {
		t.Errorf("the leftover survived the next start (err=%v)\n%s", err, out)
	}
	if !strings.Contains(out, "Removed a leftover reveal") {
		t.Errorf("the sweep did not say what it reclaimed\n%s", out)
	}
}

// The sweep removes reveal sandboxes and nothing else. A directory that does
// not carry the prefix is somebody else's and is left alone.
func TestReveal_SweepTouchesOnlyItsOwn(t *testing.T) {
	dir, _ := workdir(t)
	secureOne(t, dir, "plain.txt")

	stranger := filepath.Join(revealRoot(dir), "not-a-reveal")
	if err := os.MkdirAll(stranger, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stranger, "keep-me.txt"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+revealSecret); code != 0 {
		t.Fatalf("exited %d\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(stranger, "keep-me.txt")); err != nil {
		t.Errorf("the sweep deleted a directory that was not a reveal sandbox: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Invariant 7: an executable inside a container is extracted and not launched.
// ---------------------------------------------------------------------------

func TestReveal_ExecutableIsExtractedAndNeverLaunched(t *testing.T) {
	dir, _ := workdir(t)
	// A container whose sealed true name is an executable type. The bytes do
	// not matter - the refusal is by type, before anything is handed over.
	exePath := filepath.Join(dir, "payload.exe")
	if err := os.WriteFile(exePath, []byte("MZ this is not really a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := secureOne(t, dir, "payload.exe")

	out, code := run(t, dir, "payload.fd-sec", "reveal", "p:"+secret)
	if code != 0 {
		t.Fatalf("reveal of an executable exited %d, want 0 (extracted, not launched)\n%s", code, out)
	}
	if !strings.Contains(out, "NOT launched") || !strings.Contains(out, "invariant 7") {
		t.Errorf("the refusal to launch is not stated in the words the invariant uses\n%s", out)
	}
	// It never reached the hand-off at all: the seam would have said so.
	if strings.Contains(out, "suppressed by FILEDO_FDSEC_NO_LAUNCH") {
		t.Errorf("an executable reached the launch path and was only stopped by the test seam\n%s", out)
	}
	// Extracted and located, so the user has something to act on.
	boxes := revealSandboxes(t, dir)
	if len(boxes) != 1 {
		t.Fatalf("the refused executable left %d sandboxes, want 1 (extracted, not deleted)\n%s", len(boxes), out)
	}
	if !strings.Contains(out, boxes[0]) {
		t.Errorf("the location of the extracted executable was not shown\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(boxes[0], "payload.exe")); err != nil {
		t.Errorf("the executable was not extracted: %v", err)
	}
	// And the next start reclaims it, as it said it would.
	if out2, code2 := run(t, dir, "fdsec", "info", "payload.fd-sec", "p:"+secret); code2 != 0 {
		t.Fatalf("the next start exited %d\n%s", code2, out2)
	}
	if n := countRevealSandboxes(t, dir); n != 0 {
		t.Errorf("%d extracted-executable sandbox(es) survived the next start", n)
	}
}

// Every script type the list names is refused the same way. A type that only
// some installations execute is still refused: a refusal costs one manual
// double-click, a wrong launch costs the machine.
func TestReveal_EveryScriptTypeIsRefused(t *testing.T) {
	for _, name := range []string{"runme.ps1", "runme.bat", "runme.cmd", "runme.vbs", "runme.js", "runme.lnk"} {
		t.Run(name, func(t *testing.T) {
			dir, _ := workdir(t)
			if err := os.WriteFile(filepath.Join(dir, name), []byte("echo nope"), 0o644); err != nil {
				t.Fatal(err)
			}
			secret := secureOne(t, dir, name)
			container := strings.TrimSuffix(name, filepath.Ext(name)) + ".fd-sec"
			out, code := run(t, dir, container, "reveal", "p:"+secret)
			if code != 0 {
				t.Fatalf("exited %d\n%s", code, out)
			}
			if !strings.Contains(out, "NOT launched") {
				t.Errorf("%s was not refused\n%s", name, out)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The options the usage banner advertises.
// ---------------------------------------------------------------------------

func TestReveal_RwIsTheOrdinaryRestore(t *testing.T) {
	dir, payload := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	out, code := run(t, dir, "plain.fd-sec", "reveal", "p:"+secret, "-rw", "to", "mine.txt")
	if code != 0 {
		t.Fatalf("reveal -rw exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "not a writable sandbox") {
		t.Errorf("-rw did not say what it is instead\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "mine.txt"))
	if err != nil {
		t.Fatalf("-rw did not produce the named file: %v\n%s", err, out)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("-rw restore is not byte-exact: %d bytes, want %d", len(got), len(payload))
	}
	// An ordinary owned file, not a read-only sandbox copy.
	f, oerr := os.OpenFile(filepath.Join(dir, "mine.txt"), os.O_WRONLY, 0)
	if oerr != nil {
		t.Errorf("-rw produced a file its owner cannot write: %v", oerr)
	} else {
		f.Close()
	}
	// And it left no sandbox at all, because it never opened one.
	if n := countRevealSandboxes(t, dir); n != 0 {
		t.Errorf("-rw opened %d sandbox(es); it is supposed to open none", n)
	}
}

func TestReveal_OptionsBelongToRevealAlone(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	for _, tc := range []struct{ verb, opt string }{
		{"unsecure", "-rw"}, {"unsecure", "-keep"},
		{"secure", "-keep"},
	} {
		target := "plain.fd-sec"
		if tc.verb == "secure" {
			target = "plain.txt"
		}
		out, code := run(t, dir, target, tc.verb, "p:"+secret, tc.opt)
		if code != 2 {
			t.Errorf("%s %s exited %d, want 2 (usage)\n%s", tc.verb, tc.opt, code, out)
		}
		if !strings.Contains(out, "is a reveal option") {
			t.Errorf("%s %s was not refused as a reveal option\n%s", tc.verb, tc.opt, out)
		}
	}

	if out, code := run(t, dir, "plain.fd-sec", "reveal", "p:"+secret, "-rw", "-keep"); code != 2 {
		t.Errorf("-rw -keep exited %d, want 2\n%s", code, out)
	} else if !strings.Contains(out, "do not combine") {
		t.Errorf("-rw -keep was refused without saying why\n%s", out)
	}
}

// A mask is a usage error, not an I/O error - and for reveal it is refused
// for a second reason that is not ergonomics. Found at S4's end and fixed in
// S3, which owns the other two verbs it affected.
func TestReveal_AMaskIsRefusedAsUsage(t *testing.T) {
	dir, _ := workdir(t)
	secret := secureOne(t, dir, "plain.txt")

	for _, tc := range []struct{ args []string }{
		{[]string{"*.fd-sec", "reveal", "p:" + secret}},
		{[]string{"*.fd-sec", "unsecure", "p:" + secret}},
		{[]string{"fdsec", "verify", "*.fd-sec", "p:" + secret}},
		{[]string{"fdsec", "info", "*.fd-sec"}},
	} {
		out, code := run(t, dir, tc.args...)
		if code != 2 {
			t.Errorf("%v exited %d, want 2 (usage, not I/O)\n%s", tc.args, code, out)
		}
		if !strings.Contains(out, "is a mask") {
			t.Errorf("%v was refused without saying a mask is the problem\n%s", tc.args, out)
		}
		if strings.Contains(out, "volume label syntax") {
			t.Errorf("%v leaked the raw syscall error instead of a usage message\n%s", tc.args, out)
		}
	}

	// And the reveal refusal says the reason that is not ergonomics.
	out, _ := run(t, dir, "*.fd-sec", "reveal", "p:"+secret)
	if !strings.Contains(out, "plaintext copies") {
		t.Errorf("the reveal mask refusal does not say why a batch is not offered\n%s", out)
	}

	// A mask is still exactly how `secure` works - the fix must not have
	// broken Q10.
	if out, code := run(t, dir, "*.txt", "secure", "p:"+secret, "-y"); code != 0 {
		t.Errorf("secure with a mask exited %d; one container per matched file is Q10\n%s", code, out)
	}
}

// ---------------------------------------------------------------------------
// The batch path. The dispatch is shared with the interactive entry, so this
// test exists to keep it shared: an edit that special-cases one of the two
// fails here.
// ---------------------------------------------------------------------------

func TestReveal_FromABatchFile(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "pw-reveal-batch"
	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("setup secure exited %d\n%s", code, out)
	}
	lst := "plain.fd-sec reveal p:" + secret + " -keep\r\n"
	if err := os.WriteFile(filepath.Join(dir, "reveal.lst"), []byte(lst), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, dir, "from", "reveal.lst")
	if code != 0 {
		t.Fatalf("batch reveal exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "1/1 commands succeeded") {
		t.Errorf("the batch reveal did not succeed\n%s", out)
	}
	if n := countRevealSandboxes(t, dir); n != 1 {
		t.Errorf("the batch reveal -keep left %d sandboxes, want 1\n%s", n, out)
	}
	// The credential is redacted in the batch echo for reveal as for every
	// other verb - the redactor already lists it, and this keeps it listed.
	if strings.Contains(out, secret) {
		t.Errorf("the batch echo repeated the reveal credential\n%s", out)
	}
	assertNoSecretOnDisk(t, dir, secret, "reveal.lst")
}

// ---------------------------------------------------------------------------
// The sealed true name must not outlive the reveal. history.json is permanent
// and the name is the one thing the container exists to hide (Q3), so a
// reveal may show it on screen and must not write it to disk.
// ---------------------------------------------------------------------------

func TestReveal_DoesNotWriteTheSealedNameIntoHistory(t *testing.T) {
	dir := t.TempDir()
	const marker = "quarterly-merger-memo.txt"
	const secret = "pw-history"
	if err := os.WriteFile(filepath.Join(dir, marker), []byte("sealed contents"), 0o644); err != nil {
		t.Fatal(err)
	}
	// `rename` gives the container a random name, so the container's own path
	// carries no trace of the true name and the only way the name could reach
	// history.json is if the reveal put it there.
	if out, code := run(t, dir, marker, "secure", "p:"+secret, "rename", "del", "-y"); code != 0 {
		t.Fatalf("secure rename exited %d\n%s", code, out)
	}
	var container string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// `rename` writes a random name with no extension at all, so the
	// container is simply whatever is left that is not the history file.
	for _, e := range entries {
		if e.IsDir() || e.Name() == "history.json" {
			continue
		}
		container = e.Name()
	}
	if container == "" {
		t.Fatalf("no container was produced")
	}
	if strings.Contains(container, "quarterly") {
		t.Fatalf("the container name carries the true name, so this test cannot tell: %s", container)
	}

	if out, code := run(t, dir, container, "reveal", "p:"+secret); code != 0 {
		t.Fatalf("reveal exited %d\n%s", code, out)
	}

	hist, herr := os.ReadFile(filepath.Join(dir, "history.json"))
	if herr != nil {
		t.Fatalf("no history was written, so this test proves nothing: %v", herr)
	}
	// Only the reveal's own entry is at issue. The `secure` entry legitimately
	// carries the name, because the user typed it on the command line - the
	// name was not sealed yet at that point. What must not happen is the
	// reveal putting it back after the container hid it.
	var revealEntry string
	for _, chunk := range strings.Split(string(hist), `"timestamp"`) {
		if strings.Contains(chunk, "fdsec-reveal") {
			revealEntry = chunk
		}
	}
	if revealEntry == "" {
		t.Fatalf("history does not record the reveal at all, so its silence about the name means nothing\n%s", hist)
	}
	if !strings.Contains(revealEntry, container) {
		t.Fatalf("the reveal entry does not name the container it opened\n%s", revealEntry)
	}
	if strings.Contains(revealEntry, "quarterly") {
		t.Errorf("the sealed true name was written to history.json, where it outlives the reveal:\n%s", revealEntry)
	}
}

// ---------------------------------------------------------------------------
// Credential hygiene, re-proven for the new verb.
// ---------------------------------------------------------------------------

func TestReveal_LeavesNoCredentialOnDisk(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "pw-reveal-hygiene"
	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("setup secure exited %d\n%s", code, out)
	}
	pf := filepath.Join(dir, "cred.txt")
	if err := os.WriteFile(pf, []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, "plain.fd-sec", "reveal", "pf:"+pf); code != 0 {
		t.Fatalf("reveal pf: exited %d\n%s", code, out)
	}
	if out, code := run(t, dir, "plain.fd-sec", "reveal", "p:"+secret); code != 0 {
		t.Fatalf("reveal p: exited %d\n%s", code, out)
	}
	assertNoSecretOnDisk(t, dir, secret, "cred.txt")
}
