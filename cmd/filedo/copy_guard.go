package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// refuseCopyPaths is the check every copy verb makes before a byte moves
// (SP-0027 COPY-01, COPY-10). A source and target that are one file - `D:\a`
// and `d:\A`, a `\\?\` or `\\localhost\D$` spelling, a hard link - used to
// truncate the source to nothing; a target inside the source folder used to
// re-enter itself level after level until the disk or the path limit gave
// out. Both are refused as usage errors: nothing was measured, exit 2.
//
// It is called at the top of every copy entry point, so the interactive and
// the batch dispatch reach the same check.
func refuseCopyPaths(verb, source, target string) error {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(target) == "" {
		return fmt.Errorf("%s: a source and a target are required", verb)
	}
	info, err := os.Stat(source)
	if err != nil {
		// The engine reports a missing source in its own words.
		return nil
	}
	same, err := sameFilePaths(source, target)
	if err != nil {
		return fmt.Errorf("%s: cannot tell whether %q and %q are the same file: %v", verb, source, target, err)
	}
	if same {
		return fmt.Errorf("%s: source %q and target %q are the same file; refusing - a copy onto itself would empty it", verb, source, target)
	}
	if info.IsDir() {
		inside, err := pathWithin(target, source)
		if err != nil {
			return fmt.Errorf("%s: cannot resolve %q against %q: %v", verb, target, source, err)
		}
		if inside {
			return overlapError(verb, source, target)
		}
	}
	return nil
}

// singleFileTarget resolves the target of a one-file copy. An existing folder,
// or a path spelled with a trailing separator (a folder that may not exist
// yet, AUD-01-F2), receives the file under its own name; the joined path is
// checked again, as refuseCopyPaths only saw the folder.
func singleFileTarget(verb, sourcePath, targetPath string) (string, error) {
	intoFolder := strings.HasSuffix(targetPath, `\`) || strings.HasSuffix(targetPath, "/")
	if ti, err := os.Stat(targetPath); err == nil && ti.IsDir() {
		intoFolder = true
	}
	if !intoFolder {
		return targetPath, nil
	}
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		return "", fmt.Errorf("cannot create the target folder: %w", err)
	}
	targetPath = filepath.Join(targetPath, filepath.Base(sourcePath))
	if err := refuseCopyPaths(verb, sourcePath, targetPath); err != nil {
		return "", err
	}
	return targetPath, nil
}

// copyRunStats is what one copy run did and did not do (COPY-06). Every file
// that was not copied is counted under the reason, printed as it happens, and
// decides the verdict: a run that left any file behind for any reason other
// than "already there" is not a finished copy, and it says so with exit 2
// instead of "Done".
type copyRunStats struct {
	verb string

	copied          atomic.Int64
	copiedBytes     atomic.Int64
	alreadyThere    atomic.Int64
	kept            atomic.Int64
	unverified      atomic.Int64
	failed          atomic.Int64
	damagedSkipped  atomic.Int64
	collisions      atomic.Int64
	partialsSkipped atomic.Int64
	specialSkipped  atomic.Int64
	// retryable counts failures a damaged-disk retry can help with: stalls
	// and device-level I/O errors, never a locked file or a full disk.
	retryable atomic.Int64

	mu    sync.Mutex
	notes []string
}

// maxCopyNotes is how many problems the summary repeats.
const maxCopyNotes = 20

func newCopyRunStats(verb string) *copyRunStats {
	return &copyRunStats{verb: verb}
}

// note prints a problem at once and keeps the first few for the summary.
func (s *copyRunStats) note(line string) {
	fmt.Printf("\n%s\n", line)
	s.mu.Lock()
	if len(s.notes) < maxCopyNotes {
		s.notes = append(s.notes, line)
	}
	s.mu.Unlock()
}

// recordFailure counts a file that could not be copied.
func (s *copyRunStats) recordFailure(path string, err error) {
	if isStopError(err) {
		return
	}
	s.failed.Add(1)
	if isCopyStall(err) || isDeviceIOError(err) {
		s.retryable.Add(1)
	}
	s.note(fmt.Sprintf("FAILED: %s: %v", path, err))
}

// recordCopied counts a finished file.
func (s *copyRunStats) recordCopied(size int64) {
	s.copied.Add(1)
	s.copiedBytes.Add(size)
}

// admit applies a skip decision: it counts what is skipped and reports
// whether the file is to be copied.
func (s *copyRunStats) admit(v skipVerdict, src, dst string, err error) bool {
	switch v {
	case copyNeeded, copyReplaceEmpty:
		return true
	case skipAlreadyCopied:
		s.alreadyThere.Add(1)
	case skipTargetDiffers:
		s.kept.Add(1)
		s.note(fmt.Sprintf("KEPT: %s already exists with a different size or time and was not overwritten", dst))
	default:
		s.unverified.Add(1)
		s.note(fmt.Sprintf("NOT CHECKED: %s could not be looked at (%v) - skipped, not overwritten", dst, err))
	}
	return false
}

// recordDamagedSkip counts a source the skip list names.
func (s *copyRunStats) recordDamagedSkip(path, listPath string) {
	s.damagedSkipped.Add(1)
	s.note(fmt.Sprintf("SKIPPED: %s is on the damaged-file list %s", path, listPath))
}

// recordCollision counts a source whose name folds onto another's.
func (s *copyRunStats) recordCollision(path, winner string) {
	s.collisions.Add(1)
	s.note(fmt.Sprintf("NAME COLLISION: %s and %s differ only in case and would land on one target name - only %s is copied", path, winner, winner))
}

// recordPartialSource counts a source that is an unfinished copy itself.
func (s *copyRunStats) recordPartialSource(path string) {
	s.partialsSkipped.Add(1)
	s.note(fmt.Sprintf("SKIPPED: %s is an unfinished copy (%s) and is not copied", path, partialSuffix))
}

// recordSpecial counts a source that is not a regular file.
func (s *copyRunStats) recordSpecial(path string) {
	s.specialSkipped.Add(1)
	s.note(fmt.Sprintf("SKIPPED: %s is not a regular file", path))
}

// incomplete is the number of files the run left behind.
func (s *copyRunStats) incomplete() int64 {
	return s.kept.Load() + s.unverified.Load() + s.failed.Load() + s.damagedSkipped.Load() + s.collisions.Load()
}

// wantsSafeRetry: the run failed files a damaged-disk pass could still get.
func (s *copyRunStats) wantsSafeRetry() bool {
	return s.retryable.Load() > 0 && !runStopRequested()
}

// finish prints the summary, records the numbers and returns the run's
// answer: nil when every file is at the target, an error (exit 2) naming the
// counts when any is not. A stopped run returns nil - its verdict is
// `Stopped`, and "Error: stopped" would be noise.
func (s *copyRunStats) finish() error {
	runNumber("copiedFiles", s.copied.Load())
	runNumber("copiedBytes", s.copiedBytes.Load())
	runNumber("alreadyThereFiles", s.alreadyThere.Load())
	runNumber("keptFiles", s.kept.Load())
	runNumber("uncheckedFiles", s.unverified.Load())
	runNumber("failedFiles", s.failed.Load())
	runNumber("damagedSkippedFiles", s.damagedSkipped.Load())
	runNumber("nameCollisions", s.collisions.Load())

	fmt.Printf("\n%s summary: %d copied (%s), %d already at the target",
		s.verb, s.copied.Load(), formatFileSize(s.copiedBytes.Load()), s.alreadyThere.Load())
	parts := s.problemParts()
	if len(parts) > 0 {
		fmt.Printf(", %s", strings.Join(parts, ", "))
	}
	fmt.Println()
	if n := s.partialsSkipped.Load(); n > 0 {
		fmt.Printf("  %d unfinished copies (%s) in the source were not copied\n", n, partialSuffix)
	}
	if n := s.specialSkipped.Load(); n > 0 {
		fmt.Printf("  %d entries that are not regular files were not copied\n", n)
	}

	if runStopRequested() {
		fmt.Printf("Stopped before the end - the files copied so far are complete; nothing half-written is left under a final name.\n")
		return nil
	}
	if s.incomplete() == 0 {
		return nil
	}
	s.mu.Lock()
	notes := append([]string(nil), s.notes...)
	s.mu.Unlock()
	if len(notes) > 0 {
		fmt.Printf("Files that are not at the target:\n")
		for _, n := range notes {
			fmt.Printf("  %s\n", n)
		}
		if int64(len(notes)) < s.incomplete() {
			fmt.Printf("  .. and %d more\n", s.incomplete()-int64(len(notes)))
		}
	}
	return fmt.Errorf("%s incomplete: %s", s.verb, strings.Join(parts, ", "))
}

func (s *copyRunStats) problemParts() []string {
	var parts []string
	add := func(n int64, what string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
		}
	}
	add(s.failed.Load(), "failed")
	add(s.kept.Load(), "kept because the target differs")
	add(s.unverified.Load(), "could not be checked at the target")
	add(s.damagedSkipped.Load(), "skipped as damaged")
	add(s.collisions.Load(), "name collisions")
	return parts
}

// caseCollisionLosers reads dir's names and returns every entry whose name
// folds (case-insensitively) onto an earlier entry's, mapped to that earlier
// name (COPY-14). A case-sensitive folder or an SMB share can hold `A.txt`
// and `a.txt`; a Windows target cannot, and copying both would leave the last
// one written - or, with parallel workers, an interleaving of the two.
// The key also drops trailing dots and spaces, as Win32 does (AUD-07-F2):
// `f.` written through `\\?\` is opened and statted as `f`.
func caseCollisionLosers(dir string) map[string]string {
	f, err := os.Open(dir)
	if err != nil {
		return nil
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil || len(names) < 2 {
		return nil
	}
	sort.Strings(names) // the order filepath.Walk visits them in
	seen := make(map[string]string, len(names))
	var losers map[string]string
	for _, n := range names {
		k := strings.TrimRight(strings.ToLower(n), ". ")
		if first, ok := seen[k]; ok {
			if losers == nil {
				losers = make(map[string]string)
			}
			losers[filepath.Join(dir, n)] = filepath.Join(dir, first)
			continue
		}
		seen[k] = n
	}
	return losers
}

// caseCollisionIndex remembers the losers of every directory a walk has
// entered. It holds only colliding names, so it stays empty on an ordinary
// tree.
type caseCollisionIndex struct {
	mu     sync.Mutex
	losers map[string]string
}

func (c *caseCollisionIndex) scanDir(dir string) {
	l := caseCollisionLosers(dir)
	if len(l) == 0 {
		return
	}
	c.mu.Lock()
	if c.losers == nil {
		c.losers = make(map[string]string)
	}
	for k, v := range l {
		c.losers[k] = v
	}
	c.mu.Unlock()
}

// winner returns the name path collides with, if it lost.
func (c *caseCollisionIndex) winner(path string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.losers[path]
	return w, ok
}
