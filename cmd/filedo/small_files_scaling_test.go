package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// SP-0124 T5 (a): Scaling proof for small files in performDelete and check.
// Verifies that processing 4000 tiny files scales linearly (at most 6x the time of 1000 files).
func TestSmallFilesScaling_CompareDelete(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}
	runBench := func(n int) time.Duration {
		src := filepath.Join(t.TempDir(), "src")
		dst := filepath.Join(t.TempDir(), "dst")
		_ = os.MkdirAll(src, 0o755)
		_ = os.MkdirAll(dst, 0o755)
		now := time.Now().Truncate(time.Second)
		for i := 0; i < n; i++ {
			name := fmt.Sprintf("f%05d.dat", i)
			sp := filepath.Join(src, name)
			dp := filepath.Join(dst, name)
			data := []byte("a")
			_ = os.WriteFile(sp, data, 0o644)
			_ = os.WriteFile(dp, data, 0o644)
			_ = os.Chtimes(sp, now, now)
			_ = os.Chtimes(dp, now, now)
		}
		srcScan := scanFiles(src)
		dstScan := scanFiles(dst)
		opts := compareOptions{assumeYes: true}
		t0 := time.Now()
		performDelete(src, dst, "source", "", opts, srcScan, dstScan)
		return time.Since(t0)
	}

	d1000 := runBench(1000)
	d4000 := runBench(4000)
	t.Logf("performDelete 1000: %v, 4000: %v", d1000, d4000)
	if d1000 > 0 && d4000 > 6*d1000 && d4000 > 5*time.Second {
		t.Errorf("performDelete scaled superlinearly: 1000=%v, 4000=%v (want <= 6x)", d1000, d4000)
	}
}

func TestSmallFilesScaling_Check(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}
	runCheckBench := func(n int) time.Duration {
		tree := checkTree(t, n)
		wd := t.TempDir()
		t0 := time.Now()
		out, code := run(t, wd, "check", tree, "--quiet")
		if code != 0 {
			t.Fatalf("check of %d files exited %d: %s", n, code, out)
		}
		return time.Since(t0)
	}

	d1000 := runCheckBench(1000)
	d4000 := runCheckBench(4000)
	t.Logf("check 1000: %v, 4000: %v", d1000, d4000)
	if d1000 > 0 && d4000 > 6*d1000 && d4000 > 5*time.Second {
		t.Errorf("check scaled superlinearly: 1000=%v, 4000=%v (want <= 6x)", d1000, d4000)
	}
}
