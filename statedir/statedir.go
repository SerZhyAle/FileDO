// Package statedir is the one place FileDO keeps the files it writes for
// itself - history.json, the copy engine's skip list, check's good and
// damaged lists and its resume state, the duplicate finder's hash cache
// (SP-0024 section 4, SP-0023 theme T5).
//
// They used to land in the current directory or beside the executable, which
// is why a check started from Explorer littered the user's folder, why an
// installed build could never keep its hash cache (Program Files and
// WindowsApps are not writable), and why two runs shared state only when they
// happened to start in the same directory. The root is
// %LOCALAPPDATA%\FileDO\state\; FILEDO_STATE_DIR moves it, which is the seam
// the tests use and the only override.
package statedir

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// EnvOverride names the variable that moves the state root.
const EnvOverride = "FILEDO_STATE_DIR"

var (
	dirOnce sync.Once
	dirPath string
	dirErr  error
)

// Dir returns the state root, creating it on first use.
func Dir() (string, error) {
	if d := os.Getenv(EnvOverride); d != "" {
		// The override is re-read every call, so a test can move it.
		if err := os.MkdirAll(d, 0o700); err != nil {
			return "", err
		}
		return d, nil
	}
	dirOnce.Do(func() {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			var err error
			base, err = os.UserCacheDir()
			if err != nil {
				dirErr = fmt.Errorf("cannot locate the per-user application data folder: %w", err)
				return
			}
		}
		dirPath = filepath.Join(base, "FileDO", "state")
		dirErr = os.MkdirAll(dirPath, 0o700)
	})
	return dirPath, dirErr
}

// Path returns where the state file name lives. The first time the file is
// asked for and does not exist yet, the first legacy copy found among legacy
// is imported: copied, never moved - the old file is the user's and stays
// where it was. Import failures are not fatal; the state simply starts empty.
func Path(name string, legacy ...string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(d, name)
	if _, err := os.Stat(p); err == nil || !errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	for _, old := range legacy {
		if old == "" {
			continue
		}
		oldAbs, aerr := filepath.Abs(old)
		if aerr != nil || strings.EqualFold(filepath.Clean(oldAbs), filepath.Clean(p)) {
			continue
		}
		fi, serr := os.Stat(oldAbs)
		if serr != nil || !fi.Mode().IsRegular() {
			continue
		}
		if cerr := importCopy(oldAbs, p); cerr == nil {
			fmt.Fprintf(os.Stderr, "Note: imported %s into %s (the old file is left in place).\n", oldAbs, p)
		}
		break
	}
	return p, nil
}

// importCopy copies src to dst through a temporary name, so a crash leaves
// either nothing or the whole file.
func importCopy(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".import-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	// Never over an existing file: another process may have imported or
	// written first, and its copy is the one to keep.
	if err := renameNoReplace(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// LegacyBesideExe names a legacy file beside the running executable.
func LegacyBesideExe(name string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Join(filepath.Dir(exe), name)
}

// LegacyInCwd names a legacy file in the current directory.
func LegacyInCwd(name string) string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return filepath.Join(wd, name)
}

// WriteFileAtomic writes data to a unique temporary file beside path, flushes
// it, and renames it over path. A reader sees the old file or the new one,
// never half of either.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	_ = os.Chmod(tmpName, perm)
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// staleLockAge is how old a lock file must be before it is taken to belong to
// a process that died holding it.
const staleLockAge = 30 * time.Second

// Lock takes an exclusive, cross-process lock for a read-modify-write of
// path, waiting up to timeout. The returned function releases it.
func Lock(path string, timeout time.Duration) (unlock func(), err error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(timeout)
	for {
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			fmt.Fprintf(f, "%d\n", os.Getpid())
			f.Close()
			return func() { os.Remove(lockPath) }, nil
		}
		// A lock file another process has just removed is still "delete
		// pending" for a moment, and creating it then fails with access
		// denied rather than "exists" - that is contention too, not an error.
		if !errors.Is(err, os.ErrExist) && !errors.Is(err, os.ErrPermission) {
			return nil, err
		}
		if errors.Is(err, os.ErrPermission) && time.Now().After(deadline) {
			return nil, err
		}
		if fi, serr := os.Stat(lockPath); serr == nil && time.Since(fi.ModTime()) > staleLockAge {
			os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for %s", lockPath)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
