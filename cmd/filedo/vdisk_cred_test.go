package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filedo/fdsec"
	"filedo/vdisk"
)

// SP-0004 P5: the credential path of the vd surface. These derive under the
// real kdf_params_id 1 (256 MiB), so they keep to a handful of derivations.

// T5.6: a keyfile credential is the digest k: already produces for the
// sibling; one changed byte is another credential; a missing keyfile is I/O,
// never a wrong credential.
func TestVD_Keyfile(t *testing.T) {
	if testing.Short() {
		t.Skip("real work factor")
	}
	dir := t.TempDir()
	key := filepath.Join(dir, "key.bin")
	if err := os.WriteFile(key, []byte("the keyfile's bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	cred, err := resolveCredential(credArg{"k", key}, false, "")
	if err != nil {
		t.Fatal(err)
	}
	sib, err := fdsecCredentialFromFile(key, true)
	if err != nil || string(sib) != string(cred) || len(cred) != 64 {
		t.Fatalf("the keyfile credential differs from the sibling's k: (%q vs %q, %v)", cred, sib, err)
	}
	path := filepath.Join(dir, "v.fdd")
	c, err := vdisk.Create(context.Background(), vdisk.CreateOptions{Path: path, LogicalSize: 1 << 20, ClusterShift: 16, Profile: vdisk.ProfileVault, Credential: cred, Keyfile: true})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := os.WriteFile(key, []byte("the keyfile's byteS"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, _ := resolveCredential(credArg{"k", key}, false, "")
	_, err = vdisk.Open(context.Background(), path, changed, vdisk.OpenRead)
	err = vdCredentialErr(err, credArg{"k", key})
	if vdExitClass(err) != vdisk.ExitCredential || vdExplain(err) != "The credential did not open this container.\nKeyfile: "+key {
		t.Fatalf("a changed keyfile: class %d, %q", vdExitClass(err), vdExplain(err))
	}
	_, err = resolveCredential(credArg{"k", filepath.Join(dir, "gone.bin")}, false, "")
	if vdExitClass(err) != vdisk.ExitIO {
		t.Fatalf("a missing keyfile: class %d (%v), want 5", vdExitClass(err), err)
	}
}

// T5.11 and FDD-BEHAVIOUR 7 rule 3: vd new .. vault without a credential is
// refused with the reason; with one it is encrypted; a credential on another
// profile makes it encrypted too; an empty one given on purpose selects
// obfuscation and says so.
func TestVD_NewCredential(t *testing.T) {
	if testing.Short() {
		t.Skip("real work factor")
	}
	dir := t.TempDir()
	// No credential and no terminal: the usage class, before any file.
	v0 := filepath.Join(dir, "v0.fdd")
	out := captureStdout(t, func() {
		err := vdNew([]string{v0, "16M", "vault"})
		if vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("vault, no credential, no terminal: %v", err)
		}
	})
	if !strings.Contains(out, vdVaultWarning) {
		t.Errorf("the vault warning was not said before the prompt:\n%s", out)
	}
	if _, err := os.Stat(v0); err == nil {
		t.Error("a refused vault left a file")
	}
	// An empty credential is never a vault.
	if err := vdNew([]string{filepath.Join(dir, "v1.fdd"), "16M", "vault", "p:"}); !errors.Is(err, vdisk.ErrVaultNeedsCredential) {
		t.Errorf("vault with p: empty: %v", err)
	}
	// A vault with a credential.
	v2 := filepath.Join(dir, "v2.fdd")
	out = captureStdout(t, func() {
		if err := vdNew([]string{v2, "16M", "vault", "p:test"}); err != nil {
			t.Error(err)
		}
	})
	if i, err := vdisk.Inspect(v2); err != nil || i.Obfuscated || i.Profile != vdisk.ProfileVault {
		t.Fatalf("vault: %+v %v", i, err)
	}
	if !strings.Contains(out, "Encrypted: the file is unreadable without the credential.") {
		t.Errorf("the encrypted limit was not printed:\n%s", out)
	}
	// ram with a credential: the paging limit.
	r := filepath.Join(dir, "r.fdd")
	out = captureStdout(t, func() {
		if err := vdNew([]string{r, "16M", "ram", "p:test"}); err != nil {
			t.Error(err)
		}
	})
	if i, _ := vdisk.Inspect(r); i.Obfuscated || !strings.Contains(out, "page file") {
		t.Errorf("ram with a credential: obfuscated=%v\n%s", i.Obfuscated, out)
	}
	// plain with an empty credential given on purpose: obfuscated, and said.
	p := filepath.Join(dir, "p.fdd")
	out = captureStdout(t, func() {
		if err := vdNew([]string{p, "16M", "p:"}); err != nil {
			t.Error(err)
		}
	})
	if i, _ := vdisk.Inspect(p); !i.Obfuscated || !strings.Contains(out, "empty credential selects obfuscation only") {
		t.Errorf("plain with p: empty: obfuscated=%v\n%s", i.Obfuscated, out)
	}
	// A seal of the vault needs its credential and keeps it.
	s := filepath.Join(dir, "s.fdd")
	if err := vdSeal([]string{v2, s, "p:Test"}); vdExitClass(err) != vdisk.ExitCredential || vdExplain(err) != "The credential did not open this container." {
		t.Fatalf("seal under a wrong credential: class %d, %q", vdExitClass(err), vdExplain(err))
	}
	captureStdout(t, func() {
		if err := vdSeal([]string{v2, s, "p:test"}); err != nil {
			t.Error(err)
		}
	})
	if c, err := vdisk.Open(context.Background(), s, fdsec.NewCredential("test"), vdisk.OpenRead); err != nil {
		t.Fatalf("the sealed vault: %v", err)
	} else {
		c.Close()
	}
}

// No refusal of vd new quotes back a word that might be a password.
func TestVD_NewNeverQuotesAWord(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{filepath.Join(dir, "a.fdd"), "hunter2"},
		{filepath.Join(dir, "a.fdd"), "16M", "hunter2"},
		{filepath.Join(dir, "a.fdd"), "16M", "p:x", "p:hunter2"},
	} {
		err := vdNew(args)
		if err == nil || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("%v: %v", args, err)
		}
	}
}

// Security review of S5: a credential in a path slot or an option value never
// reaches an error message, which history.json records.
func TestVD_NoCredentialInErrors(t *testing.T) {
	dir := t.TempDir()
	v := filepath.Join(dir, "v.fdd")
	for _, args := range [][]string{
		{"seal", "p:hunter2", v},
		{"info", "p:hunter2"},
		{"new", "p:hunter2", "20G"},
		{"mount", "p:hunter2", v},
	} {
		err := runVd(args, NewHistoryLogger(nil), true)
		if err == nil || strings.Contains(err.Error(), "hunter2") || vdExitClass(err) != vdisk.ExitUsage {
			t.Errorf("%v: %v", args, err)
		}
	}
	if err := vdSeal([]string{v, "p:hunter2"}); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("seal into p:hunter2: %v", err)
	}
	if _, err := vdParseMountOpts([]string{v, "as", "p:hunter2"}); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("as p:hunter2: %v", err)
	}
	// An empty password file and an over-long credential are usage.
	empty := filepath.Join(dir, "empty.txt")
	os.WriteFile(empty, []byte("\r\n"), 0o600)
	if _, err := vdResolveCredential(credArg{"pf", empty}, false); vdExitClass(err) != vdisk.ExitUsage {
		t.Errorf("empty pf: %v", err)
	}
	if _, err := vdResolveCredential(credArg{"p", strings.Repeat("x", vdMaxCredential+1)}, false); vdExitClass(err) != vdisk.ExitUsage {
		t.Errorf("long credential: %v", err)
	}
}
