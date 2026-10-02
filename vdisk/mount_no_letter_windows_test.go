//go:build windows

package vdisk

import (
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
	"unsafe"
)

const testVolume = `\\?\Volume{12345678-1234-1234-1234-123456789abc}\`

// opRecorder is a fake mountOperations that records every call in order.
type opRecorder struct {
	calls     []string
	vols      []string
	dirs      []string
	secureErr func(string) error
	setErr    error
	removeErr error
	deleteErr error
	verifyErr error
	entries   []sweepEntry
	target    string
	targetErr error
}

func (r *opRecorder) ops() mountOperations {
	return mountOperations{
		secureDir: func(p string) error {
			r.calls = append(r.calls, "secure:"+p)
			if r.secureErr != nil {
				return r.secureErr(p)
			}
			return nil
		},
		removeDir: func(p string) error { r.calls = append(r.calls, "remove:"+p); return r.removeErr },
		setMount: func(vol, dir string) error {
			r.calls = append(r.calls, "set:"+vol+"|"+dir)
			r.vols = append(r.vols, vol)
			r.dirs = append(r.dirs, dir)
			return r.setErr
		},
		deleteMount:  func(p string) error { r.calls = append(r.calls, "delete:"+p); return r.deleteErr },
		verifyVolume: func(v string) error { r.calls = append(r.calls, "verify:"+v); return r.verifyErr },
		readDir:      func(string) ([]sweepEntry, error) { return r.entries, nil },
		mountTarget: func(p string) (string, error) {
			r.calls = append(r.calls, "target:"+p)
			return r.target, r.targetErr
		},
	}
}
func newRecordingManager(t *testing.T) (*NoLetterMountManager, *opRecorder) {
	t.Helper()
	m := NewNoLetterMountManager(t.TempDir())
	r := &opRecorder{}
	m.ops = r.ops()
	return m, r
}
func fakeMountManager(t *testing.T) *NoLetterMountManager {
	t.Helper()
	m, _ := newRecordingManager(t)
	return m
}
func TestNoLetterV3AndStableGUID(t *testing.T) {
	m := fakeMountManager(t)
	d, e := m.CreateNoLetterMount("id", testVolume)
	if e != nil {
		t.Fatal(e)
	}
	if !d.FolderMount || d.MountPath == testVolume || filepath.Dir(d.MountPath) != m.basePath || d.FallbackReason != "" {
		t.Fatalf("wrong folder mount: %+v", d)
	}
	if GetStableVolumePath("id", testVolume) != testVolume {
		t.Fatal("invalid volume fallback")
	}
	if _, e = m.CreateNoLetterMount("id", testVolume); e == nil {
		t.Fatal("duplicate container accepted")
	}
	if _, e = m.CreateNoLetterMount("other", testVolume); e == nil {
		t.Fatal("same volume mounted twice")
	}
	d.MountPath = "tampered"
	again, _ := m.GetNoLetterMount("id")
	if again.MountPath == "tampered" {
		t.Fatal("snapshot aliases manager")
	}
	if e = m.DestroyAll(); e != nil {
		t.Fatal(e)
	}
	if _, ok := m.GetNoLetterMount("id"); ok {
		t.Fatal("destroy left mount recorded")
	}
}

// The folder mount gets the normalised volume GUID (trailing backslash) and
// the directory that was just secured, in that argument order.
func TestNoLetterSetMountArguments(t *testing.T) {
	const guid = "12345678-1234-1234-1234-123456789abc"
	want := `\\?\Volume{` + guid + `}\`
	for _, in := range []string{want, "{" + guid + "}", guid, `\\?\Volume{` + strings.ToUpper(guid) + `}\`} {
		m, r := newRecordingManager(t)
		d, e := m.CreateNoLetterMount("id", in)
		if e != nil {
			t.Fatalf("%q: %v", in, e)
		}
		if len(r.vols) != 1 || !strings.EqualFold(r.vols[0], want) || !strings.HasSuffix(r.vols[0], `}\`) || !strings.HasPrefix(r.vols[0], `\\?\Volume{`) {
			t.Fatalf("%q: setMount volume %q", in, r.vols)
		}
		if len(r.dirs) != 1 || r.dirs[0] != d.MountPath || filepath.Dir(r.dirs[0]) != m.basePath {
			t.Fatalf("%q: setMount dir %q, mount %+v", in, r.dirs, d)
		}
		// t.TempDir exists, so the parent directory is not re-created.
		wantCalls := []string{
			"secure:" + m.basePath,
			"secure:" + d.MountPath,
			"set:" + r.vols[0] + "|" + d.MountPath,
		}
		var got []string
		for _, c := range r.calls {
			if !strings.HasPrefix(c, "verify:") {
				got = append(got, c)
			}
		}
		if !reflect.DeepEqual(got, wantCalls) {
			t.Fatalf("%q: call order %q, want %q", in, got, wantCalls)
		}
		if d.VolumeGUID != r.vols[0] {
			t.Fatalf("%q: stored GUID %q", in, d.VolumeGUID)
		}
	}
}
func TestNoLetterNonceFormatAndUniqueness(t *testing.T) {
	m, r := newRecordingManager(t)
	nonce := regexp.MustCompile(`^[0-9a-f]{32}$`)
	seen := map[string]bool{}
	for i := 0; i < 300; i++ {
		guid := fmt.Sprintf(`\\?\Volume{00000000-0000-0000-0000-%012x}\`, i)
		d, e := m.CreateNoLetterMount(fmt.Sprintf("id%d", i), guid)
		if e != nil {
			t.Fatal(e)
		}
		name := filepath.Base(d.MountPath)
		if !nonce.MatchString(name) {
			t.Fatalf("nonce %q is not 32 lowercase hex chars", name)
		}
		if seen[name] {
			t.Fatalf("nonce %q repeated", name)
		}
		seen[name] = true
	}
	if len(r.dirs) != 300 {
		t.Fatalf("setMount calls: %d", len(r.dirs))
	}
}
func TestNoLetterFallbackAndCleanup(t *testing.T) {
	for _, failure := range []string{"base", "dir", "mount"} {
		t.Run(failure, func(t *testing.T) {
			m, r := newRecordingManager(t)
			switch failure {
			case "base":
				r.secureErr = func(string) error { return os.ErrPermission }
			case "dir":
				r.secureErr = func(p string) error {
					if p != m.basePath {
						return ErrMountPointACL
					}
					return nil
				}
			case "mount":
				r.setErr = errors.New("mount failed")
			}
			d, e := m.CreateNoLetterMount("id", testVolume)
			if e != nil {
				t.Fatal(e)
			}
			if d.MountPath != testVolume || d.FolderMount {
				t.Fatalf("bad fallback: %+v", d)
			}
			if d.FallbackReason == "" {
				t.Fatal("fallback reason swallowed")
			}
			switch failure {
			case "base":
				if !strings.Contains(d.FallbackReason, os.ErrPermission.Error()) {
					t.Fatalf("reason %q", d.FallbackReason)
				}
			case "dir":
				if !strings.Contains(d.FallbackReason, ErrMountPointACL.Error()) {
					t.Fatalf("reason %q", d.FallbackReason)
				}
			case "mount":
				if !strings.Contains(d.FallbackReason, "mount failed") {
					t.Fatalf("reason %q", d.FallbackReason)
				}
				// The directory that setMount was given is the one removed.
				last := r.calls[len(r.calls)-1]
				if len(r.dirs) != 1 || last != "remove:"+r.dirs[0] {
					t.Fatalf("failed mount directory was retained: %q", r.calls)
				}
			}
			snap, _ := m.GetNoLetterMount("id")
			if snap.FallbackReason != d.FallbackReason {
				t.Fatal("reason not stored with the mount")
			}
			if e = m.DestroyNoLetterMount("id"); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestNoLetterCleanupFailureAfterMountFailureIsAnError(t *testing.T) {
	m, r := newRecordingManager(t)
	r.setErr = errors.New("mount failed")
	r.removeErr = os.ErrPermission
	if _, e := m.CreateNoLetterMount("id", testVolume); !errors.Is(e, os.ErrPermission) {
		t.Fatalf("cleanup failure hidden: %v", e)
	}
	if _, ok := m.GetNoLetterMount("id"); ok {
		t.Fatal("failed create recorded a mount")
	}
}
func TestNoLetterInvalidVolumeAndCleanupFailures(t *testing.T) {
	m := fakeMountManager(t)
	if _, e := m.CreateNoLetterMount("id", "../../bad"); e == nil {
		t.Fatal("invalid GUID accepted")
	}
	m.ops.verifyVolume = func(string) error { return os.ErrNotExist }
	if _, e := m.CreateNoLetterMount("id", testVolume); e == nil {
		t.Fatal("missing volume accepted")
	}
	m = fakeMountManager(t)
	m.CreateNoLetterMount("id", testVolume)
	m.ops.deleteMount = func(string) error { return os.ErrPermission }
	if e := m.DestroyAll(); e == nil {
		t.Fatal("detach failure hidden")
	}
	if _, ok := m.GetNoLetterMount("id"); !ok {
		t.Fatal("failed detach forgot mount")
	}
}

// Destroy detaches first and removes the directory second, with the mount's own
// path for both, and a half-finished destroy can be retried to completion.
func TestNoLetterDestroyOrderAndRetry(t *testing.T) {
	m, r := newRecordingManager(t)
	d, e := m.CreateNoLetterMount("id", testVolume)
	if e != nil {
		t.Fatal(e)
	}
	r.calls = nil

	r.removeErr = os.ErrPermission
	if e = m.DestroyNoLetterMount("id"); !errors.Is(e, os.ErrPermission) {
		t.Fatalf("directory removal failure hidden: %v", e)
	}
	if _, ok := m.GetNoLetterMount("id"); !ok {
		t.Fatal("mount forgotten while its directory is still there")
	}
	if want := []string{"delete:" + d.MountPath, "remove:" + d.MountPath}; !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("first attempt %q, want %q", r.calls, want)
	}

	// The retry sees an already-detached directory.
	r.calls = nil
	r.removeErr = nil
	r.deleteErr = windows.ERROR_NOT_A_REPARSE_POINT
	if e = m.DestroyNoLetterMount("id"); e != nil {
		t.Fatalf("retry after half-finished destroy failed: %v", e)
	}
	if want := []string{"delete:" + d.MountPath, "remove:" + d.MountPath}; !reflect.DeepEqual(r.calls, want) {
		t.Fatalf("retry %q, want %q", r.calls, want)
	}
	if _, ok := m.GetNoLetterMount("id"); ok {
		t.Fatal("completed destroy left the mount recorded")
	}
	if _, ok := m.GetNoLetterMountByPath(d.MountPath); ok {
		t.Fatal("path index kept")
	}
	if _, ok := m.GetNoLetterMountByGUID(d.VolumeGUID); ok {
		t.Fatal("GUID index kept")
	}
	if e = m.DestroyNoLetterMount("id"); !errors.Is(e, ErrVolumeNotMounted) {
		t.Fatalf("second destroy: %v", e)
	}
}
func TestNoLetterDestroyToleratesGoneAndRejectsOthers(t *testing.T) {
	for _, gone := range []error{windows.ERROR_NOT_A_REPARSE_POINT, windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND} {
		m, r := newRecordingManager(t)
		m.CreateNoLetterMount("id", testVolume)
		r.deleteErr = gone
		r.removeErr = &os.PathError{Op: "remove", Path: "x", Err: windows.ERROR_FILE_NOT_FOUND}
		if e := m.DestroyNoLetterMount("id"); e != nil {
			t.Fatalf("%v: %v", gone, e)
		}
	}
	m, r := newRecordingManager(t)
	m.CreateNoLetterMount("id", testVolume)
	r.deleteErr = windows.ERROR_ACCESS_DENIED
	r.calls = nil
	if e := m.DestroyNoLetterMount("id"); !errors.Is(e, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("real detach failure hidden: %v", e)
	}
	if len(r.calls) != 1 || !strings.HasPrefix(r.calls[0], "delete:") {
		t.Fatalf("directory removed after a failed detach: %q", r.calls)
	}
	// A V2 mount owns no directory: nothing to detach or remove.
	m, r = newRecordingManager(t)
	r.setErr = errors.New("x")
	m.CreateNoLetterMount("v2", testVolume)
	r.calls = nil
	if e := m.DestroyNoLetterMount("v2"); e != nil || len(r.calls) != 0 {
		t.Fatalf("V2 destroy: %v %q", e, r.calls)
	}
}

var (
	sweepNow   = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	nonceOld   = strings.Repeat("a", 32)
	nonceOld2  = strings.Repeat("b", 32)
	nonceFresh = strings.Repeat("c", 32)
)

func sweepManager(t *testing.T) (*NoLetterMountManager, *opRecorder) {
	m, r := newRecordingManager(t)
	m.now = func() time.Time { return sweepNow }
	return m, r
}
func TestNoLetterSweepDanglingLiveAndUnknown(t *testing.T) {
	m, r := sweepManager(t)
	old := sweepNow.Add(-time.Hour)
	r.entries = []sweepEntry{
		{Name: nonceOld, Dir: true, Reparse: true, ModTime: old},
		{Name: nonceOld2, Dir: true, Reparse: true, ModTime: old},
		{Name: nonceFresh, Dir: true, Reparse: true, ModTime: sweepNow.Add(-time.Second)},
		{Name: strings.Repeat("d", 32), Dir: true, Reparse: true, ModTime: old},
		{Name: strings.Repeat("e", 32), Dir: true, Reparse: true, ModTime: old},
	}
	// aaaa: target gone -> detach then remove. bbbb: target answers -> left.
	// cccc: too fresh. dddd: target unreadable -> left. eeee: odd error -> left.
	verify := map[string]error{}
	m.ops.mountTarget = func(p string) (string, error) {
		r.calls = append(r.calls, "target:"+filepath.Base(p))
		switch filepath.Base(p)[0] {
		case 'd':
			return "", windows.ERROR_ACCESS_DENIED
		case 'a':
			return `\\?\Volume{aaaaaaaa-0000-0000-0000-000000000000}\`, nil
		case 'b':
			return `\\?\Volume{bbbbbbbb-0000-0000-0000-000000000000}\`, nil
		}
		return `\\?\Volume{eeeeeeee-0000-0000-0000-000000000000}\`, nil
	}
	verify[`\\?\Volume{aaaaaaaa-0000-0000-0000-000000000000}\`] = windows.ERROR_FILE_NOT_FOUND
	verify[`\\?\Volume{bbbbbbbb-0000-0000-0000-000000000000}\`] = nil
	verify[`\\?\Volume{eeeeeeee-0000-0000-0000-000000000000}\`] = windows.ERROR_ACCESS_DENIED
	m.ops.verifyVolume = func(v string) error { r.calls = append(r.calls, "verify:"+v); return verify[v] }
	if e := m.Initialize(); e != nil {
		t.Fatal(e)
	}
	pa := filepath.Join(m.basePath, nonceOld)
	wantTail := []string{
		"target:" + nonceOld, "verify:" + `\\?\Volume{aaaaaaaa-0000-0000-0000-000000000000}\`, "delete:" + pa, "remove:" + pa,
	}
	var got []string
	for _, c := range r.calls {
		if !strings.HasPrefix(c, "secure:") {
			got = append(got, c)
		}
	}
	if len(got) < len(wantTail) || !reflect.DeepEqual(got[:len(wantTail)], wantTail) {
		t.Fatalf("sweep order %q, want prefix %q", got, wantTail)
	}
	for _, c := range got {
		if strings.HasPrefix(c, "delete:") || strings.HasPrefix(c, "remove:") {
			if !strings.HasSuffix(c, nonceOld) {
				t.Fatalf("sweep touched a live, fresh or unknown mount: %q", got)
			}
		}
	}
	rep := m.LastSweep()
	if len(rep.Removed) != 1 || rep.Removed[0] != pa || len(rep.Skipped) != 3 || len(rep.Errors) != 0 {
		t.Fatalf("report %+v", rep)
	}
}
func TestNoLetterSweepDetachFailureKeepsDirectory(t *testing.T) {
	m, r := sweepManager(t)
	r.entries = []sweepEntry{{Name: nonceOld, Dir: true, Reparse: true, ModTime: sweepNow.Add(-time.Hour)}}
	r.target = testVolume
	r.verifyErr = windows.ERROR_PATH_NOT_FOUND
	r.deleteErr = windows.ERROR_ACCESS_DENIED
	if e := m.Initialize(); e != nil {
		t.Fatal(e)
	}
	for _, c := range r.calls {
		if strings.HasPrefix(c, "remove:") {
			t.Fatalf("directory removed after a failed detach: %q", r.calls)
		}
	}
	if rep := m.LastSweep(); len(rep.Errors) != 1 || len(rep.Removed) != 0 {
		t.Fatalf("report %+v", rep)
	}
}
func TestNoLetterSweepOnlyOnceAndSkipsTracked(t *testing.T) {
	m, r := sweepManager(t)
	d, e := m.CreateNoLetterMount("id", testVolume)
	if e != nil {
		t.Fatal(e)
	}
	// The directory this manager owns is listed as plain and old: it must stay.
	r.entries = []sweepEntry{
		{Name: filepath.Base(d.MountPath), Dir: true, ModTime: sweepNow.Add(-time.Hour)},
		{Name: nonceOld, Dir: true, ModTime: sweepNow.Add(-time.Hour)},
	}
	r.calls = nil
	if e = m.Initialize(); e != nil {
		t.Fatal(e)
	}
	var removed []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, "remove:") {
			removed = append(removed, c)
		}
	}
	if want := []string{"remove:" + filepath.Join(m.basePath, nonceOld)}; !reflect.DeepEqual(removed, want) {
		t.Fatalf("removed %q, want %q", removed, want)
	}
	// Creating another mount must not sweep again.
	r.calls = nil
	r.entries = []sweepEntry{{Name: nonceOld2, Dir: true, ModTime: sweepNow.Add(-time.Hour)}}
	if _, e = m.CreateNoLetterMount("id2", `\\?\Volume{00000000-0000-0000-0000-000000000002}\`); e != nil {
		t.Fatal(e)
	}
	for _, c := range r.calls {
		if strings.HasPrefix(c, "remove:") {
			t.Fatalf("create swept again: %q", r.calls)
		}
	}
}

// Real directories: only an old, empty, 32-hex directory goes; content never.
func TestNoLetterSweepRealDirectories(t *testing.T) {
	base := t.TempDir()
	m := NewNoLetterMountManager(base)
	m.ops.secureDir = func(string) error { return nil }
	old := time.Now().Add(-time.Hour)
	mk := func(name string, files ...string) string {
		p := filepath.Join(base, name)
		if e := os.Mkdir(p, 0o700); e != nil {
			t.Fatal(e)
		}
		for _, f := range files {
			if e := os.WriteFile(filepath.Join(p, f), []byte("keep"), 0o600); e != nil {
				t.Fatal(e)
			}
		}
		os.Chtimes(p, old, old)
		return p
	}
	empty := mk(nonceOld)
	full := mk(nonceOld2, "data.txt")
	other := mk("not-a-nonce")
	fresh := mk(nonceFresh)
	now := time.Now()
	os.Chtimes(fresh, now, now)
	file := filepath.Join(base, strings.Repeat("f", 32))
	if e := os.WriteFile(file, []byte("x"), 0o600); e != nil {
		t.Fatal(e)
	}
	os.Chtimes(file, old, old)
	if e := m.Initialize(); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(empty); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("stale empty directory kept")
	}
	for _, p := range []string{full, filepath.Join(full, "data.txt"), other, fresh, file} {
		if _, e := os.Stat(p); e != nil {
			t.Fatalf("sweep removed %s: %v", p, e)
		}
	}
	rep := m.LastSweep()
	if len(rep.Removed) != 1 || rep.Removed[0] != empty || len(rep.Skipped) != 1 || rep.Skipped[0] != full {
		t.Fatalf("report %+v", rep)
	}
}
func TestNoLetterSweepMissingBaseIsNotAnError(t *testing.T) {
	m, _ := sweepManager(t)
	m.ops.readDir = func(string) ([]sweepEntry, error) { return nil, os.ErrNotExist }
	if e := m.Initialize(); e != nil {
		t.Fatal(e)
	}
	if rep := m.LastSweep(); len(rep.Errors) != 0 {
		t.Fatalf("report %+v", rep)
	}
	m.ops.readDir = func(string) ([]sweepEntry, error) { return nil, os.ErrPermission }
	if e := m.Initialize(); e != nil {
		t.Fatalf("sweep failure must not fail Initialize: %v", e)
	}
	if rep := m.LastSweep(); len(rep.Errors) != 1 {
		t.Fatalf("report %+v", rep)
	}
}

func TestMountSDDLShape(t *testing.T) {
	const user = "S-1-5-21-1111-2222-3333-1001"
	if got := mountSDDLForSID(sidSystem); got != "D:P(A;OICI;FA;;;SY)" {
		t.Fatalf("SYSTEM SDDL %q", got)
	}
	if got, want := mountSDDLForSID(user), "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;"+user+")"; got != want {
		t.Fatalf("user SDDL %q, want %q", got, want)
	}
	if got := mountSDDLForSID(sidAdministrators); !strings.HasSuffix(got, "("+"A;OICI;FA;;;"+sidAdministrators+")") {
		t.Fatalf("admin-token SDDL %q", got)
	}
	cur, e := currentUserSID()
	if e != nil {
		t.Fatal(e)
	}
	real, e := mountSDDL()
	if e != nil || real != mountSDDLForSID(cur) {
		t.Fatalf("mountSDDL %q %v", real, e)
	}
	if isRunningAsLocalSystem() != (cur == sidSystem) {
		t.Fatal("isRunningAsLocalSystem disagrees with the token")
	}
}
func TestMountOwnerTrusted(t *testing.T) {
	const user = "S-1-5-21-1111-2222-3333-1001"
	for owner, want := range map[string]bool{
		user: true, sidSystem: true, sidAdministrators: true,
		"S-1-5-21-1111-2222-3333-1002": false, "S-1-1-0": false, "S-1-5-32-545": false, "": false,
	} {
		if mountOwnerTrusted(owner, user) != want {
			t.Fatalf("owner %q: want %t", owner, want)
		}
	}
	// A SYSTEM process still rejects a foreign owner.
	if mountOwnerTrusted("S-1-5-21-1111-2222-3333-1001", sidSystem) {
		t.Fatal("foreign owner accepted for SYSTEM")
	}
}
func TestBaseDirectoryUserAndSystem(t *testing.T) {
	env := map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`, "SystemRoot": `C:\Windows`}
	m := NewNoLetterMountManager("")
	m.getenv = func(k string) string { return env[k] }
	m.isSystem = func() bool { return false }
	if got, e := m.BasePath(); e != nil || got != `C:\Users\u\AppData\Local\FileDO\FMS` {
		t.Fatalf("user base %q %v", got, e)
	}
	m.isSystem = func() bool { return true }
	if got, e := m.BasePath(); e != nil || got != `C:\Windows\System32\config\systemprofile\AppData\Local\FileDO\FMS` {
		t.Fatalf("SYSTEM base %q %v", got, e)
	}
	env["SystemRoot"] = ""
	if _, e := m.BasePath(); !errors.Is(e, ErrMountPointCreation) {
		t.Fatalf("SYSTEM without SystemRoot: %v", e)
	}
	m.isSystem = func() bool { return false }
	env["LOCALAPPDATA"] = `relative\path`
	if _, e := m.BasePath(); !errors.Is(e, ErrMountPointCreation) {
		t.Fatalf("relative LOCALAPPDATA: %v", e)
	}
	env["LOCALAPPDATA"] = ""
	if _, e := m.BasePath(); !errors.Is(e, ErrMountPointCreation) {
		t.Fatalf("empty LOCALAPPDATA: %v", e)
	}
	if got, _ := NewNoLetterMountManager(`D:\fixed`).BasePath(); got != `D:\fixed` {
		t.Fatalf("explicit base %q", got)
	}
}

// fakeDirSystem drives secureMountDirectoryWith without touching the OS.
type fakeDirSystem struct {
	user      string
	createErr error
	attrs     uint32
	attrsErr  error
	owner     string
	ownerErr  error
	setErr    error
	calls     []string
	acl       *windows.ACL
}

func (f *fakeDirSystem) sys() dirSystem {
	return dirSystem{
		userSID: func() (string, error) { return f.user, nil },
		create: func(string, *windows.SECURITY_DESCRIPTOR) error {
			f.calls = append(f.calls, "create")
			return f.createErr
		},
		attrs:   func(string) (uint32, error) { f.calls = append(f.calls, "attrs"); return f.attrs, f.attrsErr },
		owner:   func(string) (string, error) { f.calls = append(f.calls, "owner"); return f.owner, f.ownerErr },
		setDACL: func(_ string, a *windows.ACL) error { f.calls = append(f.calls, "setDACL"); f.acl = a; return f.setErr },
	}
}

const fakeUser = "S-1-5-21-1111-2222-3333-1001"

func existingDir() *fakeDirSystem {
	return &fakeDirSystem{user: fakeUser, createErr: windows.ERROR_ALREADY_EXISTS, attrs: windows.FILE_ATTRIBUTE_DIRECTORY, owner: fakeUser}
}
func TestSecureMountDirectoryCreatesWithoutTouchingExisting(t *testing.T) {
	f := &fakeDirSystem{user: fakeUser}
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(f.calls, []string{"create"}) {
		t.Fatalf("calls %q", f.calls)
	}
	f = &fakeDirSystem{user: fakeUser, createErr: windows.ERROR_ACCESS_DENIED}
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, windows.ERROR_ACCESS_DENIED) || len(f.calls) != 1 {
		t.Fatalf("create failure: %v %q", e, f.calls)
	}
	bad := dirSystem{userSID: func() (string, error) { return "", windows.ERROR_NO_TOKEN }}
	if e := secureMountDirectoryWith(`C:\x`, bad); !errors.Is(e, windows.ERROR_NO_TOKEN) {
		t.Fatalf("token failure: %v", e)
	}
}
func TestSecureMountDirectoryExistingBranch(t *testing.T) {
	// Reparse points and plain files are never reused, and are never re-ACLed.
	for name, attrs := range map[string]uint32{
		"reparse dir":  windows.FILE_ATTRIBUTE_DIRECTORY | windows.FILE_ATTRIBUTE_REPARSE_POINT,
		"reparse file": windows.FILE_ATTRIBUTE_REPARSE_POINT,
		"plain file":   windows.FILE_ATTRIBUTE_NORMAL,
	} {
		f := existingDir()
		f.attrs = attrs
		if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, ErrMountPointACL) {
			t.Fatalf("%s: %v", name, e)
		}
		if len(f.calls) == 0 || containsCall(f.calls, "owner") || containsCall(f.calls, "setDACL") {
			t.Fatalf("%s: calls %q", name, f.calls)
		}
	}
	f := existingDir()
	f.attrsErr = windows.ERROR_ACCESS_DENIED
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, windows.ERROR_ACCESS_DENIED) || containsCall(f.calls, "setDACL") {
		t.Fatalf("attrs failure: %v %q", e, f.calls)
	}
	f = existingDir()
	f.ownerErr = windows.ERROR_ACCESS_DENIED
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, windows.ERROR_ACCESS_DENIED) || containsCall(f.calls, "setDACL") {
		t.Fatalf("owner failure: %v %q", e, f.calls)
	}

	// Foreign owners are rejected before any ACL change.
	for _, owner := range []string{"S-1-5-21-1111-2222-3333-1002", "S-1-1-0", "S-1-5-32-545"} {
		f = existingDir()
		f.owner = owner
		if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, ErrMountPointACL) || containsCall(f.calls, "setDACL") {
			t.Fatalf("owner %s: %v %q", owner, e, f.calls)
		}
	}

	// The user, SYSTEM and Administrators (an elevated admin token's default
	// owner) are accepted; the protected DACL is re-applied.
	for _, owner := range []string{fakeUser, sidSystem, sidAdministrators} {
		f = existingDir()
		f.owner = owner
		if e := secureMountDirectoryWith(`C:\x`, f.sys()); e != nil {
			t.Fatalf("owner %s: %v", owner, e)
		}
		if !reflect.DeepEqual(f.calls, []string{"create", "attrs", "owner", "setDACL"}) || f.acl == nil || f.acl.AceCount != 2 {
			t.Fatalf("owner %s: calls %q acl %+v", owner, f.calls, f.acl)
		}
	}
	f = existingDir()
	f.setErr = windows.ERROR_ACCESS_DENIED
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("re-ACL failure hidden: %v", e)
	}
}
func TestSecureMountDirectorySystemToken(t *testing.T) {
	f := existingDir()
	f.user = sidSystem
	f.owner = sidSystem
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); e != nil {
		t.Fatal(e)
	}
	if f.acl == nil || f.acl.AceCount != 1 {
		t.Fatalf("SYSTEM DACL must hold only SYSTEM: %+v", f.acl)
	}
	// SYSTEM accepts an Administrators-owned directory but not a user-owned one.
	f = existingDir()
	f.user = sidSystem
	f.owner = sidAdministrators
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); e != nil {
		t.Fatal(e)
	}
	f = existingDir()
	f.user = sidSystem
	f.owner = fakeUser
	if e := secureMountDirectoryWith(`C:\x`, f.sys()); !errors.Is(e, ErrMountPointACL) {
		t.Fatalf("user-owned directory under SYSTEM: %v", e)
	}
}
func containsCall(calls []string, c string) bool {
	for _, x := range calls {
		if x == c {
			return true
		}
	}
	return false
}

type aceInfo struct {
	Type, Flags uint8
	Mask        uint32
	SID         string
}

func daclOf(t *testing.T, path string) (windows.SECURITY_DESCRIPTOR_CONTROL, []aceInfo) {
	t.Helper()
	sd, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		t.Fatal(e)
	}
	control, _, e := sd.Control()
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil {
		t.Fatalf("no DACL: %v", e)
	}
	var out []aceInfo
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &ace); e != nil {
			t.Fatal(e)
		}
		out = append(out, aceInfo{ace.Header.AceType, ace.Header.AceFlags, uint32(ace.Mask), (*windows.SID)(unsafe.Pointer(&ace.SidStart)).String()})
	}
	return control, out
}

// requireExactDACL: protected, exactly the SYSTEM and current-user (or SYSTEM
// alone) allow-ACEs, full control, inherited to children, none of them inherited.
func requireExactDACL(t *testing.T, path string) {
	t.Helper()
	cur, e := currentUserSID()
	if e != nil {
		t.Fatal(e)
	}
	control, got := daclOf(t, path)
	if control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("DACL not protected (control %#x)", control)
	}
	const fullControl, objectAndContainerInherit = 0x1F01FF, 0x03
	sids := []string{sidSystem}
	if cur != sidSystem {
		sids = append(sids, cur)
	}
	var want []aceInfo
	for _, s := range sids {
		want = append(want, aceInfo{windows.ACCESS_ALLOWED_ACE_TYPE, objectAndContainerInherit, fullControl, s})
	}
	sort.Slice(got, func(i, j int) bool { return got[i].SID < got[j].SID })
	sort.Slice(want, func(i, j int) bool { return want[i].SID < want[j].SID })
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DACL\n got  %+v\n want %+v", got, want)
	}
}
func TestPrivateMountDirectoryHasProtectedACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mount")
	if e := secureMountDirectory(path); e != nil {
		t.Fatal(e)
	}
	requireExactDACL(t, path)
}

// The second run must reuse the directory this process itself created, whatever
// owner its token gave it (user, or Administrators for an elevated admin), and
// must repair a widened DACL back to the exact shape.
func TestPrivateMountDirectoryRerunAndRepair(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mount")
	if e := secureMountDirectory(path); e != nil {
		t.Fatal(e)
	}
	if e := secureMountDirectory(path); e != nil {
		t.Fatalf("second run rejected our own directory: %v", e)
	}
	requireExactDACL(t, path)

	wide, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;WD)")
	if e != nil {
		t.Fatal(e)
	}
	acl, _, e := wide.DACL()
	if e != nil {
		t.Fatal(e)
	}
	if e = windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil); e != nil {
		t.Skipf("cannot widen the DACL on this machine: %v", e)
	}
	if _, got := daclOf(t, path); len(got) != 1 || got[0].SID != "S-1-1-0" {
		t.Fatalf("setup did not widen the DACL: %+v", got)
	}
	if e = secureMountDirectory(path); e != nil {
		t.Fatal(e)
	}
	requireExactDACL(t, path)
}
func TestSecureMountDirectoryRefusesPlainFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if e := os.WriteFile(path, []byte("x"), 0o600); e != nil {
		t.Fatal(e)
	}
	if e := secureMountDirectory(path); !errors.Is(e, ErrMountPointACL) {
		t.Fatalf("file reused as mount directory: %v", e)
	}
}
