package vdisk

import (
	"bytes"
	"context"
	"errors"
	"math/rand"
	"testing"
)

// SP-0148 S9: the partition carrier's read cache. The device under it counts
// what reaches it; every read a cache serves must equal what an uncached read
// of the same bytes would return.

type countingDevice struct {
	*strictDevice
	reads, writes int
	readBytes     int64
	failWrites    bool
}

func (d *countingDevice) ReadAt(p []byte, off int64) (int, error) {
	d.reads++
	d.readBytes += int64(len(p))
	return d.strictDevice.ReadAt(p, off)
}

func (d *countingDevice) WriteAt(p []byte, off int64) (int, error) {
	d.writes++
	if d.failWrites {
		// A write that landed half: the first sector only.
		d.strictDevice.WriteAt(p[:d.sector], off)
		return 0, errors.New("device failed")
	}
	return d.strictDevice.WriteAt(p, off)
}

func newCachedRegion(t testing.TB, sector, cacheBytes int64) (*regionCarrier, *countingDevice) {
	t.Helper()
	dev := &countingDevice{strictDevice: newStrictDevice(testPartition, sector, 0x33)}
	r := newTestRegion(t, dev, testPartition, sector)
	if !EnableReadCache(r, cacheBytes) {
		t.Fatal("the cache did not turn on")
	}
	return r, dev
}

// A random mix of reads and writes - aligned and not, small and large, on
// 512- and 4096-byte-sector devices - always reads what a plain slice holds.
func TestReadCache_MatchesReference(t *testing.T) {
	for _, sector := range []int64{512, 4096} {
		r, dev := newCachedRegion(t, sector, 64*cacheBlock) // 256 KiB: small, so eviction runs constantly
		ref := bytes.Clone(dev.data)
		rng := rand.New(rand.NewSource(7 + sector))
		for i := 0; i < 6000; i++ {
			size := 1 + rng.Intn(3*cacheBlock)
			if rng.Intn(25) == 0 {
				size = 70*cacheBlock + rng.Intn(cacheBlock) // above the cache's own size limit: bypasses it
			}
			off := rng.Int63n(testPartition - int64(size))
			if rng.Intn(3) == 0 { // aligned to the device sector, the container's own shape
				off &^= sector - 1
				size = (size + int(sector) - 1) &^ (int(sector) - 1)
				if off+int64(size) > testPartition {
					size = int(testPartition - off)
				}
			}
			if rng.Intn(4) == 0 {
				p := make([]byte, size)
				rng.Read(p)
				if _, err := r.WriteAt(p, off); err != nil {
					t.Fatalf("sector %d write %d@%d: %v", sector, size, off, err)
				}
				copy(ref[off:], p)
				continue
			}
			got := make([]byte, size)
			if _, err := r.ReadAt(got, off); err != nil {
				t.Fatalf("sector %d read %d@%d: %v", sector, size, off, err)
			}
			if !bytes.Equal(got, ref[off:off+int64(size)]) {
				t.Fatalf("sector %d read %d@%d differs from the reference", sector, size, off)
			}
		}
		if !bytes.Equal(dev.data, ref) {
			t.Fatalf("sector %d: the device differs from the reference - a write was lost or misplaced", sector)
		}
		if hits, misses := ReadCacheStats(r); hits == 0 || misses == 0 {
			t.Fatalf("sector %d: hits %d, misses %d; both must happen in this mix", sector, hits, misses)
		}
	}
}

// A repeated small read reaches the device once.
func TestReadCache_HitSkipsDevice(t *testing.T) {
	r, dev := newCachedRegion(t, 4096, 1<<20)
	buf := make([]byte, 4096)
	for i := 0; i < 50; i++ {
		if _, err := r.ReadAt(buf, 40960); err != nil {
			t.Fatal(err)
		}
	}
	if dev.reads != 1 {
		t.Fatalf("%d device reads for 50 reads of one block, want 1", dev.reads)
	}
	if hits, misses := ReadCacheStats(r); hits != 49 || misses != 1 {
		t.Fatalf("hits %d, misses %d, want 49 and 1", hits, misses)
	}
}

// Blocks missing from the middle of a read are fetched in runs, not one by one.
func TestReadCache_MissesAreCoalesced(t *testing.T) {
	r, dev := newCachedRegion(t, 4096, 1<<20)
	one := make([]byte, 4096)
	r.ReadAt(one, 4*4096) // block 4 cached
	dev.reads = 0
	all := make([]byte, 10*4096) // blocks 0..9
	if _, err := r.ReadAt(all, 0); err != nil {
		t.Fatal(err)
	}
	if dev.reads != 2 { // [0,4) and [5,10)
		t.Fatalf("%d device reads, want 2 (the run before block 4 and the run after)", dev.reads)
	}
}

// A read above the cache's size limit goes straight to the device and does
// not push the working set out.
func TestReadCache_LargeReadBypasses(t *testing.T) {
	r, dev := newCachedRegion(t, 4096, 64*cacheBlock) // limit 16 blocks
	small := make([]byte, 4096)
	r.ReadAt(small, 0)
	big := make([]byte, 32*4096)
	if _, err := r.ReadAt(big, 1<<20); err != nil {
		t.Fatal(err)
	}
	dev.reads = 0
	r.ReadAt(small, 0)
	if dev.reads != 0 {
		t.Fatal("a large read evicted a cached block")
	}
	r.ReadAt(small, 1<<20)
	if dev.reads != 1 {
		t.Fatal("a large read was cached; it must bypass")
	}
}

// Least recently used goes first, and the cache never grows past its cap.
func TestReadCache_BoundedLRU(t *testing.T) {
	r, dev := newCachedRegion(t, 4096, 16*cacheBlock)
	buf := make([]byte, 4096)
	for b := int64(0); b < 16; b++ {
		r.ReadAt(buf, b*4096)
	}
	r.ReadAt(buf, 0) // block 0 is now the newest
	r.ReadAt(buf, 16*4096)
	if n := r.cache.lru.Len(); n != 16 {
		t.Fatalf("%d cached blocks, cap 16", n)
	}
	dev.reads = 0
	r.ReadAt(buf, 0)
	if dev.reads != 0 {
		t.Fatal("the newest block was evicted")
	}
	r.ReadAt(buf, 1*4096)
	if dev.reads != 1 {
		t.Fatal("the oldest block was not the one evicted")
	}
}

// A write updates a cached block, so the next read sees it without the device.
func TestReadCache_WriteUpdates(t *testing.T) {
	r, dev := newCachedRegion(t, 4096, 1<<20)
	buf := make([]byte, 4096)
	r.ReadAt(buf, 8192)
	nw := bytes.Repeat([]byte{0xAB}, 4096)
	if _, err := r.WriteAt(nw, 8192); err != nil {
		t.Fatal(err)
	}
	dev.reads = 0
	if _, err := r.ReadAt(buf, 8192); err != nil || !bytes.Equal(buf, nw) || dev.reads != 0 {
		t.Fatalf("after the write: err %v, equal %v, device reads %d", err, bytes.Equal(buf, nw), dev.reads)
	}
	if !bytes.Equal(dev.data[8192:12288], nw) {
		t.Fatal("the device does not hold the written bytes: write-through broke")
	}
}

// A write that fails drops what it touched: the cache must not keep bytes the
// device never took, nor the old bytes of a block the device half changed.
func TestReadCache_FailedWriteForgets(t *testing.T) {
	r, dev := newCachedRegion(t, 4096, 1<<20)
	buf := make([]byte, 2*4096)
	r.ReadAt(buf, 0)
	dev.failWrites = true
	bad := bytes.Repeat([]byte{0xEE}, 2*4096)
	if _, err := r.WriteAt(bad, 0); err == nil {
		t.Fatal("the failing device accepted the write")
	}
	dev.failWrites = false
	got := make([]byte, 2*4096)
	if _, err := r.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, dev.data[:2*4096]) {
		t.Fatal("the cache disagrees with the device after a failed write")
	}
}

// A whole container on a cached carrier: create, write, close, reopen with a
// cache, read twice (the second from the cache) and verify. The cache sits
// under the container's own writes too, so the creation order is exercised.
func TestReadCache_ContainerRoundTrip(t *testing.T) {
	for _, cache := range []bool{false, true} {
		dev := newStrictDevice(testPartition, 4096, 0x5a)
		r := newTestRegion(t, dev, testPartition, 4096)
		if cache {
			EnableReadCache(r, 1<<20)
		}
		c, err := CreateOn(context.Background(), r, partOptions(ProfilePlain))
		if err != nil {
			t.Fatal(err)
		}
		data := pattern(3, 400000)
		if _, err := c.WriteAt(data, 1<<20+13); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		r2 := newTestRegion(t, dev, testPartition, 4096)
		if cache {
			EnableReadCache(r2, 1<<20)
		}
		c2, err := OpenOn(context.Background(), r2, "fdpart:test", nil, OpenRead)
		if err != nil {
			t.Fatal(err)
		}
		got := make([]byte, len(data))
		for pass := 0; pass < 2; pass++ {
			if _, err := c2.ReadAt(got, 1<<20+13); err != nil || !bytes.Equal(got, data) {
				t.Fatalf("cache %v pass %d: read back %v, equal %v", cache, pass, err, bytes.Equal(got, data))
			}
		}
		if rep, err := c2.Verify(context.Background(), nil); err != nil || len(rep.Problems) != 0 {
			t.Fatalf("cache %v verify: %v %v", cache, err, rep.Problems)
		}
		if err := c2.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

// Cap and carrier kind: a file carrier takes no cache, a cap below eight
// blocks leaves it off, and the cap never exceeds the partition.
func TestReadCache_Enable(t *testing.T) {
	dev := newStrictDevice(testPartition, 4096, 0)
	r := newTestRegion(t, dev, testPartition, 4096)
	if EnableReadCache(r, 4*cacheBlock) {
		t.Fatal("a cap of four blocks turned the cache on")
	}
	if !EnableReadCache(r, 1<<40) {
		t.Fatal("a large cap was refused")
	}
	if got := r.cache.maxBlocks; int64(got)*cacheBlock != testPartition {
		t.Fatalf("cap %d blocks, want the whole partition", got)
	}
	if EnableReadCache(fileLikeCarrier{}, 1<<20) {
		t.Fatal("a non-region carrier took a cache")
	}
}

type fileLikeCarrier struct{ Carrier }
