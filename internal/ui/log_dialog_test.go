package ui

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAppendLogLine verifies the log line bookkeeping: lines accumulate
// without trimming until max+chunk, then one chunked trim drops the oldest
// entries back to max. The chunked hysteresis keeps full text-buffer rebuilds
// rare instead of once per appended line.
//
// Regression test: AppendLog previously rebuilt the whole TextBuffer with
// strings.Join on every line — O(n²) over a connection's output.
func TestAppendLogLine(t *testing.T) {
	const (
		max   = 5
		chunk = 3
	)

	var lines []string
	var trims int

	// Append max+chunk-1 lines: no trim may happen yet.
	for i := 0; i < max+chunk-1; i++ {
		var trimmed bool
		lines, trimmed = appendLogLine(lines, fmt.Sprintf("line-%d", i), max, chunk)
		assert.False(t, trimmed, "no trim expected before exceeding max+chunk")
	}
	assert.Len(t, lines, max+chunk-1)

	// One more append crosses the threshold: a single trim back to max.
	lines, trimmed := appendLogLine(lines, fmt.Sprintf("line-%d", max+chunk-1), max, chunk)
	if assert.True(t, trimmed, "crossing max+chunk must trigger a trim") {
		trims++
	}
	assert.Len(t, lines, max)
	assert.Equal(t, "line-3", lines[0], "oldest lines must be dropped")
	assert.Equal(t, fmt.Sprintf("line-%d", max+chunk-1), lines[len(lines)-1],
		"newest line must be retained")

	// The next chunk of appends must again be trim-free.
	for i := 0; i < chunk-1; i++ {
		var trimmed bool
		lines, trimmed = appendLogLine(lines, "more", max, chunk)
		assert.False(t, trimmed)
	}
	assert.Equal(t, 1, trims, "rebuilds must be chunked, not per-line")
}
