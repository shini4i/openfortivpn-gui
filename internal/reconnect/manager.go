// Package reconnect provides automatic VPN reconnection management.
package reconnect

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

const (
	// maxReconnectDelay caps the exponential backoff so reconnects keep
	// happening at a reasonable cadence even after many failed attempts.
	maxReconnectDelay = 60 * time.Second
	// reconnectJitterFraction is the relative jitter (±20%) applied to each
	// delay so multiple clients don't hammer a recovering gateway in lockstep.
	reconnectJitterFraction = 0.2
)

// reconnectDelay returns the delay before the given attempt (1-based):
// exponential backoff (base * 2^(attempt-1)) capped at maxReconnectDelay,
// with ±reconnectJitterFraction random jitter. A non-positive base defaults
// to 1 second so that jitter always applies and reconnects are never
// instantaneous.
func reconnectDelay(base time.Duration, attempt int) time.Duration {
	if base <= 0 {
		base = time.Second
	}
	if attempt < 1 {
		attempt = 1
	}

	delay := base
	for i := 1; i < attempt && delay < maxReconnectDelay; i++ {
		delay *= 2
	}
	if delay > maxReconnectDelay {
		delay = maxReconnectDelay
	}

	// #nosec G404 -- jitter spreads reconnect timing; not security-sensitive
	jitter := 1 + reconnectJitterFraction*(2*rand.Float64()-1)
	return time.Duration(float64(delay) * jitter)
}

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
	// OnFailed is called when a reconnect cannot continue and nothing else
	// reports it. Reaching the attempt limit through a terminal state change
	// does not call it: that change is itself the report.
	OnFailed func(err error)
}

// Manager handles automatic VPN reconnection logic.
// It is safe for concurrent use.
type Manager struct {
	mu           sync.Mutex
	attemptCount int
	// running is the attempt performReconnect is executing. A failure can
	// reach us twice — as a returned error and as a state change — so only the
	// first one to advance attemptCount past it re-arms.
	running                 int
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

// OnConnectionSucceeded should be called when a connection succeeds. Ends the
// reconnect sequence, so the next drop gets a full attempt budget, clears the
// user-initiated flag, and cancels any pending timer.
func (m *Manager) OnConnectionSucceeded() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endSequenceLocked()
	m.userInitiatedDisconnect = false

	// Cancel any pending reconnect timer
	if m.reconnectTimer != nil {
		if !m.reconnectTimer.Stop() {
			// Timer already fired, drain the channel to avoid stale callback
			select {
			case <-m.reconnectTimer.C:
			default:
			}
		}
		m.reconnectTimer = nil
	}
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

// ShouldReconnect reports whether a reconnect should be armed: an unexpected
// drop from Connected, or the failure of an attempt this manager started. A
// reconnect attempt never ends Connected->Disconnected, so without the second
// case a sequence would stop after one try. Reaching MaxAttempts ends it here.
func (m *Manager) ShouldReconnect(oldState, newState vpn.ConnectionState) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	// The tunnel dropped, or an attempt this manager started has just failed —
	// a reconnect never ends Connected->Disconnected, so without the second
	// case the sequence would stop after one try.
	unexpectedDrop := oldState == vpn.StateConnected && newState == vpn.StateDisconnected
	attemptFailed := m.attemptCount > 0 && m.attemptCount == m.running &&
		oldState.IsTransitioning() && newState.IsTerminal()
	if !unexpectedDrop && !attemptFailed {
		return false
	}

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
		// No OnFailed here: the state change being handled already moves the
		// display off "Reconnecting" and reports the failure.
		m.endSequenceLocked()
		return false
	}

	return true
}

// endSequence ends the reconnect sequence so the next connection starts from a
// clean count.
func (m *Manager) endSequence() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endSequenceLocked()
}

// endSequenceLocked is endSequence for callers already holding m.mu.
func (m *Manager) endSequenceLocked() {
	m.attemptCount = 0
	m.running = 0
}

// StartReconnect begins the reconnection sequence.
// Increments attempt counter and schedules a reconnection after an
// exponentially backed-off, jittered delay derived from the configured base.
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

	delay := reconnectDelay(time.Duration(m.config.DelaySeconds)*time.Second, attempt)

	// Schedule reconnect on main thread.
	// Capture timer reference to detect if it was cancelled/replaced before callback runs.
	var thisTimer *time.Timer
	m.reconnectTimer = time.AfterFunc(delay, func() {
		// Check if this timer is still the active one
		m.mu.Lock()
		if m.reconnectTimer != thisTimer {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()

		if m.scheduleOnMain != nil {
			m.scheduleOnMain(m.performReconnect)
		} else {
			m.performReconnect()
		}
	})
	thisTimer = m.reconnectTimer
	m.mu.Unlock()

	slog.Info("Scheduling reconnect attempt",
		"profile", profileName,
		"attempt", attempt,
		"max", m.config.MaxAttempts,
		"delay", delay)
}

// Cancel stops any pending reconnection attempt and ends the sequence, so the
// next connection starts from a clean attempt count.
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.endSequenceLocked()

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
	// A timer already dispatched cannot be retracted, so the sequence may have
	// ended while this was queued. Attempt zero means it did.
	if m.attemptCount == 0 {
		m.mu.Unlock()
		slog.Debug("Skipping reconnect: the sequence ended before this attempt ran")
		return
	}

	p := m.lastConnectedProfile
	attempt := m.attemptCount
	// Claim this attempt, so a failure reported twice re-arms only once.
	m.running = attempt
	userDisconnected := m.userInitiatedDisconnect
	ctx := m.ctx
	connectFunc := m.connectFunc
	passwordProvider := m.passwordProvider
	callbacks := m.callbacks
	m.mu.Unlock()

	// Each of these abandons the attempt without starting it, so the sequence
	// ends here — a claimed attempt left behind would let an unrelated later
	// failure arm a reconnect nobody asked for.
	if userDisconnected {
		slog.Debug("Skipping reconnect: user initiated disconnect during timer wait")
		m.endSequence()
		return
	}

	if p == nil {
		slog.Error("Cannot reconnect: no profile stored")
		m.endSequence()
		return
	}

	if connectFunc == nil {
		slog.Error("Cannot reconnect: no connect function configured")
		m.endSequence()
		return
	}

	slog.Info("Performing reconnect attempt", "profile", p.Name, "attempt", attempt)

	// Determine password based on auth method
	var password string
	if !p.AuthMethod.NeedsPassword() {
		// Certificate and SAML auth carry no stored password
		password = ""
	} else {
		if passwordProvider == nil {
			slog.Error("Cannot reconnect: password provider not available", "profile", p.Name)
			m.endSequence()
			if callbacks.OnFailed != nil {
				callbacks.OnFailed(errors.New("password provider not configured"))
			}
			return
		}

		var err error
		password, err = passwordProvider.Get(p.ID)
		if err != nil || password == "" {
			if err == nil {
				err = errors.New("password is empty")
			}
			slog.Error("Cannot reconnect: password not available in keyring",
				"profile", p.Name, "error", err)
			m.endSequence()
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

	// Perform the reconnection. An attempt that starts and then fails reaches
	// the next attempt through ShouldReconnect; one that never starts raises no
	// state change at all, so it has to re-arm here or the sequence ends.
	if err := connectFunc(ctx, p, password); err != nil {
		slog.Error("Reconnect failed", "profile", p.Name, "error", err)
		m.retryOrGiveUp(err)
	}
}

// retryOrGiveUp schedules another attempt, or ends the sequence once the
// configured limit is reached. It does nothing when the state change for this
// same failure has already re-armed.
func (m *Manager) retryOrGiveUp(cause error) {
	m.mu.Lock()
	// A sequence that has already ended reads (0, 0), which must not count as
	// a live attempt: nothing here may resurrect it.
	stale := m.running == 0 || m.attemptCount != m.running
	exhausted := m.attemptCount >= m.config.MaxAttempts
	if !stale && exhausted {
		m.endSequenceLocked()
	}
	onFailed := m.callbacks.OnFailed
	m.mu.Unlock()

	switch {
	case stale:
		return
	case exhausted:
		// Reported here because the caller may have raised no state change:
		// a connect refused before it began, or a failed helper round trip.
		if onFailed != nil {
			onFailed(cause)
		}
	default:
		m.StartReconnect()
	}
}
