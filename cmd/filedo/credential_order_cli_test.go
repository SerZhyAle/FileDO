package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Black-box tests for AUD-09-F3 and AUD-29-F2 (one change): a credential-shaped
// `p:` token is redacted on every command line whatever the verb or its
// position, and the verb-first container slot refuses one. The rule is
// FDSEC-BEHAVIOUR 1.4 section 8.2. Every run points the state root (history.json
// included) at its working directory, so assertNoSecretOnDisk sweeps the events
// file and the history together.

// TestRedactCredentialAnyPosition holds the redactor's rule directly: `p:` with
// a value that is not a path on P: is `p:***` in any position of any line;
// `p:\..`, `p:/..` and the upper-case `P:` the parser never reads as a
// password stay as typed outside the fdsec family.
func TestRedactCredentialAnyPosition(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{
			[]string{"filedo", "plain.txt", "secrue", "p:hunter2"},
			[]string{"filedo", "plain.txt", "secrue", "p:***"},
		},
		{
			[]string{"filedo", "fdsec", "verify", "p:hunter2", "a.fd-sec"},
			[]string{"filedo", "fdsec", "verify", "p:***", "***"},
		},
		{
			[]string{"fdsec", "info", "p:hunter2"},
			[]string{"fdsec", "info", "p:***"},
		},
		{
			[]string{"filedo", "p:hunter2", "secure"},
			[]string{"filedo", "p:***", "secure"},
		},
		{
			[]string{"filedo", "fdsec", "verify", `p:\dir\a.fd-sec`, "p:hunter2"},
			[]string{"filedo", "fdsec", "verify", `p:\dir\a.fd-sec`, "p:***"},
		},
		{
			[]string{"filedo", `p:\dir`, "info"},
			[]string{"filedo", `p:\dir`, "info"},
		},
		{
			[]string{"filedo", "p:/dir", "info"},
			[]string{"filedo", "p:/dir", "info"},
		},
		{
			[]string{"filedo", "P:hunter2", "info"},
			[]string{"filedo", "P:hunter2", "info"},
		},
	}
	for _, c := range cases {
		got := redactCredentialArgs(c.in)
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("redactCredentialArgs(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
	if got := redactCredentialTarget("p:hunter2"); got != "p:***" {
		t.Errorf("redactCredentialTarget(p:hunter2) = %q", got)
	}
	if got := redactCredentialTarget(`p:\dir\a.fd-sec`); got != `p:\dir\a.fd-sec` {
		t.Errorf("redactCredentialTarget kept no path on P: %q", got)
	}
}

// TestMistypedContainerVerbRedactsCredential is AUD-29-F2: a container verb with
// a transposed letter is not an fdsec line, and the `p:` value it carries still
// reaches neither the event stream nor history.json.
func TestMistypedContainerVerbRedactsCredential(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "hunter2-mistyped-verb"
	events := filepath.Join(dir, "ev.jsonl")
	out, code := run(t, dir, filepath.Join(dir, "plain.txt"), "secrue", "p:"+secret, "--events", events)
	if code != 2 {
		t.Fatalf("the mistyped verb exited %d, want 2\n%s", code, out)
	}
	if !exists(events) {
		t.Fatalf("no events file was written\n%s", out)
	}
	if b := mustRead(t, events); !strings.Contains(string(b), "p:***") {
		t.Errorf("the run event does not carry the redacted token\n%s", b)
	}
	assertNoSecretOnDisk(t, dir, secret)
}

// TestFdsecCredentialBeforeContainerRefused is AUD-09-F3: `fdsec verify|info
// p:<secret> <container>` - the credential typed before the container - is a
// usage error (Not proven, exit 2) that names neither token, and the secret is
// in neither the events file nor history.json. A `p:\..` container argument is
// a path on drive P:, never refused as a credential.
func TestFdsecCredentialBeforeContainerRefused(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "hunter2-before-the-container"
	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("setup secure exited %d\n%s", code, out)
	}
	for _, sub := range []string{"verify", "info"} {
		for _, tail := range [][]string{{"plain.fd-sec"}, {}} {
			events := filepath.Join(dir, "ev-"+sub+".jsonl")
			os.Remove(events)
			args := append([]string{"fdsec", sub, "p:" + secret}, tail...)
			args = append(args, "--events", events)
			out, code := run(t, dir, args...)
			if code != fdsecExitUsage {
				t.Fatalf("filedo %q exited %d, want %d\n%s", args, code, fdsecExitUsage, out)
			}
			if v := lastResult(t, events)["verdict"]; v != "Not proven" {
				t.Fatalf("filedo %q ended %v, want Not proven\n%s", args, v, out)
			}
			if !strings.Contains(out, "the container path comes first") {
				t.Errorf("filedo %q does not say the container comes first\n%s", args, out)
			}
			if strings.Contains(out, secret) || strings.Contains(out, "plain.fd-sec") {
				t.Errorf("the refusal of %q quotes a token\n%s", args, out)
			}
		}
	}
	// The refused lines are in history.json, redacted - so the sweep below
	// read the file the leak used to reach.
	if h := string(mustRead(t, filepath.Join(dir, "history.json"))); !strings.Contains(h, "fdsec verify p:***") || !strings.Contains(h, "fdsec info p:***") {
		t.Errorf("history.json does not hold the refused lines redacted\n%s", h)
	}
	assertNoSecretOnDisk(t, dir, secret)

	// A path on drive P: in the container slot is opened, not refused: it
	// fails as I/O (nothing there), never as the usage refusal above.
	out, code := run(t, dir, "fdsec", "verify", `p:\fdsec-no-such-dir-4a1\x.fd-sec`, "p:"+secret)
	if code == fdsecExitUsage || strings.Contains(out, "the container path comes first") {
		t.Errorf("a p:\\ path was refused as a credential: exit %d\n%s", code, out)
	}
	assertNoSecretOnDisk(t, dir, secret)
}

// TestCredentialInAnErrorIsRedacted is SP-0064 T1-F2: a credential-shaped
// `p:` token that lands in a path or operation slot is quoted back by the
// error it causes. The arguments were redacted, the error text was not, so
// the password reached the finding event and history.json's error field. (The
// console keeps what the user typed; the GUI screens its report itself.)
func TestCredentialInAnErrorIsRedacted(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "hunter2-in-an-error"
	p := "p:" + secret
	for i, args := range [][]string{
		{p},
		{p, "secure"},
		{"plain.txt", p, "secure"},
		{"copy", p, filepath.Join(dir, "dst3")},
		{"check", p},
		{"compare", dir, p},
	} {
		events := filepath.Join(dir, fmt.Sprintf("ev-%d.jsonl", i))
		if out, code := run(t, dir, append(args, "--events", events)...); code == 0 {
			t.Errorf("filedo %q exited 0\n%s", args, out)
		}
	}
	assertNoSecretOnDisk(t, dir, secret)
}

// TestFdsecBarePasswordBeforeContainerRefused is SP-0064 AUD-52-F1: the bare
// form of the same slip, `fdsec verify <password> <container>`. The word in
// the container slot names nothing on disk, is not written like a path, and
// a token follows it, so it is unrecognised: refused like a `p:` token there,
// quoted nowhere, and `***` in every record (FDSEC-BEHAVIOUR section 8.2).
func TestFdsecBarePasswordBeforeContainerRefused(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "hunter2bare"
	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("setup secure exited %d\n%s", code, out)
	}
	for _, sub := range []string{"verify", "info"} {
		events := filepath.Join(dir, "ev-bare-"+sub+".jsonl")
		args := []string{"fdsec", sub, secret, "plain.fd-sec", "--events", events}
		out, code := run(t, dir, args...)
		if code != fdsecExitUsage {
			t.Fatalf("filedo %q exited %d, want %d\n%s", args, code, fdsecExitUsage, out)
		}
		if strings.Contains(out, secret) {
			t.Errorf("the refusal of %q quotes the password\n%s", args, out)
		}
	}
	assertNoSecretOnDisk(t, dir, secret)

	// The right order still works, and a missing container written like a
	// path is still a missing file, not this refusal.
	if out, code := run(t, dir, "fdsec", "verify", "plain.fd-sec", "p:"+secret); code != 0 {
		t.Fatalf("fdsec verify <container> <password> exited %d\n%s", code, out)
	}
	if out, code := run(t, dir, "fdsec", "verify", "nosuch.fd-sec", "p:"+secret); code == fdsecExitUsage {
		t.Errorf("a missing container path was refused as a password\n%s", out)
	}
}

// TestCredentialWithSpacesInAnErrorIsRedacted is SP-0064 R-F1: a `p:` password
// that holds a space is one argument, and the error that quotes it must lose
// all of it, not only the part up to the first space.
func TestCredentialWithSpacesInAnErrorIsRedacted(t *testing.T) {
	dir, _ := workdir(t)
	const tail = "horse-battery-staple"
	for i, args := range [][]string{
		{"p:correct " + tail, "secure"},
		{"plain.txt", "p:correct " + tail, "secure"},
	} {
		events := filepath.Join(dir, fmt.Sprintf("ev-sp-%d.jsonl", i))
		if out, code := run(t, dir, append(args, "--events", events)...); code == 0 {
			t.Errorf("filedo %q exited 0\n%s", args, out)
		}
	}
	assertNoSecretOnDisk(t, dir, tail)
}
