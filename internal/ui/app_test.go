package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeExecutable creates an executable stub file and returns its path.
func writeExecutable(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755))
	return path
}

// TestResolveOpenfortivpnPath covers the precedence between an explicitly
// configured binary path and a PATH lookup. The configured default
// ("/usr/bin/openfortivpn") does not exist on every distribution, so a
// configured path that cannot be resolved must not strand the app.
func TestResolveOpenfortivpnPath(t *testing.T) {
	t.Run("configured path wins over one on PATH", func(t *testing.T) {
		configured := writeExecutable(t, t.TempDir(), "openfortivpn")
		pathDir := t.TempDir()
		onPath := writeExecutable(t, pathDir, "openfortivpn")
		t.Setenv("PATH", pathDir)

		got, err := resolveOpenfortivpnPath(configured)

		assert.NoError(t, err)
		assert.Equal(t, configured, got, "the configured path takes precedence")
		assert.NotEqual(t, onPath, got)
	})

	t.Run("missing configured path falls back to PATH", func(t *testing.T) {
		dir := t.TempDir()
		onPath := writeExecutable(t, dir, "openfortivpn")
		t.Setenv("PATH", dir)

		got, err := resolveOpenfortivpnPath("/nonexistent/openfortivpn")

		assert.NoError(t, err)
		assert.Equal(t, onPath, got, "must fall back to the binary found on PATH")
	})

	t.Run("empty configured path uses PATH", func(t *testing.T) {
		dir := t.TempDir()
		onPath := writeExecutable(t, dir, "openfortivpn")
		t.Setenv("PATH", dir)

		got, err := resolveOpenfortivpnPath("")

		assert.NoError(t, err)
		assert.Equal(t, onPath, got)
	})

	t.Run("errors when neither the configured path nor PATH resolves", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())

		got, err := resolveOpenfortivpnPath("/nonexistent/openfortivpn")

		assert.Error(t, err)
		assert.Empty(t, got)
	})
}
