package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
)

// Method combo indices in the profile editor. Mirrors the StringList order
// in setupWidget: 0 = Password, 1 = Certificate, 2 = SAML/SSO.
const (
	methodIndexPassword    uint = 0
	methodIndexCertificate uint = 1
	methodIndexSAML        uint = 2
)

func TestAuthMethodToSelection(t *testing.T) {
	tests := []struct {
		name      string
		method    profile.AuthMethod
		wantIndex uint
		wantOTP   bool
	}{
		{"password maps to password method, OTP off", profile.AuthMethodPassword, methodIndexPassword, false},
		{"otp maps to password method, OTP on", profile.AuthMethodOTP, methodIndexPassword, true},
		{"certificate maps to certificate method", profile.AuthMethodCertificate, methodIndexCertificate, false},
		{"saml maps to saml method", profile.AuthMethodSAML, methodIndexSAML, false},
		{"unknown method falls back to password", profile.AuthMethod("bogus"), methodIndexPassword, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIndex, gotOTP := authMethodToSelection(tt.method)
			assert.Equal(t, tt.wantIndex, gotIndex, "method index")
			assert.Equal(t, tt.wantOTP, gotOTP, "otp enabled")
		})
	}
}

func TestSelectionToAuthMethod(t *testing.T) {
	tests := []struct {
		name       string
		index      uint
		otpEnabled bool
		want       profile.AuthMethod
	}{
		{"password method, OTP off -> password", methodIndexPassword, false, profile.AuthMethodPassword},
		{"password method, OTP on -> otp", methodIndexPassword, true, profile.AuthMethodOTP},
		{"certificate method -> certificate (OTP ignored)", methodIndexCertificate, true, profile.AuthMethodCertificate},
		{"saml method, OTP off -> saml", methodIndexSAML, false, profile.AuthMethodSAML},
		{"saml method -> saml (OTP ignored)", methodIndexSAML, true, profile.AuthMethodSAML},
		{"unknown index falls back to password", uint(99), false, profile.AuthMethodPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectionToAuthMethod(tt.index, tt.otpEnabled)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAuthMethodSelectionRoundTrip verifies every stored AuthMethod survives a
// load-into-editor (authMethodToSelection) then save-from-editor
// (selectionToAuthMethod) cycle unchanged. This is the core guarantee: a
// profile's auth method must not silently change just by opening the editor.
func TestAuthMethodSelectionRoundTrip(t *testing.T) {
	methods := []profile.AuthMethod{
		profile.AuthMethodPassword,
		profile.AuthMethodOTP,
		profile.AuthMethodCertificate,
		profile.AuthMethodSAML,
	}

	for _, m := range methods {
		t.Run(string(m), func(t *testing.T) {
			index, otp := authMethodToSelection(m)
			got := selectionToAuthMethod(index, otp)
			assert.Equal(t, m, got, "round trip must preserve auth method")
		})
	}
}
