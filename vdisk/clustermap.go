package vdisk

import (
	"encoding/binary"

	"golang.org/x/crypto/blake2b"
)

// The cluster map (FDD-FORMAT section 10). A copy is cluster_count u64le
// entries in logical order, then zeros up to M; an entry is a physical cluster
// index or the sentinel. The map is stored in the clear and bound to the
// sealed header by map_digest, so a changed map byte is found, never followed.
//
// Allocation is this package's policy, not the format's (P2 section 5): a
// plain container allocates on first write, taking the lowest physical index
// that nothing references and no map copy occupies, and appends when there is
// none; fast and ram allocate every cluster at creation in logical order.

// encodeMap lays out one map copy of m bytes.
func encodeMap(entries []uint64, m uint64) []byte {
	b := make([]byte, m)
	for i, e := range entries {
		binary.LittleEndian.PutUint64(b[i*mapEntrySize:], e)
	}
	return b
}

// mapDigest is BLAKE2b-256 of a copy's M bytes (section 10.1 item 1).
func mapDigest(copyBytes []byte) [32]byte { return blake2b.Sum256(copyBytes) }

// mapCopyOffset is where copy 0 (A) or 1 (B) of a header's map lives.
func (h *header) mapCopyOffset(copyIndex uint8) uint64 {
	if copyIndex == 1 {
		return h.MapOffset + h.MapStride
	}
	return h.MapOffset
}

// clusterLimit is P = floor((L - 4096 - data_offset) / C), 0 when
// L - 4096 <= data_offset: the physical clusters that lie wholly inside the
// file, before the backup header (section 10.1 item 2).
func (h *header) clusterLimit(fileLen uint64) uint64 {
	if fileLen < headerSize || fileLen-headerSize <= h.DataOffset {
		return 0
	}
	return (fileLen - headerSize - h.DataOffset) >> h.ClusterShift
}

// metaClusters returns the physical cluster indices the slot region and the
// two map copies occupy - the positions no referenced cluster may take and the
// allocator must skip (section 10.1 item 4). Regions before data_offset
// occupy none.
func (h *header) metaClusters() map[uint64]bool {
	out := map[uint64]bool{}
	m, ok := mapSize(h.ClusterCount)
	if !ok {
		return out
	}
	for _, r := range []region{
		{"key slots", h.SlotsOffset, h.SlotsOffset + slotRegionSize},
		{"map copy A", h.MapOffset, h.MapOffset + m},
		{"map copy B", h.MapOffset + h.MapStride, h.MapOffset + h.MapStride + m},
	} {
		if r.end <= h.DataOffset {
			continue
		}
		lo := (max(r.start, h.DataOffset) - h.DataOffset) >> h.ClusterShift
		hi := (r.end - 1 - h.DataOffset) >> h.ClusterShift
		for p := lo; p <= hi; p++ {
			out[p] = true
		}
	}
	return out
}

// checkMap reads the active copy's bytes and applies section 10.1 items 1-5 in
// order, each failure being damage. It returns the entries.
func checkMap(h *header, copyBytes []byte, fileLen uint64) ([]uint64, error) {
	if d := mapDigest(copyBytes); d != h.MapDigest {
		return nil, damagedf("the active cluster map does not match map_digest")
	}
	n := int(h.ClusterCount)
	entries := make([]uint64, n)
	limit := h.clusterLimit(fileLen)
	seen := newBitset()
	meta := h.metaClusters()
	var allocated uint64
	for i := range entries {
		e := binary.LittleEndian.Uint64(copyBytes[i*mapEntrySize:])
		entries[i] = e
		if e == sentinel {
			continue
		}
		if e >= limit {
			return nil, damagedf("logical cluster %d is stored past the end of the file (physical %d, the file holds %d) - the file was cut", i, e, limit)
		}
		if seen.has(e) {
			return nil, damagedf("physical cluster %d is referenced twice", e)
		}
		seen.set(e)
		allocated++
	}
	for p := range meta {
		if seen.has(p) {
			return nil, damagedf("physical cluster %d overlaps the key slots or a map copy", p)
		}
	}
	if allocated != h.AllocatedClusters {
		return nil, damagedf("the map holds %d clusters, allocated_clusters says %d", allocated, h.AllocatedClusters)
	}
	return entries, nil
}

// bitset is a growable set of physical cluster indices.
type bitset struct{ w []uint64 }

func newBitset() *bitset { return &bitset{} }

func (b *bitset) has(i uint64) bool {
	k := i >> 6
	return k < uint64(len(b.w)) && b.w[k]&(1<<(i&63)) != 0
}

func (b *bitset) set(i uint64) {
	k := i >> 6
	for uint64(len(b.w)) <= k {
		b.w = append(b.w, 0)
	}
	b.w[k] |= 1 << (i & 63)
}

func (b *bitset) clear(i uint64) {
	if k := i >> 6; k < uint64(len(b.w)) {
		b.w[k] &^= 1 << (i & 63)
	}
}
