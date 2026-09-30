package vdisk

import (
	"context"
	"os"

	"filedo/fdsec"
)

// Seal writes a sealed copy of the container at src to dst (SP-0004 P4
// section 4.2): the same volume, its clusters in logical order, profile
// sealed, and a new container id - the copy is a container of its own, so
// both can be mounted side by side. src is opened read-only and is never
// changed; dst must not exist.
//
// The copy is written under dst + ".partial" and renamed to dst only after
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

func copyContainer(ctx context.Context, o SealOptions, seal bool) (err error) {
	src, dst, p := o.Src, o.Dst, o.Progress
	if _, err := os.Stat(dst); err == nil {
		return usagef("%s already exists; a container is never written over an existing file", dst)
	}
	s, err := Open(ctx, src, o.Credential, OpenRead)
	if err != nil {
		return err
	}
	defer s.Close()
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
	tmp := dst + ".partial"
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
			os.Remove(tmp)
		}
	}()
	cs := info.ClusterSize
	buf := make([]byte, cs)
	total := info.ClusterCount
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
		}
		report(p, cl+1, total)
	}
	closeCopy := d.Close
	if seal {
		closeCopy = d.closeSealed
	}
	if err := closeCopy(); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		os.Remove(tmp)
		return usagef("%s appeared while the seal ran; the sealed copy was removed", dst)
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return ioErr(err)
	}
	return nil
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
