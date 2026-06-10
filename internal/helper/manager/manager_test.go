// Package manager provides the VPN connection manager for the helper daemon.
package manager

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shini4i/openfortivpn-gui/internal/helper/protocol"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// TestValidateAndResolveFilePath tests the path validator which is critical
// for security. It ensures file paths are absolute, normalizes traversal
// sequences, and blocks access to sensitive system locations.
func TestValidateAndResolveFilePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantErr  string
		wantPath string
	}{
		{
			name:     "empty path - should pass (optional fields)",
			path:     "",
			wantPath: "",
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
			name:     "valid absolute path - should pass",
			path:     "/home/user/cert.pem",
			wantPath: "/home/user/cert.pem",
		},
		{
			name:     "valid absolute path with nested directories - should pass",
			path:     "/home/user/.config/openfortivpn/certs/client.pem",
			wantPath: "/home/user/.config/openfortivpn/certs/client.pem",
		},
		{
			name:     "root path - should pass",
			path:     "/cert.pem",
			wantPath: "/cert.pem",
		},
		{
			name:     "dot in filename - should pass",
			path:     "/home/user/cert.pem.bak",
			wantPath: "/home/user/cert.pem.bak",
		},
		{
			name:    "current directory reference - should fail",
			path:    "./cert.pem",
			wantErr: "path must be absolute",
		},
		{
			name:    "parent directory only - should fail (relative)",
			path:    "../cert.pem",
			wantErr: "path must be absolute",
		},
		{
			name:     "trailing slash on directory - should pass",
			path:     "/home/user/certs/",
			wantPath: "/home/user/certs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAndResolveFilePath(tt.path)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPath, got)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateAndResolveFilePath_Traversal verifies that ".." segments are
// normalized and the RESULT is policed: traversal into a sensitive location
// fails, while traversal that lands on a harmless path is equivalent to
// specifying that path directly (clients can already pass any absolute path).
//
// Regression note: the previous implementation rejected any path containing
// the substring "..", which also broke legitimate filenames like cert..pem.
func TestValidateAndResolveFilePath_Traversal(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantErr  string
		wantPath string
	}{
		{
			name:    "traversal to etc/passwd - sensitive, should fail",
			path:    "/home/../etc/passwd",
			wantErr: "sensitive system path",
		},
		{
			name:    "multiple traversal to etc/shadow - sensitive, should fail",
			path:    "/home/user/../../../etc/shadow",
			wantErr: "sensitive system path",
		},
		{
			name:    "traversal to root ssh keys - sensitive, should fail",
			path:    "/home/user/../../../../root/.ssh/id_rsa",
			wantErr: "sensitive system path",
		},
		{
			name:     "double dot in filename - legitimate name, should pass",
			path:     "/home/user/cert..pem",
			wantPath: "/home/user/cert..pem",
		},
		{
			name:     "traversal to harmless path - normalized and allowed",
			path:     "/home/user/../other/file.pem",
			wantPath: "/home/other/file.pem",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateAndResolveFilePath(tt.path)
			if tt.wantErr == "" {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPath, got)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateAndResolveFilePath_SymlinkProtection tests that symlinks to
// sensitive files are blocked and that allowed symlinks resolve to their
// TARGET — the resolved path is what gets handed to openfortivpn, closing
// the validate-then-swap TOCTOU window.
func TestValidateAndResolveFilePath_SymlinkProtection(t *testing.T) {
	// Create a temp directory for test symlinks
	tempDir := t.TempDir()

	// Create a regular file that should be allowed
	regularFile := filepath.Join(tempDir, "regular.pem")
	err := os.WriteFile(regularFile, []byte("test content"), 0600)
	require.NoError(t, err)

	// resolvedTempDir accounts for tempDir itself living behind a symlink
	// (e.g. /tmp -> /private/tmp), so target comparisons stay exact.
	resolvedTempDir, err := filepath.EvalSymlinks(tempDir)
	require.NoError(t, err)
	resolvedRegularFile := filepath.Join(resolvedTempDir, "regular.pem")

	// Test regular file - should pass and resolve to itself
	t.Run("regular file should pass", func(t *testing.T) {
		got, err := validateAndResolveFilePath(regularFile)
		require.NoError(t, err)
		assert.Equal(t, resolvedRegularFile, got)
	})

	// Test non-existent file - should pass (file may not exist yet)
	t.Run("non-existent file should pass with cleaned path", func(t *testing.T) {
		path := filepath.Join(tempDir, "nonexistent.pem")
		got, err := validateAndResolveFilePath(path)
		require.NoError(t, err)
		assert.Equal(t, path, got)
	})

	// Test symlink to regular file - should pass and return the TARGET
	t.Run("symlink to regular file resolves to target", func(t *testing.T) {
		symlinkPath := filepath.Join(tempDir, "symlink.pem")
		err := os.Symlink(regularFile, symlinkPath)
		require.NoError(t, err)

		got, err := validateAndResolveFilePath(symlinkPath)
		require.NoError(t, err)
		assert.Equal(t, resolvedRegularFile, got,
			"the resolved target, not the symlink, must be returned")
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

		_, err = validateAndResolveFilePath(symlinkPath)
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

		_, err = validateAndResolveFilePath(symlinkPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sensitive system path")
	})

	// Non-existent path normalizing into a sensitive location must still fail
	t.Run("non-existent path inside sensitive prefix should fail", func(t *testing.T) {
		_, err := validateAndResolveFilePath("/root/definitely-missing/cert.pem")
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

// --- Event scoping tests ---------------------------------------------------

// mockController implements vpn.VPNController for manager tests. It records
// the registered callbacks so tests can fire controller events directly.
type mockController struct {
	mu            sync.Mutex
	state         vpn.ConnectionState
	connectErr    error
	onStateChange func(old, new vpn.ConnectionState)
	onOutput      func(line string)
	onEvent       func(event *vpn.OutputEvent)
	onError       func(err error)
}

func newMockController() *mockController {
	return &mockController{state: vpn.StateDisconnected}
}

func (c *mockController) GetState() vpn.ConnectionState {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}
func (c *mockController) GetAssignedIP() string { return "" }
func (c *mockController) GetInterface() string  { return "" }
func (c *mockController) CanConnect() bool      { return c.GetState().CanConnect() }
func (c *mockController) CanDisconnect() bool   { return c.GetState().CanDisconnect() }
func (c *mockController) Connect(_ context.Context, _ *profile.Profile, _ *vpn.ConnectOptions) error {
	return c.connectErr
}
func (c *mockController) Disconnect(_ context.Context) error { return nil }
func (c *mockController) OnStateChange(cb func(old, new vpn.ConnectionState)) {
	c.onStateChange = cb
}
func (c *mockController) OnOutput(cb func(line string))           { c.onOutput = cb }
func (c *mockController) OnEvent(cb func(event *vpn.OutputEvent)) { c.onEvent = cb }
func (c *mockController) OnError(cb func(err error))              { c.onError = cb }

// recordingSender implements EventSender and records every delivery so tests
// can assert exactly which client received which event.
type recordingSender struct {
	mu         sync.Mutex
	broadcasts []*protocol.Event
	sent       map[string][]*protocol.Event
}

func newRecordingSender() *recordingSender {
	return &recordingSender{sent: make(map[string][]*protocol.Event)}
}

func (s *recordingSender) Broadcast(event *protocol.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.broadcasts = append(s.broadcasts, event)
}

func (s *recordingSender) SendToClient(clientID string, event *protocol.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent[clientID] = append(s.sent[clientID], event)
	return nil
}

func (s *recordingSender) sentTo(clientID string) []*protocol.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*protocol.Event(nil), s.sent[clientID]...)
}

func (s *recordingSender) broadcastCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.broadcasts)
}

// connectAs sends a valid connect request from the given client and requires success.
func connectAs(t *testing.T, m *Manager, clientID string) {
	t.Helper()
	params := protocol.ConnectParams{
		ProfileID:  "550e8400-e29b-41d4-a716-446655440000",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "user",
		AuthMethod: "password",
	}
	req, err := protocol.NewRequest("req-1", protocol.CommandConnect, params)
	require.NoError(t, err)

	resp := m.HandleRequest(req, clientID)
	require.True(t, resp.Success, "connect must succeed: %+v", resp.Error)
}

// TestEventScoping_OutputGoesOnlyToInitiator verifies that openfortivpn
// output — which can contain usernames and gateway responses — is delivered
// exclusively to the client that initiated the connection, never broadcast.
//
// Regression test: all events used to be broadcast to every socket peer, so
// any member of the openfortivpn-gui group could observe another member's
// connection output.
func TestEventScoping_OutputGoesOnlyToInitiator(t *testing.T) {
	ctrl := newMockController()
	sender := newRecordingSender()
	m := NewManagerWithController(ctrl, sender)

	connectAs(t, m, "client-A")

	ctrl.onOutput("DEBUG:  sensitive connection output")
	ctrl.onError(errors.New("auth failed for user"))
	ctrl.onEvent(&vpn.OutputEvent{Type: vpn.EventGotIP, Data: map[string]string{"ip": "10.0.0.2"}})

	assert.Len(t, sender.sentTo("client-A"), 3, "initiator must receive output, error, and vpn events")
	assert.Equal(t, 0, sender.broadcastCount(), "output/error/vpn events must never be broadcast")
	assert.Empty(t, sender.sentTo("client-B"), "bystander clients must receive nothing")
}

// TestEventScoping_StateChangeIsBroadcast verifies that coarse connection
// state remains public: every client needs it to render the correct UI state.
func TestEventScoping_StateChangeIsBroadcast(t *testing.T) {
	ctrl := newMockController()
	sender := newRecordingSender()
	m := NewManagerWithController(ctrl, sender)

	connectAs(t, m, "client-A")
	ctrl.onStateChange(vpn.StateDisconnected, vpn.StateConnecting)

	assert.Equal(t, 1, sender.broadcastCount(), "state changes must be broadcast to all clients")
	assert.Empty(t, sender.sentTo("client-A"), "state changes must not be duplicated as scoped sends")
}

// TestEventScoping_NoActiveClient_DropsPrivateEvents verifies that private
// events with no owning client are dropped rather than broadcast.
func TestEventScoping_NoActiveClient_DropsPrivateEvents(t *testing.T) {
	ctrl := newMockController()
	sender := newRecordingSender()
	m := NewManagerWithController(ctrl, sender)
	_ = m

	ctrl.onOutput("orphaned output line")
	ctrl.onError(errors.New("orphaned error"))

	assert.Equal(t, 0, sender.broadcastCount())
	assert.Empty(t, sender.sent)
}

// TestEventScoping_DisconnectedClearsActiveClient verifies the active client
// is forgotten once the connection ends, so later events cannot leak to the
// previous initiator.
func TestEventScoping_DisconnectedClearsActiveClient(t *testing.T) {
	ctrl := newMockController()
	sender := newRecordingSender()
	m := NewManagerWithController(ctrl, sender)

	connectAs(t, m, "client-A")
	ctrl.onStateChange(vpn.StateConnected, vpn.StateDisconnected)
	ctrl.onOutput("post-disconnect output")

	assert.Empty(t, sender.sentTo("client-A"), "no private events after disconnect")
}

// TestEventScoping_ConnectErrorClearsActiveClient verifies a failed connect
// does not leave a stale active client behind.
func TestEventScoping_ConnectErrorClearsActiveClient(t *testing.T) {
	ctrl := newMockController()
	ctrl.connectErr = errors.New("gateway unreachable")
	sender := newRecordingSender()
	m := NewManagerWithController(ctrl, sender)

	params := protocol.ConnectParams{
		ProfileID:  "550e8400-e29b-41d4-a716-446655440000",
		Host:       "vpn.example.com",
		Port:       443,
		Username:   "user",
		AuthMethod: "password",
	}
	req, err := protocol.NewRequest("req-1", protocol.CommandConnect, params)
	require.NoError(t, err)

	resp := m.HandleRequest(req, "client-A")
	require.False(t, resp.Success)

	ctrl.onOutput("output after failed connect")
	assert.Empty(t, sender.sentTo("client-A"))
}

// TestNewManagerWithController_NilSenderPanics verifies the nil-sender guard:
// silently dropping events would hide the client-scoping guarantees.
func TestNewManagerWithController_NilSenderPanics(t *testing.T) {
	assert.Panics(t, func() {
		NewManagerWithController(newMockController(), nil)
	})
}
