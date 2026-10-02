// Package vdisk - SP-0121 FMS Share Integration: shared disk state management.
//
// This file implements the shared disk state tracking and holder rules for
// virtual disks shared through FMS for Windows.

package vdisk

import (
	"encoding/json"
	"errors"
	"filedo/statedir"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Errors for shared disk operations.
var (
	ErrDiskNotShared = errors.New("disk is not shared")
	ErrAlreadyShared = errors.New("disk is already shared")
	ErrWrongHolder   = errors.New("disk is held by another process")
	ErrNoHolder      = errors.New("no holder for this disk")
	ErrSharedBusy    = errors.New("shared disk is busy")
	ErrAutostartKey  = errors.New("autostart key error")
)

// DiskHolder indicates who currently holds/controls a disk.
type DiskHolder int

const (
	HolderNone       DiskHolder = iota // Disk is not held by anyone
	HolderFileDO                       // Disk is held by FileDO (user session)
	HolderFMSService                   // Disk is held by FMS service (Server edition)
	HolderFMSSession                   // Disk is held by FMS worker in user session (User edition)
)

// String returns a human-readable name for the holder.
func (h DiskHolder) String() string {
	switch h {
	case HolderNone:
		return "none"
	case HolderFileDO:
		return "FileDO"
	case HolderFMSService:
		return "FMS Service"
	case HolderFMSSession:
		return "FMS Session"
	default:
		return "unknown"
	}
}

// SharedDiskState represents the sharing state of a virtual disk.
type SharedDiskState struct {
	// ContainerID is the unique identifier of the container
	ContainerID string `json:"container_id"`

	// ContainerPath is the absolute path to the .fdd file
	ContainerPath string `json:"container_path"`

	// Shared indicates if the disk is currently shared through FMS
	Shared bool `json:"shared"`

	// RootName is the name used for the SFTP root
	RootName string `json:"root_name"`

	// Holder indicates who currently holds the disk
	Holder DiskHolder `json:"holder"`

	// State indicates the current operational state
	State DiskState `json:"state"`

	// ReadOnly indicates if the share is read-only
	ReadOnly bool `json:"read_only"`

	// Autostart indicates if the disk should be automatically opened at boot
	Autostart bool `json:"autostart"`

	// HasStoredKey indicates if an encryption key is stored for autostart
	HasStoredKey bool `json:"has_stored_key"`

	// Encrypted indicates the container needs a password; autostart then needs a stored key
	Encrypted bool `json:"encrypted,omitempty"`

	// MountPath is the path where the disk is mounted (for debugging)
	MountPath string `json:"mount_path,omitempty"`

	// OpenHandles is the number of open file handles (for drain logic)
	OpenHandles int `json:"open_handles"`

	// SharedAt is when the disk was first shared
	SharedAt time.Time `json:"shared_at"`

	// LastOpened is when the disk was last opened
	LastOpened time.Time `json:"last_opened,omitempty"`

	// LastClosed is when the disk was last closed
	LastClosed time.Time `json:"last_closed,omitempty"`

	// ErrorMessage contains the last error message (if any)
	ErrorMessage string `json:"error_message,omitempty"`
}

// persisted returns the part of the record that survives a restart. Load resets
// the rest, so it is neither written nor a reason to write.
func (d SharedDiskState) persisted() SharedDiskState {
	d.OpenHandles = 0
	d.Holder = HolderNone
	d.MountPath = ""
	d.State = StateClosed
	return d
}

// DiskState represents the operational state of a shared disk.
type DiskState int

const (
	StateUnshared DiskState = iota // Disk is not shared
	StateClosed                    // Disk is shared but closed
	StateOpening                   // Disk is in the process of being opened
	StateOpen                      // Disk is shared and open (mounted and ready)
	StateClosing                   // Disk is in the process of being closed (draining)
	StateFailed                    // Disk failed to open or mount
	StateLocked                    // Disk is encrypted and needs a password
)

// String returns a human-readable name for the state.
func (s DiskState) String() string {
	switch s {
	case StateUnshared:
		return "unshared"
	case StateClosed:
		return "closed"
	case StateOpening:
		return "opening"
	case StateOpen:
		return "open"
	case StateClosing:
		return "closing"
	case StateFailed:
		return "failed"
	case StateLocked:
		return "locked"
	default:
		return "unknown"
	}
}

// AutostartKeyStore stores protected bytes under the holder's account.
// FileDO's snapshot uses no backend: the authoritative worker owns credentials.
type AutostartKeyStore interface {
	Store(string, []byte) error
	Destroy(string) error
	Load(string) ([]byte, error)
}
type SharedDiskManager struct {
	mu      sync.RWMutex
	disks   map[string]*SharedDiskState
	pathMap map[string]string
	file    string
	keys    AutostartKeyStore
}

func NewSharedDiskManager(file string, keys AutostartKeyStore) *SharedDiskManager {
	return &SharedDiskManager{disks: map[string]*SharedDiskState{}, pathMap: map[string]string{}, file: file, keys: keys}
}

var globalSharedDiskManager = NewSharedDiskManager("", nil)

func GetSharedDiskManager() *SharedDiskManager { return globalSharedDiskManager }

// pathKey canonicalises a container path so that two spellings of one file
// (relative, case, \\?\ prefix, symlink or 8.3 name of an existing file) share a key.
func pathKey(p string) string {
	if strings.HasPrefix(p, `\\?\UNC\`) {
		p = `\\` + p[len(`\\?\UNC\`):]
	} else if strings.HasPrefix(p, `\\?\`) {
		p = p[len(`\\?\`):]
	}
	if a, e := filepath.Abs(p); e == nil {
		p = a
	}
	p = filepath.Clean(p)
	if !strings.HasPrefix(p, `\\`) { // a dead UNC host would stall the lookup
		if r, e := filepath.EvalSymlinks(p); e == nil {
			p = r
		}
	}
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}
func (m *SharedDiskManager) saveLocked() error {
	if m.file == "" {
		return nil
	}
	disks := make(map[string]*SharedDiskState, len(m.disks))
	for id, d := range m.disks {
		v := d.persisted()
		disks[id] = &v
	}
	b, e := json.Marshal(struct {
		Version int                         `json:"version"`
		Disks   map[string]*SharedDiskState `json:"disks"`
	}{1, disks})
	if e != nil {
		return e
	}
	return statedir.WriteFileAtomic(m.file, b, 0600)
}

// validateRecord is the identity check shared by Load and Reconcile.
func validateRecord(d *SharedDiskState) error {
	if d == nil || d.ContainerID == "" || !d.Shared || d.ContainerPath == "" || d.RootName == "" {
		return errors.New("invalid shared-state record")
	}
	return nil
}
func (m *SharedDiskManager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.file == "" {
		return nil
	}
	b, e := os.ReadFile(m.file)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	var v struct {
		Version int                         `json:"version"`
		Disks   map[string]*SharedDiskState `json:"disks"`
	}
	if e = json.Unmarshal(b, &v); e != nil {
		return e
	}
	if v.Version != 1 {
		return errors.New("unsupported shared-state version")
	}
	paths := map[string]string{}
	names := map[string]bool{}
	for id, d := range v.Disks {
		if d == nil || id == "" || d.ContainerID != id || validateRecord(d) != nil {
			return errors.New("invalid shared-state record")
		}
		key := pathKey(d.ContainerPath)
		name := strings.ToLower(d.RootName)
		if _, ok := paths[key]; ok || names[name] {
			return errors.New("duplicate shared-state record")
		}
		paths[key] = id
		names[name] = true
		d.OpenHandles = 0
		d.Holder = HolderNone
		d.MountPath = ""
		d.State = StateClosed
	}
	m.disks = v.Disks
	if m.disks == nil {
		m.disks = map[string]*SharedDiskState{}
	}
	m.pathMap = paths
	return nil
}
func (m *SharedDiskManager) RegisterSharedDisk(id, path, name string, ro bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" || path == "" || name == "" {
		return ErrUsage
	}
	if m.disks[id] != nil || m.pathMap[pathKey(path)] != "" {
		return ErrAlreadyShared
	}
	for _, d := range m.disks {
		if strings.EqualFold(d.RootName, name) {
			return errors.New("root name already exists")
		}
	}
	m.disks[id] = &SharedDiskState{ContainerID: id, ContainerPath: path, RootName: name, Shared: true, ReadOnly: ro, State: StateClosed, SharedAt: time.Now()}
	m.pathMap[pathKey(path)] = id
	if e := m.saveLocked(); e != nil {
		delete(m.disks, id)
		delete(m.pathMap, pathKey(path))
		return e
	}
	return nil
}
func (m *SharedDiskManager) UnregisterSharedDisk(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.disks[id]
	if d == nil {
		return ErrDiskNotShared
	}
	if d.OpenHandles != 0 || d.State == StateOpen || d.State == StateOpening || d.State == StateClosing {
		return ErrSharedBusy
	}
	if m.keys != nil {
		if e := m.keys.Destroy(id); e != nil {
			return e
		}
	} else if d.HasStoredKey {
		return ErrAutostartKey
	}
	delete(m.disks, id)
	delete(m.pathMap, pathKey(d.ContainerPath))
	if e := m.saveLocked(); e != nil {
		d.HasStoredKey = false
		d.Autostart = false
		m.disks[id] = d
		m.pathMap[pathKey(d.ContainerPath)] = id
		return e
	}
	return nil
}
func copyShared(d *SharedDiskState) (*SharedDiskState, bool) {
	if d == nil {
		return nil, false
	}
	v := *d
	return &v, true
}
func (m *SharedDiskManager) GetSharedDiskState(id string) (*SharedDiskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return copyShared(m.disks[id])
}
func (m *SharedDiskManager) GetSharedDiskStateByPath(path string) (*SharedDiskState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return copyShared(m.disks[m.pathMap[pathKey(path)]])
}
func (m *SharedDiskManager) mutate(id string, fn func(*SharedDiskState) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.disks[id]
	if d == nil {
		return ErrDiskNotShared
	}
	old := *d
	if e := fn(d); e != nil {
		*d = old
		return e
	}
	// Handle counts, state, holder and mount path are reset by Load: a change
	// confined to them is applied in memory without touching the file.
	if d.persisted() == old.persisted() {
		return nil
	}
	if e := m.saveLocked(); e != nil {
		*d = old
		return e
	}
	return nil
}

// checkHolder is the single holder rule behind SetDiskHolder and CanBeHeldBy.
// Every record here is shared, and a shared disk is never mounted by FileDO
// (DISK-SHARE-17), so FileDO is not an allowed holder.
func checkHolder(d *SharedDiskState, h DiskHolder) error {
	if h < HolderNone || h > HolderFMSSession || h == HolderFileDO {
		return ErrWrongHolder
	}
	if h != HolderNone && d.Holder != HolderNone && d.Holder != h {
		return ErrWrongHolder
	}
	if h == HolderNone && (d.OpenHandles > 0 || d.State == StateOpen || d.State == StateOpening || d.State == StateClosing) {
		return ErrSharedBusy
	}
	return nil
}
func (m *SharedDiskManager) SetDiskHolder(id string, h DiskHolder) error {
	return m.mutate(id, func(d *SharedDiskState) error {
		if e := checkHolder(d, h); e != nil {
			return e
		}
		d.Holder = h
		return nil
	})
}
func (m *SharedDiskManager) GetDiskHolder(id string) (DiskHolder, error) {
	d, ok := m.GetSharedDiskState(id)
	if !ok {
		return HolderNone, ErrDiskNotShared
	}
	return d.Holder, nil
}
func (m *SharedDiskManager) SetDiskState(id string, state DiskState) error {
	return m.mutate(id, func(d *SharedDiskState) error {
		if state < StateClosed || state > StateLocked {
			return ErrUsage
		}
		if state == StateClosed && d.OpenHandles > 0 {
			return ErrSharedBusy
		}
		d.State = state
		switch state {
		case StateOpen:
			d.LastOpened = time.Now()
			d.ErrorMessage = ""
		case StateClosed:
			d.LastClosed = time.Now()
			d.MountPath = ""
		}
		return nil
	})
}
func (m *SharedDiskManager) SetDiskError(id, msg string) error {
	return m.mutate(id, func(d *SharedDiskState) error { d.ErrorMessage = msg; return nil })
}
func (m *SharedDiskManager) SetAutostart(id string, enable bool, password []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.disks[id]
	if d == nil {
		return ErrDiskNotShared
	}
	old := *d
	if !enable {
		if m.keys != nil {
			if e := m.keys.Destroy(id); e != nil {
				return e
			}
		} else if d.HasStoredKey {
			return ErrAutostartKey
		}
		d.Autostart = false
		d.HasStoredKey = false
		// The key is gone whatever happens to the file, so memory keeps the truth.
		return m.saveLocked()
	}
	// An encrypted disk cannot autostart without a key. For a disk not known to be
	// encrypted no key is needed: the worker refuses the request if it disagrees.
	if len(password) == 0 && !d.HasStoredKey && d.Encrypted {
		return ErrAutostartKey
	}
	var restore func()
	if len(password) > 0 {
		if m.keys == nil {
			return ErrAutostartKey
		}
		prev, loadErr := m.keys.Load(id)
		if e := m.keys.Store(id, password); e != nil {
			return e
		}
		restore = func() {
			if loadErr == nil {
				m.keys.Store(id, prev)
			} else {
				m.keys.Destroy(id)
			}
		}
		d.HasStoredKey = true
	}
	d.Autostart = true
	if e := m.saveLocked(); e != nil {
		*d = old
		if restore != nil {
			restore()
		}
		return e
	}
	return nil
}
func (m *SharedDiskManager) IsShared(id string) bool {
	d, ok := m.GetSharedDiskState(id)
	return ok && d.Shared
}
func (m *SharedDiskManager) IsSharedByPath(p string) bool {
	d, ok := m.GetSharedDiskStateByPath(p)
	return ok && d.Shared
}

// CanBeHeldBy reports whether SetDiskHolder(id, h) would be accepted. An unknown
// id answers false: a missing snapshot does not prove that nobody holds the disk.
func (m *SharedDiskManager) CanBeHeldBy(id string, h DiskHolder) bool {
	d, ok := m.GetSharedDiskState(id)
	return ok && checkHolder(d, h) == nil
}
func (m *SharedDiskManager) IncrementOpenHandles(id string) error {
	return m.mutate(id, func(d *SharedDiskState) error {
		if d.State != StateOpen {
			return ErrSharedBusy
		}
		d.OpenHandles++
		return nil
	})
}
func (m *SharedDiskManager) DecrementOpenHandles(id string) error {
	return m.mutate(id, func(d *SharedDiskState) error {
		if d.OpenHandles == 0 {
			return ErrSharedBusy
		}
		d.OpenHandles--
		return nil
	})
}
func (m *SharedDiskManager) GetOpenHandles(id string) (int, error) {
	d, ok := m.GetSharedDiskState(id)
	if !ok {
		return 0, ErrDiskNotShared
	}
	return d.OpenHandles, nil
}

// StoreAutostartKey stores the key of a registered disk and records HasStoredKey.
// It does not turn autostart on.
func (m *SharedDiskManager) StoreAutostartKey(id string, b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.disks[id]
	if d == nil {
		return ErrDiskNotShared
	}
	if m.keys == nil {
		return ErrAutostartKey
	}
	if len(b) == 0 {
		return ErrUsage
	}
	old := *d
	prev, loadErr := m.keys.Load(id)
	if e := m.keys.Store(id, b); e != nil {
		return e
	}
	d.HasStoredKey = true
	if e := m.saveLocked(); e != nil {
		*d = old
		if loadErr == nil {
			m.keys.Store(id, prev)
		} else {
			m.keys.Destroy(id)
		}
		return e
	}
	return nil
}

// DestroyAutostartKey removes the key of a registered disk. A disk that needed
// the key can no longer autostart, so the mark is cleared with it.
func (m *SharedDiskManager) DestroyAutostartKey(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.disks[id]
	if d == nil {
		return ErrDiskNotShared
	}
	if m.keys == nil {
		return ErrAutostartKey
	}
	if e := m.keys.Destroy(id); e != nil {
		return e
	}
	if d.HasStoredKey || d.Encrypted {
		d.Autostart = false
	}
	d.HasStoredKey = false
	return m.saveLocked()
}
func (m *SharedDiskManager) GetAutostartKey(id string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.keys == nil {
		return nil, ErrAutostartKey
	}
	return m.keys.Load(id)
}

// Reconcile records one verified worker record without storing credentials
// locally. The record replaces the snapshot's record with the same ContainerID
// (a changed path or name is re-indexed); it is refused when invalid or when its
// path or root name belongs to another disk, leaving the snapshot untouched.
func (m *SharedDiskManager) Reconcile(d SharedDiskState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := validateRecord(&d); e != nil {
		return e
	}
	key := pathKey(d.ContainerPath)
	if other := m.pathMap[key]; other != "" && other != d.ContainerID {
		return ErrAlreadyShared
	}
	for id, o := range m.disks {
		if id != d.ContainerID && strings.EqualFold(o.RootName, d.RootName) {
			return errors.New("root name already exists")
		}
	}
	old := m.disks[d.ContainerID]
	oldKey := ""
	if old != nil {
		oldKey = pathKey(old.ContainerPath)
		delete(m.pathMap, oldKey)
	}
	m.disks[d.ContainerID] = &d
	m.pathMap[key] = d.ContainerID
	if e := m.saveLocked(); e != nil {
		delete(m.pathMap, key)
		if old == nil {
			delete(m.disks, d.ContainerID)
		} else {
			m.disks[d.ContainerID] = old
			m.pathMap[oldKey] = d.ContainerID
		}
		return e
	}
	return nil
}

// ReconcileAll makes the snapshot equal to a complete worker list: records of
// disks missing from the list are dropped (and their stored keys destroyed when a
// key backend exists). A record that is invalid or repeats the path or root name
// of an earlier one is skipped, so one bad record neither wipes the others nor
// reaches the file; the skipped reasons are returned joined after the rest has
// been applied. A failed write restores the previous snapshot.
func (m *SharedDiskManager) ReconcileAll(list []SharedDiskState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	disks := make(map[string]*SharedDiskState, len(list))
	paths := make(map[string]string, len(list))
	names := make(map[string]bool, len(list))
	var skipped []error
	for i := range list {
		d := list[i]
		if e := validateRecord(&d); e != nil {
			skipped = append(skipped, fmt.Errorf("worker record %d (%q): %w", i, d.ContainerID, e))
			continue
		}
		key, name := pathKey(d.ContainerPath), strings.ToLower(d.RootName)
		if disks[d.ContainerID] != nil || paths[key] != "" || names[name] {
			skipped = append(skipped, fmt.Errorf("worker record %d (%q): duplicate id, path or root name", i, d.ContainerID))
			continue
		}
		disks[d.ContainerID] = &d
		paths[key] = d.ContainerID
		names[name] = true
	}
	oldDisks, oldPaths := m.disks, m.pathMap
	m.disks, m.pathMap = disks, paths
	if e := m.saveLocked(); e != nil {
		m.disks, m.pathMap = oldDisks, oldPaths
		return e
	}
	if m.keys != nil {
		for id, d := range oldDisks {
			if disks[id] == nil && d.HasStoredKey {
				if e := m.keys.Destroy(id); e != nil {
					skipped = append(skipped, fmt.Errorf("destroy key of dropped disk %q: %w", id, e))
				}
			}
		}
	}
	return errors.Join(skipped...)
}
