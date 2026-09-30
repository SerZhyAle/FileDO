package vdisk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"

	"filedo/fdsec"
)

// SP-0004 P5: the credential path. Every test here derives under a lowered
// work factor (lowKDF); the real row is exercised once, by TestVD_KDF_Row1.

// lowKDF lowers kdf_params_id 1 to a work factor that costs milliseconds.
func lowKDF(t testing.TB) {
	old := kdfParams
	kdfParams = map[uint32]fdsec.KDFParams{1: {MemoryKiB: 1024, Time: 1, Lanes: 1}}
	t.Cleanup(func() { kdfParams = old })
}

func newVault(t testing.TB, cred string) (*Container, *memBacking) {
	t.Helper()
	return newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential(cred)})
}

func openMemCred(b backing, cred string, mode OpenMode) (*Container, error) {
	return openOn(context.Background(), b, fdsec.NewCredential(cred), mode)
}

// T5.4: the credential key is FDD-FORMAT 8.2's construction, run through the
// shared fdsec.DeriveKey under FDD's pepper - checked against an independent
// spelling of the formula.
func TestVD_KDF(t *testing.T) {
	lowKDF(t)
	salt := bytes.Repeat([]byte{7}, 16)
	for _, cred := range []string{"", "test", "Test", strings.Repeat("x", 200), "café"} {
		got, err := credKey(fdsec.NewCredential(cred), salt, 1)
		if err != nil {
			t.Fatal(err)
		}
		h, _ := blake2b.New512(pepper[:])
		h.Write(fdsec.NewCredential(cred))
		want := argon2.IDKey(h.Sum(nil), salt, 1, 1024, 1, 32)
		if !bytes.Equal(got, want) {
			t.Fatalf("cred_key(%q) differs from the independent construction", cred)
		}
	}
	// NFC: the decomposed and the composed spelling are one credential.
	a, _ := credKey(fdsec.NewCredential("café"), salt, 1)
	b, _ := credKey(fdsec.NewCredential("café"), salt, 1)
	if !bytes.Equal(a, b) {
		t.Fatal("NFC normalisation did not unify two spellings of one credential")
	}
	if _, err := credKey(fdsec.NewCredential("x"), salt, 99); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("an unknown kdf_params_id: %v, want unsupported", err)
	}
}

// Row 1 as shipped: the work factor suite 2 pinned (SP-0019 D3), compared
// with suite 2's own table so the two cannot drift apart (FDD-FORMAT 8.2).
func TestVD_KDF_Row1(t *testing.T) {
	p := kdfParamsTable[1]
	if p != (fdsec.KDFParams{MemoryKiB: 262144, Time: 3, Lanes: 4}) {
		t.Fatalf("kdf_params_id 1 is %+v; a row in use is never edited", p)
	}
	if got := fdsec.Suite2Profiles()[0]; got != p {
		t.Fatalf("row 1 %+v is not suite 2's entry 0 %+v, which FDD-FORMAT 8.2 says it is", p, got)
	}
}

// T5.5: kinds 2 and 3 round-trip, a wrong key fails the tag as a wrong
// credential, and a slot that never opens is random bytes.
func TestVD_Slots_Credential(t *testing.T) {
	dataKey := bytes.Repeat([]byte{0x5a}, dataKeySize)
	k := bytes.Repeat([]byte{1}, 32)
	other := bytes.Repeat([]byte{2}, 32)
	for _, kind := range []byte{slotKindPassphrase, slotKindKeyfile} {
		r := newDetRand(fmt.Sprintf("slots %d", kind))
		region, err := drawSlotsKind(r, k, kind, dataKey)
		if err != nil {
			t.Fatal(err)
		}
		got, opened, err := openSlotsAt(region, k, false)
		if err != nil || !bytes.Equal(got, dataKey) || len(opened) != 1 || opened[0] != 0 {
			t.Fatalf("kind %d: key %x opened %v err %v", kind, got, opened, err)
		}
		if _, err := openSlots(region, other, false); !errors.Is(err, ErrCredential) {
			t.Fatalf("kind %d under another key: %v, want wrong credential", kind, err)
		}
		// Under obf_key a credential slot is a slot of the wrong kind.
		if _, err := openSlots(region, k, true); !errors.Is(err, ErrDamaged) {
			t.Fatalf("kind %d read as obfuscated: %v, want damaged", kind, err)
		}
		// Slots 1-7 are the draws themselves: nothing marks them unused.
		if !bytes.Equal(region[slotSize:], r.draws[len(r.draws)-1]) {
			t.Fatal("an unused slot is not the CSPRNG output it was drawn as")
		}
		// A changed byte in the used slot reads as a wrong credential: the
		// tag cannot tell a wrong key from a changed byte (FDD-FORMAT 8.3).
		bad := append([]byte(nil), region...)
		bad[100] ^= 1
		if _, err := openSlots(bad, k, false); !errors.Is(err, ErrCredential) {
			t.Fatalf("a tampered slot: %v, want wrong credential", err)
		}
	}
	// Two slots under two credentials that hold different data keys: damaged.
	r := newDetRand("two keys")
	a, _ := drawSlot(r, k, slotKindPassphrase, dataKey)
	b, _ := drawSlot(r, k, slotKindPassphrase, bytes.Repeat([]byte{0x33}, dataKeySize))
	region := append(append(append([]byte(nil), a...), b...), make([]byte, 6*slotSize)...)
	if _, err := openSlots(region, k, false); !errors.Is(err, ErrDamaged) {
		t.Fatalf("two data keys: %v, want damaged", err)
	}
}

// T5.7: a vault opens only with its credential; the header still answers
// without one.
func TestVD_Vault(t *testing.T) {
	lowKDF(t)
	deterministic(t, "vault")
	c, mb := newVault(t, "test")
	data := pattern(3, 300000)
	if _, err := c.WriteAt(data, 70000); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	img := mb.snapshot()
	if bytes.Contains(img, data[:4096]) {
		t.Fatal("plaintext is in the file")
	}
	res, err := resolveHeaders(&memBacking{data: img}, int64(len(img)))
	if err != nil {
		t.Fatal(err)
	}
	info := res.info(int64(len(img)))
	if info.Profile != ProfileVault || info.Obfuscated || !info.Clean || info.KDFID != kdfArgon2id || info.KDFParamsID != 1 || info.Protection() != "encrypted" {
		t.Fatalf("the header without a credential: %+v", info)
	}
	rc, err := openMemCred(&memBacking{data: img}, "test", OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(data))
	if _, err := rc.ReadAt(got, 70000); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("read back: %v", err)
	}
	rc.Close()
	for _, wrong := range []string{"Test", "", "test ", " test"} {
		if _, err := openMemCred(&memBacking{data: img}, wrong, OpenRead); ExitClass(err) != ExitCredential {
			t.Fatalf("credential %q: class %d (%v), want 3", wrong, ExitClass(err), err)
		}
	}
	// Written again under its credential, it stays a vault.
	wc, err := openMemCred(&memBacking{data: img}, "test", OpenWrite)
	if err != nil {
		t.Fatal(err)
	}
	if wc.Info().Profile != ProfileVault || wc.Info().Obfuscated {
		t.Fatalf("reopened for writing: %+v", wc.Info())
	}
	wc.Close()
}

// A vault is never made from no credential; any other profile given a
// credential is encrypted; a keyfile credential records kind 3.
func TestVD_Vault_Create(t *testing.T) {
	lowKDF(t)
	o := CreateOptions{LogicalSize: 1 << 20, Profile: ProfileVault}
	err := validateCreate(&o)
	if !errors.Is(err, ErrVaultNeedsCredential) || ExitClass(err) != ExitUsage || Explain(err) != msgVault {
		t.Fatalf("vault without a credential: class %d, %q", ExitClass(err), Explain(err))
	}
	for _, p := range []Profile{ProfilePlain, ProfileFast, ProfileRAM} {
		c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: p, Credential: fdsec.NewCredential("pw")})
		c.Close()
		if _, err := openMemCred(&memBacking{data: mb.snapshot()}, "no", OpenRead); ExitClass(err) != ExitCredential {
			t.Fatalf("%s with a credential opened under another one: %v", p, err)
		}
		rc, err := openMemCred(&memBacking{data: mb.snapshot()}, "pw", OpenRead)
		if err != nil || rc.Info().Obfuscated {
			t.Fatalf("%s with a credential: %v", p, err)
		}
		rc.Close()
	}
	// An obfuscated container ignores a credential (FDD-BEHAVIOUR 7 rule 4).
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16})
	c.Close()
	if rc, err := openMemCred(&memBacking{data: mb.snapshot()}, "anything", OpenRead); err != nil || !rc.Info().Obfuscated {
		t.Fatalf("obfuscated with a credential: %v", err)
	}
	// Kind 3 for a keyfile.
	digest := strings.Repeat("ab", 32)
	c, mb = newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential(digest), Keyfile: true})
	c.Close()
	img := mb.snapshot()
	res, _ := resolveHeaders(&memBacking{data: img}, int64(len(img)))
	k, _ := credKey(fdsec.NewCredential(digest), res.hdr.KDFSalt[:], 1)
	s := img[res.hdr.SlotsOffset:][:slotSize]
	sk := slotKey(k, s[:slotSaltSize])
	pt := openSlotForTest(t, sk[:], s)
	if pt[4] != slotKindKeyfile {
		t.Fatalf("a keyfile slot records kind %d", pt[4])
	}
	o = CreateOptions{LogicalSize: 1 << 20, Keyfile: true}
	if err := validateCreate(&o); ExitClass(err) != ExitUsage {
		t.Fatalf("a keyfile with no credential: %v", err)
	}
}

// T5.10: the failures of the exit criterion are distinct, named, and carry
// their class; damage is damage with or without a credential.
func TestVD_FailureTaxonomy(t *testing.T) {
	lowKDF(t)
	c, mb := newVault(t, "test")
	c.WriteAt(pattern(9, 200000), 0)
	c.Close()
	img := mb.snapshot()

	type row struct {
		name  string
		img   []byte
		cred  string
		class int
		msg   string
	}
	bothBad := append([]byte(nil), img...)
	bothBad[200] ^= 1
	bothBad[len(bothBad)-200] ^= 1
	res, _ := resolveHeaders(&memBacking{data: img}, int64(len(img)))
	cut := int64(res.hdr.DataOffset) + 70000
	short := append([]byte(nil), img[:cut]...)
	notOurs := make([]byte, 64<<10)
	newDetRand("not ours").Read(notOurs)
	rows := []row{
		{"wrong credential", img, "Test", ExitCredential, msgCredential},
		{"empty credential", img, "", ExitCredential, msgCredential},
		{"both headers damaged, no credential", bothBad, "", ExitDamaged, msgHeaders},
		{"both headers damaged, the right credential", bothBad, "test", ExitDamaged, msgHeaders},
		{"not a container", notOurs, "test", ExitDamaged, msgHeaders},
		{"truncated", short, "test", ExitDamaged, fmt.Sprintf(msgTruncated, uint64(len(img))-uint64(cut))},
	}
	for _, r := range rows {
		_, err := openMemCred(&memBacking{data: append([]byte(nil), r.img...)}, r.cred, OpenRead)
		if ExitClass(err) != r.class || Explain(err) != r.msg {
			t.Errorf("%s: class %d, %q; want %d, %q", r.name, ExitClass(err), Explain(err), r.class, r.msg)
		}
	}
	// No message names a slot or a count.
	for _, m := range []string{msgCredential, msgHeaders, msgTruncated} {
		if strings.Contains(strings.ToLower(m), "slot") {
			t.Errorf("a door message names a slot: %q", m)
		}
	}
}

// P5 section 5 rule 3: a failed open writes nothing, in any mode.
func TestVD_NoWriteOnFailedOpen(t *testing.T) {
	lowKDF(t)
	path := filepath.Join(t.TempDir(), "v.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential("test")})
	if err != nil {
		t.Fatal(err)
	}
	c.WriteAt(pattern(4, 100000), 0)
	c.Close()
	digest := func() [32]byte {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return sha256.Sum256(b)
	}
	before := digest()
	for _, mode := range []OpenMode{OpenRead, OpenWrite, OpenMount} {
		for _, cred := range []string{"", "Test", "wrong"} {
			if _, err := Open(context.Background(), path, fdsec.NewCredential(cred), mode); ExitClass(err) != ExitCredential {
				t.Fatalf("mode %d credential %q: %v", mode, cred, err)
			}
			if digest() != before {
				t.Fatalf("a failed open in mode %d changed the file", mode)
			}
		}
	}
	info, err := Inspect(path)
	if err != nil || info.MountCount != 0 {
		t.Fatalf("after failed opens: %+v %v", info, err)
	}
}

// A sealed copy of a vault is encrypted under the same credential, with a new
// data key.
func TestVD_Vault_Seal(t *testing.T) {
	lowKDF(t)
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "v.fdd"), filepath.Join(dir, "s.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential("test")})
	if err != nil {
		t.Fatal(err)
	}
	data := pattern(5, 150000)
	c.WriteAt(data, 1000)
	c.Close()
	if err := Seal(context.Background(), src, dst, nil); ExitClass(err) != ExitCredential {
		t.Fatalf("sealing a vault with no credential: %v", err)
	}
	if err := SealWith(context.Background(), SealOptions{Src: src, Dst: dst, Credential: fdsec.NewCredential("test")}); err != nil {
		t.Fatal(err)
	}
	s, err := Open(context.Background(), dst, fdsec.NewCredential("test"), OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if i := s.Info(); i.Profile != ProfileSealed || i.Obfuscated {
		t.Fatalf("the sealed copy: %+v", i)
	}
	got := make([]byte, len(data))
	if _, err := s.ReadAt(got, 1000); err != nil || !bytes.Equal(got, data) {
		t.Fatalf("sealed read back: %v", err)
	}
	if _, err := Open(context.Background(), dst, fdsec.NewCredential("x"), OpenRead); ExitClass(err) != ExitCredential {
		t.Fatalf("the sealed copy under another credential: %v", err)
	}
}

func openSlotForTest(t testing.TB, sk []byte, s []byte) []byte {
	t.Helper()
	a, err := chacha20poly1305.NewX(sk)
	if err != nil {
		t.Fatal(err)
	}
	pt, err := a.Open(nil, s[slotSaltSize:slotSaltSize+slotNonceSize], s[slotSaltSize+slotNonceSize:slotSaltSize+slotNonceSize+slotSealedSize], []byte(adSlot))
	if err != nil {
		t.Fatal("the slot does not open under its key")
	}
	return pt
}

// failAfter lets n writes through and fails every later one: a stop at a
// chosen step of a slot rewrite.
type failAfter struct {
	*memBacking
	n int
}

func (f *failAfter) WriteAt(p []byte, off int64) (int, error) {
	if f.n == 0 {
		return 0, errors.New("stopped here by the test")
	}
	f.n--
	return f.memBacking.WriteAt(p, off)
}

// T5.8 under the one-credential rule: the credential changes without the data
// region moving, the old one stops opening, and a stop between the two slot
// writes leaves a container that either credential opens.
func TestVD_Pass(t *testing.T) {
	lowKDF(t)
	c, mb := newVault(t, "old")
	data := pattern(6, 250000)
	c.WriteAt(data, 5000)
	c.Close()
	base := mb.snapshot()
	res, _ := resolveHeaders(&memBacking{data: base}, int64(len(base)))
	dataStart := int(res.hdr.DataOffset)

	img := &memBacking{data: append([]byte(nil), base...)}
	if err := changeCredentialOn(img, fdsec.NewCredential("old"), fdsec.NewCredential("new"), false); err != nil {
		t.Fatal(err)
	}
	after := img.snapshot()
	if !bytes.Equal(after[dataStart:], base[dataStart:]) || !bytes.Equal(after[:int(res.hdr.SlotsOffset)], base[:int(res.hdr.SlotsOffset)]) {
		t.Fatal("changing the credential touched more than the slot region")
	}
	if _, err := openMemCred(&memBacking{data: after}, "old", OpenRead); ExitClass(err) != ExitCredential {
		t.Fatalf("the old credential after the change: %v", err)
	}
	rc, err := openMemCred(&memBacking{data: after}, "new", OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, rc); !bytes.Equal(got[5000:5000+len(data)], data) {
		t.Fatal("the volume changed with the credential")
	}
	rc.Close()
	// Twice more: the rule holds from any position the slot now sits in.
	if err := changeCredentialOn(img, fdsec.NewCredential("new"), fdsec.NewCredential("third"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := openMemCred(&memBacking{data: img.snapshot()}, "third", OpenRead); err != nil {
		t.Fatal(err)
	}

	// A stop after the new slot is written, before the old one is retired.
	stop := &failAfter{memBacking: &memBacking{data: append([]byte(nil), base...)}, n: 1}
	if err := changeCredentialOn(stop, fdsec.NewCredential("old"), fdsec.NewCredential("new"), false); err == nil {
		t.Fatal("the stopped change reported success")
	}
	for _, cred := range []string{"old", "new"} {
		if _, err := openMemCred(&memBacking{data: stop.snapshot()}, cred, OpenRead); err != nil {
			t.Fatalf("after a stop between the slot writes, %q: %v", cred, err)
		}
	}

	// Refusals write nothing.
	for _, r := range []struct {
		old, next string
		class     int
	}{
		{"wrong", "new", ExitCredential},
		{"old", "", ExitUsage},
	} {
		img := &memBacking{data: append([]byte(nil), base...)}
		err := changeCredentialOn(img, fdsec.NewCredential(r.old), fdsec.NewCredential(r.next), false)
		if ExitClass(err) != r.class || !bytes.Equal(img.snapshot(), base) {
			t.Errorf("%q -> %q: class %d (%v), file changed %v", r.old, r.next, ExitClass(err), err, !bytes.Equal(img.snapshot(), base))
		}
	}
	oc, omb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16})
	oc.Close()
	if err := changeCredentialOn(omb, nil, fdsec.NewCredential("x"), false); ExitClass(err) != ExitUsage {
		t.Errorf("an obfuscated container: %v", err)
	}
}

// A sealed container is never written again, its slots included (security
// review of S5).
func TestVD_Pass_Sealed(t *testing.T) {
	lowKDF(t)
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "v.fdd"), filepath.Join(dir, "s.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: src, LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential("a")})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := SealWith(context.Background(), SealOptions{Src: src, Dst: dst, Credential: fdsec.NewCredential("a")}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(dst)
	if err := ChangeCredential(dst, fdsec.NewCredential("a"), fdsec.NewCredential("b"), false); ExitClass(err) != ExitUsage {
		t.Fatalf("a sealed container: %v", err)
	}
	if after, _ := os.ReadFile(dst); !bytes.Equal(before, after) {
		t.Fatal("the sealed container changed")
	}
}

// T5.9, the last-slot rule: an encrypted container is never left without a
// credential in place - the empty credential is refused and nothing is
// written.
func TestVD_LastSlot(t *testing.T) {
	lowKDF(t)
	path := filepath.Join(t.TempDir(), "v.fdd")
	c, err := Create(context.Background(), CreateOptions{Path: path, LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential("test")})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	before, _ := os.ReadFile(path)
	if err := ChangeCredential(path, fdsec.NewCredential("test"), nil, false); ExitClass(err) != ExitUsage {
		t.Fatalf("dropping the last credential: %v", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
		t.Fatal("a refused drop wrote to the file")
	}
	if c, err := Open(context.Background(), path, fdsec.NewCredential("test"), OpenRead); err != nil {
		t.Fatalf("the credential no longer opens it: %v", err)
	} else {
		c.Close()
	}
}

// T5.9, the conversion (owner decision 2026-09-27): a vault loses its
// credential only as a copy, asked for by name; the source is untouched and
// keeps it.
func TestVD_Vault_Copy(t *testing.T) {
	lowKDF(t)
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "v.fdd")
	c, err := Create(ctx, CreateOptions{Path: src, LogicalSize: 1 << 20, ClusterShift: 16, Profile: ProfileVault, Credential: fdsec.NewCredential("test")})
	if err != nil {
		t.Fatal(err)
	}
	data := pattern(9, 150000)
	c.WriteAt(data, 3000)
	c.Close()
	srcBytes, _ := os.ReadFile(src)
	srcID := func() string { i, _ := Inspect(src); return i.ContainerID }()
	check := func(path, cred string, profile Profile, obfuscated bool) {
		t.Helper()
		d, err := Open(ctx, path, fdsec.NewCredential(cred), OpenRead)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(path), err)
		}
		defer d.Close()
		i := d.Info()
		if i.Profile != profile || i.Obfuscated != obfuscated || i.ContainerID == srcID {
			t.Fatalf("%s: profile %v obfuscated %v id %s", filepath.Base(path), i.Profile, i.Obfuscated, i.ContainerID)
		}
		got := make([]byte, len(data))
		if _, err := d.ReadAt(got, 3000); err != nil || !bytes.Equal(got, data) {
			t.Fatalf("%s: the volume differs (%v)", filepath.Base(path), err)
		}
	}
	// Without the credential there is no copy at all.
	if err := CopyWith(ctx, CopyOptions{Src: src, Dst: filepath.Join(dir, "x.fdd"), Obfuscate: true}); ExitClass(err) != ExitCredential {
		t.Fatalf("a copy without the credential: %v", err)
	}
	// A plain copy keeps the vault and its credential.
	keep := filepath.Join(dir, "keep.fdd")
	if err := CopyWith(ctx, CopyOptions{Src: src, Dst: keep, Credential: fdsec.NewCredential("test")}); err != nil {
		t.Fatal(err)
	}
	check(keep, "test", ProfileVault, false)
	// Obfuscate: a writable plain container that opens for anyone.
	open := filepath.Join(dir, "open.fdd")
	if err := CopyWith(ctx, CopyOptions{Src: src, Dst: open, Credential: fdsec.NewCredential("test"), Obfuscate: true}); err != nil {
		t.Fatal(err)
	}
	check(open, "", ProfilePlain, true)
	w, err := Open(ctx, open, nil, OpenWrite)
	if err != nil {
		t.Fatalf("the obfuscated copy is not writable: %v", err)
	}
	w.Close()
	// And a seal with Obfuscate is sealed and obfuscated.
	sealed := filepath.Join(dir, "sealed.fdd")
	if err := SealWith(ctx, SealOptions{Src: src, Dst: sealed, Credential: fdsec.NewCredential("test"), Obfuscate: true}); err != nil {
		t.Fatal(err)
	}
	check(sealed, "", ProfileSealed, true)
	// The source never changed, and still needs its credential.
	if after, _ := os.ReadFile(src); !bytes.Equal(srcBytes, after) {
		t.Fatal("a copy changed the source")
	}
	if _, err := Open(ctx, src, nil, OpenRead); ExitClass(err) != ExitCredential {
		t.Fatalf("the source opens without its credential: %v", err)
	}
}
