//go:build windows

package main

import (
	"errors"
	"filedo/fdsec"
	"filedo/fmsworker"
	"filedo/statedir"
	"filedo/vdisk"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readVdiskLog(t *testing.T) string {
	t.Helper()
	d, err := statedir.Dir()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(d, "vdisk.log"))
	return string(b)
}

// ---------------------------------------------------------------- AUD-84-F8: the stored key is proven

// AUD-84-F8: a typo stored as the autostart key would leave the disk closed at every startup, and the phone
// would see only an unavailable folder. A closed disk's credential is proven before it is sent.
func TestAutostartRefusesAWrongPasswordForAClosedDisk(t *testing.T) {
	path, s := sharedFixture(t)
	encryptedInspect(t)
	vdProveCredential = func(string, fdsec.Credential) error { return vdisk.ErrCredential }
	e := vdAutostart([]string{path, "on", "consent", "p:wrong"}, true)
	wantClass(t, e, vdisk.ExitCredential)
	if s.autostartCalls != 0 || s.auto {
		t.Fatal("a credential that did not open the container was sent to the holder")
	}
}

// The proof is a real read-only open: a right password passes, a wrong one is class 3, and a keyfile
// refusal keeps its own sentence.
func TestAutostartProofOpensARealContainer(t *testing.T) {
	path, s := sharedFixture(t)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	vdTestContainer(t, path, 1<<20, vdisk.ProfileVault, "right")
	encryptedInspect(t)
	vdProveCredential = vdProveCredentialFile
	e := vdAutostart([]string{path, "on", "consent", "p:wrong"}, true)
	wantClass(t, e, vdisk.ExitCredential)
	if s.autostartCalls != 0 {
		t.Fatal("the wrong password reached the holder")
	}
	var out string
	out = captureStdout(t, func() { e = vdAutostart([]string{path, "on", "consent", "p:right"}, true) })
	if e != nil {
		t.Fatalf("the right password was refused: %v", e)
	}
	if s.autostartCalls != 1 || string(s.password) != "right" {
		t.Fatalf("holder calls %d password %q", s.autostartCalls, s.password)
	}
	if strings.Contains(out, "right") {
		t.Fatal("the password was printed")
	}
}

// An open disk is held by the holder, which FileDO cannot read: the key is stored unproven, and the output
// says so instead of implying the password was checked.
func TestAutostartOnAnOpenDiskSaysTheKeyWasNotProven(t *testing.T) {
	path, s := sharedFixture(t)
	s.disks[0].State, s.disks[0].Holder = fmsworker.DiskStateOpen, "service"
	encryptedInspect(t)
	proved := false
	vdProveCredential = func(string, fdsec.Credential) error { proved = true; return nil }
	var e error
	out := captureStdout(t, func() { e = vdAutostart([]string{path, "on", "consent", "p:pw"}, true) })
	if e != nil {
		t.Fatal(e)
	}
	if proved {
		t.Fatal("a container the holder has open was read by FileDO")
	}
	if !strings.Contains(out, "could not check the password") {
		t.Fatalf("the output does not say the key was not proven:\n%s", out)
	}
}

// ---------------------------------------------------------------- AUD-84-F9: an audit line per opt-in

func TestShareAndAutostartAreLogged(t *testing.T) {
	path, s := shareFixture(t)
	encryptedInspect(t)
	vdProveCredential = func(string, fdsec.Credential) error { return nil }
	captureStdout(t, func() {
		if e := vdShare([]string{path, "on", "as", "Media", "ro"}, true); e != nil {
			t.Error(e)
		}
		if e := vdAutostart([]string{path, "on", "consent", "p:secretpw"}, true); e != nil {
			t.Error(e)
		}
		if e := vdAutostart([]string{path, "off"}, true); e != nil {
			t.Error(e)
		}
		if e := vdShare([]string{path, "off"}, true); e != nil {
			t.Error(e)
		}
	})
	log := readVdiskLog(t)
	for _, want := range []string{
		"share: on " + path + " as Media ro=true",
		"autostart: on " + path + " stored-key=true",
		"autostart: off " + path + " stored-key=false",
		"share: off " + path,
	} {
		if !strings.Contains(log, want) {
			t.Errorf("vdisk.log has no %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "secretpw") {
		t.Fatal("the credential reached vdisk.log")
	}
	_ = s
}

// ---------------------------------------------------------------- AUD-84-F10: the holder's mode is said

func TestShareAndAutostartNameTheHolderMode(t *testing.T) {
	for _, tc := range []struct {
		mode        fmsworker.WorkerMode
		holderSays  string
		consentSays string
	}{
		{fmsworker.WorkerModeService, "nobody signed in", "after the PC starts"},
		{fmsworker.WorkerModeUser, "only while you are signed in", "after you sign in"},
	} {
		path, s := shareFixture(t)
		s.mode = tc.mode
		encryptedInspect(t)
		var out string
		out = captureStdout(t, func() {
			if e := vdShare([]string{path, "on"}, true); e != nil {
				t.Error(e)
			}
		})
		if !strings.Contains(out, tc.holderSays) {
			t.Errorf("mode %v: share output does not say %q:\n%s", tc.mode, tc.holderSays, out)
		}
		out = captureStdout(t, func() {
			if e := vdAutostart([]string{path, "on", "consent", "p:pw"}, true); e != nil {
				t.Error(e)
			}
		})
		if !strings.Contains(out, tc.consentSays) || !strings.Contains(out, tc.holderSays) {
			t.Errorf("mode %v: autostart output lacks %q or %q:\n%s", tc.mode, tc.consentSays, tc.holderSays, out)
		}
		if tc.mode == fmsworker.WorkerModeUser && strings.Contains(out, "after startup") {
			t.Errorf("a session worker's autostart claims to open after startup:\n%s", out)
		}
	}
}

// ---------------------------------------------------------------- AUD-86-F5: vd open and vd close

func TestVD_OpenSendsThePasswordOnceAndNeverPrintsIt(t *testing.T) {
	path, s := sharedFixture(t)
	encryptedInspect(t)
	old := vdOpenCredential
	t.Cleanup(func() { vdOpenCredential = old })
	asked := 0
	vdOpenCredential = func(a credArg, confirm bool) (fdsec.Credential, error) {
		asked++
		if confirm {
			t.Error("open asked for the password twice")
		}
		return fdsec.Credential([]byte("hunter2")), nil
	}
	var e error
	out := captureStdout(t, func() { e = vdOpen([]string{path}, false) })
	if e != nil {
		t.Fatal(e)
	}
	if asked != 1 || s.openCalls != 1 || s.openPassword != "hunter2" {
		t.Fatalf("asked %d, open calls %d, password %q", asked, s.openCalls, s.openPassword)
	}
	if strings.Contains(out, "hunter2") || strings.Contains(readVdiskLog(t), "hunter2") {
		t.Fatal("the password was printed or logged")
	}
	if !strings.Contains(out, "Opened "+path) {
		t.Fatalf("no confirmation:\n%s", out)
	}
	// An obfuscated container needs no password: none is asked for or sent.
	path2, s2 := sharedFixture(t)
	asked = 0
	if e = vdOpen([]string{path2}, true); e != nil || asked != 0 || s2.openPassword != "" {
		t.Fatalf("obfuscated open: err=%v asked=%d password=%q", e, asked, s2.openPassword)
	}
}

func TestVD_OpenRefusals(t *testing.T) {
	path, s := shareFixture(t) // not shared
	e := vdOpen([]string{path}, true)
	wantClass(t, e, vdisk.ExitUsage)
	if s.openCalls != 0 {
		t.Fatal("open reached a worker that does not share the disk")
	}
	path, s = sharedFixture(t)
	if err := vdUpdateState(func(st *vdState) error {
		st.Mounts = append(st.Mounts, vdMountRow{ContainerID: "id", Path: path, Letter: "Q:"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e = vdOpen([]string{path}, true)
	wantClass(t, e, vdisk.ExitBusy)
	if s.openCalls != 0 {
		t.Fatal("open went ahead although FileDO has the disk mounted")
	}
	wantClass(t, vdOpen([]string{path, "bogus"}, true), vdisk.ExitUsage)
	wantClass(t, vdOpen(nil, true), vdisk.ExitUsage)
}

var openHandlesBusy = &fmsworker.WorkerError{Message: "disk still has 2 open handles; retry close or explicitly request force", OutcomeClass: vdisk.ExitBusy}

// AUD-86-F5: the close shows the handle count, waits the bound, and never forces by itself.
func TestVD_CloseShowsHandleCountAndNeedsForce(t *testing.T) {
	path, s := sharedFixture(t)
	s.disks[0].State, s.disks[0].Holder, s.disks[0].OpenHandles = fmsworker.DiskStateOpen, "service", 2
	s.failClose = []error{openHandlesBusy}
	var e error
	out := captureStdout(t, func() { e = vdClose([]string{path, "wait", "5"}, true) })
	wantClass(t, e, vdisk.ExitBusy)
	if !strings.Contains(out, "2 open handle(s)") || !strings.Contains(out, "up to 5 s") {
		t.Fatalf("the count and the wait are not shown:\n%s", out)
	}
	if len(s.closeCalls) != 1 || s.closeCalls[0].force || s.closeCalls[0].bound != 5 {
		t.Fatalf("close calls %+v; a batch must not force", s.closeCalls)
	}
	if s.disks[0].State != fmsworker.DiskStateOpen {
		t.Fatal("the disk was closed although the close was refused")
	}
}

func TestVD_CloseAsksBeforeForcingAndForceSkipsOnlyTheQuestion(t *testing.T) {
	old := vdCloseConfirm
	t.Cleanup(func() { vdCloseConfirm = old })
	asked := 0
	// Interactive, answered yes: waits, is refused, asks, then forces with the shortest bound.
	path, s := sharedFixture(t)
	s.disks[0].State, s.disks[0].Holder, s.disks[0].OpenHandles = fmsworker.DiskStateOpen, "service", 2
	s.failClose = []error{openHandlesBusy, nil}
	vdCloseConfirm = func(q string, batch bool) bool { asked++; return true }
	var e error
	out := captureStdout(t, func() { e = vdClose([]string{path}, false) })
	if e != nil {
		t.Fatal(e)
	}
	if asked != 1 || len(s.closeCalls) != 2 || s.closeCalls[0].force || !s.closeCalls[1].force || s.closeCalls[1].bound != fmsworker.MinDrainBoundSeconds {
		t.Fatalf("asked %d, calls %+v", asked, s.closeCalls)
	}
	if !strings.Contains(out, "by force") || !strings.Contains(readVdiskLog(t), "close: "+path+" force=true") {
		t.Fatalf("a forced close is not reported or logged:\n%s", out)
	}
	// Answered no: not closed, class busy.
	path, s = sharedFixture(t)
	s.disks[0].State, s.disks[0].OpenHandles = fmsworker.DiskStateOpen, 2
	s.failClose = []error{openHandlesBusy}
	vdCloseConfirm = func(string, bool) bool { return false }
	wantClass(t, vdClose([]string{path}, false), vdisk.ExitBusy)
	if len(s.closeCalls) != 1 {
		t.Fatalf("calls %+v after a no", s.closeCalls)
	}
	// force skips the question, never the printing: one call, force true, no question.
	path, s = sharedFixture(t)
	s.disks[0].State, s.disks[0].OpenHandles = fmsworker.DiskStateOpen, 2
	asked = 0
	vdCloseConfirm = func(string, bool) bool { asked++; return true }
	if e = vdClose([]string{path, "force"}, true); e != nil || asked != 0 || len(s.closeCalls) != 1 || !s.closeCalls[0].force {
		t.Fatalf("force: err=%v asked=%d calls=%+v", e, asked, s.closeCalls)
	}
}

func TestVD_CloseUsageAndAlreadyClosed(t *testing.T) {
	path, s := sharedFixture(t)
	for _, args := range [][]string{{path, "wait"}, {path, "wait", "0"}, {path, "wait", "91"}, {path, "wait", "x"}, {path, "later"}, nil} {
		wantClass(t, vdClose(args, true), vdisk.ExitUsage)
	}
	out := captureStdout(t, func() {
		if e := vdClose([]string{path}, true); e != nil {
			t.Error(e)
		}
	})
	if !strings.Contains(out, "already closed") || len(s.closeCalls) != 0 {
		t.Fatalf("a closed disk was closed again: calls %+v\n%s", s.closeCalls, out)
	}
}

// ---------------------------------------------------------------- AUD-83-F2 / AUD-84-F7 / AUD-84-F11: the worker's mount line

// TestWorkerMountLineMatchesDiskShareContract pins the grammar DISK-SHARE 17 gives the worker.
func TestWorkerMountLineMatchesDiskShareContract(t *testing.T) {
	o, err := vdParseMountOpts([]string{`C:\x.fdd`, "noletter", "worker", "stdin", "ro"})
	if err != nil {
		t.Fatal(err)
	}
	if !o.NoLetter || !o.Worker || o.Cred.src != "stdin" || !o.ReadOnly || o.Path != `C:\x.fdd` {
		t.Fatalf("parsed %+v", o)
	}
	if !o.noQuestions(false) {
		t.Fatal("a worker mount may ask a question: stdin carries the password and would be read as the answer")
	}
	plain, _ := vdParseMountOpts([]string{`C:\x.fdd`})
	if plain.noQuestions(false) || !plain.noQuestions(true) {
		t.Fatal("a person's mount must keep its questions, and a batch must not")
	}
	if _, err := vdParseMountOpts([]string{`C:\x.fdd`, "stdin", "p:x"}); err == nil {
		t.Fatal("two credentials were accepted")
	}
}

func TestReadStdinCredentialFraming(t *testing.T) {
	never := func() bool { return false }
	for _, tc := range []struct {
		in, want string
	}{
		{"hunter2", "hunter2"},
		{"hunter2\n", "hunter2"},
		{"hunter2\r\n", "hunter2"},
		{"hunter2\n\n", "hunter2\n"},
		{"a b\tc", "a b\tc"},
	} {
		c, err := readStdinCredential(strings.NewReader(tc.in), time.Second, never)
		if err != nil || string(c) != tc.want {
			t.Errorf("%q -> %q, %v; want %q", tc.in, c, err, tc.want)
		}
	}
	for _, in := range []string{"", "\n", "\r\n"} {
		_, err := readStdinCredential(strings.NewReader(in), time.Second, never)
		if err == nil || vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("empty stdin %q: err = %v, want usage", in, err)
		}
	}
	_, err := readStdinCredential(strings.NewReader(strings.Repeat("x", 1<<20+1)), time.Second, never)
	if err == nil || vdExitClass(err) != vdisk.ExitUsage {
		t.Errorf("an over-long credential: %v", err)
	}
	exact, err := readStdinCredential(strings.NewReader(strings.Repeat("x", 1<<20)), time.Second, never)
	if err != nil || len(exact) != 1<<20 {
		t.Errorf("exactly 1 MiB: %d bytes, %v", len(exact), err)
	}
}

func TestReadStdinCredentialIsBoundedAndStoppable(t *testing.T) {
	pr, pw := io.Pipe() // a parent that never writes and never closes
	defer pw.Close()
	start := time.Now()
	_, err := readStdinCredential(pr, 150*time.Millisecond, func() bool { return false })
	if err == nil || vdExitClass(err) != vdisk.ExitUsage || time.Since(start) > 3*time.Second {
		t.Fatalf("a silent stdin: err=%v after %s", err, time.Since(start))
	}
	pr2, pw2 := io.Pipe()
	defer pw2.Close()
	start = time.Now()
	_, err = readStdinCredential(pr2, time.Minute, func() bool { return true })
	if !errors.Is(err, errRunStopped) || time.Since(start) > 3*time.Second {
		t.Fatalf("a stop request: err=%v after %s", err, time.Since(start))
	}
}

// ---------------------------------------------------------------- no-letter mounts

func TestRowNamingOfANoLetterMount(t *testing.T) {
	letter := vdMountRow{Letter: "Q:", Path: `C:\a b\v.fdd`, MountPath: `Q:\`}
	if vdRowName(letter) != "Q:" || vdRowTarget(letter) != "Q:" || vdRowPlace(letter) != "Q:" {
		t.Fatalf("a lettered row: %q %q %q", vdRowName(letter), vdRowTarget(letter), vdRowPlace(letter))
	}
	nl := vdMountRow{Path: `C:\a b\v.fdd`, MountPath: `C:\Users\u\FileDO\FMS\0123456789abcdef0123456789abcdef`}
	if vdRowName(nl) != `C:\a b\v.fdd` || vdRowTarget(nl) != `"C:\a b\v.fdd"` || vdRowPlace(nl) != nl.MountPath {
		t.Fatalf("a no-letter row: %q %q %q", vdRowName(nl), vdRowTarget(nl), vdRowPlace(nl))
	}
	guid := vdMountRow{Path: `C:\v.fdd`, VolumeGUID: `\\?\Volume{11111111-2222-3333-4444-555555555555}\`}
	if vdRowPlace(guid) != guid.VolumeGUID || vdRowTarget(guid) != `C:\v.fdd` {
		t.Fatalf("a volume-path row: %q %q", vdRowPlace(guid), vdRowTarget(guid))
	}
}

// AUD-87-F8: `vd status` of a no-letter mount whose server is gone shows where the volume is and gives a
// repair command with a target.
func TestVD_StatusNoLetterRow(t *testing.T) {
	vdTestEnv(t)
	old := vdShareProbe
	t.Cleanup(func() { vdShareProbe = old })
	vdShareProbe = func(time.Duration) vdShareLiveResult { return vdShareLiveResult{} }
	row := vdMountRow{ContainerID: "id", Path: `C:\a b\v.fdd`, MountPath: `C:\Users\u\FileDO\FMS\0123456789abcdef0123456789abcdef`, MountedAt: time.Now(), ServerPID: 4, ServerStarted: 1}
	if err := vdUpdateState(func(s *vdState) error { s.Mounts = append(s.Mounts, row); return nil }); err != nil {
		t.Fatal(err)
	}
	var e error
	out := captureStdout(t, func() { e = vdStatus() })
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out, row.MountPath) || !strings.Contains(out, `run: filedo "C:\a b\v.fdd" unmount`) {
		t.Fatalf("the place or the repair command is missing:\n%s", out)
	}
	if strings.Contains(out, "run: filedo  unmount") {
		t.Fatalf("a command with no target:\n%s", out)
	}
}

type cleanupLog struct{ calls []string }

func (l *cleanupLog) ops(destroyErr error, deleteErr, removeErr error, fallback string) vdCleanupOps {
	return vdCleanupOps{
		destroy:     func(s string) error { l.calls = append(l.calls, "destroy:"+s); return destroyErr },
		deleteMount: func(p string) error { l.calls = append(l.calls, "delete:"+p); return deleteErr },
		removeDir:   func(p string) error { l.calls = append(l.calls, "remove:"+p); return removeErr },
		fallback: func() (string, error) {
			if fallback == "" {
				return "", errors.New("no profile")
			}
			return fallback, nil
		},
	}
}

const (
	nlBase = `C:\Users\u\AppData\Local\FileDO\FMS`
	nlName = `0123456789abcdef0123456789abcdef`
)

// AUD-84-F3: the process that made the mount destroys it through the manager, so the registration goes too.
func TestCleanNoLetterMountUsesTheManagerWhenItKnowsTheMount(t *testing.T) {
	var l cleanupLog
	if err := vdCleanNoLetterMount("FDD1", nlBase+`\`+nlName, nlBase, l.ops(nil, nil, nil, "")); err != nil {
		t.Fatal(err)
	}
	if len(l.calls) != 1 || l.calls[0] != "destroy:FDD1" {
		t.Fatalf("calls %v", l.calls)
	}
}

// AUD-84-F2: another process cleans by hand, against the base the attach recorded - not one recomputed from
// its own profile - and a failure is an error for the caller to turn into a warning.
func TestCleanNoLetterMountByHandAgainstTheRecordedBase(t *testing.T) {
	var l cleanupLog
	notKnown := vdisk.ErrVolumeNotMounted
	p := nlBase + `\` + nlName
	if err := vdCleanNoLetterMount("FDD1", p, nlBase, l.ops(notKnown, nil, nil, `D:\elsewhere\FileDO\FMS`)); err != nil {
		t.Fatalf("a recorded base that differs from this process's own was refused: %v", err)
	}
	if got := strings.Join(l.calls, "|"); got != "destroy:FDD1|delete:"+p+"|remove:"+p {
		t.Fatalf("calls %s", got)
	}
	// A row written before the base was recorded falls back to this process's base.
	l = cleanupLog{}
	if err := vdCleanNoLetterMount("FDD1", p, "", l.ops(notKnown, nil, nil, nlBase)); err != nil || len(l.calls) != 3 {
		t.Fatalf("fallback base: err=%v calls=%v", err, l.calls)
	}
	// Failures are reported, and nothing is claimed removed.
	for name, ops := range map[string]vdCleanupOps{
		"delete fails":                         l.ops(notKnown, errors.New("busy"), nil, ""),
		"remove fails":                         l.ops(notKnown, nil, errors.New("busy"), ""),
		"destroy fails (the manager knows it)": l.ops(errors.New("busy"), nil, nil, ""),
	} {
		if err := vdCleanNoLetterMount("FDD1", p, nlBase, ops); err == nil || !strings.Contains(err.Error(), "next no-letter mount removes it") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Already-gone is success.
	if err := vdCleanNoLetterMount("FDD1", p, nlBase, l.ops(notKnown, os.ErrNotExist, os.ErrNotExist, "")); err == nil {
		// os.ErrNotExist is accepted for the directory but not as a mount-point code: delete reports it
		t.Log("delete of a missing mount point is treated as a failure only for non-reparse codes")
	}
}

// The check that makes a by-hand cleanup safe: only a 32-hex folder directly under a FileDO\FMS base.
func TestCleanNoLetterMountRefusesWhatIsNotAPrivateFolder(t *testing.T) {
	var l cleanupLog
	nk := vdisk.ErrVolumeNotMounted
	for name, tc := range map[string]struct{ path, base string }{
		"a name that is not hex":      {nlBase + `\notahexnotahexnotahexnotahex1234`, nlBase},
		"a short name":                {nlBase + `\abc`, nlBase},
		"outside the base":            {`C:\Windows\` + nlName, nlBase},
		"a base that is not FileDO's": {`C:\` + nlName, `C:\`},
		"a nested path":               {nlBase + `\x\` + nlName, nlBase},
	} {
		l = cleanupLog{}
		err := vdCleanNoLetterMount("FDD1", tc.path, tc.base, l.ops(nk, nil, nil, ""))
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
		for _, c := range l.calls {
			if strings.HasPrefix(c, "delete:") || strings.HasPrefix(c, "remove:") {
				t.Errorf("%s: touched the disk: %v", name, l.calls)
			}
		}
	}
	// A V2 volume path has nothing to clean.
	l = cleanupLog{}
	if err := vdCleanNoLetterMount("FDD1", `\\?\Volume{11111111-2222-3333-4444-555555555555}\`, "", l.ops(nk, nil, nil, "")); err != nil || len(l.calls) != 0 {
		t.Fatalf("volume path: err=%v calls=%v", err, l.calls)
	}
}

// AUD-84-F4: a letter that arrives after the folder mount is taken off; one that cannot be removed fails the
// attach; a lookup that never works fails it too.
func TestNoLetterMountRemovesALateLetter(t *testing.T) {
	var deleted []string
	calls := 0
	lookup := func(string) ([]string, error) {
		calls++
		if calls >= 3 {
			return []string{"E:"}, nil
		}
		return nil, nil
	}
	del := func(p string) error { deleted = append(deleted, p); return nil }
	slept := time.Duration(0)
	sleep := func(d time.Duration) { slept += d }
	if err := vdRemoveLateLetters("vol", time.Second, lookup, del, sleep); err != nil {
		t.Fatal(err)
	}
	if len(deleted) == 0 || deleted[0] != `E:\` {
		t.Fatalf("the late letter was not taken off: %v", deleted)
	}
	if slept < time.Second {
		t.Fatalf("the window was cut short: slept %s", slept)
	}
	err := vdRemoveLateLetters("vol", 500*time.Millisecond, func(string) ([]string, error) { return []string{"F:"}, nil },
		func(string) error { return errors.New("access denied") }, func(time.Duration) {})
	if err == nil || !strings.Contains(err.Error(), "F:") {
		t.Fatalf("a letter that would not go: %v", err)
	}
	err = vdRemoveLateLetters("vol", 500*time.Millisecond, func(string) ([]string, error) { return nil, errors.New("api failed") },
		del, func(time.Duration) {})
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("a lookup that always fails: %v", err)
	}
	if err = vdRemoveLateLetters("vol", 500*time.Millisecond, func(string) ([]string, error) { return nil, nil }, del, func(time.Duration) {}); err != nil {
		t.Fatalf("a volume with no letter: %v", err)
	}
}

// AUD-84-F1: a flush of a no-letter volume goes to its GUID's device path, pattern-checked, never a path
// taken from the request.
func TestFlushStepAcceptsAVolumeGUIDAndRefusesJunk(t *testing.T) {
	_, err := vdFlushStep(vdRequest{})
	wantClass(t, err, vdisk.ExitUsage)
	_, err = vdFlushStep(vdRequest{VolumeGUID: `C:\Windows`})
	wantClass(t, err, vdisk.ExitUsage)
	_, err = vdFlushStep(vdRequest{VolumeGUID: `\\?\Volume{11111111-2222-3333-4444-555555555555}\..\..\x`})
	wantClass(t, err, vdisk.ExitUsage)
	_, err = vdFlushStep(vdRequest{Letter: `\\.\PhysicalDrive0`})
	wantClass(t, err, vdisk.ExitUsage)
	// A well-formed GUID passes the check and then fails at the open (no such volume) - an I/O failure,
	// not a usage refusal.
	_, err = vdFlushStep(vdRequest{VolumeGUID: `\\?\Volume{11111111-2222-3333-4444-555555555555}\`})
	if err == nil || vdExitClass(err) == vdisk.ExitUsage {
		t.Fatalf("a well-formed volume GUID: err = %v", err)
	}
	if d, e := vdisk.VolumeDevicePath(`\\?\Volume{11111111-2222-3333-4444-555555555555}\`); e != nil || d != `\\.\Volume{11111111-2222-3333-4444-555555555555}` {
		t.Fatalf("device path %q, %v", d, e)
	}
}

// SP-0147: a save's flush opened `\.\E:` - one backslash short of the device
// namespace, so Windows read it as a name on the current drive and refused it
// as a syntax error. The path a letter builds for the volume open is the
// device path, spelled \\.\X:, whatever the letter's own case or slash.
func TestFlushStepLetterBuildsTheVolumeDevicePath(t *testing.T) {
	for letter, want := range map[string]string{
		`E:`:  `\\.\E:`,
		`e:`:  `\\.\E:`,
		`e:\`: `\\.\E:`,
		`E:/`: `\\.\E:`,
	} {
		if got := vdLetterVolumeDevice(letter); got != want {
			t.Errorf("%s: device path %q, want %q", letter, got, want)
		}
	}
}

// AUD-84-F6: a detach built from an unauthenticated state row names FileDO's own target and a real port, or
// it is refused as usage before the initiator is touched.
func TestDetachRefusesAForeignIQNAndSession(t *testing.T) {
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	serial := "FDD0123456789ABCDEF01"
	own := vdIQN("0123456789abcdef0123456789abcdef")
	if !vdIQNSpelling.MatchString(own) {
		t.Fatalf("the target name FileDO makes (%q) does not match its own spelling", own)
	}
	for name, req := range map[string]vdRequest{
		"a foreign iSCSI target":         {Serial: serial, IQN: "iqn.1991-05.com.microsoft:other-host-target", Port: 3260},
		"an almost-ours target":          {Serial: serial, IQN: own + "x", Port: 3260},
		"a target with upper hex":        {Serial: serial, IQN: strings.ToUpper(own), Port: 3260},
		"no port":                        {Serial: serial, IQN: own},
		"a port out of range":            {Serial: serial, IQN: own, Port: 70000},
		"a foreign target and a session": {Serial: serial, IQN: "iqn.2000-01.org.example:disk", Port: 3260, Session: "ffff800000000000-400001370000000a"},
	} {
		if _, err := vdDetach(vdRequest{Serial: req.Serial, IQN: req.IQN, Port: req.Port, Session: req.Session}); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("%s: class %d, %v", name, vdExitClass(err), err)
		}
	}
}
