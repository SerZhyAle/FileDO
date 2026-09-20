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
func PackFile(dstPath, srcPath string, meta Metadata, cred Credential, p Params) (Info, error) {
	var zero Info
	if _, err := os.Stat(dstPath); err == nil {
		return zero, fmt.Errorf("fdsec: refusing to overwrite existing %s", dstPath)
	} else if !os.IsNotExist(err) {
		return zero, fmt.Errorf("fdsec: examine %s: %w", dstPath, err)
	}
	src, err := os.Open(srcPath)
	if err != nil {
		return zero, err
	}
	defer src.Close()

	tmp, err := tempSibling(dstPath)
	if err != nil {
		return zero, err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return zero, err
	}
	info, err := Pack(f, src, meta, cred, p)
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

	// Read-back: reopen and unpack to nowhere - every chunk tag and the final
	// digest are verified before the rename (FDSEC-FORMAT.md section 10 step 5).
	rb, err := os.Open(tmp)
	if err != nil {
		os.Remove(tmp)
		return zero, err
	}
	_, err = Unpack(io.Discard, rb, cred)
	rb.Close()
	if err != nil {
		os.Remove(tmp)
		return zero, fmt.Errorf("fdsec: read-back verification failed: %w", err)
	}

	if err := os.Rename(tmp, dstPath); err != nil {
		os.Remove(tmp)
		return zero, fmt.Errorf("fdsec: rename into place: %w", err)
	}
	return info, nil
}

// tempSibling builds an obviously-partial temporary name next to dstPath.
func tempSibling(dstPath string) (string, error) {
	suffix := make([]byte, 6)
	if _, err := io.ReadFull(rand.Reader, suffix); err != nil {
		return "", err
	}
	dir, base := filepath.Split(dstPath)
	return filepath.Join(dir, fmt.Sprintf("%s.fdsec-partial-%x", base, suffix)), nil
}
