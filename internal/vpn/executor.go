package vpn

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"syscall"
)

// Process represents a running process with stdin/stdout/stderr pipes.
type Process interface {
	// Start starts the process but does not wait for it to complete.
	Start() error
	// Wait waits for the process to exit and returns the error.
	Wait() error
	// Kill sends a kill signal to the process.
	Kill() error
	// Stdin returns a writer to the process's stdin.
	Stdin() io.WriteCloser
	// Stdout returns a reader from the process's stdout.
	Stdout() io.ReadCloser
	// Stderr returns a reader from the process's stderr.
	Stderr() io.ReadCloser
}

// ProcessExecutor creates processes for execution.
type ProcessExecutor interface {
	// CreateProcess creates a new process with the given command and arguments.
	CreateProcess(ctx context.Context, name string, args ...string) (Process, error)
}

// cmdWithPipes holds a command and its associated pipes.
// This is used as the common base for both realProcess and directProcess.
type cmdWithPipes struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

// newCmdWithPipes creates a command with stdin/stdout/stderr pipes configured.
// The process is started in its own process group to allow killing all child processes.
func newCmdWithPipes(ctx context.Context, name string, args ...string) (*cmdWithPipes, error) {
	cmd := exec.CommandContext(ctx, name, args...)

	// Start process in its own process group so we can kill all children
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	return &cmdWithPipes{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}, nil
}

// RealExecutor implements ProcessExecutor using os/exec.
type RealExecutor struct{}

// NewRealExecutor creates a new RealExecutor.
func NewRealExecutor() *RealExecutor {
	return &RealExecutor{}
}

// CreateProcess creates a real process using exec.CommandContext.
// The process is started in its own process group to allow killing
// all child processes when disconnecting.
func (e *RealExecutor) CreateProcess(ctx context.Context, name string, args ...string) (Process, error) {
	cwp, err := newCmdWithPipes(ctx, name, args...)
	if err != nil {
		return nil, err
	}

	return &realProcess{cmdWithPipes: cwp}, nil
}

// realProcess wraps exec.Cmd to implement Process interface.
type realProcess struct {
	*cmdWithPipes
}

func (p *realProcess) Start() error {
	return p.cmdWithPipes.cmd.Start()
}

func (p *realProcess) Wait() error {
	return p.cmdWithPipes.cmd.Wait()
}

// Kill terminates the process and all its children by killing the process group.
// Since the process may be running as root (via pkexec), we use pkexec to send
// the kill signal if direct signaling fails.
//
// The process is started with Setpgid=true, which creates a new process group
// where PGID equals the PID. Using negative PID in Kill() targets the entire
// process group, ensuring child processes (openfortivpn spawned by pkexec)
// are also terminated.
func (p *realProcess) Kill() error {
	if p.cmdWithPipes.cmd.Process == nil {
		return nil
	}

	pgid := p.cmdWithPipes.cmd.Process.Pid

	// First try sending SIGTERM to the entire process group directly.
	// This works if the process is running as the same user.
	// Using negative pgid kills all processes in the group.
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err == nil {
		return nil
	} else if err == syscall.ESRCH {
		// Process/group already terminated - nothing to do
		return nil
	}

	// Process group is likely running as root (via pkexec).
	// Use pkexec to send SIGTERM to the process group.
	// The "--" ensures negative numbers aren't treated as options.
	// #nosec G204 -- pgid is from syscall.Getpgid(), not user input
	killCmd := exec.Command("pkexec", "kill", "-TERM", "--", fmt.Sprintf("-%d", pgid))
	if err := killCmd.Run(); err != nil {
		// Check if user cancelled the pkexec authentication dialog or pkexec
		// is unavailable. Exit codes 126 (authorization failed/cancelled) and
		// 127 (command not found) indicate we should not retry.
		if isPkexecCancellation(err) {
			return fmt.Errorf("authentication cancelled or pkexec not available: %w", err)
		}
		// SIGTERM failed for another reason, try SIGKILL as last resort
		// #nosec G204 -- pgid is from syscall.Getpgid(), not user input
		killCmd = exec.Command("pkexec", "kill", "-KILL", "--", fmt.Sprintf("-%d", pgid))
		if err := killCmd.Run(); err != nil {
			if isPkexecCancellation(err) {
				return fmt.Errorf("authentication cancelled or pkexec not available: %w", err)
			}
			return fmt.Errorf("failed to kill process group: %w", err)
		}
	}

	return nil
}

// isPkexecCancellation checks if the error indicates the user cancelled
// the pkexec authentication dialog or pkexec is not available.
// Exit code 126 = pkexec authorization failed/cancelled.
// Exit code 127 = command not found.
func isPkexecCancellation(err error) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		code := exitErr.ExitCode()
		return code == 126 || code == 127
	}
	return false
}

func (p *realProcess) Stdin() io.WriteCloser {
	return p.cmdWithPipes.stdin
}

func (p *realProcess) Stdout() io.ReadCloser {
	return p.cmdWithPipes.stdout
}

func (p *realProcess) Stderr() io.ReadCloser {
	return p.cmdWithPipes.stderr
}

// DirectExecutor implements ProcessExecutor for privileged contexts.
// Unlike RealExecutor, it runs commands directly without pkexec wrapper,
// as it's intended for use by the helper daemon which already runs as root.
type DirectExecutor struct{}

// NewDirectExecutor creates a new DirectExecutor.
func NewDirectExecutor() *DirectExecutor {
	return &DirectExecutor{}
}

// CreateProcess creates a process without privilege escalation.
// This is used by the helper daemon which already runs with root privileges.
func (e *DirectExecutor) CreateProcess(ctx context.Context, name string, args ...string) (Process, error) {
	cwp, err := newCmdWithPipes(ctx, name, args...)
	if err != nil {
		return nil, err
	}

	return &directProcess{cmdWithPipes: cwp}, nil
}

// directProcess wraps exec.Cmd for privileged execution contexts.
// Unlike realProcess, it can kill processes directly without pkexec.
type directProcess struct {
	*cmdWithPipes
}

func (p *directProcess) Start() error {
	return p.cmdWithPipes.cmd.Start()
}

func (p *directProcess) Wait() error {
	return p.cmdWithPipes.cmd.Wait()
}

// Kill terminates the process and all its children by killing the process group.
// Since the helper daemon runs as root, we can send signals directly without pkexec.
func (p *directProcess) Kill() error {
	if p.cmdWithPipes.cmd.Process == nil {
		return nil
	}

	pgid := p.cmdWithPipes.cmd.Process.Pid

	// Send SIGTERM to the entire process group.
	// Using negative pgid kills all processes in the group.
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err == nil {
		return nil
	} else if err == syscall.ESRCH {
		// Process/group already terminated - nothing to do
		return nil
	}

	// SIGTERM failed, try SIGKILL as last resort
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return fmt.Errorf("failed to kill process group: %w", err)
	}

	return nil
}

func (p *directProcess) Stdin() io.WriteCloser {
	return p.cmdWithPipes.stdin
}

func (p *directProcess) Stdout() io.ReadCloser {
	return p.cmdWithPipes.stdout
}

func (p *directProcess) Stderr() io.ReadCloser {
	return p.cmdWithPipes.stderr
}
