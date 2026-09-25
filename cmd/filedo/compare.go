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
    // allowMismatch: delete a pair even when size or time differ.
    allowMismatch bool
}

// splitCompareArgs takes the flags out of a compare's extra words.
func splitCompareArgs(extra []string) (compareOptions, []string) {
    var opts compareOptions
    var rest []string
    for _, a := range extra {
        switch strings.ToLower(a) {
        case "--by-hash":
            opts.byHash = true
        case "--allow-mismatch":
            opts.allowMismatch = true
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

    start := time.Now()
    fmt.Printf("🔍 Comparing folders...\n  Source: %s\n  Target: %s\n\n", src, dst)

    srcScan, dstScan := scanFiles(src), scanFiles(dst)
    res := compareScans(src, dst, srcScan, dstScan)

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
    hash bool
}

type delStats struct {
    srcCount int64
    srcBytes int64
    dstCount int64
    dstBytes int64
}

var compareDeleteModes = map[string]bool{"source": true, "target": true, "old": true, "new": true, "small": true, "big": true}

// sameContent hashes two files and compares the digests.
func sameContent(a, b string) (bool, error) {
    ha, err := hashFileSHA256(a)
    if err != nil {
        return false, err
    }
    hb, err := hashFileSHA256(b)
    if err != nil {
        return false, err
    }
    return bytes.Equal(ha, hb), nil
}

func hashFileSHA256(p string) ([]byte, error) {
    f, err := os.Open(p)
    if err != nil {
        return nil, err
    }
    defer f.Close()
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil {
        return nil, err
    }
    return h.Sum(nil), nil
}

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
        srcTask := delTask{side: "source", rel: s.rel, abs: srcAbs, other: dstAbs, size: s.size}
        dstTask := delTask{side: "target", rel: d.rel, abs: dstAbs, other: srcAbs, size: d.size}
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
                // The two names of a pair must be two files.
                same, err := sameFilePaths(t.abs, t.other)
                if err != nil {
                    fail(t, "cannot tell the pair apart: %v", err)
                    continue
                }
                if same {
                    fail(t, "both sides are one file - not deleted")
                    continue
                }
                if t.hash {
                    eq, err := sameContent(t.abs, t.other)
                    if err != nil {
                        fail(t, "cannot hash the pair: %v", err)
                        continue
                    }
                    if !eq {
                        fail(t, "content differs - not deleted")
                        continue
                    }
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
    if err := writeDeleteLog(srcRoot, dstRoot, mode, sideOnly, deleted, append(errs, mismatches...), stats, time.Since(start)); err != nil {
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
