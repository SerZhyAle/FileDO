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

// deriveKEK derives the key-encryption key from the credential, the header salt
// and the threshold branch (FDSEC-FORMAT.md section 6):
//
//	len <  threshold  (slow)  Argon2id(credential, salt, T, M KiB, P, 32)
//	len >= threshold  (fast)  BLAKE2b-512(salt || credential)[0:32]
//
// The threshold is at least 1, so an empty credential always takes the slow
// branch. Which branch ran is stored nowhere.
func deriveKEK(cred Credential, h *header) []byte {
	if len(cred) < int(h.Threshold) {
		return argon2.IDKey(cred, h.Salt[:], h.KDFTime, h.KDFMemoryKiB, h.KDFLanes, fileKeySize)
	}
	s := blake2b.Sum512(append(append([]byte{}, h.Salt[:]...), cred...))
	return s[:fileKeySize]
}
