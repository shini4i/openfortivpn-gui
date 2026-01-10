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
	nameRow         *adw.EntryRow
	descriptionRow  *adw.EntryRow
	hostRow         *adw.EntryRow
	portRow         *adw.SpinRow
	realmRow        *adw.EntryRow
	usernameRow     *adw.EntryRow
	authMethodRow   *adw.ComboRow
	clientCertRow   *adw.EntryRow
	clientKeyRow    *adw.EntryRow
	trustedCertRow  *adw.EntryRow
	setDNSRow       *adw.SwitchRow
	setRoutesRow    *adw.SwitchRow

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

// updateAuthMethodVisibility shows/hides fields based on auth method.
// Index 0 = Password, 1 = Certificate, 2 = SAML/SSO
func (pe *ProfileEditor) updateAuthMethodVisibility() {
	selected := pe.authMethodRow.Selected()
	isCertAuth := selected == 1
	isSAMLAuth := selected == 2

	// Certificate fields only for cert auth
	pe.certGroup.SetVisible(isCertAuth)
	// Username for password auth only (SAML doesn't need it upfront)
	pe.usernameRow.SetVisible(!isCertAuth && !isSAMLAuth)
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

	// Auth method: 0 = Password, 1 = Certificate, 2 = SAML
	switch p.AuthMethod {
	case profile.AuthMethodCertificate:
		pe.authMethodRow.SetSelected(1)
	case profile.AuthMethodSAML:
		pe.authMethodRow.SetSelected(2)
	default:
		pe.authMethodRow.SetSelected(0)
	}

	// Certificate fields
	pe.clientCertRow.SetText(p.ClientCertPath)
	pe.clientKeyRow.SetText(p.ClientKeyPath)
	pe.trustedCertRow.SetText(p.TrustedCert)

	// Switches
	pe.setDNSRow.SetActive(p.SetDNS)
	pe.setRoutesRow.SetActive(p.SetRoutes)

	pe.updateAuthMethodVisibility()
}

// GetProfile returns the current profile with editor values.
func (pe *ProfileEditor) GetProfile() *profile.Profile {
	if pe.currentProfile == nil {
		return nil
	}

	// Create a copy with updated values
	p := &profile.Profile{
		ID:          pe.currentProfile.ID,
		Name:        pe.nameRow.Text(),
		Description: pe.descriptionRow.Text(),
		Host:        pe.hostRow.Text(),
		Port:        int(pe.portRow.Value()),
	}

	p.Realm = pe.realmRow.Text()
	p.Username = pe.usernameRow.Text()

	// Auth method: 0 = Password, 1 = Certificate, 2 = SAML
	switch pe.authMethodRow.Selected() {
	case 1:
		p.AuthMethod = profile.AuthMethodCertificate
	case 2:
		p.AuthMethod = profile.AuthMethodSAML
	default:
		p.AuthMethod = profile.AuthMethodPassword
	}

	// Certificate fields
	p.ClientCertPath = pe.clientCertRow.Text()
	p.ClientKeyPath = pe.clientKeyRow.Text()
	p.TrustedCert = pe.trustedCertRow.Text()

	// Switches
	p.SetDNS = pe.setDNSRow.Active()
	p.SetRoutes = pe.setRoutesRow.Active()

	return p
}

// clearFields resets all fields to empty values.
func (pe *ProfileEditor) clearFields() {
	pe.nameRow.SetText("")
	pe.descriptionRow.SetText("")
	pe.hostRow.SetText("")
	pe.portRow.SetValue(443)
	pe.realmRow.SetText("")
	pe.usernameRow.SetText("")
	pe.authMethodRow.SetSelected(0)
	pe.clientCertRow.SetText("")
	pe.clientKeyRow.SetText("")
	pe.trustedCertRow.SetText("")
	pe.setDNSRow.SetActive(true)
	pe.setRoutesRow.SetActive(true)
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
	pe.clientCertRow.SetSensitive(enabled)
	pe.clientKeyRow.SetSensitive(enabled)
	pe.trustedCertRow.SetSensitive(enabled)
	pe.setDNSRow.SetSensitive(enabled)
	pe.setRoutesRow.SetSensitive(enabled)
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

// Validate checks if the current profile values are valid.
func (pe *ProfileEditor) Validate() error {
	p := pe.GetProfile()
	if p == nil {
		return nil
	}
	return p.Validate()
}
