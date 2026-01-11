// Package profile provides VPN profile management functionality including
// storage, validation, and CRUD operations for VPN connection profiles.
package profile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

var (
	// ErrStoreNotFound is returned when a requested profile does not exist.
	ErrStoreNotFound = errors.New("profile not found")
	// ErrStoreExists is returned when trying to create a profile that already exists.
	ErrStoreExists = errors.New("profile already exists")
	// ErrStoreInvalidID is returned when an invalid profile ID is provided.
	ErrStoreInvalidID = errors.New("invalid profile ID format")
)

// Backwards compatibility aliases for renamed error sentinels.
var (
	// Deprecated: Use ErrStoreNotFound instead.
	ErrProfileNotFound = ErrStoreNotFound
	// Deprecated: Use ErrStoreExists instead.
	ErrProfileExists = ErrStoreExists
	// Deprecated: Use ErrStoreInvalidID instead.
	ErrInvalidProfileID = ErrStoreInvalidID
)

// StoreReader defines read-only operations for profile storage.
type StoreReader interface {
	// Load retrieves a profile by ID.
	Load(id string) (*Profile, error)
	// List returns all stored profiles along with any errors encountered.
	List() (*ListResult, error)
	// Exists checks if a profile with the given ID exists.
	Exists(id string) (bool, error)
}

// StoreWriter defines write operations for profile storage.
type StoreWriter interface {
	// Save persists a profile to disk.
	Save(p *Profile) error
	// Delete removes a profile by ID.
	Delete(id string) error
}

// StoreInterface defines the complete interface for profile storage operations.
// This interface is implemented by Store and can be used for dependency injection
// and testing purposes.
type StoreInterface interface {
	StoreReader
	StoreWriter
}

// Compile-time check that Store implements StoreInterface.
var _ StoreInterface = (*Store)(nil)

// Store manages persistence of VPN profiles.
type Store struct {
	baseDir string
	mu      sync.RWMutex
}

// NewStore creates a new profile store at the given directory.
func NewStore(baseDir string) (*Store, error) {
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create profile directory: %w", err)
	}

	return &Store{baseDir: baseDir}, nil
}

// profilePath returns the file path for a profile after validating the ID.
// This prevents path traversal attacks by ensuring the ID is a valid UUID.
func (s *Store) profilePath(id string) (string, error) {
	if _, err := uuid.Parse(id); err != nil {
		return "", ErrStoreInvalidID
	}
	return filepath.Join(s.baseDir, id+".json"), nil
}

// Save persists a profile to disk using atomic write (write to temp, then rename).
// Profile validation is skipped during save to allow saving draft profiles.
// Validation should be performed before connecting.
func (s *Store) Save(p *Profile) error {
	// Basic sanity checks (ID must be valid UUID)
	if p.ID == "" {
		return errors.New("profile ID is required")
	}
	if _, err := uuid.Parse(p.ID); err != nil {
		return fmt.Errorf("invalid profile ID format: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal profile: %w", err)
	}

	path, err := s.profilePath(p.ID)
	if err != nil {
		return err
	}

	// Atomic write: write to temp file, then rename
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write profile file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // Clean up temp file on failure
		return fmt.Errorf("failed to finalize profile file: %w", err)
	}

	return nil
}

// Load retrieves a profile by ID.
// Draft profiles (incomplete data) are allowed - validation happens at connect time.
func (s *Store) Load(id string) (*Profile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path, err := s.profilePath(id)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrStoreNotFound
		}
		return nil, fmt.Errorf("failed to read profile file: %w", err)
	}

	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	return &p, nil
}

// Delete removes a profile by ID.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path, err := s.profilePath(id)
	if err != nil {
		return err
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return ErrStoreNotFound
		}
		return fmt.Errorf("failed to delete profile file: %w", err)
	}

	return nil
}

// ListError represents an error encountered while loading a specific profile.
type ListError struct {
	ProfileID string
	Err       error
}

// Error implements the error interface for ListError.
func (e ListError) Error() string {
	return fmt.Sprintf("profile %s: %v", e.ProfileID, e.Err)
}

// Unwrap returns the underlying error, enabling error chain support with errors.Is/As.
func (e ListError) Unwrap() error {
	return e.Err
}

// ListResult contains the results of listing profiles, including any errors.
type ListResult struct {
	Profiles []*Profile
	Errors   []ListError
}

// List returns all stored profiles along with any errors encountered.
func (s *Store) List() (*ListResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read profile directory: %w", err)
	}

	result := &ListResult{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		id := entry.Name()[:len(entry.Name())-5] // Remove .json extension

		// Validate ID format before attempting to load
		if _, err := uuid.Parse(id); err != nil {
			result.Errors = append(result.Errors, ListError{
				ProfileID: id,
				Err:       fmt.Errorf("invalid profile ID in filename: %w", err),
			})
			continue
		}

		p, err := s.loadUnsafe(id)
		if err != nil {
			result.Errors = append(result.Errors, ListError{
				ProfileID: id,
				Err:       fmt.Errorf("failed to load profile: %w", err),
			})
			continue
		}
		result.Profiles = append(result.Profiles, p)
	}

	return result, nil
}

// loadUnsafe loads a profile without acquiring locks (caller must hold lock).
func (s *Store) loadUnsafe(id string) (*Profile, error) {
	path := filepath.Join(s.baseDir, id+".json") // ID already validated by caller
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}

	return &p, nil
}

// Exists checks if a profile with the given ID exists.
func (s *Store) Exists(id string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path, err := s.profilePath(id)
	if err != nil {
		return false, err
	}

	_, err = os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
