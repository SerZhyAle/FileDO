package vdisk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func fileSHA(t testing.TB, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// T2.7: plain and fast produce a header with the requested geometry, the
// 1.0 writer layout of FDD-FORMAT section 5.3, and a file of the size it
// implies; a container is never created over an existing file.
func TestVD_Create(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		profile     Profile
		size        int64
		sec, clu    uint8
		wantFileLen int64
	}{
		{ProfilePlain, 16 << 20, 12, 16, 20480},
		{ProfilePlain, 20 << 30, 0, 0, 8192 + 2*163840 + 4096},
		{ProfileFast, 1 << 20, 9, 16, (1 << 20) + (1 << 20) + 4096},
		{ProfileFast, 3 << 20, 12, 20, (1 << 20) + (3 << 20) + 4096},
	} {
		path := filepath.Join(dir, c.profile.String()+strconv.FormatInt(c.size, 10)+".fdd")
		ct, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: c.size, Profile: c.profile, SectorShift: c.sec, ClusterShift: c.clu, FriendlyName: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if err := ct.Close(); err != nil {
			t.Fatal(err)
		}
		info, err := Inspect(path)
		if err != nil {
			t.Fatal(err)
		}
		wantSec, wantClu := int64(1)<<DefaultSectorShift, int64(1)<<DefaultClusterShift
		if c.sec != 0 {
			wantSec = 1 << c.sec
		}
		if c.clu != 0 {
			wantClu = 1 << c.clu
		}
		if info.Profile != c.profile || info.LogicalSize != c.size || info.SectorSize != wantSec || info.ClusterSize != wantClu ||
			!info.Clean || !info.Obfuscated || info.FileSize != c.wantFileLen || info.PhysicalSize != c.wantFileLen ||
			info.FriendlyName != "test" || info.Protection() != "obfuscated" || info.VersionMajor != 1 {
			t.Errorf("%s %d: %+v", c.profile, c.size, info)
		}
		if _, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: c.size}); !errors.Is(err, ErrUsage) {
			t.Errorf("created over an existing file: %v", err)
		}
	}
	for _, o := range []CreateOptions{
		{LogicalSize: 0},
		{LogicalSize: 4096 + 512},
		{LogicalSize: 1 << 20, ClusterShift: 15},
		{LogicalSize: 1 << 20, SectorShift: 10},
		{LogicalSize: 1 << 20, FriendlyName: strings.Repeat("x", 65)},
	} {
		o.Path = filepath.Join(dir, "bad.fdd")
		if _, err := Create(context.Background(), o); !errors.Is(err, ErrUsage) {
			t.Errorf("%+v: %v, want usage", o, err)
		}
	}
	// A vault with no credential is refused as usage, with the reason
	// (SP-0004 S5; TestVD_Vault_Create has the rest of the credential path).
	if _, err := Create(context.Background(), CreateOptions{Path: filepath.Join(dir, "bad.fdd"), LogicalSize: 1 << 20, Profile: ProfileVault}); !errors.Is(err, ErrVaultNeedsCredential) {
		t.Errorf("vault without a credential: %v", err)
	}
	for _, o := range []CreateOptions{
		{LogicalSize: 1 << 20, Profile: ProfileSealed},
		{LogicalSize: 1 << 20, Digests: true},
	} {
		o.Path = filepath.Join(dir, "bad.fdd")
		if _, err := Create(context.Background(), o); !errors.Is(err, ErrUnsupported) {
			t.Errorf("%+v: %v, want unsupported", o, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.fdd")); !os.IsNotExist(err) {
		t.Error("a refused create left a file")
	}
}

// T2.7: a stopped creation of a fast container removes the partial file.
func TestVD_Create_Stopped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stop.fdd")
	ctx, cancel := context.WithCancel(context.Background())
	stopAt := &stopAfter{n: 3, cancel: cancel}
	_, err := Create(ctx, CreateOptions{Path: path, LogicalSize: 8 << 20, Profile: ProfileFast, ClusterShift: 16, Progress: stopAt})
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("%v, want stopped", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("a stopped create left its file")
	}
}

type stopAfter struct {
	n      int64
	cancel func()
	calls  int64
}

func (s *stopAfter) Progress(done, total int64) {
	s.calls++
	if s.calls == s.n {
		s.cancel()
	}
}

// T2.8: a clean Close sets the marker and last_good_save; data written reads
// back after a reopen, through a real file.
func TestVD_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rt.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 8 << 20, ClusterShift: 16, SectorShift: 9})
	if err != nil {
		t.Fatal(err)
	}
	ref := newRef(8<<20, 1<<16)
	for i, w := range []struct {
		off int64
		n   int
	}{{0, 512}, {1, 1}, {65535, 2}, {100000, 300000}, {8<<20 - 700, 700}, {4 << 20, 1 << 16}} {
		p := pattern(byte(i+1), w.n)
		if n, err := c.WriteAt(p, w.off); err != nil || n != w.n {
			t.Fatalf("write %d: %d %v", i, n, err)
		}
		ref.writeAt(p, w.off)
	}
	if info := c.Info(); info.Clean {
		t.Fatal("the marker is still set during a write session")
	}
	if !bytes.Equal(readAll(t, c), ref.image()) {
		t.Fatal("the open session reads back differently")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Clean || info.LastGoodSave.IsZero() || info.PhysicalSize != info.FileSize || info.Backup != "current" {
		t.Fatalf("after Close: %+v", info)
	}
	c, err = Open(context.Background(), path, nil, OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if !bytes.Equal(readAll(t, c), ref.image()) {
		t.Fatal("the reopened container reads back differently")
	}
	if _, err := c.WriteAt([]byte{1}, 0); !errors.Is(err, ErrUsage) {
		t.Fatalf("a write to a read-only open: %v", err)
	}
	// Reads at and past the end follow io.ReaderAt.
	b := make([]byte, 10)
	if n, err := c.ReadAt(b, 8<<20-4); n != 4 || err == nil {
		t.Fatalf("a read across the end: %d %v", n, err)
	}
}

// T2.8: offsets past the volume fail and extend nothing.
func TestVD_RoundTrip_Bounds(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16})
	before := mb.snapshot()
	for _, w := range [][2]int64{{1 << 20, 1}, {1<<20 - 1, 2}, {-1, 1}, {2 << 20, 0}} {
		if _, err := c.WriteAt(make([]byte, w[1]), w[0]); !errors.Is(err, ErrUsage) {
			t.Errorf("write %d at %d: %v", w[1], w[0], err)
		}
	}
	if n, err := c.WriteAt(nil, 1<<20); n != 0 || err != nil {
		t.Errorf("an empty write at the end: %d %v", n, err)
	}
	if !bytes.Equal(before, mb.snapshot()) {
		t.Error("a refused write changed the file")
	}
	// A write of zeros to an unallocated cluster allocates nothing.
	if _, err := c.WriteAt(make([]byte, 1<<16), 1<<16); err != nil {
		t.Fatal(err)
	}
	if c.allocated != 0 {
		t.Errorf("zeros allocated %d clusters", c.allocated)
	}
}

// T2.5: a 20 GiB plain container occupies under 8 MiB before anything is
// written, and a written cluster costs one cluster, not the volume.
func TestVD_Sparse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 20 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Size() >= 8<<20 {
		t.Fatalf("an empty 20 GiB container is %d bytes", fi.Size())
	}
	if _, err := c.WriteAt(pattern(1, 4096), 10<<30); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Size() > 3<<20 {
		t.Fatalf("one written cluster made the file %d bytes", fi.Size())
	}
}

// T2.6: every commit increments the generation and alternates the copies, and
// a commit interrupted at any step resolves to exactly one generation.
func TestVD_Map(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 4 << 20, ClusterShift: 16})
	gen, active := c.hdr.MapGeneration, c.hdr.MapActive
	for i := 0; i < 5; i++ {
		if _, err := c.WriteAt(pattern(byte(i), 100), int64(i)<<16); err != nil {
			t.Fatal(err)
		}
		if err := c.Flush(); err != nil {
			t.Fatal(err)
		}
		if c.hdr.MapGeneration != gen+1 || c.hdr.MapActive == active {
			t.Fatalf("commit %d: generation %d active %d, was %d %d", i, c.hdr.MapGeneration, c.hdr.MapActive, gen, active)
		}
		gen, active = c.hdr.MapGeneration, c.hdr.MapActive
		// Both copies are kept: the inactive one still holds the previous map.
		h := c.hdr
		img := mb.snapshot()
		m, _ := mapSize(h.ClusterCount)
		prev := img[h.mapCopyOffset(1-h.MapActive):][:m]
		if n := binary.LittleEndian.Uint64(prev[i*8:]); n != sentinel {
			t.Fatalf("the inactive copy already holds cluster %d", i)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

// T2.9: grow increases the logical size, leaves existing clusters untouched
// byte for byte, and works for every map placement - same stride, a wider
// stride (from either active copy), and relocation into the data region.
func TestVD_Grow(t *testing.T) {
	for _, g := range []struct {
		name     string
		profile  Profile
		from, to int64
		clu      uint8
		flip     bool
	}{
		{"same stride", ProfilePlain, 4 << 20, 5 << 20, 16, false},
		{"wider stride A", ProfilePlain, 4 << 20, 40 << 20, 16, false},
		{"wider stride B", ProfilePlain, 4 << 20, 40 << 20, 16, true},
		{"relocate", ProfilePlain, 4 << 20, 5 << 30, 16, false},
		{"fast", ProfileFast, 1 << 20, 3<<20 + 4096, 16, false},
		{"ram", ProfileRAM, 1 << 20, 2 << 20, 16, false},
	} {
		t.Run(g.name, func(t *testing.T) {
			c, mb := newMemContainer(t, CreateOptions{LogicalSize: g.from, ClusterShift: g.clu, Profile: g.profile})
			ref := newRef(g.from, 1<<g.clu)
			w := pattern(3, 150000)
			if _, err := c.WriteAt(w, 70000); err != nil {
				t.Fatal(err)
			}
			ref.writeAt(w, 70000)
			if err := c.Flush(); err != nil {
				t.Fatal(err)
			}
			if g.flip != (c.hdr.MapActive == 1) {
				if _, err := c.WriteAt(pattern(4, 10), 3<<16); err != nil {
					t.Fatal(err)
				}
				ref.writeAt(pattern(4, 10), 3<<16)
				if err := c.Flush(); err != nil {
					t.Fatal(err)
				}
			}
			before := map[int64][]byte{}
			cs := int64(c.hdr.clusterSize())
			img := mb.snapshot()
			for cl, e := range c.entries {
				if e != sentinel {
					before[int64(cl)] = append([]byte(nil), img[int64(c.hdr.DataOffset)+int64(e)*cs:][:cs]...)
				}
			}
			if err := c.Grow(context.Background(), g.to); err != nil {
				t.Fatal(err)
			}
			if relocated := c.hdr.MapOffset >= c.hdr.DataOffset; relocated != (g.name == "relocate") {
				t.Fatalf("map at %d, data at %d", c.hdr.MapOffset, c.hdr.DataOffset)
			}
			ref.grow(g.to)
			img = mb.snapshot()
			for cl, ct := range before {
				e := c.entries[cl]
				if !bytes.Equal(img[int64(c.hdr.DataOffset)+int64(e)*cs:][:cs], ct) {
					t.Fatalf("grow changed the ciphertext of cluster %d", cl)
				}
			}
			w2 := pattern(9, 5000)
			if _, err := c.WriteAt(w2, g.to-5000); err != nil {
				t.Fatal(err)
			}
			ref.writeAt(w2, g.to-5000)
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			rc := mustOpenMem(t, mb, OpenRead)
			if rc.Info().LogicalSize != g.to {
				t.Fatalf("size %d", rc.Info().LogicalSize)
			}
			sameVolume(t, rc, ref)
			if _, err := rc.Verify(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
		})
	}
	c, _ := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16})
	if err := c.Grow(context.Background(), 1<<20); !errors.Is(err, ErrUsage) {
		t.Errorf("grow to the same size: %v", err)
	}
}

// T2.10: compact releases unused space, every remaining cluster reads back
// identically, and no key is used - the moves are byte copies.
func TestVD_Compact(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 4 << 20, ClusterShift: 16})
	ref := newRef(4<<20, 1<<16)
	for i := 0; i < 20; i++ {
		p := pattern(byte(i+1), 1<<16)
		if _, err := c.WriteAt(p, int64(i)<<16); err != nil {
			t.Fatal(err)
		}
		ref.writeAt(p, int64(i)<<16)
	}
	// Zero some clusters by releasing them the way a later trim would: drop
	// them from the map, which leaves holes in the file.
	for _, cl := range []int{0, 3, 4, 10} {
		c.used.clear(c.entries[cl])
		c.entries[cl] = sentinel
		c.allocated--
		ref.writeAt(make([]byte, 1<<16), int64(cl)<<16)
	}
	c.pending = true
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	lenBefore := len(mb.snapshot())
	c.cipher = nil // any use of the key now panics: compaction must not need it
	if err := c.Compact(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := len(mb.snapshot()); got != lenBefore-4<<16 {
		t.Fatalf("the file is %d bytes, want %d", got, lenBefore-4<<16)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	rc := mustOpenMem(t, mb, OpenRead)
	if !bytes.Equal(readAll(t, rc), ref.image()) {
		t.Fatal("compaction changed the volume")
	}
	if !rc.Info().Clean {
		t.Fatal("not clean after close")
	}
	// Compacting a compact container changes nothing.
	wc := mustOpenMem(t, mb, OpenWrite)
	before := mb.snapshot()
	if err := wc.Compact(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	wc.Close()
	if !bytes.Equal(before, mb.snapshot()) {
		t.Fatal("compacting a compact container wrote to it")
	}
}

// T2.11: a container without digests says so rather than passing; a changed
// map byte is found and named; a read-only verify writes nothing.
func TestVD_Verify(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 2 << 20, ClusterShift: 16})
	if _, err := c.WriteAt(pattern(1, 300000), 0); err != nil {
		t.Fatal(err)
	}
	c.Close()
	before := mb.snapshot()
	r, err := mustOpenMem(t, mb, OpenRead).Verify(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.DataVerified || r.ClustersRead != 5 || r.Backup != "current" || !strings.Contains(strings.Join(r.Notes, "\n"), "not verified") {
		t.Fatalf("report: %+v", r)
	}
	if !bytes.Equal(before, mb.snapshot()) {
		t.Fatal("verify wrote to the container")
	}
	// Backup damaged: the container opens, verify names the problem.
	img := append([]byte(nil), before...)
	img[len(img)-100] ^= 1
	r, err = mustOpenMem(t, &memBacking{data: img}, OpenRead).Verify(context.Background(), nil)
	if !errors.Is(err, ErrDamaged) || r.Backup != "unreadable" {
		t.Fatalf("a damaged backup: %v %+v", err, r)
	}
}

// T2.12: the exported image is byte-identical to the logical volume; the VHD
// form adds a valid fixed-disk footer; an existing destination is refused and
// a stopped export leaves nothing.
func TestVD_ExportRaw(t *testing.T) {
	dir := t.TempDir()
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 3 << 20, ClusterShift: 16})
	ref := newRef(3<<20, 1<<16)
	for _, off := range []int64{0, 777777, 3<<20 - 4096} {
		p := pattern(byte(off), 4096)
		c.WriteAt(p, off)
		ref.writeAt(p, off)
	}
	c.Close()
	rc := mustOpenMem(t, mb, OpenRead)
	img := filepath.Join(dir, "vol.img")
	if err := rc.ExportRaw(context.Background(), img, RawFormImage, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(img); !bytes.Equal(got, ref.image()) {
		t.Fatal("the raw image differs from the volume")
	}
	if err := rc.ExportRaw(context.Background(), img, RawFormImage, nil); !errors.Is(err, ErrUsage) {
		t.Fatalf("an existing destination: %v", err)
	}
	if err := rc.ExportRaw(context.Background(), filepath.Join(dir, "no", "x.img"), RawFormImage, nil); !errors.Is(err, ErrUsage) {
		t.Fatalf("a missing folder: %v", err)
	}
	vhd := filepath.Join(dir, "vol.vhd")
	if err := rc.ExportRaw(context.Background(), vhd, RawFormVHD, nil); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(vhd)
	if len(got) != 3<<20+512 || !bytes.Equal(got[:3<<20], ref.image()) {
		t.Fatal("the VHD body differs")
	}
	f := got[3<<20:]
	var sum uint32
	for i, x := range f {
		if i < 64 || i >= 68 {
			sum += uint32(x)
		}
	}
	if string(f[:8]) != "conectix" || binary.BigEndian.Uint32(f[60:]) != 2 || binary.BigEndian.Uint64(f[48:]) != 3<<20 || binary.BigEndian.Uint32(f[64:]) != ^sum {
		t.Fatalf("footer: %x", f[:96])
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopPath := filepath.Join(dir, "stop.img")
	if err := rc.ExportRaw(ctx, stopPath, RawFormImage, &stopAfter{n: 2, cancel: cancel}); !errors.Is(err, ErrStopped) {
		t.Fatalf("stopped export: %v", err)
	}
	if _, err := os.Stat(stopPath); !os.IsNotExist(err) {
		t.Fatal("a stopped export left its file")
	}
	if _, err := rc.ExportTree(context.Background(), dir, Selector{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ExportTree: %v", err)
	}
}

// A read-only open never writes, and neither does a failed open; the backup
// that carried an open is repaired only by the next writer.
func TestVD_ReadOnlyWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ro.fdd")
	c, _ := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 2 << 20, ClusterShift: 16})
	c.WriteAt(pattern(2, 100000), 5000)
	c.Close()
	raw, _ := os.ReadFile(path)
	raw[sealedOffset+7] ^= 0x40
	os.WriteFile(path, raw, 0o644)
	sum := fileSHA(t, path)
	info, err := Inspect(path)
	if err != nil || !info.FromBackup {
		t.Fatalf("inspect: %v %+v", err, info)
	}
	rc, err := Open(context.Background(), path, []byte("ignored"), OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	readAll(t, rc)
	rc.Verify(context.Background(), nil)
	rc.Close()
	if fileSHA(t, path) != sum {
		t.Fatal("a read-only open changed the file")
	}
	wc, err := Open(context.Background(), path, nil, OpenWrite)
	if err != nil {
		t.Fatal(err)
	}
	wc.Close()
	if info, _ := Inspect(path); info.FromBackup || info.Backup != "current" {
		t.Fatalf("the writer did not repair the primary: %+v", info)
	}
}

// ErrBusy: a second writer, or a reader while a writer holds the container.
func TestVD_Busy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 1 << 20, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), path, nil, OpenWrite); !errors.Is(err, ErrBusy) {
		t.Fatalf("a second writer: %v", err)
	}
	if _, err := Open(context.Background(), path, nil, OpenRead); !errors.Is(err, ErrBusy) {
		t.Fatalf("a reader beside a writer: %v", err)
	}
	if _, err := Inspect(path); err != nil {
		t.Fatalf("inspect beside a writer: %v", err)
	}
	c.Close()
	r1, err := Open(context.Background(), path, nil, OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Open(context.Background(), path, nil, OpenRead)
	if err != nil {
		t.Fatalf("two readers: %v", err)
	}
	r1.Close()
	r2.Close()
}

// The ram profile: writes stay in memory until a save, a save follows the
// three steps of FDD-FORMAT section 10.4, and last_good_save is the time the
// save began.
func TestVD_RAMSave(t *testing.T) {
	_, clock := deterministic(t, "ram")
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileRAM})
	img := mb.snapshot()
	p := pattern(5, 70000)
	if _, err := c.WriteAt(p, 1000); err != nil {
		t.Fatal(err)
	}
	h := mustOpenMem(t, &memBacking{data: mb.snapshot()}, OpenRead).hdr
	if h.Clean != 0 {
		t.Fatal("clean still set after a write")
	}
	cs := int64(1 << 16)
	if !bytes.Equal(mb.snapshot()[h.DataOffset:][:2*cs], img[h.DataOffset:][:2*cs]) {
		t.Fatal("a ram write reached the file before a save")
	}
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	saveStart := clock.reads[len(clock.reads)-1]
	h = mustOpenMem(t, &memBacking{data: mb.snapshot()}, OpenRead).hdr
	if h.SaveInProgress != 0 || h.SaveStarted != 0 || h.LastGoodSave != uint64(saveStart.UnixNano()) {
		t.Fatalf("after the save: in progress %d started %d last good %d, want %d", h.SaveInProgress, h.SaveStarted, h.LastGoodSave, saveStart.UnixNano())
	}
	c.Close()
	got := readAll(t, mustOpenMem(t, mb, OpenRead))
	if !bytes.Equal(got[1000:1000+len(p)], p) {
		t.Fatal("the saved ram volume reads back differently")
	}
}

// TestVD_Escape is G1 in one function: a container is written, closed, and
// its volume leaves it with nothing but this package - byte for byte.
func TestVD_Escape(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "escape.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 6 << 20, ClusterShift: 16, FriendlyName: "escape"})
	if err != nil {
		t.Fatal(err)
	}
	ref := newRef(6<<20, 1<<16)
	for i := 0; i < 40; i++ {
		off := int64(i) * 150001 % (6<<20 - 9000)
		p := pattern(byte(i*13), 9000-i*7)
		c.WriteAt(p, off)
		ref.writeAt(p, off)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	rc, err := Open(context.Background(), path, nil, OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	out := filepath.Join(dir, "escape.img")
	if err := rc.ExportRaw(context.Background(), out, RawFormImage, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); !bytes.Equal(got, ref.image()) {
		t.Fatal("the escape route returned a different volume")
	}
}

// TestVD_Escape_Imports holds principle 1 as a property of the dependency
// graph: the extraction files import no network, no process start, no
// transport.
func TestVD_Escape_Imports(t *testing.T) {
	for _, f := range []string{"export_raw.go", "export_tree.go", "container.go", "backing_file.go", "clustermap.go", "crypto_xts.go", "header_seal.go", "slots.go", "format.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if p == "net" || strings.HasPrefix(p, "net/") || p == "os/exec" || p == "syscall" || strings.Contains(p, "iscsi") || strings.HasPrefix(p, "filedo/cmd") {
				t.Errorf("%s imports %s", f, p)
			}
		}
	}
}

// A changed byte anywhere in a container is either harmless or named - never
// a panic, never a class other than damage for a file whose header opened.
func TestVD_Robustness_ByteFlips(t *testing.T) {
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16})
	c.WriteAt(pattern(1, 200000), 3000)
	c.Close()
	base := mb.snapshot()
	for i := 0; i < 400; i++ {
		img := append([]byte(nil), base...)
		at := (i*7919 + i*i*31) % len(img)
		img[at] ^= byte(1 << (i % 8))
		rc, err := openMem(&memBacking{data: img}, OpenRead)
		if err != nil {
			if cls := ExitClass(err); cls != ExitDamaged {
				t.Fatalf("byte %d: class %d (%v)", at, cls, err)
			}
			continue
		}
		readAll(t, rc)
		rc.Verify(context.Background(), nil)
	}
	for _, n := range []int{0, 100, minFileSize, 30000, len(base) - 1} {
		if _, err := openMem(&memBacking{data: base[:n]}, OpenRead); err == nil && n < len(base) {
			t.Fatalf("a file cut to %d bytes opened", n)
		}
	}
}
