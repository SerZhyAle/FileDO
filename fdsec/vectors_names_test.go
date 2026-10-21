package fdsec

import (
	"bytes"
	"crypto/sha256" //nolint:gosec // fixed test seed derivation, not security
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

// The sealed-name boundary vectors that FDSEC-FORMAT 1.4 section 20 owes: an
// authenticated container for each case the amendment names, in every suite -
// the four characters U+0022 U+003C U+003E U+007C (valid), the code points
// around them and the names that merely look like a refused one (valid), and
// the names section 7 still refuses (damaged, authenticated, so the answer is
// "damaged" and never "wrong credential").
//
// They live in their own folder, testdata/vectors/names, beside the committed
// vectors of suites 1, 2 and 3 and never inside them: section 20 says the
// existing vectors stay untouched, and a new folder is the only way a
// regeneration cannot reach them. Regenerate after a deliberate change:
//
//	FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Names
//
// (that run writes only this folder; the three existing vector tests rewrite
// theirs only when they are named in -run).

const (
	vecNamesDir  = "testdata/vectors/names"
	vecNamesSeed = "fdsec-vector-names/"
)

// vecNamesCred is a credential of more than 64 UTF-8 bytes. Suites 1 and 3 take
// the fast branch of the key derivation for it, which keeps a corpus of a
// hundred containers quick to write and to replay; suite 2 has no fast branch
// and pays its Argon2id try-list as always. The cases are about names, not
// about the derivation, which suite 1's own vectors pin on both branches.
const vecNamesCred = vecFastCred

// namesEntrySpec is one entry of a suite-3 case.
type namesEntrySpec struct {
	path    string
	dir     bool
	content []byte
}

// namesSpec is what a case is made of; the container is built from it.
type namesSpec struct {
	id, desc string
	suite    int
	expect   string // "opens" or "damaged"
	refused  string // for "damaged": which element carries the refused name
	name     string // suites 1, 2: the sealed name; suite 3: the root's name
	content  []byte // suites 1, 2: the original's bytes
	entries  []namesEntrySpec
	// How a "damaged" container is made. Suite 3 has two places a name lives:
	// the root (forged by re-sealing the directory block) and the manifest
	// (written by packTree directly, past the checked writer).
	forgeRoot bool
}

type vecNamesEntry struct {
	Kind    string   `json:"kind"`
	Path    string   `json:"path"`
	PathHex hexBytes `json:"path_hex"`
	Content hexBytes `json:"content,omitempty"`
}

type vecNamesCase struct {
	ID          string          `json:"id"`
	Suite       int             `json:"suite"`
	Expect      string          `json:"expect"`
	Refused     string          `json:"refused_element,omitempty"`
	Description string          `json:"description"`
	SealedName  string          `json:"sealed_name"`
	SealedHex   hexBytes        `json:"sealed_name_hex"`
	Content     hexBytes        `json:"content,omitempty"`
	Entries     []vecNamesEntry `json:"entries,omitempty"`
	Credential  string          `json:"credential"`
	Params      *Params         `json:"params,omitempty"`
	File        string          `json:"file"`
	TotalLen    int64           `json:"total_len"`
	FileIdent   string          `json:"blake2b256_of_file"`
}

type vecNamesFile struct {
	Format      string            `json:"format"`
	Generated   string            `json:"generated"`
	Contract    string            `json:"contract"`
	HowToReplay []string          `json:"how_to_replay"`
	KDF         map[string]uint32 `json:"kdf_profile"`
	Suite2      []Suite2Profile   `json:"suite2_kdf_try_list"`
	NotCovered  []string          `json:"not_covered"`
	Cases       []*vecNamesCase   `json:"cases"`
}

// namesValidList are names the format accepts, for suites 1, 2 and (as the
// root and as a component) 3.
func namesValidList() []struct{ id, desc, name string } {
	return []struct{ id, desc, name string }{
		{"quote", "U+0022 inside a name", `a"b.txt`},
		{"less-than", "U+003C inside a name", `a<b.txt`},
		{"greater-than", "U+003E inside a name", `a>b.txt`},
		{"vertical-bar", "U+007C inside a name", `a|b.txt`},
		{"all-four", "a name made only of the four characters", `"<>|`},
		{"all-four-inner", "the four characters inside one name, with a dot and an extension", `x"y<z>w|v.txt`},
		{"edges", "the four characters first and last in the name", `|first-and-last>`},
		{"neighbours", "the code points next to the four - ! # ; = { } - and a dot", `!#;={}.txt`},
		{"dot-and-space", "a dot and a space inside a name", `a b.c.txt`},
		{"lookalike-con", "starts like the reserved stem CON but is not one", `CONTRACT.pdf`},
		{"lookalike-com10", "COM10 is not a reserved device name", `com10.log`},
		{"units-255", "255 UTF-16 units, the longest name a Windows file system holds", strings.Repeat("n", 251) + ".txt"},
		{"units-256", "256 UTF-16 units: valid in the format, one more than a Windows file name holds", strings.Repeat("n", 252) + ".txt"},
	}
}

// namesRefusedList are names section 7 still refuses; the last two show that
// a valid character beside a refused element launders nothing.
func namesRefusedList() []struct{ id, desc, name string } {
	return []struct{ id, desc, name string }{
		{"empty", "an empty name", ""},
		{"dot", "the current-directory reference", "."},
		{"dot-dot", "the parent-directory reference", ".."},
		{"slash", "a forward slash", "a/b"},
		{"backslash", "a backslash", `a\b`},
		{"colon", "a colon (an alternate data stream on NTFS)", "a:b"},
		{"star", "the wildcard *", "a*b"},
		{"question", "the wildcard ?", "a?b"},
		{"control", "a control character, U+0001", "a\x01b"},
		{"delete", "U+007F", "a\x7fb"},
		{"reserved-con", "the reserved device name CON", "CON"},
		{"reserved-ext", "a reserved stem with an extension, nul.txt", "nul.txt"},
		{"reserved-com9", "the reserved device name COM9", "COM9"},
		{"trailing-dot", "a name that ends in a dot", "trailing."},
		{"trailing-space", "a name that ends in a space", "trailing "},
		{"four-and-slash", "a valid character beside a separator is still refused", `a|b/c`},
		{"four-and-colon", "a valid character beside a colon is still refused", `a"b:c`},
		{"invalid-utf8", "a byte that is not UTF-8 (0xFF between two letters): damaged in every suite, never replaced by U+FFFD (FDSEC-FORMAT 1.5 section 22)", "a\xffb"},
	}
}

// jsonName is what a name looks like in vectors.json: encoding/json writes each
// byte that is not UTF-8 as U+FFFD, so the text field is lossy for exactly the
// invalid-UTF-8 case and the hexadecimal field carries the bytes.
func jsonName(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, w := utf8.DecodeRuneInString(s[i:])
		b.WriteRune(r) // an invalid byte decodes as U+FFFD with width 1
		i += w
	}
	return b.String()
}

func vecNamesSpecs() []namesSpec {
	var out []namesSpec
	payload := []byte("sealed-name boundary vector payload")

	// Suites 1 and 2: one container per name.
	for _, suite := range []int{1, 2} {
		p := fmt.Sprintf("s%d-", suite)
		for _, v := range namesValidList() {
			out = append(out, namesSpec{id: p + "valid-" + v.id, desc: v.desc, suite: suite, expect: "opens", name: v.name, content: payload})
		}
		longest := strings.Repeat(`"<>|`, MaxNameBytes/4) // 3000 bytes, the most suite 1 holds
		if suite == 2 {
			longest = strings.Repeat(`"<>|`, MaxNameBytesSuite2/4) + `"<>` // 1023 bytes, the most suite 2 holds
		}
		out = append(out, namesSpec{id: p + "valid-longest", desc: fmt.Sprintf("the longest name this suite holds (%d bytes), made of the four characters", len(longest)), suite: suite, expect: "opens", name: longest, content: payload})
		for _, v := range namesRefusedList() {
			out = append(out, namesSpec{id: p + "refused-" + v.id, desc: v.desc, suite: suite, expect: "damaged", refused: "name", name: v.name, content: payload})
		}
	}

	// Suite 3: the root and the components are separate places.
	for _, c := range namesFour {
		out = append(out,
			namesSpec{id: "s3-valid-root-" + hex.EncodeToString([]byte(c)), desc: "U+" + fmt.Sprintf("%04X", []rune(c)[0]) + " inside the root folder's name", suite: 3, expect: "opens",
				name: "r" + c + "t", entries: []namesEntrySpec{{path: "f.txt", content: []byte("one")}}},
			namesSpec{id: "s3-valid-component-" + hex.EncodeToString([]byte(c)), desc: "U+" + fmt.Sprintf("%04X", []rune(c)[0]) + " inside a folder name and a file name below it", suite: 3, expect: "opens",
				name: "root", entries: []namesEntrySpec{{path: "d" + c + "q", dir: true}, {path: "d" + c + "q/f" + c + ".txt", content: []byte("two")}}})
	}
	out = append(out,
		namesSpec{id: "s3-valid-all-four", desc: "the four characters together in the root, a folder and a file", suite: 3, expect: "opens",
			name: `"<>|`, entries: []namesEntrySpec{{path: `"<>|`, dir: true}, {path: `"<>|/x|y.txt`, content: []byte("three")}}},
		namesSpec{id: "s3-valid-components", desc: "components that look like a refused name but are valid: the neighbours of the four, a dot and a space, a stem that only starts like a device, 255 and 256 units, the four at both edges", suite: 3, expect: "opens",
			name: "a b.c", entries: []namesEntrySpec{
				{path: `!#;={}.txt`, content: []byte("a")}, {path: `CONTRACT.pdf`, content: []byte("b")}, {path: `a b.c.txt`, content: []byte("c")},
				{path: `com10.log`, content: []byte("d")}, {path: `|first-and-last>`, content: []byte("e")},
				{path: strings.Repeat("n", 251) + ".txt", content: []byte("f")}, {path: strings.Repeat("n", 252) + ".txt", content: []byte("g")},
			}},
		namesSpec{id: "s3-valid-longest-root", desc: "the longest root name the format holds (3000 bytes), made of the four characters", suite: 3, expect: "opens",
			name: strings.Repeat(`"<>|`, MaxNameBytes/4), entries: []namesEntrySpec{{path: "f.txt", content: []byte("four")}}},
	)
	for _, v := range namesRefusedList() {
		out = append(out, namesSpec{id: "s3-refused-root-" + v.id, desc: "the root folder's name: " + v.desc, suite: 3, expect: "damaged", refused: "root", name: v.name,
			entries: []namesEntrySpec{{path: "f.txt", content: []byte("x")}}, forgeRoot: true})
	}
	for _, v := range namesRefusedList() {
		switch v.id {
		case "empty", "slash", "four-and-slash":
			continue // a path shape in suite 3: a slash splits into two valid components, an empty one is the case below
		}
		out = append(out, namesSpec{id: "s3-refused-component-" + v.id, desc: "a component of a manifest path: " + v.desc, suite: 3, expect: "damaged", refused: "component", name: "root",
			entries: []namesEntrySpec{{path: v.name, content: []byte("x")}}})
	}
	out = append(out,
		namesSpec{id: "s3-refused-component-empty", desc: "a manifest path with an empty component (two slashes in a row)", suite: 3, expect: "damaged", refused: "component", name: "root",
			entries: []namesEntrySpec{{path: "a", dir: true}, {path: "a//b", content: []byte("x")}}},
		namesSpec{id: "s3-refused-component-rooted", desc: "a manifest path that starts with a slash", suite: 3, expect: "damaged", refused: "component", name: "root",
			entries: []namesEntrySpec{{path: "/abs", content: []byte("x")}}},
	)
	return out
}

func (s namesSpec) fileName() string {
	switch s.suite {
	case 2:
		return s.id + ".bin"
	}
	return s.id + ".fd-sec"
}

// vecNamesTree builds the tree a suite-3 spec describes, with the fixed times
// of the other vectors.
func vecNamesTree(s namesSpec) *Tree {
	items := map[string][]byte{}
	var order []string
	for _, e := range s.entries {
		order = append(order, e.path)
		if e.dir {
			items[e.path] = nil
		} else {
			items[e.path] = e.content
			if items[e.path] == nil {
				items[e.path] = []byte{}
			}
		}
	}
	tr := memTree(s.name, items, order)
	tr.Meta.EncodedAt = vecTimes.EncodedAt
	tr.Meta.CreatedAt = vecTimes.CreatedAt
	tr.Meta.AccessedAt = vecTimes.AccessedAt
	tr.Meta.ModifiedAt = vecTimes.ModifiedAt
	return tr
}

// buildNamesVector writes the container a spec describes with the reference
// writer under a fixed random stream, forging what the checked writer will not
// write. Everything is deterministic: the same code writes the same bytes.
func buildNamesVector(t *testing.T, s namesSpec) []byte {
	t.Helper()
	cred := NewCredential(vecNamesCred)
	saved := randSource
	randSource = bytes.NewReader(vecStream(vecNamesSeed+s.id, 1<<17))
	defer func() { randSource = saved }()

	var out bytes.Buffer
	switch s.suite {
	case 1:
		meta := vecTimes
		meta.Size = int64(len(s.content))
		if s.expect == "opens" {
			meta.Name = s.name
			if _, err := Pack(&out, bytes.NewReader(s.content), meta, cred, fastParams()); err != nil {
				t.Fatalf("%s: Pack: %v", s.id, err)
			}
			return out.Bytes()
		}
		// The checked writer refuses a refused name, as it must: write a valid
		// one and re-seal the authenticated metadata with the refused name.
		meta.Name = "placeholder.txt"
		if _, err := Pack(&out, bytes.NewReader(s.content), meta, cred, fastParams()); err != nil {
			t.Fatalf("%s: Pack: %v", s.id, err)
		}
		return forgeSuite1Name(t, out.Bytes(), cred, s.name, nil)
	case 2:
		if s.expect == "opens" {
			meta := Metadata{Name: s.name, Size: int64(len(s.content)), EncodedAt: vec2EncodedAt}
			if _, err := PackSuite2(&out, bytes.NewReader(s.content), meta, cred); err != nil {
				t.Fatalf("%s: PackSuite2: %v", s.id, err)
			}
			return out.Bytes()
		}
		return forgeSuite2(t, validBody(t, s.name, s.content, s2TailMin), cred)
	case 3:
		if s.expect == "opens" {
			if _, err := PackTree(&out, vecNamesTree(s), cred, fastParams()); err != nil {
				t.Fatalf("%s: PackTree: %v", s.id, err)
			}
			return out.Bytes()
		}
		if s.forgeRoot {
			base := namesSpec{id: s.id, suite: 3, expect: "opens", name: "root", entries: s.entries}
			if _, err := PackTree(&out, vecNamesTree(base), cred, fastParams()); err != nil {
				t.Fatalf("%s: PackTree: %v", s.id, err)
			}
			return forgeSuite3Root(t, out.Bytes(), cred, s.name, nil)
		}
		// The manifest is written by packTree directly, past the writer's own
		// checks, with the entries in manifest order.
		var entries []TreeEntry
		files := map[string][]byte{}
		var total int64
		for _, e := range s.entries {
			te := TreeEntry{Path: e.path, Kind: KindFile, Size: int64(len(e.content)),
				CreatedAt: vecTimes.CreatedAt, AccessedAt: vecTimes.AccessedAt, ModifiedAt: vecTimes.ModifiedAt}
			if e.dir {
				te.Kind, te.Size = KindDir, 0
			} else {
				files[e.path] = e.content
			}
			total += te.Size
			entries = append(entries, te)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
		tr := vecNamesTree(namesSpec{name: s.name})
		tr.Open = func(p string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(files[p])), nil }
		if _, err := packTree(&out, tr, entries, total, cred, fastParams(), streamOpts{}); err != nil {
			t.Fatalf("%s: packTree: %v", s.id, err)
		}
		return out.Bytes()
	}
	t.Fatalf("%s: no such suite %d", s.id, s.suite)
	return nil
}

func vecNamesRecord(s namesSpec, b []byte) *vecNamesCase {
	c := &vecNamesCase{
		ID: s.id, Suite: s.suite, Expect: s.expect, Refused: s.refused, Description: s.desc,
		SealedName: jsonName(s.name), SealedHex: []byte(s.name), Credential: vecNamesCred,
		File: s.fileName(), TotalLen: int64(len(b)), FileIdent: hex.EncodeToString(mustBlake(b)),
	}
	switch s.suite {
	case 1, 2:
		c.Content = s.content
	}
	if s.suite != 2 {
		p := fastParams()
		c.Params = &p
	}
	for _, e := range s.entries {
		kind := "file"
		if e.dir {
			kind = "directory"
		}
		c.Entries = append(c.Entries, vecNamesEntry{Kind: kind, Path: jsonName(e.path), PathHex: []byte(e.path), Content: e.content})
	}
	return c
}

// replayNamesVector is the reader's side: the committed bytes go through the
// reader and the answer must be the one the case records. "damaged" means the
// authenticated container is refused as damage - not as a wrong credential,
// not as unsupported - and nothing was written.
func replayNamesVector(t *testing.T, c *vecNamesCase, b []byte) {
	t.Helper()
	cred := NewCredential(c.Credential)
	wantDamaged := func(err error) {
		t.Helper()
		if !errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) || errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: got %v, want damaged", c.ID, err)
		}
	}
	switch c.Suite {
	case 1, 2:
		var out bytes.Buffer
		m, err := Unpack(&out, bytes.NewReader(b), cred)
		if c.Expect == "damaged" {
			wantDamaged(err)
			if out.Len() != 0 {
				t.Errorf("%s: %d bytes were written for a damaged name", c.ID, out.Len())
			}
			return
		}
		if err != nil {
			t.Errorf("%s: a valid name was refused: %v", c.ID, err)
			return
		}
		if m.Name != c.SealedName || !bytes.Equal(out.Bytes(), c.Content) {
			t.Errorf("%s: came back as a %d-byte name and %d bytes", c.ID, len(m.Name), out.Len())
		}
	case 3:
		sink, tm, entries, err := unpackTreeBytes(b, cred)
		if c.Expect == "damaged" {
			wantDamaged(err)
			if sink != nil && len(sink.order) != 0 {
				t.Errorf("%s: the sink was handed %d entries before a damaged name was found", c.ID, len(sink.order))
			}
			return
		}
		if err != nil {
			t.Errorf("%s: a valid name was refused: %v", c.ID, err)
			return
		}
		if tm.Name != c.SealedName || len(entries) != len(c.Entries) {
			t.Errorf("%s: root %d bytes, %d entries; want %d entries", c.ID, len(tm.Name), len(entries), len(c.Entries))
		}
		for _, e := range c.Entries {
			if e.Kind == "file" {
				if got := sink.files[e.Path]; got == nil || !bytes.Equal(got.Bytes(), e.Content) {
					t.Errorf("%s: entry %q did not round-trip", c.ID, e.Path)
				}
			}
		}
	}
}

func TestVectors_Names(t *testing.T) {
	savedA, savedS := activeProfile, suite2Profiles
	activeProfile, suite2Profiles = profileV1, suite2ProfilesV1
	defer func() { activeProfile, suite2Profiles = savedA, savedS }()

	specs := vecNamesSpecs()
	writing := os.Getenv("FDSEC_WRITE_VECTORS") == "1"

	// What the code writes today. Suite 2 opens only under the real Argon2id
	// try-list, so its refused-name cases are replayed from the committed bytes
	// and rebuilt only when the vectors are being written.
	built := map[string][]byte{}
	for _, s := range specs {
		if !writing && s.suite == 2 && s.expect == "damaged" {
			continue
		}
		built[s.id] = buildNamesVector(t, s)
	}

	if writing {
		if err := os.MkdirAll(vecNamesDir, 0o755); err != nil {
			t.Fatal(err)
		}
		vf := &vecNamesFile{
			Format:    "FDSEC sealed-name boundary vectors - see FDSEC-FORMAT.md sections 20 to 22 (document version 1.5) and FDSEC-BEHAVIOUR.md section 18",
			Generated: "2026-10-21",
			Contract:  "FDSEC-FORMAT 1.5 sections 20 to 22",
			HowToReplay: []string{
				"Every container authenticates under the credential in its case. For each case open it with that credential and read it to the end.",
				"expect \"opens\": the reader must accept the sealed name (suites 1, 2) or the root name and every component (suite 3), return it byte for byte, and return the payload.",
				"expect \"damaged\": the reader must answer damaged - not wrong-credential-or-tamper, not unsupported - and write nothing. The container is authenticated, so the credential is right; its sealed name breaks a rule of FDSEC-FORMAT section 7.",
				"The four characters U+0022 U+003C U+003E U+007C are valid in every case that carries them. Whether a destination can create such a name is a restore matter (FDSEC-BEHAVIOUR section 18) and not part of any case here.",
				"Every \"refused\" case is a rule of section 7 that section 20 leaves unchanged (or, for invalid-utf8, section 22 states). The four-and-* cases show that a valid character beside a refused element does not make the name valid.",
				"sealed_name_hex (and path_hex) are the authoritative bytes. Where the sealed name is not valid UTF-8 (the invalid-utf8 cases) sealed_name and path show each invalid byte as U+FFFD, because JSON text cannot carry it: compare the hexadecimal field, never the text.",
			},
			KDF:    map[string]uint32{"m_kib": profileV1.MemoryKiB, "t": profileV1.Time, "p": uint32(profileV1.Lanes), "threshold": profileV1.Threshold},
			Suite2: suite2ProfilesV1,
			NotCovered: []string{
				"a sealed name that is not in NFC: FDSEC-FORMAT section 22 item 3 leaves it undecided - suites 2 and 3 of the reference reader refuse it, suite 1 reads it as written - so no case pins it",
				"the destination's own limits (the four characters, 255 UTF-16 units): FDSEC-BEHAVIOUR section 18, a restore matter",
			},
		}
		files := map[string][]byte{}
		for _, s := range specs {
			vf.Cases = append(vf.Cases, vecNamesRecord(s, built[s.id]))
			files[s.fileName()] = built[s.id]
		}
		var j bytes.Buffer
		enc := json.NewEncoder(&j)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(vf); err != nil {
			t.Fatal(err)
		}
		files[vecJSON] = j.Bytes()
		prov := "# Sealed-name boundary vectors of FDSEC-FORMAT 1.5 sections 20 to 22, written by the reference writer in the FileDO repository (fdsec/vectors_names_test.go).\n" +
			"# Generated, never hand-edited: FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Names. The vectors of suites 1, 2 and 3 beside this folder are untouched.\n" +
			"# Asserted by TestVectors_Names. The shared contracts catalog holds a byte-identical copy (secure-container/vectors/names/); the second reader (FastMediaSorter Lite) has not replayed them.\n" +
			"contract: FDSEC-FORMAT\nversion: 1.5\ngenerated: 2026-10-21\n"
		for _, n := range sortedKeys(files) {
			if err := os.WriteFile(filepath.Join(vecNamesDir, n), files[n], 0o644); err != nil {
				t.Fatal(err)
			}
			prov += fmt.Sprintf("sha256 %s %x\n", n, sha256.Sum256(files[n]))
		}
		if err := os.WriteFile(filepath.Join(vecNamesDir, "PROVENANCE.txt"), []byte(prov), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("sealed-name boundary vectors rewritten to %s (%d cases)", vecNamesDir, len(specs))
	}

	// Every committed file is the one PROVENANCE names, and no other file is there.
	provRaw, err := os.ReadFile(filepath.Join(vecNamesDir, "PROVENANCE.txt"))
	if err != nil {
		t.Fatalf("committed names PROVENANCE.txt missing (run FDSEC_WRITE_VECTORS=1 go test ./fdsec -run TestVectors_Names): %v", err)
	}
	named := map[string]bool{}
	for _, line := range strings.Split(string(provRaw), "\n") {
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) == 3 && parts[0] == "sha256" {
			named[parts[1]] = true
			if got := fmt.Sprintf("%x", sha256.Sum256(mustRead(t, filepath.Join(vecNamesDir, parts[1])))); got != parts[2] {
				t.Fatalf("PROVENANCE sha256 mismatch for %s", parts[1])
			}
		}
	}
	des, err := os.ReadDir(vecNamesDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if de.Name() != "PROVENANCE.txt" && !named[de.Name()] {
			t.Errorf("%s is in the folder and PROVENANCE does not name it", de.Name())
		}
	}
	if len(named) != len(specs)+1 {
		t.Fatalf("PROVENANCE names %d files, want %d", len(named), len(specs)+1)
	}

	if catDir := os.Getenv("FDSEC_CATALOG_VECTORS"); catDir != "" {
		cat := filepath.Join(catDir, "names")
		if _, err := os.Stat(cat); err != nil {
			t.Logf("the catalog at %s holds no names/ folder yet; nothing to compare (the boundary vectors are owed there)", catDir)
		} else {
			for n := range named {
				if !bytes.Equal(mustRead(t, filepath.Join(vecNamesDir, n)), mustRead(t, filepath.Join(cat, n))) {
					t.Fatalf("catalog names vector file %s does not match the local fixture", n)
				}
			}
			mustRead(t, filepath.Join(cat, "PROVENANCE.txt"))
		}
	}

	var committed vecNamesFile
	if err := json.Unmarshal(mustRead(t, filepath.Join(vecNamesDir, vecJSON)), &committed); err != nil {
		t.Fatal(err)
	}
	if len(committed.Cases) != len(specs) {
		t.Fatalf("committed vectors.json holds %d cases, the generator %d", len(committed.Cases), len(specs))
	}
	counts := map[string]int{}
	for i, s := range specs {
		got := committed.Cases[i]
		if got.ID != s.id || got.Suite != s.suite || got.Expect != s.expect || got.SealedName != jsonName(s.name) || got.File != s.fileName() || !bytes.Equal(got.SealedHex, []byte(s.name)) {
			t.Fatalf("case %d (%s): vectors.json drifted from the generator", i, s.id)
		}
		b := mustRead(t, filepath.Join(vecNamesDir, got.File))
		if got.FileIdent != hex.EncodeToString(mustBlake(b)) || got.TotalLen != int64(len(b)) {
			t.Fatalf("%s: vectors.json does not describe its file", s.id)
		}
		if fresh, ok := built[s.id]; ok && !bytes.Equal(fresh, b) {
			t.Fatalf("%s: the container the code writes today is not the committed one", s.id)
		}
		replayNamesVector(t, got, b)
		counts[fmt.Sprintf("suite %d %s", s.suite, s.expect)]++
	}
	t.Logf("replayed %d committed cases: %v", len(committed.Cases), counts)
}
