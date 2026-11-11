package backupzip

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"filedo/fsx"
)

// ErrHostileEntry marks an archive whose entry names would write outside the
// destination or create a name Windows reserves. The whole restore is refused
// before the first byte is written; the archive is the defect (spec 3.6).
var ErrHostileEntry = errors.New("the archive is refused")

// Screened is one zip entry that passed screening.
type Screened struct {
	Rel  string // '/'-separated, cleaned, never empty
	Dir  bool
	File *zip.File // nil for a folder only implied by a file's path
}

// reservedStems are the device names Windows answers instead of a file, with
// or without an extension (FDSEC-FORMAT 1.6 section 22, the same set).
var reservedStems = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// checkComponent refuses one path component that Windows would not create as
// written: empty, `.` or `..`, a reserved device stem, a trailing dot or space
// (Windows drops them, so two names could become one), a character Windows
// refuses, a drive letter or an alternate stream (any colon), a control
// character.
func checkComponent(c string) error {
	switch {
	case c == "":
		return errors.New("an empty path component")
	case c == "." || c == "..":
		return errors.New("a '.' or '..' component, which climbs out of the destination")
	case strings.HasSuffix(c, ".") || strings.HasSuffix(c, " "):
		return errors.New("a name ending in a dot or a space")
	case strings.ContainsAny(c, `<>:"|?*\/`):
		return errors.New("a character Windows refuses in a name, a drive letter or a stream")
	}
	for _, r := range c {
		if r < 0x20 || r == 0x7f {
			return errors.New("a control character in a name")
		}
	}
	stem := c
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if reservedStems[strings.ToUpper(strings.TrimRight(stem, " "))] {
		return fmt.Errorf("the reserved device name %s", strings.ToUpper(stem))
	}
	return nil
}

// cleanName turns a stored entry name into a '/'-separated relative path, or
// refuses it: an absolute path, a drive letter, a UNC prefix and every
// component checkComponent refuses.
func cleanName(name string) (rel string, dir bool, err error) {
	n := strings.ReplaceAll(name, `\`, "/")
	if n == "" {
		return "", false, errors.New("an empty name")
	}
	if strings.HasPrefix(n, "/") {
		return "", false, errors.New("an absolute path")
	}
	dir = strings.HasSuffix(n, "/")
	n = strings.TrimSuffix(n, "/")
	parts := strings.Split(n, "/")
	for _, p := range parts {
		if err := checkComponent(p); err != nil {
			return "", false, err
		}
	}
	return n, dir, nil
}

// ScreenEntries checks every entry of an archive before anything is written,
// and refuses the whole archive at the first entry that fails. Beside the name
// rules of cleanName it refuses a link or a device stored as an entry, two
// entries that are one name to Windows (case and Unicode folding aside, a
// folder ignores case), and a file whose name is also used as a folder.
func ScreenEntries(files []*zip.File) ([]Screened, error) {
	refuse := func(name, why string) error {
		return fmt.Errorf("%w: the entry %q is %s; nothing was written", ErrHostileEntry, name, why)
	}
	seen := map[string]int{} // folded path -> index in out
	var out []Screened
	add := func(rel string, dir bool, f *zip.File, stored string) error {
		key := strings.Map(unicode.ToUpper, rel)
		if i, ok := seen[key]; ok {
			if out[i].Dir && dir {
				// A folder named twice (implied by a file, then stored) is
				// one folder; the stored entry brings its time.
				if out[i].File == nil {
					out[i].File = f
				}
				return nil
			}
			return refuse(stored, "a second entry with a name Windows treats as the same")
		}
		seen[key] = len(out)
		out = append(out, Screened{Rel: rel, Dir: dir, File: f})
		return nil
	}
	for _, f := range files {
		rel, dir, err := cleanName(f.Name)
		if err != nil {
			return nil, refuse(f.Name, err.Error())
		}
		m := f.Mode()
		if m&(fs.ModeSymlink|fs.ModeDevice|fs.ModeNamedPipe|fs.ModeSocket|fs.ModeCharDevice|fs.ModeIrregular) != 0 {
			return nil, refuse(f.Name, "a link or a device, which a backup never holds")
		}
		if m.IsDir() {
			dir = true
		}
		// Every parent is a folder; a file of the same name refuses the pair.
		parts := strings.Split(rel, "/")
		for i := 1; i < len(parts); i++ {
			if err := add(strings.Join(parts[:i], "/"), true, nil, f.Name); err != nil {
				return nil, err
			}
		}
		if dir {
			if err := add(rel, true, f, f.Name); err != nil {
				return nil, err
			}
			continue
		}
		if err := add(rel, false, f, f.Name); err != nil {
			return nil, err
		}
	}
	// Parents before children, so folders exist before what they hold.
	sort.SliceStable(out, func(i, j int) bool { return strings.Count(out[i].Rel, "/") < strings.Count(out[j].Rel, "/") })
	return out, nil
}

// TotalSize is the uncompressed size the screened files declare - what the
// destination volume must hold.
func TotalSize(s []Screened) (files int64, bytes uint64) {
	for _, e := range s {
		if !e.Dir && e.File != nil {
			files++
			bytes += e.File.UncompressedSize64
		}
	}
	return files, bytes
}

// UnpackOptions tunes Unpack.
type UnpackOptions struct {
	Stop     func() bool
	Progress func(files int64, bytes int64)
}

// ErrChecksum marks an entry whose bytes do not match the checksum the zip
// stored for it: the archive is damaged, which is a judgement about it.
var ErrChecksum = errors.New("the archive is damaged")

// Unpack writes the screened entries under root, which must exist and be
// empty - the caller's partial folder. Every path is joined under root and
// checked to stay there, a second line behind the screening. A stop ends with
// ErrStopped; the caller removes root on every failure.
func Unpack(root string, entries []Screened, o UnpackOptions) (files, bytes int64, err error) {
	base := filepath.Clean(root)
	buf := make([]byte, 1<<20)
	type dirTime struct {
		path string
		f    *zip.File
	}
	var dirs []dirTime
	for _, e := range entries {
		if o.Stop != nil && o.Stop() {
			return files, bytes, ErrStopped
		}
		target := filepath.Join(base, filepath.FromSlash(e.Rel))
		if !strings.HasPrefix(target, base+string(filepath.Separator)) {
			return files, bytes, fmt.Errorf("%w: the entry %q resolves outside the destination; nothing more was written", ErrHostileEntry, e.Rel)
		}
		long := fsx.LongPath(target)
		if e.Dir {
			if err := os.MkdirAll(long, 0o755); err != nil {
				return files, bytes, err
			}
			if e.File != nil {
				dirs = append(dirs, dirTime{long, e.File})
			}
			continue
		}
		n, err := writeEntry(long, e.File, buf, o, files, bytes)
		bytes += n
		if err != nil {
			return files, bytes, err
		}
		files++
	}
	// A folder's time is set after everything inside it was written, deepest
	// first, or writing its children would move it again.
	for i := len(dirs) - 1; i >= 0; i-- {
		t := dirs[i].f.Modified
		if !t.IsZero() {
			_ = os.Chtimes(dirs[i].path, t, t)
		}
	}
	if o.Stop != nil && o.Stop() {
		return files, bytes, ErrStopped
	}
	return files, bytes, nil
}

func writeEntry(path string, f *zip.File, buf []byte, o UnpackOptions, filesDone, bytesDone int64) (int64, error) {
	r, err := f.Open()
	if err != nil {
		if errors.Is(err, zip.ErrAlgorithm) {
			return 0, fmt.Errorf("the entry %q uses a compression method other than deflate or store: %w", f.Name, ErrHostileEntry)
		}
		return 0, err
	}
	defer r.Close()
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return 0, err
	}
	var n int64
	for {
		if o.Stop != nil && o.Stop() {
			out.Close()
			return n, ErrStopped
		}
		k, rerr := r.Read(buf)
		if k > 0 {
			if _, werr := out.Write(buf[:k]); werr != nil {
				out.Close()
				return n, werr
			}
			n += int64(k)
			if o.Progress != nil {
				o.Progress(filesDone, bytesDone+n)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			var corrupt flate.CorruptInputError
			if errors.Is(rerr, zip.ErrChecksum) || errors.Is(rerr, zip.ErrFormat) || errors.Is(rerr, io.ErrUnexpectedEOF) || errors.As(rerr, &corrupt) {
				return n, fmt.Errorf("%w: the entry %q does not match its stored checksum", ErrChecksum, f.Name)
			}
			return n, rerr
		}
	}
	if err := out.Close(); err != nil {
		return n, err
	}
	if t := f.Modified; !t.IsZero() {
		_ = os.Chtimes(path, t, t)
	}
	return n, nil
}
