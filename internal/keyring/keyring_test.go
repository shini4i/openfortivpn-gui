package keyring

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	zkeyring "github.com/zalando/go-keyring"
)

func TestSystemKeyring_Save(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	profileID := "550e8400-e29b-41d4-a716-446655440000"
	password := "super-secret-password"

	err := store.Save(profileID, password)
	require.NoError(t, err)

	// Verify it was stored
	stored, err := store.Get(profileID)
	require.NoError(t, err)
	assert.Equal(t, password, stored)
}

func TestSystemKeyring_Get(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	profileID := "550e8400-e29b-41d4-a716-446655440000"
	password := "my-password"

	// First store a password
	err := store.Save(profileID, password)
	require.NoError(t, err)

	// Then retrieve it
	retrieved, err := store.Get(profileID)
	require.NoError(t, err)
	assert.Equal(t, password, retrieved)
}

func TestSystemKeyring_Get_NotFound(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	// Use a valid UUID that doesn't have a stored password
	_, err := store.Get("00000000-0000-0000-0000-000000000000")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyringCredentialNotFound)
}

func TestSystemKeyring_Delete(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	profileID := "550e8400-e29b-41d4-a716-446655440000"
	password := "to-be-deleted"

	// Store a password first
	err := store.Save(profileID, password)
	require.NoError(t, err)

	// Delete it
	err = store.Delete(profileID)
	require.NoError(t, err)

	// Verify it's gone
	_, err = store.Get(profileID)
	assert.ErrorIs(t, err, ErrKeyringCredentialNotFound)
}

func TestSystemKeyring_Delete_NotFound(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	// Deleting a non-existent password should not error (idempotent)
	// Use a valid UUID that doesn't have a stored password
	err := store.Delete("00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
}

func TestSystemKeyring_Delete_Error(t *testing.T) {
	customErr := errors.New("keyring service unavailable")
	zkeyring.MockInitWithError(customErr)

	store := NewSystemKeyring()
	err := store.Delete("550e8400-e29b-41d4-a716-446655440000")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete credential")
}

func TestSystemKeyring_Save_Error(t *testing.T) {
	customErr := errors.New("keyring service unavailable")
	zkeyring.MockInitWithError(customErr)

	store := NewSystemKeyring()
	// Use a valid UUID to ensure we reach the keyring call
	err := store.Save("550e8400-e29b-41d4-a716-446655440000", "password")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to store credential")
}

func TestSystemKeyring_Get_Error(t *testing.T) {
	customErr := errors.New("keyring service unavailable")
	zkeyring.MockInitWithError(customErr)

	store := NewSystemKeyring()
	// Use a valid UUID to ensure we reach the keyring call
	_, err := store.Get("550e8400-e29b-41d4-a716-446655440000")

	require.Error(t, err)
	// Should wrap the underlying error but not be ErrKeyringCredentialNotFound
	assert.NotErrorIs(t, err, ErrKeyringCredentialNotFound)
}

func TestServiceName(t *testing.T) {
	// Verify the service name constant is set correctly
	assert.Equal(t, "openfortivpn-gui", ServiceName)
}

func TestSystemKeyring_Save_InvalidProfileID(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	err := store.Save("invalid-not-a-uuid", "password")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyringInvalidProfileID)
}

func TestSystemKeyring_Get_InvalidProfileID(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	_, err := store.Get("invalid-not-a-uuid")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyringInvalidProfileID)
}

func TestSystemKeyring_Delete_InvalidProfileID(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	err := store.Delete("invalid-not-a-uuid")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyringInvalidProfileID)
}

func TestSystemKeyring_Save_EmptyProfileID(t *testing.T) {
	zkeyring.MockInit()

	store := NewSystemKeyring()
	err := store.Save("", "password")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrKeyringInvalidProfileID)
}

func TestNewSystemKeyring(t *testing.T) {
	store := NewSystemKeyring()
	assert.NotNil(t, store)
}

func TestSystemKeyring_ImplementsStoreInterface(t *testing.T) {
	// Compile-time check that SystemKeyring implements Store
	var _ Store = (*SystemKeyring)(nil)
}
