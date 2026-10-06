//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"filedo/fdsec"
	"filedo/vdisk"
)

// The command side of partition disks (SP-0148 sections 4 and 5): vd new part,
// vd image, vd adopt, the partition form of destroy, and every existing verb on
// a partition disk's name or locator. The work happens in this unelevated
// process; elevation is only the brokered open of the partition (vdisk_part_windows.go).

// errVdPartPackaged is D8: the Store build lists, creates and opens no
// partition disk.
var errVdPartPackaged = fmt.Errorf("%w: %s", vdisk.ErrUnsupported, vdPartStoreSentence)

// The refusals of section 5.4, by name.
var (
	errVdPartFixed   = fmt.Errorf("%w: a partition disk has a fixed size; compact and grow apply to file disks", vdisk.ErrUnsupported)
	errVdPartShare   = fmt.Errorf("%w: sharing a partition disk through FMS needs a newer FMS; share a file disk", vdisk.ErrUnsupported)
	errVdPartNoScan  = vdUsagef("noscan applies to file disks; a partition disk is not scanned as a file")
	errVdPartAddFile = vdUsagef("a partition disk is registered by filedo vd new part or filedo vd adopt, not by vd add")
)

// vdPartEntryFor finds the registered partition disk a word names: its name,
// its locator, or its partition GUID.
func vdPartEntryFor(word string) (vdRegEntry, bool) {
	r, err := vdLoadRegistry()
	if err != nil {
		return vdRegEntry{}, false
	}
	g := vdLocatorGUID(word)
	for _, e := range r.Containers {
		if !e.isPart() {
			continue
		}
		if strings.EqualFold(e.Name, word) || strings.EqualFold(e.Path, word) || (g != "" && g == vdNormGUID(e.Part.PartitionGUID)) {
			return e, true
		}
	}
	return vdRegEntry{}, false
}

// vdPathExists reports whether a file or folder of that name exists.
func vdPathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// vdPartRecordOf is an entry's identity with its container id.
func vdPartRecordOf(e vdRegEntry) vdPartRecord {
	rec := *e.Part
	rec.Locator = e.Path
	rec.ContainerID = e.ContainerID
	return rec
}

// vdPartRoute sends partition disk work to its own forms (vdisk_cmd.go runVd).
// done is false for everything that is not a partition disk's, and for the
// verbs whose file form handles a locator already (unmount, save, forget,
// auto, list, status).
func vdPartRoute(verb string, rest []string, batch bool) (bool, error) {
	partNew := verb == "new" && len(rest) > 0 && strings.EqualFold(rest[0], "part")
	special := partNew || verb == "image" || verb == "adopt"
	var e vdRegEntry
	isPart := false
	if len(rest) > 0 && !special {
		switch verb {
		case "unmount", "save", "forget", "auto", "guard", "list", "status", "stop", "disks", "new", "register", "unregister":
			return false, nil
		}
		if vdPartWordIsPath(rest[0], vdPathExists) {
			return false, nil // the file of that name, as vdResolve takes it
		}
		e, isPart = vdPartEntryFor(rest[0])
		if !isPart && isPartLocator(rest[0]) {
			if vdPackaged() {
				return true, errVdPartPackaged
			}
			return true, vdUsagef("%s is not registered on this machine; register it first with: filedo vd adopt %s", rest[0], rest[0])
		}
	}
	if !special && !isPart {
		return false, nil
	}
	if vdPackaged() {
		return true, errVdPartPackaged
	}
	switch {
	case partNew:
		return true, vdNewPart(rest[1:], batch)
	case verb == "image":
		return true, vdImage(rest, batch)
	case verb == "adopt":
		return true, vdAdopt(rest, batch)
	}
	args := rest[1:]
	switch verb {
	case "mount", "info", "verify", "export", "pass", "seal", "clone", "format":
		// Unfinished work holds no container to use; destroy ends it.
		if e.partPending() != "" {
			return true, fmt.Errorf("%w: %s", vdisk.ErrDamaged, e.partUnfinishedText())
		}
	}
	switch verb {
	case "mount":
		return true, vdPartMount(e, args, batch)
	case "info":
		return true, vdPartInfo(e, args, batch)
	case "verify":
		return true, vdPartVerify(e, args, batch)
	case "export":
		return true, vdPartExport(e, args, batch)
	case "pass":
		return true, vdPartPass(e, args, batch)
	case "seal":
		return true, vdPartCopy(e, args, true, batch)
	case "clone":
		return true, vdPartCopy(e, args, false, batch)
	case "destroy":
		return true, vdPartDestroy(e, args, batch)
	case "format":
		return true, vdPartFormat(e, args, batch)
	case "compact", "grow":
		return true, errVdPartFixed
	case "share", "autostart", "open", "close":
		return true, errVdPartShare
	case "add":
		return true, errVdPartAddFile
	}
	return false, nil
}

// ---------------------------------------------------------------- opening

// vdPartOpenRead opens a registered partition disk's container read-only
// through a brokered handle (one consent prompt), with the identity checks of
// section 6.1: the partition, then the container id.
func vdPartOpenRead(e vdRegEntry, cred fdsec.Credential, batch bool) (*vdisk.Container, error) {
	return vdPartOpenMode(e, cred, vdisk.OpenRead, batch)
}

func vdPartOpenMode(e vdRegEntry, cred fdsec.Credential, mode vdisk.OpenMode, batch bool) (*vdisk.Container, error) {
	rec := vdPartRecordOf(e)
	c, err := vdPartCarrier(rec, mode != vdisk.OpenRead, batch)
	if err != nil {
		return nil, err
	}
	info, err := vdisk.InspectOn(c, e.Path)
	if err != nil {
		c.Close()
		return nil, err
	}
	if err := vdPartCheckID(e, info); err != nil {
		c.Close()
		return nil, err
	}
	cont, err := vdisk.OpenOn(vdContext(), c, e.Path, cred, mode)
	if err != nil {
		c.Close()
		return nil, err
	}
	return cont, nil
}

// vdPartCheckID is the final word of section 6.1: the header's container id
// is the registered one.
func vdPartCheckID(e vdRegEntry, info vdisk.Info) error {
	if e.ContainerID != "" && !strings.EqualFold(info.ContainerID, e.ContainerID) {
		return fmt.Errorf("%w: the partition of %s holds container %s, not the registered %s; nothing was read or written", errVdPartChanged, e.Name, info.ContainerID, e.ContainerID)
	}
	return nil
}

// vdPartInspect reads a registered partition disk's header (one prompt).
func vdPartInspect(e vdRegEntry, batch bool) (vdisk.Info, error) {
	c, err := vdPartCarrier(vdPartRecordOf(e), false, batch)
	if err != nil {
		return vdisk.Info{}, err
	}
	defer c.Close()
	info, err := vdisk.InspectOn(c, e.Path)
	if err != nil {
		return info, err
	}
	return info, vdPartCheckID(e, info)
}

// vdPartRefuseMounted is class 8 while the disk is mounted.
func vdPartRefuseMounted(e vdRegEntry, verb string) error {
	if m, ok := vdFindMount(e.ContainerID); ok {
		return errBusy(fmt.Sprintf("%s is mounted at %s; %s needs it unmounted first (filedo vd unmount %s), and no option skips that", e.Name, vdRowPlace(m), verb, e.Name))
	}
	return nil
}

// vdPartAbout is how a partition disk is named in a message.
func vdPartAbout(e vdRegEntry) string {
	model := e.Part.DiskModel
	if model == "" {
		model = "disk {" + e.Part.DiskGUID + "}"
	}
	return fmt.Sprintf("%s (partition disk on %s, %s)", e.Name, model, vdHumanSize(e.Part.Length))
}

// ---------------------------------------------------------------- vd new part

// vdNewPart: filedo vd new part <disk> size <N|max> [at <offset>]
// [fast|plain|vault|ram] [as <name>] [label <text>] [password] [force].
func vdNewPart(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("vd new part needs a disk: filedo vd new part <disk> size <N|max> [at <offset>] [fast|plain|vault|ram] [as <name>] [label <text>]")
	}
	diskWord := args[0]
	profile := vdisk.ProfileFast
	var size, at int64 = 0, -1
	sizeGiven, force := false, false
	name, label := "", ""
	var cred credArg
	for i := 1; i < len(args); i++ {
		if a, ok := credentialToken(args[i]); ok {
			if cred.given() {
				return vdUsagef("one credential per container: give p:, pf:, pe: or k: once")
			}
			cred = a
			continue
		}
		switch w := strings.ToLower(args[i]); w {
		case "plain":
			profile = vdisk.ProfilePlain
		case "fast":
			profile = vdisk.ProfileFast
		case "ram":
			profile = vdisk.ProfileRAM
		case "vault":
			profile = vdisk.ProfileVault
		case "sealed":
			return fmt.Errorf("%w: a sealed or cloned copy is written to a new file", vdisk.ErrUnsupported)
		case "force", "-y":
			force = true
		case "size", "at", "as", "label":
			if i+1 >= len(args) {
				return vdUsagef("%s needs a value", w)
			}
			v := args[i+1]
			i++
			switch w {
			case "size":
				sizeGiven = true
				if strings.EqualFold(v, "max") {
					size = 0
					continue
				}
				n, err := vdParseSize(v, "vd new part")
				if err != nil {
					return err
				}
				size = n
			case "at":
				n, err := strconv.ParseInt(v, 10, 64)
				if err != nil || n < 0 {
					return vdUsagef("at takes the free space's start in bytes, as filedo vd disks prints it")
				}
				at = n
			case "as":
				name = v
			case "label":
				label = v
			}
		default:
			// Never quoted back: the word may be a password without p:.
			return vdUsagef("unknown word %d for vd new part: want size <N|max>, at <offset>, fast, plain, vault, ram, as <name>, label <text>, force, or a credential as p:, pf:, pe: or k:", i+2)
		}
	}
	if !sizeGiven {
		return vdUsagef("vd new part needs a size: size <N> (like 40G) or size max")
	}
	if label != "" {
		if _, err := vdValidLabel(label); err != nil {
			return err
		}
	}
	disks, err := vdDiscoverDisks()
	if err != nil {
		return err
	}
	d, err := vdDiskByWord(disks, diskWord)
	if err != nil {
		return err
	}
	ext, psize, err := vdPickExtent(d, at, size)
	if err != nil {
		return err
	}
	if name == "" {
		name = vdPartDefaultName(label, d.Number, psize)
	}
	if !vdNameSpelling.MatchString(name) || driveRootSpelling.MatchString(name) {
		return vdUsagef("%q is not a usable name: letters, digits, - and _, up to 40, starting with a letter or digit (and not a drive letter); choose one with: as <name>", name)
	}
	if reg, err := vdLoadRegistry(); err != nil {
		return err
	} else if i := reg.find(name); i >= 0 {
		return vdUsagef("the name %s is taken by %s; choose another with: as <name>", name, reg.Containers[i].Path)
	}
	// The credential before anything is changed: a vault asks twice.
	var key fdsec.Credential
	if profile == vdisk.ProfileVault {
		fmt.Println(vdVaultWarning)
	}
	if profile == vdisk.ProfileVault || cred.given() {
		c, err := vdResolveCredential(cred, true)
		if err != nil {
			return err
		}
		key = c
		defer clear(key)
	}
	if profile == vdisk.ProfileVault && len(key) == 0 {
		return vdisk.ErrVaultNeedsCredential
	}
	// The confirmation (section 4.2): everything the person needs to know the
	// right disk is acted on. Nothing to type - no data is destroyed.
	model := d.Model
	if model == "" {
		model = "(no model)"
	}
	fmt.Printf("Disk %d: %s, %s, %s, GPT disk id {%s}%s.\n", d.Number, model, strings.ToUpper(d.Bus), vdHumanSize(d.Size), d.GUID, map[bool]string{true: ", the system disk", false: ""}[d.System])
	where := ""
	switch {
	case ext.Before != "" && ext.After != "":
		where = fmt.Sprintf(", between %s and %s", ext.Before, ext.After)
	case ext.Before != "":
		where = ", after " + ext.Before
	case ext.After != "":
		where = ", before " + ext.After
	}
	fmt.Printf("Free space: %s from byte %d to %d%s.\n", vdHumanSize(ext.Length), ext.Offset, ext.Offset+ext.Length, where)
	fmt.Printf("New partition disk %s: %s at byte %d, profile %s.\n", name, vdHumanSize(psize), ext.Offset, profile)
	fmt.Println("Nothing on this disk outside the free space is changed.")
	for _, l := range vdPartFacts(profile) {
		fmt.Println(l)
	}
	if d.Warning != "" {
		fmt.Println("Note: " + d.Warning + ".")
	}
	if batch && !force {
		return vdUsagef("vd new part changes a disk's partition table, and a batch never answers the question; a batch line says force")
	}
	if !force && !vdConfirm("Create it?", batch) {
		return vdUsagef("nothing was created: the question was not answered yes")
	}
	rec := vdPartRecord{DiskGUID: d.GUID, DiskModel: d.Model, Bus: d.Bus}
	req := vdRequest{Part: &vdPartRequest{Op: "create", DiskGUID: d.GUID, Offset: ext.Offset, Size: psize, Layout: d.Partitions, BrokerPID: os.Getpid()}}
	if !windows.GetCurrentProcessToken().IsElevated() {
		if batch {
			return errTransport("creating a partition needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
		}
		fmt.Println("Creating the partition needs administrator consent; Windows will ask for it now.")
	}
	res, err := vdRunElevated("_part", req, batch, nil)
	if err != nil {
		return err
	}
	rec.PartitionGUID, rec.Offset, rec.Length = res.PartGUID, res.PartOffset, res.PartLength
	rec.Locator = vdLocator(res.PartGUID)
	h := windows.Handle(res.Handle)
	if err := vdCheckPartHandle(h, rec); err != nil {
		windows.CloseHandle(h)
		return err
	}
	// Registered before the container is written, as unfinished: a stop from
	// here on leaves a partition FileDO can still name and delete.
	pending := rec
	pending.Pending = vdPendingCreate
	entry := vdRegEntry{Name: name, Path: rec.Locator, Carrier: "partition", Part: &pending, Profile: profile.String(), Added: time.Now()}
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		if i := r.find(name); i >= 0 {
			return vdUsagef("the name %s was taken meanwhile by %s", name, r.Containers[i].Path)
		}
		r.Containers = append(r.Containers, entry)
		return nil
	}); err != nil {
		windows.CloseHandle(h)
		return fmt.Errorf("the partition %s was made, but it could not be registered (filedo vd adopt %s registers it as unfinished, and filedo vd destroy then deletes it): %w", rec.Locator, rec.Locator, err)
	}
	var verifyH windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &verifyH, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		windows.CloseHandle(h)
		return err
	}
	carrier, err := vdisk.NewDeviceCarrier(h)
	if err != nil {
		windows.CloseHandle(verifyH)
		return err
	}
	vdWriterStamp()
	line := vdPercent("Writing the container")
	o := vdisk.CreateOptions{Path: rec.Locator, Profile: profile, FriendlyName: label, Credential: key, Keyfile: cred.keyfile(), Progress: line}
	c, err := vdisk.CreateOn(vdContext(), carrier, o)
	line.end()
	if err != nil {
		carrier.Close()
		windows.CloseHandle(verifyH)
		return fmt.Errorf("%w - the partition %s stays, empty; remove it with: filedo vd destroy %s", err, rec.Locator, name)
	}
	info := c.Info()
	// The header is written: its container id is recorded at once, so destroy
	// can prove the partition is FileDO's own whatever happens next.
	stays := fmt.Sprintf("the partition %s stays, unfinished; remove it with: filedo vd destroy %s", rec.Locator, name)
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		if i := r.findPart(rec.PartitionGUID); i >= 0 {
			r.Containers[i].Part.ContainerID = info.ContainerID
			return nil
		}
		return fmt.Errorf("the registration of %s is gone", name)
	}); err != nil {
		c.Close()
		windows.CloseHandle(verifyH)
		return fmt.Errorf("the container %s was written, but its id could not be recorded (register it with: filedo vd adopt %s): %w", info.ContainerID, rec.Locator, err)
	}
	if err := c.Close(); err != nil {
		windows.CloseHandle(verifyH)
		return fmt.Errorf("%w - %s", err, stays)
	}
	// Re-open through a fresh carrier and verify before it counts as made.
	vc, err := vdisk.NewDeviceCarrier(verifyH)
	if err != nil {
		return fmt.Errorf("%w - %s", err, stays)
	}
	check, err := vdisk.OpenOn(vdContext(), vc, rec.Locator, key, vdisk.OpenRead)
	if err != nil {
		vc.Close()
		return fmt.Errorf("the new container does not open: %w - %s", err, stays)
	}
	rep, verr := check.Verify(vdContext(), nil)
	check.Close()
	if verr != nil || len(rep.Problems) != 0 {
		return fmt.Errorf("%w: the new container does not verify (%v %v) - %s", vdisk.ErrDamaged, verr, rep.Problems, stays)
	}
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		if i := r.findPart(rec.PartitionGUID); i >= 0 {
			r.Containers[i].ContainerID = info.ContainerID
			r.Containers[i].LogicalSize = info.LogicalSize
			r.Containers[i].Protection = info.Protection()
			r.Containers[i].Part.ContainerID = info.ContainerID
			r.Containers[i].Part.Pending = ""
		}
		return nil
	}); err != nil {
		return fmt.Errorf("%w - %s", err, stays)
	}
	fmt.Printf("Created %s: a partition disk, %s, %s volume; %s.\n", name, info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info))
	fmt.Printf("Locator: %s\n", rec.Locator)
	for _, l := range vdLimits(info, cred.given() && len(key) == 0) {
		fmt.Println(l)
	}
	fmt.Printf("Mount it with: filedo vd mount %s  (the first mount formats the volume NTFS)\n", name)
	vdLogf("new part %s: %s on disk {%s} at %d, %d bytes, container %s", name, rec.Locator, rec.DiskGUID, rec.Offset, rec.Length, info.ContainerID)
	return nil
}

// vdPartFacts are the statements of section 4.7 that creation prints.
func vdPartFacts(p vdisk.Profile) []string {
	out := []string{
		"A partition disk needs administrator consent to mount and for every read (info, verify, export); filedo vd image copies it into an ordinary .fdd file, which needs none.",
		"It is not faster than a file disk.",
	}
	switch p {
	case vdisk.ProfilePlain, vdisk.ProfileVault:
		out = append(out, "plain and vault leave whatever the free space held before in the clusters they have not written yet; fast overwrites the whole partition now.")
	default:
		out = append(out, p.String()+" writes the whole partition now, overwriting whatever the free space held before.")
	}
	out = append(out, "Disk Management shows the partition and can delete it; filedo vd destroy is the safe way.")
	return out
}

// vdPartDefaultName is the label, else "Partition-disk-<n>-<size>" in the
// registry's spelling.
func vdPartDefaultName(label string, disk int, size int64) string {
	if label != "" {
		var b strings.Builder
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
				b.WriteRune(r)
			case r == ' ':
				b.WriteRune('-')
			}
		}
		if s := strings.Trim(b.String(), "-_"); s != "" && len(s) <= 40 {
			return s
		}
	}
	return fmt.Sprintf("Partition-disk-%d-%s", disk, strings.ReplaceAll(vdHumanSize(size), " ", ""))
}

// vdValidLabel accepts a volume label the format step can use.
func vdValidLabel(label string) (string, error) {
	if len(label) > 64 {
		return "", vdUsagef("the label is longer than 64 bytes")
	}
	return label, nil
}

// vdDiskByWord resolves disk:{GUID} or the number vd disks printed to one
// disk, once; the GUID is what is acted on from here on.
func vdDiskByWord(disks []vdDisk, word string) (vdDisk, error) {
	w := strings.TrimSpace(word)
	if strings.HasPrefix(strings.ToLower(w), "disk:") {
		g := vdNormGUID(w[5:])
		if g == "" {
			return vdDisk{}, vdUsagef("%s is not a disk id; use disk:{GUID} or the number filedo vd disks prints", word)
		}
		d, err := vdFindDisk(disks, g)
		if err != nil {
			return vdDisk{}, vdUsagef("%v", err)
		}
		return d, nil
	}
	n, err := strconv.Atoi(w)
	if err != nil {
		return vdDisk{}, vdUsagef("the disk is disk:{GUID} or the number filedo vd disks prints")
	}
	for _, d := range disks {
		if d.Number == n {
			if d.GUID == "" {
				return d, fmt.Errorf("%w: disk %d: %s", vdisk.ErrUnsupported, n, vdWhy(map[bool]string{true: vdWhyMBR, false: vdWhyRaw}[d.Style == "mbr"]))
			}
			fmt.Printf("Disk %d is disk:{%s}; that id, not the number, is acted on.\n", n, d.GUID)
			return d, nil
		}
	}
	return vdDisk{}, vdUsagef("there is no disk %d (see: filedo vd disks)", n)
}

// ---------------------------------------------------------------- vd adopt

// vdAdopt: filedo vd adopt <locator> [as <name>] - register a FileDO partition
// found on a disk (section 4.6), after reading its header (one prompt).
func vdAdopt(args []string, batch bool) error {
	if len(args) < 1 {
		return vdUsagef("adopt needs a locator: filedo vd adopt fdpart:{GUID} [as <name>] (filedo vd disks prints it)")
	}
	g := vdLocatorGUID(args[0])
	if g == "" {
		return vdUsagef("adopt takes a partition locator fdpart:{GUID}, as filedo vd disks prints it")
	}
	name := ""
	switch {
	case len(args) == 3 && strings.EqualFold(args[1], "as"):
		name = args[2]
	case len(args) != 1:
		return vdUsagef("adopt takes a locator and an optional name: filedo vd adopt fdpart:{GUID} [as <name>]")
	}
	// An unfinished creation's entry is registered again from the header
	// read now (its name kept unless another is given); any other is final.
	var prior *vdRegEntry
	if e, ok := vdPartEntryFor(args[0]); ok {
		if err := vdAdoptOver(e); err != nil {
			return err
		}
		prior = &e
		if name == "" {
			name = e.Name
		}
	}
	disks, err := vdDiscoverDisks()
	if err != nil {
		return err
	}
	var hit []struct {
		d vdDisk
		p vdDiskPart
	}
	for _, d := range disks {
		for _, p := range d.Partitions {
			if vdNormGUID(p.GUID) == g {
				hit = append(hit, struct {
					d vdDisk
					p vdDiskPart
				}{d, p})
			}
		}
	}
	switch {
	case len(hit) == 0:
		return fmt.Errorf("%w: no disk on this machine has the partition %s", vdisk.ErrIO, vdLocator(g))
	case len(hit) > 1:
		return fmt.Errorf("%w: %d disks have the partition %s (a cloned disk?)", vdisk.ErrIO, len(hit), vdLocator(g))
	case !hit[0].p.FileDO:
		return fmt.Errorf("%w: the partition %s is not of the FileDO type; FileDO adopts only its own partitions", vdisk.ErrUnsupported, vdLocator(g))
	}
	d, p := hit[0].d, hit[0].p
	rec := vdPartRecord{Locator: vdLocator(g), DiskGUID: d.GUID, PartitionGUID: g, Offset: p.Offset, Length: p.Length, DiskModel: d.Model, Bus: d.Bus}
	c, err := vdPartCarrier(rec, false, batch)
	if err != nil {
		return err
	}
	info, ierr := vdisk.InspectOn(c, rec.Locator)
	zeroed := false
	if ierr != nil {
		if L, err := c.Size(); err == nil {
			zeroed, _ = vdHeaderPositionsZero(c, L)
		}
	}
	c.Close()
	unfinished, err := vdAdoptVerdict(ierr, zeroed)
	if err != nil {
		return err
	}
	if unfinished && prior != nil {
		return vdUsagef("%s is registered already as %s, and its partition still holds no container; %s", rec.Locator, prior.Name, prior.partUnfinishedText())
	}
	if name == "" {
		name = vdPartDefaultName(info.FriendlyName, d.Number, p.Length)
	}
	if !vdNameSpelling.MatchString(name) || driveRootSpelling.MatchString(name) {
		return vdUsagef("%q is not a usable name: letters, digits, - and _, up to 40, starting with a letter or digit; choose one with: as <name>", name)
	}
	entry := vdRegEntry{Name: name, Path: rec.Locator, Added: time.Now(), Carrier: "partition", Part: &rec}
	if unfinished {
		// Both header positions zero: a creation that never finished. It is
		// registered so destroy can delete it, and mounts nothing.
		rec.Pending = vdPendingCreate
	} else {
		rec.ContainerID = info.ContainerID
		entry.ContainerID, entry.Profile, entry.LogicalSize, entry.Protection = info.ContainerID, info.Profile.String(), info.LogicalSize, info.Protection()
	}
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		if prior != nil {
			// Replaced only if it is still the unfinished entry read above.
			i := r.findPart(g)
			if i < 0 || r.Containers[i].partPending() != vdPendingCreate {
				return vdUsagef("the registration of %s changed meanwhile; nothing was registered", rec.Locator)
			}
			r.Containers = append(r.Containers[:i], r.Containers[i+1:]...)
		} else if i := r.findPart(g); i >= 0 {
			return vdUsagef("%s was registered meanwhile as %s", rec.Locator, r.Containers[i].Name)
		}
		if i := r.find(name); i >= 0 {
			return vdUsagef("the name %s is taken by %s; choose another with: as <name>", name, r.Containers[i].Path)
		}
		r.Containers = append(r.Containers, entry)
		return nil
	}); err != nil {
		return err
	}
	if unfinished {
		fmt.Printf("Adopted %s as %s, unfinished: both header positions of the partition read as zeros, so it holds no container - a creation that never finished. It cannot be mounted; delete it with: filedo vd destroy %s\n", rec.Locator, name, name)
		fmt.Printf("Disk: %s {%s}, the partition is %s at byte %d.\n", d.Model, d.GUID, vdHumanSize(p.Length), p.Offset)
		return nil
	}
	fmt.Printf("Adopted %s as %s: container %s, %s, %s volume; %s.\n", rec.Locator, name, info.ContainerID, info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info))
	fmt.Printf("Disk: %s {%s}, the partition is %s at byte %d.\n", d.Model, d.GUID, vdHumanSize(p.Length), p.Offset)
	return nil
}

// ---------------------------------------------------------------- vd image

// vdImage: filedo vd image <name> to <new.fdd> [force] - the partition copied
// into an ordinary container file (section 4.4).
func vdImage(args []string, batch bool) error {
	if len(args) < 3 || !strings.EqualFold(args[1], "to") {
		return vdUsagef("image needs a partition disk and a new file: filedo vd image <name> to <new.fdd>")
	}
	for _, w := range args[3:] {
		if !strings.EqualFold(w, "force") && !strings.EqualFold(w, "-y") {
			return vdUsagef("image takes a partition disk, to <new.fdd> and force")
		}
	}
	e, ok := vdPartEntryFor(args[0])
	switch {
	case ok && vdPartWordIsPath(args[0], vdPathExists):
		return vdUsagef("%s is a file in this folder, and a path that exists wins over a name; name the partition disk by its locator %s", args[0], e.Path)
	case !ok && isFddPath(args[0]):
		return vdUsagef("%s is a file disk already; image copies a partition disk into a file (clone copies a file)", args[0])
	case !ok:
		return vdUsagef("%s is not a registered partition disk (see: filedo vd list)", args[0])
	case e.partPending() != "":
		return fmt.Errorf("%w: %s", vdisk.ErrDamaged, e.partUnfinishedText())
	}
	dst := args[2]
	if err := vdRefuseCredentialSlot("new file slot", dst); err != nil {
		return err
	}
	if !isFddPath(dst) {
		return vdUsagef("the image is a container file, whose name ends in .fdd")
	}
	if abs, err := absPath(dst); err == nil {
		dst = abs
	}
	if _, err := os.Stat(dst); err == nil {
		return vdUsagef("%s exists; an image is never written over an existing file", dst)
	}
	if err := vdPartRefuseMounted(e, "image"); err != nil {
		return err
	}
	c, err := vdPartCarrier(vdPartRecordOf(e), false, batch)
	if err != nil {
		return err
	}
	defer c.Close()
	info, err := vdisk.InspectOn(c, e.Path)
	if err != nil {
		return err
	}
	if err := vdPartCheckID(e, info); err != nil {
		return err
	}
	line := vdPercent("Imaging")
	err = vdisk.ImageToFile(vdContext(), c, dst, line)
	line.end()
	if err != nil {
		return err
	}
	fmt.Printf("Imaged %s into %s: an ordinary container file of %s (the same container, id %s). The partition is unchanged.\n", vdPartAbout(e), dst, vdHumanSize(e.Part.Length), info.ContainerID)
	fmt.Println("Every read (info, verify, export) of the file needs no administrator consent. The file and the partition are the same container: mount one at a time.")
	vdLogf("image %s to %s", e.Path, dst)
	return nil
}

// ---------------------------------------------------------------- read-path verbs

func vdPartInfo(e vdRegEntry, args []string, batch bool) error {
	for _, w := range args {
		if isCredentialToken(w) {
			return vdUsagef("info takes no credential: the header it reads opens without one")
		}
		return vdUsagef("info takes one container")
	}
	// A mounted partition disk answers from what is known, with no prompt
	// (section 8.4): its server holds the partition alone.
	if m, ok := vdFindMount(e.ContainerID); ok {
		fmt.Printf("Container:     %s\n", e.Path)
		fmt.Printf("Carrier:       %s\n", vdPartAbout(e))
		fmt.Printf("Profile:       %s\n", e.Profile)
		fmt.Printf("Protection:    %s\n", e.Protection)
		fmt.Printf("Volume size:   %s\n", vdSize(e.LogicalSize))
		fmt.Printf("Container id:  %s\n", e.ContainerID)
		fmt.Printf("Mounted now:   %s (its block server holds the partition; unmount it to read the header)\n", vdRowPlace(m))
		return nil
	}
	info, err := vdPartInspect(e, batch)
	if err != nil {
		return err
	}
	vdInfoPrint(e.Path, info)
	fmt.Printf("Carrier:       %s, locator %s\n", vdPartAbout(e), e.Path)
	EmitFindingEvent("info", "Container inspected", map[string]interface{}{"vdInfo": map[string]interface{}{"containerId": info.ContainerID, "obfuscated": info.Obfuscated, "profile": info.Profile.String(), "versionMajor": info.VersionMajor, "versionMinor": info.VersionMinor, "carrier": "partition"}})
	return nil
}

// vdPartKeyFor is the credential a partition disk needs: none when obfuscated
// (by its header, refreshed into the registry), else the resolved one.
func vdPartKeyFor(e vdRegEntry, a credArg) (fdsec.Credential, error) {
	info := vdisk.Info{Obfuscated: e.Protection != "encrypted"}
	return vdCredentialFor(info, a)
}

func vdPartVerify(e vdRegEntry, args []string, batch bool) error {
	cred, err := vdCredOnly("verify", args)
	if err != nil {
		return err
	}
	defer vdForgetEnv(cred)
	if err := vdPartRefuseMounted(e, "verify"); err != nil {
		return err
	}
	key, err := vdPartKeyFor(e, cred)
	if err != nil {
		return err
	}
	defer clear(key)
	c, err := vdPartOpenRead(e, key, batch)
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	defer c.Close()
	return vdVerifyOpened(e.Path, c.Info(), c, true)
}

func vdPartExport(e vdRegEntry, args []string, batch bool) error {
	o, err := vdParseExport(append([]string{e.Path}, args...))
	defer vdForgetEnv(o.cred)
	if err != nil {
		return err
	}
	if o.form < 0 {
		return fmt.Errorf("%w: %s", vdisk.ErrUnsupported, vdExportTreeRefusal)
	}
	if _, err := os.Stat(o.dest); err == nil {
		return vdUsagef("%s exists; an export never writes over a file", o.dest)
	}
	if err := vdPartRefuseMounted(e, "export"); err != nil {
		return err
	}
	key, err := vdPartKeyFor(e, o.cred)
	if err != nil {
		return err
	}
	defer clear(key)
	c, err := vdPartOpenRead(e, key, batch)
	if err != nil {
		return vdCredentialErr(err, o.cred)
	}
	defer c.Close()
	return vdExportOpened(e.Path, o.dest, o.form, c.Info(), c)
}

func vdPartPass(e vdRegEntry, args []string, batch bool) error {
	var old, next credArg
	sawNew := false
	for i := 0; i < len(args); i++ {
		w := args[i]
		if strings.EqualFold(w, "new") && !sawNew {
			sawNew = true
			if i+1 < len(args) {
				a, ok := credentialToken(args[i+1])
				if !ok {
					return vdUsagef("the word after new is the new credential as p:, pf:, pe: or k: - never a bare word")
				}
				next = a
				i++
			}
			continue
		}
		if a, ok := credentialToken(w); ok && !sawNew && !old.given() {
			old = a
			continue
		}
		return vdUsagef("unknown word %d for pass: want [<current credential>] [new <new credential>], each as p:, pf:, pe: or k:", i+2)
	}
	defer vdForgetEnv(old)
	defer vdForgetEnv(next)
	if e.Protection != "encrypted" {
		return vdUsagef("%s has no credential to change; it is obfuscated, not encrypted, and opens without one", e.Name)
	}
	if err := vdPartRefuseMounted(e, "pass"); err != nil {
		return err
	}
	oldKey, err := vdResolveCredentialAs(old, false, "Current credential (no echo): ")
	if err != nil {
		return err
	}
	defer clear(oldKey)
	newKey, err := vdResolveCredentialAs(next, true, "New credential (no echo): ")
	if err != nil {
		return err
	}
	defer clear(newKey)
	if len(newKey) == 0 {
		return vdUsagef("an encrypted container is never given an empty credential, and the credential was not changed; to remove the password, image or clone it: filedo vd clone %s <new.fdd> nopass", e.Name)
	}
	c, err := vdPartCarrier(vdPartRecordOf(e), true, batch)
	if err != nil {
		return err
	}
	defer c.Close()
	info, err := vdisk.InspectOn(c, e.Path)
	if err != nil {
		return err
	}
	if err := vdPartCheckID(e, info); err != nil {
		return err
	}
	if err := vdisk.ChangeCredentialOn(c, oldKey, newKey, next.keyfile()); err != nil {
		return vdCredentialErr(err, old)
	}
	fmt.Printf("Changed the credential of %s. The data was not rewritten: only the key slot changed, and the data key inside it is the same.\n", e.Name)
	fmt.Println("An image of this partition made before now still opens with the old credential.")
	vdLogf("pass %s: credential changed (keyfile=%v)", e.Path, next.keyfile())
	return nil
}

// vdPartCopy is seal and clone of a partition disk: the source through a
// brokered read handle, the destination always a new file.
func vdPartCopy(e vdRegEntry, args []string, seal bool, batch bool) error {
	verb := "clone"
	if seal {
		verb = "seal"
	}
	if len(args) < 1 {
		return vdUsagef("%s needs a new file: filedo vd %s %s <new.fdd> [nopass]", verb, verb, e.Name)
	}
	dst := args[0]
	var cred credArg
	nopass := false
	for _, w := range args[1:] {
		if strings.EqualFold(w, "nopass") {
			nopass = true
			continue
		}
		a, ok := credentialToken(w)
		if !ok || cred.given() {
			return vdUsagef("%s takes a container, a new file, nopass and at most one credential (p:, pf:, pe: or k:)", verb)
		}
		cred = a
	}
	defer vdForgetEnv(cred)
	if err := vdRefuseCredentialSlot("new file slot", dst); err != nil {
		return err
	}
	if isPartLocator(dst) {
		return fmt.Errorf("%w: a sealed or cloned copy is written to a new file", vdisk.ErrUnsupported)
	}
	if !isFddPath(dst) {
		return vdUsagef("the new file of %s is a container file, whose name ends in .fdd", verb)
	}
	if _, err := os.Stat(dst); err == nil {
		return vdUsagef("%s exists; a container is never written over an existing file", dst)
	}
	if err := vdPartRefuseMounted(e, verb); err != nil {
		return err
	}
	key, err := vdPartKeyFor(e, cred)
	if err != nil {
		return err
	}
	defer clear(key)
	src, err := vdPartOpenRead(e, key, batch)
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	defer src.Close()
	si := src.Info()
	o := vdisk.SealOptions{Src: e.Name, Dst: dst, Obfuscate: nopass, Credential: key, Keyfile: cred.keyfile()}
	if si.Obfuscated {
		o.Credential = nil
	}
	vdWriterStamp()
	line := vdPercent(map[bool]string{true: "Sealing", false: "Copying"}[seal])
	o.Progress = line
	if seal {
		err = vdisk.SealFrom(vdContext(), src, o)
	} else {
		err = vdisk.CopyFrom(vdContext(), src, o)
	}
	line.end()
	if err != nil {
		return vdCredentialErr(err, cred)
	}
	info, err := vdisk.Inspect(dst)
	if err != nil {
		return err
	}
	if seal {
		fmt.Printf("Sealed %s into %s: %s volume, read-only for good; %s. %s is unchanged.\n", e.Name, dst, vdSize(info.LogicalSize), vdProtectionNote(info), e.Name)
	} else {
		fmt.Printf("Cloned %s into %s: %s, %s volume; %s. %s is unchanged.\n", e.Name, dst, info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info), e.Name)
		fmt.Printf("New container id: %s (a container of its own: both can be mounted side by side).\n", info.ContainerID)
	}
	if nopass && !si.Obfuscated {
		fmt.Println(vdNopassNote)
	}
	vdLogf("%s %s into %s (nopass=%v)", verb, e.Path, dst, nopass)
	return nil
}

// ---------------------------------------------------------------- destroy

// vdPartDestroy is the partition form of destroy (section 4.5): refused while
// mounted; the person types the disk's name; wipe overwrites the whole
// partition first; then the _part step deletes it after re-proving that it is
// FileDO's own.
func vdPartDestroy(e vdRegEntry, args []string, batch bool) error {
	wipe, force := false, false
	for i, w := range args {
		switch strings.ToLower(w) {
		case "wipe":
			wipe = true
		case "force", "-y":
			force = true
		default:
			if isCredentialToken(w) {
				return vdUsagef("destroy takes no credential: deleting the partition needs none")
			}
			return vdUsagef("unknown word %d for destroy: want wipe, force", i+2)
		}
	}
	if err := vdPartRefuseMounted(e, "destroy"); err != nil {
		return err
	}
	if s, err := vdLoadState(); err != nil {
		return errBusy(fmt.Sprintf("the mount state cannot be read, so %s cannot be proved unused: %v", e.Name, err))
	} else {
		for _, m := range s.Mounts {
			if strings.EqualFold(m.Path, e.Path) {
				return errBusy(fmt.Sprintf("%s is mounted at %s; destroy needs it unmounted first", e.Name, vdRowPlace(m)))
			}
		}
	}
	if vdAutoTasks()[strings.ToLower(e.Name)] {
		return vdUsagef("%s mounts automatically at logon; run: filedo vd auto off %s first", e.Name, e.Name)
	}
	if batch && !force {
		return vdUsagef("destroy deletes the partition, and a batch never answers the question; a batch line says force")
	}
	rec := vdPartRecordOf(e)
	what := e.ContainerID
	switch e.partPending() {
	case vdPendingCreate:
		what = "(never finished)"
	case vdPendingDestroy:
		what += " (an earlier destroy did not finish)"
	}
	fmt.Printf("destroy deletes the partition disk %s: %s, %s at byte %d on disk %s {%s}, container %s.\n",
		e.Name, rec.Locator, vdHumanSize(rec.Length), rec.Offset, rec.DiskModel, rec.DiskGUID, what)
	fmt.Println("The volume inside and everything on it are gone for good, and the space returns to unallocated.")
	if wipe {
		fmt.Println("wipe overwrites the whole partition first. On an SSD the drive may keep copies of old blocks that no overwrite reaches; the overwrite lowers the odds of recovery and promises nothing.")
	}
	if !force {
		fmt.Printf("Type the disk's name (%s) to delete it: ", e.Name)
		if strings.TrimSpace(vdReadLine()) != e.Name {
			return vdUsagef("nothing was deleted: the name was not typed")
		}
	}
	req := vdRequest{Part: &vdPartRequest{Op: "delete", Record: rec, ContainerID: e.partRecordedID(), Unfinished: e.partPending() != "", BrokerPID: os.Getpid()}}
	elevated := windows.GetCurrentProcessToken().IsElevated()
	if !elevated {
		if batch {
			return errTransport("deleting a partition needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
		}
		fmt.Println("Deleting the partition needs administrator consent; Windows will ask for it now.")
	}
	var err error
	switch {
	case wipe && elevated:
		// The proof that it is FileDO's own comes before the overwrite, which
		// erases the header it rests on; the registry then records the wipe,
		// and the step accepts the zeroed header positions it leaves - now or
		// on a retry after a stop.
		h, oerr := vdOpenResolved(rec, true, false)
		if oerr != nil {
			return oerr
		}
		if err = vdProveOwnPartition(h, req.Part); err != nil {
			windows.CloseHandle(h)
			return err
		}
		if err = vdMarkPartWiping(e); err != nil {
			windows.CloseHandle(h)
			return err
		}
		if err = vdPartWipe(h, rec); err != nil {
			return err
		}
		req.Part.Unfinished = true
		_, err = vdRunElevated("_part", req, batch, nil)
	case wipe:
		// The step proves ownership before it hands the partition over; the
		// wipe is recorded before the first byte is overwritten, so a stop
		// anywhere after leaves an entry whose destroy accepts the zeros.
		req.Part.Wipe = true
		_, err = vdRunElevatedBroker("_part", req, rec, func(h windows.Handle) error {
			if err := vdMarkPartWiping(e); err != nil {
				windows.CloseHandle(h)
				return err
			}
			return vdPartWipe(h, rec)
		})
	default:
		_, err = vdRunElevated("_part", req, batch, nil)
	}
	if err != nil {
		return err
	}
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		if i := r.find(e.Name); i >= 0 {
			r.Containers = append(r.Containers[:i], r.Containers[i+1:]...)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("the partition is deleted, but its name could not be forgotten (filedo vd forget %s does it): %w", e.Name, err)
	}
	fmt.Printf("Destroyed %s: the partition is deleted and its space is unallocated again. Forgot the name %s.\n", rec.Locator, e.Name)
	vdLogf("destroy %s (%s, wipe=%v)", e.Name, rec.Locator, wipe)
	return nil
}

// vdMarkPartWiping records on an entry that the wipe of its partition begins
// (FDD-BEHAVIOUR 6 item 10), before the first byte is overwritten.
func vdMarkPartWiping(e vdRegEntry) error {
	if err := vdUpdateRegistry(func(r *vdRegistry) error {
		i := r.findPart(e.Part.PartitionGUID)
		if i < 0 {
			return fmt.Errorf("%s is not registered any more", e.Name)
		}
		if r.Containers[i].partPending() != vdPendingCreate {
			r.Containers[i].Part.Pending = vdPendingDestroy // an unfinished creation stays one
		}
		return nil
	}); err != nil {
		return fmt.Errorf("the overwrite could not be recorded, so nothing was overwritten: %w", err)
	}
	return nil
}

// vdPartWipe overwrites the whole partition with zeros through the handle it
// owns, with progress and the run's stop.
func vdPartWipe(h windows.Handle, rec vdPartRecord) error {
	c, err := vdisk.NewDeviceCarrier(h)
	if err != nil {
		return err
	}
	defer c.Close()
	L, _ := c.Size()
	const chunk = 4 << 20
	buf := make([]byte, chunk)
	line := vdPercent("Overwriting")
	defer line.end()
	ctx := vdContext()
	for off := int64(0); off < L; off += chunk {
		if ctx.Err() != nil {
			return fmt.Errorf("%w: the overwrite was stopped; the partition was not deleted", vdisk.ErrStopped)
		}
		n := min(chunk, L-off)
		if _, err := c.WriteAt(buf[:n], off); err != nil {
			return fmt.Errorf("%w: the overwrite failed at byte %d: %v; the partition was not deleted", vdisk.ErrIO, off, err)
		}
		line.Progress(off+n, L)
	}
	return c.Sync()
}

// vdReadLine reads one answer line from the console.
var vdReadLine = func() string {
	var s string
	fmt.Scanln(&s)
	return s
}

// ---------------------------------------------------------------- format

func vdPartFormat(e vdRegEntry, args []string, batch bool) error {
	o, err := vdParseFormat(append([]string{e.Path}, args...))
	defer vdForgetEnv(o.cred)
	if err != nil {
		return err
	}
	if err := vdPartRefuseMounted(e, "format"); err != nil {
		return err
	}
	if batch && !o.force {
		return vdUsagef("format destroys the volume's contents, and a batch never answers the question; a batch line says force")
	}
	fmt.Printf("format destroys everything on the volume of %s and leaves an empty %s volume. The partition stays, with its container id and its credential.\n", vdPartAbout(e), vdFileSystem(o.fs))
	if !o.force && !vdConfirm("Format it?", batch) {
		return vdUsagef("nothing was formatted: the question was not answered yes")
	}
	return vdPartServe(e, vdPartServeOpts{format: true, fs: o.fs, label: o.label, cred: o.cred}, batch)
}

// ---------------------------------------------------------------- mount

func vdPartMount(e vdRegEntry, args []string, batch bool) error {
	o, err := vdParseMountOpts(append([]string{e.Path}, args...))
	if err != nil {
		return err
	}
	switch {
	case o.NoScan:
		return errVdPartNoScan
	case o.Worker || o.NoLetter:
		return errVdPartShare
	}
	if o.Keep {
		if o.Cred.given() {
			return vdUsagef("keep remounts without asking, so it cannot carry a credential; it keeps obfuscated containers only")
		}
		plain := []string{e.Path}
		for _, a := range args {
			if !strings.EqualFold(a, "keep") {
				plain = append(plain, a)
			}
		}
		return vdKeep(e.Path, plain, batch)
	}
	return vdPartServe(e, vdPartServeOpts{letter: o.Letter, ro: o.ReadOnly, cred: o.Cred}, o.noQuestions(batch))
}

type vdPartServeOpts struct {
	letter    string
	ro        bool
	cred      credArg
	format    bool
	fs, label string
}

// vdPartServe is mount and format of a partition disk (section 8.3): one
// consent prompt brokers the partition handle to this command, which reads the
// header, asks what it must, and starts the unelevated block server with the
// handle; the same elevated step then logs the initiator in.
func vdPartServe(e vdRegEntry, o vdPartServeOpts, batch bool) error {
	elevated := windows.GetCurrentProcessToken().IsElevated()
	if batch && !elevated {
		return errTransport("mounting needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console")
	}
	if e.partPending() != "" || e.ContainerID == "" {
		return fmt.Errorf("%w: %s", vdisk.ErrDamaged, e.partUnfinishedText())
	}
	if !o.format {
		if err := vdClearStaleMount(e.ContainerID, e.Name, batch); err != nil {
			return err
		}
	} else if err := vdPartRefuseMounted(e, "format"); err != nil {
		return err
	}
	rec := vdPartRecordOf(e)
	ro := o.ro
	if !o.format {
		ro = vdPartMountRO(o.ro, e.Profile)
	}
	// The partition is opened for writing unless ro is known now; a sealed
	// header read under the handle makes the mount read-only after that.
	openRW := !ro
	// The credential before the prompt, from the registry's protection (the
	// header plaintext is public and recorded at creation or adoption); the
	// header read under the handle has the final word below.
	var cred []byte
	if e.Protection == "encrypted" {
		c, err := vdResolveCredential(o.cred, false)
		if err != nil {
			return err
		}
		cred = c
		defer clear(cred)
	} else if o.cred.given() {
		fmt.Println(vdCredentialNotUsed)
		vdForgetEnv(o.cred)
	}
	var srv *vdServer
	var info vdisk.Info
	onHandle := func(h windows.Handle) error {
		var dup windows.Handle
		if err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &dup, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
			windows.CloseHandle(h)
			return err
		}
		c, err := vdisk.NewDeviceCarrier(dup)
		if err != nil {
			windows.CloseHandle(h)
			return err
		}
		info, err = vdisk.InspectOn(c, e.Path)
		c.Close()
		if err == nil {
			err = vdPartCheckID(e, info)
		}
		if err == nil && !info.Obfuscated && cred == nil {
			err = vdUsagef("%s is encrypted now, but its registration says obfuscated; register it again (filedo vd forget %s, then filedo vd adopt %s)", e.Name, e.Name, e.Path)
		}
		if err == nil && !o.format {
			err = vdPartMountNotes(e, info, &ro, batch)
		}
		if err != nil {
			windows.CloseHandle(h)
			return err
		}
		if ro && openRW {
			// Read-only by the header: the server gets a copy of the handle
			// that can only read; the share-0 open stays the one lock.
			var rd windows.Handle
			if err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &rd, windows.GENERIC_READ, false, 0); err != nil {
				windows.CloseHandle(h)
				return err
			}
			windows.CloseHandle(h)
			h = rd
		}
		srv, err = vdLaunchServerWith(e.Path, info, ro, cred, o.cred, h)
		windows.CloseHandle(h) // the server holds its own inherited copy
		return err
	}
	handoff, _, err := vdFiles(e.ContainerID)
	if err != nil {
		return err
	}
	req := vdRequest{Letter: o.letter, ReadOnly: ro, Label: o.label, FS: o.fs}
	verb := "_attach"
	if o.format {
		verb, req.FormatOnly = "_format", true
	}
	var res vdResult
	if elevated {
		h, oerr := vdOpenResolved(rec, !ro, true)
		if oerr != nil {
			return oerr
		}
		if err := onHandle(h); err != nil {
			return err
		}
		req.ReadOnly = ro // the header may have made it read-only
		req.Port, req.IQN, req.Secret, req.Serial = srv.h.Port, srv.h.IQN, srv.h.Secret, srv.h.Serial
		req.NeverHeld = srv.h.NeverHeld
		if req.Label == "" {
			req.Label = srv.h.Label
		}
		res, err = vdRunElevated(verb, req, batch, nil)
	} else {
		fmt.Println("Mounting a partition disk needs administrator consent to open the partition and connect the disk; Windows will ask for it once, now.")
		req.Part = &vdPartRequest{Op: "attach", Record: rec, Write: !ro, BrokerPID: os.Getpid(), HandoffPath: handoff}
		res, err = vdRunElevatedBroker(verb, req, rec, onHandle)
	}
	os.Remove(handoff) // the secret is not needed any more
	if o.format {
		if srv != nil {
			srv.stop("clean")
		}
		if err != nil {
			return err
		}
		fmt.Printf("Formatted the volume of %s: an empty %s volume. The partition, its container id and its credential are unchanged.\n", e.Name, vdFileSystem(o.fs))
		vdLogf("format %s as %s", e.Path, vdFileSystem(o.fs))
		return nil
	}
	if err != nil {
		if srv != nil {
			srv.stop("clean")
		}
		return err
	}
	row := vdMountRow{ContainerID: info.ContainerID, Path: e.Path, Letter: res.Letter, ReadOnly: ro,
		MountedAt: time.Now(), ServerPID: srv.pid, ServerStarted: srv.started, Port: srv.h.Port, IQN: srv.h.IQN,
		Serial: srv.h.Serial, Session: res.Session, Profile: info.Profile.String(), MountPath: res.MountPath,
		VolumeGUID: res.VolumeGUID, MountBase: res.MountBase, Carrier: "partition"}
	if err := vdUpdateState(func(s *vdState) error {
		s.Mounts = append(s.Mounts, row)
		return nil
	}); err != nil {
		return fmt.Errorf("mounted at %s, but the mount state could not be recorded (unmount by letter still works): %w", res.Letter, err)
	}
	if res.Formatted {
		fmt.Println("The new volume was formatted NTFS, with indexing turned off (the drive's Properties can turn it back on).")
	}
	roText := ""
	if ro {
		roText = " read-only"
	}
	fmt.Printf("Mounted %s%s at %s. Unmount with: filedo vd unmount %s\n", e.Name, roText, res.MountPath, e.Name)
	EmitFindingEvent("info", "Disk mounted", map[string]interface{}{"vdMount": map[string]interface{}{"containerId": info.ContainerID, "mountPath": res.MountPath, "volumeGuid": res.VolumeGUID, "letter": res.Letter, "readOnly": ro, "carrier": "partition"}})
	vdLogf("mount %s (%s) at %s", e.Name, e.Path, res.MountPath)
	vdTouchRegistry(info.ContainerID)
	return nil
}

// vdPartMountNotes is what mount says about the header before it attaches,
// as for a file (FDD-BEHAVIOUR 5 rule 1).
func vdPartMountNotes(e vdRegEntry, info vdisk.Info, ro *bool, batch bool) error {
	switch info.Profile {
	case vdisk.ProfilePlain, vdisk.ProfileFast, vdisk.ProfileRAM, vdisk.ProfileVault:
	case vdisk.ProfileSealed:
		*ro = true
	default:
		return fmt.Errorf("%w: mounting a %s container is not carried by this build", vdisk.ErrUnsupported, info.Profile)
	}
	fmt.Printf("Container %s: %s, %s, %s.\n", vdPartAbout(e), info.Profile, vdSize(info.LogicalSize), vdProtectionNote(info))
	switch {
	case info.SaveInProgress:
		fmt.Printf("WARNING: a save of this ram container was interrupted. It began %s; the last complete save was %s.\n", vdTime(info.SaveStarted), vdTime(info.LastGoodSave))
		fmt.Println("The volume may hold a mixture of the two states. Image it to a file first if its contents matter (filedo vd image).")
		if !*ro && !vdConfirm("Mount it anyway?", batch) {
			return vdUsagef("not mounted: an interrupted save needs an answer (mount it from a console, or mount it ro to look first)")
		}
	case !info.Clean:
		fmt.Printf("Note: it was not closed cleanly the last time; last good save %s. Windows checks the volume as it would after a power loss.\n", vdTime(info.LastGoodSave))
	}
	if info.Profile == vdisk.ProfileRAM && !*ro {
		p := vdisk.DefaultSavePolicy
		fmt.Printf("ram: writes are kept in memory and saved to the partition every %d s or at %d MB; a crash loses what was written since the last save.\n", int(p.Every.Seconds()), p.DirtyLimit>>20)
	}
	return nil
}

// ---------------------------------------------------------------- the server's open

// vdServeOpenPart is the block server's open of a partition disk: the handle
// it inherited is checked against the registered partition - GUID, offset,
// length and type, as vdCheckPartHandle - before any I/O (section 12), then
// the container is opened on it and its id checked against the registry.
func vdServeOpenPart(locator string, h windows.Handle, cred fdsec.Credential, mode vdisk.OpenMode) (*vdisk.Container, error) {
	if h == 0 {
		return nil, vdUsagef("_serve of a partition disk needs its handle")
	}
	e, ok := vdPartEntryFor(locator)
	if !ok || !isPartLocator(locator) {
		windows.CloseHandle(h)
		return nil, vdUsagef("%s is not a registered partition disk; the server serves only one", locator)
	}
	if err := vdCheckPartHandle(h, vdPartRecordOf(e)); err != nil {
		windows.CloseHandle(h)
		return nil, err
	}
	c, err := vdisk.NewDeviceCarrier(h)
	if err != nil {
		return nil, err
	}
	// The server's handle is the only one on the partition (share mode 0), so
	// a write-through read cache is coherent (SP-0148 S9).
	vdisk.EnableReadCache(c, vdPartReadCacheBytes(os.Getenv(vdPartReadCacheEnv)))
	cont, err := vdisk.OpenOn(context.Background(), c, locator, cred, mode)
	if err != nil {
		c.Close()
		return nil, err
	}
	if e.ContainerID != "" && !strings.EqualFold(cont.Info().ContainerID, e.ContainerID) {
		cont.Close()
		return nil, fmt.Errorf("%w: the partition %s holds container %s, not the registered %s", errVdPartChanged, locator, cont.Info().ContainerID, e.ContainerID)
	}
	return cont, nil
}

// ---------------------------------------------------------------- list and status

// vdPartState is what discovery says about a registered partition disk, with
// no prompt: "ok", "missing" (not connected), "different" (it changed or is
// ambiguous), or "unreadable" (discovery failed).
func vdPartState(e vdRegEntry, disks []vdDisk) (state, why string) {
	if disks == nil {
		return "unreadable", "the disks could not be listed"
	}
	_, _, err := vdResolvePart(vdPartRecordOf(e), disks)
	switch {
	case err == nil:
		return "ok", ""
	case errors.Is(err, errVdPartNotConnected):
		return "missing", err.Error()
	}
	return "different", err.Error()
}

// vdPartListState is the bracket of `vd list` for a partition disk.
func vdPartListState(e vdRegEntry, disks []vdDisk) string {
	if m, ok := vdFindMount(e.ContainerID); ok {
		return "mounted at " + vdRowPlace(m)
	}
	switch e.partPending() {
	case vdPendingCreate:
		return "UNFINISHED - its creation did not complete; filedo vd destroy " + e.Name + " deletes it"
	case vdPendingDestroy:
		return "UNFINISHED - its destroy did not complete; filedo vd destroy " + e.Name + " finishes it"
	}
	switch st, why := vdPartState(e, disks); st {
	case "ok":
		return fmt.Sprintf("on %s, %s at byte %d", e.Part.DiskModel, vdHumanSize(e.Part.Length), e.Part.Offset)
	case "missing":
		return "NOT CONNECTED - its disk is not on this machine"
	default:
		return "CHANGED - " + why
	}
}

// vdSnapPartFn fills a partition disk's row of the status snapshot; a seam.
var vdSnapPartFn = vdSnapPart

var (
	vdSnapDisksOnce sync.Once
	vdSnapDisks     []vdDisk
)

func vdSnapPart(row *vdSnapContainer, wantID string) {
	vdSnapDisksOnce.Do(func() { vdSnapDisks, _ = vdDiscoverDisks() })
	e, ok := vdPartEntryFor(row.Locator)
	if !ok {
		row.File, row.FileError = "unreadable", "the partition disk is not registered on this machine"
		return
	}
	row.File, row.FileError = vdPartState(e, vdSnapDisks)
	switch e.partPending() {
	case vdPendingCreate:
		row.File, row.FileError = "unreadable", "its creation did not complete"
	case vdPendingDestroy:
		row.File, row.FileError = "unreadable", "its destroy did not complete"
	}
	if row.Protection == "" {
		row.Protection = e.Protection
	}
}

// vdInspectAny is vdisk.Inspect for a file, and the brokered header read of a
// registered partition disk for a locator (the keep watcher runs elevated, so
// it opens the partition itself).
func vdInspectAny(path string) (vdisk.Info, error) {
	if !isPartLocator(path) {
		return vdisk.Inspect(path)
	}
	e, ok := vdPartEntryFor(path)
	if !ok {
		return vdisk.Info{}, vdUsagef("%s is not a registered partition disk", path)
	}
	return vdPartInspect(e, true)
}
