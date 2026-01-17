package stats

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"zero", 0, "0 B"},
		{"one byte", 1, "1 B"},
		{"just under 1 KiB", 1023, "1023 B"},
		{"exactly 1 KiB", 1024, "1.0 KiB"},
		{"1.5 KiB", 1536, "1.5 KiB"},
		{"just under 1 MiB", 1024*1024 - 1, "1024.0 KiB"},
		{"exactly 1 MiB", 1024 * 1024, "1.0 MiB"},
		{"1.5 MiB", 1024 * 1024 * 3 / 2, "1.5 MiB"},
		{"exactly 1 GiB", 1024 * 1024 * 1024, "1.0 GiB"},
		{"exactly 1 TiB", 1024 * 1024 * 1024 * 1024, "1.0 TiB"},
		{"large value", 1024 * 1024 * 1024 * 1024 * 10, "10.0 TiB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatRate(t *testing.T) {
	tests := []struct {
		name        string
		bytesPerSec float64
		expected    string
	}{
		{"zero", 0, "0 B/s"},
		{"one byte per second", 1, "1 B/s"},
		{"just under 1 KiB/s", 1023, "1023 B/s"},
		{"exactly 1 KiB/s", 1024, "1.0 KiB/s"},
		{"1.5 KiB/s", 1536, "1.5 KiB/s"},
		{"exactly 1 MiB/s", 1024 * 1024, "1.0 MiB/s"},
		{"exactly 1 GiB/s", 1024 * 1024 * 1024, "1.0 GiB/s"},
		{"fractional value", 512.5, "512 B/s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatRate(tt.bytesPerSec)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{"zero", 0, "0s"},
		{"one second", time.Second, "1s"},
		{"30 seconds", 30 * time.Second, "30s"},
		{"one minute", time.Minute, "1m 0s"},
		{"1 minute 30 seconds", time.Minute + 30*time.Second, "1m 30s"},
		{"one hour", time.Hour, "1h 0m 0s"},
		{"1 hour 30 minutes", time.Hour + 30*time.Minute, "1h 30m 0s"},
		{"1 hour 30 minutes 45 seconds", time.Hour + 30*time.Minute + 45*time.Second, "1h 30m 45s"},
		{"24 hours", 24 * time.Hour, "24h 0m 0s"},
		{"sub-second", 500 * time.Millisecond, "0s"},
		{"negative", -time.Second, "0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatDuration(tt.duration)
			assert.Equal(t, tt.expected, result)
		})
	}
}
