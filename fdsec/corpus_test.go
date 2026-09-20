package fdsec

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// The corpus of stage S2 (tactics section 3): every member is chosen to
// exercise a boundary. Round trip = pack then unpack, byte-exact, with
// metadata fidelity asserted by the shared helper.

func TestRoundTrip_Corpus(t *testing.T) {
	p := fastParams() // capacity 1008 per chunk slot, alignment 512
	capacity := int(p.ChunkSize) - tagSize

	cases := []struct {
		name    string
		content []byte
	}{
		{"empty", []byte{}},
		{"one byte", []byte{0xA5}},
		{"exactly one chunk", make([]byte, capacity)},
		{"one chunk plus one", make([]byte, capacity+1)},
		{"several chunks", make([]byte, 3*capacity+37)},
		{"high entropy", randContent(t, 4*capacity)},
		{"size is an exact multiple of the alignment", make([]byte, 4*int(p.ClusterAlignment))},
		{"binary with all byte values", func() []byte {
			b := make([]byte, 256*8)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := fixedTimes
			roundTrip(t, tc.content, meta, NewCredential("pw123"), p)
		})
	}
}

// The empty credential is accepted - obfuscation with no secrecy, and a
// container that still round trips (spec 7.2, invariant 10: never called
// protection; here we only prove the mechanics).
func TestRoundTrip_EmptyCredential(t *testing.T) {
	roundTrip(t, []byte("obfuscation, not secrecy"), fixedTimes, NewCredential(""), fastParams())
}

// A credential at or above the threshold takes the fast branch and still
// round trips; the branch is recomputed by the reader, never stored.
func TestRoundTrip_ThresholdBranches(t *testing.T) {
	p := fastParams()                     // threshold 8
	short := strings.Repeat("a", 7)       // below: slow Argon2id branch
	long := strings.Repeat("a", 8)        // at: fast salted-hash branch
	longer := strings.Repeat("long ", 20) // well above
	for _, cred := range []string{short, long, longer} {
		roundTrip(t, []byte("payload"), fixedTimes, NewCredential(cred), p)
	}
}

// Names: NFC normalization, the 3000-byte cap, and a zero timestamp that comes
// back zero.
func TestMetadata_NameAndTimeEdges(t *testing.T) {
	p := fastParams()

	t.Run("NFC name normalizes", func(t *testing.T) {
		meta := fixedTimes
		meta.Name = "cafe\u0301.txt" // NFD form of café.txt
		container := packBytes(t, []byte("x"), meta, NewCredential("pw123"), p)
		_, m, err := unpackContainer(container, NewCredential("pw123"))
		if err != nil {
			t.Fatal(err)
		}
		if m.Name != "café.txt" {
			t.Fatalf("name not NFC-normalized: %q", m.Name)
		}
	})

	t.Run("3000-byte name packs, 3001 refused", func(t *testing.T) {
		meta := fixedTimes
		meta.Name = strings.Repeat("n", MaxNameBytes)
		roundTrip(t, []byte("x"), meta, NewCredential("pw123"), p)

		meta.Name = strings.Repeat("n", MaxNameBytes+1)
		meta.Size = 1
		out := &countingWriter{}
		if _, err := Pack(out, strings.NewReader("x"), meta, NewCredential("pw123"), p); err == nil {
			t.Fatal("oversized name accepted")
		} else if !strings.Contains(err.Error(), "name") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("zero times come back zero", func(t *testing.T) {
		meta := Metadata{Name: "zero.bin", Size: 1}
		container := packBytes(t, []byte("z"), meta, NewCredential("pw123"), p)
		_, m, err := unpackContainer(container, NewCredential("pw123"))
		if err != nil {
			t.Fatal(err)
		}
		if !m.CreatedAt.IsZero() || !m.AccessedAt.IsZero() || !m.ModifiedAt.IsZero() {
			t.Fatalf("zero timestamps not preserved: %v %v %v", m.CreatedAt, m.AccessedAt, m.ModifiedAt)
		}
		if m.EncodedAt.IsZero() {
			t.Fatal("EncodedAt was not defaulted to now")
		}
	})
}

// Zero Params fields take the package defaults, and the defaults land in the
// header byte for byte (FDSEC-FORMAT.md section 6 writer defaults).
func TestParams_DefaultsEchoInHeader(t *testing.T) {
	container := packBytes(t, []byte("defaults"), fixedTimes, NewCredential("pw"), Params{})
	h, err := parseHeader(bytes.NewReader(container))
	if err != nil {
		t.Fatal(err)
	}
	if h.ClusterAlignment != DefaultClusterAlignment || h.ChunkSize != DefaultChunkSize ||
		h.Threshold != DefaultThreshold || h.KDFMemoryKiB != DefaultKDFMemoryKiB ||
		h.KDFTime != DefaultKDFTime || h.KDFLanes != DefaultKDFLanes {
		t.Fatalf("defaults not recorded: %+v", h)
	}
}

// Params validation refuses impossible values before anything is written. A
// zero field is not among them: zero means "take the default", which
// TestParams_DefaultsEchoInHeader covers.
func TestParams_RejectsImpossible(t *testing.T) {
	base := fastParams()
	bad := []Params{
		{ClusterAlignment: 513, ChunkSize: 1024, Threshold: 8, KDFMemoryKiB: 1, KDFTime: 1, KDFLanes: 1}, // not a power of two
		{ClusterAlignment: 256, ChunkSize: 512, Threshold: 8, KDFMemoryKiB: 1, KDFTime: 1, KDFLanes: 1},  // below 512
		{ClusterAlignment: 512, ChunkSize: 1000, Threshold: 8, KDFMemoryKiB: 1, KDFTime: 1, KDFLanes: 1}, // not a multiple of the alignment
		{ClusterAlignment: 512, ChunkSize: 256, Threshold: 8, KDFMemoryKiB: 1, KDFTime: 1, KDFLanes: 1},  // chunk slot below the alignment
	}
	for i, p := range bad {
		if _, err := p.withDefaults(); err == nil {
			t.Fatalf("case %d accepted: %+v", i, p)
		}
	}
	if _, err := base.withDefaults(); err != nil {
		t.Fatalf("base params rejected: %v", err)
	}
}

// meta.Size must equal what the source yields - a caller error, reported
// before any container is produced.
func TestPack_SizeMismatchRefused(t *testing.T) {
	meta := fixedTimes
	meta.Size = 100
	w := &countingWriter{}
	if _, err := Pack(w, strings.NewReader("short"), meta, NewCredential("pw"), fastParams()); err == nil {
		t.Fatal("size mismatch accepted")
	}
	if w.n != 0 {
		t.Fatalf("%d bytes written before the mismatch was caught", w.n)
	}
}

// The frame dispatch: magic means framed, anything else is not (the quiet
// fallback point, FDSEC-FORMAT.md section 13.4).
func TestIsFramed(t *testing.T) {
	container := packBytes(t, []byte("x"), fixedTimes, NewCredential("pw"), fastParams())
	if !IsFramed(container) {
		t.Fatal("container not recognized as framed")
	}
	if IsFramed([]byte("FDSE")) || IsFramed([]byte{}) || IsFramed(randContent(t, 64)) {
		t.Fatal("non-frame recognized as framed")
	}
}

// A container shorter than the header is damage, not a crash.
func TestUnpack_TooShort(t *testing.T) {
	for _, n := range []int{0, 4, 5, 80} {
		if _, _, err := unpackContainer(make([]byte, n), NewCredential("pw")); !errors.Is(err, ErrDamaged) && !errors.Is(err, ErrUnsupported) {
			t.Fatalf("%d bytes: want damaged or unsupported, got %v", n, err)
		}
	}
}

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}
