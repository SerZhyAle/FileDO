package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// CopyStrategy represents different copy strategies
type CopyStrategy int

const (
	StrategyRegular  CopyStrategy = iota // Regular copy for simple cases
	StrategyFast                         // FastCopy for optimal performance
	StrategySync                         // SyncCopy for cache-heavy scenarios
	StrategyBalanced                     // Balanced copy for HDD-to-HDD operations
	StrategyMax                          // Maximum performance copy
)

// CopyAnalysis contains analysis results for copy strategy selection
type CopyAnalysis struct {
	Strategy          CopyStrategy
	StrategyName      string
	Reason            string
	SourceType        string
	TargetType        string
	SourceDriveInfo   *DriveInfo // Enhanced drive information
	TargetDriveInfo   *DriveInfo // Enhanced drive information
	OptimalConfig     *OptimalCopyConfig // Optimal copy configuration
	EstimatedFileCount int64
	EstimatedSize     int64
	AnalysisDuration  time.Duration
}

// AnalyzeCopyStrategy performs a brief analysis (max 15 seconds) to determine optimal copy strategy
//
// The analysis runs on its own goroutine and hands its result back by value
// over a buffered channel (COPY-18): on a timeout the goroutine can still
// finish and exit - an unbuffered send used to block it forever - and it
// never writes into a struct the caller is already reading.
func AnalyzeCopyStrategy(sourcePath, targetPath string) (*CopyAnalysis, error) {
	start := time.Now()

	fmt.Printf("🔍 Analyzing copy strategy (max 15 seconds)...\n")

	type result struct {
		sourceType, targetType string
		size, files            int64
		strategy               CopyStrategy
		name, reason           string
	}
	done := make(chan result, 1)

	go func() {
		var r result
		// Analyze source and target
		sourceInfo, sourceType := analyzeLocation(sourcePath)
		_, targetType := analyzeLocation(targetPath)
		r.sourceType, r.targetType = sourceType, targetType

		// Quick size estimation for directories
		if sourceInfo != nil && sourceInfo.IsDir() {
			r.size, r.files = quickDirectorySizeEstimate(sourcePath, 5*time.Second)
		} else if sourceInfo != nil {
			r.size, r.files = sourceInfo.Size(), 1
		} else {
			// Source doesn't exist yet, use minimal estimates
			r.size, r.files = 1024*1024, 1
		}

		r.strategy, r.name, r.reason = selectOptimalStrategy(sourceType, targetType, r.size, r.files)
		done <- r
	}()

	analysis := &CopyAnalysis{}
	select {
	case r := <-done:
		analysis.SourceType, analysis.TargetType = r.sourceType, r.targetType
		analysis.EstimatedSize, analysis.EstimatedFileCount = r.size, r.files
		analysis.Strategy, analysis.StrategyName, analysis.Reason = r.strategy, r.name, r.reason
		analysis.AnalysisDuration = time.Since(start)
		fmt.Printf("Source: %s (%s)\n", sourcePath, r.sourceType)
		fmt.Printf("Target: %s (%s)\n", targetPath, r.targetType)
		fmt.Printf("✅ Analysis completed in %v\n", analysis.AnalysisDuration)
	case <-time.After(15 * time.Second):
		analysis.AnalysisDuration = 15 * time.Second
		// Default strategy if analysis times out
		analysis.Strategy = StrategyFast
		analysis.StrategyName = "fastcopy"
		analysis.Reason = "Analysis timed out, using default fast strategy"
		fmt.Printf("⏰ Analysis timed out after 15s, using default fast strategy\n")
	}

	fmt.Printf("📋 Selected strategy: %s (%s)\n", analysis.StrategyName, analysis.Reason)
	return analysis, nil
}

// analyzeLocation determines the type of location (SSD, HDD, Network, etc.)
func analyzeLocation(path string) (os.FileInfo, string) {
	info, err := os.Stat(path)
	if err != nil {
		// Path doesn't exist, try to determine from path structure
		if strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//") {
			return nil, "Network Share"
		}
		if len(path) >= 2 && path[1] == ':' {
			driveLetter := strings.ToUpper(string(path[0]))
			driveType := analyzeDriveType(driveLetter)
			return nil, driveType
		}
		return nil, "Unknown Location"
	}
	
	// Network path detection
	if strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//") {
		return info, "Network Share"
	}
	
	// Determine drive from absolute path
	absPath, _ := filepath.Abs(path)
	if len(absPath) >= 2 && absPath[1] == ':' {
		driveLetter := strings.ToUpper(string(absPath[0]))
		driveType := analyzeDriveType(driveLetter)
		return info, driveType
	}
	
	// Explicit drive path (like D:\file.txt)
	if len(path) >= 2 && path[1] == ':' {
		driveLetter := strings.ToUpper(string(path[0]))
		driveType := analyzeDriveType(driveLetter)
		return info, driveType
	}
	
	return info, "Local Storage"
}

// AnalyzeCopyStrategyAdvanced performs comprehensive copy strategy analysis with drive detection
func AnalyzeCopyStrategyAdvanced(sourcePath, targetPath string) (*CopyAnalysis, error) {
	return analyzeCopyStrategyAdvancedWithOutput(sourcePath, targetPath, true)
}

// AnalyzeCopyStrategyQuiet performs quiet copy strategy analysis for basic copy command
func AnalyzeCopyStrategyQuiet(sourcePath, targetPath string) (*CopyAnalysis, error) {
	return analyzeCopyStrategyAdvancedWithOutput(sourcePath, targetPath, false)
}

// analyzeCopyStrategyAdvancedWithOutput performs comprehensive analysis with optional output
func analyzeCopyStrategyAdvancedWithOutput(sourcePath, targetPath string, verbose bool) (*CopyAnalysis, error) {
	startTime := time.Now()
	
	if verbose {
		fmt.Printf("🔍 Performing advanced drive analysis...\n")
	}
	
	// Extract drive letters
	sourceDrive := extractDriveLetter(sourcePath)
	targetDrive := extractDriveLetter(targetPath)
	
	if sourceDrive == "" || targetDrive == "" {
		// Fallback to basic analysis if drive letters can't be extracted
		if verbose {
			fmt.Printf("⚠️ Could not extract drive letters, using basic analysis\n")
		}
		return AnalyzeCopyStrategy(sourcePath, targetPath)
	}
	
	// Analyze source drive
	sourceInfo, err := AnalyzeDrive(sourceDrive)
	if err != nil {
		if verbose {
			fmt.Printf("⚠️ Failed to analyze source drive %s: %v, using basic analysis\n", sourceDrive, err)
		}
		return AnalyzeCopyStrategy(sourcePath, targetPath)
	}
	
	// Analyze target drive
	targetInfo, err := AnalyzeDrive(targetDrive)
	if err != nil {
		if verbose {
			fmt.Printf("⚠️ Failed to analyze target drive %s: %v, using basic analysis\n", targetDrive, err)
		}
		return AnalyzeCopyStrategy(sourcePath, targetPath)
	}
	
	// Display drive analysis results only if verbose
	if verbose {
		displayDriveAnalysis(sourceInfo, targetInfo)
	}
	
	// Get optimal configuration
	optimalConfig := GetOptimalCopyConfig(sourceInfo, targetInfo)
	
	// Quick size estimation (limit to 3 seconds for quiet mode, 5 for verbose)
	estimationTime := 3 * time.Second
	if verbose {
		estimationTime = 5 * time.Second
	}
	estimatedSize, estimatedFiles := quickDirectorySizeEstimate(sourcePath, estimationTime)
	
	// Determine strategy based on comprehensive analysis
	strategy, strategyName, reason := determineAdvancedStrategy(&optimalConfig, sourceInfo, targetInfo, estimatedSize)
	
	analysis := &CopyAnalysis{
		Strategy:          strategy,
		StrategyName:      strategyName,
		Reason:            reason,
		SourceType:        fmt.Sprintf("%s (%s, %s)", sourceInfo.DriveType, sourceInfo.FileSystem, formatSize(sourceInfo.TotalSize)),
		TargetType:        fmt.Sprintf("%s (%s, %s)", targetInfo.DriveType, targetInfo.FileSystem, formatSize(targetInfo.TotalSize)),
		SourceDriveInfo:   sourceInfo,
		TargetDriveInfo:   targetInfo,
		OptimalConfig:     &optimalConfig,
		EstimatedFileCount: estimatedFiles,
		EstimatedSize:     estimatedSize,
		AnalysisDuration:  time.Since(startTime),
	}
	
	if verbose {
		fmt.Printf("🎯 Selected strategy: %s\n", strategyName)
		fmt.Printf("📊 Optimal threads: %d, Buffer: %s, Small file threshold: %s\n", 
			optimalConfig.OptimalThreadCount,
			formatSize(uint64(optimalConfig.OptimalBufferSize)),
			formatSize(uint64(optimalConfig.SmallFileThreshold)))
		fmt.Printf("⏱️  Analysis completed in %v\n\n", analysis.AnalysisDuration)
	}
	
	return analysis, nil
}

// displayDriveAnalysis shows detailed drive information
func displayDriveAnalysis(source, target *DriveInfo) {
	fmt.Printf("📁 Source Drive %s: %s | %s | Cluster: %s | %s / %s\n",
		source.DriveLetter,
		source.DriveType,
		source.FileSystem,
		formatSize(uint64(source.ClusterSize)),
		formatSize(source.FreeSize),
		formatSize(source.TotalSize))
	
	fmt.Printf("📁 Target Drive %s: %s | %s | Cluster: %s | %s / %s\n",
		target.DriveLetter,
		target.DriveType,
		target.FileSystem,
		formatSize(uint64(target.ClusterSize)),
		formatSize(target.FreeSize),
		formatSize(target.TotalSize))
}

// determineAdvancedStrategy selects strategy based on comprehensive drive analysis
func determineAdvancedStrategy(config *OptimalCopyConfig, source, target *DriveInfo, estimatedSize int64) (CopyStrategy, string, string) {
	sourceType := source.DriveType
	targetType := target.DriveType
	
	// Check for specific optimizations based on drive combination
	switch {
	case sourceType == DriveTypeSSD && targetType == DriveTypeSSD:
		// SSD to SSD: Use maximum performance
		return StrategyMax, "Maximum Performance (SSD → SSD)", 
			fmt.Sprintf("Both drives are SSDs with %d threads, %s buffers for maximum throughput", 
				config.OptimalThreadCount, formatSize(uint64(config.OptimalBufferSize)))
	
	case sourceType == DriveTypeHDD && targetType == DriveTypeHDD:
		// HDD to HDD: Use balanced approach
		return StrategyBalanced, "Balanced (HDD → HDD)", 
			fmt.Sprintf("Both drives are HDDs, using %d threads with %s buffers for optimal sequential access", 
				config.OptimalThreadCount, formatSize(uint64(config.OptimalBufferSize)))
	
	case sourceType == DriveTypeUSB || targetType == DriveTypeUSB:
		// USB involved: Use conservative approach
		if sourceType == DriveTypeUSB && targetType == DriveTypeUSB {
			return StrategySync, "Conservative (USB → USB)", 
				fmt.Sprintf("USB to USB transfer, using single thread with %s buffers to minimize disconnection risk", 
					formatSize(uint64(config.OptimalBufferSize)))
		} else if sourceType == DriveTypeUSB {
			return StrategyFast, "Fast (USB → Internal)", 
				fmt.Sprintf("USB source detected, using %d threads with %s buffers", 
					config.OptimalThreadCount, formatSize(uint64(config.OptimalBufferSize)))
		} else {
			return StrategyFast, "Fast (Internal → USB)", 
				fmt.Sprintf("USB target detected, using %d threads with %s buffers to minimize wear", 
					config.OptimalThreadCount, formatSize(uint64(config.OptimalBufferSize)))
		}
	
	case (sourceType == DriveTypeSSD && targetType == DriveTypeHDD):
		// SSD to HDD: Balanced approach favoring read speed
		return StrategyFast, "Fast (SSD → HDD)", 
			fmt.Sprintf("SSD source with HDD target, using %d threads optimized for fast reads and sequential writes", 
				config.OptimalThreadCount)
	
	case (sourceType == DriveTypeHDD && targetType == DriveTypeSSD):
		// HDD to SSD: Balanced approach favoring write speed  
		return StrategyFast, "Fast (HDD → SSD)", 
			fmt.Sprintf("HDD source with SSD target, using %d threads optimized for sequential reads and fast writes", 
				config.OptimalThreadCount)
	
	case sourceType == DriveTypeNetwork || targetType == DriveTypeNetwork:
		// Network involved: Conservative approach
		return StrategySync, "Network Transfer", 
			fmt.Sprintf("Network drive detected, using single thread with %s buffers to handle latency", 
				formatSize(uint64(config.OptimalBufferSize)))
	
	default:
		// Default to fast copy with determined configuration
		return StrategyFast, "Fast (Auto-detected)", 
			fmt.Sprintf("Auto-detected configuration: %d threads, %s buffers", 
				config.OptimalThreadCount, formatSize(uint64(config.OptimalBufferSize)))
	}
}

// formatSize formats bytes into human-readable format
func formatSize(bytes uint64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	} else {
		return fmt.Sprintf("%.2f GB", float64(bytes)/(1024*1024*1024))
	}
}

// extractDriveLetter extracts drive letter from a path
func extractDriveLetter(path string) string {
	if len(path) >= 2 && path[1] == ':' {
		return strings.ToUpper(string(path[0]))
	}
	return ""
}
func analyzeDriveType(driveLetter string) string {
	// Enhanced drive type detection with better heuristics
	
	// System drive (C:) is usually SSD in modern systems
	if driveLetter == "C" {
		return "System Drive (likely SSD)"
	}
	
	// Common data drive patterns
	switch driveLetter {
	case "D":
		// D: is commonly a data HDD in dual-drive systems
		return "Data Drive D: (likely HDD)"
	case "E":
		// E: could be HDD or external drive
		return "Data Drive E: (HDD/External)"
	case "F", "G", "H":
		// Later letters often indicate external/USB drives
		return fmt.Sprintf("External Drive %s: (USB/HDD)", driveLetter)
	default:
		// For other drives, assume HDD unless proven otherwise
		if driveLetter >= "I" {
			return fmt.Sprintf("External Drive %s: (likely USB)", driveLetter)
		}
		return fmt.Sprintf("Data Drive %s: (HDD)", driveLetter)
	}
}

// estimateWalk is the walk quickDirectorySizeEstimate samples with. It is a
// variable so a test can make it slow (COPY-18).
var estimateWalk = filepath.Walk

// quickDirectorySizeEstimate performs a quick sampling of directory contents
//
// The sampling walk runs on its own goroutine and keeps its counts in
// atomics; the result comes back by value over a buffered channel, so a
// timeout neither races with the walk nor leaves it blocked on a send nobody
// will receive (COPY-18).
func quickDirectorySizeEstimate(dirPath string, maxTime time.Duration) (int64, int64) {
	start := time.Now()
	const maxSampledDirs = 10

	type sample struct {
		size, files int64
		dirs        int
	}
	var liveSize, liveFiles atomic.Int64
	var liveDirs atomic.Int32
	done := make(chan sample, 1)

	go func() {
		var s sample
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Warning: Directory scan panicked: %v\n", r)
			}
			done <- s
		}()

		err := estimateWalk(dirPath, func(path string, info os.FileInfo, err error) error {
			if time.Since(start) > maxTime {
				return fmt.Errorf("timeout")
			}
			if err != nil {
				return nil // Skip errors, continue sampling
			}
			if info.IsDir() {
				s.dirs++
				liveDirs.Add(1)
				if s.dirs > maxSampledDirs {
					return filepath.SkipDir // Skip remaining subdirs to save time
				}
				return nil
			}
			s.size += info.Size()
			s.files++
			liveSize.Add(info.Size())
			liveFiles.Add(1)
			return nil
		})
		if err != nil && !strings.Contains(err.Error(), "timeout") {
			fmt.Printf("Warning: Directory walk failed: %v\n", err)
		}
	}()

	var s sample
	select {
	case s = <-done:
	case <-time.After(maxTime):
		// What the walk had counted by now; it finishes on its own.
		s = sample{size: liveSize.Load(), files: liveFiles.Load(), dirs: int(liveDirs.Load())}
		fmt.Printf("Directory size estimation timed out after %v\n", maxTime)
	}

	// Extrapolate if we hit limits or return defaults
	if s.size == 0 && s.files == 0 {
		return 1024 * 1024 * 100, 100 // Default: 100MB, 100 files
	}
	if s.dirs >= maxSampledDirs {
		s.size *= 3 // Rough extrapolation
		s.files *= 3
	}
	return s.size, s.files
}

// selectOptimalStrategy chooses the best copy strategy based on analysis
func selectOptimalStrategy(sourceType, targetType string, estimatedSize, estimatedFiles int64) (CopyStrategy, string, string) {
	// Network scenarios
	if strings.Contains(sourceType, "Network") || strings.Contains(targetType, "Network") {
		return StrategyFast, "fastcopy", "Network transfer detected - using optimized parallel copy"
	}
	
	// Very large dataset scenarios - use maximum performance
	if estimatedSize > 50*1024*1024*1024 || estimatedFiles > 50000 { // >50GB or >50k files
		return StrategyMax, "maxcopy", fmt.Sprintf("Very large dataset detected (%.2f GB, %d files) - using maximum performance mode", 
			float64(estimatedSize)/(1024*1024*1024), estimatedFiles)
	}
	
	// HDD-to-HDD scenarios - use balanced mode for optimal I/O
	if strings.Contains(sourceType, "HDD") && strings.Contains(targetType, "HDD") {
		if estimatedSize > 1024*1024*1024 { // >1GB
			return StrategyBalanced, "balanced", fmt.Sprintf("HDD-to-HDD transfer detected (%.2f GB) - using balanced mode for optimal I/O", 
				float64(estimatedSize)/(1024*1024*1024))
		}
	}
	
	// Large dataset scenarios
	if estimatedSize > 10*1024*1024*1024 || estimatedFiles > 10000 { // >10GB or >10k files
		return StrategyFast, "fastcopy", fmt.Sprintf("Large dataset detected (%.2f GB, %d files) - using parallel copy", 
			float64(estimatedSize)/(1024*1024*1024), estimatedFiles)
	}
	
	// Cache-heavy scenarios (same drive type, moderate size)
	if sourceType == targetType && strings.Contains(sourceType, "Drive") && 
		estimatedSize > 1024*1024*1024 && estimatedSize < 10*1024*1024*1024 { // 1-10GB
		return StrategySync, "synccopy", "Same drive type detected with moderate size - using sync copy to avoid cache effects"
	}
	
	// SSD scenarios with large data - use maximum performance
	if (strings.Contains(sourceType, "SSD") || strings.Contains(targetType, "SSD")) &&
		estimatedSize > 5*1024*1024*1024 { // >5GB on SSD
		return StrategyMax, "maxcopy", "SSD with large dataset detected - using maximum performance mode"
	}
	
	// SSD scenarios
	if strings.Contains(sourceType, "SSD") || strings.Contains(targetType, "SSD") {
		return StrategyFast, "fastcopy", "SSD detected - using fast parallel copy for optimal performance"
	}
	
	// Small transfers
	if estimatedSize < 100*1024*1024 && estimatedFiles < 1000 { // <100MB, <1000 files
		return StrategyRegular, "copy", "Small transfer detected - using regular copy"
	}
	
	// Default to fast copy for most scenarios
	return StrategyFast, "fastcopy", "Using optimized parallel copy as default strategy"
}

// ExecuteSelectedStrategy runs the appropriate copy command based on strategy
func ExecuteSelectedStrategy(analysis *CopyAnalysis, sourcePath, targetPath string) error {
	fmt.Printf("\n🚀 Executing %s strategy...\n", analysis.StrategyName)
	
	// Use advanced execution if optimal configuration is available
	if analysis.OptimalConfig != nil {
		return ExecuteOptimalStrategy(analysis, sourcePath, targetPath)
	}
	
	// Fallback to basic strategy execution
	switch analysis.Strategy {
	case StrategyRegular:
		return handleCopyCommandNoDamage([]string{"copy", sourcePath, targetPath})
	case StrategyFast:
		return handleFastCopyCommand(sourcePath, targetPath)
	case StrategySync:
		return handleSyncCopyCommand(sourcePath, targetPath)
	case StrategyBalanced:
		return handleBalancedCopyCommand(sourcePath, targetPath)
	case StrategyMax:
		return handleMaxCopyCommand(sourcePath, targetPath)
	default:
		return fmt.Errorf("unknown copy strategy: %v", analysis.Strategy)
	}
}

// ExecuteOptimalStrategy runs copy with optimal configuration based on drive analysis
func ExecuteOptimalStrategy(analysis *CopyAnalysis, sourcePath, targetPath string) error {
	return FastCopyOptimal(sourcePath, targetPath, analysis.OptimalConfig)
}
