package fdsec

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"testing"
	"time"
)

// fastParams keep the corpus quick: the layout parameters are header fields,
// so small values are as valid as the defaults - the reader obeys the file.
func fastParams() Params {
	return Params{ClusterAlignment: 512, ChunkSize: 1024}
}

// testProfile is the work factor this suite runs under. The KDF profile is a
// format constant rather than a file field (FDSEC-FORMAT.md section 6), so the
// only way to keep a corpus of hundreds of containers quick is the
// activeProfile seam - the same kind of seam as randSource, and just as absent
// from every shipped path. TestVectors_Suite1 puts the real profile back for
// the duration of the committed vectors, which must pin the format and not the
// test setting.
var testProfile = kdfProfile{MemoryKiB: 1024, Time: 1, Lanes: 1, Threshold: 8}

func TestMain(m *testing.M) {
	activeProfile = testProfile
	os.Exit(m.Run())
}

var fixedTimes = Metadata{
	Name:       "corpus.bin",
	EncodedAt:  time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	CreatedAt:  time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	AccessedAt: time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC),
	ModifiedAt: time.Date(2026, 8, 19, 23, 59, 59, 0, time.UTC),
}

// packBytes packs content into an in-memory container. Size is derived from
// the content; tests that exercise a size mismatch call Pack directly.
func packBytes(t *testing.T, content []byte, meta Metadata, cred Credential, p Params) []byte {
	t.Helper()
	meta.Size = int64(len(content))
	out := bytes.NewBuffer(nil)
	info, err := Pack(out, bytes.NewReader(content), meta, cred, p)
	if err != nil {
		t.Fatalf("Pack: %v", err)
	}
	if want := int64(len(content)); info.Size != want {
		t.Fatalf("Pack reported size %d, want %d", info.Size, want)
	}
	return out.Bytes()
}

// roundTrip packs content and unpacks it, asserting byte-exact recovery and
// metadata fidelity. Returns the container for tamper tests.
func roundTrip(t *testing.T, content []byte, meta Metadata, cred Credential, p Params) ([]byte, Metadata) {
	t.Helper()
	container := packBytes(t, content, meta, cred, p)
	got, m, err := unpackContainer(container, cred)
	if err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("round trip not byte-exact: got %d bytes, want %d", len(got), len(content))
	}
	if m.Name != meta.Name {
		t.Fatalf("name: got %q, want %q", m.Name, meta.Name)
	}
	if m.Size != int64(len(content)) {
		t.Fatalf("size: got %d, want %d", m.Size, len(content))
	}
	assertTimeEq := func(what string, got, want time.Time) {
		if !got.Equal(want) {
			t.Fatalf("%s: got %v, want %v", what, got, want)
		}
	}
	assertTimeEq("EncodedAt", m.EncodedAt, meta.EncodedAt)
	assertTimeEq("CreatedAt", m.CreatedAt, meta.CreatedAt)
	assertTimeEq("AccessedAt", m.AccessedAt, meta.AccessedAt)
	assertTimeEq("ModifiedAt", m.ModifiedAt, meta.ModifiedAt)
	return container, m
}

func unpackContainer(b []byte, cred Credential) ([]byte, Metadata, error) {
	var out bytes.Buffer
	m, err := Unpack(&out, bytes.NewReader(b), cred)
	return out.Bytes(), m, err
}

func randContent(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		t.Fatal(err)
	}
	return b
}

// faultWriter fails once the byte limit is reached, recording everything that
// made it through - the abrupt-interruption simulation of stage S2.
type faultWriter struct {
	buf   bytes.Buffer
	limit int64
	err   error
}

func (w *faultWriter) Write(p []byte) (int, error) {
	if w.err == nil && int64(w.buf.Len())+int64(len(p)) > w.limit {
		n, _ := w.buf.Write(p[:max(0, w.limit-int64(w.buf.Len()))])
		w.err = io.ErrShortWrite
		return n, io.ErrShortWrite
	}
	return w.buf.Write(p)
}
