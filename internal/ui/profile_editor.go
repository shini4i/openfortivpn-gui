package ui

import (
	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
)

// ProfileEditor provides a form for editing VPN profile settings.
type ProfileEditor struct {
	widget *gtk.Box

	// Form fields
	nameRow          *adw.EntryRow
	descriptionRow   *adw.EntryRow
	hostRow          *adw.EntryRow
	portRow          *adw.SpinRow
	realmRow         *adw.EntryRow
	usernameRow      *adw.EntryRow
	authMethodRow    *adw.ComboRow
	otpRow           *adw.SwitchRow
	clientCertRow    *adw.EntryRow
	clientKeyRow     *adw.EntryRow
	trustedCertRow   *adw.EntryRow
	setDNSRow        *adw.SwitchRow
	setRoutesRow     *adw.SwitchRow
	halfInternetRow  *adw.SwitchRow
	autoReconnectRow *adw.SwitchRow

	// Certificate rows group (to show/hide)
	certGroup *adw.PreferencesGroup

	// Save button
	saveButton *gtk.Button

	// Current profile
	currentProfile *profile.Profile

	// Dirty state tracking
	isDirty    bool
	populating bool // True when populating fields to prevent false dirty state

	// Callbacks
	onSave func(p *profile.Profile)
}

// Method combo indices in the profile editor. Mirrors the StringList order
// in setupWidget: 0 = Password, 1 = Certificate, 2 = SAML/SSO.
const (
	methodIndexPassword    uint = 0
	methodIndexCertificate uint = 1
	methodIndexSAML        uint = 2
)

// NewProfileEditor creates a new profile editor widget.
func NewProfileEditor() *ProfileEditor {
	pe := &ProfileEditor{}
	pe.setupWidget()
	return pe
}

// setupWidget creates the profile editor UI.
func (pe *ProfileEditor) setupWidget() {
	pe.widget = gtk.NewBox(gtk.OrientationVertical, 0)

	// Create preferences page for organized groups
	prefsPage := adw.NewPreferencesPage()

	// Profile info group
	profileGroup := adw.NewPreferencesGroup()
	profileGroup.SetTitle("Profile")
	profileGroup.SetDescription("Profile name and description")

	pe.nameRow = adw.NewEntryRow()
	pe.nameRow.SetTitle("Name")
	pe.nameRow.ConnectChanged(pe.markDirty)
	profileGroup.Add(pe.nameRow)

	pe.descriptionRow = adw.NewEntryRow()
	pe.descriptionRow.SetTitle("Description")
	pe.descriptionRow.ConnectChanged(pe.markDirty)
	profileGroup.Add(pe.descriptionRow)

	prefsPage.Add(profileGroup)

	// Connection settings group
	connectionGroup := adw.NewPreferencesGroup()
	connectionGroup.SetTitle("Connection")
	connectionGroup.SetDescription("VPN server connection settings")

	pe.hostRow = adw.NewEntryRow()
	pe.hostRow.SetTitle("Server Host")
	pe.hostRow.SetInputPurpose(gtk.InputPurposeURL)
	pe.hostRow.ConnectChanged(pe.markDirty)
	connectionGroup.Add(pe.hostRow)

	pe.portRow = adw.NewSpinRowWithRange(1, 65535, 1)
	pe.portRow.SetTitle("Port")
	pe.portRow.SetValue(443)
	pe.portRow.ConnectChanged(pe.markDirty)
	connectionGroup.Add(pe.portRow)

	pe.realmRow = adw.NewEntryRow()
	pe.realmRow.SetTitle("Realm")
	pe.realmRow.ConnectChanged(pe.markDirty)
	connectionGroup.Add(pe.realmRow)

	prefsPage.Add(connectionGroup)

	// Authentication settings group
	authGroup := adw.NewPreferencesGroup()
	authGroup.SetTitle("Authentication")
	authGroup.SetDescription("How to authenticate with the VPN server")

	// Auth method combo
	pe.authMethodRow = adw.NewComboRow()
	pe.authMethodRow.SetTitle("Method")
	authMethods := gtk.NewStringList([]string{"Password", "Certificate", "SAML/SSO"})
	pe.authMethodRow.SetModel(authMethods)
	pe.authMethodRow.NotifyProperty("selected", func() {
		pe.updateAuthMethodVisibility()
		pe.markDirty()
	})
	authGroup.Add(pe.authMethodRow)

	// Two-factor toggle. Only relevant for password authentication, where the
	// server additionally requires a one-time token prompted at connect time.
	pe.otpRow = adw.NewSwitchRow()
	pe.otpRow.SetTitle("Require one-time password (2FA)")
	pe.otpRow.SetSubtitle("Prompt for a 2FA token when connecting")
	pe.otpRow.NotifyProperty("active", pe.markDirty)
	authGroup.Add(pe.otpRow)

	pe.usernameRow = adw.NewEntryRow()
	pe.usernameRow.SetTitle("Username")
	pe.usernameRow.ConnectChanged(pe.markDirty)
	authGroup.Add(pe.usernameRow)

	prefsPage.Add(authGroup)

	// Certificate settings group
	pe.certGroup = adw.NewPreferencesGroup()
	pe.certGroup.SetTitle("Certificate Authentication")
	pe.certGroup.SetDescription("Client certificate and key paths")

	pe.clientCertRow = adw.NewEntryRow()
	pe.clientCertRow.SetTitle("Client Certificate")
	pe.clientCertRow.SetInputPurpose(gtk.InputPurposeURL)
	pe.clientCertRow.ConnectChanged(pe.markDirty)
	pe.certGroup.Add(pe.clientCertRow)

	pe.clientKeyRow = adw.NewEntryRow()
	pe.clientKeyRow.SetTitle("Client Key")
	pe.clientKeyRow.SetInputPurpose(gtk.InputPurposeURL)
	pe.clientKeyRow.ConnectChanged(pe.markDirty)
	pe.certGroup.Add(pe.clientKeyRow)

	prefsPage.Add(pe.certGroup)

	// Advanced settings group
	advancedGroup := adw.NewPreferencesGroup()
	advancedGroup.SetTitle("Advanced")
	advancedGroup.SetDescription("Additional connection options")

	pe.trustedCertRow = adw.NewEntryRow()
	pe.trustedCertRow.SetTitle("Trusted Certificate")
	pe.trustedCertRow.SetInputPurpose(gtk.InputPurposeURL)
	pe.trustedCertRow.ConnectChanged(pe.markDirty)
	advancedGroup.Add(pe.trustedCertRow)

	pe.setDNSRow = adw.NewSwitchRow()
	pe.setDNSRow.SetTitle("Set DNS")
	pe.setDNSRow.SetSubtitle("Configure system DNS when connected")
	pe.setDNSRow.SetActive(true)
	pe.setDNSRow.NotifyProperty("active", pe.markDirty)
	advancedGroup.Add(pe.setDNSRow)

	pe.setRoutesRow = adw.NewSwitchRow()
	pe.setRoutesRow.SetTitle("Set Routes")
	pe.setRoutesRow.SetSubtitle("Configure routing table when connected")
	pe.setRoutesRow.SetActive(true)
	pe.setRoutesRow.NotifyProperty("active", pe.markDirty)
	advancedGroup.Add(pe.setRoutesRow)

	pe.halfInternetRow = adw.NewSwitchRow()
	pe.halfInternetRow.SetTitle("Half-Internet Routes")
	pe.halfInternetRow.SetSubtitle("Use two /1 routes instead of replacing the default route")
	pe.halfInternetRow.SetActive(false)
	pe.halfInternetRow.NotifyProperty("active", pe.markDirty)
	advancedGroup.Add(pe.halfInternetRow)

	pe.autoReconnectRow = adw.NewSwitchRow()
	pe.autoReconnectRow.SetTitle("Auto Reconnect")
	pe.autoReconnectRow.SetSubtitle("Automatically reconnect if the connection drops unexpectedly")
	pe.autoReconnectRow.SetActive(true)
	pe.autoReconnectRow.NotifyProperty("active", pe.markDirty)
	advancedGroup.Add(pe.autoReconnectRow)

	prefsPage.Add(advancedGroup)

	// Add clamp for proper width
	clamp := adw.NewClamp()
	clamp.SetMaximumSize(600)
	clamp.SetChild(prefsPage)

	pe.widget.Append(clamp)

	// Save button at the bottom
	buttonBox := gtk.NewBox(gtk.OrientationHorizontal, 0)
	buttonBox.SetHAlign(gtk.AlignCenter)
	buttonBox.SetMarginTop(16)
	buttonBox.SetMarginBottom(16)

	pe.saveButton = gtk.NewButtonWithLabel("Save")
	pe.saveButton.AddCSSClass("suggested-action")
	pe.saveButton.AddCSSClass("pill")
	pe.saveButton.SetSensitive(false)
	pe.saveButton.ConnectClicked(pe.onSaveClicked)
	buttonBox.Append(pe.saveButton)

	pe.widget.Append(buttonBox)

	// Initial visibility state
	pe.updateAuthMethodVisibility()
}

// authMethodToSelection maps a stored AuthMethod to the editor's controls:
// the Method combo index and whether the 2FA (OTP) toggle is on.
//
// OTP is not a distinct method in the UI; it is password authentication with a
// second factor, so it maps to the Password method index with the toggle on.
// Unknown methods fall back to plain password to keep the editor usable.
func authMethodToSelection(m profile.AuthMethod) (methodIndex uint, otpEnabled bool) {
	switch m {
	case profile.AuthMethodOTP:
		return methodIndexPassword, true
	case profile.AuthMethodCertificate:
		return methodIndexCertificate, false
	case profile.AuthMethodSAML:
		return methodIndexSAML, false
	default: // AuthMethodPassword and any unknown value
		return methodIndexPassword, false
	}
}

// selectionToAuthMethod maps the editor's Method combo index and 2FA toggle
// state back to a stored AuthMethod. The toggle is only meaningful for the
// Password method; for certificate/SAML it is ignored.
func selectionToAuthMethod(methodIndex uint, otpEnabled bool) profile.AuthMethod {
	switch methodIndex {
	case methodIndexPassword: // the 2FA toggle promotes Password to OTP
		if otpEnabled {
			return profile.AuthMethodOTP
		}
		return profile.AuthMethodPassword
	case methodIndexCertificate:
		return profile.AuthMethodCertificate
	case methodIndexSAML:
		return profile.AuthMethodSAML
	default: // unexpected index: fall back to plain password
		return profile.AuthMethodPassword
	}
}

// updateAuthMethodVisibility shows/hides fields based on auth method.
func (pe *ProfileEditor) updateAuthMethodVisibility() {
	selected := pe.authMethodRow.Selected()
	isPasswordAuth := selected == methodIndexPassword
	isCertAuth := selected == methodIndexCertificate
	isSAMLAuth := selected == methodIndexSAML

	// Certificate fields only for cert auth
	pe.certGroup.SetVisible(isCertAuth)
	// Username for password auth only (SAML doesn't need it upfront)
	pe.usernameRow.SetVisible(!isCertAuth && !isSAMLAuth)
	// 2FA is a modifier on password auth only
	pe.otpRow.SetVisible(isPasswordAuth)
}

// markDirty is called when any field value changes.
// It is skipped during profile population to avoid false dirty state.
func (pe *ProfileEditor) markDirty() {
	if pe.populating {
		return
	}
	if pe.currentProfile != nil && !pe.isDirty {
		pe.isDirty = true
		pe.saveButton.SetSensitive(true)
	}
}

// onSaveClicked is called when the Save button is clicked.
func (pe *ProfileEditor) onSaveClicked() {
	if pe.onSave != nil && pe.currentProfile != nil {
		pe.onSave(pe.GetProfile())
		pe.isDirty = false
		pe.saveButton.SetSensitive(false)
	}
}

// SetProfile loads a profile into the editor.
func (pe *ProfileEditor) SetProfile(p *profile.Profile) {
	pe.currentProfile = p
	pe.isDirty = false
	pe.saveButton.SetSensitive(false)

	if p == nil {
		pe.clearFields()
		pe.setFieldsEnabled(false)
		return
	}

	// Set populating flag to prevent markDirty during field population
	pe.populating = true
	defer func() {
		pe.populating = false
		pe.isDirty = false
		pe.saveButton.SetSensitive(false)
	}()

	pe.setFieldsEnabled(true)

	// Populate fields
	pe.nameRow.SetText(p.Name)
	pe.descriptionRow.SetText(p.Description)
	pe.hostRow.SetText(p.Host)
	pe.portRow.SetValue(float64(p.Port))
	pe.realmRow.SetText(p.Realm)
	pe.usernameRow.SetText(p.Username)

	// Auth method: 0 = Password, 1 = Certificate, 2 = SAML. OTP loads as the
	// Password method with the 2FA toggle on.
	methodIndex, otpEnabled := authMethodToSelection(p.AuthMethod)
	pe.authMethodRow.SetSelected(methodIndex)
	pe.otpRow.SetActive(otpEnabled)

	// Certificate fields
	pe.clientCertRow.SetText(p.ClientCertPath)
	pe.clientKeyRow.SetText(p.ClientKeyPath)
	pe.trustedCertRow.SetText(p.TrustedCert)

	// Switches
	pe.setDNSRow.SetActive(p.SetDNS)
	pe.setRoutesRow.SetActive(p.SetRoutes)
	pe.halfInternetRow.SetActive(p.HalfInternetRoutes)
	pe.autoReconnectRow.SetActive(p.AutoReconnect)

	pe.updateAuthMethodVisibility()

}

// editorFormValues holds the raw values read from the editor's form widgets.
// It exists so the widget-to-profile merge (applyFormValues) is a pure,
// testable function with no GTK dependency.
type editorFormValues struct {
	Name               string
	Description        string
	Host               string
	Port               int
	Realm              string
	Username           string
	MethodIndex        uint
	OTPEnabled         bool
	ClientCertPath     string
	ClientKeyPath      string
	TrustedCert        string
	SetDNS             bool
	SetRoutes          bool
	HalfInternetRoutes bool
	AutoReconnect      bool
}

// applyFormValues returns a copy of current with all editor-controlled fields
// overwritten from the form values. Starting from a copy (rather than a fresh
// struct) guarantees that any profile field without a form widget survives a
// save unchanged — fields can only be lost by an explicit overwrite here.
func applyFormValues(current *profile.Profile, v editorFormValues) *profile.Profile {
	p := *current

	p.Name = v.Name
	p.Description = v.Description
	p.Host = v.Host
	p.Port = v.Port
	p.Realm = v.Realm
	p.Username = v.Username
	p.AuthMethod = selectionToAuthMethod(v.MethodIndex, v.OTPEnabled)
	p.ClientCertPath = v.ClientCertPath
	p.ClientKeyPath = v.ClientKeyPath
	p.TrustedCert = v.TrustedCert
	p.SetDNS = v.SetDNS
	p.SetRoutes = v.SetRoutes
	p.HalfInternetRoutes = v.HalfInternetRoutes
	p.AutoReconnect = v.AutoReconnect

	return &p
}

// GetProfile returns a copy of the current profile with all editor-controlled
// fields replaced by the form values. Fields not represented in the form are
// preserved from the stored profile. Returns nil if no profile is loaded.
func (pe *ProfileEditor) GetProfile() *profile.Profile {
	if pe.currentProfile == nil {
		return nil
	}

	return applyFormValues(pe.currentProfile, editorFormValues{
		Name:               pe.nameRow.Text(),
		Description:        pe.descriptionRow.Text(),
		Host:               pe.hostRow.Text(),
		Port:               int(pe.portRow.Value()),
		Realm:              pe.realmRow.Text(),
		Username:           pe.usernameRow.Text(),
		MethodIndex:        pe.authMethodRow.Selected(),
		OTPEnabled:         pe.otpRow.Active(),
		ClientCertPath:     pe.clientCertRow.Text(),
		ClientKeyPath:      pe.clientKeyRow.Text(),
		TrustedCert:        pe.trustedCertRow.Text(),
		SetDNS:             pe.setDNSRow.Active(),
		SetRoutes:          pe.setRoutesRow.Active(),
		HalfInternetRoutes: pe.halfInternetRow.Active(),
		AutoReconnect:      pe.autoReconnectRow.Active(),
	})
}

// clearFields resets all fields to empty values.
func (pe *ProfileEditor) clearFields() {
	pe.nameRow.SetText("")
	pe.descriptionRow.SetText("")
	pe.hostRow.SetText("")
	pe.portRow.SetValue(443)
	pe.realmRow.SetText("")
	pe.usernameRow.SetText("")
	pe.authMethodRow.SetSelected(methodIndexPassword)
	pe.otpRow.SetActive(false)
	pe.clientCertRow.SetText("")
	pe.clientKeyRow.SetText("")
	pe.trustedCertRow.SetText("")
	pe.setDNSRow.SetActive(true)
	pe.setRoutesRow.SetActive(true)
	pe.halfInternetRow.SetActive(false)
	pe.autoReconnectRow.SetActive(true)
}

// setFieldsEnabled enables or disables all form fields.
func (pe *ProfileEditor) setFieldsEnabled(enabled bool) {
	pe.nameRow.SetSensitive(enabled)
	pe.descriptionRow.SetSensitive(enabled)
	pe.hostRow.SetSensitive(enabled)
	pe.portRow.SetSensitive(enabled)
	pe.realmRow.SetSensitive(enabled)
	pe.usernameRow.SetSensitive(enabled)
	pe.authMethodRow.SetSensitive(enabled)
	pe.otpRow.SetSensitive(enabled)
	pe.clientCertRow.SetSensitive(enabled)
	pe.clientKeyRow.SetSensitive(enabled)
	pe.trustedCertRow.SetSensitive(enabled)
	pe.setDNSRow.SetSensitive(enabled)
	pe.setRoutesRow.SetSensitive(enabled)
	pe.halfInternetRow.SetSensitive(enabled)
	pe.autoReconnectRow.SetSensitive(enabled)
	pe.saveButton.SetSensitive(enabled && pe.isDirty)
}

// OnSave registers a callback for when the profile is saved.
func (pe *ProfileEditor) OnSave(callback func(p *profile.Profile)) {
	pe.onSave = callback
}

// MarkNewProfile marks the current profile as new (unsaved) and enables the save button.
// This should be called after SetProfile for newly created profiles.
func (pe *ProfileEditor) MarkNewProfile() {
	if pe.currentProfile != nil {
		pe.isDirty = true
		pe.saveButton.SetSensitive(true)
	}
}

// Widget returns the root GTK widget for the profile editor.
func (pe *ProfileEditor) Widget() gtk.Widgetter {
	return pe.widget
}

// ClearSelection clears text selection in all entry rows to prevent visual highlighting.
func (pe *ProfileEditor) ClearSelection() {
	pe.nameRow.SelectRegion(0, 0)
	pe.descriptionRow.SelectRegion(0, 0)
	pe.hostRow.SelectRegion(0, 0)
	pe.realmRow.SelectRegion(0, 0)
	pe.usernameRow.SelectRegion(0, 0)
	pe.clientCertRow.SelectRegion(0, 0)
	pe.clientKeyRow.SelectRegion(0, 0)
	pe.trustedCertRow.SelectRegion(0, 0)
}

// Validate checks if the current profile values are valid.
func (pe *ProfileEditor) Validate() error {
	p := pe.GetProfile()
	if p == nil {
		return nil
	}
	return p.Validate()
}
