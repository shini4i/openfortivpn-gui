package stats

import (
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultPollInterval is the default interval between stats polls.
	DefaultPollInterval = 2 * time.Second

	// sysfsNetPath is the base path for network interface statistics.
	sysfsNetPath = "/sys/class/net"
)

// Collector periodically collects network statistics from a VPN interface.
type Collector struct {
	pollInterval time.Duration

	mu            sync.RWMutex
	interfaceName string
	baselineRx    uint64
	baselineTx    uint64
	lastRx        uint64
	lastTx        uint64
	lastTime      time.Time
	startTime     time.Time
	onStats       func(NetworkStats)

	stopChan chan struct{}
	stopped  bool
}

// NewCollector creates a new stats collector with the given poll interval.
// If pollInterval is 0, DefaultPollInterval is used.
func NewCollector(pollInterval time.Duration) *Collector {
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	return &Collector{
		pollInterval: pollInterval,
		stopChan:     make(chan struct{}),
	}
}

// OnStats registers a callback that is invoked each time new stats are collected.
// The callback is called from the polling goroutine.
func (c *Collector) OnStats(callback func(NetworkStats)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onStats = callback
}

// Start begins collecting statistics for the given interface.
// It captures the current byte counts as baseline for session totals.
// If already running with a different interface, it restarts with the new one.
func (c *Collector) Start(interfaceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.stopped && c.interfaceName != "" {
		if c.interfaceName == interfaceName {
			// Already running with same interface.
			return nil
		}
		// Running with different interface - stop the current collector.
		c.stopped = true
		close(c.stopChan)
		slog.Info("Stats collector restarting for new interface",
			"old", c.interfaceName, "new", interfaceName)
	}

	// Read initial stats for baseline.
	rx, tx, err := c.readInterfaceStats(interfaceName)
	if err != nil {
		return err
	}

	c.interfaceName = interfaceName
	c.baselineRx = rx
	c.baselineTx = tx
	c.lastRx = rx
	c.lastTx = tx
	c.lastTime = time.Now()
	c.startTime = time.Now()
	c.stopped = false
	c.stopChan = make(chan struct{})

	go c.pollLoop(c.stopChan)

	slog.Info("Stats collector started", "interface", interfaceName)
	return nil
}

// Stop stops the stats collector.
func (c *Collector) Stop() {
	c.mu.Lock()
	if c.stopped || c.interfaceName == "" {
		c.mu.Unlock()
		return
	}
	c.stopped = true
	iface := c.interfaceName
	c.interfaceName = ""
	close(c.stopChan)
	c.mu.Unlock()

	slog.Info("Stats collector stopped", "interface", iface)
}

// IsRunning returns true if the collector is actively polling.
func (c *Collector) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.stopped && c.interfaceName != ""
}

// pollLoop runs the main polling loop.
// It accepts its own stopChan to ensure it only responds to its own stop signal,
// preventing race conditions when Start() is called with a different interface.
func (c *Collector) pollLoop(stopChan <-chan struct{}) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	// Emit initial stats immediately.
	c.collectAndEmit(stopChan)

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			c.collectAndEmit(stopChan)
		}
	}
}

// collectAndEmit reads current stats, calculates rates, and emits the result.
// Handles counter rollover/reset by detecting when current values are less than previous.
// The stopChan parameter is used to verify this goroutine is still the current one.
func (c *Collector) collectAndEmit(stopChan <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if this goroutine is stale (collector was restarted with different interface).
	select {
	case <-stopChan:
		return
	default:
	}

	if c.interfaceName == "" {
		return
	}

	rx, tx, err := c.readInterfaceStats(c.interfaceName)
	if err != nil {
		slog.Debug("Failed to read interface stats", "interface", c.interfaceName, "error", err)
		return
	}

	now := time.Now()
	elapsed := now.Sub(c.lastTime).Seconds()

	// Handle counter rollover/reset: if current < last, counters were reset
	var deltaRx, deltaTx uint64
	if rx < c.lastRx {
		// RX counter reset - update baseline and use current value as delta
		slog.Debug("RX counter reset detected", "interface", c.interfaceName)
		c.baselineRx = rx
		deltaRx = 0
	} else {
		deltaRx = rx - c.lastRx
	}

	if tx < c.lastTx {
		// TX counter reset - update baseline and use current value as delta
		slog.Debug("TX counter reset detected", "interface", c.interfaceName)
		c.baselineTx = tx
		deltaTx = 0
	} else {
		deltaTx = tx - c.lastTx
	}

	// Calculate rates from deltas
	var rxRate, txRate float64
	if elapsed > 0 {
		rxRate = float64(deltaRx) / elapsed
		txRate = float64(deltaTx) / elapsed
	}

	// Calculate session totals (handle potential underflow from baseline reset)
	var sessionRx, sessionTx uint64
	if rx >= c.baselineRx {
		sessionRx = rx - c.baselineRx
	}
	if tx >= c.baselineTx {
		sessionTx = tx - c.baselineTx
	}

	stats := NetworkStats{
		Interface:      c.interfaceName,
		RxBytes:        rx,
		TxBytes:        tx,
		RxBytesPerSec:  rxRate,
		TxBytesPerSec:  txRate,
		SessionRxBytes: sessionRx,
		SessionTxBytes: sessionTx,
		Duration:       now.Sub(c.startTime),
		Timestamp:      now,
	}

	c.lastRx = rx
	c.lastTx = tx
	c.lastTime = now

	callback := c.onStats
	if callback != nil {
		// Release lock before callback to prevent deadlocks.
		c.mu.Unlock()
		callback(stats)
		c.mu.Lock()
	}
}

// readInterfaceStats reads rx_bytes and tx_bytes from sysfs for the given interface.
// The ifaceName parameter is sourced from net.Interfaces() and is considered safe
// (no path-traversal characters possible from the kernel's interface enumeration).
func (c *Collector) readInterfaceStats(ifaceName string) (rx, tx uint64, err error) {
	statsDir := filepath.Join(sysfsNetPath, ifaceName, "statistics")

	rxBytes, err := c.readStatFile(filepath.Join(statsDir, "rx_bytes"))
	if err != nil {
		return 0, 0, err
	}

	txBytes, err := c.readStatFile(filepath.Join(statsDir, "tx_bytes"))
	if err != nil {
		return 0, 0, err
	}

	return rxBytes, txBytes, nil
}

// readStatFile reads a single stat file and parses it as uint64.
// The path is constructed from sysfsNetPath and an interface name from net.Interfaces(),
// which cannot contain path-traversal characters, making this safe.
func (c *Collector) readStatFile(path string) (uint64, error) {
	// #nosec G304 -- path is constructed from sysfsNetPath constant and ifaceName from net.Interfaces()
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
