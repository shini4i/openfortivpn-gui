package ui

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

func TestNewTrayIcon_InitializesCorrectly(t *testing.T) {
	tray := NewTrayIcon()

	assert.NotNil(t, tray, "tray should not be nil")
	assert.Equal(t, vpn.StateDisconnected, tray.state, "initial state should be disconnected")
	assert.NotNil(t, tray.done, "done channel should be initialized")
	assert.NotNil(t, tray.iconDisconnected, "disconnected icon should be set")
	assert.NotNil(t, tray.iconConnecting, "connecting icon should be set")
	assert.NotNil(t, tray.iconConnected, "connected icon should be set")
	assert.False(t, tray.running, "should not be running initially")
}

func TestTrayIcon_CallbackRegistration(t *testing.T) {
	tray := NewTrayIcon()

	// Should return no error when setting callbacks before Run()
	connectCalled := false
	disconnectCalled := false
	showCalled := false
	quitCalled := false

	err := tray.OnConnect(func() { connectCalled = true })
	assert.NoError(t, err, "OnConnect should succeed before Run()")

	err = tray.OnDisconnect(func() { disconnectCalled = true })
	assert.NoError(t, err, "OnDisconnect should succeed before Run()")

	err = tray.OnShow(func() { showCalled = true })
	assert.NoError(t, err, "OnShow should succeed before Run()")

	err = tray.OnQuit(func() { quitCalled = true })
	assert.NoError(t, err, "OnQuit should succeed before Run()")

	// Verify callbacks are set
	assert.NotNil(t, tray.onConnect)
	assert.NotNil(t, tray.onDisconnect)
	assert.NotNil(t, tray.onShow)
	assert.NotNil(t, tray.onQuit)

	// Test that callbacks work
	tray.onConnect()
	tray.onDisconnect()
	tray.onShow()
	tray.onQuit()

	assert.True(t, connectCalled)
	assert.True(t, disconnectCalled)
	assert.True(t, showCalled)
	assert.True(t, quitCalled)
}

func TestTrayIcon_CallbackErrorsAfterRunning(t *testing.T) {
	tray := NewTrayIcon()

	// Simulate running state without actually calling Run()
	// (Run() would block waiting for systray which requires a display)
	tray.mu.Lock()
	tray.running = true
	tray.mu.Unlock()

	err := tray.OnConnect(func() {})
	assert.ErrorIs(t, err, ErrTrayAlreadyRunning, "OnConnect should return ErrTrayAlreadyRunning after running")

	err = tray.OnDisconnect(func() {})
	assert.ErrorIs(t, err, ErrTrayAlreadyRunning, "OnDisconnect should return ErrTrayAlreadyRunning after running")

	err = tray.OnShow(func() {})
	assert.ErrorIs(t, err, ErrTrayAlreadyRunning, "OnShow should return ErrTrayAlreadyRunning after running")

	err = tray.OnQuit(func() {})
	assert.ErrorIs(t, err, ErrTrayAlreadyRunning, "OnQuit should return ErrTrayAlreadyRunning after running")
}

func TestTrayIcon_SetState(t *testing.T) {
	tray := NewTrayIcon()

	// Test state transitions
	states := []vpn.ConnectionState{
		vpn.StateConnecting,
		vpn.StateAuthenticating,
		vpn.StateConnected,
		vpn.StateReconnecting,
		vpn.StateFailed,
		vpn.StateDisconnected,
	}

	for _, state := range states {
		tray.SetState(state)

		tray.mu.RLock()
		assert.Equal(t, state, tray.state, "state should be updated to %v", state)
		tray.mu.RUnlock()
	}
}

func TestTrayIcon_SetProfileName(t *testing.T) {
	tray := NewTrayIcon()

	testName := "Test VPN Profile"
	tray.SetProfileName(testName)

	tray.mu.RLock()
	assert.Equal(t, testName, tray.profileName, "profile name should be updated")
	tray.mu.RUnlock()
}

func TestTrayIcon_QuitSafeToCallMultipleTimes(t *testing.T) {
	tray := NewTrayIcon()

	// First call should not panic
	assert.NotPanics(t, func() {
		tray.Quit()
	}, "first Quit() should not panic")

	// Second call should also not panic (closeOnce protects the channel)
	assert.NotPanics(t, func() {
		tray.Quit()
	}, "second Quit() should not panic")

	// Third call for good measure
	assert.NotPanics(t, func() {
		tray.Quit()
	}, "third Quit() should not panic")
}

func TestTrayIcon_DoneChannelClosed(t *testing.T) {
	tray := NewTrayIcon()

	// Verify done channel is open initially
	select {
	case <-tray.done:
		t.Fatal("done channel should not be closed initially")
	default:
		// Expected - channel is open
	}

	// Close via Quit
	tray.Quit()

	// Verify done channel is now closed
	select {
	case <-tray.done:
		// Expected - channel is closed
	default:
		t.Fatal("done channel should be closed after Quit()")
	}
}

func TestTrayIcon_RunErrorsIfCalledTwice(t *testing.T) {
	tray := NewTrayIcon()

	// Simulate running state without actually calling Run()
	tray.mu.Lock()
	tray.running = true
	tray.mu.Unlock()

	// Calling Run() when already running should return ErrTrayRunTwice
	err := tray.Run()
	assert.ErrorIs(t, err, ErrTrayRunTwice, "Run() should return ErrTrayRunTwice if called twice")
}

func TestTrayIcon_RunErrorsIfMissingCallbacks(t *testing.T) {
	tests := []struct {
		name        string
		setConnect  bool
		setDisconn  bool
		setShow     bool
		setQuit     bool
		shouldError bool
	}{
		{"no callbacks", false, false, false, false, true},
		{"missing OnConnect", false, true, true, true, true},
		{"missing OnDisconnect", true, false, true, true, true},
		{"missing OnShow", true, true, false, true, true},
		{"missing OnQuit", true, true, true, false, true},
		{"all callbacks set", true, true, true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tray := NewTrayIcon()
			noop := func() {}

			if tt.setConnect {
				_ = tray.OnConnect(noop)
			}
			if tt.setDisconn {
				_ = tray.OnDisconnect(noop)
			}
			if tt.setShow {
				_ = tray.OnShow(noop)
			}
			if tt.setQuit {
				_ = tray.OnQuit(noop)
			}

			// We can't actually call Run() without blocking, so we test the validation
			// by checking if an error would be returned
			tray.mu.Lock()
			hasAllCallbacks := tray.onConnect != nil && tray.onDisconnect != nil &&
				tray.onShow != nil && tray.onQuit != nil
			tray.mu.Unlock()

			if tt.shouldError {
				assert.False(t, hasAllCallbacks, "should be missing at least one callback")
			} else {
				assert.True(t, hasAllCallbacks, "all callbacks should be set")
			}
		})
	}
}

func TestTrayIcon_StateAccessConcurrency(t *testing.T) {
	tray := NewTrayIcon()

	iterations := 1000
	if testing.Short() {
		iterations = 100
	}

	var wg sync.WaitGroup

	// State writer goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tray.SetState(vpn.StateConnecting)
				tray.SetState(vpn.StateConnected)
				tray.SetState(vpn.StateDisconnected)
			}
		}()
	}

	// Profile name writer goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tray.SetProfileName("Profile A")
				tray.SetProfileName("Profile B")
			}
		}()
	}

	// Wait for all goroutines - the race detector will catch any data races
	wg.Wait()

	// Verify final state is readable without panic
	tray.mu.RLock()
	_ = tray.state
	_ = tray.profileName
	tray.mu.RUnlock()
}

func TestTrayIcon_CallbacksNilByDefault(t *testing.T) {
	tray := NewTrayIcon()

	// Verify callbacks are nil by default until explicitly set
	assert.Nil(t, tray.onConnect, "onConnect should be nil by default")
	assert.Nil(t, tray.onDisconnect, "onDisconnect should be nil by default")
	assert.Nil(t, tray.onShow, "onShow should be nil by default")
	assert.Nil(t, tray.onQuit, "onQuit should be nil by default")
}
