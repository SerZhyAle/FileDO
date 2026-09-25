package fdsec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
	"strings"
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
	h, _, err := openHead(bytes.NewReader(raw), NewCredential("pw123"))
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
		if !strings.Contains(err.Error(), "chunk 0") {
			t.Fatalf("error %v does not name chunk index", err)
		}
	})

	t.Run("flipped chunk 1 payload byte is class A naming chunk 1", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[preLen+slot+100] ^= 0x01
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
		if !strings.Contains(err.Error(), "chunk 1") {
			t.Fatalf("error %v does not name chunk 1", err)
		}
	})

	t.Run("flipped sealed-metadata byte is class A", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[preMeta+100] ^= 0x01
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
	})

	// A head byte is either the salt (which changes every derived key) or a
	// masked, digest-bound parameter byte. Either way nothing downstream
	// authenticates, and the outcome is the merged class A - the format cannot
	// tell head damage from a wrong credential, and says so (FDSEC-FORMAT.md
	// section 12).
	t.Run("flipped salt byte is class A", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[10] ^= 0x01
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
	})

	t.Run("flipped masked parameter byte is class A", func(t *testing.T) {
		b := bytes.Clone(raw)
		b[saltSize+4] ^= 0x01
		_, _, err := unpackContainer(b, cred)
		assertClass(t, "A", err)
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

// rewriteHeader rewrites a finished container the way another writer would
// have written it: the header field is changed, the header digest recomputed,
// and every AEAD output that binds that digest - slot 0, the sealed metadata,
// every chunk - re-sealed under the new associated data.
//
// Nothing weaker would do. The head is masked and the digest is bound into
// everything, so a plain byte edit never reaches the header validation: it
// fails as class A first (TestTamper_Classes covers that). A container that
// claims an unknown version or suite can only come from a writer that meant
// it, and that is what this builds - a future writer's file, offered to
// today's reader.
func rewriteHeader(t *testing.T, raw []byte, cred Credential, mutate func(*header)) []byte {
	t.Helper()
	orig, fileKey, err := openHead(bytes.NewReader(raw), cred)
	if err != nil {
		t.Fatal(err)
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		t.Fatal(err)
	}

	forged := *orig
	mutate(&forged)
	forged.setDigest()

	// The sealed metadata and the chunks keep their layout - only their
	// associated data changes - so the original header drives the arithmetic.
	metaNonce, err := deriveNonce(fileKey, metaCtx())
	if err != nil {
		t.Fatal(err)
	}
	metaPT, fail := fileAEAD.Open(nil, metaNonce, raw[preMeta:preMeta+metaSize], adMeta(orig.HeaderDigest))
	if fail != nil {
		t.Fatal(fail)
	}
	meta, _, err := parseMetadataBlock(metaPT)
	if err != nil {
		t.Fatal(err)
	}

	maskKey, kek, err := deriveKeys(cred, orig.Salt[:])
	if err != nil {
		t.Fatal(err)
	}
	kekAEAD, err := newXAEAD(kek)
	if err != nil {
		t.Fatal(err)
	}
	wrapNonce, err := randBytes(nonceSize)
	if err != nil {
		t.Fatal(err)
	}
	var slot0 slotRecord
	slot0.Type = typeCredWrap
	copy(slot0.Payload[wrapNonceOff:], wrapNonce)
	copy(slot0.Payload[nonceSize:], kekAEAD.Seal(nil, wrapNonce, fileKey, adSlot0(forged.HeaderDigest)))
	head, err := buildHead(&forged, &slot0, maskKey)
	if err != nil {
		t.Fatal(err)
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
			t.Fatal(err)
		}
		pt, fail := fileAEAD.Open(nil, nonce, b[off:off+n+tagSize], adChunk(orig.HeaderDigest, i, k, i == k-1, n))
		if fail != nil {
			t.Fatalf("chunk %d does not reopen: %v", i, fail)
		}
		copy(b[off:], fileAEAD.Seal(nil, nonce, pt, adChunk(forged.HeaderDigest, i, k, i == k-1, n)))
	}
	return b
}

func TestTamper_EditedHeaderFields(t *testing.T) {
	raw, _, _, _ := tampered(t)
	cred := NewCredential("pw123")

	t.Run("unknown suite refused by name", func(t *testing.T) {
		b := rewriteHeader(t, raw, cred, func(h *header) { h.Suite = 4 })
		_, _, err := unpackContainer(b, cred)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("want unsupported, got %v", err)
		}
		if got := err.Error(); !bytes.Contains([]byte(got), []byte("suite id 4")) {
			t.Fatalf("refusal does not name the suite: %v", err)
		}
	})

	t.Run("unknown version refused", func(t *testing.T) {
		b := rewriteHeader(t, raw, cred, func(h *header) { h.Version = 2 })
		assertClass(t, "unsupported", mustUnpackErr(t, b, cred))
	})

	t.Run("unknown flag bit refused", func(t *testing.T) {
		b := rewriteHeader(t, raw, cred, func(h *header) { h.Flags = 0x0003 })
		assertClass(t, "unsupported", mustUnpackErr(t, b, cred))
	})

	// The forgery is only as good as its own round trip: an untouched rewrite
	// must still open, or the refusals above would prove nothing.
	t.Run("an unmutated rewrite still opens", func(t *testing.T) {
		b := rewriteHeader(t, raw, cred, func(h *header) {})
		if _, _, err := unpackContainer(b, cred); err != nil {
			t.Fatalf("the rewrite helper does not produce a readable container: %v", err)
		}
	})

	t.Run("reserved slot type refused by name", func(t *testing.T) {
		// Slot 1's type byte is masked and its clear value is typeEmpty (0), so
		// XOR - not assignment - is what makes it read back as typeRecovery.
		// Assigning left it at 2 XOR the mask byte, which is 0 once in 256
		// containers: the test passed a blank slot and failed at random.
		b := bytes.Clone(raw)
		b[headerSize+slotSize] ^= typeRecovery // slot 1
		_, _, err := unpackContainer(b, cred)
		if !errors.Is(err, ErrUnsupported) {
			t.Fatalf("want unsupported, got %v", err)
		}
	})

	t.Run("chunk size breaking alignment is damage", func(t *testing.T) {
		b := rewriteHeader(t, raw, cred, func(h *header) { h.ChunkSize = h.ChunkSize + 1 })
		assertClass(t, "B", mustUnpackErr(t, b, cred))
	})

	t.Run("cluster alignment that is not a power of two is damage", func(t *testing.T) {
		b := rewriteHeader(t, raw, cred, func(h *header) { h.ClusterAlignment = 513 })
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
// wrong credential is never reported as damage. The converse now holds only
// past the head: because the head carries no marker and no stored verifier,
// damage inside it is indistinguishable from a wrong credential and from a
// file that never was a container - one class, and the message says all three
// (FDSEC-FORMAT.md section 12).
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

	t.Run("head damage is class A, and the message names all three readings", func(t *testing.T) {
		b := bytes.Clone(raw)
		binary.LittleEndian.PutUint16(b[saltSize+2:saltSize+4], 0) // flip masked flag bytes
		_, _, err := unpackContainer(b, correct)
		assertClass(t, "A", err)
		for _, want := range []string{"wrong credential", "not a container", "tampered"} {
			if !bytes.Contains([]byte(err.Error()), []byte(want)) {
				t.Fatalf("the message does not mention %q: %v", want, err)
			}
		}
	})

	t.Run("damage past the head is never class A", func(t *testing.T) {
		// Truncation is the one kind of damage the container can still name:
		// the head opened, so the arithmetic is trustworthy.
		_, _, err := unpackContainer(raw[:len(raw)-int(fastParams().ClusterAlignment)], correct)
		assertClass(t, "B", err)
	})

	t.Run("random bytes are class A, not a crash", func(t *testing.T) {
		_, _, err := unpackContainer(randContent(t, len(raw)), correct)
		assertClass(t, "A", err)
	})
}

func rewriteMetadata(t *testing.T, raw []byte, cred Credential, forge func(origMeta Metadata, origDigest [32]byte) []byte) []byte {
	t.Helper()
	orig, fileKey, err := openHead(bytes.NewReader(raw), cred)
	if err != nil {
		t.Fatal(err)
	}
	fileAEAD, err := newXAEAD(fileKey)
	if err != nil {
		t.Fatal(err)
	}
	metaNonce, err := deriveNonce(fileKey, metaCtx())
	if err != nil {
		t.Fatal(err)
	}
	metaPT, fail := fileAEAD.Open(nil, metaNonce, raw[preMeta:preMeta+metaSize], adMeta(orig.HeaderDigest))
	if fail != nil {
		t.Fatal(fail)
	}
	meta, digest, err := parseMetadataBlock(metaPT)
	if err != nil {
		t.Fatal(err)
	}
	newMeta := forge(meta, digest)
	b := bytes.Clone(raw)
	copy(b[preMeta:preMeta+metaSize], fileAEAD.Seal(nil, metaNonce, newMeta, adMeta(orig.HeaderDigest)))
	return b
}

func TestTamper_ForgedMetadata(t *testing.T) {
	raw, _, _, _ := tampered(t)
	cred := NewCredential("pw123")

	forgedNames := []string{`..\x`, `a/b`, `c:x`, `CON`, `x.`}
	for _, badName := range forgedNames {
		t.Run("forged name "+badName+" is damage", func(t *testing.T) {
			b := rewriteMetadata(t, raw, cred, func(origMeta Metadata, origDigest [32]byte) []byte {
				block := make([]byte, metaPlain)
				nameBytes := []byte(badName)
				binary.LittleEndian.PutUint16(block[0:2], uint16(len(nameBytes)))
				copy(block[2:], nameBytes)
				o := 2 + len(nameBytes)
				binary.LittleEndian.PutUint64(block[o:], uint64(origMeta.Size))
				copy(block[o+40:], origDigest[:])
				return block
			})
			_, _, err := unpackContainer(b, cred)
			assertClass(t, "B", err)
		})
	}

	forgedSizes := map[string]uint64{
		"2^63":   1 << 63,
		"2^64-1": math.MaxUint64,
		"2^63-1": math.MaxInt64,
	}
	for name, badSize := range forgedSizes {
		t.Run("forged size "+name+" is damage", func(t *testing.T) {
			b := rewriteMetadata(t, raw, cred, func(origMeta Metadata, origDigest [32]byte) []byte {
				block := make([]byte, metaPlain)
				nameBytes := []byte(origMeta.Name)
				binary.LittleEndian.PutUint16(block[0:2], uint16(len(nameBytes)))
				copy(block[2:], nameBytes)
				o := 2 + len(nameBytes)
				binary.LittleEndian.PutUint64(block[o:], badSize)
				copy(block[o+40:], origDigest[:])
				return block
			})
			_, _, err := unpackContainer(b, cred)
			assertClass(t, "B", err)
		})
	}
}
