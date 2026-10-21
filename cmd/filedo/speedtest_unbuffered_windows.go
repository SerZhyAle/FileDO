//go:build windows

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// sectorAlignSize is the least alignment used for unbuffered I/O: buffer
	// address, transfer size and file offset must be multiples of the
	// volume's sector size. 4096 satisfies 512-byte, 512e and 4Kn drives; a
	// volume that reports a larger sector gets that (capSectorAlign).
	sectorAlignSize = 4096

	// unbufferedChunkSize is the copy buffer size used for unbuffered I/O.
	// Must be a multiple of every alignment used (at most 64 KiB).
	unbufferedChunkSize = 8 * 1024 * 1024 // 8 MB

	// Windows CreateFile flags
	fileFlagNoBuffering  = 0x20000000
	fileFlagWriteThrough = 0x80000000
)

// allocSectorAligned allocates a sectorAlignSize-aligned buffer via VirtualAlloc.
// VirtualAlloc always returns memory aligned to the system allocation granularity
// (64 KB on x86/x64), which satisfies any sector alignment requirement.
// The caller must free the returned pointer with freeSectorAligned.
func allocSectorAligned(size int) (uintptr, []byte, error) {
	ptr, err := windows.VirtualAlloc(0, uintptr(size),
		windows.MEM_COMMIT|windows.MEM_RESERVE, windows.PAGE_READWRITE)
	if err != nil {
		return 0, nil, fmt.Errorf("VirtualAlloc(%d): %w", size, err)
	}
	buf := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), size)
	return ptr, buf, nil
}

func freeSectorAligned(ptr uintptr) {
	_ = windows.VirtualFree(ptr, 0, windows.MEM_RELEASE)
}

// createFileWindows wraps syscall.CreateFile for convenience.
func createFileWindows(path string, access, share, createDisp, flagsAndAttrs uint32) (syscall.Handle, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	h, err := syscall.CreateFile(p, access, share, nil, createDisp, flagsAndAttrs, 0)
	if err != nil {
		return syscall.InvalidHandle, err
	}
	return h, nil
}

// roundUpTo rounds n up to the nearest multiple of align (a power of two).
func roundUpTo(n int64, align int) int64 {
	a := int64(align)
	return (n + a - 1) &^ (a - 1)
}

// roundUpSector rounds n up to the nearest multiple of sectorAlignSize.
func roundUpSector(n int64) int64 {
	return roundUpTo(n, sectorAlignSize)
}

// handleIO reads and writes a synchronous handle at its file pointer.
type handleIO struct{ h syscall.Handle }

func (x handleIO) Read(p []byte) (int, error) {
	var n uint32
	err := syscall.ReadFile(x.h, p, &n, nil)
	if err == syscall.ERROR_HANDLE_EOF || (err == nil && n == 0 && len(p) > 0) {
		return int(n), io.EOF
	}
	return int(n), err
}

func (x handleIO) Write(p []byte) (int, error) {
	var n uint32
	err := syscall.WriteFile(x.h, p, &n, nil)
	return int(n), err
}

// copyEnds is the two sides of a measured copy. An unbuffered side that the
// target refuses mid-copy (ERROR_INVALID_PARAMETER - a share that accepts the
// flag at open and not in practice, or a short read that leaves the next
// offset off a sector boundary) is reopened buffered at the offset reached,
// and the copy goes on (SP-0024 CLI-29).
type copyEnds struct {
	src         io.Reader
	dst         io.Writer
	srcNoBuffer bool
	dstNoBuffer bool
	align       int
	// reopenSrcBuffered and reopenDstBuffered return a buffered handle
	// positioned at offset. Nil means the side cannot fall back.
	reopenSrcBuffered func(offset int64) (io.Reader, error)
	reopenDstBuffered func(offset int64) (io.Writer, error)
}

func isInvalidParameter(err error) bool {
	return errors.Is(err, windows.ERROR_INVALID_PARAMETER)
}

// doCopy copies exactly fileSize bytes from e.src to e.dst through buf (at
// least unbufferedChunkSize bytes, aligned).
//
//   - An unbuffered source is read in whole sectors; the padding past the end
//     of the file is discarded.
//   - An unbuffered destination is written in whole sectors; the last chunk is
//     zero-padded and the caller truncates the file afterwards.
//   - A write that reports fewer bytes than it was given is an error
//     (io.ErrShortWrite), never counted as complete.
func doCopy(e *copyEnds, buf []byte, fileSize int64) (int64, error) {
	align := e.align
	if align < sectorAlignSize {
		align = sectorAlignSize
	}
	var total int64
	for total < fileSize {
		if speedStopRequested() {
			return total, errRunStopped
		}
		want := int64(len(buf))
		if fileSize-total < want {
			want = fileSize - total
		}
		readSize := want
		if e.srcNoBuffer {
			readSize = roundUpTo(want, align)
			if readSize > int64(len(buf)) {
				readSize = int64(len(buf))
			}
		}

		nr, err := e.src.Read(buf[:readSize])
		if err != nil && isInvalidParameter(err) && e.srcNoBuffer && e.reopenSrcBuffered != nil {
			r, rerr := e.reopenSrcBuffered(total)
			if rerr != nil {
				return total, fmt.Errorf("reopening the source buffered: %w", rerr)
			}
			e.src, e.srcNoBuffer = r, false
			continue
		}
		if err != nil && err != io.EOF {
			return total, fmt.Errorf("ReadFile: %w", err)
		}
		if nr == 0 {
			return total, fmt.Errorf("the source ended after %d of %d bytes: %w", total, fileSize, io.ErrUnexpectedEOF)
		}

		payload := int64(nr)
		if payload > fileSize-total {
			payload = fileSize - total
		}
		last := total+payload >= fileSize
		// A short unbuffered read that is not a whole number of sectors - SMB
		// does this - leaves the next read off a sector boundary.
		srcMisaligned := e.srcNoBuffer && !last && int64(nr)%int64(align) != 0

		writeSize := payload
		if e.dstNoBuffer {
			if !last && payload%int64(align) != 0 {
				// Padding in the middle of the file would corrupt it: the
				// rest of the copy goes through a buffered handle.
				if e.reopenDstBuffered == nil {
					return total, fmt.Errorf("unaligned chunk of %d bytes for an unbuffered destination", payload)
				}
				w, werr := e.reopenDstBuffered(total)
				if werr != nil {
					return total, fmt.Errorf("reopening the destination buffered: %w", werr)
				}
				e.dst, e.dstNoBuffer = w, false
			} else {
				writeSize = roundUpTo(payload, align)
				if writeSize > int64(len(buf)) {
					writeSize = int64(len(buf))
				}
				for i := payload; i < writeSize; i++ {
					buf[i] = 0
				}
			}
		}

		nw, err := e.dst.Write(buf[:writeSize])
		if err != nil && isInvalidParameter(err) && e.dstNoBuffer && e.reopenDstBuffered != nil && nw == 0 {
			w, werr := e.reopenDstBuffered(total)
			if werr != nil {
				return total, fmt.Errorf("reopening the destination buffered: %w", werr)
			}
			e.dst, e.dstNoBuffer = w, false
			writeSize = payload
			nw, err = e.dst.Write(buf[:writeSize])
		}
		if err != nil {
			return total, fmt.Errorf("WriteFile: %w", err)
		}
		if int64(nw) != writeSize {
			return total, fmt.Errorf("WriteFile wrote %d of %d bytes: %w", nw, writeSize, io.ErrShortWrite)
		}
		total += payload

		if srcMisaligned {
			if e.reopenSrcBuffered == nil {
				return total, fmt.Errorf("short unbuffered read of %d bytes: %w", nr, io.ErrUnexpectedEOF)
			}
			r, rerr := e.reopenSrcBuffered(total)
			if rerr != nil {
				return total, fmt.Errorf("reopening the source buffered: %w", rerr)
			}
			e.src, e.srcNoBuffer = r, false
		}
	}
	return total, nil
}

// reopenAt opens path buffered for reading or writing, positioned at offset,
// and registers it for closing.
func reopenAt(path string, write bool, offset int64, closers *[]io.Closer) (*os.File, error) {
	var f *os.File
	var err error
	if write {
		f, err = os.OpenFile(path, os.O_WRONLY, 0)
	} else {
		f, err = os.Open(path)
	}
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	*closers = append(*closers, f)
	return f, nil
}

func closeAll(closers []io.Closer) {
	for _, c := range closers {
		c.Close()
	}
}

// copySpeedTestUpload copies src → dst measuring the *write* speed of the destination.
//
// Strategy:
//   - Source is opened with standard buffered I/O (the local test file was just
//     created and will be served from the OS page cache; that is fine - we want
//     reads to be fast so they don't become the bottleneck).
//   - Destination is opened with FILE_FLAG_NO_BUFFERING | FILE_FLAG_WRITE_THROUGH
//     so every write goes directly past the OS write-behind cache to the actual
//     storage device or network path, giving a true write-speed measurement.
//   - Falls back to WRITE_THROUGH-only (no NO_BUFFERING) if the destination does
//     not support unbuffered I/O (e.g. some network shares, FAT32 volumes), at
//     open or later in the copy.
//   - If that also fails, falls back to the regular copyFileOptimized path.
func copySpeedTestUpload(src, dst string) (int64, error) {
	// Allocate sector-aligned buffer once.
	ptr, buf, err := allocSectorAligned(unbufferedChunkSize)
	if err != nil {
		// Cannot allocate aligned memory - fall back.
		return copyFileOptimized(src, dst)
	}
	defer freeSectorAligned(ptr)

	srcInfo, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	fileSize := srcInfo.Size()

	// Open source with regular buffered I/O.
	srcHandle, err := createFileWindows(src,
		syscall.GENERIC_READ, syscall.FILE_SHARE_READ,
		syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return 0, fmt.Errorf("open source '%s': %w", src, err)
	}
	defer syscall.CloseHandle(srcHandle)

	// Try NO_BUFFERING | WRITE_THROUGH for destination first.
	dstFlags := uint32(fileFlagNoBuffering | fileFlagWriteThrough)
	dstHandle, err := createFileWindows(dst,
		syscall.GENERIC_WRITE, 0,
		syscall.CREATE_ALWAYS, dstFlags)
	if err != nil {
		// Some network shares reject NO_BUFFERING - try WRITE_THROUGH only.
		dstFlags = fileFlagWriteThrough
		dstHandle, err = createFileWindows(dst,
			syscall.GENERIC_WRITE, 0,
			syscall.CREATE_ALWAYS, dstFlags)
		if err != nil {
			// Give up and use regular path.
			return copyFileOptimized(src, dst)
		}
	}

	var closers []io.Closer
	e := &copyEnds{
		src:         handleIO{srcHandle},
		dst:         handleIO{dstHandle},
		dstNoBuffer: (dstFlags & fileFlagNoBuffering) != 0,
		align:       capSectorAlign(dst),
		reopenDstBuffered: func(offset int64) (io.Writer, error) {
			syscall.CloseHandle(dstHandle)
			dstHandle = syscall.InvalidHandle
			return reopenAt(dst, true, offset, &closers)
		},
	}
	total, copyErr := doCopy(e, buf, fileSize)

	if dstHandle != syscall.InvalidHandle {
		syscall.CloseHandle(dstHandle)
	}
	closeAll(closers)

	if copyErr != nil {
		os.Remove(dst)
		return 0, copyErr
	}

	// An unbuffered last write was rounded up to a whole sector; truncate the
	// file back to the actual size.
	if fi, err := os.Stat(dst); err == nil && fi.Size() != fileSize {
		f, ferr := os.OpenFile(dst, os.O_WRONLY, 0o644)
		if ferr != nil {
			return total, fmt.Errorf("truncating the padded copy: %w", ferr)
		}
		terr := f.Truncate(fileSize)
		f.Close()
		if terr != nil {
			return total, fmt.Errorf("truncating the padded copy: %w", terr)
		}
	}

	return total, nil
}

// copySpeedTestDownload copies src → dst measuring the *read* speed of the source.
//
// Strategy:
//   - Source is opened with FILE_FLAG_NO_BUFFERING so reads bypass the OS page
//     cache entirely, forcing the OS to fetch data from the actual device on
//     every read.  This gives a true read-speed measurement even when the file
//     was recently written and might otherwise be served from cache.
//   - Falls back to regular buffered I/O if the source does not support
//     unbuffered I/O (network paths on some configurations), at open or later
//     in the copy.
//   - Destination is opened with standard buffered I/O (we are not measuring the
//     local write speed here).
func copySpeedTestDownload(src, dst string) (int64, error) {
	ptr, buf, err := allocSectorAligned(unbufferedChunkSize)
	if err != nil {
		return copyFileOptimized(src, dst)
	}
	defer freeSectorAligned(ptr)

	srcInfo, err := os.Stat(src)
	if err != nil {
		return 0, err
	}
	fileSize := srcInfo.Size()

	// Try NO_BUFFERING on source.
	srcFlags := uint32(fileFlagNoBuffering)
	srcHandle, err := createFileWindows(src,
		syscall.GENERIC_READ, syscall.FILE_SHARE_READ,
		syscall.OPEN_EXISTING, srcFlags)
	noBuffer := true
	if err != nil {
		// Fall back to buffered read (e.g. network share that rejects NO_BUFFERING).
		noBuffer = false
		srcHandle, err = createFileWindows(src,
			syscall.GENERIC_READ, syscall.FILE_SHARE_READ,
			syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL)
		if err != nil {
			return 0, fmt.Errorf("open source '%s': %w", src, err)
		}
	}

	// Destination: regular buffered write.
	dstHandle, err := createFileWindows(dst,
		syscall.GENERIC_WRITE, 0,
		syscall.CREATE_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		syscall.CloseHandle(srcHandle)
		return 0, fmt.Errorf("open destination '%s': %w", dst, err)
	}

	var closers []io.Closer
	e := &copyEnds{
		src:         handleIO{srcHandle},
		dst:         handleIO{dstHandle},
		srcNoBuffer: noBuffer,
		align:       capSectorAlign(src),
		reopenSrcBuffered: func(offset int64) (io.Reader, error) {
			syscall.CloseHandle(srcHandle)
			srcHandle = syscall.InvalidHandle
			return reopenAt(src, false, offset, &closers)
		},
	}
	total, copyErr := doCopy(e, buf, fileSize)
	if srcHandle != syscall.InvalidHandle {
		syscall.CloseHandle(srcHandle)
	}
	syscall.CloseHandle(dstHandle) // before a removal, which an open handle would refuse
	closeAll(closers)
	if copyErr != nil {
		os.Remove(dst)
		return 0, copyErr
	}
	return total, nil
}
