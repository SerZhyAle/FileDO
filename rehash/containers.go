package rehash

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
)

// ---- ZIP / OOXML / ODF / EPUB (PKWARE EOCD archive comment) ----------------

type zipPlan struct {
	eocdOffset int64
	commentLen int
	keepLen    int // leading comment bytes carried over to the output
	entryCount int
	entryCRCs  []uint32
}

type zipHandler struct{}

func (zipHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "ZIP",
		Extensions: []string{".zip", ".docx", ".xlsx", ".pptx", ".odt", ".ods", ".odp", ".epub"},
		MinDelta:   -65535, // negative only when a full comment drops earlier tags
		MaxDelta:   31,
		Mechanism:  "ZIP EOCD archive comment",
	}
}

// stripZipTags removes the rehash tags that end an archive comment.
func stripZipTags(comment string) string {
	const tagLen = len(tagPrefix) + nonceBytes*2
	for len(comment) >= tagLen && strings.HasPrefix(comment[len(comment)-tagLen:], tagPrefix) {
		comment = strings.TrimSuffix(comment[:len(comment)-tagLen], "\n")
	}
	return comment
}

func isSignedZipEntry(name string) bool {
	n := strings.ToLower(strings.ReplaceAll(name, `\`, "/"))
	if strings.HasPrefix(n, "_xmlsignatures/") || n == "meta-inf/signatures.xml" {
		return true
	}
	if strings.HasPrefix(n, "meta-inf/") {
		if strings.HasSuffix(n, ".sf") || strings.HasSuffix(n, ".rsa") ||
			strings.HasSuffix(n, ".dsa") || strings.HasSuffix(n, ".ec") {
			return true
		}
	}
	return false
}

func (zipHandler) prepare(ctx context.Context, src *os.File, srcSize int64) (any, error) {
	if srcSize < 22 {
		return nil, &SkipError{Code: "invalid-format", Reason: "invalid ZIP: file too short"}
	}
	var lead [4]byte
	if _, err := src.ReadAt(lead[:], 0); err != nil {
		return nil, err
	}
	if !bytes.Equal(lead[:], []byte{'P', 'K', 0x03, 0x04}) && !bytes.Equal(lead[:], []byte{'P', 'K', 0x05, 0x06}) {
		return nil, &SkipError{Code: "invalid-format", Reason: "ZIP has non-ZIP leading bytes (self-extracting or unknown header)"}
	}

	zr, err := zip.NewReader(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid ZIP: %v", err)}
	}

	crcs := make([]uint32, len(zr.File))
	for i, f := range zr.File {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if isSignedZipEntry(f.Name) {
			return nil, &SkipError{Code: "signed-file", Reason: "ZIP/document contains digital signature metadata that would be invalidated"}
		}
		crcs[i] = f.CRC32
	}

	commentLen := len(zr.Comment)
	eocdOffset := srcSize - 22 - int64(commentLen)
	if eocdOffset < 0 {
		return nil, &SkipError{Code: "invalid-format", Reason: "ZIP EOCD offset out of range"}
	}
	var eocd [22]byte
	if _, err := src.ReadAt(eocd[:], eocdOffset); err != nil {
		return nil, err
	}
	if !bytes.Equal(eocd[0:4], []byte{'P', 'K', 0x05, 0x06}) || int(binary.LittleEndian.Uint16(eocd[20:22])) != commentLen {
		return nil, &SkipError{Code: "invalid-format", Reason: "ZIP has trailing data after EOCD or non-standard EOCD layout"}
	}

	addLen := func(keep int) int {
		if keep > 0 {
			return len(tagPrefix) + nonceBytes*2 + 1 // leading '\n' -> 31
		}
		return len(tagPrefix) + nonceBytes*2 // 30
	}
	// A comment filled up by earlier runs drops their tags, so a run can be
	// repeated without end; the archive's own comment text stays.
	keep := commentLen
	if keep+addLen(keep) > 65535 {
		keep = len(stripZipTags(zr.Comment))
		if n := keep + addLen(keep); n > 65535 || n == commentLen {
			return nil, &SkipError{Code: "invalid-format", Reason: "ZIP archive comment cannot grow past 65535 bytes"}
		}
	}

	return &zipPlan{
		eocdOffset: eocdOffset,
		commentLen: commentLen,
		keepLen:    keep,
		entryCount: len(zr.File),
		entryCRCs:  crcs,
	}, nil
}

func (zipHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*zipPlan)
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	origH := sha256.New()
	outH := sha256.New()
	prefixLen := plan.eocdOffset + 20 // everything before the 2-byte EOCD comment length field
	if _, err := copyChecked(ctx, io.MultiWriter(dst, outH), io.TeeReader(src, origH), prefixLen); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	tail := make([]byte, 2+plan.commentLen)
	if _, err := io.ReadFull(io.TeeReader(src, origH), tail); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	tag := tagPrefix + nonce
	added := tag
	if plan.keepLen > 0 {
		added = "\n" + tag
	}
	newCommentLen := plan.keepLen + len(added)
	var lenBuf [2]byte
	binary.LittleEndian.PutUint16(lenBuf[:], uint16(newCommentLen))

	outTail := make([]byte, 0, 2+newCommentLen)
	outTail = append(outTail, lenBuf[:]...)
	outTail = append(outTail, tail[2:2+plan.keepLen]...)
	outTail = append(outTail, added...)
	if _, err := io.MultiWriter(dst, outH).Write(outTail); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	outSize := srcSize + int64(newCommentLen-plan.commentLen)
	ozr, err := zip.NewReader(dst, outSize)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("ZIP output failed strict archive read: %w", err)
	}
	if len(ozr.File) != plan.entryCount {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("ZIP output entry count mismatch")
	}
	if !strings.HasSuffix(ozr.Comment, tag) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("ZIP output archive comment missing tag")
	}
	for i, f := range ozr.File {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
			}
		}
		if f.CRC32 != plan.entryCRCs[i] {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("ZIP entry %q CRC mismatch", f.Name)
		}
		rc, err := f.Open()
		if err != nil {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("ZIP output entry %q cannot be opened: %w", f.Name, err)
		}
		_, cerr := copyChecked(ctx, io.Discard, rc, -1)
		closeErr := rc.Close()
		if cerr != nil {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("ZIP output entry %q failed CRC/decompress verification: %w", f.Name, cerr)
		}
		if closeErr != nil {
			return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, closeErr
		}
	}

	var origSum, outSum [sha256.Size]byte
	copy(origSum[:], origH.Sum(nil))
	copy(outSum[:], outH.Sum(nil))
	return origSum, outSize, outSum, nil
}

// ---- WAV (Microsoft RIFF WAVE: JUNK chunk appended at EOF) -----------------

func walkWAV(r io.ReadSeeker, totalSize int64) (riffSize uint32, chunks int, lastFourCC string, err error) {
	if totalSize < 12 {
		return 0, 0, "", fmt.Errorf("WAV file too short")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, 0, "", err
	}
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, 0, "", err
	}
	if string(hdr[0:4]) != "RIFF" || string(hdr[8:12]) != "WAVE" {
		return 0, 0, "", fmt.Errorf("missing RIFF/WAVE header")
	}
	riffSize = binary.LittleEndian.Uint32(hdr[4:8])
	if int64(riffSize)+8 != totalSize {
		return 0, 0, "", fmt.Errorf("RIFF size %d does not match file size %d", int64(riffSize)+8, totalSize)
	}
	pos := int64(12)
	hasFmt, hasData := false, false
	var chdr [8]byte
	for pos < totalSize {
		if totalSize-pos < 8 {
			return 0, 0, "", fmt.Errorf("truncated WAV chunk header at offset %d", pos)
		}
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return 0, 0, "", err
		}
		if _, err := io.ReadFull(r, chdr[:]); err != nil {
			return 0, 0, "", err
		}
		fourCC := string(chdr[0:4])
		csize := int64(binary.LittleEndian.Uint32(chdr[4:8]))
		step := 8 + csize + (csize & 1)
		if step > totalSize-pos {
			return 0, 0, "", fmt.Errorf("WAV chunk %q extends past EOF at offset %d", fourCC, pos)
		}
		if fourCC == "fmt " {
			hasFmt = true
		} else if fourCC == "data" {
			hasData = true
		}
		lastFourCC = fourCC
		chunks++
		pos += step
	}
	if !hasFmt || !hasData {
		return 0, 0, "", fmt.Errorf("WAV missing required fmt or data chunk")
	}
	return riffSize, chunks, lastFourCC, nil
}

type wavPlan struct {
	riffSize   uint32
	chunkCount int
}

type wavHandler struct{}

func (wavHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "WAV",
		Extensions: []string{".wav"},
		MinDelta:   38,
		MaxDelta:   38,
		Mechanism:  "RIFF JUNK chunk at EOF",
	}
}

func (wavHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	if srcSize+38 > int64(0xFFFFFFFF)+8 {
		return nil, &SkipError{Code: "invalid-format", Reason: "WAV file would exceed 32-bit RIFF size limit"}
	}
	riffSize, chunks, _, err := walkWAV(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid WAV: %v", err)}
	}
	return &wavPlan{riffSize: riffSize, chunkCount: chunks}, nil
}

func (wavHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*wavPlan)
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	origH := sha256.New()
	outH := sha256.New()

	var hdr [12]byte
	if _, err := io.ReadFull(io.TeeReader(src, origH), hdr[:]); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	outHdr := hdr
	binary.LittleEndian.PutUint32(outHdr[4:8], plan.riffSize+38)
	if _, err := io.MultiWriter(dst, outH).Write(outHdr[:]); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	if _, err := copyChecked(ctx, io.MultiWriter(dst, outH), io.TeeReader(src, origH), srcSize-12); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	// JUNK chunk: "JUNK" (4) + LE uint32 30 (4) + "FileDO-rehash:<16 hex>" (30, even) = 38 bytes.
	payload := tagPrefix + nonce
	junk := make([]byte, 8+len(payload))
	copy(junk[0:4], "JUNK")
	binary.LittleEndian.PutUint32(junk[4:8], uint32(len(payload)))
	copy(junk[8:], payload)
	if _, err := io.MultiWriter(dst, outH).Write(junk); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	outSize := srcSize + int64(len(junk))
	outRiff, outChunks, lastCC, err := walkWAV(dst, outSize)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("WAV output failed strict RIFF verification: %w", err)
	}
	if outRiff != plan.riffSize+38 || outChunks != plan.chunkCount+1 || lastCC != "JUNK" {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("WAV output chunk layout mismatch")
	}

	var origSum, outSum [sha256.Size]byte
	copy(origSum[:], origH.Sum(nil))
	copy(outSum[:], outH.Sum(nil))
	return origSum, outSize, outSum, nil
}

// ---- MP4 / M4A / M4V / MOV (ISO/IEC 14496-12: top-level free box at EOF) ---

func isPrintableFourCC(b []byte) bool {
	if len(b) != 4 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return true
}

func walkMP4Boxes(r io.ReadSeeker, totalSize int64) (count int, lastType string, err error) {
	count, lastType, seen, err := walkISOBoxes(r, totalSize)
	if err != nil {
		return 0, "", err
	}
	if !seen["moov"] || !seen["mdat"] {
		return 0, "", fmt.Errorf("MP4/MOV missing required ftyp, moov, or mdat box")
	}
	return count, lastType, nil
}

// walkISOBoxes walks the top-level ISO/IEC 14496-12 boxes; the first must be
// ftyp and the boxes must tile the file exactly.
func walkISOBoxes(r io.ReadSeeker, totalSize int64) (count int, lastType string, seen map[string]bool, err error) {
	if totalSize < 16 {
		return 0, "", nil, fmt.Errorf("ISO media file too short")
	}
	pos := int64(0)
	seen = map[string]bool{}
	var hdr [8]byte
	var ext [8]byte
	for pos < totalSize {
		if totalSize-pos < 8 {
			return 0, "", nil, fmt.Errorf("truncated MP4 box header at offset %d", pos)
		}
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return 0, "", nil, err
		}
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return 0, "", nil, err
		}
		if !isPrintableFourCC(hdr[4:8]) {
			return 0, "", nil, fmt.Errorf("non-printable MP4 box type at offset %d", pos)
		}
		boxType := string(hdr[4:8])
		size32 := binary.BigEndian.Uint32(hdr[0:4])
		var boxSize int64
		switch size32 {
		case 0:
			return 0, "", nil, fmt.Errorf("open-ended MP4 box %q (size 0) cannot be safely extended", boxType)
		case 1:
			if totalSize-pos < 16 {
				return 0, "", nil, fmt.Errorf("truncated 64-bit MP4 box %q at offset %d", boxType, pos)
			}
			if _, err := io.ReadFull(r, ext[:]); err != nil {
				return 0, "", nil, err
			}
			u64 := binary.BigEndian.Uint64(ext[:])
			if u64 < 16 || u64 > uint64(totalSize-pos) {
				return 0, "", nil, fmt.Errorf("invalid 64-bit MP4 box %q size at offset %d", boxType, pos)
			}
			boxSize = int64(u64)
		default:
			if size32 < 8 || int64(size32) > totalSize-pos {
				return 0, "", nil, fmt.Errorf("invalid MP4 box %q size %d at offset %d", boxType, size32, pos)
			}
			boxSize = int64(size32)
		}
		if count == 0 && boxType != "ftyp" {
			return 0, "", nil, fmt.Errorf("MP4/MOV does not start with ftyp box (found %q)", boxType)
		}
		seen[boxType] = true
		lastType = boxType
		count++
		pos += boxSize
	}
	return count, lastType, seen, nil
}

type mp4Plan struct {
	boxCount int
}

type mp4Handler struct{}

func (mp4Handler) spec() FormatSpec {
	return FormatSpec{
		Name:       "MP4",
		Extensions: []string{".mp4", ".m4a", ".m4v", ".mov", ".3gp", ".3g2", ".cr3"},
		MinDelta:   38,
		MaxDelta:   38,
		Mechanism:  "top-level free box at EOF",
	}
}

func (mp4Handler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	count, _, err := walkMP4Boxes(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid MP4/MOV: %v", err)}
	}
	return &mp4Plan{boxCount: count}, nil
}

func (mp4Handler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, nonce string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*mp4Plan)
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	origH := sha256.New()
	outH := sha256.New()

	if _, err := copyChecked(ctx, io.MultiWriter(dst, outH), io.TeeReader(src, origH), srcSize); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	// free box: BE uint32 38 (4) + "free" (4) + "FileDO-rehash:<16 hex>" (30) = 38 bytes.
	payload := tagPrefix + nonce
	box := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(box[0:4], uint32(len(box)))
	copy(box[4:8], "free")
	copy(box[8:], payload)
	if _, err := io.MultiWriter(dst, outH).Write(box); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	outSize := srcSize + int64(len(box))
	outCount, lastType, err := walkMP4Boxes(dst, outSize)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("MP4 output failed strict box verification: %w", err)
	}
	if outCount != plan.boxCount+1 || lastType != "free" {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("MP4 output box layout mismatch")
	}

	var origSum, outSum [sha256.Size]byte
	copy(origSum[:], origH.Sum(nil))
	copy(outSum[:], outH.Sum(nil))
	return origSum, outSize, outSum, nil
}

// ---- FLAC (RFC 9639: PADDING metadata block before audio frames) -----------

func walkFLAC(r io.ReadSeeker, totalSize int64) (lastHeaderOffset, audioOffset int64, blockCount int, err error) {
	if totalSize < 4+4+34+2 {
		return 0, 0, 0, fmt.Errorf("FLAC file too short")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return 0, 0, 0, err
	}
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return 0, 0, 0, err
	}
	if string(magic[:]) != "fLaC" {
		return 0, 0, 0, fmt.Errorf("missing fLaC signature")
	}
	pos := int64(4)
	var bh [4]byte
	for pos < totalSize {
		if totalSize-pos < 4 {
			return 0, 0, 0, fmt.Errorf("truncated FLAC metadata header at offset %d", pos)
		}
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return 0, 0, 0, err
		}
		if _, err := io.ReadFull(r, bh[:]); err != nil {
			return 0, 0, 0, err
		}
		isLast := (bh[0] & 0x80) != 0
		btype := bh[0] & 0x7F
		if btype == 127 {
			return 0, 0, 0, fmt.Errorf("invalid FLAC metadata block type 127")
		}
		blen := int64(bh[1])<<16 | int64(bh[2])<<8 | int64(bh[3])
		if blockCount == 0 && (btype != 0 || blen != 34) {
			return 0, 0, 0, fmt.Errorf("FLAC first metadata block is not 34-byte STREAMINFO")
		}
		next := pos + 4 + blen
		if next > totalSize {
			return 0, 0, 0, fmt.Errorf("FLAC metadata block extends past EOF at offset %d", pos)
		}
		blockCount++
		if isLast {
			if totalSize-next < 2 {
				return 0, 0, 0, fmt.Errorf("FLAC missing audio frames after metadata")
			}
			var sync [2]byte
			if _, err := r.Seek(next, io.SeekStart); err != nil {
				return 0, 0, 0, err
			}
			if _, err := io.ReadFull(r, sync[:]); err != nil {
				return 0, 0, 0, err
			}
			if sync[0] != 0xFF || (sync[1]&0xFE) != 0xF8 {
				return 0, 0, 0, fmt.Errorf("FLAC audio frame sync code not found at offset %d", next)
			}
			return pos, next, blockCount, nil
		}
		pos = next
	}
	return 0, 0, 0, fmt.Errorf("FLAC metadata missing is-last flag")
}

type flacPlan struct {
	lastHeaderOffset int64
	audioOffset      int64
	blockCount       int
}

type flacHandler struct{}

func (flacHandler) spec() FormatSpec {
	return FormatSpec{
		Name:       "FLAC",
		Extensions: []string{".flac"},
		MinDelta:   20,
		MaxDelta:   20,
		Mechanism:  "PADDING metadata block before audio frames",
	}
}

func (flacHandler) prepare(_ context.Context, src *os.File, srcSize int64) (any, error) {
	lastOff, audioOff, count, err := walkFLAC(src, srcSize)
	if err != nil {
		return nil, &SkipError{Code: "invalid-format", Reason: fmt.Sprintf("invalid FLAC: %v", err)}
	}
	if audioOff > MaxInMemoryBytes {
		return nil, &SkipError{Code: "too-large", Reason: "FLAC metadata section exceeds memory limit"}
	}
	return &flacPlan{lastHeaderOffset: lastOff, audioOffset: audioOff, blockCount: count}, nil
}

func (flacHandler) writeAndVerify(ctx context.Context, src *os.File, srcSize int64, p any, dst *os.File, _ string) ([sha256.Size]byte, int64, [sha256.Size]byte, error) {
	plan := p.(*flacPlan)
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	origH := sha256.New()
	outH := sha256.New()

	meta := make([]byte, plan.audioOffset)
	if _, err := io.ReadFull(io.TeeReader(src, origH), meta); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	outMeta := append([]byte(nil), meta...)
	outMeta[plan.lastHeaderOffset] &^= 0x80 // clear is-last on previous final metadata block

	// Final PADDING block: header 0x81 0x00 0x00 0x10 (isLast=1, type=1, len=16) + 16 zero bytes = 20 bytes.
	padBlock := make([]byte, 20)
	padBlock[0] = 0x81
	padBlock[3] = 0x10

	mw := io.MultiWriter(dst, outH)
	if _, err := mw.Write(outMeta); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	if _, err := mw.Write(padBlock); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}
	if _, err := copyChecked(ctx, mw, io.TeeReader(src, origH), srcSize-plan.audioOffset); err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, err
	}

	outSize := srcSize + int64(len(padBlock))
	_, outAudioOff, outCount, err := walkFLAC(dst, outSize)
	if err != nil {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("FLAC output failed strict metadata verification: %w", err)
	}
	if outCount != plan.blockCount+1 || outAudioOff != plan.audioOffset+int64(len(padBlock)) {
		return [sha256.Size]byte{}, 0, [sha256.Size]byte{}, fmt.Errorf("FLAC output metadata layout mismatch")
	}

	var origSum, outSum [sha256.Size]byte
	copy(origSum[:], origH.Sum(nil))
	copy(outSum[:], outH.Sum(nil))
	return origSum, outSize, outSum, nil
}
