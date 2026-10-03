package vdisk

import (
	"io"

	"filedo/fdsec"
)

// Changing the credential of an encrypted container (SP-0004 P5 T5.8,
// FDD-BEHAVIOUR 7 rule 6): slots are rewritten, the data key and the data
// region never are.
//
// A slot that does not open under a credential in hand is indistinguishable
// from an unused one (FDD-FORMAT 7.1), so a writer cannot tell a free slot
// from a slot another credential uses. FileDO therefore keeps one credential
// per encrypted container - it writes exactly one slot of kind 2 or 3 and
// never adds a second - and under that rule every slot the credential in hand
// does not open is free. Adding and dropping credentials waits for a way to
// know which slots are used (the open question recorded in P5).

// ChangeCredential replaces the credential of the encrypted container at
// path. The order makes a stop at any point safe: the new slot is written
// into a free position and flushed, read back, and only then is the old slot
// overwritten with random bytes. Stopped between the two, the container opens
// with either credential; never with neither.
func ChangeCredential(path string, old, next fdsec.Credential, nextKeyfile bool) error {
	fb, err := openFile(path, true, true)
	if err != nil {
		return err
	}
	defer fb.Close()
	return changeCredentialOn(fb, old, next, nextKeyfile)
}

func changeCredentialOn(b backing, old, next fdsec.Credential, nextKeyfile bool) error {
	L, err := b.Size()
	if err != nil {
		return ioErr(err)
	}
	res, err := resolveHeaders(b, L)
	if err != nil {
		return err
	}
	h := res.hdr
	switch {
	case fixedCap(b) != 0 && h.PhysicalSize != uint64(L):
		return unsupportedf("the container was written for %d bytes and its partition is %d; its credential is not changed", h.PhysicalSize, L)
	case h.obfuscated():
		return usagef("this container is obfuscated, not encrypted: it opens without a credential, so it has none to change")
	case h.Profile == ProfileSealed:
		return usagef("a sealed container is never written again, its key slots included; seal a copy of the source under the new credential instead")
	case h.Flags&flagDigest != 0:
		return unsupportedf("the container carries a digest table this build does not maintain; it is not written")
	case h.VersionMinor > versionMinor:
		return unsupportedf("format version 1.%d is newer than this writer's 1.%d; its credential is changed by a newer build", h.VersionMinor, versionMinor)
	case len(next) == 0:
		return usagef("an encrypted container is never given the empty credential; the credential was not changed")
	}
	dataKey, opened, err := unlockSlots(b, &h, old)
	if err != nil {
		return err
	}
	defer clear(dataKey)
	free := -1
	for i := 0; i < slotCount && free < 0; i++ {
		free = i
		for _, o := range opened {
			if o == i {
				free = -1
				break
			}
		}
	}
	if free < 0 {
		return damagedf("every key slot opens under one credential, which no FileDO writer makes")
	}
	k, err := credKey(next, h.KDFSalt[:], h.KDFParamsID)
	if err != nil {
		return err
	}
	defer clear(k)
	slot, err := drawSlot(randSource, k, slotKindFor(nextKeyfile), dataKey)
	if err != nil {
		return err
	}
	if err := writeSlot(b, &h, free, slot); err != nil {
		return err
	}
	// Read back before anything is destroyed: the new slot has to open, and
	// to the same data key.
	region := make([]byte, slotRegionSize)
	if err := readFull(b, region, int64(h.SlotsOffset)); err != nil {
		return err
	}
	got, now, err := openSlotsAt(region, k, false)
	defer clear(got)
	if err != nil || !sameBytes(got, dataKey) || !containsIndex(now, free) {
		return damagedf("the new key slot did not read back; the old credential still opens the container")
	}
	for _, i := range opened {
		retired := make([]byte, slotSize)
		if _, err := io.ReadFull(randSource, retired); err != nil {
			return err
		}
		if err := writeSlot(b, &h, i, retired); err != nil {
			return err
		}
	}
	return nil
}

// writeSlot writes one 512-byte slot and flushes it. On a partition the slot
// region is written whole - the changed slot inside its unchanged neighbours -
// because a device with 4096-byte sectors refuses a 512-byte write
// (FDD-FORMAT section 3.1 rule 4). The order of slots and flushes is the same.
func writeSlot(b backing, h *header, i int, slot []byte) error {
	if fixedCap(b) != 0 {
		region := make([]byte, slotRegionSize)
		if err := readFull(b, region, int64(h.SlotsOffset)); err != nil {
			return err
		}
		copy(region[i*slotSize:], slot)
		if _, err := b.WriteAt(region, int64(h.SlotsOffset)); err != nil {
			return ioErr(err)
		}
		return ioErr(b.Sync())
	}
	if _, err := b.WriteAt(slot, int64(h.SlotsOffset)+int64(i)*slotSize); err != nil {
		return ioErr(err)
	}
	return ioErr(b.Sync())
}

func containsIndex(s []int, i int) bool {
	for _, v := range s {
		if v == i {
			return true
		}
	}
	return false
}
