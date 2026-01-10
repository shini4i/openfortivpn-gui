package profile

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProfile(t *testing.T) {
	p := NewProfile("Test VPN")

	assert.NotEmpty(t, p.ID)
	assert.Equal(t, "Test VPN", p.Name)
	assert.Equal(t, 443, p.Port)
	assert.Equal(t, AuthMethodPassword, p.AuthMethod)
	assert.True(t, p.SetDNS)
	assert.True(t, p.SetRoutes)
	assert.True(t, p.AutoReconnect)
}

func TestProfile_Validate(t *testing.T) {
	tests := []struct {
		name    string
		profile *Profile
		wantErr string
	}{
		{
			name: "valid password profile",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "",
		},
		{
			name: "valid SAML profile without username",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodSAML,
			},
			wantErr: "",
		},
		{
			name: "valid certificate profile",
			profile: &Profile{
				ID:             "550e8400-e29b-41d4-a716-446655440000",
				Name:           "Work VPN",
				Host:           "vpn.company.com",
				Port:           443,
				AuthMethod:     AuthMethodCertificate,
				ClientCertPath: "/path/to/cert.pem",
				ClientKeyPath:  "/path/to/key.pem",
			},
			wantErr: "",
		},
		{
			name: "valid profile with IP address",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "192.168.1.1",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "",
		},
		{
			name: "missing ID",
			profile: &Profile{
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "profile ID is required",
		},
		{
			name: "invalid ID format",
			profile: &Profile{
				ID:         "not-a-uuid",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid profile ID format",
		},
		{
			name: "missing name",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "profile name is required",
		},
		{
			name: "missing host",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "host is required",
		},
		{
			name: "invalid port - too low",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       0,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name: "invalid port - too high",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       70000,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "port must be between 1 and 65535",
		},
		{
			name: "missing username for password auth",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
			},
			wantErr: "username is required for password/OTP authentication",
		},
		{
			name: "missing cert path for certificate auth",
			profile: &Profile{
				ID:            "550e8400-e29b-41d4-a716-446655440000",
				Name:          "Work VPN",
				Host:          "vpn.company.com",
				Port:          443,
				AuthMethod:    AuthMethodCertificate,
				ClientKeyPath: "/path/to/key.pem",
			},
			wantErr: "client certificate path is required",
		},
		{
			name: "missing key path for certificate auth",
			profile: &Profile{
				ID:             "550e8400-e29b-41d4-a716-446655440000",
				Name:           "Work VPN",
				Host:           "vpn.company.com",
				Port:           443,
				AuthMethod:     AuthMethodCertificate,
				ClientCertPath: "/path/to/cert.pem",
			},
			wantErr: "client key path is required",
		},
		{
			name: "invalid auth method",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: "invalid",
				Username:   "john.doe",
			},
			wantErr: "invalid authentication method",
		},
		{
			name: "invalid host - contains shell metacharacter semicolon",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.com;rm -rf /",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: contains forbidden character",
		},
		{
			name: "invalid host - contains pipe",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.com|cat /etc/passwd",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: contains forbidden character",
		},
		{
			name: "invalid host - contains newline",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn.com\nmalicious",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: contains control characters", // newline is ASCII 10, caught by control char check
		},
		{
			name: "invalid host - starts with hyphen",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "-vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: hostname cannot start or end with hyphen",
		},
		{
			name: "invalid host - contains space",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: contains forbidden character",
		},
		{
			name: "invalid host - control character",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn\x00.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: contains control characters",
		},
		{
			name: "valid host - IPv6 address",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "2001:db8::1",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "",
		},
		{
			name: "invalid host - hostname too long",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "a." + string(make([]byte, 254)), // Create hostname > 253 chars
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host",
		},
		{
			name: "invalid host - label too long",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       string(make([]byte, 64)) + ".com", // Label > 63 chars
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host",
		},
		{
			name: "invalid host - empty label",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work VPN",
				Host:       "vpn..company.com", // Empty label between dots
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "invalid host: empty label",
		},
		{
			name: "invalid name - whitespace only",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "   ",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "profile name is required",
		},
		{
			name: "invalid name - control character",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       "Work\x00VPN",
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "name contains invalid control character",
		},
		{
			name: "invalid name - too long",
			profile: &Profile{
				ID:         "550e8400-e29b-41d4-a716-446655440000",
				Name:       strings.Repeat("a", 101),
				Host:       "vpn.company.com",
				Port:       443,
				AuthMethod: AuthMethodPassword,
				Username:   "john.doe",
			},
			wantErr: "name is too long",
		},
		{
			name: "invalid description - control character",
			profile: &Profile{
				ID:          "550e8400-e29b-41d4-a716-446655440000",
				Name:        "Work VPN",
				Description: "My\tVPN",
				Host:        "vpn.company.com",
				Port:        443,
				AuthMethod:  AuthMethodPassword,
				Username:    "john.doe",
			},
			wantErr: "description contains invalid control character",
		},
		{
			name: "invalid description - too long",
			profile: &Profile{
				ID:          "550e8400-e29b-41d4-a716-446655440000",
				Name:        "Work VPN",
				Description: strings.Repeat("a", 501),
				Host:        "vpn.company.com",
				Port:        443,
				AuthMethod:  AuthMethodPassword,
				Username:    "john.doe",
			},
			wantErr: "description is too long",
		},
		{
			name: "valid profile with description",
			profile: &Profile{
				ID:          "550e8400-e29b-41d4-a716-446655440000",
				Name:        "Work VPN",
				Description: "My work VPN connection",
				Host:        "vpn.company.com",
				Port:        443,
				AuthMethod:  AuthMethodPassword,
				Username:    "john.doe",
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.profile.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestValidAuthMethods(t *testing.T) {
	methods := ValidAuthMethods()

	assert.Len(t, methods, 4)
	assert.Contains(t, methods, AuthMethodPassword)
	assert.Contains(t, methods, AuthMethodOTP)
	assert.Contains(t, methods, AuthMethodCertificate)
	assert.Contains(t, methods, AuthMethodSAML)
}
