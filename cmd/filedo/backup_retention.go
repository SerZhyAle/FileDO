package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf16"

	"filedo/backupzip"
	"filedo/fsx"
)

// Dated retention and monthly-first preservation of `backup` (SP-0230,
// CLI-EVENT-STREAM 0.18 items I to K):
//
//	filedo backup <source> [to <folder>] [stem <name>] keep-days <1-999> [keep-monthly] [sec pe:<VAR>] ..
//
// Set files are named `<stem>-<yyMMddHHmm><ext>` in local wall-clock time
// (`.zip` plain, `.fd-sec` sealed). Dates are parsed exclusively from file names,
// never from file modification times (stateless and resilient to sync folders).
// The new archive is built under a partial name, verified by full read-back,
// renamed into place with no replacement, and ONLY THEN are older set files
// past keepDays retired - keeping the earliest file of each calendar month
// indefinitely when keep-monthly is in force.

type backupKeep struct {
	days      int       // retention window in days, 1..999
	monthly   bool      // keep the earliest file of each calendar month forever
	stem      string    // set stem (caller's or source folder's name)
	dir       string    // destination folder
	ext       string    // ".zip" or ".fd-sec"
	now       time.Time // local wall-clock time captured at plan time
	zipPath   string    // <dir>/<stem>-<yyMMddHHmm>.zip
	finalPath string    // <dir>/<stem>-<yyMMddHHmm><ext>
}

func backupKeepName(stem string, at time.Time, ext string) string {
	return stem + "-" + at.Format("0601021504") + ext
}

// backupKeepStemProblem checks whether stem makes a valid Windows filename
// including `-<yyMMddHHmm><ext>`.
func backupKeepStemProblem(stem string) string {
	if err := backupzip.CheckName(stem); err != nil {
		return err.Error()
	}
	sample := stem + "-2611171300" + fdsecExtension
	if n := len(utf16.Encode([]rune(sample))); n > 255 {
		return fmt.Sprintf("makes a file name of %d characters, and a name holds 255", n)
	}
	return ""
}

// backupKeepTimestamp parses `<stem>-<yyMMddHHmm><ext>` in local time, matching
// Windows case-insensitivity on stem and ext. Non-set names and invalid
// calendar dates return (zero, false) and are never touched.
func backupKeepTimestamp(name, stem, ext string) (time.Time, bool) {
	// Minimum: 1-char stem + "-" + 10 digits + ext.
	if len(name) < len(ext)+12 || !strings.EqualFold(name[len(name)-len(ext):], ext) {
		return time.Time{}, false
	}
	body := name[:len(name)-len(ext)]
	if len(body) < 12 {
		return time.Time{}, false
	}
	digits := body[len(body)-10:]
	for i := 0; i < len(digits); i++ {
		if digits[i] < '0' || digits[i] > '9' {
			return time.Time{}, false
		}
	}
	head := body[:len(body)-10]
	if head == "" || head[len(head)-1] != '-' || !strings.EqualFold(head[:len(head)-1], stem) {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("0601021504", digits, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

type backupKeepFile struct {
	name  string
	stamp time.Time
}

type backupKeepSelection struct {
	retire      []string
	keptCount   int
	monthlyKept int
}

type yearMonthKey struct {
	year  int
	month time.Month
}

// backupKeepSelect determines which set files in names to retire for keepDays
// and keepMonthly at local wall-clock time now. Dates come solely from names.
// When protect is non-empty, that filename (case-insensitive) is never retired.
func backupKeepSelect(names []string, stem, ext string, keepDays int, keepMonthly bool, now time.Time, protect string) backupKeepSelection {
	var parsed []backupKeepFile
	for _, n := range names {
		if ts, ok := backupKeepTimestamp(n, stem, ext); ok {
			parsed = append(parsed, backupKeepFile{name: n, stamp: ts})
		}
	}
	sort.Slice(parsed, func(i, j int) bool {
		if !parsed[i].stamp.Equal(parsed[j].stamp) {
			return parsed[i].stamp.Before(parsed[j].stamp)
		}
		return strings.ToLower(parsed[i].name) < strings.ToLower(parsed[j].name)
	})

	monthlyFirst := make(map[string]bool)
	if keepMonthly {
		seenMonth := make(map[yearMonthKey]bool)
		for _, f := range parsed {
			ym := yearMonthKey{year: f.stamp.Year(), month: f.stamp.Month()}
			if !seenMonth[ym] {
				seenMonth[ym] = true
				monthlyFirst[f.name] = true
			}
		}
	}

	cutoff := now.AddDate(0, 0, -keepDays)
	var out backupKeepSelection
	for _, f := range parsed {
		if protect != "" && strings.EqualFold(f.name, protect) {
			continue
		}
		if !f.stamp.Before(cutoff) {
			continue
		}
		if keepMonthly && monthlyFirst[f.name] {
			out.monthlyKept++
			continue
		}
		out.retire = append(out.retire, f.name)
	}
	out.keptCount = len(parsed) - len(out.retire)
	return out
}

// backupKeepScan returns the filenames of regular set members in dir.
func backupKeepScan(dir, stem, ext string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		if _, ok := backupKeepTimestamp(e.Name(), stem, ext); ok {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// backupKeepPlan resolves the destination folder and target filename for a
// dated-retention run before anything is written.
func backupKeepPlan(o *backupOpts, defaultStem, defaultDir string) (*backupKeep, error) {
	stem := defaultStem
	if o.stem != "" {
		stem = o.stem
	}
	if why := backupKeepStemProblem(stem); why != "" {
		return nil, exitClassf(10, "the stem of a dated-retention backup is one file-name part, and this one is refused: %s; name another with stem", why)
	}
	dir, err := backupResolveSetFolder(o, stem, defaultDir, "keep-days")
	if err != nil {
		return nil, err
	}
	ext := ".zip"
	if o.sec {
		ext = fdsecExtension
	}
	now := time.Now()
	zipPath := filepath.Join(dir, backupKeepName(stem, now, ".zip"))
	finalPath := filepath.Join(dir, backupKeepName(stem, now, ext))
	if _, err := os.Lstat(finalPath); err == nil {
		return nil, backupDestinationRefused("%s already exists; a backup never replaces a file - wait a minute or name another destination or stem", finalPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return &backupKeep{
		days:      o.keepDays,
		monthly:   o.keepMonthly,
		stem:      stem,
		dir:       dir,
		ext:       ext,
		now:       now,
		zipPath:   zipPath,
		finalPath: finalPath,
	}, nil
}

// backupKeepCommit renames the verified partial into finalPath (no replacement)
// only if no stop was requested beforehand. A stop or rename error removes the
// partial and retires nothing.
func backupKeepCommit(partial string, k *backupKeep, unregister func()) error {
	if runStopRequested() {
		os.Remove(partial)
		return backupStopped("nothing was retired; the set is as it was")
	}
	unregister()
	if err := fsx.RenameNoReplace(partial, k.finalPath); err != nil {
		os.Remove(partial)
		return err
	}
	return nil
}

// backupKeepReport publishes the CLI-EVENT-STREAM 0.18 retention numbers after
// the new artifact is in place.
func backupKeepReport(k *backupKeep, retired, kept, monthlyKept int) {
	runNumber("retiredCount", retired)
	runNumber("keptCount", kept)
	runNumber("keepDays", k.days)
	if k.monthly {
		runNumber("monthlyKept", monthlyKept)
	}
}

// backupKeepNote formats the console summary of what the retention pass did.
func backupKeepNote(k *backupKeep, retired, kept, monthlyKept int) string {
	if k.monthly {
		return fmt.Sprintf("keep-days %d + keep-monthly: retired %d, kept %d (%d monthly)", k.days, retired, kept, monthlyKept)
	}
	return fmt.Sprintf("keep-days %d: retired %d, kept %d", k.days, retired, kept)
}

// backupKeepRetire runs after the new artifact is at k.finalPath: scans the
// destination set, removes every eligible file older than k.days (preserving
// each calendar month's earliest file when k.monthly is true), emits the
// published numbers, and returns the first removal error if any failed.
func backupKeepRetire(k *backupKeep) (retired, kept, monthlyKept int, err error) {
	names, err := backupKeepScan(k.dir, k.stem, k.ext)
	if err != nil {
		return 0, 1, 0, err
	}
	sel := backupKeepSelect(names, k.stem, k.ext, k.days, k.monthly, k.now, filepath.Base(k.finalPath))
	var firstErr error
	failedRemove := 0
	for _, name := range sel.retire {
		rerr := os.Remove(filepath.Join(k.dir, name))
		switch {
		case rerr == nil:
			retired++
		case errors.Is(rerr, os.ErrNotExist):
			// Already gone since the scan.
		default:
			failedRemove++
			if firstErr == nil {
				firstErr = rerr
			}
		}
	}
	kept = sel.keptCount + failedRemove
	monthlyKept = sel.monthlyKept
	backupKeepReport(k, retired, kept, monthlyKept)
	return retired, kept, monthlyKept, firstErr
}

// backupKeepFinishPlain verifies a plain zip built under partial, renames it
// into place, and only then retires older files of the set.
func backupKeepFinishPlain(res backupzip.PackResult, abs string, level int, k *backupKeep, hl *HistoryLogger) error {
	partial := res.Path
	removeCleanup := globalInterruptHandler.AddCleanup(func() {
		if globalInterruptHandler.IsForceExit() {
			os.Remove(partial)
		}
	})
	defer removeCleanup()

	runStep("verify", "Reading the new backup back before older ones are retired")
	if err := backupzip.Verify(partial, res.Files, res.Bytes, runStopRequested); err != nil {
		os.Remove(partial)
		switch {
		case errors.Is(err, backupzip.ErrStopped):
			return backupStopped("nothing was retired; the set is as it was")
		case errors.Is(err, backupzip.ErrVerify):
			return defectf("the new backup does not read back as it was written (%v); nothing was retired and nothing was left under a final name", err)
		}
		return err
	}
	if err := backupKeepCommit(partial, k, removeCleanup); err != nil {
		return err
	}
	runNumber("archiveBytes", res.Size)
	retired, kept, monthlyKept, rerr := backupKeepRetire(k)
	fmt.Printf("OK %s -> %s (%s, level %d, %s files, %s -> %s)\n", abs, k.finalPath, backupKeepNote(k, retired, kept, monthlyKept), level, formatCount(res.Files), formatBytes(uint64(res.Bytes)), formatBytes(uint64(res.Size)))
	hl.SetResult("archive", k.finalPath)
	return rerr
}
