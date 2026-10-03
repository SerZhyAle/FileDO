package vdisk

import (
	"context"
	"math"
)

// Grow makes the volume larger. Existing clusters are not touched: new
// clusters have new logical indices, and the XTS tweak is logical (FDD-FORMAT
// section 9). The map is committed with the new geometry in one commit
// (section 10.2), so a crash leaves either the old size or the new one.
//
// Where the larger map goes is writer policy (section 5.3): in place when both
// copies still fit before data_offset - widening map_stride if needed - and
// otherwise both copies move to fresh space at the top of the data region,
// written under the extending order before the header points at them.
func (c *Container) Grow(ctx context.Context, newSize int64) error {
	defer c.lockSave()()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usable(true); err != nil {
		return err
	}
	// SP-0148 D6: no grow of a partition container in this version, although
	// its map_stride is reserved for one (FDD-FORMAT 3.1 rule 5).
	if fixedCap(c.b) != 0 {
		return unsupportedf("a partition disk has a fixed size; compact and grow apply to file disks")
	}
	h := c.hdr
	if newSize <= int64(h.LogicalSize) {
		return usagef("grow makes a volume larger: %d is not above %d", newSize, h.LogicalSize)
	}
	if uint64(newSize)%h.sectorSize() != 0 {
		return usagef("the volume size %d is not a multiple of the %d-byte sector", newSize, h.sectorSize())
	}
	cs := h.clusterSize()
	newCC := (uint64(newSize)-1)>>h.ClusterShift + 1
	newM, ok := mapSize(newCC)
	if !ok || newM > math.MaxInt32 {
		return unsupportedf("a %d-byte volume needs a map this build cannot hold", newSize)
	}
	if err := c.beginSession(); err != nil {
		return err
	}
	if c.pending {
		if err := c.commit(nil); err != nil {
			return err
		}
	}

	offset, stride := h.MapOffset, h.MapStride
	switch {
	case newM <= stride && offset+stride+newM <= h.DataOffset:
		// In place, same stride: the inactive copy takes newM bytes and still
		// ends before the other copy or before data_offset.
	case newM > stride && offset+2*newM <= h.DataOffset:
		// In place with a wider stride. Copy B moves up; when B is the active
		// copy, a commit first makes A active, so the new B never overlaps it.
		if c.hdr.MapActive == 1 {
			if err := c.commit(nil); err != nil {
				return err
			}
		}
		stride = newM
	default:
		// Relocate both copies above every cluster and map position in use.
		top := max(c.hdr.clusterLimit(uint64(c.fileLen)), c.maxReferenced())
		for p := range c.meta {
			top = max(top, p+1)
		}
		offset = h.DataOffset + top*cs
		stride = newM
		k := (2*newM + cs - 1) / cs
		if err := c.extendTo(int64(h.DataOffset + (top+k)*cs + headerSize)); err != nil {
			return err
		}
	}
	// A backup that sits in the metadata zone (a container with no cluster yet)
	// moves past the larger copies before either is written.
	if metaEnd := offset + stride + newM; uint64(c.fileLen)-headerSize < metaEnd {
		if err := c.extendTo(int64(metaEnd + headerSize)); err != nil {
			return err
		}
	}

	next := c.hdr
	next.LogicalSize = uint64(newSize)
	next.ClusterCount = newCC
	next.MapOffset = offset
	next.MapStride = stride
	newMeta := next.metaClusters()
	oldMeta := c.meta
	// Until the commit lands the old copies are still the live ones, so the
	// allocator avoids both sets.
	both := map[uint64]bool{}
	for p := range oldMeta {
		both[p] = true
	}
	for p := range newMeta {
		both[p] = true
	}
	c.meta = both

	oldCC := int64(len(c.entries))
	oldLogical := int64(h.LogicalSize)
	rollback := func() {
		for _, e := range c.entries[oldCC:] {
			if e != sentinel {
				c.used.clear(e)
				c.allocated--
			}
		}
		c.entries = c.entries[:oldCC]
		c.meta = oldMeta
		c.allocHint = 0
	}
	for i := uint64(len(c.entries)); i < newCC; i++ {
		c.entries = append(c.entries, sentinel)
	}

	// The tail of the old last cluster becomes volume; a conforming writer
	// left it as encrypted zeros, and it is written as such here so that no
	// other writer's leftovers surface.
	if tail := oldLogical % int64(cs); tail != 0 && c.entries[oldCC-1] != sentinel {
		if err := c.writeSectors(make([]byte, int64(cs)-tail), c.entries[oldCC-1], oldCC-1, tail); err != nil {
			rollback()
			return err
		}
	}

	if h.Profile == ProfileFast || h.Profile == ProfileRAM {
		var top uint64
		for cl := oldCC; cl < int64(newCC); cl++ {
			p := c.allocate()
			c.entries[cl] = p
			top = max(top, p+1)
		}
		if err := c.extendTo(int64(h.DataOffset + top*cs + headerSize)); err != nil {
			rollback()
			return err
		}
		for cl := oldCC; cl < int64(newCC); cl++ {
			if stopped(ctx) {
				rollback()
				return ErrStopped
			}
			if err := c.writeCluster(make([]byte, cs), c.entries[cl], cl); err != nil {
				rollback()
				return err
			}
		}
	}
	c.pending = true
	if err := c.commit(func(h *header) {
		h.LogicalSize = next.LogicalSize
		h.ClusterCount = next.ClusterCount
		h.MapOffset = next.MapOffset
		h.MapStride = next.MapStride
	}); err != nil {
		return err
	}
	c.meta = newMeta
	return nil
}
