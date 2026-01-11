// Package config manages application-level configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// AppName is the application identifier used for XDG paths.
	AppName = "openfortivpn-gui"
	// ConfigFileName is the name of the main configuration file.
	ConfigFileName = "config.json"
	// ProfilesDirName is the name of the directory containing profile files.
	ProfilesDirName = "profiles"
)

// Config represents the application configuration.
type Config struct {
	DefaultProfileID      string `json:"default_profile_id,omitempty"`
	ReconnectDelaySeconds int    `json:"reconnect_delay_seconds"`
	MaxReconnectAttempts  int    `json:"max_reconnect_attempts"`
	ShowNotifications     bool   `json:"show_notifications"`
	AutoConnect           bool   `json:"auto_connect"`
	OpenFortiVPNPath      string `json:"openfortivpn_path"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ReconnectDelaySeconds: 5,
		MaxReconnectAttempts:  3,
		ShowNotifications:     true,
		AutoConnect:           false,
		OpenFortiVPNPath:      "/usr/bin/openfortivpn",
	}
}

// Paths holds the resolved configuration directories.
type Paths struct {
	ConfigDir   string
	ProfilesDir string
	ConfigFile  string
}

// GetPaths returns the configuration paths following XDG Base Directory spec.
func GetPaths() (*Paths, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		configHome = filepath.Join(homeDir, ".config")
	}

	configDir := filepath.Join(configHome, AppName)
	return &Paths{
		ConfigDir:   configDir,
		ProfilesDir: filepath.Join(configDir, ProfilesDirName),
		ConfigFile:  filepath.Join(configDir, ConfigFileName),
	}, nil
}

// EnsurePaths creates all necessary configuration directories.
func (p *Paths) EnsurePaths() error {
	if err := os.MkdirAll(p.ConfigDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	if err := os.MkdirAll(p.ProfilesDir, 0700); err != nil {
		return fmt.Errorf("failed to create profiles directory: %w", err)
	}
	return nil
}

// Load reads the configuration from disk.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// Save writes the configuration to disk using atomic write (write to temp, then rename).
func Save(path string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // Clean up temp file on failure
		return fmt.Errorf("failed to finalize config file: %w", err)
	}

	return nil
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.ReconnectDelaySeconds < 0 {
		return fmt.Errorf("reconnect delay must be non-negative")
	}
	if c.MaxReconnectAttempts < 0 {
		return fmt.Errorf("max reconnect attempts must be non-negative")
	}
	// OpenFortiVPNPath is validated at runtime (exec.LookPath in app.go)
	// but we ensure it's not empty as a basic sanity check
	if c.OpenFortiVPNPath == "" {
		return fmt.Errorf("openfortivpn path must not be empty")
	}
	return nil
}

// Manager provides high-level configuration management.
// It is safe for concurrent use from multiple goroutines.
type Manager struct {
	paths  *Paths       // Immutable after construction
	config *Config      // Protected by mu
	mu     sync.RWMutex // Protects config only
}

// NewManager creates a new configuration manager.
// It ensures all necessary directories exist and loads the configuration.
func NewManager() (*Manager, error) {
	paths, err := GetPaths()
	if err != nil {
		return nil, fmt.Errorf("failed to get config paths: %w", err)
	}

	if err := paths.EnsurePaths(); err != nil {
		return nil, fmt.Errorf("failed to create config directories: %w", err)
	}

	cfg, err := Load(paths.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return &Manager{
		paths:  paths,
		config: cfg,
	}, nil
}

// GetConfig returns a copy of the current configuration.
// The returned copy is safe to read without holding locks.
func (m *Manager) GetConfig() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	// Return a copy to prevent race conditions on the config fields
	cfg := *m.config
	return &cfg
}

// GetProfilesPath returns the path to the profiles directory.
func (m *Manager) GetProfilesPath() string {
	return m.paths.ProfilesDir
}

// GetConfigDir returns the path to the configuration directory.
func (m *Manager) GetConfigDir() string {
	return m.paths.ConfigDir
}

// SaveConfig saves the current configuration to disk.
func (m *Manager) SaveConfig() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return Save(m.paths.ConfigFile, m.config)
}

// UpdateConfig updates the configuration with a new value and saves it.
func (m *Manager) UpdateConfig(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := cfg.Validate(); err != nil {
		return err
	}
	m.config = cfg
	// Save directly without calling SaveConfig to avoid lock reentry
	return Save(m.paths.ConfigFile, m.config)
}

// UpdateField atomically updates a single config field using a mutator function.
// This avoids read-modify-write race conditions by holding the lock during the entire operation.
// If validation fails, the original config is preserved.
func (m *Manager) UpdateField(mutator func(cfg *Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a copy to apply mutation and validate before committing
	configCopy := *m.config
	mutator(&configCopy)
	if err := configCopy.Validate(); err != nil {
		return err
	}

	// Validation passed, apply the change
	*m.config = configCopy
	return Save(m.paths.ConfigFile, m.config)
}
