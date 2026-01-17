package vpn

import (
	"errors"
	"net"
	"strings"
	"time"
)

// ErrInterfaceNotFound is returned when the VPN interface cannot be detected.
var ErrInterfaceNotFound = errors.New("VPN interface not found")

// DetectVPNInterface finds the network interface that has the given IP address assigned.
// This is used to identify the VPN tunnel interface after connection is established.
func DetectVPNInterface(assignedIP string) (string, error) {
	if assignedIP == "" {
		return "", ErrInterfaceNotFound
	}

	targetIP := net.ParseIP(assignedIP)
	if targetIP == nil {
		return "", ErrInterfaceNotFound
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	for _, iface := range ifaces {
		// Skip non-VPN interfaces early for efficiency.
		if !isVPNInterface(iface.Name) {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip != nil && ip.Equal(targetIP) {
				return iface.Name, nil
			}
		}
	}

	return "", ErrInterfaceNotFound
}

// isVPNInterface returns true if the interface name matches common VPN interface patterns.
// openfortivpn typically creates ppp* or tun* interfaces, though tap* is also possible.
func isVPNInterface(name string) bool {
	return strings.HasPrefix(name, "ppp") || strings.HasPrefix(name, "tun") || strings.HasPrefix(name, "tap")
}

// DetectInterfaceWithRetry attempts to detect the VPN interface by IP address
// with exponential backoff retries. Returns the interface name or error after
// all retries are exhausted.
//
// Default values: maxRetries=5, initialBackoff=100ms
func DetectInterfaceWithRetry(assignedIP string, maxRetries int, initialBackoff time.Duration) (string, error) {
	if maxRetries <= 0 {
		maxRetries = 5
	}
	if initialBackoff <= 0 {
		initialBackoff = 100 * time.Millisecond
	}

	backoff := initialBackoff
	for i := 0; i < maxRetries; i++ {
		ifaceName, err := DetectVPNInterface(assignedIP)
		if err == nil {
			return ifaceName, nil
		}

		// Wait before retry with exponential backoff.
		time.Sleep(backoff)
		backoff *= 2
	}

	return "", ErrInterfaceNotFound
}
