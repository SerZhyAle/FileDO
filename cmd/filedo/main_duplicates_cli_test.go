package main

// Black-box tests for the duplicate finder's command surface (SP-0028 section
// 3). They drive the exe TestMain in fdsec_cli_test.go builds, with stdin on
// NUL - what the GUI's child process and any automation see - and every file
// they could delete lives in t.TempDir().
//
// Run them with:  go test ./cmd/filedo/ -count=1 -vet=off -run Duplicates

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"filedo/fileduplicates"
	"filedo/statedir"

	"golang.org/x/sys/windows"
)

// runDupCLI runs filedo with stdin on NUL and a state root of its own. extra
// entries in env override the inherited environment.
func runDupCLI(t *testing.T, env []string, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(filedoExe, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = append(os.Environ(), statedir.EnvOverride+"="+t.TempDir(), "FILEDO_NO_UI=1")
	cmd.Env = append(cmd.Env, env...)
	nul, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer nul.Close()
	cmd.Stdin = nul
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running filedo %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

var dupBase = time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)

// dupCopies writes three identical files, a.bin created first and c.bin
// last, with one shared modification time. The default rule keeps c.bin.
func dupCopies(t *testing.T) (dir string, paths []string) {
	t.Helper()
	dir = t.TempDir()
	data := bytes.Repeat([]byte("duplicate payload "), 2000)
	for i, n := range []string{"a.bin", "b.bin", "c.bin"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		dupSetCreated(t, p, dupBase.Add(time.Duration(i)*time.Hour), dupBase)
		paths = append(paths, p)
	}
	return dir, paths
}

func dupSetCreated(t *testing.T, path string, created, modified time.Time) {
	t.Helper()
	p, _ := windows.UTF16PtrFromString(path)
	h, err := windows.CreateFile(p, windows.FILE_WRITE_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)
	c := windows.NsecToFiletime(created.UnixNano())
	m := windows.NsecToFiletime(modified.UnixNano())
	if err := windows.SetFileTime(h, &c, nil, &m); err != nil {
		t.Fatal(err)
	}
}

func survivors(paths []string) int {
	n := 0
	for _, p := range paths {
		if exists(p) {
			n++
		}
	}
	return n
}

// DUP-03: with stdin on NUL and no -y, a deleting or moving run exits 2 and
// touches nothing - whatever the order of the words.
func TestDuplicatesCLI_DeleteNeedsYesWithoutConsole(t *testing.T) {
	dir, paths := dupCopies(t)
	target := filepath.Join(t.TempDir(), "moved")
	for _, words := range [][]string{
		{"cd", "del"},
		{"cd", "new", "del"}, {"cd", "del", "new"},
		{"cd", "old", "del"}, {"cd", "del", "old"},
		{"cd", "xyz", "del"}, {"cd", "del", "xyz"},
		{"cd", "move", target}, {"cd", "abc", "move", target},
	} {
		out, code := runDupCLI(t, nil, append([]string{dir}, words...)...)
		if code != 2 {
			t.Errorf("%v: exit %d, want 2\n%s", words, code, out)
		}
		if !strings.Contains(out, "-y") {
			t.Errorf("%v: the refusal does not name -y\n%s", words, out)
		}
	}
	if survivors(paths) != 3 {
		t.Fatal("a refused run deleted a file")
	}
	if exists(target) {
		t.Fatal("a refused move created its target")
	}
}

// DUP-03: -y is the consent, wherever it stands.
func TestDuplicatesCLI_DeleteWithYes(t *testing.T) {
	for _, words := range [][]string{{"cd", "del", "-y"}, {"cd", "-y", "old", "del"}, {"cd", "--yes", "del"}} {
		dir, paths := dupCopies(t)
		wd := t.TempDir()
		events := filepath.Join(wd, "events.jsonl")
		args := append([]string{"--events", events, dir}, words...)
		out, code := runDupCLI(t, nil, args...)
		if code != 0 {
			t.Fatalf("%v: exit %d\n%s", words, code, out)
		}
		if survivors(paths) != 1 || !exists(paths[2]) {
			t.Fatalf("%v: want only the newest copy (c.bin) left\n%s", words, out)
		}
		result := lastResult(t, events)
		numbers, _ := result["numbers"].(map[string]interface{})
		if numbers["deleted"] != float64(2) {
			t.Errorf("%v: result numbers %v, want deleted=2", words, numbers)
		}
	}
}

// DUP-03: the same from a batch file, which is a separate dispatch.
func TestDuplicatesCLI_BatchFile(t *testing.T) {
	dir, paths := dupCopies(t)
	lst := filepath.Join(t.TempDir(), "cmds.lst")
	if err := os.WriteFile(lst, []byte(dir+" cd del\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := runDupCLI(t, nil, "from", lst); code != 2 || survivors(paths) != 3 {
		t.Fatalf("batch without -y: exit %d, %d left\n%s", code, survivors(paths), out)
	}
	if err := os.WriteFile(lst, []byte(dir+" cd del -y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, code := runDupCLI(t, nil, "from", lst); code != 0 || survivors(paths) != 1 {
		t.Fatalf("batch with -y: exit %d, %d left\n%s", code, survivors(paths), out)
	}
}

// DUP-04: a delete that fails ends the run as could-not-verify, exit 2.
func TestDuplicatesCLI_FailedDeleteExits2(t *testing.T) {
	dir, paths := dupCopies(t)
	// Go opens without FILE_SHARE_DELETE, so while this handle is open the
	// child's delete of a.bin fails with a sharing violation.
	held, err := os.Open(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	wd := t.TempDir()
	events := filepath.Join(wd, "events.jsonl")
	out, code := runDupCLI(t, nil, "--events", events, dir, "cd", "del", "-y")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
	if !exists(paths[0]) || exists(paths[1]) || !exists(paths[2]) {
		t.Fatalf("want a.bin (held) and c.bin (kept) left, b.bin deleted\n%s", out)
	}
	if v := lastResult(t, events)["verdict"]; v != "Not proven" {
		t.Errorf("verdict %v, want Not proven", v)
	}
}

// DUP-16: a device that is not there is an error, not "no duplicates".
func TestDuplicatesCLI_MissingRootExits2(t *testing.T) {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		t.Fatal(err)
	}
	letter := ""
	for l := 'Z'; l >= 'D'; l-- {
		if mask&(1<<uint(l-'A')) == 0 {
			letter = string(l) + ":"
			break
		}
	}
	if letter == "" {
		t.Skip("no free drive letter to name a missing device")
	}
	out, code := runDupCLI(t, nil, "device", letter, "cd")
	if code != 2 {
		t.Errorf("device %s cd: exit %d, want 2\n%s", letter, code, out)
	}
	missing := filepath.Join(t.TempDir(), "no-such-folder")
	if out, code := runDupCLI(t, nil, "folder", missing, "cd"); code != 2 {
		t.Errorf("missing folder: exit %d, want 2\n%s", code, out)
	}
}

// DUP-06: a protected place needs a typed confirmation that -y does not give;
// with stdin on NUL the run is refused and nothing is touched. The stand-in
// is the system TEMP folder, pointed at a scratch folder for the child alone
// (not Program Files: WOW64 resets %ProgramFiles% in a 32-bit child).
func TestDuplicatesCLI_ProtectedRootRefusedEvenWithYes(t *testing.T) {
	dir, paths := dupCopies(t)
	out, code := runDupCLI(t, []string{"TEMP=" + dir, "TMP=" + dir}, dir, "cd", "del", "-y")
	if code != 2 {
		t.Fatalf("exit %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "TEMP") {
		t.Errorf("the refusal does not name the place\n%s", out)
	}
	if survivors(paths) != 3 {
		t.Fatal("a file in a protected place was deleted without a confirmation")
	}
}

// A typo in a rule word must not quietly become the default rule.
func TestDuplicatesCLI_UnknownWordExits2(t *testing.T) {
	dir, paths := dupCopies(t)
	out, code := runDupCLI(t, nil, dir, "cd", "del", "nwe", "-y")
	if code != 2 || survivors(paths) != 3 {
		t.Fatalf("exit %d, %d left\n%s", code, survivors(paths), out)
	}
}

// AUD-12-F1: a candidate that cannot be read for hashing is counted in the
// "could not be read" warning, not dropped in silence. The share-none handle
// lets the scan's identity probe (attributes only) through but not the hash.
func TestDuplicatesCLI_UnhashableFileIsReported(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte("duplicate payload "), 2000)
	a, b := filepath.Join(dir, "a.bin"), filepath.Join(dir, "b.bin")
	for _, p := range []string{a, b} {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := windows.UTF16PtrFromString(b)
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(h)

	out, _ := runDupCLI(t, nil, dir, "cd")
	if !strings.Contains(out, "1 files or folders could not be read") {
		t.Fatalf("a file the scan could not hash was left out without a warning\n%s", out)
	}
}

// AUD-12-F2: a list that was asked for and cannot be written refuses a
// deleting or moving run before it touches anything.
func TestDuplicatesCLI_ListWriteFailureStopsDelete(t *testing.T) {
	for _, verb := range []string{"del", "move"} {
		dir, paths := dupCopies(t)
		list := filepath.Join(t.TempDir(), "no-such-folder", "dups.lst")
		args := []string{dir, "cd"}
		target := ""
		if verb == "move" {
			target = filepath.Join(t.TempDir(), "moved")
			args = append(args, "move", target)
		} else {
			args = append(args, "del")
		}
		args = append(args, "-y", "list", list)
		out, code := runDupCLI(t, nil, args...)
		if code != 2 {
			t.Errorf("%s: exit %d, want 2\n%s", verb, code, out)
		}
		if survivors(paths) != 3 {
			t.Errorf("%s: %d of 3 files left after the list could not be written\n%s", verb, survivors(paths), out)
		}
		if target != "" {
			if entries, _ := os.ReadDir(target); len(entries) != 0 {
				t.Errorf("move: %d files moved after the list could not be written\n%s", len(entries), out)
			}
		}
	}
}

// DUP-10: --help, the README modifier table and the parser say the same thing
// about every rule word, and -y is documented in the help and in every README
// locale (theme T10).
func TestDuplicatesHelpAgreesWithReadmeAndCode(t *testing.T) {
	root := repoRoot(t)
	readme := readSurface(t, root, "README.md")
	kept := map[fileduplicates.DuplicateSelectionMode]string{
		fileduplicates.NewestAsOriginal:     "newest",
		fileduplicates.OldestAsOriginal:     "oldest",
		fileduplicates.LastAlphaAsOriginal:  "alphabetically last",
		fileduplicates.FirstAlphaAsOriginal: "alphabetically first",
	}
	keepPhrase := regexp.MustCompile(`(?i)keep (?:the )?(newest|oldest|alphabetically last|alphabetically first)`)
	for _, word := range []string{"old", "new", "abc", "xyz"} {
		want := kept[fileduplicates.ParseArguments([]string{word}).SelectionMode]

		helpLine := regexp.MustCompile(`(?m)^\s+` + word + ` → (.*)$`).FindStringSubmatch(usage)
		if helpLine == nil {
			t.Errorf("--help has no rule line for %q", word)
			continue
		}
		if m := keepPhrase.FindStringSubmatch(helpLine[1]); m == nil || strings.ToLower(m[1]) != want {
			t.Errorf("--help says %q for %s; the code keeps the %s copy", helpLine[1], word, want)
		}

		row := regexp.MustCompile("(?m)^\\| `" + word + "` \\| ([^|]*)\\|").FindStringSubmatch(readme)
		if row == nil {
			t.Errorf("README.md has no modifier row for %q", word)
			continue
		}
		if m := keepPhrase.FindStringSubmatch(row[1]); m == nil || strings.ToLower(m[1]) != want {
			t.Errorf("README.md says %q for %s; the code keeps the %s copy", row[1], word, want)
		}
	}

	if !strings.Contains(usage, "-y (or --yes)") {
		t.Error("--help does not document -y for cd")
	}
	if strings.Contains(usage, "order doesn't matter") || strings.Contains(usage, "flexible order") {
		t.Error("--help still carries the old order claim")
	}
	for _, f := range []string{"README.md", "README.ru.md", "README.ua.md", "README.de.md", "README.fr.md"} {
		body := readSurface(t, root, f)
		if !strings.Contains(body, "| `-y`, `--yes` |") {
			t.Errorf("%s has no -y row in its modifier table", f)
		}
		if !strings.Contains(body, `%LOCALAPPDATA%\FileDO\state\`) {
			t.Errorf("%s does not say where the hash cache lives", f)
		}
		if strings.Contains(body, "MD5") {
			t.Errorf("%s still names MD5 as the duplicate proof", f)
		}
	}
}
