package vdisk

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

// The unbuffered device I/O path over a real handle. A partition cannot be
// opened without elevation, so a file opened the way a partition is
// (FILE_FLAG_NO_BUFFERING, no write-through) stands in: it imposes the same
// sector alignment on offsets, lengths and memory, so the bounce buffers and
// the chunking are exercised for real.
func TestVDPart_DeviceIOUnbuffered(t *testing.T) {
	path := filepath.Join(t.TempDir(), "region.bin")
	if err := os.WriteFile(path, make([]byte, testPartition), 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_NO_BUFFERING, 0)
	if err != nil {
		t.Fatal(err)
	}
	dev := &deviceIO{h: h}
	r, err := NewRegion(dev, testPartition, 4096)
	if err != nil {
		dev.Close()
		t.Fatal(err)
	}
	c, err := CreateOn(context.Background(), r, partOptions(ProfileFast))
	if err != nil {
		t.Fatal(err)
	}
	// An unaligned caller buffer: a slice one byte into an allocation.
	raw := make([]byte, 3<<20+1)
	data := raw[1:]
	copy(data, pattern(12, len(data)))
	if _, err := c.WriteAt(data, 1<<20); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := Open(context.Background(), path, nil, OpenRead)
	if err != nil {
		t.Fatalf("the region written through the unbuffered handle: %v", err)
	}
	defer f.Close()
	got := make([]byte, len(data))
	if _, err := f.ReadAt(got, 1<<20); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read back: %v", err)
	}
}

// An indicative speed check of the partition read cache (SP-0148 S9; the gate
// itself is the end-to-end measurement of manual kit H14). Random 4 KiB reads
// at queue depth 1 over a working set of 32 MiB on an unbuffered handle, with
// and without the cache: `go test ./vdisk -run xxx -bench ReadCache`.
func BenchmarkReadCache_Rand4K(b *testing.B) {
	for _, withCache := range []bool{false, true} {
		name := "nocache"
		if withCache {
			name = "cache"
		}
		b.Run(name, func(b *testing.B) {
			const size, working = 256 << 20, 32 << 20
			path := filepath.Join(b.TempDir(), "region.bin")
			if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
				b.Fatal(err)
			}
			p, _ := windows.UTF16PtrFromString(path)
			h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
				windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_NO_BUFFERING, 0)
			if err != nil {
				b.Fatal(err)
			}
			dev := &deviceIO{h: h}
			c, err := NewRegion(dev, size, 4096)
			if err != nil {
				dev.Close()
				b.Fatal(err)
			}
			defer c.Close()
			if withCache {
				EnableReadCache(c, 64<<20)
			}
			buf := make([]byte, 4096)
			x := uint64(88172645463325252)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				x ^= x << 13
				x ^= x >> 7
				x ^= x << 17
				if _, err := c.ReadAt(buf, int64(x%(working/4096))*4096); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
