package vdisk

import (
	"bytes"
	"crypto/aes"
	"encoding/binary"
	"fmt"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/blake2b"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/xts"
)

// indepRead is a second reader of FDD-FORMAT, written from the document's
// reader algorithm (section 12.1) with the primitives it names and nothing
// else: it calls no function of this package, reads every field at the
// offset the document gives it, and uses golang.org/x/crypto/xts for the data
// layer where the package has its own implementation. The vectors are checked
// against it as well as against the package, so a vector cannot merely agree
// with the writer that made it.

var indepPepper, _ = hexBytes("b1d3a9054d75ef6bf721ac3898cdf9b11c4f25a361ce40c56d670decd603ffd1")

type indepResult struct {
	outcome       string // "opens", "damaged", "unsupported", "credential"
	reason        string
	headerSource  string // "primary" or "backup"
	primaryPlain  []byte
	backupPlain   []byte
	headerKeyP    []byte
	headerKeyB    []byte
	obfKey        []byte
	pwSeed        []byte
	credKey       []byte
	slotKeys      [][]byte
	dataKey       []byte
	activeMap     []byte
	allocated     [][2]uint64 // logical, physical
	logicalSize   uint64
	volume        []byte
	volumeBlake2b []byte
}

func indepKeyed(key []byte, parts ...[]byte) []byte {
	h, _ := blake2b.New256(key)
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func indepOpenHeader(block []byte, ad string) ([]byte, []byte, bool) {
	k := indepKeyed(indepPepper, block[0:32])
	a, _ := chacha20poly1305.NewX(k)
	pt, err := a.Open(nil, block[32:56], block[64:4096], []byte(ad))
	return pt, k, err == nil
}

func indepRead(f []byte) indepResult { return indepReadWith(f, nil) }

// indepReadWith reads with a credential, given as its UTF-8 bytes (the
// vectors' credentials are ASCII, so NFC is the identity on them).
func indepReadWith(f, cred []byte) (r indepResult) {
	fail := func(outcome, format string, args ...interface{}) indepResult {
		r.outcome, r.reason = outcome, fmt.Sprintf(format, args...)
		return r
	}
	L := uint64(len(f))
	le := binary.LittleEndian
	// Step 1.
	if L < 20480 {
		return fail("damaged", "shorter than 20480")
	}
	// Step 2: the headers (section 6.1).
	ptP, kP, okP := indepOpenHeader(f[0:4096], "FDD/v1/header")
	ptB, kB, okB := indepOpenHeader(f[L-4096:], "FDD/v1/header-backup")
	if okB {
		r.backupPlain, r.headerKeyB = ptB, kB
	}
	var pt []byte
	switch {
	case okP:
		pt, r.primaryPlain, r.headerKeyP, r.headerSource = ptP, ptP, kP, "primary"
	case okB:
		pt, r.headerSource = ptB, "backup"
	default:
		return fail("damaged", "neither header opens")
	}
	// Step 3: the header checks of section 5.2 that decide a vector.
	if string(pt[0:4]) != "FDDC" {
		return fail("damaged", "magic")
	}
	if le.Uint16(pt[4:]) != 1 {
		return fail("unsupported", "major")
	}
	flags := pt[9]
	sectorShift, clusterShift := le.Uint16(pt[10:]), le.Uint32(pt[12:])
	if pt[8] > 4 || flags&0xF5 != 0 || (sectorShift != 9 && sectorShift != 12) || clusterShift < 16 || clusterShift > 22 {
		return fail("unsupported", "profile, flags or geometry")
	}
	logical, clusterCount, allocated := le.Uint64(pt[16:]), le.Uint64(pt[32:]), le.Uint64(pt[40:])
	mapOffset, mapStride, mapActive := le.Uint64(pt[48:]), le.Uint64(pt[64:]), pt[80]
	dataOffset, slotsOffset := le.Uint64(pt[88:]), le.Uint64(pt[96:])
	C := uint64(1) << clusterShift
	S := uint64(1) << sectorShift
	M := (clusterCount*8 + 4095) &^ 4095
	if logical == 0 || logical%S != 0 || clusterCount != (logical+C-1)/C || allocated > clusterCount ||
		mapStride < M || dataOffset%max(1<<20, C) != 0 || mapOffset+mapStride+M > L-4096 || slotsOffset+4096 > mapOffset {
		return fail("damaged", "geometry")
	}
	r.logicalSize = logical
	// Step 4: K (section 8) - obf_key when flags bit 3 is set, else cred_key
	// from the credential under the kdf_params_id row of section 8.2.
	kdfSalt := pt[256:272]
	var K []byte
	if flags&0x08 != 0 {
		r.obfKey = indepKeyed(indepPepper, []byte("FDD/v1/obfuscation"), kdfSalt)
		K = r.obfKey
	} else {
		if le.Uint32(pt[248:]) != 1 || le.Uint32(pt[252:]) != 1 {
			return fail("unsupported", "kdf_id or kdf_params_id")
		}
		if len(cred) == 0 {
			return fail("credential", "encrypted, and no credential")
		}
		h, _ := blake2b.New512(indepPepper)
		h.Write(cred)
		r.pwSeed = h.Sum(nil)
		r.credKey = argon2.IDKey(r.pwSeed, kdfSalt, 3, 262144, 4, 32)
		K = r.credKey
	}
	// Step 5: all eight slots.
	for i := uint64(0); i < 8; i++ {
		s := f[slotsOffset+i*512:][:512]
		sk := indepKeyed(K, []byte("FDD/v1/slot-key"), s[0:16])
		r.slotKeys = append(r.slotKeys, sk)
		a, _ := chacha20poly1305.NewX(sk)
		spt, err := a.Open(nil, s[16:40], s[40:136], []byte("FDD/v1/slot"))
		if err != nil {
			continue
		}
		kindOK := spt[4] == 1
		if flags&0x08 == 0 {
			kindOK = spt[4] == 2 || spt[4] == 3
		}
		if string(spt[0:4]) != "SLOT" || !kindOK || (r.dataKey != nil && !bytes.Equal(r.dataKey, spt[8:72])) {
			return fail("damaged", "slot")
		}
		r.dataKey = spt[8:72]
	}
	if r.dataKey == nil && flags&0x08 == 0 {
		return fail("credential", "no slot opens under the credential")
	}
	if r.dataKey == nil {
		return fail("damaged", "no slot opens")
	}
	// Step 6: the active map (section 10.1).
	active := f[mapOffset+uint64(mapActive)*mapStride:][:M]
	if d := blake2b.Sum256(active); !bytes.Equal(d[:], pt[352:384]) {
		return fail("damaged", "map digest")
	}
	r.activeMap = active
	var P uint64
	if L-4096 > dataOffset {
		P = (L - 4096 - dataOffset) / C
	}
	seen := map[uint64]bool{}
	for c := uint64(0); c < clusterCount; c++ {
		e := le.Uint64(active[c*8:])
		if e == 0xFFFFFFFFFFFFFFFF {
			continue
		}
		if e >= P {
			return fail("damaged", "cluster %d past the end of the file", c)
		}
		if seen[e] {
			return fail("damaged", "physical cluster twice")
		}
		seen[e] = true
		r.allocated = append(r.allocated, [2]uint64{c, e})
	}
	if uint64(len(r.allocated)) != allocated {
		return fail("damaged", "allocated_clusters")
	}
	// Step 7: the logical volume (section 9).
	x, err := xts.NewCipher(aes.NewCipher, r.dataKey)
	if err != nil {
		return fail("damaged", "data key")
	}
	r.volume = make([]byte, logical)
	for _, a := range r.allocated {
		c, p := a[0], a[1]
		n := min(C, logical-c*C)
		for o := uint64(0); o < n; o += S {
			x.Decrypt(r.volume[c*C+o:c*C+o+S], f[dataOffset+p*C+o:][:S], (c*C+o)/S)
		}
	}
	d := blake2b.Sum256(r.volume)
	r.volumeBlake2b = d[:]
	r.outcome = "opens"
	return r
}
