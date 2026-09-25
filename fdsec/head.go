package fdsec

import (
	"fmt"
	"io"
)

// The head is the first 668 bytes of a container: the clear salt, the masked
// parameter block and the masked key slots (FDSEC-FORMAT.md sections 3-5).
// Writing and opening it live together here because the two are one contract:
// what the writer masks, the reader unmasks in the same order, and the reader
// interprets nothing until slot 0 has authenticated.

// ScreenLength reports whether a file of this length could be a container at
// all. It is the whole of what a caller can screen without the credential: a
// container carries no marker, so only its length is readable from outside.
// A container of suite 1 or 3 is a whole number of clusters (FDSEC-FORMAT.md
// section 9) and holds at least a head, the sealed metadata and one empty
// chunk; a suite-2 file has no alignment but is at least 9308 bytes and never
// ends in a frame too short for a tag and a byte (section 18.4). A file that
// fits neither shape is certainly not a container - and a file that fits one
// still says nothing.
func ScreenLength(n int64) error {
	if suite2LengthPossible(n) {
		return nil
	}
	const minAlign = 512
	minPre := (int64(preMeta+metaSize) + minAlign - 1) / minAlign * minAlign
	minTotal := (minPre + tagSize + minAlign - 1) / minAlign * minAlign
	switch {
	case n < minTotal:
		return fmt.Errorf("%w: %d bytes is smaller than the smallest possible container (%d bytes)", ErrDamaged, n, minTotal)
	case n%minAlign != 0:
		return fmt.Errorf("%w: %d bytes is neither a whole number of %d-byte clusters nor a length a suite-2 container can have", ErrDamaged, n, minAlign)
	}
	return nil
}

// buildHead lays out the head and masks everything after the salt.
func buildHead(h *header, slot0 *slotRecord, maskKey []byte) ([]byte, error) {
	b := make([]byte, 0, preMeta)
	b = append(b, h.marshal()...)
	b = append(b, marshalSlots(slot0)...)
	if len(b) != preMeta || len(b[maskOff:]) != maskLen {
		return nil, fmt.Errorf("fdsec: internal: head is %d bytes, want %d", len(b), preMeta)
	}
	if err := mask(maskKey, b[maskOff:]); err != nil {
		return nil, err
	}
	return b, nil
}

// openHead reads the head, unmasks it under the credential and unwraps the file
// key from slot 0 - the reader algorithm of FDSEC-FORMAT.md section 11, steps
// 1 to 5.
//
// Until slot 0 authenticates, not one byte of the head means anything: a wrong
// credential, a file that was never a container and a damaged head all produce
// the same noise and therefore the same single outcome (section 12). Only after
// the tag holds are the header and the slots interpreted, and only then can an
// unknown version or a broken structural rule be reported as what it is.
func openHead(r io.Reader, cred Credential) (*header, []byte, error) {
	b := make([]byte, preMeta)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, nil, fmt.Errorf("%w: file is too short to be a container (%v)", ErrDamaged, err)
	}
	maskKey, kek, err := deriveKeys(cred, b[:saltSize])
	if err != nil {
		return nil, nil, err
	}
	defer clear(maskKey) // FDSEC-15
	defer clear(kek)
	if err := mask(maskKey, b[maskOff:]); err != nil {
		return nil, nil, err
	}

	h := &header{}
	h.unmarshal(b[:headerSize])
	h.setDigest()
	slots := parseSlots(b[headerSize:])

	kekAEAD, err := newXAEAD(kek)
	if err != nil {
		return nil, nil, err
	}
	wrapNonce := slots[0].Payload[wrapNonceOff:nonceSize]
	wrapped := slots[0].Payload[nonceSize : nonceSize+wrappedSize]
	fileKey, fail := kekAEAD.Open(nil, wrapNonce, wrapped, adSlot0(h.HeaderDigest))
	if fail != nil {
		return nil, nil, fmt.Errorf("%w: key slot 0 does not authenticate", ErrCredentialOrTamper)
	}

	// Authenticated: from here the bytes are ours and the outcome classes part
	// company again (FDSEC-FORMAT.md section 12).
	if err := validateHeader(h); err != nil {
		return nil, nil, err
	}
	if err := validateSlots(slots); err != nil {
		return nil, nil, err
	}
	return h, fileKey, nil
}
