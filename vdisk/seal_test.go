package vdisk

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filedo/fsx"
)

// TestVD_Seal: the sealed copy reads back identically, is a container of its
// own, refuses every write, and leaves its source untouched.
func TestVD_Seal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 3 << 20, ClusterShift: 16, FriendlyName: "to seal"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		c.WriteAt(pattern(byte(i), 7000), int64(i)*230001%(3<<20-7000))
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(src)
	dst := filepath.Join(dir, "sealed.fdd")
	if err := Seal(context.Background(), src, dst, nil); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
		t.Fatal("seal changed its source")
	}
	if _, err := os.Stat(dst + fsx.PartialSuffix); !os.IsNotExist(err) {
		t.Fatal("the partial file was left behind")
	}
	si, _ := Inspect(src)
	di, err := Inspect(dst)
	if err != nil {
		t.Fatal(err)
	}
	if di.Profile != ProfileSealed || !di.Clean || di.ContainerID == si.ContainerID || di.FriendlyName != "to seal" {
		t.Fatalf("sealed header: %+v", di)
	}
	a, _ := Open(context.Background(), src, nil, OpenRead)
	b, err := Open(context.Background(), dst, nil, OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readAll(t, a), readAll(t, b)) {
		t.Fatal("the sealed volume reads differently")
	}
	a.Close()
	if _, err := b.WriteAt([]byte{1}, 0); !errors.Is(err, ErrUsage) {
		t.Fatalf("a write to a sealed container: %v", err)
	}
	b.Close()
	if _, err := Open(context.Background(), dst, nil, OpenMount); err == nil {
		t.Fatal("a sealed container opened for writing")
	}
	if err := Seal(context.Background(), src, dst, nil); !errors.Is(err, ErrUsage) {
		t.Fatalf("seal over an existing file: %v", err)
	}
}

// TestVDCrash_Seal: a seal stopped half-way leaves the source byte-identical
// and nothing at the destination.
func TestVDCrash_Seal(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 4 << 20, ClusterShift: 16, Profile: ProfileFast})
	if err != nil {
		t.Fatal(err)
	}
	c.WriteAt(pattern(3, 4<<20), 0)
	c.Close()
	before, _ := os.ReadFile(src)
	ctx, cancel := context.WithCancel(context.Background())
	dst := filepath.Join(dir, "sealed.fdd")
	err = Seal(ctx, src, dst, progressFunc(func(done, total int64) {
		if done == total/2 {
			cancel()
		}
	}))
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("a stopped seal: %v", err)
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
		t.Fatal("an interrupted seal changed its source")
	}
	for _, p := range []string{dst, dst + fsx.PartialSuffix} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left behind", p)
		}
	}
}

// TestVD_SealRefusesUnclean: a source not closed cleanly is not frozen.
func TestVD_SealRefusesUnclean(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 1 << 20, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	c.WriteAt(pattern(1, 100), 0)
	c.Abandon()
	if err := Seal(context.Background(), src, filepath.Join(dir, "s.fdd"), nil); !errors.Is(err, ErrUsage) {
		t.Fatalf("sealing an unclean source: %v", err)
	}
}

// A second process can claim the final name after the initial existence
// check. Neither verb may replace that file at publication time.
func TestVD_CopyPublicationDoesNotReplaceAnArrivingFile(t *testing.T) {
	for _, seal := range []bool{false, true} {
		dir := t.TempDir()
		src, dst := filepath.Join(dir, "src.fdd"), filepath.Join(dir, "dst.fdd")
		c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 1 << 20, ClusterShift: 16})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.WriteAt(pattern(7, 4096), 0); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		marker := []byte("another owner's file")
		p := progressFunc(func(done, total int64) {
			if done == 1 {
				if err := os.WriteFile(dst, marker, 0o600); err != nil {
					t.Fatal(err)
				}
			}
		})
		o := SealOptions{Src: src, Dst: dst, Progress: p}
		if seal {
			err = SealWith(context.Background(), o)
		} else {
			err = CopyWith(context.Background(), o)
		}
		if !errors.Is(err, ErrUsage) {
			t.Fatalf("seal=%v: arriving file returned %v", seal, err)
		}
		if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, marker) {
			t.Fatalf("seal=%v: arriving file changed: %q, %v", seal, got, err)
		}
		if left := sp64Partials(dst); len(left) != 0 {
			t.Fatalf("seal=%v: partial survived: %q", seal, left)
		}
	}
}

type progressFunc func(done, total int64)

func (f progressFunc) Progress(done, total int64) { f(done, total) }

// sp64Source is a small closed plain container with one written cluster.
func sp64Source(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "src.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 1 << 20, ClusterShift: 16})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteAt(pattern(7, 4096), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return src
}

// sp64Partials lists the files beside dst other than dst and the source.
func sp64Partials(dst string) []string {
	m, _ := filepath.Glob(dst + "?*")
	return m
}

// AUD-36-F3: the copy is written under the repo's partial name, which every
// copy verb already recognises, not under an ad hoc ".partial".
func TestVD_CopyPartialUsesTheSharedSuffix(t *testing.T) {
	dir := t.TempDir()
	src, dst := sp64Source(t, dir), filepath.Join(dir, "dst.fdd")
	var seen []string
	p := progressFunc(func(done, total int64) {
		if done == 1 {
			seen = sp64Partials(dst)
		}
	})
	if err := CopyWith(context.Background(), CopyOptions{Src: src, Dst: dst, Progress: p}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != dst+fsx.PartialSuffix {
		t.Fatalf("the copy was written under %q, want %q", seen, dst+fsx.PartialSuffix)
	}
	if left := sp64Partials(dst); len(left) != 0 {
		t.Fatalf("a partial survived a finished copy: %q", left)
	}
}

// AUD-36-F3: a file that appears at dst after every check, right before the
// publishing rename, is never replaced: usage error, bytes unchanged, the
// partial removed.
func TestVD_CopyFinalizeKeepsAFileThatArrivesAfterTheChecks(t *testing.T) {
	defer func(f func(tmp, dst string)) { copyBeforePublish = f }(copyBeforePublish)
	for _, seal := range []bool{false, true} {
		dir := t.TempDir()
		src, dst := sp64Source(t, dir), filepath.Join(dir, "dst.fdd")
		marker := []byte("another owner's file")
		copyBeforePublish = func(tmp, d string) {
			if err := os.WriteFile(d, marker, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		o := SealOptions{Src: src, Dst: dst}
		var err error
		if seal {
			err = SealWith(context.Background(), o)
		} else {
			err = CopyWith(context.Background(), o)
		}
		if !errors.Is(err, ErrUsage) {
			t.Fatalf("seal=%v: a file arriving before the rename returned %v", seal, err)
		}
		if got, err := os.ReadFile(dst); err != nil || !bytes.Equal(got, marker) {
			t.Fatalf("seal=%v: the arriving file changed: %q, %v", seal, got, err)
		}
		if left := sp64Partials(dst); len(left) != 0 {
			t.Fatalf("seal=%v: a partial survived: %q", seal, left)
		}
	}
}

// AUD-36-F3: a destination whose existence cannot be checked is not taken as
// absent: "could not check", class 5.
func TestVD_CopyRefusesADestinationItCannotCheck(t *testing.T) {
	dir := t.TempDir()
	src := sp64Source(t, dir)
	dst := filepath.Join(dir, "bad<name>.fdd")
	if _, err := os.Stat(dst); err == nil || os.IsNotExist(err) {
		t.Skipf("this system's Stat of %q does not fail with a not-exist-free error: %v", dst, err)
	}
	err := CopyWith(context.Background(), CopyOptions{Src: src, Dst: dst})
	if err == nil || ExitClass(err) != ExitIO || !strings.Contains(err.Error(), "could not check") {
		t.Fatalf("an unreadable destination returned class %d: %v", ExitClass(err), err)
	}
}

// AUD-36-F3: a partial the failed copy could not remove is named in the error.
func TestVD_CopyNamesAPartialItCouldNotRemove(t *testing.T) {
	dir := t.TempDir()
	src, dst := sp64Source(t, dir), filepath.Join(dir, "dst.fdd")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var held *os.File
	p := progressFunc(func(done, total int64) {
		if done == 1 {
			if m := sp64Partials(dst); len(m) == 1 {
				held, _ = os.Open(m[0]) // no FILE_SHARE_DELETE: the removal fails
			}
			cancel()
		}
	})
	err := CopyWith(ctx, CopyOptions{Src: src, Dst: dst, Progress: p})
	if held == nil {
		t.Fatal("the partial could not be held open")
	}
	name := held.Name()
	held.Close()
	defer os.Remove(name)
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("a stopped copy returned %v", err)
	}
	if !strings.Contains(err.Error(), name) {
		t.Fatalf("the error does not name the partial it left: %v", err)
	}
}
