package main

import (
    "fmt"
    "os"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "time"
)

const (
    checkReadDelayThreshold = 2 * time.Second
    checkWarmupGrace        = 10 * time.Second
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
    mode          checkMode
    workers       int
    quiet         bool
    verbose       bool
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

// Simplified CheckFolder function for CHECK subproject
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

    // Simple file scanner for CHECK subproject
    var totalFiles int64
    var processedFiles int64

    fmt.Printf("Starting CHECK scan of %s\n", root)
    
    start := time.Now()
    
    err = filepath.Walk(root, func(path string, fileInfo os.FileInfo, err error) error {
        if err != nil {
            return nil // Skip errors, continue scanning
        }
        
        if fileInfo.IsDir() {
            return nil // Skip directories
        }
        
        if ih.IsInterrupted() {
            return fmt.Errorf("interrupted")
        }
        
        totalFiles++
        
        // Simple check: try to read first 1KB of file
        if file, err := os.Open(path); err == nil {
            buffer := make([]byte, 1024)
            readStart := time.Now()
            _, readErr := file.Read(buffer)
            readTime := time.Since(readStart)
            file.Close()
            
            if readErr == nil && readTime < cfg.threshold {
                processedFiles++
                if !cfg.quiet && processedFiles%100 == 0 {
                    fmt.Printf("Processed: %d files, Time: %.1fs\r", processedFiles, time.Since(start).Seconds())
                }
            } else if readTime >= cfg.threshold {
                fmt.Printf("\nSlow read detected: %s (%.1fs)\n", path, readTime.Seconds())
            }
        }
        
        return nil
    })
    
    elapsed := time.Since(start)
    
    if !cfg.quiet {
        fmt.Printf("\nCHECK completed: total=%d, processed=%d, duration=%s\n", 
            totalFiles, processedFiles, formatDuration(elapsed))
    }
    
    return err
}

func loadCheckConfig(root string) *checkConfig {
    cfg := &checkConfig{
        threshold:   time.Duration(getEnvFloat("FILEDO_CHECK_THRESHOLD_SECONDS", 2.0) * float64(time.Second)),
        warmupGrace: time.Duration(getEnvFloat("FILEDO_CHECK_WARMUP_SECONDS", 10.0) * float64(time.Second)),
        workers:     getEnvInt("FILEDO_CHECK_WORKERS", 4),
        mode:        detectMode(),
        verbose:     getEnvInt("FILEDO_CHECK_VERBOSE", 0) == 1,
        quiet:       getEnvInt("FILEDO_CHECK_QUIET", 0) == 1,
    }
    
    if cfg.workers <= 0 {
        cfg.workers = runtime.NumCPU()
        if cfg.workers < 4 {
            cfg.workers = 4
        } else if cfg.workers > 8 {
            cfg.workers = 8
        }
    }
    
    return cfg
}