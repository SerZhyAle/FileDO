package fdsec

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestPackReadBackObservesStop is AUD-11-F2 (FDSEC-02): a stop that lands in
// the read-back ends the pack with ErrStopped and leaves no container and no
// temporary sibling. The read-back used to run to its end with no stop, the
// container was renamed into place, and the run said "Stopped" for work that
// had completed.
func TestPackReadBackObservesStop(t *testing.T) {
	cred := NewCredential("pw")
	payload := make([]byte, 300000)
	for i := range payload {
		payload[i] = byte(i * 7)
	}
	packs := map[string]func(dir, dst string, stop func() bool) error{
		"PackFile": func(dir, dst string, stop func() bool) error {
			src := filepath.Join(dir, "original.bin")
			if err := os.WriteFile(src, payload, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := PackFile(dst, src, Metadata{Name: "original.bin", Size: int64(len(payload))}, cred, Params{}, WithStop(stop))
			return err
		},
		"PackFileSuite2": func(dir, dst string, stop func() bool) error {
			src := filepath.Join(dir, "original.bin")
			if err := os.WriteFile(src, payload, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := PackFileSuite2(dst, src, Metadata{Name: "original.bin", Size: int64(len(payload))}, cred, WithStop(stop))
			return err
		},
		"PackTreeFile": func(dir, dst string, stop func() bool) error {
			items, order := sampleItems()
			src := filepath.Join(dir, "src")
			writeDiskTree(t, src, items, order)
			tree, err := ScanTree(src, ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = PackTreeFile(dst, tree, cred, fastParams(), WithStop(stop))
			return err
		},
	}
	for name, pack := range packs {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			dst := filepath.Join(dir, "out.fd-sec")
			var stopped atomic.Bool
			reached := false
			beforeReadBack = func(string) { reached = true; stopped.Store(true) }
			defer func() { beforeReadBack = nil }()
			err := pack(dir, dst, stopped.Load)
			if !reached {
				t.Fatal("the read-back seam was never reached")
			}
			if !errors.Is(err, ErrStopped) {
				t.Fatalf("a stop in the read-back returned %v, want ErrStopped", err)
			}
			if _, err := os.Stat(dst); !os.IsNotExist(err) {
				t.Fatalf("a container was left at %s after a stop in the read-back", dst)
			}
			if left, _ := filepath.Glob(filepath.Join(dir, "*.fdsec-partial-*")); len(left) != 0 {
				t.Fatalf("a stopped pack left %v", left)
			}
		})
	}
}
