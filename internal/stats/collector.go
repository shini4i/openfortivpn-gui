package stats

import (
	"errors"
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
func (c *Collector) Start(interfaceName string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.stopped && c.interfaceName != "" {
		// Already running.
		return nil
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

	go c.pollLoop()

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
func (c *Collector) pollLoop() {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()

	// Emit initial stats immediately.
	c.collectAndEmit()

	for {
		select {
		case <-c.stopChan:
			return
		case <-ticker.C:
			c.collectAndEmit()
		}
	}
}

// collectAndEmit reads current stats, calculates rates, and emits the result.
func (c *Collector) collectAndEmit() {
	c.mu.Lock()
	defer c.mu.Unlock()

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

	var rxRate, txRate float64
	if elapsed > 0 {
		rxRate = float64(rx-c.lastRx) / elapsed
		txRate = float64(tx-c.lastTx) / elapsed
	}

	stats := NetworkStats{
		Interface:      c.interfaceName,
		RxBytes:        rx,
		TxBytes:        tx,
		RxBytesPerSec:  rxRate,
		TxBytesPerSec:  txRate,
		SessionRxBytes: rx - c.baselineRx,
		SessionTxBytes: tx - c.baselineTx,
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
// The path is validated to ensure it's within the expected sysfs location.
func (c *Collector) readStatFile(path string) (uint64, error) {
	// Validate path is within expected sysfs location to prevent path traversal.
	// Clean the path and verify it starts with the expected base.
	cleanPath := filepath.Clean(path)
	if !strings.HasPrefix(cleanPath, sysfsNetPath+string(filepath.Separator)) {
		return 0, errors.New("invalid stats path: outside sysfs network directory")
	}

	data, err := os.ReadFile(cleanPath) // #nosec G304 -- path validated above
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
}
