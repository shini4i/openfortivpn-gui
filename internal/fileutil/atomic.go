// Package fileutil provides common file operations.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWrite writes data to the target path atomically using a write-rename pattern.
// This ensures the target file is never in a partially-written state.
// Uses a unique temporary file to avoid collisions with concurrent writes.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	pattern := filepath.Base(path) + ".tmp.*"

	// Create unique temp file in the same directory (required for atomic rename)
	tmpFile, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	// Clean up temp file on any error
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()

	// Write data
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}

	// Sync to ensure durability before rename
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	// Set permissions (CreateTemp uses 0600 by default)
	if err := os.Chmod(tmpPath, perm); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename to final path: %w", err)
	}

	success = true
	return nil
}
