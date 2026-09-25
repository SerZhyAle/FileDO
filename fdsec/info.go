package fdsec

import (
	"fmt"
	"io"
)

// HeaderInfo is the view of the container head that `fdsec info` reports
// (FDSEC-FORMAT.md section 4). It takes the credential like every other read:
// the head is masked, so there is nothing a credential-free look could report -
// not the version, not the suite, not that the file is a container at all. What
// stays sealed even here is the payload's own metadata: the true name, the real
// size and the timestamps.
type HeaderInfo struct {
	FormatVersion    uint8
	SuiteID          uint8
	ClusterAlignment uint32
	ChunkSize        uint32
	KDFMemoryKiB     uint32 // format constant of this version, not a file field
	KDFTime          uint32
	KDFLanes         uint8
	Threshold        uint32
	ContainerSize    int64

	// A directory container (suite 3) also reports its sealed totals once
	// the credential has authenticated: that it holds a folder, how many
	// entries, how many bytes. Before that it reports nothing.
	IsTree      bool
	TreeEntries int64
	TreeSize    int64
}

// ReadHeaderInfo opens the head of a container under cred and reports it. A
// wrong credential, a file that is not a container and a damaged head are one
// outcome, ErrCredentialOrTamper, and the message says so.
func ReadHeaderInfo(r io.ReadSeeker, cred Credential) (HeaderInfo, error) {
	var hi HeaderInfo
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return hi, fmt.Errorf("fdsec: seek container: %w", err)
	}
	c, err := Open(r, cred)
	if err != nil {
		return hi, err
	}
	if c.IsTree() {
		tm, err := c.TreeMetadata()
		if err != nil {
			return hi, err
		}
		hi.IsTree, hi.TreeEntries, hi.TreeSize = true, tm.Entries, tm.TotalSize
	}
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return hi, fmt.Errorf("fdsec: size container: %w", err)
	}
	hi.ContainerSize = size
	if c.s2 != nil {
		// Suite 2 has no head: the version is the document's, the frame is
		// the suite's constant, and the profile is the try-list entry that
		// authenticated. There is no cluster alignment and no fast branch.
		p := c.s2.profile
		hi.FormatVersion = FormatVersion
		hi.SuiteID = SuiteID2
		hi.ChunkSize = s2Frame
		hi.KDFMemoryKiB, hi.KDFTime, hi.KDFLanes = p.MemoryKiB, p.Time, p.Lanes
		return hi, nil
	}
	p := activeProfile
	hi.FormatVersion = c.h.Version
	hi.SuiteID = c.h.Suite
	hi.ClusterAlignment = c.h.ClusterAlignment
	hi.ChunkSize = c.h.ChunkSize
	hi.KDFMemoryKiB = p.MemoryKiB
	hi.KDFTime = p.Time
	hi.KDFLanes = p.Lanes
	hi.Threshold = p.Threshold
	hi.ContainerSize = size
	return hi, nil
}
