// Package rehash rewrites a file in its native format so that it gets a new
// whole-file SHA-256 and a bounded, nonzero size change while its decoded
// content and metadata stay the same. Every supported format is validated on
// input and re-verified on output with a strict reader before the new bytes
// replace the original; unknown, malformed, read-only or digitally signed
// files are skipped and left as they are. A rewritten file is again a valid
// input, so a run can be repeated any number of times, and nothing is recorded
// beside the files.
package rehash

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"filedo/fsx"
)

const (
	// MaxInMemoryBytes bounds strict in-memory decode formats (PNG, JPEG, GIF,
	// JSON, XML) so a huge input is skipped cleanly instead of exhausting RAM.
	MaxInMemoryBytes int64 = 256 << 20

	copyBufSize = 1 << 20
	nonceBytes  = 8
	tagPrefix   = "FileDO-rehash:"
)

// randReader is the randomness source for per-file nonces; tests may swap it.
var randReader io.Reader = rand.Reader

// FormatSpec documents one verified native format and its exact size delta
// bounds in bytes.
type FormatSpec struct {
	Name       string
	Extensions []string
	MinDelta   int64
	MaxDelta   int64
	Mechanism  string
}

// SkipError explains why an eligible regular file was skipped and left as it
// was.
type SkipError struct {
	Code   string
	Reason string
}

func (e *SkipError) Error() string {
	return e.Reason
}

// IsSkip reports whether err is a SkipError and returns it.
func IsSkip(err error) (*SkipError, bool) {
	var se *SkipError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// Result summarizes one verified native-format rewrite.
type Result struct {
	Format         string
	OriginalSize   int64
	OutputSize     int64
	SizeDelta      int64
	OriginalSHA256 [sha256.Size]byte
	OutputSHA256   [sha256.Size]byte
}

type formatHandler interface {
	spec() FormatSpec
	prepare(ctx context.Context, src *os.File, srcSize int64) (any, error)
	writeAndVerify(ctx context.Context, src *os.File, srcSize int64, plan any, dst *os.File, nonce string) (origHash [sha256.Size]byte, outSize int64, outHash [sha256.Size]byte, err error)
}

var allHandlers = []formatHandler{
	pngHandler{},
	jpegHandler{},
	gifHandler{},
	zipHandler{},
	wavHandler{},
	mp4Handler{},
	flacHandler{},
	jsonHandler{},
	xmlHandler{},
	heifHandler{},
	webpHandler{},
	bmpHandler{},
	tiffHandler{},
	pdfHandler{},
	rtfHandler{},
	textHandler{textPlain},
	textHandler{textMarkup},
	textHandler{textCode},
	textHandler{textINI},
	mp3Handler{},
	aiffHandler{},
	mkvHandler{},
	aviHandler{},
	tsHandler{},
	psHandler{},
}

var handlerByExt = func() map[string]formatHandler {
	m := make(map[string]formatHandler, 80)
	for _, h := range allHandlers {
		for _, ext := range h.spec().Extensions {
			if _, dup := m[ext]; dup {
				panic("rehash: extension " + ext + " registered twice")
			}
			m[ext] = h
		}
	}
	return m
}()

// SupportedFormats returns the documented specifications of all verified
// native formats in stable display order.
func SupportedFormats() []FormatSpec {
	out := make([]FormatSpec, 0, len(allHandlers))
	for _, h := range allHandlers {
		s := h.spec()
		s.Extensions = append([]string(nil), s.Extensions...)
		out = append(out, s)
	}
	return out
}

// FormatForExtension returns the FormatSpec registered for the file extension,
// if any.
func FormatForExtension(ext string) (FormatSpec, bool) {
	h, ok := handlerByExt[strings.ToLower(ext)]
	if !ok {
		return FormatSpec{}, false
	}
	s := h.spec()
	s.Extensions = append([]string(nil), s.Extensions...)
	return s, true
}

func newNonce() (string, error) {
	var b [nonceBytes]byte
	if _, err := io.ReadFull(randReader, b[:]); err != nil {
		return "", fmt.Errorf("cannot generate rehash nonce: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// RewriteFile reads path, verifies its native format, writes a strictly
// verified version with a new SHA-256 and bounded size delta into a partial
// file beside it, and replaces the original with it. Nothing else is left
// behind.
//
// Unknown, invalid, signed or read-only files return a *SkipError and stay
// untouched, as does any file the run fails on or is stopped at. A file that
// changed while it was being processed is not replaced.
func RewriteFile(ctx context.Context, path string) (Result, error) {
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
	}
	ext := strings.ToLower(filepath.Ext(path))
	h, ok := handlerByExt[ext]
	if !ok {
		if ext == "" {
			return Result{}, &SkipError{Code: "unsupported-format", Reason: "unsupported format (file has no extension)"}
		}
		return Result{}, &SkipError{Code: "unsupported-format", Reason: fmt.Sprintf("unsupported format (%s)", ext)}
	}

	linked, err := os.Lstat(path)
	if err != nil {
		return Result{}, err
	}
	if !linked.Mode().IsRegular() {
		return Result{}, &SkipError{Code: "not-regular", Reason: "not a regular file"}
	}
	if linked.Mode().Perm()&0o200 == 0 {
		return Result{}, &SkipError{Code: "read-only", Reason: "read-only file (not rewritten)"}
	}

	src, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	defer src.Close()

	fi, err := src.Stat()
	if err != nil {
		return Result{}, err
	}
	srcSize := fi.Size()

	plan, err := h.prepare(ctx, src, srcSize)
	if err != nil {
		return Result{}, err
	}

	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
	}

	nonce, err := newNonce()
	if err != nil {
		return Result{}, err
	}

	pf, err := fsx.CreatePartial(path)
	if err != nil {
		return Result{}, err
	}
	defer pf.Abort()

	origHash, outSize, outHash, err := h.writeAndVerify(ctx, src, srcSize, plan, pf.File, nonce)
	if err != nil {
		return Result{}, err
	}

	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
	}

	spec := h.spec()
	delta := outSize - srcSize
	if delta == 0 || delta < spec.MinDelta || delta > spec.MaxDelta {
		return Result{}, fmt.Errorf("%s output size delta %d outside documented bound [%d..%d]", spec.Name, delta, spec.MinDelta, spec.MaxDelta)
	}
	if outHash == origHash {
		return Result{}, fmt.Errorf("%s output SHA-256 matches original; refusing to commit", spec.Name)
	}

	// The original must be closed before it can be replaced, and must still be
	// the file that was read.
	if err := src.Close(); err != nil {
		return Result{}, err
	}
	now, err := os.Lstat(path)
	if err != nil {
		return Result{}, err
	}
	if now.Size() != srcSize || !now.ModTime().Equal(fi.ModTime()) {
		return Result{}, fmt.Errorf("%q changed while it was being rehashed; not replaced", path)
	}
	if err := pf.ReplaceOriginal(fi.ModTime()); err != nil {
		return Result{}, err
	}

	return Result{
		Format:         spec.Name,
		OriginalSize:   srcSize,
		OutputSize:     outSize,
		SizeDelta:      delta,
		OriginalSHA256: origHash,
		OutputSHA256:   outHash,
	}, nil
}

// copyChecked streams exactly n bytes from r to w while checking ctx between
// chunks. When n < 0 it copies until EOF.
func copyChecked(ctx context.Context, w io.Writer, r io.Reader, n int64) (int64, error) {
	buf := make([]byte, copyBufSize)
	var copied int64
	for {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				return copied, err
			}
		}
		toRead := len(buf)
		if n >= 0 {
			rem := n - copied
			if rem <= 0 {
				return copied, nil
			}
			if rem < int64(toRead) {
				toRead = int(rem)
			}
		}
		nr, rerr := r.Read(buf[:toRead])
		if nr > 0 {
			nw, werr := w.Write(buf[:nr])
			copied += int64(nw)
			if werr != nil {
				return copied, werr
			}
			if nw != nr {
				return copied, io.ErrShortWrite
			}
		}
		if rerr == io.EOF {
			if n >= 0 && copied != n {
				return copied, io.ErrUnexpectedEOF
			}
			return copied, nil
		}
		if rerr != nil {
			return copied, rerr
		}
	}
}

func readBoundedFile(ctx context.Context, src *os.File, srcSize int64, formatName string) ([]byte, error) {
	if srcSize > MaxInMemoryBytes {
		return nil, &SkipError{
			Code:   "too-large",
			Reason: fmt.Sprintf("%s file exceeds %d MiB strict in-memory verification limit", formatName, MaxInMemoryBytes>>20),
		}
	}
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	data := make([]byte, srcSize)
	if _, err := io.ReadFull(src, data); err != nil {
		return nil, err
	}
	return data, nil
}
