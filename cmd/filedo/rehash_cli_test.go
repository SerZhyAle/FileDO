package main

import (
	"bytes"
	"crypto/sha256"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeCLITestPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.SetNRGBA(1, 2, color.NRGBA{R: 120, G: 200, B: 80, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fileSHA256(t *testing.T, p string) [sha256.Size]byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

func TestRehashCLI_SingleFileAndTreeRepeated(t *testing.T) {
	dir, _ := workdir(t)
	srcDir := filepath.Join(dir, "src")
	nested := filepath.Join(srcDir, "sub", "deep")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	srcPNG := filepath.Join(srcDir, "photo.png")
	srcJSON := filepath.Join(nested, "meta.json")
	srcBin := filepath.Join(nested, "opaque.bin")
	if err := os.WriteFile(srcPNG, makeCLITestPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcJSON, []byte(`{"name":"filedo","count":3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(srcBin, []byte{0x00, 0x11, 0x22, 0x33}, 0o644); err != nil {
		t.Fatal(err)
	}
	binHash := fileSHA256(t, srcBin)

	// 1. A single file gets a new digest in place.
	before := fileSHA256(t, srcPNG)
	ev1 := filepath.Join(dir, "ev1.jsonl")
	out, code := run(t, dir, "--events", ev1, "rehash", srcPNG)
	if code != 0 {
		t.Fatalf("single rehash exited %d, want 0\n%s", code, out)
	}
	if v := lastResult(t, ev1)["verdict"]; v != "Done" {
		t.Fatalf("single rehash verdict = %v, want Done", v)
	}
	if fileSHA256(t, srcPNG) == before {
		t.Fatal("single rehash left the SHA-256 unchanged")
	}

	// 2. The same tree runs again and again; every run changes every supported
	// file and leaves the unsupported one alone.
	for i := 0; i < 3; i++ {
		pngBefore, jsonBefore := fileSHA256(t, srcPNG), fileSHA256(t, srcJSON)
		out, code = run(t, dir, "rehash", srcDir)
		if code != 0 {
			t.Fatalf("tree rehash %d exited %d, want 0\n%s", i, code, out)
		}
		if fileSHA256(t, srcPNG) == pngBefore || fileSHA256(t, srcJSON) == jsonBefore {
			t.Fatalf("tree rehash %d left a supported file unchanged\n%s", i, out)
		}
		if fileSHA256(t, srcBin) != binHash {
			t.Fatal("unsupported file was modified")
		}
		if !strings.Contains(out, "SKIPPED:") {
			t.Fatalf("expected SKIPPED reasons in output:\n%s", out)
		}
	}
	for folder, want := range map[string]int{srcDir: 2, nested: 2} {
		left, err := os.ReadDir(folder)
		if err != nil {
			t.Fatal(err)
		}
		if len(left) != want {
			t.Fatalf("%s holds %d entries, want %d", folder, len(left), want)
		}
	}
}

func TestRehashCLI_StopFileAndUsageGuards(t *testing.T) {
	dir, _ := workdir(t)
	src := filepath.Join(dir, "item.png")
	if err := os.WriteFile(src, makeCLITestPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	before := fileSHA256(t, src)

	// A second path, or none -> exit 10
	for _, args := range [][]string{
		{"rehash"},
		{"rehash", src, filepath.Join(dir, "copy.png")},
		{"rehash", src, "to", filepath.Join(dir, "copy.png")},
		{"rehash", os.Getenv("SystemRoot")},
	} {
		out, code := run(t, dir, args...)
		if code != 10 {
			t.Fatalf("%v exited %d, want 10\n%s", args, code, out)
		}
	}

	// Pre-existing stop file -> Stopped, exit 16, nothing changed
	stopFile := filepath.Join(dir, "stop.flag")
	if err := os.WriteFile(stopFile, []byte("stop"), 0o644); err != nil {
		t.Fatal(err)
	}
	ev := filepath.Join(dir, "ev-stop.jsonl")
	out, code := run(t, dir, "--stop-file", stopFile, "--events", ev, "rehash", src)
	if code != 16 {
		t.Fatalf("stopped rehash exited %d, want 16\n%s", code, out)
	}
	if v := lastResult(t, ev)["verdict"]; v != "Stopped" {
		t.Fatalf("stopped rehash verdict = %v, want Stopped", v)
	}
	if fileSHA256(t, src) != before {
		t.Fatal("stopped rehash changed the file")
	}
	if _, err := os.Lstat(src + ".filedo-partial"); !os.IsNotExist(err) {
		t.Fatal("stopped rehash left partial file")
	}
}

func TestRehashCLI_BatchList(t *testing.T) {
	dir, _ := workdir(t)
	src := filepath.Join(dir, "batch.json")
	if err := os.WriteFile(src, []byte(`{"batch":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	before := fileSHA256(t, src)
	lst := filepath.Join(dir, "run.lst")
	if err := os.WriteFile(lst, []byte("rehash \""+src+"\"\nrehash \""+src+"\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code := run(t, dir, "from", lst)
	if code != 0 {
		t.Fatalf("batch rehash exited %d, want 0\n%s", code, out)
	}
	if fileSHA256(t, src) == before {
		t.Fatal("batch rehash left the SHA-256 unchanged")
	}
}
