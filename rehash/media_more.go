package rehash

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"
	"os"
	"path/filepath"
	"strings"
)

// ---- HEIF / HEIC / AVIF (ISO/IEC 23008-12: top-level free box at EOF) ------

var heifBrands = map[string]bool{
	"mif1": true, "msf1": true, "heic": true, "heix": true, "heim": true, "heis": true,
	"hevc": true, "hevx": true, "avif": true, "avis": true, "mif2": true,
}

func heifBrand(r io.ReaderAt) error {
	h, err := readAt(r, 0, 8)
	if err != nil {
		return err
	}
	size := int(binary.BigEndian.Uint32(h[0:4]))
	if size < 16 || size > 4096 {
		return fmt.Errorf("ftyp box size %d out of range", size)
	}
	b, err := readAt(r, 8, size-8)
	if err != nil {
		return err
	}
	if heifBrands[string(b[0:4])] {
		return nil
	}
	for i := 8; i+4 <= len(b); i += 4 {
		if heifBrands[string(b[i:i+4])] {
			return nil
		}
	}
	return fmt.Errorf("ftyp brands are not HEIF/AVIF")
}

func freeBox(nonce string) []byte {
	payload := tagPayload(nonce)
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], "free")
	copy(box[8:], payload)
	return box
}

type heifHandler struct{}

func (heifHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "HEIF",
		Extensions: []string{".heic", ".heif", ".hif", ".avif"},
		MinDelta:   38,
		MaxDelta:   38,
		Mechanism:  "top-level free box at EOF (item offsets are absolute and do not move)",
	}
}

func (heifHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	count, _, seen, err := walkISOBoxes(src, srcSize)
	if err == nil && !seen["meta"] {
		err = fmt.Errorf("no top-level meta box")
	}
	if err == nil {
		err = heifBrand(src)
	}
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid HEIF/AVIF: %v", err)}
	}
	return &mp4Plan{boxCount: count}, nil
}

func (heifHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*mp4Plan)
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, nil, appendAt(srcSize, freeBox(nonce)))
	if err != nil {
		return o, 0, h, err
	}
	count, last, seen, err := walkISOBoxes(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("HEIF output failed strict box verification: %w", err)
	}
	if count != plan.boxCount+1 || last != "free" || !seen["meta"] {
		return o, 0, h, fmt.Errorf("HEIF output box layout mismatch")
	}
	return o, outSize, h, nil
}

// ---- AIFF / AIFC (IFF: APPL chunk at the end, FORM size grown) -------------

type iffPlan struct {
	riffs     int
	chunks    int
	padBefore bool
	sizePos   int64 // offset of the container size field to grow
	sizeVal   uint32
}

func parseAIFF(r io.ReaderAt, size int64) (*iffPlan, error) {
	if size < 12+8+18 {
		return nil, fmt.Errorf("AIFF file too short")
	}
	h, err := readAt(r, 0, 12)
	if err != nil {
		return nil, err
	}
	if string(h[0:4]) != "FORM" || (string(h[8:12]) != "AIFF" && string(h[8:12]) != "AIFC") {
		return nil, fmt.Errorf("missing FORM/AIFF header")
	}
	formSize := binary.BigEndian.Uint32(h[4:8])
	if int64(formSize)+8 != size {
		return nil, fmt.Errorf("FORM size does not match file size")
	}
	chunks, err := walkRIFFChunks(r, 12, size, binary.BigEndian)
	if err != nil {
		return nil, err
	}
	hasComm := false
	for _, c := range chunks {
		if c.id == "COMM" {
			hasComm = true
		}
	}
	if !hasComm {
		return nil, fmt.Errorf("AIFF has no COMM chunk")
	}
	last := chunks[len(chunks)-1]
	return &iffPlan{
		chunks:    len(chunks),
		padBefore: last.size&1 != 0 && last.off+8+int64(last.size) == size,
		sizePos:   4,
		sizeVal:   formSize,
	}, nil
}

type aiffHandler struct{}

func (aiffHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "AIFF",
		Extensions: []string{".aif", ".aiff", ".aifc"},
		MinDelta:   42,
		MaxDelta:   43,
		Mechanism:  "APPL application chunk at the end, FORM size grown",
	}
}

func (aiffHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	if srcSize+43 > int64(0xFFFFFFFF)+8 {
		return nil, &SkipError{Code: "invalid-format", Reason: "AIFF file would exceed the 32-bit FORM size limit"}
	}
	plan, err := parseAIFF(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid AIFF: %v", err)}
	}
	return plan, nil
}

func (aiffHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*iffPlan)
	payload := tagPayload(nonce)
	chunk := make([]byte, 12+len(payload))
	copy(chunk[0:4], "APPL")
	binary.BigEndian.PutUint32(chunk[4:8], uint32(4+len(payload)))
	copy(chunk[8:12], "FDRH")
	copy(chunk[12:], payload)
	if plan.padBefore {
		chunk = append([]byte{0}, chunk...)
	}
	var sz [4]byte
	binary.BigEndian.PutUint32(sz[:], plan.sizeVal+uint32(len(chunk)))
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, []byteEdit{{plan.sizePos, sz[:]}}, appendAt(srcSize, chunk))
	if err != nil {
		return o, 0, h, err
	}
	outPlan, err := parseAIFF(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("AIFF output failed strict chunk verification: %w", err)
	}
	if outPlan.chunks != plan.chunks+1 {
		return o, 0, h, fmt.Errorf("AIFF output chunk layout mismatch")
	}
	return o, outSize, h, nil
}

// ---- AVI (RIFF AVI / OpenDML AVIX: JUNK chunk inside the last RIFF) --------

func parseAVI(r io.ReaderAt, size int64) (*iffPlan, string, error) {
	pos := int64(0)
	var lastOff int64
	var lastSize uint32
	var lastChunk string
	riffs, inner := 0, 0
	padBefore := false
	for pos < size {
		if size-pos < 12 {
			return nil, "", fmt.Errorf("truncated RIFF header at offset %d", pos)
		}
		h, err := readAt(r, pos, 12)
		if err != nil {
			return nil, "", err
		}
		form := string(h[8:12])
		if string(h[0:4]) != "RIFF" || (riffs == 0 && form != "AVI ") || (riffs > 0 && form != "AVIX") {
			return nil, "", fmt.Errorf("unexpected RIFF form %q at offset %d", form, pos)
		}
		rs := binary.LittleEndian.Uint32(h[4:8])
		end := pos + 8 + int64(rs)
		if end > size {
			return nil, "", fmt.Errorf("RIFF at offset %d extends past EOF", pos)
		}
		chunks, err := walkRIFFChunks(r, pos+12, end, binary.LittleEndian)
		if err != nil {
			return nil, "", err
		}
		if len(chunks) == 0 {
			return nil, "", fmt.Errorf("empty RIFF at offset %d", pos)
		}
		last := chunks[len(chunks)-1]
		padBefore = last.size&1 != 0 && last.off+8+int64(last.size) == end
		lastChunk = last.id
		inner = len(chunks)
		lastOff, lastSize = pos, rs
		riffs++
		pos = end + int64(rs&1)
	}
	if riffs == 0 || pos != size {
		return nil, "", fmt.Errorf("AVI RIFF lists do not tile the file")
	}
	if lastSize&1 != 0 {
		return nil, "", fmt.Errorf("last RIFF has an odd size")
	}
	return &iffPlan{riffs: riffs, chunks: inner, padBefore: padBefore, sizePos: lastOff + 4, sizeVal: lastSize}, lastChunk, nil
}

type aviHandler struct{}

func (aviHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "AVI",
		Extensions: []string{".avi"},
		MinDelta:   38,
		MaxDelta:   39,
		Mechanism:  "JUNK chunk at the end of the last RIFF list, its size grown",
	}
}

func (aviHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	plan, _, err := parseAVI(src, srcSize)
	if err == nil && int64(plan.sizeVal)+39 > 0xFFFFFFFF {
		err = fmt.Errorf("last RIFF would exceed the 32-bit size limit")
	}
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid AVI: %v", err)}
	}
	return plan, nil
}

func (aviHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*iffPlan)
	payload := tagPayload(nonce)
	chunk := make([]byte, 8+len(payload))
	copy(chunk[0:4], "JUNK")
	binary.LittleEndian.PutUint32(chunk[4:8], uint32(len(payload)))
	copy(chunk[8:], payload)
	if plan.padBefore {
		chunk = append([]byte{0}, chunk...)
	}
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], plan.sizeVal+uint32(len(chunk)))
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, []byteEdit{{plan.sizePos, sz[:]}}, appendAt(srcSize, chunk))
	if err != nil {
		return o, 0, h, err
	}
	outPlan, lastID, err := parseAVI(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("AVI output failed strict RIFF verification: %w", err)
	}
	if outPlan.riffs != plan.riffs || outPlan.chunks != plan.chunks+1 || lastID != "JUNK" {
		return o, 0, h, fmt.Errorf("AVI output chunk layout mismatch")
	}
	return o, outSize, h, nil
}

// ---- Matroska / WebM (RFC 8794 EBML: Void element at the end of Segment) ---

func ebmlVint(b []byte) (val uint64, n int, unknown bool, ok bool) {
	if len(b) == 0 || b[0] == 0 {
		return 0, 0, false, false
	}
	n = bits.LeadingZeros8(b[0]) + 1
	if len(b) < n {
		return 0, 0, false, false
	}
	val = uint64(b[0] & (0xFF >> n))
	for i := 1; i < n; i++ {
		val = val<<8 | uint64(b[i])
	}
	return val, n, val == (uint64(1)<<(7*n))-1, true
}

type ebmlElem struct {
	id        uint32
	sizePos   int64
	sizeLen   int
	size      uint64
	unknown   bool
	dataStart int64
}

func readEBMLElem(r io.ReaderAt, pos, limit int64) (ebmlElem, error) {
	n := int64(12)
	if limit-pos < n {
		n = limit - pos
	}
	if n < 2 {
		return ebmlElem{}, fmt.Errorf("truncated EBML element at offset %d", pos)
	}
	b, err := readAt(r, pos, int(n))
	if err != nil {
		return ebmlElem{}, err
	}
	if b[0] == 0 {
		return ebmlElem{}, fmt.Errorf("invalid EBML id at offset %d", pos)
	}
	idLen := bits.LeadingZeros8(b[0]) + 1
	if idLen > 4 || idLen >= len(b) {
		return ebmlElem{}, fmt.Errorf("invalid EBML id at offset %d", pos)
	}
	var id uint32
	for i := 0; i < idLen; i++ {
		id = id<<8 | uint32(b[i])
	}
	size, sl, unknown, ok := ebmlVint(b[idLen:])
	if !ok {
		return ebmlElem{}, fmt.Errorf("invalid EBML size at offset %d", pos)
	}
	e := ebmlElem{id: id, sizePos: pos + int64(idLen), sizeLen: sl, size: size, unknown: unknown, dataStart: pos + int64(idLen+sl)}
	if !unknown && (size > uint64(limit) || e.dataStart+int64(size) > limit) {
		return ebmlElem{}, fmt.Errorf("EBML element 0x%X at offset %d extends past its parent", id, pos)
	}
	return e, nil
}

type mkvPlan struct {
	segment  ebmlElem
	children int
	open     bool // a child of unknown size runs to EOF (live recording)
}

func parseMKV(r io.ReaderAt, size int64) (*mkvPlan, error) {
	hdr, err := readEBMLElem(r, 0, size)
	if err != nil {
		return nil, err
	}
	if hdr.id != 0x1A45DFA3 || hdr.unknown {
		return nil, fmt.Errorf("missing EBML header")
	}
	hdrEnd := hdr.dataStart + int64(hdr.size)
	docType := ""
	for pos := hdr.dataStart; pos < hdrEnd; {
		e, err := readEBMLElem(r, pos, hdrEnd)
		if err != nil || e.unknown {
			return nil, fmt.Errorf("invalid EBML header element")
		}
		if e.id == 0x4282 && e.size <= 64 {
			b, err := readAt(r, e.dataStart, int(e.size))
			if err != nil {
				return nil, err
			}
			docType = strings.TrimRight(string(b), "\x00")
		}
		pos = e.dataStart + int64(e.size)
	}
	if docType != "matroska" && docType != "webm" {
		return nil, fmt.Errorf("EBML DocType %q is not matroska or webm", docType)
	}
	seg, err := readEBMLElem(r, hdrEnd, size)
	if err != nil {
		return nil, err
	}
	if seg.id != 0x18538067 {
		return nil, fmt.Errorf("missing Segment after EBML header")
	}
	if !seg.unknown && seg.dataStart+int64(seg.size) != size {
		return nil, fmt.Errorf("data after the Segment end")
	}
	plan := &mkvPlan{segment: seg}
	pos := seg.dataStart
	for pos < size {
		e, err := readEBMLElem(r, pos, size)
		if err != nil {
			return nil, err
		}
		plan.children++
		if e.unknown {
			plan.open = true
			break
		}
		pos = e.dataStart + int64(e.size)
	}
	if plan.children == 0 {
		return nil, fmt.Errorf("empty Segment")
	}
	return plan, nil
}

func mkvLastChildID(r io.ReaderAt, plan *mkvPlan, size int64) uint32 {
	pos := plan.segment.dataStart
	var id uint32
	for pos < size {
		e, err := readEBMLElem(r, pos, size)
		if err != nil || e.unknown {
			return 0
		}
		id = e.id
		pos = e.dataStart + int64(e.size)
	}
	return id
}

type mkvHandler struct{}

func (mkvHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "Matroska",
		Extensions: []string{".mkv", ".mka", ".webm"},
		MinDelta:   32,
		MaxDelta:   32,
		Mechanism:  "EBML Void element at the end of the Segment, Segment size grown",
	}
}

func (mkvHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	plan, err := parseMKV(src, srcSize)
	if err == nil && !plan.segment.unknown && plan.segment.size+32 >= (uint64(1)<<(7*plan.segment.sizeLen))-1 {
		err = fmt.Errorf("Segment size field cannot hold the grown size")
	}
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid Matroska/WebM: %v", err)}
	}
	return plan, nil
}

func (mkvHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*mkvPlan)
	payload := tagPayload(nonce)
	void := append([]byte{0xEC, 0x80 | byte(len(payload))}, payload...)
	var edits []byteEdit
	if !plan.segment.unknown {
		n := plan.segment.sizeLen
		v := (plan.segment.size + uint64(len(void))) | uint64(1)<<(7*n)
		b := make([]byte, n)
		for i := n - 1; i >= 0; i-- {
			b[i] = byte(v)
			v >>= 8
		}
		edits = append(edits, byteEdit{plan.segment.sizePos, b})
	}
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, edits, appendAt(srcSize, void))
	if err != nil {
		return o, 0, h, err
	}
	outPlan, err := parseMKV(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("Matroska output failed strict EBML verification: %w", err)
	}
	if outPlan.open != plan.open || (!plan.open && (outPlan.children != plan.children+1 || mkvLastChildID(dst, outPlan, outSize) != 0xEC)) {
		return o, 0, h, fmt.Errorf("Matroska output element layout mismatch")
	}
	return o, outSize, h, nil
}

// ---- MPEG transport stream (ISO/IEC 13818-1: null packet PID 0x1FFF) -------

type tsPlan struct {
	packet int    // 188, or 192 for M2TS with TP_extra_header
	prefix []byte // TP_extra_header copied from the last packet
}

func checkTS(r io.ReaderAt, size int64, ps int) error {
	if size%int64(ps) != 0 || size < int64(2*ps) {
		return fmt.Errorf("file size is not a whole number of %d-byte packets", ps)
	}
	n := size / int64(ps)
	sample := int64((1 << 20) / ps)
	sync := int64(ps - 188)
	b := make([]byte, 1)
	for i := int64(0); i < n; i++ {
		if i == sample && n-sample > i {
			i = n - sample
		}
		if _, err := r.ReadAt(b, i*int64(ps)+sync); err != nil {
			return err
		}
		if b[0] != 0x47 {
			return fmt.Errorf("lost sync byte at packet %d", i)
		}
	}
	return nil
}

type tsHandler struct{}

func (tsHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "MPEG-TS",
		Extensions: []string{".ts", ".m2ts", ".mts"},
		MinDelta:   188,
		MaxDelta:   192,
		Mechanism:  "null packet (PID 0x1FFF) appended",
	}
}

func (tsHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	head, err := readAt(src, 0, 8)
	if err != nil || (head[0] != 0x47 && head[4] != 0x47) {
		return nil, &SkipError{Code: "unsupported-format", Reason: "not an MPEG transport stream (no 0x47 sync byte)"}
	}
	ps := 188
	if head[0] != 0x47 {
		ps = 192
	}
	if err := checkTS(src, srcSize, ps); err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid MPEG-TS: %v", err)}
	}
	plan := &tsPlan{packet: ps}
	if ps == 192 {
		if plan.prefix, err = readAt(src, srcSize-192, 4); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (tsHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*tsPlan)
	pkt := make([]byte, 188)
	pkt[0], pkt[1], pkt[2], pkt[3] = 0x47, 0x1F, 0xFF, 0x10
	for i := 4; i < 188; i++ {
		pkt[i] = 0xFF
	}
	copy(pkt[4:], tagPayload(nonce))
	pkt = append(append([]byte{}, plan.prefix...), pkt...)
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, nil, appendAt(srcSize, pkt))
	if err != nil {
		return o, 0, h, err
	}
	if err := checkTS(dst, outSize, plan.packet); err != nil {
		return o, 0, h, fmt.Errorf("MPEG-TS output failed strict packet verification: %w", err)
	}
	last, err := readAt(dst, outSize-188, 4)
	if err != nil || last[0] != 0x47 || last[1]&0x1F != 0x1F || last[2] != 0xFF {
		return o, 0, h, fmt.Errorf("MPEG-TS output null packet missing")
	}
	return o, outSize, h, nil
}

// ---- MPEG program stream (ISO/IEC 13818-1: padding_stream PES 0xBE) --------

type psInfo struct {
	packs, packets int
	endAt          int64 // offset of the program end code, -1 when absent
	lastPack       []byte
}

func walkMPEGPS(r io.ReaderAt, size int64) (*psInfo, error) {
	br := bufio.NewReaderSize(io.NewSectionReader(r, 0, size), 1<<20)
	info := &psInfo{endAt: -1}
	pos := int64(0)
	for pos < size {
		b, err := br.Peek(4)
		if err != nil {
			return nil, fmt.Errorf("truncated start code at offset %d", pos)
		}
		if b[3] != 0xB9 {
			if b, err = br.Peek(6); err != nil {
				return nil, fmt.Errorf("truncated packet header at offset %d", pos)
			}
		}
		if b[0] != 0 || b[1] != 0 || b[2] != 1 {
			return nil, fmt.Errorf("lost start code at offset %d", pos)
		}
		if pos == 0 && b[3] != 0xBA {
			return nil, fmt.Errorf("stream does not start with a pack header")
		}
		var n int
		switch id := b[3]; {
		case id == 0xBA:
			switch {
			case b[4]&0xC0 == 0x40:
				h, err := br.Peek(14)
				if err != nil {
					return nil, fmt.Errorf("truncated pack header at offset %d", pos)
				}
				n = 14 + int(h[13]&7)
			case b[4]&0xF0 == 0x20:
				n = 12
			default:
				return nil, fmt.Errorf("unknown pack header at offset %d", pos)
			}
			h, err := br.Peek(n)
			if err != nil {
				return nil, fmt.Errorf("truncated pack header at offset %d", pos)
			}
			info.lastPack = append(info.lastPack[:0], h...)
			info.packs++
		case id == 0xB9:
			if pos+4 != size {
				return nil, fmt.Errorf("data after the program end code")
			}
			info.endAt = pos
			n = 4
		case id == 0xBB || id >= 0xBC:
			l := int(binary.BigEndian.Uint16(b[4:6]))
			if l == 0 {
				return nil, fmt.Errorf("zero-length packet at offset %d", pos)
			}
			n = 6 + l
			if id != 0xBB {
				info.packets++
			}
		default:
			return nil, fmt.Errorf("unexpected start code 0x%02X at offset %d", id, pos)
		}
		if int64(n) > size-pos {
			return nil, fmt.Errorf("packet at offset %d extends past EOF", pos)
		}
		if _, err := br.Discard(n); err != nil {
			return nil, err
		}
		pos += int64(n)
	}
	if info.packs == 0 || info.packets == 0 {
		return nil, fmt.Errorf("no packs or packets")
	}
	return info, nil
}

type psPlan struct {
	info    *psInfo
	sector  bool
	padding []byte
}

type psHandler struct{}

func (psHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "MPEG-PS",
		Extensions: []string{".mpg", ".mpeg", ".vob"},
		MinDelta:   36,
		MaxDelta:   2048,
		Mechanism:  "padding_stream packet (0xBE) before the program end; a full 2048-byte padding pack for DVD .vob",
	}
}

func (psHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	info, err := walkMPEGPS(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid MPEG-PS: %v", err)}
	}
	plan := &psPlan{info: info}
	pes := func(n int) []byte {
		b := make([]byte, 6+n)
		b[2], b[3] = 1, 0xBE
		binary.BigEndian.PutUint16(b[4:6], uint16(n))
		for i := 6; i < len(b); i++ {
			b[i] = 0xFF
		}
		return b
	}
	if strings.EqualFold(filepath.Ext(src.Name()), ".vob") && srcSize%2048 == 0 && len(info.lastPack)+6 < 2048 {
		plan.sector = true
		plan.padding = append(append([]byte{}, info.lastPack...), pes(2048-len(info.lastPack)-6)...)
	} else {
		plan.padding = pes(30)
	}
	return plan, nil
}

func (psHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, _ string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*psPlan)
	at := srcSize
	if plan.info.endAt >= 0 {
		at = plan.info.endAt
	}
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, nil, byteInsert{at: at, b: plan.padding})
	if err != nil {
		return o, 0, h, err
	}
	out, err := walkMPEGPS(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("MPEG-PS output failed strict packet verification: %w", err)
	}
	wantPacks := plan.info.packs
	if plan.sector {
		wantPacks++
	}
	if out.packets != plan.info.packets+1 || out.packs != wantPacks {
		return o, 0, h, fmt.Errorf("MPEG-PS output packet layout mismatch")
	}
	return o, outSize, h, nil
}

// ---- MP3 (ID3v2.3/2.4: TXXX frame added, tag size grown) -------------------

type id3Info struct {
	present bool
	major   byte
	end     int64 // first byte after the tag (audio start)
	frames  int
}

func synchsafe(b []byte) (uint32, bool) {
	var v uint32
	for _, c := range b {
		if c&0x80 != 0 {
			return 0, false
		}
		v = v<<7 | uint32(c)
	}
	return v, true
}

func putSynchsafe(b []byte, v uint32) {
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = byte(v & 0x7F)
		v >>= 7
	}
}

func isMPEGAudioFrame(b []byte) bool {
	return len(b) >= 4 && b[0] == 0xFF && b[1]&0xE0 == 0xE0 &&
		(b[1]>>3)&3 != 1 && (b[1]>>1)&3 != 0 && b[2]>>4 != 0xF && b[2]>>4 != 0 && (b[2]>>2)&3 != 3
}

func parseID3MP3(r io.ReaderAt, size int64) (*id3Info, error) {
	if size < 14 {
		return nil, fmt.Errorf("MP3 file too short")
	}
	h, err := readAt(r, 0, 10)
	if err != nil {
		return nil, err
	}
	info := &id3Info{}
	if string(h[0:3]) == "ID3" {
		info.present = true
		info.major = h[3]
		if info.major != 3 && info.major != 4 {
			return nil, fmt.Errorf("ID3v2.%d tag is not supported", info.major)
		}
		if h[5]&0xD0 != 0 {
			return nil, fmt.Errorf("ID3v2 tag uses unsynchronisation, an extended header or a footer")
		}
		ts, ok := synchsafe(h[6:10])
		if !ok {
			return nil, fmt.Errorf("ID3v2 tag size is not synchsafe")
		}
		info.end = 10 + int64(ts)
		if info.end+4 > size {
			return nil, fmt.Errorf("ID3v2 tag extends past EOF")
		}
		tag, err := readAt(r, 10, int(ts))
		if err != nil {
			return nil, err
		}
		pos := 0
		for pos+10 <= len(tag) && tag[pos] != 0 {
			for _, c := range tag[pos : pos+4] {
				if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
					return nil, fmt.Errorf("invalid ID3v2 frame id at tag offset %d", pos)
				}
			}
			var fs uint32
			if info.major == 4 {
				if fs, ok = synchsafe(tag[pos+4 : pos+8]); !ok {
					return nil, fmt.Errorf("ID3v2.4 frame size is not synchsafe")
				}
			} else {
				fs = binary.BigEndian.Uint32(tag[pos+4 : pos+8])
			}
			if int64(pos)+10+int64(fs) > int64(len(tag)) {
				return nil, fmt.Errorf("ID3v2 frame extends past the tag")
			}
			pos += 10 + int(fs)
			info.frames++
		}
		if len(bytes.Trim(tag[pos:], "\x00")) != 0 {
			return nil, fmt.Errorf("ID3v2 padding holds data")
		}
	}
	a, err := readAt(r, info.end, 4)
	if err != nil {
		return nil, err
	}
	if !isMPEGAudioFrame(a) {
		return nil, fmt.Errorf("no MPEG audio frame at offset %d", info.end)
	}
	return info, nil
}

type mp3Handler struct{}

func (mp3Handler) spec() FormatSpec {
	return FormatSpec{
		Name:       "MP3",
		Extensions: []string{".mp3"},
		MinDelta:   42,
		MaxDelta:   52,
		Mechanism:  "ID3v2 TXXX frame added (a new ID3v2.3 tag when the file has none); padding untouched",
	}
}

func (mp3Handler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	info, err := parseID3MP3(src, srcSize)
	if err == nil && info.present && info.end-10+42 >= 1<<28 {
		err = fmt.Errorf("ID3v2 tag cannot grow past 256 MiB")
	}
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid MP3: %v", err)}
	}
	return info, nil
}

func (mp3Handler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	info := p.(*id3Info)
	// TXXX: ISO-8859-1 encoding byte, unique description "FileDO-rehash:<nonce>",
	// NUL, empty value - 32 data bytes, 42 with the frame header.
	data := append(append([]byte{0}, tagPayload(nonce)...), 0)
	frame := make([]byte, 10+len(data))
	copy(frame[0:4], "TXXX")
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(data))) // < 128: same bytes as synchsafe
	copy(frame[10:], data)

	var edits []byteEdit
	var ins byteInsert
	if info.present {
		sz := make([]byte, 4)
		putSynchsafe(sz, uint32(info.end-10)+uint32(len(frame)))
		edits = append(edits, byteEdit{6, sz})
		ins = byteInsert{at: 10, b: frame}
	} else {
		hdr := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0}
		putSynchsafe(hdr[6:10], uint32(len(frame)))
		ins = byteInsert{at: 0, b: append(hdr, frame...)}
	}
	o, outSize, h, err := streamEdited(ctx, src, srcSize, dst, edits, ins)
	if err != nil {
		return o, 0, h, err
	}
	out, err := parseID3MP3(dst, outSize)
	if err != nil {
		return o, 0, h, fmt.Errorf("MP3 output failed strict tag verification: %w", err)
	}
	if !out.present || out.end != info.end+int64(len(ins.b)) || out.frames != info.frames+1 {
		return o, 0, h, fmt.Errorf("MP3 output tag layout mismatch")
	}
	first, err := readAt(dst, 10, 4)
	if err != nil || !bytes.Equal(first, []byte("TXXX")) {
		return o, 0, h, fmt.Errorf("MP3 output TXXX frame missing")
	}
	return o, outSize, h, nil
}
