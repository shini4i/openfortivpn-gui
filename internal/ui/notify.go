// Package ui provides the GTK4/libadwaita user interface for openfortivpn-gui.
package ui

import (
	"log/slog"
	"sync/atomic"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// NotificationType identifies the type of notification to display.
type NotificationType int

const (
	// NotifyConnected indicates a successful VPN connection.
	NotifyConnected NotificationType = iota
	// NotifyDisconnected indicates the VPN has disconnected.
	NotifyDisconnected
	// NotifyConnectionFailed indicates a connection failure.
	NotifyConnectionFailed
	// NotifyReconnecting indicates the VPN is attempting to reconnect.
	NotifyReconnecting
)

// Notifier manages desktop notifications for VPN events.
// All methods are safe for concurrent access.
type Notifier struct {
	app     *adw.Application
	enabled atomic.Bool
}

// NewNotifier creates a new notification manager.
// The app parameter should be a GTK Application that supports sending notifications.
func NewNotifier(app *adw.Application) *Notifier {
	n := &Notifier{
		app: app,
	}
	n.enabled.Store(true)
	return n
}

// SetEnabled enables or disables notifications.
// This method is safe for concurrent access.
func (n *Notifier) SetEnabled(enabled bool) {
	n.enabled.Store(enabled)
}

// IsEnabled returns whether notifications are enabled.
// This method is safe for concurrent access.
func (n *Notifier) IsEnabled() bool {
	return n.enabled.Load()
}

// Notify sends a desktop notification.
// This method is safe to call from any goroutine - GTK operations are
// dispatched to the main thread via glib.IdleAdd().
func (n *Notifier) Notify(notifyType NotificationType, profileName string) {
	if !n.enabled.Load() || n.app == nil {
		return
	}

	var title, body, icon string

	switch notifyType {
	case NotifyConnected:
		title = "VPN Connected"
		body = "Connected to " + profileName
		icon = "network-vpn-symbolic"
	case NotifyDisconnected:
		title = "VPN Disconnected"
		body = "Disconnected from " + profileName
		icon = "network-vpn-disconnected-symbolic"
	case NotifyConnectionFailed:
		title = "VPN Connection Failed"
		body = "Failed to connect to " + profileName
		icon = "dialog-error-symbolic"
	case NotifyReconnecting:
		title = "VPN Reconnecting"
		body = "Reconnecting to " + profileName
		icon = "network-vpn-acquiring-symbolic"
	default:
		return
	}

	// Dispatch GTK operations to main thread - GTK is not thread-safe
	glib.IdleAdd(func() {
		notification := gio.NewNotification(title)
		notification.SetBody(body)
		notification.SetIcon(gio.NewThemedIcon(icon))

		// Use a unique ID per notification type so they replace each other
		notificationID := "vpn-status"
		n.app.SendNotification(notificationID, notification)

		slog.Debug("Notification sent", "title", title, "body", body)
	})
}

// NotifyConnected sends a connected notification.
func (n *Notifier) NotifyConnected(profileName string) {
	n.Notify(NotifyConnected, profileName)
}

// NotifyDisconnected sends a disconnected notification.
func (n *Notifier) NotifyDisconnected(profileName string) {
	n.Notify(NotifyDisconnected, profileName)
}

// NotifyConnectionFailed sends a connection failed notification.
func (n *Notifier) NotifyConnectionFailed(profileName string) {
	n.Notify(NotifyConnectionFailed, profileName)
}

// NotifyReconnecting sends a reconnecting notification.
func (n *Notifier) NotifyReconnecting(profileName string) {
	n.Notify(NotifyReconnecting, profileName)
}
