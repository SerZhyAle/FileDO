package rehash

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeSampleImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 30),
				G: uint8(y * 40),
				B: uint8((x + y) * 15),
				A: 255,
			})
		}
	}
	return img
}

func makePNGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, makeSampleImage()); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeJPEGBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, makeSampleImage(), &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeGIF89aBytes(t *testing.T) []byte {
	t.Helper()
	pal := color.Palette{
		color.RGBA{0, 0, 0, 255},
		color.RGBA{255, 0, 0, 255},
		color.RGBA{0, 255, 0, 255},
		color.RGBA{0, 0, 255, 255},
	}
	f1 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	f2 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	f1.SetColorIndex(1, 1, 1)
	f2.SetColorIndex(2, 2, 2)
	var buf bytes.Buffer
	err := gif.EncodeAll(&buf, &gif.GIF{
		Image:     []*image.Paletted{f1, f2},
		Delay:     []int{10, 20},
		LoopCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeZIPBytes(t *testing.T, entries map[string][]byte, comment string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if comment != "" {
		if err := zw.SetComment(comment); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func makeWAVBytes() []byte {
	var body bytes.Buffer
	body.WriteString("WAVE")
	// fmt chunk (PCM 16-bit mono 8000 Hz)
	body.WriteString("fmt ")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], 16)
	body.Write(sz[:])
	fmtData := []byte{
		1, 0, // PCM
		1, 0, // 1 channel
		0x40, 0x1F, 0, 0, // 8000 Hz
		0x80, 0x3E, 0, 0, // byte rate 16000
		2, 0, // block align
		16, 0, // bits per sample
	}
	body.Write(fmtData)
	// data chunk (4 PCM samples = 8 bytes)
	body.WriteString("data")
	binary.LittleEndian.PutUint32(sz[:], 8)
	body.Write(sz[:])
	body.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})

	var out bytes.Buffer
	out.WriteString("RIFF")
	binary.LittleEndian.PutUint32(sz[:], uint32(body.Len()))
	out.Write(sz[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

func makeMP4Bytes() []byte {
	box := func(typ string, payload []byte) []byte {
		b := make([]byte, 8+len(payload))
		binary.BigEndian.PutUint32(b[0:4], uint32(len(b)))
		copy(b[4:8], typ)
		copy(b[8:], payload)
		return b
	}
	var out bytes.Buffer
	out.Write(box("ftyp", []byte("isom\x00\x00\x02\x00isommp41")))
	out.Write(box("moov", []byte("synthetic-moov-data")))
	out.Write(box("mdat", []byte("synthetic-media-bytes")))
	return out.Bytes()
}

func makeFLACBytes() []byte {
	var out bytes.Buffer
	out.WriteString("fLaC")
	// Final STREAMINFO metadata block: isLast=1, type=0, length=34
	out.Write([]byte{0x80, 0x00, 0x00, 34})
	out.Write(make([]byte, 34))
	// Synthetic audio frame starting with FLAC sync code 0xFFF8
	out.Write([]byte{0xFF, 0xF8, 0x69, 0x02, 0x00, 0x00})
	return out.Bytes()
}

func TestTransformFile_AllSupportedFormats(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name string
		data []byte
		want FormatSpec
	}{
		{"sample.png", makePNGBytes(t), pngHandler{}.spec()},
		{"sample.jpg", makeJPEGBytes(t), jpegHandler{}.spec()},
		{"sample.gif", makeGIF89aBytes(t), gifHandler{}.spec()},
		{"sample.zip", makeZIPBytes(t, map[string][]byte{"a.txt": []byte("hello world")}, ""), zipHandler{}.spec()},
		{"commented.docx", makeZIPBytes(t, map[string][]byte{"word/document.xml": []byte("<doc/>")}, "prior note"), zipHandler{}.spec()},
		{"sample.wav", makeWAVBytes(), wavHandler{}.spec()},
		{"sample.mp4", makeMP4Bytes(), mp4Handler{}.spec()},
		{"sample.flac", makeFLACBytes(), flacHandler{}.spec()},
		{"sample.json", []byte(`{"id":42,"ok":true,"items":[1,2,3]}`), jsonHandler{}.spec()},
		{"sample.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect width="4" height="4"/></svg>`), xmlHandler{}.spec()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(dir, "in-"+tc.name)
			dst := filepath.Join(dir, "out", tc.name)
			if err := os.WriteFile(src, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			origSum := sha256.Sum256(tc.data)

			res, err := transformCopy(src, dst)
			if err != nil {
				t.Fatalf("transformCopy(%s) failed: %v", tc.name, err)
			}
			if res.Format != tc.want.Name {
				t.Fatalf("format = %q, want %q", res.Format, tc.want.Name)
			}
			if res.OriginalSHA256 != origSum {
				t.Fatalf("original hash mismatch")
			}
			if res.OutputSHA256 == res.OriginalSHA256 {
				t.Fatalf("output SHA-256 equals original")
			}
			if res.SizeDelta < tc.want.MinDelta || res.SizeDelta > tc.want.MaxDelta {
				t.Fatalf("size delta = %d, want [%d..%d]", res.SizeDelta, tc.want.MinDelta, tc.want.MaxDelta)
			}

			// Verify output file on disk matches reported digest and size.
			outBytes, err := os.ReadFile(dst)
			if err != nil {
				t.Fatal(err)
			}
			if int64(len(outBytes)) != res.OutputSize || sha256.Sum256(outBytes) != res.OutputSHA256 {
				t.Fatalf("output file on disk does not match reported result")
			}

			// Re-running on the output itself produces a third distinct SHA-256.
			dst2 := filepath.Join(dir, "out2", tc.name)
			res2, err := transformCopy(dst, dst2)
			if err != nil {
				t.Fatalf("second transformCopy(%s) failed: %v", tc.name, err)
			}
			if res2.OutputSHA256 == res.OutputSHA256 || res2.OutputSHA256 == origSum {
				t.Fatalf("second rehash did not produce a distinct SHA-256")
			}
		})
	}
}

func TestTransformFile_SkipsUnknownCorruptSignedAndReadOnly(t *testing.T) {
	dir := t.TempDir()

	signedDocx := makeZIPBytes(t, map[string][]byte{
		"word/document.xml":       []byte("<doc/>"),
		"_xmlsignatures/sig1.xml": []byte("<Signature/>"),
	}, "")
	signedJarZip := makeZIPBytes(t, map[string][]byte{
		"a.txt":            []byte("x"),
		"META-INF/CERT.SF": []byte("sig"),
	}, "")
	pngWithTrailing := append(makePNGBytes(t), []byte("trailing")...)
	gif87a := append([]byte("GIF87a"), makeGIF89aBytes(t)[6:]...)
	signedXML := []byte(`<root><Signature xmlns="http://www.w3.org/2000/09/xmldsig#"/></root>`)

	skips := []struct {
		name     string
		data     []byte
		wantCode string
	}{
		{"opaque.bin", []byte{0x00, 0x01, 0x02, 0x03}, "unsupported-format"},
		{"app.exe", []byte("MZ\x90\x00"), "unsupported-format"},
		{"signed.docx", signedDocx, "signed-file"},
		{"signed.zip", signedJarZip, "signed-file"},
		{"signed.xml", signedXML, "signed-file"},
		{"trailing.png", pngWithTrailing, "invalid-format"},
		{"bad.jpg", []byte("not a jpeg"), "invalid-format"},
		{"old.gif", gif87a, "unsupported-format"},
		{"ndjson.json", []byte("{\"a\":1}\n{\"b\":2}\n"), "invalid-format"},
		{"bom.json", append([]byte{0xEF, 0xBB, 0xBF}, []byte(`{"a":1}`)...), "invalid-format"},
	}

	for _, tc := range skips {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, "skip-"+tc.name)
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := RewriteFile(context.Background(), path)
			se, ok := IsSkip(err)
			if !ok {
				t.Fatalf("RewriteFile(%s) err = %v, want SkipError", tc.name, err)
			}
			if se.Code != tc.wantCode {
				t.Fatalf("skip code = %q, want %q (%s)", se.Code, tc.wantCode, se.Reason)
			}
			assertUntouched(t, path, tc.data)
		})
	}

	// A read-only file is left as it is.
	data := makePNGBytes(t)
	path := filepath.Join(dir, "locked.png")
	if err := os.WriteFile(path, data, 0o444); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(path, 0o644)
	_, err := RewriteFile(context.Background(), path)
	if se, ok := IsSkip(err); !ok || se.Code != "read-only" {
		t.Fatalf("read-only err = %v, want read-only skip", err)
	}
	assertUntouched(t, path, data)
}

func assertUntouched(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s was modified", path)
	}
	if _, err := os.Lstat(path + ".filedo-partial"); !os.IsNotExist(err) {
		t.Fatalf("%s: partial file left behind", path)
	}
}

// transformCopy rewrites a copy of src placed at dst, so a test can compare
// the two.
func transformCopy(src, dst string) (Result, error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return Result{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return Result{}, err
	}
	return RewriteFile(context.Background(), dst)
}

func TestRewriteFile_RepeatsInPlaceAndLeavesOnlyTheFile(t *testing.T) {
	dir := t.TempDir()
	full := strings.Repeat("n", 100) + strings.Repeat("\n"+tagPrefix+"0123456789abcdef", 2110)
	files := map[string][]byte{
		"doc.pdf":  makeClassicPDF(""),
		"full.zip": makeZIPBytes(t, map[string][]byte{"a.txt": []byte("x")}, full),
		"pic.png":  makePNGBytes(t),
	}
	for name, data := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		seen := map[[sha256.Size]byte]bool{sha256.Sum256(data): true}
		for run := 0; run < 5; run++ {
			res, err := RewriteFile(context.Background(), path)
			if err != nil {
				t.Fatalf("%s run %d: %v", name, run, err)
			}
			if seen[res.OutputSHA256] {
				t.Fatalf("%s run %d repeated a digest", name, run)
			}
			seen[res.OutputSHA256] = true
		}
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != len(files) {
		t.Fatalf("folder holds %d entries, want %d", len(left), len(files))
	}
}

func TestRewriteFile_CancelledContextChangesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.png")
	data := makePNGBytes(t)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RewriteFile(ctx, path); err == nil {
		t.Fatal("expected cancelled context error")
	}
	assertUntouched(t, path, data)
}
