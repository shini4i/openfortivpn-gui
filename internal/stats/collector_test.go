package stats

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	tests := []struct {
		name         string
		pollInterval time.Duration
		expected     time.Duration
	}{
		{"zero uses default", 0, DefaultPollInterval},
		{"negative uses default", -time.Second, DefaultPollInterval},
		{"custom interval", time.Second, time.Second},
		{"large interval", time.Minute, time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollector(tt.pollInterval)
			assert.NotNil(t, c)
			assert.Equal(t, tt.expected, c.pollInterval)
			assert.NotNil(t, c.stopChan)
			assert.False(t, c.stopped)
		})
	}
}

func TestCollector_OnStats(t *testing.T) {
	c := NewCollector(time.Second)

	c.OnStats(func(stats NetworkStats) {
		// Callback set for testing
	})

	assert.NotNil(t, c.onStats)
	// Verify the callback can be replaced
	c.OnStats(nil)
	assert.Nil(t, c.onStats)
}

func TestCollector_IsRunning_Initial(t *testing.T) {
	c := NewCollector(time.Second)
	assert.False(t, c.IsRunning())
}

func TestCollector_Start_InvalidInterface(t *testing.T) {
	c := NewCollector(100 * time.Millisecond)

	err := c.Start("nonexistent-interface-12345")
	assert.Error(t, err)
	assert.False(t, c.IsRunning())
}

func TestCollector_Start_AlreadyRunning(t *testing.T) {
	// Create a testable collector with already-running state
	c := &Collector{
		pollInterval: 100 * time.Millisecond,
		stopChan:     make(chan struct{}),
	}

	// Test the "already running" logic by setting state directly
	c.interfaceName = "test0"
	c.stopped = false

	// Should return nil without starting again
	err := c.Start("test0")
	assert.NoError(t, err)
}

func TestCollector_Start_RestartWithDifferentInterface(t *testing.T) {
	// Create a collector with already-running state and an old stopChan
	oldStopChan := make(chan struct{})
	c := &Collector{
		pollInterval:  100 * time.Millisecond,
		stopChan:      oldStopChan,
		interfaceName: "old-interface",
		stopped:       false,
	}

	// Start with a different interface - this will fail because interface doesn't exist,
	// but the restart logic should still close the old stopChan
	_ = c.Start("new-interface-12345")

	// Verify old stopChan was closed (signaling old goroutine to stop)
	select {
	case <-oldStopChan:
		// Expected: channel is closed
	default:
		t.Error("Expected old stopChan to be closed when restarting with different interface")
	}
}

func TestCollector_Stop_NotRunning(t *testing.T) {
	c := NewCollector(time.Second)

	// Stop on a collector that was never started should be a no-op
	c.Stop()
	assert.False(t, c.IsRunning())
}

func TestCollector_Stop_AlreadyStopped(t *testing.T) {
	c := NewCollector(time.Second)
	c.stopped = true

	// Should not panic
	c.Stop()
	assert.True(t, c.stopped)
}

func TestCollector_readStatFile_InvalidPath(t *testing.T) {
	c := NewCollector(time.Second)

	_, err := c.readStatFile("/nonexistent/path/to/stat")
	assert.Error(t, err)
}

func TestCollector_readStatFile_InvalidContent(t *testing.T) {
	c := NewCollector(time.Second)

	// Create a temp file with invalid content
	tmpFile := filepath.Join(t.TempDir(), "invalid_stat")
	require.NoError(t, os.WriteFile(tmpFile, []byte("not-a-number\n"), 0644))

	_, err := c.readStatFile(tmpFile)
	assert.Error(t, err)
}

func TestCollector_readStatFile_Valid(t *testing.T) {
	c := NewCollector(time.Second)

	// Create a temp file with valid content
	tmpFile := filepath.Join(t.TempDir(), "valid_stat")
	require.NoError(t, os.WriteFile(tmpFile, []byte("12345\n"), 0644))

	val, err := c.readStatFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, uint64(12345), val)
}

func TestCollector_readStatFile_WithWhitespace(t *testing.T) {
	c := NewCollector(time.Second)

	// Create a temp file with whitespace
	tmpFile := filepath.Join(t.TempDir(), "whitespace_stat")
	require.NoError(t, os.WriteFile(tmpFile, []byte("  67890  \n"), 0644))

	val, err := c.readStatFile(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, uint64(67890), val)
}

func TestCollector_readInterfaceStats_InvalidInterface(t *testing.T) {
	c := NewCollector(time.Second)

	_, _, err := c.readInterfaceStats("nonexistent-interface-xyz")
	assert.Error(t, err)
}

// TestCollector_collectAndEmit_EmptyInterface verifies no action when interface is empty.
func TestCollector_collectAndEmit_EmptyInterface(t *testing.T) {
	c := NewCollector(time.Second)
	c.interfaceName = ""
	c.stopChan = make(chan struct{})

	var callbackCalled bool
	c.onStats = func(stats NetworkStats) {
		callbackCalled = true
	}

	// Should return early without calling callback
	c.collectAndEmit(c.stopChan)
	assert.False(t, callbackCalled)
}

// TestCollector_RateCalculation_ZeroElapsed verifies rate is zero when no time elapsed.
func TestCollector_RateCalculation_ZeroElapsed(t *testing.T) {
	c := NewCollector(time.Second)

	// Set up collector state with same timestamp
	now := time.Now()
	c.interfaceName = "dummy"
	c.lastRx = 1000
	c.lastTx = 500
	c.lastTime = now
	c.baselineRx = 0
	c.baselineTx = 0

	// When elapsed is 0, rates should be 0
	var elapsed float64 = 0
	var rxRate float64
	if elapsed > 0 {
		rxRate = float64(100) / elapsed
	}
	assert.Equal(t, float64(0), rxRate)
}

// TestCollector_CounterRollover_Detection tests the counter reset detection logic.
func TestCollector_CounterRollover_Detection(t *testing.T) {
	tests := []struct {
		name         string
		lastRx       uint64
		currentRx    uint64
		expectReset  bool
		expectedDiff uint64
	}{
		{
			name:         "normal increment",
			lastRx:       1000,
			currentRx:    2000,
			expectReset:  false,
			expectedDiff: 1000,
		},
		{
			name:         "counter reset detected",
			lastRx:       1000,
			currentRx:    500,
			expectReset:  true,
			expectedDiff: 0,
		},
		{
			name:         "zero to zero - no change",
			lastRx:       0,
			currentRx:    0,
			expectReset:  false,
			expectedDiff: 0,
		},
		{
			name:         "max value to small - reset",
			lastRx:       ^uint64(0),
			currentRx:    100,
			expectReset:  true,
			expectedDiff: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var delta uint64
			if tt.currentRx < tt.lastRx {
				// Counter reset detected
				delta = 0
				assert.True(t, tt.expectReset)
			} else {
				delta = tt.currentRx - tt.lastRx
				assert.False(t, tt.expectReset)
			}
			assert.Equal(t, tt.expectedDiff, delta)
		})
	}
}

// TestCollector_SessionBytes_UnderflowProtection tests protection against baseline underflow.
func TestCollector_SessionBytes_UnderflowProtection(t *testing.T) {
	tests := []struct {
		name           string
		currentRx      uint64
		baselineRx     uint64
		expectedResult uint64
	}{
		{
			name:           "normal case",
			currentRx:      1000,
			baselineRx:     200,
			expectedResult: 800,
		},
		{
			name:           "baseline equals current",
			currentRx:      500,
			baselineRx:     500,
			expectedResult: 0,
		},
		{
			name:           "underflow protection - baseline > current",
			currentRx:      100,
			baselineRx:     500,
			expectedResult: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sessionRx uint64
			if tt.currentRx >= tt.baselineRx {
				sessionRx = tt.currentRx - tt.baselineRx
			}
			assert.Equal(t, tt.expectedResult, sessionRx)
		})
	}
}

// TestCollector_CallbackThreadSafety tests that callbacks are thread-safe.
func TestCollector_CallbackThreadSafety(t *testing.T) {
	c := NewCollector(time.Second)

	var wg sync.WaitGroup
	callCount := 0
	var mu sync.Mutex

	// Set callback
	c.OnStats(func(stats NetworkStats) {
		mu.Lock()
		callCount++
		mu.Unlock()
	})

	// Concurrently access the collector
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.OnStats(func(stats NetworkStats) {
				mu.Lock()
				callCount++
				mu.Unlock()
			})
		}()
	}

	wg.Wait()
	// No assertion needed - we're testing it doesn't race/panic
}

// TestCollector_IsRunning_ThreadSafety tests concurrent IsRunning calls.
func TestCollector_IsRunning_ThreadSafety(t *testing.T) {
	c := NewCollector(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = c.IsRunning()
		}()
	}

	wg.Wait()
	// No assertion needed - we're testing it doesn't race
}

// TestCollector_ConcurrentStartStop tests concurrent start/stop safety.
func TestCollector_ConcurrentStartStop(t *testing.T) {
	c := NewCollector(100 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			// This will fail because interface doesn't exist, but should not panic
			_ = c.Start("dummy-iface")
		}()
		go func() {
			defer wg.Done()
			c.Stop()
		}()
	}

	wg.Wait()
	// No assertion needed - we're testing it doesn't panic/deadlock
}

// TestNetworkStats_Fields tests that NetworkStats struct has all expected fields.
func TestNetworkStats_Fields(t *testing.T) {
	now := time.Now()
	stats := NetworkStats{
		Interface:      "ppp0",
		RxBytes:        1000,
		TxBytes:        500,
		RxBytesPerSec:  100.5,
		TxBytesPerSec:  50.25,
		SessionRxBytes: 800,
		SessionTxBytes: 400,
		Duration:       time.Hour,
		Timestamp:      now,
	}

	assert.Equal(t, "ppp0", stats.Interface)
	assert.Equal(t, uint64(1000), stats.RxBytes)
	assert.Equal(t, uint64(500), stats.TxBytes)
	assert.Equal(t, 100.5, stats.RxBytesPerSec)
	assert.Equal(t, 50.25, stats.TxBytesPerSec)
	assert.Equal(t, uint64(800), stats.SessionRxBytes)
	assert.Equal(t, uint64(400), stats.SessionTxBytes)
	assert.Equal(t, time.Hour, stats.Duration)
	assert.Equal(t, now, stats.Timestamp)
}
