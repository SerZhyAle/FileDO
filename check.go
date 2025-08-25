package main

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"
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

    // Use global interrupt handler
    ih := globalInterruptHandler
    if ih == nil {
        ih = NewInterruptHandler()
    }

    // Damaged handler for skip list integration (quiet to reduce noise)
    damaged, err := NewDamagedDiskHandlerQuiet()
    if err != nil {
        return fmt.Errorf("failed to init damaged handler: %v", err)
    }
    defer damaged.Close()

    // Collect files
    jobs := make(chan string, 1024)
    var totalFiles int64
    var skippedFiles int64
    var damagedFiles int64
    var processedFiles int64
    var totalReadBytes int64
    var lastDamaged atomic.Value // string

    // One-time warm-up allowance for spin-up delays
    var warmupUsed int32 = 0

    walkerErrCh := make(chan error, 1)
    go func() {
        walkerErrCh <- filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
            if err != nil {
                return nil
            }
            if fi.IsDir() {
                return nil
            }
            if ih.IsForceExit() || ih.IsInterrupted() {
                return fmt.Errorf("interrupted")
            }
            atomic.AddInt64(&totalFiles, 1)
            if damaged.ShouldSkipFile(path) {
                atomic.AddInt64(&skippedFiles, 1)
                return nil
            }
            jobs <- path
            return nil
        })
        close(jobs)
    }()

    // Progress ticker
    start := time.Now()
    ticker := time.NewTicker(1 * time.Second)
    quit := make(chan struct{})
    go func() {
        for {
            select {
            case <-ticker.C:
                elapsed := time.Since(start).Seconds()
                if elapsed <= 0 {
                    elapsed = 1
                }
                readMB := float64(atomic.LoadInt64(&totalReadBytes)) / (1024.0 * 1024.0)
                speed := readMB / elapsed
                fmt.Printf("\rCHECK: found=%d, checked=%d, damaged=%d, skipped=%d, read=%.1f MB, speed=%.1f MB/s",
                    atomic.LoadInt64(&totalFiles), atomic.LoadInt64(&processedFiles), atomic.LoadInt64(&damagedFiles), atomic.LoadInt64(&skippedFiles), readMB, speed)
            case <-quit:
                return
            }
        }
    }()

    // Workers
    workerCount := runtime.NumCPU()
    if workerCount < 4 {
        workerCount = 4
    } else if workerCount > 8 {
        workerCount = 8
    }

    var wg sync.WaitGroup
    wg.Add(workerCount)

    for i := 0; i < workerCount; i++ {
        go func() {
            defer wg.Done()
            buf := make([]byte, 64*1024) // 64KB read probe
            for p := range jobs {
                if ih.IsForceExit() || ih.IsInterrupted() {
                    return
                }
                fi, err := os.Stat(p)
                if err != nil || fi.Size() == 0 {
                    continue
                }
                f, err := os.Open(p)
                if err != nil {
                    // Opening error: consider as damaged readability
                    damaged.LogDamagedFile(p, "check-open-error", fi.Size(), 1, fmt.Sprintf("open error: %v", err))
                    atomic.AddInt64(&damagedFiles, 1)
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

                t0 := time.Now()
                n, rerr := f.Read(buf)
                close(done)
                f.Close()
                elapsed := time.Since(t0)
                if n > 0 {
                    atomic.AddInt64(&totalReadBytes, int64(n))
                }

                if rerr != nil && rerr.Error() != "EOF" {
                    // Treat read errors as damage
                    damaged.LogDamagedFile(p, "check-read-error", fi.Size(), 1, fmt.Sprintf("read error: %v", rerr))
                    lastDamaged.Store(p)
                    atomic.AddInt64(&damagedFiles, 1)
                    atomic.AddInt64(&processedFiles, 1)
                    continue
                }

                if elapsed > checkReadDelayThreshold {
                    if atomic.LoadInt32(&warmupUsed) == 0 && elapsed <= checkWarmupGrace {
                        // Consume one-time warm-up allowance
                        atomic.StoreInt32(&warmupUsed, 1)
                        atomic.AddInt64(&processedFiles, 1)
                        continue
                    }
                    // Borderline delays: quick one-time retry to reduce false positives
                    if elapsed <= checkReadDelayThreshold+checkRetryWindow {
                        time.Sleep(checkRetrySleep)
                        f2, e2 := os.Open(p)
                        if e2 == nil {
                            t1 := time.Now()
                            n2, r2 := f2.Read(buf)
                            f2.Close()
                            if n2 > 0 {
                                atomic.AddInt64(&totalReadBytes, int64(n2))
                            }
                            d2 := time.Since(t1)
                            if r2 == nil || (r2 != nil && r2.Error() == "EOF") {
                                if d2 <= checkReadDelayThreshold {
                                    // Accept as OK on retry
                                    atomic.AddInt64(&processedFiles, 1)
                                    continue
                                }
                            }
                        }
                    }
                    damaged.LogDamagedFile(p, "check-delay", fi.Size(), 1, fmt.Sprintf(">2s read delay (%.1fs)", elapsed.Seconds()))
                    lastDamaged.Store(p)
                    atomic.AddInt64(&damagedFiles, 1)
                    atomic.AddInt64(&processedFiles, 1)
                } else {
                    atomic.AddInt64(&processedFiles, 1)
                }
            }
        }()
    }

    // Wait for traversal
    if err := <-walkerErrCh; err != nil && err.Error() != "interrupted" {
        return fmt.Errorf("walk error: %v", err)
    }
    wg.Wait()

    // Stop progress
    close(quit)
    ticker.Stop()
    fmt.Print("\n")

    fmt.Printf("\nCHECK completed: total=%d, skipped(damaged-before)=%d, newly-damaged=%d\n",
        atomic.LoadInt64(&totalFiles), atomic.LoadInt64(&skippedFiles), atomic.LoadInt64(&damagedFiles))
    return nil
}
