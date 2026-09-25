package fdsec

import (
	"crypto/cipher"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"runtime"
	"strconv"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/text/unicode/norm"
)

// Suite 2 - the quiet suite (FDSEC-FORMAT.md section 18). It shares nothing on
// disk with suites 1 and 3: no head, no key slots, no cluster alignment. The
// file is
//
//	salt (16) || nonce (24) || frame 0 || frame 1 || .. || frame k-1
//
// and every frame is one XChaCha20-Poly1305 seal of up to 64 KiB of the body
// stream. The body stream is the owner's layout: a 1024-byte name block, a
// 12-byte timestamp, a 24-byte size block, the payload, and a random tail of
// 8..32 KiB. Nothing is stored in the clear but the salt and the nonce, which
// are random, so the file reads as uniform noise from byte 0.
//
// The key is Argon2id over a keyed-BLAKE2b fold of the credential under a
// fixed pepper. The pepper is not a secret (the source is public, SP-0019 D1)
// and nothing leans on it for confidentiality: it only means a dictionary
// cannot be run against suite 2 without this constant as well.

const (
	SuiteID2 = 2

	s2SaltSize   = 16
	s2NonceSize  = 24
	s2Lead       = s2SaltSize + s2NonceSize // 40: salt || nonce
	s2NameBlock  = 1024
	s2TimeBlock  = 12
	s2SizeBlock  = 24
	s2BodyHead   = s2NameBlock + s2TimeBlock + s2SizeBlock // 1060
	s2TailMin    = 8 << 10
	s2TailMax    = 32 << 10
	s2FramePlain = 64 << 10               // plaintext bytes per full frame
	s2Frame      = s2FramePlain + tagSize // on-disk bytes per full frame
	s2AD         = "FD-SEC/suite2/v1"     // associated-data prefix of every frame
	s2NameEnd    = 13                     // CR ends the name inside the name block
	s2SizeEnd    = '.'                    // '.' ends the decimal size inside the size block
	s2TimeLayout = "060102150405"         // YYMMDDHHmmss, UTC, YY = year - 2000

	// MaxNameBytesSuite2 is the longest true name suite 2 can seal: the name
	// block is 1024 bytes and one of them is the CR that ends the name.
	MaxNameBytesSuite2 = s2NameBlock - 1

	// suite2MinLen is the shortest possible suite-2 file: the lead, an empty
	// payload's body (head plus the shortest tail) and one tag.
	suite2MinLen = s2Lead + s2BodyHead + s2TailMin + tagSize // 9308
)

// suite2Pepper is the fixed 32-byte constant folded into every suite-2
// credential (FDSEC-FORMAT.md section 18.2). Drawn once from the OS CSPRNG on
// 2026-09-25 (SP-0019 D5) and frozen: changing one byte makes every suite-2
// file ever written unreadable.
var suite2Pepper = [32]byte{
	0x0e, 0x65, 0x71, 0x27, 0xf4, 0x33, 0xe1, 0x3f,
	0xfd, 0x72, 0x72, 0x81, 0x22, 0xfc, 0x6e, 0x78,
	0x3a, 0x41, 0x37, 0x66, 0xd0, 0x66, 0xfe, 0x42,
	0xdb, 0x5a, 0x50, 0x22, 0x05, 0xf0, 0xeb, 0xed,
}

// Suite2Profile is one Argon2id work factor of suite 2. Nothing on disk names
// it: the reader walks the try-list until frame 0 authenticates.
type Suite2Profile struct {
	MemoryKiB uint32
	Time      uint32
	Lanes     uint8
}

// suite2ProfilesV1 is the try-list of suite 2, current profile first. A
// profile is appended when a harder one becomes current and is never removed
// or changed: a file written under it keeps opening only while it is here
// (FDSEC-FORMAT.md section 18.2).
var suite2ProfilesV1 = []Suite2Profile{
	{MemoryKiB: 262144, Time: 3, Lanes: 4}, // pinned 2026-09-25, SP-0019 D3
}

// suite2Profiles is the try-list every shipped path uses; a test seam of the
// same kind as activeProfile, and assigned by nothing but tests.
var suite2Profiles = suite2ProfilesV1

// Suite2Profiles reports the try-list, current profile first.
func Suite2Profiles() []Suite2Profile { return append([]Suite2Profile(nil), suite2Profiles...) }

// deriveSuite2Key = Argon2id(BLAKE2b-512(key = pepper, msg = credential), salt,
// T, M, P, 32). There is no length threshold: every credential, empty
// included, takes the one derivation.
func deriveSuite2Key(cred Credential, salt []byte, p Suite2Profile) ([]byte, error) {
	h, err := blake2b.New512(suite2Pepper[:])
	if err != nil {
		return nil, err
	}
	h.Write(cred)
	// The 256 MiB this derivation takes is the largest allocation FileDO
	// makes, and under the dispatch it follows suite 1's 64 MiB. Collecting
	// first lets the heap reuse that block instead of reserving both, which
	// matters in the 32-bit build (SP-0019 D3 measurement).
	runtime.GC()
	return argon2.IDKey(h.Sum(nil), salt, p.Time, p.MemoryKiB, p.Lanes, fileKeySize), nil
}

// suite2FrameNonce XORs u64le(i) into the last eight bytes of the file nonce.
// The key is unique per file (the salt is), so a nonce unique per frame
// within the file is all the AEAD needs.
func suite2FrameNonce(nonce []byte, i int64) []byte {
	n := append([]byte(nil), nonce...)
	var ib [8]byte
	binary.LittleEndian.PutUint64(ib[:], uint64(i))
	for j := 0; j < 8; j++ {
		n[16+j] ^= ib[j]
	}
	return n
}

// suite2FrameAD = "FD-SEC/suite2/v1" || u64le(i) || u8(last). The index makes
// reordering detectable, the last flag makes truncation at a frame boundary
// detectable.
func suite2FrameAD(i int64, last bool) []byte {
	b := make([]byte, 0, len(s2AD)+9)
	b = append(b, s2AD...)
	b = binary.LittleEndian.AppendUint64(b, uint64(i))
	if last {
		return append(b, 1)
	}
	return append(b, 0)
}

// suite2Frames splits a file of n bytes into its frame count and the on-disk
// length of its last frame. ok is false when no suite-2 file has this length:
// shorter than the smallest one, or a last frame too short to hold a tag and
// one byte.
func suite2Frames(n int64) (k, last int64, ok bool) {
	if n < suite2MinLen {
		return 0, 0, false
	}
	l := n - s2Lead
	k = (l + s2Frame - 1) / s2Frame
	last = l - (k-1)*s2Frame
	return k, last, last > tagSize
}

// suite2LengthPossible reports whether a file of n bytes could be a suite-2
// file. Like ScreenLength it looks at nothing but the length.
func suite2LengthPossible(n int64) bool {
	_, _, ok := suite2Frames(n)
	return ok
}

// suite2Body is the parsed fixed head of the body stream.
type suite2Body struct {
	Name      string
	EncodedAt time.Time
	Size      int64
}

func buildSuite2BodyHead(name string, encodedAt time.Time, size int64, pad func(int) ([]byte, error)) ([]byte, error) {
	b := make([]byte, 0, s2BodyHead)
	fill, err := pad(s2NameBlock - len(name) - 1)
	if err != nil {
		return nil, err
	}
	b = append(b, name...)
	b = append(b, s2NameEnd)
	b = append(b, fill...)
	b = append(b, encodedAt.UTC().Format(s2TimeLayout)...)
	digits := strconv.FormatInt(size, 10)
	fill, err = pad(s2SizeBlock - len(digits) - 1)
	if err != nil {
		return nil, err
	}
	b = append(b, digits...)
	b = append(b, s2SizeEnd)
	b = append(b, fill...)
	if len(b) != s2BodyHead {
		return nil, fmt.Errorf("fdsec: internal: suite-2 body head is %d bytes, want %d", len(b), s2BodyHead)
	}
	return b, nil
}

// parseSuite2BodyHead runs only after frame 0 authenticated, so every failure
// here is damage, never a credential problem (FDSEC-FORMAT.md section 12).
func parseSuite2BodyHead(b []byte) (suite2Body, error) {
	var sb suite2Body
	nameBlock := b[:s2NameBlock]
	end := -1
	for i, v := range nameBlock {
		if v == s2NameEnd {
			end = i
			break
		}
	}
	if end < 1 {
		return sb, fmt.Errorf("%w: suite-2 name block has no name", ErrDamaged)
	}
	name := string(nameBlock[:end])
	if err := ValidateName(name); err != nil || norm.NFC.String(name) != name {
		return sb, fmt.Errorf("%w: sealed name is not a valid file name", ErrDamaged)
	}
	sb.Name = name

	ts := string(b[s2NameBlock : s2NameBlock+s2TimeBlock])
	t, err := time.Parse(s2TimeLayout, ts)
	if err != nil {
		return sb, fmt.Errorf("%w: suite-2 timestamp is not YYMMDDHHmmss", ErrDamaged)
	}
	// Go reads a two-digit year 69..99 as 19xx; suite 2 always means 20xx.
	if t.Year() < 2000 {
		t = t.AddDate(100, 0, 0)
	}
	if t.UTC().Format(s2TimeLayout) != ts {
		return sb, fmt.Errorf("%w: suite-2 timestamp is not YYMMDDHHmmss", ErrDamaged)
	}
	sb.EncodedAt = t.UTC()

	sizeBlock := b[s2NameBlock+s2TimeBlock : s2BodyHead]
	dot := -1
	for i, v := range sizeBlock {
		if v == s2SizeEnd {
			dot = i
			break
		}
		if v < '0' || v > '9' {
			return sb, fmt.Errorf("%w: suite-2 size block is not a decimal size", ErrDamaged)
		}
	}
	if dot < 1 {
		return sb, fmt.Errorf("%w: suite-2 size block is not a decimal size", ErrDamaged)
	}
	digits := string(sizeBlock[:dot])
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || strconv.FormatInt(n, 10) != digits {
		return sb, fmt.Errorf("%w: suite-2 size %q is not a canonical size", ErrDamaged, digits)
	}
	sb.Size = n
	return sb, nil
}

// PackSuite2 streams src into a complete suite-2 file written to dst
// (FDSEC-FORMAT.md section 18.5). src is read once and must yield exactly
// meta.Size bytes. Only the true name, the size and the time of encryption
// are sealed: suite 2 carries no original timestamps and no stored digest -
// every frame is authenticated, and the last one is flagged. The empty
// credential is accepted and produces obfuscation with no secrecy.
func PackSuite2(dst io.Writer, src io.Reader, meta Metadata, cred Credential, opts ...StreamOption) (Info, error) {
	info, _, err := packSuite2(dst, src, meta, cred, opts...)
	return info, err
}

func packSuite2(dst io.Writer, src io.Reader, meta Metadata, cred Credential, opts ...StreamOption) (Info, [digestSize]byte, error) {
	var info Info
	var digest [digestSize]byte
	so := newStreamOpts(opts)
	name := norm.NFC.String(meta.Name)
	if err := ValidateName(name); err != nil {
		return info, digest, fmt.Errorf("fdsec: %w", err)
	}
	if len(name) > MaxNameBytesSuite2 {
		return info, digest, fmt.Errorf("fdsec: a suite-2 container holds a name of at most %d UTF-8 bytes; this one has %d", MaxNameBytesSuite2, len(name))
	}
	if meta.Size < 0 {
		return info, digest, fmt.Errorf("fdsec: negative metadata size %d", meta.Size)
	}
	if meta.EncodedAt.IsZero() {
		meta.EncodedAt = time.Now().UTC()
	}
	if y := meta.EncodedAt.UTC().Year(); y < 2000 || y > 2099 {
		return info, digest, fmt.Errorf("fdsec: suite 2 records the time of encryption as YYMMDDHHmmss, which holds 2000..2099, not %d", y)
	}
	n := meta.Size
	if len(suite2Profiles) == 0 {
		return info, digest, fmt.Errorf("fdsec: internal: suite 2 has no profile")
	}

	// Randomness, in the draw order of FDSEC-FORMAT.md section 18.7.
	salt, err := randBytes(s2SaltSize)
	if err != nil {
		return info, digest, fmt.Errorf("fdsec: salt: %w", err)
	}
	nonce, err := randBytes(s2NonceSize)
	if err != nil {
		return info, digest, fmt.Errorf("fdsec: nonce: %w", err)
	}
	head, err := buildSuite2BodyHead(name, meta.EncodedAt, n, randBytes)
	if err != nil {
		return info, digest, err
	}
	tl, err := randBytes(4)
	if err != nil {
		return info, digest, err
	}
	tailLen := int64(s2TailMin + binary.LittleEndian.Uint32(tl)%(s2TailMax-s2TailMin+1))
	tail, err := randBytes(int(tailLen))
	if err != nil {
		return info, digest, err
	}

	if n > math.MaxInt64-s2BodyHead-tailLen {
		return info, digest, fmt.Errorf("fdsec: size %d is too large for suite 2", n)
	}
	bodyLen := s2BodyHead + n + tailLen
	k := (bodyLen + s2FramePlain - 1) / s2FramePlain

	key, err := deriveSuite2Key(cred, salt, suite2Profiles[0])
	if err != nil {
		return info, digest, err
	}
	aead, err := newXAEAD(key)
	clear(key) // FDSEC-15: the AEAD holds its own copy
	if err != nil {
		return info, digest, err
	}

	if _, err := dst.Write(salt); err != nil {
		return info, digest, fmt.Errorf("fdsec: write salt: %w", err)
	}
	if _, err := dst.Write(nonce); err != nil {
		return info, digest, fmt.Errorf("fdsec: write nonce: %w", err)
	}

	dh, err := blake2b.New256(nil)
	if err != nil {
		return info, digest, err
	}
	payload := &countingReader{r: io.TeeReader(io.LimitReader(src, n), dh)}
	body := io.MultiReader(bytesReader(head), payload, bytesReader(tail))
	pt := make([]byte, s2FramePlain)
	ctBuf := make([]byte, 0, s2Frame)
	written := int64(s2Lead)
	for i := int64(0); i < k; i++ {
		if so.stopped() {
			return info, digest, ErrStopped
		}
		want := int64(s2FramePlain)
		if i == k-1 {
			want = bodyLen - (k-1)*s2FramePlain
		}
		if _, err := io.ReadFull(body, pt[:want]); err != nil {
			return info, digest, fmt.Errorf("fdsec: source yielded %d bytes, metadata says %d", payload.n, n)
		}
		ct := aead.Seal(ctBuf[:0], suite2FrameNonce(nonce, i), pt[:want], suite2FrameAD(i, i == k-1))
		if _, err := dst.Write(ct); err != nil {
			return info, digest, fmt.Errorf("fdsec: write frame %d: %w", i, err)
		}
		written += int64(len(ct))
		if so.progress != nil {
			done := min(max((i+1)*s2FramePlain-s2BodyHead, 0), n)
			so.progress(done, n)
		}
	}
	if payload.n != n {
		return info, digest, fmt.Errorf("fdsec: source yielded %d bytes, metadata says %d", payload.n, n)
	}
	var extra [1]byte
	if m, _ := src.Read(extra[:]); m > 0 {
		return info, digest, fmt.Errorf("fdsec: source yielded more bytes than the metadata's %d", n)
	}
	if written != s2Lead+bodyLen+k*tagSize {
		return info, digest, fmt.Errorf("fdsec: internal: suite-2 length arithmetic is wrong")
	}
	copy(digest[:], dh.Sum(nil))
	info.Size = n
	info.Chunks = k
	info.TotalLen = written
	return info, digest, nil
}

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	m, err := c.r.Read(p)
	c.n += int64(m)
	return m, err
}

type byteSliceReader struct{ b []byte }

func (r *byteSliceReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	m := copy(p, r.b)
	r.b = r.b[m:]
	return m, nil
}

func bytesReader(b []byte) io.Reader { return &byteSliceReader{b: b} }

// suite2State is an opened suite-2 file: the key that authenticated frame 0,
// the frame geometry the file length implies, and frame 0's plaintext.
type suite2State struct {
	aead    cipher.AEAD
	nonce   []byte
	profile Suite2Profile
	frames  int64
	lastLen int64 // on-disk bytes of the last frame
	first   []byte
	body    suite2Body
	bodyLen int64
}

// openSuite2 is the suite-2 reader of FDSEC-FORMAT.md section 18.6, steps 1
// to 5: walk the try-list until frame 0 authenticates, then parse the body
// head and check the file length against the sealed size. Until frame 0
// authenticates, a wrong credential, a file that is not a suite-2 file and a
// damaged frame 0 are one outcome.
func openSuite2(src io.ReadSeeker, cred Credential) (*suite2State, error) {
	total, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, fmt.Errorf("fdsec: size container: %w", err)
	}
	k, lastLen, ok := suite2Frames(total)
	if !ok {
		return nil, fmt.Errorf("%w: %d bytes is no suite-2 length", ErrDamaged, total)
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("fdsec: seek container: %w", err)
	}
	lead := make([]byte, s2Lead)
	if _, err := io.ReadFull(src, lead); err != nil {
		return nil, fmt.Errorf("%w: file is too short to be a container (%v)", ErrDamaged, err)
	}
	f0 := int64(s2Frame)
	if k == 1 {
		f0 = lastLen
	}
	ct := make([]byte, f0)
	if _, err := io.ReadFull(src, ct); err != nil {
		return nil, fmt.Errorf("%w: frame 0 unreadable (%v)", ErrDamaged, err)
	}
	salt, nonce := lead[:s2SaltSize], lead[s2SaltSize:]
	ad := suite2FrameAD(0, k == 1)
	fn := suite2FrameNonce(nonce, 0)

	for _, p := range suite2Profiles {
		key, err := deriveSuite2Key(cred, salt, p)
		if err != nil {
			return nil, err
		}
		aead, err := newXAEAD(key)
		clear(key) // FDSEC-15: the AEAD holds its own copy
		if err != nil {
			return nil, err
		}
		pt, fail := aead.Open(nil, fn, ct, ad)
		if fail != nil {
			continue
		}
		// Authenticated: from here every refusal is damage.
		st := &suite2State{aead: aead, nonce: append([]byte(nil), nonce...), profile: p, frames: k, lastLen: lastLen, first: pt}
		st.bodyLen = total - s2Lead - k*tagSize
		if len(pt) < s2BodyHead {
			return nil, fmt.Errorf("%w: frame 0 is shorter than the body head", ErrDamaged)
		}
		if st.body, err = parseSuite2BodyHead(pt); err != nil {
			return nil, err
		}
		tail := st.bodyLen - s2BodyHead - st.body.Size
		if st.body.Size > st.bodyLen || tail < s2TailMin || tail > s2TailMax {
			return nil, fmt.Errorf("%w: file length %d is impossible for size %d", ErrDamaged, total, st.body.Size)
		}
		return st, nil
	}
	return nil, fmt.Errorf("%w: frame 0 does not authenticate", ErrCredentialOrTamper)
}

// unpackSuite2 streams the payload out of an opened suite-2 file, opening
// every frame - the tail's included, because only the last frame's flag
// proves the file was not cut short - and returns the payload's BLAKE2b-256.
func (c *Container) unpackSuite2(dst io.Writer, so streamOpts) (Metadata, [digestSize]byte, error) {
	st := c.s2
	var digest [digestSize]byte
	meta := Metadata{Name: st.body.Name, Size: st.body.Size, EncodedAt: st.body.EncodedAt}
	dh, err := blake2b.New256(nil)
	if err != nil {
		return meta, digest, err
	}
	payloadEnd := int64(s2BodyHead) + st.body.Size
	ct := make([]byte, s2Frame)
	plain := make([]byte, 0, s2FramePlain)
	for i := int64(0); i < st.frames; i++ {
		if so.stopped() {
			return meta, digest, ErrStopped
		}
		pt := st.first
		if i > 0 {
			want := int64(s2Frame)
			if i == st.frames-1 {
				want = st.lastLen
			}
			if _, err := c.src.Seek(s2Lead+i*s2Frame, io.SeekStart); err != nil {
				return meta, digest, fmt.Errorf("fdsec: seek frame %d: %w", i, err)
			}
			if _, err := io.ReadFull(c.src, ct[:want]); err != nil {
				return meta, digest, fmt.Errorf("%w: frame %d unreadable (%v)", ErrDamaged, i, err)
			}
			var fail error
			pt, fail = st.aead.Open(plain[:0], suite2FrameNonce(st.nonce, i), ct[:want], suite2FrameAD(i, i == st.frames-1))
			if fail != nil {
				return meta, digest, fmt.Errorf("%w: frame %d does not authenticate", ErrCredentialOrTamper, i)
			}
		}
		// The part of this frame's plaintext that is payload.
		start := i * s2FramePlain
		lo := max(int64(s2BodyHead)-start, 0)
		hi := min(payloadEnd-start, int64(len(pt)))
		if lo < hi {
			if _, err := dst.Write(pt[lo:hi]); err != nil {
				return meta, digest, fmt.Errorf("fdsec: write output: %w", err)
			}
			dh.Write(pt[lo:hi])
			if so.progress != nil {
				so.progress(start+hi-s2BodyHead, st.body.Size)
			}
		}
	}
	copy(digest[:], dh.Sum(nil))
	return meta, digest, nil
}
