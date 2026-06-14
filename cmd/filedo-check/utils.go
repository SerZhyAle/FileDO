package main

import (
	"fmt"
	"time"
)

// formatDuration formats a duration for short output with consistent spacing
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	} else {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
}

// formatETA formats duration for ETA display in human-readable format
func formatETA(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	secs := d.Seconds()
	if secs < 60 {
		return fmt.Sprintf("%.0fs", secs)
	} else if secs < 3600 {
		m := int64(secs) / 60
		s := secs - float64(m*60)
		return fmt.Sprintf("%dm %.0fs", m, s)
	} else {
		h := int64(secs) / 3600
		rem := secs - float64(h*3600)
		m := int64(rem) / 60
		s := rem - float64(m*60)
		return fmt.Sprintf("%dh %dm %.0fs", h, m, s)
	}
}

// formatBytes formats bytes into human-readable format
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// formatBytesToMB formats bytes to MB
func formatBytesToMB(b int64) string {
	mbFloat := float64(b) / (1024 * 1024)
	return fmt.Sprintf("%.2f MB", mbFloat)
}

// min returns the minimum of two integers  
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}