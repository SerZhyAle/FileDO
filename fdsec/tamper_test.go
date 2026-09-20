package fdsec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// The tampering cases of stage S2 (tactics section 4). Each asserts the error
// CLASS, never the message text - except the suite refusal, which is asserted
// to name the suite it refuses.

// tampered builds one container up front (several chunks, k = 3) and returns
// it with its layout numbers.
func tampered(t *testing.T) (raw []byte, preLen int64, chunkSize int64, k int64) {
	t.Helper()
	p := fastParams() // alignment 512, slot 1024, capacity 1008
	content := make([]byte, 3*(int(p.ChunkSize)-tagSize)+37)
	raw = packBytes(t, content, fixedTimes, NewCredential("pw123"), p)
	h, err := parseHeader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return raw, h.preLen(), int64(h.ChunkSize), h.chunkCount(int64(len(content)))
}

func classOf(t *testing.T, err error) string {
	t.Helper()
	switch {
	case errors.Is(err, ErrCredentialOrTamper):
		return "A"
	case errors.Is(err, ErrDamaged):
		return "B"
	case errors.Is(err, ErrUnsupported):
		return "unsupported"
	default:
		t.Fatalf("error fits no class: %v", err)
		return ""
	}
}

func assertClass(t *testing.T, want string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("tampering not detected; want class %s", want)
	}
	if got := classOf(t, err); got != want {
		t.Fatalf("error class %s, want %s: %v", got, want, err)
	}
}

func TestTamper_Classes(t *testing.T) {
	raw, preLen, slot, _ := tampered(t)
	cred := NewCredential("pw123")

	t.Run("flipped payload byte is class A", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[preLen+100] ^= 0x01
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
	})

	t.Run("flipped header byte is class B", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[10] ^= 0x01
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "B", err)
	})

	t.Run("reordered chunks are class A", func(t *testing.T) {
		b := bytes.Clone(raw)
		c0 := bytes.Clone(b[preLen : preLen+slot])
		c1 := bytes.Clone(b[preLen+slot : preLen+2*slot])
		copy(b[preLen:], c1)
		copy(b[preLen+slot:], c0)
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
	})

	t.Run("truncated mid-header is class B", func(t *testing.T) {
		_, _, err := unpackContainer(raw[:40], cred)
		assertClass(t, "B", err)
	})

	t.Run("truncated mid-chunk is class B", func(t *testing.T) {
		_, _, err := unpackContainer(raw[:preLen+500], cred)
		assertClass(t, "B", err)
	})

	t.Run("dropped last chunk is class B", func(t *testing.T) {
		_, _, err := unpackContainer(raw[:preLen+2*slot], cred)
		assertClass(t, "B", err)
	})

	t.Run("dropped middle chunk (shifted) is class B", func(t *testing.T) {
		b := append(bytes.Clone(raw[:preLen+slot]), raw[preLen+2*slot:]...)
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "B", err)
	})

	t.Run("payload spliced between containers is detected", func(t *testing.T) {
		// Two containers of the same original, different salts/keys: swapping
		// chunk 0 of one into the other must fail (header digest in the AD).
		other := packBytes(t, make([]byte, 3*(1024-tagSize)+37), fixedTimes, NewCredential("pw123"), fastParams())
		b := bytes.Clone(raw)
		copy(b[preLen:preLen+slot], other[preLen:preLen+slot])
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
	})
}

// rewriteHeader mutates a field and recomputes the header digest, so the
// mutation is a deliberate, digest-consistent edit (tactics section 4,
// "edited header field").
func rewriteHeader(t *testing.T, raw []byte, mutate func(*header)) []byte {
	t.Helper()
	hr := bytes.NewReader(raw)
	h, err := parseHeader(hr)
	if err != nil {
		t.Fatal(err)
	}
	mutate(h)
	h.setDigest()
	b := bytes.Clone(raw)
	copy(b[:headerSize], h.marshal())
	return b
}

func TestTamper_EditedHeaderFields(t *testing.T) {
	raw, _, _, _ := tampered(t)
	cred := NewCredential("pw123")

	t.Run("unknown suite refused by name", func(t *testing.T) {
		b := rewriteHeader(t, raw, func(h *header) { h.Suite = 3 })
		_, _, err := unpackContainer(b, cred)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("want unsupported, got %v", err)
		}
		if got := err.Error(); !bytes.Contains([]byte(got), []byte("suite id 3")) {
			t.Fatalf("refusal does not name the suite: %v", err)
		}
	})

	t.Run("unknown version refused", func(t *testing.T) {
		b := rewriteHeader(t, raw, func(h *header) { h.Version = 2 })
		assertClass(t, "unsupported", mustUnpackErr(t, b, cred))
	})

	t.Run("unknown flag bit refused", func(t *testing.T) {
		b := rewriteHeader(t, raw, func(h *header) { h.Flags = 0x0003 })
		assertClass(t, "unsupported", mustUnpackErr(t, b, cred))
	})

	t.Run("reserved slot type refused by name", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[headerSize+slotSize] = typeRecovery // slot 1
		_, _, err := unpackContainer(b, cred)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("want unsupported, got %v", err)
		}
	})

	t.Run("chunk size breaking alignment is damage", func(t *testing.T) {
		b := rewriteHeader(t, raw, func(h *header) { h.ChunkSize = h.ChunkSize + 1 })
		assertClass(t, "B", mustUnpackErr(t, b, cred))
	})

	t.Run("KDF key length not 32 is damage", func(t *testing.T) {
		b := rewriteHeader(t, raw, func(h *header) { h.KDFKeyLen = 31 })
		assertClass(t, "B", mustUnpackErr(t, b, cred))
	})

	t.Run("zero threshold is damage", func(t *testing.T) {
		b := rewriteHeader(t, raw, func(h *header) { h.Threshold = 0 })
		assertClass(t, "B", mustUnpackErr(t, b, cred))
	})
}

func mustUnpackErr(t *testing.T, b []byte, cred Credential) error {
	t.Helper()
	_, _, err := unpackContainer(b, cred)
	if err == nil {
		t.Fatal("tampering not detected")
	}
	return err
}

// The invariant-9 property, its own test so a regression is unmissable: a
// wrong credential is never reported as damage, and damage is never reported
// as a wrong credential.
func TestWrongCredential_NeverDamage(t *testing.T) {
	raw, _, _, _ := tampered(t)
	correct := NewCredential("pw123")

	t.Run("wrong credential on intact file is class A", func(t *testing.T) {
		_, _, err := unpackContainer(raw, NewCredential("pw124"))
		assertClass(t, "A", err)
	})

	t.Run("empty credential packed, non-empty offered is class A", func(t *testing.T) {
		container := packBytes(t, []byte("obfuscated"), fixedTimes, NewCredential(""), fastParams())
		_, _, err := unpackContainer(container, NewCredential("x"))
		assertClass(t, "A", err)
	})

	t.Run("non-empty packed, empty offered is class A", func(t *testing.T) {
		container := packBytes(t, []byte("secret-ish"), fixedTimes, correct, fastParams())
		_, _, err := unpackContainer(container, NewCredential(""))
		assertClass(t, "A", err)
	})

	t.Run("corrupt container is never class A via the header path", func(t *testing.T) {
		b := bytes.Clone(raw)
		binary.LittleEndian.PutUint16(b[7:9], 0) // clear flags without fixing the digest
		_, _, err := unpackContainer(b, correct)
		assertClass(t, "B", err)
	})
}
