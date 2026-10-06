package vdisk

import "container/list"

// The partition carrier's read cache (SP-0148 S9, D12). A raw partition handle
// is never cached by Windows, so every read the iSCSI server makes reaches the
// device; a file container gets the file system's cache for free. This is the
// bounded stand-in: whole 4096-byte blocks, least recently used out first, a
// byte cap, and write-through - a write goes to the device first and only then
// updates or drops the blocks it touched, so the cache never holds bytes the
// device does not. Durability is unchanged: nothing is held back, Sync stays a
// FlushFileBuffers.
//
// It is coherent because the server's handle is the only one open on the
// partition (share mode 0, D10). A program that writes the partition through
// \\.\PhysicalDriveN behind FileDO's back (SP-0148 section 17) is the same risk
// as one writing a mounted .fdd file; the cache makes that stale for as long
// as a block stays cached.

const cacheBlock = headerSize // 4096

// ReadCacheDefaultBytes is the cap the product gives a mounted partition disk.
const ReadCacheDefaultBytes int64 = 256 << 20

type cacheEntry struct {
	blk  int64
	data []byte
}

type readCache struct {
	maxBlocks int
	maxIO     int64 // reads and writes larger than this bypass the cache (a scan must not flush it)
	byBlk     map[int64]*list.Element
	lru       *list.List // front = most recently used
	hits      int64
	misses    int64
}

func newReadCache(maxBytes int64) *readCache {
	n := int(maxBytes / cacheBlock)
	return &readCache{
		maxBlocks: n,
		maxIO:     maxBytes / 4,
		byBlk:     make(map[int64]*list.Element, min(n, 1<<14)),
		lru:       list.New(),
	}
}

func (c *readCache) get(blk int64) []byte {
	if e, ok := c.byBlk[blk]; ok {
		c.lru.MoveToFront(e)
		return e.Value.(*cacheEntry).data
	}
	return nil
}

func (c *readCache) has(blk int64) bool {
	_, ok := c.byBlk[blk]
	return ok
}

// put stores a copy of one block, reusing the buffer of the block it evicts.
func (c *readCache) put(blk int64, data []byte) {
	if e, ok := c.byBlk[blk]; ok {
		copy(e.Value.(*cacheEntry).data, data)
		c.lru.MoveToFront(e)
		return
	}
	var ent *cacheEntry
	if c.lru.Len() >= c.maxBlocks {
		old := c.lru.Back()
		ent = old.Value.(*cacheEntry)
		delete(c.byBlk, ent.blk)
		c.lru.Remove(old)
	} else {
		ent = &cacheEntry{data: make([]byte, cacheBlock)}
	}
	ent.blk = blk
	copy(ent.data, data)
	c.byBlk[blk] = c.lru.PushFront(ent)
}

func (c *readCache) drop(blk int64) {
	if e, ok := c.byBlk[blk]; ok {
		delete(c.byBlk, blk)
		c.lru.Remove(e)
	}
}

// dropRange forgets every block that overlaps [off, off+n).
func (c *readCache) dropRange(off, n int64) {
	for b := off / cacheBlock; b*cacheBlock < off+n; b++ {
		c.drop(b)
	}
}

// afterWrite makes the cache agree with a write of p at off that the device
// accepted: a cached block takes the new bytes, a block the write covers whole
// is stored, any other block stays absent. A write too large to cache only
// drops what it overlaps.
func (c *readCache) afterWrite(p []byte, off int64) {
	if int64(len(p)) > c.maxIO {
		c.dropRange(off, int64(len(p)))
		return
	}
	end := off + int64(len(p))
	for b := off / cacheBlock; b*cacheBlock < end; b++ {
		bs, be := b*cacheBlock, (b+1)*cacheBlock
		switch {
		case bs >= off && be <= end:
			c.put(b, p[bs-off:be-off])
		case c.has(b):
			e := c.byBlk[b].Value.(*cacheEntry)
			lo, hi := max(bs, off), min(be, end)
			copy(e.data[lo-bs:hi-bs], p[lo-off:hi-off])
			c.lru.MoveToFront(c.byBlk[b])
		}
	}
}

// EnableReadCache gives a partition carrier a read cache of at most maxBytes
// (never more than the partition itself). It reports whether c is a partition
// carrier that took one; a cap below one block, or a file carrier, leaves it
// off. Call it before the carrier is shared; it is not meant to be toggled
// under load.
func EnableReadCache(c Carrier, maxBytes int64) bool {
	r, ok := c.(*regionCarrier)
	if !ok {
		return false
	}
	maxBytes = min(maxBytes, r.capacity)
	r.mu.Lock()
	defer r.mu.Unlock()
	if maxBytes < 8*cacheBlock {
		r.cache = nil
		return false
	}
	r.cache = newReadCache(maxBytes)
	return true
}

// ReadCacheStats is how many cache blocks reads found, and had to fetch, since
// the cache was enabled; (0, 0) for a carrier without one.
func ReadCacheStats(c Carrier) (hits, misses int64) {
	r, ok := c.(*regionCarrier)
	if !ok {
		return 0, 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cache == nil {
		return 0, 0
	}
	return r.cache.hits, r.cache.misses
}

// readCached serves p at off (already bounds-checked, n bytes) from the cache,
// fetching the blocks it lacks from the device in as few reads as possible.
// The caller holds r.mu.
func (r *regionCarrier) readCached(p []byte, off int64) error {
	c := r.cache
	lo := off &^ (cacheBlock - 1)
	hi := (off + int64(len(p)) + cacheBlock - 1) &^ (cacheBlock - 1)
	buf := make([]byte, hi-lo)
	for b := lo; b < hi; {
		if d := c.get(b / cacheBlock); d != nil {
			copy(buf[b-lo:], d)
			c.hits++
			b += cacheBlock
			continue
		}
		e := b + cacheBlock
		for e < hi && !c.has(e/cacheBlock) {
			e += cacheBlock
		}
		if _, err := r.dev.ReadAt(buf[b-lo:e-lo], b); err != nil {
			return err
		}
		for x := b; x < e; x += cacheBlock {
			c.put(x/cacheBlock, buf[x-lo:x-lo+cacheBlock])
			c.misses++
		}
		b = e
	}
	copy(p, buf[off-lo:])
	return nil
}
