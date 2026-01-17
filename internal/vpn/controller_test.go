package vpn

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewController(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	require.NotNil(t, ctrl)
	assert.Equal(t, StateDisconnected, ctrl.GetState())
	assert.Equal(t, "/usr/bin/openfortivpn", ctrl.openfortivpnPath)
}

func TestController_GetState(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	assert.Equal(t, StateDisconnected, ctrl.GetState())
}

func TestController_SetState(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	// Valid transition: Disconnected -> Connecting
	err := ctrl.setState(StateConnecting)
	require.NoError(t, err)
	assert.Equal(t, StateConnecting, ctrl.GetState())

	// Valid transition: Connecting -> Authenticating (SAML auth prompt)
	err = ctrl.setState(StateAuthenticating)
	require.NoError(t, err)
	assert.Equal(t, StateAuthenticating, ctrl.GetState())

	// Invalid transition: Authenticating -> Reconnecting
	err = ctrl.setState(StateReconnecting)
	require.Error(t, err)
	assert.Equal(t, StateAuthenticating, ctrl.GetState()) // State unchanged
}

func TestController_OnStateChange(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	var receivedOld, receivedNew ConnectionState
	var callCount int
	var mu sync.Mutex

	ctrl.OnStateChange(func(old, new ConnectionState) {
		mu.Lock()
		defer mu.Unlock()
		receivedOld = old
		receivedNew = new
		callCount++
	})

	err := ctrl.setState(StateConnecting)
	require.NoError(t, err)

	mu.Lock()
	assert.Equal(t, StateDisconnected, receivedOld)
	assert.Equal(t, StateConnecting, receivedNew)
	assert.Equal(t, 1, callCount)
	mu.Unlock()
}

func TestController_OnOutput(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	var receivedLine string
	var mu sync.Mutex

	ctrl.OnOutput(func(line string) {
		mu.Lock()
		defer mu.Unlock()
		receivedLine = line
	})

	ctrl.emitOutput("Test output line")

	mu.Lock()
	assert.Equal(t, "Test output line", receivedLine)
	mu.Unlock()
}

func TestController_OnEvent(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	var receivedEvent *OutputEvent
	var mu sync.Mutex

	ctrl.OnEvent(func(event *OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		receivedEvent = event
	})

	event := &OutputEvent{
		Type:    EventConnected,
		Message: "Tunnel is up",
	}
	ctrl.emitEvent(event)

	mu.Lock()
	require.NotNil(t, receivedEvent)
	assert.Equal(t, EventConnected, receivedEvent.Type)
	mu.Unlock()
}

func TestController_CanConnect(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	// Should be able to connect from disconnected state
	assert.True(t, ctrl.CanConnect())

	// Transition to connecting
	_ = ctrl.setState(StateConnecting)
	assert.False(t, ctrl.CanConnect())

	// Transition to connected
	_ = ctrl.setState(StateConnected)
	assert.False(t, ctrl.CanConnect())

	// Transition to disconnected
	_ = ctrl.setState(StateDisconnected)
	assert.True(t, ctrl.CanConnect())
}

func TestController_CanDisconnect(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	// Cannot disconnect from disconnected state
	assert.False(t, ctrl.CanDisconnect())

	// Transition to connecting
	_ = ctrl.setState(StateConnecting)
	assert.True(t, ctrl.CanDisconnect())

	// Transition to connected
	_ = ctrl.setState(StateConnected)
	assert.True(t, ctrl.CanDisconnect())
}

func TestController_GetAssignedIP(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	// Initially empty
	assert.Empty(t, ctrl.GetAssignedIP())

	// Set IP
	ctrl.setAssignedIP("10.0.0.100")
	assert.Equal(t, "10.0.0.100", ctrl.GetAssignedIP())

	// Clear IP
	ctrl.setAssignedIP("")
	assert.Empty(t, ctrl.GetAssignedIP())
}

func TestController_ProcessOutput(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")
	_ = ctrl.setState(StateConnecting)

	var events []*OutputEvent
	var mu sync.Mutex

	ctrl.OnEvent(func(event *OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	// Process tunnel up message
	ctrl.processOutput("Tunnel is up and running.")

	mu.Lock()
	require.Len(t, events, 1)
	assert.Equal(t, EventConnected, events[0].Type)
	mu.Unlock()

	assert.Equal(t, StateConnected, ctrl.GetState())
}

func TestController_ProcessOutput_GotIP(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")
	_ = ctrl.setState(StateConnecting)

	ctrl.processOutput("Got addresses: [10.0.0.50], ns [10.0.0.1]")

	assert.Equal(t, "10.0.0.50", ctrl.GetAssignedIP())
}

func TestController_ProcessOutput_Error(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")
	_ = ctrl.setState(StateConnecting)

	var lastError string
	var mu sync.Mutex

	ctrl.OnError(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		lastError = err.Error()
	})

	ctrl.processOutput("ERROR:  VPN authentication failed.")

	mu.Lock()
	assert.Contains(t, lastError, "VPN authentication failed")
	mu.Unlock()

	assert.Equal(t, StateFailed, ctrl.GetState())
}

func TestController_BuildCommand(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
		Realm:      "testrealm",
	}

	args := ctrl.buildCommandArgs(p, nil)

	assert.Contains(t, args, "vpn.example.com:443")
	assert.Contains(t, args, "-u")
	assert.Contains(t, args, "testuser")
	assert.Contains(t, args, "--realm=testrealm")
	assert.Contains(t, args, "--set-dns=1")
	assert.Contains(t, args, "--set-routes=1")
}

func TestController_BuildCommand_NoDNS(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     false,
		SetRoutes:  false,
	}

	args := ctrl.buildCommandArgs(p, nil)

	assert.Contains(t, args, "--set-dns=0")
	assert.Contains(t, args, "--set-routes=0")
	assert.Contains(t, args, "--half-internet-routes=0")
}

func TestController_BuildCommand_HalfInternetRoutes(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:                 "550e8400-e29b-41d4-a716-446655440000",
		Name:               "Test VPN",
		Host:               "vpn.example.com",
		Port:               443,
		Username:           "testuser",
		AuthMethod:         profile.AuthMethodPassword,
		SetDNS:             true,
		SetRoutes:          true,
		HalfInternetRoutes: true,
	}

	args := ctrl.buildCommandArgs(p, nil)

	assert.Contains(t, args, "--set-routes=1")
	assert.Contains(t, args, "--half-internet-routes=1")
}

func TestController_BuildCommand_Certificate(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:             "550e8400-e29b-41d4-a716-446655440000",
		Name:           "Test VPN",
		Host:           "vpn.example.com",
		Port:           443,
		AuthMethod:     profile.AuthMethodCertificate,
		ClientCertPath: "/path/to/cert.pem",
		ClientKeyPath:  "/path/to/key.pem",
		SetDNS:         true,
		SetRoutes:      true,
	}

	args := ctrl.buildCommandArgs(p, nil)

	assert.Contains(t, args, "--user-cert=/path/to/cert.pem")
	assert.Contains(t, args, "--user-key=/path/to/key.pem")
}

func TestController_BuildCommand_TrustedCert(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:          "550e8400-e29b-41d4-a716-446655440000",
		Name:        "Test VPN",
		Host:        "vpn.example.com",
		Port:        443,
		Username:    "testuser",
		AuthMethod:  profile.AuthMethodPassword,
		TrustedCert: "abc123def456",
		SetDNS:      true,
		SetRoutes:   true,
	}

	args := ctrl.buildCommandArgs(p, nil)

	assert.Contains(t, args, "--trusted-cert=abc123def456")
}

func TestController_BuildCommand_SAML(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		AuthMethod: profile.AuthMethodSAML,
		SetDNS:     true,
		SetRoutes:  true,
	}

	args := ctrl.buildCommandArgs(p, nil)

	// SAML auth should include --saml-login flag
	assert.Contains(t, args, "--saml-login")
	// SAML auth should NOT include username
	assert.NotContains(t, args, "-u")
}

func TestController_BuildCommandArgs_OTP(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		AuthMethod: profile.AuthMethodOTP,
		Username:   "testuser",
		SetDNS:     true,
		SetRoutes:  true,
	}

	// Test with OTP provided
	opts := &ConnectOptions{
		Password: "password",
		OTP:      "123456",
	}
	args := ctrl.buildCommandArgs(p, opts)

	// OTP auth should include username
	assert.Contains(t, args, "-u")
	assert.Contains(t, args, "testuser")

	// OTP should be passed via --otp flag
	assert.Contains(t, args, "--otp=123456")
}

func TestController_BuildCommandArgs_OTP_Empty(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		AuthMethod: profile.AuthMethodOTP,
		Username:   "testuser",
		SetDNS:     true,
		SetRoutes:  true,
	}

	// Test with no OTP (nil opts)
	args := ctrl.buildCommandArgs(p, nil)

	// Should NOT include --otp flag when OTP is empty
	for _, arg := range args {
		assert.NotContains(t, arg, "--otp=")
	}

	// Test with empty OTP string
	opts := &ConnectOptions{Password: "password", OTP: ""}
	args = ctrl.buildCommandArgs(p, opts)

	// Should still NOT include --otp flag
	for _, arg := range args {
		assert.NotContains(t, arg, "--otp=")
	}
}

func TestController_Connect_SAML_NoPasswordWritten(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		AuthMethod: profile.AuthMethodSAML,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{})
	require.NoError(t, err)

	// Give time for any async writes (should not happen for SAML)
	time.Sleep(50 * time.Millisecond)

	// SAML auth should NOT write anything to stdin
	assert.Empty(t, executor.GetProcess().GetStdinContent(), "SAML auth should not write password to stdin")

	// Verify --saml-login flag is in the command args
	args := executor.GetLastArgs()
	assert.Contains(t, args, "--saml-login")

	// Cleanup
	executor.GetProcess().CompleteProcess()
}

func TestController_Connect_SAML_PasswordIgnored(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		AuthMethod: profile.AuthMethodSAML,
		SetDNS:     true,
		SetRoutes:  true,
	}

	// Pass a non-empty password - should still be ignored for SAML
	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "should-be-ignored"})
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// SAML auth should NOT write anything to stdin even if password provided
	assert.Empty(t, executor.GetProcess().GetStdinContent())

	executor.GetProcess().CompleteProcess()
}

func TestController_ProcessOutput_Authenticate(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")
	_ = ctrl.setState(StateConnecting)

	var events []*OutputEvent
	var mu sync.Mutex

	ctrl.OnEvent(func(event *OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	ctrl.processOutput("Authenticate at 'https://sso.example.com/auth?session=abc'")

	mu.Lock()
	require.Len(t, events, 1)
	assert.Equal(t, EventAuthenticate, events[0].Type)
	assert.Equal(t, "https://sso.example.com/auth?session=abc", events[0].GetData("url"))
	mu.Unlock()

	assert.Equal(t, StateAuthenticating, ctrl.GetState())
}

func TestController_Disconnect_NotConnected(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	err := ctrl.Disconnect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestController_Connect_AlreadyConnecting(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")
	_ = ctrl.setState(StateConnecting)

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot connect")
}

func TestController_Connect_InvalidProfile(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	// Profile with invalid hostname (contains shell metacharacter)
	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com; rm -rf /",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile")
	// State should remain disconnected
	assert.Equal(t, StateDisconnected, ctrl.GetState())
}

func TestController_ConcurrentStateAccess(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Multiple concurrent state reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = ctrl.GetState()
			_ = ctrl.GetAssignedIP()
			_ = ctrl.CanConnect()
			_ = ctrl.CanDisconnect()
		}()
	}

	wg.Wait()
}

func TestController_StateTransitionOnDisconnect(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	// Simulate being in connected state
	_ = ctrl.setState(StateConnecting)
	_ = ctrl.setState(StateConnected)
	ctrl.setAssignedIP("10.0.0.1")

	// Track state change
	var newState ConnectionState
	var mu sync.Mutex
	ctrl.OnStateChange(func(old, new ConnectionState) {
		mu.Lock()
		defer mu.Unlock()
		newState = new
	})

	// Process disconnect event
	ctrl.processOutput("Tunnel is down.")

	// Verify state transition (synchronous, no sleep needed)
	mu.Lock()
	assert.Equal(t, StateDisconnected, newState)
	mu.Unlock()
	assert.Empty(t, ctrl.GetAssignedIP())
}

// Tests using mock executor for full Connect/Disconnect coverage

func TestController_Connect_Success(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// Pre-populate stdout with expected output
	process := executor.GetProcess()
	process.WriteToStdout("Connecting to gateway...")
	process.WriteToStdout("Got addresses: [10.0.0.100], ns [10.0.0.1]")
	process.WriteToStdout("Tunnel is up and running.")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	// Track events
	var events []*OutputEvent
	var mu sync.Mutex
	ctrl.OnEvent(func(event *OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, event)
	})

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "secretpassword"})
	require.NoError(t, err)

	// Verify process was started
	assert.True(t, process.IsStarted())

	// Verify command was constructed correctly with pkexec
	assert.Equal(t, "pkexec", executor.GetLastName())
	args := executor.GetLastArgs()
	assert.Contains(t, args, "/usr/bin/openfortivpn") // First arg is the actual command
	assert.Contains(t, args, "vpn.example.com:443")
	assert.Contains(t, args, "-u")
	assert.Contains(t, args, "testuser")

	// Complete the process
	process.CompleteProcess()

	// Give goroutines time to process
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateDisconnected
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestController_Connect_ProcessCreateError(t *testing.T) {
	executor := NewMockExecutor()
	executor.SetCreateError(errors.New("failed to create process"))
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create process")
	assert.Equal(t, StateFailed, ctrl.GetState())
}

func TestController_Connect_ProcessStartError(t *testing.T) {
	executor := NewMockExecutor()
	executor.GetProcess().SetStartError(errors.New("failed to start"))
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start openfortivpn")
	assert.Equal(t, StateFailed, ctrl.GetState())
}

func TestController_Connect_PasswordWrittenToStdin(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "mysecretpassword"})
	require.NoError(t, err)

	// Give goroutine time to write password
	assert.Eventually(t, func() bool {
		return executor.GetProcess().GetStdinContent() == "mysecretpassword\n"
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Cleanup
	executor.GetProcess().CompleteProcess()
}

func TestController_Connect_OutputProcessing(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// Pre-populate with tunnel up message
	process := executor.GetProcess()
	process.WriteToStdout("Tunnel is up and running.")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	var connectedEventReceived bool
	var mu sync.Mutex
	ctrl.OnEvent(func(event *OutputEvent) {
		mu.Lock()
		defer mu.Unlock()
		if event.Type == EventConnected {
			connectedEventReceived = true
		}
	})

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.NoError(t, err)

	// Wait for state to become connected
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateConnected
	}, 100*time.Millisecond, 10*time.Millisecond)

	mu.Lock()
	assert.True(t, connectedEventReceived)
	mu.Unlock()

	// Cleanup
	process.CompleteProcess()
}

func TestController_Connect_ErrorOutput(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// Pre-populate with error message
	process := executor.GetProcess()
	process.WriteToStderr("ERROR: VPN authentication failed.")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	var receivedError string
	var mu sync.Mutex
	ctrl.OnError(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		receivedError = err.Error()
	})

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "wrongpassword"})
	require.NoError(t, err) // Connect itself succeeds, error comes from output

	// Wait for error to be processed
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return receivedError != ""
	}, 100*time.Millisecond, 10*time.Millisecond)

	mu.Lock()
	assert.Contains(t, receivedError, "VPN authentication failed")
	mu.Unlock()

	assert.Equal(t, StateFailed, ctrl.GetState())

	// Cleanup
	process.CompleteProcess()
}

func TestController_Connect_IPAssignment(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// Pre-populate with IP assignment
	process := executor.GetProcess()
	process.WriteToStdout("Got addresses: [10.0.0.50], ns [10.0.0.1]")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.NoError(t, err)

	// Wait for IP to be assigned
	assert.Eventually(t, func() bool {
		return ctrl.GetAssignedIP() == "10.0.0.50"
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Cleanup
	process.CompleteProcess()
}

func TestController_Disconnect_Success(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	// Connect first
	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.NoError(t, err)
	assert.Equal(t, StateConnecting, ctrl.GetState())

	// Now disconnect
	err = ctrl.Disconnect(context.Background())
	require.NoError(t, err)

	// Verify process was killed
	assert.True(t, executor.GetProcess().IsKilled())

	// Wait for state to transition
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateDisconnected
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestController_Disconnect_FromConnectedState(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// Pre-populate with tunnel up message
	process := executor.GetProcess()
	process.WriteToStdout("Tunnel is up and running.")

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.NoError(t, err)

	// Wait for connected state
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateConnected
	}, 100*time.Millisecond, 10*time.Millisecond)

	// Now disconnect
	err = ctrl.Disconnect(context.Background())
	require.NoError(t, err)

	// Verify process was killed
	assert.True(t, process.IsKilled())

	// Wait for disconnected state
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateDisconnected
	}, 100*time.Millisecond, 10*time.Millisecond)
}

func TestController_Disconnect_KillError(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// Set up Kill to return an error (e.g., user cancelled pkexec auth)
	process := executor.GetProcess()
	process.SetKillError(errors.New("authentication cancelled or pkexec not available"))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	// Connect first
	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.NoError(t, err)
	assert.Equal(t, StateConnecting, ctrl.GetState())

	// Now try to disconnect - should return the Kill error
	err = ctrl.Disconnect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to kill VPN process")
	assert.Contains(t, err.Error(), "authentication cancelled")
}

func TestController_ProcessCompletion_AutoDisconnect(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodPassword,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"})
	require.NoError(t, err)
	assert.Equal(t, StateConnecting, ctrl.GetState())

	// Simulate process exiting on its own
	executor.GetProcess().CompleteProcess()

	// Should auto-transition to disconnected
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateDisconnected
	}, 100*time.Millisecond, 10*time.Millisecond)
}
