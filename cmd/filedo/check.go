package main

import (
    "context"
    "errors"
    "flag"
    "fmt"
    "io"
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
    resume        bool // skip files already on the good list
    report        string // "", "csv", "json"
    reportFile    string
    hddSleepMs    int
    // single-reader and adaptive throttle
    singleReaderOverride int    // -1 auto, 0 force off, 1 force on
    ewmaAlpha            float64
    ewmaHighFrac         float64
    ewmaLowFrac          float64
    maxSleepMs           int
    sleepStepMs          int
}

type checkJob struct {
    path string
    size int64
    vol  string
    mod  time.Time
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
    // single-reader override: -1 auto (default), 0 force off, 1 force on
    if v := os.Getenv("FILEDO_CHECK_SINGLE_READER"); strings.TrimSpace(v) != "" {
        if n, err := strconv.Atoi(v); err == nil { cfg.singleReaderOverride = n } else { cfg.singleReaderOverride = -1 }
    } else {
        cfg.singleReaderOverride = -1
    }
    // EWMA adaptive throttling params
    cfg.ewmaAlpha = getEnvFloat("FILEDO_CHECK_EWMA_ALPHA", 0.1)
    cfg.ewmaHighFrac = getEnvFloat("FILEDO_CHECK_EWMA_HIGH_FRAC", 0.8)
    cfg.ewmaLowFrac = getEnvFloat("FILEDO_CHECK_EWMA_LOW_FRAC", 0.3)
    cfg.maxSleepMs = getEnvInt("FILEDO_CHECK_MAX_SLEEP_MS", 200)
    cfg.sleepStepMs = getEnvInt("FILEDO_CHECK_SLEEP_STEP_MS", 5)
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

// HandleCheckArgs parses CLI flags for CHECK, sets corresponding FILEDO_CHECK_* env vars (flags take precedence), then runs CheckFolder.
func HandleCheckArgs(root string, args []string) error {
    fs := flag.NewFlagSet("check", flag.ContinueOnError)
    fs.SetOutput(os.Stdout)

    thr := fs.Float64("threshold", math.NaN(), "Max allowed first-read delay in seconds (FILEDO_CHECK_THRESHOLD_SECONDS)")
    warm := fs.Float64("warmup", math.NaN(), "Warm-up grace in seconds (FILEDO_CHECK_WARMUP_SECONDS)")
    warmIdle := fs.Float64("warmup-idle", math.NaN(), "Idle reset for warmup in seconds (FILEDO_CHECK_WARMUP_IDLE_RESET_SECONDS)")
    workers := fs.Int("workers", -1, "Worker count (auto if not set) (FILEDO_CHECK_WORKERS)")
    bufKB := fs.Int("buf-kb", -1, "Read buffer size in KB (FILEDO_CHECK_BUF_KB)")
    mode := fs.String("mode", "", "Mode: quick|balanced|deep (FILEDO_CHECK_MODE)")
    balancedMinMB := fs.Int("balanced-min-mb", -1, "Min size in MB for mid-file probe (FILEDO_CHECK_BALANCED_MIN_MB)")
    minMB := fs.Float64("min-mb", math.NaN(), "Min file size in MB to include (FILEDO_CHECK_MIN_MB)")
    maxMB := fs.Float64("max-mb", math.NaN(), "Max file size in MB to include (FILEDO_CHECK_MAX_MB)")
    includeExt := fs.String("include-ext", "", "Include extensions, comma-separated (FILEDO_CHECK_INCLUDE_EXT)")
    excludeExt := fs.String("exclude-ext", "", "Exclude extensions, comma-separated (FILEDO_CHECK_EXCLUDE_EXT)")
    maxFiles := fs.Int64("max-files", -1, "Limit number of files to process (FILEDO_CHECK_MAX_FILES)")
    maxSeconds := fs.Float64("max-seconds", math.NaN(), "Limit total duration in seconds (FILEDO_CHECK_MAX_DURATION_SEC)")
    precount := fs.Bool("precount", false, "Enable pre-count to improve ETA (FILEDO_CHECK_PRECOUNT=1)")
    _ = fs.Bool("no-precount", false, "Disable pre-count (overrides --precount)")
    dryRun := fs.Bool("dry-run", false, "Do not modify state, just simulate (FILEDO_CHECK_DRYRUN=1)")
    verbose := fs.Bool("verbose", false, "Verbose output (FILEDO_CHECK_VERBOSE=1)")
    quiet := fs.Bool("quiet", false, "Quiet output (FILEDO_CHECK_QUIET=1)")
    resume := fs.Bool("resume", false, "Carry on: skip files an earlier run already read cleanly - the good list (FILEDO_CHECK_RESUME=1)")
    report := fs.String("report", "", "Report format: csv|json (FILEDO_CHECK_REPORT)")
    reportFile := fs.String("report-file", "", "Report file path (FILEDO_CHECK_REPORT_FILE)")
    hddSleepMs := fs.Int("hdd-sleep-ms", -1, "Fixed inter-file sleep for HDD in ms (FILEDO_CHECK_HDD_SLEEP_MS)")
    singleReader := fs.String("single-reader", "", "auto|on|off (FILEDO_CHECK_SINGLE_READER)")
    ewmaAlpha := fs.Float64("ewma-alpha", math.NaN(), "EWMA alpha [0..1] (FILEDO_CHECK_EWMA_ALPHA)")
    ewmaHigh := fs.Float64("ewma-high-frac", math.NaN(), "High fraction of threshold (FILEDO_CHECK_EWMA_HIGH_FRAC)")
    ewmaLow := fs.Float64("ewma-low-frac", math.NaN(), "Low fraction of threshold (FILEDO_CHECK_EWMA_LOW_FRAC)")
    maxSleep := fs.Int("max-sleep-ms", -1, "Max adaptive sleep in ms (FILEDO_CHECK_MAX_SLEEP_MS)")
    sleepStep := fs.Int("sleep-step-ms", -1, "Adaptive sleep step in ms (FILEDO_CHECK_SLEEP_STEP_MS)")
    goodList := fs.String("good-list", "", "Path to good files list (FILEDO_CHECK_GOODLIST)")

    if err := fs.Parse(args); err != nil {
        return err
    }

    // Only set env for flags that were explicitly provided
    visited := map[string]bool{}
    fs.Visit(func(f *flag.Flag) { visited[f.Name] = true })

    for name := range visited {
        switch name {
        case "threshold":
            os.Setenv("FILEDO_CHECK_THRESHOLD_SECONDS", fmt.Sprintf("%g", *thr))
        case "warmup":
            os.Setenv("FILEDO_CHECK_WARMUP_SECONDS", fmt.Sprintf("%g", *warm))
        case "warmup-idle":
            os.Setenv("FILEDO_CHECK_WARMUP_IDLE_RESET_SECONDS", fmt.Sprintf("%g", *warmIdle))
        case "workers":
            os.Setenv("FILEDO_CHECK_WORKERS", fmt.Sprintf("%d", *workers))
        case "buf-kb":
            if *bufKB < checkMinBufKB || *bufKB > checkMaxBufKB {
                return fmt.Errorf("check: --buf-kb must be %d..%d, got %d", checkMinBufKB, checkMaxBufKB, *bufKB)
            }
            os.Setenv("FILEDO_CHECK_BUF_KB", fmt.Sprintf("%d", *bufKB))
        case "mode":
            os.Setenv("FILEDO_CHECK_MODE", *mode)
        case "balanced-min-mb":
            os.Setenv("FILEDO_CHECK_BALANCED_MIN_MB", fmt.Sprintf("%d", *balancedMinMB))
        case "min-mb":
            os.Setenv("FILEDO_CHECK_MIN_MB", fmt.Sprintf("%g", *minMB))
        case "max-mb":
            os.Setenv("FILEDO_CHECK_MAX_MB", fmt.Sprintf("%g", *maxMB))
        case "include-ext":
            os.Setenv("FILEDO_CHECK_INCLUDE_EXT", *includeExt)
        case "exclude-ext":
            os.Setenv("FILEDO_CHECK_EXCLUDE_EXT", *excludeExt)
        case "max-files":
            os.Setenv("FILEDO_CHECK_MAX_FILES", fmt.Sprintf("%d", *maxFiles))
        case "max-seconds":
            os.Setenv("FILEDO_CHECK_MAX_DURATION_SEC", fmt.Sprintf("%g", *maxSeconds))
        case "dry-run":
            if *dryRun { os.Setenv("FILEDO_CHECK_DRYRUN", "1") } else { os.Setenv("FILEDO_CHECK_DRYRUN", "0") }
        case "verbose":
            if *verbose { os.Setenv("FILEDO_CHECK_VERBOSE", "1") } else { os.Setenv("FILEDO_CHECK_VERBOSE", "0") }
        case "quiet":
            if *quiet { os.Setenv("FILEDO_CHECK_QUIET", "1") } else { os.Setenv("FILEDO_CHECK_QUIET", "0") }
        case "resume":
            if *resume { os.Setenv("FILEDO_CHECK_RESUME", "1") } else { os.Setenv("FILEDO_CHECK_RESUME", "0") }
        case "report":
            os.Setenv("FILEDO_CHECK_REPORT", *report)
        case "report-file":
            os.Setenv("FILEDO_CHECK_REPORT_FILE", *reportFile)
        case "hdd-sleep-ms":
            os.Setenv("FILEDO_CHECK_HDD_SLEEP_MS", fmt.Sprintf("%d", *hddSleepMs))
        case "single-reader":
            v := strings.ToLower(strings.TrimSpace(*singleReader))
            switch v {
            case "1", "on", "true", "yes":
                os.Setenv("FILEDO_CHECK_SINGLE_READER", "1")
            case "0", "off", "false", "no":
                os.Setenv("FILEDO_CHECK_SINGLE_READER", "0")
            case "-1", "auto", "":
                os.Setenv("FILEDO_CHECK_SINGLE_READER", "-1")
            default:
                // try to pass as-is
                os.Setenv("FILEDO_CHECK_SINGLE_READER", v)
            }
        case "ewma-alpha":
            os.Setenv("FILEDO_CHECK_EWMA_ALPHA", fmt.Sprintf("%g", *ewmaAlpha))
        case "ewma-high-frac":
            os.Setenv("FILEDO_CHECK_EWMA_HIGH_FRAC", fmt.Sprintf("%g", *ewmaHigh))
        case "ewma-low-frac":
            os.Setenv("FILEDO_CHECK_EWMA_LOW_FRAC", fmt.Sprintf("%g", *ewmaLow))
        case "max-sleep-ms":
            os.Setenv("FILEDO_CHECK_MAX_SLEEP_MS", fmt.Sprintf("%d", *maxSleep))
        case "sleep-step-ms":
            os.Setenv("FILEDO_CHECK_SLEEP_STEP_MS", fmt.Sprintf("%d", *sleepStep))
        case "good-list":
            os.Setenv("FILEDO_CHECK_GOODLIST", *goodList)
        }
    }

    // Resolve precedence for pre-count pair of flags
    if visited["no-precount"] {
        os.Setenv("FILEDO_CHECK_PRECOUNT", "0")
    } else if visited["precount"] {
        if *precount { os.Setenv("FILEDO_CHECK_PRECOUNT", "1") } else { os.Setenv("FILEDO_CHECK_PRECOUNT", "0") }
    }

    return CheckFolder(root)
}

// Read buffer bounds for --buf-kb (CHK-09). Zero bytes read nothing and so
// passed every file; a negative size panicked.
const (
    checkMinBufKB = 4
    checkMaxBufKB = 64 * 1024
)

// errCheckLimit ends the walk when --max-files or --max-seconds was reached;
// errCheckStopped when the run was asked to stop.
var (
    errCheckLimit   = errors.New("check limit reached")
    errCheckStopped = errors.New("check stopped")
)

// checkOutcome is what reading one file said about it.
type checkOutcome int

const (
    checkOK checkOutcome = iota
    checkDamaged
    // checkUnverified: the file could not be read for a reason that is not
    // the media's - locked, access denied - so nothing was learned (CHK-04).
    checkUnverified
    checkStopped
)

type checkResult struct {
    outcome checkOutcome
    first   time.Duration
    status  string
    detail  string
}

// checkFile reads one file the way the mode asks and judges it. A slow read
// or a device-level I/O error is damage; a sharing violation or an access
// denial is "could not verify" and is never recorded as damage (SP-0023 T3).
func checkFile(ctx context.Context, job checkJob, cfg *checkConfig, buf []byte, warmup func(*os.File), warmupUsed *int32, readBytes *int64) checkResult {
    p, size := job.path, job.size
    judgeErr := func(first time.Duration, err error) checkResult {
        if ctx.Err() != nil || isStopError(err) {
            return checkResult{outcome: checkStopped, first: first}
        }
        if isDeviceIOError(err) {
            return checkResult{outcome: checkDamaged, first: first, status: "read-error", detail: fmt.Sprintf("read error: %v", err)}
        }
        return checkResult{outcome: checkUnverified, first: first, status: "not-verified", detail: err.Error()}
    }

    f, err := os.Open(p)
    if err != nil {
        r := judgeErr(0, err)
        if r.outcome == checkDamaged {
            r.status = "open-error"
        }
        return r
    }
    done := make(chan struct{})
    go func() {
        select {
        case <-ctx.Done():
            f.Close()
        case <-done:
        }
    }()
    defer func() {
        close(done)
        f.Close()
    }()
    if warmup != nil {
        warmup(f)
    }

    probe := func(off int64) (time.Duration, bool, error) {
        if off > 0 {
            if _, err := f.Seek(off, io.SeekStart); err != nil { return 0, false, err }
        }
        t0 := time.Now()
        n, rerr := f.Read(buf)
        d := time.Since(t0)
        if n > 0 { atomic.AddInt64(readBytes, int64(n)) }
        if rerr != nil && rerr != io.EOF { return d, false, rerr }
        if d > cfg.threshold {
            if d <= cfg.threshold+checkRetryWindow {
                time.Sleep(checkRetrySleep)
                if f2, e2 := os.Open(p); e2 == nil {
                    if off > 0 { f2.Seek(off, io.SeekStart) }
                    t1 := time.Now()
                    n2, r2 := f2.Read(buf)
                    d2 := time.Since(t1)
                    f2.Close()
                    if n2 > 0 { atomic.AddInt64(readBytes, int64(n2)) }
                    if r2 == nil || r2 == io.EOF { return d2, d2 > cfg.threshold, nil }
                    return d2, false, r2
                }
            }
            return d, true, nil
        }
        return d, false, nil
    }

    e1, slow, rerr := probe(0)
    if rerr != nil {
        return judgeErr(e1, rerr)
    }
    if slow {
        // One slow first read per run is the disk spinning up, not damage.
        if !(e1 <= cfg.warmupGrace && atomic.CompareAndSwapInt32(warmupUsed, 0, 1)) {
            return checkResult{outcome: checkDamaged, first: e1, status: "delay-first",
                detail: fmt.Sprintf(">%.1fs read delay (%.1fs)", cfg.threshold.Seconds(), e1.Seconds())}
        }
    }
    if cfg.mode != modeQuick {
        minBytes := cfg.minSizeBytes
        if minBytes == 0 { minBytes = toBytesMBEnv(float64(cfg.balancedMinMB)) }
        if size >= minBytes {
            var points []float64
            if cfg.mode == modeBalanced { points = []float64{0.5} } else { points = []float64{0.25, 0.5, 0.75} }
            for _, frac := range points {
                off := int64(float64(size-int64(len(buf))) * frac)
                if off < 0 { off = 0 }
                if off > size-int64(len(buf)) { off = size - int64(len(buf)) }
                e, slowMid, err := probe(off)
                if err != nil {
                    return judgeErr(e1, err)
                }
                if slowMid {
                    return checkResult{outcome: checkDamaged, first: e1, status: "delay-probe",
                        detail: fmt.Sprintf(">%.1fs read delay mid (%.1fs)", cfg.threshold.Seconds(), e.Seconds())}
                }
            }
        }
    }
    if ctx.Err() != nil {
        return checkResult{outcome: checkStopped, first: e1}
    }
    return checkResult{outcome: checkOK, first: e1}
}

// checkNotes prints the first few files check could not judge and counts
// the rest, so a sweep over a locked profile does not flood the console.
type checkNotes struct {
    mu     sync.Mutex
    shown  int
    hidden int
}

func (n *checkNotes) add(quiet bool, format string, args ...interface{}) {
    n.mu.Lock()
    defer n.mu.Unlock()
    if quiet || n.shown >= 20 {
        n.hidden++
        return
    }
    n.shown++
    fmt.Printf("\n"+format+"\n", args...)
}

// CheckFolder scans all files under root and performs a fast read test.
// If a file's first read takes > 2s (except a one-time warm-up up to 10s),
// it is marked as damaged and recorded in check's damaged list.
//
// root may also be a single file. filepath.Walk already visits exactly that
// one file, so the engine below needs no second shape - but two of its rules
// are about narrowing a sweep and have no business narrowing an explicit
// choice, so a single file is never filtered out by size or extension, never
// skipped for being on the good list and never answered from the damaged
// list. A user who points at one file is asking about that file, today.
//
// The lists live in the state root (%LOCALAPPDATA%\FileDO\state), never
// beside the files checked (CHK-10), and they are check's own: copy's skip
// list is a different file (CHK-04). An entry names a file by path, size and
// modification time, so a file that changed is read again.
//
// The verdict (CHK-03): a file on the damaged list is a defect again - it is
// re-reported, not re-read; a folder that cannot be listed or a file that
// cannot be opened (locked, denied) is "could not verify"; and a sweep that
// read nothing at all verified nothing.
func CheckFolder(root string) error {
    info, err := os.Stat(root)
    if err != nil {
        return fmt.Errorf("path error: %v", err)
    }
    singleFile := !info.IsDir()
    if singleFile && !info.Mode().IsRegular() {
        return fmt.Errorf("%s is neither a folder nor a regular file", root)
    }

    cfg := loadCheckConfig(root)
    if cfg.bufSize < checkMinBufKB*1024 || cfg.bufSize > checkMaxBufKB*1024 {
        return fmt.Errorf("check: the read buffer must be %d..%d KB, got %d KB (--buf-kb / FILEDO_CHECK_BUF_KB)",
            checkMinBufKB, checkMaxBufKB, cfg.bufSize/1024)
    }

    ih := globalInterruptHandler
    if ih == nil {
        ih = NewInterruptHandler()
    }
    ctx := ih.Context()

    // Check's own lists, in the state root.
    damagedList, derr := openStateList(checkDamagedName, false)
    if derr != nil {
        fmt.Printf("Warning: cannot read the damaged list: %v\n", derr)
    }
    defer damagedList.Close()
    var goodList *fileStateList
    var gerr error
    if goodFile := strings.TrimSpace(os.Getenv("FILEDO_CHECK_GOODLIST")); goodFile != "" {
        goodList, gerr = loadStateList(goodFile)
    } else {
        goodList, gerr = openStateList(checkGoodListName, true)
    }
    if gerr != nil {
        fmt.Printf("Warning: cannot read the good list: %v\n", gerr)
    }
    defer goodList.Close()
    // A dry run changes no state; a single-file check never reads the good
    // list, so it has no business writing one.
    if cfg.dryRun {
        damagedList.readOnly = true
        goodList.readOnly = true
    }
    if singleFile {
        goodList.readOnly = true
    }
    if !cfg.quiet {
        if !singleFile && cfg.resume {
            fmt.Printf("Resuming: files already on the good list are not read again (%s : %d)\n", goodList.Path(), goodList.Len())
        }
        if !singleFile && damagedList.Len() > 0 {
            fmt.Printf("Using damaged list: %s : %d\n", damagedList.Path(), damagedList.Len())
        }
        if n := goodList.LegacyIgnored() + damagedList.LegacyIgnored(); n > 0 {
            fmt.Printf("Note: %d older list entries carry no size or time and are not trusted.\n", n)
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
    var foundFiles int64   // files the walk handed on or answered itself
    var skippedGood int64  // on the good list (--resume): not read again
    var knownDamaged int64 // on the damaged list: re-reported, not read
    var damagedFiles int64 // found damaged in this run
    var checkedFiles int64 // read and judged in this run
    var unverifiedFiles int64
    var walkErrors int64
    var totalReadBytes int64
    var lastDamaged atomic.Value // string
    var warmupUsed int32 = 0
    var notes checkNotes

    // The first worker to reach a limit closes stop; the walker's send
    // selects on it and on the run's context, so a walker can never block on
    // a full queue that no worker will drain again (CHK-02).
    stop := make(chan struct{})
    var stopOnce sync.Once
    stopWorkers := func() { stopOnce.Do(func() { close(stop) }) }
    start := time.Now()

    walkerErrCh := make(chan error, 1)
    go func() {
        walkErr := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
            if ih.IsForceExit() || ih.IsInterrupted() { return errCheckStopped }
            select {
            case <-stop:
                return errCheckLimit
            default:
            }
            if err != nil {
                atomic.AddInt64(&walkErrors, 1)
                notes.add(cfg.quiet, "Could not read %s: %v - not checked", p, err)
                return nil
            }
            if fi.IsDir() { return nil }
            sz := fi.Size()
            if sz == 0 { return nil }
            if !singleFile {
                if cfg.minSizeBytes > 0 && sz < cfg.minSizeBytes { return nil }
                if cfg.maxSizeBytes > 0 && sz > cfg.maxSizeBytes { return nil }
                if cfg.includeExt != nil || cfg.excludeExt != nil {
                    ext := strings.ToLower(filepath.Ext(fi.Name()))
                    if cfg.includeExt != nil && !cfg.includeExt[ext] { return nil }
                    if cfg.excludeExt != nil && cfg.excludeExt[ext] { return nil }
                }
            }
            atomic.AddInt64(&foundFiles, 1)
            if !singleFile {
                if cfg.resume && goodList.HasInfo(p, fi) {
                    atomic.AddInt64(&skippedGood, 1)
                    return nil
                }
                if damagedList.HasInfo(p, fi) {
                    atomic.AddInt64(&knownDamaged, 1)
                    lastDamaged.Store(p)
                    if rep != nil { rep.Write(p, sz, 0, "known-damaged") }
                    return nil
                }
            }
            select {
            case jobs <- checkJob{path: p, size: sz, vol: volumeOf(p), mod: fi.ModTime()}:
                return nil
            case <-stop:
                return errCheckLimit
            case <-ctx.Done():
                return errCheckStopped
            }
        })
        close(jobs)
        walkerErrCh <- walkErr
    }()

    // Optional pre-count for a better ETA. It is an estimate for a sweep and
    // is kept apart from the walker's own count: stored into the same
    // counter, the walker added to it and a 3-file folder reported 6.
    var precTotal int64
    if cfg.precount && !singleFile {
        filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
            if ih.IsInterrupted() { return errCheckStopped }
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
            if cfg.resume && goodList.HasInfo(p, fi) { return nil }
            if damagedList.HasInfo(p, fi) { return nil }
            atomic.AddInt64(&precTotal, 1)
            return nil
        })
    }

    // Progress ticker
    ticker := time.NewTicker(1 * time.Second)
    quit := make(chan struct{})
    tickerDone := make(chan struct{})
    go func() {
        defer close(tickerDone)
        lastLen := 0
        for {
            select {
            case <-ticker.C:
                elapsed := time.Since(start).Seconds()
                if elapsed <= 0 { elapsed = 1 }
                readMB := float64(atomic.LoadInt64(&totalReadBytes)) / (1024.0 * 1024.0)
                speed := readMB / elapsed
                checked := atomic.LoadInt64(&checkedFiles) + atomic.LoadInt64(&unverifiedFiles)
                toRead := atomic.LoadInt64(&precTotal)
                if toRead == 0 {
                    toRead = atomic.LoadInt64(&foundFiles) - atomic.LoadInt64(&skippedGood) - atomic.LoadInt64(&knownDamaged)
                }
                rate := float64(checked) / elapsed
                remaining := math.Max(0, float64(toRead-checked))
                eta := time.Duration(0)
                if rate > 0 {
                    eta = time.Duration(float64(time.Second) * remaining / rate)
                }
                line := fmt.Sprintf("\rCHECK: found=%d, checked=%d, damaged=%d, known-damaged=%d, not-read=%d, skipped-good=%d, read=%.1f MB, speed=%.1f MB/s, rate=%.1f chk/s, ETA=%s",
                    atomic.LoadInt64(&foundFiles), atomic.LoadInt64(&checkedFiles), atomic.LoadInt64(&damagedFiles),
                    atomic.LoadInt64(&knownDamaged), atomic.LoadInt64(&unverifiedFiles), atomic.LoadInt64(&skippedGood),
                    readMB, speed, rate, formatETA(eta))
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

    // handle counts one judged file; it reports whether the worker must stop.
    handle := func(job checkJob, r checkResult) bool {
        switch r.outcome {
        case checkStopped:
            return true
        case checkDamaged:
            atomic.AddInt64(&damagedFiles, 1)
            atomic.AddInt64(&checkedFiles, 1)
            lastDamaged.Store(job.path)
            if err := damagedList.Add(job.path, job.size, job.mod); err != nil {
                notes.add(cfg.quiet, "Warning: cannot record %s in the damaged list: %v", job.path, err)
            }
            if rep != nil { rep.Write(job.path, job.size, r.first, r.status) }
        case checkUnverified:
            atomic.AddInt64(&unverifiedFiles, 1)
            notes.add(cfg.quiet, "Could not read %s (%s) - not judged", job.path, r.detail)
            if rep != nil { rep.Write(job.path, job.size, r.first, r.status) }
        default:
            atomic.AddInt64(&checkedFiles, 1)
            if rep != nil { rep.Write(job.path, job.size, r.first, "ok") }
            goodList.Add(job.path, job.size, job.mod)
        }
        done := atomic.LoadInt64(&checkedFiles) + atomic.LoadInt64(&unverifiedFiles)
        if cfg.maxFiles > 0 && done >= cfg.maxFiles {
            stopWorkers()
            return true
        }
        if cfg.maxDuration > 0 && time.Since(start) >= cfg.maxDuration {
            stopWorkers()
            return true
        }
        return false
    }
    // stopped reports whether a worker must end before its next file.
    stopped := func() bool {
        if ih.IsForceExit() || ih.IsInterrupted() { return true }
        select {
        case <-stop:
            return true
        default:
            return false
        }
    }

    // Workers or single-reader depending on drive type
    rootVol := volumeOf(root)
    singleReader := false
    if cfg.singleReaderOverride == 1 {
        singleReader = true
    } else if cfg.singleReaderOverride == 0 {
        singleReader = false
    } else {
        if rootVol != "" && rootVol != "NET" {
            if info, err := AnalyzeDrive(rootVol); err == nil && info != nil {
                singleReader = (info.DriveType == DriveTypeHDD || info.DriveType == DriveTypeUSB)
            }
        }
    }

    var wg sync.WaitGroup
    if singleReader {
        // Sequential reader with adaptive throttling (EWMA)
        wg.Add(1)
        go func() {
            defer wg.Done()
            buf := make([]byte, cfg.bufSize)
            var vw volumeWarmup
            warmup := func(f *os.File) {
                if cfg.warmupGrace <= 0 { return }
                now := time.Now()
                if !vw.used || (cfg.warmupIdle > 0 && now.Sub(vw.last) >= cfg.warmupIdle) {
                    vw.used = true
                    vw.last = now
                    f.Read(make([]byte, 4))
                    f.Seek(0, io.SeekStart)
                } else {
                    vw.last = now
                }
            }
            var ewma float64
            sleepMs := 0
            for job := range jobs {
                if stopped() { return }
                r := checkFile(ctx, job, cfg, buf, warmup, &warmupUsed, &totalReadBytes)

                // EWMA update for adaptive throttling
                if r.first > 0 {
                    if ewma == 0 {
                        ewma = r.first.Seconds()
                    } else {
                        alpha := cfg.ewmaAlpha
                        if alpha < 0 { alpha = 0 } else if alpha > 1 { alpha = 1 }
                        ewma = alpha*r.first.Seconds() + (1-alpha)*ewma
                    }
                    high := cfg.ewmaHighFrac * cfg.threshold.Seconds()
                    low := cfg.ewmaLowFrac * cfg.threshold.Seconds()
                    step := cfg.sleepStepMs
                    if step <= 0 { step = 1 }
                    maxS := cfg.maxSleepMs
                    if maxS < 0 { maxS = 0 }
                    if ewma > high { sleepMs = int(math.Min(float64(maxS), float64(sleepMs+step))) }
                    if ewma < low { sleepMs = int(math.Max(0, float64(sleepMs-step))) }
                }
                if handle(job, r) { return }
                if sleepMs > 0 { time.Sleep(time.Duration(sleepMs) * time.Millisecond) }
            }
        }()
    } else {
        // Parallel workers
        workerCount := decideWorkers(root, cfg)
        wg.Add(workerCount)
        for i := 0; i < workerCount; i++ {
            go func() {
                defer wg.Done()
                buf := make([]byte, cfg.bufSize)
                vw := make(map[string]*volumeWarmup)
                for job := range jobs {
                    if stopped() { return }
                    var warmup func(*os.File)
                    if job.vol != "" && cfg.warmupGrace > 0 {
                        vol := job.vol
                        warmup = func(f *os.File) {
                            v := vw[vol]
                            now := time.Now()
                            if v == nil { v = &volumeWarmup{}; vw[vol] = v }
                            if !v.used || (cfg.warmupIdle > 0 && now.Sub(v.last) >= cfg.warmupIdle) {
                                v.used = true
                                v.last = now
                                f.Read(make([]byte, 4))
                                f.Seek(0, io.SeekStart)
                            } else {
                                v.last = now
                            }
                        }
                    }
                    r := checkFile(ctx, job, cfg, buf, warmup, &warmupUsed, &totalReadBytes)
                    if handle(job, r) { return }
                    if cfg.hddSleepMs > 0 { time.Sleep(time.Duration(cfg.hddSleepMs) * time.Millisecond) }
                }
            }()
        }
    }

    walkErr := <-walkerErrCh
    wg.Wait()

    close(quit)
    <-tickerDone
    ticker.Stop()
    if !cfg.quiet { fmt.Print("\n") }

    if walkErr != nil && !errors.Is(walkErr, errCheckLimit) && !errors.Is(walkErr, errCheckStopped) {
        atomic.AddInt64(&walkErrors, 1)
        notes.add(cfg.quiet, "The walk ended early: %v", walkErr)
    }

    found := atomic.LoadInt64(&foundFiles)
    checked := atomic.LoadInt64(&checkedFiles)
    newDamaged := atomic.LoadInt64(&damagedFiles)
    oldDamaged := atomic.LoadInt64(&knownDamaged)
    unverified := atomic.LoadInt64(&unverifiedFiles)
    walkErrs := atomic.LoadInt64(&walkErrors)
    good := atomic.LoadInt64(&skippedGood)

    runNumber("totalFiles", found)
    runNumber("checkedFiles", checked)
    runNumber("skippedGoodFiles", good)
    runNumber("damagedFiles", newDamaged+oldDamaged)
    runNumber("newlyDamagedFiles", newDamaged)
    runNumber("knownDamagedFiles", oldDamaged)
    runNumber("unverifiedFiles", unverified)
    runNumber("unreadableEntries", walkErrs)

    // A file that reads slowly or not at all is a judgement about the
    // target, and so is one this list already knows: `Failed`, exit 1.
    if n := newDamaged + oldDamaged; n > 0 {
        runDefect("damaged-files",
            fmt.Sprintf("%d file(s) read slowly or not at all (%d found in this run, %d already on the damaged list)", n, newDamaged, oldDamaged),
            map[string]interface{}{"damagedFiles": n, "newlyDamagedFiles": newDamaged, "knownDamagedFiles": oldDamaged, "totalFiles": found})
    }

    var problems []string
    if unverified > 0 {
        problems = append(problems, fmt.Sprintf("%d file(s) could not be opened or read (locked or access denied) and were not judged", unverified))
    }
    if walkErrs > 0 {
        problems = append(problems, fmt.Sprintf("%d folder(s) or entries could not be listed", walkErrs))
    }
    isStopped := ih.IsInterrupted()
    if !isStopped && checked == 0 && newDamaged+oldDamaged == 0 && len(problems) == 0 {
        if good > 0 {
            problems = append(problems, fmt.Sprintf("nothing was read: all %d file(s) are already on the good list (--resume) - run without --resume to read them again", good))
        } else {
            problems = append(problems, "nothing was read: there is no non-empty file to check here")
        }
    }

    if !cfg.quiet {
        fmt.Printf("\nCHECK completed: found=%d, checked=%d, damaged(new)=%d, damaged(known, not re-read)=%d, not-read=%d, skipped(good, --resume)=%d\n",
            found, checked, newDamaged, oldDamaged, unverified, good)
        if n := notes.hidden; n > 0 {
            fmt.Printf("(%d more unreadable entries not listed)\n", n)
        }
        if newDamaged > 0 && !cfg.dryRun {
            fmt.Printf("Damaged list: %s\n", damagedList.Path())
        }
        // One file gets one sentence. The counters above answer a sweep;
        // they do not answer "is this file all right", which is the only
        // question a single-file check was asked.
        if singleFile {
            switch {
            case info.Size() == 0:
                fmt.Printf("%s is empty - there is nothing to read, so nothing to judge.\n", root)
            case newDamaged > 0:
                fmt.Printf("%s reads SLOWLY or not at all and was logged as damaged.\n", root)
            case unverified > 0:
                fmt.Printf("%s could not be read (locked or access denied) - not judged.\n", root)
            case checked > 0:
                fmt.Printf("%s reads cleanly.\n", root)
            }
        }
    }
    if len(problems) > 0 && !isStopped {
        return fmt.Errorf("check could not verify everything: %s", strings.Join(problems, "; "))
    }
    return nil
}

// Reporting
type reportWriter struct {
    mu   sync.Mutex
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

// Write adds one row. Workers call it concurrently.
func (w *reportWriter) Write(path string, size int64, elapsed time.Duration, status string) {
    if w == nil || w.f == nil { return }
    w.mu.Lock()
    defer w.mu.Unlock()
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
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.kind == "json" {
        fmt.Fprintln(w.f, "\n]")
    }
    w.f.Close()
}
