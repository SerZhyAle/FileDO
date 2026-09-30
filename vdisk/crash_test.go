package vdisk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// Abrupt failure (P2 section 6.3). A failBacking dies at a chosen write, flush
// or truncate; the image it leaves - both with every write that returned
// (the file system kept them) and with only what was flushed (it kept
// nothing else) - must open, and every logical sector must read as it was
// before the interrupted operation or as that operation left it. Never a
// blend inside a sector, never damage, never a silent third state. A writer
// then opens the image, repairs what section 6.2 says it repairs, and the
// result verifies.

type crashOp func(ctx context.Context, c *Container) error

// crashBase builds a closed container in memory and returns its image and the
// reference it holds.
func crashBase(t *testing.T, o CreateOptions, fill func(c *Container, ref *refVolume)) ([]byte, *refVolume) {
	t.Helper()
	c, mb := newMemContainer(t, o)
	ref := newRef(o.LogicalSize, 1<<o.ClusterShift)
	if fill != nil {
		fill(c, ref)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return mb.snapshot(), ref
}

func writeRef(t *testing.T, c *Container, ref *refVolume, p []byte, off int64) {
	t.Helper()
	if _, err := c.WriteAt(p, off); err != nil {
		t.Fatal(err)
	}
	ref.writeAt(p, off)
}

// checkCrashImage is the must-hold of every crash case.
func checkCrashImage(t *testing.T, what string, img []byte, before, after *refVolume) *Container {
	t.Helper()
	c, err := openMem(&memBacking{data: append([]byte(nil), img...)}, OpenRead)
	if err != nil {
		t.Fatalf("%s: the image does not open: %v", what, err)
	}
	size := c.Info().LogicalSize
	if size != before.size && size != after.size {
		t.Fatalf("%s: volume size %d is neither %d nor %d", what, size, before.size, after.size)
	}
	ss := c.Info().SectorSize
	got := make([]byte, size)
	if _, err := c.ReadAt(got, 0); err != nil {
		t.Fatalf("%s: read: %v", what, err)
	}
	b, a := make([]byte, ss), make([]byte, ss)
	for off := int64(0); off < size; off += ss {
		clear(b)
		if off < before.size {
			before.readAt(b, off)
		}
		after.readAt(a, off)
		if s := got[off : off+ss]; !bytes.Equal(s, b) && !bytes.Equal(s, a) {
			t.Fatalf("%s: the sector at %d is neither the old nor the new one", what, off)
		}
	}
	// The next writer repairs the pair, and what it leaves verifies and reads
	// the same.
	wb := &memBacking{data: append([]byte(nil), img...)}
	wc, err := openMem(wb, OpenWrite)
	if err != nil {
		t.Fatalf("%s: a writer cannot open the image: %v", what, err)
	}
	if err := wc.Close(); err != nil {
		t.Fatalf("%s: close after repair: %v", what, err)
	}
	rc := mustOpenMem(t, wb, OpenRead)
	if r, err := rc.Verify(context.Background(), nil); err != nil || r.Backup != "current" {
		t.Fatalf("%s: after the writer's repair: %v, backup %q", what, err, r.Backup)
	}
	if !bytes.Equal(readAll(t, rc), got) {
		t.Fatalf("%s: the writer's repair changed the volume", what)
	}
	return c
}

// crashSweep runs op once to count its writes and flushes, then once per
// write (whole, and torn in half) and once per flush with the failure there.
func crashSweep(t *testing.T, base []byte, before, after *refVolume, op crashOp) {
	t.Helper()
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	c := mustOpenMem(t, fb, OpenWrite)
	if err := op(context.Background(), c); err != nil {
		t.Fatalf("the operation fails without a crash: %v", err)
	}
	checkCrashImage(t, "no crash", fb.inner.snapshot(), after, after)
	writes, syncs := fb.writes, fb.syncs
	if writes == 0 || syncs == 0 {
		t.Fatal("the operation wrote nothing")
	}
	type point struct {
		write, sync int
		torn, trunc bool
	}
	var points []point
	for n := 1; n <= writes; n++ {
		points = append(points, point{write: n}, point{write: n, torn: true})
	}
	for n := 1; n <= syncs; n++ {
		points = append(points, point{sync: n})
	}
	points = append(points, point{trunc: true})
	for _, p := range points {
		fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
		fb.failWrite, fb.failSync, fb.torn, fb.failTruncate = p.write, p.sync, p.torn, p.trunc
		c, err := openMem(fb, OpenWrite)
		if err != nil {
			t.Fatalf("open: %v", err)
		}
		err = op(context.Background(), c)
		if fb.dead && err == nil {
			t.Fatalf("%+v: the operation reported success after the store failed", p)
		}
		if fb.dead && !errors.Is(err, ErrIO) {
			t.Fatalf("%+v: a store failure surfaced as %v, not I/O", p, err)
		}
		what := fmt.Sprintf("crash %+v", p)
		checkCrashImage(t, what+" (all writes kept)", fb.inner.snapshot(), before, after)
		checkCrashImage(t, what+" (flushed writes kept)", fb.durable, before, after)
	}
}

func baseOptions() CreateOptions {
	return CreateOptions{LogicalSize: 2 << 20, ClusterShift: 16, SectorShift: 12}
}

func fillSome(c *Container, ref *refVolume) {
	for i, off := range []int64{0, 5 << 16, 9<<16 + 1234} {
		p := pattern(byte(i+1), 70000)
		c.WriteAt(p, off)
		ref.writeAt(p, off)
	}
}

// FailAfterNWrites, TornSectorWrite, FailDuringMapCommit, FailOnSync as a
// sweep over a write-and-flush: new clusters (the extending order), an
// in-place overwrite, and the map commit.
func TestVDCrash_WriteFlush(t *testing.T) {
	base, before := crashBase(t, baseOptions(), fillSome)
	after := before.clone()
	p1, p2 := pattern(40, 3<<16), pattern(41, 5000)
	after.writeAt(p1, 20<<16)
	after.writeAt(p2, 100)
	crashSweep(t, base, before, after, func(ctx context.Context, c *Container) error {
		if _, err := c.WriteAt(p1, 20<<16); err != nil {
			return err
		}
		if _, err := c.WriteAt(p2, 100); err != nil {
			return err
		}
		return c.Flush()
	})
}

// The first allocation of an empty container: its backup sits in the
// metadata zone and moves to the new end first (the extending order).
func TestVDCrash_FirstAllocation(t *testing.T) {
	base, before := crashBase(t, baseOptions(), nil)
	after := before.clone()
	p := pattern(50, 1<<16+300)
	after.writeAt(p, 7<<16)
	crashSweep(t, base, before, after, func(ctx context.Context, c *Container) error {
		if _, err := c.WriteAt(p, 7<<16); err != nil {
			return err
		}
		return c.Close()
	})
}

// FailDuringGrow as a sweep, for each map placement.
func TestVDCrash_Grow(t *testing.T) {
	for _, g := range []struct {
		name string
		to   int64
		prof Profile
	}{{"same stride", 2<<20 + 4096, ProfilePlain}, {"wider stride", 64 << 20, ProfilePlain}, {"relocate", 5 << 30, ProfilePlain}, {"fast", 3 << 20, ProfileFast}} {
		t.Run(g.name, func(t *testing.T) {
			o := baseOptions()
			o.Profile = g.prof
			base, before := crashBase(t, o, fillSome)
			after := before.clone()
			after.grow(g.to)
			if g.to > 1<<30 {
				// A 5 GiB volume is compared through its header and a short read,
				// not sector by sector: the sweep checks the geometry and the old
				// clusters.
				checkGrowCrashes(t, base, g.to)
				return
			}
			crashSweep(t, base, before, after, func(ctx context.Context, c *Container) error {
				return c.Grow(ctx, g.to)
			})
		})
	}
}

// checkGrowCrashes is the relocation sweep for a volume too large to compare
// sector by sector: the size is old or new, the old clusters read as before,
// and a writer's repair verifies.
func checkGrowCrashes(t *testing.T, base []byte, to int64) {
	want := readAll(t, mustOpenMem(t, &memBacking{data: base}, OpenRead))
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	c := mustOpenMem(t, fb, OpenWrite)
	if err := c.Grow(context.Background(), to); err != nil {
		t.Fatal(err)
	}
	for n := 1; n <= fb.writes; n++ {
		for _, torn := range []bool{false, true} {
			fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
			fb.failWrite, fb.torn = n, torn
			c := mustOpenMem(t, fb, OpenWrite)
			if err := c.Grow(context.Background(), to); err == nil {
				t.Fatalf("write %d: grow succeeded on a dead store", n)
			}
			for _, img := range [][]byte{fb.inner.snapshot(), fb.durable} {
				rc, err := openMem(&memBacking{data: img}, OpenRead)
				if err != nil {
					t.Fatalf("write %d torn %v: %v", n, torn, err)
				}
				if s := rc.Info().LogicalSize; s != 2<<20 && s != to {
					t.Fatalf("size %d", s)
				}
				got := make([]byte, len(want))
				rc.ReadAt(got, 0)
				if !bytes.Equal(got, want) {
					t.Fatalf("write %d torn %v: the old volume changed", n, torn)
				}
				wb := &memBacking{data: append([]byte(nil), img...)}
				wc, err := openMem(wb, OpenWrite)
				if err != nil {
					t.Fatal(err)
				}
				wc.Close()
				if _, err := mustOpenMem(t, wb, OpenRead).Verify(context.Background(), nil); err != nil {
					t.Fatalf("write %d torn %v: after repair: %v", n, torn, err)
				}
			}
		}
	}
}

// compactBase is a container with holes: clusters written, then dropped from
// the map, the way a later release of unused clusters leaves them.
func compactBase(t *testing.T) ([]byte, *refVolume) {
	return crashBase(t, baseOptions(), func(c *Container, ref *refVolume) {
		for i := 0; i < 12; i++ {
			p := pattern(byte(i+1), 1<<16)
			c.WriteAt(p, int64(i)<<16)
			ref.writeAt(p, int64(i)<<16)
		}
		c.Flush()
		for _, cl := range []int{1, 2, 6} {
			c.used.clear(c.entries[cl])
			c.entries[cl] = sentinel
			c.allocated--
			ref.writeAt(make([]byte, 1<<16), int64(cl)<<16)
		}
		c.pending = true
	})
}

// The shrinking order as a sweep: every write, flush and the truncate of a
// compaction (FailDuringCompactTruncate among them).
func TestVDCrash_Compact(t *testing.T) {
	base, before := compactBase(t)
	crashSweep(t, base, before, before, func(ctx context.Context, c *Container) error {
		return c.Compact(ctx, nil)
	})
}

// FailDuringCompactTruncate: the headers describe the shorter file and the cut
// never happened. The open finds the backup at physical_size - 4096, and the
// next writer finishes the cut.
func TestVDCrash_FailDuringCompactTruncate(t *testing.T) {
	base, before := compactBase(t)
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	fb.failTruncate = true
	c := mustOpenMem(t, fb, OpenWrite)
	if err := c.Compact(context.Background(), nil); !errors.Is(err, ErrIO) {
		t.Fatalf("compact on a failing truncate: %v", err)
	}
	img := fb.inner.snapshot()
	if len(img) != len(base) {
		t.Fatal("the file was cut")
	}
	// Destroy the stale backup at L - 4096: only the one at physical_size -
	// 4096 is left to find.
	copy(img[len(img)-headerSize:], make([]byte, headerSize))
	rc := mustOpenMem(t, &memBacking{data: img}, OpenRead)
	info := rc.Info()
	if info.PhysicalSize >= info.FileSize || info.Backup != "current" || info.FromBackup {
		t.Fatalf("after the cut failed: %+v", info)
	}
	if !bytes.Equal(readAll(t, rc), before.image()) {
		t.Fatal("the volume changed")
	}
	wb := &memBacking{data: img}
	wc := mustOpenMem(t, wb, OpenWrite)
	wc.Close()
	if int64(len(wb.snapshot())) != info.PhysicalSize {
		t.Fatalf("the writer left %d bytes, the header says %d", len(wb.snapshot()), info.PhysicalSize)
	}
	if _, err := mustOpenMem(t, wb, OpenRead).Verify(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
}

// StopMidCompact: the stop arrives between moves. The run says stopped, the
// container stays at its last committed generation, and Close marks it clean.
func TestVDCrash_StopMidCompact(t *testing.T) {
	base, before := compactBase(t)
	mb := &memBacking{data: append([]byte(nil), base...)}
	c := mustOpenMem(t, mb, OpenWrite)
	gen := c.hdr.MapGeneration
	ctx, cancel := context.WithCancel(context.Background())
	err := c.Compact(ctx, &stopAfter{n: 1, cancel: cancel})
	if !errors.Is(err, ErrStopped) || ExitClass(err) != ExitStopped {
		t.Fatalf("a stopped compaction: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	rc := mustOpenMem(t, mb, OpenRead)
	if info := rc.Info(); !info.Clean || info.MapGeneration != gen {
		t.Fatalf("after the stop: clean %v, generation %d (was %d)", info.Clean, info.MapGeneration, gen)
	}
	if !bytes.Equal(readAll(t, rc), before.image()) {
		t.Fatal("the stopped compaction changed the volume")
	}
}

// FailDuringHeaderWrite: the primary is torn mid-write, so the backup carries
// the open, and the next writer rewrites the primary.
func TestVDCrash_FailDuringHeaderWrite(t *testing.T) {
	base, before := crashBase(t, baseOptions(), fillSome)
	// Count the writes of a flush that commits, and tear the primary header
	// write of its commit: the second write from the end is the primary.
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	c := mustOpenMem(t, fb, OpenWrite)
	c.WriteAt(pattern(1, 10), 30<<16)
	c.Flush()
	primaryWrite := fb.writes - 1
	fb = newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	fb.failWrite, fb.torn = primaryWrite, true
	c = mustOpenMem(t, fb, OpenWrite)
	c.WriteAt(pattern(1, 10), 30<<16)
	if err := c.Flush(); !errors.Is(err, ErrIO) {
		t.Fatalf("flush: %v", err)
	}
	img := fb.inner.snapshot()
	rc := mustOpenMem(t, &memBacking{data: img}, OpenRead)
	if !rc.Info().FromBackup {
		t.Fatal("the torn primary still opened")
	}
	after := before.clone()
	after.writeAt(pattern(1, 10), 30<<16)
	checkCrashImage(t, "torn primary", img, before, after)
}

// FailOnSync: a failed flush surfaces as ErrIO and the clean marker is not
// set by the Close that follows.
func TestVDCrash_FailOnSync(t *testing.T) {
	base, _ := crashBase(t, baseOptions(), fillSome)
	// The first flush of a session is the clean-marker header write; let it
	// through and fail the flush of the data.
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	fb.failSync = 3
	c := mustOpenMem(t, fb, OpenWrite)
	c.WriteAt(pattern(3, 5000), 100) // in place: no extension, so the third flush is the data flush
	err := c.Flush()
	if !errors.Is(err, ErrIO) {
		t.Fatalf("flush on a failing sync: %v", err)
	}
	if err := c.Close(); !errors.Is(err, ErrIO) {
		t.Fatalf("close after a failed flush: %v", err)
	}
	rc := mustOpenMem(t, &memBacking{data: fb.inner.snapshot()}, OpenRead)
	if rc.Info().Clean {
		t.Fatal("the clean marker was set after a failed flush")
	}
}

// KillNoClose: the process disappears after a flush. The marker stays clear,
// and the next open reports it with the time of the last good save.
func TestVDCrash_KillNoClose(t *testing.T) {
	base, before := crashBase(t, baseOptions(), fillSome)
	mb := &memBacking{data: append([]byte(nil), base...)}
	c := mustOpenMem(t, mb, OpenMount)
	p := pattern(9, 90000)
	c.WriteAt(p, 3<<16)
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	// no Close: the process is gone
	rc := mustOpenMem(t, &memBacking{data: mb.snapshot()}, OpenRead)
	info := rc.Info()
	if info.Clean || info.LastGoodSave.IsZero() || info.MountCount != 1 {
		t.Fatalf("after a kill: %+v", info)
	}
	after := before.clone()
	after.writeAt(p, 3<<16)
	if !bytes.Equal(readAll(t, rc), after.image()) {
		t.Fatal("a flushed write was lost")
	}
}

// An interrupted ram save (FDD-FORMAT section 10.4): the header says a save
// was in progress, with both times, and the marker is clear. As a sweep, every
// point of the save leaves each sector old or new.
func TestVDCrash_RAMSave(t *testing.T) {
	o := baseOptions()
	o.Profile = ProfileRAM
	base, before := crashBase(t, o, fillSome)
	after := before.clone()
	p := pattern(60, 4<<16)
	after.writeAt(p, 13<<16)
	crashSweep(t, base, before, after, func(ctx context.Context, c *Container) error {
		if _, err := c.WriteAt(p, 13<<16); err != nil {
			return err
		}
		return c.Flush()
	})
	// Named state: a crash inside step 2 of the save.
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	c := mustOpenMem(t, fb, OpenWrite)
	c.WriteAt(p, 13<<16)
	fb.failWrite = fb.writes + 3 // the save header pair, then the first cluster
	if err := c.Flush(); !errors.Is(err, ErrIO) {
		t.Fatalf("save: %v", err)
	}
	info := mustOpenMem(t, &memBacking{data: fb.inner.snapshot()}, OpenRead).Info()
	if info.Clean || !info.SaveInProgress || info.SaveStarted.IsZero() || info.LastGoodSave.IsZero() {
		t.Fatalf("an interrupted save: %+v", info)
	}
}
