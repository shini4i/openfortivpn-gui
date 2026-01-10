// Package ui provides the GTK4/libadwaita user interface for openfortivpn-gui.
package ui

import (
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
)

// PreferencesWindow shows application preferences.
type PreferencesWindow struct {
	window *adw.PreferencesWindow

	// Settings widgets
	notificationsSwitch *adw.SwitchRow
	autoConnectSwitch   *adw.SwitchRow

	// Callbacks
	onNotificationsChanged func(enabled bool)
	onAutoConnectChanged   func(enabled bool)

	// Track previous state to detect changes
	prevNotifications bool
	prevAutoConnect   bool
}

// NewPreferencesWindow creates a new preferences window.
func NewPreferencesWindow(parent *MainWindow) *PreferencesWindow {
	pw := &PreferencesWindow{}
	pw.setupWindow(parent)
	return pw
}

// setupWindow creates the preferences window UI.
func (pw *PreferencesWindow) setupWindow(parent *MainWindow) {
	pw.window = adw.NewPreferencesWindow()
	pw.window.SetTitle("Preferences")
	pw.window.SetModal(true)
	pw.window.SetDefaultSize(400, 300)

	// Set transient parent
	if parent != nil && parent.window != nil {
		pw.window.SetTransientFor(&parent.window.Window)
	}

	// Create general page
	generalPage := adw.NewPreferencesPage()
	generalPage.SetTitle("General")
	generalPage.SetIconName("preferences-system-symbolic")

	// Behavior group
	behaviorGroup := adw.NewPreferencesGroup()
	behaviorGroup.SetTitle("Behavior")
	behaviorGroup.SetDescription("Configure application behavior")

	// Notifications switch
	pw.notificationsSwitch = adw.NewSwitchRow()
	pw.notificationsSwitch.SetTitle("Desktop Notifications")
	pw.notificationsSwitch.SetSubtitle("Show notifications for VPN state changes")
	pw.notificationsSwitch.SetActive(true)
	pw.prevNotifications = true
	behaviorGroup.Add(pw.notificationsSwitch)

	// Auto-connect switch
	pw.autoConnectSwitch = adw.NewSwitchRow()
	pw.autoConnectSwitch.SetTitle("Auto-Connect on Startup")
	pw.autoConnectSwitch.SetSubtitle("Automatically connect to the last used profile")
	pw.autoConnectSwitch.SetActive(false)
	pw.prevAutoConnect = false
	behaviorGroup.Add(pw.autoConnectSwitch)

	generalPage.Add(behaviorGroup)
	pw.window.Add(generalPage)

	// Handle window close to trigger callbacks
	pw.window.ConnectCloseRequest(func() bool {
		pw.handleClose()
		return false // Allow close
	})
}

// handleClose is called when the preferences window is closed.
func (pw *PreferencesWindow) handleClose() {
	// Check for notification changes
	if pw.notificationsSwitch.Active() != pw.prevNotifications {
		if pw.onNotificationsChanged != nil {
			pw.onNotificationsChanged(pw.notificationsSwitch.Active())
		}
	}

	// Check for auto-connect changes
	if pw.autoConnectSwitch.Active() != pw.prevAutoConnect {
		if pw.onAutoConnectChanged != nil {
			pw.onAutoConnectChanged(pw.autoConnectSwitch.Active())
		}
	}
}

// Present shows the preferences window.
func (pw *PreferencesWindow) Present() {
	pw.window.Present()
}

// SetNotificationsEnabled sets the notifications toggle state.
func (pw *PreferencesWindow) SetNotificationsEnabled(enabled bool) {
	pw.notificationsSwitch.SetActive(enabled)
	pw.prevNotifications = enabled
}

// SetAutoConnect sets the auto-connect toggle state.
func (pw *PreferencesWindow) SetAutoConnect(enabled bool) {
	pw.autoConnectSwitch.SetActive(enabled)
	pw.prevAutoConnect = enabled
}

// OnNotificationsChanged registers a callback for notification setting changes.
func (pw *PreferencesWindow) OnNotificationsChanged(callback func(enabled bool)) {
	pw.onNotificationsChanged = callback
}

// OnAutoConnectChanged registers a callback for auto-connect setting changes.
func (pw *PreferencesWindow) OnAutoConnectChanged(callback func(enabled bool)) {
	pw.onAutoConnectChanged = callback
}
