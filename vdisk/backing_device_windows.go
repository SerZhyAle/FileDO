package vdisk

import (
	"encoding/binary"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The partition carrier over a Windows partition handle (SP-0148 9.1). The
// handle is opened by the caller - unelevated code never can, so it arrives
// brokered from an elevated step - with FILE_FLAG_NO_BUFFERING and without
// FILE_FLAG_WRITE_THROUGH (SP-0148 D12): durability is the flush after every
// step of the commit order, as on a file. The partition device enforces its
// own bounds (offsets are relative to the partition's first byte); the region
// carrier checks them first.

const (
	ioctlDiskGetLengthInfo     = 0x0007405C
	ioctlDiskGetDriveGeometry  = 0x00070000
	ioctlStorageQueryProperty  = 0x002D1400
	storageAccessAlignmentProp = 6
	propertyStandardQuery      = 0
	deviceAlign                = 4096 // memory alignment of every unbuffered transfer
	deviceChunk                = 1 << 20
)

// deviceIO is a RegionDevice over a partition handle. Unbuffered I/O needs
// sector-aligned memory; a caller's buffer that is not page-aligned goes
// through a page-aligned bounce buffer.
type deviceIO struct {
	h      windows.Handle
	mu     sync.Mutex
	bounce []byte
}

func alignedBuffer(n int) []byte {
	raw := make([]byte, n+deviceAlign)
	skip := (deviceAlign - int(uintptr(unsafe.Pointer(&raw[0]))%deviceAlign)) % deviceAlign
	return raw[skip : skip+n]
}

func isAligned(p []byte) bool {
	return len(p) == 0 || uintptr(unsafe.Pointer(&p[0]))%deviceAlign == 0
}

func (d *deviceIO) transfer(p []byte, off int64, write bool) (int, error) {
	done := 0
	for done < len(p) {
		n := min(len(p)-done, deviceChunk)
		part := p[done : done+n]
		buf := part
		if !isAligned(part) {
			d.mu.Lock()
			if d.bounce == nil {
				d.bounce = alignedBuffer(deviceChunk)
			}
			buf = d.bounce[:n]
			if write {
				copy(buf, part)
			}
		}
		pos := off + int64(done)
		ol := &windows.Overlapped{Offset: uint32(pos), OffsetHigh: uint32(pos >> 32)}
		var got uint32
		var err error
		if write {
			err = windows.WriteFile(d.h, buf, &got, ol)
		} else {
			err = windows.ReadFile(d.h, buf, &got, ol)
		}
		if !isAligned(part) {
			if !write && err == nil {
				copy(part, buf[:got])
			}
			d.mu.Unlock()
		}
		if err != nil {
			return done, err
		}
		if int(got) != n {
			return done + int(got), windows.ERROR_HANDLE_EOF
		}
		done += n
	}
	return done, nil
}

func (d *deviceIO) ReadAt(p []byte, off int64) (int, error)  { return d.transfer(p, off, false) }
func (d *deviceIO) WriteAt(p []byte, off int64) (int, error) { return d.transfer(p, off, true) }
func (d *deviceIO) Sync() error                              { return windows.FlushFileBuffers(d.h) }
func (d *deviceIO) Close() error                             { return windows.CloseHandle(d.h) }

// DeviceLength is the partition's length from IOCTL_DISK_GET_LENGTH_INFO.
func DeviceLength(h windows.Handle) (int64, error) {
	var out [8]byte
	var n uint32
	if err := windows.DeviceIoControl(h, ioctlDiskGetLengthInfo, nil, 0, &out[0], uint32(len(out)), &n, nil); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(out[:])), nil
}

// DeviceSectors is the device's logical and physical sector size, from the
// access-alignment descriptor, or the drive geometry where a device has none.
func DeviceSectors(h windows.Handle) (logical, physical int64, err error) {
	var q [12]byte // STORAGE_PROPERTY_QUERY: PropertyId, QueryType, AdditionalParameters[1]
	binary.LittleEndian.PutUint32(q[0:], storageAccessAlignmentProp)
	binary.LittleEndian.PutUint32(q[4:], propertyStandardQuery)
	var out [28]byte // STORAGE_ACCESS_ALIGNMENT_DESCRIPTOR
	var n uint32
	// Version, Size, BytesPerCacheLine, BytesOffsetForCacheAlignment,
	// BytesPerLogicalSector (16), BytesPerPhysicalSector (20), ...
	if e := windows.DeviceIoControl(h, ioctlStorageQueryProperty, &q[0], uint32(len(q)), &out[0], uint32(len(out)), &n, nil); e == nil && n >= 24 {
		logical = int64(binary.LittleEndian.Uint32(out[16:]))
		physical = int64(binary.LittleEndian.Uint32(out[20:]))
		if logical > 0 {
			return logical, max(physical, logical), nil
		}
	}
	var g [24]byte // DISK_GEOMETRY: Cylinders, MediaType, TracksPerCylinder, SectorsPerTrack, BytesPerSector
	if err := windows.DeviceIoControl(h, ioctlDiskGetDriveGeometry, nil, 0, &g[0], uint32(len(g)), &n, nil); err != nil {
		return 0, 0, err
	}
	logical = int64(binary.LittleEndian.Uint32(g[20:]))
	if logical <= 0 {
		logical = 512
	}
	return logical, logical, nil
}

// NewDeviceCarrier makes the partition carrier over an open partition handle.
// The carrier owns the handle from here on - Close closes it - also when this
// call fails.
func NewDeviceCarrier(h windows.Handle) (Carrier, error) {
	d := &deviceIO{h: h}
	L, err := DeviceLength(h)
	if err != nil {
		d.Close()
		return nil, ioErr(err)
	}
	logical, _, err := DeviceSectors(h)
	if err != nil {
		d.Close()
		return nil, ioErr(err)
	}
	// The backup is the partition's last 4096 bytes (FDD-FORMAT section 3.1):
	// a length that is not a whole multiple of it was not made by a writer of
	// this format, and rounding it down would move the backup.
	if L%headerSize != 0 {
		d.Close()
		return nil, unsupportedf("the partition is %d bytes, not a multiple of %d; it is not a FileDO partition carrier", L, headerSize)
	}
	c, err := NewRegion(d, L, logical)
	if err != nil {
		d.Close()
		return nil, err
	}
	return c, nil
}
