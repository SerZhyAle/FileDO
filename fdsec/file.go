package fdsec

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// PackFile packs srcPath into a container at dstPath, enforcing the two writer
// invariants of FDSEC-FORMAT.md section 10 at the file level:
//
//   - the container is written to a temporary sibling and renamed into place
//     only after it was closed, reopened and read back in full, so an
//     interrupted pack never leaves a container that looks complete
//     (safety invariant 3); the temporary file is removed on any failure;
//   - the final name is never written over: an existing dstPath is an error,
//     and the overwrite decision belongs to the caller (safety invariant 2).
//
// The read-back is the proof of principle 3: by the time PackFile returns,
// the container has been reopened and every chunk verified against the
// original's own digest.
func PackFile(dstPath, srcPath string, meta Metadata, cred Credential, p Params, opts ...StreamOption) (Info, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return Info{}, err
	}
	defer src.Close()
	return packToFile(dstPath, newStreamOpts(opts).replace, func(f io.Writer) (Info, error) {
		return Pack(f, src, meta, cred, p, opts...)
	}, func(rb io.ReadSeeker) error {
		return verifyContainer(rb, cred)
	})
}

// PackFileSuite2 is PackFile for suite 2 (FDSEC-FORMAT.md section 18): the
// same temporary sibling, the same refusal to overwrite, the same full
// read-back before the rename. Suite 2 stores no digest, so the read-back
// compares the payload it recovers with the digest of what was packed.
func PackFileSuite2(dstPath, srcPath string, meta Metadata, cred Credential, opts ...StreamOption) (Info, error) {
	src, err := os.Open(srcPath)
	if err != nil {
		return Info{}, err
	}
	defer src.Close()
	var packed [digestSize]byte
	return packToFile(dstPath, newStreamOpts(opts).replace, func(f io.Writer) (Info, error) {
		info, digest, err := packSuite2(f, src, meta, cred, opts...)
		packed = digest
		return info, err
	}, func(rb io.ReadSeeker) error {
		st, err := openSuite2(rb, cred)
		if err != nil {
			return err
		}
		c := &Container{s2: st, src: rb}
		_, got, err := c.unpackSuite2(io.Discard, streamOpts{})
		if err != nil {
			return err
		}
		if got != packed {
			return fmt.Errorf("%w: recovered bytes do not hash to what was packed", ErrDamaged)
		}
		return nil
	})
}

// packToFile is the file-level half PackFile, PackFileSuite2 and PackTreeFile
// share: refuse an existing name, write a temporary sibling, flush, read it
// back in full with verify, and only then rename it into place.
//
// The final rename never replaces a file unless replace is set: a name that
// appeared while the pack ran (a second secure to the same container name,
// FDSEC-01) is reported as ErrExists with nothing written over it. With
// replace set - the user chose to overwrite this exact path - the old
// destination is replaced by that same rename, after the read-back, and is
// untouched by every failure before it (FDSEC-03).
func packToFile(dstPath string, replace bool, write func(io.Writer) (Info, error), verify func(io.ReadSeeker) error) (Info, error) {
	var zero Info
	if !replace {
		if _, err := os.Stat(dstPath); err == nil {
			return zero, fmt.Errorf("refusing to overwrite existing %s: %w", dstPath, ErrExists)
		} else if !os.IsNotExist(err) {
			return zero, fmt.Errorf("fdsec: examine %s: %w", dstPath, err)
		}
	}

	tmp, err := tempSibling(dstPath)
	if err != nil {
		return zero, err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return zero, err
	}
	info, err := write(f)
	if err != nil {
		f.Close()
		os.Remove(tmp)
		return zero, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return zero, fmt.Errorf("fdsec: flush container: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return zero, fmt.Errorf("fdsec: close container: %w", err)
	}

	// beforeReadBack is the fault-injection seam of the read-back proof
	// (nil in every shipped path, set only by a test that has to make the
	// read-back fail on purpose). It is the same kind of seam as randSource.
	if beforeReadBack != nil {
		beforeReadBack(tmp)
	}

	// Read-back: reopen and unpack to nowhere - every chunk tag and every
	// digest are verified before the rename (FDSEC-FORMAT.md sections 10
	// step 5, 17.7 step 5).
	rb, err := os.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return zero, err
	}
	err = verify(rb)
	rb.Close()
	if err != nil {
		os.Remove(tmp)
		return zero, fmt.Errorf("fdsec: read-back verification failed: %w", err)
	}

	if err := moveIntoPlace(tmp, dstPath, replace); err != nil {
		os.Remove(tmp)
		return zero, fmt.Errorf("fdsec: rename into place: %w", err)
	}
	return info, nil
}

// verifyContainer reads a container of either suite end to end, writing
// nothing.
func verifyContainer(rs io.ReadSeeker, cred Credential) error {
	c, err := Open(rs, cred)
	if err != nil {
		return err
	}
	if c.IsTree() {
		_, _, err = c.VerifyTree()
		return err
	}
	_, err = c.Unpack(io.Discard)
	return err
}

// beforeReadBack is called with the temporary container's path after it was
// written and closed and before it is read back. Production leaves it nil.
var beforeReadBack func(tmpPath string)

// tempSibling builds an obviously-partial temporary name next to dstPath.
func tempSibling(dstPath string) (string, error) {
	suffix := make([]byte, 6)
	if _, err := io.ReadFull(rand.Reader, suffix); err != nil {
		return "", err
	}
	dir, base := filepath.Split(dstPath)
	return filepath.Join(dir, fmt.Sprintf("%s.fdsec-partial-%x", base, suffix)), nil
}
