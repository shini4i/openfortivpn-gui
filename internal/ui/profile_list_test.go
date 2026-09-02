package ui

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/shini4i/openfortivpn-gui/internal/profile"
)

// TestSyncProfileInSlice_ReplacesMatchingID verifies that syncProfileInSlice
// swaps the entry whose ID matches for the new pointer, leaving every other
// entry untouched.
//
// Regression test: UpdateProfile used to update only the label widgets and the
// profileMap, leaving pl.profiles (which row selection indexes into) holding
// the pre-edit pointer. Re-selecting an edited profile then served stale data
// and a subsequent connect silently persisted the pre-edit values, reverting
// the saved change. The slice must be reconciled whenever a profile is updated.
func TestSyncProfileInSlice_ReplacesMatchingID(t *testing.T) {
	a := &profile.Profile{ID: "11111111-1111-1111-1111-111111111111", Host: "old.example.com"}
	b := &profile.Profile{ID: "22222222-2222-2222-2222-222222222222", Host: "b.example.com"}
	pl := &ProfileList{profiles: []*profile.Profile{a, b}}

	aEdited := &profile.Profile{ID: a.ID, Host: "new.example.com"}
	pl.syncProfileInSlice(aEdited)

	assert.Same(t, aEdited, pl.profiles[0], "matching entry must be replaced with the new pointer")
	assert.Equal(t, "new.example.com", pl.profiles[0].Host, "slice must expose the edited value")
	assert.Same(t, b, pl.profiles[1], "non-matching entries must be left untouched")
}

// TestSyncProfileInSlice_NoMatchLeavesSliceUnchanged verifies that a profile
// whose ID is not present is a no-op rather than appending or panicking.
func TestSyncProfileInSlice_NoMatchLeavesSliceUnchanged(t *testing.T) {
	a := &profile.Profile{ID: "11111111-1111-1111-1111-111111111111", Host: "a.example.com"}
	pl := &ProfileList{profiles: []*profile.Profile{a}}

	orphan := &profile.Profile{ID: "99999999-9999-9999-9999-999999999999", Host: "orphan.example.com"}
	pl.syncProfileInSlice(orphan)

	assert.Len(t, pl.profiles, 1, "no entry must be added for an unknown ID")
	assert.Same(t, a, pl.profiles[0], "existing entry must be left untouched")
}

// TestSyncProfileInSlice_EmptySlice verifies the zero-value case does not panic.
func TestSyncProfileInSlice_EmptySlice(t *testing.T) {
	pl := &ProfileList{}
	assert.NotPanics(t, func() {
		pl.syncProfileInSlice(&profile.Profile{ID: "11111111-1111-1111-1111-111111111111"})
	})
	assert.Empty(t, pl.profiles)
}

// TestProfileForDeletion_ReturnsRefreshedProfile verifies that the delete
// resolver returns the profile currently held in profileMap rather than the
// pointer captured when the row was created.
//
// Regression test: the delete button closure in addProfileRow captures the
// profile pointer at row-creation time. UpdateProfile refreshes
// profileMap[id].profile on edit but cannot reach that captured variable, so
// deleting an edited profile would show pre-edit values (e.g. the old name in
// the confirmation dialog). Resolving by ID at click time fixes this.
func TestProfileForDeletion_ReturnsRefreshedProfile(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	captured := &profile.Profile{ID: id, Name: "old-name"}
	refreshed := &profile.Profile{ID: id, Name: "new-name"}
	pl := &ProfileList{
		profileMap: map[string]*profileRow{
			id: {profile: refreshed},
		},
	}

	got := pl.profileForDeletion(captured)

	assert.Same(t, refreshed, got, "must resolve the current map entry, not the captured pointer")
}

// TestProfileForDeletion_FallsBackToCaptured verifies that when the profile is
// no longer tracked in the map (or its entry has a nil profile), the resolver
// returns the captured pointer so deletion still has a target.
func TestProfileForDeletion_FallsBackToCaptured(t *testing.T) {
	captured := &profile.Profile{ID: "11111111-1111-1111-1111-111111111111", Name: "only-copy"}

	tests := []struct {
		name string
		pl   *ProfileList
	}{
		{"id absent from map", &ProfileList{profileMap: map[string]*profileRow{}}},
		{"nil map", &ProfileList{}},
		{"entry has nil profile", &ProfileList{profileMap: map[string]*profileRow{
			captured.ID: {profile: nil},
		}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Same(t, captured, tt.pl.profileForDeletion(captured))
		})
	}
}

// TestGetProfileByID covers the lookup that a profile save consults for the
// auth method as last saved, which decides whether a stored password is now
// orphaned and must be dropped from the keyring.
func TestGetProfileByID(t *testing.T) {
	tracked := &profile.Profile{
		ID:         "11111111-1111-1111-1111-111111111111",
		AuthMethod: profile.AuthMethodPassword,
	}
	pl := &ProfileList{profileMap: map[string]*profileRow{tracked.ID: {profile: tracked}}}

	assert.Same(t, tracked, pl.GetProfileByID(tracked.ID),
		"must serve the tracked pointer, not a copy of it")
	assert.Nil(t, pl.GetProfileByID("22222222-2222-2222-2222-222222222222"))
	assert.Nil(t, (&ProfileList{}).GetProfileByID(tracked.ID), "a nil map must not panic")
}
