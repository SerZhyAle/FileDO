package vdisk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Property tests (P2 section 6.2): seeded sequences of 200 operations drive
// the container and the reference with the same operations, and the whole
// logical image is compared byte for byte after every step. A failure prints
// its seed, its step and the first differing offset; a seed that ever failed
// is pinned in regressionSeeds. FDD_PROPERTY_SEEDS=n runs n more seeds.

var regressionSeeds = []uint64{}

func TestVD_Property_Sequences(t *testing.T) {
	seeds := append([]uint64{1, 2, 3, 4}, regressionSeeds...)
	if n, err := strconv.Atoi(os.Getenv("FDD_PROPERTY_SEEDS")); err == nil {
		for i := 0; i < n; i++ {
			seeds = append(seeds, uint64(1000+i))
		}
	}
	for _, seed := range seeds {
		for _, cfg := range []struct {
			profile Profile
			shift   uint8
		}{{ProfilePlain, 9}, {ProfilePlain, 12}, {ProfileFast, 12}, {ProfileRAM, 9}} {
			name := fmt.Sprintf("seed%d/%s/%d", seed, cfg.profile, 1<<cfg.shift)
			t.Run(name, func(t *testing.T) { runSequence(t, seed, cfg.profile, cfg.shift) })
		}
	}
}

func runSequence(t *testing.T, seed uint64, profile Profile, shift uint8) {
	rng := rand.New(rand.NewPCG(seed, uint64(profile)<<8|uint64(shift)))
	const cs = 1 << 16
	ss := int64(1) << shift
	// A fully mapped profile decrypts its whole volume at every comparison, so
	// it runs on a smaller one; the edge cases are per cluster, not per size.
	size := []int64{1 << 20, 1<<20 + 3*4096, 5 * cs / 2 * 4}[rng.IntN(3)]
	limit := int64(3 << 20)
	if profile != ProfilePlain {
		size, limit = (size/4)&^4095, 1<<20
	}
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: size, ClusterShift: 16, SectorShift: shift, Profile: profile})
	ref := newRef(size, cs)
	var log []string
	fail := func(step int, format string, args ...interface{}) {
		t.Helper()
		t.Fatalf("seed %d, step %d (%s): %s\nlast operations:\n%s", seed, step, profile, fmt.Sprintf(format, args...), strings.Join(log[max(0, len(log)-8):], "\n"))
	}
	span := func() (int64, int) {
		lengths := []int{0, 1, int(ss), cs - 1, cs, cs + 1, rng.IntN(3 * cs), rng.IntN(600) + 1}
		n := lengths[rng.IntN(len(lengths))]
		if int64(n) > ref.size {
			n = int(ref.size)
		}
		offs := []int64{
			0,
			int64(rng.IntN(int(ref.size/cs)))*cs - int64(rng.IntN(3)),
			ref.size - ss,
			ref.size - int64(n),
			rng.Int64N(ref.size - int64(n) + 1),
		}
		off := offs[rng.IntN(len(offs))]
		off = max(0, min(off, ref.size-int64(n)))
		return off, n
	}
	for step := 0; step < 200; step++ {
		switch op := rng.IntN(100); {
		case op < 45:
			off, n := span()
			p := pattern(byte(rng.IntN(256)), n)
			if rng.IntN(8) == 0 {
				clear(p)
			}
			log = append(log, fmt.Sprintf("write(%d, %d)", off, n))
			if _, err := c.WriteAt(p, off); err != nil {
				fail(step, "write: %v", err)
			}
			ref.writeAt(p, off)
		case op < 58:
			off, n := span()
			log = append(log, fmt.Sprintf("read(%d, %d)", off, n))
			got, want := make([]byte, n), make([]byte, n)
			if k, err := c.ReadAt(got, off); err != nil || k != n {
				fail(step, "read: %d %v", k, err)
			}
			ref.readAt(want, off)
			if !bytes.Equal(got, want) {
				fail(step, "a read differs from the reference")
			}
		case op < 68:
			log = append(log, "flush")
			if err := c.Flush(); err != nil {
				fail(step, "flush: %v", err)
			}
		case op < 76:
			log = append(log, "close+reopen")
			if err := c.Close(); err != nil {
				fail(step, "close: %v", err)
			}
			var err error
			if c, err = openMem(mb, OpenWrite); err != nil {
				fail(step, "reopen: %v", err)
			}
			if !c.Info().Clean {
				fail(step, "reopened unclean after a clean close")
			}
		case op < 81:
			if ref.size >= limit {
				continue
			}
			newSize := ref.size + ss*int64(1+rng.IntN(200))
			log = append(log, fmt.Sprintf("grow(%d)", newSize))
			if err := c.Grow(context.Background(), newSize); err != nil {
				fail(step, "grow: %v", err)
			}
			ref.grow(newSize)
		case op < 87:
			log = append(log, "compact")
			if err := c.Compact(context.Background(), nil); err != nil {
				fail(step, "compact: %v", err)
			}
		case op < 92:
			log = append(log, "verify")
			if _, err := c.Verify(context.Background(), nil); err != nil {
				fail(step, "verify: %v", err)
			}
		default:
			off := ref.size - int64(rng.IntN(int(ss)))
			n := int(ref.size-off) + 1 + rng.IntN(5000)
			log = append(log, fmt.Sprintf("write past the end (%d, %d)", off, n))
			if _, err := c.WriteAt(make([]byte, n), off); !errors.Is(err, ErrUsage) {
				fail(step, "a write past the end: %v", err)
			}
		}
		if got, want := readAll(t, c), ref.image(); !bytes.Equal(got, want) {
			i := 0
			for i < len(got) && got[i] == want[i] {
				i++
			}
			fail(step, "the volume differs from the reference at offset %d", i)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatalf("seed %d: close: %v", seed, err)
	}
	rc := mustOpenMem(t, mb, OpenRead)
	if !bytes.Equal(readAll(t, rc), ref.image()) {
		t.Fatalf("seed %d: the reopened volume differs", seed)
	}
	if _, err := rc.Verify(context.Background(), nil); err != nil {
		t.Fatalf("seed %d: verify after close: %v", seed, err)
	}
}
