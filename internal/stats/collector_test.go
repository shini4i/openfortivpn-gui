package stats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadStatFile_PathTraversal(t *testing.T) {
	c := NewCollector(0)

	tests := []struct {
		name        string
		path        string
		expectError bool
	}{
		{
			name:        "valid sysfs path",
			path:        "/sys/class/net/eth0/statistics/rx_bytes",
			expectError: false, // Will fail to read (file doesn't exist), but path is valid
		},
		{
			name:        "path traversal attempt",
			path:        "/sys/class/net/../../../etc/passwd",
			expectError: true,
		},
		{
			name:        "absolute path outside sysfs",
			path:        "/etc/passwd",
			expectError: true,
		},
		{
			name:        "relative path traversal",
			path:        "/sys/class/net/eth0/../../shadow",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.readStatFile(tt.path)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid stats path")
			}
			// Note: valid paths may still error if file doesn't exist,
			// but that's a different error (not path validation)
		})
	}
}
