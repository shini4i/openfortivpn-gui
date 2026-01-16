// Package manager provides the VPN connection manager for the helper daemon.
package manager

import (
	"os"
	"path/filepath"
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

// TestValidateFilePath_SymlinkProtection tests that symlinks to sensitive files are blocked.
func TestValidateFilePath_SymlinkProtection(t *testing.T) {
	// Create a temp directory for test symlinks
	tempDir := t.TempDir()

	// Create a regular file that should be allowed
	regularFile := filepath.Join(tempDir, "regular.pem")
	err := os.WriteFile(regularFile, []byte("test content"), 0600)
	require.NoError(t, err)

	// Test regular file - should pass
	t.Run("regular file should pass", func(t *testing.T) {
		err := validateFilePath(regularFile)
		require.NoError(t, err)
	})

	// Test non-existent file - should pass (file may not exist yet)
	t.Run("non-existent file should pass", func(t *testing.T) {
		err := validateFilePath(filepath.Join(tempDir, "nonexistent.pem"))
		require.NoError(t, err)
	})

	// Test symlink to regular file in same directory - should pass
	t.Run("symlink to regular file should pass", func(t *testing.T) {
		symlinkPath := filepath.Join(tempDir, "symlink.pem")
		err := os.Symlink(regularFile, symlinkPath)
		require.NoError(t, err)

		err = validateFilePath(symlinkPath)
		require.NoError(t, err)
	})

	// Test symlink to /etc/passwd (readable by all users) - should fail
	t.Run("symlink to sensitive file should fail", func(t *testing.T) {
		// /etc/passwd is readable by all users, so we can test symlink resolution
		// without needing root privileges
		if _, err := os.Stat("/etc/passwd"); os.IsNotExist(err) {
			t.Skip("/etc/passwd not available")
		}

		symlinkPath := filepath.Join(tempDir, "passwd_link.pem")
		err := os.Symlink("/etc/passwd", symlinkPath)
		require.NoError(t, err)

		err = validateFilePath(symlinkPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sensitive system path")
	})

	// Test symlink to /proc/self/environ - should fail
	t.Run("symlink to /proc should fail", func(t *testing.T) {
		if _, err := os.Stat("/proc/self/environ"); os.IsNotExist(err) {
			t.Skip("/proc not available")
		}

		symlinkPath := filepath.Join(tempDir, "proc_link.pem")
		err := os.Symlink("/proc/self/environ", symlinkPath)
		require.NoError(t, err)

		err = validateFilePath(symlinkPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sensitive system path")
	})
}

// TestIsSensitivePath tests the sensitive path detection.
func TestIsSensitivePath(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		sensitive bool
	}{
		{
			name:      "/etc/shadow is sensitive",
			path:      "/etc/shadow",
			sensitive: true,
		},
		{
			name:      "/etc/passwd is sensitive",
			path:      "/etc/passwd",
			sensitive: true,
		},
		{
			name:      "/etc/sudoers is sensitive",
			path:      "/etc/sudoers",
			sensitive: true,
		},
		{
			name:      "/etc/group is sensitive",
			path:      "/etc/group",
			sensitive: true,
		},
		{
			name:      "/etc/gshadow is sensitive",
			path:      "/etc/gshadow",
			sensitive: true,
		},
		{
			name:      "/etc/ssh/ directory is sensitive",
			path:      "/etc/ssh/sshd_config",
			sensitive: true,
		},
		{
			name:      "/etc/security/ directory is sensitive",
			path:      "/etc/security/access.conf",
			sensitive: true,
		},
		{
			name:      "/etc/pam.d/ directory is sensitive",
			path:      "/etc/pam.d/common-auth",
			sensitive: true,
		},
		{
			name:      "/etc/krb5.keytab is sensitive",
			path:      "/etc/krb5.keytab",
			sensitive: true,
		},
		{
			name:      "/root/ directory is sensitive",
			path:      "/root/.bashrc",
			sensitive: true,
		},
		{
			name:      "/proc/ is sensitive",
			path:      "/proc/self/environ",
			sensitive: true,
		},
		{
			name:      "/sys/ is sensitive",
			path:      "/sys/kernel/version",
			sensitive: true,
		},
		{
			name:      "/dev/ is sensitive",
			path:      "/dev/sda",
			sensitive: true,
		},
		{
			name:      "/boot/ is sensitive",
			path:      "/boot/grub/grub.cfg",
			sensitive: true,
		},
		{
			name:      "/var/lib/secrets/ is sensitive",
			path:      "/var/lib/secrets/myapp.key",
			sensitive: true,
		},
		{
			name:      "/var/log/ is sensitive",
			path:      "/var/log/auth.log",
			sensitive: true,
		},
		{
			name:      "regular /home path is not sensitive",
			path:      "/home/user/cert.pem",
			sensitive: false,
		},
		{
			name:      "/etc/ssl path is not sensitive",
			path:      "/etc/ssl/certs/ca-certificates.crt",
			sensitive: false,
		},
		{
			name:      "/etc/pki path is not sensitive",
			path:      "/etc/pki/tls/certs/cert.pem",
			sensitive: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSensitivePath(tt.path)
			assert.Equal(t, tt.sensitive, result)
		})
	}
}

// TestResolvePathSafely tests the path resolution function.
func TestResolvePathSafely(t *testing.T) {
	tempDir := t.TempDir()

	// Create a regular file
	regularFile := filepath.Join(tempDir, "regular.txt")
	err := os.WriteFile(regularFile, []byte("test"), 0600)
	require.NoError(t, err)

	// Create a symlink to the regular file
	symlinkFile := filepath.Join(tempDir, "symlink.txt")
	err = os.Symlink(regularFile, symlinkFile)
	require.NoError(t, err)

	t.Run("resolves regular file", func(t *testing.T) {
		resolved, err := resolvePathSafely(regularFile)
		require.NoError(t, err)
		assert.Equal(t, regularFile, resolved)
	})

	t.Run("resolves symlink to target", func(t *testing.T) {
		resolved, err := resolvePathSafely(symlinkFile)
		require.NoError(t, err)
		assert.Equal(t, regularFile, resolved)
	})

	t.Run("resolves chained symlinks", func(t *testing.T) {
		// Create an intermediate symlink pointing to regularFile
		intermediateLink := filepath.Join(tempDir, "intermediate.txt")
		err := os.Symlink(regularFile, intermediateLink)
		require.NoError(t, err)

		// Create a chained symlink pointing to the intermediate symlink
		chainedLink := filepath.Join(tempDir, "chained.txt")
		err = os.Symlink(intermediateLink, chainedLink)
		require.NoError(t, err)

		// Verify that resolvePathSafely follows the entire chain
		resolved, err := resolvePathSafely(chainedLink)
		require.NoError(t, err)
		assert.Equal(t, regularFile, resolved)
	})

	t.Run("returns error for non-existent file", func(t *testing.T) {
		_, err := resolvePathSafely(filepath.Join(tempDir, "nonexistent.txt"))
		require.Error(t, err)
		assert.True(t, os.IsNotExist(err))
	})
}
