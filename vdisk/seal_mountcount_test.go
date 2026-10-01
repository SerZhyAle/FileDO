package vdisk

import (
	"context"
	"path/filepath"
	"testing"
)

// SP-0064 T3-F4: a copy holds its source's volume, so it carries the source's
// mount count, and at least 1 once any cluster was copied. Mount reads
// mount_count 0 as "never held data" and may then format a disk that shows no
// partition table; a clone of a used volume must never look like that.
func TestVD_CopyCarriesTheMountCount(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	mk := func(name string, profile Profile, mounts int, data bool) string {
		t.Helper()
		p := filepath.Join(dir, name)
		c, err := Create(ctx, CreateOptions{Path: p, LogicalSize: 1 << 20, ClusterShift: 16, Profile: profile})
		if err != nil {
			t.Fatal(err)
		}
		if data {
			if _, err := c.WriteAt(pattern(9, 4096), 0); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < mounts; i++ {
			m, err := Open(ctx, p, nil, OpenMount)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := m.WriteAt(pattern(byte(i), 512), 65536); err != nil {
				t.Fatal(err)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
		}
		return p
	}
	count := func(p string) uint64 {
		t.Helper()
		i, err := Inspect(p)
		if err != nil {
			t.Fatal(err)
		}
		return i.MountCount
	}

	for _, c := range []struct {
		name    string
		profile Profile
		mounts  int
		data    bool
		seal    bool
		want    uint64
	}{
		{"used plain, clone", ProfilePlain, 2, true, false, 2},
		{"used plain, seal", ProfilePlain, 3, true, true, 3},
		{"never mounted, data written, clone", ProfilePlain, 0, true, false, 1},
		{"fresh plain, clone", ProfilePlain, 0, false, false, 0},
		{"fresh fast, clone", ProfileFast, 0, false, false, 0},
		{"used fast, clone", ProfileFast, 1, false, false, 1},
	} {
		src := mk(c.name+".fdd", c.profile, c.mounts, c.data)
		if got := count(src); got != uint64(c.mounts) {
			t.Fatalf("%s: the source counts %d mounts, want %d", c.name, got, c.mounts)
		}
		dst := filepath.Join(dir, c.name+".copy.fdd")
		var err error
		if c.seal {
			err = Seal(ctx, src, dst, nil)
		} else {
			err = CopyWith(ctx, CopyOptions{Src: src, Dst: dst})
		}
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got := count(dst); got != c.want {
			t.Errorf("%s: the copy records mount_count %d, want %d", c.name, got, c.want)
		}
		if i, _ := Inspect(dst); !i.Clean {
			t.Errorf("%s: the copy is not marked closed cleanly", c.name)
		}
	}
}
