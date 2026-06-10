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

// EventSender delivers events to clients. Broadcast reaches every connected
// client; SendToClient reaches exactly one. The split exists for privacy:
// coarse state changes are public, but openfortivpn output and error text can
// carry connection details and must only reach the client that initiated the
// connection.
type EventSender interface {
	Broadcast(event *protocol.Event)
	SendToClient(clientID string, event *protocol.Event) error
}

// Manager handles VPN operations and translates between the protocol and controller.
type Manager struct {
	controller vpn.VPNController
	sender     EventSender

	mu                 sync.RWMutex
	connectedProfileID string
	activeClientID     string // client that initiated the current connection
}

// NewManager creates a new VPN manager with a default controller.
// This is a convenience wrapper around NewManagerWithController.
func NewManager(openfortivpnPath string, sender EventSender) *Manager {
	return NewManagerWithController(vpn.NewController(openfortivpnPath, vpn.WithDirectMode()), sender)
}

// NewManagerWithController creates a new VPN manager with the provided controller.
// This constructor allows injecting a mock controller for testing.
// Panics if sender is nil: silently dropping events would hide the
// client-scoping guarantees this type exists to provide.
func NewManagerWithController(controller vpn.VPNController, sender EventSender) *Manager {
	if sender == nil {
		panic("manager: NewManagerWithController called with nil EventSender")
	}

	m := &Manager{
		controller: controller,
		sender:     sender,
	}

	// Set up callbacks to forward controller events to clients
	m.controller.OnStateChange(m.onStateChange)
	m.controller.OnOutput(m.onOutput)
	m.controller.OnEvent(m.onEvent)
	m.controller.OnError(m.onError)

	return m
}

// HandleRequest processes a request from the given client and returns a response.
func (m *Manager) HandleRequest(req *protocol.Request, clientID string) *protocol.Response {
	switch req.Command {
	case protocol.CommandConnect:
		return m.handleConnect(req, clientID)
	case protocol.CommandDisconnect:
		return m.handleDisconnect(req)
	case protocol.CommandStatus:
		return m.handleStatus(req)
	default:
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidCommand,
			fmt.Sprintf("unknown command: %s", req.Command))
	}
}

func (m *Manager) handleConnect(req *protocol.Request, clientID string) *protocol.Response {
	var params protocol.ConnectParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams,
			"invalid connect params")
	}

	// Validate file paths to prevent path traversal attacks. The RESOLVED
	// paths are what gets handed to openfortivpn: validating one path and
	// opening another would leave a symlink-swap TOCTOU window.
	certPath, err := validateAndResolveFilePath(params.ClientCertPath)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidParams,
			fmt.Sprintf("invalid client cert path: %v", err))
	}
	keyPath, err := validateAndResolveFilePath(params.ClientKeyPath)
	if err != nil {
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
		ClientCertPath:     certPath,
		ClientKeyPath:      keyPath,
		SetDNS:             params.SetDNS,
		SetRoutes:          params.SetRoutes,
		HalfInternetRoutes: params.HalfInternetRoutes,
	}

	// Validate profile
	if err := p.Validate(); err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeProfileInvalid,
			fmt.Sprintf("invalid profile: %v", err))
	}

	// Check if we can connect and store profile/client IDs atomically to
	// prevent race conditions where two concurrent connects could both pass
	// the CanConnect() check
	m.mu.Lock()
	if !m.controller.CanConnect() {
		m.mu.Unlock()
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInvalidState,
			fmt.Sprintf("cannot connect: current state is %s", m.controller.GetState()))
	}
	m.connectedProfileID = params.ProfileID
	m.activeClientID = clientID
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
		m.activeClientID = ""
		m.mu.Unlock()
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeConnectionFailed, err.Error())
	}

	resp, err := protocol.NewSuccessResponse(req.ID, nil)
	if err != nil {
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeInternalError, err.Error())
	}
	return resp
}

// validateAndResolveFilePath validates that a file path is safe for use with
// the VPN client and returns the symlink-resolved path the caller MUST use.
// It defends against:
//   - Path traversal to sensitive locations (".." segments are normalized by
//     Clean and the cleaned path is checked against the sensitive list)
//   - Non-absolute paths
//   - Symlink-based attacks pointing to sensitive system files
//
// The sensitive-path policy is enforced lexically on the cleaned path before
// any filesystem access, so the rejection holds even when the target cannot
// be stat'd (e.g. files under /root unreadable even to root on hardened
// systems). The resolved path is then re-checked to catch a harmless-looking
// path that resolves INTO a sensitive location via symlinks.
//
// Returning the resolved path collapses the symlink-swap vector for the
// validated path: an attacker can no longer have us validate a benign symlink
// and let openfortivpn (running as root) follow it to a sensitive target after
// the check. It does NOT eliminate the generic open-time TOCTOU that exists
// for any privileged process opening a user-supplied path — a directory
// component on the resolved path could still be swapped between resolution and
// open. The residual risk is bounded by the sensitive-prefix policy above and
// by the fact that cert/key contents are never echoed back to the client.
//
// For paths that don't exist yet, symlinks can't be resolved; the cleaned
// path is returned so openfortivpn reports the missing file itself.
func validateAndResolveFilePath(path string) (string, error) {
	if path == "" {
		return "", nil // Empty paths are allowed (optional fields)
	}

	// Must be absolute path
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be absolute")
	}

	cleanPath := filepath.Clean(path)

	// Lexical policy check FIRST, before any filesystem access. A path that
	// already points into a sensitive prefix must be rejected regardless of
	// whether the helper can stat it. The helper runs as root and could stat
	// most targets, but files like those under /root or /etc/shadow may be
	// unreadable even to root on hardened systems; relying on a successful
	// Lstat to reach the policy check would let "permission denied" mask the
	// rejection and surface a confusing error instead of the policy one.
	if isSensitivePath(cleanPath) {
		return "", fmt.Errorf("access to sensitive system path not allowed")
	}

	realPath, err := resolvePathSafely(cleanPath)
	if err != nil {
		// If the file doesn't exist, we can't resolve symlinks. The lexical
		// check above already cleared the cleaned path, so hand it back and
		// let openfortivpn report the missing-file error.
		if os.IsNotExist(err) {
			return cleanPath, nil
		}
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}

	// Symlink defense: a path that looks harmless lexically may resolve INTO
	// a sensitive location. Re-check the resolved target.
	if isSensitivePath(realPath) {
		return "", fmt.Errorf("access to sensitive system path not allowed")
	}

	return realPath, nil
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
		// Clear connection tracking even on error - the connection may be
		// effectively terminated even if the controller reports failure.
		// This matches onStateChange cleanup behavior for edge cases.
		m.clearActiveConnection()
		return protocol.NewErrorResponse(req.ID, protocol.ErrCodeDisconnectFailed, err.Error())
	}

	m.clearActiveConnection()

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

// clearActiveConnection resets the connection-tracking state.
func (m *Manager) clearActiveConnection() {
	m.mu.Lock()
	m.connectedProfileID = ""
	m.activeClientID = ""
	m.mu.Unlock()
}

// sendToActiveClient delivers an event only to the client that initiated the
// current connection. If no connection is active the event is dropped:
// openfortivpn output and error text can carry connection details (usernames,
// gateway responses) that must not reach bystander clients on the socket.
func (m *Manager) sendToActiveClient(event *protocol.Event) {
	m.mu.RLock()
	clientID := m.activeClientID
	m.mu.RUnlock()

	if clientID == "" {
		return
	}

	if err := m.sender.SendToClient(clientID, event); err != nil {
		slog.Warn("Failed to send event to client", "client", clientID, "error", err)
	}
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
	// Coarse connection state is intentionally public: every client needs it
	// to render the correct UI state.
	m.sender.Broadcast(event)

	// Clear connection tracking when disconnected
	if new == vpn.StateDisconnected || new == vpn.StateFailed {
		m.clearActiveConnection()
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
	m.sendToActiveClient(event)
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
	m.sendToActiveClient(event)
}

func (m *Manager) onError(err error) {
	event, eventErr := protocol.NewEvent(protocol.EventError, protocol.ErrorData{
		Message: err.Error(),
	})
	if eventErr != nil {
		slog.Error("Failed to create error event", "error", eventErr)
		return
	}
	m.sendToActiveClient(event)
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
