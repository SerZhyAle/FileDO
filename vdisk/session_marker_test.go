package vdisk

import (
	"context"
	"errors"
	"testing"
)

// SP-0072 AUD-36-F1/F2: a writer session that did not mount, or that did not
// complete a save, must not write "closed cleanly" over what it found. A
// header with clean 1 next to save_in_progress 1 is refused by the reader as
// damaged (FDD-FORMAT 5.2), which turned a normal verb after a power cut inside
// a ram save into a container nothing could open.

func interruptedRAMSave(t *testing.T) []byte {
	t.Helper()
	o := baseOptions()
	o.Profile = ProfileRAM
	base, _ := crashBase(t, o, fillSome)
	fb := newFailBacking(&memBacking{data: append([]byte(nil), base...)})
	c := mustOpenMem(t, fb, OpenWrite)
	c.WriteAt(pattern(60, 4<<16), 13<<16)
	fb.failWrite = fb.writes + 3 // the save header pair, then the first cluster
	if err := c.Flush(); !errors.Is(err, ErrIO) {
		t.Fatalf("save: %v", err)
	}
	snap := fb.inner.snapshot()
	info := mustOpenMem(t, &memBacking{data: append([]byte(nil), snap...)}, OpenRead).Info()
	if info.Clean || !info.SaveInProgress {
		t.Fatalf("the fixture is not an interrupted save: %+v", info)
	}
	return snap
}

func TestVDSession_GrowAfterAnInterruptedRAMSaveStaysReadable(t *testing.T) {
	snap := interruptedRAMSave(t)
	mb := &memBacking{data: snap}
	c := mustOpenMem(t, mb, OpenWrite)
	if err := c.Grow(context.Background(), 4<<20); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := openMem(&memBacking{data: mb.snapshot()}, OpenRead)
	if err != nil {
		t.Fatalf("the container cannot be opened after grow closed over an interrupted save: %v", err)
	}
	info := r.Info()
	if info.Clean && info.SaveInProgress {
		t.Fatalf("clean and save_in_progress are both set: %+v", info)
	}
	if info.Clean || !info.SaveInProgress {
		t.Errorf("the interrupted save was hidden by the session: clean=%v save_in_progress=%v", info.Clean, info.SaveInProgress)
	}
	if rep, verr := r.Verify(context.Background(), nil); verr != nil {
		t.Errorf("Verify after the session: %v (%v)", verr, rep)
	}
}

// A session that did not mount keeps the marker it found: a container killed
// while mounted stays "not closed cleanly" after an offline grow.
func TestVDSession_AnOfflineWriterKeepsTheUncleanMarker(t *testing.T) {
	base, _ := crashBase(t, baseOptions(), fillSome)
	mb := &memBacking{data: append([]byte(nil), base...)}
	c := mustOpenMem(t, mb, OpenMount)
	c.WriteAt(pattern(7, 4096), 3<<16)
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	killed := mb.snapshot() // no Close: the process died while the volume was mounted
	if info := mustOpenMem(t, &memBacking{data: append([]byte(nil), killed...)}, OpenRead).Info(); info.Clean {
		t.Fatalf("the fixture is clean: %+v", info)
	}

	g := &memBacking{data: killed}
	w := mustOpenMem(t, g, OpenWrite)
	if err := w.Grow(context.Background(), 3<<20); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if info := mustOpenMem(t, &memBacking{data: g.snapshot()}, OpenRead).Info(); info.Clean {
		t.Errorf("an offline grow reset the not-closed-cleanly marker: %+v", info)
	}

	// A container that was clean at open still ends clean.
	cb := &memBacking{data: append([]byte(nil), base...)}
	cw := mustOpenMem(t, cb, OpenWrite)
	if err := cw.Grow(context.Background(), 3<<20); err != nil {
		t.Fatal(err)
	}
	if err := cw.Close(); err != nil {
		t.Fatal(err)
	}
	if info := mustOpenMem(t, &memBacking{data: cb.snapshot()}, OpenRead).Info(); !info.Clean {
		t.Errorf("a clean container did not end clean after grow: %+v", info)
	}
}
