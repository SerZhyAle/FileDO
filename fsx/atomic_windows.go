package fsx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// B2 of SP-0027 (theme T4 of SP-0023): atomic, non-replacing writes.
//
// A copy writes `<name>.filedo-partial`, flushes it, sets its times, and only
// then renames it to the final name - with no replace unless the caller chose
// to replace that exact path. A failure, a stop or a crash therefore leaves
// either nothing under the final name or the whole file, never a truncated
// file that the next run would skip as "already copied" and that
// `compare .. del source` would trust. Go's os.Rename on Windows is
// MoveFileEx(REPLACE_EXISTING), so it is never the final step for user data.

// PartialSuffix marks a file that is still being written.
const PartialSuffix = ".filedo-partial"

// ErrDestinationExists is returned when a no-replace rename finds the final
// name taken - by another process, or by a file that appeared mid-copy.
var ErrDestinationExists = errors.New("destination already exists")

// IsPartialName reports whether a name is one of FileDO's in-progress files.
func IsPartialName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), PartialSuffix)
}

// RenameNoReplace moves src to dst; if dst exists it fails with
// ErrDestinationExists and changes nothing.
func RenameNoReplace(src, dst string) error {
	return moveFile(src, dst, false)
}

// RenameReplace moves src over dst - for the one case where the user chose to
// overwrite that exact path, and only after the new bytes are complete.
func RenameReplace(src, dst string) error {
	return moveFile(src, dst, true)
}

func moveFile(src, dst string, replace bool) error {
	from, err := windows.UTF16PtrFromString(LongPath(src))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(LongPath(dst))
	if err != nil {
		return err
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(from, to, flags); err != nil {
		if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_FILE_EXISTS {
			return &os.LinkError{Op: "rename", Old: src, New: dst, Err: ErrDestinationExists}
		}
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: err}
	}
	return nil
}

// PartialFile is one in-progress write.
type PartialFile struct {
	*os.File
	final   string
	partial string
	done    bool
}

// CreatePartial opens `<dst>.filedo-partial` for a fresh write. A leftover
// partial from an earlier failed run is replaced: it is ours and incomplete.
func CreatePartial(dst string) (*PartialFile, error) {
	partial := dst + PartialSuffix
	f, err := os.OpenFile(partial, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &PartialFile{File: f, final: dst, partial: partial}, nil
}

// commit flushes the partial, gives it the source's modification time (and
// access time), closes it and renames it into place. With replace false an
// existing destination is never overwritten.
func (p *PartialFile) Commit(modTime time.Time, replace bool) error {
	if p.done {
		return errors.New("partial file already finished")
	}
	if err := p.File.Sync(); err != nil {
		p.Abort()
		return err
	}
	if err := p.File.Close(); err != nil {
		p.Abort()
		return err
	}
	if !modTime.IsZero() {
		if err := os.Chtimes(p.partial, modTime, modTime); err != nil {
			p.Abort()
			return err
		}
	}
	var err error
	if replace {
		err = RenameReplace(p.partial, p.final)
	} else {
		err = RenameNoReplace(p.partial, p.final)
	}
	if err != nil {
		p.Abort()
		return err
	}
	p.done = true
	return nil
}

// abort closes and removes the partial. It is safe to call more than once and
// after commit; every error path and every stop ends here.
func (p *PartialFile) Abort() {
	if p.done {
		return
	}
	p.done = true
	p.File.Close()
	os.Remove(p.partial)
}

// CopyOptions tunes CopyFile.
type CopyOptions struct {
	// Replace allows an existing destination to be overwritten - only when the
	// user asked for that exact path.
	Replace bool
	// BufferSize is the copy buffer; zero means 1 MiB.
	BufferSize int
	// Progress, when set, is told how many bytes each write added.
	Progress func(n int64)
}

// CopyFile copies src to dst through a partial file and a no-replace
// rename, honouring ctx between buffers. The source's modification time is
// carried over. It refuses a destination that is the source itself.
func CopyFile(ctx context.Context, src, dst string, opt CopyOptions) error {
	if same, err := SameFile(src, dst); err != nil {
		return err
	} else if same {
		return fmt.Errorf("%q and %q are the same file", src, dst)
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if !opt.Replace {
		if _, err := os.Lstat(dst); err == nil {
			return &os.LinkError{Op: "copy", Old: src, New: dst, Err: ErrDestinationExists}
		}
	}
	out, err := CreatePartial(dst)
	if err != nil {
		return err
	}
	defer out.Abort()

	size := opt.BufferSize
	if size <= 0 {
		size = 1 << 20
	}
	buf := make([]byte, size)
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		n, rerr := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			if opt.Progress != nil {
				opt.Progress(int64(n))
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return out.Commit(info.ModTime(), opt.Replace)
}
