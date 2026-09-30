package vdisk

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"errors"
	"math"
	"testing"

	"golang.org/x/crypto/xts"
)

// T2.1: every field of the header plaintext survives encode and decode, sits
// at the offset FDD-FORMAT section 5 gives it, and the reserved bytes are zero.
func TestVD_Header_RoundTripAndOffsets(t *testing.T) {
	h := header{
		VersionMajor: 1, VersionMinor: 0, Profile: ProfileFast, Flags: flagPepperWrapped,
		SectorShift: 12, ClusterShift: 20, LogicalSize: 1 << 30, PhysicalSize: 0x1122334455,
		ClusterCount: 1024, AllocatedClusters: 1000, MapOffset: 8192, MapEntrySize: 8, MapCopies: 2,
		MapStride: 8192, MapGeneration: 77, MapActive: 1, SaveInProgress: 0, DataOffset: 1 << 20,
		SlotsOffset: 4096, SlotCount: 8, SlotSize: 512, Clean: 1, LastGoodSave: 1111, Created: 2222,
		MountCount: 3333, WriterStamp: 2609261530, KDFID: 0, KDFParamsID: 0, PartCount: 1,
		SaveStarted: 0,
	}
	for i := range h.ContainerID {
		h.ContainerID[i] = byte(0xA0 + i)
	}
	copy(h.FriendlyName[:], "Work disk")
	for i := range h.KDFSalt {
		h.KDFSalt[i] = byte(0x10 + i)
	}
	for i := range h.MapDigest {
		h.MapDigest[i] = byte(0xC0 + i)
	}
	b := h.encode()
	if len(b) != plaintextSize {
		t.Fatalf("plaintext is %d bytes", len(b))
	}
	got, ok := decodeHeader(b)
	if !ok || got != h {
		t.Fatalf("round trip lost a field:\n got %+v\nwant %+v", got, h)
	}
	for _, c := range []struct {
		off  int
		want string
	}{
		{0, "46444443"}, {4, "0100"}, {8, "01"}, {9, "08"}, {10, "0c00"}, {12, "14000000"},
		{16, "0000004000000000"}, {24, "5544332211000000"}, {72, "4d00000000000000"}, {80, "01"},
		{88, "0000100000000000"}, {104, "08000002"}, {128, "01"}, {152, "050d000000000000"},
		{160, "a0a1a2a3"}, {176, "576f726b"}, {240, "da2b869b00000000"}, {256, "10111213"},
		{324, "01000000"}, {352, "c0c1c2c3"},
	} {
		want, _ := hex.DecodeString(c.want)
		if !bytes.Equal(b[c.off:c.off+len(want)], want) {
			t.Errorf("offset %d: %x, want %s", c.off, b[c.off:c.off+len(want)], c.want)
		}
	}
	for _, r := range [][2]int{{82, 88}, {108, 112}, {129, 136}, {384, plaintextSize}} {
		if !allZero(b[r[0]:r[1]]) {
			t.Errorf("reserved bytes %d..%d are not zero", r[0], r[1])
		}
	}
}

// T2.1: section 5.2 in its order - unsupported before damaged, and every item
// named in the document refused as the class it names.
func TestVD_Header_Checks(t *testing.T) {
	base := func() header {
		c, _ := newMemContainer(t, CreateOptions{LogicalSize: 16 << 20, ClusterShift: 16})
		return c.hdr
	}
	L := uint64(20480)
	if h := base(); checkHeader(&h, L) != nil {
		t.Fatalf("a fresh header fails its own checks: %v", checkHeader(&h, L))
	}
	cases := []struct {
		name string
		mut  func(h *header)
		want error
	}{
		{"major 2", func(h *header) { h.VersionMajor = 2; h.Created = 0 }, ErrUnsupported},
		{"minor 7 reads", func(h *header) { h.VersionMinor = 7 }, nil},
		{"profile 5", func(h *header) { h.Profile = 5 }, ErrUnsupported},
		{"compressed", func(h *header) { h.Flags |= flagCompressed }, ErrUnsupported},
		{"multi-file", func(h *header) { h.Flags |= flagMultiFile }, ErrUnsupported},
		{"flag bit 5", func(h *header) { h.Flags |= 1 << 5 }, ErrUnsupported},
		{"sector shift 10", func(h *header) { h.SectorShift = 10 }, ErrUnsupported},
		{"cluster shift 23", func(h *header) { h.ClusterShift = 23 }, ErrUnsupported},
		{"map entry 16", func(h *header) { h.MapEntrySize = 16 }, ErrUnsupported},
		{"slot count 9", func(h *header) { h.SlotCount = 9 }, ErrUnsupported},
		{"kdf 2", func(h *header) { h.KDFID = 2 }, ErrUnsupported},
		{"kdf row 9", func(h *header) { h.Flags &^= flagPepperWrapped; h.KDFID = 1; h.KDFParamsID = 9 }, ErrUnsupported},
		{"digest bit reads", func(h *header) { h.Flags |= flagDigest }, nil},
		{"logical 0", func(h *header) { h.LogicalSize = 0 }, ErrDamaged},
		{"logical odd", func(h *header) { h.LogicalSize += 100 }, ErrDamaged},
		{"cluster count", func(h *header) { h.ClusterCount++ }, ErrDamaged},
		{"allocated", func(h *header) { h.AllocatedClusters = h.ClusterCount + 1 }, ErrDamaged},
		{"map offset", func(h *header) { h.MapOffset += 512 }, ErrDamaged},
		{"stride", func(h *header) { h.MapStride = 0 }, ErrDamaged},
		{"data offset", func(h *header) { h.DataOffset += 4096 }, ErrDamaged},
		{"clean 2", func(h *header) { h.Clean = 2 }, ErrDamaged},
		{"save without start", func(h *header) { h.Clean = 0; h.SaveInProgress = 1 }, ErrDamaged},
		{"save and clean", func(h *header) { h.SaveInProgress = 1; h.SaveStarted = 5 }, ErrDamaged},
		{"created 0", func(h *header) { h.Created = 0 }, ErrDamaged},
		{"obfuscated with kdf", func(h *header) { h.KDFID = 1; h.KDFParamsID = 1 }, ErrDamaged},
		{"vault obfuscated", func(h *header) { h.Profile = ProfileVault }, ErrDamaged},
		{"digest fields", func(h *header) { h.DigestOffset = 4096 }, ErrDamaged},
		{"part count", func(h *header) { h.PartCount = 2 }, ErrDamaged},
		{"compress fields", func(h *header) { h.CompressTableLength = 1 }, ErrDamaged},
		{"slots over map", func(h *header) { h.SlotsOffset = 8192 }, ErrDamaged},
		{"map past end", func(h *header) { h.MapOffset = 1 << 30 }, ErrDamaged},
	}
	for _, c := range cases {
		h := base()
		c.mut(&h)
		err := checkHeader(&h, L)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// T2.2: a flipped byte in the primary is recovered from the backup; both
// flipped is damage; the kind-1 data key comes back through the backup, which
// is why it is wrapped under a key from kdf_salt and not from the primary's
// salt; a changed map byte is damage, never followed.
func TestVD_HeaderSeal(t *testing.T) {
	deterministic(t, "seal")
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 4 << 20, ClusterShift: 16})
	if _, err := c.WriteAt(pattern(1, 200000), 12345); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	want := readAll(t, mustOpenMem(t, mb, OpenRead))
	good := mb.snapshot()
	L := len(good)

	primaryFlipped := append([]byte(nil), good...)
	primaryFlipped[sealedOffset+100] ^= 0x01
	pc := mustOpenMem(t, &memBacking{data: primaryFlipped}, OpenRead)
	if !pc.Info().FromBackup {
		t.Fatal("the primary was flipped and the backup did not carry the open")
	}
	if !bytes.Equal(readAll(t, pc), want) {
		t.Fatal("the volume read through the backup differs")
	}

	bothFlipped := append([]byte(nil), primaryFlipped...)
	bothFlipped[L-headerSize+sealedOffset+100] ^= 0x01
	if _, err := openMem(&memBacking{data: bothFlipped}, OpenRead); !errors.Is(err, ErrDamaged) {
		t.Fatalf("both headers flipped: %v, want damaged", err)
	}

	// The salt area is not sealed but feeds header_key: a changed salt byte is
	// a header that does not open, like any other change.
	saltFlipped := append([]byte(nil), good...)
	saltFlipped[3] ^= 0x80
	if c := mustOpenMem(t, &memBacking{data: saltFlipped}, OpenRead); !c.Info().FromBackup {
		t.Fatal("a changed primary salt still opened the primary")
	}

	// A backup never passes for a primary: the associated data differs.
	swapped := append([]byte(nil), good...)
	copy(swapped[0:headerSize], good[L-headerSize:])
	if c := mustOpenMem(t, &memBacking{data: swapped}, OpenRead); !c.Info().FromBackup {
		t.Fatal("a backup block at offset 0 opened as the primary")
	}

	// map_digest: one changed byte of the active map copy is damage.
	h := mustOpenMem(t, mb, OpenRead).hdr
	mapFlipped := append([]byte(nil), good...)
	mapFlipped[h.mapCopyOffset(h.MapActive)] ^= 0x01
	if _, err := openMem(&memBacking{data: mapFlipped}, OpenRead); !errors.Is(err, ErrDamaged) {
		t.Fatalf("a changed map byte: %v, want damaged", err)
	}
	// ... while the inactive copy may hold anything.
	inactiveFlipped := append([]byte(nil), good...)
	inactiveFlipped[h.mapCopyOffset(1-h.MapActive)] ^= 0x01
	if _, err := openMem(&memBacking{data: inactiveFlipped}, OpenRead); err != nil {
		t.Fatalf("a changed inactive copy: %v", err)
	}

	// A file that is not a container, and one shorter than any.
	if _, err := openMem(&memBacking{data: pattern(9, 1<<20)}, OpenRead); !errors.Is(err, ErrDamaged) {
		t.Fatalf("not a container: %v", err)
	}
	if _, err := openMem(&memBacking{data: make([]byte, minFileSize-1)}, OpenRead); !errors.Is(err, ErrDamaged) {
		t.Fatalf("short file: %v", err)
	}
}

// T2.2: a header sealed by a different pepper is indistinguishable from
// damage (FDD-FORMAT section 6.1 step 3).
func TestVD_HeaderSeal_OtherPepper(t *testing.T) {
	_, mb := newMemContainer(t, CreateOptions{LogicalSize: 1 << 20, ClusterShift: 16})
	img := mb.snapshot()
	old := pepper
	pepper[0] ^= 1
	defer func() { pepper = old }()
	if _, err := openMem(&memBacking{data: img}, OpenRead); !errors.Is(err, ErrDamaged) {
		t.Fatalf("another build family: %v, want damaged", err)
	}
}

// T2.3: an unused slot is indistinguishable from random, and so is a used one
// - by a chi-square test over the bytes of 1000 generated slot regions - and a
// used slot yields the data key under obf_key only.
func TestVD_Slots(t *testing.T) {
	var counts [2][256]int
	total := [2]int{}
	for i := 0; i < 1000; i++ {
		kdfSalt := make([]byte, 16)
		kdfSalt[0], kdfSalt[1] = byte(i), byte(i>>8)
		k := obfKey(kdfSalt)
		dataKey := pattern(byte(i), dataKeySize)
		region, err := drawSlots(randSource, k[:], dataKey)
		if err != nil {
			t.Fatal(err)
		}
		got, err := openSlots(region, k[:], true)
		if err != nil || !bytes.Equal(got, dataKey) {
			t.Fatalf("slot %d: %v", i, err)
		}
		other := obfKey(append(append([]byte(nil), kdfSalt[:15]...), 0xff))
		if _, err := openSlots(region, other[:], true); !errors.Is(err, ErrDamaged) {
			t.Fatalf("slots opened under another key: %v", err)
		}
		if _, err := openSlots(region, other[:], false); !errors.Is(err, ErrCredential) {
			t.Fatalf("an encrypted container's slots under a wrong key: %v, want wrong credential", err)
		}
		for j, x := range region {
			used := 0
			if j >= slotSize {
				used = 1
			}
			counts[used][x]++
			total[used]++
		}
	}
	// 255 degrees of freedom: the 99.99th percentile of chi-square is about 347.
	for kind, name := range []string{"used slot", "unused slots"} {
		exp := float64(total[kind]) / 256
		var chi float64
		for _, n := range counts[kind] {
			chi += (float64(n) - exp) * (float64(n) - exp) / exp
		}
		if chi > 347 || math.IsNaN(chi) {
			t.Errorf("%s bytes are not uniform: chi-square %.1f", name, chi)
		}
	}
}

// T2.3: a slot that opens but breaks section 7.2 is refused as the class the
// document names.
func TestVD_Slots_Outcomes(t *testing.T) {
	k := obfKey(make([]byte, 16))
	build := func(kind, flags byte, magic string) []byte {
		region := pattern(3, slotRegionSize)
		salt, nonce, pad := make([]byte, slotSaltSize), make([]byte, slotNonceSize), make([]byte, slotPadSize)
		s := sealSlot(k[:], salt, nonce, pad, kind, pattern(5, dataKeySize))
		// reseal with a changed plaintext where the test needs one
		if flags != 0 || magic != "SLOT" {
			pt := slotPlaintext(kind, pattern(5, dataKeySize))
			pt[5] = flags
			copy(pt[0:4], magic)
			sk := slotKey(k[:], salt)
			s = sealRaw(sk[:], salt, nonce, pad, pt)
		}
		copy(region[2*slotSize:], s)
		return region
	}
	for _, c := range []struct {
		name string
		reg  []byte
		want error
	}{
		{"kind 1", build(1, 0, "SLOT"), nil},
		{"kind 2 under obf_key", build(2, 0, "SLOT"), ErrDamaged},
		{"kind 4", build(4, 0, "SLOT"), ErrUnsupported},
		{"slot flags", build(1, 1, "SLOT"), ErrUnsupported},
		{"no magic", build(1, 0, "SLOX"), ErrDamaged},
	} {
		_, err := openSlots(c.reg, k[:], true)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// T2.4: the IEEE 1619 AES-256-XTS vector (a 512-byte data unit, sequence
// number 0xff) passes through the sector layer, and a ciphertext moved to
// another place decrypts unchanged, because the tweak is the logical index.
func TestVD_XTS(t *testing.T) {
	key, _ := hex.DecodeString("27182818284590452353602874713526624977572470936999595749669676273141592653589793238462643383279502884197169399375105820974944592")
	want, _ := hex.DecodeString("1c3b3a102f770386e4836c99e370cf9bea00803f5e482357a4ae12d414a3e63b5d31e276f8fe4a8d66b317f9ac683f44680a86ac35adfc3345befecb4bb188fd5776926c49a3095eb108fd1098baec70aaa66999a72a82f27d848b21d4a741b0c5cd4d5fff9dac89aeba122961d03a757123e9870f8acf1000020887891429ca2a3e7a7d7df7b10355165c8b9a6d0a7de8b062c4500dc4cd120c0f7418dae3d0b5781c34803fa75421c790dfe1de1834f280d7667b327f6c8cd7557e12ac3a0f93ec05c52e0493ef31a12d3d9260f79a289d6a379bc70c50841473d1a8cc81ec583e9645e07b8d9670655ba5bbcfecc6dc3966380ad8fecb17b6ba02469a020a84e18e8f84252070c13e9f1f289be54fbc481457778f616015e1327a02b140f1505eb309326d68378f8374595c849d84f4c333ec4423885143cb47bd71c5edae9be69a2ffeceb1bec9de244fbe15992b11b77c040f12bd8f6a975a44a0f90c29a9abc3d4d893927284c58754cce294529f8614dcd2aba991925fedc4ae74ffac6e333b93eb4aff0479da9a410e4450e0dd7ae4c6e2910900575da401fc07059f645e8b7e9bfdef33943054ff84011493c27b3429eaedb4ed5376441a77ed43851ad77f16f541dfd269d50d6a5f14fb0aab1cbb4c1550be97f7ab4066193c4caa773dad38014bd2092fa755c824bb5e54c4f36ffda9fcea70b9c6e693e148c151")
	plain := make([]byte, 512)
	for i := range plain {
		plain[i] = byte(i)
	}
	sc, err := newSectorCipher(key, 9)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 512)
	sc.encrypt(got, plain, 0xff)
	if !bytes.Equal(got, want) {
		t.Fatalf("IEEE 1619 vector: %x", got[:32])
	}

	// A cluster moved in the file keeps its ciphertext and reads back unchanged.
	c, mb := newMemContainer(t, CreateOptions{LogicalSize: 2 << 20, ClusterShift: 16, SectorShift: 9})
	data := pattern(7, 3<<16)
	if _, err := c.WriteAt(data, 1<<16); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c = mustOpenMem(t, mb, OpenRead)
	h := c.hdr
	cs := int64(h.clusterSize())
	img := mb.snapshot()
	// Swap the physical positions of logical clusters 1 and 2 by moving bytes
	// and swapping their map entries.
	p1, p2 := c.entries[1], c.entries[2]
	a := append([]byte(nil), img[int64(h.DataOffset)+int64(p1)*cs:][:cs]...)
	b := append([]byte(nil), img[int64(h.DataOffset)+int64(p2)*cs:][:cs]...)
	copy(img[int64(h.DataOffset)+int64(p1)*cs:], b)
	copy(img[int64(h.DataOffset)+int64(p2)*cs:], a)
	moved := &memBacking{data: img}
	mc := mustOpenMem(t, moved, OpenWrite)
	mc.entries[1], mc.entries[2] = p2, p1
	mc.pending = true
	if err := mc.beginSession(); err != nil {
		t.Fatal(err)
	}
	if err := mc.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, mustOpenMem(t, moved, OpenRead)); !bytes.Equal(got[1<<16:1<<16+len(data)], data) {
		t.Fatal("a moved ciphertext did not decrypt unchanged")
	}
}

// T2.4: the sector layer is byte-identical to golang.org/x/crypto/xts, the
// independent oracle, for both allowed sector sizes, sector indices across the
// 64-bit range, and several sectors in one call.
func TestVD_XTS_MatchesXCrypto(t *testing.T) {
	for _, shift := range []uint16{9, 12} {
		n := 1 << shift
		for k := 0; k < 8; k++ {
			key := pattern(byte(k*31+int(shift)), dataKeySize)
			ours, err := newSectorCipher(key, shift)
			if err != nil {
				t.Fatal(err)
			}
			oracle, err := xts.NewCipher(aes.NewCipher, key)
			if err != nil {
				t.Fatal(err)
			}
			for _, first := range []uint64{0, 1, 0xff, 1 << 31, 1<<63 - 2, ^uint64(0) - 3} {
				plain := pattern(byte(first)+byte(k), 3*n)
				got := make([]byte, len(plain))
				ours.encrypt(got, plain, first)
				want := make([]byte, len(plain))
				for i := 0; i < 3; i++ {
					oracle.Encrypt(want[i*n:(i+1)*n], plain[i*n:(i+1)*n], first+uint64(i))
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("sector %d bytes, first sector %d: differs from x/crypto/xts", n, first)
				}
				ours.decrypt(got, got, first)
				if !bytes.Equal(got, plain) {
					t.Fatalf("sector %d bytes, first sector %d: decrypt is not the inverse", n, first)
				}
			}
		}
	}
}

// T2.17: every sentinel maps to its class, the classes are distinct, and an
// error from the store underneath is I/O.
func TestVD_Errors(t *testing.T) {
	want := map[error]int{
		ErrUsage: 2, ErrCredential: 3, ErrDamaged: 4, ErrIO: 5, ErrUnsupported: 6, ErrBusy: 8,
		ErrStopped: ExitStopped,
	}
	seen := map[int]bool{}
	for e, code := range want {
		if got := ExitClass(e); got != code {
			t.Errorf("%v: class %d, want %d", e, got, code)
		}
		if got := ExitClass(damagedf("wrapped %v", e)); got != ExitDamaged && e == ErrDamaged {
			t.Errorf("wrapped damage: %d", got)
		}
		if seen[code] {
			t.Errorf("class %d used twice", code)
		}
		seen[code] = true
	}
	if ExitClass(ioErr(errCrash)) != ExitIO || !errors.Is(ioErr(errCrash), errCrash) {
		t.Error("a backing error is not I/O, or lost its cause")
	}
	if ExitClass(ioErr(ErrStopped)) != ExitStopped {
		t.Error("ioErr reclassified a stop")
	}
	if ExitClass(nil) != ExitSuccess {
		t.Error("nil is not success")
	}
}
