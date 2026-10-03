//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"filedo/vdisk"
)

// Discovery of disks, partitions and free extents for partition disks
// (SP-0148 6.2). Every handle is opened with desired access 0, which needs no
// elevation and mounts nothing (SP-0123 E-D1): device number, storage
// descriptors, geometry, attributes, the layout, and the volumes' extents.

const (
	ioctlStorageGetDeviceNumber = 0x002D1080
	ioctlDiskGetDiskAttributes  = 0x000700F0
	ioctlDiskGetPartitionInfoEx = 0x00070048
	vdMaxDisks                  = 64
	partitionEntrySize          = 144
)

// vdOpen0 opens a device for queries only.
func vdOpen0(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, 0, 0)
}

func le32(b []byte, o int) uint32 {
	if len(b) < o+4 {
		return 0
	}
	return binary.LittleEndian.Uint32(b[o:])
}

func le64(b []byte, o int) uint64 {
	if len(b) < o+8 {
		return 0
	}
	return binary.LittleEndian.Uint64(b[o:])
}

func guidAt(b []byte, o int) string {
	if len(b) < o+16 {
		return ""
	}
	var g windows.GUID
	copy((*[16]byte)(unsafe.Pointer(&g))[:], b[o:o+16])
	return vdNormGUID(g.String())
}

func cstrAt(b []byte, o uint32) string {
	if o == 0 || int(o) >= len(b) {
		return ""
	}
	v := b[o:]
	if e := bytes.IndexByte(v, 0); e >= 0 {
		v = v[:e]
	}
	return strings.TrimSpace(string(v))
}

// vdStorageProperty runs IOCTL_STORAGE_QUERY_PROPERTY for one property id.
func vdStorageProperty(h windows.Handle, id uint32, size int) ([]byte, error) {
	q := make([]byte, 12)
	binary.LittleEndian.PutUint32(q, id)
	out := make([]byte, size)
	n, err := ioctl(h, ioctlStorageQueryProperty, q, out)
	if err != nil {
		return nil, err
	}
	return out[:n], nil
}

// vdReadLayout reads a disk's or partition's layout: the style, the GPT disk
// GUID and usable range, and every entry.
func vdReadLayout(h windows.Handle) (style string, guid string, start, length int64, maxParts int, parts []vdDiskPart, err error) {
	size := 48 + partitionEntrySize*128
	var out []byte
	for {
		out = make([]byte, size)
		var n uint32
		n, err = ioctl(h, ioctlDiskGetDriveLayoutEx, nil, out)
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) && size < 1<<20 {
			size *= 2
			continue
		}
		if err != nil {
			return "", "", 0, 0, 0, nil, err
		}
		out = out[:n]
		break
	}
	count := int(le32(out, 4))
	switch le32(out, 0) {
	case 0:
		style = "mbr"
	case 1:
		style = "gpt"
		guid = guidAt(out, 8)
		start, length = int64(le64(out, 24)), int64(le64(out, 32))
		maxParts = int(le32(out, 40))
	default:
		return "raw", "", 0, 0, 0, nil, nil
	}
	for i := 0; i < count; i++ {
		o := 48 + partitionEntrySize*i
		if len(out) < o+partitionEntrySize {
			break
		}
		e := out[o : o+partitionEntrySize]
		p := vdPartEntry(e)
		if p.Length == 0 && style == "mbr" {
			continue
		}
		parts = append(parts, p)
	}
	return style, guid, start, length, maxParts, parts, nil
}

// vdPartEntry decodes one PARTITION_INFORMATION_EX.
func vdPartEntry(e []byte) vdDiskPart {
	p := vdDiskPart{Offset: int64(le64(e, 8)), Length: int64(le64(e, 16)), Number: int(le32(e, 24))}
	if le32(e, 0) == 1 { // GPT
		p.Type = guidAt(e, 32)
		p.GUID = guidAt(e, 48)
		p.Attributes = le64(e, 64)
		name := make([]uint16, 36)
		for i := range name {
			name[i] = binary.LittleEndian.Uint16(e[72+2*i:])
		}
		p.Name = windows.UTF16ToString(name)
		p.Kind = vdPartKind(p.Type)
		p.FileDO = p.Kind == "fileDO"
	} else {
		p.Type = fmt.Sprintf("MBR:%02X", e[32])
		p.Kind = "mbr"
	}
	return p
}

// vdPartitionInfo is the entry a partition handle is open on
// (IOCTL_DISK_GET_PARTITION_INFO_EX): how a brokered handle is checked.
func vdPartitionInfo(h windows.Handle) (vdDiskPart, error) {
	out := make([]byte, partitionEntrySize)
	if _, err := ioctl(h, ioctlDiskGetPartitionInfoEx, nil, out); err != nil {
		return vdDiskPart{}, err
	}
	return vdPartEntry(out), nil
}

// vdReadDisk describes PhysicalDrive n; ok is false when there is none.
func vdReadDisk(n int) (vdDisk, bool) {
	h, err := vdOpen0(fmt.Sprintf(`\\.\PhysicalDrive%d`, n))
	if err != nil {
		return vdDisk{}, false
	}
	defer windows.CloseHandle(h)
	d := vdDisk{Number: n, Online: true, Partitions: []vdDiskPart{}, Free: []vdExtent{}}
	if b, err := vdStorageProperty(h, 0, 4096); err == nil && len(b) >= 32 {
		d.Removable = b[10] != 0
		d.Vendor = cstrAt(b, le32(b, 12))
		d.Model = strings.TrimSpace(d.Vendor + " " + cstrAt(b, le32(b, 16)))
		d.SerialPresent = cstrAt(b, le32(b, 24)) != ""
		d.BusType = le32(b, 28)
	}
	d.Bus = vdBusToken(d.BusType)
	if b, err := vdStorageProperty(h, 6, 28); err == nil && len(b) >= 24 {
		d.Logical, d.Physical = int64(le32(b, 16)), int64(le32(b, 20))
	}
	g := make([]byte, 256)
	if _, err := ioctl(h, ioctlDiskGetDriveGeometryEx, nil, g); err == nil {
		d.Size = int64(le64(g, 24))
		if d.Logical == 0 {
			d.Logical = int64(le32(g, 20))
			d.Physical = d.Logical
		}
	}
	a := make([]byte, 16)
	if _, err := ioctl(h, ioctlDiskGetDiskAttributes, nil, a); err == nil {
		attrs := le64(a, 8)
		d.Online, d.ReadOnly = attrs&diskAttributeOffline == 0, attrs&diskAttributeReadOnly != 0
	}
	style, guid, start, length, maxParts, parts, err := vdReadLayout(h)
	if err != nil {
		d.Style, d.Reason = "unknown", vdWhyUnreadable
		return d, true
	}
	d.Style, d.GUID, d.UsableStart, d.UsableLength, d.MaxPartitions = style, guid, start, length, maxParts
	if parts != nil {
		d.Partitions = parts
	}
	return d, true
}

// vdVolumeMap is every volume's letters by (disk, offset) of its extent, and
// the disk the Windows directory is on. Zero-length or multi-extent volumes
// name nothing (a zero-length extent exists on real machines, SP-0123 E-D1).
func vdVolumeMap() (letters map[[2]int64][]string, systemDisk int) {
	letters = map[[2]int64][]string{}
	systemDisk = -1
	winDir, _ := windows.GetWindowsDirectory()
	buf := make([]uint16, windows.MAX_PATH+1)
	fh, err := windows.FindFirstVolume(&buf[0], uint32(len(buf)))
	if err != nil {
		return letters, systemDisk
	}
	defer windows.FindVolumeClose(fh)
	for {
		name := windows.UTF16ToString(buf)
		names := make([]uint16, 1024)
		var rl uint32
		var mounts []string
		if windows.GetVolumePathNamesForVolumeName(&buf[0], &names[0], uint32(len(names)), &rl) == nil {
			for _, s := range multiSZ(names[:rl]) {
				if len(s) <= 3 && strings.HasSuffix(s, `:\`) {
					mounts = append(mounts, strings.TrimSuffix(s, `\`))
				}
			}
		}
		if h, err := vdOpen0(strings.TrimSuffix(name, `\`)); err == nil {
			out := make([]byte, 8+24*16)
			if n, err := ioctl(h, ioctlVolumeGetDiskExtents, nil, out); err == nil && n >= 32 && le32(out, 0) == 1 {
				disk, off, ln := int64(le32(out, 8)), int64(le64(out, 16)), int64(le64(out, 24))
				if ln > 0 {
					letters[[2]int64{disk, off}] = mounts
					for _, m := range mounts {
						if len(winDir) >= 2 && strings.EqualFold(m, winDir[:2]) {
							systemDisk = int(disk)
						}
					}
				}
			}
			windows.CloseHandle(h)
		}
		if windows.FindNextVolume(fh, &buf[0], uint32(len(buf))) != nil {
			break
		}
	}
	return letters, systemDisk
}

// vdDiscoverDisks is the whole discovery, judged by the refusal table, with
// each FileDO partition's registered name filled in.
func vdDiscoverDisks() ([]vdDisk, error) {
	var disks []vdDisk
	for n := 0; n < vdMaxDisks; n++ {
		if d, ok := vdReadDisk(n); ok {
			disks = append(disks, d)
		}
	}
	if len(disks) == 0 {
		return nil, fmt.Errorf("%w: no disk could be queried on this machine", vdisk.ErrIO)
	}
	letters, sys := vdVolumeMap()
	names := vdPartNames()
	for i := range disks {
		d := &disks[i]
		d.System = d.Number == sys
		for j := range d.Partitions {
			p := &d.Partitions[j]
			p.Letters = letters[[2]int64{int64(d.Number), p.Offset}]
			if p.FileDO {
				p.Registered = names[vdNormGUID(p.GUID)]
			}
		}
	}
	unreadable := map[int]bool{}
	for _, d := range disks {
		if d.Reason == vdWhyUnreadable {
			unreadable[d.Number] = true
		}
	}
	vdJudgeDisks(disks)
	for i := range disks {
		if unreadable[disks[i].Number] {
			disks[i].Usable, disks[i].Reason = false, vdWhyUnreadable
		}
	}
	return disks, nil
}

// vdPartNames maps each registered partition's GUID to its name.
func vdPartNames() map[string]string {
	out := map[string]string{}
	r, err := vdLoadRegistry()
	if err != nil {
		return out
	}
	for _, e := range r.Containers {
		if e.Part != nil {
			out[vdNormGUID(e.Part.PartitionGUID)] = e.Name
		}
	}
	return out
}

// vdPartDevicePath is the device of partition m on disk n, for this
// operation only (section 6.1: the numbers are never stored).
func vdPartDevicePath(disk, part int) string {
	return fmt.Sprintf(`\\?\GLOBALROOT\Device\Harddisk%d\Partition%d`, disk, part)
}

// ---------------------------------------------------------------- vd disks

// vdDisksSchema is the `vd disks json` wire (SP-0148 9.3), local to FileDO and
// its GUI.
const (
	vdDisksSchema  = "filedo.vd-disks"
	vdDisksVersion = 1
)

type vdDisksWire struct {
	Schema    string   `json:"schema"`
	Version   int      `json:"version"`
	Available bool     `json:"available"`
	Reason    string   `json:"reason,omitempty"`
	Disks     []vdDisk `json:"disks"`
}

// vdDisks is `filedo vd disks [json]`: every disk, its partitions and its
// free extents, each usable or not with the reason. No elevation.
func vdDisks(args []string) error {
	asJSON := len(args) == 1 && strings.EqualFold(args[0], "json")
	if len(args) > 0 && !asJSON {
		return vdUsagef("vd disks takes no other word but json")
	}
	if vdPackaged() {
		if asJSON {
			b, _ := json.Marshal(vdDisksWire{Schema: vdDisksSchema, Version: vdDisksVersion, Reason: "store-build", Disks: []vdDisk{}})
			fmt.Println(string(b))
			return nil
		}
		fmt.Println(vdPartStoreSentence)
		return nil
	}
	disks, err := vdDiscoverDisks()
	if err != nil {
		return err
	}
	if asJSON {
		b, err := json.Marshal(vdDisksWire{Schema: vdDisksSchema, Version: vdDisksVersion, Available: true, Disks: disks})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}
	vdPrintDisks(os.Stdout, disks)
	return nil
}

// vdPartStoreSentence is section 4.7's sentence for the Store build.
const vdPartStoreSentence = "Partition disks are not available in the Microsoft Store version."
