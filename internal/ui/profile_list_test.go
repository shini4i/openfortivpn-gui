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
