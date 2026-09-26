package main

import (
	"fmt"
	"time"
)

type ProgressTracker struct {
	startTime      time.Time
	totalItems     int64
	currentItem    int64
	totalBytes     int64
	currentBytes   int64
	lastUpdate     time.Time
	updateInterval time.Duration
	// completionShown records that the line for reaching totalItems was
	// printed; see PrintProgress.
	completionShown bool
}

func NewProgressTracker(totalItems, totalBytes int64) *ProgressTracker {
	return &ProgressTracker{
		startTime:      time.Now(),
		totalItems:     totalItems,
		totalBytes:     totalBytes,
		lastUpdate:     time.Now(),
		updateInterval: time.Second,
	}
}

// NewProgressTrackerWithInterval creates a new progress tracker with custom update interval
func NewProgressTrackerWithInterval(totalItems, totalBytes int64, interval time.Duration) *ProgressTracker {
	return &ProgressTracker{
		startTime:      time.Now(),
		totalItems:     totalItems,
		totalBytes:     totalBytes,
		lastUpdate:     time.Now(),
		updateInterval: interval,
	}
}

// SetUpdateInterval changes the update interval
func (pt *ProgressTracker) SetUpdateInterval(interval time.Duration) {
	pt.updateInterval = interval
}

func (pt *ProgressTracker) Update(itemsDone, bytesDone int64) {
	pt.currentItem = itemsDone
	pt.currentBytes = bytesDone
}

func (pt *ProgressTracker) ShouldUpdate() bool {
	return time.Since(pt.lastUpdate) >= pt.updateInterval
}

// GetTimeSinceLastUpdate returns time since last update
func (pt *ProgressTracker) GetTimeSinceLastUpdate() time.Duration {
	return time.Since(pt.lastUpdate)
}

func (pt *ProgressTracker) GetETA() time.Duration {
	if pt.currentItem == 0 {
		return 0
	}

	elapsed := time.Since(pt.startTime)
	itemsRemaining := pt.totalItems - pt.currentItem

	if itemsRemaining <= 0 {
		return 0
	}

	avgTimePerItem := elapsed / time.Duration(pt.currentItem)
	return avgTimePerItem * time.Duration(itemsRemaining)
}

func (pt *ProgressTracker) GetCurrentSpeed() float64 {
	if pt.currentBytes == 0 {
		return 0
	}

	elapsed := time.Since(pt.startTime)
	if elapsed.Seconds() <= 0 {
		return 0
	}

	return float64(pt.currentBytes) / (1024 * 1024) / elapsed.Seconds()
}

func (pt *ProgressTracker) PrintProgress(operation string) {
	// The throttle holds, with one exception: the first time the count
	// reaches its total, so the bar ends on its last value. Only the first
	// time - a total that is an estimate (the container's chunk count) can be
	// passed by every later call, and each of those used to print a line and
	// emit an event, unthrottled (AUD-29-F4).
	if !pt.ShouldUpdate() && (pt.currentItem < pt.totalItems || pt.completionShown) {
		return
	}
	if pt.currentItem >= pt.totalItems {
		pt.completionShown = true
	}

	pt.lastUpdate = time.Now()

	speedMBps := pt.GetCurrentSpeed()
	eta := pt.GetETA()
	gbProcessed := float64(pt.currentBytes) / (1024 * 1024 * 1024)

	var etaStr string
	if eta > 0 && pt.currentItem < pt.totalItems {
		etaStr = "ETA: " + formatETA(eta)
	} else {
		etaStr = "ETA: --"
	}

	fmt.Printf("%s: %d/%d (%6.1f MB/s) - %6.2f GB %s\r",
		operation, pt.currentItem, pt.totalItems, speedMBps, gbProcessed, etaStr)

	// The same tick, on the machine channel. Every long loop in the program -
	// fill, the capacity test, pack and unpack - already draws its bar through
	// this tracker, so wiring the `progress` event here is what gives them all
	// one without thirteen call sites drifting apart. It sits after the
	// throttle above on purpose: the console line and the event are the same
	// beat (CLI-EVENT-STREAM rule 7; speedBps is bytes per second, which is
	// what the contract names, not the megabytes the console line shows).
	EmitProgressEvent(pt.currentItem, pt.totalItems, pt.currentBytes, pt.totalBytes,
		speedMBps*1024*1024, operation)
}

// PrintProgressCustom prints custom progress format without ETA (for network operations)
func (pt *ProgressTracker) PrintProgressCustom(format string, args ...interface{}) {
	if !pt.ShouldUpdate() {
		return
	}

	pt.lastUpdate = time.Now()
	fmt.Printf(format, args...)
}

// ForceUpdate marks the tracker for immediate update
func (pt *ProgressTracker) ForceUpdate() {
	pt.lastUpdate = time.Now().Add(-pt.updateInterval)
}

func (pt *ProgressTracker) Finish(operation string) {
	elapsed := time.Since(pt.startTime)
	avgSpeedMBps := float64(pt.currentBytes) / (1024 * 1024) / elapsed.Seconds()
	gbProcessed := float64(pt.currentBytes) / (1024 * 1024 * 1024)

	fmt.Printf("\n\n%s Statistics:\n", operation)
	fmt.Printf("Items processed: %d\n", pt.currentItem)
	fmt.Printf("Total data: %.2f GB\n", gbProcessed)
	fmt.Printf("Total time: %s\n", formatDuration(elapsed))
	if elapsed.Seconds() > 0 {
		fmt.Printf("Average speed: %.2f MB/s\n", avgSpeedMBps)
	}
}
