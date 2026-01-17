package ui

import (
	"fmt"

	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/shini4i/openfortivpn-gui/internal/stats"
)

// StatsDisplay shows the current VPN traffic statistics.
type StatsDisplay struct {
	widget *gtk.Box

	// Rate labels
	rxRateLabel *gtk.Label
	txRateLabel *gtk.Label

	// Session total labels
	rxTotalLabel *gtk.Label
	txTotalLabel *gtk.Label

	// Duration label
	durationLabel *gtk.Label
}

// NewStatsDisplay creates a new traffic stats display widget.
func NewStatsDisplay() *StatsDisplay {
	sd := &StatsDisplay{}
	sd.setupWidget()
	return sd
}

// setupWidget creates the stats display UI.
// Layout: ↓ 5.2 KiB/s  ↑ 1.1 KiB/s  │  Session: ↓ 125 MiB  ↑ 32 MiB  │  1h 23m
func (sd *StatsDisplay) setupWidget() {
	sd.widget = gtk.NewBox(gtk.OrientationHorizontal, 8)
	sd.widget.SetHAlign(gtk.AlignCenter)
	sd.widget.SetMarginTop(4)
	sd.widget.SetMarginBottom(4)
	sd.widget.SetVisible(false) // Hidden by default

	// Download rate
	sd.rxRateLabel = gtk.NewLabel("↓ 0 B/s")
	sd.rxRateLabel.SetOpacity(dimmedOpacity)
	sd.widget.Append(sd.rxRateLabel)

	// Upload rate
	sd.txRateLabel = gtk.NewLabel("↑ 0 B/s")
	sd.txRateLabel.SetOpacity(dimmedOpacity)
	sd.widget.Append(sd.txRateLabel)

	// Separator
	sep1 := gtk.NewSeparator(gtk.OrientationVertical)
	sep1.SetMarginStart(4)
	sep1.SetMarginEnd(4)
	sd.widget.Append(sep1)

	// Session label
	sessionLabel := gtk.NewLabel("Session:")
	sessionLabel.SetOpacity(dimmedOpacity)
	sd.widget.Append(sessionLabel)

	// Session download total
	sd.rxTotalLabel = gtk.NewLabel("↓ 0 B")
	sd.rxTotalLabel.SetOpacity(dimmedOpacity)
	sd.widget.Append(sd.rxTotalLabel)

	// Session upload total
	sd.txTotalLabel = gtk.NewLabel("↑ 0 B")
	sd.txTotalLabel.SetOpacity(dimmedOpacity)
	sd.widget.Append(sd.txTotalLabel)

	// Separator
	sep2 := gtk.NewSeparator(gtk.OrientationVertical)
	sep2.SetMarginStart(4)
	sep2.SetMarginEnd(4)
	sd.widget.Append(sep2)

	// Duration
	sd.durationLabel = gtk.NewLabel("0s")
	sd.durationLabel.SetOpacity(dimmedOpacity)
	sd.widget.Append(sd.durationLabel)
}

// SetStats updates the displayed statistics.
func (sd *StatsDisplay) SetStats(s stats.NetworkStats) {
	glib.IdleAdd(func() {
		// Update rate labels
		sd.rxRateLabel.SetLabel(fmt.Sprintf("↓ %s", stats.FormatRate(s.RxBytesPerSec)))
		sd.txRateLabel.SetLabel(fmt.Sprintf("↑ %s", stats.FormatRate(s.TxBytesPerSec)))

		// Update session total labels
		sd.rxTotalLabel.SetLabel(fmt.Sprintf("↓ %s", stats.FormatBytes(s.SessionRxBytes)))
		sd.txTotalLabel.SetLabel(fmt.Sprintf("↑ %s", stats.FormatBytes(s.SessionTxBytes)))

		// Update duration
		sd.durationLabel.SetLabel(stats.FormatDuration(s.Duration))
	})
}

// SetVisible shows or hides the stats display.
func (sd *StatsDisplay) SetVisible(visible bool) {
	glib.IdleAdd(func() {
		sd.widget.SetVisible(visible)
	})
}

// Widget returns the root GTK widget for the stats display.
func (sd *StatsDisplay) Widget() gtk.Widgetter {
	return sd.widget
}
