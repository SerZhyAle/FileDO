//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// sectorAlignSize is the alignment used for unbuffered I/O buffers.
	// Windows requires buffer address, transfer size and file offset to be
	// multiples of the physical sector size. 4096 satisfies both 512-byte
	// (legacy) and 4096-byte (Advanced Format) drives.
	sectorAlignSize = 4096

	// unbufferedChunkSize is the copy buffer size used for unbuffered I/O.
	// Must be a multiple of sectorAlignSize.
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

// roundUpSector rounds n up to the nearest multiple of sectorAlignSize.
func roundUpSector(n int64) int64 {
	return (n + int64(sectorAlignSize) - 1) &^ (int64(sectorAlignSize) - 1)
}

// copySpeedTestUpload copies src → dst measuring the *write* speed of the destination.
//
// Strategy:
//   - Source is opened with standard buffered I/O (the local test file was just
//     created and will be served from the OS page cache; that is fine — we want
//     reads to be fast so they don't become the bottleneck).
//   - Destination is opened with FILE_FLAG_NO_BUFFERING | FILE_FLAG_WRITE_THROUGH
//     so every write goes directly past the OS write-behind cache to the actual
//     storage device or network path, giving a true write-speed measurement.
//   - Falls back to WRITE_THROUGH-only (no NO_BUFFERING) if the destination does
//     not support unbuffered I/O (e.g. some network shares, FAT32 volumes).
//   - If that also fails, falls back to the regular copyFileOptimized path.
func copySpeedTestUpload(src, dst string) (int64, error) {
	// Allocate sector-aligned buffer once.
	ptr, buf, err := allocSectorAligned(unbufferedChunkSize)
	if err != nil {
		// Cannot allocate aligned memory — fall back.
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
		// Some network shares reject NO_BUFFERING — try WRITE_THROUGH only.
		dstFlags = fileFlagWriteThrough
		dstHandle, err = createFileWindows(dst,
			syscall.GENERIC_WRITE, 0,
			syscall.CREATE_ALWAYS, dstFlags)
		if err != nil {
			// Give up and use regular path.
			return copyFileOptimized(src, dst)
		}
	}

	noBuffer := (dstFlags & fileFlagNoBuffering) != 0

	total, copyErr := doCopy(srcHandle, dstHandle, buf, fileSize, false, noBuffer)

	syscall.CloseHandle(dstHandle)

	if copyErr != nil {
		os.Remove(dst)
		return 0, copyErr
	}

	// When NO_BUFFERING is active the last write was rounded up to a full sector;
	// truncate the file back to the actual size.
	if noBuffer {
		f, ferr := os.OpenFile(dst, os.O_WRONLY, 0644)
		if ferr == nil {
			_ = f.Truncate(fileSize)
			f.Close()
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
//     unbuffered I/O (network paths on some configurations).
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
	defer syscall.CloseHandle(srcHandle)

	// Destination: regular buffered write.
	dstHandle, err := createFileWindows(dst,
		syscall.GENERIC_WRITE, 0,
		syscall.CREATE_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL)
	if err != nil {
		return 0, fmt.Errorf("open destination '%s': %w", dst, err)
	}
	defer syscall.CloseHandle(dstHandle)

	total, copyErr := doCopy(srcHandle, dstHandle, buf, fileSize, noBuffer, false)
	if copyErr != nil {
		os.Remove(dst)
		return 0, copyErr
	}
	return total, nil
}

// doCopy is the inner read/write loop shared by upload and download helpers.
//
//   - srcHnd   – source file handle
//   - dstHnd   – destination file handle
//   - buf      – sector-aligned buffer (must be at least unbufferedChunkSize bytes)
//   - fileSize – exact byte count to copy from source
//   - srcNoBuffer – source was opened with FILE_FLAG_NO_BUFFERING:
//     read size must be a multiple of sectorAlignSize; last read may return more
//     bytes than remaining, so we cap the write to fileSize.
//   - dstNoBuffer – destination was opened with FILE_FLAG_NO_BUFFERING:
//     write size must be a multiple of sectorAlignSize; the last chunk is
//     zero-padded up to the sector boundary; caller must truncate afterwards.
func doCopy(srcHnd, dstHnd syscall.Handle, buf []byte, fileSize int64, srcNoBuffer, dstNoBuffer bool) (int64, error) {
	var total int64
	remaining := fileSize

	for remaining > 0 {
		want := int64(len(buf))
		if remaining < want {
			want = remaining
		}

		// For NO_BUFFERING source, round read size up to next sector.
		readSize := want
		if srcNoBuffer {
			readSize = roundUpSector(want)
			if readSize > int64(len(buf)) {
				readSize = int64(len(buf))
			}
		}

		var nr uint32
		if err := syscall.ReadFile(srcHnd, buf[:readSize], &nr, nil); err != nil {
			return total, fmt.Errorf("ReadFile: %w", err)
		}
		if nr == 0 {
			break
		}

		// Actual payload bytes (cap to remaining to discard sector-padding from read).
		payload := int64(nr)
		if payload > remaining {
			payload = remaining
		}

		// For NO_BUFFERING destination, round write size up to sector boundary
		// and zero-fill the padding so we don't write garbage.
		writeSize := payload
		if dstNoBuffer {
			writeSize = roundUpSector(payload)
			if writeSize > int64(len(buf)) {
				writeSize = int64(len(buf))
			}
			// Zero out the padding region.
			for i := payload; i < writeSize; i++ {
				buf[i] = 0
			}
		}

		var nw uint32
		if err := syscall.WriteFile(dstHnd, buf[:writeSize], &nw, nil); err != nil {
			return total, fmt.Errorf("WriteFile: %w", err)
		}

		total += payload
		remaining -= payload
	}

	return total, nil
}
