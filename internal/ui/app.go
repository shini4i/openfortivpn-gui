// Package ui provides the GTK4/libadwaita user interface for openfortivpn-gui.
package ui

import (
	"context"
	"log/slog"
	"os/exec"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/shini4i/openfortivpn-gui/internal/config"
	"github.com/shini4i/openfortivpn-gui/internal/keyring"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

const (
	// AppID is the application identifier following reverse DNS notation.
	AppID = "com.github.shini4i.openfortivpn-gui"
)

// Version is the application version, set at build time via ldflags.
var Version = "dev"

// App represents the main application controller.
// It manages the GTK application lifecycle and wires together all components.
type App struct {
	app    *adw.Application
	window *MainWindow
	tray   *TrayIcon

	// Services
	configManager *config.Manager
	profileStore  *profile.Store
	keyringStore  keyring.Store
	vpnController *vpn.Controller

	// Notification manager
	notifier *Notifier

	// Application-level context for VPN operations
	ctx       context.Context
	ctxCancel context.CancelFunc
}

// AppConfig holds configuration for creating a new App instance.
type AppConfig struct {
	// OpenfortivpnPath is the path to the openfortivpn binary.
	OpenfortivpnPath string
}

// NewApp creates a new application instance with the given configuration.
func NewApp(cfg *AppConfig) (*App, error) {
	// Initialize config manager
	configManager, err := config.NewManager()
	if err != nil {
		return nil, err
	}

	// Initialize profile store
	profileStore, err := profile.NewStore(configManager.GetProfilesPath())
	if err != nil {
		return nil, err
	}

	// Initialize keyring store
	keyringStore := keyring.NewSystemKeyring()

	// Determine and validate openfortivpn path
	openfortivpnPath := cfg.OpenfortivpnPath
	if openfortivpnPath == "" {
		openfortivpnPath = "openfortivpn" // Use PATH lookup
	}

	// Validate the openfortivpn binary can be found
	resolvedPath, err := validateOpenfortivpnPath(openfortivpnPath)
	if err != nil {
		slog.Warn("openfortivpn not found", "path", openfortivpnPath, "error", err)
		// Continue anyway - user might install it later, or path might become valid
	} else {
		openfortivpnPath = resolvedPath
		slog.Debug("openfortivpn found", "path", openfortivpnPath)
	}

	// Initialize VPN controller
	vpnController := vpn.NewController(openfortivpnPath)

	// Create application-level context for VPN operations
	ctx, cancel := context.WithCancel(context.Background())

	app := &App{
		configManager: configManager,
		profileStore:  profileStore,
		keyringStore:  keyringStore,
		vpnController: vpnController,
		ctx:           ctx,
		ctxCancel:     cancel,
	}

	return app, nil
}

// Run starts the GTK application and blocks until it exits.
// Returns the exit code from the GTK application.
func (a *App) Run(args []string) int {
	a.app = adw.NewApplication(AppID, gio.ApplicationFlagsNone)

	a.app.ConnectActivate(func() {
		a.onActivate()
	})

	// Handle shutdown
	a.app.ConnectShutdown(func() {
		a.onShutdown()
	})

	return a.app.Run(args)
}

// onActivate is called when the application is activated.
// If profiles exist, the app starts in tray-only mode (window hidden).
// The window is always created to ensure VPN callbacks are registered.
func (a *App) onActivate() {
	// Register application actions
	a.registerActions()

	// Initialize notifier and sync enabled state from saved config
	if a.notifier == nil {
		a.notifier = NewNotifier(a.app)
		cfg := a.configManager.GetConfig()
		a.notifier.SetEnabled(cfg.ShowNotifications)
	}

	// Initialize system tray (before window so we can pass it)
	if a.tray == nil {
		a.initTray()
	}

	// Always create window to ensure VPN callbacks are registered
	// (callbacks handle tray updates, notifications, state display)
	a.ensureWindow()

	// Keep app running even when window is hidden (tray mode)
	a.app.Hold()

	// Check if profiles exist to determine startup mode
	if a.hasProfiles() {
		slog.Info("Starting in tray-only mode", "reason", "profiles exist")
		// Try auto-connect if enabled
		a.tryAutoConnect()
	} else {
		// First-time setup: show window for profile creation
		a.window.Present()
	}
}

// tryAutoConnect attempts to automatically connect to the default profile on startup.
// This is called only when AutoConnect is enabled and a default profile is configured.
func (a *App) tryAutoConnect() {
	cfg := a.configManager.GetConfig()
	if !cfg.AutoConnect {
		return
	}

	if cfg.DefaultProfileID == "" {
		slog.Debug("Auto-connect enabled but no default profile configured")
		return
	}

	// Verify the profile exists before scheduling connection
	if _, err := a.profileStore.Load(cfg.DefaultProfileID); err != nil {
		slog.Warn("Auto-connect profile not found", "profile_id", cfg.DefaultProfileID, "error", err)
		return
	}

	// Capture profile ID for closure to avoid race conditions
	profileID := cfg.DefaultProfileID
	slog.Info("Auto-connecting to default profile", "profile_id", profileID)

	// Select the profile and trigger connection
	// Using glib.IdleAdd to ensure GTK operations happen on main thread
	glib.IdleAdd(func() {
		if a.window != nil {
			a.window.selectProfileByID(profileID)
			a.window.triggerConnect()
		}
	})
}

// registerActions registers the application-level actions for menu items.
func (a *App) registerActions() {
	// About action
	aboutAction := gio.NewSimpleAction("about", nil)
	aboutAction.ConnectActivate(func(param *glib.Variant) {
		a.ShowAboutDialog()
	})
	a.app.AddAction(aboutAction)

	// Preferences action
	prefsAction := gio.NewSimpleAction("preferences", nil)
	prefsAction.ConnectActivate(func(param *glib.Variant) {
		a.ShowPreferencesDialog()
	})
	a.app.AddAction(prefsAction)

	// Quit action
	quitAction := gio.NewSimpleAction("quit", nil)
	quitAction.ConnectActivate(func(param *glib.Variant) {
		a.Quit()
	})
	a.app.AddAction(quitAction)

	// Register keyboard shortcuts
	a.registerAccelerators()
}

// registerAccelerators sets up keyboard shortcuts for common actions.
func (a *App) registerAccelerators() {
	// Quit: Ctrl+Q
	a.app.SetAccelsForAction("app.quit", []string{"<Control>q"})

	// Preferences: Ctrl+comma (standard GNOME shortcut)
	a.app.SetAccelsForAction("app.preferences", []string{"<Control>comma"})
}

// GetVPNController returns the VPN controller instance.
// This is useful for testing and external state monitoring.
func (a *App) GetVPNController() *vpn.Controller {
	return a.vpnController
}

// GetProfileStore returns the profile store instance.
func (a *App) GetProfileStore() profile.StoreInterface {
	return a.profileStore
}

// GetConfigManager returns the config manager instance.
func (a *App) GetConfigManager() *config.Manager {
	return a.configManager
}

// Quit terminates the application gracefully.
func (a *App) Quit() {
	if a.app != nil {
		a.app.Quit()
	}
}

// ShowAboutDialog displays the application's about dialog.
func (a *App) ShowAboutDialog() {
	a.ensureWindow()
	if a.window == nil {
		slog.Error("Window unexpectedly nil after creation", "action", "about_dialog")
		return
	}

	about := adw.NewAboutDialog()
	about.SetApplicationName("OpenFortiVPN GUI")
	about.SetApplicationIcon("network-vpn-symbolic")
	about.SetDeveloperName("shini4i")
	about.SetVersion(Version)
	about.SetWebsite("https://github.com/shini4i/openfortivpn-gui")
	about.SetIssueURL("https://github.com/shini4i/openfortivpn-gui/issues")
	about.SetLicenseType(gtk.LicenseGPL30)
	about.SetComments("A GTK4 GUI client for Fortinet SSL VPN")

	about.Present(a.window.window)
}

// ShowPreferencesDialog displays the application preferences window.
func (a *App) ShowPreferencesDialog() {
	a.ensureWindow()
	if a.window == nil {
		slog.Error("Window unexpectedly nil after creation", "action", "preferences_dialog")
		return
	}

	prefs := NewPreferencesWindow(a.window)

	// Load current values from config
	cfg := a.configManager.GetConfig()
	prefs.SetNotificationsEnabled(cfg.ShowNotifications)
	prefs.SetAutoConnect(cfg.AutoConnect)

	// Also sync notifier state with config value
	if a.notifier != nil {
		a.notifier.SetEnabled(cfg.ShowNotifications)
	}

	// Handle notification preference changes
	prefs.OnNotificationsChanged(func(enabled bool) {
		if a.notifier != nil {
			a.notifier.SetEnabled(enabled)
		}
		a.updateConfigField(func(cfg *config.Config) {
			cfg.ShowNotifications = enabled
		})
		slog.Info("Notifications setting changed", "enabled", enabled)
	})

	// Handle auto-connect preference changes
	prefs.OnAutoConnectChanged(func(enabled bool) {
		a.updateConfigField(func(cfg *config.Config) {
			cfg.AutoConnect = enabled
		})
		slog.Info("Auto-connect setting changed", "enabled", enabled)
	})

	prefs.Present()
}

// updateConfigField atomically updates a single config field and persists the change.
// The mutator function receives the current config and should modify the desired field.
// This uses UpdateField to avoid read-modify-write race conditions.
func (a *App) updateConfigField(mutator func(cfg *config.Config)) {
	if err := a.configManager.UpdateField(mutator); err != nil {
		slog.Error("Failed to persist config change", "error", err)
	}
}

// initTray initializes the system tray icon and its callbacks.
func (a *App) initTray() {
	a.tray = NewTrayIcon()

	// Set up tray callbacks (errors logged but not propagated - these are programmer errors
	// that should never occur since callbacks are always set before Run)
	if err := a.tray.OnConnect(func() {
		// Trigger connection from tray - window needed for callbacks but stays hidden
		glib.IdleAdd(func() {
			a.ensureWindow()
			if a.window == nil {
				slog.Error("Window unexpectedly nil after creation", "action", "tray_connect")
				return
			}
			a.window.triggerConnect()
		})
	}); err != nil {
		slog.Error("Failed to register tray OnConnect callback", "error", err)
	}

	if err := a.tray.OnDisconnect(func() {
		glib.IdleAdd(func() {
			// Disconnect doesn't require window, but ensure it exists for state display
			if a.window != nil {
				a.window.triggerDisconnect()
			} else {
				// Direct disconnect via controller when window doesn't exist
				if err := a.vpnController.Disconnect(); err != nil {
					slog.Error("Tray disconnect error", "error", err)
				}
			}
		})
	}); err != nil {
		slog.Error("Failed to register tray OnDisconnect callback", "error", err)
	}

	if err := a.tray.OnShow(func() {
		glib.IdleAdd(func() {
			a.ensureWindow()
			if a.window == nil {
				slog.Error("Window unexpectedly nil after creation", "action", "tray_show")
				return
			}
			a.window.Present()
		})
	}); err != nil {
		slog.Error("Failed to register tray OnShow callback", "error", err)
	}

	if err := a.tray.OnQuit(func() {
		glib.IdleAdd(func() {
			a.Quit()
		})
	}); err != nil {
		slog.Error("Failed to register tray OnQuit callback", "error", err)
	}

	// Start tray in background (error logged but not fatal - tray is optional)
	go func() {
		if err := a.tray.Run(); err != nil {
			slog.Error("Tray icon error", "error", err)
		}
	}()
}

// onShutdown handles application shutdown, cleaning up resources.
func (a *App) onShutdown() {
	slog.Info("Application shutting down")

	// Cancel the application context to signal VPN operations to terminate
	if a.ctxCancel != nil {
		a.ctxCancel()
	}

	// Disconnect VPN if connected
	state := a.vpnController.GetState()
	if state.CanDisconnect() {
		slog.Info("Disconnecting VPN before shutdown")
		if err := a.vpnController.Disconnect(); err != nil {
			slog.Error("Error disconnecting VPN", "error", err)
		}
	}

	// Stop system tray
	if a.tray != nil {
		a.tray.Quit()
	}

	slog.Info("Shutdown complete")
}

// hasProfiles checks if any VPN profiles are configured.
// Returns true if at least one profile exists, false otherwise.
func (a *App) hasProfiles() bool {
	result, err := a.profileStore.List()
	if err != nil {
		slog.Error("Error checking profiles for startup", "error", err)
		return false // Show window on error so user can see what's wrong
	}
	return len(result.Profiles) > 0
}

// ensureWindow creates the main window if it doesn't exist.
// This enables lazy window creation for tray-only startup.
// Note: NewMainWindow always returns a valid pointer, but callers check
// for nil as a defensive measure in case of GTK threading issues.
func (a *App) ensureWindow() {
	if a.window == nil {
		a.window = NewMainWindow(a.app, &MainWindowDeps{
			ProfileStore:  a.profileStore,
			KeyringStore:  a.keyringStore,
			VPNController: a.vpnController,
			ConfigManager: a.configManager,
			Tray:          a.tray,
			Notifier:      a.notifier,
			Ctx:           a.ctx,
		})

		// Register callback to track which profile is being connected to
		// This updates DefaultProfileID for auto-connect feature
		a.window.OnProfileConnecting(func(profileID string) {
			a.updateConfigField(func(cfg *config.Config) {
				cfg.DefaultProfileID = profileID
			})
			slog.Debug("Updated default profile for auto-connect", "profile_id", profileID)
		})
	}
}

// validateOpenfortivpnPath validates that the openfortivpn binary exists and is executable.
// If the path is not absolute, it attempts to resolve it using PATH lookup.
// Returns the resolved absolute path on success, or an error if the binary cannot be found.
func validateOpenfortivpnPath(path string) (string, error) {
	return exec.LookPath(path)
}
