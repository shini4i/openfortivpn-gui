package vpn

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPkexecCancellation(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "non-exit error",
			err:      errors.New("some other error"),
			expected: false,
		},
		{
			name:     "exit code 126 - authorization cancelled",
			err:      &exec.ExitError{ProcessState: createProcessState(126)},
			expected: true,
		},
		{
			name:     "exit code 127 - command not found",
			err:      &exec.ExitError{ProcessState: createProcessState(127)},
			expected: true,
		},
		{
			name:     "exit code 1 - general error",
			err:      &exec.ExitError{ProcessState: createProcessState(1)},
			expected: false,
		},
		{
			name:     "exit code 0 - success",
			err:      &exec.ExitError{ProcessState: createProcessState(0)},
			expected: false,
		},
		{
			name:     "exit code 255 - other error",
			err:      &exec.ExitError{ProcessState: createProcessState(255)},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPkexecCancellation(tt.err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// createProcessState creates a *os.ProcessState with the given exit code.
// This is a helper for testing that uses a real process to get a ProcessState.
func createProcessState(exitCode int) *os.ProcessState {
	// Run a simple command that exits with the desired code.
	// "exit <code>" via sh is the most portable way.
	cmd := exec.Command("sh", "-c", "exit "+strconv.Itoa(exitCode))
	_ = cmd.Run()
	return cmd.ProcessState
}
