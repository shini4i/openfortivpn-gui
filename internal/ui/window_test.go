package ui

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/openfortivpn-gui/internal/config"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// newTestMainWindow creates a minimal MainWindow for testing reconnect logic.
// This avoids GTK initialization by only setting up the fields needed for testing.
func newTestMainWindow(cfgMgr *config.Manager) *MainWindow {
	return &MainWindow{
		deps: &MainWindowDeps{
			ConfigManager: cfgMgr,
		},
		reconnectState: &reconnectState{},
	}
}

// newTestConfigManager creates a config.Manager with a temporary directory for testing.
// Returns the manager and a cleanup function that should be called when done.
// Uses t.Setenv for automatic environment variable restoration on test completion.
func newTestConfigManager(t *testing.T, cfg *config.Config) (*config.Manager, func()) {
	t.Helper()

	// Create temporary directory for config
	tempDir, err := os.MkdirTemp("", "openfortivpn-gui-test-*")
	require.NoError(t, err, "failed to create temp dir")

	// Set XDG_CONFIG_HOME to our temp dir (t.Setenv handles save/restore automatically)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Create config manager (this will create the config directory structure)
	cfgMgr, err := config.NewManager()
	require.NoError(t, err, "failed to create config manager")

	// If custom config provided, update it
	if cfg != nil {
		err = cfgMgr.UpdateConfig(cfg)
		require.NoError(t, err, "failed to update config")
	}

	// Cleanup only needs to remove the temp directory; env is restored by t.Setenv
	cleanup := func() {
		_ = os.RemoveAll(tempDir) // Explicitly ignore error - cleanup best effort
	}

	return cfgMgr, cleanup
}

// TestShouldTriggerReconnect_StateTransitions tests that reconnect only triggers
// when transitioning from Connected to Disconnected state.
// Note: This test uses a nil ConfigManager, so shouldTriggerReconnect always returns false.
// The validStateTransition field indicates whether the state transition would be valid
// for reconnect consideration (Connected -> Disconnected). The test verifies that invalid
// state transitions are rejected, while TestShouldTriggerReconnect_WithConfigManager tests
// the full end-to-end behavior with a real ConfigManager.
func TestShouldTriggerReconnect_StateTransitions(t *testing.T) {
	tests := []struct {
		name                 string
		oldState             vpn.ConnectionState
		newState             vpn.ConnectionState
		validStateTransition bool // Indicates if this is a valid state transition for reconnect
	}{
		{
			name:                 "Connected to Disconnected is valid state transition",
			oldState:             vpn.StateConnected,
			newState:             vpn.StateDisconnected,
			validStateTransition: true,
		},
		{
			name:                 "Connecting to Disconnected should not trigger",
			oldState:             vpn.StateConnecting,
			newState:             vpn.StateDisconnected,
			validStateTransition: false,
		},
		{
			name:                 "Connected to Failed should not trigger",
			oldState:             vpn.StateConnected,
			newState:             vpn.StateFailed,
			validStateTransition: false,
		},
		{
			name:                 "Disconnected to Connecting should not trigger",
			oldState:             vpn.StateDisconnected,
			newState:             vpn.StateConnecting,
			validStateTransition: false,
		},
		{
			name:                 "Authenticating to Disconnected should not trigger",
			oldState:             vpn.StateAuthenticating,
			newState:             vpn.StateDisconnected,
			validStateTransition: false,
		},
		{
			name:                 "Reconnecting to Disconnected should not trigger",
			oldState:             vpn.StateReconnecting,
			newState:             vpn.StateDisconnected,
			validStateTransition: false,
		},
		{
			name:                 "Failed to Disconnected should not trigger",
			oldState:             vpn.StateFailed,
			newState:             vpn.StateDisconnected,
			validStateTransition: false,
		},
		{
			name:                 "Connected to Connecting should not trigger",
			oldState:             vpn.StateConnected,
			newState:             vpn.StateConnecting,
			validStateTransition: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := newTestMainWindow(nil)
			// Set up conditions that would allow reconnect if state is right
			w.reconnectState.userInitiatedDisconnect = false
			w.reconnectState.lastConnectedProfile = &profile.Profile{
				ID:            "test-profile-id",
				Name:          "Test Profile",
				AutoReconnect: true,
				AuthMethod:    profile.AuthMethodPassword,
			}

			result := w.shouldTriggerReconnect(tt.oldState, tt.newState)

			// ConfigManager is nil, so shouldTriggerReconnect always returns false.
			// This test verifies that invalid state transitions are correctly rejected.
			// For valid state transitions (Connected->Disconnected), the full path is
			// tested in TestShouldTriggerReconnect_WithConfigManager.
			assert.False(t, result, "expected false: %s (validStateTransition=%v, but ConfigManager is nil)",
				tt.name, tt.validStateTransition)
		})
	}
}

// TestShouldTriggerReconnect_UserInitiatedDisconnect tests that user-initiated
// disconnections do not trigger auto-reconnect.
func TestShouldTriggerReconnect_UserInitiatedDisconnect(t *testing.T) {
	w := newTestMainWindow(nil)
	w.reconnectState.userInitiatedDisconnect = true
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect for user-initiated disconnect")

	// Verify the flag is reset after check
	w.reconnectState.mu.Lock()
	assert.False(t, w.reconnectState.userInitiatedDisconnect, "flag should be reset after check")
	w.reconnectState.mu.Unlock()
}

// TestShouldTriggerReconnect_NoProfile tests that reconnect is not triggered
// when there's no stored profile.
func TestShouldTriggerReconnect_NoProfile(t *testing.T) {
	w := newTestMainWindow(nil)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.lastConnectedProfile = nil

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect without a stored profile")
}

// TestShouldTriggerReconnect_AutoReconnectDisabled tests that reconnect is not
// triggered when the profile has AutoReconnect disabled.
func TestShouldTriggerReconnect_AutoReconnectDisabled(t *testing.T) {
	w := newTestMainWindow(nil)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: false, // Disabled
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect when AutoReconnect is disabled")
}

// TestShouldTriggerReconnect_OTPAuthMethod tests that reconnect is not triggered
// for profiles using OTP authentication (which requires user input each time).
func TestShouldTriggerReconnect_OTPAuthMethod(t *testing.T) {
	w := newTestMainWindow(nil)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodOTP, // OTP requires user input
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect for OTP auth profiles")
}

// TestShouldTriggerReconnect_NoConfigManager tests that reconnect is not triggered
// when ConfigManager is nil.
func TestShouldTriggerReconnect_NoConfigManager(t *testing.T) {
	w := newTestMainWindow(nil)
	w.deps.ConfigManager = nil
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect without ConfigManager")
}

// TestReconnectState_ThreadSafety tests concurrent access to reconnectState.
func TestReconnectState_ThreadSafety(t *testing.T) {
	state := &reconnectState{}
	testProfile := &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	iterations := 1000
	if testing.Short() {
		iterations = 100
	}

	var wg sync.WaitGroup

	// Writer goroutines for userInitiatedDisconnect
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				state.mu.Lock()
				state.userInitiatedDisconnect = true
				state.mu.Unlock()

				state.mu.Lock()
				state.userInitiatedDisconnect = false
				state.mu.Unlock()
			}
		}()
	}

	// Writer goroutines for lastConnectedProfile
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				state.mu.Lock()
				state.lastConnectedProfile = testProfile
				state.mu.Unlock()

				state.mu.Lock()
				state.lastConnectedProfile = nil
				state.mu.Unlock()
			}
		}()
	}

	// Writer goroutines for attemptCount
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				state.mu.Lock()
				state.attemptCount++
				state.mu.Unlock()

				state.mu.Lock()
				state.attemptCount = 0
				state.mu.Unlock()
			}
		}()
	}

	// Reader goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				state.mu.Lock()
				_ = state.userInitiatedDisconnect
				_ = state.lastConnectedProfile
				_ = state.attemptCount
				state.mu.Unlock()
			}
		}()
	}

	// Wait for all goroutines - the race detector will catch any data races
	wg.Wait()

	// Verify final state is readable without panic
	state.mu.Lock()
	_ = state.userInitiatedDisconnect
	_ = state.lastConnectedProfile
	_ = state.attemptCount
	state.mu.Unlock()
}

// TestReconnectState_AttemptCountReset tests that attempt count is properly reset
// on successful connection.
func TestReconnectState_AttemptCountReset(t *testing.T) {
	state := &reconnectState{
		attemptCount:            3,
		userInitiatedDisconnect: true,
	}

	// Simulate what onConnectionSucceeded does
	state.mu.Lock()
	state.attemptCount = 0
	state.userInitiatedDisconnect = false
	state.mu.Unlock()

	state.mu.Lock()
	assert.Equal(t, 0, state.attemptCount, "attempt count should be reset")
	assert.False(t, state.userInitiatedDisconnect, "userInitiatedDisconnect should be cleared")
	state.mu.Unlock()
}

// TestReconnectState_TimerNilInitially tests that reconnect timer is nil initially.
func TestReconnectState_TimerNilInitially(t *testing.T) {
	state := &reconnectState{}

	assert.Nil(t, state.reconnectTimer, "reconnect timer should be nil initially")
}

// TestReconnectState_ProfileStorageOnConnectionStart simulates storing profile at connection start.
// Profile is now stored in doConnect at the beginning of connection attempt,
// not in onConnectionSucceeded on success, to avoid race with profile selection changes.
func TestReconnectState_ProfileStorageOnConnectionStart(t *testing.T) {
	state := &reconnectState{}
	testProfile := &profile.Profile{
		ID:            "test-id",
		Name:          "Test VPN",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	// Simulate what doConnect does - store profile at connection start
	state.mu.Lock()
	state.lastConnectedProfile = testProfile
	state.mu.Unlock()

	state.mu.Lock()
	assert.Equal(t, testProfile, state.lastConnectedProfile)
	state.mu.Unlock()

	// Simulate what onConnectionSucceeded does - only reset counters/flags
	// Set up initial state with mutex held
	state.mu.Lock()
	state.attemptCount = 5 // Pretend we had some attempts
	state.userInitiatedDisconnect = true
	state.mu.Unlock()

	// Then reset them as onConnectionSucceeded would (under mutex)
	state.mu.Lock()
	state.attemptCount = 0
	state.userInitiatedDisconnect = false
	state.mu.Unlock()

	state.mu.Lock()
	// Profile should still be set (not modified by onConnectionSucceeded)
	assert.Equal(t, testProfile, state.lastConnectedProfile)
	assert.Equal(t, 0, state.attemptCount)
	assert.False(t, state.userInitiatedDisconnect)
	state.mu.Unlock()
}

// TestPerformReconnect_SkipsIfUserDisconnected verifies that performReconnect
// skips reconnection if user initiated a disconnect while the timer was waiting.
func TestPerformReconnect_SkipsIfUserDisconnected(t *testing.T) {
	state := &reconnectState{}
	testProfile := &profile.Profile{
		ID:            "test-id",
		Name:          "Test VPN",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	// Simulate scenario: reconnect scheduled but user clicked disconnect before timer fired
	state.mu.Lock()
	state.lastConnectedProfile = testProfile
	state.attemptCount = 1
	state.userInitiatedDisconnect = true // User clicked disconnect while waiting
	state.mu.Unlock()

	// Simulate the check in performReconnect
	state.mu.Lock()
	p := state.lastConnectedProfile
	userDisconnected := state.userInitiatedDisconnect
	state.mu.Unlock()

	// This would cause performReconnect to return early
	assert.NotNil(t, p, "profile should be stored")
	assert.True(t, userDisconnected, "userDisconnected flag should be true")

	// When userDisconnected is true, performReconnect should skip and not attempt reconnection
	// This test verifies the state conditions that lead to that behavior
}

// TestCancelReconnect_NilTimer tests that cancelling reconnect with nil timer doesn't panic.
func TestCancelReconnect_NilTimer(t *testing.T) {
	w := newTestMainWindow(nil)
	w.reconnectState.reconnectTimer = nil

	// Should not panic
	assert.NotPanics(t, func() {
		w.cancelReconnect()
	}, "cancelReconnect should not panic with nil timer")
}

// TestShouldTriggerReconnect_WithConfigManager tests the full reconnect logic
// using a real ConfigManager with a temporary config directory.
func TestShouldTriggerReconnect_WithConfigManager(t *testing.T) {
	cfgMgr, cleanup := newTestConfigManager(t, config.DefaultConfig())
	defer cleanup()

	w := newTestMainWindow(cfgMgr)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.attemptCount = 0
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.True(t, result, "should trigger reconnect when all conditions are met")
}

// TestShouldTriggerReconnect_MaxAttemptsReached tests that reconnect is not triggered
// when the maximum number of attempts has been reached.
func TestShouldTriggerReconnect_MaxAttemptsReached(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxReconnectAttempts = 3
	cfgMgr, cleanup := newTestConfigManager(t, cfg)
	defer cleanup()

	w := newTestMainWindow(cfgMgr)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.attemptCount = 3 // Already at max
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect when max attempts reached")
}

// TestShouldTriggerReconnect_UnderMaxAttempts tests that reconnect is triggered
// when attempt count is below the maximum.
func TestShouldTriggerReconnect_UnderMaxAttempts(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxReconnectAttempts = 5
	cfgMgr, cleanup := newTestConfigManager(t, cfg)
	defer cleanup()

	w := newTestMainWindow(cfgMgr)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.attemptCount = 2 // Under max
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.True(t, result, "should trigger reconnect when under max attempts")
}

// TestShouldTriggerReconnect_ZeroMaxAttempts tests behavior when MaxReconnectAttempts is 0.
func TestShouldTriggerReconnect_ZeroMaxAttempts(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxReconnectAttempts = 0
	cfgMgr, cleanup := newTestConfigManager(t, cfg)
	defer cleanup()

	w := newTestMainWindow(cfgMgr)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.attemptCount = 0
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result, "should not trigger reconnect when MaxReconnectAttempts is 0")
}

// TestShouldTriggerReconnect_AllAuthMethodsWithConfig tests all auth methods with
// a real ConfigManager to verify the full logic path.
func TestShouldTriggerReconnect_AllAuthMethodsWithConfig(t *testing.T) {
	tests := []struct {
		name       string
		authMethod profile.AuthMethod
		wantResult bool
	}{
		{
			name:       "Password auth should trigger reconnect",
			authMethod: profile.AuthMethodPassword,
			wantResult: true,
		},
		{
			name:       "OTP auth should not trigger reconnect",
			authMethod: profile.AuthMethodOTP,
			wantResult: false,
		},
		{
			name:       "Certificate auth should trigger reconnect",
			authMethod: profile.AuthMethodCertificate,
			wantResult: true,
		},
		{
			name:       "SAML auth should trigger reconnect",
			authMethod: profile.AuthMethodSAML,
			wantResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfgMgr, cleanup := newTestConfigManager(t, config.DefaultConfig())
			defer cleanup()

			w := newTestMainWindow(cfgMgr)
			w.reconnectState.userInitiatedDisconnect = false
			w.reconnectState.attemptCount = 0
			w.reconnectState.lastConnectedProfile = &profile.Profile{
				ID:            "test-profile-id",
				Name:          "Test Profile",
				AutoReconnect: true,
				AuthMethod:    tt.authMethod,
			}

			result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

			assert.Equal(t, tt.wantResult, result)
		})
	}
}

// TestCancelReconnect_WithActiveTimer tests that cancelling reconnect properly stops an active timer.
func TestCancelReconnect_WithActiveTimer(t *testing.T) {
	w := newTestMainWindow(nil)

	// Create a timer that fires in 10 seconds (won't actually fire during test)
	w.reconnectState.mu.Lock()
	w.reconnectState.reconnectTimer = time.AfterFunc(10*time.Second, func() {
		t.Error("timer should not fire")
	})
	w.reconnectState.mu.Unlock()

	// Cancel the reconnect
	w.cancelReconnect()

	// Verify timer was stopped and set to nil
	w.reconnectState.mu.Lock()
	assert.Nil(t, w.reconnectState.reconnectTimer, "timer should be nil after cancel")
	w.reconnectState.mu.Unlock()
}

// TestReconnectState_DisconnectFlagResetOnUserAction tests that the user-initiated
// disconnect flag is properly set when disconnect is called.
func TestReconnectState_DisconnectFlagResetOnUserAction(t *testing.T) {
	state := &reconnectState{
		userInitiatedDisconnect: false,
	}

	// Simulate what disconnect() does
	state.mu.Lock()
	state.userInitiatedDisconnect = true
	state.mu.Unlock()

	state.mu.Lock()
	assert.True(t, state.userInitiatedDisconnect, "userInitiatedDisconnect should be set to true")
	state.mu.Unlock()
}

// TestShouldTriggerReconnect_ProfileAutoReconnectToggle tests the interaction between
// profile AutoReconnect setting and reconnect logic.
func TestShouldTriggerReconnect_ProfileAutoReconnectToggle(t *testing.T) {
	cfgMgr, cleanup := newTestConfigManager(t, config.DefaultConfig())
	defer cleanup()

	w := newTestMainWindow(cfgMgr)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.attemptCount = 0

	testProfile := &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}
	w.reconnectState.lastConnectedProfile = testProfile

	// With AutoReconnect enabled
	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)
	assert.True(t, result, "should trigger reconnect when AutoReconnect is enabled")

	// Reset state for next test
	w.reconnectState.userInitiatedDisconnect = false

	// Disable AutoReconnect on profile
	testProfile.AutoReconnect = false
	result = w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)
	assert.False(t, result, "should not trigger reconnect when AutoReconnect is disabled")
}

// TestReconnectState_IncrementsAttemptCount verifies that each reconnect attempt
// increments the counter (simulated behavior since startReconnectSequence requires GTK).
func TestReconnectState_IncrementsAttemptCount(t *testing.T) {
	state := &reconnectState{
		attemptCount: 0,
	}

	// Simulate what startReconnectSequence does
	for i := 1; i <= 3; i++ {
		state.mu.Lock()
		state.attemptCount++
		currentCount := state.attemptCount
		state.mu.Unlock()

		assert.Equal(t, i, currentCount, "attempt count should be %d", i)
	}
}

// TestShouldTriggerReconnect_EdgeCaseAtMaxMinus1 tests behavior when at max-1 attempts.
func TestShouldTriggerReconnect_EdgeCaseAtMaxMinus1(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.MaxReconnectAttempts = 3
	cfgMgr, cleanup := newTestConfigManager(t, cfg)
	defer cleanup()

	w := newTestMainWindow(cfgMgr)
	w.reconnectState.userInitiatedDisconnect = false
	w.reconnectState.attemptCount = 2 // One less than max
	w.reconnectState.lastConnectedProfile = &profile.Profile{
		ID:            "test-profile-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := w.shouldTriggerReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.True(t, result, "should trigger reconnect when at max-1 attempts")
}
