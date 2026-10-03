package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filedo/vdisk"
)

// SP-0148 S2: discovery's model, judged on recorded layouts. The fixture is
// the shape of a real machine's disks (an NVMe system disk with a free extent
// between C: and the recovery partition, an MBR disk, an offline disk, a
// FileDO iSCSI disk), anonymised: every GUID here is made up.

const (
	gDisk2   = "11111111-2222-4333-8444-555555555555"
	gDisk0   = "21111111-2222-4333-8444-555555555555"
	gDisk7   = "31111111-2222-4333-8444-555555555555"
	gPartFD  = "A1111111-2222-4333-8444-555555555555"
	gPartFD2 = "A2111111-2222-4333-8444-555555555555"
	gPartFD3 = "A3111111-2222-4333-8444-555555555555"
	gPartFD4 = "A4111111-2222-4333-8444-555555555555"
	gBasic   = "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7"
	gRecov   = "DE94BBA4-06D1-4D40-A16A-BFD50179D6AC"
	gEFI     = "C12A7328-F81F-11D2-BA4B-00A0C93EC93B"
	gMSR     = "E3C9E316-0B5C-4DB8-817D-F92DF00215AE"
)

func part(n int, guid string, off, ln int64, typ string, letters ...string) vdDiskPart {
	p := vdDiskPart{Number: n, GUID: guid, Offset: off, Length: ln, Type: typ, Kind: vdPartKind(typ), Letters: letters}
	p.FileDO = p.Kind == "fileDO"
	return p
}

// recordedDisks is the machine of 2026-10-03, anonymised.
func recordedDisks() []vdDisk {
	const GiB = int64(1) << 30
	sys := vdDisk{Number: 2, GUID: gDisk2, Model: "NVMe system disk", Bus: "nvme", Size: 4000787030016, Style: "gpt",
		Logical: 512, Physical: 4096, Online: true, System: true, UsableStart: 17408, UsableLength: 4000787030016 - 17408 - 16896, MaxPartitions: 128,
		Partitions: []vdDiskPart{
			part(1, "B0000001-2222-4333-8444-555555555555", 1048576, 209715200, gEFI),
			part(2, "B0000002-2222-4333-8444-555555555555", 210763776, 16777216, gMSR),
			part(3, "B0000003-2222-4333-8444-555555555555", 227540992, 3564000000000-227540992, gBasic, "C:"),
			part(4, "B0000004-2222-4333-8444-555555555555", 3669349105664, 893386752, gRecov),
			part(5, "B0000005-2222-4333-8444-555555555555", 3670242492416, 307*GiB, gBasic, "P:"),
		}}
	mbr := vdDisk{Number: 1, Model: "SATA MBR disk", Bus: "sata", Size: 500 * GiB, Style: "mbr", Online: true,
		Partitions: []vdDiskPart{{Number: 1, Offset: 1 << 20, Length: 50 << 20, Type: "MBR:07", Kind: "mbr"}}}
	off := vdDisk{Number: 0, GUID: gDisk0, Model: "SATA disk", Bus: "sata", Size: 2000 * GiB, Style: "gpt", Online: false,
		UsableStart: 17408, UsableLength: 2000*GiB - 34816, MaxPartitions: 128,
		Partitions: []vdDiskPart{part(1, "C0000001-2222-4333-8444-555555555555", 1<<20, 1000*GiB, gBasic)}}
	iscsi := vdDisk{Number: 7, GUID: gDisk7, Model: "FileDO FDD Container", Vendor: "FileDO", Bus: "iscsi", Size: 20 * GiB, Style: "gpt", Online: true,
		UsableStart: 17408, UsableLength: 20*GiB - 34816, MaxPartitions: 128}
	return []vdDisk{off, mbr, sys, iscsi}
}

func TestVDPart_RecordedLayoutJudged(t *testing.T) {
	disks := recordedDisks()
	vdJudgeDisks(disks)
	want := map[int]string{0: vdWhyOffline, 1: vdWhyMBR, 2: "", 7: vdWhyISCSI}
	for _, d := range disks {
		if d.Reason != want[d.Number] || d.Usable != (want[d.Number] == "") {
			t.Errorf("disk %d: usable %v reason %q, want %q", d.Number, d.Usable, d.Reason, want[d.Number])
		}
	}
	sys := disks[2]
	if len(sys.Free) != 2 {
		t.Fatalf("the system disk has %d free extents, want 2 (between C: and recovery, at the end): %+v", len(sys.Free), sys.Free)
	}
	gap := sys.Free[0]
	if gap.Offset%vdMiB != 0 || gap.Length%vdMiB != 0 || gap.Offset < 3564000000000 || gap.Offset+gap.Length > 3669349105664 {
		t.Fatalf("the gap is not aligned inside C:..recovery: %+v", gap)
	}
	if !gap.Usable || gap.Before != "C:" || gap.After != "recovery" {
		t.Fatalf("the gap: %+v", gap)
	}
	// Unusable disks list their extents unusable, with the disk's reason.
	for _, e := range disks[0].Free {
		if e.Usable || e.Reason != vdWhyOffline {
			t.Fatalf("an offline disk's extent: %+v", e)
		}
	}
}

func TestVDPart_FreeExtentArithmetic(t *testing.T) {
	const M = vdMiB
	// Alignment up at the start and down at the end; a gap under 64 MiB is
	// not listed; a zero-length entry occupies nothing; overlapping entries.
	parts := []vdDiskPart{
		{Offset: 1 * M, Length: 100 * M},
		{Offset: 200*M + 4096, Length: 10 * M},   // leaves 101M..200M+4096: 99 MiB
		{Offset: 300 * M, Length: 0},             // zero-length: nothing
		{Offset: 250 * M, Length: 30*M + 512},    // 210M+4096 .. 250M is 39 MiB: too small
		{Offset: 270 * M, Length: 5 * M},         // overlaps the previous one
		{Offset: 400*M + 17, Length: 100*M - 17}, // 280M+512 .. 400M+17
	}
	got := vdFreeExtents(17408, 600*M, parts)
	want := []vdExtent{{Offset: 101 * M, Length: 99 * M}, {Offset: 281 * M, Length: 119 * M}, {Offset: 500 * M, Length: 100 * M}}
	if len(got) != len(want) {
		t.Fatalf("extents %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i].Offset != want[i].Offset || got[i].Length != want[i].Length {
			t.Fatalf("extent %d: %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestVDPart_RefusalTable(t *testing.T) {
	base := func() vdDisk {
		return vdDisk{Number: 9, GUID: "91111111-2222-4333-8444-555555555555", Bus: "sata", Style: "gpt", Online: true, UsableStart: 17408, UsableLength: 1 << 40, MaxPartitions: 128}
	}
	cases := []struct {
		name string
		mod  func(d *vdDisk)
		want string
	}{
		{"usable", func(d *vdDisk) {}, ""},
		{"mbr", func(d *vdDisk) { d.Style = "mbr" }, vdWhyMBR},
		{"raw", func(d *vdDisk) { d.Style = "raw" }, vdWhyRaw},
		{"offline", func(d *vdDisk) { d.Online = false }, vdWhyOffline},
		{"read-only", func(d *vdDisk) { d.ReadOnly = true }, vdWhyReadOnly},
		{"iscsi", func(d *vdDisk) { d.Bus = "iscsi" }, vdWhyISCSI},
		{"FileDO vendor on another bus", func(d *vdDisk) { d.Vendor = "FileDO" }, vdWhyISCSI},
		{"spaces", func(d *vdDisk) { d.Bus = "spaces" }, vdWhySpaces},
		{"usb", func(d *vdDisk) { d.Bus = "usb" }, vdWhyUSB},
		{"removable", func(d *vdDisk) { d.Removable = true }, vdWhyRemovable},
		{"sd", func(d *vdDisk) { d.Bus = "sd" }, vdWhyRemovable},
		{"virtual bus", func(d *vdDisk) { d.Bus = "virtual" }, vdWhyBus},
		{"dynamic", func(d *vdDisk) {
			d.Partitions = []vdDiskPart{part(1, "D1111111-2222-4333-8444-555555555555", 1<<20, 1<<20, "5808C8AA-7E8F-42E0-85D2-E1E90434CFB3")}
		}, vdWhyDynamic},
		{"spaces member", func(d *vdDisk) {
			d.Partitions = []vdDiskPart{part(1, "D2111111-2222-4333-8444-555555555555", 1<<20, 1<<20, "E75CAF8F-F680-4CEE-AFA3-B001E56EFC2D")}
		}, vdWhySpacesMember},
		{"no entry slot", func(d *vdDisk) {
			d.MaxPartitions = 1
			d.Partitions = []vdDiskPart{part(1, "D3111111-2222-4333-8444-555555555555", 1<<20, 1<<20, gBasic)}
		}, vdWhyNoSlot},
		{"nvme", func(d *vdDisk) { d.Bus = "nvme" }, ""},
		{"vhdx", func(d *vdDisk) { d.Bus = "file-backed-virtual" }, ""},
	}
	for _, c := range cases {
		d := base()
		c.mod(&d)
		ds := []vdDisk{d}
		vdJudgeDisks(ds)
		if ds[0].Reason != c.want {
			t.Errorf("%s: reason %q, want %q", c.name, ds[0].Reason, c.want)
		}
		if c.name == "vhdx" && ds[0].Warning == "" {
			t.Error("a VHDX disk carries no warning that its space lives in a file")
		}
	}
	// Two disks with one GPT GUID: both refused.
	a, b := base(), base()
	b.Number = 10
	ds := []vdDisk{a, b}
	vdJudgeDisks(ds)
	if ds[0].Reason != vdWhyDuplicate || ds[1].Reason != vdWhyDuplicate {
		t.Fatalf("cloned disks: %q %q", ds[0].Reason, ds[1].Reason)
	}
	for tok := range map[string]bool{vdWhyMBR: true, vdWhyDuplicate: true, vdWhyTooSmall: true} {
		if vdWhy(tok) == tok {
			t.Errorf("reason %q has no sentence", tok)
		}
	}
}

func TestVDPart_Locator(t *testing.T) {
	if got := vdLocator("{a1111111-2222-4333-8444-555555555555}"); got != "fdpart:{A1111111-2222-4333-8444-555555555555}" {
		t.Fatalf("locator %q", got)
	}
	if g := vdLocatorGUID("FDPART:{a1111111-2222-4333-8444-555555555555}"); g != gPartFD {
		t.Fatalf("guid %q", g)
	}
	for _, bad := range []string{"fdpart:", "fdpart:{xyz}", "C:\\x.fdd", "work"} {
		if vdLocatorGUID(bad) != "" {
			t.Errorf("%q gave a GUID", bad)
		}
	}
	if !isPartLocator("fdpart:{x}") || isPartLocator("x.fdd") {
		t.Fatal("isPartLocator")
	}
}

func TestVDPart_Resolution(t *testing.T) {
	disks := recordedDisks()
	fd := part(6, gPartFD, 3600000000000, 40<<30, vdPartType)
	disks[2].Partitions = append(disks[2].Partitions, fd)
	rec := vdPartRecord{Locator: vdLocator(gPartFD), DiskGUID: gDisk2, PartitionGUID: gPartFD, Offset: fd.Offset, Length: fd.Length}
	d, p, err := vdResolvePart(rec, disks)
	if err != nil || d.Number != 2 || p.Number != 6 {
		t.Fatalf("resolution: disk %d part %d %v", d.Number, p.Number, err)
	}
	// Changed offset, length, disk, type.
	for name, mod := range map[string]func(r *vdPartRecord){
		"offset": func(r *vdPartRecord) { r.Offset += vdMiB },
		"length": func(r *vdPartRecord) { r.Length -= vdMiB },
		"disk":   func(r *vdPartRecord) { r.DiskGUID = gDisk0 },
	} {
		r := rec
		mod(&r)
		if _, _, err := vdResolvePart(r, disks); !errors.Is(err, errVdPartChanged) {
			t.Errorf("%s changed: %v", name, err)
		}
	}
	retyped := recordedDisks()
	retyped[2].Partitions = append(retyped[2].Partitions, part(6, gPartFD, fd.Offset, fd.Length, gBasic))
	if _, _, err := vdResolvePart(rec, retyped); !errors.Is(err, errVdPartChanged) {
		t.Errorf("retyped: %v", err)
	}
	// Not connected: class 5 for a read, 7 for a mount.
	_, _, err = vdResolvePart(rec, recordedDisks())
	if !errors.Is(err, errVdPartNotConnected) {
		t.Fatalf("missing: %v", err)
	}
	if vdExitClass(vdPartClass(err, false)) != vdisk.ExitIO || vdExitClass(vdPartClass(err, true)) != vdExitTransport {
		t.Fatalf("classes: read %d, mount %d", vdExitClass(vdPartClass(err, false)), vdExitClass(vdPartClass(err, true)))
	}
	// A cloned disk carries the partition twice: ambiguous, never picked.
	twice := recordedDisks()
	twice[2].Partitions = append(twice[2].Partitions, fd)
	twice[1].Partitions = append(twice[1].Partitions, fd)
	if _, _, err := vdResolvePart(rec, twice); !errors.Is(err, errVdPartAmbiguous) {
		t.Fatalf("twice: %v", err)
	}
}

func TestVDPart_LayoutChecks(t *testing.T) {
	before := recordedDisks()[2].Partitions
	same := append([]vdDiskPart(nil), before...)
	same[0], same[1] = same[1], same[0]
	if !vdSameLayout(before, same) {
		t.Fatal("order must not matter")
	}
	moved := append([]vdDiskPart(nil), before...)
	moved[2].Length -= vdMiB
	if vdSameLayout(before, moved) {
		t.Fatal("a resized neighbour must count as a change")
	}
	n := part(6, gPartFD, 3600000000000, 40<<30, vdPartType)
	after := append(append([]vdDiskPart(nil), before...), n)
	if got, err := vdNewEntry(before, after, n.Offset, n.Length); err != nil || got.GUID != gPartFD {
		t.Fatalf("new entry: %v", err)
	}
	if _, err := vdNewEntry(before, after, n.Offset+vdMiB, n.Length); err == nil {
		t.Fatal("a new entry at another offset was accepted")
	}
	wrongType := append(append([]vdDiskPart(nil), before...), part(6, gPartFD, n.Offset, n.Length, gBasic))
	if _, err := vdNewEntry(before, wrongType, n.Offset, n.Length); err == nil {
		t.Fatal("a new entry of another type was accepted")
	}
	if _, err := vdNewEntry(before, append(after, part(7, "E1111111-2222-4333-8444-555555555555", 1, 1, gBasic)), n.Offset, n.Length); err == nil {
		t.Fatal("two new entries were accepted")
	}
	if _, err := vdNewEntry(before, append(moved, n), n.Offset, n.Length); err == nil {
		t.Fatal("a changed neighbour was accepted")
	}
}

func TestVDPart_PickExtent(t *testing.T) {
	disks := recordedDisks()
	vdJudgeDisks(disks)
	sys := disks[2]
	gap := sys.Free[0]
	if e, n, err := vdPickExtent(sys, -1, 0); err != nil || e.Offset != gap.Offset || n != gap.Length {
		t.Fatalf("size max: %+v %d %v", e, n, err)
	}
	if _, n, err := vdPickExtent(sys, gap.Offset, 10<<30+12345); err != nil || n != 10<<30 {
		t.Fatalf("a size rounds down to 1 MiB: %d %v", n, err)
	}
	if _, _, err := vdPickExtent(sys, gap.Offset, gap.Length+vdMiB); !errors.Is(err, vdisk.ErrUsage) {
		t.Fatalf("too large: %v", err)
	}
	if _, _, err := vdPickExtent(sys, 12345, 0); !errors.Is(err, vdisk.ErrUsage) {
		t.Fatalf("no extent there: %v", err)
	}
	if _, _, err := vdPickExtent(sys, -1, 10<<20); !errors.Is(err, vdisk.ErrUsage) {
		t.Fatalf("under 64 MiB: %v", err)
	}
	if _, _, err := vdPickExtent(disks[1], -1, 0); !errors.Is(err, vdisk.ErrUnsupported) {
		t.Fatalf("an MBR disk: %v", err)
	}
	// The first usable extent that fits: a request larger than the first gap
	// takes the next one.
	if e, _, err := vdPickExtent(sys, -1, sys.Free[1].Length); err != nil || (sys.Free[1].Length > gap.Length && e.Offset != sys.Free[1].Offset) {
		t.Fatalf("first fit: %+v %v", e, err)
	}
}

func TestVDPart_DisksWireAndText(t *testing.T) {
	disks := recordedDisks()
	vdJudgeDisks(disks)
	b, err := json.Marshal(vdDisksWire{Schema: vdDisksSchema, Version: vdDisksVersion, Available: true, Disks: disks})
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]interface{}
	json.Unmarshal(b, &back)
	if back["schema"] != "filedo.vd-disks" || back["version"].(float64) != 1 {
		t.Fatalf("header: %v %v", back["schema"], back["version"])
	}
	d0 := back["disks"].([]interface{})[2].(map[string]interface{})
	for _, k := range []string{"number", "guid", "model", "bus", "size", "style", "logical_sector", "physical_sector", "online", "read_only", "removable", "usable", "partitions", "free"} {
		if _, ok := d0[k]; !ok {
			t.Errorf("a disk row has no %q", k)
		}
	}
	f0 := d0["free"].([]interface{})[0].(map[string]interface{})
	for _, k := range []string{"offset", "length", "usable"} {
		if _, ok := f0[k]; !ok {
			t.Errorf("a free row has no %q", k)
		}
	}
	var out bytes.Buffer
	vdPrintDisks(&out, disks)
	text := out.String()
	for _, want := range []string{"(system disk)", "between C: and recovery", "usable", "filedo vd new part disk:{" + gDisk2 + "}", "MBR", vdWhy(vdWhyOffline)} {
		if !strings.Contains(text, want) {
			t.Errorf("vd disks text lacks %q:\n%s", want, text)
		}
	}
	// A FileDO partition nobody registered says how to adopt it.
	disks[2].Partitions = append(disks[2].Partitions, part(6, gPartFD, 3600000000000, 40<<30, vdPartType))
	out.Reset()
	vdPrintDisks(&out, disks)
	if !strings.Contains(out.String(), "filedo vd adopt fdpart:{"+gPartFD+"}") {
		t.Errorf("an unregistered FileDO partition does not name vd adopt:\n%s", out.String())
	}
}

// The frozen type GUID is the contract's, and never the research GUID.
func TestVDPart_TypeGUIDFrozen(t *testing.T) {
	if vdPartType != "6AFB315B-8976-4841-84EC-D30AFA89A33D" {
		t.Fatalf("the FileDO partition type GUID changed: %s (FDD-FORMAT section 3.1, a frozen anchor)", vdPartType)
	}
	if strings.HasPrefix(strings.ToLower(vdPartType), "5c0a7e3b") {
		t.Fatal("the research GUID is not the product's")
	}
	if vdPartKind(vdPartType) != "fileDO" || vdPartKind("{"+strings.ToLower(vdPartType)+"}") != "fileDO" {
		t.Fatal("the FileDO type is not recognised in every spelling")
	}
}

// The create step's bounds check, in arithmetic that cannot overflow.
func TestVDPart_InsideExtent(t *testing.T) {
	const M = vdMiB
	e := vdExtent{Offset: 100 * M, Length: 200 * M}
	cases := []struct {
		name         string
		offset, size int64
		want         bool
	}{
		{"whole extent", 100 * M, 200 * M, true},
		{"inside", 150 * M, 64 * M, true},
		{"ends at the end", 236 * M, 64 * M, true},
		{"one byte over", 236 * M, 64*M + 1, false},
		{"before the extent", 99 * M, 64 * M, false},
		{"after the extent", 300 * M, 64 * M, false},
		{"zero size", 150 * M, 0, false},
		{"negative size", 150 * M, -M, false},
		{"offset+size overflows", 150 * M, math.MaxInt64, false},
		{"offset near the top", math.MaxInt64 - M, 64 * M, false},
	}
	for _, c := range cases {
		if got := vdInsideExtent(e, c.offset, c.size); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if vdInsideExtent(vdExtent{Offset: math.MaxInt64 - M, Length: math.MaxInt64}, math.MaxInt64-M, M) != true {
		t.Error("an extent whose end overflows still holds a range at its start")
	}
	if vdInsideExtent(vdExtent{Offset: -M, Length: 200 * M}, 0, M) {
		t.Error("an extent at a negative offset was accepted")
	}
}

func TestVDPart_HeaderPositionsZero(t *testing.T) {
	const L = 64 << 10
	img := make([]byte, L)
	img[L/2] = 1 // data between the positions does not count
	if z, err := vdHeaderPositionsZero(bytes.NewReader(img), L); err != nil || !z {
		t.Fatalf("zeroed positions: %v %v", z, err)
	}
	for _, at := range []int{0, vdHeaderBytes - 1, L - vdHeaderBytes, L - 1} {
		b := append([]byte(nil), img...)
		b[at] = 0xFD
		if z, err := vdHeaderPositionsZero(bytes.NewReader(b), L); err != nil || z {
			t.Errorf("a byte at %d: %v %v", at, z, err)
		}
	}
	if _, err := vdHeaderPositionsZero(bytes.NewReader(img[:vdHeaderBytes]), vdHeaderBytes); !errors.Is(err, vdisk.ErrIO) {
		t.Errorf("a carrier too small for a header: %v", err)
	}
	if _, err := vdHeaderPositionsZero(bytes.NewReader(img[:L/2]), L); !errors.Is(err, vdisk.ErrIO) {
		t.Errorf("a short read: %v", err)
	}
}

// The ownership proof of destroy (FDD-BEHAVIOUR 6 item 10): the registered id,
// or, for unfinished work, zeros or the id the creation recorded.
func TestVDPart_ProveOwn(t *testing.T) {
	const id, other = "11111111-0000-4000-8000-000000000001", "22222222-0000-4000-8000-000000000002"
	loc := vdLocator(gPartFD)
	cases := []struct {
		name       string
		want       string
		unfinished bool
		zeroed     bool
		got        string
		why        error
		ok         bool
		class      error
	}{
		{"finished, its id", id, false, false, id, nil, true, nil},
		{"finished, id in other case", id, false, false, strings.ToUpper(id), nil, true, nil},
		{"finished, another id", id, false, false, other, nil, false, vdisk.ErrUnsupported},
		{"finished, zeros", id, false, true, "", vdisk.ErrDamaged, false, vdisk.ErrDamaged},
		{"finished, no id registered", "", false, false, id, nil, false, vdisk.ErrUnsupported},
		{"unfinished creation, zeros", "", true, true, "", nil, true, nil},
		{"unfinished creation, the recorded header", id, true, false, id, nil, true, nil},
		{"unfinished creation, another header", id, true, false, other, nil, false, vdisk.ErrUnsupported},
		{"unfinished creation, a header nobody recorded", "", true, false, id, nil, false, vdisk.ErrUnsupported},
		{"unfinished, neither zeros nor a header", id, true, false, "", vdisk.ErrDamaged, false, vdisk.ErrUnsupported},
		{"wipe begun, zeros", id, true, true, "", nil, true, nil},
		{"wipe stopped early, the header still there", id, true, false, id, nil, true, nil},
	}
	for _, c := range cases {
		err := vdProveOwn(loc, c.want, c.unfinished, c.zeroed, c.got, c.why)
		if (err == nil) != c.ok {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if c.class != nil && !errors.Is(err, c.class) {
			t.Errorf("%s: %v is not %v", c.name, err, c.class)
		}
	}
	if err := vdProveOwn(loc, "", true, false, id, nil); err == nil || !strings.Contains(err.Error(), "filedo vd adopt "+loc) {
		t.Errorf("an unrecorded header does not name the way out: %v", err)
	}
}

func TestVDPart_AdoptVerdict(t *testing.T) {
	if u, err := vdAdoptVerdict(nil, false); err != nil || u {
		t.Fatalf("a container: %v %v", u, err)
	}
	if u, err := vdAdoptVerdict(vdisk.ErrDamaged, true); err != nil || !u {
		t.Fatalf("zeroed positions: %v %v", u, err)
	}
	if _, err := vdAdoptVerdict(vdisk.ErrDamaged, false); !errors.Is(err, vdisk.ErrDamaged) {
		t.Fatalf("a non-zero, non-container header: %v", err)
	}
	if _, err := vdAdoptVerdict(vdisk.ErrIO, false); !errors.Is(err, vdisk.ErrIO) {
		t.Fatalf("a read failure: %v", err)
	}
	rec := vdPartRecord{PartitionGUID: gPartFD}
	creating := vdRegEntry{Name: "new1", Path: vdLocator(gPartFD), Part: &vdPartRecord{PartitionGUID: gPartFD, Pending: vdPendingCreate}}
	legacy := vdRegEntry{Name: "new2", Path: vdLocator(gPartFD), Part: &rec}
	wiping := vdRegEntry{Name: "old", Path: vdLocator(gPartFD), ContainerID: "x", Part: &vdPartRecord{PartitionGUID: gPartFD, Pending: vdPendingDestroy}}
	done := vdRegEntry{Name: "done", Path: vdLocator(gPartFD), ContainerID: "x", Part: &rec}
	if vdAdoptOver(creating) != nil || vdAdoptOver(legacy) != nil {
		t.Fatal("adopt over an unfinished creation was refused")
	}
	if err := vdAdoptOver(wiping); !errors.Is(err, vdisk.ErrUsage) || !strings.Contains(err.Error(), "filedo vd destroy old") {
		t.Fatalf("adopt over a destroy that did not finish: %v", err)
	}
	if err := vdAdoptOver(done); !errors.Is(err, vdisk.ErrUsage) || !strings.Contains(err.Error(), "registered already as done") {
		t.Fatalf("adopt over a finished entry: %v", err)
	}
}

// The unfinished-work flag survives the registry file, and an entry an
// earlier build wrote (no container id, no flag) reads as an unfinished
// creation.
func TestVDPart_PendingRoundTrip(t *testing.T) {
	r := vdRegistry{Version: 1, Containers: []vdRegEntry{
		{Name: "creating", Path: vdLocator(gPartFD), Carrier: "partition", Part: &vdPartRecord{PartitionGUID: gPartFD, Pending: vdPendingCreate, ContainerID: "rec-id"}},
		{Name: "wiping", Path: vdLocator(gPartFD2), Carrier: "partition", ContainerID: "reg-id", Part: &vdPartRecord{PartitionGUID: gPartFD2, Pending: vdPendingDestroy}},
		{Name: "legacy", Path: vdLocator(gPartFD3), Carrier: "partition", Part: &vdPartRecord{PartitionGUID: gPartFD3}},
		{Name: "done", Path: vdLocator(gPartFD4), Carrier: "partition", ContainerID: "done-id", Part: &vdPartRecord{PartitionGUID: gPartFD4, ContainerID: "done-id"}},
		{Name: "file", Path: `C:\x.fdd`, ContainerID: "file-id"},
	}}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"pending":"destroy"`)) || bytes.Count(b, []byte(`"pending"`)) != 2 {
		t.Fatalf("the flag is not written as pending, or written when empty: %s", b)
	}
	var back vdRegistry
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	want := map[string]struct{ pending, id string }{
		"creating": {vdPendingCreate, "rec-id"},
		"wiping":   {vdPendingDestroy, "reg-id"},
		"legacy":   {vdPendingCreate, ""},
		"done":     {"", "done-id"},
		"file":     {"", "file-id"},
	}
	for _, e := range back.Containers {
		w := want[e.Name]
		if e.partPending() != w.pending || e.partRecordedID() != w.id {
			t.Errorf("%s: pending %q id %q, want %q %q", e.Name, e.partPending(), e.partRecordedID(), w.pending, w.id)
		}
	}
	if i := back.findPart("{" + strings.ToLower(gPartFD2) + "}"); i < 0 || back.Containers[i].Name != "wiping" {
		t.Fatalf("findPart: %d", i)
	}
	if back.findPart("") >= 0 || back.findPart("E9999999-2222-4333-8444-555555555555") >= 0 {
		t.Fatal("findPart found what is not there")
	}
	for _, e := range back.Containers[:2] {
		if !strings.Contains(e.partUnfinishedText(), "filedo vd destroy "+e.Name) {
			t.Errorf("%s: %q does not name destroy", e.Name, e.partUnfinishedText())
		}
	}
}

func TestVDPart_MountReadOnly(t *testing.T) {
	cases := []struct {
		asked   bool
		profile string
		want    bool
	}{
		{false, "fast", false},
		{true, "fast", true},
		{false, "sealed", true},
		{false, "SEALED", true},
		{true, "sealed", true},
		{false, "", false},
	}
	for _, c := range cases {
		if got := vdPartMountRO(c.asked, c.profile); got != c.want {
			t.Errorf("asked %v profile %q: %v, want %v", c.asked, c.profile, got, c.want)
		}
	}
}

// A path that exists wins over a partition disk's name (FDD-BEHAVIOUR 9).
func TestVDPart_WordIsPath(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "work")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	if !vdPartWordIsPath(file, exists) {
		t.Fatal("an existing file is not taken as a path")
	}
	if vdPartWordIsPath(filepath.Join(dir, "other"), exists) {
		t.Fatal("a missing file is taken as a path")
	}
	always := func(string) bool { return true }
	if vdPartWordIsPath(vdLocator(gPartFD), always) || vdPartWordIsPath("", always) {
		t.Fatal("a locator or an empty word is taken as a path")
	}
}
