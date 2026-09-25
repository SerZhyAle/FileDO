package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// The arithmetic of a capacity run (SP-0026 CAP-11, CAP-12): pure functions of
// the free space, the volume's facts and the user's numbers, so the plan a
// run will follow can be tested without a disk.

const (
	capMiB = int64(1) << 20
	capGiB = int64(1) << 30

	// capacityMinTestBytes is the least a test will write.
	capacityMinTestBytes = 100 * capMiB
	// fatMaxFileBytes is FAT's per-file limit (4 GiB - 1) in whole MiB.
	fatMaxFileBytes = 4*capGiB - capMiB
	// fatSubdirName holds the test files on FAT12/16, whose root directory
	// has room for 512 entries only.
	fatSubdirName = "FileDO_Test"
	// systemReserveFloor is the least a run leaves free on the system volume.
	systemReserveFloor = 10 * capGiB
)

func gbOf(b int64) float64 { return float64(b) / float64(capGiB) }

// systemVolumeReserve is what a run keeps free on the volume holding Windows:
// the larger of 10 GB and 10% (CAP-12). A test that fills the system volume
// is a test that can stop the machine it runs on.
func systemVolumeReserve(total int64) int64 {
	r := total / 10
	if r < systemReserveFloor {
		r = systemReserveFloor
	}
	return r
}

// fsMaxFileBytes is the largest file a file system takes (0 = no limit that
// matters here).
func fsMaxFileBytes(fs string) int64 {
	switch strings.ToUpper(fs) {
	case "FAT", "FAT12", "FAT16", "FAT32":
		return fatMaxFileBytes
	}
	return 0
}

// fsNeedsSubdir: FAT12/16 report themselves as "FAT"; their fixed root
// directory would run out of entries before the disk runs out of space.
func fsNeedsSubdir(fs string) bool {
	switch strings.ToUpper(fs) {
	case "FAT", "FAT12", "FAT16":
		return true
	}
	return false
}

// capacityPlan is what a fake-capacity test will write.
type capacityPlan struct {
	Files    int
	FileSize int64
	Budget   int64 // bytes the test may use
	Reserve  int64 // bytes kept free on purpose (the system volume)
	SubDir   string
	Notes    []string
}

// Target is the number of bytes the plan writes.
func (p capacityPlan) Target() int64 { return int64(p.Files) * p.FileSize }

// planCapacityTest turns free space and a requested file count into a plan:
// 95% of the free space (less the system volume's reserve), split into whole
// megabytes per file, never fewer than 1 MB per file, never more files than
// the space holds, never a file larger than the file system takes.
func planCapacityTest(free int64, maxFiles int, v volumeFacts) (capacityPlan, error) {
	var p capacityPlan
	if maxFiles < 1 {
		return p, fmt.Errorf("the number of test files must be at least 1, got %d", maxFiles)
	}
	budget := free / 100 * 95
	if v.SystemVolume {
		p.Reserve = systemVolumeReserve(v.Total)
		if left := free - p.Reserve; left < budget {
			budget = left
		}
		p.Notes = append(p.Notes, fmt.Sprintf(
			"System volume: keeping %.1f GB free (the larger of 10 GB and 10%% of the volume) - the test may use %.2f GB",
			gbOf(p.Reserve), gbOf(maxInt64(budget, 0))))
	}
	if budget < capacityMinTestBytes {
		if v.SystemVolume {
			return p, fmt.Errorf("insufficient free space: at least 100 MB beyond the %.1f GB kept free on the system volume is required, but only %d MB is free",
				gbOf(p.Reserve), free/capMiB)
		}
		return p, fmt.Errorf("insufficient free space: at least 100 MB required, but only %d MB available", free/capMiB)
	}
	p.Budget = budget

	fileSize := budget / int64(maxFiles) / capMiB * capMiB
	if fileSize < capMiB {
		fileSize = capMiB
	}
	files := maxFiles
	if limit := fsMaxFileBytes(v.FileSystem); limit > 0 && fileSize > limit {
		fileSize = limit
		files = int(budget / fileSize)
		p.Notes = append(p.Notes, fmt.Sprintf("%s holds at most 4 GB per file: using %d files of %d MB to cover the same space",
			v.FileSystem, files, fileSize/capMiB))
	}
	if int64(files)*fileSize > budget {
		files = int(budget / fileSize)
		p.Notes = append(p.Notes, fmt.Sprintf("%d files of %d MB do not fit: writing %d files", maxFiles, fileSize/capMiB, files))
	}
	if files < 1 {
		files = 1
	}
	if fsNeedsSubdir(v.FileSystem) {
		p.SubDir = fatSubdirName
		p.Notes = append(p.Notes, fmt.Sprintf("%s root directories hold 512 entries: the files go into %s", v.FileSystem, fatSubdirName))
	}
	p.Files, p.FileSize = files, fileSize
	return p, nil
}

// fillReserve is what a fill leaves free: on the system volume the same
// reserve as a test; elsewhere the smaller of 100 MB and 5% of the volume.
func fillReserve(v volumeFacts) int64 {
	if v.SystemVolume {
		return systemVolumeReserve(v.Total)
	}
	r := 100 * capMiB
	if f := v.Total / 20; f < r {
		r = f
	}
	return r
}

// fillPlan is what a fill will write.
type fillPlan struct {
	FileSize int64
	MaxFiles int64
	Reserve  int64
	SubDir   string
	Notes    []string
}

// planFill sizes a fill: as many files of sizeMB as fit in the free space less
// the reserve.
func planFill(free int64, sizeMB int, v volumeFacts) (fillPlan, error) {
	var p fillPlan
	p.FileSize = int64(sizeMB) * capMiB
	if limit := fsMaxFileBytes(v.FileSystem); limit > 0 && p.FileSize > limit {
		p.FileSize = limit
		p.Notes = append(p.Notes, fmt.Sprintf("%s holds at most 4 GB per file: using %d MB files", v.FileSystem, limit/capMiB))
	}
	p.Reserve = fillReserve(v)
	if v.SystemVolume {
		p.Notes = append(p.Notes, fmt.Sprintf("System volume: keeping %.1f GB free (the larger of 10 GB and 10%% of the volume)", gbOf(p.Reserve)))
	}
	p.MaxFiles = (free - p.Reserve) / p.FileSize
	if p.MaxFiles <= 0 {
		return p, fmt.Errorf("insufficient space to create even one file of %d MB (%.2f GB free, %.2f GB kept free)",
			p.FileSize/capMiB, gbOf(free), gbOf(p.Reserve))
	}
	if fsNeedsSubdir(v.FileSystem) {
		p.SubDir = fatSubdirName
		p.Notes = append(p.Notes, fmt.Sprintf("%s root directories hold 512 entries: the files go into %s", v.FileSystem, fatSubdirName))
	}
	return p, nil
}

// fillCoverageSlack is how much free space a completed fill may leave behind:
// its reserve, the remainder smaller than one file, and a margin for the file
// system's own bookkeeping.
func fillCoverageSlack(v volumeFacts, fileSize int64) int64 {
	return fillReserve(v) + fileSize + 64*capMiB
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Names

// capacityStamp is the time part of a test file's name: ddHHmmss (CAP-10 -
// the seconds were missing).
const capacityStampLayout = "02150405"

// capacityRunID is the short run id a test file's name carries: the low half
// of the run nonce.
func capacityRunID(nonce uint64) string { return fmt.Sprintf("%08x", uint32(nonce)) }

// capacityFileName is FILL_<seq>_<ddHHmmss>_<run id>.tmp.
func capacityFileName(seq int64, width int, stamp, runID string) string {
	return fmt.Sprintf("FILL_%0*d_%s_%s.tmp", width, seq, stamp, runID)
}

// The exact shapes of FileDO's own files (CAP-14). `clean` and `fill verify`
// look at nothing else.
var (
	fillNameCurrent = regexp.MustCompile(`^FILL_(\d{3,})_(\d{8})_([0-9a-f]{8})\.tmp$`)
	fillNameLegacy  = regexp.MustCompile(`^FILL_(\d{3,})_(\d{6,8})\.tmp$`)
	speedtestName   = regexp.MustCompile(`^speedtest_((download|local)_)?\d+_\d+\.txt$`)
	probeFileName   = regexp.MustCompile(`^__filedo_(access_)?test_\d+\.tmp$`)
)

// fillNameInfo parses a FILL file name.
func fillNameInfo(name string) (seq int64, runID string, ok bool) {
	if m := fillNameCurrent.FindStringSubmatch(name); m != nil {
		n, err := strconv.ParseInt(m[1], 10, 64)
		return n, m[3], err == nil
	}
	if m := fillNameLegacy.FindStringSubmatch(name); m != nil {
		n, err := strconv.ParseInt(m[1], 10, 64)
		return n, "", err == nil
	}
	return 0, "", false
}

// ---------------------------------------------------------------------------
// The result event

// filesLeftListCap bounds the paths a result event names: a counterfeit with
// 25,000 kept files must not produce a multi-megabyte result line (GUI-10).
// The full count is always a number of its own.
const filesLeftListCap = 1000

func recordFilesLeft(paths []string) {
	if len(paths) == 0 {
		return
	}
	runNumber("filesLeftTotal", len(paths))
	if len(paths) > filesLeftListCap {
		paths = paths[:filesLeftListCap]
	}
	runFilesLeft(paths...)
}
