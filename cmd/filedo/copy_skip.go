package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// B3 of SP-0027: the one rule for "this file is already copied" (COPY-02,
// COPY-04, COPY-19).
//
// A target is already the copy of its source when it has the same size AND
// the same modification time - never merely "it exists and is not empty",
// which is how a truncated file under the final name used to pass for a
// finished one. A target that cannot be looked at in time is "could not
// verify": it is skipped, counted and reported, never taken for absent and
// overwritten. A target that exists and differs is kept - a copy never
// overwrites a file it did not write - and is counted too, so the run does
// not claim to have mirrored a tree it did not. The one exception is an empty
// target where the source is not empty: there is nothing in it to lose, and
// it is the shape the stop bug of older versions left behind (COPY-04).

type skipVerdict int

const (
	// copyNeeded: no target yet - copy it.
	copyNeeded skipVerdict = iota
	// copyReplaceEmpty: an empty leftover target - copy and replace it.
	copyReplaceEmpty
	// skipAlreadyCopied: same size and same time - nothing to do.
	skipAlreadyCopied
	// skipTargetDiffers: a different file is there - keep it, count it.
	skipTargetDiffers
	// skipUnverified: the target could not be looked at in time - skip and
	// count, never overwrite.
	skipUnverified
)

func (v skipVerdict) String() string {
	switch v {
	case copyNeeded:
		return "copy"
	case copyReplaceEmpty:
		return "replace-empty"
	case skipAlreadyCopied:
		return "already-copied"
	case skipTargetDiffers:
		return "target-differs"
	default:
		return "unverified"
	}
}

// modTimeTolerance is how far two modification times may be apart and still
// be "the same time": FAT keeps two-second steps and some SMB servers whole
// seconds, so the time a copy set can come back rounded.
const modTimeTolerance = 2 * time.Second

func sameModTime(a, b time.Time) bool {
	d := a.Sub(b)
	if d < 0 {
		d = -d
	}
	return d <= modTimeTolerance
}

// targetStat looks at a target without following a link. It is a variable so
// a test can make it time out (COPY-19).
var targetStat = func(path string) (os.FileInfo, error) {
	return lstatWithTimeout(path, FileOperationTimeout)
}

// errStatTimeout marks a look at a path that did not answer in time.
var errStatTimeout = errors.New("the file system did not answer in time")

// lstatWithTimeout is os.Lstat bounded by timeout, for a share or a failing
// disk that does not answer.
func lstatWithTimeout(path string, timeout time.Duration) (os.FileInfo, error) {
	type result struct {
		info os.FileInfo
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		info, err := os.Lstat(path)
		ch <- result{info, err}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	select {
	case r := <-ch:
		return r.info, r.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%s: %w after %v", path, errStatTimeout, timeout)
	}
}

// skipDecision decides what a copy does with one source file and its target.
// The error is set only for skipUnverified and says why.
func skipDecision(src os.FileInfo, dst string) (skipVerdict, error) {
	if isPartialName(dst) {
		// A partial is never a finished copy of anything.
		return skipTargetDiffers, nil
	}
	ti, err := targetStat(dst)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return copyNeeded, nil
		}
		return skipUnverified, err
	}
	if ti.IsDir() || !ti.Mode().IsRegular() {
		// A folder, a link or a device is there, not a copy.
		return skipTargetDiffers, nil
	}
	if ti.Size() == src.Size() && (sameModTime(ti.ModTime(), src.ModTime()) || src.Size() == 0) {
		// Two empty files are the same bytes whatever their times say.
		return skipAlreadyCopied, nil
	}
	if ti.Size() == 0 && src.Size() > 0 {
		return copyReplaceEmpty, nil
	}
	return skipTargetDiffers, nil
}
