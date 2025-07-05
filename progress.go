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

func (pt *ProgressTracker) Update(itemsDone, bytesDone int64) {
	pt.currentItem = itemsDone
	pt.currentBytes = bytesDone
}

func (pt *ProgressTracker) ShouldUpdate() bool {
	return time.Since(pt.lastUpdate) >= pt.updateInterval
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
	if !pt.ShouldUpdate() && pt.currentItem < pt.totalItems {
		return
	}

	pt.lastUpdate = time.Now()

	percentComplete := float64(pt.currentItem) / float64(pt.totalItems) * 100
	speedMBps := pt.GetCurrentSpeed()
	eta := pt.GetETA()
	gbProcessed := float64(pt.currentBytes) / (1024 * 1024 * 1024)

	var etaStr string
	if eta > 0 && pt.currentItem < pt.totalItems {
		if eta < time.Minute {
			etaStr = fmt.Sprintf("ETA: %ds", int(eta.Seconds()))
		} else if eta < time.Hour {
			etaStr = fmt.Sprintf("ETA: %dm%ds", int(eta.Minutes()), int(eta.Seconds())%60)
		} else {
			etaStr = fmt.Sprintf("ETA: %dh%dm", int(eta.Hours()), int(eta.Minutes())%60)
		}
	} else {
		etaStr = "ETA: --"
	}

	fmt.Printf("%s: %3.0f%% %d/%d items (%6.1f MB/s) - %6.2f GB %s\r",
		operation, percentComplete, pt.currentItem, pt.totalItems, speedMBps, gbProcessed, etaStr)
}

func (pt *ProgressTracker) Finish(operation string) {
	elapsed := time.Since(pt.startTime)
	avgSpeedMBps := float64(pt.currentBytes) / (1024 * 1024) / elapsed.Seconds()
	gbProcessed := float64(pt.currentBytes) / (1024 * 1024 * 1024)

	fmt.Printf("\n\n%s Complete!\n", operation)
	fmt.Printf("Items processed: %d\n", pt.currentItem)
	fmt.Printf("Total data: %.2f GB\n", gbProcessed)
	fmt.Printf("Total time: %s\n", formatDuration(elapsed))
	if elapsed.Seconds() > 0 {
		fmt.Printf("Average speed: %.2f MB/s\n", avgSpeedMBps)
	}
}
