package profile

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestStore(t *testing.T) (*Store, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "profile-store-test")
	require.NoError(t, err)

	store, err := NewStore(dir)
	require.NoError(t, err)

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	return store, cleanup
}

func validTestProfile() *Profile {
	return &Profile{
		ID:            "550e8400-e29b-41d4-a716-446655440000",
		Name:          "Test VPN",
		Host:          "vpn.example.com",
		Port:          443,
		AuthMethod:    AuthMethodPassword,
		Username:      "testuser",
		SetDNS:        true,
		SetRoutes:     true,
		AutoReconnect: true,
	}
}

func TestNewStore(t *testing.T) {
	dir, err := os.MkdirTemp("", "store-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	storeDir := filepath.Join(dir, "profiles")
	store, err := NewStore(storeDir)

	require.NoError(t, err)
	assert.NotNil(t, store)
	assert.DirExists(t, storeDir)
}

func TestStore_Save(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	p := validTestProfile()
	err := store.Save(p)

	require.NoError(t, err)
	path, err := store.profilePath(p.ID)
	require.NoError(t, err)
	assert.FileExists(t, path)
}

func TestStore_Save_InvalidID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Test missing ID
	p := &Profile{
		Name: "Test",
		Host: "vpn.example.com",
		Port: 443,
	}
	err := store.Save(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "profile ID is required")

	// Test invalid ID format
	p.ID = "not-a-uuid"
	err = store.Save(p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid profile ID format")
}

func TestStore_Save_DraftProfile(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Save should allow draft profiles (incomplete data) with valid ID
	p := &Profile{
		ID:   "550e8400-e29b-41d4-a716-446655440000",
		Name: "Draft Profile",
		// Missing Host, Port, etc. - should still save
	}

	err := store.Save(p)
	require.NoError(t, err, "Save should allow draft profiles")

	// Verify it was saved
	loaded, err := store.Load(p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.Name, loaded.Name)
}

func TestStore_Load(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	original := validTestProfile()
	require.NoError(t, store.Save(original))

	loaded, err := store.Load(original.ID)

	require.NoError(t, err)
	assert.Equal(t, original.ID, loaded.ID)
	assert.Equal(t, original.Name, loaded.Name)
	assert.Equal(t, original.Host, loaded.Host)
	assert.Equal(t, original.Port, loaded.Port)
	assert.Equal(t, original.AuthMethod, loaded.AuthMethod)
}

func TestStore_Load_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Use a valid UUID format that doesn't exist
	_, err := store.Load("00000000-0000-0000-0000-000000000000")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreNotFound)
}

func TestStore_Load_InvalidID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := store.Load("invalid-id-format")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreInvalidID)
}

func TestStore_Delete(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	p := validTestProfile()
	require.NoError(t, store.Save(p))
	path, err := store.profilePath(p.ID)
	require.NoError(t, err)
	require.FileExists(t, path)

	err = store.Delete(p.ID)

	require.NoError(t, err)
	assert.NoFileExists(t, path)
}

func TestStore_Delete_NotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Use a valid UUID format that doesn't exist
	err := store.Delete("00000000-0000-0000-0000-000000000000")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreNotFound)
}

func TestStore_Delete_InvalidID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	err := store.Delete("invalid-id-format")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreInvalidID)
}

func TestStore_List(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	p1 := validTestProfile()
	p2 := validTestProfile()
	p2.ID = "660e8400-e29b-41d4-a716-446655440001"
	p2.Name = "Second VPN"

	require.NoError(t, store.Save(p1))
	require.NoError(t, store.Save(p2))

	result, err := store.List()

	require.NoError(t, err)
	assert.Len(t, result.Profiles, 2)
	assert.Empty(t, result.Errors)
}

func TestStore_List_Empty(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	result, err := store.List()

	require.NoError(t, err)
	assert.Empty(t, result.Profiles)
	assert.Empty(t, result.Errors)
}

func TestStore_Exists(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	p := validTestProfile()
	require.NoError(t, store.Save(p))

	exists, err := store.Exists(p.ID)
	require.NoError(t, err)
	assert.True(t, exists)

	// Use a valid UUID format that doesn't exist
	exists, err = store.Exists("00000000-0000-0000-0000-000000000000")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestStore_Exists_InvalidID(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	_, err := store.Exists("invalid-id-format")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrStoreInvalidID)
}

func TestStore_List_WithCorruptedProfile(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Save a valid profile
	p := validTestProfile()
	require.NoError(t, store.Save(p))

	// Create a corrupted profile file with valid UUID filename
	corruptedID := "660e8400-e29b-41d4-a716-446655440001"
	corruptedPath := filepath.Join(store.baseDir, corruptedID+".json")
	err := os.WriteFile(corruptedPath, []byte("invalid json {{{"), 0600)
	require.NoError(t, err)

	result, err := store.List()

	require.NoError(t, err)
	assert.Len(t, result.Profiles, 1)
	assert.Equal(t, p.ID, result.Profiles[0].ID)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, corruptedID, result.Errors[0].ProfileID)
	assert.Contains(t, result.Errors[0].Err.Error(), "failed to load profile")
}

func TestStore_List_WithInvalidUUIDFilename(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	// Save a valid profile
	p := validTestProfile()
	require.NoError(t, store.Save(p))

	// Create a file with invalid UUID filename
	invalidPath := filepath.Join(store.baseDir, "not-a-uuid.json")
	err := os.WriteFile(invalidPath, []byte(`{"id":"test"}`), 0600)
	require.NoError(t, err)

	result, err := store.List()

	require.NoError(t, err)
	assert.Len(t, result.Profiles, 1)
	assert.Equal(t, p.ID, result.Profiles[0].ID)
	// Invalid UUID filename should be reported as error
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "not-a-uuid", result.Errors[0].ProfileID)
	assert.Contains(t, result.Errors[0].Err.Error(), "invalid profile ID")
}

func TestListError_Error(t *testing.T) {
	err := ListError{
		ProfileID: "550e8400-e29b-41d4-a716-446655440000",
		Err:       fmt.Errorf("test error"),
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "550e8400-e29b-41d4-a716-446655440000")
	assert.Contains(t, errStr, "test error")
}

func TestListError_Unwrap(t *testing.T) {
	// Test that Unwrap returns the underlying error for error chain support
	underlyingErr := fmt.Errorf("underlying error")
	listErr := ListError{
		ProfileID: "550e8400-e29b-41d4-a716-446655440000",
		Err:       underlyingErr,
	}

	// Verify Unwrap returns the underlying error
	unwrapped := listErr.Unwrap()
	assert.Equal(t, underlyingErr, unwrapped)

	// Verify errors.Is works through the chain
	wrappedErr := fmt.Errorf("wrapped: %w", underlyingErr)
	listErrWithWrapped := ListError{
		ProfileID: "test-id",
		Err:       wrappedErr,
	}
	assert.ErrorIs(t, listErrWithWrapped, underlyingErr)
}

func TestStore_Save_ProfileWithDescription(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	p := validTestProfile()
	p.Description = "My work VPN connection"
	err := store.Save(p)
	require.NoError(t, err)

	// Load and verify description is preserved
	loaded, err := store.Load(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "My work VPN connection", loaded.Description)
}

func TestStore_Save_ProfileWithEmptyDescription(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	p := validTestProfile()
	p.Description = ""
	err := store.Save(p)
	require.NoError(t, err)

	// Load and verify empty description is handled
	loaded, err := store.Load(p.ID)
	require.NoError(t, err)
	assert.Equal(t, "", loaded.Description)
}

func TestStore_ConcurrentAccess(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	var wg sync.WaitGroup
	const numGoroutines = 10

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			p := NewProfile(fmt.Sprintf("Profile %d", n))
			p.Host = "vpn.example.com"
			p.Username = "user"
			err := store.Save(p)
			assert.NoError(t, err)
		}(i)
	}
	wg.Wait()

	// Verify all profiles were saved
	result, err := store.List()
	require.NoError(t, err)
	assert.Len(t, result.Profiles, numGoroutines)
	assert.Empty(t, result.Errors)

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.List()
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
}

