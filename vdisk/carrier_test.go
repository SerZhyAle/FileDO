package vdisk

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"

	"filedo/fdsec"
)

// SP-0148 S1: the partition carrier. A region over a memory device stands in
// for a partition; every rule of FDD-FORMAT section 3.1 is held here.

// strictDevice is a whole partition in memory that refuses what a partition
// device refuses: an access outside it, and one not aligned to its sector.
type strictDevice struct {
	data   []byte
	sector int64
	syncs  int
}

func newStrictDevice(size, sector int64, fill byte) *strictDevice {
	d := &strictDevice{data: make([]byte, size), sector: sector}
	if fill != 0 {
		for i := range d.data {
			d.data[i] = fill ^ byte(i>>12)
		}
	}
	return d
}

func (d *strictDevice) check(n int, off int64) error {
	if off < 0 || off+int64(n) > int64(len(d.data)) {
		return errOutOfRegion
	}
	if off%d.sector != 0 || int64(n)%d.sector != 0 {
		return errors.New("strict device: unaligned I/O")
	}
	return nil
}

func (d *strictDevice) ReadAt(p []byte, off int64) (int, error) {
	if err := d.check(len(p), off); err != nil {
		return 0, err
	}
	return copy(p, d.data[off:]), nil
}

func (d *strictDevice) WriteAt(p []byte, off int64) (int, error) {
	if err := d.check(len(p), off); err != nil {
		return 0, err
	}
	return copy(d.data[off:], p), nil
}

func (d *strictDevice) Sync() error { d.syncs++; return nil }

const testPartition = 8 << 20

func newTestRegion(t testing.TB, d RegionDevice, size, sector int64) *regionCarrier {
	t.Helper()
	c, err := NewRegion(d, size, sector)
	if err != nil {
		t.Fatal(err)
	}
	return c.(*regionCarrier)
}

func partOptions(p Profile) CreateOptions {
	o := CreateOptions{Profile: p, ClusterShift: 16}
	if p == ProfileVault {
		o.Credential = fdsec.Credential("pass")
	}
	return o
}

// Every profile allowed on a partition is created, written, closed cleanly,
// reopened and verified; physical_size is L, the backup sits at L - 4096, and
// not one write was unaligned on a 4096-byte-sector device.
func TestVDPart_ProfilesRoundTrip(t *testing.T) {
	for _, p := range []Profile{ProfilePlain, ProfileFast, ProfileVault, ProfileRAM} {
		t.Run(p.String(), func(t *testing.T) {
			dev := newStrictDevice(testPartition, 4096, 0x5a)
			r := newTestRegion(t, dev, testPartition, 4096)
			c, err := CreateOn(context.Background(), r, partOptions(p))
			if err != nil {
				t.Fatal(err)
			}
			info := c.Info()
			if want := PartitionLogicalMax(testPartition, 16); info.LogicalSize != want || want <= 0 {
				t.Fatalf("volume %d, want the partition's maximum %d", info.LogicalSize, want)
			}
			if info.PhysicalSize != testPartition || info.FileSize != testPartition {
				t.Fatalf("physical_size %d, L %d; both must be the partition's %d", info.PhysicalSize, info.FileSize, testPartition)
			}
			data := pattern(9, 300000)
			if _, err := c.WriteAt(data, 1<<20+77); err != nil {
				t.Fatal(err)
			}
			if p == ProfileRAM {
				if err := c.Save(); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			if r.bridged != 0 {
				t.Fatalf("%d writes were not whole 4096-byte sectors", r.bridged)
			}
			r2 := newTestRegion(t, dev, testPartition, 4096)
			c2, err := OpenOn(context.Background(), r2, "fdpart:test", partOptions(p).Credential, OpenRead)
			if err != nil {
				t.Fatal(err)
			}
			defer c2.Close()
			got := make([]byte, len(data))
			if _, err := c2.ReadAt(got, 1<<20+77); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("read back: %v, equal %v", err, bytes.Equal(got, data))
			}
			rep, err := c2.Verify(context.Background(), nil)
			if err != nil || len(rep.Problems) != 0 {
				t.Fatalf("verify: %v %v", err, rep.Problems)
			}
			if !c2.Info().Clean || c2.Info().Path != "fdpart:test" {
				t.Fatalf("clean %v, path %q", c2.Info().Clean, c2.Info().Path)
			}
			if !c2.Fixed() {
				t.Fatal("a partition container does not report itself fixed")
			}
		})
	}
}

const bigPartition = 64 << 20

// The map_stride reservation (FDD-FORMAT 3.1 rule 5): the stride is one copy
// for floor(L / C) clusters, data_offset follows both copies at that stride.
func TestVDPart_MapStrideReserved(t *testing.T) {
	dev := newStrictDevice(bigPartition, 512, 0)
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, bigPartition, 512), CreateOptions{Profile: ProfilePlain, ClusterShift: 16, LogicalSize: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	want, _ := mapSize(bigPartition >> 16)
	m, _ := mapSize(c.hdr.ClusterCount)
	if c.hdr.MapStride != want || m >= want {
		t.Fatalf("map_stride %d, want the reserved %d (one copy is %d)", c.hdr.MapStride, want, m)
	}
	if c.hdr.DataOffset != partitionDataOffset(want, 16) {
		t.Fatalf("data_offset %d", c.hdr.DataOffset)
	}
}

// A partition never grows, shrinks or truncates (rules 1-2): compact and grow
// are refused by name, and a volume larger than the partition is refused
// before any byte is written.
func TestVDPart_FixedRefusals(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	r := newTestRegion(t, dev, testPartition, 4096)
	if _, err := CreateOn(context.Background(), r, CreateOptions{Profile: ProfileFast, ClusterShift: 16, LogicalSize: testPartition}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("a volume as large as the partition: %v, want unsupported", err)
	}
	if _, err := CreateOn(context.Background(), r, CreateOptions{Profile: ProfilePlain, SectorShift: 9}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("sector 9 on a partition: %v, want unsupported", err)
	}
	if _, err := CreateOn(context.Background(), r, CreateOptions{Profile: ProfileSealed}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("sealed on a partition: %v, want unsupported", err)
	}
	c, err := CreateOn(context.Background(), r, partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Compact(context.Background(), nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("compact: %v", err)
	}
	if err := c.Grow(context.Background(), c.Info().LogicalSize*2); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("grow: %v", err)
	}
	if err := r.Truncate(testPartition - 4096); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("truncate: %v", err)
	}
	if _, err := r.WriteAt(make([]byte, 4096), testPartition); !errors.Is(err, errOutOfRegion) {
		t.Fatalf("a write past the partition: %v", err)
	}
	// Every cluster of the volume fits: write the last one.
	last := c.Info().LogicalSize - 4096
	if _, err := c.WriteAt(pattern(3, 4096), last); err != nil {
		t.Fatalf("the last sector of the volume: %v", err)
	}
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
}

// Compatibility (SP-0148 13, SP-0123 E-P7): a partition's bytes copied into a
// file open with the file reader, verify and export; and a file container
// written into a partition of exactly its length opens there.
func TestVDPart_ImageIsAFileAndBack(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0x33)
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	data := pattern(21, 200000)
	if _, err := c.WriteAt(data, 4096*3); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	raw := filepath.Join(dir, "raw.fdd")
	if err := os.WriteFile(raw, dev.data, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Open(context.Background(), raw, nil, OpenRead)
	if err != nil {
		t.Fatalf("the partition's bytes as a file: %v", err)
	}
	got := make([]byte, len(data))
	if _, err := f.ReadAt(got, 4096*3); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file read: %v", err)
	}
	if rep, err := f.Verify(context.Background(), nil); err != nil || len(rep.Problems) != 0 {
		t.Fatalf("file verify: %v %v", err, rep.Problems)
	}
	exp := filepath.Join(dir, "vol.img")
	if err := f.ExportRaw(context.Background(), exp, RawFormImage, nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	vol, _ := os.ReadFile(exp)
	if !bytes.Equal(vol[4096*3:4096*3+len(data)], data) {
		t.Fatal("the export of the file copy differs")
	}
	// The file copy opens for writing as a file too (physical_size = L).
	w, err := Open(context.Background(), raw, nil, OpenWrite)
	if err != nil {
		t.Fatalf("the file copy for writing: %v", err)
	}
	w.Close()

	// A file container in a partition of exactly its length.
	fc := filepath.Join(dir, "file.fdd")
	fcont, err := Create(context.Background(), CreateOptions{Path: fc, LogicalSize: 2 << 20, Profile: ProfileFast, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	fcont.WriteAt(data, 0)
	if err := fcont.Close(); err != nil {
		t.Fatal(err)
	}
	img, _ := os.ReadFile(fc)
	pdev := newStrictDevice(int64(len(img)), 4096, 0)
	copy(pdev.data, img)
	pc, err := OpenOn(context.Background(), newTestRegion(t, pdev, int64(len(img)), 4096), "p", nil, OpenWrite)
	if err != nil {
		t.Fatalf("a file container in a partition of its length: %v", err)
	}
	if _, err := pc.ReadAt(got, 0); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read: %v", err)
	}
	pc.Close()
}

// A partition resized under its container (E-P4): readable, never written.
func TestVDPart_ResizedRegionReadOnly(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	c.WriteAt(pattern(1, 8192), 0)
	c.Close()
	bigger := newStrictDevice(testPartition+(1<<20), 4096, 0)
	copy(bigger.data, dev.data)
	before := append([]byte(nil), bigger.data...)
	r := newTestRegion(t, bigger, int64(len(bigger.data)), 4096)
	if _, err := OpenOn(context.Background(), r, "p", nil, OpenWrite); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("write open of a resized partition: %v, want unsupported", err)
	}
	rc, err := OpenOn(context.Background(), r, "p", nil, OpenRead)
	if err != nil {
		t.Fatalf("read open of a resized partition: %v", err)
	}
	rc.Close()
	if !bytes.Equal(before, bigger.data) {
		t.Fatal("a refused or read-only open changed the partition")
	}
}

// A credential change on a 4096-byte-sector partition writes the slot region
// whole (rule 4): no bridged write, and the new credential opens it.
func TestVDPart_ChangeCredential4Kn(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	r := newTestRegion(t, dev, testPartition, 4096)
	c, err := CreateOn(context.Background(), r, partOptions(ProfileVault))
	if err != nil {
		t.Fatal(err)
	}
	c.WriteAt(pattern(4, 5000), 0)
	c.Close()
	r2 := newTestRegion(t, dev, testPartition, 4096)
	if err := ChangeCredentialOn(r2, fdsec.Credential("pass"), fdsec.Credential("next"), false); err != nil {
		t.Fatal(err)
	}
	if r2.bridged != 0 {
		t.Fatalf("%d slot writes were not whole sectors", r2.bridged)
	}
	if _, err := OpenOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), "p", fdsec.Credential("pass"), OpenRead); !errors.Is(err, ErrCredential) {
		t.Fatalf("the old credential: %v", err)
	}
	n, err := OpenOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), "p", fdsec.Credential("next"), OpenRead)
	if err != nil {
		t.Fatalf("the new credential: %v", err)
	}
	n.Close()
}

// An interrupted creation leaves both header positions zeroed (rule 6): the
// partition reads as damaged, never as the container that was there before.
func TestVDPart_InterruptedCreationIsDamaged(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	old, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	old.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CreateOn(ctx, newTestRegion(t, dev, testPartition, 4096), partOptions(ProfileFast)); !errors.Is(err, ErrStopped) {
		t.Fatalf("a stopped fast creation: %v", err)
	}
	if _, err := InspectOn(newTestRegion(t, dev, testPartition, 4096), "p"); !errors.Is(err, ErrDamaged) {
		t.Fatalf("after an interrupted creation: %v, want damaged", err)
	}
	if !allZero(dev.data[:4096]) || !allZero(dev.data[testPartition-4096:]) {
		t.Fatal("a header position is not zero")
	}
}

// Image to file copies the regions and leaves the residue behind: the image
// opens as a file, reads the volume, and holds zeros where the partition held
// someone else's old bytes.
func TestVDPart_ImageToFile(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0x77) // residue everywhere
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	data := pattern(31, 150000)
	c.WriteAt(data, 64<<10)
	c.Close()
	dst := filepath.Join(t.TempDir(), "image.fdd")
	if err := ImageToFile(context.Background(), newTestRegion(t, dev, testPartition, 4096), dst, nil); err != nil {
		t.Fatal(err)
	}
	if err := ImageToFile(context.Background(), newTestRegion(t, dev, testPartition, 4096), dst, nil); !errors.Is(err, ErrUsage) {
		t.Fatalf("an image over an existing file: %v", err)
	}
	f, err := Open(context.Background(), dst, nil, OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(data))
	if _, err := f.ReadAt(got, 64<<10); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("image read: %v", err)
	}
	if rep, err := f.Verify(context.Background(), nil); err != nil || len(rep.Problems) != 0 {
		t.Fatalf("image verify: %v %v", err, rep.Problems)
	}
	f.Close()
	img, _ := os.ReadFile(dst)
	if int64(len(img)) != testPartition {
		t.Fatalf("image length %d", len(img))
	}
	// The last unreferenced cluster before the backup kept residue on the
	// partition and is zero in the image.
	at := testPartition - 4096 - (64 << 10)
	if allZero(dev.data[at:at+4096]) || !allZero(img[at:at+4096]) {
		t.Fatal("residue outside every region was copied into the image")
	}
}

// Seal and copy from an opened partition source go to a new file.
func TestVDPart_SealFrom(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	data := pattern(8, 70000)
	c.WriteAt(data, 0)
	c.Close()
	src, err := OpenOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), "fdpart:x", nil, OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dir := t.TempDir()
	if err := SealFrom(context.Background(), src, SealOptions{Src: "fdpart:x", Dst: filepath.Join(dir, "s.fdd")}); err != nil {
		t.Fatal(err)
	}
	if err := CopyFrom(context.Background(), src, CopyOptions{Src: "fdpart:x", Dst: filepath.Join(dir, "c.fdd")}); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"s.fdd", "c.fdd"} {
		f, err := Open(context.Background(), filepath.Join(dir, n), nil, OpenRead)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(data))
		f.ReadAt(got, 0)
		f.Close()
		if !bytes.Equal(got, data) {
			t.Fatalf("%s differs", n)
		}
	}
}

// Crash sweeps on a region (SP-0123 E-P5 ported): a write-and-flush and a ram
// save cut at every write and flush leave a partition that opens with every
// sector old or new.
func TestVDPart_CrashSweep(t *testing.T) {
	for _, p := range []Profile{ProfilePlain, ProfileRAM} {
		t.Run(p.String(), func(t *testing.T) {
			const size = 4 << 20
			mk := func() *memBacking { return &memBacking{data: make([]byte, size)} }
			base := mk()
			c, err := CreateOn(context.Background(), newTestRegion(t, base, size, 4096), partOptions(p))
			if err != nil {
				t.Fatal(err)
			}
			before := newRef(c.Info().LogicalSize, 1<<16)
			fillSome(c, before)
			if p == ProfileRAM {
				if err := c.Save(); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			img := base.snapshot()
			after := before.clone()
			p1 := pattern(60, 3<<16)
			after.writeAt(p1, 11<<16)
			op := func(c *Container) error {
				if _, err := c.WriteAt(p1, 11<<16); err != nil {
					return err
				}
				if p == ProfileRAM {
					return c.Save()
				}
				return c.Flush()
			}
			probe := newFailBacking(&memBacking{data: append([]byte(nil), img...)})
			pc, err := OpenOn(context.Background(), newTestRegion(t, probe, size, 4096), "p", nil, OpenWrite)
			if err != nil {
				t.Fatal(err)
			}
			if err := op(pc); err != nil {
				t.Fatal(err)
			}
			for n := 1; n <= probe.writes; n++ {
				for _, torn := range []bool{false, true} {
					fb := newFailBacking(&memBacking{data: append([]byte(nil), img...)})
					fb.failWrite, fb.torn = n, torn
					c, err := OpenOn(context.Background(), newTestRegion(t, fb, size, 4096), "p", nil, OpenWrite)
					if err != nil {
						t.Fatal(err)
					}
					if err := op(c); err == nil && fb.dead {
						t.Fatalf("write %d: success after the device failed", n)
					}
					checkCrashImage(t, "partition crash", fb.inner.snapshot(), before, after)
					checkCrashImage(t, "partition crash, flushed only", fb.durable, before, after)
					// The cut partition opens as a partition too, and for writing.
					for _, im := range [][]byte{fb.inner.snapshot(), fb.durable} {
						w, err := OpenOn(context.Background(), newTestRegion(t, &memBacking{data: im}, size, 4096), "p", nil, OpenWrite)
						if err != nil {
							t.Fatalf("write %d: the cut partition does not open for writing: %v", n, err)
						}
						if err := w.Close(); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
		})
	}
}

// The gaps between the regions - before data_offset too - keep whatever the
// disk held; an image copies the regions only (FDD-FORMAT section 5.3).
func TestVDPart_ImageLeavesGapResidue(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0x77)
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfilePlain))
	if err != nil {
		t.Fatal(err)
	}
	h := c.hdr
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	m, _ := mapSize(h.ClusterCount)
	gapStart := int64(h.MapOffset + h.MapStride + m)
	dst := filepath.Join(t.TempDir(), "i.fdd")
	if err := ImageToFile(context.Background(), newTestRegion(t, dev, testPartition, 4096), dst, nil); err != nil {
		t.Fatal(err)
	}
	img, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !allZero(img[gapStart:h.DataOffset]) {
		t.Fatalf("the residue in [%d, %d) was copied into the image", gapStart, h.DataOffset)
	}
}

// A sparse file container copied onto a partition of its own length could
// never place its next cluster: a writer refuses it before anything changes,
// and it still opens for reading (FDD-FORMAT section 3.1 rule 2).
func TestVDPart_SparseCopyOpensReadOnly(t *testing.T) {
	fc := filepath.Join(t.TempDir(), "f.fdd")
	f, err := Create(context.Background(), CreateOptions{Path: fc, LogicalSize: 2 << 20, Profile: ProfilePlain, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(bytes.Repeat([]byte{1}, 4096), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	img, err := os.ReadFile(fc)
	if err != nil {
		t.Fatal(err)
	}
	dev := newStrictDevice(int64(len(img)), 4096, 0)
	copy(dev.data, img)
	before := append([]byte(nil), dev.data...)
	if _, err := OpenOn(context.Background(), newTestRegion(t, dev, int64(len(img)), 4096), "p", nil, OpenWrite); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("write open of a sparse copy: %v, want unsupported", err)
	}
	if !bytes.Equal(before, dev.data) {
		t.Fatal("a refused write open changed the partition")
	}
	r, err := OpenOn(context.Background(), newTestRegion(t, dev, int64(len(img)), 4096), "p", nil, OpenRead)
	if err != nil {
		t.Fatalf("read open of a sparse copy: %v", err)
	}
	r.Close()
}

// A credential change is a write: a partition whose physical_size is not L
// refuses it untouched (FDD-FORMAT section 3.1 rule 1).
func TestVDPart_ChangeCredentialResizedRefused(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(ProfileVault))
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	bigger := newStrictDevice(testPartition+(1<<20), 4096, 0)
	copy(bigger.data, dev.data)
	before := append([]byte(nil), bigger.data...)
	err = ChangeCredentialOn(newTestRegion(t, bigger, int64(len(bigger.data)), 4096), fdsec.Credential("pass"), fdsec.Credential("next"), false)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("change credential on a resized partition: %v, want unsupported", err)
	}
	if !bytes.Equal(before, bigger.data) {
		t.Fatal("a partition whose physical_size is not L was written")
	}
}

// The region refuses an offset whose end overflows before the device sees it.
func TestVDPart_RegionOffsetOverflow(t *testing.T) {
	d := &offsetDevice{last: -1}
	r := newTestRegion(t, d, testPartition, 4096)
	if _, err := r.WriteAt(make([]byte, 4096), math.MaxInt64-100); !errors.Is(err, errOutOfRegion) {
		t.Fatalf("overflowing write: %v, want errOutOfRegion", err)
	}
	if d.last != -1 {
		t.Fatalf("the device saw offset %d", d.last)
	}
}

type offsetDevice struct{ last int64 }

func (d *offsetDevice) ReadAt(p []byte, off int64) (int, error)  { d.last = off; return len(p), nil }
func (d *offsetDevice) WriteAt(p []byte, off int64) (int, error) { d.last = off; return len(p), nil }
func (d *offsetDevice) Sync() error                              { return nil }

// FDD-FORMAT section 15: on every profile a partition carries, the image of a
// partition is a container file, and that file written back into a partition
// of exactly its length is a partition carrier again - read on every profile,
// written where every logical cluster already has its place (section 3.1
// rule 2: fast and ram allocate them all at creation).
func TestVDPart_ImageBothDirectionsEveryProfile(t *testing.T) {
	for _, p := range []Profile{ProfilePlain, ProfileFast, ProfileVault, ProfileRAM} {
		t.Run(p.String(), func(t *testing.T) {
			cred := partOptions(p).Credential
			dev := newStrictDevice(testPartition, 4096, 0x3c)
			c, err := CreateOn(context.Background(), newTestRegion(t, dev, testPartition, 4096), partOptions(p))
			if err != nil {
				t.Fatal(err)
			}
			data := pattern(31, 150000)
			if _, err := c.WriteAt(data, 4096*5); err != nil {
				t.Fatal(err)
			}
			if p == ProfileRAM {
				if err := c.Save(); err != nil {
					t.Fatal(err)
				}
			}
			if err := c.Close(); err != nil {
				t.Fatal(err)
			}
			img := filepath.Join(t.TempDir(), "img.fdd")
			if err := ImageToFile(context.Background(), newTestRegion(t, dev, testPartition, 4096), img, nil); err != nil {
				t.Fatal(err)
			}
			f, err := Open(context.Background(), img, cred, OpenRead)
			if err != nil {
				t.Fatalf("the image as a file: %v", err)
			}
			got := make([]byte, len(data))
			if _, err := f.ReadAt(got, 4096*5); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("image read: %v", err)
			}
			if rep, err := f.Verify(context.Background(), nil); err != nil || len(rep.Problems) != 0 {
				t.Fatalf("image verify: %v %v", err, rep.Problems)
			}
			f.Close()

			raw, err := os.ReadFile(img)
			if err != nil {
				t.Fatal(err)
			}
			back := newStrictDevice(int64(len(raw)), 4096, 0x5e)
			copy(back.data, raw)
			r, err := OpenOn(context.Background(), newTestRegion(t, back, int64(len(raw)), 4096), "p", cred, OpenRead)
			if err != nil {
				t.Fatalf("the image back on a partition: %v", err)
			}
			if _, err := r.ReadAt(got, 4096*5); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("partition read: %v", err)
			}
			r.Close()
			w, err := OpenOn(context.Background(), newTestRegion(t, back, int64(len(raw)), 4096), "p", cred, OpenWrite)
			if err != nil {
				t.Fatalf("the image back on a partition, for writing: %v", err)
			}
			if _, err := w.WriteAt(data, 0); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
