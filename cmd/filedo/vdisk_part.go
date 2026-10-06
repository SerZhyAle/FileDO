package main

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"filedo/vdisk"
)

// Partition disks (SP-0148): a virtual disk whose container spans a GPT
// partition FileDO created in unallocated space (FDD-FORMAT section 3.1,
// FDD-BEHAVIOUR sections 6 item 10, 8.1, 8.2). This file is the model - disks,
// their partitions and free extents, the refusal table, the locator and its
// resolution - kept free of Windows calls so the rules are tested on recorded
// layouts. Discovery lives in vdisk_disks_windows.go, the elevated steps in
// vdisk_part_windows.go.

// vdPartType is the FileDO GPT partition type GUID, FDD-FORMAT section 3.1:
// generated once on 2026-10-03 and frozen (a frozen anchor in AGENTS.md).
// Never the SP-0123 research GUID.
const vdPartType = "6AFB315B-8976-4841-84EC-D30AFA89A33D"

// vdPartName is the GPT partition name a FileDO partition carries.
const vdPartName = "FileDO disk"

const (
	vdLocatorPrefix = "fdpart:"
	vdMiB           = int64(1) << 20
	vdPartMin       = int64(vdisk.PartitionMinSize)
)

// Stable reason tokens of section 6.3, shared by `vd disks`, its JSON and the
// GUI, which maps each to its own localised words.
const (
	vdWhyMBR          = "mbr"
	vdWhyRaw          = "not-initialized"
	vdWhyOffline      = "offline"
	vdWhyReadOnly     = "read-only"
	vdWhyISCSI        = "iscsi"
	vdWhySpaces       = "spaces"
	vdWhyUSB          = "usb"
	vdWhyRemovable    = "removable"
	vdWhyBus          = "bus"
	vdWhyDynamic      = "dynamic"
	vdWhySpacesMember = "spaces-member"
	vdWhyDuplicate    = "duplicate-guid"
	vdWhyNoSlot       = "no-entry-slot"
	vdWhyTooSmall     = "too-small"
	vdWhyUnreadable   = "unreadable"
)

// vdWhyText is the English sentence of each reason (section 5.4: the reason
// from section 6.3 is the message).
var vdWhyText = map[string]string{
	vdWhyMBR:          "the disk uses MBR partitioning; partition disks need GPT, whose partitions have a stable identity",
	vdWhyRaw:          "the disk is not initialized; FileDO never initializes a disk",
	vdWhyOffline:      "the disk is offline",
	vdWhyReadOnly:     "the disk is read-only",
	vdWhyISCSI:        "the disk is an iSCSI disk (FileDO's own mounted disks among them)",
	vdWhySpaces:       "the disk is a Storage Spaces disk",
	vdWhyUSB:          "the disk is on USB; removable and USB disks are not supported in this version",
	vdWhyRemovable:    "the disk is removable; removable and USB disks are not supported in this version",
	vdWhyBus:          "the disk's bus is not one partition disks support (NVMe, SATA, SAS, SCSI, ATA, RAID, virtual disk files)",
	vdWhyDynamic:      "the disk is a dynamic disk",
	vdWhySpacesMember: "the disk belongs to a Storage Spaces pool",
	vdWhyDuplicate:    "another disk on this machine has the same GPT disk id (a cloned disk); FileDO cannot tell them apart",
	vdWhyNoSlot:       "the disk's partition table has no free entry",
	vdWhyTooSmall:     "the free space is smaller than 64 MiB",
	vdWhyUnreadable:   "the disk's partition table could not be read",
}

func vdWhy(token string) string {
	if t, ok := vdWhyText[token]; ok {
		return t
	}
	return token
}

// Known GPT partition types, by kind token.
var vdPartKinds = map[string]string{
	"EBD0A0A2-B9E5-4433-87C0-68B6B72699C7": "basic-data",
	"C12A7328-F81F-11D2-BA4B-00A0C93EC93B": "efi",
	"E3C9E316-0B5C-4DB8-817D-F92DF00215AE": "msr",
	"DE94BBA4-06D1-4D40-A16A-BFD50179D6AC": "recovery",
	"5808C8AA-7E8F-42E0-85D2-E1E90434CFB3": "ldm-metadata",
	"AF9B60A0-1431-4F62-BC68-3311714A69AD": "ldm-data",
	"E75CAF8F-F680-4CEE-AFA3-B001E56EFC2D": "spaces",
	"CADDEBF1-4400-4DE8-B103-12117DCF3CCF": "spaces-replica",
	vdPartType:                             "fileDO",
}

var vdKindText = map[string]string{
	"basic-data": "basic data", "efi": "EFI system", "msr": "Microsoft reserved", "recovery": "recovery",
	"ldm-metadata": "dynamic disk metadata", "ldm-data": "dynamic disk data", "spaces": "Storage Spaces",
	"spaces-replica": "Storage Spaces replica", "fileDO": "FileDO disk", "other": "other", "mbr": "MBR partition",
}

func vdPartKind(typeGUID string) string {
	if k, ok := vdPartKinds[strings.ToUpper(strings.Trim(typeGUID, "{}"))]; ok {
		return k
	}
	return "other"
}

// vdDiskPart is one partition entry of a disk's layout.
type vdDiskPart struct {
	Number     int      `json:"number"`
	GUID       string   `json:"guid"`
	Offset     int64    `json:"offset"`
	Length     int64    `json:"length"`
	Type       string   `json:"type"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name,omitempty"`
	Attributes uint64   `json:"attributes,omitempty"`
	FileDO     bool     `json:"fileDO"`
	Letters    []string `json:"letters,omitempty"`
	Registered string   `json:"registered,omitempty"`
}

// vdExtent is a run of unallocated space, 1 MiB-aligned.
type vdExtent struct {
	Offset int64  `json:"offset"`
	Length int64  `json:"length"`
	Usable bool   `json:"usable"`
	Reason string `json:"reason,omitempty"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// vdDisk is one physical disk as discovery saw it (section 6.2).
type vdDisk struct {
	Number        int          `json:"number"`
	GUID          string       `json:"guid,omitempty"`
	Model         string       `json:"model"`
	Vendor        string       `json:"vendor,omitempty"`
	Bus           string       `json:"bus"`
	BusType       uint32       `json:"-"`
	Size          int64        `json:"size"`
	Style         string       `json:"style"`
	Logical       int64        `json:"logical_sector"`
	Physical      int64        `json:"physical_sector"`
	Online        bool         `json:"online"`
	ReadOnly      bool         `json:"read_only"`
	Removable     bool         `json:"removable"`
	SerialPresent bool         `json:"serial_present"`
	System        bool         `json:"system,omitempty"`
	UsableStart   int64        `json:"-"`
	UsableLength  int64        `json:"-"`
	MaxPartitions int          `json:"-"`
	Usable        bool         `json:"usable"`
	Reason        string       `json:"reason,omitempty"`
	Warning       string       `json:"warning,omitempty"`
	Partitions    []vdDiskPart `json:"partitions"`
	Free          []vdExtent   `json:"free"`
}

var vdBusTokens = map[uint32]string{1: "scsi", 2: "atapi", 3: "ata", 4: "1394", 5: "ssa", 6: "fibre", 7: "usb", 8: "raid",
	9: "iscsi", 10: "sas", 11: "sata", 12: "sd", 13: "mmc", 14: "virtual", 15: "file-backed-virtual", 16: "spaces",
	17: "nvme", 18: "scm", 19: "ufs"}

func vdBusToken(t uint32) string {
	if s, ok := vdBusTokens[t]; ok {
		return s
	}
	return "unknown"
}

// vdBusAllowed: NVMe, SATA, SAS, SCSI, ATA, RAID, and virtual disk files (the
// tests run on VHDX disks; a warning says the space lives in a file).
var vdBusAllowed = map[string]bool{"scsi": true, "ata": true, "raid": true, "sas": true, "sata": true, "nvme": true, "file-backed-virtual": true}

func alignUpMiB(x int64) int64   { return (x + vdMiB - 1) / vdMiB * vdMiB }
func alignDownMiB(x int64) int64 { return x / vdMiB * vdMiB }

// vdFreeExtents is the usable range minus every partition, each gap aligned up
// at its start and down at its end to 1 MiB; gaps below the minimum are not
// listed (section 6.2). A zero-length entry occupies nothing.
func vdFreeExtents(start, length int64, parts []vdDiskPart) []vdExtent {
	end := start + length
	occupied := make([]vdDiskPart, 0, len(parts))
	for _, p := range parts {
		if p.Length > 0 {
			occupied = append(occupied, p)
		}
	}
	sort.Slice(occupied, func(i, j int) bool { return occupied[i].Offset < occupied[j].Offset })
	var out []vdExtent
	cursor := start
	add := func(from, to int64) {
		a, b := alignUpMiB(from), alignDownMiB(to)
		if b-a >= vdPartMin {
			out = append(out, vdExtent{Offset: a, Length: b - a})
		}
	}
	for _, p := range occupied {
		if p.Offset > cursor {
			add(cursor, min(p.Offset, end))
		}
		cursor = max(cursor, p.Offset+p.Length)
	}
	if cursor < end {
		add(cursor, end)
	}
	return out
}

// vdPartLabel names a partition for a person: its letters, else its kind.
func vdPartLabel(p vdDiskPart) string {
	if len(p.Letters) > 0 {
		return strings.Join(p.Letters, " ")
	}
	if p.Kind == "fileDO" && p.Registered != "" {
		return "FileDO disk " + p.Registered
	}
	return vdKindText[p.Kind]
}

// vdJudgeDisks applies the refusal table of section 6.3 to every disk and
// extent. A disk is usable only if every row passes; an extent only if its
// disk is and it is itself large enough.
func vdJudgeDisks(disks []vdDisk) {
	guids := map[string]int{}
	for _, d := range disks {
		if d.GUID != "" {
			guids[d.GUID]++
		}
	}
	for i := range disks {
		d := &disks[i]
		d.Reason = vdDiskRefusal(d, guids)
		d.Usable = d.Reason == ""
		if d.Bus == "file-backed-virtual" {
			d.Warning = "a virtual disk file: the partition's space lives in that file"
		}
		if d.Style == "gpt" {
			d.Free = vdFreeExtents(d.UsableStart, d.UsableLength, d.Partitions)
		}
		for j := range d.Free {
			e := &d.Free[j]
			e.Usable, e.Reason = d.Usable, d.Reason
			if e.Usable && e.Length < vdPartMin {
				e.Usable, e.Reason = false, vdWhyTooSmall
			}
			e.Before, e.After = vdNeighbours(d.Partitions, e.Offset, e.Offset+e.Length)
		}
	}
}

func vdDiskRefusal(d *vdDisk, guids map[string]int) string {
	switch {
	case d.Style == "mbr":
		return vdWhyMBR
	case d.Style != "gpt":
		return vdWhyRaw
	case !d.Online:
		return vdWhyOffline
	case d.ReadOnly:
		return vdWhyReadOnly
	case d.Bus == "iscsi" || strings.EqualFold(d.Vendor, "FileDO"):
		return vdWhyISCSI
	case d.Bus == "spaces":
		return vdWhySpaces
	case d.Bus == "usb":
		return vdWhyUSB
	case d.Removable || d.Bus == "sd" || d.Bus == "mmc":
		return vdWhyRemovable
	case !vdBusAllowed[d.Bus]:
		return vdWhyBus
	}
	for _, p := range d.Partitions {
		switch p.Kind {
		case "ldm-metadata", "ldm-data":
			return vdWhyDynamic
		case "spaces", "spaces-replica":
			return vdWhySpacesMember
		}
	}
	if d.GUID == "" || guids[d.GUID] > 1 {
		return vdWhyDuplicate
	}
	if d.MaxPartitions > 0 && len(d.Partitions) >= d.MaxPartitions {
		return vdWhyNoSlot
	}
	return ""
}

// vdNeighbours names the partitions just before and just after a range.
func vdNeighbours(parts []vdDiskPart, from, to int64) (before, after string) {
	var b, a *vdDiskPart
	for i := range parts {
		p := &parts[i]
		if p.Length == 0 {
			continue
		}
		if p.Offset+p.Length <= from && (b == nil || p.Offset > b.Offset) {
			b = p
		}
		if p.Offset >= to && (a == nil || p.Offset < a.Offset) {
			a = p
		}
	}
	if b != nil {
		before = vdPartLabel(*b)
	}
	if a != nil {
		after = vdPartLabel(*a)
	}
	return before, after
}

// ---------------------------------------------------------------- the locator

// vdPartRecord is the stored identity of a partition container (section 6.1):
// the key everywhere a path is the key for a file.
type vdPartRecord struct {
	Locator       string `json:"locator"`
	DiskGUID      string `json:"disk_guid"`
	PartitionGUID string `json:"partition_guid"`
	Offset        int64  `json:"offset"`
	Length        int64  `json:"length"`
	ContainerID   string `json:"container_id,omitempty"`
	DiskModel     string `json:"disk_model,omitempty"`
	Bus           string `json:"bus,omitempty"`
	// Pending is the work on the partition that has not completed
	// (vdPendingCreate, vdPendingDestroy), "" when there is none.
	Pending string `json:"pending,omitempty"`
}

// The unfinished work a partition disk's registry entry records, so a later
// destroy finds the state that work leaves (FDD-BEHAVIOUR 6 item 10).
const (
	// vdPendingCreate: the creation did not complete. The partition holds
	// zeros at both header positions, or the header of the container whose
	// id the entry's Part.ContainerID recorded once the header was written.
	vdPendingCreate = "create"
	// vdPendingDestroy: a wipe began. The partition holds zeros, or what is
	// left of the registered container's header.
	vdPendingDestroy = "destroy"
)

// partPending is an entry's unfinished work: its recorded Pending, or create
// for an entry with no container id (as an earlier build wrote one).
func (e vdRegEntry) partPending() string {
	switch {
	case !e.isPart():
		return ""
	case e.Part.Pending != "":
		return e.Part.Pending
	case e.ContainerID == "":
		return vdPendingCreate
	}
	return ""
}

// partRecordedID is the container id an entry's partition must carry: the
// registered one, else the one an unfinished creation recorded.
func (e vdRegEntry) partRecordedID() string {
	if e.ContainerID != "" || e.Part == nil {
		return e.ContainerID
	}
	return e.Part.ContainerID
}

// partUnfinishedText says what an unfinished entry is and how it ends.
func (e vdRegEntry) partUnfinishedText() string {
	if e.partPending() == vdPendingDestroy {
		return fmt.Sprintf("a destroy of %s did not finish; finish it with: filedo vd destroy %s", e.Name, e.Name)
	}
	return fmt.Sprintf("the creation of %s never finished; delete it with: filedo vd destroy %s", e.Name, e.Name)
}

// findPart is the index of the partition disk entry of a partition GUID.
func (r *vdRegistry) findPart(guid string) int {
	g := vdNormGUID(guid)
	for i, e := range r.Containers {
		if e.isPart() && g != "" && vdNormGUID(e.Part.PartitionGUID) == g {
			return i
		}
	}
	return -1
}

// vdInsideExtent reports whether size bytes at offset lie inside the extent,
// in arithmetic that cannot overflow.
func vdInsideExtent(e vdExtent, offset, size int64) bool {
	if e.Offset < 0 || e.Length < 0 || size <= 0 || offset < e.Offset {
		return false
	}
	into := offset - e.Offset
	return into <= e.Length && size <= e.Length-into
}

// vdHeaderBytes is the size of one header position (FDD-FORMAT section 3.1).
const vdHeaderBytes = 4096

func vdAllZero(b []byte) bool {
	for _, x := range b {
		if x != 0 {
			return false
		}
	}
	return true
}

// vdHeaderPositionsZero reports whether both header positions of a carrier of
// L bytes read as zeros: [0, 4096) and [L - 4096, L).
func vdHeaderPositionsZero(r io.ReaderAt, L int64) (bool, error) {
	if L < 2*vdHeaderBytes {
		return false, fmt.Errorf("%w: the partition is %d bytes, too small for a header", vdisk.ErrIO, L)
	}
	buf := make([]byte, vdHeaderBytes)
	for _, off := range []int64{0, L - vdHeaderBytes} {
		if _, err := r.ReadAt(buf, off); err != nil {
			return false, fmt.Errorf("%w: %v", vdisk.ErrIO, err)
		}
		if !vdAllZero(buf) {
			return false, nil
		}
	}
	return true, nil
}

// vdProveOwn is the ownership proof of section 8.2 on what the partition holds:
// zeroed (both header positions read as zeros), or the container id its header
// carries (got; "" with why when it does not read as a container). A finished
// entry needs the registered id (want). An unfinished one is also deleted when
// both positions are zeros, and with the id it recorded when it has one.
func vdProveOwn(locator, want string, unfinished, zeroed bool, got string, why error) error {
	if got == "" && why == nil {
		why = vdisk.ErrDamaged
	}
	switch {
	case unfinished && zeroed:
		return nil
	case got == "" && unfinished:
		return fmt.Errorf("%w: the partition %s holds neither zeros nor a readable container header (%s), so it is not the unfinished work FileDO recorded; nothing was deleted", vdisk.ErrUnsupported, locator, vdisk.Explain(why))
	case got == "":
		return fmt.Errorf("the partition %s does not read as a container (%s), so FileDO cannot prove it is its own; nothing was deleted: %w", locator, vdisk.Explain(why), why)
	case want == "" && unfinished:
		return fmt.Errorf("%w: the partition %s holds container %s, which FileDO did not record for this unfinished creation; nothing was deleted - register it with filedo vd adopt %s, then destroy it", vdisk.ErrUnsupported, locator, got, locator)
	case want == "" || !strings.EqualFold(got, want):
		return fmt.Errorf("%w: the partition holds container %s, not the registered %s; nothing was deleted", vdisk.ErrUnsupported, got, want)
	}
	return nil
}

// vdAdoptVerdict is what adopt makes of a FileDO-type partition's header: a
// container (headerErr nil) is registered; a partition whose both header
// positions read as zeros is an unfinished creation, registered as such so
// destroy can delete it; anything else is refused with the header's error.
func vdAdoptVerdict(headerErr error, zeroed bool) (unfinished bool, err error) {
	switch {
	case headerErr == nil:
		return false, nil
	case zeroed:
		return true, nil
	}
	return false, headerErr
}

// vdAdoptOver decides adopt over an entry that already names the partition:
// only an unfinished creation is registered again (its header now read).
func vdAdoptOver(e vdRegEntry) error {
	switch e.partPending() {
	case vdPendingCreate:
		return nil
	case vdPendingDestroy:
		return vdUsagef("%s", e.partUnfinishedText())
	}
	return vdUsagef("%s is registered already as %s", e.Path, e.Name)
}

// vdPartMountRO is a partition disk mount's read-only before the header is
// read: asked for, or a sealed container by its registration (the header has
// the final word, vdPartMountNotes).
func vdPartMountRO(asked bool, profile string) bool {
	return asked || strings.EqualFold(profile, vdisk.ProfileSealed.String())
}

// vdPartWordIsPath reports whether a verb's word means an existing file or
// folder rather than a partition disk's name: a path that exists wins over a
// name (FDD-BEHAVIOUR section 9), as vdResolve decides for a file disk. A
// locator is never a path.
func vdPartWordIsPath(word string, exists func(string) bool) bool {
	return word != "" && !isPartLocator(word) && exists(word)
}

var vdGUIDSpelling = regexp.MustCompile(`^[0-9A-F]{8}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{12}$`)

// vdNormGUID is a GUID upper case, without braces; "" when it is not one.
func vdNormGUID(s string) string {
	g := strings.ToUpper(strings.Trim(strings.TrimSpace(s), "{}"))
	if !vdGUIDSpelling.MatchString(g) {
		return ""
	}
	return g
}

// vdLocator is the locator of a partition GUID: fdpart:{GUID}.
func vdLocator(partitionGUID string) string {
	return vdLocatorPrefix + "{" + vdNormGUID(partitionGUID) + "}"
}

// isPartLocator reports whether a word is spelled as a locator.
func isPartLocator(s string) bool {
	return strings.HasPrefix(strings.ToLower(s), vdLocatorPrefix)
}

// vdLocatorGUID is the partition GUID a locator names, or "".
func vdLocatorGUID(s string) string {
	if !isPartLocator(s) {
		return ""
	}
	return vdNormGUID(s[len(vdLocatorPrefix):])
}

var (
	// errVdPartNotConnected: no disk on this machine has the partition.
	errVdPartNotConnected = errors.New("the partition disk is not connected")
	// errVdPartAmbiguous: two or more disks have it (a cloned disk).
	errVdPartAmbiguous = errors.New("the partition was found on more than one disk")
	// errVdPartChanged: it is there, but not where or what it was.
	errVdPartChanged = errors.New("the partition changed")
)

// vdResolvePart is the resolution of section 6.1 over a discovery: exactly one
// entry whose partition GUID is the record's, on the record's disk, at its
// offset and length, of the FileDO type. The disk number it returns is good
// for this operation only and is never stored.
func vdResolvePart(rec vdPartRecord, disks []vdDisk) (vdDisk, vdDiskPart, error) {
	want := vdNormGUID(rec.PartitionGUID)
	if want == "" {
		return vdDisk{}, vdDiskPart{}, vdUsagef("%s is not a partition locator", rec.Locator)
	}
	var hits []int
	var parts []vdDiskPart
	for i, d := range disks {
		for _, p := range d.Partitions {
			if vdNormGUID(p.GUID) == want {
				hits = append(hits, i)
				parts = append(parts, p)
			}
		}
	}
	switch len(hits) {
	case 0:
		return vdDisk{}, vdDiskPart{}, fmt.Errorf("%w: no disk on this machine has the partition %s; connect its disk, or forget the name", errVdPartNotConnected, rec.Locator)
	case 1:
	default:
		return vdDisk{}, vdDiskPart{}, fmt.Errorf("%w: %d disks have the partition %s (a cloned disk?); disconnect the copy and try again", errVdPartAmbiguous, len(hits), rec.Locator)
	}
	d, p := disks[hits[0]], parts[0]
	switch {
	case !strings.EqualFold(vdNormGUID(d.GUID), vdNormGUID(rec.DiskGUID)):
		return d, p, fmt.Errorf("%w: the partition %s is on disk {%s}, not on the disk {%s} it was made on", errVdPartChanged, rec.Locator, d.GUID, rec.DiskGUID)
	case p.Offset != rec.Offset || p.Length != rec.Length:
		return d, p, fmt.Errorf("%w: the partition %s is now %d bytes at %d, not %d bytes at %d; nothing is written to it", errVdPartChanged, rec.Locator, p.Length, p.Offset, rec.Length, rec.Offset)
	case !p.FileDO:
		return d, p, fmt.Errorf("%w: the partition %s is no longer of the FileDO type; nothing is written to it", errVdPartChanged, rec.Locator)
	}
	return d, p, nil
}

// vdPartClass maps a resolution failure to its class: not connected is the
// I/O class for a read, transport unavailable for a mount (section 6.1); the
// others are "could not verify" (class 5) - the identity did not hold.
func vdPartClass(err error, mount bool) error {
	if errors.Is(err, errVdPartNotConnected) && mount {
		return errTransport(err.Error())
	}
	if errors.Is(err, errVdPartNotConnected) || errors.Is(err, errVdPartAmbiguous) || errors.Is(err, errVdPartChanged) {
		return fmt.Errorf("%w: %s", vdisk.ErrIO, err.Error())
	}
	return err
}

// vdSameLayout is the whole-list comparison of section 6.4: every entry's
// GUID, offset, length and type the same.
func vdSameLayout(a, b []vdDiskPart) bool {
	if len(a) != len(b) {
		return false
	}
	key := func(p vdDiskPart) string {
		return fmt.Sprintf("%s|%d|%d|%s", vdNormGUID(p.GUID), p.Offset, p.Length, strings.ToUpper(p.Type))
	}
	ka, kb := make([]string, len(a)), make([]string, len(b))
	for i := range a {
		ka[i], kb[i] = key(a[i]), key(b[i])
	}
	sort.Strings(ka)
	sort.Strings(kb)
	for i := range ka {
		if ka[i] != kb[i] {
			return false
		}
	}
	return true
}

// vdNewEntry is the after-check of section 6.4: exactly one new entry, at the
// confirmed offset and size, of the FileDO type; every other entry unchanged.
func vdNewEntry(before, after []vdDiskPart, offset, size int64) (vdDiskPart, error) {
	var added []vdDiskPart
	for _, p := range after {
		found := false
		for _, q := range before {
			if vdNormGUID(p.GUID) == vdNormGUID(q.GUID) {
				found = true
				break
			}
		}
		if !found {
			added = append(added, p)
		}
	}
	if len(added) != 1 {
		return vdDiskPart{}, fmt.Errorf("the partition table shows %d new entries, not one", len(added))
	}
	n := added[0]
	rest := make([]vdDiskPart, 0, len(after)-1)
	for _, p := range after {
		if vdNormGUID(p.GUID) != vdNormGUID(n.GUID) {
			rest = append(rest, p)
		}
	}
	switch {
	case !vdSameLayout(before, rest):
		return n, fmt.Errorf("another partition entry changed while the new one was made")
	case n.Offset != offset || n.Length != size:
		return n, fmt.Errorf("the new partition is %d bytes at %d, not %d bytes at %d", n.Length, n.Offset, size, offset)
	case !n.FileDO:
		return n, fmt.Errorf("the new partition is not of the FileDO type")
	}
	return n, nil
}

// vdPickExtent chooses the free extent of a new partition and its size: at
// picks the extent by its start (-1: the first usable extent that fits), size
// 0 is the whole extent, and a size is rounded down to 1 MiB (section 4.2).
func vdPickExtent(d vdDisk, at, size int64) (vdExtent, int64, error) {
	if !d.Usable {
		return vdExtent{}, 0, fmt.Errorf("%w: disk %d: %s", vdisk.ErrUnsupported, d.Number, vdWhy(d.Reason))
	}
	if size != 0 {
		size = alignDownMiB(size)
		if size < vdPartMin {
			return vdExtent{}, 0, vdUsagef("a partition disk is at least 64 MiB")
		}
	}
	for _, e := range d.Free {
		if at >= 0 && e.Offset != at {
			continue
		}
		if !e.Usable {
			if at >= 0 {
				return vdExtent{}, 0, fmt.Errorf("%w: the free space at %d: %s", vdisk.ErrUnsupported, at, vdWhy(e.Reason))
			}
			continue
		}
		n := size
		if n == 0 {
			n = e.Length
		}
		if n > e.Length {
			if at >= 0 {
				return vdExtent{}, 0, vdUsagef("the free space at %d holds %s, less than the %s asked for", at, vdHumanSize(e.Length), vdHumanSize(n))
			}
			continue
		}
		return e, n, nil
	}
	if at >= 0 {
		return vdExtent{}, 0, vdUsagef("disk %d has no free space starting at %d (see: filedo vd disks)", d.Number, at)
	}
	return vdExtent{}, 0, vdUsagef("disk %d has no usable free space of that size (see: filedo vd disks)", d.Number)
}

// vdPrintDisks is the text of `vd disks` (section 4.1).
func vdPrintDisks(w io.Writer, disks []vdDisk) {
	for i, d := range disks {
		if i > 0 {
			fmt.Fprintln(w)
		}
		model := d.Model
		if model == "" {
			model = "(no model)"
		}
		state := "online"
		switch {
		case !d.Online:
			state = "offline"
		case d.ReadOnly:
			state = "read-only"
		}
		style := strings.ToUpper(d.Style)
		if d.GUID != "" {
			style += " {" + d.GUID + "}"
		}
		fmt.Fprintf(w, "Disk %d  %s  %s  %s  %s  sectors %d/%d  %s", d.Number, model, d.Bus, vdHumanSize(d.Size), style, d.Logical, d.Physical, state)
		if d.System {
			fmt.Fprint(w, "  (system disk)")
		}
		fmt.Fprintln(w)
		if !d.Usable {
			fmt.Fprintf(w, "  not usable for partition disks - %s\n", vdWhy(d.Reason))
		} else if d.Warning != "" {
			fmt.Fprintf(w, "  note: %s\n", d.Warning)
		}
		parts := append([]vdDiskPart(nil), d.Partitions...)
		sort.Slice(parts, func(a, b int) bool { return parts[a].Offset < parts[b].Offset })
		for _, p := range parts {
			if p.Length == 0 {
				continue
			}
			kind := vdKindText[p.Kind]
			if l := strings.Join(p.Letters, " "); l != "" {
				kind += " " + l
			}
			line := fmt.Sprintf("  partition %-3d %-24s %10s  at %s", p.Number, kind, vdHumanSize(p.Length), vdHumanSize(p.Offset))
			if p.FileDO {
				if p.Registered != "" {
					line += "  " + p.Registered
				} else {
					line += "  not registered - filedo vd adopt " + vdLocator(p.GUID)
				}
			}
			fmt.Fprintln(w, line)
		}
		for _, e := range d.Free {
			between := ""
			switch {
			case e.Before != "" && e.After != "":
				between = fmt.Sprintf(" (between %s and %s)", e.Before, e.After)
			case e.Before != "":
				between = fmt.Sprintf(" (after %s)", e.Before)
			case e.After != "":
				between = fmt.Sprintf(" (before %s)", e.After)
			}
			verdict := "usable"
			if !e.Usable {
				verdict = "not usable - " + vdWhy(e.Reason)
			}
			fmt.Fprintf(w, "  free          %-24s %10s  at %s%s  %s\n", "", vdHumanSize(e.Length), vdHumanSize(e.Offset), between, verdict)
			if e.Usable {
				fmt.Fprintf(w, "                create: filedo vd new part disk:{%s} size max at %d\n", d.GUID, e.Offset)
			}
		}
	}
}

// vdHumanSize is a size with one decimal in the largest unit that fits.
func vdHumanSize(n int64) string {
	switch {
	case n >= 1<<40:
		return fmt.Sprintf("%.1f TiB", float64(n)/(1<<40))
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	default:
		return fmt.Sprintf("%d MiB", n>>20)
	}
}

// vdPartReadCacheEnv names the knob that sizes (or, at 0, switches off) the
// read cache of a served partition disk. It exists for the speed measurement's
// baseline (SP-0148 S9, manual kit H14); the product leaves it unset.
const vdPartReadCacheEnv = "FILEDO_VD_READCACHE_MB"

// vdPartReadCacheBytes is the cache cap for a served partition disk: the
// default, or the megabytes in value when it parses as a whole number >= 0.
func vdPartReadCacheBytes(value string) int64 {
	mb, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || mb < 0 || mb > 1<<20 {
		return vdisk.ReadCacheDefaultBytes
	}
	return mb << 20
}
