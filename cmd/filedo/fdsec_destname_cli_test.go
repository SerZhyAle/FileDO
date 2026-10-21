package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"filedo/fdsec"
)

// FDSEC-FORMAT 1.4 section 20 and FDSEC-BEHAVIOUR 1.6 section 18, against the
// shipped exe. The four characters U+0022 U+003C U+003E U+007C are valid in an
// authenticated sealed name in every suite; a Windows destination cannot create
// them, nor a name over 255 UTF-16 units. The restore therefore stops - exit 5,
// one sentence that names the rule and never the name, nothing written, the
// container untouched - and `rename` and `to <path>` stay the way out. Every
// test here builds the container with the reference writer (which now accepts
// the name), runs the real command and reads what is left on disk.

const (
	destCred = "pw-destination-name"
	// destLeak is the distinctive part of every sealed name below: it must not
	// appear in the console, history.json or the event file.
	destLeak = "zebra"
)

var destPayload = []byte("the payload of a container whose sealed name this PC cannot create")

type destEntry struct {
	path    string
	dir     bool
	content []byte
}

// destWrite writes c.fd-sec into dir with the reference writer and returns its
// path. suite 1 and 2 take name; suite 3 takes name as the root and entries.
func destWrite(t *testing.T, dir string, suite int, name string, entries ...destEntry) string {
	t.Helper()
	cred := fdsec.NewCredential(destCred)
	var out bytes.Buffer
	var err error
	switch suite {
	case 1:
		_, err = fdsec.Pack(&out, bytes.NewReader(destPayload), fdsec.Metadata{Name: name, Size: int64(len(destPayload))}, cred, fdsec.Params{})
	case 2:
		_, err = fdsec.PackSuite2(&out, bytes.NewReader(destPayload), fdsec.Metadata{Name: name, Size: int64(len(destPayload))}, cred)
	case 3:
		tr := &fdsec.Tree{Meta: fdsec.TreeMetadata{Name: name}}
		files := map[string][]byte{}
		for _, e := range entries {
			te := fdsec.TreeEntry{Path: e.path, Kind: fdsec.KindFile, Size: int64(len(e.content))}
			if e.dir {
				te.Kind, te.Size = fdsec.KindDir, 0
			} else {
				files[e.path] = e.content
			}
			tr.Entries = append(tr.Entries, te)
		}
		tr.Open = func(p string) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(files[p])), nil }
		_, err = fdsec.PackTree(&out, tr, cred, fdsec.Params{})
	}
	if err != nil {
		t.Fatalf("the reference writer refused the test container (suite %d): %v", suite, err)
	}
	path := filepath.Join(dir, "c.fd-sec")
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// destRun runs `filedo c.fd-sec <args> p:<credential>` in dir with an event
// file, and returns the console text, the exit code and the event file.
func destRun(t *testing.T, dir string, args ...string) (string, int, string) {
	t.Helper()
	ev := filepath.Join(dir, "events.jsonl")
	os.Remove(ev)
	full := append([]string{"--events", ev, "c.fd-sec"}, args...)
	full = append(full, "p:"+destCred)
	out, code := run(t, dir, full...)
	events := ""
	if b, err := os.ReadFile(ev); err == nil {
		events = string(b)
	}
	return out, code, events
}

func destNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]bool{}
	for _, de := range des {
		m[de.Name()] = true
	}
	return m
}

// destRevealLeft counts what a reveal or a start left under the sandbox root.
func destRevealLeft(dir string) []string {
	var left []string
	des, err := os.ReadDir(revealRoot(dir))
	if err != nil {
		return nil
	}
	for _, de := range des {
		left = append(left, de.Name())
	}
	return left
}

// destFindingReasons returns the details.reason of every "error" finding in an
// event file, in order (CLI-EVENT-STREAM rule 17); a finding with no reason adds
// nothing.
func destFindingReasons(t *testing.T, events string) []string {
	t.Helper()
	var reasons []string
	for _, line := range strings.Split(events, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var ev struct {
			Kind string `json:"kind"`
			Data struct {
				Type    string `json:"type"`
				Details struct {
					Reason string `json:"reason"`
				} `json:"details"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("event line is not JSON: %v\n%s", err, line)
		}
		if ev.Kind == "finding" && ev.Data.Type == "error" && ev.Data.Details.Reason != "" {
			reasons = append(reasons, ev.Data.Details.Reason)
		}
	}
	return reasons
}

// assertDestinationRefusal is the whole contract in one place: the class, the
// sentence, no name anywhere, nothing written, the container untouched.
func assertDestinationRefusal(t *testing.T, dir, out string, code int, events string, before map[string]bool, container []byte) {
	t.Helper()
	t.Logf("exit %d, console: %s", code, strings.TrimSpace(out))
	if code != 5 {
		t.Errorf("exit %d, want 5 (the I/O and destination class)\n%s", code, out)
	}
	if !strings.Contains(out, fdsecDestinationNameMarker) {
		t.Errorf("the console does not carry the destination-name sentence\n%s", out)
	}
	if strings.Contains(strings.ToLower(out), "damaged") {
		t.Errorf("a name the destination cannot create was reported as damage\n%s", out)
	}
	if strings.Contains(out, "wrong password") || strings.Contains(out, "not a container") {
		t.Errorf("a valid container was reported as a wrong password or not a container\n%s", out)
	}
	if strings.Contains(out, destLeak) {
		t.Errorf("the console echoes the sealed name\n%s", out)
	}
	if !strings.Contains(events, fdsecDestinationNameMarker) {
		t.Errorf("the event stream does not carry the sentence (a window with no console shows the event text)\n%s", events)
	}
	if strings.Contains(events, destLeak) {
		t.Errorf("the event stream holds the sealed name\n%s", events)
	}
	// The closed token (FDSEC-BEHAVIOUR 1.7 section 19, CLI-EVENT-STREAM rule 17):
	// one line of its own on the console, and details.reason on the error finding.
	reasonLines := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimRight(line, "\r") == fdsecReasonLine {
			reasonLines++
		}
	}
	if reasonLines != 1 {
		t.Errorf("the console carries the line %q %d times, want exactly once\n%s", fdsecReasonLine, reasonLines, out)
	}
	if got := destFindingReasons(t, events); len(got) != 1 || got[0] != fdsecReasonDestinationName {
		t.Errorf("the error findings carry details.reason %q, want exactly [%q]\n%s", got, fdsecReasonDestinationName, events)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "history.json")); err == nil && bytes.Contains(b, []byte(destLeak)) {
		t.Errorf("history.json holds the sealed name\n%s", b)
	}
	for name := range destNames(t, dir) {
		switch name {
		case "history.json", "events.jsonl", ".reveal-root":
			continue
		}
		if !before[name] {
			t.Errorf("%s appeared, but nothing was to be written", name)
		}
	}
	if left := destRevealLeft(dir); len(left) != 0 {
		t.Errorf("a sandbox survived the refusal: %v", left)
	}
	if got := mustRead(t, filepath.Join(dir, "c.fd-sec")); !bytes.Equal(got, container) {
		t.Error("the container was changed")
	}
}

var destRandomName = regexp.MustCompile(`^[A-Za-z0-9]{24}$`)

// Every suite, every one of the four characters, and a name of 256 units:
// `unsecure` stops, and says so.
func TestDestName_UnsecureStopsWithNothingWritten(t *testing.T) {
	long := strings.Repeat(destLeak, 50) + "12345t" // 256 UTF-16 units
	if utf16Len(long) != 256 {
		t.Fatalf("test name has %d units", utf16Len(long))
	}
	type row struct {
		label string
		suite int
		name  string
	}
	var rows []row
	for i, c := range destFour {
		for _, suite := range []int{1, 2, 3} {
			rows = append(rows, row{fmt.Sprintf("suite %d char %d", suite, i+1), suite, destLeak + c + "crossing.txt"})
		}
	}
	rows = append(rows, row{"suite 1, 256 units", 1, long}, row{"suite 2, 256 units", 2, long}, row{"suite 3 root, 256 units", 3, long})
	for _, r := range rows {
		t.Run(r.label, func(t *testing.T) {
			dir := t.TempDir()
			var path string
			if r.suite == 3 {
				path = destWrite(t, dir, 3, r.name, destEntry{path: "inside.txt", content: []byte("one")})
			} else {
				path = destWrite(t, dir, r.suite, r.name)
			}
			container := mustRead(t, path)
			before := destNames(t, dir)
			out, code, events := destRun(t, dir, "unsecure")
			assertDestinationRefusal(t, dir, out, code, events, before, container)
		})
	}
}

// A name INSIDE a folder container has no alternate: the restore stops before
// the first entry is written, and the container still verifies.
func TestDestName_FolderEntryStopsTheRestore(t *testing.T) {
	long := strings.Repeat(destLeak, 50) + "12345t"
	for _, tc := range []struct{ label, comp string }{
		{"quote", destLeak + `"q`}, {"less-than", destLeak + "<q"}, {"greater-than", destLeak + ">q"}, {"bar", destLeak + "|q"}, {"256 units", long},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			path := destWrite(t, dir, 3, "root",
				destEntry{path: "a.txt", content: []byte("first")},
				destEntry{path: tc.comp, dir: true},
				destEntry{path: tc.comp + "/f.txt", content: []byte("below")})
			container := mustRead(t, path)
			before := destNames(t, dir)
			out, code, events := destRun(t, dir, "unsecure")
			assertDestinationRefusal(t, dir, out, code, events, before, container)
			if !strings.Contains(out, "inside this folder container") {
				t.Errorf("the entry refusal is not the folder variant\n%s", out)
			}
			// rename and to <path> name the root only; an entry has no alternate.
			out, code, events = destRun(t, dir, "unsecure", "rename")
			assertDestinationRefusal(t, dir, out, code, events, before, container)
			// What is inside still verifies: it is a valid container.
			if vout, vcode := run(t, dir, "fdsec", "verify", "c.fd-sec", "p:"+destCred); vcode != 0 {
				t.Errorf("fdsec verify exited %d for a valid container\n%s", vcode, vout)
			}
		})
	}
}

// The ways out. A container the first test stopped restores under a name the
// user chose (to <path>) or a random one (rename), byte for byte, and the
// container stays.
func TestDestName_RenameAndToStillRestoreIt(t *testing.T) {
	for _, suite := range []int{1, 2} {
		name := destLeak + `|"<>crossing.txt`
		t.Run(fmt.Sprintf("suite %d", suite), func(t *testing.T) {
			dir := t.TempDir()
			path := destWrite(t, dir, suite, name)
			container := mustRead(t, path)
			before := destNames(t, dir)

			chosen := filepath.Join(dir, "chosen.txt")
			if out, code, _ := destRun(t, dir, "unsecure", "to", chosen); code != 0 {
				t.Fatalf("to <path> exited %d\n%s", code, out)
			}
			if got := mustRead(t, chosen); !bytes.Equal(got, destPayload) {
				t.Error("to <path>: the restored bytes differ")
			}
			os.Remove(chosen)

			out, code, _ := destRun(t, dir, "unsecure", "rename")
			if code != 0 {
				t.Fatalf("rename exited %d\n%s", code, out)
			}
			var renamed []string
			for n := range destNames(t, dir) {
				if !before[n] && n != "history.json" && n != "events.jsonl" && n != ".reveal-root" {
					renamed = append(renamed, n)
				}
			}
			if len(renamed) != 1 || !destRandomName.MatchString(renamed[0]) {
				t.Fatalf("rename left %v, want one random 24-character name", renamed)
			}
			if got := mustRead(t, filepath.Join(dir, renamed[0])); !bytes.Equal(got, destPayload) {
				t.Error("rename: the restored bytes differ")
			}
			if !bytes.Equal(mustRead(t, filepath.Join(dir, "c.fd-sec")), container) {
				t.Error("the container was changed")
			}
			assertNoPartials(t, dir)
		})
	}

	t.Run("suite 3 root", func(t *testing.T) {
		dir := t.TempDir()
		destWrite(t, dir, 3, destLeak+`"<>|root`, destEntry{path: "d", dir: true}, destEntry{path: "d/f.txt", content: []byte("below")})
		chosen := filepath.Join(dir, "chosen-folder")
		if out, code, _ := destRun(t, dir, "unsecure", "to", chosen); code != 0 {
			t.Fatalf("to <path> exited %d\n%s", code, out)
		}
		if got := mustRead(t, filepath.Join(chosen, "d", "f.txt")); string(got) != "below" {
			t.Errorf("the folder did not come back: %q", got)
		}
		before := destNames(t, dir)
		out, code, _ := destRun(t, dir, "unsecure", "rename")
		if code != 0 {
			t.Fatalf("rename exited %d\n%s", code, out)
		}
		found := false
		for n := range destNames(t, dir) {
			if !before[n] && destRandomName.MatchString(n) {
				found = exists(filepath.Join(dir, n, "d", "f.txt"))
			}
		}
		if !found {
			t.Error("rename did not restore the folder under a random name")
		}
		assertNoPartials(t, dir)
	})
}

// Alternate-name consent and collision (FDSEC-BEHAVIOUR 1.6 section 18): the
// name the user chose goes through the same collision rule as any other, an
// existing file is never replaced, and a restore into an existing folder still
// uses the sealed name and so still stops.
func TestDestName_AlternateNameCollisionAndFolder(t *testing.T) {
	dir := t.TempDir()
	path := destWrite(t, dir, 1, destLeak+"|crossing.txt")
	container := mustRead(t, path)
	chosen := filepath.Join(dir, "chosen.txt")
	if err := os.WriteFile(chosen, []byte("the user's own file"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Without -y and without a console: the question cannot be asked, and the
	// existing file is untouched.
	out, code, _ := destRun(t, dir, "unsecure", "to", chosen)
	if code == 0 {
		t.Errorf("an existing destination was restored over, exit 0\n%s", out)
	}
	if string(mustRead(t, chosen)) != "the user's own file" {
		t.Error("an existing destination was overwritten")
	}
	// With -y the rule is the suffix, never the overwrite.
	out, code, _ = destRun(t, dir, "unsecure", "to", chosen, "-y")
	if code != 0 {
		t.Fatalf("-y exited %d\n%s", code, out)
	}
	if string(mustRead(t, chosen)) != "the user's own file" {
		t.Error("-y overwrote the existing destination")
	}
	if got := mustRead(t, filepath.Join(dir, "chosen-1.txt")); !bytes.Equal(got, destPayload) {
		t.Error("-y: the suffixed copy is missing or differs")
	}
	if !bytes.Equal(mustRead(t, filepath.Join(dir, "c.fd-sec")), container) {
		t.Error("the container was changed")
	}
	assertNoPartials(t, dir)

	// to <existing folder>: the sealed name becomes the file name, so it stops.
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	before := destNames(t, dir)
	out, code, events := destRun(t, dir, "unsecure", "to", sub)
	assertDestinationRefusal(t, dir, out, code, events, before, container)
	if des, _ := os.ReadDir(sub); len(des) != 0 {
		t.Errorf("a refused restore wrote into the folder: %v", des)
	}
}

// The limit is 255 UTF-16 units: a name of exactly 255 restores, the
// neighbours of the four characters restore, and a lookalike of a device name
// restores. A refusal that also refused these would be a feature nobody can use.
func TestDestName_WhatThePredicateMustNotRefuse(t *testing.T) {
	for _, name := range []string{strings.Repeat("n", 251) + ".txt", "!#;={}.txt", "CONTRACT.pdf", "a b.c.txt"} {
		dir := t.TempDir()
		destWrite(t, dir, 1, name)
		out, code, _ := destRun(t, dir, "unsecure")
		if code != 0 {
			t.Errorf("%.12q: exit %d, want 0\n%s", name, code, out)
			continue
		}
		if got := mustRead(t, filepath.Join(dir, name)); !bytes.Equal(got, destPayload) {
			t.Errorf("%.12q: restored bytes differ", name)
		}
	}
}

// reveal always uses the sealed name for the copy, so it stops before the copy
// exists; the sentence points at unsecure; -rw is that same restore.
func TestDestName_RevealStopsAndPointsAtUnsecure(t *testing.T) {
	for _, tc := range []struct {
		label string
		suite int
		name  string
	}{
		{"suite 1 bar", 1, destLeak + "|crossing.txt"},
		{"suite 2 less-than", 2, destLeak + "<crossing.txt"},
		{"suite 1 256 units", 1, strings.Repeat(destLeak, 50) + "12345t"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			dir := t.TempDir()
			path := destWrite(t, dir, tc.suite, tc.name)
			container := mustRead(t, path)
			before := destNames(t, dir)
			out, code, events := destRun(t, dir, "reveal")
			assertDestinationRefusal(t, dir, out, code, events, before, container)
			if !strings.Contains(out, "nothing was revealed") || !strings.Contains(out, "unsecure to <path>") {
				t.Errorf("the reveal refusal does not say what was not done and where to go\n%s", out)
			}

			// -rw is the ordinary restore: the same stop, and to <path> is its way out.
			out, code, events = destRun(t, dir, "reveal", "-rw")
			assertDestinationRefusal(t, dir, out, code, events, before, container)
			chosen := filepath.Join(dir, "chosen.txt")
			if out, code, _ := destRun(t, dir, "reveal", "-rw", "to", chosen); code != 0 {
				t.Fatalf("reveal -rw to <path> exited %d\n%s", code, out)
			}
			if got := mustRead(t, chosen); !bytes.Equal(got, destPayload) {
				t.Error("reveal -rw to <path>: the restored bytes differ")
			}
		})
	}
}

// unsecure start restores into a sandbox under the sealed name; it stops the
// same way, leaves no sandbox, and rename is its way out.
func TestDestName_StartStopsAndRenameStarts(t *testing.T) {
	dir := t.TempDir()
	path := destWrite(t, dir, 1, destLeak+">crossing.txt")
	container := mustRead(t, path)
	before := destNames(t, dir)
	out, code, events := destRun(t, dir, "unsecure", "start")
	assertDestinationRefusal(t, dir, out, code, events, before, container)
	if left := startSandboxes(t, dir); len(left) != 0 {
		t.Errorf("a start sandbox survived the refusal: %v", left)
	}
	out, code, _ = destRun(t, dir, "unsecure", "start", "rename")
	if code != 0 {
		t.Fatalf("unsecure start rename exited %d\n%s", code, out)
	}
	if left := startSandboxes(t, dir); len(left) != 0 {
		t.Errorf("a start sandbox survived the run: %v\n%s", left, out)
	}
}

// What the format refuses is still damage, exit 4, in every verb that restores:
// section 20 relaxes four characters and nothing else, and the sandbox's
// escape and device-name screening is exactly as strict as it was.
func TestDestName_FormatRefusalsStayDamage(t *testing.T) {
	for _, bad := range []string{`..\escaped.txt`, "CON", "a:b", "trailing.", "bad\x01name"} {
		t.Run(strings.NewReplacer("\\", "_", ":", "_", "\x01", "_").Replace(bad), func(t *testing.T) {
			dir := t.TempDir()
			path := destWrite(t, dir, 1, "ok.txt")
			forged, err := fdsec.ForgeMetadataNameForTest(mustRead(t, path), fdsec.NewCredential(destCred), bad)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, forged, 0o644); err != nil {
				t.Fatal(err)
			}
			before := destNames(t, dir)
			for _, verb := range [][]string{{"unsecure"}, {"reveal"}, {"unsecure", "start"}} {
				out, code, events := destRun(t, dir, verb...)
				if code != 4 {
					t.Errorf("%v: exit %d, want 4 (damaged)\n%s", verb, code, out)
				}
				if strings.Contains(out, fdsecDestinationNameMarker) {
					t.Errorf("%v: a format refusal is worded as a destination failure\n%s", verb, out)
				}
				// The closed token belongs to a destination refusal alone.
				if strings.Contains(out, fdsecReasonLine) || len(destFindingReasons(t, events)) != 0 {
					t.Errorf("%v: a format refusal carries the destination-name token\n%s\n%s", verb, out, events)
				}
			}
			for name := range destNames(t, dir) {
				if !before[name] && name != "history.json" && name != "events.jsonl" && name != ".reveal-root" {
					t.Errorf("%s appeared for a refused name", name)
				}
			}
			if left := destRevealLeft(dir); len(left) != 0 {
				t.Errorf("a sandbox survived: %v", left)
			}
		})
	}
}

// Reading is not restoring: fdsec verify and fdsec info open a container
// whose name this PC cannot create, in every suite.
func TestDestName_VerifyAndInfoStillRead(t *testing.T) {
	for suite := 1; suite <= 3; suite++ {
		t.Run(fmt.Sprintf("suite %d", suite), func(t *testing.T) {
			dir := t.TempDir()
			if suite == 3 {
				destWrite(t, dir, 3, destLeak+`"<>|root`, destEntry{path: "d|q", dir: true}, destEntry{path: "d|q/f<.txt", content: []byte("x")})
			} else {
				destWrite(t, dir, suite, destLeak+`"<>|name.txt`)
			}
			for _, sub := range []string{"verify", "info"} {
				if out, code := run(t, dir, "fdsec", sub, "c.fd-sec", "p:"+destCred); code != 0 {
					t.Errorf("fdsec %s exited %d for a valid container\n%s", sub, code, out)
				}
			}
		})
	}
}
