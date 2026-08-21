package ui

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/keyring"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
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

// fakeKeyring records Delete calls so tests can assert whether a stored
// password was discarded.
type fakeKeyring struct {
	password string
	deleted  []string
	getErr   error
	delErr   error
}

func (f *fakeKeyring) Save(profileID, password string) error {
	f.password = password
	return nil
}

func (f *fakeKeyring) Get(profileID string) (string, error) {
	return f.password, f.getErr
}

func (f *fakeKeyring) Delete(profileID string) error {
	f.deleted = append(f.deleted, profileID)
	return f.delErr
}

// TestMainWindow_DiscardRejectedPassword asserts a stored password is dropped
// only when the gateway actually rejected the credentials, so the next connect
// re-prompts instead of silently reusing a password that cannot work — and is
// kept for every other kind of failure.
func TestMainWindow_DiscardRejectedPassword(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	newWindow := func(kr keyring.Store, connecting *profile.Profile) *MainWindow {
		return &MainWindow{
			deps:              &MainWindowDeps{KeyringStore: kr},
			connectingProfile: connecting,
			scheduleOnMain:    func(fn func()) { fn() },
		}
	}

	passwordProfile := func() *profile.Profile {
		return &profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword}
	}

	// A rejected one-time password produces the same gateway error as a
	// rejected password, so the stored account password must survive it —
	// otherwise every expired token destroys a credential the UI cannot show.
	t.Run("OTP failure keeps the stored password", func(t *testing.T) {
		kr := &fakeKeyring{password: "right"}
		w := newWindow(kr, &profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodOTP})

		w.discardRejectedPassword(errors.New(
			"Could not authenticate to gateway. Please check the password, client certificate, etc."))

		assert.Empty(t, kr.deleted, "a rejected token must not invalidate the account password")
	})

	t.Run("credential failure discards the stored password", func(t *testing.T) {
		kr := &fakeKeyring{password: "wrong"}
		w := newWindow(kr, passwordProfile())

		w.discardRejectedPassword(errors.New(
			"Could not authenticate to gateway. Please check the password, client certificate, etc."))

		assert.Equal(t, []string{profileID}, kr.deleted)
	})

	t.Run("realm failure keeps the stored password", func(t *testing.T) {
		kr := &fakeKeyring{password: "right"}
		w := newWindow(kr, passwordProfile())

		w.discardRejectedPassword(errors.New(
			"Could not authenticate to the gateway. Please make sure tunnel mode is allowed by the gateway, check the realm, etc."))

		assert.Empty(t, kr.deleted, "a realm problem must not invalidate the password")
	})

	t.Run("unrelated error keeps the stored password", func(t *testing.T) {
		kr := &fakeKeyring{password: "right"}
		w := newWindow(kr, passwordProfile())

		w.discardRejectedPassword(errors.New("connection refused"))

		assert.Empty(t, kr.deleted)
	})

	t.Run("no in-flight profile is a no-op", func(t *testing.T) {
		kr := &fakeKeyring{password: "wrong"}
		w := newWindow(kr, nil)

		assert.NotPanics(t, func() {
			w.discardRejectedPassword(errors.New(
				"Could not authenticate to gateway. Please check the password, client certificate, etc."))
		})
		assert.Empty(t, kr.deleted)
	})

	t.Run("keyring delete failure is tolerated", func(t *testing.T) {
		kr := &fakeKeyring{password: "wrong", delErr: errors.New("keyring locked")}
		w := newWindow(kr, passwordProfile())

		assert.NotPanics(t, func() {
			w.discardRejectedPassword(errors.New(
				"Could not authenticate to gateway. Please check the password, client certificate, etc."))
		})
		assert.Equal(t, []string{profileID}, kr.deleted,
			"the delete must be attempted before its failure is swallowed")
	})

	t.Run("nil error is a no-op", func(t *testing.T) {
		kr := &fakeKeyring{password: "right"}
		w := newWindow(kr, passwordProfile())

		w.discardRejectedPassword(nil)

		assert.Empty(t, kr.deleted)
	})

	// Tray-only paths build a window with partial deps, so losing this guard
	// would turn any VPN error into a nil-interface panic on the main thread.
	t.Run("missing keyring store is a no-op", func(t *testing.T) {
		w := &MainWindow{
			deps:              &MainWindowDeps{},
			connectingProfile: passwordProfile(),
			scheduleOnMain:    func(fn func()) { fn() },
		}

		assert.NotPanics(t, func() {
			w.discardRejectedPassword(errors.New(
				"Could not authenticate to gateway. Please check the password, client certificate, etc."))
		})
	})
}

// TestMainWindow_ReleaseConnectingProfile asserts the in-flight profile is
// forgotten once an attempt finishes, so a late credential error cannot be
// attributed to whichever profile is connected next.
func TestMainWindow_ReleaseConnectingProfile(t *testing.T) {
	tests := []struct {
		state    vpn.ConnectionState
		released bool
	}{
		{vpn.StateDisconnected, true},
		{vpn.StateFailed, true},
		{vpn.StateConnected, false},
		{vpn.StateConnecting, false},
		{vpn.StateAuthenticating, false},
		{vpn.StateReconnecting, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			w := &MainWindow{
				deps:              &MainWindowDeps{},
				connectingProfile: &profile.Profile{ID: "in-flight"},
			}

			w.releaseConnectingProfile(tt.state)

			if tt.released {
				assert.Nil(t, w.connectingProfile, "a finished attempt must be forgotten")
			} else {
				assert.NotNil(t, w.connectingProfile, "an in-progress attempt must be retained")
			}
		})
	}
}

// TestMainWindow_HandleError_DropsSupersededConnection covers a callback that
// was raised for one connection but reaches the main thread after the user has
// started another: acting on it would discard the new connection's password for
// a rejection that belongs to the old one.
func TestMainWindow_HandleError_DropsSupersededConnection(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"
	const credentialFailure = "Could not authenticate to gateway. Please check the password, client certificate, etc."

	newWindow := func(kr keyring.Store, shown *[]string) *MainWindow {
		w := &MainWindow{
			deps:              &MainWindowDeps{KeyringStore: kr},
			connectingProfile: &profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword},
			scheduleOnMain:    func(fn func()) { fn() },
		}
		w.presentError = func(_, message string) { *shown = append(*shown, message) }
		w.connection.begin()
		return w
	}

	t.Run("a retry started in between drops the error", func(t *testing.T) {
		kr := &fakeKeyring{password: "right"}
		var shown []string
		w := newWindow(kr, &shown)

		// The retry lands between the callback being raised and the main loop
		// running it, which is the whole window this guard exists for.
		w.scheduleOnMain = func(fn func()) {
			w.connection.begin()
			fn()
		}

		w.handleError(errors.New(credentialFailure))

		assert.Empty(t, kr.deleted,
			"a rejection from a superseded connection must not discard the current one's password")
		assert.Empty(t, shown, "nor report a failure the current connection has not had")
	})

	t.Run("the current connection's error is acted on", func(t *testing.T) {
		kr := &fakeKeyring{password: "wrong"}
		var shown []string
		w := newWindow(kr, &shown)

		w.handleError(errors.New(credentialFailure))

		assert.Equal(t, []string{profileID}, kr.deleted)
		assert.Equal(t, []string{credentialFailure}, shown)
	})
}

// TestConnectionCounter tracks which connection a callback belongs to; a
// callback raised under an earlier count must not be treated as current.
func TestConnectionCounter(t *testing.T) {
	var counter connectionCounter

	assert.True(t, counter.isCurrent(0), "no connection started yet")

	first := counter.begin()
	assert.True(t, counter.isCurrent(first))
	assert.Equal(t, first, counter.current())

	second := counter.begin()
	assert.NotEqual(t, first, second)
	assert.True(t, counter.isCurrent(second))
	assert.False(t, counter.isCurrent(first), "the earlier connection is superseded")
}

// TestMainWindow_CountConnection pins which state starts a new connection count.
// Only the controller's Connecting announcement does, since that is the one
// delivered while the previous attempt's callbacks are held off.
func TestMainWindow_CountConnection(t *testing.T) {
	w := &MainWindow{}

	w.countConnection(vpn.StateConnecting)
	first := w.connection.current()
	assert.NotZero(t, first)

	for _, state := range []vpn.ConnectionState{
		vpn.StateConnected,
		vpn.StateAuthenticating,
		vpn.StateReconnecting,
		vpn.StateFailed,
		vpn.StateDisconnected,
	} {
		w.countConnection(state)
		assert.Equal(t, first, w.connection.current(),
			"%s must not start a new connection count", state)
	}

	w.countConnection(vpn.StateConnecting)
	assert.NotEqual(t, first, w.connection.current(), "a retry starts a new count")
}

// TestMainWindow_ForgetSavedPassword covers the explicit forget action, which
// is the only way an OTP profile can replace a stale password: a rejected
// one-time token reports the same gateway error as a rejected password, so the
// automatic discard cannot run for those profiles.
func TestMainWindow_ForgetSavedPassword(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	newWindow := func(kr keyring.Store, shown, toasts *[]string) *MainWindow {
		w := &MainWindow{deps: &MainWindowDeps{KeyringStore: kr}}
		w.presentError = func(_, message string) { *shown = append(*shown, message) }
		w.presentToast = func(message string) { *toasts = append(*toasts, message) }
		return w
	}

	t.Run("deletes the stored password and confirms it", func(t *testing.T) {
		kr := &fakeKeyring{password: "stale"}
		var shown, toasts []string
		w := newWindow(kr, &shown, &toasts)

		w.forgetSavedPassword(&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodOTP})

		assert.Equal(t, []string{profileID}, kr.deleted)
		assert.Empty(t, shown, "a successful delete must not report an error")
		assert.Len(t, toasts, 1, "success is otherwise invisible in the window")
	})

	t.Run("a failed delete is reported", func(t *testing.T) {
		kr := &fakeKeyring{password: "stale", delErr: errors.New("keyring locked")}
		var shown, toasts []string
		w := newWindow(kr, &shown, &toasts)

		w.forgetSavedPassword(&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodOTP})

		assert.Equal(t, []string{profileID}, kr.deleted)
		assert.Equal(t, []string{"keyring locked"}, shown,
			"the user must learn the password is still stored")
		assert.Empty(t, toasts, "a failure must not be confirmed as a success")
	})

	t.Run("nil profile is a no-op", func(t *testing.T) {
		kr := &fakeKeyring{password: "stale"}
		var shown, toasts []string
		w := newWindow(kr, &shown, &toasts)

		assert.NotPanics(t, func() { w.forgetSavedPassword(nil) })
		assert.Empty(t, kr.deleted)
		assert.Empty(t, toasts)
	})

	// Tray-only paths build a window with partial deps.
	t.Run("missing keyring store is a no-op", func(t *testing.T) {
		var shown, toasts []string
		w := newWindow(nil, &shown, &toasts)

		assert.NotPanics(t, func() {
			w.forgetSavedPassword(&profile.Profile{ID: profileID})
		})
		assert.Empty(t, shown, "a missing keyring store must not surface an error dialog")
		assert.Empty(t, toasts, "nor claim a password was forgotten")
	})
}

// TestMainWindow_ForgetPassword_NilGuards covers the guards that keep the
// forget path safe on a window without widgets: the tray-only and test paths
// build a MainWindow whose toast overlay was never created.
func TestMainWindow_ForgetPassword_NilGuards(t *testing.T) {
	assert.NotPanics(t, func() { (&MainWindow{}).showToast("anything") },
		"a window with no toast overlay must not reach Adw")
	assert.NotPanics(t, func() { (&MainWindow{}).onForgetPassword(nil) },
		"a nil profile must return before the dialog is constructed")
}
