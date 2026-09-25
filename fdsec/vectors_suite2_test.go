package fdsec

import (
	"bytes"
	"crypto/sha256" //nolint:gosec // fixed test seed derivation, not security
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
)

// The committed suite-2 vectors (FDSEC-FORMAT.md section 16 points 11-14,
// section 18). They live in their own folder beside suites 1 and 3.
//
// Regenerate after a deliberate format change - which for suite 2 can only
// mean a new try-list entry, never a change to a pinned one:
//
//	FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite2

const vec2Dir = "testdata/vectors/suite2"

// vec2Frame names one frame's inputs and where its ciphertext sits in the
// committed file; the ciphertext itself is the file, so it is not repeated.
type vec2Frame struct {
	Nonce  hexBytes `json:"nonce"`
	AD     hexBytes `json:"associated_data"`
	Offset int64    `json:"offset"`
	Length int64    `json:"length_with_tag"`
	Tag    hexBytes `json:"tag"`
}

type vec2Container struct {
	Description  string       `json:"description"`
	Name         string       `json:"name"`
	Content      hexBytes     `json:"content"`
	Credential   string       `json:"credential"`
	EncodedAt    string       `json:"encoded_at"`
	RandomStream hexBytes     `json:"random_inputs_in_draw_order"`
	Salt         hexBytes     `json:"salt"`
	Nonce        hexBytes     `json:"nonce"`
	PwSeed       hexBytes     `json:"pepper_fold"`
	Key          hexBytes     `json:"key"`
	BodyHead     hexBytes     `json:"body_head"`
	TailLen      int64        `json:"tail_len"`
	Frames       []*vec2Frame `json:"frames"`
	TotalLen     int64        `json:"total_len"`
	File         string       `json:"file"`
	FileIdent    string       `json:"blake2b256_of_file"`
}

type vec2File struct {
	Format        string                    `json:"format"`
	Generated     string                    `json:"generated"`
	TryList       []Suite2Profile           `json:"kdf_try_list"`
	Pepper        hexBytes                  `json:"pepper"`
	ADPrefix      string                    `json:"associated_data_prefix"`
	FramePlain    int64                     `json:"frame_plaintext_bytes"`
	KeyEmptyCred  hexBytes                  `json:"key_empty_credential"`
	SeedEmptyCred hexBytes                  `json:"pepper_fold_empty_credential"`
	Containers    map[string]*vec2Container `json:"containers"`
}

type vec2Case struct {
	key, desc, name string
	content         []byte
}

func vec2Cases() []vec2Case {
	return []vec2Case{
		{"empty", "an empty original (N = 0): one frame", "empty.txt", []byte{}},
		{"onebyte", "a one-byte original: one frame", "one.bin", []byte("A")},
		{"multiframe", "a 70000-byte original: the body spans two frames", "multi.bin", bytes.Repeat([]byte("0123456789abcdef"), 4375)},
	}
}

var vec2EncodedAt = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func pepperFold(cred Credential) []byte {
	h, _ := blake2b.New512(suite2Pepper[:])
	h.Write(cred)
	return h.Sum(nil)
}

func buildVector2(t *testing.T, c vec2Case, cred string) (*vec2Container, []byte) {
	t.Helper()
	stream := vecStream("fdsec-vector-suite2/"+c.key, 1<<17)
	src := bytes.NewReader(stream)
	saved := randSource
	randSource = src
	var out bytes.Buffer
	_, err := PackSuite2(&out, bytes.NewReader(c.content), Metadata{Name: c.name, Size: int64(len(c.content)), EncodedAt: vec2EncodedAt}, NewCredential(cred))
	randSource = saved
	if err != nil {
		t.Fatalf("%s: PackSuite2: %v", c.key, err)
	}
	used := stream[:len(stream)-src.Len()]
	b := out.Bytes()

	salt, nonce := b[:s2SaltSize], b[s2SaltSize:s2Lead]
	if !bytes.Equal(used[:s2Lead], b[:s2Lead]) {
		t.Fatalf("%s: the salt and nonce are not the first 40 random bytes", c.key)
	}
	key, err := deriveSuite2Key(NewCredential(cred), salt, suite2Profiles[0])
	if err != nil {
		t.Fatal(err)
	}
	st, err := openSuite2(bytes.NewReader(b), NewCredential(cred))
	if err != nil {
		t.Fatalf("%s: open: %v", c.key, err)
	}
	// Draw order: salt, nonce, name fill, size fill, u32le tail draw, tail.
	off := s2Lead + (s2NameBlock - len(c.name) - 1) + (s2SizeBlock - len(fmt.Sprint(len(c.content))) - 1)
	tailLen := int64(s2TailMin + binary.LittleEndian.Uint32(used[off:])%(s2TailMax-s2TailMin+1))
	if int64(len(used)) != int64(off)+4+tailLen {
		t.Fatalf("%s: %d random bytes drawn, the draw order accounts for %d", c.key, len(used), int64(off)+4+tailLen)
	}

	v := &vec2Container{
		Description:  c.desc,
		Name:         c.name,
		Content:      c.content,
		Credential:   cred,
		EncodedAt:    vec2EncodedAt.Format(time.RFC3339),
		RandomStream: bytes.Clone(used),
		Salt:         bytes.Clone(salt),
		Nonce:        bytes.Clone(nonce),
		PwSeed:       pepperFold(NewCredential(cred)),
		Key:          key,
		BodyHead:     bytes.Clone(st.first[:s2BodyHead]),
		TailLen:      tailLen,
		TotalLen:     int64(len(b)),
		File:         "quiet-" + c.key + ".bin",
		FileIdent:    hex.EncodeToString(mustBlake(b)),
	}
	a, _ := newXAEAD(key)
	for i := int64(0); i < st.frames; i++ {
		l := int64(s2Frame)
		if i == st.frames-1 {
			l = st.lastLen
		}
		ct := b[s2Lead+i*s2Frame : s2Lead+i*s2Frame+l]
		fn, ad := suite2FrameNonce(nonce, i), suite2FrameAD(i, i == st.frames-1)
		if _, err := a.Open(nil, fn, ct, ad); err != nil {
			t.Fatalf("%s: frame %d: %v", c.key, i, err)
		}
		v.Frames = append(v.Frames, &vec2Frame{Nonce: fn, AD: ad, Offset: s2Lead + i*s2Frame, Length: l, Tag: bytes.Clone(ct[len(ct)-tagSize:])})
	}
	return v, b
}

func TestVectors_Suite2(t *testing.T) {
	saved := suite2Profiles
	suite2Profiles = suite2ProfilesV1
	defer func() { suite2Profiles = saved }()

	vf := &vec2File{
		Format:     "FDSEC suite 2 (the quiet suite) - see FDSEC-FORMAT.md sections 16 and 18, the contract these vectors ship beside",
		Generated:  "2026-09-25",
		TryList:    suite2ProfilesV1,
		Pepper:     suite2Pepper[:],
		ADPrefix:   s2AD,
		FramePlain: s2FramePlain,
		Containers: map[string]*vec2Container{},
	}
	files := map[string][]byte{}
	for _, c := range vec2Cases() {
		v, b := buildVector2(t, c, vecSlowCred)
		vf.Containers[c.key] = v
		files[v.File] = b
	}
	vf.SeedEmptyCred = pepperFold(NewCredential(""))
	var err error
	if vf.KeyEmptyCred, err = deriveSuite2Key(NewCredential(""), vf.Containers["empty"].Salt, suite2ProfilesV1[0]); err != nil {
		t.Fatal(err)
	}
	if len(vf.Containers["multiframe"].Frames) != 2 {
		t.Fatalf("the multiframe vector has %d frames, want 2", len(vf.Containers["multiframe"].Frames))
	}

	if os.Getenv("FDSEC_WRITE_VECTORS") == "1" {
		if err := os.MkdirAll(vec2Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		j, err := json.MarshalIndent(vf, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		files[vecJSON] = append(j, '\n')
		prov := "# Vendored copy of the FDSEC-FORMAT suite-2 conformance vectors from the shared contracts catalog.\n# Never edit the vectors here: re-vendor them from the catalog and rewrite the sha256 rows.\n# Asserted by TestVectors_Suite2.\ncontract: FDSEC-FORMAT\nversion: 1.3\nvendored: 2026-09-25\n"
		for _, n := range sortedKeys(files) {
			if err := os.WriteFile(filepath.Join(vec2Dir, n), files[n], 0o644); err != nil {
				t.Fatal(err)
			}
			prov += fmt.Sprintf("sha256 %s %x\n", n, sha256.Sum256(files[n]))
		}
		if err := os.WriteFile(filepath.Join(vec2Dir, "PROVENANCE.txt"), []byte(prov), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("suite-2 vectors rewritten to %s", vec2Dir)
	}

	provRaw, err := os.ReadFile(filepath.Join(vec2Dir, "PROVENANCE.txt"))
	if err != nil {
		t.Fatalf("committed suite-2 PROVENANCE.txt missing (run FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite2): %v", err)
	}
	named := 0
	for _, line := range strings.Split(string(provRaw), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 3 && parts[0] == "sha256" {
			named++
			if got := fmt.Sprintf("%x", sha256.Sum256(mustRead(t, filepath.Join(vec2Dir, parts[1])))); got != parts[2] {
				t.Fatalf("PROVENANCE sha256 mismatch for %s", parts[1])
			}
		}
	}
	if named != len(vec2Cases())+1 {
		t.Fatalf("PROVENANCE names %d files, want %d", named, len(vec2Cases())+1)
	}
	if catDir := os.Getenv("FDSEC_CATALOG_VECTORS"); catDir != "" {
		for _, n := range append([]string{vecJSON, "PROVENANCE.txt"}, sortedKeys(files)...) {
			if !bytes.Equal(mustRead(t, filepath.Join(vec2Dir, n)), mustRead(t, filepath.Join(catDir, "suite2", n))) {
				t.Fatalf("catalog suite-2 vector file %s does not match the local fixture", n)
			}
		}
	}

	var committed vec2File
	if err := json.Unmarshal(mustRead(t, filepath.Join(vec2Dir, vecJSON)), &committed); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed.Pepper, suite2Pepper[:]) {
		t.Fatal("the pepper drifted from the committed vector - every suite-2 file ever written would stop opening")
	}
	if fmt.Sprint(committed.TryList) != fmt.Sprint(suite2ProfilesV1) {
		t.Fatalf("the try-list %v drifted from the committed %v", suite2ProfilesV1, committed.TryList)
	}
	if !bytes.Equal(committed.KeyEmptyCred, vf.KeyEmptyCred) || !bytes.Equal(committed.SeedEmptyCred, vf.SeedEmptyCred) {
		t.Fatal("the empty-credential derivation drifted")
	}
	for _, c := range vec2Cases() {
		want, got := vf.Containers[c.key], committed.Containers[c.key]
		if got == nil {
			t.Fatalf("committed suite-2 vectors have no %q container", c.key)
		}
		if !bytes.Equal(files[want.File], mustRead(t, filepath.Join(vec2Dir, got.File))) {
			t.Fatalf("%s: the file drifted from the committed vector", c.key)
		}
		for what, pair := range map[string][2][]byte{
			"random stream": {want.RandomStream, got.RandomStream},
			"pepper fold":   {want.PwSeed, got.PwSeed},
			"key":           {want.Key, got.Key},
			"body head":     {want.BodyHead, got.BodyHead},
		} {
			if !bytes.Equal(pair[0], pair[1]) {
				t.Fatalf("%s: %s drifted from the committed vector", c.key, what)
			}
		}
		if len(want.Frames) != len(got.Frames) || want.TailLen != got.TailLen {
			t.Fatalf("%s: frame layout drifted", c.key)
		}
		for i := range want.Frames {
			w, g := want.Frames[i], got.Frames[i]
			if !bytes.Equal(w.Nonce, g.Nonce) || !bytes.Equal(w.AD, g.AD) || w.Offset != g.Offset || w.Length != g.Length || !bytes.Equal(w.Tag, g.Tag) {
				t.Fatalf("%s frame %d drifted", c.key, i)
			}
		}

		// The third-party property: the committed random stream and the
		// document reproduce the committed file, and it opens.
		saved := randSource
		randSource = bytes.NewReader(got.RandomStream)
		var out bytes.Buffer
		_, err := PackSuite2(&out, bytes.NewReader(got.Content), Metadata{Name: got.Name, Size: int64(len(got.Content)), EncodedAt: vec2EncodedAt}, NewCredential(got.Credential))
		randSource = saved
		if err != nil || !bytes.Equal(out.Bytes(), mustRead(t, filepath.Join(vec2Dir, got.File))) {
			t.Fatalf("%s: rebuilding from the committed randoms does not reproduce the file (%v)", c.key, err)
		}
		var plain bytes.Buffer
		m, err := Unpack(&plain, bytes.NewReader(out.Bytes()), NewCredential(got.Credential))
		if err != nil || !bytes.Equal(plain.Bytes(), c.content) || m.Name != c.name || !m.EncodedAt.Equal(vec2EncodedAt) {
			t.Fatalf("%s: committed file does not unpack (%v)", c.key, err)
		}
		if blake := blake2b.Sum256(out.Bytes()); hex.EncodeToString(blake[:]) != got.FileIdent {
			t.Fatalf("%s: file digest drifted", c.key)
		}
	}
}

// TestSuite1_StillOpens is gate G5's second half (SP-0019 section 2): every
// container committed before suite 2 existed - the suite-1 vectors and the
// suite-3 vectors - still opens through the dispatching reader, under the real
// profiles, byte-exact, and still refuses a wrong credential with the single
// outcome. Proven on the files, not asserted.
func TestSuite1_StillOpens(t *testing.T) {
	saved1, saved2 := activeProfile, suite2Profiles
	activeProfile, suite2Profiles = profileV1, suite2ProfilesV1
	defer func() { activeProfile, suite2Profiles = saved1, saved2 }()

	var v1 vectorsFile
	if err := json.Unmarshal(mustRead(t, filepath.Join(vecDir, vecJSON)), &v1); err != nil {
		t.Fatal(err)
	}
	opened := 0
	for key, vc := range v1.Containers {
		if vc.File == "" {
			continue
		}
		b := mustRead(t, filepath.Join(vecDir, vc.File))
		c, err := Open(bytes.NewReader(b), NewCredential(vc.Credential))
		if err != nil || c.Suite() != SuiteID1 {
			t.Fatalf("suite-1 vector %s no longer opens as suite 1: %v", key, err)
		}
		var out bytes.Buffer
		m, err := c.Unpack(&out)
		if err != nil || !bytes.Equal(out.Bytes(), vc.Content) || m.Name != vc.Name {
			t.Fatalf("suite-1 vector %s does not unpack byte-exact: %v", key, err)
		}
		if _, err := Open(bytes.NewReader(b), NewCredential(vc.Credential+"x")); err == nil || !strings.Contains(err.Error(), ErrCredentialOrTamper.Error()) {
			t.Fatalf("suite-1 vector %s, wrong credential: %v", key, err)
		}
		opened++
	}

	var v3 vec3File
	if err := json.Unmarshal(mustRead(t, filepath.Join(vec3Dir, vecJSON)), &v3); err != nil {
		t.Fatal(err)
	}
	for key, vc := range v3.Containers {
		b := mustRead(t, filepath.Join(vec3Dir, vc.File))
		c, err := Open(bytes.NewReader(b), NewCredential(vc.Credential))
		if err != nil || c.Suite() != SuiteID3 {
			t.Fatalf("suite-3 vector %s no longer opens as suite 3: %v", key, err)
		}
		if _, _, err := c.VerifyTree(); err != nil {
			t.Fatalf("suite-3 vector %s does not verify: %v", key, err)
		}
		opened++
	}
	if opened < 2+len(vec3Cases()) {
		t.Fatalf("opened %d committed containers, want at least %d", opened, 2+len(vec3Cases()))
	}
}
