package reconnect

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPasswordProvider implements PasswordProvider for testing.
type mockPasswordProvider struct {
	passwords map[string]string
	err       error
}

func (m *mockPasswordProvider) Get(profileID string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	pw, ok := m.passwords[profileID]
	if !ok {
		return "", errors.New("password not found")
	}
	return pw, nil
}

func TestNewManager(t *testing.T) {
	cfg := DefaultConfig()
	m := NewManager(cfg, nil)

	assert.NotNil(t, m)
	assert.Equal(t, cfg.MaxAttempts, m.config.MaxAttempts)
	assert.Equal(t, cfg.DelaySeconds, m.config.DelaySeconds)
}

func TestManager_OnConnectionSucceeded(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.attemptCount = 5
	m.userInitiatedDisconnect = true

	m.OnConnectionSucceeded()

	assert.Equal(t, 0, m.attemptCount)
	assert.False(t, m.userInitiatedDisconnect)
}

func TestManager_SetUserDisconnect(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)

	m.SetUserDisconnect()

	assert.True(t, m.userInitiatedDisconnect)
}

func TestManager_StoreConnectedProfile(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)

	p := &profile.Profile{
		ID:   "test-id",
		Name: "Test Profile",
	}

	m.StoreConnectedProfile(p)

	assert.NotNil(t, m.lastConnectedProfile)
	assert.Equal(t, "test-id", m.lastConnectedProfile.ID)

	// Verify it's a copy, not the same pointer
	assert.NotSame(t, p, m.lastConnectedProfile)
}

func TestManager_StoreConnectedProfile_Nil(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)

	m.StoreConnectedProfile(nil)

	assert.Nil(t, m.lastConnectedProfile)
}

func TestManager_ShouldReconnect_Success(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:            "test-id",
		Name:          "Test Profile",
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}

	result := m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.True(t, result)
}

func TestManager_ShouldReconnect_WrongStateTransition(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		AutoReconnect: true,
	}

	// Not from Connected state
	assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateDisconnected))

	// Not to Disconnected state
	assert.False(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateFailed))
}

func TestManager_ShouldReconnect_UserInitiated(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		AutoReconnect: true,
	}
	m.userInitiatedDisconnect = true

	result := m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result)
	// Flag should be reset
	assert.False(t, m.userInitiatedDisconnect)
}

func TestManager_ShouldReconnect_NoProfile(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)

	result := m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result)
}

func TestManager_ShouldReconnect_AutoReconnectDisabled(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		AutoReconnect: false,
	}

	result := m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result)
}

func TestManager_ShouldReconnect_OTPAuth(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodOTP,
	}

	result := m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result)
}

func TestManager_ShouldReconnect_MaxAttemptsReached(t *testing.T) {
	cfg := Config{MaxAttempts: 3, DelaySeconds: 1}
	m := NewManager(cfg, nil)
	m.lastConnectedProfile = &profile.Profile{
		AutoReconnect: true,
		AuthMethod:    profile.AuthMethodPassword,
	}
	m.attemptCount = 3

	result := m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected)

	assert.False(t, result)
}

func TestManager_StartReconnect(t *testing.T) {
	scheduled := make(chan struct{})
	scheduleOnMain := func(fn func()) {
		close(scheduled)
	}

	cfg := Config{MaxAttempts: 3, DelaySeconds: 0} // 0 delay for testing
	m := NewManager(cfg, scheduleOnMain)
	m.lastConnectedProfile = &profile.Profile{
		ID:   "test-id",
		Name: "Test Profile",
	}

	m.StartReconnect()

	assert.Equal(t, 1, m.attemptCount)

	// Wait for timer to fire
	select {
	case <-scheduled:
		// Success - scheduleOnMain was called
	case <-time.After(100 * time.Millisecond):
		t.Fatal("scheduleOnMain was not called within timeout")
	}
}

func TestManager_Cancel(t *testing.T) {
	cfg := Config{MaxAttempts: 3, DelaySeconds: 10}
	m := NewManager(cfg, nil)
	m.lastConnectedProfile = &profile.Profile{Name: "Test"}

	m.StartReconnect()
	assert.NotNil(t, m.reconnectTimer)

	m.Cancel()
	assert.Nil(t, m.reconnectTimer)
}

func TestManager_PerformReconnect_Success(t *testing.T) {
	var connectedProfile *profile.Profile
	var connectedPassword string
	done := make(chan struct{})

	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:         "test-id",
		Name:       "Test Profile",
		AuthMethod: profile.AuthMethodPassword,
	}
	m.passwordProvider = &mockPasswordProvider{
		passwords: map[string]string{"test-id": "secret"},
	}
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		connectedProfile = p
		connectedPassword = password
		close(done)
		return nil
	}
	m.ctx = context.Background()

	m.performReconnect()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("connect was not called within timeout")
	}

	require.NotNil(t, connectedProfile)
	assert.Equal(t, "test-id", connectedProfile.ID)
	assert.Equal(t, "secret", connectedPassword)
}

func TestManager_PerformReconnect_SAML_NoPassword(t *testing.T) {
	var connectedPassword string
	done := make(chan struct{})

	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:         "test-id",
		Name:       "SAML Profile",
		AuthMethod: profile.AuthMethodSAML,
	}
	// No password provider needed for SAML
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		connectedPassword = password
		close(done)
		return nil
	}
	m.ctx = context.Background()

	m.performReconnect()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("connect was not called within timeout")
	}

	assert.Equal(t, "", connectedPassword)
}

func TestManager_PerformReconnect_NoProfile(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		t.Error("Connect should not be called")
		return nil
	}

	m.performReconnect()
	// Should return early without calling connect
}

func TestManager_PerformReconnect_UserDisconnected(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{ID: "test"}
	m.userInitiatedDisconnect = true
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		t.Error("Connect should not be called")
		return nil
	}

	m.performReconnect()
	// Should return early without calling connect
}

func TestManager_PerformReconnect_NoPasswordProvider(t *testing.T) {
	var failedCalled bool

	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:         "test-id",
		AuthMethod: profile.AuthMethodPassword,
	}
	m.callbacks = Callbacks{
		OnFailed: func(err error) {
			failedCalled = true
		},
	}
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		t.Error("Connect should not be called")
		return nil
	}

	m.performReconnect()

	assert.True(t, failedCalled)
}

func TestManager_PerformReconnect_PasswordError(t *testing.T) {
	var failedCalled bool

	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:         "test-id",
		AuthMethod: profile.AuthMethodPassword,
	}
	m.passwordProvider = &mockPasswordProvider{
		err: errors.New("keyring error"),
	}
	m.callbacks = Callbacks{
		OnFailed: func(err error) {
			failedCalled = true
		},
	}
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		t.Error("Connect should not be called")
		return nil
	}

	m.performReconnect()

	assert.True(t, failedCalled)
}

func TestManager_GetAttemptCount(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.attemptCount = 5

	assert.Equal(t, 5, m.GetAttemptCount())
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 3, cfg.MaxAttempts)
	assert.Equal(t, 5, cfg.DelaySeconds)
}
