//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"filedo/fsx"

	"golang.org/x/sys/windows"
)

// Test-file I/O for the fake-capacity engine: the one writer every tester and
// every fill uses (SP-0026 CAP-01, CAP-02, CAP-10), the read-back path that
// bypasses the Windows cache (CAP-03), and the space and volume queries the
// plans are made from (CAP-11, CAP-12, CAP-19) - each of the last behind a
// package variable, so a test can stand a small or failing volume in.

// ---------------------------------------------------------------------------
// The run nonce

// capacityNonceValue is the current run's nonce. It goes into every header and
// is folded into every body block, so data an earlier run left in the same
// clusters never passes for this run's (CAP-15).
var capacityNonceValue atomic.Uint64

func randomNonce() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		binary.LittleEndian.PutUint64(b[:], uint64(time.Now().UnixNano()))
	}
	n := binary.LittleEndian.Uint64(b[:])
	if n == 0 {
		n = 1
	}
	return n
}

// newCapacityNonce starts a new run and returns its nonce.
func newCapacityNonce() uint64 {
	n := randomNonce()
	capacityNonceValue.Store(n)
	return n
}

// capacityNonce is the current run's nonce, made on first use.
func capacityNonce() uint64 {
	if n := capacityNonceValue.Load(); n != 0 {
		return n
	}
	capacityNonceValue.CompareAndSwap(0, randomNonce())
	return capacityNonceValue.Load()
}

// ---------------------------------------------------------------------------
// The writer

// testFileBufferSize is the write buffer of the capacity test's writer.
const testFileBufferSize = 64 * 1024 * 1024

// writeCapacityFile writes one test file: created with CREATE_NEW, so a name
// is never reused and an old file's tail never survives under a new header
// (CAP-10); filled range by range from the format; flushed to the device
// before it returns. created reports whether a file exists on disk because
// of this call - whole or partial - so a caller never removes a file it did
// not make. progress, when not nil, counts the bytes handed to the system.
func writeCapacityFile(ctx context.Context, path string, size int64, bufSize int, progress *atomic.Int64) (created bool, err error) {
	meta, err := newTestFileMeta(filepath.Base(path), capacityNonce(), size, time.Now().Format("20060102_150405"))
	if err != nil {
		return false, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return false, err
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = cerr
		}
	}()

	if bufSize < tfBlockSize {
		bufSize = tfBlockSize
	}
	bufSize -= bufSize % tfBlockSize
	if int64(bufSize) > size {
		bufSize = int((size + tfBlockSize - 1) / tfBlockSize * tfBlockSize)
	}
	buf := make([]byte, bufSize)

	var unsynced int64
	for off := int64(0); off < size; {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		n := int64(len(buf))
		if size-off < n {
			n = size - off
		}
		chunk := buf[:n]
		meta.fill(chunk, off)
		w, werr := f.Write(chunk)
		if progress != nil {
			progress.Add(int64(w))
		}
		if werr != nil {
			return true, werr
		}
		off += n
		// A fill's progress is watched (fillStallTimeout). Flushing as it
		// goes keeps what the cache holds - and so the silent flush at the
		// end of a file - small, so a slow stick is never mistaken for a
		// stalled one.
		if unsynced += n; progress != nil && unsynced >= fillSyncEvery && off < size {
			if err := f.Sync(); err != nil {
				return true, err
			}
			unsynced = 0
		}
	}
	if err := f.Sync(); err != nil {
		return true, err
	}
	return true, nil
}

// ---------------------------------------------------------------------------
// The read-back path

// verifyReader is an open test file read past the operating system's cache.
type verifyReader interface {
	ReadAt(p []byte, off int64) (int, error)
	Size() int64
	// Unbuffered reports whether reads really bypass the cache. A target that
	// refuses unbuffered I/O (some shares) is read buffered and says so.
	Unbuffered() bool
	Close() error
}

// openForVerify opens a test file for verification. Every verification read
// goes through it (CAP-03), which is why it is a variable: a test proves the
// route by standing a recorder in.
var openForVerify = openUnbufferedForVerify

// verifyOpenFlags are the CreateFile flags of a verification read.
const verifyOpenFlags = windows.FILE_ATTRIBUTE_NORMAL | windows.FILE_FLAG_NO_BUFFERING

type unbufferedReader struct {
	path  string
	h     windows.Handle
	f     *os.File // the buffered fallback, once in use
	size  int64
	align int
	buf   []byte
}

func openUnbufferedForVerify(path string) (verifyReader, error) {
	p, err := windows.UTF16PtrFromString(fsx.LongPath(path))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, verifyOpenFlags, 0)
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER || err == windows.ERROR_NOT_SUPPORTED {
			return openBufferedForVerify(path)
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &info); err != nil {
		windows.CloseHandle(h)
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	return &unbufferedReader{
		path:  path,
		h:     h,
		size:  int64(info.FileSizeHigh)<<32 | int64(info.FileSizeLow),
		align: capSectorAlign(path),
	}, nil
}

func openBufferedForVerify(path string) (verifyReader, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	return &unbufferedReader{path: path, h: windows.InvalidHandle, f: f, size: fi.Size()}, nil
}

func (r *unbufferedReader) Size() int64      { return r.size }
func (r *unbufferedReader) Unbuffered() bool { return r.f == nil }

func (r *unbufferedReader) Close() error {
	var err error
	if r.h != windows.InvalidHandle {
		err = windows.CloseHandle(r.h)
		r.h = windows.InvalidHandle
	}
	if r.f != nil {
		if ferr := r.f.Close(); err == nil {
			err = ferr
		}
	}
	return err
}

// ReadAt reads any range: the unbuffered handle is asked for the sector-
// aligned span that covers it, and the part asked for is copied out.
func (r *unbufferedReader) ReadAt(p []byte, off int64) (int, error) {
	if r.f != nil {
		return r.f.ReadAt(p, off)
	}
	if len(p) == 0 {
		return 0, nil
	}
	align := int64(r.align)
	start := off &^ (align - 1)
	end := (off + int64(len(p)) + align - 1) &^ (align - 1)
	need := int(end - start)
	if len(r.buf) < need {
		r.buf = capAlignedBuffer(need, r.align)
	}
	total := 0
	for total < need {
		pos := start + int64(total)
		ov := windows.Overlapped{Offset: uint32(pos), OffsetHigh: uint32(pos >> 32)}
		var n uint32
		err := windows.ReadFile(r.h, r.buf[total:need], &n, &ov)
		if err == windows.ERROR_HANDLE_EOF {
			break
		}
		if err == windows.ERROR_INVALID_PARAMETER {
			// The volume wants a larger alignment than it reported, or it
			// does not do unbuffered reads at all: read on buffered, and say
			// so through Unbuffered (SP-0024 CLI-29).
			f, oerr := os.Open(r.path)
			if oerr != nil {
				return 0, oerr
			}
			windows.CloseHandle(r.h)
			r.h = windows.InvalidHandle
			r.f = f
			return r.f.ReadAt(p, off)
		}
		if err != nil {
			return 0, &os.PathError{Op: "read", Path: r.path, Err: err}
		}
		if n == 0 {
			break
		}
		total += int(n)
		if int64(n)%align != 0 {
			break // the end of the file
		}
	}
	skip := int(off - start)
	if total <= skip {
		return 0, io.EOF
	}
	n := copy(p, r.buf[skip:total])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// capAlignedBuffer returns a size-byte slice whose first byte sits on an
// align boundary (align is a power of two). The Go heap does not move
// objects, so the alignment holds for the slice's lifetime.
func capAlignedBuffer(size, align int) []byte {
	raw := make([]byte, size+align)
	off := int(uintptr(unsafe.Pointer(&raw[0])) & uintptr(align-1))
	if off != 0 {
		off = align - off
	}
	return raw[off : off+size : off+size]
}

var (
	procCapGetDiskFreeSpaceW = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetDiskFreeSpaceW")
	capSectorCache           sync.Map // volume root -> alignment
)

// capVolumeRoot returns the root of the volume holding path (`E:\`,
// `\\server\share\`, or a mount point).
func capVolumeRoot(path string) (string, error) { return fsx.VolumePathName(path) }

// capSectorAlign is the alignment unbuffered I/O on path's volume needs: the
// volume's reported sector size, never less than 4096 (which also satisfies
// 512-byte and 512e drives) and never more than 64 KiB (SP-0024 CLI-29).
func capSectorAlign(path string) int {
	const minAlign, maxAlign = 4096, 64 * 1024
	root, err := capVolumeRoot(path)
	if err != nil {
		return minAlign
	}
	if v, ok := capSectorCache.Load(root); ok {
		return v.(int)
	}
	align := minAlign
	rp, err := windows.UTF16PtrFromString(root)
	if err == nil {
		var spc, bps, freeClusters, totalClusters uint32
		r, _, _ := procCapGetDiskFreeSpaceW.Call(uintptr(unsafe.Pointer(rp)),
			uintptr(unsafe.Pointer(&spc)), uintptr(unsafe.Pointer(&bps)),
			uintptr(unsafe.Pointer(&freeClusters)), uintptr(unsafe.Pointer(&totalClusters)))
		if r != 0 && bps > minAlign && bps <= maxAlign && bps&(bps-1) == 0 {
			align = int(bps)
		}
	}
	capSectorCache.Store(root, align)
	return align
}

// ---------------------------------------------------------------------------
// Space and volume queries

// diskSpaceQuery reports the bytes free to this user and the size of the
// volume holding path. A failed query is an error, never a guess (CAP-19).
var diskSpaceQuery = func(path string) (free, total uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, tot, totFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &totFree); err != nil {
		return 0, 0, err
	}
	return avail, tot, nil
}

// testFreeBytesEnv caps the free space the capacity verbs (test, fill, fill
// verify) believe a volume has. It can only ever make a run write less, and
// it is how the black-box tests stand a small, fully covered volume in for
// the developer's disk.
const testFreeBytesEnv = "FILEDO_TEST_FREE_BYTES"

func clampToInt64(v uint64) int64 {
	if v > 1<<62 {
		return 1 << 62
	}
	return int64(v)
}

// capacityFreeSpace is diskSpaceQuery in signed bytes, with the test cap.
func capacityFreeSpace(path string) (free, total int64, err error) {
	f, t, err := diskSpaceQuery(path)
	if err != nil {
		return 0, 0, err
	}
	free, total = clampToInt64(f), clampToInt64(t)
	if v := os.Getenv(testFreeBytesEnv); v != "" {
		if n, perr := strconv.ParseInt(v, 10, 64); perr == nil && n >= 0 && n < free {
			free = n
		}
	}
	return free, total, nil
}

// volumeFacts are the properties of the target's volume a plan depends on.
type volumeFacts struct {
	Total        int64
	FileSystem   string
	SystemVolume bool
}

// capacityVolumeFacts reads them. Best effort: an unknown file system gets no
// per-file cap, an unknown size no percentage.
var capacityVolumeFacts = func(path string) volumeFacts {
	var v volumeFacts
	if _, total, err := diskSpaceQuery(path); err == nil {
		v.Total = clampToInt64(total)
	}
	if root, err := capVolumeRoot(path); err == nil {
		if rp, err := windows.UTF16PtrFromString(root); err == nil {
			var fsName [windows.MAX_PATH + 1]uint16
			if windows.GetVolumeInformation(rp, nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))) == nil {
				v.FileSystem = windows.UTF16ToString(fsName[:])
			}
		}
	}
	v.SystemVolume = onSystemVolume(path)
	return v
}

// probeWritable proves the target takes a write before a capacity verb plans
// around it (CAP-04): a write-locked card, a folder this user cannot write
// and a read-only share all stop here, as "could not verify".
func probeWritable(dir string) error {
	name := filepath.Join(dir, fmt.Sprintf("__filedo_test_%d.tmp", time.Now().UnixNano()))
	f, err := os.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return fmt.Errorf("%s is not writable: %w", dir, err)
	}
	_, werr := f.WriteString("test")
	cerr := f.Close()
	os.Remove(name)
	if werr != nil {
		return fmt.Errorf("%s is not writable: %w", dir, werr)
	}
	if cerr != nil {
		return fmt.Errorf("%s is not writable: %w", dir, cerr)
	}
	return nil
}

// capacityContext is the stop model's context, or a background one where no
// handler exists (unit tests).
func capacityContext() context.Context {
	if globalInterruptHandler != nil {
		return globalInterruptHandler.Context()
	}
	return context.Background()
}

