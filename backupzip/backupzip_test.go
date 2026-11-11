package backupzip

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTree(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for rel, b := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		b, rerr := os.ReadFile(p)
		out[filepath.ToSlash(rel)] = b
		return rerr
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func noise(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// TestRoundTrip packs a tree and restores it byte for byte, the empty folder
// included, with the source name in the comment and never a path.
func TestRoundTrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Photos")
	files := map[string][]byte{
		"a.txt":              bytes.Repeat([]byte("text compresses well "), 4000),
		"sub/b.bin":          noise(300 << 10),
		"sub/deeper/с.txt":   []byte("a non-ASCII name"),
		"sub/empty-file.dat": {},
	}
	writeTree(t, src, files)
	if err := os.MkdirAll(filepath.Join(src, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	w, err := Walk(src, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if w.Files != 4 {
		t.Fatalf("walk found %d files, want 4", w.Files)
	}
	dst := filepath.Join(t.TempDir(), "Photos-2611101530.zip")
	res, err := Pack(dst, w, PackOptions{Level: 6, Name: "Photos"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != dst || res.Files != 4 {
		t.Fatalf("pack result %+v", res)
	}
	if _, err := os.Stat(dst + "." + "filedo-partial"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the partial file is still there: %v", err)
	}
	zr, err := zip.OpenReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if got := SourceName(zr.Comment); got != "Photos" {
		t.Fatalf("recorded name %q, want Photos", got)
	}
	if strings.Contains(zr.Comment, string(filepath.Separator)) {
		t.Fatalf("the comment carries a path: %q", zr.Comment)
	}
	entries, err := ScreenEntries(zr.File)
	if err != nil {
		t.Fatal(err)
	}
	back := filepath.Join(t.TempDir(), "back")
	if err := os.Mkdir(back, 0o755); err != nil {
		t.Fatal(err)
	}
	n, _, err := Unpack(back, entries, UnpackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Fatalf("unpacked %d files, want 4", n)
	}
	got := readTree(t, back)
	for rel, want := range files {
		if !bytes.Equal(got[rel], want) {
			t.Errorf("%s differs after the round trip", rel)
		}
	}
	if fi, err := os.Stat(filepath.Join(back, "empty-dir")); err != nil || !fi.IsDir() {
		t.Errorf("the empty folder was not restored: %v", err)
	}
}

// TestPackNeverReplaces is the no-replace write of spec 3.3.
func TestPackNeverReplaces(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("x")})
	w, err := Walk(src, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "taken.zip")
	if err := os.WriteFile(dst, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Pack(dst, w, PackOptions{Level: 1, Name: "x"}); err == nil {
		t.Fatal("Pack wrote over an existing file")
	}
	if b, _ := os.ReadFile(dst); string(b) != "keep me" {
		t.Fatal("the existing file was changed")
	}
}

func TestPackNeverReplacesExistingPartial(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "backup.zip")
	partial := dst + ".filedo-partial"
	if err := os.WriteFile(partial, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	registered := false
	_, err := Pack(dst, Walked{}, PackOptions{Level: 0, Name: "x", RegisterCleanup: func(func()) func() {
		registered = true
		return func() {}
	}})
	if err == nil || registered {
		t.Fatalf("existing partial was accepted or registered for cleanup: %v", err)
	}
	if b, err := os.ReadFile(partial); err != nil || string(b) != "keep me" {
		t.Fatalf("existing partial changed: %q, %v", b, err)
	}
}

// TestPackStopLeavesNothing: a stop ends with ErrStopped and no file under
// the final name or the partial one (spec 3.7).
func TestPackStopLeavesNothing(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.bin": noise(3 << 20), "b.bin": noise(3 << 20)})
	w, err := Walk(src, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "stopped.zip")
	calls := 0
	_, err = Pack(dst, w, PackOptions{Level: 1, Name: "x", Stop: func() bool { calls++; return calls > 3 }})
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("want ErrStopped, got %v", err)
	}
	for _, p := range []string{dst, dst + ".filedo-partial"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is left after a stop", p)
		}
	}
}

func TestPackStopAfterLastEntryLeavesNothing(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "empty.zip")
	_, err := Pack(dst, Walked{}, PackOptions{Level: 0, Name: "empty", Stop: func() bool { return true }})
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("want ErrStopped, got %v", err)
	}
	for _, p := range []string{dst, dst + ".filedo-partial"} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is left after a stop: %v", p, err)
		}
	}
}

// TestPackKeepLeavesThePartialName: the intermediate zip of a sealed backup
// never carries the final name.
func TestPackKeepLeavesThePartialName(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("x")})
	w, _ := Walk(src, WalkOptions{})
	dst := filepath.Join(t.TempDir(), "sealed.zip")
	res, err := Pack(dst, w, PackOptions{Level: 1, Name: "x", Keep: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != dst+".filedo-partial" {
		t.Fatalf("Keep returned %s", res.Path)
	}
	if _, err := os.Stat(dst); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("Keep committed the final name")
	}
}

// TestWalkLinks: a link refuses the walk naming it, unless SkipLinks lists it
// instead (D7).
func TestWalkLinks(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"a.txt": []byte("x"), "link/inside.txt": []byte("y")})
	isLink := func(p string) bool { return filepath.Base(p) == "link" }
	_, err := Walk(src, WalkOptions{IsLink: isLink})
	var le *LinkError
	if !errors.As(err, &le) || filepath.Base(le.Path) != "link" {
		t.Fatalf("want a LinkError naming link, got %v", err)
	}
	w, err := Walk(src, WalkOptions{IsLink: isLink, SkipLinks: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.SkippedLinks) != 1 || w.SkippedLinks[0] != "link" || w.Files != 1 {
		t.Fatalf("skip-links walk: %+v", w)
	}
}

// TestWalkSkipsSystemFilesAtTheRootOnly (D6).
func TestWalkSkipsSystemFilesAtTheRootOnly(t *testing.T) {
	src := t.TempDir()
	writeTree(t, src, map[string][]byte{"pagefile.sys": []byte("p"), "sub/pagefile.sys": []byte("q"), "a.txt": []byte("a")})
	w, err := Walk(src, WalkOptions{SkipSystem: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.SkippedSystem) != 1 || w.Files != 2 {
		t.Fatalf("system skip: %+v", w)
	}
}

// TestChooseLevel is the rule the Q3 measurements froze.
func TestChooseLevel(t *testing.T) {
	cases := []struct {
		name   string
		saving map[int]float64
		want   int
	}{
		{"media", map[int]float64{1: -5.1, 6: 0.3, 9: 0.3}, 0},
		{"text, level 6 adds a lot, 9 nothing", map[int]float64{1: 29.6, 6: 37.5, 9: 37.5}, 6},
		{"sources, 9 adds under a point", map[int]float64{1: 33.9, 6: 43.7, 9: 44.4}, 6},
		{"9 adds a point", map[int]float64{1: 30, 6: 40, 9: 41}, 9},
		{"6 adds under a point", map[int]float64{1: 70, 6: 70.5, 9: 75}, 1},
		{"deadline after level 1", map[int]float64{1: 40}, 1},
		{"deadline after level 1, media", map[int]float64{1: 1}, 0},
		{"nothing measured", map[int]float64{}, 6},
	}
	for _, c := range cases {
		if got := chooseLevel(ProbeResult{SampleBytes: 1, Saving: c.saving}); got != c.want {
			t.Errorf("%s: level %d, want %d", c.name, got, c.want)
		}
	}
	if got := chooseLevel(ProbeResult{}); got != 0 {
		t.Errorf("empty sample: level %d, want 0", got)
	}
}

// TestProbeMediaAndText: incompressible content stores, text compresses, and
// the probe ends inside its deadline.
func TestProbeMediaAndText(t *testing.T) {
	media := t.TempDir()
	writeTree(t, media, map[string][]byte{"a.jpg": noise(2 << 20), "b.mp4": noise(2 << 20)})
	text := t.TempDir()
	writeTree(t, text, map[string][]byte{"a.txt": bytes.Repeat([]byte("the quick brown fox jumps over the lazy dog, "), 50000)})
	for _, c := range []struct {
		dir  string
		zero bool
	}{{media, true}, {text, false}} {
		w, err := Walk(c.dir, WalkOptions{})
		if err != nil {
			t.Fatal(err)
		}
		p, err := Probe(w, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if (p.Level == 0) != c.zero {
			t.Errorf("%s: level %d", c.dir, p.Level)
		}
		if p.Seconds > ProbeDeadline.Seconds() {
			t.Errorf("the probe took %.1f s", p.Seconds)
		}
	}
}

// TestProbeDeadline: a clock past the deadline stops the probe and it still
// answers.
func TestProbeDeadline(t *testing.T) {
	dir := t.TempDir()
	writeTree(t, dir, map[string][]byte{"a.txt": bytes.Repeat([]byte("abc "), 10000)})
	w, _ := Walk(dir, WalkOptions{})
	saved := probeClock
	defer func() { probeClock = saved }()
	start := time.Now()
	calls := 0
	probeClock = func() time.Time {
		calls++
		if calls > 3 {
			return start.Add(time.Minute)
		}
		return start
	}
	p, err := Probe(w, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !p.TimedOut {
		t.Fatal("the probe did not report its deadline")
	}
}

// TestScreenEntriesRefusesHostileNames is the hostile-vector set of spec 3.6:
// every one refuses the whole archive.
func TestScreenEntriesRefusesHostileNames(t *testing.T) {
	hostile := []string{
		"../x", "a/../../x", `..\x`, `a\..\..\x`, "/abs", `\abs`, "C:/x", `C:\x`, "c:x",
		"//server/share/x", `\\server\share\x`, "a/./b", "a//b", "CON", "con.txt", "a/NUL", "LPT1.log",
		"com9", "aux.tar.gz", "trail.", "trail ", "a:stream", "a\x01b", "q?", "a*b", `a"b`, "a|b", "a<b", "a>b",
	}
	for _, name := range hostile {
		zr := zipOf(t, []string{"ok.txt", name})
		if _, err := ScreenEntries(zr.File); !errors.Is(err, ErrHostileEntry) {
			t.Errorf("%q was not refused: %v", name, err)
		}
	}
	for _, pair := range [][]string{{"A.txt", "a.TXT"}, {"x", "x/y"}, {"Dir/a", "dir"}} {
		zr := zipOf(t, pair)
		if _, err := ScreenEntries(zr.File); !errors.Is(err, ErrHostileEntry) {
			t.Errorf("%q was not refused as one name: %v", pair, err)
		}
	}
	zr := zipOf(t, []string{"dir/", "dir/a.txt", "DIR/b.txt", "con-not-reserved.txt", "nul2.txt"})
	if _, err := ScreenEntries(zr.File); err != nil {
		t.Errorf("an ordinary archive was refused: %v", err)
	}
}

// TestUnpackDamagedEntry: a byte flipped inside a stored entry is the
// archive's checksum failure, a judgement about the archive.
func TestUnpackDamagedEntry(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fw, _ := zw.CreateHeader(&zip.FileHeader{Name: "a.bin", Method: zip.Store})
	payload := bytes.Repeat([]byte{0x41}, 4096)
	_, _ = fw.Write(payload)
	_ = zw.Close()
	b := buf.Bytes()
	i := bytes.Index(b, payload)
	b[i+100] ^= 0xFF
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	s, err := ScreenEntries(zr.File)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if _, _, err := Unpack(root, s, UnpackOptions{}); !errors.Is(err, ErrChecksum) {
		t.Fatalf("want ErrChecksum, got %v", err)
	}
}

func TestUnpackStoppedAfterLastEntry(t *testing.T) {
	zr := zipOf(t, []string{"last.txt"})
	entries, err := ScreenEntries(zr.File)
	if err != nil {
		t.Fatal(err)
	}
	stopped := false
	_, _, err = Unpack(t.TempDir(), entries, UnpackOptions{
		Stop:     func() bool { return stopped },
		Progress: func(_, _ int64) { stopped = true },
	})
	if !errors.Is(err, ErrStopped) {
		t.Fatalf("stop during the last entry: got %v, want ErrStopped", err)
	}
}

func zipOf(t *testing.T, names []string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(n, "/") {
			_, _ = w.Write([]byte("x"))
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	// With GODEBUG zipinsecurepath=0 the reader also reports a name it finds
	// insecure; the archive is still read, and the screening is what refuses.
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		t.Fatal(err)
	}
	return zr
}
