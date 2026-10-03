package vdisk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"

	"filedo/fdsec"
	"filedo/fsx"
)

// Seal writes a sealed copy of the container at src to dst (SP-0004 P4
// section 4.2): the same volume, its clusters in logical order, profile
// sealed, and a new container id - the copy is a container of its own, so
// both can be mounted side by side. src is opened read-only and is never
// changed; dst must not exist.
//
// The copy is written under dst + fsx.PartialSuffix and renamed to dst only after
// its final header is on disk, so an interrupted or failed seal leaves no
// file at dst, and removes the partial one (FDD-BEHAVIOUR 6.7).
//
// A source that was not closed cleanly, or whose ram save was cut, is
// refused: sealing freezes the volume for good, and a volume the file system
// may still have to repair is not one to freeze.
//
// Compression (Q16) is not applied: FDD-FORMAT 1.0 reserves flags bit 0 but
// defines no table, and a table is a format minor of its own.
func Seal(ctx context.Context, src, dst string, p ProgressSink) error {
	return SealWith(ctx, SealOptions{Src: src, Dst: dst, Progress: p})
}

// SealOptions is a seal of an encrypted container as well: the credential
// opens the source and wraps the sealed copy, which gets a new data key of its
// own. For an obfuscated source the credential is not used and the copy is
// obfuscated too - a seal never changes which of the two a volume has, unless
// Obfuscate asks for it.
type SealOptions struct {
	Src, Dst   string
	Credential fdsec.Credential
	Keyfile    bool // the credential is a keyfile's digest: the copy's slot is kind 3
	// Obfuscate writes the copy without a credential: obfuscated, so anyone
	// with the file reads it. It is the one way an encrypted volume loses its
	// credential - never in place, always as a new container beside the
	// source, which keeps its credential (FDD-BEHAVIOUR 7 rule 6).
	Obfuscate bool
	Progress  ProgressSink
}

// CopyOptions are the options of CopyWith, the same as a seal's.
type CopyOptions = SealOptions

// SealWith is Seal with a credential.
func SealWith(ctx context.Context, o SealOptions) error { return copyContainer(ctx, o, true) }

// CopyWith writes a copy of src to dst that stays writable: the same volume
// in a container of its own, with a new container id and data key, under the
// same rules as a seal (a clean source; dst must not exist; a partial file
// never remains). The copy keeps the source's profile, except that a vault
// copied with Obfuscate, and a sealed source, become plain.
func CopyWith(ctx context.Context, o CopyOptions) error { return copyContainer(ctx, o, false) }

// copyBeforePublish runs after every check of a copy, right before the rename
// that publishes it: the test seam of AUD-36-F3 (a file arriving at dst then).
var copyBeforePublish = func(tmp, dst string) {}

func copyContainer(ctx context.Context, o SealOptions, seal bool) (err error) {
	src, dst := o.Src, o.Dst
	// A Stat that fails for another reason than not-exist is not proof of
	// absence (AUD-36-F3): "could not check", class 5.
	if _, err := os.Stat(dst); err == nil {
		return usagef("%s already exists; a container is never written over an existing file", dst)
	} else if !os.IsNotExist(err) {
		return ioErr(fmt.Errorf("could not check whether %s exists: %w", dst, err))
	}
	s, err := Open(ctx, src, o.Credential, OpenRead)
	if err != nil {
		return err
	}
	defer s.Close()
	return copyFrom(ctx, s, o, seal)
}

// SealFrom is SealWith from a source the caller opened read-only - a
// partition container, whose carrier has no path (SP-0148 9.1). o.Src only
// names the source in messages. The destination is always a new file.
func SealFrom(ctx context.Context, s *Container, o SealOptions) error {
	return copyFromChecked(ctx, s, o, true)
}

// CopyFrom is CopyWith from a source the caller opened read-only.
func CopyFrom(ctx context.Context, s *Container, o CopyOptions) error {
	return copyFromChecked(ctx, s, o, false)
}

func copyFromChecked(ctx context.Context, s *Container, o SealOptions, seal bool) error {
	if _, err := os.Stat(o.Dst); err == nil {
		return usagef("%s already exists; a container is never written over an existing file", o.Dst)
	} else if !os.IsNotExist(err) {
		return ioErr(fmt.Errorf("could not check whether %s exists: %w", o.Dst, err))
	}
	if s.mode != OpenRead {
		return usagef("a copy reads its source through a read-only open")
	}
	return copyFrom(ctx, s, o, seal)
}

func copyFrom(ctx context.Context, s *Container, o SealOptions, seal bool) (err error) {
	src, dst, p := o.Src, o.Dst, o.Progress
	info := s.Info()
	act := "copying"
	if seal {
		act = "sealing"
	}
	switch {
	case seal && info.Profile == ProfileSealed:
		return usagef("%s is sealed already", src)
	case info.SaveInProgress:
		return damagedf("a save of %s was interrupted; mount it, check the volume and unmount it cleanly before %s", src, act)
	case !info.Clean:
		return usagef("%s was not closed cleanly; mount it once so Windows checks the volume, unmount it, then try again", src)
	}
	profile := ProfilePlain
	if !seal && info.Profile != ProfileSealed && !(info.Profile == ProfileVault && o.Obfuscate) {
		profile = info.Profile
	}
	shift := func(n int64) uint8 {
		var s uint8
		for n > 1 {
			n >>= 1
			s++
		}
		return s
	}
	var cred fdsec.Credential
	if !info.Obfuscated && !o.Obfuscate {
		cred = o.Credential
	}
	// The repo's partial name, which every copy verb recognises (AUD-36-F3).
	tmp := dst + fsx.PartialSuffix
	d, err := Create(ctx, CreateOptions{
		Path:         tmp,
		LogicalSize:  info.LogicalSize,
		Profile:      profile,
		ClusterShift: shift(info.ClusterSize),
		SectorShift:  shift(info.SectorSize),
		FriendlyName: info.FriendlyName,
		Credential:   cred,
		Keyfile:      o.Keyfile && len(cred) != 0,
	})
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			d.Discard()
			// A partial left behind is reported by name (AUD-36-F3).
			if rerr := os.Remove(tmp); rerr != nil && !os.IsNotExist(rerr) {
				err = fmt.Errorf("%w; the unfinished copy %s could not be removed (%v) - delete it by hand", err, tmp, rerr)
			}
		}
	}()
	cs := info.ClusterSize
	buf := make([]byte, cs)
	total := info.ClusterCount
	copied := false
	for cl := int64(0); cl < total; cl++ {
		if stopped(ctx) {
			return ErrStopped
		}
		if !s.clusterAllocated(cl) {
			continue
		}
		n := min(cs, info.LogicalSize-cl*cs)
		if _, err := s.ReadAt(buf[:n], cl*cs); err != nil {
			return err
		}
		if !allZero(buf[:n]) {
			if _, err := d.WriteAt(buf[:n], cl*cs); err != nil {
				return err
			}
			copied = true
		}
		report(p, cl+1, total)
	}
	// The copy holds the source's volume, so it carries the source's mount
	// count, and at least 1 once any cluster was copied: mount reads count 0
	// as "never held data" and may format a disk with no partition table
	// (T3-F4).
	if carry := carriedMountCount(info.MountCount, copied); carry != 0 {
		if err := d.setMountCount(carry); err != nil {
			return err
		}
	}
	closeCopy := d.Close
	if seal {
		closeCopy = d.closeSealed
	}
	if err := closeCopy(); err != nil {
		return err
	}
	// Reopen the completed file and read every referenced cluster before it
	// becomes the named result. A successful writer close alone does not prove
	// that the finished map and its referenced data can be read back.
	check, err := Open(ctx, tmp, cred, OpenRead)
	if err != nil {
		return err
	}
	report, verifyErr := check.Verify(ctx, nil)
	if verifyErr == nil && len(report.Problems) == 0 && report.ClustersRead == report.AllocatedClusters {
		// Format 1.0 has no data digests. Compare the logical volume with the
		// source while both read locks are held, including sparse zero regions.
		from, to := make([]byte, 1<<20), make([]byte, 1<<20)
		for off := int64(0); off < info.LogicalSize; off += int64(len(from)) {
			if stopped(ctx) {
				verifyErr = ErrStopped
				break
			}
			n := int(min(int64(len(from)), info.LogicalSize-off))
			if _, verifyErr = s.ReadAt(from[:n], off); verifyErr != nil {
				break
			}
			if _, verifyErr = check.ReadAt(to[:n], off); verifyErr != nil {
				break
			}
			if !bytes.Equal(from[:n], to[:n]) {
				verifyErr = damagedf("the completed copy differs from its source at volume offset %d", off)
				break
			}
		}
	}
	closeErr := check.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if len(report.Problems) != 0 || report.ClustersRead != report.AllocatedClusters {
		return damagedf("the completed copy could not be verified")
	}
	if stopped(ctx) {
		return ErrStopped
	}
	copyBeforePublish(tmp, dst)
	if err := fsx.RenameNoReplace(tmp, dst); err != nil {
		if errors.Is(err, fsx.ErrDestinationExists) {
			return usagef("%s appeared while the copy ran; the new copy was removed", dst)
		}
		return ioErr(err)
	}
	return nil
}

// carriedMountCount is the mount count a copy records: the source's, and at
// least 1 when the copy received any data.
func carriedMountCount(source uint64, copied bool) uint64 {
	if copied && source == 0 {
		return 1
	}
	return source
}

// setMountCount writes the header pair with mount_count n, every other field
// as it stands; a copy calls it before its close.
func (c *Container) setMountCount(n uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return usagef("the container is closed")
	}
	h := c.hdr
	h.MountCount = n
	return c.writeHeaderPair(h)
}

func (c *Container) clusterAllocated(cl int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.entries[cl] != sentinel
}

// closeSealed ends the writing session of a seal: one commit that sets the
// profile to sealed and the clean marker, then the file is closed. From then
// on no build opens it for writing.
func (c *Container) closeSealed() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return usagef("the container is closed")
	}
	if c.failed != nil {
		return c.failed
	}
	err := c.commit(func(h *header) {
		h.Profile = ProfileSealed
		h.Clean = 1
	})
	c.closed = true
	if cerr := c.b.Close(); err == nil && cerr != nil {
		err = ioErr(cerr)
	}
	return err
}
