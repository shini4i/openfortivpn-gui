// Package vpn provides VPN connection management functionality.
package vpn

// ConnectionState represents the current state of a VPN connection.
type ConnectionState string

const (
	// StateDisconnected indicates no active VPN connection.
	StateDisconnected ConnectionState = "disconnected"
	// StateAuthenticating indicates the VPN is waiting for user authentication (e.g., SAML).
	StateAuthenticating ConnectionState = "authenticating"
	// StateConnecting indicates the VPN tunnel is being established.
	StateConnecting ConnectionState = "connecting"
	// StateConnected indicates the VPN tunnel is active.
	StateConnected ConnectionState = "connected"
	// StateReconnecting indicates the VPN is attempting to reconnect after a drop.
	StateReconnecting ConnectionState = "reconnecting"
	// StateFailed indicates the connection attempt failed.
	StateFailed ConnectionState = "failed"
)

// IsConnected returns true if the state represents an active VPN connection.
func (s ConnectionState) IsConnected() bool {
	return s == StateConnected
}

// IsTransitioning returns true if the state represents an in-progress connection attempt.
func (s ConnectionState) IsTransitioning() bool {
	return s == StateAuthenticating || s == StateConnecting || s == StateReconnecting
}

// CanConnect returns true if a new connection can be initiated from this state.
func (s ConnectionState) CanConnect() bool {
	return s == StateDisconnected || s == StateFailed
}

// CanDisconnect returns true if the connection can be terminated from this state.
func (s ConnectionState) CanDisconnect() bool {
	return s == StateAuthenticating || s == StateConnecting || s == StateConnected || s == StateReconnecting
}

// validTransitions defines the allowed state transitions.
var validTransitions = map[ConnectionState][]ConnectionState{
	StateDisconnected: {
		StateAuthenticating,
		StateConnecting,
	},
	StateAuthenticating: {
		StateConnecting,
		StateConnected, // After SAML auth completes, tunnel establishes directly
		StateDisconnected,
		StateFailed,
	},
	StateConnecting: {
		StateAuthenticating, // For SAML auth - openfortivpn prompts for browser auth during connecting
		StateConnected,
		StateDisconnected,
		StateFailed,
	},
	StateConnected: {
		StateDisconnected,
		StateReconnecting,
	},
	StateReconnecting: {
		StateConnecting,
		StateDisconnected,
		StateFailed,
	},
	StateFailed: {
		StateDisconnected,
		StateAuthenticating,
		StateConnecting,
	},
}

// IsValidTransition checks if transitioning from one state to another is allowed.
func IsValidTransition(from, to ConnectionState) bool {
	allowed, ok := validTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// AllStates returns all possible connection states.
func AllStates() []ConnectionState {
	return []ConnectionState{
		StateDisconnected,
		StateAuthenticating,
		StateConnecting,
		StateConnected,
		StateReconnecting,
		StateFailed,
	}
}
