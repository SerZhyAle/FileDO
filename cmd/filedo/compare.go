package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"filedo/fsx"
)

// fileMeta is one file a compare found. rel is the relative path exactly as
// it is on disk: the map it lives in is keyed case-folded, the way Windows
// matches names, but a delete path built from the folded key missed the file
// or hit another one in a case-sensitive folder (CHK-07).
type fileMeta struct {
	rel  string
	size int64
	mod  time.Time
}

type diffEntry struct {
	relPath string
	srcSize int64
	dstSize int64
}

type CompareResult struct {
	SourceRoot string
	TargetRoot string

	SourceTotalFiles int64
	SourceTotalSize  int64
	TargetTotalFiles int64
	TargetTotalSize  int64

	OnlySourceFiles int64
	OnlySourceSize  int64
	OnlyTargetFiles int64
	OnlyTargetSize  int64

	SameFiles int64
	SameSize  int64

	DiffFiles      int64
	DiffSourceSize int64
	DiffTargetSize int64

	Diffs []diffEntry
}

// folderScan is one side of a compare: its files, and what could not be
// read or told apart.
type folderScan struct {
	files      map[string]fileMeta
	count      int64
	total      int64
	errors     []string        // entries that could not be read (CHK-06)
	collisions map[string]bool // keys two names fold onto (CHK-07)
}

// compareOptions are the flags a compare's delete phase takes.
type compareOptions struct {
	// byHash: a pair is the same file when its content hashes match (and
	// its sizes), whatever the times say.
	byHash bool
	// strict: judge the comparison itself - Passed when both sides hold
	// the same files, Failed when any file is only on one side or differs
	// (SP-0130 R1). Without it the compare stays an act: a difference is
	// information, and the verdict is Done.
	strict bool
	// allowMismatch: delete a pair even when size or time differ.
	allowMismatch bool
	// assumeYes: --yes skips the question before the delete phase, never a
	// safety check (AUD-16-F1).
	assumeYes bool
}

// splitCompareArgs takes the flags out of a compare's extra words.
func splitCompareArgs(extra []string) (compareOptions, []string) {
	var opts compareOptions
	var rest []string
	for _, a := range extra {
		switch strings.ToLower(a) {
		case "--by-hash":
			opts.byHash = true
		case "--strict":
			opts.strict = true
		case "--allow-mismatch":
			opts.allowMismatch = true
		case "--yes", "-y", "--force", "/y":
			opts.assumeYes = true
		default:
			rest = append(rest, a)
		}
	}
	return opts, rest
}

func handleCompareCommand(sourcePath, targetPath string, extraArgs ...string) error {
	// Normalize roots
	src := filepath.Clean(sourcePath)
	dst := filepath.Clean(targetPath)

	// Validate
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return fmt.Errorf("source is not a directory: %s", sourcePath)
	}
	if info, err := os.Stat(dst); err != nil || !info.IsDir() {
		return fmt.Errorf("target is not a directory: %s", targetPath)
	}

	// One folder under two spellings - `X` and `x\`, a junction, subst,
	// `\\localhost\D$`, `\\?\` - or one inside the other: every file would
	// match itself and `del source` deleted all of them (CHK-01).
	overlap, err := pathsOverlap(src, dst)
	if err != nil {
		return fmt.Errorf("compare: cannot resolve %q and %q: %v", sourcePath, targetPath, err)
	}
	if overlap {
		return overlapError("compare", sourcePath, targetPath)
	}

	opts, extra := splitCompareArgs(extraArgs)
	deleteMode, sideOnly := "", ""
	if len(extra) > 0 {
		op := strings.ToLower(extra[0])
		if op == "delete" || op == "del" {
			if len(extra) < 2 {
				return fmt.Errorf("DELETE requires a mode: source|target|old|new|small|big")
			}
			deleteMode = strings.ToLower(extra[1])
			if !compareDeleteModes[deleteMode] {
				return fmt.Errorf("invalid delete mode: %s (allowed: source|target|old|new|small|big)", deleteMode)
			}
			if len(extra) >= 3 {
				s := strings.ToLower(extra[2])
				if s != "source" && s != "target" {
					return fmt.Errorf("invalid side qualifier: %s (allowed: source|target)", s)
				}
				sideOnly = s
			}
		}
	}

	// --strict judges the comparison; a delete phase mutates one side, and
	// a verdict about a snapshot that is about to change is not an answer
	// anyone asked for (SP-0130 R3: the delete phase keeps its own rules).
	if opts.strict && deleteMode != "" {
		return fmt.Errorf("compare: --strict judges the comparison and cannot be combined with a delete phase")
	}

	start := time.Now()
	fmt.Printf("🔍 Comparing folders...\n  Source: %s\n  Target: %s\n\n", src, dst)

	srcScan, dstScan := scanFiles(src), scanFiles(dst)
	res := compareScans(src, dst, srcScan, dstScan)
	// The counts ride the result event in every mode (SP-0130 R2, published
	// as CLI-EVENT-STREAM rule 16): a program reads the comparison from the
	// channel that exists for it, not from this text.
	runNumber("onlyInSource", res.OnlySourceFiles)
	runNumber("onlyInTarget", res.OnlyTargetFiles)
	runNumber("differentFiles", res.DiffFiles)
	runNumber("sameFiles", res.SameFiles)
	runNumber("totalSource", res.SourceTotalFiles)
	runNumber("totalTarget", res.TargetTotalFiles)

	// Print summary to console (pre-delete snapshot)
	fmt.Printf("Summary:\n")
	fmt.Printf("  Only in source: %d files, %s\n", res.OnlySourceFiles, formatBytesShort(uint64(res.OnlySourceSize)))
	fmt.Printf("  Only in target: %d files, %s\n", res.OnlyTargetFiles, formatBytesShort(uint64(res.OnlyTargetSize)))
	fmt.Printf("  Present in both (same size): %d files, %s\n", res.SameFiles, formatBytesShort(uint64(res.SameSize)))
	if res.DiffFiles > 0 {
		fmt.Printf("  Present in both (different size): %d files, src=%s, dst=%s\n", res.DiffFiles, formatBytesShort(uint64(res.DiffSourceSize)), formatBytesShort(uint64(res.DiffTargetSize)))
	} else {
		fmt.Printf("  Present in both (different size): %d files\n", res.DiffFiles)
	}
	fmt.Printf("  Total on source: %d files, %s\n", res.SourceTotalFiles, formatBytesShort(uint64(res.SourceTotalSize)))
	fmt.Printf("  Total on target: %d files, %s\n", res.TargetTotalFiles, formatBytesShort(uint64(res.TargetTotalSize)))

	// What could not be read or told apart makes the comparison incomplete:
	// a file under an unreadable folder shows up as "only on the other
	// side", which is not the truth (CHK-06).
	var problems []string
	for _, side := range []struct {
		name string
		scan *folderScan
	}{{"source", srcScan}, {"target", dstScan}} {
		for i, e := range side.scan.errors {
			if i < 10 {
				fmt.Printf("  Could not read (%s): %s\n", side.name, e)
			}
		}
		if n := len(side.scan.errors); n > 0 {
			problems = append(problems, fmt.Sprintf("%d entries on the %s side could not be read", n, side.name))
		}
		if n := len(side.scan.collisions); n > 0 {
			fmt.Printf("  %d name(s) on the %s side differ only in case - those pairs are not compared or deleted\n", n, side.name)
			problems = append(problems, fmt.Sprintf("%d name(s) on the %s side differ only in case", n, side.name))
		}
	}

	// Optional deletion phase
	if deleteMode != "" {
		delProblems := performDelete(src, dst, deleteMode, sideOnly, opts, srcScan, dstScan)
		problems = append(problems, delProblems...)
	}

	fmt.Printf("\nCompleted in %s\n", formatDuration(time.Since(start)))

	// Write detailed log
	logName := makeCompareLogName()
	if err := writeCompareLog(logName, res, time.Since(start)); err != nil {
		fmt.Printf("Warning: cannot write log file: %v\n", err)
	} else {
		fmt.Printf("Log saved to %s\n", logName)
	}

	if len(problems) > 0 && !runStopRequested() {
		return fmt.Errorf("compare could not verify everything: %s", strings.Join(problems, "; "))
	}
	// SP-0130: with --strict the comparison itself is the verdict. The
	// scans' unreadable entries have already ended the run Not proven
	// above, and a stopped scan judges nothing.
	if opts.strict {
		if runStopRequested() {
			return nil
		}
		return judgeCompareStrict(src, dst, opts, res, srcScan, dstScan)
	}
	return nil
}

// compareStrictRequested reports whether the extra words ask for the judging
// mode. dispatch reads it before the run opens - a strict compare ends
// Passed or Failed, and only a run opened as a judge can - and
// splitCompareArgs reads the same word again to keep it out of the delete
// phase's arguments.
func compareStrictRequested(extra []string) bool {
	for _, a := range extra {
		if strings.EqualFold(a, "--strict") {
			return true
		}
	}
	return false
}

// judgeCompareStrict is --strict's verdict side (SP-0130 R1). A pair is the
// same file when its size and modification time match - the rule the delete
// phase applies - or, with --by-hash, when its content hashes the same,
// whatever the times say. A file only on one side differs, and the numbers
// the result carries are this mode's comparison, so the verdict is the
// numbers' own answer. The scans' unreadable entries were handled by the
// caller; a pair that cannot be hashed ends Not proven here.
func judgeCompareStrict(srcRoot, dstRoot string, opts compareOptions, res *CompareResult, srcScan, dstScan *folderScan) error {
	var samePairs, diffPairs int64
	var diffNames, hashProblems []string
	for key, s := range srcScan.files {
		d, ok := dstScan.files[key]
		if !ok {
			continue // only on the source side: counted already
		}
		eq := s.size == d.size && sameModTime(s.mod, d.mod)
		if opts.byHash && s.size == d.size {
			eqBytes, err := sameContent(filepath.Join(srcRoot, s.rel), filepath.Join(dstRoot, d.rel))
			if runStopRequested() {
				return nil
			}
			if err != nil {
				hashProblems = append(hashProblems, fmt.Sprintf("%s could not be hashed: %v", s.rel, err))
				continue
			}
			eq = eqBytes
		}
		if eq {
			samePairs++
		} else {
			diffPairs++
			diffNames = append(diffNames, s.rel)
		}
	}
	if len(hashProblems) > 0 {
		return fmt.Errorf("compare could not verify everything: %s", strings.Join(hashProblems, "; "))
	}
	// SP-0130 R2: the counts are this mode's comparison, so a consumer can
	// read the verdict off them.
	runNumber("differentFiles", diffPairs)
	runNumber("sameFiles", samePairs)
	if n := len(diffNames); n > 0 {
		sort.Strings(diffNames)
		fmt.Printf("Pairs that differ (--strict):\n")
		for i, rel := range diffNames {
			if i >= 10 {
				fmt.Printf("  .. and %d more\n", n-10)
				break
			}
			fmt.Printf("  %s\n", rel)
		}
	}
	n := res.OnlySourceFiles + res.OnlyTargetFiles + diffPairs
	if n > 0 {
		return recordedDefect("compare-differs",
			fmt.Sprintf("%d file(s) differ between the two trees (--strict)", n),
			map[string]interface{}{
				"onlyInSource":   res.OnlySourceFiles,
				"onlyInTarget":   res.OnlyTargetFiles,
				"differentFiles": diffPairs,
				"sameFiles":      samePairs,
				"totalSource":    res.SourceTotalFiles,
				"totalTarget":    res.TargetTotalFiles,
			})
	}
	fmt.Printf("The trees hold the same files (--strict).\n")
	return nil
}

// compareScans computes the sets of a compare from its two scans.
func compareScans(srcRoot, dstRoot string, srcScan, dstScan *folderScan) *CompareResult {
	res := &CompareResult{
		SourceRoot:       srcRoot,
		TargetRoot:       dstRoot,
		SourceTotalFiles: srcScan.count,
		SourceTotalSize:  srcScan.total,
		TargetTotalFiles: dstScan.count,
		TargetTotalSize:  dstScan.total,
	}

	// Compute sets
	for key, sMeta := range srcScan.files {
		if dMeta, ok := dstScan.files[key]; ok {
			if sMeta.size == dMeta.size {
				res.SameFiles++
				res.SameSize += sMeta.size
			} else {
				res.DiffFiles++
				res.DiffSourceSize += sMeta.size
				res.DiffTargetSize += dMeta.size
				res.Diffs = append(res.Diffs, diffEntry{relPath: sMeta.rel, srcSize: sMeta.size, dstSize: dMeta.size})
			}
		} else {
			res.OnlySourceFiles++
			res.OnlySourceSize += sMeta.size
		}
	}
	for key, dMeta := range dstScan.files {
		if _, ok := srcScan.files[key]; !ok {
			res.OnlyTargetFiles++
			res.OnlyTargetSize += dMeta.size
		}
	}

	// Sort diffs for stable logging
	sort.Slice(res.Diffs, func(i, j int) bool { return res.Diffs[i].relPath < res.Diffs[j].relPath })
	return res
}

// compareFolders scans both roots and compares them.
func compareFolders(srcRoot, dstRoot string) (*CompareResult, error) {
	return compareScans(srcRoot, dstRoot, scanFiles(srcRoot), scanFiles(dstRoot)), nil
}

// scanFiles lists every file under root. Nothing it cannot read is dropped
// silently: it is named in errors.
func scanFiles(root string) *folderScan {
	s := &folderScan{files: make(map[string]fileMeta, 1024), collisions: make(map[string]bool)}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if runStopRequested() {
			return filepath.SkipAll
		}
		if err != nil {
			s.errors = append(s.errors, fmt.Sprintf("%s: %v", path, err))
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			s.errors = append(s.errors, fmt.Sprintf("%s: %v", path, ierr))
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			s.errors = append(s.errors, fmt.Sprintf("%s: %v", path, rerr))
			return nil
		}
		key := strings.ToLower(filepath.ToSlash(rel))
		if _, dup := s.files[key]; dup {
			s.collisions[key] = true
		}
		s.files[key] = fileMeta{rel: rel, size: info.Size(), mod: info.ModTime()}
		s.count++
		s.total += info.Size()
		return nil
	})
	if err != nil {
		s.errors = append(s.errors, fmt.Sprintf("%s: %v", root, err))
	}
	return s
}

func makeCompareLogName() string {
	ts := time.Now().Format("20060102_150405")
	return fmt.Sprintf("compare_report_%s.log", ts)
}

func writeCompareLog(path string, res *CompareResult, took time.Duration) error {
	var b strings.Builder
	b.WriteString("FileDO Compare Report\n")
	b.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Source: %s\n", res.SourceRoot))
	b.WriteString(fmt.Sprintf("Target: %s\n\n", res.TargetRoot))

	b.WriteString("Summary\n")
	b.WriteString(fmt.Sprintf("Only in source: %d files, %s\n", res.OnlySourceFiles, formatBytesShort(uint64(res.OnlySourceSize))))
	b.WriteString(fmt.Sprintf("Only in target: %d files, %s\n", res.OnlyTargetFiles, formatBytesShort(uint64(res.OnlyTargetSize))))
	b.WriteString(fmt.Sprintf("Present in both (same size): %d files, %s\n", res.SameFiles, formatBytesShort(uint64(res.SameSize))))
	if res.DiffFiles > 0 {
		b.WriteString(fmt.Sprintf("Present in both (different size): %d files, src=%s, dst=%s\n", res.DiffFiles, formatBytesShort(uint64(res.DiffSourceSize)), formatBytesShort(uint64(res.DiffTargetSize))))
	} else {
		b.WriteString(fmt.Sprintf("Present in both (different size): %d files\n", res.DiffFiles))
	}
	b.WriteString(fmt.Sprintf("Total on source: %d files, %s\n", res.SourceTotalFiles, formatBytesShort(uint64(res.SourceTotalSize))))
	b.WriteString(fmt.Sprintf("Total on target: %d files, %s\n", res.TargetTotalFiles, formatBytesShort(uint64(res.TargetTotalSize))))
	b.WriteString(fmt.Sprintf("Time: %s\n", formatDuration(took)))

	if len(res.Diffs) > 0 {
		b.WriteString("\nFiles present on both sides with different sizes:\n")
		for _, d := range res.Diffs {
			b.WriteString(fmt.Sprintf("%s | src=%s | dst=%s\n", d.relPath, formatBytesShort(uint64(d.srcSize)), formatBytesShort(uint64(d.dstSize))))
		}
	}

	return os.WriteFile(path, []byte(b.String()), 0644)
}

type delTask struct {
	side  string // "source" or "target"
	rel   string
	abs   string
	other string // the pair's file on the other side
	size  int64
	// hash: delete only if the two files' contents hash the same (--by-hash).
	hash                bool
	selfMeta, otherMeta fileMeta
	// selfID and otherID are the two files' volume serial and file ID, read
	// before the question, so the deletion acts only on the objects the user
	// was shown. A directory listing gives no file ID on ReFS, FAT32 or exFAT,
	// so the scan's own FileInfo cannot carry it (AUD-54-F1).
	selfID, otherID fileID
}

type fileID struct {
	vol uint32
	idx uint64
}

func compareFileID(p string) (fileID, error) {
	vol, idx, err := fileIdentity(p)
	return fileID{vol, idx}, err
}

// A pair selected from the scan must still name the same two unchanged files
// at the deletion boundary. A replacement with the same size and time is not
// the file whose deletion the user approved.
func unchangedCompareFile(path string, scanned fileMeta, id fileID) error {
	fid, err := fsx.FileIDOf(path)
	if err != nil {
		return err
	}
	if !fid.IsRegular || fid.Size != scanned.size || !sameModTime(fid.ModTime, scanned.mod) {
		return fmt.Errorf("file changed since the scan")
	}
	if fid.VolSerial != id.vol || fid.FileIndex != id.idx {
		return fmt.Errorf("file replaced since the scan")
	}
	return nil
}

type delStats struct {
	srcCount int64
	srcBytes int64
	dstCount int64
	dstBytes int64
}

// compareConfirm asks before compare deletes anything. It is clean's question
// (cleanConfirm): a line from stdin, "y" or "yes" is yes, and a closed stdin
// is no answer. A variable so a test can answer.
var compareConfirm = func(prompt string) (answered, yes bool) { return cleanConfirm(prompt) }

var compareDeleteModes = map[string]bool{"source": true, "target": true, "old": true, "new": true, "small": true, "big": true}

// sameContent hashes two files and compares the digests.
var compareHashFile = hashFileSHA256

func sameContent(a, b string) (bool, error) {
	ha, err := compareHashFile(a)
	if err != nil {
		return false, err
	}
	if runStopRequested() {
		return false, errCompareStopped
	}
	hb, err := compareHashFile(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ha, hb), nil
}

// hashFileSHA256 reads the file in 1 MiB blocks and ends at the next block
// once a stop is requested, so a stop never waits for a large file to be read
// to its end (AUD-16-F3).
func hashFileSHA256(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if runStopRequested() {
			return nil, errCompareStopped
		}
		n, err := f.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return h.Sum(nil), nil
}

var errCompareStopped = fmt.Errorf("compare stopped")

// performDelete removes one side of the pairs a compare found, by mode. It
// returns what it could not do.
//
// `del source` and `del target` delete a file because its twin exists on the
// other side, so the twin must be the same file's copy: equal size and equal
// modification time, or equal content with --by-hash. A pair that differs is
// reported and kept - with a truncated copy on one side, deleting the other
// was deleting the only intact original (CHK-05); --allow-mismatch overrides.
// And before any remove, a pair whose two names are one file is skipped
// (CHK-01).
func performDelete(srcRoot, dstRoot, mode string, sideOnly string, opts compareOptions, srcScan, dstScan *folderScan) []string {
	var problems []string
	var mismatches []string

	// Build tasks
	tasks := make([]delTask, 0, 1024)
	for key, s := range srcScan.files {
		d, ok := dstScan.files[key]
		if !ok || srcScan.collisions[key] || dstScan.collisions[key] {
			continue
		}
		srcAbs := filepath.Join(srcRoot, s.rel)
		dstAbs := filepath.Join(dstRoot, d.rel)
		srcTask := delTask{side: "source", rel: s.rel, abs: srcAbs, other: dstAbs, size: s.size, selfMeta: s, otherMeta: d}
		dstTask := delTask{side: "target", rel: d.rel, abs: dstAbs, other: srcAbs, size: d.size, selfMeta: d, otherMeta: s}
		switch mode {
		case "source", "target":
			t := srcTask
			if mode == "target" {
				t = dstTask
			}
			if !opts.allowMismatch {
				if s.size != d.size {
					mismatches = append(mismatches, fmt.Sprintf("%s: size %d vs %d", s.rel, s.size, d.size))
					continue
				}
				if opts.byHash {
					t.hash = true
				} else if !sameModTime(s.mod, d.mod) {
					mismatches = append(mismatches, fmt.Sprintf("%s: modified %s vs %s", s.rel,
						s.mod.Format(time.RFC3339), d.mod.Format(time.RFC3339)))
					continue
				}
			}
			tasks = append(tasks, t)
		case "old":
			if s.mod.Before(d.mod) {
				if sideOnly == "" || sideOnly == "source" {
					tasks = append(tasks, srcTask)
				}
			} else if d.mod.Before(s.mod) {
				if sideOnly == "" || sideOnly == "target" {
					tasks = append(tasks, dstTask)
				}
			}
		case "new":
			if s.mod.After(d.mod) {
				if sideOnly == "" || sideOnly == "source" {
					tasks = append(tasks, srcTask)
				}
			} else if d.mod.After(s.mod) {
				if sideOnly == "" || sideOnly == "target" {
					tasks = append(tasks, dstTask)
				}
			}
		case "small":
			if s.size < d.size {
				if sideOnly == "" || sideOnly == "source" {
					tasks = append(tasks, srcTask)
				}
			} else if d.size < s.size {
				if sideOnly == "" || sideOnly == "target" {
					tasks = append(tasks, dstTask)
				}
			}
		case "big":
			if s.size > d.size {
				if sideOnly == "" || sideOnly == "source" {
					tasks = append(tasks, srcTask)
				}
			} else if d.size > s.size {
				if sideOnly == "" || sideOnly == "target" {
					tasks = append(tasks, dstTask)
				}
			}
		}
	}

	// Read both files' identity now, before the question: what is deleted
	// later must be the object listed here, not a replacement with the same
	// size and time. A pair whose identity cannot be read is kept.
	type pairIDResult struct {
		idx     int
		selfID  fileID
		otherID fileID
		err     error
	}
	idCh := make(chan int, 256)
	resCh := make(chan pairIDResult, 256)
	var idWg sync.WaitGroup
	idWorkers := 8
	if len(tasks) < idWorkers {
		idWorkers = len(tasks)
	}
	for i := 0; i < idWorkers; i++ {
		idWg.Add(1)
		go func() {
			defer idWg.Done()
			for idx := range idCh {
				t := tasks[idx]
				self, err := compareFileID(t.abs)
				var other fileID
				if err == nil {
					other, err = compareFileID(t.other)
				}
				resCh <- pairIDResult{idx: idx, selfID: self, otherID: other, err: err}
			}
		}()
	}
	go func() {
		for i := range tasks {
			if runStopRequested() {
				break
			}
			idCh <- i
		}
		close(idCh)
		idWg.Wait()
		close(resCh)
	}()

	results := make([]pairIDResult, len(tasks))
	for res := range resCh {
		results[res.idx] = res
	}
	if runStopRequested() {
		return append(problems, "stopped before the delete: nothing deleted")
	}
	identified := tasks[:0]
	var unidentified []string
	for i, t := range tasks {
		res := results[i]
		if res.err != nil {
			unidentified = append(unidentified, fmt.Sprintf("%s: cannot identify the pair: %v", t.rel, res.err))
			continue
		}
		t.selfID = res.selfID
		t.otherID = res.otherID
		identified = append(identified, t)
	}
	tasks = identified
	if len(unidentified) > 0 {
		sort.Strings(unidentified)
		fmt.Printf("Kept %d pair(s) whose files could not be identified:\n", len(unidentified))
		for i, u := range unidentified {
			if i >= 10 {
				fmt.Printf("  .. and %d more\n", len(unidentified)-10)
				break
			}
			fmt.Printf("  %s\n", u)
		}
		problems = append(problems, fmt.Sprintf("%d pair(s) could not be identified and were not deleted", len(unidentified)))
	}

	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		fmt.Printf("Kept %d pair(s) whose two files differ (use --by-hash to compare content, --allow-mismatch to delete anyway):\n", len(mismatches))
		for i, m := range mismatches {
			if i >= 10 {
				fmt.Printf("  .. and %d more\n", len(mismatches)-10)
				break
			}
			fmt.Printf("  %s\n", m)
		}
		problems = append(problems, fmt.Sprintf("%d pair(s) differ in size or time and were not deleted", len(mismatches)))
	}

	if len(tasks) == 0 {
		fmt.Printf("No files to delete for mode '%s'.\n", mode)
		return problems
	}

	// The delete phase asks, the way clean does: the count and the rule, a
	// few of the names, then y/N. --yes skips the question and nothing else
	// (the pair checks below still run); a closed stdin - the GUI's, a
	// script's - is no answer, so nothing is deleted and the run is
	// Not proven (AUD-16-F1).
	modeText := strings.ToUpper(mode) + " mode"
	if sideOnly != "" {
		modeText += ", " + strings.ToUpper(sideOnly) + " only"
	}
	fmt.Printf("\nCompare will permanently delete %d files (%s):\n", len(tasks), modeText)
	for i, t := range tasks {
		if i == 10 {
			fmt.Printf("  .. and %d more\n", len(tasks)-10)
			break
		}
		fmt.Printf("  %s: %s\n", t.side, t.rel)
	}
	if opts.assumeYes {
		fmt.Printf("Confirmation skipped (--yes).\n")
	} else {
		answered, yes := compareConfirm(fmt.Sprintf("\nDelete these %d files (%s)? (y/N): ", len(tasks), modeText))
		if !answered {
			return append(problems, fmt.Sprintf("the delete needs a confirmation: %d files listed (%s), nothing deleted - pass --yes to delete them without a prompt", len(tasks), modeText))
		}
		if !yes {
			fmt.Printf("Nothing deleted.\n")
			return problems
		}
	}

	fmt.Printf("Deleting %d files (%s mode)...\n", len(tasks), strings.ToUpper(mode))
	start := time.Now()

	// Run workers
	workerCount := 4
	tasksCh := make(chan delTask, 256)
	var deleted []delTask
	var errs []string
	var stats delStats
	var mu sync.Mutex
	fail := func(t delTask, format string, args ...interface{}) {
		mu.Lock()
		errs = append(errs, fmt.Sprintf("%s: %s (%s)", t.side, t.rel, fmt.Sprintf(format, args...)))
		mu.Unlock()
	}

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasksCh {
				// After a stop the rest of the queue is drained unread so the
				// sender below is never left on a full channel (AUD-54-F2).
				if runStopRequested() {
					continue
				}
				// The two names of a pair must be two files.
				if t.selfID.vol == t.otherID.vol && t.selfID.idx == t.otherID.idx {
					fail(t, "both sides are one file - not deleted")
					continue
				}
				if t.hash {
					if runStopRequested() {
						continue
					}
					eq, err := sameContent(t.abs, t.other)
					if runStopRequested() {
						continue
					}
					if err != nil {
						fail(t, "cannot hash the pair: %v", err)
						continue
					}
					if !eq {
						fail(t, "content differs - not deleted")
						continue
					}
				}
				if runStopRequested() {
					continue
				}
				if err := unchangedCompareFile(t.abs, t.selfMeta, t.selfID); err != nil {
					fail(t, "delete candidate changed: %v", err)
					continue
				}
				if err := unchangedCompareFile(t.other, t.otherMeta, t.otherID); err != nil {
					fail(t, "other file changed: %v", err)
					continue
				}
				if runStopRequested() {
					continue
				}
				if err := os.Remove(t.abs); err != nil {
					fail(t, "%v", err)
					continue
				}
				mu.Lock()
				deleted = append(deleted, t)
				if t.side == "source" {
					stats.srcCount++
					stats.srcBytes += t.size
				} else {
					stats.dstCount++
					stats.dstBytes += t.size
				}
				mu.Unlock()
			}
		}()
	}

	for _, t := range tasks {
		if runStopRequested() {
			break
		}
		tasksCh <- t
	}
	close(tasksCh)
	wg.Wait()

	// Summary output
	fmt.Printf("Deleted from source: %d files, %s\n", stats.srcCount, formatBytesShort(uint64(stats.srcBytes)))
	fmt.Printf("Deleted from target: %d files, %s\n", stats.dstCount, formatBytesShort(uint64(stats.dstBytes)))
	fmt.Printf("Completed in %s\n", formatDuration(time.Since(start)))
	if len(errs) > 0 {
		sort.Strings(errs)
		fmt.Printf("Not deleted: %d (see log)\n", len(errs))
		for i, e := range errs {
			if i >= 10 {
				break
			}
			fmt.Printf("  %s\n", e)
		}
		problems = append(problems, fmt.Sprintf("%d file(s) could not be deleted", len(errs)))
	}

	// Write delete log
	if err := writeDeleteLog(srcRoot, dstRoot, mode, sideOnly, deleted, append(append(errs, mismatches...), unidentified...), stats, time.Since(start)); err != nil {
		fmt.Printf("Warning: cannot write delete log: %v\n", err)
	}
	return problems
}

func writeDeleteLog(srcRoot, dstRoot, mode, sideOnly string, deleted []delTask, errs []string, stats delStats, took time.Duration) error {
	ts := time.Now().Format("20060102_150405")
	suffix := strings.ToLower(mode)
	if sideOnly != "" {
		suffix += "_" + sideOnly
	}
	name := fmt.Sprintf("delete_report_%s_%s.log", suffix, ts)
	var b strings.Builder
	b.WriteString("FileDO Delete Report\n")
	if sideOnly != "" {
		b.WriteString(fmt.Sprintf("Mode: %s (%s only)\n", strings.ToUpper(mode), strings.ToUpper(sideOnly)))
	} else {
		b.WriteString(fmt.Sprintf("Mode: %s\n", strings.ToUpper(mode)))
	}
	b.WriteString(fmt.Sprintf("Generated: %s\n", time.Now().Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("Source: %s\nTarget: %s\n", srcRoot, dstRoot))
	b.WriteString(fmt.Sprintf("Time: %s\n\n", formatDuration(took)))
	b.WriteString("Summary\n")
	b.WriteString(fmt.Sprintf("Deleted from source: %d files, %s\n", stats.srcCount, formatBytesShort(uint64(stats.srcBytes))))
	b.WriteString(fmt.Sprintf("Deleted from target: %d files, %s\n", stats.dstCount, formatBytesShort(uint64(stats.dstBytes))))
	if len(deleted) > 0 {
		b.WriteString("\nDeleted files:\n")
		for _, d := range deleted {
			b.WriteString(fmt.Sprintf("%s | %s | %s\n", d.side, d.rel, formatBytesShort(uint64(d.size))))
		}
	}
	if len(errs) > 0 {
		b.WriteString("\nNot deleted:\n")
		for _, e := range errs {
			b.WriteString(e + "\n")
		}
	}
	if err := os.WriteFile(name, []byte(b.String()), 0644); err != nil {
		return err
	}
	fmt.Printf("Delete log saved to %s\n", name)
	return nil
}
