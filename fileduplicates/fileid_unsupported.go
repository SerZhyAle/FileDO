//go:build !windows

package fileduplicates

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Fallbacks for platforms FileDO does not ship on. There is no file ID here,
// so no file is ever "identified": the hash cache is never trusted and the
// byte comparison before every delete or move carries the whole weight.

func statTimes(info fs.FileInfo) (created, accessed time.Time) {
	return info.ModTime(), info.ModTime()
}

func identify(file *DuplicateFileInfo) error {
	_, err := os.Stat(file.Path)
	return err
}

func sameObject(a, b string) bool {
	ia, err := os.Stat(a)
	if err != nil {
		return false
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ia, ib)
}

var forceCopyMove = false

var errDestinationExists = errors.New("destination already exists")

func isDestinationExists(err error) bool { return errors.Is(err, errDestinationExists) }

func moveNoReplace(ctx context.Context, src, dst string, stop func() bool) error {
	if _, err := os.Lstat(dst); err == nil {
		return &os.LinkError{Op: "rename", Old: src, New: dst, Err: errDestinationExists}
	}
	return os.Rename(src, dst)
}

func classifyProtected(p string) (bool, string) {
	abs, err := filepath.Abs(p)
	if err == nil && filepath.Dir(abs) == abs {
		return true, "the root of a filesystem"
	}
	return false, ""
}

func systemFolderClassifier() func(p string) (bool, string) {
	return func(string) (bool, string) { return false, "" }
}

func scanExclusions(root string) []string { return nil }
