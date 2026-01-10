package vpn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnectionState_String(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected string
	}{
		{StateDisconnected, "disconnected"},
		{StateAuthenticating, "authenticating"},
		{StateConnecting, "connecting"},
		{StateConnected, "connected"},
		{StateReconnecting, "reconnecting"},
		{StateFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.state))
		})
	}
}

func TestConnectionState_IsConnected(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected bool
	}{
		{StateDisconnected, false},
		{StateAuthenticating, false},
		{StateConnecting, false},
		{StateConnected, true},
		{StateReconnecting, false},
		{StateFailed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.IsConnected())
		})
	}
}

func TestConnectionState_IsTransitioning(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected bool
	}{
		{StateDisconnected, false},
		{StateAuthenticating, true},
		{StateConnecting, true},
		{StateConnected, false},
		{StateReconnecting, true},
		{StateFailed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.IsTransitioning())
		})
	}
}

func TestConnectionState_CanConnect(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected bool
	}{
		{StateDisconnected, true},
		{StateAuthenticating, false},
		{StateConnecting, false},
		{StateConnected, false},
		{StateReconnecting, false},
		{StateFailed, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.CanConnect())
		})
	}
}

func TestConnectionState_CanDisconnect(t *testing.T) {
	tests := []struct {
		state    ConnectionState
		expected bool
	}{
		{StateDisconnected, false},
		{StateAuthenticating, true},
		{StateConnecting, true},
		{StateConnected, true},
		{StateReconnecting, true},
		{StateFailed, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.state.CanDisconnect())
		})
	}
}

func TestValidTransitions(t *testing.T) {
	validTransitions := []struct {
		from ConnectionState
		to   ConnectionState
	}{
		// From Disconnected
		{StateDisconnected, StateAuthenticating},
		{StateDisconnected, StateConnecting},

		// From Authenticating
		{StateAuthenticating, StateConnecting},
		{StateAuthenticating, StateConnected}, // SAML: tunnel connects directly after browser auth
		{StateAuthenticating, StateDisconnected},
		{StateAuthenticating, StateFailed},

		// From Connecting
		{StateConnecting, StateAuthenticating}, // SAML: openfortivpn prompts for browser auth
		{StateConnecting, StateConnected},
		{StateConnecting, StateDisconnected},
		{StateConnecting, StateFailed},

		// From Connected
		{StateConnected, StateDisconnected},
		{StateConnected, StateReconnecting},

		// From Reconnecting
		{StateReconnecting, StateConnecting},
		{StateReconnecting, StateDisconnected},
		{StateReconnecting, StateFailed},

		// From Failed
		{StateFailed, StateDisconnected},
		{StateFailed, StateAuthenticating},
		{StateFailed, StateConnecting},
	}

	for _, tt := range validTransitions {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			assert.True(t, IsValidTransition(tt.from, tt.to),
				"Expected transition from %s to %s to be valid", tt.from, tt.to)
		})
	}
}

func TestInvalidTransitions(t *testing.T) {
	invalidTransitions := []struct {
		from ConnectionState
		to   ConnectionState
	}{
		// Cannot go directly to Connected without Connecting (except from Authenticating for SAML)
		{StateDisconnected, StateConnected},

		// Cannot go to Reconnecting from non-Connected states
		{StateDisconnected, StateReconnecting},
		{StateAuthenticating, StateReconnecting},
		{StateConnecting, StateReconnecting},

		// Cannot go backward to Authenticating from Connected
		{StateConnected, StateAuthenticating},
		{StateConnected, StateConnecting},

		// Self transitions are not valid
		{StateDisconnected, StateDisconnected},
		{StateConnected, StateConnected},
	}

	for _, tt := range invalidTransitions {
		t.Run(string(tt.from)+"->"+string(tt.to), func(t *testing.T) {
			assert.False(t, IsValidTransition(tt.from, tt.to),
				"Expected transition from %s to %s to be invalid", tt.from, tt.to)
		})
	}
}

func TestAllStates(t *testing.T) {
	states := AllStates()

	assert.Len(t, states, 6)
	assert.Contains(t, states, StateDisconnected)
	assert.Contains(t, states, StateAuthenticating)
	assert.Contains(t, states, StateConnecting)
	assert.Contains(t, states, StateConnected)
	assert.Contains(t, states, StateReconnecting)
	assert.Contains(t, states, StateFailed)
}

func TestIsValidTransition_UnknownState(t *testing.T) {
	unknownState := ConnectionState("unknown")

	// Transition from unknown state should be invalid
	assert.False(t, IsValidTransition(unknownState, StateConnected))
	assert.False(t, IsValidTransition(unknownState, StateDisconnected))

	// Transition to unknown state should be invalid
	assert.False(t, IsValidTransition(StateDisconnected, unknownState))
	assert.False(t, IsValidTransition(StateConnected, unknownState))
}
