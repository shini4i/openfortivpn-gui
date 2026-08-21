package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// TestAssignedIPVisible asserts the tunnel address is shown only while the
// tunnel is actually up. It must not linger from the previous connection while
// the app is connecting, reconnecting or authenticating.
func TestAssignedIPVisible(t *testing.T) {
	const ip = "10.0.0.50"

	tests := []struct {
		state vpn.ConnectionState
		ip    string
		want  bool
	}{
		{vpn.StateConnected, ip, true},
		{vpn.StateConnected, "", false},
		{vpn.StateConnecting, ip, false},
		{vpn.StateReconnecting, ip, false},
		{vpn.StateAuthenticating, ip, false},
		{vpn.StateDisconnected, ip, false},
		{vpn.StateFailed, ip, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state)+"/ip="+tt.ip, func(t *testing.T) {
			assert.Equal(t, tt.want, assignedIPVisible(tt.state, tt.ip))
		})
	}
}
