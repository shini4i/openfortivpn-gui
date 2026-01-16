// Package manager provides the VPN connection manager for the helper daemon.
package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// sensitivePathPrefixes contains paths that should never be accessed via symlinks.
// These paths contain sensitive system data that could leak information if read.
var sensitivePathPrefixes = []string{
	"/etc/shadow",
	"/etc/gshadow",
	"/etc/sudoers",
	"/etc/passwd",
	"/etc/group",
	"/etc/ssh/",
	"/etc/security/",
	"/etc/pam.d/",
	"/etc/krb5.keytab",
	"/root/",
	"/proc/",
	"/sys/",
	"/dev/",
	"/boot/",
	"/var/lib/secrets/",
	"/var/log/",
}

// EventBroadcaster is called to broadcast events to all clients.
type EventBroadcaster func(event *protocol.Event)

// Manager handles VPN operations and translates between the protocol and controller.
type Manager struct {
	controller  vpn.VPNController
	broadcaster EventBroadcaster

	mu                 sync.RWMutex
	connectedProfileID string
}

// NewManager creates a new VPN manager with a default controller.
// This is a convenience wrapper around NewManagerWithController.
func NewManager(openfortivpnPath string, broadcaster EventBroadcaster) *Manager {
	return NewManagerWithController(vpn.NewControllerDirect(openfortivpnPath), broadcaster)
}

// NewManagerWithController creates a new VPN manager with the provided controller.
// This constructor allows injecting a mock controller for testing.
func NewManagerWithController(controller vpn.VPNController, broadcaster EventBroadcaster) *Manager {
	m := &Manager{
		controller:  controller,
		broadcaster: broadcaster,
	}

	// Set up callbacks to broadcast events
	m.controller.OnStateChange(m.onStateChange)
	m.controller.OnOutput(m.onOutput)
	m.controller.OnEvent(m.onEvent)
	m.controller.OnError(m.onError)

	return m
}

// HandleRequest processes a request and returns a response.
func (m *Manager) HandleRequest(req *protocol.Request) *protocol.Response {
	switch req.Command {
	case protocol.CommandConnect:
		return m.handleConnect(req)
	case protocol.CommandDisconnect:
		return m.handleDisconnect(req)
	case protocol.CommandStatus:
		return m.handleStatus(req)
	default:
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidCommand,
			fmt.Sprintf("unknown command: %s", req.Command))
	}
}

func (m *Manager) handleConnect(req *protocol.Request) *protocol.Response {
	var params protocol.ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams,
			"invalid connect params")
	}

	// Validate file paths to prevent path traversal attacks
	if err := validateFilePath(params.ClientCertPath); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams,
			fmt.Sprintf("invalid client cert path: %v", err))
	}
	if err := validateFilePath(params.ClientKeyPath); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams,
			fmt.Sprintf("invalid client key path: %v", err))
	}

	// Build profile from params
	p := &profile.Profile{
		ID:                 params.ProfileID,
		Name:               "helper-connection",
		Host:               params.Host,
		Port:               params.Port,
		Username:           params.Username,
		AuthMethod:         profile.AuthMethod(params.AuthMethod),
		Realm:              params.Realm,
		TrustedCert:        params.TrustedCert,
		ClientCertPath:     params.ClientCertPath,
		ClientKeyPath:      params.ClientKeyPath,
		SetDNS:             params.SetDNS,
		SetRoutes:          params.SetRoutes,
		HalfInternetRoutes: params.HalfInternetRoutes,
	}

	// Validate profile
	if err := p.Validate(); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeProfileInvalid,
			fmt.Sprintf("invalid profile: %v", err))
	}

	// Check if we can connect and store profile ID atomically to prevent race conditions
	// where two concurrent connects could both pass CanConnect() check
	m.mu.Lock()
	if !m.controller.CanConnect() {
		m.mu.Unlock()
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidState,
			fmt.Sprintf("cannot connect: current state is %s", m.controller.GetState()))
	}
	m.connectedProfileID = params.ProfileID
	m.mu.Unlock()

	// Build connect options
	opts := &vpn.ConnectOptions{
		Password: params.Password,
		OTP:      params.OTP,
	}

	// Initiate connection
	if err := m.controller.Connect(context.Background(), p, opts); err != nil {
		m.mu.Lock()
		m.connectedProfileID = ""
		m.mu.Unlock()
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeConnectionFailed, err.Error())
	}

	resp, err := protocol.NewSuccessResponse(req.ID, nil)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInternalError, err.Error())
	}
	return resp
}

// validateFilePath validates that a file path is safe for use with the VPN client.
// It defends against:
//   - Path traversal attacks (../)
//   - Non-absolute paths
//   - Symlink-based attacks pointing to sensitive system files
//
// The function resolves symlinks and checks that the real path doesn't point to
// sensitive system locations that could leak information through error messages.
func validateFilePath(path string) error {
	if path == "" {
		return nil // Empty paths are allowed (optional fields)
	}

	// Check for path traversal BEFORE cleaning (Clean resolves .. sequences)
	if strings.Contains(path, "..") {
		return fmt.Errorf("path traversal not allowed")
	}

	// Must be absolute path
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute")
	}

	// Resolve symlinks to get the real path
	realPath, err := resolvePathSafely(path)
	if err != nil {
		// If the file doesn't exist, we can't resolve symlinks
		// Allow it through - openfortivpn will handle the error appropriately
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if resolved path points to sensitive locations
	if isSensitivePath(realPath) {
		return fmt.Errorf("access to sensitive system path not allowed")
	}

	return nil
}

// resolvePathSafely resolves symlinks in a path, handling the case where
// intermediate directories may be symlinks.
func resolvePathSafely(path string) (string, error) {
	// First check if the path exists
	_, err := os.Lstat(path)
	if err != nil {
		return "", err
	}

	// Resolve all symlinks in the path
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}

	return resolved, nil
}

// isSensitivePath checks if a path points to a sensitive system location.
func isSensitivePath(path string) bool {
	cleanPath := filepath.Clean(path)
	for _, prefix := range sensitivePathPrefixes {
		if cleanPath == prefix || strings.HasPrefix(cleanPath, prefix) {
			return true
		}
	}
	return false
}

func (m *Manager) handleDisconnect(req *protocol.Request) *protocol.Response {
	if !m.controller.CanDisconnect() {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidState,
			fmt.Sprintf("cannot disconnect: current state is %s", m.controller.GetState()))
	}

	if err := m.controller.Disconnect(context.Background()); err != nil {
		// Clear connectedProfileID even on error - the connection may be
		// effectively terminated even if the controller reports failure.
		// This matches onStateChange cleanup behavior for edge cases.
		m.mu.Lock()
		m.connectedProfileID = ""
		m.mu.Unlock()
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeDisconnectFailed, err.Error())
	}

	m.mu.Lock()
	m.connectedProfileID = ""
	m.mu.Unlock()

	resp, err := protocol.NewSuccessResponse(req.ID, nil)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInternalError, err.Error())
	}
	return resp
}

func (m *Manager) handleStatus(req *protocol.Request) *protocol.Response {
	m.mu.RLock()
	profileID := m.connectedProfileID
	m.mu.RUnlock()

	result := protocol.StatusResult{
		State:              string(m.controller.GetState()),
		AssignedIP:         m.controller.GetAssignedIP(),
		ConnectedProfileID: profileID,
	}

	resp, err := protocol.NewSuccessResponse(req.ID, result)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInternalError, err.Error())
	}
	return resp
}

func (m *Manager) onStateChange(old, new vpn.ConnectionState) {
	event, err := protocol.NewEvent(protocol.EventStateChange, protocol.StateChangeData{
		From: string(old),
		To:   string(new),
	})
	if err != nil {
		slog.Error("Failed to create state change event", "error", err)
		return
	}
	m.broadcaster(event)

	// Clear profile ID when disconnected
	if new == vpn.StateDisconnected || new == vpn.StateFailed {
		m.mu.Lock()
		m.connectedProfileID = ""
		m.mu.Unlock()
	}
}

func (m *Manager) onOutput(line string) {
	event, err := protocol.NewEvent(protocol.EventOutput, protocol.OutputData{
		Line: line,
	})
	if err != nil {
		slog.Error("Failed to create output event", "error", err)
		return
	}
	m.broadcaster(event)
}

func (m *Manager) onEvent(e *vpn.OutputEvent) {
	data := protocol.VPNEventData{
		EventType: string(e.Type),
		Message:   e.Message,
		Data:      e.Data,
	}
	event, err := protocol.NewEvent(protocol.EventVPN, data)
	if err != nil {
		slog.Error("Failed to create VPN event", "error", err)
		return
	}
	m.broadcaster(event)
}

func (m *Manager) onError(err error) {
	event, eventErr := protocol.NewEvent(protocol.EventError, protocol.ErrorData{
		Message: err.Error(),
	})
	if eventErr != nil {
		slog.Error("Failed to create error event", "error", eventErr)
		return
	}
	m.broadcaster(event)
}

// GetState returns the current VPN state.
func (m *Manager) GetState() vpn.ConnectionState {
	return m.controller.GetState()
}

// Shutdown gracefully disconnects the VPN if connected.
// Uses a timeout to prevent hanging indefinitely.
func (m *Manager) Shutdown() {
	if m.controller.CanDisconnect() {
		slog.Info("Disconnecting VPN before shutdown")

		const shutdownTimeout = 10 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := m.controller.Disconnect(ctx); err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				slog.Error("Disconnect timed out during shutdown", "timeout", shutdownTimeout)
			} else {
				slog.Error("Failed to disconnect during shutdown", "error", err)
			}
		}
	}
}
