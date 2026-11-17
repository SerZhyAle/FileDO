package backupzip

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrVerify is a zip that does not read back as what Pack just wrote: a
// central directory that cannot be read, a file count or a total size that
// differs from the packed one, or an entry whose bytes fail their checksum. An
// error of any other kind from Verify is the read itself failing and carries
// the operating system's own error, so a caller can tell a bad zip from a
// device that would not answer.
var ErrVerify = errors.New("backupzip: the zip does not read back as it was written")

// Verify reads the finished zip at path back in full: the central directory,
// the number of files and their total size against what Pack reported, and
// every file's bytes against its stored checksum (archive/zip checks the CRC
// when an entry is read to its end). It is what lets a caller treat the zip as
// a complete generation before it retires an older one. stop is polled between
// buffers; a stop ends with ErrStopped and nothing is changed - Verify reads
// and never writes.
func Verify(path string, files, bytes int64, stop func() bool) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		if errors.Is(err, zip.ErrFormat) || errors.Is(err, zip.ErrChecksum) {
			return fmt.Errorf("%w: %v", ErrVerify, err)
		}
		return err
	}
	defer zr.Close()

	var gotFiles, gotBytes int64
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		gotFiles++
		gotBytes += int64(f.UncompressedSize64)
	}
	if gotFiles != files || gotBytes != bytes {
		return fmt.Errorf("%w: the zip holds %d files and %d bytes, the run packed %d files and %d bytes", ErrVerify, gotFiles, gotBytes, files, bytes)
	}

	buf := make([]byte, 1<<20)
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrVerify, err)
		}
		for {
			if stop != nil && stop() {
				rc.Close()
				return ErrStopped
			}
			_, rerr := rc.Read(buf)
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				rc.Close()
				if errors.Is(rerr, zip.ErrChecksum) || errors.Is(rerr, io.ErrUnexpectedEOF) || isFlateCorrupt(rerr) {
					return fmt.Errorf("%w: %v", ErrVerify, rerr)
				}
				return rerr
			}
		}
		rc.Close()
	}
	return nil
}

// isFlateCorrupt reports a deflate stream that does not decode: the one error
// type compress/flate returns for it.
func isFlateCorrupt(err error) bool {
	var corrupt flate.CorruptInputError
	return errors.As(err, &corrupt)
}
