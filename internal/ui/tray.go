// Package ui provides the GTK4/libadwaita user interface for openfortivpn-gui.
package ui

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"fyne.io/systray"

	"context"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/shini4i/openfortivpn-gui/internal/stats"
	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

const defaultDBusTimeout = time.Second

const (
	sniBusName   = "org.kde.StatusNotifierWatcher"
	sniPath      = "/StatusNotifierWatcher"
	sniInterface = "org.kde.StatusNotifierWatcher"
	sniProperty  = "IsStatusNotifierHostRegistered"
)

var (
	// ErrTrayAlreadyRunning is returned when attempting to modify callbacks after Run() has been called.
	ErrTrayAlreadyRunning = errors.New("cannot modify callbacks after TrayIcon.Run() is called")
	// ErrTrayRunTwice is returned when Run() is called more than once.
	ErrTrayRunTwice = errors.New("TrayIcon.Run() called twice")
	// ErrTrayMissingCallbacks is returned when Run() is called without all required callbacks set.
	ErrTrayMissingCallbacks = errors.New("all callbacks (OnConnect, OnDisconnect, OnShow, OnQuit) must be set before calling Run()")
)

// trayMenu holds the tray's menu entries. onReady creates them together and
// publishes them as one value, so every entry is either set or the whole menu
// is still unpublished; status therefore doubles as the "menu is ready" flag.
type trayMenu struct {
	status      *systray.MenuItem
	trafficRate *systray.MenuItem
	connect     *systray.MenuItem
	disconnect  *systray.MenuItem
	show        *systray.MenuItem
	quit        *systray.MenuItem
}

// TrayIcon manages the system tray icon and menu.
type TrayIcon struct {
	mu sync.RWMutex

	// State
	state       vpn.ConnectionState
	profileName string

	// Menu entries; zero until onReady publishes them.
	menu trayMenu

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
	_, _, menu := t.snapshot()
	if menu.status == nil {
		return // Menu not published yet
	}

	rateText := fmt.Sprintf("↓ %s  ↑ %s",
		stats.FormatRate(s.RxBytesPerSec),
		stats.FormatRate(s.TxBytesPerSec))

	menu.trafficRate.SetTitle(rateText)
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
	status := systray.AddMenuItem("Status: Disconnected", "Current connection status")
	status.Disable()

	trafficRate := systray.AddMenuItem("", "Current traffic rates")
	trafficRate.Disable()
	trafficRate.Hide()

	systray.AddSeparator()

	connect := systray.AddMenuItem("Connect", "Connect to VPN")
	disconnect := systray.AddMenuItem("Disconnect", "Disconnect from VPN")
	disconnect.Disable()

	systray.AddSeparator()

	show := systray.AddMenuItem("Show Window", "Show the main window")
	quit := systray.AddMenuItem("Quit", "Quit the application")

	// Publish the finished menu as a unit. Until this returns, readers on the
	// GTK main thread see no menu at all rather than a partially built one.
	t.publishMenu(trayMenu{
		status: status, trafficRate: trafficRate,
		connect: connect, disconnect: disconnect,
		show: show, quit: quit,
	})

	// Handle menu clicks in a goroutine
	go t.handleMenuClicks()

	slog.Info("System tray initialized")
}

// onExit is called when the tray is being closed.
func (t *TrayIcon) onExit() {
	slog.Info("System tray closed")
}

// publishMenu stores the menu in a single critical section. onReady builds it
// on the systray goroutine while the GTK main thread may already be pushing
// updates, so publishing entry by entry would both race those readers and
// expose a half-built menu.
func (t *TrayIcon) publishMenu(m trayMenu) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.menu = m
}

// snapshot reads the state, profile name and menu together under the lock, so
// a reader cannot mix a new state with a stale menu.
func (t *TrayIcon) snapshot() (vpn.ConnectionState, string, trayMenu) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.state, t.profileName, t.menu
}

// handleMenuClicks processes menu item clicks. It is started by onReady once
// the menu is published, so the entries it reads are already final.
func (t *TrayIcon) handleMenuClicks() {
	_, _, menu := t.snapshot()
	if menu.status == nil {
		slog.Error("Tray click handler started before the menu was published")
		return
	}

	for {
		select {
		case <-t.done:
			return
		case _, ok := <-menu.connect.ClickedCh:
			if !ok {
				return
			}
			if t.onConnect != nil {
				t.onConnect()
			}
		case _, ok := <-menu.disconnect.ClickedCh:
			if !ok {
				return
			}
			if t.onDisconnect != nil {
				t.onDisconnect()
			}
		case _, ok := <-menu.show.ClickedCh:
			if !ok {
				return
			}
			if t.onShow != nil {
				t.onShow()
			}
		case _, ok := <-menu.quit.ClickedCh:
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
	state, profileName, menu := t.snapshot()
	if menu.status == nil {
		return // Not initialized yet
	}

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
	state, profileName, menu := t.snapshot()
	if menu.status == nil {
		return // Not initialized yet
	}

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
	menu.status.SetTitle(statusText)

	// Show/hide traffic rate based on connection state
	if state == vpn.StateConnected {
		menu.trafficRate.Show()
	} else {
		menu.trafficRate.Hide()
	}

	// Update connect menu item to show which profile will be used
	if profileName != "" && state.CanConnect() {
		menu.connect.SetTitle(fmt.Sprintf("Connect (%s)", profileName))
	} else {
		menu.connect.SetTitle("Connect")
	}

	// Enable/disable connect/disconnect based on state
	if state.CanConnect() {
		menu.connect.Enable()
	} else {
		menu.connect.Disable()
	}

	if state.CanDisconnect() {
		menu.disconnect.Enable()
	} else {
		menu.disconnect.Disable()
	}
}

// hasTraySupport probes DBus to determine if a StatusNotifierHost is registered
// on org.kde.StatusNotifierWatcher. Bounded by defaultDBusTimeout.
func hasTraySupport() bool {
	return hasTraySupportWithTimeout(defaultDBusTimeout)
}

// hasTraySupportWithTimeout evaluates tray support within a specified time budget.
func hasTraySupportWithTimeout(timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return false
	}
	defer func() { _ = conn.Close() }()

	obj := conn.Object(sniBusName, dbus.ObjectPath(sniPath))

	var variant dbus.Variant
	err = obj.CallWithContext(
		ctx,
		"org.freedesktop.DBus.Properties.Get",
		0,
		sniInterface,
		sniProperty,
	).Store(&variant)
	if err != nil {
		return false
	}

	registered, ok := variant.Value().(bool)
	return ok && registered
}
