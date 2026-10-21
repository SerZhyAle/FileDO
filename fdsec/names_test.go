package fdsec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

// Sealed names after FDSEC-FORMAT 1.4 section 20 (the amendment of 2026-10-19):
// the four characters U+0022 U+003C U+003E U+007C are valid in an authenticated
// name in every suite, and every other refusal of section 7 stands. The tests
// pin both halves - the positive case and the negative one - for suite 1, the
// suite-2 name block and the suite-3 root and components, and then the seam
// that lets a caller refuse a name its destination cannot create without the
// reader calling the container damaged (WithNameCheck).

// namesFour are the four characters, one per entry.
var namesFour = []string{`"`, `<`, `>`, `|`}

// namesValid are names the format accepts: the four characters in every
// position, and the characters that sit next to them in the code table, which
// must stay valid too.
var namesValid = []string{
	`a"b.txt`, `a<b.txt`, `a>b.txt`, `a|b.txt`, // one of each, inside
	`"leading.txt`, `<leading.txt`, `>leading.txt`, `|leading.txt`, // each in first position
	`inner"<>|all.txt`, `"<>|`, // all four together
	`!#;={}.txt`,              // the neighbours U+0021 U+0023 U+003B U+003D U+007B U+007D
	`a.b.c`, `a b`, `.hidden`, // a dot and a space inside
	`CONTRACT.pdf`, `com10.log`, `nullify.txt`, `lpt.md`, // look like a reserved stem but are not
}

// namesRefused are names section 7 still refuses; each is damage in a reader.
var namesRefused = []string{
	"", ".", "..", `a/b`, `a\b`, `a:b`, `a*b`, `a?b`,
	"a\x01b", "a\x1fb", "a\x7fb", "a\x00b",
	"CON", "con", "NUL", "nul.txt", "COM1", "COM9.mp4", "LPT1", "lpt9.x", "AUX", "PRN",
	"trailing.", "trailing ", "trailing. ", "trailing  ",
	// Bytes that are not UTF-8 (FDSEC-FORMAT 1.5 section 22): a lone 0xFF, a
	// truncated two-byte sequence and an encoded surrogate half. The reader
	// refuses them; it must not turn them into U+FFFD and create a file.
	"a\xffb", "\xff", "abc\xc3", "\xed\xa0\x80",
}

func TestValidateName_FourCharactersAreValid(t *testing.T) {
	for _, c := range namesFour {
		if err := ValidateName(c); err != nil {
			t.Errorf("U+%04X alone: %v", []rune(c)[0], err)
		}
	}
	for _, n := range namesValid {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) refused a name section 20 keeps valid: %v", n, err)
		}
	}
	// The longest name the format holds, made of the four characters.
	longest := strings.Repeat(`"<>|`, MaxNameBytes/4)
	if len(longest) != MaxNameBytes {
		t.Fatalf("test name is %d bytes", len(longest))
	}
	if err := ValidateName(longest); err != nil {
		t.Errorf("a %d-byte name of the four characters: %v", MaxNameBytes, err)
	}
}

func TestValidateName_EverythingElseStillRefused(t *testing.T) {
	for _, n := range namesRefused {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q) accepted a name section 7 refuses", n)
		}
	}
	if err := ValidateName(strings.Repeat("n", MaxNameBytes+1)); err == nil {
		t.Error("a name of more than MaxNameBytes bytes was accepted")
	}
	// A name that carries one of the four AND a refused element is refused for
	// the refused element: valid characters do not launder anything.
	for _, n := range []string{`a|b/c`, `a"b\c`, `<>:x`, `a|*`, "x|\x01", "x>.", `a<b?`, `|\`} {
		if err := ValidateName(n); err == nil {
			t.Errorf("ValidateName(%q): a refused element beside a valid character was accepted", n)
		}
	}
	// A refusal never echoes the name (a hostile container chooses it).
	const hostile = `distinctive-NAME/with|slash`
	if err := ValidateName(hostile); err == nil || strings.Contains(err.Error(), "distinctive") {
		t.Errorf("ValidateName echoes the name or accepts it: %v", err)
	}
}

// A refusal of any suite never echoes the sealed name: the error reaches
// history.json and the event stream, and a hostile container chooses the name.
func TestNames_RefusalsDoNotEchoTheName(t *testing.T) {
	cred := NewCredential("pw123")
	const hostile = `distinctive-NAME\with|slash`
	raw, _, _, _ := tampered(t)
	if _, _, err := unpackContainer(forgeSuite1Name(t, raw, cred, hostile, nil), cred); err == nil || strings.Contains(err.Error(), "distinctive") {
		t.Errorf("suite 1: %v", err)
	}
	if _, _, err := unpackSuite2Bytes(t, forgeSuite2(t, validBody(t, hostile, []byte("a"), s2TailMin), cred), cred); err == nil || strings.Contains(err.Error(), "distinctive") {
		t.Errorf("suite 2: %v", err)
	}
	b := hostilePack(t, []TreeEntry{{Path: hostile, Kind: KindFile, Size: 1}}, map[string][]byte{hostile: []byte("a")}, cred)
	if _, _, _, err := unpackTreeBytes(b, cred); err == nil || strings.Contains(err.Error(), "distinctive") {
		t.Errorf("suite 3 component: %v", err)
	}
	good := packTreeBytes(t, memTree("root", map[string][]byte{"f": []byte("x")}, []string{"f"}), cred, fastParams())
	if _, _, _, err := unpackTreeBytes(forgeSuite3Root(t, good, cred, hostile, nil), cred); err == nil || strings.Contains(err.Error(), "distinctive") {
		t.Errorf("suite 3 root: %v", err)
	}
}

// Suite 1: the checked writer packs the four characters and the reader returns
// the name and the bytes untouched.
func TestNames_Suite1_FourCharactersRoundTrip(t *testing.T) {
	cred := NewCredential("pw123")
	content := []byte("the payload does not depend on the name")
	for _, name := range append(append([]string{}, namesValid...), namesFour...) {
		meta := fixedTimes
		meta.Name = name
		b := packBytes(t, content, meta, cred, fastParams())
		got, m, err := unpackContainer(b, cred)
		if err != nil {
			t.Errorf("suite 1, %q: %v", name, err)
			continue
		}
		if m.Name != name || !bytes.Equal(got, content) {
			t.Errorf("suite 1, %q: came back as %q with %d bytes", name, m.Name, len(got))
		}
	}
}

// Suite 1: a refused name is damage in the reader - never the credential class,
// never silently accepted - and the checked writer will not produce one.
func TestNames_Suite1_RefusedNamesAreDamage(t *testing.T) {
	raw, _, _, _ := tampered(t)
	cred := NewCredential("pw123")
	for _, bad := range namesRefused {
		b := forgeSuite1Name(t, raw, cred, bad, nil)
		_, _, err := unpackContainer(b, cred)
		if !errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) {
			t.Errorf("suite 1, forged name %q: got %v, want damaged", bad, err)
			continue
		}
		var out bytes.Buffer
		if _, err := Pack(&out, bytes.NewReader([]byte("x")), Metadata{Name: bad, Size: 1}, cred, fastParams()); err == nil {
			t.Errorf("suite 1: the writer packed the refused name %q", bad)
		}
	}
	// And the same container with each of the four characters forged in opens:
	// the forging path itself proves nothing about the format, only the reader's
	// answer to an authenticated name does.
	for _, ok := range append(append([]string{}, namesValid...), namesFour...) {
		b := forgeSuite1Name(t, raw, cred, ok, nil)
		if _, m, err := unpackContainer(b, cred); err != nil || m.Name != ok {
			t.Errorf("suite 1, forged valid name %q: %q, %v", ok, m.Name, err)
		}
	}
}

// Suite 2: the name block holds the four characters, and a refused name in an
// authenticated frame 0 is damage.
func TestNames_Suite2_FourCharactersAndRefusals(t *testing.T) {
	cred := NewCredential("pw123")
	for _, name := range append(append([]string{}, namesValid...), namesFour...) {
		meta := s2Meta
		meta.Name = name
		b := packSuite2Bytes(t, []byte("quiet"), meta, cred)
		got, m, err := unpackSuite2Bytes(t, b, cred)
		if err != nil || m.Name != name || string(got) != "quiet" {
			t.Errorf("suite 2, %q: %q, %q, %v", name, m.Name, got, err)
		}
	}
	for _, bad := range namesRefused {
		if strings.ContainsRune(bad, '\r') {
			continue // a CR ends the name in the block; not a name character here
		}
		b := forgeSuite2(t, validBody(t, bad, []byte("abc"), s2TailMin), cred)
		_, _, err := unpackSuite2Bytes(t, b, cred)
		if !errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) {
			t.Errorf("suite 2, forged name %q: got %v, want damaged", bad, err)
		}
	}
	// 1023 bytes of the four characters is the longest the block can hold.
	long := strings.Repeat(`"<>|`, MaxNameBytesSuite2/4) + `"<>`
	if len(long) != MaxNameBytesSuite2 {
		t.Fatalf("test name is %d bytes", len(long))
	}
	meta := s2Meta
	meta.Name = long
	b := packSuite2Bytes(t, []byte("x"), meta, cred)
	if _, m, err := unpackSuite2Bytes(t, b, cred); err != nil || m.Name != long {
		t.Errorf("suite 2, longest name of the four characters: %v", err)
	}
}

// Suite 3: the four characters in the root name and in a component, at the
// top and nested; a refused component or root is damage, found before the sink
// sees anything.
func TestNames_Suite3_FourCharactersAndRefusals(t *testing.T) {
	cred := NewCredential("pw123")
	items := map[string][]byte{
		`d"q`:           nil,
		`d"q/f<lt.txt`:  []byte("one"),
		`g>t|p`:         nil,
		`g>t|p/h|.bin`:  []byte("two"),
		`plain.txt`:     []byte("three"),
		`"<>|`:          []byte("four"),
		`inner`:         nil,
		`inner/a>b.dat`: []byte("five"),
	}
	order := []string{`d"q`, `d"q/f<lt.txt`, `g>t|p`, `g>t|p/h|.bin`, `plain.txt`, `"<>|`, `inner`, `inner/a>b.dat`}
	for _, root := range append(append([]string{}, namesValid...), namesFour...) {
		tr := memTree(root, items, order)
		b := packTreeBytes(t, tr, cred, fastParams())
		sink, tm, entries, err := unpackTreeBytes(b, cred)
		if err != nil {
			t.Errorf("suite 3, root %q: %v", root, err)
			continue
		}
		if tm.Name != root || len(entries) != len(order) {
			t.Errorf("suite 3, root %q: came back as %q with %d entries", root, tm.Name, len(entries))
		}
		for p, want := range items {
			if want == nil {
				continue
			}
			if got := sink.files[p]; got == nil || !bytes.Equal(got.Bytes(), want) {
				t.Errorf("suite 3, root %q: entry %q did not round-trip", root, p)
			}
		}
	}

	// Refused components and roots: hostilePack bypasses the writer's checks.
	file := func(p string) TreeEntry { return TreeEntry{Path: p, Kind: KindFile, Size: 1} }
	for _, bad := range namesRefused {
		if bad == "" || strings.ContainsAny(bad, "/") {
			continue // an empty or slash-bearing component is a path shape; hostile-manifest tests own it
		}
		entries := []TreeEntry{file(bad)}
		b := hostilePack(t, entries, map[string][]byte{bad: []byte("x")}, cred)
		_, _, _, err := unpackTreeBytes(b, cred)
		if !errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) {
			t.Errorf("suite 3, forged component %q: got %v, want damaged", bad, err)
		}
		// A refused root, forged into an otherwise valid container.
		good := packTreeBytes(t, memTree("root", map[string][]byte{"f": []byte("x")}, []string{"f"}), cred, fastParams())
		forged := forgeSuite3Root(t, good, cred, bad, nil)
		if _, _, _, err := unpackTreeBytes(forged, cred); !errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) {
			t.Errorf("suite 3, forged root %q: got %v, want damaged", bad, err)
		}
	}
	// A valid root forged into the same container opens (the forging path is not
	// what is under test, the reader's answer is).
	good := packTreeBytes(t, memTree("root", map[string][]byte{"f": []byte("x")}, []string{"f"}), cred, fastParams())
	for _, ok := range namesFour {
		forged := forgeSuite3Root(t, good, cred, "r"+ok+"t", nil)
		if _, tm, _, err := unpackTreeBytes(forged, cred); err != nil || tm.Name != "r"+ok+"t" {
			t.Errorf("suite 3, forged valid root with %q: %q, %v", ok, tm.Name, err)
		}
	}
}

// errNotHere is what a caller's check returns in these tests: a stand-in for
// the CLI's destination-name error. It must reach the caller unchanged.
var errNotHere = errors.New("this destination cannot create that name")

// WithNameCheck: the reader calls it with the authenticated name before any
// output, an error from it ends the unpack unchanged and not as damage, and no
// check changes nothing.
func TestWithNameCheck_Suites1And2(t *testing.T) {
	cred := NewCredential("pw123")
	content := bytes.Repeat([]byte("0123456789abcdef"), 200)
	suite1 := func(name string) []byte {
		meta := fixedTimes
		meta.Name = name
		return packBytes(t, content, meta, cred, fastParams())
	}
	suite2 := func(name string) []byte {
		meta := s2Meta
		meta.Name = name
		return packSuite2Bytes(t, content, meta, cred)
	}
	for suite, pack := range map[string]func(string) []byte{"suite 1": suite1, "suite 2": suite2} {
		for _, name := range []string{`a|b.txt`, `a"b`, `x<y>z`, "fine.txt"} {
			b := pack(name)
			c, err := Open(bytes.NewReader(b), cred)
			if err != nil {
				t.Fatal(err)
			}
			var seen []string
			w := &countingWriter{}
			refuse := strings.ContainsAny(name, `"<>|`)
			_, err = c.Unpack(w, WithNameCheck(func(n string) error {
				seen = append(seen, n)
				if refuse {
					return errNotHere
				}
				return nil
			}))
			if len(seen) != 1 || seen[0] != name {
				t.Errorf("%s, %q: the check saw %q", suite, name, seen)
			}
			if refuse {
				if !errors.Is(err, errNotHere) || errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) || errors.Is(err, ErrUnsupported) {
					t.Errorf("%s, %q: got %v, want the caller's own error and no container class", suite, name, err)
				}
				if w.n != 0 {
					t.Errorf("%s, %q: %d bytes of output were written before the refusal", suite, name, w.n)
				}
			} else if err != nil || w.n != int64(len(content)) {
				t.Errorf("%s, %q: a check that passes broke the unpack: %v, %d bytes", suite, name, err, w.n)
			}
		}
	}

	// The check never sees a name the format refuses: that is damage first.
	raw, _, _, _ := tampered(t)
	forged := forgeSuite1Name(t, raw, cred, `a/b`, nil)
	called := false
	_, err := Unpack(io.Discard, bytes.NewReader(forged), cred, WithNameCheck(func(string) error { called = true; return errNotHere }))
	if !errors.Is(err, ErrDamaged) || called {
		t.Errorf("a name the format refuses: got %v, check called %v; want damaged and no call", err, called)
	}
	// No check, no change: the default reads every valid name.
	if _, m, err := unpackContainer(suite1(`a|b`), cred); err != nil || m.Name != `a|b` {
		t.Errorf("without a check: %v", err)
	}
}

// refusingSink counts what the unpack hands it.
type refusingSink struct{ dirs, files int }

func (s *refusingSink) Dir(TreeEntry) error { s.dirs++; return nil }
func (s *refusingSink) File(TreeEntry) (io.WriteCloser, error) {
	s.files++
	return nopWriteCloser{io.Discard}, nil
}

func TestWithNameCheck_Suite3(t *testing.T) {
	cred := NewCredential("pw123")
	items := map[string][]byte{"ok": nil, "ok/fine.txt": []byte("a"), "ok/bad|name.txt": []byte("b"), "z.txt": []byte("c")}
	order := []string{"ok", "ok/fine.txt", "ok/bad|name.txt", "z.txt"}
	b := packTreeBytes(t, memTree(`r"oot`, items, order), cred, fastParams())
	c, err := Open(bytes.NewReader(b), cred)
	if err != nil {
		t.Fatal(err)
	}

	var seen []string
	sink := &refusingSink{}
	_, _, err = c.UnpackTree(sink, WithNameCheck(func(n string) error {
		seen = append(seen, n)
		if strings.Contains(n, "|") {
			return errNotHere
		}
		return nil
	}))
	if !errors.Is(err, errNotHere) || errors.Is(err, ErrDamaged) {
		t.Errorf("got %v, want the caller's own error", err)
	}
	if sink.dirs != 0 || sink.files != 0 {
		t.Errorf("the sink saw %d directories and %d files before the refusal", sink.dirs, sink.files)
	}
	// Components, not whole paths, and not the root: the caller reads the root
	// with TreeMetadata and knows whether it will use it.
	for _, n := range seen {
		if strings.Contains(n, "/") || n == `r"oot` {
			t.Errorf("the check was handed %q, want a bare entry component", n)
		}
	}
	if len(seen) == 0 {
		t.Error("the check was never called")
	}

	// A check that passes leaves the unpack whole.
	sink = &refusingSink{}
	if _, es, err := c.UnpackTree(sink, WithNameCheck(func(string) error { return nil })); err != nil || len(es) != len(order) || sink.files != 3 {
		t.Errorf("a passing check: %v, %d entries, %d files", err, len(es), sink.files)
	}
	// Verify passes no check, so a container whose names this destination
	// cannot create still verifies.
	if tm, _, err := c.VerifyTree(); err != nil || tm.Name != `r"oot` {
		t.Errorf("VerifyTree: %q, %v", tm.Name, err)
	}
	// The read-back a pack runs and TreeMetadata (the root name) are unaffected.
	if tm, err := c.TreeMetadata(); err != nil || tm.Name != `r"oot` {
		t.Errorf("TreeMetadata: %q, %v", tm.Name, err)
	}
}

// forgeSuite1Name re-seals a suite-1 container's metadata block with an
// arbitrary, unchecked name and keeps every other field. pad fills the block's
// tail; nil draws it from the random source. The name is not validated: this
// is how a crafted container is made.
func forgeSuite1Name(t *testing.T, raw []byte, cred Credential, name string, pad []byte) []byte {
	t.Helper()
	return rewriteMetadata(t, raw, cred, func(origMeta Metadata, origDigest [32]byte) []byte {
		block := make([]byte, metaPlain)
		binary.LittleEndian.PutUint16(block[0:2], uint16(len(name)))
		copy(block[2:], name)
		o := 2 + len(name)
		binary.LittleEndian.PutUint64(block[o:], uint64(origMeta.Size))
		binary.LittleEndian.PutUint64(block[o+8:], toFILETIME(origMeta.EncodedAt))
		binary.LittleEndian.PutUint64(block[o+16:], toFILETIME(origMeta.CreatedAt))
		binary.LittleEndian.PutUint64(block[o+24:], toFILETIME(origMeta.AccessedAt))
		binary.LittleEndian.PutUint64(block[o+32:], toFILETIME(origMeta.ModifiedAt))
		copy(block[o+40:], origDigest[:])
		off := o + 40 + digestSize
		if off > metaPlain {
			t.Fatalf("name of %d bytes overflows the metadata block", len(name))
		}
		if pad == nil {
			var err error
			if pad, err = randBytes(metaPlain - off); err != nil {
				t.Fatal(err)
			}
		}
		copy(block[off:], pad)
		return block
	})
}

// forgeSuite3Root re-seals a suite-3 container's directory block with an
// arbitrary, unchecked root name, keeping the counts, the times and the
// manifest digest. pad fills the block's tail; nil draws it from the random
// source.
func forgeSuite3Root(t *testing.T, raw []byte, cred Credential, name string, pad []byte) []byte {
	t.Helper()
	orig, fileKey, err := openHead(bytes.NewReader(raw), cred)
	if err != nil {
		t.Fatal(err)
	}
	a, err := newXAEAD(fileKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce, err := deriveNonce(fileKey, dirCtx())
	if err != nil {
		t.Fatal(err)
	}
	plain, fail := a.Open(nil, nonce, raw[preMeta:preMeta+metaSize], adDir(orig.HeaderDigest))
	if fail != nil {
		t.Fatal(fail)
	}
	oldLen := int(binary.LittleEndian.Uint16(plain[0:2]))
	rest := plain[2+oldLen : 2+oldLen+dirFixed-2] // E, M, T, four times, manifest digest
	block := make([]byte, metaPlain)
	binary.LittleEndian.PutUint16(block[0:2], uint16(len(name)))
	copy(block[2:], name)
	o := 2 + len(name)
	copy(block[o:], rest)
	off := o + len(rest)
	if off > metaPlain {
		t.Fatalf("root name of %d bytes overflows the directory block", len(name))
	}
	if pad == nil {
		if pad, err = randBytes(metaPlain - off); err != nil {
			t.Fatal(err)
		}
	}
	copy(block[off:], pad)
	b := bytes.Clone(raw)
	copy(b[preMeta:preMeta+metaSize], a.Seal(nil, nonce, block, adDir(orig.HeaderDigest)))
	return b
}
