package vdisk

import (
	"sort"
	"sync"
	"time"
)

// The ram profile's write path (FDD-FORMAT section 10.4). Writes land in
// memory; the dirty-cluster map says which clusters differ from the file, and
// a save writes exactly those back. The dirty map is state about the buffer,
// not about the file: it is never stored, and the format records only whether
// a save finished (save_in_progress, save_started, last_good_save).
//
// Only the clusters written since the last save are held in memory; the rest
// are read from the file, which is the saved image. Memory use is therefore
// bounded by the save policy (SavePolicy), not by the volume size - which is
// why SP-0004 S4 does not keep the whole volume in a buffer (P4 amendment 2).
//
// A ram volume's flush is not a save. SYNCHRONIZE CACHE is answered from
// memory (TargetConfig.WriteBack), and the file follows at the save policy's
// pace: what a ram container loses on a crash is what was written since the
// last completed save, which is the profile's stated contract (spec 9).

type ramState struct {
	// saveMu makes saves one at a time. It is always taken before c.mu, and
	// Flush, Close, Abandon and Discard take it too, so no header write can
	// overlap a save that is writing clusters outside c.mu.
	saveMu sync.Mutex
	dirty  map[uint64][]byte // logical cluster -> its plaintext, written since the running save's snapshot
	saving map[uint64][]byte // the snapshot a running save is writing; nil between saves
}

func newRAMState() *ramState { return &ramState{dirty: map[uint64][]byte{}} }

// ramWrite applies a write to the in-memory clusters, loading a cluster the
// first time it is dirtied - from the snapshot a running save holds when it
// is there (the file may be half-written at that moment), from the file
// otherwise. A cluster the map does not hold (a ram container is fully
// mapped, so only a foreign writer leaves one) takes the ordinary allocating
// path.
func (c *Container) ramWrite(p []byte, off int64) error {
	cs := int64(c.hdr.clusterSize())
	for len(p) > 0 {
		cl := off / cs
		in := off - cl*cs
		n := int(min(int64(len(p)), cs-in))
		if c.entries[cl] == sentinel {
			if err := c.writeLogical(p[:n], off); err != nil {
				return err
			}
		} else {
			buf := c.ram.dirty[uint64(cl)]
			if buf == nil {
				if old := c.ram.saving[uint64(cl)]; old != nil {
					buf = append([]byte(nil), old...)
				} else {
					buf = make([]byte, cs)
					if err := c.readSectors(buf, c.entries[cl], cl, 0); err != nil {
						return err
					}
				}
				c.ram.dirty[uint64(cl)] = buf
			}
			copy(buf[in:], p[:n])
		}
		p, off = p[n:], off+int64(n)
	}
	return nil
}

// ramCluster is the in-memory copy of logical cluster cl, or nil when the
// file holds its current content.
func (c *Container) ramCluster(cl uint64) []byte {
	if buf := c.ram.dirty[cl]; buf != nil {
		return buf
	}
	return c.ram.saving[cl]
}

// RAMState is what a ram container holds that its file does not yet.
type RAMState struct {
	DirtyBytes   int64     // written since the last save began, not yet in the file
	Saving       bool      // a save is writing clusters now
	LastGoodSave time.Time // the time the last completed save began; zero when never
}

// RAMState reports the dirty amount; ok is false for any other profile.
func (c *Container) RAMState() (s RAMState, ok bool) {
	if c.ram == nil {
		return s, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cs := int64(c.hdr.clusterSize())
	s.DirtyBytes = int64(len(c.ram.dirty)) * cs
	s.Saving = c.ram.saving != nil
	if s.Saving {
		s.DirtyBytes += int64(len(c.ram.saving)) * cs
	}
	s.LastGoodSave = nsTime(c.hdr.LastGoodSave)
	return s, true
}

// Save writes a ram container's dirty clusters back (section 10.4) and
// returns when the file holds the volume as it was when the save began. A
// second Save waits for a running one. Writers are not blocked while the
// clusters are written: the dirty map is swapped for an empty one under the
// lock, the snapshot is written outside it, and writes arriving meanwhile go
// to the new map and to the next save. For any other profile Save is Flush.
func (c *Container) Save() error {
	if c.ram == nil {
		return c.Flush()
	}
	r := c.ram
	r.saveMu.Lock()
	defer r.saveMu.Unlock()

	c.mu.Lock()
	if err := c.usable(false); err != nil {
		c.mu.Unlock()
		return err
	}
	if c.mode == OpenRead || !c.session {
		c.mu.Unlock()
		return nil
	}
	if c.failed != nil {
		c.mu.Unlock()
		return c.failed
	}
	if len(r.dirty) == 0 && !c.pending {
		defer c.mu.Unlock()
		if err := c.b.Sync(); err != nil {
			return c.fail(err)
		}
		return nil
	}
	started, err := c.beginSave()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	snap := r.dirty
	r.saving, r.dirty = snap, map[uint64][]byte{}
	order := sortedClusters(snap)
	phys := make([]uint64, len(order))
	for i, cl := range order {
		phys[i] = c.entries[cl]
	}
	cs := int64(c.hdr.clusterSize())
	base := int64(c.hdr.DataOffset)
	shift := c.hdr.SectorShift
	c.mu.Unlock()

	// Outside the lock: the snapshot's buffers are never written again (a new
	// write copies them into the new map first), so they are read here while
	// readers may read them too; the ciphertext goes to a buffer of its own.
	var werr error
	ct := make([]byte, cs)
	for i, cl := range order {
		c.cipher.encrypt(ct, snap[cl], uint64(int64(cl)*cs)>>shift)
		if _, err := c.b.WriteAt(ct, base+int64(phys[i])*cs); err != nil {
			werr = err
			break
		}
	}
	if werr == nil {
		werr = c.b.Sync()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	r.saving = nil
	if werr != nil {
		// Nothing of the snapshot is lost from memory: a cluster not written
		// again since goes back into the dirty map. The file is left as the
		// header says - a save in progress - and the container writes nothing
		// more.
		for cl, buf := range snap {
			if r.dirty[cl] == nil {
				r.dirty[cl] = buf
			}
		}
		return c.fail(werr)
	}
	return c.endSave(started)
}

// save is the whole save under c.mu, for the end of a session: nothing else
// runs then, so there is nothing to keep unblocked. The caller holds saveMu.
func (c *Container) save() error {
	if c.failed != nil {
		return c.failed
	}
	if len(c.ram.dirty) == 0 && !c.pending {
		if err := c.b.Sync(); err != nil {
			return c.fail(err)
		}
		return nil
	}
	started, err := c.beginSave()
	if err != nil {
		return err
	}
	for _, cl := range sortedClusters(c.ram.dirty) {
		buf := append([]byte(nil), c.ram.dirty[cl]...)
		if err := c.writeCluster(buf, c.entries[cl], int64(cl)); err != nil {
			return err
		}
	}
	if err := c.b.Sync(); err != nil {
		return c.fail(err)
	}
	c.ram.dirty = map[uint64][]byte{}
	return c.endSave(started)
}

// beginSave is step 1 of section 10.4: a header write with save_in_progress
// = 1 and save_started = now.
func (c *Container) beginSave() (uint64, error) {
	started := uint64(nowFunc().UnixNano())
	h := c.hdr
	h.SaveInProgress = 1
	h.SaveStarted = started
	h.PhysicalSize = uint64(c.fileLen)
	return started, c.writeHeaderPair(h)
}

// endSave is step 3: clear both and set last_good_save to the time step 1
// began - in the same commit as a pending map change, when there is one.
func (c *Container) endSave(started uint64) error {
	finish := func(h *header) {
		h.SaveInProgress = 0
		h.SaveStarted = 0
		h.LastGoodSave = started
	}
	if c.pending {
		return c.commit(finish)
	}
	h := c.hdr
	finish(&h)
	return c.writeHeaderPair(h)
}

func sortedClusters(m map[uint64][]byte) []uint64 {
	order := make([]uint64, 0, len(m))
	for cl := range m {
		order = append(order, cl)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	return order
}

// lockSave takes a ram container's save lock, so that a session end never
// overlaps a running save. It returns the unlock.
func (c *Container) lockSave() func() {
	if c.ram == nil {
		return func() {}
	}
	c.ram.saveMu.Lock()
	return c.ram.saveMu.Unlock
}
