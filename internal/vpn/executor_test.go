package vpn

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDirectProcess_Kill_EscalatesToSigkill verifies that a process which
// traps SIGTERM is still terminated: Kill must escalate to SIGKILL after the
// grace period instead of reporting success on mere signal delivery.
//
// Regression test: Kill previously returned nil as soon as SIGTERM was
// delivered, so a wedged openfortivpn that ignored SIGTERM kept the tunnel
// up while the UI reported a successful disconnect.
func TestDirectProcess_Kill_EscalatesToSigkill(t *testing.T) {
	oldGrace := sigtermGracePeriod
	sigtermGracePeriod = 300 * time.Millisecond
	defer func() { sigtermGracePeriod = oldGrace }()

	e := NewDirectExecutor()
	// The shell traps SIGTERM and loops forever; only SIGKILL can stop it.
	proc, err := e.CreateProcess(context.Background(), "sh", "-c", `trap "" TERM; while true; do sleep 0.1; done`)
	require.NoError(t, err)
	require.NoError(t, proc.Start())

	done := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(done)
	}()

	// Give the shell a moment to install the trap.
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, proc.Kill())

	select {
	case <-done:
		// Process died — escalation worked.
	case <-time.After(5 * time.Second):
		t.Fatal("process survived Kill: SIGTERM was ignored and no SIGKILL escalation happened")
	}
}

// TestDirectProcess_Kill_GracefulExitSkipsSigkill verifies that a process
// which honors SIGTERM exits within the grace period without needing SIGKILL.
func TestDirectProcess_Kill_GracefulExitSkipsSigkill(t *testing.T) {
	e := NewDirectExecutor()
	proc, err := e.CreateProcess(context.Background(), "sleep", "30")
	require.NoError(t, err)
	require.NoError(t, proc.Start())

	done := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(done)
	}()

	start := time.Now()
	require.NoError(t, proc.Kill())

	select {
	case <-done:
		assert.Less(t, time.Since(start), sigtermGracePeriod,
			"a SIGTERM-compliant process must exit well before the grace period elapses")
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit after SIGTERM")
	}
}

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
