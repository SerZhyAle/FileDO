//go:build windows

package main

import (
	"errors"
	"filedo/fmsworker"
	"filedo/fsx"
	"filedo/statedir"
	"filedo/vdisk"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shareWorkerStub is a worker whose disks slice is its truth. Every call has its
// own failure knob, so a test fails exactly the call it means to.
type shareWorkerStub struct {
	roots []string
	disks []fmsworker.SharedDiskInfo

	failShare, failUnshare, failAutostart, failStatus, failList, failRoots error
	keepKey, ignoreAutostart                                                bool

	shared, removed, auto bool
	password              []byte
	ro                    bool
	name                  string
	shareCalls            int
	unshareCalls          int
	autostartCalls        int
}

func (s *shareWorkerStub) find(path string) int {
	for i, d := range s.disks {
		if strings.EqualFold(d.ContainerPath, path) {
			return i
		}
	}
	return -1
}
func (s *shareWorkerStub) ShareDisk(path, name string, ro bool) error {
	s.shareCalls++
	if s.failShare != nil {
		return s.failShare
	}
	s.shared, s.ro, s.name = true, ro, name
	s.disks = append(s.disks, fmsworker.SharedDiskInfo{ContainerID: "id", ContainerPath: path, RootName: name, ReadOnly: ro, State: fmsworker.DiskStateClosed, Holder: "none", SharedAt: time.Now()})
	return nil
}
func (s *shareWorkerStub) UnshareDisk(path string) error {
	s.unshareCalls++
	if s.failUnshare != nil {
		return s.failUnshare
	}
	s.removed = true
	if i := s.find(path); i >= 0 {
		s.disks = append(s.disks[:i], s.disks[i+1:]...)
	}
	return nil
}
func (s *shareWorkerStub) SetAutostart(path string, enable bool, password []byte) error {
	s.autostartCalls++
	if s.failAutostart != nil {
		return s.failAutostart
	}
	i := s.find(path)
	if i < 0 {
		return fmsworker.ErrDiskNotShared
	}
	s.auto = enable
	s.password = append([]byte(nil), password...)
	if !s.ignoreAutostart {
		s.disks[i].Autostart = enable
	}
	if enable {
		s.disks[i].HasStoredKey = len(password) > 0
	} else if !s.keepKey {
		s.disks[i].HasStoredKey = false
	}
	return nil
}
func (s *shareWorkerStub) GetDiskStatus(path string) (*fmsworker.SharedDiskInfo, error) {
	if s.failStatus != nil {
		return nil, s.failStatus
	}
	i := s.find(path)
	if i < 0 {
		return nil, fmsworker.ErrDiskNotShared
	}
	v := s.disks[i]
	return &v, nil
}
func (s *shareWorkerStub) ListSharedDisks() ([]fmsworker.SharedDiskInfo, error) {
	if s.failList != nil {
		return nil, s.failList
	}
	return append([]fmsworker.SharedDiskInfo(nil), s.disks...), nil
}
func (s *shareWorkerStub) ListRoots() ([]string, error) {
	if s.failRoots != nil {
		return nil, s.failRoots
	}
	return s.roots, nil
}
func (s *shareWorkerStub) WorkerMode() fmsworker.WorkerMode { return fmsworker.WorkerModeService }

func shareFixture(t *testing.T) (string, *shareWorkerStub) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FILEDO_STATE_DIR", filepath.Join(dir, "state"))
	path := filepath.Join(dir, "media.fdd")
	if e := os.WriteFile(path, []byte("test fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	s := &shareWorkerStub{}
	oldFactory, oldInspect := vdWorkerFactory, vdShareInspect
	t.Cleanup(func() { vdWorkerFactory = oldFactory; vdShareInspect = oldInspect })
	vdWorkerFactory = func() (vdWorker, error) { return s, nil }
	vdShareInspect = func(string) (vdisk.Info, error) {
		return vdisk.Info{ContainerID: "id", FriendlyName: "Media/2026", Obfuscated: true, Profile: vdisk.ProfilePlain}, nil
	}
	return path, s
}

// sharedFixture is shareFixture with the container already shared on the worker.
func sharedFixture(t *testing.T) (string, *shareWorkerStub) {
	t.Helper()
	path, s := shareFixture(t)
	s.disks = []fmsworker.SharedDiskInfo{{ContainerID: "id", ContainerPath: path, RootName: "Media-2026", State: fmsworker.DiskStateClosed, Holder: "none", SharedAt: time.Now()}}
	return path, s
}

// captureBoth runs f and returns what it wrote to stdout and to stderr.
func captureBoth(t *testing.T, f func()) (string, string) {
	t.Helper()
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldErr := os.Stderr
	os.Stderr = ew
	done := make(chan string)
	go func() { b, _ := io.ReadAll(er); done <- string(b) }()
	out := captureStdout(t, f)
	ew.Close()
	os.Stderr = oldErr
	return out, <-done
}

func snapshotFile(t *testing.T) string {
	t.Helper()
	p, e := statedir.Path("vdisk-shared.json")
	if e != nil {
		t.Fatal(e)
	}
	return p
}

// snapshotState is the local cache's record for path, read through the real loader.
func snapshotState(t *testing.T, path string) (*vdisk.SharedDiskState, bool) {
	t.Helper()
	m, unlock, e := vdShareSnapshot()
	if e != nil {
		t.Fatalf("the snapshot is not loadable: %v", e)
	}
	defer unlock()
	return m.GetSharedDiskStateByPath(path)
}

// blockSnapshot makes every write of the snapshot fail: its path is a folder.
func blockSnapshot(t *testing.T) {
	t.Helper()
	if e := os.MkdirAll(snapshotFile(t), 0o700); e != nil {
		t.Fatal(e)
	}
}

func wantClass(t *testing.T, e error, class int) {
	t.Helper()
	if e == nil {
		t.Fatalf("no error; want class %d", class)
	}
	if got := vdExitClass(e); got != class {
		t.Fatalf("exit class %d, want %d (%v)", got, class, e)
	}
}

func TestShareRegistersWorkerAndSnapshot(t *testing.T) {
	path, s := shareFixture(t)
	var e error
	out := captureStdout(t, func() { e = vdShare([]string{path, "on", "ro"}, false) })
	if e != nil {
		t.Fatal(e)
	}
	if !s.shared || !s.ro || s.name != "Media-2026" || s.shareCalls != 1 {
		t.Fatalf("incorrect share: %+v", s)
	}
	for _, want := range []string{path, "'Media-2026'", "read-only: true", "QR code"} {
		if !strings.Contains(out, want) {
			t.Fatalf("confirmation lacks %q:\n%s", want, out)
		}
	}
	d, ok := snapshotState(t, path)
	if !ok || !d.Shared || !d.ReadOnly || d.RootName != "Media-2026" {
		t.Fatal("successful share did not persist")
	}
}

func TestSharePathValidation(t *testing.T) {
	path, _ := shareFixture(t)
	dir := filepath.Dir(path)
	t.Run("non-fdd file", func(t *testing.T) {
		txt := filepath.Join(dir, "notes.txt")
		if e := os.WriteFile(txt, []byte("x"), 0600); e != nil {
			t.Fatal(e)
		}
		_, e := vdSharePath(txt, true)
		wantClass(t, e, vdisk.ExitUsage)
	})
	t.Run("directory", func(t *testing.T) {
		_, e := vdSharePath(dir, true)
		wantClass(t, e, vdisk.ExitUsage)
	})
	t.Run("missing", func(t *testing.T) {
		_, e := vdSharePath(path+"x.fdd", true)
		wantClass(t, e, vdisk.ExitIO)
	})
	t.Run("upper-case extension", func(t *testing.T) {
		up := filepath.Join(dir, "UP.FDD")
		if e := os.WriteFile(up, []byte("x"), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := vdSharePath(up, true); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("absolute", func(t *testing.T) {
		got, e := vdSharePath(path, true)
		if e != nil || !filepath.IsAbs(got) || !strings.EqualFold(got, path) {
			t.Fatalf("absolute path changed: %s %v", got, e)
		}
	})
	t.Run("relative", func(t *testing.T) {
		t.Chdir(dir)
		got, e := vdSharePath("media.fdd", true)
		want, _ := fsx.Resolve(path)
		if e != nil || !filepath.IsAbs(got) || !strings.EqualFold(got, want) {
			t.Fatalf("relative path became %s (%v), want %s", got, e, want)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		link := filepath.Join(dir, "link.fdd")
		if e := os.Symlink(path, link); e != nil {
			t.Skipf("symbolic links are not available here: %v", e)
		}
		got, e := vdSharePath(link, true)
		want, _ := fsx.Resolve(path)
		if e != nil || !strings.EqualFold(got, want) {
			t.Fatalf("symlink resolved to %s (%v), want its target %s", got, e, want)
		}
	})
}

func TestShareInvalidArgumentsAreUsageClass(t *testing.T) {
	path, s := shareFixture(t)
	for _, args := range [][]string{{}, {path}, {path, "bad"}, {path, "on", "as"}, {path, "on", "as", "Media", "extra"}, {path, "off", "ro"}, {path, "on", "as", "a/b"}, {path, "on", "ro", "ro"}, {path, "on", "unknown"}, {path, "on", "as", ".."}} {
		wantClass(t, vdShare(args, false), vdisk.ExitUsage)
	}
	if s.shareCalls != 0 || s.unshareCalls != 0 {
		t.Fatal("a rejected command reached the worker")
	}
	wantClass(t, vdShare([]string{path + "missing.fdd", "on"}, false), vdisk.ExitIO)
}

func TestShareWorkerErrorWritesNoSnapshot(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		class int
	}{
		{"plain", errors.New("worker refused"), vdisk.ExitIO},
		{"worker class busy", &fmsworker.WorkerError{Message: "disk is held", OutcomeClass: vdisk.ExitBusy}, vdisk.ExitBusy},
		{"worker class credential", &fmsworker.WorkerError{Message: "wrong key", OutcomeClass: vdisk.ExitCredential}, vdisk.ExitCredential},
		{"worker class invalid", &fmsworker.WorkerError{Message: "odd", OutcomeClass: 99}, vdisk.ExitIO},
		{"worker class none", &fmsworker.WorkerError{Message: "odd"}, vdisk.ExitIO},
		{"worker unavailable", fmsworker.ErrWorkerUnavailable, vdisk.ExitUnsupported},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path, s := shareFixture(t)
			s.failShare = c.err
			var e error
			out := captureStdout(t, func() { e = vdShare([]string{path, "on"}, false) })
			wantClass(t, e, c.class)
			if !errors.Is(e, c.err) || !strings.Contains(e.Error(), c.err.Error()) {
				t.Fatalf("the worker's own failure is not reachable: %v", e)
			}
			if s.shareCalls != 1 || s.shared {
				t.Fatalf("the worker call did not fail as intended: %+v", s)
			}
			if _, se := os.Stat(snapshotFile(t)); !errors.Is(se, os.ErrNotExist) {
				t.Fatalf("a failed share wrote a snapshot (%v)", se)
			}
			if strings.Contains(out, "Shared ") {
				t.Fatalf("failure reported as success:\n%s", out)
			}
		})
	}
}

func TestShareRootNameClash(t *testing.T) {
	path, s := shareFixture(t)
	s.roots = []string{"MEDIA-2026"}
	e := vdShare([]string{path, "on"}, false)
	if !errors.Is(e, fmsworker.ErrRootNameClash) {
		t.Fatalf("clash lost its type: %v", e)
	}
	wantClass(t, e, vdisk.ExitUsage)
	if s.shareCalls != 0 {
		t.Fatal("clash reached the worker")
	}
	s.roots = []string{"Other"}
	if e = vdShare([]string{path, "on", "as", "media-2026"}, false); e != nil {
		t.Fatal(e)
	}
}

func TestShareAlreadyShared(t *testing.T) {
	t.Run("same container id", func(t *testing.T) {
		path, s := shareFixture(t)
		s.disks = []fmsworker.SharedDiskInfo{{ContainerID: "id", ContainerPath: filepath.Join(filepath.Dir(path), "elsewhere.fdd"), RootName: "Other"}}
		e := vdShare([]string{path, "on"}, false)
		if !errors.Is(e, fmsworker.ErrAlreadyShared) {
			t.Fatalf("got %v", e)
		}
		wantClass(t, e, vdisk.ExitBusy)
		if s.shareCalls != 0 {
			t.Fatal("duplicate reached the worker")
		}
	})
	t.Run("same file other id", func(t *testing.T) {
		path, s := shareFixture(t)
		s.disks = []fmsworker.SharedDiskInfo{{ContainerID: "another", ContainerPath: strings.ToUpper(path), RootName: "Other"}}
		e := vdShare([]string{path, "on"}, false)
		if !errors.Is(e, fmsworker.ErrAlreadyShared) {
			t.Fatalf("got %v", e)
		}
		wantClass(t, e, vdisk.ExitBusy)
		if s.shareCalls != 0 {
			t.Fatal("duplicate reached the worker")
		}
	})
}

func TestShareHoldsBackWhenMountStateIsUnknown(t *testing.T) {
	path, s := shareFixture(t)
	sp, e := vdStatePath()
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Dir(sp), 0o700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(sp, []byte("{not json"), 0o600); e != nil {
		t.Fatal(e)
	}
	e = vdShare([]string{path, "on"}, false)
	if e == nil || !strings.Contains(e.Error(), "mount state") {
		t.Fatalf("an unreadable mount state let the share through: %v", e)
	}
	wantClass(t, e, vdisk.ExitIO)
	if s.shareCalls != 0 {
		t.Fatal("share reached the worker with an unknown holder")
	}
	if e = os.WriteFile(sp, []byte(`{"version":1,"mounts":[{"container_id":"ID","path":"x","letter":"X:"}]}`), 0o600); e != nil {
		t.Fatal(e)
	}
	e = vdShare([]string{path, "on"}, false)
	if !errors.Is(e, vdisk.ErrBusy) || !strings.Contains(e.Error(), "X:") {
		t.Fatalf("a mounted disk was shared: %v", e)
	}
	wantClass(t, e, vdisk.ExitBusy)
	if s.shareCalls != 0 {
		t.Fatal("share reached the worker for a mounted disk")
	}
}

// A snapshot that cannot be written after the worker did its work never turns
// the command into a failure that invites a repeat.
func TestShareSnapshotFailureIsOnlyAWarning(t *testing.T) {
	path, s := shareFixture(t)
	blockSnapshot(t)
	var e error
	out, errOut := captureBoth(t, func() { e = vdShare([]string{path, "on"}, false) })
	if e != nil {
		t.Fatalf("a snapshot failure failed the share: %v", e)
	}
	if !s.shared || !strings.Contains(out, "Shared ") {
		t.Fatalf("share not reported:\n%s", out)
	}
	if !strings.Contains(errOut, "warning:") || !strings.Contains(errOut, "snapshot") {
		t.Fatalf("no warning on stderr: %q", errOut)
	}
	// A repeat would now be refused as a duplicate: success was the right answer.
	wantClass(t, vdShare([]string{path, "on"}, false), vdisk.ExitBusy)
}

func TestShareRepairsUnreadableOrDuplicateSnapshot(t *testing.T) {
	for _, content := range []string{
		"{broken",
		`{"version":1,"disks":{"a":{"container_id":"a","container_path":"C:\\x\\one.fdd","shared":true,"root_name":"Dup"},"b":{"container_id":"b","container_path":"C:\\x\\ONE.fdd","shared":true,"root_name":"Dup2"}}}`,
	} {
		path, s := shareFixture(t)
		sp := snapshotFile(t)
		if e := os.MkdirAll(filepath.Dir(sp), 0o700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(sp, []byte(content), 0o600); e != nil {
			t.Fatal(e)
		}
		var e error
		_, errOut := captureBoth(t, func() { e = vdShare([]string{path, "on"}, false) })
		if e != nil || !s.shared {
			t.Fatalf("a bad cache stopped the share: %v", e)
		}
		if strings.Contains(errOut, "warning:") {
			t.Fatalf("the cache was not repaired: %s", errOut)
		}
		if d, ok := snapshotState(t, path); !ok || !d.Shared {
			t.Fatal("the repaired cache lacks the disk")
		}
		// And the next command is not poisoned.
		if e = vdShare([]string{path, "off"}, false); e != nil {
			t.Fatal(e)
		}
	}
}

func TestUnsharePreservesWorkerFailure(t *testing.T) {
	path, s := sharedFixture(t)
	if e := vdRefreshShareSnapshot(s); e != nil {
		t.Fatal(e)
	}
	s.failUnshare = errors.New("disk still busy")
	e := vdShare([]string{path, "off"}, false)
	if e == nil || !strings.Contains(e.Error(), "disk still busy") {
		t.Fatalf("failed close/unshare reported success: %v", e)
	}
	wantClass(t, e, vdisk.ExitIO)
	if _, ok := snapshotState(t, path); !ok {
		t.Fatal("a failed unshare dropped the snapshot")
	}
	s.failUnshare = &fmsworker.WorkerError{Message: "open handles: 2", OutcomeClass: vdisk.ExitBusy}
	wantClass(t, vdShare([]string{path, "off"}, false), vdisk.ExitBusy)
	s.failUnshare = nil
	if e = vdShare([]string{path, "off"}, false); e != nil {
		t.Fatal(e)
	}
	if !s.removed || len(s.disks) != 0 {
		t.Fatal("no unshare request")
	}
}

func TestUnshareRemovesSnapshot(t *testing.T) {
	path, s := shareFixture(t)
	if e := vdShare([]string{path, "on"}, false); e != nil {
		t.Fatal(e)
	}
	if _, ok := snapshotState(t, path); !ok {
		t.Fatal("no snapshot to remove")
	}
	var e error
	out := captureStdout(t, func() { e = vdShare([]string{path, "off"}, false) })
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out, "Unshared") || s.unshareCalls != 1 {
		t.Fatalf("unshare not reported:\n%s", out)
	}
	if d, ok := snapshotState(t, path); ok {
		t.Fatalf("unshare left %+v in the snapshot", d)
	}
}

// off is idempotent: a disk the worker does not know is unshared already, and
// its local record must not outlive that.
func TestUnshareOfUnknownDiskForgetsSnapshot(t *testing.T) {
	for _, notShared := range []error{
		fmsworker.ErrDiskNotShared,
		&fmsworker.WorkerError{Message: "Disk is not shared"},
	} {
		path, s := sharedFixture(t)
		if e := vdRefreshShareSnapshot(s); e != nil {
			t.Fatal(e)
		}
		s.disks = nil
		s.failUnshare = notShared
		var e error
		out := captureStdout(t, func() { e = vdShare([]string{path, "off"}, false) })
		if e != nil {
			t.Fatalf("off of an unshared disk failed: %v", e)
		}
		if !strings.Contains(out, "not shared") {
			t.Fatalf("the user is not told it was not shared:\n%s", out)
		}
		if _, ok := snapshotState(t, path); ok {
			t.Fatal("the stale record survived")
		}
	}
}

func TestUnshareShowsOpenHandles(t *testing.T) {
	path, s := sharedFixture(t)
	s.disks[0].OpenHandles = 3
	out := captureStdout(t, func() { _ = vdShare([]string{path, "off"}, false) })
	if !strings.Contains(out, "3 open handle(s)") {
		t.Fatalf("handle count not shown:\n%s", out)
	}
}

func TestUnshareSnapshotFailureIsOnlyAWarning(t *testing.T) {
	path, s := sharedFixture(t)
	blockSnapshot(t)
	var e error
	_, errOut := captureBoth(t, func() { e = vdShare([]string{path, "off"}, false) })
	if e != nil || !s.removed {
		t.Fatalf("unshare failed over the cache: %v", e)
	}
	if !strings.Contains(errOut, "warning:") {
		t.Fatalf("no warning: %q", errOut)
	}
}

func TestWorkerErrorClasses(t *testing.T) {
	for _, c := range []struct {
		name  string
		err   error
		class int
	}{
		{"already shared", fmsworker.ErrAlreadyShared, vdisk.ExitBusy},
		{"already shared (vdisk)", vdisk.ErrAlreadyShared, vdisk.ExitBusy},
		{"root name clash", fmsworker.ErrRootNameClash, vdisk.ExitUsage},
		{"not shared", fmsworker.ErrDiskNotShared, vdisk.ExitUsage},
		{"disk not found", fmsworker.ErrDiskNotFound, vdisk.ExitUsage},
		{"worker unavailable", fmsworker.ErrWorkerUnavailable, vdisk.ExitUnsupported},
		{"not capable", fmsworker.ErrNotCapable, vdisk.ExitUnsupported},
		{"communication", fmsworker.ErrCommunication, vdisk.ExitIO},
		{"worker class 2", &fmsworker.WorkerError{Message: "m", OutcomeClass: 2}, vdisk.ExitUsage},
		{"worker class 4", &fmsworker.WorkerError{Message: "m", OutcomeClass: 4}, vdisk.ExitDamaged},
		{"worker class 6", &fmsworker.WorkerError{Message: "m", OutcomeClass: 6}, vdisk.ExitUnsupported},
		{"worker class 7 is no container class", &fmsworker.WorkerError{Message: "m", OutcomeClass: 7}, vdisk.ExitIO},
		{"worker class beats text", &fmsworker.WorkerError{Message: "disk is not shared", OutcomeClass: 3}, vdisk.ExitCredential},
		{"worker text only", &fmsworker.WorkerError{Message: "disk is not shared"}, vdisk.ExitUsage},
		{"wrapped", errors.Join(errors.New("ctx"), fmsworker.ErrRootNameClash), vdisk.ExitUsage},
		{"already classified", vdUsagef("x"), vdisk.ExitUsage},
		{"unknown", errors.New("boom"), vdisk.ExitIO},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := vdWorkerError(c.err)
			wantClass(t, e, c.class)
			if !errors.Is(e, c.err) {
				t.Fatal("classification hid the worker's error")
			}
			if e.Error() != c.err.Error() {
				t.Fatalf("message changed: %q", e.Error())
			}
		})
	}
	if vdWorkerError(nil) != nil {
		t.Fatal("nil became an error")
	}
}

func TestRootNamesFollowFMSRules(t *testing.T) {
	for _, name := range []string{"Media: 2026", "Снимки", "Video (2)", strings.Repeat("x", 80)} {
		if e := validateRootName(name); e != nil {
			t.Fatalf("FMS-compatible name refused: %s", name)
		}
	}
	if cleanRootName(" / ") != "" || cleanRootName(" a/b\\c ") != "a-b-c" {
		t.Fatal("sanitization differs from ShareRootNames")
	}
	if generateRootNameFromContainer(vdisk.Info{}, "MEDIA.FDD") != "MEDIA" {
		t.Fatal("uppercase extension was kept")
	}
	if generateRootNameFromContainer(vdisk.Info{FriendlyName: "Media/2026"}, "x.fdd") != "Media-2026" {
		t.Fatal("friendly name not sanitized")
	}
}
