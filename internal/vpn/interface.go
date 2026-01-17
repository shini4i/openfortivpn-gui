// Package vpn provides VPN connection management for openfortivpn.
package vpn

import (
	"context"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
)

// VPNController defines the interface for VPN connection management.
// This interface allows for different implementations:
//   - Controller: Direct implementation using pkexec (requires password prompts)
//   - HelperClient: Client that communicates with privileged helper daemon (no prompts)
type VPNController interface {
	// GetState returns the current connection state.
	GetState() ConnectionState

	// GetAssignedIP returns the IP address assigned by the VPN server.
	// Returns empty string if not connected.
	GetAssignedIP() string

	// GetInterface returns the network interface name used by the VPN tunnel.
	// Returns empty string if not connected or interface not detected.
	GetInterface() string

	// CanConnect returns true if a connection can be initiated from the current state.
	CanConnect() bool

	// CanDisconnect returns true if a disconnection can be initiated from the current state.
	CanDisconnect() bool

	// Connect initiates a VPN connection with the given profile and options.
	// The context can be used to cancel the connection attempt.
	// Returns an error if the connection cannot be started.
	Connect(ctx context.Context, p *profile.Profile, opts *ConnectOptions) error

	// Disconnect terminates the active VPN connection.
	// The context can be used to cancel the disconnection attempt.
	// Returns an error if disconnection fails.
	Disconnect(ctx context.Context) error

	// OnStateChange registers a callback that is invoked when the connection state changes.
	// The callback receives the old and new connection states.
	OnStateChange(callback func(old, new ConnectionState))

	// OnOutput registers a callback that is invoked for each line of output from openfortivpn.
	// This is used for logging and debugging purposes.
	OnOutput(callback func(line string))

	// OnEvent registers a callback that is invoked when a parsed event is detected.
	// Events include things like IP assignment, authentication requests, etc.
	OnEvent(callback func(event *OutputEvent))

	// OnError registers a callback that is invoked when an error occurs.
	// This is used to display error messages to the user.
	OnError(callback func(err error))
}

// Ensure Controller implements VPNController interface.
var _ VPNController = (*Controller)(nil)
