package fdsec

import (
	"bytes"
	"crypto/sha256" //nolint:gosec // fixed test seed derivation, not security
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/blake2b"
)

// The committed suite-3 vectors (FDSEC-FORMAT.md section 16 points 7-10,
// section 17). They live beside suite 1's, in their own folder, so neither
// suite's regeneration can disturb the other's files.
//
// Regenerate after a deliberate format change:
//
//	FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite3

const vec3Dir = "testdata/vectors/suite3"

type vec3Entry struct {
	Path       string   `json:"path"`
	Kind       string   `json:"kind"`
	Content    hexBytes `json:"content,omitempty"`
	CreatedAt  string   `json:"created_at"`
	AccessedAt string   `json:"accessed_at"`
	ModifiedAt string   `json:"modified_at"`
}

type vec3Stream struct {
	What      string      `json:"stream"`
	Offset    int64       `json:"offset"`
	Plaintext int64       `json:"plaintext_len"`
	GapPad    hexBytes    `json:"gap_pad_after"`
	Chunks    []*vecChunk `json:"chunks"`
}

type vec3Container struct {
	Description    string       `json:"description"`
	RootName       string       `json:"root_name"`
	EntriesAsGiven []vec3Entry  `json:"entries_in_input_order"`
	ManifestOrder  []string     `json:"manifest_order"`
	Credential     string       `json:"credential"`
	Params         Params       `json:"params"`
	EncodedAt      string       `json:"encoded_at"`
	RootCreatedAt  string       `json:"root_created_at"`
	RootAccessedAt string       `json:"root_accessed_at"`
	RootModifiedAt string       `json:"root_modified_at"`
	RandomStream   hexBytes     `json:"random_inputs_in_draw_order"`
	MaskKey        hexBytes     `json:"mask_key"`
	KEK            hexBytes     `json:"kek"`
	FileKey        hexBytes     `json:"file_key"`
	HeaderPlain    hexBytes     `json:"header_unmasked"`
	SlotsPlain     hexBytes     `json:"slots_unmasked"`
	DirBlockPlain  hexBytes     `json:"directory_block_unmasked"`
	DirBlockSealed hexBytes     `json:"directory_block_sealed"`
	ManifestPlain  hexBytes     `json:"manifest_plaintext"`
	Streams        []vec3Stream `json:"streams"`
	TotalLen       int64        `json:"total_len"`
	PreLen         int64        `json:"payload_offset"`
	File           string       `json:"file"`
	FileIdent      string       `json:"blake2b256_of_file"`
}

type vec3File struct {
	Format     string                    `json:"format"`
	Generated  string                    `json:"generated"`
	KDF        map[string]uint32         `json:"kdf_profile"`
	Containers map[string]*vec3Container `json:"containers"`
}

type vec3Case struct {
	key, desc, root string
	params          Params
	items           map[string][]byte
	order           []string
}

func vec3Cases() []vec3Case {
	small := Params{ClusterAlignment: 512, ChunkSize: 1024} // P = 1008
	return []vec3Case{
		{key: "empty", desc: "an empty directory: zero entries", root: "empty-dir",
			params: DefaultParams(), items: map[string][]byte{}, order: nil},
		{key: "onesubdir", desc: "a directory holding one empty subdirectory and no files", root: "only-subdir",
			params: DefaultParams(), items: map[string][]byte{"sub": nil}, order: []string{"sub"}},
		{key: "unsorted", desc: "entries created in an order that is not their byte-wise sorted order", root: "unsorted",
			params: small,
			items: map[string][]byte{
				"zeta.txt": []byte("z"), "beta": nil, "Alpha": nil, "Alpha/b.txt": []byte("bb"), "alpha.txt": []byte("a"),
			},
			order: []string{"zeta.txt", "beta", "Alpha", "alpha.txt", "Alpha/b.txt"}},
		{key: "multichunk", desc: "more than one entry needs more than one chunk (small chunk size)", root: "multichunk",
			params: small,
			items: map[string][]byte{
				"big1.bin":     bytes.Repeat([]byte("0123456789abcdef"), 157)[:2500], // 3 chunks
				"dir":          nil,
				"dir/big2.bin": bytes.Repeat([]byte("fedcba9876543210"), 94)[:1500], // 2 chunks
				"empty.bin":    {},
			},
			order: []string{"empty.bin", "dir", "dir/big2.bin", "big1.bin"}},
	}
}

func vec3Tree(c vec3Case) *Tree {
	tr := memTree(c.root, c.items, c.order)
	tr.Meta.EncodedAt = vecTimes.EncodedAt
	tr.Meta.CreatedAt = vecTimes.CreatedAt
	tr.Meta.AccessedAt = vecTimes.AccessedAt
	tr.Meta.ModifiedAt = vecTimes.ModifiedAt
	return tr
}

// buildVector3 packs one case under a fixed random stream and spells the
// container out field by field.
func buildVector3(t *testing.T, c vec3Case, cred string) (*vec3Container, []byte) {
	t.Helper()
	stream := vecStream("fdsec-vector-suite3/"+c.key, 1<<16)
	src := bytes.NewReader(stream)
	saved := randSource
	randSource = src
	var out bytes.Buffer
	_, err := PackTree(&out, vec3Tree(c), NewCredential(cred), c.params)
	randSource = saved
	if err != nil {
		t.Fatalf("%s: PackTree: %v", c.key, err)
	}
	used := stream[:len(stream)-src.Len()]
	b := out.Bytes()

	salt := used[:saltSize]
	fileKey := used[saltSize : saltSize+fileKeySize]
	maskKey, kek, err := deriveKeys(NewCredential(cred), salt)
	if err != nil {
		t.Fatal(err)
	}
	plainHead := bytes.Clone(b[:preMeta])
	if err := mask(maskKey, plainHead[maskOff:]); err != nil {
		t.Fatal(err)
	}
	ct, err := Open(bytes.NewReader(b), NewCredential(cred))
	if err != nil {
		t.Fatalf("%s: open: %v", c.key, err)
	}
	h := ct.h
	nonce, _ := deriveNonce(fileKey, dirCtx())
	dirPlain, err := ct.aead.Open(nil, nonce, b[preMeta:preMeta+metaSize], adDir(h.HeaderDigest))
	if err != nil {
		t.Fatal(err)
	}
	sink := newMemSink()
	tm, entries, err := ct.UnpackTree(sink)
	if err != nil {
		t.Fatalf("%s: unpack: %v", c.key, err)
	}
	manifest := encodeManifest(entries)

	v := &vec3Container{
		Description:    c.desc,
		RootName:       c.root,
		Credential:     cred,
		Params:         c.params,
		EncodedAt:      tm.EncodedAt.Format(time.RFC3339Nano),
		RootCreatedAt:  tm.CreatedAt.Format(time.RFC3339Nano),
		RootAccessedAt: tm.AccessedAt.Format(time.RFC3339Nano),
		RootModifiedAt: tm.ModifiedAt.Format(time.RFC3339Nano),
		RandomStream:   bytes.Clone(used),
		MaskKey:        maskKey,
		KEK:            kek,
		FileKey:        bytes.Clone(fileKey),
		HeaderPlain:    bytes.Clone(plainHead[:headerSize]),
		SlotsPlain:     bytes.Clone(plainHead[headerSize:preMeta]),
		DirBlockPlain:  dirPlain,
		DirBlockSealed: bytes.Clone(b[preMeta : preMeta+metaSize]),
		ManifestPlain:  manifest,
		TotalLen:       int64(len(b)),
		PreLen:         h.preLen(),
		File:           "dir-" + c.key + ".fd-sec",
		FileIdent:      hex.EncodeToString(mustBlake(b)),
	}
	for _, p := range c.order {
		e := vec3Entry{Path: p, Kind: "file", Content: c.items[p]}
		if c.items[p] == nil {
			e.Kind = "directory"
		}
		for _, te := range vec3Tree(c).Entries {
			if te.Path == p {
				e.CreatedAt = te.CreatedAt.Format(time.RFC3339Nano)
				e.AccessedAt = te.AccessedAt.Format(time.RFC3339Nano)
				e.ModifiedAt = te.ModifiedAt.Format(time.RFC3339Nano)
			}
		}
		v.EntriesAsGiven = append(v.EntriesAsGiven, e)
	}
	for _, e := range entries {
		v.ManifestOrder = append(v.ManifestOrder, e.Path)
	}

	// Every stream, chunk by chunk, with its nonce and associated data.
	spell := func(what string, id streamID, off, n int64) int64 {
		k := h.chunkCount(n)
		s := vec3Stream{What: what, Offset: off, Plaintext: n}
		pos := off
		for i := int64(0); i < k; i++ {
			l := h.chunkCapacity()
			if i == k-1 {
				l = h.lastLen(n)
			}
			cn, _ := deriveNonce(fileKey, id.nonceCtx(i))
			s.Chunks = append(s.Chunks, &vecChunk{
				Nonce:      cn,
				AD:         id.ad(h.HeaderDigest, i, k, i == k-1, l),
				Ciphertext: bytes.Clone(b[pos : pos+l+tagSize]),
			})
			pos = off + (i+1)*int64(h.ChunkSize)
		}
		end := off + h.streamLen(n)
		s.GapPad = bytes.Clone(b[end:h.alignUp(end)])
		v.Streams = append(v.Streams, s)
		return h.alignUp(end)
	}
	off := spell("manifest", streamID{manifest: true}, h.preLen(), int64(len(manifest)))
	for i, e := range entries {
		if e.Kind == KindFile {
			off = spell("entry "+e.Path, streamID{entry: int64(i)}, off, e.Size)
		}
	}
	if off != int64(len(b)) {
		t.Fatalf("%s: spelled streams end at %d, the container is %d bytes", c.key, off, len(b))
	}
	return v, b
}

func TestVectors_Suite3(t *testing.T) {
	saved := activeProfile
	activeProfile = profileV1
	defer func() { activeProfile = saved }()

	vf := &vec3File{
		Format:     "FDSEC suite 3 (directory container), format version 1 - see FDSEC-FORMAT.md sections 16 and 17, the contract these vectors ship beside",
		Generated:  "2026-09-25",
		KDF:        map[string]uint32{"m_kib": profileV1.MemoryKiB, "t": profileV1.Time, "p": uint32(profileV1.Lanes), "threshold": profileV1.Threshold},
		Containers: map[string]*vec3Container{},
	}
	files := map[string][]byte{}
	for _, c := range vec3Cases() {
		v, b := buildVector3(t, c, vecSlowCred)
		vf.Containers[c.key] = v
		files[v.File] = b
	}

	if os.Getenv("FDSEC_WRITE_VECTORS") == "1" {
		if err := os.MkdirAll(vec3Dir, 0o755); err != nil {
			t.Fatal(err)
		}
		j, err := json.MarshalIndent(vf, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		files[vecJSON] = append(j, '\n')
		names := make([]string, 0, len(files))
		for n := range files {
			names = append(names, n)
		}
		sort.Strings(names)
		prov := "# Vendored copy of the FDSEC-FORMAT suite-3 conformance vectors from the shared contracts catalog.\n# Never edit the vectors here: re-vendor them from the catalog and rewrite the sha256 rows.\n# Asserted by TestVectors_Suite3.\ncontract: FDSEC-FORMAT\nversion: 1.2\nvendored: 2026-09-25\n"
		for _, n := range names {
			if err := os.WriteFile(filepath.Join(vec3Dir, n), files[n], 0o644); err != nil {
				t.Fatal(err)
			}
			prov += fmt.Sprintf("sha256 %s %x\n", n, sha256.Sum256(files[n]))
		}
		if err := os.WriteFile(filepath.Join(vec3Dir, "PROVENANCE.txt"), []byte(prov), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("suite-3 vectors rewritten to %s", vec3Dir)
	}

	// Every committed file is the one PROVENANCE names.
	provRaw, err := os.ReadFile(filepath.Join(vec3Dir, "PROVENANCE.txt"))
	if err != nil {
		t.Fatalf("committed suite-3 PROVENANCE.txt missing (run FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Suite3): %v", err)
	}
	named := 0
	for _, line := range strings.Split(string(provRaw), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 3 && parts[0] == "sha256" {
			named++
			if got := fmt.Sprintf("%x", sha256.Sum256(mustRead(t, filepath.Join(vec3Dir, parts[1])))); got != parts[2] {
				t.Fatalf("PROVENANCE sha256 mismatch for %s", parts[1])
			}
		}
	}
	if named != len(vec3Cases())+1 {
		t.Fatalf("PROVENANCE names %d files, want %d", named, len(vec3Cases())+1)
	}
	if catDir := os.Getenv("FDSEC_CATALOG_VECTORS"); catDir != "" {
		for _, n := range append([]string{vecJSON, "PROVENANCE.txt"}, sortedKeys(files)...) {
			if n == vecJSON && files[n] == nil {
				continue
			}
			if !bytes.Equal(mustRead(t, filepath.Join(vec3Dir, n)), mustRead(t, filepath.Join(catDir, "suite3", n))) {
				t.Fatalf("catalog suite-3 vector file %s does not match the local fixture", n)
			}
		}
	}

	var committed vec3File
	if err := json.Unmarshal(mustRead(t, filepath.Join(vec3Dir, vecJSON)), &committed); err != nil {
		t.Fatal(err)
	}
	for _, c := range vec3Cases() {
		want := vf.Containers[c.key]
		got := committed.Containers[c.key]
		if got == nil {
			t.Fatalf("committed suite-3 vectors have no %q container", c.key)
		}
		// The container the code writes today is the committed one, byte for byte.
		if !bytes.Equal(files[want.File], mustRead(t, filepath.Join(vec3Dir, got.File))) {
			t.Fatalf("%s: the container drifted from the committed vector", c.key)
		}
		for what, pair := range map[string][2][]byte{
			"mask key":         {want.MaskKey, got.MaskKey},
			"kek":              {want.KEK, got.KEK},
			"unmasked header":  {want.HeaderPlain, got.HeaderPlain},
			"unmasked slots":   {want.SlotsPlain, got.SlotsPlain},
			"directory block":  {want.DirBlockPlain, got.DirBlockPlain},
			"sealed dir block": {want.DirBlockSealed, got.DirBlockSealed},
			"manifest":         {want.ManifestPlain, got.ManifestPlain},
			"random stream":    {want.RandomStream, got.RandomStream},
		} {
			if !bytes.Equal(pair[0], pair[1]) {
				t.Fatalf("%s: %s drifted from the committed vector", c.key, what)
			}
		}
		if len(got.Streams) != len(want.Streams) {
			t.Fatalf("%s: %d streams committed, %d written", c.key, len(got.Streams), len(want.Streams))
		}
		for i := range want.Streams {
			ws, gs := want.Streams[i], got.Streams[i]
			if ws.Offset != gs.Offset || ws.Plaintext != gs.Plaintext || len(ws.Chunks) != len(gs.Chunks) {
				t.Fatalf("%s stream %d layout drifted", c.key, i)
			}
			for j := range ws.Chunks {
				if !bytes.Equal(ws.Chunks[j].Nonce, gs.Chunks[j].Nonce) || !bytes.Equal(ws.Chunks[j].AD, gs.Chunks[j].AD) || !bytes.Equal(ws.Chunks[j].Ciphertext, gs.Chunks[j].Ciphertext) {
					t.Fatalf("%s stream %d chunk %d drifted", c.key, i, j)
				}
			}
		}

		// The third-party property: the committed random stream plus the
		// document reproduce the committed container, and it opens.
		saved := randSource
		randSource = bytes.NewReader(got.RandomStream)
		var out bytes.Buffer
		_, err := PackTree(&out, vec3Tree(c), NewCredential(got.Credential), got.Params)
		randSource = saved
		if err != nil || !bytes.Equal(out.Bytes(), mustRead(t, filepath.Join(vec3Dir, got.File))) {
			t.Fatalf("%s: rebuilding from the committed randoms does not reproduce the file (%v)", c.key, err)
		}
		sink, _, _, err := unpackTreeBytes(out.Bytes(), NewCredential(got.Credential))
		if err != nil {
			t.Fatalf("%s: committed container does not unpack: %v", c.key, err)
		}
		for p, content := range c.items {
			if content != nil && !bytes.Equal(sink.files[p].Bytes(), content) {
				t.Fatalf("%s: %s did not round-trip", c.key, p)
			}
		}
		if fmt.Sprint(got.ManifestOrder) != fmt.Sprint(sink.order) {
			t.Fatalf("%s: manifest order %v, unpacked %v", c.key, got.ManifestOrder, sink.order)
		}
		if blake := blake2b.Sum256(out.Bytes()); hex.EncodeToString(blake[:]) != got.FileIdent {
			t.Fatalf("%s: file digest drifted", c.key)
		}
	}
	if u := committed.Containers["unsorted"]; u != nil {
		given := make([]string, 0, len(u.EntriesAsGiven))
		for _, e := range u.EntriesAsGiven {
			given = append(given, e.Path)
		}
		if fmt.Sprint(given) == fmt.Sprint(u.ManifestOrder) {
			t.Fatal("the unsorted vector's input order is already sorted - it proves nothing")
		}
	}
}

func sortedKeys(m map[string][]byte) []string {
	var k []string
	for n := range m {
		k = append(k, n)
	}
	sort.Strings(k)
	return k
}
