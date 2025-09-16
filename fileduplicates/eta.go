package fileduplicates

import (
	"fmt"
	"time"
)

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
