package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRenameNoReplaceKeepsExisting is T4: the final step never overwrites.
func TestRenameNoReplaceKeepsExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new.tmp")
	dst := filepath.Join(dir, "final.bin")
	os.WriteFile(src, []byte("new"), 0o644)
	os.WriteFile(dst, []byte("old"), 0o644)

	err := renameNoReplace(src, dst)
	if !errors.Is(err, errDestinationExists) {
		t.Fatalf("renameNoReplace over an existing file: err = %v, want errDestinationExists", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old" {
		t.Fatalf("the existing destination changed: %q", got)
	}
	if !exists(src) {
		t.Fatal("the source must stay where it was when the rename is refused")
	}
	if err := renameReplace(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "new" {
		t.Fatalf("renameReplace did not replace: %q", got)
	}
}

// TestAtomicCopyFile covers B2: bytes, mtime, no replace, no partial left on
// failure or cancellation, and refusal of a copy onto itself.
func TestAtomicCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	payload := bytes.Repeat([]byte("0123456789abcdef"), 70000)
	os.WriteFile(src, payload, 0o644)
	old := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	os.Chtimes(src, old, old)

	dst := filepath.Join(dir, "dst.bin")
	if err := atomicCopyFile(context.Background(), src, dst, atomicCopyOptions{BufferSize: 4096}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); !bytes.Equal(got, payload) {
		t.Fatal("copied bytes differ")
	}
	if fi, _ := os.Stat(dst); !fi.ModTime().Equal(old) {
		t.Fatalf("mtime not preserved: %v", fi.ModTime())
	}
	if exists(dst + partialSuffix) {
		t.Fatal("a partial file was left after a successful copy")
	}

	// No replace by default.
	os.WriteFile(dst, []byte("keep me"), 0o644)
	if err := atomicCopyFile(context.Background(), src, dst, atomicCopyOptions{}); !errors.Is(err, errDestinationExists) {
		t.Fatalf("copy over an existing file: err = %v, want errDestinationExists", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "keep me" {
		t.Fatal("the existing destination was overwritten")
	}

	// Cancelled mid-way: nothing under the final name, no partial.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	dst2 := filepath.Join(dir, "dst2.bin")
	if err := atomicCopyFile(ctx, src, dst2, atomicCopyOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy: err = %v", err)
	}
	if exists(dst2) || exists(dst2+partialSuffix) {
		t.Fatal("a cancelled copy left a file behind")
	}

	// Onto itself, in another spelling.
	if err := atomicCopyFile(context.Background(), src, strings.ToUpper(src), atomicCopyOptions{Replace: true}); err == nil {
		t.Fatal("a copy onto itself must be refused")
	}
	if got, _ := os.ReadFile(src); !bytes.Equal(got, payload) {
		t.Fatal("the source changed during a refused self-copy")
	}
}
