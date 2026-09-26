package fdsec

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// AUD-11-F1: a container read that fails for a reason other than running out
// of bytes is the environment's failure - a byte-range lock, a sharing
// violation, a share or a device that went away - and is class C. It must
// never be reported as ErrDamaged, a verdict on a container the run never
// read, and the cause must stay on the error chain for the caller to classify.

// errLockViolation is ERROR_LOCK_VIOLATION as a read of a locked range
// returns it on Windows.
var errLockViolation = &os.PathError{Op: "read", Path: "box.fd-sec", Err: syscall.Errno(33)}

// failReader serves b, but a read whose range covers failAt fails with err
// once skip such reads have been served normally.
type failReader struct {
	r      *bytes.Reader
	failAt int64
	skip   int
	err    error
}

func (f *failReader) Read(p []byte) (int, error) {
	pos, _ := f.r.Seek(0, io.SeekCurrent)
	if pos <= f.failAt && f.failAt < pos+int64(len(p)) {
		if f.skip > 0 {
			f.skip--
			return f.r.Read(p)
		}
		n, _ := f.r.Read(p[:f.failAt-pos])
		return n, f.err
	}
	return f.r.Read(p)
}

func (f *failReader) Seek(off int64, whence int) (int64, error) { return f.r.Seek(off, whence) }

func TestReadError_EnvironmentIsNotDamage(t *testing.T) {
	p := fastParams()
	cred := NewCredential("pw-read-errors")

	// Suite 1, three chunks.
	s1 := packBytes(t, randContent(t, 2*(int(p.ChunkSize)-tagSize)+5), fixedTimes, cred, p)
	h1, _, err := openHead(bytes.NewReader(s1), cred)
	if err != nil {
		t.Fatal(err)
	}

	// Suite 2, three frames.
	var s2buf bytes.Buffer
	s2content := randContent(t, 2*s2FramePlain+100)
	meta2 := fixedTimes
	meta2.Size = int64(len(s2content))
	if _, err := PackSuite2(&s2buf, bytes.NewReader(s2content), meta2, cred); err != nil {
		t.Fatal(err)
	}
	s2 := s2buf.Bytes()

	// Suite 3, a folder.
	items, order := sampleItems()
	base := t.TempDir()
	src := filepath.Join(base, "tree")
	writeDiskTree(t, src, items, order)
	tree, err := ScanTree(src, ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var s3buf bytes.Buffer
	if _, err := PackTree(&s3buf, tree, cred, p); err != nil {
		t.Fatal(err)
	}
	s3 := s3buf.Bytes()
	h3, _, err := openHead(bytes.NewReader(s3), cred)
	if err != nil {
		t.Fatal(err)
	}

	unpackFile := func(r io.ReadSeeker) error {
		_, err := Unpack(io.Discard, r, cred)
		return err
	}
	verifyTree := func(r io.ReadSeeker) error {
		c, err := Open(r, cred)
		if err != nil {
			return err
		}
		_, _, err = c.VerifyTree()
		return err
	}

	cases := []struct {
		name   string
		b      []byte
		failAt int64
		skip   int
		read   func(io.ReadSeeker) error
	}{
		{"suite 1 head", s1, 10, 0, unpackFile},
		{"suite 1 metadata", s1, preMeta + 10, 0, unpackFile},
		{"suite 1 middle chunk", s1, h1.preLen() + int64(p.ChunkSize) + 10, 0, unpackFile},
		{"suite 2 lead", s2, 10, 1, unpackFile},
		{"suite 2 frame 0", s2, s2Lead + 10, 1, unpackFile},
		{"suite 2 middle frame", s2, s2Lead + s2Frame + 10, 0, unpackFile},
		{"suite 3 directory block", s3, preMeta + 10, 0, verifyTree},
		{"suite 3 manifest", s3, h3.preLen() + 10, 0, verifyTree},
		{"suite 3 last entry chunk", s3, int64(len(s3)) - 10, 0, verifyTree},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The container is sound: the same read with no fault succeeds.
			if err := c.read(bytes.NewReader(c.b)); err != nil {
				t.Fatalf("clean read failed: %v", err)
			}

			err := c.read(&failReader{r: bytes.NewReader(c.b), failAt: c.failAt, skip: c.skip, err: errLockViolation})
			if err == nil {
				t.Fatal("a failing read reported success")
			}
			if errors.Is(err, ErrDamaged) || errors.Is(err, ErrCredentialOrTamper) || errors.Is(err, ErrUnsupported) {
				t.Fatalf("an environmental read error was reported as a verdict on the container: %v", err)
			}
			if !errors.Is(err, syscall.Errno(33)) {
				t.Fatalf("the read error's cause is lost from the chain: %v", err)
			}

			// Running out of bytes at the same place is still damage.
			err = c.read(&failReader{r: bytes.NewReader(c.b), failAt: c.failAt, skip: c.skip, err: io.ErrUnexpectedEOF})
			if !errors.Is(err, ErrDamaged) {
				t.Fatalf("an unexpected EOF is not reported as damage: %v", err)
			}
		})
	}
}
