package rehash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
)

// ---- BMP (Windows DIB: bytes after the pixel array, bfSize grown) ----------

type bmpInfo struct {
	headerSize uint32
	pixelEnd   int64
}

func parseBMP(r io.ReaderAt, size int64) (*bmpInfo, error) {
	if size < 26 {
		return nil, fmt.Errorf("BMP file too short")
	}
	h, err := readAt(r, 0, 26)
	if err != nil {
		return nil, err
	}
	if string(h[0:2]) != "BM" {
		return nil, fmt.Errorf("missing BM signature")
	}
	if int64(binary.LittleEndian.Uint32(h[2:6])) != size {
		return nil, fmt.Errorf("BMP bfSize %d does not match file size %d", binary.LittleEndian.Uint32(h[2:6]), size)
	}
	offBits := int64(binary.LittleEndian.Uint32(h[10:14]))
	dibSize := binary.LittleEndian.Uint32(h[14:18])
	var width, height int64
	var bpp, compression, sizeImage uint32
	switch dibSize {
	case 12:
		width = int64(binary.LittleEndian.Uint16(h[18:20]))
		height = int64(binary.LittleEndian.Uint16(h[20:22]))
		bpp = uint32(binary.LittleEndian.Uint16(h[24:26]))
	case 40, 52, 56, 64, 108, 124:
		if size < 14+int64(dibSize) {
			return nil, fmt.Errorf("truncated BMP info header")
		}
		d, err := readAt(r, 14, int(dibSize))
		if err != nil {
			return nil, err
		}
		width = int64(int32(binary.LittleEndian.Uint32(d[4:8])))
		height = int64(int32(binary.LittleEndian.Uint32(d[8:12])))
		bpp = uint32(binary.LittleEndian.Uint16(d[14:16]))
		compression = binary.LittleEndian.Uint32(d[16:20])
		sizeImage = binary.LittleEndian.Uint32(d[20:24])
	default:
		return nil, fmt.Errorf("unknown BMP info header size %d", dibSize)
	}
	if height < 0 {
		height = -height
	}
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("invalid BMP dimensions %dx%d", width, height)
	}
	if offBits < 14+int64(dibSize) || offBits > size {
		return nil, fmt.Errorf("BMP pixel offset %d out of range", offBits)
	}
	var pixels int64
	switch compression {
	case 0, 3, 6: // BI_RGB, BI_BITFIELDS, BI_ALPHABITFIELDS
		switch bpp {
		case 1, 4, 8, 16, 24, 32:
		default:
			return nil, fmt.Errorf("invalid BMP bit depth %d", bpp)
		}
		pixels = ((width*int64(bpp) + 31) / 32) * 4 * height
	case 1, 2, 4, 5: // RLE8, RLE4, JPEG, PNG
		if sizeImage == 0 {
			return nil, fmt.Errorf("compressed BMP without image size")
		}
		pixels = int64(sizeImage)
	default:
		return nil, fmt.Errorf("unknown BMP compression %d", compression)
	}
	if offBits+pixels > size {
		return nil, fmt.Errorf("BMP pixel array extends past EOF")
	}
	return &bmpInfo{headerSize: dibSize, pixelEnd: offBits + pixels}, nil
}

type bmpHandler struct{}

func (bmpHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "BMP",
		Extensions: []string{".bmp", ".dib"},
		MinDelta:   30,
		MaxDelta:   30,
		Mechanism:  "bytes after the pixel array, bfSize grown",
	}
}

func (bmpHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	if srcSize+30 > 0xFFFFFFFF {
		return nil, &SkipError{Code: "invalid-format", Reason: "BMP file would exceed the 32-bit bfSize limit"}
	}
	info, err := parseBMP(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid BMP: %v", err)}
	}
	return info, nil
}

func (bmpHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	info := p.(*bmpInfo)
	payload := tagPayload(nonce)
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(srcSize)+uint32(len(payload)))
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, []byteEdit{{2, sz[:]}}, appendAt(srcSize, payload))
	if err != nil {
		return o, 0, h, err
	}
	outInfo, err := parseBMP(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("BMP output failed strict header verification: %w", err)
	}
	if *outInfo != *info {
		return o, 0, h, fmt.Errorf("BMP output pixel layout changed")
	}
	return o, outSize, h, nil
}

// ---- WebP (RFC 9649: unknown chunk at the end of an extended file) ---------

type riffChunk struct {
	off  int64
	id   string
	size uint32
}

// walkRIFFChunks walks the chunks of one RIFF form from start to end.
func walkRIFFChunks(r io.ReaderAt, start, end int64, order binary.ByteOrder) ([]riffChunk, error) {
	var out []riffChunk
	pos := start
	for pos < end {
		if end-pos < 8 {
			return nil, fmt.Errorf("truncated chunk header at offset %d", pos)
		}
		h, err := readAt(r, pos, 8)
		if err != nil {
			return nil, err
		}
		if !isPrintableFourCC(h[0:4]) {
			return nil, fmt.Errorf("non-printable chunk id at offset %d", pos)
		}
		size := order.Uint32(h[4:8])
		step := 8 + int64(size) + int64(size&1)
		if step > end-pos {
			// The pad byte of a final odd chunk is optional in practice.
			if 8+int64(size) != end-pos {
				return nil, fmt.Errorf("chunk %q extends past its container at offset %d", h[0:4], pos)
			}
			step = end - pos
		}
		out = append(out, riffChunk{off: pos, id: string(h[0:4]), size: size})
		pos += step
	}
	return out, nil
}

type webpPlan struct {
	chunks []riffChunk
	vp8x   []byte // non-nil when a simple file must be wrapped
}

func parseWebP(r io.ReaderAt, size int64) ([]riffChunk, error) {
	if size < 20 {
		return nil, fmt.Errorf("WebP file too short")
	}
	h, err := readAt(r, 0, 12)
	if err != nil {
		return nil, err
	}
	if string(h[0:4]) != "RIFF" || string(h[8:12]) != "WEBP" {
		return nil, fmt.Errorf("missing RIFF/WEBP header")
	}
	if int64(binary.LittleEndian.Uint32(h[4:8]))+8 != size {
		return nil, fmt.Errorf("RIFF size does not match file size")
	}
	chunks, err := walkRIFFChunks(r, 12, size, binary.LittleEndian)
	if err != nil {
		return nil, err
	}
	switch chunks[0].id {
	case "VP8X":
		if chunks[0].size < 10 {
			return nil, fmt.Errorf("VP8X chunk too short")
		}
		hasImage := false
		for _, c := range chunks[1:] {
			if c.id == "VP8 " || c.id == "VP8L" || c.id == "ANMF" {
				hasImage = true
			}
		}
		if !hasImage {
			return nil, fmt.Errorf("extended WebP has no image data")
		}
	case "VP8 ", "VP8L":
		if len(chunks) != 1 {
			return nil, fmt.Errorf("simple WebP has extra chunks")
		}
	default:
		return nil, fmt.Errorf("unexpected first WebP chunk %q", chunks[0].id)
	}
	return chunks, nil
}

// webpVP8X builds the VP8X chunk that turns a simple file into an extended one.
func webpVP8X(r io.ReaderAt, c riffChunk) ([]byte, error) {
	if c.size < 10 {
		return nil, fmt.Errorf("WebP bitstream too short")
	}
	b, err := readAt(r, c.off+8, 10)
	if err != nil {
		return nil, err
	}
	var w, h uint32
	var flags byte
	if c.id == "VP8 " {
		if b[0]&1 != 0 || b[3] != 0x9D || b[4] != 0x01 || b[5] != 0x2A {
			return nil, fmt.Errorf("VP8 bitstream is not a key frame")
		}
		w = uint32(binary.LittleEndian.Uint16(b[6:8]) & 0x3FFF)
		h = uint32(binary.LittleEndian.Uint16(b[8:10]) & 0x3FFF)
	} else {
		if b[0] != 0x2F {
			return nil, fmt.Errorf("missing VP8L signature")
		}
		v := binary.LittleEndian.Uint32(b[1:5])
		if v>>29 != 0 {
			return nil, fmt.Errorf("unknown VP8L version")
		}
		w = (v & 0x3FFF) + 1
		h = ((v >> 14) & 0x3FFF) + 1
		if (v>>28)&1 != 0 {
			flags |= 0x10 // alpha
		}
	}
	if w == 0 || h == 0 {
		return nil, fmt.Errorf("WebP canvas has zero size")
	}
	x := make([]byte, 18)
	copy(x[0:4], "VP8X")
	binary.LittleEndian.PutUint32(x[4:8], 10)
	x[8] = flags
	put24 := func(p []byte, v uint32) { p[0], p[1], p[2] = byte(v), byte(v>>8), byte(v>>16) }
	put24(x[12:15], w-1)
	put24(x[15:18], h-1)
	return x, nil
}

type webpHandler struct{}

func (webpHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "WebP",
		Extensions: []string{".webp"},
		MinDelta:   38,
		MaxDelta:   57,
		Mechanism:  "unknown chunk at the end of an extended (VP8X) file; a simple file is wrapped in VP8X first",
	}
}

func (webpHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	chunks, err := parseWebP(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid WebP: %v", err)}
	}
	plan := &webpPlan{chunks: chunks}
	if chunks[0].id != "VP8X" {
		if plan.vp8x, err = webpVP8X(src, chunks[0]); err != nil {
			return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid WebP: %v", err)}
		}
	}
	return plan, nil
}

func (webpHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*webpPlan)
	payload := tagPayload(nonce)
	chunk := make([]byte, 8+len(payload))
	copy(chunk[0:4], "FDRH")
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(len(payload)))
	copy(chunk[8:], payload)

	inserts := []byteInsert{}
	if plan.vp8x != nil {
		inserts = append(inserts, byteInsert{at: 12, b: plan.vp8x})
	}
	last := plan.chunks[len(plan.chunks)-1]
	if last.size&1 != 0 && last.off+8+int64(last.size) == srcSize {
		chunk = append([]byte{0}, chunk...) // pad byte the odd final chunk lacked
	}
	inserts = append(inserts, appendAt(srcSize, chunk))
	riff := srcSize - 8
	for _, in := range inserts {
		riff += int64(len(in.b))
	}
	if riff > 0xFFFFFFFF {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("WebP output would exceed the 32-bit RIFF size limit")
	}
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(riff))
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, []byteEdit{{4, sz[:]}}, inserts...)
	if err != nil {
		return o, 0, h, err
	}
	outChunks, err := parseWebP(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("WebP output failed strict chunk verification: %w", err)
	}
	want := len(plan.chunks) + 1
	if plan.vp8x != nil {
		want++
	}
	if len(outChunks) != want || outChunks[0].id != "VP8X" || outChunks[len(outChunks)-1].id != "FDRH" {
		return o, 0, h, fmt.Errorf("WebP output chunk layout mismatch")
	}
	return o, outSize, h, nil
}

// ---- TIFF / DNG / camera RAW (tagged block after all IFD-referenced data) --

type tiffInfo struct {
	ifds int
}

func parseTIFF(r io.ReaderAt, size int64) (*tiffInfo, error) {
	if size < 16 {
		return nil, fmt.Errorf("TIFF file too short")
	}
	h, err := readAt(r, 0, 16)
	if err != nil {
		return nil, err
	}
	var order binary.ByteOrder
	switch string(h[0:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("missing TIFF byte-order mark")
	}
	magic := order.Uint16(h[2:4])
	big := false
	var first int64
	switch {
	case magic == 42 || magic == 0x4F52 || magic == 0x5352 || magic == 0x0055: // TIFF, ORF, ORF, RW2
		first = int64(order.Uint32(h[4:8]))
	case magic == 43:
		if order.Uint16(h[4:6]) != 8 {
			return nil, fmt.Errorf("unsupported BigTIFF offset size")
		}
		big = true
		first = int64(order.Uint64(h[8:16]))
	default:
		return nil, fmt.Errorf("unknown TIFF magic %d", magic)
	}
	entrySize, countSize, offSize := int64(12), int64(2), int64(4)
	if big {
		entrySize, countSize, offSize = 20, 8, 8
	}
	seen := map[int64]bool{}
	ifds := 0
	for off := first; off != 0; {
		if seen[off] || ifds >= 1024 {
			return nil, fmt.Errorf("TIFF IFD chain loops")
		}
		seen[off] = true
		if off < 8 || off+countSize > size {
			return nil, fmt.Errorf("TIFF IFD offset %d out of range", off)
		}
		cb, err := readAt(r, off, int(countSize))
		if err != nil {
			return nil, err
		}
		var n int64
		if big {
			n = int64(order.Uint64(cb))
		} else {
			n = int64(order.Uint16(cb))
		}
		if n == 0 || n > 4096 || off+countSize+n*entrySize+offSize > size {
			return nil, fmt.Errorf("TIFF IFD at %d has an invalid entry count", off)
		}
		body, err := readAt(r, off+countSize, int(n*entrySize+offSize))
		if err != nil {
			return nil, err
		}
		for i := int64(0); i < n; i++ {
			e := body[i*entrySize : (i+1)*entrySize]
			typ := order.Uint16(e[2:4])
			unit := map[uint16]int64{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 6: 1, 7: 1, 8: 2, 9: 4, 10: 8, 11: 4, 12: 8, 13: 4, 16: 8, 17: 8, 18: 8}[typ]
			if unit == 0 {
				continue // unknown types are skipped by readers
			}
			var count, valOff int64
			if big {
				count = int64(order.Uint64(e[4:12]))
				valOff = int64(order.Uint64(e[12:20]))
			} else {
				count = int64(order.Uint32(e[4:8]))
				valOff = int64(order.Uint32(e[8:12]))
			}
			if count < 0 || count > size {
				return nil, fmt.Errorf("TIFF tag count out of range")
			}
			if unit*count > offSize && (valOff < 0 || valOff+unit*count > size) {
				return nil, fmt.Errorf("TIFF tag %d value extends past EOF", order.Uint16(e[0:2]))
			}
		}
		tail := body[n*entrySize:]
		if big {
			off = int64(order.Uint64(tail))
		} else {
			off = int64(order.Uint32(tail))
		}
		ifds++
	}
	if ifds == 0 {
		return nil, fmt.Errorf("TIFF has no IFD")
	}
	return &tiffInfo{ifds: ifds}, nil
}

type tiffHandler struct{}

func (tiffHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "TIFF",
		Extensions: []string{".tif", ".tiff", ".dng", ".nef", ".cr2", ".arw", ".orf", ".rw2"},
		MinDelta:   30,
		MaxDelta:   30,
		Mechanism:  "unreferenced tagged block after all IFD data; header and IFDs unchanged",
	}
}

func (tiffHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	info, err := parseTIFF(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid TIFF/RAW: %v", err)}
	}
	return info, nil
}

func (tiffHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	info := p.(*tiffInfo)
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, nil, appendAt(srcSize, tagPayload(nonce)))
	if err != nil {
		return o, 0, h, err
	}
	outInfo, err := parseTIFF(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("TIFF output failed strict IFD verification: %w", err)
	}
	if outInfo.ifds != info.ifds {
		return o, 0, h, fmt.Errorf("TIFF output IFD chain changed")
	}
	tail, err := readAt(dst, srcSize, int(outSize-srcSize))
	if err != nil || !bytes.HasPrefix(tail, []byte(tagPrefix)) {
		return o, 0, h, fmt.Errorf("TIFF output tag block missing")
	}
	return o, outSize, h, nil
}
