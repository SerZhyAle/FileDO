package fdsec

import (
	"bytes"
	"os"
	"runtime"
	"testing"
	"time"
)

// TestMeasure_Suite2Profile is the entry measurement SP-0019 D3 requires
// before the suite-2 profile is pinned: each derivation on its own, and the
// whole cost a wrong password pays under the dispatch of FDSEC-FORMAT.md
// section 13.4 (suite 1's derivation, then suite 2's), in the build the
// machine under test runs - the local toolchain is 386, so a plain run on the
// owner's machine is the 32-bit measurement. It is opt-in because it spends
// real work factors:
//
//	FDSEC_MEASURE_SUITE2=1 go test ./fdsec -run TestMeasure_Suite2Profile -v
func TestMeasure_Suite2Profile(t *testing.T) {
	if os.Getenv("FDSEC_MEASURE_SUITE2") != "1" {
		t.Skip("set FDSEC_MEASURE_SUITE2=1 to measure the real suite-2 profile")
	}
	saved1, saved2 := activeProfile, suite2Profiles
	activeProfile, suite2Profiles = profileV1, suite2ProfilesV1
	defer func() { activeProfile, suite2Profiles = saved1, saved2 }()

	t.Logf("GOARCH=%s GOOS=%s NumCPU=%d", runtime.GOARCH, runtime.GOOS, runtime.NumCPU())
	salt := bytes.Repeat([]byte{7}, saltSize)
	cred := NewCredential("correct horse")

	peak := func() uint64 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.Sys
	}
	timeIt := func(what string, fn func()) time.Duration {
		runtime.GC()
		start := time.Now()
		fn()
		d := time.Since(start)
		t.Logf("%-44s %8.3f s   runtime Sys %4d MiB", what, d.Seconds(), peak()>>20)
		return d
	}

	for i := 0; i < 3; i++ {
		timeIt("suite 1 derivation (64 MiB, T=3, P=4)", func() { deriveRoot(cred, salt) })
		timeIt("suite 2 derivation (256 MiB, T=3, P=4)", func() {
			if _, err := deriveSuite2Key(cred, salt, suite2Profiles[0]); err != nil {
				t.Fatal(err)
			}
		})
	}

	// A real suite-2 file, then a wrong password against it through Open:
	// suite 1's head fails, then suite 2's frame 0 fails.
	var buf bytes.Buffer
	if _, err := PackSuite2(&buf, bytes.NewReader([]byte("x")), Metadata{Name: "m.txt", Size: 1}, cred); err != nil {
		t.Fatal(err)
	}
	var worst time.Duration
	for i := 0; i < 3; i++ {
		d := timeIt("wrong password, full dispatch (1 then 2)", func() {
			if _, err := Open(bytes.NewReader(buf.Bytes()), NewCredential("wrong")); err == nil {
				t.Fatal("a wrong password opened the file")
			}
		})
		worst = max(worst, d)
	}
	timeIt("right password, suite 2 (1 fails, 2 opens)", func() {
		if _, err := Open(bytes.NewReader(buf.Bytes()), cred); err != nil {
			t.Fatal(err)
		}
	})
	t.Logf("worst wrong-password dispatch: %.3f s", worst.Seconds())
}
