package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"filedo/backupzip"
	"filedo/rehash"
)

// Native-format hash change, in place (FILEDO-0242):
//
//	filedo rehash <file|folder|drive|\\server\share\folder> [nohist]
//
// Rewrites eligible regular files: a format-valid non-content edit verified by
// a strict reader, a different whole-file SHA-256 and a nonzero size delta
// within each format's documented byte bound. The new bytes are completed in a
// partial file and then replace the original; nothing else is stored, and the
// same path can be processed again any number of times. Unsupported,
// malformed, read-only or digitally signed files are skipped with an explicit
// reason and stay as they were.

type rehashOpts struct {
	src string
}

func parseRehashArgs(args []string) (*rehashOpts, error) {
	var pos []string
	for _, t := range args {
		switch strings.ToLower(t) {
		case "nohist", "no_history":
			continue
		}
		pos = append(pos, t)
	}
	if len(pos) != 1 {
		return nil, exitClassf(10, "rehash takes one path and rewrites its files in place: filedo rehash <file|folder|drive>")
	}
	return &rehashOpts{src: pos[0]}, nil
}

func handleRehashCommand(args []string, hl *HistoryLogger) error {
	o, err := parseRehashArgs(args)
	target := ""
	if o != nil {
		target = o.src
	}
	hl.SetCommand("rehash", target, "rehash")
	beginRun(runActs, "rehash", target, append([]string{"rehash"}, args...))
	if err != nil {
		return err
	}
	return runRehash(o, hl)
}

func rehashContext() context.Context {
	if globalInterruptHandler != nil {
		return globalInterruptHandler.Context()
	}
	return context.Background()
}

func isVolumeRootSystemEntry(name string) bool {
	if strings.EqualFold(name, "$RECYCLE.BIN") {
		return true
	}
	for _, s := range backupzip.SystemFiles {
		if strings.EqualFold(s, name) {
			return true
		}
	}
	return false
}

// refuseRehashSystemPath refuses a target inside, or containing, a folder whose
// files must not be rewritten: Windows, Program Files and FileDO's own data.
func refuseRehashSystemPath(path string) error {
	for _, pp := range wipeProtectedPaths() {
		if !pp.inside {
			continue
		}
		if _, err := os.Lstat(pp.path); errors.Is(err, os.ErrNotExist) {
			continue
		}
		inside, err := pathWithin(path, pp.path)
		if err != nil {
			return fmt.Errorf("rehash: cannot resolve %q against %q: %w", path, pp.path, err)
		}
		contains, err := pathWithin(pp.path, path)
		if err != nil {
			return fmt.Errorf("rehash: cannot resolve %q against %q: %w", pp.path, path, err)
		}
		if inside || contains {
			return exitClassf(10, "rehash: %q is inside or contains %s (%s); its files are not rewritten", path, pp.label, pp.path)
		}
	}
	return nil
}

type rehashStats struct {
	processed          int64
	skippedUnsupported int64
	skippedOther       int64
	failed             int64
	bytesRead          int64
	bytesWritten       int64
	causes             causeKeeper
}

func (s *rehashStats) skippedTotal() int64 {
	return s.skippedUnsupported + s.skippedOther
}

func (s *rehashStats) countSkip(code string) {
	switch code {
	case "unsupported-format", "signed-file", "invalid-format":
		s.skippedUnsupported++
	default:
		s.skippedOther++
	}
}

func (s *rehashStats) recordNumbers() {
	runNumber("processedFiles", s.processed)
	runNumber("skippedFiles", s.skippedTotal())
	runNumber("skippedUnsupported", s.skippedUnsupported)
	runNumber("skippedOther", s.skippedOther)
	runNumber("failedFiles", s.failed)
	runNumber("bytesRead", s.bytesRead)
	runNumber("bytesWritten", s.bytesWritten)
}

func runRehash(o *rehashOpts, hl *HistoryLogger) error {
	if runStopRequested() {
		fmt.Println("Stopped before starting - nothing was changed.")
		return nil
	}

	absSrc, err := filepath.Abs(deviceRootPath(o.src))
	if err != nil {
		return err
	}

	srcInfo, err := os.Lstat(absSrc)
	if err != nil {
		return preRunError(fmt.Errorf("source %q cannot be read: %w", absSrc, err))
	}
	if srcInfo.Mode()&fs.ModeSymlink != 0 || fdsecRefusedReparse(absSrc) {
		return exitClassf(10, "rehash: source %q is a junction or symbolic link; links are not followed - pass the real target path instead", absSrc)
	}
	if srcInfo.IsDir() {
		if err := refuseLinkedSourceRoot("rehash", absSrc); err != nil {
			return err
		}
	} else if !srcInfo.Mode().IsRegular() {
		return exitClassf(10, "rehash: source %q is not a regular file or folder", absSrc)
	}
	if err := refuseRehashSystemPath(absSrc); err != nil {
		return err
	}

	if !srcInfo.IsDir() {
		return runRehashSingleFile(absSrc, hl)
	}
	// Directory or mounted data volume tree.
	return runRehashTree(absSrc, hl)
}

func runRehashSingleFile(absSrc string, hl *HistoryLogger) error {
	ctx := rehashContext()

	runStep("rehash", "Rehashing single file")
	var stats rehashStats
	res, err := rehash.RewriteFile(ctx, absSrc)
	if err != nil {
		if runStopRequested() || errors.Is(err, context.Canceled) {
			stats.recordNumbers()
			fmt.Println("Stopped before completion - the file was not changed.")
			return nil
		}
		if se, ok := rehash.IsSkip(err); ok {
			stats.countSkip(se.Code)
			fmt.Printf("SKIPPED: %s (%s)\n", filepath.Base(absSrc), se.Reason)
			stats.recordNumbers()
			hl.SetResultSummary(fmt.Sprintf("Rehashed: 0, Skipped: 1 (%s)", se.Code))
			return nil
		}
		stats.failed++
		stats.causes.keep(err)
		stats.recordNumbers()
		return stats.causes.classify(fmt.Errorf("rehash failed for %s: %w", absSrc, err))
	}

	stats.processed = 1
	stats.bytesRead = res.OriginalSize
	stats.bytesWritten = res.OutputSize
	stats.recordNumbers()

	fmt.Printf("REHASHED: %s [%s, %+d B, SHA-256 %s.. -> %s..]\n",
		absSrc, res.Format, res.SizeDelta,
		hex.EncodeToString(res.OriginalSHA256[:6]),
		hex.EncodeToString(res.OutputSHA256[:6]))
	hl.SetResultSummary(fmt.Sprintf("Rehashed: 1 (%s, %+d B)", res.Format, res.SizeDelta))
	return nil
}

type rehashItem struct {
	rel  string
	src  string
	size int64
}

func runRehashTree(absSrc string, hl *HistoryLogger) error {
	ctx := rehashContext()
	rootVol := backupIsRoot(absSrc)
	var stats rehashStats

	runStep("scan", "Scanning source tree")
	var items []rehashItem
	var totalBytes int64

	var walk func(dir, rel string) error
	walk = func(dir, rel string) error {
		if runStopRequested() || ctx.Err() != nil {
			return context.Canceled
		}
		des, err := os.ReadDir(dir)
		if err != nil {
			stats.failed++
			stats.causes.keep(err)
			fmt.Printf("FAILED: %s (cannot read directory: %v)\n", dir, err)
			return nil
		}
		sort.Slice(des, func(i, j int) bool { return des[i].Name() < des[j].Name() })
		losers := caseCollisionLosers(dir)

		for _, de := range des {
			if runStopRequested() || ctx.Err() != nil {
				return context.Canceled
			}
			name := de.Name()
			full := filepath.Join(dir, name)
			itemRel := name
			if rel != "" {
				itemRel = filepath.Join(rel, name)
			}

			if rel == "" && rootVol && isVolumeRootSystemEntry(name) {
				stats.skippedOther++
				fmt.Printf("SKIPPED: %s (volume system entry)\n", itemRel)
				continue
			}

			if winner, lost := losers[full]; lost {
				stats.skippedOther++
				fmt.Printf("SKIPPED: %s (case-insensitive name collision with %s)\n", itemRel, filepath.Base(winner))
				continue
			}

			fi, lerr := os.Lstat(full)
			if lerr != nil {
				stats.failed++
				stats.causes.keep(lerr)
				fmt.Printf("FAILED: %s (cannot stat: %v)\n", itemRel, lerr)
				continue
			}
			if fi.Mode()&fs.ModeSymlink != 0 || fdsecRefusedReparse(full) {
				stats.skippedOther++
				fmt.Printf("SKIPPED: %s (reparse point or link not followed)\n", itemRel)
				continue
			}
			if fi.IsDir() {
				if err := walk(full, itemRel); err != nil {
					return err
				}
				continue
			}
			if isPartialName(name) {
				stats.skippedOther++
				fmt.Printf("SKIPPED: %s (unfinished partial file)\n", itemRel)
				continue
			}
			if !fi.Mode().IsRegular() {
				stats.skippedOther++
				fmt.Printf("SKIPPED: %s (not a regular file)\n", itemRel)
				continue
			}

			items = append(items, rehashItem{rel: itemRel, src: full, size: fi.Size()})
			totalBytes += fi.Size()
		}
		return nil
	}

	if err := walk(absSrc, ""); err != nil && (runStopRequested() || errors.Is(err, context.Canceled)) {
		stats.recordNumbers()
		fmt.Println("\nStopped during scan - nothing was changed.")
		return nil
	}

	runStep("rehash", fmt.Sprintf("Processing %d regular file(s)", len(items)))
	started := time.Now()
	totalItems := int64(len(items))
	var doneItems int64
	var doneBytes int64

	stopped := func(done int64) error {
		stats.recordNumbers()
		fmt.Printf("\nStopped after %d/%d file(s) - rehashed files are complete, the rest were not changed.\n",
			done, totalItems)
		return nil
	}

	for _, it := range items {
		if runStopRequested() || ctx.Err() != nil {
			return stopped(doneItems)
		}

		res, err := rehash.RewriteFile(ctx, it.src)
		doneItems++
		if err != nil {
			if runStopRequested() || errors.Is(err, context.Canceled) {
				return stopped(doneItems - 1)
			}
			if se, ok := rehash.IsSkip(err); ok {
				stats.countSkip(se.Code)
				fmt.Printf("SKIPPED: %s (%s)\n", it.rel, se.Reason)
			} else {
				stats.failed++
				stats.causes.keep(err)
				fmt.Printf("FAILED: %s (%v)\n", it.rel, err)
			}
		} else {
			stats.processed++
			stats.bytesRead += res.OriginalSize
			stats.bytesWritten += res.OutputSize
			doneBytes += res.OutputSize
			fmt.Printf("REHASHED: %s [%s, %+d B]\n", it.rel, res.Format, res.SizeDelta)
		}

		elapsed := time.Since(started).Seconds()
		speed := 0.0
		if elapsed > 0 {
			speed = float64(doneBytes) / elapsed
		}
		EmitProgressEvent(doneItems, totalItems, doneBytes, totalBytes, speed, it.rel)
	}

	stats.recordNumbers()
	fmt.Printf("\nrehash summary: %d rehashed, %d skipped (%d unsupported/invalid/signed, %d other), %d failed\n",
		stats.processed, stats.skippedTotal(), stats.skippedUnsupported, stats.skippedOther, stats.failed)
	hl.SetResultSummary(fmt.Sprintf("Rehashed: %d, Skipped: %d, Failed: %d", stats.processed, stats.skippedTotal(), stats.failed))

	if stats.failed > 0 {
		summary := fmt.Sprintf("rehash completed with %d failed file(s)", stats.failed)
		return stats.causes.classify(errors.New(summary))
	}
	return nil
}
