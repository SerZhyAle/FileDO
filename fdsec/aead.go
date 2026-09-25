package fdsec

import (
	"crypto/cipher"
	"encoding/binary"

	"golang.org/x/crypto/chacha20poly1305"
)

// AEAD wiring and the two derivations fixed by FDSEC-FORMAT.md section 8:
// chunk and metadata nonces are derived from the file key (never stored - a
// stored nonce would break the cluster alignment), and every AEAD call carries
// a domain-separated associated-data string bound to the header digest.

const (
	adPrefixSlot0 = "FDSEC1/slot0"
	adPrefixMeta  = "FDSEC1/meta"
	adPrefixChunk = "FDSEC1/chunk"
	noncePrefix   = "FDSEC1/nonce/"

	// Suite 3 (FDSEC-FORMAT.md sections 17.3, 17.5). The head is suite 1's,
	// so slot 0 keeps its "FDSEC1/slot0" binding and the nonce derivation
	// keeps its prefix; only what is sealed after the head is new.
	adPrefixDir      = "FDSEC3/dir"
	adPrefixManifest = "FDSEC3/manifest"
	adPrefixEntry    = "FDSEC3/entry"
)

func newXAEAD(key []byte) (cipher.AEAD, error) {
	return chacha20poly1305.NewX(key)
}

// deriveNonce = BLAKE2b-256(key = file_key, msg = "FDSEC1/nonce/" || ctx)[0:24]
// (FDSEC-FORMAT.md section 8, "Nonce derivation").
func deriveNonce(fileKey []byte, ctx string) ([]byte, error) {
	h, err := newDigest(fileKey)
	if err != nil {
		return nil, err
	}
	h.Write([]byte(noncePrefix))
	h.Write([]byte(ctx))
	return h.Sum(nil)[:nonceSize], nil
}

func metaCtx() string { return "meta" }

// chunkCtx is "chunk:" || u64le(i).
func chunkCtx(i int64) []byte {
	b := make([]byte, 0, len("chunk:")+8)
	b = append(b, "chunk:"...)
	b = binary.LittleEndian.AppendUint64(b, uint64(i))
	return b
}

func adSlot0(digest [digestSize]byte) []byte {
	return append([]byte(adPrefixSlot0), digest[:]...)
}

func adMeta(digest [digestSize]byte) []byte {
	return append([]byte(adPrefixMeta), digest[:]...)
}

// adChunk = "FDSEC1/chunk" || header_digest || u64le(i) || u64le(k) || u8(last)
// || u64le(len_i) (FDSEC-FORMAT.md section 8, "Chunk sealing"). The index makes
// reordering detectable, the digest makes splicing between containers
// detectable, the last flag and length make truncation detectable.
func adChunk(digest [digestSize]byte, i, k int64, last bool, n int64) []byte {
	b := make([]byte, 0, len(adPrefixChunk)+digestSize+8+8+1+8)
	b = append(b, adPrefixChunk...)
	b = append(b, digest[:]...)
	b = binary.LittleEndian.AppendUint64(b, uint64(i))
	b = binary.LittleEndian.AppendUint64(b, uint64(k))
	if last {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	b = binary.LittleEndian.AppendUint64(b, uint64(n))
	return b
}
