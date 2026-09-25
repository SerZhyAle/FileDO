package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"

	"filedo/fdsec"
	"filedo/statedir"
)

// The SP-0025 proofs at the command surface. Each test names its ticket.

// TestSecureToSelfRefused is FDSEC-03: `secure to <source>` is a usage error
// before a byte moves, and the source is untouched.
func TestSecureToSelfRefused(t *testing.T) {
	dir, payload := workdir(t)
	out, code := run(t, dir, "plain.txt", "secure", "to", "plain.txt", "-y", "p:pw")
	if code != fdsecExitUsage {
		t.Fatalf("exit %d, want %d (usage)\n%s", code, fdsecExitUsage, out)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Fatal("the source changed")
	}
}

// TestUnsecureToContainerRefused is FDSEC-03's unsecure half: the restored
// file can never land on the container it comes from.
func TestUnsecureToContainerRefused(t *testing.T) {
	dir, _ := workdir(t)
	if out, code := run(t, dir, "plain.txt", "secure", "p:pw"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	before := mustRead(t, filepath.Join(dir, "plain.fd-sec"))
	out, code := run(t, dir, "plain.fd-sec", "unsecure", "to", "plain.fd-sec", "del", "-y", "p:pw")
	if code != fdsecExitUsage {
		t.Fatalf("exit %d, want %d (usage)\n%s", code, fdsecExitUsage, out)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.fd-sec")); !bytes.Equal(got, before) {
		t.Fatal("the container changed")
	}
}

// TestCollisionOverwriteKeepsOldUntilVerified is FDSEC-03: the "o" answer no
// longer deletes the destination; it only asks the final rename to replace it.
func TestCollisionOverwriteKeepsOldUntilVerified(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "report.fd-sec")
	os.WriteFile(dst, []byte("old container"), 0o644)
	got, replace, err := fdsecResolveCollisionWith(dst, &fdsecOpts{}, false, func(string) (string, error) { return "o", nil })
	if err != nil || got != dst || !replace {
		t.Fatalf("overwrite answer: got %q replace=%v err=%v", got, replace, err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "old container" {
		t.Fatal("the old destination was deleted when overwrite was merely chosen")
	}

	// A container this run produced is never offered for overwrite.
	o := &fdsecOpts{produced: map[string]bool{fdsecPathKey(dst): true}}
	got, replace, err = fdsecResolveCollisionWith(dst, o, false, func(string) (string, error) {
		t.Fatal("a container this run wrote must not be offered for overwrite")
		return "", nil
	})
	if err != nil || replace || got == dst {
		t.Fatalf("produced-here collision: got %q replace=%v err=%v", got, replace, err)
	}
}

// TestSecureConcurrentSameStem is FDSEC-01: two secures to one container name
// (x.jpg and x.cr2 from one Explorer multi-select) both keep their container.
func TestSecureConcurrentSameStem(t *testing.T) {
	dir := t.TempDir()
	jpg := bytes.Repeat([]byte("JPEG"), 300000)
	cr2 := bytes.Repeat([]byte("RAW2"), 300000)
	os.WriteFile(filepath.Join(dir, "x.jpg"), jpg, 0o644)
	os.WriteFile(filepath.Join(dir, "x.cr2"), cr2, 0o644)

	var wg sync.WaitGroup
	codes := make([]int, 2)
	outs := make([]string, 2)
	for i, name := range []string{"x.jpg", "x.cr2"} {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			outs[i], codes[i] = run(t, dir, name, "secure", "del", "-y", "p:pw")
		}(i, name)
	}
	wg.Wait()
	for i := range codes {
		if codes[i] != 0 {
			t.Fatalf("secure %d exited %d\n%s", i, codes[i], outs[i])
		}
	}
	containers, _ := filepath.Glob(filepath.Join(dir, "x*.fd-sec"))
	if len(containers) != 2 {
		t.Fatalf("want two containers, got %v\n%s\n%s", containers, outs[0], outs[1])
	}
	// Both round-trip: each container restores one of the two originals.
	restored := map[string]bool{}
	for _, c := range containers {
		sub := t.TempDir()
		if out, code := run(t, dir, filepath.Base(c), "unsecure", "to", sub, "p:pw"); code != 0 {
			t.Fatalf("unsecure %s exited %d\n%s", c, code, out)
		}
		entries, _ := os.ReadDir(sub)
		for _, e := range entries {
			b := mustRead(t, filepath.Join(sub, e.Name()))
			switch {
			case bytes.Equal(b, jpg):
				restored["jpg"] = true
			case bytes.Equal(b, cr2):
				restored["cr2"] = true
			}
		}
	}
	if !restored["jpg"] || !restored["cr2"] {
		t.Fatalf("an original was lost: restored %v", restored)
	}
}

// TestSecureStopFileKeepsOriginal is FDSEC-02: with a stop requested, secure
// del writes no container, leaves no partial, keeps the original, and the
// verdict is Stopped.
func TestSecureStopFileKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	big := bytes.Repeat([]byte("0123456789abcdef"), 4<<20) // 64 MiB
	os.WriteFile(filepath.Join(dir, "big.bin"), big, 0o644)
	stop := filepath.Join(dir, "run.stop")
	os.WriteFile(stop, nil, 0o644)
	events := filepath.Join(dir, "ev.jsonl")

	out, code := run(t, dir, "--events", events, "--stop-file", stop, "big.bin", "secure", "del", "-y", "p:pw")
	if code != 0 {
		t.Fatalf("a stopped run exits 0 (rule 15), got %d\n%s", code, out)
	}
	if v := lastResult(t, events)["verdict"]; v != "Stopped" {
		t.Fatalf("verdict %v, want Stopped\n%s", v, out)
	}
	if !exists(filepath.Join(dir, "big.bin")) {
		t.Fatal("the original was deleted under a Stopped verdict")
	}
	if exists(filepath.Join(dir, "big.fd-sec")) {
		t.Fatal("a stopped secure left a container")
	}
	assertNoPartials(t, dir)
}

// TestSecureMaskStopTouchesNothing is FDSEC-02 for a mask: no target is
// started after a stop.
func TestSecureMaskStopTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		os.WriteFile(filepath.Join(dir, n), []byte("content of "+n), 0o644)
	}
	stop := filepath.Join(dir, "run.stop")
	os.WriteFile(stop, nil, 0o644)
	out, _ := run(t, dir, "--stop-file", stop, "*.txt", "secure", "del", "-y", "p:pw")
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		if !exists(filepath.Join(dir, n)) {
			t.Errorf("%s was deleted after a stop\n%s", n, out)
		}
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.fd-sec")); len(m) > 1 {
		t.Errorf("targets were still packed after a stop: %v", m)
	}
}

// TestPeCredentialUnsetOrEmptyRefused is FDSEC-19.
func TestPeCredentialUnsetOrEmptyRefused(t *testing.T) {
	dir, payload := workdir(t)
	out, code := run(t, dir, "plain.txt", "secure", "wipe", "-y", "pe:FILEDO_TEST_DEFINITELY_UNSET_VAR")
	if code != fdsecExitUsage {
		t.Fatalf("unset pe: exit %d, want %d\n%s", code, fdsecExitUsage, out)
	}
	cmd := exec.Command(filedoExe, "plain.txt", "secure", "wipe", "-y", "pe:FILEDO_TEST_EMPTY_VAR")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "FILEDO_TEST_EMPTY_VAR=", statedir.EnvOverride+"="+dir, "FILEDO_FDSEC_NO_LAUNCH=1")
	cmd.Stdin = strings.NewReader("")
	b, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != fdsecExitUsage {
		t.Fatalf("empty pe: want exit %d, got %v\n%s", fdsecExitUsage, err, b)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Fatal("the source changed")
	}
	if exists(filepath.Join(dir, "plain.fd-sec")) {
		t.Fatal("a container was written with no credential")
	}
}

// TestPeCredentialClearedAfterResolve is FDSEC-07: the variable leaves the
// environment the moment it is read, before any child could inherit it.
func TestPeCredentialClearedAfterResolve(t *testing.T) {
	t.Setenv("FILEDO_TEST_PE_CRED", "hunter2-probe")
	o := &fdsecOpts{credSrc: "pe", credVal: "FILEDO_TEST_PE_CRED"}
	cred, err := resolveFdsecCredential(o, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(cred) == "" {
		t.Fatal("the credential was not read")
	}
	if _, still := os.LookupEnv("FILEDO_TEST_PE_CRED"); still {
		t.Fatal("the credential variable is still in the environment after it was read")
	}
}

// TestRunEventRedactsCredentials_VerbFirst is FDSEC-04.
func TestRunEventRedactsCredentials_VerbFirst(t *testing.T) {
	dir, _ := workdir(t)
	const secret = "verbfirst-secret-77"
	if out, code := run(t, dir, "plain.txt", "secure", "p:"+secret); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	for _, sub := range []string{"verify", "info"} {
		for _, form := range []string{"p:" + secret, secret} {
			events := filepath.Join(dir, sub+"-events.jsonl")
			os.Remove(events)
			run(t, dir, "--events", events, "fdsec", sub, "plain.fd-sec", form)
			raw, err := os.ReadFile(events)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), secret) {
				t.Errorf("fdsec %s %s: the credential reached the events file:\n%s", sub, form, raw)
			}
		}
	}
	assertNoSecretInHistory(t, dir, secret)
}

// TestTwoBareTokensNotEchoed is FDSEC-05.
func TestTwoBareTokensNotEchoed(t *testing.T) {
	dir, _ := workdir(t)
	events := filepath.Join(dir, "ev.jsonl")
	out, code := run(t, dir, "--events", events, "plain.txt", "secure", "correctbattery", "horsestaple")
	if code != fdsecExitUsage {
		t.Fatalf("exit %d, want %d\n%s", code, fdsecExitUsage, out)
	}
	for _, s := range []string{"correctbattery", "horsestaple"} {
		raw, _ := os.ReadFile(events)
		if strings.Contains(string(raw), s) {
			t.Errorf("%q reached the events file", s)
		}
		assertNoSecretInHistory(t, dir, s)
	}
}

// TestRedactorFollowsTheParser is FDSEC-18: in the target-first form a
// sub-verb word is the password, and it is redacted like one.
func TestRedactorFollowsTheParser(t *testing.T) {
	got := redactCredentialArgs([]string{"filedo", "a.txt", "secure", "verify"})
	if got[3] != "***" {
		t.Errorf("`a.txt secure verify`: the parser takes verify as the password, the redactor kept it: %v", got)
	}
	got = redactCredentialArgs([]string{"fdsec", "verify", "c.fd-sec", "pw"})
	if got[2] != "c.fd-sec" || got[3] != "***" {
		t.Errorf("verb-first verify: %v", got)
	}
	got = redactCredentialArgs([]string{"x.fd-sec", "unsecure", "start", "p:pw"})
	if got[2] != "start" || got[3] != "p:***" {
		t.Errorf("start is an option word: %v", got)
	}
}

// TestFdsecResolveCollision_LongName is FDSEC-09: a 254-character name ends
// quickly with a free, valid name, and the search is bounded.
func TestFdsecResolveCollision_LongName(t *testing.T) {
	dir := t.TempDir()
	name := strings.Repeat("n", 247) + ".fd-sec" // 254 characters
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Skipf("cannot create a 254-character name here: %v", err)
	}
	done := make(chan struct{})
	var got string
	var err error
	go func() {
		got, _, err = fdsecResolveCollisionWith(p, &fdsecOpts{assumeYes: true}, false, nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("the collision rule did not finish within 1 s")
	}
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if utf16Len(filepath.Base(got)) > fdsecMaxNameUnits {
		t.Fatalf("the suffixed name is %d characters", utf16Len(filepath.Base(got)))
	}
	if exists(got) {
		t.Fatal("the suffixed name is taken")
	}
}

// TestOneCharacterFileIsSecured is FDSEC-14.
func TestOneCharacterFileIsSecured(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("one letter"), 0o644)
	if out, code := run(t, dir, "file", "a", "secure", "p:pw"); code != 0 {
		t.Fatalf("file a secure exited %d\n%s", code, out)
	}
	if !exists(filepath.Join(dir, "a.fd-sec")) {
		t.Fatal("no a.fd-sec")
	}
	os.Remove(filepath.Join(dir, "a.fd-sec"))
	if out, code := run(t, dir, "a", "secure", "p:pw"); code != 0 {
		t.Fatalf("a secure (an existing file) exited %d\n%s", code, out)
	}
}

// TestRevealRwWithoutDash is FDSEC-13.
func TestRevealRwWithoutDash(t *testing.T) {
	dir, payload := workdir(t)
	secret := secureOne(t, dir, "plain.txt")
	os.Remove(filepath.Join(dir, "plain.txt"))
	if out, code := run(t, dir, "plain.fd-sec", "reveal", "rw", "p:"+secret); code != 0 {
		t.Fatalf("reveal rw exited %d\n%s", code, out)
	}
	if got := mustRead(t, filepath.Join(dir, "plain.txt")); !bytes.Equal(got, payload) {
		t.Fatal("reveal rw did not restore the file")
	}
}

// TestRevealNeverLaunchTable is FDSEC-08: the table covers the interpreter
// and Outlook Level-1 types the audit named, and a reveal refuses them.
func TestRevealNeverLaunchTable(t *testing.T) {
	required := []string{".py", ".pyw", ".pyz", ".pyzw", ".pl", ".rb", ".appref-ms", ".wsb", ".xll",
		".msix", ".msixbundle", ".appx", ".appxbundle", ".appinstaller", ".msu", ".mst", ".psc1",
		".ps1xml", ".ws", ".wsc", ".scf", ".library-ms", ".search-ms", ".searchconnector-ms",
		".settingcontent-ms", ".theme", ".themepack", ".diagcab", ".iso", ".img", ".vhd", ".vhdx",
		".jnlp", ".website", ".cer", ".crt", ".der", ".ade", ".adp", ".mde", ".accde", ".shb", ".shs",
		".ins", ".isp", ".exe", ".bat", ".cmd", ".ps1", ".vbs", ".js", ".hta", ".msi", ".lnk"}
	for _, ext := range required {
		if !fdsecNeverLaunch[ext] {
			t.Errorf("%s is missing from the never-launch table", ext)
		}
	}
	for _, name := range []string{"invoice.py", "app.appref-ms", "disk.iso"} {
		dir, _ := workdir(t)
		os.WriteFile(filepath.Join(dir, name), []byte("payload"), 0o644)
		secret := secureOne(t, dir, name)
		container := strings.TrimSuffix(name, filepath.Ext(name)) + ".fd-sec"
		out, code := run(t, dir, container, "reveal", "p:"+secret)
		if code != 0 || !strings.Contains(out, "NOT launched") {
			t.Errorf("%s: exit %d, not refused\n%s", name, code, out)
		}
	}
}

// TestKeyfileDigestIsStreamedAndUnchanged is FDSEC-16.
func TestKeyfileDigestIsStreamedAndUnchanged(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key.bin")
	data := bytes.Repeat([]byte{7, 1, 9}, 700001)
	os.WriteFile(p, data, 0o644)
	got, err := fdsecKeyfileDigest(p)
	if err != nil {
		t.Fatal(err)
	}
	if got != blake2b.Sum256(data) {
		t.Fatal("the streamed keyfile digest differs from the whole-file digest")
	}
}

// TestReparseTagClassification is FDSEC-11.
func TestReparseTagClassification(t *testing.T) {
	data := []uint32{0x9000001A, 0x9000101A, 0x9000F01A, reparseTagDedup, reparseTagWOF, reparseTagOneDrive}
	links := []uint32{reparseTagSymlink, reparseTagMountPoint, reparseTagAppExecLink, 0x12345678}
	for _, tag := range data {
		if !fdsecDataReparseTag(tag) {
			t.Errorf("tag %#x is data and must be allowed", tag)
		}
	}
	for _, tag := range links {
		if fdsecDataReparseTag(tag) {
			t.Errorf("tag %#x must be refused", tag)
		}
	}
	// A real junction is refused; an ordinary file is not.
	dir := t.TempDir()
	plain := filepath.Join(dir, "f.txt")
	os.WriteFile(plain, []byte("x"), 0o644)
	if fdsecRefusedReparse(plain) {
		t.Error("an ordinary file was refused as a reparse point")
	}
	j := filepath.Join(dir, "j")
	if err := exec.Command("cmd", "/c", "mklink", "/J", j, dir).Run(); err == nil {
		if !fdsecRefusedReparse(j) {
			t.Error("a junction must be refused")
		}
	}
}

// TestSecureKeepsAnOriginalEditedAfterThePack is FDSEC-12: an original that
// changes between the read-back and the disposition is kept.
func TestSecureKeepsAnOriginalEditedAfterThePack(t *testing.T) {
	saved := globalInterruptHandler
	globalInterruptHandler = newInterruptHandlerNoSignals()
	defer func() { globalInterruptHandler = saved }()
	fdsecBeforeDisposition = func(p string) {
		f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		if err == nil {
			f.WriteString(" - edited after the pack")
			f.Close()
		}
	}
	defer func() { fdsecBeforeDisposition = nil }()

	dir := t.TempDir()
	src := filepath.Join(dir, "doc.txt")
	os.WriteFile(src, []byte("the packed content"), 0o644)
	hl := NewHistoryLogger(nil)
	hl.enabled = false
	o := &fdsecOpts{del: true, assumeYes: true}
	if err := fdsecSecureOne(src, o, fdsec.NewCredential("pw"), hl); err != nil {
		t.Fatalf("secure: %v", err)
	}
	if !exists(src) {
		t.Fatal("an original edited after the pack was deleted")
	}
	if !exists(filepath.Join(dir, "doc.fd-sec")) {
		t.Fatal("the container is missing")
	}
}

// TestHistoryErrorIsScreened is FDSEC-06: history.json records the screened
// message, never the sealed name an error carries for the console.
func TestHistoryErrorIsScreened(t *testing.T) {
	hl := NewHistoryLogger([]string{"filedo", "x.fd-sec", "unsecure"})
	hl.enabled = true
	hl.SetError(fdsecScreenEventError(os.ErrExist))
	err := fdsecScreenEventError(&os.PathError{Op: "rename", Path: `C:\t\TopSecretMerger.txt`, Err: os.ErrExist})
	hl.SetError(err)
	if strings.Contains(hl.entry.ErrorMsg, "TopSecretMerger") {
		t.Fatalf("history carries the sealed name: %s", hl.entry.ErrorMsg)
	}
	if _, nerr := fdsecSandboxName(strings.Repeat("s", 300) + ".txt"); nerr == nil || strings.Contains(nerr.Error(), "sss") {
		t.Fatalf("a 300-character sealed name must be refused without being echoed: %v", nerr)
	}
}
