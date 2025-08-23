package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	EstimatedFileCount int64
	EstimatedSize     int64
	AnalysisDuration  time.Duration
}

// AnalyzeCopyStrategy performs a brief analysis (max 15 seconds) to determine optimal copy strategy
func AnalyzeCopyStrategy(sourcePath, targetPath string) (*CopyAnalysis, error) {
	start := time.Now()
	analysis := &CopyAnalysis{}
	
	fmt.Printf("🔍 Analyzing copy strategy (max 15 seconds)...\n")
	
	// Create timeout channel
	timeout := time.After(15 * time.Second)
	done := make(chan bool)
	
	go func() {
		defer func() { done <- true }()
		
		// Analyze source and target
		sourceInfo, sourceType := analyzeLocation(sourcePath)
		_, targetType := analyzeLocation(targetPath)
		
		analysis.SourceType = sourceType
		analysis.TargetType = targetType
		
		fmt.Printf("Source: %s (%s)\n", sourcePath, sourceType)
		fmt.Printf("Target: %s (%s)\n", targetPath, targetType)
		
		// Quick size estimation for directories
		if sourceInfo != nil && sourceInfo.IsDir() {
			estimateSize, estimateFiles := quickDirectorySizeEstimate(sourcePath, 5*time.Second)
			analysis.EstimatedSize = estimateSize
			analysis.EstimatedFileCount = estimateFiles
		} else if sourceInfo != nil {
			analysis.EstimatedSize = sourceInfo.Size()
			analysis.EstimatedFileCount = 1
		} else {
			// Source doesn't exist yet, use minimal estimates
			analysis.EstimatedSize = 1024 * 1024 // 1MB
			analysis.EstimatedFileCount = 1
		}
		
		// Determine strategy based on analysis
		analysis.Strategy, analysis.StrategyName, analysis.Reason = selectOptimalStrategy(
			sourceType, targetType, analysis.EstimatedSize, analysis.EstimatedFileCount)
	}()
	
	// Wait for completion or timeout
	select {
	case <-done:
		analysis.AnalysisDuration = time.Since(start)
		fmt.Printf("✅ Analysis completed in %v\n", analysis.AnalysisDuration)
	case <-timeout:
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

// analyzeDriveType attempts to determine drive type (SSD/HDD/USB/etc.)
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

// quickDirectorySizeEstimate performs a quick sampling of directory contents
func quickDirectorySizeEstimate(dirPath string, maxTime time.Duration) (int64, int64) {
	start := time.Now()
	var totalSize int64
	var fileCount int64
	var sampledDirs int
	const maxSampledDirs = 10
	
	// Create timeout channel for safety
	timeout := time.After(maxTime)
	done := make(chan bool)
	
	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Warning: Directory scan panicked: %v\n", r)
			}
			done <- true
		}()
		
		err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
			if time.Since(start) > maxTime {
				return fmt.Errorf("timeout")
			}
			
			if err != nil {
				return nil // Skip errors, continue sampling
			}
			
			if info.IsDir() {
				sampledDirs++
				if sampledDirs > maxSampledDirs {
					return filepath.SkipDir // Skip remaining subdirs to save time
				}
				return nil
			}
			
			totalSize += info.Size()
			fileCount++
			return nil
		})
		
		if err != nil && !strings.Contains(err.Error(), "timeout") {
			fmt.Printf("Warning: Directory walk failed: %v\n", err)
		}
	}()
	
	// Wait for completion or timeout
	select {
	case <-done:
		// Completed normally
	case <-timeout:
		// Timed out
		fmt.Printf("Directory size estimation timed out after %v\n", maxTime)
	}
	
	// Extrapolate if we hit limits or return defaults
	if totalSize == 0 && fileCount == 0 {
		return 1024 * 1024 * 100, 100 // Default: 100MB, 100 files
	}
	
	if sampledDirs >= maxSampledDirs {
		totalSize = totalSize * 3 // Rough extrapolation
		fileCount = fileCount * 3
	}
	
	return totalSize, fileCount
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
	
	switch analysis.Strategy {
	case StrategyRegular:
		return handleCopyCommand([]string{"copy", sourcePath, targetPath})
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
