package main

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "sort"
    "strings"
    "sync"
    "time"
)

type fileMeta struct {
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

    start := time.Now()
    fmt.Printf("🔍 Comparing folders...\n  Source: %s\n  Target: %s\n\n", src, dst)

    res, err := compareFolders(src, dst)
    if err != nil {
        return err
    }

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

    // Optional deletion phase
    if len(extraArgs) > 0 {
        op := strings.ToLower(extraArgs[0])
        if op == "delete" || op == "del" {
            if len(extraArgs) < 2 {
                return fmt.Errorf("DELETE requires a mode: source|target|old|new|small|big")
            }
            mode := strings.ToLower(extraArgs[1])
            var sideOnly string
            if len(extraArgs) >= 3 {
                s := strings.ToLower(extraArgs[2])
                if s == "source" || s == "target" {
                    sideOnly = s
                } else {
                    return fmt.Errorf("invalid side qualifier: %s (allowed: source|target)", s)
                }
            }
            if err := performDelete(src, dst, mode, sideOnly); err != nil {
                return err
            }
        }
    }

    fmt.Printf("\nCompleted in %s\n", formatDuration(time.Since(start)))

    // Write detailed log
    logName := makeCompareLogName()
    if err := writeCompareLog(logName, res, time.Since(start)); err != nil {
        fmt.Printf("Warning: cannot write log file: %v\n", err)
    } else {
        fmt.Printf("Log saved to %s\n", logName)
    }

    return nil
}

func compareFolders(srcRoot, dstRoot string) (*CompareResult, error) {
    srcMap, srcFiles, srcSize, err := scanFiles(srcRoot)
    if err != nil {
        return nil, err
    }
    dstMap, dstFiles, dstSize, err := scanFiles(dstRoot)
    if err != nil {
        return nil, err
    }

    res := &CompareResult{
        SourceRoot:       srcRoot,
        TargetRoot:       dstRoot,
        SourceTotalFiles: srcFiles,
        SourceTotalSize:  srcSize,
        TargetTotalFiles: dstFiles,
        TargetTotalSize:  dstSize,
    }

    // Compute sets
    for rel, sMeta := range srcMap {
        if dMeta, ok := dstMap[rel]; ok {
            if sMeta.size == dMeta.size {
                res.SameFiles++
                res.SameSize += sMeta.size
            } else {
                res.DiffFiles++
                res.DiffSourceSize += sMeta.size
                res.DiffTargetSize += dMeta.size
                res.Diffs = append(res.Diffs, diffEntry{relPath: rel, srcSize: sMeta.size, dstSize: dMeta.size})
            }
        } else {
            res.OnlySourceFiles++
            res.OnlySourceSize += sMeta.size
        }
    }
    for rel, dMeta := range dstMap {
        if _, ok := srcMap[rel]; !ok {
            res.OnlyTargetFiles++
            res.OnlyTargetSize += dMeta.size
        }
    }

    // Sort diffs for stable logging
    sort.Slice(res.Diffs, func(i, j int) bool { return res.Diffs[i].relPath < res.Diffs[j].relPath })
    return res, nil
}

func scanFiles(root string) (map[string]fileMeta, int64, int64, error) {
    m := make(map[string]fileMeta, 1024)
    var files int64
    var total int64
    normalizeKey := func(rel string) string {
        p := filepath.ToSlash(rel)
        if runtime.GOOS == "windows" {
            p = strings.ToLower(p)
        }
        return p
    }

    err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
        if err != nil {
            // Skip entries we cannot read
            return nil
        }
        if d.IsDir() {
            return nil
        }
    info, ierr := d.Info()
        if ierr != nil {
            return nil
        }
        rel, rerr := filepath.Rel(root, path)
        if rerr != nil {
            return nil
        }
    key := normalizeKey(rel)
    m[key] = fileMeta{size: info.Size(), mod: info.ModTime()}
        files++
        total += info.Size()
        return nil
    })
    if err != nil {
        return nil, 0, 0, err
    }
    return m, files, total, nil
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
    side   string // "source" or "target"
    rel    string
    abs    string
    size   int64
}

type delStats struct {
    srcCount int64
    srcBytes int64
    dstCount int64
    dstBytes int64
}

func performDelete(srcRoot, dstRoot, mode string, sideOnly string) error {
    allowed := map[string]bool{"source": true, "target": true, "old": true, "new": true, "small": true, "big": true}
    if !allowed[mode] {
        return fmt.Errorf("invalid delete mode: %s (allowed: source|target|old|new|small|big)", mode)
    }

    // Scan both sides using same normalization as compare
    srcMap, _, _, err := scanFiles(srcRoot)
    if err != nil {
        return err
    }
    dstMap, _, _, err := scanFiles(dstRoot)
    if err != nil {
        return err
    }

    // Build tasks
    tasks := make([]delTask, 0, 1024)
    for rel, s := range srcMap {
        if d, ok := dstMap[rel]; ok {
            switch mode {
            case "source":
                tasks = append(tasks, delTask{side: "source", rel: rel, abs: filepath.Join(srcRoot, rel), size: s.size})
            case "target":
                tasks = append(tasks, delTask{side: "target", rel: rel, abs: filepath.Join(dstRoot, rel), size: d.size})
            case "old":
                if s.mod.Before(d.mod) {
                    if sideOnly == "" || sideOnly == "source" {
                        tasks = append(tasks, delTask{side: "source", rel: rel, abs: filepath.Join(srcRoot, rel), size: s.size})
                    }
                } else if d.mod.Before(s.mod) {
                    if sideOnly == "" || sideOnly == "target" {
                        tasks = append(tasks, delTask{side: "target", rel: rel, abs: filepath.Join(dstRoot, rel), size: d.size})
                    }
                }
            case "new":
                if s.mod.After(d.mod) {
                    if sideOnly == "" || sideOnly == "source" {
                        tasks = append(tasks, delTask{side: "source", rel: rel, abs: filepath.Join(srcRoot, rel), size: s.size})
                    }
                } else if d.mod.After(s.mod) {
                    if sideOnly == "" || sideOnly == "target" {
                        tasks = append(tasks, delTask{side: "target", rel: rel, abs: filepath.Join(dstRoot, rel), size: d.size})
                    }
                }
            case "small":
                if s.size < d.size {
                    if sideOnly == "" || sideOnly == "source" {
                        tasks = append(tasks, delTask{side: "source", rel: rel, abs: filepath.Join(srcRoot, rel), size: s.size})
                    }
                } else if d.size < s.size {
                    if sideOnly == "" || sideOnly == "target" {
                        tasks = append(tasks, delTask{side: "target", rel: rel, abs: filepath.Join(dstRoot, rel), size: d.size})
                    }
                }
            case "big":
                if s.size > d.size {
                    if sideOnly == "" || sideOnly == "source" {
                        tasks = append(tasks, delTask{side: "source", rel: rel, abs: filepath.Join(srcRoot, rel), size: s.size})
                    }
                } else if d.size > s.size {
                    if sideOnly == "" || sideOnly == "target" {
                        tasks = append(tasks, delTask{side: "target", rel: rel, abs: filepath.Join(dstRoot, rel), size: d.size})
                    }
                }
            }
        }
    }

    if len(tasks) == 0 {
        fmt.Printf("No files to delete for mode '%s'.\n", mode)
        return nil
    }

    fmt.Printf("Deleting %d files (%s mode)...\n", len(tasks), strings.ToUpper(mode))
    start := time.Now()

    // Run workers
    workerCount := 4
    tasksCh := make(chan delTask, 256)
    var deleted []delTask
    var errors []string
    var stats delStats
    var mu sync.Mutex

    var wg sync.WaitGroup
    for i := 0; i < workerCount; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for t := range tasksCh {
                if err := os.Remove(t.abs); err != nil {
                    mu.Lock()
                    errors = append(errors, fmt.Sprintf("%s: %s (%v)", t.side, t.rel, err))
                    mu.Unlock()
                } else {
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
            }
        }()
    }

    for _, t := range tasks {
        tasksCh <- t
    }
    close(tasksCh)
    wg.Wait()

    // Summary output
    fmt.Printf("Deleted from source: %d files, %s\n", stats.srcCount, formatBytesShort(uint64(stats.srcBytes)))
    fmt.Printf("Deleted from target: %d files, %s\n", stats.dstCount, formatBytesShort(uint64(stats.dstBytes)))
    fmt.Printf("Completed in %s\n", formatDuration(time.Since(start)))
    if len(errors) > 0 {
        fmt.Printf("Errors: %d (see log)\n", len(errors))
    }

    // Write delete log
    if err := writeDeleteLog(srcRoot, dstRoot, mode, sideOnly, deleted, errors, stats, time.Since(start)); err != nil {
        fmt.Printf("Warning: cannot write delete log: %v\n", err)
    }
    return nil
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
        b.WriteString("\nErrors:\n")
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
