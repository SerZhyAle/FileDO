package main

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// SP-0208: the generation set of `backup` (CLI-EVENT-STREAM 0.17 items D to G).

func TestBackupGenNumber(t *testing.T) {
	cases := []struct {
		name string
		want int
		ok   bool
	}{
		{"Photos_001.zip", 1, true},
		{"Photos_007.zip", 7, true},
		{"photos_003.ZIP", 3, true},     // the file system's case rule
		{"Photos_008.zip", 0, false},    // above the keep-count
		{"Photos_000.zip", 0, false},    // below 001
		{"Photos_1.zip", 0, false},      // not three digits
		{"Photos_0001.zip", 0, false},   // four digits
		{"Photos_001.fd-sec", 0, false}, // the other set
		{"Photos_001.zip.filedo-partial", 0, false},
		{"Photos_001.zip.bak", 0, false},
		{"xPhotos_001.zip", 0, false}, // another stem
		{"Photos001.zip", 0, false},   // no underscore
		{"Photos_00a.zip", 0, false},
		{"_001.zip", 0, false}, // no stem
	}
	for _, c := range cases {
		n, ok := backupGenNumber(c.name, "Photos", ".zip", 7)
		if ok != c.ok || n != c.want {
			t.Errorf("backupGenNumber(%q) = %d, %v; want %d, %v", c.name, n, ok, c.want, c.ok)
		}
	}
	if n, ok := backupGenNumber("Docs_002.fd-sec", "Docs", ".fd-sec", 999); !ok || n != 2 {
		t.Errorf("the sealed set was not read: %d, %v", n, ok)
	}
}

func TestBackupGenNext(t *testing.T) {
	base := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }
	cases := []struct {
		name    string
		present map[int]time.Time
		keep    int
		want    int
	}{
		{"an empty set starts at 001", map[int]time.Time{}, 3, 1},
		{"the number after the newest", map[int]time.Time{1: at(1)}, 3, 2},
		{"001 after the keep-count", map[int]time.Time{1: at(1), 2: at(2), 3: at(3)}, 3, 1},
		{"after a wrap the newest is 001, the next 002", map[int]time.Time{1: at(4), 2: at(2), 3: at(3)}, 3, 2},
		{"a deleted number does not stop the run", map[int]time.Time{1: at(1), 3: at(3)}, 4, 4},
		{"a gap after the newest is the next number", map[int]time.Time{1: at(4), 3: at(3)}, 4, 2},
		{"equal times: the higher number is the newer", map[int]time.Time{1: at(1), 2: at(1)}, 5, 3},
		{"a keep-count of 1 reuses 001", map[int]time.Time{1: at(1)}, 1, 1},
	}
	for _, c := range cases {
		if got := backupGenNext(c.present, c.keep); got != c.want {
			t.Errorf("%s: backupGenNext = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestBackupStemProblem(t *testing.T) {
	for _, ok := range []string{"Photos", "my.photos", "Docs 2026", "фото", "a-b_c"} {
		if why := backupStemProblem(ok); why != "" {
			t.Errorf("stem %q refused: %s", ok, why)
		}
	}
	for _, bad := range []string{"", ".", "..", `a\b`, "a/b", "a:b", "p:pw", "a*b", "con", "NUL", "COM1", "name.", "name ", "tab\tname", strings.Repeat("x", 250)} {
		if backupStemProblem(bad) == "" {
			t.Errorf("stem %q was accepted", bad)
		}
	}
}

func TestBackupGenArgs(t *testing.T) {
	o, err := parseBackupArgs("backup", []string{`D:\Photos`, "to", `E:\Bak`, "gen", "7", "stem", "Docs", "sec", "pe:BAK_PW", "y"})
	if err != nil {
		t.Fatal(err)
	}
	if o.gen != 7 || o.stem != "Docs" || !o.sec || o.to != `E:\Bak` || o.cred.src != "pe" {
		t.Fatalf("parsed %+v", o)
	}
	if o, err := parseBackupArgs("backup", []string{`D:\Photos`, "gen", "999"}); err != nil || o.gen != 999 || o.stem != "" {
		t.Fatalf("gen 999 alone: %+v %v", o, err)
	}
	refused := [][]string{
		{`D:\x`, "gen"},                         // no number
		{`D:\x`, "gen", "0"},                    // below 1
		{`D:\x`, "gen", "1000"},                 // above 999
		{`D:\x`, "gen", "many"},                 // not a number
		{`D:\x`, "gen", "3", "stem"},            // no stem value
		{`D:\x`, "gen", "3", "stem", "a/b"},     // not one file-name part
		{`D:\x`, "gen", "3", "stem", ".."},      // climbs out
		{`D:\x`, "gen", "3", "stem", "con"},     // a device name
		{`D:\x`, "stem", "Docs"},                // a stem needs gen
		{`D:\x`, "gen", "3", "sec", "keep-zip"}, // a kept zip would never be retired
		{`D:\x`, "gen", "3", "keep-zip"},        // keep-zip needs sec anyway
	}
	for _, args := range refused {
		_, err := parseBackupArgs("backup", args)
		if err == nil {
			t.Errorf("backup %q was accepted", args)
		} else if failureExitCode(err) != 10 {
			t.Errorf("backup %q ended with class %d, want 10", args, failureExitCode(err))
		}
	}
	// restore has neither word: they are not options there.
	for _, args := range [][]string{{"a.zip", "gen", "3"}, {"a.zip", "stem", "x"}} {
		if _, err := parseBackupArgs("restore", args); err == nil {
			t.Errorf("restore %q was accepted", args)
		}
	}
}

// TestBackupGenRedaction: the number and the stem stay readable in a recorded
// line, a word after `gen` that is not a number does not, and on a restore line
// a word after `stem` is still screened (it may be a password).
func TestBackupGenRedaction(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"backup", `D:\Photos`, "to", `E:\Bak`, "gen", "7", "stem", "Docs", "sec", "pe:PW"},
			[]string{"backup", `D:\Photos`, "to", `E:\Bak`, "gen", "7", "stem", "Docs", "sec", "pe:PW"}},
		{[]string{"backup", `D:\Photos`, "gen", "hunter2"}, []string{"backup", `D:\Photos`, "gen", "***"}},
		{[]string{"backup", `D:\Photos`, "gen", "3", "stem", "p:hunter2"}, []string{"backup", `D:\Photos`, "gen", "3", "stem", "p:***"}},
		{[]string{"restore", `C:\b.fd-sec`, "stem", "hunter2"}, []string{"restore", `C:\b.fd-sec`, "***", "***"}},
	}
	for _, c := range cases {
		if got := redactCredentialArgs(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("redact %q = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------- end to end

// genEnv is a source folder and an empty destination, with the global state a
// run needs: an interrupt handler without signals and a fresh run record.
type genEnv struct {
	src, dest string
}

func newGenEnv(t *testing.T) *genEnv {
	t.Helper()
	root := t.TempDir()
	e := &genEnv{src: filepath.Join(root, "Photos"), dest: filepath.Join(root, "bak")}
	for _, d := range []string{e.src, e.dest} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.write(t, "a.txt", "alpha")
	e.write(t, filepath.Join("sub", "b.txt"), "bravo")
	savedHandler, savedRun := globalInterruptHandler, currentRun
	globalInterruptHandler = newInterruptHandlerNoSignals()
	t.Cleanup(func() { globalInterruptHandler, currentRun = savedHandler, savedRun })
	return e
}

func (e *genEnv) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(e.src, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// run is one `backup <src> to <dest> gen <keep> [more words]` and the numbers its
// result carries.
func (e *genEnv) run(t *testing.T, keep int, more ...string) (map[string]interface{}, error) {
	t.Helper()
	args := append([]string{e.src, "to", e.dest, "gen", strconv.Itoa(keep), "level", "1"}, more...)
	o, err := parseBackupArgs("backup", args)
	if err != nil {
		t.Fatalf("parse %q: %v", args, err)
	}
	currentRun = &runOutcome{numbers: make(map[string]interface{})}
	err = runBackup(o, &HistoryLogger{})
	return currentRun.numbers, err
}

// names lists the destination, sorted.
func (e *genEnv) names(t *testing.T) []string {
	t.Helper()
	ents, err := os.ReadDir(e.dest)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ent := range ents {
		out = append(out, ent.Name())
	}
	sort.Strings(out)
	return out
}

// age sets the modification time of a generation, as the passing of time would:
// the newest generation is the one with the latest time.
func (e *genEnv) age(t *testing.T, name string, hours int) {
	t.Helper()
	at := time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC).Add(time.Duration(hours) * time.Hour)
	if err := os.Chtimes(filepath.Join(e.dest, name), at, at); err != nil {
		t.Fatal(err)
	}
}

// zipHas reports whether the zip at path holds an entry of that name.
func zipHas(t *testing.T, path, entry string) bool {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == entry {
			return true
		}
	}
	return false
}

func wantNumbers(t *testing.T, got map[string]interface{}, generation, retired, keep int) {
	t.Helper()
	for k, want := range map[string]int{"generation": generation, "retiredGeneration": retired, "keepCount": keep} {
		if v, ok := got[k]; !ok || v != want {
			t.Errorf("numbers[%q] = %v (present %v), want %d", k, v, ok, want)
		}
	}
}

// TestBackupGen_RotationRetiresOnlyTheOldest: three runs fill a set of three, the
// fourth replaces 001 and the fifth 002, the set never holds more than three, a
// retired number carries the new content, and no foreign file is touched.
func TestBackupGen_RotationRetiresOnlyTheOldest(t *testing.T) {
	e := newGenEnv(t)
	foreign := map[string]string{
		"Photos_004.zip":              "left by a larger keep-count",
		"Other_001.zip":               "another stem",
		"Photos_001.fd-sec":           "the sealed set of this stem",
		"Photos_001.zip.bak":          "another extension",
		"notes.txt":                   "not a backup",
		"Photos_002.zip.filedo-other": "not ours either",
	}
	for n, body := range foreign {
		if err := os.WriteFile(filepath.Join(e.dest, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	for i, name := range []string{"Photos_001.zip", "Photos_002.zip", "Photos_003.zip"} {
		got, err := e.run(t, 3)
		if err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		wantNumbers(t, got, i+1, 0, 3)
		if _, ok := got["archiveBytes"]; !ok {
			t.Errorf("run %d: no archiveBytes", i+1)
		}
		e.age(t, name, i+1)
	}
	e.write(t, "d.txt", "delta") // the fourth generation holds a file the first three do not
	oldTwo, err := os.ReadFile(filepath.Join(e.dest, "Photos_002.zip"))
	if err != nil {
		t.Fatal(err)
	}

	got, err := e.run(t, 3) // the set is full: 001 is the oldest and is replaced
	if err != nil {
		t.Fatalf("run 4: %v", err)
	}
	wantNumbers(t, got, 1, 1, 3)
	if !zipHas(t, filepath.Join(e.dest, "Photos_001.zip"), "d.txt") {
		t.Error("001 was not replaced by the new generation")
	}
	if zipHas(t, filepath.Join(e.dest, "Photos_003.zip"), "d.txt") {
		t.Error("003 changed")
	}
	if b, _ := os.ReadFile(filepath.Join(e.dest, "Photos_002.zip")); !reflect.DeepEqual(b, oldTwo) {
		t.Error("002 changed in a run that retired 001")
	}
	e.age(t, "Photos_001.zip", 4)

	got, err = e.run(t, 3)
	if err != nil {
		t.Fatalf("run 5: %v", err)
	}
	wantNumbers(t, got, 2, 2, 3)
	if !zipHas(t, filepath.Join(e.dest, "Photos_002.zip"), "d.txt") {
		t.Error("002 was not replaced by the new generation")
	}

	for n, body := range foreign {
		if b, err := os.ReadFile(filepath.Join(e.dest, n)); err != nil || string(b) != body {
			t.Errorf("%s was touched: %q, %v", n, b, err)
		}
	}
	wantNames := []string{"Other_001.zip", "Photos_001.fd-sec", "Photos_001.zip", "Photos_001.zip.bak", "Photos_002.zip", "Photos_002.zip.filedo-other", "Photos_003.zip", "Photos_004.zip", "notes.txt"}
	if got := e.names(t); !reflect.DeepEqual(got, wantNames) {
		t.Errorf("destination holds %q, want %q (no partial left behind)", got, wantNames)
	}
}

// TestBackupGen_ANumberDeletedByHandDoesNotStopTheRun: with 002 gone the sequence
// goes on from the newest, and the missing number is written again when it comes
// round - no state but the folder takes part (D2).
func TestBackupGen_ANumberDeletedByHandDoesNotStopTheRun(t *testing.T) {
	e := newGenEnv(t)
	for i, name := range []string{"Photos_001.zip", "Photos_002.zip", "Photos_003.zip"} {
		if _, err := e.run(t, 4); err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		e.age(t, name, i+1)
	}
	if err := os.Remove(filepath.Join(e.dest, "Photos_002.zip")); err != nil {
		t.Fatal(err)
	}
	got, err := e.run(t, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantNumbers(t, got, 4, 0, 4) // after the newest, 003; nothing was in 004
	e.age(t, "Photos_004.zip", 4)
	got, err = e.run(t, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantNumbers(t, got, 1, 1, 4) // 001 is occupied: retired by replacement
	e.age(t, "Photos_001.zip", 5)
	got, err = e.run(t, 4)
	if err != nil {
		t.Fatal(err)
	}
	wantNumbers(t, got, 2, 0, 4) // the number that was missing is written again, nothing retired
}

// TestBackupGen_DefaultStemIsTheFolderNameAndStemNamesTheSet: without stem the set
// carries the source folder's name; with it, two sources of one name stay apart.
func TestBackupGen_DefaultStemIsTheFolderNameAndStemNamesTheSet(t *testing.T) {
	e := newGenEnv(t)
	if _, err := e.run(t, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := e.run(t, 2, "stem", "Work"); err != nil {
		t.Fatal(err)
	}
	if got, want := e.names(t), []string{"Photos_001.zip", "Work_001.zip"}; !reflect.DeepEqual(got, want) {
		t.Errorf("destination holds %q, want %q", got, want)
	}
}

// TestBackupGen_SealedSetKeepsItsOwnNumbers: with sec the files are .fd-sec, the
// plain set of the same stem is neither read nor retired, and the full sealed set
// retires only its own oldest file.
func TestBackupGen_SealedSetKeepsItsOwnNumbers(t *testing.T) {
	e := newGenEnv(t)
	if _, err := e.run(t, 2); err != nil { // one plain generation
		t.Fatal(err)
	}
	plain, err := os.ReadFile(filepath.Join(e.dest, "Photos_001.zip"))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Photos_001.fd-sec", "Photos_002.fd-sec"} {
		got, err := e.run(t, 2, "sec", "p:pw")
		if err != nil {
			t.Fatalf("sealed run %d: %v", i+1, err)
		}
		wantNumbers(t, got, i+1, 0, 2)
		e.age(t, name, i+1)
	}
	got, err := e.run(t, 2, "sec", "p:pw")
	if err != nil {
		t.Fatal(err)
	}
	wantNumbers(t, got, 1, 1, 2)
	if b, _ := os.ReadFile(filepath.Join(e.dest, "Photos_001.zip")); !reflect.DeepEqual(b, plain) {
		t.Error("the plain set changed in a sealed run")
	}
	if got, want := e.names(t), []string{"Photos_001.fd-sec", "Photos_001.zip", "Photos_002.fd-sec"}; !reflect.DeepEqual(got, want) {
		t.Errorf("destination holds %q, want %q (no intermediate zip, no partial)", got, want)
	}
}

// TestBackupGen_AStaleBackupLeftBeforeIsNoObstacle: a partial left by a run that
// died does not wedge the rotation - the next run clears its own partial name.
func TestBackupGen_AStalePartialIsCleared(t *testing.T) {
	e := newGenEnv(t)
	stale := filepath.Join(e.dest, "Photos_001.zip.filedo-partial")
	if err := os.WriteFile(stale, []byte("a run died here"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := e.run(t, 3)
	if err != nil {
		t.Fatal(err)
	}
	wantNumbers(t, got, 1, 0, 3)
	if got := e.names(t); !reflect.DeepEqual(got, []string{"Photos_001.zip"}) {
		t.Errorf("destination holds %q", got)
	}
}

// TestBackupGen_CommitRetiresNothingWhenStopped: a stop that arrives after the new
// generation is built and before it is published retires nothing, removes the
// partial, and leaves the old file as it was; without the stop the same call
// removes exactly the old file of the number and puts the new one in its place.
func TestBackupGen_CommitRetiresNothingWhenStopped(t *testing.T) {
	e := newGenEnv(t)
	final := filepath.Join(e.dest, "Photos_001.zip")
	partial := final + ".filedo-partial"
	if err := os.WriteFile(final, []byte("old generation"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, []byte("new generation"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &backupGen{keep: 3, stem: "Photos", dir: e.dest, ext: ".zip", number: 1, occupied: true}

	globalInterruptHandler.Interrupt()
	retired, err := backupGenCommit(partial, g, func() {})
	if !errors.Is(err, errRunStopped) || retired {
		t.Fatalf("a stopped commit = %v, retired %v; want errRunStopped and nothing retired", err, retired)
	}
	if b, _ := os.ReadFile(final); string(b) != "old generation" {
		t.Errorf("the old generation changed: %q", b)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("the partial was left behind by a stopped run")
	}

	globalInterruptHandler = newInterruptHandlerNoSignals()
	if err := os.WriteFile(partial, []byte("new generation"), 0o644); err != nil {
		t.Fatal(err)
	}
	retired, err = backupGenCommit(partial, g, func() {})
	if err != nil || !retired {
		t.Fatalf("commit = %v, retired %v; want the old file retired", err, retired)
	}
	if b, _ := os.ReadFile(final); string(b) != "new generation" {
		t.Errorf("the new generation is not in place: %q", b)
	}
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("the partial name is still there after the rename")
	}
}

// TestBackupGen_APlainFreeSlotRetiresNothing: an unoccupied number is published
// by the rename alone and nothing is removed.
func TestBackupGen_AFreeSlotRetiresNothing(t *testing.T) {
	e := newGenEnv(t)
	final := filepath.Join(e.dest, "Photos_002.zip")
	partial := final + ".filedo-partial"
	if err := os.WriteFile(partial, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &backupGen{keep: 3, stem: "Photos", dir: e.dest, ext: ".zip", number: 2}
	if retired, err := backupGenCommit(partial, g, func() {}); err != nil || retired {
		t.Fatalf("commit = %v, retired %v; want published and nothing retired", err, retired)
	}
	if b, _ := os.ReadFile(final); string(b) != "new" {
		t.Errorf("published content: %q", b)
	}
}

// ---------------------------------------------------------------- the refusals

func reasonOf(err error) string {
	if d := reasonDetails(err); d != nil {
		if r, ok := d["reason"].(string); ok {
			return r
		}
	}
	return ""
}

// TestBackupGen_DefinitionRefusalsCarryTheirToken: a definition the verb refuses
// ends in class 10 with the token of its kind (item G), a missing destination
// folder is class 11 and carries none (item F), and nothing is written.
func TestBackupGen_DefinitionRefusalsCarryTheirToken(t *testing.T) {
	e := newGenEnv(t)
	file := filepath.Join(filepath.Dir(e.dest), "afile.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(e.dest, "Photos_001.zip"), 0o755); err != nil {
		t.Fatal(err) // a folder stands where the first generation would go
	}
	run := func(words ...string) error {
		o, err := parseBackupArgs("backup", words)
		if err != nil {
			t.Fatalf("parse %q: %v", words, err)
		}
		currentRun = &runOutcome{numbers: make(map[string]interface{})}
		return runBackup(o, &HistoryLogger{})
	}
	cases := []struct {
		name   string
		args   []string
		class  int
		reason string
	}{
		{"the source is a file", []string{file, "to", e.dest, "level", "1"}, 10, reasonBackupSourceRefused},
		{"gen: to names a file", []string{e.src, "to", file, "gen", "3", "level", "1"}, 10, reasonBackupDestinationRefused},
		{"gen: to names an archive file that is not there", []string{e.src, "to", filepath.Join(e.dest, "x.zip"), "gen", "3", "level", "1"}, 10, reasonBackupDestinationRefused},
		{"gen: the slot is held by a folder", []string{e.src, "to", e.dest, "gen", "3", "level", "1"}, 10, reasonBackupDestinationRefused},
		{"gen: to names a folder that is not there", []string{e.src, "to", filepath.Join(e.dest, "missing"), "gen", "3", "level", "1"}, 11, ""},
		{"gen: the destination lies inside the source", []string{e.src, "to", filepath.Join(e.src, "sub"), "gen", "3", "level", "1"}, 10, reasonBackupDestinationRefused},
		{"no gen: to names a file that exists", []string{e.src, "to", file, "level", "1"}, 10, reasonBackupDestinationRefused},
	}
	for _, c := range cases {
		err := run(c.args...)
		if err == nil {
			t.Errorf("%s: no error", c.name)
			continue
		}
		if got := failureExitCode(err); got != c.class {
			t.Errorf("%s: class %d, want %d (%v)", c.name, got, c.class, err)
		}
		if got := reasonOf(err); got != c.reason {
			t.Errorf("%s: reason %q, want %q", c.name, got, c.reason)
		}
	}
	if got, want := e.names(t), []string{"Photos_001.zip"}; !reflect.DeepEqual(got, want) {
		t.Errorf("a refused run wrote something: %q", got)
	}
}
