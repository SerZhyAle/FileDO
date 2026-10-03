package vdisk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"sync"
	"time"

	"filedo/fdsec"

	"golang.org/x/text/unicode/norm"
)

// Randomness and time come through these two variables so that the vectors
// of FDD-FORMAT section 15 can fix every random input and every timestamp.
// Nothing but a test replaces them.
var (
	randSource io.Reader = rand.Reader
	nowFunc              = time.Now
)

// WriterStamp is the yyMMddHHmm stamp of the running build as the integer its
// digits spell (FDD-FORMAT section 5, writer_stamp); 0 means unknown. The
// program sets it once at start-up; the package never guesses it.
var WriterStamp uint64

// OpenMode says what an open may do to the file.
type OpenMode uint8

const (
	// OpenRead writes nothing, ever - not the other header copy, not a
	// counter, not a timestamp (FDD-BEHAVIOUR section 3 rule 2).
	OpenRead OpenMode = iota
	// OpenWrite repairs the header pair at open, and clears the clean marker
	// before the first data or map write.
	OpenWrite
	// OpenMount is OpenWrite that also counts the mount in mount_count.
	OpenMount
)

// ProgressSink receives the progress of a long operation. The program adapts
// its own progress tracker to it; the package imports no product code, so it
// is testable anywhere. A nil sink is allowed.
type ProgressSink interface {
	Progress(done, total int64)
}

func report(p ProgressSink, done, total int64) {
	if p != nil {
		p.Progress(done, total)
	}
}

// stopped reports whether ctx has ended the operation. The program cancels ctx
// from its one stop model (the stop file and Ctrl+C).
func stopped(ctx context.Context) bool {
	return ctx != nil && ctx.Err() != nil
}

// CreateOptions describes a new container.
type CreateOptions struct {
	Path         string
	LogicalSize  int64 // the size of the volume
	Profile      Profile
	ClusterShift uint8 // 0 = DefaultClusterShift
	SectorShift  uint8 // 0 = DefaultSectorShift
	FriendlyName string
	Digests      bool             // the digest table (Q5): format 1.0 reserves it, no build writes it
	Credential   fdsec.Credential // empty = obfuscated, the pepper-wrapped slot 0; otherwise encrypted
	Keyfile      bool             // the credential is a keyfile's digest: its slot is kind 3, not 2
	Progress     ProgressSink     // preallocation of fast and ram
}

// Info is what the header says, readable without any credential (FDD-FORMAT
// section 12.1, steps 1-3).
type Info struct {
	Path              string
	VersionMajor      int
	VersionMinor      int
	Profile           Profile
	Obfuscated        bool // flags bit 3: obfuscated when set, encrypted when clear
	Flags             uint8
	SectorSize        int64
	ClusterSize       int64
	LogicalSize       int64
	FileSize          int64 // L, the file's length now
	PhysicalSize      int64 // the length the header was written for
	MissingBytes      int64 // physical_size - L when the file is shorter than its header says
	ClusterCount      int64
	AllocatedClusters int64
	MapGeneration     uint64
	Clean             bool
	SaveInProgress    bool
	SaveStarted       time.Time // zero when none
	LastGoodSave      time.Time // zero when never
	Created           time.Time
	MountCount        uint64
	ContainerID       string // UUID text
	FriendlyName      string
	WriterStamp       uint64
	KDFID             uint32
	KDFParamsID       uint32
	FromBackup        bool   // the primary did not open and the backup carried the header
	Backup            string // "current", "older" or "unreadable"; "" when the backup carried
}

// Protection names the layer in the words of FDD-BEHAVIOUR section 2.
func (i Info) Protection() string {
	if i.Obfuscated {
		return "obfuscated"
	}
	return "encrypted"
}

// Container is an open container. Its methods are safe for concurrent use; a
// block server calls ReadAt, WriteAt and Flush from several goroutines.
type Container struct {
	mu      sync.Mutex
	path    string
	b       backing
	mode    OpenMode
	closed  bool
	failed  error // a failed write or flush: nothing more is written, and Close does not mark it clean
	res     headerResolution
	hdr     header // the committed header: the plaintext the primary holds
	saltP   []byte
	saltB   []byte
	fileLen int64 // L as this writer maintains it; the backup header sits at fileLen - 4096

	// failedHeader says failed came from a header write or its flush: which
	// copy the file now holds is uncertain, so a ram save never clears it
	// (AUD-35-F5; a failed data write is retried instead).
	failedHeader bool

	entries   []uint64 // the working map: committed entries plus pending allocations
	used      *bitset  // physical indices the working map references
	meta      map[uint64]bool
	allocated uint64
	allocHint uint64
	pending   bool // the working map differs from the committed one
	session   bool // clean = 0 has been written in this session
	// openedClean is the clean marker as the file had it when it was opened.
	// beginSession clears the marker for the session; closeSession puts "closed
	// cleanly" back only where that is true (see keepMarker).
	openedClean bool

	cipher *sectorCipher
	ram    *ramState
}

// Create makes a new container file and returns it open for writing. It never
// replaces an existing file. An interrupted or failed creation removes the
// partial file, so nothing that looks complete is left (FDD-BEHAVIOUR 6.7).
func Create(ctx context.Context, o CreateOptions) (*Container, error) {
	if err := validateCreate(&o); err != nil {
		return nil, err
	}
	fb, err := createFile(o.Path)
	if err != nil {
		return nil, err
	}
	c, err := createOn(ctx, fb, o)
	if err != nil {
		fb.Close()
		os.Remove(o.Path)
		return nil, err
	}
	c.path = o.Path
	return c, nil
}

func validateCreate(o *CreateOptions) error {
	if o.SectorShift == 0 {
		o.SectorShift = DefaultSectorShift
	}
	if o.ClusterShift == 0 {
		o.ClusterShift = DefaultClusterShift
	}
	switch {
	case o.SectorShift != 9 && o.SectorShift != 12:
		return usagef("the sector size must be 512 or 4096 bytes (shift 9 or 12), not shift %d", o.SectorShift)
	case o.ClusterShift < minClusterShift || o.ClusterShift > maxClusterShift:
		return usagef("the cluster size must be 64 KiB to 4 MiB (shift 16..22), not shift %d", o.ClusterShift)
	case o.LogicalSize <= 0 || o.LogicalSize%(1<<o.SectorShift) != 0:
		return usagef("the volume size %d is not a positive multiple of the %d-byte sector", o.LogicalSize, 1<<o.SectorShift)
	}
	cc := uint64(o.LogicalSize-1)>>o.ClusterShift + 1
	if m, ok := mapSize(cc); !ok || m > math.MaxInt32 {
		return unsupportedf("a %d-byte volume with %d-byte clusters needs a map this build cannot hold", o.LogicalSize, 1<<o.ClusterShift)
	}
	switch o.Profile {
	case ProfilePlain, ProfileFast, ProfileRAM:
	case ProfileSealed:
		return unsupportedf("a sealed container is made by seal from an existing one, not created")
	case ProfileVault:
		// FDD-BEHAVIOUR 7 rule 3: refused with the reason, never silently
		// made obfuscated instead.
		if len(o.Credential) == 0 {
			return ErrVaultNeedsCredential
		}
	default:
		return usagef("unknown profile %d", uint8(o.Profile))
	}
	if o.Keyfile && len(o.Credential) == 0 {
		return usagef("a keyfile credential is the keyfile's digest and is never empty")
	}
	if o.Digests {
		return unsupportedf("format 1.0 reserves the digest table but defines no layout for it; no build writes one yet")
	}
	o.FriendlyName = norm.NFC.String(o.FriendlyName)
	if len(o.FriendlyName) > 64 {
		return usagef("the friendly name is %d bytes of UTF-8; at most 64", len(o.FriendlyName))
	}
	for i := 0; i < len(o.FriendlyName); i++ {
		if o.FriendlyName[i] == 0 {
			return usagef("the friendly name contains a zero byte")
		}
	}
	return nil
}

func draw(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(randSource, b); err != nil {
		return nil, err
	}
	return b, nil
}

// createOn writes a new container into an empty backing. Randomness is drawn
// in the order of FDD-FORMAT section 15.1.
func createOn(ctx context.Context, b backing, o CreateOptions) (*Container, error) {
	kdfSalt, err := draw(16)
	if err != nil {
		return nil, err
	}
	id, err := draw(16)
	if err != nil {
		return nil, err
	}
	id[6] = id[6]&0x0f | 0x40 // UUID version 4
	id[8] = id[8]&0x3f | 0x80 // RFC 9562 variant
	dataKey, err := draw(dataKeySize)
	if err != nil {
		return nil, err
	}
	defer clear(dataKey)
	// The key the one used slot is sealed under: obf_key from public data
	// alone, or cred_key from the credential - only the second makes the
	// container encrypted (section 7.3).
	flags := uint8(flagPepperWrapped)
	kind := byte(slotKindPepper)
	var kdfID, paramsID uint32
	var k []byte
	if len(o.Credential) != 0 {
		if k, err = credKey(o.Credential, kdfSalt, writerKDFParamsID); err != nil {
			return nil, err
		}
		flags, kind, kdfID, paramsID = 0, slotKindFor(o.Keyfile), kdfArgon2id, writerKDFParamsID
	} else {
		ok := obfKey(kdfSalt)
		k = ok[:]
	}
	slots, err := drawSlotsKind(randSource, k, kind, dataKey)
	clear(k)
	if err != nil {
		return nil, err
	}
	saltP, err := draw(headerSaltSize)
	if err != nil {
		return nil, err
	}
	saltB, err := draw(headerSaltSize)
	if err != nil {
		return nil, err
	}

	now := uint64(nowFunc().UnixNano())
	if now == 0 {
		now = 1
	}
	h := header{
		VersionMajor: versionMajor,
		VersionMinor: versionMinor,
		Profile:      o.Profile,
		Flags:        flags,
		KDFID:        kdfID,
		KDFParamsID:  paramsID,
		SectorShift:  uint16(o.SectorShift),
		ClusterShift: uint32(o.ClusterShift),
		LogicalSize:  uint64(o.LogicalSize),
		MapOffset:    writerMapOffset,
		MapEntrySize: mapEntrySize,
		MapCopies:    mapCopies,
		SlotsOffset:  writerSlotsOffset,
		SlotCount:    slotCount,
		SlotSize:     slotSize,
		Clean:        1,
		LastGoodSave: now,
		Created:      now,
		WriterStamp:  WriterStamp,
		PartCount:    1,
	}
	h.ClusterCount = (h.LogicalSize-1)>>h.ClusterShift + 1
	copy(h.ContainerID[:], id)
	copy(h.FriendlyName[:], o.FriendlyName)
	copy(h.KDFSalt[:], kdfSalt)
	m, _ := mapSize(h.ClusterCount)
	h.MapStride = m
	metaEnd := h.MapOffset + h.MapStride + m
	h.DataOffset, _ = alignUp(metaEnd, max(uint64(dataAlign), h.clusterSize()))
	// A fixed carrier reserves map_stride for the largest volume it can ever
	// hold, so a later grow inside the partition moves no map (FDD-FORMAT
	// section 3.1 rule 5).
	capacity := fixedCap(b)
	if capacity != 0 {
		stride, ok := partitionStride(capacity, o.ClusterShift)
		if !ok || stride < m {
			return nil, unsupportedf("a %d-byte volume does not fit a %d-byte partition", h.LogicalSize, capacity)
		}
		h.MapStride = stride
		metaEnd = h.MapOffset + h.MapStride + m
		h.DataOffset = partitionDataOffset(stride, o.ClusterShift)
	}

	c := &Container{b: b, mode: OpenWrite, saltP: saltP, saltB: saltB, used: newBitset(), openedClean: true} // a new container has no history to keep
	if c.cipher, err = newSectorCipher(dataKey, h.SectorShift); err != nil {
		return nil, err
	}
	c.entries = make([]uint64, h.ClusterCount)
	full := o.Profile == ProfileFast || o.Profile == ProfileRAM // vault allocates on demand, as plain does
	for i := range c.entries {
		if full {
			c.entries[i] = uint64(i)
			c.used.set(uint64(i))
		} else {
			c.entries[i] = sentinel
		}
	}
	if full {
		c.allocated = h.ClusterCount
		c.fileLen = int64(h.DataOffset + h.ClusterCount*h.clusterSize() + headerSize)
	} else {
		c.fileLen = int64(metaEnd + headerSize)
	}
	// On a fixed carrier L is the partition's length from the first write on:
	// the backup sits at its end and nothing ever extends, so the whole
	// volume must fit now (FDD-FORMAT section 3.1 rules 1-2).
	if capacity != 0 {
		if need := int64(h.DataOffset + h.ClusterCount*h.clusterSize() + headerSize); need > capacity {
			return nil, unsupportedf("a %d-byte volume needs %d bytes; the partition holds %d", h.LogicalSize, need, capacity)
		}
		c.fileLen = capacity
	}

	if _, err := b.WriteAt(slots, int64(h.SlotsOffset)); err != nil {
		return nil, ioErr(err)
	}
	mapBytes := encodeMap(c.entries, m)
	for _, copyIndex := range []uint8{0, 1} {
		if _, err := b.WriteAt(mapBytes, int64(h.mapCopyOffset(copyIndex))); err != nil {
			return nil, ioErr(err)
		}
	}
	if full {
		// An allocated cluster is whole (section 9): every sector is written,
		// here as the ciphertext of zeros at its own logical index.
		cs := int64(h.clusterSize())
		plain := make([]byte, cs)
		ct := make([]byte, cs)
		for i := uint64(0); i < h.ClusterCount; i++ {
			if stopped(ctx) {
				return nil, ErrStopped
			}
			c.cipher.encrypt(ct, plain, i*uint64(cs)>>h.SectorShift)
			if _, err := b.WriteAt(ct, int64(h.DataOffset)+int64(i)*cs); err != nil {
				return nil, ioErr(err)
			}
			report(o.Progress, int64(i+1), int64(h.ClusterCount))
		}
	}
	if err := b.Sync(); err != nil {
		return nil, ioErr(err)
	}
	h.MapGeneration = 1
	h.AllocatedClusters = c.allocated
	h.MapDigest = mapDigest(mapBytes)
	h.PhysicalSize = uint64(c.fileLen)
	c.meta = h.metaClusters()
	if err := c.writeHeaderPair(h); err != nil {
		return nil, err
	}
	c.res = headerResolution{hdr: h, backup: "current"}
	if o.Profile == ProfileRAM {
		c.ram = newRAMState()
	}
	return c, nil
}

// Open opens a container. cred opens an encrypted container and is not used
// for an obfuscated one, which
// needs none (FDD-BEHAVIOUR section 7 rule 4). A read-only open never writes;
// a failed open of any kind leaves the file byte-identical.
func Open(ctx context.Context, path string, cred fdsec.Credential, mode OpenMode) (*Container, error) {
	fb, err := openFile(path, mode != OpenRead, true)
	if err != nil {
		return nil, err
	}
	c, err := openOn(ctx, fb, cred, mode)
	if err != nil {
		fb.Close()
		return nil, err
	}
	c.path = path
	return c, nil
}

// Inspect reads the header only: no data key, no credential, no lock and no
// write access. It is what makes a report work on an encrypted container and
// on a read-only medium.
func Inspect(path string) (Info, error) {
	fb, err := openFile(path, false, false)
	if err != nil {
		return Info{}, err
	}
	defer fb.Close()
	L, err := fb.Size()
	if err != nil {
		return Info{}, ioErr(err)
	}
	res, err := resolveHeaders(fb, L)
	if err != nil {
		return Info{}, err
	}
	info := res.info(L)
	info.Path = path
	return info, nil
}

func openOn(ctx context.Context, b backing, cred fdsec.Credential, mode OpenMode) (*Container, error) {
	L, err := b.Size()
	if err != nil {
		return nil, ioErr(err)
	}
	res, err := resolveHeaders(b, L)
	if err != nil {
		return nil, err
	}
	h := res.hdr
	dataKey, _, err := unlockSlots(b, &h, cred)
	if err != nil {
		return nil, err
	}
	defer clear(dataKey)
	m, _ := mapSize(h.ClusterCount)
	if m > math.MaxInt32 {
		return nil, unsupportedf("the cluster map is %d bytes, more than this build can hold", m)
	}
	copyBytes := make([]byte, m)
	if err := readFull(b, copyBytes, int64(h.mapCopyOffset(h.MapActive))); err != nil {
		return nil, err
	}
	entries, err := checkMap(&h, copyBytes, uint64(L))
	if err != nil {
		if uint64(L) < h.PhysicalSize {
			return nil, truncatedf(h.PhysicalSize-uint64(L), err)
		}
		return nil, err
	}
	c := &Container{b: b, mode: mode, res: res, hdr: h, fileLen: L, entries: entries, used: newBitset(), meta: h.metaClusters(), openedClean: h.Clean == 1}
	c.saltP = append([]byte(nil), res.saltP[:]...)
	if res.backupSaltKnown {
		c.saltB = append([]byte(nil), res.saltB[:]...)
	}
	for _, e := range entries {
		if e != sentinel {
			c.used.set(e)
			c.allocated++
		}
	}
	if c.cipher, err = newSectorCipher(dataKey, h.SectorShift); err != nil {
		return nil, err
	}
	if mode == OpenRead {
		return c, nil
	}
	switch {
	case h.VersionMinor > versionMinor:
		return nil, unsupportedf("format version 1.%d is newer than this writer's 1.%d; it opens read-only", h.VersionMinor, versionMinor)
	case h.Flags&flagDigest != 0:
		return nil, unsupportedf("the container carries a digest table this build does not maintain; it opens read-only")
	case h.Profile == ProfileSealed:
		return nil, unsupportedf("a sealed container is never written")
	}
	if err := c.repair(); err != nil {
		return nil, err
	}
	if h.Profile == ProfileRAM {
		c.ram = newRAMState()
	}
	return c, nil
}

// repair is what only a writer does, before it writes anything else
// (FDD-FORMAT section 6.2): rebuild the primary from the backup under a fresh
// salt, rewrite a backup that did not open or is older, and settle a file
// whose length is not the one the header was written for.
func (c *Container) repair() error {
	res := &c.res
	h := c.hdr
	// On a fixed carrier physical_size is the partition's length for the
	// container's whole life. Any other value means the partition changed
	// size under the container, or a file of another length was copied in:
	// readable, but no writer may move the backup (FDD-FORMAT 3.1 rule 1).
	if fixedCap(c.b) != 0 && h.PhysicalSize != uint64(c.fileLen) {
		return unsupportedf("the container was written for %d bytes and its partition is %d; it opens read-only", h.PhysicalSize, c.fileLen)
	}
	// A partition never extends, so a writer opens only a container whose
	// every logical cluster already has a physical place inside it: a file
	// container copied onto a partition of its own length may still be sparse
	// (FDD-FORMAT section 3.1 rule 2).
	if fixedCap(c.b) != 0 && h.clusterLimit(uint64(c.fileLen)) < h.ClusterCount+uint64(len(c.meta)) {
		return unsupportedf("the container's volume does not fit its partition when full; it opens read-only")
	}
	// A shrink interrupted between its steps 3 and 4 (section 10.2): the new
	// backup is in place at physical_size - 4096 and only the truncate is
	// missing. Nothing the header references lies past physical_size.
	if res.primaryOK && h.PhysicalSize < uint64(c.fileLen) && res.shortBackupCurrent && c.maxReferenced() <= h.clusterLimit(h.PhysicalSize) {
		if err := c.b.Truncate(int64(h.PhysicalSize)); err != nil {
			return c.fail(err)
		}
		if err := c.b.Sync(); err != nil {
			return c.fail(err)
		}
		c.fileLen = int64(h.PhysicalSize)
		res.endBackupCurrent, res.backup = true, "current"
	}
	if res.primaryOK && res.endBackupCurrent && h.PhysicalSize == uint64(c.fileLen) {
		return nil
	}
	if !res.primaryOK {
		s, err := draw(headerSaltSize)
		if err != nil {
			return err
		}
		c.saltP = s
	}
	if c.saltB == nil {
		s, err := draw(headerSaltSize)
		if err != nil {
			return err
		}
		c.saltB = s
	}
	h.PhysicalSize = uint64(c.fileLen)
	return c.writeHeaderPair(h)
}

func (c *Container) maxReferenced() uint64 {
	var top uint64
	for _, e := range c.entries {
		if e != sentinel && e+1 > top {
			top = e + 1
		}
	}
	return top
}

// headerResolution is the outcome of section 6.1.
type headerResolution struct {
	hdr                header
	primaryOK          bool
	saltP              [32]byte
	saltB              [32]byte
	backupSaltKnown    bool
	fromBackup         bool
	backup             string // for a primary that opened: "current", "older" or "unreadable"
	endBackupCurrent   bool   // the backup at L - 4096 holds the primary's plaintext
	shortBackupCurrent bool   // so does one at physical_size - 4096 inside a longer file
}

func (r *headerResolution) info(L int64) Info {
	h := &r.hdr
	i := Info{
		VersionMajor:      int(h.VersionMajor),
		VersionMinor:      int(h.VersionMinor),
		Profile:           h.Profile,
		Obfuscated:        h.obfuscated(),
		Flags:             h.Flags,
		SectorSize:        int64(h.sectorSize()),
		ClusterSize:       int64(h.clusterSize()),
		LogicalSize:       int64(h.LogicalSize),
		FileSize:          L,
		PhysicalSize:      int64(h.PhysicalSize),
		ClusterCount:      int64(h.ClusterCount),
		AllocatedClusters: int64(h.AllocatedClusters),
		MapGeneration:     h.MapGeneration,
		Clean:             h.Clean == 1,
		SaveInProgress:    h.SaveInProgress == 1,
		SaveStarted:       nsTime(h.SaveStarted),
		LastGoodSave:      nsTime(h.LastGoodSave),
		Created:           nsTime(h.Created),
		MountCount:        h.MountCount,
		ContainerID:       uuidText(h.ContainerID),
		FriendlyName:      h.friendlyName(),
		WriterStamp:       h.WriterStamp,
		KDFID:             h.KDFID,
		KDFParamsID:       h.KDFParamsID,
		FromBackup:        r.fromBackup,
		Backup:            r.backup,
	}
	if h.PhysicalSize > uint64(L) {
		i.MissingBytes = int64(h.PhysicalSize - uint64(L))
	}
	return i
}

func nsTime(ns uint64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(ns)).UTC()
}

func uuidText(id [16]byte) string {
	s := hex.EncodeToString(id[:])
	return s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
}

// resolveHeaders applies FDD-FORMAT sections 6.1 and 5.2: it finds the header
// that carries the container and checks it. It reads and never writes.
func resolveHeaders(b backing, L int64) (headerResolution, error) {
	var r headerResolution
	if L < minFileSize {
		return r, damagedf("the file is %d bytes, shorter than any container (%d)", L, minFileSize)
	}
	block := make([]byte, headerSize)
	if err := readFull(b, block, 0); err != nil {
		return r, err
	}
	ptP, saltP, okP := openHeaderBlock(block, false)
	r.saltP = saltP
	if err := readFull(b, block, L-headerSize); err != nil {
		return r, err
	}
	ptB, saltB, okB := openHeaderBlock(block, true)
	if okB {
		r.saltB, r.backupSaltKnown = saltB, true
	}
	var pt []byte
	switch {
	case okP:
		pt, r.primaryOK = ptP, true
		r.endBackupCurrent = okB && sameBytes(ptP, ptB)
		olderSeen := okB && !r.endBackupCurrent
		// When the primary names another length, the backup is looked for at
		// physical_size - 4096 as well: a shrink or an extension was cut
		// between its steps (section 10.2).
		h, _ := decodeHeader(ptP)
		if ps := int64(h.PhysicalSize); h.PhysicalSize != uint64(L) && ps >= minFileSize && ps <= L {
			if err := readFull(b, block, ps-headerSize); err != nil {
				return r, err
			}
			if ptS, saltS, okS := openHeaderBlock(block, true); okS {
				r.shortBackupCurrent = sameBytes(ptP, ptS)
				olderSeen = olderSeen || !r.shortBackupCurrent
				if !r.backupSaltKnown {
					r.saltB, r.backupSaltKnown = saltS, true
				}
			}
		}
		switch {
		case r.endBackupCurrent || r.shortBackupCurrent:
			r.backup = "current"
		case olderSeen:
			r.backup = "older"
		default:
			r.backup = "unreadable"
		}
	case okB:
		pt, r.fromBackup = ptB, true
	default:
		return r, errHeadersDamaged
	}
	h, magicOK := decodeHeader(pt)
	if !magicOK {
		return r, damagedf("the header opened without its FDDC marker")
	}
	if err := checkHeader(&h, uint64(L)); err != nil {
		return r, err
	}
	r.hdr = h
	return r, nil
}

// readFull reads exactly len(p) bytes at off. A short read inside a region the
// header placed inside the file is damage (the file changed under us or was
// cut); any other failure is I/O.
func readFull(b backing, p []byte, off int64) error {
	n, err := b.ReadAt(p, off)
	if n == len(p) {
		return nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		return damagedf("the file ends inside a region at offset %d", off)
	}
	return ioErr(err)
}

// writeHeaderPair writes h as the primary, flushes, writes it as the backup at
// fileLen - 4096, and flushes - the header write of FDD-FORMAT section 10.2's
// shape. Each copy gets a fresh nonce and pad.
func (c *Container) writeHeaderPair(h header) error {
	if c.failed != nil {
		return c.failed
	}
	pt := h.encode()
	if err := c.writeHeaderBlock(pt, 0, c.saltP, false); err != nil {
		return err
	}
	if err := c.writeHeaderBlock(pt, c.fileLen-headerSize, c.saltB, true); err != nil {
		return err
	}
	c.hdr = h
	c.res.primaryOK, c.res.endBackupCurrent, c.res.backup = true, true, "current"
	return nil
}

func (c *Container) writeHeaderBlock(pt []byte, off int64, salt []byte, backup bool) error {
	nonce, err := draw(headerNonceSize)
	if err != nil {
		return err
	}
	pad, err := draw(headerPadSize)
	if err != nil {
		return err
	}
	if _, err := c.b.WriteAt(sealHeaderBlock(salt, nonce, pad, pt, backup), off); err != nil {
		return c.failHeader(err)
	}
	if err := c.b.Sync(); err != nil {
		return c.failHeader(err)
	}
	return nil
}

// failHeader is fail for a header write or its flush. It stays sticky even on
// a ram save's path: after it, which header copy the file holds is not known,
// and nothing more is written to it (AUD-35-F5).
func (c *Container) failHeader(err error) error {
	e := c.fail(err)
	if c.failed == e {
		c.failedHeader = true
	}
	return e
}

// fail records a write or flush failure. From then on the container writes
// nothing and Close leaves the clean marker clear.
func (c *Container) fail(err error) error {
	e := ioErr(err)
	if c.failed == nil {
		c.failed = e
	}
	return e
}

// extendTo is step 1 of the extending order (FDD-FORMAT section 10.2): before
// anything is written past the current end, a backup sealing the current,
// unchanged header is written at newLen - 4096 and flushed. The file then ends
// at newLen, and every later write lands in bytes no header references.
func (c *Container) extendTo(newLen int64) error {
	if newLen <= c.fileLen {
		return nil
	}
	// A partition never extends: refused before any byte is written
	// (FDD-FORMAT section 3.1 rule 2).
	if fixedCap(c.b) != 0 {
		// Sticky: the cluster map already names the clusters this write
		// wanted, and Close must not commit it.
		return c.fail(unsupportedf("the container needs %d bytes; its partition holds %d", newLen, c.fileLen))
	}
	if err := c.writeHeaderBlock(c.hdr.encode(), newLen-headerSize, c.saltB, true); err != nil {
		return err
	}
	c.fileLen = newLen
	return nil
}

// beginSession clears the clean marker before the first data or map write of
// a session (FDD-FORMAT section 10.3), counting the mount when it is one.
func (c *Container) beginSession() error {
	if c.failed != nil {
		return c.failed
	}
	if c.session {
		return nil
	}
	h := c.hdr
	h.Clean = 0
	if c.mode == OpenMount {
		h.MountCount++
	}
	h.PhysicalSize = uint64(c.fileLen)
	if err := c.writeHeaderPair(h); err != nil {
		return err
	}
	c.session = true
	return nil
}

// commit is the three-step map commit (FDD-FORMAT section 10.2): the working
// map into the inactive copy and flush (which also flushes every data write
// before it), then the primary, then the backup, each flushed. change, when
// not nil, changes other header fields in the same commit (the geometry of
// grow, the clean marker of a close).
func (c *Container) commit(change func(h *header)) error {
	if c.failed != nil {
		return c.failed
	}
	h := c.hdr
	h.LastGoodSave = uint64(nowFunc().UnixNano())
	if change != nil {
		change(&h)
	}
	inactive := 1 - h.MapActive
	m, _ := mapSize(h.ClusterCount)
	buf := encodeMap(c.entries, m)
	if _, err := c.b.WriteAt(buf, int64(h.mapCopyOffset(inactive))); err != nil {
		return c.fail(err)
	}
	if err := c.b.Sync(); err != nil {
		return c.fail(err)
	}
	h.MapGeneration++
	h.MapActive = inactive
	h.MapDigest = mapDigest(buf)
	h.AllocatedClusters = c.allocated
	h.PhysicalSize = uint64(c.fileLen)
	if err := c.writeHeaderPair(h); err != nil {
		return err
	}
	c.pending = false
	return nil
}

func (c *Container) usable(write bool) error {
	if c.closed {
		return usagef("the container is closed")
	}
	if write && c.hdr.Profile == ProfileSealed {
		// The third of a sealed container's three refusals (P4 section 4.1),
		// after the write-protect bit and the block server's DATA PROTECT:
		// even an internal caller cannot write one by mistake.
		return usagef("a sealed container is never written")
	}
	if write && c.mode == OpenRead {
		return usagef("the container is open read-only")
	}
	if write && c.failed != nil {
		return c.failed
	}
	return nil
}

// Info reports the header the container was opened with, as updated by this
// session's own header writes.
func (c *Container) Info() Info {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.res
	r.hdr = c.hdr
	i := r.info(c.fileLen)
	i.Path = c.path
	return i
}

// ReadAt reads logical volume bytes (FDD-FORMAT section 9). A read that reaches
// the end of the volume returns what it read and io.EOF.
func (c *Container) ReadAt(p []byte, off int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usable(false); err != nil {
		return 0, err
	}
	size := int64(c.hdr.LogicalSize)
	if off < 0 {
		return 0, usagef("negative offset %d", off)
	}
	if off >= size {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > size-off {
		n = int(size - off)
	}
	if err := c.readLogical(p[:n], off); err != nil {
		return 0, err
	}
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (c *Container) readLogical(p []byte, off int64) error {
	cs := int64(c.hdr.clusterSize())
	for len(p) > 0 {
		cl := off / cs
		in := off - cl*cs
		n := int(min(int64(len(p)), cs-in))
		if c.ram != nil {
			if buf := c.ramCluster(uint64(cl)); buf != nil {
				copy(p[:n], buf[in:])
				p, off = p[n:], off+int64(n)
				continue
			}
		}
		if e := c.entries[cl]; e == sentinel {
			clear(p[:n])
		} else if err := c.readSectors(p[:n], e, cl, in); err != nil {
			return err
		}
		p, off = p[n:], off+int64(n)
	}
	return nil
}

// readSectors decrypts the sectors of physical cluster phys (logical cluster
// cl) that cover [in, in+len(p)) of the cluster, and copies that range out.
func (c *Container) readSectors(p []byte, phys uint64, cl int64, in int64) error {
	ss := int64(c.hdr.sectorSize())
	cs := int64(c.hdr.clusterSize())
	lo := in &^ (ss - 1)
	hi := (in + int64(len(p)) + ss - 1) &^ (ss - 1)
	buf := make([]byte, hi-lo)
	if err := readFull(c.b, buf, int64(c.hdr.DataOffset)+int64(phys)*cs+lo); err != nil {
		return err
	}
	c.cipher.decrypt(buf, buf, uint64(cl*cs+lo)/uint64(ss))
	copy(p, buf[in-lo:])
	return nil
}

// WriteAt writes logical volume bytes. A write past the end of the volume
// fails and writes nothing: the volume never extends itself.
func (c *Container) WriteAt(p []byte, off int64) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usable(true); err != nil {
		return 0, err
	}
	size := int64(c.hdr.LogicalSize)
	if off < 0 || off > size || int64(len(p)) > size-off {
		return 0, usagef("a %d-byte write at %d does not fit the %d-byte volume", len(p), off, size)
	}
	if len(p) == 0 {
		return 0, nil
	}
	if err := c.beginSession(); err != nil {
		return 0, err
	}
	if c.ram != nil {
		if err := c.ramWrite(p, off); err != nil {
			return 0, err
		}
		return len(p), nil
	}
	if err := c.writeLogical(p, off); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *Container) writeLogical(p []byte, off int64) error {
	h := &c.hdr
	cs := int64(h.clusterSize())
	first, last := off/cs, (off+int64(len(p))-1)/cs
	// Allocate every cluster this write needs before writing any, so that the
	// file is extended once (step 1 of the extending order) and not per cluster.
	fresh := map[int64]bool{}
	var top int64 = -1
	for cl := first; cl <= last; cl++ {
		if c.entries[cl] != sentinel {
			continue
		}
		lo, hi := max(off, cl*cs), min(off+int64(len(p)), (cl+1)*cs)
		if allZero(p[lo-off : hi-off]) {
			continue // an unallocated cluster already reads as zeros
		}
		phys := c.allocate()
		c.entries[cl] = phys
		fresh[cl] = true
		top = max(top, int64(phys))
	}
	if len(fresh) > 0 {
		c.pending = true
		if err := c.extendTo(int64(h.DataOffset) + (top+1)*cs + headerSize); err != nil {
			return err
		}
	}
	for cl := first; cl <= last; cl++ {
		lo, hi := max(off, cl*cs), min(off+int64(len(p)), (cl+1)*cs)
		seg := p[lo-off : hi-off]
		phys := c.entries[cl]
		switch {
		case phys == sentinel:
			continue
		case fresh[cl]:
			buf := make([]byte, cs)
			copy(buf[lo-cl*cs:], seg)
			if err := c.writeCluster(buf, phys, cl); err != nil {
				return err
			}
		default:
			if err := c.writeSectors(seg, phys, cl, lo-cl*cs); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeCluster encrypts a whole plaintext cluster at its logical index and
// writes it at physical position phys.
func (c *Container) writeCluster(plain []byte, phys uint64, cl int64) error {
	cs := int64(c.hdr.clusterSize())
	c.cipher.encrypt(plain, plain, uint64(cl*cs)>>c.hdr.SectorShift)
	if _, err := c.b.WriteAt(plain, int64(c.hdr.DataOffset)+int64(phys)*cs); err != nil {
		return c.fail(err)
	}
	return nil
}

// writeSectors writes seg at [in, in+len(seg)) of an allocated cluster: whole
// sectors are encrypted as they are, a partly covered sector is read,
// decrypted, patched and encrypted again.
func (c *Container) writeSectors(seg []byte, phys uint64, cl int64, in int64) error {
	ss := int64(c.hdr.sectorSize())
	cs := int64(c.hdr.clusterSize())
	lo := in &^ (ss - 1)
	hi := (in + int64(len(seg)) + ss - 1) &^ (ss - 1)
	at := int64(c.hdr.DataOffset) + int64(phys)*cs + lo
	first := uint64(cl*cs+lo) / uint64(ss)
	buf := make([]byte, hi-lo)
	if lo != in || hi != in+int64(len(seg)) {
		if err := readFull(c.b, buf, at); err != nil {
			return err
		}
		c.cipher.decrypt(buf, buf, first)
	}
	copy(buf[in-lo:], seg)
	c.cipher.encrypt(buf, buf, first)
	if _, err := c.b.WriteAt(buf, at); err != nil {
		return c.fail(err)
	}
	return nil
}

// allocate takes the lowest physical index nothing references and no map copy
// or slot region occupies.
func (c *Container) allocate() uint64 {
	p := c.allocHint
	for c.used.has(p) || c.meta[p] {
		p++
	}
	c.used.set(p)
	c.allocated++
	c.allocHint = p + 1
	return p
}

func allZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// Flush makes every write so far durable: the data, then the map commit. For
// a ram container it is a save (FDD-FORMAT section 10.4).
func (c *Container) Flush() error {
	if c.ram != nil {
		return c.Save()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.usable(false); err != nil {
		return err
	}
	if c.mode == OpenRead || !c.session {
		return nil
	}
	if c.failed != nil {
		return c.failed
	}
	if c.pending {
		return c.commit(nil)
	}
	if err := c.b.Sync(); err != nil {
		return c.fail(err)
	}
	return nil
}

// Close ends the session. After writes it saves or commits what is pending
// and then writes the header with clean = 1, physical_size = L and
// last_good_save = now - the only way the marker is ever set (FDD-FORMAT
// section 10.3). A container whose write or flush failed is closed without it.
func (c *Container) Close() error {
	defer c.lockSave()()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.closeSession()
	if cerr := c.b.Close(); err == nil && cerr != nil {
		err = ioErr(cerr)
	}
	return err
}

// Abandon ends the session the way a forced detach must (FDD-BEHAVIOUR section
// 3 rule 3): what is pending is committed and flushed, so the map describes
// every write that reached the container, but the clean marker is left clear
// - and cleared now if this session had not cleared it yet - because the file
// system above was pulled off its volume and the volume may be inconsistent.
// The next open reports the container as not closed cleanly.
func (c *Container) Abandon() error { return c.endUnclean(true) }

// Discard ends a ram session without its final save (`unmount nosave`): what
// was written since the last completed save is dropped on purpose, and the
// file stays the image that save left, marked not closed cleanly. For any
// other profile it is Abandon, since there is nothing in memory to drop.
func (c *Container) Discard() error { return c.endUnclean(c.ram == nil) }

func (c *Container) endUnclean(saveRAM bool) error {
	defer c.lockSave()()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	err := c.abandonSession(saveRAM)
	if cerr := c.b.Close(); err == nil && cerr != nil {
		err = ioErr(cerr)
	}
	return err
}

func (c *Container) abandonSession(saveRAM bool) error {
	if c.mode == OpenRead {
		return nil
	}
	if err := c.beginSession(); err != nil {
		return err
	}
	if c.ram != nil && saveRAM {
		if err := c.save(); err != nil {
			return err
		}
	}
	if c.pending {
		return c.commit(nil)
	}
	if err := c.b.Sync(); err != nil {
		return c.fail(err)
	}
	return nil
}

func (c *Container) closeSession() error {
	if c.mode == OpenRead {
		return nil
	}
	if c.failed != nil {
		return c.failed
	}
	if !c.session {
		return nil
	}
	if c.ram != nil {
		if err := c.save(); err != nil {
			return err
		}
	}
	// "Closed cleanly" is put back only where it is true (FDD-BEHAVIOUR 5 rules
	// 2 and 3, AUD-36-F1/F2): never beside an interrupted save - the reader
	// refuses clean 1 with save_in_progress 1 as damaged, and a ram container cut
	// inside a save would become unopenable after any writer verb - and never by
	// a session that did not mount over a container that was not clean when it
	// was opened (a grow or compact does not recover a volume Windows had mounted).
	keepMarker := c.hdr.SaveInProgress == 1 || (c.mode != OpenMount && !c.openedClean)
	markClean := func(h *header) {
		if !keepMarker {
			h.Clean = 1
		}
	}
	if c.pending {
		return c.commit(markClean)
	}
	if err := c.b.Sync(); err != nil {
		return c.fail(err)
	}
	h := c.hdr
	markClean(&h)
	h.PhysicalSize = uint64(c.fileLen)
	h.LastGoodSave = uint64(nowFunc().UnixNano())
	return c.writeHeaderPair(h)
}
