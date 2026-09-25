package fdsec

import (
	"crypto/rand"
	"io"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/text/unicode/norm"
)

// randSource is the package randomness seam (FDSEC-FORMAT.md section 1: the OS
// CSPRNG). Tests and the vector generator substitute a fixed reader; nothing
// else may touch it.
var randSource io.Reader = rand.Reader

func randBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(randSource, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Credential is a normalized credential (FDSEC-FORMAT.md section 6): NFC, UTF-8,
// no trimming - a leading or trailing space is part of the credential. Any
// length, including zero, is accepted; an empty credential yields a container
// that is obfuscation with no secrecy.
type Credential []byte

// NewCredential normalizes s into a Credential.
func NewCredential(s string) Credential {
	return Credential(norm.NFC.String(s))
}

func (c Credential) String() string { return string(c) }

// kdfProfile is the work factor of one format version (FDSEC-FORMAT.md
// section 6). It is deliberately NOT a header field: the header is masked with
// a credential-derived keystream, so the reader has to derive before it can
// read, and a work factor it could read first would have to sit in the clear
// and name the format. The profile therefore belongs to the format version,
// and a harder profile is a new version, not a per-file choice.
type kdfProfile struct {
	MemoryKiB uint32 // Argon2id memory, slow branch
	Time      uint32 // Argon2id iterations, slow branch
	Lanes     uint8  // Argon2id parallelism, slow branch
	Threshold uint32 // credential length in UTF-8 bytes at which the fast branch takes over
}

// profileV1 is the profile of format version 1, the only one defined.
var profileV1 = kdfProfile{MemoryKiB: 65536, Time: 3, Lanes: 4, Threshold: 64}

// activeProfile is the profile every shipped path uses. It is a package seam of
// the same kind as randSource: a test may lower the work factor so a corpus of
// hundreds of containers stays quick, and nothing else may assign it. A
// container written under a lowered profile is readable only under that same
// profile, which is exactly why no shipped path touches this.
var activeProfile = profileV1

// deriveRoot derives the root key from the credential and the container salt
// (FDSEC-FORMAT.md section 6), on the branch the credential's length picks:
//
//	len <  threshold  (slow)  Argon2id(credential, salt, T, M KiB, P, 32)
//	len >= threshold  (fast)  BLAKE2b-512(salt || credential)[0:32]
//
// The threshold is at least 1, so an empty credential always takes the slow
// branch. Which branch ran is stored nowhere.
func deriveRoot(cred Credential, salt []byte) []byte {
	p := activeProfile
	if len(cred) < int(p.Threshold) {
		return argon2.IDKey(cred, salt, p.Time, p.MemoryKiB, p.Lanes, fileKeySize)
	}
	s := blake2b.Sum512(append(append([]byte{}, salt...), cred...))
	return s[:fileKeySize]
}

// Domain separation of the two keys the root produces (FDSEC-FORMAT.md
// section 6). The mask key never touches an AEAD and the KEK never touches the
// keystream, so neither use can leak the other.
const (
	maskLabel = "FDSEC1/mask"
	kekLabel  = "FDSEC1/kek"
)

func subKey(root []byte, label string) ([]byte, error) {
	h, err := newDigest(root)
	if err != nil {
		return nil, err
	}
	h.Write([]byte(label))
	return h.Sum(nil), nil
}

// deriveKeys returns the mask key and the key-encryption key of one container.
func deriveKeys(cred Credential, salt []byte) (maskKey, kek []byte, err error) {
	root := deriveRoot(cred, salt)
	// Best-effort overwrite of key material once it is no longer needed
	// (FDSEC-BEHAVIOUR 8.2, FDSEC-15). Go may have copied it; this clears
	// the copy this package owns.
	defer clear(root)
	if maskKey, err = subKey(root, maskLabel); err != nil {
		return nil, nil, err
	}
	if kek, err = subKey(root, kekLabel); err != nil {
		return nil, nil, err
	}
	return maskKey, kek, nil
}
