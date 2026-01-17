// Package fileutil provides common file operations.
package fileutil

import (
	"fmt"
	"os"
)

// AtomicWrite writes data to the target path atomically using a write-rename pattern.
// This ensures the target file is never in a partially-written state.
// The temporary file uses the same permissions as specified.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, perm); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath) // Clean up temp file on failure
		return fmt.Errorf("rename to final: %w", err)
	}
	return nil
}
