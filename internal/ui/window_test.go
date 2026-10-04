package ui

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/keyring"
	"github.com/shini4i/openfortivpn-gui/internal/profile"
	"github.com/shini4i/openfortivpn-gui/internal/reconnect"
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

// TestMainWindow_DiscardPasswordForAuthMethod asserts a stored password is
// dropped when a profile stops using one: the entry is keyed by profile ID, so
// it would otherwise be unreachable from the UI yet reused if the profile
// switched back to password auth.
func TestMainWindow_DiscardPasswordForAuthMethod(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	newWindow := func(kr keyring.Store, shown *[]string) *MainWindow {
		w := &MainWindow{deps: &MainWindowDeps{KeyringStore: kr}}
		w.presentError = func(_, message string) { *shown = append(*shown, message) }
		return w
	}

	cases := []struct {
		name        string
		prev        profile.AuthMethod
		next        profile.AuthMethod
		wantDeleted bool
	}{
		{"password to certificate", profile.AuthMethodPassword, profile.AuthMethodCertificate, true},
		{"password to saml", profile.AuthMethodPassword, profile.AuthMethodSAML, true},
		{"otp to saml", profile.AuthMethodOTP, profile.AuthMethodSAML, true},
		{"password kept", profile.AuthMethodPassword, profile.AuthMethodPassword, false},
		{"password to otp", profile.AuthMethodPassword, profile.AuthMethodOTP, false},
		{"certificate to saml", profile.AuthMethodCertificate, profile.AuthMethodSAML, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kr := &fakeKeyring{password: "orphan"}
			var shown []string
			w := newWindow(kr, &shown)

			w.discardPasswordForAuthMethod(
				&profile.Profile{ID: profileID, AuthMethod: tc.prev},
				&profile.Profile{ID: profileID, AuthMethod: tc.next},
			)

			if tc.wantDeleted {
				assert.Equal(t, []string{profileID}, kr.deleted,
					"a profile that no longer authenticates with a password must not keep one")
			} else {
				assert.Empty(t, kr.deleted,
					"the keyring must not be touched when no password can be orphaned")
			}
			assert.Empty(t, shown)
		})
	}

	t.Run("a failed delete is reported", func(t *testing.T) {
		kr := &fakeKeyring{password: "orphan", delErr: errors.New("keyring locked")}
		var shown []string
		w := newWindow(kr, &shown)

		w.discardPasswordForAuthMethod(
			&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword},
			&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML},
		)

		assert.Equal(t, []string{profileID}, kr.deleted)
		assert.Equal(t, []string{"keyring locked"}, shown,
			"the user must learn the password is still stored")
	})

	t.Run("a nil profile is a no-op", func(t *testing.T) {
		kr := &fakeKeyring{password: "orphan"}
		var shown []string
		w := newWindow(kr, &shown)

		assert.NotPanics(t, func() {
			w.discardPasswordForAuthMethod(
				&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword}, nil)
		})
		assert.Empty(t, kr.deleted)
		assert.Empty(t, shown)
	})

	// A profile the list does not track yet is new, so it can have no password.
	t.Run("an untracked profile is a no-op", func(t *testing.T) {
		kr := &fakeKeyring{password: "orphan"}
		var shown []string
		w := newWindow(kr, &shown)

		assert.NotPanics(t, func() {
			w.discardPasswordForAuthMethod(nil,
				&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML})
		})
		assert.Empty(t, kr.deleted)
		assert.Empty(t, shown)
	})

	// Tray-only paths build a window with partial deps.
	t.Run("a missing keyring store is a no-op", func(t *testing.T) {
		var shown []string
		w := newWindow(nil, &shown)

		assert.NotPanics(t, func() {
			w.discardPasswordForAuthMethod(
				&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword},
				&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML},
			)
		})
		assert.Empty(t, shown)
	})
}

// fakeProfileStore records saves. Only Save is exercised by saveProfile; the
// rest of profile.StoreInterface is present to satisfy it.
type fakeProfileStore struct {
	saved     []string
	saveErr   error
	deleteErr error
}

func (f *fakeProfileStore) Load(string) (*profile.Profile, error) { return nil, nil }
func (f *fakeProfileStore) List() (*profile.ListResult, error)    { return nil, nil }
func (f *fakeProfileStore) Exists(string) (bool, error)           { return false, nil }
func (f *fakeProfileStore) Delete(string) error                   { return f.deleteErr }

func (f *fakeProfileStore) Save(p *profile.Profile) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, p.ID)
	return nil
}

// TestMainWindow_SaveProfile covers the store write persistProfile wraps: the
// orphaned-password cleanup that depends on the auth method as last saved, and
// the report of whether the profile is new.
func TestMainWindow_SaveProfile(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	newWindow := func(store profile.StoreInterface, kr keyring.Store, tracked *profile.Profile) *MainWindow {
		w := &MainWindow{
			deps:        &MainWindowDeps{ProfileStore: store, KeyringStore: kr},
			profileList: &ProfileList{profileMap: map[string]*profileRow{}},
		}
		if tracked != nil {
			w.profileList.profileMap[tracked.ID] = &profileRow{profile: tracked}
		}
		w.presentError = func(string, string) {}
		return w
	}

	t.Run("switching a tracked profile away from password auth", func(t *testing.T) {
		store := &fakeProfileStore{}
		kr := &fakeKeyring{password: "orphan"}
		w := newWindow(store, kr, &profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword})

		isNew, err := w.saveProfile(&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML})

		assert.NoError(t, err)
		assert.False(t, isNew, "the list already tracks this profile")
		assert.Equal(t, []string{profileID}, store.saved)
		assert.Equal(t, []string{profileID}, kr.deleted,
			"the password the profile no longer uses must not survive the save")
	})

	t.Run("a profile that never used password auth", func(t *testing.T) {
		store := &fakeProfileStore{}
		kr := &fakeKeyring{}
		w := newWindow(store, kr, &profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML})

		isNew, err := w.saveProfile(&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML})

		assert.NoError(t, err)
		assert.False(t, isNew)
		assert.Equal(t, []string{profileID}, store.saved)
		assert.Empty(t, kr.deleted,
			"nothing can be orphaned, so the save must not unlock the keyring")
	})

	t.Run("a profile the list does not track is new", func(t *testing.T) {
		store := &fakeProfileStore{}
		kr := &fakeKeyring{}
		w := newWindow(store, kr, nil)

		isNew, err := w.saveProfile(&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML})

		assert.NoError(t, err)
		assert.True(t, isNew)
		assert.Equal(t, []string{profileID}, store.saved)
		assert.Empty(t, kr.deleted, "a new profile cannot have a stored password")
	})

	t.Run("a failed save keeps the password", func(t *testing.T) {
		store := &fakeProfileStore{saveErr: errors.New("disk full")}
		kr := &fakeKeyring{password: "still needed"}
		w := newWindow(store, kr, &profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodPassword})

		isNew, err := w.saveProfile(&profile.Profile{ID: profileID, AuthMethod: profile.AuthMethodSAML})

		assert.EqualError(t, err, "disk full")
		assert.False(t, isNew)
		assert.Empty(t, kr.deleted,
			"the profile is still a password profile on disk, so it still needs its password")
	})
}

// TestMainWindow_ShowError covers the single error path every failure now takes,
// so a handler that reports one can be tested without a display.
func TestMainWindow_ShowError(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	t.Run("a failed delete reaches the seam", func(t *testing.T) {
		var shown []string
		store := &fakeProfileStore{deleteErr: errors.New("profile directory is read-only")}
		w := &MainWindow{deps: &MainWindowDeps{ProfileStore: store, KeyringStore: &fakeKeyring{}}}
		w.presentError = func(_, message string) { shown = append(shown, message) }

		w.performDeleteProfile(&profile.Profile{ID: profileID})

		assert.Equal(t, []string{"profile directory is read-only"}, shown)
	})
}

// fakeController reports a fixed state and counts Connect and Disconnect
// calls. Methods the window tests never reach are left to the embedded nil
// interface, so an unexpected call panics.
type fakeController struct {
	vpn.VPNController
	state         vpn.ConnectionState
	connects      int
	disconnects   int
	disconnectErr error
	connectErr    error
}

func (f *fakeController) GetState() vpn.ConnectionState { return f.state }
func (f *fakeController) CanConnect() bool               { return f.state.CanConnect() }
func (f *fakeController) CanDisconnect() bool            { return f.state.CanDisconnect() }

func (f *fakeController) Connect(context.Context, *profile.Profile, *vpn.ConnectOptions) error {
	f.connects++
	return f.connectErr
}

func (f *fakeController) Disconnect(context.Context) error {
	f.disconnects++
	if f.disconnectErr != nil {
		return f.disconnectErr
	}
	if !f.state.CanDisconnect() {
		return errors.New("not connected: current state is " + string(f.state))
	}
	return nil
}

// TestMainWindow_ConnectionControlDuringReconnectWait covers the wait between
// reconnect attempts: the window offers Disconnect while the controller is already
// Disconnected or Failed. Activating it must cancel the pending reconnect, not
// connect or ask a controller with nothing to stop to disconnect.
func TestMainWindow_ConnectionControlDuringReconnectWait(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	// newWindow arms a reconnect whose timer cannot fire during the test.
	newWindow := func(state vpn.ConnectionState) (*MainWindow, *fakeController, *reconnect.Manager, *[]string) {
		ctrl := &fakeController{state: state}
		mgr := reconnect.NewManager(reconnect.Config{MaxAttempts: 3, DelaySeconds: 3600}, func(func()) {})
		mgr.StoreConnectedProfile(&profile.Profile{ID: profileID, Name: "work", AutoReconnect: true})
		mgr.StartReconnect()
		t.Cleanup(mgr.Cancel)

		tray := NewTrayIcon()
		tray.SetState(vpn.StateReconnecting)

		var shown []string
		w := &MainWindow{deps: &MainWindowDeps{VPNController: ctrl, ReconnectManager: mgr, Tray: tray}}
		w.presentError = func(_, message string) { shown = append(shown, message) }
		return w, ctrl, mgr, &shown
	}

	for _, state := range []vpn.ConnectionState{vpn.StateDisconnected, vpn.StateFailed} {
		t.Run("window button while the controller is "+string(state), func(t *testing.T) {
			w, ctrl, mgr, shown := newWindow(state)

			w.onConnectClicked()

			assert.Zero(t, ctrl.connects, "a Disconnect click must not start a connection")
			assert.Zero(t, ctrl.disconnects, "the controller has nothing to disconnect")
			assert.Zero(t, mgr.GetAttemptCount(), "the pending reconnect must be cancelled")
			assert.Equal(t, state, trayState(w.deps.Tray), "the tray must stop showing Reconnecting")
			assert.Empty(t, *shown)
		})

		t.Run("tray Disconnect while the controller is "+string(state), func(t *testing.T) {
			w, ctrl, mgr, shown := newWindow(state)

			w.triggerDisconnect()

			assert.Zero(t, ctrl.connects)
			assert.Zero(t, ctrl.disconnects, "the controller has nothing to disconnect")
			assert.Zero(t, mgr.GetAttemptCount(), "the pending reconnect must be cancelled")
			assert.Equal(t, state, trayState(w.deps.Tray), "the tray must stop showing Reconnecting")
			assert.Empty(t, *shown, "stopping a reconnect is not an error")
		})
	}

	t.Run("a reconnect attempt in progress is disconnected", func(t *testing.T) {
		w, ctrl, mgr, shown := newWindow(vpn.StateConnecting)

		w.onConnectClicked()

		assert.Equal(t, 1, ctrl.disconnects, "the running attempt has a process to stop")
		assert.Zero(t, ctrl.connects)
		assert.Zero(t, mgr.GetAttemptCount())
		assert.Empty(t, *shown)
	})
}

// TestMainWindow_ConnectionControlWithoutReconnect keeps the ordinary paths
// intact: with no reconnect pending, the control follows the controller.
func TestMainWindow_ConnectionControlWithoutReconnect(t *testing.T) {
	t.Run("disconnected connects", func(t *testing.T) {
		ctrl := &fakeController{state: vpn.StateDisconnected}
		var shown []string
		w := &MainWindow{deps: &MainWindowDeps{VPNController: ctrl}}
		w.presentError = func(_, message string) { shown = append(shown, message) }

		w.onConnectClicked()

		// connect() bails out on the missing selection, which proves the
		// connect path ran without needing GTK widgets.
		assert.Equal(t, []string{"Please select a profile to connect."}, shown)
		assert.Zero(t, ctrl.disconnects)
	})

	t.Run("connected disconnects", func(t *testing.T) {
		ctrl := &fakeController{state: vpn.StateConnected}
		w := &MainWindow{deps: &MainWindowDeps{VPNController: ctrl}}
		w.presentError = func(string, string) {}

		w.onConnectClicked()

		assert.Equal(t, 1, ctrl.disconnects)
		assert.Zero(t, ctrl.connects)
	})
}

// TestMainWindow_PersistProfile covers the save step shared by the Save button
// and Connect: a profile the list does not track yet must be added to it, or a
// new profile connected before its first Save never appears in the sidebar.
func TestMainWindow_PersistProfile(t *testing.T) {
	const profileID = "3f8a1c6e-1d2b-4c9a-8e7f-0a1b2c3d4e5f"

	type shownProfile struct {
		id    string
		isNew bool
	}

	newWindow := func(store profile.StoreInterface, tracked *profile.Profile) (*MainWindow, *[]shownProfile) {
		w := &MainWindow{
			deps:        &MainWindowDeps{ProfileStore: store, KeyringStore: &fakeKeyring{}},
			profileList: &ProfileList{profileMap: map[string]*profileRow{}},
		}
		if tracked != nil {
			w.profileList.profileMap[tracked.ID] = &profileRow{profile: tracked}
		}
		var shown []shownProfile
		w.showSavedProfile = func(p *profile.Profile, isNew bool) {
			shown = append(shown, shownProfile{id: p.ID, isNew: isNew})
		}
		return w, &shown
	}

	t.Run("a new profile is added to the list", func(t *testing.T) {
		store := &fakeProfileStore{}
		w, shown := newWindow(store, nil)
		p := &profile.Profile{ID: profileID}

		err := w.persistProfile(p)

		assert.NoError(t, err)
		assert.Equal(t, []string{profileID}, store.saved)
		assert.Equal(t, []shownProfile{{id: profileID, isNew: true}}, *shown)
		assert.Same(t, p, w.selectedProfile)
	})

	t.Run("a tracked profile is updated in place", func(t *testing.T) {
		store := &fakeProfileStore{}
		w, shown := newWindow(store, &profile.Profile{ID: profileID})
		p := &profile.Profile{ID: profileID, Name: "renamed"}

		err := w.persistProfile(p)

		assert.NoError(t, err)
		assert.Equal(t, []string{profileID}, store.saved)
		assert.Equal(t, []shownProfile{{id: profileID, isNew: false}}, *shown)
		assert.Same(t, p, w.selectedProfile)
	})

	t.Run("a failed save leaves the list and selection alone", func(t *testing.T) {
		w, shown := newWindow(&fakeProfileStore{saveErr: errors.New("disk full")}, nil)
		previous := &profile.Profile{ID: profileID}
		w.selectedProfile = previous

		err := w.persistProfile(&profile.Profile{ID: profileID})

		assert.EqualError(t, err, "disk full")
		assert.Empty(t, *shown)
		assert.Same(t, previous, w.selectedProfile)
	})
}

// trayState reads the state the tray currently displays.
func trayState(tray *TrayIcon) vpn.ConnectionState {
	tray.mu.RLock()
	defer tray.mu.RUnlock()
	return tray.state
}

// TestMainWindow_DisconnectAndAutoReconnect covers how a user disconnect
// affects the next drop. A successful one must not be reconnected. A failed
// one, such as a cancelled pkexec prompt, leaves the tunnel up, so its next
// real drop must still be reconnected rather than skipped as the user's doing.
func TestMainWindow_DisconnectAndAutoReconnect(t *testing.T) {
	tests := []struct {
		name          string
		disconnectErr error
		wantShown     []string
		wantReconnect bool
	}{
		{name: "successful disconnect", wantReconnect: false},
		{
			name:          "failed disconnect",
			disconnectErr: errors.New("authentication cancelled"),
			wantShown:     []string{"authentication cancelled"},
			wantReconnect: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := &fakeController{state: vpn.StateConnected, disconnectErr: tt.disconnectErr}
			mgr := reconnect.NewManager(reconnect.DefaultConfig(), func(func()) {})
			mgr.StoreConnectedProfile(&profile.Profile{Name: "work", AutoReconnect: true, AuthMethod: profile.AuthMethodCertificate})

			var shown []string
			w := &MainWindow{deps: &MainWindowDeps{VPNController: ctrl, ReconnectManager: mgr}}
			w.presentError = func(_, message string) { shown = append(shown, message) }

			w.disconnect()

			assert.Equal(t, 1, ctrl.disconnects)
			assert.Equal(t, tt.wantShown, shown)
			assert.Equal(t, tt.wantReconnect, mgr.ShouldReconnect(vpn.StateConnected, vpn.StateDisconnected))
		})
	}
}

// TestMainWindow_DoConnectReleasesProfileOnRefusal covers a connect refused
// before any state change, such as an unreachable helper daemon. Nothing will
// release the profile later, so it must not keep naming the tray and alerts.
func TestMainWindow_DoConnectReleasesProfileOnRefusal(t *testing.T) {
	ctrl := &fakeController{state: vpn.StateDisconnected, connectErr: errors.New("helper daemon not available")}
	var shown []string
	w := &MainWindow{deps: &MainWindowDeps{VPNController: ctrl}, logDialog: &LogDialog{}}
	w.presentError = func(_, message string) { shown = append(shown, message) }

	w.doConnect(&profile.Profile{Name: "work"}, &vpn.ConnectOptions{})

	assert.Nil(t, w.connectingProfile)
	assert.Equal(t, []string{"helper daemon not available"}, shown)
}

// TestMainWindow_ActiveProfileName covers which profile the tray and
// notifications name: the one being connected, not whichever profile the user
// has since clicked in the sidebar.
func TestMainWindow_ActiveProfileName(t *testing.T) {
	work := &profile.Profile{Name: "work"}
	home := &profile.Profile{Name: "home"}

	tests := []struct {
		name       string
		connecting *profile.Profile
		selected   *profile.Profile
		want       string
	}{
		{name: "connecting profile wins over the selection", connecting: work, selected: home, want: "work"},
		{name: "selection when nothing is connecting", selected: home, want: "home"},
		{name: "nothing at all", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := &MainWindow{connectingProfile: tt.connecting, selectedProfile: tt.selected}
			assert.Equal(t, tt.want, w.activeProfileName())
		})
	}
}

// TestMainWindow_ReconnectGaveUp covers the end of a reconnect sequence that
// produced no state change of its own, such as a password missing from the
// keyring. The user must see why, and the tray must leave Reconnecting.
func TestMainWindow_ReconnectGaveUp(t *testing.T) {
	ctrl := &fakeController{state: vpn.StateDisconnected}
	tray := NewTrayIcon()
	tray.SetState(vpn.StateReconnecting)

	var shown []string
	w := &MainWindow{deps: &MainWindowDeps{VPNController: ctrl, Tray: tray}}
	w.presentError = func(_, message string) { shown = append(shown, message) }

	w.reconnectGaveUp(errors.New("password not available in keyring"))

	assert.Equal(t, []string{"password not available in keyring"}, shown)
	assert.Equal(t, vpn.StateDisconnected, trayState(tray))
}
