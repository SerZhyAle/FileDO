package fdsec

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// The suite-3 tests (FDSEC-FORMAT.md section 17, SP-0009 D1). The exit
// criterion they carry: a byte-exact round trip over a real directory tree -
// an empty directory, an empty file, a file crossing several chunks, a tree
// several levels deep - and a hostile manifest built by the test itself
// refused as damaged with nothing written outside the destination.

// memSink collects an unpacked tree in memory.
type memSink struct {
	dirs  []string
	files map[string]*bytes.Buffer
	order []string
}

func newMemSink() *memSink { return &memSink{files: map[string]*bytes.Buffer{}} }

func (s *memSink) Dir(e TreeEntry) error {
	s.dirs = append(s.dirs, e.Path)
	s.order = append(s.order, e.Path)
	return nil
}

func (s *memSink) File(e TreeEntry) (io.WriteCloser, error) {
	b := &bytes.Buffer{}
	s.files[e.Path] = b
	s.order = append(s.order, e.Path)
	return nopWriteCloser{b}, nil
}

// memTree builds an in-memory Tree from a path -> content map; a nil content
// is a directory. Entries keep the map's iteration order - deliberately not
// sorted, so the writer's sort is what the tests see.
func memTree(name string, items map[string][]byte, order []string) *Tree {
	t := &Tree{Meta: TreeMetadata{
		Name:       name,
		EncodedAt:  time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC),
		CreatedAt:  time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC),
		AccessedAt: time.Date(2025, 6, 7, 8, 9, 10, 0, time.UTC),
		ModifiedAt: time.Date(2026, 8, 19, 23, 59, 59, 0, time.UTC),
	}}
	for _, p := range order {
		i := len(p) // a time that follows the entry, never its position in the list
		c := items[p]
		e := TreeEntry{
			Path:       p,
			Kind:       KindFile,
			Size:       int64(len(c)),
			CreatedAt:  time.Date(2024, 1, 1, 0, 0, i, 0, time.UTC),
			AccessedAt: time.Date(2024, 2, 1, 0, 0, i, 0, time.UTC),
			ModifiedAt: time.Date(2024, 3, 1, 0, 0, i, 0, time.UTC),
		}
		if c == nil {
			e.Kind, e.Size = KindDir, 0
		}
		t.Entries = append(t.Entries, e)
	}
	t.Open = func(p string) (io.ReadCloser, error) {
		c, ok := items[p]
		if !ok || c == nil {
			return nil, os.ErrNotExist
		}
		return io.NopCloser(bytes.NewReader(c)), nil
	}
	return t
}

func packTreeBytes(t *testing.T, tr *Tree, cred Credential, p Params) []byte {
	t.Helper()
	var out bytes.Buffer
	info, err := PackTree(&out, tr, cred, p)
	if err != nil {
		t.Fatalf("PackTree: %v", err)
	}
	if info.TotalLen != int64(out.Len()) {
		t.Fatalf("PackTree reported %d bytes, wrote %d", info.TotalLen, out.Len())
	}
	return out.Bytes()
}

func unpackTreeBytes(b []byte, cred Credential) (*memSink, TreeMetadata, []TreeEntry, error) {
	c, err := Open(bytes.NewReader(b), cred)
	if err != nil {
		return nil, TreeMetadata{}, nil, err
	}
	s := newMemSink()
	tm, es, err := c.UnpackTree(s)
	return s, tm, es, err
}

// writeDiskTree lays out a tree on disk, creating entries in the given order.
func writeDiskTree(t *testing.T, root string, items map[string][]byte, order []string) {
	t.Helper()
	for _, p := range order {
		full := filepath.Join(root, filepath.FromSlash(p))
		if items[p] == nil {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, items[p], 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// readDiskTree reads a tree back: path -> content, nil for a directory.
func readDiskTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	got := map[string][]byte{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if fi.IsDir() {
			got[rel] = nil
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if b == nil {
			b = []byte{}
		}
		got[rel] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func sameTree(t *testing.T, got, want map[string][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("restored tree has %d entries, want %d\n got %v\nwant %v", len(got), len(want), keys(got), keys(want))
	}
	for p, w := range want {
		g, ok := got[p]
		if !ok {
			t.Fatalf("restored tree lacks %s", p)
		}
		if (w == nil) != (g == nil) {
			t.Fatalf("%s: kind differs after the round trip", p)
		}
		if w != nil && !bytes.Equal(g, w) {
			t.Fatalf("%s: %d bytes back, want %d", p, len(g), len(w))
		}
	}
}

func keys(m map[string][]byte) []string {
	var k []string
	for p := range m {
		k = append(k, p)
	}
	sort.Strings(k)
	return k
}

func patterned(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*7) ^ seed
	}
	return b
}

// sampleItems is the D1 tree: an empty directory, an empty file, a file
// crossing several chunks (P = 1008 under fastParams), and depth.
func sampleItems() (map[string][]byte, []string) {
	items := map[string][]byte{
		"zeta.bin":                  patterned(5000, 1), // 5 chunks
		"a":                         nil,
		"a/b":                       nil,
		"a/b/c":                     nil,
		"a/b/c/deep.txt":            []byte("deep"),
		"a/b/c/d":                   nil,
		"a/b/c/d/e":                 nil,
		"a/b/c/d/e/deeper.bin":      patterned(2100, 9), // 3 chunks
		"empty-dir":                 nil,
		"empty.txt":                 {},
		"Mixed Case":                nil,
		"Mixed Case/имя файла.txt":  []byte("unicode name"),
		"Mixed Case/second (1).dat": patterned(1008, 3), // exactly one full chunk
	}
	// Creation order that is not the sorted order.
	order := []string{"zeta.bin", "empty-dir", "Mixed Case", "Mixed Case/second (1).dat", "a", "a/b", "a/b/c", "a/b/c/d", "a/b/c/d/e", "a/b/c/d/e/deeper.bin", "a/b/c/deep.txt", "empty.txt", "Mixed Case/имя файла.txt"}
	return items, order
}

func TestTree_RoundTripOnDisk(t *testing.T) {
	items, order := sampleItems()
	base := t.TempDir()
	src := filepath.Join(base, "My Folder")
	writeDiskTree(t, src, items, order)
	if err := os.Mkdir(src+"-unused", 0o755); err != nil { // a sibling the restore must not touch
		t.Fatal(err)
	}

	tree, err := ScanTree(src, ScanOptions{})
	if err != nil {
		t.Fatalf("ScanTree: %v", err)
	}
	if tree.Meta.Name != "My Folder" {
		t.Fatalf("root name %q", tree.Meta.Name)
	}
	cred := NewCredential("tree-pw")
	dst := filepath.Join(base, "My Folder.fd-sec")
	info, err := PackTreeFile(dst, tree, cred, fastParams())
	if err != nil {
		t.Fatalf("PackTreeFile: %v", err)
	}
	fi, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() != info.TotalLen || fi.Size()%512 != 0 {
		t.Fatalf("container is %d bytes, info says %d, alignment 512", fi.Size(), info.TotalLen)
	}
	if err := ScreenLength(fi.Size()); err != nil {
		t.Fatalf("a directory container fails the length screen: %v", err)
	}

	f, err := os.Open(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	c, err := Open(f, cred)
	if err != nil {
		t.Fatal(err)
	}
	if !c.IsTree() || c.Suite() != SuiteID3 {
		t.Fatalf("suite %d, want 3", c.Suite())
	}
	out := filepath.Join(base, "restore")
	if err := os.Mkdir(out, 0o755); err != nil {
		t.Fatal(err)
	}
	tm, entries, err := c.UnpackTreeInto(out)
	if err != nil {
		t.Fatalf("UnpackTreeInto: %v", err)
	}
	if tm.Name != "My Folder" || tm.Entries != int64(len(items)) {
		t.Fatalf("directory block: %+v", tm)
	}
	var want int64
	for _, c := range items {
		want += int64(len(c))
	}
	if tm.TotalSize != want {
		t.Fatalf("total size %d, want %d", tm.TotalSize, want)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i-1].Path >= entries[i].Path {
			t.Fatalf("entries are not in byte-wise order: %q then %q", entries[i-1].Path, entries[i].Path)
		}
	}
	sameTree(t, readDiskTree(t, out), items)

	hi, err := ReadHeaderInfo(f, cred)
	if err != nil {
		t.Fatal(err)
	}
	if !hi.IsTree || hi.TreeEntries != int64(len(items)) || hi.TreeSize != want || hi.SuiteID != SuiteID3 {
		t.Fatalf("header info does not describe the tree: %+v", hi)
	}
}

func TestTree_EmptyRootIsAValidContainer(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "nothing")
	if err := os.Mkdir(src, 0o755); err != nil {
		t.Fatal(err)
	}
	tree, err := ScanTree(src, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	cred := NewCredential("")
	dst := filepath.Join(base, "nothing.fd-sec")
	if _, err := PackTreeFile(dst, tree, cred, Params{}); err != nil {
		t.Fatalf("an empty folder was refused: %v", err)
	}
	f, _ := os.Open(dst)
	defer f.Close()
	c, err := Open(f, cred)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(base, "out")
	os.Mkdir(out, 0o755)
	tm, entries, err := c.UnpackTreeInto(out)
	if err != nil || tm.Entries != 0 || len(entries) != 0 {
		t.Fatalf("empty tree: %v, %+v, %d entries", err, tm, len(entries))
	}
	if got := readDiskTree(t, out); len(got) != 0 {
		t.Fatalf("an empty folder restored with %d entries", len(got))
	}
}

func TestTree_DeterministicUnderFixedRandomness(t *testing.T) {
	items, order := sampleItems()
	rev := append([]string(nil), order...)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	pack := func(o []string) []byte {
		saved := randSource
		randSource = bytes.NewReader(vecStream("fdsec-tree-determinism", 1<<16))
		defer func() { randSource = saved }()
		return packTreeBytes(t, memTree("root", items, o), NewCredential("pw"), fastParams())
	}
	if !bytes.Equal(pack(order), pack(rev)) {
		t.Fatal("two packs of the same tree listed in different orders differ under the same randomness")
	}
}

func TestTree_SuitesAnswerOnlyTheirOwnQuestion(t *testing.T) {
	cred := NewCredential("pw")
	items, order := sampleItems()
	treeC := packTreeBytes(t, memTree("root", items, order), cred, fastParams())
	if _, err := Unpack(io.Discard, bytes.NewReader(treeC), cred); !errors.Is(err, ErrTreeContainer) {
		t.Fatalf("Unpack of a directory container: %v, want ErrTreeContainer", err)
	}
	fileC := packBytes(t, []byte("one file"), fixedTimes, cred, fastParams())
	c, err := Open(bytes.NewReader(fileC), cred)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.UnpackTree(newMemSink()); !errors.Is(err, ErrFileContainer) {
		t.Fatalf("UnpackTree of a file container: %v, want ErrFileContainer", err)
	}
	if hi, err := ReadHeaderInfo(bytes.NewReader(fileC), cred); err != nil || hi.IsTree {
		t.Fatalf("a file container reported as a tree: %+v, %v", hi, err)
	}
}

func TestTree_OutcomeClasses(t *testing.T) {
	cred := NewCredential("pw")
	items, order := sampleItems()
	good := packTreeBytes(t, memTree("root", items, order), cred, fastParams())
	if _, _, _, err := unpackTreeBytes(good, cred); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	t.Run("wrong credential", func(t *testing.T) {
		if _, _, _, err := unpackTreeBytes(good, NewCredential("not-it")); !errors.Is(err, ErrCredentialOrTamper) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a flipped byte in every region is tamper, never damage", func(t *testing.T) {
		// The directory block, the manifest stream and a file stream.
		for _, off := range []int{preMeta + 100, 4096 + 10, len(good) - 700} {
			b := bytes.Clone(good)
			b[off] ^= 0x40
			_, _, _, err := unpackTreeBytes(b, cred)
			if !errors.Is(err, ErrCredentialOrTamper) {
				t.Fatalf("flip at %d: got %v", off, err)
			}
		}
	})
	t.Run("truncation is damage", func(t *testing.T) {
		for _, cut := range []int{512, 1024, len(good) / 2} {
			_, _, _, err := unpackTreeBytes(good[:len(good)-cut], cred)
			if !errors.Is(err, ErrDamaged) {
				t.Fatalf("cut %d: got %v", cut, err)
			}
		}
	})
	t.Run("one cluster too many is damage", func(t *testing.T) {
		b := append(bytes.Clone(good), make([]byte, 512)...)
		if _, _, _, err := unpackTreeBytes(b, cred); !errors.Is(err, ErrDamaged) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("swapping two equal-size file streams is tamper", func(t *testing.T) {
		its := map[string][]byte{"a.bin": patterned(300, 1), "b.bin": patterned(300, 2)}
		c := packTreeBytes(t, memTree("r", its, []string{"a.bin", "b.bin"}), cred, fastParams())
		// Stream layout under fastParams: pre_len 5120, manifest stream in
		// [5120, 5632), a.bin at 5632, b.bin at 6144.
		b := bytes.Clone(c)
		copy(b[5632:6144], c[6144:6656])
		copy(b[6144:6656], c[5632:6144])
		if _, _, _, err := unpackTreeBytes(b, cred); !errors.Is(err, ErrCredentialOrTamper) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestTree_ReadBackFailureLeavesNothing(t *testing.T) {
	base := t.TempDir()
	items, order := sampleItems()
	src := filepath.Join(base, "src")
	writeDiskTree(t, src, items, order)
	tree, err := ScanTree(src, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	beforeReadBack = func(tmp string) {
		b, _ := os.ReadFile(tmp)
		b[len(b)-600] ^= 1
		os.WriteFile(tmp, b, 0o644)
	}
	defer func() { beforeReadBack = nil }()
	dst := filepath.Join(base, "src.fd-sec")
	if _, err := PackTreeFile(dst, tree, NewCredential("pw"), fastParams()); err == nil {
		t.Fatal("a corrupted read-back was accepted")
	}
	des, _ := os.ReadDir(base)
	for _, d := range des {
		if d.Name() != "src" {
			t.Fatalf("a failed pack left %s behind", d.Name())
		}
	}
}

func TestTree_FileChangedAfterScanIsRefused(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	writeDiskTree(t, src, map[string][]byte{"f.txt": []byte("before")}, []string{"f.txt"})
	tree, err := ScanTree(src, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(src, "f.txt"), []byte("after, and longer"), 0o644)
	if _, err := PackTreeFile(filepath.Join(base, "x.fd-sec"), tree, NewCredential("pw"), fastParams()); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("a file that changed size after the scan was packed: %v", err)
	}
}

// hostilePack writes a suite-3 container whose manifest breaks the format's
// rules - the writer's own checks bypassed, exactly as a crafted container
// would be - so the reader's checks are the only line left.
func hostilePack(t *testing.T, entries []TreeEntry, content map[string][]byte, cred Credential) []byte {
	t.Helper()
	tr := &Tree{
		Meta: TreeMetadata{Name: "evil"},
		Open: func(p string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(content[p])), nil
		},
	}
	var total int64
	for _, e := range entries {
		total += e.Size
	}
	var out bytes.Buffer
	if _, err := packTree(&out, tr, entries, total, cred, fastParams(), streamOpts{}); err != nil {
		t.Fatalf("hostile pack: %v", err)
	}
	return out.Bytes()
}

func TestTree_HostileManifestIsDamageAndWritesNothing(t *testing.T) {
	cred := NewCredential("pw")
	payload := []byte("escaped")
	file := func(p string) TreeEntry { return TreeEntry{Path: p, Kind: KindFile, Size: int64(len(payload))} }
	dir := func(p string) TreeEntry { return TreeEntry{Path: p, Kind: KindDir} }

	cases := map[string][]TreeEntry{
		"dot-dot":             {file("../escaped.txt")},
		"nested dot-dot":      {dir("a"), file("a/../../escaped.txt")},
		"absolute":            {file("/escaped.txt")},
		"drive letter":        {file("C:/escaped.txt")},
		"drive-relative":      {file("C:escaped.txt")},
		"backslash":           {file(`..\escaped.txt`)},
		"UNC":                 {file(`//server/share/escaped.txt`)},
		"empty component":     {dir("a"), file("a//escaped.txt")},
		"trailing slash":      {file("escaped.txt/")},
		"dot":                 {file("./escaped.txt")},
		"device name":         {file("CON")},
		"trailing dot":        {file("escaped.txt.")},
		"control character":   {file("esc\x01aped.txt")},
		"stream name":         {file("a.txt:stream")},
		"missing parent":      {file("nodir/escaped.txt")},
		"parent is a file":    {file("a"), file("a/escaped.txt")},
		"out of order":        {file("b.txt"), file("a.txt")},
		"duplicate":           {file("a.txt"), file("a.txt")},
		"directory with size": {{Path: "d", Kind: KindDir, Size: 7}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			content := map[string][]byte{}
			for _, e := range entries {
				if e.Kind == KindFile {
					content[e.Path] = payload
				}
			}
			b := hostilePack(t, entries, content, cred)
			base := t.TempDir()
			root := filepath.Join(base, "dest", "root")
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatal(err)
			}
			c, err := Open(bytes.NewReader(b), cred)
			if err != nil {
				t.Fatal(err)
			}
			_, _, err = c.UnpackTreeInto(root)
			if !errors.Is(err, ErrDamaged) {
				t.Fatalf("got %v, want damaged", err)
			}
			if strings.Contains(err.Error(), "escaped") {
				t.Fatalf("the refusal echoes the hostile path: %v", err)
			}
			// Nothing anywhere under base but the two directories the test made.
			var found []string
			filepath.Walk(base, func(p string, fi os.FileInfo, err error) error {
				if err == nil && !fi.IsDir() {
					found = append(found, p)
				}
				return nil
			})
			if len(found) != 0 || len(readDiskTree(t, root)) != 0 {
				t.Fatalf("a hostile manifest wrote %v", found)
			}
		})
	}

	t.Run("a reserved kind is unsupported, by name", func(t *testing.T) {
		b := hostilePack(t, []TreeEntry{{Path: "link", Kind: 3}}, nil, cred)
		if _, _, _, err := unpackTreeBytes(b, cred); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestTree_SafeJoinRefusesEveryEscape(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	for _, p := range []string{"..", "../x", "a/../../x", "/x", "C:/x", `C:\x`, `\\srv\sh\x`, "", "a//b", "a/", ".", "x:y"} {
		if _, err := SafeJoin(root, p); !errors.Is(err, ErrDamaged) {
			t.Errorf("SafeJoin(%q) = %v, want damaged", p, err)
		}
	}
	got, err := SafeJoin(root, "a/b c/d.txt")
	if err != nil || got != filepath.Join(root, "a", "b c", "d.txt") {
		t.Fatalf("SafeJoin of an ordinary path: %q, %v", got, err)
	}
}

func TestTree_ReparsePointsAreRefusedNotFollowed(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "src")
	writeDiskTree(t, src, map[string][]byte{"sub": nil, "sub/f.txt": []byte("x")}, []string{"sub", "sub/f.txt"})

	t.Run("the caller's classification is honoured", func(t *testing.T) {
		flagged := filepath.Join(src, "sub")
		_, err := ScanTree(src, ScanOptions{IsReparsePoint: func(p string) bool { return p == flagged }})
		if err == nil || !strings.Contains(err.Error(), flagged) || !strings.Contains(err.Error(), "reparse point") {
			t.Fatalf("a flagged reparse point was not refused by name: %v", err)
		}
	})
	t.Run("at the root", func(t *testing.T) {
		_, err := ScanTree(src, ScanOptions{IsReparsePoint: func(p string) bool { return p == src }})
		if err == nil {
			t.Fatal("a reparse point at the root was packed")
		}
	})
	t.Run("a real junction", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("junctions are a Windows construct")
		}
		outside := filepath.Join(base, "outside")
		os.Mkdir(outside, 0o755)
		os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside the tree"), 0o644)
		j := filepath.Join(src, "jn")
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, outside).CombinedOutput(); err != nil {
			t.Skipf("cannot create a junction here: %v %s", err, out)
		}
		defer os.Remove(j)
		_, err := ScanTree(src, ScanOptions{})
		if err == nil || !strings.Contains(err.Error(), "jn") {
			t.Fatalf("a junction inside the tree was not refused by name: %v", err)
		}
	})
	t.Run("a real symbolic link", func(t *testing.T) {
		l := filepath.Join(src, "ln")
		if err := os.Symlink(filepath.Join(src, "sub", "f.txt"), l); err != nil {
			t.Skipf("cannot create a symbolic link here: %v", err)
		}
		defer os.Remove(l)
		if _, err := ScanTree(src, ScanOptions{}); err == nil {
			t.Fatal("a symbolic link inside the tree was packed")
		}
	})
}

func TestTree_RestoreNeverOverwrites(t *testing.T) {
	cred := NewCredential("pw")
	b := packTreeBytes(t, memTree("r", map[string][]byte{"f.txt": []byte("new")}, []string{"f.txt"}), cred, fastParams())
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "f.txt"), []byte("old"), 0o644)
	c, _ := Open(bytes.NewReader(b), cred)
	if _, _, err := c.UnpackTreeInto(root); err == nil {
		t.Fatal("a restore into a non-empty root was allowed")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "f.txt")); string(got) != "old" {
		t.Fatal("an existing file was overwritten")
	}
}
