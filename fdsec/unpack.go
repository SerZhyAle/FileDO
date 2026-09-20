package fdsec

import (
	"fmt"
	"io"

	"golang.org/x/crypto/blake2b"
)

// Unpack streams the original file out of a suite-1 container, implementing
// the reader algorithm of FDSEC-FORMAT.md section 11, and returns the sealed
// metadata it recovered. The final digest check makes the round trip
// byte-exact: the recovered bytes must hash to the digest stored at pack time.
// Error classes follow section 12 and never fall through into each other.
func Unpack(dst io.Writer, src io.ReadSeeker, cred Credential) (Metadata, error) {
	var meta Metadata
	if _, err := src.Seek(0, io.SeekStart); err != nil {
		return meta, fmt.Errorf("fdsec: seek container: %w", err)
	}
	h, err := parseHeader(src)
	if err != nil {
		return meta, err
	}
	slots, err := parseSlots(src)
	if err != nil {
		return meta, err
	}

	// Unwrap slot 0: either authenticates or it does not - there is no partial
	// answer and no separate verifier (FDSEC-FORMAT.md sections 5, 12).
	kek := deriveKEK(cred, h)
	kekAEAD, err := newXAEAD(kek)
	if err != nil {
		return meta, err
	}
	var wrapNonce [nonceSize]byte
	copy(wrapNonce[:], slots[0].Payload[wrapNonceOff:nonceSize])
	var wrapped [wrappedSize]byte
	copy(wrapped[:], slots[0].Payload[nonceSize:nonceSize+wrappedSize])
	fileKey, fail := kekAEAD.Open(nil, wrapNonce[:], wrapped[:], adSlot0(h.HeaderDigest))
	if fail != nil {
		return meta, fmt.Errorf("%w: key slot 0 does not authenticate", ErrCredentialOrTamper)
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		return meta, err
	}

	// Sealed metadata.
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
	total, err := src.Seek(0, io.SeekEnd)
	if err != nil {
		return meta, fmt.Errorf("fdsec: size container: %w", err)
	}
	preLen := h.preLen()
	k := h.chunkCount(meta.Size)
	last := h.lastLen(meta.Size)
	lMin := preLen + (k-1)*int64(h.ChunkSize) + last + tagSize
	if total < lMin || total >= lMin+int64(h.ClusterAlignment) || total%int64(h.ClusterAlignment) != 0 {
		return meta, fmt.Errorf("%w: file length %d is impossible for size %d (want %d..%d, cluster-aligned)", ErrDamaged, total, meta.Size, lMin, lMin+int64(h.ClusterAlignment)-1)
	}

	// Chunks, in order, each opened with its own derived nonce and AD.
	dh, err := blake2b.New256(nil)
	if err != nil {
		return meta, err
	}
	capacity := h.chunkCapacity()
	ct := make([]byte, capacity+tagSize)
	for i := int64(0); i < k; i++ {
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
	}
	if [digestSize]byte(dh.Sum(nil)) != digest {
		return meta, fmt.Errorf("%w: recovered bytes do not hash to the packed digest", ErrDamaged)
	}
	return meta, nil
}
