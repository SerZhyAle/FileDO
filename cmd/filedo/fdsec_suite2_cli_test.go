package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Suite 2 from the command line (SP-0019 D2, D6): one option on secure,
// and every other verb finds the suite by opening. These runs use the real
// work factors - the binary has no test seam - so there are few of them.

func TestFdsecSuite2SecureRestoreInfoVerify(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-quiet-suite"

	out, code := run(t, dir, "plain.txt", "secure", "suite2", "p:"+secret)
	if code != 0 || !strings.Contains(out, "suite 2") {
		t.Fatalf("secure suite2 exited %d, want 0 and a suite-2 report\n%s", code, out)
	}
	container := filepath.Join(dir, "plain.fd-sec")
	if !exists(container) {
		t.Fatalf("no container written\n%s", out)
	}
	fi, _ := os.Stat(container)
	if fi.Size() < 40+1060+8192+int64(len(payload))+16 {
		t.Fatalf("the container is %d bytes, too short for a suite-2 file of this payload", fi.Size())
	}

	out, code = run(t, dir, "fdsec", "info", "plain.fd-sec", "p:"+secret)
	if code != 0 || !strings.Contains(out, "suite 2") || !strings.Contains(out, "256 MiB") || !strings.Contains(out, "does not carry the original's timestamps") {
		t.Fatalf("info exited %d or did not describe suite 2\n%s", code, out)
	}
	out, code = run(t, dir, "fdsec", "verify", "plain.fd-sec", "p:"+secret)
	if code != 0 || !strings.Contains(out, "every frame authenticated") {
		t.Fatalf("verify exited %d, want 0\n%s", code, out)
	}

	// A wrong password is the one outcome, exit 3, and nothing is written.
	out, code = run(t, dir, "plain.fd-sec", "unsecure", "p:not-it", "to", "wrong.txt")
	if code != 3 || exists(filepath.Join(dir, "wrong.txt")) {
		t.Fatalf("wrong password: exit %d, want 3\n%s", code, out)
	}
	if !strings.Contains(out, "not a container") {
		t.Errorf("the wrong-password message names fewer than all three readings\n%s", out)
	}

	out, code = run(t, dir, "plain.fd-sec", "unsecure", "p:"+secret, "to", "restored.txt")
	if code != 0 {
		t.Fatalf("unsecure exited %d, want 0\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "restored.txt")); !bytes.Equal(got, payload) {
		t.Fatal("the suite-2 container did not restore byte-exactly")
	}
	assertNoSecretOnDisk(t, dir, secret)
	assertNoPartials(t, dir)
}

// The 5.6 decision: rename drops the .fd-sec suffix, and the blob is opened by
// the explicit verb - for suite 2 exactly as for suite 1.
func TestFdsecSuite2NamelessBlob(t *testing.T) {
	dir, payload := workdir(t)
	const secret = "pw-quiet-blob"
	if out, code := run(t, dir, "plain.txt", "secure", "suite2", "rename", "p:"+secret); code != 0 {
		t.Fatalf("secure suite2 rename exited %d\n%s", code, out)
	}
	var blob string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == "" && !e.IsDir() && e.Name() != "plain.txt" {
			blob = e.Name()
		}
	}
	if blob == "" {
		t.Fatal("no extensionless container was written")
	}
	if out, code := run(t, dir, blob, "unsecure", "p:"+secret, "to", "back.txt"); code != 0 {
		t.Fatalf("unsecure on the suite-2 blob exited %d\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "back.txt")); !bytes.Equal(got, payload) {
		t.Fatal("the suite-2 blob did not restore byte-exactly")
	}
}

func TestFdsecSuite2Refusals(t *testing.T) {
	dir, _ := workdir(t)
	if out, code := run(t, dir, "plain.txt", "secure", "p:pw"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	if out, code := run(t, dir, "plain.fd-sec", "unsecure", "suite2", "p:pw"); code != 2 {
		t.Errorf("suite2 on unsecure: exit %d, want 2 (usage)\n%s", code, out)
	}
	if out, code := run(t, dir, "fdsec", "info", "plain.fd-sec", "suite2", "p:pw"); code != 2 {
		t.Errorf("suite2 on info: exit %d, want 2 (usage)\n%s", code, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, "folder", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "folder", "sub", "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, dir, "folder", "secure", "suite2", "p:pw")
	if code != 2 || exists(filepath.Join(dir, "folder.fd-sec")) {
		t.Errorf("suite2 on a folder: exit %d, want 2 and no container\n%s", code, out)
	}
	// The default did not move (D2): a plain secure is still suite 1.
	out, code = run(t, dir, "fdsec", "info", "plain.fd-sec", "p:pw")
	if code != 0 || !strings.Contains(out, "suite 1") {
		t.Errorf("the default writer is no longer suite 1\n%s", out)
	}
}

func TestRedactCredentialArgsKeepsSuite2(t *testing.T) {
	got := redactCredentialArgs([]string{"filedo", "plain.txt", "secure", "suite2", "p:hunter2"})
	want := []string{"filedo", "plain.txt", "secure", "suite2", "p:***"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
