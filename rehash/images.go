package rehash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
)

// imagesEqual compares two decoded images pixel by pixel in full 16-bit RGBA.
func imagesEqual(a, b image.Image) bool {
	ba, bb := a.Bounds(), b.Bounds()
	if ba != bb {
		return false
	}
	for y := ba.Min.Y; y < ba.Max.Y; y++ {
		for x := ba.Min.X; x < ba.Max.X; x++ {
			ra, ga, ba2, aa := a.At(x, y).RGBA()
			rb, gb, bb2, ab := b.At(x, y).RGBA()
			if ra != rb || ga != gb || ba2 != bb2 || aa != ab {
				return false
			}
		}
	}
	return true
}

// ---- PNG (W3C PNG Third Edition: ancillary tEXt chunk before IEND) ---------

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type pngChunk struct {
	offset int
	typ    string
	raw    []byte
}

func parsePNGChunks(data []byte) ([]pngChunk, int, error) {
	if len(data) < 8 || !bytes.Equal(data[:8], pngSignature) {
		return nil, 0, fmt.Errorf("missing PNG signature")
	}
	pos := 8
	var chunks []pngChunk
	for pos < len(data) {
		if len(data)-pos < 12 {
			return nil, 0, fmt.Errorf("truncated PNG chunk header at offset %d", pos)
		}
		length := binary.BigEndian.Uint32(data[pos : pos+4])
		if length > 0x7fffffff {
			return nil, 0, fmt.Errorf("PNG chunk length out of range at offset %d", pos)
		}
		end := int64(pos) + 12 + int64(length)
		if end > int64(len(data)) {
			return nil, 0, fmt.Errorf("truncated PNG chunk body at offset %d", pos)
		}
		typ := string(data[pos+4 : pos+8])
		wantCRC := binary.BigEndian.Uint32(data[end-4 : end])
		gotCRC := crc32.ChecksumIEEE(data[pos+4 : end-4])
		if wantCRC != gotCRC {
			return nil, 0, fmt.Errorf("PNG chunk %q CRC mismatch at offset %d", typ, pos)
		}
		chunks = append(chunks, pngChunk{
			offset: pos,
			typ:    typ,
			raw:    data[pos:end],
		})
		if typ == "IEND" {
			if length != 0 {
				return nil, 0, fmt.Errorf("PNG IEND chunk has non-zero length %d", length)
			}
			if int(end) != len(data) {
				return nil, 0, fmt.Errorf("PNG has %d trailing bytes after IEND", len(data)-int(end))
			}
			if len(chunks) < 2 || chunks[0].typ != "IHDR" {
				return nil, 0, fmt.Errorf("PNG does not begin with IHDR")
			}
			return chunks, pos, nil
		}
		pos = int(end)
	}
	return nil, 0, fmt.Errorf("PNG missing IEND chunk")
}

type pngPlan struct {
	data       []byte
	chunks     []pngChunk
	iendOffset int
	decoded    image.Image
}

type pngHandler struct{}

func (pngHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "PNG",
		Extensions: []string{".png"},
		MinDelta:   42,
		MaxDelta:   42,
		Mechanism:  "ancillary tEXt chunk before IEND",
	}
}

func (pngHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "PNG")
	if err != nil {
		return nil, err
	}
	chunks, iendOffset, err := parsePNGChunks(data)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid PNG: %v", err)}
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("PNG strict decode failed: %v", err)}
	}
	return &pngPlan{data: data, chunks: chunks, iendOffset: iendOffset, decoded: img}, nil
}

func (pngHandler) writeAndVerify(ctx context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*pngPlan)
	// tEXt payload: keyword "FileDO-rehash" (13) + NUL (1) + 16-hex nonce (16) = 30 bytes.
	// Total chunk: 4 len + 4 type + 30 payload + 4 CRC = 42 bytes.
	payload := make([]byte, 0, 30)
	payload = append(payload, "FileDO-rehash"...)
	payload = append(payload, 0)
	payload = append(payload, nonce...)

	chunk := make([]byte, 12+len(payload))
	binary.BigEndian.PutUint32(chunk[0:4], uint32(len(payload)))
	copy(chunk[4:8], "tEXt")
	copy(chunk[8:8+len(payload)], payload)
	binary.BigEndian.PutUint32(chunk[8+len(payload):], crc32.ChecksumIEEE(chunk[4:8+len(payload)]))

	out := make([]byte, 0, len(plan.data)+len(chunk))
	out = append(out, plan.data[:plan.iendOffset]...)
	out = append(out, chunk...)
	out = append(out, plan.data[plan.iendOffset:]...)

	outChunks, _, err := parsePNGChunks(out)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PNG output failed strict chunk verification: %w", err)
	}
	if len(outChunks) != len(plan.chunks)+1 {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PNG output chunk count mismatch")
	}
	origBeforeEnd := len(plan.chunks) - 1
	for i := 0; i < origBeforeEnd; i++ {
		if !bytes.Equal(outChunks[i].raw, plan.chunks[i].raw) {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PNG original chunk %d changed", i)
		}
	}
	if !bytes.Equal(outChunks[len(outChunks)-1].raw, plan.chunks[origBeforeEnd].raw) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PNG IEND chunk changed")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
		}
	}
	outImg, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PNG output failed strict decode: %w", err)
	}
	if !imagesEqual(plan.decoded, outImg) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("PNG decoded pixels changed")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}

// ---- JPEG (ITU-T T.81: COM marker segment before first SOS) ----------------

// jpegSegment is one marker segment that precedes the first SOS.
type jpegSegment struct {
	start     int // offset of the segment's first 0xFF byte
	marker    byte
	bodyStart int // offset just past the 2-byte segment length
	body      []byte
}

// jpegLayout is the marker structure of the primary JPEG image. Bytes from
// primaryEnd to EOF are the trailer (phone gain maps, motion photos, maker
// data); parseJPEG reports them and planJPEGTrailer decides whether they are
// a known, offset-safe kind.
type jpegLayout struct {
	firstSOS   int
	primaryEnd int
	headSegs   []jpegSegment
}

func parseJPEG(data []byte) (*jpegLayout, error) {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil, fmt.Errorf("missing JPEG SOI marker")
	}
	pos := 2
	firstSOS := 0
	var head []jpegSegment
	for pos < len(data) {
		if data[pos] != 0xFF {
			return nil, fmt.Errorf("expected JPEG marker 0xFF at offset %d", pos)
		}
		segStart := pos
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			return nil, fmt.Errorf("truncated JPEG marker at EOF")
		}
		marker := data[pos]
		pos++
		if marker == 0xD9 { // EOI
			if firstSOS == 0 {
				return nil, fmt.Errorf("JPEG has no SOS scan data")
			}
			return &jpegLayout{firstSOS: firstSOS, primaryEnd: pos, headSegs: head}, nil
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if marker == 0xD8 {
			return nil, fmt.Errorf("unexpected nested JPEG SOI at offset %d", segStart)
		}
		if pos+2 > len(data) {
			return nil, fmt.Errorf("truncated JPEG segment length at offset %d", segStart)
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			return nil, fmt.Errorf("invalid JPEG segment length %d at offset %d", segLen, segStart)
		}
		if firstSOS == 0 && marker != 0xDA {
			head = append(head, jpegSegment{start: segStart, marker: marker, bodyStart: pos + 2, body: data[pos+2 : pos+segLen]})
		}
		if marker == 0xDA { // SOS
			if firstSOS == 0 {
				firstSOS = segStart
			}
			pos += segLen
			// Walk entropy-coded scan until a non-stuffed, non-restart marker.
			foundNext := false
			for pos < len(data) {
				if data[pos] != 0xFF {
					pos++
					continue
				}
				ffPos := pos
				for pos < len(data) && data[pos] == 0xFF {
					pos++
				}
				if pos >= len(data) {
					return nil, fmt.Errorf("truncated JPEG scan at EOF")
				}
				next := data[pos]
				if next == 0x00 || (next >= 0xD0 && next <= 0xD7) {
					pos++
					continue
				}
				pos = ffPos
				foundNext = true
				break
			}
			if !foundNext {
				return nil, fmt.Errorf("JPEG scan ended without EOI")
			}
			continue
		}
		pos += segLen
	}
	return nil, fmt.Errorf("JPEG missing EOI marker")
}

type jpegPlan struct {
	data    []byte
	layout  *jpegLayout
	trailer *jpegTrailerPlan
	decoded image.Image
}

type jpegHandler struct{}

func (jpegHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "JPEG",
		Extensions: []string{".jpg", ".jpeg"},
		MinDelta:   34,
		MaxDelta:   34,
		Mechanism:  "COM comment segment before first SOS (before the MPF segment when the file carries one); data after EOI kept byte-identical",
	}
}

func (jpegHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "JPEG")
	if err != nil {
		return nil, err
	}
	layout, err := parseJPEG(data)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid JPEG: %v", err)}
	}
	trailer, err := planJPEGTrailer(data, layout)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid JPEG: %v", err)}
	}
	img, err := jpeg.Decode(bytes.NewReader(data[:layout.primaryEnd]))
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("JPEG strict decode failed: %v", err)}
	}
	return &jpegPlan{data: data, layout: layout, trailer: trailer, decoded: img}, nil
}

func (jpegHandler) writeAndVerify(ctx context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*jpegPlan)
	// COM segment: FF FE (2) + BE length 32 (2) + "FileDO-rehash:<16 hex>" (30) = 34 bytes.
	payload := tagPrefix + nonce
	seg := make([]byte, 4+len(payload))
	seg[0] = 0xFF
	seg[1] = 0xFE
	binary.BigEndian.PutUint16(seg[2:4], uint16(2+len(payload)))
	copy(seg[4:], payload)

	at := plan.trailer.insertAt
	out := make([]byte, 0, len(plan.data)+len(seg))
	out = append(out, plan.data[:at]...)
	out = append(out, seg...)
	out = append(out, plan.data[at:]...)
	if err := plan.trailer.patchOutput(out, len(seg)); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	outLayout, err := parseJPEG(out)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG output failed strict marker verification: %w", err)
	}
	if outLayout.firstSOS != plan.layout.firstSOS+len(seg) || outLayout.primaryEnd != plan.layout.primaryEnd+len(seg) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG output SOS or EOI offset mismatch")
	}
	if !bytes.Equal(out[outLayout.primaryEnd:], plan.data[plan.layout.primaryEnd:]) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG output trailer after EOI changed")
	}
	outTrailer, err := planJPEGTrailer(out, outLayout)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG output trailer failed verification: %w", err)
	}
	if outTrailer.images != plan.trailer.images {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG output MPF image count changed")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
		}
	}
	outImg, err := jpeg.Decode(bytes.NewReader(out[:outLayout.primaryEnd]))
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG output failed strict decode: %w", err)
	}
	if !imagesEqual(plan.decoded, outImg) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("JPEG decoded pixels changed")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}

// ---- GIF89a (Comment Extension 0x21 0xFE before Trailer 0x3B) --------------

func skipGIFSubBlocks(data []byte, pos int) (int, error) {
	for {
		if pos >= len(data) {
			return 0, fmt.Errorf("truncated GIF sub-block at offset %d", pos)
		}
		subLen := int(data[pos])
		pos++
		if subLen == 0 {
			return pos, nil
		}
		if pos+subLen > len(data) {
			return 0, fmt.Errorf("GIF sub-block extends past EOF at offset %d", pos)
		}
		pos += subLen
	}
}

func parseGIF89a(data []byte) (int, error) {
	if len(data) < 14 {
		return 0, fmt.Errorf("GIF file too short")
	}
	hdr := string(data[:6])
	if hdr == "GIF87a" {
		return 0, &SkipError{Code: "unsupported-format", Reason: "GIF87a has no standard comment extension (only GIF89a is supported)"}
	}
	if hdr != "GIF89a" {
		return 0, fmt.Errorf("missing GIF89a header")
	}
	pos := 13
	packed := data[10]
	if packed&0x80 != 0 {
		gctSize := 3 * (1 << ((packed & 0x07) + 1))
		pos += gctSize
		if pos > len(data) {
			return 0, fmt.Errorf("truncated GIF global color table")
		}
	}
	for pos < len(data) {
		switch data[pos] {
		case 0x3B: // Trailer
			if pos+1 != len(data) {
				return 0, fmt.Errorf("GIF has %d trailing bytes after trailer", len(data)-pos-1)
			}
			return pos, nil
		case 0x2C: // Image Descriptor
			if pos+10 > len(data) {
				return 0, fmt.Errorf("truncated GIF image descriptor at offset %d", pos)
			}
			imgPacked := data[pos+9]
			pos += 10
			if imgPacked&0x80 != 0 {
				lctSize := 3 * (1 << ((imgPacked & 0x07) + 1))
				pos += lctSize
				if pos > len(data) {
					return 0, fmt.Errorf("truncated GIF local color table")
				}
			}
			if pos >= len(data) {
				return 0, fmt.Errorf("truncated GIF LZW minimum code size")
			}
			pos++ // LZW min code size
			var err error
			pos, err = skipGIFSubBlocks(data, pos)
			if err != nil {
				return 0, err
			}
		case 0x21: // Extension
			if pos+2 > len(data) {
				return 0, fmt.Errorf("truncated GIF extension at offset %d", pos)
			}
			pos += 2
			var err error
			pos, err = skipGIFSubBlocks(data, pos)
			if err != nil {
				return 0, err
			}
		default:
			return 0, fmt.Errorf("unexpected GIF block introducer 0x%02x at offset %d", data[pos], pos)
		}
	}
	return 0, fmt.Errorf("GIF missing trailer 0x3B")
}

func gifsEqual(a, b *gif.GIF) bool {
	if a.LoopCount != b.LoopCount || a.BackgroundIndex != b.BackgroundIndex ||
		a.Config.Width != b.Config.Width || a.Config.Height != b.Config.Height {
		return false
	}
	if len(a.Image) != len(b.Image) || len(a.Delay) != len(b.Delay) || len(a.Disposal) != len(b.Disposal) {
		return false
	}
	for i := range a.Image {
		if a.Delay[i] != b.Delay[i] || a.Disposal[i] != b.Disposal[i] {
			return false
		}
		if !imagesEqual(a.Image[i], b.Image[i]) {
			return false
		}
	}
	return true
}

type gifPlan struct {
	data       []byte
	trailerPos int
	decoded    *gif.GIF
}

type gifHandler struct{}

func (gifHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "GIF89a",
		Extensions: []string{".gif"},
		MinDelta:   34,
		MaxDelta:   34,
		Mechanism:  "Comment Extension block before trailer",
	}
}

func (gifHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	data, err := readBoundedFile(ctx, src, srcSize, "GIF89a")
	if err != nil {
		return nil, err
	}
	trailerPos, err := parseGIF89a(data)
	if err != nil {
		if _, ok := IsSkip(err); ok {
			return nil, err
		}
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid GIF89a: %v", err)}
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("GIF strict decode failed: %v", err)}
	}
	return &gifPlan{data: data, trailerPos: trailerPos, decoded: decoded}, nil
}

func (gifHandler) writeAndVerify(ctx context.Context, _ *os.File, _ int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*gifPlan)
	// Comment Extension: 0x21, 0xFE, len(30), payload(30), 0x00 = 34 bytes.
	payload := tagPrefix + nonce
	ext := make([]byte, 4+len(payload))
	ext[0] = 0x21
	ext[1] = 0xFE
	ext[2] = byte(len(payload))
	copy(ext[3:3+len(payload)], payload)
	ext[3+len(payload)] = 0x00

	out := make([]byte, 0, len(plan.data)+len(ext))
	out = append(out, plan.data[:plan.trailerPos]...)
	out = append(out, ext...)
	out = append(out, plan.data[plan.trailerPos:]...)

	outTrailer, err := parseGIF89a(out)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("GIF output failed strict block verification: %w", err)
	}
	if outTrailer != plan.trailerPos+len(ext) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("GIF output trailer offset mismatch")
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
		}
	}
	outGIF, err := gif.DecodeAll(bytes.NewReader(out))
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("GIF output failed strict decode: %w", err)
	}
	if !gifsEqual(plan.decoded, outGIF) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("GIF decoded frames or metadata changed")
	}
	if _, err := dst.Write(out); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	return sha256.Sum256(plan.data), int64(len(out)), sha256.Sum256(out), nil
}
