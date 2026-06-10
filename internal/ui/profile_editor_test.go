package ui

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
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

// TestApplyFormValues_PreservesAndOverwritesAllFields verifies that
// applyFormValues produces a profile where every editor-controlled field comes
// from the form and nothing from the stored profile is lost.
//
// Regression test for the field-loss bug: GetProfile used to construct a fresh
// Profile{} and copy only editor-backed fields, silently zeroing AutoReconnect
// and HalfInternetRoutes on every save (NewProfile defaults AutoReconnect to
// true, so the first save killed auto-reconnect for every profile).
func TestApplyFormValues_PreservesAndOverwritesAllFields(t *testing.T) {
	current := &profile.Profile{
		ID:                 "0d6db4a4-6d27-4626-a047-87a05a91c218",
		Name:               "old-name",
		Description:        "old-desc",
		Host:               "old.example.com",
		Port:               443,
		AuthMethod:         profile.AuthMethodPassword,
		Username:           "old-user",
		Realm:              "old-realm",
		TrustedCert:        "old-cert-hash",
		ClientCertPath:     "/old/cert.pem",
		ClientKeyPath:      "/old/key.pem",
		SetDNS:             false,
		SetRoutes:          false,
		HalfInternetRoutes: true,
		AutoReconnect:      true,
	}

	form := editorFormValues{
		Name:               "new-name",
		Description:        "new-desc",
		Host:               "new.example.com",
		Port:               10443,
		Realm:              "new-realm",
		Username:           "new-user",
		MethodIndex:        methodIndexPassword,
		OTPEnabled:         true,
		ClientCertPath:     "/new/cert.pem",
		ClientKeyPath:      "/new/key.pem",
		TrustedCert:        "new-cert-hash",
		SetDNS:             true,
		SetRoutes:          true,
		HalfInternetRoutes: false,
		AutoReconnect:      false,
	}

	got := applyFormValues(current, form)

	want := &profile.Profile{
		ID:                 current.ID, // identity is never editor-controlled
		Name:               "new-name",
		Description:        "new-desc",
		Host:               "new.example.com",
		Port:               10443,
		AuthMethod:         profile.AuthMethodOTP, // password method + 2FA toggle
		Username:           "new-user",
		Realm:              "new-realm",
		TrustedCert:        "new-cert-hash",
		ClientCertPath:     "/new/cert.pem",
		ClientKeyPath:      "/new/key.pem",
		SetDNS:             true,
		SetRoutes:          true,
		HalfInternetRoutes: false,
		AutoReconnect:      false,
	}
	assert.Equal(t, want, got, "every field must be either editor-controlled or preserved from the stored profile")
}

// TestApplyFormValues_DoesNotMutateCurrent verifies the stored profile is left
// untouched: applyFormValues must return a copy so a later Cancel/reselect
// still sees the on-disk values.
func TestApplyFormValues_DoesNotMutateCurrent(t *testing.T) {
	current := &profile.Profile{
		ID:            "0d6db4a4-6d27-4626-a047-87a05a91c218",
		Name:          "original",
		AutoReconnect: true,
	}
	snapshot := *current

	_ = applyFormValues(current, editorFormValues{Name: "changed", AutoReconnect: false})

	assert.Equal(t, snapshot, *current, "applyFormValues must not mutate the input profile")
}

// TestApplyFormValues_RoundTripPreservesProfile verifies that loading a profile
// into form values and applying them back yields an identical profile — opening
// the editor and saving without touching anything must be a no-op.
func TestApplyFormValues_RoundTripPreservesProfile(t *testing.T) {
	methods := []profile.AuthMethod{
		profile.AuthMethodPassword,
		profile.AuthMethodOTP,
		profile.AuthMethodCertificate,
		profile.AuthMethodSAML,
	}

	// Exercise both boolean polarities so a round-trip that only works when
	// the flags happen to be true cannot slip through.
	for _, flags := range []bool{true, false} {
		for _, m := range methods {
			t.Run(fmt.Sprintf("%s/flags=%t", m, flags), func(t *testing.T) {
				current := &profile.Profile{
					ID:                 "0d6db4a4-6d27-4626-a047-87a05a91c218",
					Name:               "vpn",
					Description:        "desc",
					Host:               "vpn.example.com",
					Port:               443,
					AuthMethod:         m,
					Username:           "user",
					Realm:              "realm",
					TrustedCert:        "hash",
					ClientCertPath:     "/cert.pem",
					ClientKeyPath:      "/key.pem",
					SetDNS:             flags,
					SetRoutes:          flags,
					HalfInternetRoutes: flags,
					AutoReconnect:      flags,
				}

				index, otp := authMethodToSelection(m)
				form := editorFormValues{
					Name:               current.Name,
					Description:        current.Description,
					Host:               current.Host,
					Port:               current.Port,
					Realm:              current.Realm,
					Username:           current.Username,
					MethodIndex:        index,
					OTPEnabled:         otp,
					ClientCertPath:     current.ClientCertPath,
					ClientKeyPath:      current.ClientKeyPath,
					TrustedCert:        current.TrustedCert,
					SetDNS:             current.SetDNS,
					SetRoutes:          current.SetRoutes,
					HalfInternetRoutes: current.HalfInternetRoutes,
					AutoReconnect:      current.AutoReconnect,
				}

				assert.Equal(t, current, applyFormValues(current, form),
					"save-without-edits must not change the profile")
			})
		}
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
