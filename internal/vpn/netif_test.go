package vpn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsVPNInterface(t *testing.T) {
	tests := []struct {
		name      string
		ifaceName string
		expected  bool
	}{
		{"ppp0", "ppp0", true},
		{"ppp1", "ppp1", true},
		{"tun0", "tun0", true},
		{"tun1", "tun1", true},
		{"tap0", "tap0", true},
		{"tap1", "tap1", true},
		{"eth0", "eth0", false},
		{"wlan0", "wlan0", false},
		{"lo", "lo", false},
		{"docker0", "docker0", false},
		{"br0", "br0", false},
		{"virbr0", "virbr0", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVPNInterface(tt.ifaceName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectVPNInterface_EmptyIP(t *testing.T) {
	_, err := DetectVPNInterface("")
	assert.Equal(t, ErrInterfaceNotFound, err)
}

func TestDetectVPNInterface_InvalidIP(t *testing.T) {
	_, err := DetectVPNInterface("not-an-ip")
	assert.Equal(t, ErrInterfaceNotFound, err)
}

func TestDetectVPNInterface_NonExistentIP(t *testing.T) {
	// Use a valid IP format that is unlikely to be assigned locally
	_, err := DetectVPNInterface("192.0.2.1")
	assert.Equal(t, ErrInterfaceNotFound, err)
}

func TestDetectVPNInterface_IPv6Format(t *testing.T) {
	// Use a valid IPv6 format that is unlikely to be assigned locally
	_, err := DetectVPNInterface("2001:db8::1")
	assert.Equal(t, ErrInterfaceNotFound, err)
}

func TestIsVPNInterface_LongerNames(t *testing.T) {
	tests := []struct {
		name      string
		ifaceName string
		expected  bool
	}{
		{"ppp100", "ppp100", true},
		{"tun123", "tun123", true},
		{"tap999", "tap999", true},
		{"ppp with suffix", "ppp0s0", true},
		{"tun with suffix", "tun0_vpn", true},
		{"almost ppp", "xppp0", false},
		{"almost tun", "vtun0", false},
		{"ppp lowercase only", "PPP0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVPNInterface(tt.ifaceName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestErrInterfaceNotFound(t *testing.T) {
	// Verify the error message
	assert.Equal(t, "VPN interface not found", ErrInterfaceNotFound.Error())
}
