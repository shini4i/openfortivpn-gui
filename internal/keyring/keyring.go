// Package keyring provides secure credential storage using the system keyring.
package keyring

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	zkeyring "github.com/zalando/go-keyring"
)

// ServiceName is the identifier used for storing credentials in the system keyring.
const ServiceName = "openfortivpn-gui"

var (
	// ErrKeyringCredentialNotFound is returned when a credential does not exist in the keyring.
	ErrKeyringCredentialNotFound = errors.New("credential not found")
	// ErrKeyringInvalidProfileID is returned when a profile ID is not a valid UUID.
	ErrKeyringInvalidProfileID = errors.New("invalid profile ID: must be a valid UUID")
)

// Store defines the interface for credential storage operations.
type Store interface {
	// Save stores a password for the given profile ID.
	Save(profileID, password string) error
	// Get retrieves the password for the given profile ID.
	Get(profileID string) (string, error)
	// Delete removes the password for the given profile ID.
	Delete(profileID string) error
}

// SystemKeyring implements Store using the system keyring.
type SystemKeyring struct{}

// NewSystemKeyring creates a new SystemKeyring instance.
func NewSystemKeyring() *SystemKeyring {
	return &SystemKeyring{}
}

// Save stores a password for the given profile ID in the system keyring.
// The profileID must be a valid UUID.
func (s *SystemKeyring) Save(profileID, password string) error {
	if err := validateProfileID(profileID); err != nil {
		return err
	}
	err := zkeyring.Set(ServiceName, profileID, password)
	if err != nil {
		return fmt.Errorf("failed to store credential: %w", err)
	}
	return nil
}

// Get retrieves the password for the given profile ID from the system keyring.
// The profileID must be a valid UUID.
// Returns ErrKeyringCredentialNotFound if no password exists for the profile.
func (s *SystemKeyring) Get(profileID string) (string, error) {
	if err := validateProfileID(profileID); err != nil {
		return "", err
	}
	password, err := zkeyring.Get(ServiceName, profileID)
	if err != nil {
		if errors.Is(err, zkeyring.ErrNotFound) {
			return "", ErrKeyringCredentialNotFound
		}
		return "", fmt.Errorf("failed to retrieve credential: %w", err)
	}
	return password, nil
}

// Delete removes the password for the given profile ID from the system keyring.
// The profileID must be a valid UUID.
// This operation is idempotent - it does not return an error if the credential doesn't exist.
func (s *SystemKeyring) Delete(profileID string) error {
	if err := validateProfileID(profileID); err != nil {
		return err
	}
	err := zkeyring.Delete(ServiceName, profileID)
	if err != nil {
		if errors.Is(err, zkeyring.ErrNotFound) {
			// Idempotent - not finding the credential is not an error
			return nil
		}
		return fmt.Errorf("failed to delete credential: %w", err)
	}
	return nil
}

// validateProfileID ensures the profile ID is a valid UUID.
// This maintains consistency with the profile store's security model.
func validateProfileID(profileID string) error {
	if _, err := uuid.Parse(profileID); err != nil {
		return ErrKeyringInvalidProfileID
	}
	return nil
}
