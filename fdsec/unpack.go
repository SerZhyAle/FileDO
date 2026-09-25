package fdsec

import (
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"math"

	"golang.org/x/crypto/blake2b"
)

// Container is a container that has authenticated under a credential
// (FDSEC-FORMAT.md section 11 steps 1 to 5, or section 18.6 steps 1 to 5).
// Only now is its suite known, so this is where a caller learns whether it
// holds one file (suite 1 or 2) or one directory tree (suite 3).
type Container struct {
	h       *header // suites 1 and 3
	fileKey []byte
	aead    cipher.AEAD
	s2      *suite2State // suite 2; h is nil
	src     io.ReadSeeker
}

// Open opens a container of any suite. Nothing on disk names the suite, so
// the order is the dispatch of FDSEC-FORMAT.md section 13.4: the head of
// suites 1 and 3 first, and only when it does not authenticate, suite 2's
// try-list. Either one authenticates or the outcome is the single one of
// section 12 - a wrong credential, not a container, or tampering - and a
// wrong credential therefore pays both derivations.
func Open(src io.ReadSeeker, cred Credential) (*Container, error) {
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("fdsec: seek container: %w", err)
	}
	h, fileKey, err := openHead(src, cred)
	if err != nil {
		if !errors.Is(err, ErrCredentialOrTamper) {
			return nil, err
		}
		total, serr := src.Seek(0, io.SeekEnd)
		if serr != nil {
			return nil, fmt.Errorf("fdsec: size container: %w", serr)
		}
		if !suite2LengthPossible(total) {
			return nil, err
		}
		st, err2 := openSuite2(src, cred)
		if err2 != nil {
			if errors.Is(err2, ErrCredentialOrTamper) {
				return nil, err
			}
			return nil, err2
		}
		return &Container{s2: st, src: src}, nil
	}
	a, err := newXAEAD(fileKey)
	if err != nil {
		return nil, err
	}
	return &Container{h: h, fileKey: fileKey, aead: a, src: src}, nil
}

// Suite is the authenticated suite id.
func (c *Container) Suite() uint8 {
	if c.s2 != nil {
		return SuiteID2
	}
	return c.h.Suite
}

// IsTree reports whether the container holds a directory tree (suite 3).
func (c *Container) IsTree() bool { return c.s2 == nil && c.h.Suite == SuiteID3 }

// Unpack streams the original file out of a suite-1 container, implementing
// the reader algorithm of FDSEC-FORMAT.md section 11, and returns the sealed
// metadata it recovered. The final digest check makes the round trip
// byte-exact: the recovered bytes must hash to the digest stored at pack time.
// Error classes follow section 12 and never fall through into each other. A
// directory container is refused with ErrTreeContainer, before a byte is
// written: it has no single file to stream.
func Unpack(dst io.Writer, src io.ReadSeeker, cred Credential, opts ...StreamOption) (Metadata, error) {
	c, err := Open(src, cred)
	if err != nil {
		return Metadata{}, err
	}
	return c.Unpack(dst, opts...)
}

// Unpack is the package-level Unpack for a container already opened.
func (c *Container) Unpack(dst io.Writer, opts ...StreamOption) (Metadata, error) {
	var meta Metadata
	if c.IsTree() {
		return meta, ErrTreeContainer
	}
	so := newStreamOpts(opts)
	if c.s2 != nil {
		meta, _, err := c.unpackSuite2(dst, so)
		return meta, err
	}
	h, fileKey, fileAEAD, src := c.h, c.fileKey, c.aead, c.src

	// Sealed metadata.
	if _, err := src.Seek(preMeta, io.SeekStart); err != nil {
		return meta, fmt.Errorf("fdsec: seek metadata: %w", err)
	}
	sealedMeta := make([]byte, metaSize)
	if _, err := io.ReadFull(src, sealedMeta); err != nil {
		return meta, fmt.Errorf("%w: metadata unreadable (%v)", ErrDamaged, err)
	}
	metaNonce, err := deriveNonce(fileKey, metaCtx())
	if err != nil {
		return meta, err
	}
	metaPlain, fail := fileAEAD.Open(nil, metaNonce, sealedMeta, adMeta(h.HeaderDigest))
	if fail != nil {
		return meta, fmt.Errorf("%w: metadata does not authenticate", ErrCredentialOrTamper)
	}
	meta, digest, err := parseMetadataBlock(metaPlain)
	if err != nil {
		return meta, err
	}

	// Length arithmetic: the file must be the unique multiple of the cluster
	// alignment in [L_min, L_min + A) (FDSEC-FORMAT.md sections 9, 11 step 7).
	if meta.Size < 0 {
		return meta, fmt.Errorf("%w: negative size %d", ErrDamaged, meta.Size)
	}
	total, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return meta, fmt.Errorf("fdsec: size container: %w", err)
	}
	preLen := h.preLen()
	k := h.chunkCount(meta.Size)
	last := h.lastLen(meta.Size)
	chunkSize := int64(h.ChunkSize)
	if k > 1 && (math.MaxInt64-preLen-last-tagSize-int64(h.ClusterAlignment))/chunkSize < (k-1) {
		return meta, fmt.Errorf("%w: sealed size %d causes length overflow", ErrDamaged, meta.Size)
	}
	lMin := preLen + (k-1)*chunkSize + last + tagSize
	if total < lMin || total >= lMin+int64(h.ClusterAlignment) || total%int64(h.ClusterAlignment) != 0 {
		return meta, fmt.Errorf("%w: file length %d is impossible for size %d (want %d..%d, cluster-aligned)", ErrDamaged, total, meta.Size, lMin, lMin+int64(h.ClusterAlignment)-1)
	}

	// Chunks, in order, each opened with its own derived nonce and AD.
	dh, err := blake2b.New256(nil)
	if err != nil {
		return meta, err
	}
	capacity := h.chunkCapacity()
	// The buffer holds one sealed chunk, and never more than the container can
	// physically hold: the length check above bounds a multi-chunk body by the
	// file size, and a one-chunk body needs only its own length. A forged
	// header with a 4 GiB chunk size and a 1-byte payload therefore costs
	// bytes, not gigabytes (FDSEC-10).
	ct, err := chunkBuffer(k, capacity, last)
	if err != nil {
		return meta, err
	}
	for i := int64(0); i < k; i++ {
		if so.stopped() {
			return meta, ErrStopped
		}
		want := capacity + tagSize
		if i == k-1 {
			want = last + tagSize
		}
		if _, err := src.Seek(preLen+i*int64(h.ChunkSize), io.SeekStart); err != nil {
			return meta, fmt.Errorf("fdsec: seek chunk %d: %w", i, err)
		}
		if _, err := io.ReadFull(src, ct[:want]); err != nil {
			return meta, fmt.Errorf("%w: chunk %d unreadable (%v)", ErrDamaged, i, err)
		}
		nonce, err := deriveNonce(fileKey, string(chunkCtx(i)))
		if err != nil {
			return meta, err
		}
		pt, fail := fileAEAD.Open(nil, nonce, ct[:want], adChunk(h.HeaderDigest, i, k, i == k-1, want-tagSize))
		if fail != nil {
			return meta, fmt.Errorf("%w: chunk %d does not authenticate", ErrCredentialOrTamper, i)
		}
		if _, err := dst.Write(pt); err != nil {
			return meta, fmt.Errorf("fdsec: write output: %w", err)
		}
		if _, err := dh.Write(pt); err != nil {
			return meta, err
		}
		if so.progress != nil {
			so.progress(int64(i)*capacity+int64(len(pt)), meta.Size)
		}
	}
	if [digestSize]byte(dh.Sum(nil)) != digest {
		return meta, fmt.Errorf("%w: recovered bytes do not hash to the packed digest", ErrDamaged)
	}
	return meta, nil
}
