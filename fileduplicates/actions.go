package fileduplicates

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ActionSummary counts what the delete or move phase did.
type ActionSummary struct {
	Deleted    int   // victims removed
	Moved      int   // victims moved to the target folder
	Skipped    int   // declined at the prompt, already gone, or one file under two names
	Refused    int   // not touched because the check right before it failed
	Failed     int   // the delete or move itself failed
	FreedBytes int64 // bytes of the removed or moved victims
	Stopped    bool  // the run was asked to stop before the end
}

// problems is the number of victims the run was asked to handle and could
// not: a refusal or a failure. Any of them ends the run as an error.
func (s ActionSummary) problems() int { return s.Refused + s.Failed }

// runState is one run's shared machinery: the stop request and the one
// reader every prompt of the run shares.
type runState struct {
	opts   *DuplicateOptions
	reader *bufio.Reader
	eof    bool
}

func newRunState(opts *DuplicateOptions) *runState {
	return &runState{opts: opts}
}

// stopped reports whether the run was asked to end (DUP-07).
func (rs *runState) stopped() bool {
	if rs.opts.Context != nil && rs.opts.Context.Err() != nil {
		return true
	}
	return rs.opts.Stop != nil && rs.opts.Stop()
}

func (rs *runState) context() context.Context {
	if rs.opts.Context != nil {
		return rs.opts.Context
	}
	return context.Background()
}

// ask prints a question and reads one line. End of input is "no", always
// (DUP-03): a closed stdin must never be read as consent, and once it is
// closed nothing further is asked.
func (rs *runState) ask(question string) (answer string, ok bool) {
	if rs.eof {
		return "", false
	}
	if rs.reader == nil {
		var in io.Reader = os.Stdin
		if rs.opts.Input != nil {
			in = rs.opts.Input
		}
		rs.reader = bufio.NewReader(in)
	}
	fmt.Print(question)
	line, err := rs.reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		rs.eof = true
		fmt.Println()
		return "", false
	}
	return strings.TrimSpace(line), true
}

// confirmProtected guards the places a person has to confirm by hand (DUP-06):
// -y does not cover them, and with nobody to ask the run is refused before it
// touches anything. where names the location, reason says why it is guarded.
func (rs *runState) confirmProtected(where, reason string) error {
	word := "DELETE"
	if rs.opts.Action == MoveAction {
		word = "MOVE"
	}
	if !rs.opts.Interactive {
		return &UsageError{Msg: fmt.Sprintf(
			"check-duplicates: refusing to %s duplicates in %s (%s) without an interactive confirmation; "+
				"-y does not cover this location. Run it from a console, or point it at a folder below it",
			strings.ToLower(word), where, reason)}
	}
	fmt.Printf("\n!!! PROTECTED LOCATION: %s is %s.\n", where, reason)
	fmt.Printf("Removing duplicates here needs a confirmation that -y does not give.\n")
	answer, ok := rs.ask(fmt.Sprintf("Type %s to continue: ", word))
	if !ok || answer != word {
		return fmt.Errorf("check-duplicates: cancelled - nothing was %s", pastTense(rs.opts.Action))
	}
	return nil
}

func pastTense(a DuplicateAction) string {
	if a == MoveAction {
		return "moved"
	}
	return "deleted"
}

// pathLess orders paths the way Windows compares names - case-insensitively -
// with the exact spelling as the tie-break, so the order is total.
func pathLess(a, b string) bool {
	la, lb := strings.ToLower(a), strings.ToLower(b)
	if la != lb {
		return la < lb
	}
	return a < b
}

// planGroup sorts a group so its first file is the one the rule keeps, and
// marks it. Newest and oldest compare creation times (DUP-09); a tie - and
// copies extracted from one archive do tie - is broken on the path, so the
// keeper is the same on every run.
func planGroup(group []DuplicateFileInfo, mode DuplicateSelectionMode) {
	sort.SliceStable(group, func(i, j int) bool {
		a, b := group[i], group[j]
		switch mode {
		case OldestAsOriginal:
			if !a.CreatedTime.Equal(b.CreatedTime) {
				return a.CreatedTime.Before(b.CreatedTime)
			}
		case NewestAsOriginal:
			if !a.CreatedTime.Equal(b.CreatedTime) {
				return a.CreatedTime.After(b.CreatedTime)
			}
		case LastAlphaAsOriginal:
			return pathLess(b.Path, a.Path)
		}
		return pathLess(a.Path, b.Path)
	})
	for i := range group {
		group[i].IsOriginal = i == 0
	}
}

// planGroups plans every group and orders the groups by their keeper, so the
// report and the prompts come in the same order on every run.
func planGroups(groups [][]DuplicateFileInfo, mode DuplicateSelectionMode) {
	for _, g := range groups {
		planGroup(g, mode)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return pathLess(groups[i][0].Path, groups[j][0].Path)
	})
}

// compareBuf is the chunk size of the byte comparison.
const compareBuf = 1 << 20

// sameContent compares two files byte for byte, without any cache: the proof
// that has to hold immediately before a duplicate is deleted or moved (DUP-01,
// DUP-05, DUP-15). A stop request ends it with ErrStopped.
func sameContent(a, b string, stop func() bool) (bool, error) {
	fa, err := os.Open(a)
	if err != nil {
		return false, err
	}
	defer fa.Close()
	fb, err := os.Open(b)
	if err != nil {
		return false, err
	}
	defer fb.Close()

	ia, err := fa.Stat()
	if err != nil {
		return false, err
	}
	ib, err := fb.Stat()
	if err != nil {
		return false, err
	}
	if !ia.Mode().IsRegular() || !ib.Mode().IsRegular() {
		return false, errors.New("not a regular file")
	}
	if ia.Size() != ib.Size() {
		return false, nil
	}

	bufA := make([]byte, compareBuf)
	bufB := make([]byte, compareBuf)
	for {
		if stop != nil && stop() {
			return false, ErrStopped
		}
		na, ea := io.ReadFull(fa, bufA)
		nb, eb := io.ReadFull(fb, bufB)
		if na != nb || !bytes.Equal(bufA[:na], bufB[:nb]) {
			return false, nil
		}
		endA := ea == io.EOF || ea == io.ErrUnexpectedEOF
		endB := eb == io.EOF || eb == io.ErrUnexpectedEOF
		if ea != nil && !endA {
			return false, ea
		}
		if eb != nil && !endB {
			return false, eb
		}
		if endA || endB {
			return endA && endB, nil
		}
	}
}

// errVerify marks a victim left alone because the check right before its
// removal failed: the keeper is gone, or the two files are not the same.
var errVerify = errors.New("not verified")

// errKeeperGone is the refusal that ends a whole group: its kept copy is gone.
var errKeeperGone = errors.New("not verified: the kept copy is gone")

// verifyPair is the check made immediately before one victim is removed:
// the keeper still exists, the victim is not the keeper under another name,
// and the two are identical byte for byte right now. skip is true for a pair
// that is simply not a pair (the victim is gone, or is the keeper itself).
func (rs *runState) verifyPair(keeper, victim DuplicateFileInfo) (skip bool, err error) {
	vi, verr := os.Lstat(victim.Path)
	if verr != nil {
		if errors.Is(verr, fs.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("%w: cannot read %s: %v", errVerify, victim.Path, verr)
	}
	if !vi.Mode().IsRegular() {
		return false, fmt.Errorf("%w: %s is no longer a regular file", errVerify, victim.Path)
	}
	if _, kerr := os.Stat(keeper.Path); kerr != nil {
		return false, fmt.Errorf("%w: %s (%v)", errKeeperGone, keeper.Path, kerr)
	}
	if sameObject(keeper.Path, victim.Path) {
		return true, nil
	}
	same, cerr := sameContent(keeper.Path, victim.Path, rs.stopped)
	if cerr != nil {
		if errors.Is(cerr, ErrStopped) {
			return false, cerr
		}
		return false, fmt.Errorf("%w: cannot compare %s with %s: %v", errVerify, victim.Path, keeper.Path, cerr)
	}
	if !same {
		return false, fmt.Errorf("%w: %s differs from the kept copy %s", errVerify, victim.Path, keeper.Path)
	}
	return false, nil
}

// maxMoveNames bounds the search for a free name in the target folder.
const maxMoveNames = 10000

// moveToTarget moves one victim into the target folder under its own name,
// or the first free `name_(n).ext`. It never replaces a file: a name that is
// taken - before the move, or by a file that appears during it - moves on to
// the next one. Any error other than "does not exist" while probing a name
// ends the search (DUP-04).
func (rs *runState) moveToTarget(src string) (string, error) {
	fileName := filepath.Base(src)
	ext := filepath.Ext(fileName)
	stem := fileName[:len(fileName)-len(ext)]
	for n := 0; n < maxMoveNames; n++ {
		name := fileName
		if n > 0 {
			name = fmt.Sprintf("%s_(%d)%s", stem, n, ext)
		}
		dst := filepath.Join(rs.opts.TargetDir, name)
		if _, err := os.Lstat(dst); err == nil {
			continue
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		err := moveNoReplace(rs.context(), src, dst, rs.stopped)
		if err == nil {
			return dst, nil
		}
		if isDestinationExists(err) {
			continue
		}
		return "", err
	}
	return "", fmt.Errorf("no free name for %s in %s after %d tries", fileName, rs.opts.TargetDir, maxMoveNames)
}

// prepareTarget creates the move target folder before anything is moved.
func (o DuplicateOptions) prepareTarget() error {
	if o.Action != MoveAction {
		return nil
	}
	if strings.TrimSpace(o.TargetDir) == "" {
		return &UsageError{Msg: "check-duplicates: 'move' needs a target folder"}
	}
	if err := os.MkdirAll(o.TargetDir, 0o755); err != nil {
		return fmt.Errorf("cannot create the move target %s: %w", o.TargetDir, err)
	}
	if fi, err := os.Stat(o.TargetDir); err != nil || !fi.IsDir() {
		return fmt.Errorf("the move target %s is not a folder", o.TargetDir)
	}
	return nil
}

// ProcessDuplicateGroups keeps one file of every group - the one the rule
// picks - and deletes or moves the others.
//
// Every victim is handled in the same order: stop request, prompt (unless
// -y), the check made immediately before the removal (keeper present, not the
// same file, identical byte for byte), then the delete or the no-replace move.
// Nothing is removed on the strength of a hash alone. A refusal or a failure
// is counted, and any of them makes the run end with an error; a stop ends it
// with ErrStopped after saying what was already done.
func ProcessDuplicateGroups(duplicateGroups [][]DuplicateFileInfo, options DuplicateOptions) (ActionSummary, error) {
	if err := options.Validate(); err != nil {
		return ActionSummary{}, err
	}
	if err := options.CheckConsent(); err != nil {
		return ActionSummary{}, err
	}
	rs := newRunState(&options)
	return rs.guardAndProcess(duplicateGroups)
}

// guardAndProcess is the entry for groups that did not come from a scan of
// one root: a file inside the Windows folder or a Program Files folder needs
// the typed confirmation first (DUP-06), and the move target is created before
// anything moves.
func (rs *runState) guardAndProcess(groups [][]DuplicateFileInfo) (ActionSummary, error) {
	if rs.opts.Action != NoAction {
		if where, reason, found := firstInSystemFolder(groups); found {
			if err := rs.confirmProtected(where, "in "+reason); err != nil {
				return ActionSummary{}, err
			}
		}
		if err := rs.opts.prepareTarget(); err != nil {
			return ActionSummary{}, err
		}
	}
	return rs.process(groups)
}

// firstInSystemFolder finds the first file inside the Windows folder or a
// Program Files folder.
func firstInSystemFolder(groups [][]DuplicateFileInfo) (string, string, bool) {
	inside := systemFolderClassifier()
	for _, g := range groups {
		for _, f := range g {
			if ok, reason := inside(f.Path); ok {
				return f.Path, reason, true
			}
		}
	}
	return "", "", false
}

func (rs *runState) process(duplicateGroups [][]DuplicateFileInfo) (ActionSummary, error) {
	var sum ActionSummary
	opts := rs.opts
	planGroups(duplicateGroups, opts.SelectionMode)
	if opts.Action == NoAction || len(duplicateGroups) == 0 {
		return sum, nil
	}

	verb := "Delete"
	if opts.Action == MoveAction {
		verb = "Move"
	}
	fmt.Printf("\nRule: %s.\n", opts.RuleDescription())
	if opts.Action == MoveAction {
		fmt.Printf("Duplicates are moved to %s; an existing file there is never replaced.\n", opts.TargetDir)
	}
	if opts.BatchMode {
		fmt.Printf("-y given: no question per file. Each file is still compared byte for byte with the kept copy first.\n")
	}

	total := 0
	for _, g := range duplicateGroups {
		total += len(g) - 1
	}

groups:
	for gi, group := range duplicateGroups {
		keeper := group[0]
		fmt.Printf("\nGroup %d of %d: keeping %s\n", gi+1, len(duplicateGroups), keeper.Path)
		for _, victim := range group[1:] {
			if rs.stopped() {
				sum.Stopped = true
				break groups
			}

			if !opts.BatchMode {
				answer, ok := rs.ask(fmt.Sprintf("%s duplicate file: %s? (y/n/a, a=all): ", verb, victim.Path))
				if !ok {
					fmt.Println("No answer (input closed): nothing more is deleted or moved.")
					sum.Skipped += countRemaining(duplicateGroups, gi, victim.Path)
					break groups
				}
				switch strings.ToLower(answer) {
				case "a":
					opts.BatchMode = true
				case "y":
				default:
					fmt.Println("Skipped")
					sum.Skipped++
					continue
				}
			}

			skip, err := rs.verifyPair(keeper, victim)
			if errors.Is(err, ErrStopped) {
				sum.Stopped = true
				break groups
			}
			if skip {
				sum.Skipped++
				continue
			}
			if err != nil {
				sum.Refused++
				fmt.Fprintf(os.Stderr, "Refused: %v\n", err)
				if errors.Is(err, errKeeperGone) {
					// Without its keeper the group has nothing left to keep.
					sum.Refused += len(group) - 1 - indexOf(group, victim.Path)
					fmt.Fprintf(os.Stderr, "Group %d skipped: its kept copy is gone.\n", gi+1)
					continue groups
				}
				continue
			}

			switch opts.Action {
			case DeleteAction:
				if err := os.Remove(victim.Path); err != nil {
					sum.Failed++
					fmt.Fprintf(os.Stderr, "Error deleting file %s: %v\n", victim.Path, err)
					continue
				}
				sum.Deleted++
				sum.FreedBytes += victim.Size
				fmt.Printf("Deleted: %s\n", victim.Path)
			case MoveAction:
				dst, err := rs.moveToTarget(victim.Path)
				if err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, ErrStopped) {
						sum.Stopped = true
						break groups
					}
					sum.Failed++
					fmt.Fprintf(os.Stderr, "Error moving file %s: %v\n", victim.Path, err)
					continue
				}
				sum.Moved++
				sum.FreedBytes += victim.Size
				fmt.Printf("Moved: %s -> %s\n", victim.Path, dst)
			}
		}
	}

	fmt.Printf("\nDuplicates: %d deleted, %d moved, %d skipped, %d refused, %d failed (of %d).\n",
		sum.Deleted, sum.Moved, sum.Skipped, sum.Refused, sum.Failed, total)

	if sum.Stopped {
		return sum, fmt.Errorf("%w: %d deleted and %d moved before the stop; the rest were not touched",
			ErrStopped, sum.Deleted, sum.Moved)
	}
	if n := sum.problems(); n > 0 {
		return sum, fmt.Errorf("%d of %d duplicate(s) could not be %s (%d refused by the check before removal, %d failed)",
			n, total, pastTense(opts.Action), sum.Refused, sum.Failed)
	}
	return sum, nil
}

// indexOf is the position of path within group (1 for the first victim).
func indexOf(group []DuplicateFileInfo, path string) int {
	for i, f := range group {
		if f.Path == path {
			return i
		}
	}
	return len(group) - 1
}

// countRemaining counts the victims from path (in group gi) to the end.
func countRemaining(groups [][]DuplicateFileInfo, gi int, path string) int {
	n := len(groups[gi]) - indexOf(groups[gi], path)
	for _, g := range groups[gi+1:] {
		n += len(g) - 1
	}
	return n
}
