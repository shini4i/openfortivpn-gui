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

// binaryUnit returns the largest binary unit that value reaches, as its divisor
// and suffix. Values below 1 KiB get a divisor of 1.
func binaryUnit(value float64) (float64, string) {
	switch {
	case value >= tib:
		return tib, "TiB"
	case value >= gib:
		return gib, "GiB"
	case value >= mib:
		return mib, "MiB"
	case value >= kib:
		return kib, "KiB"
	default:
		return 1, "B"
	}
}

// FormatBytes formats a byte count using binary units (KiB, MiB, GiB, TiB).
func FormatBytes(bytes uint64) string {
	if bytes < kib {
		return fmt.Sprintf("%d B", bytes)
	}

	divisor, unit := binaryUnit(float64(bytes))
	return fmt.Sprintf("%.1f %s", float64(bytes)/divisor, unit)
}

// FormatRate formats a bytes-per-second rate using binary units.
func FormatRate(bytesPerSec float64) string {
	if bytesPerSec < kib {
		return fmt.Sprintf("%.0f B/s", bytesPerSec)
	}

	divisor, unit := binaryUnit(bytesPerSec)
	return fmt.Sprintf("%.1f %s/s", bytesPerSec/divisor, unit)
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
