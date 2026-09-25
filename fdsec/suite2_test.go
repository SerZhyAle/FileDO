package fdsec

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Suite 2 (FDSEC-FORMAT.md section 18) under the lowered test try-list.

var s2Meta = Metadata{Name: "quiet.bin", EncodedAt: time.Date(2026, 9, 25, 10, 11, 12, 0, time.UTC)}

func packSuite2Bytes(t *testing.T, content []byte, meta Metadata, cred Credential) []byte {
	t.Helper()
	meta.Size = int64(len(content))
	var out bytes.Buffer
	info, err := PackSuite2(&out, bytes.NewReader(content), meta, cred)
	if err != nil {
		t.Fatalf("PackSuite2: %v", err)
	}
	if info.TotalLen != int64(out.Len()) || info.Size != meta.Size {
		t.Fatalf("PackSuite2 reported %+v for a %d-byte file", info, out.Len())
	}
	return out.Bytes()
}

func openBytes(b []byte, cred Credential) (*Container, error) {
	return Open(bytes.NewReader(b), cred)
}

func unpackSuite2Bytes(t *testing.T, b []byte, cred Credential) ([]byte, Metadata, error) {
	t.Helper()
	var out bytes.Buffer
	m, err := Unpack(&out, bytes.NewReader(b), cred)
	return out.Bytes(), m, err
}

// forgeSuite2 seals an arbitrary body under cred with the current try-list's
// first profile, so a test can hand the reader a body that authenticates but
// breaks a structural rule.
func forgeSuite2(t *testing.T, body []byte, cred Credential) []byte {
	t.Helper()
	salt := bytes.Repeat([]byte{1}, s2SaltSize)
	nonce := bytes.Repeat([]byte{2}, s2NonceSize)
	key, err := deriveSuite2Key(cred, salt, suite2Profiles[0])
	if err != nil {
		t.Fatal(err)
	}
	a, err := newXAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	out := append(append([]byte{}, salt...), nonce...)
	k := (int64(len(body)) + s2FramePlain - 1) / s2FramePlain
	for i := int64(0); i < k; i++ {
		end := min((i+1)*s2FramePlain, int64(len(body)))
		out = a.Seal(out, suite2FrameNonce(nonce, i), body[i*s2FramePlain:end], suite2FrameAD(i, i == k-1))
	}
	return out
}

func validBody(t *testing.T, name string, payload []byte, tail int) []byte {
	t.Helper()
	head, err := buildSuite2BodyHead(name, s2Meta.EncodedAt, int64(len(payload)), randBytes)
	if err != nil {
		t.Fatal(err)
	}
	return append(append(head, payload...), make([]byte, tail)...)
}

func TestSuite2_RoundTripCorpus(t *testing.T) {
	cred := NewCredential("pw123")
	firstPayload := s2FramePlain - s2BodyHead // payload bytes that fit frame 0
	cases := map[string][]byte{
		"empty":                     {},
		"one byte":                  {0xA5},
		"payload fills frame 0":     make([]byte, firstPayload),
		"payload one past frame 0":  make([]byte, firstPayload+1),
		"several frames":            make([]byte, 3*s2FramePlain+37),
		"high entropy, many frames": randContent(t, 5*s2FramePlain+1),
		"all byte values": func() []byte {
			b := make([]byte, 256*8)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			b := packSuite2Bytes(t, content, s2Meta, cred)
			c, err := openBytes(b, cred)
			if err != nil {
				t.Fatal(err)
			}
			if c.Suite() != SuiteID2 || c.IsTree() {
				t.Fatalf("suite %d, tree %v; want suite 2, one file", c.Suite(), c.IsTree())
			}
			got, m, err := unpackSuite2Bytes(t, b, cred)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, content) {
				t.Fatalf("payload did not round-trip (%d bytes back, %d in)", len(got), len(content))
			}
			if m.Name != s2Meta.Name || m.Size != int64(len(content)) || !m.EncodedAt.Equal(s2Meta.EncodedAt) {
				t.Fatalf("metadata %+v", m)
			}
			if !m.CreatedAt.IsZero() || !m.ModifiedAt.IsZero() || !m.AccessedAt.IsZero() {
				t.Fatal("suite 2 carries no original timestamps, yet some came back")
			}
			// The body is 1060 + N + a tail of 8..32 KiB, framed at 64 KiB.
			body := int64(len(b)) - s2Lead - ((int64(len(b))-s2Lead+s2Frame-1)/s2Frame)*tagSize
			tail := body - s2BodyHead - int64(len(content))
			if tail < s2TailMin || tail > s2TailMax {
				t.Fatalf("tail of %d bytes is outside 8..32 KiB", tail)
			}
		})
	}
}

func TestSuite2_EmptyCredentialAndNames(t *testing.T) {
	b := packSuite2Bytes(t, []byte("obfuscation, not secrecy"), s2Meta, NewCredential(""))
	if _, _, err := unpackSuite2Bytes(t, b, NewCredential("")); err != nil {
		t.Fatalf("empty credential: %v", err)
	}

	meta := s2Meta
	meta.Name = "café.txt"
	b = packSuite2Bytes(t, []byte("x"), meta, NewCredential("pw"))
	if _, m, err := unpackSuite2Bytes(t, b, NewCredential("pw")); err != nil || m.Name != "café.txt" {
		t.Fatalf("NFC name: %q, %v", m.Name, err)
	}

	meta.Name = strings.Repeat("n", MaxNameBytesSuite2)
	b = packSuite2Bytes(t, []byte("x"), meta, NewCredential("pw"))
	if _, m, err := unpackSuite2Bytes(t, b, NewCredential("pw")); err != nil || m.Name != meta.Name {
		t.Fatalf("longest name: %v", err)
	}
	meta.Name = strings.Repeat("n", MaxNameBytesSuite2+1)
	meta.Size = 1
	if _, err := PackSuite2(io.Discard, bytes.NewReader([]byte("x")), meta, NewCredential("pw")); err == nil {
		t.Fatal("a 1024-byte name was accepted; the name block holds 1023 and the CR")
	}
	meta.Name = "a/b"
	if _, err := PackSuite2(io.Discard, bytes.NewReader([]byte("x")), meta, NewCredential("pw")); err == nil {
		t.Fatal("a path was accepted as a name")
	}
	meta.Name = "ok.txt"
	meta.EncodedAt = time.Date(1999, 12, 31, 0, 0, 0, 0, time.UTC)
	if _, err := PackSuite2(io.Discard, bytes.NewReader([]byte("x")), meta, NewCredential("pw")); err == nil {
		t.Fatal("a year YYMMDDHHmmss cannot hold was accepted")
	}
}

func TestSuite2_SizeMismatchRefused(t *testing.T) {
	meta := s2Meta
	meta.Size = 5
	if _, err := PackSuite2(io.Discard, bytes.NewReader([]byte("abc")), meta, NewCredential("pw")); err == nil {
		t.Fatal("a short source was accepted")
	}
	if _, err := PackSuite2(io.Discard, bytes.NewReader([]byte("abcdefg")), meta, NewCredential("pw")); err == nil {
		t.Fatal("a long source was accepted")
	}
}

// The outcome classes of FDSEC-FORMAT.md section 12, for suite 2.
func TestSuite2_OutcomeClasses(t *testing.T) {
	cred := NewCredential("pw123")
	content := randContent(t, 3*s2FramePlain)
	good := packSuite2Bytes(t, content, s2Meta, cred)
	frames := (int64(len(good)) - s2Lead + s2Frame - 1) / s2Frame
	if frames < 3 {
		t.Fatalf("want at least 3 frames, have %d", frames)
	}
	flip := func(off int64) []byte {
		b := bytes.Clone(good)
		b[off] ^= 0x01
		return b
	}
	credOrTamper := map[string][]byte{
		"salt byte":           flip(3),
		"nonce byte":          flip(s2SaltSize + 5),
		"frame 0 byte":        flip(s2Lead + 100),
		"frame 1 byte":        flip(s2Lead + s2Frame + 7),
		"last frame tag byte": flip(int64(len(good)) - 1),
		"frames 1 and 2 swapped": func() []byte {
			b := bytes.Clone(good)
			f1 := bytes.Clone(b[s2Lead+s2Frame : s2Lead+2*s2Frame])
			copy(b[s2Lead+s2Frame:], b[s2Lead+2*s2Frame:s2Lead+3*s2Frame])
			copy(b[s2Lead+2*s2Frame:], f1)
			return b
		}(),
	}
	for name, b := range credOrTamper {
		t.Run(name, func(t *testing.T) {
			_, _, err := unpackSuite2Bytes(t, b, cred)
			if !errors.Is(err, ErrCredentialOrTamper) {
				t.Fatalf("want wrong-credential-or-tamper, got %v", err)
			}
		})
	}
	t.Run("wrong credential", func(t *testing.T) {
		if _, err := openBytes(good, NewCredential("pw124")); !errors.Is(err, ErrCredentialOrTamper) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("never a container", func(t *testing.T) {
		if _, err := openBytes(randContent(t, 20000), cred); !errors.Is(err, ErrCredentialOrTamper) {
			t.Fatalf("got %v", err)
		}
	})

	// A cut inside the tail's slack is caught by the last frame's tag, and the
	// tag cannot tell a cut from a flip: either class is honest, never success.
	t.Run("one byte cut", func(t *testing.T) {
		_, _, err := unpackSuite2Bytes(t, good[:len(good)-1], cred)
		if !errors.Is(err, ErrCredentialOrTamper) && !errors.Is(err, ErrDamaged) {
			t.Fatalf("got %v", err)
		}
	})

	// Damage: the body authenticated but breaks a structural rule - including
	// a length the sealed size makes impossible, which is truncation.
	damaged := map[string][]byte{
		"cut at a frame edge":     good[:s2Lead+2*s2Frame],
		"one frame appended":      append(bytes.Clone(good), good[s2Lead+s2Frame:s2Lead+2*s2Frame]...),
		"tail shorter than 8 KiB": forgeSuite2(t, validBody(t, "a.txt", []byte("abc"), s2TailMin-1), cred),
		"tail longer than 32 KiB": forgeSuite2(t, validBody(t, "a.txt", []byte("abc"), s2TailMax+1), cred),
		"size beyond the body": func() []byte {
			body := validBody(t, "a.txt", []byte("abc"), s2TailMin)
			copy(body[s2NameBlock+s2TimeBlock:], "999999.")
			return forgeSuite2(t, body, cred)
		}(),
		"name with a separator": func() []byte {
			body := validBody(t, "a.txt", []byte("abc"), s2TailMin)
			copy(body, "a\\b.txt\r")
			return forgeSuite2(t, body, cred)
		}(),
		"no CR in the name block": func() []byte {
			body := validBody(t, "a.txt", []byte("abc"), s2TailMin)
			copy(body, bytes.Repeat([]byte{'n'}, s2NameBlock))
			return forgeSuite2(t, body, cred)
		}(),
		"timestamp not a date": func() []byte {
			body := validBody(t, "a.txt", []byte("abc"), s2TailMin)
			copy(body[s2NameBlock:], "261399999999")
			return forgeSuite2(t, body, cred)
		}(),
		"size with a leading zero": func() []byte {
			body := validBody(t, "a.txt", []byte("abc"), s2TailMin+1)
			copy(body[s2NameBlock+s2TimeBlock:], "03.")
			return forgeSuite2(t, body, cred)
		}(),
	}
	for name, b := range damaged {
		t.Run(name, func(t *testing.T) {
			if _, err := openBytes(b, cred); !errors.Is(err, ErrDamaged) {
				t.Fatalf("want damaged, got %v", err)
			}
		})
	}
	t.Run("the forge itself is valid", func(t *testing.T) {
		b := forgeSuite2(t, validBody(t, "a.txt", []byte("abc"), s2TailMin), cred)
		got, _, err := unpackSuite2Bytes(t, b, cred)
		if err != nil || string(got) != "abc" {
			t.Fatalf("%q, %v", got, err)
		}
	})
}

// The dispatch of FDSEC-FORMAT.md section 13.4: one Open for every suite.
func TestSuite2_Dispatch(t *testing.T) {
	cred := NewCredential("pw123")
	s1 := packBytes(t, []byte("suite one"), fixedTimes, cred, DefaultParams())
	s2 := packSuite2Bytes(t, []byte("suite two"), s2Meta, cred)
	var s3 bytes.Buffer
	if _, err := PackTree(&s3, memTree("root", map[string][]byte{"f": []byte("suite three")}, []string{"f"}), cred, DefaultParams()); err != nil {
		t.Fatal(err)
	}
	for want, b := range map[uint8][]byte{SuiteID1: s1, SuiteID2: s2, SuiteID3: s3.Bytes()} {
		c, err := openBytes(b, cred)
		if err != nil {
			t.Fatalf("suite %d: %v", want, err)
		}
		if c.Suite() != want {
			t.Fatalf("opened as suite %d, want %d", c.Suite(), want)
		}
		if _, err := openBytes(b, NewCredential("wrong")); !errors.Is(err, ErrCredentialOrTamper) {
			t.Fatalf("suite %d, wrong credential: %v", want, err)
		}
	}
	c, _ := openBytes(s2, cred)
	if _, err := c.TreeMetadata(); !errors.Is(err, ErrFileContainer) {
		t.Fatalf("TreeMetadata on suite 2: %v", err)
	}
	if _, _, err := c.VerifyTree(); !errors.Is(err, ErrFileContainer) {
		t.Fatalf("VerifyTree on suite 2: %v", err)
	}

	hi, err := ReadHeaderInfo(bytes.NewReader(s2), cred)
	if err != nil {
		t.Fatal(err)
	}
	p := suite2Profiles[0]
	if hi.SuiteID != SuiteID2 || hi.IsTree || hi.ContainerSize != int64(len(s2)) || hi.KDFMemoryKiB != p.MemoryKiB || hi.KDFTime != p.Time || hi.KDFLanes != p.Lanes || hi.Threshold != 0 || hi.ClusterAlignment != 0 {
		t.Fatalf("header info %+v", hi)
	}
	if _, err := ReadHeaderInfo(bytes.NewReader(s2), NewCredential("wrong")); !errors.Is(err, ErrCredentialOrTamper) {
		t.Fatalf("info, wrong credential: %v", err)
	}

	// Damage and truncation of a suite-1 file keep their suite-1 answers:
	// the dispatch reaches suite 2 only when suite 1's head does not open.
	if _, _, err := unpackContainer(s1[:len(s1)-512], cred); !errors.Is(err, ErrDamaged) {
		t.Fatalf("truncated suite-1 container: %v", err)
	}
	if _, err := openBytes(s1[:600], cred); !errors.Is(err, ErrDamaged) {
		t.Fatalf("suite-1 head cut short: %v", err)
	}
}

// A retired profile keeps opening: the try-list is walked until frame 0
// authenticates (FDSEC-FORMAT.md section 18.2).
func TestSuite2_TryList(t *testing.T) {
	saved := suite2Profiles
	defer func() { suite2Profiles = saved }()
	old := Suite2Profile{MemoryKiB: 1024, Time: 1, Lanes: 1}
	cur := Suite2Profile{MemoryKiB: 2048, Time: 1, Lanes: 2}

	suite2Profiles = []Suite2Profile{old}
	cred := NewCredential("pw")
	b := packSuite2Bytes(t, []byte("written under the old profile"), s2Meta, cred)

	suite2Profiles = []Suite2Profile{cur, old}
	hi, err := ReadHeaderInfo(bytes.NewReader(b), cred)
	if err != nil {
		t.Fatalf("a file under a retired profile does not open: %v", err)
	}
	if hi.KDFMemoryKiB != old.MemoryKiB || hi.KDFLanes != old.Lanes {
		t.Fatalf("reported profile %+v, want the retired one", hi)
	}
	b2 := packSuite2Bytes(t, []byte("new"), s2Meta, cred)
	suite2Profiles = []Suite2Profile{old}
	if _, err := openBytes(b2, cred); !errors.Is(err, ErrCredentialOrTamper) {
		t.Fatalf("the writer did not use the current (first) profile: %v", err)
	}
}

func TestSuite2_ScreenLength(t *testing.T) {
	for _, n := range []int64{suite2MinLen, suite2MinLen + 1, 12345, s2Lead + s2Frame + tagSize + 1} {
		if err := ScreenLength(n); err != nil {
			t.Errorf("%d bytes can be a suite-2 file, refused: %v", n, err)
		}
	}
	for _, n := range []int64{suite2MinLen - 1, s2Lead + s2Frame + 5, s2Lead + s2Frame + tagSize} {
		if n%512 == 0 {
			continue
		}
		if err := ScreenLength(n); !errors.Is(err, ErrDamaged) {
			t.Errorf("%d bytes is no container length, passed: %v", n, err)
		}
	}
}

func TestSuite2_PackFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "plain.txt")
	content := randContent(t, 2*s2FramePlain+99)
	if err := os.WriteFile(src, content, 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "plain.fd-sec")
	cred := NewCredential("pw")
	meta := s2Meta
	meta.Name, meta.Size = "plain.txt", int64(len(content))
	info, err := PackFileSuite2(dst, src, meta, cred)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dst)
	if err != nil || fi.Size() != info.TotalLen {
		t.Fatalf("container on disk: %v, %d vs %d", err, fi.Size(), info.TotalLen)
	}
	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	m, err := Unpack(&out, f, cred)
	f.Close()
	if err != nil || !bytes.Equal(out.Bytes(), content) || m.Name != "plain.txt" {
		t.Fatalf("unpack from disk: %v", err)
	}
	if _, err := PackFileSuite2(dst, src, meta, cred); err == nil {
		t.Fatal("an existing container was overwritten")
	}

	// A read-back that fails leaves nothing behind.
	dst2 := filepath.Join(dir, "broken.fd-sec")
	beforeReadBack = func(tmp string) {
		b, _ := os.ReadFile(tmp)
		b[s2Lead+s2Frame+3] ^= 1
		os.WriteFile(tmp, b, 0o600)
	}
	defer func() { beforeReadBack = nil }()
	if _, err := PackFileSuite2(dst2, src, meta, cred); !errors.Is(err, ErrCredentialOrTamper) {
		t.Fatalf("a corrupted read-back passed: %v", err)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "broken*"))
	if len(left) != 0 {
		t.Fatalf("a failed pack left %v", left)
	}
}
