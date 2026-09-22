package fdsec

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The abrupt-interruption simulation of stage S2 (tactics section 5): a
// writer that dies at a chosen byte offset. After every interruption point
// the partial output must fail as damaged or unsupported - never as a
// credential problem, and never silently pass for a complete container.

func TestInterrupt_NoCompleteLookingPartial(t *testing.T) {
	p := fastParams()
	content := make([]byte, 2*(int(p.ChunkSize)-tagSize)+5) // k = 3
	meta := fixedTimes
	cred := NewCredential("pw123")

	// Reference layout numbers from a clean pack.
	clean := packBytes(t, content, meta, cred, p)
	h, _, err := openHead(bytes.NewReader(clean), cred)
	if err != nil {
		t.Fatal(err)
	}
	preLen := h.preLen()
	total := int64(len(clean))

	points := map[string]int64{
		"before anything":      0,
		"mid-head":             40,
		"first chunk boundary": preLen,
		"mid-chunk":            preLen + 17,
		"one byte before done": total - 1,
	}
	for name, limit := range points {
		t.Run(name, func(t *testing.T) {
			w := &faultWriter{limit: limit}
			if _, err := Pack(w, bytes.NewReader(content), meta, cred, p); err == nil {
				t.Fatalf("Pack at limit %d reported success", limit)
			}
			partial := w.buf.Bytes()
			_, _, err := unpackContainer(partial, cred)
			if err == nil {
				t.Fatal("partial container unpacked as complete")
			}
			if errors.Is(err, ErrCredentialOrTamper) {
				t.Fatalf("partial reported as credential/tamper problem: %v", err)
			}
			if !errors.Is(err, ErrDamaged) && !errors.Is(err, ErrUnsupported) {
				t.Fatalf("partial reported neither damage nor refusal: %v", err)
			}
		})
	}
}

// PackFile: the file-level invariants - temp name + read-back + rename, no
// silent overwrite, no leftover partial on failure.
func TestPackFile(t *testing.T) {
	dir := t.TempDir()
	p := fastParams()
	cred := NewCredential("pw123")

	srcPath := filepath.Join(dir, "original.bin")
	src := bytes.Repeat([]byte{0x5A}, 3*(int(p.ChunkSize)-tagSize)+9)
	if err := os.WriteFile(srcPath, src, 0o600); err != nil {
		t.Fatal(err)
	}
	meta := fixedTimes
	meta.Size = int64(len(src))
	dstPath := filepath.Join(dir, "original.fd-sec")

	t.Run("happy path leaves only the final container", func(t *testing.T) {
		info, err := PackFile(dstPath, srcPath, meta, cred, p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(dstPath); err != nil {
			t.Fatalf("final container missing: %v", err)
		}
		leftovers, err := filepath.Glob(filepath.Join(dir, "*fdsec-partial*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(leftovers) != 0 {
			t.Fatalf("partial left behind: %v", leftovers)
		}
		// The read-back already verified the container; unpack once more to
		// prove the on-disk bytes are the round trip.
		f, err := os.Open(dstPath)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		var out bytes.Buffer
		m, err := Unpack(&out, f, cred)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out.Bytes(), src) {
			t.Fatal("on-disk container does not round trip")
		}
		if m.Size != int64(len(src)) {
			t.Fatalf("size %d, want %d", m.Size, len(src))
		}
		if info.TotalLen != int64(mustStatSize(t, dstPath)) {
			t.Fatalf("Info.TotalLen %d, file is %d", info.TotalLen, mustStatSize(t, dstPath))
		}
	})

	t.Run("refuses to overwrite", func(t *testing.T) {
		before := mustStatSize(t, dstPath)
		if _, err := PackFile(dstPath, srcPath, meta, cred, p); err == nil {
			t.Fatal("existing destination overwritten")
		}
		if mustStatSize(t, dstPath) != before {
			t.Fatal("destination changed despite refusal")
		}
	})

	t.Run("failure before rename leaves no container and no partial", func(t *testing.T) {
		badMeta := meta
		badMeta.Size = meta.Size + 1 // caught in pass 1, nothing written
		other := filepath.Join(dir, "never.fd-sec")
		if _, err := PackFile(other, srcPath, badMeta, cred, p); err == nil {
			t.Fatal("bad metadata accepted")
		}
		if _, err := os.Stat(other); !os.IsNotExist(err) {
			t.Fatalf("destination exists after failure: %v", err)
		}
		leftovers, _ := filepath.Glob(filepath.Join(dir, "*fdsec-partial*"))
		if len(leftovers) != 0 {
			t.Fatalf("partial left behind: %v", leftovers)
		}
	})
}

func mustStatSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}
