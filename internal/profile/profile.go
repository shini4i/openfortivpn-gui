// Package profile provides VPN profile management functionality.
package profile

import (
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
)

// AuthMethod represents the authentication method for a VPN profile.
type AuthMethod string

const (
	AuthMethodPassword    AuthMethod = "password"
	AuthMethodOTP         AuthMethod = "otp"
	AuthMethodCertificate AuthMethod = "certificate"
	AuthMethodSAML        AuthMethod = "saml"

	// Maximum lengths for text fields to prevent UI issues.
	maxNameLength        = 100
	maxDescriptionLength = 500

	// Limits for the fields handed to openfortivpn's argument vector, generous
	// on purpose: the point is to screen control characters and absurd input,
	// not to second-guess a gateway. --trusted-cert takes a SHA-256 digest (64
	// hex chars); 128 is headroom for a pasted colon-separated fingerprint.
	maxUsernameLength    = 256
	maxRealmLength       = 256
	maxTrustedCertLength = 128
)

// Profile represents a VPN connection configuration.
type Profile struct {
	ID                 string     `json:"id"`
	Name               string     `json:"name"`
	Description        string     `json:"description,omitempty"`
	Host               string     `json:"host"`
	Port               int        `json:"port"`
	AuthMethod         AuthMethod `json:"auth_method"`
	Username           string     `json:"username"`
	Realm              string     `json:"realm,omitempty"`
	TrustedCert        string     `json:"trusted_cert,omitempty"`
	ClientCertPath     string     `json:"client_cert_path,omitempty"`
	ClientKeyPath      string     `json:"client_key_path,omitempty"`
	SetDNS             bool       `json:"set_dns"`
	SetRoutes          bool       `json:"set_routes"`
	HalfInternetRoutes bool       `json:"half_internet_routes"`
	AutoReconnect      bool       `json:"auto_reconnect"`
}

// NewProfile creates a new profile with default values and a generated UUID.
func NewProfile(name string) *Profile {
	return &Profile{
		ID:            uuid.New().String(),
		Name:          name,
		Port:          443,
		AuthMethod:    AuthMethodPassword,
		SetDNS:        true,
		SetRoutes:     true,
		AutoReconnect: true,
	}
}

// Validate checks if the profile configuration is valid.
func (p *Profile) Validate() error {
	if p.ID == "" {
		return errors.New("profile ID is required")
	}

	if _, err := uuid.Parse(p.ID); err != nil {
		return fmt.Errorf("invalid profile ID format: %w", err)
	}

	if strings.TrimSpace(p.Name) == "" {
		return errors.New("profile name is required")
	}
	if err := validateTextInput(p.Name, "name", maxNameLength); err != nil {
		return err
	}

	// Description is optional, but validate if provided
	if p.Description != "" {
		if err := validateTextInput(p.Description, "description", maxDescriptionLength); err != nil {
			return err
		}
	}

	if strings.TrimSpace(p.Host) == "" {
		return errors.New("host is required")
	}

	// Validate host is either a valid hostname or IP address
	if err := validateHost(p.Host); err != nil {
		return err
	}

	if p.Port < 1 || p.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", p.Port)
	}

	// Screened for the same reasons as the fields above: these three reach
	// openfortivpn's argument vector, and the helper daemon relies on Validate
	// as the gate for parameters supplied over its socket.
	if err := validateTextInput(p.Username, "username", maxUsernameLength); err != nil {
		return err
	}
	if err := validateTextInput(p.Realm, "realm", maxRealmLength); err != nil {
		return err
	}
	if err := validateTextInput(p.TrustedCert, "trusted certificate", maxTrustedCertLength); err != nil {
		return err
	}

	switch p.AuthMethod {
	case AuthMethodPassword, AuthMethodOTP, AuthMethodSAML:
		// Username may be optional for SAML
		if p.AuthMethod != AuthMethodSAML && strings.TrimSpace(p.Username) == "" {
			return errors.New("username is required for password/OTP authentication")
		}
	case AuthMethodCertificate:
		if strings.TrimSpace(p.ClientCertPath) == "" {
			return errors.New("client certificate path is required for certificate authentication")
		}
		if strings.TrimSpace(p.ClientKeyPath) == "" {
			return errors.New("client key path is required for certificate authentication")
		}
	default:
		return fmt.Errorf("invalid authentication method: %s", p.AuthMethod)
	}

	return nil
}

// NeedsPassword reports whether the method requires a password from the user.
// Certificate auth proves identity with the key pair and SAML collects
// credentials in the browser, so neither has one to prompt for or to store.
func (m AuthMethod) NeedsPassword() bool {
	switch m {
	case AuthMethodPassword, AuthMethodOTP:
		return true
	default:
		return false
	}
}

// ValidAuthMethods returns all valid authentication methods.
func ValidAuthMethods() []AuthMethod {
	return []AuthMethod{
		AuthMethodPassword,
		AuthMethodOTP,
		AuthMethodCertificate,
		AuthMethodSAML,
	}
}

// validateHost validates that the host is a safe hostname or IP address.
// This prevents command injection and other security issues.
//
// Security model: this is a strict ALLOWLIST. A host is either a parseable IP
// address or an RFC 1123 hostname whose labels contain only [A-Za-z0-9-].
// Every shell metacharacter, quote, or whitespace byte is rejected by the
// per-label character check below, so no separate blacklist is needed.
func validateHost(host string) error {
	// Check for empty host
	if host == "" {
		return errors.New("host is required")
	}

	// Check for control characters and null bytes up front for a clearer error
	for _, r := range host {
		if r < 32 || r == 127 { // Control characters
			return errors.New("invalid host: contains control characters")
		}
	}

	// Try to parse as IP address first
	if net.ParseIP(host) != nil {
		return nil
	}

	// Validate as hostname according to RFC 1123
	if len(host) > 253 {
		return errors.New("invalid host: hostname too long (max 253 characters)")
	}

	// Hostname cannot start or end with a hyphen or dot
	if strings.HasPrefix(host, "-") || strings.HasSuffix(host, "-") {
		return errors.New("invalid host: hostname cannot start or end with hyphen")
	}
	if strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") {
		return errors.New("invalid host: hostname cannot start or end with dot")
	}

	// Check each label
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if len(label) == 0 {
			return errors.New("invalid host: empty label in hostname")
		}
		if len(label) > 63 {
			return errors.New("invalid host: label too long (max 63 characters)")
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return errors.New("invalid host: label cannot start or end with hyphen")
		}
		// Each character must be alphanumeric or hyphen
		for _, r := range label {
			isLower := r >= 'a' && r <= 'z'
			isUpper := r >= 'A' && r <= 'Z'
			isDigit := r >= '0' && r <= '9'
			isHyphen := r == '-'
			if !isLower && !isUpper && !isDigit && !isHyphen {
				return fmt.Errorf("invalid host: invalid character %q in hostname", r)
			}
		}
	}

	return nil
}

// validateTextInput validates a text field for control characters and length.
// This prevents GTK rendering issues with malicious Unicode and ensures
// reasonable field lengths.
func validateTextInput(value, fieldName string, maxLength int) error {
	if len(value) > maxLength {
		return fmt.Errorf("%s is too long (max %d characters)", fieldName, maxLength)
	}

	for i, r := range value {
		// Reject control characters (ASCII 0-31 and 127)
		// Allow common whitespace: space (32), but not tabs/newlines in single-line fields
		if r < 32 || r == 127 {
			return fmt.Errorf("%s contains invalid control character at position %d", fieldName, i)
		}
	}

	return nil
}
