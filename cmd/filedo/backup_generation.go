package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"filedo/backupzip"
	"filedo/fsx"
)

// The generation set of `backup` (SP-0208, CLI-EVENT-STREAM 0.17 item D):
//
//	filedo backup <source> gen <N> [stem <name>] [to <folder>] [sec pe:<VAR>] ..
//
// gen switches a numbered set on and names its keep-count, 1 to 999; stem names
// the set. The artifact is `<stem>_<NNN>.zip`, or `.fd-sec` with sec; the stem
// is the caller's or the source folder's name, and a set is the files of one
// stem and one extension in one destination, so two stems, or the plain and the
// sealed set of one stem, never share or retire each other's files.
//
// Which number: derived from the destination and from nothing else. The newest
// generation of the set is the file with the latest modification time (equal
// times: the higher number) among the files whose whole name is
// `<stem>_<NNN><ext>` with NNN in 001..N, the file system's case rule deciding
// the comparison; the run writes the number after it, 001 after N, and 001 when
// the set is empty. A number that is missing does not stop the run, and a file
// numbered above N, left by an earlier and larger keep-count, is not in the set.
//
// Retirement: when the number chosen is taken it is the oldest generation of
// the full set, and it is retired by being replaced. The new generation is
// built under a partial name and read back first (a plain zip by
// backupzip.Verify, a sealed one by fdsec.PackFile's own read-back); only then
// is the old file of that number removed and the new one renamed into place.
// Nothing is removed earlier, a failed or stopped run retires nothing, and at
// most one file is removed per run, only one whose whole name matches.

// backupGenMax is the largest keep-count: a generation number is three digits.
const backupGenMax = 999

// backupGen is the numbered set one run writes into.
type backupGen struct {
	keep     int    // the keep-count N, 1..999
	stem     string // the set's stem
	dir      string // the destination folder
	ext      string // ".zip", or the container's extension when the run seals
	number   int    // the generation this run builds, 1..keep
	occupied bool   // that number is taken by a file of the set: retired by replacement
}

func backupGenName(stem string, n int, ext string) string {
	return fmt.Sprintf("%s_%03d%s", stem, n, ext)
}

// path is the final name of the generation this run builds.
func (g *backupGen) path() string {
	return filepath.Join(g.dir, backupGenName(g.stem, g.number, g.ext))
}

// backupSourceRefused and backupDestinationRefused are the two definition errors
// of `backup`: class 10, the token on the `error` finding, no numbers
// (CLI-EVENT-STREAM rule 17, 0.17 item G). A usage error - a word that is
// missing or malformed - carries no token: it is read by its class.
func backupSourceRefused(format string, a ...interface{}) error {
	return withReason(exitClassf(10, format, a...), reasonBackupSourceRefused, nil)
}

func backupDestinationRefused(format string, a ...interface{}) error {
	return withReason(exitClassf(10, format, a...), reasonBackupDestinationRefused, nil)
}

// backupStemProblem says why a stem cannot be used, or "" when it can: one
// file-name part Windows creates as written, short enough to carry the number
// and the longer extension (CLI-EVENT-STREAM 0.17 item D1).
func backupStemProblem(stem string) string {
	if err := backupzip.CheckName(stem); err != nil {
		return err.Error()
	}
	// The longest name this stem can make: `<stem>_<NNN>` plus the container's extension.
	if n := len(utf16.Encode([]rune(backupGenName(stem, backupGenMax, fdsecExtension)))); n > 255 {
		return fmt.Sprintf("makes a file name of %d characters, and a name holds 255", n)
	}
	return ""
}

// backupGenNumber reads the generation off a file name: the number NNN when the
// whole name is `<stem>_<NNN><ext>` with NNN inside 001..keep, the file system's
// case rule (Windows folds case) deciding the comparison.
func backupGenNumber(name, stem, ext string, keep int) (int, bool) {
	if len(name) < len(ext)+5 || !strings.EqualFold(name[len(name)-len(ext):], ext) {
		return 0, false
	}
	body := name[:len(name)-len(ext)] // <stem>_<NNN>
	digits := body[len(body)-3:]
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return 0, false
		}
	}
	head := body[:len(body)-3] // <stem>_
	if head == "" || head[len(head)-1] != '_' || !strings.EqualFold(head[:len(head)-1], stem) {
		return 0, false
	}
	n, err := strconv.Atoi(digits)
	if err != nil || n < 1 || n > keep {
		return 0, false
	}
	return n, true
}

// backupGenScan lists the generations of one set present in dir, each with its
// modification time. Only regular files count: a folder or a link under a
// matching name is not a generation and is never retired.
func backupGenScan(dir, stem, ext string, keep int) (map[int]time.Time, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	present := make(map[int]time.Time)
	for _, e := range entries {
		n, ok := backupGenNumber(e.Name(), stem, ext, keep)
		if !ok || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if errors.Is(err, os.ErrNotExist) {
			continue // gone since the listing
		}
		if err != nil {
			return nil, err
		}
		present[n] = info.ModTime()
	}
	return present, nil
}

// backupGenNext is the number a run writes: the one after the newest generation
// present (latest modification time, equal times the higher number), 001 after
// the keep-count, and 001 when the set is empty.
func backupGenNext(present map[int]time.Time, keep int) int {
	newest, at := 0, time.Time{}
	for n, t := range present {
		if newest == 0 || t.After(at) || (t.Equal(at) && n > newest) {
			newest, at = n, t
		}
	}
	if newest == 0 || newest >= keep {
		return 1
	}
	return newest + 1
}

// backupArchiveName reports whether a path ends in one of the two extensions a
// backup artifact carries.
func backupArchiveName(p string) bool {
	ext := filepath.Ext(p)
	return strings.EqualFold(ext, ".zip") || strings.EqualFold(ext, fdsecExtension)
}

// backupResolveSetFolder resolves the destination folder for a backup set
// (`gen` or `keep-days`): `to` must name an existing folder (class 10
// backup-destination-refused on a file or archive name; class 11 on a missing
// folder). Without `to` it returns defaultDir.
func backupResolveSetFolder(o *backupOpts, stem, defaultDir, option string) (string, error) {
	if !o.haveTo {
		return defaultDir, nil
	}
	to, err := filepath.Abs(o.to)
	if err != nil {
		return "", err
	}
	st, serr := os.Stat(to)
	switch {
	case serr == nil && st.IsDir():
		return to, nil
	case serr == nil:
		return "", backupDestinationRefused("%s is a file; with %s, to names the folder the backups go into, and a backup never replaces a file", to, option)
	case errors.Is(serr, os.ErrNotExist) && backupArchiveName(to):
		return "", backupDestinationRefused("with %s, to names a folder: the files are named by the run for stem %s", option, stem)
	case errors.Is(serr, os.ErrNotExist):
		return "", exitClassf(11, "the folder %s does not exist; with %s, to names an existing folder", to, option)
	default:
		return "", serr
	}
}

// backupGenPlan resolves the set this run writes into and the number it takes,
// before anything is written. defaultStem and defaultDir are what the run uses
// when stem and to are not given: the source folder's name, and the folder
// beside the source (the current folder for a drive or share root).
func backupGenPlan(o *backupOpts, defaultStem, defaultDir string) (*backupGen, error) {
	stem := defaultStem
	if o.stem != "" {
		stem = o.stem
	}
	if why := backupStemProblem(stem); why != "" {
		return nil, exitClassf(10, "the stem of a numbered backup is one file-name part, and this one is refused: %s; name another with stem", why)
	}
	dir, err := backupResolveSetFolder(o, stem, defaultDir, "gen")
	if err != nil {
		return nil, err
	}
	ext := ".zip"
	if o.sec {
		ext = fdsecExtension
	}
	present, err := backupGenScan(dir, stem, ext, o.gen)
	if err != nil {
		return nil, err
	}
	g := &backupGen{keep: o.gen, stem: stem, dir: dir, ext: ext, number: backupGenNext(present, o.gen)}
	_, g.occupied = present[g.number]
	if !g.occupied {
		// The slot is free of the set, but anything else that holds the name is
		// not ours to replace or remove (item D4).
		if _, err := os.Lstat(g.path()); err == nil {
			return nil, backupDestinationRefused("%s already exists and is not a file of the numbered set; a backup never replaces it", g.path())
		}
	}
	return g, nil
}

// backupGenClearPartial removes the partial name a run is about to use when an
// earlier run died and left it: a crash must not wedge a rotation that nobody
// watches. A partial another run still holds open cannot be removed (a sharing
// violation), and that ends this run as a file in use - an obstacle that may
// pass - rather than letting two runs write one file.
func backupGenClearPartial(partial string) error {
	if err := os.Remove(partial); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// backupGenCommit is the common ending of a plain and a sealed generation: the
// new one stands complete and verified under its partial name. A stop is honoured
// up to here and not past it. Then the old file of the number, when the set was
// full, is removed - the one removal of the run - and the new one renamed into
// place; unregister drops the forced-exit cleanup first, because that cleanup
// would delete the partial and these two steps must not be cut between.
func backupGenCommit(partial string, g *backupGen, unregister func()) (retired bool, err error) {
	if runStopRequested() {
		os.Remove(partial)
		return false, backupStopped("nothing was retired; the set is as it was")
	}
	unregister()
	if g.occupied {
		switch rerr := os.Remove(g.path()); {
		case rerr == nil:
			retired = true
		case !errors.Is(rerr, os.ErrNotExist):
			os.Remove(partial)
			return false, rerr
		}
	}
	if err := fsx.RenameNoReplace(partial, g.path()); err != nil {
		if retired {
			return true, fmt.Errorf("generation %03d was retired, but the new one could not be moved into its place; the verified new generation is kept as %s: %w", g.number, partial, err)
		}
		os.Remove(partial)
		return false, err
	}
	return retired, nil
}

// backupGenNote is the console's one-line account of the generation written.
func backupGenNote(g *backupGen, retired bool) string {
	if retired {
		return fmt.Sprintf("generation %03d of %d, the old generation %03d retired", g.number, g.keep, g.number)
	}
	return fmt.Sprintf("generation %03d of %d", g.number, g.keep)
}

// backupGenReport puts the three published keys on the result (CLI-EVENT-STREAM
// 0.17 items D and E). It is called only once the artifact is published, so a
// run that ends otherwise never carries them.
func backupGenReport(g *backupGen, retired bool) {
	runNumber("generation", g.number)
	retiredNumber := 0
	if retired {
		retiredNumber = g.number
	}
	runNumber("retiredGeneration", retiredNumber)
	runNumber("keepCount", g.keep)
}

// backupGenFinishPlain publishes a plain generation: the zip stands closed under
// its partial name, is read back in full, and only then takes the place of the
// oldest generation (backupGenCommit).
func backupGenFinishPlain(res backupzip.PackResult, abs string, level int, g *backupGen, hl *HistoryLogger) error {
	partial := res.Path
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			os.Remove(partial)
		}
	})
	defer removeCleanup()

	runStep("verify", "Reading the new generation back before an older one is retired")
	if err := backupzip.Verify(partial, res.Files, res.Bytes, runStopRequested); err != nil {
		os.Remove(partial)
		switch {
		case errors.Is(err, backupzip.ErrStopped):
			return backupStopped("nothing was retired; the set is as it was")
		case errors.Is(err, backupzip.ErrVerify):
			return defectf("the new generation does not read back as it was written (%v); nothing was retired and nothing was left under a final name", err)
		}
		return err
	}
	retired, err := backupGenCommit(partial, g, removeCleanup)
	if err != nil {
		return err
	}
	runNumber("archiveBytes", res.Size)
	backupGenReport(g, retired)
	fmt.Printf("OK %s -> %s (%s, level %d, %s files, %s -> %s)\n", abs, g.path(), backupGenNote(g, retired), level, formatCount(res.Files), formatBytes(uint64(res.Bytes)), formatBytes(uint64(res.Size)))
	hl.SetResult("archive", g.path())
	return nil
}
