//go:build windows

package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"filedo/statedir"
	"filedo/vdisk"
)

// The elevated half of partition disks (SP-0148 section 8). An elevated step
// only (a) changes the partition table, through the Storage Management API
// (`_part`), or (b) opens a partition device and hands the handle to an
// unelevated FileDO process (`_open`, and `_attach` for a mount). It never
// reads or writes container data, holds a credential or listens on a socket
// (FDD-BEHAVIOUR 6.9; D9). Every target is re-derived here from the identity
// the request carries (the AUD-31-F4 rule of `_task`): the disk and partition
// numbers are found by GUID in this process and never taken from the request.

// vdPartRequest is a partition disk's part of a vdRequest.
type vdPartRequest struct {
	Op     string       `json:"op"` // open, create, delete, attach
	Record vdPartRecord `json:"record"`
	Write  bool         `json:"write,omitempty"`
	// BrokerPID is the FileDO process the handle is duplicated into: the
	// command that asked. Its image must be this executable.
	BrokerPID int `json:"broker_pid"`
	// create: the disk by its GPT GUID, the extent, and the partition list the
	// user confirmed (section 6.4).
	DiskGUID string       `json:"disk_guid,omitempty"`
	Offset   int64        `json:"offset,omitempty"`
	Size     int64        `json:"size,omitempty"`
	Layout   []vdDiskPart `json:"layout,omitempty"`
	// delete: the container id the header must carry (the registered one, or
	// the one an unfinished creation recorded), and Unfinished for work that
	// did not complete (vdPartRecord.Pending): both header positions zero then
	// prove it as well (vdProveOwn).
	ContainerID string `json:"container_id,omitempty"`
	Unfinished  bool   `json:"unfinished,omitempty"`
	Wipe        bool   `json:"wipe,omitempty"`
	// attach: the handoff the block server writes once it serves the
	// container (it lives in the state root, by container id).
	HandoffPath string `json:"handoff_path,omitempty"`
}

const ioctlDiskSetPartitionInfoEx = 0x0007C10C

// vdOpenPartition opens partition part of disk disk for I/O: read-write with
// share mode 0 (the one-holder lock of D10), or read-only sharing reads only,
// so a mount's exclusive open and this one exclude each other either way.
// Unbuffered, never write-through (D12).
func vdOpenPartition(disk, part int, write bool) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(vdPartDevicePath(disk, part))
	if err != nil {
		return 0, err
	}
	access, share := uint32(windows.GENERIC_READ), uint32(windows.FILE_SHARE_READ)
	if write {
		access, share = windows.GENERIC_READ|windows.GENERIC_WRITE, 0
	}
	h, err := windows.CreateFile(p, access, share, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_NO_BUFFERING, 0)
	if err != nil {
		if errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
			return 0, errBusy("the partition is in use by another FileDO process or program (mounted, or being read)")
		}
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return 0, errTransport("opening a partition needs administrator rights")
		}
		return 0, fmt.Errorf("%w: could not open the partition: %v", vdisk.ErrIO, err)
	}
	return h, nil
}

// vdCheckPartHandle proves a handle is open on the record's partition: its
// GUID, offset and length (section 12: the receiver validates a brokered
// handle before the first I/O).
func vdCheckPartHandle(h windows.Handle, rec vdPartRecord) error {
	p, err := vdPartitionInfo(h)
	if err != nil {
		return fmt.Errorf("%w: the partition handle could not be checked: %v", vdisk.ErrIO, err)
	}
	if vdNormGUID(p.GUID) != vdNormGUID(rec.PartitionGUID) || p.Offset != rec.Offset || p.Length != rec.Length || !p.FileDO {
		return fmt.Errorf("%w: the handle is not open on the partition %s; nothing was read or written", errVdPartChanged, rec.Locator)
	}
	return nil
}

// vdImageOf is the executable image of an open process.
func vdImageOf(proc windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(proc, 0, &buf[0], &n); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

// vdOpenRequester opens the FileDO process pid that asked for a partition
// handle, once, for the image check, the duplication and the wait alike: the
// process checked is the process served. A process whose image is not this
// executable gets nothing. The caller closes the handle.
func vdOpenRequester(pid int) (windows.Handle, error) {
	self, err := os.Executable()
	if err != nil {
		return 0, err
	}
	proc, err := windows.OpenProcess(windows.PROCESS_DUP_HANDLE|windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return 0, errTransport(fmt.Sprintf("the FileDO process %d that asked for the partition is gone: %v", pid, err))
	}
	img, err := vdImageOf(proc)
	if err != nil {
		windows.CloseHandle(proc)
		return 0, errTransport(fmt.Sprintf("the FileDO process %d that asked for the partition is gone: %v", pid, err))
	}
	if !strings.EqualFold(filepath.Clean(img), filepath.Clean(self)) {
		windows.CloseHandle(proc)
		return 0, vdUsagef("process %d is not this FileDO (%s); no partition handle was given to it", pid, img)
	}
	return proc, nil
}

// vdDupInto duplicates h into the FileDO process pid and returns the handle's
// value there (vdOpenRequester checks the process).
func vdDupInto(h windows.Handle, pid int) (windows.Handle, error) {
	if pid == os.Getpid() {
		var out windows.Handle
		err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &out, 0, false, windows.DUPLICATE_SAME_ACCESS)
		return out, err
	}
	proc, err := vdOpenRequester(pid)
	if err != nil {
		return 0, err
	}
	defer windows.CloseHandle(proc)
	return vdDupIntoProc(h, proc)
}

// vdDupIntoProc duplicates h into an opened requester.
func vdDupIntoProc(h, proc windows.Handle) (windows.Handle, error) {
	var out windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), h, proc, &out, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return 0, err
	}
	return out, nil
}

// vdCloseRemote closes a handle this step duplicated into proc and never
// told it about.
func vdCloseRemote(proc, out windows.Handle) {
	windows.DuplicateHandle(proc, out, 0, nil, 0, false, windows.DUPLICATE_CLOSE_SOURCE)
}

// vdResolveHere is the resolution of section 6.1 in this process.
func vdResolveHere(rec vdPartRecord, mount bool) (vdDisk, vdDiskPart, error) {
	disks, err := vdDiscoverDisks()
	if err != nil {
		return vdDisk{}, vdDiskPart{}, err
	}
	d, p, err := vdResolvePart(rec, disks)
	return d, p, vdPartClass(err, mount)
}

// vdOpenResolved resolves a record and opens its partition, checked.
func vdOpenResolved(rec vdPartRecord, write, mount bool) (windows.Handle, error) {
	d, p, err := vdResolveHere(rec, mount)
	if err != nil {
		return 0, err
	}
	h, err := vdOpenPartition(d.Number, p.Number, write)
	if err != nil {
		return 0, err
	}
	if err := vdCheckPartHandle(h, rec); err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	return h, nil
}

// vdOpenStep is `_open` (section 8.4): open the partition, check it, and
// duplicate the handle into the asking process. Nothing is read here.
func vdOpenStep(req vdRequest) (vdResult, error) {
	var res vdResult
	if req.Part == nil {
		return res, vdUsagef("_open needs a partition request")
	}
	h, err := vdOpenResolved(req.Part.Record, req.Part.Write, false)
	if err != nil {
		return res, err
	}
	defer windows.CloseHandle(h)
	out, err := vdDupInto(h, req.Part.BrokerPID)
	if err != nil {
		return res, err
	}
	res.Handle = uint64(out)
	vdLogf("open %s for process %d (write=%v)", req.Part.Record.Locator, req.Part.BrokerPID, req.Part.Write)
	return res, nil
}

// vdBrokerHand hands h to the asking process and waits for its answer: the
// handle's value goes to <result>.handle (CREATE_NEW, as the result itself),
// and the step continues when <result>.go appears, or stops on
// <result>.cancel or when the asking process is gone. There is no time limit
// while it lives: what it does with the handle (a wipe of the whole
// partition, a mount's questions) takes as long as it takes, and its own stop
// or exit ends the wait.
func vdBrokerHand(h windows.Handle, pid int, resPath string) error {
	proc, err := vdOpenRequester(pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(proc)
	out, err := vdDupIntoProc(h, proc)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(resPath+".handle", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		vdCloseRemote(proc, out) // never announced: the asking process cannot know of it
		return err
	}
	_, werr := f.WriteString(strconv.FormatUint(uint64(out), 10))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		// The asking process may have read the value already and owns the
		// copy then; it is not closed behind its back.
		return werr
	}
	for {
		if _, err := os.Stat(resPath + ".go"); err == nil {
			return nil
		}
		if _, err := os.Stat(resPath + ".cancel"); err == nil {
			return fmt.Errorf("%w: the command that asked stopped", vdisk.ErrStopped)
		}
		ev, err := windows.WaitForSingleObject(proc, 100)
		if err != nil {
			return errTransport("the command that asked cannot be watched: " + err.Error())
		}
		if ev == windows.WAIT_OBJECT_0 {
			// Its answer may have landed just before it exited.
			if _, err := os.Stat(resPath + ".go"); err == nil {
				return nil
			}
			return fmt.Errorf("%w: the command that asked is gone", vdisk.ErrStopped)
		}
	}
}

// ---------------------------------------------------------------- _attach for a partition

// vdPartAttach is `_attach` for a partition disk (section 8.3): resolve and
// re-validate, open the partition with share mode 0 (read-only for ro), check
// the handle, hand it to the mounting command - which inspects the container,
// asks what it must, and starts the unelevated block server with the handle -
// then wait for the server's handoff and log the initiator in as for a file.
// One consent prompt, as for a file.
func vdPartAttach(req vdRequest, resPath string) (vdResult, error) {
	var res vdResult
	pr := req.Part
	if err := vdCheckHandoffPath(pr.HandoffPath, pr.Record.ContainerID); err != nil {
		return res, err
	}
	h, err := vdOpenResolved(pr.Record, !req.ReadOnly, true)
	if err != nil {
		return res, err
	}
	err = vdBrokerHand(h, pr.BrokerPID, resPath)
	windows.CloseHandle(h)
	if err != nil {
		return res, err
	}
	hand, err := vdAwaitHandoff(pr.HandoffPath, 90*time.Second, resPath+".cancel")
	if err != nil {
		return res, err
	}
	if pr.Record.ContainerID != "" && !strings.EqualFold(hand.ContainerID, pr.Record.ContainerID) {
		return res, fmt.Errorf("%w: the block server serves container %s, not %s; nothing was attached", errVdPartChanged, hand.ContainerID, pr.Record.ContainerID)
	}
	req.Port, req.IQN, req.Secret, req.Serial = hand.Port, hand.IQN, hand.Secret, hand.Serial
	if req.Label == "" {
		req.Label = hand.Label
	}
	req.NeverHeld = hand.NeverHeld
	// The header the mounting command read may make the mount read-only (a
	// sealed container): the server says so, and the disk comes online so.
	req.ReadOnly = req.ReadOnly || hand.ReadOnly
	return vdAttach(req, resPath+".cancel")
}

// vdPartFormatStep is `_format` for a partition disk: the attach with
// FormatOnly, then the ordinary detach, as vdFormatStep for a file.
func vdPartFormatStep(req vdRequest, resPath string) (vdResult, error) {
	req.FormatOnly = true
	res, err := vdPartAttach(req, resPath)
	if err != nil {
		return res, err
	}
	if _, derr := vdDetach(vdRequest{Port: req.Port, IQN: req.IQN, Serial: req.Serial, Session: res.Session}); derr != nil {
		return res, fmt.Errorf("the volume was formatted, but the disk could not be detached: %w", derr)
	}
	return res, nil
}

// vdCheckHandoffPath accepts only the handoff file this container's server
// writes in the state root, never a path of the request's choosing.
func vdCheckHandoffPath(p, containerID string) error {
	want, _, err := vdFiles(containerID)
	if err != nil {
		return err
	}
	if containerID == "" || !strings.EqualFold(filepath.Clean(p), filepath.Clean(want)) {
		return vdUsagef("the handoff file is not the container's own; nothing was attached")
	}
	return nil
}

// vdAwaitHandoff waits for a block server's handoff.
func vdAwaitHandoff(path string, timeout time.Duration, cancel string) (vdHandoff, error) {
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if _, err := os.Stat(cancel); err == nil {
			return vdHandoff{}, fmt.Errorf("%w: the mount was interrupted", vdisk.ErrStopped)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var h vdHandoff
		if json.Unmarshal(b, &h) != nil {
			continue
		}
		if h.Error != "" {
			return h, vdClassError(h.Class, "the block server could not start: "+h.Error)
		}
		if h.Port != 0 {
			return h, nil
		}
	}
	return vdHandoff{}, errTransport("the block server did not start serving the partition within " + timeout.String())
}

// ---------------------------------------------------------------- _part

// vdPartStep is `_part` (section 8.2): create or delete a FileDO partition.
func vdPartStep(req vdRequest, resPath string) (vdResult, error) {
	if req.Part == nil {
		return vdResult{}, vdUsagef("_part needs a partition request")
	}
	switch req.Part.Op {
	case "create":
		return vdPartCreateStep(req.Part)
	case "delete":
		return vdPartDeleteStep(req.Part, resPath)
	}
	return vdResult{}, vdUsagef("unknown partition step %q", req.Part.Op)
}

// vdFindDisk is the one disk whose GPT GUID is guid, or a refusal.
func vdFindDisk(disks []vdDisk, guid string) (vdDisk, error) {
	var hit []vdDisk
	for _, d := range disks {
		if d.GUID != "" && strings.EqualFold(d.GUID, vdNormGUID(guid)) {
			hit = append(hit, d)
		}
	}
	switch len(hit) {
	case 0:
		return vdDisk{}, fmt.Errorf("%w: no disk with the id {%s} is connected; nothing was changed", vdisk.ErrIO, guid)
	case 1:
		return hit[0], nil
	}
	return vdDisk{}, fmt.Errorf("%w: %d disks have the id {%s}; nothing was changed", vdisk.ErrIO, len(hit), guid)
}

func vdPartCreateStep(pr *vdPartRequest) (vdResult, error) {
	var res vdResult
	disks, err := vdDiscoverDisks()
	if err != nil {
		return res, err
	}
	d, err := vdFindDisk(disks, pr.DiskGUID)
	if err != nil {
		return res, err
	}
	// Re-validation before the table changes (section 6.4): the disk still
	// passes the refusal table, its whole partition list is the one the user
	// confirmed, and the new partition lies inside one usable free extent.
	if !d.Usable {
		return res, fmt.Errorf("%w: disk %d: %s; nothing was changed", vdisk.ErrUnsupported, d.Number, vdWhy(d.Reason))
	}
	if !vdSameLayout(d.Partitions, pr.Layout) {
		return res, fmt.Errorf("%w: the partitions of disk %d changed since they were shown; nothing was changed - list them again (filedo vd disks)", vdisk.ErrIO, d.Number)
	}
	inside := false
	for _, e := range d.Free {
		if e.Usable && vdInsideExtent(e, pr.Offset, pr.Size) {
			inside = true
		}
	}
	if !inside || pr.Size < vdPartMin || pr.Offset%vdMiB != 0 || pr.Size%vdMiB != 0 {
		return res, vdUsagef("%d bytes at %d is not inside a usable free extent of disk %d; nothing was changed", pr.Size, pr.Offset, d.Number)
	}
	vdLogf("part create: disk %d {%s} %s at %d, %d bytes", d.Number, d.GUID, d.Model, pr.Offset, pr.Size)
	script := fmt.Sprintf(`$d = @(Get-Disk | Where-Object { $_.Guid -eq '{%s}' })
if ($d.Count -ne 1) { throw 'the disk {%s} is not exactly one disk' }
if ($d[0].Number -ne %d) { throw 'the disk number changed' }
$p = New-Partition -DiskNumber %d -Offset %d -Size %d -GptType '{%s}'
"$($p.Guid)"`, d.GUID, d.GUID, d.Number, d.Number, pr.Offset, pr.Size, vdPartType)
	out, err := vdPowerShell(script)
	if err != nil {
		return res, fmt.Errorf("%w: Windows did not create the partition: %s", vdisk.ErrIO, strings.TrimSpace(out))
	}
	// After the change: exactly one new entry, where and what was confirmed,
	// everything else unchanged.
	disks, err = vdDiscoverDisks()
	if err != nil {
		return res, err
	}
	d2, err := vdFindDisk(disks, pr.DiskGUID)
	if err != nil {
		return res, err
	}
	n, err := vdNewEntry(d.Partitions, d2.Partitions, pr.Offset, pr.Size)
	if err != nil {
		return res, fmt.Errorf("%w: after the partition was made: %v - check the disk in Disk Management", vdisk.ErrIO, err)
	}
	vdLogf("part create: partition %d {%s} made", n.Number, n.GUID)
	h, err := vdOpenPartition(d2.Number, n.Number, true)
	if err != nil {
		return res, err
	}
	defer windows.CloseHandle(h)
	rec := vdPartRecord{PartitionGUID: n.GUID, Offset: n.Offset, Length: n.Length}
	rec.Locator = vdLocator(n.GUID)
	if err := vdCheckPartHandle(h, rec); err != nil {
		return res, err
	}
	if err := vdSetPartName(h, n); err != nil {
		vdLogf("part create: the GPT name was not set: %v", err)
	}
	// Both header positions zeroed before anything else is written
	// (FDD-FORMAT 3.1 rule 6), through a second handle the carrier owns.
	var z windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &z, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return res, err
	}
	c, err := vdisk.NewDeviceCarrier(z)
	if err != nil {
		return res, err
	}
	zerr := vdisk.ZeroHeaderPositions(c)
	c.Close()
	if zerr != nil {
		return res, zerr
	}
	out2, err := vdDupInto(h, pr.BrokerPID)
	if err != nil {
		return res, err
	}
	res.Handle, res.PartGUID, res.PartOffset, res.PartLength = uint64(out2), n.GUID, n.Offset, n.Length
	return res, nil
}

// vdSetPartName writes the GPT name `FileDO disk` into the new partition's
// entry (IOCTL_DISK_SET_PARTITION_INFO_EX on its own handle, which changes that
// entry only), keeping its type, id and attributes, and reads it back. Best
// effort: the name is set where the platform allows and never relied on
// (FDD-FORMAT section 3.1); the VHDX run of 2026-10-03 saw the partition
// device answer "Incorrect function", and the partition is used regardless.
func vdSetPartName(h windows.Handle, p vdDiskPart) error {
	in := make([]byte, 8+112) // PartitionStyle, then PARTITION_INFORMATION_GPT at 8
	binary.LittleEndian.PutUint32(in, 1)
	tg, err := windows.GUIDFromString("{" + vdPartType + "}")
	if err != nil {
		return err
	}
	id, err := windows.GUIDFromString("{" + vdNormGUID(p.GUID) + "}")
	if err != nil {
		return err
	}
	copy(in[8:24], (*[16]byte)(unsafe.Pointer(&tg))[:])
	copy(in[24:40], (*[16]byte)(unsafe.Pointer(&id))[:])
	binary.LittleEndian.PutUint64(in[40:], p.Attributes)
	for i, c := range windows.StringToUTF16(vdPartName) {
		if i >= 36 {
			break
		}
		binary.LittleEndian.PutUint16(in[48+2*i:], c)
	}
	if _, err := ioctl(h, ioctlDiskSetPartitionInfoEx, in, nil); err != nil {
		return err
	}
	back, err := vdPartitionInfo(h)
	if err != nil {
		return err
	}
	if !back.FileDO || vdNormGUID(back.GUID) != vdNormGUID(p.GUID) || back.Name != vdPartName {
		return fmt.Errorf("the entry reads back as type %s id %s name %q", back.Type, back.GUID, back.Name)
	}
	return nil
}

func vdPartDeleteStep(pr *vdPartRequest, resPath string) (vdResult, error) {
	var res vdResult
	d, p, err := vdResolveHere(pr.Record, false)
	if err != nil {
		return res, err
	}
	if !p.FileDO {
		return res, fmt.Errorf("%w: the partition %s is not of the FileDO type; FileDO deletes only its own partitions", vdisk.ErrUnsupported, pr.Record.Locator)
	}
	h, err := vdOpenPartition(d.Number, p.Number, true)
	if err != nil {
		return res, err
	}
	if err := vdCheckPartHandle(h, pr.Record); err != nil {
		windows.CloseHandle(h)
		return res, err
	}
	// The proof that it is FileDO's own container (section 8.2): the header's
	// container id is the registered one - or, for work that never finished
	// (a creation, a wipe), both header positions read as zeros.
	if err := vdProveOwnPartition(h, pr); err != nil {
		windows.CloseHandle(h)
		return res, err
	}
	if pr.Wipe {
		if err := vdBrokerHand(h, pr.BrokerPID, resPath); err != nil {
			windows.CloseHandle(h)
			return res, err
		}
	}
	windows.CloseHandle(h)
	before := d.Partitions
	script := fmt.Sprintf(`$p = @(Get-Partition | Where-Object { $_.Guid -eq '{%s}' })
if ($p.Count -ne 1) { throw 'the partition {%s} is not exactly one partition' }
if ($p[0].GptType -ne '{%s}') { throw 'the partition is not of the FileDO type' }
if ($p[0].Offset -ne %d -or $p[0].Size -ne %d) { throw 'the partition moved' }
Remove-Partition -DiskNumber $p[0].DiskNumber -PartitionNumber $p[0].PartitionNumber -Confirm:$false`,
		vdNormGUID(p.GUID), vdNormGUID(p.GUID), vdPartType, p.Offset, p.Length)
	if out, err := vdPowerShell(script); err != nil {
		return res, fmt.Errorf("%w: Windows did not delete the partition: %s", vdisk.ErrIO, strings.TrimSpace(out))
	}
	disks, err := vdDiscoverDisks()
	if err != nil {
		return res, err
	}
	d2, err := vdFindDisk(disks, d.GUID)
	if err != nil {
		return res, err
	}
	rest := make([]vdDiskPart, 0, len(before))
	for _, q := range before {
		if vdNormGUID(q.GUID) != vdNormGUID(p.GUID) {
			rest = append(rest, q)
		}
	}
	if !vdSameLayout(rest, d2.Partitions) {
		return res, fmt.Errorf("%w: after the delete, the partition list of disk %d is not the old one minus the FileDO partition - check it in Disk Management", vdisk.ErrIO, d2.Number)
	}
	vdLogf("part delete: %s removed from disk %d {%s}", pr.Record.Locator, d2.Number, d2.GUID)
	return res, nil
}

// vdProveOwnPartition reads the header through the open handle.
func vdProveOwnPartition(h windows.Handle, pr *vdPartRequest) error {
	var dup windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), h, windows.CurrentProcess(), &dup, 0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return err
	}
	c, err := vdisk.NewDeviceCarrier(dup)
	if err != nil {
		return err
	}
	defer c.Close()
	zeroed := false
	if pr.Unfinished {
		L, err := c.Size()
		if err != nil {
			return fmt.Errorf("%w: %v", vdisk.ErrIO, err)
		}
		if zeroed, err = vdHeaderPositionsZero(c, L); err != nil {
			return err
		}
	}
	got := ""
	var why error
	if !zeroed {
		info, err := vdisk.InspectOn(c, pr.Record.Locator)
		if err != nil {
			why = err
		} else {
			got = info.ContainerID
		}
	}
	return vdProveOwn(pr.Record.Locator, pr.ContainerID, pr.Unfinished, zeroed, got, why)
}

// ---------------------------------------------------------------- the asking side

// vdPartAcquire gets a checked handle on a registered partition: opened here
// when this process is elevated, else brokered by the `_open` step (one
// consent prompt). The caller owns the handle.
func vdPartAcquire(rec vdPartRecord, write, batch bool) (windows.Handle, error) {
	if windows.GetCurrentProcessToken().IsElevated() {
		return vdOpenResolved(rec, write, false)
	}
	if batch {
		return 0, errTransport("reading a partition disk needs administrator rights, and a batch never raises a consent prompt; run the batch from an elevated console, or image the disk to a file first (filedo vd image)")
	}
	fmt.Println("Reading a partition disk needs administrator consent to open the partition; Windows will ask for it now.")
	res, err := vdRunElevated("_open", vdRequest{Part: &vdPartRequest{Op: "open", Record: rec, Write: write, BrokerPID: os.Getpid()}}, batch, nil)
	if err != nil {
		return 0, err
	}
	h := windows.Handle(res.Handle)
	if err := vdCheckPartHandle(h, rec); err != nil {
		windows.CloseHandle(h)
		return 0, err
	}
	return h, nil
}

// vdPartCarrier is vdPartAcquire as a container carrier.
func vdPartCarrier(rec vdPartRecord, write, batch bool) (vdisk.Carrier, error) {
	h, err := vdPartAcquire(rec, write, batch)
	if err != nil {
		return nil, err
	}
	return vdisk.NewDeviceCarrier(h)
}

// vdRunElevatedBroker runs an elevated step that hands this process a
// partition handle and then waits (vdBrokerHand): onHandle gets the checked
// handle while the step waits, and its answer lets the step go on or stops
// it. Only for a caller that is not elevated; an elevated one opens the
// partition itself.
func vdRunElevatedBroker(verb string, req vdRequest, rec vdPartRecord, onHandle func(h windows.Handle) error) (vdResult, error) {
	d, err := statedir.Dir()
	if err != nil {
		return vdResult{}, err
	}
	tag := fmt.Sprintf("vd-%d-%d", os.Getpid(), time.Now().UnixNano())
	reqPath := d + `\` + tag + ".request.json"
	resPath := d + `\` + tag + ".result.json"
	for _, p := range []string{reqPath, resPath, resPath + ".cancel", resPath + ".go", resPath + ".handle"} {
		defer os.Remove(p)
	}
	req.StateDir = d
	b, _ := json.Marshal(req)
	if err := os.WriteFile(reqPath, b, 0o600); err != nil {
		return vdResult{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return vdResult{}, err
	}
	params := strings.Join([]string{"vd", verb, syscall.EscapeArg(reqPath), syscall.EscapeArg(resPath), vdRequestDigest(b)}, " ")
	info := &shellExecuteInfo{
		fMask:        seeMaskNoCloseProcess | seeMaskNoAsync | seeMaskFlagNoUI,
		lpVerb:       windows.StringToUTF16Ptr("runas"),
		lpFile:       windows.StringToUTF16Ptr(exe),
		lpParameters: windows.StringToUTF16Ptr(params),
		nShow:        windows.SW_HIDE,
	}
	info.cbSize = uint32(unsafe.Sizeof(*info))
	if r, _, e := procShellExecuteEx.Call(uintptr(unsafe.Pointer(info))); r == 0 {
		if errors.Is(e, windows.ERROR_CANCELLED) {
			return vdResult{}, errTransport("administrator consent was not given; nothing was changed")
		}
		return vdResult{}, errTransport("could not start the elevated step: " + e.Error())
	}
	defer windows.CloseHandle(info.hProcess)
	answered := false
	for !answered {
		if ev, _ := windows.WaitForSingleObject(info.hProcess, 100); ev == windows.WAIT_OBJECT_0 {
			break
		}
		raw, err := os.ReadFile(resPath + ".handle")
		if err != nil || len(raw) == 0 {
			continue
		}
		answered = true
		n, perr := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		h := windows.Handle(n)
		herr := perr
		if herr == nil {
			herr = vdCheckPartHandle(h, rec)
		}
		if herr == nil {
			herr = onHandle(h)
		} else if perr == nil {
			windows.CloseHandle(h)
		}
		mark := resPath + ".go"
		if herr != nil {
			mark = resPath + ".cancel"
		}
		os.WriteFile(mark, []byte("1"), 0o600)
		windows.WaitForSingleObject(info.hProcess, windows.INFINITE)
		if herr != nil {
			return vdResult{}, herr
		}
	}
	out, err := os.ReadFile(resPath)
	if err != nil {
		return vdResult{}, fmt.Errorf("the elevated step left no result (see vdisk.log in the state folder): %w", err)
	}
	var res vdResult
	if err := json.Unmarshal(out, &res); err != nil {
		return vdResult{}, err
	}
	if !res.OK {
		return res, vdClassError(res.Class, res.Error)
	}
	return res, nil
}
