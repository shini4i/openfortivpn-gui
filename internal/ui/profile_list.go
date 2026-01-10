package ui

import (
	"github.com/diamondburned/gotk4/pkg/pango"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
)

// ProfileList displays a list of VPN profiles in the sidebar.
type ProfileList struct {
	widget *gtk.Box
	list   *gtk.ListBox

	// Profile data
	profiles   []*profile.Profile
	profileMap map[string]*profileRow

	// Callbacks
	onSelected func(p *profile.Profile)
	onDeleted  func(p *profile.Profile)
}

// profileRow holds the GTK row and associated profile data.
type profileRow struct {
	row           *gtk.ListBoxRow
	profile       *profile.Profile
	titleLabel    *gtk.Label
	subtitleLabel *gtk.Label
}

// NewProfileList creates a new profile list widget.
func NewProfileList() *ProfileList {
	pl := &ProfileList{
		profileMap: make(map[string]*profileRow),
	}

	pl.setupWidget()
	return pl
}

// setupWidget creates the profile list UI.
func (pl *ProfileList) setupWidget() {
	// Create container box
	pl.widget = gtk.NewBox(gtk.OrientationVertical, 0)

	// Create list box
	pl.list = gtk.NewListBox()
	pl.list.SetSelectionMode(gtk.SelectionSingle)

	// Handle selection changes
	pl.list.ConnectRowSelected(func(row *gtk.ListBoxRow) {
		if row == nil {
			return
		}

		// Find the profile for this row by comparing row indices
		rowIndex := row.Index()
		if rowIndex >= 0 && rowIndex < len(pl.profiles) {
			p := pl.profiles[rowIndex]
			if pl.onSelected != nil {
				pl.onSelected(p)
			}
		}
	})

	// Add placeholder for empty state
	pl.list.SetPlaceholder(pl.createEmptyPlaceholder())

	pl.widget.Append(pl.list)
}

// createEmptyPlaceholder creates a placeholder widget for when there are no profiles.
func (pl *ProfileList) createEmptyPlaceholder() gtk.Widgetter {
	box := gtk.NewBox(gtk.OrientationVertical, 12)
	box.SetVAlign(gtk.AlignCenter)
	box.SetHAlign(gtk.AlignCenter)
	box.SetMarginTop(24)
	box.SetMarginBottom(24)
	box.SetMarginStart(12)
	box.SetMarginEnd(12)

	icon := gtk.NewImageFromIconName("network-vpn-symbolic")
	icon.SetPixelSize(64)
	icon.AddCSSClass("dim-label")
	box.Append(icon)

	label := gtk.NewLabel("No Profiles")
	label.AddCSSClass("title-2")
	label.AddCSSClass("dim-label")
	box.Append(label)

	sublabel := gtk.NewLabel("Click + to add a VPN profile")
	sublabel.AddCSSClass("dim-label")
	box.Append(sublabel)

	return box
}

// SetProfiles updates the profile list with new profiles.
func (pl *ProfileList) SetProfiles(profiles []*profile.Profile) {
	// Clear existing rows
	pl.clearRows()

	pl.profiles = profiles
	pl.profileMap = make(map[string]*profileRow)

	// Add rows for each profile
	for _, p := range profiles {
		pl.addProfileRow(p)
	}
}

// clearRows removes all rows from the list.
func (pl *ProfileList) clearRows() {
	for {
		row := pl.list.RowAtIndex(0)
		if row == nil {
			break
		}
		pl.list.Remove(row)
	}
}

// addProfileRow adds a single profile row to the list.
func (pl *ProfileList) addProfileRow(p *profile.Profile) {
	row := gtk.NewListBoxRow()
	row.SetActivatable(true)

	// Main horizontal container
	hbox := gtk.NewBox(gtk.OrientationHorizontal, 12)
	hbox.SetMarginTop(8)
	hbox.SetMarginBottom(8)
	hbox.SetMarginStart(12)
	hbox.SetMarginEnd(12)

	// VPN icon (prefix)
	icon := gtk.NewImageFromIconName("network-vpn-symbolic")
	icon.SetVAlign(gtk.AlignCenter)
	hbox.Append(icon)

	// Vertical box for title and subtitle (center, expands)
	textBox := gtk.NewBox(gtk.OrientationVertical, 2)
	textBox.SetVAlign(gtk.AlignCenter)
	textBox.SetHExpand(true)

	titleLabel := gtk.NewLabel(p.Name)
	titleLabel.SetXAlign(0)
	titleLabel.SetEllipsize(pango.EllipsizeEnd)
	titleLabel.AddCSSClass("heading")
	textBox.Append(titleLabel)

	// Show description if available, otherwise show host
	subtitle := p.Host
	if p.Description != "" {
		subtitle = p.Description
	}
	subtitleLabel := gtk.NewLabel(subtitle)
	subtitleLabel.AddCSSClass("caption")
	subtitleLabel.SetOpacity(0.7) // Subtle dimming without being invisible
	subtitleLabel.SetXAlign(0)
	subtitleLabel.SetEllipsize(pango.EllipsizeEnd)
	textBox.Append(subtitleLabel)

	hbox.Append(textBox)

	// Delete button (suffix)
	deleteButton := gtk.NewButtonFromIconName("edit-delete-symbolic")
	deleteButton.SetVAlign(gtk.AlignCenter)
	deleteButton.AddCSSClass("flat")
	deleteButton.SetTooltipText("Delete Profile")

	// Capture profile for closure
	captured := p
	deleteButton.ConnectClicked(func() {
		if pl.onDeleted != nil {
			pl.onDeleted(captured)
		}
	})
	hbox.Append(deleteButton)

	row.SetChild(hbox)

	// Store mapping
	pl.profileMap[p.ID] = &profileRow{
		row:           row,
		profile:       p,
		titleLabel:    titleLabel,
		subtitleLabel: subtitleLabel,
	}

	pl.list.Append(row)
}

// SelectProfile selects the profile with the given ID.
func (pl *ProfileList) SelectProfile(id string) {
	if pr, ok := pl.profileMap[id]; ok {
		pl.list.SelectRow(pr.row)
	}
}

// ClearSelection deselects any currently selected profile.
func (pl *ProfileList) ClearSelection() {
	pl.list.UnselectAll()
}

// GetSelectedProfile returns the currently selected profile, or nil if none.
func (pl *ProfileList) GetSelectedProfile() *profile.Profile {
	row := pl.list.SelectedRow()
	if row == nil {
		return nil
	}

	// Use row index to find the profile
	rowIndex := row.Index()
	if rowIndex >= 0 && rowIndex < len(pl.profiles) {
		return pl.profiles[rowIndex]
	}

	return nil
}

// OnProfileSelected registers a callback for when a profile is selected.
func (pl *ProfileList) OnProfileSelected(callback func(p *profile.Profile)) {
	pl.onSelected = callback
}

// OnProfileDeleted registers a callback for when a profile delete is requested.
func (pl *ProfileList) OnProfileDeleted(callback func(p *profile.Profile)) {
	pl.onDeleted = callback
}

// Widget returns the root GTK widget for the profile list.
func (pl *ProfileList) Widget() gtk.Widgetter {
	return pl.widget
}

// UpdateProfile updates the display of a specific profile in the list.
func (pl *ProfileList) UpdateProfile(p *profile.Profile) {
	if pr, ok := pl.profileMap[p.ID]; ok {
		// Update custom labels (SetTitle/SetSubtitle don't work with custom child)
		pr.titleLabel.SetText(p.Name)

		// Show description if available, otherwise show host
		subtitle := p.Host
		if p.Description != "" {
			subtitle = p.Description
		}
		pr.subtitleLabel.SetText(subtitle)

		pr.profile = p
	}
}

// GetProfileByID returns the profile with the given ID, or nil if not found.
func (pl *ProfileList) GetProfileByID(id string) *profile.Profile {
	if pr, ok := pl.profileMap[id]; ok {
		return pr.profile
	}
	return nil
}
