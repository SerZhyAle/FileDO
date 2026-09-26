package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// AUD-11-F1: a container this run could not read is not a damaged container.
// Another process holds a byte-range lock over everything after the head, so
// the head opens and the first read past it fails with ERROR_LOCK_VIOLATION:
// verify and unsecure end in class C (I/O, verdict Not proven), never in the
// damaged class B, and nothing is restored.
func TestFdsecLockedContainerIsNotProvenNotDamaged(t *testing.T) {
	wd, _ := workdir(t)
	const secret = "pw-locked-range"
	if out, code := run(t, wd, "plain.txt", "secure", "p:"+secret, "to", "box.fd-sec"); code != 0 {
		t.Fatalf("secure exited %d\n%s", code, out)
	}
	container := filepath.Join(wd, "box.fd-sec")

	f, err := os.Open(container)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	const head = 668 // the suite-1 head (FDSEC-FORMAT.md section 4); the metadata follows it
	ol := &windows.Overlapped{Offset: head}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, uint32(fi.Size()-head), 0, ol); err != nil {
		t.Fatalf("LockFileEx: %v", err)
	}
	defer windows.UnlockFileEx(windows.Handle(f.Fd()), 0, uint32(fi.Size()-head), 0, ol)

	events := filepath.Join(t.TempDir(), "events.jsonl")
	out, code := run(t, wd, "--events", events, "fdsec", "verify", container, "p:"+secret)
	if code != fdsecExitIO {
		t.Errorf("verify of a locked container exited %d, want the I/O class %d\n%s", code, fdsecExitIO, out)
	}
	if got := lastResult(t, events)["verdict"]; got != "Not proven" {
		t.Errorf("verdict %q, want %q - the run never read the container\n%s", got, "Not proven", out)
	}
	if strings.Contains(out, "damaged") {
		t.Errorf("a container the run could not read is called damaged\n%s", out)
	}

	out, code = run(t, wd, "box.fd-sec", "unsecure", "p:"+secret, "to", "restored.txt")
	if code != fdsecExitIO {
		t.Errorf("unsecure of a locked container exited %d, want the I/O class %d\n%s", code, fdsecExitIO, out)
	}
	if exists(filepath.Join(wd, "restored.txt")) {
		t.Error("unsecure of a container it could not read left a restored file")
	}
	assertNoPartials(t, wd)
}
