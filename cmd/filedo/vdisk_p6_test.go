package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"filedo/fdsec"
	"filedo/vdisk"
)

// SP-0004 P6: the console surface of the container verbs - the alias table
// and its collisions (T6.7), order-independent options (T6.8), sizes (T6.9),
// the credential token (T6.10), several targets (T6.12), the destructive pair
// (T6.13), the packaged build (T6.26), and every verb that runs without
// elevation. The elevated path of format is the manual kit's.

// vdTestContainer makes a container of size bytes; cred "" is obfuscated.
func vdTestContainer(t *testing.T, path string, size int64, profile vdisk.Profile, cred string) {
	t.Helper()
	var c fdsec.Credential
	if cred != "" {
		c = fdsec.NewCredential(cred)
	}
	ct, err := vdisk.Create(context.Background(), vdisk.CreateOptions{Path: path, LogicalSize: size, ClusterShift: 16, Profile: profile, Credential: c})
	if err != nil {
		t.Fatal(err)
	}
	if err := ct.Close(); err != nil {
		t.Fatal(err)
	}
}

// vdTestFill writes a pattern into the volume and returns what the whole
// volume holds now.
func vdTestFill(t *testing.T, path, cred string) []byte {
	t.Helper()
	var k fdsec.Credential
	if cred != "" {
		k = fdsec.NewCredential(cred)
	}
	c, err := vdisk.Open(context.Background(), path, k, vdisk.OpenWrite)
	if err != nil {
		t.Fatal(err)
	}
	want := make([]byte, c.Info().LogicalSize)
	for i := 64 << 10; i < 320<<10; i++ {
		want[i] = byte(i*7 + i>>9)
	}
	if _, err := c.WriteAt(want[64<<10:320<<10], 64<<10); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	return want
}

// vdTestVolume reads the whole volume back.
func vdTestVolume(t *testing.T, path, cred string) []byte {
	t.Helper()
	var k fdsec.Credential
	if cred != "" {
		k = fdsec.NewCredential(cred)
	}
	c, err := vdisk.Open(context.Background(), path, k, vdisk.OpenRead)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	b := make([]byte, c.Info().LogicalSize)
	if _, err := c.ReadAt(b, 0); err != nil {
		t.Fatal(err)
	}
	return b
}

func vdTestHash(t *testing.T, path string) [32]byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(b)
}

// vdTestRun runs one vd line as the namespace form does and returns what it
// printed.
func vdTestRun(t *testing.T, batch bool, args ...string) (string, error) {
	t.Helper()
	var err error
	out := captureStdout(t, func() { err = runVd(args, NewHistoryLogger(nil), batch) })
	return out, err
}

// vdTestMounted records the container as mounted at X:, as a mount would.
func vdTestMounted(t *testing.T, path string) {
	t.Helper()
	i, err := vdisk.Inspect(path)
	if err != nil {
		t.Fatal(err)
	}
	abs, _ := filepath.Abs(path)
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = append(s.Mounts, vdMountRow{ContainerID: i.ContainerID, Path: abs, Letter: "X:"})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func vdTestEnv(t *testing.T) string {
	t.Helper()
	t.Setenv("FILEDO_STATE_DIR", t.TempDir())
	return t.TempDir()
}

func TestVD_DestroyWipeForceRefusesUnreadableMountState(t *testing.T) {
	dir := vdTestEnv(t)
	path := filepath.Join(dir, "keep.fdd")
	vdTestContainer(t, path, 1<<20, vdisk.ProfilePlain, "")
	before := vdTestHash(t, path)
	statePath, err := vdStatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(statePath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := vdTestRun(t, true, "destroy", path, "wipe", "force"); vdExitClass(err) != vdisk.ExitBusy {
		t.Fatalf("unreadable state allowed forced wipe: %v", err)
	}
	if got := vdTestHash(t, path); got != before {
		t.Fatal("a refused wipe changed the container")
	}
}

// AUD-34-F7: a container held open by a process this state root does not list
// (another account's mount, an unreadable state file) is not overwritten by
// destroy wipe force. The holder shares read and write as a block server does;
// the wipe opens with no sharing, so it is refused as busy, class 8, before one
// byte is written.
func TestVD_DestroyWipeForceRefusesAContainerAnotherProcessHolds(t *testing.T) {
	dir := vdTestEnv(t)
	path := filepath.Join(dir, "held.fdd")
	vdTestContainer(t, path, 1<<20, vdisk.ProfilePlain, "")
	before := vdTestHash(t, path)
	held, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, err := vdTestRun(t, true, "destroy", path, "wipe", "force"); vdExitClass(err) != vdisk.ExitBusy {
		t.Fatalf("a held container was not refused as busy: %v", err)
	}
	if got := vdTestHash(t, path); got != before {
		t.Fatal("a refused wipe changed the container held open by another process")
	}
	if _, err := vdisk.Inspect(path); err != nil {
		t.Fatalf("the held container no longer reads: %v", err)
	}
}

// T6.7: every alias of spec 5.2 resolves to its verb, and no word of the verb
// table collides with the generic chain's operation words or the sibling's
// option words - both read from the code, not from a copy.
func TestVD_Aliases(t *testing.T) {
	brief := map[string]string{
		"mount": "mount", "mnt": "mount", "attach": "mount",
		"unmount": "unmount", "umount": "unmount", "detach": "unmount",
		"export": "export", "extract": "export", "ext": "export",
		"verify": "verify", "vfy": "verify", "compact": "compact", "shrink": "compact",
		"grow": "grow", "resize": "grow", "new": "new", "create": "new",
		"destroy": "destroy", "erase": "destroy",
		"clone": "clone", "pass": "pass", "format": "format", "seal": "seal", "save": "save", "info": "info",
		"chkdsk": "chkdsk",
	}
	for w, want := range brief {
		for _, spelling := range []string{w, strings.ToUpper(w)} {
			if v, ok := vdVerbOf(spelling); !ok || v.name != want {
				t.Errorf("%q resolves to %q (%v), want %q", spelling, v.name, ok, want)
			}
		}
	}
	for _, w := range []string{"vd", "vdisk", "VDisk"} {
		if verbOf(w) != "vd" {
			t.Errorf("%q is not the vd verb", w)
		}
	}
	if _, ok := vdVerbOf("privilege"); ok {
		t.Error("vd privilege exists; it was removed (P6 owner decision 3)")
	}
	seen := map[string]string{}
	words := append([]string{}, list_of_flags_for_vd...)
	for _, v := range vdVerbs {
		for _, w := range v.words {
			if other, dup := seen[w]; dup {
				t.Errorf("%q names both %s and %s", w, other, v.name)
			}
			seen[w] = v.name
			if !vdRedactVerbs[w] {
				t.Errorf("%q is a vd verb the redaction does not screen after", w)
			}
			// info is the one shared word, on purpose: x.fdd info answers
			// from the container (spec 5.2).
			if v.name != "info" {
				words = append(words, w)
			}
		}
	}
	for _, w := range words {
		if isOperationWord(w) {
			t.Errorf("%q is an operation word of the generic chain (isOperationWord)", w)
		}
	}
	for opt := range fdsecOptionWords {
		for _, w := range words {
			if strings.EqualFold(opt, w) {
				t.Errorf("%q is an option word of the sibling (fdsecOptionWords)", w)
			}
		}
	}
	for w := range vdRedactVerbs {
		if _, ok := seen[w]; !ok && w != "register" && w != "unregister" {
			t.Errorf("the redaction screens after %q, which is no vd verb", w)
		}
	}
	// A target-first line is claimed for every container verb.
	for _, v := range vdVerbs {
		if got := vdClaims([]string{"a.fdd", v.words[0]}); got != v.container {
			t.Errorf("vdClaims(a.fdd %s) = %v, want %v", v.words[0], got, v.container)
		}
	}
	if !vdClaims([]string{`D:\x\*.fdd`, "info"}) || !vdClaims([]string{"*.fdd", "vfy"}) {
		t.Error("a mask of containers is not claimed")
	}
}

// T6.8: options are an order-independent set.
func TestVD_Options(t *testing.T) {
	var first vdMountOpts
	for i, args := range [][]string{
		{"a.fdd", "ro", "as", "X:", "noscan", "p:pw"},
		{"a.fdd", "as", "x", "ro", "p:pw", "noscan"},
		{"a.fdd", "p:pw", "noscan", "readonly", "as", "X:\\"},
	} {
		o, err := vdParseMountOpts(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if i == 0 {
			first = o
		} else if o != first {
			t.Errorf("%v parsed as %+v, want %+v", args, o, first)
		}
	}
	var ex vdExportOpts
	for i, args := range [][]string{
		{"a.fdd", `D:\o.img`, "raw", "p:pw"},
		{"a.fdd", "raw", `D:\o.img`, "p:pw"},
		{"a.fdd", "p:pw", "to", `D:\o.img`, "RAW"},
		{"a.fdd", "raw", "p:pw", "to", `D:\o.img`},
	} {
		o, err := vdParseExport(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if i == 0 {
			ex = o
		} else if o != ex {
			t.Errorf("%v parsed as %+v, want %+v", args, o, ex)
		}
	}
	var fo vdFormatOpts
	for i, args := range [][]string{
		{"a.fdd", "fs", "exfat", "label", "Work", "force", "p:pw"},
		{"a.fdd", "p:pw", "force", "label", "Work", "fs", "exFAT"},
		{"a.fdd", "label", "Work", "-y", "fs", "EXFAT", "p:pw"},
	} {
		o, err := vdParseFormat(args)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if i == 0 {
			fo = o
		} else if !reflect.DeepEqual(o, fo) {
			t.Errorf("%v parsed as %+v, want %+v", args, o, fo)
		}
	}
	if fo.fs != "exfat" || fo.label != "Work" || !fo.force || fo.cred != (credArg{"p", "pw"}) {
		t.Fatalf("format: %+v", fo)
	}
	for _, bad := range [][]string{
		{"a.fdd", "fs", "fat32"}, {"a.fdd", "label"}, {"a.fdd", "p:a", "k:b"},
	} {
		if _, err := vdParseFormat(bad); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("format %v: %v", bad, err)
		}
	}
	for _, bad := range [][]string{
		{"a.fdd"}, {"a.fdd", "raw", "vhd", `D:\o`}, {"a.fdd", "to"}, {"a.fdd", `D:\o`, `D:\p`}, {"a.fdd", "to", "p:secret"},
	} {
		if _, err := vdParseExport(bad); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("export %v: %v", bad, err)
		}
	}
}

// T6.9: sizes are parseSize's, as for speed and fill, without speed's 10 GiB
// bound; a size that does not parse is usage and is never quoted back.
func TestVD_Size(t *testing.T) {
	for in, want := range map[string]int64{
		"20G": 20 << 30, "512M": 512 << 20, "1T": 1 << 40, "1.5T": 3 << 39, "2048": 2 << 30,
		"100g": 100 << 30, "64MB": 64 << 20, "1024k": 1 << 20,
	} {
		if got, err := vdParseSize(in, "grow"); err != nil || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"hunter2", "0", "-1G", "1.5K", "", "20X", "G"} {
		_, err := vdParseSize(in, "grow")
		if vdExitClass(err) != vdisk.ExitUsage || (len(in) > 3 && strings.Contains(err.Error(), in)) {
			t.Errorf("%q: %v", in, err)
		}
	}
}

// T6.10: the credential token of the shared grammar - p:, pf:, pe:, k:, case
// sensitive; a bare token only as mount's one word; and every other verb
// refuses a bare word as usage without quoting it or touching anything.
func TestVD_CredentialToken(t *testing.T) {
	for tok, want := range map[string]credArg{
		"p:pw": {"p", "pw"}, "pf:C:\\pw.txt": {"pf", "C:\\pw.txt"}, "pe:VAR": {"pe", "VAR"}, "k:C:\\key": {"k", "C:\\key"}, "p:": {"p", ""},
	} {
		if a, ok := credentialToken(tok); !ok || a != want {
			t.Errorf("%q: %+v %v", tok, a, ok)
		}
	}
	for _, tok := range []string{"P:pw", "PF:x", "hunter2", "pw:x"} {
		if _, ok := credentialToken(tok); ok {
			t.Errorf("%q is taken as a credential", tok)
		}
	}
	if o, err := vdParseMountOpts([]string{"a.fdd", "hunter2"}); err != nil || o.Cred != (credArg{"bare", "hunter2"}) {
		t.Errorf("the sole trailing token: %+v %v", o, err)
	}
	if _, err := vdParseMountOpts([]string{"a.fdd", "ro", "hunter2"}); vdExitClass(err) != vdisk.ExitUsage || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("a bare password beside an option: %v", err)
	}

	dir := vdTestEnv(t)
	p := filepath.Join(dir, "a.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	before := vdTestHash(t, p)
	out := filepath.Join(dir, "out.fdd")
	for _, args := range [][]string{
		{"verify", p, "hunter2"},
		{"info", p, "p:hunter2"},
		{"compact", p, "hunter2"},
		{"grow", p, "2M", "hunter2"},
		{"grow", p, "hunter2"},
		{"export", p, filepath.Join(dir, "o.img"), "raw", "hunter2"},
		{"clone", p, out, "hunter2"},
		{"seal", p, out, "hunter2"},
		{"clone", p, "hunter2"},
		{"pass", p, "hunter2"},
		{"pass", p, "new", "hunter2"},
		{"format", p, "hunter2"},
		{"format", p, "fs", "ntfs", "hunter2"},
		{"chkdsk", p, "hunter2"},
		{"chkdsk", p, "fix", "hunter2"},
		{"destroy", p, "hunter2"},
		{"destroy", p, "p:hunter2"},
		{"verify", p, "p:a", "p:hunter2"},
	} {
		_, err := vdTestRun(t, true, args...)
		if vdExitClass(err) != vdisk.ExitUsage || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%v: class %d, %v", args[:1], vdExitClass(err), err)
		}
	}
	if vdTestHash(t, p) != before {
		t.Fatal("a refused line changed the container")
	}
	for _, f := range []string{out, filepath.Join(dir, "o.img")} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("a refused line wrote %s", f)
		}
	}
}

// verify: read-only; a clean container passes and says the data was read,
// not verified; damage is class 4.
func TestVD_Verify(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "v.fdd")
	vdTestContainer(t, p, 2<<20, vdisk.ProfilePlain, "")
	vdTestFill(t, p, "")
	before := vdTestHash(t, p)
	out, err := vdTestRun(t, true, "verify", p)
	if err != nil {
		t.Fatalf("a clean container: %v\n%s", err, out)
	}
	if !strings.Contains(out, "read, not verified") || !strings.Contains(out, "obfuscated, not encrypted") {
		t.Errorf("the report does not say what it proved:\n%s", out)
	}
	if vdTestHash(t, p) != before {
		t.Fatal("verify wrote to the container")
	}
	// The primary header overwritten: the backup carries it, and that is damage.
	b, _ := os.ReadFile(p)
	copy(b[:4096], bytes.Repeat([]byte{0x5A}, 4096))
	os.WriteFile(p, b, 0o600)
	out, err = vdTestRun(t, true, "vfy", p)
	if vdExitClass(err) != vdisk.ExitDamaged || !strings.Contains(out, "PROBLEM") {
		t.Fatalf("a damaged primary: class %d %v\n%s", vdExitClass(err), err, out)
	}
	// Both headers: nothing opens, and that is damage too, never a credential.
	copy(b[len(b)-4096:], bytes.Repeat([]byte{0xA5}, 4096))
	os.WriteFile(p, b, 0o600)
	if _, err = vdTestRun(t, true, "verify", p, "p:any"); vdExitClass(err) != vdisk.ExitDamaged {
		t.Fatalf("both headers damaged: class %d %v", vdExitClass(err), err)
	}
}

// T6.12: several containers and a mask, each reported, one failure does not
// abandon the rest, and the command ends with the first failure's class.
func TestVD_MultiTarget(t *testing.T) {
	dir := vdTestEnv(t)
	for _, n := range []string{"a", "b", "c"} {
		vdTestContainer(t, filepath.Join(dir, n+".fdd"), 1<<20, vdisk.ProfilePlain, "")
	}
	os.WriteFile(filepath.Join(dir, "note.txt"), []byte("x"), 0o600)
	out, err := vdTestRun(t, true, "info", filepath.Join(dir, "*.fdd"))
	if err != nil || strings.Count(out, "Container:") != 3 || !strings.Contains(out, "none failed") {
		t.Fatalf("info over a mask: %v\n%s", err, out)
	}
	// c is damaged beyond opening; a and b are still verified, and reported.
	c := filepath.Join(dir, "c.fdd")
	os.WriteFile(c, bytes.Repeat([]byte{1}, 64<<10), 0o600)
	out, err = vdTestRun(t, true, "verify", filepath.Join(dir, "a.fdd"), c, filepath.Join(dir, "b.fdd"))
	var many *vdManyError
	if vdExitClass(err) != vdisk.ExitDamaged || !errors.As(err, &many) || strings.Count(out, "no damage found") != 2 {
		t.Fatalf("one damaged of three: class %d %v\n%s", vdExitClass(err), err, out)
	}
	if !strings.Contains(err.Error(), "1 of 3") {
		t.Errorf("the failure does not count: %v", err)
	}
	if _, err := vdTestRun(t, true, "info", filepath.Join(dir, "a.fdd"), "hunter2"); vdExitClass(err) != vdisk.ExitUsage || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("a second word that is no container: %v", err)
	}
	if _, err := vdTestRun(t, true, "info", filepath.Join(dir, "zz*.fdd")); vdExitClass(err) != vdisk.ExitUsage {
		t.Errorf("a mask that matches nothing: %v", err)
	}
}

// export: raw and vhd are the volume's bytes; an existing destination is
// usage and stays as it was; the file tree is unsupported, class 6.
func TestVD_Export(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "e.fdd")
	vdTestContainer(t, p, 2<<20, vdisk.ProfilePlain, "")
	want := vdTestFill(t, p, "")
	before := vdTestHash(t, p)
	img := filepath.Join(dir, "e.img")
	if out, err := vdTestRun(t, true, "export", p, img, "raw"); err != nil {
		t.Fatalf("raw: %v\n%s", err, out)
	}
	if got, _ := os.ReadFile(img); !bytes.Equal(got, want) {
		t.Fatal("the raw image is not the volume")
	}
	vhd := filepath.Join(dir, "e.vhd")
	if _, err := vdTestRun(t, true, "ext", p, "vhd", "to", vhd); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(vhd)
	if len(got) != len(want)+512 || !bytes.Equal(got[:len(want)], want) || string(got[len(want):len(want)+8]) != "conectix" {
		t.Fatal("the vhd is not the volume and a fixed-VHD footer")
	}
	os.WriteFile(filepath.Join(dir, "taken.img"), []byte("mine"), 0o600)
	if _, err := vdTestRun(t, true, "export", p, filepath.Join(dir, "taken.img"), "raw"); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("an existing destination: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "taken.img")); string(b) != "mine" {
		t.Fatal("an existing destination was written")
	}
	tree := filepath.Join(dir, "tree")
	if _, err := vdTestRun(t, true, "export", p, tree); vdExitClass(err) != vdisk.ExitUnsupported || !strings.Contains(err.Error(), "raw") {
		t.Fatalf("the file tree: %v", err)
	}
	if _, err := os.Stat(tree); err == nil {
		t.Fatal("the refused tree export wrote something")
	}
	if vdTestHash(t, p) != before {
		t.Fatal("export changed the container")
	}
}

// compact and grow: the volume keeps its data; grow makes it larger only; a
// mounted container is busy, class 8, and is not touched.
func TestVD_CompactGrow(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "g.fdd")
	vdTestContainer(t, p, 2<<20, vdisk.ProfilePlain, "")
	want := vdTestFill(t, p, "")
	if out, err := vdTestRun(t, true, "compact", p); err != nil || !strings.Contains(out, "Compacted") {
		t.Fatalf("compact: %v\n%s", err, out)
	}
	if !bytes.Equal(vdTestVolume(t, p, ""), want) {
		t.Fatal("compact changed the volume")
	}
	out, err := vdTestRun(t, true, "resize", p, "4M")
	if err != nil || !strings.Contains(out, "Extend Volume") {
		t.Fatalf("grow: %v\n%s", err, out)
	}
	if i, _ := vdisk.Inspect(p); i.LogicalSize != 4<<20 {
		t.Fatalf("grown to %d", i.LogicalSize)
	}
	if got := vdTestVolume(t, p, ""); !bytes.Equal(got[:len(want)], want) || !bytes.Equal(got[len(want):], make([]byte, 2<<20)) {
		t.Fatal("grow changed the data or left the new part unzeroed")
	}
	for _, size := range []string{"4M", "1M"} {
		if _, err := vdTestRun(t, true, "grow", p, size); vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("grow to %s: %v", size, err)
		}
	}
	vdTestMounted(t, p)
	before := vdTestHash(t, p)
	for _, args := range [][]string{{"compact", p}, {"grow", p, "8M"}} {
		if _, err := vdTestRun(t, true, args...); vdExitClass(err) != vdisk.ExitBusy {
			t.Errorf("%s while mounted: %v", args[0], err)
		}
	}
	if vdTestHash(t, p) != before {
		t.Fatal("a refused verb changed a mounted container")
	}
}

// pass: the credential changes and the data key does not; a wrong old
// credential is class 3; an empty new one is refused with the file untouched;
// an obfuscated container has none to change.
func TestVD_Pass(t *testing.T) {
	if testing.Short() {
		t.Skip("real work factor")
	}
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "p.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfileVault, "old")
	want := vdTestFill(t, p, "old")
	out, err := vdTestRun(t, true, "pass", p, "p:old", "new", "p:new")
	if err != nil || !strings.Contains(out, "not rewritten") || !strings.Contains(out, "old credential") {
		t.Fatalf("pass: %v\n%s", err, out)
	}
	if !bytes.Equal(vdTestVolume(t, p, "new"), want) {
		t.Fatal("the new credential does not open the same volume")
	}
	before := vdTestHash(t, p)
	if _, err := vdTestRun(t, true, "pass", p, "p:old", "new", "p:x"); vdExitClass(err) != vdisk.ExitCredential {
		t.Fatalf("the old credential after the change: class %d %v", vdExitClass(err), err)
	}
	_, err = vdTestRun(t, true, "pass", p, "p:new", "new", "p:")
	if vdExitClass(err) != vdisk.ExitUsage || !strings.Contains(err.Error(), "clone <new.fdd> nopass") {
		t.Fatalf("an empty new credential: %v", err)
	}
	if vdTestHash(t, p) != before {
		t.Fatal("a refused pass changed the file")
	}
	o := filepath.Join(dir, "o.fdd")
	vdTestContainer(t, o, 1<<20, vdisk.ProfilePlain, "")
	if _, err := vdTestRun(t, true, "pass", o, "new", "p:x"); vdExitClass(err) != vdisk.ExitUsage || !strings.Contains(err.Error(), "obfuscated") {
		t.Fatalf("an obfuscated container: %v", err)
	}
	vdTestMounted(t, p)
	if _, err := vdTestRun(t, true, "pass", p, "p:new", "new", "p:x"); vdExitClass(err) != vdisk.ExitBusy {
		t.Fatalf("a mounted container: %v", err)
	}
	if vdTestHash(t, p) != before {
		t.Fatal("a refused pass changed the file")
	}
}

// clone and seal nopass: a copy of its own, the source byte-identical; an
// encrypted source's clone stays encrypted, and nopass makes it obfuscated -
// said so, and never called decrypted.
func TestVD_Clone(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "src.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfileFast, "")
	want := vdTestFill(t, p, "")
	before := vdTestHash(t, p)
	c := filepath.Join(dir, "c.fdd")
	out, err := vdTestRun(t, true, "clone", p, c)
	if err != nil || !strings.Contains(out, "is unchanged") || !strings.Contains(out, "New container id") {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	si, _ := vdisk.Inspect(p)
	ci, _ := vdisk.Inspect(c)
	if ci.ContainerID == si.ContainerID || ci.Profile != vdisk.ProfileFast || !ci.Obfuscated || !bytes.Equal(vdTestVolume(t, c, ""), want) {
		t.Fatalf("the clone: %+v", ci)
	}
	if _, err := vdTestRun(t, true, "clone", p, c); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("onto an existing file: %v", err)
	}
	if _, err := vdTestRun(t, true, "clone", p, filepath.Join(dir, "c.img")); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("a destination that is no .fdd: %v", err)
	}
	if vdTestHash(t, p) != before {
		t.Fatal("clone changed its source")
	}
	if testing.Short() {
		return
	}
	v := filepath.Join(dir, "v.fdd")
	vdTestContainer(t, v, 1<<20, vdisk.ProfileVault, "k")
	vwant := vdTestFill(t, v, "k")
	vbefore := vdTestHash(t, v)
	e := filepath.Join(dir, "e.fdd")
	if _, err := vdTestRun(t, true, "clone", v, e, "p:k"); err != nil {
		t.Fatal(err)
	}
	if i, _ := vdisk.Inspect(e); i.Obfuscated || i.Profile != vdisk.ProfileVault || !bytes.Equal(vdTestVolume(t, e, "k"), vwant) {
		t.Fatalf("the clone of a vault: %+v", i)
	}
	n := filepath.Join(dir, "n.fdd")
	out, err = vdTestRun(t, true, "clone", v, n, "nopass", "p:k")
	if err != nil || !strings.Contains(out, vdNopassNote) {
		t.Fatalf("clone nopass: %v\n%s", err, out)
	}
	for _, w := range []string{"decrypt", "unprotected"} {
		if strings.Contains(strings.ToLower(out), w) {
			t.Errorf("nopass says %q:\n%s", w, out)
		}
	}
	if i, _ := vdisk.Inspect(n); !i.Obfuscated || i.Profile != vdisk.ProfilePlain || !bytes.Equal(vdTestVolume(t, n, ""), vwant) {
		t.Fatalf("the nopass clone: %+v", i)
	}
	s := filepath.Join(dir, "s.fdd")
	if out, err := vdTestRun(t, true, "seal", v, s, "p:k", "NOPASS"); err != nil || !strings.Contains(out, vdNopassNote) {
		t.Fatalf("seal nopass: %v\n%s", err, out)
	}
	if i, _ := vdisk.Inspect(s); !i.Obfuscated || i.Profile != vdisk.ProfileSealed {
		t.Fatalf("the nopass seal: %+v", i)
	}
	if vdTestHash(t, v) != vbefore {
		t.Fatal("a copy changed its encrypted source")
	}
}

// T6.13, destroy: force removes the file; a batch without force is refused
// and the file stays; a mounted container is busy whatever the options; a
// registered container is forgotten with its file; wipe goes through the
// single-file wipe.
func TestVD_DestructiveChecks(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "d.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	if _, err := vdTestRun(t, true, "destroy", p); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("a batch without force: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("a refused destroy removed the file")
	}
	if _, err := vdTestRun(t, true, "destroy", filepath.Join(dir, "d.txt"), "force"); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("a file that is no .fdd: %v", err)
	}
	captureStdout(t, func() {
		if err := vdAdd([]string{p, "as", "doomed"}); err != nil {
			t.Fatal(err)
		}
	})
	vdTestMounted(t, p)
	if _, err := vdTestRun(t, true, "destroy", p, "force", "wipe"); vdExitClass(err) != vdisk.ExitBusy {
		t.Fatalf("a mounted container: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal("a mounted container was removed")
	}
	vdUpdateState(func(s *vdState) error { s.Mounts = nil; return nil })
	out, err := vdTestRun(t, true, "erase", "doomed", "-y")
	if err != nil || !strings.Contains(out, "Forgot the registered name doomed") {
		t.Fatalf("destroy by name: %v\n%s", err, out)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("destroy force left the file")
	}
	if r, _ := vdLoadRegistry(); len(r.Containers) != 0 {
		t.Fatalf("the registered name survived: %+v", r.Containers)
	}
	w := filepath.Join(dir, "w.fdd")
	vdTestContainer(t, w, 1<<20, vdisk.ProfilePlain, "")
	out, err = vdTestRun(t, true, "destroy", w, "wipe", "force")
	if err != nil || !strings.Contains(out, "Overwritten and removed") || !strings.Contains(out, "does not guarantee erasure") {
		t.Fatalf("destroy wipe: %v\n%s", err, out)
	}
	if _, err := os.Stat(w); err == nil {
		t.Fatal("destroy wipe left the file")
	}
	// A file that does not read as a container is still removed when named.
	j := filepath.Join(dir, "junk.fdd")
	os.WriteFile(j, []byte("not a container"), 0o600)
	if out, err := vdTestRun(t, true, "destroy", j, "force"); err != nil || !strings.Contains(out, "does not read as a container") {
		t.Fatalf("a damaged container: %v\n%s", err, out)
	}
}

// T6.13, format: the refusals that come before any elevation - mounted (8, no
// option skips it), a batch without force (2), a sealed container (6), and a
// batch that is not elevated (7). The format itself needs the initiator and
// administrator rights and is the manual kit's.
func TestVD_FormatRefusals(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "f.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	s := filepath.Join(dir, "s.fdd")
	if err := vdisk.Seal(context.Background(), p, s, nil); err != nil {
		t.Fatal(err)
	}
	before := vdTestHash(t, p)
	if _, err := vdTestRun(t, true, "format", p); vdExitClass(err) != vdisk.ExitUsage {
		t.Fatalf("a batch without force: %v", err)
	}
	if _, err := vdTestRun(t, true, "format", s, "force"); vdExitClass(err) != vdisk.ExitUnsupported {
		t.Fatalf("a sealed container: %v", err)
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		if _, err := vdTestRun(t, true, "format", p, "force"); vdExitClass(err) != vdExitTransport {
			t.Fatalf("a batch that is not elevated: %v", err)
		}
	}
	vdTestMounted(t, p)
	for _, args := range [][]string{{"format", p}, {"format", p, "force"}, {"format", p, "-y", "fs", "exfat"}} {
		if _, err := vdTestRun(t, false, args...); vdExitClass(err) != vdisk.ExitBusy {
			t.Errorf("%v while mounted: %v", args[2:], err)
		}
	}
	if vdTestHash(t, p) != before {
		t.Fatal("a refused format changed the container")
	}
}

// T6.26: inside a package every verb that needs the mount path is class 6
// with one sentence; the file verbs keep working.
func TestVD_Packaged(t *testing.T) {
	dir := vdTestEnv(t)
	p := filepath.Join(dir, "k.fdd")
	vdTestContainer(t, p, 1<<20, vdisk.ProfilePlain, "")
	was := vdPackaged
	vdPackaged = func() bool { return true }
	defer func() { vdPackaged = was }()
	for _, args := range [][]string{
		{"mount", p}, {"unmount", p}, {"unmount", "X:"}, {"save", p}, {"format", p, "force"}, {"auto", "work", "logon"}, {"guard", "status"}, {"mount", filepath.Join(dir, "d.vhdx")},
	} {
		_, err := vdTestRun(t, false, args...)
		if vdExitClass(err) != vdisk.ExitUnsupported || !strings.Contains(err.Error(), "Microsoft Store build") || strings.Count(err.Error(), ". ") > 0 {
			t.Errorf("%v in a package: class %d %v", args, vdExitClass(err), err)
		}
	}
	for _, args := range [][]string{
		{"info", p}, {"verify", p}, {"export", p, filepath.Join(dir, "k.img"), "raw"}, {"compact", p}, {"grow", p, "2M"},
		{"clone", p, filepath.Join(dir, "c.fdd")}, {"seal", p, filepath.Join(dir, "s.fdd")}, {"list"}, {"status"},
	} {
		if _, err := vdTestRun(t, false, args...); err != nil {
			t.Errorf("%v in a package: %v", args[:1], err)
		}
	}
	if _, err := vdTestRun(t, true, "destroy", p, "force"); err != nil {
		t.Errorf("destroy in a package: %v", err)
	}
	vdPackaged = func() bool { return false }
	if _, err := vdTestRun(t, false, "verify", filepath.Join(dir, "c.fdd")); err != nil {
		t.Error(err)
	}
}

// The Store build lists, creates and opens no partition disk (SP-0148 D8):
// vd disks prints the Store sentence and succeeds, its json says unavailable,
// and every partition verb - and every verb on a partition disk's name or
// locator - is class 6 with that sentence, before any disk is read.
func TestVD_PackagedRefusesPartitionDisks(t *testing.T) {
	dir := vdTestEnv(t)
	const guid = "0B9A6F1E-3C4D-4E5F-8A9B-0C1D2E3F4A5B"
	loc := vdLocator(guid)
	adoptLoc := vdLocator("7C2E5A10-1B2C-4D3E-9F80-112233445566")
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		r.Containers = append(r.Containers, vdRegEntry{Name: "pwork", Path: loc, Carrier: "partition", Profile: "fast",
			Part: &vdPartRecord{Locator: loc, DiskGUID: "1A2B3C4D-0000-4000-8000-00000000AA01", PartitionGUID: guid, Offset: 1 << 20, Length: 64 << 20}})
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	was := vdPackaged
	vdPackaged = func() bool { return true }
	defer func() { vdPackaged = was }()

	out, err := vdTestRun(t, false, "disks")
	if err != nil || strings.TrimSpace(out) != vdPartStoreSentence {
		t.Errorf("vd disks in a package: %v, printed %q", err, out)
	}
	out, err = vdTestRun(t, false, "disks", "json")
	var wire struct {
		Schema    string            `json:"schema"`
		Version   int               `json:"version"`
		Available bool              `json:"available"`
		Reason    string            `json:"reason"`
		Disks     []json.RawMessage `json:"disks"`
	}
	if err != nil || json.Unmarshal([]byte(out), &wire) != nil || wire.Schema != vdDisksSchema || wire.Version != vdDisksVersion ||
		wire.Available || wire.Reason != "store-build" || wire.Disks == nil || len(wire.Disks) != 0 {
		t.Errorf("vd disks json in a package: %v, printed %q", err, out)
	}

	img := filepath.Join(dir, "img.fdd")
	for _, args := range [][]string{
		{"new", "part", "2", "size", "max", "force"},
		{"new", "part", "disk:{1A2B3C4D-0000-4000-8000-00000000AA01}", "size", "64M", "as", "x", "force"},
		{"image", "pwork", "to", img}, {"image", loc, "to", img, "force"},
		{"adopt", adoptLoc}, {"adopt", adoptLoc, "as", "found"},
		// The registered name and the locator, on the verbs routed to the partition forms.
		{"info", "pwork"}, {"verify", "pwork"}, {"export", "pwork", filepath.Join(dir, "p.img"), "raw"},
		{"clone", "pwork", filepath.Join(dir, "c.fdd")}, {"seal", "pwork", filepath.Join(dir, "s.fdd")},
		{"pass", "pwork"}, {"destroy", "pwork", "force"}, {"destroy", loc, "wipe", "force"},
		{"compact", "pwork"}, {"grow", loc, "1G"}, {"share", "pwork", "on"},
		{"info", loc}, {"verify", loc},
		// An unregistered locator is refused the same way, not as "register it first".
		{"info", adoptLoc}, {"destroy", adoptLoc, "force"},
	} {
		_, err := vdTestRun(t, true, args...)
		if vdExitClass(err) != vdisk.ExitUnsupported || !errors.Is(err, errVdPartPackaged) || !strings.Contains(err.Error(), vdPartStoreSentence) {
			t.Errorf("%v in a package: class %d %v", args, vdExitClass(err), err)
		}
	}
	// mount and format need the transport, which the package does not carry at
	// all: the earlier, general Store refusal answers, class 6 all the same.
	for _, args := range [][]string{{"mount", "pwork"}, {"mount", loc, "ro"}, {"format", "pwork", "force"}} {
		_, err := vdTestRun(t, true, args...)
		if vdExitClass(err) != vdisk.ExitUnsupported || !strings.Contains(err.Error(), "Microsoft Store") {
			t.Errorf("%v in a package: class %d %v", args, vdExitClass(err), err)
		}
	}
	for _, p := range []string{img, filepath.Join(dir, "p.img"), filepath.Join(dir, "c.fdd"), filepath.Join(dir, "s.fdd")} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("a refused verb wrote %s", p)
		}
	}
	// forget is bookkeeping, not a partition open: the Store build may still
	// drop a name a desktop build registered.
	if _, err := vdTestRun(t, true, "forget", "pwork"); err != nil {
		t.Errorf("forget of a partition disk in a package: %v", err)
	}
}

// A verb that takes no subject refuses a word after it without quoting it:
// the word may be a password, and the refusal reaches history.json.
func TestVD_SubjectlessVerbsRefuseAWord(t *testing.T) {
	vdTestEnv(t)
	for _, verb := range []string{"list", "ls", "status", "stop"} {
		_, err := vdTestRun(t, false, verb, "hunter2")
		if vdExitClass(err) != vdisk.ExitUsage {
			t.Fatalf("vd %s <word>: %v", verb, err)
		}
		if strings.Contains(err.Error(), "hunter2") {
			t.Fatalf("vd %s quoted the word back: %v", verb, err)
		}
	}
}
