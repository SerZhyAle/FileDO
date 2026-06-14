package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// DriveInfo contains detailed information about a drive
type DriveInfo struct {
	DriveLetter     string
	DriveType       DriveType
	FileSystem      string
	ClusterSize     uint32
	SectorsPerCluster uint32
	BytesPerSector  uint32
	TotalSize       uint64
	FreeSize        uint64
	IsRemovable     bool
	IsReady         bool
	VolumeName      string
	SerialNumber    uint32
}

// DriveType represents different types of storage devices
type DriveType int

const (
	DriveTypeUnknown DriveType = iota
	DriveTypeHDD
	DriveTypeSSD
	DriveTypeUSB
	DriveTypeNetwork
	DriveTypeRAM
	DriveTypeOptical
	DriveTypeRemovable
)

func (dt DriveType) String() string {
	switch dt {
	case DriveTypeHDD:
		return "HDD"
	case DriveTypeSSD:
		return "SSD"
	case DriveTypeUSB:
		return "USB"
	case DriveTypeNetwork:
		return "Network"
	case DriveTypeRAM:
		return "RAM"
	case DriveTypeOptical:
		return "Optical"
	case DriveTypeRemovable:
		return "Removable"
	default:
		return "Unknown"
	}
}

// Windows API constants
const (
	DRIVE_UNKNOWN     = 0
	DRIVE_NO_ROOT_DIR = 1
	DRIVE_REMOVABLE   = 2
	DRIVE_FIXED       = 3
	DRIVE_REMOTE      = 4
	DRIVE_CDROM       = 5
	DRIVE_RAMDISK     = 6
)

// AnalyzeDrive performs comprehensive drive analysis
func AnalyzeDrive(driveLetter string) (*DriveInfo, error) {
	if len(driveLetter) != 1 {
		return nil, fmt.Errorf("invalid drive letter: %s", driveLetter)
	}
	
	drivePath := driveLetter + ":\\"
	info := &DriveInfo{
		DriveLetter: strings.ToUpper(driveLetter),
	}
	
	// Get basic drive type from Windows API
	driveType, err := getWindowsDriveType(drivePath)
	if err != nil {
		return nil, fmt.Errorf("failed to get drive type: %v", err)
	}
	
	// Get volume information (file system, cluster size, etc.)
	err = getVolumeInformation(drivePath, info)
	if err != nil {
		return nil, fmt.Errorf("failed to get volume information: %v", err)
	}
	
	// Get disk space information
	err = getDiskSpaceInformation(drivePath, info)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk space: %v", err)
	}
	
	// Determine detailed drive type
	info.DriveType = classifyDriveType(driveType, info)
	
	return info, nil
}

// getWindowsDriveType calls Windows GetDriveType API
func getWindowsDriveType(rootPath string) (uint32, error) {
	rootPathPtr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return 0, err
	}
	
	ret, _, _ := procGetDriveType.Call(uintptr(unsafe.Pointer(rootPathPtr)))
	return uint32(ret), nil
}

// getVolumeInformation gets file system and cluster information
func getVolumeInformation(rootPath string, info *DriveInfo) error {
	rootPathPtr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return err
	}
	
	var volumeName [256]uint16
	var serialNumber uint32
	var maxComponentLength uint32
	var fileSystemFlags uint32
	var fileSystemName [256]uint16
	
	ret, _, _ := procGetVolumeInformation.Call(
		uintptr(unsafe.Pointer(rootPathPtr)),
		uintptr(unsafe.Pointer(&volumeName[0])),
		uintptr(len(volumeName)),
		uintptr(unsafe.Pointer(&serialNumber)),
		uintptr(unsafe.Pointer(&maxComponentLength)),
		uintptr(unsafe.Pointer(&fileSystemFlags)),
		uintptr(unsafe.Pointer(&fileSystemName[0])),
		uintptr(len(fileSystemName)),
	)
	
	if ret == 0 {
		return fmt.Errorf("GetVolumeInformation failed")
	}
	
	info.VolumeName = syscall.UTF16ToString(volumeName[:])
	info.SerialNumber = serialNumber
	info.FileSystem = syscall.UTF16ToString(fileSystemName[:])
	info.IsReady = true
	
	return nil
}

// getDiskSpaceInformation gets cluster size and disk space
func getDiskSpaceInformation(rootPath string, info *DriveInfo) error {
	rootPathPtr, err := syscall.UTF16PtrFromString(rootPath)
	if err != nil {
		return err
	}
	
	var sectorsPerCluster uint32
	var bytesPerSector uint32
	var freeClusters uint32
	var totalClusters uint32
	
	ret, _, _ := procGetDiskFreeSpace.Call(
		uintptr(unsafe.Pointer(rootPathPtr)),
		uintptr(unsafe.Pointer(&sectorsPerCluster)),
		uintptr(unsafe.Pointer(&bytesPerSector)),
		uintptr(unsafe.Pointer(&freeClusters)),
		uintptr(unsafe.Pointer(&totalClusters)),
	)
	
	if ret == 0 {
		return fmt.Errorf("GetDiskFreeSpace failed")
	}
	
	info.SectorsPerCluster = sectorsPerCluster
	info.BytesPerSector = bytesPerSector
	info.ClusterSize = sectorsPerCluster * bytesPerSector
	info.TotalSize = uint64(totalClusters) * uint64(info.ClusterSize)
	info.FreeSize = uint64(freeClusters) * uint64(info.ClusterSize)
	
	return nil
}

// classifyDriveType determines the specific drive type based on various factors
func classifyDriveType(windowsDriveType uint32, info *DriveInfo) DriveType {
	switch windowsDriveType {
	case DRIVE_REMOVABLE:
		info.IsRemovable = true
		// Differentiate between USB and floppy/other removable
		if info.TotalSize > 2*1024*1024*1024 { // > 2GB, likely USB
			return DriveTypeUSB
		}
		return DriveTypeRemovable
		
	case DRIVE_FIXED:
		// Fixed drives - need to determine SSD vs HDD vs USB
		// Check if this might be an external USB drive masquerading as fixed
		return classifyFixedDrive(info)
		
	case DRIVE_REMOTE:
		return DriveTypeNetwork
		
	case DRIVE_CDROM:
		return DriveTypeOptical
		
	case DRIVE_RAMDISK:
		return DriveTypeRAM
		
	default:
		return DriveTypeUnknown
	}
}

// classifyFixedDrive attempts to distinguish between SSD, HDD, and external USB for fixed drives
func classifyFixedDrive(info *DriveInfo) DriveType {
	// Heuristics for drive type detection:
	
	// 1. Check for external/USB drives that report as FIXED
	// Common patterns for external USB drives:
	driveLetterInt := int(info.DriveLetter[0])
	if driveLetterInt >= int('E') && driveLetterInt <= int('Z') {
		// Drives E: and later are often external
		// But first check if it's likely an internal drive
		if !isLikelyInternalDrive(info) && isLikelyExternalUSBDrive(info) {
			return DriveTypeUSB
		}
	}
	
	// 2. System drive (C:) is usually SSD in modern systems  
	if info.DriveLetter == "C" {
		return DriveTypeSSD
	}
	
	// 3. Check file system - newer file systems often indicate SSD
	switch info.FileSystem {
	case "ReFS":
		return DriveTypeSSD // ReFS is typically used on SSDs
	}
	
	// 4. Drive size patterns for SSD/HDD classification
	sizeGB := info.TotalSize / (1024 * 1024 * 1024)
	
	// Very large drives (> 4TB) are usually HDDs
	if sizeGB > 4000 {
		return DriveTypeHDD
	}
	
	// 5. Cluster size heuristics
	// SSDs often have larger cluster sizes for performance
	if info.ClusterSize >= 64*1024 { // 64KB+ clusters often indicate SSD optimization
		return DriveTypeSSD
	}
	
	// 6. Default classification based on drive letter for internal drives
	switch info.DriveLetter {
	case "D":
		return DriveTypeHDD // D: is commonly HDD in dual-drive systems
	default:
		// For other internal drives, assume HDD unless indicators suggest SSD
		return DriveTypeHDD
	}
}

// isLikelyExternalUSBDrive checks various indicators for external USB drives
func isLikelyExternalUSBDrive(info *DriveInfo) bool {
	sizeGB := info.TotalSize / (1024 * 1024 * 1024)
	
	// 1. exFAT file system is very common for large external drives
	if info.FileSystem == "exFAT" {
		// exFAT is primarily used for large external drives (>32GB, typically >500GB)
		if sizeGB >= 32 { // exFAT threshold - internal drives rarely use exFAT
			return true
		}
	}
	
	// 2. Very large drives (>10TB) are almost always external
	if sizeGB > 10000 {
		return true
	}
	
	// 3. NTFS drives: need more specific checks to avoid false positives
	if info.FileSystem == "NTFS" {
		// NTFS + 4K clusters + large size might be external
		if info.ClusterSize == 4096 && sizeGB >= 1500 {
			// But exclude common internal HDD sizes (3TB, 4TB, 5TB, 6TB)
			// Internal HDDs commonly come in these sizes
			internalHDDSizes := []uint64{3000, 4000, 5000, 6000}
			for _, size := range internalHDDSizes {
				if sizeGB >= size-100 && sizeGB <= size+100 {
					return false // Likely internal HDD
				}
			}
			// If not a common internal size, might be external
			return true
		}
	}
	
	// 4. Specific size patterns typical for retail external drives
	// These sizes are more common in external drives than internal ones
	retailExternalSizes := []uint64{1000, 2000, 8000, 12000, 16000} // 1TB, 2TB, 8TB, 12TB, 16TB
	
	for _, size := range retailExternalSizes {
		variance := uint64(100)
		if size >= 8000 {
			variance = 300 // Larger variance for very big drives
		}
		if sizeGB >= size-variance && sizeGB <= size+variance {
			// Additional check for exFAT or specific patterns
			if info.FileSystem == "exFAT" || sizeGB >= 8000 {
				return true
			}
		}
	}
	
	// 5. Specific size indicators for WD external drives (known retail sizes)
	// WD Books and Elements have specific sizes that differ from internal drives
	if sizeGB >= 1800 && sizeGB <= 2200 { // ~2TB WD drives
		return info.FileSystem == "exFAT" || info.FileSystem == "NTFS" 
	}
	if sizeGB >= 7200 && sizeGB <= 8200 { // ~8TB WD drives (like WD Book)
		return true // 8TB is more typical for external than internal
	}
	
	return false
}

// isLikelyInternalDrive checks if drive characteristics suggest it's an internal drive
func isLikelyInternalDrive(info *DriveInfo) bool {
	sizeGB := info.TotalSize / (1024 * 1024 * 1024)
	
	// 1. Common internal HDD sizes (manufacturer standard sizes)
	commonInternalSizes := []uint64{1000, 2000, 3000, 4000, 5000, 6000} // 1TB, 2TB, 3TB, 4TB, 5TB, 6TB
	for _, size := range commonInternalSizes {
		if sizeGB >= size-50 && sizeGB <= size+50 {
			// Additional indicators for internal drives
			if info.FileSystem == "NTFS" {
				// NTFS on common internal sizes is typically internal
				return true
			}
		}
	}
	
	// 2. Drive letters D:, F:, G: with NTFS are commonly internal
	switch info.DriveLetter {
	case "D", "F", "G":
		if info.FileSystem == "NTFS" && sizeGB >= 500 && sizeGB <= 6000 {
			return true
		}
	}
	
	return false
}

// GetOptimalCopyConfig returns optimized copy configuration based on drive analysis
func GetOptimalCopyConfig(sourceDrive, targetDrive *DriveInfo) OptimalCopyConfig {
	config := OptimalCopyConfig{
		SourceInfo: sourceDrive,
		TargetInfo: targetDrive,
	}
	
	// Determine optimal strategy based on source and target drive types
	sourceType := sourceDrive.DriveType
	targetType := targetDrive.DriveType
	
	// Set buffer sizes based on cluster sizes
	config.SmallFileThreshold = calculateSmallFileThreshold(sourceDrive, targetDrive)
	config.OptimalBufferSize = calculateOptimalBufferSize(sourceDrive, targetDrive)
	config.MaxBufferSize = calculateMaxBufferSize(sourceDrive, targetDrive)
	
	// Set thread count based on drive types
	config.OptimalThreadCount = calculateOptimalThreadCount(sourceType, targetType)
	
	// Set copy strategy
	config.Strategy = determineCopyStrategy(sourceType, targetType, sourceDrive, targetDrive)
	
	// Set file system specific optimizations
	config.FileSystemOptimizations = getFileSystemOptimizations(sourceDrive, targetDrive)
	
	return config
}

// OptimalCopyConfig contains optimized settings for copy operations
type OptimalCopyConfig struct {
	SourceInfo              *DriveInfo
	TargetInfo              *DriveInfo
	Strategy                string
	OptimalThreadCount      int
	SmallFileThreshold      int64
	OptimalBufferSize       int
	MaxBufferSize          int
	FileSystemOptimizations map[string]interface{}
}

// calculateSmallFileThreshold determines the optimal threshold for small files
func calculateSmallFileThreshold(source, target *DriveInfo) int64 {
	// Base threshold on cluster sizes
	maxClusterSize := source.ClusterSize
	if target.ClusterSize > maxClusterSize {
		maxClusterSize = target.ClusterSize
	}
	
	// Small files should be at least 4x cluster size for efficiency
	threshold := int64(maxClusterSize) * 4
	
	// Adjust based on drive types
	if source.DriveType == DriveTypeSSD && target.DriveType == DriveTypeSSD {
		// SSD to SSD can handle smaller thresholds efficiently
		threshold = int64(maxClusterSize) * 2
	} else if source.DriveType == DriveTypeUSB || target.DriveType == DriveTypeUSB {
		// USB drives benefit from larger thresholds
		threshold = int64(maxClusterSize) * 8
	}
	
	// Set reasonable bounds
	if threshold < 64*1024 {
		threshold = 64 * 1024 // Minimum 64KB
	} else if threshold > 4*1024*1024 {
		threshold = 4 * 1024 * 1024 // Maximum 4MB
	}
	
	return threshold
}

// calculateOptimalBufferSize determines the best buffer size for the drive combination
func calculateOptimalBufferSize(source, target *DriveInfo) int {
	// Start with cluster-aligned buffer sizes
	maxClusterSize := source.ClusterSize
	if target.ClusterSize > maxClusterSize {
		maxClusterSize = target.ClusterSize
	}
	
	// Base buffer size on drive types
	var bufferSize uint32
	
	sourceType := source.DriveType
	targetType := target.DriveType
	
	switch {
	case sourceType == DriveTypeSSD && targetType == DriveTypeSSD:
		// SSD to SSD: Large buffers for maximum throughput
		bufferSize = maxClusterSize * 1024 // Start with 1024 clusters
	case sourceType == DriveTypeHDD && targetType == DriveTypeHDD:
		// HDD to HDD: Medium buffers to balance sequential access
		bufferSize = maxClusterSize * 512 // 512 clusters
	case sourceType == DriveTypeUSB || targetType == DriveTypeUSB:
		// USB involved: Smaller buffers to reduce latency
		bufferSize = maxClusterSize * 256 // 256 clusters
	default:
		// Mixed types: Conservative approach
		bufferSize = maxClusterSize * 512 // 512 clusters
	}
	
	// Set reasonable bounds based on file system
	minSize := uint32(1 * 1024 * 1024)  // 1MB minimum
	maxSize := uint32(64 * 1024 * 1024) // 64MB maximum
	
	// Adjust maximums based on file systems
	if source.FileSystem == "FAT32" || target.FileSystem == "FAT32" {
		maxSize = 32 * 1024 * 1024 // 32MB for FAT32
	} else if source.FileSystem == "exFAT" || target.FileSystem == "exFAT" {
		maxSize = 128 * 1024 * 1024 // 128MB for exFAT
	}
	
	if bufferSize < minSize {
		bufferSize = minSize
	} else if bufferSize > maxSize {
		bufferSize = maxSize
	}
	
	return int(bufferSize)
}

// calculateMaxBufferSize determines the maximum buffer size for large files
func calculateMaxBufferSize(source, target *DriveInfo) int {
	optimalSize := calculateOptimalBufferSize(source, target)
	
	// Maximum buffer is typically 2-4x the optimal size
	maxSize := optimalSize * 4
	
	// Set absolute limits based on available memory and drive types
	absoluteMax := 256 * 1024 * 1024 // 256MB absolute maximum
	
	if source.DriveType == DriveTypeUSB || target.DriveType == DriveTypeUSB {
		absoluteMax = 64 * 1024 * 1024 // 64MB for USB to avoid latency
	}
	
	if maxSize > absoluteMax {
		maxSize = absoluteMax
	}
	
	return maxSize
}

// calculateOptimalThreadCount determines the best number of threads for the drive combination
func calculateOptimalThreadCount(sourceType, targetType DriveType) int {
	// Thread count optimization based on drive characteristics
	
	switch {
	case sourceType == DriveTypeSSD && targetType == DriveTypeSSD:
		// SSD to SSD: Can handle many parallel operations
		return 16
	case sourceType == DriveTypeHDD && targetType == DriveTypeHDD:
		// HDD to HDD: Limit threads to maintain sequential access patterns  
		return 4
	case sourceType == DriveTypeUSB || targetType == DriveTypeUSB:
		// USB involved: Very limited parallel access
		return 2
	case (sourceType == DriveTypeSSD && targetType == DriveTypeHDD) ||
		 (sourceType == DriveTypeHDD && targetType == DriveTypeSSD):
		// Mixed SSD/HDD: Balance between source and target capabilities
		return 8
	case sourceType == DriveTypeNetwork || targetType == DriveTypeNetwork:
		// Network drives: Moderate parallelism
		return 6
	default:
		// Conservative default
		return 4
	}
}

// determineCopyStrategy selects the best copy strategy based on drive analysis
func determineCopyStrategy(sourceType, targetType DriveType, source, target *DriveInfo) string {
	// Strategy selection based on comprehensive analysis
	
	totalSizeGB := source.TotalSize / (1024 * 1024 * 1024)
	
	switch {
	case sourceType == DriveTypeSSD && targetType == DriveTypeSSD:
		// SSD to SSD: Maximum performance strategy
		return "MaxPerformance"
	case sourceType == DriveTypeHDD && targetType == DriveTypeHDD:
		// HDD to HDD: Balanced strategy optimized for sequential access
		return "Balanced"
	case sourceType == DriveTypeUSB || targetType == DriveTypeUSB:
		// USB involved: Conservative strategy to minimize wear and disconnection risk
		return "Conservative"
	case totalSizeGB < 100:
		// Small drives: Fast strategy
		return "Fast"
	default:
		// Default balanced approach
		return "Balanced"
	}
}

// getFileSystemOptimizations returns specific optimizations for file systems
func getFileSystemOptimizations(source, target *DriveInfo) map[string]interface{} {
	opts := make(map[string]interface{})
	
	// Source file system optimizations
	switch source.FileSystem {
	case "NTFS":
		opts["source_supports_large_files"] = true
		opts["source_supports_compression"] = true
		opts["source_cluster_size"] = source.ClusterSize
	case "FAT32":
		opts["source_max_file_size"] = uint64(4*1024*1024*1024 - 1) // 4GB - 1 byte
		opts["source_cluster_size"] = source.ClusterSize
	case "exFAT":
		opts["source_supports_large_files"] = true
		opts["source_optimized_for_flash"] = true
		opts["source_cluster_size"] = source.ClusterSize
	}
	
	// Target file system optimizations  
	switch target.FileSystem {
	case "NTFS":
		opts["target_supports_large_files"] = true
		opts["target_supports_compression"] = true
		opts["target_cluster_size"] = target.ClusterSize
	case "FAT32":
		opts["target_max_file_size"] = uint64(4*1024*1024*1024 - 1) // 4GB - 1 byte
		opts["target_requires_file_splitting"] = true
		opts["target_cluster_size"] = target.ClusterSize
	case "exFAT":
		opts["target_supports_large_files"] = true
		opts["target_optimized_for_flash"] = true
		opts["target_cluster_size"] = target.ClusterSize
	}
	
	// Cross-file system optimizations
	if source.FileSystem != target.FileSystem {
		opts["cross_filesystem"] = true
		if source.FileSystem == "NTFS" && (target.FileSystem == "FAT32" || target.FileSystem == "exFAT") {
			opts["strip_ntfs_features"] = true
		}
	}
	
	return opts
}
