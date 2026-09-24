package fdsec

import (
	"bytes"
	"encoding/binary"
)

// ForgeSuiteForTest rewrites a container with a mutated suite byte and valid AEAD tags.
// For testing unsupported suite refusals only.
func ForgeSuiteForTest(raw []byte, cred Credential, suite uint8) ([]byte, error) {
	orig, fileKey, err := openHead(bytes.NewReader(raw), cred)
	if err != nil {
		return nil, err
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		return nil, err
	}

	forged := *orig
	forged.Suite = suite
	forged.setDigest()

	metaNonce, err := deriveNonce(fileKey, metaCtx())
	if err != nil {
		return nil, err
	}
	metaPT, fail := fileAEAD.Open(nil, metaNonce, raw[preMeta:preMeta+metaSize], adMeta(orig.HeaderDigest))
	if fail != nil {
		return nil, fail
	}
	meta, _, err := parseMetadataBlock(metaPT)
	if err != nil {
		return nil, err
	}

	maskKey, kek, err := deriveKeys(cred, orig.Salt[:])
	if err != nil {
		return nil, err
	}
	kekAEAD, err := newXAEAD(kek)
	if err != nil {
		return nil, err
	}
	wrapNonce, err := randBytes(nonceSize)
	if err != nil {
		return nil, err
	}
	var slot0 slotRecord
	slot0.Type = typeCredWrap
	copy(slot0.Payload[wrapNonceOff:], wrapNonce)
	copy(slot0.Payload[nonceSize:], kekAEAD.Seal(nil, wrapNonce, fileKey, adSlot0(forged.HeaderDigest)))
	head, err := buildHead(&forged, &slot0, maskKey)
	if err != nil {
		return nil, err
	}

	b := bytes.Clone(raw)
	copy(b[:preMeta], head)
	copy(b[preMeta:], fileAEAD.Seal(nil, metaNonce, metaPT, adMeta(forged.HeaderDigest)))

	preLen := orig.preLen()
	k := orig.chunkCount(meta.Size)
	last := orig.lastLen(meta.Size)
	for i := int64(0); i < k; i++ {
		n := orig.chunkCapacity()
		if i == k-1 {
			n = last
		}
		off := preLen + i*int64(orig.ChunkSize)
		nonce, err := deriveNonce(fileKey, string(chunkCtx(i)))
		if err != nil {
			return nil, err
		}
		pt, fail := fileAEAD.Open(nil, nonce, b[off:off+n+tagSize], adChunk(orig.HeaderDigest, i, k, i == k-1, n))
		if fail != nil {
			return nil, fail
		}
		copy(b[off:], fileAEAD.Seal(nil, nonce, pt, adChunk(forged.HeaderDigest, i, k, i == k-1, n)))
	}
	return b, nil
}

// ForgeMetadataNameForTest rewrites a container with an invalid/forged sealed true name.
// For testing damage refusal on illegal sealed names.
func ForgeMetadataNameForTest(raw []byte, cred Credential, forgedName string) ([]byte, error) {
	orig, fileKey, err := openHead(bytes.NewReader(raw), cred)
	if err != nil {
		return nil, err
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		return nil, err
	}
	metaNonce, err := deriveNonce(fileKey, metaCtx())
	if err != nil {
		return nil, err
	}
	metaPT, fail := fileAEAD.Open(nil, metaNonce, raw[preMeta:preMeta+metaSize], adMeta(orig.HeaderDigest))
	if fail != nil {
		return nil, fail
	}
	meta, digest, err := parseMetadataBlock(metaPT)
	if err != nil {
		return nil, err
	}
	name := forgedName
	newBlock := make([]byte, metaPlain)
	binary.LittleEndian.PutUint16(newBlock[0:2], uint16(len(name)))
	copy(newBlock[2:], name)
	o := 2 + len(name)
	binary.LittleEndian.PutUint64(newBlock[o:], uint64(meta.Size))
	binary.LittleEndian.PutUint64(newBlock[o+8:], toFILETIME(meta.EncodedAt))
	binary.LittleEndian.PutUint64(newBlock[o+16:], toFILETIME(meta.CreatedAt))
	binary.LittleEndian.PutUint64(newBlock[o+24:], toFILETIME(meta.AccessedAt))
	binary.LittleEndian.PutUint64(newBlock[o+32:], toFILETIME(meta.ModifiedAt))
	copy(newBlock[o+40:], digest[:])
	off := o + 40 + digestSize
	pad, err := randBytes(metaPlain - off)
	if err != nil {
		return nil, err
	}
	copy(newBlock[off:], pad)

	b := bytes.Clone(raw)
	copy(b[preMeta:preMeta+metaSize], fileAEAD.Seal(nil, metaNonce, newBlock, adMeta(orig.HeaderDigest)))
	return b, nil
}
