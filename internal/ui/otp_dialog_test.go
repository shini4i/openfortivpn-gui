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
