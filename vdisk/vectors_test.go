package vdisk

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"golang.org/x/crypto/blake2b"
)

// The conformance vectors of FDD-FORMAT section 15 that stage S2 produces.
// The authoritative set is the catalog's disk-container/vectors/; this
// package keeps a byte-identical fixture under testdata/vectors/.
//
//	go test ./vdisk/ -run TestVD_Vectors                     checks the fixture
//	FDD_CATALOG_VECTORS=<catalog vectors dir> ...            also byte-compares it with the catalog
//	FDD_WRITE_VECTORS=1 FDD_NTFS_VOLUME=<16 MiB NTFS volume> regenerates the fixture (see PROVENANCE.txt)
//
// Every vector is checked three ways: its sha256 against PROVENANCE.txt; its
// expected parse result against indepRead, a reader written from the document
// alone; and against this package's own reader, which must also leave the file
// byte-identical. The writer-made vectors are then rebuilt from their recorded
// random draws and compared byte for byte, so the writer is pinned too.

const vecDir = "testdata/vectors"

// The known file written into the NTFS volume of plain-onefile.fdd: 2000 lines
// "FileDO FDD vector line NNNNN\n", 58000 bytes.
const onefileName = "hello.txt"

func onefileContent() []byte {
	var b bytes.Buffer
	for i := 1; i <= 2000; i++ {
		fmt.Fprintf(&b, "FileDO FDD vector line %05d\n", i)
	}
	return b.Bytes()
}

func hexBytes(s string) ([]byte, error) { return hex.DecodeString(s) }

type vecDraw struct {
	What string `json:"what"`
	Hex  string `json:"hex"`
}

type vecParams struct {
	Profile      string `json:"profile"`
	SectorShift  int    `json:"sector_shift"`
	ClusterShift int    `json:"cluster_shift"`
	LogicalSize  int64  `json:"logical_size"`
	WriterStamp  uint64 `json:"writer_stamp"`
	FriendlyName string `json:"friendly_name"`
	Credential   string `json:"credential,omitempty"` // the passphrase of a kind-2 slot
}

type vecExpected struct {
	Outcome      string   `json:"outcome"` // opens, damaged
	ExitClass    int      `json:"exit_class"`
	HeaderSource string   `json:"header_source,omitempty"`
	Refused      []string `json:"refused_credentials,omitempty"` // each gives exit class 3
	MissingBytes int64    `json:"missing_bytes,omitempty"`
	Note         string   `json:"note"`
}

type vecParse struct {
	PrimaryPlaintext string      `json:"primary_header_plaintext,omitempty"`
	BackupPlaintext  string      `json:"backup_header_plaintext,omitempty"`
	HeaderKeyPrimary string      `json:"header_key_primary,omitempty"`
	HeaderKeyBackup  string      `json:"header_key_backup,omitempty"`
	ObfKey           string      `json:"obf_key,omitempty"`
	PwSeed           string      `json:"pw_seed,omitempty"`
	CredKey          string      `json:"cred_key,omitempty"`
	SlotKeys         []string    `json:"slot_keys"`
	DataKey          string      `json:"data_key"`
	ActiveMapCopy    string      `json:"active_map_copy"`
	Allocated        [][2]uint64 `json:"allocated_clusters"`
	VolumeBlake2b256 string      `json:"volume_blake2b_256"`
}

type vecFileEntry struct {
	Name         string `json:"name"`
	Size         int    `json:"size"`
	SHA256       string `json:"sha256"`
	VolumeOffset int64  `json:"volume_offset"`
	Note         string `json:"note"`
}

type vecMeta struct {
	Vector       string         `json:"vector"`
	Contract     string         `json:"contract"`
	Generator    string         `json:"generator"`
	DerivedFrom  string         `json:"derived_from,omitempty"`
	Mutation     string         `json:"mutation,omitempty"`
	Parameters   *vecParams     `json:"parameters,omitempty"`
	FileSize     int            `json:"file_size"`
	SHA256       string         `json:"sha256"`
	Expected     vecExpected    `json:"expected"`
	RandomDraws  []vecDraw      `json:"random_draws,omitempty"`
	ClockReads   []string       `json:"clock_readings,omitempty"`
	WriteOps     []string       `json:"write_operations,omitempty"`
	Parse        *vecParse      `json:"parse,omitempty"`
	Files        []vecFileEntry `json:"files,omitempty"`
	VolumeFormat string         `json:"volume_format,omitempty"`
}

// drawNames labels the random draws of a creation in the order of FDD-FORMAT
// section 15.1, then every header write's nonce and pad.
func drawNames(draws [][]byte) []vecDraw {
	var out []vecDraw
	names := []string{"kdf_salt", "container_id (before the version and variant bits)", "data_key",
		"slot 0 slot_salt", "slot 0 slot_nonce", "slot 0 padding"}
	for i := 1; i < 8; i++ {
		names = append(names, fmt.Sprintf("slot %d (unused)", i))
	}
	names = append(names, "primary salt", "backup salt")
	w := 0
	for i, d := range draws {
		var n string
		switch {
		case i < len(names):
			n = names[i]
		case (i-len(names))%2 == 0:
			w++
			n = fmt.Sprintf("header write %d nonce", w)
		default:
			n = fmt.Sprintf("header write %d pad", w)
		}
		out = append(out, vecDraw{n, hex.EncodeToString(d)})
	}
	return out
}

type builtVector struct {
	file []byte
	meta vecMeta
}

var vecParamsDefault = vecParams{Profile: "plain", SectorShift: 12, ClusterShift: 16, LogicalSize: 16 << 20, WriterStamp: 0}

// buildWriterVector creates a container deterministically and runs writes on it.
func buildWriterVector(t *testing.T, name string, p vecParams, profile Profile, writes func(c *Container) []string) builtVector {
	t.Helper()
	r, clock := deterministic(t, name)
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: p.LogicalSize, Profile: profile, SectorShift: uint8(p.SectorShift), ClusterShift: uint8(p.ClusterShift), FriendlyName: p.FriendlyName, Credential: []byte(p.Credential)})
	var ops []string
	if writes != nil {
		ops = writes(c)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	f := mb.snapshot()
	m := vecMeta{
		Vector:      name + ".fdd",
		Parameters:  &p,
		RandomDraws: drawNames(r.draws),
		WriteOps:    ops,
	}
	for _, t := range clock.reads {
		m.ClockReads = append(m.ClockReads, t.Format("2006-01-02T15:04:05.000000000Z"))
	}
	return builtVector{f, m}
}

func buildVectors(t *testing.T, ntfsVolume []byte) map[string]builtVector {
	out := map[string]builtVector{}
	empty := vecParamsDefault
	empty.FriendlyName = "plain-empty"
	v := buildWriterVector(t, "plain-empty", empty, ProfilePlain, nil)
	v.meta.Expected = vecExpected{Outcome: "opens", ExitClass: 0, HeaderSource: "primary", Note: "every logical byte is zero; data_offset lies past the end of the file"}
	out["plain-empty"] = v

	one := vecParamsDefault
	one.FriendlyName = "plain-onefile"
	v = buildWriterVector(t, "plain-onefile", one, ProfilePlain, func(c *Container) []string {
		var ops []string
		for off := int64(0); off < int64(len(ntfsVolume)); off += 1 << 16 {
			cl := ntfsVolume[off : off+1<<16]
			if allZero(cl) {
				continue
			}
			if _, err := c.WriteAt(cl, off); err != nil {
				t.Fatal(err)
			}
			ops = append(ops, fmt.Sprintf("write the 65536 volume bytes at %d", off))
		}
		return ops
	})
	content := onefileContent()
	at := bytes.Index(ntfsVolume, content)
	if at < 0 {
		t.Fatal("the known file is not contiguous in the NTFS volume")
	}
	sum := sha256.Sum256(content)
	v.meta.Files = []vecFileEntry{{Name: onefileName, Size: len(content), SHA256: hex.EncodeToString(sum[:]), VolumeOffset: int64(at),
		Note: "the file's data is one contiguous run; extracting the raw volume and reading size bytes at volume_offset returns it"}}
	v.meta.VolumeFormat = "a bare NTFS volume (no partition table), 512-byte sectors, 4096-byte clusters, label FDDVECTOR, made by ntfs-3g mkntfs 2022.10.3 with zero timestamps; the file copied in by ntfscp"
	v.meta.Expected = vecExpected{Outcome: "opens", ExitClass: 0, HeaderSource: "primary", Note: "the file extracts byte-exact"}
	out["plain-onefile"] = v

	fast := vecParamsDefault
	fast.Profile, fast.LogicalSize, fast.FriendlyName = "fast", 1<<20, "fast-preallocated"
	v = buildWriterVector(t, "fast-preallocated", fast, ProfileFast, func(c *Container) []string {
		if _, err := c.WriteAt(fastPattern(), 0); err != nil {
			t.Fatal(err)
		}
		return []string{"write the 1048576-byte pattern at 0: byte i = (i*31 + i>>12) mod 256"}
	})
	v.meta.Expected = vecExpected{Outcome: "opens", ExitClass: 0, HeaderSource: "primary", Note: "every cluster allocated in logical order; no sentinel in the map"}
	out["fast-preallocated"] = v

	pe := out["plain-empty"].file
	flipP := int64(sealedOffset + 100)
	dh := append([]byte(nil), pe...)
	dh[flipP] ^= 0x01
	out["damaged-header"] = builtVector{dh, vecMeta{Vector: "damaged-header.fdd", DerivedFrom: "plain-empty.fdd",
		Mutation: fmt.Sprintf("byte %d (inside the primary's sealed region) XOR 0x01", flipP),
		Expected: vecExpected{Outcome: "opens", ExitClass: 0, HeaderSource: "backup", Note: "the backup carries the open; a read-only open writes nothing; the volume is plain-empty's"}}}

	flipB := int64(len(pe)) - headerSize + sealedOffset + 100
	db := append([]byte(nil), dh...)
	db[flipB] ^= 0x01
	out["damaged-both"] = builtVector{db, vecMeta{Vector: "damaged-both.fdd", DerivedFrom: "plain-empty.fdd",
		Mutation: fmt.Sprintf("bytes %d and %d (inside each header's sealed region) XOR 0x01", flipP, flipB),
		Expected: vecExpected{Outcome: "damaged", ExitClass: ExitDamaged, Note: "damaged with or without a credential; never a wrong credential"}}}

	po := out["plain-onefile"].file
	cut := int64(1<<20) + 1<<16 + 777
	tr := append([]byte(nil), po[:cut]...)
	out["truncated"] = builtVector{tr, vecMeta{Vector: "truncated.fdd", DerivedFrom: "plain-onefile.fdd",
		Mutation: fmt.Sprintf("the file cut to its first %d bytes, inside physical cluster 1 of the data region", cut),
		Expected: vecExpected{Outcome: "damaged", ExitClass: ExitDamaged, HeaderSource: "primary", MissingBytes: int64(len(po)) - cut,
			Note: "the header opens and names the missing bytes; the map references clusters past the end; damaged, not a crash"}}}

	// Stage S5: the credential path, under kdf_params_id 1 as the table has it.
	vault := vecParamsDefault
	vault.Profile, vault.FriendlyName, vault.Credential = "vault", "vault-empty", "test"
	v = buildWriterVector(t, "vault-empty", vault, ProfileVault, nil)
	v.meta.Expected = vecExpected{Outcome: "opens", ExitClass: 0, HeaderSource: "primary", Refused: []string{"Test", ""},
		Note: "opens with the credential test; Test and the empty credential are a wrong credential (class 3), never damage; every logical byte is zero"}
	out["vault-empty"] = v

	for k, v := range out {
		s := sha256.Sum256(v.file)
		v.meta.SHA256 = hex.EncodeToString(s[:])
		v.meta.FileSize = len(v.file)
		v.meta.Contract = "FDD-FORMAT document 0.1 (draft), wire version 1.0"
		v.meta.Generator = "FileDO vdisk, SP-0004 stage S2: FDD_WRITE_VECTORS=1 go test ./vdisk/ -run TestVD_Vectors"
		if k == "vault-empty" {
			v.meta.Generator = "FileDO vdisk, SP-0004 stage S5: FDD_WRITE_VECTORS=1 go test ./vdisk/ -run TestVD_Vectors"
		}
		if r := indepReadWith(v.file, credOf(v.meta)); r.outcome == "opens" {
			v.meta.Parse = parseOf(r)
		}
		out[k] = v
	}
	return out
}

// credOf is the credential a vector opens with; nil for an obfuscated one.
func credOf(m vecMeta) []byte {
	if m.Parameters == nil || m.Parameters.Credential == "" {
		return nil
	}
	return []byte(m.Parameters.Credential)
}

func fastPattern() []byte {
	b := make([]byte, 1<<20)
	for i := range b {
		b[i] = byte(i*31 + i>>12)
	}
	return b
}

func parseOf(r indepResult) *vecParse {
	p := &vecParse{
		PrimaryPlaintext: hex.EncodeToString(r.primaryPlain),
		BackupPlaintext:  hex.EncodeToString(r.backupPlain),
		HeaderKeyPrimary: hex.EncodeToString(r.headerKeyP),
		HeaderKeyBackup:  hex.EncodeToString(r.headerKeyB),
		ObfKey:           hex.EncodeToString(r.obfKey),
		PwSeed:           hex.EncodeToString(r.pwSeed),
		CredKey:          hex.EncodeToString(r.credKey),
		DataKey:          hex.EncodeToString(r.dataKey),
		ActiveMapCopy:    hex.EncodeToString(r.activeMap),
		Allocated:        r.allocated,
		VolumeBlake2b256: hex.EncodeToString(r.volumeBlake2b),
	}
	if p.Allocated == nil {
		p.Allocated = [][2]uint64{}
	}
	for _, k := range r.slotKeys {
		p.SlotKeys = append(p.SlotKeys, hex.EncodeToString(k))
	}
	return p
}

var vecOrder = []string{"plain-empty", "plain-onefile", "fast-preallocated", "damaged-header", "damaged-both", "truncated", "vault-empty"}

const provenanceHead = `# Conformance vectors for FDD-FORMAT and FDD-BEHAVIOUR.
# Seven vectors of FDD-FORMAT.md section 15, generated by FileDO's reference implementation (package
# vdisk), never written by hand: six on 2026-09-26 (SP-0004 stage S2) and vault-empty.fdd on 2026-09-27
# (stage S5, the credential path, kdf_params_id 1 as section 8.2 fixes it). Each .fdd has a .json of its
# expected parse result: parameters, every random draw in the order of section 15.1, the clock readings,
# both header plaintexts, every derived key (obf_key, or pw_seed and cred_key), the active map, the
# allocated clusters, and the BLAKE2b-256 of the logical volume. Every value the writer chose is
# recorded, so the vectors stay valid whatever the [PENDING G0] defaults become, as long as sector_shift
# 12 and cluster_shift 16 stay inside the allowed ranges; a narrowing of the ranges regenerates them.
# plain-onefile.fdd holds a bare NTFS volume made without any Windows formatter:
#   mkntfs -F -Q -T -s 512 -c 4096 -p 0 -H 0 -S 0 -L FDDVECTOR volume.img   (ntfs-3g 2022.10.3, 16 MiB file)
#   ntfscp -f volume.img hello.txt /hello.txt   (hello.txt = 2000 lines "FileDO FDD vector line NNNNN\n")
# and every 64 KiB volume cluster that is not all zeros written into the container in logical order.
# Planned, not generated here: sealed-compressed.fdd (with the minor that defines the compression table).
contract: FDD-FORMAT
version: 0.1
generator: FDD_WRITE_VECTORS=1 FDD_NTFS_VOLUME=<volume.img> go test ./vdisk/ -run TestVD_Vectors
fixture copy: vdisk/testdata/vectors/ in the FileDO repository, byte-identical to this folder
`

func TestVD_Vectors(t *testing.T) {
	if os.Getenv("FDD_WRITE_VECTORS") == "1" {
		// Without FDD_NTFS_VOLUME the volume is the one the committed
		// plain-onefile.fdd holds, which rebuilds it byte for byte.
		var vol []byte
		if p := os.Getenv("FDD_NTFS_VOLUME"); p != "" {
			var err error
			if vol, err = os.ReadFile(p); err != nil {
				t.Fatalf("FDD_NTFS_VOLUME: %v", err)
			}
		} else {
			vol = indepRead(mustReadFile(t, filepath.Join(vecDir, "plain-onefile.fdd"))).volume
		}
		writeVectors(t, buildVectors(t, vol))
	}

	// 1. Every file is the one PROVENANCE.txt names.
	prov := mustReadFile(t, filepath.Join(vecDir, "PROVENANCE.txt"))
	named := map[string]string{}
	for _, line := range strings.Split(string(prov), "\n") {
		if f := strings.Fields(line); len(f) == 3 && f[0] == "sha256" {
			named[f[1]] = f[2]
		}
	}
	if len(named) != 2*len(vecOrder) {
		t.Fatalf("PROVENANCE.txt names %d files, want %d", len(named), 2*len(vecOrder))
	}
	for n, sum := range named {
		if got := fmt.Sprintf("%x", sha256.Sum256(mustReadFile(t, filepath.Join(vecDir, n)))); got != sum {
			t.Fatalf("%s: sha256 %s, PROVENANCE says %s", n, got, sum)
		}
	}
	// 2. The fixture is the catalog, byte for byte.
	if cat := os.Getenv("FDD_CATALOG_VECTORS"); cat != "" {
		for _, n := range append(sortedNames(named), "PROVENANCE.txt") {
			if !bytes.Equal(mustReadFile(t, filepath.Join(vecDir, n)), mustReadFile(t, filepath.Join(cat, n))) {
				t.Fatalf("the catalog's %s differs from the fixture", n)
			}
		}
	}

	files := map[string][]byte{}
	metas := map[string]vecMeta{}
	for _, name := range vecOrder {
		f := mustReadFile(t, filepath.Join(vecDir, name+".fdd"))
		var m vecMeta
		if err := json.Unmarshal(mustReadFile(t, filepath.Join(vecDir, name+".json")), &m); err != nil {
			t.Fatalf("%s.json: %v", name, err)
		}
		files[name], metas[name] = f, m
		t.Run(name, func(t *testing.T) { checkVector(t, name, f, m) })
	}

	// 3. The writer still makes the writer-made vectors, byte for byte, from
	// their recorded draws. plain-onefile is rebuilt from the volume the
	// independent reader extracts from the committed file.
	vol := indepRead(files["plain-onefile"]).volume
	rebuilt := buildVectors(t, vol)
	for _, name := range vecOrder {
		if !bytes.Equal(rebuilt[name].file, files[name]) {
			t.Errorf("%s: the writer no longer produces the committed vector", name)
		}
		want, _ := json.Marshal(metas[name])
		got, _ := json.Marshal(rebuilt[name].meta)
		if !bytes.Equal(want, got) {
			t.Errorf("%s: the expected parse result drifted from the committed .json", name)
		}
	}
}

func checkVector(t *testing.T, name string, f []byte, m vecMeta) {
	if len(f) != m.FileSize {
		t.Fatalf("file size %d, the .json says %d", len(f), m.FileSize)
	}
	// Against the independent reader.
	cred := credOf(m)
	r := indepReadWith(f, cred)
	for _, w := range m.Expected.Refused {
		if got := indepReadWith(f, []byte(w)); got.outcome != "credential" {
			t.Fatalf("independent reader, credential %q: %s (%s), want a wrong credential", w, got.outcome, got.reason)
		}
	}
	if r.outcome != m.Expected.Outcome {
		t.Fatalf("independent reader: %s (%s), want %s", r.outcome, r.reason, m.Expected.Outcome)
	}
	if m.Expected.HeaderSource != "" && r.headerSource != m.Expected.HeaderSource && r.outcome == "opens" {
		t.Fatalf("independent reader: header from %s, want %s", r.headerSource, m.Expected.HeaderSource)
	}
	if r.outcome == "opens" {
		if m.Parse == nil {
			t.Fatal("the .json has no parse result")
		}
		got, _ := json.Marshal(parseOf(r))
		want, _ := json.Marshal(m.Parse)
		if !bytes.Equal(got, want) {
			t.Fatal("the independent reader's parse differs from the .json")
		}
	}
	for _, fe := range m.Files {
		if r.volume == nil {
			t.Fatal("no volume to extract from")
		}
		data := r.volume[fe.VolumeOffset : fe.VolumeOffset+int64(fe.Size)]
		if fmt.Sprintf("%x", sha256.Sum256(data)) != fe.SHA256 || !bytes.Equal(data, onefileContent()) {
			t.Fatalf("%s does not extract byte-exact", fe.Name)
		}
	}

	// Against this package, through a real file: report, open, the volume
	// digest, the file extracted from ExportRaw, the class - and a file that
	// is byte-identical afterwards.
	path := filepath.Join(t.TempDir(), name+".fdd")
	if err := os.WriteFile(path, f, 0o644); err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(f)
	info, ierr := Inspect(path)
	// An obfuscated vector ignores a credential; an encrypted one opens with
	// its own and refuses every other with class 3.
	type attempt struct {
		cred  []byte
		class int
	}
	attempts := []attempt{{nil, m.Expected.ExitClass}, {[]byte("test"), m.Expected.ExitClass}}
	if cred != nil {
		attempts = []attempt{{cred, m.Expected.ExitClass}}
		for _, w := range m.Expected.Refused {
			attempts = append(attempts, attempt{[]byte(w), ExitCredential})
		}
	}
	for _, a := range attempts {
		c, err := Open(context.Background(), path, a.cred, OpenRead)
		if got := ExitClass(err); got != a.class {
			t.Fatalf("open (credential %q): class %d (%v), want %d", a.cred, got, err, a.class)
		}
		if err != nil {
			continue
		}
		if c.Info().FromBackup != (m.Expected.HeaderSource == "backup") {
			t.Fatal("the package did not use the header the vector names")
		}
		vol := readAll(t, c)
		if d := blake2b.Sum256(vol); hex.EncodeToString(d[:]) != m.Parse.VolumeBlake2b256 {
			t.Fatal("the package reads a different volume")
		}
		out := filepath.Join(t.TempDir(), "volume.img")
		if err := c.ExportRaw(context.Background(), out, RawFormImage, nil); err != nil {
			t.Fatal(err)
		}
		img := mustReadFile(t, out)
		for _, fe := range m.Files {
			if !bytes.Equal(img[fe.VolumeOffset:fe.VolumeOffset+int64(fe.Size)], onefileContent()) {
				t.Fatalf("%s does not extract byte-exact from the exported image", fe.Name)
			}
		}
		c.Close()
	}
	switch m.Expected.Outcome {
	case "opens":
		want := "obfuscated"
		if cred != nil {
			want = "encrypted"
		}
		if ierr != nil || info.Protection() != want || (m.Parameters != nil && info.Profile.String() != m.Parameters.Profile) {
			t.Fatalf("report: %v %+v", ierr, info)
		}
	case "damaged":
		if name == "truncated" {
			if ierr != nil || info.MissingBytes != m.Expected.MissingBytes {
				t.Fatalf("report on a truncated file: %v, missing %d, want %d", ierr, info.MissingBytes, m.Expected.MissingBytes)
			}
		} else if !errors.Is(ierr, ErrDamaged) {
			t.Fatalf("report: %v, want damaged", ierr)
		}
	}
	if after := sha256.Sum256(mustReadFile(t, path)); after != before {
		t.Fatal("a read-path operation changed the vector")
	}
}

func writeVectors(t *testing.T, vs map[string]builtVector) {
	if err := os.MkdirAll(vecDir, 0o755); err != nil {
		t.Fatal(err)
	}
	var prov strings.Builder
	prov.WriteString(provenanceHead)
	for _, name := range vecOrder {
		v := vs[name]
		j, err := json.MarshalIndent(v.meta, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		j = append(j, '\n')
		for _, f := range []struct {
			n string
			b []byte
		}{{name + ".fdd", v.file}, {name + ".json", j}} {
			if err := os.WriteFile(filepath.Join(vecDir, f.n), f.b, 0o644); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&prov, "sha256 %s %x\n", f.n, sha256.Sum256(f.b))
		}
	}
	if err := os.WriteFile(filepath.Join(vecDir, "PROVENANCE.txt"), []byte(prov.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("vectors written to %s", vecDir)
}

func mustReadFile(t testing.TB, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sortedNames(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
