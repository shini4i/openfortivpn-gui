// Package reconnect provides automatic VPN reconnection management.
package reconnect

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// Config holds reconnection configuration.
type Config struct {
	MaxAttempts  int
	DelaySeconds int
}

// DefaultConfig returns default reconnection configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		DelaySeconds: 5,
	}
}

// PasswordProvider retrieves stored passwords for reconnection.
type PasswordProvider interface {
	Get(profileID string) (string, error)
}

// ConnectFunc is a function that initiates a VPN connection.
// It should be provided by the UI layer to handle connection with proper context.
type ConnectFunc func(ctx context.Context, p *profile.Profile, password string) error

// Callbacks contains optional callbacks for reconnection events.
type Callbacks struct {
	// OnReconnecting is called when a reconnect attempt is about to start.
	OnReconnecting func()
	// OnFailed is called when reconnect fails and cannot continue.
	OnFailed func(err error)
}

// Manager handles automatic VPN reconnection logic.
// It is safe for concurrent use.
type Manager struct {
	mu                      sync.Mutex
	attemptCount            int
	reconnectTimer          *time.Timer
	userInitiatedDisconnect bool
	lastConnectedProfile    *profile.Profile

	config           Config
	passwordProvider PasswordProvider
	connectFunc      ConnectFunc
	callbacks        Callbacks
	ctx              context.Context
	scheduleOnMain   func(func()) // Schedules function to run on main/UI thread
}

// NewManager creates a new ReconnectManager.
// scheduleOnMain should schedule the provided function to run on the main/UI thread
// (e.g., glib.IdleAdd in GTK applications).
func NewManager(cfg Config, scheduleOnMain func(func())) *Manager {
	return &Manager{
		config:         cfg,
		scheduleOnMain: scheduleOnMain,
	}
}

// SetPasswordProvider sets the password provider for retrieving stored credentials.
func (m *Manager) SetPasswordProvider(provider PasswordProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.passwordProvider = provider
}

// SetConnectFunc sets the function used to initiate connections.
func (m *Manager) SetConnectFunc(fn ConnectFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.connectFunc = fn
}

// SetContext sets the context for connection operations.
func (m *Manager) SetContext(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = ctx
}

// SetCallbacks sets the event callbacks.
func (m *Manager) SetCallbacks(cb Callbacks) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = cb
}

// OnConnectionSucceeded should be called when a connection succeeds.
// Resets the attempt counter and clears user-initiated flag.
func (m *Manager) OnConnectionSucceeded() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.attemptCount = 0
	m.userInitiatedDisconnect = false
}

// SetUserDisconnect marks the next disconnect as user-initiated.
// This prevents auto-reconnect for intentional disconnections.
func (m *Manager) SetUserDisconnect() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.userInitiatedDisconnect = true
}

// StoreConnectedProfile stores a copy of the profile for potential reconnection.
// The profile is copied to prevent issues if the original is modified.
func (m *Manager) StoreConnectedProfile(p *profile.Profile) {
	if p == nil {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy to avoid mutations affecting reconnection
	profileCopy := *p
	m.lastConnectedProfile = &profileCopy
}

// ShouldReconnect determines if reconnection should be attempted based on state transition.
// Returns true if the disconnect was unexpected and reconnection is allowed.
func (m *Manager) ShouldReconnect(oldState, newState vpn.ConnectionState) bool {
	// Only trigger on unexpected disconnect from Connected state
	if oldState != vpn.StateConnected || newState != vpn.StateDisconnected {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Check for user-initiated disconnect
	if m.userInitiatedDisconnect {
		m.userInitiatedDisconnect = false // Reset flag
		slog.Debug("Skipping auto-reconnect: user-initiated disconnect")
		return false
	}

	// Check if we have a profile to reconnect
	p := m.lastConnectedProfile
	if p == nil {
		slog.Debug("Skipping auto-reconnect: no profile stored")
		return false
	}

	// Check if auto-reconnect is enabled for this profile
	if !p.AutoReconnect {
		slog.Debug("Skipping auto-reconnect: disabled for profile", "profile", p.Name)
		return false
	}

	// OTP requires user input each time, so we can't auto-reconnect
	if p.AuthMethod == profile.AuthMethodOTP {
		slog.Debug("Skipping auto-reconnect: OTP authentication requires user input", "profile", p.Name)
		return false
	}

	// Check attempt limit
	if m.attemptCount >= m.config.MaxAttempts {
		slog.Warn("Max reconnect attempts reached",
			"profile", p.Name,
			"attempts", m.attemptCount,
			"max", m.config.MaxAttempts)
		return false
	}

	return true
}

// StartReconnect begins the reconnection sequence.
// Increments attempt counter and schedules a reconnection after the configured delay.
func (m *Manager) StartReconnect() {
	m.mu.Lock()

	m.attemptCount++
	attempt := m.attemptCount
	profileName := ""
	if m.lastConnectedProfile != nil {
		profileName = m.lastConnectedProfile.Name
	}

	// Stop any existing timer
	if m.reconnectTimer != nil {
		m.reconnectTimer.Stop()
	}

	delay := time.Duration(m.config.DelaySeconds) * time.Second

	// Schedule reconnect on main thread
	m.reconnectTimer = time.AfterFunc(delay, func() {
		if m.scheduleOnMain != nil {
			m.scheduleOnMain(m.performReconnect)
		} else {
			m.performReconnect()
		}
	})
	m.mu.Unlock()

	slog.Info("Scheduling reconnect attempt",
		"profile", profileName,
		"attempt", attempt,
		"max", m.config.MaxAttempts,
		"delay", delay)
}

// Cancel stops any pending reconnection attempt.
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reconnectTimer != nil {
		m.reconnectTimer.Stop()
		m.reconnectTimer = nil
		slog.Debug("Cancelled pending reconnect")
	}
}

// GetAttemptCount returns the current reconnection attempt count.
func (m *Manager) GetAttemptCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.attemptCount
}

func (m *Manager) performReconnect() {
	m.mu.Lock()
	p := m.lastConnectedProfile
	attempt := m.attemptCount
	userDisconnected := m.userInitiatedDisconnect
	ctx := m.ctx
	connectFunc := m.connectFunc
	passwordProvider := m.passwordProvider
	callbacks := m.callbacks
	m.mu.Unlock()

	// Check if user disconnected during timer wait
	if userDisconnected {
		slog.Debug("Skipping reconnect: user initiated disconnect during timer wait")
		return
	}

	if p == nil {
		slog.Error("Cannot reconnect: no profile stored")
		return
	}

	if connectFunc == nil {
		slog.Error("Cannot reconnect: no connect function configured")
		return
	}

	slog.Info("Performing reconnect attempt", "profile", p.Name, "attempt", attempt)

	// Determine password based on auth method
	var password string
	if p.AuthMethod == profile.AuthMethodSAML {
		// SAML doesn't need a stored password
		password = ""
	} else {
		if passwordProvider == nil {
			slog.Error("Cannot reconnect: password provider not available", "profile", p.Name)
			if callbacks.OnFailed != nil {
				callbacks.OnFailed(errors.New("password provider not configured"))
			}
			return
		}

		var err error
		password, err = passwordProvider.Get(p.ID)
		if err != nil || password == "" {
			slog.Error("Cannot reconnect: password not available in keyring",
				"profile", p.Name, "error", err)
			if callbacks.OnFailed != nil {
				callbacks.OnFailed(err)
			}
			return
		}
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Notify that reconnect is starting
	if callbacks.OnReconnecting != nil {
		callbacks.OnReconnecting()
	}

	// Perform the reconnection
	if err := connectFunc(ctx, p, password); err != nil {
		slog.Error("Reconnect failed", "profile", p.Name, "error", err)
		// Don't call OnFailed here - let the state machine handle further attempts
	}
}
