package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "math"
    "os"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "time"
)

const (
    checkReadDelayThreshold = 2 * time.Second
    checkWarmupGrace        = 10 * time.Second
    checkRetryWindow        = 500 * time.Millisecond
    checkRetrySleep         = 50 * time.Millisecond
)

type checkMode int

const (
    modeQuick checkMode = iota
    modeBalanced
    modeDeep
)

type checkConfig struct {
    threshold     time.Duration
    warmupGrace   time.Duration
    warmupIdle    time.Duration
    workers       int
    bufSize       int
    mode          checkMode
    balancedMinMB int64
    minSizeBytes  int64
    maxSizeBytes  int64
    includeExt    map[string]bool
    excludeExt    map[string]bool
    maxFiles      int64
    maxDuration   time.Duration
    precount      bool
    dryRun        bool
    verbose       bool
    quiet         bool
    resume        bool
    report        string // "", "csv", "json"
    reportFile    string
    hddSleepMs    int
}

type checkJob struct {
    path string
    size int64
    vol  string
}

type volumeWarmup struct {
    used bool
    last time.Time
}

func getenvDefault(name, def string) string {
    v := os.Getenv(name)
    if v == "" {
        return def
    }
    return v
}

func getEnvFloat(name string, def float64) float64 {
    v := os.Getenv(name)
    if v == "" {
        return def
    }
    f, err := strconv.ParseFloat(v, 64)
    if err != nil {
        return def
    }
    return f
}

func getEnvInt(name string, def int) int {
    v := os.Getenv(name)
    if v == "" {
        return def
    }
    n, err := strconv.Atoi(v)
    if err != nil {
        return def
    }
    return n
}

func parseExtSet(s string) map[string]bool {
    if s == "" {
        return nil
    }
    m := make(map[string]bool)
    for _, p := range strings.Split(s, ",") {
        e := strings.TrimSpace(strings.ToLower(p))
        if e == "" {
            continue
        }
        if !strings.HasPrefix(e, ".") {
            e = "." + e
        }
        m[e] = true
    }
    return m
}

func toBytesMBEnv(val float64) int64 { return int64(val * 1024.0 * 1024.0) }

func detectMode() checkMode {
    m := strings.ToLower(os.Getenv("FILEDO_CHECK_MODE"))
    switch m {
    case "balanced":
        return modeBalanced
    case "deep":
        return modeDeep
    default:
        return modeQuick
    }
}

func volumeOf(path string) string {
    if len(path) >= 2 && path[1] == ':' {
        return strings.ToUpper(string(path[0]))
    }
    if strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//") {
        return "NET"
    }
    return ""
}

func decideWorkers(root string, cfg *checkConfig) int {
    if cfg.workers > 0 {
        return cfg.workers
    }
    vol := volumeOf(root)
    if vol != "" && vol != "NET" {
        if info, err := AnalyzeDrive(vol); err == nil && info != nil {
            switch info.DriveType {
            case DriveTypeHDD:
                return 3
            case DriveTypeSSD:
                return 8
            case DriveTypeUSB:
                return 3
            case DriveTypeNetwork:
                return 5
            default:
            }
        }
    } else {
        return 5
    }
    wc := runtime.NumCPU()
    if wc < 4 {
        wc = 4
    } else if wc > 8 {
        wc = 8
    }
    return wc
}

func loadCheckConfig(root string) *checkConfig {
    cfg := &checkConfig{
        threshold:     time.Duration(getEnvFloat("FILEDO_CHECK_THRESHOLD_SECONDS", 2.0) * float64(time.Second)),
        warmupGrace:   time.Duration(getEnvFloat("FILEDO_CHECK_WARMUP_SECONDS", 10.0) * float64(time.Second)),
        warmupIdle:    time.Duration(getEnvFloat("FILEDO_CHECK_WARMUP_IDLE_RESET_SECONDS", 30.0) * float64(time.Second)),
        workers:       getEnvInt("FILEDO_CHECK_WORKERS", 0),
        bufSize:       getEnvInt("FILEDO_CHECK_BUF_KB", 64) * 1024,
        mode:          detectMode(),
        balancedMinMB: int64(getEnvFloat("FILEDO_CHECK_BALANCED_MIN_MB", 128.0)),
        minSizeBytes:  toBytesMBEnv(getEnvFloat("FILEDO_CHECK_MIN_MB", 0)),
        maxSizeBytes:  toBytesMBEnv(getEnvFloat("FILEDO_CHECK_MAX_MB", 0)),
        includeExt:    parseExtSet(os.Getenv("FILEDO_CHECK_INCLUDE_EXT")),
        excludeExt:    parseExtSet(os.Getenv("FILEDO_CHECK_EXCLUDE_EXT")),
        maxFiles:      int64(getEnvInt("FILEDO_CHECK_MAX_FILES", 0)),
        maxDuration:   time.Duration(getEnvFloat("FILEDO_CHECK_MAX_DURATION_SEC", 0) * float64(time.Second)),
    precount:      getEnvInt("FILEDO_CHECK_PRECOUNT", 1) == 1,
        dryRun:        getEnvInt("FILEDO_CHECK_DRYRUN", 0) == 1,
        verbose:       getEnvInt("FILEDO_CHECK_VERBOSE", 0) == 1,
        quiet:         getEnvInt("FILEDO_CHECK_QUIET", 0) == 1,
        resume:        getEnvInt("FILEDO_CHECK_RESUME", 0) == 1,
        report:        strings.ToLower(os.Getenv("FILEDO_CHECK_REPORT")),
        reportFile:    os.Getenv("FILEDO_CHECK_REPORT_FILE"),
        hddSleepMs:    getEnvInt("FILEDO_CHECK_HDD_SLEEP_MS", 0),
    }
    if cfg.report != "csv" && cfg.report != "json" {
        cfg.report = ""
    }
    if cfg.report != "" && cfg.reportFile == "" {
        cfg.reportFile = fmt.Sprintf("check_report_%s.%s", time.Now().Format("20060102_150405"), cfg.report)
    }
    if cfg.workers <= 0 {
        cfg.workers = decideWorkers(root, cfg)
    }
    return cfg
}

// CheckFolder scans all files under root and performs a fast read test.
// If a file's first read takes > 2s (except a one-time warm-up up to 10s),
// it is marked as damaged and appended to skip_files.list immediately.
func CheckFolder(root string) error {
    info, err := os.Stat(root)
    if err != nil {
        return fmt.Errorf("path error: %v", err)
    }
    if !info.IsDir() {
        return fmt.Errorf("%s is not a directory", root)
    }

    cfg := loadCheckConfig(root)

    ih := globalInterruptHandler
    if ih == nil {
        ih = NewInterruptHandler()
    }

    damaged, err := NewDamagedDiskHandlerQuiet()
    if err != nil {
        return fmt.Errorf("failed to init damaged handler: %v", err)
    }
    defer damaged.Close()

    // Load good files list (check_files.list) with optional override via env
    wd, _ := os.Getwd()
    goodFile := os.Getenv("FILEDO_CHECK_GOODLIST")
    if strings.TrimSpace(goodFile) == "" {
        goodFile = filepath.Join(wd, "check_files.list")
    }
    goodSet := make(map[string]bool)
    var goodMu sync.Mutex
    normGood := func(p string) string {
        if p == "" { return p }
        if ap, err := filepath.Abs(p); err == nil { p = ap }
        p = filepath.Clean(p)
        return strings.ToLower(p)
    }
    // Load existing good list if present
    if f, e := os.Open(goodFile); e == nil {
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            s := strings.TrimSpace(scanner.Text())
            if s != "" && !strings.HasPrefix(s, "#") {
                goodSet[normGood(s)] = true
            }
        }
        f.Close()
    }
    goodHas := func(p string) bool {
        key := normGood(p)
        goodMu.Lock()
        _, ok := goodSet[key]
        goodMu.Unlock()
        return ok
    }
    goodAppend := func(p string) {
        if p == "" { return }
        key := normGood(p)
        goodMu.Lock()
        if goodSet[key] {
            goodMu.Unlock()
            return
        }
        if f, e := os.OpenFile(goodFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); e == nil {
            fmt.Fprintln(f, p)
            f.Close()
            goodSet[key] = true
        }
        goodMu.Unlock()
    }
    if !cfg.quiet {
        if fi, err := os.Stat(goodFile); err == nil && !fi.IsDir() {
            fmt.Printf("Using good list: %s : %d\n", goodFile, len(goodSet))
        }
        if fi, err := os.Stat(damaged.config.SkipListFile); err == nil && !fi.IsDir() {
            fmt.Printf("Using damaged list: %s : %d\n", damaged.config.SkipListFile, damaged.GetSkippedStats())
        }
    }

    // Optional report
    var rep *reportWriter
    if cfg.report != "" {
        if r, e := newReportWriter(cfg.report, cfg.reportFile); e == nil {
            rep = r
            defer rep.Close()
        } else {
            fmt.Printf("Report init failed: %v\n", e)
        }
    }

    jobs := make(chan checkJob, 1024)
    var totalFiles int64
    var skippedFiles int64
    var damagedFiles int64
    var processedFiles int64
    var totalReadBytes int64
    var lastDamaged atomic.Value // string
    var warmupUsed int32 = 0
    var stopFlag int32 = 0

    // Resume support
    var resumeUntil string
    if cfg.resume {
        if st := loadCheckState(); st != nil {
            resumeUntil = st.LastProcessedPath
            if resumeUntil != "" && !cfg.quiet {
                fmt.Printf("Resuming after: %s\n", resumeUntil)
            }
        }
    }

    walkerErrCh := make(chan error, 1)
    go func() {
        walkerErrCh <- filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
            if err != nil { return nil }
            if fi.IsDir() { return nil }
            if ih.IsForceExit() || ih.IsInterrupted() { return fmt.Errorf("interrupted") }
            if atomic.LoadInt32(&stopFlag) != 0 { return fmt.Errorf("stopped") }

            // Filters
            sz := fi.Size()
            if sz == 0 { return nil }
            if cfg.minSizeBytes > 0 && sz < cfg.minSizeBytes { return nil }
            if cfg.maxSizeBytes > 0 && sz > cfg.maxSizeBytes { return nil }
            if cfg.includeExt != nil || cfg.excludeExt != nil {
                ext := strings.ToLower(filepath.Ext(fi.Name()))
                if cfg.includeExt != nil && !cfg.includeExt[ext] { return nil }
                if cfg.excludeExt != nil && cfg.excludeExt[ext] { return nil }
            }

            // Skip if previously checked good
            if goodHas(p) {
                atomic.AddInt64(&skippedFiles, 1)
                return nil
            }

            atomic.AddInt64(&totalFiles, 1)
            if damaged.ShouldSkipFile(p) {
                atomic.AddInt64(&skippedFiles, 1)
                return nil
            }
            if cfg.resume && resumeUntil != "" {
                if strings.EqualFold(p, resumeUntil) {
                    resumeUntil = "" // reached marker, start processing next files
                }
                return nil
            }
            jobs <- checkJob{path: p, size: sz, vol: volumeOf(p)}
            return nil
        })
        close(jobs)
    }()

    // Optional pre-count for better ETA
    if cfg.precount {
        var precTotal int64
        filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
            if err != nil || fi == nil || fi.IsDir() { return nil }
            sz := fi.Size()
            if sz == 0 { return nil }
            if cfg.minSizeBytes > 0 && sz < cfg.minSizeBytes { return nil }
            if cfg.maxSizeBytes > 0 && sz > cfg.maxSizeBytes { return nil }
            if cfg.includeExt != nil || cfg.excludeExt != nil {
                ext := strings.ToLower(filepath.Ext(fi.Name()))
                if cfg.includeExt != nil && !cfg.includeExt[ext] { return nil }
                if cfg.excludeExt != nil && cfg.excludeExt[ext] { return nil }
            }
            // Skip previously good and damaged
            if goodHas(p) { return nil }
            if damaged.ShouldSkipFile(p) { return nil }
            atomic.AddInt64(&precTotal, 1)
            return nil
        })
        atomic.StoreInt64(&totalFiles, precTotal)
    }

    // Progress ticker
    start := time.Now()
    ticker := time.NewTicker(1 * time.Second)
    quit := make(chan struct{})
    go func() {
        lastLen := 0
        for {
            select {
            case <-ticker.C:
                elapsed := time.Since(start).Seconds()
                if elapsed <= 0 { elapsed = 1 }
                readMB := float64(atomic.LoadInt64(&totalReadBytes)) / (1024.0 * 1024.0)
                speed := readMB / elapsed
                checked := atomic.LoadInt64(&processedFiles)
                found := atomic.LoadInt64(&totalFiles)
                rate := float64(checked) / elapsed
                remaining := math.Max(0, float64(found-checked))
                eta := time.Duration(0)
                if rate > 0 {
                    eta = time.Duration(float64(time.Second) * remaining / rate)
                }
                line := fmt.Sprintf("\rCHECK: found=%d, checked=%d, damaged=%d, skipped=%d, read=%.1f MB, speed=%.1f MB/s, rate=%.1f chk/s, ETA=%s",
                    found, checked, atomic.LoadInt64(&damagedFiles), atomic.LoadInt64(&skippedFiles), readMB, speed, rate, formatETA(eta))
                if v := lastDamaged.Load(); v != nil && cfg.verbose {
                    line += fmt.Sprintf(", last=%s", v.(string))
                }
                if !cfg.quiet {
                    // Clear tail from previous longer line
                    pad := 0
                    if ll := len(line); ll < lastLen { pad = lastLen - ll }
                    if pad > 0 { line += strings.Repeat(" ", pad) }
                    fmt.Print(line)
                    lastLen = len(line)
                }
            case <-quit:
                return
            }
        }
    }()

    // Workers
    workerCount := decideWorkers(root, cfg)
    var wg sync.WaitGroup
    wg.Add(workerCount)

    for i := 0; i < workerCount; i++ {
        go func() {
            defer wg.Done()
            buf := make([]byte, cfg.bufSize)
            // Per-volume warmup tracking
            var vwMu sync.Mutex
            vw := make(map[string]*volumeWarmup)
            for job := range jobs {
                if ih.IsForceExit() || ih.IsInterrupted() { return }
                if atomic.LoadInt32(&stopFlag) != 0 { return }
                p := job.path
                size := job.size
                if size == 0 { continue }
                f, err := os.Open(p)
                if err != nil {
                    damaged.LogDamagedFile(p, "check-open-error", size, 1, fmt.Sprintf("open error: %v", err))
                    lastDamaged.Store(p)
                    atomic.AddInt64(&damagedFiles, 1)
                    atomic.AddInt64(&processedFiles, 1)
                    if rep != nil { rep.Write(p, size, 0, "open-error") }
                    continue
                }

                // Ensure read unblocks on interrupt
                done := make(chan struct{})
                go func(ff *os.File) {
                    select {
                    case <-ih.Context().Done():
                        ff.Close()
                    case <-done:
                    }
                }(f)

                // Ensure we close at the end
                var firstElapsed time.Duration
                var status string
                var damagedMark bool

                // Per-volume warmup with idle reset
                if job.vol != "" && cfg.warmupGrace > 0 {
                    vwMu.Lock()
                    v := vw[job.vol]
                    now := time.Now()
                    if v == nil { v = &volumeWarmup{}; vw[job.vol] = v }
                    if !v.used {
                        v.used = true
                        v.last = now
                        f.Read(make([]byte, 4))
                    } else if cfg.warmupIdle > 0 && now.Sub(v.last) >= cfg.warmupIdle {
                        v.last = now
                        f.Read(make([]byte, 4))
                    } else {
                        v.last = now
                    }
                    vwMu.Unlock()
                }

                // Helper: probe at offset with retry logic (reopen on retry)
                probe := func(label string, off int64) (time.Duration, bool) {
                    if off > 0 {
                        if _, err := f.Seek(off, 0); err != nil {
                            return 0, true
                        }
                    }
                    t0 := time.Now()
                    n, rerr := f.Read(buf)
                    elapsed := time.Since(t0)
                    if n > 0 { atomic.AddInt64(&totalReadBytes, int64(n)) }
                    if rerr != nil && rerr.Error() != "EOF" {
                        return elapsed, true
                    }
                    if elapsed > cfg.threshold {
                        if elapsed <= cfg.threshold+checkRetryWindow {
                            time.Sleep(checkRetrySleep)
                            f2, e2 := os.Open(p)
                            if e2 == nil {
                                if off > 0 { f2.Seek(off, 0) }
                                t1 := time.Now()
                                n2, r2 := f2.Read(buf)
                                d2 := time.Since(t1)
                                f2.Close()
                                if n2 > 0 { atomic.AddInt64(&totalReadBytes, int64(n2)) }
                                if r2 == nil || (r2 != nil && r2.Error() == "EOF") {
                                    if d2 <= cfg.threshold {
                                        return d2, false
                                    }
                                }
                                return d2, true
                            }
                        }
                        return elapsed, true
                    }
                    return elapsed, false
                }

                // First probe at start
                e1, bad1 := probe("start", 0)
                firstElapsed = e1
                if bad1 {
                    if atomic.LoadInt32(&warmupUsed) == 0 && e1 <= cfg.warmupGrace {
                        atomic.StoreInt32(&warmupUsed, 1)
                        // treat as ok due to warmup, continue to extra probes
                    } else {
                        damaged.LogDamagedFile(p, "check-delay", size, 1, fmt.Sprintf(">%.0fs read delay (%.0fs)", cfg.threshold.Seconds(), e1.Seconds()))
                        lastDamaged.Store(p)
                        damagedMark = true
                        status = "delay-first"
                    }
                }

                // Additional probes for balanced/deep
                if !damagedMark && cfg.mode != modeQuick {
                    // minimal size check for balanced/deep
                    minBytes := cfg.minSizeBytes
                    if minBytes == 0 { minBytes = toBytesMBEnv(float64(cfg.balancedMinMB)) }
                    if size >= minBytes {
                        var points []float64
                        if cfg.mode == modeBalanced {
                            points = []float64{0.5}
                        } else {
                            points = []float64{0.25, 0.5, 0.75}
                        }
                        for _, frac := range points {
                            off := int64(float64(size-int64(len(buf))) * frac)
                            if off < 0 { off = 0 }
                            if off > size-int64(len(buf)) { off = size - int64(len(buf)) }
                            e, bad := probe("p", off)
                            if bad {
                                damaged.LogDamagedFile(p, "check-delay", size, 1, fmt.Sprintf(">%.0fs read delay mid (%.0fs)", cfg.threshold.Seconds(), e.Seconds()))
                                lastDamaged.Store(p)
                                damagedMark = true
                                status = "delay-probe"
                                break
                            }
                        }
                    }
                }

                // Finalize
                close(done)
                f.Close()
                if damagedMark {
                    atomic.AddInt64(&damagedFiles, 1)
                    atomic.AddInt64(&processedFiles, 1)
                    if rep != nil { rep.Write(p, size, firstElapsed, status) }
                } else {
                    atomic.AddInt64(&processedFiles, 1)
                    if rep != nil { rep.Write(p, size, firstElapsed, "ok") }
                    // Mark as good immediately
                    goodAppend(p)
                }

                // Limits
                if cfg.maxFiles > 0 && atomic.LoadInt64(&processedFiles) >= cfg.maxFiles {
                    atomic.StoreInt32(&stopFlag, 1)
                    return
                }
                if cfg.maxDuration > 0 && time.Since(start) >= cfg.maxDuration {
                    atomic.StoreInt32(&stopFlag, 1)
                    return
                }

                // Optional gentle limiter for HDD
                if cfg.hddSleepMs > 0 { time.Sleep(time.Duration(cfg.hddSleepMs) * time.Millisecond) }
            }
        }()
    }

    if err := <-walkerErrCh; err != nil && err.Error() != "interrupted" && err.Error() != "stopped" {
        return fmt.Errorf("walk error: %v", err)
    }
    wg.Wait()

    close(quit)
    ticker.Stop()
    if !cfg.quiet { fmt.Print("\n") }

    if !cfg.quiet {
        fmt.Printf("\nCHECK completed: total=%d, skipped(damaged-before)=%d, newly-damaged=%d\n",
            atomic.LoadInt64(&totalFiles), atomic.LoadInt64(&skippedFiles), atomic.LoadInt64(&damagedFiles))
    }
    return nil
}

// Reporting
type reportWriter struct {
    kind string
    f    *os.File
    n    int
}

func newReportWriter(kind, path string) (*reportWriter, error) {
    f, err := os.Create(path)
    if err != nil { return nil, err }
    w := &reportWriter{kind: kind, f: f}
    if kind == "csv" {
        fmt.Fprintln(f, "path,size,first_read_ms,status")
    } else if kind == "json" {
        fmt.Fprintln(f, "[")
    }
    return w, nil
}

func (w *reportWriter) Write(path string, size int64, elapsed time.Duration, status string) {
    if w == nil || w.f == nil { return }
    ms := float64(elapsed.Milliseconds())
    if w.kind == "csv" {
        fmt.Fprintf(w.f, "%q,%d,%.1f,%q\n", path, size, ms, status)
    } else if w.kind == "json" {
        if w.n > 0 { fmt.Fprintln(w.f, ",") }
        fmt.Fprintf(w.f, "  {\n    \"path\": %q,\n    \"size\": %d,\n    \"first_read_ms\": %.1f,\n    \"status\": %q\n  }", path, size, ms, status)
        w.n++
    }
}

func (w *reportWriter) Close() {
    if w == nil || w.f == nil { return }
    if w.kind == "json" {
        fmt.Fprintln(w.f, "\n]")
    }
    w.f.Close()
}

// Resume state
type checkState struct {
    LastProcessedPath string    `json:"lastProcessedPath"`
    Timestamp         time.Time `json:"timestamp"`
}

func loadCheckState() *checkState {
    b, err := os.ReadFile("check_state.json")
    if err != nil { return nil }
    var s checkState
    if jsonErr := json.Unmarshal(b, &s); jsonErr != nil { return nil }
    return &s
}

func saveCheckState(path string) {
    if path == "" { return }
    s := checkState{LastProcessedPath: path, Timestamp: time.Now()}
    if b, err := json.MarshalIndent(s, "", "  "); err == nil {
        _ = os.WriteFile("check_state.json", b, 0644)
    }
}
