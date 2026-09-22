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
	p := fastParams()
	th := int(activeProfile.Threshold)        // 8 under testProfile
	short := strings.Repeat("a", th-1)        // below: slow Argon2id branch
	long := strings.Repeat("a", th)           // at: fast salted-hash branch
	longer := strings.Repeat("long ", 5*th/2) // well above
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
// masked header byte for byte (FDSEC-FORMAT.md section 6 writer defaults).
func TestParams_DefaultsEchoInHeader(t *testing.T) {
	cred := NewCredential("pw")
	container := packBytes(t, []byte("defaults"), fixedTimes, cred, Params{})
	h, _, err := openHead(bytes.NewReader(container), cred)
	if err != nil {
		t.Fatal(err)
	}
	if h.ClusterAlignment != DefaultClusterAlignment || h.ChunkSize != DefaultChunkSize {
		t.Fatalf("defaults not recorded: %+v", h)
	}
}

// Params validation refuses impossible values before anything is written. A
// zero field is not among them: zero means "take the default", which
// TestParams_DefaultsEchoInHeader covers.
func TestParams_RejectsImpossible(t *testing.T) {
	base := fastParams()
	bad := []Params{
		{ClusterAlignment: 513, ChunkSize: 1024}, // not a power of two
		{ClusterAlignment: 256, ChunkSize: 512},  // below 512
		{ClusterAlignment: 512, ChunkSize: 1000}, // not a multiple of the alignment
		{ClusterAlignment: 512, ChunkSize: 256},  // chunk slot below the alignment
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

// The container says nothing about its own nature (FDSEC-FORMAT.md sections 4,
// 13.4, 14). This is the regression guard for the whole point of the masked
// head: no marker string anywhere, no constant bytes shared by two containers
// of the same input, and no stretch of repeated bytes where the reserved key
// slots and their zero payloads sit.
func TestContainer_CarriesNoMarker(t *testing.T) {
	cred := NewCredential("pw")
	a := packBytes(t, []byte("x"), fixedTimes, cred, fastParams())
	b := packBytes(t, []byte("x"), fixedTimes, cred, fastParams())

	for _, marker := range []string{"FDSEC", "fdsec", "fd-sec", "FileDO"} {
		if bytes.Contains(a, []byte(marker)) {
			t.Fatalf("the container carries the marker %q", marker)
		}
	}
	if len(a) != len(b) {
		t.Fatalf("same input gave different lengths: %d and %d", len(a), len(b))
	}
	if bytes.Equal(a[:preMeta], b[:preMeta]) {
		t.Fatal("two containers of the same input share their head byte for byte")
	}
	// The 640-byte key-slot region is 561 zero bytes before masking (the empty
	// slots plus slot 0's reserved tail). Unmasked, that would be the loudest
	// pattern in the file.
	if run := longestRun(a[headerSize:preMeta]); run > 8 {
		t.Fatalf("the key-slot region has a run of %d equal bytes: it is not masked", run)
	}
}

func longestRun(b []byte) int {
	best, cur := 0, 0
	for i := range b {
		if i > 0 && b[i] == b[i-1] {
			cur++
		} else {
			cur = 1
		}
		if cur > best {
			best = cur
		}
	}
	return best
}

// A file too short to hold a head is damage, not a crash - and the report says
// nothing about whether it ever was a container.
func TestUnpack_TooShort(t *testing.T) {
	for _, n := range []int{0, 4, 5, 80, preMeta - 1} {
		if _, _, err := unpackContainer(make([]byte, n), NewCredential("pw")); !errors.Is(err, ErrDamaged) {
			t.Fatalf("%d bytes: want damaged, got %v", n, err)
		}
	}
}

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}
