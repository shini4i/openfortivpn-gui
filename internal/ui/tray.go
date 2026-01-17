// Package ui provides the GTK4/libadwaita user interface for openfortivpn-gui.
package ui

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"fyne.io/systray"

	"github.com/shini4i/openfortivpn-gui/internal/stats"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

var (
	// ErrTrayAlreadyRunning is returned when attempting to modify callbacks after Run() has been called.
	ErrTrayAlreadyRunning = errors.New("cannot modify callbacks after TrayIcon.Run() is called")
	// ErrTrayRunTwice is returned when Run() is called more than once.
	ErrTrayRunTwice = errors.New("TrayIcon.Run() called twice")
	// ErrTrayMissingCallbacks is returned when Run() is called without all required callbacks set.
	ErrTrayMissingCallbacks = errors.New("all callbacks (OnConnect, OnDisconnect, OnShow, OnQuit) must be set before calling Run()")
)

// TrayIcon manages the system tray icon and menu.
type TrayIcon struct {
	mu sync.RWMutex

	// State
	state       vpn.ConnectionState
	profileName string

	// Menu items
	menuStatus      *systray.MenuItem
	menuTrafficRate *systray.MenuItem
	menuConnect     *systray.MenuItem
	menuDisconnect  *systray.MenuItem
	menuShow        *systray.MenuItem
	menuQuit        *systray.MenuItem

	// Callbacks - must be set before Run() is called
	onConnect    func()
	onDisconnect func()
	onShow       func()
	onQuit       func()

	// Icons (set once in NewTrayIcon, read-only after initialization)
	iconDisconnected []byte
	iconConnecting   []byte
	iconConnected    []byte

	// Done channel to signal goroutine termination
	done chan struct{}

	// Lifecycle flags
	running   bool
	closeOnce sync.Once
}

// NewTrayIcon creates a new system tray icon manager.
func NewTrayIcon() *TrayIcon {
	return &TrayIcon{
		state:            vpn.StateDisconnected,
		iconDisconnected: iconDisconnectedPNG,
		iconConnecting:   iconConnectingPNG,
		iconConnected:    iconConnectedPNG,
		done:             make(chan struct{}),
	}
}

// OnConnect registers a callback for when Connect is clicked in tray.
// Must be called before Run(). Returns ErrTrayAlreadyRunning if called after Run().
func (t *TrayIcon) OnConnect(callback func()) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return ErrTrayAlreadyRunning
	}
	t.onConnect = callback
	return nil
}

// OnDisconnect registers a callback for when Disconnect is clicked in tray.
// Must be called before Run(). Returns ErrTrayAlreadyRunning if called after Run().
func (t *TrayIcon) OnDisconnect(callback func()) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return ErrTrayAlreadyRunning
	}
	t.onDisconnect = callback
	return nil
}

// OnShow registers a callback for when Show Window is clicked in tray.
// Must be called before Run(). Returns ErrTrayAlreadyRunning if called after Run().
func (t *TrayIcon) OnShow(callback func()) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return ErrTrayAlreadyRunning
	}
	t.onShow = callback
	return nil
}

// OnQuit registers a callback for when Quit is clicked in tray.
// Must be called before Run(). Returns ErrTrayAlreadyRunning if called after Run().
func (t *TrayIcon) OnQuit(callback func()) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.running {
		return ErrTrayAlreadyRunning
	}
	t.onQuit = callback
	return nil
}

// SetState updates the tray icon and menu based on connection state.
func (t *TrayIcon) SetState(state vpn.ConnectionState) {
	t.mu.Lock()
	t.state = state
	t.mu.Unlock()
	t.updateIcon()
	t.updateMenu()
}

// SetProfileName sets the current profile name for display in tray.
func (t *TrayIcon) SetProfileName(name string) {
	t.mu.Lock()
	t.profileName = name
	t.mu.Unlock()
	t.updateMenu()
}

// SetStats updates the traffic rate display in the tray menu.
func (t *TrayIcon) SetStats(s stats.NetworkStats) {
	if t.menuTrafficRate == nil {
		return
	}

	rateText := fmt.Sprintf("↓ %s  ↑ %s",
		stats.FormatRate(s.RxBytesPerSec),
		stats.FormatRate(s.TxBytesPerSec))

	t.menuTrafficRate.SetTitle(rateText)
}

// Run starts the system tray icon. This should be called in a goroutine
// as it blocks until the tray is closed. All callbacks (OnConnect, OnDisconnect,
// OnShow, OnQuit) must be registered before calling Run().
// Returns ErrTrayMissingCallbacks if any callback is not set.
// Returns ErrTrayRunTwice if called more than once.
func (t *TrayIcon) Run() error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return ErrTrayRunTwice
	}

	// Validate all required callbacks are set
	if t.onConnect == nil || t.onDisconnect == nil || t.onShow == nil || t.onQuit == nil {
		t.mu.Unlock()
		return ErrTrayMissingCallbacks
	}

	t.running = true
	t.mu.Unlock()

	systray.Run(t.onReady, t.onExit)
	return nil
}

// Quit closes the system tray icon and terminates the click handler goroutine.
// Safe to call multiple times.
func (t *TrayIcon) Quit() {
	t.closeOnce.Do(func() {
		close(t.done)
		systray.Quit()
	})
}

// onReady is called when the tray is ready to be configured.
func (t *TrayIcon) onReady() {
	// Set initial icon and tooltip
	systray.SetIcon(t.iconDisconnected)
	systray.SetTitle("OpenFortiVPN")
	systray.SetTooltip("OpenFortiVPN GUI - Disconnected")

	// Create menu items
	t.menuStatus = systray.AddMenuItem("Status: Disconnected", "Current connection status")
	t.menuStatus.Disable()

	t.menuTrafficRate = systray.AddMenuItem("", "Current traffic rates")
	t.menuTrafficRate.Disable()
	t.menuTrafficRate.Hide()

	systray.AddSeparator()

	t.menuConnect = systray.AddMenuItem("Connect", "Connect to VPN")
	t.menuDisconnect = systray.AddMenuItem("Disconnect", "Disconnect from VPN")
	t.menuDisconnect.Disable()

	systray.AddSeparator()

	t.menuShow = systray.AddMenuItem("Show Window", "Show the main window")
	t.menuQuit = systray.AddMenuItem("Quit", "Quit the application")

	// Handle menu clicks in a goroutine
	go t.handleMenuClicks()

	slog.Info("System tray initialized")
}

// onExit is called when the tray is being closed.
func (t *TrayIcon) onExit() {
	slog.Info("System tray closed")
}

// handleMenuClicks processes menu item clicks.
func (t *TrayIcon) handleMenuClicks() {
	for {
		select {
		case <-t.done:
			return
		case _, ok := <-t.menuConnect.ClickedCh:
			if !ok {
				return
			}
			if t.onConnect != nil {
				t.onConnect()
			}
		case _, ok := <-t.menuDisconnect.ClickedCh:
			if !ok {
				return
			}
			if t.onDisconnect != nil {
				t.onDisconnect()
			}
		case _, ok := <-t.menuShow.ClickedCh:
			if !ok {
				return
			}
			if t.onShow != nil {
				t.onShow()
			}
		case _, ok := <-t.menuQuit.ClickedCh:
			if !ok {
				return
			}
			if t.onQuit != nil {
				t.onQuit()
			}
		}
	}
}

// updateIcon updates the tray icon based on current state.
func (t *TrayIcon) updateIcon() {
	if t.menuStatus == nil {
		return // Not initialized yet
	}

	t.mu.RLock()
	state := t.state
	profileName := t.profileName
	t.mu.RUnlock()

	var icon []byte
	var tooltip string

	switch state {
	case vpn.StateConnected:
		icon = t.iconConnected
		tooltip = "OpenFortiVPN GUI - Connected"
		if profileName != "" {
			tooltip = fmt.Sprintf("OpenFortiVPN GUI - Connected to %s", profileName)
		}
	case vpn.StateConnecting, vpn.StateAuthenticating, vpn.StateReconnecting:
		icon = t.iconConnecting
		tooltip = "OpenFortiVPN GUI - Connecting..."
	default:
		icon = t.iconDisconnected
		tooltip = "OpenFortiVPN GUI - Disconnected"
	}

	systray.SetIcon(icon)
	systray.SetTooltip(tooltip)
}

// updateMenu updates the menu items based on current state.
func (t *TrayIcon) updateMenu() {
	if t.menuStatus == nil {
		return // Not initialized yet
	}

	t.mu.RLock()
	state := t.state
	profileName := t.profileName
	t.mu.RUnlock()

	// Update status text
	var statusText string
	switch state {
	case vpn.StateConnected:
		statusText = "Status: Connected"
		if profileName != "" {
			statusText = fmt.Sprintf("Status: Connected to %s", profileName)
		}
	case vpn.StateConnecting:
		statusText = "Status: Connecting..."
	case vpn.StateAuthenticating:
		statusText = "Status: Authenticating..."
	case vpn.StateReconnecting:
		statusText = "Status: Reconnecting..."
	case vpn.StateFailed:
		statusText = "Status: Connection Failed"
	default:
		statusText = "Status: Disconnected"
	}
	t.menuStatus.SetTitle(statusText)

	// Show/hide traffic rate based on connection state
	if t.menuTrafficRate != nil {
		if state == vpn.StateConnected {
			t.menuTrafficRate.Show()
		} else {
			t.menuTrafficRate.Hide()
		}
	}

	// Update connect menu item to show which profile will be used
	if profileName != "" && state.CanConnect() {
		t.menuConnect.SetTitle(fmt.Sprintf("Connect (%s)", profileName))
	} else {
		t.menuConnect.SetTitle("Connect")
	}

	// Enable/disable connect/disconnect based on state
	if state.CanConnect() {
		t.menuConnect.Enable()
	} else {
		t.menuConnect.Disable()
	}

	if state.CanDisconnect() {
		t.menuDisconnect.Enable()
	} else {
		t.menuDisconnect.Disable()
	}
}
