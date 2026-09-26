package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"filedo/statedir"
)

// The SP-0034 (AUD-03) fixes at the command surface. Each test names its
// finding.

// TestFdsecLongPathFileIsNotANetworkTarget is AUD-03-F2: a file or a folder
// spelled with the \\?\ prefix is what it is, not a network share; only a share
// root or a path that cannot be reached keeps the network refusal.
func TestFdsecLongPathFileIsNotANetworkTarget(t *testing.T) {
	dir, payload := workdir(t)
	long := `\\?\` + filepath.Join(dir, "plain.txt")
	out, code := run(t, dir, long, "secure", "del", "-y", "p:pw")
	if code != 0 {
		t.Fatalf("%s secure del exited %d\n%s", long, code, out)
	}
	if !exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Fatalf("no container beside the source\n%s", out)
	}
	if exists(filepath.Join(dir, "plain.txt")) {
		t.Fatalf("del kept the original after a verified pack\n%s", out)
	}
	out, code = run(t, dir, `\\?\`+filepath.Join(dir, "plain.fd-sec"), "unsecure", "p:pw")
	if code != 0 {
		t.Fatalf("unsecure through \\\\?\\ exited %d\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Fatal("the restored file differs from the original")
	}

	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("inside"), 0o644)
	if out, code := run(t, dir, `\\?\`+sub, "secure", "p:pw"); code != 0 {
		t.Fatalf("folder secure through \\\\?\\ exited %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "sub.fd-sec")) {
		t.Fatal("no folder container")
	}

	// A share root that does not answer is still a network target.
	out, code = run(t, dir, `\\localhost\filedo-aud03-no-such-share`, "secure", "p:pw")
	if code == 0 || !strings.Contains(out, "network") {
		t.Fatalf("an unreachable share root: exit %d, want the network refusal\n%s", code, out)
	}
}

// TestSecureMaskFolderPartIsLiteral is AUD-03-F3: only the last component of
// a mask is a pattern and only * and ? are wildcards, so a folder named a[1]
// is that folder, never the class that matches a1.
func TestSecureMaskFolderPartIsLiteral(t *testing.T) {
	dir := t.TempDir()
	plain, bracket := filepath.Join(dir, "a1"), filepath.Join(dir, "a[1]")
	os.Mkdir(plain, 0o755)
	os.Mkdir(bracket, 0o755)
	os.WriteFile(filepath.Join(plain, "x.txt"), []byte("not named"), 0o644)
	os.WriteFile(filepath.Join(bracket, "y.txt"), []byte("named"), 0o644)

	out, code := run(t, dir, filepath.Join(bracket, "*.txt"), "secure", "del", "-y", "p:pw")
	if code != 0 {
		t.Fatalf("mask secure exited %d\n%s", code, out)
	}
	if !exists(filepath.Join(bracket, "y.fd-sec")) || exists(filepath.Join(bracket, "y.txt")) {
		t.Fatalf("the named folder's file was not packed\n%s", out)
	}
	if !exists(filepath.Join(plain, "x.txt")) || exists(filepath.Join(plain, "x.fd-sec")) {
		t.Fatalf("a folder the mask did not name was packed or its original deleted\n%s", out)
	}

	// In the last component a bracket is a literal character too.
	os.WriteFile(filepath.Join(dir, "r[1].txt"), []byte("bracket"), 0o644)
	os.WriteFile(filepath.Join(dir, "r1.txt"), []byte("plain"), 0o644)
	if out, code := run(t, dir, "r[1].*", "secure", "p:pw"); code != 0 {
		t.Fatalf("r[1].* secure exited %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "r[1].fd-sec")) || exists(filepath.Join(dir, "r1.fd-sec")) {
		t.Fatal("r[1].* must match r[1].txt and only it")
	}
}

// TestRemoveFdsecPartialsIsLiteral is AUD-03-F3's cleanup half: the partials
// of a[1].fd-sec are removed, and those of a concurrent a1.fd-sec are not.
func TestRemoveFdsecPartialsIsLiteral(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "a[1].fd-sec.fdsec-partial-1")
	other := filepath.Join(dir, "a1.fd-sec.fdsec-partial-2")
	os.WriteFile(mine, nil, 0o644)
	os.WriteFile(other, nil, 0o644)
	removeFdsecPartials(filepath.Join(dir, "a[1].fd-sec"))
	if exists(mine) {
		t.Error("the destination's own partial was left")
	}
	if !exists(other) {
		t.Error("another container's partial was removed")
	}
}

// TestFdsecVerifyObservesStop is AUD-03-F4: a stopped verify claims nothing -
// no OK line - and its verdict is Stopped, for a file and a folder container.
func TestFdsecVerifyObservesStop(t *testing.T) {
	dir, _ := workdir(t)
	sub := filepath.Join(dir, "sub")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("inside"), 0o644)
	for _, src := range []string{"plain.txt", "sub"} {
		if out, code := run(t, dir, src, "secure", "p:pw"); code != 0 {
			t.Fatalf("setup secure %s exited %d\n%s", src, code, out)
		}
	}
	stop := filepath.Join(dir, "run.stop")
	os.WriteFile(stop, nil, 0o644)
	for _, c := range []string{"plain.fd-sec", "sub.fd-sec"} {
		events := filepath.Join(dir, c+".ev.jsonl")
		out, code := run(t, dir, "--events", events, "--stop-file", stop, "fdsec", "verify", c, "p:pw")
		if code != 0 {
			t.Fatalf("%s: a stopped run exits 0, got %d\n%s", c, code, out)
		}
		if strings.Contains(out, "OK ") {
			t.Errorf("%s: a stopped verify claimed a pass\n%s", c, out)
		}
		if v := lastResult(t, events)["verdict"]; v != "Stopped" {
			t.Errorf("%s: verdict %v, want Stopped\n%s", c, v, out)
		}
	}
}

// TestPeCredentialServesEveryBatchLine is AUD-03-F5: a variable named by pe:
// resolves on every line of a batch, while FDSEC-07 keeps it out of the
// process environment (TestPeCredentialClearedAfterResolve).
func TestPeCredentialServesEveryBatchLine(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte("content of "+n), 0o644)
	}
	writeLst(t, dir, "work.lst", "a.txt secure pe:FILEDO_AUD03_PE\nb.txt secure pe:FILEDO_AUD03_PE\n")
	cmd := exec.Command(filedoExe, "from", "work.lst")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FILEDO_AUD03_PE=batch-pw", "FILEDO_FDSEC_NO_LAUNCH=1",
		"FILEDO_FDSEC_REVEAL_ROOT="+revealRoot(dir), statedir.EnvOverride+"="+dir)
	cmd.Stdin = strings.NewReader("")
	b, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the two-line pe: batch failed: %v\n%s", err, b)
	}
	for _, n := range []string{"a", "b"} {
		out := t.TempDir()
		if o, code := run(t, dir, n+".fd-sec", "unsecure", "to", out, "p:batch-pw"); code != 0 {
			t.Fatalf("%s.fd-sec does not open with the variable's value: exit %d\n%s\n%s", n, code, o, b)
		}
	}
}
