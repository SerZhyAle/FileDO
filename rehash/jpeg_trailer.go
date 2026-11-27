package rehash

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"
)

// Phone JPEGs carry data after the primary image's EOI: further images that
// the CIPA DC-007 Multi-Picture Format directory points at (MPO, iPhone and
// Android Ultra HDR gain maps), a Motion Photo video located from the end of
// the file, a Samsung SEFT trailer, or zero padding.
//
// MPF offsets are relative to the MP endian marker inside the APP2 segment.
// The rehash COM segment is therefore inserted before that APP2 segment, so
// the marker and every image after EOI move by the same amount and no offset
// changes; only the first image's Individual Image Size grows. Motion Photo
// and SEFT locate their data from the end of the file, which does not move.
// Any other trailer is unknown and the file is skipped.

type mpfEntry struct {
	sizePos int // absolute offset of the Individual Image Size field
	size    uint32
	offset  uint32
}

type mpfInfo struct {
	base    int // absolute offset of the MP endian marker
	order   binary.ByteOrder
	entries []mpfEntry
}

type jpegTrailerPlan struct {
	insertAt int
	mpf      *mpfInfo
	images   int
}

var (
	xmpAPP1Prefix = []byte("http://ns.adobe.com/xap/1.0/\x00")
	mpfAPP2Prefix = []byte("MPF\x00")
)

func parseMPF(seg jpegSegment) (*mpfInfo, error) {
	body := seg.body
	if len(body) < 4+8 {
		return nil, fmt.Errorf("MPF segment too short")
	}
	tiff := body[4:]
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(tiff, []byte("II*\x00")):
		order = binary.LittleEndian
	case bytes.HasPrefix(tiff, []byte("MM\x00*")):
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("MPF endian marker invalid")
	}
	ifd := int(order.Uint32(tiff[4:8]))
	if ifd < 8 || ifd+2 > len(tiff) {
		return nil, fmt.Errorf("MPF index IFD offset out of range")
	}
	count := int(order.Uint16(tiff[ifd : ifd+2]))
	if ifd+2+count*12 > len(tiff) {
		return nil, fmt.Errorf("MPF index IFD truncated")
	}
	declared := -1
	var entriesOff, entriesLen int
	for i := 0; i < count; i++ {
		e := tiff[ifd+2+i*12 : ifd+2+i*12+12]
		tag := order.Uint16(e[0:2])
		n := int(order.Uint32(e[4:8]))
		switch tag {
		case 0xB001:
			declared = int(order.Uint32(e[8:12]))
		case 0xB002:
			if n <= 0 || n%16 != 0 {
				return nil, fmt.Errorf("MPF entry array has invalid length %d", n)
			}
			entriesLen = n
			entriesOff = int(order.Uint32(e[8:12]))
		}
	}
	if entriesLen == 0 {
		return nil, fmt.Errorf("MPF directory has no MP Entry tag")
	}
	if entriesOff < 8 || entriesOff+entriesLen > len(tiff) {
		return nil, fmt.Errorf("MPF entry array out of range")
	}
	if declared >= 0 && declared != entriesLen/16 {
		return nil, fmt.Errorf("MPF image count %d does not match %d entries", declared, entriesLen/16)
	}
	base := seg.bodyStart + 4
	info := &mpfInfo{base: base, order: order}
	for i := 0; i < entriesLen/16; i++ {
		p := entriesOff + i*16
		info.entries = append(info.entries, mpfEntry{
			sizePos: base + p + 4,
			size:    order.Uint32(tiff[p+4 : p+8]),
			offset:  order.Uint32(tiff[p+8 : p+12]),
		})
	}
	return info, nil
}

func isPaddingBytes(b []byte) bool {
	for _, c := range b {
		if c != 0x00 && c != 0xFF {
			return false
		}
	}
	return true
}

// xmpDeclaresPrimaryLength reports a GContainer directory whose Primary item
// carries an explicit Item:Length, which a grown primary image would break.
func xmpDeclaresPrimaryLength(xmp []byte) bool {
	rest := xmp
	for {
		i := bytes.Index(rest, []byte(`Item:Semantic="Primary"`))
		if i < 0 {
			return false
		}
		start := bytes.LastIndexByte(rest[:i], '<')
		end := bytes.IndexByte(rest[i:], '>')
		if start < 0 || end < 0 {
			return true
		}
		if bytes.Contains(rest[start:i+end], []byte("Item:Length")) {
			return true
		}
		rest = rest[i+end:]
	}
}

func planJPEGTrailer(data []byte, l *jpegLayout) (*jpegTrailerPlan, error) {
	plan := &jpegTrailerPlan{insertAt: l.firstSOS, images: 1}
	motion := false
	mpfIndex := -1
	for i, s := range l.headSegs {
		switch {
		case s.marker == 0xE1 && bytes.HasPrefix(s.body, xmpAPP1Prefix):
			if bytes.Contains(s.body, []byte("MotionPhoto")) || bytes.Contains(s.body, []byte("MicroVideo")) {
				motion = true
			}
			if xmpDeclaresPrimaryLength(s.body) {
				return nil, fmt.Errorf("XMP container declares a fixed primary image length")
			}
		case s.marker == 0xE2 && mpfIndex < 0 && bytes.HasPrefix(s.body, mpfAPP2Prefix):
			mpfIndex = i
		}
	}

	type span struct{ start, end int }
	var spans []span
	if mpfIndex >= 0 {
		for _, s := range l.headSegs[mpfIndex+1:] {
			if s.marker == 0xE0 || s.marker == 0xE1 {
				return nil, fmt.Errorf("APP0/APP1 segment after the MPF segment")
			}
		}
		info, err := parseMPF(l.headSegs[mpfIndex])
		if err != nil {
			return nil, err
		}
		first := info.entries[0]
		if first.offset != 0 || int64(first.size) != int64(l.primaryEnd) {
			return nil, fmt.Errorf("MPF first image entry (size %d, offset %d) does not match the primary image (%d bytes)", first.size, first.offset, l.primaryEnd)
		}
		for _, e := range info.entries[1:] {
			if e.size == 0 && e.offset == 0 {
				continue
			}
			abs := int64(info.base) + int64(e.offset)
			end := abs + int64(e.size)
			if abs < int64(l.primaryEnd) || end > int64(len(data)) || e.size < 4 {
				return nil, fmt.Errorf("MPF image at offset %d outside the trailer", abs)
			}
			if data[abs] != 0xFF || data[abs+1] != 0xD8 {
				return nil, fmt.Errorf("MPF image at offset %d does not start with SOI", abs)
			}
			spans = append(spans, span{int(abs), int(end)})
		}
		plan.insertAt = l.headSegs[mpfIndex].start
		plan.mpf = info
		plan.images = len(info.entries)
	}

	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	cursor := l.primaryEnd
	for _, s := range spans {
		if s.start < cursor {
			return nil, fmt.Errorf("MPF images overlap")
		}
		if !isPaddingBytes(data[cursor:s.start]) {
			return nil, fmt.Errorf("unknown data between MPF images")
		}
		cursor = s.end
	}
	rest := data[cursor:]
	switch {
	case len(rest) == 0, isPaddingBytes(rest):
	case bytes.HasSuffix(rest, []byte("SEFT")):
	case motion && len(rest) >= 8 && string(rest[4:8]) == "ftyp":
	default:
		return nil, fmt.Errorf("JPEG has %d trailing bytes after EOI of an unknown kind", len(rest))
	}
	return plan, nil
}

// patchOutput grows the first MPF image size by delta inside out, where the
// COM segment of delta bytes is already inserted at insertAt.
func (p *jpegTrailerPlan) patchOutput(out []byte, delta int) error {
	if p.mpf == nil {
		return nil
	}
	pos := p.mpf.entries[0].sizePos + delta
	if pos+4 > len(out) {
		return fmt.Errorf("MPF size field out of range")
	}
	p.mpf.order.PutUint32(out[pos:pos+4], p.mpf.entries[0].size+uint32(delta))
	return nil
}
