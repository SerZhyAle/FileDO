package vdisk

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/blake2b"
)

// The escape route (principle 1): the logical volume out of the container,
// decrypted, with no mount, no driver, no socket and no elevation. It
// understands nothing about what the volume holds, so it cannot be wrong
// about a file system it never parses - which is why it comes first and is
// always available. This file imports nothing that reaches a transport.

// RawForm is the shape of an exported image.
type RawForm uint8

const (
	// RawFormImage is the bare image: the volume's bytes, what another tool
	// wants.
	RawFormImage RawForm = iota
	// RawFormVHD is the same bytes followed by a 512-byte fixed-VHD footer,
	// which Windows attaches with no FileDO installed - valid when the volume
	// holds a whole disk (a partition table), which the transport decides.
	RawFormVHD
)

// ExportRaw writes the logical volume to dest, a file that must not exist in
// a folder that must (FDD-BEHAVIOUR section 3 rule 4). A stop or a failure
// removes the partial file.
func (c *Container) ExportRaw(ctx context.Context, dest string, form RawForm, p ProgressSink) (err error) {
	if form != RawFormImage && form != RawFormVHD {
		return usagef("unknown raw form %d", form)
	}
	info := c.Info()
	if fi, statErr := os.Stat(filepath.Dir(dest)); statErr != nil || !fi.IsDir() {
		return usagef("the destination folder %s does not exist", filepath.Dir(dest))
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return usagef("%s already exists; an export never writes over a file", dest)
		}
		return ioErr(err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = ioErr(cerr)
		}
		if err != nil {
			os.Remove(dest)
		}
	}()
	size := info.LogicalSize
	chunk := info.ClusterSize
	buf := make([]byte, chunk)
	for off := int64(0); off < size; off += chunk {
		if stopped(ctx) {
			return ErrStopped
		}
		n := min(chunk, size-off)
		if _, err := c.ReadAt(buf[:n], off); err != nil && err != io.EOF {
			return err
		}
		if _, err := f.Write(buf[:n]); err != nil {
			return ioErr(err)
		}
		report(p, off+n, size)
	}
	if form == RawFormVHD {
		if _, err := f.Write(vhdFooter(uint64(size), nowFunc())); err != nil {
			return ioErr(err)
		}
	}
	if err := f.Sync(); err != nil {
		return ioErr(err)
	}
	return nil
}

// vhdFooter builds the 512-byte footer of a fixed VHD (Microsoft Virtual Hard
// Disk Image Format Specification, "Hard Disk Footer Format"); all fields are
// big-endian.
func vhdFooter(size uint64, now time.Time) []byte {
	b := make([]byte, 512)
	be := binary.BigEndian
	copy(b[0:8], "conectix")
	be.PutUint32(b[8:], 2)                   // features: reserved bit, always set
	be.PutUint32(b[12:], 0x00010000)         // file format version 1.0
	be.PutUint64(b[16:], 0xFFFFFFFFFFFFFFFF) // data offset: none, a fixed disk
	be.PutUint32(b[24:], vhdTimestamp(now))  // seconds since 2000-01-01 UTC
	copy(b[28:32], "fdo ")                   // creator application
	be.PutUint32(b[32:], 0x00010000)         // creator version
	be.PutUint32(b[36:], 0x5769326B)         // creator host OS: "Wi2k"
	be.PutUint64(b[40:], size)               // original size
	be.PutUint64(b[48:], size)               // current size
	be.PutUint32(b[56:], vhdGeometry(size))  // cylinders, heads, sectors per track
	be.PutUint32(b[60:], 2)                  // disk type: fixed
	id := blake2b.Sum256(b[:64])
	copy(b[68:84], id[:16]) // unique id: derived from the fields above, so the footer draws no randomness
	var sum uint32
	for _, x := range b {
		sum += uint32(x)
	}
	be.PutUint32(b[64:], ^sum)
	return b
}

func vhdTimestamp(t time.Time) uint32 {
	s := t.Unix() - time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	if s < 0 {
		return 0
	}
	return uint32(s)
}

// vhdGeometry is the CHS calculation of the VHD specification's appendix.
func vhdGeometry(size uint64) uint32 {
	total := size / 512
	if total > 65535*16*255 {
		total = 65535 * 16 * 255
	}
	var spt, heads, cth uint64
	if total >= 65535*16*63 {
		spt, heads = 255, 16
		cth = total / spt
	} else {
		spt = 17
		cth = total / spt
		heads = (cth + 1023) / 1024
		if heads < 4 {
			heads = 4
		}
		if cth >= heads*1024 || heads > 16 {
			spt, heads = 31, 16
			cth = total / spt
		}
		if cth >= heads*1024 {
			spt, heads = 63, 16
			cth = total / spt
		}
	}
	cyl := cth / heads
	return uint32(cyl)<<16 | uint32(heads)<<8 | uint32(spt)
}
