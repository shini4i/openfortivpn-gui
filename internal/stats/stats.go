package stats

import "time"

// NetworkStats contains network traffic statistics for a VPN interface.
type NetworkStats struct {
	// Interface is the network interface name (e.g., "ppp0", "tun0").
	Interface string

	// RxBytes is the total bytes received on the interface.
	RxBytes uint64
	// TxBytes is the total bytes transmitted on the interface.
	TxBytes uint64

	// RxBytesPerSec is the current receive rate in bytes per second.
	RxBytesPerSec float64
	// TxBytesPerSec is the current transmit rate in bytes per second.
	TxBytesPerSec float64

	// SessionRxBytes is the total bytes received since connection started.
	SessionRxBytes uint64
	// SessionTxBytes is the total bytes transmitted since connection started.
	SessionTxBytes uint64

	// Duration is the time elapsed since the connection was established.
	Duration time.Duration

	// Timestamp is when these statistics were collected.
	Timestamp time.Time
}
