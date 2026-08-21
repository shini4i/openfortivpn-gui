package vpn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
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

// TestController_BuildCommandArgs asserts the exact openfortivpn argument
// vector for each supported profile/options shape. Each expected slice is
// derived directly from buildCommandArgs in its emission order, so full-slice
// equality validates ordering, flag/value pairing (e.g. "-u" adjacent to the
// username), and completeness — catching accidental, duplicated, or contradictory
// flags that a presence-only check (assert.Contains) would miss.
//
// Note: the password is intentionally absent from every expected slice — it is
// delivered via stdin, not argv (see TestController_Connect_OTP_DeliversPasswordAndOTP
// and TestController_Connect_PasswordWrittenToStdin).
func TestController_BuildCommandArgs(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	const (
		host = "vpn.example.com"
		port = 443
	)

	tests := []struct {
		name string
		p    *profile.Profile
		opts *ConnectOptions
		want []string
	}{
		{
			name: "password auth with realm, dns and routes enabled",
			p: &profile.Profile{
				Host: host, Port: port, Username: "testuser",
				AuthMethod: profile.AuthMethodPassword,
				Realm:      "testrealm",
				SetDNS:     true, SetRoutes: true,
			},
			want: []string{
				"vpn.example.com:443",
				"-u", "testuser",
				"--realm=testrealm",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
			},
		},
		{
			name: "password auth with dns and routes disabled",
			p: &profile.Profile{
				Host: host, Port: port, Username: "testuser",
				AuthMethod: profile.AuthMethodPassword,
				SetDNS:     false, SetRoutes: false,
			},
			want: []string{
				"vpn.example.com:443",
				"-u", "testuser",
				"--set-dns=0",
				"--set-routes=0",
				"--half-internet-routes=0",
			},
		},
		{
			name: "password auth with half-internet-routes enabled",
			p: &profile.Profile{
				Host: host, Port: port, Username: "testuser",
				AuthMethod: profile.AuthMethodPassword,
				SetDNS:     true, SetRoutes: true, HalfInternetRoutes: true,
			},
			want: []string{
				"vpn.example.com:443",
				"-u", "testuser",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=1",
			},
		},
		{
			name: "password auth without username omits -u",
			p: &profile.Profile{
				Host: host, Port: port,
				AuthMethod: profile.AuthMethodPassword,
				SetDNS:     true, SetRoutes: true,
			},
			want: []string{
				"vpn.example.com:443",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
			},
		},
		{
			// SECURITY: the OTP must never appear in argv — it is delivered
			// via stdin (see TestController_Connect_OTP_DeliversPasswordAndOTP).
			name: "otp auth keeps username and never puts the otp in argv",
			p: &profile.Profile{
				Host: host, Port: port, Username: "testuser",
				AuthMethod: profile.AuthMethodOTP,
				SetDNS:     true, SetRoutes: true,
			},
			opts: &ConnectOptions{Password: "password", OTP: "123456"},
			want: []string{
				"vpn.example.com:443",
				"-u", "testuser",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
			},
		},
		{
			name: "otp auth without otp value produces same argv",
			p: &profile.Profile{
				Host: host, Port: port, Username: "testuser",
				AuthMethod: profile.AuthMethodOTP,
				SetDNS:     true, SetRoutes: true,
			},
			opts: nil,
			want: []string{
				"vpn.example.com:443",
				"-u", "testuser",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
			},
		},
		{
			name: "certificate auth passes user-cert and user-key, no -u",
			p: &profile.Profile{
				Host: host, Port: port,
				AuthMethod:     profile.AuthMethodCertificate,
				ClientCertPath: "/path/to/cert.pem",
				ClientKeyPath:  "/path/to/key.pem",
				SetDNS:         true, SetRoutes: true,
			},
			want: []string{
				"vpn.example.com:443",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
				"--user-cert=/path/to/cert.pem",
				"--user-key=/path/to/key.pem",
			},
		},
		{
			name: "saml auth passes --saml-login, no -u",
			p: &profile.Profile{
				Host: host, Port: port,
				AuthMethod: profile.AuthMethodSAML,
				SetDNS:     true, SetRoutes: true,
			},
			want: []string{
				"vpn.example.com:443",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
				"--saml-login",
			},
		},
		{
			name: "trusted cert appends --trusted-cert",
			p: &profile.Profile{
				Host: host, Port: port, Username: "testuser",
				AuthMethod:  profile.AuthMethodPassword,
				TrustedCert: "abc123def456",
				SetDNS:      true, SetRoutes: true,
			},
			want: []string{
				"vpn.example.com:443",
				"-u", "testuser",
				"--set-dns=1",
				"--set-routes=1",
				"--half-internet-routes=0",
				"--trusted-cert=abc123def456",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ctrl.buildCommandArgs(tt.p, tt.opts)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestController_Connect_OTP_DeliversPasswordAndOTP verifies that a 2FA
// connection delivers BOTH credentials via stdin — password first (answering
// openfortivpn's password prompt), then the one-time password (answering the
// "Two-factor authentication token:" prompt). Neither secret may ever appear
// in argv, which is world-readable via /proc/<pid>/cmdline.
func TestController_Connect_OTP_DeliversPasswordAndOTP(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodOTP,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{
		Password: "topsecret",
		OTP:      "123456",
	})
	require.NoError(t, err)

	// Neither credential may appear in argv.
	for _, arg := range executor.GetLastArgs() {
		assert.NotContains(t, arg, "topsecret", "password must never appear in command-line arguments")
		assert.NotContains(t, arg, "123456", "OTP must never appear in command-line arguments")
	}

	// Both credentials are delivered via stdin, password first.
	assert.Eventually(t, func() bool {
		return executor.GetProcess().GetStdinContent() == "topsecret\n123456\n"
	}, 100*time.Millisecond, 10*time.Millisecond, "password then OTP must be written to stdin")

	// Cleanup
	executor.GetProcess().CompleteProcess()
}

// TestController_Connect_OTPWithoutPassword_DeliversOTP verifies that the OTP
// still reaches stdin when no account password is supplied.
func TestController_Connect_OTPWithoutPassword_DeliversOTP(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	p := &profile.Profile{
		ID:         "550e8400-e29b-41d4-a716-446655440000",
		Name:       "Test VPN",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "testuser",
		AuthMethod: profile.AuthMethodOTP,
		SetDNS:     true,
		SetRoutes:  true,
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{OTP: "654321"})
	require.NoError(t, err)

	assert.Eventually(t, func() bool {
		return executor.GetProcess().GetStdinContent() == "654321\n"
	}, 100*time.Millisecond, 10*time.Millisecond, "OTP must be written to stdin even without a password")

	executor.GetProcess().CompleteProcess()
}

// TestController_OutputProcessing_LongLine verifies that output lines longer
// than bufio.Scanner's 64 KiB default are still delivered instead of silently
// killing the output-processing goroutine.
//
// Regression test: the scanners previously used the default buffer, so one
// long line stopped all event parsing for the rest of the connection.
func TestController_OutputProcessing_LongLine(t *testing.T) {
	executor := NewMockExecutor()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))

	// One line well past the 64 KiB default cap, followed by a parseable event.
	longLine := strings.Repeat("x", 100*1024)
	process := executor.GetProcess()
	process.WriteToStdout(longLine)
	process.WriteToStdout("Tunnel is up and running.")

	var gotLong, gotTunnelUp bool
	var mu sync.Mutex
	ctrl.OnOutput(func(line string) {
		mu.Lock()
		defer mu.Unlock()
		if line == longLine {
			gotLong = true
		}
		if line == "Tunnel is up and running." {
			gotTunnelUp = true
		}
	})

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

	require.NoError(t, ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "pw"}))

	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return gotLong && gotTunnelUp
	}, 2*time.Second, 10*time.Millisecond,
		"long line and subsequent output must both be delivered")

	executor.GetProcess().CompleteProcess()
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
	assert.Error(t, executor.GetLastCtx().Err(), "the failed attempt must release its context")
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
	assert.Error(t, executor.GetLastCtx().Err(), "the failed attempt must release its context")
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

	// Wrap the real cancel func in a spy so we can assert the context is
	// released even when Kill fails. Regression guard: Disconnect cancels via
	// a deferred call so the early return on Kill error still runs it; dropping
	// that would leak the context and the goroutines that hold it.
	var cancelCalled bool
	ctrl.mu.Lock()
	realCancel := ctrl.cancel
	ctrl.cancel = func() {
		cancelCalled = true
		realCancel()
	}
	ctrl.mu.Unlock()

	// Now try to disconnect - should return the Kill error
	err = ctrl.Disconnect(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to kill VPN process")
	assert.Contains(t, err.Error(), "authentication cancelled")
	assert.True(t, cancelCalled, "cancel must run even when Kill fails, to release the context")
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

// TestController_Disconnect_GracefulBeforeContextCancel verifies that Disconnect
// delivers the graceful SIGTERM (and its grace window) BEFORE cancelling the
// process context.
//
// The process is launched with exec.CommandContext, so cancelling the context
// SIGKILLs the child. If cancel() ran before the graceful Kill(), an
// openfortivpn that needs a moment to tear the tunnel down cleanly would be
// SIGKILLed mid-shutdown, defeating the SIGTERM->grace->SIGKILL escalation this
// code implements.
//
// The child traps SIGTERM, sleeps briefly, then exits 42. Observing exit code
// 42 proves the trap ran to completion — i.e. SIGTERM was honored and no
// context-driven SIGKILL interrupted it. The sleep widens the window so the
// old cancel()-first ordering would reliably SIGKILL the child instead.
func TestController_Disconnect_GracefulBeforeContextCancel(t *testing.T) {
	oldGrace := sigtermGracePeriod
	sigtermGracePeriod = 2 * time.Second
	defer func() { sigtermGracePeriod = oldGrace }()

	c := NewController("unused", WithDirectMode())

	ctx, cancel := context.WithCancel(context.Background())
	proc, err := NewDirectExecutor().CreateProcess(ctx, "sh", "-c",
		`trap 'sleep 0.3; exit 42' TERM; while true; do sleep 0.05; done`)
	require.NoError(t, err)
	require.NoError(t, proc.Start())

	waitErr := make(chan error, 1)
	go func() { waitErr <- proc.Wait() }()

	// Let the shell install its SIGTERM trap before we signal it.
	time.Sleep(200 * time.Millisecond)

	// Wire the live process into the controller as if Connect had started it.
	c.mu.Lock()
	c.state = StateConnected
	c.process = proc
	c.ctx = ctx
	c.cancel = cancel
	c.mu.Unlock()

	require.NoError(t, c.Disconnect(context.Background()))

	select {
	case err := <-waitErr:
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "process should exit via its SIGTERM trap, not SIGKILL")
		assert.Equal(t, 42, exitErr.ExitCode(),
			"SIGTERM must reach the process and its trap run to completion before the context is cancelled")
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after Disconnect")
	}
}

// TestController_ProcessOutput_Error_EmitsErrorBeforeStateChange pins the
// emission order the UI depends on: the error callback must run before the
// terminal state change, because the state change is what forgets the in-flight
// profile the error handler needs to invalidate a rejected password.
func TestController_ProcessOutput_Error_EmitsErrorBeforeStateChange(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")
	_ = ctrl.setState(StateConnecting)

	var mu sync.Mutex
	var sequence []string

	ctrl.OnError(func(error) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, "error")
	})
	ctrl.OnStateChange(func(_, newState ConnectionState) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, "state:"+string(newState))
	})

	ctrl.processOutput(
		"ERROR:  Could not authenticate to gateway. Please check the password, client certificate, etc.")

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"error", "state:failed"}, sequence,
		"the error must reach the UI before the profile is forgotten")
}

// TestController_ProcessCompletion_WaitsForPendingOutput pins the ordering the
// UI depends on: the last output line reaches the callbacks before the terminal
// state does. The connection outlives outputDrainTimeout on purpose, since the
// bound must be measured from the process exit, not from the connect.
func TestController_ProcessCompletion_WaitsForPendingOutput(t *testing.T) {
	const drainTimeout = 150 * time.Millisecond

	proc := newFakeProcess()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(&fakeExecutor{processes: []Process{proc}}))
	ctrl.outputDrainTimeout = drainTimeout

	var mu sync.Mutex
	var sequence []string
	ctrl.OnOutput(func(string) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, "output")
	})
	ctrl.OnError(func(error) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, "error")
	})
	ctrl.OnStateChange(func(_, newState ConnectionState) {
		mu.Lock()
		defer mu.Unlock()
		sequence = append(sequence, "state:"+string(newState))
	})

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

	// The final error line becomes readable exactly when the process is reaped,
	// so the drain is what decides whether it is seen at all.
	proc.onWait = func() {
		proc.stderr.deliver("ERROR:  Could not authenticate to gateway. Please check the password, client certificate, etc.")
		proc.stdout.deliver()
	}

	err := ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "wrongpassword"})
	require.NoError(t, err)

	// A session longer than the drain bound, with the pipes quiet throughout.
	time.Sleep(2 * drainTimeout)

	proc.exit()

	require.Eventually(t, func() bool {
		return ctrl.GetState() == StateFailed
	}, time.Second, 10*time.Millisecond)

	// Join the completion goroutine before the test ends, so its pipe cleanup is
	// not still in flight.
	require.Eventually(t, func() bool {
		return proc.stdout.isClosed() && proc.stderr.isClosed()
	}, time.Second, 10*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"state:connecting", "output", "error", "state:failed"}, sequence,
		"the final line must reach the UI before the connection reaches a terminal state")
}

// TestController_ProcessCompletion_BoundedOutputDrain verifies that output pipes
// which never reach EOF cannot wedge the state machine: a grandchild that
// inherited them keeps them open after openfortivpn itself is gone, so the drain
// is bounded, the pipes are closed, and the terminal state still arrives.
func TestController_ProcessCompletion_BoundedOutputDrain(t *testing.T) {
	proc := newFakeProcess()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(&fakeExecutor{processes: []Process{proc}}))
	ctrl.outputDrainTimeout = 50 * time.Millisecond

	var mu sync.Mutex
	var emittedErrors []string
	ctrl.OnError(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		emittedErrors = append(emittedErrors, err.Error())
	})

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

	// Process exits, but nothing ever closes the output pipes.
	proc.exit()

	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateDisconnected
	}, time.Second, 10*time.Millisecond)

	assert.True(t, proc.stdout.isClosed(), "the abandoned stdout pipe must be released")
	assert.True(t, proc.stderr.isClosed(), "the abandoned stderr pipe must be released")

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, emittedErrors, "closing our own pipes is not a failure worth reporting")
}

// gatedReader emulates one output pipe of a process: Read blocks while the pipe
// is open and empty, and once closed it reports os.ErrClosed the way a closed
// *os.File does, discarding anything not yet read.
type gatedReader struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	eof    bool
	closed bool
}

func newGatedReader() *gatedReader {
	r := &gatedReader{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *gatedReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.buf) == 0 && !r.eof && !r.closed {
		r.cond.Wait()
	}
	if r.closed {
		return 0, os.ErrClosed
	}
	if len(r.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}

func (r *gatedReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	r.cond.Broadcast()
	return nil
}

func (r *gatedReader) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// deliver makes the given lines readable and then ends the stream.
func (r *gatedReader) deliver(lines ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, line := range lines {
		r.buf = append(r.buf, line+"\n"...)
	}
	r.eof = true
	r.cond.Broadcast()
}

// fakeProcess emulates the pipe contract newCmdWithPipes provides: the output
// pipes survive Wait, so only the child's exit or an explicit Close ends them.
// MockProcess cannot stand in here — its readers report EOF immediately, so it
// cannot model output still pending when the process is reaped.
type fakeProcess struct {
	stdout   *gatedReader
	stderr   *gatedReader
	stdin    *mockWriteCloser
	exited   chan struct{}
	exitOnce sync.Once

	// onWait runs inside Wait, just before it returns: whatever it delivers is
	// pending at the instant the process is reaped.
	onWait func()
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{
		stdout: newGatedReader(),
		stderr: newGatedReader(),
		stdin:  &mockWriteCloser{buf: &bytes.Buffer{}},
		exited: make(chan struct{}),
	}
}

// exit signals that the process has terminated.
func (p *fakeProcess) exit() {
	p.exitOnce.Do(func() { close(p.exited) })
}

func (p *fakeProcess) Start() error { return nil }

func (p *fakeProcess) Wait() error {
	<-p.exited
	if p.onWait != nil {
		p.onWait()
	}
	return nil
}

func (p *fakeProcess) Kill() error {
	p.exit()
	return nil
}

func (p *fakeProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *fakeProcess) Stdout() io.ReadCloser { return p.stdout }
func (p *fakeProcess) Stderr() io.ReadCloser { return p.stderr }

// fakeExecutor hands out preconstructed processes, one per CreateProcess call.
// Running out is an error rather than a repeat: handing the same process back
// twice would make the controller's ownership check trivially true.
type fakeExecutor struct {
	mu        sync.Mutex
	processes []Process

	// beforeCreate runs at the start of each CreateProcess call, which is the
	// window where the attempt owns the state but has no process registered.
	beforeCreate func(call int)
	calls        int
}

func (e *fakeExecutor) CreateProcess(context.Context, string, ...string) (Process, error) {
	e.mu.Lock()
	hook := e.beforeCreate
	e.calls++
	call := e.calls
	e.mu.Unlock()

	if hook != nil {
		hook(call)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.processes) == 0 {
		return nil, errors.New("fakeExecutor: no process left to hand out")
	}
	process := e.processes[0]
	e.processes = e.processes[1:]
	return process, nil
}

// TestController_ProcessCompletion_DoesNotClobberNewerConnection covers the
// window the bounded drain opens: a failed connection whose pipes are still held
// open keeps its completion goroutine alive while the user reconnects. That
// goroutine must leave the newer connection's state and process untouched.
func TestController_ProcessCompletion_DoesNotClobberNewerConnection(t *testing.T) {
	failed := newFakeProcess()
	current := newFakeProcess()
	ctrl := NewController("/usr/bin/openfortivpn",
		WithExecutor(&fakeExecutor{processes: []Process{failed, current}}))
	ctrl.outputDrainTimeout = 150 * time.Millisecond

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

	require.NoError(t, ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "wrongpassword"}))

	// The first attempt fails and exits, but a descendant keeps stdout open, so
	// its completion goroutine sits in the bounded drain.
	failed.stderr.deliver("ERROR:  Could not authenticate to gateway. Please check the password, client certificate, etc.")
	failed.exit()
	assert.Eventually(t, func() bool {
		return ctrl.GetState() == StateFailed
	}, time.Second, 10*time.Millisecond)

	// The user retries while the stale goroutine is still draining.
	require.NoError(t, ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "rightpassword"}))
	require.Equal(t, StateConnecting, ctrl.GetState())
	t.Cleanup(current.exit)

	// Closed pipes mean the stale goroutine is past its drain and has reached
	// the ownership check — without this the assertions below pass vacuously.
	require.Eventually(t, func() bool {
		return failed.stdout.isClosed() && failed.stderr.isClosed()
	}, time.Second, 10*time.Millisecond)

	assert.Never(t, func() bool {
		return ctrl.GetState() != StateConnecting
	}, 100*time.Millisecond, 10*time.Millisecond,
		"a stale completion goroutine must not report a terminal state for a newer connection")

	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	assert.Same(t, current, ctrl.process, "the newer process must stay registered")
	assert.NotNil(t, ctrl.stdin, "the newer connection's stdin must stay open")
}

// TestController_ScanOutput_ClosedPipeIsNotAnError pins that releasing our own
// output pipes stays silent. The completion path closes them once the scanners
// are done, and surfacing that as an error would pop a VPN error dialog after
// every connection.
func TestController_ScanOutput_ClosedPipeIsNotAnError(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	var mu sync.Mutex
	var emittedErrors []string
	ctrl.OnError(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		emittedErrors = append(emittedErrors, err.Error())
	})

	reader := newGatedReader()
	require.NoError(t, reader.Close())

	// An uncancelled context: the suppression must come from the error itself,
	// not from a shutdown that happens to be in progress.
	ctrl.scanOutput(context.Background(), "stdout", reader)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, emittedErrors)
}

// TestController_Disconnect_BoundedOutputDrain covers a user-initiated
// disconnect whose output pipes outlive the process: the drain must still give
// up on the bound so the UI learns the connection is gone.
func TestController_Disconnect_BoundedOutputDrain(t *testing.T) {
	proc := newFakeProcess()
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(&fakeExecutor{processes: []Process{proc}}))
	ctrl.outputDrainTimeout = 50 * time.Millisecond

	var mu sync.Mutex
	var emittedErrors []string
	ctrl.OnError(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		emittedErrors = append(emittedErrors, err.Error())
	})

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

	require.NoError(t, ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "password"}))

	// Kill reaps the process, but a descendant keeps the pipes open.
	require.NoError(t, ctrl.Disconnect(context.Background()))

	require.Eventually(t, func() bool {
		return ctrl.GetState() == StateDisconnected
	}, time.Second, 10*time.Millisecond)

	assert.True(t, proc.stdout.isClosed(), "the abandoned stdout pipe must be released")
	assert.True(t, proc.stderr.isClosed(), "the abandoned stderr pipe must be released")

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, emittedErrors, "a requested disconnect is not a failure")
}

// TestController_ProcessCompletion_DoesNotClobberRetryBeforeItsProcessStarts
// closes the narrow window a process-identity check leaves open: a retry owns
// the state from the moment it reaches Connecting, but registers its process
// only later. A stale completion goroutine waking in between must still stand
// down, or the retry's live tunnel becomes unstoppable from the UI.
func TestController_ProcessCompletion_DoesNotClobberRetryBeforeItsProcessStarts(t *testing.T) {
	failed := newFakeProcess()
	retried := newFakeProcess()
	executor := &fakeExecutor{processes: []Process{failed, retried}}
	ctrl := NewController("/usr/bin/openfortivpn", WithExecutor(executor))
	ctrl.outputDrainTimeout = 50 * time.Millisecond

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

	// The retry parks inside CreateProcess — state already Connecting, no
	// process registered — until the stale goroutine has had its say.
	executor.beforeCreate = func(call int) {
		if call != 2 {
			return
		}
		failed.exit()
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if ctrl.GetState() != StateConnecting {
				return // the stale goroutine reported a terminal state
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	require.NoError(t, ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "wrongpassword"}))

	// The first attempt fails; its pipes stay open, so its completion goroutine
	// is still alive when the retry starts.
	failed.stderr.deliver("ERROR:  Could not authenticate to gateway. Please check the password, client certificate, etc.")
	require.Eventually(t, func() bool {
		return ctrl.GetState() == StateFailed
	}, time.Second, 10*time.Millisecond)

	require.NoError(t, ctrl.Connect(context.Background(), p, &ConnectOptions{Password: "rightpassword"}))
	t.Cleanup(retried.exit)

	assert.Equal(t, StateConnecting, ctrl.GetState(),
		"the retry must keep the state it claimed before its process was registered")

	ctrl.mu.RLock()
	defer ctrl.mu.RUnlock()
	assert.Same(t, retried, ctrl.process, "the retry's process must stay registered")
	assert.NotNil(t, ctrl.stdin, "the retry's stdin must stay open")
}

// TestController_ReportDisconnected_IgnoresStaleAttempt covers the gap between a
// completion goroutine's ownership check and its state change: a scanner that
// outlived the drain can fail the old attempt, which frees the UI to start the
// next one. The stale goroutine must not report that newer attempt as gone.
func TestController_ReportDisconnected_IgnoresStaleAttempt(t *testing.T) {
	ctrl := NewController("/usr/bin/openfortivpn")

	var mu sync.Mutex
	var transitions []string
	ctrl.OnStateChange(func(_, newState ConnectionState) {
		mu.Lock()
		defer mu.Unlock()
		transitions = append(transitions, string(newState))
	})

	stale, err := ctrl.beginAttempt()
	require.NoError(t, err)
	require.NoError(t, ctrl.setState(StateFailed))

	current, err := ctrl.beginAttempt()
	require.NoError(t, err)
	require.NotEqual(t, stale, current)

	ctrl.reportDisconnected(stale)

	assert.Equal(t, StateConnecting, ctrl.GetState(),
		"a stale attempt must not report the current one as disconnected")

	// The attempt that does own the controller still gets its terminal state.
	ctrl.reportDisconnected(current)
	assert.Equal(t, StateDisconnected, ctrl.GetState())

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"connecting", "failed", "connecting", "disconnected"}, transitions,
		"the stale call must emit no state change at all")
}
