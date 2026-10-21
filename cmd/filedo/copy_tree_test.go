package main

// The plain copy walks the source tree once (SP-0002 item 5): every directory
// is created and every file copied as the walk reaches it, and a counting walk
// runs first only when the command line asks for --precount. These tests hold
// both halves - the number of walks, and that the copy is still complete and
// byte-exact either way - and then drive the built exe through the target-first
// grammar, from a .lst batch file first, because that is the path a verb can
// silently miss (AGENTS.md "Testing").

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copySourceTree writes a small nested tree and returns its root together with
// the relative path and bytes of every file in it.
func copySourceTree(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "src")
	files := map[string][]byte{
		"top.txt":                          []byte("top level"),
		filepath.Join("a", "one.bin"):      bytes.Repeat([]byte{0x5a, 0x01}, 30000),
		filepath.Join("a", "b", "two.txt"): []byte("two levels down"),
		filepath.Join("c", "empty.txt"):    {},
	}
	for rel, body := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "d", "no-files"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, files
}

func assertTreeCopied(t *testing.T, dst string, files map[string][]byte) {
	t.Helper()
	for rel, want := range files {
		got, err := os.ReadFile(filepath.Join(dst, rel))
		if err != nil {
			t.Errorf("%s was not copied: %v", rel, err)
			continue
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s is not byte-exact: %d bytes, want %d", rel, len(got), len(want))
		}
	}
	if info, err := os.Stat(filepath.Join(dst, "d", "no-files")); err != nil || !info.IsDir() {
		t.Errorf("the empty directory d\\no-files was not recreated: %v", err)
	}
}

func TestPlainCopyWalksTheTreeOnce(t *testing.T) {
	cases := []struct {
		name     string
		precount bool
		walks    int
	}{
		{"default copies during the one walk", false, 1},
		{"--precount adds exactly one counting walk", true, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src, files := copySourceTree(t)
			dst := filepath.Join(t.TempDir(), "dst")

			walks := 0
			realWalk := copyWalk
			copyWalk = func(root string, fn filepath.WalkFunc) error {
				walks++
				return realWalk(root, fn)
			}
			prev := copyPrecount
			copyPrecount = c.precount
			t.Cleanup(func() { copyWalk = realWalk; copyPrecount = prev })

			if err := copyDirectory(src, dst); err != nil {
				t.Fatalf("copyDirectory: %v", err)
			}
			if walks != c.walks {
				t.Errorf("the copy walked the source tree %d times, want %d", walks, c.walks)
			}
			assertTreeCopied(t, dst, files)
		})
	}
}

func TestCopyProgressSaysWhenTotalsAreStillGrowing(t *testing.T) {
	p := &CopyProgress{}
	p.AddTotalFiles(3)
	p.AddTotalSize(3 * 1024 * 1024)

	if got := p.countsText(1, 1024*1024); !strings.Contains(got, "found so far") {
		t.Errorf("uncounted totals are shown as final: %q", got)
	}
	if got := p.etaText(1024 * 1024); got != "unknown" {
		t.Errorf("an ETA against a partial total was given: %q", got)
	}

	p.TotalsKnown = true
	if got := p.countsText(1, 1024*1024); strings.Contains(got, "found so far") || !strings.Contains(got, "1/3 files") {
		t.Errorf("counted totals are not shown as done/total: %q", got)
	}
}

func TestTargetFirstCopyFromBatchFileAndInteractively(t *testing.T) {
	src, files := copySourceTree(t)
	dir := t.TempDir()
	lst := strings.Join([]string{
		"folder " + src + " copy " + filepath.Join(dir, "batch-plain"),
		"folder " + src + " copy " + filepath.Join(dir, "batch-precount") + " --precount",
		// The line after a --precount line must not inherit it.
		"folder " + src + " copy " + filepath.Join(dir, "batch-after"),
	}, "\r\n")
	if err := os.WriteFile(filepath.Join(dir, "copy.lst"), []byte(lst), 0o644); err != nil {
		t.Fatal(err)
	}

	out, code := run(t, dir, "from", "copy.lst")
	if code != 0 {
		t.Fatalf("batch copy exited %d, want 0\n%s", code, out)
	}
	if !strings.Contains(out, "3/3 commands succeeded") {
		t.Errorf("not every batch copy succeeded\n%s", out)
	}
	for _, d := range []string{"batch-plain", "batch-precount", "batch-after"} {
		assertTreeCopied(t, filepath.Join(dir, d), files)
	}
	if n := strings.Count(out, "Counting files first (--precount)"); n != 1 {
		t.Errorf("--precount counted first on %d batch lines, want exactly the one that asked\n%s", n, out)
	}
	if n := strings.Count(out, "totals grow as files are found"); n != 2 {
		t.Errorf("%d batch lines copied during the walk, want 2\n%s", n, out)
	}

	out, code = run(t, dir, "folder", src, "copy", filepath.Join(dir, "typed"), "--precount")
	if code != 0 {
		t.Fatalf("interactive copy exited %d, want 0\n%s", code, out)
	}
	assertTreeCopied(t, filepath.Join(dir, "typed"), files)
	if !strings.Contains(out, "Found 4 files") {
		t.Errorf("--precount did not report the counted totals up front\n%s", out)
	}
}

// The parallel copy keeps its progress counters in FastCopyProgress, and a
// 32-bit build panicked on the first small-file batch while one of them sat
// behind a time.Time. The exe the suite builds is whatever GOARCH the host
// toolchain has, so on a 386 toolchain this is the regression test.
func TestFastCopyCompletesTheTree(t *testing.T) {
	src, files := copySourceTree(t)
	dir := t.TempDir()
	out, code := run(t, dir, "fastcopy", src, filepath.Join(dir, "fast"))
	if code != 0 {
		t.Fatalf("fastcopy exited %d, want 0\n%s", code, out)
	}
	if strings.Contains(out, "panic") {
		t.Fatalf("fastcopy panicked\n%s", out)
	}
	assertTreeCopied(t, filepath.Join(dir, "fast"), files)
}
