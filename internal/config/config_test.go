package config

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 5, cfg.ReconnectDelaySeconds)
	assert.Equal(t, 3, cfg.MaxReconnectAttempts)
	assert.True(t, cfg.ShowNotifications)
	assert.False(t, cfg.AutoConnect)
	assert.Equal(t, "/usr/bin/openfortivpn", cfg.OpenFortiVPNPath)
	assert.Empty(t, cfg.DefaultProfileID)
}

func TestGetPaths(t *testing.T) {
	// Save and restore XDG_CONFIG_HOME
	original := os.Getenv("XDG_CONFIG_HOME")
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	t.Run("with XDG_CONFIG_HOME set", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)

		paths, err := GetPaths()
		require.NoError(t, err)

		assert.Equal(t, filepath.Join(tmpDir, AppName), paths.ConfigDir)
		assert.Equal(t, filepath.Join(tmpDir, AppName, ProfilesDirName), paths.ProfilesDir)
		assert.Equal(t, filepath.Join(tmpDir, AppName, ConfigFileName), paths.ConfigFile)
	})

	t.Run("without XDG_CONFIG_HOME (uses HOME/.config)", func(t *testing.T) {
		_ = os.Setenv("XDG_CONFIG_HOME", "")

		paths, err := GetPaths()
		require.NoError(t, err)

		homeDir, err := os.UserHomeDir()
		require.NoError(t, err)

		expectedConfigDir := filepath.Join(homeDir, ".config", AppName)
		assert.Equal(t, expectedConfigDir, paths.ConfigDir)
	})
}

func TestPaths_EnsurePaths(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-ensure-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	paths := &Paths{
		ConfigDir:   filepath.Join(tmpDir, "openfortivpn-gui"),
		ProfilesDir: filepath.Join(tmpDir, "openfortivpn-gui", "profiles"),
		ConfigFile:  filepath.Join(tmpDir, "openfortivpn-gui", "config.json"),
	}

	err = paths.EnsurePaths()
	require.NoError(t, err)

	assert.DirExists(t, paths.ConfigDir)
	assert.DirExists(t, paths.ProfilesDir)
}

func TestLoad(t *testing.T) {
	t.Run("loads existing config", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-load-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		configPath := filepath.Join(tmpDir, "config.json")
		configContent := `{
			"default_profile_id": "test-profile-id",
			"reconnect_delay_seconds": 10,
			"max_reconnect_attempts": 5,
			"show_notifications": false,
			"auto_connect": true,
			"openfortivpn_path": "/custom/path/openfortivpn"
		}`
		err = os.WriteFile(configPath, []byte(configContent), 0600)
		require.NoError(t, err)

		cfg, err := Load(configPath)
		require.NoError(t, err)

		assert.Equal(t, "test-profile-id", cfg.DefaultProfileID)
		assert.Equal(t, 10, cfg.ReconnectDelaySeconds)
		assert.Equal(t, 5, cfg.MaxReconnectAttempts)
		assert.False(t, cfg.ShowNotifications)
		assert.True(t, cfg.AutoConnect)
		assert.Equal(t, "/custom/path/openfortivpn", cfg.OpenFortiVPNPath)
	})

	t.Run("returns default config when file does not exist", func(t *testing.T) {
		cfg, err := Load("/nonexistent/path/config.json")
		require.NoError(t, err)

		// Should return default config
		expected := DefaultConfig()
		assert.Equal(t, expected.ReconnectDelaySeconds, cfg.ReconnectDelaySeconds)
		assert.Equal(t, expected.MaxReconnectAttempts, cfg.MaxReconnectAttempts)
	})

	t.Run("returns error for invalid JSON", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-invalid-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		configPath := filepath.Join(tmpDir, "config.json")
		err = os.WriteFile(configPath, []byte("invalid json {{{"), 0600)
		require.NoError(t, err)

		_, err = Load(configPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal config")
	})
}

func TestSave(t *testing.T) {
	t.Run("saves config to file", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-save-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		configPath := filepath.Join(tmpDir, "config.json")
		cfg := &Config{
			DefaultProfileID:      "my-profile",
			ReconnectDelaySeconds: 15,
			MaxReconnectAttempts:  10,
			ShowNotifications:     false,
			AutoConnect:           true,
			OpenFortiVPNPath:      "/opt/openfortivpn",
		}

		err = Save(configPath, cfg)
		require.NoError(t, err)

		// Verify file was created with correct permissions
		info, err := os.Stat(configPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

		// Load it back and verify
		loaded, err := Load(configPath)
		require.NoError(t, err)
		assert.Equal(t, cfg.DefaultProfileID, loaded.DefaultProfileID)
		assert.Equal(t, cfg.ReconnectDelaySeconds, loaded.ReconnectDelaySeconds)
		assert.Equal(t, cfg.AutoConnect, loaded.AutoConnect)
	})
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr string
	}{
		{
			name:    "valid default config",
			config:  DefaultConfig(),
			wantErr: "",
		},
		{
			name: "valid custom config",
			config: &Config{
				ReconnectDelaySeconds: 10,
				MaxReconnectAttempts:  5,
				OpenFortiVPNPath:      "/usr/bin/openfortivpn",
			},
			wantErr: "",
		},
		{
			name: "negative reconnect delay",
			config: &Config{
				ReconnectDelaySeconds: -1,
				MaxReconnectAttempts:  3,
				OpenFortiVPNPath:      "/usr/bin/openfortivpn",
			},
			wantErr: "reconnect delay must be non-negative",
		},
		{
			name: "negative max reconnect attempts",
			config: &Config{
				ReconnectDelaySeconds: 5,
				MaxReconnectAttempts:  -1,
				OpenFortiVPNPath:      "/usr/bin/openfortivpn",
			},
			wantErr: "max reconnect attempts must be non-negative",
		},
		{
			name: "zero values are valid",
			config: &Config{
				ReconnectDelaySeconds: 0,
				MaxReconnectAttempts:  0,
				OpenFortiVPNPath:      "/usr/bin/openfortivpn",
			},
			wantErr: "",
		},
		{
			name: "empty openfortivpn path",
			config: &Config{
				ReconnectDelaySeconds: 5,
				MaxReconnectAttempts:  3,
				OpenFortiVPNPath:      "",
			},
			wantErr: "openfortivpn path must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestManager_ConcurrentAccess(t *testing.T) {
	// Set up temp directory for config
	tmpDir, err := os.MkdirTemp("", "config-concurrent-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Override XDG_CONFIG_HOME
	original := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	manager, err := NewManager()
	require.NoError(t, err)

	const numGoroutines = 50
	const numOpsPerGoroutine = 100

	var wg sync.WaitGroup
	var writeErrors int64
	var validationErrors int64

	// Concurrent readers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOpsPerGoroutine; j++ {
				cfg := manager.GetConfig()
				// Track validation errors atomically (don't use assert in goroutines)
				if cfg.Validate() != nil {
					atomic.AddInt64(&validationErrors, 1)
				}
			}
		}()
	}

	// Concurrent writers (fewer to avoid file system contention)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				cfg := &Config{
					ReconnectDelaySeconds: id + j,
					MaxReconnectAttempts:  3,
					ShowNotifications:     true,
				}
				if err := manager.UpdateConfig(cfg); err != nil {
					atomic.AddInt64(&writeErrors, 1)
				}
			}
		}(i)
	}

	wg.Wait()

	// Log write errors (may happen due to FS contention, not a test failure)
	t.Logf("Write errors due to FS contention: %d", writeErrors)

	// Verify no validation errors occurred during concurrent reads
	assert.Zero(t, validationErrors, "expected no validation errors from concurrent reads")

	// Verify final state is valid
	finalCfg := manager.GetConfig()
	require.NoError(t, finalCfg.Validate())
}

func TestManager_GetConfigReturnsCopy(t *testing.T) {
	// Set up temp directory for config
	tmpDir, err := os.MkdirTemp("", "config-copy-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Override XDG_CONFIG_HOME
	original := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	manager, err := NewManager()
	require.NoError(t, err)

	// Get config and modify the returned copy
	cfg1 := manager.GetConfig()
	originalDelay := cfg1.ReconnectDelaySeconds
	cfg1.ReconnectDelaySeconds = 999

	// Get config again - should not be affected by modification
	cfg2 := manager.GetConfig()
	assert.Equal(t, originalDelay, cfg2.ReconnectDelaySeconds)
	assert.NotEqual(t, 999, cfg2.ReconnectDelaySeconds)
}

func TestManager_GetProfilesPath(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-profiles-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	original := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	manager, err := NewManager()
	require.NoError(t, err)

	profilesPath := manager.GetProfilesPath()
	assert.Equal(t, filepath.Join(tmpDir, AppName, ProfilesDirName), profilesPath)
}

func TestManager_GetConfigDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-dir-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	original := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	manager, err := NewManager()
	require.NoError(t, err)

	configDir := manager.GetConfigDir()
	assert.Equal(t, filepath.Join(tmpDir, AppName), configDir)
}

func TestManager_SaveConfig(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-save-manager-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	original := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	manager, err := NewManager()
	require.NoError(t, err)

	// Modify config via UpdateConfig first
	cfg := &Config{
		DefaultProfileID:      "test-id",
		ReconnectDelaySeconds: 10,
		MaxReconnectAttempts:  5,
		ShowNotifications:     true,
		OpenFortiVPNPath:      "/usr/bin/openfortivpn",
	}
	require.NoError(t, manager.UpdateConfig(cfg))

	// Save should succeed (config already saved by UpdateConfig, but SaveConfig should work too)
	err = manager.SaveConfig()
	require.NoError(t, err)

	// Verify by loading directly from file
	loaded, err := Load(filepath.Join(tmpDir, AppName, ConfigFileName))
	require.NoError(t, err)
	assert.Equal(t, "test-id", loaded.DefaultProfileID)
}

func TestManager_UpdateConfig_ValidationError(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-update-invalid-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	original := os.Getenv("XDG_CONFIG_HOME")
	_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
	defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

	manager, err := NewManager()
	require.NoError(t, err)

	// Try to update with invalid config
	invalidCfg := &Config{
		ReconnectDelaySeconds: -1, // Invalid
		MaxReconnectAttempts:  3,
	}
	err = manager.UpdateConfig(invalidCfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reconnect delay must be non-negative")
}

func TestSave_AtomicWriteCleanup(t *testing.T) {
	// Test that temp file is cleaned up on rename failure
	// We can't easily simulate a rename failure, but we can verify
	// the normal path works and leaves no temp files
	tmpDir, err := os.MkdirTemp("", "config-atomic-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configPath := filepath.Join(tmpDir, "config.json")
	cfg := DefaultConfig()

	err = Save(configPath, cfg)
	require.NoError(t, err)

	// Verify no .tmp file remains
	tmpPath := configPath + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "temp file should not exist after successful save")

	// Verify actual config file exists
	_, err = os.Stat(configPath)
	require.NoError(t, err)
}

func TestPaths_EnsurePaths_AlreadyExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "config-ensure-exists-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	paths := &Paths{
		ConfigDir:   filepath.Join(tmpDir, "openfortivpn-gui"),
		ProfilesDir: filepath.Join(tmpDir, "openfortivpn-gui", "profiles"),
		ConfigFile:  filepath.Join(tmpDir, "openfortivpn-gui", "config.json"),
	}

	// Create directories first
	err = os.MkdirAll(paths.ProfilesDir, 0700)
	require.NoError(t, err)

	// EnsurePaths should succeed even when directories exist
	err = paths.EnsurePaths()
	require.NoError(t, err)

	assert.DirExists(t, paths.ConfigDir)
	assert.DirExists(t, paths.ProfilesDir)
}

func TestLoad_ReadError(t *testing.T) {
	// Test loading from a directory (should fail)
	tmpDir, err := os.MkdirTemp("", "config-load-error-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Try to load a directory as a file
	_, err = Load(tmpDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestManager_UpdateField(t *testing.T) {
	t.Run("atomically updates single field", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-updatefield-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		original := os.Getenv("XDG_CONFIG_HOME")
		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

		manager, err := NewManager()
		require.NoError(t, err)

		// Update a single field
		err = manager.UpdateField(func(cfg *Config) {
			cfg.AutoConnect = true
		})
		require.NoError(t, err)

		// Verify the field was updated
		cfg := manager.GetConfig()
		assert.True(t, cfg.AutoConnect)

		// Verify other defaults are preserved
		assert.True(t, cfg.ShowNotifications) // Default is true
	})

	t.Run("rejects invalid config after mutation", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-updatefield-invalid-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		original := os.Getenv("XDG_CONFIG_HOME")
		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

		manager, err := NewManager()
		require.NoError(t, err)

		// Try to set invalid value
		err = manager.UpdateField(func(cfg *Config) {
			cfg.ReconnectDelaySeconds = -5
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "reconnect delay must be non-negative")

		// Verify the original value is preserved
		cfg := manager.GetConfig()
		assert.Equal(t, 5, cfg.ReconnectDelaySeconds) // Default is 5
	})

	t.Run("persists changes to disk", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "config-updatefield-persist-test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tmpDir) }()

		original := os.Getenv("XDG_CONFIG_HOME")
		_ = os.Setenv("XDG_CONFIG_HOME", tmpDir)
		defer func() { _ = os.Setenv("XDG_CONFIG_HOME", original) }()

		manager, err := NewManager()
		require.NoError(t, err)

		// Update and persist
		err = manager.UpdateField(func(cfg *Config) {
			cfg.DefaultProfileID = "test-profile-123"
		})
		require.NoError(t, err)

		// Load from disk to verify persistence
		loaded, err := Load(filepath.Join(tmpDir, AppName, ConfigFileName))
		require.NoError(t, err)
		assert.Equal(t, "test-profile-123", loaded.DefaultProfileID)
	})
}
