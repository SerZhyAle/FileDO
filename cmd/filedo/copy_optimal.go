package main

import (
	"fmt"
)

// FastCopyOptimal performs copy with optimal configuration based on drive
// analysis. Files that stall or hit device errors are retried in the
// damaged-disk mode.
func FastCopyOptimal(sourcePath, targetPath string, optimalConfig *OptimalCopyConfig) error {
	if err := refuseCopyPaths("copy", sourcePath, targetPath); err != nil {
		return err
	}
	config := optimalFastCopyConfig(optimalConfig)

	fmt.Printf("Using optimal configuration:\n")
	fmt.Printf("   Workers: %d | Buffer: up to %s | Small file threshold: %s\n",
		config.MaxConcurrentFiles,
		formatSize(uint64(copyBufferLimit(config.MaxConcurrentFiles, config.MaxBufferSize))),
		formatSize(uint64(config.SmallFileThreshold)))

	return copyWithSafeFallback("copy", sourcePath, targetPath, config)
}

// optimalFastCopyConfig turns a drive analysis into an engine configuration.
//
// The analysis can ask for a 256 MB buffer (64 KB clusters on SSD, 128 KB on
// exFAT). The largest pooled buffer is 128 MB, and slicing it to 256 MB was a
// panic that took the whole process down on the first file over 128 MB
// (COPY-08); the buffer is clamped here, at the use site, and again by the
// engine to what the build's address space allows for every worker at once.
func optimalFastCopyConfig(optimalConfig *OptimalCopyConfig) FastCopyConfig {
	threads := optimalConfig.OptimalThreadCount
	if threads < 1 {
		threads = 1
	}
	maxBuffer := optimalConfig.MaxBufferSize
	if maxBuffer > copyBufferCap() {
		maxBuffer = copyBufferCap()
	}
	if maxBuffer < minCopyBuffer {
		maxBuffer = minCopyBuffer
	}
	minBuffer := optimalConfig.OptimalBufferSize / 4
	if minBuffer < minCopyBuffer {
		minBuffer = minCopyBuffer
	}
	if minBuffer > maxBuffer {
		minBuffer = maxBuffer
	}
	directIO := false
	forceFlush := false
	if optimalConfig.SourceInfo != nil && optimalConfig.TargetInfo != nil {
		directIO = optimalConfig.SourceInfo.DriveType == DriveTypeHDD || optimalConfig.TargetInfo.DriveType == DriveTypeHDD
		forceFlush = optimalConfig.TargetInfo.DriveType == DriveTypeUSB
	}
	return FastCopyConfig{
		MaxConcurrentFiles: threads,
		MinBufferSize:      minBuffer,
		MaxBufferSize:      maxBuffer,
		LargeFileThreshold: optimalConfig.SmallFileThreshold,
		PreallocateSpace:   true,
		UseMemoryMapping:   false,
		MemoryMapThreshold: 0,
		SmallFileThreshold: optimalConfig.SmallFileThreshold,
		SmallFileBatchSize: 25,
		DirectIO:           directIO,
		ForceFlush:         forceFlush,
		SyncReadWrite:      false,
	}
}
