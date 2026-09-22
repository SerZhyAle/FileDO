package fdsec

import (
	"bytes"
	"crypto/sha256" //nolint:gosec // fixed test seed derivation, not security
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20"
)

// The committed test vectors of FDSEC-FORMAT.md section 16 (the contract lives
// outside this repository - see AGENTS.md "External contracts"). A third party
// implements from the format document, replays the recorded inputs, and
// compares byte for byte; this test does the same from this side and also
// guards that the committed files stay in sync with the code.
//
// Regenerate after a deliberate format change:
//
//	FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite1

const (
	vecDir        = "testdata/vectors"
	vecJSON       = "vectors.json"
	vecEmptyFile  = "container-empty.fd-sec"
	vecOneFile    = "container-onebyte.fd-sec"
	vecSlowCred   = "correct horse battery staple" // 30 bytes < threshold 64: Argon2id branch
	vecEmptyName  = "empty.txt"
	vecOneName    = "one.bin"
	vecOneContent = "A"
)

const vecFastCred = "a deliberately long passphrase of more than sixty-four UTF-8 bytes, carrying its own entropy" // fast branch

var vecTimes = Metadata{
	Name:       "",
	EncodedAt:  time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC),
	CreatedAt:  time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
	AccessedAt: time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC),
	ModifiedAt: time.Date(2026, 8, 19, 23, 59, 59, 0, time.UTC),
}

type hexBytes []byte

func (h *hexBytes) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := hex.DecodeString(s)
	if err != nil {
		return err
	}
	*h = v
	return nil
}

func (h hexBytes) MarshalJSON() ([]byte, error) { return json.Marshal(hex.EncodeToString(h)) }

type vecRandoms struct {
	Salt      hexBytes `json:"salt"`
	FileKey   hexBytes `json:"file_key"`
	WrapNonce hexBytes `json:"wrap_nonce"`
	MetaPad   hexBytes `json:"meta_pad"`
	AlignPad  hexBytes `json:"align_pad"`
	TailPad   hexBytes `json:"tail_pad"`
}

type vecLayout struct {
	MaskKey     hexBytes `json:"mask_key"`
	KEK         hexBytes `json:"kek"`
	HeaderPlain hexBytes `json:"header_unmasked"`
	SlotsPlain  hexBytes `json:"slots_unmasked"`

	Header    hexBytes `json:"header_on_disk"`
	Slots     hexBytes `json:"slots_on_disk"`
	Meta      hexBytes `json:"sealed_metadata"`
	AlignPad  hexBytes `json:"align_pad"`
	Payload   hexBytes `json:"payload"`
	TailPad   hexBytes `json:"tail_pad"`
	TotalLen  int64    `json:"total_len"`
	PreLen    int64    `json:"payload_offset"`
	FileIdent string   `json:"blake2b256_of_file"`
}

type vecChunk struct {
	Nonce      hexBytes `json:"nonce"`
	AD         hexBytes `json:"associated_data"`
	Ciphertext hexBytes `json:"ciphertext"`
}

type vecContainer struct {
	Name       string     `json:"name"`
	Size       int64      `json:"size"`
	Content    hexBytes   `json:"content"`
	Credential string     `json:"credential"`
	EncodedAt  string     `json:"encoded_at"`
	CreatedAt  string     `json:"created_at"`
	AccessedAt string     `json:"accessed_at"`
	ModifiedAt string     `json:"modified_at"`
	Randoms    vecRandoms `json:"random_inputs"`
	Layout     vecLayout  `json:"layout"`
	File       string     `json:"file"`
	Chunk0     *vecChunk  `json:"chunk0,omitempty"`
}

type vectorsFile struct {
	Format     string                   `json:"format"`
	Generated  string                   `json:"generated"`
	KDF        map[string]uint32        `json:"kdf_profile"`
	RootSlow   hexBytes                 `json:"root_slow_branch"`
	RootFast   hexBytes                 `json:"root_fast_branch"`
	Containers map[string]*vecContainer `json:"containers"`
}

// vecStream derives the fixed per-container randoms from a seed phrase: one
// deterministic keystream, sliced in the documented draw order (salt, file
// key, wrap nonce, metadata pad, alignment pad, tail pad). The lengths are
// layout facts a third party computes from the format document itself.
func vecStream(seed string, total int) []byte {
	k := sha256.Sum256([]byte(seed))
	c, err := chacha20.NewUnauthenticatedCipher(k[:], []byte("fdsec-vec-01"))
	if err != nil {
		panic(err)
	}
	out := make([]byte, total)
	c.XORKeyStream(out, out)
	return out
}

func vecLayoutLengths(name string, size int64) []int {
	p := DefaultParams()
	h := header{ChunkSize: p.ChunkSize, ClusterAlignment: p.ClusterAlignment}
	// Metadata block: u16le len || name || 40 (size + 4 FILETIMEs) || 32 digest.
	metaPad := metaPlain - 2 - len(name) - 72
	preLen := h.preLen()
	alignPad := int(preLen) - preMeta - metaSize
	k := h.chunkCount(size)
	last := h.lastLen(size)
	total := preLen + (k-1)*int64(p.ChunkSize) + last + tagSize
	tail := (int64(p.ClusterAlignment) - total%int64(p.ClusterAlignment)) % int64(p.ClusterAlignment)
	return []int{saltSize, fileKeySize, nonceSize, metaPad, alignPad, int(tail)}
}

func buildVectorContainer(name string, content []byte, cred string) (*vecContainer, []byte) {
	meta := vecTimes
	meta.Name = name
	meta.Size = int64(len(content))

	lengths := vecLayoutLengths(name, int64(len(content)))
	total := 0
	for _, n := range lengths {
		total += n
	}
	stream := vecStream("fdsec-vector/"+name, total)
	r := &vecRandoms{}
	off := 0
	take := func(n int) []byte {
		b := stream[off : off+n]
		off += n
		return bytes.Clone(b)
	}
	r.Salt = take(saltSize)
	r.FileKey = take(fileKeySize)
	r.WrapNonce = take(nonceSize)
	r.MetaPad = take(lengths[3])
	r.AlignPad = take(lengths[4])
	r.TailPad = take(lengths[5])

	saved := randSource
	randSource = bytes.NewReader(stream)
	defer func() { randSource = saved }()

	var out bytes.Buffer
	Pack(&out, bytes.NewReader(content), meta, NewCredential(cred), DefaultParams())
	b := out.Bytes()

	v := &vecContainer{
		Name:       name,
		Size:       int64(len(content)),
		Content:    content,
		Credential: cred,
		EncodedAt:  meta.EncodedAt.Format(time.RFC3339Nano),
		CreatedAt:  meta.CreatedAt.Format(time.RFC3339Nano),
		AccessedAt: meta.AccessedAt.Format(time.RFC3339Nano),
		ModifiedAt: meta.ModifiedAt.Format(time.RFC3339Nano),
		Randoms:    *r,
	}
	h, _, _ := openHead(bytes.NewReader(b), NewCredential(cred))
	preLen := h.preLen()
	fileLen := int64(len(b))
	tailLen := len(r.TailPad)

	// The head is masked on disk, so the vectors publish both sides of the
	// mask and the key that produces it: a third party unmasks, compares the
	// plaintext head field by field, and re-masks to the committed bytes.
	maskKey, kek, err := deriveKeys(NewCredential(cred), r.Salt)
	if err != nil {
		panic(err)
	}
	plainHead := bytes.Clone(b[:preMeta])
	if err := mask(maskKey, plainHead[maskOff:]); err != nil {
		panic(err)
	}

	v.Layout = vecLayout{
		MaskKey:     maskKey,
		KEK:         kek,
		HeaderPlain: bytes.Clone(plainHead[:headerSize]),
		SlotsPlain:  bytes.Clone(plainHead[headerSize:preMeta]),

		Header:    bytes.Clone(b[:headerSize]),
		Slots:     bytes.Clone(b[headerSize:preMeta]),
		Meta:      bytes.Clone(b[preMeta : preMeta+metaSize]),
		AlignPad:  bytes.Clone(b[preMeta+metaSize : preLen]),
		Payload:   bytes.Clone(b[preLen : fileLen-int64(tailLen)]),
		TailPad:   bytes.Clone(b[fileLen-int64(tailLen):]),
		TotalLen:  fileLen,
		PreLen:    preLen,
		FileIdent: hex.EncodeToString(mustBlake(b)),
	}
	return v, b
}

func mustBlake(b []byte) []byte {
	d := blake2b.Sum256(b)
	return d[:]
}

func TestVectors_Suite1(t *testing.T) {
	// The committed vectors pin the format, so they run under the real KDF
	// profile of format version 1 - never under the suite's fast test profile.
	saved := activeProfile
	activeProfile = profileV1
	defer func() { activeProfile = saved }()

	emptyVec, emptyBytes := buildVectorContainer(vecEmptyName, []byte{}, vecSlowCred)
	emptyVec.File = vecEmptyFile
	oneVec, oneBytes := buildVectorContainer(vecOneName, []byte(vecOneContent), vecSlowCred)
	oneVec.File = vecOneFile

	// The root-key vectors pin both threshold branches with the empty
	// container's salt and the profile of format version 1.
	h, _, err := openHead(bytes.NewReader(emptyBytes), NewCredential(vecSlowCred))
	if err != nil {
		t.Fatal(err)
	}
	rootSlow := deriveRoot(NewCredential(vecSlowCred), h.Salt[:])
	rootFast := deriveRoot(NewCredential(vecFastCred), h.Salt[:])
	if len(NewCredential(vecFastCred)) < int(profileV1.Threshold) {
		t.Fatal("fast-branch vector credential is shorter than the threshold")
	}

	// The chunk vector pins the one-byte container's single chunk: its derived
	// nonce, its associated data, its ciphertext.
	obKey := oneVec.Randoms.FileKey
	nonce, err := deriveNonce(obKey, string(chunkCtx(0)))
	if err != nil {
		t.Fatal(err)
	}
	ad := adChunk(parseHeaderDigest(t, oneBytes, vecSlowCred), 0, 1, true, 1)
	oneVec.Chunk0 = &vecChunk{Nonce: nonce, AD: ad, Ciphertext: bytes.Clone(oneVec.Layout.Payload)}

	vf := &vectorsFile{
		Format:    "FDSEC suite 1, format version 1 - see FDSEC-FORMAT.md section 16, the contract these vectors ship beside",
		Generated: "2026-09-20",
		KDF:       map[string]uint32{"m_kib": profileV1.MemoryKiB, "t": profileV1.Time, "p": uint32(profileV1.Lanes), "threshold": profileV1.Threshold},
		RootSlow:  rootSlow,
		RootFast:  rootFast,
		Containers: map[string]*vecContainer{
			"empty":   emptyVec,
			"onebyte": oneVec,
		},
	}

	if os.Getenv("FDSEC_WRITE_VECTORS") == "1" {
		if err := os.MkdirAll(vecDir, 0o755); err != nil {
			t.Fatal(err)
		}
		j, err := json.MarshalIndent(vf, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vecDir, vecJSON), append(j, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vecDir, vecEmptyFile), emptyBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vecDir, vecOneFile), oneBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("vectors rewritten to %s", vecDir)
	}

	// Validation against the committed files.
	raw, err := os.ReadFile(filepath.Join(vecDir, vecJSON))
	if err != nil {
		t.Fatalf("committed vectors.json missing (run FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite1): %v", err)
	}
	var committed vectorsFile
	if err := json.Unmarshal(raw, &committed); err != nil {
		t.Fatal(err)
	}

	checkBytes := func(what string, got, want []byte) {
		t.Helper()
		if !bytes.Equal(got, want) {
			t.Fatalf("%s does not match the committed vector (got %d bytes, want %d)", what, len(got), len(want))
		}
	}
	checkBytes("committed empty container", emptyBytes, mustRead(t, filepath.Join(vecDir, vecEmptyFile)))
	checkBytes("committed one-byte container", oneBytes, mustRead(t, filepath.Join(vecDir, vecOneFile)))

	if len(committed.RootSlow) != fileKeySize || len(committed.RootFast) != fileKeySize {
		t.Fatal("committed root-key vectors are not 32 bytes")
	}
	if !bytes.Equal(committed.RootSlow, rootSlow) {
		t.Error("slow-branch root key drifted from the committed vector")
	}
	if !bytes.Equal(committed.RootFast, rootFast) {
		t.Error("fast-branch root key drifted from the committed vector")
	}
	for key, want := range map[string]*vecContainer{"empty": emptyVec, "onebyte": oneVec} {
		got := committed.Containers[key]
		if got == nil {
			t.Fatalf("committed vectors.json has no %q container", key)
		}
		checkBytes(key+" mask key", want.Layout.MaskKey, got.Layout.MaskKey)
		checkBytes(key+" kek", want.Layout.KEK, got.Layout.KEK)
		checkBytes(key+" unmasked header", want.Layout.HeaderPlain, got.Layout.HeaderPlain)
		checkBytes(key+" unmasked slots", want.Layout.SlotsPlain, got.Layout.SlotsPlain)
		checkBytes(key+" header", want.Layout.Header, got.Layout.Header)
		checkBytes(key+" slots", want.Layout.Slots, got.Layout.Slots)
		checkBytes(key+" sealed metadata", want.Layout.Meta, got.Layout.Meta)
		checkBytes(key+" payload", want.Layout.Payload, got.Layout.Payload)
		checkBytes(key+" tail pad", want.Layout.TailPad, got.Layout.TailPad)
		if got.Layout.TotalLen != want.Layout.TotalLen || got.Layout.PreLen != want.Layout.PreLen {
			t.Errorf("%s layout numbers drifted: %+v vs %+v", key, got.Layout, want.Layout)
		}
	}
	if c := committed.Containers["onebyte"]; c != nil && c.Chunk0 != nil {
		checkBytes("onebyte chunk0 nonce", nonce, c.Chunk0.Nonce)
		checkBytes("onebyte chunk0 ad", ad, c.Chunk0.AD)
		checkBytes("onebyte chunk0 ciphertext", oneVec.Layout.Payload, c.Chunk0.Ciphertext)
	} else {
		t.Error("committed onebyte chunk0 vector missing")
	}

	// The committed containers must unpack - the read-back the format promises.
	for _, f := range []string{vecEmptyFile, vecOneFile} {
		b := mustRead(t, filepath.Join(vecDir, f))
		if _, _, err := unpackContainer(b, NewCredential(vecSlowCred)); err != nil {
			t.Fatalf("committed %s does not unpack: %v", f, err)
		}
	}

	// The committed randoms must reproduce the committed file byte for byte -
	// this is the third-party property: doc + vectors.json => identical bytes.
	for key, name := range map[string]string{"empty": vecEmptyName, "onebyte": vecOneName} {
		c := committed.Containers[key]
		content := c.Content
		stream := concat(c.Randoms.Salt, c.Randoms.FileKey, c.Randoms.WrapNonce, c.Randoms.MetaPad, c.Randoms.AlignPad, c.Randoms.TailPad)
		meta := vecTimes
		meta.Name = name
		meta.Size = int64(len(content))
		meta.EncodedAt = mustTime(t, c.EncodedAt)
		meta.CreatedAt = mustTime(t, c.CreatedAt)
		meta.AccessedAt = mustTime(t, c.AccessedAt)
		meta.ModifiedAt = mustTime(t, c.ModifiedAt)
		saved := randSource
		randSource = bytes.NewReader(stream)
		var out bytes.Buffer
		_, err := Pack(&out, bytes.NewReader(content), meta, NewCredential(c.Credential), DefaultParams())
		randSource = saved
		if err != nil {
			t.Fatalf("%s: rebuild from committed randoms: %v", key, err)
		}
		checkBytes(key+" rebuilt from committed randoms", out.Bytes(), mustRead(t, filepath.Join(vecDir, c.File)))
	}
}

func parseHeaderDigest(t *testing.T, b []byte, cred string) [digestSize]byte {
	t.Helper()
	h, _, err := openHead(bytes.NewReader(b), NewCredential(cred))
	if err != nil {
		t.Fatal(err)
	}
	return h.HeaderDigest
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tv, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return tv
}

func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
