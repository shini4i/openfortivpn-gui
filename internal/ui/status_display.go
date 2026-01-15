package ui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// StatusDisplay shows the current VPN connection status in a compact form.
type StatusDisplay struct {
	widget *gtk.Box

	// Status components
	stateLabel   *gtk.Label
	profileLabel *gtk.Label
	ipLabel      *gtk.Label

	// State
	state      vpn.ConnectionState
	assignedIP string
}

// NewStatusDisplay creates a new status display widget.
func NewStatusDisplay() *StatusDisplay {
	sd := &StatusDisplay{
		state: vpn.StateDisconnected,
	}

	sd.setupWidget()
	return sd
}

// setupWidget creates the compact status display UI.
func (sd *StatusDisplay) setupWidget() {
	sd.widget = gtk.NewBox(gtk.OrientationHorizontal, 12)
	sd.widget.SetHAlign(gtk.AlignCenter)
	sd.widget.SetMarginTop(8)
	sd.widget.SetMarginBottom(8)

	// VPN icon
	icon := gtk.NewImageFromIconName("network-vpn-symbolic")
	icon.SetPixelSize(20)
	sd.widget.Append(icon)

	// State label
	sd.stateLabel = gtk.NewLabel("Disconnected")
	sd.stateLabel.AddCSSClass("heading")
	sd.widget.Append(sd.stateLabel)

	// Separator
	sep := gtk.NewSeparator(gtk.OrientationVertical)
	sep.SetMarginStart(4)
	sep.SetMarginEnd(4)
	sd.widget.Append(sep)

	// Profile label
	sd.profileLabel = gtk.NewLabel("No profile")
	sd.profileLabel.SetOpacity(dimmedOpacity) // Subtle dimming without being invisible in dark themes
	sd.widget.Append(sd.profileLabel)

	// IP label (hidden by default)
	sd.ipLabel = gtk.NewLabel("")
	sd.ipLabel.SetOpacity(dimmedOpacity) // Subtle dimming without being invisible in dark themes
	sd.ipLabel.SetVisible(false)
	sd.widget.Append(sd.ipLabel)

	sd.updateStateDisplay()
}

// SetState updates the displayed connection state.
func (sd *StatusDisplay) SetState(state vpn.ConnectionState) {
	glib.IdleAdd(func() {
		sd.state = state
		sd.updateStateDisplay()
	})
}

// updateStateDisplay updates the UI based on the current state.
func (sd *StatusDisplay) updateStateDisplay() {
	var stateText string

	switch sd.state {
	case vpn.StateDisconnected:
		stateText = "Disconnected"
		sd.ipLabel.SetVisible(false)
	case vpn.StateConnecting:
		stateText = "Connecting..."
	case vpn.StateAuthenticating:
		stateText = "Authenticating..."
	case vpn.StateConnected:
		stateText = "Connected"
		if sd.assignedIP != "" {
			sd.ipLabel.SetText(fmt.Sprintf("• %s", sd.assignedIP))
			sd.ipLabel.SetVisible(true)
		}
	case vpn.StateReconnecting:
		stateText = "Reconnecting..."
	case vpn.StateFailed:
		stateText = "Failed"
		sd.ipLabel.SetVisible(false)
	default:
		stateText = string(sd.state)
	}

	sd.stateLabel.SetLabel(stateText)

	// Update state label CSS
	sd.stateLabel.RemoveCSSClass("success")
	sd.stateLabel.RemoveCSSClass("error")
	sd.stateLabel.RemoveCSSClass("warning")

	switch sd.state {
	case vpn.StateConnected:
		sd.stateLabel.AddCSSClass("success")
	case vpn.StateFailed:
		sd.stateLabel.AddCSSClass("error")
	case vpn.StateConnecting, vpn.StateAuthenticating, vpn.StateReconnecting:
		sd.stateLabel.AddCSSClass("warning")
	}
}

// SetProfileInfo sets the profile name to display.
func (sd *StatusDisplay) SetProfileInfo(name string) {
	glib.IdleAdd(func() {
		if name == "" {
			sd.profileLabel.SetLabel("No profile")
		} else {
			sd.profileLabel.SetLabel(name)
		}
	})
}

// SetAssignedIP sets the assigned IP address to display.
func (sd *StatusDisplay) SetAssignedIP(ip string) {
	glib.IdleAdd(func() {
		sd.assignedIP = ip
		if ip != "" && sd.state == vpn.StateConnected {
			sd.ipLabel.SetText(fmt.Sprintf("• %s", ip))
			sd.ipLabel.SetVisible(true)
		} else {
			sd.ipLabel.SetVisible(false)
		}
	})
}

// Widget returns the root GTK widget for the status display.
func (sd *StatusDisplay) Widget() gtk.Widgetter {
	return sd.widget
}
