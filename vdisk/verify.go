package vdisk

import (
	"context"
	"fmt"
	"math"
)

// VerifyReport is what Verify found. Problems are damage; Notes are facts a
// user should know that are not damage.
type VerifyReport struct {
	Protection        string // "obfuscated" or "encrypted"
	FromBackup        bool   // the primary header did not open
	Backup            string // "current", "older" or "unreadable"
	Clean             bool
	SaveInProgress    bool
	MapGeneration     uint64
	AllocatedClusters int64
	ClustersRead      int64
	DataVerified      bool // false in format 1.0: there is no digest table to check the data against
	Notes             []string
	Problems          []string
}

// Verify checks what the file holds now, from the file: the header pair, the
// key slots, the active map, and that every referenced cluster can be read.
// It changes nothing. Format 1.0 reserves the digest table (Q5) but defines no
// layout for it, and XTS detects nothing, so the data region is read and not
// verified - the report says so rather than passing it (FDD-FORMAT sections 9
// and 13.3). Damage found is returned as ErrDamaged with the report.
func (c *Container) Verify(ctx context.Context, p ProgressSink) (VerifyReport, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var r VerifyReport
	if err := c.usable(false); err != nil {
		return r, err
	}
	L, err := c.b.Size()
	if err != nil {
		return r, ioErr(err)
	}
	res, err := resolveHeaders(c.b, L)
	if err != nil {
		return r, err
	}
	h := res.hdr
	r.Protection = res.info(L).Protection()
	r.FromBackup, r.Backup = res.fromBackup, res.backup
	r.Clean, r.SaveInProgress = h.Clean == 1, h.SaveInProgress == 1
	r.MapGeneration = h.MapGeneration
	r.AllocatedClusters = int64(h.AllocatedClusters)
	switch {
	case res.fromBackup:
		r.Problems = append(r.Problems, "the primary header does not open; the backup carried the container (the next writer rebuilds the primary)")
	case res.backup == "unreadable":
		r.Problems = append(r.Problems, "the backup header does not open (the next writer rewrites it)")
	case res.backup == "older":
		r.Notes = append(r.Notes, "the backup header is older than the primary, as a write cut between the two leaves it (the next writer rewrites it)")
	}
	if h.SaveInProgress == 1 {
		r.Notes = append(r.Notes, "a ram save was interrupted: the volume may hold a mixture of two states")
	} else if h.Clean == 0 {
		r.Notes = append(r.Notes, "the container was not closed cleanly, or is in use")
	}

	region := make([]byte, slotRegionSize)
	if err := readFull(c.b, region, int64(h.SlotsOffset)); err != nil {
		return r, err
	}
	k := obfKey(h.KDFSalt[:])
	if h.obfuscated() {
		if _, err := openSlots(region, k[:], true); err != nil {
			r.Problems = append(r.Problems, err.Error())
		}
	} else {
		r.Notes = append(r.Notes, "the key slots of an encrypted container are checked when it is opened with its credential")
	}
	m, _ := mapSize(h.ClusterCount)
	if m > math.MaxInt32 {
		return r, unsupportedf("the cluster map is %d bytes, more than this build can hold", m)
	}
	copyBytes := make([]byte, m)
	if err := readFull(c.b, copyBytes, int64(h.mapCopyOffset(h.MapActive))); err != nil {
		return r, err
	}
	entries, err := checkMap(&h, copyBytes, uint64(L))
	if err != nil {
		r.Problems = append(r.Problems, err.Error())
	} else {
		cs := int64(h.clusterSize())
		buf := make([]byte, cs)
		total := int64(h.AllocatedClusters)
		for _, e := range entries {
			if e == sentinel {
				continue
			}
			if stopped(ctx) {
				return r, ErrStopped
			}
			if err := readFull(c.b, buf, int64(h.DataOffset)+int64(e)*cs); err != nil {
				return r, err
			}
			r.ClustersRead++
			report(p, r.ClustersRead, total)
		}
	}
	if h.Flags&flagDigest != 0 {
		r.Notes = append(r.Notes, "the container carries a digest table this build cannot read")
	}
	r.Notes = append(r.Notes, "format 1.0 records no data digests: the data region was read, not verified")
	if len(r.Problems) > 0 {
		return r, fmt.Errorf("%w: %d problem(s): %s", ErrDamaged, len(r.Problems), r.Problems[0])
	}
	return r, nil
}
