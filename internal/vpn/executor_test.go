package vpn

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	proc := startTermIgnoringShell(t, e)

	done := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(done)
	}()

	require.NoError(t, proc.Kill())

	select {
	case <-done:
		// Process died — escalation worked.
	case <-time.After(5 * time.Second):
		t.Fatal("process survived Kill: SIGTERM was ignored and no SIGKILL escalation happened")
	}
}

// TestRealProcess_Kill_EscalatesToSigkill covers the pkexec executor's
// same-user path, which signals the group directly the same way directProcess
// does: a process that traps SIGTERM must still be killed, without pkexec.
func TestRealProcess_Kill_EscalatesToSigkill(t *testing.T) {
	oldGrace := sigtermGracePeriod
	sigtermGracePeriod = 300 * time.Millisecond
	defer func() { sigtermGracePeriod = oldGrace }()

	proc := startTermIgnoringShell(t, NewRealExecutor())

	done := make(chan struct{})
	go func() {
		_ = proc.Wait()
		close(done)
	}()

	require.NoError(t, proc.Kill())

	select {
	case <-done:
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

// TestCmdWithPipes_OutputSurvivesWait pins the pipe ownership the completion
// path depends on: exec.Cmd must not close the output read ends when Wait sees
// the process exit, so output buffered at exit is still readable afterwards.
func TestCmdWithPipes_OutputSurvivesWait(t *testing.T) {
	proc, err := NewDirectExecutor().CreateProcess(context.Background(), "sh", "-c",
		`echo "last stdout line"; echo "last stderr line" >&2`)
	require.NoError(t, err)
	require.NoError(t, proc.Start())
	require.NoError(t, proc.Wait())

	stdout, err := io.ReadAll(proc.Stdout())
	require.NoError(t, err, "stdout must still be readable after Wait")
	assert.Equal(t, "last stdout line\n", string(stdout))

	stderr, err := io.ReadAll(proc.Stderr())
	require.NoError(t, err, "stderr must still be readable after Wait")
	assert.Equal(t, "last stderr line\n", string(stderr))

	require.NoError(t, proc.Stdout().Close())
	require.NoError(t, proc.Stderr().Close())
}

// TestCmdWithPipes_StartFailureClosesPipes verifies that a failed start releases
// the output pipes. Their read ends belong to cmdWithPipes rather than exec.Cmd,
// so nothing else would ever close them: the helper daemon would leak two
// descriptors per rejected connect request.
func TestCmdWithPipes_StartFailureClosesPipes(t *testing.T) {
	proc, err := NewDirectExecutor().CreateProcess(context.Background(),
		filepath.Join(t.TempDir(), "no-such-openfortivpn"))
	require.NoError(t, err)
	require.Error(t, proc.Start())

	_, err = proc.Stdout().Read(make([]byte, 1))
	assert.ErrorIs(t, err, os.ErrClosed, "stdout read end must be closed")

	_, err = proc.Stderr().Read(make([]byte, 1))
	assert.ErrorIs(t, err, os.ErrClosed, "stderr read end must be closed")
}

// startTermIgnoringShell starts a shell that ignores SIGTERM and returns once
// the trap is installed, so a Kill test cannot pass on SIGTERM alone.
func startTermIgnoringShell(t *testing.T, e ProcessExecutor) Process {
	t.Helper()
	proc, err := e.CreateProcess(context.Background(), "sh", "-c", `trap "" TERM; echo ready; while true; do sleep 0.1; done`)
	require.NoError(t, err)
	require.NoError(t, proc.Start())

	line, err := bufio.NewReader(proc.Stdout()).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)
	return proc
}
