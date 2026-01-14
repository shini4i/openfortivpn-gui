// Package manager provides the VPN connection manager for the helper daemon.
package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateFilePath tests the validateFilePath function which is critical for security.
// It prevents path traversal attacks by ensuring file paths are absolute and don't contain
// directory traversal sequences.
func TestValidateFilePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "empty path - should pass (optional fields)",
			path:    "",
			wantErr: "",
		},
		{
			name:    "relative path - should fail",
			path:    "cert.pem",
			wantErr: "path must be absolute",
		},
		{
			name:    "relative path with subdirectory - should fail",
			path:    "certs/cert.pem",
			wantErr: "path must be absolute",
		},
		{
			name:    "valid absolute path - should pass",
			path:    "/home/user/cert.pem",
			wantErr: "",
		},
		{
			name:    "valid absolute path with nested directories - should pass",
			path:    "/home/user/.config/openfortivpn/certs/client.pem",
			wantErr: "",
		},
		{
			name:    "root path - should pass",
			path:    "/cert.pem",
			wantErr: "",
		},
		{
			name:    "dot in filename - should pass",
			path:    "/home/user/cert.pem.bak",
			wantErr: "",
		},
		{
			name:    "current directory reference - should fail",
			path:    "./cert.pem",
			wantErr: "path must be absolute",
		},
		{
			name:    "parent directory only - should fail",
			path:    "../cert.pem",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "trailing slash on directory - should pass",
			path:    "/home/user/certs/",
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilePath(tt.path)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateFilePath_PathTraversal tests path traversal detection.
// The implementation checks for ".." in the ORIGINAL path before cleaning,
// which correctly blocks path traversal attempts.
func TestValidateFilePath_PathTraversal(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr string
	}{
		{
			name:    "traversal to etc/passwd - should fail",
			path:    "/home/../etc/passwd",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "multiple traversal - should fail",
			path:    "/home/user/../../../etc/shadow",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "traversal to root ssh keys - should fail",
			path:    "/home/user/../../../../root/.ssh/id_rsa",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "double dot in filename - false positive but safe",
			path:    "/home/user/cert..pem",
			wantErr: "path traversal not allowed",
		},
		{
			name:    "embedded traversal - should fail",
			path:    "/home/user/../other/file.pem",
			wantErr: "path traversal not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFilePath(tt.path)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
