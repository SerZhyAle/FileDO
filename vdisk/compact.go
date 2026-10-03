package vdisk

import (
	"context"
	"sort"
)

// Compact releases the physical space no cluster uses: the clusters stored
// highest in the file move down into the lowest free positions, the map is
// committed, and the file is cut to its new end - the shrinking order of
// FDD-FORMAT section 10.2:
//
//  1. copy clusters into positions the active map does not reference, write
//     the new map into the inactive copy, flush;
//  2. the primary with the new map and physical_size = new_L, flush;
//  3. the backup at new_L - 4096, flush;
//  4. truncate to new_L, flush.
//
// Because the XTS tweak is logical, a move is a byte copy: no cluster is
// decrypted and no key is used. A stop before step 2 leaves the container at
// its last committed generation, and Close then marks it clean.
//
// The moves fill holes in one pass (the highest cluster into the lowest free
// position), which is the most a single safe commit can do: a target must be
// a position the live map does not reference. Logical order in the file is
// not restored; the format does not ask for it.
func (c *Container) Compact(ctx context.Context, p ProgressSink) error {
	defer c.lockSave()()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usable(true); err != nil {
		return err
	}
	// A partition has no end of file to give back (FDD-FORMAT 3.1 rule 2).
	if fixedCap(c.b) != 0 {
		return unsupportedf("a partition disk has a fixed size; compact and grow apply to file disks")
	}
	if c.ram != nil && len(c.ram.dirty) > 0 {
		if err := c.save(); err != nil {
			return err
		}
	}
	if c.pending {
		if err := c.commit(nil); err != nil {
			return err
		}
	}
	h := c.hdr
	cs := int64(h.clusterSize())

	type ref struct {
		phys uint64
		cl   int
	}
	refs := make([]ref, 0, c.allocated)
	for cl, e := range c.entries {
		if e != sentinel {
			refs = append(refs, ref{e, cl})
		}
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].phys > refs[j].phys })
	type move struct {
		src, dst uint64
		cl       int
	}
	var moves []move
	next := append([]uint64(nil), c.entries...)
	hole := uint64(0)
	for _, r := range refs {
		for c.used.has(hole) || c.meta[hole] {
			hole++
		}
		if hole >= r.phys {
			break
		}
		moves = append(moves, move{r.phys, hole, r.cl})
		next[r.cl] = hole
		hole++
	}

	var top uint64
	for _, e := range next {
		if e != sentinel {
			top = max(top, e+1)
		}
	}
	for p := range c.meta {
		top = max(top, p+1)
	}
	m, _ := mapSize(h.ClusterCount)
	metaEnd := max(h.SlotsOffset+slotRegionSize, h.MapOffset+m, h.MapOffset+h.MapStride+m)
	newLen := int64(metaEnd) + headerSize
	if top > 0 {
		newLen = max(newLen, int64(h.DataOffset)+int64(top)*cs+headerSize)
	}
	if len(moves) == 0 && newLen >= c.fileLen {
		return nil
	}
	if err := c.beginSession(); err != nil {
		return err
	}
	buf := make([]byte, cs)
	for i, mv := range moves {
		if stopped(ctx) {
			return ErrStopped
		}
		if err := readFull(c.b, buf, int64(h.DataOffset)+int64(mv.src)*cs); err != nil {
			return err
		}
		if _, err := c.b.WriteAt(buf, int64(h.DataOffset)+int64(mv.dst)*cs); err != nil {
			return c.fail(err)
		}
		report(p, int64(i+1), int64(len(moves)))
	}
	if stopped(ctx) {
		return ErrStopped
	}
	if newLen > c.fileLen {
		if err := c.extendTo(newLen); err != nil {
			return err
		}
	}
	c.entries = next
	c.used = newBitset()
	for _, e := range next {
		if e != sentinel {
			c.used.set(e)
		}
	}
	c.allocHint = 0
	oldLen := c.fileLen
	c.fileLen = newLen
	c.pending = true
	if err := c.commit(nil); err != nil {
		return err
	}
	if newLen < oldLen {
		if err := c.b.Truncate(newLen); err != nil {
			return c.fail(err)
		}
		if err := c.b.Sync(); err != nil {
			return c.fail(err)
		}
	}
	return nil
}
