package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// The test-file format of the fake-capacity engine (SP-0026 CAP-03, CAP-15).
//
// A test file is a run of 4 KiB blocks. Every block begins with a readable tag
// naming its file, its run and its own offset - F<seq>_<run nonce>_<block> -
// and the rest of the block is xorshift output seeded by (run nonce, file
// name, block index). The file's first bytes are the header line and its last
// bytes are the same line again (the footer). That keeps the engine's three
// signals in the bytes themselves and makes each of them stronger:
//
//   - header == footer, and the header names the file it sits in;
//   - the body names its file, as the old F<seq>_ tag did, and now also its
//     offset and its run, so a controller that answers a read with another
//     address's data, or with what an earlier run left in the same cluster,
//     returns bytes the verifier can prove it never wrote there;
//   - the body is incompressible, so an NTFS-compressed folder or a
//     deduplicating share cannot store a whole test in a few megabytes.
//
// The verifier regenerates the expected bytes from the header alone. Files
// written before this format (header "FILEDO_TEST_<name>_<ts>") are still
// read by `fill verify` for one release, as a weaker check it names as such.

const (
	tfBlockSize    = 4096
	tfHeaderPrefix = "FILEDO_TEST2 "
	tfLegacyPrefix = "FILEDO_TEST_"
	// tfMaxHeaderLen bounds the search for the header's newline.
	tfMaxHeaderLen = 512
)

// errDataMismatch marks a read-back that returned bytes FileDO did not write
// at that offset - the one verification outcome that is evidence against the
// media. Everything else a verification can meet (a file that cannot be
// opened, a share that went away) proves nothing and is not wrapped with it.
var errDataMismatch = errors.New("data read back does not match what was written")

// testFileMeta is everything needed to regenerate a test file's bytes.
type testFileMeta struct {
	Name   string // base name, as written into the header
	Nonce  uint64 // the run nonce
	Size   int64
	Stamp  string
	Header []byte // the header line, newline included; also the footer
	tag    []byte // "F<seq>_<nonce>_", the start of every block's tag
	key    uint64
}

// newTestFileMeta builds the description of one test file. The name must not
// contain whitespace, because the header is space-separated.
func newTestFileMeta(name string, nonce uint64, size int64, stamp string) (*testFileMeta, error) {
	if name == "" || strings.ContainsAny(name, " \t\r\n") {
		return nil, fmt.Errorf("invalid test file name %q", name)
	}
	if stamp == "" || strings.ContainsAny(stamp, " \t\r\n") {
		return nil, fmt.Errorf("invalid test file stamp %q", stamp)
	}
	header := fmt.Sprintf("%s%s %016x %d %s\n", tfHeaderPrefix, name, nonce, size, stamp)
	if len(header) > tfMaxHeaderLen {
		return nil, fmt.Errorf("test file name %q is too long", name)
	}
	if size < int64(2*len(header)) {
		return nil, fmt.Errorf("a test file of %d bytes cannot hold its header and footer", size)
	}
	h := fnv.New64a()
	h.Write([]byte(name))
	return &testFileMeta{
		Name:   name,
		Nonce:  nonce,
		Size:   size,
		Stamp:  stamp,
		Header: []byte(header),
		tag:    []byte(fmt.Sprintf("F%s_%016x_", testFileSeq(name), nonce)),
		key:    splitmix64(nonce ^ h.Sum64()),
	}, nil
}

// testFileSeq is the sequence number a FILL_<seq>_... name carries, or "0".
func testFileSeq(name string) string {
	const prefix = "FILL_"
	if !strings.HasPrefix(name, prefix) {
		return "0"
	}
	rest := name[len(prefix):]
	end := strings.IndexAny(rest, "_.")
	if end <= 0 {
		return "0"
	}
	for _, c := range rest[:end] {
		if c < '0' || c > '9' {
			return "0"
		}
	}
	return rest[:end]
}

// parseTestFileHeader reads a current-format header line (newline included)
// back into the description that wrote it. The line must be exactly the one
// newTestFileMeta would produce, so a header damaged in any byte is refused.
func parseTestFileHeader(line string) (*testFileMeta, error) {
	if !strings.HasPrefix(line, tfHeaderPrefix) || !strings.HasSuffix(line, "\n") {
		return nil, fmt.Errorf("not a FileDO test file header")
	}
	fields := strings.Split(strings.TrimSuffix(line[len(tfHeaderPrefix):], "\n"), " ")
	if len(fields) != 4 {
		return nil, fmt.Errorf("malformed FileDO test file header")
	}
	nonce, err := strconv.ParseUint(fields[1], 16, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed FileDO test file header: %v", err)
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("malformed FileDO test file header: %v", err)
	}
	m, err := newTestFileMeta(fields[0], nonce, size, fields[3])
	if err != nil {
		return nil, err
	}
	if string(m.Header) != line {
		return nil, fmt.Errorf("malformed FileDO test file header")
	}
	return m, nil
}

// headerLine returns the first line of head (newline included) if one ends
// within the first tfMaxHeaderLen bytes.
func headerLine(head []byte) (string, bool) {
	limit := head
	if len(limit) > tfMaxHeaderLen {
		limit = limit[:tfMaxHeaderLen]
	}
	i := bytes.IndexByte(limit, '\n')
	if i < 0 {
		return "", false
	}
	return string(limit[:i+1]), true
}

// legacyHeaderName returns the file name an old-format header line names:
// "FILEDO_TEST_FILL_00001_021504.tmp_20260324_012341" names
// "FILL_00001_021504.tmp".
func legacyHeaderName(line string) (string, bool) {
	line = strings.TrimSuffix(line, "\n")
	if !strings.HasPrefix(line, tfLegacyPrefix) {
		return "", false
	}
	rest := line[len(tfLegacyPrefix):]
	idx := strings.Index(rest, ".tmp")
	if idx < 0 {
		return "", false
	}
	return rest[:idx] + ".tmp", true
}

func splitmix64(x uint64) uint64 {
	x += 0x9E3779B97F4A7C15
	x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9
	x = (x ^ (x >> 27)) * 0x94D049BB133111EB
	return x ^ (x >> 31)
}

// block writes block b of the file (before the header and footer overlays)
// into dst, which is exactly tfBlockSize long.
func (m *testFileMeta) block(dst []byte, b int64) {
	s := splitmix64(m.key + uint64(b)*0x9E3779B97F4A7C15)
	if s == 0 {
		s = 0x2545F4914F6CDD1D
	}
	for i := 0; i+8 <= len(dst); i += 8 {
		s ^= s << 13
		s ^= s >> 7
		s ^= s << 17
		binary.LittleEndian.PutUint64(dst[i:], s)
	}
	n := copy(dst, m.tag)
	const hexDigits = "0123456789abcdef"
	for i := 9; i >= 0; i-- {
		dst[n+i] = hexDigits[(uint64(b)>>(4*uint(9-i)))&0xF]
	}
	dst[n+10] = '\n'
}

// fill writes into dst the bytes the file holds at [off, off+len(dst)).
func (m *testFileMeta) fill(dst []byte, off int64) {
	var scratch [tfBlockSize]byte
	pos := off
	rest := dst
	for len(rest) > 0 {
		b := pos / tfBlockSize
		in := int(pos % tfBlockSize)
		n := tfBlockSize - in
		if n > len(rest) {
			n = len(rest)
		}
		if in == 0 && n == tfBlockSize {
			m.block(rest[:tfBlockSize], b)
		} else {
			m.block(scratch[:], b)
			copy(rest[:n], scratch[in:in+n])
		}
		rest = rest[n:]
		pos += int64(n)
	}
	overlayAt(dst, off, m.Header, 0)
	overlayAt(dst, off, m.Header, m.Size-int64(len(m.Header)))
}

// overlayAt copies src, which belongs at file offset at, into the part of dst
// (which holds file offset off onwards) that it overlaps.
func overlayAt(dst []byte, off int64, src []byte, at int64) {
	start, end := at, at+int64(len(src))
	if off > start {
		start = off
	}
	if dEnd := off + int64(len(dst)); dEnd < end {
		end = dEnd
	}
	if start >= end {
		return
	}
	copy(dst[start-off:end-off], src[start-at:end-at])
}

// tfMismatchError is a read-back that did not hold what was written.
type tfMismatchError struct {
	Path   string
	Offset int64
	What   string
}

func (e *tfMismatchError) Error() string {
	return fmt.Sprintf("%s: data at offset %d %s", e.Path, e.Offset, e.What)
}

func (e *tfMismatchError) Unwrap() error { return errDataMismatch }

// check compares got, read at file offset off, with what m wrote there. want
// is scratch space at least len(got) long.
func (m *testFileMeta) check(path string, got []byte, off int64, want []byte) error {
	want = want[:len(got)]
	m.fill(want, off)
	if bytes.Equal(got, want) {
		return nil
	}
	i := 0
	for i < len(got) && got[i] == want[i] {
		i++
	}
	at := off + int64(i)
	blockStart := (at / tfBlockSize) * tfBlockSize
	from := int(blockStart - off)
	if from < 0 {
		from = 0
	}
	to := from + tfBlockSize
	if to > len(got) {
		to = len(got)
	}
	return &tfMismatchError{Path: path, Offset: at, What: m.describeForeign(got[from:to])}
}

// describeForeign says, in words a user can act on, what a block that did not
// match holds instead. m may be nil when the file's own header is gone; the
// description then cannot tell this run from another.
func (m *testFileMeta) describeForeign(got []byte) string {
	switch {
	case len(got) == 0:
		return "is missing"
	case allBytes(got, 0x00):
		return "reads back as zeros - the write never reached the media"
	case allBytes(got, 0xFF):
		return "reads back as erased flash (0xFF) - the write never reached the media"
	}
	if line, ok := headerLine(got); ok {
		if other, err := parseTestFileHeader(line); err == nil {
			if m != nil && other.Name == m.Name {
				return "holds this file's header, but not the data written with it"
			}
			return fmt.Sprintf("holds the header of test file %s - another file's data", other.Name)
		}
		if name, ok := legacyHeaderName(line); ok {
			return fmt.Sprintf("holds the header of an older test file %s", name)
		}
	}
	if seq, nonce, blk, ok := parseBlockTag(got); ok {
		switch {
		case m == nil:
			return fmt.Sprintf("holds block %d of test file F%s - another file's data", blk, seq)
		case nonce == m.Nonce:
			return fmt.Sprintf("holds block %d of test file F%s of this run - the device returns another address's data", blk, seq)
		default:
			return fmt.Sprintf("holds block %d of test file F%s from an earlier run - the write never reached the media", blk, seq)
		}
	}
	return "holds data FileDO did not write there"
}

// parseBlockTag reads the tag at the start of a block.
func parseBlockTag(got []byte) (seq string, nonce uint64, blk int64, ok bool) {
	line, found := headerLine(got)
	if !found || len(line) < 2 || line[0] != 'F' {
		return "", 0, 0, false
	}
	parts := strings.Split(strings.TrimSuffix(line[1:], "\n"), "_")
	if len(parts) != 3 || len(parts[1]) != 16 || len(parts[2]) != 10 {
		return "", 0, 0, false
	}
	n, err := strconv.ParseUint(parts[1], 16, 64)
	if err != nil {
		return "", 0, 0, false
	}
	b, err := strconv.ParseInt(parts[2], 16, 64)
	if err != nil {
		return "", 0, 0, false
	}
	return parts[0], n, b, true
}

func allBytes(b []byte, v byte) bool {
	for _, c := range b {
		if c != v {
			return false
		}
	}
	return true
}
