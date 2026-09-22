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
}

// ReadHeaderInfo opens the head of a container under cred and reports it. A
// wrong credential, a file that is not a container and a damaged head are one
// outcome, ErrCredentialOrTamper, and the message says so.
func ReadHeaderInfo(r io.ReadSeeker, cred Credential) (HeaderInfo, error) {
	var hi HeaderInfo
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return hi, fmt.Errorf("fdsec: seek container: %w", err)
	}
	h, _, err := openHead(r, cred)
	if err != nil {
		return hi, err
	}
	size, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return hi, fmt.Errorf("fdsec: size container: %w", err)
	}
	p := activeProfile
	return HeaderInfo{
		FormatVersion:    h.Version,
		SuiteID:          h.Suite,
		ClusterAlignment: h.ClusterAlignment,
		ChunkSize:        h.ChunkSize,
		KDFMemoryKiB:     p.MemoryKiB,
		KDFTime:          p.Time,
		KDFLanes:         p.Lanes,
		Threshold:        p.Threshold,
		ContainerSize:    size,
	}, nil
}
