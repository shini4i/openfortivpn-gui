package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsValidOTP verifies OTP format validation. Most authenticators produce
// 6-digit codes, but FortiToken hardware and SMS tokens can be longer and
// alphanumeric — the validator must not reject them.
//
// Regression test: the previous pattern (^\d{4,8}$) rejected alphanumeric
// tokens some FortiGate deployments issue.
func TestIsValidOTP(t *testing.T) {
	tests := []struct {
		name  string
		otp   string
		valid bool
	}{
		{"standard 6-digit TOTP", "123456", true},
		{"4-digit code", "1234", true},
		{"8-digit code", "12345678", true},
		{"alphanumeric token", "a1b2c3d4", true},
		{"uppercase alphanumeric token", "FTK2A4X9", true},
		{"16-character token", strings.Repeat("a1", 8), true},
		{"too short", "123", false},
		{"too long", strings.Repeat("1", 17), false},
		{"empty", "", false},
		{"contains space", "123 456", false},
		{"contains shell metacharacter", "123;rm", false},
		{"contains unicode", "12345ä", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, isValidOTP(tt.otp))
		})
	}
}

// TestOTPResultFor verifies the mapping from an AdwAlertDialog response id to a
// dialog result: only the "submit" response carries the OTP; every other
// response id (cancel, the close-response emitted when the dialog is dismissed,
// or anything unexpected) is treated as a cancellation.
//
// Regression context: the Submit button no longer vetoes invalid input inside
// the response handler (AdwAlertDialog auto-closes on any response, so a veto
// there silently aborted the connect with no feedback). Submit is now gated to
// valid input via SetResponseEnabled, and this mapper is the pure decision the
// handler applies once a response fires.
func TestOTPResultFor(t *testing.T) {
	tests := []struct {
		name          string
		response      string
		otp           string
		wantCancelled bool
		wantOTP       string
	}{
		{"submit carries the entered token", "submit", "123456", false, "123456"},
		{"cancel is a cancellation", "cancel", "123456", true, ""},
		{"empty/close response is a cancellation", "", "123456", true, ""},
		{"unexpected response is a cancellation", "bogus", "123456", true, ""},
		// The mapper does NOT re-validate: token validity is enforced by the
		// dialog's SetResponseEnabled gate, so a "submit" response carries
		// whatever was entered verbatim. These cases pin that trust boundary so
		// the gate cannot be dropped on the assumption the mapper validates.
		{"submit passes an empty token through verbatim", "submit", "", false, ""},
		{"submit passes an invalid token through verbatim", "submit", "12", false, "12"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := otpResultFor(tt.response, tt.otp)
			assert.Equal(t, tt.wantCancelled, got.Cancelled, "cancelled")
			assert.Equal(t, tt.wantOTP, got.OTP, "otp")
		})
	}
}
