package vdisk

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func newStateFile(t *testing.T) (string, *SharedDiskManager) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state.json")
	return p, NewSharedDiskManager(p, nil)
}

// breakFile makes every later write of the state file fail.
func breakFile(t *testing.T, p string) {
	t.Helper()
	os.Remove(p)
	if e := os.Mkdir(p, 0o700); e != nil {
		t.Fatal(e)
	}
}
func record(id, path, name string) SharedDiskState {
	return SharedDiskState{ContainerID: id, ContainerPath: path, RootName: name, Shared: true, State: StateClosed, SharedAt: time.Now()}
}
func reloaded(t *testing.T, p string) *SharedDiskManager {
	t.Helper()
	m := NewSharedDiskManager(p, nil)
	if e := m.Load(); e != nil {
		t.Fatalf("persisted file is not loadable: %v", e)
	}
	return m
}
func TestSharedStateMissingDisk(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	if d, ok := m.GetSharedDiskState("missing"); ok || d != nil {
		t.Fatal("missing id found")
	}
	if _, ok := m.GetSharedDiskStateByPath("missing.fdd"); ok {
		t.Fatal("missing path found")
	}
	if _, e := m.GetDiskHolder("missing"); !errors.Is(e, ErrDiskNotShared) {
		t.Fatal(e)
	}
	if _, e := m.GetOpenHandles("missing"); !errors.Is(e, ErrDiskNotShared) {
		t.Fatal(e)
	}
	if m.IsShared("missing") || m.IsSharedByPath("missing.fdd") {
		t.Fatal("missing disk is shared")
	}
	for name, e := range map[string]error{
		"state":     m.SetDiskState("missing", StateOpen),
		"error":     m.SetDiskError("missing", "x"),
		"holder":    m.SetDiskHolder("missing", HolderFMSService),
		"increment": m.IncrementOpenHandles("missing"),
		"decrement": m.DecrementOpenHandles("missing"),
		"autostart": m.SetAutostart("missing", true, nil),
		"unshare":   m.UnregisterSharedDisk("missing"),
	} {
		if !errors.Is(e, ErrDiskNotShared) {
			t.Errorf("%s on a missing disk: %v", name, e)
		}
	}
}
func TestSharedStateSetDiskStateAndError(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	for _, s := range []DiskState{StateUnshared, DiskState(99), DiskState(-1)} {
		if e := m.SetDiskState("id", s); !errors.Is(e, ErrUsage) {
			t.Errorf("state %d accepted: %v", s, e)
		}
	}
	m.SetDiskError("id", "mount failed")
	if e := m.SetDiskState("id", StateOpen); e != nil {
		t.Fatal(e)
	}
	d, _ := m.GetSharedDiskState("id")
	if d.State != StateOpen || d.LastOpened.IsZero() || d.ErrorMessage != "" {
		t.Fatalf("open did not clear the error or stamp the time: %+v", d)
	}
	m.IncrementOpenHandles("id")
	if e := m.SetDiskState("id", StateClosed); !errors.Is(e, ErrSharedBusy) {
		t.Fatalf("closed with a live handle: %v", e)
	}
	m.DecrementOpenHandles("id")
	m.reconcileMountPath("id", `C:\mnt`)
	if e := m.SetDiskState("id", StateClosed); e != nil {
		t.Fatal(e)
	}
	d, _ = m.GetSharedDiskState("id")
	if d.State != StateClosed || d.LastClosed.IsZero() || d.MountPath != "" {
		t.Fatalf("close did not reset: %+v", d)
	}
	if e := m.SetDiskError("id", "later"); e != nil {
		t.Fatal(e)
	}
	if d, _ = m.GetSharedDiskState("id"); d.ErrorMessage != "later" {
		t.Fatal("error message lost")
	}
}

// reconcileMountPath sets the ephemeral mount path the way a worker record would.
func (m *SharedDiskManager) reconcileMountPath(id, mount string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disks[id].MountPath = mount
}
func TestSharedStateConcurrentMixedAccess(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	m.SetDiskState("id", StateOpen)
	for i := 0; i < 100; i++ {
		m.IncrementOpenHandles("id")
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(4)
		go func() {
			defer wg.Done()
			if e := m.DecrementOpenHandles("id"); e != nil {
				t.Error(e)
			}
		}()
		go func() { defer wg.Done(); m.GetSharedDiskState("id"); m.GetOpenHandles("id") }()
		go func() { defer wg.Done(); m.SetDiskError("id", "x") }()
		go func() { defer wg.Done(); m.IsSharedByPath("a.fdd"); m.CanBeHeldBy("id", HolderFMSService) }()
	}
	wg.Wait()
	if n, _ := m.GetOpenHandles("id"); n != 0 {
		t.Fatalf("handles=%d", n)
	}
	if e := m.DecrementOpenHandles("id"); !errors.Is(e, ErrSharedBusy) {
		t.Fatalf("underflow: %v", e)
	}
}
func TestSharedStateHandleAccountingNeverTouchesTheFile(t *testing.T) {
	p, m := newStateFile(t)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	m.SetDiskState("id", StateOpen)
	before, _ := os.ReadFile(p)
	breakFile(t, p) // any write now fails
	for i := 0; i < 3; i++ {
		if e := m.IncrementOpenHandles("id"); e != nil {
			t.Fatalf("handle accounting wrote the file: %v", e)
		}
	}
	m.DecrementOpenHandles("id")
	if n, _ := m.GetOpenHandles("id"); n != 2 {
		t.Fatalf("accounting undone by a write failure: %d", n)
	}
	if e := m.SetDiskHolder("id", HolderFMSService); e != nil {
		t.Fatalf("holder change wrote the file: %v", e)
	}
	if e := m.SetDiskState("id", StateFailed); e != nil {
		t.Fatalf("state change wrote the file: %v", e)
	}
	os.Remove(p)
	os.WriteFile(p, before, 0o600)
	m.SetDiskState("id", StateOpen)
	m.IncrementOpenHandles("id")
	after, _ := os.ReadFile(p)
	if !strings.Contains(string(after), `"open_handles":0`) || !strings.Contains(string(after), `"holder":0`) || !strings.Contains(string(after), `"state":1`) {
		t.Fatalf("ephemeral fields persisted: %s", after)
	}
	d, _ := reloaded(t, p).GetSharedDiskState("id")
	if d.OpenHandles != 0 || d.Holder != HolderNone || d.State != StateClosed {
		t.Fatalf("restart state: %+v", d)
	}
}
func TestSharedStatePersistedFieldChangeIsRolledBackOnWriteFailure(t *testing.T) {
	p, m := newStateFile(t)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	breakFile(t, p)
	if e := m.SetDiskError("id", "boom"); e == nil {
		t.Fatal("write failure ignored")
	}
	d, _ := m.GetSharedDiskState("id")
	if d.ErrorMessage != "" {
		t.Fatal("failed write left the change visible")
	}
	if e := m.SetDiskState("id", StateOpen); e == nil {
		t.Fatal("write failure ignored")
	}
	if d, _ = m.GetSharedDiskState("id"); d.State != StateClosed || !d.LastOpened.IsZero() {
		t.Fatalf("failed write left the change visible: %+v", d)
	}
}
func TestSharedStateHolderRuleMatchesCanBeHeldBy(t *testing.T) {
	if NewSharedDiskManager("", nil).CanBeHeldBy("missing", HolderFMSService) {
		t.Fatal("an unknown disk was declared free")
	}
	setups := map[string]func(*SharedDiskManager){
		"idle":         func(*SharedDiskManager) {},
		"service":      func(m *SharedDiskManager) { m.SetDiskHolder("id", HolderFMSService) },
		"session":      func(m *SharedDiskManager) { m.SetDiskHolder("id", HolderFMSSession) },
		"service open": func(m *SharedDiskManager) { m.SetDiskHolder("id", HolderFMSService); m.SetDiskState("id", StateOpen) },
		"closing": func(m *SharedDiskManager) {
			m.SetDiskHolder("id", HolderFMSService)
			m.SetDiskState("id", StateClosing)
		},
	}
	for name, setup := range setups {
		for _, h := range []DiskHolder{HolderNone, HolderFileDO, HolderFMSService, HolderFMSSession, DiskHolder(9), DiskHolder(-1)} {
			m := NewSharedDiskManager("", nil)
			m.RegisterSharedDisk("id", "a.fdd", "Media", false)
			setup(m)
			can := m.CanBeHeldBy("id", h)
			if e := m.SetDiskHolder("id", h); can != (e == nil) {
				t.Errorf("%s/%v: CanBeHeldBy=%v but SetDiskHolder=%v", name, h, can, e)
			}
		}
	}
	m := NewSharedDiskManager("", nil)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if m.CanBeHeldBy("id", HolderFileDO) {
		t.Fatal("FileDO may not hold a shared disk")
	}
	if e := m.SetDiskHolder("id", HolderFileDO); !errors.Is(e, ErrWrongHolder) {
		t.Fatalf("FileDO became holder of a shared disk: %v", e)
	}
	m.SetDiskHolder("id", HolderFMSService)
	m.SetDiskState("id", StateOpen)
	if e := m.SetDiskHolder("id", HolderNone); !errors.Is(e, ErrSharedBusy) {
		t.Fatalf("holder released under an open disk: %v", e)
	}
}
func TestSharedStateAutostartWriteFailureRollsBack(t *testing.T) {
	p, _ := newStateFile(t)
	keys := &memoryKeys{values: map[string][]byte{}}
	m := NewSharedDiskManager(p, keys)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	breakFile(t, p)
	if e := m.SetAutostart("id", true, []byte("secret")); e == nil {
		t.Fatal("write failure ignored")
	}
	d, _ := m.GetSharedDiskState("id")
	if d.Autostart || d.HasStoredKey {
		t.Fatalf("flags survived a failed save: %+v", d)
	}
	if len(keys.values) != 0 {
		t.Fatal("orphaned key left behind")
	}
	// A key that existed before is put back, not destroyed.
	os.Remove(p)
	if e := m.SetAutostart("id", true, []byte("old")); e != nil {
		t.Fatal(e)
	}
	breakFile(t, p)
	if e := m.SetAutostart("id", true, []byte("new")); e == nil {
		t.Fatal("write failure ignored")
	}
	if string(keys.values["id"]) != "old" {
		t.Fatalf("previous key not restored: %q", keys.values["id"])
	}
	if d, _ = m.GetSharedDiskState("id"); !d.Autostart || !d.HasStoredKey {
		t.Fatalf("previous flags lost: %+v", d)
	}
}
func TestSharedStateDisableAutostartKeepsMemoryTruthfulOnWriteFailure(t *testing.T) {
	p, _ := newStateFile(t)
	keys := &memoryKeys{values: map[string][]byte{}}
	m := NewSharedDiskManager(p, keys)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	m.SetAutostart("id", true, []byte("secret"))
	breakFile(t, p)
	if e := m.SetAutostart("id", false, nil); e == nil {
		t.Fatal("write failure ignored")
	}
	d, _ := m.GetSharedDiskState("id")
	if len(keys.values) != 0 || d.HasStoredKey || d.Autostart {
		t.Fatalf("key destroyed but flags claim otherwise: keys=%d %+v", len(keys.values), d)
	}
}
func TestSharedStateAutostartNeedsKeyOnlyForEncryptedDisks(t *testing.T) {
	keys := &memoryKeys{values: map[string][]byte{}}
	m := NewSharedDiskManager("", keys)
	m.RegisterSharedDisk("plain", "a.fdd", "Plain", false)
	if e := m.SetAutostart("plain", true, nil); e != nil {
		t.Fatalf("a disk without encryption needs no key: %v", e)
	}
	if d, _ := m.GetSharedDiskState("plain"); !d.Autostart || d.HasStoredKey {
		t.Fatalf("%+v", d)
	}
	enc := record("enc", "b.fdd", "Enc")
	enc.Encrypted = true
	if e := m.Reconcile(enc); e != nil {
		t.Fatal(e)
	}
	if e := m.SetAutostart("enc", true, nil); !errors.Is(e, ErrAutostartKey) {
		t.Fatalf("encrypted autostart without a key: %v", e)
	}
	if d, _ := m.GetSharedDiskState("enc"); d.Autostart {
		t.Fatal("autostart set without a key")
	}
	if e := m.SetAutostart("enc", true, []byte("pw")); e != nil {
		t.Fatal(e)
	}
	if e := m.SetAutostart("enc", true, nil); e != nil {
		t.Fatalf("stored key not reused: %v", e)
	}
	if e := m.SetAutostart("enc", false, nil); e != nil {
		t.Fatal(e)
	}
	if d, _ := m.GetSharedDiskState("enc"); d.Autostart || d.HasStoredKey || len(keys.values) != 0 {
		t.Fatalf("%+v keys=%d", d, len(keys.values))
	}
	noBackend := NewSharedDiskManager("", nil)
	noBackend.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if e := noBackend.SetAutostart("id", true, []byte("pw")); !errors.Is(e, ErrAutostartKey) {
		t.Fatalf("a key was accepted without a backend: %v", e)
	}
	if d, _ := noBackend.GetSharedDiskState("id"); d.Autostart || d.HasStoredKey {
		t.Fatalf("%+v", d)
	}
}
func TestSharedStateStoreAndDestroyAutostartKey(t *testing.T) {
	keys := &memoryKeys{values: map[string][]byte{}}
	p := filepath.Join(t.TempDir(), "state.json")
	m := NewSharedDiskManager(p, keys)
	if e := m.StoreAutostartKey("missing", []byte("pw")); !errors.Is(e, ErrDiskNotShared) {
		t.Fatal(e)
	}
	if e := m.DestroyAutostartKey("missing"); !errors.Is(e, ErrDiskNotShared) {
		t.Fatal(e)
	}
	if len(keys.values) != 0 {
		t.Fatal("a key was stored for a disk that is not shared")
	}
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if e := m.StoreAutostartKey("id", nil); !errors.Is(e, ErrUsage) {
		t.Fatalf("empty key: %v", e)
	}
	if e := m.StoreAutostartKey("id", []byte("pw")); e != nil {
		t.Fatal(e)
	}
	if d, _ := m.GetSharedDiskState("id"); !d.HasStoredKey || d.Autostart {
		t.Fatalf("store must record the key and leave the mark alone: %+v", d)
	}
	if d, _ := reloaded(t, p).GetSharedDiskState("id"); !d.HasStoredKey {
		t.Fatal("key flag not persisted")
	}
	m.SetAutostart("id", true, nil)
	keys.fail = true
	if e := m.DestroyAutostartKey("id"); e == nil {
		t.Fatal("destroy failure ignored")
	}
	if d, _ := m.GetSharedDiskState("id"); !d.HasStoredKey || !d.Autostart {
		t.Fatalf("flags changed although the key is still there: %+v", d)
	}
	keys.fail = false
	if e := m.DestroyAutostartKey("id"); e != nil {
		t.Fatal(e)
	}
	if d, _ := m.GetSharedDiskState("id"); d.HasStoredKey || d.Autostart || len(keys.values) != 0 {
		t.Fatalf("destroy left inconsistent state: %+v", d)
	}
	breakFile(t, p)
	if e := m.StoreAutostartKey("id", []byte("pw")); e == nil {
		t.Fatal("write failure ignored")
	}
	if d, _ := m.GetSharedDiskState("id"); d.HasStoredKey || len(keys.values) != 0 {
		t.Fatalf("failed save left a key behind: %+v", d)
	}
	nokeys := NewSharedDiskManager("", nil)
	nokeys.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if e := nokeys.StoreAutostartKey("id", []byte("pw")); !errors.Is(e, ErrAutostartKey) {
		t.Fatal(e)
	}
	if e := nokeys.DestroyAutostartKey("id"); !errors.Is(e, ErrAutostartKey) {
		t.Fatal(e)
	}
}
func TestSharedStateReconcileOne(t *testing.T) {
	p, m := newStateFile(t)
	m.RegisterSharedDisk("a", `C:\x\a.fdd`, "Alpha", false)
	m.RegisterSharedDisk("b", `C:\x\b.fdd`, "Beta", false)
	for name, bad := range map[string]SharedDiskState{
		"no id":        record("", `C:\x\c.fdd`, "C"),
		"no path":      record("c", "", "C"),
		"no name":      record("c", `C:\x\c.fdd`, ""),
		"not shared":   {ContainerID: "c", ContainerPath: `C:\x\c.fdd`, RootName: "C"},
		"path of b":    record("c", `C:\X\B.FDD`, "C"),
		"name of b":    record("c", `C:\x\c.fdd`, "BETA"),
		"path from b2": record("a", `C:\x\b.fdd`, "Alpha"),
	} {
		if e := m.Reconcile(bad); e == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if d, ok := m.GetSharedDiskState("a"); !ok || d.ContainerPath != `C:\x\a.fdd` {
		t.Fatal("a refused record damaged the snapshot")
	}
	if _, ok := m.GetSharedDiskState("c"); ok {
		t.Fatal("a refused record was stored")
	}
	moved := record("a", `C:\x\moved.fdd`, "Alpha2")
	moved.State, moved.Holder = StateOpen, HolderFMSService
	if e := m.Reconcile(moved); e != nil {
		t.Fatal(e)
	}
	if _, ok := m.GetSharedDiskStateByPath(`C:\x\a.fdd`); ok {
		t.Fatal("stale path still resolves")
	}
	if d, ok := m.GetSharedDiskStateByPath(`C:\x\moved.fdd`); !ok || d.ContainerID != "a" || d.State != StateOpen || d.Holder != HolderFMSService {
		t.Fatalf("moved record: %+v", d)
	}
	if e := m.RegisterSharedDisk("n", `C:\x\a.fdd`, "Alpha", false); e != nil {
		t.Fatalf("freed path and name still blocked: %v", e)
	}
	again := reloaded(t, p)
	for _, id := range []string{"a", "b", "n"} {
		if _, ok := again.GetSharedDiskState(id); !ok {
			t.Errorf("%s missing after reload", id)
		}
	}
	if d, _ := again.GetSharedDiskState("a"); d.State != StateClosed || d.Holder != HolderNone {
		t.Fatalf("restart kept live state: %+v", d)
	}
	breakFile(t, p)
	if e := m.Reconcile(record("a", `C:\x\third.fdd`, "Third")); e == nil {
		t.Fatal("write failure ignored")
	}
	if d, ok := m.GetSharedDiskStateByPath(`C:\x\moved.fdd`); !ok || d.RootName != "Alpha2" {
		t.Fatal("failed write was not rolled back")
	}
	if _, ok := m.GetSharedDiskStateByPath(`C:\x\third.fdd`); ok {
		t.Fatal("failed write left a path entry")
	}
}
func TestSharedStateReconcileAll(t *testing.T) {
	keys := &memoryKeys{values: map[string][]byte{}}
	p := filepath.Join(t.TempDir(), "state.json")
	m := NewSharedDiskManager(p, keys)
	m.RegisterSharedDisk("stale", `C:\x\stale.fdd`, "Stale", false)
	m.SetAutostart("stale", true, []byte("pw"))
	m.RegisterSharedDisk("kept", `C:\x\kept.fdd`, "Kept", false)
	live := record("kept", `C:\x\kept2.fdd`, "Kept")
	live.State, live.OpenHandles, live.Holder = StateOpen, 4, HolderFMSSession
	list := []SharedDiskState{
		live,
		record("new", `C:\x\new.fdd`, "New"),
		record("dupid", `C:\x\other.fdd`, "Other"),
		record("new", `C:\x\new-again.fdd`, "NewAgain"),
		record("dup-path", `C:\X\NEW.fdd`, "Different"),
		record("dup-name", `C:\x\name.fdd`, "NEW"),
		record("", `C:\x\empty.fdd`, "Empty"),
		{ContainerID: "unshared", ContainerPath: `C:\x\u.fdd`, RootName: "U"},
		record("nopath", "", "NoPath"),
	}
	err := m.ReconcileAll(list)
	if err == nil {
		t.Fatal("skipped records were not reported")
	}
	want := map[string]bool{"kept": true, "new": true, "dupid": true}
	for id := range want {
		if _, ok := m.GetSharedDiskState(id); !ok {
			t.Errorf("%s missing", id)
		}
	}
	for _, id := range []string{"stale", "dup-path", "dup-name", "", "unshared", "nopath"} {
		if _, ok := m.GetSharedDiskState(id); ok {
			t.Errorf("%q should not be in the snapshot", id)
		}
	}
	if d, _ := m.GetSharedDiskState("new"); d.ContainerPath != `C:\x\new.fdd` {
		t.Fatalf("the first of the duplicates must win: %+v", d)
	}
	if d, _ := m.GetSharedDiskState("kept"); d.State != StateOpen || d.OpenHandles != 4 || d.Holder != HolderFMSSession {
		t.Fatalf("worker state not taken: %+v", d)
	}
	if _, ok := m.GetSharedDiskStateByPath(`C:\x\stale.fdd`); ok {
		t.Fatal("stale path still resolves")
	}
	if _, ok := m.GetSharedDiskStateByPath(`C:\x\kept.fdd`); ok {
		t.Fatal("old path of a moved disk still resolves")
	}
	if d, ok := m.GetSharedDiskStateByPath(`C:\x\kept2.fdd`); !ok || d.ContainerID != "kept" {
		t.Fatal("path map not rebuilt")
	}
	if len(keys.values) != 0 {
		t.Fatal("key of a dropped disk was kept")
	}
	again := reloaded(t, p)
	for id := range want {
		if _, ok := again.GetSharedDiskState(id); !ok {
			t.Errorf("%s lost in the round trip", id)
		}
	}
	if _, ok := again.GetSharedDiskState("stale"); ok {
		t.Fatal("dropped disk came back")
	}
	if e := m.RegisterSharedDisk("stale2", `C:\x\stale.fdd`, "Stale", false); e != nil {
		t.Fatalf("dropped disk still blocks its path or name: %v", e)
	}
	if e := m.ReconcileAll(nil); e != nil {
		t.Fatal(e)
	}
	if _, ok := m.GetSharedDiskState("kept"); ok {
		t.Fatal("an empty worker list must empty the snapshot")
	}
	m.ReconcileAll([]SharedDiskState{record("x", `C:\x\x.fdd`, "X")})
	breakFile(t, p)
	if e := m.ReconcileAll([]SharedDiskState{record("y", `C:\x\y.fdd`, "Y")}); e == nil {
		t.Fatal("write failure ignored")
	}
	if _, ok := m.GetSharedDiskState("x"); !ok || m.IsShared("y") {
		t.Fatal("failed write was not rolled back")
	}
	if _, ok := m.GetSharedDiskStateByPath(`C:\x\x.fdd`); !ok {
		t.Fatal("path map not rolled back")
	}
}
func TestSharedStateLoadRejectsInvalidFiles(t *testing.T) {
	good := `{"container_id":"a","container_path":"C:\\x\\a.fdd","shared":true,"root_name":"A"}`
	other := `{"container_id":"b","container_path":"C:\\x\\b.fdd","shared":true,"root_name":"B"}`
	cases := map[string]string{
		"version":        `{"version":2,"disks":{"a":` + good + `}}`,
		"no version":     `{"disks":{"a":` + good + `}}`,
		"id mismatch":    `{"version":1,"disks":{"zzz":` + good + `}}`,
		"empty key":      `{"version":1,"disks":{"":` + good + `}}`,
		"null record":    `{"version":1,"disks":{"a":null}}`,
		"not shared":     `{"version":1,"disks":{"a":{"container_id":"a","container_path":"C:\\x\\a.fdd","root_name":"A"}}}`,
		"no path":        `{"version":1,"disks":{"a":{"container_id":"a","shared":true,"root_name":"A"}}}`,
		"no name":        `{"version":1,"disks":{"a":{"container_id":"a","container_path":"C:\\x\\a.fdd","shared":true}}}`,
		"duplicate path": `{"version":1,"disks":{"a":` + good + `,"b":{"container_id":"b","container_path":"c:\\X\\A.fdd","shared":true,"root_name":"B"}}}`,
		"duplicate name": `{"version":1,"disks":{"a":` + good + `,"b":{"container_id":"b","container_path":"C:\\x\\b.fdd","shared":true,"root_name":"a"}}}`,
		"not json":       `damaged`,
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "state.json")
			m := NewSharedDiskManager(p, nil)
			m.RegisterSharedDisk("keep", "k.fdd", "Keep", false)
			os.WriteFile(p, []byte(content), 0o600)
			if e := m.Load(); e == nil {
				t.Fatal("invalid file accepted")
			}
			if !m.IsShared("keep") {
				t.Fatal("a failed load wiped the snapshot")
			}
		})
	}
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte(`{"version":1,"disks":{"a":`+good+`,"b":`+other+`}}`), 0o600)
	m := NewSharedDiskManager(p, nil)
	if e := m.Load(); e != nil {
		t.Fatal(e)
	}
	if !m.IsSharedByPath(`C:\x\B.fdd`) || !m.IsShared("a") {
		t.Fatal("valid file not loaded")
	}
	os.WriteFile(p, []byte(`{"version":1}`), 0o600)
	if e := m.Load(); e != nil || m.IsShared("a") {
		t.Fatalf("empty file: %v", e)
	}
	if e := NewSharedDiskManager(filepath.Join(t.TempDir(), "none.json"), nil).Load(); e != nil {
		t.Fatalf("missing file is a fresh start: %v", e)
	}
}
func TestPathKeyCanonicalisation(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path spellings")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "Disk.fdd")
	os.WriteFile(file, []byte("x"), 0o600)
	want := pathKey(file)
	for name, spelling := range map[string]string{
		"upper":        strings.ToUpper(file),
		"lower":        strings.ToLower(file),
		"extended":     `\\?\` + file,
		"dot segments": filepath.Join(dir, "sub", "..", ".", "Disk.fdd"),
		"slashes":      strings.ReplaceAll(file, `\`, `/`),
		"doubled":      strings.Replace(file, `\`, `\\`, 1),
	} {
		if got := pathKey(spelling); got != want {
			t.Errorf("%s: %q != %q", name, got, want)
		}
	}
	if pathKey(`\\?\UNC\srv\share\a.fdd`) != pathKey(`\\srv\share\A.FDD`) {
		t.Error("UNC spellings differ")
	}
	if pathKey(`\\?\C:\nowhere\a.fdd`) != pathKey(`C:\NOWHERE\a.fdd`) {
		t.Error("missing file spellings differ")
	}
	if pathKey(`C:\a.fdd`) == pathKey(`C:\b.fdd`) {
		t.Error("different files share a key")
	}
	wd, _ := os.Getwd()
	if pathKey("rel.fdd") != pathKey(filepath.Join(wd, "rel.fdd")) {
		t.Error("a relative path is not resolved against the working directory")
	}
	m := NewSharedDiskManager("", nil)
	if e := m.RegisterSharedDisk("a", file, "A", false); e != nil {
		t.Fatal(e)
	}
	if e := m.RegisterSharedDisk("b", `\\?\`+strings.ToUpper(file), "B", false); !errors.Is(e, ErrAlreadyShared) {
		t.Fatalf("a second spelling of one file bypassed the check: %v", e)
	}
	if !m.IsSharedByPath(strings.ToLower(file)) {
		t.Fatal("lookup by another spelling failed")
	}
}

// A worker that sends a thousand bad records must not make FileDO print a thousand reasons
// (AUD-86-F4): the first eight are named, the rest are a count.
func TestReconcileAllCapsSkipReasons(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	list := make([]SharedDiskState, 500)
	e := m.ReconcileAll(list)
	if e == nil {
		t.Fatal("500 empty records reconciled without a complaint")
	}
	msg := e.Error()
	if got := strings.Count(msg, "worker record "); got != maxSkipReasons {
		t.Fatalf("%d reasons named, want %d: %.200s", got, maxSkipReasons, msg)
	}
	if !strings.Contains(msg, "and 492 more") {
		t.Fatalf("the rest is not counted: %.300s", msg)
	}
	if len(msg) > 2000 {
		t.Fatalf("the error text is %d bytes", len(msg))
	}
}
