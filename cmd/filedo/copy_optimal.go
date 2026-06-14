package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// FastCopyOptimal performs copy with optimal configuration based on drive analysis
func FastCopyOptimal(sourcePath, targetPath string, optimalConfig *OptimalCopyConfig) error {
	// First attempt: Try optimal strategy
	err := fastCopyOptimalInternal(sourcePath, targetPath, optimalConfig)
	if err == nil {
		return nil // Success!
	}
	
	// Check for errors that indicate hardware problems
	errorStr := err.Error()
	isHardwareError := strings.Contains(errorStr, "slice bounds out of range") ||
		strings.Contains(errorStr, "runtime error") ||
		strings.Contains(errorStr, "panic") ||
		strings.Contains(errorStr, "out of memory") ||
		strings.Contains(errorStr, "insufficient memory") ||
		strings.Contains(errorStr, "buffer allocation failed")
	
	if isHardwareError {
		fmt.Printf("\n⚠️  Hardware error detected: %v\n", err)
		fmt.Printf("🛡️ Automatically switching to SAFE RESCUE mode for damaged drives...\n")
		return SafeCopy(sourcePath, targetPath)
	}
	
	return err // Return original error if not hardware-related
}

// fastCopyOptimalInternal is the original implementation
func fastCopyOptimalInternal(sourcePath, targetPath string, optimalConfig *OptimalCopyConfig) error {
	// Use global interrupt handler
	handler := globalInterruptHandler
	if handler == nil {
		handler = NewInterruptHandler()
	}
	
	// Create custom configuration based on optimal settings
	config := FastCopyConfig{
		MaxConcurrentFiles: optimalConfig.OptimalThreadCount,
		MinBufferSize:      optimalConfig.OptimalBufferSize / 4,    // Quarter of optimal for small files
		MaxBufferSize:      optimalConfig.MaxBufferSize,
		LargeFileThreshold: optimalConfig.SmallFileThreshold,       // Use calculated threshold
		PreallocateSpace:   true,                                   // Enable preallocation
		UseMemoryMapping:   false,                                  // Disabled for stability
		MemoryMapThreshold: 0,                                      // Never use memory mapping
		SmallFileThreshold: optimalConfig.SmallFileThreshold,       
		SmallFileBatchSize: 25,                                     
		DirectIO:           optimalConfig.SourceInfo.DriveType == DriveTypeHDD || optimalConfig.TargetInfo.DriveType == DriveTypeHDD,
		ForceFlush:         optimalConfig.TargetInfo.DriveType == DriveTypeUSB, // Force flush for USB
		SyncReadWrite:      false,                                  // Async for performance
	}
	
	progress := &FastCopyProgress{
		StartTime:       time.Now(),
		LastSpeedUpdate: time.Now(),
	}
	
	fmt.Printf("🎯 Using optimal configuration:\n")
	fmt.Printf("   Threads: %d | Buffer: %s | Small file threshold: %s\n", 
		config.MaxConcurrentFiles, 
		formatSize(uint64(config.MaxBufferSize)),
		formatSize(uint64(config.SmallFileThreshold)))
	fmt.Printf("   DirectIO: %v | ForceFlush: %v\n", config.DirectIO, config.ForceFlush)
	
	// Check if source exists
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("source path error: %v", err)
	}
	
	if sourceInfo.IsDir() {
		// Directory copy with optimization
		err := copyDirectoryOptimized(sourcePath, targetPath, progress, config, handler)
		runtime.GC()
		debug.FreeOSMemory()
		return err
	} else {
		// Single file copy
		progress.TotalFiles = 1
		progress.TotalSize = sourceInfo.Size()
		progress.ActualFiles = 1
		progress.ActualSize = sourceInfo.Size()
		err := copyFileSingle(sourcePath, targetPath, sourceInfo, progress, config, handler)
		runtime.GC()
		debug.FreeOSMemory()
		return err
	}
}
