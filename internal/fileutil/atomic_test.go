package fileutil

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAtomicWrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "atomic-write-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "test.txt")
	data := []byte("hello world")

	err = AtomicWrite(path, data, 0600)
	require.NoError(t, err)

	// Verify file exists with correct content
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, content)

	// Verify permissions
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())

	// Verify temp file was cleaned up
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err))
}

func TestAtomicWrite_OverwriteExisting(t *testing.T) {
	dir, err := os.MkdirTemp("", "atomic-write-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "test.txt")

	// Write initial content
	err = AtomicWrite(path, []byte("initial"), 0600)
	require.NoError(t, err)

	// Overwrite with new content
	err = AtomicWrite(path, []byte("updated"), 0600)
	require.NoError(t, err)

	// Verify new content
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, []byte("updated"), content)
}

func TestAtomicWrite_DirectoryNotExist(t *testing.T) {
	path := "/nonexistent/dir/test.txt"

	err := AtomicWrite(path, []byte("data"), 0600)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create temp file")
}

func TestAtomicWrite_EmptyData(t *testing.T) {
	dir, err := os.MkdirTemp("", "atomic-write-test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()

	path := filepath.Join(dir, "empty.txt")

	err = AtomicWrite(path, []byte{}, 0600)
	require.NoError(t, err)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Empty(t, content)
}
