package vdisk

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"filedo/fdsec"
)

// Every constant of FDD-FORMAT (wire version 1.0). Offsets and sizes are the
// document's; the section each block comes from is named beside it.

// File map (FDD-FORMAT sections 3 and 4.2).
const (
	headerSize      = 4096 // one header block: salt || nonce || pad || sealed
	headerSaltSize  = 32
	headerNonceSize = 24
	headerPadSize   = 8
	sealedOffset    = headerSaltSize + headerNonceSize + headerPadSize // 64
	plaintextSize   = 4016
	sealedSize      = plaintextSize + 16 // 4032
	minFileSize     = 20480              // shorter than any container (section 12)
)

// Version (section 13.1).
const (
	versionMajor = 1
	versionMinor = 0
)

// Key slots (section 7).
const (
	slotCount      = 8
	slotSize       = 512
	slotRegionSize = slotCount * slotSize // 4096
)

// Cluster map (section 10).
const (
	mapEntrySize = 8
	mapCopies    = 2
	mapAlign     = 4096
	sentinel     = ^uint64(0)
)

// Sizing (section 11). The allowed ranges are what a 1.0 reader accepts.
const (
	minClusterShift = 16
	maxClusterShift = 22
	dataAlign       = 1 << 20
)

// Writer defaults, fixed by FDD-FORMAT section 11 (decided by SP-0004 gate G0,
// passed 2026-09-27, closed in the catalog 2026-09-29). A reader accepts every
// value in the allowed ranges whatever a writer chose.
const (
	DefaultSectorShift  = 12 // 4096-byte crypto sector
	DefaultClusterShift = 20 // 1 MiB cluster
)

// The 1.0 writer layout (section 5.3): slots at 4096, map copy A at 8192.
const (
	writerSlotsOffset = 4096
	writerMapOffset   = 8192
)

// Header flag bits (section 5.1).
const (
	flagCompressed    = 1 << 0
	flagDigest        = 1 << 1
	flagMultiFile     = 1 << 2
	flagPepperWrapped = 1 << 3
	flagsReserved     = 0xF0
)

// KDF identifiers (section 8.2). kdfParamsTable holds the parameter sets this
// build knows, by kdf_params_id; a row is retired, never edited, once a
// container written under it exists.
const (
	kdfNone     = 0
	kdfArgon2id = 1
)

var kdfParamsTable = map[uint32]fdsec.KDFParams{
	// FDD-FORMAT section 8.2, fixed 2026-09-27: the profile suite 2 pinned as
	// its try-list entry 0. A row is retired, never edited; a harder profile is
	// a new row.
	1: {MemoryKiB: 262144, Time: 3, Lanes: 4},
}

// writerKDFParamsID is the row a new encrypted container is written under.
const writerKDFParamsID = 1

// kdfParams is the table every shipped path derives through: a test seam of
// the same kind as randSource, lowered by tests so a derivation costs
// milliseconds, and assigned by nothing else. A container written under a
// lowered table opens only under that same table.
var kdfParams = kdfParamsTable

// Profile is the header's profile byte (section 1). The values are the wire
// values.
type Profile uint8

const (
	ProfilePlain Profile = iota
	ProfileFast
	ProfileRAM
	ProfileSealed
	ProfileVault
)

func (p Profile) String() string {
	switch p {
	case ProfilePlain:
		return "plain"
	case ProfileFast:
		return "fast"
	case ProfileRAM:
		return "ram"
	case ProfileSealed:
		return "sealed"
	case ProfileVault:
		return "vault"
	}
	return fmt.Sprintf("profile(%d)", uint8(p))
}

var headerMagic = [4]byte{'F', 'D', 'D', 'C'}

// header is the 4016-byte plaintext of section 5, field for field. Reserved
// bytes have no field: they are written as zero and never read.
type header struct {
	VersionMajor        uint16
	VersionMinor        uint16
	Profile             Profile
	Flags               uint8
	SectorShift         uint16
	ClusterShift        uint32
	LogicalSize         uint64
	PhysicalSize        uint64
	ClusterCount        uint64
	AllocatedClusters   uint64
	MapOffset           uint64
	MapEntrySize        uint32
	MapCopies           uint32
	MapStride           uint64
	MapGeneration       uint64
	MapActive           uint8
	SaveInProgress      uint8
	DataOffset          uint64
	SlotsOffset         uint64
	SlotCount           uint16
	SlotSize            uint16
	DigestOffset        uint64
	DigestLength        uint64
	Clean               uint8
	LastGoodSave        uint64
	Created             uint64
	MountCount          uint64
	ContainerID         [16]byte
	FriendlyName        [64]byte
	WriterStamp         uint64
	KDFID               uint32
	KDFParamsID         uint32
	KDFSalt             [16]byte
	ManifestRoot        [32]byte
	PartID              [16]byte
	PartIndex           uint32
	PartCount           uint32
	CompressTableOffset uint64
	CompressTableLength uint64
	SaveStarted         uint64
	MapDigest           [32]byte
}

// encode lays the header out as its 4016-byte plaintext (section 5).
func (h *header) encode() []byte {
	b := make([]byte, plaintextSize)
	le := binary.LittleEndian
	copy(b[0:4], headerMagic[:])
	le.PutUint16(b[4:], h.VersionMajor)
	le.PutUint16(b[6:], h.VersionMinor)
	b[8] = uint8(h.Profile)
	b[9] = h.Flags
	le.PutUint16(b[10:], h.SectorShift)
	le.PutUint32(b[12:], h.ClusterShift)
	le.PutUint64(b[16:], h.LogicalSize)
	le.PutUint64(b[24:], h.PhysicalSize)
	le.PutUint64(b[32:], h.ClusterCount)
	le.PutUint64(b[40:], h.AllocatedClusters)
	le.PutUint64(b[48:], h.MapOffset)
	le.PutUint32(b[56:], h.MapEntrySize)
	le.PutUint32(b[60:], h.MapCopies)
	le.PutUint64(b[64:], h.MapStride)
	le.PutUint64(b[72:], h.MapGeneration)
	b[80] = h.MapActive
	b[81] = h.SaveInProgress
	le.PutUint64(b[88:], h.DataOffset)
	le.PutUint64(b[96:], h.SlotsOffset)
	le.PutUint16(b[104:], h.SlotCount)
	le.PutUint16(b[106:], h.SlotSize)
	le.PutUint64(b[112:], h.DigestOffset)
	le.PutUint64(b[120:], h.DigestLength)
	b[128] = h.Clean
	le.PutUint64(b[136:], h.LastGoodSave)
	le.PutUint64(b[144:], h.Created)
	le.PutUint64(b[152:], h.MountCount)
	copy(b[160:176], h.ContainerID[:])
	copy(b[176:240], h.FriendlyName[:])
	le.PutUint64(b[240:], h.WriterStamp)
	le.PutUint32(b[248:], h.KDFID)
	le.PutUint32(b[252:], h.KDFParamsID)
	copy(b[256:272], h.KDFSalt[:])
	copy(b[272:304], h.ManifestRoot[:])
	copy(b[304:320], h.PartID[:])
	le.PutUint32(b[320:], h.PartIndex)
	le.PutUint32(b[324:], h.PartCount)
	le.PutUint64(b[328:], h.CompressTableOffset)
	le.PutUint64(b[336:], h.CompressTableLength)
	le.PutUint64(b[344:], h.SaveStarted)
	copy(b[352:384], h.MapDigest[:])
	return b
}

// decodeHeader reads a plaintext and returns whether its magic is FDDC.
// Nothing else is judged here: checkHeader applies section 5.2 in its order.
func decodeHeader(b []byte) (header, bool) {
	var h header
	if len(b) != plaintextSize {
		return h, false
	}
	le := binary.LittleEndian
	h.VersionMajor = le.Uint16(b[4:])
	h.VersionMinor = le.Uint16(b[6:])
	h.Profile = Profile(b[8])
	h.Flags = b[9]
	h.SectorShift = le.Uint16(b[10:])
	h.ClusterShift = le.Uint32(b[12:])
	h.LogicalSize = le.Uint64(b[16:])
	h.PhysicalSize = le.Uint64(b[24:])
	h.ClusterCount = le.Uint64(b[32:])
	h.AllocatedClusters = le.Uint64(b[40:])
	h.MapOffset = le.Uint64(b[48:])
	h.MapEntrySize = le.Uint32(b[56:])
	h.MapCopies = le.Uint32(b[60:])
	h.MapStride = le.Uint64(b[64:])
	h.MapGeneration = le.Uint64(b[72:])
	h.MapActive = b[80]
	h.SaveInProgress = b[81]
	h.DataOffset = le.Uint64(b[88:])
	h.SlotsOffset = le.Uint64(b[96:])
	h.SlotCount = le.Uint16(b[104:])
	h.SlotSize = le.Uint16(b[106:])
	h.DigestOffset = le.Uint64(b[112:])
	h.DigestLength = le.Uint64(b[120:])
	h.Clean = b[128]
	h.LastGoodSave = le.Uint64(b[136:])
	h.Created = le.Uint64(b[144:])
	h.MountCount = le.Uint64(b[152:])
	copy(h.ContainerID[:], b[160:176])
	copy(h.FriendlyName[:], b[176:240])
	h.WriterStamp = le.Uint64(b[240:])
	h.KDFID = le.Uint32(b[248:])
	h.KDFParamsID = le.Uint32(b[252:])
	copy(h.KDFSalt[:], b[256:272])
	copy(h.ManifestRoot[:], b[272:304])
	copy(h.PartID[:], b[304:320])
	h.PartIndex = le.Uint32(b[320:])
	h.PartCount = le.Uint32(b[324:])
	h.CompressTableOffset = le.Uint64(b[328:])
	h.CompressTableLength = le.Uint64(b[336:])
	h.SaveStarted = le.Uint64(b[344:])
	copy(h.MapDigest[:], b[352:384])
	return h, [4]byte(b[0:4]) == headerMagic
}

func (h *header) sectorSize() uint64  { return 1 << h.SectorShift }
func (h *header) clusterSize() uint64 { return 1 << h.ClusterShift }

// obfuscated reports flags bit 3: the data key is pepper-wrapped.
func (h *header) obfuscated() bool { return h.Flags&flagPepperWrapped != 0 }

// mapSize is M = align_up(cluster_count * 8, 4096), or false when it does not
// fit in 64 bits.
func mapSize(clusterCount uint64) (uint64, bool) {
	hi, lo := bits.Mul64(clusterCount, mapEntrySize)
	if hi != 0 {
		return 0, false
	}
	return alignUp(lo, mapAlign)
}

// alignUp is the smallest multiple of a (a power of two) that is >= x, or
// false on overflow.
func alignUp(x, a uint64) (uint64, bool) {
	r, carry := bits.Add64(x, a-1, 0)
	if carry != 0 {
		return 0, false
	}
	return r &^ (a - 1), true
}

// region is a half-open byte range of the file.
type region struct {
	name       string
	start, end uint64
}

func (r region) overlaps(o region) bool { return r.start < o.end && o.start < r.end }

// fixedRegions lists the regions of section 5.3 that the header alone places:
// the primary, the slots, both map copies and the backup at L - 4096. The
// digest table is absent in 1.0 (flags bit 1 is never written). The clusters
// the map references are checked with the map (section 10.1).
func (h *header) fixedRegions(fileLen uint64) ([]region, bool) {
	m, ok := mapSize(h.ClusterCount)
	if !ok {
		return nil, false
	}
	slotsEnd, c1 := bits.Add64(h.SlotsOffset, slotRegionSize, 0)
	aEnd, c2 := bits.Add64(h.MapOffset, m, 0)
	bStart, c3 := bits.Add64(h.MapOffset, h.MapStride, 0)
	bEnd, c4 := bits.Add64(bStart, m, 0)
	if c1|c2|c3|c4 != 0 || fileLen < headerSize {
		return nil, false
	}
	rs := []region{
		{"primary header", 0, headerSize},
		{"key slots", h.SlotsOffset, slotsEnd},
		{"map copy A", h.MapOffset, aEnd},
		{"map copy B", bStart, bEnd},
		{"backup header", fileLen - headerSize, fileLen},
	}
	return rs, true
}

// checkHeader applies FDD-FORMAT section 5.2 items 2-4 to a header whose seal
// opened and whose magic is FDDC (item 1), for a file of fileLen bytes. The
// first failing check decides the outcome.
func checkHeader(h *header, fileLen uint64) error {
	// Item 2: an unknown major is refused before any other field is read.
	if h.VersionMajor != versionMajor {
		return unsupportedf("format version %d.%d (this build reads major %d) - a newer build may read it", h.VersionMajor, h.VersionMinor, versionMajor)
	}
	// Item 3.
	switch {
	case h.Profile > ProfileVault:
		return unsupportedf("profile %d", uint8(h.Profile))
	case h.Flags&flagCompressed != 0:
		return unsupportedf("flags bit 0 (compressed)")
	case h.Flags&flagMultiFile != 0:
		return unsupportedf("flags bit 2 (multi-file)")
	case h.Flags&flagsReserved != 0:
		return unsupportedf("reserved flag bits 0x%02x", h.Flags&flagsReserved)
	case h.SectorShift != 9 && h.SectorShift != 12:
		return unsupportedf("sector_shift %d", h.SectorShift)
	case h.ClusterShift < minClusterShift || h.ClusterShift > maxClusterShift:
		return unsupportedf("cluster_shift %d", h.ClusterShift)
	case h.MapEntrySize != mapEntrySize:
		return unsupportedf("map_entry_size %d", h.MapEntrySize)
	case h.MapCopies != mapCopies:
		return unsupportedf("map_copies %d", h.MapCopies)
	case h.SlotCount != slotCount:
		return unsupportedf("slot_count %d", h.SlotCount)
	case h.SlotSize != slotSize:
		return unsupportedf("slot_size %d", h.SlotSize)
	case h.KDFID > kdfArgon2id:
		return unsupportedf("kdf_id %d", h.KDFID)
	case h.KDFID == kdfArgon2id && !knownKDFParams(h.KDFParamsID):
		return unsupportedf("kdf_params_id %d", h.KDFParamsID)
	}
	// Item 4.
	s, c := h.sectorSize(), h.clusterSize()
	if h.LogicalSize == 0 || h.LogicalSize%s != 0 {
		return damagedf("logical_size %d is not a positive multiple of the %d-byte sector", h.LogicalSize, s)
	}
	if want := (h.LogicalSize-1)>>h.ClusterShift + 1; h.ClusterCount != want {
		return damagedf("cluster_count %d, logical_size says %d", h.ClusterCount, want)
	}
	if h.AllocatedClusters > h.ClusterCount {
		return damagedf("allocated_clusters %d exceeds cluster_count %d", h.AllocatedClusters, h.ClusterCount)
	}
	m, ok := mapSize(h.ClusterCount)
	if !ok {
		return damagedf("cluster_count %d cannot be mapped", h.ClusterCount)
	}
	if h.MapOffset%mapAlign != 0 || h.MapStride%mapAlign != 0 || h.SlotsOffset%mapAlign != 0 {
		return damagedf("a region offset is not 4096-aligned")
	}
	if h.MapStride < m {
		return damagedf("map_stride %d is below the map size %d", h.MapStride, m)
	}
	if h.DataOffset%max(uint64(dataAlign), c) != 0 {
		return damagedf("data_offset %d is not aligned", h.DataOffset)
	}
	switch {
	case h.MapActive > 1 || h.SaveInProgress > 1 || h.Clean > 1:
		return damagedf("a boolean field holds a value above 1")
	case h.SaveInProgress == 1 && h.SaveStarted == 0:
		return damagedf("save_in_progress without save_started")
	case h.SaveInProgress == 1 && h.Clean == 1:
		return damagedf("save_in_progress and clean are both set")
	case h.Created == 0:
		return damagedf("created is 0")
	case h.obfuscated() && h.KDFID != kdfNone:
		return damagedf("an obfuscated container names kdf_id %d", h.KDFID)
	case !h.obfuscated() && h.KDFID != kdfArgon2id:
		return damagedf("an encrypted container names kdf_id %d", h.KDFID)
	case h.Profile == ProfileVault && h.obfuscated():
		return damagedf("a vault container marked pepper-wrapped")
	case h.Flags&flagDigest == 0 && (h.DigestOffset != 0 || h.DigestLength != 0 || h.ManifestRoot != [32]byte{}):
		return damagedf("digest fields set without flags bit 1")
	case h.Flags&flagMultiFile == 0 && (h.PartID != [16]byte{} || h.PartIndex != 0 || h.PartCount != 1):
		return damagedf("multi-file fields set without flags bit 2")
	case h.Flags&flagCompressed == 0 && (h.CompressTableOffset != 0 || h.CompressTableLength != 0):
		return damagedf("compression fields set without flags bit 0")
	}
	rs, ok := h.fixedRegions(fileLen)
	if !ok {
		return damagedf("the regions do not fit a file of %d bytes", fileLen)
	}
	for i, r := range rs {
		if r.end > fileLen {
			return damagedf("the %s ends at %d, past the end of the %d-byte file", r.name, r.end, fileLen)
		}
		for _, o := range rs[i+1:] {
			if r.overlaps(o) {
				return damagedf("the %s overlaps the %s", r.name, o.name)
			}
		}
	}
	return nil
}

// friendlyName returns the name: the bytes before the first zero (section 5).
func (h *header) friendlyName() string {
	n := len(h.FriendlyName)
	for i, c := range h.FriendlyName {
		if c == 0 {
			n = i
			break
		}
	}
	return string(h.FriendlyName[:n])
}

func knownKDFParams(id uint32) bool {
	_, ok := kdfParamsTable[id]
	return ok
}
