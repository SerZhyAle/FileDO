package vdisk

import (
	"context"
	"errors"
	"io"
	"sync"

	"filedo/fdsec"
)

// A carrier is what holds a container's bytes (FDD-FORMAT section 3.1): a
// file, or a partition the container spans. The container addresses it in
// carrier offsets, 0 being the primary header and L - 4096 the backup.
//
// Carrier is the exported face of the backing seam, so a caller that opened a
// partition device (SP-0148) can hand it to CreateOn, OpenOn and InspectOn.
// Create, Open and Inspect by path are unchanged and open a file carrier.
type Carrier interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	Truncate(size int64) error
	Sync() error
	Size() (int64, error)
	Close() error
}

// fixedCapacity marks a carrier whose length never changes: a partition. The
// writer consults it in the places that assume a file can grow or shrink -
// createOn, extendTo, repair, Compact, Grow (FDD-FORMAT section 3.1 rules 1-2).
type fixedCapacity interface {
	fixedCapacity() int64
}

// fixedCap reports the length of a fixed carrier, or 0 for a file.
func fixedCap(b backing) int64 {
	if f, ok := b.(fixedCapacity); ok {
		return f.fixedCapacity()
	}
	return 0
}

// PartitionMinSize is the smallest partition FileDO creates (SP-0148 7.2).
const PartitionMinSize = 64 << 20

// PartitionClusterShift is the cluster a partition container of L bytes is
// created with: the format's default, or 64 KiB below 1 GiB (SP-0148 7.2).
func PartitionClusterShift(L int64) uint8 {
	if L < 1<<30 {
		return minClusterShift
	}
	return DefaultClusterShift
}

// partitionStride is the map_stride a fixed carrier of L bytes reserves: one
// map copy for the largest cluster count L could ever hold (FDD-FORMAT
// section 3.1 rule 5).
func partitionStride(L int64, clusterShift uint8) (uint64, bool) {
	return mapSize(uint64(L) >> clusterShift)
}

// partitionDataOffset is data_offset on a fixed carrier: after both copies at
// the reserved stride.
func partitionDataOffset(stride uint64, clusterShift uint8) uint64 {
	do, _ := alignUp(uint64(writerMapOffset)+2*stride, max(uint64(dataAlign), uint64(1)<<clusterShift))
	return do
}

// PartitionLogicalMax is the largest volume, in whole clusters, a container
// spanning a partition of L bytes carries (FDD-FORMAT section 3.1 rule 5). 0
// when L holds no cluster at all.
func PartitionLogicalMax(L int64, clusterShift uint8) int64 {
	stride, ok := partitionStride(L, clusterShift)
	if !ok {
		return 0
	}
	do := partitionDataOffset(stride, clusterShift)
	if uint64(L) < do+headerSize {
		return 0
	}
	cs := uint64(1) << clusterShift
	return int64((uint64(L) - headerSize - do) / cs * cs)
}

// RegionDevice is the block device under a fixed carrier: a partition handle
// in the product, a file or a memory image in a test.
type RegionDevice interface {
	ReadAt(p []byte, off int64) (int, error)
	WriteAt(p []byte, off int64) (int, error)
	Sync() error
}

var errOutOfRegion = errors.New("vdisk: I/O outside the partition refused")

// regionCarrier is a fixed carrier: [0, capacity) of a device that takes I/O
// only in whole sectors. An access outside the window is refused before it
// reaches the device (the partition driver refuses it too, SP-0123 W4); an
// unaligned read is widened, an unaligned write becomes a read-modify-write of
// the covering sectors. The container's own I/O at sector 12 is always
// aligned; the bridge exists so nothing above it has to know the device.
type regionCarrier struct {
	mu       sync.Mutex
	dev      RegionDevice
	capacity int64
	sector   int64
	closer   io.Closer
	bridged  int // unaligned writes turned into read-modify-writes; the tests hold it at 0
}

// NewRegion makes a fixed carrier of capacity bytes over dev, whose I/O unit
// is sector bytes (512 or 4096). Close closes dev when it is an io.Closer.
func NewRegion(dev RegionDevice, capacity, sector int64) (Carrier, error) {
	if sector <= 0 || sector&(sector-1) != 0 || sector > headerSize {
		return nil, unsupportedf("a device sector of %d bytes is not supported (512 to 4096)", sector)
	}
	// The product creates nothing below PartitionMinSize; the carrier itself
	// only needs room for the smallest container (FDD-FORMAT section 12).
	if capacity < 20480 || capacity%headerSize != 0 {
		return nil, unsupportedf("a partition of %d bytes cannot carry a container (at least 20480, a multiple of 4096)", capacity)
	}
	r := &regionCarrier{dev: dev, capacity: capacity, sector: sector}
	if c, ok := dev.(io.Closer); ok {
		r.closer = c
	}
	return r, nil
}

func (r *regionCarrier) fixedCapacity() int64 { return r.capacity }

func (r *regionCarrier) span(n int, off int64) (lo, hi int64) {
	lo = off &^ (r.sector - 1)
	hi = (off + int64(n) + r.sector - 1) &^ (r.sector - 1)
	return lo, hi
}

func (r *regionCarrier) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errOutOfRegion
	}
	if off >= r.capacity {
		return 0, io.EOF
	}
	n, short := len(p), false
	if int64(n) > r.capacity-off {
		n, short = int(r.capacity-off), true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if lo, hi := r.span(n, off); lo != off || hi != off+int64(n) {
		buf := make([]byte, hi-lo)
		if _, err := r.dev.ReadAt(buf, lo); err != nil {
			return 0, err
		}
		copy(p[:n], buf[off-lo:])
	} else if _, err := r.dev.ReadAt(p[:n], off); err != nil {
		return 0, err
	}
	if short {
		return n, io.EOF
	}
	return n, nil
}

func (r *regionCarrier) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off > r.capacity || int64(len(p)) > r.capacity-off {
		return 0, errOutOfRegion
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if lo, hi := r.span(len(p), off); lo != off || hi != off+int64(len(p)) {
		r.bridged++
		buf := make([]byte, hi-lo)
		if _, err := r.dev.ReadAt(buf, lo); err != nil {
			return 0, err
		}
		copy(buf[off-lo:], p)
		if _, err := r.dev.WriteAt(buf, lo); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	return r.dev.WriteAt(p, off)
}

// Truncate is a no-op at the partition's own length and refused otherwise: a
// partition has no end of file to move.
func (r *regionCarrier) Truncate(size int64) error {
	if size == r.capacity {
		return nil
	}
	return unsupportedf("a partition disk has a fixed length of %d bytes and cannot become %d", r.capacity, size)
}

func (r *regionCarrier) Sync() error          { return r.dev.Sync() }
func (r *regionCarrier) Size() (int64, error) { return r.capacity, nil }

func (r *regionCarrier) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}
	return nil
}

// zeroHeaderPositions overwrites [0, 4096) and [L - 4096, L) with zeros and
// flushes: the first step of creating on a fixed carrier, so a stop at any
// later point leaves "not a container" (FDD-FORMAT section 3.1 rule 6).
func zeroHeaderPositions(b backing, L int64) error {
	z := make([]byte, headerSize)
	if _, err := b.WriteAt(z, 0); err != nil {
		return ioErr(err)
	}
	if _, err := b.WriteAt(z, L-headerSize); err != nil {
		return ioErr(err)
	}
	return ioErr(b.Sync())
}

// ZeroHeaderPositions is zeroHeaderPositions for a caller that holds a fixed
// carrier before any container exists on it (the elevated _part step).
func ZeroHeaderPositions(c Carrier) error {
	L := fixedCap(c)
	if L == 0 {
		return usagef("only a partition carrier has its header positions zeroed")
	}
	return zeroHeaderPositions(c, L)
}

// CreateOn writes a new container across a fixed carrier (a partition). The
// volume size 0 takes the largest the partition holds. A failed or stopped
// creation leaves both header positions zeroed - a partition that reads as
// damaged, never one that looks complete (FDD-BEHAVIOUR 6 item 10). The
// carrier belongs to the container from here on; Close closes it.
func CreateOn(ctx context.Context, b Carrier, o CreateOptions) (*Container, error) {
	L := fixedCap(b)
	if L == 0 {
		return nil, usagef("CreateOn takes a partition carrier; a file container is made by Create")
	}
	if o.SectorShift == 0 {
		o.SectorShift = DefaultSectorShift
	}
	if o.SectorShift != 12 {
		return nil, unsupportedf("a partition disk uses 4096-byte sectors (shift 12), not shift %d", o.SectorShift)
	}
	if o.ClusterShift == 0 {
		o.ClusterShift = PartitionClusterShift(L)
	}
	if o.LogicalSize == 0 {
		o.LogicalSize = PartitionLogicalMax(L, o.ClusterShift)
	}
	if o.Profile == ProfileSealed {
		return nil, unsupportedf("a sealed container is written to a new file, never to a partition")
	}
	if err := validateCreate(&o); err != nil {
		return nil, err
	}
	if err := zeroHeaderPositions(b, L); err != nil {
		return nil, err
	}
	c, err := createOn(ctx, b, o)
	if err != nil {
		// Nothing was sealed yet unless the header pair itself failed; zero
		// both positions again so no half-written header survives.
		zeroHeaderPositions(b, L)
		return nil, err
	}
	c.path = o.Path
	return c, nil
}

// OpenOn opens the container a carrier holds. label names it in Info.Path (a
// partition's locator); the carrier belongs to the container from here on,
// and a failed open leaves it to the caller.
func OpenOn(ctx context.Context, b Carrier, label string, cred fdsec.Credential, mode OpenMode) (*Container, error) {
	c, err := openOn(ctx, b, cred, mode)
	if err != nil {
		return nil, err
	}
	c.path = label
	return c, nil
}

// InspectOn reads the header only, as Inspect does for a file. It never
// writes and leaves the carrier open.
func InspectOn(b Carrier, label string) (Info, error) {
	L, err := b.Size()
	if err != nil {
		return Info{}, ioErr(err)
	}
	res, err := resolveHeaders(b, L)
	if err != nil {
		return Info{}, err
	}
	info := res.info(L)
	info.Path = label
	return info, nil
}

// ChangeCredentialOn is ChangeCredential on an open carrier.
func ChangeCredentialOn(b Carrier, old, next fdsec.Credential, nextKeyfile bool) error {
	return changeCredentialOn(b, old, next, nextKeyfile)
}

// Fixed reports whether the container sits on a fixed carrier (a partition):
// no compact, no grow, and a seal or copy only to a new file.
func (c *Container) Fixed() bool { return fixedCap(c.b) != 0 }
