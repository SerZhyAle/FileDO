package vdisk

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type memoryKeys struct {
	values map[string][]byte
	fail   bool
}

func (s *memoryKeys) Store(id string, b []byte) error {
	if s.fail {
		return errors.New("key write failed")
	}
	s.values[id] = append([]byte(nil), b...)
	return nil
}
func (s *memoryKeys) Load(id string) ([]byte, error) {
	b, ok := s.values[id]
	if !ok {
		return nil, ErrAutostartKey
	}
	return append([]byte(nil), b...), nil
}
func (s *memoryKeys) Destroy(id string) error {
	if s.fail {
		return errors.New("key removal failed")
	}
	delete(s.values, id)
	return nil
}
func TestSharedStateClashesAndSnapshotIsolation(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	if e := m.RegisterSharedDisk("id", "a.fdd", "Media", false); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][3]string{{"id", "b.fdd", "Other"}, {"other", "a.fdd", "Other"}, {"other", "b.fdd", "MEDIA"}} {
		if e := m.RegisterSharedDisk(args[0], args[1], args[2], false); e == nil {
			t.Fatal("duplicate identity, path or root accepted")
		}
	}
	d, ok := m.GetSharedDiskStateByPath("a.fdd")
	if !ok {
		t.Fatal("path lookup failed")
	}
	d.RootName = "changed"
	again, _ := m.GetSharedDiskState("id")
	if again.RootName != "Media" {
		t.Fatal("returned pointer aliases live state")
	}
}
func TestSharedStateSingleHolder(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if e := m.SetDiskHolder("id", HolderFMSService); e != nil {
		t.Fatal(e)
	}
	for _, h := range []DiskHolder{HolderFileDO, HolderFMSSession} {
		if e := m.SetDiskHolder("id", h); !errors.Is(e, ErrWrongHolder) {
			t.Fatalf("second holder %v accepted: %v", h, e)
		}
	}
	if m.CanBeHeldBy("id", HolderFMSSession) {
		t.Fatal("a second FMS holder bypassed ownership")
	}
	if e := m.SetDiskHolder("id", HolderNone); e != nil {
		t.Fatal(e)
	}
}
func TestSharedStateConcurrentHandles(t *testing.T) {
	m := NewSharedDiskManager("", nil)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if e := m.IncrementOpenHandles("id"); e == nil {
		t.Fatal("closed disk accepted an open")
	}
	m.SetDiskState("id", StateOpen)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e := m.IncrementOpenHandles("id"); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	if n, _ := m.GetOpenHandles("id"); n != 100 {
		t.Fatalf("lost handle increments: %d", n)
	}
	if e := m.UnregisterSharedDisk("id"); !errors.Is(e, ErrSharedBusy) {
		t.Fatal("open disk was unshared")
	}
	for i := 0; i < 100; i++ {
		m.DecrementOpenHandles("id")
	}
	if e := m.DecrementOpenHandles("id"); e == nil {
		t.Fatal("underflow hidden")
	}
}
func TestSharedStatePersistenceDoesNotInventLiveMounts(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	m := NewSharedDiskManager(p, nil)
	m.RegisterSharedDisk("id", "a.fdd", "Media", true)
	m.SetDiskHolder("id", HolderFMSService)
	m.SetDiskState("id", StateOpen)
	m.IncrementOpenHandles("id")
	loaded := NewSharedDiskManager(p, nil)
	if e := loaded.Load(); e != nil {
		t.Fatal(e)
	}
	d, ok := loaded.GetSharedDiskState("id")
	if !ok || !d.ReadOnly || d.State != StateClosed || d.Holder != HolderNone || d.OpenHandles != 0 {
		t.Fatalf("unsafe restart state: %+v", d)
	}
	if e := loaded.UnregisterSharedDisk("id"); e != nil {
		t.Fatal(e)
	}
	again := NewSharedDiskManager(p, nil)
	again.Load()
	if again.IsShared("id") {
		t.Fatal("removal not persisted")
	}
	os.WriteFile(p, []byte("damaged"), 0600)
	if e := again.Load(); e == nil {
		t.Fatal("corrupt state silently discarded")
	}
}
func TestSharedStateKeyLifecycle(t *testing.T) {
	keys := &memoryKeys{values: map[string][]byte{}}
	m := NewSharedDiskManager("", keys)
	m.RegisterSharedDisk("id", "a.fdd", "Media", false)
	if e := m.SetAutostart("id", true, []byte("credential")); e != nil {
		t.Fatal(e)
	}
	keys.fail = true
	if e := m.SetAutostart("id", false, nil); e == nil {
		t.Fatal("key deletion failure ignored")
	}
	d, _ := m.GetSharedDiskState("id")
	if !d.HasStoredKey {
		t.Fatal("failed deletion claimed key gone")
	}
	keys.fail = false
	if e := m.SetAutostart("id", false, nil); e != nil {
		t.Fatal(e)
	}
	if len(keys.values) != 0 {
		t.Fatal("credential retained")
	}
	m.SetAutostart("id", true, []byte("credential"))
	if e := m.UnregisterSharedDisk("id"); e != nil {
		t.Fatal(e)
	}
	if len(keys.values) != 0 {
		t.Fatal("unshare retained key")
	}
}
func TestSharedStateFailedPersistenceRollsBack(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "parent", "state.json")
	os.WriteFile(filepath.Join(dir, "parent"), []byte("not a directory"), 0600)
	m := NewSharedDiskManager(p, nil)
	if e := m.RegisterSharedDisk("id", "a.fdd", "Media", false); e == nil {
		t.Fatal("write unexpectedly succeeded")
	}
	if m.IsShared("id") {
		t.Fatal("failed registration became visible")
	}
}
