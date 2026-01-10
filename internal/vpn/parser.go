package vpn

import (
	"regexp"
	"strings"
)

// EventType represents the type of event parsed from openfortivpn output.
type EventType string

const (
	// EventAuthenticate indicates a URL for web/SAML authentication.
	EventAuthenticate EventType = "authenticate"
	// EventConnecting indicates the tunnel is being established.
	EventConnecting EventType = "connecting"
	// EventConnected indicates the tunnel is up and running.
	EventConnected EventType = "connected"
	// EventDisconnected indicates the tunnel has gone down.
	EventDisconnected EventType = "disconnected"
	// EventGotIP indicates the VPN assigned an IP address.
	EventGotIP EventType = "got_ip"
	// EventError indicates an error occurred.
	EventError EventType = "error"
	// EventOTPRequired indicates OTP/2FA input is needed.
	EventOTPRequired EventType = "otp_required"
	// EventPasswordRequired indicates password input is needed.
	EventPasswordRequired EventType = "password_required"
)

// OutputEvent represents a parsed event from openfortivpn output.
type OutputEvent struct {
	Type    EventType
	Message string
	Data    map[string]string
}

// HasData checks if a data key exists in the event.
func (e *OutputEvent) HasData(key string) bool {
	if e.Data == nil {
		return false
	}
	_, ok := e.Data[key]
	return ok
}

// GetData retrieves a data value by key, returning empty string if not found.
func (e *OutputEvent) GetData(key string) string {
	if e.Data == nil {
		return ""
	}
	return e.Data[key]
}

// Regex patterns for parsing openfortivpn output.
var (
	// Matches: Authenticate at 'https://...'
	authenticatePattern = regexp.MustCompile(`Authenticate at '([^']+)'`)

	// Matches: Tunnel is up and running.
	tunnelUpPattern = regexp.MustCompile(`Tunnel is up and running`)

	// Matches: Tunnel is down.
	tunnelDownPattern = regexp.MustCompile(`Tunnel is down`)

	// Matches: Got addresses: [10.0.0.100], ns [...]
	gotAddressesPattern = regexp.MustCompile(`Got addresses: \[([^\]]+)\]`)

	// Matches: ERROR: message
	errorPattern = regexp.MustCompile(`ERROR:\s*(.+)`)

	// Matches: Connecting to gateway...
	connectingPattern = regexp.MustCompile(`Connecting to gateway`)

	// Matches OTP/2FA prompts
	otpPattern = regexp.MustCompile(`(?i)(two-factor|otp|token:)`)

	// Matches password prompts
	passwordPattern = regexp.MustCompile(`(?i)(password:)`)
)

// ParseLine parses a single line of openfortivpn output and returns an event if recognized.
// Returns nil if the line doesn't match any known pattern.
func ParseLine(line string) *OutputEvent {
	// Skip empty or whitespace-only lines
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	// Check for authentication URL
	if matches := authenticatePattern.FindStringSubmatch(line); matches != nil {
		return &OutputEvent{
			Type:    EventAuthenticate,
			Message: line,
			Data:    map[string]string{"url": matches[1]},
		}
	}

	// Check for tunnel up
	if tunnelUpPattern.MatchString(line) {
		return &OutputEvent{
			Type:    EventConnected,
			Message: "Tunnel is up and running",
		}
	}

	// Check for tunnel down
	if tunnelDownPattern.MatchString(line) {
		return &OutputEvent{
			Type:    EventDisconnected,
			Message: "Tunnel is down",
		}
	}

	// Check for IP address assignment
	if matches := gotAddressesPattern.FindStringSubmatch(line); matches != nil {
		return &OutputEvent{
			Type:    EventGotIP,
			Message: line,
			Data:    map[string]string{"ip": matches[1]},
		}
	}

	// Check for errors
	if matches := errorPattern.FindStringSubmatch(line); matches != nil {
		return &OutputEvent{
			Type:    EventError,
			Message: strings.TrimSpace(matches[1]),
		}
	}

	// Check for connecting status
	if connectingPattern.MatchString(line) {
		return &OutputEvent{
			Type:    EventConnecting,
			Message: "Connecting to gateway",
		}
	}

	// Check for OTP prompt
	if otpPattern.MatchString(line) {
		return &OutputEvent{
			Type:    EventOTPRequired,
			Message: line,
		}
	}

	// Check for password prompt
	if passwordPattern.MatchString(line) {
		return &OutputEvent{
			Type:    EventPasswordRequired,
			Message: line,
		}
	}

	// Unrecognized line
	return nil
}
