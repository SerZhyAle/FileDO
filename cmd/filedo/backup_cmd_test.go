package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRestoreNeverRemovesExistingPartial(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "backup.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.Create("file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("archive")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "restore")
	partial := dest + ".filedo-partial"
	if err := os.Mkdir(partial, 0o755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(partial, "keep.txt")
	if err := os.WriteFile(marker, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runRestore(&backupOpts{src: archive, to: dest, haveTo: true}, &HistoryLogger{}); err == nil {
		t.Fatal("restore accepted an existing partial folder")
	}
	if b, err := os.ReadFile(marker); err != nil || string(b) != "keep me" {
		t.Fatalf("existing partial changed: %q, %v", b, err)
	}
}

// TestBackupArgsParse holds the grammar of spec 3.3 and the refusals that
// keep the two verbs' words apart.
func TestBackupArgsParse(t *testing.T) {
	o, err := parseBackupArgs("backup", []string{`D:\Photos`, "to", `E:\Bak`, "level", "9", "sec", "keep-zip", "skip-links", "pe:BAK_PW", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if o.src != `D:\Photos` || o.to != `E:\Bak` || o.level != 9 || !o.sec || !o.keepZip || !o.skipLinks || !o.yes || o.cred.src != "pe" || o.cred.val != "BAK_PW" {
		t.Fatalf("parsed %+v", o)
	}
	if o, err := parseBackupArgs("backup", []string{`k:\data`}); err != nil || o.src != `k:\data` || o.cred.given() {
		t.Fatalf("a drive K: source was read as a keyfile: %+v %v", o, err)
	}
	if o, err := parseBackupArgs("backup", []string{`p:\data`}); err != nil || o.src != `p:\data` {
		t.Fatalf("a drive P: source was read as a password: %+v %v", o, err)
	}
	if o, err := parseBackupArgs("restore", []string{"b.fd-sec", "k:x.key", "suffix", "del"}); err != nil || o.cred.src != "k" || !o.suffix || !o.del {
		t.Fatalf("restore words: %+v %v", o, err)
	}
	refused := [][]string{
		{`D:\x`, "level", "10"},        // outside 0-9
		{`D:\x`, "keep-zip"},           // keep-zip needs sec
		{`D:\x`, "p:pw"},               // a password needs sec
		{`D:\x`, "suffix"},             // a restore word
		{`D:\x`, "hunter2"},            // a bare word that may be a password
		{`D:\x`, "sec", "p:a", "pe:B"}, // two password sources
	}
	for _, args := range refused {
		if _, err := parseBackupArgs("backup", args); err == nil {
			t.Errorf("backup %q was accepted", args)
		} else if strings.Contains(err.Error(), "hunter2") {
			t.Errorf("the refusal quotes the word that may be a password: %v", err)
		}
	}
	if _, err := parseBackupArgs("backup", nil); err == nil {
		t.Error("backup with no source was accepted")
	}
	for _, args := range [][]string{{"a.zip", "sec"}, {"a.zip", "root"}, {"a.zip", "level", "1"}} {
		if _, err := parseBackupArgs("restore", args); err == nil {
			t.Errorf("restore %q was accepted", args)
		}
	}
}

func TestBackupZipPath(t *testing.T) {
	for _, c := range []struct {
		path   string
		sealed bool
		want   string
	}{
		{"copy", false, "copy.zip"},
		{"copy.ZIP", true, "copy.ZIP"},
		{"copy.fd-sec", true, "copy.zip"},
	} {
		got, err := backupZipPath(c.path, c.sealed)
		if err != nil || got != c.want {
			t.Errorf("backupZipPath(%q, %v) = %q, %v; want %q", c.path, c.sealed, got, err, c.want)
		}
	}
	for _, path := range []string{"copy.txt", "copy.fd-sec"} {
		if _, err := backupZipPath(path, false); err == nil {
			t.Errorf("accepted plain backup filename %q", path)
		}
	}
}

// TestBackupRedaction: a backup or restore line keeps its paths and words and
// never its password (FDSEC-BEHAVIOUR section 8.2, CLI-EVENT-STREAM rule 13);
// `sec` after backup is an option, not the fdsec verb.
func TestBackupRedaction(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"backup", `D:\Photos`, "sec", "p:hunter2"}, []string{"backup", `D:\Photos`, "sec", "p:***"}},
		{[]string{"filedo.exe", "-backup", `D:\Photos`, "to", `E:\Bak`, "level", "6", "sec", "pe:PW", "y"},
			[]string{"filedo.exe", "-backup", `D:\Photos`, "to", `E:\Bak`, "level", "6", "sec", "pe:PW", "y"}},
		{[]string{"backup", `D:\Photos`, "sec", "hunter2"}, []string{"backup", `D:\Photos`, "sec", "***"}},
		{[]string{"backup", `D:\Photos`, "level", "hunter2"}, []string{"backup", `D:\Photos`, "level", "***"}},
		{[]string{"restore", `C:\b.fd-sec`, "to", `D:\`, "p:hunter2", "suffix"}, []string{"restore", `C:\b.fd-sec`, "to", `D:\`, "p:***", "suffix"}},
		{[]string{"restore", "p:hunter2", `C:\b.fd-sec`}, []string{"restore", "p:***", `C:\b.fd-sec`}},
	}
	for _, c := range cases {
		if got := redactCredentialArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("redact %q = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBackupVerbsDispatch: both verbs and their dashed spellings reach the
// one dispatch (D1).
func TestBackupVerbsDispatch(t *testing.T) {
	for w, want := range map[string]string{"backup": "backup", "-backup": "backup", "RESTORE": "restore", "-restore": "restore"} {
		if got := verbOf(w); got != want {
			t.Errorf("verbOf(%q) = %q, want %q", w, got, want)
		}
	}
}
