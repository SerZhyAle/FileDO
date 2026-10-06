//go:build windows

package vdisk

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unsafe"
)

var (
	ErrNoLetterNotSupported = errors.New("no-letter mount not supported")
	ErrMountPointCreation   = errors.New("failed to create mount point")
	ErrMountPointACL        = errors.New("failed to set mount point ACL")
	ErrVolumeNotFound       = errors.New("volume not found")
	ErrVolumeNotMounted     = errors.New("volume not mounted")
)

const (
	sidSystem         = "S-1-5-18"
	sidAdministrators = "S-1-5-32-544"
)

type NoLetterMount struct {
	ContainerID         string
	VolumeGUID          string
	MountPath           string
	CreatedAt           time.Time
	IsServiceAccessible bool
	FolderMount         bool
	// FallbackReason says why the private folder mount (V3) was not used and
	// MountPath is the volume GUID path (V2). Empty when FolderMount is true.
	FallbackReason string
}

// sweepEntry is one child of the private base directory, as seen without
// following reparse points.
type sweepEntry struct {
	Name    string
	Dir     bool
	Reparse bool
	ModTime time.Time
}

// SweepReport is the outcome of the last stale-directory sweep.
type SweepReport struct {
	Removed []string
	Skipped []string
	Errors  []string
}

// sweepMinAge keeps the sweep away from a directory another process has just
// created and is about to mount.
const sweepMinAge = time.Minute

type mountOperations struct {
	secureDir    func(string) error
	removeDir    func(string) error
	setMount     func(string, string) error
	deleteMount  func(string) error
	verifyVolume func(string) error
	readDir      func(string) ([]sweepEntry, error)
	mountTarget  func(string) (string, error)
}

func nativeMountOperations() mountOperations {
	return mountOperations{secureMountDirectory, os.Remove, CreateFolderMountPoint, DeleteVolumeMountPoint, verifyVolumeGUID, readSweepEntries, volumeNameForMountPoint}
}

type NoLetterMountManager struct {
	mu        sync.RWMutex
	mounts    map[string]*NoLetterMount
	guids     map[string]string
	paths     map[string]string
	basePath  string
	ops       mountOperations
	isSystem  func() bool
	getenv    func(string) string
	now       func() time.Time
	swept     bool
	lastSweep SweepReport
}

func NewNoLetterMountManager(base string) *NoLetterMountManager {
	return &NoLetterMountManager{mounts: map[string]*NoLetterMount{}, guids: map[string]string{}, paths: map[string]string{}, basePath: base, ops: nativeMountOperations(), isSystem: isRunningAsLocalSystem, getenv: os.Getenv, now: time.Now}
}

var globalNoLetterMountManager = NewNoLetterMountManager("")

func GetNoLetterMountManager() *NoLetterMountManager { return globalNoLetterMountManager }
func currentUserSID() (string, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return "", e
	}
	return u.User.Sid.String(), nil
}
func isRunningAsLocalSystem() bool {
	s, e := currentUserSID()
	return e == nil && s == sidSystem
}
func (m *NoLetterMountManager) baseLocked() (string, error) {
	if m.basePath != "" {
		return m.basePath, nil
	}
	// An operator-provided base wins over both profile defaults. The FMS worker's
	// service mode sets it (FDO_NOLETTER_BASE): a disk it mounts for its phones
	// must also be reachable by the enrolled management user, and the system
	// profile's ancestors deny traversal to everyone but SYSTEM - a DACL on the
	// mount directory alone cannot fix that. The value must already carry the
	// private base's shape (...FileDO\FMS), so the recorded-base checks of the
	// cleanup path accept it unchanged.
	if override := filepath.Clean(m.getenv(NoLetterBaseEnv)); override != "." && filepath.IsAbs(override) && noLetterBaseShape(override) {
		return override, nil
	}
	root := m.getenv("LOCALAPPDATA")
	if m.isSystem() {
		root = filepath.Join(m.getenv("SystemRoot"), "System32", "config", "systemprofile", "AppData", "Local")
	}
	if root == "" || !filepath.IsAbs(root) {
		return "", ErrMountPointCreation
	}
	return filepath.Join(root, "FileDO", "FMS"), nil
}

// NoLetterBaseEnv names the operator-provided no-letter mount base. Empty means
// the profile defaults. Only the process's own operator sets it (the FMS worker);
// an arbitrary value is still confined: it must be absolute and carry the
// ...FileDO\FMS shape the cleanup path verifies recorded bases against.
const NoLetterBaseEnv = "FDO_NOLETTER_BASE"

// noLetterBaseShape mirrors the cleanup path's private-base rule: the base is a
// directory named FMS inside a directory named FileDO. Kept local so the vdisk
// package does not import the command layer.
func noLetterBaseShape(base string) bool {
	return strings.EqualFold(filepath.Base(base), "FMS") &&
		strings.EqualFold(filepath.Base(filepath.Dir(base)), "FileDO")
}

func (m *NoLetterMountManager) BasePath() (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.baseLocked()
}
func mountSDDL() (string, error) {
	sid, e := currentUserSID()
	if e != nil {
		return "", e
	}
	return mountSDDLForSID(sid), nil
}

// MountGrantSIDEnv names one extra string SID the protected mount DACL grants
// full control to - beside SYSTEM and the running user. The FMS worker passes
// its enrolled management SID (its --manage-sid), so a disk the service mounts
// stays private to the machine account and to the person who shares it; every
// other principal is still locked out, and the DACL stays protected (no
// inheritance). A value that is not a well-formed SID is ignored whole.
const MountGrantSIDEnv = "FDO_MOUNT_GRANT_SID"

// grantEnv is a var so tests can inject the environment the DACL builder reads.
var grantEnv = os.Getenv

// mountSDDLForSID is a protected DACL: SYSTEM plus the running user, nobody else.
func mountSDDLForSID(sid string) string {
	s := "D:P(A;OICI;FA;;;SY)"
	if sid != sidSystem {
		s += "(A;OICI;FA;;;" + sid + ")"
	}
	if granted := strings.TrimSpace(grantEnv(MountGrantSIDEnv)); granted != sid && granted != sidSystem && validStringSID(granted) {
		s += "(A;OICI;FA;;;" + granted + ")"
	}
	return s
}

// validStringSID reports whether value parses as a Windows SID string. The DACL
// string is built by concatenation, so an unvalidated value could inject ACEs -
// the same rule the control pipe's SDDL builder follows.
func validStringSID(value string) bool {
	if value == "" {
		return false
	}
	var sid *windows.SID
	if e := windows.ConvertStringSidToSid(windows.StringToUTF16Ptr(value), &sid); e != nil {
		return false
	}
	windows.LocalFree(windows.Handle(unsafe.Pointer(sid)))
	return true
}

// mountOwnerTrusted accepts the owners the OS gives a directory the process
// created: the user itself, SYSTEM, and Administrators (the default owner of
// an elevated admin token). Any other owner is foreign.
func mountOwnerTrusted(owner, user string) bool {
	return owner == user || owner == sidSystem || owner == sidAdministrators
}

// dirSystem is the file-system surface secureMountDirectory needs.
type dirSystem struct {
	userSID func() (string, error)
	create  func(path string, sd *windows.SECURITY_DESCRIPTOR) error
	attrs   func(path string) (uint32, error)
	owner   func(path string) (string, error)
	setDACL func(path string, acl *windows.ACL) error
}

func nativeDirSystem() dirSystem {
	return dirSystem{
		userSID: currentUserSID,
		create: func(path string, sd *windows.SECURITY_DESCRIPTOR) error {
			p, e := windows.UTF16PtrFromString(path)
			if e != nil {
				return e
			}
			sa := windows.SecurityAttributes{SecurityDescriptor: sd}
			sa.Length = uint32(unsafe.Sizeof(sa))
			return windows.CreateDirectory(p, &sa)
		},
		attrs: func(path string) (uint32, error) {
			p, e := windows.UTF16PtrFromString(path)
			if e != nil {
				return 0, e
			}
			return windows.GetFileAttributes(p)
		},
		owner: func(path string) (string, error) {
			existing, e := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
			if e != nil {
				return "", e
			}
			o, _, e := existing.Owner()
			if e != nil {
				return "", e
			}
			return o.String(), nil
		},
		setDACL: func(path string, acl *windows.ACL) error {
			return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
		},
	}
}
func secureMountDirectory(path string) error {
	return secureMountDirectoryWith(path, nativeDirSystem())
}
func secureMountDirectoryWith(path string, sys dirSystem) error {
	user, e := sys.userSID()
	if e != nil {
		return e
	}
	sd, e := windows.SecurityDescriptorFromString(mountSDDLForSID(user))
	if e != nil {
		return e
	}
	e = sys.create(path, sd)
	if !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
		return e
	}
	// An existing directory is reused only when it is a plain directory owned
	// by someone who could not have planted it; then the DACL is re-applied.
	attrs, e := sys.attrs(path)
	if e != nil {
		return e
	}
	if attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 || attrs&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return ErrMountPointACL
	}
	owner, e := sys.owner(path)
	if e != nil {
		return e
	}
	if !mountOwnerTrusted(owner, user) {
		return ErrMountPointACL
	}
	acl, _, e := sd.DACL()
	if e != nil {
		return e
	}
	return sys.setDACL(path, acl)
}
func (m *NoLetterMountManager) Initialize() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.swept = false
	return m.initializeLocked()
}

// LastSweep reports what the last stale-directory sweep removed, left alone
// and could not handle.
func (m *NoLetterMountManager) LastSweep() SweepReport {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r := m.lastSweep
	r.Removed = append([]string(nil), r.Removed...)
	r.Skipped = append([]string(nil), r.Skipped...)
	r.Errors = append([]string(nil), r.Errors...)
	return r
}
func (m *NoLetterMountManager) initializeLocked() error {
	base, e := m.baseLocked()
	if e != nil {
		return e
	}
	// Create each new directory with the ACL already in place.
	parent := filepath.Dir(base)
	if _, e = os.Stat(parent); errors.Is(e, os.ErrNotExist) {
		if e = m.ops.secureDir(parent); e != nil {
			return e
		}
	}
	if e = m.ops.secureDir(base); e != nil {
		return e
	}
	if !m.swept {
		m.swept = true
		m.lastSweep = m.sweepLocked(base)
	}
	return nil
}

var mountNoncePattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// sweepLocked clears directories a crash left under base. It never deletes
// content: a plain directory goes only if empty (os.Remove), and a volume mount
// point only when its target volume is gone, by detaching it first. A mount
// whose volume still answers, or whose state cannot be read, is left alone.
func (m *NoLetterMountManager) sweepLocked(base string) SweepReport {
	var r SweepReport
	entries, e := m.ops.readDir(base)
	if e != nil {
		if !errors.Is(e, os.ErrNotExist) {
			r.Errors = append(r.Errors, e.Error())
		}
		return r
	}
	now := m.now()
	for _, en := range entries {
		path := filepath.Join(base, en.Name)
		if !en.Dir || !mountNoncePattern.MatchString(en.Name) || m.paths[pathKey(path)] != "" || now.Sub(en.ModTime) < sweepMinAge {
			continue
		}
		if en.Reparse {
			target, e := m.ops.mountTarget(path)
			if e != nil {
				r.Skipped = append(r.Skipped, path)
				continue
			}
			if verr := m.ops.verifyVolume(target); verr == nil || !(errors.Is(verr, windows.ERROR_FILE_NOT_FOUND) || errors.Is(verr, windows.ERROR_PATH_NOT_FOUND)) {
				r.Skipped = append(r.Skipped, path)
				continue
			}
			if e := m.ops.deleteMount(path); e != nil && !mountGone(e) {
				r.Errors = append(r.Errors, e.Error())
				continue
			}
		}
		if e := m.ops.removeDir(path); e != nil && !errors.Is(e, os.ErrNotExist) {
			r.Skipped = append(r.Skipped, path)
			continue
		}
		r.Removed = append(r.Removed, path)
	}
	return r
}

// mountGone matches the errors a detach returns when nothing is left to detach.
func mountGone(e error) bool {
	return errors.Is(e, windows.ERROR_NOT_A_REPARSE_POINT) || errors.Is(e, windows.ERROR_FILE_NOT_FOUND) || errors.Is(e, windows.ERROR_PATH_NOT_FOUND)
}
func readSweepEntries(dir string) ([]sweepEntry, error) {
	des, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	out := make([]sweepEntry, 0, len(des))
	for _, de := range des {
		p, e := windows.UTF16PtrFromString(filepath.Join(dir, de.Name()))
		if e != nil {
			continue
		}
		attrs, e := windows.GetFileAttributes(p)
		if e != nil {
			continue
		}
		info, e := de.Info()
		if e != nil {
			continue
		}
		out = append(out, sweepEntry{Name: de.Name(), Dir: attrs&windows.FILE_ATTRIBUTE_DIRECTORY != 0, Reparse: attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, ModTime: info.ModTime()})
	}
	return out, nil
}
func volumeNameForMountPoint(path string) (string, error) {
	p, e := windows.UTF16PtrFromString(strings.TrimRight(path, `\`) + `\`)
	if e != nil {
		return "", e
	}
	buf := make([]uint16, 64)
	if e = windows.GetVolumeNameForVolumeMountPoint(p, &buf[0], uint32(len(buf))); e != nil {
		return "", e
	}
	return windows.UTF16ToString(buf), nil
}

var volumeGUIDPattern = regexp.MustCompile(`(?i)^\\\\\?\\Volume\{[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\}\\$`)

func normalizeVolumeGUID(v string) (string, error) {
	if !strings.HasPrefix(v, `\\?\Volume{`) {
		v = `\\?\Volume{` + strings.Trim(v, "{}\\") + `}\`
	}
	if !volumeGUIDPattern.MatchString(v) {
		return "", ErrVolumeNotFound
	}
	return v, nil
}
func verifyVolumeGUID(v string) error {
	p, e := windows.UTF16PtrFromString(v)
	if e != nil {
		return e
	}
	return windows.GetVolumeInformation(p, nil, 0, nil, nil, nil, nil, 0)
}
func (m *NoLetterMountManager) CreateNoLetterMount(id, guid string) (*NoLetterMount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" || m.mounts[id] != nil {
		return nil, fmt.Errorf("%w: a no-letter mount of container %q is already registered in this process (or the id is empty)", ErrNoLetterNotSupported, id)
	}
	vol, e := normalizeVolumeGUID(guid)
	if e != nil {
		return nil, e
	}
	if m.guids[strings.ToLower(vol)] != "" {
		return nil, fmt.Errorf("%w: volume %s is already registered as a no-letter mount in this process", ErrNoLetterNotSupported, vol)
	}
	if e = m.ops.verifyVolume(vol); e != nil {
		return nil, fmt.Errorf("%w: %v", ErrVolumeNotFound, e)
	}
	mount := &NoLetterMount{ContainerID: id, VolumeGUID: vol, MountPath: vol, CreatedAt: time.Now(), IsServiceAccessible: true}
	if e = m.initializeLocked(); e != nil {
		mount.FallbackReason = "private mount base unavailable: " + e.Error()
	} else {
		base, _ := m.baseLocked()
		var nonce [16]byte
		if _, e = rand.Read(nonce[:]); e != nil {
			return nil, e
		}
		dir := filepath.Join(base, hex.EncodeToString(nonce[:]))
		if e = m.ops.secureDir(dir); e != nil {
			mount.FallbackReason = "private mount directory not secured: " + e.Error()
		} else if e = m.ops.setMount(vol, dir); e != nil {
			if cleanup := m.ops.removeDir(dir); cleanup != nil {
				return nil, fmt.Errorf("mount failed and directory cleanup failed: %w", cleanup)
			}
			mount.FallbackReason = "folder mount point not set: " + e.Error()
		} else {
			mount.MountPath = dir
			mount.FolderMount = true
		}
	}
	// V2 uses the verified volume GUID when the private folder mount is unavailable.
	m.mounts[id] = mount
	m.guids[strings.ToLower(vol)] = id
	m.paths[pathKey(mount.MountPath)] = id
	v := *mount
	return &v, nil
}
func (m *NoLetterMountManager) GetNoLetterMount(id string) (*NoLetterMount, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	d := m.mounts[id]
	if d == nil {
		return nil, false
	}
	v := *d
	return &v, true
}
func (m *NoLetterMountManager) GetNoLetterMountByPath(path string) (*NoLetterMount, bool) {
	m.mu.RLock()
	id := m.paths[pathKey(path)]
	m.mu.RUnlock()
	return m.GetNoLetterMount(id)
}
func (m *NoLetterMountManager) GetNoLetterMountByGUID(guid string) (*NoLetterMount, bool) {
	m.mu.RLock()
	id := m.guids[strings.ToLower(guid)]
	m.mu.RUnlock()
	return m.GetNoLetterMount(id)
}
func (m *NoLetterMountManager) DestroyNoLetterMount(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.destroyLocked(id)
}
func (m *NoLetterMountManager) destroyLocked(id string) error {
	d := m.mounts[id]
	if d == nil {
		return ErrVolumeNotMounted
	}
	if d.FolderMount {
		// Both steps tolerate "already gone", so a retry after a half-finished
		// destroy (detached, directory still there) completes instead of failing.
		if e := m.ops.deleteMount(d.MountPath); e != nil && !mountGone(e) {
			return e
		}
		if e := m.ops.removeDir(d.MountPath); e != nil && !errors.Is(e, os.ErrNotExist) {
			return e
		}
	}
	delete(m.mounts, id)
	delete(m.guids, strings.ToLower(d.VolumeGUID))
	delete(m.paths, pathKey(d.MountPath))
	return nil
}
func (m *NoLetterMountManager) DestroyAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs []error
	for id := range m.mounts {
		if e := m.destroyLocked(id); e != nil {
			errs = append(errs, e)
		}
	}
	return errors.Join(errs...)
}
func (m *NoLetterMountManager) MountPathForContainer(id string) (string, bool) {
	d, ok := m.GetNoLetterMount(id)
	if !ok {
		return "", false
	}
	return d.MountPath, true
}
func (m *NoLetterMountManager) VolumeGUIDForContainer(id string) (string, bool) {
	d, ok := m.GetNoLetterMount(id)
	if !ok {
		return "", false
	}
	return d.VolumeGUID, true
}
func (m *NoLetterMountManager) VerifyMountPointAccessible(path string) error {
	_, e := os.Stat(path)
	return e
}
func CreateFolderMountPoint(guid, path string) error {
	v, e := normalizeVolumeGUID(guid)
	if e != nil {
		return e
	}
	vp, e := windows.UTF16PtrFromString(v)
	if e != nil {
		return e
	}
	mp, e := windows.UTF16PtrFromString(strings.TrimRight(path, `\`) + `\`)
	if e != nil {
		return e
	}
	return windows.SetVolumeMountPoint(mp, vp)
}
func DeleteVolumeMountPoint(path string) error {
	p, e := windows.UTF16PtrFromString(strings.TrimRight(path, `\`) + `\`)
	if e != nil {
		return e
	}
	return windows.DeleteVolumeMountPoint(p)
}
func GetVolumePath(id, guid string) string {
	if d, ok := globalNoLetterMountManager.GetNoLetterMount(id); ok {
		return d.MountPath
	}
	v, _ := normalizeVolumeGUID(guid)
	return v
}
func GetStableVolumePath(id, guid string) string { v, _ := normalizeVolumeGUID(guid); return v }

// VolumeDevicePath is the device path (\\.\Volume{GUID}) of a volume named by its GUID path, for a handle
// that flushes or locks it. The GUID is pattern-checked: a request never supplies a path.
func VolumeDevicePath(guid string) (string, error) {
	v, e := normalizeVolumeGUID(guid)
	if e != nil {
		return "", e
	}
	return `\\.\` + strings.TrimSuffix(strings.TrimPrefix(v, `\\?\`), `\`), nil
}
