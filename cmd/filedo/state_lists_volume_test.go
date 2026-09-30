package main

// AUD-14-F1 (SP-0045): a damaged, good or skip list entry is trusted only on
// the volume and the file it was recorded on. The lists keyed an entry on the
// drive-letter path alone, so the next card at the same letter holding a copy
// with its mtime kept inherited the old card's verdict: check re-reported it
// damaged without reading it (exit 1) and safecopy skipped it. The owner's
// decision: each line carries the volume serial and the file ID next to size
// and mtime (format 2, statedir/STATE-LISTS.md); an entry whose volume or file
// does not match is ignored and the file is read again; older lines without
// the identity are never trusted.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// firstFile is the first regular file under root.
func firstFile(t *testing.T, root string) string {
	t.Helper()
	var first string
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && first == "" {
			first = p
		}
		return nil
	})
	if first == "" {
		t.Fatal("no file in the tree")
	}
	return first
}

// seedList writes a list file by hand, the way another run or another card's
// run would have left it.
func seedList(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckDamagedEntryFromAnotherVolumeIsReadAgain(t *testing.T) {
	root := checkTree(t, 3)
	bad := firstFile(t, root)
	info, err := os.Stat(bad)
	if err != nil {
		t.Fatal(err)
	}
	vol, fid, err := fileIdentity(bad)
	if err != nil {
		t.Fatal(err)
	}
	mod := info.ModTime().UTC().Format(stateListTimeLayout)
	rows := map[string][]string{
		// Card A's run: same path, size and mtime, another volume serial.
		"foreign volume": {
			fmt.Sprintf("%s%d", stateListHeaderPrefix, stateListFormat),
			fmt.Sprintf("%s\t%d\t%s\t%08X\t%016X", bad, info.Size(), mod, vol^0xFFFFFFFF, fid),
		},
		// A format 1 line from an older FileDO: no volume at all.
		"legacy line": {
			fmt.Sprintf("%s\t%d\t%s", bad, info.Size(), mod),
		},
		// Same volume, another file: a copy put at the same path.
		"other file": {
			fmt.Sprintf("%s%d", stateListHeaderPrefix, stateListFormat),
			fmt.Sprintf("%s\t%d\t%s\t%08X\t%016X", bad, info.Size(), mod, vol, fid+1),
		},
	}
	for name, lines := range rows {
		t.Run(strings.ReplaceAll(name, " ", "_"), func(t *testing.T) {
			wd := t.TempDir()
			seedList(t, filepath.Join(wd, checkDamagedName), lines...)
			out, code := run(t, wd, "check", root, "--no-precount")
			if code != 0 {
				t.Errorf("%s: exit %d, want 0 - the entry was trusted\n%s", name, code, out)
			}
			if !strings.Contains(out, "checked=3") || !strings.Contains(out, "damaged(known, not re-read)=0") {
				t.Errorf("%s: the file was not read again\n%s", name, out)
			}
		})
	}

	// The control: the same line with the file's real identity is trusted.
	wd := t.TempDir()
	seedList(t, filepath.Join(wd, checkDamagedName),
		fmt.Sprintf("%s%d", stateListHeaderPrefix, stateListFormat),
		fmt.Sprintf("%s\t%d\t%s\t%08X\t%016X", bad, info.Size(), mod, vol, fid))
	out, code := run(t, wd, "check", root, "--no-precount")
	if code != 1 || !strings.Contains(out, "damaged(known, not re-read)=1") {
		t.Errorf("an entry with the file's own identity was not trusted: exit %d\n%s", code, out)
	}
}

// The recovery flow: the damaged file is replaced at the same path by a copy
// with the same size and mtime. It is a different file and is read again.
func TestCheckDamagedEntryForReplacedCopyIsReadAgain(t *testing.T) {
	root := checkTree(t, 3)
	bad := firstFile(t, root)
	wd := t.TempDir()
	l, err := loadStateList(filepath.Join(wd, checkDamagedName))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.AddInfo(bad, info); err != nil {
		t.Fatal(err)
	}
	l.Close()

	body, err := os.ReadFile(bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}
	// Another file first, so the copy cannot land on the freed file ID.
	writeFile(t, bad+".spacer", []byte("x"))
	writeFile(t, bad, body)
	if err := os.Chtimes(bad, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	os.Remove(bad + ".spacer")

	out, code := run(t, wd, "check", root, "--no-precount")
	if code != 0 || !strings.Contains(out, "checked=3") {
		t.Errorf("a copy at the damaged file's path inherited its verdict: exit %d\n%s", code, out)
	}
}

func TestStateListFormat2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	writeFile(t, p, patternBytes(300, 1))
	info, _ := os.Stat(p)
	listPath := filepath.Join(dir, checkGoodListName)

	l, err := loadStateList(listPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.AddInfo(p, info); err != nil {
		t.Fatal(err)
	}
	l.Close()
	b, _ := os.ReadFile(listPath)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 || lines[0] != fmt.Sprintf("%s%d", stateListHeaderPrefix, stateListFormat) {
		t.Fatalf("want the format header and one line, got\n%s", b)
	}
	if f := strings.Split(lines[1], "\t"); len(f) != 5 || len(f[3]) != 8 || len(f[4]) != 16 {
		t.Fatalf("the line does not carry path, size, mtime, volume serial and file ID: %q", lines[1])
	}
	l2, err := loadStateList(listPath)
	if err != nil {
		t.Fatal(err)
	}
	if !l2.HasInfo(p, info) || l2.LegacyIgnored() != 0 {
		t.Errorf("a format 2 entry is not trusted after a reload (legacy=%d)", l2.LegacyIgnored())
	}
	// A foreign identity for the same path is not the same file.
	l2.identity = func(string) (uint32, uint64, error) { return 1, 2, nil }
	if l2.HasInfo(p, info) {
		t.Error("an entry was trusted for a file on another volume")
	}
}

func TestStateListLegacyLinesAreRewrittenNotTrusted(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	writeFile(t, p, patternBytes(300, 1))
	info, _ := os.Stat(p)
	listPath := filepath.Join(dir, copySkipListName)
	seedList(t, listPath,
		`C:\old\path-only.bin`,
		fmt.Sprintf("%s\t%d\t%s", p, info.Size(), info.ModTime().UTC().Format(stateListTimeLayout)),
		"garbage\tline\twith\ttoo\tmany\tfields",
		"\x00\x01 not text at all")

	l, err := loadStateList(listPath)
	if err != nil {
		t.Fatal(err)
	}
	if l.Len() != 0 || l.LegacyIgnored() != 4 {
		t.Errorf("want 0 trusted and 4 ignored lines, got %d and %d", l.Len(), l.LegacyIgnored())
	}
	if l.HasInfo(p, info) {
		t.Error("a format 1 line was trusted")
	}
	other := filepath.Join(dir, "g.bin")
	writeFile(t, other, patternBytes(200, 2))
	oi, _ := os.Stat(other)
	if err := l.AddInfo(other, oi); err != nil {
		t.Fatal(err)
	}
	l.Close()
	b, _ := os.ReadFile(listPath)
	if strings.Contains(string(b), "path-only") || strings.Contains(string(b), "garbage") ||
		!strings.HasPrefix(string(b), fmt.Sprintf("%s%d\n", stateListHeaderPrefix, stateListFormat)) {
		t.Errorf("the first write did not rewrite the list without its untrusted lines\n%s", b)
	}
	l2, _ := loadStateList(listPath)
	if l2.Len() != 1 || l2.LegacyIgnored() != 0 || !l2.HasInfo(other, oi) {
		t.Errorf("after the rewrite: %d trusted, %d ignored", l2.Len(), l2.LegacyIgnored())
	}
}

func TestStateListNewerFormatIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	writeFile(t, p, patternBytes(300, 1))
	info, _ := os.Stat(p)
	vol, fid, err := fileIdentity(p)
	if err != nil {
		t.Fatal(err)
	}
	listPath := filepath.Join(dir, checkDamagedName)
	body := fmt.Sprintf("%s%d\n%s\t%d\t%s\t%08X\t%016X\n", stateListHeaderPrefix, stateListFormat+1,
		p, info.Size(), info.ModTime().UTC().Format(stateListTimeLayout), vol, fid)
	seedList(t, listPath, strings.TrimSuffix(body, "\n"))

	l, err := loadStateList(listPath)
	if err != nil {
		t.Fatal(err)
	}
	if l.HasInfo(p, info) {
		t.Error("an entry from a newer format was trusted")
	}
	l.AddInfo(p, info)
	l.Close()
	if b, _ := os.ReadFile(listPath); string(b) != body {
		t.Errorf("a list in a newer format was written to\n%s", b)
	}
}

// The copy side: a skip-list entry recorded on another volume does not make
// the copy skip the file.
func TestCopySkipListEntryFromAnotherVolumeIsCopied(t *testing.T) {
	state := isolateState(t)
	src := filepath.Join(t.TempDir(), "src")
	for i := 0; i < 3; i++ {
		writeFile(t, filepath.Join(src, fmt.Sprintf("f%d.jpg", i)), patternBytes(1000+i, byte(i)))
	}
	listed := filepath.Join(src, "f1.jpg")
	info, _ := os.Stat(listed)
	vol, fid, err := fileIdentity(listed)
	if err != nil {
		t.Fatal(err)
	}
	l, err := loadStateList(filepath.Join(state, copySkipListName))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.addStamp(listed, fileStamp{size: info.Size(), mod: info.ModTime(), vol: vol ^ 0xFFFFFFFF, fid: fid}); err != nil {
		t.Fatal(err)
	}
	l.Close()

	stats, err := runCopyEngine("fastcopy", src, filepath.Join(t.TempDir(), "dst"), NewFastCopyConfig(), nil, newInterruptHandlerNoSignals())
	if err != nil || stats.damagedSkipped.Load() != 0 || stats.copied.Load() != 3 {
		t.Errorf("want 3 copied and none skipped; got copied=%d skipped=%d err=%v",
			stats.copied.Load(), stats.damagedSkipped.Load(), err)
	}
}
