package backupzip

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"filedo/fsx"
)

// CommentPrefix opens the zip comment a backup writes. What follows it is the
// source folder's name and nothing else: never its path, because a plain zip
// that carries the machine's folder structure hands it to whoever receives the
// file (SP-0198 D5). Restore reads the name back as its default folder name.
const CommentPrefix = "FileDO backup of: "

// SourceName reads the folder name a backup recorded in its comment, or ""
// when the comment is not one FileDO wrote or the name is not a plain folder
// name (a foreign zip, or a crafted one: the name never becomes a path).
func SourceName(comment string) string {
	if !strings.HasPrefix(comment, CommentPrefix) {
		return ""
	}
	name := strings.TrimSpace(strings.TrimPrefix(comment, CommentPrefix))
	if checkComponent(name) != nil {
		return ""
	}
	return name
}

// PackOptions tunes Pack.
type PackOptions struct {
	Level int    // 0 = store, 1-9 = deflate
	Name  string // the source folder's name, recorded in the comment
	Stop  func() bool
	Open  func(Entry) (io.ReadCloser, error) // nil opens Entry.Path
	// Progress is told the files and bytes packed so far, after each buffer.
	Progress func(files, bytes int64)
	// Keep leaves the finished zip under its partial name (dst +
	// fsx.PartialSuffix) instead of renaming it into place, and returns that
	// path: the intermediate zip of a sealed backup never carries a final
	// name, so nothing that looks like a finished backup is left in clear if
	// the seal does not finish (FDSEC-BEHAVIOUR 1.9 section 21).
	Keep bool
	// RegisterCleanup registers a forced-exit cleanup after the partial has
	// been created exclusively. The returned function unregisters it.
	RegisterCleanup func(func()) func()
}

// PackResult is what Pack wrote.
type PackResult struct {
	Path  string // the final zip, or its partial name under Keep
	Size  int64
	Files int64
	Bytes int64
}

// Pack writes the entries of w into a zip at dst. The zip is written under
// dst + fsx.PartialSuffix and renamed into place only when it is complete and
// flushed, and never over an existing file: a stop, a failure or a crash leaves
// nothing under the final name (spec 3.7). A stop ends with ErrStopped and the
// partial file removed.
func Pack(dst string, w Walked, o PackOptions) (PackResult, error) {
	if o.Level < 0 || o.Level > 9 {
		return PackResult{}, fmt.Errorf("zip level %d is outside 0-9", o.Level)
	}
	// Under Keep the zip never takes the name dst - it stays under its partial
	// name, created exclusively below - so a file already at dst (a plain backup
	// of the same minute) is no obstacle; the caller checks dst itself when it
	// will commit the zip there (keep-zip).
	if !o.Keep {
		if _, err := os.Lstat(dst); err == nil {
			return PackResult{}, fmt.Errorf("%s already exists; a backup never replaces a file - name another destination: %w", dst, fsx.ErrDestinationExists)
		} else if !errors.Is(err, os.ErrNotExist) {
			return PackResult{}, err
		}
	}
	open := o.Open
	if open == nil {
		open = func(e Entry) (io.ReadCloser, error) { return os.Open(e.Path) }
	}
	pf, err := fsx.CreatePartialExclusive(dst)
	if err != nil {
		return PackResult{}, err
	}
	if o.RegisterCleanup != nil {
		unregister := o.RegisterCleanup(func() { pf.Abort() })
		defer unregister()
	}
	partial := dst + fsx.PartialSuffix
	res := PackResult{Path: dst}
	fail := func(err error) (PackResult, error) {
		pf.Abort()
		return PackResult{}, err
	}

	zw := zip.NewWriter(pf.File)
	level := o.Level
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(out, level)
	})
	if err := zw.SetComment(CommentPrefix + o.Name); err != nil {
		return fail(err)
	}
	method := zip.Deflate
	if o.Level == 0 {
		method = zip.Store
	}
	buf := make([]byte, 1<<20)
	for _, e := range w.Entries {
		if o.Stop != nil && o.Stop() {
			return fail(ErrStopped)
		}
		h := &zip.FileHeader{Name: e.Rel, Method: method, Modified: e.ModTime}
		if e.Dir {
			h.Name += "/"
			h.Method = zip.Store
			if _, err := zw.CreateHeader(h); err != nil {
				return fail(err)
			}
			continue
		}
		// A file over 4 GiB needs the zip64 sizes in its local header, which
		// archive/zip writes only when it knows the size up front.
		h.UncompressedSize64 = uint64(e.Size)
		fw, err := zw.CreateHeader(h)
		if err != nil {
			return fail(err)
		}
		if err := copyEntry(fw, e, open, buf, o, &res); err != nil {
			return fail(err)
		}
		res.Files++
	}
	if err := zw.Close(); err != nil {
		return fail(err)
	}
	if o.Stop != nil && o.Stop() {
		return fail(ErrStopped)
	}
	if o.Keep {
		if err := pf.File.Sync(); err != nil {
			return fail(err)
		}
		st, err := pf.File.Stat()
		if err != nil {
			return fail(err)
		}
		if err := pf.File.Close(); err != nil {
			os.Remove(partial)
			return PackResult{}, err
		}
		res.Path, res.Size = partial, st.Size()
		return res, nil
	}
	if err := pf.Commit(time.Time{}, false); err != nil {
		return PackResult{}, err
	}
	if st, err := os.Stat(dst); err == nil {
		res.Size = st.Size()
	}
	return res, nil
}

func copyEntry(fw io.Writer, e Entry, open func(Entry) (io.ReadCloser, error), buf []byte, o PackOptions, res *PackResult) error {
	f, err := open(e)
	if err != nil {
		return err
	}
	defer f.Close()
	var n int64
	for {
		if o.Stop != nil && o.Stop() {
			return ErrStopped
		}
		k, rerr := f.Read(buf)
		if k > 0 {
			n += int64(k)
			// The zip's local header promised e.Size bytes; a file that grew
			// since the walk is cut there and reported, never written past.
			if n > e.Size {
				return fmt.Errorf("%s grew while it was being packed; run the backup again when nothing writes to it", e.Path)
			}
			if _, werr := fw.Write(buf[:k]); werr != nil {
				return werr
			}
			res.Bytes += int64(k)
			if o.Progress != nil {
				o.Progress(res.Files, res.Bytes)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if n != e.Size {
		return fmt.Errorf("%s shrank while it was being packed; run the backup again when nothing writes to it", e.Path)
	}
	return nil
}
