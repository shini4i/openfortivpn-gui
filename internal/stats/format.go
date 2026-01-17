package stats

import (
	"fmt"
	"time"
)

const (
	// Binary unit multipliers (1024-based).
	kib = 1024
	mib = kib * 1024
	gib = mib * 1024
	tib = gib * 1024
)

// FormatBytes formats a byte count using binary units (KiB, MiB, GiB, TiB).
func FormatBytes(bytes uint64) string {
	switch {
	case bytes >= tib:
		return fmt.Sprintf("%.1f TiB", float64(bytes)/float64(tib))
	case bytes >= gib:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gib))
	case bytes >= mib:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(mib))
	case bytes >= kib:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/float64(kib))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// FormatRate formats a bytes-per-second rate using binary units.
func FormatRate(bytesPerSec float64) string {
	switch {
	case bytesPerSec >= float64(gib):
		return fmt.Sprintf("%.1f GiB/s", bytesPerSec/float64(gib))
	case bytesPerSec >= float64(mib):
		return fmt.Sprintf("%.1f MiB/s", bytesPerSec/float64(mib))
	case bytesPerSec >= float64(kib):
		return fmt.Sprintf("%.1f KiB/s", bytesPerSec/float64(kib))
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}
}

// FormatDuration formats a duration in a human-readable format.
// Returns formats like "1h 23m 45s", "23m 45s", or "45s" depending on duration.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm %ds", hours, minutes, seconds)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
