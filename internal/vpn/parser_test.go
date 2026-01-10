package vpn

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLine_AuthenticateURL(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		wantURL  string
		wantType EventType
	}{
		{
			name:     "SAML authentication URL",
			line:     "Authenticate at 'https://vpn.example.com:443/remote/saml/start?id=abc123'",
			wantURL:  "https://vpn.example.com:443/remote/saml/start?id=abc123",
			wantType: EventAuthenticate,
		},
		{
			name:     "Web authentication URL",
			line:     "Authenticate at 'https://10.0.0.1/remote/login'",
			wantURL:  "https://10.0.0.1/remote/login",
			wantType: EventAuthenticate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseLine(tt.line)
			require.NotNil(t, event)
			assert.Equal(t, tt.wantType, event.Type)
			assert.Equal(t, tt.wantURL, event.Data["url"])
		})
	}
}

func TestParseLine_TunnelUp(t *testing.T) {
	lines := []string{
		"Tunnel is up and running.",
		"INFO:   Tunnel is up and running.",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			event := ParseLine(line)
			require.NotNil(t, event)
			assert.Equal(t, EventConnected, event.Type)
		})
	}
}

func TestParseLine_TunnelDown(t *testing.T) {
	lines := []string{
		"Tunnel is down.",
		"INFO:   Tunnel is down.",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			event := ParseLine(line)
			require.NotNil(t, event)
			assert.Equal(t, EventDisconnected, event.Type)
		})
	}
}

func TestParseLine_GotAddresses(t *testing.T) {
	tests := []struct {
		name   string
		line   string
		wantIP string
	}{
		{
			name:   "IPv4 address",
			line:   "Got addresses: [10.0.0.100], ns [10.0.0.1, 10.0.0.2]",
			wantIP: "10.0.0.100",
		},
		{
			name:   "Different IP format",
			line:   "INFO:   Got addresses: [192.168.1.50], ns [8.8.8.8]",
			wantIP: "192.168.1.50",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseLine(tt.line)
			require.NotNil(t, event)
			assert.Equal(t, EventGotIP, event.Type)
			assert.Equal(t, tt.wantIP, event.Data["ip"])
		})
	}
}

func TestParseLine_Error(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantMessage string
	}{
		{
			name:        "Connection error",
			line:        "ERROR:  Could not connect to gateway.",
			wantMessage: "Could not connect to gateway.",
		},
		{
			name:        "Authentication error",
			line:        "ERROR:  VPN authentication failed.",
			wantMessage: "VPN authentication failed.",
		},
		{
			name:        "Generic error",
			line:        "ERROR:  Something went wrong",
			wantMessage: "Something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := ParseLine(tt.line)
			require.NotNil(t, event)
			assert.Equal(t, EventError, event.Type)
			assert.Equal(t, tt.wantMessage, event.Message)
		})
	}
}

func TestParseLine_ConnectingToGateway(t *testing.T) {
	lines := []string{
		"Connecting to gateway...",
		"INFO:   Connecting to gateway...",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			event := ParseLine(line)
			require.NotNil(t, event)
			assert.Equal(t, EventConnecting, event.Type)
		})
	}
}

func TestParseLine_UnrecognizedLine(t *testing.T) {
	lines := []string{
		"Some random log message",
		"DEBUG: internal state xyz",
		"",
		"   ",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			event := ParseLine(line)
			assert.Nil(t, event, "Expected nil for unrecognized line: %q", line)
		})
	}
}

func TestParseLine_OTPRequired(t *testing.T) {
	lines := []string{
		"Two-factor authentication token:",
		"Please enter OTP:",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			event := ParseLine(line)
			require.NotNil(t, event)
			assert.Equal(t, EventOTPRequired, event.Type)
		})
	}
}

func TestParseLine_PasswordRequired(t *testing.T) {
	lines := []string{
		"VPN account password:",
		"Password:",
	}

	for _, line := range lines {
		t.Run(line, func(t *testing.T) {
			event := ParseLine(line)
			require.NotNil(t, event)
			assert.Equal(t, EventPasswordRequired, event.Type)
		})
	}
}

func TestEventType_String(t *testing.T) {
	tests := []struct {
		eventType EventType
		expected  string
	}{
		{EventAuthenticate, "authenticate"},
		{EventConnecting, "connecting"},
		{EventConnected, "connected"},
		{EventDisconnected, "disconnected"},
		{EventGotIP, "got_ip"},
		{EventError, "error"},
		{EventOTPRequired, "otp_required"},
		{EventPasswordRequired, "password_required"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.eventType))
		})
	}
}

func TestOutputEvent_HasData(t *testing.T) {
	event := &OutputEvent{
		Type:    EventGotIP,
		Message: "Got IP address",
		Data:    map[string]string{"ip": "10.0.0.1"},
	}

	assert.True(t, event.HasData("ip"))
	assert.False(t, event.HasData("nonexistent"))
}

func TestOutputEvent_GetData(t *testing.T) {
	event := &OutputEvent{
		Type:    EventGotIP,
		Message: "Got IP address",
		Data:    map[string]string{"ip": "10.0.0.1"},
	}

	assert.Equal(t, "10.0.0.1", event.GetData("ip"))
	assert.Equal(t, "", event.GetData("nonexistent"))
}

func TestOutputEvent_HasData_NilData(t *testing.T) {
	event := &OutputEvent{
		Type:    EventConnected,
		Message: "Tunnel is up",
		Data:    nil,
	}

	assert.False(t, event.HasData("anything"))
}

func TestOutputEvent_GetData_NilData(t *testing.T) {
	event := &OutputEvent{
		Type:    EventConnected,
		Message: "Tunnel is up",
		Data:    nil,
	}

	assert.Equal(t, "", event.GetData("anything"))
}
