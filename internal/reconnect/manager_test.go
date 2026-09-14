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

	// No attempt is in flight, so a transition out of Connecting belongs to a
	// connection the user started.
	assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateDisconnected))
}

// TestManager_ShouldReconnect_FailedTunnelIsNotRetried holds a product
// decision, not a state-machine detail: a live tunnel ending in Failed means
// the helper daemon died, which reconnecting cannot recover — dialling again
// would only strand the GUI on "Reconnecting".
func TestManager_ShouldReconnect_FailedTunnelIsNotRetried(t *testing.T) {
	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		AutoReconnect: true,
	}

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

// TestManager_ShouldReconnect_MaxAttemptsReached covers the limit check on the
// tunnel-drop entry. The count is set directly because no attempt is running on
// this path; the limit reached through a live sequence is covered by
// TestManager_ShouldReconnect_EndsSequenceAtLimit.
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
	assert.Zero(t, m.GetAttemptCount(), "reaching the limit must end the sequence")
}

// TestReconnectDelay_ExponentialBackoffWithJitter verifies the delay grows
// exponentially per attempt, is capped, and stays within the jitter envelope.
//
// Regression test: reconnect previously used a fixed delay for every attempt,
// hammering a flapping gateway at a constant cadence.
func TestReconnectDelay_ExponentialBackoffWithJitter(t *testing.T) {
	base := 5 * time.Second

	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 1, want: 5 * time.Second},
		{attempt: 2, want: 10 * time.Second},
		{attempt: 3, want: 20 * time.Second},
		{attempt: 4, want: 40 * time.Second},
		{attempt: 5, want: maxReconnectDelay},  // 80s capped to 60s
		{attempt: 50, want: maxReconnectDelay}, // huge attempt must not overflow
	}

	for _, tt := range tests {
		got := reconnectDelay(base, tt.attempt)
		lo := time.Duration(float64(tt.want) * (1 - reconnectJitterFraction))
		hi := time.Duration(float64(tt.want) * (1 + reconnectJitterFraction))
		assert.GreaterOrEqual(t, got, lo, "attempt %d below jitter envelope", tt.attempt)
		assert.LessOrEqual(t, got, hi, "attempt %d above jitter envelope", tt.attempt)
	}
}

// TestReconnectDelay_ZeroBase verifies a zero base delay defaults to 1 second
// so that reconnects are never instantaneous and jitter always applies.
func TestReconnectDelay_ZeroBase(t *testing.T) {
	// A zero base defaults to 1s, then follows the normal backoff×jitter path.
	cases := []struct {
		attempt int
		want    time.Duration
	}{
		{1, time.Second},      // 1s * 2^0
		{5, 16 * time.Second}, // 1s * 2^4, still below the 60s cap
	}
	for _, tc := range cases {
		got := reconnectDelay(0, tc.attempt)
		lo := time.Duration(float64(tc.want) * (1 - reconnectJitterFraction))
		hi := time.Duration(float64(tc.want) * (1 + reconnectJitterFraction))
		assert.GreaterOrEqual(t, got, lo, "attempt %d below jitter envelope", tc.attempt)
		assert.LessOrEqual(t, got, hi, "attempt %d above jitter envelope", tc.attempt)
	}
}

func TestManager_StartReconnect(t *testing.T) {
	// scheduleOnMain is required by NewManager, but the timer's firing is not
	// awaited here: doing so would mean a real ≥0.8s sleep. The reconnect
	// timing is covered deterministically by TestReconnectDelay_*; this test
	// only verifies that StartReconnect schedules an attempt synchronously.
	m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 1}, func(func()) {})
	m.lastConnectedProfile = &profile.Profile{
		ID:   "test-id",
		Name: "Test Profile",
	}

	m.StartReconnect()
	defer m.Cancel() // stop the pending timer so it doesn't fire after the test

	// Read both fields under m.mu: StartReconnect mutates them under the lock,
	// and the pending timer goroutine may touch attemptCount when it fires.
	m.mu.Lock()
	assert.Equal(t, 1, m.attemptCount)
	assert.NotNil(t, m.reconnectTimer, "StartReconnect should schedule a timer")
	m.mu.Unlock()
}

func TestManager_Cancel(t *testing.T) {
	cfg := Config{MaxAttempts: 3, DelaySeconds: 10}
	m := NewManager(cfg, nil)
	m.lastConnectedProfile = &profile.Profile{Name: "Test"}

	m.StartReconnect()
	assert.True(t, timerArmed(m))

	m.Cancel()
	assert.False(t, timerArmed(m))
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

// TestManager_PerformReconnect_Certificate_NoPassword asserts a certificate
// profile reconnects without a keyring password. Certificate auth proves
// identity with the key pair, so demanding a stored password would abort every
// auto-reconnect for these profiles.
func TestManager_PerformReconnect_Certificate_NoPassword(t *testing.T) {
	var connectedPassword string
	var failed error
	done := make(chan struct{})

	m := NewManager(DefaultConfig(), nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:         "cert-id",
		Name:       "Certificate Profile",
		AuthMethod: profile.AuthMethodCertificate,
	}
	// Deliberately no password provider: a certificate profile has no stored
	// password, so needing one would be the bug.
	m.callbacks = Callbacks{OnFailed: func(err error) { failed = err }}
	m.connectFunc = func(ctx context.Context, p *profile.Profile, password string) error {
		connectedPassword = password
		close(done)
		return nil
	}

	m.performReconnect()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("connect was not called for a certificate profile")
	}

	assert.Equal(t, "", connectedPassword)
	assert.NoError(t, failed, "certificate reconnect must not report a password failure")
}

// TestManager_ShouldReconnect_ReArmsAfterFailedAttempt is the defect this suite
// missed: a reconnect attempt ends Connecting->Failed, never
// Connected->Disconnected, so without a second trigger the sequence stops after
// one try and MaxAttempts is unreachable.
func TestManager_ShouldReconnect_ReArmsAfterFailedAttempt(t *testing.T) {
	// newRetrying returns a manager whose first attempt is under way, as it is
	// once StartReconnect's timer has fired.
	newRetrying := func(maxAttempts int) *Manager {
		m := NewManager(Config{MaxAttempts: maxAttempts, DelaySeconds: 10}, nil)
		m.lastConnectedProfile = &profile.Profile{
			Name:          "Test",
			AuthMethod:    profile.AuthMethodCertificate,
			AutoReconnect: true,
		}
		m.SetConnectFunc(func(context.Context, *profile.Profile, string) error { return nil })

		require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected),
			"the original drop must arm the first attempt")
		m.StartReconnect()
		m.performReconnect()
		return m
	}

	for _, terminal := range []vpn.ConnectionState{vpn.StateDisconnected, vpn.StateFailed} {
		t.Run("a failed attempt re-arms, ending in "+string(terminal), func(t *testing.T) {
			m := newRetrying(3)
			defer m.Cancel()

			assert.True(t, m.ShouldReconnect(vpn.StateConnecting, terminal),
				"a reconnect attempt that failed must arm the next one")
		})
	}

	t.Run("stops at the configured limit", func(t *testing.T) {
		m := newRetrying(3)
		defer m.Cancel()

		for i := 2; i <= 3; i++ {
			require.True(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed),
				"attempt %d of 3 must be armed", i)
			m.StartReconnect()
			m.performReconnect()
			require.Equal(t, i, m.GetAttemptCount())
		}

		assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed),
			"the sequence must stop once MaxAttempts is reached")
		assert.Zero(t, m.GetAttemptCount(), "a finished sequence must not leak into the next one")
	})

	t.Run("a connect the user started is not a reconnect", func(t *testing.T) {
		m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 10}, nil)
		m.lastConnectedProfile = &profile.Profile{
			Name:          "Test",
			AuthMethod:    profile.AuthMethodCertificate,
			AutoReconnect: true,
		}

		assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed),
			"no attempt is in flight, so a failed manual connect must not arm one")
	})

	t.Run("the state change landing first still counts once", func(t *testing.T) {
		m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 10}, nil)
		m.lastConnectedProfile = &profile.Profile{
			Name:          "Test",
			AuthMethod:    profile.AuthMethodCertificate,
			AutoReconnect: true,
		}
		// The controller fails the attempt and only then returns the error, so
		// the re-arm happens while Connect is still on the stack.
		m.SetConnectFunc(func(context.Context, *profile.Profile, string) error {
			if m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed) {
				m.StartReconnect()
			}
			return errors.New("pkexec dismissed")
		})

		require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
		m.StartReconnect()
		m.performReconnect()
		defer m.Cancel()

		assert.Equal(t, 2, m.GetAttemptCount(),
			"the returned error must be ignored once the state change has re-armed")
	})

	t.Run("one failure reported twice counts once", func(t *testing.T) {
		m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 10}, nil)
		m.lastConnectedProfile = &profile.Profile{
			Name:          "Test",
			AuthMethod:    profile.AuthMethodCertificate,
			AutoReconnect: true,
		}
		// Connect both moves the controller to Failed and returns the error.
		m.SetConnectFunc(func(context.Context, *profile.Profile, string) error {
			return errors.New("pkexec dismissed")
		})

		require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
		m.StartReconnect()
		m.performReconnect() // re-arms on the returned error
		defer m.Cancel()

		require.Equal(t, 2, m.GetAttemptCount())
		assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed),
			"the state change for a failure already counted must not arm again")
		assert.Equal(t, 2, m.GetAttemptCount())
	})
}

// TestManager_ShouldReconnect_EndsSequenceAtLimit checks the counter is cleared
// when the retries stop. The state change being handled reports the failure to
// the user, so no callback fires here.
func TestManager_ShouldReconnect_EndsSequenceAtLimit(t *testing.T) {
	m := NewManager(Config{MaxAttempts: 1, DelaySeconds: 10}, nil)
	m.lastConnectedProfile = &profile.Profile{
		Name:          "Test",
		AuthMethod:    profile.AuthMethodCertificate,
		AutoReconnect: true,
	}
	m.SetConnectFunc(func(context.Context, *profile.Profile, string) error { return nil })

	failed := make(chan error, 1)
	m.SetCallbacks(Callbacks{OnFailed: func(err error) { failed <- err }})

	require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
	m.StartReconnect()
	m.performReconnect()

	assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed))
	assert.Zero(t, m.GetAttemptCount(), "a finished sequence must not leak into the next one")
	assert.Empty(t, failed, "the terminal state change already reports this failure")
}

// TestManager_Cancel_ResetsAttempts stops a stale sequence being inherited by
// the next connection the user starts by hand.
func TestManager_Cancel_ResetsAttempts(t *testing.T) {
	m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 10}, nil)
	m.lastConnectedProfile = &profile.Profile{Name: "Test"}

	m.StartReconnect()
	require.Equal(t, 1, m.GetAttemptCount())

	m.Cancel()

	assert.Zero(t, m.GetAttemptCount())
}

// TestManager_PerformReconnect_SyncFailureReArms covers a connect that never
// starts — pkexec refused, say. It raises no state change, so nothing else can
// notice the attempt died.
func TestManager_PerformReconnect_SyncFailureReArms(t *testing.T) {
	m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 10}, nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:            "test-id",
		Name:          "Test",
		AuthMethod:    profile.AuthMethodCertificate,
		AutoReconnect: true,
	}
	m.SetConnectFunc(func(context.Context, *profile.Profile, string) error {
		return errors.New("pkexec dismissed")
	})

	m.attemptCount = 1
	m.performReconnect()

	assert.Equal(t, 2, m.GetAttemptCount(), "a connect that never started must arm the next attempt")
	assert.True(t, timerArmed(m))
	m.Cancel()
}

// TestManager_PerformReconnect_SyncFailureGivesUpAtLimit is the same path at
// the end of the sequence.
func TestManager_PerformReconnect_SyncFailureGivesUpAtLimit(t *testing.T) {
	m := NewManager(Config{MaxAttempts: 2, DelaySeconds: 10}, nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:            "test-id",
		Name:          "Test",
		AuthMethod:    profile.AuthMethodCertificate,
		AutoReconnect: true,
	}
	m.SetConnectFunc(func(context.Context, *profile.Profile, string) error {
		return errors.New("pkexec dismissed")
	})

	failed := make(chan error, 1)
	m.SetCallbacks(Callbacks{OnFailed: func(err error) { failed <- err }})

	m.attemptCount = 2
	m.performReconnect()

	select {
	case err := <-failed:
		assert.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("the last attempt failing must report the failure")
	}

	assert.Zero(t, m.GetAttemptCount())
}

// TestManager_RetryOrGiveUp_IgnoresEndedSequence covers a sequence that ends
// between an attempt claiming its slot and that attempt's error coming back —
// a Cancel from a connection the user started, say. The late error must not
// restart what was just stopped.
func TestManager_RetryOrGiveUp_IgnoresEndedSequence(t *testing.T) {
	m := NewManager(Config{MaxAttempts: 3, DelaySeconds: 10}, nil)
	m.lastConnectedProfile = &profile.Profile{
		Name:          "Test",
		AuthMethod:    profile.AuthMethodCertificate,
		AutoReconnect: true,
	}

	failed := make(chan error, 1)
	m.SetCallbacks(Callbacks{OnFailed: func(err error) { failed <- err }})

	m.Cancel() // the sequence is over: attemptCount and running are both zero

	m.retryOrGiveUp(errors.New("pkexec dismissed"))

	assert.Zero(t, m.GetAttemptCount(), "an ended sequence must not be restarted")
	assert.False(t, timerArmed(m), "nor armed with a fresh timer")
	assert.Empty(t, failed, "nor reported as a fresh failure")
}

// timerArmed reports whether a reconnect is pending, taking the lock the timer
// callback also uses.
func timerArmed(m *Manager) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reconnectTimer != nil
}

// startedSequence returns a manager one attempt into a live sequence, as it is
// once StartReconnect's timer has fired.
func startedSequence(t *testing.T, cfg Config, connect ConnectFunc) *Manager {
	t.Helper()

	m := NewManager(cfg, nil)
	m.lastConnectedProfile = &profile.Profile{
		ID:            "test-id",
		Name:          "Test",
		AuthMethod:    profile.AuthMethodPassword,
		AutoReconnect: true,
	}
	m.SetPasswordProvider(&mockPasswordProvider{passwords: map[string]string{"test-id": "pw"}})
	m.SetConnectFunc(connect)

	require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
	m.StartReconnect()
	return m
}

// TestManager_PerformReconnect_PasswordFailureEndsSequence covers a keyring that
// stops answering mid-sequence. The attempt is abandoned before it starts, so
// the sequence has to end rather than leave a claimed attempt behind that a
// later unrelated failure could re-arm.
func TestManager_PerformReconnect_PasswordFailureEndsSequence(t *testing.T) {
	tests := []struct {
		name     string
		provider PasswordProvider
	}{
		{"no provider configured", nil},
		{"provider fails", &mockPasswordProvider{err: errors.New("keyring locked")}},
		{"stored password is empty", &mockPasswordProvider{passwords: map[string]string{"test-id": ""}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := startedSequence(t, Config{MaxAttempts: 3, DelaySeconds: 10},
				func(context.Context, *profile.Profile, string) error { return nil })
			m.SetPasswordProvider(tt.provider)

			failed := make(chan error, 1)
			m.SetCallbacks(Callbacks{OnFailed: func(err error) { failed <- err }})

			m.performReconnect()

			select {
			case err := <-failed:
				assert.Error(t, err)
			case <-time.After(time.Second):
				t.Fatal("an unusable password must be reported")
			}

			assert.Zero(t, m.GetAttemptCount(), "the sequence must end, not stall mid-count")
			assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed),
				"no phantom attempt may survive to be re-armed")
		})
	}
}

// TestManager_PerformReconnect_AbandonedAttemptEndsSequence covers the attempt
// that is dropped before it starts because the user disconnected while the
// timer was running.
func TestManager_PerformReconnect_AbandonedAttemptEndsSequence(t *testing.T) {
	m := startedSequence(t, Config{MaxAttempts: 3, DelaySeconds: 10},
		func(context.Context, *profile.Profile, string) error {
			t.Fatal("a user disconnect must not reach the connect function")
			return nil
		})
	m.SetUserDisconnect()

	m.performReconnect()

	assert.Zero(t, m.GetAttemptCount())
	assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed))
}

// TestManager_OnConnectionSucceeded_RestoresBudget holds the user-visible
// promise of MaxAttempts: a reconnect that succeeds on a later attempt leaves
// the next drop a full budget, not the remainder of the last one.
func TestManager_OnConnectionSucceeded_RestoresBudget(t *testing.T) {
	m := startedSequence(t, Config{MaxAttempts: 3, DelaySeconds: 10},
		func(context.Context, *profile.Profile, string) error { return nil })
	defer m.Cancel()

	m.performReconnect()
	require.True(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed))
	m.StartReconnect()
	require.Equal(t, 2, m.GetAttemptCount())

	m.OnConnectionSucceeded()
	assert.Zero(t, m.GetAttemptCount())

	// A later drop starts counting from one again.
	require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
	m.StartReconnect()
	assert.Equal(t, 1, m.GetAttemptCount(), "a successful reconnect must restore the whole budget")
}

// TestManager_StartReconnect_DispatchesThroughScheduleOnMain closes the retry
// loop the way production does. The callback must not run the connect on the
// timer goroutine: the UI work it triggers is not thread-safe.
func TestManager_StartReconnect_DispatchesThroughScheduleOnMain(t *testing.T) {
	connected := make(chan struct{})
	scheduled := make(chan struct{}, 1)

	m := NewManager(Config{MaxAttempts: 1, DelaySeconds: 1}, func(fn func()) {
		select {
		case scheduled <- struct{}{}:
		default:
		}
		fn()
	})
	m.lastConnectedProfile = &profile.Profile{
		ID:            "test-id",
		Name:          "Test",
		AuthMethod:    profile.AuthMethodCertificate,
		AutoReconnect: true,
	}
	m.SetConnectFunc(func(context.Context, *profile.Profile, string) error {
		close(connected)
		return nil
	})

	m.StartReconnect()

	select {
	case <-connected:
	case <-time.After(3 * time.Second):
		t.Fatal("the armed timer never performed the reconnect")
	}

	assert.Len(t, scheduled, 1, "the attempt must be marshalled onto the main thread")
}

// TestManager_EndedSequence_LeavesNoPhantomAttempt covers the claim outliving
// the sequence that made it. A stale attempt number can coincide with the next
// sequence's count, arming a reconnect for an attempt that never ran.
func TestManager_EndedSequence_LeavesNoPhantomAttempt(t *testing.T) {
	m := startedSequence(t, Config{MaxAttempts: 3, DelaySeconds: 10},
		func(context.Context, *profile.Profile, string) error { return nil })
	m.performReconnect() // attempt 1 is claimed and under way

	m.Cancel() // the user starts their own connection, ending the sequence

	// That connection drops, opening a fresh sequence whose first attempt has
	// been armed but not yet performed.
	require.True(t, m.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
	m.StartReconnect()
	defer m.Cancel()
	require.Equal(t, 1, m.GetAttemptCount())

	assert.False(t, m.ShouldReconnect(vpn.StateConnecting, vpn.StateFailed),
		"no attempt has been performed in this sequence, so nothing may re-arm")
}
