package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/vpn"
)

// TestMainWindow_HandleStateChange_MarshalsToMainThread verifies that VPN
// state-change handling defers all UI/tray work to the GTK main thread via
// scheduleOnMain instead of running it on the caller's goroutine.
//
// Regression test for the SIGSEGV crash: VPN state changes are delivered on
// the controller's output-processing goroutine. Touching GTK or the
// systray/D-Bus stack from that background goroutine corrupts their state and
// crashes the process. All deps and widgets are deliberately left nil here; if
// the handler touched any of them inline (the bug), it would panic instead of
// scheduling the work for the main thread.
func TestMainWindow_HandleStateChange_MarshalsToMainThread(t *testing.T) {
	var scheduled []func()
	w := &MainWindow{
		deps: &MainWindowDeps{},
		scheduleOnMain: func(fn func()) {
			scheduled = append(scheduled, fn)
		},
	}

	assert.NotPanics(t, func() {
		w.handleStateChange(vpn.StateDisconnected, vpn.StateConnecting)
	}, "handler must not touch GTK widgets on the caller goroutine")

	assert.Len(t, scheduled, 1, "UI work must be marshaled to the main thread exactly once")
}

// TestMainWindow_HandleError_MarshalsToMainThread verifies that VPN error
// reporting is marshaled onto the GTK main thread.
//
// Regression test: VPN errors are emitted on the controller's
// output-processing goroutine. showError creates an adw.AlertDialog, which
// must not be constructed off the main thread or the process crashes
// (SIGSEGV). With deps/widgets left nil, the buggy inline version would panic
// instead of scheduling.
func TestMainWindow_HandleError_MarshalsToMainThread(t *testing.T) {
	var scheduled []func()
	w := &MainWindow{
		deps: &MainWindowDeps{},
		scheduleOnMain: func(fn func()) {
			scheduled = append(scheduled, fn)
		},
	}

	assert.NotPanics(t, func() {
		w.handleError(errors.New("boom"))
	}, "error handler must not create GTK dialogs on the caller goroutine")

	assert.Len(t, scheduled, 1, "error display must be marshaled to the main thread")
}

// TestMainWindow_HandleEvent_MarshalsToMainThread verifies that VPN output
// events are marshaled onto the GTK main thread.
//
// Regression test: events (e.g. SAML EventAuthenticate, which opens a browser
// and may show an error dialog) are emitted on the controller's background
// goroutine and must not touch GTK directly.
func TestMainWindow_HandleEvent_MarshalsToMainThread(t *testing.T) {
	var scheduled []func()
	w := &MainWindow{
		deps: &MainWindowDeps{},
		scheduleOnMain: func(fn func()) {
			scheduled = append(scheduled, fn)
		},
	}

	event := &vpn.OutputEvent{Type: vpn.EventAuthenticate}

	assert.NotPanics(t, func() {
		w.handleEvent(event)
	}, "event handler must not touch GTK on the caller goroutine")

	assert.Len(t, scheduled, 1, "event handling must be marshaled to the main thread")
}
