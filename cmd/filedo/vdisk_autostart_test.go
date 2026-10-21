//go:build windows

package main

import (
	"errors"
	"filedo/fdsec"
	"filedo/fmsworker"
	"filedo/vdisk"
	"strings"
	"testing"
)

// encryptedInspect makes the fixture container an encrypted one.
func encryptedInspect(t *testing.T) {
	old := vdShareInspect
	t.Cleanup(func() { vdShareInspect = old })
	vdShareInspect = func(string) (vdisk.Info, error) { return vdisk.Info{ContainerID: "id", Obfuscated: false}, nil }
}

func TestEncryptedAutostartAlwaysRequiresConsent(t *testing.T) {
	for _, provided := range []bool{false, true} {
		t.Run(map[bool]string{false: "prompt", true: "provided"}[provided], func(t *testing.T) {
			path, s := sharedFixture(t)
			encryptedInspect(t)
			old := vdAutostartConsent
			t.Cleanup(func() { vdAutostartConsent = old })
			called := false
			vdAutostartConsent = func(string, bool) bool { called = true; return false }
			args := []string{path, "on"}
			if provided {
				args = append(args, "p:credential")
			}
			e := vdAutostart(args, true)
			wantClass(t, e, vdisk.ExitUsage)
			if !strings.Contains(e.Error(), "consent") {
				t.Fatalf("refusal does not say why: %v", e)
			}
			if !called || s.auto || s.autostartCalls != 0 {
				t.Fatal("provided credential bypassed consent")
			}
		})
	}
}

func TestEncryptedAutostartStoresOnlyAfterConsent(t *testing.T) {
	path, s := sharedFixture(t)
	encryptedInspect(t)
	old := vdAutostartCredential
	t.Cleanup(func() { vdAutostartCredential = old })
	password := fdsec.Credential([]byte("credential"))
	vdAutostartCredential = func(credArg, bool) (fdsec.Credential, error) { return password, nil }
	var e error
	out := captureStdout(t, func() { e = vdAutostart([]string{path, "on", "consent", "p:credential"}, true) })
	if e != nil {
		t.Fatal(e)
	}
	if !s.auto || string(s.password) != "credential" {
		t.Fatal("credential not delivered to holder")
	}
	if !strings.Contains(out, "Autostart enabled for "+path) {
		t.Fatalf("no confirmation:\n%s", out)
	}
	for _, b := range password {
		if b != 0 {
			t.Fatal("credential buffer not cleared")
		}
	}
	d, ok := snapshotState(t, path)
	if !ok || !d.Autostart || !d.HasStoredKey {
		t.Fatalf("snapshot flags after autostart on: %+v", d)
	}
	if s.disks[0].Encrypted = true; vdShareStates(s.disks)[0].Encrypted != true {
		t.Fatal("the encrypted flag is not copied into the snapshot record")
	}
	out = captureStdout(t, func() { e = vdAutostart([]string{path, "off"}, true) })
	if e != nil {
		t.Fatal(e)
	}
	if s.auto || len(s.password) != 0 || s.disks[0].Autostart || s.disks[0].HasStoredKey {
		t.Fatal("disable retained password")
	}
	if !strings.Contains(out, "destroyed its stored key") {
		t.Fatalf("no confirmation:\n%s", out)
	}
	d, ok = snapshotState(t, path)
	if !ok || d.Autostart || d.HasStoredKey {
		t.Fatalf("snapshot flags after autostart off: %+v", d)
	}
}

// What the command prints about the key and the mark is what the holder's own
// record says after the call, never what was merely requested.
func TestAutostartConfirmationIsVerified(t *testing.T) {
	t.Run("off with the key kept", func(t *testing.T) {
		path, s := sharedFixture(t)
		s.disks[0].Autostart, s.disks[0].HasStoredKey = true, true
		s.keepKey = true
		var e error
		out := captureStdout(t, func() { e = vdAutostart([]string{path, "off"}, true) })
		if e == nil || !strings.Contains(e.Error(), "stored key") || !strings.Contains(e.Error(), "not destroyed") {
			t.Fatalf("a kept key was not reported: %v", e)
		}
		if strings.Contains(out, "destroyed") || strings.Contains(out, "disabled") {
			t.Fatalf("unverified confirmation printed:\n%s", out)
		}
	})
	t.Run("off with the mark kept", func(t *testing.T) {
		path, s := sharedFixture(t)
		s.disks[0].Autostart = true
		s.ignoreAutostart = true
		var e error
		out := captureStdout(t, func() { e = vdAutostart([]string{path, "off"}, true) })
		if e == nil || !strings.Contains(e.Error(), "did not disable") || strings.Contains(out, "disabled") {
			t.Fatalf("a kept mark was not reported: %v\n%s", e, out)
		}
	})
	t.Run("on without the mark", func(t *testing.T) {
		path, s := sharedFixture(t)
		s.ignoreAutostart = true
		var e error
		out := captureStdout(t, func() { e = vdAutostart([]string{path, "on"}, true) })
		if e == nil || !strings.Contains(e.Error(), "did not enable") || strings.Contains(out, "enabled") {
			t.Fatalf("a missing mark was not reported: %v\n%s", e, out)
		}
	})
	t.Run("on encrypted without a stored key", func(t *testing.T) {
		path, s := sharedFixture(t)
		encryptedInspect(t)
		old := vdAutostartCredential
		t.Cleanup(func() { vdAutostartCredential = old })
		vdAutostartCredential = func(credArg, bool) (fdsec.Credential, error) { return fdsec.Credential(nil), nil }
		var e error
		out := captureStdout(t, func() { e = vdAutostart([]string{path, "on", "consent"}, true) })
		if s.autostartCalls != 1 || e == nil || !strings.Contains(e.Error(), "no stored key") || strings.Contains(out, "enabled") {
			t.Fatalf("a missing key was not reported: %v\n%s", e, out)
		}
	})
	t.Run("status cannot be read back", func(t *testing.T) {
		path, s := sharedFixture(t)
		s.disks[0].Autostart = true
		wrap := &statusAfterSet{shareWorkerStub: s}
		vdWorkerFactory = func() (vdWorker, error) { return wrap, nil }
		var e error
		out := captureStdout(t, func() { e = vdAutostart([]string{path, "off"}, true) })
		if e == nil || !strings.Contains(e.Error(), "confirm") || strings.Contains(out, "disabled") {
			t.Fatalf("an unconfirmed result was reported as done: %v\n%s", e, out)
		}
	})
}

// statusAfterSet answers the first status call and fails the later ones.
type statusAfterSet struct {
	*shareWorkerStub
	n int
}

func (w *statusAfterSet) GetDiskStatus(p string) (*fmsworker.SharedDiskInfo, error) {
	w.n++
	if w.n > 1 {
		return nil, errors.New("status unavailable")
	}
	return w.shareWorkerStub.GetDiskStatus(p)
}

func TestPlainAutostartDoesNotReadCredential(t *testing.T) {
	path, s := sharedFixture(t)
	old := vdAutostartCredential
	t.Cleanup(func() { vdAutostartCredential = old })
	vdAutostartCredential = func(credArg, bool) (fdsec.Credential, error) {
		t.Fatal("plain disk requested password")
		return nil, nil
	}
	oldConsent := vdAutostartConsent
	t.Cleanup(func() { vdAutostartConsent = oldConsent })
	vdAutostartConsent = func(string, bool) bool { t.Fatal("plain disk asked for consent"); return false }
	if e := vdAutostart([]string{path, "on"}, true); e != nil {
		t.Fatal(e)
	}
	if !s.auto || len(s.password) != 0 {
		t.Fatal("plain autostart incorrect")
	}
	wantClass(t, vdAutostart([]string{path, "on", "p:x"}, true), vdisk.ExitUsage)
}

// DISK-SHARE-21: the existing password is asked for once, not typed twice.
func TestAutostartPromptsForThePasswordOnce(t *testing.T) {
	path, s := sharedFixture(t)
	encryptedInspect(t)
	oldC, oldK := vdAutostartConsent, vdAutostartCredential
	t.Cleanup(func() { vdAutostartConsent, vdAutostartCredential = oldC, oldK })
	asked, confirm, given := 0, true, true
	vdAutostartConsent = func(string, bool) bool { return true }
	vdAutostartCredential = func(a credArg, c bool) (fdsec.Credential, error) {
		asked++
		confirm, given = c, a.given()
		return fdsec.Credential("typed"), nil
	}
	if e := vdAutostart([]string{path, "on"}, false); e != nil {
		t.Fatal(e)
	}
	if asked != 1 || confirm || given {
		t.Fatalf("prompt path: asked %d times, confirm=%v, given=%v", asked, confirm, given)
	}
	if string(s.password) != "typed" {
		t.Fatal("typed password not delivered")
	}
}

func TestAutostartExplainsKeyStorageAndAcceptsInteractiveConsent(t *testing.T) {
	path, s := sharedFixture(t)
	encryptedInspect(t)
	oldC, oldK := vdAutostartConsent, vdAutostartCredential
	t.Cleanup(func() { vdAutostartConsent, vdAutostartCredential = oldC, oldK })
	question, batchSeen := "", true
	vdAutostartConsent = func(q string, batch bool) bool { question, batchSeen = q, batch; return true }
	vdAutostartCredential = func(credArg, bool) (fdsec.Credential, error) { return fdsec.Credential("pw"), nil }
	var e error
	out := captureStdout(t, func() { e = vdAutostart([]string{path, "on"}, false) })
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"protected store on this PC", "without anyone typing its password", "protects a copy of the closed container"} {
		if !strings.Contains(out, want) {
			t.Fatalf("warning lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(question, "credential") || batchSeen {
		t.Fatalf("consent asked as %q (batch=%v)", question, batchSeen)
	}
	if !s.auto || string(s.password) != "pw" {
		t.Fatal("consent given, nothing stored")
	}
	// A plain container has no key to store, so says nothing of one.
	path2, _ := sharedFixture(t)
	out = captureStdout(t, func() { _ = vdAutostart([]string{path2, "on"}, true) })
	if strings.Contains(out, "protected store") {
		t.Fatalf("plain container got the key warning:\n%s", out)
	}
}

func TestAutostartFailuresDoNotReportSuccess(t *testing.T) {
	path, s := sharedFixture(t)
	for _, args := range [][]string{{}, {path, "bad"}, {path, "off", "p:credential"}, {path, "on", "unexpected"}, {path, "on", "consent", "consent"}, {path, "off", "consent"}} {
		wantClass(t, vdAutostart(args, true), vdisk.ExitUsage)
	}
	if s.autostartCalls != 0 {
		t.Fatal("a rejected command reached the worker")
	}
}

func TestAutostartWorkerErrorChangesNothing(t *testing.T) {
	path, s := sharedFixture(t)
	if e := vdRefreshShareSnapshot(s); e != nil {
		t.Fatal(e)
	}
	s.failAutostart = &fmsworker.WorkerError{Message: "key store refused", OutcomeClass: vdisk.ExitCredential}
	var e error
	out := captureStdout(t, func() { e = vdAutostart([]string{path, "on"}, true) })
	wantClass(t, e, vdisk.ExitCredential)
	if s.autostartCalls != 1 || s.auto {
		t.Fatalf("the worker call did not fail as intended: %+v", s)
	}
	if strings.Contains(out, "enabled") {
		t.Fatalf("failure reported as success:\n%s", out)
	}
	if d, ok := snapshotState(t, path); !ok || d.Autostart {
		t.Fatalf("a failed autostart changed the snapshot: %+v", d)
	}
	s.failAutostart = errors.New("plain failure")
	wantClass(t, vdAutostart([]string{path, "on"}, true), vdisk.ExitIO)
}

func TestAutostartNeedsAShare(t *testing.T) {
	t.Run("not shared", func(t *testing.T) {
		path, s := shareFixture(t)
		for _, action := range []string{"on", "off"} {
			e := vdAutostart([]string{path, action}, true)
			if e == nil || !strings.Contains(e.Error(), "share the container first") || !errors.Is(e, fmsworker.ErrDiskNotShared) {
				t.Fatalf("autostart %s of an unshared disk: %v", action, e)
			}
			wantClass(t, e, vdisk.ExitUsage)
		}
		if s.autostartCalls != 0 {
			t.Fatal("an unshared disk reached SetAutostart")
		}
	})
	t.Run("worker text", func(t *testing.T) {
		path, s := shareFixture(t)
		s.failStatus = &fmsworker.WorkerError{Message: "disk is not shared"}
		e := vdAutostart([]string{path, "on"}, true)
		if e == nil || !strings.Contains(e.Error(), "share the container first") {
			t.Fatalf("got %v", e)
		}
		wantClass(t, e, vdisk.ExitUsage)
	})
	t.Run("other status failure", func(t *testing.T) {
		path, s := sharedFixture(t)
		s.failStatus = errors.New("pipe broke")
		e := vdAutostart([]string{path, "on"}, true)
		if e == nil || strings.Contains(e.Error(), "share the container first") {
			t.Fatalf("an unrelated failure got the share hint: %v", e)
		}
		wantClass(t, e, vdisk.ExitIO)
	})
	t.Run("worker unavailable", func(t *testing.T) {
		path, s := sharedFixture(t)
		s.failStatus = fmsworker.ErrWorkerUnavailable
		e := vdAutostart([]string{path, "on"}, true)
		if e == nil || strings.Contains(e.Error(), "share the container first") {
			t.Fatalf("an unavailable worker got the share hint: %v", e)
		}
		wantClass(t, e, vdisk.ExitUnsupported)
	})
	t.Run("no worker at all", func(t *testing.T) {
		path, _ := sharedFixture(t)
		vdWorkerFactory = func() (vdWorker, error) {
			return nil, errors.New("start or update FMS for Windows")
		}
		e := vdAutostart([]string{path, "on"}, true)
		if e == nil || strings.Contains(e.Error(), "share the container first") {
			t.Fatalf("got %v", e)
		}
	})
}

func TestAutostartSnapshotFailureIsOnlyAWarning(t *testing.T) {
	path, s := sharedFixture(t)
	blockSnapshot(t)
	var e error
	out, errOut := captureBoth(t, func() { e = vdAutostart([]string{path, "on"}, true) })
	if e != nil || !s.auto {
		t.Fatalf("a snapshot failure failed the autostart: %v", e)
	}
	if !strings.Contains(out, "Autostart enabled") || !strings.Contains(errOut, "warning:") {
		t.Fatalf("stdout %q stderr %q", out, errOut)
	}
}

func TestWorkerMountParserHasNoInteractiveOrLetterFallback(t *testing.T) {
	o, e := vdParseMountOpts([]string{"a.fdd", "noletter", "worker", "stdin"})
	if e != nil || !o.NoLetter || !o.Worker || o.Cred.src != "stdin" {
		t.Fatalf("worker grammar: %+v %v", o, e)
	}
	if _, e := vdParseMountOpts([]string{"a.fdd", "noletter", "as", "X:"}); e == nil {
		t.Fatal("conflicting mount modes accepted")
	}
}
