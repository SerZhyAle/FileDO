package rehash

import (
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func be32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func le16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }

func isoBox(typ string, payload []byte) []byte {
	return append(append(be32(uint32(8+len(payload))), typ...), payload...)
}

func riffChunkBytes(id string, payload []byte, order binary.ByteOrder) []byte {
	b := make([]byte, 8, 8+len(payload)+1)
	copy(b, id)
	order.PutUint32(b[4:8], uint32(len(payload)))
	b = append(b, payload...)
	if len(payload)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

// makeMPFJPEG builds a primary JPEG with an APP2 MPF directory that points at
// a second JPEG appended after the primary EOI, as iPhone and Ultra HDR do.
func makeMPFJPEG(t *testing.T) []byte {
	t.Helper()
	primary := makeJPEGBytes(t)
	second := makeJPEGBytes(t)
	// MPF payload: "MPF\0" + TIFF header (8) + IFD (2 + 3*12 + 4) + 2 entries (32).
	const ifdLen = 2 + 3*12 + 4
	entriesOff := uint32(8 + ifdLen)
	mpf := []byte("MPF\x00II*\x00")
	mpf = append(mpf, le32(8)...)
	mpf = append(mpf, le16(3)...)
	mpf = append(mpf, le16(0xB000)...)
	mpf = append(mpf, le16(7)...)
	mpf = append(mpf, le32(4)...)
	mpf = append(mpf, "0100"...)
	mpf = append(mpf, le16(0xB001)...)
	mpf = append(mpf, le16(4)...)
	mpf = append(mpf, le32(1)...)
	mpf = append(mpf, le32(2)...)
	mpf = append(mpf, le16(0xB002)...)
	mpf = append(mpf, le16(7)...)
	mpf = append(mpf, le32(32)...)
	mpf = append(mpf, le32(entriesOff)...)
	mpf = append(mpf, le32(0)...)
	segLen := 2 + len(mpf) + 32
	primaryLen := len(primary) + 2 + segLen
	const base = 2 + 4 + 4 // SOI + APP2 marker/length + "MPF\0"
	entries := append(le32(0x030000), le32(uint32(primaryLen))...)
	entries = append(entries, le32(0)...)
	entries = append(entries, 0, 0, 0, 0)
	entries = append(entries, le32(0)...)
	entries = append(entries, le32(uint32(len(second)))...)
	entries = append(entries, le32(uint32(primaryLen-base))...)
	entries = append(entries, 0, 0, 0, 0)
	seg := append([]byte{0xFF, 0xE2, byte(segLen >> 8), byte(segLen)}, mpf...)
	seg = append(seg, entries...)
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, primary[2:]...)
	if len(out) != primaryLen {
		t.Fatalf("MPF sample primary length %d, want %d", len(out), primaryLen)
	}
	return append(out, second...)
}

func makeMotionPhotoJPEG(t *testing.T) []byte {
	t.Helper()
	jpg := makeJPEGBytes(t)
	xmp := append([]byte("http://ns.adobe.com/xap/1.0/\x00"), `<x:xmpmeta><rdf:Description GCamera:MotionPhoto="1"/></x:xmpmeta>`...)
	n := 2 + len(xmp)
	seg := append([]byte{0xFF, 0xE1, byte(n >> 8), byte(n)}, xmp...)
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, jpg[2:]...)
	return append(out, makeMP4Bytes()...)
}

func makeBMPBytes() []byte {
	pixels := make([]byte, 16) // 2x2, 24-bit, rows padded to 8 bytes
	h := []byte("BM")
	h = append(h, le32(uint32(14+40+len(pixels)))...)
	h = append(h, 0, 0, 0, 0)
	h = append(h, le32(54)...)
	h = append(h, le32(40)...)
	h = append(h, le32(2)...)
	h = append(h, le32(2)...)
	h = append(h, le16(1)...)
	h = append(h, le16(24)...)
	h = append(h, le32(0)...)
	h = append(h, le32(uint32(len(pixels)))...)
	h = append(h, make([]byte, 16)...)
	return append(h, pixels...)
}

func makeWebPBytes(extended bool) []byte {
	v := uint32(3) | uint32(3)<<14 // 4x4, no alpha
	vp8l := riffChunkBytes("VP8L", append([]byte{0x2F}, append(le32(v), 0, 0, 0, 0, 0)...), binary.LittleEndian)
	body := []byte("WEBP")
	if extended {
		x := make([]byte, 10)
		x[4], x[7] = 3, 3
		body = append(body, riffChunkBytes("VP8X", x, binary.LittleEndian)...)
	}
	body = append(body, vp8l...)
	return append(append([]byte("RIFF"), le32(uint32(len(body)))...), body...)
}

func makeTIFFBytes() []byte {
	b := []byte("II*\x00")
	b = append(b, le32(8)...)
	b = append(b, le16(1)...)
	b = append(b, le16(256)...) // ImageWidth
	b = append(b, le16(3)...)   // SHORT
	b = append(b, le32(1)...)
	b = append(b, le32(4)...)
	return append(b, le32(0)...)
}

func makeClassicPDF(extraTrailer string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	o1 := b.Len()
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	o2 := b.Len()
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	x := b.Len()
	fmt.Fprintf(&b, "xref\n0 3\n0000000000 65535 f\r\n%010d 00000 n\r\n%010d 00000 n\r\n", o1, o2)
	fmt.Fprintf(&b, "trailer\n<< /Size 3 /Root 1 0 R /ID [<0123abcd><0123abcd>]%s >>\nstartxref\n%d\n%%%%EOF\n", extraTrailer, x)
	return b.Bytes()
}

// makeStreamPDF builds a PDF whose only xref section is a Flate xref stream
// with the PNG Up predictor, as current PDF writers produce.
func makeStreamPDF(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("%PDF-1.5\n")
	o1 := b.Len()
	b.WriteString("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n")
	o2 := b.Len()
	b.WriteString("2 0 obj\n<< /Type /Pages /Kids [] /Count 0 >>\nendobj\n")
	o3 := b.Len()
	rows := [][]byte{{0, 0, 0, 0, 0, 0xFF}, {1, 0, 0, 0, byte(o1), 0}, {1, 0, 0, 0, byte(o2), 0}, {1, 0, 0, 0, byte(o3), 0}}
	var raw []byte
	prev := make([]byte, 6)
	for _, r := range rows {
		raw = append(raw, 2)
		for i := range r {
			raw = append(raw, r[i]-prev[i])
		}
		prev = r
	}
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(raw)
	zw.Close()
	fmt.Fprintf(&b, "3 0 obj\n<< /Type /XRef /Size 4 /W [1 4 1] /Root 1 0 R /Filter /FlateDecode /DecodeParms << /Predictor 12 /Columns 6 >> /Length %d >>\nstream\n", z.Len())
	b.Write(z.Bytes())
	fmt.Fprintf(&b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", o3)
	return b.Bytes()
}

var mp3Frame = append([]byte{0xFF, 0xFB, 0x90, 0x64}, make([]byte, 60)...)

func makeID3MP3() []byte {
	frame := append([]byte("TIT2"), be32(6)...)
	frame = append(frame, 0, 0, 0)
	frame = append(frame, "Title"...)
	tag := append(frame, make([]byte, 20)...) // padding
	h := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, byte(len(tag))}
	return append(append(h, tag...), mp3Frame...)
}

func makeAIFFBytes() []byte {
	comm := make([]byte, 18)
	comm[1] = 1 // one channel
	ssnd := make([]byte, 12)
	body := append([]byte("AIFF"), riffChunkBytes("COMM", comm, binary.BigEndian)...)
	body = append(body, riffChunkBytes("SSND", ssnd, binary.BigEndian)...)
	return append(append([]byte("FORM"), be32(uint32(len(body)))...), body...)
}

func makeMKVBytes(unknownSize bool) []byte {
	hdr := []byte{0x42, 0x82, 0x84}
	hdr = append(hdr, "webm"...)
	ebml := append([]byte{0x1A, 0x45, 0xDF, 0xA3, 0x80 | byte(len(hdr))}, hdr...)
	info := []byte{0x15, 0x49, 0xA9, 0x66, 0x83, 0x2A, 0xD7, 0xB1}
	cluster := []byte{0x1F, 0x43, 0xB6, 0x75, 0x82, 0xE7, 0x80}
	children := append(info, cluster...)
	seg := []byte{0x18, 0x53, 0x80, 0x67}
	if unknownSize {
		seg = append(seg, 0x01, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	} else {
		seg = append(seg, 0x01, 0, 0, 0, 0, 0, 0, byte(len(children)))
	}
	return append(append(ebml, seg...), children...)
}

func makeAVIBytes() []byte {
	body := append([]byte("AVI "), riffChunkBytes("LIST", []byte("hdrl"), binary.LittleEndian)...)
	body = append(body, riffChunkBytes("LIST", []byte("movi"), binary.LittleEndian)...)
	return append(append([]byte("RIFF"), le32(uint32(len(body)))...), body...)
}

func makeTSBytes(m2ts bool) []byte {
	var out []byte
	for i := 0; i < 3; i++ {
		if m2ts {
			out = append(out, 0, 0, 0, byte(i))
		}
		p := make([]byte, 188)
		p[0], p[1], p[2], p[3] = 0x47, 0x00, 0x11, 0x10
		out = append(out, p...)
	}
	return out
}

func mpeg2Pack() []byte {
	return []byte{0, 0, 1, 0xBA, 0x44, 0, 4, 0, 4, 1, 0, 0, 3, 0xF8}
}

func makePSBytes(sector bool) []byte {
	out := mpeg2Pack()
	n := 4
	if sector {
		n = 2048 - len(out) - 6
	}
	out = append(out, 0, 0, 1, 0xE0, byte(n>>8), byte(n))
	out = append(out, make([]byte, n)...)
	if !sector {
		out = append(out, 0, 0, 1, 0xB9)
	}
	return out
}

func utf16LE(s string) []byte {
	b := []byte{0xFF, 0xFE}
	for _, r := range s {
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

func transformOnce(t *testing.T, dir, name string, data []byte) (Result, []byte) {
	t.Helper()
	src := filepath.Join(dir, "in-"+name)
	dst := filepath.Join(dir, "out", name)
	if err := os.WriteFile(src, data, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := transformCopy(src, dst)
	if err != nil {
		t.Fatalf("transformCopy(%s) failed: %v", name, err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if res.OriginalSHA256 != sha256.Sum256(data) || res.OutputSHA256 != sha256.Sum256(out) || res.OutputSHA256 == res.OriginalSHA256 {
		t.Fatalf("%s: reported digests do not match the files", name)
	}
	spec, _ := FormatForExtension(filepath.Ext(name))
	if res.SizeDelta < spec.MinDelta || res.SizeDelta > spec.MaxDelta || res.Format != spec.Name {
		t.Fatalf("%s: format %s delta %d outside [%d..%d]", name, res.Format, res.SizeDelta, spec.MinDelta, spec.MaxDelta)
	}
	// The copy is itself a valid input: a second pass gives a third digest.
	dst2 := filepath.Join(dir, "out2", name)
	res2, err := transformCopy(dst, dst2)
	if err != nil {
		t.Fatalf("second transformCopy(%s) failed: %v", name, err)
	}
	if res2.OutputSHA256 == res.OutputSHA256 || res2.OutputSHA256 == res.OriginalSHA256 {
		t.Fatalf("%s: second pass did not give a distinct digest", name)
	}
	return res, out
}

func TestTransformFile_MoreFormats(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"mpf.jpg", makeMPFJPEG(t)},
		{"motion.jpg", makeMotionPhotoJPEG(t)},
		{"padded.jpg", append(makeJPEGBytes(t), 0, 0, 0, 0)},
		{"samsung.jpeg", append(makeJPEGBytes(t), []byte("\x00\x00SEFH....SEFT")...)},
		{"photo.heic", append(append(isoBox("ftyp", []byte("heic\x00\x00\x00\x00mif1heic")), isoBox("meta", []byte("x"))...), isoBox("mdat", []byte("img"))...)},
		{"photo.avif", append(append(isoBox("ftyp", []byte("avif\x00\x00\x00\x00mif1avif")), isoBox("meta", []byte("x"))...), isoBox("mdat", []byte("img"))...)},
		{"clip.3gp", makeMP4Bytes()},
		{"simple.webp", makeWebPBytes(false)},
		{"extended.webp", makeWebPBytes(true)},
		{"image.bmp", makeBMPBytes()},
		{"scan.tif", makeTIFFBytes()},
		{"raw.dng", makeTIFFBytes()},
		{"table.pdf", makeClassicPDF("")},
		{"stream.pdf", makeStreamPDF(t)},
		{"letter.rtf", []byte(`{\rtf1\ansi Hello {\b bold} \{x\} \bin3 {}}` + "}\r\n")},
		{"notes.txt", []byte("hello\r\nworld")},
		{"wide.txt", utf16LE("hello\r\n")},
		{"app.log", []byte("line one\n")},
		{"readme.md", []byte("# Title\n\n```\ncode\n```\ntext\n")},
		{"page.html", []byte("<!doctype html><html><body>x</body></html>")},
		{"site.css", []byte("body{margin:0}\n")},
		{"app.js", []byte("console.log(1) // end")},
		{"conf.ini", []byte("[a]\r\nk=v\r\n")},
		{"tagged.mp3", makeID3MP3()},
		{"bare.mp3", append([]byte{}, mp3Frame...)},
		{"tone.aiff", makeAIFFBytes()},
		{"clip.webm", makeMKVBytes(false)},
		{"live.mkv", makeMKVBytes(true)},
		{"clip.avi", makeAVIBytes()},
		{"stream.ts", makeTSBytes(false)},
		{"disc.m2ts", makeTSBytes(true)},
		{"movie.mpg", makePSBytes(false)},
		{"VTS_01_1.VOB", makePSBytes(true)},
	}
	dir := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transformOnce(t, dir, tc.name, tc.data)
		})
	}
}

func TestTransformFile_JPEGTrailerKeptAndMPFStillResolves(t *testing.T) {
	data := makeMPFJPEG(t)
	layout, err := parseJPEG(data)
	if err != nil {
		t.Fatal(err)
	}
	_, out := transformOnce(t, t.TempDir(), "mpf.jpg", data)
	outLayout, err := parseJPEG(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out[outLayout.primaryEnd:], data[layout.primaryEnd:]) {
		t.Fatal("trailer after EOI changed")
	}
	plan, err := planJPEGTrailer(out, outLayout)
	if err != nil {
		t.Fatalf("output MPF directory does not resolve: %v", err)
	}
	if plan.mpf == nil || int(plan.mpf.entries[0].size) != outLayout.primaryEnd || plan.images != 2 {
		t.Fatal("output MPF first image size was not grown")
	}
	if plan.insertAt != 2+34 {
		t.Fatalf("COM segment not placed before the MPF segment (MPF at %d)", plan.insertAt)
	}
}

func TestTransformFile_PDFUpdateKeepsOriginalPrefix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		data   []byte
		stream bool
	}{{"table.pdf", makeClassicPDF(""), false}, {"stream.pdf", makeStreamPDF(t), true}} {
		_, out := transformOnce(t, t.TempDir(), tc.name, tc.data)
		if !bytes.HasPrefix(out, tc.data) {
			t.Fatalf("%s: original bytes are not a prefix of the copy", tc.name)
		}
		chain, err := pdfResolveChain(out)
		if err != nil {
			t.Fatal(err)
		}
		if chain.sections != 2 || chain.newest.stream != tc.stream {
			t.Fatalf("%s: update section has %d sections, stream=%v", tc.name, chain.sections, chain.newest.stream)
		}
	}
}

func TestTransformFile_MoreFormatsSkipMalformed(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name, code string
		data       []byte
	}{
		{"unknown-trailer.jpg", "invalid-format", append(makeJPEGBytes(t), "garbage after the picture"...)},
		{"bad.heic", "invalid-format", isoBox("ftyp", []byte("isom\x00\x00\x00\x00isom"))},
		{"bad.webp", "invalid-format", []byte("RIFF\x04\x00\x00\x00WEBQ")},
		{"bad.bmp", "invalid-format", []byte("BM not a bitmap at all.....")},
		{"bad.tif", "invalid-format", []byte("II*\x00\xFF\xFF\x00\x00 broken tiff")},
		{"bad.pdf", "invalid-format", []byte("%PDF-1.4\nno xref here\n%%EOF\n")},
		{"signed.pdf", "signed-file", makeClassicPDF(" /Sig << /ByteRange [0 1 2 3] >>")},
		{"locked.pdf", "unsupported-format", makeClassicPDF(" /Encrypt 9 0 R")},
		{"bad.rtf", "invalid-format", []byte(`{\rtf1 never closed`)},
		{"binary.txt", "invalid-format", []byte("a\x00b")},
		{"fence.md", "invalid-format", []byte("```\nopen code fence\n")},
		{"pgp.txt", "signed-file", []byte("-----BEGIN PGP SIGNED MESSAGE-----\nx\n-----BEGIN PGP SIGNATURE-----\ny\n")},
		{"bad.mp3", "invalid-format", []byte("not an mpeg audio file at all")},
		{"bad.aiff", "invalid-format", []byte("FORM\x00\x00\x00\x04AIFX")},
		{"bad.mkv", "invalid-format", []byte{0x1A, 0x45, 0xDF, 0xA3, 0x80, 0x18, 0x53}},
		{"bad.avi", "invalid-format", []byte("RIFF\x04\x00\x00\x00WAVE")},
		{"code.ts", "unsupported-format", []byte("export const x: number = 1;\n")},
		{"bad.mpg", "invalid-format", []byte("not a program stream")},
		{"old.csv", "unsupported-format", []byte("a,b\n1,2\n")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name)
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := RewriteFile(context.Background(), path)
			se, ok := IsSkip(err)
			if !ok || se.Code != tc.code {
				t.Fatalf("err = %v, want skip %q", err, tc.code)
			}
			assertUntouched(t, path, tc.data)
		})
	}
}
