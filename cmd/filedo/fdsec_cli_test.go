package main

// Black-box acceptance tests for the fdsec command surface (PLAN stage S3).
// They drive a freshly built filedo.exe, because three of the things the exit
// criterion asks about are only observable from outside the process: the exit
// code, what lands in history.json, and whether a verb that works
// interactively also works from a .lst batch file.
//
// Run them with:  go test ./cmd/filedo/ -count=1 -vet=off
// (-vet=off because package main carries pre-existing vet debt - see
// AGENTS.md "Testing"; the debt is unrelated to these tests.)

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filedo/fdsec"
)

var filedoExe string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "fdsec-cli-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot create the build directory:", err)
		os.Exit(2)
	}
	filedoExe = filepath.Join(dir, "filedo_test_build.exe")
	build := exec.Command("go", "build", "-o", filedoExe, ".")
	if out, berr := build.CombinedOutput(); berr != nil {
		fmt.Fprintf(os.Stderr, "cannot build filedo: %v\n%s", berr, out)
		os.RemoveAll(dir)
		os.Exit(2)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// revealRoot is the sandbox root a test's filedo runs use: one per working
// directory, so a test never sweeps another's sandbox and nothing lands in the
// developer's real profile.
func revealRoot(wd string) string { return filepath.Join(wd, ".reveal-root") }

// run executes filedo in wd with stdin closed - the non-interactive shape the
// batch path and any automation see. It returns the combined output and the
// exit code. Two seams are set for every run: the reveal sandbox root is
// moved into wd, and the hand-off to the machine's registered handler is
// suppressed, because a test suite that opens real player and editor windows
// on the developer's desktop is a test suite nobody runs twice.
func run(t *testing.T, wd string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = wd
	cmd.Env = append(os.Environ(),
		"FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(wd),
	)
	cmd.Stdin = strings.NewReader("")
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running filedo %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// workdir builds a scratch directory holding one file of known bytes.
func workdir(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	payload := make([]byte, 40000)
	for i := range payload {
		payload[i] = byte(i%251) ^ 0x5a
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.txt"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, payload
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return b
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// assertNoSecretOnDisk sweeps every file under dir - history.json included -
// for the secret's bytes. Safety invariant 8: a credential must not survive a
// run anywhere the user's disk can be read later. The user's own
// credential-bearing files are excluded by name: a password file, a keyfile
// and a .lst batch script hold the secret because the user put it there.
func assertNoSecretOnDisk(t *testing.T, dir, secret string, userOwned ...string) {
	t.Helper()
	owned := map[string]bool{}
	for _, n := range userOwned {
		owned[n] = true
	}
	needle := []byte(secret)
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if owned[info.Name()] {
			return nil
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil // a file we cannot read cannot be asserted about
		}
		if bytes.Contains(b, needle) {
			t.Errorf("the credential %q appears in %s", secret, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// assertNoPartials proves nothing half-written survived a run.
func assertNoPartials(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		n := e.Name()
		if strings.Contains(n, ".fdsec-partial-") || strings.HasPrefix(n, ".fdsec-restore-") {
			t.Errorf("a partial file survived: %s", n)
		}
	}
}

// ---------------------------------------------------------------------------
// The exit criterion, part 1: every verb works from a .lst batch file as well
// as interactively. A test that only exercised the interactive path could not
// see a missing batch-dispatch edit (S3 section 2).
// ---------------------------------------------------------------------------

func TestFdsecEveryVerbFromBatchFile(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "s3cr3t-from-the-batch-file"
	lst := strings.Join([]string{
		"plain.txt secure p:" + secret,
		"fdsec info plain.fd-sec p:" + secret,
		"fdsec verify plain.fd-sec p:" + secret,
		"plain.fd-sec unsecure p:" + secret + " to restored.txt",
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "work.lst"), []byte(lst), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "from", "work.lst")
	if code != 0 {
		t.Fatalf("batch run exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "4/4 commands succeeded") {
		t.Errorf("not every batch command succeeded\n%s", out)
	}
	if got := mustRead(t, filepath.Join(dir, "restored.txt")); !bytes.Equal(got, payload) {
		t.Errorf("the batch round trip is not byte-exact: %d bytes back, want %d", len(got), len(payload))
	}

	// The batch echo repeats the command line, so it is redacted too.
	if strings.Contains(out, secret) {
		t.Errorf("the credential was echoed by the batch runner\n%s", out)
	}
	if !strings.Contains(out, "p:***") {
		t.Errorf("the echo did not keep the redacted shape p:***\n%s", out)
	}
	if n := strings.Count(out, "p:***"); n != 4 {
		t.Errorf("only %d of the 4 credential-bearing batch lines were redacted\n%s", n, out)
	}
	assertNoSecretOnDisk(t, dir, secret, "work.lst")
	assertNoPartials(t, dir)
}

func TestFdsecSameVerbsInteractively(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "s3cr3t-typed-by-hand"

	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("secure exited %d, want 0\n%s", code, out)
	}
	if out, code := run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+secret); code != 0 {
		t.Fatalf("fdsec info exited %d, want 0\n%s", code, out)
	}
	if out, code := run(t, dir, "fdsec", "verify", "plain.fd-sec", "p:"+secret); code != 0 {
		t.Fatalf("fdsec verify exited %d, want 0\n%s", code, out)
	}
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret, "to", "restored.txt"); code != 0 {
		t.Fatalf("unsecure exited %d, want 0\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "restored.txt")); !bytes.Equal(got, payload) {
		t.Error("the interactive round trip is not byte-exact")
	}

	// The documented type prefix reaches the same dispatch.
	if out, code := run(t, dir, "file", "plain.txt", "secure", "p:"+secret, "to", "prefixed.fd-sec"); code != 0 {
		t.Fatalf("file <target> secure exited %d, want 0\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "prefixed.fd-sec")) {
		t.Error("the file-prefixed form did not produce a container")
	}
	assertNoSecretOnDisk(t, dir, secret)
	assertNoPartials(t, dir)
}

// ---------------------------------------------------------------------------
// The exit criterion, part 2: no credential appears anywhere after a run that
// used every credential source.
// ---------------------------------------------------------------------------

func TestFdsecCredentialHygieneAcrossEverySource(t *testing.T) {
	const (
		fileSecret = "pw-file-source-7f3a"
		envSecret  = "pw-env-source-91bd"
		argSecret  = "pw-arg-source-2c6e"
		bareSecret = "pw-bare-source-4d81"
		keyBytes   = "key-file-bytes-e5a0-not-a-password-but-still-secret"
	)

	sources := []struct {
		name    string
		secrets []string
		args    func(dir string) []string
	}{
		{"argument", []string{argSecret}, func(string) []string { return []string{"p:" + argSecret} }},
		{"bare", []string{bareSecret}, func(string) []string { return []string{bareSecret} }},
		{"password file", []string{fileSecret}, func(dir string) []string { return []string{"pf:pw.txt"} }},
		{"environment", []string{envSecret}, func(string) []string { return []string{"pe:FDSEC_TEST_PW"} }},
		{"keyfile", []string{keyBytes}, func(string) []string { return []string{"k:key.bin"} }},
	}

	for _, s := range sources {
		t.Run(s.name, func(t *testing.T) {
			dir, payload := workdir(t)
			if err := os.WriteFile(filepath.Join(dir, "pw.txt"), []byte(fileSecret+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "key.bin"), []byte(keyBytes), 0o644); err != nil {
				t.Fatal(err)
			}

			cred := s.args(dir)
			pack := exec.Command(filedoExe, append([]string{"plain.txt", "secure"}, cred...)...)
			pack.Dir = dir
			pack.Stdin = strings.NewReader("")
			pack.Env = append(os.Environ(), "FDSEC_TEST_PW="+envSecret)
			packOut, err := pack.CombinedOutput()
			if err != nil {
				t.Fatalf("secure via %s failed: %v\n%s", s.name, err, packOut)
			}

			// The restore takes no options at all, because a bare password is
			// only valid as the sole trailing token - so every source is
			// exercised through the same, narrowest grammar. The original is
			// removed first, so the container restores onto its own true name.
			if rerr := os.Remove(filepath.Join(dir, "plain.txt")); rerr != nil {
				t.Fatal(rerr)
			}
			unpack := exec.Command(filedoExe, append([]string{"plain.fd-sec", "unsecure"}, cred...)...)
			unpack.Dir = dir
			unpack.Stdin = strings.NewReader("")
			unpack.Env = append(os.Environ(), "FDSEC_TEST_PW="+envSecret)
			unpackOut, err := unpack.CombinedOutput()
			if err != nil {
				t.Fatalf("unsecure via %s failed: %v\n%s", s.name, err, unpackOut)
			}
			if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
				t.Errorf("the %s round trip is not byte-exact", s.name)
			}

			console := string(packOut) + string(unpackOut)
			for _, secret := range s.secrets {
				if strings.Contains(console, secret) {
					t.Errorf("the credential leaked to the console via %s\n%s", s.name, console)
				}
			}
			// The password file and the keyfile are the user's own files and
			// legitimately hold the secret; everything else must not.
			for _, secret := range s.secrets {
				assertNoSecretInHistory(t, dir, secret)
			}
			assertNoPartials(t, dir)
		})
	}
}

func assertNoSecretInHistory(t *testing.T, dir, secret string) {
	t.Helper()
	path := filepath.Join(dir, "history.json")
	if !exists(path) {
		t.Fatalf("history.json was not written; the hygiene assertion would be vacuous")
	}
	if bytes.Contains(mustRead(t, path), []byte(secret)) {
		t.Errorf("the credential %q reached history.json", secret)
	}
	// The source path stays: a history entry that cannot name what it did is
	// useless, and a path is not the secret.
	if !bytes.Contains(mustRead(t, path), []byte("plain")) {
		t.Errorf("redaction swallowed the target as well as the credential:\n%s", mustRead(t, path))
	}
}

func TestFdsecNoPromptWhenStdinIsNotATerminal(t *testing.T) {
	dir, _ := workdir(t)
	out, code := run(t, dir, "plain.txt", "secure")
	if code != 2 {
		t.Errorf("a missing credential off a terminal exited %d, want 2 (usage)\n%s", code, out)
	}
	for _, form := range []string{"p:", "pf:", "pe:", "k:"} {
		if !strings.Contains(out, form) {
			t.Errorf("the refusal does not name the %s form\n%s", form, out)
		}
	}
	if exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Error("a container was written although no credential was available")
	}
}

// ---------------------------------------------------------------------------
// The exit criterion, part 3: no path removes an original that has not been
// read back to its own digest.
// ---------------------------------------------------------------------------

func TestFdsecDeleteOnlyAfterTheReadBack(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-delete-after-readback"

	out, code := run(t, dir, "plain.txt", "secure", "del", "-y", "p:"+secret)
	if code != 0 {
		t.Fatalf("secure del -y exited %d, want 0\n%s", code, out)
	}
	if exists(filepath.Join(dir, "plain.txt")) {
		t.Fatal("the original survived a successful secure del")
	}
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret); code != 0 {
		t.Fatalf("unsecure after the delete exited %d, want 0\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Error("the file restored after secure del is not the original")
	}
	assertNoPartials(t, dir)
}

// A pack that fails must leave the original where it was, and -y must not
// change that. The other half of this guarantee - that PackFile itself
// refuses to return a container whose read-back failed - is proven by
// TestPackFileReadBackGuard in the fdsec package, with the read-back made to
// fail on purpose.
func TestFdsecFailedPackKeepsTheOriginalEvenUnderForce(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-failed-pack"
	dest := filepath.Join("no-such-folder", "out.fd-sec")

	out, code := run(t, dir, "plain.txt", "secure", "del", "-y", "to", dest, "p:"+secret)
	if code == 0 {
		t.Fatalf("secure into a missing parent folder succeeded\n%s", out)
	}
	if !exists(filepath.Join(dir, "plain.txt")) {
		t.Fatal("the original was removed although the pack failed")
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Error("the original was modified although the pack failed")
	}
	if !strings.Contains(out, "refusing to invent a folder") {
		t.Errorf("the refusal does not say a folder was not invented\n%s", out)
	}
	assertNoPartials(t, dir)
}

func TestFdsecWipePrintsTheHonestCaveatAndRemoves(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-wipe-caveat"

	out, code := run(t, dir, "plain.txt", "secure", "wipe", "-y", "p:"+secret)
	if code != 0 {
		t.Fatalf("secure wipe -y exited %d, want 0\n%s", code, out)
	}
	// -y skips the prompt, never the caveat: the honest limit of an
	// overwrite-in-place is information the automating user still needs.
	if !strings.Contains(out, "does not guarantee erasure") {
		t.Errorf("the SSD/COW caveat was skipped along with the prompt\n%s", out)
	}
	if exists(filepath.Join(dir, "plain.txt")) {
		t.Fatal("the original survived secure wipe -y")
	}
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret); code != 0 {
		t.Fatalf("unsecure after the wipe exited %d, want 0\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Error("the file restored after secure wipe is not the original")
	}
}

func TestFdsecWipeWithoutForceKeepsTheOriginalOffATerminal(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-wipe-unconfirmed"

	out, code := run(t, dir, "plain.txt", "secure", "wipe", "p:"+secret)
	if code != 0 {
		t.Fatalf("secure wipe exited %d, want 0\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "plain.txt")) {
		t.Fatal("the original was wiped although WIPE was never typed")
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Error("the original was modified although WIPE was never typed")
	}
	if !strings.Contains(out, "Original kept") {
		t.Errorf("the run does not say the original was kept\n%s", out)
	}
}

func TestFdsecUnsecureDelKeepsTheContainerOnAWrongCredential(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "pw-right-one"

	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("secure exited %d, want 0\n%s", code, out)
	}
	before := mustRead(t, filepath.Join(dir, "plain.fd-sec"))

	out, code := run(t, dir, "plain.fd-sec", "unsecure", "del", "-y", "p:definitely-wrong")
	if code != 3 {
		t.Errorf("a wrong credential exited %d, want 3\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Fatal("the container was deleted after a failed restore")
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "plain.fd-sec")), before) {
		t.Error("the container changed after a failed restore")
	}
	assertNoPartials(t, dir)
}

// "Unsecure and start" restores the file, hands it to its registered handler
// (suppressed by the FILEDO_FDSEC_NO_LAUNCH seam here, the same as reveal's
// launch tests), and leaves the container in place - start never deletes it.
//
// Where that copy is written, and how long it stays, is SP-0008's subject and
// fdsec_start_test.go's: since that change the copy lives in a protected
// sandbox and is removed when the run ends, so what this test still owns is
// the hand-off and the container - and the byte-exactness of a start's
// restore is proved through a named destination, which is the one form that
// keeps the file.
func TestFdsecUnsecureStartLaunchesTheRestoredFile(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-unsecure-start"

	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("secure exited %d, want 0\n%s", code, out)
	}
	if err := os.Remove(filepath.Join(dir, "plain.txt")); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "plain.fd-sec", "unsecure", "start", "p:"+secret)
	if code != 0 {
		t.Fatalf("unsecure start exited %d, want 0\n%s", code, out)
	}
	if exists(filepath.Join(dir, "plain.txt")) {
		t.Errorf("unsecure start wrote the decrypted original beside the container\n%s", out)
	}
	if !strings.Contains(out, "suppressed by FILEDO_FDSEC_NO_LAUNCH") {
		t.Errorf("unsecure start did not attempt a hand-off to a registered handler\n%s", out)
	}
	if !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Error("unsecure start removed the container; start does not imply del")
	}

	// The same command with a destination of its own: that copy is the
	// user's, kept, and it is where the round trip can still be checked.
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "to", "kept.bin", "start", "p:"+secret); code != 0 {
		t.Fatalf("unsecure to .. start exited %d, want 0\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "kept.bin")); !bytes.Equal(got, payload) {
		t.Error("the file restored by unsecure start is not the original")
	}
}

// start is meaningless outside unsecure: secure has no restored file to hand
// off, and reveal already launches (or refuses to, for an executable) on its
// own.
func TestFdsecStartOptionRejectedOutsideUnsecure(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "pw-start-elsewhere"

	out, code := run(t, dir, "plain.txt", "secure", "start", "p:"+secret)
	if code != 2 {
		t.Errorf("secure start exited %d, want 2 (usage)\n%s", code, out)
	}
	if !strings.Contains(out, "start is an unsecure option") {
		t.Errorf("the refusal does not name why start was rejected\n%s", out)
	}
}

func TestFdsecNeverClobbersAnExistingName(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "pw-no-clobber"

	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("first secure exited %d\n%s", code, out)
	}
	first := mustRead(t, filepath.Join(dir, "plain.fd-sec"))

	out, code := run(t, dir, "plain.txt", "secure", "-y", "p:"+secret)
	if code != 0 {
		t.Fatalf("second secure exited %d, want 0\n%s", code, out)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "plain.fd-sec")), first) {
		t.Error("the existing container was overwritten under -y")
	}
	if !exists(filepath.Join(dir, "plain-1.fd-sec")) {
		t.Errorf("the second container did not take a suffixed name\n%s", out)
	}
}

func TestFdsecRenameWritesANamelessBlobThatStillRestores(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-nameless-blob"

	out, code := run(t, dir, "plain.txt", "secure", "rename", "p:"+secret)
	if code != 0 {
		t.Fatalf("secure rename exited %d, want 0\n%s", code, out)
	}
	var blob string
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == "" && e.Name() != "plain.txt" {
			blob = e.Name()
		}
	}
	if blob == "" {
		t.Fatalf("no extensionless container was written\n%s", out)
	}
	if len(blob) != 24 {
		t.Errorf("the random name is %d characters, want 24: %q", len(blob), blob)
	}
	// unsecure on a blob is explicit-verb-only; it must still work.
	if out, code := run(t, dir, blob, "unsecure", "p:"+secret, "to", "restored.txt"); code != 0 {
		t.Fatalf("unsecure on the blob exited %d, want 0\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "restored.txt")); !bytes.Equal(got, payload) {
		t.Error("the blob did not restore byte-exactly")
	}
}

func TestFdsecMaskPacksOneContainerPerFile(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.log"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("body of "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out, code := run(t, dir, "*.txt", "secure", "p:pw-mask")
	if code != 0 {
		t.Fatalf("a mask target exited %d, want 0\n%s", code, out)
	}
	for _, want := range []string{"a.fd-sec", "b.fd-sec"} {
		if !exists(filepath.Join(dir, want)) {
			t.Errorf("the mask did not produce %s\n%s", want, out)
		}
	}
	if exists(filepath.Join(dir, "c.fd-sec")) {
		t.Error("the mask packed a file it does not match")
	}
	assertNoPartials(t, dir)
}

// ---------------------------------------------------------------------------
// Refusals and the exit-code taxonomy. A wrong credential is class A and is
// never reported as damage (FD-SEC-CONTRACT.md section 7.1).
// ---------------------------------------------------------------------------

func TestFdsecRefusalsAndExitCodes(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		t.Helper()
		dir, _ := workdir(t)
		const secret = "pw-exit-codes"
		if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
			t.Fatalf("setup secure exited %d\n%s", code, out)
		}
		return dir, secret
	}

	t.Run("wrong credential is 3, never damage", func(t *testing.T) {
		dir, _ := setup(t)
		out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:not-it")
		if code != 3 {
			t.Errorf("exit %d, want 3\n%s", code, out)
		}
		if strings.Contains(strings.ToLower(out), "damaged") {
			t.Errorf("a wrong credential was reported as damage\n%s", out)
		}
	})

	t.Run("an unsupported suite-3 container is 6 naming FDSEC and update", func(t *testing.T) {
		dir, secret := setup(t)
		path := filepath.Join(dir, "plain.fd-sec")
		origBytes := mustRead(t, path)
		suite3Bytes, err := fdsec.ForgeSuiteForTest(origBytes, fdsec.NewCredential(secret), 3)
		if err != nil {
			t.Fatal(err)
		}
		suite3Path := filepath.Join(dir, "suite3.fd-sec")
		if err := os.WriteFile(suite3Path, suite3Bytes, 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, "suite3.fd-sec", "unsecure", "p:"+secret)
		if code != 6 {
			t.Errorf("exit %d, want 6 (unsupported)\n%s", code, out)
		}
		if !strings.Contains(out, "FDSEC") || !strings.Contains(out, "update") {
			t.Errorf("unsupported output %q does not contain FDSEC and update", out)
		}
	})

	t.Run("a container with forged path-traversing name is 4, does not escape", func(t *testing.T) {
		dir, secret := setup(t)
		path := filepath.Join(dir, "plain.fd-sec")
		origBytes := mustRead(t, path)
		forgedBytes, err := fdsec.ForgeMetadataNameForTest(origBytes, fdsec.NewCredential(secret), `..\escaped.txt`)
		if err != nil {
			t.Fatal(err)
		}
		forgedPath := filepath.Join(dir, "forged.fd-sec")
		if err := os.WriteFile(forgedPath, forgedBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		escapedTarget := filepath.Join(filepath.Dir(dir), "escaped.txt")
		os.Remove(escapedTarget)
		defer os.Remove(escapedTarget)

		out, code := run(t, dir, "forged.fd-sec", "unsecure", "p:"+secret)
		if code != 4 {
			t.Errorf("exit %d, want 4 (damaged)\n%s", code, out)
		}
		if exists(escapedTarget) {
			t.Errorf("forged sealed name escaped to parent directory: %s", escapedTarget)
		}
		assertNoPartials(t, dir)
	})

	t.Run("a tampered container is 3", func(t *testing.T) {
		dir, secret := setup(t)
		path := filepath.Join(dir, "plain.fd-sec")
		b := mustRead(t, path)
		b[len(b)/2] ^= 0xff
		if err := os.WriteFile(path, b, 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret)
		if code != 3 {
			t.Errorf("exit %d, want 3 (credential-or-tamper)\n%s", code, out)
		}
	})

	t.Run("a truncated container is 4", func(t *testing.T) {
		dir, secret := setup(t)
		path := filepath.Join(dir, "plain.fd-sec")
		b := mustRead(t, path)
		if err := os.WriteFile(path, b[:len(b)/3], 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret)
		if code != 4 {
			t.Errorf("exit %d, want 4 (damaged)\n%s", code, out)
		}
	})

	// A container carries no marker, so "this is not one of ours" can only be
	// said from the length - and only when the length makes it impossible.
	t.Run("a file that cannot be a container by length is 4", func(t *testing.T) {
		dir, _ := setup(t)
		path := filepath.Join(dir, "stranger.fd-sec")
		if err := os.WriteFile(path, []byte("this is not a container at all"), 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, "stranger.fd-sec", "unsecure", "p:whatever")
		if code != 4 {
			t.Errorf("exit %d, want 4 (damaged: impossible length)\n%s", code, out)
		}
	})

	t.Run("a stranger of a plausible length is 3, and claims nothing", func(t *testing.T) {
		dir, _ := setup(t)
		path := filepath.Join(dir, "plausible.fd-sec")
		blob := make([]byte, 12288) // a whole number of clusters, big enough
		for i := range blob {
			blob[i] = byte(i * 31)
		}
		if err := os.WriteFile(path, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, "plausible.fd-sec", "unsecure", "p:whatever")
		if code != 3 {
			t.Errorf("exit %d, want 3 (credential, not a container, or tampering - one outcome)\n%s", code, out)
		}
		if !strings.Contains(out, "not a container") {
			t.Errorf("the message does not offer the reading that it never was a container\n%s", out)
		}
	})

	t.Run("reveal with a wrong credential is 3, not damage", func(t *testing.T) {
		dir, _ := setup(t)
		out, code := run(t, dir, "plain.fd-sec", "reveal", "p:not-the-password")
		if code != 3 {
			t.Errorf("exit %d, want 3 (credential or tamper)\n%s", code, out)
		}
		if n := countRevealSandboxes(t, dir); n != 0 {
			t.Errorf("a failed reveal left %d sandbox(es) behind\n%s", n, out)
		}
	})

	t.Run("re-packing a container is refused", func(t *testing.T) {
		dir, secret := setup(t)
		out, code := run(t, dir, "plain.fd-sec", "secure", "p:"+secret)
		if code == 0 {
			t.Errorf("a container was packed again\n%s", out)
		}
		if !strings.Contains(out, "already a .fd-sec container") {
			t.Errorf("the refusal does not say why\n%s", out)
		}
	})

	t.Run("a folder is refused and the vault is named", func(t *testing.T) {
		dir, _ := setup(t)
		sub := filepath.Join(dir, "subdir")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		out, code := run(t, dir, "subdir", "secure", "p:pw")
		if code == 0 {
			t.Errorf("a folder was accepted as a source\n%s", out)
		}
		if !strings.Contains(out, "vault") {
			t.Errorf("the refusal does not name the alternative\n%s", out)
		}
	})

	t.Run("rename and to do not combine", func(t *testing.T) {
		dir, secret := setup(t)
		out, code := run(t, dir, "plain.txt", "secure", "rename", "to", "x.fd-sec", "p:"+secret)
		if code != 2 {
			t.Errorf("exit %d, want 2 (usage)\n%s", code, out)
		}
	})

	t.Run("a bare password beside an option is a usage error", func(t *testing.T) {
		dir, _ := setup(t)
		out, code := run(t, dir, "plain.txt", "secure", "rename", "some-password")
		if code != 2 {
			t.Errorf("exit %d, want 2 (usage)\n%s", code, out)
		}
		if !strings.Contains(out, "p:<password>") {
			t.Errorf("the refusal does not name the p: form\n%s", out)
		}
	})

	t.Run("a verb with no target is a usage error, not silence", func(t *testing.T) {
		dir, _ := setup(t)
		out, code := run(t, dir, "secure")
		if code != 2 {
			t.Errorf("exit %d, want 2 (usage)\n%s", code, out)
		}
		if !strings.Contains(out, "needs a target") {
			t.Errorf("the refusal does not say what is missing\n%s", out)
		}
	})

	t.Run("an unanswerable collision names the flag to pass", func(t *testing.T) {
		dir, secret := setup(t)
		// plain.txt is still there, so restoring onto the true name collides
		// - and there is no terminal to ask.
		out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret)
		if code != 2 {
			t.Errorf("exit %d, want 2 (usage)\n%s", code, out)
		}
		if !strings.Contains(out, "-y") {
			t.Errorf("the refusal does not name the flag that resolves it\n%s", out)
		}
		assertNoPartials(t, dir)
	})

	t.Run("an unknown fdsec sub-verb is a usage error", func(t *testing.T) {
		dir, _ := setup(t)
		out, code := run(t, dir, "fdsec", "frobnicate", "plain.fd-sec")
		if code != 2 {
			t.Errorf("exit %d, want 2 (usage)\n%s", code, out)
		}
	})

	t.Run("register refuses an option it does not have", func(t *testing.T) {
		dir, _ := setup(t)
		out, code := run(t, dir, "fdsec", "register", "-everywhere")
		if code != 2 {
			t.Errorf("exit %d, want 2 (usage)\n%s", code, out)
		}
		if !strings.Contains(out, "-all-users") {
			t.Errorf("the refusal does not name the option the command does have\n%s", out)
		}
	})
}

// An empty credential is accepted and must be labelled honestly wherever it
// appears - never called protection (invariant 10).
func TestFdsecEmptyCredentialIsLabelledNoSecrecy(t *testing.T) {
	dir, payload := workdir(t)
	out, code := run(t, dir, "plain.txt", "secure", "p:")
	if code != 0 {
		t.Fatalf("an empty credential exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "NO SECRECY") {
		t.Errorf("the empty credential was not labelled honestly\n%s", out)
	}
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "p:", "to", "restored.txt"); code != 0 {
		t.Fatalf("unsecure with an empty credential exited %d\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "restored.txt")); !bytes.Equal(got, payload) {
		t.Error("the obfuscation-only round trip is not byte-exact")
	}
}

// The help has to carry the surface, or the surface does not exist for anyone
// who did not read the spec.
func TestFdsecAppearsInTheHelp(t *testing.T) {
	dir := t.TempDir()
	full, code := run(t, dir, "help")
	if code != 0 {
		t.Fatalf("help exited %d\n%s", code, full)
	}
	for _, want := range []string{
		"SECRET FILES", "secure", "unsecure", "fdsec info", "fdsec verify",
		"p:<password>", "pf:<file>", "pe:<VAR>", "k:<keyfile>",
		"NO SECRECY", "does not guarantee erasure", "Exit codes",
	} {
		if !strings.Contains(full, want) {
			t.Errorf("the detailed help does not mention %q", want)
		}
	}
	short, code := run(t, dir, "-?")
	if code != 0 {
		t.Fatalf("short help exited %d\n%s", code, short)
	}
	for _, want := range []string{"secure", "unsecure"} {
		if !strings.Contains(short, want) {
			t.Errorf("the short help does not mention %q", want)
		}
	}
}

// redactCredentialArgs is the one piece that has to be right before anything
// else ships, so it is also tested directly.
func TestRedactCredentialArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{
			[]string{"filedo", "plain.txt", "secure", "del", "p:hunter2"},
			[]string{"filedo", "plain.txt", "secure", "del", "p:***"},
		},
		{
			[]string{"filedo", "plain.txt", "secure", "hunter2"},
			[]string{"filedo", "plain.txt", "secure", "***"},
		},
		{
			[]string{"filedo", "plain.txt", "secure", "pf:C:\\keys\\pw.txt"},
			[]string{"filedo", "plain.txt", "secure", "pf:C:\\keys\\pw.txt"},
		},
		{
			[]string{"filedo", "plain.txt", "secure", "pe:FDSEC_PW"},
			[]string{"filedo", "plain.txt", "secure", "pe:FDSEC_PW"},
		},
		{
			[]string{"filedo", "c.fd-sec", "unsecure", "to", "D:\\out\\name.bin", "p:hunter2"},
			[]string{"filedo", "c.fd-sec", "unsecure", "to", "D:\\out\\name.bin", "p:***"},
		},
		{
			[]string{"filedo", "fdsec", "verify", "c.fd-sec", "hunter2"},
			[]string{"filedo", "fdsec", "verify", "c.fd-sec", "***"},
		},
		{
			[]string{"filedo", "D:", "test", "100", "del"},
			[]string{"filedo", "D:", "test", "100", "del"},
		},
	}
	for _, c := range cases {
		got := redactCredentialArgs(c.in)
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("redactCredentialArgs(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

// The help text is one big fmt.Sprintf, so a literal percent sign in it is a
// format verb unless it is doubled. That bit an owner: the reveal paragraph
// named the sandbox root as %LOCALAPPDATA% and reached the screen as
// "%!L(MISSING)OCALAPPDATA%!\(MISSING)FileDO". Nothing caught it, because the
// gate proved the help mentions reveal and never read what it said. This
// asserts the whole of both help screens renders, which is the class of
// defect rather than the one instance.
func TestHelp_RendersWithoutFormatErrors(t *testing.T) {
	dir, _ := workdir(t)
	for _, verb := range []string{"-?", "help"} {
		out, code := run(t, dir, verb)
		if code != 0 {
			t.Fatalf("%s exited %d\n%s", verb, code, out)
		}
		// Every failed verb fmt reports starts with this, whatever follows.
		if strings.Contains(out, "%!") {
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "%!") {
					t.Errorf("%s printed an unrendered format verb:\n%s", verb, line)
				}
			}
		}
		if !strings.Contains(out, "reveal") {
			t.Errorf("%s does not name reveal at all\n%s", verb, out)
		}
	}

	// The detailed help is where a reveal's rules are written down, and the
	// sandbox root is the one path an owner has to be able to look at by hand.
	out, _ := run(t, dir, "help")
	if !strings.Contains(out, `%LOCALAPPDATA%\FileDO\reveal`) {
		t.Errorf("the detailed help does not print the sandbox root as a path that can be pasted\n%s", out)
	}
}

func TestFdsecRestoreDir(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "sub")
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	containerPath := filepath.Join(dir, "test.fd-sec")

	// 1. here
	oHere := &fdsecOpts{here: true}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	gotHere, err := fdsecRestoreDir(oHere, containerPath)
	if err != nil || gotHere != wd {
		t.Errorf("here: got (%q, %v), want (%q, nil)", gotHere, err, wd)
	}

	// 2. to existing dir
	oToDir := &fdsecOpts{haveTo: true, to: subDir}
	gotToDir, err := fdsecRestoreDir(oToDir, containerPath)
	if err != nil || gotToDir != subDir {
		t.Errorf("to dir: got (%q, %v), want (%q, nil)", gotToDir, err, subDir)
	}

	// 3. to file in existing dir
	targetFile := filepath.Join(subDir, "restored.txt")
	oToFile := &fdsecOpts{haveTo: true, to: targetFile}
	gotToFile, err := fdsecRestoreDir(oToFile, containerPath)
	if err != nil || gotToFile != subDir {
		t.Errorf("to file: got (%q, %v), want (%q, nil)", gotToFile, err, subDir)
	}

	// 4. default (container dir)
	oDefault := &fdsecOpts{}
	gotDefault, err := fdsecRestoreDir(oDefault, containerPath)
	if err != nil || gotDefault != dir {
		t.Errorf("default: got (%q, %v), want (%q, nil)", gotDefault, err, dir)
	}
}

func TestFdsecUnsecure_SealedNameNotWrittenToHistory(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "pw-history-test"
	const trueName = "secret-invoice-987654.docx"
	origPath := filepath.Join(dir, trueName)
	if err := os.WriteFile(origPath, []byte("confidential content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, code := run(t, dir, trueName, "secure", "p:"+secret); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	os.Remove(origPath)
	os.Remove(filepath.Join(dir, "history.json"))

	containerName := "secret-invoice-987654.fd-sec"
	if out, code := run(t, dir, containerName, "unsecure", "p:"+secret); code != 0 {
		t.Fatalf("unsecure exited %d\n%s", code, out)
	}

	histPath := filepath.Join(dir, "history.json")
	if !exists(histPath) {
		t.Fatalf("history.json was not written")
	}
	histData := string(mustRead(t, histPath))
	if strings.Contains(histData, trueName) {
		t.Errorf("history.json contains the sealed true name %q:\n%s", trueName, histData)
	}
}
