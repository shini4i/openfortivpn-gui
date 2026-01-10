package ui

import (
	"context"
	"log/slog"
	"strings"

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
	windowDefaultWidth  = 900
	windowDefaultHeight = 600
)

// MainWindowDeps holds the dependencies required by MainWindow.
type MainWindowDeps struct {
	ProfileStore  profile.StoreInterface
	KeyringStore  keyring.Store
	VPNController *vpn.Controller
	ConfigManager *config.Manager
	Tray          *TrayIcon
	Notifier      *Notifier
	// Ctx is the application-level context for VPN operations.
	// When cancelled, ongoing VPN connections should be terminated.
	Ctx context.Context
}

// MainWindow represents the main application window with split view layout.
type MainWindow struct {
	window *adw.ApplicationWindow
	deps   *MainWindowDeps

	// UI components
	splitView     *adw.NavigationSplitView
	profileList   *ProfileList
	profileEditor *ProfileEditor
	statusDisplay *StatusDisplay
	connectButton *gtk.Button
	logDialog     *LogDialog

	// State
	selectedProfile *profile.Profile

	// Callbacks
	onProfileConnecting func(profileID string)
}

// NewMainWindow creates a new main window instance.
func NewMainWindow(app *adw.Application, deps *MainWindowDeps) *MainWindow {
	w := &MainWindow{
		deps: deps,
	}

	w.setupWindow(app)
	w.setupLayout()
	w.setupCallbacks()
	w.loadProfiles()

	return w
}

// setupWindow creates and configures the application window.
func (w *MainWindow) setupWindow(app *adw.Application) {
	w.window = adw.NewApplicationWindow(&app.Application)
	w.window.SetTitle("OpenFortiVPN")
	w.window.SetDefaultSize(windowDefaultWidth, windowDefaultHeight)

	// Handle window close: hide instead of quit (app stays in tray)
	w.window.ConnectCloseRequest(func() bool {
		// Use IdleAdd to ensure hide happens on GTK main thread
		glib.IdleAdd(func() {
			w.window.SetVisible(false)
		})
		return true // Prevent default close behavior
	})
}

// setupLayout creates the split view layout with sidebar and content.
func (w *MainWindow) setupLayout() {
	// Create the split view
	w.splitView = adw.NewNavigationSplitView()

	// Create sidebar (profile list)
	w.profileList = NewProfileList()
	sidebarPage := w.createSidebarPage()
	w.splitView.SetSidebar(sidebarPage)

	// Create content area (profile editor + status)
	w.profileEditor = NewProfileEditor()
	w.statusDisplay = NewStatusDisplay()
	contentPage := w.createContentPage()
	w.splitView.SetContent(contentPage)

	// Set up adaptive behavior
	w.splitView.SetMinSidebarWidth(250)
	w.splitView.SetMaxSidebarWidth(400)

	// Add breakpoint for mobile/narrow view
	breakpoint := adw.NewBreakpoint(adw.BreakpointConditionParse("max-width: 600sp"))
	breakpoint.AddSetter(w.splitView, "collapsed", true)
	w.window.AddBreakpoint(breakpoint)

	w.window.SetContent(w.splitView)
}

// createSidebarPage creates the navigation page for the sidebar.
func (w *MainWindow) createSidebarPage() *adw.NavigationPage {
	// Header bar with add button
	headerBar := adw.NewHeaderBar()

	addButton := gtk.NewButtonFromIconName("list-add-symbolic")
	addButton.SetTooltipText("Add Profile")
	addButton.ConnectClicked(w.onAddProfile)
	headerBar.PackStart(addButton)

	// Menu button
	menuButton := gtk.NewMenuButton()
	menuButton.SetIconName("open-menu-symbolic")
	menuButton.SetMenuModel(w.createMainMenu())
	headerBar.PackEnd(menuButton)

	// Toolbar view for sidebar
	toolbarView := adw.NewToolbarView()
	toolbarView.AddTopBar(headerBar)
	toolbarView.SetContent(w.profileList.Widget())

	// Create navigation page
	page := adw.NewNavigationPage(toolbarView, "Profiles")
	page.SetTag("sidebar")

	return page
}

// createContentPage creates the navigation page for the main content area.
func (w *MainWindow) createContentPage() *adw.NavigationPage {
	// Create log dialog
	w.logDialog = NewLogDialog()

	// Header bar
	headerBar := adw.NewHeaderBar()

	// View Log button
	logButton := gtk.NewButtonFromIconName("utilities-terminal-symbolic")
	logButton.SetTooltipText("View Connection Log")
	logButton.ConnectClicked(func() {
		w.logDialog.Present(w.window)
	})
	headerBar.PackStart(logButton)

	// Connect/Disconnect button
	w.connectButton = gtk.NewButtonWithLabel("Connect")
	w.connectButton.AddCSSClass("suggested-action")
	w.connectButton.ConnectClicked(w.onConnectClicked)
	headerBar.PackEnd(w.connectButton)

	// Main content box
	contentBox := gtk.NewBox(gtk.OrientationVertical, 0)

	// Add compact status display at top
	contentBox.Append(w.statusDisplay.Widget())

	// Separator
	sep := gtk.NewSeparator(gtk.OrientationHorizontal)
	contentBox.Append(sep)

	// Add profile editor (scrollable, takes available space)
	scrolledWindow := gtk.NewScrolledWindow()
	scrolledWindow.SetVExpand(true)
	scrolledWindow.SetPolicy(gtk.PolicyNever, gtk.PolicyAutomatic)
	scrolledWindow.SetChild(w.profileEditor.Widget())
	contentBox.Append(scrolledWindow)

	// Toolbar view
	toolbarView := adw.NewToolbarView()
	toolbarView.AddTopBar(headerBar)
	toolbarView.SetContent(contentBox)

	// Create navigation page
	page := adw.NewNavigationPage(toolbarView, "Profile")
	page.SetTag("content")

	return page
}

// createMainMenu creates the application menu model.
func (w *MainWindow) createMainMenu() *gio.Menu {
	menu := gio.NewMenu()

	// Add menu items
	menu.Append("Preferences", "app.preferences")
	menu.Append("About", "app.about")
	menu.Append("Quit", "app.quit")

	return menu
}

// setupCallbacks registers callbacks for VPN controller events.
func (w *MainWindow) setupCallbacks() {
	// Profile selection callback
	w.profileList.OnProfileSelected(func(p *profile.Profile) {
		w.selectedProfile = p
		w.profileEditor.SetProfile(p)
		w.updateStatusForProfile(p)
		// Update tray to show which profile will be connected
		if w.deps.Tray != nil && p != nil {
			w.deps.Tray.SetProfileName(p.Name)
		}
	})

	// Profile save callback - save changes when user clicks Save
	w.profileEditor.OnSave(func(p *profile.Profile) {
		if err := w.deps.ProfileStore.Save(p); err != nil {
			w.showError("Error Saving Profile", err.Error())
			return
		}

		// Check if this is a new profile (not in list yet)
		if w.profileList.GetProfileByID(p.ID) == nil {
			// New profile - refresh the list and select it
			w.loadProfiles()
			w.profileList.SelectProfile(p.ID)
		} else {
			// Existing profile - just update the display
			w.profileList.UpdateProfile(p)
		}

		// Keep selected profile reference in sync
		w.selectedProfile = p
	})

	// Profile deletion callback
	w.profileList.OnProfileDeleted(func(p *profile.Profile) {
		w.onDeleteProfile(p)
	})

	// VPN state change callback
	w.deps.VPNController.OnStateChange(func(oldState, newState vpn.ConnectionState) {
		// Update UI on main thread
		w.statusDisplay.SetState(newState)

		// Update connect button state
		w.updateConnectButton(newState)

		// Get profile name for notifications
		profileName := ""
		if w.selectedProfile != nil {
			profileName = w.selectedProfile.Name
		}

		// Update tray
		if w.deps.Tray != nil {
			w.deps.Tray.SetState(newState)
			if profileName != "" {
				w.deps.Tray.SetProfileName(profileName)
			}
		}

		// Send notifications
		if w.deps.Notifier != nil {
			if profileName == "" {
				profileName = "VPN"
			}
			switch newState {
			case vpn.StateConnected:
				w.deps.Notifier.NotifyConnected(profileName)
			case vpn.StateDisconnected:
				w.deps.Notifier.NotifyDisconnected(profileName)
			case vpn.StateFailed:
				w.deps.Notifier.NotifyConnectionFailed(profileName)
			case vpn.StateReconnecting:
				w.deps.Notifier.NotifyReconnecting(profileName)
			}
		}
	})

	// VPN output callback - send logs to dialog
	w.deps.VPNController.OnOutput(func(line string) {
		w.logDialog.AppendLog(line)
	})

	// VPN error callback
	w.deps.VPNController.OnError(func(err error) {
		w.showError("VPN Error", err.Error())
	})

	// VPN event callback for IP assignment and SAML authentication
	w.deps.VPNController.OnEvent(func(event *vpn.OutputEvent) {
		switch event.Type {
		case vpn.EventGotIP:
			if ip := event.GetData("ip"); ip != "" {
				w.statusDisplay.SetAssignedIP(ip)
			}
		case vpn.EventAuthenticate:
			// Open browser for SAML/web authentication
			if url := event.GetData("url"); url != "" {
				w.openBrowser(url)
			}
		}
	})
}

// loadProfiles loads all profiles from the store and populates the list.
func (w *MainWindow) loadProfiles() {
	result, err := w.deps.ProfileStore.List()
	if err != nil {
		w.showError("Error Loading Profiles", err.Error())
		return
	}

	// Log any partial load errors (corrupted or unreadable profile files)
	if len(result.Errors) > 0 {
		for _, listErr := range result.Errors {
			slog.Warn("Failed to load profile", "profile_id", listErr.ProfileID, "error", listErr.Err)
		}
	}

	w.profileList.SetProfiles(result.Profiles)

	if len(result.Profiles) == 0 {
		// No profiles - auto-create a new one for first-time users
		w.onAddProfile()
		return
	}

	// Try to select the last used profile (DefaultProfileID from config)
	profileIDToSelect := w.getDefaultProfileID(result.Profiles)
	w.profileList.SelectProfile(profileIDToSelect)
}

// getDefaultProfileID returns the profile ID to select on startup.
// Uses DefaultProfileID from config if it exists in the profiles list,
// otherwise falls back to the first profile.
// Precondition: profiles slice must not be empty.
func (w *MainWindow) getDefaultProfileID(profiles []*profile.Profile) string {
	if len(profiles) == 0 {
		slog.Error("getDefaultProfileID called with empty profiles slice")
		return ""
	}

	if w.deps.ConfigManager == nil {
		return profiles[0].ID
	}

	cfg := w.deps.ConfigManager.GetConfig()
	if cfg.DefaultProfileID != "" {
		// Verify the default profile still exists
		for _, p := range profiles {
			if p.ID == cfg.DefaultProfileID {
				return cfg.DefaultProfileID
			}
		}
		slog.Debug("Default profile not found, selecting first profile",
			"default_profile_id", cfg.DefaultProfileID)
	}

	return profiles[0].ID
}

// onAddProfile handles the add profile button click.
func (w *MainWindow) onAddProfile() {
	// Create new profile with defaults but don't save yet
	newProfile := profile.NewProfile("")

	// Clear selection in list and set profile in editor for editing
	w.profileList.ClearSelection()
	w.selectedProfile = newProfile
	w.profileEditor.SetProfile(newProfile)
	w.profileEditor.MarkNewProfile()
}

// onDeleteProfile handles profile deletion.
func (w *MainWindow) onDeleteProfile(p *profile.Profile) {
	// Show confirmation dialog
	dialog := adw.NewAlertDialog("Delete Profile?", "")
	dialog.SetBody("Are you sure you want to delete the profile \"" + p.Name + "\"? This action cannot be undone.")
	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("delete", "Delete")
	dialog.SetResponseAppearance("delete", adw.ResponseDestructive)
	dialog.SetDefaultResponse("cancel")
	dialog.SetCloseResponse("cancel")

	dialog.ConnectResponse(func(response string) {
		if response == "delete" {
			w.performDeleteProfile(p)
		}
	})

	dialog.Present(w.window)
}

// performDeleteProfile actually deletes the profile after confirmation.
func (w *MainWindow) performDeleteProfile(p *profile.Profile) {
	// Delete password from keyring
	if err := w.deps.KeyringStore.Delete(p.ID); err != nil {
		slog.Warn("Failed to delete password from keyring", "error", err, "profile_id", p.ID)
	}

	// Delete profile
	if err := w.deps.ProfileStore.Delete(p.ID); err != nil {
		w.showError("Error Deleting Profile", err.Error())
		return
	}

	// Clear selection if this was the selected profile
	if w.selectedProfile != nil && w.selectedProfile.ID == p.ID {
		w.selectedProfile = nil
		w.profileEditor.SetProfile(nil)
	}

	// Refresh the list
	w.loadProfiles()
}

// onConnectClicked handles the connect/disconnect button click.
func (w *MainWindow) onConnectClicked() {
	state := w.deps.VPNController.GetState()

	if state.CanDisconnect() {
		w.disconnect()
	} else if state.CanConnect() {
		w.connect()
	}
}

// connect initiates a VPN connection with the selected profile.
func (w *MainWindow) connect() {
	if w.selectedProfile == nil {
		w.showError("No Profile Selected", "Please select a profile to connect.")
		return
	}

	// Get the current profile data from editor (in case it was modified)
	currentProfile := w.profileEditor.GetProfile()
	if currentProfile == nil {
		w.showError("Invalid Profile", "Profile data is invalid.")
		return
	}

	// Validate the profile before attempting connection
	if err := currentProfile.Validate(); err != nil {
		w.showError("Validation Error", err.Error())
		return
	}

	// Save any changes to the profile
	if err := w.deps.ProfileStore.Save(currentProfile); err != nil {
		w.showError("Error Saving Profile", err.Error())
		return
	}

	// Notify that we're connecting to this profile (for auto-connect tracking)
	if w.onProfileConnecting != nil {
		w.onProfileConnecting(currentProfile.ID)
	}

	// SAML authentication doesn't require password - credentials come from browser
	if currentProfile.AuthMethod == profile.AuthMethodSAML {
		w.doConnect(currentProfile, &vpn.ConnectOptions{})
		return
	}

	// Get password from keyring for password-based auth
	password, err := w.deps.KeyringStore.Get(currentProfile.ID)
	if err != nil || password == "" {
		// Show password dialog
		w.showPasswordDialog(currentProfile)
		return
	}

	// OTP authentication requires an additional one-time password
	if currentProfile.AuthMethod == profile.AuthMethodOTP {
		w.showOTPDialog(currentProfile, password)
		return
	}

	w.doConnect(currentProfile, &vpn.ConnectOptions{Password: password})
}

// showPasswordDialog shows a dialog to enter the password.
func (w *MainWindow) showPasswordDialog(p *profile.Profile) {
	dialog := adw.NewAlertDialog("Enter Password", "")
	dialog.SetBody("Enter the password for " + p.Name)

	// Create password entry
	passwordEntry := adw.NewPasswordEntryRow()
	passwordEntry.SetTitle("Password")
	dialog.SetExtraChild(passwordEntry)

	dialog.AddResponse("cancel", "Cancel")
	dialog.AddResponse("connect", "Connect")
	dialog.SetResponseAppearance("connect", adw.ResponseSuggested)
	dialog.SetDefaultResponse("connect")
	dialog.SetCloseResponse("cancel")

	dialog.ConnectResponse(func(response string) {
		if response == "connect" {
			password := passwordEntry.Text()
			if password != "" {
				// Save password to keyring (log errors but don't block connection)
				if err := w.deps.KeyringStore.Save(p.ID, password); err != nil {
					slog.Warn("Failed to save password to keyring", "error", err, "profile_id", p.ID)
				}
				// OTP authentication requires an additional one-time password
				if p.AuthMethod == profile.AuthMethodOTP {
					w.showOTPDialog(p, password)
					return
				}
				w.doConnect(p, &vpn.ConnectOptions{Password: password})
			}
		}
	})

	dialog.Present(w.window)
}

// showOTPDialog shows a dialog to enter the one-time password for 2FA.
func (w *MainWindow) showOTPDialog(p *profile.Profile, password string) {
	ShowOTPDialog(w.window, func(otp string, cancelled bool) {
		if !cancelled && otp != "" {
			w.doConnect(p, &vpn.ConnectOptions{
				Password: password,
				OTP:      otp,
			})
		}
	})
}

// doConnect performs the actual VPN connection.
func (w *MainWindow) doConnect(p *profile.Profile, opts *vpn.ConnectOptions) {
	// Clear previous logs
	w.logDialog.Clear()

	// Use app-level context for VPN connection (cancelled on app shutdown)
	ctx := w.deps.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if err := w.deps.VPNController.Connect(ctx, p, opts); err != nil {
		w.showError("Connection Error", err.Error())
	}
}

// disconnect terminates the active VPN connection.
func (w *MainWindow) disconnect() {
	if err := w.deps.VPNController.Disconnect(); err != nil {
		w.showError("Disconnect Error", err.Error())
	}
}

// updateStatusForProfile updates the status display for the selected profile.
func (w *MainWindow) updateStatusForProfile(p *profile.Profile) {
	if p == nil {
		w.statusDisplay.SetProfileInfo("")
		return
	}
	w.statusDisplay.SetProfileInfo(p.Name)
}

// updateConnectButton updates the connect button based on VPN state.
func (w *MainWindow) updateConnectButton(state vpn.ConnectionState) {
	glib.IdleAdd(func() {
		if w.connectButton == nil {
			return
		}

		switch {
		case state.CanDisconnect():
			// Connected or connecting - show Disconnect
			w.connectButton.SetLabel("Disconnect")
			w.connectButton.RemoveCSSClass("suggested-action")
			w.connectButton.AddCSSClass("destructive-action")
			w.connectButton.SetSensitive(true)
		case state.CanConnect():
			// Disconnected or failed - show Connect
			w.connectButton.SetLabel("Connect")
			w.connectButton.RemoveCSSClass("destructive-action")
			w.connectButton.AddCSSClass("suggested-action")
			w.connectButton.SetSensitive(true)
		default:
			// Unknown state - disable button
			w.connectButton.SetSensitive(false)
		}
	})
}

// showError displays an error dialog.
func (w *MainWindow) showError(title, message string) {
	dialog := adw.NewAlertDialog(title, message)
	dialog.AddResponse("ok", "OK")
	dialog.SetDefaultResponse("ok")
	dialog.Present(w.window)
}

// Present shows the main window.
func (w *MainWindow) Present() {
	w.window.Present()
}

// Window returns the underlying GTK window.
func (w *MainWindow) Window() *adw.ApplicationWindow {
	return w.window
}

// triggerConnect initiates a connection from external sources (e.g., system tray).
// It uses the currently selected profile.
func (w *MainWindow) triggerConnect() {
	w.connect()
}

// triggerDisconnect terminates the VPN connection from external sources (e.g., system tray).
func (w *MainWindow) triggerDisconnect() {
	w.disconnect()
}

// selectProfileByID selects the profile with the given ID.
// This is used for auto-connect functionality.
func (w *MainWindow) selectProfileByID(profileID string) {
	if w.profileList == nil {
		slog.Error("Cannot select profile: profile list not initialized")
		return
	}
	w.profileList.SelectProfile(profileID)
}

// OnProfileConnecting registers a callback that is called when a profile connection is initiated.
// The callback receives the profile ID being connected to.
func (w *MainWindow) OnProfileConnecting(callback func(profileID string)) {
	w.onProfileConnecting = callback
}

// openBrowser opens the given URL in the default browser for SAML authentication.
func (w *MainWindow) openBrowser(url string) {
	slog.Info("Opening browser for SAML authentication", "url", url)

	// Basic validation - ensure it's an HTTP(S) URL
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://") {
		slog.Warn("Invalid URL scheme, skipping browser launch", "url", url)
		return
	}

	// Use GIO's AppInfo to launch URL in default browser
	// This is more reliable across different desktop environments
	if err := gio.AppInfoLaunchDefaultForURI(url, nil); err != nil {
		slog.Error("Failed to open browser", "error", err)
		w.showError("Browser Error", "Failed to open browser for SAML authentication: "+err.Error())
	}
}
